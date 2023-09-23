package tsunami

import "testing"

func BenchmarkCSV(b *testing.B) {
	// run function b.N times
	readCSV("in.product.csv")
}
