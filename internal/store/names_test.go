package store

import "testing"

// TestSharedName checks the detection of a name or an alias that another connection holds.
//
// Parameters:
//   - t: test to report to
func TestSharedName(t *testing.T) {
	list := []Connection{
		{Name: "prod", Aliases: []string{"p", "Web"}},
		{Name: "db", Aliases: []string{"data"}},
	}

	tests := []struct {
		name      string
		skip      string
		rec       Connection
		word      string
		owner     string
		wantFound bool
	}{
		{name: "free", rec: Connection{Name: "new", Aliases: []string{"n"}}},
		{name: "same name is left to the store", rec: Connection{Name: "prod"}},
		{
			name: "name is an alias", rec: Connection{Name: "p"}, word: "p", owner: "prod",
			wantFound: true,
		},
		{
			name: "name is an alias ignoring case", rec: Connection{Name: "WEB"}, word: "WEB",
			owner: "prod", wantFound: true,
		},
		{
			name: "alias is a name", rec: Connection{Name: "x", Aliases: []string{"DB"}},
			word: "DB", owner: "db", wantFound: true,
		},
		{
			name: "alias is an alias", rec: Connection{Name: "x", Aliases: []string{"ok", "data"}},
			word: "data", owner: "db", wantFound: true,
		},
		{
			name: "own record is skipped", skip: "prod",
			rec: Connection{Name: "prod", Aliases: []string{"p", "Web", "more"}},
		},
		{
			name: "rename keeps the other records", skip: "prod",
			rec: Connection{Name: "main", Aliases: []string{"data"}}, word: "data",
			owner: "db", wantFound: true,
		},
		{
			name: "rename onto another name is left to the store", skip: "prod",
			rec: Connection{Name: "db"},
		},
	}

	for _, tt := range tests {
		word, owner, found := SharedName(list, tt.skip, tt.rec)

		if found != tt.wantFound || word != tt.word || owner != tt.owner {
			t.Errorf("%s: got (%q, %q, %v), want (%q, %q, %v)", tt.name, word, owner, found,
				tt.word, tt.owner, tt.wantFound)
		}
	}

	if _, _, found := SharedName(nil, "", Connection{Name: "a"}); found {
		t.Error("an empty list shares nothing")
	}
}

// TestReservedRecordName checks the words and the prefixes that no connection, alias or
// repository may take, with any case, and some neighbours that stay free.
//
// Parameters:
//   - t: test to report to
func TestReservedRecordName(t *testing.T) {
	tests := []struct {
		name   string
		word   bool
		prefix bool
	}{
		{"add", true, false},
		{"rm", true, false},
		{"list", true, false},
		{"ls", true, false},
		{"open", true, false},
		{"edit", true, false},
		{"import", true, false},
		{"find", true, false},
		{"status", true, false},
		{"tunnel", true, false},
		{"tunnels", true, false},
		{"snippets", true, false},
		{"sync", true, false},
		{"refresh", true, false},
		{"restore", true, false},
		{"keep", true, false},
		{"pick", true, false},
		{"export", true, false},
		{"tags", true, false},
		{"fav", true, false},
		{"EDIT", true, false},
		{"Fav", true, false},
		{"__tote_pick", false, true},
		{"__tote_", false, true},
		{"_tote", false, true},
		{"_tote_x", false, true},
		{"_toteX", false, true},
		{"__TOTE_x", false, true},
		{"editor", false, false},
		{"web1", false, false},
		{"my-list", false, false},
		{"tote", false, false},
		{"_tot", false, false},
		{"x_tote", false, false},
		{"", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReservedWord(tt.name); got != tt.word {
				t.Errorf("ReservedWord(%q) = %v", tt.name, got)
			}

			if got := ReservedPrefix(tt.name); got != tt.prefix {
				t.Errorf("ReservedPrefix(%q) = %v", tt.name, got)
			}

			if got := ReservedRecordName(tt.name); got != (tt.word || tt.prefix) {
				t.Errorf("ReservedRecordName(%q) = %v", tt.name, got)
			}
		})
	}
}

// TestReservedNamesDoNotBreakLoading checks that a file edited by hand with a reserved name
// still loads: the rule belongs to the add path and to the doctor, not to the validation of a
// record that is read.
//
// Parameters:
//   - t: test to report to
func TestReservedNamesDoNotBreakLoading(t *testing.T) {
	for _, name := range []string{"edit", "__tote_pick", "_tote"} {
		if err := (Connection{Name: name, Target: "h.example.org"}).Validate(); err != nil {
			t.Errorf("connection %q: %v", name, err)
		}
	}
}
