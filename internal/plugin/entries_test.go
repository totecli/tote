package plugin

import (
	"strings"
	"testing"
)

// tool and repoTool are the permissions that the snippets of these tests need.
var (
	tool     = []string{"exec:tool"}
	repoTool = []string{"exec:tool", "host:read:repos"}
)

// cmd builds one command table from its key lines.
//
// Parameters:
//   - lines: key lines placed after the id and the label
func cmd(lines string) string {
	return "[[command]]\nid = \"go\"\nlabel = \"Go\"\n" + lines
}

// run is the key lines of a valid pager command that runs "tool" with the given argument.
//
// Parameters:
//   - args: TOML array body, such as "\"{x}\""
func run(args string) string {
	return "run = [\"tool\"" + args + "]\noutput = \"pager\"\n"
}

// field builds a [[command.field]] table.
//
// Parameters:
//   - lines: key lines placed after the id
func field(lines string) string {
	return "[[command.field]]\nid = \"x\"\nlabel = \"X\"\n" + lines
}

// TestRulesEntryShape checks the keys of a contribution against each other.
//
// Parameters:
//   - t: test to report to
func TestRulesEntryShape(t *testing.T) {
	in := func(perms []string, body string) string { return decl("", perms, body) }
	url := []string{"host:open-url"}

	tests := []ruleCase{
		{
			name: "label missing", src: in(tool, "[[command]]\nid = \"go\"\n"+run("")),
			code: CodeKeyMissing, path: "command[0].label",
		},
		{
			name: "label long", src: in(tool, "[[command]]\nid = \"go\"\nlabel = \""+times("a",
				61)+"\"\n"+run("")),
			code: CodeLimitExceeded, path: "command[0].label",
		},
		{
			name: "id grammar", src: in(tool, "[[command]]\nid = \"Go\"\nlabel = \"G\"\n"+run("")),
			code: CodeIDInvalid, path: "command[0].id",
		},
		{
			name: "id duplicate", src: in(tool, cmd(run(""))+cmd(run(""))), code: CodeIDDuplicate,
			path: "command[1].id",
		},
		{
			name: "run and open", src: in(append(url, tool...),
				cmd(run("")+"open = \"https://x.example/\"\n")),
			code: CodeKeyConflict,
		},
		{
			name: "neither run nor open", src: in(tool, cmd("")), code: CodeKeyMissing,
			path: "command[0].run",
		},
		{
			name: "run empty", src: in(tool, cmd("run = []\noutput = \"pager\"\n")),
			code: CodeValueInvalid,
			path: "command[0].run",
		},
		{
			name: "run too long", src: in(tool, cmd("run = [\"tool\""+times(",\"a\"",
				64)+"]\noutput = \"pager\"\n")),
			code: CodeLimitExceeded, path: "command[0].run",
		},
		{
			name: "element too long", src: in(tool, cmd("run = [\"tool\", \""+times("a",
				1025)+"\"]\noutput = \"pager\"\n")),
			code: CodeLimitExceeded, path: "command[0].run[1]",
		},
		{
			name: "output missing", src: in(tool, cmd("run = [\"tool\"]\n")), code: CodeKeyMissing,
			path: "command[0].output",
		},
		{
			name: "output unknown", src: in(tool, cmd("run = [\"tool\"]\noutput = \"loud\"\n")),
			code: CodeValueInvalid, path: "command[0].output",
		},
		{
			name: "output table reserved", src: in(tool,
				cmd("run = [\"tool\"]\noutput = \"table\"\n")),
			code: CodeReserved,
		},
		{
			name: "output pane reserved", src: in(tool,
				cmd("run = [\"tool\"]\noutput = \"pane\"\n")),
			code: CodeReserved,
		},
		{
			name: "output stream reserved", src: in(tool,
				cmd("run = [\"tool\"]\noutput = \"stream\"\n")),
			code: CodeReserved,
		},
		{
			name: "pause with pager", src: in(tool, cmd(run("")+"pause = \"never\"\n")),
			code: CodeKeyConflict, path: "command[0].pause",
		},
		{
			name: "pause unknown", src: in(tool,
				cmd("run = [\"tool\"]\noutput = \"terminal\"\npause = \"x\"\n")),
			code: CodeValueInvalid,
		},
		{
			name: "timeout with terminal", src: in(tool,
				cmd("run = [\"tool\"]\noutput = \"terminal\"\ntimeout = \"1m\"\n")),
			code: CodeKeyConflict, path: "command[0].timeout",
		},
		{
			name: "timeout too long", src: in(tool, cmd(run("")+"timeout = \"31m\"\n")),
			code: CodeLimitExceeded, path: "command[0].timeout",
		},
		{name: "timeout unit", src: in(tool, cmd(run("")+"timeout = \"5h\"\n")), code: CodeValueInvalid},
		{name: "timeout zero", src: in(tool, cmd(run("")+"timeout = \"0s\"\n")), code: CodeValueInvalid},
		{
			name: "timeout huge", src: in(tool, cmd(run("")+"timeout = \"99999999999m\"\n")),
			code: CodeValueInvalid,
		},
		{name: "timeout bare", src: in(tool, cmd(run("")+"timeout = \"30\"\n")), code: CodeValueInvalid},
		{name: "confirm empty", src: in(tool, cmd(run("")+"confirm = \"\"\n")), code: CodeValueInvalid},
		{name: "confirm type", src: in(tool, cmd(run("")+"confirm = 1\n")), code: CodeTypeInvalid},
		{
			name: "confirm unknown placeholder", src: in(tool,
				cmd(run("")+"confirm = \"Go {nope}?\"\n")),
			code: CodePlaceholderUnknown, path: "command[0].confirm",
		},
		{name: "key invalid", src: in(tool, cmd(run("")+"key = \"!\"\n")), code: CodeValueInvalid},
		{name: "key two chars", src: in(tool, cmd(run("")+"key = \"ab\"\n")), code: CodeLimitExceeded},
		{
			name: "key reserved", src: in(tool, cmd(run("")+"key = \"q\"\n")),
			code: CodeKeyReserved,
			warn: true,
		},
		{
			name: "keywords count", src: in(tool, cmd(run("")+"keywords = ["+times("\"k\",",
				9)+"]\n")),
			code: CodeLimitExceeded,
		},
		{
			name: "keyword long", src: in(tool, cmd(run("")+"keywords = [\""+times("k",
				25)+"\"]\n")),
			code: CodeLimitExceeded, path: "command[0].keywords[0]",
		},
		{
			name: "allow_dash unknown", src: in(tool, cmd(run("")+"allow_dash = [\"zz\"]\n")),
			code: CodePlaceholderUnknown, path: "command[0].allow_dash[0]",
		},
		{
			name: "allow_dash grammar", src: in(tool, cmd(run("")+"allow_dash = [\"Z Z\"]\n")),
			code: CodeValueInvalid,
		},
		{
			name: "open without permission", src: in(nil, cmd("open = \"https://x.example/\"\n")),
			code: CodePermMissing, path: "command[0].open",
		},
		{
			name: "open http", src: in(url, cmd("open = \"http://x.example/\"\n")),
			code: CodeValueInvalid,
			path: "command[0].open",
		},
		{
			name: "open userinfo", src: in(url, cmd("open = \"https://u@x.example/\"\n")),
			code: CodeValueInvalid,
		},
		{
			name: "open with cwd", src: in(url,
				cmd("open = \"https://x.example/\"\ncwd = \"{home}\"\n")),
			code: CodeKeyConflict, path: "command[0].cwd",
		},
		{
			name: "open with output", src: in(url,
				cmd("open = \"https://x.example/\"\noutput = \"pager\"\n")),
			code: CodeKeyConflict,
		},
		{
			name: "open with env", src: in(append(url, "env:A"),
				cmd("open = \"https://x.example/\"\n[command.env]\nA = \"1\"\n")),
			code: CodeKeyConflict,
		},
		{
			name: "item action without target", src: in(repoTool,
				"[[item_action]]\nid = \"a\"\nlabel = \"A\"\n"+run("")), code: CodeKeyMissing,
			path: "item_action[0].target",
		},
		{
			name: "item action connection", src: in(repoTool,
				"[[item_action]]\nid = \"a\"\nlabel = \"A\"\ntarget = \"connection\"\n"+run("")),
			code: CodeReserved, path: "item_action[0].target",
		},
		{
			name: "item action target unknown", src: in(repoTool,
				"[[item_action]]\nid = \"a\"\nlabel = \"A\"\ntarget = \"disk\"\n"+run("")),
			code: CodeValueInvalid,
		},
		{
			name: "item action target none", src: in(repoTool,
				"[[item_action]]\nid = \"a\"\nlabel = \"A\"\ntarget = \"none\"\n"+run("")),
			code: CodeValueInvalid,
		},
		{
			name: "item action needs repos", src: in(tool,
				"[[item_action]]\nid = \"a\"\nlabel = \"A\"\ntarget = \"repo\"\n"+run("")),
			code: CodePermMissing, path: "item_action[0].target",
		},
		{
			name: "repo placeholder needs repos", src: in(tool, cmd(run(", \"{repo.slug}\""))),
			code: CodePermMissing, path: "command[0].run[1]",
		},
	}

	runRules(t, tests)
}

// TestRulesArgv checks the argv template: program, placeholders, scope and the lint rules.
//
// Parameters:
//   - t: test to report to
func TestRulesArgv(t *testing.T) {
	in := func(perms []string, body string) string { return decl("", perms, body) }
	setting := func(typ string) string {
		return "[[setting]]\nid = \"s\"\ntype = \"" + typ + "\"\nlabel = \"S\"\n"
	}

	tests := []ruleCase{
		{
			name: "program is a placeholder",
			src: in(tool, cmd("run = [\"{x}\"]\noutput = \"pager\"\n"+
				"[[command.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"X\"\n")), code: CodeArgv0Invalid,
			path: "command[0].run[0]",
		},
		{
			name: "program has a slash", src: in([]string{"exec:tool"},
				cmd("run = [\"./tool\"]\noutput = \"pager\"\n")),
			code: CodeArgv0Invalid,
		},
		{
			name: "program has a brace escape", src: in(tool,
				cmd("run = [\"to{{ol\"]\noutput = \"pager\"\n")),
			code: CodeArgv0Invalid,
		},
		{
			name: "program not covered", src: in(nil, cmd(run(""))), code: CodeExecUncovered,
			path: "command[0].run[0]",
		},
		{
			name: "program is tote", src: in([]string{"exec:other"},
				cmd("run = [\"tote\"]\noutput = \"pager\"\n")),
			code: CodeExecSelf,
		},
		{
			name: "program is a shell", src: in([]string{"exec:other"},
				cmd("run = [\"bash\", \"-c\", \"x\"]\noutput = \"pager\"\n")),
			code: CodeArgvShell, path: "command[0].run[0]",
		},
		{
			name: "stray open brace", src: in(tool,
				cmd("run = [\"tool\", \"{\"]\noutput = \"pager\"\n")),
			code: CodePlaceholderSyntax, path: "command[0].run[1]",
		},
		{
			name: "stray close brace", src: in(tool,
				cmd("run = [\"tool\", \"a}\"]\noutput = \"pager\"\n")),
			code: CodePlaceholderSyntax,
		},
		{
			name: "empty placeholder", src: in(tool,
				cmd("run = [\"tool\", \"{}\"]\noutput = \"pager\"\n")),
			code: CodePlaceholderSyntax,
		},
		{
			name: "bad placeholder name", src: in(tool,
				cmd("run = [\"tool\", \"{A-b}\"]\noutput = \"pager\"\n")),
			code: CodePlaceholderSyntax,
		},
		{
			name: "unknown field", src: in(tool, cmd(run(", \"{nope}\""))),
			code: CodePlaceholderUnknown,
			path: "command[0].run[1]",
		},
		{
			name: "unknown repo member", src: in(repoTool, cmd(run(", \"{repo.url}\""))),
			code: CodePlaceholderUnknown,
		},
		{
			name: "unknown setting", src: in(tool, cmd(run(", \"{setting.zz}\""))),
			code: CodePlaceholderUnknown,
		},
		{
			name: "item outside a list action", src: in(tool, cmd(run(", \"{item.id}\""))),
			code: CodePlaceholderScope,
		},
		{
			name: "list setting inside text", src: in(tool,
				setting("list")+cmd(run(", \"a{setting.s}\""))),
			code: CodePlaceholderList,
		},
		{
			name: "list setting in cwd", src: in(tool,
				setting("list")+cmd(run("")+"cwd = \"{setting.s}\"\n")),
			code: CodePlaceholderList,
		},
		{
			name: "field declared after use", src: in(tool, cmd(run(""))+
				"[[command.field]]\nid = \"a\"\ntype = \"text\"\nlabel = \"A\"\n"+
				"default = \"{b}\"\n"+
				"[[command.field]]\nid = \"b\"\ntype = \"text\"\nlabel = \"B\"\n"),
			code: CodePlaceholderUnknown, path: "command[0].field[0].default",
		},
		{
			name: "env not covered", src: in(tool, cmd(run("")+"[command.env]\nA = \"1\"\n")),
			code: CodeEnvUncovered, path: "command[0].env.A",
		},
		{
			name: "env blocked", src: in(append([]string{"env:A"}, tool...),
				cmd(run("")+"[command.env]\nLD_PRELOAD = \"1\"\n")),
			code: CodeEnvRefused,
		},
		{
			name: "env tote", src: in(tool, cmd(run("")+"[command.env]\nTOTE_X = \"1\"\n")),
			code: CodeEnvRefused,
		},
		{
			name: "env name", src: in(tool, cmd(run("")+"[command.env]\nlower = \"1\"\n")),
			code: CodeValueInvalid,
		},
		{
			name: "env placeholder", src: in(append([]string{"env:A"}, tool...),
				cmd(run("")+"[command.env]\nA = \"{nope}\"\n")),
			code: CodePlaceholderUnknown,
		},
		{
			name: "env count", src: in(tool, cmd(run("")+"[command.env]\n"+envLines(17))),
			code: CodeLimitExceeded,
		},
		{
			name: "ssh without dashdash", src: in([]string{"exec:ssh"},
				cmd("run = [\"ssh\", \"{x}\"]\noutput = \"pager\"\n"+
					"[[command.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"X\"\n")), code: CodeArgvSSHDashDash,
			path: "command[0].run[1]",
		},
		{name: "ssh placeholder before dashdash", src: in([]string{"exec:ssh"},
			cmd("run = [\"ssh\", \"{x}\", \"--\", \"h\"]\noutput = \"pager\"\n"+
				"[[command.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"X\"\n")), code: CodeArgvSSHDashDash},
		{
			name: "git dash c", src: in([]string{"exec:git"},
				cmd("run = [\"git\", \"-c\", \"a.b=c\", \"status\"]\noutput = \"pager\"\n")),
			code: CodeArgvRiskyFlag, warn: true,
		},
		{
			name: "git exec path attached", src: in([]string{"exec:git"},
				cmd("run = [\"git\", \"--exec-path=/x\"]\noutput = \"pager\"\n")),
			code: CodeArgvRiskyFlag, warn: true,
		},
		{
			name: "ssh dash o attached", src: in([]string{"exec:ssh"},
				cmd("run = [\"ssh\", \"-oProxyCommand=x\", \"--\", \"h\"]\noutput = \"pager\"\n")),
			code: CodeArgvRiskyFlag, warn: true,
		},
		{
			name: "curl config", src: in([]string{"exec:curl"},
				cmd("run = [\"curl\", \"--config\", \"f\"]\noutput = \"pager\"\n")),
			code: CodeArgvRiskyFlag, warn: true,
		},
		{
			name: "tar to command", src: in([]string{"exec:tar"},
				cmd("run = [\"tar\", \"--to-command=x\"]\noutput = \"pager\"\n")),
			code: CodeArgvRiskyFlag, warn: true,
		},
		{
			name: "exec permission unused", src: in([]string{"exec:tool", "exec:never"},
				cmd(run(""))),
			code: CodeExecUnused, path: "permissions[1]", warn: true,
		},
	}

	runRules(t, tests)

	valid := []string{
		in(tool, cmd(run(", \"{x}\"")+"[[command.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"X\"\n")),
		in(tool, cmd("run = [\"tool\", \"{{literal}}\", \"{x?}\"]\noutput = \"pager\"\n"+
			"[[command.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"X\"\n")),
		in(tool, setting("list")+cmd(run(", \"{setting.s}\""))),
		in([]string{"exec:ssh"}, cmd("run = [\"ssh\", \"--\", \"{x}\"]\noutput = \"pager\"\n"+
			"[[command.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"X\"\n")),
		in(append([]string{"host:read:repos"}, tool...),
			cmd(run(", \"{repo.slug}\", \"{repo.path}\", \"{cwd}\", \"{home}\""))),
		in(tool,
			cmd(run(", \"--tag={x}\"")+"allow_dash = [\"x\"]\n[[command.field]]\nid = \"x\"\ntype = "+
				"\"text\"\nlabel = \"X\"\n")),
	}

	for i, src := range valid {
		if m, rep := decode(src); m == nil {
			t.Errorf("valid case %d refused: %v", i, rep.Issues)
		}
	}
}

// envLines builds n environment entries.
//
// Parameters:
//   - n: number of entries
func envLines(n int) string {
	var b strings.Builder

	for i := 0; i < n; i++ {
		b.WriteString("V" + string(rune('A'+i)) + " = \"1\"\n")
	}

	return b.String()
}

// TestRulesFields checks the form fields.
//
// Parameters:
//   - t: test to report to
func TestRulesFields(t *testing.T) {
	in := func(f string) string { return decl("", tool, cmd(run(""))+f) }

	tests := []ruleCase{
		{
			name: "id grammar",
			src:  in("[[command.field]]\nid = \"X\"\ntype = \"text\"\nlabel = \"X\"\n"),
			code: CodeIDInvalid, path: "command[0].field[0].id",
		},
		{
			name: "id reserved repo",
			src:  in("[[command.field]]\nid = \"repo\"\ntype = \"text\"\nlabel = \"X\"\n"),
			code: CodeIDInvalid,
		},
		{
			name: "id reserved item",
			src:  in("[[command.field]]\nid = \"item\"\ntype = \"text\"\nlabel = \"X\"\n"),
			code: CodeIDInvalid,
		},
		{
			name: "id duplicate", src: in(field("type = \"text\"\n") + field("type = \"text\"\n")),
			code: CodeIDDuplicate, path: "command[0].field[1].id",
		},
		{
			name: "type missing", src: in(field("")), code: CodeKeyMissing,
			path: "command[0].field[0].type",
		},
		{name: "type unknown", src: in(field("type = \"color\"\n")), code: CodeValueInvalid},
		{
			name: "type secret key", src: in(field("type = \"text\"\nsecret = true\n")),
			code: CodeReserved,
			path: "command[0].field[0].secret",
		},
		{name: "remember key", src: in(field("type = \"text\"\nremember = true\n")), code: CodeReserved},
		{
			name: "choices_from key", src: in(field("type = \"choice\"\nchoices_from = [\"x\"]\n")),
			code: CodeReserved,
		},
		{name: "type connection", src: in(field("type = \"connection\"\n")), code: CodeReserved},
		{name: "type connections", src: in(field("type = \"connections\"\n")), code: CodeReserved},
		{name: "type repo", src: in(field("type = \"repo\"\n")), code: CodeReserved},
		{name: "type path", src: in(field("type = \"path\"\n")), code: CodeReserved},
		{
			name: "label long",
			src: in("[[command.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"" +
				times("a", 61) + "\"\n"),
			code: CodeLimitExceeded,
		},
		{
			name: "hint long", src: in(field("type = \"text\"\nhint = \"" + times("a",
				201) + "\"\n")),
			code: CodeLimitExceeded,
		},
		{
			name: "text with choices", src: in(field("type = \"text\"\nchoices = [\"a\"]\n")),
			code: CodeKeyConflict, path: "command[0].field[0].choices",
		},
		{
			name: "text with flag", src: in(field("type = \"text\"\nflag = \"-a\"\n")),
			code: CodeKeyConflict,
		},
		{
			name: "choice with pattern",
			src:  in(field("type = \"choice\"\nchoices = [\"a\"]\npattern = \"^a$\"\n")),
			code: CodeKeyConflict,
		},
		{
			name: "bool with max_len",
			src:  in(field("type = \"bool\"\nflag = \"-a\"\nmax_len = 3\n")),
			code: CodeKeyConflict,
		},
		{
			name: "max_len too big", src: in(field("type = \"text\"\nmax_len = 1025\n")),
			code: CodeLimitExceeded,
		},
		{name: "max_len zero", src: in(field("type = \"text\"\nmax_len = 0\n")), code: CodeLimitExceeded},
		{
			name: "choice without choices", src: in(field("type = \"choice\"\n")),
			code: CodeKeyMissing,
			path: "command[0].field[0].choices",
		},
		{
			name: "choice empty list", src: in(field("type = \"choice\"\nchoices = []\n")),
			code: CodeValueInvalid,
		},
		{
			name: "choice duplicate",
			src:  in(field("type = \"choice\"\nchoices = [\"a\", \"a\"]\n")),
			code: CodeValueInvalid, path: "command[0].field[0].choices[1]",
		},
		{
			name: "too many choices",
			src: in(field("type = \"choice\"\nchoices = [" + times("\"a\",", 65) +
				"]\n")),
			code: CodeLimitExceeded,
		},
		{
			name: "choice too long",
			src: in(field("type = \"choice\"\nchoices = [\"" + times("a", 65) +
				"\"]\n")),
			code: CodeLimitExceeded,
		},
		{
			name: "choice default outside",
			src:  in(field("type = \"choice\"\nchoices = [\"a\", \"\"]\ndefault = \"b\"\n")),
			code: CodeValueInvalid, path: "command[0].field[0].default",
		},
		{
			name: "bool without flag", src: in(field("type = \"bool\"\n")), code: CodeKeyMissing,
			path: "command[0].field[0].flag",
		},
		{
			name: "flag without dash", src: in(field("type = \"bool\"\nflag = \"force\"\n")),
			code: CodeValueInvalid,
		},
		{
			name: "flag with space", src: in(field("type = \"bool\"\nflag = \"-a b\"\n")),
			code: CodeValueInvalid,
		},
		{
			name: "flag long", src: in(field("type = \"bool\"\nflag = \"-" + times("a",
				64) + "\"\n")),
			code: CodeLimitExceeded,
		},
		{
			name: "bool default type",
			src:  in(field("type = \"bool\"\nflag = \"-a\"\ndefault = \"yes\"\n")),
			code: CodeTypeInvalid,
		},
		{
			name: "unanchored pattern", src: in(field("type = \"text\"\npattern = \"[0-9]+\"\n")),
			code: CodePatternUnanchored, path: "command[0].field[0].pattern",
		},
		{
			name: "pattern without start",
			src:  in(field("type = \"text\"\npattern = \"[0-9]+$\"\n")),
			code: CodePatternUnanchored,
		},
		{
			name: "pattern without end", src: in(field("type = \"text\"\npattern = \"^[0-9]+\"\n")),
			code: CodePatternUnanchored,
		},
		{
			name: "pattern alternation", src: in(field("type = \"text\"\npattern = \"^a|b$\"\n")),
			code: CodePatternUnanchored,
		},
		{
			name: "pattern escaped dollar",
			src:  in(field("type = \"text\"\npattern = \"^a\\\\$\"\n")),
			code: CodePatternUnanchored,
		},
		{
			name: "pattern multiline", src: in(field("type = \"text\"\npattern = \"(?m)^a$\"\n")),
			code: CodePatternUnanchored,
		},
		{
			name: "pattern invalid", src: in(field("type = \"text\"\npattern = \"^(a$\"\n")),
			code: CodeValueInvalid,
		},
		{
			name: "pattern long", src: in(field("type = \"text\"\npattern = \"^" + times("a", 255) +
				"$\"\n")),
			code: CodeLimitExceeded,
		},
		{
			name: "default fails pattern",
			src:  in(field("type = \"text\"\npattern = \"^[0-9]+$\"\ndefault = \"a\"\n")),
			code: CodeValueInvalid, path: "command[0].field[0].default",
		},
		{
			name: "default too long",
			src:  in(field("type = \"text\"\nmax_len = 2\ndefault = \"abc\"\n")),
			code: CodeValueInvalid,
		},
		{
			name: "default placeholder unknown",
			src:  in(field("type = \"text\"\ndefault = \"{zz}\"\n")),
			code: CodePlaceholderUnknown,
		},
		{
			name: "default item scope",
			src:  in(field("type = \"text\"\ndefault = \"{item.id}\"\n")),
			code: CodePlaceholderScope,
		},
		{
			name: "default repo needs permission",
			src:  in(field("type = \"text\"\ndefault = \"{repo.slug}\"\n")),
			code: CodePermMissing,
		},
		{
			name: "too many fields", src: in(manyFields(17)), code: CodeLimitExceeded,
			path: "command[0].field",
		},
	}

	runRules(t, tests)

	for _, ok := range []string{
		field("type = \"text\"\npattern = \"^[0-9]{1,4}$\"\ndefault = \"0\"\nrequired = true\n"),
		field("type = \"text\"\npattern = \"^(a|b)$\"\n"),
		field("type = \"text\"\npattern = \"^a$|^b$\"\n"),
		field("type = \"choice\"\nchoices = [\"\", \"-a\", \"b\"]\ndefault = \"b\"\n"),
		field("type = \"bool\"\nflag = \"--force\"\ndefault = true\n"),
		field("type = \"text\"\ndefault = \"{setting.s}\"\n") +
			"[[setting]]\nid = \"s\"\ntype = \"string\"\nlabel = \"S\"\n",
	} {
		if m, rep := decode(in(ok)); m == nil {
			t.Errorf("valid field refused: %v\n%s", rep.Issues, ok)
		}
	}
}

// manyFields builds n distinct fields of a command.
//
// Parameters:
//   - n: number of fields
func manyFields(n int) string {
	var b strings.Builder

	for i := 0; i < n; i++ {
		b.WriteString("[[command.field]]\nid = \"f" + string(rune('a'+i)) +
			"\"\ntype = \"text\"\nlabel = \"F\"\n")
	}

	return b.String()
}

// TestRulesSettings checks the [[setting]] tables.
//
// Parameters:
//   - t: test to report to
func TestRulesSettings(t *testing.T) {
	set := func(lines string) string {
		return decl("", nil, "[[setting]]\nid = \"s\"\nlabel = \"S\"\n"+lines)
	}

	tests := []ruleCase{
		{name: "type missing", src: set(""), code: CodeKeyMissing, path: "setting[0].type"},
		{name: "type password", src: set("type = \"password\"\n"), code: CodeValueInvalid},
		{name: "type dir reserved", src: set("type = \"dir\"\n"), code: CodeReserved},
		{name: "type secret_ref reserved", src: set("type = \"secret_ref\"\n"), code: CodeReserved},
		{
			name: "id token_env", src: strings.Replace(set("type = \"string\"\n"), "\"s\"",
				"\"token_env\"",
				1),
			code: CodeReserved, path: "setting[0].id",
		},
		{
			name: "id token_cmd", src: strings.Replace(set("type = \"string\"\n"), "\"s\"",
				"\"token_cmd\"",
				1),
			code: CodeReserved,
		},
		{
			name: "id schema", src: strings.Replace(set("type = \"string\"\n"), "\"s\"",
				"\"schema\"", 1),
			code: CodeIDInvalid, path: "setting[0].id",
		},
		{
			name: "id double underscore", src: strings.Replace(set("type = \"string\"\n"), "\"s\"",
				"\"a__b\"", 1),
			code: CodeIDInvalid, path: "setting[0].id",
		},
		{
			name: "id grammar", src: strings.Replace(set("type = \"string\"\n"), "\"s\"", "\"S-x\"",
				1),
			code: CodeIDInvalid,
		},
		{
			name: "id duplicate", src: set("type = \"string\"\n") +
				"[[setting]]\nid = \"s\"\nlabel = \"S\"\ntype = \"bool\"\n",
			code: CodeIDDuplicate, path: "setting[1].id",
		},
		{
			name: "enum without choices", src: set("type = \"enum\"\n"), code: CodeKeyMissing,
			path: "setting[0].choices",
		},
		{
			name: "enum duplicate", src: set("type = \"enum\"\nchoices = [\"a\", \"a\"]\n"),
			code: CodeValueInvalid,
		},
		{
			name: "enum default outside",
			src:  set("type = \"enum\"\nchoices = [\"a\"]\ndefault = \"b\"\n"),
			code: CodeValueInvalid,
		},
		{
			name: "enum too many", src: set("type = \"enum\"\nchoices = [" + times("\"a\",",
				65) + "]\n"),
			code: CodeLimitExceeded,
		},
		{
			name: "choices on a string", src: set("type = \"string\"\nchoices = [\"a\"]\n"),
			code: CodeKeyConflict, path: "setting[0].choices",
		},
		{name: "min on a string", src: set("type = \"string\"\nmin = 1\n"), code: CodeKeyConflict},
		{
			name: "must_exist on a string", src: set("type = \"string\"\nmust_exist = true\n"),
			code: CodeKeyConflict,
		},
		{
			name: "max_items on a string", src: set("type = \"string\"\nmax_items = 3\n"),
			code: CodeKeyConflict,
		},
		{
			name: "pattern on an int", src: set("type = \"int\"\npattern = \"^1$\"\n"),
			code: CodeKeyConflict,
		},
		{
			name: "pattern unanchored", src: set("type = \"string\"\npattern = \"[a-z]+\"\n"),
			code: CodePatternUnanchored, path: "setting[0].pattern",
		},
		{
			name: "list pattern unanchored", src: set("type = \"list\"\npattern = \"[a-z]+\"\n"),
			code: CodePatternUnanchored,
		},
		{
			name: "string default fails pattern",
			src:  set("type = \"string\"\npattern = \"^[a-z]+$\"\ndefault = \"A\"\n"),
			code: CodeValueInvalid, path: "setting[0].default",
		},
		{
			name: "string default too long",
			src:  set("type = \"string\"\nmax_len = 2\ndefault = \"abc\"\n"),
			code: CodeValueInvalid,
		},
		{name: "default type", src: set("type = \"bool\"\ndefault = \"yes\"\n"), code: CodeTypeInvalid},
		{name: "int bounds", src: set("type = \"int\"\nmin = 5\nmax = 1\n"), code: CodeValueInvalid},
		{
			name: "int default low", src: set("type = \"int\"\nmin = 5\ndefault = 1\n"),
			code: CodeValueInvalid,
		},
		{
			name: "int default high", src: set("type = \"int\"\nmax = 5\ndefault = 9\n"),
			code: CodeValueInvalid,
		},
		{
			name: "int out of range", src: set("type = \"int\"\nmax = 3000000000\n"),
			code: CodeLimitExceeded,
		},
		{
			name: "list default too long",
			src:  set("type = \"list\"\nmax_items = 1\ndefault = [\"a\", \"b\"]\n"),
			code: CodeLimitExceeded,
		},
		{
			name: "list item fails pattern",
			src:  set("type = \"list\"\npattern = \"^a$\"\ndefault = [\"b\"]\n"),
			code: CodeValueInvalid,
		},
		{
			name: "max_items too big", src: set("type = \"list\"\nmax_items = 501\n"),
			code: CodeLimitExceeded,
		},
		{
			name: "max_len too big", src: set("type = \"string\"\nmax_len = 1025\n"),
			code: CodeLimitExceeded,
		},
		{
			name: "label long", src: strings.Replace(set("type = \"string\"\n"), "\"S\"",
				"\""+times("a",
					61)+"\"", 1),
			code: CodeLimitExceeded,
		},
		{name: "unknown key", src: set("type = \"string\"\nsecret = true\n"), code: CodeUnknownKey},
	}

	runRules(t, tests)

	m := mustDecode(t, decl("", nil, ""+
		"[[setting]]\nid = \"a\"\ntype = \"string\"\nlabel = \"A\"\npattern = \"^[a-z]{0,5}$\"\ndefault "+
		"= \"\"\n"+
		"[[setting]]\nid = \"b\"\ntype = \"enum\"\nlabel = \"B\"\nchoices = [\"x\", \"y\"]\ndefault = "+
		"\"y\"\n"+
		"[[setting]]\nid = \"c\"\ntype = \"int\"\nlabel = \"C\"\nmin = -5\nmax = 5\ndefault = -5\n"+
		"[[setting]]\nid = \"d\"\ntype = \"list\"\nlabel = \"D\"\ndefault = [\"p\", \"q\"]\n"+
		"[[setting]]\nid = \"e\"\ntype = \"bool\"\nlabel = \"E\"\ndefault = true\n"+
		"[[setting]]\nid = \"f\"\ntype = \"path\"\nlabel = \"F\"\nmust_exist = true\n"))

	if len(m.Settings) != 6 || m.Settings[3].MaxItems != 100 || m.Settings[0].MaxLen != 256 {
		t.Errorf("settings = %+v", m.Settings)
	}

	if name := SettingEnvName("my-plug", "token_x"); name != "TOTE_PLUGIN_MY__PLUG_TOKEN_X" {
		t.Errorf("SettingEnvName = %q", name)
	}
}

// TestRulesActionsAndLists checks the exec tables.
//
// Parameters:
//   - t: test to report to
func TestRulesActionsAndLists(t *testing.T) {
	act := func(lines string) string {
		return execM("", nil, "[[action]]\nid = \"a\"\nlabel = \"A\"\n"+lines)
	}
	lst := func(lines string) string {
		return execM("", nil, "[[list]]\nid = \"l\"\nlabel = \"L\"\n"+lines)
	}
	col := "[[list.column]]\nid = \"c\"\nlabel = \"C\"\n"

	tests := []ruleCase{
		{name: "action output missing", src: act(""), code: CodeKeyMissing, path: "action[0].output"},
		{
			name: "action terminal reserved", src: act("output = \"terminal\"\n"),
			code: CodeReserved,
			path: "action[0].output",
		},
		{
			name: "action pause", src: act("output = \"pager\"\npause = \"never\"\n"),
			code: CodeKeyConflict,
		},
		{
			name: "action target connection",
			src:  act("output = \"pager\"\ntarget = \"connection\"\n"),
			code: CodeReserved,
		},
		{
			name: "action target repo needs permission",
			src:  act("output = \"pager\"\ntarget = \"repo\"\n"),
			code: CodePermMissing,
		},
		{
			name: "command terminal in exec tier", src: execM("", tool,
				cmd("run = [\"tool\"]\noutput = \"terminal\"\n")),
			code: CodeReserved,
		},
		{name: "list columns missing", src: lst(""), code: CodeKeyMissing, path: "list[0].column"},
		{name: "list too many columns", src: lst(times(col, 9)), code: CodeLimitExceeded},
		{
			name: "column id", src: lst("[[list.column]]\nid = \"C\"\nlabel = \"C\"\n"),
			code: CodeIDInvalid,
		},
		{name: "column duplicate", src: lst(col + col), code: CodeIDDuplicate},
		{name: "column width low", src: lst(col + "width = 3\n"), code: CodeLimitExceeded},
		{name: "column width high", src: lst(col + "width = 81\n"), code: CodeLimitExceeded},
		{name: "column align", src: lst(col + "align = \"center\"\n"), code: CodeValueInvalid},
		{
			name: "list timeout", src: lst("timeout = \"121s\"\n" + col), code: CodeLimitExceeded,
			path: "list[0].timeout",
		},
		{name: "list timeout minutes", src: lst("timeout = \"3m\"\n" + col), code: CodeLimitExceeded},
		{name: "list cwd", src: lst("cwd = \"{home}\"\n" + col), code: CodeUnknownKey},
		{name: "list target", src: lst("target = \"repo\"\n" + col), code: CodeUnknownKey},
		{
			name: "list action target",
			src: execM("", tool, "[[list]]\nid = \"l\"\nlabel = \"L\"\n"+col+
				"[[list.action]]\nid = \"a\"\nlabel = \"A\"\ntarget = \"repo\"\n"+run("")),
			code: CodeUnknownKey,
		},
		{
			name: "list action duplicate",
			src: execM("", tool, "[[list]]\nid = \"l\"\nlabel = \"L\"\n"+col+
				"[[list.action]]\nid = \"a\"\nlabel = \"A\"\n"+run("")+"[[list.action]]\nid = \"a\"\nlabel = "+
				"\"A\"\n"+run("")),
			code: CodeIDDuplicate, path: "list[0].action[1].id",
		},
		{
			name: "list action item data key", src: execM("", tool,
				"[[list]]\nid = \"l\"\nlabel = \"L\"\n"+col+
					"[[list.action]]\nid = \"a\"\nlabel = \"A\"\n"+run(", \"{item.data.Url}\"")),
			code: CodePlaceholderSyntax,
		},
		{
			name: "list action unknown item member", src: execM("", tool,
				"[[list]]\nid = \"l\"\nlabel = \"L\"\n"+col+
					"[[list.action]]\nid = \"a\"\nlabel = \"A\"\n"+run(", \"{item.name}\"")),
			code: CodePlaceholderUnknown,
		},
		{
			name: "list action field uses item", src: execM("", tool,
				"[[list]]\nid = \"l\"\nlabel = \"L\"\n"+col+
					"[[list.action]]\nid = \"a\"\nlabel = \"A\"\n"+run("")+
					"[[list.action.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"X\"\n"+
					"default = \"{item.id}\"\n"),
			code: CodePlaceholderScope,
		},
		{name: "list action needs program", src: execM("", nil,
			"[[list]]\nid = \"l\"\nlabel = \"L\"\n"+col+
				"[[list.action]]\nid = \"a\"\nlabel = \"A\"\n"+run("")), code: CodeExecUncovered},
		{name: "too many list actions", src: execM("", tool, "[[list]]\nid = \"l\"\nlabel = \"L\"\n"+col+
			manyListActions(17)), code: CodeLimitExceeded},
	}

	runRules(t, tests)

	ok := execM("", append([]string{"host:open-url"}, tool...),
		"[[list]]\nid = \"l\"\nlabel = \"L\"\ntimeout = \"2m\"\n"+col+
			"width = 4\nalign = \"right\"\n[[list.action]]\nid = \"a\"\nlabel = \"A\"\n"+run(", "+
			"\"{item.id}\", \"{item.data.url}\"")+
			"[[list.action]]\nid = \"o\"\nlabel = \"O\"\nopen = \"{item.data.url}\"\n"+
			"[[action]]\nid = \"r\"\nlabel = \"R\"\noutput = \"silent\"\nconfirm = true\ntimeout = "+
			"\"30s\"\n")

	m := mustDecode(t, ok)
	if len(m.Lists[0].Actions) != 2 || m.Lists[0].Columns[0].Width != 4 || !m.Actions[0].Confirm.Set {
		t.Errorf("exec tables = %+v %+v", m.Lists, m.Actions)
	}
}

// manyListActions builds n distinct list actions.
//
// Parameters:
//   - n: number of actions
func manyListActions(n int) string {
	var b strings.Builder

	for i := 0; i < n; i++ {
		b.WriteString("[[list.action]]\nid = \"a" + string(rune('a'+i)) + "\"\nlabel = \"A\"\n" + run(""))
	}

	return b.String()
}

// TestRulesCounts checks the number of contributions.
//
// Parameters:
//   - t: test to report to
func TestRulesCounts(t *testing.T) {
	many := func(n int) string {
		var b strings.Builder

		for i := 0; i < n; i++ {
			b.WriteString("[[command]]\nid = \"c" + itoa(i) +
				"\"\nlabel = \"C\"\nrun = [\"tool\"]\noutput = \"pager\"\n")
		}

		return b.String()
	}

	if m, rep := decode(decl("", tool, many(64))); m == nil {
		t.Errorf("64 contributions refused: %v", rep.Issues)
	}

	m, rep := decode(decl("", tool, many(65)))
	if m != nil || find(rep, CodeLimitExceeded, "") == nil {
		t.Errorf("65 contributions accepted: %v", rep.Issues)
	}

	mixed := many(60) + times("[[setting]]\nid = \"s\"\nlabel = \"S\"\ntype = \"bool\"\n", 5)
	for i := 0; i < 5; i++ {
		mixed = strings.Replace(mixed, "id = \"s\"", "id = \"s"+string(rune('a'+i))+"\"", 1)
	}

	if m, rep := decode(decl("", tool, mixed)); m != nil || find(rep, CodeLimitExceeded, "") == nil {
		t.Errorf("65 contributions of mixed kinds accepted: %v", rep.Issues)
	}
}

// TestRulesDeprecatedKeys checks that a deprecated key is a warning that names its replacement
// and does not make the manifest invalid, and that the same key is an error once it is no longer
// deprecated.
//
// Parameters:
//   - t: test to report to
func TestRulesDeprecatedKeys(t *testing.T) {
	saved := deprecatedKeys
	deprecatedKeys = map[string]string{"command.legacy": "run", "old_top": "license"}

	defer func() { deprecatedKeys = saved }()

	src := decl("old_top = 1\n", tool, cmd(run("")+"legacy = true\n"))

	m, rep := decode(src)
	if m == nil {
		t.Fatalf("deprecated keys refused the manifest: %v", rep.Issues)
	}

	w := find(rep, CodeDeprecatedKey, "command[0].legacy")
	if w == nil || w.Severity != SeverityWarning || len(w.Args) != 2 || w.Args[1] != "run" {
		t.Errorf("deprecated key issue = %+v", w)
	}

	if find(rep, CodeDeprecatedKey, "old_top") == nil {
		t.Errorf("root deprecated key not reported: %v", rep.Issues)
	}

	deprecatedKeys = map[string]string{}

	if m, _ := decode(src); m != nil {
		t.Error("a key that is no longer deprecated was accepted")
	}
}
