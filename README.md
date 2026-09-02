# Mauri's Weather & Water Conditions

A Go service for San Francisco Bay and Delta sailing conditions. It combines NOAA/NDBC wind observations, NOAA CO-OPS current predictions, National Weather Service forecast context, and optional map overlays into a practical browser dashboard, text reports, JSON output, and a compact voice-oriented Bottom Line.

The default wind station is **PSBC1**.

## Current release

**Public version: 1.9.0**  
**Generated source lineage: v115**

Version 1.9.0 introduces a streamlined browser workflow. The main conditions page now focuses on the **Bottom Line**, including compact wind metrics and a one-day tidal-current graph. The rest of the dashboard is available from a separate **Planning and Details** page, which preserves the active query state and provides the full set of planning, map, current, wind, forecast, and customization controls.

## What it does

The service is designed to answer two practical questions:

1. What is the wind doing now?
2. What are the tidal currents expected to do during the preferred planning period?

Wind observations come from NOAA/NDBC. Current predictions come from NOAA CO-OPS. Forecast-zone context and marine forecast information come from the National Weather Service. Optional map context includes NOAA/NESDIS satellite cloud cover, NOAA HMS smoke analysis, and NEXRAD radar imagery.

The application is a conditions-planning aid. It is not a navigation system and is not a substitute for official navigation products, charts, notices, or prudent seamanship.

## Browser workflow

### Bottom Line page

Open:

```text
/report?format=html
```

The main browser page is intentionally compact. It presents:

- the selected location/station context
- Bottom Line wind metric tiles
- the latest observation time
- a one-day tidal-current graph
- a **Planning and Details →** link

The Bottom Line graph reuses the same current-chart renderer as the full Currents card, but it is fixed to a one-day range so the landing page stays concise and immediately useful on a phone.

If the one-day current graph cannot be generated, the existing Bottom Line current narrative remains available as a fallback.

### Planning and Details page

Open:

```text
/planning
```

or use the **Planning and Details →** link from the Bottom Line page.

The Planning and Details page reuses the same report-generation path as the main report and preserves the current query parameters. It contains the full operational dashboard, including location selection, wind station tools, maps and overlays, wind history, current planning controls, current-range controls, forecast context, tidal/lunar context, and supporting reference cards.

A **← Back to Bottom Line** link returns to the streamlined conditions page while preserving the active report state.

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

A user can establish the selected location by:

- clicking the map
- entering latitude and longitude
- using browser geolocation with **My location**

Panning or zooming does not change the selected location.

Nearby wind-station discovery is centered on the selected location. Candidate stations are previewed before the user commits one as the wind source.

The map includes recenter controls for:

- selected location
- selected wind station
- selected currents station

Recenter actions preserve the current zoom level.

## Map Types

The **Map Types** dropdown provides mutually exclusive basemaps:

- Street Map
- Nautical Chart
- Satellite
- Hybrid

Changing basemap does not change location, station, forecast, current, or planning state.

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

The streamlined HTML Bottom Line presentation does not replace the established non-HTML interfaces.

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

The current release candidate is **1.9.0**.

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
