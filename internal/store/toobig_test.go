package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteRefusesWhatReadRefuses checks that an add or a replace that would push the file past
// the read cap fails with ErrTooBig, leaves the file as it was, and keeps the collection usable.
//
// Parameters:
//   - t: test to report to
func TestWriteRefusesWhatReadRefuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recs.toml")
	c := engOpen(t, path)
	chunk := strings.Repeat("d", 512<<10)

	var added int
	for ; added < 16; added++ {
		err := c.Add(testRec{Name: "r" + string(rune('a'+added)), Desc: chunk})
		if err != nil {
			if !errors.Is(err, ErrTooBig) {
				t.Fatalf("add %d: %v, want ErrTooBig", added, err)
			}

			break
		}
	}

	if added == 0 || added == 16 {
		t.Fatalf("the cap was reached after %d adds", added)
	}

	before, err := os.ReadFile(path)
	if err != nil || len(before) > maxFileSize {
		t.Fatalf("file after the refusal: %d bytes, %v", len(before), err)
	}

	err = c.Add(testRec{Name: "zz", Desc: chunk})
	if !errors.Is(err, ErrTooBig) || !strings.Contains(err.Error(), "recs.toml") {
		t.Errorf("second add = %v, want ErrTooBig naming the file", err)
	}

	err = c.Replace("ra", testRec{Name: "ra", Desc: chunk + chunk + chunk})
	if !errors.Is(err, ErrTooBig) {
		t.Errorf("replace = %v, want ErrTooBig", err)
	}

	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Error("a refused write changed the file")
	}
	engNoTemp(t, filepath.Dir(path))

	fresh := engOpen(t, path)
	if len(fresh.List()) != added {
		t.Errorf("reloaded %d records, want %d", len(fresh.List()), added)
	}

	if err := fresh.Delete("ra"); err != nil {
		t.Fatalf("delete after the refusal: %v", err)
	}

	if err := fresh.Add(testRec{Name: "small", Desc: "x"}); err != nil {
		t.Errorf("small add after a delete: %v", err)
	}
}

// TestPatchTableRefusesTooBig checks that the table patcher applies the same cap as a collection.
//
// Parameters:
//   - t: test to report to
func TestPatchTableRefusesTooBig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.toml")
	tblWrite(t, path, "schema = 1\n")

	err := PatchTable(path, "", map[string]any{"big": strings.Repeat("x", maxFileSize)}, nil)
	if !errors.Is(err, ErrTooBig) {
		t.Fatalf("PatchTable = %v, want ErrTooBig", err)
	}

	if got := tblRead(t, path); got != "schema = 1\n" {
		t.Errorf("file changed: %q", got)
	}
}
