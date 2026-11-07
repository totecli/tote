package plugin

import "io/fs"

// Contributes counts what a plugin adds to tote: the numbers `plugin list --json` prints.
type Contributes struct {
	ItemActions int `json:"item_actions"`
	Commands    int `json:"commands"`
	Lists       int `json:"lists"`
	Settings    int `json:"settings"`
}

// ContributesOf counts the contributions of a manifest.
//
// Parameters:
//   - m: validated manifest, may be nil for a plugin that has none
func ContributesOf(m *Manifest) Contributes {
	if m == nil {
		return Contributes{}
	}

	return Contributes{
		ItemActions: len(m.ItemActions), Commands: len(m.Commands),
		Lists: len(m.Lists), Settings: len(m.Settings),
	}
}

// Piece names one contribution of a plugin: its table, its id and its label in a language.
type Piece struct {
	Table string `json:"table"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PiecesOf lists the contributions of a manifest, in the order of the file: the commands, the item
// actions, the actions of an exec plugin, the lists and the settings.
//
// Parameters:
//   - m: validated manifest, may be nil for a plugin that has none
//   - cat: catalog of the package, for the labels
//   - lang: language of the labels, such as "fr"; any other value gives English
func PiecesOf(m *Manifest, cat Catalog, lang string) []Piece {
	if m == nil {
		return nil
	}

	var out []Piece

	for _, group := range [][]Entry{m.Commands, m.ItemActions, m.Actions} {
		for _, e := range group {
			out = append(out, Piece{
				Table: e.Table, ID: e.ID,
				Label: cat.Text(lang, e.Table+"."+e.ID+".label", e.Label),
			})
		}
	}

	for _, l := range m.Lists {
		out = append(out, Piece{
			Table: "list", ID: l.ID,
			Label: cat.Text(lang, "list."+l.ID+".label", l.Label),
		})
	}

	for _, st := range m.Settings {
		out = append(out, Piece{
			Table: "setting", ID: st.ID,
			Label: cat.Text(lang, "setting."+st.ID+".label", st.Label),
		})
	}

	return out
}

// Available describes a plugin shipped with tote that is not installed, as `plugin list` shows
// it: the identity and the access it would ask for, read from the embedded copy and its pin.
// Title and Description are in the requested language when the plugin has a translation.
type Available struct {
	ID          string
	Version     string
	Tier        string
	Title       string
	Description string
	Caps        []string
	Grant       string
	Contributes Contributes
	Items       []Piece
}

// Available lists the plugins shipped with tote that the snapshot does not hold, in the order of
// the set. It reads the embedded files only: nothing is written, hashed or run, and an embedded
// plugin that does not decode is left out, because only a broken build can have one.
//
// Parameters:
//   - snap: what Load found
//   - lang: language of the texts, such as "fr"; any other value gives English
func (r *Registry) Available(snap Snapshot, lang string) []Available {
	set := r.opts.Bundled
	if set == nil {
		return nil
	}

	var out []Available

	for _, id := range set.IDs() {
		if _, installed := snap.Get(id); installed {
			continue
		}

		fsys, version, _, surface, ok := set.Lookup(id)
		if !ok || fsys == nil {
			continue
		}

		if a, ok := r.describeBundled(fsys, id, version, surface, lang); ok {
			out = append(out, a)
		}
	}

	return out
}

// describeBundled decodes the embedded manifest of a plugin.
//
// Parameters:
//   - fsys: embedded files of the plugin
//   - id: plugin id
//   - version: version of the pin
//   - surface: surface_sha256 of the pin
//   - lang: language of the texts
func (r *Registry) describeBundled(fsys fs.FS, id, version, surface, lang string) (Available,
	bool,
) {
	data, err := fs.ReadFile(fsys, manifestName)
	if err != nil {
		return Available{}, false
	}

	m, _ := Decode(data, DecodeOptions{Host: r.opts.Host, WantID: id, Official: true})
	if m == nil {
		return Available{}, false
	}

	locales := map[string]map[string]string{}

	for _, rel := range sortedKeys(localeFiles) {
		raw, rerr := fs.ReadFile(fsys, rel)
		if rerr != nil {
			continue
		}

		if table, issues := ParseLocale(raw); len(issues) == 0 {
			locales[localeFiles[rel]] = table
		}
	}

	cat := NewCatalog(MergeLocales(m, locales))
	caps := EffectiveCaps(m)

	return Available{
		ID:          id,
		Version:     version,
		Tier:        m.Tier,
		Title:       cat.Text(lang, "title", m.Title),
		Description: cat.Text(lang, "description", m.Description),
		Caps:        caps,
		Grant:       GrantToken(caps, surface),
		Contributes: ContributesOf(m),
		Items:       PiecesOf(m, cat, lang),
	}, true
}
