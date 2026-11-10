package e2e

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// sgrSequence matches a select graphic rendition sequence of a styled capture.
var sgrSequence = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

// isSeparator reports whether a rune separates the parameters of a style sequence.
//
// Parameters:
//   - r: rune to test
func isSeparator(r rune) bool {
	return r == ';' || r == ':'
}

// colorCodes returns the color parameters of the styles in a capture: the 30 to 37, 40 to 47, 90
// to 97 and 100 to 107 codes and the 38 and 48 codes that start an extended color. The default
// colors 39 and 49 and the text attributes are not colors.
//
// Parameters:
//   - styled: capture of the pane with its escape sequences
func colorCodes(styled string) []string {
	var out []string

	for _, m := range sgrSequence.FindAllStringSubmatch(styled, -1) {
		for _, p := range strings.FieldsFunc(m[1], isSeparator) {
			n, err := strconv.Atoi(p)
			if err != nil {
				continue
			}

			if (n >= 30 && n <= 38) || (n >= 40 && n <= 48) || (n >= 90 && n <= 97) ||
				(n >= 100 && n <= 107) {
				out = append(out, p)
			}
		}
	}

	return out
}

// TestReviewColorValuesOnTheRealBinary runs the real program in a terminal that could show true
// color and checks what the owner saw by hand: TOTE_COLOR=never, and the other words for "no
// color", really remove every color, and a value that names a color depth still gets colors. The
// unit tests check the parser and the choice of the profile; only this test sees the escape
// sequences that reach the terminal.
//
// Parameters:
//   - t: test to report to
func TestReviewColorValuesOnTheRealBinary(t *testing.T) {
	tests := []struct {
		value string
		color bool
	}{
		{"truecolor", true},
		{"256", true},
		{"never", false},
		{"off", false},
		{"no", false},
		{"false", false},
		{"0", false},
		{"none", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)

			// The theme is fixed: tmux never answers the question about the background, and the
			// automatic theme then stays neutral, with no color whatever TOTE_COLOR says.
			sb.write(t, filepath.Join(sb.Dir, "config", "config.toml"),
				"schema = 1\n[ui]\ntheme = \"dark\"\n")

			tm := newTerm(t, sb, startOptions{Env: []string{
				"TOTE_COLOR=" + tt.value,
				"COLORTERM=truecolor",
			}}, "tote", "co")

			tm.waitText(cmdOf("alpha"))

			codes := colorCodes(tm.styled())

			if tt.color && len(codes) == 0 {
				t.Errorf("TOTE_COLOR=%s drew no color:\n%q", tt.value, tm.styled())
			}

			if !tt.color && len(codes) != 0 {
				t.Errorf("TOTE_COLOR=%s drew colors %v:\n%q", tt.value, codes, tm.styled())
			}

			tm.quit()
		})
	}
}

// TestReviewUnknownColorWarnsOnTheRealBinary checks that a value of TOTE_COLOR that means nothing
// is said on the terminal, once the interface has gone: the line the program printed on its
// error output before it took the screen is still there, and names the value.
//
// Parameters:
//   - t: test to report to
func TestReviewUnknownColorWarnsOnTheRealBinary(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{Env: []string{"TOTE_COLOR=rainbow"}}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.quit()

	want := fmt.Sprintf("tote: unknown TOTE_COLOR value %q, using automatic detection", "rainbow")
	if text := tm.look().Text; !strings.Contains(text, want) {
		t.Errorf("the terminal does not show %q after the interface:\n%s", want, text)
	}
}
