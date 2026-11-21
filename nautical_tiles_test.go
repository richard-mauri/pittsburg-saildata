package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseNauticalTilePath(t *testing.T) {
	valid := []string{
		"/api/charts/tiles/0/0/0.png",
		"/api/charts/tiles/11/327/792.png",
		"/api/charts/tiles/16/10672/25741.png",
	}
	for _, path := range valid {
		_, _, _, ok := parseNauticalTilePath(path)
		if !ok { t.Errorf("expected valid %s", path) }
	}
	invalid := []string{
		"/api/charts/tiles/11/2048/0.png",
		"/api/charts/tiles/11/0/2048.png",
		"/api/charts/tiles/19/0/0.png",
		"/api/charts/tiles/11/-1/0.png",
		"/api/charts/tiles/11/1/../2.png",
		"/api/charts/tiles/11/1/2.jpg",
		"/api/charts/tiles/11/1/2.png/extra",
		"/api/charts/tiles/11/1/2.png?x=1",
	}
	for _, path := range invalid {
		_, _, _, ok := parseNauticalTilePath(path)
		if ok { t.Errorf("expected invalid %s", path) }
	}
}

func TestNauticalTileHandlerUnavailableAndLocal(t *testing.T) {
	h := nauticalTileHandler("")
	r := httptest.NewRequest(http.MethodGet, "/api/charts/tiles/11/327/792.png", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("no chart directory must give uncached 503, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") != "60" { t.Fatal("expected retry advice") }

	root := t.TempDir()
	h = nauticalTileHandler(root)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound { t.Fatalf("missing configured tile: %d", w.Code) }

	dir := filepath.Join(root, "11", "327")
	if err := os.MkdirAll(dir, 0700); err != nil { t.Fatal(err) }
	png := append([]byte{137,80,78,71,13,10,26,10}, []byte("fixture PNG content")...)
	if err := os.WriteFile(filepath.Join(dir, "792.png"), png, 0600); err != nil { t.Fatal(err) }
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), png) {
		t.Fatalf("expected local PNG tile; got %d %q", w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "image/png" { t.Fatal("wrong content type") }
	if w.Header().Get("Cache-Control") != "private, max-age=3600" { t.Fatal("wrong cache control") }

	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, r.URL.Path, nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 { t.Fatal("HEAD must return metadata only") }
	badMethod := httptest.NewRecorder()
	h.ServeHTTP(badMethod, httptest.NewRequest(http.MethodPost, r.URL.Path, nil))
	if badMethod.Code != http.StatusMethodNotAllowed { t.Fatal("non GET/HEAD not refused") }
	badCoord := httptest.NewRecorder()
	h.ServeHTTP(badCoord, httptest.NewRequest(http.MethodGet, "/api/charts/tiles/11/2048/0.png", nil))
	if badCoord.Code != http.StatusBadRequest { t.Fatal("invalid coordinates not rejected") }
	if err := os.WriteFile(filepath.Join(dir, "792.png"), []byte("not a PNG"), 0600); err != nil { t.Fatal(err) }
	badPNG := httptest.NewRecorder()
	h.ServeHTTP(badPNG, r)
	if badPNG.Code != http.StatusServiceUnavailable { t.Fatal("corrupt tile not rejected") }
}
