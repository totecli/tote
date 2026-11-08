package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestSecReviewConfigLayoutsFailClosed pins the kill switch against the layouts of config.toml: a
// plugin that config.toml switches off is disabled whatever the spelling (table, dotted key, inline
// table, quoted key, CRLF, byte order mark), and a file that cannot be parsed makes the plugin
// broken, never enabled. A switch that is there but cannot be read (not a boolean, not a table)
// turns the plugin off, a key the loader does not know is ignored with a warning, and a table for
// an id that no plugin can have names no plugin.
//
// Parameters:
//   - t: test to report to
func TestSecReviewConfigLayoutsFailClosed(t *testing.T) {
	cases := map[string]struct {
		cfg  string
		want State
	}{
		"table":       {"[plugins.demo]\nenabled = false\n", StateDisabled},
		"dotted":      {"[plugins]\ndemo.enabled = false\n", StateDisabled},
		"dotted top":  {"plugins.demo.enabled = false\n", StateDisabled},
		"inline":      {"plugins = { demo = { enabled = false } }\n", StateDisabled},
		"inline2":     {"[plugins]\ndemo = { enabled = false }\n", StateDisabled},
		"quoted":      {"[plugins.\"demo\"]\nenabled = false\n", StateDisabled},
		"crlf":        {"[plugins.demo]\r\nenabled = false\r\n", StateDisabled},
		"bom":         {"\xef\xbb\xbf[plugins.demo]\nenabled = false\n", StateDisabled},
		"string":      {"[plugins.demo]\nenabled = \"false\"\n", StateDisabled},
		"duplicate":   {"[plugins.demo]\nenabled = false\nenabled = true\n", StateBroken},
		"array":       {"[[plugins.demo]]\nenabled = false\n", StateDisabled},
		"unknown":     {"[plugins.demo]\nenabled = false\nfoo = 1\n", StateDisabled},
		"unknown top": {"surprise = 1\n[plugins.demo]\nenabled = false\n", StateDisabled},
		"schema 2":    {"schema = 2\n[plugins.demo]\nenabled = false\n", StateBroken},
		"upper id":    {"[plugins.DEMO]\nenabled = false\n", StateEnabled},
	}

	for name, tc := range cases {
		l := newLab(t)
		l.useSystemTools()
		l.script("tool", "exit 0\n")
		l.plug("demo", []string{"exec:tool"}, runBody)

		if err := os.MkdirAll(filepath.Dir(l.paths.ConfigFile()), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(l.paths.ConfigFile(), []byte(tc.cfg), 0o600); err != nil {
			t.Fatal(err)
		}

		var got State

		for _, p := range l.reg(nil).Load().Plugins() {
			got = p.State
		}

		if got != tc.want {
			t.Errorf("%s: state %q, want %q", name, got, tc.want)
		}
	}
}

// TestSecReviewLockForgeryFailsClosed pins the lock: a record that is duplicated, truncated,
// retyped or replaced by an inline array never leaves a plugin enabled that the file marks as
// disabled, and a lock that does not parse makes the plugin broken.
//
// Parameters:
//   - t: test to report to
func TestSecReviewLockForgeryFailsClosed(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()
	l.script("tool", "exit 0\n")
	l.plug("demo", []string{"exec:tool"}, runBody)

	orig, err := os.ReadFile(l.paths.LockFile())
	if err != nil {
		t.Fatal(err)
	}

	text := string(orig)
	disabled := strings.Replace(text, "disabled = false", "disabled = true", 1)
	record := func(s string) string { return s[strings.Index(s, "[[plugin]]"):] }

	cases := map[string]struct {
		lock string
		want State
	}{
		"original":        {text, StateEnabled},
		"disabled":        {disabled, StateDisabled},
		"duplicate first": {disabled + "\n" + record(text), StateBroken},
		"duplicate last":  {text + "\n" + record(disabled), StateBroken},
		"truncated":       {text[:len(text)/2], StateBroken},
		"empty":           {"", StateBroken},
		"string flag": {
			strings.Replace(text, "disabled = false", "disabled = \"true\"", 1),
			StateBroken,
		},
		"int flag":     {strings.Replace(text, "disabled = false", "disabled = 1", 1), StateBroken},
		"inline array": {"schema = 1\nplugin = [ {id = \"demo\", disabled = true} ]\n", StateBroken},
	}

	for name, tc := range cases {
		if err := os.WriteFile(l.paths.LockFile(), []byte(tc.lock), 0o600); err != nil {
			t.Fatal(err)
		}

		var got State

		for _, p := range l.reg(nil).Load().Plugins() {
			got = p.State
		}

		if got != tc.want {
			t.Errorf("%s: state %q, want %q", name, got, tc.want)
		}
	}
}

// TestSecReviewHostileSources pins the refusals of the copy of a local source: a FIFO, a name
// outside the grammar, a manifest over the limit, a NUL byte and an undeclared file are all
// refused at once, and none of them stalls the install.
//
// Parameters:
//   - t: test to report to
func TestSecReviewHostileSources(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()

	r := l.reg(nil)
	good := manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", ""))

	fifo := l.srcDir("fifo", map[string]string{"plugin.toml": good})
	if err := syscall.Mkfifo(filepath.Join(fifo, "README"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"fifo": fifo,
		"homoglyph name": l.srcDir("uni", map[string]string{
			"plugin.toml": good,
			"REAME\u0430": "y",
		}),
		"manifest over the limit": l.srcDir("big", map[string]string{"plugin.toml": good +
			"# " + strings.Repeat("x", 300<<10) + "\n"}),
		"NUL and invalid UTF-8": l.srcDir("nul", map[string]string{"plugin.toml": good +
			"# \x00\xff\n"}),
		"hidden file": l.srcDir("hidden", map[string]string{"plugin.toml": good, ".hidden/x": "y"}),
	}

	for name, dir := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		start := time.Now()

		st, err := r.Plan(ctx, InstallRequest{Source: dir})

		cancel()

		if st != nil {
			_ = st.Abort()
		}

		if err == nil {
			t.Errorf("%s: Plan accepted the source", name)
		}

		if time.Since(start) > 2*time.Second {
			t.Errorf("%s: Plan took %v", name, time.Since(start))
		}
	}
}

// TestSecReviewLocaleIngestion pins that a locale value with an escape sequence, a bidi override,
// a C1 control, a line feed or a tag character is refused as a whole, not cleaned and kept.
//
// Parameters:
//   - t: test to report to
func TestSecReviewLocaleIngestion(t *testing.T) {
	for name, value := range map[string]string{
		"osc":  `\u001b]52;c;AAAA\u0007x`,
		"bidi": `a\u202eb`,
		"c1":   `a\u009bb`,
		"nl":   `a\nb`,
		"tag":  `a\U000E0041b`,
	} {
		if _, issues := ParseLocale([]byte("title = \"" + value + "\"\n")); len(issues) == 0 {
			t.Errorf("%s: ParseLocale accepted the value", name)
		}
	}
}
