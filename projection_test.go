// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package osmviews

import (
	"math"
	"testing"
)

const worldSize = 262_144

// reference is an independent implementation of the slippy-tile northing,
// written the textbook way with ln(tan + sec) instead of asinh(tan).
func reference(lng, lat float64, size int) (int, int) {
	rad := lat * math.Pi / 180
	x := (lng + 180) / 360 * float64(size)
	y := (1 - math.Log(math.Tan(rad)+1/math.Cos(rad))/math.Pi) / 2 * float64(size)
	return int(math.Floor(x)), int(math.Floor(y))
}

func TestProjectNullIslandIsTheCentrePixel(t *testing.T) {
	x, y, ok := project(0, 0, worldSize)
	if !ok || x != worldSize/2 || y != worldSize/2 {
		t.Errorf("project(0, 0) = %d, %d, %v; want %d, %d, true", x, y, ok, worldSize/2, worldSize/2)
	}
}

func TestProjectAntimeridianIsTheLeftEdge(t *testing.T) {
	for _, lng := range []float64{-180, 180, -540} {
		if x, _, _ := project(lng, 0, worldSize); x != 0 {
			t.Errorf("project(%v, 0): x = %d, want 0", lng, x)
		}
	}
}

func TestProjectLongitudeWraps(t *testing.T) {
	for _, lng := range []float64{-178, 0, 90, 179.9} {
		x, y, _ := project(lng, 20, worldSize)
		for _, other := range []float64{lng + 360, lng - 360} {
			if ox, oy, _ := project(other, 20, worldSize); ox != x || oy != y {
				t.Errorf("project(%v, 20) = %d, %d; project(%v, 20) = %d, %d", other, ox, oy, lng, x, y)
			}
		}
	}
	x1, _, _ := project(182, 0, worldSize)
	x2, _, _ := project(-178, 0, worldSize)
	if x1 != x2 {
		t.Errorf("182°E is x = %d, 178°W is x = %d", x1, x2)
	}
}

func TestProjectRejectsOutOfRangeAndNonFinite(t *testing.T) {
	for _, p := range [][2]float64{
		{0, 85.06}, {0, -85.06}, {0, 90}, {0, -90},
		{math.NaN(), 0}, {0, math.Inf(1)}, {math.Inf(-1), 0},
	} {
		if _, _, ok := project(p[0], p[1], worldSize); ok {
			t.Errorf("project(%v, %v) is ok, want not ok", p[0], p[1])
		}
	}
}

func TestProjectMatchesReferenceInEveryQuadrant(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lat, lng float64
		x, y     int
	}{
		{"Tokyo", 35.6586, 139.7016, 232_799, 103_246},
		{"New York", 40.7128, -74.0060, 77_182, 98_561},
		{"Sydney", -33.8688, 151.2093, 241_179, 157_310},
		{"Buenos Aires", -34.6037, -58.3816, 88_559, 157_957},
	} {
		x, y, ok := project(tc.lng, tc.lat, worldSize)
		if !ok || x != tc.x || y != tc.y {
			t.Errorf("%s: project = %d, %d, %v; want %d, %d", tc.name, x, y, ok, tc.x, tc.y)
		}
		if rx, ry := reference(tc.lng, tc.lat, worldSize); rx != tc.x || ry != tc.y {
			t.Errorf("%s: reference = %d, %d; want %d, %d", tc.name, rx, ry, tc.x, tc.y)
		}
	}
}

func FuzzProject(f *testing.F) {
	f.Add(0.0, 0.0)
	f.Add(-180.0, 85.0)
	f.Add(1e300, -1e-300)
	f.Fuzz(func(t *testing.T, lng, lat float64) {
		x, y, ok := project(lng, lat, worldSize)
		if ok && (x < 0 || x >= worldSize || y < 0 || y >= worldSize) {
			t.Errorf("project(%v, %v) = %d, %d: outside the raster", lng, lat, x, y)
		}
	})
}
