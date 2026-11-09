package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/ui"
	"github.com/totecli/tote/internal/update"
)

// scenario is one broken (or healthy) installation and what the check of one id must say.
//
// setup breaks the healthy fixture, id names the check, level and key are its expected outcome
// and the message key, fix is the expected key of the hint when not empty, and has lists texts
// that the finding must contain, in its message, hint or details.
type scenario struct {
	name  string
	setup func(f *fixture)
	id    string
	level level
	key   string
	fix   string
	has   []string
}

// notRoot skips a test that needs the mode bits to keep the user out.
//
// Parameters:
//   - t: running test
func notRoot(t *testing.T) {
	t.Helper()

	if os.Getuid() == 0 {
		t.Skip("the mode bits do not apply to root")
	}
}

// collectKeys adds every catalog key of a message and of the messages inside it to a set.
//
// Parameters:
//   - set: keys seen so far
//   - m: message to walk
func collectKeys(set map[string]bool, m msg) {
	if m.Key == "" {
		return
	}

	set[m.Key] = true

	for _, a := range m.Args {
		if n, ok := a.(msg); ok {
			collectKeys(set, n)
		}
	}
}

// receipt returns the JSON of an install receipt.
//
// Parameters:
//   - exe: path of the program it describes
//   - method: install method
func receipt(exe, method string) string {
	return fmt.Sprintf(`{"schema":1,"install_method":%q,"version":"1.2.3","channel":"stable",`+
		`"path":%q}`, method, exe)
}

// cacheFile returns the JSON of an update cache.
//
// Parameters:
//   - at: time of the check
//   - channel: channel the check was made for
//   - latest: newest version found, empty for a failed check
func cacheFile(at time.Time, channel, latest string) string {
	return fmt.Sprintf(`{"schema":1,"checked_at":%q,"channel":%q,"latest":%q}`,
		at.UTC().Format(time.RFC3339), channel, latest)
}

// TestHealthy checks that a healthy installation passes every check, in the documented order.
//
// Parameters:
//   - t: test to report to
func TestHealthy(t *testing.T) {
	f := newFixture(t)
	found := f.run()

	want := []string{
		"version", "install", "path", "dir.config", "dir.data", "dir.state", "dir.cache",
		"dir.plugins", "file.config", "file.connections", "via", "file.aliases", "file.repos",
		"file.funcs",
		"names",
		"shell", "ssh", "git", "term", "locale", "color", "mouse", "clipboard", "size", "tmux",
		"runtime", "handoff", "update",
	}

	if got := ids(found); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("checks = %v, want %v", got, want)
	}

	for _, x := range found {
		if x.Level != levelOK {
			t.Errorf("%s: %v %s", x.ID, x.Level, text(x))
		}
	}
}

// TestDirsUnresolved checks that unusable directories are one failure and that the checks that
// depend on them are left out.
//
// Parameters:
//   - t: test to report to
func TestDirsUnresolved(t *testing.T) {
	f := newFixture(t)
	f.env.Home = "relative/home"

	found := f.run()

	if x := get(t, found, "dirs"); x.Level != levelFail || x.Msg.Key != "cli_doctor_dirs_unresolved" {
		t.Errorf("dirs = %v %s", x.Level, text(x))
	}

	for _, id := range ids(found) {
		if strings.HasPrefix(id, "dir.") || strings.HasPrefix(id, "file.") || id == "names" ||
			id == "update" {
			t.Errorf("check %q ran without directories", id)
		}
	}
}

// TestScenarios runs every scenario and checks that, together, they use every message that the
// checks can print and that each prints cleanly in both languages.
//
// Parameters:
//   - t: test to report to
func TestScenarios(t *testing.T) {
	covered := map[string]bool{}
	exp := scenarios()

	for _, sc := range exp {
		t.Run(sc.name, func(t *testing.T) {
			f := newFixture(t)
			sc.setup(f)

			x := get(t, f.run(), sc.id)
			if x.Level != sc.level || x.Msg.Key != sc.key {
				t.Fatalf("%s: got %v %s, want %v %s", sc.id, x.Level, text(x), sc.level, sc.key)
			}

			if sc.fix != "" && x.Fix.Key != sc.fix {
				t.Errorf("fix = %q, want %q", x.Fix.Key, sc.fix)
			}

			if strings.Contains(text(x), f.home) {
				t.Errorf("the home directory is not shortened: %s", text(x))
			}

			for _, w := range sc.has {
				if !strings.Contains(text(x), w) {
					t.Errorf("%s lacks %q", text(x), w)
				}
			}

			collectKeys(covered, x.Msg)
			collectKeys(covered, x.Fix)

			for _, d := range x.Details {
				collectKeys(covered, d)
			}

			for _, lang := range []string{"en", "fr"} {
				h := newHarness(t)
				h.vars["TOTE_LANG"] = lang

				all := []msg{x.Msg, x.Fix}
				all = append(all, x.Details...)

				for _, m := range all {
					if m.Key == "" {
						continue
					}

					if got := h.app.render(m); strings.Contains(got, "%!") ||
						strings.HasPrefix(got, "!") {
						t.Errorf("%s: %s renders as %q", lang, m.Key, got)
					}
				}
			}
		})
	}

	// Keys that only the verb prints, covered by the tests of the verb.
	verbKeys := map[string]bool{
		"cli_doctor_header": true, "cli_doctor_label_ok": true, "cli_doctor_label_warn": true,
		"cli_doctor_label_fail": true, "cli_doctor_fix": true, "cli_doctor_summary": true,
		"cli_doctor_reset_alone": true, "cli_doctor_reset_no_terminal": true,
		"cli_doctor_reset_done": true, "cli_doctor_kind_extra": true,
	}

	for _, k := range i18n.Keys() {
		if strings.HasPrefix(k, "cli_doctor_") && !covered[k] && !verbKeys[k] {
			t.Errorf("no scenario prints %s", k)
		}
	}
}

// scenarios lists the installations that the checks are tried on.
func scenarios() []scenario {
	var all []scenario

	for _, g := range [][]scenario{
		hostScenarios(), dirScenarios(), fileScenarios(), viaScenarios(), nameScenarios(),
		shellScenarios(),
		termScenarios(), clipScenarios(), runtimeScenarios(), updateScenarios(),
	} {
		all = append(all, g...)
	}

	return all
}

// hostScenarios covers the version, the install method, PATH and the programs.
func hostScenarios() []scenario {
	noop := func(*fixture) {}

	return []scenario{
		{
			"unresolved", func(f *fixture) { f.env.Home = "relative" }, "dirs", levelFail,
			"cli_doctor_dirs_unresolved", "cli_doctor_fix_dirs", nil,
		},
		{"release", noop, "version", levelOK, "cli_doctor_version", "", []string{"1.2.3"}},
		{
			"dev", func(f *fixture) { f.env.Version = "dev" }, "version", levelOK,
			"cli_doctor_version_dev", "", nil,
		},
		{"no receipt", noop, "install", levelOK, "cli_doctor_install_none", "", []string{"~/bin/tote"}},
		{"receipt", func(f *fixture) {
			f.write(".local/state/tote/install.json", receipt(f.env.Exe, "script"), 0o600)
		}, "install", levelOK, "cli_doctor_install_receipt", "", []string{"script", "1.2.3"}},
		{
			"bad receipt", func(f *fixture) {
				f.write(".local/state/tote/install.json", "{", 0o600)
			}, "install", levelWarn, "cli_doctor_install_bad_receipt", "cli_doctor_fix_receipt",
			[]string{"~/.local/state/tote/install.json", "cli_doctor_install_guess"},
		},
		{"foreign receipt", func(f *fixture) {
			f.write(".local/state/tote/install.json", receipt("/elsewhere/tote", "script"), 0o600)
		}, "install", levelWarn, "cli_doctor_install_foreign", "", []string{"/elsewhere/tote"}},
		{
			"receipt of another version", func(f *fixture) {
				f.write(".local/state/tote/install.json", receipt(f.env.Exe, "script"), 0o600)
				f.env.Version = "1.4.0"
			}, "install", levelWarn, "cli_doctor_install_stale_receipt",
			"cli_doctor_fix_receipt_stale",
			[]string{"1.2.3", "1.4.0"},
		},
		{"receipt of a development build", func(f *fixture) {
			f.write(".local/state/tote/install.json", receipt(f.env.Exe, "script"), 0o600)
			f.env.Version = "dev"
		}, "install", levelOK, "cli_doctor_install_receipt", "", []string{"script", "1.2.3"}},
		{
			"apt", func(f *fixture) { f.env.Exe = "/usr/lib/tote-doctor-test/tote" }, "install",
			levelOK,
			"cli_doctor_install_managed", "",
			[]string{"apt", "sudo apt"},
		},
		{
			"brew", func(f *fixture) { f.env.Exe = "/opt/homebrew/bin/tote-doctor-test" },
			"install",
			levelOK, "cli_doctor_install_managed", "",
			[]string{"brew", "brew upgrade"},
		},
		{"unknown exe", func(f *fixture) {
			f.env.Exe, f.env.ExeErr = "", fmt.Errorf("gone")
		}, "install", levelWarn, "cli_doctor_install_noexe", "", []string{"gone"}},

		{"path ok", noop, "path", levelOK, "cli_doctor_path_ok", "", []string{"~/bin/tote"}},
		{"path via link", func(f *fixture) {
			if err := os.Symlink(f.tools["tote"], f.path("bin/alias")); err != nil {
				f.t.Fatal(err)
			}

			f.tools["tote"] = f.path("bin/alias")
		}, "path", levelOK, "cli_doctor_path_ok", "", nil},
		{
			"not in path", func(f *fixture) { delete(f.tools, "tote") }, "path", levelWarn,
			"cli_doctor_path_missing", "cli_doctor_fix_path",
			[]string{"~/bin"},
		},
		{
			"shadowed", func(f *fixture) { f.tools["tote"] = f.tools["ssh"] }, "path", levelWarn,
			"cli_doctor_path_other", "cli_doctor_fix_shadow",
			[]string{"~/bin/ssh"},
		},

		{"ssh found", noop, "ssh", levelOK, "cli_doctor_ssh", "", []string{"~/bin/ssh"}},
		{
			"ssh missing", func(f *fixture) { delete(f.tools, "ssh") }, "ssh", levelWarn,
			"cli_doctor_ssh_missing", "cli_doctor_fix_install",
			[]string{"ssh"},
		},
		{"ssh missing with connections", func(f *fixture) {
			delete(f.tools, "ssh")
			f.conf("connections.toml", fixtureConns)
		}, "ssh", levelFail, "cli_doctor_ssh_missing", "", nil},
		{"ssh missing without ssh connections", func(f *fixture) {
			delete(f.tools, "ssh")
			f.conf("connections.toml", "schema = 1\n[[connection]]\nname = \"m\"\nvia = \"mosh\"\n"+
				"target = \"m.example.org\"\n")
		}, "ssh", levelWarn, "cli_doctor_ssh_missing", "", nil},
		{"git found", noop, "git", levelOK, "cli_doctor_git", "", []string{"~/bin/git"}},
		{"git missing", func(f *fixture) {
			delete(f.tools, "git")
			f.conf("connections.toml", fixtureConns)
		}, "git", levelWarn, "cli_doctor_git_missing", "cli_doctor_fix_install", []string{"git"}},
	}
}

// dirScenarios covers the four directories.
func dirScenarios() []scenario {
	noop := func(*fixture) {}
	label := func(k string) []string { return []string{k} }

	return []scenario{
		{
			"dir ok", noop, "dir.config", levelOK, "cli_doctor_dir_ok", "",
			[]string{"cli_doctor_dir_config", "~/.config/tote", "0700"},
		},
		{"dir missing", func(f *fixture) {
			if err := os.Remove(f.path(".cache/tote")); err != nil {
				f.t.Fatal(err)
			}
		}, "dir.cache", levelOK, "cli_doctor_dir_missing", "", label("cli_doctor_dir_cache")},
		{
			"private dir open", func(f *fixture) { f.mkdir(".config/tote", 0o755) }, "dir.config",
			levelWarn, "cli_doctor_dir_open", "cli_doctor_fix_chmod",
			[]string{"0755", "chmod"},
		},
		{
			"data dir shared", func(f *fixture) { f.mkdir(".local/share/tote", 0o777) }, "dir.data",
			levelWarn, "cli_doctor_dir_shared", "cli_doctor_fix_chmod",
			[]string{"0777"},
		},
		{
			"cache dir open is fine", func(f *fixture) { f.mkdir(".cache/tote", 0o755) },
			"dir.cache", levelOK, "cli_doctor_dir_ok", "", nil,
		},
		{
			"plugins dir ok", func(f *fixture) { f.mkdir(".local/share/tote/plugins", 0o700) },
			"dir.plugins", levelOK, "cli_doctor_dir_ok", "",
			[]string{"0700"},
		},
		{
			"plugins dir shared", func(f *fixture) { f.mkdir(".local/share/tote/plugins", 0o777) },
			"dir.plugins", levelFail, "cli_doctor_dir_plugins_shared", "cli_doctor_fix_chmod_gow",
			[]string{"0777", "~/.local/share/tote/plugins"},
		},
		{
			"plugins dir open is fine",
			func(f *fixture) { f.mkdir(".local/share/tote/plugins", 0o755) },
			"dir.plugins", levelOK, "cli_doctor_dir_ok", "", nil,
		},
		{
			"cache dir group writable", func(f *fixture) { f.mkdir(".cache/tote", 0o775) },
			"dir.cache", levelWarn, "cli_doctor_dir_shared", "", nil,
		},
		{"not a directory", func(f *fixture) {
			if err := os.Remove(f.path(".local/state/tote")); err != nil {
				f.t.Fatal(err)
			}

			f.write(".local/state/tote", "x", 0o600)
		}, "dir.state", levelFail, "cli_doctor_dir_not_dir", "cli_doctor_fix_not_dir", nil},
		{
			"broken link", func(f *fixture) {
				if err := os.Remove(f.path(".cache/tote")); err != nil {
					f.t.Fatal(err)
				}

				if err := os.Symlink(f.path("nowhere"), f.path(".cache/tote")); err != nil {
					f.t.Fatal(err)
				}
			}, "dir.cache", levelFail, "cli_doctor_dir_broken_link", "cli_doctor_fix_link",
			[]string{"~/nowhere"},
		},
		{"link to a directory", func(f *fixture) {
			f.mkdir("real", 0o700)

			if err := os.Remove(f.path(".local/share/tote")); err != nil {
				f.t.Fatal(err)
			}

			if err := os.Symlink(f.path("real"), f.path(".local/share/tote")); err != nil {
				f.t.Fatal(err)
			}
		}, "dir.data", levelOK, "cli_doctor_dir_link", "", []string{"~/real", "0700"}},
		{
			"owned by someone else", func(f *fixture) { f.env.UID = os.Getuid() + 1 }, "dir.config",
			levelFail, "cli_doctor_dir_foreign", "cli_doctor_fix_chown",
			[]string{"chown"},
		},
		{"owned by someone else, as root", func(f *fixture) {
			notRoot(f.t)
			f.env.UID = 0
		}, "dir.config", levelWarn, "cli_doctor_dir_foreign", "", nil},
		{
			"owner cannot enter", func(f *fixture) {
				notRoot(f.t)
				f.mkdir(".config/tote", 0o500)
				f.t.Cleanup(func() { _ = os.Chmod(f.path(".config/tote"), 0o700) })
			}, "dir.config", levelFail, "cli_doctor_dir_no_access", "cli_doctor_fix_chmod",
			[]string{"0500"},
		},
		{"cannot be inspected", func(f *fixture) {
			notRoot(f.t)
			f.mkdir(".local", 0o000)
			f.t.Cleanup(func() { _ = os.Chmod(f.path(".local"), 0o700) })
		}, "dir.data", levelFail, "cli_doctor_dir_error", "", nil},
	}
}

// viaConns builds a connections.toml from connections written as name and via.
//
// Parameters:
//   - pairs: name and via of each connection, one after the other; an empty via writes none
func viaConns(pairs ...string) string {
	var b strings.Builder

	b.WriteString("schema = 1\n")

	for i := 0; i < len(pairs); i += 2 {
		fmt.Fprintf(&b, "\n[[connection]]\nname = %q\ntarget = \"h\"\n", pairs[i])

		if pairs[i+1] != "" {
			fmt.Fprintf(&b, "via = %q\n", pairs[i+1])
		}
	}

	return b.String()
}

// viaSetup returns the setup of a fixture whose PATH is its bin directory, where the given
// programs are executable scripts that leave a mark if they are ever run, and whose connections
// are the given ones.
//
// Parameters:
//   - conns: content of connections.toml
//   - tools: names of the programs to put in the PATH
func viaSetup(conns string, tools ...string) func(*fixture) {
	return func(f *fixture) {
		f.vars["PATH"] = f.path("bin")
		f.conf("connections.toml", conns)

		for _, n := range tools {
			f.write("bin/"+n, "#!/bin/sh\ntouch '"+f.path("ran")+"'\n", 0o755)
		}
	}
}

// viaScenarios covers the check of the programs that the vias of the connections start.
func viaScenarios() []scenario {
	return []scenario{
		{"no connection", func(*fixture) {}, "via", levelOK, "cli_doctor_via_none", "", nil},
		{
			"only plain ssh", viaSetup(viaConns("a", "", "b", "ssh")), "via", levelOK,
			"cli_doctor_via_none", "", nil,
		},
		{
			"verb found", viaSetup(viaConns("a", "wssh"), "wssh"), "via", levelOK,
			"cli_doctor_via_ok", "", nil,
		},
		{
			"template found", viaSetup(viaConns("a", "ssh -t admin@jump.example.org {user@}{host}"),
				"ssh"),
			"via", levelOK, "cli_doctor_via_ok", "", nil,
		},
		{"path with a slash found", func(f *fixture) {
			viaSetup(viaConns("a", "bin/ssh -4"))(f)
			f.t.Chdir(f.home)
		}, "via", levelOK, "cli_doctor_via_ok", "", nil},
		{
			"shell alias", viaSetup(viaConns("a", "ssh", "b", "jmp")), "via", levelWarn,
			"cli_doctor_via_missing", "cli_via_hint",
			[]string{"1", "cli_doctor_via_item b jmp"},
		},
		{
			"template with a missing program", viaSetup(viaConns("a", "jmp -t {host}"), "ssh"),
			"via", levelWarn, "cli_doctor_via_missing", "cli_via_hint",
			[]string{"cli_doctor_via_item a jmp"},
		},
		{
			"program that is not executable", func(f *fixture) {
				viaSetup(viaConns("a", "plain"))(f)
				f.write("bin/plain", "#!/bin/sh\n", 0o644)
			}, "via", levelWarn, "cli_doctor_via_missing", "cli_via_hint",
			[]string{"cli_doctor_via_item a plain"},
		},
		{
			"program in a relative directory", func(f *fixture) {
				viaSetup(viaConns("a", "wssh"), "wssh")(f)
				f.vars["PATH"] = "bin"
			}, "via", levelWarn, "cli_doctor_via_missing", "cli_via_hint",
			[]string{"cli_doctor_via_item a wssh"},
		},
		{
			"path with a slash that does not exist", func(f *fixture) {
				viaSetup(viaConns("a", "nowhere/ssh"))(f)
				f.t.Chdir(f.home)
			}, "via", levelWarn, "cli_doctor_via_missing", "cli_via_hint",
			[]string{"cli_doctor_via_item a nowhere/ssh"},
		},
		{
			"program that is a directory", func(f *fixture) {
				viaSetup(viaConns("a", "adir"))(f)

				if err := os.MkdirAll(f.path("bin/adir"), 0o755); err != nil {
					f.t.Fatal(err)
				}
			}, "via", levelWarn, "cli_doctor_via_missing", "cli_via_hint",
			[]string{"cli_doctor_via_item a adir"},
		},
		{
			"invalid connection is skipped", viaSetup(viaConns("a", "/nowhere/jmp")), "via",
			levelOK,
			"cli_doctor_via_none", "", nil,
		},
		{
			"seven missing, five named", viaSetup(viaConns(many7()...)), "via", levelWarn,
			"cli_doctor_via_missing", "cli_via_hint",
			[]string{"7", "cli_doctor_via_item c5 tool5", "cli_doctor_via_more 2"},
		},
	}
}

// TestViaNeverRuns checks that the check of the vias only looks at the files: a program that is
// found is not started, and a list of seven missing programs names five.
//
// Parameters:
//   - t: test to report to
func TestViaNeverRuns(t *testing.T) {
	f := newFixture(t)
	viaSetup(viaConns("a", "wssh", "b", "ssh -4 {host}", "c", "gone"), "wssh", "ssh")(f)

	var via finding

	for _, x := range f.run() {
		if x.ID == "via" {
			via = x
		}
	}

	if via.Level != levelWarn || len(via.Details) != 1 || strings.Contains(text(via), "wssh") {
		t.Errorf("via = %s", text(via))
	}

	if _, err := os.Stat(f.path("ran")); err == nil {
		t.Error("a program of the PATH was started")
	}

	f = newFixture(t)
	viaSetup(viaConns(many7()...))(f)

	for _, x := range f.run() {
		if x.ID == "via" && len(x.Details) != maxListed+1 {
			t.Errorf("%d detail lines, want %d: %s", len(x.Details), maxListed+1, text(x))
		}
	}
}

// many7 returns the names and vias of seven connections whose program is not found.
func many7() []string {
	var out []string

	for i := 1; i <= 7; i++ {
		out = append(out, fmt.Sprintf("c%d", i), fmt.Sprintf("tool%d", i))
	}

	return out
}

// deadRepos builds a repos.toml from paths; the repositories are named r0, r1, and so on.
//
// Parameters:
//   - paths: path of each repository
func deadRepos(paths ...string) string {
	var b strings.Builder

	b.WriteString("schema = 1\n")

	for i, p := range paths {
		fmt.Fprintf(&b, "\n[[repo]]\nname = \"r%d\"\npath = %q\n", i, p)
	}

	return b.String()
}

// fileScenarios covers config.toml and the four data files.
func fileScenarios() []scenario {
	noop := func(*fixture) {}
	conns := func(text string) func(*fixture) {
		return func(f *fixture) { f.conf("connections.toml", text) }
	}

	return []scenario{
		{
			"no data file", noop, "file.aliases", levelOK, "cli_doctor_file_missing", "",
			[]string{"aliases.toml"},
		},
		{
			"valid connections", func(f *fixture) { f.conf("connections.toml", fixtureConns) },
			"file.connections", levelOK, "cli_doctor_file_ok", "",
			[]string{"connections.toml 3"},
		},
		{
			"valid aliases", func(f *fixture) { f.conf("aliases.toml", fixtureAliases) },
			"file.aliases", levelOK, "cli_doctor_file_ok", "",
			[]string{"aliases.toml 2"},
		},
		{
			"valid repos", func(f *fixture) {
				f.mkdir("code/proj", 0o700)
				f.mkdir("srv/docs", 0o700)
				f.conf("repos.toml", deadRepos("~/code/proj", "~/srv/docs"))
			},
			"file.repos", levelOK, "cli_doctor_file_ok", "",
			[]string{"repos.toml 2"},
		},
		{
			"valid funcs", func(f *fixture) {
				f.write("bin/hello.sh", "echo hello\n", 0o600)
				f.conf("funcs.toml", fixtureFuncs)
			},
			"file.funcs", levelOK, "cli_doctor_file_ok", "",
			[]string{"funcs.toml 2"},
		},
		{
			"repository path missing", func(f *fixture) {
				f.mkdir("code/proj", 0o700)
				f.conf("repos.toml", deadRepos("~/code/proj", "~/code/gone"))
			},
			"file.repos", levelWarn, "cli_doctor_repos_gone", "cli_doctor_fix_repos_gone",
			[]string{"1", "cli_doctor_gone_item r1 ~/code/gone"},
		},
		{
			"repository path is a file", func(f *fixture) {
				f.write("code/file", "x", 0o600)
				f.conf("repos.toml", deadRepos("~/code/file"))
			},
			"file.repos", levelWarn, "cli_doctor_repos_gone", "cli_doctor_fix_repos_gone",
			[]string{"cli_doctor_gone_item r0 ~/code/file"},
		},
		{
			"repository path is relative", func(f *fixture) {
				f.conf("repos.toml", deadRepos("rel/dir"))
			},
			"file.repos", levelWarn, "cli_doctor_repos_gone", "cli_doctor_fix_repos_gone",
			[]string{"cli_doctor_gone_item r0 rel/dir"},
		},
		{
			"seven repositories missing", func(f *fixture) {
				f.conf("repos.toml", deadRepos("/n/0", "/n/1", "/n/2", "/n/3", "/n/4", "/n/5", "/n/6"))
			},
			"file.repos", levelWarn, "cli_doctor_repos_gone", "cli_doctor_fix_repos_gone",
			[]string{"7", "cli_doctor_gone_item r4 /n/4", "cli_doctor_gone_more 2"},
		},
		{
			"function script missing", func(f *fixture) { f.conf("funcs.toml", fixtureFuncs) },
			"file.funcs", levelWarn, "cli_doctor_funcs_gone", "cli_doctor_fix_funcs_gone",
			[]string{"1", "cli_doctor_gone_item hello ~/bin/hello.sh"},
		},
		{
			"function script is a directory", func(f *fixture) {
				f.mkdir("bin/hello.sh", 0o700)
				f.conf("funcs.toml", fixtureFuncs)
			},
			"file.funcs", levelWarn, "cli_doctor_funcs_gone", "cli_doctor_fix_funcs_gone",
			[]string{"cli_doctor_gone_item hello ~/bin/hello.sh"},
		},
		{
			"missing script and invalid record", func(f *fixture) {
				f.conf("funcs.toml", fixtureFuncs+"\n[[func]]\nname = \"1x\"\nkind = \"source\"\n"+
					"script = \"/s\"\n")
			},
			"file.funcs", levelWarn, "cli_doctor_file_invalid", "cli_doctor_fix_invalid",
			[]string{"cli_doctor_funcs_gone 1"},
		},
		{
			"syntax error", conns("schema = 1\n\n[[connection]]\nname = \"a\"\ntarget =\n"),
			"file.connections", levelFail, "cli_doctor_file_syntax", "cli_doctor_fix_syntax",
			[]string{"connections.toml 5", "cli_doctor_file_readonly"},
		},
		{
			"duplicate record", conns("schema = 1\n[[connection]]\nname = \"a\"\n" +
				"target = \"h\"\n\n[[connection]]\nname = \"a\"\ntarget = \"h\"\n"),
			"file.connections", levelFail,
			"cli_doctor_file_syntax", "",
			[]string{"duplicate key", " 6 "},
		},
		{
			"unsupported schema", conns("schema = 7\n"), "file.connections", levelFail,
			"cli_doctor_file_syntax", "",
			[]string{"schema"},
		},
		{
			"layout error", conns("schema = 1\nconnection = [{name = \"a\"}]\n"),
			"file.connections",
			levelFail, "cli_doctor_file_error", "", nil,
		},
		{
			"invalid record", conns("schema = 1\n\n[[connection]]\nname = \"bad name!\"\n" +
				"target = \"h\"\n"), "file.connections", levelWarn, "cli_doctor_file_invalid",
			"cli_doctor_fix_invalid",
			[]string{"cli_doctor_file_invalid_item 3 bad name!"},
		},
		{
			"needs review", conns("schema = 1\n[[connection]]\nname = \"a\"\ntarget = \"h\"\n" +
				"needs_review = true\n"), "file.connections", levelWarn, "cli_doctor_file_review",
			"cli_doctor_fix_review", nil,
		},
		{
			"shared file", func(f *fixture) {
				f.write(".config/tote/aliases.toml", fixtureAliases, 0o666)
			}, "file.aliases", levelWarn, "cli_doctor_file_shared", "cli_doctor_fix_chmod_file",
			[]string{"0666"},
		},
		{
			"read-only file", func(f *fixture) {
				f.write(".config/tote/aliases.toml", fixtureAliases, 0o400)
			}, "file.aliases", levelWarn, "cli_doctor_file_locked", "cli_doctor_fix_unlock",
			[]string{"0400"},
		},
		{"dangling link", func(f *fixture) {
			if err := os.Symlink(f.path("nowhere.toml"), f.path(".config/tote/repos.toml")); err != nil {
				f.t.Fatal(err)
			}
		}, "file.repos", levelFail, "cli_doctor_file_unreadable", "cli_doctor_fix_link_file", nil},
		{
			"directory in place of a file", func(f *fixture) {
				f.mkdir(".config/tote/funcs.toml",
					0o700)
			},
			"file.funcs", levelFail, "cli_doctor_file_unreadable", "", nil,
		},

		{"no config", noop, "file.config", levelOK, "cli_doctor_config_missing", "", nil},
		{"valid config", func(f *fixture) {
			f.conf("config.toml", "schema = 1\n[ui]\ntheme = \"dark\"\n")
		}, "file.config", levelOK, "cli_doctor_config_ok", "", []string{"config.toml 1"}},
		{
			"config syntax error", func(f *fixture) { f.conf("config.toml", "schema = 1\n[ui\n") },
			"file.config", levelFail, "cli_doctor_config_syntax", "cli_doctor_fix_config",
			[]string{"config.toml 2"},
		},
		{
			"config with a wrong type", func(f *fixture) {
				f.conf("config.toml",
					"[ui]\ntheme = 3\n")
			},
			"file.config", levelWarn, "cli_doctor_config_ignored", "cli_doctor_fix_config",
			[]string{"cli_doctor_config_at_line 2", "ui.theme must be a string, found an integer"},
		},
		{
			"unknown key", func(f *fixture) {
				f.conf("config.toml", "schema = 1\n[ui]\ntheem = \"dark\"\n")
			}, "file.config", levelWarn, "cli_doctor_config_ignored", "cli_doctor_fix_config",
			[]string{"cli_doctor_config_at_line 3", "cli_doctor_config_unknown_key ui.theem"},
		},
		{
			"unknown key of a newer version", func(f *fixture) {
				f.conf("config.toml", "schema = 1\n[shell]\nbind = true\ncompat_mn = true\n")
			}, "file.config", levelWarn, "cli_doctor_config_ignored", "cli_doctor_fix_config",
			[]string{"cli_doctor_config_at_line 3", "cli_doctor_config_unknown_key shell.bind"},
		},
		{
			"bad value", func(f *fixture) { f.conf("config.toml", "[ui]\ntheme = \"pink\"\n") },
			"file.config", levelWarn, "cli_doctor_config_ignored", "",
			[]string{"cli_doctor_config_at_line 2", "ui.theme"},
		},
		{"valid width", func(f *fixture) {
			f.conf("config.toml", "schema = 1\n[ui]\nwidth = \"auto\"\n")
		}, "file.config", levelOK, "cli_doctor_config_ok", "", []string{"config.toml 1"}},
		{
			"width full is gone", func(f *fixture) {
				f.conf("config.toml", "[ui]\nwidth = \"full\"\n")
			},
			"file.config", levelWarn, "cli_doctor_config_ignored", "",
			[]string{"cli_doctor_config_at_line 2", "ui.width", "allowed values: auto, 120, 160"},
		},
		{
			"bad width", func(f *fixture) { f.conf("config.toml", "[ui]\nwidth = \"200\"\n") },
			"file.config", levelWarn, "cli_doctor_config_ignored", "",
			[]string{"cli_doctor_config_at_line 2", "ui.width", "allowed values: auto, 120, 160"},
		},
		{
			"width as a number", func(f *fixture) { f.conf("config.toml", "[ui]\nwidth = 160\n") },
			"file.config", levelWarn, "cli_doctor_config_ignored", "",
			[]string{"cli_doctor_config_at_line 2", "ui.width must be a string, found an integer"},
		},
		{
			"unsupported config schema", func(f *fixture) { f.conf("config.toml", "schema = 2\n") },
			"file.config", levelFail, "cli_doctor_config_syntax", "cli_doctor_fix_config",
			[]string{"config.toml 1", "schema is 2"},
		},
		{
			"bad extra name", func(f *fixture) {
				f.conf("config.toml", "schema = 1\n[shell]\nextra_names = [\"ok\", \"bad name\"]\n")
			}, "file.config", levelWarn, "cli_doctor_config_ignored", "",
			[]string{"cli_doctor_config_at_line 3", "shell.extra_names"},
		},
		{
			"config too large", func(f *fixture) {
				f.conf("config.toml", "schema = 1\n# "+strings.Repeat("x", 70<<10)+"\n")
			}, "file.config", levelFail, "cli_doctor_config_invalid", "cli_doctor_fix_config",
			[]string{"config.toml"},
		},
		{
			"shared config", func(f *fixture) {
				f.write(".config/tote/config.toml", "schema = 1\n",
					0o666)
			},
			"file.config", levelWarn, "cli_doctor_file_shared", "cli_doctor_fix_chmod_file", nil,
		},
	}
}

// nameScenarios covers the clashes between names.
func nameScenarios() []scenario {
	return []scenario{
		{"no clash", func(f *fixture) {
			f.conf("aliases.toml", fixtureAliases)
			f.conf("repos.toml", fixtureRepos)
			f.conf("funcs.toml", fixtureFuncs)
			f.conf("connections.toml", fixtureConns)
		}, "names", levelOK, "cli_doctor_names_ok", "", []string{"names_ok 9"}},
		{
			"reserved name", func(f *fixture) {
				f.conf("aliases.toml", "schema = 1\n[[alias]]\nname = \"eval\"\ncmd = \"ls\"\n")
			}, "names", levelWarn, "cli_doctor_names_reserved", "cli_doctor_fix_rename",
			[]string{"kind_alias eval"},
		},
		{
			"alias and repository share a name", func(f *fixture) {
				f.conf("aliases.toml",
					"schema = 1\n[[alias]]\nname = \"gs\"\ncmd = \"git status\"\n")
				f.conf("repos.toml", "schema = 1\n[[repo]]\nname = \"gs\"\npath = \"~/gs\"\n")
			}, "names", levelWarn, "cli_doctor_names_duplicate", "cli_doctor_fix_names",
			[]string{"kind_repo gs"},
		},
		{"function and alias share a name", func(f *fixture) {
			f.conf("aliases.toml", "schema = 1\n[[alias]]\nname = \"hello\"\ncmd = \"echo\"\n")
			f.conf("funcs.toml", fixtureFuncs)
		}, "names", levelWarn, "cli_doctor_names_duplicate", "", []string{"kind_func hello"}},
		{
			"extra name clash", func(f *fixture) {
				f.conf("aliases.toml", fixtureAliases)
				f.conf("config.toml", "schema = 1\n[shell]\nextra_names = [\"ll\"]\n")
			}, "names", levelWarn, "cli_doctor_names_duplicate", "",
			[]string{"cli_doctor_kind_extra ll"},
		},
		{"compat name clash", func(f *fixture) {
			f.conf("funcs.toml", "schema = 1\n[[func]]\nname = \"mn\"\nkind = \"exec\"\n"+
				"script = \"/s/mn\"\nmanaged = false\n")
			f.conf("config.toml", "schema = 1\n[shell]\ncompat_mn = true\n")
		}, "names", levelWarn, "cli_doctor_names_duplicate", "", []string{"cli_doctor_kind_extra mn"}},
		{"connections share a word", func(f *fixture) {
			f.conf("connections.toml", "schema = 1\n[[connection]]\nname = \"a\"\ntarget = \"h\"\n\n"+
				"[[connection]]\nname = \"b\"\ntarget = \"h2\"\naliases = [\"A\"]\n")
		}, "names", levelWarn, "cli_doctor_names_shared", "cli_doctor_fix_names", []string{"b A a"}},
		{
			"connection named like a sub-verb", func(f *fixture) {
				f.conf("connections.toml", "schema = 1\n[[connection]]\nname = \"edit\"\n"+
					"target = \"h\"\naliases = [\"ok\", \"Find\"]\n")
			}, "names", levelWarn, "cli_doctor_names_command", "cli_doctor_fix_rename",
			[]string{"kind_connection edit", "Find edit"},
		},
		{
			"alias and repository named like a sub-verb", func(f *fixture) {
				f.conf("aliases.toml", "schema = 1\n[[alias]]\nname = \"status\"\ncmd = \"ls\"\n")
				f.conf("repos.toml", "schema = 1\n[[repo]]\nname = \"sync\"\npath = \"~/s\"\n")
			}, "names", levelWarn, "cli_doctor_names_command", "cli_doctor_fix_rename",
			[]string{"kind_alias status", "kind_repo sync"},
		},
		{
			"alias under a reserved prefix", func(f *fixture) {
				f.conf("aliases.toml",
					"schema = 1\n[[alias]]\nname = \"__tote_pick\"\ncmd = \"ls\"\n")
			}, "names", levelWarn, "cli_doctor_names_reserved", "cli_doctor_fix_rename",
			[]string{"kind_alias __tote_pick"},
		},
		{"several clashes", func(f *fixture) {
			f.conf("aliases.toml", "schema = 1\n[[alias]]\nname = \"source\"\ncmd = \"ls\"\n\n"+
				"[[alias]]\nname = \"eval\"\ncmd = \"ls\"\n")
		}, "names", levelWarn, "cli_doctor_names_reserved", "", []string{"eval"}},
	}
}

// shellScenarios covers the shell integration.
func shellScenarios() []scenario {
	bashrc := func(text string) func(*fixture) {
		return func(f *fixture) {
			f.vars["TOTE_SHELL"] = ""
			f.write(".bashrc", text, 0o600)
		}
	}

	return []scenario{
		{
			"wrapper active", func(*fixture) {}, "shell", levelOK, "cli_doctor_shell_active", "",
			[]string{"bash"},
		},
		{
			"bad marker", func(f *fixture) { f.vars["TOTE_SHELL"] = "fish" }, "shell", levelWarn,
			"cli_doctor_shell_marker", "cli_doctor_fix_shell_marker", nil,
		},
		{
			"no wrapper, no line", func(f *fixture) { f.vars["TOTE_SHELL"] = "" }, "shell",
			levelWarn,
			"cli_doctor_shell_missing", "cli_doctor_fix_shell_add",
			[]string{"~/.bashrc", `eval "$(tote init bash)"`},
		},
		{
			"line only in a comment", bashrc("# eval \"$(tote init bash)\"\n"), "shell", levelWarn,
			"cli_doctor_shell_missing", "", nil,
		},
		{
			"line present", bashrc("export A=1\neval \"$(tote init bash)\"\n"), "shell", levelWarn,
			"cli_doctor_shell_inactive", "cli_doctor_fix_shell_reload",
			[]string{"~/.bashrc 2"},
		},
		{"source form", func(f *fixture) {
			f.vars["TOTE_SHELL"] = ""
			f.write(".profile", "source <(tote init bash)\n", 0o600)
		}, "shell", levelWarn, "cli_doctor_shell_inactive", "", []string{"~/.profile 1"}},
		{
			"line for another shell", bashrc("eval \"$(tote init zsh)\"\n"), "shell", levelWarn,
			"cli_doctor_shell_wrong", "cli_doctor_fix_shell_add",
			[]string{"zsh bash"},
		},
		{"zsh with ZDOTDIR", func(f *fixture) {
			f.vars["TOTE_SHELL"], f.vars["SHELL"] = "", "/usr/bin/zsh"
			f.vars["ZDOTDIR"] = f.path("zdot")
			f.write("zdot/.zshrc", "eval \"$(tote init zsh)\"\n", 0o600)
		}, "shell", levelWarn, "cli_doctor_shell_inactive", "", []string{"~/zdot/.zshrc 1"}},
		{
			"zsh without line", func(f *fixture) {
				f.vars["TOTE_SHELL"], f.vars["SHELL"] = "", "/usr/bin/zsh"
			}, "shell", levelWarn, "cli_doctor_shell_missing", "",
			[]string{"~/.zshrc", `eval "$(tote init zsh)"`},
		},
		{"unsupported shell", func(f *fixture) {
			f.vars["TOTE_SHELL"], f.vars["SHELL"] = "", "/usr/bin/fish"
		}, "shell", levelWarn, "cli_doctor_shell_unsupported", "", []string{"fish"}},
		{"unknown shell", func(f *fixture) {
			f.vars["TOTE_SHELL"], f.vars["SHELL"] = "", ""
		}, "shell", levelWarn, "cli_doctor_shell_unset", "", nil},
	}
}

// termScenarios covers TERM, the locale, the colors, the size and tmux.
func termScenarios() []scenario {
	noop := func(*fixture) {}
	size := func(w, h int) func(*fixture) {
		return func(f *fixture) { f.env.Width, f.env.Height = w, h }
	}
	vars := func(kv ...string) func(*fixture) {
		return func(f *fixture) {
			for i := 0; i < len(kv); i += 2 {
				f.vars[kv[i]] = kv[i+1]
			}
		}
	}
	term := func(t ui.Terminal) func(*fixture) {
		return func(f *fixture) { f.env.Term = t }
	}

	return []scenario{
		{"term set", noop, "term", levelOK, "cli_doctor_term", "", []string{"xterm-256color"}},
		{
			"term unset", vars("TERM", ""), "term", levelWarn, "cli_doctor_term_unset",
			"cli_doctor_fix_term", nil,
		},
		{"term dumb", vars("TERM", "dumb"), "term", levelWarn, "cli_doctor_term_dumb", "", nil},
		{"term linux", vars("TERM", "linux"), "term", levelOK, "cli_doctor_term_linux", "", nil},

		{
			"utf8 locale", noop, "locale", levelOK, "cli_doctor_locale_ok", "",
			[]string{"LANG=en_US.UTF-8"},
		},
		{
			"locale from LC_ALL", vars("LC_ALL", "fr_FR.UTF-8"), "locale", levelOK,
			"cli_doctor_locale_ok", "",
			[]string{"LC_ALL=fr_FR.UTF-8"},
		},
		{
			"ascii locale", func(f *fixture) {
				f.vars["LANG"] = "C"
				f.env.Term = ui.Terminal{Color: "truecolor", ASCII: true}
			}, "locale", levelWarn, "cli_doctor_locale_ascii", "cli_doctor_fix_locale",
			[]string{"LANG=C"},
		},
		{"no locale", func(f *fixture) {
			f.vars["LANG"] = ""
			f.env.Term = ui.Terminal{Color: "truecolor", ASCII: true}
		}, "locale", levelWarn, "cli_doctor_locale_ascii", "", []string{"-"}},
		{
			"forced ascii", term(ui.Terminal{Color: "truecolor", UTF8: true, ASCII: true}),
			"locale",
			levelOK, "cli_doctor_locale_ok", "",
			[]string{"cli_doctor_locale_forced"},
		},

		{
			"truecolor", noop, "color", levelOK, "cli_doctor_color_chosen", "",
			[]string{"cli_doctor_profile_truecolor"},
		},
		{
			"256 colors", term(ui.Terminal{Color: "256", UTF8: true}), "color", levelOK,
			"cli_doctor_color_chosen", "",
			[]string{"cli_doctor_profile_256"},
		},
		{
			"16 colors", term(ui.Terminal{Color: "16", UTF8: true}), "color", levelOK,
			"cli_doctor_color_chosen", "",
			[]string{"cli_doctor_profile_16"},
		},
		{"TOTE_COLOR", func(f *fixture) {
			f.vars["TOTE_COLOR"] = "256"
			f.env.Term = ui.Terminal{Color: "256", Override: "TOTE_COLOR", UTF8: true}
		}, "color", levelOK, "cli_doctor_color_tote", "", []string{"cli_doctor_profile_256 256"}},
		{
			"NO_COLOR", term(ui.Terminal{Color: "none", Override: "NO_COLOR", UTF8: true}), "color",
			levelOK, "cli_doctor_color_nocolor", "", nil,
		},
		{
			"bad TOTE_COLOR", func(f *fixture) {
				f.vars["TOTE_COLOR"] = "rainbow"
				f.env.Term = ui.Terminal{Color: "256", BadColor: true, UTF8: true}
			}, "color", levelWarn, "cli_doctor_color_bad", "cli_doctor_fix_color",
			[]string{"rainbow", "cli_doctor_profile_256"},
		},
		{
			"no color", term(ui.Terminal{Color: "none", UTF8: true}), "color", levelWarn,
			"cli_doctor_color_none", "cli_doctor_fix_color", nil,
		},
		{
			"dumb terminal has no color", term(ui.Terminal{Color: "none", UTF8: true, Dumb: true}),
			"color", levelOK, "cli_doctor_color_chosen", "",
			[]string{"cli_doctor_profile_none"},
		},

		{"mouse on", noop, "mouse", levelOK, "cli_doctor_mouse_on", "", nil},
		{"mouse off by setting", func(f *fixture) {
			f.conf("config.toml", "schema = 1\n[ui]\nmouse = false\n")
		}, "mouse", levelOK, "cli_doctor_mouse_setting", "", nil},
		{
			"mouse and a dumb terminal", vars("TERM", "dumb"), "mouse", levelOK,
			"cli_doctor_mouse_term", "",
			[]string{"dumb"},
		},
		{
			"mouse and the linux console", vars("TERM", "linux"), "mouse", levelOK,
			"cli_doctor_mouse_term", "",
			[]string{"linux"},
		},
		{
			"mouse and no TERM", vars("TERM", ""), "mouse", levelOK, "cli_doctor_mouse_noterm", "",
			nil,
		},

		{"full layout", noop, "size", levelOK, "cli_doctor_size_full", "", []string{"120 40"}},
		{"compact layout", size(60, 30), "size", levelOK, "cli_doctor_size_compact", "", nil},
		{
			"tiny terminal", size(23, 8), "size", levelWarn, "cli_doctor_size_tiny",
			"cli_doctor_fix_size",
			[]string{"23 8 24 7"},
		},
		{
			"just below the minimum", size(24, 6), "size", levelWarn, "cli_doctor_size_tiny",
			"cli_doctor_fix_size",
			[]string{"24 6 24 7"},
		},
		{"smallest compact", size(24, 7), "size", levelOK, "cli_doctor_size_compact", "", nil},
		{"compact by setting", func(f *fixture) {
			f.conf("config.toml", "schema = 1\n[ui]\ncompact = \"always\"\n")
		}, "size", levelOK, "cli_doctor_size_compact", "", nil},
		{
			"no terminal", func(f *fixture) { f.env.Interactive = false }, "size", levelOK,
			"cli_doctor_size_none", "", nil,
		},
		{
			"unknown size", func(f *fixture) { f.env.SizeOK = false }, "size", levelOK,
			"cli_doctor_size_none", "", nil,
		},

		{"no multiplexer", noop, "tmux", levelOK, "cli_doctor_tmux_none", "", nil},
		{
			"inside tmux", vars("TMUX", "/tmp/tmux-1/default,1,0", "TERM", "tmux-256color"), "tmux",
			levelOK, "cli_doctor_tmux_inside", "",
			[]string{"tmux-256color"},
		},
		{
			"inside tmux with a screen TERM", vars("TMUX", "x", "TERM", "screen-256color"), "tmux",
			levelOK, "cli_doctor_tmux_inside", "", nil,
		},
		{
			"inside tmux with the wrong TERM", vars("TMUX", "x"), "tmux", levelWarn,
			"cli_doctor_tmux_term", "cli_doctor_fix_tmux",
			[]string{"xterm-256color"},
		},
		{
			"inside screen", vars("STY", "1.pts", "TERM", "screen"), "tmux", levelOK,
			"cli_doctor_screen_inside", "", nil,
		},
		{
			"inside screen with the wrong TERM", vars("STY", "1.pts"), "tmux", levelWarn,
			"cli_doctor_screen_term", "", nil,
		},
	}
}

// runtimeScenarios covers the hand-off directory and the files left in it.
func runtimeScenarios() []scenario {
	noop := func(*fixture) {}
	stale := func(names ...string) func(*fixture) {
		return func(f *fixture) {
			for _, n := range names {
				f.write("run/"+n, "", 0o600)
				f.age("run/"+n, 2*time.Hour)
			}
		}
	}

	return []scenario{
		{
			"runtime ok", noop, "runtime", levelOK, "cli_doctor_runtime_ok", "",
			[]string{"~/run", "0700"},
		},
		{
			"runtime missing", func(f *fixture) { f.vars["XDG_RUNTIME_DIR"] = f.path("nowhere") },
			"runtime", levelFail, "cli_doctor_runtime_missing", "cli_doctor_fix_runtime", nil,
		},
		{"runtime not a directory", func(f *fixture) {
			f.write("file", "", 0o600)
			f.vars["XDG_RUNTIME_DIR"] = f.path("file")
		}, "runtime", levelFail, "cli_doctor_runtime_not_dir", "", nil},
		{"runtime not writable", func(f *fixture) {
			notRoot(f.t)
			f.mkdir("run", 0o500)
			f.t.Cleanup(func() { _ = os.Chmod(f.path("run"), 0o700) })
		}, "runtime", levelFail, "cli_doctor_runtime_readonly", "", []string{"0500"}},
		{
			"runtime open", func(f *fixture) { f.mkdir("run", 0o755) }, "runtime", levelWarn,
			"cli_doctor_runtime_open", "cli_doctor_fix_chmod",
			[]string{"0755"},
		},
		{"runtime of someone else", func(f *fixture) {
			f.mkdir("run", 0o770)
			f.env.UID = os.Getuid() + 1
		}, "runtime", levelWarn, "cli_doctor_runtime_foreign", "", nil},
		{"temporary directory without sticky bit", func(f *fixture) {
			f.vars["XDG_RUNTIME_DIR"] = ""
			f.mkdir("tmp", 0o777)
		}, "runtime", levelWarn, "cli_doctor_runtime_sticky", "cli_doctor_fix_sticky", []string{"0777"}},
		{"temporary directory with sticky bit", func(f *fixture) {
			f.vars["XDG_RUNTIME_DIR"] = ""

			if err := os.Chmod(f.path("tmp"), 0o777|os.ModeSticky); err != nil {
				f.t.Fatal(err)
			}
		}, "runtime", levelOK, "cli_doctor_runtime_ok", "", nil},

		{"no leftover", noop, "handoff", levelOK, "cli_doctor_handoff_ok", "", []string{"~/run"}},
		{
			"leftover", stale("tote.abcd1234"), "handoff", levelWarn, "cli_doctor_handoff_stale",
			"cli_doctor_fix_stale",
			[]string{"1 ~/run", "cli_doctor_handoff_item ~/run/tote.abcd1234"},
		},
		{"recent file", func(f *fixture) {
			f.write("run/tote.abcd1234", "", 0o600)
			f.age("run/tote.abcd1234", 5*time.Minute)
		}, "handoff", levelOK, "cli_doctor_handoff_ok", "", nil},
		{"file of this run", func(f *fixture) {
			stale("tote.abcd1234")(f)
			f.vars["TOTE_HANDOFF"] = f.path("run/tote.abcd1234")
		}, "handoff", levelOK, "cli_doctor_handoff_ok", "", nil},
		{
			"unrelated names", stale("tote.short", "tote.abcd1234x", "other.abcd1234"), "handoff",
			levelOK, "cli_doctor_handoff_ok", "", nil,
		},
		{
			"directory named like a file", func(f *fixture) { f.mkdir("run/tote.abcd1234", 0o700) },
			"handoff", levelOK, "cli_doctor_handoff_ok", "", nil,
		},
		{
			"many leftovers", stale("tote.aaaaaaa1", "tote.aaaaaaa2", "tote.aaaaaaa3",
				"tote.aaaaaaa4",
				"tote.aaaaaaa5", "tote.aaaaaaa6", "tote.aaaaaaa7"), "handoff", levelWarn,
			"cli_doctor_handoff_stale", "",
			[]string{"7 ~/run", "cli_doctor_handoff_more 2"},
		},
		{
			"missing file of this run",
			func(f *fixture) { f.vars["TOTE_HANDOFF"] = f.path("run/gone") },
			"handoff", levelWarn, "cli_doctor_handoff_bad", "cli_doctor_fix_handoff", nil,
		},
		{"bad file and leftover", func(f *fixture) {
			stale("tote.abcd1234")(f)
			f.vars["TOTE_HANDOFF"] = f.path("run/gone")
		}, "handoff", levelWarn, "cli_doctor_handoff_bad", "", []string{"cli_doctor_handoff_stale"}},
	}
}

// updateScenarios covers the cache of the update check.
func updateScenarios() []scenario {
	cache := func(text string) func(*fixture) {
		return func(f *fixture) { f.write(".cache/tote/update-check.json", text, 0o600) }
	}

	now := time.Now()

	return []scenario{
		{"no check yet", func(*fixture) {}, "update", levelOK, "cli_doctor_update_never", "", nil},
		{"no release key", func(f *fixture) {
			f.env.Keys = func() ([]update.PublicKey, error) { return nil, update.ErrNoKey }
		}, "update", levelWarn, "cli_doctor_update_nokey", "cli_doctor_fix_nokey", nil},
		{"no release key in a development build", func(f *fixture) {
			f.env.Version = "dev"
			f.env.Keys = func() ([]update.PublicKey, error) { return nil, update.ErrNoKey }
		}, "update", levelOK, "cli_doctor_update_never", "", nil},
		{
			"check off", func(f *fixture) {
				f.conf("config.toml",
					"schema = 1\n[update]\ncheck = false\n")
			},
			"update", levelOK, "cli_doctor_update_off", "", nil,
		},
		{
			"update available", cache(cacheFile(now, "stable", "1.3.0")), "update", levelOK,
			"cli_doctor_update_available", "",
			[]string{"1.3.0"},
		},
		{
			"up to date", cache(cacheFile(now, "stable", "1.2.3")), "update", levelOK,
			"cli_doctor_update_current", "", nil,
		},
		{
			"failed check", cache(cacheFile(now, "stable", "")), "update", levelOK,
			"cli_doctor_update_failed", "", nil,
		},
		{
			"corrupt cache", cache("{"), "update", levelWarn, "cli_doctor_update_unreadable",
			"cli_doctor_fix_cache",
			[]string{"~/.cache/tote/update-check.json"},
		},
		{
			"cache of another channel", cache(cacheFile(now, "beta", "1.3.0-beta.1")), "update",
			levelWarn, "cli_doctor_update_unreadable", "", nil,
		},
		{
			"cache in the future", cache(cacheFile(now.Add(48*time.Hour), "stable", "1.3.0")),
			"update",
			levelWarn, "cli_doctor_update_future", "cli_doctor_fix_clock", nil,
		},
		{"cache of a beta build", func(f *fixture) {
			f.env.Version = "1.3.0-beta.1"
			cache(cacheFile(now, "beta", "1.3.0-beta.2"))(f)
		}, "update", levelOK, "cli_doctor_update_available", "", []string{"1.3.0-beta.2"}},
		{"unreadable cache", func(f *fixture) {
			notRoot(f.t)
			f.mkdir(".cache/tote", 0o000)
			f.t.Cleanup(func() { _ = os.Chmod(f.path(".cache/tote"), 0o700) })
		}, "update", levelWarn, "cli_doctor_update_unreadable", "", nil},
	}
}

// TestHandoffDir checks the choice of the hand-off directory, which must be the wrapper's.
//
// Parameters:
//   - t: test to report to
func TestHandoffDir(t *testing.T) {
	tests := []struct {
		xdg, tmp string
		want     string
		private  bool
	}{
		{"/run/user/1", "/var/tmp", "/run/user/1", true},
		{"", "/var/tmp", "/var/tmp", false},
		{"", "", "/tmp", false},
	}

	for _, tt := range tests {
		f := newFixture(t)
		f.vars["XDG_RUNTIME_DIR"], f.vars["TMPDIR"] = tt.xdg, tt.tmp

		d := &diagnosis{env: f.env}
		if got, private := d.handoffDir(); got != tt.want || private != tt.private {
			t.Errorf("handoffDir(%q, %q) = %q, %v", tt.xdg, tt.tmp, got, private)
		}
	}
}

// TestHelpers checks the small pure functions of the checks.
//
// Parameters:
//   - t: test to report to
func TestHelpers(t *testing.T) {
	d := &diagnosis{env: diagEnv{Home: "/home/u"}}

	for in, want := range map[string]string{
		"/home/u": "~", "/home/u/.config/tote": "~/.config/tote", "/home/user/x": "/home/user/x",
		"/etc": "/etc",
	} {
		if got := d.tilde(in); got != want {
			t.Errorf("tilde(%q) = %q, want %q", in, got, want)
		}
	}

	for in, want := range map[string]string{
		"/home/u/a b":   "'/home/u/a b'",
		"/home/u/it's":  `'/home/u/it'\''s'`,
		"/home/u/x":     "~/x",
		"/srv/data":     "/srv/data",
		"/srv/da ta":    "'/srv/da ta'",
		"/home/u/$HOME": `'/home/u/$HOME'`,
	} {
		if got := d.word(in); got != want {
			t.Errorf("word(%q) = %q, want %q", in, got, want)
		}
	}

	if got := quoteWord(""); got != "''" {
		t.Errorf("quoteWord(\"\") = %q", got)
	}

	want := "open ~/.config/tote/x: denied, /home/user/y"

	if got := d.scrub("open /home/u/.config/tote/x: denied, /home/user/y"); got != want {
		t.Errorf("scrub = %q, want %q", got, want)
	}

	if got := (&diagnosis{}).scrub("open /a/b"); got != "open /a/b" {
		t.Errorf("scrub without a home = %q", got)
	}

	if got := filepath.Base(d.word("/x/y")); got != "y" {
		t.Errorf("word of a plain path = %q", got)
	}
}
