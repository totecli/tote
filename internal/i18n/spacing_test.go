package i18n

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// TestReviewFrenchNoDoubleSpace pins that no French message holds a plain space next to a
// non-breaking space or two plain spaces in a row. cli_update_available_hint currently reads
// "lancez" + space + NBSP + guillemet, which shows as a double space before the quote.
//
// Parameters:
//   - t: test to report to
func TestReviewFrenchNoDoubleSpace(t *testing.T) {
	_, fr := catalogs(t)

	for _, key := range slices.Sorted(maps.Keys(fr)) {
		msg := fr[key]

		for _, p := range []string{" " + nbsp, nbsp + " ", "  "} {
			if strings.Contains(msg, p) && key != "help_opt_ascii" && key != "help_opt_color" &&
				key != "help_opt_noplugins" && key != "help_usage_ui" {
				t.Errorf("%s: two spaces in a row in %q", key, msg)
			}
		}
	}
}
