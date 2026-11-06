package plugin

import (
	"strings"
	"testing"
)

// TestRedactor checks the exact-value redaction, the shape net and the PEM state.
//
// Parameters:
//   - t: test to report to
func TestRedactor(t *testing.T) {
	const (
		ghp = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
		jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0." +
			"dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		pem1 = "-----BEGIN OPENSSH PRIVATE KEY-----"
		pem2 = "-----END OPENSSH PRIVATE KEY-----"
	)

	tests := []struct {
		name   string
		values []string
		in     string
		want   string
	}{
		{"exact", []string{"hunter22secret"}, "pw=hunter22secret!", "pw=****!"},
		{"exact twice", []string{"hunter22secret"}, "hunter22secret hunter22secret", "**** ****"},
		{"short value is left alone", []string{"abc"}, "abc", "abc"},
		{"longest first", []string{"abcd1234", "abcd12345678"}, "x abcd12345678 y", "x **** y"},
		{
			"multi line value",
			[]string{"line-one-secret\nline-two-secret"},
			"a line-two-secret b", "a **** b",
		},
		{"github token", nil, "token " + ghp + " end", "token **** end"},
		{"fine grained token", nil, "github_pat_11ABCDEFG0abcdefghijkl_mnop", "****"},
		{"aws key", nil, "AKIAIOSFODNN7EXAMPLE", "****"},
		{"gitlab token", nil, "glpat-abcdefghijklmnopqrst", "****"},
		{"slack token", nil, "xoxb-1234567890-abcdef", "****"},
		{"jwt", nil, "t=" + jwt, "t=****"},
		{"bearer", nil, "Authorization: Bearer abcdefghijklmnop1234", "Authorization: Bearer ****"},
		{"url credentials", nil, "https://user:pass@host/x", "https://****@host/x"},
		{"url without credentials", nil, "https://host/x", "https://host/x"},
		{"pem on one line", nil, pem1 + "abc" + pem2, "****"},
		{"nothing", nil, "plain text", "plain text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewRedactor(tt.values).Line(tt.in); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}

	t.Run("pem block over lines", func(t *testing.T) {
		rd := NewRedactor(nil)
		got := rd.Text("before\n" + pem1 + "\nAAAAB3NzaC1\nmore\n" + pem2 + "\nafter")

		if got != "before\n****\n****\n****\n****\nafter" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("an open pem block ends after 200 lines", func(t *testing.T) {
		rd := NewRedactor(nil)
		lines := []string{pem1}

		for i := 0; i < 250; i++ {
			lines = append(lines, "visible")
		}

		got := strings.Split(rd.Text(strings.Join(lines, "\n")), "\n")

		if got[1] != Mask || got[len(got)-1] != "visible" {
			t.Errorf("first %q last %q", got[1], got[len(got)-1])
		}
	})

	t.Run("a nil redactor changes nothing", func(t *testing.T) {
		var rd *Redactor
		if rd.Line("x") != "x" || rd.Text("x") != "x" || rd.Scrub("x") != "x" || rd.MaxSecretLen() != 0 {
			t.Error("nil redactor altered the text")
		}
	})

	t.Run("max secret length", func(t *testing.T) {
		if n := NewRedactor([]string{"abcd", "abcdefgh"}).MaxSecretLen(); n != 8 {
			t.Errorf("got %d", n)
		}
	})
}

// TestShapeCandidate checks the cheap pre-filter of the credential shapes: it never says no to a
// text that a shape matches, and it does not panic on the ends of a string.
//
// Parameters:
//   - t: test to report to
func TestShapeCandidate(t *testing.T) {
	secret := "abcdefghijklmnopqrstuvwxyz0123456789"

	tests := []string{
		"ghp_" + secret,
		"github_pat_" + secret,
		"glpat-" + secret,
		"xoxb-" + secret,
		"AKIAABCDEFGHIJKLMNOP",
		"AIza" + secret,
		"sk-" + secret,
		"eyJabcdefgh.abcdefgh.abcdefgh",
		"BeArEr " + secret,
		"TOKEN " + secret,
		"https://user:pass@host/",
	}

	rd := NewRedactor(nil)

	for _, s := range tests {
		if !shapeCandidate(s) {
			t.Errorf("%q: the pre-filter let a credential shape through", s)
		}

		if got := rd.Scrub("x " + s); !strings.Contains(got, Mask) {
			t.Errorf("%q: not redacted: %q", s, got)
		}
	}

	for _, s := range []string{"", "t", "to", "tok", "toke", "b", "bear", "beare", "plain text y"} {
		if shapeCandidate(s) {
			t.Errorf("%q: candidate", s)
		}
	}
}

// FuzzShapePrefilter checks that the pre-filter of the credential shapes never changes what the
// expressions would do: the redaction with and without it gives the same text.
//
// Parameters:
//   - f: fuzz target
func FuzzShapePrefilter(f *testing.F) {
	for _, s := range []string{
		"ghp_" + strings.Repeat("a", 30), "Bearer " + strings.Repeat("a", 20),
		"sk-" + strings.Repeat("b", 24), "https://u:p@h/", "TOKEN x", "plain", "tok", "bear",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			return
		}

		full := s
		for _, re := range tokenShapes {
			full = re.ReplaceAllString(full, Mask)
		}

		full = bearerShape.ReplaceAllString(full, "${1}${2}"+Mask)
		full = userinfoShape.ReplaceAllString(full, "${1}"+Mask+"@")

		if got := NewRedactor(nil).Scrub(s); got != full {
			t.Fatalf("%q: got %q want %q", s, got, full)
		}
	})
}
