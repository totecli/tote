package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sort"
)

// Kinds of surface escalation, from decision (f) of the specification: a changed command asks for
// a new consent only when it removes a confirmation, adds a name to allow_dash or to the env
// table, changes the value of an env name (review of 4 November 2026: the value can start a
// program), or changes the pinned hash of a platform build without a version bump.
const (
	EscConfirmRemoved  = "confirm_removed"
	EscAllowDashAdded  = "allow_dash_added"
	EscEnvAdded        = "env_added"
	EscEnvChanged      = "env_changed"
	EscPlatformChanged = "platform_changed"
)

// Table names of the surface that are not tables of the manifest, and the joiner of the id of a
// list action.
const (
	surfaceTableExec     = "exec"
	surfaceTablePlatform = "platform"
	surfaceTableListAct  = "list.action"
	listActionIDJoiner   = "."
)

// defaultModes is the request kinds of an exec plugin that declares no modes.
var defaultModes = []string{"list", "run"}

// surfaceEntry is a contribution reduced to what it runs, with the id that names it in the
// surface: the id itself, or <list>.<action> for a list action.
type surfaceEntry struct {
	id string
	e  *Entry
}

// surfaceEntries lists the contributions of a manifest by table.
//
// Parameters:
//   - m: validated manifest
//
// Returns a map from table name to the entries of that table, in file order.
func surfaceEntries(m *Manifest) map[string][]surfaceEntry {
	out := map[string][]surfaceEntry{}

	for _, set := range []struct {
		table   string
		entries []Entry
	}{{"command", m.Commands}, {"item_action", m.ItemActions}, {"action", m.Actions}} {
		for i := range set.entries {
			e := &set.entries[i]
			out[set.table] = append(out[set.table], surfaceEntry{e.ID, e})
		}
	}

	for _, l := range m.Lists {
		for i := range l.Actions {
			a := &l.Actions[i]
			out[surfaceTableListAct] = append(out[surfaceTableListAct],
				surfaceEntry{l.ID + listActionIDJoiner + a.ID, a})
		}
	}

	return out
}

// SurfaceObjects computes the inputs of surface_sha256: one object per contribution, with the
// keys table, id, target, run, open, env, cwd, allow_dash, output, pause, timeout, confirm and
// fields (id, type, flag, pattern, choices, required), plus for the exec tier one object for
// [exec] (bin, args, modes) and one for each [[platform]] (os, arch, bin, sha256). A [[list]]
// contributes its id and timeout, and a [[list.action]] has the id <list>.<action>.
//
// Labels, descriptions, keywords, hints and locales are not part of it: it answers "does this
// version run the same commands". The result is ordered by table then id.
//
// A key of a contribution or of a field is written only when it is set: table and id (and the type
// of a field) are always there, every other key is left out at its zero value (empty string,
// empty list, empty table, false). That is the rule that keeps the hash of an installed plugin
// when a later release reads a new manifest key: a key added later counts only when a manifest
// sets it, and its zero value must mean what the plugin did before the key existed.
//
// Parameters:
//   - m: validated manifest
func SurfaceObjects(m *Manifest) []map[string]any {
	var out []map[string]any

	for table, entries := range surfaceEntries(m) {
		for _, se := range entries {
			out = append(out, entryObject(table, se.id, se.e))
		}
	}

	for _, l := range m.Lists {
		o := entryObject("list", l.ID, &Entry{Timeout: l.Timeout})
		out = append(out, o)
	}

	if m.Tier == TierExec {
		out = append(out, execObjects(m)...)
	}

	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i]["table"].(string), out[j]["table"].(string)
		if ti != tj {
			return ti < tj
		}

		return out[i]["id"].(string) < out[j]["id"].(string)
	})

	return out
}

// entryObject builds the surface object of one contribution. A key at its zero value is left
// out, except table and id.
//
// Parameters:
//   - table: name of the table
//   - id: id in the surface
//   - e: contribution
func entryObject(table, id string, e *Entry) map[string]any {
	obj := map[string]any{"table": table, "id": id}

	setString(obj, "target", e.Target)
	setString(obj, "open", e.Open)
	setString(obj, "cwd", e.Cwd)
	setString(obj, "output", e.Output)
	setString(obj, "pause", e.Pause)
	setString(obj, "timeout", e.Timeout)

	if len(e.Run) > 0 {
		obj["run"] = e.Run
	}

	if len(e.Env) > 0 {
		env := make(map[string]string, len(e.Env))
		for k, v := range e.Env {
			env[k] = v
		}

		obj["env"] = env
	}

	if len(e.AllowDash) > 0 {
		allow := slices.Clone(e.AllowDash)
		sort.Strings(allow)

		obj["allow_dash"] = allow
	}

	switch {
	case e.Confirm.Text != "":
		obj["confirm"] = e.Confirm.Text
	case e.Confirm.Set:
		obj["confirm"] = true
	}

	if len(e.Fields) > 0 {
		fields := make([]map[string]any, 0, len(e.Fields))
		for _, f := range e.Fields {
			fields = append(fields, fieldObject(f))
		}

		obj["fields"] = fields
	}

	return obj
}

// fieldObject builds the surface object of one field: id and type always, the other keys when set.
//
// Parameters:
//   - f: field of a contribution
func fieldObject(f Field) map[string]any {
	obj := map[string]any{"id": f.ID, "type": f.Type}

	setString(obj, "flag", f.Flag)
	setString(obj, "pattern", f.Pattern)

	if len(f.Choices) > 0 {
		obj["choices"] = f.Choices
	}

	if f.Required {
		obj["required"] = true
	}

	return obj
}

// setString stores a string in a surface object when it is not empty.
//
// Parameters:
//   - obj: surface object
//   - key: name of the key
//   - value: value to store, left out when empty
func setString(obj map[string]any, key, value string) {
	if value != "" {
		obj[key] = value
	}
}

// orEmpty turns a nil list into an empty one, so that it marshals as [] and not null.
//
// Parameters:
//   - list: list to normalise
func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}

	return list
}

// execObjects builds the surface objects of [exec] and [[platform]].
//
// Parameters:
//   - m: validated manifest of the exec tier
func execObjects(m *Manifest) []map[string]any {
	var out []map[string]any

	if m.Exec != nil {
		modes := m.Exec.Modes
		if len(modes) == 0 {
			modes = defaultModes
		}

		out = append(out, map[string]any{
			"table": surfaceTableExec, "id": "", "bin": m.Exec.Bin, "args": orEmpty(m.Exec.Args),
			"modes": orEmpty(modes),
		})
	}

	for _, b := range m.Builds {
		out = append(out, map[string]any{
			"table": surfaceTablePlatform, "id": b.OS + "/" + b.Arch, "os": b.OS, "arch": b.Arch,
			"bin": b.Bin, "sha256": b.SHA256,
		})
	}

	return out
}

// SurfaceJSON renders the surface as canonical JSON: sorted keys, no spaces, no HTML escaping.
//
// Parameters:
//   - m: validated manifest
func SurfaceJSON(m *Manifest) []byte {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	// A tree of strings, booleans, lists and maps always encodes.
	_ = enc.Encode(SurfaceObjects(m))

	return bytes.TrimRight(buf.Bytes(), "\n")
}

// SurfaceSHA256 hashes the canonical surface of a manifest.
//
// Parameters:
//   - m: validated manifest
//
// Returns the 64 hexadecimal digits of surface_sha256.
func SurfaceSHA256(m *Manifest) string {
	sum := sha256.Sum256(SurfaceJSON(m))

	return hex.EncodeToString(sum[:])
}

// SurfaceChange is one change of command surface that asks for a new consent.
//
// Kind is one of the Esc constants; Table and ID name the contribution, or the platform build
// (table "platform", id os/arch).
type SurfaceChange struct {
	Kind  string
	Table string
	ID    string
}

// SurfaceEscalations compares the command surface of two versions of a plugin.
//
// The rule is the same for both tiers: a new consent is asked when an entry that kept its id lost
// its confirmation (removed, or turned from true or a text to false), when an entry gains a name
// in allow_dash or in its env table (a new entry counts as gaining all of its names), or when the
// pinned sha256 of a platform build changes while the version stays the same. Any other change of
// the surface is shown in the diff of commands but never asked.
//
// Parameters:
//   - old: manifest of the installed version
//   - next: manifest of the new version
//
// Returns the changes, sorted by table, id then kind; empty when nothing asks.
func SurfaceEscalations(old, next *Manifest) []SurfaceChange {
	var out []SurfaceChange

	oldEntries := surfaceEntries(old)

	for table, entries := range surfaceEntries(next) {
		prev := map[string]*Entry{}
		for _, se := range oldEntries[table] {
			prev[se.id] = se.e
		}

		for _, se := range entries {
			out = append(out, entryChanges(table, se, prev[se.id])...)
		}
	}

	out = append(out, platformChanges(old, next)...)

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}

		if a.ID != b.ID {
			return a.ID < b.ID
		}

		return a.Kind < b.Kind
	})

	return out
}

// entryChanges lists the escalations of one contribution of the new version.
//
// Parameters:
//   - table: name of the table
//   - se: contribution of the new version
//   - prev: same contribution in the installed version, nil when the id is new
func entryChanges(table string, se surfaceEntry, prev *Entry) []SurfaceChange {
	var out []SurfaceChange

	add := func(kind string) { out = append(out, SurfaceChange{kind, table, se.id}) }

	if prev != nil && prev.Confirm.Set && !se.e.Confirm.Set {
		add(EscConfirmRemoved)
	}

	var oldAllow, oldEnv []string

	if prev != nil {
		oldAllow, oldEnv = prev.AllowDash, sortedKeys(prev.Env)
	}

	if prev != nil && envValueChanged(prev.Env, se.e.Env) {
		add(EscEnvChanged)
	}

	if hasNew(se.e.AllowDash, oldAllow) {
		add(EscAllowDashAdded)
	}

	if hasNew(sortedKeys(se.e.Env), oldEnv) {
		add(EscEnvAdded)
	}

	return out
}

// envValueChanged reports whether a name of the env table kept its name and changed its value:
// the value can start another program, so it needs a new consent like a new name does.
//
// Parameters:
//   - old: env table of the installed version
//   - next: env table of the new version
func envValueChanged(old, next map[string]string) bool {
	for k, v := range next {
		if o, ok := old[k]; ok && o != v {
			return true
		}
	}

	return false
}

// hasNew reports whether a list holds a name that another list lacks.
//
// Parameters:
//   - names: names of the new version
//   - old: names of the installed version
func hasNew(names, old []string) bool {
	for _, n := range names {
		if !slices.Contains(old, n) {
			return true
		}
	}

	return false
}

// platformChanges lists the platform builds whose pinned hash changed under an unchanged version.
//
// Parameters:
//   - old: manifest of the installed version
//   - next: manifest of the new version
func platformChanges(old, next *Manifest) []SurfaceChange {
	if old.Version != next.Version {
		return nil
	}

	var out []SurfaceChange

	for _, nb := range next.Builds {
		for _, ob := range old.Builds {
			if ob.OS == nb.OS && ob.Arch == nb.Arch && ob.SHA256 != nb.SHA256 {
				out = append(out, SurfaceChange{
					EscPlatformChanged, surfaceTablePlatform,
					nb.OS + "/" + nb.Arch,
				})
			}
		}
	}

	return out
}

// CommandRef is a command of a plugin as the consent view lists it: the template that tote runs
// or the URL template that it opens, the working directory template, the allow_dash names and
// the env table as sorted NAME=template lines. The env table is part of what a command runs: a
// value can make the declared program start another one, so it is shown with the command.
type CommandRef struct {
	Table     string
	ID        string
	Run       []string
	Open      string
	Cwd       string
	AllowDash []string
	Env       []string
}

// Commands lists the commands of a plugin, which are its code when it is declarative: every run
// and every open template, in full.
//
// Parameters:
//   - m: validated manifest
//
// Returns the commands sorted by table then id.
func Commands(m *Manifest) []CommandRef {
	var out []CommandRef

	for table, entries := range surfaceEntries(m) {
		for _, se := range entries {
			if se.e.Run != nil || se.e.Open != "" {
				out = append(out, commandRef(table, se))
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}

		return out[i].ID < out[j].ID
	})

	return out
}

// commandRef builds the consent view of one contribution.
//
// Parameters:
//   - table: name of the table
//   - se: contribution
func commandRef(table string, se surfaceEntry) CommandRef {
	allow := slices.Clone(se.e.AllowDash)
	slices.Sort(allow)

	return CommandRef{table, se.id, se.e.Run, se.e.Open, se.e.Cwd, allow, envTableLines(se.e.Env)}
}

// sameCommand reports whether two commands run the same thing.
//
// Parameters:
//   - a: first command
//   - b: second command
func sameCommand(a, b CommandRef) bool {
	return a.Open == b.Open && a.Cwd == b.Cwd && slices.Equal(a.Run, b.Run) &&
		slices.Equal(a.AllowDash, b.AllowDash) && slices.Equal(a.Env, b.Env)
}

// CommandChange is one difference between the commands of two versions: Kind is "added",
// "removed" or "changed"; Old and New hold the command on each side, the zero value on the side
// where it does not exist.
type CommandChange struct {
	Kind string
	Old  CommandRef
	New  CommandRef
}

// DiffCommands compares the commands of two versions, for the diff the consent view shows.
//
// Parameters:
//   - old: manifest of the installed version
//   - next: manifest of the new version
//
// Returns the added, removed and changed commands, sorted by table then id.
func DiffCommands(old, next *Manifest) []CommandChange {
	key := func(c CommandRef) string { return c.Table + "\x00" + c.ID }

	prev := map[string]CommandRef{}
	for _, c := range Commands(old) {
		prev[key(c)] = c
	}

	var out []CommandChange

	seen := map[string]bool{}

	for _, c := range Commands(next) {
		seen[key(c)] = true

		o, existed := prev[key(c)]

		switch {
		case !existed:
			out = append(out, CommandChange{"added", CommandRef{}, c})
		case !sameCommand(o, c):
			out = append(out, CommandChange{"changed", o, c})
		}
	}

	for _, c := range Commands(old) {
		if !seen[key(c)] {
			out = append(out, CommandChange{"removed", c, CommandRef{}})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].New, out[j].New
		if out[i].Kind == "removed" {
			a = out[i].Old
		}

		if out[j].Kind == "removed" {
			b = out[j].Old
		}

		return a.Table+"\x00"+a.ID < b.Table+"\x00"+b.ID
	})

	return out
}
