package tested

import "testing"

func BenchmarkJoin(b *testing.B) {
	nums := []int{1, 2, 3, 4, 5}
	for b.Loop() {
		_ = Join(nums)
	}
}
