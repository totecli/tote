package ui

import (
	"errors"
	"strings"

	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

const (
	// probeName is a connection, repository, alias or function name that passes every check.
	probeName = "a"

	// probeTarget is a connection target that passes every check.
	probeTarget = "a"

	// probeScript is a script path that passes every check.
	probeScript = "/a"

	// probeCmd is an alias command that passes every check.
	probeCmd = "a"

	// probePath is a repository path that passes every check.
	probePath = "/a"

	// limitName is the most characters typed in a name.
	limitName = 64

	// limitText is the most characters typed in a command, a path or a list.
	limitText = 1024

	// limitDesc is the most characters typed in a description, as many as the store accepts.
	limitDesc = 200
)

// formFields builds the fields of the form of a kind.
//
// Parameters:
//   - k: kind of the record
//   - rec: record to edit as a store.Connection, store.Repo, store.Alias or store.Func value,
//     anything else to create one
//
// Returns the fields in the order they are shown, filled with the record when there is one, and
// the key of the record being edited, empty when a new one is created.
func formFields(k kind, rec any) ([]*formField, string) {
	switch k {
	case kindConn:
		c, _ := rec.(store.Connection)

		return connFields(c), c.Name
	case kindRepo:
		r, _ := rec.(store.Repo)

		return repoFields(r), r.Name
	case kindAlias:
		a, _ := rec.(store.Alias)

		return aliasFields(a), a.Name
	}

	f, _ := rec.(store.Func)

	return funcFields(f), f.Name
}

// fill puts the values of a record in text fields.
//
// Parameters:
//   - fields: fields to fill
//   - values: text of each field by its ID
func fill(fields []*formField, values map[string]string) {
	for _, fd := range fields {
		if v := values[fd.ID]; v != "" && fd.Type == fieldText {
			fd.setText(v)
		}
	}
}

// connFields builds the fields of a connection.
//
// Parameters:
//   - c: connection to edit, the zero value to create one
func connFields(c store.Connection) []*formField {
	fields := []*formField{
		newTextField("name", "ui_field_name", "ui_hint_conn_name", limitName),
		newTextField("target", "ui_field_target", "ui_hint_conn_target", 255),
		newTextField("via", "ui_field_via", "ui_hint_conn_via", 255),
		newTextField("user", "ui_field_user", "ui_hint_conn_user", 32),
		newTextField("aliases", "ui_field_aliases", "ui_hint_conn_aliases", limitText),
		newTextField("ip", "ui_field_ip", "ui_hint_conn_ip", 45),
		newTextField("desc", "ui_field_desc", "ui_hint_desc", limitDesc),
	}

	fields[2].Default = "ui_default_via"

	fill(fields, map[string]string{
		"name": c.Name, "target": c.Target, "via": c.Via, "user": c.User,
		"aliases": strings.Join(c.Aliases, ","), "ip": c.IP, "desc": c.Desc,
	})

	return fields
}

// repoFields builds the fields of a repository.
//
// Parameters:
//   - r: repository to edit, the zero value to create one
func repoFields(r store.Repo) []*formField {
	fields := []*formField{
		newTextField("name", "ui_field_name", "ui_hint_shell_name", limitName),
		newTextField("path", "ui_field_path", "ui_hint_repo_path", limitText),
		newTextField("desc", "ui_field_desc", "ui_hint_desc", limitDesc),
	}

	fill(fields, map[string]string{"name": r.Name, "path": r.Path, "desc": r.Desc})

	return fields
}

// aliasFields builds the fields of an alias.
//
// Parameters:
//   - a: alias to edit, the zero value to create one
func aliasFields(a store.Alias) []*formField {
	fields := []*formField{
		newTextField("name", "ui_field_name", "ui_hint_shell_name", limitName),
		newTextField("cmd", "ui_field_cmd", "ui_hint_alias_cmd", limitText),
		newTextField("desc", "ui_field_desc", "ui_hint_desc", limitDesc),
	}

	fill(fields, map[string]string{"name": a.Name, "cmd": a.Cmd, "desc": a.Desc})

	return fields
}

// funcFields builds the fields of a function.
//
// Parameters:
//   - f: function to edit, the zero value to create one
func funcFields(f store.Func) []*formField {
	kindField := newChoiceField("kind", "ui_field_kind",
		[]string{store.KindSource, store.KindExec},
		[]string{"ui_choice_source", "ui_choice_exec"},
		[]string{"ui_hint_kind_source", "ui_hint_kind_exec"})

	if f.Kind == store.KindExec {
		kindField.Pick = 1
	}

	managed := newToggleField("managed", "ui_field_managed", "ui_hint_managed")
	managed.On = f.Managed

	fields := []*formField{
		newTextField("name", "ui_field_name", "ui_hint_shell_name", limitName),
		kindField,
		newTextField("script", "ui_field_script", "ui_hint_func_script", limitText),
		managed,
		newTextField("desc", "ui_field_desc", "ui_hint_desc", limitDesc),
	}

	fill(fields, map[string]string{"name": f.Name, "script": f.Script, "desc": f.Desc})

	return fields
}

// splitAliases splits the aliases field into its names.
//
// Parameters:
//   - raw: names separated by commas, spaces around a name are ignored
//
// Returns nil when there is no name.
func splitAliases(raw string) []string {
	var out []string

	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}

	return out
}

// normalizeConn applies to the values of a connection what the command line applies: ssh is the
// same as an empty via, and a target written as user@host gives the user when none is set.
//
// Parameters:
//   - vals: text of each field by its ID, changed in place
func normalizeConn(vals map[string]string) {
	if vals["via"] == "ssh" {
		vals["via"] = ""
	}

	user, host, ok := strings.Cut(vals["target"], "@")
	if ok && vals["user"] == "" && user != "" && host != "" && !strings.Contains(host, "@") {
		vals["user"], vals["target"] = user, host
	}
}

// buildRecord makes the record of a kind out of the values of its fields.
//
// Parameters:
//   - k: kind of the record
//   - vals: text of each field by its ID, already normalized
//
// Returns a store.Connection, store.Repo, store.Alias or store.Func value. Nothing is checked.
func buildRecord(k kind, vals map[string]string) any {
	switch k {
	case kindConn:
		return store.Connection{
			Name: vals["name"], Via: vals["via"], Target: vals["target"], User: vals["user"],
			Aliases: splitAliases(vals["aliases"]), IP: vals["ip"], Desc: vals["desc"],
		}
	case kindRepo:
		return store.Repo{Name: vals["name"], Path: vals["path"], Desc: vals["desc"]}
	case kindAlias:
		return store.Alias{Name: vals["name"], Cmd: vals["cmd"], Desc: vals["desc"]}
	}

	return store.Func{
		Name: vals["name"], Desc: vals["desc"], Kind: vals["kind"], Script: vals["script"],
		Managed: vals["managed"] == "true",
	}
}

// probeConn checks one field of a connection inside a record whose other fields are valid.
//
// Parameters:
//   - id: field to check
//   - vals: text of each field by its ID, normalized
//
// Returns the validation error of the field, or nil.
func probeConn(id string, vals map[string]string) error {
	c := store.Connection{Name: probeName, Target: probeTarget}

	switch id {
	case "name":
		c.Name = vals["name"]
	case "via":
		c.Via = vals["via"]
	case "target":
		c.Target = vals["target"]
		if (store.Connection{Name: probeName, Via: vals["via"], Target: probeTarget}).
			Validate() == nil {
			c.Via = vals["via"]
		}
	case "user":
		c.User = vals["user"]
	case "aliases":
		c.Aliases = splitAliases(vals["aliases"])
		if store.ValidConnectionName(vals["name"]) == nil {
			c.Name = vals["name"]
		}
	case "ip":
		c.IP = vals["ip"]
	case "desc":
		c.Desc = vals["desc"]
	}

	return c.Validate()
}

// probeShell checks one field of an alias, a repository or a function inside a record whose other
// fields are valid.
//
// Parameters:
//   - k: kind of the record
//   - id: field to check
//   - vals: text of each field by its ID
//
// Returns the validation error of the field, or nil.
func probeShell(k kind, id string, vals map[string]string) error {
	v := vals[id]

	switch k {
	case kindRepo:
		r := store.Repo{Name: probeName, Path: probePath}

		switch id {
		case "name":
			r.Name = v
		case "path":
			r.Path = v
		case "desc":
			r.Desc = v
		}

		return r.Validate()
	case kindAlias:
		a := store.Alias{Name: probeName, Cmd: probeCmd}

		switch id {
		case "name":
			a.Name = v
		case "cmd":
			a.Cmd = v
		case "desc":
			a.Desc = v
		}

		return a.Validate()
	}

	f := store.Func{Name: probeName, Kind: store.KindSource, Script: probeScript}

	switch id {
	case "name":
		f.Name = v
	case "script":
		f.Script = v
	case "desc":
		f.Desc = v
	}

	return f.Validate()
}

var (
	// errRelative marks a repository path that depends on the current directory.
	errRelative = errors.New("path is relative")

	// errScriptRelative marks a script path that would be searched in PATH.
	errScriptRelative = errors.New("script path is relative")
)

// checkPath refuses a path that does not start from the root or from the home directory.
//
// Parameters:
//   - k: kind of the record
//   - id: field to check
//   - vals: text of each field by its ID
//
// Returns errRelative for a relative repository path, errScriptRelative for a relative script
// of a function that is sourced, and nil for every other field.
func checkPath(k kind, id string, vals map[string]string) error {
	switch {
	case k == kindRepo && id == "path":
		if !shell.AbsOrHome(vals["path"]) {
			return errRelative
		}
	case k == kindFunc && id == "script" && vals["kind"] == store.KindSource:
		if !shell.ValidScript(vals["script"]) {
			return errScriptRelative
		}
	}

	return nil
}
