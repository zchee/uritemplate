// Copyright (C) 2016 Kohei YOSHIDA. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

// A varname containing pct-encoded characters is not the same variable as
// a varname with those same characters decoded.
//
// -- https://tools.ietf.org/html/rfc6570#section-2.3
type Values map[string]Value

func (v Values) Set(name string, value Value) {
	v[name] = value
}

func (v Values) Get(name string) Value {
	if v == nil {
		return Value{}
	}
	return v[name]
}

type ValueType uint8

const (
	ValueTypeString = iota
	ValueTypeList
	ValueTypeKV
	valueTypeLast
)

var valueTypeNames = []string{
	"String",
	"List",
	"KV",
}

func (vt ValueType) String() string {
	if vt < valueTypeLast {
		return valueTypeNames[vt]
	}
	return ""
}

type Value struct {
	T ValueType
	V []string
}

func (v Value) String() string {
	if v.Valid() && v.T == ValueTypeString {
		return v.V[0]
	}
	return ""
}

func (v Value) List() []string {
	if v.Valid() && v.T == ValueTypeList {
		return v.V
	}
	return nil
}

func (v Value) KV() []string {
	if v.Valid() && v.T == ValueTypeKV {
		return v.V
	}
	return nil
}

func (v Value) Valid() bool {
	switch v.T {
	default:
		return false
	case ValueTypeString:
		return len(v.V) > 0
	case ValueTypeList:
		return len(v.V) > 0
	case ValueTypeKV:
		return len(v.V) > 0 && len(v.V)%2 == 0
	}
}

func (v Value) expand(w *acc, spec varspec, exp *expression) error {
	switch v.T {
	case ValueTypeString:
		val := v.V[0]
		var maxlen int
		if max := len(val); spec.maxlen < 1 || spec.maxlen > max {
			maxlen = max
		} else {
			maxlen = spec.maxlen
		}

		if exp.named {
			w.writeString(spec.name)
			if val == "" {
				w.writeString(exp.ifemp)
				return nil
			}
			w.writeByte('=')
		}
		return exp.escapeValue(w, val[:maxlen])
	case ValueTypeList:
		var sep string
		if spec.explode {
			sep = exp.sep
		} else {
			sep = ","
		}

		var pre string
		var preifemp string
		if spec.explode && exp.named {
			pre = spec.name + "="
			preifemp = spec.name + exp.ifemp
		}

		if !spec.explode && exp.named {
			w.writeString(spec.name)
			w.writeByte('=')
		}
		for i := range v.V {
			val := v.V[i]
			if i > 0 {
				w.writeString(sep)
			}
			if val == "" {
				w.writeString(preifemp)
				continue
			}
			w.writeString(pre)

			if err := exp.escapeValue(w, val); err != nil {
				return err
			}
		}
	case ValueTypeKV:
		var sep string
		var kvsep string
		if spec.explode {
			sep = exp.sep
			kvsep = "="
		} else {
			sep = ","
			kvsep = ","
		}

		var ifemp string
		var kLiteral bool
		if spec.explode && exp.named {
			ifemp = exp.ifemp
			kLiteral = true
		} else {
			ifemp = ","
		}

		if !spec.explode && exp.named {
			w.writeString(spec.name)
			w.writeByte('=')
		}

		for i := 0; i < len(v.V); i += 2 {
			if i > 0 {
				w.writeString(sep)
			}
			if kLiteral {
				if err := escapeLiteral(w, v.V[i]); err != nil {
					return err
				}
			} else if err := exp.escapeValue(w, v.V[i]); err != nil {
				return err
			}
			if v.V[i+1] == "" {
				w.writeString(ifemp)
				continue
			}
			w.writeString(kvsep)

			if err := exp.escapeValue(w, v.V[i+1]); err != nil {
				return err
			}
		}
	}
	return nil
}

// String returns Value that represents string.
func String(v string) Value {
	return Value{
		T: ValueTypeString,
		V: []string{v},
	}
}

// List returns Value that represents list.
func List(v ...string) Value {
	return Value{
		T: ValueTypeList,
		V: v,
	}
}

// KV returns Value that represents associative list.
// KV panics if len(kv) is not even.
func KV(kv ...string) Value {
	if len(kv)%2 != 0 {
		panic("uritemplate.go: count of the kv must be even number")
	}
	return Value{
		T: ValueTypeKV,
		V: kv,
	}
}
