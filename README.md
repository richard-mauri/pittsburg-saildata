# Mauri's Weather & Water Conditions

A sailing-planning dashboard for the San Francisco Bay–Delta corridor and other supported waters. Select a sailing location to see nearby **observed wind**, **predicted tidal currents**, forecasts and environmental map overlays.

**Dashboard:** https://weather.themauris.org  
**Source:** https://github.com/richard-mauri/pittsburg-saildata

## Getting started

Open the dashboard, click the map where you want to sail, and generate conditions. Read the **Bottom Line** summary, then use **Conditions Now** and **Planning and Details** for observations, current timing, forecast context and overlays. You need not know NOAA station numbers: the application can select a nearby station with recent usable wind, or you can override it manually when you know a station is more representative.

Moving or zooming the map changes the view; **clicking a location** changes the selected sailing position. These actions are intentionally different.

## Understanding the data

**Wind** is measured at NOAA stations, including NDBC and selected validated NOAA CO-OPS sources. Review observation age, station identity, anemometer height and exposure.

**Current** is NOAA **tidal-current prediction**, not a direct measurement at your boat. Use the ebb, flood, slack, maximum-speed and time plots as planning context. Wind and current stations are selected separately.

**Forecasts and map layers** may include marine/point weather, radar, pressure, temperature, smoke/fire, wind barbs and swell depending on coverage and upstream availability. These are distinct from observations and tidal-current predictions.

## Main views and interfaces

- **Conditions Now:** concise current conditions and Bottom Line.
- **Planning and Details:** deeper wind, forecast and current-planning information.
- **Wind History:** observation plots/tables with 1, 4, 8, 12, 16, 20 or 24-hour windows where data are available.
- **Station selection:** automatic nearby-usable choice or explicit manual override.
- **Maps:** environmental context and map overlays.

The Go service supports browser, text and JSON outputs where available. Documented routes include `/report`, `/planning`, `/wind-readings`, `/wind-stations`, `/point-weather` and `/marine-forecast`; parameters and formats depend on the endpoint.

## Operating limits

The documented public service is hosted by a locally running Go application exposed through Cloudflare Tunnel; the server and tunnel must be running for public access. Wind measurements may be delayed, stations may be unavailable, and predicted currents may differ from actual water movement.

**This is a planning and exploration tool, not a navigation or safety-critical instrument.** Use official forecasts, marine notices, charts, local knowledge and seamanship before sailing.

## Development documentation

This README is for users. For contributors and coding agents, see [AGENTS.md](AGENTS.md) (mandatory agent instructions), [ARCHITECTURE.md](ARCHITECTURE.md) (system and behavior contracts), [DEVELOPMENT.md](DEVELOPMENT.md) (GitHub Issues, branches, draft PRs, CI and acceptance), and [CURRENT_WORK.md](CURRENT_WORK.md) (current active handoff). Development changes follow GitHub PRs rather than downloadable replacement-file handoffs.

## Optional account preferences

Sign in at [Homepage](https://home.themauris.org/) and launch **Sailing Outlook** from its card. SailingGo shows **Signed in as [user] (Homepage)** directly below the Back to Homepage link. Previously saved account preferences load automatically, and subsequent changes are saved automatically to Homepage's per-user revisioned `/api/storage/sailing` record. There are no Save, Load or Account Sync buttons.

Persisted choices include wind/distance units, selected-location and station URL settings, planning parameters and map type/overlay selections where supported. Browser-only visits continue working, with browser-local settings independent from authenticated account data. An expired/revoked token, unsupported stored schema or a revision conflict never authorizes overwriting someone else's account. If a concurrent edit conflicts, the current browser selection is retained and the sign-in status indicates the conflict. Homepage's companion authentication/storage feature (draft PR #27) must be installed for authenticated persistence to work.


## Unit changes without a page reload

Wind and distance unit selectors update visible values in place without navigating or rebuilding the sailing map. Returning from Homepage applies saved units without first showing the wrong units and reloading the page. SailingGo fetches updated report values from its existing Go service and updates wind history using its existing data endpoint. If the update fails, the selector returns to the previous units instead of mislabeling displayed data. Selected sailing-location and station changes remain distinct from unit preferences and can require full report navigation.

## Direct-visit recovery of an existing Homepage login

If you open SailingGo directly in a fresh Safari tab with no transferable session bearer, the dashboard briefly navigates to Homepage's fixed `/launch/sailing` recovery route. Homepage verifies its existing unexpired and unrevoked parent session and returns SailingGo with a new parent-bound, fragment-only child bearer. SailingGo then automatically restores that user's saved preferences. If Homepage has no valid login, the bridge returns the dashboard once with `#suite_mode=local` and SailingGo remains usable with independent browser-only settings. There is no separate SailingGo login, automatic session renewal or credential transfer. This requires Homepage Issue #24 / PR #25 to be installed in addition to SailingGo Issue #19.

## Optional administrator activity reporting (Issue #21 draft)

When both `SAILING_ACTIVITY_URL` and a private 32-character-or-longer `HOMEPAGE_ACTIVITY_KEY_SAILING` are configured, SailingGo reports **aggregate, advisory** visitor and server work statistics to Homepage's separate administrator collector. The matching server-only key must also be configured for the `sailing` service on Homepage. The collector URL must use HTTPS or loopback-only HTTP and the exact `/api/admin/activity/ingest` path. Without both settings, SailGo's web interface and its background reporting are unchanged. Do not place service keys in browser scripts, files, Git, account settings or query strings.

While enabled, the SailingGo browser sends an ephemeral random tab identifier, visibility and interaction hints only to its own same-origin `/api/activity/ping`. The SailingGo server calculates recent observed tab counts over 2/5/15/60-minute windows and counts in-flight report, planning, forecast, weather, map-overlay and wind routes, plus bounded background swell prefetch work. It sends only the resulting aggregate JSON every minute to Homepage. No sailing coordinates, station IDs, forecast queries, account identities, personal settings, visitor IDs or tokens are transmitted. Verified signed-in classification is not yet implemented; currently those browser tabs also count under anonymous/unverified. This monitoring does **not** authorize or initiate a restart and is not a count of unique people.

### Isolated Mac/Safari acceptance without a production restart

Use a separate detached worktree of `origin/feat/21-suite-activity` so the owner’s existing `~/projects/sailing-go` checkout is not disturbed. Run `go test -count=1 .` and `go build -o /temporary/path/sailing-go .`. Start the candidate with `-server -port 8087`, plus `SAILING_ACTIVITY_TEST_LISTEN=127.0.0.1:8087`, `SAILING_ACTIVITY_URL=http://127.0.0.1:8788/api/admin/activity/ingest`, and a new disposable `HOMEPAGE_ACTIVITY_KEY_SAILING`. Start only the disposable Homepage test binary using its existing `TEST_ROOT/data`, `LISTEN_ADDR=127.0.0.1:8788` and the **same** private key; do not use the production `homego.sh` or SailingGo launcher. The first Safari page is `http://127.0.0.1:8087/welcome`; the second is `http://127.0.0.1:8788/suite-admin`. Use the existing disposable Homepage test administrator and refresh Session Activity after at least 75 seconds: SailingGo should say Reporting and show the observed browser tab. The test candidate may still fetch live NOAA data on user navigation, so this is an **online test**, not a fixture-only weather replay. Mac/Safari and live tunnel acceptance are not established by a Go CI pass.
