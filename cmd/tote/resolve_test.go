package main

import (
	"strings"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// TestReviewResolvePrecedence checks the order of the tiers: an exact name beats an exact alias
// of another connection, which beats a prefix, and the exact tiers try the case as typed before
// they ignore it.
//
// Parameters:
//   - t: test to report to
func TestReviewResolvePrecedence(t *testing.T) {
	conns := []store.Connection{
		{Name: "other", Aliases: []string{"web"}},
		{Name: "web"},
		{Name: "web2"},
		{Name: "db", Aliases: []string{"dbx"}},
		{Name: "db2"},
	}

	for _, tt := range []struct {
		handle string
		want   []string
	}{
		{"web", []string{"web"}},
		{"db", []string{"db"}},
		{"dbx", []string{"db"}},
		{"WEB", []string{"web"}},
		{"DBX", []string{"db"}},
		{"we", []string{"other", "web", "web2"}},
		{"DB2", []string{"db2"}},
		{"w", nil},
		{"zz", nil},
		{"", nil},
	} {
		var got []string
		for _, c := range resolveConn(conns, tt.handle, 2) {
			got = append(got, c.Name)
		}

		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("resolveConn(%q) = %v, want %v", tt.handle, got, tt.want)
		}
	}
}

// TestReviewResolveNonASCIIPrefix checks that a prefix cut inside a multibyte character of the
// handle does not match and does not panic.
//
// Parameters:
//   - t: test to report to
func TestReviewResolveNonASCIIPrefix(t *testing.T) {
	conns := []store.Connection{{Name: "web"}}

	for _, handle := range []string{"\xc3", "w\xc3\xa9", "\u00e9\u00e9", "webwebweb"} {
		if got := resolveConn(conns, handle, 1); len(got) != 0 {
			t.Errorf("resolveConn(%q) = %v, want nothing", handle, got)
		}
	}
}

// TestReviewRepoOpenHostilePath checks that a path read from a file that was edited by hand
// never reaches the terminal with an escape sequence in it.
//
// Parameters:
//   - t: test to report to
func TestReviewRepoOpenHostilePath(t *testing.T) {
	h := newHarness(t)
	h.write("repos.toml", "schema = 1\n\n[[repo]]\nname = \"evil\"\n"+
		"path = \"/srv/\\u001b]0;pwned\\u0007x\\u202ey\"\n")

	h.run("repos", "open", "evil")

	if strings.ContainsAny(h.out.String(), "\x1b\x07\u202e") {
		t.Errorf("raw control characters in stdout %q", h.out.String())
	}
}
