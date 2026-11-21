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
