package plugin

import (
	"context"
	"strings"
	"testing"
)

// TestReviewCheckBundledPins covers the check of a bundled plugin that is not installed, which no
// test reached (checkBundled and pinFindings were at 0 percent): a bundled copy that is what its
// pin says gives no finding, and a version, a tree or a surface that drifted from the pin is an
// error that fails the check, named by field.
//
// Parameters:
//   - t: test to report to
func TestReviewCheckBundledPins(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")

	files := map[string]string{"plugin.toml": manifest("demo", "1.0.0", []string{"exec:tool"},
		cmdWith("go", ""))}
	good := bundle(t, l, files)

	zero := strings.Repeat("0", 64)

	tests := []struct {
		name   string
		change func(b *fakeBundle)
		field  string
	}{
		{"intact", func(*fakeBundle) {}, ""},
		{"version drifted", func(b *fakeBundle) { b.version = "0.9.0" }, "version"},
		{"tree drifted", func(b *fakeBundle) { b.tree = zero }, "tree_sha256"},
		{"surface drifted", func(b *fakeBundle) { b.surface = zero }, "surface_sha256"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := *good
			tt.change(&b)

			r := l.reg(func(o *Options) { o.Bundled = &b })

			res, err := r.Check(context.Background(), CheckRequest{Target: "demo", Strict: true})
			if err != nil {
				t.Fatal(err)
			}

			if res.Kind != TargetBundled || res.ID != "demo" || res.TreeSHA256 == "" {
				t.Errorf("result %+v", res)
			}

			var pins []Finding

			for _, f := range res.Findings {
				if f.Code == CodePinMismatch {
					pins = append(pins, f)
				}
			}

			switch {
			case tt.field == "" && (len(pins) != 0 || res.Failed()):
				t.Errorf("an intact bundle fails: %+v", res.Findings)
			case tt.field != "" && (len(pins) != 1 || pins[0].Path != tt.field || !res.Failed()):
				t.Errorf("pin findings %+v failed %v, want one on %s", pins, res.Failed(), tt.field)
			}
		})
	}
}
