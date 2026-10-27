package store

import (
	"errors"
	"strings"
	"testing"
)

// TestReviewVerifyEditRefusesAWrongEdit pins the last safety net of PatchTable, which no test
// reached (verifyEdit was covered at 64 percent): whatever the line editor produced, the text is
// decoded again and compared with the tree that was intended. An edited text that does not parse,
// that changes another key, that drops a table or that leaves the old value must be refused with
// ErrLayout, and the right text must pass.
//
// Parameters:
//   - t: test to report to
func TestReviewVerifyEditRefusesAWrongEdit(t *testing.T) {
	body := []byte("[other]\nkeep = 1\n\n[plugins.gft]\nenabled = true\nname = \"x\"\n")

	tpath, err := parseTablePath("plugins.gft")
	if err != nil {
		t.Fatal(err)
	}

	ed, err := newTableEdit(tpath, map[string]any{"enabled": false}, nil)
	if err != nil {
		t.Fatal(err)
	}

	good, err := patchBytes(body, tpath, ed)
	if err != nil {
		t.Fatal(err)
	}

	if err := verifyEdit(body, good, tpath, ed, false); err != nil {
		t.Fatalf("the intended text was refused: %v", err)
	}

	tests := []struct {
		name string
		out  string
	}{
		{"old value kept", string(body)},
		{"other key changed", strings.Replace(string(good), "keep = 1", "keep = 2", 1)},
		{"other table lost", strings.Replace(string(good), "[other]\nkeep = 1\n", "", 1)},
		{"sibling key lost", strings.Replace(string(good), "name = \"x\"\n", "", 1)},
		{"does not parse", string(good) + "[broken\n"},
		{"value added elsewhere", string(good) + "\n[extra]\nx = 1\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyEdit(body, []byte(tt.out), tpath, ed, false)
			if !errors.Is(err, ErrLayout) {
				t.Errorf("verifyEdit = %v, want ErrLayout", err)
			}
		})
	}
}
