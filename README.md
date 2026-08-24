# Mauri's Sailing Outlook

A Go service that combines NOAA/NDBC wind observations with NOAA CO-OPS
current predictions to produce a sailing-oriented outlook for the San
Francisco Bay and Delta.

The project began as a command-line utility centered on Pittsburg/Suisun
Bay. It now includes a branded HTML report, automatic wind-station
selection from latitude/longitude, nearby-station browsing, current
comparisons, and an interactive map for choosing where you plan to sail.

## Highlights

-   NOAA/NDBC real-time wind observations
-   NOAA CO-OPS current predictions
-   Default wind station: **PSBC1 --- Pittsburg (Suisun Bay), CA**
-   Interactive Bay/Delta map chooser
-   Click a sailing location instead of knowing a station ID
-   Automatic nearest-usable wind-station selection from decimal `lat` /
    `lon`
-   Concurrent probing of nearby meteorological stations
-   Nearby wind-station browser with clickable station choices
-   `AUTO` and `SELECTED` station states when comparing stations
-   Current-event timeline and smooth current graph
-   Relative comparison of today's ebb/flood maxima
-   Comparison with recent current cycles
-   Branded HTML output: **Mauri's Sailing Outlook**
-   Plain-text and JSON API output
-   Historical wind reports
-   Compact output for assistants and integrations
-   Render deployment

## Try the web interface

Production service:

``` text
https://pittsburg-saildata.onrender.com/report?format=html
```

The HTML report includes a **Choose Sailing Location** map.

1.  Click approximately where you plan to sail.
2.  The map shows the selected latitude/longitude.
3.  Click **Generate Sailing Outlook**.
4.  The service finds a nearby NDBC station with usable recent wind and
    generates the report.
5.  Use **Nearby Wind Stations** to compare or manually select another
    station.

The map also shows the selected sailing location, wind observation
station, and current prediction station when those locations are
available.

The map UI uses Leaflet and OpenStreetMap tiles in the browser. The Go
server does not host map tiles.

## How location-based wind selection works

A request such as:

``` text
/report?lat=38.042&lon=-121.838&format=html
```

uses decimal latitude/longitude as the sailing location.

The service loads active NDBC station metadata, considers nearby
meteorological-capable stations, sorts them by distance, probes them
concurrently for usable recent wind, and selects the nearest usable
station. The resulting wind reference is then used to build the sailing
outlook and select current data.

Wind-station diagnostics can be exposed with `debug_wind=1`:

``` bash
curl -sS "http://localhost:8080/report?lat=37.9105&lon=-122.3602&debug_wind=1"
```

The automatic search is bounded so a difficult inland coordinate does
not make the report wait through a long sequence of failed station
requests.

## Nearby Wind Stations

HTML reports include a clickable **Nearby Wind Stations** table.

When a report started with a `lat` / `lon`, that sailing location
remains the geographic anchor while you compare stations.

When browsing without a supplied location, the selected station acts as
the browsing anchor. The default page therefore starts at PSBC1 and lets
you explore neighboring stations across the Bay and Delta.

`AUTO` identifies the station the automatic resolver prefers. `SELECTED`
identifies the station currently driving the report.

## Data sources

### Wind

Wind observations come from the NOAA National Data Buoy Center (NDBC),
primarily its `realtime2` station data. Wind speed and gust are
converted to knots.

### Current

Current predictions come from NOAA CO-OPS current-prediction data.

The report intentionally calls these **current** predictions rather than
tide data. It reports current events and speeds, including maximum ebb,
maximum flood, and slack-water transitions when available.

Current strength is described primarily through measured speeds and
relative comparisons rather than potentially misleading absolute
adjectives.

## Build

``` bash
go build -o sailing-go .
```

Run the default command-line report:

``` bash
./sailing-go
```

Start the REST server:

``` bash
./sailing-go -server
```

Then open:

``` text
http://127.0.0.1:8080/report?format=html
```

## REST API examples

Default text report:

``` bash
curl -sS "http://localhost:8080/report"
```

Explicit wind station:

``` bash
curl -sS "http://localhost:8080/report?station=RCMC1"
```

Automatic selection from a sailing location:

``` bash
curl -sS "http://localhost:8080/report?lat=37.9105&lon=-122.3602"
```

HTML:

``` text
http://127.0.0.1:8080/report?lat=37.9105&lon=-122.3602&format=html
```

Diagnostics:

``` bash
curl -sS "http://localhost:8080/report?lat=37.9105&lon=-122.3602&debug_wind=1"
```

Current-station override:

``` bash
curl -sS "http://localhost:8080/report?station=PSBC1&current_station=SFB1325&bin=9"
```

Change sailing window:

``` bash
curl -sS "http://localhost:8080/report?station=PSBC1&start=11&end=18"
```

Full JSON:

``` bash
curl -sS "http://localhost:8080/report?station=PSBC1&format=json"
```

Compact text:

``` bash
curl -sS "http://localhost:8080/report?station=PSBC1&compact=1"
```

Compact JSON:

``` bash
curl -sS "http://localhost:8080/report?station=PSBC1&format=json&compact=1"
```

`Accept: application/json` is also supported.

## Important query parameters

  -----------------------------------------------------------------------
  Parameter                           Purpose
  ----------------------------------- -----------------------------------
  `format=html`                       Branded interactive HTML report

  `format=json`                       JSON report

  `station=ID`                        Explicit NDBC wind-station override

  `lat=...&lon=...`                   Decimal sailing location for
                                      automatic station selection

  `debug_wind=1`                      Wind-station candidate diagnostics

  `current_station=ID`                NOAA current-prediction station
                                      override

  `bin=N`                             Current-prediction bin override

  `start=HOUR`                        Sailing-window start hour

  `end=HOUR`                          Sailing-window end hour

  `at=DATETIME`                       Historical report time

  `compact=1`                         Compact output
  -----------------------------------------------------------------------

Latitude and longitude are intentionally decimal degrees.

## HTML report

The HTML version currently includes:

-   branded sailing photograph and social-preview metadata
-   Bottom Line summary
-   wind direction, speed, gust, and observation time
-   selected wind-station context
-   interactive sailing-location map
-   nearby clickable wind stations
-   current prediction station
-   current events with predicted speeds
-   relative ebb/flood comparisons
-   smooth current-speed graph
-   full text report for reference

## Deployment

The service is deployed from GitHub to Render. Render supplies the
`PORT` environment variable, which the server honors.

Production:

``` text
https://pittsburg-saildata.onrender.com
```

Typical workflow:

``` text
edit
  ↓
gofmt
  ↓
go build
  ↓
git diff
  ↓
git commit
  ↓
git push
  ↓
GitHub
  ↓
Render automatic deployment
```

## Notes

This is a sailing-planning utility, **not a navigation system**.
Observations and predictions can be delayed, missing, geographically
unrepresentative, or affected by local station exposure. Use appropriate
marine forecasts, observations, charts, and seamanship judgment when
making sailing decisions.

Map locations are a convenient way to choose the area of interest;
clicking a point does not imply that the point is navigable water or
safe for sailing.

## Author

Personal sailing-data project by Richard Mauri.
