# Current work — SailingGo

**Active:** [Issue #37](https://github.com/richard-mauri/pittsburg-saildata/issues/37) and one [draft PR #38](https://github.com/richard-mauri/pittsburg-saildata/pull/38), branch `charts/37-ncds-mbtiles-evaluation` targeting merged `main` `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47` (PR #36). No other chart PRs, merges or deployment.

**NEXT — Nautical selected but no detail/no warning (owner Safari Nov. 21, 2026):** Owner provided a Safari screenshot around Pittsburg at zoom 11: Nautical radio was checked; Street Map remained visible, no error notice. Existing fallback only detected all-tile error on a completed Leaflet load; no response or successful but transparent WMS images were not covered. The updated candidate adds a **12-second** response check when NOAA tile loading starts. Zero tile responses shows a **not responding / Street fallback / try later** message; successful tile responses retain the chart layer and show a **conditional** visual-content advisory (no claim that imagery is blank). Map status badge wraps instead of truncating. Switching map types cancels outstanding timer. Existing map preference, Street underlay, overlays and markers remain unchanged. CI-only Node regression exercises the real event handlers with a fake timer and verifies both paths. Safari/live NOAA acceptance remains outstanding.

**NOAA download and audit:** All exact owner curl/SQLite/Go evidence in Issue #37. NOAA landing page HTTP 200; chart ArcGIS metadata hosts timed out, direct directory listing HTTP 403. `ncds_19c.mbtiles` downloaded outside Git; 241,352,704 bytes; Last-Modified May 28, 2024; SHA-256 `6f887b5df127d142f6f6826f8968f41977e89417e2205fccd0cc7524fa26f096`; SQLite integrity `ok`, PNG overlay zoom 0–16. Pittsburg sample tiles z14/z15/z16 177/177/237 bytes, possibly blank but **pixels not inspected**; Delta z16 sample absent. NCDS package freshness, full geographic coverage, current neighboring datasets and public hosting attribution/reuse gates remain open. No MBTiles HTTP tile service or switcher implemented.

**Language / Terminal constraint:** Go only, no Python. Standalone read-only Go inspector invokes `sqlite3 -readonly`. Its NOAA timeouts now print nonfatal warnings and return to prompt; exit status is not a NOAA dataset-success signal. The owner confirmed the prompt returned.

**Data safety:** No authentication, Homepage storage/accounts, Guest/personal explicit Save Changes/Discard Changes, private settings or tokens, NOAA weather/current data, production processes, Cloudflare tunnels, launchers, deployment, tags, release or version/build-counter changes. Owner Mac dirty/unpushed work, browser state and server storage are inaccessible and were not inspected. `*.mbtiles` remains ignored.

**Policy/docs:** Homepage's full merged `SUITE_DEVELOPMENT_POLICY.md` v1.1.0 and SailingGo AGENTS, README, ARCHITECTURE, DEVELOPMENT, CURRENT_WORK reviewed. ARCHITECTURE and DEVELOPMENT updated for bounded WMS diagnostics and Safari acceptance; CURRENT_WORK refreshed. README and AGENTS unchanged; behavior remains local to Nautical visual status.

**Verification:** CI must pass on the exact newest PR head. Node tests simulate WMS tile response/error and timeout plus existing preference/radar fallback contracts; Go root and auditor tests also run in CI. Owner Safari and live NOAA outcomes are still unverified, even if CI succeeds.

**Authority:** This draft is neither merged nor deployed. `NEXT` for further development; only `FINISH` authorizes merge/Issue close/branch deletion. No restart, deployment, tag or release authorization.

**Mac handoff (stop if initial status shows uncommitted work; do not start/restart services):**
```sh
cd ~/projects/sailing-go
git status --short --branch
# Stop here if any uncommitted changes are shown.
git fetch origin
git checkout charts/37-ncds-mbtiles-evaluation
git pull --ff-only origin charts/37-ncds-mbtiles-evaluation
git status --short --branch
./devbuild.sh
./sailing-go -version
```
