# Issue #41 — Phase 0: offline chart installer design and isolated POC

Status: **experimental**. This does not modify the running SailingGo service, install charts on iPhone, provide offline support, or process GPS. The Mac-hosted HTTPS application remains unchanged. No on-water use.

## Architecture decision candidates

The Go server should expose an authenticated, bounded, read-only chart inventory and package service backed by **existing** NOAA XYZ PNG tiles. Proposed production routes:

- `GET /api/offline-charts/manifest?west=…&south=…&east=…&north=…&z=14` — exact available/missing tile count, bytes, tile paths, dataset version and bounds.
- `GET /api/offline-charts/package?…` — future bounded streaming ZIP *or* individually requested tiles. Compare Safari memory, resume and failure recovery before choosing.

The iPhone client should display a mobile-only install screen, select a small rectangle and one native zoom (default 14), verify full coverage, download into browser-managed storage, and show version/bytes/progress/remove. A service worker must make a **minimal** offline entry point, Leaflet resources, styles, tracking code and chart tiles available without Homepage/network access. It must not cache credentials, protected API responses or other users' preferences. Authentication and offline trust model require review before integration.

Track mode is client-side: explicit geolocation consent, Leaflet boat marker/follow, SOG/COG and depths first, then timestamped local trip storage and gaps on Safari suspension. **COG is not boat heading**. Never upload GPS tracks by default. Offline tiles are a supplemental chart snapshot, not navigation-authoritative.

## Included proof of concept

`cmd/offlinechartpoc/main.go` is an **independent loopback-only HTTP manifest server**. It never writes, downloads, generates, or modifies chart tiles. It accepts z13–15, checks bounded geographic selections (at most 256 cells), inventories PNG-signature files and reports available bytes/missing cells. It exposes individual read-only tile URLs only for local API inspection; it is not integrated into SailingGo's authentication or server routes. It is **not** suitable for public exposure. The API's package endpoint, signed manifest, chart provenance, installer, offline service worker, and GPS features are intentionally **not implemented**.

`cmd/offlinechartpoc/installer-preview.html` is a mobile-width **UI design prototype** for previewing tile availability. The Install button is intentionally disabled. The page must be served on the same origin as the POC endpoint to work; the POC server does not currently serve it. This avoids suggesting that a local HTTP demo can test iPhone geolocation or offline service workers. It is available for code/UI review.

### Optional isolated local experiment (never point at the live cache)

Copy a **small** set of test PNG tiles into a separate temporary XYZ directory yourself, then, from a clean Mac checkout of this PR branch:

```sh
cd ~/projects/sailing-go
git status --short --branch
# Stop if uncommitted changes appear.
git fetch origin
git checkout feature/41-offline-chart-poc
git pull --ff-only origin feature/41-offline-chart-poc
./devbuild.sh
# Optional POC only, in a separate terminal; use an isolated test tile directory:
go run ./cmd/offlinechartpoc -root /path/to/isolated/test-tiles -listen 127.0.0.1:8099
```

Example API request:
`http://127.0.0.1:8099/api/offline-charts/manifest?west=-121.99&south=38.02&east=-121.90&north=38.08&z=14`

Do not run `sailgo.sh` or change the live Cloudflare Tunnel as part of this experiment.

## Next development gates

1. Inspect the actual root chart handler and auth middleware, map template/script bindings, CSP, cache headers and Homepage offline implications. Existing `/api/charts/tiles/` is read-only and currently returns `Cache-Control: no-store`; an offline installer must explicitly handle that policy rather than assuming normal browser caching.
2. Validate exact tile-range math at boundary conditions and PNG integrity/provenance; choose size limits and missing-tile behavior.
3. Build production-grade API with auth, strict quotas and cancellation, and unit/integration tests.
4. Implement service worker, manifest-driven transactional installer and offline entry point; test on iPhone Safari over the existing HTTPS Cloudflare route **only after separate owner approval to run the candidate**.
5. Add foreground GPS/Leaflet tracking independently of the selected weather-report location. Test Airplane Mode, reload, storage eviction, lock/unlock gaps and offline GPS on-device.

## Safety and acceptance

The operational Mac `cache/nautical_tiles` is not a test target. Never mutate it, reacquire NOAA tiles, restart the live service or tunnel, or weaken Homepage account isolation. No CI or iPhone test can be claimed until observed. No merge without explicit FINISH.
