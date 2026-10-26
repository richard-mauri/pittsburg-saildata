# Mauri's Weather & Water Conditions — current work

**Snapshot: October 26, 2026.** This is one portable active-work snapshot; reverify GitHub head, Issue and PR status when resuming. The owner's local Mac state and deployed version have **not** been verified in this GitHub documentation session.

## Authoritative baseline and confirmed facts

- Repository: https://github.com/richard-mauri/pittsburg-saildata ; merged baseline: `main`, inspected at commit `ed0d66f8a2cb9d87a631cd1a808882ed55a5397f` before this documentation branch.
- Documented checkout: `~/projects/sailing-go` (not inspected); public https://weather.themauris.org runs from a local Go server through Cloudflare Tunnel, per prior handoff.
- GitHub `main.go` declares `appVersion = "1.16.0"` and `buildVersion = "v382"`. The earlier handoff records user-confirmed local v382 following a History fix, but a newer local build may exist. Reconcile before runtime work.
- The last documented fix introduced `GET /wind-readings` for History; expected `{hours, readings, summary}` and 1/4/8/12/16/20/24-hour selections. Live endpoint, current UI and NOAA responses were not tested here.
- If actual current source remains v382, the provisional next development build is v383; do not bump semantic version during normal iterations.

## Active Issue and documentation scope

- **Issue:** https://github.com/richard-mauri/pittsburg-saildata/issues/1 — five-file GitHub development documentation.
- **Branch:** `docs/issue-1-five-file-workflow`, created from the `main` commit noted above.
- **PR:** draft PR against `main` to be created/verified during this documentation task; do not assume a PR exists until verified on GitHub.
- **Only intended changes:** update `README.md`, `AGENTS.md`; add `ARCHITECTURE.md`, `DEVELOPMENT.md`, `CURRENT_WORK.md`. This is documentation-only; no source, `WELCOME.md`, CI workflow, Cloudflare, build version or runtime modifications.

## Validation and approval state

Markdown files and referenced paths need verification on the final PR head. GitHub Actions CI cannot be claimed passed: no workflow was present on sailing `main` at initial inspection. No local Mac checkout, Go build/test, Safari inspection, NOAA live checks, tunnel changes or deployment were performed for this documentation work. Owner review and explicit merge authorization are outstanding.

## Separate follow-up work

- Implement a sailing-specific GitHub Actions `go-ci.yml` in its **own PR**, taking account of Go toolchain version and nested `ui/` module.
- Implement a ScraperGo-inspired slate/teal light/dark theme in a subsequent scoped UI PR, preserving sailing warning semantics and all map/chart behavior.
- Review legacy `WELCOME.md` separately; it includes outdated Render-era links and must not be silently combined with this five-file workflow PR.
- Check the owner's actual local Git status and current installed build before any runtime/source replacement.

**Next action:** inspect final documentation PR diff, Markdown links and exact latest head; hand off the draft PR for owner review. Do not merge/tag/release without explicit authorization.
