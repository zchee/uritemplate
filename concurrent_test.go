// Copyright 2026 The uritemplate Authors. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"sync"
	"testing"
)

// BenchmarkMatchConcurrent measures Match throughput under contention. Because
// the match program is published via a sync.Once and read lock-free thereafter,
// concurrent matches on one Template run in parallel rather than serializing on a
// mutex.
func BenchmarkMatchConcurrent(b *testing.B) {
	tmpl := MustNew("https://{host}/users{/user}{/media}")
	const input = "https://example.com/users/kevin/pics"
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = tmpl.Match(input)
		}
	})
}

// TestConcurrentAccessRaceFree drives Match, Regexp, and Varnames concurrently on
// a single Template to prove the lock-free hot paths and the sync.Once-guarded
// lazy builds are race-free under -race. Many goroutines may race to be the first
// caller of each accessor; all must observe the same published value.
func TestConcurrentAccessRaceFree(t *testing.T) {
	tmpl := MustNew("https://{host}/users{/user}{/media}")
	const input = "https://example.com/users/kevin/pics"

	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				if got := tmpl.Match(input); got == nil {
					t.Errorf("Match returned nil for a matching input")
					return
				}
				_ = tmpl.Regexp()
				_ = tmpl.Varnames()
			}
		}()
	}
	wg.Wait()
}
