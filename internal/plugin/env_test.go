package plugin

import (
	"sort"
	"strings"
	"testing"
)

// hostileParent is an environment full of what a plugin program must never see.
var hostileParent = map[string]string{
	"LD_PRELOAD": "/tmp/evil.so", "LD_LIBRARY_PATH": "/tmp", "DYLD_INSERT_LIBRARIES": "/tmp/x",
	"BASH_ENV": "/tmp/rc", "ENV": "/tmp/rc", "IFS": "x", "PS4": "$(id)", "PROMPT_COMMAND": "id",
	"SHELLOPTS": "xtrace", "BASH_FUNC_x%%": "() { id; }", "CDPATH": "/tmp",
	"GIT_DIR": "/tmp/g", "GIT_WORK_TREE": "/tmp/w", "GIT_INDEX_FILE": "/tmp/i",
	"GIT_SSH_COMMAND": "id", "GIT_SSH": "id", "GIT_CONFIG_GLOBAL": "/tmp/c", "GIT_CONFIG_COUNT": "1",
	"GIT_EXEC_PATH": "/tmp/e", "GIT_ASKPASS": "/tmp/a", "SSH_ASKPASS": "/tmp/a",
	"SSH_AUTH_SOCK": "/tmp/agent", "XDG_RUNTIME_DIR": "/run/user/1000", "TOTE_HANDOFF": "/tmp/h",
	"TOTE_SHELL": "bash", "__TOTE_DEFS": "x", "TOTE_PLUGIN_ID": "forged", "TOTE_PLUGIN_DIR": "/tmp",
	"TOTE_API": "99", "GITHUB_TOKEN": "ghp_secretsecretsecret", "GH_TOKEN": "gh-secret-token",
	"AWS_SECRET_ACCESS_KEY": "aws-secret", "HTTP_PROXY": "http://p:1", "https_proxy": "http://p:2",
	"NO_PROXY": "x", "DISPLAY": ":0", "DBUS_SESSION_BUS_ADDRESS": "unix:path=/x",
	"PYTHONPATH": "/tmp", "PERL5OPT": "-M", "RUBYOPT": "-r", "NODE_OPTIONS": "--require x",
	"JAVA_TOOL_OPTIONS": "-javaagent:x", "SHELL": "/bin/zsh", "LC_FOO": "x", "PWD": "/tmp",
	"OLDPWD": "/tmp", "_": "/usr/bin/evil", "EDITOR": "vim", "VISUAL": "vim", "PAGER": "less",
	"MANPAGER": "id", "LESSOPEN": "|id", "TERMINFO": "/tmp", "XDG_DATA_DIRS": "/tmp",
	"XDG_CONFIG_DIRS": "/tmp", "SUDO_USER": "x", "SSH_CONNECTION": "x", "TMUX": "x",
}

// copiedParent is what the environment builder is allowed to copy.
var copiedParent = map[string]string{
	"USER": "alice", "LOGNAME": "alice", "LANG": "fr_FR.UTF-8", "LANGUAGE": "fr", "LC_ALL": "C",
	"LC_CTYPE": "C.UTF-8", "TERM": "xterm-256color", "COLORTERM": "truecolor", "NO_COLOR": "1",
	"TZ": "Europe/Paris", "TMPDIR": "/tmp/t", "XDG_CONFIG_HOME": "/xdg/c", "XDG_DATA_HOME": "/xdg/d",
	"XDG_STATE_HOME": "/xdg/s", "XDG_CACHE_HOME": "/xdg/k",
}

// envLab builds a sandbox whose tote environment is hostile, with a plugin that runs "tool" and
// the given permissions and entry body.
//
// Parameters:
//   - t: test to report to
//   - perms: permission entries of the plugin
//   - body: tables of the manifest
func envLab(t *testing.T, perms []string, body string) (*lab, *Registry) {
	t.Helper()

	l := newLab(t)
	l.useSystemTools()
	l.script("tool", "exit 0\n")

	for k, v := range hostileParent {
		l.env[k] = v
	}

	for k, v := range copiedParent {
		l.env[k] = v
	}

	l.env["PATH"] = ":.:relative/bin:" + l.bin + ":" + shellPath + ":"

	l.plug("demo", perms, body)

	return l, l.reg(nil)
}

// echoBody is one command that runs "tool" in the pager.
const echoBody = `
[[command]]
id = "go"
label = "Go"
run = ["tool"]
output = "pager"
`

// keysOf lists the names of an environment, sorted.
//
// Parameters:
//   - env: KEY=value entries
func keysOf(env []string) []string {
	var keys []string

	for k := range envMap(env) {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// TestEnvironmentFromNothing is the hostile parent environment test: with a plugin that declares
// nothing, the program gets exactly the allow-list of section 5.3 and tote's own variables, and
// not one other name, whatever tote itself runs in.
//
// Parameters:
//   - t: test to report to
func TestEnvironmentFromNothing(t *testing.T) {
	l, r := envLab(t, []string{"exec:tool"}, echoBody)

	p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "go", Lang: "fr"})
	env := envMap(p.Env)

	want := []string{
		"COLORTERM", "GIT_TERMINAL_PROMPT", "HOME", "LANG", "LANGUAGE", "LC_ALL",
		"LC_CTYPE", "LOGNAME", "NO_COLOR", "PATH", "PYTHONDONTWRITEBYTECODE", "TERM", "TMPDIR",
		"TOTE_API", "TOTE_BIN", "TOTE_CACHE_DIR", "TOTE_CONFIG_DIR", "TOTE_DATA_DIR",
		"TOTE_HOST_VERSION", "TOTE_LANG", "TOTE_PLUGIN_CACHE", "TOTE_PLUGIN_DIR", "TOTE_PLUGIN_ID",
		"TOTE_PLUGIN_STATE", "TOTE_STATE_DIR", "TZ", "USER", "XDG_CACHE_HOME", "XDG_CONFIG_HOME",
		"XDG_DATA_HOME", "XDG_STATE_HOME",
	}

	if got := keysOf(p.Env); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("names\n got  %v\n want %v", got, want)
	}

	for k, v := range copiedParent {
		if env[k] != v {
			t.Errorf("%s = %q want %q", k, env[k], v)
		}
	}

	rec, _ := l.lock().Get("demo")
	stateDir, _ := l.paths.PluginStateDir("demo")
	cacheDir, _ := l.paths.PluginCacheDir("demo")

	exact := map[string]string{
		"TOTE_API": "1", "TOTE_HOST_VERSION": "1.0.3", "TOTE_LANG": "fr",
		"TOTE_BIN": "/usr/local/bin/tote", "TOTE_PLUGIN_ID": "demo",
		"TOTE_PLUGIN_DIR": l.treeDir(rec), "TOTE_PLUGIN_STATE": stateDir,
		"TOTE_PLUGIN_CACHE": cacheDir, "PYTHONDONTWRITEBYTECODE": "1", "GIT_TERMINAL_PROMPT": "0",
		"TOTE_CONFIG_DIR": l.env["TOTE_CONFIG_DIR"], "TOTE_DATA_DIR": l.env["TOTE_DATA_DIR"],
		"TOTE_STATE_DIR": l.env["TOTE_STATE_DIR"], "TOTE_CACHE_DIR": l.env["TOTE_CACHE_DIR"],
		"PATH": l.bin + ":" + shellPath, "HOME": l.home,
	}

	for k, v := range exact {
		if env[k] != v {
			t.Errorf("%s = %q want %q", k, env[k], v)
		}
	}

	for _, secret := range []string{"ghp_secret", "gh-secret", "aws-secret", "forged", "evil"} {
		if strings.Contains(strings.Join(p.Env, "\n"), secret) {
			t.Errorf("%q leaked into the environment", secret)
		}
	}
}

// TestEnvironmentDeclared checks what a declaration adds: proxies with a net: permission, a
// variable with env:, never a name that loads code, and the redaction values.
//
// Parameters:
//   - t: test to report to
func TestEnvironmentDeclared(t *testing.T) {
	t.Run("net adds the proxy variables", func(t *testing.T) {
		_, r := envLab(t, []string{"exec:tool", "net:example.com"}, echoBody)
		env := envMap(prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "go"}).Env)

		for _, k := range []string{"HTTP_PROXY", "https_proxy", "NO_PROXY"} {
			if env[k] == "" {
				t.Errorf("%s missing", k)
			}
		}

		if _, ok := env["ALL_PROXY"]; ok {
			t.Error("a variable that was not set appeared")
		}
	})

	t.Run("env passes one variable", func(t *testing.T) {
		_, r := envLab(t, []string{"exec:tool", "env:GITHUB_TOKEN", "env:NOT_SET"}, echoBody)
		p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "go"})
		env := envMap(p.Env)

		if env["GITHUB_TOKEN"] != "ghp_secretsecretsecret" {
			t.Errorf("GITHUB_TOKEN = %q", env["GITHUB_TOKEN"])
		}

		if _, ok := env["GH_TOKEN"]; ok {
			t.Error("GH_TOKEN passed without a declaration")
		}

		if _, ok := env["NOT_SET"]; ok {
			t.Error("a declared variable that is not set appeared")
		}

		if len(p.secrets) != 1 || p.secrets[0] != "ghp_secretsecretsecret" {
			t.Errorf("secrets %q", p.secrets)
		}
	})

	t.Run("the runtime directory only when declared", func(t *testing.T) {
		_, r := envLab(t, []string{"exec:tool", "env:XDG_RUNTIME_DIR", "env:SSH_AUTH_SOCK"}, echoBody)
		env := envMap(prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "go"}).Env)

		if env["XDG_RUNTIME_DIR"] != "/run/user/1000" || env["SSH_AUTH_SOCK"] != "/tmp/agent" {
			t.Errorf("declared variables missing: %v", env)
		}
	})

	t.Run("a name that loads code is never passed", func(t *testing.T) {
		// A validated manifest cannot declare these names; the builder refuses them as well.
		l, r := envLab(t, []string{"exec:tool"}, echoBody)
		m := &Manifest{Permissions: []string{"env:LD_PRELOAD", "env:GIT_DIR", "env:PYTHONPATH"}}

		env, _ := r.buildEnv(Plugin{ID: "demo", Manifest: m}, l.env["PATH"], Avoid{}, envPlan{})

		for _, k := range []string{"LD_PRELOAD", "GIT_DIR", "PYTHONPATH"} {
			if _, ok := envMap(env)[k]; ok {
				t.Errorf("%s passed", k)
			}
		}
	})

	t.Run("an env table is expanded and wins over the parent", func(t *testing.T) {
		body := `
[[command]]
id = "go"
label = "Go"
run = ["tool"]
output = "pager"
env = { MY_FLAG = "on-{text}", OPT = "{opt?}", LONGVAL = "0123456789abcdef" }
[[command.field]]
id = "text"
type = "text"
label = "T"
[[command.field]]
id = "opt"
type = "text"
label = "O"
`
		_, r := envLab(t, []string{"exec:tool", "env:MY_FLAG", "env:OPT", "env:LONGVAL"}, body)
		p := prepare(t, r, Call{
			Plugin: "demo", Table: TableCommand, Entry: "go",
			Values: map[string]string{"text": "x"},
		})
		env := envMap(p.Env)

		if env["MY_FLAG"] != "on-x" || env["LONGVAL"] != "0123456789abcdef" {
			t.Errorf("env table: %v", env)
		}

		if _, ok := env["OPT"]; ok {
			t.Error("an optional empty value must drop the variable")
		}

		if len(p.secrets) != 1 || p.secrets[0] != "0123456789abcdef" {
			t.Errorf("only a value of 8 bytes or more is redacted: %q", p.secrets)
		}
	})
}

// TestEnvironmentModes checks the variables that depend on the kind of run: NO_COLOR in the mono
// theme, and the git prompt switch for captured runs only.
//
// Parameters:
//   - t: test to report to
func TestEnvironmentModes(t *testing.T) {
	body := echoBody + `
[[command]]
id = "term"
label = "Term"
run = ["tool"]
output = "terminal"
`
	l, r := envLab(t, []string{"exec:tool"}, body)
	delete(l.env, "NO_COLOR")

	term := envMap(prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "term"}).Env)
	if _, ok := term["GIT_TERMINAL_PROMPT"]; ok {
		t.Error("a terminal run must not disable the git prompt")
	}

	mono := envMap(prepare(t, r,
		Call{Plugin: "demo", Table: TableCommand, Entry: "go", Mono: true}).Env)
	if mono["NO_COLOR"] != "1" || mono["GIT_TERMINAL_PROMPT"] != "0" {
		t.Errorf("mono: %v", mono)
	}

	plain := envMap(prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "go"}).Env)
	if _, ok := plain["NO_COLOR"]; ok {
		t.Error("NO_COLOR set without the mono theme or the user's variable")
	}
}

// TestScrubPath checks the PATH given to the program.
//
// Parameters:
//   - t: test to report to
func TestScrubPath(t *testing.T) {
	avoid := Avoid{Under: []string{"/data/plugins"}, Exactly: []string{"/work"}}

	tests := []struct{ in, want string }{
		{"", ""},
		{":", ""},
		{".", ""},
		{"bin:../bin", ""},
		{"/usr/bin:/bin", "/usr/bin:/bin"},
		{"/usr/bin::/bin:.", "/usr/bin:/bin"},
		{"/usr/bin/../bin//", "/usr/bin"},
		{"/work:/usr/bin", "/usr/bin"},
		{"/work/sub:/usr/bin", "/work/sub:/usr/bin"},
		{"/data/plugins/x/bin:/usr/bin", "/usr/bin"},
		{"/data/plugins:/usr/bin", "/usr/bin"},
		{"/data/pluginsx:/usr/bin", "/data/pluginsx:/usr/bin"},
	}

	for _, tt := range tests {
		if got := scrubPath(tt.in, avoid); got != tt.want {
			t.Errorf("scrubPath(%q) = %q want %q", tt.in, got, tt.want)
		}
	}
}

// TestEnvironmentWithoutPath checks that an empty scrubbed PATH is left out: an empty PATH would
// make some shells search the working directory.
//
// Parameters:
//   - t: test to report to
func TestEnvironmentWithoutPath(t *testing.T) {
	l := newLab(t)
	r := l.reg(nil)

	env, _ := r.buildEnv(Plugin{ID: "demo", Manifest: &Manifest{}}, ":.:rel", Avoid{}, envPlan{})

	if _, ok := envMap(env)["PATH"]; ok {
		t.Errorf("PATH present: %v", env)
	}

	l.env["HOME"] = ""

	env, _ = r.buildEnv(Plugin{ID: "demo", Manifest: &Manifest{}}, "/usr/bin", Avoid{}, envPlan{})
	if envMap(env)["HOME"] != l.home {
		t.Errorf("HOME falls back to the configured home: %v", envMap(env)["HOME"])
	}

	l.env["TZ"] = "a\x00b"

	env, _ = r.buildEnv(Plugin{ID: "demo", Manifest: &Manifest{}}, "/usr/bin", Avoid{}, envPlan{})
	if _, ok := envMap(env)["TZ"]; ok {
		t.Error("a value with a NUL byte was passed")
	}
}
