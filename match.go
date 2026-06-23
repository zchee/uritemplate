// Copyright (C) 2016 Kohei YOSHIDA. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

// Match reports whether expansion is a possible expansion of the template and,
// if so, returns the variable values that produce it. It returns nil when the
// expansion does not match.
//
// Matching is performed by a bespoke recursive-descent scanner (scanner.go) that
// inverts the expansion grammar directly. The per-template segment matchers are
// built once and cached on the Template.
func (tmpl *Template) Match(expansion string) Values {
	// The match program is built at most once via progOnce and is immutable
	// thereafter. After the first call this is an atomic done-flag check plus a
	// lock-free read, so concurrent matches run fully in parallel with no mutex
	// contention.
	tmpl.progOnce.Do(func() {
		tmpl.prog = buildProg(tmpl.exprs)
	})
	return match(tmpl.prog, expansion)
}
