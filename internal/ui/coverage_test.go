package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/i18n"
)

// TestReviewClipboardTmuxBeforeScreen pins the order of the rules of clipboardUsable: tmux
// announces itself with TERM=screen or screen-256color in many setups, and the option
// set-clipboard decides there, not the prefix of TERM. The table of TestClipboardUsable only
// uses tmux-256color, so a mutant that tests the prefix first survives it.
//
// Parameters:
//   - t: test to report to
func TestReviewClipboardTmuxBeforeScreen(t *testing.T) {
	for _, term := range []string{"screen", "screen-256color"} {
		env := map[string]string{"TERM": term, "TMUX": "/tmp/x,1,0"}
		get := func(k string) string { return env[k] }

		if !clipboardUsable(get, func() string { return "on" }) {
			t.Errorf("TERM=%s inside tmux with set-clipboard on: not usable", term)
		}

		if clipboardUsable(get, func() string { return "off" }) {
			t.Errorf("TERM=%s inside tmux with set-clipboard off: usable", term)
		}

		delete(env, "TMUX")

		if clipboardUsable(get, func() string { return "on" }) {
			t.Errorf("TERM=%s outside tmux: usable", term)
		}
	}
}

// TestReviewLanguageAutoFollowsLocale pins what the interface speaks after the language setting
// is reset to its default: the language that the locale of the environment gives, not the
// default of the catalog. The live switch is checked by TestConfigLanguageLive only in a
// locale that is already English, where "the setting" and "the locale" cannot be told apart.
//
// Parameters:
//   - t: test to report to
func TestReviewLanguageAutoFollowsLocale(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.ConfigFile(), "schema = 1\n")

	s = s.with(func(o *Options) {
		o.Start = StartConfig
		o.Printer = i18n.New("fr")
		o.Settings = config.DefaultSettings()
		o.Environ = append([]string{"LANG=fr_FR.UTF-8"}, o.Environ[2:]...)
	})

	g := newRig(t, s)

	if got := g.r.e.Tr.Lang(); got != "fr" {
		t.Fatalf("the interface starts in %q, want fr", got)
	}

	selectRow(g, "lang")
	g.press("enter", "down", "enter")
	g.await("the switch to English", func() bool { return g.r.e.Tr.Lang() == "en" })

	if got := readConfig(t, s); !strings.Contains(got, `lang = "en"`) {
		t.Errorf("config.toml lacks the language:\n%s", got)
	}

	g.press("d", "y")
	g.await("the reset", func() bool { return !strings.Contains(readConfig(t, s), "lang") })

	if got := g.r.e.Tr.Lang(); got != "fr" {
		t.Errorf("after the reset the interface speaks %q, the locale says fr", got)
	}
}

// TestReviewTmuxClipboard runs the real query of the tmux option against a fake tmux put first
// in the PATH. It was the only function of copy.go that no test reached: the value is trimmed,
// the arguments are the ones that read a server option, and a tmux that fails or that does not
// answer within clipboardProbeTimeout gives the empty string, which refuses the copy, without
// holding the interface.
//
// Parameters:
//   - t: test to report to
func TestReviewTmuxClipboard(t *testing.T) {
	// quick is far above what a script takes to start and below the sleep of the one that does not
	// answer, so that a busy machine does not fail the test and a missing timeout does.
	const quick = 10 * time.Second

	tests := []struct {
		name  string
		body  string
		want  string
		limit time.Duration
	}{
		{
			"on", "#!/bin/sh\n[ \"$*\" = \"show-options -sv set-clipboard\" ] && echo on\n", "on",
			quick,
		},
		{"padded", "#!/bin/sh\nprintf '  external \\n'\n", "external", quick},
		{"fails", "#!/bin/sh\necho on\nexit 1\n", "", quick},
		{"no answer", "#!/bin/sh\nexec sleep 20\n", "", clipboardProbeTimeout + quick},
	}

	path := os.Getenv("PATH")

	for _, tt := range tests {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(tt.body), 0o700); err != nil {
			t.Fatal(err)
		}

		t.Setenv("PATH", dir+string(os.PathListSeparator)+path)

		start := time.Now()
		got := tmuxClipboard()

		if got != tt.want {
			t.Errorf("%s: tmuxClipboard() = %q, want %q", tt.name, got, tt.want)
		}

		if took := time.Since(start); took > tt.limit {
			t.Errorf("%s: the query took %v, want at most %v", tt.name, took, tt.limit)
		}
	}
}

// TestReviewSettingsLayoutRefusal changes a setting whose table config.toml writes as an inline
// table or with dotted keys, which the loader accepts and PatchTable refuses with ErrLayout. The
// screen has to say so in the words of the catalog, keep the file as it was and go on with the
// settings it had; the branch of writeSetting that maps the error was reached by no test.
//
// Parameters:
//   - t: test to report to
func TestReviewSettingsLayoutRefusal(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{"inline table", "schema = 1\nui = { theme = \"dark\" }\n"},
		{"dotted keys", "schema = 1\nui.theme = \"dark\"\n"},
	}

	for _, tt := range tests {
		g, s := configRig(t, tt.file)

		selectRow(g, "compact")
		g.press("enter", "down", "enter")
		g.await("the refusal", func() bool { return g.toastText() != "" })

		if want := g.r.e.t("ui_set_layout"); g.toastText() != want {
			t.Errorf("%s: toast %q, want %q", tt.name, g.toastText(), want)
		}

		if !g.r.toast.err {
			t.Errorf("%s: the refusal is not shown as an error", tt.name)
		}

		if got := readConfig(t, s); got != tt.file {
			t.Errorf("%s: a refused write changed the file:\n%s", tt.name, got)
		}

		if got := g.r.e.Settings.UI.Compact; got != config.DefaultSettings().UI.Compact {
			t.Errorf("%s: the running settings took the refused value %q", tt.name, got)
		}
	}
}
