package ui

import (
	tea "charm.land/bubbletea/v2"
)

// stepState remembers what the left and right keys of the settings screen asked for, since the
// keys can repeat faster than a write ends.
//
// Want holds, by setting, the value asked for last and Busy the value that is being written. A
// setting is in Want and Busy from the first step until the write of its last value ends. The
// state lives in the environment and not in the screen, so that a step that waits behind a write
// is still written when the screen was left in the meantime, and so that quitting can see it.
type stepState struct {
	want map[string]string
	busy map[string]string
}

// empty reports whether no step is asked for or being written.
func (st *stepState) empty() bool {
	return len(st.want) == 0 && len(st.busy) == 0
}

// queued reports whether a value was asked for behind the write that is running, which nothing
// has written yet.
func (st *stepState) queued() bool {
	for id, v := range st.want {
		if st.busy[id] != v {
			return true
		}
	}

	return false
}

// stepFrom returns the value a step starts from: the last one that was asked for while its write
// has not ended, else the value in force.
//
// Parameters:
//   - d: setting to read
func (e *env) stepFrom(d settingDef) string {
	if v, ok := e.Steps.want[d.id]; ok {
		return v
	}

	return d.value(e.Settings)
}

// stepTo asks for a new value of a setting and starts its write. Only one write of a setting runs
// at a time, since two writes could end in either order and leave the older value in the file: a
// value asked for while a write runs is kept and written when that one ends, see stepEnded. A value
// the setting already has, or is about to have, writes nothing.
//
// Parameters:
//   - d: setting to write
//   - to: value to give it
//
// Returns the command that writes the value, or nil when nothing starts now: the value is the one
// in force or the one asked for last, or it waits behind a write that is running.
func (e *env) stepTo(d settingDef, to string) tea.Cmd {
	st := &e.Steps

	if to == e.stepFrom(d) {
		return nil
	}

	if st.want == nil {
		st.want, st.busy = map[string]string{}, map[string]string{}
	}

	st.want[d.id] = to

	if _, writing := st.busy[d.id]; writing {
		return nil
	}

	st.busy[d.id] = to

	return writeSettingCmd(e, d, to, false)
}

// stepEnded takes the end of a write that stepTo started. The value asked for in the meantime, if
// there is one, is written next; after a failure it is forgotten, so that the next step starts
// from the value that is really in the file.
//
// Parameters:
//   - m: result of the write; one that stepTo did not start is ignored
//
// Returns the command that writes the value asked for since, or nil.
func (e *env) stepEnded(m settingWroteMsg) tea.Cmd {
	st := &e.Steps

	wrote, ok := st.busy[m.ID]
	if !ok || m.Reset || wrote != m.Value {
		return nil
	}

	delete(st.busy, m.ID)

	to, asked := st.want[m.ID]
	if m.Err != nil || !asked || to == m.Value {
		delete(st.want, m.ID)

		return nil
	}

	d, found := findSetting(m.ID)
	if !found {
		delete(st.want, m.ID)

		return nil
	}

	st.busy[m.ID] = to

	return writeSettingCmd(e, d, to, false)
}
