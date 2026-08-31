# Delta Sailing Data

A Go service that combines NOAA/NDBC wind observations with NOAA CO-OPS current predictions to produce conditions-oriented reports for the San Francisco Bay and Delta.

The project supports command-line use, plain-text and JSON reports, and a browser UI with an interactive Leaflet map. The default wind station is **PSBC1**.

## What it does

The service answers two practical questions: what the wind is doing now, and what the current is expected to do during the preferred planning window.

Wind observations come from NOAA/NDBC. Forecast-zone context, coastal marine forecasts where available, and active weather alerts come from the National Weather Service. Current predictions come from NOAA CO-OPS. NOAA HMS provides the optional satellite-smoke overlay. The service combines those sources into concise reports, nearby-station choices, forecast context, tidal-current charts, tidal-range context, and planning-oriented summaries.

## Browser workflow

The map workflow is intentionally explicit.

Click the map to set the **★ Selected location**, enter an exact latitude and longitude and choose **Use location**, or use **◎ My location** with browser permission. All three methods feed the same selected-location state. Direct coordinates support users who already have a position from a GPS, chartplotter, chart, or other navigation source.

Then use **Find stations near selected location** to retrieve nearby wind stations. Panning and zooming only change the view; they do not change the selected location. To search somewhere else, click the map again or enter different coordinates.

The latitude/longitude entry fields are also the visible coordinate readout for the selected location; the older redundant `Selected: lat, lon` status line has been removed.

The map legend is intentionally not context-specific. It always defines **★ Selected location**, **▲ Selected wind station**, **△ Nearby wind station**, and **◆ Selected currents station**, even when one or more markers are not currently present.

Clicking a nearby wind-station candidate opens a fixed information panel. The candidate is not committed until **Use this wind station** is selected. When a suitable currents prediction station is available within the automatic-selection distance limit, the panel previews that station. If no suitable currents station exists within the limit, the panel explicitly reports that no nearby currents prediction station is available.

The map includes recenter controls for the selected location, selected wind station, and selected currents station. These controls preserve the user's current zoom level. **◎ My location** sets and centers the selected location but does not automatically change the committed wind station.

The Leaflet base-layer selector offers **Street Map**, **Nautical Chart**, **Satellite**, and **Hybrid**. These are mutually exclusive background maps. NWS forecast-zone geometry and NOAA satellite smoke remain separate overlays, so either overlay can be shown on any basemap without changing selection or station state.

The **Nearby Wind Stations** table is constrained to a compact scrolling panel with a sticky header. It now shows each candidate's latest available wind direction, sustained wind, gust, observation age, and distance from the selected location. The initial server-rendered list and the dynamic **Find Stations** refresh use the same wind-enrichment behavior so the displayed columns do not appear and disappear depending on how the list was loaded.

Candidate map markers use the same Leaflet tooltip styling as the selected markers. The selected-location tooltip is simply **Selected location**.

The former Wind-card **Browse nearby wind stations** link was removed. Station discovery and station selection are now centered on the map workflow rather than exposed through two competing entry points.

## Features

### Wind

NOAA/NDBC real-time observations are used for wind data. The service supports the default PSBC1 station as well as arbitrary active NDBC stations. Reports include current wind direction, speed and gust, recent observations, statistics and trends, previous-afternoon summaries, and historical reports.

The service dynamically retrieves NDBC station metadata instead of maintaining a hard-coded list of wind stations.

When the selected NDBC station reports `ATMP`, the browser Wind card also shows the latest air temperature converted to °F. The Bottom Line wind sentence includes that air temperature when available. If the selected station does not report air temperature, the application leaves that value unavailable rather than silently substituting a different weather station.

Nearby-station rows show compact live wind information such as `NW 10 kt G13` plus the age of that station's latest observation.

The full-width browser Wind card includes a recent-observation history graph with separate sustained-wind and gust lines. A **10 / 20 / 30 / 40 / 50** reading selector changes the history depth without reloading the page. The selector uses the `/wind-readings` JSON endpoint to refresh the graph, recent-readings table, and wind summary together from one NDBC fetch.

The same selected recent-observation set is carried into the full text report. For example, selecting 30 readings causes the text report's wind-detail section to show **LATEST 30 OBSERVATIONS**, keeping the browser graph/table and detailed report aligned.

### NWS forecast and forecast-zone context

For live browser reports, the service uses the **selected location's** latitude/longitude to query the National Weather Service `/points` API. The lookup identifies the NWS forecast zone containing that point. If no selected location exists on initial page load, the committed wind station coordinates may be used temporarily; once the user selects a location, that location becomes authoritative.

The selected point may resolve to a coastal marine zone such as `PZZ530` or to a normal public forecast zone such as `CAZ302` or `CAZ510`. The map control is therefore labeled **Show NWS forecast zone [zone]**, not “marine forecast zone.” When NWS supplies zone geometry, the optional overlay draws that forecast-zone boundary for geographic context. Changing the selected location refreshes the zone ID, checkbox label, and geometry without changing the committed wind station.

For a coastal marine zone, the service retrieves the official NWS coastal marine text product and shows the first four forecast periods in the full-width **NWS Forecast** card. For a non-marine forecast zone, the zone boundary remains useful, but the application does not pretend that a coastal marine forecast exists. Instead of exposing an HTTP 404, the card explains that no coastal marine text forecast is published for that NWS forecast zone.

The card also checks active NWS alerts for the **selected point** and can show up to three distinct alert types. Alert wording is kept general because the selected point may be in a marine or non-marine forecast zone.

Forecast and alert retrieval are browser-focused. Historical reports, text/JSON output, Full Report Details, the station-browser page, and `/voice` do not make additional live NWS forecast requests. NWS retrieval failures do not prevent the rest of the conditions page from rendering.

### NOAA satellite smoke overlay

The map includes an optional **Show satellite smoke (NOAA)** layer using NOAA Hazard Mapping System (HMS) smoke polygons. The layer is off by default and does not change selected location, station selection, map center, or forecast-zone state.

The service uses NOAA's dated HMS KML archive and searches recent dates for the newest available analysis. The KML polygons are parsed into Leaflet geometry and classified as **light**, **medium**, or **heavy** smoke. The map status reports the analysis date, total polygon count, and how many polygons intersect the current map view so an apparently empty overlay can be distinguished from a loading failure.

HMS smoke density is qualitative satellite analysis, **not AQI and not measured PM2.5 concentration**. Because NOAA polygons can overlap, the fills and outlines are intentionally very translucent so the basemap, station markers, and forecast-zone boundaries remain readable. On Satellite and Hybrid, the smoke palette changes automatically to higher-contrast yellow/amber/magenta styling with clearer outlines; Street Map and Nautical Chart retain the subdued palette.

### Browser presentation

Browser pages display a random Yogi Berra quote loaded from `assets/yogiisms.txt`. On the main conditions page and `/welcome`, the quote appears at the bottom of the hero image. If the asset cannot be read, the page still renders normally without a quote. This feature is browser-only and does not affect report calculations or text/JSON output.

### Currents

NOAA CO-OPS current predictions provide flood, ebb, slack, current direction, prediction bin/depth, timelines, and current charts. The service automatically chooses a suitable current-prediction station and supports manual station/bin overrides. Automatic current-station selection is capped at **30 nautical miles** from the selected wind station; farther stations are treated as unavailable rather than presented as representative local current data. Explicit `current_station` overrides are not blocked by this automatic-selection limit.

The HTML report supports one-, three-, and seven-day current views and planning hints for a preferred planning period. The current-speed chart uses a stable default scale of **±3.5 kt** so different dates can be compared visually; it expands only when displayed predictions exceed that range.

The browser layout keeps current timing close to the graph. The former standalone **Current — [date]** summary card and separate Current Timeline card have been removed. The daylight/conditions window now appears directly in the **Tidal Current** card, and flood/ebb/slack milestones are presented there as a compact **Key current times** list. The separate **Tidal & Lunar Context** card remains available for lunar-cycle and tidal-range context.

The current chart can overlay each day's predicted high-to-low tidal range on a separate right-side axis. That axis uses a stable default **0–10 ft** scale and expands only when needed. Each day is shown as a thin vertical marker centered in its day bucket rather than as a wide bar that could imply duration. The marker color is classified relative to the surrounding lunar-cycle median: **Normal-cycle** is less than 15% above the median, **Elevated** is at least 15% above, **Large** is at least 30% above, and **Exceptional** is at least 45% above. The numeric tidal-range value uses a consistent text color so the classification color is carried by the marker rather than the number.

Multi-day current views keep the ordinary flood/ebb/slack event dots on the graph but do not include a separate Previous/Next event navigator, slider, selected-event cursor line, or selected-event red-dot state. The graph itself is the event reference. The actual **NOW** marker is shown only when the real current time falls within the displayed date range.

### Planning thresholds and Bottom Line

Current-planning thresholds are independently configurable for ebb and flood. The current defaults are:

- preferred below **2.0 kt**
- caution from **2.0 kt** up to **3.0 kt**
- red flag at **3.0 kt** and above

The browser UI exposes independent caution and red-flag thresholds for ebb and flood. The query parameters `caution_ebb` and `caution_flood` control caution thresholds; `max_ebb` and `max_flood` retain their existing names and represent the red-flag thresholds.

The planning classifier compares the one-decimal current value used by the planning UI with the configured threshold. For example, a predicted flood maximum of 1.96 kt is displayed and classified as 2.0 kt.

The Bottom Line no longer repeats a standalone planning-status label followed by a second sentence beginning with the same status. Instead, the planning cause is expressed once as a complete sentence that includes the triggering current speed and the applicable threshold when available, for example:

```text
Caution due to flood current reaching 2.0 kt during the preferred planning period; flood caution threshold is 2.0 kt.
```

The Bottom Line then continues with the wind/current narrative. The wind sentence includes selected-station air temperature when NDBC supplies it.

For multi-day reports, the overall planning period uses the worst status present: any red-flag day makes the period Red Flag; otherwise any caution day makes it Caution; otherwise it is Preferred.

### Service

The application provides a command-line interface and HTTP service. Important endpoints include `/report`, `/wind-readings`, `/wind-stations`, `/marine-forecast`, `/smoke-overlay`, `/health`, `/welcome`, and `/voice`.

`/wind-readings` is a lightweight browser-facing JSON endpoint for recent observations from the selected NDBC station. It supports the same 10/20/30/40/50 reading choices as the Wind card and lets the graph/table update without rebuilding the full current report page.

`/report?format=html` returns the interactive browser report. Plain text and JSON are also supported.

`/voice` returns a compact plain-text Bottom Line intended for possible voice clients. It uses the same planning cause and Bottom Line generation as the browser report. ChatGPT Voice integration is currently considered experimental/deferred; the endpoint remains available without being a required part of the normal application workflow.

## Program structure

The application is split across the Go package rather than being built from `main.go` alone.

```text
sailing-go/
├── main.go
├── wind.go
├── currents.go
├── go.mod
├── README.md
└── assets/
    ├── hero.jpg
    └── yogiisms.txt
```

`main.go` contains application orchestration, the REST server, report rendering, embedded HTML/JavaScript, map interaction, and combined report formatting.

`wind.go` contains NDBC observation retrieval, parsing, wind conversion, station discovery, statistics, trends, and historical wind handling.

`currents.go` contains NOAA current-station metadata, station selection, prediction retrieval, flood/ebb/slack processing, and current-report generation.

Build the complete package rather than compiling an individual `.go` file.

## Data sources

Wind observations are retrieved from NOAA National Data Buoy Center `realtime2` products. Active station metadata is retrieved dynamically from the NDBC active-stations feed.

Live browser forecast context uses National Weather Service products. The selected location's coordinates are resolved through the NWS `/points` API to identify the applicable **forecast zone**. For coastal marine zones, the official marine text forecast is retrieved from the NWS coastal marine text-product directory. For non-marine zones, the application retains the NWS zone context and geometry but does not claim that a marine text forecast exists. Active alerts are retrieved from the NWS alerts API for the selected point. If no location has yet been selected on initial page load, the committed wind station may be used as a temporary forecast anchor until the user chooses a location.

Satellite-smoke polygons are retrieved from NOAA's Hazard Mapping System dated KML archive. They are qualitative light/medium/heavy smoke analysis rather than AQI or measured particulate concentration.

Current predictions and current-station metadata are retrieved from NOAA CO-OPS Tides & Currents.

The application is a conditions-planning aid, not a navigation system or substitute for official navigation products.

## Building

Format the package:

```bash
gofmt -w main.go wind.go currents.go
```

Build:

```bash
go build -o sailing-go .
```

Run the CLI with the default PSBC1 station:

```bash
./sailing-go
```

Run the HTTP server:

```bash
./sailing-go -server
```

The server uses port `8080` locally unless overridden. On Render, the `PORT` environment variable is used automatically.

## HTTP examples

Open the HTML report:

```text
http://localhost:8080/report?format=html
```

Request a specific wind station:

```bash
curl -sS "http://localhost:8080/report?station=PSBC1"
```

Request JSON:

```bash
curl -sS -H "Accept: application/json" "http://localhost:8080/report?station=PSBC1"
```

Request the voice-oriented Bottom Line:

```bash
curl -sS "http://localhost:8080/voice?station=PSBC1"
```

Historical wind:

```bash
curl -sS "http://localhost:8080/report?station=SANF1&at=2026-08-20T15:00"
```

Health check:

```bash
curl -sS "http://localhost:8080/health"
```

## Deployment

The service is deployed from GitHub to Render.

Production service:

```text
https://pittsburg-saildata.onrender.com
```

A normal development cycle is to format, build, test locally, inspect `git diff`, commit, push to GitHub, and allow Render to deploy the new revision.

## Map/UI design notes

The map UI went through a recovery and simplification pass after several state-management regressions. The current design keeps the selected location, committed stations, candidate stations, viewport, and UI status from acting as competing sources of truth.

The browser map maintains authoritative client-side state for the selected location, selected wind-station ID, wind candidates, search activity, and currents-overlay preference. Candidate markers are rebuilt from candidate state rather than incrementally accumulated.

Station IDs are normalized before comparison. Literal wrapping quotes are stripped, whitespace is trimmed, IDs are upper-cased, the committed wind station is excluded from the candidate set, and duplicate candidate IDs are suppressed. This prevents the selected **▲** wind station from also appearing as an outlined **△** candidate.

The fixed wind-station information panel is used for candidate selection. Clicking a candidate previews information; **Use this wind station** performs the actual commit.

Automatic currents preview and committed-report selection share a **30 nmi** usefulness limit. A candidate whose nearest suitable NOAA current prediction station is farther away does not get a misleading ◆ preview marker.

The earlier viewport-centered **Search this area for wind stations** mode was removed. There is one station-discovery model: choose a **★ Selected location**, then use **Find stations near selected location**.

Exact latitude/longitude entry and map clicking both feed the same selected-location state transition. This avoids maintaining separate coordinate-selection behaviors.

The recenter controls for **★ Selected location**, **▲ Selected wind station**, and **◆ Selected currents station** preserve the user's zoom level. They change map center only.

## Useful San Francisco Bay / Delta wind stations

Useful references include PSBC1 for Pittsburg/Suisun Bay, PCOC1 for Port Chicago, MZXC1 for Martinez, UPBC1 for the Martinez bridge area, DPXC1 for Davis Point, RCMC1 and PPXC1 for Richmond, TIBC1 for Tiburon, and FTPC1 for the central/southern Bay.

These are reference stations, not a hard-coded application whitelist. The service discovers active stations dynamically.

## Maintainability note

`main.go` is currently large and relatively monolithic. In addition to application startup and HTTP orchestration, it contains substantial embedded HTML, CSS, JavaScript, and Leaflet map behavior. That concentration makes UI state changes harder to reason about and increases the risk of regressions when otherwise small map changes touch several concerns at once.

This is not currently a reason to refactor a working deployment. A future cleanup should be treated as a separate, deliberate project after behavior is stable. Recent UI work has deliberately removed low-value interaction state and redundant entry points.

A useful cleanup target is the nearby-wind-station enrichment path. Initial HTML rendering and the `/wind-stations` endpoint now intentionally expose the same wind/direction/gust/age information; future refactoring should preserve that behavior while reducing duplicated candidate-assembly logic.

The safest larger refactor would be to separate browser-facing assets from `main.go`: move the embedded templates, CSS, and Leaflet JavaScript into dedicated template/static files while preserving behavior. After that, HTTP/report orchestration could move into a `report.go` or `handlers.go`, leaving `main.go` primarily responsible for startup, configuration, and route registration.

The existing `wind.go` and `currents.go` split already provides a useful boundary for data-source logic. Any future refactor should preserve those boundaries and prioritize behavior-preserving moves over simultaneous redesign.

## Versioning and releases

The public application version is maintained as a single static `appVersion` constant in `main.go`. Intermediate regeneration filenames do not change the public version. Bump it only when preparing a committed release.

The project uses a three-part version number with this convention:

- **major** — a finalized release milestone
- **minor** — a new feature or significant bug fix
- **micro** — small UI polish or a minor refinement

The current release is **1.6.0**.

The v87 refinement keeps release **1.6.0** and renames the generic **Map** basemap choice to **Street Map** for clearer distinction from Nautical Chart, Satellite, and Hybrid. This is a label-only UI change; internal `map_layer` behavior remains unchanged.

The v86 refinement keeps release **1.6.0** and makes the NOAA HMS smoke styling basemap-aware. Street Map and Nautical Chart retain the subdued v83 smoke palette. Satellite and Hybrid use brighter yellow/amber/magenta smoke fills with stronger outlines so the smoke remains distinguishable over brown/green aerial imagery. Changing basemaps restyles any visible smoke layer in place and does not alter smoke data, selected location, stations, center, zoom, or NWS forecast-zone state.

Release 1.6.0 expands the Leaflet basemap selector to four mutually exclusive choices: **Street Map** (OpenStreetMap), **Nautical Chart** (NOAA ENC-based Chart Display Service), **Satellite** (Esri World Imagery), and **Hybrid** (Esri imagery with reference labels). Changing the basemap preserves map center, zoom, selected location, selected wind/current stations, NWS forecast-zone state, and NOAA smoke-overlay state. The selected basemap is preserved in generated map URLs through the `map_layer` query parameter.

Release 1.5.1 updates the browser product branding from **Mauri's Wind & Current Conditions** to **Mauri's Weather & Water Conditions** across the main report, welcome page, browser metadata, page titles, and footers. No forecast, wind, current, geolocation, smoke-overlay, or map-selection behavior changes in this release.

Release 1.5.0 adds the **◎ My location** map control, generalized **NWS Forecast** / **NWS forecast zone** handling for both marine and non-marine selected points, and the optional NOAA HMS satellite-smoke overlay. Browser geolocation, map clicks, and direct coordinates all feed the same authoritative selected-location state. Changing that location refreshes NWS forecast-zone context while leaving the committed wind station unchanged until the user explicitly selects another station. The smoke overlay is qualitative light/medium/heavy satellite analysis, is off by default, and uses deliberately light opacity so the sailing map remains readable.

Release 1.4.0 introduced selected-location-driven NWS coastal marine forecast support, active NWS alerts, and forecast-zone geometry on the Leaflet map. It also retained the random Yogi Berra quote on the hero image introduced after 1.3.0, along with wind history, selected-station air temperature, direct-coordinate entry, nearby-station selection, current planning, tidal context, and the experimental `/voice` endpoint.

Release 1.3.0 added the full-width recent-wind history graph, asynchronous 10/20/30/40/50 observation selection, shared recent-observation depth between the browser and full text report, a simplified current-report layout, compact **Key current times** inside the Tidal Current card, and relocation of the daylight/conditions window into that same current-analysis card.

The Render service can continue building and deploying from the `main` branch. A Git tag marks the exact commit corresponding to a public release without changing the deployment workflow.

For release `1.6.0`, after the final code and README changes are ready:

```sh
git status
git diff
git add main.go README.md
git commit -m "Release v1.6.0: add satellite and hybrid basemaps"
git push origin main
git tag -a v1.6.0 -m "Release v1.6.0"
git push origin v1.6.0
git log --oneline --decorate -5
```

The application displays `1.6.0`, while the corresponding Git tag uses the conventional `v1.6.0` form.

For a later release, update only the static `appVersion` value in `main.go` during the final pre-commit regeneration, update this README when release notes or workflow documentation change, commit and push `main`, then create and push the matching annotated tag.
