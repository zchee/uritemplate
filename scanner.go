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
	segs []segMatcher
	keys []string
}

type scanner struct {
	input string
	prog  *prog
	caps  [][]int
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
			p.segs = append(p.segs, literalMatcher(string(e)))
		case *expression:
			p.segs = append(p.segs, expressionMatcher(e, intern))
		}
	}
	return p
}

// match runs the scanner over expansion using the pre-built segment matchers and
// returns the captured variables, or nil if the template does not match.
func match(p *prog, expansion string) Values {
	s := scanner{
		input: expansion,
		prog:  p,
		caps:  make([][]int, len(p.keys)),
	}

	// The whole template matches only if every segment matches AND the final
	// continuation lands exactly at end-of-input (the whole input must be consumed).
	if !s.matchFrom(0, 0) {
		return nil
	}

	out := make(Values, len(p.keys))
	for id, idx := range s.caps {
		if len(idx) == 0 {
			continue
		}
		v := Value{V: make([]string, len(idx)/2)}
		for i := range v.V {
			v.V[i] = pctDecode(s.input[idx[2*i]:idx[2*i+1]])
		}
		if len(v.V) == 1 {
			v.T = ValueTypeString
		} else {
			v.T = ValueTypeList
		}
		out[p.keys[id]] = v
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
	return s.prog.segs[segIdx](s, pos, segIdx)
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
	return matchVarspecs(s, e, named, capIDs, segIdx, i+1, pos)
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
	var loop func(p int) bool
	loop = func(p int) bool {
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

	saved := len(s.caps[capID])
	// Greedy: try the longest span first, then shrink toward lo.
	for j := len(ends) - 1; j >= lo; j-- {
		end := ends[j]
		s.caps[capID] = append(s.caps[capID][:saved], start, end)
		if k(end) {
			return true
		}
	}
	// Restore: no span worked, drop our appended segment(s).
	s.caps[capID] = s.caps[capID][:saved]
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
	for {
		if maxlen > 0 && len(ends)-1 >= maxlen {
			break
		}
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
