package plugin

import (
	"strings"
	"testing"
)

// viewKeys lists the message keys of a view, in order.
//
// Parameters:
//   - v: the view
func viewKeys(v ConsentView) []string {
	var out []string

	for _, l := range v.Lines {
		if l.Key != "" {
			out = append(out, l.Key)
		} else if l.Label != "" {
			out = append(out, l.Label)
		}
	}

	return out
}

// viewLine returns the first line of a view that has a message key, or false.
//
// Parameters:
//   - v: the view
//   - key: catalog key to look for
func viewLine(v ConsentView, key string) (ViewLine, bool) {
	for _, l := range v.Lines {
		if l.Key == key {
			return l, true
		}
	}

	return ViewLine{}, false
}

// TestConsentViewInstall checks the order of section 7.6 (the commands before the access) for the
// install of a declarative plugin and that the closing warning ends the view.
//
// Parameters:
//   - t: test to report to
func TestConsentViewInstall(t *testing.T) {
	d := Diff{
		ID: "demo", Version: "1.0.0", Tier: TierDeclarative, Kind: KindLocal,
		Class: OriginLocal, Source: "/src/demo", TreeSHA256: strings.Repeat("ab", 32),
		Description: "A neutral demo plugin.", Caps: []string{"exec:echo"},
		Commands: []CommandRef{{Table: "command", ID: "hello", Run: []string{"echo", "hello"}}},
	}

	v := NewConsentView(d, ViewInstall, false)
	keys := viewKeys(v)

	want := []string{
		"consent_title_install", "consent_source", "consent_origin_local",
		"consent_checksum_local",
		"consent_code_declarative", "consent_description_head", "consent_cmds_head",
		"consent_cmd_line", "consent_can_head", "consent_cap_exec", "consent_legend_enforced",
		"consent_warn_declarative",
	}

	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("keys:\n got %v\nwant %v", keys, want)
	}

	if last := v.Lines[len(v.Lines)-1]; last.Level != LevelWarn {
		t.Errorf("last line = %+v", last)
	}

	for i, l := range v.Lines {
		if l.Level == LevelGap && i > 0 && v.Lines[i-1].Level == LevelGap {
			t.Errorf("two gaps in a row at %d", i)
		}
	}
}

// TestConsentViewModes checks the title of each mode and the escalation title of an update that
// adds access.
//
// Parameters:
//   - t: test to report to
func TestConsentViewModes(t *testing.T) {
	base := Diff{
		ID: "demo", Version: "2.0.0", OldVersion: "1.0.0", Tier: TierDeclarative,
		Update: true, Unchanged: 1,
	}

	tests := []struct {
		name string
		d    Diff
		mode string
		want string
	}{
		{"install", Diff{ID: "demo", Version: "1.0.0"}, ViewInstall, "consent_title_install"},
		{"allow", Diff{ID: "demo", Version: "1.0.0"}, ViewAllow, "consent_title_allow"},
		{"quiet update", base, ViewUpdate, "consent_title_update"},
		{
			"escalation",
			func() Diff { d := base; d.Added = []string{"net:example.org"}; return d }(),
			ViewUpdate, "consent_title_escalation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := NewConsentView(tc.d, tc.mode, false)
			if got := viewKeys(v)[0]; got != tc.want {
				t.Errorf("title = %s, want %s", got, tc.want)
			}
		})
	}

	d := base
	d.Added = []string{"net:example.org"}
	d.Removed = []string{"exec:echo"}

	v := NewConsentView(d, ViewUpdate, false)
	for _, key := range []string{
		"consent_diff_added", "consent_diff_kept", "consent_diff_removed",
		"consent_diff_cmds",
	} {
		if _, ok := viewLine(v, key); !ok {
			t.Errorf("the update view lacks %s", key)
		}
	}

	if _, ok := viewLine(v, "consent_can_head"); ok {
		t.Error("an update shows the full list of access")
	}
}

// TestConsentViewChecksum checks which line tells where the checksum comes from.
//
// Parameters:
//   - t: test to report to
func TestConsentViewChecksum(t *testing.T) {
	sum := strings.Repeat("cd", 32)

	tests := []struct {
		name   string
		d      Diff
		pinned bool
		want   string
	}{
		{"local", Diff{Kind: KindLocal, TreeSHA256: sum}, false, "consent_checksum_local"},
		{"bundled", Diff{Kind: KindBundled, TreeSHA256: sum}, false, "consent_checksum_embedded"},
		{"release", Diff{Kind: KindRelease, ArchiveSHA256: sum}, true, "consent_checksum_pinned"},
		{
			"pinned archive",
			Diff{Kind: KindLocal, ArchiveSHA256: sum},
			true,
			"consent_checksum_pinned",
		},
		{
			"unpinned archive",
			Diff{Kind: KindLocal, ArchiveSHA256: sum},
			false,
			"consent_checksum_local",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := viewLine(NewConsentView(tc.d, ViewInstall, tc.pinned), tc.want); !ok {
				t.Errorf("no %s in %v", tc.want, viewKeys(NewConsentView(tc.d, ViewInstall,
					tc.pinned)))
			}
		})
	}
}

// TestConsentViewCaps checks the line of each kind of capability and the level of the dangerous
// ones.
//
// Parameters:
//   - t: test to report to
func TestConsentViewCaps(t *testing.T) {
	tests := []struct {
		cap   string
		key   string
		level string
		tag   string
	}{
		{"exec:echo", "consent_cap_exec", LevelItem, TagEnforced},
		{"exec:sh", "consent_cap_exec_broad", LevelStrong, TagEnforced},
		{"net:example.org", "consent_cap_net", LevelItem, TagDeclared},
		{"env:MY_MODE", "consent_cap_env", LevelItem, TagEnforced},
		{"env:API_TOKEN", "consent_cap_env_sensitive", LevelStrong, TagEnforced},
		{"fs:read:cwd", "consent_cap_fs_read", LevelItem, TagDeclared},
		{"fs:write:tmp", "consent_cap_fs_write", LevelItem, TagDeclared},
		{"fs:read:home", "consent_cap_fs_read", LevelStrong, TagDeclared},
		{CapDerivedTTY, "consent_cap_tty", LevelStrong, TagEnforced},
	}

	for _, tc := range tests {
		t.Run(tc.cap, func(t *testing.T) {
			l := capLine(tc.cap, TierDeclarative)
			if l.Key != tc.key || l.Level != tc.level || l.Tag != tc.tag {
				t.Errorf("got %+v, want key %s level %s tag %s", l, tc.key, tc.level, tc.tag)
			}
		})
	}

	if l := capLine("exec:echo", TierExec); l.Tag != TagDeclared {
		t.Errorf("exec: of an exec plugin is not enforced: %+v", l)
	}
}

// TestConsentViewCleansPluginText checks that what the plugin wrote is cleaned and cut, and that
// the description is cut to three lines.
//
// Parameters:
//   - t: test to report to
func TestConsentViewCleansPluginText(t *testing.T) {
	d := Diff{
		ID: "demo", Version: "1.0.0", Tier: TierExec, Program: "run\x1b[31m-me",
		Source:      strings.Repeat("s", 1000),
		Description: "one\ntwo\nthree\nfour\nfive\x1b]0;x\x07",
	}

	v := NewConsentView(d, ViewInstall, false)

	quotes := 0

	for _, l := range v.Lines {
		if strings.ContainsAny(l.Text, "\x1b\x07") {
			t.Errorf("control character left in %+v", l)
		}

		if len([]rune(l.Text)) > 400 {
			t.Errorf("text not cut: %d runes", len([]rune(l.Text)))
		}

		for _, a := range l.Args {
			if strings.ContainsAny(a.Text, "\x1b\x07") {
				t.Errorf("control character left in an argument of %+v", l)
			}
		}

		if l.Level == LevelQuote {
			quotes++
		}
	}

	if quotes != viewDescLines {
		t.Errorf("%d description lines, want %d", quotes, viewDescLines)
	}

	if _, ok := viewLine(v, "consent_code_exec"); !ok {
		t.Error("an exec plugin has no line about its own program")
	}

	if _, ok := viewLine(v, "consent_warn_exec"); !ok {
		t.Error("an exec plugin has no exec warning")
	}
}

// TestConsentViewMissing checks the lines about a program that is not on this machine.
//
// Parameters:
//   - t: test to report to
func TestConsentViewMissing(t *testing.T) {
	d := Diff{
		ID: "demo", Version: "1.0.0",
		Missing: []Requires{{Bin: "tool", Hint: "install tool"}},
	}

	v := NewConsentView(d, ViewInstall, false)
	if l, ok := viewLine(v, "consent_missing_dep"); !ok || l.Args[0].Text != "tool" {
		t.Errorf("missing line = %+v, %v", l, ok)
	}

	if l, ok := viewLine(v, "consent_hint"); !ok || l.Args[0].Text != "install tool" {
		t.Errorf("hint line = %+v, %v", l, ok)
	}
}

// TestExitFor checks the mapping of a download answered with 401 to the authentication exit code
// and that every other status keeps its class.
//
// Parameters:
//   - t: test to report to
func TestExitFor(t *testing.T) {
	tests := []struct {
		code string
		args []string
		want int
	}{
		{CodeNetStatus, []string{"https://x", "401"}, ExitAuth},
		{CodeNetStatus, []string{"https://x", "404"}, ExitClass(CodeNetStatus)},
		{CodeNetStatus, []string{"https://x"}, ExitClass(CodeNetStatus)},
		{CodeNetStatus, nil, ExitClass(CodeNetStatus)},
		{CodeHashMismatch, nil, ExitIntegrity},
	}

	for _, tc := range tests {
		if got := ExitFor(tc.code, tc.args); got != tc.want {
			t.Errorf("ExitFor(%s, %v) = %d, want %d", tc.code, tc.args, got, tc.want)
		}
	}

	if ExitClass(CodeNetStatus) == ExitAuth {
		t.Error("ExitClass alone must not map a status to authentication")
	}
}

// TestProgramOf checks which program a manifest shows in the consent view.
//
// Parameters:
//   - t: test to report to
func TestProgramOf(t *testing.T) {
	tests := []struct {
		name string
		m    *Manifest
		want string
	}{
		{"declarative", &Manifest{Tier: TierDeclarative}, ""},
		{"exec without table", &Manifest{Tier: TierExec}, ""},
		{"exec bin", &Manifest{Tier: TierExec, Exec: &Exec{Bin: "prog"}}, "prog"},
		{"platform build", &Manifest{
			Tier: TierExec, Exec: &Exec{},
			Builds: []Build{{Bin: "first"}, {Bin: "second"}},
		}, "first"},
		{"exec without any bin", &Manifest{Tier: TierExec, Exec: &Exec{}}, ""},
	}

	for _, tc := range tests {
		if got := programOf(tc.m); got != tc.want {
			t.Errorf("%s: programOf = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestOriginClassExported checks the exported wrapper against the rules of the origin classes.
//
// Parameters:
//   - t: test to report to
func TestOriginClassExported(t *testing.T) {
	tests := []struct {
		kind, origin, id, want string
	}{
		{KindBundled, "", "gft", OriginBundled},
		{KindLocal, "", "demo", OriginLocal},
		{KindRelease, "acme/demo", "demo", OriginCommunity},
		{KindRelease, "totecli/gft", "gft", OriginOfficial},
		{KindRelease, "totecli/ghrel", "gft", OriginCommunity},
		{KindRelease, "totecli/demo", "demo", OriginCommunity},
		{KindRelease, "totecli/plugin-gft", "gft", OriginCommunity},
	}

	for _, tc := range tests {
		if got := OriginClass(tc.kind, tc.origin, tc.id); got != tc.want {
			t.Errorf("OriginClass(%s, %s, %s) = %s, want %s", tc.kind, tc.origin, tc.id, got,
				tc.want)
		}
	}
}
