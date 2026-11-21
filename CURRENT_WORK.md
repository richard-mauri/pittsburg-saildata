# Current work — SailingGo

**Active verified baseline:** [Issue #37](https://github.com/richard-mauri/pittsburg-saildata/issues/37), draft [PR #38](https://github.com/richard-mauri/pittsburg-saildata/pull/38), branch `charts/37-ncds-mbtiles-evaluation` against merged `main` `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47`. Code head `0047c6c511be909da92bb1d062ce91a7dfe4518e` passed GitHub Actions run `44934965785`; after this handoff document commit, CI must be checked again on the new exact PR head. PR stays draft.

**Owner NEXT phase — implement Go NOAA fallback and self-populating cache:** Nautical browser layer always calls SailingGo's `/api/charts/tiles/{z}/{x}/{y}.png`, never NOAA directly. `nautical_tiles.go` now serves validated 256×256 PNGs from operator-provided `SAILING_NAUTICAL_TILE_DIR` or default `os.UserCacheDir()/sailing-go/nautical-tiles`. On cache miss, Go translates the XYZ tile into an EPSG:3857 WMS 1.3.0 GetMap request using the pre-existing NOAA NCDS MaritimeChartService endpoint, with a five-second timeout, maximum four concurrent upstream calls, deduplicated same-tile misses, and 30-second cooldown after failure. On valid NOAA response, the tile is atomically saved as `z/x/y.png`; later requests hit disk without NOAA. Invalid/upstream errors yield HTTP 503 and trigger the browser's existing Street fallback / Loading+Cancel / Failure+Close panel, including 12-second timeout and eight-second auto-dismiss. No automatic browser retries or direct NOAA WMS browser traffic.

**Testing:** Existing Go handler tests updated for true PNG fixtures. New offline httptest WMS tests validate Web Mercator bbox orientation, full PNG decode, disk persistence/offline hit, malformed upstream rejection, concurrent same-tile deduplication, four-call concurrency ceiling, request timeout, circuit-breaker cooldown and continued cached hits during an outage. Existing Node UI/prefs regression checks the same-origin endpoint. Exact-head GitHub Actions must pass; live NOAA, Safari, cache content/coverage and owner Mac are not verified. Standalone audit tooling unchanged.

**Data/operations:** No NOAA bulk download, chart refresh/eviction, MBTiles SQLite reading, source update checker, automatic chart freshness determination or reliable offline corridor coverage yet. Owner's old `ncds_19c.mbtiles` remains outside Git and is not automatically served. Caching only begins on a successful real NOAA tile response after running the new binary; cached images may become stale and are not certified navigation data. NOAA watcher is a separate automation. No personal/Guest settings writes, account changes, credentials, subscription data, tunnel/listener/launcher changes, version/build-counter bump, merge or deployment. Owner-local dirty/unpushed source cannot be inspected by the GitHub connector.

**Policy/docs:** Homepage merged-main suite policy v1.1.0 and all five SailingGo documents read. README, ARCHITECTURE, DEVELOPMENT, CURRENT_WORK updated to describe this phase; AGENTS unchanged.

**Approval:** No FINISH, merge, issue closure, remote branch deletion, launch/restart, deployment, tag, semantic version or release. Owner must accept Safari and NOAA results.

**Mac commands, stop if tree dirty:**
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
Owner separately chooses whether/when to launch or restart the service; running processes and Terminal were not touched.
