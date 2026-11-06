package plugin

import (
	"regexp/syntax"
	"strings"
)

// riskyFlags lists, per program, the options that make it run another program or read an
// arbitrary configuration. A plugin that writes one literally gets a warning: no list can stop a
// program that runs other programs, and the consent view, which prints each command, is the real
// gate.
var riskyFlags = map[string][]string{
	"git":   {"-c", "--exec-path", "--config-env"},
	"ssh":   {"-o", "-e", "-F"},
	"scp":   {"-o", "-e", "-F"},
	"rsync": {"-o", "-e", "-F"},
	"sftp":  {"-o", "-e", "-F"},
	"curl":  {"-K", "--config"},
	"wget":  {"-K", "--config"},
	"tar":   {"--to-command", "--use-compress-program"},
}

// sshFamily lists the programs whose placeholders must come after a literal "--".
var sshFamily = []string{"ssh", "scp", "rsync", "sftp"}

// riskyFlag tells whether a literal argv element is one of the risky options of its program.
//
// Parameters:
//   - prog: name of the program at argv[0]
//   - elem: leading literal text of an element after argv[0]
//
// Returns the option that matched, or an empty string. A long option matches alone or followed
// by "="; a short option matches alone and, for every program but git, with its value attached
// (-oProxyCommand=x, -Kfile).
func riskyFlag(prog, elem string) string {
	for _, f := range riskyFlags[prog] {
		long := strings.HasPrefix(f, "--")

		switch {
		case elem == f:
			return f
		case long && strings.HasPrefix(elem, f+"="):
			return f
		case !long && prog != "git" && strings.HasPrefix(elem, f):
			return f
		}
	}

	return ""
}

// anchoredPattern tells whether a regular expression can match a whole value only: every
// alternative starts with ^ and ends with $. The check is made on the parsed expression, so
// ^a|b$, (?m)^a$ and ^a\$ are not anchored. A pattern is matched with MatchString, so an
// unanchored [0-9]+ would accept "1 --force".
//
// Parameters:
//   - pat: expression in the syntax of the standard library
//
// Returns false when the expression does not parse.
func anchoredPattern(pat string) bool {
	re, err := syntax.Parse(pat, syntax.Perl)
	if err != nil {
		return false
	}

	return startsAnchored(re) && endsAnchored(re)
}

// startsAnchored tells whether every match of an expression starts at the beginning of the text.
//
// Parameters:
//   - re: parsed expression
func startsAnchored(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpBeginText:
		return true
	case syntax.OpCapture, syntax.OpConcat:
		return len(re.Sub) > 0 && startsAnchored(re.Sub[0])
	case syntax.OpAlternate:
		return allSub(re, startsAnchored)
	}

	return false
}

// endsAnchored tells whether every match of an expression ends at the end of the text.
//
// Parameters:
//   - re: parsed expression
func endsAnchored(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpEndText:
		return true
	case syntax.OpCapture:
		return len(re.Sub) > 0 && endsAnchored(re.Sub[0])
	case syntax.OpConcat:
		return len(re.Sub) > 0 && endsAnchored(re.Sub[len(re.Sub)-1])
	case syntax.OpAlternate:
		return allSub(re, endsAnchored)
	}

	return false
}

// allSub tells whether a test holds for every sub-expression.
//
// Parameters:
//   - re: parsed expression
//   - test: predicate
func allSub(re *syntax.Regexp, test func(*syntax.Regexp) bool) bool {
	for _, s := range re.Sub {
		if !test(s) {
			return false
		}
	}

	return len(re.Sub) > 0
}
