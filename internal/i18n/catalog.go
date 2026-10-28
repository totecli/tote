package i18n

import (
	_ "embed"
	"maps"
	"slices"

	"github.com/BurntSushi/toml"
)

// enData is the raw content of the English catalogue, the default language and the fallback.
//
//go:embed en.toml
var enData []byte

// frData is the raw content of the French catalogue.
//
//go:embed fr.toml
var frData []byte

// parse decodes one catalogue file into a map from message key to message.
//
// Parameters:
//   - data: TOML content, a flat table whose values are all strings
//
// Returns the messages, or an error when the TOML is invalid, a key is defined twice or a value
// is not a string.
func parse(data []byte) (map[string]string, error) {
	var messages map[string]string

	if _, err := toml.Decode(string(data), &messages); err != nil {
		return nil, err
	}

	return messages, nil
}

// load decodes an embedded catalogue and returns an empty one when it cannot be decoded.
//
// Parameters:
//   - data: TOML content of an embedded catalogue
//
// Returns the messages. The embedded files are validated by the tests, so a failure here is
// answered by visible "!key!" markers instead of a crash of the command line.
func load(data []byte) map[string]string {
	messages, err := parse(data)
	if err != nil {
		return map[string]string{}
	}

	return messages
}

// Keys returns the keys of the English catalogue in sorted order.
//
// It is meant for tests that check every key is used or defined.
func Keys() []string {
	return slices.Sorted(maps.Keys(load(enData)))
}
