package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// constFamilies maps the name prefix of the string constants of the plugin package to the prefix
// of the catalogue key that must exist for each of their values. A constant of one family has a
// message in both languages, so that a new code cannot reach the command line without words.
var constFamilies = []struct{ names, keys string }{
	{"Code", PrefixError},
	{"Reason", PrefixReason},
	{"Archive", "plugin_archive_"},
	{"Esc", "plugin_esc_"},
	{"Fail", "plugin_fail_"},
	{"Left", "plugin_left_"},
	{"Limit", "plugin_limit_"},
	{"Orphan", "plugin_orphan_"},
	{"Reject", "plugin_reject_"},
	{"Unsafe", "plugin_unsafe_"},
	{"Why", "plugin_why_"},
	{"Source", "plugin_source_"},
}

// pluginConsts reads the string constants of the plugin package from its source files.
//
// Parameters:
//   - t: test to report to
//
// Returns a map from constant name to value, for the constants whose value is a string literal.
func pluginConsts(t *testing.T) map[string]string {
	t.Helper()

	files, err := filepath.Glob("../plugin/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("cannot list the plugin package: %v", err)
	}

	out := map[string]string{}

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, perr := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if perr != nil {
			t.Fatalf("%s: %v", name, perr)
		}

		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}

			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)

				for i, id := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}

					lit, isLit := vs.Values[i].(*ast.BasicLit)
					if !isLit || lit.Kind != token.STRING {
						continue
					}

					if val, uerr := strconv.Unquote(lit.Value); uerr == nil {
						out[id.Name] = val
					}
				}
			}
		}
	}

	return out
}

// TestPluginConstFamilies checks that every code of the plugin package has a message in both
// languages. The check reads the constants of the package, so adding a code without its words
// fails here.
//
// Parameters:
//   - t: test to report to
func TestPluginConstFamilies(t *testing.T) {
	en, fr := catalogs(t)
	consts := pluginConsts(t)

	checked := 0

	for _, name := range slices.Sorted(maps.Keys(consts)) {
		for _, fam := range constFamilies {
			rest, ok := strings.CutPrefix(name, fam.names)
			if !ok || rest == "" || rest[0] < 'A' || rest[0] > 'Z' {
				continue
			}

			key := fam.keys + CodeKey(consts[name])
			checked++

			if _, has := en[key]; !has {
				t.Errorf("%s = %q: key %s is missing in en.toml", name, consts[name], key)
			}

			if _, has := fr[key]; !has {
				t.Errorf("%s = %q: key %s is missing in fr.toml", name, consts[name], key)
			}
		}
	}

	if checked < 100 {
		t.Errorf("only %d codes checked, the constant reader is probably broken", checked)
	}
}

// TestPluginCodeVerbs checks that the messages of the stable codes only use %s and %q, which is
// what the renderer of a code fills in, and the same number of them in both languages.
//
// Parameters:
//   - t: test to report to
func TestPluginCodeVerbs(t *testing.T) {
	en, fr := catalogs(t)

	for key, msg := range en {
		if !strings.HasPrefix(key, PrefixError) && !strings.HasPrefix(key, PrefixReason) {
			continue
		}

		for i := 0; i+1 < len(msg); i++ {
			if msg[i] == '%' && msg[i+1] != 's' && msg[i+1] != 'q' {
				t.Errorf("%s: verb %%%c is not %%s or %%q in %q", key, msg[i+1], msg)
			}
		}

		if verbCount(msg) != verbCount(fr[key]) {
			t.Errorf("%s: %d verbs in English, %d in French", key, verbCount(msg),
				verbCount(fr[key]))
		}
	}
}

// TestPluginStatesHaveWords checks the names of the seven states and of the available plugins.
//
// Parameters:
//   - t: test to report to
func TestPluginStatesHaveWords(t *testing.T) {
	en, _ := catalogs(t)

	for _, state := range []string{
		"enabled", "disabled", "broken", "incompatible", "needs-consent", "quarantined",
		"missing-dependency", "available",
	} {
		key := PrefixState + CodeKey(state)
		if en[key] == "" {
			t.Errorf("state %s has no key %s", state, key)
		}

		fix := "plugin_fix_" + CodeKey(state)
		if state != "enabled" && state != "available" && en[fix] == "" {
			t.Errorf("state %s has no key %s", state, fix)
		}
	}
}

// TestCodeRenderer covers the rendering of a stable code: the key of a dashed code, the padding of
// missing arguments, the cutting of extra ones and the fallback for an unknown code.
//
// Parameters:
//   - t: test to report to
func TestCodeRenderer(t *testing.T) {
	en, fr := New("en"), New("fr")

	cases := []struct {
		name   string
		p      *Printer
		prefix string
		code   string
		args   []string
		want   string
		ok     bool
	}{
		{
			"english", en, PrefixError, "hash_mismatch",
			[]string{"aa", "bb"},
			"checksum mismatch: expected aa, got bb. Nothing was installed.", true,
		},
		{
			"missing arguments are empty", en, PrefixError, "hash_mismatch",
			[]string{"aa"},
			"checksum mismatch: expected aa, got . Nothing was installed.", true,
		},
		{
			"extra arguments are dropped", en, PrefixError, "busy",
			[]string{"x", "y"},
			"another tote is changing plugins, try again in a moment", true,
		},
		{
			"quoted", en, PrefixError, "not_installed",
			[]string{"demo"},
			"plugin \"demo\" is not installed", true,
		},
		{"dashed code", en, PrefixState, "needs-consent", nil, "consent needed", true},
		{
			"dotted code", en, PrefixError, "argv.shell",
			[]string{"bash"},
			"bash is a shell or a launcher: a command may not run it", true,
		},
		{
			"french", fr, PrefixError, "busy", nil,
			"un autre tote modifie les plugins, r\u00e9essayez dans un instant", true,
		},
		{"unknown code", en, PrefixError, "no_such_code", []string{"x"}, "no_such_code", false},
		{"reason", en, PrefixReason, "lock_off", nil, "turned off by tote plugin disable", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.p.Code(tc.prefix, tc.code, tc.args)
			if got != tc.want || ok != tc.ok {
				t.Errorf("Code(%q, %q, %q) = %q, %v; want %q, %v",
					tc.prefix, tc.code, tc.args, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestHas covers the lookup of a key in the language of the Printer and in English.
//
// Parameters:
//   - t: test to report to
func TestHas(t *testing.T) {
	if !New("fr").Has("plugin_err_busy") || New("en").Has("plugin_err_nope") {
		t.Error("Has does not follow the catalogue")
	}
}
