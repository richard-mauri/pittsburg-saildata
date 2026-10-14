# Sailing-Go v367 — NOAA Wind Data Reliability

**Application:** Mauri's Weather & Water Conditions  
**Semantic version:** 1.16.0  
**Development build:** v367  
**Baseline:** v366 / 1.15.0  
**Date:** October 14, 2026

## Purpose

Add NOAA CO-OPS wind observations to the existing NDBC wind system following the October 14 incident in which NDBC posted missing (`MM`) wind fields at multiple Bay/Delta stations while CO-OPS still provided measurements. This is an additive wind-source change. The current-prediction engine, charts, mapping, station browser, controls, and existing UI remain in place.

## Replacement files

- `main-updated-v367-final.go` → `~/sailing-go/main.go`
- `wind-updated-v367.go` → `~/sailing-go/wind.go`
- `currents-updated-v367.go` → `~/sailing-go/currents.go`
- `README-updated-v367.md` → `~/sailing-go/README.md` (when approving documentation update)

These are three complete Go source files; no ZIP, installation script, or extra production `.go` file is needed. Retain all other repository files, including `go.mod`, `go.sum`, and assets.

## Implementation

For **PSBC1 (9415115), MZXC1 (9415102), and PPXC1 (9414847)**, `getWindStation` fetches CO-OPS `product=wind` and NDBC `realtime2`, evaluates the latest valid speed in each provider's response, and chooses the freshest acceptable source. When only one source provides valid wind, that provider is used. If the two most recent valid records have identical timestamps, CO-OPS takes priority. Unsupported station IDs keep NDBC-only behavior.

NOAA CO-OPS English wind speed and gust values are interpreted as **knots**, then converted to the application's existing **meters-per-second** `Observation` storage. Existing knots and mph presentation remains in place. CO-OPS parsing requires quality flag `0,0`, a valid UTC timestamp, nonnegative finite speed, and reasonable field ranges. Missing gust/direction remain absent, never silently borrowed from other times or sources.

A wind record must be **no more than 30 minutes old** to qualify for automatic nearest-usable station selection. If no provider supplies fresh wind for an explicitly chosen station, the latest valid older reading may still be included but is flagged **stale**. The latest-wind report no longer uses newer NDBC `MM` rows as if they were wind measurements. Source and stale status are exposed in `latest`/`latest_10` JSON objects; the HTML observation-age text adds the NOAA source and a `STALE` label.

Provider histories are **not merged** for statistics, avoiding duplicate counting. The selected provider supplies the wind series for that report. Historical requests via `-at` and `/report?at=...` continue using the NDBC archive, not CO-OPS's `date=recent` data.

**Caveat:** Other cached NDBC-derived displays, including air-temperature data, remain independent. A wind reading can have a different timestamp from air temperature; do not interpret them as one synchronized measurement.

## Station-selection workflow — finding for later review

The current UI has **two distinct location-based workflows**. Entering latitude/longitude and choosing **Center Map → Latitude & Longitude** only pans the map; it does not select a sailing location or wind station. Clicking the map establishes a selected location. **Find stations near selected location** calls `/wind-stations?lat=...&lon=...` and lists nearby stations, including those with missing or stale wind. Clicking a station uses an explicit `station=` parameter to open its report; this is **manual selection**, not automatic selection.

A separate, reachable server path invokes `findNearestUsableWindStation()` when `/report` receives `lat` and `lon` **without** `station`, e.g. `/report?format=html&lat=37.84233&lon=-122.27097`. The report's **Use nearest usable station** link also removes the explicit station parameter. This secondary path applies the v367 30-minute freshness criterion; `[AUTO]` diagnostics belong to that report path, **not** the nearby-station search table. Absence of AUTO in the table does not establish dead code.

**Revisit later:** Decide whether to retain the secondary automatic report-selection workflow, expose it more clearly in the UI, or consolidate overlapping station-discovery logic. Do not remove code or change the established map/table behavior without first testing both paths. This is a source-level trace, not evidence of actual usage frequency.

## Deployment and rollback

Before replacing files, save copies of your current `main.go`, `wind.go`, `currents.go`, and `README.md`. Replace only the files above, then from `~/sailing-go` run:

```sh
gofmt -w main.go wind.go currents.go
go test ./...
go run . -server -port 8080
```

Use the existing local/public test URL to verify selected stations, automatic selection, both `format=html` and `format=json`, historical requests, wind units, and map rendering. Do not push or deploy until the complete local repository passes regression checks and browser validation. Roll back by restoring the saved v366 files and restarting the existing service.

## Verification status

The three generated Go files compiled together successfully using `GO111MODULE=off go test ./...` in a temporary directory. Isolated tests passed for a valid CO-OPS response, a rejected quality flag, NDBC `MM` rows, source choice, and stale station rejection. This **does not** constitute a full integration test against every repository file or a live NOAA availability check. Local tests and browser inspection remain required.

## Change boundary

`main.go`: updates version/build metadata, retains NDBC historical paths, adds source and stale annotation to existing observation-age display.  
`wind.go`: adds CO-OPS parser/fetcher and provider selection, field-level validity and source fields, and latest-valid wind selection.  
`currents.go`: requires a genuinely recent observation for automatic wind-station selection and corrects latest-record selection.

## Known limits and next steps

CO-OPS station mappings are deliberately limited to three investigated stations until others are verified. This iteration uses a fixed 30-minute freshness policy. HTTP requests are not cached by this layer; monitor response latency/rate if station browsing is heavy. Check CO-OPS quality-flag semantics and station coverage against the latest NOAA documentation during live integration. A stale reading may still appear in some existing summary language; verify UI contexts during the browser acceptance pass before release.

