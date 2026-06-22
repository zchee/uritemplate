// Copyright 2026 The uritemplate Authors. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import "unsafe"

// acc is a minimal append-based byte accumulator used by the Expand path. Unlike
// strings.Builder it carries no copy-check pointer, so when callers take its
// address (*acc) it stays on the stack and the only heap allocation is its
// backing array — which is then handed to the caller as the result string with
// no extra copy. It is not safe for concurrent use and is never retained beyond
// a single Expand call.
type acc struct {
	b []byte
}

func (w *acc) writeString(s string) {
	w.b = append(w.b, s...)
}

func (w *acc) writeByte(c byte) {
	w.b = append(w.b, c)
}

// String returns the accumulated bytes as a string without copying. The backing
// array must not be mutated afterward; acc is discarded after this call, so the
// returned string is the sole owner.
func (w *acc) String() string {
	if len(w.b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(w.b), len(w.b))
}
