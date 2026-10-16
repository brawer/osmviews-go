// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package osmviews_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/brawer/osmviews-go"
)

func ExampleOpen() {
	r, err := osmviews.Open("osmviews.tiff")
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	// Rank is 0 (nobody looks here) to 1 (one of the most-viewed places on
	// Earth). Coordinates are longitude, then latitude, as in GeoJSON;
	// values drift weekly.
	for _, place := range []struct {
		name     string
		lng, lat float64
	}{
		{"Tokyo, Shibuya", 139.7013, 35.6586},   // ~0.69
		{"Zürich, Altstetten", 8.4889, 47.3915}, // ~0.66
		{"Ushuaia", -68.3030, -54.8019},         // ~0.56
		{"Sahara", 13.0000, 23.0000},            // ~0.00
	} {
		rank, err := r.Rank(place.lng, place.lat)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%-20s %.2f\n", place.name, rank)
	}
	fmt.Println("data up to", r.Date().Format("2006-01-02"))

	// Worth logging once at the end of a long-running job.
	slog.Info("osmviews", "metrics", r.Metrics())
}

// This example downloads the current raster with the standard library
// alone, checking it against the SHA-256 in the data package descriptor.
func Example_download() {
	resp, err := http.Get(osmviews.DataPackageURL)
	if err != nil {
		log.Fatal(err)
	}
	var pkg struct {
		Resources []struct {
			Name, Path, Hash string
		}
	}
	err = json.NewDecoder(resp.Body).Decode(&pkg)
	resp.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
	var path, hash string
	for _, res := range pkg.Resources {
		if res.Name == "osmviews" {
			path, hash = res.Path, res.Hash
		}
	}
	base, _ := url.Parse(osmviews.DataPackageURL)
	ref, err := url.Parse(path)
	if err != nil || path == "" {
		log.Fatalf("no osmviews resource in %s", osmviews.DataPackageURL)
	}
	rasterURL := base.ResolveReference(ref).String()

	resp, err = http.Get(rasterURL)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", rasterURL, resp.Status)
	}
	f, err := os.Create("osmviews.tiff")
	if err != nil {
		log.Fatal(err)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	if got := "sha256:" + hex.EncodeToString(h.Sum(nil)); got != hash {
		log.Fatalf("checksum mismatch for %s: got %s, want %s", rasterURL, got, hash)
	}
}
