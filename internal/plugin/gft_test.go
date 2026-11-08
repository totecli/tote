package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// gftLab installs the gft fixture, a fake gft and xdg-open, a repository with a GitHub remote and
// one without, and returns the sandbox with the path of the first repository.
//
// Parameters:
//   - t: test to report to
func gftLab(t *testing.T) (*lab, string) {
	t.Helper()

	l := newLab(t)
	l.useSystemTools()
	l.script("gft", "exit 0\n")
	l.script(OpenerName(runtime.GOOS), "exit 0\n")
	l.install("gft", gftFiles(t))

	return l, l.gitRepo("myrepo", "git@github.com:20syldev/tote.git")
}

// TestGftScenarios replays the table of section 10: what each entry of gft prepares for the
// repository myrepo, whose remote is git@github.com:20syldev/tote.git.
//
// Parameters:
//   - t: test to report to
func TestGftScenarios(t *testing.T) {
	l, repo := gftLab(t)
	l.gitRepo("plain", "")

	r := l.reg(nil)
	gft := filepath.Join(l.bin, "gft")

	item := func(name, id string, values map[string]string) Call {
		return Call{Plugin: "gft", Table: TableItemAction, Entry: id, Repo: name, Values: values}
	}

	tests := []struct {
		name    string
		call    Call
		argv    []string
		dir     string
		display string
	}{
		{
			"release", item("myrepo", "release", nil),
			[]string{gft, "20syldev/tote"},
			repo,
			"gft 20syldev/tote",
		},
		{
			"assets", item("myrepo", "assets", nil),
			[]string{gft, "20syldev/tote", "--assets"},
			repo,
			"gft 20syldev/tote --assets",
		},
		{
			"notes", item("myrepo", "notes", nil),
			[]string{gft, "20syldev/tote", "--notes"},
			repo,
			"gft 20syldev/tote --notes",
		},
		{
			"stash list on a repository without remote", item("plain", "stash-list", nil),
			[]string{gft, "stash", "list"},
			filepath.Join(l.root, "repos", "plain"),
			"gft stash list",
		},
		{
			"stash show takes its default", item("myrepo", "stash-show", nil),
			[]string{gft, "stash", "show", "0"},
			repo, "gft stash show 0",
		},
		{
			"drop stash 2", item("myrepo", "stash-drop", map[string]string{"n": "2"}),
			[]string{gft, "stash", "drop", "2", "--yes"},
			repo, "gft stash drop 2 --yes",
		},
		{
			"clean", item("myrepo", "stash-clean", nil),
			[]string{gft, "stash", "clean", "--yes"},
			repo,
			"gft stash clean --yes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := prepare(t, r, tt.call)

			if strings.Join(p.Argv, "|") != strings.Join(tt.argv, "|") {
				t.Errorf("argv %q want %q", p.Argv, tt.argv)
			}

			if p.Dir != tt.dir || p.Display != tt.display || p.Output != OutputPager {
				t.Errorf("dir %q display %q output %q", p.Dir, p.Display, p.Output)
			}
		})
	}

	t.Run("drop asks, with the repository name", func(t *testing.T) {
		p := prepare(t, r, item("myrepo", "stash-drop", map[string]string{"n": "2"}))

		if !p.Confirm.Required || p.Confirm.Text != "Drop stash 2 of myrepo?" {
			t.Errorf("confirm %+v", p.Confirm)
		}

		c := item("myrepo", "stash-drop", map[string]string{"n": "2"})
		c.Lang = "fr"

		if got := prepare(t, r, c).Confirm.Text; !strings.Contains(got, "stash 2 de myrepo") ||
			strings.Contains(got, "Drop") {
			t.Errorf("fr confirm %q", got)
		}
	})

	t.Run("the release entries are unavailable without a GitHub remote", func(t *testing.T) {
		for _, id := range []string{"release", "assets", "notes"} {
			e := refusal(t, r, item("plain", id, nil))

			if e.Code != CodeUnavailable || e.Args[0] != "repo.slug" {
				t.Errorf("%s: %s %v", id, e.Code, e.Args)
			}
		}
	})

	t.Run("a repository that is not saved", func(t *testing.T) {
		if e := refusal(t, r, item("ghost", "release", nil)); e.Code != CodeUnavailable ||
			e.Args[0] != "repo" {
			t.Errorf("got %v", e)
		}

		if e := refusal(t, r, item("", "release", nil)); e.Code != CodeUnavailable {
			t.Errorf("got %v", e)
		}
	})

	t.Run("a stash number outside its pattern", func(t *testing.T) {
		for _, n := range []string{"", "12345", "-1", "1 --force", "a", "1\n"} {
			e := refusal(t, r, item("myrepo", "stash-drop", map[string]string{"n": n}))

			if e.Code != CodeInputInvalid {
				t.Errorf("n=%q: %s %v", n, e.Code, e.Args)
			}
		}
	})
}

// TestGftCommands checks the palette commands of gft in the current repository: the slug defaults
// to the repository the working directory is in, an empty optional tag is dropped, a tag that
// starts with a dash is refused, and the terminal command carries its pause policy.
//
// Parameters:
//   - t: test to report to
func TestGftCommands(t *testing.T) {
	l, repo := gftLab(t)
	sub := filepath.Join(repo, "internal", "x")

	must(t, os.MkdirAll(sub, 0o700))

	r := l.reg(func(o *Options) { o.Cwd = sub })
	gft := filepath.Join(l.bin, "gft")

	cmd := func(id string, values map[string]string) Call {
		return Call{Plugin: "gft", Table: TableCommand, Entry: id, Values: values}
	}

	t.Run("show with a view, no tag", func(t *testing.T) {
		p := prepare(t, r, cmd("show", map[string]string{"view": "--assets"}))

		if want := []string{gft, "20syldev/tote", "--assets"}; strings.Join(p.Argv, "|") !=
			strings.Join(want, "|") {
			t.Errorf("argv %q", p.Argv)
		}

		if p.Dir != sub {
			t.Errorf("dir %q want the working directory", p.Dir)
		}
	})

	t.Run("show with a tag", func(t *testing.T) {
		p := prepare(t, r, cmd("show", map[string]string{"tag": "v1.2.3"}))

		if got := strings.Join(p.Argv[1:], " "); got != "20syldev/tote v1.2.3" {
			t.Errorf("argv %q", got)
		}
	})

	t.Run("a tag that starts with a dash", func(t *testing.T) {
		e := refusal(t, r, cmd("show", map[string]string{"tag": "-rf"}))

		if e.Code != CodeDashRefused || e.Args[0] != "tag" {
			t.Errorf("got %s %v", e.Code, e.Args)
		}
	})

	t.Run("a slug typed by hand", func(t *testing.T) {
		p := prepare(t, r, cmd("show", map[string]string{"slug": "cli/cli"}))

		if p.Argv[1] != "cli/cli" {
			t.Errorf("argv %q", p.Argv)
		}

		e := refusal(t, r, cmd("show", map[string]string{"slug": "cli cli"}))
		if e.Code != CodeInputInvalid {
			t.Errorf("got %v", e)
		}
	})

	t.Run("get in the terminal", func(t *testing.T) {
		p := prepare(t, r, cmd("get", map[string]string{"pattern": "linux-amd64", "verify": "true"}))

		want := []string{gft, "20syldev/tote", "--get", "linux-amd64", "--checksum"}
		if strings.Join(p.Argv, "|") != strings.Join(want, "|") {
			t.Errorf("argv %q", p.Argv)
		}

		if !p.IsTerminal() || p.Pause != PauseAlways || p.Timeout != 0 {
			t.Errorf("output %q pause %q timeout %v", p.Output, p.Pause, p.Timeout)
		}

		if _, err := os.Stat(p.Dir); err != nil {
			t.Error(err)
		}
	})

	t.Run("install asks", func(t *testing.T) {
		p := prepare(t, r, cmd("install", nil))

		if p.Confirm.Text != "Download and install 20syldev/tote?" || !p.IsTerminal() {
			t.Errorf("confirm %+v", p.Confirm)
		}
	})

	t.Run("open shows the url and runs no program", func(t *testing.T) {
		p := prepare(t, r, cmd("open", nil))

		if p.OpenURL != "https://github.com/20syldev/tote/releases" || len(p.Argv) != 0 ||
			!p.IsOpen() || p.opener == "" {
			t.Errorf("open %q argv %q opener %q", p.OpenURL, p.Argv, p.opener)
		}
	})

	t.Run("open with a hostile slug", func(t *testing.T) {
		for _, slug := range []string{"evil.com@x/y", "a/b?x#", "a b/c"} {
			if e := refusal(t, r, cmd("open", map[string]string{"slug": slug})); e.Code != CodeInputInvalid {
				t.Errorf("slug %q: %v", slug, e)
			}
		}
	})

	t.Run("outside any repository the slug is empty", func(t *testing.T) {
		far := l.reg(nil)

		e := refusal(t, far, cmd("show", nil))
		if e.Code != CodeInputInvalid || e.Args[0] != "slug" {
			t.Errorf("got %s %v", e.Code, e.Args)
		}
	})
}

// TestGftMissingProgram is the emptied PATH case: gft is not installed, the plugin is in the
// missing-dependency state, and the refusal says so before anything is looked at.
//
// Parameters:
//   - t: test to report to
func TestGftMissingProgram(t *testing.T) {
	l, _ := gftLab(t)
	l.env["PATH"] = filepath.Join(l.root, "empty")

	e := refusal(t, l.reg(nil), Call{
		Plugin: "gft", Table: TableItemAction, Entry: "release",
		Repo: "myrepo",
	})

	if e.Code != CodeNotRunnable || e.Args[1] != string(StateMissingDependency) {
		t.Errorf("got %s %v", e.Code, e.Args)
	}
}

// TestOpenDefenses checks the URL of an open contribution on its own.
//
// Parameters:
//   - t: test to report to
func TestOpenDefenses(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		host string
		ok   bool
	}{
		{"https", "https://github.com/o/r/releases", "github.com", true},
		{"no literal host", "https://x.dev/y", "", true},
		{"port", "https://github.com:8443/x", "", true},
		{"http", "http://github.com/x", "", false},
		{"javascript", "javascript:alert(1)", "", false},
		{"file", "file:///etc/passwd", "", false},
		{"userinfo", "https://user@github.com/x", "", false},
		{"userinfo with password", "https://user:pw@github.com/x", "", false},
		{"userinfo disguised", "https://github.com@evil.dev/x", "github.com", false},
		{"host moved by a value", "https://evil.dev/x", "github.com", false},
		{"host case", "https://GitHub.com/x", "github.com", true},
		{"backslash", `https://github.com\@evil.dev/x`, "", false},
		{"space", "https://github.com/a b", "", false},
		{"newline", "https://github.com/a\nb", "", false},
		{"empty host", "https:///x", "", false},
		{"dash host", "https://-x.dev/", "", false},
		{"non ascii", "https://g\u00efthub.com/", "", false},
		{"too long", "https://github.com/" + strings.Repeat("a", 2040), "", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckOpenURL(tt.raw, tt.host)

			if (err == nil) != tt.ok || (tt.ok && got != tt.raw) {
				t.Errorf("got %q, %v want ok=%v", got, err, tt.ok)
			}

			if err != nil && err.Code != CodeOpenInvalid {
				t.Errorf("code %s", err.Code)
			}
		})
	}
}

// TestLiteralHost checks how the host is read from an open template.
//
// Parameters:
//   - t: test to report to
func TestLiteralHost(t *testing.T) {
	tests := []struct{ src, want string }{
		{"https://github.com/{slug}/releases", "github.com"},
		{"https://github.com", "github.com"},
		{"https://github.com?x", "github.com"},
		{"{item.data.url}", ""},
		{"https://{host}/x", ""},
		{"https://host{x}", ""},
		{"http://github.com/x", ""},
	}

	for _, tt := range tests {
		if got := literalHost(tpl(t, tt.src)); got != tt.want {
			t.Errorf("%q: got %q want %q", tt.src, got, tt.want)
		}
	}
}
