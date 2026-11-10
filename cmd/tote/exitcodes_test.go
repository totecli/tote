package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/plugin"
)

// TestPluginExitCodes reaches every exit code of the plugin verbs from the command line, one case
// per code, and checks that the JSON error object carries the same code as the process.
//
// Parameters:
//   - t: test to report to
func TestPluginExitCodes(t *testing.T) {
	e := newPluginEnv(t)

	data := archive(t, manifestOf("demo", "1.0.0", "exec:echo"))
	url := e.release("acme", "demo", "1.0.0", "demo.tar.gz", data)
	token := e.grantOf(url, "--sha256", sum(data))

	e.hub.handle(hostRepo, "/acme/private/releases/download/1.0.0/p.tar.gz",
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })

	tests := []struct {
		name string
		args []string
		exit int
	}{
		{"success", []string{"plugin", "list"}, 0},
		{"failure", []string{"plugin", "info", "nope"}, 1},
		{"usage", []string{"plugin", "frobnicate"}, 2},
		{"auth", []string{
			"plugin", "install",
			"https://github.com/acme/private/releases/download/1.0.0/p.tar.gz", "--sha256",
			sum([]byte("x")),
		}, 3},
		{"policy", []string{
			"plugin", "install", url, "--sha256", sum(data), "--grant",
			"exec:echo@000000000000",
		}, 4},
		{"integrity", []string{
			"plugin", "install", url, "--sha256", sum([]byte("other")),
			"--grant", token,
		}, 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rc, _, _ := e.do(tc.args...); rc != tc.exit {
				t.Errorf("exit %d, want %d", rc, tc.exit)
			}

			if tc.exit == 0 {
				return
			}

			rc, out, errs := e.do(append(tc.args, "--json")...)
			if rc != tc.exit || errs != "" || !strings.Contains(out, `"ok": false`) ||
				!strings.Contains(out, `"exit": `+string(rune('0'+tc.exit))) {
				t.Errorf("--json: exit %d, stdout %q, stderr %q", rc, out, errs)
			}
		})
	}
}

// TestPluginExitOfEveryCode checks that no code of the plugin package maps to an exit code
// outside the documented range: 1 to 5, or 127 for a program that cannot start.
//
// Parameters:
//   - t: test to report to
func TestPluginExitOfEveryCode(t *testing.T) {
	for _, code := range allCodes(t) {
		if rc := plugin.ExitFor(code, nil); (rc < 1 || rc > 5) && rc != plugin.ExitCannotStart {
			t.Errorf("code %q maps to exit %d", code, rc)
		}
	}
}

// keyPrefixes are the families of catalog keys that belong to the plugin verbs and screens.
var keyPrefixes = []string{"plugin_", "consent_", "cli_plugin_", "usage_plugin_"}

// TestPluginKeysAreUsed fails on a message of the plugin families that nothing can reach: it must
// be named by a literal in the sources, belong to a family a source builds the key of, or be one
// of the verbatim texts of the security appendix, which a test guards. The appendix texts that no
// source names yet (the buttons and the lines of the capability list that only the screens of the
// terminal interface show) are kept for that step: the appendix test makes sure they stay as the
// specification wrote them until a screen uses them.
//
// Parameters:
//   - t: test to report to
func TestPluginKeysAreUsed(t *testing.T) {
	var sources strings.Builder

	for _, dir := range []string{
		".", filepath.Join("..", "..", "internal", "plugin"),
		filepath.Join("..", "..", "internal", "i18n"),
	} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}

		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}

			b, rerr := os.ReadFile(name)
			if rerr != nil {
				t.Fatal(rerr)
			}

			sources.Write(b)
		}
	}

	src := sources.String()

	appendix, err := os.ReadFile(filepath.Join("..", "..", "internal", "i18n", "appendix_test.go"))
	if err != nil {
		t.Fatal(err)
	}

	// A family is a literal ending with an underscore, to which the code adds a code or a name.
	families := regexp.MustCompile(`"((?:plugin|consent|cli_plugin|usage_plugin)_[a-z_]*_)"`)

	var prefixes []string

	for _, m := range families.FindAllStringSubmatch(src, -1) {
		prefixes = append(prefixes, m[1])
	}

	catalog, err := os.ReadFile(filepath.Join("..", "..", "internal", "i18n", "en.toml"))
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range strings.Split(string(catalog), "\n") {
		key, _, ok := strings.Cut(line, " = ")
		if !ok || !hasAnyPrefix(key, keyPrefixes) {
			continue
		}

		if strings.Contains(src, `"`+key+`"`) || strings.Contains(string(appendix), `"`+key+`"`) {
			continue
		}

		if hasAnyPrefix(key, prefixes) {
			continue
		}

		t.Errorf("the key %s is defined and used nowhere", key)
	}
}

// hasAnyPrefix reports whether s starts with one of the prefixes.
//
// Parameters:
//   - s: string to test
//   - prefixes: starts to look for
func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}

	return false
}
