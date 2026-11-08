package plugin

import (
	"errors"
	"testing"
)

// TestReviewExitClassesOfTheSpec pins the exit code of every stable code that section 9 of the
// specification names, since a script depends on them: usage 2, policy 4, integrity 5. Only
// grant_mismatch, enable_refused and a few more were covered one by one.
//
// Parameters:
//   - t: test to report to
func TestReviewExitClassesOfTheSpec(t *testing.T) {
	want := map[int][]string{
		ExitUsage: {
			"bad_source", "hash_required", "hash_malformed", "consent_needed", "bad_id",
			"source_required",
		},
		ExitPolicy: {"grant_mismatch", "not_runnable", "enable_refused", "from_plugin", "busy"},
		ExitIntegrity: {
			"hash_mismatch", "archive_bomb", "archive_traversal", "too_large",
			"tree_hash", "platform_hash", "tree_collision", "origin_redirect",
		},
		ExitFailure: {"auth_required"},
	}

	for exit, codes := range want {
		for _, code := range codes {
			if got := ExitClass(code); got != exit {
				t.Errorf("ExitClass(%q) = %d, want %d", code, got, exit)
			}

			if got := ExitOf(newError(code)); got != exit {
				t.Errorf("ExitOf(%q) = %d, want %d", code, got, exit)
			}
		}
	}

	if ExitOf(nil) != 0 || ExitOf(errors.New("plain")) != ExitFailure {
		t.Errorf("nil gives %d, a plain error %d", ExitOf(nil), ExitOf(errors.New("plain")))
	}
}
