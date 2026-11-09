package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/i18n"
)

// sandbox points the process environment at a throwaway directory.
//
// Parameters:
//   - t: running test
//
// Returns the root of the sandbox.
func sandbox(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("TOTE_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("TOTE_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("TOTE_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("TOTE_CACHE_DIR", filepath.Join(root, "cache"))
	t.Setenv("TOTE_LANG", "en")
	t.Setenv("TOTE_HANDOFF", "")
	t.Setenv("XDG_RUNTIME_DIR", "")

	return root
}

// runCase is one table row of TestRun: arguments and the expected exit code and output parts.
type runCase struct {
	name   string
	args   []string
	code   int
	stdout string
	stderr string
}

// checkRun runs one case through run and compares the exit code and the output.
//
// Parameters:
//   - t: test to report to
//   - c: case to run
func checkRun(t *testing.T, c runCase) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	if code := run(c.args, &stdout, &stderr); code != c.code {
		t.Fatalf("exit code = %d, want %d", code, c.code)
	}
	if !strings.Contains(stdout.String(), c.stdout) {
		t.Errorf("stdout = %q, want it to contain %q", stdout.String(), c.stdout)
	}
	if !strings.Contains(stderr.String(), c.stderr) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), c.stderr)
	}
}

// TestRun checks the exit code and the output of each top-level command through run.
//
// Parameters:
//   - t: test to report to
func TestRun(t *testing.T) {
	sandbox(t)

	cases := []runCase{
		{"no args", nil, 0, "Usage:", ""},
		{"help", []string{"--help"}, 0, "Usage:", ""},
		{"version", []string{"--version"}, 0, "tote dev", ""},
		{"unknown", []string{"nope"}, 2, "", `unknown command "nope"`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { checkRun(t, c) })
	}
}

// TestRunEndToEnd drives a whole session through run in a sandbox: add, list, open, remove.
//
// Parameters:
//   - t: test to report to
func TestRunEndToEnd(t *testing.T) {
	root := sandbox(t)

	steps := []struct {
		args []string
		code int
		out  string
	}{
		{
			[]string{"co", "add", "web1", "--target", "10.0.0.1", "--alias", "w"},
			0,
			"added connection \"web1\"\n",
		},
		{[]string{"alias", "add", "ll", "ls -la"}, 0, "added alias \"ll\"\n"},
		{[]string{"repos", "add", "proj", "~/code/proj"}, 0, "added repository \"proj\"\n"},
		{[]string{"funcs", "add", "hello", "--script", "/s/hello.sh"}, 0, "added function \"hello\"\n"},
		{[]string{"co", "-l"}, 0, "web1\t\t10.0.0.1\t\tw\t\t\n"},
		{[]string{"alias", "-l", "--format", "tsv"}, 0, "ll\tls -la\t\n"},
		{[]string{"repos", "open", "proj"}, 0, filepath.Join(root, "home", "code", "proj") + "\n"},
		{[]string{"config", "path"}, 0, filepath.Join(root, "config") + "\n"},
		{[]string{"co", "rm", "web1", "--yes"}, 0, "removed connection \"web1\"\n"},
		{[]string{"co", "-l"}, 0, ""},
	}

	for _, s := range steps {
		var stdout, stderr bytes.Buffer

		if code := run(s.args, &stdout, &stderr); code != s.code || stdout.String() != s.out {
			t.Errorf("%v: code %d, stdout %q, stderr %q, want %d %q",
				s.args, code, stdout.String(), stderr.String(), s.code, s.out)
		}
	}

	var stdout, stderr bytes.Buffer

	if code := run([]string{"init", "bash"}, &stdout, &stderr); code != 0 ||
		!strings.Contains(stdout.String(), "alias ll='ls -la'") {
		t.Errorf("init bash: code %d, stderr %q", code, stderr.String())
	}

	info, err := os.Stat(filepath.Join(root, "config", "aliases.toml"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("aliases.toml: %v %v, want mode 0600", info, err)
	}
}

// TestCatalogKeysExist checks that every catalog key used by the command line is defined.
//
// Parameters:
//   - t: test to report to
func TestCatalogKeysExist(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	known := map[string]bool{}
	for _, k := range i18n.Keys() {
		known[k] = true
	}

	re := regexp.MustCompile(`"((?:cli|help|usage|header|kind|arg)_[a-z_]+)"`)

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}

		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}

		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			if !known[m[1]] {
				t.Errorf("%s uses the undefined key %s", f, m[1])
			}
		}
	}
}

// TestModuleVersion checks that a pseudo-version or a dirty build keeps the literal dev and that
// a tagged version is reported without its v.
//
// Parameters:
//   - t: test to report to
func TestModuleVersion(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "dev"},
		{"(devel)", "dev"},
		{"v0.0.0-20261109205113-159407959e94", "dev"},
		{"v1.2.4-0.20261109205113-159407959e94", "dev"},
		{"v1.2.3-pre.0.20261109205113-159407959e94", "dev"},
		{"v1.2.3+dirty", "dev"},
		{"v1.0.0", "1.0.0"},
		{"1.0.0", "1.0.0"},
		{"v1.0.0-rc.1", "1.0.0-rc.1"},
		{"v1.0.0-20261109205113", "1.0.0-20261109205113"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := moduleVersion(tt.in); got != tt.want {
				t.Errorf("moduleVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
