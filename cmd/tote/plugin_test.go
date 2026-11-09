package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/plugin"
	"github.com/totecli/tote/internal/ui"
)

// listed prepares the sandbox the list and info goldens read: two installed plugins, one of them
// turned off, and gft shipped with tote.
//
// Parameters:
//   - t: running test
func listed(t *testing.T) *pluginEnv {
	t.Helper()

	e := newPluginEnv(t).withBundled()
	e.install(e.plugin("demo", "1.0.0", "exec:echo"))
	e.install(e.plugin("zed", "2.1.0", "exec:echo", "net:example.org"))
	e.must("plugin", "disable", "zed")

	return e
}

// TestPluginListFormats runs the list verb in each format, for each spelling of the verb, and
// compares the text with the reference files.
//
// Parameters:
//   - t: test to report to
func TestPluginListFormats(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		t.Run("table "+lang, func(t *testing.T) {
			e := listed(t)
			e.vars["TOTE_LANG"] = lang
			e.tty = true

			rc, out, errs := e.do("plugin")
			if rc != 0 || errs != "" {
				t.Fatalf("exit %d, stderr %q", rc, errs)
			}

			golden(t, "plugin_list_"+lang+".txt", out)

			for _, args := range [][]string{
				{"plugins"},
				{"plugin", "list"},
				{"plugin", "ls"},
				{"plugin", "--format", "table"},
			} {
				if _, again, _ := e.do(args...); again != out {
					t.Errorf("%v differs from the bare verb:\n%s", args, again)
				}
			}
		})
	}

	e := listed(t)

	_, tsv, _ := e.do("plugin")
	golden(t, "plugin_list.tsv", tsv)

	_, j, _ := e.do("plugin", "--json")
	golden(t, "plugin_list.json", j)

	if _, same, _ := e.do("plugin", "list", "--format", "json"); same != j {
		t.Errorf("--format json differs from --json")
	}
}

// TestPluginListJSONShape checks the keys of the plugin object of the list.
//
// Parameters:
//   - t: test to report to
func TestPluginListJSONShape(t *testing.T) {
	e := listed(t)

	var doc struct {
		Schema  int              `json:"schema"`
		Plugins []map[string]any `json:"plugins"`
	}

	if err := json.Unmarshal([]byte(e.must("plugin", "--json")), &doc); err != nil {
		t.Fatal(err)
	}

	if doc.Schema != 1 || len(doc.Plugins) != 3 {
		t.Fatalf("schema %d, %d plugins", doc.Schema, len(doc.Plugins))
	}

	want := []string{
		"id", "title", "version", "tier", "kind", "origin", "state", "reasons",
		"enabled", "consented", "caps", "grant", "contributes",
	}

	for _, p := range doc.Plugins {
		for _, k := range want {
			if _, ok := p[k]; !ok {
				t.Errorf("plugin %v lacks the key %s", p["id"], k)
			}
		}

		if len(p) != len(want) {
			t.Errorf("plugin %v has %d keys, want %d", p["id"], len(p), len(want))
		}

		c, _ := p["contributes"].(map[string]any)
		for _, k := range []string{"item_actions", "commands", "lists", "settings"} {
			if _, ok := c[k]; !ok {
				t.Errorf("contributes lacks %s", k)
			}
		}
	}

	states := map[string]string{}
	for _, p := range doc.Plugins {
		states[p["id"].(string)] = p["state"].(string)
	}

	want2 := map[string]string{"demo": "enabled", "zed": "disabled", "gft": "available"}
	for id, s := range want2 {
		if states[id] != s {
			t.Errorf("state of %s = %q, want %q", id, states[id], s)
		}
	}
}

// TestPluginListUsage checks the options of the list verb that are refused.
//
// Parameters:
//   - t: test to report to
func TestPluginListUsage(t *testing.T) {
	e := newPluginEnv(t)

	for _, args := range [][]string{
		{"plugin", "--format", "xml"}, {"plugin", "list", "extra"}, {"plugin", "--nope"},
	} {
		if rc, out, _ := e.do(args...); rc != exitUsage || out != "" {
			t.Errorf("%v: exit %d, stdout %q", args, rc, out)
		}
	}

	rc, out, _ := e.do("plugin", "--json", "--format", "tsv")
	if rc != exitUsage || !strings.Contains(out, "--json cannot be combined with --format tsv") {
		t.Errorf("--json with --format tsv: exit %d, stdout %q", rc, out)
	}
}

// TestPluginInfo shows the details of an installed plugin, of one shipped with tote and of one
// that does not exist.
//
// Parameters:
//   - t: test to report to
func TestPluginInfo(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		e := listed(t)
		e.vars["TOTE_LANG"] = lang

		rc, out, _ := e.do("plugin", "info", "zed")
		if rc != 0 {
			t.Fatalf("info zed: exit %d", rc)
		}

		golden(t, "plugin_info_"+lang+".txt", out)
	}

	e := listed(t)
	golden(t, "plugin_info.json", e.must("plugin", "info", "demo", "--json"))
	golden(t, "plugin_info_available.txt", e.must("plugin", "info", "gft"))

	rc, out, errs := e.do("plugin", "info", "dem")
	if rc != exitFail || out != "" || !strings.Contains(errs, `did you mean "demo"?`) {
		t.Errorf("info dem: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	rc, out, _ = e.do("plugin", "info", "nope", "--json")
	if rc != exitFail || !strings.Contains(out, `"code": "not_installed"`) {
		t.Errorf("info nope --json: exit %d, stdout %q", rc, out)
	}

	if rc, _, _ = e.do("plugin", "info"); rc != exitUsage {
		t.Errorf("info without id: exit %d", rc)
	}
}

// TestPluginErrorObject checks the JSON object of a failure, for a failure of the plugin layer and
// for a wrong command line.
//
// Parameters:
//   - t: test to report to
func TestPluginErrorObject(t *testing.T) {
	e := newPluginEnv(t)

	tests := []struct {
		args []string
		code string
		exit int
	}{
		{[]string{"plugin", "info", "nope", "--json"}, "not_installed", 1},
		{[]string{"plugin", "install", "x", "--json"}, "bad_source", 2},
		{[]string{"plugin", "enable", "--json"}, "usage", 2},
		{[]string{"plugin", "disable", "a", "b", "--json"}, "usage", 2},
		{[]string{"plugin", "frobnicate", "--json"}, "usage", 2},
		{[]string{"plugin", "list", "--bogus", "--json"}, "usage", 2},
	}

	for _, tc := range tests {
		rc, out, errs := e.do(tc.args...)

		var doc struct {
			Schema int  `json:"schema"`
			OK     bool `json:"ok"`
			Error  struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Exit    int    `json:"exit"`
			} `json:"error"`
		}

		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Errorf("%v: %v in %q", tc.args, err, out)

			continue
		}

		if rc != tc.exit || doc.Schema != 1 || doc.OK || doc.Error.Code != tc.code ||
			doc.Error.Exit != tc.exit || doc.Error.Message == "" || errs != "" {
			t.Errorf("%v: exit %d, %+v, stderr %q", tc.args, rc, doc, errs)
		}
	}
}

// TestPluginRunIsReserved checks that the verb run is not built: it is an unknown subverb, and
// the id run cannot be taken by a plugin.
//
// Parameters:
//   - t: test to report to
func TestPluginRunIsReserved(t *testing.T) {
	e := newPluginEnv(t)

	rc, out, errs := e.do("plugin", "run", "demo", "list")
	if rc != exitUsage || out != "" || !strings.Contains(errs, `unknown subcommand "run"`) {
		t.Errorf("exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	if slices.Contains(pluginVerbs, "run") {
		t.Error("run is in the list of subverbs")
	}
}

// TestPluginUnknownSubverb checks the suggestion for a mistyped subverb.
//
// Parameters:
//   - t: test to report to
func TestPluginUnknownSubverb(t *testing.T) {
	e := newPluginEnv(t)

	rc, _, errs := e.do("plugin", "instal", "x")
	if rc != exitUsage || !strings.Contains(errs, `did you mean "install"?`) {
		t.Errorf("exit %d, stderr %q", rc, errs)
	}
}

// TestPluginHelp checks the help of the plugin verb, in the root help and on its own, in both
// languages and under both spellings.
//
// Parameters:
//   - t: test to report to
func TestPluginHelp(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		e := newPluginEnv(t)
		e.vars["TOTE_LANG"] = lang
		p := i18n.New(lang)

		root := e.must("help")
		for _, key := range []string{"help_cmd_plugin", "help_opt_noplugins"} {
			if !strings.Contains(root, p.T(key)) {
				t.Errorf("%s: the root help lacks %s", lang, key)
			}
		}

		for _, args := range [][]string{
			{"help", "plugin"},
			{"help", "plugins"},
			{"plugin", "-h"},
			{"plugin", "install", "--help"},
			{"plugin", "check", "-h"},
		} {
			out := e.must(args...)

			for _, key := range []string{
				"usage_plugin_list", "usage_plugin_info",
				"usage_plugin_install", "usage_plugin_update", "usage_plugin_enable",
				"usage_plugin_disable", "usage_plugin_remove", "usage_plugin_check",
				"usage_plugin_exit",
			} {
				if !strings.Contains(out, p.T(key)) {
					t.Errorf("%s %v lacks %s", lang, args, key)
				}
			}
		}

		if lang == "en" {
			golden(t, "plugin_help_en.txt", e.must("help", "plugin"))
		} else {
			golden(t, "plugin_help_fr.txt", e.must("help", "plugin"))
		}
	}
}

// TestPluginReservedIDs checks that every name the command line answers to, derived from the
// command table, is a reserved plugin id, and that the subverbs of the plugin verb cannot be
// confused with a plugin either.
//
// Parameters:
//   - t: test to report to
func TestPluginReservedIDs(t *testing.T) {
	seen := 0

	for _, c := range commands() {
		for _, n := range c.names {
			seen++

			if plugin.ReservedID(n) == "" {
				t.Errorf("the command line answers to %q but it is not a reserved plugin id", n)
			}
		}
	}

	if seen < 20 {
		t.Errorf("only %d names read from the command table", seen)
	}

	if plugin.ReservedID("plugin") == "" || plugin.ReservedID("plugins") == "" ||
		plugin.ReservedID("run") != "" && !slices.Contains(pluginVerbs, "run") {
		t.Error("plugin, plugins and run must stay reserved")
	}
}

// TestPluginNoPlugins checks the session switch: --no-plugins before the verb, and the variable.
// Nothing is refused for reading, enable is refused (exit 4), and a plugin is disabled with the
// reason.
//
// Parameters:
//   - t: test to report to
func TestPluginNoPlugins(t *testing.T) {
	e := newPluginEnv(t)
	e.install(e.plugin("demo", "1.0.0", "exec:echo"))

	for _, tc := range []struct {
		name string
		args []string
		env  string
	}{
		{"flag", []string{"--no-plugins", "plugin", "--json"}, ""},
		{"variable", []string{"plugin", "--json"}, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.vars["TOTE_NO_PLUGINS"] = tc.env
			defer delete(e.vars, "TOTE_NO_PLUGINS")

			out := e.must(tc.args...)
			if !strings.Contains(out, `"state": "disabled"`) ||
				!strings.Contains(out, `"code": "session_off"`) {
				t.Errorf("list under the switch:\n%s", out)
			}
		})
	}

	e.vars["TOTE_NO_PLUGINS"] = "0"

	if out := e.must("plugin", "--json"); !strings.Contains(out, `"state": "enabled"`) {
		t.Errorf("TOTE_NO_PLUGINS=0 turned the plugins off:\n%s", out)
	}

	delete(e.vars, "TOTE_NO_PLUGINS")

	rc, _, errs := e.do("--no-plugins", "plugin", "enable", "demo")
	if rc != exitRefused || !strings.Contains(errs, "off for this run") ||
		!strings.Contains(errs, "without --no-plugins") {
		t.Errorf("enable under the switch: exit %d, stderr %q", rc, errs)
	}

	if rc, _, _ := e.do("plugin", "--no-plugins"); rc != exitUsage {
		t.Errorf("--no-plugins after the verb: exit %d, want %d", rc, exitUsage)
	}

	if rc, out, _ := e.do("--no-plugins", "plugin", "info", "demo"); rc != 0 ||
		!strings.Contains(out, "disabled") {
		t.Errorf("info under the switch: exit %d, stdout %q", rc, out)
	}
}

// TestPluginFromPlugin checks that a mutating verb is refused, exit 4, when it is called from a
// plugin, whatever the verb.
//
// Parameters:
//   - t: test to report to
func TestPluginFromPlugin(t *testing.T) {
	e := newPluginEnv(t)
	dir := e.plugin("demo", "1.0.0", "exec:echo")
	e.install(dir)

	e.vars["TOTE_PLUGIN_ID"] = "demo"

	for _, args := range [][]string{
		{"plugin", "install", dir, "--force", "--grant", "x"},
		{"plugin", "disable", "demo"},
		{"plugin", "enable", "demo"},
		{"plugin", "remove", "demo", "--yes"},
		{"plugin", "update", "demo", "--grant", "x"},
	} {
		rc, _, errs := e.do(args...)
		if rc != exitRefused || !strings.Contains(errs, "started by a plugin") {
			t.Errorf("%v: exit %d, stderr %q", args, rc, errs)
		}
	}

	if rc, _, _ := e.do("plugin", "list"); rc != 0 {
		t.Errorf("list from a plugin: exit %d", rc)
	}
}

// TestPluginCompletion checks the completion of the subverbs, the options and the plugin ids.
//
// Parameters:
//   - t: test to report to
func TestPluginCompletion(t *testing.T) {
	e := listed(t)
	e.fixtures()

	tests := []struct {
		words []string
		want  []string
		not   []string
	}{
		{[]string{"plugin", ""}, pluginVerbs, []string{"run", "--json"}},
		{[]string{"plugins", "ch"}, []string{"check"}, []string{"install"}},
		{[]string{"plugin", "-"}, []string{"--format", "--json"}, nil},
		{
			[]string{"plugin", "install", "-"},
			[]string{"--sha256", "--grant", "--dry-run", "--force", "--json"},
			nil,
		},
		{
			[]string{"plugin", "install", "--force", "--"},
			[]string{"--sha256", "--grant", "--dry-run", "--json"},
			[]string{"--force"},
		},
		{
			[]string{"plugin", "check", "-"},
			[]string{"--strict", "--official", "--argv", "--set", "--repo", "--json"},
			nil,
		},
		{[]string{"plugin", "remove", "-"}, []string{"--yes", "--purge", "--json"}, nil},
		{[]string{"plugin", "enable", ""}, []string{"demo", "zed"}, []string{"gft"}},
		{[]string{"plugin", "info", "d"}, []string{"demo"}, []string{"zed"}},
		{[]string{"plugin", "install", ""}, []string{"gft"}, []string{"demo"}},
		{[]string{"plugin", "check", ""}, []string{"demo", "zed", "gft"}, nil},
		{[]string{"plugin", "enable", "demo", ""}, nil, []string{"demo", "zed"}},
		{[]string{"plugin", "install", "--grant", ""}, nil, []string{"gft"}},
		{[]string{"plugin", "update", "demo", ""}, nil, []string{"demo"}},
	}

	for _, tc := range tests {
		got := names(completeLines(t, e.harness, tc.words...))

		for _, w := range tc.want {
			if !slices.Contains(got, w) {
				t.Errorf("%v: %q is missing from %v", tc.words, w, got)
			}
		}

		for _, w := range tc.not {
			if slices.Contains(got, w) {
				t.Errorf("%v: %q must not be offered, got %v", tc.words, w, got)
			}
		}
	}

	if verbs := names(completeLines(t, e.harness, "pl")); !slices.Contains(verbs, "plugin") ||
		!slices.Contains(verbs, "plugins") {
		t.Errorf("the verb is not completed: %v", verbs)
	}
}

// TestPluginMessagesRender renders every code of the plugin layer with no argument, with one and
// with several, in both languages, and checks that nothing is left raw: no missing key, no
// Printf error, no empty message.
//
// Parameters:
//   - t: test to report to
func TestPluginMessagesRender(t *testing.T) {
	consts := map[string]bool{}
	for _, c := range allCodes(t) {
		consts[c] = true
	}

	for _, lang := range []string{"en", "fr"} {
		e := newPluginEnv(t)
		e.vars["TOTE_LANG"] = lang

		for code := range consts {
			for _, args := range [][]string{nil, {"a"}, {"a", "b"}, {"a", "b", "c", "d"}} {
				msg := e.app.errText(code, args, nil)

				if msg == "" || strings.Contains(msg, "%!") || strings.Contains(msg, "!plugin_") {
					t.Errorf("%s %s %v: %q", lang, code, args, msg)
				}

				if _, ok := e.app.tr().Code(i18n.PrefixError, code, args); !ok {
					t.Errorf("%s: no message for %s", lang, code)
				}
			}
		}
	}
}

// TestPluginReasonsRender renders every reason of a state in both languages.
//
// Parameters:
//   - t: test to report to
func TestPluginReasonsRender(t *testing.T) {
	reasons := constValues(t, "Reason")
	if len(reasons) < 20 {
		t.Fatalf("only %d reasons read", len(reasons))
	}

	for _, lang := range []string{"en", "fr"} {
		e := newPluginEnv(t)
		e.vars["TOTE_LANG"] = lang

		for _, code := range reasons {
			for _, args := range [][]string{nil, {"a"}, {"a", "b", "c"}} {
				msg := e.app.reasonText(plugin.Reason{Code: code, Args: args})

				if msg == "" || strings.Contains(msg, "%!") || msg == code {
					t.Errorf("%s %s %v: %q", lang, code, args, msg)
				}
			}
		}
	}
}

// TestPluginOpensScreen checks that a bare "tote plugin" opens the plugins screen on a terminal
// with the registry as the plugin host, prints the list when the output is not a terminal, and
// that a verb never opens the interface.
//
// Parameters:
//   - t: test to report to
func TestPluginOpensScreen(t *testing.T) {
	e := newPluginEnv(t).withBundled()
	p := withUI(e.harness)

	if rc := e.run("plugin"); rc != exitOK || len(p.calls) != 1 {
		t.Fatalf("plugin on a terminal: exit %d, %d starts, stderr %q", rc, len(p.calls),
			e.errb.String())
	}

	if o := p.calls[0]; o.Start != ui.StartPlugins || o.Plugins == nil {
		t.Errorf("start %v, host %v, want the plugins screen with a host", o.Start, o.Plugins)
	}

	e.run("plugin", "list")
	e.run("plugin", "info", "nope")

	if len(p.calls) != 1 {
		t.Errorf("a verb opened the interface: %d starts", len(p.calls))
	}

	e.tty, e.ttyIn = false, false
	e.out.Reset()

	if rc := e.run("plugin"); rc != exitOK || len(p.calls) != 1 || e.out.Len() == 0 {
		t.Errorf("plugin without a terminal: exit %d, %d starts, stdout %q", rc, len(p.calls),
			e.out.String())
	}
}

// TestHomeKnowsThePlugins checks that the welcome page gets the plugin host too, so that its
// plugins row exists.
//
// Parameters:
//   - t: test to report to
func TestHomeKnowsThePlugins(t *testing.T) {
	h := newHarness(t)
	p := withUI(h)

	if rc := h.run(); rc != exitOK || len(p.calls) != 1 || p.calls[0].Plugins == nil {
		t.Errorf("exit %d, calls %+v", rc, p.calls)
	}
}
