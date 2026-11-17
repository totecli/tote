package ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/plugin"
)

// dataWide is the data set of the frames of the boxes that grow on a wide terminal: ASCII
// connections, an alias with a very long command and a repository with a very long path.
const dataWide = dataPlugRun + 1

// The sizes of the reference frames of the boxes that adapt to the terminal: wide enough for a
// box to reach its widest, and wide and tall enough for the palette to reach its tallest.
var (
	wideBoxSize  = [2]int{200, 40}
	wideBoxTall  = [2]int{200, 50}
	wideBoxCheck = checkedBesides([][2]int{wideBoxSize})
)

// writeWideData writes the files of dataWide.
//
// Parameters:
//   - t: running test
//   - s: sandbox to write in
func writeWideData(t *testing.T, s *sandbox) {
	t.Helper()

	cut := strings.Index(fixtureConns, "[[connection]]\nname = \"emoji\"")
	writeFile(t, s.Opts.Paths.ConnectionsFile(), fixtureConns[:cut])
	writeFile(t, s.Opts.Paths.AliasesFile(), "schema = 1\n\n[[alias]]\nname = \"ll\"\n"+
		"cmd = \"ls -la\"\n\n[[alias]]\nname = \"backup\"\ncmd = \"rsync -az --delete "+
		"--exclude=node_modules --exclude=.cache /srv/work/proj/ backup.example.com:/srv/backups/"+
		"proj/\"\n")
	writeFile(t, s.Opts.Paths.ReposFile(), "schema = 1\n\n[[repo]]\nname = \"proj\"\n"+
		"path = \"/srv/work/team-platform/services/billing/proj\"\ndesc = \"This tool\"\n")
}

// longChoices returns the values of a list of suggestions whose values and labels are long.
func longChoices() []plugin.Choice {
	out := make([]plugin.Choice, 0, 30)

	for i := 0; i < 30; i++ {
		n := strconv.Itoa(30 - i)
		out = append(out, plugin.Choice{
			Value: "v1." + n + ".0-rc.1+build.20261103.0123456789abcdef",
			Label: "release candidate " + n + " cut from the maintenance branch of the autumn rebuild",
		})
	}

	return out
}

// cannedMenu returns the menu of the actions of a repository with long labels, a key in front of
// the first, and one action that cannot run.
//
// Parameters:
//   - e: shared environment
func cannedMenu(e *env) *actionMenu {
	offers := []plugin.Offer{
		{
			Plugin: "gft", ID: "release", Label: "Show the latest release of the repository",
			Key: "r", Available: true,
		},
		{
			Plugin: "gft", ID: "notes", Label: "Write the notes of the next release from the commits",
			Key: "n", Form: true, Available: true,
		},
		{
			Plugin: "gft", ID: "stash-list", Label: "List the stashes of the repository",
			Available: true,
		},
		{
			Plugin: "gft", ID: "stash-drop", Label: "Drop a stash", Confirm: true,
			Why: &plugin.Reason{Code: plugin.CodeDepMissing, Args: []string{"gft", "install gft"}},
		},
	}

	return newActionMenu(e, "proj", offers)
}

// wideBoxFrames lists the reference frames of the boxes that adapt to a wide terminal, each in
// English, in French and in ASCII.
func wideBoxFrames() []frameCase {
	var out []frameCase

	for _, look := range pluginLooks {
		out = append(out,
			frameCase{
				name: "palettelong" + look.suffix, data: dataWide, start: StartConnections,
				keys: []string{"ctrl+k"}, sizes: [][2]int{wideBoxTall},
				check: checkedBesides([][2]int{wideBoxTall}), setup: look.setup,
			},
			frameCase{
				name: "palettelongquery" + look.suffix, data: dataWide, start: StartConnections,
				keys: []string{"ctrl+k", "type:ba"}, sizes: [][2]int{wideBoxSize},
				check: wideBoxCheck, setup: look.setup,
			},
			frameCase{
				name: "runmenuwide" + look.suffix, data: dataPlugRun, start: StartRepos,
				keys: []string{"tab", "down"}, sizes: [][2]int{wideBoxSize},
				check: wideBoxCheck, setup: look.setup,
			},
			frameCase{
				name: "menulong" + look.suffix, data: dataPlugRun, start: StartRepos,
				keys: []string{"menu:long", "end"}, sizes: [][2]int{wideBoxSize},
				check: wideBoxCheck, setup: look.setup,
			},
			frameCase{
				name: "suggestpickerlong" + look.suffix, data: dataPlugRun, start: StartHome,
				keys: []string{"suggest:pickerlong"}, sizes: [][2]int{wideBoxSize},
				check: wideBoxCheck, setup: look.setup,
			},
			frameCase{
				name: "suggestwaitwide" + look.suffix, data: dataPlugRun, start: StartHome,
				keys: []string{"suggest:waitlong"}, sizes: [][2]int{wideBoxSize},
				check: wideBoxCheck, setup: look.setup,
			},
		)
	}

	return out
}
