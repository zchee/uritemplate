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

// TestASCIITablesMatchRangeTables proves the [128]bool fast-path tables are an
// exact substitute for the authoritative unicode.RangeTable membership checks
// across the whole ASCII range. If they ever diverge, the escape fast path would
// silently mis-encode, so this guards the optimization's core invariant.
func TestASCIITablesMatchRangeTables(t *testing.T) {
	for c := range rune(128) {
		if got, want := tblUnreserved[c], unicode.Is(rangeUnreserved, c); got != want {
			t.Errorf("tblUnreserved[%#x] = %v, want %v", c, got, want)
		}
		if got, want := tblUnreservedReserved[c], unicode.In(c, rangeUnreserved, rangeReserved); got != want {
			t.Errorf("tblUnreservedReserved[%#x] = %v, want %v", c, got, want)
		}
	}
}

// TestEscapeUTF8 verifies that non-ASCII values are percent-encoded by their
// UTF-8 bytes, per RFC 6570 (the prior implementation incorrectly encoded the
// rune's code-point bytes, e.g. "é" -> "%E9" instead of "%C3%A9").
func TestEscapeUTF8(t *testing.T) {
	tests := map[string]struct {
		in     string
		wantU  string // escapeExceptU (unreserved only)
		wantUR string // escapeExceptUR (unreserved + reserved)
	}{
		"ascii unreserved": {in: "abc-._~", wantU: "abc-._~", wantUR: "abc-._~"},
		"space and bang":   {in: "Hello World!", wantU: "Hello%20World%21", wantUR: "Hello%20World!"},
		"percent":          {in: "50%", wantU: "50%25", wantUR: "50%25"},
		"reserved slash":   {in: "a/b", wantU: "a%2Fb", wantUR: "a/b"},
		"latin1 e acute":   {in: "é", wantU: "%C3%A9", wantUR: "%C3%A9"},
		"cjk":              {in: "日本", wantU: "%E6%97%A5%E6%9C%AC", wantUR: "%E6%97%A5%E6%9C%AC"},
		"emoji 4 bytes":    {in: "🦀", wantU: "%F0%9F%A6%80", wantUR: "%F0%9F%A6%80"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var wu acc
			if err := escapeExceptU(&wu, tt.in); err != nil {
				t.Fatalf("escapeExceptU(%q): unexpected error %v", tt.in, err)
			}
			if got := wu.String(); got != tt.wantU {
				t.Errorf("escapeExceptU(%q) = %q, want %q", tt.in, got, tt.wantU)
			}
			var wur acc
			if err := escapeExceptUR(&wur, tt.in); err != nil {
				t.Fatalf("escapeExceptUR(%q): unexpected error %v", tt.in, err)
			}
			if got := wur.String(); got != tt.wantUR {
				t.Errorf("escapeExceptUR(%q) = %q, want %q", tt.in, got, tt.wantUR)
			}
		})
	}
}

// TestEscapeInvalidUTF8 verifies invalid UTF-8 input is rejected with an error
// (preserving the previous behavior).
func TestEscapeInvalidUTF8(t *testing.T) {
	tests := map[string]string{
		"lone continuation": "\x80",
		"truncated 2-byte":  "a\xc3",
		"invalid lead":      "\xff\xfe",
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			var w acc
			if err := escapeExceptU(&w, in); err == nil {
				t.Errorf("escapeExceptU(%q): want error, got nil (wrote %q)", in, w.String())
			}
		})
	}
}

// TestExpandUTF8 exercises the fix end-to-end through the public Expand API.
func TestExpandUTF8(t *testing.T) {
	tmpl := MustNew("{name}{+raw}")
	vars := Values{
		"name": String("José"),
		"raw":  String("/café"),
	}
	got, err := tmpl.Expand(vars)
	if err != nil {
		t.Fatalf("Expand: unexpected error %v", err)
	}
	// "José" under unreserved: J o s %C3%A9 ; "/café" under unreserved+reserved:
	// '/' allowed, c a f %C3%A9.
	const want = "Jos%C3%A9/caf%C3%A9"
	if got != want {
		t.Errorf("Expand = %q, want %q", got, want)
	}
}
