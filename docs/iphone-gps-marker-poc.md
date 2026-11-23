# Issue #41 — foreground iPhone GPS marker proof of concept

This draft adds an **opt-in, mobile-width-only** GPS marker to the existing Leaflet map on Conditions Now. It is separate from the offline chart installer draft PR #42 and does not implement offline downloads, track recording, background location or navigation guidance.

## Behavior

On a mobile viewport (up to 767px) and HTTPS secure context, the map's lower-left control shows **Show My Position**. A user tap requests Safari geolocation via `watchPosition` (high accuracy, no cached position, 15-second timeout). A blue location dot with a white border appears over the selected map layer; a translucent circle shows reported GPS horizontal accuracy. The map follows the dot by default; dragging the map suspends following, and **Follow Boat** resumes it. **Stop GPS** clears the watch; the last location dot remains visible as a historical last fix, not live navigation.

GPS coordinates remain in the browser. The feature does **not** call SailingGo APIs with the GPS position, change URL parameters, select a sailing/weather location, modify Homepage settings, record a trip or touch the Mac chart cache. Marker location is a device position, **not bow heading**. Safari foreground/unlocked is the only supported test condition; iOS may suspend watches when backgrounded/locked.

## Owner iPhone acceptance (requires separately approved candidate deployment)

1. Build and run the PR candidate intentionally through the existing owner-controlled Mac/Cloudflare HTTPS route; **do not run the launcher or restart the service/tunnel automatically**.
2. Open `https://weather.themauris.org` on iPhone Safari and open Conditions Now with the Nautical Chart layer at native zoom 13–15.
3. Tap **Show My Position** and grant **Precise Location** permission. Confirm a blue GPS dot and accuracy circle appear at the device's actual position, and that the map follows while walking outside.
4. Drag the map: following should pause. Tap **Follow Boat** to recenter. Tap **Stop GPS** and verify watch stops.
5. Verify that moving the GPS dot or following does **not** change selected sailing coordinates, weather stations, URL, overlays or desktop UI. Deny permission and confirm a clear status message. Check GPS in Safari foreground; after lock/unlock a fresh fix may be needed.
6. GPS may be outside cached nautical chart coverage (for example, walking inland). The blue marker can still appear on the Street Map, and missing NOAA chart tiles must not be represented as water or verified chart coverage.

## Validation boundaries

GitHub CI checks Go compilation and regression tests; it cannot verify actual iPhone GPS, Safari location permissions, visual layering, device motion, HTTPS authorization, live chart availability or Mac/Cloudflare behavior. No code in this PR should be described as deployed, merged or field tested until observed. No merge, deployment, restart or release without separate approval.
