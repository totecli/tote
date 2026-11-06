package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// TestSlugTable checks the derivation table of section 3.5 through the whole path a remote takes:
// cleaned first, then reduced to a slug.
//
// Parameters:
//   - t: test to report to
func TestSlugTable(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		clean  string
		slug   string
	}{
		{"https", "https://github.com/o/r", "https://github.com/o/r", "o/r"},
		{"https dot git", "https://github.com/o/r.git", "https://github.com/o/r.git", "o/r"},
		{"https trailing slash", "https://github.com/o/r/", "https://github.com/o/r/", "o/r"},
		{"https upper host", "https://GitHub.com/o/r", "https://GitHub.com/o/r", "o/r"},
		{"scp like", "git@github.com:o/r.git", "git@github.com:o/r.git", "o/r"},
		{"scp like no git suffix", "git@github.com:o/r", "git@github.com:o/r", "o/r"},
		{"ssh url", "ssh://git@github.com/o/r.git", "ssh://github.com/o/r.git", "o/r"},
		{"git url", "git://github.com/o/r.git", "git://github.com/o/r.git", "o/r"},
		{
			"credentials are dropped", "https://user:secret@github.com/o/r",
			"https://github.com/o/r", "o/r",
		},
		{
			"token in userinfo is dropped",
			"https://ghp_abcdefghijklmnopqrstuvwxyz0123456789@github.com/o/r",
			"https://github.com/o/r", "o/r",
		},
		{"ssh host alias", "git@gh-alias:o/r.git", "git@gh-alias:o/r.git", ""},
		{"other host", "https://gitlab.com/o/r.git", "https://gitlab.com/o/r.git", ""},
		{"lookalike host", "https://github.com.evil.dev/o/r", "https://github.com.evil.dev/o/r", ""},
		{"host with port", "https://github.com:8443/o/r", "https://github.com:8443/o/r", ""},
		{"three segments", "https://github.com/o/r/x", "https://github.com/o/r/x", ""},
		{"one segment", "https://github.com/o", "https://github.com/o", ""},
		{"name with a space", "https://github.com/o/r r", "", ""},
		{"name outside the charset", "https://github.com/o/r%20x", "https://github.com/o/r%20x", ""},
		{"dot dot segment", "https://github.com/../r", "https://github.com/../r", ""},
		{"dot segment", "https://github.com/o/.", "https://github.com/o/.", ""},
		{"leading dash owner", "https://github.com/-o/r", "", ""},
		{"leading dash name", "git@github.com:o/-r", "git@github.com:o/-r", ""},
		{"empty", "", "", ""},
		{"at sign left in the path", "ssh://0/@", "", ""},
		{"at sign left in the query", "https://github.com/o/r?k=a@b", "", ""},
		{"file scheme", "file:///etc/passwd", "", ""},
		{"file without slashes", "file:/etc/passwd", "", ""},
		{"ext transport", "ext::sh -c 'touch /tmp/x'", "", ""},
		{"ext upper", "EXT::sh", "", ""},
		{"http is refused", "http://github.com/o/r", "", ""},
		{"ftp is refused", "ftp://github.com/o/r", "", ""},
		{"local path", "/srv/git/r.git", "", ""},
		{"relative path", "../r.git", "", ""},
		{"host starting with a dash", "ssh://-oProxyCommand=x/o/r", "", ""},
		{"scp host starting with a dash", "-oProxyCommand=x:o/r", "", ""},
		{"scp path starting with a dash", "git@host:-x/r", "", ""},
		{"url path starting with a dash", "ssh://host/-x/r", "", ""},
		{"control character", "https://github.com/o/r\x07", "", ""},
		{"newline", "https://github.com/o/r\nx", "", ""},
		{"nul", "https://github.com/o/r\x00", "", ""},
		{"bidi override", "https://github.com/o/r\u202e", "", ""},
		{"non ascii host", "https://g\u00efthub.com/o/r", "", ""},
		{"inner space", "ssh://host/o /r", "", ""},
		{"scp user with a bad character", "us er@github.com:o/r", "", ""},
		{"scp credentials in the path", "user:secret@github.com:o/r", "", ""},
		{"too long", "https://github.com/o/" + strings.Repeat("r", 1100), "", ""},
		{"bracket host in scp", "git@[::1]:o/r", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CleanRemote(tt.remote)

			if got != tt.clean || ok != (tt.clean != "") {
				t.Fatalf("CleanRemote = %q %v want %q", got, ok, tt.clean)
			}

			if slug := SlugOf(got); slug != tt.slug {
				t.Errorf("SlugOf(%q) = %q want %q", got, slug, tt.slug)
			}
		})
	}
}

// TestPickRemote checks the choice of the remote from git's output: origin, else the only one.
//
// Parameters:
//   - t: test to report to
func TestPickRemote(t *testing.T) {
	tests := []struct{ name, out, want string }{
		{"origin", "remote.origin.url https://github.com/o/r\n", "https://github.com/o/r"},
		{
			"origin wins",
			"remote.up.url https://github.com/a/b\nremote.origin.url git@github.com:o/r\n",
			"git@github.com:o/r",
		},
		{"only remote", "remote.upstream.url https://github.com/o/r\n", "https://github.com/o/r"},
		{
			"two without origin",
			"remote.a.url https://github.com/o/r\nremote.b.url https://github.com/o/s\n",
			"",
		},
		{"dotted name", "remote.my.fork.url https://github.com/o/r\n", "https://github.com/o/r"},
		{"refused url", "remote.origin.url file:///x\n", ""},
		{"garbage lines", "x\nremote.origin.pushurl https://github.com/o/r\n\n", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		if got := pickRemote([]byte(tt.out)); got != tt.want {
			t.Errorf("%s: got %q want %q", tt.name, got, tt.want)
		}
	}
}

// TestCleanBranch checks the validation of a branch name.
//
// Parameters:
//   - t: test to report to
func TestCleanBranch(t *testing.T) {
	tests := []struct{ in, want string }{
		{"main\n", "main"},
		{"feature/x-1", "feature/x-1"},
		{"", ""},
		{"-x", ""},
		{"a b", ""},
		{"a\tb", ""},
		{"a\x1bb", ""},
		{"a\xffb", ""},
		{strings.Repeat("a", 257), ""},
	}

	for _, tt := range tests {
		if got := cleanBranch(tt.in); got != tt.want {
			t.Errorf("cleanBranch(%q) = %q want %q", tt.in, got, tt.want)
		}
	}
}

// TestCurrentRepo checks that the current repository is the one whose resolved path contains the
// working directory, the longest path winning, on a segment boundary.
//
// Parameters:
//   - t: test to report to
func TestCurrentRepo(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")

	for _, d := range []string{
		"code/tote/sub/deep", "code/tote-extra", "code/tote/vendor/inner",
		"other",
	} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	linkTarget := filepath.Join(home, "code", "tote")
	if err := os.Symlink(linkTarget, filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}

	repos := []store.Repo{
		{Name: "tote", Path: filepath.Join(home, "code", "tote")},
		{Name: "extra", Path: "~/code/tote-extra"},
		{Name: "inner", Path: "~/code/tote/vendor/inner"},
		{Name: "gone", Path: "~/code/missing"},
		{Name: "rel", Path: "relative/path"},
		{Name: "linked", Path: "~/link"},
	}

	tests := []struct {
		name string
		cwd  string
		want string
	}{
		{"root", filepath.Join(home, "code", "tote"), "linked"},
		{"below", filepath.Join(home, "code", "tote", "sub", "deep"), "linked"},
		{"longest wins", filepath.Join(home, "code", "tote", "vendor", "inner"), "inner"},
		{"segment boundary", filepath.Join(home, "code", "tote-extra"), "extra"},
		{"through a link", filepath.Join(home, "link", "sub"), "linked"},
		{"outside", filepath.Join(home, "other"), ""},
		{"prefix only", filepath.Join(home, "code", "tot"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CurrentRepo(repos, tt.cwd, home)

			if tt.want == "" {
				if ok {
					t.Fatalf("found %q, want none", got.Name)
				}

				return
			}

			// "tote" and "linked" resolve to the same directory; the later record wins a tie.
			if !ok || (got.Name != tt.want && (tt.want != "linked" || got.Name != "tote")) {
				t.Errorf("got %q %v want %q", got.Name, ok, tt.want)
			}
		})
	}
}

// TestExpandRepoPath checks that a stored repository path must expand to an absolute, existing
// directory, with "~" read as the home.
//
// Parameters:
//   - t: test to report to
func TestExpandRepoPath(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{root, root, true},
		{"~", root, true},
		{"~/", root, true},
		{root + "/../" + filepath.Base(root), root, true},
		{"relative", "", false},
		{"~other", "", false},
		{root + "/file", "", false},
		{root + "/missing", "", false},
		{"", "", false},
		{root + "/\xff", "", false},
	}

	for _, tt := range tests {
		got, ok := ExpandRepoPath(tt.in, root)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ExpandRepoPath(%q) = %q %v want %q %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// FuzzCleanRemote checks that the cleaned remote never holds what the specification forbids and
// that the slug is either empty or two safe segments.
//
// Parameters:
//   - f: fuzz target
func FuzzCleanRemote(f *testing.F) {
	for _, s := range []string{
		"https://github.com/o/r", "git@github.com:o/r.git", "ext::x", "-x:y",
		"ssh://u:p@h/-x", "file:/x",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		got, ok := CleanRemote(raw)
		if !ok {
			return
		}

		if !printableASCII(got) || len(got) > maxRemoteBytes || strings.Contains(got, "@") &&
			strings.Contains(got, "://") {
			t.Fatalf("CleanRemote(%q) = %q", raw, got)
		}

		if slug := SlugOf(got); slug != "" {
			parts := strings.Split(slug, "/")
			if len(parts) != 2 || strings.HasPrefix(parts[0], "-") || strings.HasPrefix(parts[1], "-") {
				t.Fatalf("slug %q from %q", slug, raw)
			}
		}
	})
}
