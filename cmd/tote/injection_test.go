package main

import "testing"

// TestReviewListOutputHasNoTerminalActiveRune checks that no list format lets a hand-edited
// description reach the terminal with a C1 control or a bidirectional override in it.
//
// Parameters:
//   - t: test to report to
func TestReviewListOutputHasNoTerminalActiveRune(t *testing.T) {
	const hostile = `schema = 1

[[alias]]
name = "ok"
cmd = "ls"
desc = "a\u009b31mb\u202ec\u007fd\u0085e\U000E0041f"
`

	for _, format := range []string{"table", "tsv", "json"} {
		h := newHarness(t)
		h.write("aliases.toml", hostile)

		if code := h.run("alias", "list", "--format", format); code != 0 {
			t.Fatalf("%s: code = %d, stderr = %q", format, code, h.errb.String())
		}

		for _, r := range h.out.String() {
			if r != '\n' && r != '\t' && unsafeRune(r) {
				t.Errorf("%s output carries U+%04X: %q", format, r, h.out.String())

				break
			}
		}
	}
}
