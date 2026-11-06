package ui

import (
	"strings"
	"testing"
)

// TestReviewReasonsAreTranslated pins the coupling between the wording of the store validators
// and the translation of the form: every reason that a bad value can produce must come out of
// reasonText in French, never as the English text of the store. A store reason that is reworded
// or added without a pattern in formerr.go makes this test fail.
//
// Parameters:
//   - t: test to report to
func TestReviewReasonsAreTranslated(t *testing.T) {
	long := strings.Repeat("a", 300)

	conn := map[string][]string{
		"name":    {"", "-x", "a b", "a/b", "\x01", "\xff", " a", "a:b", "a{", long},
		"target":  {"", "-x", "a b", "\x01", "\xff", ":a", "a:", long},
		"via":     {"-x", "{", "}", "{nope}", "\x01", "\xff", long},
		"user":    {"-x", "a b", "\x01", "\xff", long},
		"aliases": {"-x", "a b", "prod,prod", "\x01", long},
		"ip":      {"nope", "1.2.3.4%eth0", "\x01", "1.2.3"},
		"desc":    {"\x01", "\xff", "a\nb", long + long + long + long},
	}

	vals := map[string]string{"name": probeName, "target": probeTarget}
	fr := newRig(t, newSandbox(t).with(func(o *Options) { o.Printer = newPrinter("fr") })).r.e

	refused := 0

	check := func(where string, v string, err error) {
		if err == nil {
			return
		}

		refused++

		got := reasonText(fr, err)
		raw := err.Error()
		_, rest, ok := strings.Cut(raw, ": ")

		if ok && strings.HasSuffix(got, rest) || got == sanitize(raw) {
			t.Errorf("%s %q: the reason is not translated: %q", where, v, got)
		}
	}

	for id, values := range conn {
		for _, v := range values {
			cur := map[string]string{"name": vals["name"], "target": vals["target"], id: v}
			check("connection "+id, v, probeConn(id, cur))
		}
	}

	shellFields := map[kind]map[string][]string{
		kindRepo:  {"name": {"", "-x", "a b", "\x01", "\xff", long}, "path": {"\x01", "\xff"}},
		kindAlias: {"name": {"", "-x", "a b", "\x01", long}, "cmd": {"", "\x01", "\xff"}},
		kindFunc: {
			"name":   {"", "-x", "a b", "\x01", "1a", long},
			"script": {"\x01", "\xff"},
		},
	}

	for k, fields := range shellFields {
		for id, values := range fields {
			for _, v := range values {
				check(kindSlug[k]+" "+id, v, probeShell(k, id, map[string]string{id: v}))
			}
		}
	}

	if refused < 40 {
		t.Errorf("only %d bad values were refused, the fixtures drifted", refused)
	}
}
