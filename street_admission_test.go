package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise a burst beyond the initial token allowance against a mock only.
func TestStreetTileSustainedBurstRespectsRateAndConcurrency(t *testing.T) {
	data := streetTestPNG(t, 256)
	var calls, active, peak atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(data)
	}))
	defer up.Close()
	p := makeStreetProxy(up.URL, up.Client().Transport, time.Second)
	const total = 24
	results := make(chan *httptest.ResponseRecorder, total)
	started := time.Now()
	for i := 0; i < total; i++ {
		go func(i int) {
			results <- streetTestGet(p, "GET", fmt.Sprintf("%s5/%d/4.png", streetTilePrefix, i), nil)
		}(i)
	}
	timer := time.NewTimer(6*time.Second)
	defer timer.Stop()
	for i := 0; i < total; i++ {
		select {
		case w := <-results:
			if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) || w.Header().Get("X-Sailing-Street-Admission-Ms") == "" {
				t.Fatalf("sustained burst tile: %d %v", w.Code, w.Header())
			}
		case <-timer.C: t.Fatal("sustained burst did not drain")
		}
	}
	// At most 16 initial tokens; the other eight require about one second.
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond {
		t.Fatalf("rate budget bypassed: %v", elapsed)
	}
	if calls.Load() != total || peak.Load() > 8 || len(p.slots) != 0 || len(p.waiters) != 0 {
		t.Fatalf("sustained burst bounds: calls=%d peak=%d slots=%d waiters=%d", calls.Load(), peak.Load(), len(p.slots), len(p.waiters))
	}
}

func TestStreetTileRateWaitCancellationAndFreeSlots(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer up.Close()
	p := makeStreetProxy(up.URL, up.Client().Transport, time.Second)
	p.tokens = -16
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("GET", "https://weather.themauris.org"+streetTestPath, nil).WithContext(ctx)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		result <- w
	}()
	streetTestAwait(t, func() bool { return len(p.waiters) == 1 })
	if len(p.slots) != 0 || calls.Load() != 0 { t.Fatal("rate waiter reserved upstream capacity") }
	cancel()
	select {
	case w := <-result:
		if w.Code != 408 || w.Header().Get("X-Sailing-Street-Result") != "request-cancelled" { t.Fatalf("rate cancel: %d %v", w.Code, w.Header()) }
	case <-time.After(2*time.Second): t.Fatal("rate waiter did not cancel")
	}
	if len(p.waiters) != 0 || len(p.slots) != 0 || calls.Load() != 0 { t.Fatal("rate cancellation leaked capacity") }
}

func TestStreetTileRateWaitQueueFull(t *testing.T) {
	p := makeStreetProxy(streetOSM, nil, time.Second)
	p.tokens = -16
	for i := 0; i < cap(p.waiters); i++ { p.waiters <- struct{}{} }
	w := streetTestGet(p, "GET", streetTestPath, nil)
	if w.Code != 429 || w.Header().Get("X-Sailing-Street-Result") != "queue-full" ||
		w.Header().Get("Retry-After") != "1" || len(p.slots) != 0 || len(p.waiters) != cap(p.waiters) {
		t.Fatalf("rate queue full: %d %v", w.Code, w.Header())
	}
}
