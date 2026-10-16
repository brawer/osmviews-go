<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# osmviews-go

[![Go Reference](https://pkg.go.dev/badge/github.com/brawer/osmviews-go.svg)](https://pkg.go.dev/github.com/brawer/osmviews-go)
[![CI](https://github.com/brawer/osmviews-go/actions/workflows/ci.yml/badge.svg)](https://github.com/brawer/osmviews-go/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/brawer/osmviews-go/badge)](https://scorecard.dev/viewer/?uri=github.com/brawer/osmviews-go)
[![REUSE status](https://api.reuse.software/badge/github.com/brawer/osmviews-go)](https://api.reuse.software/info/github.com/brawer/osmviews-go)

Go client for [OSMViews](https://osmviews.brawer.ch), a world-wide ranking of
geographic locations by how much they are looked at on OpenStreetMap-based
maps. See the [main project](https://github.com/brawer/osmviews) for
background.

OSMViews aggregates a year of OpenStreetMap map-tile access logs into a single
raster covering the whole planet. This package reads a copy of that raster from
local disk and answers point queries.

## Usage

```sh
go get github.com/brawer/osmviews-go
```

```go
import "github.com/brawer/osmviews-go" // package osmviews

r, err := osmviews.Open("osmviews.tiff")
if err != nil {
	log.Fatal(err)
}
defer r.Close()

// Rank is 0 (nobody looks here) to 1 (one of the most-viewed places on
// Earth). Coordinates are longitude, then latitude, as in GeoJSON.
rank, err := r.Rank(139.7013, 35.6586) // Tokyo, Shibuya
if err != nil {
	log.Fatal(err)
}
fmt.Printf("%.2f\n", rank)

// Values drift weekly, but are around:
//   Tokyo, Shibuya       139.7013,  35.6586   0.69
//   Zürich, Altstetten     8.4889,  47.3915   0.66
//   Ushuaia              -68.3030, -54.8019   0.56
//   Sahara                13.0000,  23.0000   0.00
```

`Rank` returns an error only when the tile holding the location can't be read
(an I/O error, or a corrupt tile, which wraps `osmviews.ErrFormat`).
Coordinates outside the map — near the poles, or `NaN` — simply rank 0.

The package does **not** download anything. The dataset (~594 MB) is rebuilt
weekly under a new dated file name, so there is no single download link: fetch
the small [data package](https://datapackage.org) descriptor at
`osmviews.DataPackageURL`, which names the current file and its SHA-256,
download that however you like, then pass the path to `osmviews.Open`. The
[package example](https://pkg.go.dev/github.com/brawer/osmviews-go#example-package-Download)
does this with `net/http` and `crypto/sha256` alone.

`r.Date()` gives the last day of OpenStreetMap tile-log data in the raster, so
you can tell which week's views you have; it matches the data package's
`version`.

A `*Raster` is safe for concurrent use by multiple goroutines: every query
takes a mutex only briefly, and tile decoding happens outside it. Decoded tiles
are kept in a small LRU cache (`osmviews.Open(path,
osmviews.WithCacheTiles(n))`, `0` disables it), so queries clustered in one
region stay fast. `r.Metrics()` returns a snapshot of counters (cache hit rate,
decode time, …) worth logging at the end of a long run; it implements
`slog.LogValuer`.

The file is read with positioned reads rather than memory-mapped, so there is
no `unsafe`, no cgo and no platform-specific code. `osmviews.New` accepts any
`io.ReaderAt` if the raster isn't a plain local file.

## Performance

`go test -bench .` on an Intel Xeon at 2.8 GHz: `Rank` returns in ~100 ns when
the tile is already cached and ~350 µs on a miss that has to read and inflate
one (the Python client takes ~1.8 µs and ~460 µs on the same machine). Each
decoded tile is 256 KiB; the default LRU holds 64 of them (~16 MiB). The tile
index, about 8 MB for the published raster, is read into memory by `Open`. For
bulk lookups, submit points in roughly spatial order (for example sorted by S2
cell ID) so neighbouring queries reuse cached tiles.

## No dependencies

Standard library only (`compress/zlib`, `encoding/binary`, `container/list`).
The TIFF header parsing and the map projection are done in-package. The minimum
supported Go version is the one in `go.mod`.

## Other clients

[osmviews-rs](https://github.com/brawer/osmviews-rs) for Rust and
[osmviews-py](https://github.com/brawer/osmviews-py) for Python read the same
file and behave the same way; the APIs follow each language's own conventions.

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). The design
and its rationale are written up in [TECHNICAL_DESIGN.md](TECHNICAL_DESIGN.md).

## Sponsoring

This package and the [OSMViews](https://github.com/brawer/osmviews) pipeline
behind it are maintained by [Sascha Brawer](https://github.com/brawer) as a
volunteer effort. If your project relies on them, please consider sponsoring
continued maintenance and future development via
[GitHub Sponsors](https://github.com/sponsors/brawer).

## License

MIT — see [LICENSE](LICENSE).

The repository is [REUSE](https://reuse.software) compliant: every file
declares its copyright and license, either in an SPDX header or via
`REUSE.toml`. All code and documentation is MIT; the sole exception is
`CODE_OF_CONDUCT.md`, adapted from the
[Contributor Covenant](https://www.contributor-covenant.org) and licensed
CC-BY-SA-4.0.
