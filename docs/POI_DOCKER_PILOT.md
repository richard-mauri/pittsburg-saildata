# Docker Northern California POI pilot

Owner NEXT approved Docker-only acquisition 2026-11-27: Docker 29.9.3 linux/aarch64, Go 1.27.3 Darwin arm64, 695 GiB available, no native Osmium/DuckDB. No Homebrew installs or operational data/runtime changes. Source selection remains open.

[Geofabrik Northern California](https://download.geofabrik.de/north-america/us/california/norcal/index.html) lists norcal-261126.osm.pbf and matching MD5 (~627 MB). Go poisample download defaults to dry run; -apply creates new Git-ignored private/poi-study-* directory, fixed HTTPS GETs, no redirects/proxy, known Content-Length, total response body <=1 GB, 15 minute context, publisher MD5 and SHA256 recorded. MD5 is integrity, not a signature. No latest fallback if the dated extract expires. Failed download cleans only its own new directory.

Docker Debian trixie-slim uses pinned precompiled osmium-tool 1.18.0-1; base/transitive packages not digest pinned. Record image ID and version. Registry/apt traffic separate from dataset budget. Offline extraction, read-only root, cap-drop, no-new-privileges, 4 GiB memory for subsequent owner extraction (2 GiB original pilot failed on SF), 2 CPUs, 128 pids, 64 MiB tmpfs, only isolated study directory bind mount. File limit per output <=512 MiB (shell units), two samples and input nominally within 4 GiB; monitor actual disk. Extract deadlines total 900 s per sample. No production data or repo mounted.

Run from repository root after safe checkout, with set -e:
```bash
docker build -t sailinggo-poi-osmium:1 tools/poi-osmium
docker image inspect --format '{{.Id}} {{.Architecture}}' sailinggo-poi-osmium:1
go run ./cmd/poisample download
study="$(go run ./cmd/poisample download -apply)"
test -f "$study/source.json"
cat "$study/source.json"
for sample in delta sf; do
  docker run --rm --network none --read-only --cap-drop ALL \
    --security-opt no-new-privileges --memory 4g --cpus 2 \
    --pids-limit 128 --tmpfs /tmp:rw,noexec,nosuid,size=64m \
    --user "$(id -u):$(id -g)" \
    --mount "type=bind,source=$PWD/$study,target=/study" \
    sailinggo-poi-osmium:1 "$sample"
  test -f "$study/$sample/complete.txt"
  cat "$study/$sample/osmium-version.txt"
  /usr/bin/time -l go run ./cmd/poisample normalize -sample "$sample" \
    -input "$study/$sample/features.geojsonseq" -output "$study/$sample/normalized"
  /usr/bin/time -l go run ./cmd/poiprobe \
    -input "$study/$sample/normalized/normalized.csv" -sqlite
done
du -sh "$study"
```

Stop if failure, preserve failed extraction, no overwrite or automatic retry. If zero accepted rows, report normalizer and skip probe. Return source manifest, image ID/version, normalizer reports, probe output and disk/time; do not return raw private data. Time -l includes Go build overhead, Docker memory limit not peak RSS.

Osmium complete_ways extract, tags-filter with references, GeoJSON sequence export with @type/@id ([export docs](https://docs.osmcode.org/osmium/latest/osmium-export.html)); relations can be incomplete, invalid geometry omitted, ways may be both lines and polygons. Raw category counts are exported feature counts, not unique source object counts. Go bounded normalization (128 MiB, 2 MiB line, 100k features), exact interior sample point predicate, stable type/id, explicit invalid/closed/unnamed/duplicate/non-point and secondary membership counts, reject conflicting duplicates, deterministic single-category CSV for poiprobe. Cafe proxy for coffee; explicit marine tags, no inference by proximity. **Point-only pilot** excludes non-point marine/parking facilities and unnamed infrastructure; raw counts not coverage proof, no arbitrary centroid or asserted navigable entrance. Actual geometry resolution, lifecycle and cross-type dedup, state polygon and border tests remain required. No production waypoint/index built; ODbL attribution/redistribution decision pending. Overture comparison deferred. CI synthetic only; real source and ARM64 image validation owner-run.

## Owner pilot results — 2026-11-27

Owner Mac (Darwin arm64, Go 1.27.3), Docker ARM64 image ID sha256:5b26235f02c5b7408015fdd653b5bc8e154ab31fe2ab1a2687ecdaf56d5bc799. Dated norcal-261126 extract downloaded in 57.170821 seconds, 657270484 PBF bytes plus 56 checksum bytes, verified publisher MD5 0712d608c1f8ca716fda71d0e7e30c6d, SHA256 9687af8e1ca263e20d2e44e7531067e09bda515b58c269a1f84b4a5c6e899bd7; downloaded_at 2026-11-27T19:26:46.662492Z. ODbL-1.0. Owner study directory private/poi-study-1425392030, total du -sh 646M (including preserved failed extractions). No repeat download. Delta extraction complete at 2 GiB; SF failed with confirmed Exit=137 OOMKilled=true at 2 GiB, succeeded Exit=0 OOMKilled=false at 4 GiB. Docker VM memory 8319172608 bytes. 4 GiB is a successful limit, not measured peak RSS; use 4g for subsequent comparable owner acquisition, not runtime, and preserve failed study directories. CI tiny synthetic fixture retains 2g. Exact owner checkout SHA and osmium --version text not supplied; image ID and actual execution reported.

| Category | Delta exported | Delta accepted | SF exported | SF accepted |
|---|---:|---:|---:|---:|
| boat_ramp | 1 | 1 | 7 | 0 |
| coffee | 8 | 4 | 375 | 328 |
| ev_charging | 1 | 0 | 12 | 5 |
| fuel | 7 | 3 | 19 | 0 |
| grocery | 9 | 3 | 36 | 32 |
| lodging | 4 | 0 | 474 | 60 |
| marina | 1 | 1 | 2 | 0 |
| marine_fuel | 0 | 0 | 1 | 1 |
| parking | 262 | 0 | 855 | 23 |
| pumpout | 0 | 0 | 1 | 0 |
| restaurant | 44 | 16 | 861 | 756 |
| Total | 337 | 28 | 2643 | 1205 |

Delta 339 features, 154 LineString/154 MultiPolygon/29 Point classified; 308 non-point, 2 unclassified, 1 unnamed/invalid name. SF 2842 features, 679 LineString/713 MultiPolygon/1251 Point classified; 1392 non-point, 199 unclassified, 42 unnamed/invalid name, 4 outside. Exported counts are feature representations, not verified unique OSM objects; geometry duplicates require actual ID audit. Point-only acceptance is not source completeness.

| Probe metric | Delta | SF |
|---|---:|---:|
| Rows | 28 | 1205 |
| Queries | 100 | 100 |
| Exact parity | pass | pass |
| Grid bytes (excludes memory directory) | 672 | 28920 |
| Grid cells | 3 | 4 |
| Grid build ms | 7.8965 | 6.66375 |
| Grid single batch ms | 0.17525 | 0.405541 |
| Grid four-worker batch ms | 0.129958 | 0.195375 |
| Grid single p95 ms | 0.003958 | 0.027083 |
| Grid four p95 ms | 0.019334 | 0.034584 |
| Scan ms | 0.052625 | 0.294917 |
| SQLite bytes | 20480 | 118784 |
| SQLite build ms | 10.468167 | 8.753125 |
| SQLite batch ms | 7.849625 | 5.85525 |

Small samples, most queries outside individual sample, SQLite CLI overhead, no RSS/cold-cache/full payload data; no statewide scaling inference. Next proposed NEXT: geometry-aware normalization on existing local GeoJSON sequences, no download, deduplicate by type/id before counting, distinguish representative point from navigable entrance, conservative in-geometry placement and exact bbox, report unnamed infrastructure separately and retain safe labels only after owner approval, preserve source provenance, compare revised counts and targeted query fixtures. Add stderr download progress and document 4g acquisition limit. Overture comparison, boundary polygon, licensing and final source/index selection still outstanding. No runtime changes or production install.
