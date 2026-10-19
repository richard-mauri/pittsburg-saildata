# Mauri's Weather & Water Conditions — Project Overview & Development Handoff

**App version:** 1.16.0  
**Working development build:** v382 (based on uploaded v381 source)  
**Handoff updated:** October 19, 2026  
**Local project:** `~/projects/sailing-go` (macOS)  
**Repository:** https://github.com/richard-mauri/pittsburg-saildata  
**Public site:** https://weather.themauris.org  
**Deployment:** locally running Go server exposed by a Cloudflare Tunnel — **not Render**  
**Default wind station:** PSBC1

## 1. Project overview

A Go-based sailing and marine-conditions dashboard originally built for the San Francisco Bay–Delta corridor, with broader station/location support where NOAA services provide data. It combines actual wind observations, tidal-current predictions, NWS weather forecasts, and multiple environmental map overlays to help interpret sailing conditions. Keep observed values, predictions, and forecasts distinguishable.

The browser has a concise **Conditions Now** view and an expanded **Planning and Details** dashboard. It includes a Bottom Line sailing summary; current wind and recent history; tidal current predictions and timing; location/station selection; marine and point forecasts; and interactive maps with wind barbs, pressure, smoke/fire, radar, temperature, swell and other overlays. Some overlays depend on region, zoom, and upstream availability.

The Go application serves HTML, text and JSON. `main.go` owns HTTP routing and much of the embedded UI/JavaScript; other Go files provide wind-data processing, currents, forecast and supporting functions. **Build/test the entire Go package**, not `main.go` alone.

## 2. Important architecture and behavior

- **Wind:** NOAA/NDBC observations; for PSBC1 (CO-OPS 9415115), MZXC1 (9415102) and PPXC1 (9414847), the v367 wind logic also checks NOAA CO-OPS and selects the freshest acceptable provider data. Other stations generally remain NDBC-only. Do not combine overlapping provider series for statistics. Flag stale wind; the automatic nearest-usable selection criterion is 30 minutes. Internal wind measurements use m/s and are displayed as knots or mph.
- **Wind history:** `GET /wind-readings?station=PSBC1&wind_hours=4&wind_unit=mph` returns `{hours, readings, summary}`; each reading supplies `time`, `direction`, `wind`, `gust`, and `age`. The browser updates its table and SVG/chart on History selection. Supported windows: 1, 4 (default), 8, 12, 16, 20, 24 hours. This *recent-history* endpoint differs from the historical `at=` report path, which uses the NDBC archive.
- **Currents:** NOAA CO-OPS tidal-current predictions, not directly observed currents. Current-station matching and warnings are distinct from wind-station selection.
- **Station/location selection:** Clicking the map selects the sailing location. Panning, zooming, or entering **Center Map → Latitude & Longitude** coordinates only changes map view. `/wind-stations` finds nearby station candidates. A manually chosen `station=` remains an explicit override. `/report` with `lat` and `lon` but no `station` uses automatic nearest-usable station selection. Preserve both workflows.
- **Main endpoints:** `/report` (HTML/text/JSON), `/planning`, `/voice`, `/health`, `/wind-readings`, `/wind-stations`, `/point-weather`, `/marine-forecast`, and other map-specific routes. Inspect existing server handlers before changing contracts.

## 3. Local development and deployment

The user operates the server locally on macOS under `~/projects/sailing-go`; the public hostname is forwarded through a Cloudflare Tunnel. Do **not** assume a Render deployment or edit Cloudflare settings for an application-route bug. Use the project's existing service launch/restart procedure; avoid starting a competing instance on the same port. A manual launch example, **only if the existing service is stopped**, is `go run . -server -port 8080`.

Before modifying files, inspect the actual current `main.go` version/build constants and `git status`; remote GitHub content or old memory may be behind the local working code. Back up affected originals. Replace only authorized files. Run from the project directory:

```sh
cd ~/projects/sailing-go
gofmt -w main.go
go test ./...
git diff --check
git status
```

Use `git diff` to review exact changes. Test the local/public service and affected browser behavior before committing and pushing. Do not place extra `.go` backup/generated files alongside production sources: Go will compile them and may see duplicate declarations. A clean Git status was reported before the v382 update, but post-update commit/push status has **not** been confirmed.

## 4. Current development handoff — v382

**Baseline:** uploaded `main.go` identified `appVersion = "1.16.0"` and `buildVersion = "v381"`. This source is more recent than the GitHub version previously inspected. Do not restart from the older source or revert features.

**Issue:** Wind History selector showed **“Update failed”** and the chart was empty. The browser fetched `/wind-readings`, but no matching Go handler was registered. The direct endpoint check returned **HTTP 404**.

**Implemented change:** The v382 `main.go` increments `buildVersion` to `v382`, keeping `appVersion` at `1.16.0`, and adds `GET /wind-readings` using existing wind-observation retrieval, history-window filtering and formatting. It returns the JSON shape already expected by the browser. No changes to `wind.go`, `currents.go`, map/UI handling, NOAA provider selection, or Cloudflare Tunnel were intended. The initial chart-render path is separate from the dynamic History request.

**Acceptance status:** User reported **“works now”** on October 19, 2026 after applying the fix to the local/Cloudflare-hosted app. This confirms user-observed functionality, **not** an independently completed full regression test. GitHub commit/push status following v382 has not been verified. Do not claim the next build is deployed until the user confirms it.

**Current matching handoff artifacts:** `main-updated-v382.go` → `~/projects/sailing-go/main.go`; this README → `~/projects/sailing-go/README.md`. The next code modification should start from the actual latest running/local files, rather than assuming these artifacts remain authoritative forever.

**Relevant acceptance probe:**

```sh
curl -i 'https://weather.themauris.org/wind-readings?station=PSBC1&wind_hours=4&wind_unit=mph'
```

Expect HTTP 200 and the `hours`, `readings`, `summary` fields while NOAA observations are available. Recheck the History selector, initial chart, mph/knots, 1-hour and 24-hour ranges. Roll back by restoring the prior `main.go` and restarting the established local service if necessary.

## 5. Rules for the next development chat

1. **Read this README and inspect the user's latest source files first.** Treat actual source constants and current Git status as authoritative; do not infer a build number from historical conversation context.
2. For iterative changes, increment the `vNNN` development build only; **do not change semantic `appVersion`** until release preparation. From verified v382, the provisional next build is **v383**, subject to checking the files at hand.
3. Preserve all unrelated functionality. Work from the complete latest source, make focused changes, and avoid replacing the long-lived project overview with an incident-only changelog.
4. For each requested source regeneration, deliver a **matching pair** of downloadable artifacts: `main-updated-vNNN.go` and `README-updated-vNNN.md` (or files matching the actual modified Go sources). The README should be this compact overview/handoff with only the *current* build change metadata refreshed. Do not accumulate a comprehensive per-build changelog.
5. Document in the handoff: baseline/build, problem and scope, changed files, implementation/compatibility notes, **what was actually tested**, what is unverified, deployment/rollback notes, and any next step or known issue. Explicitly distinguish user-reported success from tool-run tests.
6. Never push GitHub, deploy, restart services, or alter tunnel settings without the user's explicit authorization. The user installs approved generated files into the local project.

## 6. Open considerations

The manual station-browsing workflow and automatic nearest-usable selection are both deliberately present; don't consolidate them without directed testing. CO-OPS mappings are restricted to validated stations, and up-to-date NOAA availability/freshness can vary. The wind-history fix is user-confirmed, but a full regression suite and post-v382 GitHub state are not established by this handoff.

**Historical documentation:** A much larger cumulative README exists in Git history at commit `6bf997284c91afc96203daa8482bcfe61e77b7f0`. It is **archive material**, not the format to regenerate for future handoffs.
