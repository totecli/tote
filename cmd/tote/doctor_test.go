package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/update"
)

// resetGolden is the exact text that "tote doctor --reset" writes: the end of mouse, focus and
// paste reporting, of application keys and of the keyboard protocols, the cursor and the text
// attributes, then the exit of the alternate screen, the clearing of the screen and the home
// position.
const resetGolden = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1005l\x1b[?1006l\x1b[?1015l" +
	"\x1b[?1004l\x1b[?2004l\x1b[?1l\x1b>\x1b[?25h\x1b[0m\x1b[<u\x1b[>4m" +
	"\x1b[?1049l\x1b[2J\x1b[H"

// doctorRig is an app wired to a fixture, with recorders for what touches the terminal.
//
// ttyOut, ttyErr and ttyIn are the answers of the terminal checks and stty counts the calls to
// the fake "stty sane".
type doctorRig struct {
	f      *fixture
	app    *app
	out    *bytes.Buffer
	errb   *bytes.Buffer
	ttyOut bool
	ttyErr bool
	ttyIn  bool
	stty   int
}

// newRig builds an app that reads the fixture and nothing else of the machine.
//
// Parameters:
//   - t: running test
//   - f: installation to look at
func newRig(t *testing.T, f *fixture) *doctorRig {
	t.Helper()

	f.vars["TOTE_LANG"] = "en"
	f.vars["COLORTERM"] = "truecolor"

	r := &doctorRig{f: f, out: &bytes.Buffer{}, errb: &bytes.Buffer{}}
	r.app = &app{
		Stdin:  strings.NewReader(""),
		Stdout: r.out,
		Stderr: r.errb,
		Getenv: func(k string) string { return f.vars[k] },
		Environ: func() []string {
			var env []string

			for k, v := range f.vars {
				env = append(env, k+"="+v)
			}

			slices.Sort(env)

			return env
		},
		Home:       f.home,
		Version:    "1.2.3",
		Executable: func() (string, error) { return f.tools["tote"], nil },
		Geteuid:    os.Geteuid,
		Now:        func() time.Time { return f.env.Now },
		LookPath:   f.lookPath,
		TermSize:   func() (int, int, bool) { return 120, 40, true },
		TmuxFeatures: func() string {
			return ""
		},
		TmuxClipboard: func() string {
			return ""
		},
		Stty: func(io.Reader) error { r.stty++; return nil },
		Updater: update.Updater{
			OS: "linux", Arch: "amd64", Keys: []update.PublicKey{{}},
		},
	}
	r.app.IsTerminal = func(w io.Writer) bool {
		if _, ok := w.(*errWriter); ok || w == io.Writer(r.out) {
			return r.ttyOut
		}

		return r.ttyErr
	}
	r.app.IsInputTerminal = func(io.Reader) bool { return r.ttyIn }

	return r
}

// run executes a command line on the app, with empty output buffers.
//
// Parameters:
//   - args: command line without the program name
func (r *doctorRig) run(args ...string) int {
	r.out.Reset()
	r.errb.Reset()

	a := *r.app

	return a.run(args)
}

// golden compares a text with a reference file of testdata; UPDATE_GOLDEN=1 rewrites the file.
//
// Parameters:
//   - t: running test
//   - name: file name in testdata
//   - got: text to compare
func golden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if got != string(want) {
		t.Errorf("%s differs from the reference file, UPDATE_GOLDEN=1 rewrites it:\n%s", name, got)
	}
}

// brokenFixture is an installation with one failure, three warnings and the rest passing: a
// data file that does not parse, a directory open to others, a missing wrapper and a missing
// cache directory.
//
// Parameters:
//   - t: running test
func brokenFixture(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.conf("aliases.toml", "schema = 1\n\n[[alias]]\nname = \"ll\"\ncmd =\n")
	f.mkdir(".config/tote", 0o755)
	f.mkdir("code/proj", 0o700)
	f.mkdir("code/docs", 0o700)
	f.conf("repos.toml", "schema = 1\n\n[[repo]]\nname = \"proj\"\npath = \"~/code/proj\"\n"+
		"desc = \"Main project\"\n\n[[repo]]\nname = \"docs\"\npath = \"~/code/docs\"\n")
	f.vars["TOTE_SHELL"] = ""
	f.write("run/tote.abcd1234", "", 0o600)
	f.age("run/tote.abcd1234", 3*time.Hour)

	if err := os.Remove(f.path(".cache/tote")); err != nil {
		t.Fatal(err)
	}

	return f
}

// TestDoctorJSONGolden checks the shape of the JSON report against a reference file.
//
// Parameters:
//   - t: test to report to
func TestDoctorJSONGolden(t *testing.T) {
	r := newRig(t, brokenFixture(t))
	r.ttyOut = true

	if code := r.run("doctor", "--json"); code != 1 {
		t.Fatalf("code = %d, want 1; stderr %q", code, r.errb.String())
	}

	if r.errb.Len() != 0 {
		t.Errorf("stderr = %q", r.errb.String())
	}

	golden(t, "doctor.json", r.out.String())

	var rep doctorReport
	if err := json.Unmarshal(r.out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}

	if rep.OK || rep.Version != "1.2.3" || rep.Summary.Fail != 1 || rep.Summary.Warn != 3 ||
		len(rep.Checks) != 28 {
		t.Errorf("report = %+v", rep.Summary)
	}

	for _, c := range rep.Checks {
		if c.ID == "" || c.Message == "" || !slices.Contains([]string{"ok", "warn", "fail"}, c.Status) {
			t.Errorf("check %+v", c)
		}
	}
}

// TestDoctorTextGolden checks the text report in both languages against reference files.
//
// Parameters:
//   - t: test to report to
func TestDoctorTextGolden(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		r := newRig(t, brokenFixture(t))
		r.f.vars["TOTE_LANG"] = lang
		r.ttyOut = true

		if code := r.run("doctor"); code != 1 {
			t.Fatalf("%s: code = %d, want 1; stderr %q", lang, code, r.errb.String())
		}

		if r.errb.Len() != 0 {
			t.Errorf("%s: stderr = %q", lang, r.errb.String())
		}

		golden(t, "doctor_"+lang+".txt", r.out.String())
	}
}

// TestDoctorExitCodes checks that a failure gives 1 and a warning does not, in both formats.
//
// Parameters:
//   - t: test to report to
func TestDoctorExitCodes(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fixture)
		code  int
	}{
		{"healthy", func(*fixture) {}, 0},
		{"warning", func(f *fixture) { f.vars["TOTE_SHELL"] = "" }, 0},
		{"failure", func(f *fixture) { f.conf("aliases.toml", "schema = 1\n[[alias]\n") }, 1},
		{"failure and warning", func(f *fixture) {
			f.conf("aliases.toml", "schema = 1\n[[alias]\n")
			f.vars["TOTE_SHELL"] = ""
		}, 1},
	}

	for _, tt := range tests {
		for _, args := range [][]string{{"doctor"}, {"doctor", "--json"}} {
			f := newFixture(t)
			tt.setup(f)

			r := newRig(t, f)
			if code := r.run(args...); code != tt.code {
				t.Errorf("%s %v: code = %d, want %d", tt.name, args, code, tt.code)
			}
		}
	}
}

// TestDoctorUsage checks the misuse of the verb.
//
// Parameters:
//   - t: test to report to
func TestDoctorUsage(t *testing.T) {
	tests := [][]string{
		{"doctor", "extra"},
		{"doctor", "--bogus"},
		{"doctor", "--json", "--reset"},
		{"doctor", "--reset", "--json"},
		{"doctor", "--json=1"},
	}

	for _, args := range tests {
		r := newRig(t, newFixture(t))
		r.ttyOut = true

		if code := r.run(args...); code != exitUsage {
			t.Errorf("%v: code = %d, want 2", args, code)
		}

		if r.out.Len() != 0 {
			t.Errorf("%v: stdout %q", args, r.out.String())
		}

		if !strings.Contains(r.errb.String(), "tote help doctor") {
			t.Errorf("%v: stderr %q", args, r.errb.String())
		}
	}

	r := newRig(t, newFixture(t))

	if code := r.run("doctor", "--help"); code != 0 || !strings.Contains(r.out.String(), "--reset") {
		t.Errorf("doctor --help: code %d, stdout %q", code, r.out.String())
	}
}

// TestDoctorReset checks the sequences written by --reset, byte for byte, and where they go.
//
// Parameters:
//   - t: test to report to
func TestDoctorReset(t *testing.T) {
	tests := []struct {
		name           string
		out, err, in   bool
		code           int
		stdout, stderr string
		stty           int
	}{
		{"terminal", true, true, true, 0, resetGolden, "tote: terminal reset\n", 1},
		{"no input terminal", true, true, false, 0, resetGolden, "tote: terminal reset\n", 0},
		{"output redirected", false, true, true, 0, "", resetGolden + "tote: terminal reset\n", 1},
		{
			"no terminal", false, false, true, 1, "",
			"tote: error: no terminal to reset: the output is not a terminal\n", 0,
		},
	}

	for _, tt := range tests {
		r := newRig(t, newFixture(t))
		r.ttyOut, r.ttyErr, r.ttyIn = tt.out, tt.err, tt.in

		if code := r.run("doctor", "--reset"); code != tt.code {
			t.Errorf("%s: code = %d, want %d", tt.name, code, tt.code)
		}

		if r.out.String() != tt.stdout || r.errb.String() != tt.stderr || r.stty != tt.stty {
			t.Errorf("%s: stdout %q, stderr %q, stty %d", tt.name, r.out.String(), r.errb.String(),
				r.stty)
		}
	}
}

// TestDoctorResetFrench checks the confirmation of --reset in French.
//
// Parameters:
//   - t: test to report to
func TestDoctorResetFrench(t *testing.T) {
	r := newRig(t, newFixture(t))
	r.f.vars["TOTE_LANG"] = "fr"
	r.ttyOut = true

	want := "tote: terminal r\u00e9par\u00e9\n"

	if code := r.run("doctor", "--reset"); code != 0 || r.errb.String() != want {
		t.Errorf("code %d, stderr %q", code, r.errb.String())
	}
}

// failingWriter is a terminal that cannot be written to.
type failingWriter struct{}

// Write implements io.Writer.
//
// Parameters:
//   - p: bytes to write
func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// TestDoctorResetWriteError checks that a terminal that cannot be written to is a failure.
//
// Parameters:
//   - t: test to report to
func TestDoctorResetWriteError(t *testing.T) {
	r := newRig(t, newFixture(t))
	r.app.Stdout = failingWriter{}
	r.app.IsTerminal = func(io.Writer) bool { return true }

	a := *r.app
	if code := a.run([]string{"doctor", "--reset"}); code != exitFail {
		t.Errorf("code = %d, want 1", code)
	}

	if r.stty != 0 {
		t.Error("stty ran after a failed write")
	}
}

// TestDoctorResetBrokenConfig checks that --reset works when every file of tote is broken,
// since it is what a person runs after a crash.
//
// Parameters:
//   - t: test to report to
func TestDoctorResetBrokenConfig(t *testing.T) {
	f := brokenFixture(t)
	f.conf("config.toml", "lang = [\n")
	f.conf("connections.toml", "\x00\x01 not toml")
	f.vars["TOTE_CONFIG_DIR"] = "relative/dir"

	r := newRig(t, f)
	r.ttyOut = true

	if code := r.run("doctor", "--reset"); code != 0 || r.out.String() != resetGolden {
		t.Errorf("code %d, stdout %q, stderr %q", code, r.out.String(), r.errb.String())
	}
}

// snapshot lists every entry below a directory with its mode, size, time and content hash.
//
// Parameters:
//   - t: running test
//   - root: directory to walk
func snapshot(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := os.Lstat(p)
		if err != nil {
			return err
		}

		line := p + " " + info.Mode().String() + " " + info.ModTime().String()
		if info.Mode().IsRegular() {
			data, _ := os.ReadFile(p)
			sum := sha256.Sum256(data)
			line += " " + string(sum[:8])
		}

		out = append(out, line)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// TestDoctorWritesNothing checks that the report and the reset leave the files of the user as
// they were, even on a broken installation.
//
// Parameters:
//   - t: test to report to
func TestDoctorWritesNothing(t *testing.T) {
	for _, args := range [][]string{{"doctor"}, {"doctor", "--json"}, {"doctor", "--reset"}} {
		f := brokenFixture(t)
		f.conf("config.toml", "[ui]\ntheem = 1\n")
		f.write(".bashrc", "eval \"$(tote init bash)\"\n", 0o644)
		f.write(".local/state/tote/install.json", "{", 0o600)

		before := snapshot(t, filepath.Dir(f.home))
		r := newRig(t, f)
		r.ttyOut = true
		r.run(args...)

		after := snapshot(t, filepath.Dir(f.home))
		if !slices.Equal(before, after) {
			t.Errorf("%v changed the files:\nbefore %q\nafter  %q", args, before, after)
		}
	}
}

// TestDoctorListed checks that the verb is in the help, the completion and the dispatch.
//
// Parameters:
//   - t: test to report to
func TestDoctorListed(t *testing.T) {
	r := newRig(t, newFixture(t))

	if r.run("help"); !regexp.MustCompile(`(?m)^\s+doctor\s+\S`).MatchString(r.out.String()) {
		t.Errorf("help lacks doctor:\n%s", r.out.String())
	}

	if r.run("help", "doctor"); !strings.Contains(r.out.String(), "tote doctor [--json]") ||
		!strings.Contains(r.out.String(), "--reset") || !strings.Contains(r.out.String(), "exit code") {
		t.Errorf("help doctor:\n%s", r.out.String())
	}

	if r.run("__complete", "doc"); !strings.HasPrefix(r.out.String(), "doctor\t") {
		t.Errorf("verb completion: %q", r.out.String())
	}

	if r.run("__complete", "doctor", "--"); r.out.String() != "--json\n--reset\n" {
		t.Errorf("flag completion: %q", r.out.String())
	}

	if r.run("docter"); !strings.Contains(r.errb.String(), "doctor") {
		t.Errorf("no suggestion for a typo: %q", r.errb.String())
	}
}

// TestDoctorRealSeams runs the verb on an app whose seams are all unset, which is how the
// production code falls back on the real machine; only the shape of the answer is checked.
//
// Parameters:
//   - t: test to report to
func TestDoctorRealSeams(t *testing.T) {
	f := newFixture(t)
	r := newRig(t, f)

	a := app{
		Stdout: r.out, Stderr: r.errb, Getenv: r.app.Getenv, Home: f.home, Version: "dev",
		IsTerminal:      func(io.Writer) bool { return false },
		IsInputTerminal: func(io.Reader) bool { return false },
	}

	code := a.run([]string{"doctor", "--json"})
	if code != 0 && code != 1 {
		t.Fatalf("code = %d, stderr %q", code, r.errb.String())
	}

	var rep doctorReport
	if err := json.Unmarshal(r.out.Bytes(), &rep); err != nil || len(rep.Checks) != 28 {
		t.Fatalf("report %v, %d checks", err, len(rep.Checks))
	}

	if a.now().IsZero() {
		t.Error("no clock")
	}

	if _, err := a.lookPath("tote-no-such-program"); err == nil {
		t.Error("lookPath found a program that does not exist")
	}

	w, h, ok := a.terminalSize()
	if ok && (w <= 0 || h <= 0) {
		t.Errorf("terminal size %dx%d", w, h)
	}

	if err := sttySane(strings.NewReader("")); err != nil {
		t.Errorf("sttySane on a reader: %v", err)
	}

	if new := newApp(io.Discard, io.Discard); new.Now == nil || new.LookPath == nil ||
		new.TermSize == nil || new.Stty == nil {
		t.Error("newApp left a doctor seam unset")
	}
}

// TestDoctorKeysUsed checks that every doctor message of the catalogs is used by the code.
//
// Parameters:
//   - t: test to report to
func TestDoctorKeysUsed(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	var src strings.Builder

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}

		src.Write(data)
	}

	catalog, err := os.ReadFile(filepath.Join("..", "..", "internal", "i18n", "en.toml"))
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`(?m)^((?:cli_doctor|usage_doctor|help_cmd_doctor)[a-z0-9_]*) =`)

	for _, m := range re.FindAllStringSubmatch(string(catalog), -1) {
		if !strings.Contains(src.String(), `"`+m[1]+`"`) {
			t.Errorf("the key %s is in the catalog and not in the code", m[1])
		}
	}
}
