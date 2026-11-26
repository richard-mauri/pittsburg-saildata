# CURRENT_WORK — SailingGo baseline housekeeping

## Current scope

Merged runtime baseline: protected main at `999a3041906c9d86112fddfe99777fe6061fde4a`, app v1.16.0 / build v439. PR [#40](https://github.com/richard-mauri/pittsburg-saildata/pull/40) is merged. Exact baseline Go CI [#551](https://github.com/richard-mauri/pittsburg-saildata/actions/runs/45181073371) passed. This is not candidate CI evidence.

Owner-authorized housekeeping closed Issues #32, #41 and #44 as not planned (not fixed), and abandoned draft PRs #42 and #43 without merge. The owner deleted obsolete remote branches, including the squash-integrated Issue #19 branch; GitHub confirmed only main remained before this cleanup branch. Owner reported clean synchronized main; current Mac state must be rechecked before checkout.

Active scope: documentation-only [Issue #46](https://github.com/richard-mauri/pittsburg-saildata/issues/46), branch `docs/46-baseline-cleanup`, targeting main. Reconcile AGENTS, README, ARCHITECTURE, DEVELOPMENT and CURRENT_WORK with the merged runtime; no code, version, cache or operational change. The draft PR and exact-head CI are the authoritative candidate records. Verify them before FINISH; do not infer candidate acceptance from this file.

Next runtime project after cleanup FINISH: [Issue #45](https://github.com/richard-mauri/pittsburg-saildata/issues/45), public street tile proxy; dependent [GPS Map Issue #46](https://github.com/richard-mauri/sailing-gps-map/issues/46). Not implemented or authorized by this documentation task.

## Operational constraints

SailingGo is Mac-hosted behind Cloudflare Tunnel, not Render. Production listener enforces Homepage authentication on nonpublic routes; explicit-save preferences preserve per-user and Guest isolation. Existing direct OSM street layer and authenticated local NOAA XYZ delivery remain unchanged.

Owner-reported NOAA cache: `cache/nautical_tiles`, SCAMIN-disabled v3 provenance, 12,499 PNGs (z12=176, z13=630, z14=2,378, z15=9,315), 75,173,427 PNG bytes; bounds west -122.70, south 37.65, east -121.45, north 38.35. Cache root also contains marineplaces. Old rollback and experiments deleted; **no retained old-cache rollback**. Owner Safari screenshots confirmed soundings z13–15; z12 limited and horizontal land-color boundary unresolved. No independent integrity/live verification in this task.

## Verification and approval

Current Homepage merged-main policy v1.2.2 retrieved; follow its live authority, not this snapshot. Documentation candidate requires content/link review and exact-head Go CI. No Mac build, Safari test, live tunnel test or deployment required for Markdown-only edits. Owner-local files inaccessible remotely; preserve all local changes. No merge, issue closure, branch deletion, version change, deployment, restart, tag or release authorized except subsequent scoped FINISH for documentation closeout.
