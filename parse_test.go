// Copyright 2026 The uritemplate Authors. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"testing"
	"unicode"
)

// TestParserASCIITablesMatchRangeTables proves the [128]bool fast-path tables
// used by isVarchar/isLiteral are an exact mirror of the unicode range tables
// over the ASCII range. If they ever diverge, the parser's fast path would
// accept or reject a character the slow path would not — a silent grammar change.
func TestParserASCIITablesMatchRangeTables(t *testing.T) {
	for c := range rune(128) {
		if got, want := tblVarchar[c], unicode.Is(rangeVarchar, c); got != want {
			t.Errorf("tblVarchar[%#x] = %v, want %v", c, got, want)
		}
		if got, want := tblLiterals[c], unicode.Is(rangeLiterals, c); got != want {
			t.Errorf("tblLiterals[%#x] = %v, want %v", c, got, want)
		}
	}
}

// TestParserFastPathAgreesWithUnicode checks the validation helpers agree with
// unicode.Is across the whole ASCII range and a sample of non-ASCII runes that
// exercise the fallback path (so the r < utf8.RuneSelf branch and the unicode.Is
// branch are both covered).
func TestParserFastPathAgreesWithUnicode(t *testing.T) {
	probes := []rune{
		0, '{', '}', ' ', '%', '.', '_', '~', 'a', 'Z', '9', 0x7f,
		0x00A0, 0xD7FF, 0xE000, '日', '本', 0x1F600 /* emoji, outside literals */, 0x10FFFD,
	}
	for _, r := range probes {
		if got, want := isVarchar(r), unicode.Is(rangeVarchar, r); got != want {
			t.Errorf("isVarchar(%#x) = %v, want %v", r, got, want)
		}
		if got, want := isLiteral(r), unicode.Is(rangeLiterals, r); got != want {
			t.Errorf("isLiteral(%#x) = %v, want %v", r, got, want)
		}
	}
}

// TestParseExprCapacityNoOverAllocation guards the exprs pre-sizing: parsing must
// still succeed for templates with varying expression counts, including none.
func TestParseExprCapacityNoOverAllocation(t *testing.T) {
	tests := map[string]struct {
		raw       string
		wantExprs int
	}{
		"no expression":           {raw: "https://example.com/", wantExprs: 1},
		"one expression":          {raw: "{var}", wantExprs: 1},
		"literal then expression": {raw: "x{var}", wantExprs: 2},
		"three expressions":       {raw: "{a}{b}{c}", wantExprs: 3},
		"empty template":          {raw: "", wantExprs: 0},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tmpl, err := New(tt.raw)
			if err != nil {
				t.Fatalf("New(%q) unexpected error: %v", tt.raw, err)
			}
			if got := len(tmpl.exprs); got != tt.wantExprs {
				t.Errorf("len(exprs) = %d, want %d", got, tt.wantExprs)
			}
		})
	}
}
