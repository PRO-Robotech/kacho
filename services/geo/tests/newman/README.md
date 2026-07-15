# kacho-geo — Newman black-box coverage (planned)

Status: **coverage gap — tracked as `Tests-followup:
[PRO-Robotech/kacho-geo#10](https://github.com/PRO-Robotech/kacho-geo/issues/10)`**
(rule #12 exception: a concrete open ticket, authored/run against a deployed
`kacho-deploy` stack). This repo has strong Go unit + integration (testcontainers)
coverage, but no black-box Newman suite that traverses the api-gateway REST mux
yet. Project rule #12 mandates, for every new RPC, at least one happy-path + one
negative Newman case through the gateway.

> This is **not** an inline-skipped case: the suite lives in a tracked issue with
> an explicit DoD, not a `pm.test.skip` / TODO. The security-critical slices are
> independently guarded at the Go layer today (see "Why not blocking this branch"
> below), so no security regression rides on the suite's absence.

This note is the landing spot for that suite (mirroring the declarative
`cases/*.py` → `gen.py` layout used by `kacho-vpc/tests/newman`). It is
intentionally additive test tooling and requires a deployed stack
(api-gateway + kacho-geo + Postgres), so it is authored against `kacho-deploy`
rather than run from this repo's `go test`.

## RPCs to cover (10)

Public read-only (`:9090` REST via gateway):

- `RegionService.Get`  — `GET /geo/v1/regions/{regionId}`
- `RegionService.List` — `GET /geo/v1/regions`
- `ZoneService.Get`    — `GET /geo/v1/zones/{zoneId}`
- `ZoneService.List`   — `GET /geo/v1/zones`

Internal admin CRUD (`:9091`, internal mux only — MUST NOT be reachable on the
public endpoint):

- `InternalRegionService.Create/Update/Delete` — `/geo/v1/regions[:verb]`
- `InternalZoneService.Create/Update/Delete`   — `/geo/v1/zones[:verb]`

## Required cases

| Case | Kind | Expectation |
|---|---|---|
| `REGION-GET-HAPPY` | happy | seeded region → 200 + body |
| `REGION-GET-NEG-NOTFOUND` | negative | absent id → `NOT_FOUND` |
| `REGION-LIST-HAPPY` | happy | pagination page_size + next_page_token |
| `REGION-LIST-NEG-BADTOKEN` | negative | garbage `page_token` → `INVALID_ARGUMENT` |
| `ZONE-GET-HAPPY` / `ZONE-GET-NEG-NOTFOUND` | happy/neg | mirror region |
| `ZONE-LIST-HAPPY` / `ZONE-LIST-NEG-BADTOKEN` | happy/neg | mirror region |
| `REGION-CREATE-HAPPY` | happy | Operation → poll `OperationService.Get` to `done=true`, response=Region |
| `REGION-DELETE-NEG-HASZONES` | negative | delete region with zones → Operation.error `FAILED_PRECONDITION` |
| `ZONE-CREATE-NEG-GHOSTREGION` | negative | create zone with absent region_id → Operation.error `FAILED_PRECONDITION` |
| `ZONE-UPDATE-NEG-GHOSTREGION` | negative | re-point region_id to absent region → Operation.error `FAILED_PRECONDITION` |
| `ADMIN-NOT-ON-PUBLIC` | security | `InternalRegionService`/`InternalZoneService` verbs unreachable on the public `:9090` REST mux |
| `OP-GET-NEG-FOREIGN` | security | poll another principal's op-id via `OperationService.Get` → `NOT_FOUND` (BOLA owner-scope, sec-hardening-r3) |
| `OP-CANCEL-NEG-FOREIGN` | security | cancel another principal's in-flight op via `OperationService.Cancel` → `NOT_FOUND`, op stays in-flight |

The `ADMIN-NOT-ON-PUBLIC` case is the black-box guard for the Internal-vs-external
split (CLAUDE.md §Запреты #6): an api-gateway restmux misregistration that exposed
an admin verb on the public endpoint would ship green today, because every
existing test calls the Go handlers/use-cases directly and never crosses the
gateway.

## Why not blocking this branch

Authoring + running the suite needs the deployed stack; it cannot be verified by
`go test` in this repo. The security-critical slice of this gap
(`ADMIN-NOT-ON-PUBLIC`) is separately guaranteed at wiring level: admin CRUD
(`InternalRegionService`/`InternalZoneService`) is registered ONLY on the internal
`:9091` server in `cmd/kacho-geo/serve.go` (via `registerServices`), and that
registration split is asserted by **`cmd/kacho-geo/serve_registration_test.go`**
(`TestRegisterServices_InternalAdminNotOnPublic` — inspects the real
`grpc.Server.GetServiceInfo()` of both listeners and fails if any Internal admin
descriptor appears on the public server).

> Correction (sec-hardening-r2): earlier this note claimed the
> `ADMIN-NOT-ON-PUBLIC` wiring was covered by `cert_bound_identity_test.go` /
> `public_principal_test.go`. That was **false** — those tests only verify
> principal anti-spoof trust-gating, never which service is on which listener. The
> gap is now closed by `serve_registration_test.go` (Go wiring guard). The Newman
> black-box case above still remains outstanding for the api-gateway REST boundary
> (restmux verb/path mapping) — tracked in
> [#10](https://github.com/PRO-Robotech/kacho-geo/issues/10) per rule #12; it is
> not a substitute for the Go wiring guard and vice-versa.

## OperationService owner-scoping (sec-hardening-r3)

The `OP-GET-NEG-FOREIGN` / `OP-CANCEL-NEG-FOREIGN` cases exercise the BOLA gate
added in round 3: `OperationService.Get`/`Cancel` are ReBAC-exempt (`Public:true`)
but owner-scoped **in the handler** — a caller that is not the operation's creator
principal gets `NOT_FOUND` (no-leak), and cannot read or cancel a foreign in-flight
admin mutation. This is already covered at the Go layer by
`internal/handler/operation_owner_test.go` (mock) and
`internal/repo/kacho/pg/operation_owner_integration_test.go` (real pgRepo, SQL
ownership predicate). The Newman cases validate the same invariant across the REST
boundary and are enumerated in issue #10.
