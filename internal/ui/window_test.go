package ui

import (
	"fmt"
	"strings"
	"testing"
)

// b2i turns a boolean into 0 or 1.
//
// Parameters:
//   - b: the boolean
func b2i(b bool) int {
	if b {
		return 1
	}

	return 0
}

// TestWindowInvariants checks the window of a list for every number of rows from 1 to 14, every
// list length from 0 to 45 and every first item: the items and the "N more" lines fill the rows
// exactly, the counts add up to the length of the list, a line exists only where items are
// hidden, and the last item can always be reached.
//
// Parameters:
//   - t: test to report to
func TestWindowInvariants(t *testing.T) {
	for rows := 1; rows <= 14; rows++ {
		for n := 0; n <= 45; n++ {
			last := maxOffset(rows, n)

			for off := -1; off <= n+2; off++ {
				w := windowOf(off, rows, n)
				name := fmt.Sprintf("rows %d, n %d, off %d", rows, n, off)

				if w.off < 0 || w.off > last {
					t.Fatalf("%s: first item %d, last allowed %d", name, w.off, last)
				}

				if w.above != w.off || w.above+w.count+w.below != n {
					t.Fatalf("%s: above %d, shown %d, below %d", name, w.above, w.count, w.below)
				}

				used := w.count + b2i(w.top) + b2i(w.bottom)
				if want := min(n, rows); used != want {
					t.Fatalf("%s: %d rows used (%+v), want %d", name, used, w, want)
				}

				lines := rows >= minMoreRows
				if w.top != (lines && w.above > 0) || w.bottom != (lines && w.below > 0) {
					t.Fatalf("%s: lines %v/%v with %d above and %d below", name, w.top, w.bottom,
						w.above, w.below)
				}

				if n > 0 && w.count < 1 {
					t.Fatalf("%s: no item is shown", name)
				}
			}

			if n == 0 {
				continue
			}

			if end := windowOf(last, rows, n); end.off+end.count != n || end.bottom {
				t.Fatalf("rows %d, n %d: the last window ends at %d with a line below %v", rows,
					n, end.off+end.count, end.bottom)
			}
		}
	}
}

// TestScrollToKeepsTheItemVisible checks, for the same sizes, that the selected item is always on
// a row of its own, that the window does not move when the item is already visible, that moving up
// puts the item on the first row, and that moving down scrolls as little as possible.
//
// Parameters:
//   - t: test to report to
func TestScrollToKeepsTheItemVisible(t *testing.T) {
	for rows := 1; rows <= 12; rows++ {
		for n := 1; n <= 40; n++ {
			last := maxOffset(rows, n)

			for off := 0; off <= last; off++ {
				for sel := 0; sel < n; sel++ {
					got := scrollTo(sel, off, rows, n)
					w := windowOf(got, rows, n)
					name := fmt.Sprintf("rows %d, n %d, off %d, sel %d -> %d", rows, n, off, sel,
						got)

					if sel < w.off || sel >= w.off+w.count {
						t.Fatalf("%s: the item is not shown (%+v)", name, w)
					}

					cur := windowOf(off, rows, n)

					switch {
					case sel >= cur.off && sel < cur.off+cur.count:
						if got != off {
							t.Fatalf("%s: the window moved though the item was visible", name)
						}
					case sel < cur.off:
						if got != sel {
							t.Fatalf("%s: going up did not put the item first", name)
						}
					default:
						if prev := windowOf(got-1, rows, n); sel < prev.off+prev.count {
							t.Fatalf("%s: scrolled by more than needed", name)
						}
					}
				}
			}
		}
	}
}

// TestScrollRowsStaysInTheWindow checks that a scroll by any number of items leaves the window in
// range and the selection inside it, for the same sizes.
//
// Parameters:
//   - t: test to report to
func TestScrollRowsStaysInTheWindow(t *testing.T) {
	for rows := 1; rows <= 12; rows++ {
		for n := 0; n <= 30; n++ {
			for _, lines := range []int{-50, -7, -3, -1, 0, 1, 3, 7, 50} {
				for sel := 0; sel < max(1, n); sel += 3 {
					gotSel, gotOff := scrollRows(sel, 0, rows, n, lines)
					w := windowOf(gotOff, rows, n)
					name := fmt.Sprintf("rows %d, n %d, sel %d, lines %d", rows, n, sel, lines)

					if gotOff < 0 || gotOff > maxOffset(rows, n) {
						t.Fatalf("%s: off %d out of range", name, gotOff)
					}

					if n > 0 && (gotSel < w.off || gotSel >= w.off+w.count) {
						t.Fatalf("%s: the selection %d left the window %+v", name, gotSel, w)
					}
				}
			}
		}
	}
}

// TestWindowStack checks the lines and the zones of a window: the "N more" lines hold the exact
// counts, the items sit between them, the zones point at the lines they are on, and the ASCII
// glyph set has its own arrows.
//
// Parameters:
//   - t: test to report to
func TestWindowStack(t *testing.T) {
	tests := []struct {
		name         string
		off, rows, n int
		ascii        bool
		wantFirst    string
		wantLast     string
		zoneIDs      []int
	}{
		{"top", 0, 6, 20, false, "item 0", "\u2193 15 more", []int{0, 1, 2, 3, 4, -2}},
		{
			"one down", 1, 6, 20, false, "\u2191 1 more", "\u2193 15 more",
			[]int{-1, 1, 2, 3, 4, -2},
		},
		{
			"middle", 9, 6, 20, false, "\u2191 9 more", "\u2193 7 more",
			[]int{-1, 9, 10, 11, 12, -2},
		},
		{
			"almost the end", 14, 6, 20, true, "^ 14 more", "v 2 more",
			[]int{-1, 14, 15, 16, 17, -2},
		},
		{"end", 15, 6, 20, false, "\u2191 15 more", "item 19", []int{-1, 15, 16, 17, 18, 19}},
		{"ascii", 3, 6, 20, true, "^ 3 more", "v 13 more", []int{-1, 3, 4, 5, 6, -2}},
		{"fits", 0, 6, 4, false, "item 0", "item 3", []int{0, 1, 2, 3}},
	}

	for _, tt := range tests {
		e := bareEnv(t, tt.ascii)
		w := windowOf(tt.off, tt.rows, tt.n)
		items := make([]string, w.count)

		for i := range items {
			items[i] = fmt.Sprintf("item %d", w.off+i)
		}

		lines, zs := w.stack(e, items, "  ", 2, 30)

		for i := range lines {
			lines[i] = strings.TrimSpace(stripStyles(lines[i]))
		}

		if lines[0] != tt.wantFirst || lines[len(lines)-1] != tt.wantLast ||
			len(lines) != min(tt.n, tt.rows) {
			t.Errorf("%s: lines %q, want %q ... %q", tt.name, lines, tt.wantFirst, tt.wantLast)
		}

		if len(zs) != len(tt.zoneIDs) {
			t.Fatalf("%s: %d zones, want %d", tt.name, len(zs), len(tt.zoneIDs))
		}

		for i, z := range zs {
			if z.id != tt.zoneIDs[i] || z.y != i || z.x != 2 || z.w != 30 {
				t.Errorf("%s: zone %d = %+v, want id %d on line %d", tt.name, i, z, tt.zoneIDs[i],
					i)
			}
		}
	}
}

// TestPageClick checks which way a click on a "N more" line scrolls.
//
// Parameters:
//   - t: test to report to
func TestPageClick(t *testing.T) {
	for _, tt := range []struct {
		id   int
		want int
		ok   bool
	}{
		{zoneMoreUp, -7, true},
		{zoneMoreDown, 7, true},
		{0, 0, false},
		{12, 0, false},
		{-3, 0, false},
	} {
		if got, ok := pageClick(tt.id, 7); got != tt.want || ok != tt.ok {
			t.Errorf("pageClick(%d) = %d, %v; want %d, %v", tt.id, got, ok, tt.want, tt.ok)
		}
	}

	if got := pageOf(0, 10, 100); got != 8 {
		t.Errorf("a page of 10 rows at the top = %d, want 8", got)
	}

	if got := pageOf(0, 1, 100); got != 1 {
		t.Errorf("a page of 1 row = %d, want 1", got)
	}
}
