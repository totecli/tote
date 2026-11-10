package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// viaReportRig returns the rig of an installation with nine connections: two with a program that
// exists, a template and a verb, and seven whose via is a name that no file of the PATH has, such
// as a shell alias.
//
// Parameters:
//   - t: running test
//   - lang: language of the report
func viaReportRig(t *testing.T, lang string) *doctorRig {
	t.Helper()

	f := newFixture(t)
	viaSetup(viaConns("web", "ssh -t admin@jump.example.org {user@}{host}", "db", "jmp", "ci", "mosh",
		"a1", "a1x", "a2", "a2x", "a3", "a3x", "a4", "a4x", "a5", "a5x", "a6", "a6x"),
		"ssh", "mosh")(f)

	r := newRig(t, f)
	r.f.vars["TOTE_LANG"] = lang
	r.ttyOut = true

	return r
}

// TestDoctorViaGolden checks the warning of the via note, its hint and its list that stops at five
// connections, in both languages and in JSON, against reference files.
//
// Parameters:
//   - t: test to report to
func TestDoctorViaGolden(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		r := viaReportRig(t, lang)

		if code := r.run("doctor"); code != 0 {
			t.Fatalf("%s: code = %d, want 0; stderr %q", lang, code, r.errb.String())
		}

		golden(t, "doctor_via_"+lang+".txt", noteLines(r.out.String(), "via"))
	}

	r := viaReportRig(t, "en")
	r.ttyOut = false

	if code := r.run("doctor", "--json"); code != 0 {
		t.Fatalf("json: code = %d, want 0; stderr %q", code, r.errb.String())
	}

	var rep doctorReport
	if err := json.Unmarshal(r.out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}

	for _, c := range rep.Checks {
		if c.ID != "via" {
			continue
		}

		out, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			t.Fatal(err)
		}

		golden(t, "doctor_via.json", string(out)+"\n")
	}
}

// TestConnectViaNotFound checks the message of "tote co" when the program of the via cannot be
// started: it keeps the real cause and the command that was shown, says that the program is not
// found and that a shell alias cannot be a via, and gives an example, in both languages. A plain
// ssh that is missing, and an error of another kind, keep the old message.
//
// Parameters:
//   - t: test to report to
func TestConnectViaNotFound(t *testing.T) {
	notFound := &exec.Error{Name: "jmp", Err: exec.ErrNotFound}
	pathErr := &fs.PathError{Op: "fork/exec", Path: "bin/jmp", Err: syscall.ENOENT}
	denied := &fs.PathError{Op: "fork/exec", Path: "bin/jmp", Err: syscall.EACCES}

	tests := []struct {
		name string
		lang string
		via  string
		err  error
		want []string
		hint bool
	}{
		{
			"alias", "en", "jmp", notFound,
			[]string{
				`cannot run "jmp"`, `exec: "jmp": executable file not found in $PATH`,
				"is not found in PATH", "a shell alias or function cannot be used as a via",
				"ssh -t admin@jump.example.org {user@}{host}", "  -> jmp h",
			},
			true,
		},
		{
			"alias in French", "fr", "jmp", notFound,
			[]string{
				`impossible de lancer "jmp"`, "introuvable dans le PATH",
				"un alias ou une fonction du shell ne peut pas servir de via",
				"ssh -t admin@jump.example.org {user@}{host}",
			},
			true,
		},
		{
			"template", "en", "jmp -t {host}", notFound,
			[]string{"is not found in PATH", "ssh -t admin@jump.example.org {user@}{host}"},
			true,
		},
		{"missing path", "en", "bin/jmp", pathErr, []string{
			"no such file or directory",
			"is not found in PATH",
		}, true},
		{"not executable", "en", "bin/jmp", denied, []string{
			"permission denied",
			"is not found in PATH",
		}, true},
		{
			"plain ssh", "en", "", &exec.Error{Name: "ssh", Err: exec.ErrNotFound},
			[]string{`cannot run "ssh"`},
			false,
		},
		{
			"explicit ssh", "en", "ssh", &exec.Error{Name: "ssh", Err: exec.ErrNotFound},
			[]string{`cannot run "ssh"`},
			false,
		},
		{
			"another error", "en", "jmp", errors.New("exec format error"),
			[]string{`cannot run "jmp": exec format error`},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.vars["TOTE_LANG"] = tt.lang

			via := ""
			if tt.via != "" {
				via = "via = " + strconv.Quote(tt.via) + "\n"
			}

			h.write("connections.toml", "schema = 1\n\n[[connection]]\nname = \"h\"\n"+via)
			h.execErr = tt.err

			if code := h.run("co", "h"); code != 127 {
				t.Fatalf("code = %d, want 127; stderr %q", code, h.errb.String())
			}

			got := h.errb.String()

			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("stderr lacks %q:\n%s", w, got)
				}
			}

			if has := strings.Contains(got, "ssh -t admin@jump.example.org {user@}{host}"); has != tt.hint {
				t.Errorf("hint shown %v, want %v:\n%s", has, tt.hint, got)
			}
		})
	}
}
