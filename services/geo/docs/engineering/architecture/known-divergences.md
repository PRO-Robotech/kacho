# kacho-geo — known, by-design divergences

Deliberate deviations that are **not** defects. Each is bounded and documented
here so a future reviewer does not re-open them as findings.

## 1. `google.golang.org/grpc` remains a transitive dep of the use-case layer

**What.** After the CQRS + error-mapper refactor (`services/geo/internal/apps/kacho/api/{region,zone}`),
the use-case packages no longer import `serviceerr`, `grpc/codes`, or
`grpc/status` directly — transport-code selection is injected as an
`ErrToStatus func(error) error` from the composition root (handler-owned
`serviceerr.ToStatus`). However `go list -deps ./services/geo/internal/apps/kacho/api/zone`
still lists `google.golang.org/grpc` (and `jackc/pgx`).

**Why it is not a defect.** Both come from `pkg/operations`,
which every kacho service's use-case imports for the async LRO envelope
(`operations.Repo`, `operations.Run`, `operations.NewFromContext`). The corelib
`operations` package is a horizontal cross-cutting concern (its `Repo` is a
pgx-backed table and its worker persists a `google.rpc.Status`); it is shared
identically by `kacho-vpc`, `kacho-compute`, `kacho-nlb`. The Clean-Architecture
rule this repo enforces is "the **service's own** adapter concerns
(pgx SQLSTATE translation, gRPC-code **selection**) must not live in the
use-case" — that is satisfied: SQLSTATE→sentinel lives in
`services/geo/internal/repo/kacho/dberr`, and code selection is injected. The residual
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

## 2. NOT a divergence any more — the black-box Newman suite lives here and CI runs it

**Status: withdrawn.** This entry registered "black-box Newman suite not run from this
repo" as an accepted gap, pointed at a ticket in the pre-monorepo `kacho-geo` repository,
and named a deploy repository that no longer exists as a separate tree. Every load-bearing
part of it is now false, and the entry is corrected rather than deleted so that nobody
re-opens the work it says is outstanding.

- **The suite is in this repository.** `services/geo/tests/newman/` carries 7 declarative
  cases and the 7 collections generated from them, plus `docs/CASES-INDEX.md` and
  `docs/RESULTS.md`. Predicate: `git ls-files services/geo/tests/newman/cases | wc -l`.
- **CI runs it.** `.github/workflows/e2e-newman.yml` runs geo as one of eight suites and
  carries a green-gate step for it (search the workflow for the geo suite gate).
- **The tracking ticket is not reachable as written.** It pointed into the repository the
  service was developed in before the move to the monorepo; development there stopped in
  July 2026. An accepted-gap entry whose ticket cannot be moved has no expiry predicate,
  which is the reason this one outlived its subject silently.

What remains true and is the only reason to read this section: the Go-layer coverage the
entry cited is real and still the fast gate — the admin-verbs-not-on-public split
(`services/geo/cmd/kacho-geo/serve_registration_test.go`, which inspects the real
`grpc.Server.GetServiceInfo()` of both listeners) and OperationService owner-scoping
(`services/geo/internal/handler/operation_owner_test.go` plus
`services/geo/internal/repo/kacho/pg/operation_owner_integration_test.go`). All three
paths are given from the repository root, because a bare file name cannot be checked.

## 3. Config via corelib `envconfig` struct-tags, not YAML/viper/koanf

**What.** `services/geo/internal/apps/kacho/config` binds all settings through
`envconfig:"…"` struct-tags via `config.LoadPrefixed("KACHO_GEO")` from `pkg/config`,
rather than a YAML file loaded through viper/koanf as the evgeniy regime
prescribes.

**Why it is not a geo-local defect.** This is the **platform-wide** config
mechanism: `pkg/config` exposes `LoadPrefixed`, and every kacho service
(`kacho-vpc`, `kacho-compute`, `kaname`, `kacho-nlb`, …) uses it identically.
Env-only 12-factor config is a deliberate cross-service decision; per-edge TLS
blocks are expressed via env-name prefixing. Migrating to a YAML/viper loader is a
workspace-wide corelib change, not a per-service one — it would be made once in
corelib for all services or not at all. Recorded here so the regime item is not
re-flagged per service.

**Boundary.** If layered/file-based config with hot-reload is ever required
platform-wide, the change lands in `pkg/config` (keeping the per-edge
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

**Note (owner-scope, no admin bypass).** `operationspb.Handler.Get/Cancel`
(общий слой `pkg/operations/operationspb`, куда полоса сведена из семи копий)
owner-scope strictly by creator-principal with **no** cluster-admin bypass.
Прежде здесь стояло «unlike `kacho-vpc`, which has a `tenant.Admin` cross-cut» —
это было неверно И ДО сведения: комментарий снятого обработчика vpc гласил
«admin-bypass тут не применяется». Расхождения не было, а противопоставление
пережило свой предмет. geo has no tenant/admin ctx
concept — every mutation already requires `system_admin`, and each operation
belongs to the admin that created it — so a bypass would be dead surface. This is
intentional, not a missing feature.

## 5. Authz knobs are geo-chosen, and the descriptor refuses to start without them

**geo no longer builds the authz interceptor itself, and never grew its own `check` package.**
Predicate: `git ls-files services/geo/internal/check | wc -l` → 0. The
service host (`pkg/servicehost`) builds the decision link — one construction site for
all services — from values the descriptor carries. Two of the knobs this section used to
call "intentionally corelib-default" are now **chosen** rather than inherited, and that
is the point rather than a rename: the revocation window (`KACHO_GEO_AUTHZ_CACHE_TTL`)
and the per-question deadline (`KACHO_GEO_AUTHZ_CHECK_TIMEOUT`) are security parameters,
and a value nobody selected can be neither discussed nor narrowed on a particular
deployment. The descriptor **refuses to start** when either is left unset.

**The deny-storm budget is likewise chosen, not inherited — this entry used to say the
opposite.** The previous edition claimed the budget "stays unset", that an unset budget
"means the storm budget is OFF", and that saying otherwise "would state a protection that
is not there". That is now backwards: the descriptor declares the budget as a **value**
(`Spec.DenyBudget`, fed from `KACHO_GEO_AUTHZ_DENY_BUDGET_PER_SEC`, default 100/s per
principal), and the axis has **no "not applicable" form** for geo — the contract refuses
to start both when the budget is left undeclared and when it is declared non-positive.
The in-code rationale is at the assignment site: geo does not decide access in its own
process, it asks kaname, so the network peer a deny-storm could knock over exists, and
an exemption ("nobody to knock over") is only open to the model owner.

Predicate, so this entry can be re-checked instead of believed:
`grep -n 'DenyBudget' services/geo/cmd/kacho-geo/serve.go` and
`grep -n 'DENY_BUDGET' services/geo/internal/apps/kacho/config/config.go`.

Why the correction is kept rather than the old text quietly swapped: a document that
declares a protection **absent** while it is present is not a harmless understatement.
It reads as licence to add the missing guard (there is nothing to add), and it invites the
opposite reading of the service's posture in any review that trusts this registry — which
is exactly what this file is for.

**Breakglass is gone entirely — there is no emergency bypass knob.** The full account of
what replaced it, and why a chain without the decision link is not expressible on the
service host at all, lives in one place: `authz-and-tuples.md` § "Отказ старта вместо
аварийного обхода". It is not restated here — two accounts of one decision diverge on the
first correction, and this pair already did.

**Audit actor never blank.** `actorFromCtx` returns the sentinel `"unknown"`
(not `""`) when a principal is explicitly present in ctx but carries an empty ID,
so a lost-attribution admin mutation is observable in the `geo_outbox` audit row
itself rather than a silent blank (CWE-778). The normal no-auth path is unaffected
— `operations.PrincipalFromContext` yields `system:bootstrap`, never an empty ID.

## 6. Orphan-`Update` LRO reconciles to `Done(current)` — reconcile-to-committed-reality, not re-apply

**What.** `operationresolver.resolveExistence` treats `Update`-metadata orphans
exactly like `Create`: resource present → `Done(current)`, absent → `Interrupted`.
If a process crashes after `operations.Create` wrote the LRO row but before the
writer-TX `UPDATE ... RETURNING` committed, the reconciler later finds the resource
present (with pre-update values) and finalizes the operation as `Done` with the
*current* (unchanged) resource — the mutation is reported as a successful operation
even though it never applied (a lost update surfaced as success).

**Why it is by-design (platform contract).** This is the **corelib LRO reconcile
contract**, not a geo-local choice: `pkg/operations` documents the
resolver semantics as "Create/Update-метаданные: ресурс присутствует →
`{OutcomeDone, current}`" — the reconciler reconciles the operation status to
committed reality and deliberately does **not** re-drive the worker closure
(re-apply). Every kacho service that uses the corelib reconciler (`kacho-vpc`,
`kacho-compute`, `kacho-nlb`) inherits the identical semantics. The resource itself
stays internally consistent (the writer-TX is atomic — it either fully committed or
not at all); only the *operation outcome* of the rare crash-mid-Update window is
optimistic. The resolver header (`services/geo/internal/operationresolver/resolver.go`) states
this contract explicitly.

**Why not changed / instrumented geo-locally.** The two candidate improvements — a
distinct terminal marker (reconcile-completed vs worker-completed) or resolving
orphan-`Update` to `Interrupted` so the client re-issues — both change the
**platform** reconcile contract, so they belong in `pkg/operations` (once,
for all services), not as a geo-only divergence that would drift geo from the shared
LRO pattern. No proto/REST contract is affected either way. The `kindUpdate`
dispatch label is retained (§ resolver `kind` enum) precisely as the named
type-level seam where such a future stricter Update-semantics would attach.

**Boundary.** If stricter LRO semantics are ever required, the change is a corelib
`Resolver`-contract revision picked up by every service; geo's
`kindUpdate` case is the attach point. Not planned.

## 7. `resolveExistence` `kind` enum keeps `kindCreate`/`kindUpdate` distinct despite an identical outcome

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

## 8. `Zone.region_id` is immutable — there is no reparent divergence (correction of a false entry)

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
(`services/geo/internal/repo/kacho/pg/update_mask_integration_test.go`): both mask spellings
refused, an empty-mask full PATCH leaves the stored `region_id` untouched, and a
create whose id is not prefixed by its region is refused before any FK is consulted.
`TestUpdate_immutableRegionId_invalidArg` (`services/geo/internal/apps/kacho/api/zone/zone_test.go`) pins the message.

## 9. geo keeps a change feed but does **not** serve the platform subscription verb — narrowing has nothing to ask

**What.** Every other domain that keeps a resource change feed serves
`kacho.cloud.subscription.InternalSubscriptionService/Subscribe`, whose edge
projection is `/subscription/v1/events`. geo keeps `geo_outbox` — a feed by shape
(`sequence_no`, `resource_kind ∈ {Region, Zone}`, `resource_id`, `event_type`,
`payload`) — and deliberately does not serve the verb. Reviewers who count
"domains with a feed" against "domains that serve" will find geo missing; this is
a decision, not an omission.

**Why it is not merely unimplemented — the mechanism cannot accept this feed.**
`subscription.Mapping.validate` (`pkg/subscription/journal.go`) requires a
non-empty kind dictionary in which **every kind is an object type of the rights
model**, and it says why in the refusal itself: without an object type there is
no way to ask whether a given caller may see a given row. The rights model
(`services/iam/internal/authzmodel/fga_model.fga`) declares 32 object types and
**zero** of them are `geo_*`. So geo cannot name one legal kind, and a wired geo
owner would refuse at start-up rather than serve an empty stream.

**Why the missing types are themselves correct.** Region and Zone are the
admin-curated global placement catalogue, and their public reads are a documented
project-scope exemption: every authenticated tenant must be able to read them in
order to place any resource at all. Per-row narrowing over that catalogue has a
single answer for every caller — "yes". Introducing `geo_region`/`geo_zone` object
types purely so that the mandatory narrower can answer "yes" to everyone would be
a check with the form of a check and none of the substance, and it would put the
catalogue back inside the per-object authz it was deliberately taken out of.

**What the feed is actually for.** `geo_outbox` records admin mutations for
attribution (see `authz-and-tuples.md`: audit-only) and is read by cursor. It is
not a tenant-facing view of tenant-owned resources, which is what the subscription
verb streams.

**Consumers, measured.** There is no `geo` console module (`ls ui-future/` → the
domain is absent), and no caller polls the catalogue on a loop: it is fetched once
and cached, because admin-curated catalogues change on the order of months. Wiring
the verb here would create the one thing the epic forbids — a surface with no
consumer.

**What would reverse this.** A `geo_*` object type appearing in the rights model:
at that point narrowing becomes a real question and the decision must be re-taken.
That reversal is not left to memory — `internal/repohygiene`
`TestEveryDomainEitherServesSubscriptionOrRecordsWhyNot` pins the premise and
fails the run if geo ever declares such a type, and equally if geo starts serving
the verb while this entry still claims it does not.

**Recorded for** kacho#1023, whose readiness predicate demanded seven owners out
of seven and would otherwise stay red against a correct tree forever.
