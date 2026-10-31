package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/config"
)

// TestChooseLayout checks the thresholds of the three layouts and the settings that override
// them.
//
// Parameters:
//   - t: test to report to
func TestChooseLayout(t *testing.T) {
	ui := func(compact, welcome string) config.UIConfig {
		u := config.DefaultSettings().UI
		u.Compact, u.Welcome = compact, welcome

		return u
	}

	tests := []struct {
		name string
		w, h int
		ui   config.UIConfig
		want layoutMode
	}{
		{"auto big", 120, 40, ui("auto", "full"), layoutFull},
		{"auto edge", 70, 20, ui("auto", "full"), layoutFull},
		{"auto narrow", 69, 40, ui("auto", "full"), layoutCompact},
		{"auto short", 120, 19, ui("auto", "full"), layoutCompact},
		{"auto smallest compact", 24, 7, ui("auto", "full"), layoutCompact},
		{"too narrow", 23, 40, ui("auto", "full"), layoutTiny},
		{"too short", 120, 6, ui("auto", "full"), layoutTiny},
		{"old minimum is compact", 39, 9, ui("auto", "full"), layoutCompact},
		{"always", 120, 40, ui("always", "full"), layoutCompact},
		{"welcome compact", 120, 40, ui("auto", "compact"), layoutCompact},
		{"never keeps full", 50, 12, ui("never", "full"), layoutFull},
		{"never keeps full at its edge", 40, 10, ui("never", "full"), layoutFull},
		{"never gives way when short", 50, 9, ui("never", "full"), layoutCompact},
		{"never gives way when narrow", 39, 12, ui("never", "full"), layoutCompact},
		{"never still tiny", 23, 12, ui("never", "full"), layoutTiny},
		{"always still tiny", 30, 6, ui("always", "full"), layoutTiny},
		{"zero", 0, 0, ui("auto", "full"), layoutTiny},
	}

	for _, tt := range tests {
		if got := chooseLayout(tt.w, tt.h, tt.ui); got != tt.want {
			t.Errorf("%s: chooseLayout(%d, %d) = %d, want %d", tt.name, tt.w, tt.h, got, tt.want)
		}
	}
}

// TestColumnWidths checks which columns are kept and how wide they are.
//
// Parameters:
//   - t: test to report to
func TestColumnWidths(t *testing.T) {
	nat := []int{10, 20, 8, 30}

	tests := []struct {
		avail int
		want  []int
	}{
		{200, []int{20, 40, 16, 118}},
		{74, []int{10, 20, 8, 30}},
		{70, []int{10, 20, 8, 26}},
		{60, []int{10, 20, 8, 16}},
		{44, []int{11, 21, 8}},
		{30, []int{10, 18}},
		{14, []int{10}},
		{5, []int{5}},
		{0, []int{0}},
	}

	for _, tt := range tests {
		got := columnWidths(nat, tt.avail)

		if !slices.Equal(got, tt.want) {
			t.Errorf("columnWidths(%d) = %v, want %v", tt.avail, got, tt.want)
		}

		sum := columnGap * (len(got) - 1)
		for _, w := range got {
			sum += w
		}

		if sum > max(tt.avail, 0) {
			t.Errorf("columnWidths(%d) = %v takes %d cells", tt.avail, got, sum)
		}
	}

	if got := columnWidths(nil, 10); got != nil {
		t.Errorf("columnWidths(nil) = %v, want nil", got)
	}
}

// TestScrollTo checks that the selected row stays visible, on a row of its own and not on a
// "N more" line, and that the window never scrolls past the end.
//
// Parameters:
//   - t: test to report to
func TestScrollTo(t *testing.T) {
	tests := []struct {
		sel, off, rows, n int
		want              int
	}{
		{0, 0, 5, 20, 0},
		{3, 0, 5, 20, 0},
		{4, 0, 5, 20, 2},
		{5, 0, 5, 20, 3},
		{18, 0, 5, 20, 16},
		{19, 0, 5, 20, 16},
		{2, 10, 5, 20, 2},
		{3, 10, 5, 5, 0},
		{0, 3, 5, 0, 0},
		{1, 0, 2, 10, 0},
		{2, 0, 2, 10, 1},
		{9, 0, 2, 10, 8},
	}

	for _, tt := range tests {
		if got := scrollTo(tt.sel, tt.off, tt.rows, tt.n); got != tt.want {
			t.Errorf("scrollTo(%d, %d, %d, %d) = %d, want %d",
				tt.sel, tt.off, tt.rows, tt.n, got, tt.want)
		}
	}
}

// TestFitLines checks the exact height of the lines.
//
// Parameters:
//   - t: test to report to
func TestFitLines(t *testing.T) {
	if got := fitLines([]string{"a", "b", "c"}, 2); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("cut = %q", got)
	}

	if got := fitLines([]string{"a"}, 3); !slices.Equal(got, []string{"a", "", ""}) {
		t.Errorf("padded = %q", got)
	}

	if got := fitLines([]string{"a"}, 0); got != nil {
		t.Errorf("zero = %q", got)
	}
}

// TestPlural checks the plural rule of each language.
//
// Parameters:
//   - t: test to report to
func TestPlural(t *testing.T) {
	tests := []struct {
		lang string
		n    int
		want string
	}{
		{"en", 0, "other"},
		{"en", 1, "one"},
		{"en", 2, "other"},
		{"fr", 0, "one"},
		{"fr", 1, "one"},
		{"fr", 2, "other"},
	}

	for _, tt := range tests {
		if got := plural(tt.lang, tt.n); got != tt.want {
			t.Errorf("plural(%q, %d) = %q, want %q", tt.lang, tt.n, got, tt.want)
		}
	}
}

// TestCountText checks the count of every kind in both languages.
//
// Parameters:
//   - t: test to report to
func TestCountText(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e

	tests := []struct {
		k    kind
		n    int
		want string
	}{
		{kindConn, 1, "1 connection"},
		{kindConn, 6, "6 connections"},
		{kindRepo, 0, "0 repos"},
		{kindAlias, 1, "1 alias"},
		{kindFunc, 2, "2 functions"},
	}

	for _, tt := range tests {
		got := countText(e, tt.k, tt.n)

		if !strings.HasPrefix(got, tt.want[:1]) || !strings.Contains(got, tt.want[2:4]) {
			t.Errorf("countText(%d, %d) = %q, want like %q", tt.k, tt.n, got, tt.want)
		}
	}
}

// TestKinds checks the catalog keys of every kind exist in the catalog.
//
// Parameters:
//   - t: test to report to
func TestKinds(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e

	for _, k := range allKinds {
		keys := []string{
			k.titleKey(), k.singularKey(), k.shortKey(), k.shortOneKey(),
			"ui_count_" + kindSlug[k] + "_one", "ui_help_open_" + kindSlug[k],
		}

		for _, c := range k.columns() {
			keys = append(keys, c.head)
		}

		for _, key := range keys {
			if got := e.t(key, 1); strings.HasPrefix(got, "!") {
				t.Errorf("kind %d: key %s is missing", k, key)
			}
		}
	}
}

// TestBoxed checks that a box is rectangular and drops a title that does not fit.
//
// Parameters:
//   - t: test to report to
func TestBoxed(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e

	box := boxed(e, "title", []string{"one", "a very long line that must be cut"}, 20)

	if len(box) != 4 {
		t.Fatalf("box has %d lines, want 4", len(box))
	}

	for _, l := range box {
		if w := e.Metric.width(l); w != 20 {
			t.Errorf("line %q is %d cells, want 20", stripStyles(l), w)
		}
	}

	if !strings.Contains(stripStyles(box[0]), "title") {
		t.Errorf("title missing from %q", stripStyles(box[0]))
	}

	narrow := boxed(e, "a title that is much too long", nil, 10)

	if strings.Contains(stripStyles(narrow[0]), "title") || e.Metric.width(narrow[0]) != 10 {
		t.Errorf("narrow box = %q", stripStyles(narrow[0]))
	}

	if got := boxed(e, "", nil, 1); e.Metric.width(got[0]) != 4 {
		t.Errorf("minimum box is %d cells wide, want 4", e.Metric.width(got[0]))
	}
}

// TestPageGeometry checks the width and the left margin of the page on terminals of every width,
// for each value of ui.width and for a value that is empty or unknown, which counts as "auto".
//
// Parameters:
//   - t: test to report to
func TestPageGeometry(t *testing.T) {
	widths := []int{0, 40, 70, 100, 119, 120, 121, 159, 160, 161, 195, 200, 300}
	caps := map[string]int{"auto": -1, "120": 120, "160": 160, "": -1, "full": -1, "bogus": -1}

	for setting, limit := range caps {
		for _, cols := range widths {
			wantW := cols
			if limit > 0 {
				wantW = min(cols, limit)
			}

			wantPad := (cols - wantW) / 2

			w, pad := pageGeometry(cols, setting)

			if w != wantW || pad != wantPad {
				t.Errorf("pageGeometry(%d, %q) = %d, %d, want %d, %d", cols, setting, w, pad,
					wantW, wantPad)
			}

			if pad < 0 || 2*pad+w > cols {
				t.Errorf("pageGeometry(%d, %q): margin %d and page %d are wider than the terminal",
					cols, setting, pad, w)
			}

			if cols < 120 && (w != cols || pad != 0) {
				t.Errorf("pageGeometry(%d, %q) = %d, %d: a terminal below every cap must keep "+
					"the page as wide as itself", cols, setting, w, pad)
			}
		}
	}
}

// TestPageGeometryTable pins the values for the terminals the owner uses, so that a change of the
// rule shows in the diff of the test.
//
// Parameters:
//   - t: test to report to
func TestPageGeometryTable(t *testing.T) {
	tests := []struct {
		cols    int
		setting string
		width   int
		pad     int
	}{
		{40, "120", 40, 0},
		{70, "160", 70, 0},
		{100, "auto", 100, 0},
		{120, "120", 120, 0},
		{121, "120", 120, 0},
		{160, "120", 120, 20},
		{195, "120", 120, 37},
		{200, "120", 120, 40},
		{300, "120", 120, 90},
		{121, "160", 121, 0},
		{160, "160", 160, 0},
		{161, "160", 160, 0},
		{195, "160", 160, 17},
		{200, "160", 160, 20},
		{300, "160", 160, 70},
		{121, "auto", 121, 0},
		{195, "auto", 195, 0},
		{200, "auto", 200, 0},
		{300, "auto", 300, 0},
		{200, "", 200, 0},
		{200, "full", 200, 0},
	}

	for _, tt := range tests {
		w, pad := pageGeometry(tt.cols, tt.setting)

		if w != tt.width || pad != tt.pad {
			t.Errorf("pageGeometry(%d, %q) = %d, %d, want %d, %d", tt.cols, tt.setting, w, pad,
				tt.width, tt.pad)
		}
	}
}

// TestClip checks the cutting of styled lines.
//
// Parameters:
//   - t: test to report to
func TestClip(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e
	styled := e.Theme.Title.Render("hello world")

	if got := e.Metric.width(e.clip(styled, 5)); got != 5 {
		t.Errorf("clip width = %d, want 5", got)
	}

	if got := e.clip(styled, 50); got != styled {
		t.Error("clip changed a line that fits")
	}

	if got := e.Metric.width(e.clipPad(styled, 15)); got != 15 {
		t.Errorf("clipPad width = %d, want 15", got)
	}

	if got := e.clip(styled, -3); e.Metric.width(got) != 0 {
		t.Errorf("clip to a negative width = %q", got)
	}
}
