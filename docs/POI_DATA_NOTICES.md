# Data attribution — Overture Places 2026-11-18.0
Local terrestrial study, not yet published. Overture Maps Foundation, Places release 2026-11-18.0. Source attribution reviewed 2026-11-27: https://docs.overturemaps.org/attribution/ (Places section), https://docs.overturemaps.org/guides/places/ (November 2026).
Overture lists CDLA Permissive 2.0 for Meta, Microsoft, PinMeTo, Krick, RenderSEO, DAC, BrightQuery, Birdeye, Uberall and OpenTravel Alliance; Foursquare Apache 2.0; AllThePlaces CC0 1.0. Preserve full source license/notice texts. Aggregate actual dataset-license combinations from the reader must be reviewed; license membership counts overlap, and this is not legal clearance. Overture and Overture-signals source metadata may describe derived fields. Do not treat data as exempt results.
Foursquare copyright 2024 Foursquare Labs, Inc. All rights reserved (Overture attribution); Overture states transformed to Overture schema, changed 2026-03-18. Foursquare current NOTICE source https://opensource.foursquare.com/places-notice-txt/ (accessed 2026-11-27; current publisher notice copyright 2026). Preserve full publisher NOTICE in `poi-licenses/Foursquare-NOTICE.txt`. Apache copy accompanies it. Other copies in poi-licenses are SPDX license-list-data v3.27.0 text files, verified against published identifiers, source links below. No claim of Foursquare endorsement.
Modification notice: SailingGo's local generation filters to California using 2026 TIGER/Line state legal boundary, rejects unqualified/confidence <0.5/closed points, maps categories, retains source coordinates/identity, creates waypoint names/notes and builds an index. No verified access/hours or navigational suitability. Census boundary reference https://www.census.gov/geographies/mapping-files/time-series/geo/tiger-line-file.html. Boundary used for filtering, not redistributed as geometry.
Full texts: [CDLA Permissive 2.0](poi-licenses/CDLA-Permissive-2.0.txt) (https://cdla.dev/permissive-2-0/), [Apache 2.0](poi-licenses/Apache-2.0.txt) (https://www.apache.org/licenses/LICENSE-2.0), [CC0 1.0](poi-licenses/CC0-1.0.txt) (https://creativecommons.org/publicdomain/zero/1.0/legalcode), [Foursquare NOTICE](poi-licenses/Foursquare-NOTICE.txt). Future API must make full texts available with shared data and prominently preserve NOTICE and modification attribution in developer documentation. These documents are prepared for that step, not served yet. No DBW or OSM marine rights clearance.


## Verified generation source-license inventory (owner report 2026-11-27)

| Source | License | Record memberships |
|---|---|---:|
| AllThePlaces | CC0-1.0 | 5341 |
| Birdeye | CDLA-Permissive-2.0 | 5 |
| BrightQuery | CDLA-Permissive-2.0 | 33256 |
| DAC | CDLA-Permissive-2.0 | 3489 |
| Foursquare | Apache-2.0 | 20789 |
| Microsoft | CDLA-Permissive-2.0 | 14308 |
| OpenTravel | CDLA-Permissive-2.0 | 91 |
| Overture | CDLA-Permissive-2.0 | 199475 |
| Overture-signals | CDLA-Permissive-2.0 | 168038 |
| PinMeTo | CDLA-Permissive-2.0 | 1 |
| RenderSEO | CDLA-Permissive-2.0 | 8 |
| Uberall | CDLA-Permissive-2.0 | 1303 |
| meta | CDLA-Permissive-2.0 | 121863 |

All 13 combinations reconciled to manifest totals (199475 CDLA, 20789 Apache, 5341 CC0; overlapping). The 11 named external sources agree with official November Places guide and attribution table reviewed 2026-11-27. Overture and Overture-signals have CDLA metadata in the actual dataset; the Places guide and https://docs.overturemaps.org/blog/2026/04/15/release-notes/ describe Overture's confidence-signal processing, but the latter label is not separately itemized in the attribution table. Preserve it as reported; no unsupported separate license assertion. No unknown license observed. This supports a terrestrial-only attribution package, not a legal opinion or publication approval. Overture release notes https://docs.overturemaps.org/blog/2025/09/24/release-notes/ explicitly require a change date for Foursquare data. SailingGo modification date: 2026-11-27 (local generation built on that date; source observation timestamp is 2026-11-18). Preserve this and both publisher/Overture copyright notices. Future API must actually expose required full texts and prominent modification notice before public installation; review all obligations then. Overture upstream has monthly releases and publicly hosted releases ordinarily retained for 60 days; future refresh design must account for changes/removals. Marine rights remain unresolved, not included.

The feature branch now embeds the complete license/NOTICE texts and public modification attribution at exact GET `/api/poi/attribution`. Activation and public delivery remain unverified and require owner approval. Reviewed generation hashes and source counts are pinned; refreshed data requires a new review.
