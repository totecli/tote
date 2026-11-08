package plugin

import "testing"

// TestReviewMissingProgramsAreListed pins the fields that tell the user which programs are
// absent: Plugin.Missing for a required one and Plugin.Absent for an optional one. The state and
// the reasons were tested, the lists the views render were not.
//
// Parameters:
//   - t: test to report to
func TestReviewMissingProgramsAreListed(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")
	l.fakeBin("present")
	l.install("demo", demoFiles("demo", "\n[[requires]]\nbin = \"absent\"\n"+
		"\n[[requires]]\nbin = \"nice\"\noptional = true\n"+
		"\n[[requires]]\nbin = \"present\"\n"))

	p, ok := l.registry(nil).Recheck("demo")
	if !ok {
		t.Fatal("plugin not found")
	}

	if p.State != StateMissingDependency {
		t.Fatalf("state %s reasons %v", p.State, p.Reasons)
	}

	if len(p.Missing) != 1 || p.Missing[0].Bin != "absent" {
		t.Errorf("Missing = %+v, want only absent", p.Missing)
	}

	if len(p.Absent) != 1 || p.Absent[0].Bin != "nice" {
		t.Errorf("Absent = %+v, want only nice", p.Absent)
	}

	if !hasReason(p.Warnings, ReasonOptionalDepAbsent) {
		t.Errorf("no optional_missing warning: %v", p.Warnings)
	}
}
