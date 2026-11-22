# CURRENT_WORK — SailingGo Issue #39 / Draft PR #40 (2026-11-22)

## Baseline and active scope

The prior merged Issue #37 / PR #38 baseline is main commit 299cb7c3d6e12eece243f6507ccfd5a6fecb4d1b. Active development is Issue #39, draft PR #40, branch charts/39-clean-wms-zoom12-15-samples, targeting main. Verify the current GitHub base/head and exact-head CI again before any merge; this file is not a live GitHub status feed. No FINISH authorization.

SailingGo runs on the owner's Mac behind **Cloudflare Tunnel**, not Render. sailgo.sh configures the local cache/nautical_tiles directory; the same-origin Go XYZ tile endpoint reads local PNGs. Browser Nautical native zoom is **12–15**; radar native zoom is unchanged. The runtime chart handler does not bulk-download NOAA imagery.

## Implemented and tested in the draft PR

Standalone cmd/nauticaltilemanager accepts NOAA WMS layers 0–7 at native z12–15 with dry-run default, cumulative tile/byte caps, 256x256 PNG validation, atomic writes and resumption. An isolated display study established that integer Display_params HonorScamin=1 disables SCAMIN filtering and restores numerous numerical soundings in owner-reviewed Suisun Bay z13 and z15 image comparisons. The opt-in -disable-scamin acquisition flag records a distinct **v3** provenance marker; the manager rejects mixing it with v2/default NOAA managed imagery.

Tile acquisition prints progress, percent, elapsed time and ETA. Bounded parallel -workers defaults to 4 and permits 1–8; fetch workers run concurrently and a single collector serializes byte-cap accounting and atomic commits. CI #527 passed on commit 0296e19d3f911d4591288d97be29b31079410498 **before the subsequent documentation edits**. Exact-head CI for the current documentation commits must be verified anew.

## Owner Mac acceptance and current acquisition

Owner confirmed the 12-tile SCAMIN-disabled Suisun Bay pilot completed: z13=2, z14=4, z15=6, total 458,796 PNG bytes. Owner manually substituted these 12 files into cache/nautical_tiles while renaming originals to .png.bak. A SHA-256 check confirmed the substituted 15/5273/12631.png matches the pilot source and differs from the original .png.bak. This is owner-reported local state; do not run managed acquisition into that directory or remove backups yet.

Owner approved complete SCAMIN-disabled regional regeneration over west -122.70, south 37.65, east -121.45, north 38.35, native z12–15. Dry-run output confirmed **12,499** tiles: z12=176, z13=630, z14=2,378, z15=9,315, with zero existing and all 12,499 new in cache/noaa-scamin-regional. Owner **started** the download using -workers 4, -max-tiles 13000, -max-bytes 1073741824, -timeout 45s, -retries 2 and -disable-scamin. **No completion output or final byte total has been received yet.** Do not claim full cache acceptance or migration.

## Next steps and safety

Wait for the owner's final download report. Validate complete inventory, exact counts, v3 provenance, decoded PNG integrity, representative chart imagery and tile seams before requesting owner approval to swap the operational cache. Preserve the old directory as a whole-directory rollback, then confirm local Mac/Cloudflare-served Safari imagery; remove backups only with explicit authorization. Do not restart processes, change the tunnel, merge PR #40, close Issue #39, delete the branch, tag, release or change versions without their respective approvals. Local uncommitted/unpushed work and runtime cannot be inspected through GitHub.

## Documentation and policy

Homepage main SUITE_DEVELOPMENT_POLICY.md v1.1.0 was read and verified. SailingGo AGENTS.md, README.md, ARCHITECTURE.md, DEVELOPMENT.md and CURRENT_WORK.md were reviewed. README, ARCHITECTURE and DEVELOPMENT now include current authoritative sections superseding historical nautical instructions; AGENTS unchanged. Before FINISH, verify all docs and current PR base/head/checks, inspect devbuild.sh, and update this snapshot with actual acquisition/migration outcomes and required six-item handoff.
