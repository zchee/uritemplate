// Copyright (C) 2016 Kohei YOSHIDA. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"regexp"
	"strconv"
	"strings"
)

type template interface {
	expand(*acc, Values) error
	regexp(*strings.Builder)
}

type literals string

func (l literals) expand(b *acc, _ Values) error {
	b.writeString(string(l))
	return nil
}

func (l literals) regexp(b *strings.Builder) {
	b.WriteString("(?:")
	b.WriteString(regexp.QuoteMeta(string(l)))
	b.WriteByte(')')
}

type varspec struct {
	name    string
	maxlen  int
	explode bool
}

type expression struct {
	vars  []varspec
	op    parseOp
	first string
	sep   string
	named bool
	ifemp string
	allow runeClass
}

func (e *expression) init() {
	switch e.op {
	case parseOpSimple:
		e.sep = ","
		e.allow = runeClassU
	case parseOpPlus:
		e.sep = ","
		e.allow = runeClassUR
	case parseOpCrosshatch:
		e.first = "#"
		e.sep = ","
		e.allow = runeClassUR
	case parseOpDot:
		e.first = "."
		e.sep = "."
		e.allow = runeClassU
	case parseOpSlash:
		e.first = "/"
		e.sep = "/"
		e.allow = runeClassU
	case parseOpSemicolon:
		e.first = ";"
		e.sep = ";"
		e.named = true
		e.allow = runeClassU
	case parseOpQuestion:
		e.first = "?"
		e.sep = "&"
		e.named = true
		e.ifemp = "="
		e.allow = runeClassU
	case parseOpAmpersand:
		e.first = "&"
		e.sep = "&"
		e.named = true
		e.ifemp = "="
		e.allow = runeClassU
	}
}

// escapeValue percent-encodes v into w according to this expression's allowed
// character class. Dispatch is static (a method, not a function-pointer field),
// which lets escape analysis keep the Expand accumulator on the stack.
func (e *expression) escapeValue(w *acc, v string) error {
	if e.allow&runeClassR == runeClassR {
		return escapeExceptUR(w, v)
	}
	return escapeExceptU(w, v)
}

func (e *expression) expand(w *acc, values Values) error {
	first := true
	for _, varspec := range e.vars {
		value := values.Get(varspec.name)
		if !value.Valid() {
			continue
		}

		if first {
			w.writeString(e.first)
			first = false
		} else {
			w.writeString(e.sep)
		}

		if err := value.expand(w, varspec, e); err != nil {
			return err
		}

	}
	return nil
}

func (e *expression) regexp(b *strings.Builder) {
	if e.first != "" {
		b.WriteString("(?:") // $1
		b.WriteString(regexp.QuoteMeta(e.first))
	}
	b.WriteByte('(') // $2
	runeClassToRegexp(b, e.allow, e.named || e.vars[0].explode)
	if len(e.vars) > 1 || e.vars[0].explode {
		max := len(e.vars) - 1
		for i := 0; i < len(e.vars); i++ {
			if e.vars[i].explode {
				max = -1
				break
			}
		}

		b.WriteString("(?:") // $3
		b.WriteString(regexp.QuoteMeta(e.sep))
		runeClassToRegexp(b, e.allow, e.named || max < 0)
		b.WriteByte(')') // $3
		if max > 0 {
			b.WriteString("{0,")
			b.WriteString(strconv.Itoa(max))
			b.WriteByte('}')
		} else {
			b.WriteByte('*')
		}
	}
	b.WriteByte(')') // $2
	if e.first != "" {
		b.WriteByte(')') // $1
	}
	b.WriteByte('?')
}

func runeClassToRegexp(b *strings.Builder, class runeClass, named bool) {
	b.WriteString("(?:(?:[")
	if class&runeClassR == 0 {
		b.WriteString(`\x2c`)
		if named {
			b.WriteString(`\x3d`)
		}
	}
	if class&runeClassU == runeClassU {
		b.WriteString(reUnreserved)
	}
	if class&runeClassR == runeClassR {
		b.WriteString(reReserved)
	}
	b.WriteString("]")
	b.WriteString("|%[[:xdigit:]][[:xdigit:]]")
	b.WriteString(")*)")
}
