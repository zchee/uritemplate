// Copyright (C) 2016 Kohei YOSHIDA. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"unicode"
	"unicode/utf8"
	"unsafe"
)

var (
	hex = []byte("0123456789ABCDEF")
	// reserved   = gen-delims / sub-delims
	// gen-delims =  ":" / "/" / "?" / "#" / "[" / "]" / "@"
	// sub-delims =  "!" / "$" / "&" / "’" / "(" / ")"
	//            /  "*" / "+" / "," / ";" / "="
	rangeReserved = &unicode.RangeTable{
		R16: []unicode.Range16{
			{Lo: 0x21, Hi: 0x21, Stride: 1}, // '!'
			{Lo: 0x23, Hi: 0x24, Stride: 1}, // '#' - '$'
			{Lo: 0x26, Hi: 0x2C, Stride: 1}, // '&' - ','
			{Lo: 0x2F, Hi: 0x2F, Stride: 1}, // '/'
			{Lo: 0x3A, Hi: 0x3B, Stride: 1}, // ':' - ';'
			{Lo: 0x3D, Hi: 0x3D, Stride: 1}, // '='
			{Lo: 0x3F, Hi: 0x40, Stride: 1}, // '?' - '@'
			{Lo: 0x5B, Hi: 0x5B, Stride: 1}, // '['
			{Lo: 0x5D, Hi: 0x5D, Stride: 1}, // ']'
		},
		LatinOffset: 9,
	}
	reReserved = `\x21\x23\x24\x26-\x2c\x2f\x3a\x3b\x3d\x3f\x40\x5b\x5d`
	// ALPHA      = %x41-5A / %x61-7A
	// DIGIT      = %x30-39
	// unreserved = ALPHA / DIGIT / "-" / "." / "_" / "~"
	rangeUnreserved = &unicode.RangeTable{
		R16: []unicode.Range16{
			{Lo: 0x2D, Hi: 0x2E, Stride: 1}, // '-' - '.'
			{Lo: 0x30, Hi: 0x39, Stride: 1}, // '0' - '9'
			{Lo: 0x41, Hi: 0x5A, Stride: 1}, // 'A' - 'Z'
			{Lo: 0x5F, Hi: 0x5F, Stride: 1}, // '_'
			{Lo: 0x61, Hi: 0x7A, Stride: 1}, // 'a' - 'z'
			{Lo: 0x7E, Hi: 0x7E, Stride: 1}, // '~'
		},
	}
	reUnreserved = `\x2d\x2e\x30-\x39\x41-\x5a\x5f\x61-\x7a\x7e`
)

// tblUnreserved and tblUnreservedReserved are O(1) ASCII membership tables for
// the unreserved and unreserved+reserved character sets. They are exact (not an
// approximation) because rangeUnreserved and rangeReserved contain only ASCII
// code points, so any byte >= utf8.RuneSelf is necessarily outside both sets and
// is percent-encoded. They are derived from the authoritative RangeTables at
// init so the two representations can never drift.
var (
	tblUnreserved         = asciiTable(rangeUnreserved)
	tblUnreservedReserved = asciiTable(rangeUnreserved, rangeReserved)
)

func asciiTable(tables ...*unicode.RangeTable) [128]bool {
	var t [128]bool
	for c := range rune(128) {
		t[c] = unicode.In(c, tables...)
	}
	return t
}

type runeClass uint8

const (
	runeClassU runeClass = 1 << iota
	runeClassR

	runeClassUR = runeClassU | runeClassR
)

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func ishex(c byte) bool {
	switch {
	case '0' <= c && c <= '9':
		return true
	case 'a' <= c && c <= 'f':
		return true
	case 'A' <= c && c <= 'F':
		return true
	default:
		return false
	}
}

func pctDecode(s string) string {
	size := len(s)
	for i := 0; i < len(s); {
		switch s[i] {
		case '%':
			size -= 2
			i += 3
		default:
			i++
		}
	}
	if size == len(s) {
		return s
	}

	buf := make([]byte, size)
	j := 0
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '%':
			buf[j] = unhex(s[i+1])<<4 | unhex(s[i+2])
			i += 3
			j++
		default:
			buf[j] = c
			i++
			j++
		}
	}
	if len(buf) == 0 {
		return ""
	}
	// buf is freshly allocated above, fully written, never mutated afterward and
	// never pooled, so it is safe to alias as a string without copying.
	return unsafe.String(unsafe.SliceData(buf), len(buf))
}

func escapeLiteral(w *acc, v string) error {
	w.writeString(v)
	return nil
}

func escapeExceptU(w *acc, v string) error {
	return escape(w, v, &tblUnreserved)
}

func escapeExceptUR(w *acc, v string) error {
	// TODO(yosida95): is pct-encoded triplets allowed here?
	return escape(w, v, &tblUnreservedReserved)
}

// escape writes v to w, percent-encoding every byte that is not allowed by tbl.
// Allowed bytes (an ASCII subset) are written verbatim; every other byte —
// including each byte of a multi-byte UTF-8 sequence — is emitted as "%XX",
// which yields correct UTF-8 percent-encoding per RFC 6570. It returns an error
// if v contains an invalid UTF-8 sequence.
func escape(w *acc, v string, tbl *[128]bool) error {
	for i := 0; i < len(v); {
		if c := v[i]; c < utf8.RuneSelf {
			if tbl[c] {
				w.writeByte(c)
			} else {
				w.writeByte('%')
				w.writeByte(hex[c>>4])
				w.writeByte(hex[c&0x0f])
			}
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(v[i:])
		if size == 1 {
			// utf8.RuneError with width 1 means an invalid encoding.
			return errorf(i, "invalid encoding")
		}
		for j := range size {
			b := v[i+j]
			w.writeByte('%')
			w.writeByte(hex[b>>4])
			w.writeByte(hex[b&0x0f])
		}
		i += size
	}
	return nil
}
