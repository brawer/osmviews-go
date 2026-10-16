// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package osmviews

// A deliberately tiny, read-only parser for the one TIFF layout that the
// OSMViews pipeline produces. It only looks at IFD 0 (the full-resolution
// level; the file also carries reduced-resolution pyramid levels in later
// IFDs, which are never needed). Anything that does not match the expected
// shape is rejected rather than trying to be a general TIFF reader.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

const (
	tagImageWidth      = 256
	tagImageLength     = 257
	tagBitsPerSample   = 258
	tagCompression     = 259
	tagSamplesPerPixel = 277
	tagPlanarConfig    = 284
	tagDateTime        = 306
	tagPredictor       = 317
	tagTileWidth       = 322
	tagTileLength      = 323
	tagTileOffsets     = 324
	tagTileByteCounts  = 325
	tagSampleFormat    = 339
	tagMaxSampleValue  = 341

	typeASCII  = 2
	typeShort  = 3
	typeLong   = 4
	typeFloat  = 11
	typeDouble = 12

	compressionNone    = 1
	compressionDeflate = 8

	tileSide   = 256
	tilePixels = tileSide * tileSide

	// tileBytes is the size of a decoded tile: 65536 32-bit floats = 256 KiB.
	tileBytes = tilePixels * 4

	// maxTileBlob bounds a tile's stored (possibly compressed) size. A
	// DEFLATE stream can't exceed its input by much even in the
	// incompressible worst case (stored blocks add ~5 bytes per 64 KiB, plus
	// the zlib wrapper), so a larger blob is malformed. Rejecting it when the
	// file is opened keeps every allocation in the decode path bounded to
	// about one tile.
	maxTileBlob = tileBytes + 4096
)

var le = binary.LittleEndian

// ErrFormat reports that a file is not a readable OSMViews raster: not a TIFF
// at all, or not the exact TIFF layout that the OSMViews pipeline produces,
// or corrupt. Errors returned by this package for such files wrap ErrFormat,
// so callers can test for it with [errors.Is].
var ErrFormat = errors.New("not a readable OSMViews raster")

func formatError(msg string) error {
	return fmt.Errorf("osmviews: %w: %s", ErrFormat, msg)
}

// header is everything Rank needs from the file header.
type header struct {
	// size is the raster width in pixels, equal to its height.
	size int
	// tilesAcross is the number of tiles along one axis, size / 256.
	tilesAcross int
	// compression is the TIFF compression tag: 1 (none) or 8 (DEFLATE).
	compression int
	// maxValue is the highest sample value anywhere in the raster, from the
	// SMaxSampleValue tag.
	maxValue float64
	// date is the last day of OpenStreetMap tile-log data painted into the
	// raster, from the DateTime tag.
	date time.Time
	// tileOffsets and tileByteCounts are indexed by tile-grid position.
	tileOffsets    []uint32
	tileByteCounts []uint32
}

type ifdEntry struct {
	typ   uint16
	count uint32
	value [4]byte // the value/offset field, verbatim
}

func (e ifdEntry) scalarInt() (int, bool) {
	if e.count != 1 {
		return 0, false
	}
	switch e.typ {
	case typeShort:
		return int(le.Uint16(e.value[:])), true
	case typeLong:
		return int(le.Uint32(e.value[:])), true
	}
	return 0, false
}

// parser reads from r, which holds n bytes.
type parser struct {
	r io.ReaderAt
	n int64
}

// read returns the length bytes at offset off, or a format error if they
// extend past the end of the file.
func (p *parser) read(off int64, length int, what string) ([]byte, error) {
	if off < 0 || length < 0 || off+int64(length) > p.n {
		return nil, formatError(what + " extends past end of file")
	}
	buf := make([]byte, length)
	n, err := p.r.ReadAt(buf, off)
	if n < length {
		if err == nil || errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("osmviews: reading %s: %w", what, err)
	}
	return buf, nil
}

// parseHeader parses and validates the header of an OSMViews GeoTIFF held in
// r, which is n bytes long. It also checks every tile's byte range, so that
// a corrupt tile table is rejected here rather than surfacing in Rank.
func parseHeader(r io.ReaderAt, n int64) (*header, error) {
	p := &parser{r: r, n: n}
	if n < 8 {
		return nil, formatError("not a TIFF (file too short)")
	}
	head, err := p.read(0, 8, "TIFF header")
	if err != nil {
		return nil, err
	}
	switch string(head[0:2]) {
	case "II":
	case "MM":
		return nil, formatError("big-endian TIFF is not supported")
	default:
		return nil, formatError("not a TIFF")
	}
	switch le.Uint16(head[2:4]) {
	case 42:
	case 43:
		return nil, formatError("BigTIFF is not supported")
	default:
		return nil, formatError("not a TIFF (unrecognized version)")
	}

	ifd := int64(le.Uint32(head[4:8]))
	countBytes, err := p.read(ifd, 2, "IFD")
	if err != nil {
		return nil, err
	}
	numEntries := int(le.Uint16(countBytes))
	// The entry table, plus the trailing 4-byte "next IFD" pointer.
	table, err := p.read(ifd+2, numEntries*12+4, "IFD")
	if err != nil {
		return nil, err
	}

	fields := make(map[uint16]int)
	var offsetsEntry, byteCountsEntry, dateEntry, maxEntry *ifdEntry
	for i := range numEntries {
		raw := table[i*12 : i*12+12]
		tag := le.Uint16(raw[0:2])
		e := &ifdEntry{typ: le.Uint16(raw[2:4]), count: le.Uint32(raw[4:8])}
		copy(e.value[:], raw[8:12])
		switch tag {
		case tagTileOffsets:
			offsetsEntry = e
		case tagTileByteCounts:
			byteCountsEntry = e
		case tagDateTime:
			dateEntry = e
		case tagMaxSampleValue:
			maxEntry = e
		default:
			if v, ok := e.scalarInt(); ok {
				fields[tag] = v
			} else {
				fields[tag] = -1
			}
		}
	}
	field := func(tag uint16) (int, bool) {
		v, ok := fields[tag]
		return v, ok
	}
	is := func(tag uint16, want int) bool {
		v, ok := field(tag)
		return ok && v == want
	}

	width, okW := field(tagImageWidth)
	length, okL := field(tagImageLength)
	if !okW || !okL || width < 0 || length < 0 {
		return nil, formatError("missing image dimensions")
	}
	if width != length {
		return nil, formatError("raster is not square")
	}
	size := width
	if size < tileSide || size&(size-1) != 0 {
		return nil, formatError("unexpected raster size")
	}
	if !is(tagTileWidth, tileSide) || !is(tagTileLength, tileSide) {
		return nil, formatError("unexpected tile size")
	}
	if !is(tagBitsPerSample, 32) || !is(tagSampleFormat, 3) {
		return nil, formatError("samples are not 32-bit float")
	}
	if !is(tagSamplesPerPixel, 1) {
		return nil, formatError("expected a single sample per pixel")
	}
	if !is(tagPlanarConfig, 1) {
		return nil, formatError("unexpected planar configuration")
	}
	compression, _ := field(tagCompression)
	if compression != compressionNone && compression != compressionDeflate {
		return nil, formatError("unsupported compression")
	}
	if pred, ok := field(tagPredictor); ok && pred != 1 {
		return nil, formatError("TIFF predictor is not supported")
	}

	if maxEntry == nil {
		return nil, formatError("missing SMaxSampleValue")
	}
	maxValue, err := p.scalarFloat(maxEntry)
	if err != nil {
		return nil, err
	}
	if math.IsNaN(maxValue) || math.IsInf(maxValue, 0) {
		return nil, formatError("SMaxSampleValue is not finite")
	}

	if dateEntry == nil {
		return nil, formatError("missing DateTime")
	}
	date, err := p.dateTime(dateEntry)
	if err != nil {
		return nil, err
	}

	tilesAcross := size / tileSide
	grid := int64(tilesAcross) * int64(tilesAcross)
	if offsetsEntry == nil {
		return nil, formatError("missing TileOffsets")
	}
	if byteCountsEntry == nil {
		return nil, formatError("missing TileByteCounts")
	}
	offsets, err := p.tileTable(offsetsEntry, grid, "TileOffsets")
	if err != nil {
		return nil, err
	}
	byteCounts, err := p.tileTable(byteCountsEntry, grid, "TileByteCounts")
	if err != nil {
		return nil, err
	}
	for i, off := range offsets {
		count := byteCounts[i]
		if count == 0 || off < 8 {
			return nil, formatError("invalid tile entry")
		}
		if count > maxTileBlob {
			return nil, formatError("tile blob is implausibly large")
		}
		if int64(off)+int64(count) > n {
			return nil, formatError("tile data extends past end of file")
		}
	}

	return &header{
		size:           size,
		tilesAcross:    tilesAcross,
		compression:    compression,
		maxValue:       maxValue,
		date:           date,
		tileOffsets:    offsets,
		tileByteCounts: byteCounts,
	}, nil
}

func (p *parser) scalarFloat(e *ifdEntry) (float64, error) {
	if e.count == 1 {
		switch e.typ {
		case typeFloat:
			return float64(math.Float32frombits(le.Uint32(e.value[:]))), nil
		case typeDouble:
			b, err := p.read(int64(le.Uint32(e.value[:])), 8, "SMaxSampleValue")
			if err != nil {
				return 0, err
			}
			return math.Float64frombits(le.Uint64(b)), nil
		}
	}
	return 0, formatError("malformed SMaxSampleValue")
}

// dateTime reads the DateTime tag (306). TIFF 6.0 spells it
// "YYYY:MM:DD HH:MM:SS", 20 bytes with the trailing NUL. The OSMViews
// pipeline puts the last tile-log day there, so the time is always midnight.
func (p *parser) dateTime(e *ifdEntry) (time.Time, error) {
	// ASCII, and long enough to be stored out of line (never packed into the
	// 4-byte value field): the string alone is 19 characters.
	if e.typ != typeASCII || e.count < 20 {
		return time.Time{}, formatError("malformed DateTime")
	}
	b, err := p.read(int64(le.Uint32(e.value[:])), int(e.count), "DateTime")
	if err != nil {
		return time.Time{}, err
	}
	for i, c := range b {
		if c == 0 {
			b = b[:i]
			break
		}
	}
	t, err := time.Parse("2006:01:02 15:04:05", string(b))
	if err != nil {
		return time.Time{}, formatError("malformed DateTime")
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
}

func (p *parser) tileTable(e *ifdEntry, grid int64, what string) ([]uint32, error) {
	var elemSize int64
	switch e.typ {
	case typeShort:
		elemSize = 2
	case typeLong:
		elemSize = 4
	default:
		return nil, formatError("unexpected " + what + " element type")
	}
	if int64(e.count) != grid {
		return nil, formatError(what + " length does not match the tile grid")
	}
	total := grid * elemSize
	if total > p.n {
		return nil, formatError(what + " extends past end of file")
	}
	if total <= 4 {
		// Would be stored inline in the entry; the OSMViews pipeline never
		// emits a single-tile image, so that path is not implemented.
		return nil, formatError("inline " + what + " not supported")
	}
	// The bounds check in read happens before anything is allocated, so a
	// crafted header cannot make this allocate more than the file's size.
	b, err := p.read(int64(le.Uint32(e.value[:])), int(total), what)
	if err != nil {
		return nil, err
	}
	out := make([]uint32, grid)
	if elemSize == 2 {
		for i := range out {
			out[i] = uint32(le.Uint16(b[2*i:]))
		}
	} else {
		for i := range out {
			out[i] = le.Uint32(b[4*i:])
		}
	}
	return out, nil
}
