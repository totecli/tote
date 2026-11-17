package ui

import (
	"strings"

	"github.com/totecli/tote/internal/plugin"
)

// longArgv is an argv too long for the status line of the form and for the box that loads.
const longArgv = "gft --releases --plain --dir=/home/tester/Downloads/" +
	"a-very-long-directory-name/with/many/parts -- demo/a-repository-with-a-long-name"

// cannedSuggest returns the form of a command with suggestions in the state a frame shows,
// without starting a program. The form has three fields, Repository, Tag and Asset; the keys are on
// Tag, which lists its suggestions, and Repository holds a value. The kinds are idle (the hint of
// the key), preparing, loading (the program runs), long (the same with an argv too long for the
// line), waiting (an earlier field is not valid), waitingrepo (no repository), failed (a program
// that exits with a status and a line), signal, timeout, toolarge, empty, ready (a list was
// shown), nomatch (a list that holds nothing that is typed), limit and stopped; the picker kinds
// are picker, pickerhidden (a list with dropped and uncounted entries), pickerfiltered,
// pickernone and pickerlong (long values and labels, which a wide terminal shows whole). The
// loading box over the form is wait (the program just started), waitsecs (it has run for seven
// seconds, the spinner on its third frame) and waitlong (an argv too long for the box).
//
// Parameters:
//   - e: shared environment
//   - kind: the state
func cannedSuggest(e *env, kind string) (overlay, *fieldsForm) {
	specs := []fieldSpec{
		{ID: "repo", Label: "Repository", Type: fieldText, Value: "demo/proj", Required: true},
		{
			ID: "tag", Label: "Tag", Type: fieldText, Suggest: true, Max: 24,
			Hint: "A release tag, or nothing for the latest",
		},
		{ID: "asset", Label: "Asset", Type: fieldText},
	}

	f := newFieldsForm("demo: Get a release", specs, "", "", func(fieldValues) string { return "" })
	f.runs, f.wide = true, true
	f.sug = &suggestState{
		memo: map[string]plugin.ChoicesResult{}, starts: map[string]int{}, field: "tag",
	}
	f.focusOn(e, 1)
	f.Init(e)

	s := f.sug
	argv := "gft --releases --plain -- demo/proj"

	res := plugin.ChoicesResult{State: plugin.ChoicesList, Items: []plugin.Choice{
		{Value: "v1.4.0", Label: "recommended"},
		{Value: "v1.3.2"},
		{Value: "v1.3.1"},
		{Value: "v1.3.0", Label: "pre-release"},
		{Value: "v1.2.0"},
		{Value: "v1.1.0"},
		{Value: "v1.0.0"},
	}, Hidden: 2}

	switch kind {
	case "preparing":
		s.phase = sugPreparing
	case "loading":
		s.phase, s.argv = sugRunning, argv
	case "long":
		s.phase, s.argv = sugRunning, longArgv
	case "wait", "waitsecs", "waitlong":
		s.phase, s.argv = sugRunning, argv

		if kind == "waitsecs" {
			s.spin, s.secs = 2, 7
		}

		if kind == "waitlong" {
			s.argv = longArgv
		}

		return newSuggestWait(e, f, "tag"), f
	case "waiting":
		s.phase, s.text = sugWaiting, f.waitText(e, "repo")
	case "waitingrepo":
		s.phase, s.text = sugWaiting, f.waitText(e, "repo.slug")
	case "failed":
		s.phase, s.text = sugFailed, e.choicesFailure(plugin.ChoicesResult{
			Reason: plugin.ChoicesReasonExit, Arg: "1", Detail: "Unknown option: --plain",
		})
	case "signal":
		s.phase, s.text = sugFailed, e.choicesFailure(plugin.ChoicesResult{
			Reason: plugin.ChoicesReasonSignal, Arg: "KILL",
		})
	case "timeout":
		s.phase, s.text = sugFailed, e.choicesFailure(plugin.ChoicesResult{
			State: plugin.ChoicesTimedOut, Reason: "timeout", Detail: "Resolving github.com",
		})
	case "toolarge":
		s.phase, s.text = sugFailed, e.choicesFailure(plugin.ChoicesResult{
			Reason: plugin.ChoicesReasonTooLarge,
		})
	case "empty":
		s.phase, s.hidden = sugEmpty, 3
	case "ready":
		s.phase, s.shown, s.hidden, s.more = sugReady, 7, 2, 40
	case "nomatch":
		s.phase, s.shown, s.hidden = sugNoMatch, 7, 2
	case "limit":
		s.phase = sugLimit
	case "stopped":
		s.phase = sugStopped
	case "picker", "pickerhidden", "pickerfiltered", "pickernone", "pickerlong":
		if kind == "pickerhidden" || kind == "pickerlong" {
			res.More = 40
		}

		if kind == "pickerlong" {
			res.Items = longChoices()
		}

		if kind != "pickerhidden" && kind != "pickerlong" {
			res.Hidden = 0
		}

		switch kind {
		case "pickerfiltered":
			f.fields[1].setText("1.3")
		case "pickernone":
			f.fields[1].setText("zzz")
		}

		pick, _ := f.pickList(e, "tag", res)

		return pick, f
	}

	return f, f
}

// pluginSuggestFrames lists the frames of the form that lists suggestions, in each state, and of
// the picker, in the sandbox of dataPlugRun.
func pluginSuggestFrames() []frameCase {
	kinds := []struct {
		name  string
		looks bool
	}{
		{"idle", true},
		{"preparing", false},
		{"loading", true},
		{"long", false},
		{"wait", true},
		{"waitsecs", false},
		{"waitlong", false},
		{"waiting", true},
		{"waitingrepo", false},
		{"failed", true},
		{"signal", false},
		{"timeout", false},
		{"toolarge", false},
		{"empty", true},
		{"ready", true},
		{"nomatch", false},
		{"limit", true},
		{"stopped", false},
		{"picker", true},
		{"pickerhidden", true},
		{"pickerfiltered", false},
		{"pickernone", false},
	}

	var out []frameCase

	for _, k := range kinds {
		looks := pluginLooks[:1]
		if k.looks {
			looks = pluginLooks
		}

		for _, look := range looks {
			out = append(out, frameCase{
				name: "suggest" + k.name + look.suffix, data: dataPlugRun, start: StartHome,
				keys: []string{"suggest:" + k.name}, sizes: pluginSizes,
				check: checkedBesides(pluginSizes), setup: look.setup,
			})
		}
	}

	return out
}

// playSuggest pushes the canned form or picker that a "suggest:" step of a frame stands for.
//
// Parameters:
//   - g: rig to drive
//   - step: the step, such as suggest:loading
func playSuggest(g *rig, step string) {
	g.t.Helper()

	ov, _ := cannedSuggest(g.r.e, strings.TrimPrefix(step, "suggest:"))
	g.r.push(ov)
}
