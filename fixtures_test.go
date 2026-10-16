// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package osmviews

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Points that land in a known grid tile of a buildTIFF raster (size 512, two
// tiles per axis). Western hemisphere: left column; northern: top row.
var (
	topLeft     = [2]float64{-90, 45}
	topRight    = [2]float64{90, 45}
	bottomLeft  = [2]float64{-90, -45}
	bottomRight = [2]float64{90, -45}
)

// defaultDate is written into the DateTime tag unless a test overrides it.
var defaultDate = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

// fixture describes a buildTIFF raster.
type fixture struct {
	compression uint16
	// tiles holds the uniform value of each grid tile, in the order top
	// left, top right, bottom left, bottom right. Tiles with an equal value
	// share one stored blob and therefore one file offset.
	tiles    [4]float32
	maxValue float32
	// date is written into the DateTime tag; the zero time omits the tag.
	date time.Time
}

// File offsets of the parts of a buildTIFF file that has a DateTime tag:
// the 20-byte DateTime string sits right after the 13-entry IFD, followed by
// the tile tables.
const (
	dateTimePos       = 8 + 2 + 13*12 + 4
	tileOffsetsPos    = dateTimePos + 20
	tileByteCountsPos = tileOffsetsPos + 16
)

// buildTIFF builds a 512 × 512 single-level GeoTIFF laid out like the real
// OSMViews file: four 256 × 256 tiles, out-of-line TileOffsets and
// TileByteCounts, 32-bit float samples, SMaxSampleValue and DateTime.
func buildTIFF(t testing.TB, f fixture) []byte {
	t.Helper()
	var blobs [][]byte
	var blobValues []uint32
	var gridToBlob [4]int
	for g, v := range f.tiles {
		bits := math.Float32bits(v)
		found := -1
		for i, b := range blobValues {
			if b == bits {
				found = i
			}
		}
		if found >= 0 {
			gridToBlob[g] = found
			continue
		}
		raw := make([]byte, tileBytes)
		for i := 0; i < tileBytes; i += 4 {
			binary.LittleEndian.PutUint32(raw[i:], bits)
		}
		blob := raw
		if f.compression == compressionDeflate {
			blob = deflate(t, raw)
		}
		blobs = append(blobs, blob)
		blobValues = append(blobValues, bits)
		gridToBlob[g] = len(blobs) - 1
	}

	hasDate := !f.date.IsZero()
	numEntries := 12
	if hasDate {
		numEntries = 13
	}
	ifdEnd := uint32(8 + 2 + numEntries*12 + 4)
	var dateBytes []byte
	if hasDate {
		dateBytes = append([]byte(f.date.Format("2006:01:02 00:00:00")), 0)
	}
	offsetsPos := ifdEnd + uint32(len(dateBytes))
	countsPos := offsetsPos + 16
	cursor := countsPos + 16
	var blobPos []uint32
	for _, b := range blobs {
		blobPos = append(blobPos, cursor)
		cursor += uint32(len(b))
	}

	var buf bytes.Buffer
	w := func(v any) {
		if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	entry := func(tag, typ uint16, count, value uint32) { w(tag); w(typ); w(count); w(value) }
	buf.WriteString("II")
	w(uint16(42))
	w(uint32(8))
	w(uint16(numEntries))
	entry(tagImageWidth, typeLong, 1, 512)
	entry(tagImageLength, typeLong, 1, 512)
	entry(tagBitsPerSample, typeShort, 1, 32)
	entry(tagCompression, typeShort, 1, uint32(f.compression))
	entry(tagSamplesPerPixel, typeShort, 1, 1)
	entry(tagPlanarConfig, typeShort, 1, 1)
	if hasDate { // TIFF tags are in ascending order; 306 belongs here
		entry(tagDateTime, typeASCII, 20, ifdEnd)
	}
	entry(tagTileWidth, typeShort, 1, 256)
	entry(tagTileLength, typeShort, 1, 256)
	entry(tagTileOffsets, typeLong, 4, offsetsPos)
	entry(tagTileByteCounts, typeLong, 4, countsPos)
	entry(tagSampleFormat, typeShort, 1, 3)
	entry(tagMaxSampleValue, typeFloat, 1, math.Float32bits(f.maxValue))
	w(uint32(0)) // no next IFD
	buf.Write(dateBytes)
	for g := range 4 {
		w(blobPos[gridToBlob[g]])
	}
	for g := range 4 {
		w(uint32(len(blobs[gridToBlob[g]])))
	}
	for _, b := range blobs {
		buf.Write(b)
	}
	if buf.Len() != int(cursor) {
		t.Fatalf("buildTIFF: wrote %d bytes, want %d", buf.Len(), cursor)
	}
	return buf.Bytes()
}

func deflate(t testing.TB, raw []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// tiles returns a fixture with DEFLATE compression, the default date and the
// given tile values and maximum.
func tiles(maxValue float32, values ...float32) fixture {
	return fixture{
		compression: compressionDeflate,
		tiles:       [4]float32(values),
		maxValue:    maxValue,
		date:        defaultDate,
	}
}

// writeTemp writes data to a file in a test-scoped temporary directory and
// returns its path.
func writeTemp(t testing.TB, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "osmviews.tiff")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// openFixture builds f, writes it to disk and opens it.
func openFixture(t testing.TB, f fixture, opts ...Option) *Raster {
	t.Helper()
	r, err := Open(writeTemp(t, buildTIFF(t, f)), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// rank calls r.Rank and fails the test on error.
func rank(t testing.TB, r *Raster, p [2]float64) float64 {
	t.Helper()
	v, err := r.Rank(p[0], p[1])
	if err != nil {
		t.Fatalf("Rank(%v, %v): %v", p[0], p[1], err)
	}
	return v
}
