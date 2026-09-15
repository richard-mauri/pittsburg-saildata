# Mauri's Weather & Water Conditions

A Go service for San Francisco Bay and Delta sailing conditions. It combines NOAA/NDBC wind observations, NOAA CO-OPS current predictions, National Weather Service forecast context, and optional map overlays into a practical browser dashboard, text reports, JSON output, and a compact voice-oriented Bottom Line.

The default wind station is **PSBC1**.

## Current release

**Public version: 1.9.2**  
**Generated source lineage: v137**

Version 1.9.2 builds on the streamlined browser workflow with clearer observation freshness, better page-loading feedback, and an updated Welcome page that matches the current planning and map functionality. The main conditions page now focuses on **Conditions Now**, including compact wind metrics and a one-day tidal-current graph. The rest of the dashboard is available from a separate **Planning and Details** page, which preserves the active query state and provides the full set of planning, map, current, wind, forecast, and customization controls.

## What it does

The service is designed to answer two practical questions:

1. What is the wind doing now?
2. What are the tidal currents expected to do during the preferred planning period?

Wind observations come from NOAA/NDBC. Current predictions come from NOAA CO-OPS. Forecast-zone context and marine forecast information come from the National Weather Service. Optional map context includes NOAA/NESDIS satellite cloud cover, NOAA HMS smoke analysis, and NEXRAD radar imagery.

The application is a conditions-planning aid. It is not a navigation system and is not a substitute for official navigation products, charts, notices, or prudent seamanship.

## Browser workflow

### Conditions Now page

Open:

```text
/report?format=html
```

The main browser page is intentionally compact. It presents:

- the selected location/station context
- Conditions Now wind metric tiles
- an **AS OF** heading using the actual latest wind-observation time plus observation age (for example, `AS OF 9:18 AM · 12 MIN AGO`)
- a one-day tidal-current graph
- a **Planning and Details →** link with an immediate loading spinner/overlay while the server-rendered planning page is loading

The Conditions Now graph reuses the same current-chart renderer as the full Currents card, but it is fixed to a one-day range so the landing page stays concise and immediately useful on a phone.

If the one-day current graph cannot be generated, the existing current narrative remains available as a fallback.

### Planning and Details page

Open:

```text
/planning
```

or use the **Planning and Details →** link from the Conditions Now page.

The Planning and Details page reuses the same report-generation path as the main report and preserves the current query parameters. It contains the full operational dashboard, including location selection, wind station tools, maps and overlays, wind history, current planning controls, current-range controls, forecast context, tidal/lunar context, and supporting reference cards.

A **← Back to Conditions Now** link returns to the streamlined conditions page while preserving the active report state.

When the user follows **Planning and Details →**, the Conditions Now page immediately displays a spinner and `Loading Planning and Details…` overlay. This is an indeterminate loading indicator; it does not claim a synthetic percentage.

## Welcome page

The `/welcome` page is kept aligned with the current browser workflow. It introduces **Conditions Now** and **Planning and Details**, explains selected-location versus map-center state, describes **Local Conditions**, nearby wind-station selection, current graphs, map types/overlays, and retains the randomized Yogi Berra quote. The footer also shows the public version and generated build identity.

## Wind

NOAA/NDBC real-time observations provide wind direction, sustained wind, gusts, and related station observations.

The service supports the default PSBC1 station as well as active NDBC stations discovered dynamically from NOAA metadata.

Browser wind features include:

- current direction, sustained wind, and gust metrics
- selected-station air temperature when NDBC supplies `ATMP`
- recent wind history from **1h through 24h**
- sustained-wind and gust lines shown as clean solid lines without persistent point markers
- adaptive time-axis labels
- draggable/tappable inspection cursor that snaps to actual observations
- compact Latest Wind Readings table with a sticky header and roughly three visible rows
- selectable wind units: knots or MPH

The recent-wind selector uses:

```text
wind_hours=1|4|8|12|16|20|24
```

The default history window is 4 hours.

Wind data remain internally represented in knots. MPH conversion is display-only. Existing JSON fields such as `wind_kt` and `gust_kt` retain knot semantics.

## Currents

NOAA CO-OPS current predictions provide flood, ebb, slack, direction, prediction depth/bin, event timing, and current charts.

The application automatically chooses a suitable current-prediction station and supports explicit current-station and bin overrides.

Automatic current-station selection is limited to **30 nautical miles** from the selected wind station. A farther automatically selected station is treated as unavailable rather than presented as representative local current data. Explicit user overrides are not blocked by this limit.

The Planning and Details page supports **1-day, 3-day, and 7-day** current views.

The current-speed chart uses a stable default vertical scale of approximately **±3.5 kt** and expands when displayed predictions exceed that range.

The graph can also show daily predicted tidal range on a separate right-side scale. Tidal-range classifications are relative to the surrounding lunar-cycle median:

- Normal-cycle: less than 15% above median
- Elevated: at least 15% above median
- Large: at least 30% above median
- Exceptional: at least 45% above median

The actual **NOW** marker is shown only when the real current time falls within the displayed date range.

## Planning thresholds

Current-planning thresholds are independently configurable for ebb and flood.

Default thresholds are:

- Preferred: below **2.0 kt**
- Caution: **2.0 kt** up to **3.0 kt**
- Red Flag: **3.0 kt** and above

The query parameters are:

```text
caution_ebb
caution_flood
max_ebb
max_flood
planning_start
planning_end
planning_buffer
current_distance_warning
```

The classifier compares the one-decimal current value shown by the planning UI with the configured thresholds.

For multi-day reports, the overall planning result uses the worst status present across the selected days.

## Map and location workflow

The interactive Leaflet map keeps selected location, committed stations, candidate stations, and map viewport as distinct pieces of state.

A user establishes the selected sailing location by clicking the map. The selected location is distinct from the map viewport center, and panning or zooming does not change it.

The latitude and longitude fields are intended to display the current map viewport center. v127 resolves those input elements directly during each Leaflet synchronization and listens across drag/move/zoom completion paths. A user may edit either field normally; typing does not select a sailing location or move the map. The edited pair is applied only by choosing **Center Map → Latitude & Longitude**, which validates the coordinates and pans the map there while preserving zoom.

Nearby wind-station discovery is centered on the selected location. Candidate wind stations are shown only when an actual selected location exists; the default map does not display candidate markers merely because the server has a default wind-station candidate list. Candidates are previewed before the user commits one as the wind source.

The **Map Types**, **Map Overlays**, and **Center Map** dropdowns share one map-control row. The **Center Map** dropdown uses momentary action buttons that pan the map without changing the zoom level or the selected sailing/report location. It includes:

- My location
- Latitude & Longitude
- selected location
- selected wind station
- selected currents station

**My location** uses browser geolocation only as a map-centering action. It does not commit a new selected location, change station selection, or alter report calculations. Center Map items do not remain selected after use, so the same action can be invoked repeatedly after manually panning the map.

Recenter actions preserve the current zoom level. The selected currents station associated with the active wind station is always shown on the map when available; there is no separate visibility checkbox. Clearing the selected location also clears the wind-station candidates derived from that location, removes the selected-location URL parameters, and leaves the latitude/longitude fields showing the current viewport center. The button is labeled **Clear selected location & candidates**.

The Choose Location card reserves a compact **Local Conditions** panel beside the Latitude/Longitude controls on wider screens, stacking below them on narrow displays. Keeping that panel present before a location is selected avoids a large card-height jump when weather data appears. When a selected ★ sailing location exists, the panel uses the NWS point forecast for that latitude/longitude and displays the NWS nearby city/state from `relativeLocation`, the current-hour forecast air temperature, the next applicable daytime high and nighttime low, and a short forecast phrase. This weather context is informational only and does not alter wind-station or currents-station selection.

## Map Types

The **Map Types** dropdown provides mutually exclusive basemaps:

- Street Map
- Nautical Chart
- Satellite
- Hybrid

Changing basemap does not change location, station, forecast, current, or planning state.

The map also shows a live scale/status label in the lower-right corner. It reports the approximate horizontal distance represented by a short screen sample in both nautical miles and statute miles, together with the current Leaflet zoom level. NOAA Nautical Chart is available at Zoom 9 or closer. If Nautical is the preferred map type and the user zooms farther out than Zoom 9, the app temporarily displays Street Map and automatically restores Nautical when the map returns to Zoom 9+; the Nautical preference is retained.

## Map overlays

The **Map Overlays** control supports independent visual overlays:

- **NWS forecast zone**
- **Satellite smoke (NOAA HMS)**
- **Satellite Cloud Cover (NOAA/NESDIS)**
- **Weather radar (NWS NEXRAD via Iowa State IEM)**

Satellite Cloud Cover is rendered from NOAA/NESDIS merged GOES GeoColor imagery for the current map viewport.

Radar uses the current NEXRAD base-reflectivity mosaic through Iowa State IEM's Web-Mercator WMS service.

Smoke uses NOAA Hazard Mapping System analysis polygons and is qualitative satellite analysis, not AQI and not measured PM2.5 concentration.

Forecast-zone styling uses enhanced contrast on imagery basemaps so the boundary remains visible over Satellite and Hybrid backgrounds.

These overlays are visual context only. They do not change report calculations or selected station/location state.

## NWS forecast context

For live browser reports, the selected location is used to resolve the applicable National Weather Service forecast zone.

If the selected point falls in a coastal marine zone, the browser can show the official NWS marine forecast text. If it resolves to a non-marine forecast zone, the zone context remains available without pretending a coastal marine forecast exists.

The browser also checks active NWS alerts for the selected point.

NWS retrieval failures do not prevent the rest of the report from rendering.

## Bottom Line compatibility

The streamlined HTML heading is **CONDITIONS NOW — AS OF <observation time> · <age>**. The separate duplicate `Latest observation:` line is intentionally omitted from this card. The established internal and non-HTML **Bottom Line** interfaces remain unchanged for compatibility.

The existing prose Bottom Line remains available for:

- plain-text reports
- compact text
- compact JSON
- Full Report Details
- `/voice`

This keeps browser presentation changes from altering established text/JSON/voice consumers.

## Service endpoints

Important endpoints include:

```text
/report
/planning
/wind-readings
/wind-stations
/marine-forecast
/smoke-overlay
/health
/welcome
/voice
```

### HTML conditions page

```text
http://localhost:8080/report?format=html
```

### Planning and Details

```text
http://localhost:8080/planning
```

### JSON report

```bash
curl -sS -H "Accept: application/json" \
  "http://localhost:8080/report?station=PSBC1"
```

### Voice-oriented Bottom Line

```bash
curl -sS "http://localhost:8080/voice?station=PSBC1"
```

### Historical wind

```bash
curl -sS \
  "http://localhost:8080/report?station=SANF1&at=2026-08-20T15:00"
```

### Health check

```bash
curl -sS "http://localhost:8080/health"
```

## Program structure

The Go application is built as a package rather than from `main.go` alone.

```text
pittsburg-saildata/
├── main.go
├── wind.go
├── currents.go
├── go.mod
├── README.md
└── assets/
    ├── hero.jpg
    └── yogiisms.txt
```

`main.go` contains application startup, HTTP routing, report orchestration, browser templates, CSS/JavaScript, map interaction, combined report formatting, and the Bottom Line / Planning and Details page split.

`wind.go` contains NDBC retrieval, parsing, station discovery, statistics, trends, conversion, and historical wind handling.

`currents.go` contains NOAA current-station metadata, station selection, prediction retrieval, flood/ebb/slack processing, and current-report generation.

## Building

Format the package:

```bash
gofmt -w main.go wind.go currents.go
```

Build the complete package:

```bash
go build -o sailing-go .
```

Run the CLI with the default wind station:

```bash
./sailing-go
```

Run the HTTP server:

```bash
./sailing-go -server
```

The local default port is `8080`. On Render, the `PORT` environment variable is used automatically.

## Deployment workflow

The normal development workflow is:

1. Generate and review a versioned source candidate such as `main-updated-v115.go`.
2. Run `gofmt` on the generated source.
3. Record SHA-256 checkpoints.
4. Manually copy the reviewed generated source to `main.go`.
5. Copy the reviewed README candidate to `README.md` when applicable.
6. Run the local project-state checker.
7. Build and test locally.
8. Inspect the Git diff.
9. Commit and push to GitHub `main`.
10. Allow Render to deploy the new revision.

Generated source filenames are development lineage identifiers and are not the same thing as the public application version.

## Versioning

The public application version is maintained in the `appVersion` constant in `main.go`.

The project uses three-part versions:

- **major** — finalized release milestone
- **minor** — new feature or significant behavior change
- **micro** — small UI polish or minor refinement

The current release candidate is **1.9.1**. Generated source builds also carry a separate `buildVersion` identifier so test clients can distinguish different 1.9.1 candidates.

### 1.9.1 / v131

- Renames the streamlined HTML **Bottom Line** heading to **Conditions Now** while preserving established internal/text/JSON/voice Bottom Line identifiers for compatibility.
- Adds **Weather at Selected Location** to the Choose Location card when a selected ★ location exists.
- Shows NWS point-forecast current-hour air temperature, expected high and low, and a short forecast near that selected location.
- Refreshes the selected-location weather immediately when the browser changes the selected ★ location.
- Displays runtime identity **Version 1.9.1 · Build v131**.

### 1.9.1 / v130

- Controlled cache-validation build: no functional change beyond advancing the visible build identifier from v129 to v130.
- Confirmed the Safari Dock web app picked up the new build without clearing Website Data after the dynamic HTML no-cache policy was introduced.

### 1.9.1 / v129

- Adds explicit no-cache headers to dynamically generated HTML responses so Safari Dock web apps and other WebView clients re-fetch current HTML instead of retaining stale builds.
- Uses `Cache-Control: no-store, no-cache, must-revalidate, max-age=0` with legacy `Pragma` and `Expires` safeguards for dynamic HTML only.
- Leaves static assets and existing data/overlay caching policies unchanged.
- Displays the runtime identity as **Version 1.9.1 · Build v129**.

### 1.9.1 / v128

- Adds an explicit `buildVersion` constant separate from the public `appVersion`.
- Displays **Version 1.9.1 · Build v128** in the HTML runtime identity so stale browser/WebView content is immediately visible during testing.

### 1.9.1 / v127

- Reworks viewport-center coordinate synchronization to resolve the Latitude/Longitude DOM inputs directly every time the map synchronizes, rather than relying on cached element references.
- Synchronizes on Leaflet `drag`, `move`, `moveend`, `zoom`, and `zoomend`, and performs an immediate startup sync through the same function.
- Keeps **Center Map → Latitude & Longitude** as the explicit action that applies manually edited coordinates and preserves zoom.

### 1.9.1 / v126

- Fixes viewport-center coordinate synchronization so Latitude/Longitude update on every Leaflet `move` and final `moveend`, even if a coordinate input previously had focus.
- Removes the focus guard that could leave the displayed map-center coordinates frozen after panning.
- Keeps **Center Map → Latitude & Longitude** as the explicit action that applies manually edited coordinates and preserves zoom.

### 1.9.1 / v125

- Added continuous latitude/longitude viewport-center synchronization on Leaflet `move` with a final `moveend` update.
- Added an edit-focus guard intended to protect manual coordinate entry; v126 removes that guard because browser focus could persist during map dragging and suppress updates.
- Kept **Center Map → Latitude & Longitude** as the explicit action that applies manually edited coordinates and preserves zoom.

### 1.9.1 / v124

- Shows nearby wind-station candidates only when an actual selected sailing location exists, eliminating the default-page candidate/clear-button state mismatch.
- Recasts the latitude/longitude fields as live map-viewport-center coordinates that update after map movement.
- Replaces **Use location** with the explicit **Center Map → Latitude & Longitude** command; manual coordinate edits do nothing until that command is chosen.
- Uses normal text editing for coordinate fields so typing does not fight numeric-input replacement behavior.
- Keeps coordinate centering pan-only at the current zoom level and does not change the selected sailing location.
- Persists map-click selected locations in the URL without reload and removes those parameters when the selected location is cleared.

### 1.9.1 / v123

- Fixes the earlier **Use location** coordinate path so blank latitude/longitude fields are rejected before numeric conversion instead of silently becoming `0,0`.
- Preserves a valid coordinate pair in the current page URL with `history.replaceState` without reloading, so a later page refresh can reconstruct the selected location.
- Keeps **Use location** pan-only at the current zoom level.

### 1.9.1 / v122

- Replaces Center Map radio-style choices with momentary action buttons so centering commands have no persistent checked state and can be repeated after panning.

### 1.9.1 / v121

- Keeps all map-centering actions under the **Center Map** dropdown and places it on the same control row as **Map Types** and **Map Overlays**.
- Always shows the currents station associated with the active wind station when one is available; removes the separate **Show selected currents station** checkbox and its visibility state.
- Renames the clear action to **Clear selected location & candidates** to make clear that candidate wind stations derived from the selected location are cleared with it.
- Keeps every Center Map choice pan-only, preserving the current zoom level.
- Keeps **My location** as centering-only browser/device geolocation without changing selected/report location or station-selection state.

### 1.9.1 / v120

- Keeps all map-centering actions under the **Center Map** dropdown.
- Makes every Center Map choice pan only, preserving the current zoom level.
- Makes **My location** center on browser/device geolocation without changing the selected sailing/report location or station-selection state.
- Keeps unavailable selected-location and station targets disabled.

### 1.9.0 / v115

The v115 release candidate introduces the browser-page split:

- `/report?format=html` becomes the streamlined Bottom Line conditions page
- Bottom Line retains the compact Wind-card-style metric tiles
- Bottom Line replaces its normal current prose with the same current graph renderer used by the Currents card, fixed to a one-day window
- `/planning` provides the full **Planning and Details** dashboard
- query parameters are preserved when navigating between Bottom Line and Planning and Details
- the Planning page includes a **Back to Bottom Line** path
- text, JSON, compact, Full Report Details, and voice behavior remain compatible

This release builds on the 1.8.0 map, weather-overlay, wind-history, and Bottom Line presentation work.

## Data sources

Primary data providers include:

- NOAA National Data Buoy Center for wind observations and active station metadata
- NOAA CO-OPS Tides & Currents for current predictions and current-station metadata
- National Weather Service for forecast-zone context, marine forecasts where applicable, and active alerts
- NOAA Hazard Mapping System for qualitative smoke analysis
- NOAA/NESDIS for merged GOES GeoColor cloud imagery
- NWS NEXRAD data through Iowa State IEM for radar display

## Useful Bay and Delta wind stations

Useful references include PSBC1 for Pittsburg/Suisun Bay, PCOC1 for Port Chicago, MZXC1 for Martinez, UPBC1 for the Martinez bridge area, DPXC1 for Davis Point, RCMC1 and PPXC1 for Richmond, TIBC1 for Tiburon, and FTPC1 for the central/southern Bay.

These are reference stations, not a hard-coded application whitelist. Active stations are discovered dynamically.

## Maintainability

`main.go` remains intentionally large and contains substantial browser HTML, CSS, JavaScript, Leaflet behavior, HTTP orchestration, and report presentation logic.

A future refactor should be treated as a separate behavior-preserving project after the current UI and release behavior are stable. The safest direction would be to move browser templates/static assets out of `main.go` first, then separate HTTP/report orchestration while preserving the existing `wind.go` and `currents.go` data-source boundaries.

## v137 / 1.9.2 changes

- Moved the live map scale/status readout to the lower-right corner.
- Established **Zoom 9** as the practical minimum for the NOAA Nautical Chart basemap.
- Nautical Chart is unavailable for new selection below Zoom 9.
- If Nautical is already the preferred basemap and the user zooms out below Zoom 9, Street Map is shown temporarily while the Nautical preference remains selected.
- A map notice explains that Nautical Chart is available at Zoom 9+.
- Zooming back to Zoom 9 or closer automatically restores the Nautical Chart.
- Legitimate inland/no-chart blank areas at supported nautical zoom levels remain unchanged.
- Advanced runtime identity to **Version 1.9.2 · Build v172**.

## v136 / 1.9.2 changes

- Added a persistent map scale/status label showing approximate nautical miles, statute miles, and the current Leaflet zoom level.
- The scale readout updates as the map pans, zooms, and is resized.
- Nautical Chart availability behavior is intentionally unchanged in this build; the new scale/zoom readout is instrumentation for choosing a realistic minimum nautical-chart zoom.
- Advanced runtime identity to **Version 1.9.2 · Build v136**.

## v135 / 1.9.2 changes

- Reworked NOAA HMS smoke styling to use a warm yellow → amber → burnt-orange density palette that contrasts more clearly with blue/green map basemaps.
- Reduced the dominance of smoke polygon outlines so the filled smoke areas read first while boundaries remain visible.
- Updated the smoke legend swatches to match the new on-map palette on both street/nautical and satellite/hybrid basemaps.
- Smoke data semantics are unchanged: NOAA HMS polygons remain qualitative satellite analysis, not AQI or measured PM2.5 concentration.
- Carries forward the v133 Conditions Now observation-age heading, Planning and Details loading overlay, refreshed Welcome page, and expanded Yogiism asset.
- Advanced runtime identity to **Version 1.9.2 · Build v135**.

## Development State and Chat Handoff

This section is the authoritative development handoff for this repository. A new ChatGPT conversation can read this section together with the current repository files and continue development without a separate `PROJECT_STATE.md`.

`README.md` is intentionally the single tracked project-state document. The former local/untracked `PROJECT_STATE.md` workflow is retired once this README/checker pair is installed.

<!-- PROJECT-STATE:BEGIN -->

- Public app version: **1.9.2**
- Generated source build: **v172**
- Next generated source build: **v173**
- Authoritative repository: **https://github.com/richard-mauri/pittsburg-saildata**
- Authoritative branch: **main**
- Release status: **v172 / 1.9.3 release baseline**

### Managed-file checkpoints

| Repository file | SHA-256 |
| --- | --- |
| `main.go` | `8d3330b490335c985fadf7ffef3970b496974839da1e0d47a45777835c112f13` |
| `assets/yogiisms.txt` | `4ebf00217e194ee26a8e8fe38237b298800b36ead0c64accdbb82f623c142371` |
| `assets/fishing_reports.json` | `02b01de77784153157c6a4a60d6ad21e286f7c191bbe204fed605659ea15ca5e` |
| `check-project-state.sh` | `85fa5062e2ae4509174b6843ebc0066f4a94e2f2e90001230ca74c07aeb500dc` |

<!-- PROJECT-STATE:END -->

`README.md` deliberately does not contain its own SHA-256 because that would create a self-referential checkpoint. Git provides the history/integrity record for README itself.

### Source-generation workflow

Complete Go source candidates are generated as `main-updated-vNN.go`. Generated candidates never overwrite repository `main.go` automatically. After review, manually copy the candidate to `main.go`, run the checker/build/tests, inspect the Git diff, and then commit/push.

The generated build number is immutable. Any change to generated Go source bytes requires a new `vNN` value and filename; do not reuse an earlier build number for a corrected candidate.

The public application version and generated build are separate identities. The current runtime identity is expected to render as:

`Version 1.9.3 · Build v172`

For future public pushes, increment the patch/micro version (`1.9.2` → `1.9.3` → `1.9.4`, and so on). Existing Git release tags are immutable: never reuse or move an existing version tag.

### Verification workflow

`check-project-state.sh` is a tracked repository file. It reads this README section directly and checks:

- SHA-256 of `main.go`
- SHA-256 of `assets/yogiisms.txt`
- SHA-256 of `assets/fishing_reports.json`
- SHA-256 of `check-project-state.sh`
- `appVersion` in `main.go` against the README public version
- `buildVersion` in `main.go` against the README generated build

Run:

```bash
./check-project-state.sh
```

A clean checkpoint should report every managed file as `MATCH`, plus matching `appVersion` and `buildVersion`.

Then run:

```bash
gofmt -w main.go
go build ./...
go test ./...
```

Before a release commit:

```bash
git status
git diff -- main.go README.md check-project-state.sh assets/yogiisms.txt assets/fishing_reports.json
```

Stage only intended tracked changes. There is no longer a local `PROJECT_STATE.md` to maintain.

### Current functional baseline

The current browser architecture is intentionally split into two pages. **Conditions Now** is the compact landing page; **Planning and Details** contains the full dashboard and customization controls. Navigation preserves active report/query state, and the Conditions Now → Planning and Details transition shows an immediate loading overlay.

Conditions Now displays the active wind/current station context, compact wind metrics, a one-day tidal-current graph, and the latest actual wind-observation timestamp plus freshness age in the heading: `CONDITIONS NOW — AS OF <time> · <age>`.

Planning and Details includes location selection, nearby wind-station discovery, current-station context, 1/3/7-day current planning, wind history from 1h through 24h, NWS forecast context, Local Conditions at a selected point, map types, independent map overlays, and Center Map controls.

The **Choose Location** card treats selected sailing location and map viewport center as separate state. Latitude/Longitude display the viewport center and can be edited without side effects; **Center Map → Latitude & Longitude** explicitly applies those values. Candidate wind stations appear only after an actual selected location exists.

The **Center Map** menu uses momentary actions for My location, Latitude & Longitude, selected location, selected wind station, and selected currents station. Centering pans without changing zoom or report selection state.

The selected currents station associated with the active wind station is shown automatically when available. **Clear selected location & candidates** removes the selected location and its derived wind candidates.

The **Local Conditions** panel is permanently reserved beside the Lat/Lon controls on wider screens to avoid layout jumps. For a selected location it uses NWS point metadata/forecast data to show nearby city/state, current-hour forecast temperature, expected high/low, and a short forecast phrase.

Dynamic HTML responses use no-cache headers so Safari/Dock WebView clients pick up new builds without requiring repeated manual website-data clearing. Runtime HTML displays both public version and generated build.

Map controls place **Map Types**, **Map Overlays**, and **Center Map** on one row. The scale/status readout appears in the lower-right and reports approximate nautical miles, statute miles, and Leaflet zoom.

NOAA Nautical Chart is considered practical at **Zoom 9+**. If Nautical is the preferred basemap and the user zooms below 9, Street Map is shown temporarily with a notice; Nautical automatically returns at Zoom 9+. Legitimate inland/no-chart blank areas at supported zooms are left unchanged.

Map overlays include NWS forecast zone, NOAA HMS qualitative smoke, **NOAA CoastWatch Sea Surface Temp**, NOAA/NESDIS cloud cover, and NEXRAD radar. Sea Surface Temp uses the NOAA ACSPO daily global 0.02° near-real-time product (`noaacwLEOACSPOSSTL3SnrtCDaily:sea_surface_temperature`) through ERDDAP WMS; the overlay requests the latest available dataset time from `/sst-info`, shows the product timestamp, and includes a °F legend corresponding to NOAA's 0–35°C display scale. Satellite/cloud/coastal gaps are expected. HMS smoke uses the current warm yellow → amber → burnt-orange light/medium/heavy palette. Smoke is qualitative satellite analysis, not AQI or measured PM2.5.

The Welcome page reflects the current Conditions Now / Planning and Details workflow and retains the randomized Yogi Berra quotation. `assets/yogiisms.txt` currently contains the expanded 59-line quote set.

Non-HTML compatibility remains intentional: plain-text reports, compact text/JSON, Full Report Details, and `/voice` retain the established Bottom Line interfaces even though the browser heading is Conditions Now.

### v138 SST overlay

v138 adds **Sea Surface Temp (NOAA CoastWatch)** to Map Overlays. It uses NOAA/NESDIS/STAR ACSPO daily near-real-time sea-surface temperature through CoastWatch ERDDAP WMS. The server-side `/sst-info` endpoint reads the latest `time_coverage_end` from NOAA metadata so the browser requests a specific latest daily field and can display its timestamp. The overlay is intended primarily for coastal/offshore ocean context such as fishing; clouds, shorelines, and inland areas can contain gaps.

### New-chat continuation instruction

When migrating development to a new conversation, provide or point the assistant to the repository/README and say:

> Read the **Development State and Chat Handoff** section of README.md, treat GitHub `main` as authoritative, and continue from the recorded generated build. Generate complete `main-updated-vNN.go` candidates, never overwrite `main.go`, run `gofmt`, and provide SHA-256 hashes and download links.

The next source candidate should therefore be **v173** unless a newer local candidate is supplied.



### SST implementation note

Sea Surface Temp uses NOAA CoastWatch MUR daily analysed sea-surface temperature imagery. Because the NOAA ERDDAP WMS does not support Leaflet's native Web-Mercator tile CRS, the app requests an EPSG:4326 PNG for the current map bounds through `/sst-overlay` and displays it as a georeferenced image overlay. SST rendering does not depend on `time_coverage_end`; the app uses ERDDAP `time=current` as a fallback and derives the displayed latest timestamp from the time-axis `actual_range` when available. Clouds and coastal/inland gaps are expected.

### SST WMS compatibility

Build v172 changes the NOAA CoastWatch SST proxy request to WMS 1.3.0 with `CRS=EPSG:4326`. For WMS 1.3.0, EPSG:4326 uses latitude/longitude axis order, so the requested bounding box is sent as `south,west,north,east`. This replaces the v140 WMS 1.1.1 `SRS=EPSG:4326` request used during SST troubleshooting.

### SST loading reliability

Build v172 keeps the v141 NOAA WMS 1.3.0 / `CRS=EPSG:4326` request and improves the image-loading path. The SST raster request now uses the map's CSS pixel dimensions with a maximum of 1200×900 instead of Retina/device-pixel doubling, the NOAA request timeout is 45 seconds, and the browser no longer preloads the same SST image before Leaflet requests it. Leaflet makes the single image request directly, while the status line remains `Loading NOAA CoastWatch SST image…` until the image either loads or reports an error.

### SST product change

Build v172 keeps the v142 WMS 1.3.0 proxy and single-image loading path, but switches the SST source to NOAA CoastWatch's MUR SST dataset, `noaacwBLENDEDsstDNDaily`, variable `analysed_sst`. NOAA's ERDDAP WMS documentation uses this exact dataset/layer in its working GetMap examples. The Geo-Polar product provides a daily global Level-4 blended SST analysis at about 5 km resolution and is served from NOAA CoastWatch Central rather than the PFEL host that was timing out from the local Go process.

### SST diagnostic fetch path

Build v172 keeps the MUR SST product and WMS 1.3.0 proxy, but changes the browser loading path to make upstream failures visible. The browser now `fetch()`es `/sst-overlay` first. If the proxy returns an error, the actual response text is shown in the SST status line instead of collapsing to a generic image-load failure. If the proxy returns a PNG successfully, the response is converted to a blob URL and displayed through Leaflet. Blob URLs are revoked when replaced or when the SST overlay is disabled.

### SST transport fallback

Build v172 keeps the MUR SST WMS request and v144 diagnostics, and hardens the Go HTTP transport for NOAA CoastWatch. The SST proxy now uses a cloned `http.Transport` with a 30-second TLS handshake timeout, a 45-second response-header timeout, and a 75-second overall request timeout. It first tries the standard CoastWatch ERDDAP endpoint and, on a transport/read failure, retries once against NOAA's `/wcn/erddap/` endpoint. Successful responses expose the endpoint label in `X-SST-Upstream`, and the browser includes that source in the SST status line.

### SST IPv4 transport workaround

Build v172 keeps the MUR SST product, WMS 1.3.0 request, endpoint fallback, and browser diagnostics from v145. The v145 diagnostics identified the actual transport failure: the local Go process resolved `coastwatch.pfeg.noaa.gov` to IPv6 and the IPv6 route timed out before the HTTPS request completed. The SST-only HTTP transport now forces `tcp4` through a dedicated `net.Dialer`; other application networking is unchanged. A successful SST status line includes `IPv4` so the workaround is visible during testing.

### SST source moved to CoastWatch Central

Build v172 changes the SST upstream hostname and product after repeated TLS failures to `coastwatch.pfeg.noaa.gov`. The overlay now uses NOAA CoastWatch Central at `coastwatch.noaa.gov`, dataset `noaacwBLENDEDsstDNDaily`, variable `analysed_sst`. This NOAA Geo-Polar Blended Day+Night product is a daily global Level-4 SST analysis at about 5 km resolution and is listed by NOAA as near-real-time. The existing WMS 1.3.0, EPSG:4326, browser fetch diagnostics, blob-image overlay, and SST-only IPv4 transport remain in place. The old PFEL/WCN SST retry path is removed so a known-bad host does not add long delays.

### SST fishing-view refinements

Build v172 keeps the working CoastWatch Central Geo-Polar Blended SST source from v147 and adds two UI refinements for practical offshore use. Sea Surface Temp is now treated as a Zoom 5+ overlay; below Zoom 5 the checkbox remains selected but the raster is removed and the status line tells the user to zoom in. Returning to Zoom 5+ automatically reloads the SST field. The old broad 32–95°F legend has been replaced by a qualitative Cooler → Warmer legend because the WMS image's color scaling is controlled by NOAA; this avoids implying exact temperature/color breakpoints that the app is not setting itself.

### SST temp-break presentation

Build v172 removes the hard SST minimum-zoom restriction. The SST overlay can be used at any zoom level; zoom level is now purely a usage choice.

The SST image path now uses NOAA CoastWatch ERDDAP `griddap` transparent PNG output instead of the WMS color defaults so the application can enforce a stable fishing-oriented temperature scale. The overlay uses `noaacwBLENDEDsstDNDaily:analysed_sst` with a fixed Rainbow palette from 45°F through 75°F, divided into approximately 1°F discrete bands. The on-page legend shows 45, 50, 55, 60, 65, 70, and 75°F. This is intended to make temperature breaks and boundaries between cooler and warmer water easier to identify and to keep the same color meaning as the map is panned or zoomed. Values below 45°F or above 75°F saturate at the palette endpoints.

### Step 2 — underwater structure overlay

Build v172 begins roadmap Step 2 while keeping the Step 1 SST-break behavior intact.

A new `Underwater Structure (NOAA bathymetry + names)` map overlay combines two NOAA sources for the current map view:

- NOAA/NCEI ETOPO shaded relief for broad seafloor shape and underwater topography.
- NOAA Marine Cadastre Undersea Feature Place Names for official named banks, seamounts, ridges, canyons, and related features.

The bathymetry is rendered below SST so temperature breaks remain visible. Official undersea names render above SST so the user can correlate a temp break with a named structure. The overlay refreshes after map movement, has no artificial zoom restriction, and is explicitly labeled as a fishing-planning aid rather than a navigation product.

ETOPO is a global relief model, so small fishing pinnacles may not be resolved. Step 2 can later add higher-resolution NOAA survey layers and a small curated fishing-feature alias registry for local names such as `the Guide` and `the 601` after their coordinates are verified.

### Step 2 label readability refinement

Build v172 keeps the v150 NOAA/NCEI ETOPO shaded-relief layer but replaces the NOAA server-rendered undersea-name image with locally styled vector labels from the NOAA Marine Cadastre `UnderseaFeaturePlaceNames` feature query service.

The app requests official point features for the current map bounds and displays only fishing-relevant structural names whose official names identify seamounts, banks, ridges, hills, knolls, shoals, reefs, rises, plateaus, pinnacles, or escarpments. Canyons and other lower-priority names are suppressed to reduce clutter.

Labels are rendered by Leaflet with larger cream/white text, a strong dark halo, and a small gold feature dot so they remain readable over blue bathymetry and can stay above the SST overlay. The status line reports how many fishing-relevant official features are currently shown. This preserves official NOAA naming—including features such as Guide Seamount—while avoiding the small blue-on-blue labels baked into the NOAA rendered map image.

### Step 3 — chlorophyll / water-clarity overlay

Build v172 implements roadmap Step 3 while preserving the completed Step 1 SST-break and Step 2 underwater-structure behavior.

A new `Chlorophyll / Water Clarity (NOAA CoastWatch)` overlay uses the NOAA CoastWatch VIIRS multi-sensor daily chlorophyll-a product:

- Dataset: `noaacwNPPN20VIIRSchlociDaily`
- Variable: `chl_oci`
- Product: NOAA S-NPP + NOAA-20 VIIRS merged daily chlorophyll-a
- Spatial resolution: about 4 km
- Units: mg/m³ chlorophyll-a

The overlay uses ERDDAP `griddap` transparent PNG output with a fixed logarithmic 0.05–5 mg/m³ color scale. The fixed scale keeps the same colors meaningful between map views. Lower chlorophyll values represent relatively clearer/blue offshore water; higher values indicate greener, more phytoplankton-rich water. The legend is marked at 0.05, 0.1, 0.2, 0.5, 1, 2, and 5 mg/m³ so the user can visually compare water-clarity boundaries with Sea Surface Temp breaks and underwater structure.

The chlorophyll raster is semi-transparent and sits above SST but below the locally styled underwater feature labels. As with SST, the CoastWatch request is proxied through the Go server and forced over IPv4 to avoid the previously observed local IPv6 path problem. The overlay refreshes after map movement and has no artificial zoom restriction.

This layer is a fishing-planning indicator, not a direct optical-water-clarity measurement. Clouds and atmospheric conditions can create missing satellite ocean-color coverage, so gaps should not be interpreted as clear or dirty water.

### Step 3 refinement — clear-water edge emphasis

Build v172 refines the Step 3 chlorophyll presentation after the first 4 km global product proved visually too dominant and blocky for the intended fishing workflow.

The underlying NOAA CoastWatch daily chlorophyll source is unchanged in this build, but the rendering is deliberately less intrusive:

- Overlay opacity is reduced from 0.58 to 0.34 so Sea Surface Temp breaks, bathymetry, and feature labels remain readable.
- The fixed chlorophyll range is narrowed from 0.05–5 mg/m³ to 0.05–2 mg/m³ on a logarithmic scale, putting more contrast into the cleaner-water range fishermen care about.
- UI wording now emphasizes `Clear-Water Edge` rather than presenting chlorophyll as a full-field water-clarity map.
- The legend is simplified to 0.05, 0.1, 0.2, 0.5, 1, and 2 mg/m³ and explicitly tells the user to look for the transition zone where clearer and greener water meet.

NOAA documents higher-resolution VIIRS sector products at about 750 m, including daily CoastWatch sector chlorophyll products. Those are the preferred future Step 3 upgrade once a reliable California-sector delivery path is wired into this app. Until then, the 4 km global product remains the working near-real-time source.

### Step 3 high-resolution chlorophyll refinement

Build v172 replaces the coarse ~4 km global chlorophyll source with NOAA CoastWatch's near-real-time S-NPP VIIRS 750 m sector product for the eastern Pacific:

- Dataset: `noaacwNPPVIIRSchlaSectorUYDaily`
- Variable: `chlor_a`
- Nominal resolution: 750 m
- Sector UY bounds: approximately 0.11°S to 44.88°N and 180.03°W to 119.97°W
- California and the offshore fishing grounds discussed in this project are within this sector.

The browser clips chlorophyll requests to the UY sector and displays the raster only over the actual intersecting bounds, avoiding geographic stretching when the map view extends inland east of the sector. Areas outside sector UY are intentionally left unpainted rather than falling back to the coarse 4 km source.

The Step 3 clear-water-edge presentation from v153 is retained: low opacity and a fixed logarithmic 0.05–2 mg/m³ scale so SST, underwater structure, and official feature labels remain readable. The purpose is to expose finer water-color boundaries that can be compared with Sea Surface Temp breaks and offshore structure.

### Step 3 high-resolution cloud-gap fill

Build v172 keeps the NOAA CoastWatch S-NPP VIIRS 750 m Sector UY chlorophyll source from v154, but changes the server-side rendering strategy to reduce the sparse "colored islands" caused by cloud masking.

Instead of showing only the newest daily scene, the Go server requests the five most recent daily scenes in parallel and builds a recency-prioritized mosaic. For each pixel, the newest valid chlorophyll value is used; if that pixel is transparent/masked in the newest scene, the server fills it from the next-most-recent scene, continuing through up to five scenes. This is intentionally not an average or temporal smoothing operation: it is a latest-valid-pixel cloud-gap fill.

The fixed logarithmic 0.05–2 mg/m³ clear-water scale, low overlay opacity, Sector UY clipping, SST-break layer, and underwater-structure labels remain unchanged. The goal is to preserve 750 m spatial detail while improving spatial continuity enough to make clear-water boundaries useful for fishing planning.

ERDDAP supports `last` and `last-n` time index selectors, which this build uses for the five recent daily scenes.

### Step 3 native NOAA gap-filled chlorophyll

Build v172 abandons the failed app-generated 5-scene mosaic from v155 and switches to NOAA's native DINEOF gap-filled chlorophyll analysis:

- Dataset: `noaacwNPPN20S3ASCIDINEOF2kmDaily`
- Variable: `chlor_a`
- Product: NOAA multi-sensor Level-4 DINEOF chlorophyll-a
- Sensors: S-NPP VIIRS, NOAA-20 VIIRS, and Sentinel-3A OLCI
- Nominal resolution: about 2 km
- Coverage: global, daily, gap-filled upstream by NOAA

The app again requests a single transparent PNG from CoastWatch ERDDAP. NOAA performs the cloud-gap filling upstream, which removes the fragile `last-n` compositing logic and should provide a continuous field without reverting all the way to the coarse 4 km daily product.

The Step 3 fishing-oriented presentation remains: low overlay opacity and a fixed logarithmic 0.05–2 mg/m³ scale so clear-water boundaries can be compared with Sea Surface Temp breaks and underwater structure. This dataset is global, so the Sector UY clipping logic is removed.

### Step 3 diagnostic repair

Build v172 repairs a source-generation regression introduced during the v155/v156 chlorophyll experiments. Those candidates no longer contained dedicated `/chlorophyll-info` and `/chlorophyll-overlay` HTTP handlers, which explains why the browser checkbox could remain selected while no chlorophyll layer or chlorophyll attribution appeared.

v157 restores the chlorophyll handlers while preserving the working SST and underwater-structure routes. It uses the NOAA native DINEOF gap-filled dataset `noaacwNPPN20S3ASCIDINEOF2kmDaily`, variable `chlor_a`, and always asks ERDDAP for the actual last indexed field with `[last]`.

The metadata endpoint now prefers the time-axis `actual_range` endpoint when reporting the latest data time. The overlay proxy also exposes the served data time through `X-Chlorophyll-Time` and returns much more of NOAA's upstream error text, including the exact upstream request URL, when a PNG request fails. The browser surfaces that diagnostic text directly. This build is intentionally diagnostic: do not change chlorophyll products again until any remaining failure is observed in the returned error message.

### Step 3 presentation cleanup

Build v172 keeps the working NOAA native DINEOF gap-filled 2 km chlorophyll source and changes only its presentation.

The chlorophyll raster now uses ERDDAP's calmer `Ocean` palette instead of `Rainbow`, while retaining the fixed logarithmic 0.05–2 mg/m³ range. Overlay opacity is reduced from 0.34 to 0.26 so Sea Surface Temp breaks, bathymetry, undersea-feature labels, and the basemap remain visually dominant.

The on-page legend is also changed to a muted clean-water palette: deep blue through blue/cyan, subdued green, yellow-green, and muted brown. The design goal is to make the clear-water transition readable without turning the map into a multicolor heatmap.

### Step 3 fishing-contrast tuning

Build v172 keeps the working NOAA native DINEOF gap-filled 2 km chlorophyll source and retunes only the visual mapping for offshore fishing.

The chlorophyll overlay opacity is increased from 0.26 to 0.38. The map rendering range is tightened from 0.05–2 mg/m³ to 0.1–1 mg/m³ on a logarithmic scale, concentrating visual contrast in the offshore transition range instead of letting very high nearshore chlorophyll dominate.

The legend now emphasizes a stronger deep-blue → cyan → green → yellow progression with marks at 0.1, 0.2, 0.3, 0.5, 0.7, and 1 mg/m³. The goal is to make the cleaner-to-greener boundary obvious enough to compare with Sea Surface Temp breaks and underwater structure without returning to the noisy full-rainbow appearance.

### SST regression repair

Build v172 fixes an SST rendering regression introduced while adding the chlorophyll overlay-bounds logic. `refreshSSTOverlay()` was accidentally changed to call `L.imageOverlay()` with `overlayBounds`, a variable that exists in the chlorophyll path but not in the SST path. That JavaScript reference error occurred after the SST PNG was fetched, so the SST checkbox could remain selected while no SST raster or attribution appeared.

The SST image overlay now correctly uses its own current map `bounds` again. Chlorophyll continues to use its separate `overlayBounds` behavior unchanged. No SST product, palette, transport, or Step 1 temp-break behavior is otherwise changed.

### Step 3 redesign — chlorophyll as edge lines

Build v172 keeps SST as the colored raster and stops displaying chlorophyll as a second filled color raster.

The app still retrieves the NOAA CoastWatch native DINEOF gap-filled 2 km chlorophyll field, but the browser now converts that image into a transparent strong-gradient edge overlay. An adaptive threshold emphasizes roughly the strongest local chlorophyll gradients in the current view. The rendered line uses a dark halo with a bright center so it remains visible over both warm and cool SST colors.

This avoids hue mixing between SST and chlorophyll. The chlorophyll layer now answers a narrower fishing question: where are the stronger cleaner-to-greener water boundaries? It is explicitly an edge detector, not an exact concentration contour. SST temperature colors and the underwater-structure labels remain unchanged.

### Step 3 redesign — numeric chlorophyll contours

Build v172 replaces the v161 image-gradient edge detector with concentration contours derived from the NOAA numeric chlorophyll grid.

The `/chlorophyll-overlay` route now requests the latest `chlor_a` grid from `noaacwNPPN20S3ASCIDINEOF2kmDaily` as ERDDAP JSON, downsamples large map extents with ERDDAP stride, reconstructs the latitude/longitude grid, and runs server-side marching-squares contour extraction.

Only three chlorophyll contours are drawn:

- 0.2 mg/m³ — cyan
- 0.3 mg/m³ — emphasized cream/white primary clear-water transition reference
- 0.5 mg/m³ — gold

Each line has a dark halo so it remains readable over SST colors. This avoids the spaghetti-like local-gradient outlines from v161 and makes the chlorophyll layer an exact concentration-boundary overlay rather than an image edge detector. Sea Surface Temp remains the colored raster and underwater structure remains unchanged.

### Step 3 contour compile fix

Build v172 fixes the Go type errors in the v162 marching-squares contour renderer. The contour endpoints are floating-point pixel coordinates, but the `drawLine` helper was mistakenly declared with integer endpoint parameters. That caused the reported `math.Abs`, `math.Round`, arithmetic, and `crossings[].x/y` compile errors.

`drawLine` now accepts `float64` endpoints and rounds only when plotting pixels. No contour levels, chlorophyll data source, SST behavior, or underwater-structure behavior are changed.

### Step 3 split chlorophyll presentation

Build v172 separates chlorophyll into two independent overlays using the same NOAA CoastWatch DINEOF gap-filled 2 km source.

`Chlorophyll Field` restores a restrained semi-transparent background raster so broad water-mass features—such as low-chlorophyll pockets, eddies, and clean-water intrusions—remain visually obvious. It uses the fixed 0.1–1 mg/m³ logarithmic display range and sits below the contour layer.

`Chlorophyll Contours` remains a separate overlay and continues to draw exact 0.2, 0.3, and 0.5 mg/m³ concentration contours from the NOAA numeric grid. The two layers can be used independently or together. Sea Surface Temp remains a separate colored raster, and underwater structure remains unchanged.

This split is intended to preserve both kinds of information the prior experiments exposed: the broad chlorophyll background pattern and the sharper cleaner-to-greener boundary references.

### Fishing Reports overlay prototype

Build v172 adds a single `Fishing Reports (recent tuna snapshot)` overlay. It is intentionally one overlay rather than separate species/confidence layers.

The prototype contains recent public Northern/Central California tuna reports:

- Four Fort Bragg albacore reports from September 1, 3, 4, and 5, 2026.
- One broad bluefin regional report described as `Cordell to Monterey` from August 17, 2026.

The Fort Bragg reports use local shorthand such as `27×35`, `25×25`, `24×37`, and `20×20`. The app converts those to approximate degree-minute positions and always draws a dashed uncertainty circle so the map does not imply survey-grade coordinates. The report popup preserves the original location wording, catch summary, size notes, provenance, and a source link.

The Cordell-to-Monterey bluefin item is rendered as a dashed broad regional polygon, not a point, because the source did not provide an exact fishing coordinate.

This is a curated snapshot, not yet a live scraper or automated fishing-report feed. The intent is to validate the map model first: one Fishing Reports overlay containing exact, derived, and broad-regional report geometries with explicit confidence.

### Fishing Reports external data file

Build v172 removes the hard-coded fishing report arrays from `main.go`.

The map now loads `GET /fishing-reports`, and that server route reads and validates `assets/fishing_reports.json`. The JSON file is therefore the update point for future fishing reports; adding, removing, or refreshing report records no longer requires changing the Go source or JavaScript map implementation.

The initial `schema_version` is `1`. Each report carries a `position_type` such as `derived_point`, `exact_point`, or `region`, plus the same provenance/confidence fields already used by the v165 prototype. Derived points retain `uncertaintyNM`; regional reports retain a polygon. The browser renders whatever valid reports are present in the file.

For this candidate, copy `fishing_reports-updated-v166.json` to `assets/fishing_reports.json` before running the checker. The fishing report asset is now included in the README managed-file checkpoint table, so `check-project-state.sh` validates its SHA-256 automatically without any checker-script change.

This is still a manually curated feed. A future ingestion job can update `assets/fishing_reports.json` or generate the same schema without changing the map layer.

### Underwater structure label decluttering

Build v172 keeps the NOAA/NCEI bathymetry relief visible at all zoom levels but adds strict controls to undersea feature names.

Feature names are now hidden below Zoom 6. At wider planning scales the label count is capped progressively: 10 at Zoom 6, 24 at Zoom 7, 45 at Zoom 8, 65 at Zoom 9, and 85 at Zoom 10+.

Fishing-relevant features are prioritized before labels are placed. Seamounts, banks, ridges, and plateaus receive highest priority; rises and escarpments follow; secondary classes such as knolls, shoals, reefs, pinnacles, and hills are lower priority.

The browser also performs screen-space collision suppression using an estimated label footprint. When two candidate names would overlap, the lower-priority/later candidate is skipped. Within the same priority class, features nearer the current map center are considered first. The structure status line reports how many labels were shown versus how many eligible features were present in the viewport.

### Offshore Trip Planning — first operational panel

Build v172 adds an `Offshore Trip Planning` panel tied to the selected ★ map destination.

The new `/offshore-trip` endpoint combines two sources for the selected lat/lon:

- the existing NWS marine-zone forecast and active alerts for that point;
- the nearest usable NDBC realtime station found within 180 nmi, preferring the closest station among the first 12 candidates that actually returns usable realtime wind or wave data.

NDBC realtime standard-meteorological data are parsed for sustained wind, gust, significant wave height, dominant wave period, average period, mean wave direction, and observation time. Wind speed is converted from m/s to knots and significant wave height from meters to feet.

The browser panel shows observed wind, gust, significant wave height, dominant period, mean wave direction, the buoy/station name and distance from the selected destination, up to four NWS marine forecast periods, and any NWS alerts. It also surfaces explicit watch items when observed conditions cross practical planning thresholds: sustained wind at 15/20 kt, gusts at 25 kt, significant seas at 6/8 ft, or at least 4 ft of significant wave height with a dominant period of 8 seconds or less.

These watch items are deliberately not presented as a magic go/no-go score. They are planning flags only; the actual observed values and official NWS forecast text remain visible.

This first pass evaluates the selected offshore destination, not the entire transit corridor. A later enhancement can add launch point, route distance, multiple forecast zones/buoys along the route, and outbound-versus-return timing.

### Offshore Trip Planning startup fix and Sea Surface Temp terminology

Build v172 fixes the v168 startup case where the `Offshore Trip Planning` card could remain hidden when the Planning page loaded with an already-selected `lat`/`lon` in the URL. The page now explicitly refreshes the offshore trip panel during initialization whenever `mapState.selectedLocation` already exists, while retaining the existing refresh when the user clicks a new ★ destination.

This build also changes the user-facing SST wording to `Sea Surface Temp` for clarity. The map overlay checkbox and legend now use `Sea Surface Temp`, and explanatory text spells out sea-surface temperature where appropriate. Internal JavaScript identifiers, `/sst-*` endpoints, diagnostic headers, and data-source plumbing remain unchanged for compatibility.

### Fishing Planning subsection

Build v172 keeps the existing `Offshore Trip Planning` card for weather, buoy observations, swell/period context, NWS forecast periods, alerts, and operational watch items, and adds a separate `Fishing Planning` subsection inside that card.

For the selected ★ destination, the Fishing Planning subsection now summarizes four fishing-specific factors:

- `Sea Surface Temp` — the latest NOAA CoastWatch Geo-Polar Blended daily SST at the selected point plus the temperature spread across an approximately 15 nmi neighborhood. The UI classifies that local spread as weak, moderate, or pronounced temp-break signal rather than pretending to know an exact break line distance.
- `Chlorophyll` — the latest NOAA CoastWatch DINEOF chlorophyll value at the selected point, a simple cleaner/transition/greener-water classification, and whether the 0.30 mg/m³ transition is crossed inside the same nearby sampling window.
- `Structure` — the nearest fishing-relevant NOAA Marine Cadastre undersea feature found within roughly one degree of latitude of the selected point, with distance in nautical miles.
- `Recent reports` — point fishing reports within 100 nmi, prioritizing the newest report, plus broad regional reports when the selected point falls inside a report polygon.

The new `/fishing-water` endpoint retrieves numeric CoastWatch SST and chlorophyll grid values server-side. The Fishing Planning subsection then combines those water values with the existing NOAA structure service and the existing `assets/fishing_reports.json` feed in the browser.

The subsection provides a short fishing-setup synthesis based on the visible factors, but it does not create an opaque numerical score. The underlying water values, structure distance, and report context remain visible so the user can make the fishing decision.

This is still selected-destination analysis rather than route-wide fishing analysis. A later enhancement can evaluate where the strongest SST/chlorophyll alignment lies around the destination instead of only summarizing the local neighborhood.

### Offshore Trip Planning eligibility gate

Build v172 makes the entire `Offshore Trip Planning` card conditional on the selected ★ location actually qualifying as offshore/coastal-ocean water.

The `/offshore-trip` endpoint now returns an explicit `is_offshore` boolean. It resolves the NWS forecast zone for the selected point, reads the zone name from the NWS forecast-zone API, and only treats recognized ocean/coastal marine zone families as eligible. Land forecast zones therefore do not qualify.

For the San Francisco/Monterey region, the enclosed-water zones `PZZ530` (San Pablo Bay, Suisun Bay, West Delta, and San Francisco Bay north of the Bay Bridge), `PZZ531` (San Francisco Bay south of the Bay Bridge), and `PZZ535` (Monterey Bay) are explicitly excluded from the offshore card. A defensive zone-name check also excludes San Francisco Bay, San Pablo Bay, Suisun Bay, West Delta, and Sacramento-San Joaquin Delta wording.

The browser now keeps the card hidden while offshore eligibility is being resolved. Only after `is_offshore: true` is returned does it reveal the card and run the Fishing Planning subsection. Non-offshore selections also skip the NDBC offshore-buoy lookup and the Fishing Planning water/structure/report requests.

This means a selected location in Antioch, the Delta, San Pablo Bay, or San Francisco Bay will not show `Offshore Trip Planning`, while selected coastal-ocean/offshore points can still use the full trip and fishing-planning workflow.

### Release 1.9.3 / Build v172

Build v172 is a release-only bump from the v171 feature-complete candidate. There are no functional changes from v171.

Public version changes from `1.9.2` to `1.9.3`, and the immutable generated source lineage advances from `v171` to `v172`.

Release 1.9.3 includes the offshore/fishing planning work completed across the preceding development builds: Sea Surface Temp terminology and temp-break context, chlorophyll field and contours, decluttered underwater structure, externalized fishing reports, Offshore Trip Planning with NWS/NDBC conditions, Fishing Planning synthesis, and the offshore eligibility gate that suppresses the offshore card for inland, Delta, Bay, and other non-offshore selections.
