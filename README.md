# Mauri's Wind & Current Conditions

A small Go service for exploring **observed wind and predicted tidal
currents** for sailing and other on-the-water planning.

The service is location-oriented rather than tied to one bay or harbor.
Choose a point on the interactive map and it finds nearby usable
NOAA/NDBC wind stations, combines those observations with NOAA CO-OPS
current predictions where available, and presents the result as a
sailing-oriented report.

The project began around Pittsburg and the San Francisco Bay/Delta, but
the location and station-selection workflow is designed to work much
more broadly wherever the underlying NOAA/NDBC data is available,
including coastal areas around the United States and available
Canadian-region stations.

## Try it

Production welcome page:

``` text
https://pittsburg-saildata.onrender.com/welcome
```

Interactive conditions page:

``` text
https://pittsburg-saildata.onrender.com/report?format=html
```

A typical workflow is:

1.  Click the area where you expect to be on the map.
2.  Click **Show Conditions**.
3.  Read the **Bottom Line**.
4.  Check the wind station being used and its distance from your
    selected location.
5.  Review the tidal-current graph and preferred-period planning hint.
6.  Select a different nearby wind station when local knowledge suggests
    it is more representative.

## Features

-   NOAA/NDBC real-time wind observations
-   NOAA CO-OPS tidal-current predictions
-   Interactive location selection
-   Automatic selection of the nearest **usable** wind station
-   Nearby wind-station candidates shown on the map
-   Explicit wind-station selection
-   Distance shown **From Selected Location**
-   Warning when the selected wind observation is geographically distant
-   Current, gust, recent wind trend, and longer-window wind statistics
-   1-, 3-, and 7-day tidal-current graphs
-   Calendar-based current planning
-   Day/night graph shading
-   Highlighted configurable preferred sailing period
-   Max ebb and max flood planning limits
-   Configurable time buffer around the preferred period
-   Preferred / Caution / Red flag planning hints
-   Event slider for max flood, max ebb, and slack-water inspection
-   Text, HTML, and JSON report formats
-   Compact output for assistants and integrations
-   REST `/report` and `/health` endpoints
-   Render deployment

## Data sources

### Wind

Wind observations come from NOAA's National Data Buoy Center (NDBC).

The service reads NDBC station metadata and `realtime2` observations.
NDBC wind speed and gust values are converted to knots.

Wind observations are **point measurements**. A nearby station is not
necessarily representative of the wind at the selected location.
Coastlines, terrain, channels, headlands, buildings, and local exposure
can produce substantial differences over surprisingly short distances.

### Tidal current

Tidal-current predictions come from NOAA CO-OPS.

The current display is **current speed and direction, not tide height**:

-   above zero = flood
-   below zero = ebb
-   zero crossings = slack water

The graph uses NOAA harmonic current predictions and identifies
significant flood, ebb, and slack events.

Current predictions are predictions, not direct measurements of the
water around your boat.

## Interactive map and wind-station selection

Clicking a location on the map supplies decimal `lat` and `lon`
coordinates to the report.

The service then:

-   finds nearby wind-station candidates;
-   checks them for recent usable wind observations;
-   chooses the nearest usable station as the automatic starting point;
-   shows the candidates on the map; and
-   reports station distance **From Selected Location**.

`AUTO` identifies the automatic choice. `SELECTED` identifies the
station currently driving the report.

You can select another candidate station while preserving the original
selected location. This is useful when local knowledge tells you that
another observation site better represents the water you care about.

### Panning and searching

Panning the map changes only the viewport. It does not silently change
the report location or station-distance calculations.

After panning, use **Search this area for wind stations** to make the
center of the visible map the new selected location and recompute nearby
candidates.

The distinction is intentional:

-   **Pan** --- explore the map.
-   **Search this area for wind stations** --- search around the visible
    map center.
-   **Click a location** --- choose a precise report location.
-   **Select a station marker** --- explicitly use that wind station
    while retaining the selected location.

### Distance warning

A geographically nearby buoy can still be surprisingly far from the
water you actually plan to sail.

When the selected wind station is sufficiently distant, the Wind card
displays a distance warning. The warning does not prevent use of the
station; it makes the separation explicit so you can decide whether the
observation is useful.

This matters especially along irregular coastlines. A buoy that sounds
geographically relevant may be many nautical miles from the selected
sailing area and may experience substantially different wind.

## Tidal-current planning

The Current card supports **1-day, 3-day, and 7-day** views.

Use the controls immediately above the graph to:

-   choose a start date with the calendar;
-   select a 1-, 3-, or 7-day range;
-   return to **Today**; or
-   move to the previous or next displayed range.

The same planning classifier is used in all three views. A date marked
Red flag in the 7-day view should therefore also be Red flag when that
date is opened by itself in the 1-day view.

### Graph

The graph visually distinguishes:

-   darker background --- night;
-   lighter background --- daylight;
-   warm highlighted band --- configured preferred sailing period.

For multi-day views, significant current events can be inspected with
the event slider below the graph. The graph's event dots are
informational rather than clickable; use the slider or Previous / Next
event controls to inspect them.

## Preferred-period planning hint

The planning hint is intended to make it faster to answer a practical
question: **which days and times look attractive for being on the
water?**

The defaults are:

  ------------------------------------------------------------------------
  Setting                                    Default Meaning
  --------------------- ---------------------------- ---------------------
  Preferred start                           12:00 PM Beginning of the
                                                     preferred sailing
                                                     period

  Preferred end                              5:00 PM End of the preferred
                                                     sailing period

  Max ebb                                     1.5 kt Maximum preferred ebb
                                                     speed

  Max flood                                   1.5 kt Maximum preferred
                                                     flood speed

  Buffer                                      60 min Extra time checked
                                                     before and after the
                                                     preferred period
  ------------------------------------------------------------------------

All of these planning values are configurable.

### Max ebb

**Max ebb** is the strongest ebb you want during the preferred period.

Strong ebb can be particularly important when wind opposes the current,
when conditions may become rougher than the wind observation alone
suggests.

The classifier examines the NOAA 6-minute current predictions throughout
the preferred period. It does not merely check whether the exact time of
maximum ebb falls inside the window.

### Max flood

**Max flood** is the strongest flood you want during the preferred
period.

A strong flood may present a different problem from a strong ebb. For
example, it can make progress difficult when you are trying to sail
against the current.

Ebb and flood therefore have separate configurable limits.

### Buffer

**Buffer** extends the planning check before and after the preferred
period.

With the default 60-minute buffer and a 12 PM--5 PM preferred period,
the classifier also looks immediately outside that period for strong
current. This prevents a large ebb or flood peaking shortly after 5 PM,
for example, from being treated as irrelevant simply because its maximum
falls a few minutes beyond the nominal window.

### Planning statuses

**Red flag** means ebb or flood reaches or exceeds its configured limit
**during the preferred period**.

**Caution** means a configured ebb or flood limit is reached **within
the buffer**, but not during the preferred period itself.

**Preferred** means ebb and flood remain below their configured limits
through those checks.

The hint reports whether ebb, flood, or both caused the classification
and includes the relevant speed and time when applicable.

These are planning cues, not safety determinations. Wind, swell,
weather, traffic, local bathymetry, and other conditions still matter.

## Date and planning parameters

The HTML current-planning controls preserve their settings in the report
URL so a configured view can be bookmarked or shared.

Important parameters include:

  -----------------------------------------------------------------------
  Parameter                           Purpose
  ----------------------------------- -----------------------------------
  `format=html`                       Interactive HTML report

  `format=json`                       JSON report

  `lat`, `lon`                        Selected decimal location

  `station`                           Explicit NDBC wind-station override

  `current_station`                   NOAA current-station override

  `bin`                               NOAA current-station bin override

  `current_date`                      Starting date for the current graph

  `current_days`                      Current graph range: 1, 3, or 7
                                      days

  `planning_start`                    Preferred-period start time

  `planning_end`                      Preferred-period end time

  `max_ebb`                           Maximum preferred ebb in knots

  `max_flood`                         Maximum preferred flood in knots

  `planning_buffer`                   Buffer before/after the preferred
                                      period, in minutes

  `debug_wind=1`                      Wind-station selection diagnostics

  `compact=1`                         Compact report output

  `at`                                Historical report date/time
  -----------------------------------------------------------------------

Default planning values may be omitted from the URL.

## API examples

Default report:

``` bash
curl -sS "http://localhost:8080/report"
```

Interactive HTML report for a selected location:

``` text
http://127.0.0.1:8080/report?format=html&lat=37.47310&lon=-122.48520
```

Explicit wind station while preserving the selected location:

``` text
http://127.0.0.1:8080/report?format=html&lat=37.47310&lon=-122.48520&station=46012
```

Wind-station diagnostics:

``` bash
curl -sS "http://localhost:8080/report?lat=37.47310&lon=-122.48520&debug_wind=1"
```

JSON:

``` bash
curl -sS "http://localhost:8080/report?lat=37.47310&lon=-122.48520&format=json"
```

Compact JSON:

``` bash
curl -sS "http://localhost:8080/report?format=json&compact=1"
```

## Build and run

Build the service:

``` bash
go build -o sailing-go .
```

Run as a server:

``` bash
./sailing-go -server
```

The local server defaults to port `8080`. The `PORT` environment
variable is honored for hosted deployment.

Open:

``` text
http://127.0.0.1:8080/welcome
```

or:

``` text
http://127.0.0.1:8080/report?format=html
```

## Deployment

The production service is deployed from GitHub to Render.

Typical workflow:

``` text
edit source
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

Production service:

``` text
https://pittsburg-saildata.onrender.com
```

## Data and safety

This is a **conditions-planning and exploration tool, not a navigation
system**.

Wind observations can be delayed, missing, or unrepresentative of the
selected water. A station's geographic distance is only one factor in
deciding whether its observation is relevant.

Tidal-current values are harmonic predictions rather than measurements
at your boat.

Use appropriate marine forecasts, observations, charts, local knowledge,
and seamanship judgment when making decisions on the water.

## Project

``` text
https://github.com/richard-mauri/pittsburg-saildata
```
