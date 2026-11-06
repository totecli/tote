package ui

import (
	"strconv"
	"strings"
	"time"
)

// bestMean measures what one run of a function costs, in a way that a busy machine does not
// spoil: the function is run in several short samples and the lowest mean of a sample is kept.
// A pause of the scheduler or a neighbor taking the processor only makes the samples it hits
// longer, so the cleanest sample is the closest to the cost of the code itself.
//
// Parameters:
//   - samples: number of samples, at least 1
//   - runs: number of runs in each sample, at least 1; short enough that a sample can be left
//     alone by the scheduler
//   - fn: the code to measure
//
// Returns the lowest mean duration of one run over the samples.
func bestMean(samples, runs int, fn func()) time.Duration {
	best := time.Duration(-1)

	for range samples {
		start := time.Now()

		for range runs {
			fn()
		}

		if mean := time.Since(start) / time.Duration(runs); best < 0 || mean < best {
			best = mean
		}
	}

	return best
}

// referenceWork is a fixed piece of work that does not use the code of the interface: it builds,
// cuts and joins strings, which is what drawing a frame mostly does. The cost of the code under
// test, measured in units of this work taken at the same time, does not depend on the speed of
// the machine or on how loaded it is, so that a limit can be set in those units.
func referenceWork() {
	var b strings.Builder

	for i := range 400 {
		b.WriteString(strconv.Itoa(i))
		b.WriteString(strings.Repeat("x", i%9))
		b.WriteByte(' ')
	}

	words := strings.Fields(b.String())

	if len(strings.Join(words, "-")) == 0 {
		panic("referenceWork built nothing")
	}
}
