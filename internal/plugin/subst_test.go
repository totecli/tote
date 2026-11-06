package plugin

import (
	"strings"
	"testing"
)

// TestParseTemplate checks the template grammar and the classification of placeholders.
//
// Parameters:
//   - t: test to report to
func TestParseTemplate(t *testing.T) {
	type want struct {
		kind     PlaceholderKind
		name     string
		member   string
		optional bool
	}

	good := []struct {
		in      string
		holders []want
		literal string
		isLit   bool
	}{
		{"", nil, "", true},
		{"plain text", nil, "plain text", true},
		{"{{", nil, "{", true},
		{"{{}}", nil, "{}", true},
		{"a{{b}}c", nil, "a{b}c", true},
		{"{x}", []want{{KindField, "x", "", false}}, "", false},
		{"{x?}", []want{{KindField, "x", "", true}}, "", false},
		{"--tag={tag}", []want{{KindField, "tag", "", false}}, "", false},
		{"{{{x}}}", []want{{KindField, "x", "", false}}, "", false},
		{"{cwd}/{home}", []want{{KindCwd, "cwd", "", false}, {KindHome, "home", "", false}}, "", false},
		{
			"{setting.token_env}",
			[]want{{KindSetting, "setting.token_env", "token_env", false}},
			"",
			false,
		},
		{"{repo.path}", []want{{KindRepo, "repo.path", "path", false}}, "", false},
		{"{repo.name}{repo.remote}{repo.slug}{repo.branch?}", []want{
			{KindRepo, "repo.name", "name", false},
			{KindRepo, "repo.remote", "remote", false},
			{KindRepo, "repo.slug", "slug", false},
			{KindRepo, "repo.branch", "branch", true},
		}, "", false},
		{"{item.id}", []want{{KindItemID, "item.id", "", false}}, "", false},
		{
			"{item.data.html_url}",
			[]want{{KindItemData, "item.data.html_url", "html_url", false}},
			"",
			false,
		},
		{"{a}{a}", []want{{KindField, "a", "", false}, {KindField, "a", "", false}}, "", false},
	}

	for _, tt := range good {
		tpl, err := ParseTemplate(tt.in)
		if err != nil {
			t.Errorf("ParseTemplate(%q) = %v", tt.in, err)

			continue
		}

		hs := tpl.Placeholders()
		if len(hs) != len(tt.holders) {
			t.Errorf("ParseTemplate(%q) holders = %+v", tt.in, hs)

			continue
		}

		for i, h := range hs {
			w := tt.holders[i]
			if h.Kind != w.kind || h.Name != w.name || h.Member != w.member || h.Optional != w.optional {
				t.Errorf("ParseTemplate(%q)[%d] = %+v, want %+v", tt.in, i, h, w)
			}
		}

		if lit, ok := tpl.Literal(); ok != tt.isLit || (ok && lit != tt.literal) {
			t.Errorf("Literal(%q) = %q, %v", tt.in, lit, ok)
		}
	}

	bad := []struct {
		in     string
		reason string
	}{
		{"{", "unterminated"},
		{"a{b", "unterminated"},
		{"}", "stray_close"},
		{"a}", "stray_close"},
		{"{{}", "stray_close"},
		{"{}", "empty"},
		{"{?}", "empty"},
		{"{{x}", "stray_close"},
		{"{a{b}}", "stray_open"},
		{"{A}", "bad_name"},
		{"{a-b}", "bad_name"},
		{"{a b}", "bad_name"},
		{"{1a}", "bad_name"},
		{"{a..b}", "bad_name"},
		{"{.a}", "bad_name"},
		{"{a.}", "bad_name"},
		{"{a??}", "bad_name"},
		{"{" + strings.Repeat("a", 33) + "}", "bad_name"},
		{"{repo}", "unknown"},
		{"{repo.url}", "unknown"},
		{"{repo.path.x}", "unknown"},
		{"{setting}", "unknown"},
		{"{setting.a.b}", "unknown"},
		{"{item}", "unknown"},
		{"{item.name}", "unknown"},
		{"{item.data}", "unknown"},
		{"{item.data.A}", "bad_name"},
		{"{x.y}", "unknown"},
		{"{cwd.x}", "unknown"},
	}

	for _, tt := range bad {
		if _, err := ParseTemplate(tt.in); err == nil || err.Reason != tt.reason {
			t.Errorf("ParseTemplate(%q) = %v, want %s", tt.in, err, tt.reason)
		}
	}

	if tpl, _ := ParseTemplate("{x}"); func() bool { _, ok := tpl.Whole(); return !ok }() {
		t.Error("Whole refused a lone placeholder")
	}

	for _, s := range []string{"a{x}", "{x}{y}", "x", ""} {
		if tpl, _ := ParseTemplate(s); func() bool { _, ok := tpl.Whole(); return ok }() {
			t.Errorf("Whole accepted %q", s)
		}
	}
}

// TestScopeCheck checks the scope rules of the placeholders.
//
// Parameters:
//   - t: test to report to
func TestScopeCheck(t *testing.T) {
	scope := Scope{Fields: map[string]string{"x": "text"}, Settings: map[string]string{"s": "string"}}
	inItem := scope
	inItem.Item = true

	tests := []struct {
		name  string
		scope Scope
		want  string
	}{
		{"x", scope, ""},
		{"y", scope, CodePlaceholderUnknown},
		{"setting.s", scope, ""},
		{"setting.t", scope, CodePlaceholderUnknown},
		{"repo.path", scope, ""},
		{"cwd", scope, ""},
		{"home", scope, ""},
		{"item.id", scope, CodePlaceholderScope},
		{"item.data.k", scope, CodePlaceholderScope},
		{"item.id", inItem, ""},
		{"item.data.k", inItem, ""},
		{"y", inItem, CodePlaceholderUnknown},
	}

	for _, tt := range tests {
		tpl, err := ParseTemplate("{" + tt.name + "}")
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}

		if got := tt.scope.Check(tpl.Placeholders()[0]); got != tt.want {
			t.Errorf("Check(%q, item=%v) = %q, want %q", tt.name, tt.scope.Item, got, tt.want)
		}
	}
}

// FuzzTemplate checks that the parser never panics and that parsing is stable: a template that
// parses has placeholders whose names parse again on their own.
//
// Parameters:
//   - f: fuzz target
func FuzzTemplate(f *testing.F) {
	for _, s := range []string{
		"{x}", "{{}}", "a{b?}c", "{", "}", "{repo.path}", "{item.data.k}",
		"\x00{",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		tpl, err := ParseTemplate(s)
		if err != nil {
			return
		}

		for _, h := range tpl.Placeholders() {
			brace := "{" + h.Name + "}"
			if _, err := ParseTemplate(brace); err != nil {
				t.Fatalf("placeholder %+v of %q does not reparse: %v", h, s, err)
			}
		}
	})
}
