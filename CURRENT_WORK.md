# Mauri's Weather & Water Conditions — current work

**Snapshot: October 26, 2026.** Reconfirm PR and build state on GitHub before continuing, and reconcile any newer local Mac work.

## Verified baseline

- PR #4 (UI template extraction) merged into `main`; its exact-head GitHub Actions run succeeded: https://github.com/richard-mauri/pittsburg-saildata/actions/runs/40350065247 .
- Theme branch base: merged `main` at `474d1d6d3a9840fd89020c29b7f87d8c58319dd9`.
- Application version remains `1.16.0`; the UI-theme candidate increments build from `v383` to `v384`.
- Local running application, Safari appearance and any unpushed local changes remain **unverified**.

## Active change — Issue #3

- Issue: https://github.com/richard-mauri/pittsburg-saildata/issues/3 .
- Branch: `feature/issue-3-scrapergo-palette`, separate from the merged extraction PR.
- Scope: use ScraperGo's semantic light/dark surface/text/accent/border tokens across `web/templates/welcome.html`, `stations.html`, `details.html`, and `sailing.html`. Preserve weather/current calculations, existing chart data and warning status semantics. Theme additions live at the end of each existing CSS block so the historical UI can be compared easily.
- No routing, NOAA processing, station-selection JavaScript, map selection, overlay behavior or Cloudflare changes intended.

## Checks and next action

- Inspect and verify GitHub Actions at the exact latest theme PR commit before handing the PR off; a passing run alone does not establish Mac/Safari appearance.
- Owner Mac/Safari acceptance should review both light and dark system appearances: welcome, Conditions Now, Planning and Details, nearby stations, history chart and table, map overlays, menus/modals and warnings. Check readability and contrast, especially the wind chart, labels and form controls.
- Owner approval required before merge, release, or service restart. Any contrast defects discovered during acceptance are fixed on the theme branch with CI rerun.
- Related work: remaining monolithic Go backend structure is out of scope; historical WELCOME.md update is separate.
