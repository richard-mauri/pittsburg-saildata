# Current work — SailingGo

**Active:** Homepage [Issue #45](https://github.com/richard-mauri/homepage/issues/45) policy adoption on `docs/45-suite-policy-adoption` from current GitHub `main`; [draft PR #27](https://github.com/richard-mauri/pittsburg-saildata/pull/27) open against `main`. Generated local v1.0.0 policy copy pins Homepage merge `502f56de374f094eec2e6d5107c89d2d963f44fd` and adds offline/online integrity/freshness checks. Documentation and validation tooling only; no consumer merge authorized.

**Merged context:** Build v404, semantic v1.16.0 was previously documented. Homepage guest/named identity, guest browser-local choices, named revisioned settings, activity verification and direct-visit recovery are implemented. NOAA measured wind, predicted tidal currents, forecasts, station/location versus viewport, History, Planning, overlays, text/JSON schemas and tunnel are unchanged.

**Next:** verify links, policy copy and exact draft PR-head CI with the other consumer PRs. The new policy check is **not** a Go CI test suite. No Mac/Safari restart for docs-only work. Preserve possible unpushed `~/projects/sailing-go` changes and private data. Owner approval is required before merge, tag, deploy, release or version change.
