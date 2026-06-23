// Copyright 2026 The uritemplate Authors. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"reflect"
	"testing"
)

// generalMatch runs only the bespoke scanner, bypassing the route fast path, so
// a test can compare the two engines directly. It mirrors match() with the
// p.route shortcut removed.
func generalMatch(p *prog, expansion string) Values {
	s := scanner{input: expansion, prog: p}
	var inlineCaps [4]captureList
	if len(p.keys) <= len(inlineCaps) {
		s.caps.init(inlineCaps[:len(p.keys)])
	} else {
		s.caps.init(make([]captureList, len(p.keys)))
	}
	if !s.matchFrom(0, 0) {
		return nil
	}
	return materializeMatch(p.keys, &s.caps, expansion)
}

// TestRouteFastPathMatchesGeneralScanner is the differential gate for the route
// fast path: for every route-eligible template, the route fast path must agree
// with the general scanner on every input — both on whether it matches and on the
// exact captures.
//
// This is the engine-independence check the plan's pre-mortem calls for: the
// route fast path is a from-scratch recursive matcher, and its risk is silently
// recapturing or rejecting cases the battle-tested scanner handles. Inputs are
// taken from the RFC corpus (the canonical expansions) plus a set of adversarial
// strings that probe boundaries, separators, and percent-encoding.
func TestRouteFastPathMatchesGeneralScanner(t *testing.T) {
	probes := []string{
		"",
		"x",
		"a/b/c",
		"https://example.com/users/kevin/pics",
		"https://example.com/users//pics",
		"one,two,three",
		"/one/two/three",
		"Hello%20World%21",
		"a%2Fb",
		"trailing/",
		"/leading",
		"50%25",
		"日本",
	}

	for _, c := range testTemplateCases {
		tmpl, err := New(c.raw)
		if err != nil {
			continue
		}
		prog := buildProg(tmpl.exprs)
		if prog.route == nil {
			continue // not route-eligible; general scanner is the only path
		}

		inputs := append([]string{c.expected}, probes...)
		for _, in := range inputs {
			route, handled := matchRoute(prog.route, prog.keys, in)
			if !handled {
				// Route path deferred to the general scanner; nothing to compare.
				continue
			}
			general := generalMatch(prog, in)

			routeNil := route == nil
			generalNil := general == nil
			if routeNil != generalNil {
				t.Errorf("%q on %q: route match=%v but general match=%v",
					c.raw, in, !routeNil, !generalNil)
				continue
			}
			if !routeNil && !reflect.DeepEqual(route, general) {
				t.Errorf("%q on %q: route captures %#v != general captures %#v",
					c.raw, in, route, general)
			}
		}
	}
}
