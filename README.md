# Mauri's Weather & Water Conditions

A sailing-planning dashboard for the San Francisco Bay–Delta corridor and other supported waters. Select a sailing location to see nearby **observed wind**, **predicted tidal currents**, forecasts and environmental map overlays.

**Dashboard:** https://weather.themauris.org  
**Source:** https://github.com/richard-mauri/pittsburg-saildata

## Getting started

Open the dashboard, click the map where you want to sail, and generate conditions. Read the **Bottom Line** summary, then use **Conditions Now** and **Planning and Details** for observations, current timing, forecast context and overlays. You need not know NOAA station numbers: the application can select a nearby station with recent usable wind, or you can override it manually when you know a station is more representative.

Moving or zooming the map changes the view; **clicking a location** changes the selected sailing position. These actions are intentionally different.

## Understanding the data

**Wind** is measured at NOAA stations, including NDBC and selected validated NOAA CO-OPS sources. Review observation age, station identity, anemometer height and exposure.

**Current** is NOAA **tidal-current prediction**, not a direct measurement at your boat. Use the ebb, flood, slack, maximum-speed and time plots as planning context. Wind and current stations are selected separately.

**Forecasts and map layers** may include marine/point weather, radar, pressure, temperature, smoke/fire, wind barbs and swell depending on coverage and upstream availability. These are distinct from observations and tidal-current predictions.

## Main views and interfaces

- **Conditions Now:** concise current conditions and Bottom Line.
- **Planning and Details:** deeper wind, forecast and current-planning information.
- **Wind History:** observation plots/tables with 1, 4, 8, 12, 16, 20 or 24-hour windows where data are available.
- **Station selection:** automatic nearby-usable choice or explicit manual override.
- **Maps:** environmental context and map overlays.

The Go service supports browser, text and JSON outputs where available. Documented routes include `/report`, `/planning`, `/wind-readings`, `/wind-stations`, `/point-weather` and `/marine-forecast`; parameters and formats depend on the endpoint.

## Operating limits

The documented public service is hosted by a locally running Go application exposed through Cloudflare Tunnel; the server and tunnel must be running for public access. Wind measurements may be delayed, stations may be unavailable, and predicted currents may differ from actual water movement.

**This is a planning and exploration tool, not a navigation or safety-critical instrument.** Use official forecasts, marine notices, charts, local knowledge and seamanship before sailing.

## Development documentation

This README is for users. For contributors and coding agents, see [AGENTS.md](AGENTS.md) (mandatory agent instructions), [ARCHITECTURE.md](ARCHITECTURE.md) (system and behavior contracts), [DEVELOPMENT.md](DEVELOPMENT.md) (GitHub Issues, branches, draft PRs, CI and acceptance), and [CURRENT_WORK.md](CURRENT_WORK.md) (current active handoff). Development changes follow GitHub PRs rather than downloadable replacement-file handoffs.

## Optional account preferences

Sign in at [Homepage](https://home.themauris.org/) and launch **Sailing Outlook** from its card. SailingGo shows **Signed in as [user]** in the common suite banner. Guest/browser-only preferences and account-save warnings appear separately below the banner. Previously saved account preferences load automatically, and subsequent changes are saved automatically to Homepage's per-user revisioned `/api/storage/sailing` record. There are no Save, Load or Account Sync buttons.

Persisted choices include wind/distance units, selected-location and station URL settings, planning parameters and map type/overlay selections where supported. Browser-only visits continue working, with browser-local settings independent from authenticated account data. An expired/revoked token, unsupported stored schema or a revision conflict never authorizes overwriting someone else's account. If a concurrent edit conflicts, the current browser selection is retained and the sign-in status indicates the conflict. Homepage's companion authentication/storage feature is merged; authenticated persistence still depends on a valid Homepage session.


## Unit changes without a page reload

Wind and distance unit selectors update visible values in place without navigating or rebuilding the sailing map. Returning from Homepage applies saved units without first showing the wrong units and reloading the page. SailingGo fetches updated report values from its existing Go service and updates wind history using its existing data endpoint. If the update fails, the selector returns to the previous units instead of mislabeling displayed data. Selected sailing-location and station changes remain distinct from unit preferences and can require full report navigation.

## Direct-visit recovery of an existing Homepage login

If you open SailingGo directly in a fresh Safari tab with no transferable session bearer, the dashboard briefly navigates to Homepage's fixed `/launch/sailing` recovery route. Homepage verifies its existing unexpired and unrevoked parent session and returns SailingGo with a new parent-bound, fragment-only child bearer. SailingGo then automatically restores that user's saved preferences. If Homepage has no valid login, the bridge returns the dashboard once with `#suite_mode=local` and SailingGo remains usable with independent browser-only settings. There is no separate SailingGo login, automatic session renewal or credential transfer. The Homepage and SailingGo companion recovery changes are merged.

## Optional administrator activity reporting (Issues #21 and #23, merged)

When both `SAILING_ACTIVITY_URL` and a private 32-character-or-longer `HOMEPAGE_ACTIVITY_KEY_SAILING` are configured, SailingGo reports **aggregate, advisory** visitor and server work statistics to Homepage's separate administrator collector. The matching server-only key must also be configured for the `sailing` service on Homepage. The collector URL must use HTTPS or loopback-only HTTP and the exact `/api/admin/activity/ingest` path. Without both settings, SailGo's web interface and its background reporting are unchanged. Do not place service keys in browser scripts, files, Git, account settings or query strings.

While enabled, the SailingGo browser sends an ephemeral random tab identifier, visibility and interaction hints only to its own same-origin `/api/activity/ping`. The SailingGo server calculates recent observed presence over 2/5/15/60-minute windows and counts in-flight report, planning, forecast, weather, map-overlay and wind routes, plus bounded background swell prefetch work. For a supplied Homepage child bearer, the server verifies identity with Homepage and groups duplicate tabs using origin-scoped pseudonymous session aliases. The private Homepage collector receives aggregate counts and named-user session summaries, never raw bearers, alias keys, sailing coordinates, station IDs, forecast queries or personal settings. Invalid/revoked identity returns 401 rather than becoming anonymous; temporary verification outages return 503. One-hour guests remain authenticated visitors with browser-only preferences. When no valid session exists on the public site, a sign-in-required dialog offers a manual return to Homepage. This monitoring does **not** authorize or initiate a restart and is not a count of unique people across the internet.

### Isolated Mac/Safari acceptance without a production restart

Use a separate detached worktree of `origin/feat/21-suite-activity` so the owner’s existing `~/projects/sailing-go` checkout is not disturbed. Run `go test -count=1 .` and `go build -o /temporary/path/sailing-go .`. Start the candidate with `-server -port 8087`, plus `SAILING_ACTIVITY_TEST_LISTEN=127.0.0.1:8087`, `SAILING_ACTIVITY_URL=http://127.0.0.1:8788/api/admin/activity/ingest`, and a new disposable `HOMEPAGE_ACTIVITY_KEY_SAILING`. Start only the disposable Homepage test binary using its existing `TEST_ROOT/data`, `LISTEN_ADDR=127.0.0.1:8788` and the **same** private key; do not use the production `homego.sh` or SailingGo launcher. The first Safari page is `http://127.0.0.1:8087/welcome`; the second is `http://127.0.0.1:8788/suite-admin`. Use the existing disposable Homepage test administrator and refresh Session Activity after at least 75 seconds: SailingGo should say Reporting and show the observed browser tab. The test candidate may still fetch live NOAA data on user navigation, so this is an **online test**, not a fixture-only weather replay. Mac/Safari and live tunnel acceptance are not established by a Go CI pass.

## Verified suite access migration — draft Issue #35

SailingGo is the fifth application in the Homepage authentication sequence. A draft security migration introduces a reusable server-side verifier and offline tests. **This initial phase is not enabled on public weather/report routes:** the existing browser-only workflow still operates in the current main and this draft candidate until browser fetches and the listener are migrated together. No security guarantee should be inferred from the presence of an unwired verifier. The next phase must adapt every map, forecast, weather, report and background client request before enabling fail-closed enforcement. Homepage remains the only session issuer and already owns personal preference storage. Do not deploy this draft as a completed access-control change.

**Guest status presentation (Issue #35 draft):** A successfully verified Homepage Guest is identified once in the common masthead. The dashboard no longer repeats the redundant `Guest (browser-only preferences)` status below it; Guest preferences remain saved only in Safari browser storage, not the Homepage personal account. Sign-in, revoked-session and verification-error notices remain visible when necessary.

## Explicit Save Changes — Suite consistency (draft PR #36)

SailingGo's Conditions Now and Planning dashboard now includes **Save Changes** alongside Wind and Distance units. Editing units, map layers, marine-place filters, station/location selections and other captured preferences updates the view and stages a tab-scoped draft; it does **not** immediately write to the account or browser's saved preference record. A separate **Unsaved changes** notice appears, and Safari warns before leaving with pending edits. **Save Changes** commits Guest settings only to this browser, or issues one revision-checked Homepage `PUT /api/storage/sailing` for a verified personal user. Homepage saves are never automatic; failures retain the draft for manual retry, and account conflicts block overwrites. Pending drafts survive same-tab navigation between weather views and are scoped to the verified user/revision. A fresh user account can establish its initial saved settings on the first explicit save. This behavior requires owner Safari acceptance and does not activate the separately staged weather-API authorization gate.

## Discard Changes and net-zero edits (development Build v429)

A companion **Discard Changes** button now clears a pending draft and restores the last successfully saved preferences without saving anything. **Save Changes** and **Discard Changes** are enabled only while there are actual differences from the saved state. For example, switching Map Type Street → Nautical → Street leaves no change to save and therefore disables both buttons and avoids an unnecessary leave-page warning. The existing Safari warning remains for genuinely unsaved changes; choosing Leave still retains the pending draft for same-tab recovery until you save or discard it. Both personal-account and Guest storage remain separate.

**Map Type change detection (Build v430):** The preference listener now evaluates the newly selected Map Type and its matching `map_layer` value together, before the map controller changes the URL. Changing Street → Nautical → Street therefore returns to a clean saved state and disables Save/Discard without an unnecessary Safari leave warning. The existing explicit-save, saved Guest preferences and map behavior remain unchanged.

**Canonical preference comparison (Build v431):** Save/Discard dirty-state detection compares canonical saved and current preference values. Unchecked map overlays or marine-place categories absent from an older saved record are equivalent to explicit `false` controls on the current page, and the Map Type radio determines the effective `map_layer`. Reversing a change should disable both buttons even when the UI has more checkbox fields than the saved record. Real overlay edits must still enable Save.

**Incremental preference state (Build v432):** After another Safari failure, the preference change handler now copies the current saved/pending state and updates only the control that changed. Map Type changes no longer pull unrelated map viewport parameters or checkbox defaults into the draft. Legacy empty Map Type values also compare as the default Street layer. Save and Discard should disable again after reversing the only edit; owner Safari confirmation is still required.

**Tidal-current Range Save detection (Build v433):** Switching the Tidal Current chart Range between 1, 3 and 7 days now stages the `current_days` preference immediately. Its existing in-place chart refresh uses a fetch and history update, so it does not trigger a full-page navigation to detect changes. Save/Discard reflect an actual range change and clear again when the saved range is restored; the chart's refresh behavior is unchanged.

**All dropdowns and checkboxes (development Build v434):** The report now tracks every dropdown and checkbox through explicit Save Changes / Discard Changes, including Wind/Distance units, Wind History, tidal-current Range, Map Type, Swell playback Speed and Step, ALERTCalifornia camera display mode, all environmental map and marine-place checkboxes, ALERTCalifornia visibility, and the tidal-range overlay. Changed values show pending state; reversing an edit clears it. Swell and camera display options are UI-only preferences stored in the same schema-1 settings record without adding artificial report URL parameters; chart checkbox selection is restored after AJAX redraws. Save commits only on user action (per-account Homepage record or Guest browser-local), and Discard restores the prior saved selections. Start date and chart navigation actions are date/location inputs, not dropdowns or checkboxes. Safari acceptance remains required.

**Wind History and preferred-period planning state (development Build v435):** Four-hour Wind History is the default, including when an older record explicitly stored `wind_hours=4`; reversing an edit to the saved history window clears Save and Discard. The preferred-period planning form now participates in the same explicit Save/Discard model: start/end times, ebb/flood caution and red thresholds, time buffer, and current-station distance caution. Values are staged before the chart refresh and restored from the current saved or pending state after the AJAX redraw. The personal account and Guest browser-local persistence boundaries are unchanged. CI tests exercise reversals and restored nondefault planning values; actual Safari acceptance is still required.

## Verified Homepage access (Build v436 draft)

Open SailingGo from Homepage as a personal user or verified Guest. Browser navigation now requires server-side authentication. An anonymous top-level report request receives a small sign-in page without weather data; the Homepage handoff is verified before issuing an encrypted Secure HttpOnly first-party browser session cookie. The Go server revalidates Homepage identity on every protected weather, forecast, report and API request, including direct links. Expired/revoked sessions receive 401; Homepage verification outages fail closed with 503. Browser chart/overlay requests work through the same-site cookie. Direct text/JSON API clients must provide their own approved Homepage Authorization Bearer header, never put tokens in URLs. Public health, welcome and minimal static resources remain available. Personal account preferences and Guest browser-only preferences retain their previous isolation. This draft is not yet Safari/tunnel/NOAA accepted.

**Development Build v437 — smoother launch and planning updates:** The brief Homepage authentication handoff uses the same automatic light/dark background and panel palette as the rest of SailingGo, rather than a bright unstyled page. Editing current-chart range or preferred-period planning values uses an in-place fetch without dimming the report cards. If the chart fetch fails, the current report remains visible and a local status message appears instead of forcing a full-page reload. The explicit Save Changes/Discard Changes contract is unchanged: editing a planning value stages a pending preference; it does not save automatically. Safari appearance and live authentication/NOAA acceptance remain to be verified.

**Development Build v438 — stationary Save/Discard row:** The Save Changes and Discard Changes buttons remain in the same toolbar row as the wind and distance unit selectors. Their status text is displayed in a dedicated smaller-font line immediately **below** the toolbar. The line reserves vertical space even when empty, so clicking Save, Discard, encountering a conflict, or clearing a notification does not shift the controls. Preference persistence, Guest/account isolation and explicit-save semantics are unchanged. Safari visual acceptance remains necessary.

**Development Build v439 — nautical map and weather radar tile fallback:** If NOAA Nautical Chart imagery is unavailable, SailingGo now keeps the regular Street Map visible beneath the transparent NOAA chart WMS. When the NOAA tile layer fails entirely, it displays a map notice and falls back to Street Map without changing the user's saved/pending Nautical selection. Weather Radar now uses Iowa State IEM's cached transparent nationwide NEXRAD N0Q PNG tiles rather than the prior WMS tile imagery that could obscure parts of the basemap with blank patches. Partial radar tile failures are reported rather than silently ignored. NOAA chart/data and NWS radar sources remain external and require live Safari verification. Radar imagery is weather context, not navigation data.
# Current work — SailingGo

**Active:** [Issue #37](https://github.com/richard-mauri/pittsburg-saildata/issues/37), draft [PR #38](https://github.com/richard-mauri/pittsburg-saildata/pull/38) on `charts/37-ncds-mbtiles-evaluation` targeting merged `main` `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47`. Continue this PR; not merged/released/deployed.

**New scoped phase — Go-owned nautical tile framework:** Safari now requests `/api/charts/tiles/{z}/{x}/{y}.png` on the SailingGo origin rather than NOAA WMS. New `nautical_tiles.go` registers a handler on the existing Go mux with the existing auth gate. No NOAA upstream request occurs. Without `SAILING_NAUTICAL_TILE_DIR` configured, valid tile requests return HTTP 503; existing browser progress/failure UI should return to Street. With a configured **pre-extracted XYZ PNG directory**, the Go handler validates z/x/y and serves tiles at `z/x/y.png`; 404 for missing tiles, 503 for bad/unreadable data, 400 invalid path, GET/HEAD only, and no-store errors. Existing Street underlay, loading/Cancel/failure Close, 12-second timeout, eight-second auto-dismiss and pending map preference Save/Discard remain in place. `nautical_tiles_test.go` covers offline handler behavior; browser Node test asserts local endpoint and no NOAA chart WMS URL.

**Limitations and pending work:** No NOAA download, NOAA proxy/cache, automatic retries, local chart provisioning, direct NCDS MBTiles SQLite read or MBTiles TMS-row conversion yet; `ncds_19c.mbtiles` is not a ready-to-serve XYZ directory. Owner's existing 241,352,704-byte `ncds_19c.mbtiles` is outside Git; SHA-256 `6f887b5df127d142f6f6826f8968f41977e89417e2205fccd0cc7524fa26f096` and May 28, 2024 file timestamp. Dataset currency, relevant Pittsburg/Delta tile coverage, neighboring region packages and official rights/attribution remain unresolved. No fallback to old charts silently. NOAA availability-watch automation is separate and does not update code or the owner's Mac.

**Preservation:** Existing accounts, private tokens/settings, auth, Guest/personal explicit Save/Discard, weather/current APIs, chart popup behavior, radar, markers, overlays, cloudflared, binary build/version counters, releases and deployment untouched. Owner-local dirty/unpushed changes remain inaccessible. Root Go app and Node CI tests required; no Python; standalone Go auditor and GitHub CI unchanged. Current Homepage main suite policy v1.1.0 and SailingGo AGENTS, README, ARCHITECTURE, DEVELOPMENT, CURRENT_WORK reviewed. README, ARCHITECTURE, DEVELOPMENT, CURRENT_WORK updated for new endpoint; AGENTS unchanged.

**Verification:** Latest CI head/results must be verified. Owner Mac/Safari/live test unverified. No local production restart or tunnel change authorized.

**Approval boundary:** PR remains draft; no FINISH, merge, issue closure, remote deletion, config change, deployment, tag, semantic version or release authorized.

**Owner commands after exact-head CI:**
```sh
cd ~/projects/sailing-go
git status --short --branch
# Stop if any uncommitted changes are shown.
git fetch origin
git checkout charts/37-ncds-mbtiles-evaluation
git pull --ff-only origin charts/37-ncds-mbtiles-evaluation
git status --short --branch
./devbuild.sh
./sailing-go -version
```
Owner chooses service launch separately; no runner process or Terminal will be stopped.

### Nautical chart cache — Go server fallback (Issue #37 draft)

SailingGo's Nautical Chart layer **always** requests `/api/charts/tiles/{z}/{x}/{y}.png` from its own Go server. The Go server now checks a persistent disk cache first. If the tile is absent it makes a bounded WMS GetMap request to NOAA, validates the returned 256×256 PNG and atomically saves it to disk for future requests. Cache hits work even when NOAA is unreachable. There is no direct browser-to-NOAA chart traffic, separate tile-server process, separate offline installer, automatic bulk download, or MBTiles reader at this stage.

The default chart cache directory is `<user cache directory>/sailing-go/nautical-tiles`, resolved by Go with `os.UserCacheDir()` (typically `~/Library/Caches/sailing-go/nautical-tiles` on macOS). The operator may override it with the `SAILING_NAUTICAL_TILE_DIR` environment variable; it must be a writable directory outside the GitHub working tree. Tiles are stored as `z/x/y.png`. A missing tile triggers one upstream NOAA request, limited to five seconds, with a maximum of four concurrent upstream requests; simultaneous same-tile requests are coalesced and failures suppress additional upstream requests temporarily. NOAA errors return HTTP 503 and activate the existing Street Map fallback and dismissible status panel. Disk read/write failures are reported, not hidden. **No charts are guaranteed to exist on first use:** NOAA must return valid chart imagery to populate this cache. Cache imagery is not automatically refreshed, and chart age/coverage is not yet validated. Do not use this display as a primary navigation source; users must verify chart updates independently. The separately downloaded `ncds_19c.mbtiles` SQLite package is not read by this handler and remains outside Git.

### Nautical chart request diagnostics (draft Issue #37)

For any request to `/api/charts/tiles/z/x/y.png`, Safari's Network response headers expose `X-Sailing-Chart-Source` (`cache`, `noaa`, or `unavailable`) and `X-Sailing-Chart-Result`. Results include `cache-hit`, `noaa-fetched`, `noaa-fetched-shared`, `noaa-timeout`, `noaa-http-error`, `noaa-invalid-png`, `cooldown`, `queue-timeout`, and `cache-write-error`. The `X-Sailing-Chart-Upstream-Status` response header contains the actual HTTP status when the upstream returned one. A 503 alone does not establish that Go contacted NOAA: check `X-Sailing-Chart-Result`; `cooldown` means no new NOAA call occurred. Outbound NOAA attempts also emit `NAUTICAL tile=z/x/y upstream=noaa result=... status=... duration_ms=...` in the **Go server log**, without full upstream URLs, local file paths, response bodies, or credentials. These diagnostics do not change JavaScript, the loading panel, or nautical-chart availability.

### Nautical chart offline test fixtures (Issue #37 draft)

The development branch includes a standalone, Go-only **fictional test tile** generator at `cmd/mocknauticaltiles`. It creates clearly marked **MOCK TEST DATA — NOT FOR NAVIGATION** XYZ PNG tiles around Pittsburg and the Delta at zooms 10–12 in an explicitly supplied, new, out-of-repository directory. It does not download NOAA data, install charts, change SailingGo settings or modify the live tile cache. It exists solely to test the pre-existing disk-cache-to-browser route independently of NOAA connectivity. See [DEVELOPMENT.md](DEVELOPMENT.md) for the bounded owner-controlled procedure. Successful mock placement is **not** evidence that NOAA chart imagery is available, current or complete.

In an **owner-authorized separate loopback test server**, adding `nautical_mock_test=1` to the local page URL allows viewing incomplete test tiles without the NOAA failure timer switching the map back to Street. This browser-only flag is inactive on the public site, and is not a way to safely expose mock tiles through the Cloudflare Tunnel. Missing mock tiles show Street underneath, and the test scope is z10–z12. See DEVELOPMENT for safety and Safari steps.

### Bounded nautical mock inspection — staged browser UI (Issue #37)

The Go-only mock generator now creates **new** fictional XYZ PNG fixtures across a bounded Pittsburg–Delta rectangle (longitude −121.98 to −121.48, latitude 37.96 to 38.16), at zooms **9–14**. Its files remain outside Git and must never be served through the public Cloudflare site or mistaken for nautical charts. The old 45-tile fixture directory is unchanged; generate a **different new directory** for the expanded set.

Nautical load presentation uses **one in-map status panel**: progress and Cancel appear when Nautical is requested **before** the nautical tile container is shown; when the current image request cycle finishes, the panel closes and valid tiles are shown. On ordinary NOAA failure the same panel changes to a retry-later error with Close (and auto-dismiss), and the map reverts to Street through the existing staged preference pathway. An explicitly opted-in localhost/127.0.0.1 `nautical_mock_test=1` view uses the same progress panel but a short 2.5-second reveal bound; missing fictional tiles do **not** trigger automatic Street selection. Public URLs cannot opt into this test-only relaxation. Safari visual acceptance and actual NOAA coverage remain separate, unverified gates.

### Nautical chart gaps and zooming (Issue #37 draft)

When a selected Nautical Chart cannot provide tiles at the current zoom/location, the map **keeps Nautical selected** and leaves Street tiles underneath any missing imagery. The existing single loading panel changes to a dismissible error explaining: **“Nautical chart tiles are unavailable at this zoom. Try changing zoom levels or moving the map.”** No automatic change of map type or implicit Save occurs on timeout or completed tile failures. Starting a new zoom/pan attempt clears the previous error. Explicit Cancel during loading still selects Street through the ordinary staged settings mechanism. A successful tile-load cycle dismisses the panel. This does not imply NOAA is working or that nautical coverage is complete.

### Latest Stage A correction — stable zoom rendering and complete supported zoom range

The nautical map control supports **display zoom levels 9 through 18**. Normal NOAA data uses native tiles through zoom 16 and Leaflet enlarges those tiles at zoom 17–18. Explicit `nautical_mock_test=1` on localhost/127.0.0.1 requests native **mock** tiles through zoom 18 to exercise every displayed level. The Go-only generator now creates **954** unmistakably marked `MOCK TEST DATA / NOT FOR NAVIGATION` XYZ PNGs in a **new, nonexistent** external directory: a wider Pittsburg–Delta rectangle through zoom 14, plus bounded 7×7 patches near Pittsburg, the previously selected Pittsburg map point and the Delta at zoom 15–18. The high-zoom patches are NOT complete Bay/Delta chart coverage.

Leaflet now controls visibility and zoom transitions throughout loading. The status panel does **not** hide the entire nautical image layer; successful tiles remain visible while further images load. A later failed batch at a zoom with previously successful tiles is partial coverage, not a total outage, and no misleading “unavailable” notice is displayed. At zooms with no successful tiles, Nautical stays selected and the same panel suggests changing zoom or moving the map. The Go NOAA proxy is unchanged and NOAA availability has not been established. The owner's older 45-tile and z9–14 fixtures are not automatically upgraded: generate a new directory and use only in an authorized isolated test instance, never the public Cloudflare site.
