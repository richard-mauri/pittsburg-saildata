# Mauri's Weather & Water Conditions

A sailing-planning dashboard for the San Francisco Bay–Delta corridor and other supported waters. Choose where you plan to sail and see nearby **wind observations**, **predicted currents**, weather forecasts and environmental map overlays in one place.

**Open the dashboard:** https://weather.themauris.org  
**Source code:** https://github.com/richard-mauri/pittsburg-saildata

> **For AI coding agents and future chats:** [AGENTS.md](AGENTS.md) contains the development procedure, current build handoff, source/version rules and Git safeguards. This README is a general user guide.

## Getting started

Open the dashboard, click the map at the area you're considering, and generate conditions for that location. Read the **Bottom Line** summary first, then explore **Conditions Now** and **Planning and Details** for supporting observations, current timing, forecasts and overlays.

You do not need to know NOAA station numbers. The system can choose a nearby wind station with sufficiently recent observations. You may also choose a station yourself when you know its exposure is more representative of your sailing area.

Map position and sailing selection are deliberately separate: moving or zooming the map changes the view; clicking a point selects the location for the sailing report.

## What the data means

**Wind** is based on measured observations from NOAA stations, including NDBC and selected validated NOAA CO-OPS sources when available. Check the station name, time and measurement freshness. Wind varies across the Bay and Delta, and station heights and exposure differ.

**Currents** are NOAA **tidal-current predictions**, not measurements of water moving at your boat. The predictions help estimate flood, ebb, slack periods and maximum currents, and can be displayed with a time-based graph. Current station choice is distinct from wind station choice.

**Forecasts** provide prospective marine/point weather information rather than observed conditions. Radar, smoke/fire, pressure, temperature, wind barbs, swell and other map overlays depend on coverage, region, zoom and upstream availability.

## Main views

- **Conditions Now:** a concise current summary and Bottom Line for the selected area.
- **Planning and Details:** deeper weather, wind-history and current-prediction information.
- **Wind History:** recent observations with a chart/table and selectable periods of 1, 4, 8, 12, 16, 20 or 24 hours, when the data source provides observations.
- **Station selection:** automatic nearby usable station or manual override.
- **Maps:** layered environmental information for spatial context and route planning.

The service also offers HTML, text and JSON reporting for integrations and scripts. Core routes include `/report`, `/planning`, `/wind-readings`, `/wind-stations`, `/point-weather` and `/marine-forecast`; availability and parameters vary by endpoint.

## Operating notes and limits

The public site is served by a locally running Go application exposed through Cloudflare Tunnel. Its server and tunnel must remain running for the public reader to be accessible.

Observations can be delayed, stations can be offline, and predicted currents will differ from actual conditions. The dashboard is **not a navigation or safety-critical instrument**. Use current official marine forecasts, notices, charts, local knowledge and seamanship before sailing.

For code changes, testing, reproducible builds and the authoritative next-chat development handoff, see [AGENTS.md](AGENTS.md).
