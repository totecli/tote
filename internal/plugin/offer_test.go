package plugin

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// offerIDs lists the ids of offers.
//
// Parameters:
//   - list: offers
func offerIDs(list []Offer) []string {
	var out []string

	for _, o := range list {
		out = append(out, o.ID)
	}

	return out
}

// TestOffers checks the commands of the enabled plugins: their order, their words in the language
// asked and the facts the palette needs.
func TestOffers(t *testing.T) {
	l, _ := gftLab(t)
	r := l.reg(nil)

	got := r.Offers(r.Load(), "en")
	if want := []string{"show", "open", "get", "install"}; !slices.Equal(offerIDs(got), want) {
		t.Fatalf("offers %v want %v", offerIDs(got), want)
	}

	show := got[0]
	if show.Plugin != "gft" || show.Table != TableCommand ||
		show.Label != "Show a GitHub release" || !show.Form || show.Confirm || show.Open ||
		show.Output != OutputPager ||
		!slices.Equal(show.Keywords, []string{"release", "assets", "notes"}) {
		t.Errorf("show = %+v", show)
	}

	if !got[1].Open || !got[3].Confirm || got[2].Output != OutputTerminal {
		t.Errorf("open %+v install %+v get %+v", got[1], got[3], got[2])
	}

	fr := r.Offers(r.Load(), "fr")
	if fr[0].Label == show.Label || fr[0].Label == "" {
		t.Errorf("French label %q", fr[0].Label)
	}

	if _, err := r.Disable("gft"); err != nil {
		t.Fatal(err)
	}

	if off := r.Offers(r.Load(), "en"); len(off) != 0 {
		t.Errorf("a disabled plugin offers %v", offerIDs(off))
	}
}

// TestRepoOffers checks the actions on a saved repository and why one cannot run.
func TestRepoOffers(t *testing.T) {
	l, _ := gftLab(t)
	l.gitRepo("plain", "")
	r := l.reg(nil)

	byID := func(list []Offer) map[string]Offer {
		out := map[string]Offer{}
		for _, o := range list {
			out[o.ID] = o
		}

		return out
	}

	mine := byID(r.RepoOffers(r.Load(), "myrepo", "en"))
	if len(mine) != 7 {
		t.Fatalf("%d actions: %v", len(mine), mine)
	}

	for id, o := range mine {
		if !o.Available || o.Why != nil || o.Table != TableItemAction {
			t.Errorf("%s: %+v", id, o)
		}
	}

	plain := byID(r.RepoOffers(r.Load(), "plain", "en"))

	for _, id := range []string{"release", "assets", "notes"} {
		o := plain[id]
		if o.Available || o.Why == nil || o.Why.Code != CodeUnavailable ||
			o.Why.Args[0] != "repo.slug" {
			t.Errorf("%s: %+v", id, o)
		}
	}

	for _, id := range []string{"stash-list", "stash-show", "stash-drop", "stash-clean"} {
		if !plain[id].Available {
			t.Errorf("%s must work without a remote: %+v", id, plain[id])
		}
	}

	if !plain["stash-drop"].Form || !plain["stash-drop"].Confirm {
		t.Errorf("drop: %+v", plain["stash-drop"])
	}

	for id, o := range byID(r.RepoOffers(r.Load(), "nobody", "en")) {
		if o.Available || o.Why == nil || o.Why.Code != CodeUnavailable {
			t.Errorf("unknown repo %s: %+v", id, o)
		}
	}
}

// TestRepoOffersMissingProgram checks that an action whose program is gone says so, with the hint
// of the plugin, and that nothing is started to find out.
func TestRepoOffersMissingProgram(t *testing.T) {
	l, _ := gftLab(t)
	r := l.reg(nil)

	if err := os.Remove(filepath.Join(l.bin, "gft")); err != nil {
		t.Fatal(err)
	}

	for _, o := range r.RepoOffers(r.Load(), "myrepo", "en") {
		if o.Available || o.Why == nil || o.Why.Code != CodeDepMissing || o.Why.Args[0] != "gft" ||
			len(o.Why.Args) < 2 || o.Why.Args[1] == "" {
			t.Errorf("%s: %+v", o.ID, o)
		}
	}
}

// TestRepoOffersAgreeWithPrepare checks that the answer of RepoOffers about an action is the one
// Prepare gives once the form is answered with a real value, over a set of actions: one without a
// form, one that needs the remote, one whose working directory comes from a field and one whose
// program is not installed.
func TestRepoOffersAgreeWithPrepare(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()
	l.script("tool", "exit 0\n")

	body := `
[[item_action]]
id = "plain"
label = "Plain"
target = "repo"
run = ["tool"]
output = "pager"

[[item_action]]
id = "slug"
label = "Slug"
target = "repo"
run = ["tool", "{repo.slug}"]
output = "pager"

[[item_action]]
id = "where"
label = "Where"
target = "repo"
run = ["tool"]
cwd = "{dir}"
output = "pager"
[[item_action.field]]
id = "dir"
type = "text"
label = "Directory"

[[item_action]]
id = "ghost"
label = "Ghost"
target = "repo"
run = ["ghost"]
output = "pager"
`

	l.plug("demo", []string{"exec:tool", "exec:ghost", "host:read:repos"}, body)

	paths := map[string]string{
		"withremote": l.gitRepo("withremote", "git@github.com:example/demo.git"),
		"noremote":   l.gitRepo("noremote", ""),
	}

	r := l.reg(nil)

	for name, path := range paths {
		for _, o := range r.RepoOffers(r.Load(), name, "en") {
			call := Call{Plugin: "demo", Table: TableItemAction, Entry: o.ID, Repo: name}
			if o.ID == "where" {
				call.Values = map[string]string{"dir": path}
			}

			_, err := r.Prepare(call)

			if o.Available != (err == nil) {
				t.Errorf("%s/%s: offered available=%v (%+v), Prepare says %v", name, o.ID,
					o.Available, o.Why, err)
			}
		}
	}

	got := map[string]bool{}

	for _, o := range r.RepoOffers(r.Load(), "noremote", "en") {
		got[o.ID] = o.Available
	}

	want := map[string]bool{"plain": true, "slug": false, "where": true, "ghost": false}
	if !equalBools(got, want) {
		t.Errorf("without a remote: %v want %v", got, want)
	}
}

// equalBools reports whether two sets of flags are the same.
//
// Parameters:
//   - a: first set
//   - b: second set
func equalBools(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}

	return true
}
