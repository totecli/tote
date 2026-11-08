package plugin

import "testing"

// BenchmarkDecodeGft measures the reading of the largest example.
//
// Parameters:
//   - b: benchmark to report to
func BenchmarkDecodeGft(b *testing.B) {
	data := readFixture(b, "gft/plugin.toml")

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		Decode(data, DecodeOptions{Host: testHost(), Official: true})
	}
}
