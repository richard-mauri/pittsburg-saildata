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
- Advanced runtime identity to **Version 1.9.2 · Build v137**.

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
- Generated source build: **v137**
- Next generated source build: **v138**
- Authoritative repository: **https://github.com/richard-mauri/pittsburg-saildata**
- Authoritative branch: **main**
- Release status: **v137 / 1.9.2 current development baseline**

### Managed-file checkpoints

| Repository file | SHA-256 |
| --- | --- |
| `main.go` | `3778cc5a24397a60c7997b406c97cd3f48b5ecc4dbce4103d0d63054ff12ced2` |
| `assets/yogiisms.txt` | `4ebf00217e194ee26a8e8fe38237b298800b36ead0c64accdbb82f623c142371` |
| `check-project-state.sh` | `85fa5062e2ae4509174b6843ebc0066f4a94e2f2e90001230ca74c07aeb500dc` |

<!-- PROJECT-STATE:END -->

`README.md` deliberately does not contain its own SHA-256 because that would create a self-referential checkpoint. Git provides the history/integrity record for README itself.

### Source-generation workflow

Complete Go source candidates are generated as `main-updated-vNN.go`. Generated candidates never overwrite repository `main.go` automatically. After review, manually copy the candidate to `main.go`, run the checker/build/tests, inspect the Git diff, and then commit/push.

The generated build number is immutable. Any change to generated Go source bytes requires a new `vNN` value and filename; do not reuse an earlier build number for a corrected candidate.

The public application version and generated build are separate identities. The current runtime identity is expected to render as:

`Version 1.9.2 · Build v137`

For future public pushes, increment the patch/micro version (`1.9.2` → `1.9.3` → `1.9.4`, and so on). Existing Git release tags are immutable: never reuse or move an existing version tag.

### Verification workflow

`check-project-state.sh` is a tracked repository file. It reads this README section directly and checks:

- SHA-256 of `main.go`
- SHA-256 of `assets/yogiisms.txt`
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
git diff -- main.go README.md check-project-state.sh assets/yogiisms.txt
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

Map overlays include NWS forecast zone, NOAA HMS qualitative smoke, NOAA/NESDIS cloud cover, and NEXRAD radar. HMS smoke uses the current warm yellow → amber → burnt-orange light/medium/heavy palette. Smoke is qualitative satellite analysis, not AQI or measured PM2.5.

The Welcome page reflects the current Conditions Now / Planning and Details workflow and retains the randomized Yogi Berra quotation. `assets/yogiisms.txt` currently contains the expanded 59-line quote set.

Non-HTML compatibility remains intentional: plain-text reports, compact text/JSON, Full Report Details, and `/voice` retain the established Bottom Line interfaces even though the browser heading is Conditions Now.

### New-chat continuation instruction

When migrating development to a new conversation, provide or point the assistant to the repository/README and say:

> Read the **Development State and Chat Handoff** section of README.md, treat GitHub `main` as authoritative, and continue from the recorded generated build. Generate complete `main-updated-vNN.go` candidates, never overwrite `main.go`, run `gofmt`, and provide SHA-256 hashes and download links.

The next source candidate should therefore be **v138** unless a newer local candidate is supplied.

