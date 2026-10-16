<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Technical design: the `osmviews` Go package

## Objective

Provide a small, dependency-free Go package that, given a locally available
copy of the OSMViews raster, answers:

> How much is the location at this longitude/latitude looked at on
> OpenStreetMap-based maps, on a scale from 0 to 1?

The package reads a file and nothing else — no network, no configuration, no
third-party modules.

## Background

[OSMViews](https://osmviews.brawer.ch) is a weekly pipeline
([brawer/osmviews](https://github.com/brawer/osmviews)) that aggregates roughly
a year of OpenStreetMap map-tile access logs into a single Cloud-Optimized
GeoTIFF. Each pixel holds a 32-bit float: the density of map views for that
patch of the planet. The file is about 594 MB and covers the whole world at
~150 m resolution.

The overall system is described in a [defensive
publication](https://raw.githubusercontent.com/brawer/osmviews/main/docs/defensive-publication/defensive-publication.pdf),
written as such to establish prior art and keep the approach unpatentable.

[Rust](https://github.com/brawer/osmviews-rs) and
[Python](https://github.com/brawer/osmviews-py) clients also exist. All three
share their behaviour — the same validation, projection, scaling and cache
policy — but not their code, and each API follows its own language's
conventions rather than mirroring the others.

## Module path

The module is `github.com/brawer/osmviews-go` and its package is `osmviews`,
following the `-go` suffix convention for Go clients of a non-Go-named
project. goimports infers the package name from such a path, so no import
alias is needed. A separate repository, rather than a nested module in the
pipeline's repository, keeps the client's version history (`v0.x`, later
`v1`) independent of the pipeline's releases (`github.com/brawer/osmviews/v2`)
and keeps the pipeline's dependencies out of client builds.

## API

```go
const DataPackageURL = "https://osmviews.brawer.ch/data/datapackage.json"
const DefaultCacheTiles = 64
var ErrFormat = errors.New("not a readable OSMViews raster")

func Open(name string, opts ...Option) (*Raster, error)
func New(r io.ReaderAt, size int64, opts ...Option) (*Raster, error)
func WithCacheTiles(n int) Option

func (r *Raster) Rank(lng, lat float64) (float64, error)
func (r *Raster) Date() time.Time
func (r *Raster) Metrics() Metrics
func (r *Raster) Close() error

type Metrics struct { Queries, OutOfRange, CacheHits, … }
func (m Metrics) HitRate() float64
func (m Metrics) LogValue() slog.Value
```

Choices that differ from the other clients, and why:

- **`Rank` returns an error.** The Rust and Python clients memory-map the file
  and return 0.0 for a tile that fails to decode. Go has no portable mmap in
  the standard library, so this package uses positioned reads, which can fail;
  Go callers expect a fallible read to say so. Coordinates outside the map are
  not an error: they rank 0, as in the other clients.
- **`New(io.ReaderAt, size)`**, alongside `Open`, mirrors `zip.NewReader` and
  `debug/elf.NewFile`: the raster can come from anything that supports random
  access, and tests can use a `bytes.Reader`.
- **Functional options** (`WithCacheTiles`) keep `Open(path)` short for the
  common case and leave room for later settings without breaking callers. A
  zero value can't mean "default" here, because 0 already means "no cache".
- **`ErrFormat` as a sentinel**, tested with `errors.Is`, rather than an error
  type: callers only ever need to tell "this is not an OSMViews file" from an
  I/O error, and the message carries the detail.
- **`Date` returns a `time.Time`** at midnight UTC, Go having no date-only
  type.
- **`Metrics` implements `slog.LogValuer`**, since logging it once at the end
  of a job is its purpose.
- **No deprecated `DOWNLOAD_URL`**: the package is new, so it never had one.

## Design

**Input.** `Open` opens the file and reads it with `ReadAt` (`pread(2)` on
Unix). Compared with a memory map this costs one system call per tile miss —
small next to inflating 256 KiB — and buys portability with no `unsafe`, no
cgo and no build tags. It also turns the memory-map clients' documented hazard
(the file must not be truncated while open, or the process can take a
`SIGBUS`) into an ordinary error from `Rank`.

**TIFF parsing.** Hand-written, in `tiff.go`. It reads only IFD 0 (the
full-resolution level; the file also carries reduced-resolution pyramid levels
that this package never needs) and validates that the file is the exact shape
the OSMViews pipeline produces — little-endian classic TIFF, 256×256 tiles,
single-band 32-bit float, DEFLATE or no compression, no predictor. It also
checks that every tile’s byte range lies inside the file and is no larger than
a 256 KiB tile could plausibly compress to (`tileBytes + 4096`), so a crafted
file can’t make `Rank` read an outsized blob. It reads the `DateTime` tag
(306), which the pipeline sets to the last day of tile-log data in the raster.
Anything else — including a missing or unparseable `DateTime` — is rejected
with an error wrapping `ErrFormat`. Every read is bounds-checked against the
file size before anything is allocated. A general TIFF library would be far
more code and surface area for a format we fully control.

**Tile index.** `TileOffsets` and `TileByteCounts` (about 1 million entries
each for the published raster, so ~8 MB together) are read into two
`[]uint32` slices at open time. The other clients leave them in the memory map;
here, keeping them in memory saves two system calls per query.

**Projection.** The OSMViews grid is Web Mercator (EPSG:3857) and lines up
exactly with the standard “slippy map” tile scheme, so mapping
longitude/latitude to a pixel is a few lines of arithmetic in `projection.go`
— no projection library. The northing uses atanh(sin φ), written as
½·ln((1 + sin φ)/(1 − sin φ)), which is identical to the other clients'
asinh(tan φ) but faster with Go's `math` package; a 10-million-point
comparison found no pixel that differs. Longitude wraps; latitudes past the
Web Mercator limit, and non-finite inputs, rank 0.

**Decompression.** Tiles are zlib-compressed, so the package uses
`compress/zlib`. `io.ReadFull` reads exactly one tile's worth of output (256
KiB), and a further one-byte read must then hit the end of the stream, which
is also where zlib verifies its Adler-32 checksum. Together with the
stored-size check at open time, no crafted tile — whatever its compression
ratio — can drive an allocation past about one tile. The compressed and
decompressed buffers and the zlib reader are recycled through a `sync.Pool`.

**Tile cache.** Decoding a tile is the expensive step, so decoded tiles are
kept in a small LRU cache (`container/list` plus a map; default 64 tiles ≈ 16
MiB, configurable; `0` disables it). The cache is **keyed by the tile’s byte
offset in the file, not by its grid position**, because the raster is sparse:
its ~1 million grid positions resolve to only about 100 000 distinct tiles, and
two “empty” tiles alone back most of the oceans. Keying by offset means a sweep
across open water occupies a single cache entry instead of evicting everything
useful.

**Concurrency.** `Rank` takes a `sync.Mutex` only to read or update the cache
and its counters; the tile read and decode of a miss happen outside the lock,
so concurrent readers don't serialize on slow work. The worst case is two
goroutines briefly decoding the same tile and producing identical results.
One `*Raster` can therefore serve many goroutines. The tests run under the race
detector in CI.

**Output.** `Rank` returns a `float64` in `0..1`. Internally this is the raw
sample divided by the raster’s embedded planetary maximum (`SMaxSampleValue`),
clamped; that scaling is an implementation detail and not part of the API
contract.

**Observability.** `Metrics` returns a snapshot — query count, cache hit rate,
evictions, cumulative decode time — meant to be logged once at the end of a
long-running job. The counters live under the mutex that every query already
takes, so they add nothing measurable to the hot path.

## Non-goals

- **Downloading or refreshing** the dataset. Callers fetch the file
  themselves; the package only exposes `DataPackageURL`, the descriptor that
  names the current dated raster and its SHA-256, plus a documented example
  that downloads and verifies it with the standard library.
- **On-demand tile loading over the network.** The file genuinely is a
  Cloud-Optimized GeoTIFF, and since `New` takes an `io.ReaderAt`, a caller
  could plug in an HTTP range-request reader; but the package targets
  pipelines that download the whole file up front, and does not ship one.
- **Writing GeoTIFFs**, reading other coordinate reference systems, or reading
  rasters other than OSMViews.
- **Exposing raw pixel values.** The `0..1` scale of `Rank` is the contract;
  see **Output**. (The dataset date *is* exposed, as `Date`.)

## Security

- **No dependencies**: standard library only, so there is no third-party
  supply chain to audit. `govulncheck` runs in CI, weekly as well as on every
  push, to catch standard-library advisories.
- **Bounded allocations**: every header read is checked against the file size
  before allocating, `Open` rejects any tile whose stored blob exceeds ~260
  KiB, and decompression stops after 256 KiB + 1 byte of output.
- **Fuzzing**: `FuzzNew` feeds arbitrary bytes to `New` and `Rank` and checks
  that they never panic, fail with anything but `ErrFormat`, or rank outside
  `0..1`; CI runs it briefly on every push.
- **CodeQL** and **OpenSSF Scorecard** run in CI; Dependabot proposes GitHub
  Actions updates as grouped monthly pull requests.
- **Releases** are plain git tags, served by the Go module proxy and recorded
  in the [checksum database](https://sum.golang.org), so every download of a
  given version is verified by `go` to be byte-identical. See
  [RELEASING.md](RELEASING.md).
