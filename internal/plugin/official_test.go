package plugin

import (
	"strings"
	"testing"
)

// TestIsOfficialOrigin checks the rule of the official origin: the origin is exactly
// totecli/<id>, and the id is one of the official ids.
//
// Parameters:
//   - t: test to report to
func TestIsOfficialOrigin(t *testing.T) {
	tests := []struct {
		name, origin, id string
		want             bool
	}{
		{"gft from totecli/gft", "totecli/gft", "gft", true},
		{"ghrel from totecli/ghrel", "totecli/ghrel", "ghrel", true},
		{"ghsec from totecli/ghsec", "totecli/ghsec", "ghsec", true},
		{"konsolai from totecli/konsolai", "totecli/konsolai", "konsolai", true},
		{"ssh-tools from totecli/ssh-tools", "totecli/ssh-tools", "ssh-tools", true},
		{"another official repository", "totecli/ghrel", "gft", false},
		{"the registry claiming gft", "totecli/registry", "gft", false},
		{"the registry for its own name", "totecli/registry", "registry", false},
		{"tote itself", "totecli/tote", "tote", false},
		{"not an official id", "totecli/demo", "demo", false},
		{"the former name", "totecli/plugin-gft", "gft", false},
		{"the former name with the former id", "totecli/plugin-gft", "plugin-gft", false},
		{"another owner", "someone/gft", "gft", false},
		{"the command repository", "20syldev/gft", "gft", false},
		{"upper case origin", "TOTECLI/GFT", "gft", false},
		{"upper case owner", "TOTECLI/gft", "gft", false},
		{"upper case id", "totecli/gft", "GFT", false},
		{"trailing slash", "totecli/gft/", "gft", false},
		{"trailing space", "totecli/gft ", "gft", false},
		{"leading space", " totecli/gft", "gft", false},
		{"git suffix", "totecli/gft.git", "gft", false},
		{"url form", "https://github.com/totecli/gft", "gft", false},
		{"host form", "github.com/totecli/gft", "gft", false},
		{"no owner", "gft", "gft", false},
		{"owner alone", "totecli/", "", false},
		{"empty origin and id", "", "", false},
		{"empty origin", "", "gft", false},
		{"empty id", "totecli/gft", "", false},
		{"id longer than the repository", "totecli/gft", "gft-extra", false},
		{"prefix of an official repository", "totecli/gft-extra", "gft", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOfficialOrigin(tc.origin, tc.id); got != tc.want {
				t.Errorf("isOfficialOrigin(%q, %q) = %v, want %v", tc.origin, tc.id, got, tc.want)
			}
		})
	}
}

// TestOfficialOriginOfParsedSources checks what the normalisation of a release gives to the rule:
// ReleaseRef.Origin lower-cases the owner and the repository, so the spelling TOTECLI/GFT of a
// release URL is the repository totecli/gft (GitHub names are case-insensitive), while every
// other variant is either refused as a source or is another repository.
//
// Parameters:
//   - t: test to report to
func TestOfficialOriginOfParsedSources(t *testing.T) {
	const tail = "/releases/download/1.0.0/plugin-gft.tar.gz"

	tests := []struct {
		name, text string
		bad        bool
		origin     string
		official   bool
	}{
		{"canonical url", "https://github.com/totecli/gft" + tail, false, "totecli/gft", true},
		{"upper case url", "https://github.com/TOTECLI/GFT" + tail, false, "totecli/gft", true},
		{
			"git suffix url", "https://github.com/totecli/gft.git" + tail, false,
			"totecli/gft.git", false,
		},
		{"trailing slash url", "https://github.com/totecli/gft/" + tail, true, "", false},
		{"repository page", "https://github.com/totecli/gft", true, "", false},
		{
			"former name url", "https://github.com/totecli/plugin-gft" + tail, false,
			"totecli/plugin-gft", false,
		},
		{
			"other repository", "https://github.com/totecli/ghrel" + tail, false,
			"totecli/ghrel", false,
		},
		{
			"registry", "https://github.com/totecli/registry" + tail, false,
			"totecli/registry", false,
		},
		{"shorthand", "totecli/gft@1.0.0", false, "totecli/gft", true},
		{"upper case shorthand", "TOTECLI/GFT@1.0.0", false, "totecli/gft", true},
		{"shorthand git suffix", "totecli/gft.git@1.0.0", false, "totecli/gft.git", false},
		{"shorthand trailing slash", "totecli/gft/@1.0.0", true, "", false},
		{"shorthand trailing space", "totecli/gft @1.0.0", true, "", false},
		{"shorthand trailing space after the tag", "totecli/gft@1.0.0 ", true, "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := ParseReleaseSource(tc.text)
			if tc.bad {
				if err == nil {
					t.Fatalf("%q was accepted as %+v", tc.text, ref)
				}

				return
			}

			if err != nil {
				t.Fatalf("%q: %v", tc.text, err)
			}

			if ref.Origin() != tc.origin {
				t.Errorf("origin = %q, want %q", ref.Origin(), tc.origin)
			}

			if got := isOfficialOrigin(ref.Origin(), "gft"); got != tc.official {
				t.Errorf("isOfficialOrigin(%q, gft) = %v, want %v", ref.Origin(), got, tc.official)
			}
		})
	}
}

// TestSameSourceBundledOfficial checks that a release replaces the bundled copy of an id only
// when its repository is totecli/<id> and the id is an official one.
//
// Parameters:
//   - t: test to report to
func TestSameSourceBundledOfficial(t *testing.T) {
	rec := LockRecord{ID: "gft", Kind: KindBundled}

	tests := []struct {
		origin, id string
		want       bool
	}{
		{"totecli/gft", "gft", true},
		{"totecli/ghrel", "gft", false},
		{"totecli/registry", "gft", false},
		{"totecli/plugin-gft", "gft", false},
		{"someone/gft", "gft", false},
		{"totecli/demo", "demo", false},
		{"", "gft", false},
	}

	for _, tc := range tests {
		got := sameSource(rec, KindRelease, tc.origin, "", tc.id)
		if got != tc.want {
			t.Errorf("sameSource(bundled, release %q, %q) = %v, want %v", tc.origin, tc.id, got,
				tc.want)
		}
	}

	// A bundled copy is not replaced by a directory or by an archive file, whatever the origin.
	if sameSource(rec, KindLocal, "totecli/gft", "/x", "gft") {
		t.Error("a local source replaces a bundled copy")
	}
}

// TestInstallReleaseOfficialRepository checks that the id of the manifest decides, never the
// repository name: an official id is accepted from totecli/<that id> and from nowhere else, and
// a repository of the organisation is not official for another id.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseOfficialRepository(t *testing.T) {
	gft := archiveOf(t, strings.Replace(manifest("demo", "1.0.0", []string{"exec:tool"},
		cmdWith("go", "")), `id = "demo"`, `id = "gft"`, 1))

	t.Run("refused", func(t *testing.T) {
		for _, repo := range []string{"ghrel", "registry", "tote", "plugin-gft", "gft.git", "demo"} {
			e := newRelEnv(t)
			url := e.publish("totecli", repo, "1.0.0", "plugin-gft.tar.gz", gft)

			pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(gft)})
			if pe.Code != CodeIDInvalid {
				t.Errorf("totecli/%s claiming gft: %s", repo, pe.Code)
			}

			pe = e.planCode(InstallRequest{Source: url, SHA256: sumOf(gft), ID: "gft"})
			if pe.Code != CodeIDInvalid {
				t.Errorf("totecli/%s claiming gft, with the id requested: %s", repo, pe.Code)
			}
		}
	})

	t.Run("accepted", func(t *testing.T) {
		e := newRelEnv(t)
		url := e.publish("totecli", "gft", "1.0.0", "plugin-gft.tar.gz", gft)

		for _, id := range []string{"", "gft"} {
			st := plan(t, e.r, InstallRequest{Source: url, SHA256: sumOf(gft), ID: id})
			if d := st.Diff(); d.Class != OriginOfficial || d.Origin != "totecli/gft" {
				t.Errorf("id %q: diff = %+v", id, d)
			}

			_ = st.Abort()
		}
	})

	t.Run("not an official id", func(t *testing.T) {
		e := newRelEnv(t)
		data := demoArchive(t, "1.0.0")
		url := e.publish("totecli", "demo", "1.0.0", "demo.tar.gz", data)

		st := plan(t, e.r, InstallRequest{Source: url, SHA256: sumOf(data)})
		if d := st.Diff(); d.Class != OriginCommunity || d.Origin != "totecli/demo" {
			t.Errorf("diff = %+v", d)
		}

		_ = st.Abort()
	})
}

// TestUpdateBundledOnlyByOfficialRelease checks that a bundled copy is updated by totecli/<id>
// of the same id and by no other repository of the organisation.
//
// Parameters:
//   - t: test to report to
func TestUpdateBundledOnlyByOfficialRelease(t *testing.T) {
	gft := func(version string) string {
		return strings.Replace(manifest("demo", version, []string{"exec:tool"}, cmdWith("go", "")),
			`id = "demo"`, `id = "gft"`, 1)
	}

	l := newLab(t)
	l.fakeBin("tool")

	h := newHub(t)
	b := bundle(t, l, map[string]string{"plugin.toml": gft("1.0.0")})
	e := &relEnv{t: t, l: l, h: h, r: l.reg(func(o *Options) {
		o.Bundled = b
		o.Transport = h.transport()
	})}

	st := plan(t, e.r, InstallRequest{Source: "gft"})
	if _, err := st.Commit(Grant{Prompt: true}); err != nil {
		t.Fatal(err)
	}

	data := archiveOf(t, gft("1.1.0"))

	for _, repo := range []string{"ghrel", "registry", "plugin-gft"} {
		url := e.publish("totecli", repo, "1.1.0", "plugin-gft.tar.gz", data)

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(data), ID: "gft", Update: true})
		if pe.Code != CodeIDInvalid {
			t.Errorf("totecli/%s: %s", repo, pe.Code)
		}
	}

	url := e.publish("totecli", "gft", "1.1.0", "plugin-gft.tar.gz", data)

	st = plan(t, e.r, InstallRequest{Source: url, SHA256: sumOf(data), ID: "gft", Update: true})
	if d := st.Diff(); d.Class != OriginOfficial || len(d.Warnings) != 0 {
		t.Errorf("diff = %+v", d)
	}

	_ = st.Abort()
}

// TestConsentClassFollowsTheID checks the label of the consent view of an installed release that
// uses an official id: official only while the recorded origin is totecli/<that id>. The record is
// rewritten here the way a tampered lock would be. Loading itself does not apply the reservation
// of ids to an installed plugin (DecodeOptions.Installed), so the class is what changes.
//
// Parameters:
//   - t: test to report to
func TestConsentClassFollowsTheID(t *testing.T) {
	data := archiveOf(t, strings.Replace(manifest("demo", "1.0.0", []string{"exec:tool"},
		cmdWith("go", "")), `id = "demo"`, `id = "gft"`, 1))

	e := newRelEnv(t)
	url := e.publish("totecli", "gft", "1.0.0", "plugin-gft.tar.gz", data)

	st := plan(t, e.r, InstallRequest{Source: url, SHA256: sumOf(data)})
	if _, err := st.Commit(Grant{Prompt: true}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		origin, want string
	}{
		{"totecli/gft", OriginOfficial},
		{"totecli/ghrel", OriginCommunity},
		{"totecli/registry", OriginCommunity},
		{"totecli/plugin-gft", OriginCommunity},
		{"someone/gft", OriginCommunity},
		{"totecli/gft", OriginOfficial},
	}

	for _, tc := range tests {
		must(t, e.l.lock().Update("gft", func(rec *LockRecord) { rec.Origin = tc.origin }))

		p, _ := e.r.Recheck("gft")
		if got := allowDiff(p).Class; got != tc.want {
			t.Errorf("origin %s: class = %s, want %s", tc.origin, got, tc.want)
		}
	}
}
