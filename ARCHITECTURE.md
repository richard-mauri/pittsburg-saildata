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

## Return to Homepage navigation
SailingGo's browser UI exposes a visible native anchor `<a href="https://home.themauris.org/">← Back to Homepage</a>` with no `target` and no JavaScript history manipulation. It navigates in the current tab and can be used regardless of account sign-in. The link adds no new server endpoints, exposes no credentials, and does not modify app-local state. Homepage PR #12 is merged; all four launcher cards use same-tab navigation.

The suite-wide return button uses the same `.suite-home-link` presentation in all four apps: 38px minimum height, 8px/12px padding, 10px radius, muted border at rest, accent border and darker fill on hover, and a visible keyboard focus outline. Place one link at the upper-left above the primary content/heading.

The shared suite navigation style is now intentionally a quiet 12px text link directly below each page's primary heading, with subtle hover background and underline and keyboard focus outline (rather than the former 38px bordered button). NewsGo does not render a duplicate signed-out Homepage action or an app Renew control; Homepage alone manages the 12-hour login session.

## Homepage-authenticated automatic preferences (Issue #17)

The embedded `web/static/account.js` runs on Conditions Now and Planning and Details. SailingGo consumes Homepage's URL-fragment `#suite_token`, removes it immediately and retains the transferable bearer only in tab-scoped `sessionStorage`. Explicit `#suite_mode=local` clears it. Identity and expiry come from the trusted `/api/session/profile` bearer validation; no independent SailingGo login exists. A small cyan **Signed in as [user] (Homepage)** label appears immediately below Back to Homepage, with no manual Save/Load controls.

On authenticated startup, SailingGo GETs the revisioned `/api/storage/sailing` account record. Saved settings are applied automatically to the current view, including canonical URL parameters and map settings where the map exists. When an account has no record, its initial preferences are established without requiring a manual sync. Changes are staged in the tab before URL navigation and optimistically PUT with the last server revision; the next page also recovers staged changes if navigation canceled an earlier request. The previously loaded account record, not a stale launch URL or another user's local settings, is authoritative. Conflict (HTTP 409), invalid schema, expiry, loss of network or a new identity must not silently overwrite another account. A conflicting browser change is retained locally and identified in the account-status text. Browser-only visits retain a distinct local browser copy and remain functional.

The preference payload schema remains version 1, with selected URL settings, overlays, map type and marine-place categories. Panning/zooming the map does not become a selected sailing location. Homepage companion Issue #26 / draft PR #27 must provide the trusted origin, storage namespace and handoff. Mac/Safari/live-tunnel acceptance is an owner-side gate.


## In-place unit changes and account restoration (build v399)

The existing Wind units and Distance units controls now update the browser URL using `history.replaceState` and dispatch a unit-change event; they do **not** set `window.location.href` or destroy the page. The embedded `web/static/unit_refresh.js` fetches the equivalent server-rendered document and updates static display text and wind metrics in place, preserving the Leaflet map and bound UI handlers. Wind history uses its existing JSON refresh path to recalculate the chart/table in the selected units. Dynamic map and chart controllers are never replaced. A failed fetch rolls the selector and URL back to the previously rendered units rather than leaving misleading values visible.

The account client applies *unit-only* saved preference differences using the same in-place mechanism, not `location.replace`. A brief bounded initialization mask on the unit-dependent controls/data prevents showing stale units while the authenticated Homepage storage read is pending. Selected station/location changes may still require server-side navigation, as they alter underlying report context. Authentication and user-data isolation are unchanged. Verify all dynamic map overlays, wind/history time ranges and unit-sensitive calculations in Safari before merge.

## Direct-visit recovery (Issue #19)

On the public `weather.themauris.org` origin only, if the embedded account client has no cached bearer and was not explicitly launched with `#suite_mode=local`, it redirects once to Homepage's exact fixed `/launch/sailing` route. Homepage runs a same-origin POST against the existing parent session and returns only a child bearer in `#suite_token` or explicit `#suite_mode=local` when no valid session exists. SailingGo clears the fragment, retains bearer only in tab-scoped sessionStorage, and performs the same validated profile+revisioned account sync as normal handoff. Explicit browser-only mode suppresses recovery, avoiding redirect loops. A direct visit when Homepage's bridge has a network error stays on its recovery error page with a manual local-mode link; it does not forge sign-in. No changes to authenticated user isolation, session lifetime, automatic preference saves or NOAA/dashboard semantics. Requires the companion Homepage Issue #24 PR #25 bridge.
