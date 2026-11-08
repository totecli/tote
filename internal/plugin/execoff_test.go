package plugin

import (
	"slices"
	"strings"
	"testing"
)

// execOnHost is a host that announces the features of the exec tier too, as a tote with the exec
// tier does.
func execOnHost() Host {
	h := testHost()
	h.Features = announcedFeatures(true)

	return h
}

// TestExecFeaturesSwitch checks the lists of announced and reserved features on both positions of
// the switch: the exec features are announced only with the exec tier, reserved otherwise, and
// never both.
//
// Parameters:
//   - t: test to report to
func TestExecFeaturesSwitch(t *testing.T) {
	tests := []struct {
		name         string
		exec         bool
		wantCount    int
		wantAnnounce bool
	}{
		{"tote 1.0", false, 6, false},
		{"with the exec tier", true, 8, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ann, res := announcedFeatures(tt.exec), reservedFeatures(tt.exec)

			if len(ann) != tt.wantCount {
				t.Errorf("announced = %v", ann)
			}

			for _, f := range execFeatures {
				if slices.Contains(ann, f) != tt.wantAnnounce {
					t.Errorf("%s announced = %v, want %v", f, !tt.wantAnnounce, tt.wantAnnounce)
				}

				if slices.Contains(res, f) == tt.wantAnnounce {
					t.Errorf("%s reserved = %v, want %v", f, tt.wantAnnounce, !tt.wantAnnounce)
				}
			}

			for _, f := range res {
				if slices.Contains(ann, f) {
					t.Errorf("feature %q is both announced and reserved", f)
				}
			}
		})
	}

	if slices.Contains(AnnouncedFeatures, "exec.run") || slices.Contains(NewHost("1.0.0").Features,
		"exec.list") {
		t.Errorf("tote 1.0 announces %v", AnnouncedFeatures)
	}
}

// TestExecTierRefused checks the one place where tote 1.0 refuses the exec tier: Decode refuses
// every manifest that says tier = "exec" with reserved_not_available at the key tier, as an
// incompatibility, and without reading the rest. The same manifests read fine with the exec tier
// on. A declarative manifest with an [exec] table keeps tier_mismatch, and a manifest that
// requires an exec feature is also refused with feature_missing.
//
// Parameters:
//   - t: test to report to
func TestExecTierRefused(t *testing.T) {
	execList := execM("", []string{"exec:tool"}, "\n[[list]]\nid = \"l\"\nlabel = \"L\"\n"+
		"[[list.column]]\nid = \"a\"\nlabel = \"A\"\n")

	tests := []struct {
		name  string
		src   string
		codes []string
		valid bool
	}{
		{"exec tier", execM("", []string{"exec:tool"}, ""), []string{CodeReserved}, true},
		{"exec tier with a list", execList, []string{CodeReserved}, true},
		{"exec tier that needs exec.run", execM("requires_features = [\"exec.run\"]\n",
			[]string{"exec:tool"}, ""), []string{CodeFeatureMissing, CodeReserved}, true},
		{"exec tier with errors elsewhere", strings.Replace(execM("", nil, ""), "[exec]",
			"[exec]\nunknown_key = 1", 1), []string{CodeReserved}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withExecTier(t, false)

			m, rep := decode(tt.src)
			if m != nil || rep.Class() != ClassIncompatible || rep.Header.Tier != TierExec {
				t.Fatalf("manifest = %v, class = %v, header = %+v", m, rep.Class(), rep.Header)
			}

			var got []string
			for _, is := range rep.Errors() {
				got = append(got, is.Code)
			}

			slices.Sort(got)

			if !slices.Equal(got, tt.codes) {
				t.Errorf("codes = %v, want %v", got, tt.codes)
			}

			is := find(rep, CodeReserved, "tier")
			if is == nil || !is.Incompatible || len(is.Args) != 1 || is.Line == 0 ||
				is.Args[0] != `tier = "exec"` {
				t.Errorf("reserved issue = %+v", is)
			}

			withExecTier(t, true)

			m, rep = Decode([]byte(tt.src), DecodeOptions{Host: execOnHost()})
			if (m != nil) != tt.valid || (m != nil && m.Tier != TierExec) {
				t.Errorf("with the exec tier on: %v %v", m, rep.Issues)
			}
		})
	}

	t.Run("declarative with an exec table", func(t *testing.T) {
		withExecTier(t, false)

		_, rep := decode(decl("", nil, "\n[exec]\nbin = \"bin/demo\"\n"))
		if find(rep, CodeTierMismatch, "exec") == nil || rep.Has(CodeReserved) {
			t.Errorf("issues = %v", rep.Issues)
		}
	})
}

// TestExecTierRefusedEverywhere checks the entry points of tote 1.0 with a package of tier exec:
// the install of a directory is refused with reserved_not_available and leaves nothing staged,
// `plugin check` reports it, and a plugin of that tier that is in the store from before is listed
// as incompatible with the reason, cannot be enabled and never runs.
//
// Parameters:
//   - t: test to report to
func TestExecTierRefusedEverywhere(t *testing.T) {
	m := strings.Replace(execM("", []string{"exec:tool"}, ""), `id = "demo"`, `id = "ex"`, 1)
	files := map[string]string{"plugin.toml": m, "bin/demo*": "#!/bin/sh\n"}

	l := chkLab(t)
	l.fakeBin("tool")

	// A plugin of the exec tier that an earlier tote installed.
	rec := l.install("ex", files)
	if rec.Tier != TierExec {
		t.Fatalf("record = %+v", rec)
	}

	r := l.reg(nil)
	if p, _ := r.Recheck("ex"); p.State != StateEnabled {
		t.Fatalf("with the exec tier on: %s %v", p.State, p.Reasons)
	}

	withExecTier(t, false)

	r = l.reg(nil)

	t.Run("installed before", func(t *testing.T) {
		p, ok := r.Recheck("ex")
		if !ok || p.State != StateIncompatible || p.Manifest != nil || p.State.Runnable() {
			t.Fatalf("plugin = %s %v", p.State, p.Reasons)
		}

		if len(p.Reasons) != 1 || p.Reasons[0].Code != CodeReserved ||
			p.Reasons[0].Args[0] != `tier = "exec"` {
			t.Errorf("reasons = %v", p.Reasons)
		}

		listed, ok := r.Load().Get("ex")
		if !ok || listed.State != StateIncompatible || len(r.Load().Enabled()) != 0 {
			t.Errorf("listed = %s", listed.State)
		}

		_, err := r.Enable("ex", Grant{Prompt: true})
		if pe := asError(t, err); pe.Code != CodeEnableRefused || ExitOf(pe) != ExitPolicy {
			t.Errorf("enable = %v", err)
		}

		if p, _ := r.Recheck("ex"); p.State != StateIncompatible {
			t.Errorf("state after enable = %s", p.State)
		}
	})

	t.Run("install", func(t *testing.T) {
		dir := l.srcDir("exsrc", files)

		pe := planErr(t, r, InstallRequest{Source: dir})
		if pe.Code != CodeReserved || ExitOf(pe) != ExitFailure {
			t.Errorf("install = %v", pe)
		}

		pe = planErr(t, r, InstallRequest{Source: dir, ID: "ex", Update: true, Force: true})
		if pe.Code != CodeReserved {
			t.Errorf("update = %v", pe)
		}

		if names := stagingNames(t, l); len(names) != 0 {
			t.Errorf("staging = %v", names)
		}
	})

	t.Run("check", func(t *testing.T) {
		res := chkDir(t, l, files, nil)
		if !res.Failed() || res.find(CodeReserved) == nil {
			t.Errorf("findings = %+v", res.Findings)
		}
	})

	t.Run("no run", func(t *testing.T) {
		_, err := r.Prepare(Call{Plugin: "ex", Table: "command", Entry: "x"})
		if pe := asError(t, err); pe.Code != CodeNotRunnable {
			t.Errorf("prepare = %v", err)
		}
	})
}
