# CURRENT_WORK — SailingGo Issue #39 / PR #40 (2026-11-22)

## Status and scope

Issue [#39](https://github.com/richard-mauri/pittsburg-saildata/issues/39), [draft PR #40](https://github.com/richard-mauri/pittsburg-saildata/pull/40), branch `charts/39-clean-wms-zoom12-15-samples`, base `main`. PR is **not merged**; no FINISH authorization. Confirm current GitHub head SHA and exact-head CI after any edit. This document is the **pre-merge handoff**; on FINISH, record merge/Issue/branch results rather than leaving the work labeled active. Prior merged Issue #37 / PR #38 is the baseline.

## Delivered implementation

The standalone Nautical Tile Manager supports bounded dry-run-first NOAA WMS layers 0–7 at native z12–15; opt-in `-disable-scamin` sends `HonorScamin=1` and creates separate v3 managed provenance, incompatible with v2/default NOAA imagery. Worker count defaults to 4 (allowed 1–8), with bounded fetches, atomic validated PNG writes, resumption, byte/tile limits and progress/ETA. Isolated WMS display/zoom and z15 seam diagnostics are included; they do not modify the runtime chart cache. Runtime Go chart delivery is read-only; SailingGo runs on the owner's Mac behind Cloudflare Tunnel, not Render.

## Owner acceptance and local data

Owner downloaded, inventoried and installed the **12,499** SCAMIN-disabled tiles for west -122.70, south 37.65, east -121.45, north 38.35: z12=176, z13=630, z14=2,378, z15=9,315; **75,173,427 PNG bytes**, about 104 MB on disk. The active `cache/nautical_tiles` has v3 `HonorScamin=1` provenance, and owner confirmed exactly 12,499 PNGs with no unexpected files or `*.bak` files. After explicitly approved cleanup, `cache/` contains only `marineplaces` and `nautical_tiles`; the old 99 MB rollback and named experiments were deleted. **Do not assume a rollback copy exists.** This is owner-reported Mac state, not verified by GitHub.

Owner's Safari screenshots confirm depth soundings at native z13, z14 and z15. Zoom 12 remains installed but owner considers it unusable. A horizontal greenish/yellow land-color boundary persists at the star's geographic location; cause unproven. Two sampled z15 four-256-versus-one-512 WMS comparisons were near-identical and did not establish tile requests as the cause. Do not claim the discontinuity resolved or the chart navigation-authoritative. No production restart or tunnel change was authorized.

## Verification and remaining authority

Earlier exact-head GitHub Actions **#539 passed** on `70cef23c828ba0785c1d9bf2b7c9911f27ab8765`; that is **not** evidence for later documentation commits. Verify new exact-head CI before owner handoff/FINISH. Local Mac/Safari sounding checks and cleanup were owner-reported; no full application regression suite, independent decoded-PNG integrity audit or live tunnel validation is claimed. Homepage merged-main `SUITE_DEVELOPMENT_POLICY.md` v1.1.0 governs. `AGENTS.md` unchanged; README user-facing, ARCHITECTURE contract, DEVELOPMENT procedures, CURRENT_WORK current status.

Next: verify latest PR head/base, checks, reviews, and doc consistency. Only owner `FINISH` authorizes merge, Issue closure and remote branch cleanup. No deployment, service/tunnel restart, semantic/build version change, tag or release authorized. Owner-local uncommitted/unpushed state is inaccessible through GitHub.
