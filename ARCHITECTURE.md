# Mauri's Weather & Water Conditions — architecture contract

Current merged behavior, not a development history. Inspect source and call sites before changing a contract. Historical experiments and superseded designs remain in Git and closed Issues/PRs.

## Runtime and component ownership

The root Go service includes `main.go` (routing/report preparation), `ui_templates.go` (the four embedded Go HTML template bindings), `web/templates/*.html` (browser UI HTML/CSS/JavaScript), `wind.go`, `currents.go`, and related sources. Go `embed.FS` includes the standalone templates in the compiled binary; the root `go.mod` declares Go 1.22 (CI uses Go 1.23). Do not confuse the root-owned `web/templates` with the separate `ui/` Go module. Build the full root package. The repository also contains `cmd/marineplacesgen.go`, supporting assets/scripts, and a separate `ui/go.mod` module. Root `go test ./...` does not automatically validate the nested module. Test it independently for changes in that module.

The public site at https://weather.themauris.org currently uses a locally running Go server exposed through the owner's Cloudflare Tunnel. Do not change listener/process ownership, launchers, private credentials, or tunnel behavior as an incidental code or theme change.

## Homepage canonical button component

The Homepage-owned shared UI module renders one shared suite banner on
Welcome, Conditions Now/Planning, Stations and Details. `ui_templates.go`
embeds its CSS and browser status formatter alongside the existing self-contained
page assets; `/assets/tanner.jpg` serves a local embedded portrait. Every main
panel uses `.suite-content`, which shares the exact viewport width and
responsive gutters with `.suite-banner`. Local page CSS controls vertical
spacing and functional content, not duplicated masthead or panel widths.
Existing `.suite-home-link` presentation and redundant links are removed.

The account client displays only `Signed in as <verified user>` through the
shared formatter. Guest state and preference conflict, save or
verification notes appear separately in the content area; role permissions,
bearer lifecycle and account record ownership remain local to SailingGo.

The root application pins `github.com/richard-mauri/homepage/shared/ui`, whose embedded `buttons.css` is included **after** SailingGo's own `web/static/theme.css` in each of the four rendered page templates. The existing server-side template assembly embeds the bytes into the response, with no runtime call to Homepage or changes to CSP. `--text` aliases SailingGo's `--ink`; its existing `--soft`, `--line`, and `--accent` provide the other canonical tokens.

Standard text actions in Conditions Now—Clear location & stations, Clear all overlays, information-popup launchers and Tidal & Lunar Context—use `button.ui-button` and must not reintroduce local appearance/hover/disabled/focus rules. Structural width/alignment styles are allowed. Compact map navigation menus, playback transports, icon-only close actions, station-camera controls and chart interactions are deliberately specialized for their distinct size and context; preserve their distinct functional requirements when reviewing design changes.

Return to Homepage is a same-tab native link in the shared banner; do not duplicate it or introduce independent session renewal.

## Environmental data semantics

**Observed wind** comes from NOAA/NDBC and selected validated NOAA CO-OPS mappings. Show timestamps, source and freshness; station height/exposure matters. Existing documented fallback mappings include PSBC1 (9415115), MZXC1 (9415102), PPXC1 (9414847); do not double-count overlapping samples from different feeds. Automatic nearest-usable wind selection has a documented 30-minute freshness rule; check source before editing.

**Tidal currents** are NOAA **predictions**, not directly observed flow at the boat. Flood, ebb, slack, maxima and current-station selection remain distinct from wind observations and wind-station choice.

**Forecasts and overlays** (marine/point weather, wind barbs, radar, smoke/fire, pressure, temperature, swell and other layers) are separate products with differing coverage, timeliness and provenance. Never present one as another.

## Selection, UI and routes

Clicking a map point selects the sailing location. **Panning, zooming, or Center Map must not change that selected location.** Preserve automatic and explicitly overridden `station=` behavior, while current-station matching stays independent. Preserve Conditions Now and Planning and Details, history charts/tables, units, warnings, responsive layout, overlays, markers and popovers.

Existing documented routes include `/report`, `/planning`, `/wind-readings`, `/wind-stations`, `/point-weather`, `/marine-forecast`, and supported HTML/text/JSON output. The last documented `/wind-readings` fix expects JSON fields `hours`, `readings`, `summary` and windows 1, 4, 8, 12, 16, 20 or 24 hours. Trace handlers and front-end consumers before changing schemas.

## Server authorization and session handoff

The production Go listener installs `NewSailingHTTPAuth(NewSailingSuiteGate())` and `ProtectWeather` before serving requests. All nonpublic routes, including unknown API paths, reports, weather/overlay data, marine-place JSON and local nautical tiles, are protected by default. The explicit public allowlist is health, welcome, activity.js, hero/tanner images, favicons and the exact unescaped numeric XYZ street tile path shape. No other map/API routes are exempt. `/suite/session` is a separately handled same-origin POST with bearer verification.

`sailing_suite_auth.go` validates strict 64-digit lowercase-hex bearer credentials against Homepage's fixed HTTPS profile endpoint using the trusted SailingGo Origin, bounded four-second client, no redirects and an 8192-byte JSON limit. The server accepts verified user/admin/Guest identities only; missing/rejected/expired identity gets 401 and verification failures get 503. Spoofed identity or browser-only flags cannot grant data access. The older standalone `ProtectAPI` helper is not the installed production guard.

Anonymous top-level report HTML navigation may receive a no-data, no-store entry page with shared light/dark palette, not a report. The entry consumes the Homepage fragment handoff, removes it from the URL, and exchanges a verified token via `/suite/session` for an AES-GCM encrypted first-party HttpOnly Secure SameSite=Lax cookie (`__Host-sailing-session`, eight-hour maximum). The process-generated key means restart invalidates cookies. Loopback uses a separate non-Secure cookie name but still verifies identity. Cookie-authenticated writes require same origin; explicit bearer headers are supported for CLI requests. Every protected request revalidates with Homepage; AJAX failures do not return the sign-in HTML body.

The embedded account client retains the transferable bearer in tab-scoped sessionStorage, never localStorage. It can request Homepage's fixed `/launch/sailing` bridge on a public-host direct visit without a bearer; explicit local mode suppresses retry loops and is not an authorization bypass. A missing or invalid session requires Homepage sign-in or verified Guest access. Do not promise anonymous access to protected reports. Session handoff failures must not expose data or weaken personal isolation.

## Explicit preferences and in-place updates

`web/static/account.js` loads the revisioned Homepage `/api/storage/sailing` schema-1 record for verified personal sessions. Guest preferences are separate browser-local data. Only Save Changes commits: personal drafts in `sailing.preferences.pending-account.v2` and Guest drafts in `sailing.preferences.pending-guest.v1` are tab-scoped; Guest Save writes `sailing.preferences.browser.v2`, personal Save uses revision-checked PUT. No automatic writer or retry timer. A conflict (409) or failure retains the draft and blocks silent overwrite; Discard restores the saved snapshot without PUT. Net-change comparison clears dirty state on reversal and beforeunload warns only for pending edits, except internal restoration navigation. Account identity and saved record, not stale launch URL or another user's local preferences, are authoritative.

Stage incremental control deltas, not incidental viewport state. Map radio choice is authoritative for map_layer; absent/unchecked overlays and legacy empty map type normalize consistently. Preserve all report selectors and checkboxes, marine-place categories, environmental overlays, wind/distance/history units, current-days and preferred-period planning inputs. UI-only swell playback speed/step and ALERTCalifornia mode remain allowlisted preference keys but excluded from report URL rewriting. Daily tidal-range overlay defaults checked. Default current_days=1 and wind_hours=4 compare equal to omission. Planning defaults follow existing controller conventions (12:00–17:00, 2.0 kt caution, 3.0 kt red, 60-minute buffer, 15.0 nmi distance). Restore chart controls after sailing-current-chart-updated without duplicate staging or fetch. Keep the Save/Discard toolbar and reserved status line separate.

Wind and distance unit changes use history.replaceState and the embedded unit_refresh.js to refresh static report values and history in place, preserving Leaflet and bound controllers. Failed fetches roll back the selector/URL. A bounded initialization mask avoids stale unit flashes during account restoration. Selected location/station changes may require deliberate navigation. Current range/date/planning updates use in-place chart requests, aria-busy and inline status; failures retain existing cards/URL, not forced reload.

## Optional Homepage activity telemetry

`activity.go` contains a strictly optional, server-only `ActivityReporter`. Environment variables `SAILING_ACTIVITY_URL` and `HOMEPAGE_ACTIVITY_KEY_SAILING` jointly enable it; if disabled, embedded templates receive no new `activity.js` reference. The existing `ui_templates.go` embedding pipeline inserts a defer-loaded script only when the private configuration validates. Browser-generated random tab IDs stay in same-origin tab `sessionStorage` and are submitted to SailingGo's own strict-origin `POST /api/activity/ping`. The reporter bounds presence to 5,000 entries, throttles repeated IDs and computes rolling windows in memory. A server-only no-redirect request submits aggregate data to Homepage `POST /api/admin/activity/ingest` as `sailing`, with an explicit service bearer, timestamp and nonce, once a minute. No coordinates, NOAA data, user IDs, station names or per-tab identifiers leave SailingGo.

The reporter uses server-side verification of the existing Homepage child bearer before marking activity signed in. The trusted Homepage profile endpoint returns origin-scoped pseudonymous session/user aliases, enabling per-session deduplication across tabs and private per-user presence summaries without putting aliases or bearers into collector reports. Rejected supplied identity gets a 401 response; Homepage verification outage gets 503. The embedded account client recognizes Homepage's one-hour `guest` role without attempting account storage, and shows a non-dismissible sign-in dialog with a manual Homepage action for invalid/expired/explicit local sessions. Named settings and URL/viewport rules are unchanged.

In `main.go`, server request middleware measures the concurrent lifetime of rendered reports and weather/map API requests, excluding health and static assets. `prefetchSwellRaw` separately increments the same in-flight count only once its bounded prefetch slot is acquired. This is a snapshot of observed work, not every upstream goroutine in the service. No changes are made to NOAA observation/prediction semantics, location/viewport separation, HTML/text/JSON response formats, authentication or page preferences. The optional `SAILING_ACTIVITY_TEST_LISTEN=127.0.0.1:8087` forces only an explicitly requested local test instance onto loopback; the existing production listener default is otherwise unchanged. Issue #23 implemented server-side Homepage verification of named/guest activity, with per-session deduplication; invalid bearer returns 401 and temporary verification outage 503.

## Map sources and nautical delivery

Street Map uses direct OpenStreetMap raster tiles in the existing Leaflet UI. Nautical uses the protected same-origin `/api/charts/tiles/{z}/{x}/{y}.png` endpoint, not live NOAA WMS. `nautical_tiles.go` reads validated 256×256 local PNGs from SAILING_NAUTICAL_TILE_DIR (owner launcher configures cache/nautical_tiles); no runtime downloads, rendering or writes. The operational dataset has native z12–15; the handler validates XYZ coordinates and returns missing/invalid tiles as errors rather than inventing content. Street underlay, status and cancellation remain available. On total error/stall, Street selection is staged, not automatically saved. Loading is indeterminate, bounded by a 12-second check; failure notice is dismissible and auto-hides after eight seconds. Do not infer NOAA outage from local tile failure. Radar is a separate transparent IEM ridge::USCOMP-N0Q-0 XYZ overlay; partial failures are surfaced. Map movement does not change sailing location.

Standalone cmd/nauticaltilemanager performs explicit NOAA WMS layers 0–7 acquisition, dry-run by default. -disable-scamin requests HonorScamin=1 and records SailingGo-managed-XYZ-v3 provenance, incompatible with v2/default imagery. Workers default 4 (1–8); validates PNGs, enforces tile/byte caps and compatible provenance, uses atomic non-overwriting writes and supports resumption. Diagnostic -zoom-samples, -compare-layers, -display-study and -seam-study outputs stay isolated from operational storage. See [DEVELOPMENT.md](DEVELOPMENT.md) for current dataset inventory, limits and operational precautions. Neither imagery nor this application is navigation-authoritative.

### Public street tiles (Issue #45)

`street_tiles.go` provides a dedicated GET-only `/api/maps/street/{z}/{x}/{y}.png` proxy, independent of the existing direct OSM UI and nautical cache. Valid XYZ zoom 0–19, no query, normalized integer coordinates, 256×256 decoded PNG and 1 MiB response cap. Invalid numeric coordinates/query return 400, other methods 405, malformed unauthenticated paths stay protected. The production constructor accepts only the exact allowlisted HTTPS OSM base (unset defaults to it); unsupported configuration fails closed with 503. No user-supplied host or redirects. A separate HTTP client uses a 10-second total timeout, 8 upstream slots, up to 32 waiting cache-miss handlers for at most 1 second, global 8 requests/sec token bucket with burst 16 and duplicate-inflight rejection; throttling returns 429 with Retry-After. Cache hits do not contact upstream. No retries or persistent writes.

The in-memory LRU stores at most 512 entries and 32 MiB of PNG payloads; honors Cache-Control, Expires, Age, ETag, Last-Modified, shared freshness, no-store/private and revalidation, with seven-day freshness only when upstream provides no cache policy. Unusable or variable representations are not stored. Expired entries revalidate with validators; no stale-on-error. Only selected cache/validator headers pass through; upstream 4xx/5xx preserved without body or credentials; invalid content/redirects yield 502, timeout 504. Distinct contactable User-Agent and actual browser Referer origin only (no sensitive query/path); no credentials, cookies, arbitrary headers or cache-bypass directives forwarded. No CORS permission is granted: ordinary cross-origin image display requires none; canvas/fetch integration is separate. Consumers must display OSM attribution and request only currently viewed tiles; no bulk/offline acquisition. Provider policy: https://operations.osmfoundation.org/policies/tiles/. Public service deployment/HTTPS and actual consumer acceptance remain separate.

## Validation boundaries

Unit tests and Linux CI do not establish Mac/Safari appearance, live NOAA availability or tunnel behavior. For UI changes inspect both major views, history including 1h/24h and mph/knots, manual and automatic station selection, click versus pan/zoom, affected overlays, dialogs and report routes. Preserve useful sailing status colors even when adopting another app's visual palette.

See [DEVELOPMENT.md](DEVELOPMENT.md) for verification and owner acceptance, and [CURRENT_WORK.md](CURRENT_WORK.md) for active scope.


### Street tile response diagnostics (Issue #45)
Following the existing nautical tile header convention, the proxy sets X-Sailing-Street-Source (proxy, cache or upstream) and X-Sailing-Street-Result on each handler response. In v441 a 429 identified concurrency-limit (slots); v442 replaces that immediate rejection with bounded waiting and reports queue-full or queue-timeout instead. After a slot is acquired, recheck fresh cache atomically with token/duplicate admission; rate-limit (token bucket), duplicate-inflight (same key) and upstream-429 remain distinct. A canceled admission exits without upstream work (408 if response can be delivered). X-Sailing-Street-Admission-Ms measures origin slot-admission time for initial misses. Fresh cache hits bypass the queue. There is no waiting for rate tokens or automatic upstream retry. Successful origin responses identify upstream-tile, upstream-not-modified, cache-revalidated or cache-hit; other results label validation, upstream HTTP and transport/read/timeout failures. These locally generated headers are not forwarded from upstream or persisted in the origin tile cache. Browser/edge cache hits may retain earlier response headers; they are not evidence of a current origin request. Status codes, Retry-After, cache semantics, 8 concurrent slots, 8/s burst 16, auth, upstream policy and tile bytes are unchanged. No new endpoint, counters, logging, CORS, storage or UI changes.

The v442 queue limits are provisional engineering defaults, not measured latency targets: 32 waiting handlers and 1 second maximum wait leave nominal headroom for the existing 10-second upstream timeout within GPS Map's 12-second client timeout. No FIFO fairness guarantee, unbounded goroutines, or upstream capacity increase; browser cancellation propagates through request context. A saturated/expired queue preserves no-store and Retry-After: 1. Cache headers retain normal browser/edge semantics.
