package ui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/i18n"
)

// runnerFunc adapts a function to the Runner interface.
type runnerFunc func(RunSpec) RunResult

// Run implements Runner.
//
// Parameters:
//   - spec: program to run
func (f runnerFunc) Run(spec RunSpec) RunResult { return f(spec) }

// TestPauseNeeded checks which results keep the output on screen.
//
// Parameters:
//   - t: test to report to
func TestPauseNeeded(t *testing.T) {
	tests := []struct {
		name string
		r    RunResult
		want bool
	}{
		{"success", RunResult{}, false},
		{"exit 1", RunResult{ExitCode: 1}, false},
		{"exit 125", RunResult{ExitCode: 125}, false},
		{"cannot run", RunResult{ExitCode: 126}, true},
		{"not found", RunResult{ExitCode: 127}, true},
		{"ssh error", RunResult{ExitCode: 255}, true},
		{"signal", RunResult{ExitCode: 143, Signal: "SIGTERM"}, true},
		{"start error", RunResult{Err: errors.New("x")}, true},
	}

	for _, tt := range tests {
		if got := pauseNeeded(tt.r); got != tt.want {
			t.Errorf("%s: pauseNeeded = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestExecCommandRun checks the sequence written around a program: banner, program output, mode
// reset, then the pause line, and that the program receives the streams of the interface.
//
// Parameters:
//   - t: test to report to
func TestExecCommandRun(t *testing.T) {
	clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	tr := i18n.New("en")

	tests := []struct {
		name      string
		res       RunResult
		wantPause string
		wantRead  bool
	}{
		{"success", RunResult{}, "", false},
		{"exit 255", RunResult{ExitCode: 255}, "255", true},
		{"signal", RunResult{ExitCode: 130, Signal: "interrupt"}, "interrupt", true},
		{
			"cannot start",
			RunResult{ExitCode: 127, Err: errors.New("no such file")},
			"no such file", true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got RunSpec

			run := runnerFunc(func(spec RunSpec) RunResult {
				got = spec

				_, _ = io.WriteString(spec.Stdout, "PROGRAM\n")
				clk.advance(1500 * time.Millisecond)

				return tt.res
			})

			var out bytes.Buffer

			in := strings.NewReader("\nleft over")
			c := newExecCommand(run, RunSpec{Argv: []string{"ssh", "host"}}, "  -> ssh host",
				"ssh host", tr, clk.now, 0)
			c.SetStdin(in)
			c.SetStdout(&out)
			c.SetStderr(&out)

			if err := c.Run(); err != nil {
				t.Fatalf("Run returned %v", err)
			}

			text := out.String()
			banner, program, reset := strings.Index(text, "-> ssh host"),
				strings.Index(text, "PROGRAM"), strings.Index(text, modeReset)

			if banner != 2 || program < banner || reset < program {
				t.Errorf("order is wrong: banner %d, program %d, reset %d in %q",
					banner, program, reset, text)
			}

			if strings.Contains(text, "\x1b[?1049l") {
				t.Error("the program output leaves the alternate screen")
			}

			if tt.wantPause == "" && strings.Contains(text[reset:], "Enter") {
				t.Errorf("unexpected pause: %q", text[reset:])
			}

			if tt.wantPause != "" && !strings.Contains(text[reset:], tt.wantPause) {
				t.Errorf("pause line lacks %q: %q", tt.wantPause, text[reset:])
			}

			if tt.wantRead && tt.res.Err == nil && !strings.Contains(text[reset:], "1.5s") {
				t.Errorf("pause line lacks the duration: %q", text[reset:])
			}

			if tt.wantRead != (in.Len() < len("\nleft over")) {
				t.Errorf("input read = %v, want %v", in.Len() < len("\nleft over"), tt.wantRead)
			}

			if got.Stdin != io.Reader(in) || got.Argv[0] != "ssh" {
				t.Errorf("spec given to the runner = %+v", got)
			}
		})
	}
}

// TestExecCommandKeepsStreams checks that the streams of the spec win over those of the
// interface.
//
// Parameters:
//   - t: test to report to
func TestExecCommandKeepsStreams(t *testing.T) {
	var own, other bytes.Buffer

	run := runnerFunc(func(spec RunSpec) RunResult {
		_, _ = io.WriteString(spec.Stdout, "x")
		return RunResult{}
	})

	c := newExecCommand(run, RunSpec{Stdout: &own, Stdin: strings.NewReader("")}, "", "x",
		i18n.New("en"), time.Now, 0)
	c.SetStdin(strings.NewReader("other"))
	c.SetStdout(&other)
	c.SetStderr(&other)

	if err := c.Run(); err != nil {
		t.Fatal(err)
	}

	if other.Len() != 0 || !strings.HasPrefix(own.String(), "x") {
		t.Errorf("own %q, other %q", own.String(), other.String())
	}
}

// TestReadLine checks that only one line is consumed from a file and that a missing reader is
// harmless.
//
// Parameters:
//   - t: test to report to
func TestReadLine(t *testing.T) {
	readLine(nil, nil, nil)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = r.Close() }()

	if _, err := w.WriteString("abc\nrest"); err != nil {
		t.Fatal(err)
	}

	_ = w.Close()

	readLine(r, nil, nil)

	rest, _ := io.ReadAll(r)
	if string(rest) != "rest" {
		t.Errorf("left %q after the line, want %q", rest, "rest")
	}

	readLine(r, nil, nil)
	readLine(strings.NewReader("no newline"), nil, nil)
}

// TestDescribeExit checks the toast for each outcome.
//
// Parameters:
//   - t: test to report to
func TestDescribeExit(t *testing.T) {
	tr := i18n.New("en")

	tests := []struct {
		name string
		r    RunResult
		want string
	}{
		{"success", RunResult{}, ""},
		{"exit", RunResult{ExitCode: 3}, "3"},
		{"signal", RunResult{ExitCode: 143, Signal: "SIGTERM"}, "SIGTERM"},
		{"error", RunResult{ExitCode: 127, Err: errors.New("boom\x1b[2J")}, "boom?[2J"},
	}

	for _, tt := range tests {
		got := describeExit(tr, "ssh host", tt.r, false)

		if tt.want == "" {
			if got != "" {
				t.Errorf("%s: %q, want nothing", tt.name, got)
			}

			continue
		}

		if !strings.Contains(got, tt.want) || !strings.Contains(got, "ssh host") ||
			strings.Contains(got, "\x1b") {
			t.Errorf("%s: %q, want %q and the command", tt.name, got, tt.want)
		}
	}
}

// TestOSRunner starts real programs of any POSIX system.
//
// Parameters:
//   - t: test to report to
func TestOSRunner(t *testing.T) {
	tests := []struct {
		name    string
		argv    []string
		env     []string
		code    int
		signal  string
		wantErr bool
		out     string
	}{
		{name: "success", argv: []string{"sh", "-c", "exit 0"}},
		{name: "exit code", argv: []string{"sh", "-c", "exit 3"}, code: 3},
		{
			name: "environment", argv: []string{"sh", "-c", "echo $TOTE_X"},
			env: []string{"TOTE_X=hello"}, out: "hello\n",
		},
		{
			name: "signal", argv: []string{"sh", "-c", "kill -TERM $$"}, code: 128 + 15,
			signal: "SIGTERM",
		},
		{name: "missing", argv: []string{"tote-no-such-program-xyz"}, code: 127, wantErr: true},
		{name: "empty", argv: nil, code: 127, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer

			got := osRunner{}.Run(RunSpec{Argv: tt.argv, Env: tt.env, Stdout: &out})

			if got.ExitCode != tt.code || got.Signal != tt.signal ||
				(got.Err != nil) != tt.wantErr {
				t.Errorf("result = %+v, want code %d signal %q err %v",
					got, tt.code, tt.signal, tt.wantErr)
			}

			if out.String() != tt.out {
				t.Errorf("output = %q, want %q", out.String(), tt.out)
			}
		})
	}
}

// TestCloseOnExecAbove checks that marking descriptors neither fails nor closes them.
//
// Parameters:
//   - t: test to report to
func TestCloseOnExecAbove(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	closeOnExecAbove(2)

	if _, err := f.Stat(); err != nil {
		t.Errorf("descriptor unusable after the call: %v", err)
	}
}

// TestResetTerminal checks the sequences written after a crash.
//
// Parameters:
//   - t: test to report to
func TestResetTerminal(t *testing.T) {
	var out bytes.Buffer

	t.Setenv("PATH", t.TempDir())

	if err := ResetTerminal(&out); err != nil {
		t.Fatalf("ResetTerminal: %v", err)
	}

	for _, want := range []string{"\x1b[?1000l", "\x1b[?2004l", "\x1b[?25h", "\x1b[?1049l"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("reset lacks %q: %q", want, out.String())
		}
	}

	if err := ResetTerminal(failWriter{}); err == nil {
		t.Error("a failed write was not reported")
	}
}

// failWriter is a writer that always fails.
type failWriter struct{}

// Write implements io.Writer.
//
// Parameters:
//   - p: bytes to write
func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
