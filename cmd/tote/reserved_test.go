package main

import (
	"strings"
	"testing"
)

// TestConnectionReservedNames checks that "co add" refuses a name or an alias that tote keeps for
// its command line, today's sub-verbs and the planned ones, and a reserved prefix, and that the
// file is left alone.
//
// Parameters:
//   - t: test to report to
func TestConnectionReservedNames(t *testing.T) {
	const host = "h.example.org"

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"edit", []string{"co", "add", "edit", "--target", host}, `"edit" is a reserved name`},
		{"rm", []string{"co", "add", "rm", "--target", host}, `"rm" is a reserved name`},
		{"list", []string{"co", "add", "LIST", "--target", host}, `"LIST" is a reserved`},
		{
			"alias",
			[]string{"co", "add", "ok1", "--target", host, "--alias", "x1,tunnel"},
			`"tunnel" is a reserved name`,
		},
		{"prefix", []string{"co", "add", "_tote_x", "--target", host}, "is a reserved name"},
		{
			"double prefix",
			[]string{"co", "add", "__tote_pick", "--target", host},
			"is a reserved name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.fixtures()
			before := h.read("connections.toml")

			code := h.run(tt.args...)
			if code == 0 || !strings.Contains(h.errb.String(), tt.want) {
				t.Errorf("code %d, stderr %q, want %q", code, h.errb.String(), tt.want)
			}

			if h.read("connections.toml") != before {
				t.Error("the file changed")
			}
		})
	}

	h := newHarness(t)
	h.fixtures()

	if code := h.run("co", "add", "editor", "--target", host, "--alias", "finder"); code != 0 {
		t.Errorf("a name that only starts with a reserved word was refused: %q", h.errb.String())
	}
}
