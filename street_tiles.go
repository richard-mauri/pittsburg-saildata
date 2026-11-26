package main

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	streetTilePrefix = "/api/maps/street/"
	streetOSM = "https://tile.openstreetmap.org"
	streetMaxBytes = 1 << 20
	streetCacheBytes = 32 << 20
	streetCacheEntries = 512
	streetRate = 8.0
	streetBurst = 16.0
)

var streetPathPattern = regexp.MustCompile(`^/api/maps/street/([0-9]{1,9})/([0-9]{1,9})/([0-9]{1,9})\.png$`)

// Only this exact path shape is public. Encoded paths are excluded by the
// production auth guard, preventing ServeMux normalization from bypassing auth.
func streetTileRoute(path string) bool { return streetPathPattern.MatchString(path) }

func streetTileKey(path string) (string, bool) {
	p := streetPathPattern.FindStringSubmatch(path)
	if p == nil { return "", false }
	z, _ := strconv.Atoi(p[1])
	x, _ := strconv.Atoi(p[2])
	y, _ := strconv.Atoi(p[3])
	if z > 19 || x >= 1<<uint(z) || y >= 1<<uint(z) { return "", false }
	return strconv.Itoa(z)+"/"+strconv.Itoa(x)+"/"+strconv.Itoa(y)+".png", true
}

type streetEntry struct {
	key string
	data []byte
	header http.Header
	stored time.Time
	age time.Duration
	freshUntil time.Time
}

type streetProxy struct {
	upstream string
	client *http.Client
	slots chan struct{}
	mu sync.Mutex
	cache map[string]*list.Element
	lru *list.List
	bytes int
	tokens float64
	last time.Time
	inflight map[string]bool
}

// Configuration is operator-only and exact allowlisted, not a client URL.
// A future provider must be reviewed and explicitly added to the allowlist.
func newStreetTileHandler(config string) http.Handler {
	config = strings.TrimSpace(config)
	if config == "" { config = streetOSM }
	if config != streetOSM { return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		streetError(w, http.StatusServiceUnavailable)
	}) }
	transport := &http.Transport{
		DialContext: (&net.Dialer{Timeout: 5*time.Second, KeepAlive: 30*time.Second}).DialContext,
		ForceAttemptHTTP2: true, DisableCompression: true, MaxConnsPerHost: 8,
		MaxIdleConnsPerHost: 8, IdleConnTimeout: 60*time.Second, TLSHandshakeTimeout: 5*time.Second,
		ResponseHeaderTimeout: 8*time.Second, MaxResponseHeaderBytes: 32<<10,
	}
	return makeStreetProxy(config, transport, 10*time.Second)
}

// Tests inject a mock transport/upstream; production only calls the allowlisted constructor.
func makeStreetProxy(upstream string, transport http.RoundTripper, timeout time.Duration) *streetProxy {
	return &streetProxy{
		upstream: upstream,
		client: &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		slots: make(chan struct{}, 8), cache: make(map[string]*list.Element), lru: list.New(),
		tokens: streetBurst, last: time.Now(), inflight: make(map[string]bool),
	}
}

func streetError(w http.ResponseWriter, status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.Error(w, http.StatusText(status), status)
}

func (p *streetProxy) lookup(key string) *streetEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	if el := p.cache[key]; el != nil {
		p.lru.MoveToFront(el)
		return el.Value.(*streetEntry)
	}
	return nil
}

func (p *streetProxy) remove(key string) {
	if el := p.cache[key]; el != nil {
		p.bytes -= len(el.Value.(*streetEntry).data)
		p.lru.Remove(el)
		delete(p.cache, key)
	}
}

func (p *streetProxy) store(e *streetEntry, allowed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.remove(e.key)
	if !allowed { return }
	for p.lru.Len() >= streetCacheEntries || p.bytes+len(e.data) > streetCacheBytes {
		p.remove(p.lru.Back().Value.(*streetEntry).key)
	}
	p.cache[e.key] = p.lru.PushFront(e)
	p.bytes += len(e.data)
}

// Only upstream misses consume the global token bucket and concurrency slots.
// Concurrent requests for the same missing tile are rejected rather than
// spawning duplicate fetches or unbounded waiters.
func (p *streetProxy) acquire(key string) bool {
	select {
	case p.slots <- struct{}{}:
	default: return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.tokens += now.Sub(p.last).Seconds()*streetRate
	if p.tokens > streetBurst { p.tokens = streetBurst }
	p.last = now
	if p.tokens < 1 || p.inflight[key] {
		<-p.slots
		return false
	}
	p.tokens--
	p.inflight[key] = true
	return true
}

func (p *streetProxy) release(key string) {
	p.mu.Lock()
	delete(p.inflight, key)
	p.mu.Unlock()
	<-p.slots
}

var streetResponseHeaders = []string{"Cache-Control", "Expires", "ETag", "Last-Modified", "Date", "Vary", "Age"}

func streetHeaders(h http.Header) http.Header {
	out := make(http.Header)
	for _, name := range streetResponseHeaders {
		for _, value := range h.Values(name) { out.Add(name, value) }
	}
	return out
}

func streetAge(h http.Header, now time.Time) time.Duration {
	var age time.Duration
	if n, err := strconv.ParseInt(h.Get("Age"), 10, 32); err == nil && n >= 0 { age = time.Duration(n)*time.Second }
	if date, err := http.ParseTime(h.Get("Date")); err == nil && now.Sub(date) > age { age = now.Sub(date) }
	return age
}

func streetFreshness(h http.Header, now time.Time) (time.Time, time.Duration, bool) {
	age := streetAge(h, now)
	allowed := true
	revalidate := false
	var maxAge, sharedAge time.Duration = -1, -1
	for _, line := range h.Values("Cache-Control") {
		for _, raw := range strings.Split(line, ",") {
			kv := strings.SplitN(strings.TrimSpace(raw), "=", 2)
			switch strings.ToLower(kv[0]) {
			case "private", "no-store": allowed = false
			case "no-cache": revalidate = true
			case "max-age", "s-maxage":
				if len(kv) != 2 { revalidate = true; continue }
				n, err := strconv.ParseInt(strings.Trim(kv[1], "\""), 10, 32)
				if err != nil || n < 0 { revalidate = true; continue }
				if strings.EqualFold(kv[0], "s-maxage") { sharedAge = time.Duration(n)*time.Second } else { maxAge = time.Duration(n)*time.Second }
			}
		}
	}
	for _, line := range h.Values("Vary") {
		for _, v := range strings.Split(line, ",") {
			if v = strings.TrimSpace(v); v != "" && !strings.EqualFold(v, "Accept-Encoding") { allowed = false }
		}
	}
	lifetime := 7*24*time.Hour
	if sharedAge >= 0 { lifetime = sharedAge } else if maxAge >= 0 { lifetime = maxAge } else if expires := h.Get("Expires"); expires != "" {
		if t, err := http.ParseTime(expires); err == nil {
			date, err := http.ParseTime(h.Get("Date"))
			if err != nil { date = now }
			lifetime = t.Sub(date)
		} else { revalidate = true }
	} else if h.Get("Cache-Control") == "" {
		h.Set("Cache-Control", "public, max-age=604800")
	} else {
		// An explicit policy without a freshness lifetime is not permission to invent one.
		revalidate = true
	}
	if revalidate { lifetime = 0 }
	remaining := lifetime-age
	if remaining < 0 { remaining = 0 }
	return now.Add(remaining), age, allowed
}

func streetWrite(w http.ResponseWriter, r *http.Request, e *streetEntry) {
	for name, values := range e.header {
		for _, value := range values { w.Header().Add(name, value) }
	}
	age := e.age + time.Since(e.stored)
	w.Header().Set("Age", strconv.FormatInt(int64(age/time.Second), 10))
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	mod, _ := http.ParseTime(e.header.Get("Last-Modified"))
	// ServeContent evaluates client validators for both cache hits and upstream
	// responses. This API returns full tiles, never byte ranges.
	req := r.Clone(r.Context())
	req.Header.Del("Range")
	req.Header.Del("If-Range")
	http.ServeContent(w, req, e.key, mod, bytes.NewReader(e.data))
}

func streetReferer(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") { return "" }
	// Preserve the actual web origin, not private path/query/fragment data.
	return u.Scheme + "://" + u.Host + "/"
}

func streetTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

func (p *streetProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		streetError(w, http.StatusMethodNotAllowed)
		return
	}
	key, ok := streetTileKey(r.URL.Path)
	if !ok || r.URL.RawQuery != "" || r.URL.EscapedPath() != r.URL.Path {
		streetError(w, http.StatusBadRequest)
		return
	}
	cached := p.lookup(key)
	if cached != nil && time.Now().Before(cached.freshUntil) {
		streetWrite(w, r, cached)
		return
	}
	if !p.acquire(key) {
		w.Header().Set("Retry-After", "1")
		streetError(w, http.StatusTooManyRequests)
		return
	}
	defer p.release(key)
	// Recheck after acquiring to avoid re-fetching a just-completed tile.
	if e := p.lookup(key); e != nil {
		cached = e
		if time.Now().Before(e.freshUntil) { streetWrite(w, r, e); return }
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, p.upstream+"/"+key, nil)
	if err != nil { streetError(w, http.StatusBadGateway); return }
	req.Header.Set("User-Agent", "SailingGo-StreetTiles/"+appVersion+" (+https://github.com/richard-mauri/pittsburg-saildata)")
	req.Header.Set("Accept", "image/png")
	if ref := streetReferer(r.Referer()); ref != "" { req.Header.Set("Referer", ref) }
	// Deliberately do not forward cookies, authorization, Origin, arbitrary
	// headers, cache bypass directives, or the request query.
	if cached != nil {
		req.Header.Set("If-None-Match", cached.header.Get("ETag"))
		req.Header.Set("If-Modified-Since", cached.header.Get("Last-Modified"))
	} else {
		for _, k := range []string{"If-None-Match", "If-Modified-Since"} {
			if v := r.Header.Get(k); v != "" && len(v) <= 1024 { req.Header.Set(k, v) }
		}
	}
	resp, err := p.client.Do(req)
	if err != nil {
		if streetTimeout(err) { streetError(w, http.StatusGatewayTimeout) } else { streetError(w, http.StatusBadGateway) }
		return
	}
	defer resp.Body.Close()
	now := time.Now()
	h := streetHeaders(resp.Header)
	if resp.StatusCode == http.StatusNotModified {
		if req.Header.Get("If-None-Match") == "" && req.Header.Get("If-Modified-Since") == "" { streetError(w, http.StatusBadGateway); return }
		if cached == nil {
			for name, values := range h { for _, value := range values { w.Header().Add(name, value) } }
			w.WriteHeader(http.StatusNotModified)
			return
		}
		merged := cached.header.Clone()
		merged.Del("Age")
		merged.Del("Date")
		for name, values := range h { merged[name] = values }
		fresh, age, allowed := streetFreshness(merged, now)
		e := &streetEntry{key, cached.data, merged, now, age, fresh}
		p.store(e, allowed)
		streetWrite(w, r, e)
		return
	}
	if resp.StatusCode != http.StatusOK {
		status := resp.StatusCode
		if status < 400 || status > 599 { status = http.StatusBadGateway }
		if v := resp.Header.Get("Retry-After"); v != "" && (status == 429 || status == 503) { w.Header().Set("Retry-After", v) }
		streetError(w, status)
		return
	}
	typ, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || typ != "image/png" || resp.ContentLength > streetMaxBytes {
		streetError(w, http.StatusBadGateway); return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, streetMaxBytes+1))
	if err != nil {
		if streetTimeout(err) { streetError(w, http.StatusGatewayTimeout) } else { streetError(w, http.StatusBadGateway) }
		return
	}
	if len(data) > streetMaxBytes { streetError(w, http.StatusBadGateway); return }
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 256 || cfg.Height != 256 { streetError(w, http.StatusBadGateway); return }
	if _, err := png.Decode(bytes.NewReader(data)); err != nil { streetError(w, http.StatusBadGateway); return }
	fresh, age, allowed := streetFreshness(h, now)
	e := &streetEntry{key, data, h, now, age, fresh}
	p.store(e, allowed)
	streetWrite(w, r, e)
}
