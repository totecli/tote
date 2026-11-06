package plugin

import (
	"slices"
	"strings"
)

// coreVerbs lists every name the command line of tote answers to, hidden verbs included. A test
// compares it with commands() in cmd/tote/help.go so that a new verb cannot be forgotten.
var coreVerbs = []string{
	"help", "version", "init", "co", "conn", "connections", "connexions", "alias", "aliases", "a",
	"repos", "repo", "r", "funcs", "func", "f", "config", "plugin", "plugins", "__complete",
}

// plannedVerbs lists the verbs and words that the roadmap announces for later releases.
var plannedVerbs = []string{
	"update", "upgrade", "uninstall", "doctor", "edit", "import", "find",
	"status", "refresh", "sync", "restore", "keep", "pick", "complete", "completion", "man",
	"tunnel", "tunnels", "snippets", "proc", "procs", "process", "processes", "ports", "history",
	"recent", "stats",
}

// reservedWords lists the names that would pass for tote itself or for a source of plugins.
var reservedWords = []string{
	"tote", "mn", "core", "host", "builtin", "bundled", "registry", "all", "none",
}

// recordFields lists the field names of the schema-1 records. They are reserved so that an
// extension key added in a later release cannot collide with an installed plugin. A later
// reservation never breaks a plugin that is already installed: the reservations are applied when a
// plugin is checked, installed or updated, and not when an installed one is loaded
// (DecodeOptions.Installed).
var recordFields = []string{
	"name", "via", "target", "user", "aliases", "ip", "desc", "needs_review", "path", "cmd", "kind",
	"script", "managed", "schema", "tags", "fav", "hidden", "port", "key", "jump", "mac", "attach",
	"session", "mode", "when", "remote", "opener", "scheme", "start", "health", "cwd",
}

// officialIDs lists the ids that only the copy shipped with tote, or the official release of that
// id (the repository totecli/<id>), may use.
var officialIDs = []string{"gft", "ghrel", "ghsec", "konsolai", "ssh-tools"}

// isOfficialOrigin tells whether a release comes from the official repository of a plugin. The
// origin must equal "totecli/" followed by the id, and the id must be one of the official ids:
// totecli/ghrel is not official for the id gft, totecli/registry is official for nothing, and the
// former name totecli/plugin-gft no longer is. There is no folding of case, no trailing slash and
// no ".git": an origin is the lower case owner/repo that ReleaseRef.Origin produces, and anything
// else is simply not equal.
//
// Parameters:
//   - origin: lower case owner/repo of a release, empty for a plugin that is not a release
//   - id: id of the plugin concerned, taken from the manifest that was read or from the install
//     request, never from the origin text
func isOfficialOrigin(origin, id string) bool {
	return slices.Contains(officialIDs, id) && origin == officialOwner+id
}

// Reasons returned by ReservedID.
const (
	ReservedVerb     = "verb"
	ReservedWord     = "word"
	ReservedPrefix   = "prefix"
	ReservedField    = "field"
	ReservedOfficial = "official"
)

// ReservedID tells whether a plugin id is reserved.
//
// Parameters:
//   - id: candidate id, already valid under config.ValidPluginID
//
// Returns the reason (ReservedVerb for a command line verb, ReservedWord for a name that would
// pass for tote, ReservedPrefix for any id starting with "tote-", ReservedField for a record field
// name, ReservedOfficial for an id reserved to the official plugins) or an empty string. The
// official ids are usable by the bundled copy and by the release of totecli/<id>: the caller
// decides, this function only reports them.
func ReservedID(id string) string {
	switch {
	case slices.Contains(coreVerbs, id) || slices.Contains(plannedVerbs, id):
		return ReservedVerb
	case slices.Contains(reservedWords, id):
		return ReservedWord
	case strings.HasPrefix(id, "tote-"):
		return ReservedPrefix
	case slices.Contains(recordFields, id):
		return ReservedField
	case slices.Contains(officialIDs, id):
		return ReservedOfficial
	}

	return ""
}

// reservedTables lists the top-level tables that API 1 reserves for later releases. A manifest
// that declares one gets reserved_not_available, so that nobody ships a feature that does nothing.
var reservedTables = []string{
	"widget", "panel", "decorator", "provider", "connector", "hook", "job", "screen", "import",
	"badge", "search",
}

// execTierAvailable is the switch of the exec tier. tote 1.0 ships the declarative tier only: while
// it is false, a manifest of tier exec is refused with reserved_not_available (probeTier), and
// the features exec.list and exec.run are neither announced in host.features nor in `plugin check`.
// The exec implementation (the runner, the protocol, the [exec] and [[list]] reading) stays in
// place, so that turning the switch on is all it takes to bring the tier back in 1.1.
var execTierAvailable = false

// execFeatures lists the host features that only exist with the exec tier.
var execFeatures = []string{"exec.list", "exec.run"}

// announcedFeatures builds the list of announced features.
//
// Parameters:
//   - exec: true when the exec tier is available
//
// Returns the features of the declarative tier, in the order of the specification, followed by
// the ones of the exec tier when it is available.
func announcedFeatures(exec bool) []string {
	list := []string{
		"command", "item_action.repo", "setting.list", "field.bool", "open.url", "output.terminal",
	}

	if exec {
		list = append(list, execFeatures...)
	}

	return list
}

// reservedFeatures builds the list of reserved features.
//
// Parameters:
//   - exec: true when the exec tier is available
//
// Returns the features that no tote announces yet, with the ones of the exec tier while that tier
// is not available.
func reservedFeatures(exec bool) []string {
	list := []string{
		"item_action.connection", "field.connection", "field.repo", "field.path",
		"field.choices_from", "field.secret", "field.remember", "exec.health", "exec.terminal",
		"exec.search", "open.markdown", "event.open", "event.log", "event.hello", "session.long",
		"event.handoff", "event.clipboard", "event.exec", "widget", "provider", "decorator",
		"hook.observe",
	}

	if !exec {
		list = append(list, execFeatures...)
	}

	return list
}

// AnnouncedFeatures lists the host features that tote announces in host.features and in
// `plugin check`: the six of the declarative tier in tote 1.0.
var AnnouncedFeatures = announcedFeatures(execTierAvailable)

// ReservedFeatures lists the features that are reserved and not announced: a manifest that needs
// one is incompatible (feature_missing), and one that uses its syntax gets reserved_not_available.
// In tote 1.0 it holds exec.list and exec.run too.
var ReservedFeatures = reservedFeatures(execTierAvailable)

// shellNames lists the shells and launchers that run their arguments as programs. Naming one in a
// declarative exec: permission is a lint error: the list is advisory, no list can stop a program
// that runs other programs, and the consent view is the real gate.
var shellNames = []string{
	"sh", "bash", "zsh", "dash", "ksh", "fish", "csh", "tcsh", "env", "xargs", "sudo", "doas", "su",
	"nohup", "nice", "ionice", "time", "timeout", "setsid", "stdbuf", "watch", "find", "awk", "gawk",
	"perl", "python", "python3", "ruby", "node", "lua", "php", "tclsh", "osascript", "eval",
}

// IsShellName reports whether a program name is a known shell or launcher.
//
// Parameters:
//   - name: program name as written in an exec: permission or at argv[0]; compared without case,
//     because a case-insensitive file system resolves SH to sh
func IsShellName(name string) bool {
	return slices.Contains(shellNames, strings.ToLower(name))
}

// blockedEnvExact and blockedEnvPrefix list the environment names that no manifest field may
// request or set: loader and interpreter variables, tote's own, the ones that make git or ssh run
// a program, and the ones through which a pager, an editor, a viewer or a library named by the
// value is started by a program that the manifest declared. The list cannot be complete: the
// consent view shows the env table with its values, and that view is the real gate.
var (
	blockedEnvExact = []string{
		"PATH", "HOME", "SHELL", "IFS", "ENV", "BASH_ENV", "SHELLOPTS", "BASHOPTS", "PS4",
		"PROMPT_COMMAND", "CDPATH", "GLOBIGNORE", "JAVA_TOOL_OPTIONS", "SSH_ASKPASS", "GIT_DIR",
		"GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_EXTERNAL_DIFF", "GIT_PAGER", "GIT_EDITOR",
		"GIT_SEQUENCE_EDITOR", "GIT_TEMPLATE_DIR", "GIT_ALLOW_PROTOCOL", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_COMMON_DIR", "GIT_ATTR_SOURCE",
		"GIT_REPLACE_REF_BASE", "PAGER", "EDITOR", "VISUAL", "MANPAGER", "MANOPT", "BROWSER",
		"LUA_INIT", "OPENSSL_CONF", "SSL_CERT_FILE", "SSL_CERT_DIR", "GCONV_PATH", "LOCPATH",
		"NLSPATH", "HOSTALIASES", "XDG_CONFIG_HOME", "XDG_CONFIG_DIRS", "XDG_DATA_HOME",
		"XDG_DATA_DIRS",
	}
	blockedEnvPrefix = []string{
		"LD_", "DYLD_", "TOTE_", "PYTHON", "PERL5", "RUBY", "NODE_", "GIT_SSH", "GIT_CONFIG",
		"GIT_ASKPASS", "GIT_EXEC_PATH", "GIT_PROXY_COMMAND", "BASH_FUNC_", "GIT_TRACE", "LESS",
		"SSH_ASKPASS",
	}
)

// BlockedEnvName reports whether a name may not be passed to a plugin program or set by a
// manifest.
//
// Parameters:
//   - name: environment variable name
func BlockedEnvName(name string) bool {
	if slices.Contains(blockedEnvExact, name) {
		return true
	}

	for _, p := range blockedEnvPrefix {
		if strings.HasPrefix(name, p) {
			return true
		}
	}

	return false
}
