// Copyright 2026 The uritemplate Authors. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"strings"
	"testing"
)

// This file holds the representative benchmark matrix used as the optimization
// baseline. The original four benchmarks in uritemplate_test.go are kept intact
// for historical continuity; this matrix broadens coverage to the cases that
// matter for "world fastest" claims: string/list/KV/reserved expansion, long
// safe and long percent-encoded values, and match/no-match/encoded inputs.
//
// Every benchmark uses for b.Loop() so the timer boundaries and keep-alive are
// handled by the testing framework. Setup (parse, value construction, dst
// preallocation) happens before the loop and is not measured.

// benchLongSafe is a ~1KB run of bytes that need no percent-encoding under the
// unreserved set, so the escape path can copy them in bulk.
var benchLongSafe = strings.Repeat("abcdefghijklmnopqrstuvwxyz0123456789-._~", 26) // 1040 bytes

// benchLongEscaped is a ~1KB run dominated by bytes that must be percent-encoded
// (space and reserved punctuation), so the escape path takes the slow branch on
// nearly every byte.
var benchLongEscaped = strings.Repeat("a b/c?d#e[f]g{h}i j&k=l", 46) // 1058 bytes

// expandMatrixVars supplies one well-typed value per benchmark variable so each
// Expand sub-benchmark exercises a distinct value shape.
var expandMatrixVars = Values{
	"var":      String("value"),
	"hello":    String("Hello World!"),       // forces percent-encoding
	"list":     List("red", "green", "blue"), // exploded/joined list
	"keys":     KV("semi", ";", "dot", ".", "comma", ","),
	"path":     String("/foo/bar"), // reserved expansion target
	"longsafe": String(benchLongSafe),
	"longesc":  String(benchLongEscaped),
}

// expandCase pairs a template with a human-readable matrix label.
type expandCase struct {
	name string
	raw  string
}

var expandMatrix = []expandCase{
	{"StringSafe", "{var}"},
	{"StringEncoded", "{hello}"},
	{"List", "{list}"},
	{"ListExplode", "{list*}"},
	{"KV", "{keys}"},
	{"KVExplode", "{keys*}"},
	{"Reserved", "{+path}"},
	{"LongSafe1KB", "{longsafe}"},
	{"LongEscaped1KB", "{+longesc}"},
	{"Mixed", "https://example.com{/var}{?list*}"},
}

func BenchmarkExpand(b *testing.B) {
	for _, c := range expandMatrix {
		tmpl := MustNew(c.raw)
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := tmpl.Expand(expandMatrixVars); err != nil {
					b.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// BenchmarkAppendExpand measures the additive caller-buffer API against the
// allocating Expand wrapper for the same templates. The Preallocated variant
// reuses one buffer across iterations and must report 0 allocs/op; the wrapper
// rows allocate the result string and are measured for honest comparison.
func BenchmarkAppendExpand(b *testing.B) {
	for _, c := range expandMatrix {
		tmpl := MustNew(c.raw)
		b.Run("Preallocated/"+c.name, func(b *testing.B) {
			buf := make([]byte, 0, 4096)
			b.ReportAllocs()
			for b.Loop() {
				var err error
				buf, err = tmpl.AppendExpand(buf[:0], expandMatrixVars)
				if err != nil {
					b.Fatalf("unexpected error: %v", err)
				}
			}
		})
		b.Run("Wrapper/"+c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := tmpl.Expand(expandMatrixVars); err != nil {
					b.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// matchCase pairs a template, an input, and whether the input is expected to
// match, so the no-match path is exercised alongside the happy paths.
type matchCase struct {
	name      string
	raw       string
	input     string
	wantMatch bool
}

var matchMatrix = []matchCase{
	{"Route3Vars", "https://{host}/users{/user}{/media}", "https://example.com/users/kevin/pics", true},
	{"ListExplode", "{/count*}", "/one/two/three", true},
	{"QueryKV", "https://example.com/foo{?bar}", "https://example.com/foo?bar=baz", true},
	{"NoMatchLiteral", "https://example.com/foo{?bar}", "https://example.com/foobaz", false},
	{"EncodedCapture", "https://{host}/q{/term}", "https://example.com/q/Hello%20World%21", true},
	{"Reserved", "{+path}", "/foo/bar/baz", true},
}

func BenchmarkMatchMatrix(b *testing.B) {
	for _, c := range matchMatrix {
		tmpl := MustNew(c.raw)
		// Validate expectation once outside the timed loop so a broken case
		// fails loudly instead of silently benchmarking the wrong path.
		if got := tmpl.Match(c.input) != nil; got != c.wantMatch {
			b.Fatalf("%s: Match(%q)!=nil = %v, want %v", c.name, c.input, got, c.wantMatch)
		}
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = tmpl.Match(c.input)
			}
		})
	}
}

// compileMatrix covers parse/compile cost from a tiny route to a dense RFC-style
// template, since New is the setup path for every other benchmark.
var compileMatrix = []expandCase{
	{"Small", "{var}"},
	{"Route", "https://{host}/users{/user}{/media}"},
	{"RFCDense", "{+path}/{var}{?list*,keys*}{#hello}"},
}

func BenchmarkCompile(b *testing.B) {
	for _, c := range compileMatrix {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := New(c.raw); err != nil {
					b.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}
