// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package osmviews

import (
	"math"
	"os"
	"testing"
	"time"
)

// openDataset opens the real ~594 MB OSMViews raster, named by the
// OSMVIEWS_TIFF environment variable or found as osmviews.tiff in the
// repository root, and skips the test if there is none.
//
// Tests using it check only relative order and coarse thresholds, never
// absolute values: the dataset is regenerated weekly and drifts.
func openDataset(t *testing.T) *Raster {
	t.Helper()
	path := os.Getenv("OSMVIEWS_TIFF")
	if path == "" {
		path = "osmviews.tiff"
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("needs the ~594 MB osmviews.tiff (set OSMVIEWS_TIFF or put it in the repository root)")
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestOnlineRanksReflectHowThePlanetIsViewed(t *testing.T) {
	r := openDataset(t)
	at := func(lng, lat float64) float64 { return rank(t, r, [2]float64{lng, lat}) }

	londonCentre := at(-0.1281, 51.5080) // Trafalgar Square
	londonInner := at(-0.0553, 51.5452)  // Hackney
	londonOuter := at(0.1730, 51.6217)   // Havering-atte-Bower
	bernCentre := at(7.4474, 46.9480)
	ushuaia := at(-68.3030, -54.8019)

	// Cross-region ordering of city centres.
	if !(londonCentre > bernCentre && bernCentre > ushuaia) {
		t.Errorf("London %v, Bern %v, Ushuaia %v: want descending", londonCentre, bernCentre, ushuaia)
	}
	// The dataset's ~150 m resolution resolves the fall-off across one city.
	if !(londonCentre > londonInner && londonInner > londonOuter) {
		t.Errorf("London centre %v, inner %v, outer %v: want descending", londonCentre, londonInner, londonOuter)
	}
	// Remote or empty places: well below any inhabited point, but not
	// necessarily exactly zero, and not ordered against each other.
	for _, p := range []struct {
		name     string
		lng, lat float64
	}{
		{"Sahara", 13, 23},
		{"remote South Pacific", -140, -30},
		{"Birdsville", 139.3508, -25.8975},
	} {
		if v := at(p.lng, p.lat); v >= 0.1 || v >= ushuaia {
			t.Errorf("%s = %v, want below 0.1 and below Ushuaia (%v)", p.name, v, ushuaia)
		}
	}
	// Poles and non-finite inputs.
	for _, p := range [][2]float64{{0, 90}, {0, -90}, {math.NaN(), 0}} {
		if v := at(p[0], p[1]); v != 0 {
			t.Errorf("Rank(%v, %v) = %v, want 0", p[0], p[1], v)
		}
	}

	m := r.Metrics()
	if m.OutOfRange != 3 || m.TilesDecoded < 1 {
		t.Errorf("Metrics = %+v; want 3 out of range, at least 1 decoded", m)
	}
}

func TestOnlineDateIsPlausible(t *testing.T) {
	r := openDataset(t)
	earliest := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	horizon := time.Now().AddDate(0, 0, 14)
	if d := r.Date(); d.Before(earliest) || d.After(horizon) {
		t.Errorf("Date = %v, want between %v and %v", d, earliest, horizon)
	}
}
