package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// nauticalTileHandler is the stable Leaflet-facing tile endpoint. The data
// source is intentionally local-only for now; a future NOAA fetch/cache or
// MBTiles reader can be added here without another browser integration.
func nauticalTileHandler(directory string) http.Handler {
	root := strings.TrimSpace(directory)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		z, x, y, ok := parseNauticalTilePath(r.URL.Path)
		if !ok {
			http.Error(w, "invalid nautical tile coordinates", http.StatusBadRequest)
			return
		}
		if root == "" {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "nautical chart tiles are unavailable", http.StatusServiceUnavailable)
			return
		}
		// Tile folders are raster XYZ: z/x/y.png. Root is an explicitly
		// configured operator-owned directory outside the repository.
		filename := filepath.Join(root, strconv.Itoa(z), strconv.Itoa(x), fmt.Sprintf("%d.png", y))
		data, err := os.ReadFile(filename)
		if os.IsNotExist(err) {
			http.Error(w, "nautical tile not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "nautical chart tile unavailable", http.StatusServiceUnavailable)
			return
		}
		if len(data) < 8 || len(data) > 4*1024*1024 ||
			!bytes.Equal(data[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
			http.Error(w, "invalid nautical tile", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	})
}

const nauticalTilePrefix = "/api/charts/tiles/"

func parseNauticalTilePath(path string) (z, x, y int, ok bool) {
	if !strings.HasPrefix(path, nauticalTilePrefix) || !strings.HasSuffix(path, ".png") {
		return 0, 0, 0, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(path, nauticalTilePrefix), ".png"), "/")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	values := [3]int{}
	for i, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" || len(part) > 9 {
			return 0, 0, 0, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, 0, 0, false
		}
		values[i] = n
	}
	z, x, y = values[0], values[1], values[2]
	if z < 0 || z > 18 || x >= 1<<uint(z) || y >= 1<<uint(z) {
		return 0, 0, 0, false
	}
	return z, x, y, true
}
