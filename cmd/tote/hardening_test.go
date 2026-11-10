package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/update"
)

// TestReviewFixKeyOverrideRefusedForRoot checks that root does not accept a replacement of the
// release keys through the environment, whatever the source.
//
// Parameters:
//   - t: test to report to
func TestReviewFixKeyOverrideRefusedForRoot(t *testing.T) {
	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	m.publish("1.1.0")

	target := installFake(h, m, "1.0.0")
	h.app.Geteuid = func() int { return 0 }

	if code := h.run("update", "--json"); code != 1 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	doc := failOut(t, h)
	if doc.Error.Code != "bad_key" || !strings.Contains(doc.Error.Message, "root") {
		t.Errorf("doc %+v", doc)
	}

	if data, _ := os.ReadFile(target); string(data) != fakeBinary("1.0.0") {
		t.Errorf("the binary was replaced: %q", data)
	}
}

// TestReviewFixRollbackGuardUnderSudo checks that the receipt is still read under sudo or doas,
// so that an older release is refused, while nothing is written to the home of the user.
//
// Parameters:
//   - t: test to report to
func TestReviewFixRollbackGuardUnderSudo(t *testing.T) {
	for _, user := range []string{"SUDO_USER", "DOAS_USER"} {
		h := newHarness(t)
		m := newTestMirror(t, newTestSigner(1))
		m.publish("1.1.0")

		target := installFake(h, m, "1.0.0")
		writeReceipt(h, update.Receipt{
			InstallMethod: "script", ReleaseTimestamp: testStamp + 1,
			Path: target, Version: "1.0.0",
		})

		h.app.Geteuid = func() int { return 0 }
		h.vars[user] = "alice"
		delete(h.vars, "TOTE_UPDATE_PUBKEY")
		h.app.Updater.Keys = []update.PublicKey{mustKey(t, m.signer)}

		if code := h.run("update", "--json"); code != 1 {
			t.Fatalf("%s: code %d, stderr %q", user, code, h.errb.String())
		}

		if doc := failOut(t, h); doc.Error.Code != "rollback" {
			t.Errorf("%s: doc %+v, want a rollback refusal", user, doc)
		}
	}
}

// TestReviewFixFrenchErrorsHaveNoEnglishTail checks the failures whose detail used to be an
// English sentence: an insecure source, a rollback and a signed comment.
//
// Parameters:
//   - t: test to report to
func TestReviewFixFrenchErrorsHaveNoEnglishTail(t *testing.T) {
	h := newHarness(t)
	h.vars["TOTE_LANG"] = "fr"

	tests := []struct {
		err  error
		want string
	}{
		{&update.InsecureError{URL: "http://x"}, "http://x"},
		{&update.InsecureError{URL: "http://x", Redirect: true}, "redirige"},
		{&update.RollbackError{Signed: 1000, Installed: 2000}, "sign\u00e9e le 1970-01-01"},
		{&update.CommentError{Field: "channel"}, "champ"},
		{update.ErrNoKey, "cl\u00e9"},
		{update.ErrKeyFormat, "cl\u00e9"},
	}

	for _, tt := range tests {
		key, args := h.app.updateErrorMessage(tt.err, installCtx{})
		msg := h.app.t(key, args...)

		if !strings.Contains(msg, tt.want) {
			t.Errorf("%v: %q does not contain %q", tt.err, msg, tt.want)
		}

		for _, w := range []string{"must start", "signed at", "expected", "insecure source"} {
			if strings.Contains(msg, w) {
				t.Errorf("%v: English text %q in %q", tt.err, w, msg)
			}
		}
	}
}

// TestReviewFixCertificateMessage checks that an untrusted certificate is not reported as a
// connectivity problem.
//
// Parameters:
//   - t: test to report to
func TestReviewFixCertificateMessage(t *testing.T) {
	h := newHarness(t)

	key, args := h.app.updateErrorMessage(
		wrapErr(update.ErrCertificate, "x509: certificate signed by unknown authority"),
		installCtx{},
	)

	msg := h.app.t(key, args...)
	if strings.Contains(msg, "offline") || !strings.Contains(msg, "certificate") {
		t.Errorf("message %q", msg)
	}

	if update.Code(wrapErr(update.ErrCertificate, "x")) != "certificate" {
		t.Error("the JSON code of a certificate failure is not certificate")
	}
}

// wrapErr wraps a sentinel the way the update package does.
//
// Parameters:
//   - target: error to wrap
//   - detail: text added after it
func wrapErr(target error, detail string) error {
	return &wrapped{target: target, text: target.Error() + ": " + detail}
}

// wrapped is an error with a text of its own that unwraps to a sentinel.
type wrapped struct {
	target error
	text   string
}

// Error implements error.
func (w *wrapped) Error() string { return w.text }

// Unwrap returns the sentinel.
func (w *wrapped) Unwrap() error { return w.target }

// TestReviewFixUninstallRemovesPathLinks checks that a symbolic link named tote in the PATH that
// leads to the program is listed and removed with it, and that a link to another program stays.
//
// Parameters:
//   - t: test to report to
func TestReviewFixUninstallRemovesPathLinks(t *testing.T) {
	h, target := uninstallSetup(t)

	bin := filepath.Join(h.dir, "linkbin")
	other := filepath.Join(h.dir, "otherbin")

	for _, d := range []string{bin, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	elsewhere := filepath.Join(h.dir, "elsewhere")
	if err := os.WriteFile(elsewhere, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(bin, "tote")
	foreign := filepath.Join(other, "tote")

	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(elsewhere, foreign); err != nil {
		t.Fatal(err)
	}

	h.vars["PATH"] = bin + string(os.PathListSeparator) + other

	if code := h.run("uninstall", "--yes"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if exists(link) {
		t.Error("the link to the program is still on the PATH")
	}

	if !exists(foreign) {
		t.Error("a link to another program was removed")
	}

	if !strings.Contains(h.out.String(), "the link "+link) {
		t.Errorf("the plan does not list the link: %q", h.out.String())
	}
}

// TestReviewFixPurgeSharedDirectoryKeepsFiles checks the message and the result when a purge
// meets a directory entry that is not empty inside a directory not named tote.
//
// Parameters:
//   - t: test to report to
func TestReviewFixPurgeSharedDirectoryKeepsFiles(t *testing.T) {
	h, _ := uninstallSetup(t)

	shared := filepath.Join(h.dir, "shared")
	foreign := filepath.Join(shared, "bin", "mytool")

	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(foreign, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_DATA_DIR"] = shared

	if code := h.run("uninstall", "--yes", "--purge"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if !exists(foreign) {
		t.Error("a file of another program was deleted")
	}

	if !strings.Contains(h.out.String(), "kept "+filepath.Join(shared, "bin")) {
		t.Errorf("the output does not say what was kept: %q", h.out.String())
	}
}

// TestReviewFixCompletionSkipsTypedFlags checks that an option already typed is not offered
// again, and that doctor offers only one of --json and --reset.
//
// Parameters:
//   - t: test to report to
func TestReviewFixCompletionSkipsTypedFlags(t *testing.T) {
	tests := []struct {
		verb  string
		typed []string
		want  []string
	}{
		{"update", []string{"--check"}, []string{"--json", "--beta", "--force"}},
		{"update", nil, []string{"--check", "--json", "--beta", "--force"}},
		{"doctor", []string{"--json"}, nil},
		{"doctor", []string{"--reset"}, nil},
		{"doctor", nil, []string{"--json", "--reset"}},
		{"alias", nil, []string{"--format", "--json"}},
		{"funcs", []string{"--json"}, nil},
		{"repos", []string{"--format"}, nil},
		{"co", nil, []string{"--format", "--json"}},
	}

	for _, tt := range tests {
		var got []string

		for _, l := range flagCandidates(tt.verb, "", tt.typed) {
			got = append(got, strings.SplitN(l, "\t", 2)[0])
		}

		if strings.Join(got, " ") != strings.Join(tt.want, " ") {
			t.Errorf("%s after %v: offered %v, want %v", tt.verb, tt.typed, got, tt.want)
		}
	}
}

// TestReviewFixStatusNotice checks the version the status line announces: the cached one with
// no request, and nothing for an up to date or development build.
//
// Parameters:
//   - t: test to report to
func TestReviewFixStatusNotice(t *testing.T) {
	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	m.publish("1.1.0")

	installFake(h, m, "1.0.0")

	a := *h.app
	a.Stdin = strings.NewReader("")

	if got := a.statusNotice(context.Background()); got != "1.1.0" {
		t.Errorf("first call = %q, want 1.1.0 (from the release list)", got)
	}

	h.vars["TOTE_UPDATE_BASE"] = "file://" + filepath.Join(h.dir, "gone")

	if got := a.statusNotice(context.Background()); got != "1.1.0" {
		t.Errorf("cached call = %q, want 1.1.0", got)
	}

	a.Version = "1.1.0"

	if got := a.statusNotice(context.Background()); got != "" {
		t.Errorf("up to date build = %q, want nothing", got)
	}
}

// TestReviewFixDoctorShellUnset checks that an unset SHELL variable gets its own message and
// not "the shell - is not supported".
//
// Parameters:
//   - t: test to report to
func TestReviewFixDoctorShellUnset(t *testing.T) {
	f := newFixture(t)
	f.vars["TOTE_SHELL"], f.vars["SHELL"] = "", ""

	x := get(t, f.run(), "shell")
	if x.Msg.Key != "cli_doctor_shell_unset" {
		t.Errorf("message %q", x.Msg.Key)
	}
}
