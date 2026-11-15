<!-- GENERATED FILE — DO NOT EDIT
source_repository: richard-mauri/homepage
source_path: SUITE_DEVELOPMENT_POLICY.md
source_commit: 502f56de374f094eec2e6d5107c89d2d963f44fd
policy_version: 1.0.0
source_sha256: 1fd61cfcb291b39521addeb2b9db14eaeac4ae638c3d96f9e54ea499ddfec816
generation: python3 scripts/suite_policy.py --sync
-->
# Suite development policy

**Policy version:** 1.0.0 (initial proposal; not effective suite-wide until owner-approved merge and consumer adoption)  
**Editable authority:** [richard-mauri/homepage](https://github.com/richard-mauri/homepage), `SUITE_DEVELOPMENT_POLICY.md` on merged `main`  
**Coordination:** [Homepage Issue #45](https://github.com/richard-mauri/homepage/issues/45)

This is the single source of *shared development and agent workflow rules* for the five independent applications: [Homepage](https://github.com/richard-mauri/homepage), [NewsGo](https://github.com/richard-mauri/newsgo), [MauriStocks](https://github.com/richard-mauri/mauristocks), [ScraperGo](https://github.com/richard-mauri/scrapergo), and [SailingGo](https://github.com/richard-mauri/pittsburg-saildata). No sixth standards repository is needed. The applications, their GitHub repositories, processes, persistent data, listeners and Cloudflare tunnels remain independent. This policy changes no runtime behavior.

## Authority, precedence, and agent entry

For any development session, read the **actual text** of this policy before editing, along with the target repository's `AGENTS.md`, `DEVELOPMENT.md`, `ARCHITECTURE.md`, `CURRENT_WORK.md`, and `README.md` from the working branch. Verify GitHub's current `main`, Issues, branch, PR, head SHA and checks; `CURRENT_WORK.md` is a current-state handoff, not evidence that GitHub still has that state. Review owner-local uncommitted and unpushed work when accessible. Do not overwrite it.

This file controls shared rules: branches, Issues, PRs, data/security safeguards, verification, handoff, approval, FINISH and branch lifecycle. Each repository's local documentation controls its own architecture, implementation, runtime startup, builds, platform testing, data migration and operations **provided it does not contradict shared policy**. `README.md` serves users; `ARCHITECTURE.md` records implemented contracts; `DEVELOPMENT.md` supplies app-specific procedures/commands; `AGENTS.md` is the mandatory entry point; `CURRENT_WORK.md` is one factual current snapshot, not a running historical diary. History belongs in Git. Read all five; edit only those affected and disclose review exceptions.

An exception to shared policy requires explicit owner approval recorded in the relevant Issue/PR and, if durable, a policy change in Homepage. A conflicting local instruction is not an implied exception. Stop and ask if instructions conflict or policy access/provenance is uncertain.

### Local/offline access and provenance

The intended consumer layout is a **generated, read-only, committed** `docs/suite-development-policy.md` copy containing the complete policy, preceded by a provenance header specifying upstream repository, upstream file, exact **merged-main commit SHA**, policy version, SHA-256 of the authoritative source, and generation instructions. Its local `AGENTS.md` must direct agents to read this copy before work, verify the metadata against the source where network is available, and treat stale or missing copies as a blocker for edits involving shared process. When offline, use the committed copy only if its provenance is complete and no known newer policy exists; report offline verification as unverified and do not perform merge/FINISH or policy synchronization while the current authoritative state cannot be checked. A remote hyperlink alone is insufficient. Homepage itself reads this original file from the actual working branch; check the merged `main` policy for precedence if editing a policy proposal.

Copies are **generated artifacts, never hand-edited**. A policy version changes only via a reviewed Homepage policy PR. Consumer changes arrive through separate reviewable PRs, with no direct writes to `main`, no assumed cross-repository write rights and no automatic merges.

## Development workflow

1. Acknowledge verified merged `main`, actual active Issue(s)/PR(s), branch, scope, architecture boundaries, local divergence, expected checks, owner Mac/Safari/live acceptance and approval restrictions *before* edits. Reconcile existing open work first.
2. For a new scoped task use a short-lived Issue-linked branch based on current `main` and one **draft PR targeting `main`**. Never reuse a legacy `dev` or another work item's branch/PR. Track reproducible bugs in the affected application's Issue with observed/expected behavior, reproduction, build, diagnostics, redacted evidence and regression risks. Use `Fixes #N`/`Closes #N` only when completion is real and appropriate. If the owner directs related changes to be tested together in the **same active PR**, keep them integrated even when separately tracked; do not create a replacement PR. Seek approval before expanding scope or starting separate cross-repository consumer PRs.
3. Inspect the current tree and dependencies before edits. Preserve tests, app boundaries and security controls. Before commits review `git status` (including untracked files and ahead/behind), unstaged and staged diffs, `git diff --check`, the exact staged file inventory, and any commits ahead of origin. Never derive `git add` paths from an old manifest. No force pushes, destructive resets, checkout over dirty work or silent data migration.
4. Verify relevant tests/regressions, formatting and builds for runtime changes; for docs-only changes verify contents, relative paths, commands, cross-links, generated policy provenance and applicable CI. After push verify required GitHub Actions/checks against the **exact latest PR head SHA**. A stale green run does not count. If no CI exists or permission prevents inspection, say **unverified**, never passed. CI does not prove owner Mac, Safari, live publisher/NOAA behavior or tunnels.
5. Refresh the draft PR title/body and `CURRENT_WORK.md` before handoff. Record exact base/head, scope, changed files, Issue links, regression risk, tests, CI run/status, remaining Mac/Safari/live checks and approval boundary. Verify the saved PR description. Every status/handoff includes the six-part checklist below and exact local Mac commands appropriate to the change. Do not represent drafted, pushed, CI-tested, locally tested, merged, deployed or released as interchangeable statuses.

### Data and operations safety

Preserve owner's local/uncommitted/unpushed files, subscriptions, exclusions, feed catalogs, watchlists, preferences, account records, per-user storage, browser history/session state, private credentials and tokens, private activity configuration, persistent data, Cloudflare/DNS/tunnel configuration and Git-private build counters. Do not print or commit secrets, cookies, identity records, logs, local binaries or private settings. Keep Homepage server authorization and per-user isolation; never infer permissions solely from UI. Application processes, tunnels and listeners are separate: do not restart or reconfigure them while changing policy. Avoid release-mode build scripts. Changes to live data, authentication, deployment, versions, tags, release or restarts require separately explicit owner authorization.

## Mandatory visible six-item checklist

For **every development status and handoff**, give a short factual account of:

1. **Baseline and scope:** verified GitHub `main`, Issue(s), actual branch/PR/base/head; whether related work belongs together in the same PR.
2. **Data safety:** what local changes, subscriptions, exclusions, preferences, accounts, settings, tokens, persistent data, browser history, tunnels and counters were checked/preserved; disclose inaccessible owner-local state.
3. **Documentation current:** review `README.md`, `ARCHITECTURE.md`, `DEVELOPMENT.md`, `AGENTS.md`, `CURRENT_WORK.md` for accuracy; list edits and exceptions; verify this policy version and any generated consumer copy.
4. **Verification:** relevant regression/Markdown/link/path checks and exact-head CI run/result; distinguish passed, failed and unverified owner Mac/Safari/live/tunnel testing.
5. **Local handoff:** complete copy-pasteable **Mac commands** with actual checkout directory, branch, clean-tree check, fetch/checkout/pull, and (for runtime changes) test/build/launch from that app's `DEVELOPMENT.md`; after FINISH, exact safe restoration commands. Use `git checkout` for the owner's Git, not `git switch`. Never force checkout over dirty work.
6. **Promotion authority and outcome:** specify approval and actual completion status separately for merge, associated Issue closure, remote branch deletion, deployment, tag and release. Never claim a step completed without observing it.

## Owner command `FINISH`

Only the owner's explicit **`FINISH`** for a clearly identified active scoped PR authorizes its *complete closeout*, in this order:

1. Confirm exactly which active PR FINISH applies to, its `main` base, current head SHA, required **passing exact-head CI**, merge safety/protection/reviews and absence of blocking issues. Stop and report if ambiguous, unavailable, failed or unsafe; never bypass protection.
2. Merge **that PR only** using an allowed normal method; verify it actually merged into `main`.
3. Close **only the completed Issue(s) associated with that PR**; verify each result. Do not automatically close the central suite coordination Issue while related consumer work remains.
4. Check that the exact short-lived **remote head branch** is merged and has no open PR or unique unmerged work depending on it. Delete that branch only, without force, or verify GitHub already removed it. Do not delete `main`, retained release/archive branches, unrelated/historical branches, or anything ambiguous.
5. Provide these exact safe owner Mac commands with the **actual repository directory and merged branch** substituted. Stop if `git status` shows uncommitted work; never discard it:

```bash
cd ~/projects/ACTUAL-REPO
git status --short --branch
# Stop here if any uncommitted changes are shown.
git fetch --prune origin
git checkout main
git pull --ff-only origin main
git branch -d ACTUAL-MERGED-BRANCH
git status --short --branch
```

Report the observed merge commit, associated Issue states, remote cleanup, CI and owner acceptance status, and any blocked/failed operation. FINISH is **not** consent for deployment, tag, release, semantic version/build-counter changes, DNS/tunnel changes, other PRs or branches, force operations, or local data loss. Separate owner approval is needed for historical/bulk branch cleanup. No automatic promotion.

## Proposed controlled synchronization (requires owner design approval)

1. After Homepage policy PR is reviewed and **merged by authorized FINISH**, pin the authoritative `main` commit and file hash; generate each consumer's `docs/suite-development-policy.md` with version/provenance, retaining a small local `AGENTS.md` entry that requires loading it. Preserve each app's unique safeguards in `DEVELOPMENT.md` and `ARCHITECTURE.md`.
2. A manually initiated generator (or bot with explicit installation permissions) opens **one draft synchronization PR per consumer repo**. It must never push `main` or enable auto-merge. Review generated changes and independently approve the scope/permissions of each PR. A cross-repo policy update is tested/reviewed as one coordinated set before any FINISH.
3. A read-only CI check verifies the local copy's header, digest, source version and mandatory entry-point/reference; when remote access is available it compares pinned source to current merged Homepage `main` and fails on stale versions. A separate handoff-evidence check may require a PR-body checklist with six factual sections, without asserting owner Mac tests passed. On network/API failure, mark provenance check inconclusive/failing, not green.
4. Demonstrate the mechanism on Homepage (source/entry point) and NewsGo (generated-copy consumer) in reviewable draft PRs, validate offline reading from a checkout, deliberately alter a test copy to confirm stale detection, and only then expand to the remaining consumers. Do not assume the app token has cross-repo write privileges; if it lacks access, provide reviewable patches for owner execution.

**Status:** This section is a design proposal for Issue #45, not permission to begin cross-repo writes or activate required checks. Approval of both the mechanism and separate consumer PR scope is pending.
