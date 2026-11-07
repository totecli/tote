package plugin

import (
	"slices"
	"strings"
)

// Offer is one contribution of an enabled plugin as the menus and the palette show it.
//
// Plugin is the id of the plugin, Table is TableCommand or TableItemAction and ID the id of the
// contribution. Label and Description are in the language of the request and cleaned, Keywords
// are the palette words and Key the accelerator the manifest asked for. Output is pager, silent or
// terminal, empty for a contribution that opens an address. Open is true for such a contribution,
// Form for one with fields and Confirm for one that asks before it runs. Available is false when
// the contribution cannot run now; Why then holds the reason as a code with its arguments, the
// same ones Prepare would refuse with, and for a program that is not installed the second
// argument is the hint of the plugin, cleaned and still to be labelled as the plugin's.
type Offer struct {
	Plugin      string
	Table       string
	ID          string
	Label       string
	Description string
	Keywords    []string
	Key         string
	Output      string
	Open        bool
	Form        bool
	Confirm     bool
	Available   bool
	Why         *Reason
}

// offerOf describes a contribution without judging whether it can run.
//
// Parameters:
//   - p: the plugin
//   - e: the contribution
//   - lang: language of the texts
func offerOf(p Plugin, e *Entry, lang string) Offer {
	base := entryKeyBase(e, nil)
	o := Offer{
		Plugin: p.ID, Table: e.Table, ID: e.ID, Key: e.Key, Output: e.Output,
		Open: e.Open != "", Form: len(e.Fields) > 0, Confirm: e.Confirm.Set, Available: true,
		Label:       p.Catalog.Text(lang, base+".label", e.Label),
		Description: p.Catalog.Text(lang, base+".description", e.Description),
	}

	if len(e.Keywords) > 0 {
		text := p.Catalog.Text(lang, base+".keywords", strings.Join(e.Keywords, ","))

		for _, k := range strings.Split(text, ",") {
			if k = strings.TrimSpace(k); k != "" {
				o.Keywords = append(o.Keywords, k)
			}
		}
	}

	return o
}

// Offers lists the commands of the enabled plugins of a snapshot, in the order of the plugins and
// of their manifests. It reads nothing and starts nothing, so a screen can call it while it draws;
// whether a command can run is only known when it is prepared, because its form is not filled in
// yet.
//
// Parameters:
//   - snap: what Load found
//   - lang: language of the texts, such as "fr"; any other value gives English
func (r *Registry) Offers(snap Snapshot, lang string) []Offer {
	var out []Offer

	for _, p := range snap.Enabled() {
		if p.Manifest == nil {
			continue
		}

		for i := range p.Manifest.Commands {
			out = append(out, offerOf(p, &p.Manifest.Commands[i], lang))
		}
	}

	return out
}

// RepoOffers lists the item actions that apply to a saved repository, with whether each can run
// now. Every plugin is read again from the files first, so a plugin that was disabled in another
// terminal offers nothing. The only program it may start is git, run by tote itself to read the
// remote of the repository.
//
// An action is unavailable, with its reason, when the repository is unknown or its directory is
// gone, when a placeholder it needs is empty (a repository without a GitHub remote has no
// {repo.slug}), when its directory is not a directory, or when its program is not installed. The
// fields of its form are not asked yet, so each is given a harmless sample for this test only.
//
// Parameters:
//   - snap: what Load found
//   - repo: name of the saved repository
//   - lang: language of the texts, such as "fr"; any other value gives English
func (r *Registry) RepoOffers(snap Snapshot, repo, lang string) []Offer {
	var out []Offer

	for _, listed := range snap.Enabled() {
		p, lc, ok := r.recheck(listed.ID)
		if !ok || !p.State.Runnable() || p.Manifest == nil {
			continue
		}

		for i := range p.Manifest.ItemActions {
			e := &p.Manifest.ItemActions[i]
			if e.Target != "repo" {
				continue
			}

			o := offerOf(p, e, lang)
			call := Call{Plugin: p.ID, Table: e.Table, Entry: e.ID, Repo: repo, Lang: lang}

			if why := r.usable(p, lc, call, e); why != nil {
				o.Available, o.Why = false, &Reason{Code: why.Code, Args: why.Args}
			}

			out = append(out, o)
		}
	}

	return out
}

// usable tells why a contribution cannot run now, nil when it can. It goes as far as the program
// lookup and starts nothing of the plugin's.
//
// Parameters:
//   - p: the plugin as it was just read
//   - lc: inputs of the pass that read it
//   - call: the call, without the values of a form
//   - e: the contribution
func (r *Registry) usable(p Plugin, lc *loadContext, call Call, e *Entry) *Error {
	pc, err := r.newPrep(p, lc, call, e, nil)
	if err != nil {
		return err
	}

	for i := range e.Fields {
		pc.vals.Fields[e.Fields[i].ID] = sampleOf(&e.Fields[i])
	}

	pathSamples(pc, e)

	if e.Open != "" {
		if _, err := pc.openURL(); err != nil {
			return err
		}

		_, err := pc.openerPath()

		return err
	}

	name, err := pc.programName()
	if err != nil {
		return err
	}

	if _, err := pc.expand(name); err != nil {
		return err
	}

	dir, err := pc.directory()
	if err != nil {
		return err
	}

	avoid := lc.avoid
	avoid.Exactly = append(slices.Clone(avoid.Exactly), dir)

	if _, found := LookBin(lc.pathEnv, name, avoid); !found {
		return pc.missing(name)
	}

	return nil
}

// pathSamples gives the fields that the working directory of a contribution is built from the path
// of the repository as their sample, since a directory that comes from a field can only be
// checked with one that exists. The registry checks the real value when the form is answered.
//
// Parameters:
//   - pc: preparation under way, whose values the samples are put in
//   - e: the contribution
func pathSamples(pc *prep, e *Entry) {
	if e.Cwd == "" || pc.vals.Repo == nil {
		return
	}

	t, err := ParseTemplate(e.Cwd)
	if err != nil {
		return
	}

	for _, part := range t.Parts {
		if h := part.Holder; h != nil && h.Kind == KindField {
			fv := pc.vals.Fields[h.Name]
			fv.Text = pc.vals.Repo.Path
			pc.vals.Fields[h.Name] = fv
		}
	}
}

// sampleOf gives a field a value that passes the checks of its type, for the test of whether a
// contribution can be built. The value is never shown and never run.
//
// Parameters:
//   - f: the field
func sampleOf(f *Field) FieldValue {
	switch f.Type {
	case "bool":
		return FieldValue{Type: f.Type}
	case "choice":
		for _, c := range f.Choices {
			if c != "" {
				return FieldValue{Text: c, Type: f.Type}
			}
		}

		return FieldValue{Type: f.Type}
	}

	return FieldValue{Text: "x", Type: f.Type}
}
