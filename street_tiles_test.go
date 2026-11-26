package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const streetTestPath = streetTilePrefix + "3/2/4.png"

func streetTestPNG(t *testing.T, width int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, width, 256))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func streetTestGet(p http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://weather.themauris.org"+path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

func TestStreetTileHTTPSPublicAndProtectedRoutes(t *testing.T) {
	data := streetTestPNG(t, 256)
	var calls atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/3/2/4.png" || r.Method != "GET" || r.URL.RawQuery != "" {
			t.Errorf("unexpected upstream route %s %s", r.Method, r.URL)
		}
		if !strings.HasPrefix(r.UserAgent(), "SailingGo-StreetTiles/") || r.Referer() != "https://gps.example/" {
			t.Errorf("bad upstream identification: %q %q", r.UserAgent(), r.Referer())
		}
		for _, h := range []string{"Authorization", "Cookie", "Origin", "Cache-Control", "Pragma", "X-User"} {
			if r.Header.Get(h) != "" {
				t.Errorf("forwarded forbidden header %s", h)
			}
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("ETag", "\"tile-v1\"")
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.Write(data)
	}))
	defer up.Close()
	p := makeStreetProxy(up.URL, up.Client().Transport, time.Second)
	mux := http.NewServeMux()
	mux.Handle(streetTilePrefix, p)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("protected route reached mux: %s", r.URL)
		w.WriteHeader(200)
	})
	auth, err := NewSailingHTTPAuth(&SailingSuiteGate{})
	if err != nil {
		t.Fatal(err)
	}
	handler := auth.ProtectWeather(mux)
	down := httptest.NewTLSServer(handler)
	defer down.Close()
	req, _ := http.NewRequest("GET", down.URL+streetTestPath, nil)
	req.Header.Set("Referer", "https://gps.example/map?private=1")
	req.Header.Set("Authorization", "Bearer must-not-forward")
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set("Origin", "https://gps.example")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("X-User", "admin")
	resp, err := down.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" ||
		resp.Header.Get("ETag") != "\"tile-v1\"" || resp.Header.Get("Cache-Control") != "public, max-age=3600" ||
		resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("HTTPS tile: %d %v", resp.StatusCode, resp.Header)
	}
	w := streetTestGet(handler, "GET", streetTestPath, nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) || calls.Load() != 1 {
		t.Fatalf("anonymous cached tile: %d, calls=%d", w.Code, calls.Load())
	}
	w = streetTestGet(handler, "GET", streetTestPath, map[string]string{"If-None-Match": "W/\"tile-v1\""})
	if w.Code != 304 || w.Body.Len() != 0 || calls.Load() != 1 {
		t.Fatalf("cache validator: %d, calls=%d", w.Code, calls.Load())
	}
	for _, path := range []string{"/api/maps/street/3/2/4.png/extra", "/api/maps/street/3/2/x.png",
		"/api/maps/street/3/2/../4.png", "/api%2fmaps%2fstreet/3/2/4.png",
		"/api/maps/future", "/api/charts/tiles/12/1/1.png", "/api/activity/identity",
		"/report?format=json", "/wind-readings", "/assets/marine_places.json"} {
		if w := streetTestGet(handler, "GET", path, nil); w.Code != 401 {
			t.Errorf("protected %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{streetTilePrefix+"20/0/0.png", streetTilePrefix+"3/8/0.png", streetTilePrefix+"0/0/1.png", streetTestPath+"?url=https://evil.example"} {
		if w := streetTestGet(handler, "GET", path, nil); w.Code != 400 {
			t.Errorf("invalid %s: %d", path, w.Code)
		}
	}
	for _, method := range []string{"POST", "HEAD", "OPTIONS", "PUT"} {
		if w := streetTestGet(handler, method, streetTestPath, nil); w.Code != 405 || w.Header().Get("Allow") != "GET" {
			t.Errorf("invalid method %s: %d", method, w.Code)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("invalid/protected requests contacted upstream")
	}
}

func TestStreetTileRevalidation(t *testing.T) {
	data := streetTestPNG(t, 256)
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("ETag", "\"v1\"")
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		if n == 1 {
			w.Header().Set("Cache-Control", "public, max-age=0")
			w.Header().Set("Content-Type", "image/png")
			w.Write(data)
			return
		}
		if r.Header.Get("If-None-Match") != "\"v1\"" || r.Header.Get("If-Modified-Since") != "Mon, 02 Jan 2006 15:04:05 GMT" {
			t.Errorf("stale tile did not revalidate: %v", r.Header)
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(304)
	}))
	defer up.Close()
	p := makeStreetProxy(up.URL, up.Client().Transport, time.Second)
	for i := 0; i < 3; i++ {
		w := streetTestGet(p, "GET", streetTestPath, nil)
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) {
			t.Fatalf("revalidated tile %d: %d", i, w.Code)
		}
		want := []string{"upstream-tile", "cache-revalidated", "cache-hit"}[i]
		if w.Header().Get("X-Sailing-Street-Result") != want {
			t.Fatalf("revalidation result %d: %v", i, w.Header())
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("want initial and revalidation only, got %d", calls.Load())
	}
}

func TestStreetTileConditionalColdCache(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "\"v1\"" || r.Header.Get("If-Modified-Since") == "" {
			t.Errorf("missing client conditional headers: %v", r.Header)
		}
		w.Header().Set("ETag", "\"v1\"")
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(304)
	}))
	defer up.Close()
	p := makeStreetProxy(up.URL, up.Client().Transport, time.Second)
	w := streetTestGet(p, "GET", streetTestPath, map[string]string{"If-None-Match": "\"v1\"", "If-Modified-Since": "Mon, 02 Jan 2006 15:04:05 GMT"})
	if w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != "\"v1\"" {
		t.Fatalf("conditional pass-through: %d %v", w.Code, w.Header())
	}
}

func TestStreetTileUpstreamFailuresAndBounds(t *testing.T) {
	data := streetTestPNG(t, 256)
	wrongSize := streetTestPNG(t, 512)
	for _, tc := range []struct {
		name string
		status int
		contentType string
		data []byte
		want int
	}{
		{"missing", 404, "text/plain", nil, 404},
		{"forbidden", 403, "text/plain", nil, 403},
		{"throttled", 429, "text/plain", nil, 429},
		{"unavailable", 503, "text/plain", nil, 503},
		{"redirect", 302, "text/plain", nil, 502},
		{"unexpected 304", 304, "", nil, 502},
		{"wrong type", 200, "text/html", data, 502},
		{"corrupt", 200, "image/png", []byte("not a PNG"), 502},
		{"wrong size", 200, "image/png", wrongSize, 502},
		{"oversized", 200, "image/png", make([]byte, streetMaxBytes+1), 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set("Location", "https://redirect.invalid/")
				w.Header().Set("Retry-After", "5")
				w.Header().Set("Set-Cookie", "upstream=secret")
				w.WriteHeader(tc.status)
				w.Write(tc.data)
			}))
			defer up.Close()
			p := makeStreetProxy(up.URL, up.Client().Transport, time.Second)
			w := streetTestGet(p, "GET", streetTestPath, nil)
			if w.Code != tc.want || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" {
				t.Fatalf("upstream failure: %d %v", w.Code, w.Header())
			}
			if (tc.want == 429 || tc.want == 503) && w.Header().Get("Retry-After") != "5" {
				t.Fatal("Retry-After lost")
			}
		})
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer up.Close()
	p := makeStreetProxy(up.URL, up.Client().Transport, 30*time.Millisecond)
	if w := streetTestGet(p, "GET", streetTestPath, nil); w.Code != 504 || w.Header().Get("X-Sailing-Street-Result") != "upstream-timeout" {
		t.Fatalf("timeout: %d", w.Code)
	}
	p = makeStreetProxy(up.URL, up.Client().Transport, time.Second)
	p.queueWait = 10 * time.Millisecond
	for i := 0; i < cap(p.slots); i++ {
		p.slots <- struct{}{}
	}
	if w := streetTestGet(p, "GET", streetTestPath, nil); w.Code != 429 || w.Header().Get("X-Sailing-Street-Result") != "queue-timeout" {
		t.Fatalf("concurrency wait deadline: %d %v", w.Code, w.Header())
	}
	if len(p.waiters) != 0 || len(p.slots) != cap(p.slots) {
		t.Fatal("queue timeout leaked capacity")
	}
	p = makeStreetProxy(up.URL, up.Client().Transport, time.Second)
	p.tokens = -1
	if w := streetTestGet(p, "GET", streetTestPath, nil); w.Code != 429 {
		t.Fatalf("rate: %d", w.Code)
	}
	if w := streetTestGet(newStreetTileHandler("https://unapproved.invalid"), "GET", streetTestPath, nil); w.Code != 503 {
		t.Fatalf("unapproved provider: %d", w.Code)
	}
}

func TestStreetTileCachePolicyAndCapacity(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		cc string
		age string
		want time.Duration
		allowed bool
	}{
		{"max-age=60", "10", 50*time.Second, true},
		{"max-age=60, s-maxage=120", "10", 110*time.Second, true},
		{"no-cache, max-age=60", "", 0, true},
		{"no-store", "", 0, false},
		{"private, max-age=60", "", time.Minute, false},
		{"", "", 7*24*time.Hour, true},
	} {
		h := make(http.Header)
		h.Set("Cache-Control", tc.cc)
		h.Set("Age", tc.age)
		fresh, _, allowed := streetFreshness(h, now)
		if fresh.Sub(now) != tc.want || allowed != tc.allowed {
			t.Errorf("cache policy %q: %v %v", tc.cc, fresh.Sub(now), allowed)
		}
	}
	h := http.Header{"Vary": {"Referer"}, "Cache-Control": {"max-age=60"}}
	if _, _, allowed := streetFreshness(h, now); allowed {
		t.Fatal("cached variable Referer representation")
	}
	p := makeStreetProxy(streetOSM, nil, time.Second)
	data := make([]byte, streetMaxBytes)
	for i := 0; i < 40; i++ {
		p.store(&streetEntry{key: string(rune('a'+i)), data: data}, true)
	}
	if p.bytes > streetCacheBytes || p.lru.Len() != 32 {
		t.Fatalf("cache byte bound: %d, entries %d", p.bytes, p.lru.Len())
	}
	p = makeStreetProxy(streetOSM, nil, time.Second)
	for i := 0; i < streetCacheEntries+1; i++ {
		p.store(&streetEntry{key: string(rune(i)), data: []byte{1}}, true)
	}
	if p.lru.Len() != streetCacheEntries || p.lookup(string(rune(0))) != nil {
		t.Fatal("cache entry bound/LRU eviction failed")
	}
}

func TestStreetTileDiagnosticAdmissionReasons(t *testing.T) {
	for _, tc := range []struct {
		name string
		setup func(*streetProxy)
	}{
		{"queue-timeout", func(p *streetProxy) {
			p.queueWait = 10 * time.Millisecond
			for i := 0; i < cap(p.slots); i++ { p.slots <- struct{}{} }
		}},
		{"queue-full", func(p *streetProxy) {
			for i := 0; i < cap(p.slots); i++ { p.slots <- struct{}{} }
			for i := 0; i < cap(p.waiters); i++ { p.waiters <- struct{}{} }
		}},
		{"rate-limit", func(p *streetProxy) { p.tokens = -16 }},
		{"duplicate-inflight", func(p *streetProxy) { p.inflight["3/2/4.png"] = true }},
		{"rate-limit", func(p *streetProxy) { p.tokens = -16; p.inflight["3/2/4.png"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(500)
			}))
			defer up.Close()
			p := makeStreetProxy(up.URL, up.Client().Transport, time.Second)
			tc.setup(p)
			slots, waiters := len(p.slots), len(p.waiters)
			w := streetTestGet(p, "GET", streetTestPath, nil)
			if w.Code != 429 || w.Header().Get("Retry-After") != "1" ||
				w.Header().Get("Cache-Control") != "no-store" ||
				w.Header().Get("X-Sailing-Street-Source") != "proxy" ||
				w.Header().Get("X-Sailing-Street-Result") != tc.name ||
				calls.Load() != 0 || len(p.slots) != slots || len(p.waiters) != waiters {
				t.Fatalf("admission: status=%d headers=%v upstream=%d slots=%d", w.Code, w.Header(), calls.Load(), len(p.slots))
			}
		})
	}
}

func TestStreetTileDiagnosticUpstreamAndCache(t *testing.T) {
	data := streetTestPNG(t, 256)
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("X-Sailing-Street-Source", "spoofed")
		w.Header().Set("X-Sailing-Street-Result", "concurrency-limit")
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("ETag", "\"diag-v1\"")
		w.Write(data)
	}))
	defer up.Close()
	p := makeStreetProxy(up.URL, up.Client().Transport, time.Second)
	for _, tc := range []struct{
		status int
		source, result string
		headers map[string]string
	}{
		{429, "upstream", "upstream-429", nil},
		{200, "upstream", "upstream-tile", nil},
		{200, "cache", "cache-hit", nil},
		{304, "cache", "cache-hit", map[string]string{"If-None-Match": "\"diag-v1\""}},
	} {
		w := streetTestGet(p, "GET", streetTestPath, tc.headers)
		if w.Code != tc.status || w.Header().Get("X-Sailing-Street-Source") != tc.source ||
			w.Header().Get("X-Sailing-Street-Result") != tc.result {
			t.Fatalf("want %d %s/%s; got %d %v", tc.status, tc.source, tc.result, w.Code, w.Header())
		}
		if tc.status == 429 && (w.Header().Get("Retry-After") != "1" || w.Header().Get("Cache-Control") != "no-store") {
			t.Fatal("upstream error contract changed")
		}
	}
	if calls.Load() != 2 { t.Fatalf("upstream calls %d", calls.Load()) }
}
