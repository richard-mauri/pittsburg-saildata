# SailingGo — app-specific development and validation

Read [AGENTS.md](AGENTS.md) and [Homepage's authoritative suite development policy](https://github.com/richard-mauri/homepage/blob/main/SUITE_DEVELOPMENT_POLICY.md). Shared Issues, branch/PR rules, six-part handoff, FINISH and cleanup are exclusively Homepage policy. These sections retain SailingGo's unique CI, local divergence, build and Mac/Safari requirements.

## GitHub Actions and limitations

For suite UI changes, compare the four HTML pages at identical Safari widths with the shared Homepage banner and controls in companion apps. Verify matching responsive gutters, one local portrait and return link, verified identity without suffix, and separate preference status. Preserve NOAA, map and preferences behavior.

SailingGo has its own `.github/workflows/go-ci.yml`: non-blocking formatting report, root Go tests and a root build on PRs targeting `main`. Verify its actual outcome on the exact PR head. The root `go.mod` now declares Go 1.22 because Homepage's pinned shared UI module requires it; CI uses Go 1.23. The nested `ui/` module remains independent and requires separate testing when edited. The shared Homepage policy does not replace app-specific CI.

A CI pass covers only performed jobs; it cannot verify local server startup, Safari, NOAA's current responses or Cloudflare Tunnel. Documentation-only changes require Markdown/content/link/path review and applicable CI, **not** a Mac build/restart.

## Complete owner-side Mac verification for runtime PRs

After checking for uncommitted local changes, substitute the **actual PR branch name** in the following and provide a copy-and-pasteable block in every runtime handoff:

```sh
cd ~/projects/sailing-go
git status --short --branch
# Stop if uncommitted changes are shown.
git fetch origin
git checkout ACTUAL-PR-BRANCH
git pull --ff-only origin ACTUAL-PR-BRANCH
./devbuild.sh
./sailing-go -version
```

`./devbuild.sh` is the **single** owner build/test command: it tests the root application package and builds the root binary. Do not separately request `go test` or `go build` from the owner. Review changed Go files for formatting; the CI formatting report is informational. Run relevant checks from `ui/` if that module changed. Do not start a competing listener or alter the local Cloudflare launcher. The production launcher `./sailgo.sh` may be run **only with separate owner authorization**, since it restarts the app and tunnel.

## Safari and operational acceptance

After the owner intentionally installs/launches a candidate, verify Conditions Now and Planning and Details, History time windows including 1h and 24h, mph/knots switching, wind recency and station overrides, predicted-current semantics, map click versus pan/zoom, overlays, and text/JSON endpoints. The `/wind-readings?station=PSBC1&wind_hours=4&wind_unit=mph` response should maintain its documented schema when upstream NOAA data are available. Record pass/fail/not-performed for every claimed check. Browser and live-weather results are separate from Linux CI.

Follow the current Homepage policy's fresh-status/diff/staging audit before commits. For runtime source regenerations follow the documented buildVersion convention; semantic appVersion changes occur at authorized FINISH. Documentation-only work changes neither version nor build counter. Never include local binaries, credentials, private counters, archives or tunnel configuration in the PR.

## NOAA nautical chart manager — current operating procedure

The standalone `cmd/nauticaltilemanager` plans NOAA WMS layers 0–7 at native zooms 12–15. A dry run performs no network download or directory writes. The opt-in `-disable-scamin` requests `HonorScamin=1` and writes v3 provenance; v2/default datasets are incompatible. `-workers` defaults to 4 (range 1–8). Downloads validate PNGs, enforce tile/byte caps and atomic writes, and resume only against compatible marked roots. Never run acquisition directly into the operational `cache/nautical_tiles` or remove it incidentally. For any future acquisition, use a **new** Git-ignored directory, dry-run and inspect inventory first; production replacement needs separate owner authorization.

For reference, the completed regional acquisition used bounds `-west -122.70 -south 37.65 -east -121.45 -north 38.35 -min-zoom 12 -max-zoom 15`, `-source noaa -disable-scamin -workers 4`, `-max-tiles 13000 -max-bytes 1073741824 -timeout 45s -retries 2`. The owner's successful acquisition produced 12,499 PNGs (z12=176, z13=630, z14=2,378, z15=9,315), 75,173,427 PNG bytes, then the owner installed the verified dataset as `cache/nautical_tiles`. This is **completed history**, not a pending acquisition or swap instruction. The previous 99 MB rollback and all named chart experiment directories were permanently removed after owner acceptance. Final cache root: `cache/marineplaces`, `cache/nautical_tiles`. No `*.bak` files were found. **There is no retained old-cache rollback.**

Owner Safari acceptance established visible depth soundings at zooms 13–15. Zoom 12 remains installed but has limited practical usefulness. A geographically aligned horizontal land-color boundary remains unresolved; do not infer its cause from tile seams or browser cache alone. Two isolated `-seam-study` four-tile versus single-512 NOAA WMS comparisons showed near-identical rendering at the sampled locations, not a conclusive boundary diagnosis. These temporary outputs were deleted; repeat diagnostics only into a fresh isolated directory with explicit owner approval. CI does not validate live NOAA, Mac Safari or Cloudflare Tunnel behavior. Do not restart `sailgo.sh`, alter production cache, merge, tag or release without separate approval.

## Pre-merge documentation and verification

Review all five repository Markdown documents and the current merged Homepage policy. Keep README user-facing, ARCHITECTURE implementation-focused, DEVELOPMENT procedural and CURRENT_WORK a single factual handoff. Verify GitHub CI against the **latest** PR head after documentation edits. Before FINISH, check PR base/head/reviews, Issue status and outstanding owner-local divergence. For authentication changes verify signed-out data denial, verified personal and Guest handoff, expired/revoked credentials, 401 vs 503, same-origin cookie writes, and separate personal/Guest preference storage; never copy tokens into diagnostics. The `./devbuild.sh` workflow above is for runtime changes; documentation-only edits require content/link/path review and GitHub CI, not a service restart.

## Public street tile proxy validation

Mock HTTPS/HTTP tests cover public versus protected routing, invalid methods/coordinates, headers and credential non-forwarding, PNG and response bounds, conditional requests and cache policy, timeouts, status translation and rate/concurrency limits. No live OSM requests in tests. The independent `SAILING_STREET_TILE_UPSTREAM` operator configuration is unset by default or exactly `https://tile.openstreetmap.org`; anything else fails closed. No changes to launcher, tunnel, NOAA cache or UI. No CORS authorization required for ordinary Leaflet image tiles; if a consumer needs canvas or fetch, review narrowly in its own scope.

After an authorized deployment, inspect a single interactive tile via HTTPS (not a bulk test): `curl -sS -D - -o /tmp/sailing-street-tile.png -H 'Referer: https://weather.themauris.org/' https://weather.themauris.org/api/maps/street/3/2/4.png`; check image/png, cache/validator headers and actual PNG, then conditionally request using observed ETag. Verify invalid coordinates return 400 and protected JSON/nautical routes still return 401 without credentials. Confirm the running version before attributing live results to a deployment. Launch/restart requires separate authorization.


### Production and interactive tile verification

Use normal interactive browsing, with caching enabled and no synthetic live bursts. Inspect Safari Web Inspector → Network for fresh failed PNG requests and capture HTTP status, X-Sailing-Street-Source, X-Sailing-Street-Result, X-Sailing-Street-Admission-Ms, Retry-After and CF-Cache-Status. A missing diagnostic header may indicate an older running binary, an intermediary response or stripping; a cache HIT may retain earlier origin headers. Local queue-full/queue-timeout/rate-wait-timeout, duplicate-inflight and upstream-429 are distinct failure paths; see ARCHITECTURE.md. Mock tests exercise 24 distinct tiles beyond the initial token allowance, queue capacity and expiration, cancellation, cache recheck and unchanged upstream limits. Admission plus upstream context is 11 seconds, with up to 4 seconds shared admission and 10 seconds upstream client cap. These bounds are not measured performance optima.

After merging and separately authorized owner launch, verify the actual binary version, local/public health and a normal single tile GET, content and validators; invalid coordinates must return 400 and protected API routes 401 without credentials. An edge cache HIT does not establish that the new origin binary served a request. Use an ordinarily viewed uncached tile if fresh-origin evidence is needed, with normal caching and no query parameters or bypass headers. Recheck ordinary GPS Map panning and zooming and existing authenticated SailingGo functionality. No private tokens, logs or data in reports. Consumer promotion is separately authorized and follows verification of the deployed provider.

## POI API verification

See [POI API](docs/POI_API.md) for owner opt-in in-process real-generation HTTP diagnostic after the normal `./devbuild.sh`. This is not service installation or launch. `SAILING_POI_GENERATION` is operator-only and defaults disabled. Separate approval for any production generation configuration or restart. No automatic data refresh.

The separately owner-approved disposable Mac listener acceptance command is documented in [POI API](docs/POI_API.md#isolated-listener-acceptance-owner-next). It uses the verified freshly built executable and never the production launcher or tunnel. No production burst tests.

Website enrichment: see docs/POI_API.md. Run go test -count=1 ./cmd/poimanager ./internal/poiweb, then go run ./cmd/poimanager websites on the owner Mac with the existing Docker image and pinned local Parquet sources. No acquisition; existing generation unchanged. Report only aggregate counts and SHA-256 before enabling a local candidate. No production activation.

Marine local acceptance: use paired GPS Map preview harness with explicit path to the existing tracked assets/marine_places.json, no production launcher. Build once via ./devbuild.sh. No generation rebuild, no Docker or downloads.
