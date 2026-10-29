package shell

import "strings"

// Quote returns s as one POSIX shell word: single quotes around the text, every embedded single
// quote written as a closed quote, an escaped quote and a reopened quote. The empty string gives
// two single quotes. The result is safe for bash, zsh and dash, whatever the bytes of s are,
// except that a NUL byte cannot live in a shell word at all and is the caller's concern.
//
// Parameters:
//   - s: text to quote, any bytes except NUL
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteIfNeeded returns s unchanged when it is made only of characters that no POSIX shell or
// zsh treats specially, and Quote(s) otherwise.
//
// Parameters:
//   - s: word to display or emit, empty gives two single quotes
func quoteIfNeeded(s string) string {
	if s == "" {
		return Quote(s)
	}

	for i := 0; i < len(s); i++ {
		if !plainByte(s[i]) || (i == 0 && s[i] == '=') {
			return Quote(s)
		}
	}

	return s
}

// plainByte reports whether a byte is harmless inside an unquoted shell word.
//
// Parameters:
//   - c: byte to test
func plainByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}

	return strings.IndexByte("_-./,:@%+=", c) >= 0
}
