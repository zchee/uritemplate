// Copyright 2026 The uritemplate Authors. All rights reserved.
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of The BSD 3-Clause License
// that can be found in the LICENSE file.

package uritemplate

import (
	"reflect"
	"sort"
	"testing"
)

// nameVal pairs a capture variable name with its expected Value.
type nameVal struct {
	name string
	val  Value
}

// matchGoldenCases freezes the EXACT current Match() capture output across the
// full RFC 6570 corpus. It is the engine-independent contract: any matcher
// implementation (Pike VM, regexp, bespoke scanner) MUST reproduce these
// captures. These assertions did not exist before: TestTemplate_Match skips
// every failMatch case (match_test.go), so list/KV/multi-var capture values
// were previously untested. Some entries encode deliberately quirky behavior of
// the shared one-group-per-expression grammar (e.g. {.who,who} -> the first
// variable greedily consumes the span); they are frozen as-is, not endorsed as
// the only correct inverse.
var matchGoldenCases = []struct {
	raw     string
	exp     string
	want    []nameVal
	wantNil bool
}{
	{raw: "'{count}'", exp: "'one,two,three'", want: []nameVal{{"count", Value{T: ValueTypeList, V: []string{"one", "two", "three"}}}}},
	{raw: "{count}", exp: "one,two,three", want: []nameVal{{"count", Value{T: ValueTypeList, V: []string{"one", "two", "three"}}}}},
	{raw: "{count*}", exp: "one,two,three", want: []nameVal{{"count", Value{T: ValueTypeList, V: []string{"one", "two", "three"}}}}},
	{raw: "{/count}", exp: "/one,two,three", want: []nameVal{{"count", Value{T: ValueTypeList, V: []string{"one", "two", "three"}}}}},
	{raw: "{/count*}", exp: "/one/two/three", want: []nameVal{{"count", Value{T: ValueTypeList, V: []string{"one", "two", "three"}}}}},
	{raw: "{;count}", exp: ";count=one,two,three", want: []nameVal{{"count", Value{T: ValueTypeList, V: []string{"one", "two", "three"}}}}},
	{raw: "{;count*}", exp: ";count=one;count=two;count=three", want: []nameVal{{"count", Value{T: ValueTypeList, V: []string{"one", "two", "three"}}}}},
	{raw: "{?count}", exp: "?count=one,two,three", want: []nameVal{{"count", Value{T: ValueTypeList, V: []string{"one", "two", "three"}}}}},
	{raw: "{?count*}", exp: "?count=one&count=two&count=three", want: []nameVal{{"count", Value{T: ValueTypeList, V: []string{"one", "two", "three"}}}}},
	{raw: "{&count*}", exp: "&count=one&count=two&count=three", want: []nameVal{{"count", Value{T: ValueTypeList, V: []string{"one", "two", "three"}}}}},
	{raw: "{var}", exp: "value", want: []nameVal{{"var", Value{T: ValueTypeString, V: []string{"value"}}}}},
	{raw: "{hello}", exp: "Hello%20World%21", want: []nameVal{{"hello", Value{T: ValueTypeString, V: []string{"Hello World!"}}}}},
	{raw: "{half}", exp: "50%25", want: []nameVal{{"half", Value{T: ValueTypeString, V: []string{"50%"}}}}},
	{raw: "O{empty}X", exp: "OX", want: []nameVal{{"empty", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "O{undef}X", exp: "OX", want: []nameVal{{"undef", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "{x,y}", exp: "1024,768", want: []nameVal{{"x", Value{T: ValueTypeString, V: []string{"1024"}}}, {"y", Value{T: ValueTypeString, V: []string{"768"}}}}},
	{raw: "{x,hello,y}", exp: "1024,Hello%20World%21,768", want: []nameVal{{"hello", Value{T: ValueTypeString, V: []string{"Hello World!"}}}, {"x", Value{T: ValueTypeString, V: []string{"1024"}}}, {"y", Value{T: ValueTypeString, V: []string{"768"}}}}},
	{raw: "?{x,empty}", exp: "?1024,", want: []nameVal{{"empty", Value{T: ValueTypeString, V: []string{""}}}, {"x", Value{T: ValueTypeString, V: []string{"1024"}}}}},
	{raw: "?{x,undef}", exp: "?1024", want: []nameVal{{"undef", Value{T: ValueTypeString, V: []string{""}}}, {"x", Value{T: ValueTypeString, V: []string{"1024"}}}}},
	{raw: "?{undef,y}", exp: "?768", want: []nameVal{{"undef", Value{T: ValueTypeString, V: []string{"768"}}}, {"y", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "{var:3}", exp: "val", want: []nameVal{{"var:3", Value{T: ValueTypeString, V: []string{"val"}}}}},
	{raw: "{var:30}", exp: "value", want: []nameVal{{"var:30", Value{T: ValueTypeString, V: []string{"value"}}}}},
	{raw: "{list}", exp: "red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "{list*}", exp: "red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "{keys}", exp: "semi,%3B,dot,.,comma,%2C", want: []nameVal{{"keys", Value{T: ValueTypeList, V: []string{"semi", ";", "dot", ".", "comma", ","}}}}},
	{raw: "{keys*}", exp: "semi=%3B,dot=.,comma=%2C", wantNil: true},
	{raw: "{+var}", exp: "value", want: []nameVal{{"var", Value{T: ValueTypeString, V: []string{"value"}}}}},
	{raw: "{+hello}", exp: "Hello%20World!", want: []nameVal{{"hello", Value{T: ValueTypeString, V: []string{"Hello World!"}}}}},
	{raw: "{+half}", exp: "50%25", want: []nameVal{{"half", Value{T: ValueTypeString, V: []string{"50%"}}}}},
	{raw: "{base}index", exp: "http%3A%2F%2Fexample.com%2Fhome%2Findex", want: []nameVal{{"base", Value{T: ValueTypeString, V: []string{"http://example.com/home/"}}}}},
	{raw: "{+base}index", exp: "http://example.com/home/index", want: []nameVal{{"base", Value{T: ValueTypeString, V: []string{"http://example.com/home/"}}}}},
	{raw: "O{+empty}X", exp: "OX", want: []nameVal{{"empty", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "O{+undef}X", exp: "OX", want: []nameVal{{"undef", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "{+path}/here", exp: "/foo/bar/here", want: []nameVal{{"path", Value{T: ValueTypeString, V: []string{"/foo/bar"}}}}},
	{raw: "here?ref={+path}", exp: "here?ref=/foo/bar", want: []nameVal{{"path", Value{T: ValueTypeString, V: []string{"/foo/bar"}}}}},
	{raw: "up{+path}{var}/here", exp: "up/foo/barvalue/here", want: []nameVal{{"path", Value{T: ValueTypeString, V: []string{"/foo/barvalue"}}}, {"var", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "{+x,hello,y}", exp: "1024,Hello%20World!,768", want: []nameVal{{"hello", Value{T: ValueTypeString, V: []string{""}}}, {"x", Value{T: ValueTypeString, V: []string{"1024,Hello World!,768"}}}, {"y", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "{+path,x}/here", exp: "/foo/bar,1024/here", want: []nameVal{{"path", Value{T: ValueTypeString, V: []string{"/foo/bar,1024"}}}, {"x", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "{+path:6}/here", exp: "/foo/b/here", want: []nameVal{{"path:6", Value{T: ValueTypeString, V: []string{"/foo/b"}}}}},
	{raw: "{+list}", exp: "red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeString, V: []string{"red,green,blue"}}}}},
	{raw: "{+list*}", exp: "red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeString, V: []string{"red,green,blue"}}}}},
	{raw: "{+keys}", exp: "semi,;,dot,.,comma,,", want: []nameVal{{"keys", Value{T: ValueTypeString, V: []string{"semi,;,dot,.,comma,,"}}}}},
	{raw: "{+keys*}", exp: "semi=;,dot=.,comma=,", want: []nameVal{{"keys", Value{T: ValueTypeString, V: []string{"semi=;,dot=.,comma=,"}}}}},
	{raw: "{#var}", exp: "#value", want: []nameVal{{"var", Value{T: ValueTypeString, V: []string{"value"}}}}},
	{raw: "{#hello}", exp: "#Hello%20World!", want: []nameVal{{"hello", Value{T: ValueTypeString, V: []string{"Hello World!"}}}}},
	{raw: "{#half}", exp: "#50%25", want: []nameVal{{"half", Value{T: ValueTypeString, V: []string{"50%"}}}}},
	{raw: "foo{#empty}", exp: "foo#", want: []nameVal{{"empty", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "foo{#undef}", exp: "foo", want: []nameVal{}},
	{raw: "{#x,hello,y}", exp: "#1024,Hello%20World!,768", want: []nameVal{{"hello", Value{T: ValueTypeString, V: []string{""}}}, {"x", Value{T: ValueTypeString, V: []string{"1024,Hello World!,768"}}}, {"y", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "{#path,x}/here", exp: "#/foo/bar,1024/here", want: []nameVal{{"path", Value{T: ValueTypeString, V: []string{"/foo/bar,1024"}}}, {"x", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "{#path:6}/here", exp: "#/foo/b/here", want: []nameVal{{"path:6", Value{T: ValueTypeString, V: []string{"/foo/b"}}}}},
	{raw: "{#list}", exp: "#red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeString, V: []string{"red,green,blue"}}}}},
	{raw: "{#list*}", exp: "#red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeString, V: []string{"red,green,blue"}}}}},
	{raw: "{#keys}", exp: "#semi,;,dot,.,comma,,", want: []nameVal{{"keys", Value{T: ValueTypeString, V: []string{"semi,;,dot,.,comma,,"}}}}},
	{raw: "{#keys*}", exp: "#semi=;,dot=.,comma=,", want: []nameVal{{"keys", Value{T: ValueTypeString, V: []string{"semi=;,dot=.,comma=,"}}}}},
	{raw: "{.who}", exp: ".fred", want: []nameVal{{"who", Value{T: ValueTypeString, V: []string{"fred"}}}}},
	{raw: "{.who,who}", exp: ".fred.fred", want: []nameVal{{"who", Value{T: ValueTypeList, V: []string{"fred.fred", ""}}}}},
	{raw: "{.half,who}", exp: ".50%25.fred", want: []nameVal{{"half", Value{T: ValueTypeString, V: []string{"50%.fred"}}}, {"who", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "www{.dom*}", exp: "www.example.com", want: []nameVal{{"dom", Value{T: ValueTypeString, V: []string{"example.com"}}}}},
	{raw: "X{.var}", exp: "X.value", want: []nameVal{{"var", Value{T: ValueTypeString, V: []string{"value"}}}}},
	{raw: "X{.empty}", exp: "X.", want: []nameVal{{"empty", Value{T: ValueTypeString, V: []string{""}}}}},
	{raw: "X{.undef}", exp: "X", want: []nameVal{}},
	{raw: "X{.var:3}", exp: "X.val", want: []nameVal{{"var:3", Value{T: ValueTypeString, V: []string{"val"}}}}},
	{raw: "X{.list}", exp: "X.red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "X{.list*}", exp: "X.red.green.blue", want: []nameVal{{"list", Value{T: ValueTypeString, V: []string{"red.green.blue"}}}}},
	{raw: "X{.keys}", exp: "X.semi,%3B,dot,.,comma,%2C", want: []nameVal{{"keys", Value{T: ValueTypeList, V: []string{"semi", ";", "dot", ".", "comma", ","}}}}},
	{raw: "X{.keys*}", exp: "X.semi=%3B.dot=..comma=%2C", wantNil: true},
	{raw: "X{.empty_keys}", exp: "X", want: []nameVal{}},
	{raw: "X{.empty_keys*}", exp: "X", want: []nameVal{}},
	{raw: "{/who}", exp: "/fred", want: []nameVal{{"who", Value{T: ValueTypeString, V: []string{"fred"}}}}},
	{raw: "{/who,who}", exp: "/fred/fred", want: []nameVal{{"who", Value{T: ValueTypeList, V: []string{"fred", "fred"}}}}},
	{raw: "{/half,who}", exp: "/50%25/fred", want: []nameVal{{"half", Value{T: ValueTypeString, V: []string{"50%"}}}, {"who", Value{T: ValueTypeString, V: []string{"fred"}}}}},
	{raw: "{/who,dub}", exp: "/fred/me%2Ftoo", want: []nameVal{{"dub", Value{T: ValueTypeString, V: []string{"me/too"}}}, {"who", Value{T: ValueTypeString, V: []string{"fred"}}}}},
	{raw: "{/var}", exp: "/value", want: []nameVal{{"var", Value{T: ValueTypeString, V: []string{"value"}}}}},
	{raw: "{/var,empty}", exp: "/value/", want: []nameVal{{"empty", Value{T: ValueTypeString, V: []string{""}}}, {"var", Value{T: ValueTypeString, V: []string{"value"}}}}},
	{raw: "{/var,undef}", exp: "/value", want: []nameVal{{"undef", Value{T: ValueTypeString, V: []string{""}}}, {"var", Value{T: ValueTypeString, V: []string{"value"}}}}},
	{raw: "{/var,x}/here", exp: "/value/1024/here", want: []nameVal{{"var", Value{T: ValueTypeString, V: []string{"value"}}}, {"x", Value{T: ValueTypeString, V: []string{"1024"}}}}},
	{raw: "{/var:1,var}", exp: "/v/value", want: []nameVal{{"var", Value{T: ValueTypeString, V: []string{"value"}}}, {"var:1", Value{T: ValueTypeString, V: []string{"v"}}}}},
	{raw: "{/list}", exp: "/red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "{/list*}", exp: "/red/green/blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "{/list*,path:4}", exp: "/red/green/blue/%2Ffoo", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}, {"path:4", Value{T: ValueTypeString, V: []string{"/foo"}}}}},
	{raw: "{/keys}", exp: "/semi,%3B,dot,.,comma,%2C", want: []nameVal{{"keys", Value{T: ValueTypeList, V: []string{"semi", ";", "dot", ".", "comma", ","}}}}},
	{raw: "{/keys*}", exp: "/semi=%3B/dot=./comma=%2C", wantNil: true},
	{raw: "{;who}", exp: ";who=fred", want: []nameVal{{"who", Value{T: ValueTypeString, V: []string{"fred"}}}}},
	{raw: "{;half}", exp: ";half=50%25", want: []nameVal{{"half", Value{T: ValueTypeString, V: []string{"50%"}}}}},
	{raw: "{;empty}", exp: ";empty", want: []nameVal{}},
	{raw: "{;v,empty,who}", exp: ";v=6;empty;who=fred", want: []nameVal{{"v", Value{T: ValueTypeString, V: []string{"6"}}}, {"who", Value{T: ValueTypeString, V: []string{"fred"}}}}},
	{raw: "{;v,bar,who}", exp: ";v=6;who=fred", want: []nameVal{{"v", Value{T: ValueTypeString, V: []string{"6"}}}, {"who", Value{T: ValueTypeString, V: []string{"fred"}}}}},
	{raw: "{;x,y}", exp: ";x=1024;y=768", want: []nameVal{{"x", Value{T: ValueTypeString, V: []string{"1024"}}}, {"y", Value{T: ValueTypeString, V: []string{"768"}}}}},
	{raw: "{;x,y,empty}", exp: ";x=1024;y=768;empty", want: []nameVal{{"x", Value{T: ValueTypeString, V: []string{"1024"}}}, {"y", Value{T: ValueTypeString, V: []string{"768"}}}}},
	{raw: "{;x,y,undef}", exp: ";x=1024;y=768", want: []nameVal{{"x", Value{T: ValueTypeString, V: []string{"1024"}}}, {"y", Value{T: ValueTypeString, V: []string{"768"}}}}},
	{raw: "{;hello:5}", exp: ";hello=Hello", want: []nameVal{{"hello:5", Value{T: ValueTypeString, V: []string{"Hello"}}}}},
	{raw: "{;list}", exp: ";list=red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "{;list*}", exp: ";list=red;list=green;list=blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "{;keys}", exp: ";keys=semi,%3B,dot,.,comma,%2C", want: []nameVal{{"keys", Value{T: ValueTypeList, V: []string{"semi", ";", "dot", ".", "comma", ","}}}}},
	{raw: "{;keys*}", exp: ";semi=%3B;dot=.;comma=%2C", wantNil: true},
	{raw: "{?who}", exp: "?who=fred", want: []nameVal{{"who", Value{T: ValueTypeString, V: []string{"fred"}}}}},
	{raw: "{?half}", exp: "?half=50%25", want: []nameVal{{"half", Value{T: ValueTypeString, V: []string{"50%"}}}}},
	{raw: "{?x,y}", exp: "?x=1024&y=768", want: []nameVal{{"x", Value{T: ValueTypeString, V: []string{"1024"}}}, {"y", Value{T: ValueTypeString, V: []string{"768"}}}}},
	{raw: "{?x,y,empty}", exp: "?x=1024&y=768&empty=", want: []nameVal{{"empty", Value{T: ValueTypeString, V: []string{""}}}, {"x", Value{T: ValueTypeString, V: []string{"1024"}}}, {"y", Value{T: ValueTypeString, V: []string{"768"}}}}},
	{raw: "{?x,y,undef}", exp: "?x=1024&y=768", want: []nameVal{{"x", Value{T: ValueTypeString, V: []string{"1024"}}}, {"y", Value{T: ValueTypeString, V: []string{"768"}}}}},
	{raw: "{?var:3}", exp: "?var=val", want: []nameVal{{"var:3", Value{T: ValueTypeString, V: []string{"val"}}}}},
	{raw: "{?list}", exp: "?list=red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "{?list*}", exp: "?list=red&list=green&list=blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "{?keys}", exp: "?keys=semi,%3B,dot,.,comma,%2C", want: []nameVal{{"keys", Value{T: ValueTypeList, V: []string{"semi", ";", "dot", ".", "comma", ","}}}}},
	{raw: "{?keys*}", exp: "?semi=%3B&dot=.&comma=%2C", wantNil: true},
	{raw: "{&who}", exp: "&who=fred", want: []nameVal{{"who", Value{T: ValueTypeString, V: []string{"fred"}}}}},
	{raw: "{&half}", exp: "&half=50%25", want: []nameVal{{"half", Value{T: ValueTypeString, V: []string{"50%"}}}}},
	{raw: "?fixed=yes{&x}", exp: "?fixed=yes&x=1024", want: []nameVal{{"x", Value{T: ValueTypeString, V: []string{"1024"}}}}},
	{raw: "{&x,y,empty}", exp: "&x=1024&y=768&empty=", want: []nameVal{{"empty", Value{T: ValueTypeString, V: []string{""}}}, {"x", Value{T: ValueTypeString, V: []string{"1024"}}}, {"y", Value{T: ValueTypeString, V: []string{"768"}}}}},
	{raw: "{&x,y,undef}", exp: "&x=1024&y=768", want: []nameVal{{"x", Value{T: ValueTypeString, V: []string{"1024"}}}, {"y", Value{T: ValueTypeString, V: []string{"768"}}}}},
	{raw: "{&var:3}", exp: "&var=val", want: []nameVal{{"var:3", Value{T: ValueTypeString, V: []string{"val"}}}}},
	{raw: "{&list}", exp: "&list=red,green,blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "{&list*}", exp: "&list=red&list=green&list=blue", want: []nameVal{{"list", Value{T: ValueTypeList, V: []string{"red", "green", "blue"}}}}},
	{raw: "{&keys}", exp: "&keys=semi,%3B,dot,.,comma,%2C", want: []nameVal{{"keys", Value{T: ValueTypeList, V: []string{"semi", ";", "dot", ".", "comma", ","}}}}},
	{raw: "{&keys*}", exp: "&semi=%3B&dot=.&comma=%2C", wantNil: true},
	{raw: "{special_chars}", exp: "2001%3Adb8%3A%3A35", want: []nameVal{{"special_chars", Value{T: ValueTypeString, V: []string{"2001:db8::35"}}}}},
}

func sortedNames(m Values) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func TestMatchGoldenCapture(t *testing.T) {
	for _, c := range matchGoldenCases {
		tmpl, err := New(c.raw)
		if err != nil {
			t.Errorf("%q: unexpected parse error: %v", c.raw, err)
			continue
		}
		got := tmpl.Match(c.exp)
		if c.wantNil {
			if got != nil {
				t.Errorf("%q against %q: want nil match, got %#v", c.raw, c.exp, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("%q against %q: want match, got nil", c.raw, c.exp)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%q against %q: got %d vars %v, want %d", c.raw, c.exp, len(got), sortedNames(got), len(c.want))
			continue
		}
		for _, nv := range c.want {
			gv, ok := got[nv.name]
			if !ok {
				t.Errorf("%q against %q: missing capture %q (got %v)", c.raw, c.exp, nv.name, sortedNames(got))
				continue
			}
			if gv.T != nv.val.T || !reflect.DeepEqual(gv.V, nv.val.V) {
				t.Errorf("%q against %q: capture %q = {T:%d V:%#v}, want {T:%d V:%#v}",
					c.raw, c.exp, nv.name, gv.T, gv.V, nv.val.T, nv.val.V)
			}
		}
	}
}

// differentialKnownDivergence lists templates where Match() currently returns
// nil (the VM rejects the expansion) but Regexp().MatchString() accepts it.
// These are explode-with-KV forms whose "=" separators the VM rejects. They are
// recorded as conscious exceptions so the Match()!=nil <=> Regexp() invariant is
// asserted everywhere else, and any change here is a deliberate decision.
var differentialKnownDivergence = map[string]bool{
	"{keys*}":   true,
	"X{.keys*}": true,
	"{/keys*}":  true,
	"{;keys*}":  true,
	"{?keys*}":  true,
	"{&keys*}":  true,
}

func TestMatchRegexpDifferential(t *testing.T) {
	for _, c := range testTemplateCases {
		tmpl, err := New(c.raw)
		if err != nil {
			t.Errorf("%q: unexpected parse error: %v", c.raw, err)
			continue
		}
		matchOK := tmpl.Match(c.expected) != nil
		regexpOK := tmpl.Regexp().MatchString(c.expected)
		if differentialKnownDivergence[c.raw] {
			if matchOK {
				t.Errorf("%q: expected known divergence (Match nil) but Match succeeded", c.raw)
			}
			continue
		}
		if matchOK != regexpOK {
			t.Errorf("%q against %q: Match()=%v but Regexp().MatchString()=%v (must agree)",
				c.raw, c.expected, matchOK, regexpOK)
		}
	}
}

// stricterThanVMCases document inputs where the scanner intentionally differs
// from the original Pike-VM matcher: it returns nil (no match) where that VM
// matched. These are hand-crafted, NON-Expand-reachable inputs — reserved or
// named expressions with a duplicated, exploded variable name fed degenerate
// separator-only input. The shared one-group-per-expression grammar makes the
// capture split genuinely ambiguous there; the scanner's ordered backtracking
// resolves it toward nil rather than a spurious empty match. The scanner never
// over-matches (it only ever returns nil where the VM matched, never the
// reverse), and every Expand-produced expansion still round-trips (see
// TestMatchGoldenCapture and the Expand->Match round-trip). These cases are
// frozen so the stricter behavior cannot silently drift.
var stricterThanVMCases = []struct {
	raw string
	in  string
}{
	{raw: "{;a*,a*}", in: ";;"},
	{raw: "{;b,c*,c*}", in: ";b=.,x,x,foo;;"},
}

func TestMatchStricterThanVM(t *testing.T) {
	for _, c := range stricterThanVMCases {
		tmpl, err := New(c.raw)
		if err != nil {
			t.Errorf("%q: unexpected parse error: %v", c.raw, err)
			continue
		}
		if got := tmpl.Match(c.in); got != nil {
			t.Errorf("%q against %q: expected nil (stricter-than-VM, non-Expand-reachable), got %#v", c.raw, c.in, got)
		}
	}
}
