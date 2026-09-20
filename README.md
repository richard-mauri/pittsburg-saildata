# Mauri's Weather & Water Conditions

A Go service for San Francisco Bay and Delta sailing conditions. It combines NOAA/NDBC wind observations, NOAA CO-OPS current predictions, National Weather Service forecast context, and optional map overlays into a practical browser dashboard, text reports, JSON output, and a compact voice-oriented Bottom Line.

The default wind station is **PSBC1**.

## Current release

**Public version: 1.9.3**  
**Generated source lineage: v241**

**Current SST status:** deferred/disabled in v195; see **Deferred SST — future approach** below.
**Current chlorophyll status:** both Chlorophyll Field and Chlorophyll Contours are deferred/disabled in v198.

Version 1.9.2 builds on the streamlined browser workflow with clearer observation freshness, better page-loading feedback, and an updated Welcome page that matches the current planning and map functionality. The main conditions page now focuses on **Conditions Now**, including compact wind metrics and a one-day tidal-current graph. The rest of the dashboard is available from a separate **Planning and Details** page, which preserves the active query state and provides the full set of planning, map, current, wind, forecast, and customization controls.




## v241 Marine Places website links

- Marine Places popups now show a compact **Website** link when the generated place record contains a `website` URL.
- Website links open in a new tab/window and are limited in the browser UI to HTTP/HTTPS URLs.
- Advances the standalone Marine Places generator to **v18**. The generator already carried `website` metadata in its place schema; v18 explicitly normalizes curated `www.` values to HTTPS and documents/preserves website metadata for the generated asset.
- Existing curated restaurant records can add a `"website":"https://…"` field without changing the rest of the schema.
- The application cache-busts `assets/marine_places.json` with build v241 so regenerated website metadata is picked up after deployment.
- Runtime identity remains public **Version 1.9.3** and advances generated build to **v241**.


## v240 Marine Places legend and local classification corrections

- Adds Marine Places to the Map Legend using the same category colors as the live map markers: Marinas, Boatyards & Repair, Fuel Docks, Launch Ramps, Marine Supply, Yacht Clubs, and Waterfront Restaurants.
- Advances the Marine Places generator independently to **v12**.
- Adds a persistent override that excludes **Pittsburg Marina** from the Boatyards & Repair category while retaining its legitimate marina/fuel/launch classifications from upstream sources.
- Adds **VeeJay Marine** at 6 Bay Side Dr, Pittsburg as a separate pinned **Boatyards & Repair** place, including aliases and published contact information.
- The application cache-busts `assets/marine_places.json` with build v240 so an updated generated asset is requested after deployment.
- Regenerate the local asset with `./placesgen.sh` after installing generator v12 and the updated `assets/marine_places_overrides.json`.
- Advanced runtime identity to **Version 1.9.3 · Build v240**.


## v239 versioned Marine Places generator

- Renames the standalone Marine Places generator source from the ambiguous `cmd/marineplacesgen/main.go` to **`cmd/marineplacesgen/marineplacesgen-v1.go`**.
- The generator now has its own independent version identity, **Marine Places generator v1**, separate from the web application's generated build lineage.
- Adds `generator_version` to newly generated `assets/marine_places.json` files so the asset records which generator revision produced it.
- The generator prints its version at startup and includes that version in its Overpass request User-Agent and generated source note.
- Refresh the dataset explicitly with `go run ./cmd/marineplacesgen/marineplacesgen-v1.go`. Future generator revisions should use `marineplacesgen-v2.go`, `marineplacesgen-v3.go`, and so on rather than reusing an unversioned `main.go`.
- No Marine Places overlay behavior or dataset contents are changed by this build; this is a generator source/versioning cleanup.
- Advanced runtime identity to **Version 1.9.3 · Build v239**.

## v238 generated Marine Places workflow

- Adds a standalone `cmd/marineplacesgen` utility so `assets/marine_places.json` is no longer intended to be maintained as a hand-entered directory.
- `assets/marine_places_audit.json` is a generated QA/provenance artifact produced alongside `assets/marine_places.json`. It records coordinate provenance and verification status for generated Marine Places data. The file is committed to the repository so generator changes and data-quality results can be reviewed and compared over time, but it is not consumed by the deployed application.
- The generator queries OpenStreetMap through an Overpass interpreter for three coverage regions: San Francisco Bay/Delta, the Half Moon Bay coast, and Santa Cruz/north Monterey Bay.
- It aggregates and normalizes marinas, boatyards/repair facilities, fuel docks, launch ramps, marine-supply locations, yacht/sailing clubs, and waterfront restaurants.
- Waterfront restaurants are filtered geographically around marina/harbor activity rather than accepting every restaurant in the very large regional bounding boxes. Marine fuel results receive a similar marine-context filter so ordinary roadside gas stations are rejected.
- Generated records are deduplicated by OSM object identity and by nearby normalized name/category matches.
- Adds `assets/marine_places_overrides.json` for persistent local corrections. Known places can be pinned, excluded, renamed, or reclassified without losing those corrections on the next generated refresh. The starter overrides pin Pittsburg Marina, Antioch City Marina, KKMI Richmond, Emeryville Marina, Pillar Point Harbor, and Santa Cruz Harbor.
- Adds **Launch Ramps** and **Marine Supply** to the Marine Places overlay menu.
- The deployed application still performs no live places search. It serves only the generated local JSON asset, so normal map use has no Overpass dependency, API key, request quota, or third-party search cost.
- Run the refresh locally from the repository root with `go run ./cmd/marineplacesgen/marineplacesgen-v1.go`. Optional flags include `-out`, `-overrides`, `-endpoint`, and `-timeout`.
- The checked-in v238 JSON remains the existing curated seed plus the new schema/categories; run the generator in the full repository when network access is available to produce the comprehensive refreshed dataset.
- Advanced runtime identity to **Version 1.9.3 · Build v238**.

## v237 expanded Marine Places dataset

- Replaces the very small starter Marine Places asset with a substantially broader Bay/Delta dataset, including the Pittsburg/Antioch area, Richmond, Berkeley, Emeryville, Alameda/Oakland Estuary, San Francisco, Sausalito/Marin, Benicia and Vallejo.
- Adds the obvious missing East Bay facilities that prompted this pass, including Emeryville marinas and a much denser Alameda/Richmond marina and boatyard set.
- Marine-place markers are now larger, solid category-colored markers with a white outline so they are easier to see against Street, Satellite and Hybrid maps.
- Each Marine Places overlay label now shows the number of records loaded for that category.
- The marine-places JSON request is cache-busted by build and served with revalidation headers so a deploy does not keep showing an older asset after the JSON changes.
- The deployed app still reads only the local `assets/marine_places.json`; there is no runtime third-party places/search API dependency.
- Advanced runtime identity to **Version 1.9.3 · Build v237**.

## v236 build repair

- Restores the Aviation Weather Center METAR cache loader that was accidentally dropped while replacing the v234 Nominatim search handlers with the v235 Marine Places overlay work.
- Restores the `cachedMetarObservation` type, short-lived METAR cache, gzip/XML decoding, and `fetchCurrentMetars` closure used by both `/wind-barbs` and `/pressure-observations`.
- This also makes the existing `compress/gzip` import used again, resolving the v235 compile errors without changing the Marine Places overlay behavior.
- Advanced runtime identity to **Version 1.9.3 · Build v236**.

## v235 curated Marine Places overlays

- Retires the generic Nominatim place-search UI from the Planning map. The map no longer mixes unrelated global text-search results into the local boating workflow.
- Adds a new **Marine Places** group under **Map Overlays** with independent toggles for **Marinas**, **Boatyards & Repair**, **Fuel Docks**, **Yacht Clubs**, and **Waterfront Restaurants**.
- Marine-place markers are loaded from the editable `assets/marine_places.json` asset rather than from a third-party search API. This keeps the feature deterministic, fast, free, and independent of Render sleep/restart behavior.
- The starter asset includes a curated Bay/Delta set and explicitly includes **KKMI Richmond** and **KKMI Sausalito** under Boatyards & Repair. Each place can carry a name, aliases, category, city, latitude/longitude, address, and note.
- Clicking a marine-place marker opens a compact popup with the available local details. Marine-place overlays are display-only and do not change the selected conditions location or selected weather/current stations.
- The JSON file is intentionally straightforward to maintain: add or edit records in `assets/marine_places.json`, then deploy normally. No API key, search-session token, cache, rate limit, or external geocoder is required for these overlays.
- Advanced runtime identity to **Version 1.9.3 · Build v235**.


## v234 local-first map search and result cleanup

- Place search now sends the current Leaflet map center and viewport bounds to `/place-search` so the server can search the visible area first instead of ranking a short name globally with no geographic context.
- The server performs a bounded Nominatim search inside the current viewport first. If that produces no matches, it falls back to a global search.
- Returned matches are sorted by distance from the current map center when a center is available, so geographically relevant results are listed ahead of far-away matches.
- The maximum remains **6 search results**. The result list is now height-bounded and independently scrollable, including a shorter mobile height so long names/addresses do not push the map far down the page.
- Selecting a search result centers/zooms the map, keeps the temporary result marker, and collapses the result list.
- Clearing the native search field clears the result list and temporary result marker.
- The existing **Clear location & stations** button now also clears the place-search text, search results, search status, and temporary search marker, making it the single map-state reset control.
- The Clear button is enabled when search text/results/marker are present even if no conditions location or station is selected.
- Search remains explicit-submit only, capped at six matches, cached, and rate-limited for the public Nominatim service; no autocomplete was added.
- Advanced runtime identity to **Version 1.9.3 · Build v234**.

## v233 map place search

- Adds an explicit **Search map** field above the Planning and Details map for cities, addresses, restaurants, landmarks, and other named places.
- Search is submit-only via the **Search** button or Enter key; it does **not** send autocomplete requests while typing.
- The browser calls the app's new `/place-search` endpoint. The Go service proxies the query to OpenStreetMap Foundation's public Nominatim service, returns up to six matches, and keeps the provider URL configurable with `NOMINATIM_BASE_URL`.
- Selecting a result pans/zooms the Leaflet map and places a temporary search marker. It does **not** silently change the selected conditions location; click the map to make a point the selected location.
- Results use the provider bounding box when available so cities/large features frame appropriately, with a point-zoom fallback for smaller places.
- Nominatim requests are serialized to no more than one cache-miss request per second, identify the application with a custom User-Agent, and are cached in memory for 24 hours. The cache is bounded for long-running instances.
- The UI includes OpenStreetMap/Nominatim attribution and a note that place search moves the map only.
- **Public Nominatim usage constraints:** this integration is intended for moderate, end-user-triggered searches only. Do not add client-side autocomplete, bulk/systematic geocoding, or scheduled queries. The public service's absolute maximum is one request per second per application, repeated queries should be cached, and the provider may require migration to another service. See the OSMF Nominatim Usage Policy before materially expanding search traffic.
- Advanced runtime identity to **Version 1.9.3 · Build v233**.

## v232 wind-chart Safari artifact fix

- Fixes the stray vertical line at the far left of the **Latest wind readings** graph and the small clipped gust fragment that could appear above it in Safari/WebKit on both macOS and iOS.
- The interactive chart inspector no longer relies on the SVG `hidden` attribute, which WebKit could partially paint before the inspector was activated.
- Inspector visibility is now controlled explicitly with `display:none` / `display`, and its cursor line and marker circles are initialized at the chart's actual plot boundary instead of SVG coordinate zero.
- Sustained-wind and gust data, chart scaling, history selection, tap/drag inspection, keyboard inspection, and mobile label sizing are unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v232**.

## v231 iOS wind-readings scroll cue

- The **Latest wind readings** table keeps its compact independently scrollable list on narrow/mobile screens.
- Because iOS Safari normally hides its native overlay scrollbar until scrolling begins, the list now shows a subtle bottom fade and **Scroll for more ↓** cue whenever additional readings are below the visible area.
- The cue automatically disappears when the user reaches the end of the readings and reappears if the list is scrolled upward again.
- The cue is only shown when the list actually overflows; short histories that fit completely in the panel do not show it.
- Changing the wind-history interval resets the readings list to the top and recalculates the cue after the updated observations are rendered.
- Desktop layout and the wind-history data itself are unchanged.

## v230 UI consistency

- Map Types, Map Overlays, and Center Map now show an explicit **× close button on desktop as well as mobile**.
- Clicking outside any of those map menus now closes it on macOS/desktop browsers too.
- Narrow/mobile layouts retain dismissal-only outside taps so the same tap cannot also select a map location or activate a control beneath the sheet.
- Existing modal information popups continue to provide their visible × button, backdrop/outside dismissal, and Escape dismissal.
- Native `<details>/<summary>` opening behavior remains untouched to avoid the prior iOS Safari Map Overlays regression.

## What it does

The service is designed to answer two practical questions:

1. What is the wind doing now?
2. What are the tidal currents expected to do during the preferred planning period?

Wind observations come from NOAA/NDBC. Current predictions come from NOAA CO-OPS. Forecast-zone context and marine forecast information come from the National Weather Service. Optional map context includes NOAA/NESDIS satellite cloud cover, NOAA HMS smoke analysis, NEXRAD radar imagery, and an observational sea-level-pressure/isobar layer derived from current NOAA/NWS Aviation Weather Center METAR data.

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
- shared page-level wind units control: knots or MPH, synchronized between Conditions Now and Planning and Details
- shared page-level distance units control: nautical miles or statute miles, synchronized between Conditions Now and Planning and Details

The recent-wind selector uses:

```text
wind_hours=1|4|8|12|16|20|24
```

The default history window is 4 hours.

Wind data remain internally represented in knots. MPH conversion is display-only. The shared `wind_unit` preference appears near the top of both Conditions Now and Planning and Details, carries across navigation, and also controls wind-barb tooltip values. Wind-barb geometry remains based on standard knot increments. Existing JSON fields such as `wind_kt` and `gust_kt` retain knot semantics.

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

A user establishes the selected location by clicking the map. The selected location is distinct from the map viewport center, and panning or zooming does not change it.

The latitude and longitude fields are intended to display the current map viewport center. v127 resolves those input elements directly during each Leaflet synchronization and listens across drag/move/zoom completion paths. A user may edit either field normally; typing does not select a sailing location or move the map. The edited pair is applied only by choosing **Center Map → Latitude & Longitude**, which validates the coordinates and pans the map there while preserving zoom.

Nearby wind-station discovery is centered on the selected location. Candidate wind stations are shown only when an actual selected location exists; the default map does not display candidate markers merely because the server has a default wind-station candidate list. Candidates are previewed before the user commits one as the wind source.

The **Map Types**, **Map Overlays**, and **Center Map** dropdowns share one map-control row. The **Center Map** dropdown uses momentary action buttons that pan the map without changing the zoom level or the selected sailing/report location. It includes:

- My location
- Latitude & Longitude
- selected location
- selected wind station
- selected currents station

**My location** uses browser geolocation only as a map-centering action. It does not commit a new selected location, change station selection, or alter report calculations. Center Map items do not remain selected after use, so the same action can be invoked repeatedly after manually panning the map.

Recenter actions preserve the current zoom level. The selected currents station associated with the active wind station is always shown on the map when available; there is no separate visibility checkbox. Clearing the selected location also clears the committed wind-station map selection, its associated currents-station marker, and the wind-station candidates derived from that location. The clear action removes `lat`, `lon`, `station`, `current_station`, and `bin` from the current browser URL while leaving the latitude/longitude fields showing the current viewport center. The button is labeled **Clear selected location, station & candidates**.

The **Location** card reserves a compact **Local Conditions** panel beside the Latitude/Longitude controls on wider screens, stacking below them on narrow displays. The visible instructions are intentionally brief; an **ⓘ About location selection** popover explains selected-location versus map-center behavior, Center Map actions, and nearby-station selection on demand. The help popover has an explicit **×** close control, closes when the user taps outside it, and treats the first outside tap as dismissal-only so the same tap does not activate the map or another control underneath. When a selected ★ location exists, the panel uses the NWS point forecast for that latitude/longitude and displays the NWS nearby city/state from `relativeLocation`, the current-hour forecast air temperature, the next applicable daytime high and nighttime low, and a short forecast phrase. This weather context is informational only and does not alter wind-station or currents-station selection.

## Map Types

The **Map Types** dropdown provides mutually exclusive basemaps:

- Street Map
- Nautical Chart
- Satellite
- Hybrid

Changing basemap does not change location, station, forecast, current, or planning state.

The map also shows a compact live scale/status row immediately below the map. It reports one distance system at a time—nautical miles or statute miles, according to the shared Distance units preference—together with the current Leaflet zoom level. The previous pixel-count text and dual-unit display are removed. NOAA Nautical Chart is available at Zoom 9 or closer. If Nautical is the preferred map type and the user zooms farther out than Zoom 9, the app temporarily displays Street Map and automatically restores Nautical when the map returns to Zoom 9+; the Nautical preference is retained.

## Map overlays

The **Map Overlays** control supports independent visual overlays. The current groups are **Weather & Hazards**, **Wind & Pressure**, **Terrain & Seafloor**, and **Observations**. The Wind & Pressure group keeps the two wind-barb layers beside the pressure/isobar layer so the observed wind field and pressure-gradient context can be viewed together.

- **NWS forecast zone**
- **Satellite smoke (NOAA HMS)**
- **Satellite Cloud Cover (NOAA/NESDIS)**
- **Weather radar (NWS NEXRAD via Iowa State IEM)**
- **Surface Pressure / Isobars (NOAA/NWS METAR)**

Satellite Cloud Cover is rendered from NOAA/NESDIS merged GOES GeoColor imagery for the current map viewport.

Radar uses the current NEXRAD base-reflectivity mosaic through Iowa State IEM's Web-Mercator WMS service.

Surface Pressure / Isobars uses current mean sea-level-pressure observations from the NOAA/NWS Aviation Weather Center METAR feed already used for the land/inland wind-barb network. The Go server exposes the current pressure observations for a padded map area through `/pressure-observations`; the browser interpolates those point values into a smooth observational pressure field and draws labeled isobars. Contour spacing is zoom-dependent: **4 mb** at wide-area zooms, **2 mb** at regional zooms, and **1 mb** when zoomed in. A white halo keeps the purple pressure contours readable over street, nautical, satellite, radar, and cloud imagery.

This pressure layer is intended to show the approximate pressure-gradient pattern around the map view. It is **not an official analyzed surface chart** and can contain interpolation uncertainty where the METAR network is sparse. Observations older than three hours are excluded, and the overlay reports when too few current pressure stations are available to construct contours.

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
/pressure-observations
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

The current release candidate is **1.9.3**. Generated source builds also carry a separate `buildVersion` identifier so test clients can distinguish successive 1.9.3 candidates.

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
- NOAA/NWS Aviation Weather Center METAR observations for the optional land/inland wind-barb layer

## Useful Bay and Delta wind stations

Useful references include PSBC1 for Pittsburg/Suisun Bay, PCOC1 for Port Chicago, MZXC1 for Martinez, UPBC1 for the Martinez bridge area, DPXC1 for Davis Point, RCMC1 and PPXC1 for Richmond, TIBC1 for Tiburon, and FTPC1 for the central/southern Bay.

These are reference stations, not a hard-coded application whitelist. Active stations are discovered dynamically.

## Maintainability

`main.go` remains intentionally large and contains substantial browser HTML, CSS, JavaScript, Leaflet behavior, HTTP orchestration, and report presentation logic.

A future refactor should be treated as a separate behavior-preserving project after the current UI and release behavior are stable. The safest direction would be to move browser templates/static assets out of `main.go` first, then separate HTTP/report orchestration while preserving the existing `wind.go` and `currents.go` data-source boundaries.

## v229 / 1.9.3 changes

- Moves the always-visible **Map Legend** into a compact **ⓘ Map legend** dialog while preserving the existing selected-location, wind-station, current-station, nearby-station, and Saildrone symbols.
- Moves the separate always-visible **Map & Data Sources** card into a compact **ⓘ Map & data sources** dialog alongside the map legend control. The source wording and navigation disclaimer are preserved.
- Both map information dialogs reuse the established desktop dialog / iOS bottom-sheet pattern with a dedicated **× close button**, backdrop/outside-tap dismissal, Escape-key dismissal, internal scrolling, and safe-area handling.
- Improves **Latest Wind Speed** graph readability on narrow/iOS screens by using a mobile-sized SVG viewBox instead of scaling the fixed 760-unit desktop chart down to phone width. Mobile rendering now uses larger axis/tick labels, more axis room, and at most four time labels while retaining every wind and gust observation in the plotted series.
- Desktop wind-chart dimensions remain essentially unchanged. No observation retrieval, wind conversion, station selection, map data source, or map-layer behavior changed.
- Advanced runtime identity to **Version 1.9.3 · Build v229**.

## v228 / 1.9.3 changes

- Moves the long **Tidal Current** chart explanation out of the always-visible card body and into a compact **ⓘ About tidal current chart** dialog while keeping the concise flood/ebb/slack explanation visible.
- Moves the Planning Hint **How these settings work** explanation into a compact **ⓘ About planning settings** dialog while keeping the actual planning controls, daily statuses, and disclaimer visible.
- Reuses the established dialog/sheet interaction pattern: dedicated header row, visible **× close button**, backdrop/outside-tap dismissal, Escape-key dismissal, internal scrolling, iOS safe-area handling, and background-scroll suppression while open.
- Preserves the existing NOAA current-chart explanation, threshold logic, planning settings, current calculations, classifications, and report semantics; this is a presentation-only cleanup.
- Advanced runtime identity to **Version 1.9.3 · Build v228**.

## v227 / 1.9.3 changes

- Replaces the always-visible **Tidal & Lunar Context** card with a compact full-width trigger, reducing the Planning and Details page length while preserving the existing tidal/lunar calculations and wording.
- Opens the same tidal/lunar content in a dismissible dialog on desktop and a bottom-sheet style panel on narrow/mobile screens.
- Adds a dedicated **× close button**, backdrop/outside-tap dismissal, Escape-key dismissal, internal scrolling, iOS safe-area handling, and background-scroll suppression while the dialog is open.
- Keeps the existing `tide-context-card` content ID inside the dialog so current-date partial refreshes continue updating the tidal/lunar content; the compact trigger label and dialog title also refresh with the selected date.
- No tide calculations, lunar-cycle calculations, current predictions, thresholds, map behavior, overlay behavior, or report semantics changed.
- Advanced runtime identity to **Version 1.9.3 · Build v227**.

## v226 / 1.9.3 changes

- Reworks the **ⓘ About location selection** popup so its title and **× close button** occupy a dedicated header row. The help body begins below that header, eliminating the v225 text overlap.
- Keeps the location-help header sticky on narrow/mobile layouts while the help body scrolls beneath it; the close button retains its 44 px touch target on phones.
- Changes the **Planning and Details / Back to Conditions Now** pill background from navy to the same blue used by **Find nearby stations** and **Clear location & stations**, removing an unnecessary visual mismatch between primary navigation/actions.
- No location-selection, station-selection, map, overlay, unit, weather, current, or report behavior changes.
- Advanced runtime identity to **Version 1.9.3 · Build v226**.

## v225 / 1.9.3 changes

- Adds an explicit **× close button** to the **ⓘ About location selection** help popover, including a 44 px touch target on narrow/mobile layouts.
- Adds reliable outside-tap/click dismissal for the location help. Taps inside the help remain interactive, and tapping the ⓘ summary still uses native `<details>` toggle behavior.
- Makes the first outside tap dismissal-only so closing the help on iOS does not also select a map point or activate a control underneath.
- Escape now closes the location help as well as an open mobile map menu.
- No map-selection, station-selection, weather, overlay, or report behavior changes beyond help-popover dismissal.
- Advanced runtime identity to **Version 1.9.3 · Build v225**.

## v224 / 1.9.3 changes

- Renames the Planning and Details map card from **Choose Location** to the more general **Location**.
- Replaces the long always-visible location instructions with one short sentence: click the map to select a location for local conditions and nearby stations.
- Adds an **ⓘ About location selection** popover beside the Location heading. The popover explains selected-location versus map-center state, Center Map behavior, Latitude/Longitude centering, nearby-station discovery, and committing a candidate wind station.
- Removes sailing-specific wording from the active location/map help where it is not needed; the underlying selected-location, station-selection, weather, and map behavior is unchanged.
- Uses a compact anchored help popover on larger screens and a bounded fixed, scrollable help sheet on narrow/mobile screens.
- Advanced runtime identity to **Version 1.9.3 · Build v224**.

## v223 / 1.9.3 changes

- Fixes the remaining mobile **Map Overlays** visibility regression introduced in v221. The menu was opening, but its panel could be positioned completely off-screen on narrow displays.
- Root cause: the older desktop-only `#map-overlays-menu` bottom-positioning rule had higher CSS specificity than the newer generic mobile fixed-sheet rule, so Safari kept `bottom: calc(100% + 6px)` even after the panel switched to `position: fixed`.
- Adds an explicit narrow-screen `#map-overlays-menu > .map-overlays-panel` override with matching specificity so the sheet is anchored inside the viewport using the safe-area bottom inset.
- Keeps native `<details>/<summary>` opening behavior, the mobile **× close button**, outside-tap dismissal, Escape dismissal, scrolling, and all overlay data behavior unchanged.
- **Map Types** and **Center Map** retain the v222 behavior; this correction is specific to the Map Overlays positioning conflict.
- Advanced runtime identity to **Version 1.9.3 · Build v223**.

## v222 / 1.9.3 changes

- Fixes the v221 iOS regression where **Map Overlays** could fail to open after the shared mobile menu-coordination logic was added.
- Restores native `<details>/<summary>` opening behavior for **Map Types**, **Map Overlays**, and **Center Map**; no JavaScript toggle listener participates in opening or coordinating those menus.
- Keeps the v221 **× close buttons** on all three mobile/narrow-screen sheets. Each close button only closes its own already-open menu.
- Keeps outside-tap dismissal and Escape-key dismissal. The first outside tap remains dismissal-only so it cannot activate the map underneath.
- Removes the v221 behavior that automatically closed another map menu through `toggle` event handling, favoring the simpler Safari-safe native interaction path.
- Desktop behavior and all map/overlay data behavior are unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v222**.

## v221 / 1.9.3 changes

- Adds a prominent **× close button** to the mobile/narrow-screen **Map Types**, **Map Overlays**, and **Center Map** sheets.
- The close control uses a 44 px touch target, remains available at the top while the sheet scrolls, and closes only the menu without changing map or overlay state.
- Extends the existing mobile fixed-sheet treatment to all three map menus for consistent iOS behavior.
- Keeps outside-tap dismissal and Escape-key dismissal; the first outside tap remains dismissal-only so it cannot accidentally activate the map underneath.
- Opening one mobile map menu automatically closes either of the other two if it is already open.
- Desktop dropdown behavior remains unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v221**.

## v220 / 1.9.3 changes

- Fixes the below-map scale readout so it follows the browser's current **Distance units** selector rather than relying only on the server-rendered distance-unit value.
- The scale now resolves units from the live selector first, then the `distance_unit` URL parameter, with the server-rendered value only as a fallback.
- Changing **Nautical Miles / Miles** updates the scale immediately before the normal navigation refresh, preventing Safari/iOS from showing `nmi` while the selector displays **Miles**.
- Leaves the v219 distance calculations, below-map scale placement, candidate/current distance formatting, and all overlay behavior unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v220**.

## v219 / 1.9.3 changes

- Adds a shared page-level **Distance units** preference beside **Wind units** immediately below the hero image on both Conditions Now and Planning and Details. Distance choices are **Nautical Miles** and **Miles**, persisted through the `distance_unit` query parameter.
- Moves the live map scale completely out of the Leaflet map and into a compact status row immediately below the map, eliminating overlap with markers, popups, attribution, and touch targets.
- Simplifies the scale text by removing the screen-pixel sample and showing only the selected distance system plus zoom, for example `Scale: 3.64 nmi · Zoom 10` or `Scale: 4.18 mi · Zoom 10`.
- Applies the selected distance unit to nearby wind-station distances and currents-station preview distances used by the map candidate workflow.
- Keeps all internal geographic distance calculations in nautical miles and converts only for display.
- Retains the v218 matched Find/Clear station controls, iOS popup stacking fixes, wind barbs, isobars, and overlay behavior unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v219**.

## v218 / 1.9.3 changes

- Fixes iOS/narrow-screen wind-station candidate popups so Leaflet popup content renders above the custom map scale/status badge and attribution controls instead of being obscured by them.
- Makes the map scale/status badge and attribution non-interactive for pointer/touch input; on narrow screens the bottom-right control stack is also kept below the popup pane so station-selection links remain tappable.
- Reorganizes the primary location/station actions into a matched pair directly below the Map Types / Map Overlays / Center Map row: **Find nearby stations** and **Clear location & stations**.
- Gives both actions the same size, color, height, and pill styling; they remain side by side when space allows and stack full-width on narrow phones.
- Keeps existing station-selection, candidate-search, clear-state, overlays, wind barbs, isobars, and iOS Map Overlays sheet behavior unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v218**.

## v217 / 1.9.3 changes

- Reorganizes **Map Overlays** so the atmospheric layers used together for wind analysis are grouped under a dedicated **Wind & Pressure** category.
- Moves **Marine / Bay Wind Barbs (NOAA/NDBC)** and **Land / Inland Wind Barbs (METAR)** out of **Observations** and into **Wind & Pressure**.
- Moves **Surface Pressure / Isobars (NOAA/NWS METAR)** out of **Weather & Hazards** and into **Wind & Pressure** so pressure-gradient context sits beside the wind field it helps explain.
- Leaves **Weather & Hazards** for NWS forecast zone, NOAA HMS smoke, NOAA satellite cloud cover, and NOAA/NWS radar; **Observations** now contains Saildrone Observations.
- Changes menu organization only; overlay data sources, rendering, caching, interaction, and map-state behavior are unchanged from v216.
- Advanced runtime identity to **Version 1.9.3 · Build v217**.

## v216 / 1.9.3 changes

- Fixes the **Clear selected location, station & candidates** enable/disable logic so the button remains available whenever any clearable map state exists: a selected sailing location, committed wind station, selected currents station, or nearby wind-station candidates.
- Removes the previous selected-location-only click guard, allowing the same clear action to remove committed wind/current station markers even after the selected sailing location has already been cleared.
- Aligns the server-rendered initial button state with the browser state logic, preventing the control from starting disabled when a wind/current station or candidate set is still present.
- Leaves the v215 isobar overlay, v214 iOS Map Overlays behavior, wind-barb overlays, and shared wind-unit behavior unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v216**.

## v215 / 1.9.3 changes

- Adds **Surface Pressure / Isobars (NOAA/NWS METAR)** under **Map Overlays → Weather & Hazards**.
- Extends the existing Aviation Weather Center complete-METAR cache parser to retain current mean sea-level pressure when the feed supplies either `sea_level_pressure_mb` or `slp_mb`.
- Adds `/pressure-observations`, which returns current METAR sea-level-pressure observations from a padded area around the map view and excludes observations older than three hours.
- Generates the pressure field locally in the browser using distance-weighted interpolation and draws labeled isobars with a high-contrast halo so the contours remain readable over all basemaps and other weather layers.
- Uses zoom-aware contour spacing: **4 mb** at wide-area zooms, **2 mb** at regional zooms, and **1 mb** at close zooms, so Bay/Delta pressure gradients remain visible without overcrowding statewide views.
- Refreshes the isobar layer after map movement while keeping it display-only: it does not change selected location, wind station, currents station, planning thresholds, or report calculations.
- Clearly labels the layer as an **interpolated observational field**, not an official NOAA analyzed surface chart; sparse-data and insufficient-range cases are reported in the map status area rather than silently drawing misleading contours.
- Keeps the v214 iOS Map Overlays dismissal behavior and all v213/v211 wind-barb and shared-units behavior unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v215**.

## v214 / 1.9.3 changes

- Refines the v212 iOS/narrow-screen **Map Overlays** fixed sheet so it can be dismissed by tapping anywhere outside the open panel instead of requiring a second tap on the **Map Overlays** button.
- Taps inside the Map Overlays sheet, including overlay checkboxes and accordion controls, keep the sheet open so multiple layers can still be changed in one visit.
- The first outside tap is dismissal-only: it is intercepted before the underlying Leaflet map or another control can receive the same touch, avoiding accidental map-location selection while closing the sheet.
- Adds Escape-key dismissal for keyboard users without changing desktop overlay positioning or any map-layer behavior.
- Keeps the v213 selected-location/station clear behavior and all wind-barb loading, caching, unit, and data-source behavior unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v214**.

## v213 / 1.9.3 changes

- Expands the map clear action to **Clear selected location, station & candidates**.
- Clearing now removes the committed selected-wind-station marker/state in addition to the selected sailing location and nearby candidate markers.
- Also removes the currents-station marker associated with that selected wind station so it cannot remain as stale map state after the wind selection is cleared.
- Removes `lat`, `lon`, `station`, `current_station`, and `bin` from the current browser URL with `history.replaceState`, so subsequent navigation/reload no longer carries the cleared explicit station/current overrides.
- Existing wind-barb overlay state is left intact, so a barb previously hidden beneath the selected wind-station marker becomes immediately tappable after clearing.
- Keeps the v212 iOS Map Overlays sheet fix and all wind-barb data/loading behavior unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v213**.

## v212 / 1.9.3 changes

- Fixes the iOS/narrow-screen Map Overlays interaction bug where Leaflet scale/attribution controls could render above the overlay panel and intercept taps.
- On viewports 700 px wide or narrower, the Map Overlays panel now becomes a fixed viewport sheet with an application-level z-index above Leaflet controls.
- The mobile panel respects iOS safe-area insets, uses dynamic viewport height (`100dvh`), and scrolls internally with momentum touch scrolling when its contents exceed the available height.
- Desktop/tablet overlay positioning and all map-layer behavior are unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v212**.

## v211 / 1.9.3 changes

- Moves the shared **Wind units: Knots / MPH** display preference from above the hero image to immediately below the large hero image on both **Conditions Now** and **Planning and Details**.
- Keeps the v210 synchronized `wind_unit` behavior unchanged: the selected unit continues to carry between the two browser pages and controls wind-card values, history displays, nearby-station values, and wind-barb tooltip units.
- Keeps wind-barb geometry internally knot-based and leaves all v209 wind-barb stability/caching behavior unchanged.
- This build is a presentation-placement refinement only; it does not change data sources, calculations, station selection, map behavior, or report semantics.
- Advanced runtime identity to **Version 1.9.3 · Build v211**.

## v210 / 1.9.3 changes

- Moves the Wind Units selector out of the Wind card and into a shared page-level display-preferences control near the top of the browser page.
- Shows the same **Wind units: Knots / MPH** preference on both **Conditions Now** and **Planning and Details**. Both pages use the existing `wind_unit` query parameter, so the setting stays synchronized when navigating between them.
- Makes wind-barb data requests use the active page wind-unit preference instead of forcing knots. NOAA/NDBC marine/Bay tooltip text and Aviation Weather Center METAR land/inland tooltip text now follow the selected display unit.
- Keeps wind-barb geometry meteorologically standard: symbols are still constructed from internally normalized knot speeds even when the tooltip is displayed in MPH.
- Removes the duplicate unit selector from the Wind card so wind units are clearly an application-wide display preference rather than a card-local setting.
- Retains v209 deterministic world-grid thinning, station caching, and buffered panning behavior unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v210**.

## v209 / 1.9.3 changes

- Stabilizes wind-barb thinning during map panning. The browser now assigns stations to a fixed Leaflet world-pixel grid at the current zoom instead of using viewport-relative screen coordinates, so a small pan at the same zoom does not cause nearby stations to trade places.
- Makes thinning deterministic inside each world-grid cell: marine/NDBC observations retain priority when both layers are enabled, then newer observations are preferred, then station ID provides a stable tie-breaker.
- Adds a browser-side observation cache keyed by layer class and station ID. New buffered viewport fetches are merged into the cache instead of replacing the entire wind-barb observation array. Cached entries expire after 20 minutes.
- Expands the wind-barb geographic prefetch buffer from 35% to 70% beyond each side of the visible map. This increases panning hysteresis so ordinary pans stay within already-loaded coverage and do not trigger unnecessary replacement fetches.
- Panning at a fixed zoom now preserves barb selection; changes are expected only when stations naturally enter/leave the visible area, observations update or expire, a source is enabled/disabled, or zoom changes enough to alter density.
- Retains the v208 Aviation Weather Center METAR cache source for land/inland observations, unrestricted viewport-based NOAA/NDBC marine observations, stale-observation fading, and independent marine and inland checkboxes.
- Advanced runtime identity to **Version 1.9.3 · Build v209**.

## v208 / 1.9.3 changes

- Fixes the v207 land-wind failure by replacing the unsupported custom Aviation Weather Center METAR bounding-box request with AWC’s complete current-METAR cache (`/data/cache/metars.cache.xml.gz`). AWC recommends cache files for larger observation sets; the Go server downloads the gzip-compressed XML feed, keeps a short two-minute in-memory cache, and filters observations locally to the buffered map viewport.
- Parses current METAR station ID, observation time, latitude/longitude, wind direction, sustained wind, and gust directly from the AWC cache. Variable/unknown wind direction is omitted when sustained wind is strong enough that a directional barb would be misleading; calm/light observations can still render as a calm circle.
- Removes the v206/v207 hard-coded Bay/coast geographic mask from the NOAA/NDBC marine layer. Every active NDBC station inside the buffered viewport with a usable latest wind observation can now participate.
- Fetches eligible NOAA/NDBC station observations concurrently with a bounded six-request worker limit so wider map views do not require strictly serial station retrieval.
- Reduces screen-space thinning from 58/50/42/34/26 px to 38/34/30/26/22/18 px across zoom levels, retaining substantially more marine and land barbs at statewide and regional views while still suppressing direct symbol collisions.
- Surfaces upstream wind-source warnings in the wind-barb status line instead of silently reporting only that no observations were returned.
- Retains the v205 buffered viewport loading model, independent marine and land checkboxes, source tooltips, and stale-observation fading after 90 minutes. Neither layer changes selected sailing location, wind station, currents station, or report calculations.
- Advanced runtime identity to **Version 1.9.3 · Build v208**.

## v207 / 1.9.3 changes

- Replaces the misleading v206 NDBC-based **Inland Wind Barbs** classification with a genuine land-station feed: **Land / Inland Wind Barbs (METAR)**.
- The land/inland layer is sourced server-side from the NOAA/NWS Aviation Weather Center Data API using current METAR observations from airport weather stations, including many ASOS/AWOS sites. It is independent of the NOAA/NDBC marine network.
- Extends `/wind-barbs` with independent `include_marine` and `include_inland` controls. Marine/Bay observations remain NOAA/NDBC; land/inland observations are Aviation Weather Center METARs.
- Keeps browser access server-side because the Aviation Weather Center Data API does not permit cross-origin browser access. Requests use the buffered map bounds and a project-specific User-Agent.
- METAR wind speed remains in knots. Numeric METAR wind directions are converted to the same 16-point compass labels used by the existing barb renderer; variable/unknown directions are omitted when a directional barb would be misleading. Calm observations can still render as the calm-wind circle.
- Adds source names to barb tooltips so NOAA/NDBC and Aviation Weather Center METAR observations can be distinguished directly on the map.
- Adds zoom-dependent collision thinning for the combined barb field. Wider views keep one observation per screen-space cell, with progressively denser display at higher zooms, reducing the overlapping coastal clusters visible at statewide scale. Marine/Bay observations are given collision priority when both layers are enabled.
- Retains the v205 buffered viewport loading model, stale-observation fading after 90 minutes, and display-only behavior: neither barb layer changes selected sailing location, wind station, currents station, or report calculations.
- Advanced runtime identity to **Version 1.9.3 · Build v207**.

## v206 / 1.9.3 changes

- Splits NOAA/NDBC observed wind barbs into two independent controls under **Map Overlays → Observations**: **Marine / Bay Wind Barbs (NOAA/NDBC)** and **Inland Wind Barbs**.
- Marine / Bay barbs remain the normal sailing-oriented display; inland barbs are opt-in so observations over land do not clutter the default Bay/Delta view.
- Adds a display-only station classification to `/wind-barbs`. The marine mask covers San Francisco Bay, San Pablo/Suisun Bay, the western/central Delta sailing corridor, Golden Gate approaches, and the immediate north-central California coast; observations outside that mask are tagged inland.
- The classification does not alter NOAA/NDBC data, selected wind station, selected sailing location, currents, or any report calculations.
- Both categories share the v205 buffered viewport cache, so enabling or disabling Inland Wind Barbs does not trigger unnecessary station re-selection or re-centering.
- The wind-barb status line reports the number of marine/Bay and inland observations currently shown, with stale observations still faded after 90 minutes.
- Advanced runtime identity to **Version 1.9.3 · Build v206**.

## v205 / 1.9.3 changes

- Fixes the v204 wind-barb clustering/pop-in behavior caused by reusing the nearest-station `/wind-stations` candidate endpoint.
- Adds a dedicated `/wind-barbs` endpoint that accepts geographic map bounds and returns every active NOAA/NDBC station inside that bounded region that has a usable latest wind observation.
- The browser requests a map area padded by roughly 35% beyond the visible viewport, with a small minimum geographic margin, so ordinary panning remains inside an already-loaded observation field instead of replacing whole nearest-station groups.
- The loaded wind-barb bounds are cached in browser state. A new request is made only when the visible map moves outside that buffered area; panning inside the buffer simply reuses the existing barb set.
- Wind-barb loading remains independent of the selected sailing location, committed wind station, and **Find stations** candidate list.
- Disabling the layer clears the buffered-bounds cache and observation layer; re-enabling forces a fresh bounded request.
- Standard meteorological barb rendering, knot-based feather increments, station tooltips, and stale-observation fading are retained.
- Advanced runtime identity to **Version 1.9.3 · Build v205**.

## v204 / 1.9.3 changes

- Fixes the initial v203 **Observed Wind Barbs (NOAA/NDBC)** implementation so the overlay no longer depends on the selected-location **Find stations** candidate list.
- Enabling the wind-barb overlay now requests nearby NDBC observations using the **current map viewport center**, even when no sailing location has been selected.
- Wind-barb observations are kept in independent overlay state and do not alter the committed wind station, selected sailing location, or nearby-station candidate list.
- The overlay refreshes after map movement with a short debounce so panning/zooming updates the displayed observation set without issuing a request for every intermediate map event.
- Disabling the overlay cancels pending refreshes, clears its observation state, and removes the barb layer.
- Retains the v203 standard meteorological barb rendering, knot-based speed feathers, observation-age tooltips, and fading of observations older than 90 minutes.
- Advanced runtime identity to **Version 1.9.3 · Build v204**.

## v203 / 1.9.3 changes

- Added an optional **Observed Wind Barbs (NOAA/NDBC)** layer under **Map Overlays → Observations**.
- Wind barbs are drawn from the latest observations already returned for nearby discovered NDBC wind stations, so the overlay does not change the selected sailing location or committed wind station.
- Barb shafts point toward the direction the wind comes from; feathers encode sustained wind speed in standard 5/10/50-knot increments. Calm/light observations use a small circle.
- The overlay accepts the browser's knots or MPH display text but converts MPH back to knots before constructing the meteorological barb, keeping the symbol convention independent of display units.
- Station tooltips show station ID, formatted observed wind, and observation age. Observations older than 90 minutes are faded and counted in the overlay status line.
- If no nearby candidates have been loaded yet, the status directs the user to select a location and use **Find stations**; enabling the layer does not recenter the map or initiate station selection.
- Saildrone, terrain/seafloor, weather/hazard overlays, station-selection behavior, currents, SST, and chlorophyll behavior are unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v203**.

## v202 / 1.9.3 changes

- Moved **Topography & Bathymetry** out of the fishing-specific category into a general **Terrain & Seafloor** overlay group. The NOAA ETOPO/Marine Cadastre data and rendering are unchanged.
- Added a purple **Saildrone latest position** icon to the existing map legend row.
- Saildrone status now reports both the total latest platform positions returned and how many are currently visible in the map viewport.
- The visible Saildrone count updates after map pan/zoom.
- Saildrone status also reports the oldest observation age when available, while preserving counts for observations older than 72 hours and unavailable configured datasets.
- Enabling Saildrone still does not automatically recenter or fit the map.
- Fishing Reports, SST, and all chlorophyll paths remain deferred/disabled.
- Advanced runtime identity to **Version 1.9.3 · Build v202**.

## v201 / 1.9.3 changes

- Removed **Fishing Reports** from the active application because the existing implementation depended on static `assets/fishing_reports.json` and therefore could not guarantee runtime freshness.
- Removed the `/fishing-reports` endpoint, map checkbox, report legend/status UI, Leaflet report pane, report parser/renderer/toggle code, and all runtime dependence on `assets/fishing_reports.json`.
- Removed the Fishing Planning **Recent reports** metric. Fishing Planning now reports only the **Nearby Offshore Feature** discovered at runtime from NOAA's named-feature service.
- An old `assets/fishing_reports.json` may still exist in the repository from previous builds; v201 does not read it and it may be deleted separately.
- Fishing Reports are deferred until a suitable runtime-discoverable, attributable upstream source is identified and validated.
- SST and all chlorophyll paths remain deferred/disabled.
- Advanced runtime identity to **Version 1.9.3 · Build v201**.

### Deferred Fishing Reports — runtime-source requirement

Fishing Reports should return only from a runtime-discoverable source. A future implementation must fetch current report data when requested, or use a short-lived server cache populated at runtime, rather than shipping a manually maintained snapshot.

Before enabling the feature, validate the candidate upstream independently and establish that automated access/reuse is permitted. The provider should preserve source name/link, report date, species/report text when available, and location confidence (exact, derived, or regional), with bounded timeouts and a short cache lifetime. If no suitable provider is reachable, the map should omit Fishing Reports rather than fall back to stale bundled data.

TempBreak remains a useful product-design reference, but it is not an approved runtime data source unless its underlying data access and reuse terms are separately established.

## v200 / 1.9.3 changes

- Restored the Fishing Reports browser implementation accidentally removed during the v198 cleanup: `loadFishingReports`, report rendering, status/legend handling, and `setFishingReportsVisible`.
- Also restored the Saildrone browser rendering/toggle functions that were removed in the same cleanup regression; the existing Saildrone endpoint/data logic was left intact.
- The `/fishing-reports` endpoint remains file-backed by `assets/fishing_reports.json`; no live external fishing-report provider was added.
- Added a stale-feed warning when the feed-level `updated` timestamp is more than 14 days old.
- Renamed the ETOPO overlay from **Topography & Bathymetry** to **Topography & Bathymetry (NOAA ETOPO + offshore feature names)** because the raster also includes inland terrain and mountains.
- Fishing Planning now labels the named-feature metric **Nearby Offshore Feature**.
- SST and all chlorophyll paths remain deferred/disabled.
- Advanced runtime identity to **Version 1.9.3 · Build v200**.

### Fishing Reports data freshness

`assets/fishing_reports.json` is no longer an active application dependency in v201.

The file-backed design is still useful because report data can be refreshed without changing Go or map UI code. The feed should keep a trustworthy top-level `updated` timestamp and dated individual reports. v200 warns in the map status when the feed timestamp is more than 14 days old, but that warning does not refresh the data.

A future improvement should use a small ingestion/update workflow that writes the same JSON schema from one or more permitted, attributable report sources. Preserve source URL, report date, location confidence, and whether a location is exact, derived from fisherman shorthand, or only regional. Avoid automatically scraping a third-party site unless its terms and reuse permissions permit it.

## v199 / 1.9.3 changes

- Fixes the v198 compile regression caused by two stale imports left after the final CoastWatch/chlorophyll helper removal.
- Removed unused standard-library imports `context` and `net`.
- No functional behavior changed from v198: SST and all chlorophyll paths remain deferred/disabled; Offshore Fishing remains Underwater Structure + Fishing Reports.
- Advanced runtime identity to **Version 1.9.3 · Build v199**.

## v198 / 1.9.3 changes

- Removed the remaining active **Chlorophyll Contours** path after live testing showed the NOAA PFEL/ERDDAP numeric request failing with a TLS handshake timeout.
- Removed `/chlorophyll-info`, `/chlorophyll-overlay`, the contour checkbox, contour legend/status UI, Leaflet pane, browser contour fetch/refresh logic, and the now-unused CoastWatch numeric helper code.
- Removed `/fishing-water` and the chlorophyll metric from Fishing Planning. Fishing Planning now evaluates only **named underwater structure** and **recent fishing reports**, so a failed CoastWatch service can no longer delay or error that card.
- Offshore Fishing now exposes only the currently independent features: **Underwater Structure** and **Fishing Reports**.
- SST remains deferred/disabled. Chlorophyll Field and Chlorophyll Contours are now both deferred/disabled.
- Advanced runtime identity to **Version 1.9.3 · Build v198**.

### Deferred chlorophyll — consolidated future approach

Both chlorophyll delivery experiments are now retired from the active application:

- **Filled Chlorophyll Field:** NOAA CoastWatch THREDDS/ncWMS `GetMap` returned HTTP 500.
- **Chlorophyll Contours / Fishing Planning numeric chlorophyll:** PFEL/ERDDAP timed out during TLS connection/handshake from the test environment.

Do not restore either path merely by changing application timeouts. A future chlorophyll implementation should first prove a small, bounded request independently with `curl` from both localhost and Render. Prefer a stable viewport-sized tile/subset endpoint. If only numeric data becomes reliable, a good fallback is to fetch a bounded numeric subset and render both the filled field and contours locally from that one small response, keeping field and contours optional and independently fail-safe.

The product-design notes from v197 remain relevant: TempBreak's Central Coast page is a useful visual/workflow reference for how offshore anglers consume SST, chlorophyll/water-color context, bathymetry, waypoints, buoy references, and weather together. It remains a design reference only unless its data sources and reuse terms are separately verified.

## v197 / 1.9.3 changes

- Removed the active **Chlorophyll Field** raster overlay after the NOAA CoastWatch THREDDS/ncWMS `GetMap` path returned HTTP 500 during live testing.
- Removed the Chlorophyll Field menu checkbox, raster legend/status UI, Leaflet pane, browser fetch/refresh logic, and `/chlorophyll-field` proxy endpoint.
- **Chlorophyll Contours were active in v197 but are deferred/disabled in v198.** Their existing numeric-data path, contour rendering, Fishing Planning chlorophyll metric, and shared chlorophyll helpers were not intentionally changed.
- SST remains deferred/disabled as documented in v195.
- Added a future chlorophyll-field recovery note and an external reference to TempBreak's Central Coast presentation for product-design research.
- Advanced runtime identity to **Version 1.9.3 · Build v197**.

### Deferred Chlorophyll Field — future approach

The filled chlorophyll raster is deferred for the same operational reason as the attempted SST WMS path: the tested NOAA CoastWatch THREDDS/ncWMS `GetMap` request returned HTTP 500 upstream. Do not restore the field simply by changing WMS parameters inside the application.

A future implementation should first prove a small, bounded chlorophyll subset/tile request outside the app from both localhost and Render. Prefer a stable service that returns a viewport-sized raster or tile directly. If only numeric data is reliable, another option is to generate the filled raster locally from the same bounded numeric subset used for contours, using a fixed fishing-oriented scale and explicit no-data/land handling. That approach would avoid depending on NOAA's WMS renderer while keeping the data source authoritative.

Keep the filled field optional and isolated from Chlorophyll Contours. Failure of the field must never prevent contours, Fishing Planning, structure, reports, or the rest of the map from working.

### External fishing-map reference: TempBreak

For future SST/chlorophyll product-design research, review TempBreak's Central Coast page:

https://tempbreak.com/index.php?cwregion=cc

The page is useful as a reference for how an offshore-fishing map presents regional SST, bottom detail, waypoints, buoy references, weather, and marine-forecast context in one view. Treat it as a **design/workflow reference only** unless its data sources, reuse rights, and any machine-access terms are separately verified. Do not copy or proxy TempBreak imagery/data merely because it is visible in a browser.

## v196 / 1.9.3 changes

- Fixes the v195 compile regression: `coastWatchWindowStats` was accidentally removed while stripping the SST implementation even though the shared chlorophyll `/fishing-water` path still uses that type.
- Restored only the small shared `coastWatchWindowStats` struct required by `fetchCoastWatchWindowStats`.
- SST remains fully disabled/deferred exactly as intended in v195; no SST UI, routes, cache, downloader, or NetCDF dependency were reintroduced.
- Advanced runtime identity to **Version 1.9.3 · Build v196**.

## v195 / 1.9.3 changes

- **Sea Surface Temp is removed from the active application for now.** The Offshore Fishing map menu no longer offers SST; the SST status/legend, browser handlers, `/sst-info`, `/sst-overlay`, and Fishing Planning SST metric are removed.
- Removed the v193/v194 direct-NetCDF downloader, ephemeral SST cache, and runtime NetCDF parsing/rendering code. This eliminates the ~150 MB cold-cache download and the long-lived SST request that produced browser `Load failed` behavior.
- Removed the pure-Go `go-native-netcdf` dependency introduced in v193. A companion `go.mod-updated-v195` restores the dependency-free module state and prior Go 1.13 language line.
- Chlorophyll, underwater structure, fishing reports, weather/hazards, and Saildrone remain independent features and are otherwise unchanged in this cleanup candidate.
- Advanced runtime identity to **Version 1.9.3 · Build v195**.

### Deferred SST — future approach

SST remains useful for offshore fishing planning, but it should return only when the upstream/data path is operationally simple enough for an interactive map. The v173–v194 experiments established these constraints:

1. Do **not** make an interactive `/sst-overlay` request wait for a whole global daily NetCDF download. The tested NOAA OSPO direct file was roughly 155 MB, which is too expensive for a cold browser session even if later pans and zooms reuse a cache.
2. Do **not** depend on the currently unreliable CoastWatch ERDDAP image path. During September 2026 testing, CoastWatch Central returned proxy/response-header failures and PFEL/ERD was unreachable from the test network.
3. Do **not** assume THREDDS/ncWMS is a drop-in replacement without first proving the exact `GetMap` request outside the application; the attempted v192 WMS path returned HTTP 500.
4. Before reintroducing SST, first prove a lightweight, bounded upstream request with `curl` from both localhost and Render. Prefer a service that returns only the requested geographic subset, tile, or small raster rather than a global source file.
5. If NOAA direct NetCDF remains the only reliable transport, investigate **HTTP range-based partial NetCDF access** or a small scheduled preprocessing service that converts each daily global file once into map-ready regional tiles. The interactive app should read small tiles/subsets, not parse/download the global file synchronously.
6. Any future SST implementation should sit behind a provider interface with explicit timeouts, source diagnostics, and a fast failure mode. SST failure must never delay unrelated map/UI functionality.
7. Preserve the useful display decisions already learned: fixed **35–95°F** scale for cross-view comparability, approximately 1°F bands, native no-data/land transparency where available, and correct Web-Mercator alignment rather than stretching an EPSG:4326 raster directly in Leaflet.

The direct NOAA OSPO HTTPS file path remains a useful research lead because an actual 2026 NetCDF object was independently confirmed with HTTP 200 and byte-range support. It is **not** an active application dependency in v195.

## v194 / 1.9.3 changes

- Cold-cache performance recovery for the direct NOAA SST path introduced in v193.
- Replaced v193's **sequential** 15-day daily-file HEAD probe loop with **15 concurrent deterministic HEAD probes**. The newest HTTP 200 result is selected after one bounded probe window instead of potentially waiting through many serial network timeouts.
- Tightened SST discovery transport limits to approximately **3 s TLS / 4 s response-header / 5 s total per probe**. Because probes execute in parallel, the discovery phase is now bounded to roughly one short network timeout rather than as much as ~150 seconds.
- The expensive operation remains the one-time direct NetCDF download on a cold cache. Once that daily file is present, subsequent pans and zooms continue to reuse the same ephemeral local file.
- Updated the browser SST loading message so a cold-cache request explicitly distinguishes parallel source discovery from the one-time ~150 MB download.
- No SST source, NetCDF parsing, rendering, scale, projection, chlorophyll, Saildrone, or map-menu behavior was otherwise changed from v193.
- v193's companion `go.mod` dependency change remains required and is unchanged for v194.
- Advanced runtime identity to **Version 1.9.3 · Build v194**.

## v193 / 1.9.3 changes

- Replaced the failed ERDDAP and THREDDS/ncWMS SST image paths with the **direct NOAA CoastWatch HTTPS NetCDF distribution** that was proven from localhost with an actual 2026 OSPO file returning `200 OK`, `Accept-Ranges: bytes`, and `Content-Type: application/x-netcdf`.
- SST now probes deterministic daily OSPO filenames backward for up to 15 days, selects the newest direct file returning HTTP 200, downloads that file once, and caches it on the running instance's ephemeral local disk.
- The local SST cache is checked for a newer source no more than once every **6 hours**. Repeated pans and zooms during a normal short session reuse the same cached daily file and do **not** re-download NOAA data.
- The cache keeps only one active daily NetCDF file plus a temporary file during atomic replacement. It is deliberately disposable: a Render restart/replacement simply causes the next SST request to rebuild the cache.
- Added a pure-Go NetCDF reader dependency (`github.com/harel/go-native-netcdf v0.1.0`) so Render does not require the native `libnetcdf` C library. A companion `go.mod-updated-v193` is generated and raises the module language version from Go 1.13 to **Go 1.18**, required by that dependency.
- SST rendering reads only the latitude rows and longitude span needed for the current viewport from the cached `analysed_sst` variable, rather than loading the entire 155 MB source into memory for every pan/zoom.
- Rendering is performed directly on Web-Mercator destination scanlines, preserves native fill-value transparency over land/no-data, and retains the established fixed **35–95°F / 60-band** display.
- Added response diagnostics `X-SST-Source-Date` and `X-SST-Cache` in addition to the existing upstream/dataset/projection diagnostics.
- The browser now warns on first SST load that one approximately 150 MB daily file may be cached; subsequent pans/zooms render locally.
- Chlorophyll is intentionally unchanged from v192 in this candidate so SST transport/cache recovery can be validated independently.
- Advanced runtime identity to **Version 1.9.3 · Build v193**.

## v192 / 1.9.3 changes

- Replaced the broken NOAA ERDDAP image path for **Sea Surface Temp** with NOAA CoastWatch **THREDDS/ncWMS** using the documented aggregated Day/Night OSPO dataset `BlendedSST5kmDayNightAggGHRSSTOSPOLoM`.
- SST WMS requests use `analysed_sst`, the existing 35–95°F equivalent Kelvin color range, 60 bands, transparent PNG output, and retain the proven v180 server-side latitude-to-Web-Mercator reprojection before Leaflet display.
- Removed the long sequential ERDDAP fallback wait from the SST map request path. THREDDS requests now use short transport limits (8 s TLS, 12 s response-header, 20 s total) so upstream trouble fails quickly instead of blocking the UI for roughly a minute.
- Replaced the chlorophyll **field image** and metadata path with NOAA CoastWatch THREDDS/ncWMS using the cataloged global 9 km DINEOF dataset `CoastWatch/VIIRS/npp-n20/chloci/DailyGlobalSCIDINEOF/WW00/LoM`, variable `chlor_a`.
- Chlorophyll field rendering retains the 0.1–1 mg/m³ logarithmic display range and existing NOAA ENC coastline mask.
- The custom chlorophyll contour renderer is intentionally left on its existing numeric data path in this recovery candidate; v192 is focused on restoring the primary SST and chlorophyll field images without introducing a NetCDF parser dependency.
- The `/sst-info` and `/chlorophyll-info` endpoints no longer wait on ERDDAP metadata; they identify the active THREDDS source immediately.
- Direct NOAA HTTPS object access was independently proven from localhost with an actual 2026 OSPO NetCDF file returning `200 OK` and `Accept-Ranges: bytes`; v192 avoids downloading those full files by using NOAA's server-side WMS rendering instead.
- Advanced runtime identity to **Version 1.9.3 · Build v192**.

## v191 / 1.9.3 changes

- Transport/failover recovery build prompted by the deployed SST error `timeout awaiting response headers` from the CoastWatch Central `transparentPng` endpoint.
- Sea Surface Temp now uses **NOAA PFEL/ERD ERDDAP** as the primary rendered-image and metadata source, paired with dataset **`nesdisBLENDEDsstDNDaily`** and variable `analysed_sst`.
- The SST image path retains a CoastWatch Central fallback using its matching Central-host dataset ID `noaacwBLENDEDsstDNDaily`. The proxy now continues to the next upstream when an endpoint returns a non-200 response or non-PNG content instead of accepting the first HTTP response unconditionally.
- Chlorophyll metadata, rendered field, numeric contour grid, and Fishing Planning chlorophyll sampling now use **NOAA PFEL/ERD ERDDAP** with dataset **`nesdisVHNnoaaSNPPnoaa20chlaGapfilledDaily`**, variable `chlor_a`.
- Fishing Planning SST numeric sampling is source-consistent with the PFEL/ERD SST dataset used by the map.
- Corrected chlorophyll contour stride estimation for the replacement approximately **9 km / 0.083333°** source grid; the old numeric request still estimated point count using the retired ~2 km spacing.
- Existing `X-SST-Upstream`, `X-SST-Dataset`, `X-Chlorophyll-Upstream`, and `X-Chlorophyll-Dataset` diagnostics now expose the actual PFEL/ERD source used by successful requests.
- Retained the v180 Web-Mercator SST reprojection, v181 **35–95°F** scale, v183 center-world clipping, v188 pure-CSS Map Overlays recovery UI, and v187 schema-aware Saildrone implementation.
- v190 remains useful failure evidence: its Central-host request reached the correct Central dataset but timed out waiting for image-response headers.
- Advanced runtime identity to **Version 1.9.3 · Build v191**.

## v190 / 1.9.3 changes

- Corrected the v189 NOAA ERDDAP source migration. v189 used `nesdis...` dataset IDs while the application was still querying the **CoastWatch Central** host `https://coastwatch.noaa.gov/erddap`, which caused the SST and chlorophyll requests to target dataset IDs that do not belong to that server.
- Restored the CoastWatch Central Geo-Polar blended SST dataset ID **`noaacwBLENDEDsstDNDaily`**, variable `analysed_sst`. This is the Central-host Day+Night global 5 km product used by the application's existing `/sst-info`, `/sst-overlay`, and Fishing Planning SST paths.
- Switched the CoastWatch Central chlorophyll source to **`noaacwNPPN20VIIRSDINEOFDaily`**, the Central-host NOAA S-NPP + NOAA-20 VIIRS DINEOF near-real-time global daily chlorophyll dataset. The active variable remains `chlor_a`.
- Kept the approximately **9 km** chlorophyll resolution wording introduced in v189, along with the existing field color range, 0.2 / 0.3 / 0.5 mg/m³ contours, NOAA ENC field land clipping, and Fishing Planning chlorophyll calculations.
- Kept the v189 `X-SST-Dataset` and `X-Chlorophyll-Dataset` response diagnostics; those headers now report the CoastWatch Central dataset IDs actually requested by the server.
- Retained v188's pure-CSS Map Overlays recovery UI, v187's schema-aware Saildrone implementation, and all v183/v180/v181 SST display geometry and scale behavior unchanged.
- v189 should be treated as a failed source-migration candidate because it crossed dataset identifiers between NOAA ERDDAP servers.
- Advanced runtime identity to **Version 1.9.3 · Build v190**.

## v189 / 1.9.3 changes

**Failed source-migration candidate:** the `nesdis...` dataset identifiers were paired with the CoastWatch Central `coastwatch.noaa.gov/erddap` host and did not restore the deployed overlays.

- Data-source recovery build for Sea Surface Temp and Chlorophyll after the deployed v183 build also stopped returning those overlays, demonstrating that the failure was not caused by the later Map Overlays category/popover UI work.
- Migrated the active NOAA Geo-Polar Blended SST dataset from retired/older identifier `noaacwBLENDEDsstDNDaily` to current NOAA NESDIS CoastWatch dataset **`noaacwBLENDEDsstDNDaily`**, retaining variable `analysed_sst`, the v180 Web-Mercator reprojection, v181 fixed **35–95°F** scale, native no-data transparency, and v183 center-world clipping.
- Migrated Chlorophyll Field, Chlorophyll Contours, and Fishing Planning chlorophyll sampling from `noaacwNPPN20VIIRSDINEOFDaily` to current NOAA NESDIS CoastWatch DINEOF gap-filled dataset **`noaacwNPPN20VIIRSDINEOFDaily`**, variable `chlor_a`.
- The replacement DINEOF chlorophyll product is approximately **9 km** resolution rather than the previous 2 km product. User-facing overlay labels/status text now state 9 km explicitly.
- Preserved the existing chlorophyll display range, contour levels (0.2 / 0.3 / 0.5 mg/m³), NOAA ENC land clipping path for the field image, and Fishing Planning calculations.
- Added `X-SST-Dataset` and `X-Chlorophyll-Dataset` response diagnostics so deployed requests identify which current NOAA dataset actually served each overlay.
- Retained v188's pure-CSS Map Overlays recovery UI and v187's schema-aware Saildrone implementation unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v189**.

## v188 / 1.9.3 changes

- Recovery build for the map-overlay regression observed after v186/v187, where Sea Surface Temp, Chlorophyll, and Saildrone all stopped displaying.
- Removed the v186 viewport-aware Map Overlays JavaScript positioning block, including its `:scope` selectors and runtime geometry calculations, from the page initialization path.
- Restored Map Overlays to a pure-CSS positioned panel so an unsupported selector or popover-positioning exception cannot abort initialization before the SST, chlorophyll, and Saildrone checkbox handlers are installed.
- The Map Overlays panel now opens **upward** from the control using ordinary absolute positioning, with the existing bounded internal scroll area retained. Because the controls sit below the map, this uses the large map area above rather than depending on the smaller remaining page area below.
- Retained the **Weather & Hazards**, **Offshore Fishing**, and **Observations** categories. Accordion behavior is preserved with simple DOM iteration that does not use `:scope` or viewport-position calculations.
- Retained v187's schema-aware NOAA PMEL Saildrone retrieval, including position-only fallback and optional wind/SST/salinity/current/wave enrichment.
- Retained the v183 SST single-image path, v180 Web-Mercator reprojection, v181 **35–95°F** scale, and v183 center-world clipping unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v188**.

## v187 / 1.9.3 changes

- Renamed the first Map Overlays category from **Marine Weather** to **Weather & Hazards**, which better covers NWS forecast zones, NOAA HMS smoke, satellite cloud cover, and weather radar and leaves room for future hazard-oriented layers.
- Reworked NOAA PMEL Saildrone retrieval to be **schema-aware** instead of requesting one hard-coded variable list from every mission.
- For each configured Saildrone dataset, the server first reads the ERDDAP `info/<dataset>/index.json` metadata and builds the tabledap query from variables actually present in that mission.
- Only `time`, `latitude`, and `longitude` are required for a platform to be plotted. `drone_id`, wind, seawater temperature, salinity, surface current, wave height, and wave period are optional enrichments selected from known variable-name aliases when available.
- If the metadata endpoint temporarily fails, the server falls back to a minimal `time,latitude,longitude` request rather than dropping the dataset solely because an optional measurement name is unavailable.
- The Saildrone status line now reports an explicit error when zero platform positions are returned and retains per-dataset failure counts when some missions are unavailable.
- Retained the v186 viewport-aware Map Overlays popover and accordion categories, v185 configured 2026 Saildrone mission list, and all SST behavior unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v187**.

## v186 / 1.9.3 changes

- Reworked **Map Overlays** from a downward-only absolute dropdown into a viewport-aware fixed popover so it is no longer constrained by the remaining page/card space below the button.
- When the menu opens, browser JavaScript measures the Map Overlays button and viewport, chooses the side with more usable vertical space, and positions the panel fully inside the viewport with an internal scroll area only when required.
- The three v185 purpose groups remain: **Marine Weather**, **Offshore Fishing**, and **Observations**.
- Converted those groups to accordion behavior: opening one category automatically closes the other open category, reducing menu height while keeping the categories easy to scan.
- Removed the long overlay-description paragraph from inside the control popover. The same documentation is now available in a separate collapsed **Map overlay details** help control below the map controls/status area.
- Retained the v185 Saildrone dataset configuration and all map-layer behavior unchanged; this build is a UI containment/layout correction only.
- Retained the v183 SST single-image path, v180 Web-Mercator reprojection, v181 **35–95°F** scale, and v183 center-world clipping unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v186**.

## v185 / 1.9.3 changes

- Reorganized **Map Overlays** into three purpose-based collapsible categories: **Marine Weather**, **Offshore Fishing**, and **Observations**.
- **Marine Weather** contains NWS forecast zone, NOAA HMS satellite smoke, NOAA satellite cloud cover, and NOAA/NWS weather radar.
- **Offshore Fishing** contains Sea Surface Temp, Chlorophyll Field, Chlorophyll Contours, Underwater Structure, and Fishing Reports. This section opens by default because it contains the most frequently combined fishing-planning layers.
- **Observations** contains Saildrone Observations and provides a home for future moving or in-situ research feeds without further bloating the fishing category.
- Added a bounded scroll area to the Map Overlays panel as a safety net (`70vh`, capped at 560 px), while preserving the existing readable checkbox/font sizing.
- Expanded the NOAA PMEL TPOS Niño 3.4 Saildrone configuration with current public dataset **`sd1090_tpos_2026`**, bringing the configured 2026 TPOS set to Saildrones 1033, 1077, 1080, and 1090.
- NOAA PMEL currently publishes realtime TPOS Saildrone mission data through public ERDDAP; the verified 2026 TPOS datasets expose position plus wind, seawater temperature, salinity, currents, and wave variables where available.
- Saildrone remains an optional observation overlay and does not change NDBC wind station selection, CO-OPS current predictions, SST, chlorophyll, Fishing Reports, or Fishing Planning behavior.
- Retained the v183 SST single-image path, v180 Web-Mercator reprojection, v181 **35–95°F** scale, and v183 center-world clipping unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v185**.

## v184 / 1.9.3 changes

- Added an optional **Saildrone Observations (NOAA PMEL)** map overlay using public NOAA PMEL ERDDAP data; no commercial Saildrone API or paid credential is required.
- Added server endpoint `/saildrone-observations`, which retrieves the latest record from configured public 2026 NOAA PMEL TPOS Niño 3.4 and Atlantic Hurricane Monitoring Saildrone datasets; v185 includes TPOS Saildrones 1033, 1077, 1080, and 1090.
- The server caches the assembled Saildrone feed for 15 minutes and limits concurrent upstream requests so a browser toggle does not repeatedly fan out to NOAA.
- Map markers show each latest reported Saildrone position. Popups include available wind, seawater temperature, salinity, surface-current, significant-wave-height, and dominant-wave-period values, with display conversion to knots/°F/feet where appropriate.
- Observations older than 72 hours remain visible but are counted as stale in the overlay status. Individual unavailable datasets are skipped rather than failing the entire overlay.
- Saildrone remains a separate optional observation layer and does not alter NDBC wind-station selection, CO-OPS tidal-current prediction, SST, chlorophyll, or Fishing Planning logic.
- Retained the v183 single-image SST path, v180 Web-Mercator reprojection, v181 **35–95°F** SST scale, and v183 low-zoom center-world clipping unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v184**.

## v183 / 1.9.3 changes

- Returned the active browser SST code to the **v181 single-image path** after v182's multi-segment `Promise.all()` implementation regressed to no visible SST when any wrapped segment failed.
- Kept the proven v180 server-side Web-Mercator reprojection and v181 fixed **35–95°F** temperature scale unchanged.
- Replaced the old low-zoom dateline rejection with a minimal **center-world clipping** rule: Leaflet's visible viewport is clipped to the single 360° world copy containing the current map center, then that displayed longitude interval is translated back into NOAA's canonical **-180° to +180°** request domain.
- The returned SST image is placed only over that clipped displayed interval. This avoids invalid NOAA longitude requests without introducing multiple simultaneous SST fetches.
- Normal single-request loading, replacement, blob cleanup, 0.50 opacity, native land/no-data transparency, and existing NOAA/IPv4 diagnostics remain unchanged from v181.
- v182 is retained below as failed-candidate history and should not be used as the SST implementation baseline.
- Advanced runtime identity to **Version 1.9.3 · Build v183**.

## v182 / 1.9.3 changes

**Failed browser candidate:** visual testing showed no SST overlay after the multi-segment change; v183 does not use this path.

- Kept the v180 Web-Mercator SST reprojection and v181 fixed **35–95°F** display scale unchanged.
- Removed the browser-side rule that disabled Sea Surface Temp whenever Leaflet's wrapped viewport extended below -180° or above +180° longitude.
- Added SST viewport segmentation at the international date line. Each visible wrapped-world segment is mapped back into NOAA's canonical **-180° to +180°** longitude domain for the server request, then displayed at its corresponding wrapped Leaflet longitude.
- A low-zoom North America view that extends west of -180° can now load adjacent SST image segments instead of reporting the layer unavailable or retaining a stale partial rectangle.
- SST layers are cleared when a new viewport refresh begins, and generated blob URLs are revoked when replaced, disabled, or failed, preventing old SST rectangles from lingering during wrapped requests.
- Multi-segment SST loads retain the same NOAA CoastWatch source, projection correction, 0.50 opacity, native land/no-data transparency, and fixed scale as the single-segment path.
- Advanced runtime identity to **Version 1.9.3 · Build v182**.

## v181 / 1.9.3 changes

- Kept the v180 server-side Web-Mercator reprojection path for Sea Surface Temp and focused this build on the display scale only.
- Widened the fixed Sea Surface Temp display range from **45–75°F** to **35–95°F** so tropical/subtropical water and unusually warm periods such as strong El Niño conditions no longer saturate into one broad hottest-color region at continental map extents.
- Increased the rendered temperature sections from 30 to 60 so the wider range still preserves approximately **1°F visual steps** and retains useful regional temperature-break contrast.
- Updated the Planning and Details SST legend, explanatory text, and runtime overlay-status wording to describe the wider fixed scale.
- Kept the proven NOAA CoastWatch `nesdisBLENDEDsstDNDaily:analysed_sst` source, SST-only IPv4 transport workaround, 0.50 Leaflet opacity, and v180 projection correction unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v181**.

## v180 / 1.9.3 changes

- Corrected the Sea Surface Temp map geometry rather than applying another coastline mask. NOAA CoastWatch ERDDAP `griddap` surface images are rendered on a regular latitude/longitude grid, while Leaflet displays the map in EPSG:3857 Web Mercator. Directly stretching the latitude/longitude PNG to Leaflet bounds aligns the outer corners but causes increasing north/south coastline drift as the map view spans more latitude.
- `/sst-overlay` now decodes the CoastWatch PNG and server-side resamples every output scanline into Web Mercator Y before returning it to the browser. Longitude remains unchanged because it is linear in both the source image and Web Mercator x coordinate.
- The reprojection uses premultiplied-alpha interpolation so CoastWatch's native transparent land/no-data edge is retained without bleeding opaque SST color into transparent pixels. No secondary coastline mask, synthetic SST fill, or browser-side vector mask is used.
- Added `X-SST-Projection` response diagnostics so a returned overlay identifies the server-side EPSG:4326-latitude-grid → EPSG:3857 reprojection path.
- Kept the proven `nesdisBLENDEDsstDNDaily:analysed_sst` source, fixed 45–75°F fishing-oriented scale, SST-only IPv4 transport workaround, and 0.50 Leaflet opacity. The data remain approximately 5 km resolution; v180 fixes map projection/alignment, not source-grid resolution.
- Updated the Planning and Details SST help/status language to distinguish corrected Web-Mercator alignment from the underlying product's still-coarse near-shore resolution.
- Advanced runtime identity to **Version 1.9.3 · Build v180**.

## v179 / 1.9.3 changes

- Restored the last known-working Sea Surface Temp implementation from v176 after the v177 JPL MUR griddap experiment and v178 MUR WMS experiment both failed to display an SST overlay reliably in browser testing.
- `/sst-info`, `/sst-overlay`, and Fishing Planning SST numeric sampling are again source-consistent on NOAA CoastWatch `nesdisBLENDEDsstDNDaily:analysed_sst`.
- `/sst-overlay` again uses the proven ERDDAP `griddap` transparent-PNG rendering path with the fixed fishing-oriented 45–75°F scale and the SST-only IPv4 transport workaround.
- Restored CoastWatch's native transparent/no-data mask and the normal Leaflet image-overlay path at 0.50 opacity. No synthetic SST fill, browser-side vector coastline mask, or JPL MUR WMS dependency remains in the active SST path.
- The v177/v178 MUR work is retained below as failed-candidate history only; it is not part of the v179 runtime behavior.
- No unrelated v178 behavior needed to be carried forward: the v176→v178 source differences were confined to the SST/MUR experiment plus build identity.
- Advanced runtime identity to **Version 1.9.3 · Build v179**.

## v178 / 1.9.3 changes

- Replaced the v177 JPL MUR `griddap` transparent-PNG rendering path with NOAA CoastWatch ERDDAP's documented **WMS GetMap** interface for the SST map overlay. v177 could resolve MUR metadata but did not reliably return a display image.
- `/sst-overlay` now proxies WMS 1.1.1 using `SRS=EPSG:4326` and `bbox=west,south,east,north`, avoiding the WMS 1.3.0 latitude/longitude axis-order ambiguity while remaining fully georeferenced to the current Leaflet viewport.
- The displayed SST layer now uses NOAA's Fahrenheit MUR dataset `jplMURSST41F:analysed_sst`. WMS supplies the dataset's native **30–90°F** display scale and transparent missing/land pixels.
- `/sst-info` now reads time metadata from `jplMURSST41F`; `time=current` remains a supported fallback and WMS defaults to the latest field if no explicit time is available.
- Numeric Fishing Planning SST calculations remain on the MUR numeric grid and continue to report Fahrenheit values after conversion; this change is to the displayed image transport, not the underlying destination-statistics logic.
- Continued the SST-specific IPv4 transport workaround for the previously unreliable local IPv6 CoastWatch route.
- Advanced runtime identity to **Version 1.9.3 · Build v178**.

## v177 / 1.9.3 changes

- Replaced the approximately 5 km NOAA Geo-Polar Blended SST display source with NASA JPL **MUR SST v4.1** (`jplMURSST41`) served through NOAA CoastWatch ERDDAP. MUR is a daily global Level-4 analysis on a 0.01° grid, approximately 1 km.
- `/sst-info`, `/sst-overlay`, and the Fishing Planning SST numeric window now use the same MUR `analysed_sst` field so the map and destination statistics remain source-consistent.
- Preserved MUR's native land/no-data mask and removed any need for synthetic shoreline clipping. The higher-resolution source is intended to improve coastal alignment directly rather than hide a coarse source edge with a second coastline product.
- Added adaptive ERDDAP source-grid stride for wide map views: requests retain native 0.01° sampling when the view fits within the output image and downsample only when there are more source cells than display pixels.
- Changed the fixed 45–75°F SST rendering from discrete 1°F bands to a continuous color ramp to reduce posterized/blocky appearance while retaining a stable cross-view temperature scale.
- Continued to force SST/CoastWatch requests over IPv4 because the earlier local IPv6 route was unreliable.
- Advanced runtime identity to **Version 1.9.3 · Build v177**.

## v176 / 1.9.3 changes

- Reverted the v175 browser-side NOAA ENC vector coastline mask after visual testing showed large rectangular SST regions leaking over land at wider map views.
- `/sst-overlay` now preserves the native NOAA CoastWatch transparent/no-data cells exactly as returned by the Geo-Polar Blended SST product. It no longer fills transparent coastal cells and no longer synthesizes SST beneath land.
- Removed the SST `/coastline-geometry` dependency and the client-side SVG/Web-Mercator masking path. This favors truthful source coverage over a cosmetically precise shoreline that the approximately 5 km SST grid cannot support.
- Sea Surface Temp is again rendered as a normal Leaflet image overlay, at 0.50 opacity, with the fixed 45–75°F fishing-oriented scale unchanged.
- User-facing SST notes now explicitly describe the native CoastWatch shoreline/no-data edge as coarse in bays and estuaries and position the layer as a regional/offshore temperature-gradient aid rather than a shoreline-precision product.
- Chlorophyll Field remains on the v174 supersampled NOAA ENC raster-mask path; numeric SST/chlorophyll retrieval, Fishing Planning calculations, chlorophyll contours, and source datasets are unchanged.
- Advanced runtime identity to **Version 1.9.3 · Build v176**.

## v175 / 1.9.3 changes

- Reworked the Sea Surface Temp shoreline path after visual review showed that supersampling the NOAA ENC raster mask still left an obviously crude, blocky coastline.
- The SST proxy now fills CoastWatch's coarse transparent land/no-data cells from the nearest valid SST color before display. This prevents the native ~5 km grid mask from carving rectangular bites out of bays, headlands, and islands.
- The browser now requests NOAA ENC Direct `Coastal.Land_Area` vector geometry for the current map bounds through a new `/coastline-geometry` endpoint and uses that geometry as an SVG mask over the SST image.
- The SVG coastline mask is transformed in Web-Mercator screen space so the visible shoreline follows the Leaflet basemap rather than a separately rendered raster mask.
- SST opacity remains 0.58 and the fixed 45–75°F fishing-oriented color scale is unchanged. Numeric SST data, Fishing Planning calculations, chlorophyll data/contours, and source datasets are unchanged.
- Chlorophyll Field continues to use the v174 supersampled raster land mask; this v175 change is specific to Sea Surface Temp.
- Advanced runtime identity to **Version 1.9.3 · Build v175**.

## v174 / 1.9.3 changes

- Refined NOAA CoastWatch raster coastline clipping to reduce the coarse, stair-stepped shoreline visible around bays, headlands, islands, and other complex coastlines.
- NOAA ENC `Coastal.Land_Area` is now requested at 2× the displayed CoastWatch raster dimensions (bounded by the NOAA service image limit) and reduced back to the raster using fractional alpha coverage instead of a same-size binary land/no-land cutoff.
- The change is display-only: Sea Surface Temp and Chlorophyll numeric data, Fishing Planning calculations, chlorophyll contours, and source datasets are unchanged.
- Reduced the Sea Surface Temp Leaflet overlay opacity from 0.72 to 0.58 so the basemap coastline and place context remain easier to read through the approximately 5 km SST field.
- Coastline-clipped raster responses expose the supersampled NOAA ENC mask in the `X-Coastline-Mask` diagnostic header.
- Advanced runtime identity to **Version 1.9.3 · Build v174**.

## v137 / 1.9.2 changes

- Moved the live map scale/status readout to the lower-right corner.
- Established **Zoom 9** as the practical minimum for the NOAA Nautical Chart basemap.
- Nautical Chart is unavailable for new selection below Zoom 9.
- If Nautical is already the preferred basemap and the user zooms out below Zoom 9, Street Map is shown temporarily while the Nautical preference remains selected.
- A map notice explains that Nautical Chart is available at Zoom 9+.
- Zooming back to Zoom 9 or closer automatically restores the Nautical Chart.
- Legitimate inland/no-chart blank areas at supported nautical zoom levels remain unchanged.
- Advanced runtime identity to **Version 1.9.2 · Build v173**.

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

- Public app version: **1.9.3**
- Generated source build: **v234**
- Next generated source build: **v238**
- Authoritative repository: **https://github.com/richard-mauri/pittsburg-saildata**
- Authoritative branch: **main**
- Release status: **v234 / 1.9.3 release candidate**

### Managed-file checkpoints

| Repository file | SHA-256 |
| --- | --- |
| `main.go` | `f6e3ac34372688442618fe84e27414a1f9048069db294952b795eb49157d469c` |
| `assets/yogiisms.txt` | `4ebf00217e194ee26a8e8fe38237b298800b36ead0c64accdbb82f623c142371` |
| `assets/fishing_reports.json` | `02b01de77784153157c6a4a60d6ad21e286f7c191bbe204fed605659ea15ca5e` |
| `check-project-state.sh` | `85fa5062e2ae4509174b6843ebc0066f4a94e2f2e90001230ca74c07aeb500dc` |

<!-- PROJECT-STATE:END -->

`README.md` deliberately does not contain its own SHA-256 because that would create a self-referential checkpoint. Git provides the history/integrity record for README itself.

### Source-generation workflow

Complete Go source candidates are generated as `main-updated-vNN.go`. Generated candidates never overwrite repository `main.go` automatically. After review, manually copy the candidate to `main.go`, run the checker/build/tests, inspect the Git diff, and then commit/push.

The generated build number is immutable. Any change to generated Go source bytes requires a new `vNN` value and filename; do not reuse an earlier build number for a corrected candidate.

The public application version and generated build are separate identities. The current runtime identity is expected to render as:

`Version 1.9.3 · Build v234`

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

The shared **Wind units: Knots / MPH** and **Distance units: Nautical Miles / Miles** controls appear immediately below the hero image on both Conditions Now and Planning and Details. They use `wind_unit` and `distance_unit` query state so both preferences remain synchronized during navigation.

Planning and Details includes location selection, nearby wind-station discovery, current-station context, 1/3/7-day current planning, wind history from 1h through 24h, NWS forecast context, Local Conditions at a selected point, map types, independent map overlays, and Center Map controls.

The **Location** card treats selected location and map viewport center as separate state. Latitude/Longitude display the viewport center and can be edited without side effects; **Center Map → Latitude & Longitude** explicitly applies those values. Candidate wind stations appear only after an actual selected location exists. Detailed behavior is available from the card’s **ⓘ About location selection** popover.

The **Center Map** menu uses momentary actions for My location, Latitude & Longitude, selected location, selected wind station, and selected currents station. Centering pans without changing zoom or report selection state.

The selected currents station associated with the active wind station is shown automatically when available. **Clear selected location, station & candidates** removes the selected location, committed wind-station map selection, associated currents-station marker, and derived wind candidates while leaving active wind-barb overlays in place.

The **Local Conditions** panel is permanently reserved beside the Lat/Lon controls on wider screens to avoid layout jumps. For a selected location it uses NWS point metadata/forecast data to show nearby city/state, current-hour forecast temperature, expected high/low, and a short forecast phrase.

Dynamic HTML responses use no-cache headers so Safari/Dock WebView clients pick up new builds without requiring repeated manual website-data clearing. Runtime HTML displays both public version and generated build.

Map controls place **Map Types**, **Map Overlays**, and **Center Map** on one row. The compact scale/status readout now sits below the map and reports only the selected distance unit plus Leaflet zoom. Map Overlays is organized into accordion-style **Weather & Hazards**, **Wind & Pressure**, **Terrain & Seafloor**, and **Observations** groups. Wind barbs and observational isobars are intentionally grouped together because they are commonly interpreted as one wind/pressure picture; Saildrone remains under Observations. On narrow/iOS screens the overlay menu uses the fixed viewport-sheet behavior added in v212-v214 so Leaflet controls cannot cover it.

NOAA Nautical Chart is considered practical at **Zoom 9+**. If Nautical is the preferred basemap and the user zooms below 9, Street Map is shown temporarily with a notice; Nautical automatically returns at Zoom 9+. Legitimate inland/no-chart blank areas at supported zooms are left unchanged.

Map overlays include NWS forecast zone, NOAA HMS qualitative smoke, NOAA/NDBC Marine / Bay wind barbs, Aviation Weather Center METAR Land / Inland wind barbs, **Surface Pressure / Isobars**, **Sea Surface Temp**, NOAA/NESDIS cloud cover, and NEXRAD radar. The two wind-barb layers share buffered viewport loading but use independent observation networks; zoom-dependent collision thinning keeps wide-area views readable. Sea Surface Temp is deferred/disabled in v195; the future approach is documented in this README, variable `analysed_sst`, a daily global Level-4 blended SST field at about 5 km resolution. v191 uses the PFEL/ERD host and its matching `nesdis...` dataset identifier for the primary path, with a matching Central-host fallback only for SST imagery. v189 uses the current NOAA NESDIS ERDDAP identifier after the older `noaacwBLENDEDsstDNDaily` path stopped serving the deployed overlay. `/sst-info` resolves the latest available dataset time and `/sst-overlay` renders the current map bounds through ERDDAP `griddap` as a transparent PNG using a fixed **35–95°F** wide-area fishing-oriented scale. Because the ERDDAP source image is linear in latitude while Leaflet is EPSG:3857 Web Mercator, v180 server-side reprojects the SST scanlines into Web Mercator before returning the PNG. The browser displays that reprojected PNG as a normal Leaflet image overlay at 0.50 opacity. CoastWatch's native transparent/no-data edge is preserved and no secondary coastline mask is applied. The wider fixed range avoids painting most warm tropical/subtropical water with one saturated hottest color while preserving cross-view comparability. At low zooms, Leaflet world wrapping can extend the viewport outside -180°/+180°. v183 keeps the reliable single-image path: it clips the visible viewport to the one 360° world copy containing the map center, translates that interval into NOAA's canonical longitude range, and displays the returned SST image over only that clipped interval. The projection fix improves geographic alignment at wide map extents; the approximately 5 km source grid still limits shoreline-scale detail. Saildrone Observations is a separate optional moving-platform layer backed by NOAA PMEL public ERDDAP; it displays the latest available position and met-ocean readings from configured 2026 missions and is not treated as a persistent local station network. v187 discovers each mission's ERDDAP schema before requesting data, requires only time/position for plotting, and treats wind, SST, salinity, currents, and wave measurements as optional enrichments. HMS smoke uses the current warm yellow → amber → burnt-orange light/medium/heavy palette. Smoke is qualitative satellite analysis, not AQI or measured PM2.5.

The Welcome page reflects the current Conditions Now / Planning and Details workflow and retains the randomized Yogi Berra quotation. `assets/yogiisms.txt` currently contains the expanded 59-line quote set.

Non-HTML compatibility remains intentional: plain-text reports, compact text/JSON, Full Report Details, and `/voice` retain the established Bottom Line interfaces even though the browser heading is Conditions Now.

### v138 SST overlay

v138 adds **Sea Surface Temp (NOAA CoastWatch)** to Map Overlays. It uses NOAA/NESDIS/STAR ACSPO daily near-real-time sea-surface temperature through CoastWatch ERDDAP WMS. The server-side `/sst-info` endpoint reads the latest `time_coverage_end` from NOAA metadata so the browser requests a specific latest daily field and can display its timestamp. The overlay is intended primarily for coastal/offshore ocean context such as fishing; clouds, shorelines, and inland areas can contain gaps.

### New-chat continuation instruction

When migrating development to a new conversation, provide or point the assistant to the repository/README and say:

> Read the **Development State and Chat Handoff** section of README.md, treat GitHub `main` as authoritative, and continue from the recorded generated build. Generate complete `main-updated-vNN.go` candidates, never overwrite `main.go`, run `gofmt`, and provide SHA-256 hashes and download links.

The next source candidate should therefore be **v231** unless a newer local candidate is supplied.



### SST implementation note

Sea Surface Temp currently uses NOAA CoastWatch Central `nesdisBLENDEDsstDNDaily:analysed_sst`, the last browser-tested working SST path from v176. `/sst-overlay` requests an ERDDAP `griddap` transparent PNG for the current EPSG:4326 map bounds, with a fixed **35–95°F** wide-area fishing-oriented color scale, then server-side reprojects that image from its latitude-linear source grid into Leaflet Web Mercator before returning the PNG for display as a georeferenced Leaflet image overlay at 0.50 opacity. CoastWatch's native transparent/no-data mask is preserved and no second coastline product is applied. The wider fixed range is intended to reduce hot-end saturation during very warm tropical/subtropical conditions while preserving stable cross-view color meaning. When Leaflet's wrapped viewport extends beyond -180°/+180°, v183 preserves the single-request path by clipping to the world copy containing the current map center and translating that clipped interval into NOAA's canonical longitude domain. `/sst-info` reads the same dataset's metadata/time axis so image and timestamp remain source-consistent. The active SST-only CoastWatch transport continues to force IPv4 because the local IPv6 route previously timed out.

### SST WMS compatibility

Build v179 retires the failed v178 JPL MUR WMS display path and restores the v176 ERDDAP `griddap` transparent-PNG request. The active SST request uses explicit latitude/longitude slices in the CoastWatch data query, so WMS 1.1.1/1.3.0 axis-order rules no longer apply to the current display path. The v178 WMS behavior is retained only in the historical change log above.

### SST loading reliability

Build v173 keeps the v141 NOAA WMS 1.3.0 / `CRS=EPSG:4326` request and improves the image-loading path. The SST raster request now uses the map's CSS pixel dimensions with a maximum of 1200×900 instead of Retina/device-pixel doubling, the NOAA request timeout is 45 seconds, and the browser no longer preloads the same SST image before Leaflet requests it. Leaflet makes the single image request directly, while the status line remains `Loading NOAA CoastWatch SST image…` until the image either loads or reports an error.

### SST product change

Build v173 keeps the v142 WMS 1.3.0 proxy and single-image loading path, but switches the SST source to NOAA CoastWatch's MUR SST dataset, `noaacwBLENDEDsstDNDaily`, variable `analysed_sst`. NOAA's ERDDAP WMS documentation uses this exact dataset/layer in its working GetMap examples. The Geo-Polar product provides a daily global Level-4 blended SST analysis at about 5 km resolution and is served from NOAA CoastWatch Central rather than the PFEL host that was timing out from the local Go process.

### SST diagnostic fetch path

Build v173 keeps the MUR SST product and WMS 1.3.0 proxy, but changes the browser loading path to make upstream failures visible. The browser now `fetch()`es `/sst-overlay` first. If the proxy returns an error, the actual response text is shown in the SST status line instead of collapsing to a generic image-load failure. If the proxy returns a PNG successfully, the response is converted to a blob URL and displayed through Leaflet. Blob URLs are revoked when replaced or when the SST overlay is disabled.

### SST transport fallback

Build v173 keeps the MUR SST WMS request and v144 diagnostics, and hardens the Go HTTP transport for NOAA CoastWatch. The SST proxy now uses a cloned `http.Transport` with a 30-second TLS handshake timeout, a 45-second response-header timeout, and a 75-second overall request timeout. It first tries the standard CoastWatch ERDDAP endpoint and, on a transport/read failure, retries once against NOAA's `/wcn/erddap/` endpoint. Successful responses expose the endpoint label in `X-SST-Upstream`, and the browser includes that source in the SST status line.

### SST IPv4 transport workaround

Build v173 keeps the MUR SST product, WMS 1.3.0 request, endpoint fallback, and browser diagnostics from v145. The v145 diagnostics identified the actual transport failure: the local Go process resolved `coastwatch.pfeg.noaa.gov` to IPv6 and the IPv6 route timed out before the HTTPS request completed. The SST-only HTTP transport now forces `tcp4` through a dedicated `net.Dialer`; other application networking is unchanged. A successful SST status line includes `IPv4` so the workaround is visible during testing.

### SST source moved to CoastWatch Central

Build v173 changes the SST upstream hostname and product after repeated TLS failures to `coastwatch.pfeg.noaa.gov`. The overlay now uses NOAA CoastWatch Central at `coastwatch.noaa.gov`, dataset `noaacwBLENDEDsstDNDaily`, variable `analysed_sst`. This NOAA Geo-Polar Blended Day+Night product is a daily global Level-4 SST analysis at about 5 km resolution and is listed by NOAA as near-real-time. The existing WMS 1.3.0, EPSG:4326, browser fetch diagnostics, blob-image overlay, and SST-only IPv4 transport remain in place. The old PFEL/WCN SST retry path is removed so a known-bad host does not add long delays.

### SST fishing-view refinements

Build v173 keeps the working CoastWatch Central Geo-Polar Blended SST source from v147 and adds two UI refinements for practical offshore use. Sea Surface Temp is now treated as a Zoom 5+ overlay; below Zoom 5 the checkbox remains selected but the raster is removed and the status line tells the user to zoom in. Returning to Zoom 5+ automatically reloads the SST field. The old broad 32–95°F legend has been replaced by a qualitative Cooler → Warmer legend because the WMS image's color scaling is controlled by NOAA; this avoids implying exact temperature/color breakpoints that the app is not setting itself.

### SST temp-break presentation

Build v173 removes the hard SST minimum-zoom restriction. The SST overlay can be used at any zoom level; zoom level is now purely a usage choice.

The SST image path now uses NOAA CoastWatch ERDDAP `griddap` transparent PNG output instead of the WMS color defaults so the application can enforce a stable fishing-oriented temperature scale. The overlay uses `nesdisBLENDEDsstDNDaily:analysed_sst` with a fixed Rainbow palette from 45°F through 75°F, divided into approximately 1°F discrete bands. The on-page legend shows 45, 50, 55, 60, 65, 70, and 75°F. This is intended to make temperature breaks and boundaries between cooler and warmer water easier to identify and to keep the same color meaning as the map is panned or zoomed. Values below 45°F or above 75°F saturate at the palette endpoints.

### Step 2 — underwater structure overlay

Build v173 begins roadmap Step 2 while keeping the Step 1 SST-break behavior intact.

A new `Underwater Structure (NOAA bathymetry + names)` map overlay combines two NOAA sources for the current map view:

- NOAA/NCEI ETOPO shaded relief for broad seafloor shape and underwater topography.
- NOAA Marine Cadastre Undersea Feature Place Names for official named banks, seamounts, ridges, canyons, and related features.

The bathymetry is rendered below SST so temperature breaks remain visible. Official undersea names render above SST so the user can correlate a temp break with a named structure. The overlay refreshes after map movement, has no artificial zoom restriction, and is explicitly labeled as a fishing-planning aid rather than a navigation product.

ETOPO is a global relief model, so small fishing pinnacles may not be resolved. Step 2 can later add higher-resolution NOAA survey layers and a small curated fishing-feature alias registry for local names such as `the Guide` and `the 601` after their coordinates are verified.

### Step 2 label readability refinement

Build v173 keeps the v150 NOAA/NCEI ETOPO shaded-relief layer but replaces the NOAA server-rendered undersea-name image with locally styled vector labels from the NOAA Marine Cadastre `UnderseaFeaturePlaceNames` feature query service.

The app requests official point features for the current map bounds and displays only fishing-relevant structural names whose official names identify seamounts, banks, ridges, hills, knolls, shoals, reefs, rises, plateaus, pinnacles, or escarpments. Canyons and other lower-priority names are suppressed to reduce clutter.

Labels are rendered by Leaflet with larger cream/white text, a strong dark halo, and a small gold feature dot so they remain readable over blue bathymetry and can stay above the SST overlay. The status line reports how many fishing-relevant official features are currently shown. This preserves official NOAA naming—including features such as Guide Seamount—while avoiding the small blue-on-blue labels baked into the NOAA rendered map image.

### Step 3 — chlorophyll / water-clarity overlay

Build v173 implements roadmap Step 3 while preserving the completed Step 1 SST-break and Step 2 underwater-structure behavior.

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

Build v173 refines the Step 3 chlorophyll presentation after the first 4 km global product proved visually too dominant and blocky for the intended fishing workflow.

The underlying NOAA CoastWatch daily chlorophyll source is unchanged in this build, but the rendering is deliberately less intrusive:

- Overlay opacity is reduced from 0.58 to 0.34 so Sea Surface Temp breaks, bathymetry, and feature labels remain readable.
- The fixed chlorophyll range is narrowed from 0.05–5 mg/m³ to 0.05–2 mg/m³ on a logarithmic scale, putting more contrast into the cleaner-water range fishermen care about.
- UI wording now emphasizes `Clear-Water Edge` rather than presenting chlorophyll as a full-field water-clarity map.
- The legend is simplified to 0.05, 0.1, 0.2, 0.5, 1, and 2 mg/m³ and explicitly tells the user to look for the transition zone where clearer and greener water meet.

NOAA documents higher-resolution VIIRS sector products at about 750 m, including daily CoastWatch sector chlorophyll products. Those are the preferred future Step 3 upgrade once a reliable California-sector delivery path is wired into this app. Until then, the 4 km global product remains the working near-real-time source.

### Step 3 high-resolution chlorophyll refinement

Build v173 replaces the coarse ~4 km global chlorophyll source with NOAA CoastWatch's near-real-time S-NPP VIIRS 750 m sector product for the eastern Pacific:

- Dataset: `noaacwNPPVIIRSchlaSectorUYDaily`
- Variable: `chlor_a`
- Nominal resolution: 750 m
- Sector UY bounds: approximately 0.11°S to 44.88°N and 180.03°W to 119.97°W
- California and the offshore fishing grounds discussed in this project are within this sector.

The browser clips chlorophyll requests to the UY sector and displays the raster only over the actual intersecting bounds, avoiding geographic stretching when the map view extends inland east of the sector. Areas outside sector UY are intentionally left unpainted rather than falling back to the coarse 4 km source.

The Step 3 clear-water-edge presentation from v153 is retained: low opacity and a fixed logarithmic 0.05–2 mg/m³ scale so SST, underwater structure, and official feature labels remain readable. The purpose is to expose finer water-color boundaries that can be compared with Sea Surface Temp breaks and offshore structure.

### Step 3 high-resolution cloud-gap fill

Build v173 keeps the NOAA CoastWatch S-NPP VIIRS 750 m Sector UY chlorophyll source from v154, but changes the server-side rendering strategy to reduce the sparse "colored islands" caused by cloud masking.

Instead of showing only the newest daily scene, the Go server requests the five most recent daily scenes in parallel and builds a recency-prioritized mosaic. For each pixel, the newest valid chlorophyll value is used; if that pixel is transparent/masked in the newest scene, the server fills it from the next-most-recent scene, continuing through up to five scenes. This is intentionally not an average or temporal smoothing operation: it is a latest-valid-pixel cloud-gap fill.

The fixed logarithmic 0.05–2 mg/m³ clear-water scale, low overlay opacity, Sector UY clipping, SST-break layer, and underwater-structure labels remain unchanged. The goal is to preserve 750 m spatial detail while improving spatial continuity enough to make clear-water boundaries useful for fishing planning.

ERDDAP supports `last` and `last-n` time index selectors, which this build uses for the five recent daily scenes.

### Step 3 native NOAA gap-filled chlorophyll

Build v173 abandons the failed app-generated 5-scene mosaic from v155 and switches to NOAA's native DINEOF gap-filled chlorophyll analysis:

- Dataset: `nesdisVHNnoaaSNPPnoaa20chlaGapfilledDaily`
- Variable: `chlor_a`
- Product: NOAA multi-sensor Level-4 DINEOF chlorophyll-a
- Sensors: S-NPP VIIRS, NOAA-20 VIIRS, and Sentinel-3A OLCI
- Nominal resolution: about 2 km
- Coverage: global, daily, gap-filled upstream by NOAA

The app again requests a single transparent PNG from CoastWatch ERDDAP. NOAA performs the cloud-gap filling upstream, which removes the fragile `last-n` compositing logic and should provide a continuous field without reverting all the way to the coarse 4 km daily product.

The Step 3 fishing-oriented presentation remains: low overlay opacity and a fixed logarithmic 0.05–2 mg/m³ scale so clear-water boundaries can be compared with Sea Surface Temp breaks and underwater structure. This dataset is global, so the Sector UY clipping logic is removed.

### Step 3 diagnostic repair

Build v173 repairs a source-generation regression introduced during the v155/v156 chlorophyll experiments. Those candidates no longer contained dedicated `/chlorophyll-info` and `/chlorophyll-overlay` HTTP handlers, which explains why the browser checkbox could remain selected while no chlorophyll layer or chlorophyll attribution appeared.

v157 restores the chlorophyll handlers while preserving the working SST and underwater-structure routes. It uses the NOAA native DINEOF gap-filled dataset `noaacwNPPN20VIIRSDINEOFDaily`, variable `chlor_a`, and always asks ERDDAP for the actual last indexed field with `[last]`.

The metadata endpoint now prefers the time-axis `actual_range` endpoint when reporting the latest data time. The overlay proxy also exposes the served data time through `X-Chlorophyll-Time` and returns much more of NOAA's upstream error text, including the exact upstream request URL, when a PNG request fails. The browser surfaces that diagnostic text directly. This build is intentionally diagnostic: do not change chlorophyll products again until any remaining failure is observed in the returned error message.

### Step 3 presentation cleanup

Build v173 keeps the working NOAA native DINEOF gap-filled 9 km chlorophyll source and changes only its presentation.

The chlorophyll raster now uses ERDDAP's calmer `Ocean` palette instead of `Rainbow`, while retaining the fixed logarithmic 0.05–2 mg/m³ range. Overlay opacity is reduced from 0.34 to 0.26 so Sea Surface Temp breaks, bathymetry, undersea-feature labels, and the basemap remain visually dominant.

The on-page legend is also changed to a muted clean-water palette: deep blue through blue/cyan, subdued green, yellow-green, and muted brown. The design goal is to make the clear-water transition readable without turning the map into a multicolor heatmap.

### Step 3 fishing-contrast tuning

Build v173 keeps the working NOAA native DINEOF gap-filled 9 km chlorophyll source and retunes only the visual mapping for offshore fishing.

The chlorophyll overlay opacity is increased from 0.26 to 0.38. The map rendering range is tightened from 0.05–2 mg/m³ to 0.1–1 mg/m³ on a logarithmic scale, concentrating visual contrast in the offshore transition range instead of letting very high nearshore chlorophyll dominate.

The legend now emphasizes a stronger deep-blue → cyan → green → yellow progression with marks at 0.1, 0.2, 0.3, 0.5, 0.7, and 1 mg/m³. The goal is to make the cleaner-to-greener boundary obvious enough to compare with Sea Surface Temp breaks and underwater structure without returning to the noisy full-rainbow appearance.

### SST regression repair

Build v173 fixes an SST rendering regression introduced while adding the chlorophyll overlay-bounds logic. `refreshSSTOverlay()` was accidentally changed to call `L.imageOverlay()` with `overlayBounds`, a variable that exists in the chlorophyll path but not in the SST path. That JavaScript reference error occurred after the SST PNG was fetched, so the SST checkbox could remain selected while no SST raster or attribution appeared.

The SST image overlay now correctly uses its own current map `bounds` again. Chlorophyll continues to use its separate `overlayBounds` behavior unchanged. No SST product, palette, transport, or Step 1 temp-break behavior is otherwise changed.

### Step 3 redesign — chlorophyll as edge lines

Build v173 keeps SST as the colored raster and stops displaying chlorophyll as a second filled color raster.

The app still retrieves the NOAA CoastWatch native DINEOF gap-filled 9 km chlorophyll field, but the browser now converts that image into a transparent strong-gradient edge overlay. An adaptive threshold emphasizes roughly the strongest local chlorophyll gradients in the current view. The rendered line uses a dark halo with a bright center so it remains visible over both warm and cool SST colors.

This avoids hue mixing between SST and chlorophyll. The chlorophyll layer now answers a narrower fishing question: where are the stronger cleaner-to-greener water boundaries? It is explicitly an edge detector, not an exact concentration contour. SST temperature colors and the underwater-structure labels remain unchanged.

### Step 3 redesign — numeric chlorophyll contours

Build v173 replaces the v161 image-gradient edge detector with concentration contours derived from the NOAA numeric chlorophyll grid.

The `/chlorophyll-overlay` route now requests the latest `chlor_a` grid from `noaacwNPPN20VIIRSDINEOFDaily` as ERDDAP JSON, downsamples large map extents with ERDDAP stride, reconstructs the latitude/longitude grid, and runs server-side marching-squares contour extraction.

Only three chlorophyll contours are drawn:

- 0.2 mg/m³ — cyan
- 0.3 mg/m³ — emphasized cream/white primary clear-water transition reference
- 0.5 mg/m³ — gold

Each line has a dark halo so it remains readable over SST colors. This avoids the spaghetti-like local-gradient outlines from v161 and makes the chlorophyll layer an exact concentration-boundary overlay rather than an image edge detector. Sea Surface Temp remains the colored raster and underwater structure remains unchanged.

### Step 3 contour compile fix

Build v173 fixes the Go type errors in the v162 marching-squares contour renderer. The contour endpoints are floating-point pixel coordinates, but the `drawLine` helper was mistakenly declared with integer endpoint parameters. That caused the reported `math.Abs`, `math.Round`, arithmetic, and `crossings[].x/y` compile errors.

`drawLine` now accepts `float64` endpoints and rounds only when plotting pixels. No contour levels, chlorophyll data source, SST behavior, or underwater-structure behavior are changed.

### Step 3 split chlorophyll presentation

Build v173 separates chlorophyll into two independent overlays using the same NOAA CoastWatch DINEOF gap-filled 9 km source.

`Chlorophyll Field` restores a restrained semi-transparent background raster so broad water-mass features—such as low-chlorophyll pockets, eddies, and clean-water intrusions—remain visually obvious. It uses the fixed 0.1–1 mg/m³ logarithmic display range and sits below the contour layer.

`Chlorophyll Contours` remains a separate overlay and continues to draw exact 0.2, 0.3, and 0.5 mg/m³ concentration contours from the NOAA numeric grid. The two layers can be used independently or together. Sea Surface Temp remains a separate colored raster, and underwater structure remains unchanged.

This split is intended to preserve both kinds of information the prior experiments exposed: the broad chlorophyll background pattern and the sharper cleaner-to-greener boundary references.

### Fishing Reports overlay prototype

Build v173 adds a single `Fishing Reports (recent tuna snapshot)` overlay. It is intentionally one overlay rather than separate species/confidence layers.

The prototype contains recent public Northern/Central California tuna reports:

- Four Fort Bragg albacore reports from September 1, 3, 4, and 5, 2026.
- One broad bluefin regional report described as `Cordell to Monterey` from August 17, 2026.

The Fort Bragg reports use local shorthand such as `27×35`, `25×25`, `24×37`, and `20×20`. The app converts those to approximate degree-minute positions and always draws a dashed uncertainty circle so the map does not imply survey-grade coordinates. The report popup preserves the original location wording, catch summary, size notes, provenance, and a source link.

The Cordell-to-Monterey bluefin item is rendered as a dashed broad regional polygon, not a point, because the source did not provide an exact fishing coordinate.

This is a curated snapshot, not yet a live scraper or automated fishing-report feed. The intent is to validate the map model first: one Fishing Reports overlay containing exact, derived, and broad-regional report geometries with explicit confidence.

### Fishing Reports external data file

Build v173 removes the hard-coded fishing report arrays from `main.go`.

The map now loads `GET /fishing-reports`, and that server route reads and validates `assets/fishing_reports.json`. The JSON file is therefore the update point for future fishing reports; adding, removing, or refreshing report records no longer requires changing the Go source or JavaScript map implementation.

The initial `schema_version` is `1`. Each report carries a `position_type` such as `derived_point`, `exact_point`, or `region`, plus the same provenance/confidence fields already used by the v165 prototype. Derived points retain `uncertaintyNM`; regional reports retain a polygon. The browser renders whatever valid reports are present in the file.

For this candidate, copy `fishing_reports-updated-v166.json` to `assets/fishing_reports.json` before running the checker. The fishing report asset is now included in the README managed-file checkpoint table, so `check-project-state.sh` validates its SHA-256 automatically without any checker-script change.

This is still a manually curated feed. A future ingestion job can update `assets/fishing_reports.json` or generate the same schema without changing the map layer.

### Underwater structure label decluttering

Build v173 keeps the NOAA/NCEI bathymetry relief visible at all zoom levels but adds strict controls to undersea feature names.

Feature names are now hidden below Zoom 6. At wider planning scales the label count is capped progressively: 10 at Zoom 6, 24 at Zoom 7, 45 at Zoom 8, 65 at Zoom 9, and 85 at Zoom 10+.

Fishing-relevant features are prioritized before labels are placed. Seamounts, banks, ridges, and plateaus receive highest priority; rises and escarpments follow; secondary classes such as knolls, shoals, reefs, pinnacles, and hills are lower priority.

The browser also performs screen-space collision suppression using an estimated label footprint. When two candidate names would overlap, the lower-priority/later candidate is skipped. Within the same priority class, features nearer the current map center are considered first. The structure status line reports how many labels were shown versus how many eligible features were present in the viewport.

### Offshore Trip Planning — first operational panel

Build v173 adds an `Offshore Trip Planning` panel tied to the selected ★ map destination.

The new `/offshore-trip` endpoint combines two sources for the selected lat/lon:

- the existing NWS marine-zone forecast and active alerts for that point;
- the nearest usable NDBC realtime station found within 180 nmi, preferring the closest station among the first 12 candidates that actually returns usable realtime wind or wave data.

NDBC realtime standard-meteorological data are parsed for sustained wind, gust, significant wave height, dominant wave period, average period, mean wave direction, and observation time. Wind speed is converted from m/s to knots and significant wave height from meters to feet.

The browser panel shows observed wind, gust, significant wave height, dominant period, mean wave direction, the buoy/station name and distance from the selected destination, up to four NWS marine forecast periods, and any NWS alerts. It also surfaces explicit watch items when observed conditions cross practical planning thresholds: sustained wind at 15/20 kt, gusts at 25 kt, significant seas at 6/8 ft, or at least 4 ft of significant wave height with a dominant period of 8 seconds or less.

These watch items are deliberately not presented as a magic go/no-go score. They are planning flags only; the actual observed values and official NWS forecast text remain visible.

This first pass evaluates the selected offshore destination, not the entire transit corridor. A later enhancement can add launch point, route distance, multiple forecast zones/buoys along the route, and outbound-versus-return timing.

### Offshore Trip Planning startup fix and Sea Surface Temp terminology

Build v173 fixes the v168 startup case where the `Offshore Trip Planning` card could remain hidden when the Planning page loaded with an already-selected `lat`/`lon` in the URL. The page now explicitly refreshes the offshore trip panel during initialization whenever `mapState.selectedLocation` already exists, while retaining the existing refresh when the user clicks a new ★ destination.

This build also changes the user-facing SST wording to `Sea Surface Temp` for clarity. The map overlay checkbox and legend now use `Sea Surface Temp`, and explanatory text spells out sea-surface temperature where appropriate. Internal JavaScript identifiers, `/sst-*` endpoints, diagnostic headers, and data-source plumbing remain unchanged for compatibility.

### Fishing Planning subsection

Build v173 keeps the existing `Offshore Trip Planning` card for weather, buoy observations, swell/period context, NWS forecast periods, alerts, and operational watch items, and adds a separate `Fishing Planning` subsection inside that card.

For the selected ★ destination, the Fishing Planning subsection now summarizes four fishing-specific factors:

- `Sea Surface Temp` — the latest NOAA CoastWatch Geo-Polar Blended daily SST at the selected point plus the temperature spread across an approximately 15 nmi neighborhood. The UI classifies that local spread as weak, moderate, or pronounced temp-break signal rather than pretending to know an exact break line distance.
- `Chlorophyll` — the latest NOAA CoastWatch DINEOF chlorophyll value at the selected point, a simple cleaner/transition/greener-water classification, and whether the 0.30 mg/m³ transition is crossed inside the same nearby sampling window.
- `Structure` — the nearest fishing-relevant NOAA Marine Cadastre undersea feature found within roughly one degree of latitude of the selected point, with distance in nautical miles.
- `Recent reports` — point fishing reports within 100 nmi, prioritizing the newest report, plus broad regional reports when the selected point falls inside a report polygon.

The new `/fishing-water` endpoint retrieves numeric CoastWatch SST and chlorophyll grid values server-side. The Fishing Planning subsection then combines those water values with the existing NOAA structure service and the existing `assets/fishing_reports.json` feed in the browser.

The subsection provides a short fishing-setup synthesis based on the visible factors, but it does not create an opaque numerical score. The underlying water values, structure distance, and report context remain visible so the user can make the fishing decision.

This is still selected-destination analysis rather than route-wide fishing analysis. A later enhancement can evaluate where the strongest SST/chlorophyll alignment lies around the destination instead of only summarizing the local neighborhood.

### Offshore Trip Planning eligibility gate

Build v173 makes the entire `Offshore Trip Planning` card conditional on the selected ★ location actually qualifying as offshore/coastal-ocean water.

The `/offshore-trip` endpoint now returns an explicit `is_offshore` boolean. It resolves the NWS forecast zone for the selected point, reads the zone name from the NWS forecast-zone API, and only treats recognized ocean/coastal marine zone families as eligible. Land forecast zones therefore do not qualify.

For the San Francisco/Monterey region, the enclosed-water zones `PZZ530` (San Pablo Bay, Suisun Bay, West Delta, and San Francisco Bay north of the Bay Bridge), `PZZ531` (San Francisco Bay south of the Bay Bridge), and `PZZ535` (Monterey Bay) are explicitly excluded from the offshore card. A defensive zone-name check also excludes San Francisco Bay, San Pablo Bay, Suisun Bay, West Delta, and Sacramento-San Joaquin Delta wording.

The browser now keeps the card hidden while offshore eligibility is being resolved. Only after `is_offshore: true` is returned does it reveal the card and run the Fishing Planning subsection. Non-offshore selections also skip the NDBC offshore-buoy lookup and the Fishing Planning water/structure/report requests.

This means a selected location in Antioch, the Delta, San Pablo Bay, or San Francisco Bay will not show `Offshore Trip Planning`, while selected coastal-ocean/offshore points can still use the full trip and fishing-planning workflow.

### Release 1.9.3 / Build v173

Build v173 is a release-only bump from the v171 feature-complete candidate. There are no functional changes from v171.

Public version changes from `1.9.2` to `1.9.3`, and the immutable generated source lineage advances from `v171` to `v172`.

Release 1.9.3 includes the offshore/fishing planning work completed across the preceding development builds: Sea Surface Temp terminology and temp-break context, chlorophyll field and contours, decluttered underwater structure, externalized fishing reports, Offshore Trip Planning with NWS/NDBC conditions, Fishing Planning synthesis, and the offshore eligibility gate that suppresses the offshore card for inland, Delta, Bay, and other non-offshore selections.

### NOAA coastline clipping for Sea Surface Temp and Chlorophyll

Build v173 adds server-side coastline clipping to the displayed Sea Surface Temp and Chlorophyll Field rasters.

The clipping mask comes from NOAA ENC Direct's `Coastal.Land_Area` polygon layer (layer 171). For each CoastWatch image request, the server requests a transparent NOAA ENC land-mask PNG using the exact same geographic bounds and pixel dimensions as the CoastWatch raster. Pixels identified as land are then made transparent before the image is returned to Leaflet.

This is a display-only operation. The original NOAA CoastWatch numeric grids used by Fishing Planning, temp-break analysis, chlorophyll context, and contour generation remain unchanged.

If the NOAA coastline-mask request fails or returns an incompatible image, the raster request fails with a diagnostic error rather than silently showing un-clipped land data. Both clipped raster responses include an `X-Coastline-Mask` diagnostic header identifying the NOAA ENC land source.

This build does not add raster smoothing or interpolation. The native data resolution and existing fishing-oriented color scales are preserved.

### Coastline rendering refinement / Build v174

Build v174 refines the server-side NOAA ENC land mask introduced in v173 after visual review showed that a same-size binary mask could look crudely registered against detailed basemap coastlines.

The CoastWatch display raster is still clipped against NOAA ENC Direct `Coastal.Land_Area` (layer 171), but the mask request is now supersampled at 2× the CoastWatch display dimensions, subject to the NOAA service's 4096-pixel image limit. Each CoastWatch pixel receives fractional land coverage derived from the higher-resolution mask rather than being discarded whenever a single same-resolution mask pixel crosses a fixed alpha threshold. This produces an antialiased transition at the shoreline and reduces visible stair-stepping without altering the underlying CoastWatch field.

Sea Surface Temp display opacity is reduced from 0.72 to 0.58 so Street Map, Nautical Chart, Satellite, and Hybrid coastline context remains more legible beneath the approximately 5 km NOAA Geo-Polar Blended SST analysis. This does not increase the scientific resolution of the SST product and should not be interpreted as higher-resolution near-shore temperature data.

The same supersampled clipping helper is also used by the Chlorophyll Field raster. Numeric SST/chlorophyll retrieval, Fishing Planning statistics, chlorophyll contours, and the source datasets remain unchanged.

### Vector coastline masking for Sea Surface Temp / Build v175

Build v175 replaces the SST-specific raster coastline cutout with a browser-side vector mask. CoastWatch's image still contains coarse transparent land/no-data cells inherited from the approximately 5 km SST grid, so `/sst-overlay` first fills those transparent cells outward from the nearest valid SST color. That infill is only a display-preparation step; the original CoastWatch numeric grid used for Fishing Planning remains unchanged.

The browser then requests NOAA ENC Direct `Coastal.Land_Area` geometry from `/coastline-geometry`. The server proxies the NOAA ArcGIS layer as GeoJSON for the current map bounds with modest geometry simplification. The client converts the polygons into an SVG mask in Web-Mercator screen coordinates and applies that mask to the SST image inside Leaflet. Land is therefore hidden by NOAA vector shoreline geometry rather than by a second rasterized coastline image.

This specifically targets the visibly crude Bay Area shoreline registration seen in v173/v174. The SST field remains a coarse daily analysis, and the new mask does not imply finer near-shore temperature accuracy. If NOAA ENC geometry cannot be fetched or parsed, the SST overlay fails with a diagnostic message instead of falling back to unmasked land coverage. Chlorophyll Field remains on the v174 supersampled raster-mask path for now.
### Native CoastWatch shoreline policy / Build v176

Build v176 retires the v175 SST vector-mask experiment after browser testing showed that the client-side SVG mask could expose large rectangular SST areas over land at wider Bay Area views. The failure demonstrated that independently reconstructing the shoreline from NOAA ENC geometry is not a reliable display path for this coarse raster product.

`/sst-overlay` now returns the NOAA CoastWatch transparent PNG with its native land/no-data transparency preserved. The server does not fill transparent SST cells and does not apply a second coastline product to Sea Surface Temp. The browser displays that image directly as a Leaflet image overlay at 0.50 opacity.

The tradeoff is intentional: the approximately 5 km Geo-Polar Blended SST grid can show a visibly coarse edge around complex coastlines, bays, and estuaries. That edge is now treated as an honest representation of source resolution rather than hidden behind a synthetic shoreline. The Sea Surface Temp layer should therefore be used to identify regional/offshore gradients and temperature breaks, not to infer temperature immediately against a detailed shoreline.

The fixed 45–75°F display scale and numeric CoastWatch values used by Fishing Planning remain unchanged. Chlorophyll Field continues to use the v174 supersampled NOAA ENC raster mask; this rollback applies only to the displayed Sea Surface Temp raster.

