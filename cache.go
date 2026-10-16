// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package osmviews

import (
	"container/list"
	"log/slog"
	"time"
)

// Metrics holds diagnostic counters from a [Raster], meant to be logged once
// at the end of a long-running job.
//
// Every field except the TilesCached and CacheCapacity gauges counts
// monotonically since the Raster was opened. The cache's memory footprint is
// TilesCached × 256 KiB.
type Metrics struct {
	// Queries is the total number of calls to Rank.
	Queries uint64

	// OutOfRange counts the queries whose coordinates fell outside the
	// covered area, and so returned 0.
	OutOfRange uint64

	// CacheHits counts tile lookups that were served from the cache.
	CacheHits uint64

	// CacheMisses counts tile lookups that missed and triggered a decode.
	CacheMisses uint64

	// TilesDecoded counts the tiles actually read and decompressed. It can
	// exceed CacheMisses only when goroutines race to decode the same tile.
	TilesDecoded uint64

	// CacheEvictions counts the cache entries dropped to make room. A large
	// value relative to CacheCapacity means the cache is too small for the
	// workload.
	CacheEvictions uint64

	// TilesCached is the number of tiles currently held in the cache.
	TilesCached int

	// CacheCapacity is the configured cache capacity, in tiles.
	CacheCapacity int

	// DecodeTime is the cumulative wall-clock time spent reading and
	// decompressing tiles.
	DecodeTime time.Duration
}

// HitRate returns the fraction of tile lookups served from the cache, or 0
// before the first lookup.
func (m Metrics) HitRate() float64 {
	lookups := m.CacheHits + m.CacheMisses
	if lookups == 0 {
		return 0
	}
	return float64(m.CacheHits) / float64(lookups)
}

// LogValue implements [slog.LogValuer], so that
//
//	slog.Info("done", "osmviews", r.Metrics())
//
// logs the counters as a group.
func (m Metrics) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Uint64("queries", m.Queries),
		slog.Uint64("out_of_range", m.OutOfRange),
		slog.Uint64("cache_hits", m.CacheHits),
		slog.Uint64("cache_misses", m.CacheMisses),
		slog.Float64("hit_rate", m.HitRate()),
		slog.Uint64("tiles_decoded", m.TilesDecoded),
		slog.Uint64("cache_evictions", m.CacheEvictions),
		slog.Int("tiles_cached", m.TilesCached),
		slog.Int("cache_capacity", m.CacheCapacity),
		slog.Duration("decode_time", m.DecodeTime),
	)
}

// tile is a decoded tile: 256 × 256 samples in row-major order.
type tile []float32

type cacheEntry struct {
	offset uint32
	data   tile
}

// tileCache is a least-recently-used cache of decoded tiles, plus the
// diagnostic counters. It is not safe for concurrent use; Raster guards it
// with a mutex that every query takes anyway, so the counters need no
// synchronization of their own.
//
// The cache is keyed by the tile's byte offset in the file, not by its grid
// position. The raster is sparse: large uniform areas (oceans, deserts) are
// stored once and referenced from many grid positions, and keying by offset
// collapses all of them onto a single cache entry.
type tileCache struct {
	capacity int
	entries  map[uint32]*list.Element // values are *cacheEntry
	lru      list.List                // front is most recently used
	m        Metrics
}

func newTileCache(capacity int) *tileCache {
	c := &tileCache{capacity: capacity, entries: make(map[uint32]*list.Element)}
	c.m.CacheCapacity = capacity
	return c
}

// recordOutOfRange records a query whose coordinates fell outside the
// covered area.
func (c *tileCache) recordOutOfRange() {
	c.m.Queries++
	c.m.OutOfRange++
}

// lookup records a query and returns the requested pixel if its tile is
// cached.
func (c *tileCache) lookup(offset uint32, pixel int) (float32, bool) {
	c.m.Queries++
	el, ok := c.entries[offset]
	if !ok {
		c.m.CacheMisses++
		return 0, false
	}
	c.m.CacheHits++
	c.lru.MoveToFront(el)
	return el.Value.(*cacheEntry).data[pixel], true
}

// insert adds a freshly decoded tile, evicting the least recently used
// entry if the cache is full.
func (c *tileCache) insert(offset uint32, data tile, decodeTime time.Duration) {
	c.m.TilesDecoded++
	c.m.DecodeTime += decodeTime
	if c.capacity <= 0 {
		return
	}
	if el, ok := c.entries[offset]; ok {
		// Another goroutine decoded the same tile concurrently.
		c.lru.MoveToFront(el)
		return
	}
	if len(c.entries) >= c.capacity {
		victim := c.lru.Back()
		c.lru.Remove(victim)
		delete(c.entries, victim.Value.(*cacheEntry).offset)
		c.m.CacheEvictions++
	}
	c.entries[offset] = c.lru.PushFront(&cacheEntry{offset: offset, data: data})
}

func (c *tileCache) metrics() Metrics {
	m := c.m
	m.TilesCached = len(c.entries)
	return m
}
