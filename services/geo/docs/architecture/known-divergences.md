# kacho-geo — known, by-design divergences

Deliberate deviations that are **not** defects. Each is bounded and documented
here so a future reviewer does not re-open them as findings.

## 1. `google.golang.org/grpc` remains a transitive dep of the use-case layer

**What.** After the CQRS + error-mapper refactor (`internal/apps/kacho/api/{region,zone}`),
the use-case packages no longer import `serviceerr`, `grpc/codes`, or
`grpc/status` directly — transport-code selection is injected as an
`ErrToStatus func(error) error` from the composition root (handler-owned
`serviceerr.ToStatus`). However `go list -deps ./internal/apps/kacho/api/zone`
still lists `google.golang.org/grpc` (and `jackc/pgx`).

**Why it is not a defect.** Both come from `github.com/PRO-Robotech/kacho-corelib/operations`,
which every kacho service's use-case imports for the async LRO envelope
(`operations.Repo`, `operations.Run`, `operations.NewFromContext`). The corelib
`operations` package is a horizontal cross-cutting concern (its `Repo` is a
pgx-backed table and its worker persists a `google.rpc.Status`); it is shared
identically by `kacho-vpc`, `kacho-compute`, `kacho-nlb`. The Clean-Architecture
rule this repo enforces is "the **service's own** adapter concerns
(pgx SQLSTATE translation, gRPC-code **selection**) must not live in the
use-case" — that is satisfied: SQLSTATE→sentinel lives in
`internal/repo/kacho/dberr`, and code selection is injected. The residual
transitive import via a corelib horizontal is out of scope and would only be
removable by a workspace-wide corelib redesign.

**Boundary.** If the LRO worker contract were ever changed so the closure could
return a plain domain sentinel (corelib-side mapper), this residual would
disappear. Not planned.

**Direct `geov1` / `protoconv` / `anypb` use in the use-case.** For the same root
cause, the use-cases also import the generated `geov1` stubs and marshal
`domain.Zone`/`domain.Region` into a proto `Any` (`geov1.{Create,Update,Delete}*Metadata`
at the mutation entrypoints; `marshalZone`/`marshalRegion` → `protoconv` + `anypb.New`
inside the `operations.Run` closure). This is **mandated by the corelib LRO
callback contract**, not a geo-local leak: `operations.NewFromContext` takes the
operation-metadata proto, and the `operations.Run(ctx, repo, id, func) (*anypb.Any, error)`
closure signature requires the terminal response to be an `Any`. Every kacho
service (`kacho-vpc`, `kacho-compute`, `kacho-nlb`) emits the LRO metadata/response
`Any` from inside the use-case identically — moving it to the handler would
diverge geo from the platform LRO pattern (godzila regime) without removing the
proto dependency, since the closure runs on the corelib worker, not the handler.
`protoconv` is the single field-mapping projection shared by handler, LRO-recovery,
and this marshal path, so there is no drift. Not a defect; not planned to change.

## 2. Black-box Newman suite not run from this repo

See `tests/newman/README.md`. The suite is tracked as a concrete open ticket
([PRO-Robotech/kacho-geo#10](https://github.com/PRO-Robotech/kacho-geo/issues/10),
`Tests-followup` per rule #12), authored against the deployed stack
(`kacho-deploy`), not `go test` in this repo. The security-critical slices are
covered at the Go layer today: the admin-verbs-not-on-public split by
`cmd/kacho-geo/serve_registration_test.go` (inspects the real
`grpc.Server.GetServiceInfo()` of both listeners), and OperationService
owner-scoping by `internal/handler/operation_owner_test.go` +
`internal/repo/kacho/pg/operation_owner_integration_test.go`.

## 3. Config via corelib `envconfig` struct-tags, not YAML/viper/koanf

**What.** `internal/apps/kacho/config` binds all settings through
`envconfig:"…"` struct-tags via `corelib config.LoadPrefixed("KACHO_GEO")`,
rather than a YAML file loaded through viper/koanf as the evgeniy regime
prescribes.

**Why it is not a geo-local defect.** This is the **platform-wide** config
mechanism: `kacho-corelib/config` exposes `LoadPrefixed`, and every kacho service
(`kacho-vpc`, `kacho-compute`, `kacho-iam`, `kacho-nlb`, …) uses it identically.
Env-only 12-factor config is a deliberate cross-service decision; per-edge TLS
blocks are expressed via env-name prefixing. Migrating to a YAML/viper loader is a
workspace-wide corelib change, not a per-service one — it would be made once in
corelib for all services or not at all. Recorded here so the regime item is not
re-flagged per service.

**Boundary.** If layered/file-based config with hot-reload is ever required
platform-wide, the change lands in `kacho-corelib/config` (keeping the per-edge
TLS structs), and every service picks it up. Not planned.

## 4. Resource-id validated by a `domain.ValidateID` function, not a newtype

**What.** `Region.ID`, `Zone.ID`, `Zone.RegionID` remain bare `string` fields.
The id-format invariant (lowercase slug `^[a-z][a-z0-9-]*$`, hyphen-separated, ≤63
chars) is enforced by `domain.ValidateID`, called from `Region.Validate` /
`Zone.Validate` on the Create path (sec-hardening-r3) — malformed ids are rejected
synchronously with `InvalidArgument` and never persisted as the canonical
cross-service reference key. This closes the substantive gap (no id-format
contract) that the audit flagged.

**Why not a self-validating newtype.** The evgeniy/godzila regime prefers domain
newtypes (`RegionID`/`ZoneID`) over bare primitives. A validating function was
chosen instead because a full newtype rollout ripples through domain structs,
repo scan targets, protoconv, and the reconciler read-ports without adding
enforcement the function does not already provide — the invariant is fully
enforced either way. The newtype refactor is a style-only follow-up (regime
alignment), not a security/consistency gap.

**Note (owner-scope, no admin bypass).** `OperationHandler.Get/Cancel` owner-scope
strictly by creator-principal with **no** cluster-admin bypass (unlike
`kacho-vpc`, which has a `tenant.Admin` cross-cut). geo has no tenant/admin ctx
concept — every mutation already requires `system_admin`, and each operation
belongs to the admin that created it — so a bypass would be dead surface. This is
intentional, not a missing feature.

## 5. Config knobs that are intentionally corelib-default, not geo-tunable

**`check.Options` carries no `CheckTimeout`/`DenyRateLimitPerSec`/`CacheTTL`/
`AllowSystemPrincipal`.** `internal/check` builds the authz interceptor with only
`ServiceName`, `IAMConn`, `Breakglass`, `Logger`; the four rate/timeout/cache
tuning knobs were removed as speculative generality (sec-hardening-r6) — no wiring
path ever set them, so they were always zero. The corelib `authz.NewInterceptor`
applies its own defaults (`CheckTimeout`→2s, cache-TTL 0, no deny-rate-limit,
system-principal not allowed), which is the intended geo posture. If geo ever needs
operator-tunable authz timing it is a new, wired feature — not a re-exposed dead
seam.

**Breakglass is dev-only (never honored in a production posture).**
`validateSecurityConfig` rejects `KACHO_GEO_AUTHZ_BREAKGLASS=true` when
`AuthMode` is `production`/`production-strict` (sec-hardening-r6). Breakglass is a
full bypass of per-RPC authz Check **and** mTLS; honoring it in production would
let a single env flag silently disable all authN/authZ on a deployed stack
(CWE-489), so it is fail-closed at startup exactly like dev-anonymous. It remains
usable only under dev `AuthMode` for a local emergency.

**Audit actor never blank.** `actorFromCtx` returns the sentinel `"unknown"`
(not `""`) when a principal is explicitly present in ctx but carries an empty ID,
so a lost-attribution admin mutation is observable in the `geo_outbox` audit row
itself rather than a silent blank (CWE-778). The normal no-auth path is unaffected
— `operations.PrincipalFromContext` yields `system:bootstrap`, never an empty ID.

## 6. Config defaults are dev-convenience (`AuthMode=dev`, `DBSSLMode=disable`); production posture is fail-closed but not the default

**What.** `internal/apps/kacho/config` defaults `AuthMode` to `"dev"` and
`DBSSLMode` to `"disable"`. In `dev` posture `validateAuthMode` only *warns* on a
plaintext DB connection (it does not fail), and the emergency opt-ins remain
individually honorable. A deploy whose env template forgets
`KACHO_GEO_AUTH_MODE=production` therefore starts in `dev` posture with an
`sslmode=disable` Postgres connection.

**Why it is not a live bypass.** The dangerous relaxations are already fail-closed
by their own explicit gate, so the `dev` default is a *misconfiguration foothold*,
not a live escape:

- **Anonymous / no-mTLS admin is impossible without a second explicit flag.** A
  non-breakglass start — regardless of `AuthMode` — still requires, via
  `validateSecurityConfig`: a non-empty `KACHO_GEO_AUTHZ_IAM_GRPC_ADDR` (per-RPC
  authz Check), `mTLS enabled on both listeners`, and a pinned trusted-forwarder
  SAN **or** the explicit `KACHO_GEO_AUTHZ_TRUST_ANY_FORWARDER=true` dev opt-in.
  So the `dev` default alone does not open an unauthenticated/unauthorized path.
- **Breakglass and trust-any are already fail-closed in production** (§5): both are
  rejected outright under `production`/`production-strict`, and both require an
  explicit env var even in dev. The only behaviors the `dev` default *itself*
  relaxes vs production are (a) plaintext DB → warn-not-fail, and (b) the two
  dev-only opt-ins remain *available if explicitly set*.
- **Production posture is enforced by the deployed stack.** `security.md` mandates
  every deploy run `production-mode` (dev-mode on a cluster is a stated security
  debt), and the Helm/env template — not the binary default — sets it; the
  `production` branch fail-closes on `sslmode=disable`.

**Why the default is not changed here.** `AuthMode=dev` + `DBSSLMode=disable` is the
required posture for local `make dev-up` and the testcontainers/unit fixtures
(local Postgres has no TLS), which `security.md` explicitly permits for
non-deployed fixtures. Flipping the binary default to `production` would fail-close
those mandated dev fixtures at startup. Hardening the default belongs to the
platform (a corelib-level "secure-by-default posture" decision shared by every
kacho service), not a geo-local flip. Recorded here so the dev-default posture is
not re-flagged as a geo defect.

**Boundary.** If the platform adopts a fail-closed default posture, it lands in
`kacho-corelib` (or the shared deploy chart) for all services at once; geo picks it
up unchanged. Until then the invariant that keeps this safe is: **every deployed
stack sets `KACHO_GEO_AUTH_MODE=production[-strict]`** (fail-closed on plaintext DB).

## 7. Orphan-`Update` LRO reconciles to `Done(current)` — reconcile-to-committed-reality, not re-apply

**What.** `operationresolver.resolveExistence` treats `Update`-metadata orphans
exactly like `Create`: resource present → `Done(current)`, absent → `Interrupted`.
If a process crashes after `operations.Create` wrote the LRO row but before the
writer-TX `UPDATE ... RETURNING` committed, the reconciler later finds the resource
present (with pre-update values) and finalizes the operation as `Done` with the
*current* (unchanged) resource — the mutation is reported as a successful operation
even though it never applied (a lost update surfaced as success).

**Why it is by-design (platform contract).** This is the **corelib LRO reconcile
contract**, not a geo-local choice: `kacho-corelib/operations` documents the
resolver semantics as "Create/Update-метаданные: ресурс присутствует →
`{OutcomeDone, current}`" — the reconciler reconciles the operation status to
committed reality and deliberately does **not** re-drive the worker closure
(re-apply). Every kacho service that uses the corelib reconciler (`kacho-vpc`,
`kacho-compute`, `kacho-nlb`) inherits the identical semantics. The resource itself
stays internally consistent (the writer-TX is atomic — it either fully committed or
not at all); only the *operation outcome* of the rare crash-mid-Update window is
optimistic. The resolver header (`internal/operationresolver/resolver.go`) states
this contract explicitly.

**Why not changed / instrumented geo-locally.** The two candidate improvements — a
distinct terminal marker (reconcile-completed vs worker-completed) or resolving
orphan-`Update` to `Interrupted` so the client re-issues — both change the
**platform** reconcile contract, so they belong in `kacho-corelib/operations` (once,
for all services), not as a geo-only divergence that would drift geo from the shared
LRO pattern. No proto/REST contract is affected either way. The `kindUpdate`
dispatch label is retained (§ resolver `kind` enum) precisely as the named
type-level seam where such a future stricter Update-semantics would attach.

**Boundary.** If stricter LRO semantics are ever required, the change is a corelib
`Resolver`-contract revision picked up by every service; geo's
`kindUpdate` case is the attach point. Not planned.

## 8. `resolveExistence` `kind` enum keeps `kindCreate`/`kindUpdate` distinct despite an identical outcome

**What.** The `kind` enum has three values (`kindCreate`, `kindUpdate`,
`kindDelete`) but `resolveExistence` only branches on `kindDelete`; `kindCreate`
and `kindUpdate` fall through to a byte-identical present→`Done` / absent→
`Interrupted` path.

**Why the distinction is intentional, not dead dup.** The identical Create/Update
outcome is the platform reconcile contract (§7), not accidental copy-paste. The
three constants mirror the corelib `Resolver`'s own Create/Update/Delete metadata
taxonomy and keep the `Resolve` switch self-documenting
(`case *UpdateRegionMetadata: … kindUpdate`). Collapsing to a bare `isDelete bool`
(or dropping `kindUpdate`) would trade a two-line reduction for a less-readable
call site (`resolveExistence(ctx, false, …)`) and would erase the type-level seam
that §7's potential future stricter Update-semantics would attach to. This is a
deliberate KISS-vs-self-documentation trade decided in favor of the named labels;
the enum comment states the rationale in-code so a maintainer does not mistake the
identical branch for an omission. Not a defect; not collapsed.

## 9. `Zone.region_id` is immutable — there is no reparent divergence (correction of a false entry)

**Status: NOT a divergence.** This section previously registered "`Zone.Update`
allows re-pointing `region_id` (reparent)" as a deliberate design choice. Every
load-bearing claim in it was false, and the entry is kept — rather than deleted — so
that the correction stays visible to whoever reads this registry as a source of
decisions.

**What is actually true.**

- **`Zone.UseCase.Update` refuses `region_id` synchronously.** Both spellings
  (`regionId`, `region_id`) in `update_mask` are rejected before the mask's known-set
  check with the conventional `InvalidArgument "regionId is immutable after
  Zone.Create"`. `zone.UpdateParams` carries **no** `RegionID` field at all, and the
  repo's `UPDATE` statement does not name the column — so no request shape, masked or
  unmasked, can move a zone between regions.
- **The wire form cannot even carry a new region.** `UpdateZoneRequest` **reserves**
  field 2 and the name `region_id` (`proto/kacho/cloud/geo/v1/internal_catalog_service.proto`),
  and `geo.v1.Zone.region_id` is documented "IMMUTABLE after create".
- **The id↔region relationship IS enforced, and on the create path.**
  `domain.ValidateZoneCoupling` requires `zone.id` to start with `regionId + "-"`
  (strictly — `ru-central10-a` under `ru-central1` is refused), so a zone whose id and
  `region_id` disagree cannot be created in the first place. The old entry's claim
  that "nothing reads the region out of the id" was the opposite of the code.
- **The tests it cited as evidence do not exist.** `TestZoneUpdateFK_NoSuchRegion` is
  not in the tree; `TestZoneUpdateAndOutbox` asserts only that an omitted `region_id`
  is left alone, which is the immutable behaviour, not a reparent path.

**Why immutability is the right answer, not an accident.** A zone is the placement
anchor every zonal resource is coherent against: volumes, network interfaces and
instances are placed by naming it. Moving a zone under a different region would
silently invalidate every one of those placements after the fact, with no way for the
owning services to notice — the coherence rule they enforce at write time would simply
have become false about already-written rows.

**Locked by** `TestZoneCannotBeReparented`
(`internal/repo/kacho/pg/update_mask_integration_test.go`): both mask spellings
refused, an empty-mask full PATCH leaves the stored `region_id` untouched, and a
create whose id is not prefixed by its region is refused before any FK is consulted.
`TestUpdate_immutableRegionId_invalidArg` (`api/zone/zone_test.go`) pins the message.
