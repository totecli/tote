package shell

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostileStrings lists values that have broken shell quoting in other programs. None contains a
// NUL byte, which no shell word can hold.
var hostileStrings = []string{
	"", " ", "'", "''", "'''", "a'b", "'a'", "\"", "\"'\"", "`", "``id``", "$(id)", "`id`",
	"${IFS}", "$HOME", "$'x'", "\\", "\\'", "\\\\", "a\\", "\\n", "a\nb", "\n", "\n\n", "a\r\nb",
	"\r", "\t", " leading", "trailing ", "-", "--", "-n", "-e", "-rf /", "--help", "=x", "~",
	"~root", "*", "?", "[a-z]", "{a,b}", "a;b", "a&&b", "a||b", "a|b", "a>b", "a<b", "(a)",
	"#comment", "!!", "!x", "%1", "\x01\x02\x1b[31m", "\x7f", "caf\u00e9", "\u65e5\u672c\u8a9e",
	"\U0001f600", "\u202e", "\u2028", "\xff\xfe", "a\xc3", "\x80", "'; touch PWNED; '",
	"$(touch PWNED)", "`touch PWNED`", "a' b\" c` d$ e\\ f", strings.Repeat("'", 50),
	strings.Repeat("a'b", 100),
}

// TestQuoteExact checks the literal output of Quote.
//
// Parameters:
//   - t: test to report to
func TestQuoteExact(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", `''`},
		{"abc", `'abc'`},
		{"a b", `'a b'`},
		{"it's", `'it'\''s'`},
		{"'", `''\'''`},
		{"''", `''\'''\'''`},
		{"$(x)", `'$(x)'`},
		{"a\nb", "'a\nb'"},
		{"\\", `'\'`},
		{"caf\u00e9", "'caf\u00e9'"},
	}

	for _, tc := range cases {
		if got := Quote(tc.in); got != tc.want {
			t.Errorf("Quote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestQuoteIfNeeded checks which words are left bare and which are quoted.
//
// Parameters:
//   - t: test to report to
func TestQuoteIfNeeded(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", `''`},
		{"ssh", "ssh"},
		{"-l", "-l"},
		{"user@host.example", "user@host.example"},
		{"/usr/bin/x_y-z.1", "/usr/bin/x_y-z.1"},
		{"a=b", "a=b"},
		{"=b", `'=b'`},
		{"a b", `'a b'`},
		{"[::1]", `'[::1]'`},
		{"fe80::1", "fe80::1"},
		{"~/x", `'~/x'`},
		{"it's", `'it'\''s'`},
		{"$HOME", `'$HOME'`},
		{"a\nb", "'a\nb'"},
		{"caf\u00e9", "'caf\u00e9'"},
		{"a*", `'a*'`},
	}

	for _, tc := range cases {
		if got := quoteIfNeeded(tc.in); got != tc.want {
			t.Errorf("quoteIfNeeded(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// randomStrings builds a reproducible list of random strings that favour shell metacharacters
// and also contain arbitrary non-NUL bytes.
//
// Parameters:
//   - n: number of strings to build
func randomStrings(n int) []string {
	rng := rand.New(rand.NewPCG(7, 11))
	tricky := []byte("'\"`$\\ \t\n\r!#&*()[]{};<>|?~=%^-.,/:@")
	out := make([]string, 0, n)

	for range n {
		size := rng.IntN(40)
		b := make([]byte, size)

		for i := range b {
			if rng.IntN(3) == 0 {
				b[i] = tricky[rng.IntN(len(tricky))]
			} else {
				b[i] = byte(1 + rng.IntN(255))
			}
		}

		out = append(out, string(b))
	}

	return out
}

// printAll builds a script that prints every value, each one followed by a NUL byte, through its
// quoted form.
//
// Parameters:
//   - values: strings to print back
func printAll(values []string) string {
	var b strings.Builder

	for _, v := range values {
		b.WriteString("printf '%s\\0' " + Quote(v) + "\n")
	}

	return b.String()
}

// checkRoundTrip prints every value through its quoted form in one shell and compares the bytes.
//
// Parameters:
//   - t: test to report to
//   - sh: path of the shell program
//   - values: strings to send through the shell, none containing a NUL
func checkRoundTrip(t *testing.T, sh string, values []string) {
	t.Helper()

	sb := newSandbox(t)

	out, errOut, code := sb.run(t, sh, printAll(values))
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}

	got := strings.Split(out, "\x00")
	if len(got) != len(values)+1 {
		t.Fatalf("got %d values, want %d", len(got)-1, len(values))
	}

	for i, v := range values {
		if got[i] != v {
			t.Errorf("value %d: got %q, want %q", i, got[i], v)
		}
	}

	if _, err := os.Stat(filepath.Join(sb.Dir, "PWNED")); err == nil {
		t.Error("an injected command ran")
	}
}

// TestQuoteRoundTrip runs hostile and random strings through every installed shell and checks
// that each one comes back byte for byte.
//
// Parameters:
//   - t: test to report to
func TestQuoteRoundTrip(t *testing.T) {
	shells := installedShells()
	if len(shells) == 0 {
		t.Skip("no shell installed")
	}

	values := append(append([]string{}, hostileStrings...), randomStrings(600)...)

	for _, sh := range shells {
		t.Run(filepath.Base(sh), func(t *testing.T) { checkRoundTrip(t, sh, values) })
	}
}

// checkCommandString runs each hostile string as the only argument of printf in "sh -c".
//
// Parameters:
//   - t: test to report to
//   - sh: path of the shell program
func checkCommandString(t *testing.T, sh string) {
	t.Helper()

	sb := newSandbox(t)

	for _, v := range hostileStrings {
		out, errOut, code := sb.runC(t, sh, "printf %s "+Quote(v))
		if code != 0 || errOut != "" || out != v {
			t.Errorf("%q: out %q, stderr %q, exit %d", v, out, errOut, code)
		}
	}

	if _, err := os.Stat(filepath.Join(sb.Dir, "PWNED")); err == nil {
		t.Error("an injected command ran")
	}
}

// TestQuoteCommandString runs each hostile string through "bash -c" with the quoted word as the
// only argument of printf, which is the form a caller would build.
//
// Parameters:
//   - t: test to report to
func TestQuoteCommandString(t *testing.T) {
	for _, sh := range installedShells() {
		t.Run(filepath.Base(sh), func(t *testing.T) { checkCommandString(t, sh) })
	}
}
