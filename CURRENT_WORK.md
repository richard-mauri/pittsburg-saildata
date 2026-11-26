# CURRENT_WORK — SailingGo baseline

## Current baseline

As of 2026-11-26, **SailingGo v1.17.0 · Build v443** is the owner-confirmed running production version at https://weather.themauris.org. [PR #48](https://github.com/richard-mauri/pittsburg-saildata/pull/48) merged at `cfa44a71be36b4898782606c134dac51d5df3a38`; [Issue #45](https://github.com/richard-mauri/pittsburg-saildata/issues/45) closed and its development branch was verified deleted. The MINOR increment introduced the backward-compatible public street tile API. [Final PR CI](https://github.com/richard-mauri/pittsburg-saildata/actions/runs/45867717706) passed at `e7a9b9b6d6712aa7a5edc7c7c2c99c6187b0599e`; [merged main CI](https://github.com/richard-mauri/pittsburg-saildata/actions/runs/45867859032) passed at the runtime merge SHA above. Source, build lineage and deployed application are unchanged by documentation housekeeping.

## Implemented contract and limits

Public GET /api/maps/street/{z}/{x}/{y}.png, narrow auth exemption for exact numeric unescaped XYZ path; valid z0–19, validated 256px PNG <=1 MiB. Fixed allowlisted HTTPS OSM upstream, no redirects, retries or forwarded credentials. Memory LRU 512 tiles/32 MiB, HTTP validators/freshness/revalidation, no persistent tile cache or bulk acquisition. Atomic slot/token admission: 8 concurrent upstream slots, 8/s burst 16, 32 waiters, 4s shared deadline, 11s combined request context and 10s upstream client cap. Queue bounds are provisional, not measured latency optima. Diagnostic response headers identify cache, proxy and upstream results and admission time. Existing direct street UI, protected nautical/weather APIs, preferences, authentication, account data, cache and tunnel unchanged. Mock tests include 24-tile bursts and cancellation; see ARCHITECTURE.md and DEVELOPMENT.md.

## Production evidence and outstanding checks

Owner confirmed startup version v1.17.0/v443. On 2026-11-26 at 17:32 UTC, production HTTPS health returned 200 (DYNAMIC), a valid street tile returned 200 image/png (2729 bytes, ETag, Cloudflare HIT, Age 5682), invalid z20 returned 400 (BYPASS, proxy/invalid-coordinates-or-query), and unauthenticated wind-readings returned 401 (DYNAMIC). All four status checks passed. Cached successful PNG is not proof of fresh origin delivery by the new binary. Earlier interactive GPS Map testing confirmed improved tile rendering with v443. Fresh successful origin tile delivery, full authenticated SailingGo Safari regression and full iPhone/consumer acceptance have not been reported. Verify during ordinary navigation with caching enabled; no synthetic bursts, forced cache bypass or data clearing.

Dependent [GPS Map PR #47](https://github.com/richard-mauri/sailing-gps-map/pull/47) remains separate; owner chose production authenticated navigation acceptance rather than changing temporary preview CORS. Consumer promotion requires separate authorization. No configuration changes or further SailingGo deployment needed for documentation housekeeping.

## Operating constraints

Preserve Mac working tree, preferences, sessions, credentials, private counters, caches and configuration. Operational NOAA cache cache/nautical_tiles: owner-reported SCAMIN-disabled v3, 12,499 PNGs (z12=176, z13=630, z14=2,378, z15=9,315); 75,173,427 PNG bytes; bounds west -122.70 south 37.65 east -121.45 north 38.35. No old-cache rollback; horizontal land-color boundary unresolved, z12 limited, soundings z13–15 owner accepted. No tag/release, tunnel/configuration or data changes; deployment is separate from Git merge. No changes to dependent PR without owner authorization.

## Documentation-only housekeeping checkpoint

Owner NEXT authorized [Issue #49](https://github.com/richard-mauri/pittsburg-saildata/issues/49) on branch `docs/49-baseline-housekeeping` from runtime baseline `cfa44a71be36b4898782606c134dac51d5df3a38`. Reviewed current Homepage policy v1.3.0 and all five standard docs. Only CURRENT_WORK.md and DEVELOPMENT.md changed; other three remain current. No semantic version or build increment, rebuild, restart or deployment. See Issue #49 for linked draft PR and exact-head CI evidence; after review, explicit FINISH required. Owner Mac state unavailable; inspect clean tree and divergence before any checkout.
