package plugin

import "testing"

// TestContributesOf checks the counts that plugin list prints for a manifest and for none.
//
// Parameters:
//   - t: test to report to
func TestContributesOf(t *testing.T) {
	if got := ContributesOf(nil); got != (Contributes{}) {
		t.Errorf("nil manifest: %+v", got)
	}

	m := &Manifest{
		Commands: make([]Entry, 2), ItemActions: make([]Entry, 1),
		Lists: make([]List, 3), Settings: make([]Setting, 4),
	}

	want := Contributes{ItemActions: 1, Commands: 2, Lists: 3, Settings: 4}
	if got := ContributesOf(m); got != want {
		t.Errorf("ContributesOf = %+v, want %+v", got, want)
	}
}
