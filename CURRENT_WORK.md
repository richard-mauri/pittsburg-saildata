# CURRENT_WORK — SailingGo Issue #45

## Active scope and authority

[Issue #45](https://github.com/richard-mauri/pittsburg-saildata/issues/45): owner explicitly resumed previously authorized NEXT / Phase 1 on 2026-11-26. Branch [feat/45-street-tile-proxy](https://github.com/richard-mauri/pittsburg-saildata/tree/feat/45-street-tile-proxy), based on verified protected main `3b6a5c75e0218a41ff6bf857251261d78fe33b91` (PR #47 merged). Only main existed before this branch; no open PR. [Draft PR #48](https://github.com/richard-mauri/pittsburg-saildata/pull/48) targets main; Issue bootstrap links updated. App baseline v1.16.0 / build v439. Current Homepage main suite policy v1.3.0 and five project documents reviewed. Existing AGENTS links to live policy; separate adoption work not authorized.

Phase 1: isolated public GET XYZ street tile API, OSM policy compliance, narrow route authorization, bounded resources and mock upstream/auth regressions. No existing UI, protected NOAA tiles, weather, session, account, deployment or tunnel changes. No persistent tile store initially; resolve policy requirements before implementation. Dependent GPS Map Issue #46 is separate. Do not revive #41 / PRs #42–43.

## Progress and next action

Branch and draft PR created; initial checkpoint b2c934502faa35dfe106c5348d16a2d0724b9ba4. OSM policy retrieved on 2026-11-26 (https://operations.osmfoundation.org/policies/tiles/): honor upstream cache headers and conditional requests, identify proxy, preserve accurate web Referer, visible attribution and interactive-only use; no bulk/offline prefetch. Prior authorized bounded in-memory cache resolves the no-persistent-store requirement. Inspected production mux, auth guard, nautical handler, Go module, devbuild and CI. Next: Implement only compliant scope, update affected docs, commit tests and checkpoint, verify actual diff and exact latest PR head CI; terminal CI evidence belongs in PR metadata. Owner Mac/browser/HTTPS acceptance remains outstanding. No new NEXT needed for authorized Phase 1.

## Safety and operating constraints

Mac-hosted behind Cloudflare Tunnel; owner-local state inaccessible, verify clean synchronized checkout before local work. Preserve preferences, sessions, credentials, caches, private counters and configuration. Existing direct OSM street UI and protected local NOAA XYZ delivery unchanged. Operational NOAA cache `cache/nautical_tiles`: owner-reported SCAMIN-disabled v3, 12,499 PNGs (z12=176, z13=630, z14=2,378, z15=9,315); 75,173,427 PNG bytes. No old-cache rollback; horizontal land-color boundary unresolved, z12 limited, soundings z13–15 owner accepted. No merge, closure, semantic version increment, release/tag, restart or deployment authorized. 
