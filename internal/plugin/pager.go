package plugin

import (
	"strings"
	"unicode/utf8"
)

// maxSGRBytes is the longest escape sequence that can be a colour sequence; a longer one is not
// one.
const maxSGRBytes = 64

// sgrAllowed holds the parameters of a colour sequence that survive the pager filter: reset, bold,
// faint, italic, underline and their ends, and the 16 foreground colours with the default one.
// Blink (5, 6), reverse (7), conceal (8), every background and the 38 and 48 extended forms are
// not in it: they could hide text or paint over the frame.
var sgrAllowed = func() map[string]bool {
	m := map[string]bool{"": true}

	for _, p := range []string{"0", "1", "2", "3", "4", "22", "23", "24", "39"} {
		m[p] = true
	}

	for i := 30; i <= 37; i++ {
		m[itoa(i)] = true
	}

	for i := 90; i <= 97; i++ {
		m[itoa(i)] = true
	}

	return m
}()

// FilterOutput makes the output of a program safe for the pager.
//
// Parameters:
//   - s: text the program wrote, any bytes
//   - sgr: true to keep the colour sequences of the whitelist (the mono theme passes false)
//
// Returns the text where only "ESC [ params m" sequences whose parameters are all in the whitelist
// survive; a colour sequence with any other parameter is dropped whole; every other escape and
// every control character except line feed and tab becomes '?' (a carriage return before a line
// feed is dropped); invalid UTF-8 becomes U+FFFD; bidi, zero-width and other format characters
// become '?'. When a colour was kept, a reset ends the text so that it cannot leak into the frame.
func FilterOutput(s string, sgr bool) string {
	var (
		b    strings.Builder
		kept bool
	)

	b.Grow(len(s))

	for i := 0; i < len(s); {
		c := s[i]

		if c == 0x1b {
			n, keep := colourSequence(s[i:], sgr)
			if n > 0 {
				if keep {
					b.WriteString(s[i : i+n])

					kept = true
				}

				i += n

				continue
			}

			b.WriteByte('?')

			i++

			continue
		}

		if c < utf8.RuneSelf {
			i = filterASCII(&b, s, i)

			continue
		}

		r, size := utf8.DecodeRuneInString(s[i:])

		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteRune(utf8.RuneError)
		case unsafeRune(r):
			b.WriteByte('?')
		default:
			b.WriteString(s[i : i+size])
		}

		i += size
	}

	if kept {
		b.WriteString("\x1b[0m")
	}

	return b.String()
}

// filterASCII writes one ASCII byte of the output.
//
// Parameters:
//   - b: result under construction
//   - s: output
//   - i: index of an ASCII byte other than ESC
//
// Returns the index of the next byte to read.
func filterASCII(b *strings.Builder, s string, i int) int {
	c := s[i]

	switch {
	case c == '\n' || c == '\t':
		b.WriteByte(c)
	case c == '\r' && i+1 < len(s) && s[i+1] == '\n':
	case c < 0x20 || c == 0x7f:
		b.WriteByte('?')
	default:
		b.WriteByte(c)
	}

	return i + 1
}

// colourSequence reads a CSI sequence ending in 'm' at the start of a text.
//
// Parameters:
//   - s: text that starts with ESC
//   - sgr: true when colours are allowed
//
// Returns the length of the sequence and whether it is kept. A length of 0 says that the text
// does not start with a colour sequence, whatever follows ESC.
func colourSequence(s string, sgr bool) (int, bool) {
	if len(s) < 3 || s[1] != '[' {
		return 0, false
	}

	for i := 2; i < len(s) && i < maxSGRBytes; i++ {
		c := s[i]

		switch {
		case c >= '0' && c <= '9', c == ';':
		case c == 'm':
			return i + 1, sgr && paramsAllowed(s[2:i])
		default:
			return 0, false
		}
	}

	return 0, false
}

// paramsAllowed checks the parameters of a colour sequence against the whitelist.
//
// Parameters:
//   - params: text between "ESC [" and "m", digits and semicolons
func paramsAllowed(params string) bool {
	for _, p := range strings.Split(params, ";") {
		if !sgrAllowed[p] {
			return false
		}
	}

	return true
}

// lineSink turns the bytes a program wrote into clean, redacted text chunks that end at a line
// break, or after at most chunkMax bytes of a line that has none.
type lineSink struct {
	pending []byte
	rd      *Redactor
	sgr     bool
	keep    int
}

// chunkMax is the longest piece of a line that is emitted without a line break.
const chunkMax = 16 << 10

// newLineSink creates a sink.
//
// Parameters:
//   - rd: redactor of the stream, may be nil
//   - sgr: true to keep the whitelisted colour sequences
func newLineSink(rd *Redactor, sgr bool) *lineSink {
	return &lineSink{rd: rd, sgr: sgr, keep: rd.MaxSecretLen()}
}

// write adds bytes and returns the chunks that are complete. A chunk includes its line break.
//
// Parameters:
//   - p: bytes read from the program
func (s *lineSink) write(p []byte) []string {
	s.pending = append(s.pending, p...)

	var out []string

	for {
		i := indexByte(s.pending, '\n')

		switch {
		case i >= 0:
			out = append(out, s.render(string(s.pending[:i]), "\n"))
			s.pending = s.pending[i+1:]
		case len(s.pending) >= chunkMax+s.keep:
			cut := len(s.pending) - s.keep
			out = append(out, s.render(string(s.pending[:cut]), ""))
			s.pending = append([]byte(nil), s.pending[cut:]...)
		default:
			return out
		}
	}
}

// flush returns the last chunk, without a line break.
func (s *lineSink) flush() []string {
	if len(s.pending) == 0 {
		return nil
	}

	out := []string{s.render(string(s.pending), "")}
	s.pending = nil

	return out
}

// render redacts, filters and terminates one line.
//
// Parameters:
//   - line: text without its line break
//   - end: line break to append, or nothing
func (s *lineSink) render(line, end string) string {
	// Redact before filtering, on the bytes the program wrote, and again after, in case the filter
	// joined the two halves of a secret that an escape sequence had cut.
	line = s.rd.Line(line)
	line = FilterOutput(line, s.sgr)

	return s.rd.Scrub(line) + end
}

// indexByte finds a byte in a slice.
//
// Parameters:
//   - b: bytes to search
//   - c: byte to find
func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}

	return -1
}
