package main

import (
	"regexp"
	"strings"
)

// urlPattern matches an address with a scheme inside a text.
var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://[^\s"'<>]+`)

// scrubURLs hides what an address typed by the user may hold that is secret, before the text is
// shown or written to a JSON document: the user and password of the authority, the query and the
// fragment. Only scheme://host/path is kept.
//
// Parameters:
//   - text: any text, possibly with addresses in it; a text without "://" is returned as is
//
// Returns the text with each address reduced. The address is cut by hand and not parsed, so that
// a malformed one is reduced too instead of being printed whole.
func scrubURLs(text string) string {
	if !strings.Contains(text, "://") {
		return text
	}

	return urlPattern.ReplaceAllStringFunc(text, func(raw string) string {
		scheme, rest, _ := strings.Cut(raw, "://")

		if i := strings.IndexAny(rest, "?#"); i >= 0 {
			rest = rest[:i]
		}

		host, path := rest, ""
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			host, path = rest[:i], rest[i:]
		}

		if i := strings.LastIndexByte(host, '@'); i >= 0 {
			host = host[i+1:]
		}

		return scheme + "://" + host + path
	})
}
