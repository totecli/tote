package plugin

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Names copied from tote's environment when they are set, section 5.3 (1). The locale family is
// the fixed set of the C library: the environment is never enumerated, so an unknown LC_ variable
// cannot pass.
var (
	copiedEnv = []string{
		"USER", "LOGNAME", "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_COLLATE", "LC_MESSAGES",
		"LC_MONETARY", "LC_NUMERIC", "LC_TIME", "LC_PAPER", "LC_NAME", "LC_ADDRESS",
		"LC_TELEPHONE", "LC_MEASUREMENT", "LC_IDENTIFICATION", "TERM", "COLORTERM", "NO_COLOR", "TZ",
		"TMPDIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
	}
	proxyEnv = []string{
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy",
		"no_proxy", "all_proxy",
	}
	toteDirEnv = []string{"TOTE_CONFIG_DIR", "TOTE_DATA_DIR", "TOTE_STATE_DIR", "TOTE_CACHE_DIR"}
)

// Smallest secret length that the exact-value redaction replaces: an environment value declared
// with env: and an entry of an env table.
const (
	minDeclaredSecret = 4
	minTableSecret    = 8
)

// envPlan is what the environment of one run is built from.
//
// Table holds the entry's env table after substitution. Dir is the working directory of the run.
// Captured is true for pager and silent runs, which get GIT_TERMINAL_PROMPT=0. Mono sets NO_COLOR.
// Lang is the language of the interface.
type envPlan struct {
	Table    map[string]string
	Lang     string
	Captured bool
	Mono     bool
}

// scrubPath removes from a PATH value the entries that a lookup must not use: empty entries,
// relative entries, the places Avoid names, and the working directory itself.
//
// Parameters:
//   - pathEnv: value of PATH
//   - avoid: places no program may be found in
//
// Returns the entries joined by the list separator, cleaned, in their order.
func scrubPath(pathEnv string, avoid Avoid) string {
	avoid = avoid.cleaned()

	var keep []string

	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}

		dir = filepath.Clean(dir)

		if underAny(dir, avoid.Under) {
			continue
		}

		if isIn(dir, avoid.Exactly) {
			continue
		}

		keep = append(keep, dir)
	}

	return strings.Join(keep, string(filepath.ListSeparator))
}

// isIn reports whether a string is in a list.
//
// Parameters:
//   - s: text to find
//   - list: candidates
func isIn(s string, list []string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}

	return false
}

// buildEnv builds the environment of a plugin program from nothing, section 5.3: what is copied
// from tote's own environment, tote's directories, the variables tote sets, each env: permission
// that is set, then the env table of the entry.
//
// Parameters:
//   - p: plugin with a verified manifest
//   - pathEnv: PATH of tote
//   - avoid: places a program may not be found in, which PATH loses
//   - plan: per-run choices
//
// Returns the KEY=value entries sorted by name, and the values the output must be redacted of.
func (r *Registry) buildEnv(p Plugin, pathEnv string, avoid Avoid,
	plan envPlan,
) ([]string, []string) {
	o := r.opts
	env := map[string]string{}

	put := func(name, value string) {
		if validEnvName(name) && !strings.ContainsRune(value, 0) {
			env[name] = value
		}
	}

	copyVar := func(name string) {
		if v, ok := r.lookup(name); ok {
			put(name, v)
		}
	}

	if path := scrubPath(pathEnv, avoid); path != "" {
		put("PATH", path)
	}

	home, ok := r.lookup("HOME")
	if !ok || home == "" {
		home = o.Home
	}

	put("HOME", home)

	for _, name := range copiedEnv {
		copyVar(name)
	}

	perms := declaredPerms(p.Manifest)

	if perms.net {
		for _, name := range proxyEnv {
			copyVar(name)
		}
	}

	for _, name := range toteDirEnv {
		copyVar(name)
	}

	if plan.Mono {
		put("NO_COLOR", "1")
	}

	r.toteVars(p, plan, put)

	var secrets []string

	for _, name := range perms.env {
		if BlockedEnvName(name) {
			continue
		}

		if v, ok := r.lookup(name); ok {
			put(name, v)

			if len(v) >= minDeclaredSecret {
				secrets = append(secrets, v)
			}
		}
	}

	for _, name := range sortedKeys(plan.Table) {
		if BlockedEnvName(name) {
			continue
		}

		put(name, plan.Table[name])

		if len(plan.Table[name]) >= minTableSecret {
			secrets = append(secrets, plan.Table[name])
		}
	}

	return flattenEnv(env), secrets
}

// toteVars sets the variables tote defines for every plugin program.
//
// Parameters:
//   - p: plugin
//   - plan: per-run choices
//   - put: setter of the environment being built
func (r *Registry) toteVars(p Plugin, plan envPlan, put func(name, value string)) {
	o := r.opts

	put("TOTE_API", "1")
	put("TOTE_HOST_VERSION", o.Host.Version)
	put("TOTE_LANG", plan.Lang)
	put("TOTE_BIN", o.Bin)
	put("TOTE_PLUGIN_ID", p.ID)
	put("TOTE_PLUGIN_DIR", p.Dir)
	put("PYTHONDONTWRITEBYTECODE", "1")

	if dir, err := o.Paths.PluginStateDir(p.ID); err == nil {
		put("TOTE_PLUGIN_STATE", dir)
	}

	if dir, err := o.Paths.PluginCacheDir(p.ID); err == nil {
		put("TOTE_PLUGIN_CACHE", dir)
	}

	if plan.Captured {
		put("GIT_TERMINAL_PROMPT", "0")
	}
}

// lookup reads one variable of tote's environment.
//
// Parameters:
//   - name: variable name
func (r *Registry) lookup(name string) (string, bool) {
	if r.opts.Lookup != nil {
		return r.opts.Lookup(name)
	}

	return os.LookupEnv(name)
}

// permSummary is what the environment builder reads from the permissions of a manifest.
type permSummary struct {
	env []string
	net bool
}

// declaredPerms extracts the env: names and the presence of a net: entry from the declared
// permissions.
//
// Parameters:
//   - m: manifest
func declaredPerms(m *Manifest) permSummary {
	var s permSummary

	for _, perm := range m.Permissions {
		switch {
		case strings.HasPrefix(perm, PermEnv+":"):
			s.env = append(s.env, strings.TrimPrefix(perm, PermEnv+":"))
		case strings.HasPrefix(perm, PermNet+":"):
			s.net = true
		}
	}

	return s
}

// validEnvName checks a variable name that tote itself sets or copies: letters, digits and
// underscore, not starting with a digit, at most 64 bytes, lower case allowed for the proxy
// family.
//
// Parameters:
//   - name: variable name
func validEnvName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}

	for i := 0; i < len(name); i++ {
		c := name[i]
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'

		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}

	return true
}

// flattenEnv turns a map into sorted KEY=value entries.
//
// Parameters:
//   - env: variables by name
func flattenEnv(env map[string]string) []string {
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}

	sort.Strings(names)

	out := make([]string, len(names))
	for i, k := range names {
		out[i] = k + "=" + env[k]
	}

	return out
}
