// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package osmviews

import "math"

// The OSMViews GeoTIFF is stored in Web Mercator (EPSG:3857) and its pixel
// grid lines up exactly with the standard "slippy map" tile scheme at the zoom
// level whose world width in pixels equals the raster width. So the mapping
// is the well-known slippy-tile math, done here in a few lines rather than
// pulling in a projection library.

// maxLat is the latitude beyond which Web Mercator is not defined. Locations
// at or past this latitude (in either hemisphere) have no data.
const maxLat = 85.05112877980659

// project maps lng/lat (WGS84 degrees) to an x, y pixel in a size × size
// raster that spans the whole Web Mercator world square.
//
// Longitude is periodic, so 182.0 is the same meridian as -178.0. The result
// is not ok when the inputs are not finite or the latitude is outside the Web
// Mercator range.
func project(lng, lat float64, size int) (x, y int, ok bool) {
	if math.IsNaN(lng) || math.IsInf(lng, 0) ||
		math.IsNaN(lat) || math.IsInf(lat, 0) || math.Abs(lat) >= maxLat {
		return 0, 0, false
	}
	turns := math.Mod(lng+180.0, 360.0)
	if turns < 0 {
		turns += 360.0
	}
	fx := turns / 360.0 * float64(size)
	// asinh(tan φ) = atanh(sin φ) = ½·ln((1 + sin φ) / (1 − sin φ)), and
	// math.Sin plus math.Log is several times faster than math.Tan plus
	// math.Asinh.
	s := math.Sin(lat * (math.Pi / 180.0))
	fy := (1.0 - 0.5*math.Log((1+s)/(1-s))/math.Pi) / 2.0 * float64(size)
	return clamp(fx, size-1), clamp(fy, size-1), true
}

func clamp(v float64, hi int) int {
	i := int(math.Floor(v))
	return min(max(i, 0), hi)
}
