# NOAA Sailing Conditions

A small Go service that combines NOAA/NDBC wind observations with NOAA
current predictions to produce concise, sailing-oriented reports for the
San Francisco Bay and Delta.

The project began as a command-line wind utility for Pittsburg and
Suisun Bay. It now provides combined wind/current reports through both a
CLI and a REST API deployed on Render.

## What it does

The service answers two basic sailing questions:

1.  **What is the wind doing now?**
2.  **What will the current be doing during the sailing window?**

Rather than presenting only raw NOAA numbers, the service generates a
concise sailing-oriented interpretation while keeping observed wind and
predicted current clearly distinguished.

The report puts the decision-oriented **BOTTOM LINE** near the top and
identifies the selected wind station by human-readable location when
NDBC metadata are available.

Example:

``` text
SAILING OUTLOOK — Pittsburg (Suisun Bay), CA (PSBC1)
=====================================================
Report time: Sat Aug 22, 2026 10:15:00 AM PDT

BOTTOM LINE
--------------------------------
Latest wind at 10:00 AM: WNW 11 kt, gusting 15 kt.
At 12:00 PM, current is predicted to be flooding.
Slack is around 2:13 PM, then the current turns to a weak ebb.
Overall, current should be relatively mild during the sailing window.

WIND
--------------------------------
Currently WNW 11.0 kt, gusting 15.0 kt.

CURRENT
--------------------------------
Using s06010 — Martinez-AMORCO Pier,
approximately 11 nmi from PSBC1.

The sailing window starts on a flood current.
Slack is around 2:13 PM, followed by an ebb.
The ebb peaks around 3:55 PM at about 0.4 kt.
```

## Features

### Wind

-   NOAA/NDBC real-time observations
-   Default wind station: **PSBC1**
-   Arbitrary active NDBC station selection
-   Latest wind direction, speed and gust
-   Latest 10 observations
-   12-hour statistics and trend
-   Previous-afternoon statistics
-   Historical reports
-   ±30-minute historical observation window
-   Conversion from meters/second to knots

### Currents

-   NOAA current predictions
-   Automatic current-station selection
-   No hard-coded wind-station list
-   Dynamically retrieves NDBC station coordinates
-   Finds the nearest NOAA current prediction station
-   Maximum flood
-   Maximum ebb
-   Slack water
-   Current direction
-   Prediction depth/bin
-   Human-readable sailing outlook
-   Configurable sailing window

### Service

-   Command-line interface
-   Plain-text reports
-   Full JSON output
-   Compact text output for phones and voice clients
-   Compact structured JSON for Alexa, GPT Actions and other assistants
-   Dynamic station/location heading when NDBC metadata are available
-   Decision-oriented `BOTTOM LINE` near the top of the report
-   REST `/report`
-   REST `/health`
-   Render deployment
-   GitHub → Render automatic deployment

------------------------------------------------------------------------

# Program structure

The program is deliberately divided into separate Go source files:

``` text
sailing-go/
├── main.go
├── wind.go
├── currents.go
└── go.mod
```

### `main.go`

Application orchestration:

-   command-line flags
-   REST server
-   `/report`
-   `/health`
-   full and compact JSON output
-   compact text output
-   combined sailing report
-   `BOTTOM LINE` generation
-   dynamic station/location heading
-   Render `PORT` handling

### `wind.go`

NDBC wind functionality:

-   NDBC `realtime2` retrieval
-   observation parsing
-   wind-speed conversion
-   latest observation
-   latest 10 observations
-   statistics
-   trends
-   historical reports
-   wind text formatting

### `currents.go`

NOAA current functionality:

-   NDBC station metadata lookup
-   wind-station coordinates
-   NOAA current-station metadata
-   nearest-current-station calculation
-   current predictions
-   flood/ebb/slack processing
-   human-readable current outlook

Keeping these components separate makes it easier to test and extend the
wind and current logic independently.

------------------------------------------------------------------------

# Data sources

## Wind --- NOAA/NDBC

Wind observations come from the NOAA National Data Buoy Center (NDBC).

NDBC station observations are retrieved from:

``` text
https://www.ndbc.noaa.gov/data/realtime2/<STATION>.txt
```

For example:

``` text
https://www.ndbc.noaa.gov/data/realtime2/PSBC1.txt
```

NDBC reports wind speed and gust in meters/second. The program converts
them to knots.

Wind direction is converted from degrees true into compass directions
such as:

``` text
W
WNW
NW
```

## NDBC station metadata

The program does **not** maintain a hard-coded list of wind stations.

Station location and metadata are obtained dynamically from:

``` text
https://www.ndbc.noaa.gov/activestations.xml
```

### Finding NDBC wind station IDs

NDBC maintains an official station-selection page:

``` text
https://www.ndbc.noaa.gov/to_station.shtml
```

This is a useful place to browse available station IDs, including NDBC,
NOAA NOS PORTS, and other observing networks.

NDBC also publishes station information and position data under:

``` text
https://www.ndbc.noaa.gov/data/stations/
```

That directory includes machine-readable station resources such as
`station_table.txt`.

The Bay and Delta station IDs listed later in this README are a
convenient sailing-oriented shortlist, not a hard-coded list required by
the application. The Go service discovers active wind-station metadata
dynamically.

This allows commands such as:

``` bash
./sailing-go -station PSBC1
```

and:

``` bash
./sailing-go -station RCMC1
```

without adding either station to the Go source.

## Current predictions --- NOAA Tides & Currents

Current predictions come from NOAA CO-OPS Tides & Currents.

The program retrieves NOAA current-station metadata and determines which
current prediction station is geographically closest to the selected
NDBC wind station.

Predictions use NOAA's `currents_predictions` product with:

``` text
interval=max_slack
```

This gives the significant current events rather than minute-by-minute
predictions:

``` text
maximum flood
slack
maximum ebb
slack
maximum flood
```

This compact representation is much more useful for a sailing report.

------------------------------------------------------------------------

# Building

Build the complete package rather than an individual `.go` file:

``` bash
go build -o sailing-go .
```

Then run:

``` bash
./sailing-go
```

Because the application now consists of multiple Go source files, do
**not** build with:

``` text
go build main.go
```

The `go build .` form compiles all Go files belonging to the package.

------------------------------------------------------------------------

# Command-line usage

## Pittsburg / Suisun Bay

PSBC1 is the default:

``` bash
./sailing-go
```

Equivalent to:

``` bash
./sailing-go -station PSBC1
```

## Richmond

``` bash
./sailing-go -station RCMC1
```

The program dynamically retrieves RCMC1's coordinates and selects the
nearest NOAA current prediction station.

## Change the sailing window

The default current window is:

``` text
12 PM – 5 PM
```

For an 11 AM through 6 PM window:

``` bash
./sailing-go -start 11 -end 18
```

These hours affect the current outlook.

## Historical wind report

``` bash
./sailing-go -at "2026-08-20T15:00"
```

Another wind station:

``` bash
./sailing-go \
  -station SANF1 \
  -at "2026-08-20T15:00"
```

Historical mode currently applies to the wind report.

------------------------------------------------------------------------

# REST server

Start locally:

``` bash
./sailing-go -server
```

Default local port:

``` text
8080
```

Health check:

``` bash
curl -sS "http://localhost:8080/health"
```

Combined PSBC1 sailing report:

``` bash
curl -sS "http://localhost:8080/report"
```

or:

``` bash
curl -sS "http://localhost:8080/report?station=PSBC1"
```

Richmond:

``` bash
curl -sS "http://localhost:8080/report?station=RCMC1"
```

Change the current window:

``` bash
curl -sS \
  "http://localhost:8080/report?station=PSBC1&start=11&end=18"
```

Historical wind:

``` bash
curl -sS \
  "http://localhost:8080/report?station=SANF1&at=2026-08-20T15:00"
```

## Output modes

The `/report` endpoint supports full text, compact text, full JSON and
compact JSON.

### Full text

``` bash
curl -sS "http://localhost:8080/report?station=PSBC1"
```

### Compact text

Compact mode keeps **BOTTOM LINE**, **WIND** and **CURRENT** while
omitting the longer observation/statistics detail:

``` bash
curl -sS "http://localhost:8080/report?station=PSBC1&compact=1"
```

### Full JSON

``` bash
curl -sS "http://localhost:8080/report?station=PSBC1&format=json"
```

The existing HTTP `Accept` header remains supported:

``` bash
curl -sS \
  -H "Accept: application/json" \
  "http://localhost:8080/report?station=PSBC1"
```

### Compact JSON

For Alexa, GPT Actions and other assistant integrations:

``` bash
curl -sS \
  "http://localhost:8080/report?station=PSBC1&format=json&compact=1"
```

Compact JSON keeps observed wind and predicted current explicitly
separate and includes the human-readable `bottom_line`.

------------------------------------------------------------------------

# Render deployment

The service is deployed from GitHub to Render.

Production service:

``` text
https://pittsburg-saildata.onrender.com
```

Render supplies the server's `PORT` environment variable automatically.

The Go server uses that value when present and falls back to port `8080`
locally.

## Production examples

Health:

``` bash
curl -sS \
  "https://pittsburg-saildata.onrender.com/health"
```

PSBC1 combined report:

``` bash
curl -sS \
  "https://pittsburg-saildata.onrender.com/report?station=PSBC1"
```

Richmond:

``` bash
curl -sS \
  "https://pittsburg-saildata.onrender.com/report?station=RCMC1"
```

JSON:

``` bash
curl -sS \
  "https://pittsburg-saildata.onrender.com/report?station=PSBC1&format=json"
```

Compact text:

``` bash
curl -sS \
  "https://pittsburg-saildata.onrender.com/report?station=PSBC1&compact=1"
```

Compact JSON for assistants:

``` bash
curl -sS \
  "https://pittsburg-saildata.onrender.com/report?station=PSBC1&format=json&compact=1"
```

------------------------------------------------------------------------

# Development and deployment workflow

After making changes:

``` bash
gofmt -w main.go wind.go currents.go
```

Build locally:

``` bash
go build -o sailing-go .
```

Test:

``` bash
./sailing-go
```

Check changes:

``` bash
git diff
```

Commit:

``` bash
git add main.go wind.go currents.go README.md
git commit -m "Integrate wind and current sailing reports"
```

Push:

``` bash
git push
```

The workflow is then:

``` text
edit
  |
  v
gofmt
  |
  v
go build .
  |
  v
local test
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

------------------------------------------------------------------------

# Useful San Francisco Bay / Delta wind stations

These NDBC stations are useful candidates for building a Delta-to-Bay
wind picture.

For the authoritative and current NDBC station list, use:

``` text
https://www.ndbc.noaa.gov/to_station.shtml
```

The table below is a convenient sailing-oriented shortlist rather than a
complete or authoritative station catalog.

  -----------------------------------------------------------------------
  ID                      Area / description      Sailing use
  ----------------------- ----------------------- -----------------------
  **PSBC1**               Pittsburg / Suisun Bay  Primary Delta station

  **PCOC1**               Port Chicago            Suisun Bay / Carquinez
                                                  approach

  **MZXC1**               Martinez area           Martinez / Carquinez
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

The application no longer requires these stations to be encoded in the
source. They are listed here simply as useful sailing references.

## Comparing wind stations

Wind stations should not automatically be treated as equivalent.

Important differences include:

-   anemometer height
-   shoreline exposure
-   surrounding structures
-   terrain
-   instrument location
-   reporting interval

A future corridor report should include enough station metadata to make
these differences clear.

------------------------------------------------------------------------

# Current-station selection

A key design goal is avoiding another hard-coded station table.

Given:

``` bash
./sailing-go -station RCMC1
```

the current implementation conceptually performs:

``` text
RCMC1
  |
  v
NDBC active-station metadata
  |
  v
latitude / longitude
  |
  v
NOAA current-station metadata
  |
  v
calculate geographic distances
  |
  v
nearest current prediction station
  |
  v
NOAA max/slack predictions
  |
  v
human sailing summary
```

The selected current station and its distance from the wind station are
included in the report.

This makes the wind/current relationship visible rather than silently
assuming that two stations represent the same location.

------------------------------------------------------------------------

# Current project status

Working functionality now includes:

-   PSBC1 as the default wind station
-   arbitrary active NDBC wind stations
-   dynamically retrieved wind-station metadata
-   dynamic report heading with station location/name and station ID
-   latest wind observation with observation time
-   latest 10 wind observations
-   12-hour wind statistics and trend
-   historical wind reports
-   automatic NOAA current-station selection
-   geographic distance calculation between wind and current stations
-   flood/ebb/slack predictions
-   human-readable current summaries
-   configurable current sailing window
-   combined wind/current report
-   `BOTTOM LINE` near the top of the report
-   explicit distinction between observed wind and predicted current
-   full text output
-   compact text output with `compact=1`
-   full JSON output with `format=json` or the HTTP `Accept` header
-   compact JSON with `format=json&compact=1`
-   REST `/report`
-   REST `/health`
-   Render deployment
-   GitHub-based automatic deployment

------------------------------------------------------------------------

# Possible next steps

## Geographic portability testing

Test the service outside the San Francisco Bay/Delta development area
using other active NDBC wind stations and NOAA current prediction
stations.

The goal is to determine where the current automatic station-selection
logic works well and where regional hydrodynamics require better station
matching.

## 1. Alexa and other voice assistants

Use compact JSON as the common assistant interface:

``` text
/report?station=PSBC1&format=json&compact=1
```

Alexa can use a thin custom-skill/Lambda adapter. Siri, ChatGPT, Grok
and other clients should likewise keep assistant-specific code thin and
leave sailing logic in this Go service.

## 2. ChatGPT GPT Action

Define `/report` with an OpenAPI schema so a custom GPT can reliably
call the service instead of depending on conversation history.

Example requests:

``` text
Read my Pittsburg sailing report.
Read my Richmond sailing report.
```

Assistant instructions should require a live API call for current
conditions and should not turn observed wind into a future forecast.

## 3. Place-name aliases

Allow human-friendly names to map to station IDs, for example:

``` text
Pittsburg -> PSBC1
Richmond  -> RCMC1
```

Initially this can live in assistant configuration; later the API could
expose station lookup or aliases.

## 4. Multi-station wind corridor

Possible endpoint:

``` text
/route?stations=PSBC1,PCOC1,MZXC1,DPXC1,RCMC1,PPXC1
```

This could show how wind changes from Pittsburg through Carquinez
Strait, San Pablo Bay and Richmond.

## 5. Evaluate NDBC station discovery sources

The application currently uses:

``` text
https://www.ndbc.noaa.gov/activestations.xml
```

NDBC also publishes:

``` text
https://www.ndbc.noaa.gov/data/stations/station_table.txt
```

Compare these sources to determine whether `station_table.txt` provides
better station names, positions, ownership/network or station-type
information.

Preserve the design goal: **do not hard-code a list of supported wind
stations.**

## 6. Better station metadata

Include or expose:

-   station name
-   latitude/longitude
-   anemometer height
-   observation age
-   exposure/instrument notes

## 7. Smarter current-station selection

Geographic proximity is useful, but the nearest station is not
necessarily the best hydrodynamic match. Future selection could consider
waterway, channel, barriers, station location, known sailing areas and
distance.

The automatically selected current station should remain visible in the
report.

## 8. Add a real wind forecast source

The current wind report is observational. A future forecast source
should be clearly separated:

``` text
OBSERVED WIND
FORECAST WIND
PREDICTED CURRENT
```

Only a real forecast source should support statements such as "wind is
expected to build this afternoon."

## 9. Caching and concurrent retrieval

A short cache could reduce NOAA traffic, latency and duplicate requests.
Concurrent retrieval will also help multi-station corridor reports and
voice-assistant response time.

  ------------------------------------------------------------
  Solid WNW breeze with relatively mild afternoon current. The
  flood switches to ebb around 2:15 PM. \`\`\`

  This should combine wind trend and current phase into one
  concise sailing-oriented assessment.

  \## 2. Multi-station wind corridor

  Retrieve several stations concurrently:

  `text PSBC1 PCOC1 MZXC1 DPXC1 RCMC1 PPXC1`

  Possible endpoint:

  `text /route?stations=PSBC1,PCOC1,MZXC1,DPXC1,RCMC1,PPXC1`

  This would show how the afternoon wind develops from
  Pittsburg through Carquinez Strait, San Pablo Bay and
  Richmond.

  \## 3. Better station metadata

  Include:

  \- station name - latitude/longitude - anemometer height -
  observation age - exposure/instrument notes

  This is especially important when comparing wind speeds from
  different stations.

  \## 4. Smarter current-station selection

  Geographic proximity is a useful first approximation, but
  the nearest station is not necessarily the best station
  hydrodynamically.

  Future selection could consider:

  \- waterway - channel - geographic barriers - station
  name/location - known sailing areas - distance

  The automatic nearest-station selection should therefore
  always remain visible in the report.

  \## 5. Mobile-friendly report

  A compact endpoint could provide only the information useful
  while rigging or sailing:

  `text /report?station=PSBC1&compact=1`

  For example:

  \`\`\`text PSBC1

  WIND WNW 14 kt Gust 17 kt Steady

  CURRENT Flood weakening Slack 2:13 PM Weak ebb after slack
  Max ebb 0.4 kt at 3:55 PM \`\`\`

  This would be particularly useful from an iPhone.

  \## 6. Caching and concurrent retrieval

  The combined report makes several NOAA requests.

  A short cache could reduce:

  \- NOAA traffic - response latency - duplicate requests

  Concurrent retrieval would also become useful when
  implementing the multi-station corridor report.
  ------------------------------------------------------------

# Safety and limitations

This is a personal sailing utility, **not a navigation system**.

Wind observations can be:

-   delayed
-   missing
-   locally influenced
-   unrepresentative of conditions elsewhere

Current data are **predictions**, not real-time measurements.

The automatically selected current station may also be geographically
close without being the most representative station for a particular
sailing area.

Use appropriate marine forecasts, observations, charts, local knowledge
and seamanship when making sailing decisions.

------------------------------------------------------------------------

# Author

Personal sailing-data project by Richard Mauri.
