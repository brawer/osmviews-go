<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Test data

`zurich-2021-W47.tiff` is a real OSMViews raster written by the pipeline's own
GeoTIFF writer, so the tests cover the exact layout the pipeline produces and
not only the synthetic files that `fixtures_test.go` builds. It was painted at
pixel zoom 14 (16 384 × 16 384 pixels, 64 × 64 tiles, most of them the shared
empty tile) from one week of tile logs around Zürich,
[`cmd/osmviews-builder/testdata/zurich-2021-W47.br`](https://github.com/brawer/osmviews/blob/main/cmd/osmviews-builder/testdata/zurich-2021-W47.br)
in brawer/osmviews, with `DateTime` set to 2021-11-28, by calling the
builder's `paint(path, 14, readers, nil, TiffMetadata{DateTime: …}, ctx)`.
The Python client gives the same ranks for it.
