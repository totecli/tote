package update

import "strings"

// releaseKeys lists the minisign public keys that sign the releases, separated by spaces.
//
// The first is the current key and the second the offline key kept for a rotation. The value
// is a placeholder in the sources: the real keys are set at build time with
// -X github.com/totecli/tote/internal/update.releaseKeys=... so the owner can inject them
// without a code change. An entry that ends with _PLACEHOLDER does not count; any other entry
// that is not a well formed key makes the build refuse to update, so that a typo in the release
// pipeline is noticed.
var releaseKeys = "RW_CURRENT_RELEASE_KEY_PLACEHOLDER RW_NEXT_RELEASE_KEY_PLACEHOLDER"

// ReleaseKeys returns the release keys embedded in this build.
//
// Returns the keys, ErrNoKey when there is none, as in a build that still has the placeholder,
// or an error wrapping ErrKeyFormat when an entry is neither a placeholder nor a well formed
// key.
func ReleaseKeys() ([]PublicKey, error) {
	var keys []PublicKey

	for _, f := range strings.Fields(releaseKeys) {
		if strings.HasSuffix(f, "_PLACEHOLDER") {
			continue
		}

		k, err := ParsePublicKey(f)
		if err != nil {
			return nil, err
		}

		keys = append(keys, k)
	}

	if len(keys) == 0 {
		return nil, ErrNoKey
	}

	return keys, nil
}
