package plugin

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// tpl parses a template that must be valid.
//
// Parameters:
//   - t: test to report to
//   - s: template text
func tpl(t testing.TB, s string) Template {
	t.Helper()

	tm, err := ParseTemplate(s)
	if err != nil {
		t.Fatalf("template %q: %v", s, err)
	}

	return tm
}

// vals builds the values of a call: text fields, a choice and a bool, settings of each type, a
// repository, a row, the working directory and the home.
func vals() *Values {
	return &Values{
		Fields: map[string]FieldValue{
			"x":     {Text: "hello", Type: "text"},
			"empty": {Text: "", Type: "text"},
			"dash":  {Text: "-rf", Type: "text"},
			"view":  {Text: "--assets", Type: "choice"},
			"flag":  {Text: "--checksum", Type: "bool"},
			"off":   {Text: "", Type: "bool"},
		},
		Settings: map[string]SettingValue{
			"owner": {Value: "20syldev"},
			"on":    {Value: true},
			"n":     {Value: int64(42)},
			"repos": {Value: []string{"a/b", "c/d", "-e"}},
			"none":  {Value: []string(nil)},
		},
		Repo: &RepoInfo{Name: "tote", Path: "/home/u/tote", Slug: "o/r", Branch: "main"},
		Item: &Item{ID: "cli/cli", Data: map[string]string{"url": "https://x.dev/y"}},
		Cwd:  "/work",
		Home: "/home/u",
	}
}

// TestExpandArgv checks the substitution of one argv element: every placeholder context, the
// optional and the list forms, and the dash rule.
//
// Parameters:
//   - t: test to report to
func TestExpandArgv(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		allow []string
		want  []string
		code  string
	}{
		{name: "literal", src: "stash", want: []string{"stash"}},
		{name: "field", src: "{x}", want: []string{"hello"}},
		{name: "field in text", src: "pre-{x}-post", want: []string{"pre-hello-post"}},
		{name: "two fields", src: "{x}{x}", want: []string{"hellohello"}},
		{name: "doubled braces", src: "{{x}}", want: []string{"{x}"}},
		{name: "doubled braces around a value", src: "{{{x}}}", want: []string{"{hello}"}},
		{name: "setting string", src: "{setting.owner}", want: []string{"20syldev"}},
		{name: "setting bool", src: "{setting.on}", want: []string{"true"}},
		{name: "setting int", src: "{setting.n}", want: []string{"42"}},
		{name: "repo slug", src: "{repo.slug}", want: []string{"o/r"}},
		{name: "repo members", src: "{repo.name}:{repo.branch}", want: []string{"tote:main"}},
		{name: "repo path", src: "{repo.path}", want: []string{"/home/u/tote"}},
		{name: "item id", src: "{item.id}", want: []string{"cli/cli"}},
		{name: "item data", src: "{item.data.url}", want: []string{"https://x.dev/y"}},
		{name: "cwd", src: "{cwd}", want: []string{"/work"}},
		{name: "home", src: "{home}/.cfg", want: []string{"/home/u/.cfg"}},
		{
			name: "list expands", src: "{setting.repos}", allow: []string{"setting.repos"},
			want: []string{"a/b", "c/d", "-e"},
		},
		{name: "empty list expands to nothing", src: "{setting.none}", want: nil},
		{name: "empty optional list expands to nothing", src: "{setting.none?}", want: nil},
		{name: "list in text", src: "x{setting.repos}", code: CodeProducedInvalid},
		{name: "empty required", src: "{empty}", code: CodeUnavailable},
		{name: "empty optional drops", src: "{empty?}", want: nil},
		{name: "empty optional drops the whole element", src: "--tag={empty?}", want: nil},
		{name: "unresolved repo member", src: "{repo.remote}", code: CodeUnavailable},
		{name: "missing field", src: "{nope}", code: CodeUnavailable},
		{name: "off bool optional", src: "{off?}", want: nil},
		{name: "off bool required", src: "{off}", code: CodeUnavailable},
		{name: "on bool gives the flag", src: "{flag?}", want: []string{"--checksum"}},
		{name: "dash refused", src: "{dash}", code: CodeDashRefused},
		{name: "dash refused optional", src: "{dash?}", code: CodeDashRefused},
		{
			name: "dash allowed by name", src: "{dash}", allow: []string{"dash"},
			want: []string{"-rf"},
		},
		{
			name: "dash allowed for another name does not help", src: "{dash}",
			allow: []string{"x"}, code: CodeDashRefused,
		},
		{name: "choice may start with a dash", src: "{view}", want: []string{"--assets"}},
		{name: "bool flag may start with a dash", src: "{flag}", want: []string{"--checksum"}},
		{
			name: "dash after a literal start is fine", src: "--name={dash}",
			want: []string{"--name=-rf"},
		},
		{name: "dash inside an element is fine", src: "a{dash}", want: []string{"a-rf"}},
		{name: "literal dash start is the author's", src: "-v", want: []string{"-v"}},
		{name: "list item with a dash is refused", src: "{setting.repos}", code: CodeDashRefused},
		{name: "setting without a dash", src: "{setting.owner}", want: []string{"20syldev"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandArgv(tpl(t, tt.src), vals(), setOf(tt.allow))

			if tt.code != "" {
				if err == nil || err.Code != tt.code {
					t.Fatalf("got %v, %v want code %s", got, err, tt.code)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

// setOf turns a list of names into a set.
//
// Parameters:
//   - names: names to put in the set
func setOf(names []string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}

	return m
}

// TestExpandHostileValues checks that a value that cannot be a safe argv element is refused with
// the rule it breaks, whichever way it reaches the element.
//
// Parameters:
//   - t: test to report to
func TestExpandHostileValues(t *testing.T) {
	long := strings.Repeat("a", MaxArgvElemBytes)

	tests := []struct {
		name  string
		value string
		rule  string
	}{
		{"newline", "a\nb", "control"},
		{"carriage return", "a\rb", "control"},
		{"nul", "a\x00b", "control"},
		{"tab", "a\tb", "control"},
		{"escape", "\x1b[31mred", "control"},
		{"del", "a\x7fb", "control"},
		{"c1 control", "a\u009bb", "control"},
		{"line separator", "a\u2028b", "control"},
		{"bidi override", "a\u202eb", "control"},
		{"zero width space", "a\u200bb", "control"},
		{"tag character", "a\U000e0041b", "control"},
		{"invalid utf8", "a\xffb", "utf8"},
		{"truncated utf8", "a\xe2\x82", "utf8"},
		{"one byte over", long + "a", "length"},
		{"huge", strings.Repeat("a", 10<<20), "length"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := vals()
			v.Fields["x"] = FieldValue{Text: tt.value, Type: "text"}

			for _, src := range []string{"{x}", "pre{x}", "{x}post"} {
				got, err := ExpandArgv(tpl(t, src), v, nil)

				if err == nil || err.Code != CodeProducedInvalid || len(got) != 0 {
					t.Fatalf("%s: got %q, %v", src, got, err)
				}

				if err.Args[1] != tt.rule {
					t.Errorf("%s: rule %q want %q", src, err.Args[1], tt.rule)
				}
			}

			if _, _, err := ExpandScalar(tpl(t, "{x}"), v, 1024); err == nil {
				t.Errorf("scalar accepted %q", tt.value)
			}
		})
	}

	t.Run("exactly the limit is fine", func(t *testing.T) {
		v := vals()
		v.Fields["x"] = FieldValue{Text: long, Type: "text"}

		got, err := ExpandArgv(tpl(t, "{x}"), v, nil)
		if err != nil || len(got) != 1 || len(got[0]) != MaxArgvElemBytes {
			t.Fatalf("got %d elements, %v", len(got), err)
		}
	})

	t.Run("a list item is checked", func(t *testing.T) {
		v := vals()
		v.Settings["repos"] = SettingValue{Value: []string{"ok", "a\nb"}}

		if _, err := ExpandArgv(tpl(t, "{setting.repos}"), v, nil); err == nil {
			t.Error("a list item with a newline was accepted")
		}
	})
}

// TestExpandScalar checks the substitution of a value that must be one text: no dash rule, the
// optional form drops the value, a list is refused.
//
// Parameters:
//   - t: test to report to
func TestExpandScalar(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
		keep bool
		code string
	}{
		{name: "literal", src: "plain", want: "plain", keep: true},
		{name: "starts with a dash", src: "{dash}", want: "-rf", keep: true},
		{name: "optional empty", src: "{empty?}", keep: false},
		{name: "required empty", src: "{empty}", code: CodeUnavailable},
		{name: "list refused", src: "{setting.repos}", code: CodeProducedInvalid},
		{name: "repo path", src: "{repo.path}/sub", want: "/home/u/tote/sub", keep: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, keep, err := ExpandScalar(tpl(t, tt.src), vals(), 1024)

			if tt.code != "" {
				if err == nil || err.Code != tt.code {
					t.Fatalf("got %q, %v want code %s", got, err, tt.code)
				}

				return
			}

			if err != nil || got != tt.want || keep != tt.keep {
				t.Errorf("got %q %v %v want %q %v", got, keep, err, tt.want, tt.keep)
			}
		})
	}
}

// TestExpandLoose checks the display form: what does not resolve gives nothing, and nothing is
// an error.
//
// Parameters:
//   - t: test to report to
func TestExpandLoose(t *testing.T) {
	v := vals()
	v.Fields["bad"] = FieldValue{Text: "a\nb", Type: "text"}

	tests := []struct{ src, want string }{
		{"Drop {x} of {repo.name}?", "Drop hello of tote?"},
		{"{empty}|{repo.remote}|{nope}", "||"},
		{"{setting.repos}", ""},
		{"{bad}", ""},
		{"{{literal}}", "{literal}"},
	}

	for _, tt := range tests {
		if got := ExpandLoose(tpl(t, tt.src), v); got != tt.want {
			t.Errorf("%q: got %q want %q", tt.src, got, tt.want)
		}
	}
}

// FuzzExpandArgv checks that no template and no value make the substitution produce an element
// that is not valid UTF-8, holds a control character or is longer than the limit, and that an
// element which starts with a dash and whose template does not is only produced for the cases the
// dash rule allows.
//
// Parameters:
//   - f: fuzz target
func FuzzExpandArgv(f *testing.F) {
	for _, s := range []string{"{x}", "a{x}b", "{x?}", "{{x}}", "{setting.repos}", "--k={x}", "{"} {
		f.Add(s, "hello")
		f.Add(s, "-rf")
		f.Add(s, "a\nb")
	}

	f.Fuzz(func(t *testing.T, src, value string) {
		if len(src) > 512 || len(value) > 8192 {
			return
		}

		tm, err := ParseTemplate(src)
		if err != nil {
			return
		}

		v := vals()
		v.Fields["x"] = FieldValue{Text: value, Type: "text"}

		got, perr := ExpandArgv(tm, v, nil)
		if perr != nil {
			return
		}

		for _, e := range got {
			if !utf8.ValidString(e) || len(e) > MaxArgvElemBytes ||
				ProducedIssue(e, MaxArgvElemBytes) != "" {
				t.Fatalf("element %q from %q with %q", e, src, value)
			}

			if strings.HasPrefix(e, "-") && len(tm.Parts) > 0 && tm.Parts[0].Holder != nil {
				t.Fatalf("dash element %q from %q with %q", e, src, value)
			}
		}
	})
}
