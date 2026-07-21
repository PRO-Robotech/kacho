# kacho-geo — Newman black-box coverage

Declarative Newman regression-suite for the **public read surface** of the kacho-geo
placement-axis catalog (Region/Zone), traversing the api-gateway REST mux. Mirrors the
`cases/*.py` → `gen.py` → Postman-collection layout of `kacho-vpc/tests/newman`.

> Prior state: this suite did not exist (coverage gap tracked as
> `PRO-Robotech/kacho-geo#10`). It is now authored (22 cases). It requires a deployed
> stack (api-gateway + kacho-geo + Postgres) and is executed by CI against
> `kacho-deploy`, not from this repo's `go test`.

## Layout

```
tests/newman/
  cases/region.py   cases/zone.py            # declarative Case/Step DSL
  scripts/gen.py                             # cases → collections (Postman v2.1)
  scripts/validate-cases.py                  # dup case-id + CASES-INDEX (hard-fail in CI before newman)
  scripts/run.sh                             # newman runner (--service, --bail, --delay, --jobs)
  collections/*.postman_collection.json      # generated (committed)
  environments/local.postman_environment.json
  docs/{TAXONOMY,CASES-INDEX,TEST-PLAN,RESULTS,PRODUCT-REQUIREMENTS}.md
```

## What is covered (22 cases: region ×11, zone ×11)

Public sync reads — `RegionService.Get/List`, `ZoneService.Get/List`
(`GET /geo/v1/regions[/{id}]`, `GET /geo/v1/zones[/{id}]`):

- **Happy** — List → 200 non-empty well-formed items; List→capture-id→Get → 200 (self-contained).
- **NotFound** — well-formed-absent id → 404 verbatim `"Region|Zone <id> not found"`.
- **Malformed** — non-slug id → 400 `INVALID_ARGUMENT` first statement, no pgx/SQL leak.
- **Pagination** — `pageSize=0`→200 default; `pageSize>1000`→400 (rejected, not clamped);
  garbage `pageToken`→400; opaque token round-trip.
- **authN** — anonymous → 401 `UNAUTHENTICATED` (EXEMPT removes authZ scope, never authN).
- **Two-projection / anonymization** — public body NotContains infra / host-class /
  placement fields (they live only in the Internal projection `:9091`).
- **Internal-vs-external split** — admin write-verb on the public endpoint as a non-admin
  is rejected (401/403/404/501), never 200/mutation.

Source of truth: APPROVED `docs/specs/sub-phase-GEO-1-region-zone-redesign-acceptance.md`
(`# verifies GEO-1-NN` annotations) + `.claude/rules/api-conventions.md`.

## Deployed-contract probe & forward-compatibility (read `docs/TEST-PLAN.md`)

Cases are authored against the **deployed** AS-IS contract of the branch CI runs
(`redesign/integration @ 8f3dca1`), cross-checked against the proto/gateway/serviceerr in
tree. The GEO-1 redesign (EXEMPT ambient-read, two-projection `status`→Internal,
`/geo/v1/internal/…` admin paths, `countryCode°`/`openForPlacement°`, malformed-text flip)
is **not yet landed** here — so assertions lock the invariants that are stable across the
redesign boundary, tolerantly where the contract is mid-flight. Redesign-only scenarios
(GEO-1-20 zero-binding→200, status field-absence, admin `/internal/` CRUD, by-lane
reason-token) are **deferred** and enumerated in `docs/TEST-PLAN.md` §"Deferred — GEO-1
redesign"; they are added to this suite alongside the GEO-1 PR (its DoD requires the
matching newman case). No product-bug red cases and no known-failing cases (see
`docs/RESULTS.md`).

## Admin-CRUD (Internal, :9091) — out of this suite

`InternalRegionService`/`InternalZoneService` (Create/Update/Delete) are Internal-only
(security.md ban #6) and gated `system_admin`; the local stand env exposes only the public
listener, so admin-CRUD conformance stays at the Go layer
(`cmd/kacho-geo/serve_registration_test.go` wiring guard;
`internal/repo/kacho/pg/*_integration_test.go`; `internal/handler/*_test.go`). This suite's
`*-CR-AUTHZ-ADMIN-NOT-PUBLIC` cases are the black-box guard that the admin surface is not
reachable/mutating on the public endpoint.

## Run

```
python3 scripts/gen.py            # regenerate collections
python3 scripts/validate-cases.py # dup-id + CASES-INDEX gate
./scripts/run.sh                  # full run (region + zone)
./scripts/run.sh --service zone   # one collection
```
