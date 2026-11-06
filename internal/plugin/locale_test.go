package plugin

import (
	"strings"
	"testing"
)

// gftManifest reads the gft example as the official plugin it is.
//
// Parameters:
//   - t: test to report to
func gftManifest(t *testing.T) *Manifest {
	t.Helper()

	m, rep := Decode(readFixture(t, "gft/plugin.toml"),
		DecodeOptions{Host: testHost(), Official: true})
	if m == nil {
		t.Fatalf("gft refused: %v", rep.Issues)
	}

	return m
}

// TestParseLocale checks the reading of a locale file: key forms, limits and encoding.
//
// Parameters:
//   - t: test to report to
func TestParseLocale(t *testing.T) {
	got,
		issues := ParseLocale([]byte("a.b = \"x\"\n\"c.d\" = \"y\"\n[e]\nf = \"z\"\n[e.g]\nh = \"w\"\n" +
		"i.j = \"v\"\n"))
	if len(issues) != 0 {
		t.Fatalf("issues = %v", issues)
	}

	want := map[string]string{"a.b": "x", "c.d": "y", "e.f": "z", "e.g.h": "w", "e.g.i.j": "v"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q = %q, want %q", k, got[k], v)
		}
	}

	if len(got) != len(want) {
		t.Errorf("got %v", got)
	}

	bad := []struct {
		name string
		data string
		code string
		line int
	}{
		{"syntax", "a = \n", CodeLocaleInvalid, 1},
		{"not utf8", "a = \"\xff\"\n", CodeLocaleInvalid, 0},
		{"too big", "# " + strings.Repeat("a", MaxLocaleSize) + "\n", CodeLocaleInvalid, 0},
		{"number", "\n\na = 1\n", CodeTypeInvalid, 3},
		{"array", "a = [\"x\"]\n", CodeTypeInvalid, 1},
		{"control in value", "a = \"x\\u001by\"\n", CodeTextInvalid, 1},
		{"line feed in value", "a = \"x\\ny\"\n", CodeTextInvalid, 1},
		{"long value", "a = \"" + strings.Repeat("a", 1025) + "\"\n", CodeLimitExceeded, 1},
		{"long key", "\"" + strings.Repeat("a", 161) + "\" = \"x\"\n", CodeLimitExceeded, 1},
		{"deep", "[a.b.c.d.e.f.g.h.i]\nk = \"x\"\n", CodeLimitExceeded, 1},
	}

	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			m, issues := ParseLocale([]byte(tt.data))
			if len(issues) == 0 || issues[0].Code != tt.code {
				t.Fatalf("issues = %v, want %s", issues, tt.code)
			}

			if tt.line != 0 && issues[0].Line != tt.line {
				t.Errorf("line = %d, want %d", issues[0].Line, tt.line)
			}

			if tt.code == CodeLocaleInvalid && m != nil {
				t.Errorf("a refused file returned a table: %v", m)
			}
		})
	}
}

// TestLocaleParityGft checks the parity rules against the gft example and its two test catalogs.
//
// Parameters:
//   - t: test to report to
func TestLocaleParityGft(t *testing.T) {
	m := gftManifest(t)

	parse := func(name string) map[string]string {
		kv, issues := ParseLocale(readFixture(t, "gft/locales/"+name+".toml"))
		if len(issues) != 0 {
			t.Fatalf("%s: %v", name, issues)
		}

		return kv
	}

	files := map[string]map[string]string{"en": parse("en"), "fr": parse("fr")}
	locales := MergeLocales(m, files)

	if issues := CheckLocales(m, locales, true); len(issues) != 0 {
		t.Fatalf("complete catalogs refused: %v", issues)
	}

	// Every translatable key is present in French, and English holds exactly the choice labels.
	all := TranslatableKeys(m)
	for _, k := range all {
		if _, ok := locales["fr"][k]; !ok {
			t.Errorf("fr lacks %q", k)
		}
	}

	if len(locales["en"]) != 5 {
		t.Errorf("en holds %d keys, want the 5 choice labels", len(locales["en"]))
	}

	// A missing French key is a warning by default and an error with strict.
	drop := map[string]map[string]string{"en": locales["en"], "fr": {}}
	for k, v := range locales["fr"] {
		if k != "command.get.label" {
			drop["fr"][k] = v
		}
	}

	warn := CheckLocales(m, drop, false)
	if len(warn) != 1 || warn[0].Code != CodeLocaleMissingKey || warn[0].Severity != SeverityWarning ||
		warn[0].Path != "locales.fr.command.get.label" {
		t.Errorf("missing key (lenient) = %v", warn)
	}

	if strict := CheckLocales(m, drop, true); len(strict) != 1 || strict[0].Severity != SeverityError {
		t.Errorf("missing key (strict) = %v", strict)
	}

	// English may omit everything but the choice labels, and must not forget those.
	noEN := map[string]map[string]string{"fr": locales["fr"]}
	if issues := CheckLocales(m, noEN, false); len(issues) != 5 {
		t.Errorf("a missing en file gives %d issues, want 5: %v", len(issues), issues)
	}

	// An unknown key is locale_extra_key.
	extra := map[string]map[string]string{"en": locales["en"], "fr": {"nope.label": "x"}}
	if is := findIssue(CheckLocales(m, extra, false), CodeLocaleExtraKey); is == nil ||
		is.Path != "locales.fr.nope.label" {
		t.Errorf("extra key not reported: %v", CheckLocales(m, extra, false))
	}
}

// findIssue returns the first issue with a code.
//
// Parameters:
//   - issues: list to search
//   - code: stable code
func findIssue(issues []Issue, code string) *Issue {
	for i := range issues {
		if issues[i].Code == code {
			return &issues[i]
		}
	}

	return nil
}

// TestLocaleValues checks the value rules, which are errors even without strict.
//
// Parameters:
//   - t: test to report to
func TestLocaleValues(t *testing.T) {
	m := gftManifest(t)

	check := func(lang, key, value string) []Issue {
		l := MergeLocales(m, nil)
		l["en"] = map[string]string{
			"command.show.field.view.choice.--assets": "A", "command.show.field.view.choice.--detect": "D",
			"command.show.field.view.choice.--notes": "N", "command.show.field.view.choice.--releases": "R",
			"command.show.field.view.choice._": "_",
		}
		l[lang] = map[string]string{key: value}

		var out []Issue

		for _, is := range CheckLocales(m, l, false) {
			if is.Code != CodeLocaleMissingKey {
				out = append(out, is)
			}
		}

		return out
	}

	tests := []struct {
		name  string
		key   string
		value string
		ok    bool
	}{
		{"fine", "command.get.label", "Get {slug}", true},
		{"unknown placeholder", "command.get.label", "Get {nope}", false},
		{"stray brace", "command.get.label", "Get }", false},
		{"item outside list action", "command.get.label", "Get {item.id}", false},
		{"escaped braces", "command.get.label", "Get {{x}}", true},
		{"field in scope", "command.install.confirm", "Install {slug}?", true},
		{"repo placeholder", "item_action.stash-drop.confirm", "Drop {n} of {repo.name}?", true},
		{"label too long", "command.get.label", strings.Repeat("a", 61), false},
		{"label at limit", "command.get.label", strings.Repeat("a", 60), true},
		{"description too long", "description", strings.Repeat("a", 201), false},
		{"keywords ok", "command.show.keywords", "a, b, c", true},
		{"too many keywords", "command.show.keywords", "a,b,c,d,e,f,g,h,i", false},
		{"keyword too long", "command.show.keywords", strings.Repeat("a", 25), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := check("fr", tt.key, tt.value)
			if (len(issues) == 0) != tt.ok {
				t.Errorf("issues = %v, want ok=%v", issues, tt.ok)
			}

			for _, is := range issues {
				if is.Severity != SeverityError || is.Code != CodeLocaleInvalid {
					t.Errorf("a value problem must be a locale_invalid error: %v", is)
				}
			}
		})
	}
}

// TestCatalog checks the lookup order and the cleaning of the catalog.
//
// Parameters:
//   - t: test to report to
func TestCatalog(t *testing.T) {
	c := NewCatalog(map[string]map[string]string{
		"fr": {"a": "bonjour", "esc": "x\x1b[31my"}, "en": {"a": "hello", "b": "only english"},
	})

	tests := []struct{ lang, key, literal, want string }{
		{"fr", "a", "lit", "bonjour"},
		{"fr", "b", "lit", "only english"},
		{"fr", "c", "lit", "lit"},
		{"de", "a", "lit", "hello"},
		{"en", "a", "lit", "hello"},
		{"fr", "esc", "", "x?[31my"},
		{"fr", "zz", "l\nit", "l?it"},
	}

	for _, tt := range tests {
		if got := c.Text(tt.lang, tt.key, tt.literal); got != tt.want {
			t.Errorf("Text(%q, %q) = %q, want %q", tt.lang, tt.key, got, tt.want)
		}
	}

	if got := NewCatalog(nil).Text("fr", "a", "lit"); got != "lit" {
		t.Errorf("empty catalog = %q", got)
	}
}

// TestMergeLocales checks that a file wins over the manifest table for a key defined twice.
//
// Parameters:
//   - t: test to report to
func TestMergeLocales(t *testing.T) {
	m := mustDecode(t, decl("", tool,
		cmdBody+"[i18n.fr]\n\"command.go.label\" = \"depuis le manifeste\"\ndescription = \"d\"\n"))

	got := MergeLocales(m,
		map[string]map[string]string{"fr": {"command.go.label": "depuis le fichier"}})
	if got["fr"]["command.go.label"] != "depuis le fichier" || got["fr"]["description"] != "d" {
		t.Errorf("merge = %v", got)
	}

	if m.I18n["fr"]["command.go.label"] != "depuis le manifeste" {
		t.Error("MergeLocales changed the manifest table")
	}
}
