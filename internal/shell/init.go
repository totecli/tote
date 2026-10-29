package shell

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/totecli/tote/internal/store"
)

// ErrUnsupported is wrapped by the error of ParseKind and DetectKind for a shell other than bash
// and zsh.
var ErrUnsupported = errors.New("unsupported shell")

// Kind names a shell that tote can generate code for.
type Kind string

const (
	// Bash is the GNU Bourne Again shell, version 3.2 or later.
	Bash Kind = "bash"

	// Zsh is the Z shell.
	Zsh Kind = "zsh"
)

// binaryName is the name of the program and of the wrapper function.
const binaryName = "tote"

// defsVar is the shell variable that remembers what the last definitions created, so that a
// reload can remove the aliases and functions that the user deleted in the meantime.
const defsVar = "__TOTE_DEFS"

// reservedNames lists the names that no alias, repository, function or extra name may take: the
// wrapper itself, the shell words that the generated code relies on, the commands and helper
// functions that it runs, and the reserved words of bash and zsh, which a function definition
// cannot be named after. The helper functions are also reserved by prefix, see Reserved.
var reservedNames = map[string]bool{
	binaryName: true, "source": true, "eval": true, "command": true, "builtin": true,
	"alias": true, "unalias": true, "unset": true, "local": true, "read": true,
	"history": true, "umask": true, "exit": true, "return": true, "emulate": true,
	":": true, ".": true, "set": true, "printf": true, "complete": true, "compdef": true,
	"_describe": true, "_tote": true, "__tote_unload": true, "__tote_complete": true,
	"if": true, "then": true, "else": true, "elif": true, "fi": true, "case": true,
	"esac": true, "for": true, "select": true, "while": true, "until": true, "do": true,
	"done": true, "in": true, "function": true, "time": true, "coproc": true,
	"repeat": true, "foreach": true, "end": true, "always": true, "nocorrect": true,
}

// Reserved tells whether a name belongs to the program or to the shell and so cannot be used for
// an alias, a repository, a function or an extra name.
//
// The names of reservedNames are exact. The prefixes __tote_ and _tote are reserved whole, see
// store.ReservedPrefix, so that a helper added later never meets a record of that name.
//
// Parameters:
//   - name: candidate name
func Reserved(name string) bool {
	return reservedNames[name] || store.ReservedPrefix(name)
}

// ValidScript tells whether the script path of a source function is safe to emit: an absolute
// path, "~", or a path under "~/". A bare file name would be searched in PATH and then in the
// current directory by "source", which could run a file of an untrusted checkout.
//
// Parameters:
//   - script: path as stored
func ValidScript(script string) bool {
	return AbsOrHome(script)
}

// AbsOrHome tells whether a stored path does not depend on the current directory: an absolute
// path, "~", or a path under "~/".
//
// Parameters:
//   - path: path as stored
func AbsOrHome(path string) bool {
	return path == "~" || strings.HasPrefix(path, "~/") || filepath.IsAbs(path)
}

// ExpandHome replaces a leading "~" or "~/" of a stored path by the home directory.
//
// Parameters:
//   - path: path as stored, never passed to a shell
//   - home: home directory, empty to leave the path alone
func ExpandHome(path, home string) string {
	if home != "" && (path == "~" || strings.HasPrefix(path, "~/")) {
		return home + path[1:]
	}

	return path
}

// Defs is everything that the definitions part of the init output is made of.
//
// Aliases and Repos become aliases, the Funcs of kind source become functions that source their
// script, the Funcs of kind exec only reserve their name because they live in BinDir, which is
// added to PATH. ExtraNames lists further names (for example "mn") that become functions
// calling the wrapper.
type Defs struct {
	Aliases    []store.Alias
	Repos      []store.Repo
	Funcs      []store.Func
	BinDir     string
	ExtraNames []string
}

// ParseKind converts a shell name into a Kind.
//
// Parameters:
//   - name: "bash" or "zsh", nothing else
//
// Returns the Kind, or an error wrapping ErrUnsupported.
func ParseKind(name string) (Kind, error) {
	switch k := Kind(name); k {
	case Bash, Zsh:
		return k, nil
	}

	return "", fmt.Errorf("%w: %q (want bash or zsh)", ErrUnsupported, name)
}

// DetectKind finds the shell kind from the path of the user shell.
//
// Parameters:
//   - shellPath: value of $SHELL; only its base name is used, and the leading '-' of a login
//     shell name is ignored
//
// Returns the Kind, or an error wrapping ErrUnsupported.
func DetectKind(shellPath string) (Kind, error) {
	return ParseKind(strings.TrimPrefix(filepath.Base(shellPath), "-"))
}

// Init returns the whole output of "tote init": the wrapper, then the definitions.
//
// Parameters:
//   - k: shell to write for
//   - d: records and settings to turn into shell code
//
// Returns the text, or an empty string for an unknown kind.
func Init(k Kind, d Defs) string {
	return Wrapper(k) + Definitions(k, d)
}

// emitter accumulates the definitions text and remembers the names already taken.
//
// The names are shared by aliases, functions and executables because a shell resolves them in
// one order. seen holds every name claimed so far, defs the "a:" and "f:" entries of the
// tracking variable, and names the commands that get a completion: tote and the extra names.
type emitter struct {
	out   strings.Builder
	seen  map[string]bool
	defs  []string
	names []string
}

// Definitions returns the aliases, the source functions, the PATH entry, the completion
// registration and the tracking variable, as shell code for k.
//
// Every value goes through Quote. A record that is invalid, reserved or a duplicate is replaced
// by a comment line such as "# tote: alias 'x' skipped (invalid name)" and never stops the
// records after it. Names defined by the previous evaluation of the same text are removed first,
// so the output can be evaluated again to reload.
//
// Parameters:
//   - k: shell to write for
//   - d: records and settings to turn into shell code
//
// Returns the text ending with a newline, or an empty string for an unknown kind.
func Definitions(k Kind, d Defs) string {
	if k != Bash && k != Zsh {
		return ""
	}

	e := &emitter{seen: map[string]bool{}, names: []string{binaryName}}
	e.out.WriteString(unloadText(k))

	e.aliases(d.Aliases)
	e.repos(d.Repos)
	e.funcs(d.Funcs)
	e.extras(d.ExtraNames)
	e.path(d.BinDir)
	e.out.WriteString(completionText(k, e.names))
	fmt.Fprintf(&e.out, "%s=%s\n", defsVar, Quote(strings.Join(e.defs, " ")))

	return e.out.String()
}

// aliases emits one alias line per valid alias record.
//
// Parameters:
//   - list: alias records in file order
func (e *emitter) aliases(list []store.Alias) {
	for _, a := range list {
		if e.admit("alias", a.Name, store.ValidAliasName(a.Name), a.Validate()) {
			e.alias(a.Name, a.Cmd)
		}
	}
}

// repos emits one alias line per valid repository record; the alias runs "tote repo open".
//
// Parameters:
//   - list: repository records in file order
func (e *emitter) repos(list []store.Repo) {
	for _, r := range list {
		if e.admit("repo", r.Name, store.ValidRepoName(r.Name), r.Validate()) {
			e.alias(r.Name, binaryName+" repo open "+quoteIfNeeded(r.Name))
		}
	}
}

// funcs emits a function for each valid source record and reserves the name of each exec one.
//
// Parameters:
//   - list: function records in file order
func (e *emitter) funcs(list []store.Func) {
	for _, f := range list {
		if f.Kind == store.KindExec {
			e.reserveExec(f)

			continue
		}

		if !e.admit("func", f.Name, store.ValidFuncName(f.Name), sourceErr(f)) {
			continue
		}

		e.function(f.Name, "source "+scriptWord(f.Script)+` "$@"`)
	}
}

// sourceErr validates a function record of kind source, script path included.
//
// Parameters:
//   - f: function record
//
// Returns nil when the record may produce code.
func sourceErr(f store.Func) error {
	if err := f.Validate(); err != nil {
		return err
	}

	if !ValidScript(f.Script) {
		return fmt.Errorf("%w: script must be an absolute path or start with ~/", store.ErrInvalidField)
	}

	return nil
}

// reserveExec marks the name of an executable as taken without emitting anything, because the
// file itself is found through PATH. A record that is invalid, reserved or a duplicate leaves a
// comment line like any other.
//
// Parameters:
//   - f: function record of kind exec
func (e *emitter) reserveExec(f store.Func) {
	e.admit("func", f.Name, store.ValidFuncName(f.Name), f.Validate())
}

// extras emits a wrapper alias function for each extra name that is allowed.
//
// Parameters:
//   - names: extra function names from the settings
func (e *emitter) extras(names []string) {
	for _, n := range names {
		if e.admit("extra name", n, store.ValidFuncName(n), nil) {
			e.function(n, binaryName+` "$@"`)
			e.names = append(e.names, n)
		}
	}
}

// admit decides whether a record may produce code, and claims its name when it may.
//
// Parameters:
//   - kind: label used in the comment line of a skipped record
//   - name: name of the record
//   - nameErr: result of the name validator for this kind of record
//   - err: result of the whole record validation, nil when the kind has none
//
// Returns true when the name is claimed; otherwise a comment line has been written.
func (e *emitter) admit(kind, name string, nameErr, err error) bool {
	switch {
	case nameErr != nil:
		e.skip(kind, name, "invalid name")
	case err != nil:
		e.skip(kind, name, "invalid value")
	case Reserved(name):
		e.skip(kind, name, "reserved name")
	case e.seen[name]:
		e.skip(kind, name, "duplicate name")
	default:
		e.seen[name] = true

		return true
	}

	return false
}

// skip writes the comment line of a refused record.
//
// Parameters:
//   - kind: label of the record kind
//   - name: name of the record, shown with every unsafe byte replaced
//   - why: short reason
func (e *emitter) skip(kind, name, why string) {
	fmt.Fprintf(&e.out, "# tote: %s '%s' skipped (%s)\n", kind, printable(name), why)
}

// alias writes an alias line and records it in the tracking list.
//
// Parameters:
//   - name: validated alias name
//   - cmd: command line the alias expands to, quoted here
func (e *emitter) alias(name, cmd string) {
	fmt.Fprintf(&e.out, "alias %s=%s\n", quoteIfNeeded(name), Quote(cmd))
	e.defs = append(e.defs, "a:"+name)
}

// function writes a function definition and records it in the tracking list. An alias of the
// same name would turn the definition into a syntax error, so it is removed on the line before.
//
// Parameters:
//   - name: validated function name
//   - body: command text of the function, already quoted where needed
func (e *emitter) function(name, body string) {
	fmt.Fprintf(&e.out, "unalias -- %s 2>/dev/null || :\n%s() { %s; }\n", name, name, body)
	e.defs = append(e.defs, "f:"+name)
}

// scriptWord quotes the path of a script for a source command. A leading "~/" is the only part
// that stays unquoted, as "$HOME", because a quoted tilde would never expand.
//
// Parameters:
//   - script: path as stored, a single line
func scriptWord(script string) string {
	if script == "~" {
		return `"$HOME"`
	}

	if rest, ok := strings.CutPrefix(script, "~/"); ok {
		return `"$HOME"/` + Quote(rest)
	}

	return Quote(script)
}

// path writes the line that puts the script directory in front of PATH once.
//
// Parameters:
//   - dir: absolute directory without ':' or newline, empty for none
func (e *emitter) path(dir string) {
	if dir == "" {
		return
	}

	if !filepath.IsAbs(dir) || strings.ContainsAny(dir, ":\n\x00") {
		e.skip("bin dir", dir, "invalid path")

		return
	}

	q := Quote(dir)
	fmt.Fprintf(&e.out, "case \":$PATH:\" in *:%s:*) ;; *) PATH=%s\":$PATH\" ;; esac\n", q, q)
}

// printable makes a name safe to show in a comment line.
//
// Parameters:
//   - s: name to show
//
// Returns at most 64 bytes of s where everything but printable ASCII and the single quote is
// kept and the rest is replaced by '?'.
func printable(s string) string {
	const limit = 64

	b := make([]byte, 0, limit)

	for i := 0; i < len(s) && len(b) < limit; i++ {
		c := s[i]
		if c < 0x20 || c > 0x7e || c == '\'' {
			c = '?'
		}

		b = append(b, c)
	}

	return string(b)
}
