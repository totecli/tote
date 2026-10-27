package store

import "strings"

// reservedWords lists the names that no connection, alias or repository may take, because tote
// reads them as a word of its own command line.
//
// The first group holds the sub-verbs of today: "tote co edit" connects to a host named edit
// until a verb of that name exists, and then it would change meaning for a record written
// before. The second group holds the sub-verbs that are planned, reserved from the first release
// so that adding one never takes a name from a record. Names are compared without regard to case.
var reservedWords = map[string]bool{
	"add": true, "rm": true, "list": true, "ls": true, "open": true,

	"edit": true, "import": true, "find": true, "status": true, "tunnel": true,
	"tunnels": true, "snippets": true, "sync": true, "refresh": true, "restore": true,
	"keep": true, "pick": true, "export": true, "tags": true, "fav": true,
}

// reservedPrefixes lists the beginnings of the names that the program keeps for the functions it
// writes into the shell: the wrapper helpers are __tote_unload and __tote_complete, and the
// completion function is _tote. A name that starts with one of them could collide with a helper
// added later. Names are compared without regard to case.
var reservedPrefixes = []string{"__tote_", "_tote"}

// ReservedWord tells whether a name is one that tote reads as a word of its command line.
//
// Parameters:
//   - name: candidate record name; the comparison ignores case
func ReservedWord(name string) bool {
	return reservedWords[strings.ToLower(name)]
}

// ReservedPrefix tells whether a name starts with a prefix that tote keeps for the helper
// functions of its shell integration.
//
// Parameters:
//   - name: candidate record name; the comparison ignores case
func ReservedPrefix(name string) bool {
	lower := strings.ToLower(name)

	for _, p := range reservedPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	return false
}

// ReservedRecordName tells whether a name may not be given to a new connection, alias or
// repository: a word of the command line (ReservedWord) or a reserved prefix (ReservedPrefix).
//
// It is the rule of "add" and of the edit forms. It is not part of the validation of a record
// that is read, so that a file edited by hand keeps loading; "tote doctor" warns about it.
//
// Parameters:
//   - name: candidate record name
func ReservedRecordName(name string) bool {
	return ReservedWord(name) || ReservedPrefix(name)
}

// SharedName looks for a connection that already uses a name or an alias of a new or changed
// connection, because a handle must designate one host only. The comparison ignores case.
//
// Parameters:
//   - list: connections of the file, in file order
//   - skip: key of the connection being replaced, which is not compared with itself; empty when
//     the connection is new
//   - rec: connection about to be written
//
// Returns the shared word, the name of the connection that owns it and true when one is found.
// A name equal to the name of another connection is left out: the store reports it as an
// existing connection when the write is made.
func SharedName(list []Connection, skip string, rec Connection) (word, owner string, found bool) {
	words := append([]string{rec.Name}, rec.Aliases...)

	for _, other := range list {
		if skip != "" && other.Name == skip {
			continue
		}

		owned := append([]string{other.Name}, other.Aliases...)

		for i, w := range words {
			if i == 0 && other.Name == w {
				continue
			}

			for _, o := range owned {
				if strings.EqualFold(w, o) {
					return w, other.Name, true
				}
			}
		}
	}

	return "", "", false
}
