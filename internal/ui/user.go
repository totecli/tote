package ui

import (
	"errors"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// maxUsers is the most logins that the keys "u" and "U" offer.
const maxUsers = 12

// userChoices lists the logins that "u" cycles through and "U" offers for a connection.
//
// Parameters:
//   - e: shared environment, which gives the connections and the login of the user
//   - c: connection that is about to be opened
//
// Returns the stored login first, written as the empty string, then root, the login of the
// user running tote and the logins of the other connections, each once and at most maxUsers in
// all. A login that the connection would refuse is left out, which leaves the stored login alone
// for a connection that cannot take another one.
func userChoices(e *env, c store.Connection) []string {
	out := []string{""}

	add := func(user string) {
		if user == "" || len(out) >= maxUsers || slices.Contains(out, user) || user == c.User {
			return
		}

		if _, err := shell.ConnectArgv(c, user); err == nil {
			out = append(out, user)
		}
	}

	add("root")
	add(e.User)

	for _, it := range e.Data.items(kindConn) {
		if other, ok := it.Rec.(store.Connection); ok {
			add(other.User)
		}
	}

	return out
}

// cycleUser moves to the next login for the connection under the cursor and says which one it
// is. The choice only lasts for the next connection made from this list.
//
// Parameters:
//   - e: shared environment
//   - it: connection under the cursor
func (s *listScreen) cycleUser(e *env, it *item) tea.Cmd {
	c, ok := it.Rec.(store.Connection)
	if !ok {
		return nil
	}

	if _, err := shell.ConnectArgv(c, "root"); errors.Is(err, shell.ErrUserOverride) {
		return toastCmd(e.t("ui_user_refused", sanitize(c.Name)), true)
	}

	choices := userChoices(e, c)
	next := choices[(max(0, slices.Index(choices, s.users[it.Key]))+1)%len(choices)]

	if next == "" {
		delete(s.users, it.Key)

		return toastCmd(e.t("ui_user_stored"), false)
	}

	s.users[it.Key] = next

	return toastCmd(e.t("ui_user_set", sanitize(next)), false)
}

// pickUser opens the list of logins to connect as. Choosing one connects at once; the last entry
// asks for a login that is not in the list.
//
// Parameters:
//   - m: connection and the login that is chosen at the moment
func (r *root) pickUser(m pickUserMsg) tea.Cmd {
	e := r.e

	rec, ok := e.Data.record(kindConn, m.Key)
	if !ok {
		return r.say(e.t("cli_not_found", e.Data.kindName(kindConn), sanitize(m.Key)), true)
	}

	c := rec.(store.Connection)

	if _, err := shell.ConnectArgv(c, "root"); errors.Is(err, shell.ErrUserOverride) {
		return r.say(e.t("ui_user_refused", sanitize(c.Name)), true)
	}

	choices := userChoices(e, c)
	items := make([]pickItem, 0, len(choices)+1)

	for _, u := range choices {
		it := pickItem{label: sanitize(u), value: u}

		if u == "" {
			it.label = e.t("ui_user_default")
			it.note = sanitize(c.User)
		}

		items = append(items, it)
	}

	const other = "\x00other"

	items = append(items, pickItem{label: e.t("ui_user_other"), value: other})

	done := func(e *env, v string) tea.Cmd {
		if v != other {
			return msgCmd(activateMsg{Kind: kindConn, Key: m.Key, User: v})
		}

		check := func(e *env, text string) string {
			_, err := shell.ConnectArgv(c, text)

			return fieldError(e, err)
		}

		ask := newPrompt(e.t("ui_user_title", sanitize(c.Name)), e.t("ui_user_prompt"), m.Cur,
			check, func(_ *env, text string) tea.Cmd {
				return msgCmd(activateMsg{Kind: kindConn, Key: m.Key, User: text})
			})

		return msgCmd(openOverlayMsg{Overlay: ask})
	}

	at := max(0, slices.Index(choices, m.Cur))

	return r.push(newPick(e.t("ui_user_title", sanitize(c.Name)), items, at, done))
}
