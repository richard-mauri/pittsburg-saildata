# Delta Sailing Data

A small Go service that combines NOAA/NDBC wind observations with NOAA current predictions to produce concise, sailing-oriented reports for the San Francisco Bay and Delta.

The project began as a command-line wind utility for Pittsburg and Suisun Bay. It now provides combined wind/current reports through both a CLI and a REST API deployed on Render.

## What it does

The service answers two basic sailing questions:

1. **What is the wind doing now?**
2. **What will the current be doing during the sailing window?**

Rather than presenting only raw NOAA numbers, the service also generates a short human-readable current outlook.

Example:

```text
DELTA SAILING OUTLOOK
================================
Report time: Sat Aug 22, 2026 7:44:58 AM PDT

WIND
--------------------------------
Currently WNW 14.0 kt, gusting 17.1 kt.
Recent observations have been 12–14 kt,
with gusts around 16–18 kt.

CURRENT
--------------------------------
Using s06010 — Martinez-AMORCO Pier,
approximately 11 nmi from PSBC1.

The sailing window starts on a flood current.
Slack is around 2:13 PM, followed by an ebb.
The ebb peaks around 3:55 PM at 0.4 kt, which is weak.
The current then weakens toward another slack around 5:28 PM.
Overall, currents should be relatively mild during the sailing window.
```

## Features

### Wind

- NOAA/NDBC real-time observations
- Default wind station: **PSBC1**
- Arbitrary active NDBC station selection
- Latest wind direction, speed and gust
- Latest 10 observations
- 12-hour statistics and trend
- Previous-afternoon statistics
- Historical reports
- ±30-minute historical observation window
- Conversion from meters/second to knots

### Currents

- NOAA current predictions
- Automatic current-station selection
- No hard-coded wind-station list
- Dynamically retrieves NDBC station coordinates
- Finds the nearest NOAA current prediction station
- Maximum flood
- Maximum ebb
- Slack water
- Current direction
- Prediction depth/bin
- Human-readable sailing outlook
- Configurable sailing window

### Service

- Command-line interface
- Plain-text reports
- JSON output
- REST `/report`
- REST `/health`
- Render deployment
- GitHub → Render automatic deployment

---

# Program structure

The program is deliberately divided into separate Go source files:

```text
sailing-go/
├── main.go
├── wind.go
├── currents.go
└── go.mod
```

### `main.go`

Application orchestration:

- command-line flags
- REST server
- `/report`
- `/health`
- JSON output
- combined sailing report
- Render `PORT` handling

### `wind.go`

NDBC wind functionality:

- NDBC `realtime2` retrieval
- observation parsing
- wind-speed conversion
- latest observation
- latest 10 observations
- statistics
- trends
- historical reports
- wind text formatting

### `currents.go`

NOAA current functionality:

- NDBC station metadata lookup
- wind-station coordinates
- NOAA current-station metadata
- nearest-current-station calculation
- current predictions
- flood/ebb/slack processing
- human-readable current outlook

Keeping these components separate makes it easier to test and extend the wind and current logic independently.

---

# Data sources

## Wind — NOAA/NDBC

Wind observations come from the NOAA National Data Buoy Center (NDBC).

NDBC station observations are retrieved from:

```text
https://www.ndbc.noaa.gov/data/realtime2/<STATION>.txt
```

For example:

```text
https://www.ndbc.noaa.gov/data/realtime2/PSBC1.txt
```

NDBC reports wind speed and gust in meters/second. The program converts them to knots.

Wind direction is converted from degrees true into compass directions such as:

```text
W
WNW
NW
```

## NDBC station metadata

The program does **not** maintain a hard-coded list of wind stations.

Station location and metadata are obtained dynamically from:

```text
https://www.ndbc.noaa.gov/activestations.xml
```

This allows commands such as:

```bash
./sailing-go -station PSBC1
```

and:

```bash
./sailing-go -station RCMC1
```

without adding either station to the Go source.

## Current predictions — NOAA Tides & Currents

Current predictions come from NOAA CO-OPS Tides & Currents.

The program retrieves NOAA current-station metadata and determines which current prediction station is geographically closest to the selected NDBC wind station.

Predictions use NOAA's `currents_predictions` product with:

```text
interval=max_slack
```

This gives the significant current events rather than minute-by-minute predictions:

```text
maximum flood
slack
maximum ebb
slack
maximum flood
```

This compact representation is much more useful for a sailing report.

---

# Building

Build the complete package rather than an individual `.go` file:

```bash
go build -o sailing-go .
```

Then run:

```bash
./sailing-go
```

Because the application now consists of multiple Go source files, do **not** build with:

```text
go build main.go
```

The `go build .` form compiles all Go files belonging to the package.

---

# Command-line usage

## Pittsburg / Suisun Bay

PSBC1 is the default:

```bash
./sailing-go
```

Equivalent to:

```bash
./sailing-go -station PSBC1
```

## Richmond

```bash
./sailing-go -station RCMC1
```

The program dynamically retrieves RCMC1's coordinates and selects the nearest NOAA current prediction station.

## Change the sailing window

The default current window is:

```text
12 PM – 5 PM
```

For an 11 AM through 6 PM window:

```bash
./sailing-go -start 11 -end 18
```

These hours affect the current outlook.

## Historical wind report

```bash
./sailing-go -at "2026-08-20T15:00"
```

Another wind station:

```bash
./sailing-go \
  -station SANF1 \
  -at "2026-08-20T15:00"
```

Historical mode currently applies to the wind report.

---

# REST server

Start locally:

```bash
./sailing-go -server
```

Default local port:

```text
8080
```

Health check:

```bash
curl -sS "http://localhost:8080/health"
```

Combined PSBC1 sailing report:

```bash
curl -sS "http://localhost:8080/report"
```

or:

```bash
curl -sS "http://localhost:8080/report?station=PSBC1"
```

Richmond:

```bash
curl -sS "http://localhost:8080/report?station=RCMC1"
```

Change the current window:

```bash
curl -sS \
  "http://localhost:8080/report?station=PSBC1&start=11&end=18"
```

Historical wind:

```bash
curl -sS \
  "http://localhost:8080/report?station=SANF1&at=2026-08-20T15:00"
```

## JSON

Request JSON using the HTTP `Accept` header:

```bash
curl -sS \
  -H "Accept: application/json" \
  "http://localhost:8080/report?station=PSBC1"
```

The JSON response keeps wind and current information structured so it can eventually be consumed by other applications.

---

# Render deployment

The service is deployed from GitHub to Render.

Production service:

```text
https://pittsburg-saildata.onrender.com
```

Render supplies the server's `PORT` environment variable automatically.

The Go server uses that value when present and falls back to port `8080` locally.

## Production examples

Health:

```bash
curl -sS \
  "https://pittsburg-saildata.onrender.com/health"
```

PSBC1 combined report:

```bash
curl -sS \
  "https://pittsburg-saildata.onrender.com/report?station=PSBC1"
```

Richmond:

```bash
curl -sS \
  "https://pittsburg-saildata.onrender.com/report?station=RCMC1"
```

JSON:

```bash
curl -sS \
  -H "Accept: application/json" \
  "https://pittsburg-saildata.onrender.com/report?station=PSBC1"
```

---

# Development and deployment workflow

After making changes:

```bash
gofmt -w main.go wind.go currents.go
```

Build locally:

```bash
go build -o sailing-go .
```

Test:

```bash
./sailing-go
```

Check changes:

```bash
git diff
```

Commit:

```bash
git add main.go wind.go currents.go README.md
git commit -m "Integrate wind and current sailing reports"
```

Push:

```bash
git push
```

The workflow is then:

```text
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

---

# Useful San Francisco Bay / Delta wind stations

These NDBC stations are useful candidates for building a Delta-to-Bay wind picture.

| ID | Area / description | Sailing use |
|---|---|---|
| **PSBC1** | Pittsburg / Suisun Bay | Primary Delta station |
| **PCOC1** | Port Chicago | Suisun Bay / Carquinez approach |
| **MZXC1** | Martinez area | Martinez / Carquinez Strait |
| **UPBC1** | Union Pacific Bridge, Martinez | Carquinez Strait comparison |
| **DPXC1** | Davis Point | Western Carquinez / San Pablo Bay |
| **RCMC1** | Richmond | Richmond / San Pablo Bay |
| **PPXC1** | Point Potrero, Richmond | Richmond / south San Pablo Bay |
| **TIBC1** | Tiburon | Central Bay / Tiburon area |
| **FTPC1** | San Francisco | Central/southern San Francisco Bay |

The application no longer requires these stations to be encoded in the source. They are listed here simply as useful sailing references.

## Comparing wind stations

Wind stations should not automatically be treated as equivalent.

Important differences include:

- anemometer height
- shoreline exposure
- surrounding structures
- terrain
- instrument location
- reporting interval

A future corridor report should include enough station metadata to make these differences clear.

---

# Current-station selection

A key design goal is avoiding another hard-coded station table.

Given:

```bash
./sailing-go -station RCMC1
```

the current implementation conceptually performs:

```text
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

The selected current station and its distance from the wind station are included in the report.

This makes the wind/current relationship visible rather than silently assuming that two stations represent the same location.

---

# Current project status

Working functionality now includes:

- PSBC1 as the default wind station
- arbitrary active NDBC wind stations
- dynamically retrieved wind-station metadata
- latest wind observation
- latest 10 wind observations
- 12-hour wind statistics
- wind trend
- historical wind reports
- automatic NOAA current-station selection
- geographic distance calculation
- flood/ebb/slack predictions
- human-readable current summaries
- configurable current sailing window
- combined wind/current report
- text output
- JSON output
- REST `/report`
- REST `/health`
- Render deployment
- GitHub-based automatic deployment

---

# Possible next steps

## 1. Improve the combined sailing summary

Add a final section such as:

```text
BOTTOM LINE
--------------------------------
Solid WNW breeze with relatively mild afternoon current.
The flood switches to ebb around 2:15 PM.
```

This should combine wind trend and current phase into one concise sailing-oriented assessment.

## 2. Multi-station wind corridor

Retrieve several stations concurrently:

```text
PSBC1
PCOC1
MZXC1
DPXC1
RCMC1
PPXC1
```

Possible endpoint:

```text
/route?stations=PSBC1,PCOC1,MZXC1,DPXC1,RCMC1,PPXC1
```

This would show how the afternoon wind develops from Pittsburg through Carquinez Strait, San Pablo Bay and Richmond.

## 3. Better station metadata

Include:

- station name
- latitude/longitude
- anemometer height
- observation age
- exposure/instrument notes

This is especially important when comparing wind speeds from different stations.

## 4. Smarter current-station selection

Geographic proximity is a useful first approximation, but the nearest station is not necessarily the best station hydrodynamically.

Future selection could consider:

- waterway
- channel
- geographic barriers
- station name/location
- known sailing areas
- distance

The automatic nearest-station selection should therefore always remain visible in the report.

## 5. Mobile-friendly report

A compact endpoint could provide only the information useful while rigging or sailing:

```text
/report?station=PSBC1&compact=1
```

For example:

```text
PSBC1

WIND
WNW 14 kt
Gust 17 kt
Steady

CURRENT
Flood weakening
Slack 2:13 PM
Weak ebb after slack
Max ebb 0.4 kt at 3:55 PM
```

This would be particularly useful from an iPhone.

## 6. Caching and concurrent retrieval

The combined report makes several NOAA requests.

A short cache could reduce:

- NOAA traffic
- response latency
- duplicate requests

Concurrent retrieval would also become useful when implementing the multi-station corridor report.

---

# Safety and limitations

This is a personal sailing utility, **not a navigation system**.

Wind observations can be:

- delayed
- missing
- locally influenced
- unrepresentative of conditions elsewhere

Current data are **predictions**, not real-time measurements.

The automatically selected current station may also be geographically close without being the most representative station for a particular sailing area.

Use appropriate marine forecasts, observations, charts, local knowledge and seamanship when making sailing decisions.

---

# Author

Personal sailing-data project by Richard Mauri.
