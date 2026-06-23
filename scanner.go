// Copyright 2026 The uritemplate Authors. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"strconv"
	"unicode/utf8"
)

// scanner is a bespoke recursive-descent matcher that inverts the expansion
// grammar implemented by (*expression).expand / (Value).expand via direct,
// allocation-light backtracking.
//
// The walk is continuation-passing: each matching step consumes some prefix of
// the input and then invokes a continuation k(pos) standing for "match the rest
// of the template starting at input[pos]". A step returns true as soon as some
// alternative makes the whole continuation succeed. Alternatives are tried in
// priority order — the present/longer/separator-included form before the
// absent/shorter form — which reproduces the leftmost-greedy, present-before-absent
// capture choices frozen in match_golden_test.go (TestMatchGoldenCapture), including
// the deliberately quirky one-group-per-expression cases.
//
// Captures are recorded as [start,end) byte ranges appended to caps[capID]
// (indexed by a deduplicated capture-key id); on a failed alternative the slice
// is truncated back to its saved length so no stale range survives a backtrack.
// Percent-decoding is deferred until a full match is found, so the only
// allocations on the hot path are the per-capture segment slices and the final
// result Values.
//
// The scanner matches every RFC 6570 corpus case and every Expand-produced
// expansion exactly as the original Pike-VM matcher did. It is not a strict
// superset on hand-crafted, non-Expand-reachable inputs: for a duplicated,
// exploded variable name in a reserved/named expression fed degenerate
// separator-only input, the ordered backtracking can resolve an ambiguous split
// toward nil where the parallel-NFA VM matched an empty value. It never
// over-matches (only ever nil where the VM matched, never the reverse); see
// TestMatchStricterThanVM.
// prog is the compiled, cached form of a template's match program: the segment
// matchers plus the deduplicated list of capture keys. Each varspec is assigned
// a capId (an index into keys) at build time; varspecs that share a capture key
// (e.g. the two "who" in "{/who,who}", or a name and its prefix-truncated twin
// only when the keys are equal) share a capId so their segments merge into one
// captured value, reproducing the map-keyed behavior of the original matcher.
type prog struct {
	segs  []segMatcher
	keys  []string
	route *routeProg

	// ambiguous is set when the template can split a given input in more than one
	// way, so the general scanner needs the failed-state memo to stay polynomial.
	// Simple templates (each expression a single non-explode varspec, e.g.
	// "{host}/{path}") are unambiguous: each segment consumes a determined span, so
	// matchFrom never revisits a state and the memo would be pure overhead.
	ambiguous bool
}

type routeProg struct {
	parts []routePart
}

type routePart struct {
	lit           string
	first         string
	capID         int
	allowReserved bool
	isVar         bool
}

type scanner struct {
	input string
	prog  *prog
	caps  captureLists

	// failed memoizes backtracking states already proven not to match, so an
	// ambiguous template cannot re-explore the same state via different upstream
	// splits. Both matcher recursions are pure functions of their state — no
	// matcher branches on captured content, and captures are write-only scratch
	// restored on every backtrack — so once a state fails it always fails. Caching
	// that collapses the otherwise-exponential cross-product of greedy-shrink splits
	// (within one expression's varspec list and across expressions, e.g.
	// "{#x,0}" or "{a,b}{c,d}{e}" over a long joinable run) to polynomial time,
	// closing a denial-of-service hazard without changing any match result.
	//
	// Keys are packed by failedKey; the map is allocated lazily and only for
	// templates flagged ambiguous, so simple templates stay allocation-free.
	failed map[uint64]bool
}

type captureLists struct {
	lists []captureList
}

type captureList struct {
	n      int
	inline [2]int
	extra  []int
}

func (c *captureLists) init(lists []captureList) {
	c.lists = lists
}

func (c *captureLists) len(id int) int {
	return c.lists[id].n
}

func (c *captureLists) append(id, start, end int) {
	c.lists[id].append(start, end)
}

func (c *captureLists) truncate(id, n int) {
	c.lists[id].truncate(n)
}

func (c *captureList) append(start, end int) {
	if c.extra != nil {
		c.extra = append(c.extra[:c.n], start, end)
		c.n += 2
		return
	}
	if c.n+2 <= len(c.inline) {
		c.inline[c.n] = start
		c.inline[c.n+1] = end
		c.n += 2
		return
	}

	c.extra = make([]int, c.n, max(8, c.n+2))
	copy(c.extra, c.inline[:c.n])
	c.extra = append(c.extra, start, end)
	c.n += 2
}

func (c *captureList) truncate(n int) {
	c.n = n
	if c.extra != nil {
		c.extra = c.extra[:n]
	}
}

func (c *captureList) at(i int) int {
	if c.extra != nil {
		return c.extra[i]
	}
	return c.inline[i]
}

// segMatcher matches one top-level template segment (a literals run or an
// *expression) starting at pos, invoking k on success. It returns true iff some
// match lets k succeed.
type segMatcher func(s *scanner, pos, segIdx int) bool

// buildProg compiles tmpl.exprs into a prog: the segment matchers the scanner
// walks plus the deduplicated capture keys. The matchers are stateless (they
// take *scanner as a parameter rather than capturing it), so the prog is cached
// on the Template and reused across Match calls.
func buildProg(exprs []template) *prog {
	p := &prog{segs: make([]segMatcher, 0, len(exprs))}
	routeOK := true
	routeParts := make([]routePart, 0, len(exprs))
	keyID := map[string]int{}
	intern := func(key string) int {
		if id, ok := keyID[key]; ok {
			return id
		}
		id := len(p.keys)
		keyID[key] = id
		p.keys = append(p.keys, key)
		return id
	}
	for i := range exprs {
		switch e := exprs[i].(type) {
		case literals:
			lit := string(e)
			p.segs = append(p.segs, literalMatcher(lit))
			if routeOK {
				routeParts = append(routeParts, routePart{lit: lit})
			}
		case *expression:
			p.segs = append(p.segs, expressionMatcher(e, intern))
			// An expression introduces split ambiguity unless it is a single simple
			// varspec with the unreserved value class: one non-explode, non-prefixed
			// varspec whose value run admits only unreserved bytes (which exclude the
			// separators, so the run divides exactly one way). Anything else —
			// multiple varspecs, an explode loop, a prefix modifier, or the reserved
			// value class (which admits "," and so lets a run divide many ways) —
			// can blow up exponentially in the general scanner against an adversarial
			// input, so it needs the failed-state memo. Note this is independent of
			// route eligibility: the route fast path is non-backtracking, but it can
			// defer to the general scanner, which must stay bounded on its own.
			if !canRouteMatch(e) || e.allow&runeClassR == runeClassR {
				p.ambiguous = true
			}
			if routeOK && canRouteMatch(e) {
				spec := e.vars[0]
				routeParts = append(routeParts, routePart{
					first:         e.first,
					capID:         intern(specKey(spec)),
					allowReserved: e.allow&runeClassR == runeClassR,
					isVar:         true,
				})
			} else {
				routeOK = false
			}
		}
	}
	// Use the route fast path only for unambiguous templates. The route matcher is
	// a separate recursive backtracker with no memoization, so for an ambiguous
	// shape (e.g. several adjacent reserved single-vars like "{+a}{+b}{+c}{+d}")
	// it would blow up to O(n^k) on adversarial input. Those templates fall through
	// to the general scanner, whose failed-state memo keeps them polynomial; the
	// route path stays for the simple unreserved-single-var case it was built for.
	if routeOK && !p.ambiguous {
		p.route = &routeProg{parts: routeParts}
	}
	return p
}

func canRouteMatch(e *expression) bool {
	if e.named || len(e.vars) != 1 {
		return false
	}
	spec := e.vars[0]
	return !spec.explode && spec.maxlen == 0
}

// match runs the scanner over expansion using the pre-built segment matchers and
// returns the captured variables, or nil if the template does not match.
func match(p *prog, expansion string) Values {
	if p.route != nil {
		if out, ok := matchRoute(p.route, p.keys, expansion); ok {
			return out
		}
	}

	s := scanner{
		input: expansion,
		prog:  p,
	}
	var inlineCaps [4]captureList
	if len(p.keys) <= len(inlineCaps) {
		s.caps.init(inlineCaps[:len(p.keys)])
	} else {
		s.caps.init(make([]captureList, len(p.keys)))
	}

	// The whole template matches only if every segment matches AND the final
	// continuation lands exactly at end-of-input (the whole input must be consumed).
	if !s.matchFrom(0, 0) {
		return nil
	}

	return materializeMatch(p.keys, &s.caps, expansion)
}

func materializeMatch(keys []string, caps *captureLists, input string) Values {
	totalValues := 0
	for i := range caps.lists {
		totalValues += caps.lists[i].n / 2
	}
	values := make([]string, totalValues)
	valueOffset := 0

	out := make(Values, len(keys))
	for id := range caps.lists {
		capList := &caps.lists[id]
		if capList.n == 0 {
			continue
		}
		n := capList.n / 2
		v := Value{V: values[valueOffset : valueOffset+n : valueOffset+n]}
		valueOffset += n
		for i := range v.V {
			v.V[i] = pctDecode(input[capList.at(2*i):capList.at(2*i+1)])
		}
		if len(v.V) == 1 {
			v.T = ValueTypeString
		} else {
			v.T = ValueTypeList
		}
		out[keys[id]] = v
	}
	return out
}

type routeStatus uint8

const (
	routeNo routeStatus = iota
	routeYes
	routeFallback
)

type routeCapture struct {
	start int
	end   int
	set   bool
}

func matchRoute(p *routeProg, keys []string, input string) (Values, bool) {
	var caps [4]routeCapture
	if len(keys) > len(caps) {
		return nil, false
	}
	switch matchRouteFrom(p, input, &caps, 0, 0) {
	case routeYes:
		return materializeRouteMatch(keys, &caps, input), true
	case routeNo:
		return nil, true
	default:
		return nil, false
	}
}

func matchRouteFrom(p *routeProg, input string, caps *[4]routeCapture, partIdx, pos int) routeStatus {
	if partIdx == len(p.parts) {
		if pos == len(input) || (pos == 0 && !routeRuneFollows(input, 0)) {
			return routeYes
		}
		return routeNo
	}

	part := p.parts[partIdx]
	if !part.isVar {
		end := pos + len(part.lit)
		if end > len(input) || input[pos:end] != part.lit {
			return routeNo
		}
		return matchRouteFrom(p, input, caps, partIdx+1, end)
	}

	if bodyPos, ok := consumeLiteral(input, pos, part.first); ok {
		switch matchRouteValue(p, input, caps, part, bodyPos, partIdx+1) {
		case routeYes:
			return routeYes
		case routeFallback:
			return routeFallback
		}
	}
	return matchRouteFrom(p, input, caps, partIdx+1, pos)
}

func matchRouteValue(p *routeProg, input string, caps *[4]routeCapture, part routePart, start, nextPart int) routeStatus {
	var buf [64]int
	ends := valueEnds(buf[:0], input, start, part.allowReserved, 0)
	saved := caps[part.capID]

	for j := len(ends) - 1; j >= 0; j-- {
		end := ends[j]
		caps[part.capID] = routeCapture{start: start, end: end, set: true}

		switch matchRouteFrom(p, input, caps, nextPart, end) {
		case routeYes:
			return routeYes
		case routeFallback:
			caps[part.capID] = saved
			return routeFallback
		}
		if _, ok := consumeLiteral(input, end, ","); ok {
			caps[part.capID] = saved
			return routeFallback
		}
	}

	caps[part.capID] = saved
	return routeNo
}

func routeRuneFollows(input string, pos int) bool {
	if pos >= len(input) {
		return false
	}
	_, size := utf8.DecodeRuneInString(input[pos:])
	return pos+size < len(input)
}

func materializeRouteMatch(keys []string, caps *[4]routeCapture, input string) Values {
	totalValues := 0
	for i := range keys {
		if caps[i].set {
			totalValues++
		}
	}
	values := make([]string, totalValues)
	valueOffset := 0

	out := make(Values, len(keys))
	for id, key := range keys {
		cap := caps[id]
		if !cap.set {
			continue
		}
		values[valueOffset] = pctDecode(input[cap.start:cap.end])
		v := Value{
			T: ValueTypeString,
			V: values[valueOffset : valueOffset+1 : valueOffset+1],
		}
		valueOffset++
		out[key] = v
	}
	return out
}

// matchFrom matches segment[segIdx] at pos and recurses to the next segment.
// Terminal acceptance requires consuming the whole input: a match is accepted
// only when pos == len(input), OR when pos == 0 AND no rune follows position 0
// (i.e., the input is empty or contains a single rune). This anchoring rule
// freezes the capture behavior: an all-empty match is only valid when no rune
// follows the current position. For example, Match of "{undef}" accepts "é"
// (one rune, captured as "") but rejects "éa".
func (s *scanner) matchFrom(segIdx, pos int) bool {
	if segIdx == len(s.prog.segs) {
		return pos == len(s.input) || (pos == 0 && !s.runeFollows(0))
	}
	// Unambiguous templates reach each state at most once, so the memo would only
	// add overhead. Run them directly. Ambiguous templates memoize segment-entry
	// failures to bound cross-expression backtracking.
	if !s.prog.ambiguous {
		return s.prog.segs[segIdx](s, pos, segIdx)
	}
	key := failedKey(segIdx, segmentEntryVarspecIdx, pos)
	if s.failed[key] {
		return false
	}
	if s.prog.segs[segIdx](s, pos, segIdx) {
		return true
	}
	s.markFailed(key)
	return false
}

// segmentEntryVarspecIdx is the varspec-index sentinel for a matchFrom
// segment-entry state, distinct from any real varspec index (0..len-1) so a
// segment-entry key never collides with a matchVarspecs key at the same
// (segIdx, pos). A segment is entered at its raw pos, whereas matchVarspecs sees
// pos after e.first is consumed, so the two are genuinely different states.
const segmentEntryVarspecIdx = 0xffff

// failedKey packs a backtracking state (segment index, varspec index within the
// expression, byte position) into a single map key. The fields are small in
// practice (a template has few segments and varspecs; pos fits in 32 bits for any
// realistic input), so the packed key is collision-free.
func failedKey(segIdx, varspecIdx, pos int) uint64 {
	return uint64(segIdx)<<48 | uint64(varspecIdx)<<32 | uint64(uint32(pos))
}

// markFailed records that a state cannot match, allocating the memo on first use.
func (s *scanner) markFailed(key uint64) {
	if s.failed == nil {
		s.failed = make(map[uint64]bool)
	}
	s.failed[key] = true
}

// loopMemo returns a per-position failed-set for a value-run loop, or nil for
// unambiguous templates that cannot blow up and so need no memo. The returned
// slice is scoped to one body-matcher call: each call's continuation differs, so
// memos must not be shared across calls.
func (s *scanner) loopMemo() []bool {
	if !s.prog.ambiguous {
		return nil
	}
	return make([]bool, len(s.input)+1)
}

// matchNext resumes matching at the segment following segIdx. Segment matchers
// call it instead of an opaque continuation closure, so the scanner pointer does
// not escape through the segMatcher func-field call.
func (s *scanner) matchNext(segIdx, pos int) bool {
	return s.matchFrom(segIdx+1, pos)
}

// runeFollows reports whether a rune begins strictly after the rune at byte
// position pos, matching the matcher's at() "next" flag (pos+width < len).
func (s *scanner) runeFollows(pos int) bool {
	if pos >= len(s.input) {
		return false
	}
	_, size := utf8.DecodeRuneInString(s.input[pos:])
	return pos+size < len(s.input)
}

// literalMatcher matches the literal text verbatim at pos.
func literalMatcher(lit string) segMatcher {
	return func(s *scanner, pos, segIdx int) bool {
		end := pos + len(lit)
		if end > len(s.input) || s.input[pos:end] != lit {
			return false
		}
		return s.matchNext(segIdx, end)
	}
}

// expressionMatcher matches an expression, which as a whole is optional: it may
// consume e.first plus its varspec list, or contribute nothing at all. The
// present alternative (consuming the expression) is tried before the absent
// alternative (skipping it entirely), matching the priority order of the
// expansion grammar.
func expressionMatcher(e *expression, intern func(string) int) segMatcher {
	named := e.allow&runeClassR == runeClassR // value-char class selector
	// Precompute each varspec's capture-key id once, at build time.
	capIDs := make([]int, len(e.vars))
	for i := range e.vars {
		capIDs[i] = intern(specKey(e.vars[i]))
	}
	return func(s *scanner, pos, segIdx int) bool {
		// Present: consume e.first, then the varspec chain.
		if p, ok := consumeLiteral(s.input, pos, e.first); ok {
			if matchVarspecs(s, e, named, capIDs, segIdx, 0, p) {
				return true
			}
		}
		// Absent: the entire expression matched nothing.
		return s.matchNext(segIdx, pos)
	}
}

// matchVarspecs matches expr.vars[i:] starting at pos and chains to k. Each
// varspec is independently optional; the expansion grammar ((*expression).expand
// and (Value).expand) defines the priority order for alternatives:
//
//	[separator + body]  >  (i>0) [body, no separator]  >  [varspec absent]
//
// This order ensures the first varspec is matched greedily before later ones,
// and the separator can be skipped for non-first varspecs if an earlier varspec
// was absent. var0 has no leading separator (the expansion grammar emits the
// separator block only for i>0), so it offers just [body] > [absent].
func matchVarspecs(s *scanner, e *expression, named bool, capIDs []int, segIdx, i, pos int) bool {
	if i == len(e.vars) {
		return s.matchNext(segIdx, pos)
	}
	// Memoize failed (segIdx, i, pos) states for ambiguous templates: a varspec
	// list can divide a value run in exponentially many ways (e.g. "{#x,0}" over a
	// long comma run), and the three alternatives below reach the same state via
	// different upstream splits. matchVarspecs is a pure function of (segIdx, i,
	// pos), so caching failures keeps the search polynomial. The real varspec index
	// i (0..len-1) never collides with matchFrom's segmentEntryVarspecIdx sentinel.
	var key uint64
	if s.prog.ambiguous {
		key = failedKey(segIdx, i, pos)
		if s.failed[key] {
			return false
		}
	}

	spec := e.vars[i]
	capID := capIDs[i]
	cont := func(next int) bool {
		return matchVarspecs(s, e, named, capIDs, segIdx, i+1, next)
	}

	// Alternative 1: leading separator (if any) followed by the body.
	if bodyPos, ok := consumeLiteral(s.input, pos, leadingSep(e, i)); ok {
		if matchVarspecBody(s, e, named, spec, capID, bodyPos, cont) {
			return true
		}
	}

	// Alternative 2 (i>0 only): body without the leading separator. This lets a
	// non-first varspec be the one that actually starts the output when every
	// earlier varspec was absent.
	if i > 0 {
		if matchVarspecBody(s, e, named, spec, capID, pos, cont) {
			return true
		}
	}

	// Alternative 3: this varspec (and its separator) contributes nothing.
	if matchVarspecs(s, e, named, capIDs, segIdx, i+1, pos) {
		return true
	}

	if s.prog.ambiguous {
		s.markFailed(key)
	}
	return false
}

// leadingSep returns the separator that precedes varspec i: e.sep for i>0,
// nothing for the first varspec.
func leadingSep(e *expression, i int) string {
	if i > 0 {
		return e.sep
	}
	return ""
}

// matchVarspecBody dispatches to the three grammar shapes (bare, named single,
// named explode) and records this varspec's captured segment(s) under its key.
// On any failure of the continuation it leaves caps untouched (each callee
// restores its own appends).
func matchVarspecBody(s *scanner, e *expression, named bool, spec varspec, capID, pos int, k func(int) bool) bool {
	switch {
	case e.named && spec.explode:
		return matchNamedExplode(s, e, named, spec, capID, pos, k)
	case e.named && !spec.explode:
		return matchNamedSingle(s, e, named, spec, capID, pos, k)
	default:
		return matchBareVar(s, e, named, spec, capID, pos, k)
	}
}

// matchBareVar handles the !named cases (parseOpSimple/Plus/Crosshatch/Dot/
// Slash). The value is one or more value-runs joined by e.sep (explode) or ','
// (non-explode); each run is captured as its own segment, so N>1 runs yield a
// List. The segment loop prioritizes stopping (exiting the loop to yield the rest
// of the input to the following varspec) before continuing to consume another
// segment, matching the expansion grammar's priority. Each value-run is itself
// greedy-with-shrink.
func matchBareVar(s *scanner, e *expression, named bool, spec varspec, capID, pos int, k func(int) bool) bool {
	joiner := ","
	if spec.explode {
		joiner = e.sep
	}
	// The memo bounds the value-run loop. valueRun enumerates every candidate span
	// end, and loop re-enters itself at those ends, so without memoization a
	// reserved or exploded value over a long joinable run (e.g. "{+path}" against
	// ",,,,,,...") re-explores the same position exponentially. loop(p) is a pure
	// function of p for a fixed continuation k, so caching failed positions keeps
	// it polynomial.
	failed := s.loopMemo()
	var loop func(p int) bool
	loop = func(p int) bool {
		if failed != nil && failed[p] {
			return false
		}
		// Stop here first: yielding the rest of the input to what follows takes
		// priority over consuming another segment.
		if k(p) {
			return true
		}
		// Continue: consume the joiner then another value-run. The value group
		// always permits an empty span (the expansion grammar allows zero-unit
		// captures), so a trailing joiner yields an extra empty segment, e.g.
		// "{/list*}" against a string with a trailing "/".
		if jp, ok := consumeLiteral(s.input, p, joiner); ok {
			if valueRun(s, named, spec, capID, jp, loop, true) {
				return true
			}
		}
		if failed != nil {
			failed[p] = true
		}
		return false
	}
	// The very first value-run; min length 0 is allowed (an empty value is a
	// legal capture, e.g. "{/var,empty}" with empty="" ).
	return valueRun(s, named, spec, capID, pos, loop, true)
}

// matchNamedSingle handles named && !explode (e.g. "{;list}", "{?x}"). It
// matches spec.name, then either "=" value with a ',' value continuation, or the
// empty form name+ifemp. The "=value(,value)*" branch is tried before the empty
// branch, and the continuation loop is preferred over stopping, all
// greedy-with-shrink.
func matchNamedSingle(s *scanner, e *expression, named bool, spec varspec, capID, pos int, k func(int) bool) bool {
	p, ok := consumeLiteral(s.input, pos, spec.name)
	if !ok {
		return false
	}

	// Branch A: name "=" value (',' value)* .
	if ep, ok := consumeLiteral(s.input, p, "="); ok {
		var loop func(q int) bool
		loop = func(q int) bool {
			// The ','-continuation is preferred over stopping (the compiled loop
			// reaches its continue thread before its exit thread), and the value
			// group permits an empty span, so a trailing ',' yields an extra
			// empty segment (e.g. "{;list}" against ";list=red,").
			if cp, ok := consumeLiteral(s.input, q, ","); ok {
				if valueRun(s, named, spec, capID, cp, loop, true) {
					return true
				}
			}
			return k(q)
		}
		if valueRun(s, named, spec, capID, ep, loop, true) {
			return true
		}
	}

	// Branch B: empty form name+ifemp (no value captured: the expansion grammar
	// permits skipping the entire capture group, so caps[key] is not touched here).
	if ip, ok := consumeLiteral(s.input, p, e.ifemp); ok {
		return k(ip)
	}
	return false
}

// matchNamedExplode handles named && explode (e.g. "{;list*}", "{?list*}"). It
// matches a sequence of "name=value" pairs joined by e.sep, or the empty form
// name+ifemp for a single empty value. Each value occurrence is captured under
// key, so multiple pairs yield a List. The "=value" form is preferred over the
// empty form, and continuing the loop is preferred over stopping.
func matchNamedExplode(s *scanner, e *expression, named bool, spec varspec, capID, pos int, k func(int) bool) bool {
	// one matches a single "name=value" (or name+ifemp) occurrence at q and then
	// either loops via e.sep into another occurrence or stops. nameDone reports
	// that spec.name has already been consumed for the current occurrence.
	var occurrence func(q int, nameDone bool) bool
	occurrence = func(q int, nameDone bool) bool {
		p := q
		if !nameDone {
			np, ok := consumeLiteral(s.input, q, spec.name)
			if !ok {
				return false
			}
			p = np
		}

		// Branch A: "=" value, then optionally e.sep + next occurrence.
		if ep, ok := consumeLiteral(s.input, p, "="); ok {
			if valueRun(s, named, spec, capID, ep, func(end int) bool {
				// Continue: e.sep then another name=value occurrence.
				if sp, ok := consumeLiteral(s.input, end, e.sep); ok {
					if occurrence(sp, false) {
						return true
					}
				}
				// Stop after this occurrence.
				return k(end)
			}, true) {
				return true
			}
		}

		// Branch B: empty form name+ifemp; then optionally loop.
		if ip, ok := consumeLiteral(s.input, p, e.ifemp); ok {
			if sp, ok := consumeLiteral(s.input, ip, e.sep); ok {
				if occurrence(sp, false) {
					return true
				}
			}
			return k(ip)
		}
		return false
	}
	return occurrence(pos, false)
}

// valueRun matches a single value-run: a maximal greedy span of value chars
// (raw allowed byte, "%XX" triplet, each counting as one unit), then shrinks one
// unit at a time on continuation failure. It records the matched [start,end)
// range under key (appending one segment) and truncates that append if the
// continuation ultimately fails. When allowEmpty is true the run may match zero
// units (the empty capture the expansion grammar permits).
//
// maxlen, when > 0, caps the number of units (matching the prefix modifier in
// RFC 6570); units are counted, not bytes, so a "%XX" triplet or any value
// char is one unit.
func valueRun(s *scanner, named bool, spec varspec, capID, start int, k func(int) bool, allowEmpty bool) bool {
	// Enumerate unit boundaries from start up to the greedy maximum, honoring
	// maxlen. ends[j] is the byte offset after consuming j units. A stack-local
	// array holds the common short-value case so no heap allocation occurs; only
	// values longer than len(buf) units fall back to a heap slice.
	//
	var buf [64]int
	ends := valueEnds(buf[:0], s.input, start, named, spec.maxlen)

	lo := 0
	if !allowEmpty {
		lo = 1
		if len(ends) <= 1 {
			// No unit available but a non-empty value is required.
			return false
		}
	}

	saved := s.caps.len(capID)
	// Greedy: try the longest span first, then shrink toward lo.
	for j := len(ends) - 1; j >= lo; j-- {
		end := ends[j]
		s.caps.truncate(capID, saved)
		s.caps.append(capID, start, end)
		if k(end) {
			return true
		}
	}
	// Restore: no span worked, drop our appended segment(s).
	s.caps.truncate(capID, saved)
	return false
}

// valueEnds appends to dst the byte offsets after consuming 0,1,2,... value
// units from input[start], stopping at the first non-value byte or after maxlen
// units when maxlen > 0. The result's first element is always start. Passing a
// stack-array-backed dst keeps short value runs allocation-free; append grows to
// the heap only when a run exceeds dst's capacity.
func valueEnds(dst []int, input string, start int, named bool, maxlen int) []int {
	ends := append(dst, start)
	pos := start
	for maxlen <= 0 || len(ends)-1 < maxlen {
		n, ok := valueUnit(input, pos, named)
		if !ok {
			break
		}
		pos += n
		ends = append(ends, pos)
	}
	return ends
}

// valueUnit reports the byte length of the single value unit at input[pos]:
//   - an ASCII byte permitted by the expression's table (tblUnreserved for the
//     U class, tblUnreservedReserved when the R bit is set), length 1; or
//   - a "%XX" percent-encoded triplet, length 3.
//
// A raw byte >= utf8.RuneSelf is never a value char (expanded values are always
// percent-encoded), so it ends the run. ok is false when no unit starts here.
func valueUnit(input string, pos int, named bool) (int, bool) {
	if pos >= len(input) {
		return 0, false
	}
	c := input[pos]
	if c == '%' {
		if pos+2 < len(input) && ishex(input[pos+1]) && ishex(input[pos+2]) {
			return 3, true
		}
		return 0, false
	}
	if c >= utf8.RuneSelf {
		return 0, false
	}
	if named {
		if tblUnreservedReserved[c] {
			return 1, true
		}
		return 0, false
	}
	if tblUnreserved[c] {
		return 1, true
	}
	return 0, false
}

// consumeLiteral advances past lit at input[pos] if it matches, reporting the
// new position. An empty lit always matches and advances nothing.
func consumeLiteral(input string, pos int, lit string) (int, bool) {
	if lit == "" {
		return pos, true
	}
	end := pos + len(lit)
	if end > len(input) || input[pos:end] != lit {
		return pos, false
	}
	return end, true
}

// specKey is the capture key for spec: "name:maxlen" when a prefix modifier is
// present (RFC 6570 prefix modifier), otherwise the bare name.
func specKey(spec varspec) string {
	if spec.maxlen > 0 {
		return spec.name + ":" + strconv.Itoa(spec.maxlen)
	}
	return spec.name
}
