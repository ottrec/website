package ottrecflt

import (
	"strings"
	"testing"
)

func TestFuzzyWordMatcher(t *testing.T) {
	// note: just need to test the normalization since we already test the match
	// logic in [TestFuzzyWordMatchFunc]
	test := func(s string, q ...string) {
		m := newFuzzyWordMatcher()
		if !m(s)(s) {
			t.Errorf("newFuzzyWordMatcher()(%q)(%q) != true", s, s)
		}
		for _, q := range q {
			if !m(q)(s) {
				t.Errorf("newFuzzyWordMatcher()(%q)(%q) != true", q, s)
			}
		}
	}
	test("òoo",
		"ooo",
		"o",
		"oo",
	)
	test("abc \u00a0 déf \u2003 ghi",
		"abc",
		"def",
		"a d",
		"abc de",
		"abc \u00a0\u2003 de",
		"de",
		"DE",
		"Dë",
		"dË",
		"DE gHi",
	)
	test("abc-def",
		"def",
		"ab de",
		"abc def",
	)
	test("abc\u2e3adef", // two-em dash
		"def",
		"ab de",
		"abc def",
	)
	test("abc.def. ghi",
		"abcdef ghi",
		"abc ghi",
	)
	test("François Dupuis Recreation Centre",
		"francois dupuis",
		"francois rec centre",
	)
	test("Hunt Club-Riverside Park Community Centre",
		"hunt club riverside",
		"hunt club - riverside park",
		"hunt club riverside centre",
	)
	test("R.J. Kennedy Community Centre and Arena",
		"rj kennedy",
		"r.j kennedy",
		"r.j. kennedy",
	)
	test("St. Laurent Complex",
		"st laurent",
		"st laurent complex",
		"St. Laurent complex",
	)
	test("Splash Wave Pool",
		"splash",
		"splash pool",
		"splash wave pool",
	)
}

func TestFuzzyWordMatchFunc(t *testing.T) {
	for _, tc := range []struct {
		str   string
		qry   string
		match bool
	}{
		{"", "", true},
		{"", "test", false},
		{" ", " ", true},
		{" ", " test ", false},
		{" another example \n test case ", "", true},
		{" another example \n test case ", " ", true},
		{" another example \n test case ", "another", true},
		{" another example \n test case ", "anothe", true},
		{" another example \n test case ", "nother", false}, // must be prefix
		{" another example \n test case ", "  an ex", true},
		{" another example \n test case ", "  an ex test", true},
		{" another example \n test case ", "  an test ex", false}, // must be in order
		{" another example \n test case ", "  another case", true},
		{" another example \n test case ", "a\nc", true},
		{"sdf", "dfg", false},
	} {
		match := fuzzyWordMatchFunc(tc.str, tc.qry, strings.HasPrefix)
		if match != tc.match {
			t.Errorf("FuzzyWordMatch(%q, %q, strings.HasPrefix) != %t", tc.str, tc.qry, tc.match)
		}
	}
}

func TestCutWord(t *testing.T) {
	for _, tc := range []struct {
		str  string
		word string
		rest string
		ok   bool
	}{
		{"", "", "", false},
		{" ", " ", "", false},
		{" \t\r\n\u00a0", " \t\r\n\u00a0", "", false},
		{"a", "a", "", true},
		{"aa", "aa", "", true},
		{"aa ", "aa", " ", true},
		{" aa", "aa", "", true},
		{" aa ", "aa", " ", true},
		{"a b", "a", " b", true},
		{"aa bb", "aa", " bb", true},
		{"aa  bb", "aa", "  bb", true},
		{" aa bb", "aa", " bb", true},
		{"  aa  bb", "aa", "  bb", true},
	} {
		word, rest, ok := cutWord(tc.str)
		if word != tc.word || rest != tc.rest || ok != tc.ok {
			t.Errorf("cutWord(%q): expected (%q, %q, %t), got (%q, %q, %t)", tc.str, tc.word, tc.rest, tc.ok, word, rest, ok)
		}
		// ensure cutting it ends eventually
		if ok {
			for prev := rest; ; {
				word, rest, ok = cutWord(prev)
				if !ok {
					break
				}
				if prev == rest {
					t.Errorf("cutWord(%q) didn't eat a word", prev)
					break
				}
				prev = rest
			}
		}
	}
}
