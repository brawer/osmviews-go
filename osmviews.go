// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package osmviews

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"time"
)

// DataPackageURL is where the OSMViews [data package] descriptor is
// published.
//
// The descriptor is a small JSON file naming the current raster, its size in
// bytes and its SHA-256; see the package documentation for how to use it.
// This package never downloads anything itself, but exposing the URL as a
// constant means a change of hosting is a version bump here rather than a
// string to hunt down in every caller.
//
// [data package]: https://datapackage.org
const DataPackageURL = "https://osmviews.brawer.ch/data/datapackage.json"

// DefaultCacheTiles is the decoded-tile cache capacity used unless
// [WithCacheTiles] says otherwise. Each tile is a fixed 256 KiB, so this is
// about 16 MiB.
const DefaultCacheTiles = 64

// An Option configures a [Raster] in [Open] or [New].
type Option func(*config)

type config struct {
	cacheTiles int
}

// WithCacheTiles sets the decoded-tile cache capacity, in tiles. Zero (or a
// negative number) disables caching.
//
// Queries that fall in roughly the same region reuse cached tiles, so a
// larger cache helps workloads with spread-out spatial locality, at the cost
// of memory: 256 KiB per tile.
func WithCacheTiles(n int) Option {
	return func(c *config) { c.cacheTiles = max(n, 0) }
}

// A Raster is a read-only view of a downloaded OSMViews raster. It is safe
// for concurrent use by multiple goroutines.
type Raster struct {
	r      io.ReaderAt
	closer io.Closer // nil when the caller owns r
	h      *header

	mu    sync.Mutex
	cache *tileCache
}

// Open opens a downloaded OSMViews GeoTIFF from local disk.
//
// The file is read with positioned reads ([os.File.ReadAt]) rather than
// memory-mapped, so replacing it on disk while the Raster is open is harmless
// on systems where an open file keeps its old contents (such as Unix, when
// the new file is renamed into place). The Raster holds the file open until
// [Raster.Close] is called.
//
// Open returns an error wrapping [ErrFormat] if the file is not a readable
// OSMViews raster.
func Open(name string, opts ...Option) (*Raster, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	r, err := New(f, info.Size(), opts...)
	if err != nil {
		f.Close()
		return nil, err
	}
	r.closer = f
	return r, nil
}

// New returns a Raster that reads an OSMViews GeoTIFF of the given size, in
// bytes, from r. The caller remains responsible for r: [Raster.Close] does
// not close it, and its contents must not change while the Raster is in use.
//
// New reads the file header and the tile index (about 8 MB for the published
// raster) into memory; tiles are read from r on demand.
//
// New returns an error wrapping [ErrFormat] if r does not hold a readable
// OSMViews raster.
func New(r io.ReaderAt, size int64, opts ...Option) (*Raster, error) {
	cfg := config{cacheTiles: DefaultCacheTiles}
	for _, opt := range opts {
		opt(&cfg)
	}
	h, err := parseHeader(r, size)
	if err != nil {
		return nil, err
	}
	return &Raster{r: r, h: h, cache: newTileCache(cfg.cacheTiles)}, nil
}

// Close releases the file opened by [Open]. For a Raster made with [New], it
// does nothing. Rank calls made after Close may fail.
func (r *Raster) Close() error {
	if r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

// Rank reports how much the location at lng/lat (WGS84 degrees, longitude
// first, as in GeoJSON) is looked at on OpenStreetMap-based maps.
//
// The result runs from 0 (effectively never) to 1 (among the most-viewed
// places on the planet), derived from a year of OpenStreetMap tile-access
// logs. Locations near the poles, beyond the map's coverage, and non-finite
// inputs rank 0. Longitude wraps, so 181 and -179 are the same place.
//
// An error means the tile holding the location could not be read: an I/O
// error, or a corrupt tile (wrapping [ErrFormat]). The rank is then 0.
func (r *Raster) Rank(lng, lat float64) (float64, error) {
	h := r.h
	x, y, ok := project(lng, lat, h.size)
	if !ok {
		r.mu.Lock()
		r.cache.recordOutOfRange()
		r.mu.Unlock()
		return 0, nil
	}

	grid := (y>>8)*h.tilesAcross + (x >> 8)
	pixel := (y&255)<<8 | (x & 255)
	offset := h.tileOffsets[grid]

	r.mu.Lock()
	value, hit := r.cache.lookup(offset, pixel)
	r.mu.Unlock()
	if hit {
		return r.scale(value), nil
	}

	// Decode the missed tile outside the lock, so concurrent readers don't
	// serialize on slow work. The worst case is two goroutines briefly
	// decoding the same tile and producing identical results.
	started := time.Now()
	t, err := r.decode(offset, h.tileByteCounts[grid])
	if err != nil {
		return 0, err
	}
	elapsed := time.Since(started)
	value = t[pixel]
	r.mu.Lock()
	r.cache.insert(offset, t, elapsed)
	r.mu.Unlock()
	return r.scale(value), nil
}

// Date returns the last day of OpenStreetMap tile-log data painted into the
// raster, at midnight UTC. The published file is regenerated weekly, so this
// tells which week's views a caller is looking at. It matches the "version"
// of the data package descriptor.
func (r *Raster) Date() time.Time {
	return r.h.date
}

// Metrics returns a snapshot of internal counters, meant to be logged once
// at the end of a long-running job.
func (r *Raster) Metrics() Metrics {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cache.metrics()
}

func (r *Raster) scale(v float32) float64 {
	maxValue := r.h.maxValue
	value := float64(v)
	if math.IsNaN(value) || math.IsInf(value, 0) || !(maxValue > 0) {
		return 0
	}
	return min(max(value/maxValue, 0), 1)
}

// scratch holds the buffers for decoding one tile, recycled across calls.
type scratch struct {
	raw []byte // the stored blob, at most maxTileBlob bytes
	out []byte // the decompressed tile, exactly tileBytes
	zr  io.ReadCloser
	br  bytes.Reader
}

var scratchPool = sync.Pool{
	New: func() any {
		return &scratch{
			raw: make([]byte, 0, maxTileBlob),
			out: make([]byte, tileBytes),
		}
	},
}

// decode reads and decompresses the tile stored at offset. The stored size
// was bounded when the file was opened, and decompression stops after one
// tile's worth of output, so a crafted tile cannot drive allocations here
// past about one tile, whatever its compression ratio.
func (r *Raster) decode(offset, count uint32) (tile, error) {
	s := scratchPool.Get().(*scratch)
	defer scratchPool.Put(s)

	raw := s.raw[:count]
	if n, err := r.r.ReadAt(raw, int64(offset)); n < len(raw) {
		if err == nil || errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("osmviews: reading tile at offset %d: %w", offset, err)
	}

	data := raw
	if r.h.compression == compressionDeflate {
		s.br.Reset(raw)
		if err := s.resetZlib(); err != nil {
			return nil, corruptTile(offset)
		}
		// io.ReadFull stops after one tile; the extra one-byte read must then
		// hit the end of the stream, which is also where zlib verifies the
		// stream's checksum.
		if _, err := io.ReadFull(s.zr, s.out); err != nil {
			return nil, corruptTile(offset)
		}
		var extra [1]byte
		if n, err := io.ReadFull(s.zr, extra[:]); n != 0 || err != io.EOF {
			return nil, corruptTile(offset)
		}
		data = s.out
	} else if len(raw) != tileBytes {
		return nil, corruptTile(offset)
	}

	t := make(tile, tilePixels)
	for i := range t {
		t[i] = math.Float32frombits(le.Uint32(data[4*i:]))
	}
	return t, nil
}

func (s *scratch) resetZlib() error {
	if s.zr == nil {
		zr, err := zlib.NewReader(&s.br)
		if err != nil {
			return err
		}
		s.zr = zr
		return nil
	}
	return s.zr.(zlib.Resetter).Reset(&s.br, nil)
}

func corruptTile(offset uint32) error {
	return fmt.Errorf("osmviews: %w: corrupt tile at offset %d", ErrFormat, offset)
}
