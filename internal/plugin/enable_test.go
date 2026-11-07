package plugin

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// unconsent removes the consent of an installed plugin, as a tree whose consent was never given.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
//   - id: plugin id
func unconsent(t *testing.T, l *lab, id string) {
	t.Helper()

	must(t, l.lock().Update(id, func(r *LockRecord) { r.ConsentedAt, r.ConsentedBy = "", "" }))
}

// TestDisableEnable checks the plain cycle: disable sets both switches and keeps the tree and the
// consent, a second disable says it was already off, enable clears both and a second enable says
// there was nothing to do.
//
// Parameters:
//   - t: test to report to
func TestDisableEnable(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")
	rec := l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
	r := l.reg(nil)

	res, err := r.Disable("demo")
	if err != nil || !res.LockFlag || !res.ConfigOff || res.ConfigFile != "" || res.Already {
		t.Fatalf("Disable = %+v %v", res, err)
	}

	p, _ := r.Recheck("demo")
	if p.State != StateDisabled {
		t.Errorf("state = %s", p.State)
	}

	if got, _ := l.lock().Get("demo"); !got.Disabled || got.ConsentedAt != rec.ConsentedAt ||
		got.TreeSHA256 != rec.TreeSHA256 {
		t.Errorf("record = %+v", got)
	}

	if data, _ := os.ReadFile(l.paths.ConfigFile()); !strings.Contains(string(data),
		"enabled = false") {
		t.Errorf("config.toml = %q", data)
	}

	if _, err := os.Stat(l.treeDir(rec)); err != nil {
		t.Errorf("the tree went with the disable: %v", err)
	}

	if res, err := r.Disable("demo"); err != nil || !res.Already {
		t.Errorf("second Disable = %+v %v", res, err)
	}

	en, err := r.Enable("demo", Grant{})
	if err != nil || en.State != StateEnabled || en.Already || en.Consented || en.ConfigFile != "" {
		t.Fatalf("Enable = %+v %v", en, err)
	}

	if got, _ := l.lock().Get("demo"); got.Disabled {
		t.Error("the kill switch is still set")
	}

	if data, _ := os.ReadFile(l.paths.ConfigFile()); strings.Contains(string(data), "enabled") {
		t.Errorf("config.toml = %q", data)
	}

	if en, err := r.Enable("demo", Grant{}); err != nil || !en.Already {
		t.Errorf("second Enable = %+v %v", en, err)
	}

	if _, err := r.Disable("nope"); asError(t, err).Code != CodeNotInstalled {
		t.Errorf("Disable of a missing plugin = %v", err)
	}

	if _, err := r.Enable("nope", Grant{}); asError(t, err).Code != CodeNotInstalled {
		t.Errorf("Enable of a missing plugin = %v", err)
	}
}

// TestDisableConfigLayouts is P9: the kill switch is the lock flag, so a config.toml that the
// editor will not touch (dotted keys, an inline table) or that does not parse never makes a
// disable fail or leaves the plugin running; when the file says enabled = false in such a layout,
// enable names the file and the plugin stays off.
//
// Parameters:
//   - t: test to report to
func TestDisableConfigLayouts(t *testing.T) {
	layouts := map[string]string{
		"dotted keys":   "plugins.demo.enabled = true\n",
		"inline table":  "[plugins]\ndemo = { enabled = true }\n",
		"inline nested": "plugins = { demo = { enabled = true } }\n",
		"another table": "[plugins.other]\nenabled = true\n[plugins.demo]\nenabled = true\n",
		"comments":      "# mine\n[plugins.demo] # the demo\nenabled = true # on\n",
		"no file":       "",
		"unparsable":    "[plugins.demo\nenabled = false\n",
	}

	for name, content := range layouts {
		t.Run(name, func(t *testing.T) {
			l := newLab(t)
			l.fakeBin("tool")
			l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))

			if content != "" {
				l.writeConfig(content)
			}

			r := l.reg(nil)

			res, err := r.Disable("demo")
			if err != nil || !res.LockFlag {
				t.Fatalf("Disable = %+v %v", res, err)
			}

			if p, _ := r.Recheck("demo"); p.State != StateDisabled {
				t.Errorf("state after Disable = %s", p.State)
			}

			if res.ConfigOff != (res.ConfigFile == "") {
				t.Errorf("ConfigOff %v with file %q", res.ConfigOff, res.ConfigFile)
			}

			if name == "unparsable" {
				if data, _ := os.ReadFile(l.paths.ConfigFile()); string(data) != content {
					t.Errorf("a file that does not parse was rewritten: %q", data)
				}
			}
		})
	}

	blocked := map[string]string{
		"dotted keys":  "plugins.demo.enabled = false\n",
		"inline table": "[plugins]\ndemo = { enabled = false }\n",
	}

	for name, content := range blocked {
		t.Run("enable "+name, func(t *testing.T) {
			l := newLab(t)
			l.fakeBin("tool")
			l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
			l.writeConfig(content)

			r := l.reg(nil)

			if p, _ := r.Recheck("demo"); p.State != StateDisabled {
				t.Fatalf("state = %s", p.State)
			}

			res, err := r.Enable("demo", Grant{})
			if err != nil {
				t.Fatal(err)
			}

			// Either the editor handled the layout, or the file is named and the plugin stays off.
			if (res.ConfigFile != "") != (res.State == StateDisabled) {
				t.Errorf("Enable = %+v", res)
			}

			if res.ConfigFile != "" {
				if data, _ := os.ReadFile(l.paths.ConfigFile()); string(data) != content {
					t.Errorf("the file changed: %q", data)
				}
			}

			if got, _ := l.lock().Get("demo"); got.Disabled {
				t.Error("the lock flag stayed set")
			}
		})
	}
}

// TestDisableNeverReadsTheManifest checks that the kill switch works when the rest is broken: a
// tree that was tampered with, a config.toml that does not parse, a missing tree.
//
// Parameters:
//   - t: test to report to
func TestDisableNeverReadsTheManifest(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")
	rec := l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
	tree := l.treeDir(rec)

	l.open(tree)
	l.open(tree + "/plugin.toml")
	must(t, os.WriteFile(tree+"/plugin.toml", []byte("garbage"), 0o600))
	l.writeConfig("[[[\n")

	r := l.reg(nil)

	res, err := r.Disable("demo")
	if err != nil || !res.LockFlag {
		t.Fatalf("Disable = %+v %v", res, err)
	}

	if got, _ := l.lock().Get("demo"); !got.Disabled {
		t.Error("the kill switch is not set")
	}

	if p, _ := r.Recheck("demo"); p.State == StateEnabled {
		t.Errorf("state = %s", p.State)
	}
}

// TestKillSwitchAcrossRegistries is the P8 kill switch: a Disable through one registry stops the
// process that another one started, at once, and neither registry runs the plugin afterwards.
//
// Parameters:
//   - t: test to report to
func TestKillSwitchAcrossRegistries(t *testing.T) {
	l, a := runSetup(t)
	b := l.reg(nil)
	pidFile := l.root + "/long.pid"

	l.script("tool", "sleep 60 &\necho $! > "+pidFile+"\nwait\n")

	done := make(chan Outcome, 1)
	p := prepare(t, a, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})

	go func() { done <- a.Run(context.Background(), p, nil) }()

	waitFile(t, pidFile)
	waitProc(t, a, "demo")

	start := time.Now()

	res, err := b.Disable("demo")
	if err != nil || !res.LockFlag || res.Killed != 1 {
		t.Fatalf("Disable = %+v %v", res, err)
	}

	select {
	case out := <-done:
		if out.Status != OutcomeSignaled && out.Status != OutcomeFailed {
			t.Errorf("outcome %+v", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end")
	}

	if time.Since(start) > 8*time.Second {
		t.Error("the kill took too long")
	}

	waitGone(t, pidFile)

	for _, r := range []*Registry{a, b} {
		if e := refusal(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"}); e.Code !=
			CodeNotRunnable {
			t.Errorf("a registry still prepares: %v", e)
		}
	}

	if out := a.Run(context.Background(), p, nil); out.Status != OutcomeRefused {
		t.Errorf("the prepared run still goes: %+v", out)
	}
}

// TestEnableConsent walks the consent of an enable: a plugin with no consent is not enabled
// without a grant; a token that is not the one of the view is a mismatch, with its own exit code;
// the token or the prompt enable it and record who consented.
//
// Parameters:
//   - t: test to report to
func TestEnableConsent(t *testing.T) {
	setup := func(t *testing.T) (*lab, *Registry, string) {
		l := newLab(t)
		l.fakeBin("tool")
		l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
		unconsent(t, l, "demo")

		r := l.reg(nil)

		d, err := r.Allow("demo")
		if err != nil || !d.NeedsConsent || !contains(d.Why, WhyUnconsent) || d.Grant == "" {
			t.Fatalf("Allow = %+v %v", d, err)
		}

		if p, _ := r.Recheck("demo"); p.State != StateNeedsConsent {
			t.Fatalf("state = %s", p.State)
		}

		return l, r, d.Grant
	}

	t.Run("no grant", func(t *testing.T) {
		l, r, token := setup(t)

		_, err := r.Enable("demo", Grant{})
		if pe := asError(t, err); pe.Code != CodeConsentNeeded || ExitOf(err) != ExitUsage ||
			len(pe.Args) == 0 || pe.Args[0] != token {
			t.Errorf("Enable = %v", err)
		}

		if rec, _ := l.lock().Get("demo"); rec.ConsentedAt != "" {
			t.Error("a consent was recorded")
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		l, r, _ := setup(t)

		for _, g := range []Grant{{Token: "sha256:00"}, {Token: "x", Prompt: true}} {
			_, err := r.Enable("demo", g)
			if asError(t, err).Code != CodeGrantMismatch || ExitOf(err) != ExitPolicy {
				t.Errorf("Enable(%+v) = %v", g, err)
			}
		}

		if rec, _ := l.lock().Get("demo"); rec.ConsentedAt != "" {
			t.Error("a consent was recorded")
		}
	})

	t.Run("token", func(t *testing.T) {
		l, r, token := setup(t)

		res, err := r.Enable("demo", Grant{Token: token})
		if err != nil || !res.Consented || res.State != StateEnabled {
			t.Fatalf("Enable = %+v %v", res, err)
		}

		if rec, _ := l.lock().Get("demo"); rec.ConsentedBy != ConsentGrant || rec.ConsentedAt == "" {
			t.Errorf("record = %+v", rec)
		}
	})

	t.Run("prompt", func(t *testing.T) {
		l, r, _ := setup(t)

		res, err := r.Enable("demo", Grant{Prompt: true})
		if err != nil || !res.Consented || res.State != StateEnabled {
			t.Fatalf("Enable = %+v %v", res, err)
		}

		if rec, _ := l.lock().Get("demo"); rec.ConsentedBy != ConsentPrompt {
			t.Errorf("record = %+v", rec)
		}
	})

	t.Run("token of a consented plugin is not read", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")
		l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
		r := l.reg(nil)

		must(t, l.lock().SetDisabled("demo", true))

		if res, err := r.Enable("demo", Grant{Token: "stale"}); err != nil ||
			res.State != StateEnabled || res.Consented {
			t.Errorf("Enable = %+v %v", res, err)
		}
	})
}

// TestGrant checks the grant verb: it records the consent and nothing else, a plugin that needs
// none says so, and a token or prompt is checked as for Enable.
//
// Parameters:
//   - t: test to report to
func TestGrant(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")
	l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))

	r := l.reg(nil)

	if res, err := r.Grant("demo", Grant{}); err != nil || !res.Already {
		t.Errorf("Grant of a consented plugin = %+v %v", res, err)
	}

	must(t, l.lock().SetDisabled("demo", true))
	unconsent(t, l, "demo")

	d, err := r.Allow("demo")
	must(t, err)

	if _, err := r.Grant("demo", Grant{}); asError(t, err).Code != CodeConsentNeeded {
		t.Errorf("Grant without a consent = %v", err)
	}

	if _, err := r.Grant("demo", Grant{Token: "no"}); asError(t, err).Code != CodeGrantMismatch {
		t.Errorf("Grant with a wrong token = %v", err)
	}

	res, err := r.Grant("demo", Grant{Token: d.Grant})
	if err != nil || res.Already || res.State != StateDisabled {
		t.Fatalf("Grant = %+v %v", res, err)
	}

	rec, _ := l.lock().Get("demo")
	if rec.ConsentedBy != ConsentGrant || !rec.Disabled {
		t.Errorf("record = %+v: the grant must keep the other switches", rec)
	}

	if _, err := r.Grant("nope", Grant{}); asError(t, err).Code != CodeNotInstalled {
		t.Errorf("Grant of a missing plugin = %v", err)
	}

	if _, err := r.Grant("../x", Grant{}); asError(t, err).Code != CodeBadID {
		t.Errorf("Grant of a bad id = %v", err)
	}
}

// TestGrantCapsEscalation checks that capabilities that the consent on record does not cover ask
// again, with the reason, and that the grant records the new ones.
//
// Parameters:
//   - t: test to report to
func TestGrantCapsEscalation(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")
	l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))

	must(t, l.lock().Update("demo", func(r *LockRecord) { r.Caps = nil }))

	r := l.reg(nil)

	d, err := r.Allow("demo")
	if err != nil || !d.NeedsConsent || !contains(d.Why, WhyCaps) || len(d.Added) == 0 {
		t.Fatalf("Allow = %+v %v", d, err)
	}

	if _, err := r.Enable("demo", Grant{}); asError(t, err).Code != CodeConsentNeeded {
		t.Errorf("Enable = %v", err)
	}

	if _, err := r.Grant("demo", Grant{Prompt: true}); err != nil {
		t.Fatal(err)
	}

	rec, _ := l.lock().Get("demo")
	if len(rec.Caps) == 0 || rec.ConsentedBy != ConsentPrompt {
		t.Errorf("record = %+v", rec)
	}

	if d, _ := r.Allow("demo"); d.NeedsConsent {
		t.Errorf("the plugin still asks: %+v", d.Why)
	}
}

// TestEnableRefusals checks that a plugin that cannot run is refused with its reasons and that
// nothing is changed, and that a missing program only shows in the state.
//
// Parameters:
//   - t: test to report to
func TestEnableRefusals(t *testing.T) {
	t.Run("broken tree", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")
		rec := l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
		must(t, l.lock().SetDisabled("demo", true))

		tree := l.treeDir(rec)
		l.open(tree)
		l.open(tree + "/plugin.toml")
		must(t, os.WriteFile(tree+"/plugin.toml", []byte("other"), 0o600))

		r := l.reg(nil)

		_, err := r.Enable("demo", Grant{Prompt: true})
		if pe := asError(t, err); pe.Code != CodeEnableRefused || ExitOf(err) != ExitIntegrity ||
			len(pe.Args) == 0 {
			t.Errorf("Enable = %v", err)
		}

		var re *ReasonsError
		if !errors.As(err, &re) || re.ID != "demo" || len(re.Reasons) != len(asError(t, err).Args) {
			t.Errorf("the reasons are not under the error: %#v", re)
		}

		if got, _ := l.lock().Get("demo"); !got.Disabled {
			t.Error("a refused enable changed the record")
		}

		if _, err := r.Allow("demo"); asError(t, err).Code != CodeEnableRefused {
			t.Errorf("Allow = %v", err)
		}

		if _, err := r.Grant("demo", Grant{Prompt: true}); asError(t, err).Code !=
			CodeEnableRefused {
			t.Errorf("Grant = %v", err)
		}
	})

	t.Run("incompatible", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")
		l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
		must(t, l.lock().SetDisabled("demo", true))

		r := l.reg(func(o *Options) {
			o.Host = NewHost("0.9.0")
			o.Host.OS, o.Host.Arch = "linux", "amd64"
		})

		_, err := r.Enable("demo", Grant{Prompt: true})
		if pe := asError(t, err); pe.Code != CodeEnableRefused || !contains(pe.Args,
			CodeToteRange) {
			t.Errorf("Enable = %v", err)
		}
	})

	t.Run("session switch", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")
		l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))

		r := l.reg(func(o *Options) { o.NoPlugins = true })

		_, err := r.Enable("demo", Grant{Prompt: true})
		if pe := asError(t, err); pe.Code != CodeEnableRefused || !contains(pe.Args,
			ReasonSessionOff) {
			t.Errorf("Enable = %v", err)
		}
	})

	t.Run("missing program", func(t *testing.T) {
		l := newLab(t)
		l.install("demo", demoFiles("demo", "\n[[requires]]\nbin = \"absent\"\n"))
		must(t, l.lock().SetDisabled("demo", true))

		r := l.reg(nil)

		res, err := r.Enable("demo", Grant{})
		if err != nil || res.State != StateMissingDependency ||
			!hasReason(res.Reasons, ReasonMissingDep) {
			t.Errorf("Enable = %+v %v", res, err)
		}

		if got, _ := l.lock().Get("demo"); got.Disabled {
			t.Error("the kill switch stayed set")
		}
	})

	t.Run("no record", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")
		rec := l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
		must(t, l.lock().Delete("demo"))

		r := l.reg(nil)

		_, err := r.Enable("demo", Grant{Prompt: true})
		if asError(t, err).Code != CodeNotInstalled && asError(t, err).Code != CodeEnableRefused {
			t.Errorf("Enable = %v", err)
		}

		if _, serr := os.Stat(l.treeDir(rec)); serr != nil {
			t.Errorf("the tree was touched: %v", serr)
		}
	})
}

// TestEnableClearsQuarantine checks that enable is the way out of a quarantine: it clears the flag
// and the counters in one write and keeps the consent.
//
// Parameters:
//   - t: test to report to
func TestEnableClearsQuarantine(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")
	rec := l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))

	must(t, l.lock().Update("demo", func(r *LockRecord) {
		r.Quarantined, r.Failures = true, 3
		r.LastFailureAt, r.LastReason = rec.InstalledAt, FailCrash
	}))

	r := l.reg(nil)

	if p, _ := r.Recheck("demo"); p.State != StateQuarantined {
		t.Fatalf("state = %s", p.State)
	}

	res, err := r.Enable("demo", Grant{})
	if err != nil || res.State != StateEnabled || res.Already || res.Consented {
		t.Fatalf("Enable = %+v %v", res, err)
	}

	got, _ := l.lock().Get("demo")
	if got.Quarantined || got.Failures != 0 || got.LastFailureAt != "" || got.LastReason != "" ||
		got.ConsentedAt != rec.ConsentedAt {
		t.Errorf("record = %+v", got)
	}
}

// TestVerbsRefuseFromAPlugin checks that every verb that changes something refuses to run from a
// plugin's own session, whether the environment says so or not, and that nothing changes.
//
// Parameters:
//   - t: test to report to
func TestVerbsRefuseFromAPlugin(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")
	l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
	src := demoSource(l, "other", "1.0.0")

	inside := false

	r := l.reg(func(o *Options) {
		base := o.Lookup
		o.Lookup = func(k string) (string, bool) {
			if inside && k == "TOTE_PLUGIN_ID" {
				return "demo", true
			}

			return base(k)
		}
	})

	st := plan(t, r, InstallRequest{Source: src})

	before, _ := os.ReadFile(l.paths.LockFile())
	inside = true

	checks := map[string]func() error{
		"Plan": func() error {
			_, err := r.Plan(context.Background(), InstallRequest{Source: src})

			return err
		},
		"Disable": func() error { _, err := r.Disable("demo"); return err },
		"Enable":  func() error { _, err := r.Enable("demo", Grant{Prompt: true}); return err },
		"Grant":   func() error { _, err := r.Grant("demo", Grant{Prompt: true}); return err },
		"Remove":  func() error { _, err := r.Remove("demo", RemoveOptions{Purge: true}); return err },
	}

	for name, call := range checks {
		err := call()
		if pe := asError(t, err); pe.Code != CodeFromPlugin || ExitOf(err) != ExitPolicy {
			t.Errorf("%s = %v", name, err)
		}
	}

	after, _ := os.ReadFile(l.paths.LockFile())
	if string(after) != string(before) {
		t.Error("the lock changed")
	}

	if _, err := os.Stat(l.paths.PluginsDir() + "/demo"); err != nil {
		t.Errorf("the plugin went: %v", err)
	}

	inside = false

	must(t, st.Abort())
}
