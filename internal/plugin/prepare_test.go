package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPrepareRequiresEnabled is test P1: a plugin runs only if the lock record, the matching tree,
// the covering consent and surface, the kill switches, the compatibility and --no-plugins all
// hold. Each condition is broken on its own and Prepare must refuse with the state it computes.
//
// Parameters:
//   - t: test to report to
func TestPrepareRequiresEnabled(t *testing.T) {
	call := Call{
		Plugin: "demo", Table: TableCommand, Entry: "echo",
		Values: map[string]string{"text": "hello"},
	}

	tests := []struct {
		name   string
		break_ func(t *testing.T, l *lab, rec LockRecord)
		opts   func(*Options)
		state  State
	}{
		{name: "intact", state: StateEnabled},
		{name: "lock flag", state: StateDisabled, break_: func(t *testing.T, l *lab, _ LockRecord) {
			must(t, l.lock().SetDisabled("demo", true))
		}},
		{name: "config switch", state: StateDisabled, break_: func(t *testing.T, l *lab, _ LockRecord) {
			l.writeConfig("[plugins.demo]\nenabled = false\n")
		}},
		{name: "session switch", state: StateDisabled, opts: func(o *Options) { o.NoPlugins = true }},
		{
			name: "never consented", state: StateNeedsConsent,
			break_: func(t *testing.T, l *lab, _ LockRecord) {
				must(t, l.lock().Update("demo", func(r *LockRecord) {
					r.ConsentedAt, r.ConsentedBy = "", ""
				}))
			},
		},
		{
			name: "consent does not cover the caps", state: StateNeedsConsent,
			break_: func(t *testing.T, l *lab, _ LockRecord) {
				must(t, l.lock().Update("demo", func(r *LockRecord) { r.Caps = nil }))
			},
		},
		{
			name: "surface changed", state: StateNeedsConsent,
			break_: func(t *testing.T, l *lab, _ LockRecord) {
				must(t, l.lock().Update("demo", func(r *LockRecord) {
					r.SurfaceSHA256 = "00000000000000000000000000000000000000000000000000000000000000ff"
				}))
			},
		},
		{
			name: "manifest modified", state: StateBroken,
			break_: func(t *testing.T, l *lab, rec LockRecord) {
				dir := l.treeDir(rec)
				l.open(dir)
				l.open(filepath.Join(dir, "plugin.toml"))
				data, err := os.ReadFile(filepath.Join(dir, "plugin.toml"))
				must(t, err)
				must(t, os.WriteFile(filepath.Join(dir, "plugin.toml"),
					append(data, []byte("\n# tampered\n")...), 0o600))
			},
		},
		{
			name: "extra file in the tree", state: StateBroken,
			break_: func(t *testing.T, l *lab, rec LockRecord) {
				dir := l.treeDir(rec)
				l.open(dir)
				must(t, os.WriteFile(filepath.Join(dir, "extra"), []byte("x"), 0o600))
			},
		},
		{name: "tree moved away", state: StateBroken, break_: func(t *testing.T, l *lab, rec LockRecord) {
			dir := l.treeDir(rec)
			l.open(filepath.Dir(dir))
			must(t, os.Rename(dir, dir+".gone"))
		}},
		{name: "no lock record", state: StateBroken, break_: func(t *testing.T, l *lab, _ LockRecord) {
			must(t, l.lock().Delete("demo"))
		}},
		{name: "quarantined", state: StateQuarantined, break_: func(t *testing.T, l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.Quarantined = true }))
		}},
		{
			name: "lock does not parse", state: StateBroken,
			break_: func(t *testing.T, l *lab, _ LockRecord) {
				l.writeFile(l.paths.LockFile(), "schema = 1\n[[plugin]\n")
			},
		},
		{
			name: "config does not parse", state: StateBroken,
			break_: func(t *testing.T, l *lab, _ LockRecord) { l.writeConfig("[[[\n") },
		},
		{name: "host too old", state: StateIncompatible, opts: func(o *Options) {
			o.Host = NewHost("0.9.0")
			o.Host.OS, o.Host.Arch = "linux", "amd64"
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			l.useSystemTools()
			l.script("tool", "exit 0\n")
			rec := l.plug("demo", []string{"exec:tool"}, toolBody)

			if tt.break_ != nil {
				tt.break_(t, l, rec)
			}

			r := l.reg(tt.opts)

			if tt.state == StateEnabled {
				prepare(t, r, call)

				return
			}

			e := refusal(t, r, call)

			if e.Code != CodeNotRunnable || e.Args[1] != string(tt.state) {
				t.Errorf("got %s %v want %s %s", e.Code, e.Args, CodeNotRunnable, tt.state)
			}

			if ExitOf(e) != ExitPolicy {
				t.Errorf("exit %d want %d", ExitOf(e), ExitPolicy)
			}
		})
	}
}

// TestPrepareMissingDependency checks that a required program that is not installed refuses the
// call through the state, and that an optional or undeclared one is reported with its hint when
// the call reaches the lookup.
//
// Parameters:
//   - t: test to report to
func TestPrepareMissingDependency(t *testing.T) {
	body := `
[[requires]]
bin = "needed"
hint = "Install needed with your package manager"

[[requires]]
bin = "wanted"
hint = "Install wanted"
optional = true

[[command]]
id = "a"
label = "A"
run = ["wanted"]
output = "pager"

[[command]]
id = "b"
label = "B"
run = ["needed"]
output = "pager"

[[command]]
id = "c"
label = "C"
run = ["undeclared"]
output = "pager"
`

	l := newLab(t)
	l.useSystemTools()
	l.plug("demo", []string{"exec:needed", "exec:wanted", "exec:undeclared"}, body)

	r := l.reg(nil)

	e := refusal(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "b"})
	if e.Code != CodeNotRunnable || e.Args[1] != string(StateMissingDependency) {
		t.Errorf("required: %s %v", e.Code, e.Args)
	}

	l.script("needed", "exit 0\n")

	e = refusal(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "a"})
	if e.Code != CodeDepMissing || e.Args[0] != "wanted" || e.Args[1] != "Install wanted" {
		t.Errorf("optional: %s %v", e.Code, e.Args)
	}

	if ExitOf(e) != ExitCannotStart {
		t.Errorf("exit %d want %d", ExitOf(e), ExitCannotStart)
	}

	e = refusal(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "c"})
	if e.Code != CodeDepMissing || e.Args[0] != "undeclared" || e.Args[1] != "" {
		t.Errorf("undeclared: %s %v", e.Code, e.Args)
	}

	prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "b"})
}

// TestPrepareRereads checks that Prepare reads the lock and config.toml again on every call, with
// the same registry: a disable made in another terminal is seen by the next Prepare.
//
// Parameters:
//   - t: test to report to
func TestPrepareRereads(t *testing.T) {
	l, r := toolLab(t)
	call := Call{
		Plugin: "demo", Table: TableCommand, Entry: "echo",
		Values: map[string]string{"text": "hello"},
	}

	prepare(t, r, call)

	must(t, l.lock().SetDisabled("demo", true))

	if e := refusal(t, r, call); e.Code != CodeNotRunnable || e.Args[1] != string(StateDisabled) {
		t.Errorf("after the lock flag: %v", e)
	}

	must(t, l.lock().SetDisabled("demo", false))
	prepare(t, r, call)

	l.writeConfig("plugins.demo.enabled = false\n")

	if e := refusal(t, r, call); e.Args[1] != string(StateDisabled) {
		t.Errorf("after config.toml: %v", e)
	}

	l.writeConfig("")
	prepare(t, r, call)
}

// TestPrepareProgramLookup checks where the program may come from: the scrubbed PATH only. A
// relative or empty entry, the working directory and the plugins directory are never searched.
//
// Parameters:
//   - t: test to report to
func TestPrepareProgramLookup(t *testing.T) {
	l := newLab(t)
	cwd := l.cwd()

	for _, d := range []string{cwd, filepath.Join(l.root, "elsewhere")} {
		must(t, os.MkdirAll(d, 0o700))
	}

	l.plug("demo", []string{"exec:tool"}, toolBody)

	hostile := "#!/bin/sh\nexit 0\n"
	must(t, os.WriteFile(filepath.Join(cwd, "tool"), []byte(hostile), 0o700))

	call := Call{
		Plugin: "demo", Table: TableCommand, Entry: "echo",
		Values: map[string]string{"text": "hello"},
	}

	t.Run("relative entries and the working directory", func(t *testing.T) {
		l.env["PATH"] = ".::bin:" + cwd + ":" + filepath.Join(l.root, "nowhere")

		if e := refusal(t, l.reg(nil), call); e.Code != CodeDepMissing {
			t.Errorf("got %v", e)
		}
	})

	t.Run("the plugins directory", func(t *testing.T) {
		dir := filepath.Join(l.paths.PluginsDir(), "demo")
		l.env["PATH"] = dir

		if e := refusal(t, l.reg(nil), call); e.Code != CodeDepMissing {
			t.Errorf("got %v", e)
		}
	})

	t.Run("a directory that holds the program", func(t *testing.T) {
		good := filepath.Join(l.root, "elsewhere")
		must(t, os.WriteFile(filepath.Join(good, "tool"), []byte(hostile), 0o700))

		l.env["PATH"] = ".:" + cwd + ":" + good

		p := prepare(t, l.reg(nil), call)
		if p.Argv[0] != filepath.Join(good, "tool") {
			t.Errorf("program %q", p.Argv[0])
		}

		if got := envMap(p.Env)["PATH"]; got != good {
			t.Errorf("PATH of the program = %q want %q", got, good)
		}
	})

	t.Run("the program is not executable", func(t *testing.T) {
		good := filepath.Join(l.root, "plain")
		must(t, os.MkdirAll(good, 0o700))
		must(t, os.WriteFile(filepath.Join(good, "tool"), []byte(hostile), 0o600))

		l.env["PATH"] = good

		if e := refusal(t, l.reg(nil), call); e.Code != CodeDepMissing {
			t.Errorf("got %v", e)
		}
	})
}
