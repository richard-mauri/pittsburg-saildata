# Current work — SailingGo

## Scope and authoritative evidence

Issue [#37](https://github.com/richard-mauri/pittsburg-saildata/issues/37), existing draft [PR #38](https://github.com/richard-mauri/pittsburg-saildata/pull/38) on `charts/37-ncds-mbtiles-evaluation`, base `main` at `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47` (reverify before handoff). Current PR head and CI must be read from GitHub, not inferred from this file. Homepage `main` SUITE_DEVELOPMENT_POLICY.md v1.1.0 governs; AGENTS, README, ARCHITECTURE, DEVELOPMENT and this document reviewed.

**Verified owner Mac/Safari 2026-11-21 10:49 PST:** Safari `/api/charts/tiles/10/165/394.png` received SailingGo-origin HTTP 503, `X-Sailing-Chart-Source: unavailable`, `X-Sailing-Chart-Result: noaa-timeout`, `Retry-After: 30`, no upstream HTTP status. Go log recorded actual outbound NOAA WMS attempts with `status=0` and full 5000ms timeouts for four requests, plus six 8–38ms context-ended attempts. Browser→Go and Go→NOAA attempt path **is proved**; NOAA returned no HTTP response and no successful tiles were cached. Root cause and chart availability are unknown. See Issue #37 **VERIFIED OWNER MAC + SAFARI LIVE DIAGNOSIS** for full redacted evidence. Separate Leaflet `_fadeAnimated` TypeError and activity endpoint 503s are not addressed.

## Current work — Stage A offline validation and Safari inspection refinement

Added standalone Go fixture generator `cmd/mocknauticaltiles/main.go` and root-package integration test `nautical_mock_tiles_test.go`. Generator produces fictional, prominently labeled **MOCK TEST DATA / NOT FOR NAVIGATION** complete 256×256 PNGs using Leaflet/XYZ Web Mercator coordinates in 3×3 neighborhoods around Pittsburg and Delta at z10–z12. Requires a **new output directory outside Git/live cache**, rejects existing paths, does not fetch NOAA, and makes no process/config/account changes. Root Go test runs it in a disposable directory and routes tiles through the **existing** Go chart handler with a localhost upstream sentinel. It asserts opaque valid PNG, exact response bytes, GET/HEAD `cache-hit` headers, no upstream request, and refusal to overwrite a fixture directory. No production chart proxy code changed, no NOAA downloads, no MBTiles import, no source selection. Stage B historical 2024 `ncds_19c.mbtiles` extraction remains **not implemented**.

**Files affected:** `cmd/mocknauticaltiles/main.go`, `nautical_mock_tiles_test.go`, `README.md`, `ARCHITECTURE.md`, `DEVELOPMENT.md`, `CURRENT_WORK.md`. `AGENTS.md` reviewed, unchanged. The offline Stage A test is included in existing root Go tests/CI; exact-head GitHub Actions must pass before owner Mac/Safari acceptance handoff. No claimed Safari rendering, no claim NOAA online or current/chart coverage, and no production data manipulation. Owner Mac working-tree or unpushed state is inaccessible from the GitHub connector.

## NEXT: Safari finding and loopback mock inspection

Owner Safari screenshot verified real rendering of labeled 256×256 XYZ mock tiles at z10 around Pittsburg. The browser then showed a persistent success advisory and, on missing mock tiles while zooming/panning, a 12-second progress/automatic switch to Street that obstructed inspection. The owner requested a correction via NEXT.

`web/templates/sailing.html` now permits **explicit test-only browser inspection** only when both the URL includes `nautical_mock_test=1` and the browser is visiting `localhost` or `127.0.0.1`. In this case, missing fixture tiles no longer start an availability timer, show NOAA progress/failure panels or automatically select Street. Street tiles remain underneath missing fixture cells. Normal public operation cannot enable the flag via URL: it retains all existing Loading/Cancel, 12-second NOAA failure, Street staged-radio, Failure/Close and auto-dismiss. A successful nautical image response now clears the generic advisory so the map is not covered indefinitely. The existing Go chart cache/WMS proxy and all account storage/Save/Discard logic remain unchanged.

CI-only Node regression covers mock opt-in guards, simulated missing mock images, absence of false fallback or obstruction, and normal NOAA response cases. Documentation in README, ARCHITECTURE, DEVELOPMENT, CURRENT_WORK updated; AGENTS reviewed unchanged. Exact latest PR head/CI must be checked live after these changes. Previous owner Safari success is **only** z10 mock rendering; visual zoom 10–12 alignment in a separately authorized loopback server remains unverified. The owner launched mock directory under a public Cloudflare route; **restore production to normal cache before continued testing**, and do not assume `unset` changes an already running process.

## Constraints and owner handoff

Preserve production mux, Go NOAA WMS proxy/cache/cooldown, existing Leaflet Nautical Loading/Cancel/Failure/Close/Street behavior, per-user and Guest Save/Discard, authentication, NOAA weather/wind/current data, overlays and reports, services/tunnels, private data and version/build counters. No new changes to live systems by this development session. **No FINISH**, merge, Issue closure, branch deletion, process/tunnel restart, deployment, release or tag authorized.

Safe owner Mac actions, stopping immediately if checkout has local changes:

```sh
cd ~/projects/sailing-go
git status --short --branch
# STOP if any uncommitted or untracked work appears.
git fetch origin
git checkout charts/37-ncds-mbtiles-evaluation
git pull --ff-only origin charts/37-ncds-mbtiles-evaluation
git status --short --branch
./devbuild.sh
./sailing-go -version
```

For isolated fixture generation after the ordinary build (no service restart): `go run ./cmd/mocknauticaltiles -dir "$HOME/Downloads/sailing-nautical-mock-$(date +%Y%m%d-%H%M%S)"`. To inspect tiles through Safari, a **separately owner-authorized test instance** pointed at the fixture directory is required. Never repoint or restart the production service/tunnel or use these fictional tiles as navigational data. Check actual browser image alignment at z10, z11, z12 and Save/Discard separately; mark this unverified until owner performs it.

## Owner NEXT — single-panel mock testing usability / wider zoom coverage (2026-11-21)

Owner reported that mock tiles appeared **before** the later progress panel, followed by a separate-seeming failure notice and automatic Street selection; zooming/panning was unusable for Stage A testing. Root cause: Leaflet could paint fast cached PNGs before a separate status event, and sparse fixture coverage triggered NOAA fallback when the loopback-only inspection mode was not active. A public `weather.themauris.org` URL must **not** bypass normal production safety.

In the **same existing draft PR #38**, `web/templates/sailing.html` now starts the existing **single** progress/Cancel panel synchronously before adding Nautical, hides the nautical Leaflet container until the request cycle settles, then reveals tiles and closes the status on success. On ordinary NOAA all-failure, the same panel transitions to error/Close and the existing staged Street fallback with eight-second dismiss; timeout remains 12 seconds. Explicit local `nautical_mock_test=1` on loopback hosts uses the same single panel, a 2.5-second reveal deadline, and **does not** switch to Street on missing fictional tiles. Real Go NOAA proxy, cache, wind/current data, auth, personal/Guest explicit Save/Discard, overlays, tunnels and version counters unchanged.

`cmd/mocknauticaltiles/main.go` now generates a **new** bounded tile set over lon [-121.98,-121.48], lat [37.96,38.16] at z9–z14 (hard ceiling 1,000 PNGs). `nautical_mock_tiles_test.go` checks endpoint cache hits at z9 and z14 in addition to earlier points; `scripts/test-explicit-save.cjs` checks loading-before-reveal, common status panel, mock missing-cell retention, and ordinary error/Close/Street behaviors. Old z10–z12 generated mock files remain unchanged and are inadequate for the expanded acceptance. **Actual owner Safari z9–z14 verification remains outstanding.**

The current exact branch head and GitHub Actions result must be rechecked from GitHub after the last documentation commit. No FINISH, merge, Issue closure, remote branch deletion, deployment, owner Mac restart, production cache mutation, tag, version or release is authorized. Owner-local dirty/unpushed tree remains inaccessible to this GitHub-only session. The owner must restore any public mock cache with separately approved process control before further Safari testing.

