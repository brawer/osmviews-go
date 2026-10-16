// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package osmviews

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestDataPackageURL(t *testing.T) {
	if !strings.HasPrefix(DataPackageURL, "https://") || !strings.HasSuffix(DataPackageURL, "/datapackage.json") {
		t.Errorf("DataPackageURL = %q", DataPackageURL)
	}
}

func TestRanksScaleAgainstThePlanetaryMaximum(t *testing.T) {
	r := openFixture(t, tiles(10, 3, 0, 10, 10))
	for _, tc := range []struct {
		name string
		p    [2]float64
		want float64
	}{
		{"top left", topLeft, 0.3},
		{"top right", topRight, 0},
		{"bottom left", bottomLeft, 1},
		{"Null Island, in the bottom-right tile", [2]float64{0, 0}, 1},
		{"beyond the Web Mercator limit", [2]float64{0, 89}, 0},
	} {
		if got := rank(t, r, tc.p); !approx(got, tc.want) {
			t.Errorf("%s: Rank = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRanksClampToTheUnitInterval(t *testing.T) {
	// The declared maximum is 10, but one tile is well above it and one is
	// below zero.
	r := openFixture(t, tiles(10, 25, -4, 10, 6))
	for _, tc := range []struct {
		p    [2]float64
		want float64
	}{
		{topLeft, 1}, {topRight, 0}, {bottomLeft, 1}, {bottomRight, 0.6},
	} {
		if got := rank(t, r, tc.p); !approx(got, tc.want) {
			t.Errorf("Rank(%v) = %v, want %v", tc.p, got, tc.want)
		}
	}
}

func TestNonFiniteSamplesRankZero(t *testing.T) {
	r := openFixture(t, tiles(10, float32(math.NaN()), float32(math.Inf(1)), 5, 5))
	if got := rank(t, r, topLeft); got != 0 {
		t.Errorf("NaN sample: Rank = %v, want 0", got)
	}
	if got := rank(t, r, topRight); got != 0 {
		t.Errorf("+Inf sample: Rank = %v, want 0", got)
	}
}

func TestSharedTilesUseOneCacheEntry(t *testing.T) {
	// The bottom-left and bottom-right tiles have the same value, so they
	// share one blob.
	r := openFixture(t, tiles(10, 3, 0, 7, 7))
	rank(t, r, bottomLeft)
	rank(t, r, bottomRight)
	m := r.Metrics()
	if m.TilesCached != 1 || m.TilesDecoded != 1 || m.CacheMisses != 1 || m.CacheHits != 1 {
		t.Errorf("Metrics = %+v; want 1 cached, 1 decoded, 1 miss, 1 hit", m)
	}
}

func TestUncompressedTiles(t *testing.T) {
	f := tiles(10, 5, 5, 5, 5)
	f.compression = compressionNone
	r := openFixture(t, f)
	if got := rank(t, r, topLeft); !approx(got, 0.5) {
		t.Errorf("Rank = %v, want 0.5", got)
	}
}

func TestMetricsTrackQueriesEvictionsAndHitRate(t *testing.T) {
	r := openFixture(t, tiles(10, 1, 2, 3, 4), WithCacheTiles(2))
	for _, p := range [][2]float64{topLeft, topRight, bottomLeft, bottomRight, {0, 89}} {
		rank(t, r, p)
	}
	m := r.Metrics()
	want := Metrics{
		Queries: 5, OutOfRange: 1, CacheMisses: 4, TilesDecoded: 4,
		CacheEvictions: 2, TilesCached: 2, CacheCapacity: 2,
		DecodeTime: m.DecodeTime,
	}
	if m != want {
		t.Errorf("Metrics = %+v, want %+v", m, want)
	}
	if m.DecodeTime <= 0 {
		t.Errorf("DecodeTime = %v, want > 0", m.DecodeTime)
	}
	if m.HitRate() != 0 {
		t.Errorf("HitRate = %v, want 0", m.HitRate())
	}
	rank(t, r, bottomRight)
	if got := r.Metrics().HitRate(); !approx(got, 0.2) {
		t.Errorf("HitRate after one hit = %v, want 0.2", got)
	}
}

func TestCacheEvictsTheLeastRecentlyUsedTile(t *testing.T) {
	r := openFixture(t, tiles(10, 1, 2, 3, 4), WithCacheTiles(2))
	rank(t, r, topLeft)
	rank(t, r, topRight)
	rank(t, r, topLeft)    // top right is now least recently used
	rank(t, r, bottomLeft) // evicts top right
	rank(t, r, topLeft)    // hit
	rank(t, r, topRight)   // miss
	if m := r.Metrics(); m.CacheHits != 2 || m.CacheMisses != 4 {
		t.Errorf("Metrics = %+v; want 2 hits, 4 misses", m)
	}
}

func TestDisabledCacheStillAnswersAndNeverStores(t *testing.T) {
	r := openFixture(t, tiles(10, 3, 0, 7, 7), WithCacheTiles(0))
	for range 2 {
		if got := rank(t, r, topLeft); !approx(got, 0.3) {
			t.Errorf("Rank = %v, want 0.3", got)
		}
	}
	if m := r.Metrics(); m.TilesCached != 0 || m.TilesDecoded != 2 || m.CacheHits != 0 {
		t.Errorf("Metrics = %+v; want 0 cached, 2 decoded, 0 hits", m)
	}
}

func TestDate(t *testing.T) {
	f := tiles(10, 1, 1, 1, 1)
	f.date = time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	r := openFixture(t, f)
	if got := r.Date(); !got.Equal(f.date) || got.Location() != time.UTC {
		t.Errorf("Date = %v, want %v", got, f.date)
	}
}

func TestNewReadsFromAnyReaderAt(t *testing.T) {
	data := buildTIFF(t, tiles(10, 3, 0, 10, 10))
	r, err := New(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if got := rank(t, r, topLeft); !approx(got, 0.3) {
		t.Errorf("Rank = %v, want 0.3", got)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
}

func TestOpenMissingFile(t *testing.T) {
	_, err := Open("no/such/osmviews.tiff")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open = %v, want fs.ErrNotExist", err)
	}
}

func TestRankAfterCloseFails(t *testing.T) {
	r, err := Open(writeTemp(t, buildTIFF(t, tiles(10, 1, 2, 3, 4))))
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if _, err := r.Rank(topLeft[0], topLeft[1]); err == nil {
		t.Error("Rank after Close succeeded, want an error")
	}
}

// corrupt returns a copy of a valid fixture file with edit applied.
func corrupt(t *testing.T, edit func(b []byte) []byte) []byte {
	t.Helper()
	b := bytes.Clone(buildTIFF(t, tiles(10, 1, 2, 3, 4)))
	return edit(b)
}

func TestRejectsMalformedFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		msg  string
	}{
		{"empty", nil, "too short"},
		{"text", []byte("Hello, world!\n"), "not a TIFF"},
		{"big-endian", corrupt(t, func(b []byte) []byte { copy(b, "MM"); return b }), "big-endian"},
		{"BigTIFF", corrupt(t, func(b []byte) []byte { b[2] = 43; return b }), "BigTIFF"},
		{"truncated", corrupt(t, func(b []byte) []byte { return b[:len(b)/2] }), "past end of file"},
		{"truncated IFD", corrupt(t, func(b []byte) []byte { return b[:40] }), "IFD"},
		{"no DateTime", buildTIFF(t, fixture{compressionDeflate, [4]float32{1, 1, 1, 1}, 10, time.Time{}}), "missing DateTime"},
		{"malformed DateTime", corrupt(t, func(b []byte) []byte {
			copy(b[dateTimePos:], "not a timestamp\x00\x00\x00\x00\x00")
			return b
		}), "malformed DateTime"},
		{"tile offset past end of file", corrupt(t, func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[tileOffsetsPos:], 0xFFFFFF00)
			return b
		}), "past end of file"},
		{"oversized tile blob", corrupt(t, func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[tileByteCountsPos:], maxTileBlob+1)
			return b
		}), "implausibly large"},
		{"zero-length tile", corrupt(t, func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[tileByteCountsPos:], 0)
			return b
		}), "invalid tile entry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Open(writeTemp(t, tc.data))
			if !errors.Is(err, ErrFormat) {
				t.Fatalf("Open = %v, want an error wrapping ErrFormat", err)
			}
			if !strings.Contains(err.Error(), tc.msg) {
				t.Errorf("Open = %q, want it to mention %q", err, tc.msg)
			}
		})
	}
}

func TestZipBombTileIsRejectedWithoutInflating(t *testing.T) {
	// A small blob that would inflate far past one tile. The decoder must
	// stop after one tile's worth of output and report the tile as corrupt.
	data := buildTIFF(t, tiles(10, 1, 1, 1, 1))
	bomb := deflate(t, make([]byte, 64<<20))
	if len(bomb) > maxTileBlob {
		t.Fatalf("bomb is %d bytes, too large for the test", len(bomb))
	}
	off := uint32(len(data))
	data = append(data, bomb...)
	binary.LittleEndian.PutUint32(data[tileOffsetsPos:], off)
	binary.LittleEndian.PutUint32(data[tileByteCountsPos:], uint32(len(bomb)))
	r, err := New(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Rank(topLeft[0], topLeft[1])
	if !errors.Is(err, ErrFormat) || got != 0 {
		t.Errorf("Rank = %v, %v; want 0 and an error wrapping ErrFormat", got, err)
	}
	// The other tiles are unaffected.
	if got := rank(t, r, bottomRight); !approx(got, 0.1) {
		t.Errorf("Rank(bottom right) = %v, want 0.1", got)
	}
}

func TestTruncatedTileStreamIsCorrupt(t *testing.T) {
	data := buildTIFF(t, tiles(10, 1, 1, 1, 1))
	off := binary.LittleEndian.Uint32(data[tileOffsetsPos:])
	count := binary.LittleEndian.Uint32(data[tileByteCountsPos:])
	data[off+count-1] ^= 0xFF // break the zlib checksum
	r, err := New(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Rank(topLeft[0], topLeft[1]); !errors.Is(err, ErrFormat) {
		t.Errorf("Rank = %v, want an error wrapping ErrFormat", err)
	}
}

// failingReaderAt serves the header but fails every read past it.
type failingReaderAt struct {
	data  []byte
	limit int64
}

var errDisk = errors.New("disk on fire")

func (f failingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > f.limit {
		return 0, errDisk
	}
	return bytes.NewReader(f.data).ReadAt(p, off)
}

func TestRankReportsIOErrors(t *testing.T) {
	data := buildTIFF(t, tiles(10, 1, 2, 3, 4))
	r, err := New(failingReaderAt{data, tileByteCountsPos + 16}, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Rank(topLeft[0], topLeft[1]); !errors.Is(err, errDisk) {
		t.Errorf("Rank = %v, want an error wrapping %v", err, errDisk)
	}
}

func TestSafeForConcurrentUse(t *testing.T) {
	r := openFixture(t, tiles(10, 1, 2, 3, 4), WithCacheTiles(2))
	const goroutines, queries = 8, 2000
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range queries {
				lng := -90 + float64(g)*20 + float64(i%5)*0.01
				lat := 45.0
				if i%2 == 1 {
					lat = -45
				}
				if _, err := r.Rank(lng, lat); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	m := r.Metrics()
	if m.Queries != goroutines*queries || m.OutOfRange != 0 {
		t.Errorf("Metrics = %+v; want %d queries, none out of range", m, goroutines*queries)
	}
	if m.CacheHits+m.CacheMisses != m.Queries || m.TilesDecoded < m.CacheMisses {
		t.Errorf("Metrics = %+v: inconsistent counters", m)
	}
}

func FuzzNew(f *testing.F) {
	f.Add(buildTIFF(f, tiles(10, 1, 2, 3, 4)))
	raw := tiles(10, 1, 2, 3, 3)
	raw.compression = compressionNone
	f.Add(buildTIFF(f, raw))
	if pipeline, err := os.ReadFile("testdata/zurich-2021-W47.tiff"); err == nil {
		f.Add(pipeline)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := New(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			if !errors.Is(err, ErrFormat) {
				t.Fatalf("New = %v, want nil or an error wrapping ErrFormat", err)
			}
			return
		}
		for _, p := range [][2]float64{topLeft, topRight, bottomLeft, bottomRight} {
			v, err := r.Rank(p[0], p[1])
			if err != nil && !errors.Is(err, ErrFormat) {
				t.Fatalf("Rank = %v", err)
			}
			if v < 0 || v > 1 || math.IsNaN(v) {
				t.Fatalf("Rank = %v, outside 0..1", v)
			}
		}
	})
}

var _ io.Closer = (*Raster)(nil)

// TestPipelineOutput reads a raster written by the OSMViews pipeline itself;
// see testdata/README.md. The expected values match the Python client's.
func TestPipelineOutput(t *testing.T) {
	r, err := Open("testdata/zurich-2021-W47.tiff")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, tc := range []struct {
		name     string
		lng, lat float64
		want     float64
	}{
		{"Zürich main station", 8.5402, 47.3782, 1},
		{"Altstetten", 8.4889, 47.3915, 0.8873},
		{"Uetliberg", 8.4914, 47.3497, 0.7840},
		{"Sahara", 13, 23, 0},
	} {
		got := rank(t, r, [2]float64{tc.lng, tc.lat})
		if math.Abs(got-tc.want) > 5e-5 {
			t.Errorf("%s: Rank = %.4f, want %.4f", tc.name, got, tc.want)
		}
	}
	if want := time.Date(2021, 11, 28, 0, 0, 0, 0, time.UTC); !r.Date().Equal(want) {
		t.Errorf("Date = %v, want %v", r.Date(), want)
	}
	if m := r.Metrics(); m.TilesDecoded != 2 {
		t.Errorf("TilesDecoded = %d, want 2 (Zürich, and the shared empty tile)", m.TilesDecoded)
	}
}
