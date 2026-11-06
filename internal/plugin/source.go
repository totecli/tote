package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// shorthandRe matches owner/repo@tag, the shorthand of a release.
var shorthandRe = regexp.MustCompile(`^[A-Za-z0-9._+-]+/[A-Za-z0-9._+-]+@[A-Za-z0-9._+-]+$`)

// sourceSpec is an install source after classification.
//
// Kind is KindLocal, KindBundled or KindRelease. Text is the source as the user wrote it. Path is
// the absolute, cleaned directory or archive file of a local source and ID the bare id of a
// bundled one. Archive tells that a local Path is an archive file. Ref is the asset of a release
// and Origin its lower case owner/repo. Pin is the digest the archive must have, lower case,
// empty for none.
type sourceSpec struct {
	Kind    string
	Text    string
	Path    string
	ID      string
	Archive bool
	Ref     ReleaseRef
	Origin  string
	Pin     string
}

// localPrefixes are the spellings that make a source a path: anything else is an id, a URL or
// the owner/repo@tag shorthand, so that a bare word is never taken for a directory.
var localPrefixes = []string{"/", "./", "../", "~/"}

// isLocalPath reports whether a source is written as a path.
//
// Parameters:
//   - text: source as typed
func isLocalPath(text string) bool {
	if text == "." || text == ".." || text == "~" {
		return true
	}

	for _, p := range localPrefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}

	return false
}

// classify decides what kind of source a text is and, for a path, resolves it.
//
// A path starts with a slash, "./", "../" or "~/"; a bare id is a bundled plugin; a URL or the
// owner/repo@tag shorthand is a release, and a file at a path is an archive. A URL that is not a
// release URL of github.com is bad_source. Nothing is read from the source here beyond one stat.
//
// Parameters:
//   - text: source as typed
//
// Returns the specification, or an *Error: bad_source for text that is none of the forms or a
// path that is not there.
func (r *Registry) classify(text string) (sourceSpec, error) {
	sp := sourceSpec{Text: text}

	switch {
	case text == "" || len(text) > maxSourceBytes || !utf8.ValidString(text) ||
		strings.IndexFunc(text, isControl) >= 0:
		return sp, newError(CodeBadSource, text)
	case isLocalPath(text):
		return r.classifyLocal(sp)
	case strings.Contains(text, "://") || shorthandRe.MatchString(text):
		ref, err := ParseReleaseSource(text)
		if err != nil {
			return sp, err
		}

		return releaseSpec(sp, ref), nil
	case validID(text):
		sp.Kind, sp.ID = KindBundled, text

		return sp, nil
	}

	return sp, newError(CodeBadSource, text)
}

// classifyLocal resolves a path source to a directory or an archive file.
//
// Parameters:
//   - sp: specification whose Text is a path
func (r *Registry) classifyLocal(sp sourceSpec) (sourceSpec, error) {
	sp, err := r.classifyPath(sp)

	var pe *Error
	if errors.As(err, &pe) && pe.Code == CodeSourceUnsupported && sp.Archive {
		return sp, nil
	}

	return sp, err
}

// classifyPath resolves a path source to an absolute directory or archive file. An archive gives
// source_unsupported with Archive set: Check does not read archives, an install does.
//
// Parameters:
//   - sp: specification whose Text is a path
func (r *Registry) classifyPath(sp sourceSpec) (sourceSpec, error) {
	path := expandHome(sp.Text, r.opts.Home)

	if !filepath.IsAbs(path) {
		cwd := r.opts.Cwd

		if cwd == "" {
			wd, err := os.Getwd()
			if err != nil {
				return sp, wrapError(CodeBadSource, err, sp.Text)
			}

			cwd = wd
		}

		path = filepath.Join(cwd, path)
	}

	sp.Path = filepath.Clean(path)

	info, err := os.Stat(sp.Path)

	switch {
	case err != nil:
		return sp, wrapError(CodeBadSource, err, sp.Text)
	case info.IsDir():
		sp.Kind = KindLocal

		return sp, nil
	case info.Mode().IsRegular():
		sp.Kind, sp.Archive = KindLocal, true

		return sp, newError(CodeSourceUnsupported, "archive", sp.Text)
	}

	return sp, newError(CodeBadSource, sp.Text)
}
