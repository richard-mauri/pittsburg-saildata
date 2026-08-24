# Mauri's Sailing Outlook ⛵

[![Mauri's Sailing Outlook --- San Francisco Bay and Delta sailing
conditions](hero.jpg)](https://pittsburg-saildata.onrender.com/report?format=html)

## Pick where you're sailing. Get the outlook.

**Mauri's Sailing Outlook** is a free sailing-planning tool for the San
Francisco Bay and Delta.

You do **not** need to know NOAA station numbers, buoy IDs, or
latitude/longitude.

**[Open the Sailing
Outlook](https://pittsburg-saildata.onrender.com/report?format=html),
click on the map where you're thinking about sailing, then click
*Generate Sailing Outlook*.**

The report combines nearby **real-time wind observations** with
**predicted currents** and puts the useful sailing information up front.

------------------------------------------------------------------------

## The 30-second version

1.  Open the Sailing Outlook.
2.  Click your sailing area on the map.
3.  Click **Generate Sailing Outlook**.
4.  Read the **Bottom Line** first.
5.  Scroll down when you want the details.

The map also shows where the wind observation and current prediction are
coming from.

If you know the Bay or Delta well, try several locations. The results
can change as the tool selects data sources appropriate to the area.

------------------------------------------------------------------------

# Questions sailors will probably ask

## Is this tide data or current data?

**Current data.**

The report uses NOAA current predictions: the predicted speed and
direction of the moving water, including **ebb, flood, maximum current,
and slack**.

That distinction matters to sailors. Tide height and current are
related, but they are not the same thing.

## Where does the wind come from?

Wind observations come from NOAA's **National Data Buoy Center (NDBC)**
network.

When you click a location on the map, the service looks at nearby
meteorological stations and finds the nearest one with recent usable
wind data.

The station used for the report is shown on the page and on the map.

## Why doesn't it simply use the closest wind station?

Because the closest station is not always providing usable recent wind
through the NOAA feed.

The service checks nearby candidates concurrently and chooses the
**nearest station that actually has usable wind observations**.

## Can I choose a different wind station myself?

Yes.

The **Nearby Wind Stations** section lists other candidates. Click a
station to regenerate the report using it.

This is useful because sailors may know that a particular station better
represents the water they care about.

The page distinguishes:

-   **AUTO** --- the station the service would choose automatically.
-   **SELECTED** --- the station currently being used.

## What is the current graph showing?

It shows how the **predicted current speed changes with time**.

That makes it much easier to see:

-   slack water;
-   building flood;
-   maximum flood;
-   building ebb;
-   maximum ebb;
-   when the current starts easing.

The report also compares important current maxima rather than relying
only on vague labels such as "strong."

## Why compare one ebb or flood with another?

A number by itself can be hard to interpret.

For example, an ebb during your sailing window may sound significant
until you see that it is only about half the magnitude of the other ebb
that day.

Current maxima also vary through lunar and other cycles, so the report
provides recent-cycle context where the NOAA data supports it.

## Can I use it for navigation or safety decisions?

No. Treat it as a **sailing-planning and conditions-exploration tool**,
not a navigation system.

Observations can be delayed or missing. Station exposure varies. Current
predictions are predictions, not measurements at your boat.

Use normal marine forecasts, observations, charts, local knowledge, and
seamanship when making sailing decisions.

------------------------------------------------------------------------

# Know these waters? Your feedback is valuable.

This project is still being improved, and feedback from sailors is
particularly useful.

If something looks questionable, that is worth reporting. Examples:

-   "That wind station isn't representative of Alameda."
-   "The current wording is confusing here."
-   "I expected the ebb to be much stronger at this time."
-   "This station would be a better choice for this sailing area."
-   "It would be useful if the report also showed \_\_\_."

You don't need to be a programmer to contribute useful information.

------------------------------------------------------------------------

# Follow or help improve the project

The project is open source on GitHub:

**[richard-mauri/pittsburg-saildata](https://github.com/richard-mauri/pittsburg-saildata)**

### ⭐ Star it

If you find the project useful, click **Star** on GitHub.

It's an easy way to show that sailors are actually using or interested
in the project.

### 👀 Follow improvements

The tool is changing fairly quickly.

On the GitHub repository, use **Watch → Custom** to choose the kinds of
updates you want. **Releases** are a good low-noise choice once project
releases are being published. Add **Issues** and **Pull Requests** if
you want to follow development more closely.

### 💡 Have an idea or find something odd?

Open a GitHub **Issue**.

Issues are not just for software bugs. Sailing knowledge, confusing
terminology, questionable station selection, and feature ideas are all
useful.

### 🔧 Write code?

Pull Requests are welcome.

The service is written in Go, and the repository contains the source and
project documentation.

------------------------------------------------------------------------

# Want the geeky version?

When you click the map, the browser sends the selected decimal
latitude/longitude to the Go service.

For wind, the service uses cached NDBC active-station metadata, finds
nearby meteorological-capable stations, calculates geographic distance,
probes nearby candidates concurrently for recent usable observations,
and chooses the nearest usable station.

For current, it uses NOAA CO-OPS current-prediction data to build the
current events, sailing-window analysis, relative comparisons, and
graph.

The HTML page is the friendly interface, but the service also provides
text and JSON output for scripts and integrations.

If that sounds interesting, **[browse the source on
GitHub](https://github.com/richard-mauri/pittsburg-saildata)**.

------------------------------------------------------------------------

## The best way to help

**Try it somewhere you actually sail.**

If the result looks good, great.

If it looks wrong or misleading, that's even more interesting --- tell
Richard what location you selected, what the report said, and what you
expected instead.
