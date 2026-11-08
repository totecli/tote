package plugin

import (
	"os"
	"sort"
	"testing"
	"time"
)

// startupBudget is the time Load may take for the three bundled plugins, from section 5.2 of the
// specification.
const startupBudget = 20 * time.Millisecond

// budgetRuns is how many cold loads are timed; the fastest is judged, so that one preemption of
// the test process by another does not fail the test.
const budgetRuns = 7

// TestStartupBudget measures a cold Load of three plugins, the two largest examples of the
// specification (gft, declarative, and ghrel, exec) and a small one, each with a fresh registry so
// that the cache cannot help. The budget is 20 ms.
//
// A timing test is only meaningful on a quiet machine. It is skipped under the race detector,
// which makes the code several times slower, and when TOTE_SLOW_TESTS or CI is set, which the
// shared runners and a loaded workstation can set; the allocation count of the same load is
// asserted in TestStartupWork, which does not depend on the clock.
//
// Parameters:
//   - t: test to report to
func TestStartupBudget(t *testing.T) {
	if raceEnabled {
		t.Skip("timings are not meaningful under the race detector")
	}

	if os.Getenv("TOTE_SLOW_TESTS") != "" || os.Getenv("CI") != "" {
		t.Skip("timing test skipped on a slow or shared host")
	}

	l := budgetLab(t)

	var runs []time.Duration

	for range budgetRuns {
		reg := l.registry(func(o *Options) { o.Host = budgetHost() })

		start := time.Now()
		snap := reg.Load()
		runs = append(runs, time.Since(start))

		if got := len(snap.Plugins()); got != 3 {
			t.Fatalf("snapshot has %d plugins", got)
		}
	}

	sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })

	t.Logf("cold Load of 3 plugins: fastest %v, median %v, slowest %v", runs[0],
		runs[len(runs)/2], runs[len(runs)-1])

	if runs[0] > startupBudget {
		t.Errorf("fastest cold Load = %v, budget %v", runs[0], startupBudget)
	}
}

// budgetHost is the host of the budget test: a development build on linux/amd64, which every
// range admits, with the features of 1.0.
func budgetHost() Host {
	h := NewHost("dev", "tote-test")
	h.OS, h.Arch = "linux", "amd64"

	return h
}

// budgetLab installs gft, ghrel and a small plugin as the bundled ones would be: consented trees
// with their locale files.
//
// Parameters:
//   - t: test to report to
func budgetLab(t *testing.T) *lab {
	t.Helper()

	l := newLab(t)

	gft := map[string]string{
		"plugin.toml":     string(readFixture(t, "gft/plugin.toml")),
		"locales/en.toml": string(readFixture(t, "gft/locales/en.toml")),
		"locales/fr.toml": string(readFixture(t, "gft/locales/fr.toml")),
	}
	ghrel := map[string]string{
		"plugin.toml": string(readFixture(t, "ghrel/plugin.toml")),
		"bin/ghrel*":  "#!/bin/sh\n",
	}

	installWith(t, l, "gft", gft)
	installWith(t, l, "ghrel", ghrel)
	installWith(t, l, "demo", demoFiles("demo", ""))

	return l
}

// installWith installs a plugin whose manifest is read with the development host.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
//   - id: plugin id
//   - files: files of the package
func installWith(t *testing.T, l *lab, id string, files map[string]string) {
	t.Helper()

	m, rep := Decode([]byte(files["plugin.toml"]), DecodeOptions{
		Host: budgetHost(), WantID: id,
		Official: true,
	})
	if m == nil {
		t.Fatalf("%s refused: %v", id, rep.Issues)
	}

	installRaw(t, l, id, files)

	must(t, l.lock().Update(id, func(r *LockRecord) {
		r.Version, r.Tier, r.Caps, r.SurfaceSHA256 = m.Version, m.Tier, EffectiveCaps(m),
			SurfaceSHA256(m)
	}))
}

// TestStartupWork checks, without a clock, that the three plugins load to the states the budget
// test assumes, and that a warm Load does not hash again.
//
// Parameters:
//   - t: test to report to
func TestStartupWork(t *testing.T) {
	l := budgetLab(t)
	reg := l.registry(func(o *Options) { o.Host = budgetHost() })

	snap := reg.Load()

	for _, id := range []string{"gft", "ghrel", "demo"} {
		p, ok := snap.Get(id)
		if !ok || p.Manifest == nil {
			t.Fatalf("%s: %+v", id, p)
		}

		// gft and ghrel need programs that the sandbox does not have; demo is whole.
		want := StateMissingDependency
		if id == "demo" {
			want = StateEnabled
		}

		if p.State != want {
			t.Errorf("%s: state %s (%v), want %s", id, p.State, reasonCodes(p.Reasons), want)
		}
	}

	if p, _ := snap.Get("gft"); p.Catalog.Text("fr", "description", "") == "" {
		t.Error("the French locale of gft was not read")
	}

	allocs := testing.AllocsPerRun(5, func() { reg.Load() })

	cold := testing.AllocsPerRun(5, func() {
		l.registry(func(o *Options) { o.Host = budgetHost() }).Load()
	})
	if allocs >= cold {
		t.Errorf("a warm Load allocates %v, a cold one %v: the cache does not help", allocs, cold)
	}
}
