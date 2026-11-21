# Current work — SailingGo

## Scope and authoritative evidence

Issue [#37](https://github.com/richard-mauri/pittsburg-saildata/issues/37), existing draft [PR #38](https://github.com/richard-mauri/pittsburg-saildata/pull/38) on `charts/37-ncds-mbtiles-evaluation`, base `main` at `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47` (reverify before handoff). Current PR head and CI must be read from GitHub, not inferred from this file. Homepage `main` SUITE_DEVELOPMENT_POLICY.md v1.1.0 governs; AGENTS, README, ARCHITECTURE, DEVELOPMENT and this document reviewed.

**Verified owner Mac/Safari 2026-11-21 10:49 PST:** Safari `/api/charts/tiles/10/165/394.png` received SailingGo-origin HTTP 503, `X-Sailing-Chart-Source: unavailable`, `X-Sailing-Chart-Result: noaa-timeout`, `Retry-After: 30`, no upstream HTTP status. Go log recorded actual outbound NOAA WMS attempts with `status=0` and full 5000ms timeouts for four requests, plus six 8–38ms context-ended attempts. Browser→Go and Go→NOAA attempt path **is proved**; NOAA returned no HTTP response and no successful tiles were cached. Root cause and chart availability are unknown. See Issue #37 **VERIFIED OWNER MAC + SAFARI LIVE DIAGNOSIS** for full redacted evidence. Separate Leaflet `_fadeAnimated` TypeError and activity endpoint 503s are not addressed.

## Current owner NEXT phase — Stage A deterministic offline validation

Added standalone Go fixture generator `cmd/mocknauticaltiles/main.go` and root-package integration test `nautical_mock_tiles_test.go`. Generator produces fictional, prominently labeled **MOCK TEST DATA / NOT FOR NAVIGATION** complete 256×256 PNGs using Leaflet/XYZ Web Mercator coordinates in 3×3 neighborhoods around Pittsburg and Delta at z10–z12. Requires a **new output directory outside Git/live cache**, rejects existing paths, does not fetch NOAA, and makes no process/config/account changes. Root Go test runs it in a disposable directory and routes tiles through the **existing** Go chart handler with a localhost upstream sentinel. It asserts opaque valid PNG, exact response bytes, GET/HEAD `cache-hit` headers, no upstream request, and refusal to overwrite a fixture directory. No production chart proxy code changed, no NOAA downloads, no MBTiles import, no source selection. Stage B historical 2024 `ncds_19c.mbtiles` extraction remains **not implemented**.

**Files affected:** `cmd/mocknauticaltiles/main.go`, `nautical_mock_tiles_test.go`, `README.md`, `ARCHITECTURE.md`, `DEVELOPMENT.md`, `CURRENT_WORK.md`. `AGENTS.md` reviewed, unchanged. The offline Stage A test is included in existing root Go tests/CI; exact-head GitHub Actions must pass before owner Mac/Safari acceptance handoff. No claimed Safari rendering, no claim NOAA online or current/chart coverage, and no production data manipulation. Owner Mac working-tree or unpushed state is inaccessible from the GitHub connector.

## Constraints and owner handoff

Preserve production mux, Go NOAA WMS proxy/cache/cooldown, existing Leaflet Nautical Loading/Cancel/Failure/Close/Street behavior, per-user and Guest Save/Discard, authentication, NOAA weather/wind/current data, overlays and reports, services/tunnels, private data and version/build counters. No changes to live systems. **No FINISH**, merge, Issue closure, branch deletion, process/tunnel restart, deployment, release or tag authorized.

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
