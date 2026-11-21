# Current work — SailingGo

**Active scope:** Issue [#37](https://github.com/richard-mauri/pittsburg-saildata/issues/37), draft [PR #38](https://github.com/richard-mauri/pittsburg-saildata/pull/38), branch `charts/37-ncds-mbtiles-evaluation`, based on merged `main` `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47` (PR #36).

**Latest owner Safari finding (Nov. 21):** The previous 12-second outage warning is now visibly working, confirming the prior response-check fix, but user reports **no immediate busy/progress cue, no Cancel, and Nautical radio remains selected while Street Map is shown after failure**. The latest candidate updates the existing map notice to show **Loading NOAA nautical chart…** and a **Cancel** button immediately when starting the response timer. Cancel switches back to Street. All-stalled timeout and completed all-error requests now also **switch the visible radio to Street**, retaining a readable retry-later notice. The switch dispatches a regular bubbled radio `change` event to the existing preference controller, staging a change without an automatic Save or personal/Guest account write. Original saved preference is retained until explicit Save. Street Map underlay, map marker/overlay behavior and source URL remain unchanged. The timer stops when leaving Nautical and may be retried on another selection. There is no fake percentage, new NOAA network request, tile-server integration or diagnosis of weekend maintenance.

**Testing and blockers:** CI JavaScript regression tests simulated loading, cancellation, timeout selection update, success advisory and existing map fallback; latest exact-head CI must be checked. Owner Mac / Safari / live NOAA acceptance remains pending; no production process or tunnel was modified by the agent. GitHub data and local owner work cannot be assumed synchronized without clean-tree/fetch/pull inspection.

**NOAA MBTiles evaluation:** Issue #37 retains exact owner audit data: download `ncds_19c.mbtiles` 241,352,704 bytes, SHA-256 `6f887b5df127d142f6f6826f8968f41977e89417e2205fccd0cc7524fa26f096`, modified May 28, 2024, integrity `ok`, PNG overlay z0–16; very small z14–16 Pittsburg sample tiles and missing Delta z16 sample. NOAA metadata endpoints timed out while distribution server and individual MBTiles file worked. Dataset currency, full local chart detail and public redistribution terms remain open. No NOAA binary committed, no Go HTTP tile endpoint or source selector. Go-only tools; no Python. `*.mbtiles` ignored.

**Safety:** Do not change auth, personal accounts, Guest/personal explicit Save/Discard, persistent storage, tokens, NOAA data, running local processes, deployment, tunnels, versions/build counters, tags or release. Mac private settings and unpushed work inaccessible to agent. No production launch/restart executed.

**Docs/policy:** Homepage merged-main suite policy v1.1.0 and SailingGo AGENTS, README, ARCHITECTURE, DEVELOPMENT, CURRENT_WORK reviewed; ARCHITECTURE/DEVELOPMENT/CURRENT_WORK updated for this UI iteration. No README or AGENTS changes.

**Authority:** Draft stays open; no merge, Issue closure, branch deletion, deployment, restart, tag or release. NEXT for scoped iterations, FINISH for accepted closeout.

**Owner Mac handoff (normal Terminal shell; stop if dirty):**
```sh
cd ~/projects/sailing-go
git status --short --branch
# Stop if any uncommitted changes are shown.
git fetch origin
git checkout charts/37-ncds-mbtiles-evaluation
git pull --ff-only origin charts/37-ncds-mbtiles-evaluation
git status --short --branch
./devbuild.sh
./sailing-go -version
```
Owner separately authorizes any intentional service launch/restart. Live Safari must confirm both the Cancel and timeout paths, including explicit personal/Guest Save/Discard.
