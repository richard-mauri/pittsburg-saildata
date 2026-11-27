# Staged California terrestrial generation — local study only

Owner NEXT approved the exact two-source plan in POI_MANAGER_PLAN.md. cmd/poimanager plan / acquire -apply / build / verify. No public API, install, production pointer, refresh automation, marine export or source redistribution authorized.

Run from clean verified branch, one command at a time. Check at least 8 GiB free (allowance, not filesystem quota). Reuses existing verified north object and local DuckDB 1.4.2 image; no new image pull unless absent. New south object exact 823024945 bytes and Census state ZIP <=32 MiB; combined bounded below 1 GB including one-byte oversize sentinel. Direct HTTPS no redirect/proxy, 15 min each, 30 min overall; SHA256 and source metadata, exclusive partial file, never overwrite. A completed source is verified and reused; partial files preserved, stop/report failure. No independent publisher checksum. Boundary archive validation during build: fixed ZIP members, expanded <=256 MiB, DBF STATEFP 06, matching SHP record ordinal/type/length, geographic NAD83/GRS1980/Greenwich/degree CRS guard. No extraction to filesystem. Point-in-polygon parity across rings incl holes/islands via latitude edge bins; approximate 5 m quarantine near boundary to account for datum uncertainty (not a validated transformation). Actual archive/CRS and boundary shape still owner-run.

Build uses 1-degree latitude bands, one file at a time, category prefilter derived from exact token mapping, ordered IDs, cap 300k candidates/band, 4M total, 256 MiB raw/band, 8 GiB cumulative raw, <=1M accepted, <=1 GiB each payload/provenance, 32 MiB index. Half-open latitude bands except final 42.1. North/south source band ranges derived from STAC extent; no rows beyond envelope. One temporary band on disk, removed only on successful processing; failure retains stage. 2h extraction context, 15min per container, 4 GiB/2 CPUs, read-only/no network/pull, same local image ID. No absolute process RSS cap for Go, no absolute disk quota. Threshold >=0.5, known open or missing status; name/ID/WKB, California polygon and license allowlist CDLA-Permissive-2.0/Apache-2.0/CC0-1.0. Unknown or missing licenses excluded; source-level notices/rights still require review before public install. Explicit restaurant expansion, exclude internet/animal cafe, mobile food truck, private holiday rental, campground, service apartments, ambiguous fuel; no auto-merging distinct IDs, conflicting same ID fails. Stable native category and seven-field waypoint JSON, source release UTC midnight, per-record provenance sidecar. No marine records.

Generation writes binary index (32-byte record lat/lon/offset/length/category; sorted by .05-degree cell then payload offset), concatenated waypoint JSON payloads, JSONL provenance, manifest hashes/counts. Integrity check all file hashes, every index record and waypoint, category counts, unique IDs, contiguous payload offsets; then rename stage to fixed local generation, no overwrite/no current pointer. Provenance file hash checked; individual provenance cross-reconciliation still needed for publication. Verify loads directory, checks same data, 32 deterministic queries, exact full JSON payload and truncation parity against sequential disk scan, 500 result limit. Warm sequential query timings, no concurrent/cold-cache test; Go heap/sys snapshots not peak RSS, owner /usr/bin/time -l includes local process measurement (not Docker VM). No actual statewide counts or runtime index format selection until owner reports. CI synthetic only; no external source transfer. Stop if any command fails; never delete old study or retry acquisition blindly.

Commands:
```sh
cd ~/projects/sailing-go
df -h .
go run ./cmd/poimanager plan
go run ./cmd/poimanager acquire -apply
/usr/bin/time -l go run ./cmd/poimanager build
/usr/bin/time -l go run ./cmd/poimanager verify
du -sh private/poi-california-2026-11-18.0
```
Return aggregate reports, not raw records. For a failed stage return terminal error and stage path. No SailingGo rebuild/restart.

Synthetic CI also tests a two-record ZIP/DBF/SHP archive with California selected by STATEFP, polygon hole and exclusion of other state, plus actual offline DuckDB SQL against a local Parquet fixture containing a casual eatery. These fixtures are not actual 2026 state data.
