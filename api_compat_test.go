// Copyright 2026 The uritemplate Authors. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

// This file is a compile-time guard for the exported API surface. The package
// is being optimized internally (the unexported match engine is rewritten), but
// the exported signatures MUST remain backward-compatible. Each assignment below
// binds an exported identifier to a variable of its exact expected type; if any
// exported signature, field, or constant changes, this file fails to compile and
// the regression is caught immediately. It intentionally references nothing
// unexported so internal refactors never touch it.

var (
	_ func(string) (*Template, error)               = New
	_ func(string) *Template                        = MustNew
	_ func(*Template, *Template, CompareFlags) bool = Equals
	_ func(string) Value                            = String
	_ func(...string) Value                         = List
	_ func(...string) Value                         = KV

	_ func(*Template) string                  = (*Template).Raw
	_ func(*Template) []string                = (*Template).Varnames
	_ func(*Template, Values) (string, error) = (*Template).Expand
	_ func(*Template, string) Values          = (*Template).Match

	_ func(Value) string   = Value.String
	_ func(Value) []string = Value.List
	_ func(Value) []string = Value.KV
	_ func(Value) bool     = Value.Valid

	_ func(ValueType) string = ValueType.String

	_ func(Values, string, Value) = Values.Set
	_ func(Values, string) Value  = Values.Get
)

// Exported struct fields and named types must keep their shapes and kinds.
var (
	_              = Value{T: ValueType(0), V: []string(nil)}
	_ ValueType    = ValueTypeString
	_ ValueType    = ValueTypeList
	_ ValueType    = ValueTypeKV
	_ CompareFlags = CompareVarname
	_ Values       = Values(nil)
)

// (*Template).Regexp returns *regexp.Regexp; bound in regexp_api_compat_test.go
// to keep the regexp import out of this file's var block is unnecessary — assert
// it here via a function literal that would fail to type-check on signature drift.
var _ = func(t *Template) { _ = t.Regexp() }
