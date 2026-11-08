package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// TestFailClosed checks that a lock or a config.toml that does not parse, or that has another
// schema, makes every plugin on disk broken, with the failure in the snapshot, and that the lock
// flag still wins when the lock is readable but config.toml is not.
//
// Parameters:
//   - t: test to report to
func TestFailClosed(t *testing.T) {
	t.Run("lock does not parse", func(t *testing.T) {
		l := newLab(t)
		l.install("demo", demoFiles("demo", ""))
		l.writeFile(l.paths.LockFile(), "schema = 1\n[[plugin]\n")

		snap := l.registry(nil).Load()
		p, _ := snap.Get("demo")

		if p.State != StateBroken || !hasReason(p.Reasons, ReasonLockUnreadable) || p.Installed {
			t.Errorf("state %s reasons %v", p.State, reasonCodes(p.Reasons))
		}

		if len(snap.Problems) != 1 || snap.Problems[0].Code != ReasonLockUnreadable {
			t.Errorf("problems = %v", snap.Problems)
		}
	})

	t.Run("config does not parse", func(t *testing.T) {
		l := newLab(t)
		l.install("demo", demoFiles("demo", ""))
		l.writeConfig("[plugins.demo\nenabled = false\n")

		snap := l.registry(nil).Load()
		p, _ := snap.Get("demo")

		if p.State != StateBroken || !hasReason(p.Reasons, ReasonConfigUnreadable) {
			t.Errorf("state %s reasons %v", p.State, reasonCodes(p.Reasons))
		}

		if len(snap.Problems) != 1 || snap.Problems[0].Code != ReasonConfigUnreadable {
			t.Errorf("problems = %v", snap.Problems)
		}
	})

	t.Run("config has an unknown key", func(t *testing.T) {
		// A key written by a later release is a warning, not a reason to stop every plugin.
		l := newLab(t)
		l.install("demo", demoFiles("demo", ""))
		l.writeConfig("surprise = 1\n")

		if p := load(t, l, "demo"); p.State != StateEnabled {
			t.Errorf("state %s", p.State)
		}
	})

	t.Run("config has another schema", func(t *testing.T) {
		l := newLab(t)
		l.install("demo", demoFiles("demo", ""))
		l.writeConfig("schema = 2\n")

		p := load(t, l, "demo")
		if p.State != StateBroken || !hasReason(p.Reasons, ReasonConfigUnreadable) {
			t.Errorf("state %s reasons %v", p.State, reasonCodes(p.Reasons))
		}
	})

	t.Run("config is not readable and the lock disables", func(t *testing.T) {
		l := newLab(t)
		l.install("demo", demoFiles("demo", ""))
		must(t, l.lock().SetDisabled("demo", true))
		l.writeConfig("[[[\n")

		p := load(t, l, "demo")
		if p.State != StateDisabled || !hasReason(p.Reasons, ReasonConfigUnreadable) {
			t.Errorf("state %s reasons %v", p.State, reasonCodes(p.Reasons))
		}
	})

	t.Run("lock record is invalid", func(t *testing.T) {
		l := newLab(t)
		rec := l.install("demo", demoFiles("demo", ""))

		data, _ := os.ReadFile(l.paths.LockFile())
		l.writeFile(l.paths.LockFile(), strings.Replace(string(data), rec.TreeSHA256, "xyz", 1))

		p := load(t, l, "demo")
		if p.State != StateBroken || !hasReason(p.Reasons, ReasonLockRecordInvalid) || p.Installed {
			t.Errorf("state %s reasons %v", p.State, reasonCodes(p.Reasons))
		}
	})

	t.Run("config plugins is not a table", func(t *testing.T) {
		l := newLab(t)
		l.install("demo", demoFiles("demo", ""))
		l.writeConfig("plugins = false\n")

		if p := load(t, l, "demo"); p.State != StateBroken {
			t.Errorf("state %s: a switch that does nothing must fail closed", p.State)
		}
	})
}

// TestDisabledKeepsOtherReasons checks that the user's intent ranks first without hiding the
// rest: a disabled plugin that is also tampered with is disabled and lists both.
//
// Parameters:
//   - t: test to report to
func TestDisabledKeepsOtherReasons(t *testing.T) {
	l := newLab(t)
	rec := l.install("demo", demoFiles("demo", ""))
	must(t, l.lock().SetDisabled("demo", true))
	l.open(filepath.Join(l.treeDir(rec), "plugin.toml"))

	p := load(t, l, "demo")

	if p.State != StateDisabled || !hasReason(p.Reasons, ReasonLockOff) ||
		!hasReason(p.Reasons, CodeTreePerms) {
		t.Errorf("state %s reasons %v", p.State, reasonCodes(p.Reasons))
	}
}

// TestKillSwitchSurvivesAnyConfigLayout is test P9: a disable is effective whatever the layout of
// config.toml. A dotted key and an inline table are layouts that PatchTable refuses; the lock flag
// still switches the plugin off, and switching it back on restores it.
//
// Parameters:
//   - t: test to report to
func TestKillSwitchSurvivesAnyConfigLayout(t *testing.T) {
	layouts := map[string]string{
		"dotted key, enabled":         "plugins.demo.enabled = true\n",
		"inline table, enabled":       "plugins = { demo = { enabled = true } }\n",
		"table, dotted inside":        "[plugins]\ndemo.enabled = true\n",
		"table next to another table": "[plugins.demo]\nenabled = true\n[ui]\ntheme = \"auto\"\n",
		"nothing":                     "",
	}

	for name, layout := range layouts {
		t.Run(name, func(t *testing.T) {
			l := newLab(t)
			l.install("demo", demoFiles("demo", ""))
			l.writeConfig(layout)

			if p := load(t, l, "demo"); p.State != StateEnabled {
				t.Fatalf("before: %s %v", p.State, p.Reasons)
			}

			patchErr := store.PatchTable(l.paths.ConfigFile(), "plugins.demo",
				map[string]any{"enabled": false}, nil)

			if err := l.lock().SetDisabled("demo", true); err != nil {
				t.Fatal(err)
			}

			p := load(t, l, "demo")
			if p.State != StateDisabled {
				t.Fatalf("after disable (PatchTable said %v): %s %v", patchErr, p.State, p.Reasons)
			}

			if !hasReason(p.Reasons, ReasonLockOff) {
				t.Errorf("reasons = %v, want the lock flag", reasonCodes(p.Reasons))
			}

			must(t, l.lock().SetDisabled("demo", false))

			if patchErr == nil {
				must(t, store.PatchTable(l.paths.ConfigFile(), "plugins.demo", nil, []string{"enabled"}))
			}
		})
	}
}

// TestKillSwitchLayoutRefusal pins the case the specification names: a hand-edited dotted key
// that says enabled = false is read as off, PatchTable refuses to edit that layout with ErrLayout,
// and so the plugin stays off whatever the lock says, until the user edits the file.
//
// Parameters:
//   - t: test to report to
func TestKillSwitchLayoutRefusal(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", ""))
	l.writeConfig("plugins.demo.enabled = false\n")

	err := store.PatchTable(l.paths.ConfigFile(), "plugins.demo", nil, []string{"enabled"})
	if err == nil || !strings.Contains(err.Error(), "layout") {
		t.Fatalf("PatchTable = %v, want ErrLayout", err)
	}

	p := load(t, l, "demo")
	if p.State != StateDisabled || !hasReason(p.Reasons, ReasonConfigOff) {
		t.Errorf("state %s reasons %v", p.State, p.Reasons)
	}

	if hasReason(p.Reasons, ReasonLockOff) {
		t.Error("the lock flag is on although nobody set it")
	}
}

// TestTwoRegistries checks that two registries over the same directories do not share a view: a
// disable written through one is seen by Recheck and by the next Load of the other, a removal of
// the record too, and a registry never answers from a snapshot older than the files.
//
// Parameters:
//   - t: test to report to
func TestTwoRegistries(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", ""))

	one, two := l.registry(nil), l.registry(nil)

	old := one.Load()
	if p, _ := two.Load().Get("demo"); p.State != StateEnabled {
		t.Fatalf("second registry: %s", p.State)
	}

	// The second registry's process disables the plugin through its own lock handle.
	other, _ := OpenLock(l.paths.LockFile())
	must(t, other.SetDisabled("demo", true))

	if p, ok := one.Recheck("demo"); !ok || p.State != StateDisabled {
		t.Errorf("Recheck after a disable: %v %s", ok, p.State)
	}

	if p, _ := one.Load().Get("demo"); p.State != StateDisabled {
		t.Errorf("Load after a disable: %s", p.State)
	}

	if p, _ := old.Get("demo"); p.State != StateEnabled {
		t.Error("a snapshot changed after it was taken")
	}

	must(t, other.SetDisabled("demo", false))
	must(t, other.Delete("demo"))

	p, ok := one.Recheck("demo")
	if !ok || p.State != StateBroken || !hasReason(p.Reasons, ReasonNoLockRecord) {
		t.Errorf("Recheck after a removal: %v %s %v", ok, p.State, p.Reasons)
	}

	// The directory is still there, so removing it makes the plugin unknown.
	dir, _ := l.paths.PluginDir("demo")
	restoreWrite(dir)
	must(t, os.RemoveAll(dir))

	if _, ok := one.Recheck("demo"); ok {
		t.Error("Recheck found a plugin that is gone")
	}
}

// TestRecheck checks the fresh state check of the pipeline: it reads the files at that moment,
// answers for an id that is installed or at least present, and never for an id outside the
// grammar, whatever is on disk.
//
// Parameters:
//   - t: test to report to
func TestRecheck(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", ""))
	reg := l.registry(nil)

	hostile := []string{"", "../data", "a/b", "Demo", "d", "demo\x00", strings.Repeat("a", 40)}

	for _, id := range hostile {
		if _, ok := reg.Recheck(id); ok {
			t.Errorf("Recheck(%q) found something", id)
		}
	}

	if _, ok := reg.Recheck("absent"); ok {
		t.Error("Recheck found an id that is nowhere")
	}

	p, ok := reg.Recheck("demo")
	if !ok || p.State != StateEnabled {
		t.Fatalf("Recheck(demo) = %v %s", ok, p.State)
	}

	l.writeConfig("[plugins.demo]\nenabled = false\n")

	if p, _ := reg.Recheck("demo"); p.State != StateDisabled {
		t.Errorf("Recheck after editing config.toml: %s", p.State)
	}
}

// TestRunsOnlyWhenEveryConditionHolds is test P1: a plugin is runnable only if the lock record,
// the matching tree, the covering consent and surface, the absence of every kill switch and the
// compatibility all hold, and each of them is read again by Recheck, even through a registry that
// saw the plugin healthy a moment before.
//
// Parameters:
//   - t: test to report to
func TestRunsOnlyWhenEveryConditionHolds(t *testing.T) {
	flips := map[string]func(l *lab, rec LockRecord){
		"record removed": func(l *lab, _ LockRecord) { must(t, l.lock().Delete("demo")) },
		"tree altered": func(l *lab, rec LockRecord) {
			f := filepath.Join(l.treeDir(rec), "plugin.toml")
			l.open(l.treeDir(rec))
			l.open(f)
			must(t, os.WriteFile(f, []byte("changed"), 0o400))
		},
		"tree removed": func(l *lab, rec LockRecord) {
			restoreWrite(filepath.Dir(l.treeDir(rec)))
			must(t, os.RemoveAll(l.treeDir(rec)))
		},
		"consent withdrawn": func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.ConsentedAt, r.ConsentedBy = "", "" }))
		},
		"capabilities not covered": func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.Caps = []string{} }))
		},
		"surface not consented": func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.SurfaceSHA256 = sum256("x") }))
		},
		"kill switch in the lock": func(l *lab, _ LockRecord) {
			must(t, l.lock().SetDisabled("demo", true))
		},
		"kill switch in config.toml": func(l *lab, _ LockRecord) {
			l.writeConfig("[plugins.demo]\nenabled = false\n")
		},
		"quarantine": func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.Quarantined = true }))
		},
		"lock unreadable":   func(l *lab, _ LockRecord) { l.writeFile(l.paths.LockFile(), "[[[") },
		"config unreadable": func(l *lab, _ LockRecord) { l.writeConfig("[[[") },
	}

	for name, flip := range flips {
		t.Run(name, func(t *testing.T) {
			l := newLab(t)
			rec := l.install("demo", demoFiles("demo", ""))
			reg := l.registry(nil)

			if p, ok := reg.Recheck("demo"); !ok || !p.State.Runnable() {
				t.Fatalf("healthy plugin is not runnable: %s %v", p.State, p.Reasons)
			}

			flip(l, rec)

			p, ok := reg.Recheck("demo")
			if ok && p.State.Runnable() {
				t.Errorf("runnable after %q", name)
			}

			if snap := reg.Load(); snap.Count().Enabled != 0 {
				t.Errorf("Load still lists an enabled plugin after %q", name)
			}
		})
	}

	t.Run("session switch", func(t *testing.T) {
		l := newLab(t)
		l.install("demo", demoFiles("demo", ""))

		p, _ := l.registry(func(o *Options) { o.NoPlugins = true }).Recheck("demo")
		if p.State.Runnable() {
			t.Error("runnable under --no-plugins")
		}
	})
}

// TestEveryKillSwitchCombination turns the three kill switches on in every combination, the
// session flag, config.toml and the lock flag, on a healthy plugin, and checks that it is
// disabled exactly when one is on and that the reasons name exactly the ones that are.
//
// Parameters:
//   - t: test to report to
func TestEveryKillSwitchCombination(t *testing.T) {
	for bits := 0; bits < 8; bits++ {
		session, cfg, lock := bits&1 != 0, bits&2 != 0, bits&4 != 0

		l := newLab(t)
		l.install("demo", demoFiles("demo", ""))

		if cfg {
			l.writeConfig("[plugins.demo]\nenabled = false\n")
		}

		if lock {
			must(t, l.lock().SetDisabled("demo", true))
		}

		p, ok := l.registry(func(o *Options) { o.NoPlugins = session }).Recheck("demo")
		if !ok {
			t.Fatalf("bits %03b: not found", bits)
		}

		var want []string
		if session {
			want = append(want, ReasonSessionOff)
		}

		if cfg {
			want = append(want, ReasonConfigOff)
		}

		if lock {
			want = append(want, ReasonLockOff)
		}

		if (p.State == StateDisabled) != (bits != 0) || p.State.Runnable() != (bits == 0) {
			t.Errorf("bits %03b: state %s", bits, p.State)
		}

		if got := reasonCodes(p.Reasons); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("bits %03b: reasons %v, want %v", bits, got, want)
		}
	}
}
