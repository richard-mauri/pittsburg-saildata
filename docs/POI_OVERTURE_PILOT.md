# Overture full-object terrestrial pilot — Issue #51

Owner NEXT authorizes one dated object, 836166912 bytes expected, body cap 1000000000, 15-minute acquisition deadline, up to 4 GiB isolated study working allowance. Prior catalog inventory (51,685 metadata bytes) in POI_SOURCE_SPLIT.md. No live API, no production install or source selection. Preserve previous OSM and marine study. No native installation.

## Procedure (owner Mac, each command separately)

From verified feature branch and repository root, inspect free disk (at least 4 GiB; no automated filesystem quota), then pull the official version-pinned CLI image. ARM64 and AMD64 published; version tag not digest pinned across hosts, local image ID captured and used for extraction. No Docker build or spatial extension download.

```sh
df -h .
docker pull duckdb/duckdb:1.4.2
docker image inspect --format '{{.Id}} {{.Architecture}}' duckdb/duckdb:1.4.2
go run ./cmd/poioverturesample download
go run ./cmd/poioverturesample download -apply
go run ./cmd/poioverturesample sample -sample delta
go run ./cmd/poiprobe -input private/poi-overture-2026-11-18.0/delta/normalized.csv -sqlite
go run ./cmd/poioverturesample sample -sample sf
go run ./cmd/poiprobe -input private/poi-overture-2026-11-18.0/sf/normalized.csv -sqlite
du -sh private/poi-overture-2026-11-18.0
```

Stop on failure, do not repeat completed download or overwrite existing study/sample. Preserve partial outputs for diagnosis. Only run probe if Rows >0. Return aggregate reports, manifest, image ID and disk usage, not raw features. Download progress printed to stderr, manifest to stdout. Source HTTPS fixed URL no proxy/redirect, expected Content-Length if present or exact streamed length plus EOF, 1 GB body cap, SHA256 and Parquet magic verified, ETag recorded as object metadata not a publisher checksum; no independently published checksum confirmed. Fresh private/poi-overture-2026-11-18.0 only; failure preserves partial file. Rehash before each extraction. Docker image pulled separately; image transfer not included in data body cap. Docker --pull=never, network none, read-only container and only one source-file mount, uid/gid, 4 GiB cgroup, 2 CPUs, 128 PIDs, 1 GiB /tmp tmpfs, DuckDB 2 GB memory and 512 MB temp allowance. Context deadline 15 minutes per extraction, on failure remove only uniquely named study container. Raw CSV cap 64 MiB, row cap 100k, normalized cap 64 MiB. Host output files limited by these budgets; no absolute disk quota or peak RSS measured.

## Normalization

Schema from https://docs.overturemaps.org/schema/reference/places/place/ and https://docs.overturemaps.org/guides/places/ (November 18 release). Select only id, primary name, basic category, primary taxonomy and hierarchy, operating status, confidence, exact WKB and source metadata. Bbox overlap pruning accommodates outward rounded bbox; missing bbox excluded. No spatial extension: disable GeoParquet conversion and decode only ordinary 2-D WKB Point in Go, exact inclusive sample bounds. Raw candidate count includes overlaps outside exact bounds; reject invalid/unnamed/closed/zero-confidence and conflicting duplicates; no inference from names or proximity. Single primary category for comparable benchmark; conservative provisional exact hierarchy/basic/primary token mapping, ignore alternates. Cafe proxy for coffee, gas_station/fuel_station, grocery_store/supermarket, lodging/hotel/motel/hostel/guest_house, parking/parking_lot/parking_garage, EV charging explicit candidates and restaurant ancestry. Exact token mapping not yet validated against full November taxonomy; inspect reported basic and primary distributions before final source decision. Unknown status value rejected, missing status permitted, nonzero confidence not a guarantee. Sources/licenses aggregated for accepted rows (multiple possible per feature), raw source metadata retained only locally. No assumed source license clearance. Names and stable source IDs retained; no public waypoint or runtime dataset created. Compare counts with OSM point-only pilot, not unique statewide physical coverage; no border/clipping or cold-cache/RSS benchmark. Existing probe includes queries outside sample; not a production latency measurement.

CI Go unit tests + synthetic local Parquet DuckDB fixture using same extraction function. No public Overture transfer in CI. Real GeoParquet metadata, owner ARM64 behavior and exact release taxonomy require owner measurement. Official image https://hub.docker.com/r/duckdb/duckdb/tags and CLI https://duckdb.org/docs/current/operations_manual/duckdb_docker .

## Owner acquisition and nullable CSV correction — 2026-11-27

Download verified exact 836166912 bytes in 11.059865958 seconds, SHA256 be8157302e8d94b648f884ae65b10f6073960b439dd0aceda2e145c75bfe5441, ETag "4deda9314cbe9252124f6c0967072f1a-13", last modified Wed, 18 Nov 2026 17:01:59 GMT, downloaded 2026-11-27T22:04:46.895873Z. ARM64 local DuckDB image sha256:3e2cca396e228ba2a458a21919a83948eef637e843c35cb0cf51606465d28402. Delta extraction finished; Go failed on hierarchy JSON. Owner read-only diagnostic found 56 literal uppercase NULL hierarchy values, not valid JSON. Normalize SQL NULL sentinels across nullable columns (including status, confidence, source metadata); missing names and IDs still rejected, malformed non-null JSON still errors. Added regression fixture. For this owner-confirmed completed CSV only, run `go run ./cmd/poioverturesample normalize -sample delta` after guarded pull. Rehashes source, reads bounded regular existing CSV, refuses existing normalized/report files, no download or Docker. Legacy CSV has no extraction-complete marker, so resume relies on owner evidence; original extraction timing not captured, no independent completeness assertion. Then probe if nonempty, proceed to SF using normal sample command. No production changes.


## Completed owner real-data measurements — 2026-11-27

Both samples successfully normalized and passed exact grid/scan/SQLite parity (100 queries each) on Darwin arm64 Go 1.27.3. Source manifest and ARM64 image ID above. SF actual extraction 0.6445015 s, normalization 0.094660416 s, raw CSV 44753371 bytes; Delta normalization-only retry 0.004168541 s, raw CSV 1916427 bytes, extraction duration unavailable. Total study du -sh 846M; no peak RSS or cold-cache measurement. No publisher checksum independent of recorded local SHA256.

| Category | Delta OSM accepted | Delta Overture raw/accepted | SF OSM accepted | SF Overture raw/accepted |
|---|---:|---:|---:|---:|
| coffee | 4 | 8/8 | 328 | 556/504 |
| ev_charging | 0 | 1/1 | 5 | 13/11 |
| fuel | 3 | 19/19 | 0 | 17/17 |
| grocery | 3 | 24/24 | 32 | 171/163 |
| lodging | 0 | 9/9 | 60 | 695/683 |
| parking | 0 | 0/0 | 23 | 399/382 |
| restaurant | 16 | 121/117 | 756 | 2485/2291 |
| Total terrestrial | 26 | 182/178 | 1204 | 4336/4051 |

Delta 1790 candidates, 1 outside, 1607 unclassified, 4 permanently closed. SF 41505 candidates, 3 outside, 37166 unclassified, 283 permanently closed, 2 unnamed/invalid. Delta 56 missing taxonomy; SF 1002. Status across exact-bound candidates: Delta 403 missing/1349 open/37 permanently closed; SF 7077 missing/31949 open/2474 permanently closed/2 temporarily closed. Confidence bins across scoped rows BEFORE subsequent rejection: Delta 15 below 0.5/24 from 0.5 to below 0.8/143 at least 0.8; SF 293/745/3298. Nonzero low confidence permitted in pilot, not verified existence. Accepted source license memberships (overlapping): Delta CDLA-Permissive-2.0 178, Apache-2.0 14, CC0-1.0 3; SF 4051/646/64 respectively. Accepted dataset memberships Delta AllThePlaces 3, BrightQuery 26, DAC 1, Foursquare 14, Microsoft 12, Overture 178, Overture-signals 135, Uberall 1, meta 122; SF AllThePlaces 64, Birdeye 3, BrightQuery 1042, DAC 1, Foursquare 646, Microsoft 273, OpenTravel 1, Overture 4051, Overture-signals 3083, Uberall 5, meta 2038. No license clearance for publication yet. Raw samples retained locally, no raw business rows committed.

| Probe metric | Delta | SF |
|---|---:|---:|
| Rows | 178 | 4051 |
| Grid file bytes | 4272 | 97224 |
| Grid cells | 3 | 4 |
| Grid build ms | 7.565167 | 8.307625 |
| Grid single batch ms | 0.097791 | 1.556834 |
| Grid single p50/p95/p99 ms | 0.000042/0.002875/0.007458 | 0.000042/0.09475/0.187041 |
| Grid four batch ms | 0.080167 | 0.663208 |
| Grid four p50/p95/p99 ms | 0.000042/0.005417/0.010875 | 0.000042/0.116792/0.160792 |
| Scan batch ms | 0.159875 | 0.786166 |
| SQLite bytes | 40960 | 331776 |
| SQLite build ms | 5.100667 | 19.2735 |
| SQLite batch ms | 4.245583 | 11.295333 |

Overture 4229 accepted terrestrial records versus point-only OSM 1230 (3.44x); sample ratios 6.85x and 3.36x. No inference of completeness, unique establishments, accuracy or statewide coverage. OSM geometry rejection biases comparison. Parking absent in Delta Overture output; present in SF. Taxonomy audit needed for broad lodging and cafe mapping, ambiguous fueling_station, convenience stores (not currently included), public vs restricted parking, duplicate identities/establishments and low-confidence records. SF scan faster than single-worker grid; SQLite includes CLI overhead, grid excludes in-memory directory and payload, many queries miss sample; no final index performance guarantee.

Recommendation for next NEXT: provisionally select Overture for terrestrial and existing curated marine export for marine, subject to owner approval, licensing and quality audit. Use existing local Parquet/CSV only for targeted category/name/coordinate and duplicate audit, assess California boundary and all catalog item extents (this one file does not cover southern CA), design bounded statewide builder, full-payload spatial benchmark and stable native category IDs aligned to GPS Map 2.2.0. Do not acquire another object without separate bounded scope. No runtime API, production dataset, service change, merge or release yet.
