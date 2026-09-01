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

The main map can be resized vertically by dragging the handle immediately below it; this changes only the viewport height, not any map or selection state. The Leaflet base-layer selector offers **Street Map**, **Nautical Chart**, **Satellite**, and **Hybrid**. These are mutually exclusive background maps. NWS forecast-zone geometry and NOAA satellite smoke remain separate overlays, so either overlay can be shown on any basemap without changing selection or station state.

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

### Wind-speed display units

The browser Wind card provides a **Wind speed** selector with **Knots** and **MPH** choices.

The selection is carried by:

`wind_unit=kts|mph`

The default is `kts`. Wind observations stay internally represented in knots and MPH is calculated only for display using `1 kt = 1.15078 mph`.

The selected display unit applies to the current/gust metrics, wind summaries, historical wind presentation, recent-reading graph/table, nearby-station wind observations, Full Report Details, and HTTP text/compact-text wind output. The browser-facing `/wind-readings` endpoint also returns its formatted wind/gust strings in the selected display unit.

Tidal-current speeds remain in knots. Existing JSON report numeric fields such as `wind_kt`, `gust_kt`, and the established `latest_10` observation data remain knot-based and are not reinterpreted by `wind_unit`.

### NWS forecast and forecast-zone context

For live browser reports, the service uses the **selected location's** latitude/longitude to query the National Weather Service `/points` API. The lookup identifies the NWS forecast zone containing that point. If no selected location exists on initial page load, the committed wind station coordinates may be used temporarily; once the user selects a location, that location becomes authoritative.

The selected point may resolve to a coastal marine zone such as `PZZ530` or to a normal public forecast zone such as `CAZ302` or `CAZ510`. The map control is therefore labeled **Show NWS forecast zone [zone]**, not “marine forecast zone.” When NWS supplies zone geometry, the optional overlay draws that forecast-zone boundary for geographic context. Changing the selected location refreshes the zone ID, checkbox label, and geometry without changing the committed wind station.

For a coastal marine zone, the service retrieves the official NWS coastal marine text product and shows the first four forecast periods in the full-width **NWS Forecast** card. For a non-marine forecast zone, the zone boundary remains useful, but the application does not pretend that a coastal marine forecast exists. Instead of exposing an HTTP 404, the card explains that no coastal marine text forecast is published for that NWS forecast zone.

The card also checks active NWS alerts for the **selected point** and can show up to three distinct alert types. Alert wording is kept general because the selected point may be in a marine or non-marine forecast zone.

Forecast and alert retrieval are browser-focused. Historical reports, text/JSON output, Full Report Details, the station-browser page, and `/voice` do not make additional live NWS forecast requests. NWS retrieval failures do not prevent the rest of the conditions page from rendering.

### Map Overlays control

The main map groups visual overlays in a **Map Overlays** dropdown.

Available overlays:

- **NWS forecast zone** — selected-location forecast-zone geometry when available
- **Satellite smoke (NOAA HMS)** — qualitative NOAA HMS light/medium/heavy smoke analysis
- **Satellite Cloud Cover (NOAA/NESDIS)** — merged GOES-East/West GeoColor rendered dynamically from NOAA/NESDIS `Most_Recent_MERGEDGC` ImageServer for the current map viewport
- **Weather radar (NWS NEXRAD via Iowa State IEM)** — current CONUS N0Q base-reflectivity mosaic using IEM's Web-Mercator `nexrad-n0q-900913` WMS layer; transparent where no precipitation echoes are present

Satellite Cloud Cover and radar are off by default. Radar is sourced from NWS NEXRAD data through Iowa State IEM's current tiled mosaic service. They are visual map context only and do not affect report calculations, selected location, selected wind/current stations, NWS forecast lookup, smoke retrieval, current planning, or text/JSON output.

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

The current release is **1.8.0**.

The v87 refinement keeps release **1.6.0** and renames the generic **Map** basemap choice to **Street Map** for clearer distinction from Nautical Chart, Satellite, and Hybrid. This is a label-only UI change; internal `map_layer` behavior remains unchanged.

The v86 refinement keeps release **1.6.0** and makes the NOAA HMS smoke styling basemap-aware. Street Map and Nautical Chart retain the subdued v83 smoke palette. Satellite and Hybrid use brighter yellow/amber/magenta smoke fills with stronger outlines so the smoke remains distinguishable over brown/green aerial imagery. Changing basemaps restyles any visible smoke layer in place and does not alter smoke data, selected location, stations, center, zoom, or NWS forecast-zone state.

The v113 refinement keeps release **1.8.0** and reduces the **Latest Wind Readings** table viewport so it shows roughly three observation rows plus the sticky header. The remaining readings stay available through the table's internal scroll, keeping the Wind card compact while preserving the full selected history window. No wind data, graph, history selection, or report behavior changes.

The v112 refinement keeps release **1.8.0** and declutters the **Choose Location** card by moving its basemap/provider and nautical-chart disclaimer to a compact **Map & Data Sources** reference card near the bottom of the page, immediately above **Need the details?**. The moved copy also updates the stale `visible-cloud` wording to **Satellite Cloud Cover**. No map provider, overlay, station-selection, or report behavior changes.

The v111 refinement keeps release **1.8.0** and improves the HTML Bottom Line presentation. Instead of rendering the latest wind as a prose sentence, the Bottom Line now reuses the Wind card's compact **Direction / Wind / Gust / Air temp** metric tiles with a smaller **Latest observation** time caption. Only the HTML presentation changes: the existing prose Bottom Line remains intact for text, compact JSON, full-detail text, and voice output so those interfaces retain their established wording and compatibility.

The v110 refinement keeps release **1.8.0** and improves the recent-wind graph interaction. The horizontal time axis now uses an adaptive set of roughly 4–6 labels across the selected history window instead of only first/middle/last. The graph also adds a direct inspection cursor: tap/click or drag across the plot to snap to the nearest actual observation and show its time, sustained wind, and gust in a compact readout above the chart. A vertical guide and temporary wind/gust markers identify the inspected observation only while using the cursor; the normal graph remains the clean no-dot solid-line presentation. Keyboard Left/Right and Home/End inspection is supported when the SVG has focus. No wind data or report calculations change.

The v109 refinement keeps release **1.8.0** and removes the individual observation dots from the recent-wind graph. Sustained wind and gust remain continuous solid lines, while the readings table continues to provide the exact observation values. No wind data, history-window, report, or map behavior changes.

The v108 refinement keeps release **1.8.0** and simplifies the recent-wind graph. The gust series is now a solid line rather than dashed, with a slightly thinner stroke than sustained wind. The legend uses the same solid treatment. No wind data, history-window, report, or map behavior changes.

The v107 refinement keeps release **1.8.0**. NWS forecast zones now use a two-layer boundary so they remain visible on Satellite/Hybrid: a wide dark halo under a bright cyan line with only a faint interior tint. Street Map/Nautical retain a simpler blue boundary with a light halo. The cloud option is renamed **Satellite Cloud Cover (NOAA)** while keeping the same NOAA/NESDIS GeoColor backend. The Wind card history selector is now time based — **1h, 4h, 8h, 12h, 16h, 20h, 24h** — with **4h default**. `/wind-readings` now uses `wind_hours`, and Full Report Details carries the same window. The legacy `latest_10` JSON field name is retained for compatibility.

The v106 refinement keeps release **1.8.0** and replaces the coarse nowCOAST visible-cloud WMS with NOAA/NESDIS **Most_Recent_MERGEDGC GeoColor** from the ArcGIS ImageServer. Instead of stretching fixed weather tiles, the browser requests `/exportImage` for the map's actual EPSG:3857 viewport and screen size, using bilinear interpolation and up to 2× device-pixel resolution. The returned image is overlaid on exactly the requested bounds and refreshed after pan/zoom, so NOAA performs the viewport rendering rather than Leaflet magnifying a coarse WMS mosaic. The old zoom-6 cloud cutoff is removed for evaluation. v106 also removes routine radar loading/success diagnostics; only genuine radar/cloud failures are shown. Radar source and v95 smoke styling are otherwise unchanged.

The v105 refinement keeps release **1.8.0** and replaces the main map's expanded Leaflet basemap radio panel with a custom **Map Types** dropdown beside **Map Overlays**. Street Map, Nautical Chart, Satellite, and Hybrid remain mutually exclusive radio choices. The underlying basemap layers are unchanged; the new control drives the same layer objects, preserves `map_layer` in the browser URL, and reasserts active cloud/radar/smoke overlay stacking after every type change. The nearby-station map's compact Leaflet selector is unchanged. Radar, cloud, and smoke data/provider behavior is unchanged.

The v104 refinement keeps release **1.8.0** and tightens the visible-cloud usefulness limit from zoom 8 to zoom 6. Testing showed the NOAA GOES visible mosaic was still too coarsely resampled at zoom 8 for local Bay Area use. v104 therefore treats the cloud overlay as a regional/synoptic layer: it automatically hides above zoom 6, displays a short zoom-out message, and restores automatically when returning to zoom 6 or lower. Radar and smoke behavior are unchanged.

The v103 refinement keeps release **1.8.0** and limits the visible-cloud overlay to zoom level 6 or lower. NOAA GOES visible imagery is a large-area meteorological product rather than street-level imagery, so allowing Leaflet to continue displaying it at close zoom produced coarse gray/blocky resampling. v103 automatically removes the cloud layer above zoom 6, shows a short status message asking the user to zoom out, and restores the layer automatically when zooming back to 6 or lower. Radar and smoke behavior are unchanged.

The v102 refinement keeps release **1.8.0** and fixes weather-overlay stacking across basemap changes. Cloud imagery now renders in a dedicated Leaflet pane at z-index 430 and radar in a dedicated pane at z-index 440, both above ordinary basemap tiles. Active cloud/radar layers are also explicitly brought to the front when enabled and after every `baselayerchange`, preventing Satellite/Hybrid/Nautical layers from covering them. Smoke/provider behavior is unchanged.

The v101 refinement keeps release **1.8.0** and fixes the actual reason the new cloud and radar checkboxes were inert. The script constructed both Leaflet overlay objects near the basemap setup, but a later variable-initialization block then executed `cloudLayer = null` and `radarLayer = null`, destroying the references before the checkbox handlers ran. v101 declares each layer at construction time and removes the later destructive null initialization. This also allows the radar loading/success/error diagnostics added in v100 to execute. Radar provider, cloud provider, and v95 smoke styling are otherwise unchanged.

The v100 refinement keeps release **1.8.0** and fixes the radar projection/layer mismatch. Leaflet's map is EPSG:3857/Web Mercator, so the IEM NEXRAD WMS now requests the Web-Mercator-specific layer `nexrad-n0q-900913` instead of `nexrad-n0q`. v100 also adds explicit radar diagnostics: enabling radar reports loading, successful tile count, or tile failures, so a transparent no-echo area can be distinguished from an actual service/rendering failure. Smoke and cloud behavior are unchanged.

The v99 refinement keeps release **1.8.0** and replaces the unsuccessful IEM TMS radar integration with IEM's explicitly documented CONUS NEXRAD Base Reflectivity WMS endpoint, `https://mesonet.agron.iastate.edu/cgi-bin/wms/nexrad/n0q.cgi?`, using Web-Mercator layer `nexrad-n0q-900913`, PNG transparency, and WMS 1.1.1. Smoke and cloud behavior are unchanged.

The v98 refinement keeps release **1.8.0** and changes only Leaflet's addressing mode for the Iowa State IEM radar tiles. IEM documents the service as TMS, so the radar `L.tileLayer` now sets `tms: true`, causing Leaflet to invert the Y tile coordinate instead of requesting the service as standard XYZ tiles. Smoke and cloud behavior are unchanged.

The v97 refinement keeps release **1.8.0** and corrects the Iowa State IEM radar tile URL. The radar overlay now uses the documented current NEXRAD TMS path `https://mesonet.agron.iastate.edu/cgi-bin/wms/nexrad/n0q.cgi?`. The radar product remains the current nationwide NEXRAD base-reflectivity mosaic. Cloud and smoke behavior are unchanged.

The v96 refinement keeps release **1.8.0** and changes only the radar transport. The unsuccessful browser-side NOAA nowCOAST WMS radar layer is replaced by Iowa State University's documented current NEXRAD N0Q Web-Mercator tile service, `https://mesonet.agron.iastate.edu/c/tile.py/1.0.0/nexrad-n0q/{z}/{x}/{y}.png`. The tiles are generated from NWS NEXRAD base-reflectivity data and are designed for standard slippy-map clients such as Leaflet. The v95 smoke fill and outline styling is unchanged.

The v95 refinement keeps release **1.8.0** and restores stronger Satellite/Hybrid smoke fills after v94's separate outline layer made polygon interiors too faint. The dedicated outline layer remains unchanged, but light/medium/heavy imagery fills are raised to approximately 18% / 24% / 30% opacity so smoke density is visible inside each boundary without obscuring the basemap.

The v94 refinement keeps release **1.8.0** and replaces the unsuccessful v92/v93 weather-overlay wiring with NOAA nowCOAST GeoServer WMS services. Visible clouds use `/geoserver/observations/satellite/ows` with `global_visible_imagery_mosaic`; radar uses `/geoserver/observations/weather_radar/ows` with `conus_base_reflectivity_mosaic`. Both use WMS 1.1.1. Radar is naturally transparent where no precipitation echoes exist. v94 also renders smoke boundaries in a separate top-layer GeoJSON outline so overlapping HMS fills cannot bury the border. Satellite/Hybrid use a thick cyan outline; Street Map/Nautical use a dark neutral outline.

The v93 refinement keeps release **1.8.0** and corrects the new NOAA cloud/radar overlay definitions after v92 produced empty layers. Visible clouds now use NOAA nowCOAST's satellite imagery WMS visible-cloud layer `25`, and radar uses the nowCOAST weather-radar WMS endpoint with `conus_base_reflectivity_mosaic`. Satellite/Hybrid smoke polygons also gain a cool cyan outline so the warm yellow/amber/magenta fills remain distinct over brown/orange aerial imagery.

Release 1.8.0 consolidates visual map layers under a **Map Overlays** dropdown. The NWS forecast-zone and NOAA HMS smoke toggles move into that menu, and two new off-by-default NOAA overlays are added: **Visible satellite clouds** from NOAA nowCOAST GOES visible imagery and **Weather radar** from NOAA/NWS nowCOAST base-reflectivity imagery. All overlays remain independent of the Street Map/Nautical Chart/Satellite/Hybrid basemap choice and do not change map selection, center, zoom, station, forecast, or report state.

Release 1.7.1 publishes the v90 map-resize refinement as a micro-version update. There are no additional behavior changes in v91 beyond the version bump and documentation alignment.

The v90 refinement makes the main Leaflet map vertically resizable. A drag handle directly below the map lets the user increase or decrease map height without changing center, zoom, selected location, wind/current stations, basemap, NWS forecast-zone state, or NOAA smoke state. Leaflet `invalidateSize()` is called during/after resizing so tiles and overlays redraw correctly. The handle also supports keyboard resizing with Up/Down arrows.

The v89 refinement keeps release **1.7.0** and fixes the remaining Bottom Line/voice path so the **Latest wind** sentence follows the selected `wind_unit`. Current-speed sentences in the same Bottom Line remain in knots. Compact JSON Bottom Line strings also honor the requested display unit, while their existing numeric `wind_kt` and `gust_kt` fields remain knot-based. The shared afternoon wind-statistics text is also made unit-aware for consistency.

Release 1.7.0 adds a global browser **Wind speed** selector with **Knots** and **MPH** choices. The selected unit is carried by `wind_unit=kts|mph` and applies to HTML wind metrics and summaries, recent-reading graph/table, nearby-station wind observations, historical wind presentation, Full Report Details, and HTTP text/compact-text wind reporting. Internal NDBC values remain in knots, tidal-current speeds remain in knots, and established JSON numeric fields remain knot-based for compatibility.

Release 1.6.0 expands the Leaflet basemap selector to four mutually exclusive choices: **Street Map** (OpenStreetMap), **Nautical Chart** (NOAA ENC-based Chart Display Service), **Satellite** (Esri World Imagery), and **Hybrid** (Esri imagery with reference labels). Changing the basemap preserves map center, zoom, selected location, selected wind/current stations, NWS forecast-zone state, and NOAA smoke-overlay state. The selected basemap is preserved in generated map URLs through the `map_layer` query parameter.

Release 1.5.1 updates the browser product branding from **Mauri's Wind & Current Conditions** to **Mauri's Weather & Water Conditions** across the main report, welcome page, browser metadata, page titles, and footers. No forecast, wind, current, geolocation, smoke-overlay, or map-selection behavior changes in this release.

Release 1.5.0 adds the **◎ My location** map control, generalized **NWS Forecast** / **NWS forecast zone** handling for both marine and non-marine selected points, and the optional NOAA HMS satellite-smoke overlay. Browser geolocation, map clicks, and direct coordinates all feed the same authoritative selected-location state. Changing that location refreshes NWS forecast-zone context while leaving the committed wind station unchanged until the user explicitly selects another station. The smoke overlay is qualitative light/medium/heavy satellite analysis, is off by default, and uses deliberately light opacity so the sailing map remains readable.

Release 1.4.0 introduced selected-location-driven NWS coastal marine forecast support, active NWS alerts, and forecast-zone geometry on the Leaflet map. It also retained the random Yogi Berra quote on the hero image introduced after 1.3.0, along with wind history, selected-station air temperature, direct-coordinate entry, nearby-station selection, current planning, tidal context, and the experimental `/voice` endpoint.

Release 1.3.0 added the full-width recent-wind history graph, asynchronous 10/20/30/40/50 observation selection, shared recent-observation depth between the browser and full text report, a simplified current-report layout, compact **Key current times** inside the Tidal Current card, and relocation of the daylight/conditions window into that same current-analysis card.

The Render service can continue building and deploying from the `main` branch. A Git tag marks the exact commit corresponding to a public release without changing the deployment workflow.

For release `1.8.0`, after the final code and README changes are ready:

```sh
git status
git diff
git add main.go README.md
git commit -m "Release v1.8.0: add map overlay controls"
git push origin main
git tag -a v1.8.0 -m "Release v1.8.0"
git push origin v1.8.0
git log --oneline --decorate -5
```

The application displays `1.8.0`, while the corresponding Git tag uses the conventional `v1.8.0` form.

For a later release, update only the static `appVersion` value in `main.go` during the final pre-commit regeneration, update this README when release notes or workflow documentation change, commit and push `main`, then create and push the matching annotated tag.
