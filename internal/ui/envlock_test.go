package ui

import (
	"testing"
)

// TestReviewIneffectiveEnvDoesNotLock documents a defect: a TOTE_LANG that names no supported
// language is skipped by config.EffectiveLang, so it changes nothing, yet the settings screen
// treats the variable as an override: it names it as the source of the language and refuses to
// change the row ("set by TOTE_LANG, change it there"). The row should be locked only by a
// variable that really decides the value. The test fails until the lock follows EffectiveLang.
//
// Parameters:
//   - t: test to report to
func TestReviewIneffectiveEnvDoesNotLock(t *testing.T) {
	g, s := configRig(t, "schema = 1\nlang = \"fr\"\n", "TOTE_LANG=klingon")

	got := sourceOf(g.r.e, readFileState(s.Opts.Paths.ConfigFile()),
		settingDefs[rowOf(t, "lang")])

	if got.locked || got.name == "TOTE_LANG" {
		t.Errorf("source %+v: a TOTE_LANG that is ignored locks the language row", got)
	}
}
