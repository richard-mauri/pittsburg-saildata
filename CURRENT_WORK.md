# Mauri's Weather & Water Conditions — current work

**Snapshot: October 26, 2026.** Recheck live GitHub branch/PR state and the owner's local source before resuming. This is an active snapshot, not a release record.

## Baseline and active Issue

- The merged baseline for this task was `main` commit `c71563fd5052a7ba987d1ed7e5acd1563594a76d`, after the five-file documentation PR #2 was merged. Owner's installed local build and unpushed changes remain unverified.
- GitHub source at task start declares `appVersion 1.16.0`, `buildVersion v382`. This development candidate sets `buildVersion v383`; no semantic version bump.
- Active Issue: https://github.com/richard-mauri/pittsburg-saildata/issues/3 — separate UI from the root Go monolith, then apply ScraperGo-inspired colors in an independent follow-up.
- Working branch: `refactor/issue-3-extract-ui-templates` from the above `main` baseline. Draft PR status must be checked live; do not assume owner approval.

## Exact first PR scope

Extract the four HTML templates (welcome, stations, details, sailing dashboard) from `main.go` into `web/templates/*.html` without modifying their HTML, CSS, or JavaScript. Bind them through `ui_templates.go` using `embed.FS`; Go module minimum raised from 1.13 to 1.16 for the embedding feature. Add a basic embedded-file regression test; update architecture. No NOAA, routes, handler interfaces, map logic, deployment or styling changes are intended.

**Static verification completed:** source reconstructed with only the four declarations removed (and v383 substituted) matches the branch's `main.go` exactly; all four extracted template bodies match original bytes. This is not proof of successful Go compilation or Safari rendering.

**Not performed:** complete root package Go tests/build, nested `ui/` module tests, owner Mac/Safari verification, live NOAA checks or deployment. The sailing repository had no GitHub Actions workflow at the start of this change; inspect any current checks for this PR head before claiming CI.

## Next actions

- Review final PR diff/embedded paths, run root Go tests/build on Mac and inspect Safari dashboard, history, station selection, map click/pan separation, popovers, and all major routes. Do not merge before owner acceptance.
- **Separate subsequent PR:** implement ScraperGo's exact semantic light/dark palette with central design tokens while preserving sailing-condition warning colors. Do not combine cosmetic changes with extraction.
- Separate CI workflow setup remains pending, as do potential legacy `WELCOME.md` updates.
- Retain local server/Cloudflare Tunnel settings unchanged. Obtain owner authorization before merge, release or restart.
