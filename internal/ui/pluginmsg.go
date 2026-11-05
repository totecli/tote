package ui

import (
	"github.com/totecli/tote/internal/plugin"
)

// pluginAct is something the user can ask of a plugin.
type pluginAct int

const (
	// actDetails shows everything about the plugin.
	actDetails pluginAct = iota

	// actToggle enables a plugin that is not enabled and disables one that is.
	actToggle

	// actAllow shows what the plugin asks for and records the consent.
	actAllow

	// actSettings opens the form of the settings of the plugin.
	actSettings

	// actInstall installs a plugin shipped with tote.
	actInstall

	// actUpdate installs the files of the plugin again from where it came from.
	actUpdate

	// actRemove asks before the plugin is removed, its settings and data being kept.
	actRemove

	// actPurge asks before the plugin is removed with its settings and data.
	actPurge

	// actCheck examines the plugin and shows what it found.
	actCheck

	// actMenu lists the actions that apply to the plugin.
	actMenu

	// actClear clears the quarantine of the plugin.
	actClear
)

// openPluginsMsg asks the root to show the plugins screen.
type openPluginsMsg struct{}

// pluginActMsg asks the root to do something with a plugin. The root reads the plugin again from
// the files, so a screen only has to name it. Seen is the state the screen showed when the user
// asked to turn the plugin on or off, empty when it is not known: a plugin that is in a state of
// the other direction by now is not toggled, as the user did not ask for what it would do.
type pluginActMsg struct {
	Act  pluginAct
	ID   string
	Seen string
}

// pluginRemoveMsg is the answer yes to the question that comes before a removal.
type pluginRemoveMsg struct {
	ID    string
	Purge bool
}

// pluginGrantMsg is the consent: the user read what a plugin asks for and accepted. Staged is the
// package waiting in the staging area, nil for a plugin that is already installed; Enable tells
// that the plugin is to be enabled too. Token is the grant token of the view the user accepted, so
// that a plugin that changed meanwhile is refused instead of being allowed unseen.
type pluginGrantMsg struct {
	ID     string
	Token  string
	Staged *plugin.Staged
	Enable bool
}

// jobOp is the kind of work a background job does.
type jobOp int

const (
	// jobPlan copies, checks and hashes a package into the staging area.
	jobPlan jobOp = iota

	// jobCommit turns a staged package into the installed one.
	jobCommit

	// jobAllow builds the view of what an installed plugin asks for.
	jobAllow

	// jobEnable turns a plugin on.
	jobEnable

	// jobGrant records the consent to an installed plugin.
	jobGrant

	// jobDisable turns a plugin off.
	jobDisable

	// jobRemove deletes a plugin.
	jobRemove

	// jobCheck examines a plugin.
	jobCheck
)

// pluginDoneMsg is the end of a background job. Seq tells which job it is, so that the answer of
// a job the user gave up is dropped; Res is what the operation returned and Err its failure.
type pluginDoneMsg struct {
	Seq    int
	Op     jobOp
	ID     string
	Staged *plugin.Staged
	Res    any
	Err    error
}

// jobCancelMsg is sent by the overlay that waits for a job when the user gives up.
type jobCancelMsg struct {
	Seq int
}

// fieldsSavedMsg is the end of the write that a generic form started. Form is the overlay that
// started it and Fail the translated reason it failed, empty when it did not.
type fieldsSavedMsg struct {
	Form *fieldsForm
	Fail string
}
