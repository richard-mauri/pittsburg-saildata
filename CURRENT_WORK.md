# CURRENT_WORK — SailingGo Issue #41 / draft PR #42 (2026-11-22)

## Baseline and scope
GitHub `main` is the base for branch `feature/41-offline-chart-poc`, draft [PR #42](https://github.com/richard-mauri/pittsburg-saildata/pull/42), tracking [Issue #41](https://github.com/richard-mauri/pittsburg-saildata/issues/41). This is a Phase 0 **isolated API/UI proof of concept**, not a production offline-chart or GPS implementation. Prior Issue #39 / PR #40 is historical and must be verified separately rather than inferred from an older CURRENT_WORK snapshot.

## Changes
- `cmd/offlinechartpoc/main.go`: loopback-only standalone read-only tile inventory API, no operational cache mutation.
- `cmd/offlinechartpoc/main_test.go`: basic tile bounds, missing-tile and read-only tests.
- `cmd/offlinechartpoc/installer-preview.html`: mobile layout mock-up and API preview script; install intentionally disabled and HTML not yet served by the POC.
- `docs/offline-chart-poc.md`: architecture, API candidates, iPhone HTTPS testing plan, limitations and next gates.
- This handoff document.

The existing root SailingGo server, chart cache, Homepage authentication, Cloudflare Tunnel, and browser runtime are **unchanged**. Owner-local Mac changes and operational data cannot be inspected from GitHub.

## Validation and handoff
Review source and run exact-head GitHub CI before claiming success. CI at initial PR head was queued; recheck after this documentation commit. Mac Go build, Safari/iPhone GPS, offline cold start and live NOAA checks **not performed**. Root `./devbuild.sh` is the single owner test/build command; do not run production launcher without separate authorization.

For owner review after exact-head CI passes:
```sh
cd ~/projects/sailing-go
git status --short --branch
# Stop if uncommitted changes appear.
git fetch origin
git checkout feature/41-offline-chart-poc
git pull --ff-only origin feature/41-offline-chart-poc
./devbuild.sh
./sailing-go -version
```

No merge, Issue closure, branch deletion, deployment, restart, build/semantic version change, tag or release authorized. Only owner FINISH may merge and close this scoped PR. Phase 1 and production integration require separate scoped approval.
