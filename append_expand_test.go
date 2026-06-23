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

// TestAppendExpandMatchesExpand proves AppendExpand produces byte-identical
// output to Expand across the whole RFC corpus, for both a nil and a prefilled
// dst. AppendExpand is the lower-allocation sibling of Expand; if they ever
// diverge, the additive API would silently mis-expand.
func TestAppendExpandMatchesExpand(t *testing.T) {
	for _, c := range testTemplateCases {
		tmpl, err := New(c.raw)
		if err != nil {
			t.Errorf("unexpected error on %q: %#v", c.raw, err)
			continue
		}

		want, werr := tmpl.Expand(testExpressionExpandVarMap)

		// nil dst: the returned slice is exactly the expansion.
		got, gerr := tmpl.AppendExpand(nil, testExpressionExpandVarMap)
		if (werr == nil) != (gerr == nil) {
			t.Errorf("%q: Expand err=%v but AppendExpand err=%v", c.raw, werr, gerr)
		}
		if string(got) != want {
			t.Errorf("%q: AppendExpand(nil) = %q, want %q (== Expand)", c.raw, got, want)
		}

		// non-nil dst: AppendExpand must append after the existing prefix.
		const prefix = "PREFIX/"
		got2, _ := tmpl.AppendExpand([]byte(prefix), testExpressionExpandVarMap)
		if string(got2) != prefix+want {
			t.Errorf("%q: AppendExpand(prefix) = %q, want %q", c.raw, got2, prefix+want)
		}
	}
}

// TestAppendExpandReusesBuffer verifies a caller can reuse one buffer across
// many expansions: after resetting length to zero the next call writes in place,
// and the result is still correct. This is the buffer-reuse pattern the API
// exists to enable.
func TestAppendExpandReusesBuffer(t *testing.T) {
	tmpl := MustNew("https://example.com/dictionary/{term}")
	vars := Values{"term": String("cat")}
	want := "https://example.com/dictionary/cat"

	buf := make([]byte, 0, 256)
	for i := range 3 {
		var err error
		buf, err = tmpl.AppendExpand(buf[:0], vars)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error %v", i, err)
		}
		if string(buf) != want {
			t.Fatalf("iteration %d: got %q, want %q", i, buf, want)
		}
	}
}

// TestAppendExpandPreallocatedZeroAlloc proves that with a sufficiently large
// preallocated dst, AppendExpand performs no allocations — the property that
// distinguishes it from Expand (which must allocate its result string).
func TestAppendExpandPreallocatedZeroAlloc(t *testing.T) {
	tmpl := MustNew("https://example.com/users{/user}{/media}")
	vars := Values{"user": String("kevin"), "media": String("pics")}
	buf := make([]byte, 0, 256)

	allocs := testing.AllocsPerRun(100, func() {
		var err error
		buf, err = tmpl.AppendExpand(buf[:0], vars)
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
	})
	if allocs != 0 {
		t.Errorf("AppendExpand with preallocated dst = %v allocs/op, want 0", allocs)
	}
}

// TestAppendExpandGrowsInsufficientBuffer verifies that a dst with capacity too
// small for the output is grown (not overflowed), and the prefix is preserved.
func TestAppendExpandGrowsInsufficientBuffer(t *testing.T) {
	tmpl := MustNew("{long}")
	vars := Values{"long": String(strings.Repeat("x", 500))}
	want := strings.Repeat("x", 500)

	// dst has a prefix but no spare capacity for the 500-byte value.
	dst := []byte("p")
	got, err := tmpl.AppendExpand(dst, vars)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if string(got) != "p"+want {
		t.Errorf("AppendExpand grew incorrectly: got %q…, want prefix 'p' + 500 'x'", got[:min(len(got), 8)])
	}
}
