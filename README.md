
# Pittsburg Delta Sailing Data

A small Go service for retrieving NOAA/NDBC real-time buoy observations and turning them into concise sailing-oriented wind reports.

The project started as a command-line tool for checking conditions around Pittsburg and the Delta, and now runs as a REST API on Render.

## Features

- NOAA/NDBC real-time wind observations
- Default station: **PSBC1**
- Arbitrary NDBC station selection
- Current wind/gust and 12-hour statistics
- Historical reports using a requested local date/time
- ±30-minute historical observation window
- Plain-text output
- JSON output for programmatic use
- REST API with `/report` and `/health`
- GitHub → Render deployment

## Data source

Wind observations come from the [NOAA National Data Buoy Center (NDBC)](https://www.ndbc.noaa.gov/).

The program reads NDBC `realtime2` station files:

```text
https://www.ndbc.noaa.gov/data/realtime2/<STATION>.txt
```

NDBC reports wind direction in degrees true and wind speed/gust in meters/second. The program converts wind speeds to knots.

## Command-line usage

Build:

```bash
go build pittsburg-saildata.go
```

Current report using PSBC1:

```bash
./pittsburg-saildata
```

Use another station:

```bash
./pittsburg-saildata -station SANF1
```

Historical report:

```bash
./pittsburg-saildata -at "2026-08-20T15:00"
```

Historical report for another station:

```bash
./pittsburg-saildata -station SANF1 -at "2026-08-20T15:00"
```

Start the REST server:

```bash
./pittsburg-saildata -server
```

Default local port: `8080`.

## REST API

Health check:

```bash
curl -sS http://localhost:8080/health
```

Current report:

```bash
curl -sS "http://localhost:8080/report"
```

Current report for another station:

```bash
curl -sS "http://localhost:8080/report?station=SANF1"
```

Historical report:

```bash
curl -sS "http://localhost:8080/report?at=2026-08-20T15:00"
```

Historical report for another station:

```bash
curl -sS "http://localhost:8080/report?station=SANF1&at=2026-08-20T15:00"
```

Request JSON:

```bash
curl -sS   -H "Accept: application/json"   "http://localhost:8080/report?station=PSBC1"
```

## Render deployment

The service is deployed from GitHub to Render.

Render supplies the `PORT` environment variable; the server uses it when running on Render.

Production service:

```text
https://pittsburg-saildata.onrender.com
```

Health check:

```bash
curl -sS https://pittsburg-saildata.onrender.com/health
```

Current PSBC1 report:

```bash
curl -sS "https://pittsburg-saildata.onrender.com/report?station=PSBC1"
```

Historical report:

```bash
curl -sS "https://pittsburg-saildata.onrender.com/report?station=SANF1&at=2026-08-20T15:00"
```

### Deployment workflow

```text
edit Go source
     |
     v
go build
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

## Useful San Francisco Bay / Delta NDBC stations

These are useful candidates for a Delta-to-Bay sailing wind picture.

| ID | Area / description | Sailing use |
|---|---|---|
| **PSBC1** | Pittsburg / Suisun Bay | Primary Delta station |
| **PCOC1** | Port Chicago | Suisun Bay / Carquinez approach |
| **MZXC1** | Martinez-Amorco Pier | Martinez / Carquinez Strait |
| **UPBC1** | Union Pacific Bridge, Martinez | Carquinez Strait comparison |
| **DPXC1** | Davis Point | Western Carquinez / San Pablo Bay |
| **RCMC1** | Richmond | Richmond / San Pablo Bay |
| **PPXC1** | Point Potrero, Richmond | Richmond / south San Pablo Bay |
| **TIBC1** | Tiburon | Central Bay / Tiburon area |
| **FTPC1** | San Francisco | Central/southern San Francisco Bay |

NDBC regional observations show PSBC1, PCOC1, MZXC1, UPBC1 and DPXC1 in the Pittsburg/Carquinez portion of the system, with RCMC1 and PPXC1 farther west toward Richmond. TIBC1 and FTPC1 extend the comparison into the central/southern Bay.

**Important:** stations are not necessarily directly comparable. Anemometer height and exposure differ. In particular, UPBC1 should not automatically be treated as equivalent to a conventional low-height marine anemometer.

## Current project status

The basic service is working and deployed on Render.

Current capabilities:

- PSBC1 default station
- Arbitrary valid NDBC station selection
- Current reports
- Historical reports with `at=`
- ±30-minute historical observation window
- Text output
- JSON output
- REST `/report`
- REST `/health`
- Render `PORT` handling
- GitHub-based deployment

## Possible next steps

### 1. Show the latest wind changes

Add a compact section showing the most recent observations:

```text
RECENT WIND
--------------------------------
 6:30 AM  WNW  9.9 kt  gust 15.9
 6:24 AM  WNW 11.1 kt  gust 15.9
 6:18 AM  WNW  9.9 kt  gust 15.9
 6:12 AM  WNW 11.1 kt  gust 17.1
 ...
```

This makes short-term acceleration, easing, and gustiness immediately visible.

### 2. Add a multi-station corridor report

A future endpoint could retrieve several stations concurrently:

```text
/route?stations=PSBC1,PCOC1,MZXC1,DPXC1,RCMC1,PPXC1
```

Possible output:

```text
PSBC1  Pittsburg       WNW  9.9 kt  gust 15.9
PCOC1  Port Chicago    WNW 11.2 kt  gust 16.8
MZXC1  Martinez        WNW 12.1 kt  gust 17.5
DPXC1  Davis Point     WNW 13.4 kt  gust 19.0
RCMC1  Richmond        WNW 15.2 kt  gust 21.1
PPXC1  Point Potrero   WNW 14.7 kt  gust 20.0
```

The goal would be to see how wind changes from Pittsburg through the Carquinez Strait and into San Pablo Bay.

### 3. Add station metadata

Maintain station metadata including:

- station ID
- human-readable name
- latitude/longitude
- area
- anemometer height
- instrumentation/exposure notes

### 4. Add tide/current information

Combine wind with NOAA tide/current predictions to produce a more complete sailing report:

```text
WIND
TIDE
CURRENT
TREND
```

This is particularly useful in the Delta and Carquinez Strait.

### 5. Add a mobile-friendly report

The REST service already makes the data accessible from an iPhone. A compact option could be:

```text
/report?station=PSBC1&compact=1
```

### 6. Add caching and concurrent retrieval

For multi-station reports, fetch stations concurrently and consider a short cache period to reduce latency and unnecessary repeated NDBC requests.

## Notes

This is a personal sailing utility, not a navigation system. NDBC observations can be delayed, missing, or affected by local exposure. Use appropriate marine forecasts, observations, and seamanship judgment when making sailing decisions.

## Author

Personal sailing-data project by Richard Mauri.
