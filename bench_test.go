// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package osmviews

import "testing"

// BenchmarkRankCached sweeps within one already-cached tile.
func BenchmarkRankCached(b *testing.B) {
	r := openFixture(b, tiles(10, 3, 0, 7, 7))
	rank(b, r, topLeft) // warm the tile
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if _, err := r.Rank(-120+float64(i%60), 45); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if m := r.Metrics(); m.HitRate() < 0.99 {
		b.Errorf("hit rate %v, want > 0.99", m.HitRate())
	}
}

// BenchmarkRankCachedParallel is BenchmarkRankCached from many goroutines,
// to show the cost of the shared lock.
func BenchmarkRankCachedParallel(b *testing.B) {
	r := openFixture(b, tiles(10, 3, 0, 7, 7))
	rank(b, r, topLeft)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for i := 0; pb.Next(); i++ {
			if _, err := r.Rank(-120+float64(i%60), 45); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

// BenchmarkRankUncached reads and inflates a tile on every call.
func BenchmarkRankUncached(b *testing.B) {
	r := openFixture(b, tiles(10, 3, 0, 7, 7), WithCacheTiles(0))
	for b.Loop() {
		if _, err := r.Rank(-90, 45); err != nil {
			b.Fatal(err)
		}
	}
}
