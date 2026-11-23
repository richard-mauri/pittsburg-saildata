# CURRENT_WORK — Issue #41 mobile GPS marker POC

## Active scope
Issue [#41](https://github.com/richard-mauri/pittsburg-saildata/issues/41); branch `feature/41-iphone-gps-marker-poc` based on GitHub `main`. Separate related draft PR #42 concerns the offline chart installer and remains independent. This phase introduces an opt-in foreground GPS marker for iPhone-width Leaflet map; no offline chart install or track recording. Exact PR URL/head/checks are authoritative on GitHub after creation.

## Files and behavior
- `web/static/gps_marker_poc.js`: mobile-only permissioned watchPosition, blue position marker and reported accuracy, follow/pan toggle and Stop GPS.
- `ui_templates.go`: embed and inject the isolated script after the existing Leaflet map constructor in the Conditions Now page; other pages unchanged.
- `gps_marker_poc_test.go`: checks embedded module exists only in Conditions Now map page.
- `docs/iphone-gps-marker-poc.md`: Safari field acceptance and limitations.
- `CURRENT_WORK.md`: this current handoff.

The selected sailing/weather location is never modified by GPS or follow. No server GPS upload, track persistence, chart acquisition, account/token change, Cloudflare modification or operational cache write. Existing offline installer draft PR #42 is not merged into this branch. Mac owner-local working tree and private data are inaccessible through GitHub.

## Validation and authority
Review exact-head CI and run the root `./devbuild.sh` on Mac after clean checkout. iPhone Safari/HTTPS GPS and chart field tests remain unverified until owner tests. Do not run `./sailgo.sh`, restart a service/tunnel, merge, close Issue #41, delete branches, bump versions, tag or release without separate owner approval. Homepage `main` suite policy v1.1.0 governs.

## Owner Mac handoff (after passing exact-head CI)
```sh
cd ~/projects/sailing-go
git status --short --branch
# Stop if uncommitted changes appear.
git fetch origin
git checkout feature/41-iphone-gps-marker-poc
git pull --ff-only origin feature/41-iphone-gps-marker-poc
./devbuild.sh
./sailing-go -version
```
