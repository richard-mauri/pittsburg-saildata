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


## Owner statewide results — 2026-11-27

Owner Mac terminal transcript shows plan, acquire -apply, build, verify and disk usage all succeeded. Northern source reused at pinned SHA256. Newly acquired:
- South Overture object 823024945 bytes, 11.217559292 s, SHA256 0679fbe44ee8642000a73ead77d196945f17b3d12a44a5bc17bd5db95b746fc3, ETag "af3fa5bae8f9d322b7a88c7a794f2036-13", last modified Wed, 18 Nov 2026 17:04:06 GMT, downloaded 2026-11-27T22:30:35.292584Z.
- Census TIGER2026 state ZIP 9957057 bytes, 0.447861625 s, SHA256 2e17ea40dff3e00163eec87a26efa0b0ff7e8fe364a191bf1a7754dcb49ea326, no ETag, last modified Wed, 07 Oct 2026 01:45:57 GMT, downloaded 2026-11-27T22:30:35.7455Z.
- Total new source bytes 832982002, within approved cap; north 836166912 bytes, SHA256 be8157302e8d94b648f884ae65b10f6073960b439dd0aceda2e145c75bfe5441. ARM64 DuckDB image sha256:3e2cca396e228ba2a458a21919a83948eef637e843c35cb0cf51606465d28402. Actual archive accepted by CRS guard, California polygon 7 rings, 12095 edges. No datum transformation or independent publisher checksum.
- Local completed generation private/poi-california-2026-11-18.0/generation; schema 1, ca-terrestrial-v1, release 2026-11-18.0, created 2026-11-18T00:00:00.000Z. Publication local study only. 12 extraction bands, 282159 prefiltered candidates, 296768951 cumulative raw CSV bytes, 199475 accepted, 82684 rejected. Rejects: confidence 15319, identity_or_name 22, operating_status 10842, outside_band 2, outside_state 47706, taxonomy 8793. No reported near-boundary quarantine, duplicate or missing/unknown-license rejects. Category prefilter means these counts are not all California Places.

| Category | Accepted |
|---|---:|
| coffee | 15523 |
| ev_charging | 999 |
| fuel | 12310 |
| grocery | 10364 |
| lodging | 19238 |
| parking | 3419 |
| restaurant | 137622 |
| Total | 199475 |

License memberships (overlapping): CDLA-Permissive-2.0 199475, Apache-2.0 20789, CC0-1.0 5341. This is allowlist evidence, not complete redistribution clearance.

| File | Bytes | SHA256 |
|---|---:|---|
| payload.bin | 56362446 | c7f24b89ac06cac0ba3eec98e3a61a1090a5fcdad432ef6962e04218aa24188b |
| index.bin | 6383200 | 322411080c4b40a181bf2215e3e924003cf75b9f53dcebd707d7f879dbc998f6 |
| provenance.jsonl | 185861155 | ddc65e65c393ddcf2481500b9fd7379ae758418d49cdfeb53a1e83113ecd36d2 |

Payload/index combined 62745646 bytes (~59.84 MiB); three files 248606801 bytes (~237.09 MiB), excluding manifest. New study du -sh 1.0G (excludes existing northern study). Build internal 9.418391041 s, outer go run time -l 10.20 s real, max RSS 94076928 bytes, peak memory footprint 22233832 bytes, Go heap 63782008, Sys 87775512 bytes. Verify outer 2.54 s real, max RSS 60751872 bytes, peak memory footprint 21709544 bytes. These process metrics include go run/toolchain effects, not isolated runtime server or Docker VM RSS. Verification after GC heap 695536 bytes, Sys 58153240 bytes; not total process memory.

32 deterministic full-JSON queries, 3660 cells, 8 truncated, parity pass, 2267953 bytes returned across query set. Grid batch 16.523 ms, sequential disk scan 1776.501627 ms (~107.52x for this workload), grid p95 1.999667 ms. Warm sequential local benchmark, not HTTP latency, cold-cache or concurrent workload. Both commands reported identical manifest and hashes; index/payload validation passed. Provenance individual cross-reconciliation remains pending. No unique real-world establishment, completeness, access, or hours guarantee; actual border positional accuracy and taxonomy still subject to quality review.

Recommendation for next NEXT: adopt native disk-backed grid as leading runtime candidate; harden generation validation and provenance reconciliation, preserve authoritative source notice texts and release-specific attribution for all accepted licenses, investigate missing bbox/coverage and boundary tolerance limitations, separate reusable read-only grid package from CLI and test corruption, bounded concurrency, deterministic result/byte caps and full-payload parity under representative dense/sparse workload. Use existing local generation read-only (no new bulk downloads or rebuild unless explicitly needed); add synthetic tests/CI. Marine remains separate/local-only pending rights. Proposed subsequent API integration phase after owner acceptance; no public routes, production dataset install, restart, merge or release authorized by this checkpoint.
