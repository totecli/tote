package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/update"
)

// jsonOut decodes the standard output of a command as an updateInfo.
//
// Parameters:
//   - t: running test
//   - h: harness whose output is read
func jsonOut(t *testing.T, h *harness) updateInfo {
	t.Helper()

	var info updateInfo
	if err := json.Unmarshal(h.out.Bytes(), &info); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", h.out.String(), err)
	}

	return info
}

// failOut decodes the standard output of a failed command as an updateFailure.
//
// Parameters:
//   - t: running test
//   - h: harness whose output is read
func failOut(t *testing.T, h *harness) updateFailure {
	t.Helper()

	var doc updateFailure
	if err := json.Unmarshal(h.out.Bytes(), &doc); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", h.out.String(), err)
	}

	return doc
}

// writeReceipt stores an install receipt in the state directory of the harness.
//
// Parameters:
//   - h: harness
//   - r: receipt to store
func writeReceipt(h *harness, r update.Receipt) {
	h.t.Helper()

	path := filepath.Join(h.vars["TOTE_STATE_DIR"], "install.json")
	if err := update.WriteReceipt(path, r); err != nil {
		h.t.Fatal(err)
	}
}

// TestUpdateCheck checks "tote update --check" for every outcome, in text and in JSON, and its
// exit code.
//
// Parameters:
//   - t: test to report to
func TestUpdateCheck(t *testing.T) {
	tests := []struct {
		name    string
		version string
		tags    []string
		args    []string
		code    int
		status  string
		text    string
	}{
		{
			"available", "1.0.0",
			[]string{"1.0.0", "1.1.0"},
			nil, 10, "available",
			"update available: 1.1.0 (installed: 1.0.0, stable channel)",
		},
		{"up to date", "1.1.0", []string{"1.1.0"}, nil, 0, "up-to-date", "tote 1.1.0 is up to date"},
		{
			"newer locally", "1.2.0",
			[]string{"1.1.0"},
			nil, 0, "newer",
			"newer than the latest release 1.1.0",
		},
		{"dev build", "dev", []string{"1.1.0"}, nil, 0, "dev", "development build"},
		{
			"beta hidden from stable", "1.0.0",
			[]string{"1.0.0", "1.1.0-beta.1"},
			nil, 0, "up-to-date",
			"is up to date",
		},
		{
			"beta with --beta", "1.0.0",
			[]string{"1.0.0", "1.1.0-beta.1"},
			[]string{"--beta"},
			10,
			"available", "1.1.0-beta.1",
		},
		{
			"beta build follows betas", "1.1.0-beta.1",
			[]string{"1.1.0-beta.2"},
			nil, 10, "available",
			"1.1.0-beta.2",
		},
	}

	for _, tt := range tests {
		for _, asJSON := range []bool{false, true} {
			h := newHarness(t)
			m := newTestMirror(t, newTestSigner(1))

			for _, tag := range tt.tags {
				m.publish(tag)
			}

			installFake(h, m, tt.version)

			args := append([]string{"update", "--check"}, tt.args...)
			if asJSON {
				args = append(args, "--json")
			}

			if code := h.run(args...); code != tt.code {
				t.Errorf("%s json=%v: code %d, stderr %q", tt.name, asJSON, code, h.errb.String())
				continue
			}

			if asJSON {
				info := jsonOut(t, h)
				if info.Status != tt.status || info.Current != tt.version || info.Available != (tt.code == 10) {
					t.Errorf("%s: %+v", tt.name, info)
				}

				continue
			}

			if !strings.Contains(h.out.String(), tt.text) {
				t.Errorf("%s: stdout %q lacks %q", tt.name, h.out.String(), tt.text)
			}
		}
	}
}

// TestUpdateApply checks a real update from a mirror: the program is replaced, the receipt is
// written and the messages are right in both languages.
//
// Parameters:
//   - t: test to report to
func TestUpdateApply(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		h := newHarness(t)
		h.vars["TOTE_LANG"] = lang
		m := newTestMirror(t, newTestSigner(1))
		m.publish("1.0.0")
		m.publish("1.1.0")

		target := installFake(h, m, "1.0.0")

		if code := h.run("update"); code != 0 {
			t.Fatalf("%s: code %d, stderr %q", lang, code, h.errb.String())
		}

		data, _ := os.ReadFile(target)
		if string(data) != fakeBinary("1.1.0") {
			t.Errorf("%s: binary = %q", lang, data)
		}

		want := "tote updated from 1.0.0 to 1.1.0 (" + target + ")\n"
		if lang == "fr" {
			want = "tote est pass\u00e9 de 1.0.0 \u00e0 1.1.0 (" + target + ")\n"
		}

		if h.out.String() != want {
			t.Errorf("%s: stdout %q, want %q", lang, h.out.String(), want)
		}

		if strings.Count(h.errb.String(), "\n") != 3 {
			t.Errorf("%s: progress lines %q", lang, h.errb.String())
		}

		r, err := update.ReadReceipt(filepath.Join(h.vars["TOTE_STATE_DIR"], "install.json"))
		if err != nil || r.Version != "1.1.0" || r.Path != target || r.ReleaseTimestamp != testStamp {
			t.Errorf("%s: receipt %+v, %v", lang, r, err)
		}

		// A second run, now as the new version, has nothing to do.
		h.app.Version = "1.1.0"

		again := "is up to date"
		if lang == "fr" {
			again = "est \u00e0 jour"
		}

		if code := h.run("update"); code != 0 || !strings.Contains(h.out.String(), again) {
			t.Errorf("%s: second run code %d, stdout %q", lang, code, h.out.String())
		}

		entries, _ := os.ReadDir(filepath.Dir(target))
		if len(entries) != 1 {
			t.Errorf("%s: directory holds %d entries", lang, len(entries))
		}
	}
}

// TestUpdateApplyJSON checks the JSON of an update, and that the progress stays silent.
//
// Parameters:
//   - t: test to report to
func TestUpdateApplyJSON(t *testing.T) {
	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	m.publish("1.1.0")

	target := installFake(h, m, "1.0.0")

	if code := h.run("update", "--json"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	info := jsonOut(t, h)
	if info.Status != "updated" || !info.Updated || info.Latest != "1.1.0" ||
		info.Path != target || h.errb.Len() != 0 {
		t.Errorf("info %+v, stderr %q", info, h.errb.String())
	}
}

// TestUpdateForce checks --force for a downgrade and a development build.
//
// Parameters:
//   - t: test to report to
func TestUpdateForce(t *testing.T) {
	tests := []struct {
		name    string
		version string
		force   bool
		after   string
		text    string
	}{
		{"downgrade refused", "1.2.0", false, "1.2.0", "newer than the latest release"},
		{"downgrade forced", "1.2.0", true, "1.1.0", "updated from 1.2.0 to 1.1.0"},
		{"dev refused", "dev", false, "dev", "development build"},
		{"dev forced", "dev", true, "1.1.0", "updated from dev to 1.1.0"},
		{"reinstall forced", "1.1.0", true, "1.1.0", "reinstalled"},
	}

	for _, tt := range tests {
		h := newHarness(t)
		m := newTestMirror(t, newTestSigner(1))
		m.publish("1.1.0")

		target := installFake(h, m, tt.version)
		args := []string{"update"}

		if tt.force {
			args = append(args, "--force")
		}

		if code := h.run(args...); code != 0 {
			t.Errorf("%s: code %d, stderr %q", tt.name, code, h.errb.String())
		}

		if data, _ := os.ReadFile(target); string(data) != fakeBinary(tt.after) {
			t.Errorf("%s: binary %q, want version %s", tt.name, data, tt.after)
		}

		if !strings.Contains(h.out.String(), tt.text) {
			t.Errorf("%s: stdout %q lacks %q", tt.name, h.out.String(), tt.text)
		}
	}
}

// TestUpdateManaged checks that an apt or brew install is never touched, nor even looked up.
//
// Parameters:
//   - t: test to report to
func TestUpdateManaged(t *testing.T) {
	tests := []struct {
		method  string
		command string
	}{
		{"apt", "sudo apt update && sudo apt upgrade tote"},
		{"brew", "brew upgrade tote"},
	}

	for _, tt := range tests {
		for _, asJSON := range []bool{false, true} {
			h := newHarness(t)
			m := newTestMirror(t, newTestSigner(1))
			m.publish("2.0.0")

			target := installFake(h, m, "1.0.0")
			h.vars["TOTE_UPDATE_BASE"] = "file://" + filepath.Join(h.dir, "never-read")
			writeReceipt(h, update.Receipt{InstallMethod: tt.method, Path: target})

			args := []string{"update"}
			if asJSON {
				args = append(args, "--json")
			}

			if code := h.run(args...); code != 0 {
				t.Errorf("%s: code %d, stderr %q", tt.method, code, h.errb.String())
			}

			if data, _ := os.ReadFile(target); string(data) != fakeBinary("1.0.0") {
				t.Errorf("%s: the binary changed", tt.method)
			}

			if asJSON {
				info := jsonOut(t, h)
				if info.Status != "managed" || info.ManagedBy != tt.method || info.Command != tt.command {
					t.Errorf("%s: %+v", tt.method, info)
				}

				continue
			}

			if o := h.out.String(); !strings.Contains(o, tt.command) ||
				!strings.Contains(o, "managed by "+tt.method) {
				t.Errorf("%s: stdout %q", tt.method, h.out.String())
			}
		}
	}
}

// TestUpdateFailures checks the message, the JSON name and the exit code of each failure, and
// that the binary is never changed.
//
// Parameters:
//   - t: test to report to
func TestUpdateFailures(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(h *harness, m *testMirror, target string)
		code   int
		errKey string
		text   string
	}{
		{"flipped checksums", func(h *harness, m *testMirror, target string) {
			p := filepath.Join(m.dir, "download", "1.1.0", update.ChecksumsName("1.1.0"))
			b, _ := os.ReadFile(p)
			b[0] ^= 1
			_ = os.WriteFile(p, b, 0o644)
		}, 1, "bad_signature", "signature of the release does not verify"},
		{"archive altered", func(h *harness, m *testMirror, target string) {
			p := filepath.Join(m.dir, "download", "1.1.0", update.ArchiveName("1.1.0", "linux", "amd64"))
			b, _ := os.ReadFile(p)
			b[len(b)/2] ^= 1
			_ = os.WriteFile(p, b, 0o644)
		}, 1, "bad_checksum", "does not match its signed checksum"},
		{"signed by another key", func(h *harness, m *testMirror, target string) {
			h.vars["TOTE_UPDATE_PUBKEY"] = newTestSigner(2).encoded()
		}, 1, "unknown_key", "install.sh or with your package manager"},
		{"no key in this build", func(h *harness, m *testMirror, target string) {
			delete(h.vars, "TOTE_UPDATE_PUBKEY")
		}, 1, "no_key", "no release key"},
		{"key override with an https source", func(h *harness, m *testMirror, target string) {
			h.vars["TOTE_UPDATE_BASE"] = "https://mirror.invalid/tote"
		}, 1, "bad_key", "only accepted with a file:// source"},
		{"bad key override", func(h *harness, m *testMirror, target string) {
			h.vars["TOTE_UPDATE_PUBKEY"] = "not a key"
		}, 1, "bad_key", "valid minisign public key"},
		{"http source", func(h *harness, m *testMirror, target string) {
			delete(h.vars, "TOTE_UPDATE_PUBKEY")
			h.vars["TOTE_UPDATE_BASE"] = "http://mirror.invalid/tote"
		}, 1, "insecure_source", "must use https://"},
		{"no mirror", func(h *harness, m *testMirror, target string) {
			h.vars["TOTE_UPDATE_BASE"] = "file://" + filepath.Join(h.dir, "absent")
		}, 1, "no_release", "no release found"},
		{"interrupted", func(h *harness, m *testMirror, target string) {
			h.app.Context = func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx, cancel
			}
		}, 130, "interrupted", "interrupted"},
		{"rollback", func(h *harness, m *testMirror, target string) {
			writeReceipt(h, update.Receipt{
				InstallMethod: "script", ReleaseTimestamp: testStamp + 1,
				Path: target,
			})
		}, 1, "rollback", "older than the installed one"},
	}

	for _, tt := range tests {
		h := newHarness(t)
		m := newTestMirror(t, newTestSigner(1))
		m.publish("1.1.0")

		target := installFake(h, m, "1.0.0")
		tt.setup(h, m, target)

		for _, asJSON := range []bool{false, true} {
			args := []string{"update"}
			if asJSON {
				args = append(args, "--json")
			}

			if code := h.run(args...); code != tt.code {
				t.Errorf("%s json=%v: code %d, stderr %q", tt.name, asJSON, code, h.errb.String())
			}

			if !asJSON && !strings.Contains(h.errb.String(), tt.text) {
				t.Errorf("%s: stderr %q lacks %q", tt.name, h.errb.String(), tt.text)
			}

			if asJSON {
				doc := failOut(t, h)
				if doc.Schema != 1 || doc.OK || doc.Status != "error" ||
					doc.Error.Code != tt.errKey || doc.Error.Exit != tt.code ||
					!strings.Contains(doc.Error.Message, tt.text) {
					t.Errorf("%s: %+v", tt.name, doc)
				}

				if h.errb.Len() != 0 {
					t.Errorf("%s: stderr %q in JSON mode", tt.name, h.errb.String())
				}
			}

			if data, _ := os.ReadFile(target); string(data) != fakeBinary("1.0.0") {
				t.Errorf("%s: the binary changed", tt.name)
			}
		}
	}
}

// TestUpdateNetworkFailures checks the offline and rate limit reports over https.
//
// Parameters:
//   - t: test to report to
func TestUpdateNetworkFailures(t *testing.T) {
	reset := time.Date(2026, 11, 3, 14, 32, 0, 0, time.Local)

	tests := []struct {
		name    string
		handler http.HandlerFunc
		closed  bool
		errKey  string
		text    string
	}{
		{"offline", nil, true, "offline", "cannot reach the release server"},
		{"rate limited", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
		}, false, "rate_limited", "try again after " + reset.Format(time.TimeOnly)},
		{"rate limited without a date", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}, false, "rate_limited", "try again later"},
		{"server error", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}, false, "server", "answered unexpectedly"},
		{"no such repository", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}, false, "no_release", "no release found"},
	}

	for _, tt := range tests {
		for _, verb := range [][]string{{"update"}, {"update", "--check"}} {
			h := newHarness(t)
			m := newTestMirror(t, newTestSigner(1))
			m.publish("1.1.0")
			target := installFake(h, m, "1.0.0")

			handler := tt.handler
			if handler == nil {
				handler = func(http.ResponseWriter, *http.Request) {}
			}

			srv := httptest.NewTLSServer(handler)
			h.app.Updater.Source.Client = srv.Client()
			h.vars["TOTE_UPDATE_BASE"] = srv.URL
			delete(h.vars, "TOTE_UPDATE_PUBKEY")
			h.app.Updater.Keys = []update.PublicKey{mustKey(t, m.signer)}

			if tt.closed {
				srv.Close()
			}

			args := append(slices.Clone(verb), "--json")
			if code := h.run(args...); code != 1 {
				t.Errorf("%s %v: code %d, stderr %q", tt.name, verb, code, h.errb.String())
			}

			if doc := failOut(t, h); doc.Error.Code != tt.errKey || h.errb.Len() != 0 {
				t.Errorf("%s %v: %+v, stderr %q", tt.name, verb, doc, h.errb.String())
			}

			if code := h.run(verb...); code != 1 || !strings.Contains(h.errb.String(), tt.text) {
				t.Errorf("%s %v: code %d, stderr %q lacks %q", tt.name, verb, code, h.errb.String(), tt.text)
			}

			if data, _ := os.ReadFile(target); string(data) != fakeBinary("1.0.0") {
				t.Errorf("%s: the binary changed", tt.name)
			}

			srv.Close()
		}
	}
}

// mustKey parses the public key of a test signer.
//
// Parameters:
//   - t: running test
//   - s: signer
func mustKey(t *testing.T, s testSigner) update.PublicKey {
	t.Helper()

	k, err := update.ParsePublicKey(s.encoded())
	if err != nil {
		t.Fatal(err)
	}

	return k
}

// TestUpdateNotWritable checks the report for a directory the user cannot write in: what to
// run, and no change.
//
// Parameters:
//   - t: test to report to
func TestUpdateNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}

	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	m.publish("1.1.0")

	target := installFake(h, m, "1.0.0")
	dir := filepath.Dir(target)

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if code := h.run("update"); code != 1 {
		t.Errorf("code %d", code)
	}

	if !strings.Contains(h.errb.String(), "cannot write in "+dir) ||
		!strings.Contains(h.errb.String(), "sudo "+shell.Quote(target)+" update") {
		t.Errorf("stderr %q", h.errb.String())
	}

	if data, _ := os.ReadFile(target); string(data) != fakeBinary("1.0.0") {
		t.Error("the binary changed")
	}
}

// TestUpdateReceipts checks which receipts an update writes, keeps or leaves alone.
//
// Parameters:
//   - t: test to report to
func TestUpdateReceipts(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(h *harness, target string)
		sudo    bool
		version string
	}{
		{"another program's receipt is kept", func(h *harness, target string) {
			writeReceipt(h, update.Receipt{
				InstallMethod: "script", Path: "/elsewhere/tote", Version: "0.1.0",
			})
		}, false, "0.1.0"},
		{"root through sudo writes nothing", func(h *harness, target string) {
			writeReceipt(h, update.Receipt{InstallMethod: "script", Path: target, Version: "0.1.0"})
		}, true, "0.1.0"},
		{"its own receipt is updated", func(h *harness, target string) {
			writeReceipt(h, update.Receipt{InstallMethod: "script", Path: target, Version: "0.1.0"})
		}, false, "1.1.0"},
	}

	for _, tt := range tests {
		h := newHarness(t)
		m := newTestMirror(t, newTestSigner(1))
		m.publish("1.1.0")

		target := installFake(h, m, "1.0.0")
		tt.setup(h, target)

		if tt.sudo {
			h.app.Geteuid = func() int { return 0 }
			h.vars["SUDO_USER"] = "alice"

			// Root does not accept the override of the release key.
			delete(h.vars, "TOTE_UPDATE_PUBKEY")
			h.app.Updater.Keys = []update.PublicKey{mustKey(t, m.signer)}
		}

		if code := h.run("update"); code != 0 {
			t.Fatalf("%s: code %d, stderr %q", tt.name, code, h.errb.String())
		}

		r, err := update.ReadReceipt(filepath.Join(h.vars["TOTE_STATE_DIR"], "install.json"))
		if err != nil || r.Version != tt.version {
			t.Errorf("%s: receipt %+v, %v; want version %s", tt.name, r, err, tt.version)
		}
	}
}

// TestUpdateChannelFromReceipt checks that a beta receipt opts in to pre-releases.
//
// Parameters:
//   - t: test to report to
func TestUpdateChannelFromReceipt(t *testing.T) {
	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	m.publish("1.0.0")
	m.publish("1.1.0-beta.1")

	target := installFake(h, m, "1.0.0")

	if code := h.run("update", "--check", "--json"); code != 0 || jsonOut(t, h).Latest != "1.0.0" {
		t.Errorf("without a receipt: code %d, %q", code, h.out.String())
	}

	writeReceipt(h, update.Receipt{InstallMethod: "script", Channel: "beta", Path: target})

	code := h.run("update", "--check", "--json")
	if code != 10 || jsonOut(t, h).Latest != "1.1.0-beta.1" {
		t.Errorf("with a beta receipt: code %d, %q", code, h.out.String())
	}
}

// TestUpdateUsage checks the misuse of the verb.
//
// Parameters:
//   - t: test to report to
func TestUpdateUsage(t *testing.T) {
	h := newHarness(t)

	for _, args := range [][]string{
		{"update", "extra"}, {"update", "--nope"}, {"uninstall", "extra"}, {"uninstall", "--nope"},
	} {
		if code := h.run(args...); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}

	for _, verb := range []string{"update", "uninstall"} {
		if code := h.run(verb, "--help"); code != 0 || !strings.Contains(h.out.String(), "tote "+verb) {
			t.Errorf("%s --help: code %d, stdout %q", verb, code, h.out.String())
		}
	}

	if code := h.run("update", "-h"); code != 0 || !strings.Contains(h.out.String(), "exits with 0") {
		t.Errorf("update -h: %q", h.out.String())
	}
}

// TestUpdateCompletion checks that the verbs and their options are completed.
//
// Parameters:
//   - t: test to report to
func TestUpdateCompletion(t *testing.T) {
	h := newHarness(t)

	all := names(completeLines(t, h, ""))
	for _, v := range []string{"update", "uninstall"} {
		if !slices.Contains(all, v) {
			t.Errorf("verb %s is not completed: %v", v, all)
		}
	}

	wantFlags := []string{"--check", "--json", "--beta", "--force"}
	if got := names(completeLines(t, h, "update", "--")); !slices.Equal(got, wantFlags) {
		t.Errorf("update options = %v", got)
	}

	if got := names(completeLines(t, h, "update", "--c")); !slices.Equal(got, []string{"--check"}) {
		t.Errorf("update --c = %v", got)
	}

	if got := completeLines(t, h, "update", "x"); got != nil {
		t.Errorf("update x = %v", got)
	}
}

// TestUpdateNotice checks the status line helper: no network, the opt-out, and the cases
// where nothing is announced.
//
// Parameters:
//   - t: test to report to
func TestUpdateNotice(t *testing.T) {
	setup := func() (*harness, string) {
		h := newHarness(t)
		m := newTestMirror(t, newTestSigner(1))
		m.publish("1.1.0")

		return h, installFake(h, m, "1.0.0")
	}

	t.Run("refresh then read", func(t *testing.T) {
		h, _ := setup()
		a := *h.app
		a.Stdin = strings.NewReader("")

		if _, ok := a.updateNotice(); ok {
			t.Error("a notice exists before any check")
		}

		n, err := a.refreshUpdateNotice(context.Background())
		if err != nil || !n.Available || n.Latest != "1.1.0" {
			t.Fatalf("refresh = %+v, %v", n, err)
		}

		// The mirror vanishes: the cache answers without any request.
		h.vars["TOTE_UPDATE_BASE"] = "file://" + filepath.Join(h.dir, "gone")

		if n, ok := a.updateNotice(); !ok || n.Latest != "1.1.0" {
			t.Errorf("notice = %+v, %v", n, ok)
		}

		if n, err := a.refreshUpdateNotice(context.Background()); err != nil || n.Latest != "1.1.0" {
			t.Errorf("second refresh = %+v, %v", n, err)
		}

		if _, err := os.Stat(filepath.Join(h.vars["TOTE_CACHE_DIR"], update.CacheFile)); err != nil {
			t.Error(err)
		}
	})

	t.Run("opt out", func(t *testing.T) {
		h, _ := setup()
		h.write("config.toml", "schema = 1\n[update]\ncheck = false\n")

		a := *h.app
		if n, err := a.refreshUpdateNotice(context.Background()); err != nil || n.Latest != "" {
			t.Errorf("refresh with update.check = false: %+v, %v", n, err)
		}

		if _, err := os.Stat(filepath.Join(h.vars["TOTE_CACHE_DIR"], update.CacheFile)); err == nil {
			t.Error("a cache was written despite the opt-out")
		}
	})

	t.Run("dev build", func(t *testing.T) {
		h, _ := setup()
		h.app.Version = "dev"
		a := *h.app

		if n, err := a.refreshUpdateNotice(context.Background()); err != nil || n.Latest != "" {
			t.Errorf("dev refresh: %+v, %v", n, err)
		}
	})

	t.Run("managed install", func(t *testing.T) {
		h, target := setup()
		writeReceipt(h, update.Receipt{InstallMethod: "apt", Path: target})
		a := *h.app

		if n, err := a.refreshUpdateNotice(context.Background()); err != nil || n.Latest != "" {
			t.Errorf("managed refresh: %+v, %v", n, err)
		}
	})

	t.Run("failure is recorded", func(t *testing.T) {
		h, _ := setup()
		h.vars["TOTE_UPDATE_BASE"] = "file://" + filepath.Join(h.dir, "gone")
		a := *h.app

		if _, err := a.refreshUpdateNotice(context.Background()); err == nil {
			t.Error("a failed check returned no error")
		}

		h.vars["TOTE_UPDATE_BASE"] = "http://insecure.invalid"

		if n, err := a.refreshUpdateNotice(context.Background()); err == nil && n.Available {
			t.Errorf("refresh within the day made a request: %+v", n)
		}
	})
}

// TestUpdateNoticeNeedsReleaseKey checks that a build without a release key makes no request for
// the status line and records nothing, since it could not install what it announced.
//
// Parameters:
//   - t: test to report to
func TestUpdateNoticeNeedsReleaseKey(t *testing.T) {
	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	m.publish("1.1.0")

	installFake(h, m, "1.0.0")
	delete(h.vars, "TOTE_UPDATE_PUBKEY")

	a := *h.app

	n, err := a.refreshUpdateNotice(context.Background())
	if !errors.Is(err, update.ErrNoKey) || n.Available {
		t.Errorf("refresh = %+v, %v, want ErrNoKey", n, err)
	}

	if exists(filepath.Join(h.vars["TOTE_CACHE_DIR"], update.CacheFile)) {
		t.Error("the check was recorded in the cache")
	}
}
