# Delta Sailing Data

A Go service that combines NOAA/NDBC wind observations with NOAA CO-OPS current predictions to produce sailing-oriented reports for the San Francisco Bay and Delta.

The project supports command-line use, plain-text and JSON reports, and a browser UI with an interactive Leaflet map. The default wind station is **PSBC1**.

## What it does

The service answers two practical questions: what the wind is doing now, and what the current is expected to do during the sailing window.

Wind observations come from NOAA/NDBC. Current predictions come from NOAA CO-OPS. The service combines those sources into concise reports, nearby-station choices, current-event timelines, and planning-oriented summaries.

## Browser workflow

The current map workflow is intentionally simple.

Click the map to set the **★ Selected location**. Then use **Find stations near selected location** to retrieve nearby wind stations. Panning and zooming only change the view; they do not change the search location. To search somewhere else, click the map again to move the selected location and run Find again.

The map legend is intentionally not context-specific. It always defines **★ Selected location**, **▲ Selected wind station**, **△ Nearby wind station**, and **◆ Currents station**, even when one or more of those markers are not currently present on the map.

Clicking a nearby wind-station candidate opens a fixed information panel. The candidate is not committed until **Use this wind station** is selected. When a suitable currents prediction station is available within the automatic-selection distance limit, the panel previews that station. If no suitable currents station exists within the limit, the panel explicitly reports that no nearby currents prediction station is available.

The map includes recenter controls for the selected location, selected wind station, and currents station. These controls preserve the user's current zoom level.

## Features

### Wind

NOAA/NDBC real-time observations are used for wind data. The service supports the default PSBC1 station as well as arbitrary active NDBC stations. Reports include current wind direction, speed and gust, recent observations, statistics and trends, previous-afternoon summaries, and historical reports.

The service dynamically retrieves NDBC station metadata instead of maintaining a hard-coded list of wind stations.

### Currents

NOAA CO-OPS current predictions provide flood, ebb, slack, current direction, prediction bin/depth, timelines, and current charts. The service automatically chooses a suitable current-prediction station and supports manual station/bin overrides. Automatic current-station selection is capped at **30 nautical miles** from the selected wind station; farther stations are treated as unavailable rather than presented as representative local current data. Explicit `current_station` overrides are not blocked by this automatic-selection limit.

The HTML report also supports one-, three-, and seven-day current views and planning hints for a preferred sailing period.

### Service

The application provides a command-line interface and HTTP service. Important endpoints are `/report`, `/wind-stations`, `/health`, and `/welcome`.

`/report?format=html` returns the interactive browser report. Plain text and JSON are also supported.

## Program structure

The application is split across the Go package rather than being built from `main.go` alone.

```text
sailing-go/
├── main.go
├── wind.go
├── currents.go
├── go.mod
└── README.md
```

`main.go` contains application orchestration, the REST server, report rendering, embedded HTML/JavaScript, map interaction, and combined report formatting.

`wind.go` contains NDBC observation retrieval, parsing, wind conversion, station discovery, statistics, trends, and historical wind handling.

`currents.go` contains NOAA current-station metadata, station selection, prediction retrieval, flood/ebb/slack processing, and current-report generation.

Build the complete package rather than compiling an individual `.go` file.

## Data sources

Wind observations are retrieved from NOAA National Data Buoy Center `realtime2` products. Active station metadata is retrieved dynamically from the NDBC active-stations feed.

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

## Recent map/UI recovery and simplification

The map UI went through a recovery and simplification pass after several state-management regressions. The goal of the current design is to keep the selected sailing location, committed stations, candidate stations, viewport, and UI status from accidentally acting as competing sources of truth.

The browser map now maintains authoritative client-side state for the selected location, selected wind-station ID, wind candidates, search activity, and currents-overlay preference. Candidate markers are rebuilt from the candidate state rather than incrementally accumulated.

Station IDs are normalized before comparison. Literal wrapping quotes are stripped, whitespace is trimmed, IDs are upper-cased, the committed wind station is excluded from the candidate set, and duplicate candidate IDs are suppressed. This prevents the selected **▲** wind station from also appearing as an outlined **△** candidate.

The fixed wind-station information panel replaced clipped or unstable marker tooltips for candidate selection. Clicking a candidate previews information; **Use this wind station** performs the actual commit.

The currents overlay is treated as user state rather than being inferred from whether a Leaflet marker happened to exist during initial page construction. The checkbox is no longer permanently disabled merely because a current marker was absent at initialization, and candidate-current preview can create the currents marker lazily when suitable current metadata is available.

Automatic currents preview and committed-report selection now share a **30 nmi** usefulness limit. A candidate whose nearest suitable NOAA current prediction station is farther away does not get a misleading ◆ preview marker. The candidate information panel instead says that no nearby currents prediction station is available. When that wind station is committed, the Current card uses the distinct message **No nearby currents prediction station** rather than the generic **Current prediction unavailable**, which remains reserved for actual current prediction/data failures.

Map initialization now distinguishes a missing `map_center_lat` or `map_center_lon` URL parameter from numeric zero. This matters because JavaScript's `Number(null)` evaluates to `0`; without the explicit missing-value check, an ordinary report URL could incorrectly initialize the Leaflet map near **0°, 0°** instead of near PSBC1 or the server-provided map center.

The earlier **Search this area for wind stations** mode was removed. There is now one station-discovery action: choose a **★ Selected location**, then use **Find stations near selected location**. This removes the ambiguous distinction between viewport-center searches and selected-location searches. The `/wind-stations` request now searches around the selected `lat` and `lon`; `search_lat` and `search_lon` are no longer part of this UI workflow.

The recenter controls for **★ Selected location**, **▲ Selected wind station**, and **◆ Currents station** preserve the user's zoom level. They change map center only, rather than forcing a minimum zoom such as 12.

## Inherited recovery commit

The current code also includes the functional recovery from commit:

```text
8ff7298 fix: correct double mapState dereference and reset button label in UI
```

That commit was created with aider using Claude Sonnet 4.6 after a damaged intermediate state was saved.

The important functional repair corrected an erroneous client-side reference from `mapState.mapState.selectedWindStationID` to `mapState.selectedWindStationID`. That bug could break candidate rendering because the renderer was dereferencing a nonexistent nested `mapState`.

The same recovery commit changed the reset control from the accidentally leaked implementation wording **Clear mapState.selectedLocation point** to the user-facing **Clear selected location point**.

That commit also normalized a number of typographic apostrophes in embedded HTML text and fixed a JavaScript indentation issue. Those were cleanup changes rather than behavioral map fixes.

The recorded history at that point was:

```text
8ff7298 fix: correct double mapState dereference and reset button label in UI
fa53fef Save damaged ChatGPT state before recovery
29149f0 Improve tidal-current planning and multi-day navigation
c294e67 feat: add map search-area workflow for wind stations
3ef9ac3 feat: add clickable wind station map markers and distance warnings
```

The search-area workflow from `c294e67` is part of the project's history, but the current UI deliberately removes that mode in favor of the simpler selected-location-plus-Find workflow.

## Useful San Francisco Bay / Delta wind stations

Useful references include PSBC1 for Pittsburg/Suisun Bay, PCOC1 for Port Chicago, MZXC1 for Martinez, UPBC1 for the Martinez bridge area, DPXC1 for Davis Point, RCMC1 and PPXC1 for Richmond, TIBC1 for Tiburon, and FTPC1 for the central/southern Bay.

These are reference stations, not a hard-coded application whitelist. The service discovers active stations dynamically.


## Maintainability note

`main.go` is currently large and relatively monolithic. In addition to application startup and HTTP orchestration, it contains substantial embedded HTML, CSS, JavaScript, and Leaflet map behavior. That concentration makes UI state changes harder to reason about and increases the risk of regressions when otherwise small map changes touch several concerns at once.

This is not currently a reason to refactor a working deployment. A future cleanup should be treated as a separate, deliberate project after the current behavior is stable.

The safest first step would be to separate the browser-facing assets from `main.go`: move the embedded templates, CSS, and Leaflet JavaScript into dedicated template/static files while preserving behavior. After that, HTTP/report orchestration could be moved into a `report.go` or `handlers.go`, leaving `main.go` primarily responsible for startup, configuration, and route registration.

The existing `wind.go` and `currents.go` split already provides a useful boundary for the data-source logic. Any future refactor should preserve those boundaries and prioritize behavior-preserving moves over simultaneous redesign.

## Development note

The browser map is deliberately being kept simpler than earlier iterations. When adding future map features, prefer explicit state transitions and a single rendering path over event handlers that directly mutate unrelated Leaflet layers and DOM controls.

A useful invariant is that the selected sailing location is the search anchor, the selected wind station is represented only by the committed **▲** marker, and nearby candidates are a rendering of the current candidate set rather than persistent incremental map objects.

## Latest UI behavior

The **Find** control remains visible even before a selected location exists. It is disabled until the user clicks the map, with wording that makes the required action explicit. The reset control is also always present and becomes enabled once a selected location exists.

The former **Show Conditions** control was removed because it could appear to act on the current viewport while actually reusing the previously selected location. The current workflow is intentionally explicit: set the **★ Selected location**, find nearby wind stations, inspect a candidate, and commit it with **Use this wind station**.

Recenter controls preserve the current zoom level. Panning never changes the selected sailing location, and the selected location remains the sole search anchor.

The map legend is a fixed vocabulary rather than a reflection of current state. It always shows all four symbol meanings so an absent marker does not also remove its explanation.

## Planning thresholds and Bottom Line

Current-planning thresholds are independently configurable for ebb and flood. The current defaults are:

- preferred below **2.0 kt**
- caution from **2.0 kt** up to **3.0 kt**
- red flag at **3.0 kt** and above

The browser UI exposes independent caution and red-flag thresholds for ebb and flood. The query parameters `caution_ebb` and `caution_flood` control caution thresholds; `max_ebb` and `max_flood` retain their existing names and represent the red-flag thresholds.

The **Bottom Line** card summarizes the entire displayed one-, three-, or seven-day planning period using the worst status present: any red-flag day makes the period Red Flag; otherwise any caution day makes it Caution; otherwise it is Preferred. The card also reports the count of preferred, caution, and red-flag days and uses a corresponding background treatment for quick scanning.
