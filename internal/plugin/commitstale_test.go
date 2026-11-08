package plugin

import (
	"os"
	"strings"
	"testing"
)

// TestReviewCommitStaleOnEachBaseField pins the staleness check of Commit one field at a time:
// the Diff the user saw was computed against a record, and a record that changed in its tree or
// in its consented capabilities since then (a second tote, a grant, a rebuild of the same
// version) must make the commit stale. Only the version and the consent date were covered.
//
// Parameters:
//   - t: test to report to
func TestReviewCommitStaleOnEachBaseField(t *testing.T) {
	tests := []struct {
		name   string
		change func(rec *LockRecord)
	}{
		{"tree", func(rec *LockRecord) { rec.TreeSHA256 = strings.Repeat("ab", 32) }},
		{"capabilities", func(rec *LockRecord) { rec.Caps = []string{"net:example.org"} }},
		{"version", func(rec *LockRecord) { rec.Version = "1.0.1" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			plain := surfaceBase("", "", "")
			r, dir := seeded(t, l, plain)

			rewrite(t, dir, withVersion(plain, "1.1.0"))

			st := plan(t, r, InstallRequest{Source: dir})

			must(t, l.lock().Update("demo", tt.change))

			if _, err := st.Commit(Grant{Prompt: true}); asError(t, err).Code != CodeStale {
				t.Errorf("commit = %v, want stale", err)
			}

			if names := stagingNames(t, l); len(names) != 0 {
				t.Errorf("staging = %v", names)
			}
		})
	}
}

// TestReviewCommitRecordCorruptedAfterPlan checks that a record of the plugin that became invalid
// between Plan and Commit stops the commit with store_corrupt and places no tree.
//
// Parameters:
//   - t: test to report to
func TestReviewCommitRecordCorruptedAfterPlan(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")
	r := l.reg(nil)

	st := plan(t, r, InstallRequest{Source: src})

	rec := sampleRecord()
	rec.ID = "demo"

	lk := l.lock()
	must(t, lk.Put(rec))

	data, err := os.ReadFile(l.paths.LockFile())
	must(t, err)

	broken := strings.Replace(string(data), sum256("t"), "zz", 1)
	must(t, os.WriteFile(l.paths.LockFile(), []byte(broken), 0o600))

	_, cerr := st.Commit(Grant{Prompt: true})
	if got := asError(t, cerr).Code; got != CodeStoreCorrupt {
		t.Fatalf("commit = %v, want %s", cerr, CodeStoreCorrupt)
	}

	if idDir, _ := l.paths.PluginDir("demo"); exists(idDir) {
		entries, _ := os.ReadDir(idDir)
		if len(entries) != 0 {
			t.Errorf("a tree was placed: %v", entries)
		}
	}
}
