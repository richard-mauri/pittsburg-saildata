# Mauri's Weather & Water Conditions — architecture contract

This documents current boundaries and non-negotiable behavior, **not** a requirement to reorganize the sailing source like ScraperGo. Inspect actual code and call sites before modifying a contract.

## Runtime and component ownership

The root Go service includes `main.go` (routing/report preparation), `ui_templates.go` (the four embedded Go HTML template bindings), `web/templates/*.html` (browser UI HTML/CSS/JavaScript), `wind.go`, `currents.go`, and related sources. Go `embed.FS` includes the standalone templates in the compiled binary; the root `go.mod` requires at least Go 1.16. Do not confuse the root-owned `web/templates` with the separate `ui/` Go module. Build the full root package. The repository also contains `cmd/marineplacesgen.go`, supporting assets/scripts, and a separate `ui/go.mod` module. Root `go test ./...` does not automatically validate the nested module. Test it independently for changes in that module.

The public site at https://weather.themauris.org currently uses a locally running Go server exposed through the owner's Cloudflare Tunnel. Do not change listener/process ownership, launchers, private credentials, or tunnel behavior as an incidental code or theme change.

## Environmental data semantics

**Observed wind** comes from NOAA/NDBC and selected validated NOAA CO-OPS mappings. Show timestamps, source and freshness; station height/exposure matters. Existing documented fallback mappings include PSBC1 (9415115), MZXC1 (9415102), PPXC1 (9414847); do not double-count overlapping samples from different feeds. Automatic nearest-usable wind selection has a documented 30-minute freshness rule; check source before editing.

**Tidal currents** are NOAA **predictions**, not directly observed flow at the boat. Flood, ebb, slack, maxima and current-station selection remain distinct from wind observations and wind-station choice.

**Forecasts and overlays** (marine/point weather, wind barbs, radar, smoke/fire, pressure, temperature, swell and other layers) are separate products with differing coverage, timeliness and provenance. Never present one as another.

## Selection, UI and routes

Clicking a map point selects the sailing location. **Panning, zooming, or Center Map must not change that selected location.** Preserve automatic and explicitly overridden `station=` behavior, while current-station matching stays independent. Preserve Conditions Now and Planning and Details, history charts/tables, units, warnings, responsive layout, overlays, markers and popovers.

Existing documented routes include `/report`, `/planning`, `/wind-readings`, `/wind-stations`, `/point-weather`, `/marine-forecast`, and supported HTML/text/JSON output. The last documented `/wind-readings` fix expects JSON fields `hours`, `readings`, `summary` and windows 1, 4, 8, 12, 16, 20 or 24 hours. Trace handlers and front-end consumers before changing schemas.

## Validation boundaries

Unit tests and Linux CI do not establish Mac/Safari appearance, live NOAA availability or tunnel behavior. For UI changes inspect both major views, history including 1h/24h and mph/knots, manual and automatic station selection, click versus pan/zoom, affected overlays, dialogs and report routes. Preserve useful sailing status colors even when adopting another app's visual palette.

See [DEVELOPMENT.md](DEVELOPMENT.md) for verification and owner acceptance, and [CURRENT_WORK.md](CURRENT_WORK.md) for active scope.
