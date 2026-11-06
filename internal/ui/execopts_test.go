package ui

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/shell"
)

// TestOSRunnerDir checks the working directory of a real program: the one of tote when the spec
// has none, the directory asked for, and a failure to start, not a panic, for a directory that is
// missing or is not one.
//
// Parameters:
//   - t: test to report to
func TestOSRunnerDir(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(dir, "file")
	if err = os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		dir     string
		want    string
		wantErr bool
	}{
		{name: "empty keeps the directory of tote", dir: "", want: here},
		{name: "directory", dir: dir, want: dir},
		{name: "missing", dir: filepath.Join(dir, "absent"), wantErr: true},
		{name: "not a directory", dir: file, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer

			got := osRunner{}.Run(RunSpec{Argv: []string{"pwd", "-P"}, Dir: tt.dir, Stdout: &out})

			if (got.Err != nil) != tt.wantErr {
				t.Fatalf("result = %+v, want error %v", got, tt.wantErr)
			}

			if tt.wantErr {
				if got.ExitCode != 127 || out.Len() != 0 {
					t.Errorf("result = %+v, output %q, want 127 and no output", got, out.String())
				}

				if shell.IsMissingProgram(got.Err) || errors.Is(got.Err, fs.ErrNotExist) {
					t.Errorf("error %v is taken for a program that is not found", got.Err)
				}

				return
			}

			if got.ExitCode != 0 || strings.TrimSpace(out.String()) != tt.want {
				t.Errorf("result = %+v, output %q, want %q", got, out.String(), tt.want)
			}
		})
	}
}

// TestPausePolicy checks, for each policy, which results make the interface wait for Enter. The
// zero value must give the rule that every program started by tote had before the policy existed.
//
// Parameters:
//   - t: test to report to
func TestPausePolicy(t *testing.T) {
	results := []struct {
		name string
		r    RunResult
	}{
		{"success", RunResult{}},
		{"exit 1", RunResult{ExitCode: 1}},
		{"exit 126", RunResult{ExitCode: 126}},
		{"exit 255", RunResult{ExitCode: 255}},
		{"signal", RunResult{ExitCode: 143, Signal: "SIGTERM"}},
		{"cannot start", RunResult{ExitCode: 127, Err: errors.New("no such file")}},
	}

	tests := []struct {
		name   string
		policy PausePolicy
		want   []bool
	}{
		{"zero value", 0, []bool{false, false, true, true, true, true}},
		{"error", PauseError, []bool{false, false, true, true, true, true}},
		{"always", PauseAlways, []bool{true, true, true, true, true, true}},
		{"never", PauseNever, []bool{false, false, false, false, false, false}},
	}

	for _, tt := range tests {
		for i, res := range results {
			if got := tt.policy.pauses(res.r); got != tt.want[i] {
				t.Errorf("%s, %s: pauses = %v, want %v", tt.name, res.name, got, tt.want[i])
			}
		}
	}

	if PausePolicy(0) != PauseError {
		t.Errorf("the zero policy is %d, want PauseError", PausePolicy(0))
	}
}

// TestParsePause checks the names of the pause policies.
//
// Parameters:
//   - t: test to report to
func TestParsePause(t *testing.T) {
	tests := []struct {
		name   string
		want   PausePolicy
		wantOK bool
	}{
		{"error", PauseError, true},
		{"always", PauseAlways, true},
		{"never", PauseNever, true},
		{"", PauseError, false},
		{"Always", PauseError, false},
		{"sometimes", PauseError, false},
	}

	for _, tt := range tests {
		if got, ok := ParsePause(tt.name); got != tt.want || ok != tt.wantOK {
			t.Errorf("ParsePause(%q) = %d, %v, want %d, %v", tt.name, got, ok, tt.want, tt.wantOK)
		}
	}
}

// TestExecCommandPausePolicy checks, through the whole command, that each policy decides whether
// the pause line is written and a line is read, that the directory of the spec reaches the runner,
// and that the zero policy behaves as before for the results that decided it.
//
// Parameters:
//   - t: test to report to
func TestExecCommandPausePolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy PausePolicy
		res    RunResult
		want   bool
	}{
		{"zero, success", 0, RunResult{}, false},
		{"zero, exit 1", 0, RunResult{ExitCode: 1}, false},
		{"zero, exit 255", 0, RunResult{ExitCode: 255}, true},
		{"error, exit 127", PauseError, RunResult{ExitCode: 127, Err: errors.New("x")}, true},
		{"always, success", PauseAlways, RunResult{}, true},
		{"always, exit 1", PauseAlways, RunResult{ExitCode: 1}, true},
		{"always, failure", PauseAlways, RunResult{ExitCode: 255}, true},
		{"never, success", PauseNever, RunResult{}, false},
		{"never, exit 255", PauseNever, RunResult{ExitCode: 255}, false},
		{"never, cannot start", PauseNever, RunResult{ExitCode: 127, Err: errors.New("x")}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got RunSpec

			run := runnerFunc(func(spec RunSpec) RunResult {
				got = spec

				return tt.res
			})

			var out bytes.Buffer

			const typed = "\nleft over"

			in := strings.NewReader(typed)
			spec := RunSpec{Argv: []string{"tool"}, Dir: "/srv/demo", Pause: tt.policy}
			c := newExecCommand(run, spec, "", "tool", newPrinter("en"), (&clock{}).now, 0)
			c.SetStdin(in)
			c.SetStdout(&out)
			c.SetStderr(&out)

			if err := c.Run(); err != nil {
				t.Fatalf("Run returned %v", err)
			}

			reset := strings.Index(out.String(), modeReset)
			if reset < 0 {
				t.Fatalf("no mode reset in %q", out.String())
			}

			after := out.String()[reset+len(modeReset):]

			if paused := strings.Contains(after, "Enter"); paused != tt.want {
				t.Errorf("pause line = %v, want %v (%q)", paused, tt.want, after)
			}

			if read := in.Len() < len(typed); read != tt.want {
				t.Errorf("a line was read = %v, want %v", read, tt.want)
			}

			if got.Dir != "/srv/demo" || got.Pause != tt.policy {
				t.Errorf("spec given to the runner = %+v", got)
			}
		})
	}
}

// TestExecCommandDirFailureVia checks that a working directory that cannot be entered is not
// explained as a via program that is not found, while a real missing program still is.
//
// Parameters:
//   - t: test to report to
func TestExecCommandDirFailureVia(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")

	tests := []struct {
		name    string
		spec    RunSpec
		wantVia bool
	}{
		{"missing directory", RunSpec{Argv: []string{"pwd"}, Dir: missing}, false},
		{"missing program", RunSpec{Argv: []string{"tote-no-such-program"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer

			c := newExecCommand(osRunner{}, tt.spec, "", "x", newPrinter("en"), (&clock{}).now, 0)
			c.via = true
			c.SetStdin(strings.NewReader("\n"))
			c.SetStdout(&out)
			c.SetStderr(&out)

			if err := c.Run(); err != nil {
				t.Fatalf("Run returned %v", err)
			}

			hint := newPrinter("en").T("cli_via_hint")
			if got := strings.Contains(out.String(), hint); got != tt.wantVia {
				t.Errorf("via hint shown = %v, want %v in %q", got, tt.wantVia, out.String())
			}
		})
	}
}

// TestExecCommandWithoutFlush checks that a spec that does not ask for the flush leaves the input
// alone: a reader that is not a file is never touched, and the program still runs once.
//
// Parameters:
//   - t: test to report to
func TestExecCommandWithoutFlush(t *testing.T) {
	for _, flush := range []bool{false, true} {
		runs := 0
		run := runnerFunc(func(RunSpec) RunResult {
			runs++

			return RunResult{}
		})

		in := strings.NewReader("typed\n")
		c := newExecCommand(run, RunSpec{Argv: []string{"tool"}, Flush: flush}, "", "tool",
			newPrinter("en"), (&clock{}).now, time.Duration(0))
		c.SetStdin(in)
		c.SetStdout(io.Discard)
		c.SetStderr(io.Discard)

		if err := c.Run(); err != nil || runs != 1 || in.Len() != len("typed\n") {
			t.Errorf("flush %v: error %v, runs %d, input left %d", flush, err, runs, in.Len())
		}
	}
}
