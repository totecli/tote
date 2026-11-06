package ui

import (
	"strconv"
	"strings"

	"github.com/totecli/tote/internal/plugin"
)

// cannedRun returns the overlay of a run in the state a frame shows, without starting a program:
// "done" is a run that ended well, "running" one that is still going, "failed" one that ended with
// status 3, "cut" one whose output was too long to keep, "colors" one that writes colors and the
// characters a terminal must never be sent, and "silent" a silent one that is going.
//
// Parameters:
//   - e: shared environment
//   - kind: the state
func cannedRun(e *env, kind string) *runView {
	prep := plugin.Prepared{Output: plugin.OutputPager}
	if kind == "silent" {
		prep.Output = plugin.OutputSilent
	}

	run := &plugRun{seq: 99, start: pluginStart{Plugin: "gft", Label: "List stashes"}, prep: prep}
	v := newRunView(e, run)

	switch kind {
	case "colors":
		v.text.add("plain \x1b[31mred\x1b[0m \x1b[1;32mgreen\x1b[0m\n")
		v.text.add("evil \x1b]0;title\x07 \x1b[2J \x1b[10;10H \u202e text\x00 end\n")
		v.text.add("a\ttab and a very long line that has to be wrapped inside the box of the " +
			"pager\n")
	default:
		for i := 0; i < 40; i++ {
			v.text.add("stash@{" + strconv.Itoa(i) + "}: WIP on main: " +
				strings.Repeat("work ", i%5+1) + "\n")
		}
	}

	if kind == "cut" {
		v.text.cut = true
		v.text.note("1 line is not shown")
	}

	switch kind {
	case "running", "silent":
		v.spin = 1
	case "failed":
		v.running = false
		v.status, _ = e.outcomeText("gft", plugin.Outcome{Status: plugin.OutcomeFailed, Exit: 3}, 0)
	default:
		v.running = false
		v.status, _ = e.outcomeText("gft", plugin.Outcome{Status: plugin.OutcomeOK}, 0)
	}

	return v
}

// pluginRunFrames lists the frames of the screens that run a contribution of a plugin: the
// commands in the palette, the menu of the actions of a repository, the forms, the questions
// before a run and the output of a run. The repository proj is at /usr, not a git repository, so
// the actions of gft that need a GitHub remote are greyed, in the sandbox of dataPlugRun.
//
// The actions of gft in the menu are, in order: release, assets, notes, stash-list, stash-show,
// stash-drop and stash-clean.
func pluginRunFrames() []frameCase {
	downs := func(n int, rest ...string) []string {
		keys := make([]string, n)
		for i := range keys {
			keys[i] = "down"
		}

		return append(keys, rest...)
	}

	cases := []struct {
		name  string
		keys  []string
		looks bool
	}{
		{"runpalette", []string{"ctrl+k", "type:release"}, true},
		{"runpalettestash", []string{"ctrl+k", "type:open the"}, false},
		{"runmenu", []string{"tab"}, true},
		{"runmenuend", []string{"tab", "end"}, false},
		{"runmenurefused", []string{"tab", "enter"}, true},
		{"runform", append([]string{"tab"}, downs(5, "enter")...), true},
		{
			"runformbad", append(append([]string{"tab"}, downs(5, "enter")...), "type:x", "enter"),
			false,
		},
		{
			"runconfirm", append(append([]string{"tab"}, downs(5, "enter")...), "type:3", "enter"),
			true,
		},
		{"runconfirmyes", append(append([]string{"tab"}, downs(5, "enter")...),
			"type:3", "enter", "tab"), false},
		{
			"runopen",
			[]string{"ctrl+k", "type:releases page", "enter", "type:demo/proj", "enter"},
			true,
		},
		{"runpager", []string{"pager:done"}, true},
		{"runpagerscroll", []string{"pager:done", "pgdown"}, false},
		{"runpagerend", []string{"pager:done", "end"}, false},
		{"runpagerrunning", []string{"pager:running"}, true},
		{"runpagerfailed", []string{"pager:failed"}, true},
		{"runpagercut", []string{"pager:cut", "end"}, false},
		{"runpagercolors", []string{"pager:colors"}, true},
		{"runsilent", []string{"pager:silent"}, false},
	}

	var out []frameCase

	for _, c := range cases {
		looks := pluginLooks[:1]
		if c.looks {
			looks = pluginLooks
		}

		for _, look := range looks {
			out = append(out, frameCase{
				name: c.name + look.suffix, data: dataPlugRun,
				start: StartRepos, keys: c.keys, sizes: pluginSizes,
				check: checkedBesides(pluginSizes), setup: look.setup,
			})
		}
	}

	return out
}
