# Mauri's Weather & Water Conditions — agent instructions

**Start here for every development session.** Read [CURRENT_WORK.md](CURRENT_WORK.md) for active work, [ARCHITECTURE.md](ARCHITECTURE.md) for system contracts, [DEVELOPMENT.md](DEVELOPMENT.md) for GitHub and acceptance procedures, and [README.md](README.md) for product behavior. Check current GitHub branches, PRs, code, and any owner-supplied local source: notes may be stale.

## Mandatory workflow acknowledgment

Before changing code or documentation, opening an Issue or creating a branch, explain the verified GitHub `main` baseline, active PR/branch and scope, local-source divergence risk, intended Issue → dedicated branch → **draft PR** workflow, architectural constraints, required CI/Mac/Safari checks and their limitations, and the owner's merge/release authorization gate. For runtime handoff, repeat the complete, branch-specific Mac command block from DEVELOPMENT.md. For documentation-only work, report Markdown/link/path checks and CI status; don't require a Mac restart.

**Work through GitHub Issues, branches and draft PRs, not downloadable replacement-file packages**, when GitHub write access is available and the owner authorizes the work. Never treat a generated file as committed, tested or installed.

## Project safeguards

- Repository: [richard-mauri/pittsburg-saildata](https://github.com/richard-mauri/pittsburg-saildata); documented Mac checkout: `~/projects/sailing-go`; public URL: https://weather.themauris.org. The current handoff says a **locally running** Go server is exposed through Cloudflare Tunnel, not Render.
- GitHub `main` is the authoritative **merged** baseline for new branches. The owner's verified local source may be newer/unpushed; reconcile before touching local work. Do not overwrite divergent changes.
- Build the entire root Go package, not `go run main.go` alone: `wind.go`, `currents.go` and other root files participate. `ui/` has a separate Go module; test it separately when in scope.
- Protect NOAA observed wind versus predicted tidal currents versus forecasts; station choice and freshness; selected sailing location versus map pan/zoom; HTML/text/JSON routes, History, overlays and mobile rendering. See ARCHITECTURE.md.
- Trace definitions **and actual callers**, handlers, UI consumers, tests and configuration; never expand a focused PR silently.
- Increment development `buildVersion vNNN` for source regenerations. Do not change semantic `appVersion` on ordinary development iterations.
- Never merge, tag, release, deploy, restart or modify Cloudflare/tunnel credentials without explicit owner authorization. Clearly distinguish drafted, formatted, CI-tested, Mac-tested, installed, committed, pushed, merged and released.

## Mandatory pre-commit audit

Never derive `git add` lists from memory or previous ZIP manifests. Before local commits inspect fresh `git status` including untracked and ahead/behind, `git diff --check`, `git diff`, `git diff --cached`, and relevant untracked files. Review source, call sites, tests, documentation and configuration. Inspect commits ahead of origin and staged file inventory (`git diff --cached --name-status`) before committing. Exclude secrets, tokens, logs and binaries. A clean tree alone proves neither a correct commit nor a push.

## PR handoff and current snapshot

Keep [CURRENT_WORK.md](CURRENT_WORK.md) as **one portable, accurate current snapshot** with Issue, branch, PR, exact head, test evidence, blockers and next step; replace superseded claims, do not append a diary. Before handing a PR to the owner, bring its title/body up to date, reread them on GitHub, and distinguish latest-head CI from unperformed Mac/Safari acceptance. Only the owner authorizes merge, tag or release. Historical documents are not current instructions.
