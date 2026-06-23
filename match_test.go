// Copyright (C) 2016 Kohei YOSHIDA. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func ExampleTemplate_Match() {
	tmpl := MustNew("https://example.com/dictionary/{term:1}/{term}")
	match := tmpl.Match("https://example.com/dictionary/c/cat")
	if match == nil {
		fmt.Println("not matched")
		return
	}

	fmt.Printf("term:1 is %q\n", match.Get("term:1").String())
	fmt.Printf("term is %q\n", match.Get("term").String())

	// Output:
	// term:1 is "c"
	// term is "cat"
}

func TestTemplate_Match(t *testing.T) {
	for i, c := range testTemplateCases {
		if c.failMatch {
			continue
		}

		tmpl, err := New(c.raw)
		if err != nil {
			t.Errorf("unexpected error on %q: %#v", c.raw, err)
			continue
		}

		match := tmpl.Match(c.expected)
		if match == nil {
			t.Errorf("%d: failed to match %q against %q", i, c.raw, c.expected)
			t.Logf("template: %q", tmpl.Raw())
			continue
		}

		for name, actual := range match {
			var expected Value
			if semi := strings.Index(name, ":"); semi >= 0 {
				maxlen, _ := strconv.Atoi(name[semi+1:])
				name = name[:semi]
				expected = testExpressionExpandVarMap[name]

				if expected.T != ValueTypeString {
					t.Errorf("%d: failed to match %q against %q", i, c.raw, c.expected)
					t.Errorf("%d: expected %#v, but got %#v", i, expected, actual)
					continue
				}
				if v := expected.V[0]; len(v) > maxlen {
					expected.V = []string{v[:maxlen]}
				}
			} else {
				expected = testExpressionExpandVarMap[name]
			}

			if actual.T != expected.T {
				t.Errorf("%d: failed to match %q against %q", i, c.raw, c.expected)
				t.Errorf("%d: expected %#v, but got %#v", i, expected, actual)
			} else if le, la := len(expected.V), len(actual.V); le == la {
				for i := range actual.V {
					if actual.V[i] != expected.V[i] {
						t.Errorf("%d: failed to match %q against %q", i, c.raw, c.expected)
						t.Errorf("%d: expected %#v, but got %#v", i, expected, actual)
						break
					}
				}
			} else if le != 0 || la != 1 || actual.V[0] != "" { // not undef
				t.Errorf("%d: failed to match %q against %q", i, c.raw, c.expected)
				t.Errorf("%d: expected %#v, but got %#v", i, expected, actual)
			}
		}
	}
}

func TestTemplateMatch_RouteFastPathCompatibility(t *testing.T) {
	tmpl := MustNew("https://{host}/users{/user}{/media}")
	got := tmpl.Match("https://example.com/users/kevin/pics")
	if got == nil {
		t.Fatal("Match returned nil")
	}
	for name, want := range map[string]string{
		"host":  "example.com",
		"user":  "kevin",
		"media": "pics",
	} {
		if got.Get(name).String() != want {
			t.Fatalf("capture %q = %q, want %q", name, got.Get(name).String(), want)
		}
	}
}

func TestTemplateMatch_RouteFastPathFallbackList(t *testing.T) {
	tmpl := MustNew("{count}")
	got := tmpl.Match("one,two,three")
	if got == nil {
		t.Fatal("Match returned nil")
	}
	want := []string{"one", "two", "three"}
	if !reflect.DeepEqual(got.Get("count").List(), want) {
		t.Fatalf("count = %#v, want %#v", got.Get("count").List(), want)
	}
}

func TestTemplateMatch_ReturnedValueSlicesDoNotOverlapOnAppend(t *testing.T) {
	tmpl := MustNew("https://{host}/users{/user}{/media}")
	got := tmpl.Match("https://example.com/users/kevin/pics")
	if got == nil {
		t.Fatal("Match returned nil")
	}

	host := got.Get("host")
	host.V = append(host.V, "mutated")
	if got.Get("user").String() != "kevin" {
		t.Fatalf("append to host value overlapped user capture: user=%#v", got.Get("user").V)
	}
	if got.Get("media").String() != "pics" {
		t.Fatalf("append to host value overlapped media capture: media=%#v", got.Get("media").V)
	}
}

func TestTemplate_NotMatch(t *testing.T) {
	tmpl := MustNew("https://example.com/foo{?bar}")
	match := tmpl.Match("https://example.com/foobaz")
	if match != nil {
		t.Errorf("must not match")
	}
}
