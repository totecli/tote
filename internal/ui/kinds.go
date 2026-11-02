package ui

// kind identifies one of the four collections the interface edits.
type kind int

const (
	// kindConn is the connections collection.
	kindConn kind = iota

	// kindRepo is the repositories collection.
	kindRepo

	// kindAlias is the aliases collection.
	kindAlias

	// kindFunc is the functions collection.
	kindFunc
)

// allKinds lists the kinds in the order of the menu.
var allKinds = []kind{kindConn, kindRepo, kindAlias, kindFunc}

// titleKey returns the catalog key of the plural name of a kind, used for tabs and titles.
func (k kind) titleKey() string {
	return [...]string{
		"ui_kind_connections", "ui_kind_repos", "ui_kind_aliases", "ui_kind_funcs",
	}[k]
}

// singularKey returns the catalog key of the singular name of a kind.
func (k kind) singularKey() string {
	return [...]string{"kind_connection", "kind_repo", "kind_alias", "kind_func"}[k]
}

// shortKey returns the catalog key of the short plural name used by the compact header.
func (k kind) shortKey() string {
	return [...]string{
		"ui_short_connections", "ui_short_repos", "ui_short_aliases", "ui_short_funcs",
	}[k]
}

// shortOneKey returns the catalog key of the short singular name used by the compact header.
func (k kind) shortOneKey() string {
	return "ui_short_" + kindSlug[k] + "_one"
}

// column describes one column of a list: the catalog key of its heading and the widest it may
// be before the text is cut.
type column struct {
	head string
	max  int
}

// columns returns the columns of a kind, the most important first. The last column takes the
// room that is left; columns are dropped from the end when the screen is too narrow.
func (k kind) columns() []column {
	switch k {
	case kindConn:
		return []column{
			{"header_name", 22},
			{"header_target", 30},
			{"header_user", 14},
			{"header_description", 80},
		}
	case kindRepo:
		return []column{{"header_name", 22}, {"header_path", 44}, {"header_description", 80}}
	case kindAlias:
		return []column{{"header_name", 22}, {"header_command", 60}, {"header_description", 80}}
	}

	return []column{{"header_name", 22}, {"header_kind", 8}, {"header_description", 80}}
}
