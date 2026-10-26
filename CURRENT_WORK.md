# Mauri's Weather & Water Conditions — current work

**Snapshot: October 26, 2026.** Reconfirm PR and build state on GitHub before continuing, and reconcile any newer local Mac work.

## Verified baseline

- PR #4 (UI template extraction) merged into `main`; its exact-head GitHub Actions run succeeded: https://github.com/richard-mauri/pittsburg-saildata/actions/runs/40350065247 .
- Theme branch base: merged `main` at `474d1d6d3a9840fd89020c29b7f87d8c58319dd9`.
- Application version remains `1.16.0`; the current UI-theme candidate is `v390` (previously `v389`); appVersion remains `1.16.0`. Prior CSS revisions under v384 did not increment the build as required; this was corrected on October 26, 2026.
- Local running application, Safari appearance and any unpushed local changes remain **unverified**.

## Active change — Issue #3

- Issue: https://github.com/richard-mauri/pittsburg-saildata/issues/3 .
- Branch: `feature/issue-3-scrapergo-palette`, separate from the merged extraction PR.
- Scope: centralize ScraperGo's semantic light/dark surface/text/accent/border tokens in one `web/static/theme.css`, embedded into all four HTML pages by `ui_templates.go`. Prior duplicated per-page theme overrides were removed. Conditions Now uses a consistent neutral panel with a status-colored left border instead of a full brown caution background. Preserve weather/current calculations and meaningful chart-series colors.
- No routing, NOAA processing, station-selection JavaScript, map selection, overlay behavior or Cloudflare changes intended.

## Checks and next action

- Inspect and verify GitHub Actions at the exact latest theme PR commit before handing the PR off; a passing run alone does not establish Mac/Safari appearance.
- The owner reported two unacceptable dark-mode screenshots (unreadable contrast, then oversized brown caution card and white selector panels). The shared-stylesheet revision responds to these but is not yet verified in Safari. Owner Mac/Safari acceptance should review both light and dark system appearances: welcome, Conditions Now, Planning and Details, nearby stations, history chart and table, map overlays, menus/modals and warnings. Check readability and contrast, especially the wind chart, labels and form controls.
- Owner approval required before merge, release, or service restart. Any contrast defects discovered during acceptance are fixed on the theme branch with CI rerun.
- Related work: remaining monolithic Go backend structure is out of scope; historical WELCOME.md update is separate.

- Build v386: Conditions Now now styles the actual `.PlanningPeriodCause` text via `.planning-period-cause`, using preferred green, caution orange, and red-flag rose while retaining cyan card edges and reserving gold for the tidal-range scale. Safari acceptance pending.

- Build v387: planning navigation loading overlay and loading dialog now use theme-matched scrim, panel, foreground, borders and spinner. Safari acceptance pending.

- Build v388: card left accents narrowed from 5px to 3px and planning-day accents from 4px to 3px; Conditions Now border now reflects preferred/caution/redflag status with green/orange/rose, and defaults to cyan. Safari acceptance pending.

- Build v389: Planning and Details, Back to Conditions Now, and View full report details use consistent neutral text-link styling with cyan hover/focus and accessible keyboard focus outline. Safari acceptance pending.

- Build v390: hero banner reduced from 360px to 260px minimum height on desktop and from 430px to 300px on narrow screens; background retains proportional `cover` scaling. Safari acceptance pending.
