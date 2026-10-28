package i18n

import "strings"

// Prefixes of the catalog keys that are named after a stable code of the plugin platform.
const (
	// PrefixError is the prefix of the message of an error code, such as plugin_err_hash_mismatch.
	PrefixError = "plugin_err_"
	// PrefixReason is the prefix of the message of a state reason, such as plugin_reason_lock_off.
	PrefixReason = "plugin_reason_"
	// PrefixState is the prefix of the name of a plugin state, such as plugin_state_enabled.
	PrefixState = "plugin_state_"
)

// Has reports whether a key has a message, in the language of the Printer or in English.
//
// Parameters:
//   - key: catalogue key such as "plugin_err_busy"
func (p *Printer) Has(key string) bool {
	if _, ok := p.own[key]; ok {
		return true
	}

	_, ok := p.base[key]

	return ok
}

// Code renders the message of a stable code.
//
// The key is the prefix followed by the code, where the dots and dashes of the code become
// underscores (catalogue keys only hold letters, digits and underscores). The values are the
// arguments of the code: missing ones are replaced by an empty text and extra ones are dropped, so
// that a code raised with another number of arguments than its message expects never prints a
// Printf error.
//
// Parameters:
//   - prefix: one of PrefixError, PrefixReason and PrefixState
//   - code: stable code, such as "hash_mismatch" or "needs-consent"
//   - args: values of the code, all texts
//
// Returns the message and true, or the code itself and false when the catalogue has no message
// for it.
func (p *Printer) Code(prefix, code string, args []string) (string, bool) {
	key := prefix + CodeKey(code)
	if !p.Has(key) {
		return code, false
	}

	msg, ok := p.own[key]
	if !ok {
		msg = p.base[key]
	}

	n := verbCount(msg)
	if n == 0 {
		return msg, true
	}

	values := make([]any, n)
	for i := range values {
		if i < len(args) {
			values[i] = args[i]
		} else {
			values[i] = ""
		}
	}

	return p.T(key, values...), true
}

// CodeKey turns a stable code into the tail of a catalogue key.
//
// Parameters:
//   - code: stable code, such as "needs-consent" or "argv.shell"
func CodeKey(code string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(code)
}

// verbCount counts the Printf verbs of a message, not the doubled percent signs.
//
// Parameters:
//   - msg: message of the catalogue
func verbCount(msg string) int {
	n := 0

	for i := 0; i < len(msg); i++ {
		if msg[i] != '%' {
			continue
		}

		if i+1 < len(msg) && msg[i+1] == '%' {
			i++

			continue
		}

		n++
	}

	return n
}
