package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const baseURL = "http://127.0.0.1:8080"

func waitFor(t *testing.T, timeout time.Duration, desc string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", desc)
}

func evalString(ctx context.Context, expr string) string {
	v, err := chromedp.Run(ctx, chromedp.Evaluate[string](expr))
	if err != nil {
		return ""
	}
	return v
}

func evalBool(ctx context.Context, expr string) bool {
	v, err := chromedp.Run(ctx, chromedp.Evaluate[bool](expr))
	if err != nil {
		return false
	}
	return v
}

func textOf(ctx context.Context, selector string) string {
	v, err := chromedp.Run(ctx, chromedp.Text(chromedp.CSS(selector)))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

func attrOf(ctx context.Context, selector, name string) string {
	r, err := chromedp.Run(ctx, chromedp.AttributeValue(chromedp.CSS(selector), name))
	if err != nil || !r.Exists {
		return ""
	}
	return r.Value
}

func mockRequestCount(ctx context.Context) int {
	v, err := chromedp.Run(ctx, chromedp.Evaluate[int](
		`Array.isArray(window.__swellMockRequests) ? window.__swellMockRequests.length : 0`,
	))
	if err != nil {
		return -1
	}
	return v
}

func mockHasHour(ctx context.Context, hour int) bool {
	expr := fmt.Sprintf(
		`Array.isArray(window.__swellMockRequests) && window.__swellMockRequests.some(r => r.hours === %d)`,
		hour,
	)
	return evalBool(ctx, expr)
}

func mockHourCount(ctx context.Context, hour int) int {
	expr := fmt.Sprintf(
		`Array.isArray(window.__swellMockRequests) ? window.__swellMockRequests.filter(r => r.hours === %d).length : 0`,
		hour,
	)
	v, err := chromedp.Run(ctx, chromedp.Evaluate[int](expr))
	if err != nil {
		return -1
	}
	return v
}

const installSwellMockJS = `
(() => {
	if (window.__swellMockInstalled) return;

	window.__swellMockInstalled = true;
	window.__swellMockRequests = [];
	window.__swellMockFailOnceHour = null;
	window.__swellMockFailedOnce = false;
	window.__swellMockFailHour = null;
	window.__swellMockFailRemaining = 0;
	window.__swellMockDelayHour = null;
	window.__swellMockDelayMS = 0;
	const realFetch = window.fetch.bind(window);

	window.fetch = async function(input, init) {
		const raw = (typeof input === "string") ? input : input.url;
		const u = new URL(raw, window.location.href);

		if (!u.pathname.endsWith("/swell-forecast")) {
			return realFetch(input, init);
		}

		const hours = Number(u.searchParams.get("hours") || "0");
		const west = Number(u.searchParams.get("west"));
		const south = Number(u.searchParams.get("south"));
		const east = Number(u.searchParams.get("east"));
		const north = Number(u.searchParams.get("north"));
		const stride = Number(u.searchParams.get("stride") || "1");
		const refresh = u.searchParams.get("refresh") === "1";
		const prime = u.searchParams.get("prime") === "1";

		window.__swellMockRequests.push({
			hours, west, south, east, north, stride, refresh, prime
		});

		if (Number(window.__swellMockFailHour) === hours && Number(window.__swellMockFailRemaining) > 0) {
			window.__swellMockFailRemaining = Number(window.__swellMockFailRemaining) - 1;
			return new Response("synthetic repeated transient swell failure", {
				status: 503,
				headers: {"Content-Type": "text/plain"}
			});
		}

		if (Number(window.__swellMockFailOnceHour) === hours && !window.__swellMockFailedOnce) {
			window.__swellMockFailedOnce = true;
			return new Response("synthetic transient swell failure", {
				status: 503,
				headers: {"Content-Type": "text/plain"}
			});
		}

		if (Number(window.__swellMockDelayHour) === hours && Number(window.__swellMockDelayMS) > 0) {
			await new Promise(resolve => setTimeout(resolve, Number(window.__swellMockDelayMS)));
		}

		const rows = 5;
		const cols = 5;
		const points = [];

		for (let y = 0; y < rows; y++) {
			const fy = y / (rows - 1);
			const lat = south + (north - south) * fy;

			for (let x = 0; x < cols; x++) {
				const fx = x / (cols - 1);
				const lon = west + (east - west) * fx;

				points.push({
					lat: lat,
					lon: lon,
					height_m: 1.2 + 0.06*x + 0.04*y + hours/240,
					period_s: 11 + 0.15*x,
					direction_deg: (245 + x*3 + y*2) % 360
				});
			}
		}

		const lonStep = Math.abs(east - west) / (cols - 1);
		const latStep = Math.abs(north - south) / (rows - 1);
		const gridStep = Math.max(lonStep, latStep, 0.01);
		const forecastTime = new Date(Date.UTC(2026, 9, 7, 12, 0, 0) + hours*3600000).toISOString();

		const payload = {
			forecast_time: forecastTime,
			hours_ahead: hours,
			grid_step_deg: gridStep,
			grid_stride: stride,
			points: points,
			source: "chromedp deterministic swell mock",
			note: "UI regression test payload"
		};

		return new Response(JSON.stringify(payload), {
			status: 200,
			headers: {
				"Content-Type": "application/json",
				"Cache-Control": "no-store"
			}
		});
	};
})();
`

func TestSwellPlaybackSmoke(t *testing.T) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	// Start Chrome and load the report before applying a test timeout.
	// Current chromedp documentation recommends not timing out the first run,
	// because that first run owns the browser lifetime.
	target := baseURL + "/report?format=html&station=PSBC1&planning=1&lat=38.0400&lon=-121.8870&map_zoom=6"
	if err := chromedp.Do(ctx,
		chromedp.Navigate(target),
		chromedp.WaitVisible(chromedp.CSS(`#map-overlays-menu`)),
	); err != nil {
		t.Fatalf("open local sailing report: %v", err)
	}

	testCtx, testCancel := context.WithTimeout(ctx, 45*time.Second)
	defer testCancel()

	// Install the deterministic fetch mock after page initialization but before
	// enabling swell. v338 does not request /swell-forecast until swell is on.
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](installSwellMockJS),
	); err != nil {
		t.Fatalf("install swell mock: %v", err)
	}

	if evalBool(testCtx, `document.querySelector("#map-show-swell")?.checked === true`) {
		t.Fatal("swell unexpectedly enabled at test start")
	}

	// Enable Global Swell Forecast through the real UI. Force both request-level
	// attempts for the initial Now frame to fail so v364 must exercise its one
	// enable-time preparation retry. Delay +3h so readiness cannot complete until
	// the full default 3-hour playback sequence has really been primed.
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			window.__swellMockFailHour = 0;
			window.__swellMockFailRemaining = 2;
			window.__swellMockDelayHour = 3;
			window.__swellMockDelayMS = 900;
			const menu = document.querySelector("#map-overlays-menu");
			const swell = document.querySelector("#map-show-swell");
			if (!menu || !swell) throw new Error("swell overlay controls not found");
			menu.open = true;
			const group = swell.closest("details.map-overlay-group");
			if (group) group.open = true;
		})()`),
		chromedp.Click(chromedp.ID(`map-show-swell`)),
		chromedp.WaitVisible(chromedp.ID(`map-swell-transport`)),
	); err != nil {
		t.Fatalf("enable swell overlay: %v", err)
	}

	waitFor(t, 2*time.Second, "initial swell current-frame preparation message", func() bool {
		return strings.Contains(textOf(testCtx, `#map-swell-prime-status`), "Loading current swell frame")
	})

	waitFor(t, 5*time.Second, "enable-time transient retry message", func() bool {
		return strings.Contains(textOf(testCtx, `#map-swell-prime-status`), "Temporary upstream error")
	})

	waitFor(t, 8*time.Second, "initial Now swell request eventually succeeds after preparation retry", func() bool {
		return mockHourCount(testCtx, 0) >= 3
	})
	waitFor(t, 5*time.Second, "default +3h playback cache prime request", func() bool {
		return mockHasHour(testCtx, 3)
	})
	if got := strings.TrimSpace(textOf(testCtx, `#map-swell-cache-reload`)); got != "Reload swell cache" {
		t.Fatalf("automatic cache preparation mislabeled Reload button as %q", got)
	}
	if got := textOf(testCtx, `#map-swell-prime-status`); got == "Swell ready." {
		t.Fatal("swell reported ready before delayed +3h playback-cache frame completed")
	}
	if got := textOf(testCtx, `#map-swell-prime-status`); !strings.Contains(strings.ToLower(got), "playback cache") {
		t.Fatalf("swell did not expose playback-cache preparation stage; got %q", got)
	}
	waitFor(t, 8*time.Second, "complete default 3h playback cache sequence", func() bool {
		return evalBool(testCtx, `(() => {
			for (let h = 3; h <= 120; h += 3) {
				if (!window.__swellMockRequests.some(r => r.hours === h && r.prime === true)) return false;
			}
			return true;
		})()`)
	})
	waitFor(t, 5*time.Second, "swell preparation ready message", func() bool {
		return textOf(testCtx, `#map-swell-prime-status`) == "Swell ready."
	})
	if evalBool(testCtx, `document.querySelector("#map-swell-cache-reload")?.hidden === true`) {
		t.Fatal("Reload swell cache button is hidden after preparation")
	}
	if evalBool(testCtx, `document.querySelector("#map-swell-cache-reload")?.disabled === true`) {
		t.Fatal("Reload swell cache button remains disabled after preparation")
	}

	// v364 regression: the Reload button listener must actually be bound after the
	// button element is assigned. Clicking it must enter loading-current, send a
	// forced refresh request, and return to ready after playback priming.
	beforeReloadNow := mockHourCount(testCtx, 0)
	if got := attrOf(testCtx, `#map-swell-cache-reload`, "data-preparation-state"); got != "ready" {
		t.Fatalf("reload button state before manual reload = %q; want ready", got)
	}
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`window.__swellMockDelayHour = 0; window.__swellMockDelayMS = 700;`),
		chromedp.Evaluate[chromedp.Void](`document.querySelector("#map-swell-cache-reload").click()`),
	); err != nil {
		t.Fatalf("invoke Reload swell cache: %v", err)
	}
	waitFor(t, 2*time.Second, "reload swell cache loading-current state", func() bool {
		return attrOf(testCtx, `#map-swell-cache-reload`, "data-preparation-state") == "loading-current" &&
			evalBool(testCtx, `document.querySelector("#map-swell-cache-reload")?.disabled === true`) &&
			strings.Contains(textOf(testCtx, `#map-swell-prime-status`), "Loading current swell frame")
	})
	waitFor(t, 8*time.Second, "reload current swell request", func() bool {
		return mockHourCount(testCtx, 0) > beforeReloadNow
	})
	if !evalBool(testCtx, `window.__swellMockRequests.some(r => r.hours === 0 && r.refresh === true)`) {
		t.Fatal("manual Reload swell cache did not refresh the current frame")
	}
	waitFor(t, 8*time.Second, "manual reload complete 3h playback cache refresh", func() bool {
		return evalBool(testCtx, `(() => {
			for (let h = 3; h <= 120; h += 3) {
				if (!window.__swellMockRequests.some(r => r.hours === h && r.prime === true && r.refresh === true)) return false;
			}
			return true;
		})()`)
	})
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`window.__swellMockDelayHour = null; window.__swellMockDelayMS = 0;`),
	); err != nil {
		t.Fatalf("clear reload swell mock delay: %v", err)
	}
	waitFor(t, 8*time.Second, "reload swell cache ready state", func() bool {
		return textOf(testCtx, `#map-swell-prime-status`) == "Swell ready." &&
			attrOf(testCtx, `#map-swell-cache-reload`, "data-preparation-state") == "ready" &&
			!evalBool(testCtx, `document.querySelector("#map-swell-cache-reload")?.disabled === true`)
	})
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`window.__swellMockFailHour = null; window.__swellMockFailRemaining = 0; window.__swellMockDelayHour = null; window.__swellMockDelayMS = 0;`),
	); err != nil {
		t.Fatalf("clear initial swell mock delay: %v", err)
	}

	if got := textOf(testCtx, `#map-swell-map-time`); !strings.Contains(got, "Swell · Now") {
		t.Fatalf("initial swell transport time = %q; want Swell · Now context", got)
	}

	if got := attrOf(testCtx, `#map-swell-map-play`, "aria-pressed"); got != "false" {
		t.Fatalf("initial Play aria-pressed = %q; want false", got)
	}
	if evalBool(testCtx, `document.querySelector("#map-swell-hours") !== null`) {
		t.Fatal("redundant Map Overlays swell slider still exists")
	}
	if got := attrOf(testCtx, `#map-swell-map-hours`, "step"); got != "1" {
		t.Fatalf("map swell slider step = %q; want 1", got)
	}
	if got := evalString(testCtx, `document.querySelector("#map-swell-playback-step")?.value || ""`); got != "3" {
		t.Fatalf("default swell playback step = %q; want 3", got)
	}
	if got := evalString(testCtx, `document.querySelector("#map-swell-playback-speed")?.value || ""`); got != "1" {
		t.Fatalf("default swell playback speed = %q; want 1", got)
	}

	// The overlay menu was opened above so the test could enable swell through
	// the real checkbox. Close it before exercising the on-map transport; leaving
	// it open can physically cover the transport in the headless viewport, causing
	// a trusted mouse click to land on the menu instead of the Play button.
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			const menu = document.querySelector("#map-overlays-menu");
			if (menu) menu.open = false;
		})()`),
	); err != nil {
		t.Fatalf("close overlay menu before transport test: %v", err)
	}

	// v364 standardizes both playback overlays below the map. Temperature and
	// swell live as separately identified rows in the shared footer transport
	// stack, while the map scale remains outside that stack at the right.
	if !evalBool(testCtx, `document.querySelector(".map-footer-transports > #map-air-temperature-transport") !== null`) {
		t.Fatal("air-temperature transport is not a direct child of the below-map transport stack")
	}
	if evalBool(testCtx, `document.querySelector(".location-map-wrap #map-air-temperature-transport") !== null`) {
		t.Fatal("air-temperature transport still lives inside the map viewport")
	}
	if evalBool(testCtx, `document.querySelector(".location-map-wrap #map-air-temperature-time") !== null`) {
		t.Fatal("air-temperature status still lives inside the map viewport")
	}
	if !evalBool(testCtx, `document.querySelector("#map-air-temperature-transport > #map-air-temperature-time") !== null`) {
		t.Fatal("air-temperature status is not the single footer status row")
	}
	if evalBool(testCtx, `document.querySelector(".map-air-temperature-transport-row #map-air-temperature-time") !== null`) {
		t.Fatal("air-temperature dynamic status is still inside the control row")
	}
	if evalBool(testCtx, `document.querySelector("#map-air-temperature-map-selected") !== null`) {
		t.Fatal("redundant air-temperature Selected status still exists")
	}
	if !evalBool(testCtx, `(() => {
		const row = document.querySelector("#map-air-temperature-transport .map-air-temperature-transport-row");
		if (!row) return false;
		return !row.textContent.includes("Air Temperature");
	})()`) {
		t.Fatal("variable air-temperature status still occupies the control row")
	}
	if evalBool(testCtx, `document.querySelector("#map-air-temperature-hours") !== null`) {
		t.Fatal("redundant Map Overlays air-temperature slider still exists")
	}
	if evalBool(testCtx, `document.querySelector("#map-air-temperature-play") !== null`) {
		t.Fatal("redundant Map Overlays air-temperature Play control still exists")
	}
	if !evalBool(testCtx, `document.querySelector("#map-air-temperature-map-hours") !== null`) {
		t.Fatal("below-map air-temperature timeline slider is missing")
	}
	if got := attrOf(testCtx, `#map-air-temperature-map-hours`, "step"); got != "0.25" {
		t.Fatalf("air-temperature footer slider step = %q; want 0.25", got)
	}
	if got := attrOf(testCtx, `#map-air-temperature-map-hours`, "max"); got != "24" {
		t.Fatalf("air-temperature footer slider max = %q; want 24", got)
	}
	if got := strings.TrimSpace(evalString(testCtx, `document.querySelector("#map-air-temperature-map-range-end")?.textContent || ""`)); got != "+24h" {
		t.Fatalf("air-temperature footer range endpoint = %q; want +24h", got)
	}
	if !evalBool(testCtx, `(() => {
		const transport = document.querySelector("#map-swell-transport");
		if (!transport) return false;
		const h = Math.round(transport.getBoundingClientRect().height);
		return h >= 120 && h <= 136;
	})()`) {
		t.Fatalf("swell transport height is not compact/fixed; got %s px",
			evalString(testCtx, `String(Math.round(document.querySelector("#map-swell-transport")?.getBoundingClientRect().height || 0))`))
	}
	if evalBool(testCtx, `document.querySelector("#map-swell-transport .map-swell-transport-row #map-swell-map-time") !== null`) {
		t.Fatal("swell dynamic status still occupies the control row")
	}
	if !evalBool(testCtx, `document.querySelector("#map-swell-transport > #map-swell-map-time") !== null`) {
		t.Fatal("swell canonical status is not below the slider")
	}
	if !evalBool(testCtx, `document.querySelector("#map-swell-transport .map-swell-cache-row > #map-swell-prime-status") !== null`) {
		t.Fatal("swell cache progress is not in the below-map control area")
	}
	if !evalBool(testCtx, `document.querySelector("#map-swell-transport .map-swell-cache-row > #map-swell-cache-reload") !== null`) {
		t.Fatal("Reload swell cache button is not in the below-map control area")
	}
	if evalBool(testCtx, `document.querySelector("#map-overlays-menu #map-swell-prime-status, #map-overlays-menu #map-swell-cache-reload") !== null`) {
		t.Fatal("swell cache progress/reload still lives in Map Overlays")
	}
	if !evalBool(testCtx, `(() => {
		const legacy = document.querySelector("#map-swell-status");
		return !!legacy && legacy.hidden === true && getComputedStyle(legacy).display === "none";
	})()`) {
		t.Fatal("legacy lower-page swell status is still visible")
	}

	// The swell row remains below the map with its hourly scrubber and Step selector.
	if !evalBool(testCtx, `document.querySelector(".map-footer-transports > #map-swell-transport") !== null`) {
		t.Fatal("swell transport is not a direct child of the below-map transport stack")
	}
	if evalBool(testCtx, `document.querySelector(".location-map-wrap #map-swell-transport") !== null`) {
		t.Fatal("swell transport still lives inside the map viewport")
	}
	if evalBool(testCtx, `getComputedStyle(document.querySelector("#map-swell-map-hours")).display === "none"`) {
		t.Fatal("hourly swell scrubber is hidden")
	}
	if evalBool(testCtx, `getComputedStyle(document.querySelector("#map-swell-playback-step")).display === "none"`) {
		t.Fatal("swell playback Step selector is hidden")
	}
	if got := textOf(testCtx, `#map-swell-map-time`); !strings.Contains(got, "Swell · Now") {
		t.Fatalf("initial swell footer status = %q; want Swell · Now context", got)
	}
	if evalBool(testCtx, `document.querySelector("#map-swell-transport .map-swell-transport-row #map-swell-map-time") !== null`) {
		t.Fatal("swell dynamic status is still inside the control row")
	}
	if !evalBool(testCtx, `(() => {
		const map = document.querySelector("#sailing-location-map");
		const transport = document.querySelector("#map-swell-transport");
		if (!map || !transport) return false;
		return transport.getBoundingClientRect().top >= map.getBoundingClientRect().bottom - 1;
	})()`) {
		t.Fatal("swell transport overlaps the map viewport")
	}
	// Temperature Range must immediately redefine the footer slider scale.
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			const range = document.querySelector("#map-air-temperature-range");
			if (!range) throw new Error("temperature Range control missing");
			range.value = "12";
			range.dispatchEvent(new Event("change", {bubbles:true}));
		})()`),
	); err != nil {
		t.Fatalf("set temperature Range to 12h: %v", err)
	}
	waitFor(t, 2*time.Second, "temperature footer slider 12h scale", func() bool {
		return attrOf(testCtx, `#map-air-temperature-map-hours`, "max") == "12" &&
			strings.TrimSpace(evalString(testCtx, `document.querySelector("#map-air-temperature-map-range-end")?.textContent || ""`)) == "+12h"
	})
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			const range = document.querySelector("#map-air-temperature-range");
			if (range) { range.value = "24"; range.dispatchEvent(new Event("change", {bubbles:true})); }
		})()`),
	); err != nil {
		t.Fatalf("restore temperature Range to 24h: %v", err)
	}

	initialTransportHeight := evalString(testCtx, `String(Math.round(document.querySelector("#map-swell-transport").getBoundingClientRect().height))`)

	// v364 regression: changing Step must preserve the newly selected value before stopSwellPlayback() refreshes the transport.
	// The test asserts the 1h selection sticks, then verifies durable cache/readiness outcomes.
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			window.__swellMockDelayHour = 61;
			window.__swellMockDelayMS = 900;
			const step = document.querySelector("#map-swell-playback-step");
			if (step) { step.value = "1"; step.dispatchEvent(new Event("change", {bubbles:true})); }
		})()`),
	); err != nil {
		t.Fatalf("switch to 1h cache: %v", err)
	}
	if got := evalString(testCtx, `document.querySelector("#map-swell-playback-step")?.value || ""`); got != "1" {
		t.Fatalf("1h step selection did not stick; got %q", got)
	}
	waitFor(t, 10*time.Second, "complete 1h cache prime sequence", func() bool {
		return evalBool(testCtx, `(() => {
			for (let h = 1; h <= 120; h += 1) {
				if (!window.__swellMockRequests.some(r => r.hours === h && r.prime === true)) return false;
			}
			return true;
		})()`)
	})
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`window.__swellMockDelayHour = null; window.__swellMockDelayMS = 0;`),
	); err != nil {
		t.Fatalf("clear 1h cache-progress delay: %v", err)
	}
	waitFor(t, 10*time.Second, "complete 1h cache ready", func() bool {
		return textOf(testCtx, `#map-swell-prime-status`) == "Swell ready." &&
			!evalBool(testCtx, `document.querySelector("#map-swell-map-play")?.disabled === true`)
	})
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`window.__swellMockDelayHour = 1; window.__swellMockDelayMS = 900;`),
	); err != nil {
		t.Fatalf("arm manual single-flight mock: %v", err)
	}
	beforeManual1 := mockHourCount(testCtx, 1)
	if err := chromedp.Do(testCtx, chromedp.Click(chromedp.ID(`map-swell-map-next`))); err != nil {
		t.Fatalf("start delayed manual Next: %v", err)
	}
	waitFor(t, 3*time.Second, "manual loading lock", func() bool {
		return mockHourCount(testCtx, 1) > beforeManual1 &&
			evalBool(testCtx, `document.querySelector("#map-swell-map-next")?.disabled === true`) &&
			evalBool(testCtx, `document.querySelector("#map-swell-map-prev")?.disabled === true`) &&
			evalBool(testCtx, `document.querySelector("#map-swell-map-now")?.disabled === true`) &&
			strings.Contains(textOf(testCtx, `#map-swell-map-time`), "Loading +1h")
	})
	busyTransportHeight := evalString(testCtx, `String(Math.round(document.querySelector("#map-swell-transport").getBoundingClientRect().height))`)
	if busyTransportHeight != initialTransportHeight {
		t.Fatalf("swell transport height changed while loading: idle=%s busy=%s", initialTransportHeight, busyTransportHeight)
	}
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			const b = document.querySelector("#map-swell-map-next");
			if (b) { b.click(); b.click(); b.click(); }
		})()`),
		chromedp.Sleep(250*time.Millisecond),
	); err != nil {
		t.Fatalf("attempt repeated manual Next clicks: %v", err)
	}
	if got := mockHourCount(testCtx, 1); got != beforeManual1+1 {
		t.Fatalf("manual single-flight issued parallel +1h requests: got %d, want %d", got, beforeManual1+1)
	}
	waitFor(t, 5*time.Second, "manual +1h load completes", func() bool {
		return !evalBool(testCtx, `document.querySelector("#map-swell-map-next")?.disabled === true`) &&
			strings.Contains(textOf(testCtx, `#map-swell-map-time`), "Swell · +1h")
	})
	if got := evalString(testCtx, `document.querySelector("#map-swell-map-hours")?.value || ""`); got != "1" {
		t.Fatalf("always-visible swell scrubber value after manual Next = %q; want 1", got)
	}

	// Return to Now and restore the normal 3-hour playback step for the existing
	// transport tests.
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`window.__swellMockDelayHour = null; window.__swellMockDelayMS = 0;`),
		chromedp.Click(chromedp.ID(`map-swell-map-now`)),
	); err != nil {
		t.Fatalf("return to Now after manual single-flight test: %v", err)
	}
	waitFor(t, 5*time.Second, "manual Now reload completes", func() bool {
		return strings.Contains(textOf(testCtx, `#map-swell-map-time`), "Swell · Now") &&
			!evalBool(testCtx, `document.querySelector("#map-swell-map-now")?.disabled === true`)
	})
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			const step = document.querySelector("#map-swell-playback-step");
			if (step) { step.value = "3"; step.dispatchEvent(new Event("change", {bubbles:true})); }
		})()`),
	); err != nil {
		t.Fatalf("restore 3h playback step: %v", err)
	}
	waitFor(t, 10*time.Second, "restored 3h cache ready", func() bool {
		return textOf(testCtx, `#map-swell-prime-status`) == "Swell ready." &&
			!evalBool(testCtx, `document.querySelector("#map-swell-map-play")?.disabled === true`)
	})

	// v364 regression: a failed cache frame must identify the missing hour and
	// Reload must retry only that missing frame instead of rebuilding the full sequence.
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			window.__swellMockFailHour = 60;
			window.__swellMockFailRemaining = 2;
			const step = document.querySelector("#map-swell-playback-step");
			if (step) { step.value = "6"; step.dispatchEvent(new Event("change", {bubbles:true})); }
		})()`),
	); err != nil {
		t.Fatalf("select 6h playback step with synthetic failure: %v", err)
	}
	if got := evalString(testCtx, `document.querySelector("#map-swell-playback-step")?.value || ""`); got != "6" {
		t.Fatalf("6h step selection did not stick; got %q", got)
	}
	waitFor(t, 10*time.Second, "6h cache incomplete identifies +60h", func() bool {
		status := textOf(testCtx, `#map-swell-prime-status`)
		return strings.Contains(status, "Playback cache incomplete") &&
			strings.Contains(status, "+60h") &&
			attrOf(testCtx, `#map-swell-cache-reload`, "data-preparation-state") == "error"
	})

	beforeRetry60 := mockHourCount(testCtx, 60)
	beforeRetry6 := mockHourCount(testCtx, 6)
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`window.__swellMockFailHour = null; window.__swellMockFailRemaining = 0;`),
		chromedp.Evaluate[chromedp.Void](`document.querySelector("#map-swell-cache-reload").click()`),
	); err != nil {
		t.Fatalf("retry missing 6h cache frame: %v", err)
	}
	// Do not assert the short-lived "Retrying ..." label. Under the browser mock,
	// a single missing frame can complete before polling observes that transient UI.
	// Durable proof is: +60h is requested again, +6h is not, and readiness returns.
	waitFor(t, 10*time.Second, "missing +60h frame retried", func() bool {
		return mockHourCount(testCtx, 60) > beforeRetry60
	})
	waitFor(t, 10*time.Second, "missing-only 6h retry returns ready", func() bool {
		return textOf(testCtx, `#map-swell-prime-status`) == "Swell ready." &&
			!evalBool(testCtx, `document.querySelector("#map-swell-map-play")?.disabled === true`)
	})
	if got := mockHourCount(testCtx, 60); got <= beforeRetry60 {
		t.Fatalf("missing +60h frame was not retried; count %d -> %d", beforeRetry60, got)
	}
	if got := mockHourCount(testCtx, 6); got != beforeRetry6 {
		t.Fatalf("missing-only retry rebuilt an already cached +6h frame: count %d -> %d", beforeRetry6, got)
	}

	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			const step = document.querySelector("#map-swell-playback-step");
			if (step) { step.value = "3"; step.dispatchEvent(new Event("change", {bubbles:true})); }
		})()`),
	); err != nil {
		t.Fatalf("restore 3h playback step after 6h retry regression: %v", err)
	}
	waitFor(t, 10*time.Second, "3h cache ready after 6h retry regression", func() bool {
		return textOf(testCtx, `#map-swell-prime-status`) == "Swell ready." &&
			!evalBool(testCtx, `document.querySelector("#map-swell-map-play")?.disabled === true`)
	})

	for _, id := range []string{"map-swell-map-now", "map-swell-map-prev", "map-swell-map-next"} {
		if !evalBool(testCtx, fmt.Sprintf(`document.querySelector("#%s") !== null`, id)) {
			t.Fatalf("missing swell transport control #%s", id)
		}
	}

	// Start playback. The test URL pins the selected map point and Zoom 6 so
	// playback eligibility is deterministic and cannot be changed by candidate-
	// station fitBounds behavior.
	if disabled := evalBool(testCtx, `document.querySelector("#map-swell-map-play")?.disabled === true`); disabled {
		zoom := textOf(testCtx, `#map-scale-status`)
		note := textOf(testCtx, `#map-swell-playback-note`)
		t.Fatalf("swell Play button disabled before playback; map=%q note=%q", zoom, note)
	}

	// v364 has already primed +3h. Capture the baseline so the test proves that
	// pressing Play actually advances/request-serves the +3h frame rather than
	// mistaking the earlier cache-prime request for playback.
	beforePlay3 := mockHourCount(testCtx, 3)

	// Instrument the click path so a failure tells us whether the browser click
	// reached the button and whether playback advanced even if aria-pressed was
	// transient.
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			window.__swellPlayClickSeen = 0;
			const b = document.querySelector("#map-swell-map-play");
			if (b) b.addEventListener("click", () => { window.__swellPlayClickSeen++; }, {capture:true});
		})()`),
		chromedp.Click(chromedp.ID(`map-swell-map-play`)),
	); err != nil {
		t.Fatalf("start swell playback: %v", err)
	}

	// Poll without calling t.Fatal so diagnostics are always printed on failure.
	playObserved := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if attrOf(testCtx, `#map-swell-map-play`, "aria-pressed") == "true" ||
			mockHourCount(testCtx, 3) > beforePlay3 {
			playObserved = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	pressed := attrOf(testCtx, `#map-swell-map-play`, "aria-pressed")
	clickSeen := evalString(testCtx, `String(window.__swellPlayClickSeen || 0)`)
	status := evalString(testCtx, `document.querySelector("#map-swell-prime-status")?.textContent?.trim() || ""`)
	note := evalString(testCtx, `document.querySelector("#map-swell-playback-note")?.textContent?.trim() || ""`)
	buttonText := evalString(testCtx, `document.querySelector("#map-swell-map-play")?.textContent?.trim() || ""`)
	disabled := evalString(testCtx, `String(!!document.querySelector("#map-swell-map-play")?.disabled)`)
	topElement := evalString(testCtx, `(() => {
		const b = document.querySelector("#map-swell-map-play");
		if (!b) return "missing-button";
		const r = b.getBoundingClientRect();
		const e = document.elementFromPoint(r.left + r.width/2, r.top + r.height/2);
		if (!e) return "none";
		return e.id || e.tagName || "unknown";
	})()`)
	t.Logf("play diagnostic: clickSeen=%s aria=%q button=%q disabled=%s topElement=%q status=%q note=%q +3req=%v",
		clickSeen, pressed, buttonText, disabled, topElement, status, note, mockHasHour(testCtx, 3))

	if !playObserved {
		t.Fatalf("Play click reached no observable playback state")
	}

	// v342 retains 3-hour playback steps at normal regional zooms.
	waitFor(t, 10*time.Second, "+3h playback request", func() bool {
		return mockHourCount(testCtx, 3) > beforePlay3
	})
	if !evalBool(testCtx, `(() => {
		const rows = window.__swellMockRequests.filter(r => r.hours === 3);
		return rows.length > 0 && rows[rows.length - 1].refresh === false;
	})()`) {
		t.Fatal("playback +3h request incorrectly bypassed the completed swell cache")
	}
	waitFor(t, 3*time.Second, "+3h UI state", func() bool {
		return evalString(testCtx, `document.querySelector("#map-swell-map-hours")?.value || ""`) == "3" &&
			strings.Contains(textOf(testCtx, `#map-swell-map-time`), "Swell · +3h")
	})

	waitFor(t, 10*time.Second, "+6h request", func() bool {
		return mockHasHour(testCtx, 6)
	})
	waitFor(t, 3*time.Second, "+6h UI state", func() bool {
		return evalString(testCtx, `document.querySelector("#map-swell-map-hours")?.value || ""`) == "6" &&
			strings.Contains(textOf(testCtx, `#map-swell-map-time`), "Swell · +6h")
	})

	// Pause and prove no more automatic swell requests are scheduled.
	if err := chromedp.Do(testCtx,
		chromedp.Click(chromedp.ID(`map-swell-map-play`)),
	); err != nil {
		t.Fatalf("pause swell playback: %v", err)
	}

	waitFor(t, 2*time.Second, "Pause state", func() bool {
		return attrOf(testCtx, `#map-swell-map-play`, "aria-pressed") == "false"
	})

	countAtPause := mockRequestCount(testCtx)
	time.Sleep(1250 * time.Millisecond)
	if got := mockRequestCount(testCtx); got != countAtPause {
		t.Fatalf("playback continued after Pause: request count %d -> %d", countAtPause, got)
	}

	// Select 1-hour playback. v364 exposes cache progress directly below the map
	// and keeps Play disabled until all 121 hourly frames are cached.
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			const e = document.querySelector("#map-swell-playback-step");
			e.value = "1";
			e.dispatchEvent(new Event("change", {bubbles:true}));
		})()`),
	); err != nil {
		t.Fatalf("select 1h playback step: %v", err)
	}
	waitFor(t, 10*time.Second, "hourly cache ready before playback", func() bool {
		return textOf(testCtx, `#map-swell-prime-status`) == "Swell ready." &&
			!evalBool(testCtx, `document.querySelector("#map-swell-map-play")?.disabled === true`)
	})
	if err := chromedp.Do(testCtx, chromedp.Click(chromedp.ID(`map-swell-map-play`))); err != nil {
		t.Fatalf("restart swell playback at 1h step: %v", err)
	}

	waitFor(t, 2*time.Second, "restarted playback state", func() bool {
		return attrOf(testCtx, `#map-swell-map-play`, "aria-pressed") == "true"
	})
	waitFor(t, 8*time.Second, "+7h hourly playback request", func() bool {
		return mockHasHour(testCtx, 7)
	})
	waitFor(t, 2*time.Second, "+7h hourly playback UI", func() bool {
		return evalString(testCtx, `document.querySelector("#map-swell-map-hours")?.value || ""`) == "7" &&
			strings.Contains(textOf(testCtx, `#map-swell-map-time`), "Swell · +7h")
	})

	if err := chromedp.Do(testCtx, chromedp.Click(chromedp.ID(`map-swell-map-play`))); err != nil {
		t.Fatalf("pause after hourly playback: %v", err)
	}
	waitFor(t, 2*time.Second, "pause after hourly playback", func() bool {
		return attrOf(testCtx, `#map-swell-map-play`, "aria-pressed") == "false"
	})

	// v342 retains temperature-style NOW / previous / next controls and playback speed.
	// With the step selector still at 1h, NOW should return to hour 0, Next should
	// advance to +1h, and Previous should return to Now.
	beforeNow := mockHourCount(testCtx, 0)
	if err := chromedp.Do(testCtx, chromedp.Click(chromedp.ID(`map-swell-map-now`))); err != nil {
		t.Fatalf("swell NOW click: %v", err)
	}
	waitFor(t, 8*time.Second, "NOW control request", func() bool {
		return mockHourCount(testCtx, 0) > beforeNow
	})
	waitFor(t, 5*time.Second, "NOW control load completes", func() bool {
		return evalString(testCtx, `document.querySelector("#map-swell-map-hours")?.value || ""`) == "0" &&
			strings.Contains(textOf(testCtx, `#map-swell-map-time`), "Swell · Now") &&
			!evalBool(testCtx, `document.querySelector("#map-swell-map-next")?.disabled === true`)
	})

	beforeNext := mockHourCount(testCtx, 1)
	if err := chromedp.Do(testCtx, chromedp.Click(chromedp.ID(`map-swell-map-next`))); err != nil {
		t.Fatalf("swell Next click: %v", err)
	}
	waitFor(t, 8*time.Second, "Next control +1h request", func() bool {
		return mockHourCount(testCtx, 1) > beforeNext
	})
	waitFor(t, 5*time.Second, "Next control +1h load completes", func() bool {
		return evalString(testCtx, `document.querySelector("#map-swell-map-hours")?.value || ""`) == "1" &&
			strings.Contains(textOf(testCtx, `#map-swell-map-time`), "Swell · +1h") &&
			!evalBool(testCtx, `document.querySelector("#map-swell-map-prev")?.disabled === true`)
	})

	beforePrev := mockHourCount(testCtx, 0)
	if err := chromedp.Do(testCtx, chromedp.Click(chromedp.ID(`map-swell-map-prev`))); err != nil {
		t.Fatalf("swell Previous click: %v", err)
	}
	waitFor(t, 8*time.Second, "Previous control Now request", func() bool {
		return mockHourCount(testCtx, 0) > beforePrev
	})
	waitFor(t, 5*time.Second, "Previous control Now load completes", func() bool {
		return evalString(testCtx, `document.querySelector("#map-swell-map-hours")?.value || ""`) == "0" &&
			strings.Contains(textOf(testCtx, `#map-swell-map-time`), "Swell · Now") &&
			!evalBool(testCtx, `document.querySelector("#map-swell-map-next")?.disabled === true`)
	})

	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			const e = document.querySelector("#map-swell-playback-speed");
			e.value = "2";
			e.dispatchEvent(new Event("change", {bubbles:true}));
		})()`),
	); err != nil {
		t.Fatalf("set swell playback speed 2x: %v", err)
	}
	if got := evalString(testCtx, `document.querySelector("#map-swell-playback-speed")?.value || ""`); got != "2" {
		t.Fatalf("swell playback speed = %q; want 2", got)
	}
	beforeSpeedPlay := mockHourCount(testCtx, 1)
	if err := chromedp.Do(testCtx, chromedp.Click(chromedp.ID(`map-swell-map-play`))); err != nil {
		t.Fatalf("start swell playback at 2x: %v", err)
	}
	waitFor(t, 5*time.Second, "2x playback +1h request", func() bool {
		return mockHourCount(testCtx, 1) > beforeSpeedPlay
	})
	if err := chromedp.Do(testCtx, chromedp.Click(chromedp.ID(`map-swell-map-play`))); err != nil {
		t.Fatalf("pause 2x swell playback: %v", err)
	}
	waitFor(t, 2*time.Second, "pause after 2x playback", func() bool {
		return attrOf(testCtx, `#map-swell-map-play`, "aria-pressed") == "false"
	})

	// Exercise the on-map manual slider using the real release fallback retained in
	// v342. Input previews/synchronizes without fetching; pointerup alone must
	// commit and fetch even if the browser does not emit a range-input change.
	previewHour := 18
	countBeforePreview := mockRequestCount(testCtx)
	previewJS := fmt.Sprintf(`
(() => {
	const e = document.querySelector("#map-swell-map-hours");
	e.value = %q;
	e.dispatchEvent(new Event("input", {bubbles:true}));
})()
`, strconv.Itoa(previewHour))

	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](previewJS),
	); err != nil {
		t.Fatalf("slider input preview: %v", err)
	}

	waitFor(t, 2*time.Second, "slider input stops playback", func() bool {
		return attrOf(testCtx, `#map-swell-map-play`, "aria-pressed") == "false"
	})

	wantPreview := fmt.Sprintf("Selected: +%dh", previewHour)
	waitFor(t, 2*time.Second, "slider preview label and synchronization", func() bool {
		got := evalString(testCtx, `document.querySelector("#map-swell-time")?.textContent?.trim() || ""`)
		return strings.Contains(got, wantPreview) &&
			evalString(testCtx, `document.querySelector("#map-swell-map-hours")?.value || ""`) == strconv.Itoa(previewHour)
	})
	previewLabel := evalString(testCtx, `document.querySelector("#map-swell-time")?.textContent?.trim() || ""`)
	t.Logf("slider preview label: %q", previewLabel)

	time.Sleep(500 * time.Millisecond)
	if got := mockRequestCount(testCtx); got != countBeforePreview {
		t.Fatalf("slider input preview fetched unexpectedly: request count %d -> %d", countBeforePreview, got)
	}

	// Do NOT dispatch change here. v341 must commit from physical release paths.
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](
			`document.querySelector("#map-swell-map-hours").dispatchEvent(new PointerEvent("pointerup", {bubbles:true, pointerType:"mouse"}))`,
		),
	); err != nil {
		t.Fatalf("slider pointer-release commit: %v", err)
	}

	waitFor(t, 8*time.Second, fmt.Sprintf("+%dh pointer-release committed request", previewHour), func() bool {
		return mockHasHour(testCtx, previewHour)
	})

	wantMapTime := fmt.Sprintf("Swell · +%dh", previewHour)
	waitFor(t, 3*time.Second, "committed slider UI", func() bool {
		return evalString(testCtx, `document.querySelector("#map-swell-map-hours")?.value || ""`) == strconv.Itoa(previewHour) &&
			strings.Contains(textOf(testCtx, `#map-swell-map-time`), wantMapTime)
	})

	// v364 regression: at Zoom 9, deliberately keep the +19h playback request
	// in flight, then make a quick zoom-out/zoom-in movement. The map's delayed
	// swell refresh starts a newer +19h request and supersedes the original
	// playback request. v364 must treat that supersession as benign and continue
	// to +20h rather than stopping playback.
	zoom9Reached := strings.Contains(textOf(testCtx, `#map-scale-status`), "Zoom 9")
	for i := 0; i < 8 && !zoom9Reached; i++ {
		if err := chromedp.Do(testCtx,
			chromedp.Click(chromedp.CSS(`.leaflet-control-zoom-in`)),
			chromedp.Sleep(350*time.Millisecond),
		); err != nil {
			t.Fatalf("zoom in for superseded-request regression: %v", err)
		}
		zoom9Reached = strings.Contains(textOf(testCtx, `#map-scale-status`), "Zoom 9")
	}
	if !zoom9Reached {
		t.Fatalf("could not reach Zoom 9; scale status=%q", textOf(testCtx, `#map-scale-status`))
	}

	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`(() => {
			window.__swellMockFailOnceHour = null;
			window.__swellMockFailedOnce = false;
			window.__swellMockDelayHour = null;
			window.__swellMockDelayMS = 0;
			const step = document.querySelector("#map-swell-playback-step");
			if (step) { step.value = "1"; step.dispatchEvent(new Event("change", {bubbles:true})); }
		})()`),
	); err != nil {
		t.Fatalf("prepare Zoom 9 hourly cache: %v", err)
	}
	waitFor(t, 10*time.Second, "Zoom 9 hourly cache ready", func() bool {
		return textOf(testCtx, `#map-swell-prime-status`) == "Swell ready." &&
			!evalBool(testCtx, `document.querySelector("#map-swell-map-play")?.disabled === true`)
	})
	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`window.__swellMockDelayHour = 19; window.__swellMockDelayMS = 850;`),
	); err != nil {
		t.Fatalf("arm delayed swell playback mock: %v", err)
	}

	before19 := mockHourCount(testCtx, 19)
	before20 := mockHourCount(testCtx, 20)
	if err := chromedp.Do(testCtx, chromedp.Click(chromedp.ID(`map-swell-map-play`))); err != nil {
		t.Fatalf("start Zoom 9 superseded-request playback: %v", err)
	}
	waitFor(t, 4*time.Second, "first delayed +19h playback request", func() bool {
		return mockHourCount(testCtx, 19) > before19
	})

	// Create a newer map refresh while the first +19h request is still pending.
	if err := chromedp.Do(testCtx,
		chromedp.Click(chromedp.CSS(`.leaflet-control-zoom-out`)),
		chromedp.Sleep(60*time.Millisecond),
		chromedp.Click(chromedp.CSS(`.leaflet-control-zoom-in`)),
	); err != nil {
		t.Fatalf("create Zoom 9 superseding refresh: %v", err)
	}

	waitFor(t, 5*time.Second, "newer +19h refresh request", func() bool {
		return mockHourCount(testCtx, 19) >= before19+2
	})
	waitFor(t, 8*time.Second, "playback continues to +20h after supersession", func() bool {
		return mockHourCount(testCtx, 20) > before20
	})

	if got := attrOf(testCtx, `#map-swell-map-play`, "aria-pressed"); got != "true" {
		t.Fatalf("playback stopped after superseded frame; aria-pressed=%q", got)
	}
	if status := evalString(testCtx, `document.querySelector("#map-swell-prime-status")?.textContent?.trim() || ""`); strings.Contains(status, "did not return enough data") {
		t.Fatalf("old generic coverage error returned after superseded request: %q", status)
	}

	if err := chromedp.Do(testCtx,
		chromedp.Evaluate[chromedp.Void](`window.__swellMockDelayHour = null; window.__swellMockDelayMS = 0;`),
		chromedp.Click(chromedp.ID(`map-swell-map-play`)),
	); err != nil {
		t.Fatalf("pause Zoom 9 superseded-request playback: %v", err)
	}
	waitFor(t, 2*time.Second, "pause after Zoom 9 superseded-request regression", func() bool {
		return attrOf(testCtx, `#map-swell-map-play`, "aria-pressed") == "false"
	})

	// Wide-zoom protection: drive the map down to Zoom 2 one click at a time.
	// Do not assume the starting zoom: this test now pins map_zoom=6 for
	// deterministic playback, whereas the older version assumed the page default
	// of Zoom 10 and blindly clicked eight times (overshooting Zoom 2).
	zoom2Reached := strings.Contains(textOf(testCtx, `#map-scale-status`), "Zoom 2")
	for i := 0; i < 12 && !zoom2Reached; i++ {
		if err := chromedp.Do(testCtx,
			chromedp.Click(chromedp.CSS(`.leaflet-control-zoom-out`)),
			chromedp.Sleep(350*time.Millisecond),
		); err != nil {
			t.Fatalf("zoom out for wide-view protection test: %v", err)
		}
		zoom2Reached = strings.Contains(textOf(testCtx, `#map-scale-status`), "Zoom 2")
	}
	if !zoom2Reached {
		t.Fatalf("could not reach Zoom 2; scale status=%q", textOf(testCtx, `#map-scale-status`))
	}
	waitFor(t, 3*time.Second, "wide-zoom swell suspension", func() bool {
		status := evalString(testCtx, `document.querySelector("#map-swell-prime-status")?.textContent?.trim() || ""`)
		return strings.Contains(status, "Zoom in to Zoom 3") &&
			evalBool(testCtx, `document.querySelector("#map-swell-map-play")?.disabled === true`) &&
			evalBool(testCtx, `document.querySelector("#map-swell-map-now")?.disabled === true`) &&
			evalBool(testCtx, `document.querySelector("#map-swell-map-next")?.disabled === true`)
	})

	countAtWideZoom := mockRequestCount(testCtx)
	time.Sleep(1 * time.Second)
	if got := mockRequestCount(testCtx); got != countAtWideZoom {
		t.Fatalf("swell requests continued below minimum display zoom: %d -> %d", countAtWideZoom, got)
	}

	// Clear All should turn swell off, hide its transport, and stop requests.
	if !evalBool(testCtx, `document.querySelector("#map-overlays-menu")?.open === true`) {
		if err := chromedp.Do(testCtx,
			chromedp.Click(chromedp.CSS(`#map-overlays-menu summary`)),
		); err != nil {
			t.Fatalf("reopen overlays menu: %v", err)
		}
	}

	if err := chromedp.Do(testCtx,
		chromedp.Click(chromedp.ID(`map-overlays-clear`)),
	); err != nil {
		t.Fatalf("Clear All: %v", err)
	}

	waitFor(t, 2*time.Second, "swell unchecked after Clear All", func() bool {
		return !evalBool(testCtx, `document.querySelector("#map-show-swell")?.checked === true`)
	})

	waitFor(t, 2*time.Second, "swell transport hidden after Clear All", func() bool {
		return evalBool(testCtx, `
(() => {
	const e = document.querySelector("#map-swell-transport");
	return !!e && getComputedStyle(e).display === "none";
})()
`)
	})

	countAfterClear := mockRequestCount(testCtx)
	time.Sleep(1250 * time.Millisecond)
	if got := mockRequestCount(testCtx); got != countAfterClear {
		t.Fatalf("swell requests continued after Clear All: request count %d -> %d", countAfterClear, got)
	}

	t.Log("PASS: v364 durable missing-frame retry + temperature/swell UX smoke test")
}
