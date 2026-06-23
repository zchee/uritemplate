// Copyright 2026 The uritemplate Authors. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"strings"
	"testing"
	"time"
)

// TestMatchNoCatastrophicBacktracking pins the fix for the exponential-time
// backtracking the scanner exhibited on inputs that an ambiguous expression list
// can split in exponentially many ways. Two distinct blow-up classes are covered:
//
//   - a single exploded varspec over a long run of joinable separators (e.g.
//     "{/list*}" against "/,,,,,,,..."); and
//   - the cross-product of greedy-shrink splits across several expressions and
//     varspecs over a long separator-free run (e.g. "{a,b}{c,d}{e}" against
//     "llll..."). This second class is the one a per-varspec memo misses; only
//     the global (segIdx, pos) memo in matchFrom bounds it.
//
// Before the fix, a sub-100-byte input ran for over a minute; the lengths below
// would be utterly intractable. Each must return well under a generous bound,
// which fails loudly if exponential behavior ever returns while staying robust to
// machine-speed variation.
func TestMatchNoCatastrophicBacktracking(t *testing.T) {
	tests := map[string]struct {
		raw   string
		build func(n int) string
		// maxN bounds the input sizes. Most shapes the memo holds at ~O(n^2) and run
		// to 1024; a few adversarial shapes are only polynomial at a higher degree
		// (see "multi adjacent reserved", below), so they are exercised at sizes that
		// still finish promptly. The point of the test is that none blow up
		// exponentially — every size returns well under the bound.
		maxN int
	}{
		"explode slash list": {
			raw:   "{/list*}",
			build: func(n int) string { return "/" + strings.Repeat(",", n) + "x" },
			maxN:  1024,
		},
		"explode query kv": {
			raw:   "{?keys*}",
			build: func(n int) string { return "/" + strings.Repeat(",", n) + "x" },
			maxN:  1024,
		},
		"multi expression separator-free": {
			raw:   "{a,b}{c,d}{e}",
			build: func(n int) string { return strings.Repeat("l", n) },
			maxN:  1024,
		},
		"multi varspec reserved": {
			raw:   "{a,b,c,d}",
			build: func(n int) string { return strings.Repeat("l", n) },
			maxN:  1024,
		},
		"fragment multi varspec comma run": {
			raw:   "{#x,0}",
			build: func(n int) string { return strings.Repeat(",", n) + "0" },
			maxN:  1024,
		},
		"reserved single var comma run": {
			// The reserved value class admits ',', so a single reserved var over a
			// long comma run with a poison percent-encoding suffix backtracks
			// heavily in the general scanner (the route fast path defers here).
			raw:   "{+path}",
			build: func(n int) string { return strings.Repeat(",", n) + "%X0" },
			maxN:  1024,
		},
		"crosshatch single var comma run": {
			raw:   "{#x}",
			build: func(n int) string { return strings.Repeat(",", n) + "%X0" },
			maxN:  1024,
		},
		"multi adjacent reserved comma run": {
			// Adjacent reserved single-vars over a comma run are the worst case the
			// general scanner handles. The route fast path is dropped for these
			// (ambiguous), and the failed-state memo keeps the general scanner
			// polynomial — but at degree ~3 (vs ~2 for the other shapes), and with a
			// large constant: the memo bounds the degree at 3 regardless of how many
			// reserved vars are adjacent, but each adds to the constant factor. It is
			// always bounded (never the original exponential hang); the low size cap
			// keeps the test fast while still proving no blow-up. (A defense-in-depth
			// step budget was considered and declined: templates are normally trusted,
			// and a budget risks false-negatives on legitimate very large matches.)
			raw:   "{+a}{+b}{+c}{+d}",
			build: func(n int) string { return strings.Repeat(",", n) + "%X0" },
			// Capped low: this O(n^3) shape plus the race detector's ~10x overhead
			// would otherwise approach the 1s bound. The point is "bounded, not
			// exponential", which n<=128 demonstrates.
			maxN: 128,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tmpl := MustNew(tt.raw)
			for n := 32; n <= tt.maxN; n *= 2 {
				input := tt.build(n)
				start := time.Now()
				_ = tmpl.Match(input) // result is irrelevant; it must return promptly
				// A generous bound: exponential backtracking would take many seconds
				// to minutes at these sizes, while the polynomial fix stays in the low
				// milliseconds (allowing headroom for the race detector's overhead).
				if elapsed := time.Since(start); elapsed > 2*time.Second {
					t.Fatalf("%s: Match on size %d took %v (>2s): catastrophic backtracking regressed",
						tt.raw, n, elapsed)
				}
			}
		})
	}
}

// TestMatchMemoPreservesResults guards that the global failed-state memo did not
// change any match outcome: for inputs that legitimately match, Match must still
// succeed and capture the same units.
func TestMatchMemoPreservesResults(t *testing.T) {
	tests := map[string]struct {
		raw   string
		input string
		key   string
		want  []string
	}{
		"explode single":         {raw: "{/list*}", input: "/red", key: "list", want: []string{"red"}},
		"explode multiple":       {raw: "{/list*}", input: "/red/green/blue", key: "list", want: []string{"red", "green", "blue"}},
		"explode trailing empty": {raw: "{/list*}", input: "/red/", key: "list", want: []string{"red", ""}},
		"multi var first":        {raw: "{a,b}", input: "x,y", key: "a", want: []string{"x"}},
		"multi var second":       {raw: "{a,b}", input: "x,y", key: "b", want: []string{"y"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := MustNew(tt.raw).Match(tt.input)
			if got == nil {
				t.Fatalf("Match(%q) = nil, want a match", tt.input)
			}
			v := got.Get(tt.key)
			if len(v.V) != len(tt.want) {
				t.Fatalf("Match(%q)[%q] = %v, want %v", tt.input, tt.key, v.V, tt.want)
			}
			for i := range tt.want {
				if v.V[i] != tt.want[i] {
					t.Errorf("Match(%q)[%q] unit %d = %q, want %q", tt.input, tt.key, i, v.V[i], tt.want[i])
				}
			}
		})
	}
}
