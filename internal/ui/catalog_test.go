package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestReviewLangVariableIgnoredByLoaderDoesNotLock checks that a TOTE_LANG the program ignores
// does not lock the language row. config.EffectiveLang only honours TOTE_LANG when its language
// part is "en" or "fr", so TOTE_LANG=de changes nothing; yet sourceOf reports the row as
// overridden by TOTE_LANG and read-only, and the screen shows "auto (fr)" and refuses to write
// even though config.toml says lang = "fr" and nothing else decides.
//
// Parameters:
//   - t: test to report to
func TestReviewLangVariableIgnoredByLoaderDoesNotLock(t *testing.T) {
	g, s := configRig(t, "schema = 1\nlang = \"fr\"\n", "TOTE_LANG=de")

	st := readFileState(s.Opts.Paths.ConfigFile())
	got := sourceOf(g.r.e, st, settingDefs[rowOf(t, "lang")])

	if got.locked || got.origin != originFile {
		t.Errorf("TOTE_LANG=de is ignored by the program but the row is %+v, want the file", got)
	}
}

// TestReviewCatalogKeysAreUsed checks that every ui_ key of the English catalog is read by the
// code of internal/ui, either as a whole literal or through one of the prefixes that the code
// completes at run time. It pins the audit result: no catalog key is left without a user.
//
// Parameters:
//   - t: test to report to
func TestReviewCatalogKeysAreUsed(t *testing.T) {
	raw, err := os.ReadFile("../i18n/en.toml")
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	var code strings.Builder

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}

		code.Write(data)
	}

	text := code.String()
	prefixes := regexp.MustCompile(`"(ui_[a-z_0-9]*_)"\s*\+`).FindAllStringSubmatch(text, -1)

	for _, m := range regexp.MustCompile(`(?m)^(ui_[a-z_0-9]+)\s*=`).FindAllStringSubmatch(
		string(raw), -1) {
		key := m[1]
		used := strings.Contains(text, `"`+key+`"`)

		for _, p := range prefixes {
			used = used || strings.HasPrefix(key, p[1])
		}

		if !used {
			t.Errorf("catalog key %s is not used by internal/ui", key)
		}
	}
}
