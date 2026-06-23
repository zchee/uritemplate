// Copyright (C) 2016 Kohei YOSHIDA. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"regexp"
	"strings"
	"sync"
)

// Template represents a URI Template.
//
// The derived forms — the match program, the variable-name list, and the regexp
// — are each built at most once, lazily, on first use and published via a
// sync.Once. After the first call every accessor is a lock-free read, so Match,
// Varnames, and Regexp are safe for concurrent use with no mutex contention.
// Building lazily keeps New cheap for templates that are parsed but, say, only
// expanded and never matched.
type Template struct {
	raw   string
	exprs []template

	progOnce sync.Once
	prog     *prog

	varnamesOnce sync.Once
	varnames     []string

	reOnce sync.Once
	re     *regexp.Regexp
}

// New parses and constructs a new Template instance based on the template.
// New returns an error if the template cannot be recognized.
func New(template string) (*Template, error) {
	return (&parser{r: template}).parseURITemplate()
}

// MustNew panics if the template cannot be recognized.
func MustNew(template string) *Template {
	ret, err := New(template)
	if err != nil {
		panic(err)
	}
	return ret
}

// Raw returns a raw URI template passed to New in string.
func (t *Template) Raw() string {
	return t.raw
}

// Varnames returns variable names used in the template.
//
// The slice is computed at most once via varnamesOnce; after the first call this
// is a lock-free read safe for concurrent callers. The returned slice is owned by
// the Template; callers must not mutate it.
func (t *Template) Varnames() []string {
	t.varnamesOnce.Do(func() {
		t.varnames = buildVarnames(t.exprs)
	})
	return t.varnames
}

// buildVarnames collects the distinct variable names across all expressions, in
// first-seen order. It runs once during parsing so Varnames needs no lock.
func buildVarnames(exprs []template) []string {
	reg := map[string]struct{}{}
	varnames := []string{}
	for i := range exprs {
		expr, ok := exprs[i].(*expression)
		if !ok {
			continue
		}
		for _, spec := range expr.vars {
			if _, ok := reg[spec.name]; ok {
				continue
			}
			reg[spec.name] = struct{}{}
			varnames = append(varnames, spec.name)
		}
	}
	return varnames
}

// Expand returns a URI reference corresponding to the template expanded using the passed variables.
func (t *Template) Expand(vars Values) (string, error) {
	w := acc{b: make([]byte, 0, t.expandSize(vars))}
	err := t.expandInto(&w, vars)
	return w.String(), err
}

// AppendExpand expands the template into dst and returns the extended buffer,
// like the append built-in. When dst has enough capacity the expansion writes in
// place with no allocation, so callers that reuse a buffer across many
// expansions amortize away the per-call result allocation that Expand must make
// to return a string.
//
// The returned slice may share dst's backing array; treat dst as consumed and
// use only the returned slice afterward. On error the buffer still contains the
// output produced before the error, mirroring Expand. A nil dst is valid and
// behaves like appending to an empty buffer.
func (t *Template) AppendExpand(dst []byte, vars Values) ([]byte, error) {
	size := t.expandSize(vars)
	if cap(dst)-len(dst) < size {
		grown := make([]byte, len(dst), len(dst)+size)
		copy(grown, dst)
		dst = grown
	}
	w := acc{b: dst}
	err := t.expandInto(&w, vars)
	return w.b, err
}

// expandInto runs the expansion, writing the result through w. It is the shared
// core of Expand and AppendExpand; the two differ only in how they source and
// return the underlying buffer.
func (t *Template) expandInto(w *acc, vars Values) error {
	for i := range t.exprs {
		var err error
		switch expr := t.exprs[i].(type) {
		case literals:
			err = expr.expand(w, vars)
		case *expression:
			err = expr.expand(w, vars)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// expandSize estimates the byte length of Expand's output so the strings.Builder
// can be sized once instead of growing incrementally. It is a heuristic: literal
// runs contribute their exact length and each defined variable value contributes
// its raw byte length (percent-encoding may expand it, but a low estimate only
// costs a later regrowth, never correctness).
func (t *Template) expandSize(vars Values) int {
	n := len(t.raw)
	for i := range t.exprs {
		expr, ok := t.exprs[i].(*expression)
		if !ok {
			continue
		}
		for _, spec := range expr.vars {
			v := vars.Get(spec.name)
			if !v.Valid() {
				continue
			}
			for _, s := range v.V {
				n += len(s)
			}
		}
	}
	return n
}

// Regexp converts the template to regexp and returns compiled *regexp.Regexp.
//
// Compilation is performed at most once, lazily, and published via reOnce; after
// the first call this is a lock-free read of the cached *regexp.Regexp.
func (t *Template) Regexp() *regexp.Regexp {
	t.reOnce.Do(func() {
		var b strings.Builder
		b.WriteByte('^')
		for _, expr := range t.exprs {
			expr.regexp(&b)
		}
		b.WriteByte('$')
		t.re = regexp.MustCompile(b.String())
	})
	return t.re
}
