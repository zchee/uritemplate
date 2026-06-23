// Copyright 2026 The uritemplate Authors. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"testing"
)

// fuzzVars is a fixed, representative variable set used by the fuzz targets so
// expansion is deterministic for any parsed template.
var fuzzVars = Values{
	"var":   String("value"),
	"hello": String("Hello World!"),
	"path":  String("/foo/bar"),
	"list":  List("red", "green", "blue"),
	"keys":  KV("semi", ";", "dot", "."),
	"x":     String("1024"),
	"y":     String("768"),
	"empty": String(""),
	"who":   String("fred"),
}

// FuzzParseExpandMatch drives the whole pipeline on arbitrary template strings.
// It asserts the invariants the optimization work must preserve:
//
//   - parsing never panics (the parser ASCII fast path and pre-sizing);
//   - Expand never panics and AppendExpand produces byte-identical output to
//     Expand (the shared expandInto core, escape chunk-copy, AppendExpand grow);
//   - an expansion produced by Expand always matches the template it came from
//     (the Expand->Match round-trip the scanner must honor).
func FuzzParseExpandMatch(f *testing.F) {
	for _, c := range testTemplateCases {
		f.Add(c.raw)
	}
	f.Add("{var}")
	f.Add("https://example.com/{?list*}{#hello}")
	f.Add("{+path:3}")

	f.Fuzz(func(t *testing.T, raw string) {
		tmpl, err := New(raw)
		if err != nil {
			return // not a valid template; nothing more to check
		}

		got, err := tmpl.Expand(fuzzVars)
		if err != nil {
			return // expansion can legitimately fail (e.g. invalid UTF-8 values)
		}

		// AppendExpand must agree with Expand exactly. This is the core invariant
		// of the additive append API and is what this fuzz target most directly
		// guards: it shares expandInto with Expand, so any divergence is a real bug
		// in the append path or the escape chunk-copy.
		appended, aerr := tmpl.AppendExpand(nil, fuzzVars)
		if aerr != nil || string(appended) != got {
			t.Fatalf("AppendExpand diverged from Expand for %q: got %q (err %v), want %q",
				raw, appended, aerr, got)
		}

		// Cross-engine check: the scanner's Match must agree with the regexp engine
		// on whether the expansion matches, the same invariant as
		// TestMatchRegexpDifferential. Two pre-existing, documented divergence
		// classes are excluded (they are matcher capture-semantics gaps unrelated
		// to this performance work, see hasMatcherDivergence): exploded KV and
		// prefix-modified (:N) varspecs, where the one-group-per-expression grammar
		// makes the capture split ambiguous. A disagreement outside those classes
		// is a real regression.
		if hasMatcherDivergence(tmpl, fuzzVars) {
			return
		}
		matchOK := tmpl.Match(got) != nil
		regexpOK := tmpl.Regexp().MatchString(got)
		if matchOK != regexpOK {
			t.Fatalf("engine disagreement for %q on %q: Match()=%v, Regexp().MatchString()=%v",
				raw, got, matchOK, regexpOK)
		}
	})
}

// hasMatcherDivergence reports whether expanding tmpl with vars exercises a
// construct where the bespoke scanner and the regexp engine are known to disagree
// independent of this performance work. Both stem from the shared
// one-group-per-expression regexp grammar, which cannot encode the precise
// capture split the scanner applies:
//
//   - a prefix-modified varspec (e.g. {#list:1}): the regexp accepts a
//     comma-joined run the scanner declines because the scanner enforces the :N
//     unit cap during capture; and
//   - an exploded varspec resolving to a KV value (e.g. {.keys*}): the "k=v"
//     separators make the capture split ambiguous for any operator.
//
// The KV class is detected from the runtime value (vars), not the template shape,
// because whether {x*} diverges depends on x being a KV rather than a list. These
// are pre-existing capture-semantics gaps in the matcher; excluding them keeps the
// fuzz oracle focused on regressions introduced by changes under test.
func hasMatcherDivergence(tmpl *Template, vars Values) bool {
	for i := range tmpl.exprs {
		expr, ok := tmpl.exprs[i].(*expression)
		if !ok {
			continue
		}
		for _, v := range expr.vars {
			if v.maxlen > 0 {
				return true
			}
			if v.explode && vars.Get(v.name).T == ValueTypeKV {
				return true
			}
		}
	}
	return false
}

// FuzzMatchNoPanic feeds arbitrary inputs to Match for a fixed set of templates,
// asserting the bespoke scanner and the route fast path never panic on hostile
// input (truncated percent-encoding, stray separators, partial matches).
func FuzzMatchNoPanic(f *testing.F) {
	seeds := []string{
		"https://example.com/users/kevin/pics",
		"/one/two/three",
		"a%2",       // truncated percent-encoding
		"%",         // lone percent
		"x?y=z&p=q", // query soup
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	templates := []*Template{
		MustNew("https://{host}/users{/user}{/media}"),
		MustNew("{/list*}"),
		MustNew("https://example.com/foo{?bar}"),
		MustNew("{+path}"),
	}

	f.Fuzz(func(t *testing.T, input string) {
		for _, tmpl := range templates {
			// Must not panic; result value is irrelevant here.
			_ = tmpl.Match(input)
		}
	})
}
