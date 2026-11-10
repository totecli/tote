package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/totecli/tote/internal/ui"
)

// TestReviewInterruptedExitCode pins the exit status of an interface stopped by SIGINT to the
// value that shells and scripts expect (128 plus 2), instead of comparing it with the constant
// of the program, which would follow any change of that constant.
//
// Parameters:
//   - t: test to report to
func TestReviewInterruptedExitCode(t *testing.T) {
	h := newHarness(t)
	p := withUI(h)
	p.err = ui.ErrInterrupted

	if rc := h.run(); rc != 130 {
		t.Errorf("exit %d after SIGINT, want 130", rc)
	}
}

// TestReviewEmptyResultNeverTouchesTheHandoff checks that leaving the interface without anything
// to do succeeds and leaves the hand-off file alone, even when that file is one the wrapper
// would not own: pressing q must not fail or write because of the file.
//
// Parameters:
//   - t: test to report to
func TestReviewEmptyResultNeverTouchesTheHandoff(t *testing.T) {
	h := newHarness(t)
	p := withUI(h)
	p.res = ui.Result{}

	file := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(file, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_HANDOFF"] = file

	if rc := h.run(); rc != exitOK || h.errb.Len() != 0 {
		t.Errorf("exit %d, stderr %q", rc, h.errb.String())
	}

	if data, _ := os.ReadFile(file); string(data) != "precious" {
		t.Errorf("the file was changed: %q", data)
	}
}

// TestReviewOptionsFromTheEnvironment checks what the command gives the interface from the
// environment it was started in: the process environment for the programs it starts, and the
// user name from USER or, when that is missing, LOGNAME.
//
// Parameters:
//   - t: test to report to
func TestReviewOptionsFromTheEnvironment(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"user", map[string]string{"USER": "alice", "LOGNAME": "bob"}, "alice"},
		{"logname", map[string]string{"LOGNAME": "bob"}, "bob"},
		{"none", nil, ""},
	}

	for _, tt := range tests {
		h := newHarness(t)
		p := withUI(h)

		for k, v := range tt.vars {
			h.vars[k] = v
		}

		if rc := h.run(); rc != exitOK || len(p.calls) != 1 {
			t.Fatalf("%s: exit %d, %d starts", tt.name, rc, len(p.calls))
		}

		o := p.calls[0]

		if o.User != tt.want || len(o.Environ) != 1 || o.Environ[0] != "A=1" {
			t.Errorf("%s: user %q environ %v, want %q and the process environment", tt.name,
				o.User, o.Environ, tt.want)
		}
	}
}
