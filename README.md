# Mauri's Sailing Outlook

Mauri's Sailing Outlook is a small Go service for turning NOAA/NDBC
marine observations and NOAA CO-OPS tidal-current predictions into
sailing-oriented reports for humans, voice assistants, and API clients.

The project began as a command-line utility for checking conditions
around Pittsburg and the San Francisco Bay--Delta system. It now
supports arbitrary NDBC wind stations, historical reports, automatic
nearby current-prediction selection, JSON output, and a REST API
suitable for voice clients such as Alexa, Siri, ChatGPT, and other
HTTP-capable assistants.

## Features

-   NOAA/NDBC real-time wind observations
-   Default wind station: **PSBC1 --- Pittsburg (Suisun Bay), CA**
-   Arbitrary active NDBC wind-station selection
-   Current wind/gust and recent/longer-term wind statistics
-   Historical wind reports using a requested local date/time
-   ±30-minute historical wind observation window
-   NOAA CO-OPS tidal-current predictions
-   Dynamic NOAA current-prediction station discovery through MDAPI
-   Automatic current-station selection using geographic and
    sailing-oriented scoring
-   Prediction-bin and depth awareness
-   Explicit current-station/bin overrides for validation or local
    knowledge
-   Historical `-at` reports with current predictions for the same date
-   Plain-text reports
-   JSON output for programmatic and voice-client use
-   Concise output mode
-   Branded, mobile-friendly HTML dashboard
-   Sailing-photo hero image served from `assets/hero.jpg`
-   Full text-report details embedded in the HTML page
-   REST API with `/report` and `/health`
-   GitHub → Render deployment

## Data sources

### Wind observations

Wind observations come from the [NOAA National Data Buoy Center
(NDBC)](https://www.ndbc.noaa.gov/).

The service reads NDBC `realtime2` station files:

``` text
https://www.ndbc.noaa.gov/data/realtime2/<STATION>.txt
```

NDBC reports wind direction in degrees true and wind speed/gust in
meters per second. The service converts wind speeds to knots.

The NDBC station lookup page is:

``` text
https://www.ndbc.noaa.gov/to_station.shtml
```

This is useful for finding station IDs outside the default Pittsburg/San
Francisco Bay--Delta area.

### Current predictions

Tidal-current predictions come from NOAA CO-OPS.

The service dynamically retrieves the NOAA current-prediction station
catalog from the CO-OPS Metadata API (MDAPI):

``` text
https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations.json?type=currentpredictions&units=english
```

The metadata includes information such as:

-   station ID
-   station name
-   latitude/longitude
-   prediction type
-   current prediction bin
-   prediction depth
-   depth reference/type

The catalog is cached in memory so a metadata request is not required
for every sailing report.

After selecting a current-prediction station and bin, the service
retrieves `currents_predictions` from the NOAA CO-OPS Data API using
`interval=max_slack`.

This provides the important current events used by the report:

-   maximum flood
-   slack
-   maximum ebb
-   next slack

## Current-station selection

Wind stations and current-prediction stations are different NOAA assets.
A nearby wind station therefore cannot simply be used as a current
station.

The service first obtains the latitude/longitude of the requested NDBC
wind station. It then evaluates NOAA CO-OPS current-prediction stations
using a scoring heuristic.

The automatic selector currently favors:

-   geographic proximity
-   harmonic/reference prediction stations
-   shallow prediction depths appropriate to the sailing-oriented report
-   open-water/point locations over narrow slough, creek, river, bridge,
    channel, or entrance locations

Distance is calculated using the Haversine formula.

The selection algorithm is deliberately kept separate from NOAA
prediction retrieval so it can be refined without changing the REST API.

### Pittsburg validation

For **PSBC1**, the automatic selector currently chooses:

``` text
SFB1325
Simmons Point, 0.6nm ESE of
bin 9
depth 6 ft
harmonic/reference prediction
```

This was validated against the BASK trip-planner display and directly
against NOAA CO-OPS.

For the August 23, 2026 comparison, NOAA `SFB1325`, bin `9`, returned
approximately:

``` text
12:47 PM  Max flood  1.87 kt → 102°
 3:54 PM  Slack
 6:07 PM  Max ebb    0.77 kt → 281°
 8:05 PM  Slack
```

The BASK display agreed essentially exactly, with only approximately
one-minute differences for two events.

This validation was important because an earlier implementation selected
`s06010 — Martinez-AMORCO Pier`, producing a substantially different
current regime.

## Build

The project uses separate Go source files:

``` text
main.go
wind.go
currents.go
```

Build the package rather than an individual source file:

``` bash
go build -o sailing-go .
```

Run the default Pittsburg report:

``` bash
./sailing-go
```

## Command-line usage

### Current Pittsburg report

``` bash
./sailing-go
```

### Another NDBC wind station

``` bash
./sailing-go -station RCMC1
```

### Historical report

Historical mode includes the historical wind observations around the
requested time **and NOAA current predictions for the same local date**:

``` bash
./sailing-go -at "2026-08-22T12:00"
```

Another station:

``` bash
./sailing-go -station RCMC1 -at "2026-08-22T12:00"
```

### Explicit current-prediction override

Automatic current selection is the normal mode, but a known NOAA
prediction station and bin can be forced:

``` bash
./sailing-go \
  -station PSBC1 \
  -current-station SFB1325 \
  -current-bin 9
```

The override also works with historical reports:

``` bash
./sailing-go \
  -at "2026-08-22T12:00" \
  -current-station SFB1325 \
  -current-bin 9
```

This is useful for validation, debugging, and locations where local
sailing knowledge should take precedence over the automatic selector.

### Current-station diagnostics

To inspect the automatic selection process:

``` bash
DEBUG_CURRENT_STATIONS=1 ./sailing-go
```

The diagnostic output shows nearby candidates with information
including:

``` text
ID
name
distance
selection score
bin
depth
depth type
prediction type
```

For PSBC1 it also makes it easy to inspect the available Simmons Point
prediction depths/bins.

## Report structure

A normal sailing report is organized around a quick decision-oriented
summary followed by supporting detail.

Example:

``` text
SAILING OUTLOOK — 9415115 - Pittsburg (Suisun Bay), CA (PSBC1)
================================

BOTTOM LINE
--------------------------------
Latest wind at 5:48 AM: WNW 10 kt, gusting 12 kt.
At 12:00 PM, current is predicted to be flooding.
Slack is around 3:54 PM, then the current turns to an ebb.

WIND
--------------------------------
...

CURRENT
--------------------------------
Selection: automatic-scored.
Using SFB1325 — Simmons Point, 0.6nm ESE of, 1.7 nmi from psbc1.
Prediction depth: 6 ft.
Prediction bin: 9.
...

CURRENT EVENTS
--------------------------------
12:47 PM  Max flood  1.87 kt → 102°
 3:54 PM  Slack
 6:07 PM  Max ebb    0.77 kt → 281°
```

The exact observations and predictions naturally vary by report date and
time.

## HTML dashboard

The server now includes a branded, mobile-friendly web presentation
called **Mauri's Sailing Outlook**.

The dashboard includes:

-   a sailing-photo hero image
-   Bottom Line summary
-   Wind card
-   Current card
-   visual current-event timeline
-   the complete text report in a detailed section

The HTML presentation uses the same report data as the text/JSON API. It
is a presentation layer rather than a separate weather/current
calculation path.

The hero photograph is stored in the repository at:

``` text
assets/hero.jpg
```

The Go server exposes it at:

``` text
/assets/hero.jpg
```

When deploying to Render, commit the `assets/hero.jpg` file along with
the Go source.

### Local HTML dashboard

Start the server:

``` bash
./sailing-go -server
```

Then open:

``` text
http://localhost:8080/
```

The root URL redirects to the HTML presentation.

HTML can also be requested explicitly:

``` text
http://localhost:8080/report?format=html
```

Another wind station can be selected in the dashboard:

``` text
http://localhost:8080/?station=RCMC1
```

Historical HTML works through the same query parameters:

``` text
http://localhost:8080/?station=PSBC1&at=2026-08-22T12:00
```

### Production HTML dashboard

The Render deployment is:

``` text
https://pittsburg-saildata.onrender.com/
```

Important: `/report` intentionally remains the plain-text/API endpoint.
To view HTML, use either the site root or `format=html`:

``` text
https://pittsburg-saildata.onrender.com/
https://pittsburg-saildata.onrender.com/report?format=html
```

This separation preserves backward compatibility for curl, Alexa,
ChatGPT, Siri, and other clients already using `/report`.

## REST API

Start the server:

``` bash
./sailing-go -server
```

The default local port is `8080`. On Render, the service uses the `PORT`
environment variable.

### Health check

``` bash
curl -sS http://localhost:8080/health
```

### Current plain-text report

`/report` defaults to the plain-text API response; it does **not**
default to HTML.

``` bash
curl -sS "http://localhost:8080/report"
```

### Another wind station

``` bash
curl -sS "http://localhost:8080/report?station=RCMC1"
```

### Historical wind + current report

``` bash
curl -sS \
  "http://localhost:8080/report?station=PSBC1&at=2026-08-22T12:00"
```

### Force a current station/bin

``` bash
curl -sS \
  "http://localhost:8080/report?station=PSBC1&current_station=SFB1325&bin=9"
```

### HTML

Request the HTML dashboard explicitly with:

``` bash
curl -sS "http://localhost:8080/report?format=html"
```

In a browser, normally use the simpler root URL:

``` text
http://localhost:8080/
```

### JSON

Request JSON with the `Accept` header:

``` bash
curl -sS \
  -H "Accept: application/json" \
  "http://localhost:8080/report?station=PSBC1"
```

JSON is intended for programmatic clients and voice integrations where
the caller should not have to parse the human-readable report.

### Concise / voice output

The service supports a concise report intended for voice assistants and
other clients that do not need the full diagnostic report.

The goal is to expose only the decision-oriented portions, particularly:

``` text
BOTTOM LINE
WIND
CURRENT
```

Use the concise option supported by the current `main.go` when
integrating a voice client.

## Voice and assistant integrations

The REST API is designed so an external assistant can call the service
and speak the returned sailing report.

Potential clients include:

-   Alexa custom skills
-   Siri/Shortcuts
-   ChatGPT actions or other HTTP integrations
-   Grok or other assistants that can invoke HTTPS endpoints

For voice clients, prefer JSON or concise output. The sailing service
should remain responsible for selecting and summarizing the marine data;
the voice client should primarily be responsible for invocation and
speech.

This avoids having each assistant independently reinterpret raw NOAA
observations.

## Render deployment

The service can be deployed from GitHub to Render.

Production service:

``` text
https://pittsburg-saildata.onrender.com/
```

Useful production routes:

``` text
/                         Mauri’s Sailing Outlook HTML dashboard
/report                   Plain-text report/API
/report?format=html       HTML dashboard explicitly
/report?format=json       JSON report
/health                   Health check
/assets/hero.jpg          Dashboard hero photograph
```

Render supplies the `PORT` environment variable.

Typical deployment workflow:

``` text
edit Go source
     |
     v
gofmt / go build
     |
     v
local report validation
     |
     v
git diff
     |
     v
git commit
     |
     v
git push
     |
     v
GitHub
     |
     v
Render automatic deployment
```

Before pushing current-selection changes, compare the local report with
a known NOAA/BASK reference where possible.

For HTML deployments, also verify that the hero image is committed:

``` bash
git status
git ls-files assets/hero.jpg
```

After Render deploys, check both presentation and API behavior:

``` text
https://pittsburg-saildata.onrender.com/
https://pittsburg-saildata.onrender.com/report
https://pittsburg-saildata.onrender.com/report?format=html
```

## Useful San Francisco Bay / Delta wind stations

These NDBC stations are useful candidates for building a Delta-to-Bay
sailing wind picture.

  -----------------------------------------------------------------------
  ID                      Area / description      Sailing use
  ----------------------- ----------------------- -----------------------
  **PSBC1**               Pittsburg / Suisun Bay  Primary Pittsburg/Delta
                                                  station

  **PCOC1**               Port Chicago            Suisun Bay / Carquinez
                                                  approach

  **MZXC1**               Martinez-Amorco Pier    Martinez / Carquinez
                                                  Strait

  **UPBC1**               Union Pacific Bridge,   Carquinez Strait
                          Martinez                comparison

  **DPXC1**               Davis Point             Western Carquinez / San
                                                  Pablo Bay

  **RCMC1**               Richmond                Richmond / San Pablo
                                                  Bay

  **PPXC1**               Point Potrero, Richmond Richmond / south San
                                                  Pablo Bay

  **TIBC1**               Tiburon                 Central Bay / Tiburon
                                                  area

  **FTPC1**               San Francisco           Central/southern San
                                                  Francisco Bay
  -----------------------------------------------------------------------

This list is only a convenient regional starting point. The service is
**not restricted to San Francisco Bay**; arbitrary valid NDBC wind
stations can be requested.

Use the NDBC station lookup page to find additional IDs:

``` text
https://www.ndbc.noaa.gov/to_station.shtml
```

### Station-comparison caution

Wind stations are not necessarily directly comparable. Anemometer
height, local exposure, instrumentation, and surrounding terrain or
structures can differ.

Similarly, the geographically nearest current-prediction station is not
necessarily the hydrodynamically best representation of a sailing area.
This is why current selection uses metadata and scoring rather than raw
distance alone, and why explicit overrides remain available.

## Current project status

Working capabilities include:

-   separate `main.go`, `wind.go`, and `currents.go`
-   PSBC1 default wind station
-   arbitrary active NDBC wind-station selection
-   current wind reports
-   historical wind reports using `-at`
-   ±30-minute historical wind observation window
-   NOAA CO-OPS current predictions
-   dynamic `currentpredictions` metadata discovery
-   in-memory NOAA current-station metadata cache
-   Haversine distance calculation
-   scored automatic current-station/bin selection
-   current prediction depth/bin reporting
-   explicit current-station/bin override
-   historical current predictions for `-at` dates
-   BASK/NOAA validation of PSBC1 → SFB1325 bin 9
-   Mauri's Sailing Outlook HTML branding
-   responsive HTML dashboard
-   sailing-photo hero image
-   full text-report detail included in HTML
-   text output
-   JSON output
-   concise/voice-oriented output
-   REST `/report`
-   REST `/health`
-   Render `PORT` handling
-   GitHub-based deployment

## Next steps

### 1. Validate automatic current selection beyond Pittsburg

PSBC1 → `SFB1325_9` is now a useful known-good test case.

The next priority is to test automatic selection for several other
sailing areas, particularly:

``` text
Port Chicago
Martinez / Carquinez Strait
Richmond / San Pablo Bay
Tiburon / Central Bay
San Francisco / Golden Gate
```

The scoring weights should be adjusted based on real hydrodynamic
suitability rather than tuned solely around Pittsburg.

### 2. Improve current-station scoring

The current heuristic is intentionally simple and transparent.

Possible improvements include:

-   current-axis alignment
-   waterway geometry
-   shoreline/channel context
-   station depth relative to sailing use
-   reference vs subordinate station relationships
-   maximum acceptable distance
-   confidence score for automatic selections

A low-confidence automatic match should be visible to the caller rather
than silently presented as authoritative.

### 3. Improve BOTTOM LINE current wording

Make the summary explicitly phase-aware.

For example:

``` text
At noon, current is predicted to be flooding strongly.
Max flood is around 12:47 PM at 1.9 kt.
Slack is around 3:54 PM, then the current turns to an ebb.
```

This is clearer than describing the entire sailing window with a single
current-strength adjective.

### 4. Extend the event window intelligently

The detailed event list currently follows the configured sailing window.
It may be useful to include the first significant transition just beyond
the window---for example, the next slack---when it materially improves
trip planning.

### 5. Add build/version identification

Expose a version or Git commit identifier in `/health` and JSON reports.

That makes it immediately obvious whether a local binary or Render
deployment is running the expected source revision.

### 6. Multi-station sailing corridor

A future endpoint could retrieve multiple wind stations concurrently:

``` text
/route?stations=PSBC1,PCOC1,MZXC1,DPXC1,RCMC1,PPXC1
```

This would show how wind conditions evolve from Pittsburg through
Carquinez Strait and San Pablo Bay.

### 7. Station metadata and confidence

Expose useful metadata in JSON, including:

-   wind station coordinates
-   selected current station
-   current-station distance
-   current bin/depth
-   prediction type
-   selection mode
-   automatic-selection score/confidence

### 8. Voice-client contracts

Define a stable, intentionally small JSON schema for Alexa/Siri/ChatGPT
clients rather than coupling voice integrations to the full internal
report structure.

For example:

``` json
{
  "location": "Pittsburg",
  "station": "PSBC1",
  "bottom_line": "...",
  "wind": "...",
  "current": "..."
}
```

## Development checks

Before committing:

``` bash
gofmt -w main.go wind.go currents.go
go build -o sailing-go .
./sailing-go
./sailing-go -at "2026-08-22T12:00"
```

For current-selection debugging:

``` bash
DEBUG_CURRENT_STATIONS=1 ./sailing-go
```

For the known-good Pittsburg current reference:

``` bash
./sailing-go \
  -station PSBC1 \
  -current-station SFB1325 \
  -current-bin 9
```

Then:

``` bash
git diff
git status
```

## Notes

This is a personal sailing utility, not a navigation system.

NOAA/NDBC observations can be delayed, missing, or affected by local
exposure. NOAA current predictions are predictions rather than direct
observations, and current conditions can differ because of weather,
runoff, river flow, and other factors.

Use appropriate marine forecasts, observations, charts, and seamanship
judgment when making sailing decisions.

## Author

Personal sailing-data project by Richard Mauri.
