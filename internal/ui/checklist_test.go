package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestChecklist checks the keys of the checklist and that an update never changes the previous
// value.
//
// Parameters:
//   - t: test to report to
func TestChecklist(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e

	c := newChecklist([]checkItem{{Label: "one"}, {Label: "two", Checked: true}, {Label: "three"}})

	if len(c.Checked()) != 0 {
		t.Fatalf("a line is ticked at the start: %v", c.Checked())
	}

	steps := []struct {
		key  string
		want []int
		cur  int
	}{
		{"space", []int{0}, 0},
		{"down", []int{0}, 1},
		{"space", []int{0, 1}, 1},
		{"down", []int{0, 1}, 2},
		{"down", []int{0, 1}, 2},
		{"space", []int{0, 1, 2}, 2},
		{"space", []int{0, 1}, 2},
		{"a", []int{0, 1, 2}, 2},
		{"a", nil, 2},
		{"a", []int{0, 1, 2}, 2},
		{"a", nil, 2},
		{"up", nil, 1},
		{"up", nil, 0},
		{"up", nil, 0},
		{"x", nil, 0},
		{"k", nil, 0},
		{"j", nil, 1},
	}

	for _, st := range steps {
		before := c
		c = c.Update(e, keyMsg(st.key))

		if !slices.Equal(c.Checked(), st.want) || c.cur != st.cur {
			t.Errorf("after %q: checked %v cursor %d, want %v %d", st.key, c.Checked(), c.cur,
				st.want, st.cur)
		}

		if st.key == "space" && slices.Equal(before.Checked(), c.Checked()) {
			t.Errorf("space changed nothing")
		}

		if c.Done() {
			t.Errorf("after %q: done", st.key)
		}
	}

	prev := c
	c.Update(e, keyMsg("space"))

	if !slices.Equal(prev.Checked(), c.Checked()) {
		t.Error("Update modified the value it was called on")
	}

	if got := c.Update(e, tea.WindowSizeMsg{}); !slices.Equal(got.Checked(), c.Checked()) {
		t.Error("a message that is not a key changed the checklist")
	}

	empty := newChecklist(nil).Update(e, keyMsg("space"))

	if len(empty.Checked()) != 0 || len(empty.View(e, 40)) != 0 {
		t.Error("an empty checklist is not empty")
	}
}

// TestChecklistConfirm checks that Enter confirms and that another key takes the confirmation
// back.
//
// Parameters:
//   - t: test to report to
func TestChecklistConfirm(t *testing.T) {
	e := newRig(t, newSandbox(t)).r.e
	c := newChecklist([]checkItem{{Label: "one"}, {Label: "two"}})

	if c.Done() {
		t.Fatal("a new checklist is done")
	}

	c = c.Update(e, keyMsg("space")).Update(e, keyMsg("enter"))

	if !c.Done() || !slices.Equal(c.Checked(), []int{0}) {
		t.Errorf("done %v checked %v", c.Done(), c.Checked())
	}

	if c = c.Update(e, keyMsg("down")); c.Done() {
		t.Error("a key did not take the confirmation back")
	}

	if empty := newChecklist(nil).Update(e, keyMsg("enter")); !empty.Done() {
		t.Error("an empty checklist cannot be confirmed")
	}

	if c = c.Update(e, keyMsg("enter")); !c.Done() || !slices.Equal(c.Checked(), []int{0}) {
		t.Errorf("done %v checked %v", c.Done(), c.Checked())
	}
}

// TestChecklistNeverPreticked checks that no line is ticked at the start, whatever the items say.
//
// Parameters:
//   - t: test to report to
func TestChecklistNeverPreticked(t *testing.T) {
	items := []checkItem{{Label: "a", Checked: true}, {Label: "b", Checked: true}}
	c := newChecklist(items)

	if len(c.Checked()) != 0 {
		t.Errorf("ticked at the start: %v", c.Checked())
	}

	if !items[0].Checked {
		t.Error("the caller's items were changed")
	}
}

// TestChecklistView checks the drawing of the boxes.
//
// Parameters:
//   - t: test to report to
func TestChecklistView(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e
	c := newChecklist([]checkItem{{Label: "plugins"}, {Label: "a very long label " +
		strings.Repeat("x", 80)}}).Update(e, keyMsg("space"))

	lines := c.View(e, 40)

	if len(lines) != 2 {
		t.Fatalf("%d lines", len(lines))
	}

	if got := stripStyles(lines[0]); !strings.Contains(got, "[x]  plugins") {
		t.Errorf("first line = %q", got)
	}

	if got := stripStyles(lines[1]); !strings.Contains(got, "[ ]  a very") {
		t.Errorf("second line = %q", got)
	}

	for _, l := range lines {
		if w := e.Metric.width(l); w > 40 {
			t.Errorf("line is %d cells", w)
		}
	}
}
