# Mauri's Weather & Water Conditions

A sailing-planning dashboard for the San Francisco Bay–Delta corridor and other supported waters. Select a sailing location to see nearby **observed wind**, **predicted tidal currents**, forecasts and environmental map overlays.

**Dashboard:** https://weather.themauris.org  
**Source:** https://github.com/richard-mauri/pittsburg-saildata

## Getting started

Launch from Homepage with verified personal or Guest access, click the map where you want to sail, and generate conditions. Read the **Bottom Line** summary, then use **Conditions Now** and **Planning and Details** for observations, current timing, forecast context and overlays. You need not know NOAA station numbers: the application can select a nearby station with recent usable wind, or you can override it manually when you know a station is more representative.

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

The Go service supports authenticated browser, text and JSON outputs where available; CLI requests can use a verified Homepage bearer. Documented routes include `/report`, `/planning`, `/wind-readings`, `/wind-stations`, `/point-weather` and `/marine-forecast`; parameters and formats depend on the endpoint.

## Operating limits

The documented public service is hosted by a locally running Go application exposed through Cloudflare Tunnel; the server and tunnel must be running for public access. Wind measurements may be delayed, stations may be unavailable, and predicted currents may differ from actual water movement.

**This is a planning and exploration tool, not a navigation or safety-critical instrument.** Use official forecasts, marine notices, charts, local knowledge and seamanship before sailing.

## Nautical charts and radar

Choose **Nautical Chart** in the map controls to view locally served NOAA nautical chart imagery over the Street Map. The current chart dataset covers parts of the San Francisco Bay–Delta corridor at native zoom levels **12–15**; it is not worldwide coverage. Depth soundings are visible at zooms **13–15** in the owner-reviewed areas. Zoom 12 is available but its chart presentation is of limited practical use. A horizontal difference in land coloration remains visible near a chart boundary; its source has not been established. Missing or unavailable chart tiles may leave Street Map visible and show a status notice. The separate Weather Radar overlay shows precipitation context and may be delayed or unavailable.

These charts and overlays are **supplemental planning context, not authoritative navigation charts**. Always use current official navigation products and notices.

## Public street tile API

The provider-independent `GET /api/maps/street/{z}/{x}/{y}.png` interface returns 256×256 PNG street tiles for XYZ zooms 0–19 without Homepage authentication. It is intended only for interactive viewing; clients must display visible [© OpenStreetMap contributors](https://www.openstreetmap.org/copyright) attribution and honor caching headers. Do not use this API to prefetch, download regions, create offline archives or bulk scrape. Interactive bursts can wait for both an upstream slot and a rate token in one bounded queue. Upstream availability is best-effort; clients must still handle throttling and errors. For diagnostics, inspect X-Sailing-Street-Source and X-Sailing-Street-Result response headers (see DEVELOPMENT.md). Existing SailingGo Street Map rendering is unchanged. Public availability depends on the running SailingGo version; GPS Map integration is separate.

## Public geographic POI API

SailingGo provides bounded, read-only California terrestrial waypoint queries at `/api/poi/status`, `/api/poi/categories` and `/api/poi/bbox` for GPS Map. Dataset availability requires explicit operator configuration; marine categories are not yet available. Location, existence, access and hours are not verified. Source attribution and complete license texts are at `/api/poi/attribution`. GPS Map consumer integration is separate. See [POI API documentation](docs/POI_API.md).

## Sign-in and preferences

Launch Sailing Outlook from [Homepage](https://home.themauris.org/). Verified personal and Guest sessions use Homepage authentication; weather reports, nautical chart data and other protected API requests require verified access. The public street tile endpoint described above and the read-only POI endpoints (when configured) are narrow exceptions. The public welcome/sign-in entry does not expose protected data. Guest access is not anonymous access. Personal settings and Guest browser-local settings remain separate. Changes to supported preferences, including units, map type and overlays, are staged until you choose **Save Changes**; use **Discard Changes** to restore the last saved selections. Reversing the only change clears the unsaved state. A pending change may trigger a warning when leaving the page. An expired session or unavailable authentication service can prevent protected weather requests; return to Homepage to sign in again.

## Development documentation

For architecture and contributors, see [ARCHITECTURE.md](ARCHITECTURE.md), [DEVELOPMENT.md](DEVELOPMENT.md), [CURRENT_WORK.md](CURRENT_WORK.md), and [AGENTS.md](AGENTS.md). The README describes user-facing behavior rather than PR status or cache maintenance.
