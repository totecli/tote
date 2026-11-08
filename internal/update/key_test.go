package update

import (
	"errors"
	"testing"
)

// TestReleaseKeys checks the embedded keys: the placeholder gives none, real keys given at
// build time (here by assignment) are used, and a malformed entry is refused.
//
// Parameters:
//   - t: test to report to
func TestReleaseKeys(t *testing.T) {
	saved := releaseKeys
	t.Cleanup(func() { releaseKeys = saved })

	if _, err := ReleaseKeys(); !errors.Is(err, ErrNoKey) {
		t.Errorf("placeholder: error %v, want ErrNoKey", err)
	}

	a, b := newSigner(1), newSigner(2)

	releaseKeys = a.encoded() + " " + b.encoded() + " RW_NEXT_KEY_PLACEHOLDER"

	keys, err := ReleaseKeys()
	if err != nil || len(keys) != 2 || keys[0].ID != a.id || keys[1].ID != b.id {
		t.Errorf("keys = %v, %v", keys, err)
	}

	releaseKeys = a.encoded() + " garbage " + b.encoded()

	if _, err := ReleaseKeys(); !errors.Is(err, ErrKeyFormat) {
		t.Errorf("malformed entry: error %v, want ErrKeyFormat", err)
	}

	releaseKeys = ""

	if _, err := ReleaseKeys(); !errors.Is(err, ErrNoKey) {
		t.Errorf("empty: error %v, want ErrNoKey", err)
	}
}
