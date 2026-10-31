package ui

import (
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/plugin"
	"github.com/totecli/tote/internal/shell"
)

// env is the state that screens and overlays read and never write.
//
// The root model owns it and updates Theme, Metric, Width, Cols, Margin, Height and Layout when
// the terminal or the settings change; everything else is fixed for the run. Data is the one
// exception to read-only: overlays write records through it. Cols and Height are the size of the
// terminal in cells. Width is the width of the page that screens draw, the terminal width up to
// the cap that ui.width gives, see pageGeometry, and Margin the blank cells at the left of a page
// narrower than the terminal.
// Layout is the mode chosen for the terminal size. Now returns the current time and Environ is
// the environment given to the programs that are started. Probe runs "ssh -G" when a connection
// starts. Profile is the color profile of the terminal, which the settings screen reports.
// Writes counts the writes that run off the event loop and have not ended. Steps holds the values
// that the keys of the settings screen asked for, see stepState. Plugins is the plugin registry,
// nil when plugins are off, and Plug caches the counts of the welcome page. Handing is true from
// the moment an overlay or a screen hands a plugin flow to the next step until that step shows its
// own overlay, so that keys that are already queued do not reach the screen underneath; the
// overlays set it, see handoff, and the root clears it, see settle. Backdrop is the kind of
// background that the terminal answered when it was asked, unknown until it answers, see
// backdrop.
type env struct {
	Tr       *i18n.Printer
	Theme    theme
	Glyphs   glyphs
	Metric   metric
	Keys     keymap
	Settings config.Settings
	Paths    config.Paths
	Data     *dataset
	Home     string
	User     string
	Version  string
	Environ  []string
	Probe    shell.Prober
	Profile  colorprofile.Profile
	Width    int
	Cols     int
	Margin   int
	Height   int
	Layout   layoutMode
	Now      func() time.Time
	Writes   *atomic.Int32
	Steps    stepState
	Plugins  PluginHost
	Plug     *pluginCache
	Handing  bool
	Backdrop backdrop
}

// handoff returns the command that delivers the message of the next step of a plugin flow, and
// raises Handing until the root has shown what the step brings.
//
// Parameters:
//   - m: message of the next step
func (e *env) handoff(m tea.Msg) tea.Cmd {
	e.Handing = true

	return msgCmd(m)
}

// writing counts a write that runs off the event loop. The root reads the count to ask before
// it quits with a write in progress.
//
// Parameters:
//   - delta: 1 when a write starts, -1 when it ends
func (e *env) writing(delta int32) {
	if e.Writes != nil {
		e.Writes.Add(delta)
	}
}

// pendingWrites reports whether a write that runs off the event loop has not ended, or a step of
// the settings screen waits for the write that is running to end before it is written.
func (e *env) pendingWrites() bool {
	return (e.Writes != nil && e.Writes.Load() > 0) || e.Steps.queued()
}

// t translates a catalog key. In the ASCII glyph set the printer is the ASCII one (see
// i18n.Printer.ASCII), which has folded the accents, the guillemets, the no-break spaces and the
// ellipsis of the catalog; the ellipsis is also written with the glyph of the set, which is the
// same three dots, for a message that comes from elsewhere.
//
// Parameters:
//   - key: catalog key
//   - args: values for the Printf directives of the message
func (e *env) t(key string, args ...any) string {
	text := e.Tr.T(key, args...)

	if e.Glyphs.ascii {
		text = strings.ReplaceAll(text, "\u2026", e.Glyphs.ellipsis)
	}

	return text
}

// fold folds a message that came from outside the catalogs of the interface, such as the text of
// an error that the command line translated, when the ASCII glyph set is in use.
//
// Parameters:
//   - text: message to show
func (e *env) fold(text string) string {
	if e.Glyphs.ascii {
		return i18n.Fold(text)
	}

	return text
}

// ptext gives a text of a plugin in the language of the interface, folded when the ASCII glyph set
// is in use: the words of a translation that a plugin ships are its own, and may hold characters
// that the ASCII set does not.
//
// Parameters:
//   - cat: catalog of the plugin
//   - key: key of the text in the catalog
//   - literal: text of the manifest, used when the catalog has none
func (e *env) ptext(cat plugin.Catalog, key, literal string) string {
	return e.fold(cat.Text(e.Tr.Lang(), key, literal))
}

// indent returns the number of spaces in front of the content: two on the full layout and one on
// the compact one.
func (e *env) indent() int {
	if e.Layout == layoutFull {
		return 2
	}

	return 1
}

// lead returns the spaces in front of a line of content.
func (e *env) lead() string {
	return spaces(e.indent())
}

// compact reports whether the compact layout is in use.
func (e *env) compact() bool {
	return e.Layout != layoutFull
}
