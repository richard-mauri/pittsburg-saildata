# Current work — SailingGo

**Active scope:** [Issue #37](https://github.com/richard-mauri/pittsburg-saildata/issues/37), [single draft PR #38](https://github.com/richard-mauri/pittsburg-saildata/pull/38), branch `charts/37-ncds-mbtiles-evaluation` targeting merged `main` `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47` (PR #36). No other PR or production change.

**Latest NEXT — direct Nautical selection timeout:** Owner Safari diagnostics Nov. 21, 2026: ten NOAA chart images all **pending**, zero loaded and zero failed, Nautical selected, Street visible, warning hidden. The previous 12-second timer depended on Leaflet's `loading` event, which may not fire. Fixed in `web/templates/sailing.html`: `applyPreferredBaseMap()` now starts the bounded NOAA response check whenever Nautical is active and no check is outstanding; covers initial saved Nautical state and manual Street → Nautical selection. Reapplying the map does not restart a running check. No response after 12 seconds triggers the existing **not responding, Street fallback, try later** message. Successful images still receive the conditional **if chart detail is missing** advisory; this does not establish blank pixels or NOAA maintenance. Existing user preferences, markers, overlays and Street Map remain intact. `scripts/test-explicit-save.cjs` tests the real map-apply function with no Leaflet loading event, initial/default Nautical, manual selection, cancellation/idempotence and previous fallback, alongside injected loading-event diagnostics.

**NOAA field evidence:** Preserved in Issue #37. Distribution webpage reachable; `encdirect.noaa.gov` timed out awaiting headers and `gis.charttools.noaa.gov` timed out at TCP connect. Downloadable `ncds_19c.mbtiles`: **241,352,704 bytes**, last modified May 28, 2024, SHA-256 `6f887b5df127d142f6f6826f8968f41977e89417e2205fccd0cc7524fa26f096`, SQLite integrity `ok`, PNG `overlay` with zoom 0–16. Pittsburg z14/15/16 sampled tiles 177/177/237 bytes suspicious but pixels unexamined; Delta z16 sample absent. Exact current NOAA polygon, chart currency, regional coverage seams, owner Safari visual parity and reuse/attribution remain unverified; no MBTiles HTTP tile server exists and no NOAA binary committed.

**Language and safety:** Go only; no Python. Read-only Go auditor optionally uses system `sqlite3 -readonly` and reports NOAA timeouts without terminating Terminal. `*.mbtiles` ignored. No changes to auth, Homepage accounts, personal/Guest Save/Discard or private data, NOAA wind or predicted-current routes, production running services, Cloudflare Tunnel, listeners, launchers, persistent storage, deployment, build counters, tags, or versions. Owner-local unpushed work, browser and account state not directly accessible; do not overwrite. Owner previously reported successful clean-tree pull/build; new head not locally tested yet.

**Policy/docs:** Current merged Homepage policy v1.1.0 read; SailingGo AGENTS, README, ARCHITECTURE, DEVELOPMENT, CURRENT_WORK reviewed. ARCHITECTURE, DEVELOPMENT, CURRENT_WORK updated for the timer correction; no README or AGENTS changes necessary.

**Verification:** Require exact-head GitHub Actions Go CI including Node regression. Owner Mac build, live Safari, NOAA host behavior and Cloudflare status remain unverified for latest commit. Build script contains Go root tests and build. CI cannot validate live network or browser visibility.

**Approval boundary:** PR stays draft. No merge, issue close, branch deletion, launch/restart, deployment, release or tag authorized. Owner controls `NEXT` / `FINISH`.

**Owner Mac handoff (only after confirmed exact-head CI; stop for dirty tree, do not restart anything):**
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
