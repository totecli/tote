package ui

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestReviewI18nReasonsFitTheForm checks that no translated validation reason is cut by the two
// line budget of a form field at the width the form has on a 100 column terminal (54 cells).
// The French reason for a connection name is 107 cells long, so its last words are replaced by
// an ellipsis and the user never reads the rule that lists the allowed characters.
//
// Parameters:
//   - t: test to report to
func TestReviewI18nReasonsFitTheForm(t *testing.T) {
	const width = 54

	raw, err := os.ReadFile("../i18n/en.toml")
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}

	re := regexp.MustCompile(`(?m)^(ui_invalid_[a-z_0-9]+)\s*=`)
	keys := re.FindAllStringSubmatch(string(raw), -1)

	for _, lang := range []string{"en", "fr"} {
		e := newRig(t, newSandbox(t).with(func(o *Options) { o.Printer = newPrinter(lang) })).r.e

		for _, k := range keys {
			text := e.t(k[1])
			if strings.Contains(text, "%") {
				continue
			}

			got := wrapWords(e, e.Glyphs.warn+" "+text, width, 2)
			if len(got) > 0 && strings.HasSuffix(got[len(got)-1], e.Glyphs.ellipsis) {
				t.Errorf("%s %s: the reason is cut at %d cells: %q", lang, k[1], width, got)
			}
		}
	}
}
