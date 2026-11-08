package plugin

import (
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestSourceChangedArguments checks that a refused change of source carries the id, the recorded
// source and the new one, in the order of the message, and that a forced one warns with the two
// sources only.
//
// Parameters:
//   - t: test to report to
func TestSourceChangedArguments(t *testing.T) {
	l := newLab(t)
	plain := surfaceBase("", "", "")
	r, dir := seeded(t, l, plain)

	other := l.srcDir("other", map[string]string{"plugin.toml": withVersion(plain, "1.1.0")})

	pe := planErr(t, r, InstallRequest{Source: other})
	if want := []string{"demo", dir, other}; !slices.Equal(pe.Args, want) {
		t.Errorf("args = %q, want %q", pe.Args, want)
	}

	st := plan(t, r, InstallRequest{Source: other, Force: true})
	defer func() { _ = st.Abort() }()

	for _, w := range st.Diff().Warnings {
		if w.Code == WarnSourceChanged && !slices.Equal(w.Args, []string{dir, other}) {
			t.Errorf("warning args = %q", w.Args)
		}
	}
}

// TestRejectionNamesTheFirstIssue checks that a package refused for a command that no exec:
// permission covers is reported with that code, which is a refusal by policy (4), and that a
// finer cause keeps the umbrella code.
//
// Parameters:
//   - t: test to report to
func TestRejectionNamesTheFirstIssue(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code string
		exit int
	}{
		{
			"exec not covered", manifest("demo", "1.0.0", nil, cmdWith("go", "")),
			CodeExecUncovered, ExitPolicy,
		},
		{"syntax", "id = ", CodeManifestInvalid, ExitFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			dir := l.srcDir("demo", map[string]string{"plugin.toml": tt.src})

			pe := planErr(t, l.reg(nil), InstallRequest{Source: dir})
			if pe.Code != tt.code || ExitOf(pe) != tt.exit {
				t.Errorf("error = %v, exit %d", pe, ExitOf(pe))
			}
		})
	}
}

// TestExitClassOfTreeFailures checks that every failure that says the files of an installed
// plugin were modified is an integrity failure, for the error of an enable too.
//
// Parameters:
//   - t: test to report to
func TestExitClassOfTreeFailures(t *testing.T) {
	tests := []struct {
		name string
		code string
		args []string
		exit int
	}{
		{"tree hash", CodeTreeHash, nil, ExitIntegrity},
		{"tree perms", CodeTreePerms, nil, ExitIntegrity},
		{"tree symlink", CodeTreeSymlink, nil, ExitIntegrity},
		{"tree missing", CodeTreeMissing, nil, ExitFailure},
		{"enable of a modified tree", CodeEnableRefused, []string{CodeTreeHash}, ExitIntegrity},
		{
			"enable of a disabled session", CodeEnableRefused,
			[]string{ReasonSessionOff},
			ExitPolicy,
		},
		{
			"enable of an incompatible plugin", CodeEnableRefused,
			[]string{CodeToteRange},
			ExitPolicy,
		},
		{"unauthorized download", CodeNetStatus, []string{"u", "401"}, ExitAuth},
	}

	for _, tt := range tests {
		if got := ExitFor(tt.code, tt.args); got != tt.exit {
			t.Errorf("%s: ExitFor = %d, want %d", tt.name, got, tt.exit)
		}
	}
}

// TestDiskErrorNamesThePath checks that the error of a lock write names the file it could not
// write, and that a write which fails leaves no empty directory of the id behind.
//
// Parameters:
//   - t: test to report to
func TestDiskErrorNamesThePath(t *testing.T) {
	pe := asError(t, writeError(&fs.PathError{
		Op: "open", Path: "/state/plugins.lock",
		Err: fs.ErrPermission,
	}))
	if pe.Code != CodeDisk || len(pe.Args) != 1 || pe.Args[0] != "/state/plugins.lock" {
		t.Errorf("error = %v", pe)
	}

	l := newLab(t)
	l.fakeBin("tool")

	dir := l.srcDir("demo", map[string]string{"plugin.toml": surfaceBase("", "", "")})
	withHook(t, failAt(PointC2Before))

	st := plan(t, l.reg(nil), InstallRequest{Source: dir})
	if _, err := st.Commit(Grant{Prompt: true}); err == nil {
		t.Fatal("the commit succeeded")
	}

	idDir, _ := l.paths.PluginDir("demo")
	if _, err := os.Stat(idDir); !os.IsNotExist(err) {
		t.Errorf("the directory of the id is still there: %v", err)
	}
}

// TestSameVersionWarning checks that a forced update to the version that is installed warns that
// it is installed, not that it is older, and that the view says so.
//
// Parameters:
//   - t: test to report to
func TestSameVersionWarning(t *testing.T) {
	l := newLab(t)
	plain := surfaceBase("", "", "")
	r, dir := seeded(t, l, plain)

	st := plan(t, r, InstallRequest{Source: dir, Force: true})
	defer func() { _ = st.Abort() }()

	d := st.Diff()
	if !hasReason(d.Warnings, WarnSameVersion) || hasReason(d.Warnings, WarnDowngrade) {
		t.Fatalf("warnings = %v", d.Warnings)
	}

	keys := viewKeys(NewConsentView(d, ViewUpdate, false))
	if !slices.Contains(keys, "consent_warn_same_version") ||
		slices.Contains(keys, "consent_warn_downgrade") {
		t.Errorf("keys = %v", keys)
	}
}

// TestConsentViewCapabilityGroups checks that the access is listed by group (terminal, programs,
// network, files, environment, host), whatever the order of the strings.
//
// Parameters:
//   - t: test to report to
func TestConsentViewCapabilityGroups(t *testing.T) {
	d := Diff{
		ID: "demo", Version: "1.0.0", Tier: TierExec, Kind: KindLocal, Class: OriginLocal,
		Caps: []string{
			"host:read:repos", "env:MY_VAR", "fs:read:home", "net:example.org",
			"exec:git", "derived:tty", "derived:code",
		},
	}

	var got []string

	for _, l := range NewConsentView(d, ViewInstall, false).Lines {
		if strings.HasPrefix(l.Key, "consent_cap_") {
			got = append(got, l.Key)
		}
	}

	want := []string{
		"consent_cap_tty", "consent_cap_code", "consent_cap_exec", "consent_cap_net",
		"consent_cap_fs_read", "consent_cap_env", "consent_cap_read_repos",
	}
	if !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

// TestPiecesOf checks the list of what a plugin adds: the tables, the ids and the labels, which
// a catalog may translate.
//
// Parameters:
//   - t: test to report to
func TestPiecesOf(t *testing.T) {
	m := &Manifest{
		Commands: []Entry{{Table: "command", ID: "hello", Label: "Hello"}},
		Lists:    []List{{ID: "all", Label: "All"}},
		Settings: []Setting{{ID: "limit", Label: "Limit"}},
	}

	cat := NewCatalog(map[string]map[string]string{"fr": {"command.hello.label": "Bonjour"}})

	want := []Piece{
		{"command", "hello", "Bonjour"},
		{"list", "all", "All"},
		{"setting", "limit", "Limit"},
	}
	if got := PiecesOf(m, cat, "fr"); !slices.Equal(got, want) {
		t.Errorf("PiecesOf = %v, want %v", got, want)
	}

	if PiecesOf(nil, cat, "en") != nil {
		t.Error("a plugin with no manifest has pieces")
	}
}
