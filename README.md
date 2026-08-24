# Mauri's Bay & Delta Conditions

A Go service for exploring **wind and predicted currents** around the
San Francisco Bay and Delta. It is intended for sailors, paddlers,
kayakers, rowers, and other people planning time on the water.

## Try it

Welcome page:

``` text
https://pittsburg-saildata.onrender.com/welcome
```

Conditions page:

``` text
https://pittsburg-saildata.onrender.com/report?format=html
```

The easiest workflow is: click where you expect to be on the map, click
**Show Conditions**, read the Bottom Line, then explore the wind-station
choices and current details if useful.

## Daylight-aware conditions window

The default current analysis now covers **local sunrise through local
sunset** for the report date.

Sunrise and sunset are calculated astronomically by the Go service using
the location of the selected wind-reference station. Because that
station is chosen near the requested map point, this provides a local
daylight window without another external API.

The daylight window automatically changes with:

-   the season and changing day length;
-   the report date;
-   the geographic area being examined;
-   historical dates supplied with `at`.

The HTML Current card identifies the default as:

``` text
Daylight window · sunrise to sunset
6:31 AM → 7:49 PM
```

(the actual times depend on date and location).

### Custom time windows

Existing `start` and `end` overrides are preserved. Supplying them
replaces the daylight default:

``` text
/report?lat=38.042&lon=-121.838&start=7&end=10&format=html
```

The command-line `-start` and `-end` flags work the same way. If both
are omitted, sunrise-to-sunset is used.

## Wind

Wind observations come from NOAA's National Data Buoy Center (NDBC).

For a map-selected latitude/longitude, the service caches active
meteorological station metadata, sorts candidates by distance, probes
nearby stations concurrently, and chooses the nearest station with
recent usable wind.

The **Nearby Wind Stations** table lets you explore alternatives. `AUTO`
is the automatic choice and `SELECTED` is the station currently driving
the report.

With no location supplied, the service retains **PSBC1 --- Pittsburg
(Suisun Bay), CA** as the default browsing anchor.

## Current

Current predictions come from NOAA CO-OPS.

The report intentionally describes **current**, not tide height. It
includes ebb, flood, slack, maximum predicted speeds, relative
comparisons between current maxima, recent-cycle context, and a smooth
current graph.

The current graph shades the active **conditions window**. By default
that shaded interval is sunrise-to-sunset.

## Interactive map

The HTML report includes a Leaflet/OpenStreetMap chooser. Clicking the
map generates decimal `lat` / `lon` parameters and feeds the existing
wind/current resolution logic.

The map can show the selected location, wind observation station, and
current prediction station.

## Welcome page

`/welcome` is the nontechnical introduction. It includes a short
explanation, Q&A, a direct link into the conditions map, and GitHub
links for Star, Watch, Issues, and Pull Requests.

## Build and run

``` bash
go build -o sailing-go .
./sailing-go -server
```

Then open:

``` text
http://127.0.0.1:8080/welcome
```

or:

``` text
http://127.0.0.1:8080/report?format=html
```

## API examples

Default text report:

``` bash
curl -sS "http://localhost:8080/report"
```

Location-based HTML report:

``` text
http://127.0.0.1:8080/report?lat=37.9105&lon=-122.3602&format=html
```

Custom conditions window:

``` bash
curl -sS "http://localhost:8080/report?lat=37.9105&lon=-122.3602&start=7&end=10"
```

Wind-selection diagnostics:

``` bash
curl -sS "http://localhost:8080/report?lat=37.9105&lon=-122.3602&debug_wind=1"
```

JSON:

``` bash
curl -sS "http://localhost:8080/report?lat=37.9105&lon=-122.3602&format=json"
```

## Important query parameters

  -----------------------------------------------------------------------
  Parameter                           Purpose
  ----------------------------------- -----------------------------------
  `format=html`                       Interactive HTML report

  `format=json`                       JSON output

  `lat`, `lon`                        Decimal location

  `station`                           Explicit NDBC wind-station override

  `start`, `end`                      Optional custom conditions-window
                                      hours; omit both for
                                      sunrise-to-sunset

  `current_station`                   NOAA current-station override

  `bin`                               NOAA current-bin override

  `debug_wind=1`                      Wind-station diagnostics

  `at`                                Historical report date/time

  `compact=1`                         Compact output
  -----------------------------------------------------------------------

## Data and safety

This is a conditions-planning and exploration tool, **not a navigation
system**. Wind observations can be delayed, missing, or unrepresentative
of a particular piece of water. Current predictions are predictions
rather than measurements at your boat. Use appropriate forecasts,
observations, charts, local knowledge, and judgment.

## Project

``` text
https://github.com/richard-mauri/pittsburg-saildata
```
