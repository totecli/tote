package plugin

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestFilterOutput checks the pager filter: only the colour sequences of the whitelist survive,
// whatever else a program prints becomes a question mark or disappears, and nothing can hide
// text or paint over the frame.
//
// Parameters:
//   - t: test to report to
func TestFilterOutput(t *testing.T) {
	const reset = "\x1b[0m"

	tests := []struct {
		name string
		in   string
		sgr  bool
		want string
	}{
		{"plain", "hello", true, "hello"},
		{"newline and tab", "a\n\tb", true, "a\n\tb"},
		{"crlf", "a\r\nb", true, "a\nb"},
		{"lone cr", "a\rb", true, "a?b"},
		{"red", "\x1b[31mred", true, "\x1b[31mred" + reset},
		{"bold red", "\x1b[1;31mx", true, "\x1b[1;31mx" + reset},
		{"empty parameter", "\x1b[mx", true, "\x1b[mx" + reset},
		{"bright", "\x1b[92mx", true, "\x1b[92mx" + reset},
		{"default fg", "\x1b[39mx", true, "\x1b[39mx" + reset},
		{"kept sequence without colours when mono", "\x1b[31mred", false, "red"},
		{"blink dropped", "a\x1b[5mb", true, "ab"},
		{"rapid blink dropped", "a\x1b[6mb", true, "ab"},
		{"reverse dropped", "a\x1b[7mb", true, "ab"},
		{"conceal dropped", "a\x1b[8mb", true, "ab"},
		{"background dropped", "a\x1b[41mb", true, "ab"},
		{"bright background dropped", "a\x1b[101mb", true, "ab"},
		{"256 colour dropped", "a\x1b[38;5;196mb", true, "ab"},
		{"truecolor dropped", "a\x1b[48;2;1;2;3mb", true, "ab"},
		{"one bad parameter drops all", "a\x1b[1;5mb", true, "ab"},
		{"cursor movement", "a\x1b[2Jb", true, "a?[2Jb"},
		{"cursor position", "a\x1b[10;10Hb", true, "a?[10;10Hb"},
		{"erase line", "\x1b[K", true, "?[K"},
		{"osc title", "\x1b]0;evil\x07x", true, "?]0;evil?x"},
		{"osc 8 link", "\x1b]8;;http://x\x1b\\t\x1b]8;;\x1b\\", true, "?]8;;http://x?\\t?]8;;?\\"},
		{"osc 52 clipboard", "\x1b]52;c;aGk=\x07", true, "?]52;c;aGk=?"},
		{"dcs", "\x1bP1$r\x1b\\", true, "?P1$r?\\"},
		{"apc", "\x1b_Ga=q\x1b\\", true, "?_Ga=q?\\"},
		{"pm", "\x1b^x\x1b\\", true, "?^x?\\"},
		{"sos", "\x1bXx\x1b\\", true, "?Xx?\\"},
		{"reset terminal", "\x1bc", true, "?c"},
		{"alt screen", "\x1b[?1049h", true, "?[?1049h"},
		{"lone escape at the end", "x\x1b", true, "x?"},
		{"escape bracket at the end", "x\x1b[", true, "x?["},
		{"unterminated sequence", "x\x1b[31", true, "x?[31"},
		{"bel", "a\x07b", true, "a?b"},
		{"backspace", "a\x08b", true, "a?b"},
		{"nul", "a\x00b", true, "a?b"},
		{"del", "a\x7fb", true, "a?b"},
		{"c1 csi", "a\u009b31mb", true, "a?31mb"},
		{"bidi override", "a\u202eb", true, "a?b"},
		{"zero width space", "a\u200bb", true, "a?b"},
		{"zero width joiner kept", "a\u200db", true, "a\u200db"},
		{"invalid utf8", "a\xffb", true, "a\ufffdb"},
		{"accents", "caf\u00e9", true, "caf\u00e9"},
		{
			"too long to be a sequence", "\x1b[" + strings.Repeat("1;", 40) + "m", true,
			"?[" + strings.Repeat("1;", 40) + "m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FilterOutput(tt.in, tt.sgr); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

// TestFilterEscapeInjection checks that no program text can leave a control character or an
// escape that is not a whitelisted colour sequence in the output.
//
// Parameters:
//   - t: test to report to
func TestFilterEscapeInjection(t *testing.T) {
	var b strings.Builder

	for i := 0; i < 256; i++ {
		b.WriteByte(byte(i))
		b.WriteString("\x1b[")
		b.WriteByte(byte(i))
	}

	for _, sgr := range []bool{true, false} {
		assertSafeOutput(t, FilterOutput(b.String(), sgr))
	}
}

// assertSafeOutput fails when the text holds anything the pager must never get.
//
// Parameters:
//   - t: test to report to
//   - s: filtered text
func assertSafeOutput(t testing.TB, s string) {
	t.Helper()

	if !utf8.ValidString(s) {
		t.Fatalf("invalid UTF-8 in %q", s)
	}

	rest := s

	for {
		i := strings.IndexByte(rest, 0x1b)
		if i < 0 {
			break
		}

		end := strings.IndexByte(rest[i:], 'm')
		if !strings.HasPrefix(rest[i:], "\x1b[") || end < 0 || !paramsAllowed(rest[i+2:i+end]) {
			t.Fatalf("escape that is not a colour in %q", s)
		}

		rest = rest[:i] + rest[i+end+1:]
	}

	for _, r := range rest {
		if r == '\n' || r == '\t' {
			continue
		}

		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || unsafeRune(r) {
			t.Fatalf("unsafe rune %U in %q", r, s)
		}
	}
}

// FuzzFilterOutput checks the property of the pager filter for any input.
//
// Parameters:
//   - f: fuzz target
func FuzzFilterOutput(f *testing.F) {
	for _, s := range []string{
		"\x1b[31mred", "\x1b]52;c;x\x07", "a\r\nb", "\x1b[38;5;1m", "\xff\x1b",
		"\u202e\u009b",
	} {
		f.Add(s, true)
		f.Add(s, false)
	}

	f.Fuzz(func(t *testing.T, s string, sgr bool) {
		if len(s) > 1<<16 {
			return
		}

		out := FilterOutput(s, sgr)
		assertSafeOutput(t, out)

		if !sgr && strings.Contains(out, "\x1b") {
			t.Fatalf("colour kept in the mono theme: %q", out)
		}
	})
}

// TestLineSink checks that output is cut at line breaks, that a line without one is cut in
// pieces, and that a secret which straddles a cut is still hidden.
//
// Parameters:
//   - t: test to report to
func TestLineSink(t *testing.T) {
	t.Run("lines", func(t *testing.T) {
		s := newLineSink(nil, true)

		got := s.write([]byte("one\ntw"))
		got = append(got, s.write([]byte("o\nthree"))...)
		got = append(got, s.flush()...)

		want := []string{"one\n", "two\n", "three"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("got %q want %q", got, want)
		}
	})

	t.Run("a 70 KiB line comes out whole, in pieces", func(t *testing.T) {
		s := newLineSink(nil, true)
		line := strings.Repeat("x", 70<<10)

		var got []string

		for i := 0; i < len(line); i += 4096 {
			got = append(got, s.write([]byte(line[i:min(i+4096, len(line))]))...)
		}

		got = append(got, s.flush()...)

		if strings.Join(got, "") != line || len(got) < 4 {
			t.Errorf("%d pieces, %d bytes", len(got), len(strings.Join(got, "")))
		}

		for _, p := range got {
			if len(p) > chunkMax+4096 {
				t.Errorf("piece of %d bytes", len(p))
			}
		}
	})

	t.Run("a secret on the cut is hidden", func(t *testing.T) {
		secret := "s3cr3t-value-that-is-long"
		s := newLineSink(NewRedactor([]string{secret}), true)
		text := strings.Repeat("a", chunkMax-10) + secret + strings.Repeat("b", 50)

		got := s.write([]byte(text))
		got = append(got, s.flush()...)

		if joined := strings.Join(got, ""); strings.Contains(joined, secret) ||
			!strings.Contains(joined, Mask) {
			t.Errorf("secret survived: %q", joined[len(joined)-100:])
		}
	})

	t.Run("escapes are filtered", func(t *testing.T) {
		s := newLineSink(nil, false)

		got := strings.Join(append(s.write([]byte("\x1b]52;c;x\x07ok\n")), s.flush()...), "")
		assertSafeOutput(t, got)
	})
}
