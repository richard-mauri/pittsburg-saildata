# Mauri's Weather & Water Conditions — current work

**Snapshot: October 30, 2026.** Reconfirm current branch/PR and reconcile newer local Mac changes before installing anything.

## Verified merged baseline and candidate

- GitHub `main` was at `d77ec32e64a4fac8311ea6dd5704e67b7c9d4324` before this change. No open pull requests were found at the start of this session.
- `main.go` declares application version `1.16.0` and baseline development build `v393`; this Issue #12 candidate increments development build to `v394` without changing appVersion.
- Local owner's `~/projects/sailing-go` working tree, Safari appearance, running service, and any unpushed changes are **unverified**. The documented public service is owner-hosted via Cloudflare Tunnel; this PR does not touch the server or tunnel.

## Active change — Issue #12

- Issue: https://github.com/richard-mauri/pittsburg-saildata/issues/12
- Branch: `fix/issue-12-welcome-theme`, forked from the verified `main` baseline.
- Scope: `web/templates/welcome.html` replaces legacy stand-alone CSS palette and hard-coded white controls/backgrounds with the shared `web/static/theme.css` semantic tokens; keep the hero photo and existing layout. `main.go` increments buildVersion to v394. This handoff document tracks active work.
- No intended changes to routing, NOAA observations, current predictions, station choice, map selection, overlays, other templates, or operational configuration.
- The previous Issue #3 theme work is now included in `main`; its older October 26 active-branch notes were superseded, not continued as an append-only log.

## Validation and owner acceptance

- GitHub file-level edits completed on the dedicated branch; Linux CI status must be checked for the **latest PR head** after drafting. No Go tests, build, or Mac/Safari visual checks are claimed yet.
- Inspect welcome in **both Safari light and dark appearance**, mobile and desktop widths, hero primary/secondary actions, GitHub action grid, card readability, loading scrim/dialog and navigation functionality. Check other major views for unintended theme interactions; the shared stylesheet itself is unchanged.
- CI cannot prove native Safari layout or live NOAA response. Preserve data semantics and existing station/map selection behavior.
- Before installing candidate locally, inspect `git status --short --branch` and reconcile changes ahead of origin. Follow the complete branch-specific Mac handoff commands from DEVELOPMENT.md.
- Owner must explicitly authorize merge, semantic version change, release, deployment and/or restart. Do not deploy directly from this working branch.
