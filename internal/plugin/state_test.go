package plugin

import (
	"slices"
	"testing"
)

// rankOracle is the table of section 4 written another way: the states in order, each with the
// condition that selects it.
//
// Parameters:
//   - f: conditions that hold
func rankOracle(f Flags) State {
	order := []struct {
		on    bool
		state State
	}{
		{f.Disabled, StateDisabled},
		{f.Broken, StateBroken},
		{f.Incompatible, StateIncompatible},
		{f.NeedsConsent, StateNeedsConsent},
		{f.Quarantined, StateQuarantined},
		{f.MissingDependency, StateMissingDependency},
	}

	for _, o := range order {
		if o.on {
			return o.state
		}
	}

	return StateEnabled
}

// TestRankExhaustive tries every combination of the six conditions, 64 of them, and checks the
// state against the oracle, the lock disabled flag being one of the inputs of the first
// condition.
//
// Parameters:
//   - t: test to report to
func TestRankExhaustive(t *testing.T) {
	seen := map[State]int{}

	for bits := 0; bits < 64; bits++ {
		f := Flags{
			Disabled: bits&1 != 0, Broken: bits&2 != 0, Incompatible: bits&4 != 0,
			NeedsConsent: bits&8 != 0, Quarantined: bits&16 != 0, MissingDependency: bits&32 != 0,
		}

		got := Rank(f)
		if want := rankOracle(f); got != want {
			t.Errorf("Rank(%+v) = %s, want %s", f, got, want)
		}

		seen[got]++
	}

	if len(seen) != 7 {
		t.Errorf("only %d of the 7 states are reachable: %v", len(seen), seen)
	}

	if seen[StateEnabled] != 1 {
		t.Errorf("enabled is the answer for %d combinations, want exactly the empty one",
			seen[StateEnabled])
	}
}

// TestResolveExhaustive builds the failing checks for every subset of the six groups and checks
// that the state is the first group present, that every reason of every group is listed, in the
// order of the table, and that an enabled plugin has none.
//
// Parameters:
//   - t: test to report to
func TestResolveExhaustive(t *testing.T) {
	codes := []string{"a", "b", "c", "d", "e", "f"}

	for bits := 0; bits < 64; bits++ {
		var f Facts

		groups := []*[]Reason{
			&f.Disabled, &f.Broken, &f.Incompatible, &f.NeedsConsent,
			&f.Quarantined, &f.MissingDependency,
		}

		var want []string

		for i, g := range groups {
			if bits&(1<<i) != 0 {
				*g = []Reason{{Code: codes[i]}, {Code: codes[i] + "2"}}
				want = append(want, codes[i], codes[i]+"2")
			}
		}

		state, reasons := Resolve(f)

		if got := reasonCodes(reasons); !slices.Equal(got, want) {
			t.Errorf("bits %06b: reasons = %v, want %v", bits, got, want)
		}

		flags := Flags{
			Disabled: bits&1 != 0, Broken: bits&2 != 0, Incompatible: bits&4 != 0,
			NeedsConsent: bits&8 != 0, Quarantined: bits&16 != 0, MissingDependency: bits&32 != 0,
		}
		if state != rankOracle(flags) {
			t.Errorf("bits %06b: state = %s, want %s", bits, state, rankOracle(flags))
		}

		if (state == StateEnabled) != (bits == 0) || (state == StateEnabled) != (len(reasons) == 0) {
			t.Errorf("bits %06b: enabled = %v with %d reasons", bits, state == StateEnabled,
				len(reasons))
		}
	}
}

// TestRunnable checks that only the enabled state lets a plugin contribute.
//
// Parameters:
//   - t: test to report to
func TestRunnable(t *testing.T) {
	for _, s := range []State{
		StateDisabled, StateBroken, StateIncompatible, StateNeedsConsent,
		StateQuarantined, StateMissingDependency, "revoked", "", "unknown",
	} {
		if s.Runnable() {
			t.Errorf("%q is runnable", s)
		}
	}

	if !StateEnabled.Runnable() {
		t.Error("enabled is not runnable")
	}
}
