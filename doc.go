// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

// Package osmviews is a client for [OSMViews], a world-wide ranking of
// geographic locations by how much they are looked at on OpenStreetMap-based
// maps.
//
// OSMViews aggregates a year of OpenStreetMap map-tile access logs into a
// single raster covering the whole planet. This package reads a copy of that
// raster from local disk and answers point queries:
//
//	r, err := osmviews.Open("osmviews.tiff")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer r.Close()
//
//	// A value from 0.0 (nobody looks here) to 1.0 (one of the most-viewed
//	// places). Coordinates are longitude, then latitude, as in GeoJSON.
//	shibuya, err := r.Rank(139.7013, 35.6586)
//
// The package does not download anything. The raster (~594 MB) is rebuilt
// weekly under a new dated file name, so there is no single download link.
// Instead, fetch the small [data package] descriptor at [DataPackageURL],
// pick the entry in its "resources" array whose "name" is "osmviews", resolve
// its "path" relative to the descriptor's URL, download that file, and check
// it against the entry's "hash" (of the form "sha256:<hex>"). The package
// example shows how to do this with the standard library alone.
//
// A [Raster] is safe for concurrent use by multiple goroutines.
//
// [OSMViews]: https://osmviews.brawer.ch
// [data package]: https://datapackage.org
package osmviews
