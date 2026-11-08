package plugin

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzReviewCheckOpenURL fuzzes the check that stands between a plugin's open template and the
// platform opener, which had no fuzz target. Whatever it accepts must be an https URL with a host
// and no user information, in printable ASCII, without a backslash, whitespace or control byte,
// and must start with a letter so that the opener cannot read it as an option.
//
// Parameters:
//   - f: fuzzer
func FuzzReviewCheckOpenURL(f *testing.F) {
	for _, s := range []string{
		"https://github.com/20syldev/tote/releases", "https://a@b.example/x",
		"https://evil.example\\@github.com/", "https://-x.example/", "http://github.com/",
		"https://github.com/%0a-y", "https://[::1]/x", "https://github.com:443/x", "-https://x/",
		"https://github.com/ a", "https://github.com/\x00", "https:///x", "https://a.b/" + "\u00e9",
	} {
		f.Add(s, "")
		f.Add(s, "github.com")
	}

	f.Fuzz(func(t *testing.T, raw, host string) {
		got, err := CheckOpenURL(raw, host)
		if err != nil {
			if got != "" {
				t.Fatalf("an error with a result %q", got)
			}

			return
		}

		u, perr := url.Parse(got)
		if perr != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" ||
			strings.HasPrefix(u.Hostname(), "-") {
			t.Fatalf("accepted %q (%v)", got, perr)
		}

		// The scheme keeps the case it was written in ("Https://x" is accepted and passed on as
		// it is), which is harmless for an opener; everything else must be plain.
		if len(got) < 8 || !strings.EqualFold(got[:8], "https://") ||
			strings.ContainsAny(got, " \t\r\n\\\x00\x7f") || !printableASCII(got) {
			t.Fatalf("accepted a URL with a forbidden byte: %q", got)
		}

		if host != "" && !strings.EqualFold(u.Host, host) {
			t.Fatalf("accepted host %q for %q", u.Host, host)
		}
	})
}

// FuzzReviewParseProcs fuzzes the reader of plugin-procs, a file that a plugin of the same user
// can write: it must never panic, and every entry it returns must be one that StopPlugin may act
// on (a valid id, a session above 1, a start token of digits or a dash).
//
// Parameters:
//   - f: fuzzer
func FuzzReviewParseProcs(f *testing.F) {
	for _, s := range []string{
		"demo 1234 -\n", "demo 1 -\n", "demo -5 -\n", "demo 99999999999999999999 -\n",
		"../x 12 -\n", "demo 12\n", "demo 12 - extra\n", "demo\t12\t55\r\n", "\x00\n",
		strings.Repeat("a 99 1\n", 3000),
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, text string) {
		entries := parseProcs(text)
		if len(entries) > maxProcsEntries {
			t.Fatalf("%d entries", len(entries))
		}

		for _, e := range entries {
			if !validID(e.ID) || e.SID <= 1 || !validToken(e.Start) {
				t.Fatalf("returned %+v", e)
			}
		}
	})
}

// FuzzReviewOpenLock fuzzes the lock file reader on arbitrary bytes: it must neither panic nor
// hand out a record that does not validate, and an unreadable file must fail closed (no record).
//
// Parameters:
//   - f: fuzzer
func FuzzReviewOpenLock(f *testing.F) {
	good := "schema = 1\n[[plugin]]\nid = \"gft\"\nversion = \"1.0.0\"\n"

	for _, s := range []string{
		"", good, "[[plugin]]\nid = 1\n", "schema = 2\n",
		"[[plugin]\n", "\xef\xbb\xbf" + good, good + good, strings.Repeat("[", 4096),
	} {
		f.Add([]byte(s))
	}

	dir := f.TempDir()

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}

		path := filepath.Join(dir, "plugins.lock")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}

		lk, err := OpenLock(path)
		if err != nil {
			return
		}

		if lk.Err() != nil && len(lk.Records()) != 0 {
			t.Fatal("records of a lock that failed to parse")
		}

		for _, rec := range lk.Records() {
			if verr := rec.Validate(); verr != nil {
				t.Fatalf("an invalid record %+v was returned as usable: %v", rec, verr)
			}
		}
	})
}
