package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withVersion changes the version of a manifest written by the helpers.
//
// Parameters:
//   - src: manifest text
//   - version: new version
func withVersion(src, version string) string {
	return strings.Replace(src, `version = "1.0.0"`, `version = "`+version+`"`, 1)
}

// seeded installs the first version of a plugin from a source directory and returns the registry
// and the directory, which later versions overwrite in place.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
//   - first: manifest of the installed version
func seeded(t *testing.T, l *lab, first string) (*Registry, string) {
	t.Helper()

	l.fakeBin("tool")

	dir := l.srcDir("demo", map[string]string{"plugin.toml": first})
	r := l.reg(nil)
	installDir(t, r, dir)

	return r, dir
}

// rewrite replaces the manifest of a source directory.
//
// Parameters:
//   - t: test to report to
//   - dir: source directory
//   - src: new manifest
func rewrite(t *testing.T, dir, src string) {
	t.Helper()

	must(t, os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(src), 0o644))
}

// TestUpdateEscalationMatrix is the escalation matrix at update level. For each pair of versions
// it checks whether the update asks for a new consent and why, that Commit refuses without a
// grant when it asks and goes through silently when it does not, and that the record and the state
// after the commit are the right ones: the consent is kept or renewed, the capabilities and the
// surface are those of the new version, and the plugin is enabled, not stuck in needs-consent.
//
// Parameters:
//   - t: test to report to
func TestUpdateEscalationMatrix(t *testing.T) {
	plain := surfaceBase("", "", "")
	confirmed := surfaceBase("confirm = true\n", "", "")
	dash := surfaceBase("", "allow_dash = [\"x\"]\n", "")
	env := surfaceBase("", "", "[command.env]\nA = \"1\"\n")
	changed := strings.Replace(plain, `run = ["tool", "{x}"]`, `run = ["tool", "--other", "{x}"]`, 1)
	moreCmds := plain + cmdWith("second", "confirm = true\n")

	net := func(perms ...string) string {
		return manifest("demo", "1.0.0", append([]string{"exec:tool"}, perms...), cmdWith("go", ""))
	}

	tests := []struct {
		name string
		old  string
		next string
		asks bool
		why  string
	}{
		{"same content", plain, plain, false, ""},
		{"command changed", plain, changed, false, ""},
		{"command added with a confirm", plain, moreCmds, false, ""},
		{"confirm removed", confirmed, plain, true, WhySurface},
		{"confirm turned into a text", confirmed, strings.Replace(confirmed, "confirm = true",
			"confirm = \"Sure?\"", 1), false, ""},
		{"confirm kept", confirmed, confirmed, false, ""},
		{"confirm added", plain, confirmed, false, ""},
		{"allow_dash added", plain, dash, true, WhySurface},
		{"allow_dash kept", dash, dash, false, ""},
		{"allow_dash removed", dash, plain, false, ""},
		{"env name added", plain, env, true, WhySurface},
		{"env name kept", env, env, false, ""},
		{"capability added", net(), net("net:example.com"), true, WhyCaps},
		{"capability dropped", net("net:example.com"), net(), false, ""},
		{
			"wildcard covers a subdomain", net("net:*.example.com"), net("net:api.example.com"),
			false, "",
		},
		{
			"subdomain does not cover the wildcard", net("net:api.example.com"),
			net("net:*.example.com"), true, WhyCaps,
		},
		{
			"write does not cover read", net("fs:write:cwd"), net("fs:write:cwd", "fs:read:cwd"),
			true, WhyCaps,
		},
		{"terminal adds derived:tty", net(), strings.Replace(net(), `output = "pager"`,
			`output = "terminal"`, 1), true, WhyCaps},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			r, dir := seeded(t, l, tt.old)

			before, _ := l.lock().Get("demo")

			rewrite(t, dir, withVersion(tt.next, "1.1.0"))

			st := plan(t, r, InstallRequest{Source: dir})
			d := st.Diff()

			if !d.Update || d.OldVersion != "1.0.0" || d.Version != "1.1.0" {
				t.Fatalf("diff = %+v", d)
			}

			if d.NeedsConsent != tt.asks {
				t.Fatalf("NeedsConsent = %v (why %v, added %v, surface %v), want %v", d.NeedsConsent,
					d.Why, d.Added, d.SurfaceChanges, tt.asks)
			}

			if tt.asks && !contains(d.Why, tt.why) {
				t.Errorf("why = %v, want %s", d.Why, tt.why)
			}

			if !tt.asks && len(d.Why) != 0 {
				t.Errorf("why = %v for a silent update", d.Why)
			}

			if tt.asks {
				if _, err := st.Commit(Grant{}); asError(t, err).Code != CodeConsentNeeded {
					t.Fatalf("Commit without consent = %v", err)
				}

				if cur, _ := l.lock().Get("demo"); cur.Version != "1.0.0" {
					t.Fatal("a refused update changed the record")
				}
			}

			res, err := st.Commit(Grant{Token: d.Grant})
			if err != nil {
				t.Fatal(err)
			}

			after, _ := l.lock().Get("demo")

			if res.State != StateEnabled || after.Version != "1.1.0" || !res.Updated ||
				res.OldVersion != "1.0.0" {
				t.Fatalf("result = %+v record %+v", res, after)
			}

			if tt.asks {
				if res.ConsentedBy != ConsentGrant || after.ConsentedBy != ConsentGrant {
					t.Errorf("consent not renewed: %+v", after)
				}
			} else if res.ConsentedBy != "" || after.ConsentedAt != before.ConsentedAt ||
				after.ConsentedBy != before.ConsentedBy {
				t.Errorf("a silent update changed the consent: %+v", after)
			}

			if after.SurfaceSHA256 != d.SurfaceSHA256 || strings.Join(after.Caps, ",") !=
				strings.Join(d.Caps, ",") || after.InstalledAt != before.InstalledAt {
				t.Errorf("record = %+v, diff %+v", after, d)
			}
		})
	}
}

// TestUpdateUnverifiedOldAsks checks that an update over a tree that no longer verifies asks for
// consent: with no trusted old version there is nothing to compare, and nothing is assumed.
//
// Parameters:
//   - t: test to report to
func TestUpdateUnverifiedOldAsks(t *testing.T) {
	l := newLab(t)
	plain := surfaceBase("", "", "")
	r, dir := seeded(t, l, plain)

	rec, _ := l.lock().Get("demo")
	l.open(l.treeDir(rec))
	l.open(filepath.Join(l.treeDir(rec), "plugin.toml"))
	must(t, os.WriteFile(filepath.Join(l.treeDir(rec), "plugin.toml"), []byte("x"), 0o600))

	rewrite(t, dir, withVersion(plain, "1.1.0"))

	d := plan(t, r, InstallRequest{Source: dir}).Diff()
	if !d.NeedsConsent || !contains(d.Why, WhyUnverified) {
		t.Errorf("diff = %+v", d)
	}
}

// TestUpdateUnconsentedAsks checks that an installed plugin that never had consent asks on update
// even when nothing escalates.
//
// Parameters:
//   - t: test to report to
func TestUpdateUnconsentedAsks(t *testing.T) {
	l := newLab(t)
	plain := surfaceBase("", "", "")
	r, dir := seeded(t, l, plain)

	must(t, l.lock().Update("demo", func(rec *LockRecord) {
		rec.ConsentedAt, rec.ConsentedBy = "", ""
	}))

	rewrite(t, dir, withVersion(plain, "1.1.0"))

	d := plan(t, r, InstallRequest{Source: dir}).Diff()
	if !d.NeedsConsent || !contains(d.Why, WhyUnconsent) {
		t.Errorf("diff = %+v", d)
	}
}

// TestUpdateRules checks the version and origin rules: a version that is not newer, a pre-release
// and a source that changed are refused, with --force they are accepted and reported, and an id
// that is installed is updated even when the verb was install. An install of the same version
// with the same files is not an update at all (TestInstallIdempotent), so the equal case is asked
// as an update.
//
// Parameters:
//   - t: test to report to
func TestUpdateRules(t *testing.T) {
	plain := surfaceBase("", "", "")

	tests := []struct {
		name    string
		version string
		code    string
		warn    string
		update  bool
	}{
		{"newer", "1.0.1", "", "", false},
		{"equal", "1.0.0", CodeDowngrade, WarnSameVersion, true},
		{"older", "0.9.0", CodeDowngrade, WarnDowngrade, false},
		{"pre-release of a newer version", "1.1.0-beta.1", CodePrerelease, WarnPrerelease, false},
		{"pre-release of the same version", "1.0.0-rc.1", CodeDowngrade, WarnDowngrade, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			r, dir := seeded(t, l, plain)

			rewrite(t, dir, withVersion(plain, tt.version))

			req := InstallRequest{Source: dir, Update: tt.update}

			if tt.code == "" {
				if d := plan(t, r, req).Diff(); !d.Update {
					t.Errorf("an install of an installed id is not an update: %+v", d)
				}

				return
			}

			if pe := planErr(t, r, req); pe.Code != tt.code || ExitOf(pe) != ExitFailure {
				t.Fatalf("error = %v", pe)
			}

			if names := stagingNames(t, l); len(names) != 0 {
				t.Errorf("staging = %v", names)
			}

			req.Force = true

			st := plan(t, r, req)
			if !hasReason(st.Diff().Warnings, tt.warn) {
				t.Errorf("warnings = %v, want %s", st.Diff().Warnings, tt.warn)
			}

			if _, err := st.Commit(Grant{Prompt: true}); err != nil {
				t.Fatal(err)
			}

			if rec, _ := l.lock().Get("demo"); rec.Version != tt.version {
				t.Errorf("version = %s", rec.Version)
			}
		})
	}
}

// TestUpdateSourceChanged checks the same-origin rule for a directory: another directory is
// another source, refused without --force and reported with it.
//
// Parameters:
//   - t: test to report to
func TestUpdateSourceChanged(t *testing.T) {
	l := newLab(t)
	plain := surfaceBase("", "", "")
	r, _ := seeded(t, l, plain)

	other := l.srcDir("other", map[string]string{"plugin.toml": withVersion(plain, "1.1.0")})

	if pe := planErr(t, r, InstallRequest{Source: other}); pe.Code != CodeSourceChanged {
		t.Fatalf("error = %v", pe)
	}

	st := plan(t, r, InstallRequest{Source: other, Force: true})
	if !hasReason(st.Diff().Warnings, WarnSourceChanged) {
		t.Errorf("warnings = %v", st.Diff().Warnings)
	}

	if _, err := st.Commit(Grant{Prompt: true}); err != nil {
		t.Fatal(err)
	}

	if rec, _ := l.lock().Get("demo"); rec.Source != other {
		t.Errorf("source = %s", rec.Source)
	}
}

// TestUpdateFromRecordedSource checks that an update with no source reads a local plugin from the
// path on record, a bundled one from the embedded copy, and asks for a source for a release.
//
// Parameters:
//   - t: test to report to
func TestUpdateFromRecordedSource(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		l := newLab(t)
		plain := surfaceBase("", "", "")
		r, dir := seeded(t, l, plain)

		rewrite(t, dir, withVersion(plain, "1.2.0"))

		st := plan(t, r, InstallRequest{ID: "demo", Update: true})
		if _, err := st.Commit(Grant{}); err != nil {
			t.Fatal(err)
		}

		if rec, _ := l.lock().Get("demo"); rec.Version != "1.2.0" {
			t.Errorf("version = %s", rec.Version)
		}
	})

	t.Run("bundled", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")

		one := bundle(t, l, map[string]string{"plugin.toml": withVersion(surfaceBase("", "", ""),
			"1.0.0")})
		r := l.reg(func(o *Options) { o.Bundled = one })
		st := plan(t, r, InstallRequest{Source: "demo"})

		if _, err := st.Commit(Grant{Prompt: true}); err != nil {
			t.Fatal(err)
		}

		if pe := planErr(t, r, InstallRequest{ID: "demo", Update: true}); pe.Code != CodeDowngrade {
			t.Errorf("same embedded copy: %v", pe)
		}

		two := bundle(t, l, map[string]string{"plugin.toml": withVersion(surfaceBase("", "", ""),
			"1.3.0")})
		r2 := l.reg(func(o *Options) { o.Bundled = two })

		st = plan(t, r2, InstallRequest{ID: "demo", Update: true})
		if _, err := st.Commit(Grant{}); err != nil {
			t.Fatal(err)
		}

		if rec, _ := l.lock().Get("demo"); rec.Version != "1.3.0" || rec.Kind != KindBundled {
			t.Errorf("record = %+v", rec)
		}
	})

	t.Run("release", func(t *testing.T) {
		l := newLab(t)
		r, _ := seeded(t, l, surfaceBase("", "", ""))

		url := "https://github.com/o/r/releases/download/1.0.0/p.tar.gz"
		must(t, l.lock().Update("demo", func(rec *LockRecord) {
			rec.Kind, rec.Source, rec.Origin = KindRelease, url, "o/r"
			rec.ArchiveSHA256 = strings.Repeat("a", 64)
		}))

		pe := planErr(t, r, InstallRequest{ID: "demo", Update: true})
		if pe.Code != CodeSourceRequired || ExitOf(pe) != ExitUsage || len(pe.Args) != 2 ||
			pe.Args[1] != url {
			t.Errorf("error = %v", pe)
		}
	})

	t.Run("not installed", func(t *testing.T) {
		l := newLab(t)

		if pe := planErr(t, l.reg(nil), InstallRequest{ID: "demo", Update: true}); pe.Code !=
			CodeNotInstalled {
			t.Errorf("error = %v", pe)
		}

		src := demoSource(l, "demo", "1.0.0")
		if pe := planErr(t, l.reg(nil), InstallRequest{Source: src, Update: true}); pe.Code !=
			CodeNotInstalled {
			t.Errorf("update of an id that is not installed: %v", pe)
		}
	})

	t.Run("bad id", func(t *testing.T) {
		l := newLab(t)

		if pe := planErr(t, l.reg(nil), InstallRequest{ID: "../x", Update: true}); pe.Code != CodeBadID ||
			ExitOf(pe) != ExitUsage {
			t.Errorf("error = %v", pe)
		}
	})
}

// TestUpdateKeepsWhatTheUserSet checks what survives an update: the kill switch, the time of the
// install, the settings, the state and cache of the plugin; and what a new version clears: the
// quarantine and the failure counters. The previous tree is gone, and the state is the new one.
//
// Parameters:
//   - t: test to report to
func TestUpdateKeepsWhatTheUserSet(t *testing.T) {
	l := newLab(t)
	plain := surfaceBase("", "", "")
	r, dir := seeded(t, l, plain)

	first, _ := l.lock().Get("demo")
	oldTree := l.treeDir(first)

	settings, _ := l.paths.PluginSettingsFile("demo")
	l.writeFile(settings, "schema = 1\nowner = \"me\"\n")

	state, _ := l.paths.PluginStateDir("demo")
	must(t, os.WriteFile(filepath.Join(state, "data"), []byte("kept"), 0o600))

	must(t, l.lock().Update("demo", func(rec *LockRecord) {
		rec.Disabled, rec.Quarantined, rec.Failures = true, true, 3
		rec.LastReason, rec.LastFailureAt = FailCrash, rec.InstalledAt
	}))

	rewrite(t, dir, withVersion(plain, "1.1.0"))

	st := plan(t, r, InstallRequest{Source: dir})
	if _, err := st.Commit(Grant{}); err != nil {
		t.Fatal(err)
	}

	rec, _ := l.lock().Get("demo")

	if !rec.Disabled || rec.Quarantined || rec.Failures != 0 || rec.LastReason != "" ||
		rec.LastFailureAt != "" || rec.InstalledAt != first.InstalledAt {
		t.Errorf("record = %+v", rec)
	}

	if p, _ := r.Recheck("demo"); p.State != StateDisabled || !hasReason(p.Reasons, ReasonLockOff) {
		t.Errorf("state %s %v", p.State, reasonCodes(p.Reasons))
	}

	if data, err := os.ReadFile(settings); err != nil || !strings.Contains(string(data), "me") {
		t.Errorf("settings = %q %v", data, err)
	}

	if data, err := os.ReadFile(filepath.Join(state, "data")); err != nil || string(data) != "kept" {
		t.Errorf("state = %q %v", data, err)
	}

	if _, err := os.Lstat(oldTree); !os.IsNotExist(err) {
		t.Errorf("the previous tree is still there: %v", err)
	}

	if names := stagingNames(t, l); len(names) != 0 {
		t.Errorf("staging = %v", names)
	}

	idDir, _ := l.paths.PluginDir("demo")
	if entries, _ := os.ReadDir(idDir); len(entries) != 1 {
		t.Errorf("trees = %d", len(entries))
	}
}

// TestCommitStaleBase checks that a Commit whose Diff was computed against a record that has
// changed since is refused with stale, and so is the second of two installs of a new id.
//
// Parameters:
//   - t: test to report to
func TestCommitStaleBase(t *testing.T) {
	t.Run("two updates", func(t *testing.T) {
		l := newLab(t)
		plain := surfaceBase("", "", "")
		r, dir := seeded(t, l, plain)

		rewrite(t, dir, withVersion(plain, "1.1.0"))

		a := plan(t, r, InstallRequest{Source: dir})
		b := plan(t, r, InstallRequest{Source: dir})

		if _, err := a.Commit(Grant{}); err != nil {
			t.Fatal(err)
		}

		if _, err := b.Commit(Grant{}); asError(t, err).Code != CodeStale {
			t.Errorf("second commit = %v", err)
		}

		if rec, _ := l.lock().Get("demo"); rec.Version != "1.1.0" {
			t.Errorf("record = %+v", rec)
		}

		if names := stagingNames(t, l); len(names) != 0 {
			t.Errorf("staging = %v", names)
		}
	})

	t.Run("two new installs", func(t *testing.T) {
		l := newLab(t)
		src := demoSource(l, "demo", "1.0.0")
		r := l.reg(nil)

		a := plan(t, r, InstallRequest{Source: src})
		b := plan(t, r, InstallRequest{Source: src})

		if _, err := a.Commit(Grant{Prompt: true}); err != nil {
			t.Fatal(err)
		}

		if _, err := b.Commit(Grant{Prompt: true}); asError(t, err).Code != CodeStale {
			t.Errorf("second commit = %v", err)
		}
	})

	t.Run("consent changed", func(t *testing.T) {
		l := newLab(t)
		plain := surfaceBase("", "", "")
		r, dir := seeded(t, l, plain)

		rewrite(t, dir, withVersion(plain, "1.1.0"))

		st := plan(t, r, InstallRequest{Source: dir})

		must(t, l.lock().Update("demo", func(rec *LockRecord) {
			rec.ConsentedAt = "2020-01-01T00:00:00Z"
		}))

		if _, err := st.Commit(Grant{}); asError(t, err).Code != CodeStale {
			t.Errorf("commit = %v", err)
		}
	})
}
