package plugin

// State is the state of an installed plugin, section 4 of the specification. Only StateEnabled
// contributes to menus, the palette and the command line. A seventh state, revoked, is reserved
// for 1.1 and will rank before all of these.
type State string

// The states, in rank order: the first one whose condition holds is the state.
const (
	StateDisabled          State = "disabled"
	StateBroken            State = "broken"
	StateIncompatible      State = "incompatible"
	StateNeedsConsent      State = "needs-consent"
	StateQuarantined       State = "quarantined"
	StateMissingDependency State = "missing-dependency"
	StateEnabled           State = "enabled"
)

// Reason is one failing check of a plugin: a stable code and its arguments. It is data, never a
// message: the host renders text from the code with its catalog. Args are cleaned and short.
type Reason struct {
	Code string   `json:"code"`
	Args []string `json:"args,omitempty"`
}

// Reasons of the disabled state.
const (
	ReasonSessionOff = "session_off"
	ReasonConfigOff  = "config_off"
	ReasonLockOff    = "lock_off"
)

// Reasons of the broken state. The tree reasons are the CodeTree constants of hash.go; the
// manifest reason carries the code of its first error and the number of errors.
const (
	ReasonNoLockRecord      = "no_lock_record"
	ReasonLockRecordInvalid = "lock_record_invalid"
	ReasonLockUnreadable    = "lock_unreadable"
	ReasonConfigUnreadable  = "config_unreadable"
	ReasonManifestInvalid   = "manifest_invalid"
	ReasonLockVersion       = "lock_version"
	ReasonLockTier          = "lock_tier"
	ReasonPackageInvalid    = "package_invalid"
)

// Reasons of the needs-consent, quarantined and missing-dependency states, and the warning for an
// optional program that is not installed.
const (
	ReasonNotConsented      = "not_consented"
	ReasonCapsEscalated     = "caps_escalated"
	ReasonEscalated         = "escalated"
	ReasonQuarantined       = "quarantined"
	ReasonMissingDep        = "missing_dependency"
	ReasonOptionalDepAbsent = "optional_missing"
	ReasonLocaleInvalid     = "locale_invalid"
)

// Flags are the conditions of section 4, one per state except enabled. Each holds when at least
// one check of its state fails.
type Flags struct {
	Disabled          bool
	Broken            bool
	Incompatible      bool
	NeedsConsent      bool
	Quarantined       bool
	MissingDependency bool
}

// Rank applies the table of section 4: the first state whose condition holds wins, and a plugin
// where none holds is enabled. The user's intent ranks first, so a disabled plugin stays listed
// and its other problems stay in its reasons.
//
// Parameters:
//   - f: conditions that hold for the plugin
func Rank(f Flags) State {
	switch {
	case f.Disabled:
		return StateDisabled
	case f.Broken:
		return StateBroken
	case f.Incompatible:
		return StateIncompatible
	case f.NeedsConsent:
		return StateNeedsConsent
	case f.Quarantined:
		return StateQuarantined
	case f.MissingDependency:
		return StateMissingDependency
	}

	return StateEnabled
}

// Facts are the failing checks of a plugin, grouped by the state they lead to. A group is empty
// when its checks all pass.
type Facts struct {
	Disabled          []Reason
	Broken            []Reason
	Incompatible      []Reason
	NeedsConsent      []Reason
	Quarantined       []Reason
	MissingDependency []Reason
}

// Resolve is the state function: it computes the state from the failing checks and lists every
// reason, in the order of the table. It is pure, nothing is stored, and the caller feeds it with
// files read at that moment.
//
// Parameters:
//   - f: failing checks of the plugin
//
// Returns the state and the reasons of all groups, so that a disabled plugin that is also broken
// shows both. The result is empty for an enabled plugin.
func Resolve(f Facts) (State, []Reason) {
	var reasons []Reason

	for _, g := range [][]Reason{
		f.Disabled, f.Broken, f.Incompatible, f.NeedsConsent,
		f.Quarantined, f.MissingDependency,
	} {
		reasons = append(reasons, g...)
	}

	state := Rank(Flags{
		Disabled:          len(f.Disabled) > 0,
		Broken:            len(f.Broken) > 0,
		Incompatible:      len(f.Incompatible) > 0,
		NeedsConsent:      len(f.NeedsConsent) > 0,
		Quarantined:       len(f.Quarantined) > 0,
		MissingDependency: len(f.MissingDependency) > 0,
	})

	return state, reasons
}

// Runnable reports whether a state lets the plugin contribute and run: only enabled does.
//
// Parameters:
//   - s: state of the plugin
func (s State) Runnable() bool { return s == StateEnabled }
