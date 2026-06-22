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
	tmpl.mu.Lock()
	if tmpl.prog == nil {
		tmpl.prog = buildProg(tmpl.exprs)
	}
	prog := tmpl.prog
	tmpl.mu.Unlock()

	return match(prog, expansion)
}
