package plugin

import (
	"regexp"
	"sort"
	"strings"
)

// Mask replaces a secret in the text of a plugin.
const Mask = "****"

// maxPEMLines bounds a PEM block that the redactor hides line by line, so that an unterminated
// header cannot hide the rest of a stream.
const maxPEMLines = 200

// tokenShapes are the shapes of well-known credentials, a best-effort net behind the exact
// values. Each match is replaced whole, except the Bearer scheme word and the URL scheme, which
// stay.
var tokenShapes = []*regexp.Regexp{
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{30,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`glpat-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`AIza[0-9A-Za-z_-]{30,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{24,}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
}

// The shapes that are not a plain token: a scheme word and its value, credentials in a url, and
// the two ends of a PEM private key block.
var (
	bearerShape   = regexp.MustCompile(`(?i)(bearer|token)(\s+)[A-Za-z0-9._~+/=-]{16,}`)
	userinfoShape = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/@:]*:[^\s/@]+@`)
	pemBegin      = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
	pemEnd        = regexp.MustCompile(`-----END [A-Z0-9 ]*PRIVATE KEY-----`)
)

// Redactor hides secrets in the text a program wrote: first the exact values tote knows (the
// environment it handed over), then the shapes of common credentials, and whole PEM private key
// blocks. It keeps the state of a PEM block between lines, so one Redactor serves one stream and
// is not safe for concurrent use.
type Redactor struct {
	exact  *strings.Replacer
	maxLen int
	inPEM  int
}

// NewRedactor creates a redactor for a list of exact secret values.
//
// Parameters:
//   - values: secrets to hide wherever they appear; empty values are ignored, a value with line
//     breaks is hidden line by line (a line shorter than 4 bytes is left alone, since it would
//     mask ordinary text), the longest values are tried first
func NewRedactor(values []string) *Redactor {
	seen := map[string]bool{}

	var list []string

	add := func(v string) {
		if len(v) >= minDeclaredSecret && !seen[v] {
			seen[v] = true

			list = append(list, v)
		}
	}

	for _, v := range values {
		add(v)

		if strings.ContainsAny(v, "\r\n") {
			for _, line := range strings.FieldsFunc(v, isLineBreak) {
				add(line)
			}
		}
	}

	sort.SliceStable(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })

	rd := &Redactor{}

	if len(list) > 0 {
		pairs := make([]string, 0, 2*len(list))
		for _, v := range list {
			pairs = append(pairs, v, Mask)
		}

		rd.exact = strings.NewReplacer(pairs...)
		rd.maxLen = len(list[0])
	}

	return rd
}

// MaxSecretLen returns the length of the longest exact value, which a caller that cuts a stream
// in the middle of a line keeps as overlap.
func (rd *Redactor) MaxSecretLen() int {
	if rd == nil {
		return 0
	}

	return rd.maxLen
}

// Line redacts one line of a stream. A PEM private key block is hidden from its header to its
// footer, over as many calls as it takes, at most 200 lines.
//
// Parameters:
//   - s: line without its line break
func (rd *Redactor) Line(s string) string {
	if rd == nil {
		return s
	}

	if rd.inPEM > 0 {
		rd.inPEM--

		if pemEnd.MatchString(s) {
			rd.inPEM = 0
		}

		return Mask
	}

	if loc := pemBegin.FindStringIndex(s); loc != nil {
		rd.inPEM = maxPEMLines

		if pemEnd.MatchString(s[loc[1]:]) {
			rd.inPEM = 0
		}

		return Mask
	}

	return rd.Scrub(s)
}

// Scrub redacts the exact values and the credential shapes of a piece of text, without the memory
// of a PEM block that Line has.
//
// Parameters:
//   - s: text to hide secrets in
func (rd *Redactor) Scrub(s string) string {
	if rd == nil {
		return s
	}

	if rd.exact != nil {
		s = rd.exact.Replace(s)
	}

	if !shapeCandidate(s) {
		return s
	}

	for _, re := range tokenShapes {
		s = re.ReplaceAllString(s, Mask)
	}

	s = bearerShape.ReplaceAllString(s, "${1}${2}"+Mask)

	return userinfoShape.ReplaceAllString(s, "${1}"+Mask+"@")
}

// shapeAnchors are literal fragments that every shape in tokenShapes contains, so that a line
// with none of them can skip the regular expressions. The bearer, token and URL shapes are
// covered by shapeCandidate itself.
var shapeAnchors = []string{
	"gh", "github_pat_", "AKIA", "AIza", "sk-", "xox", "eyJ", "glpat-", "://",
}

// shapeCandidate reports whether a piece of text may hold a credential shape. A false answer is
// certain; a true one only means that the expressions must look. It keeps the cost of a large
// plain output linear with a small constant.
//
// Parameters:
//   - s: text to look at
func shapeCandidate(s string) bool {
	for _, a := range shapeAnchors {
		if strings.Contains(s, a) {
			return true
		}
	}

	for i := 0; i < len(s); i++ {
		switch s[i] | 0x20 {
		case 'b':
			if i+6 <= len(s) && strings.EqualFold(s[i:i+6], "bearer") {
				return true
			}
		case 't':
			if i+5 <= len(s) && strings.EqualFold(s[i:i+5], "token") {
				return true
			}
		}
	}

	return false
}

// Text redacts a text of several lines, with the PEM state of a fresh stream.
//
// Parameters:
//   - s: text to hide secrets in
func (rd *Redactor) Text(s string) string {
	if rd == nil {
		return s
	}

	rd.inPEM = 0

	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = rd.Line(l)
	}

	rd.inPEM = 0

	return strings.Join(lines, "\n")
}

// isLineBreak reports whether a rune ends a line.
//
// Parameters:
//   - r: rune to test
func isLineBreak(r rune) bool { return r == '\n' || r == '\r' }
