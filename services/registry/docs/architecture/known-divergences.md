# Known architectural divergences — kacho-registry

Deliberate, reviewed deviations from a strict Clean-Architecture reading. Recorded
here (per workspace CLAUDE.md «Не баг … Документировать осознанные by-design
отклонения») so they are not re-filed as defects. Each entry states the rule, the
divergence, why it is accepted, and what would change the decision.

## 1. Use-case layer imports gRPC status + proto stubs (LRO envelope)

**Rule.** Clean Architecture: `service/` (use-case) imports only `domain`; grpc-stubs
/ `status` / proto are adapter concerns and belong in `handler/`.

**Divergence.** `internal/apps/kacho/api/registry` imports `registryv1` proto-stubs and
`anypb` (`ProtoRegistry`, `CreateRegistryMetadata`, `registryAny`) — the LRO envelope.

**R3 update (2026-07-05, sec-hardening-r3).** The *error path* leak flagged by the 3rd
audit is closed: the use-case no longer imports `google.golang.org/grpc/codes` /
`status` and no longer hand-codes gRPC codes. Every use-case error is now expressed as a
`regerrors.*` domain sentinel and mapped through the **single** `serviceerr.ToStatus`
seam via the `errmap.go` helpers (`failInvalidArg` / `failFailedPrecondition` /
`failAlreadyExists` / `failUnavailable`). `grep -R 'grpc/status\|grpc/codes'
internal/apps/kacho/api/registry` is now empty. Exact error texts are preserved (the
sentinel wraps the same message; `serviceerr` strips the sentinel prefix), so the wire
contract is unchanged. The residual documented below is now **only** the proto/`Any` LRO
envelope, not error transport.

**Why the proto/`Any` residual is accepted.**
- **Inherent to the kachō async-LRO pattern.** Every mutation returns an
  `operation.Operation`; its `response`/`metadata` are `google.protobuf.Any`, and the
  worker closure that finalises the operation lives *in the use-case* (it captures the
  request-ctx principal and the created domain object). Serialising the domain result
  into a proto `Any` therefore happens inside the use-case by construction — the proto
  import cannot be removed no matter how the error path is refactored. This matches the
  established kachō LRO layout (godzila skill: "async Operation LRO envelope", use-case
  owns the worker). `corelib operations.Run` maps the worker error via
  `status.FromError`, so the worker closure must yield a gRPC status (a bare sentinel
  would collapse to INTERNAL) — the `serviceerr` seam supplies exactly that.
- **Established test contract.** 25+ use-case unit assertions call `status.Code()` on
  the use-case return (the use-case *is* the layer producing the gRPC-shaped LRO
  envelope); inverting to bare sentinels would require rewriting them for no wire change.

**What would revisit this.** If a non-gRPC transport is ever added, extract the
proto-`Any` serialisation behind a mapper injected at the composition root. Until then
the coupling is confined to the LRO envelope and reconciled by a single mapper.

## 2. Data-plane OCI proxy has no separate use-case layer

**Rule.** Thin handler: no domain-state branching or resource-lifecycle side-effects in
transport (`handler/`).

**Divergence.** `internal/dataplane/handler.go` decides the push verb from repo state
(`RepoExists → v_update@repository` else `v_create@registry`), emits the
register-on-first-push intent, and encodes the cross-repo mount exfil-guard directly in
the HTTP handler; there is no `service/` layer under `internal/dataplane`.

**Why accepted.**
- The data-plane is a **transport-level OCI auth-proxy**, deliberately designed as an
  orchestrator over injected ports (`TokenVerifier`, `Authorizer`, `Backend`,
  `Forwarder`, `RepoRegistrar`, `RegistryLookup`) — parse → verify → authz → forward.
  Its "business logic" is a small, fixed authorization policy (exists→verb table,
  exfil-guard, register-on-first-push), not a rich domain model.
- That policy is **fully unit-tested** against fakes in
  `internal/dataplane/handler_test.go` (verb selection, existence-hiding, mount guard,
  register-on-first-push emission), so it is not un-testable transport code in practice.
- The authorization vocabulary it uses is already centralised in `internal/domain`
  (verb-relations, object refs, subject encoding) — the transport only *applies* it.

**What would revisit this.** If a second consumer needs the same "first-push
materialises repo authz object" decision, or the verb-mapping policy grows beyond the
current fixed table, extract a `dataplane` use-case (AuthorizePush / AuthorizePull /
RegisterOnFirstPush) owning the decision and leave the handler to translate it to HTTP
status codes.

## 3. Cross-service (registry ↔ zot) TOCTOU windows are software-validated, not DB-enforced

**Rule.** CLAUDE.md «Within-service refs — DB-уровень обязателен» (#10): every reference
and invariant *inside one service DB* must be a DB construct (FK / UNIQUE / EXCLUDE /
CAS); software `check → act` is forbidden.

**Divergence (by rule's own cross-service exception).** Two narrow windows exist across
the registry-DB ↔ zot boundary — a *different DB / different service* boundary that rule
#10 and #8 explicitly exempt (no cross-DB FK possible):

- **Delete-vs-push** (`internal/apps/kacho/api/registry/delete.go`): `doDelete` CAS-marks
  the registry `DELETING`, re-checks `zot.NamespaceEmpty` **after** the CAS, then
  physically DELETEs the row + emits the unregister-intent. A push authorized *before*
  tuple removal could land content in the gap between the second `NamespaceEmpty()==true`
  check and the final DELETE.
- **DeleteTag emptiness** (`deletetag.go` `unregisterRepoIfEmpty`): after deleting the
  last tag it reads `zot.ListTags`; if empty **and the repo has no overlay row** (i.e. it
  is ephemeral and therefore ceased to exist together with its content) it emits the repo
  unregister-intent. A push landing a new tag between the read and the emit strips a
  parent-tuple the push just created. The window applies to ephemeral repos only: a
  durable repo outlives emptiness — it stays readable and listable, so its authz object
  is never stripped here (only `DeleteRepository` removes it, in the same transaction as
  the row).

**Why accepted.**
- **The boundary is cross-service** (registry Postgres vs zot's own store) — DB
  constraints cannot span it (database-per-service, #8). Rule #10 keeps software
  validation + graceful dangling-ref as the sanctioned pattern precisely here.
- **Defense-in-depth already in place.** Delete is `forward-only` and re-checks
  `NamespaceEmpty` *after* the CAS→DELETING (a real second guard, not a single
  check-then-act), and `zot`-unavailable → `Unavailable`/`FailedPrecondition`
  (fail-closed, retriable) rather than a destructive erase.
- **Self-healing.** Register-on-first-push re-materialises any stripped repo/namespace
  authz object on the next push; the transient state is an existence-hiding `NOT_FOUND`,
  never a cross-tenant leak or data-loss of committed metadata.

**What would revisit this.** A true write-fence: gate data-plane push authorization on
registry status (deny push while `DELETING`) so `DELETING` becomes a hard fence, and/or
order the register/unregister intents by a per-repo monotonic marker (the
`registry_outbox.source_version` BIGSERIAL from migration 0002 already gives
commit-order monotonicity for registries — extending it to repo intents would let
register-on-push always win). Both are behavioural changes to the push path and are
tracked as follow-ups rather than folded into a contracts-frozen hardening pass.

## 4. Platform config via envconfig struct-tags (not viper/koanf YAML)

**Rule.** evgeniy regime: service config is YAML via viper/koanf, not `envconfig`
struct-tags.

**Divergence.** `internal/apps/kacho/config/config.go` loads config from env via
`corelib config.LoadPrefixed` with `envconfig:"…"` struct-tags.

**Why accepted.** This is the **platform-wide** corelib convention
(`corecfg.LoadPrefixed` is used identically by every `kacho-*` service). It is a
regime-conformance choice made once at the corelib layer, not a registry-local defect;
changing it is a workspace-wide migration of `kacho-corelib/config` under a dedicated
release phase, out of scope for a single-service hardening pass. No runtime defect —
only the layered-profile / hot-reload affordances of viper/koanf are unavailable.

**What would revisit this.** A platform decision to migrate `kacho-corelib/config` to
viper/koanf YAML with env override; then every service (including this one) follows.

## 5. Authenticated-deny → 404 existence-hiding: live e2e assertion blocked on test infra

**Rule.** CLAUDE.md #12: security invariants are enforced end-to-end, not only by unit
fakes.

**Gap (test-infrastructure, not code).** The core tenant-isolation invariant —
*authenticated non-member sees `NOT_FOUND` (existence-hidden), never a 403 leak, never
success-with-data* — has no **live** black-box (Newman-through-gateway) assertion. The
single-user dev stand registers exactly one IAM identity (cluster-admin); a dev-JWT with
an unregistered `sub` is treated as `UNAUTHENTICATED` (401), so `jwtStranger` cases
cannot drive an *authenticated-but-ungranted* request, and the viewer-tier cases are
fixture-gated (SKIP while `jwtProjectViewerA` is empty). See
`tests/newman/cases/registry-authz.py` docstring.

**Current mitigation (runnable in CI, no stand).**
- Real authz-seam: `internal/check/viewer_boundary_test.go` runs the **real** corelib
  authz-interceptor over the registry `PermissionMap` with a fake `CheckClient` granting
  exactly `v_get`, asserting Update/Delete → `NOT_FOUND` (existence-hidden) for an
  authenticated principal. Not a handler fake — the production interceptor + map.
- Handler ScopeFiltered path: `internal/handler/listauthz_test.go`
  (`TestHandler_REG22_ListRepositories_NamespaceDeny_NotFound`, `REG24` deny) drives an
  **authenticated** principal (`carolCtx`) with a denying authorizer →
  `NOT_FOUND`, and `filterRegistries`/`filterRepos` return empty (not 403) — the exact
  production `repoAuthz` logic, only the network Check faked.

**Why not closed here.** Closing the *live* gap requires provisioning a second IdP
identity + a project-scoped viewer grant on the deployed stand — test-environment
infrastructure, not a contract-safe code change, and not exercisable from a
build/test-only hardening pass. Shipping Newman Python that cannot be run here would be
unverified test code (against the verification discipline).

**What would revisit this.** Provision `jwtProjectViewerA` (second IdP identity + viewer
role grant) on the stand; the three fixture-gated viewer cases in `registry-authz.py`
then enforce authenticated-deny→404 automatically with no code change.

## 6. ScopeFiltered-RPC row-filter / existence-hiding lives in `handler/listauthz.go`, not the use-case

**Rule.** Thin handler: no domain-state / security decisions in transport; per-object
authz belongs with the use-case.

**Divergence.** For the three `ScopeFiltered` collection RPCs
(`List` / `ListRepositories` / `ListTags`) the per-object authz — row-filter,
existence-hiding (`deny → NOT_FOUND`), fail-closed on iam.Check error — is applied by
`internal/handler/listauthz.go` (`repoAuthz.filterRegistries` / `filterRepos` /
`namespaceGate` / `checkRepo`), *after* the use-case returns the unfiltered set. These
RPCs are deliberately marked `ScopeFiltered` so the per-RPC authz-interceptor skips them
(a single-object Check cannot express row-filter + existence-hiding at once).

**Why accepted.**
- `repoAuthz` is a **distinct authz component** wired into the handler, not ad-hoc
  transport branching — the package doc treats `use-case/authz` as a peer decision layer
  («ветвления по domain-state — в use-case/authz»). It is the *same* `Authorizer` port
  and the *same* centralised `internal/domain` verb-relations / object-refs the
  interceptor and data-plane use; drift between planes is structurally excluded.
- It is **directly unit-tested** as the production authz seam
  (`internal/handler/listauthz_test.go`: authenticated-deny → `NOT_FOUND`, filters return
  empty not 403) — see divergence #5 — so it is not un-testable transport code.
- Pushing the filter into the use-case would force the use-case to emit transport-shaped
  `NOT_FOUND`/`UNAVAILABLE` existence-hiding (a gRPC-status concern) or a bespoke
  hidden-existence sentinel + rewiring the `Authorizer` port through every `List*`
  signature and its unit tests — trading one layering seam for another with no wire
  change and real regression surface on security-critical code.

**What would revisit this.** A second consumer of `uc.List` / `uc.ListRepositories`
(e.g. a REST projection or a new admin RPC) that must not re-implement the filter:
introduce an authz-scoped list use-case returning already-filtered domain results plus a
hidden-existence sentinel, and reduce the handler to sentinel→gRPC-status translation.
Until a second caller exists, the single filtered path is the whole surface and the risk
the finding describes (a future caller forgetting to filter) has no live instance.

## 7. Data-plane harness is not invoked by any workflow (the collections ARE)

**Rule.** testing.md: the Newman/Postman suite is the primary regression infra; a new
RPC/field ships its Newman case in the same PR.

**Divergence — уточнена, прежняя формулировка была неверна.** Здесь стояло, что
чёрно-ящичные коллекции registry «не вызываются НИ ОДНИМ workflow». Это перестало быть
правдой: `.github/workflows/e2e-newman.yml` поднимает стек и несёт шаг-гейт «newman
зелёный (registry)» с рабочим каталогом `services/registry/tests/newman`, а
`deploy/scripts/newman-parallel.sh` держит `registry` в списке суит (и намеренно гоняет
её одним потоком — материализация прав тяжёлая). Так что коллекции исполняются.

Что действительно НЕ вызывается ничем — **сквозной харнесс плоскости данных**
(`scripts/dataplane-e2e.sh`): ни один workflow и ни один скрипт стенда его не зовёт
(проверяется поиском по имени файла — ноль вхождений вне самого файла, его самопроверки
и документов). Его инварианты — 401-вызов без токена, регистрация при первой записи,
запрет разрушительного удаления на плоскости данных, обход по закодированному
разделителю, классификация артефакта — держатся только ручным прогоном. Это открытый
долг, а не принятое решение.

Per-repo `ci.yaml` по-прежнему гоняет только сборку/юниты/интеграцию/линт/govuln — это и
есть исходная часть расхождения, и она сохраняется.

**Why accepted.**
- Newman is a **through-the-gateway black box**: it needs a live api-gateway + Hydra
  token-exchange + IAM/OpenFGA + zot + Postgres — i.e. the aggregate deployed stack. Per
  `polyrepo.md`, e2e-through-api-gateway is owned by the deployed stand
  (`kacho-deploy` / `kacho-test`), not by a per-service unit-CI runner that has no such
  stack. Spinning the full multi-service topology inside this repo's `ci.yaml` would
  duplicate the deploy repo's responsibility.
- The cases/collections **are** authored, `validate-cases.py`/`gen.py`-clean and
  committed, so the regression assets exist and are review-gated; only their *execution*
  is deferred to the stand. Shipping a CI job that cannot bring up the stack would be a
  perpetually-red or perpetually-skipped job (no signal), which the verification
  discipline forbids.
- REST-contract / gateway-wiring / cross-service-authz regressions are additionally
  guarded here by build-time seams: `internal/handler/*_test.go` (ScopeFiltered authz),
  `internal/check/viewer_boundary_test.go` (real corelib interceptor + PermissionMap),
  and the dataplane unit suite.

**What would revisit this.** Для коллекций это уже произошло — их гоняет
`e2e-newman.yml` на поднятом стеке. Остаётся харнесс плоскости данных: у него есть всё,
чтобы быть вызванным (обязательные переменные проверяются на старте, вердикт в числах,
шаг, снятый с прогона, роняет код возврата, и есть самопроверка честности вердикта
`make -C services/registry dataplane-e2e-selftest`, которой стенд не нужен). Не хватает
шага, который выдаёт ему ключ служебной учётки и реестр с правом записи. Пока такого
шага нет, перечисленные инварианты плоскости данных **не проверяются автоматически** —
и это записано здесь как число, а не как принятое решение.

## 8. `AuthMode` Go-config default is `dev` (production posture set by the deploy profile)

**Rule.** security.md: «Любой деплой — production-mode (anonymous fail-closed) + mTLS/JWT»
— every deployment must run in production posture; the dev anonymous-fallback is for local
fixtures only, never a deployed stand.

**Divergence.** `internal/apps/kacho/config/config.go` defaults `AuthMode` to `"dev"` (and,
consistently, `HydraJWKSURL` to a plaintext `http://` in-cluster URL, `HydraIssuer` to `""`,
`DBSSLMode` to `disable`). When `KACHO_REGISTRY_AUTH_MODE` is unset the service falls back to
`dev`, in which the data-plane guards `requireSecureJWKSURL` / `requireIssuerPinned`
(serve.go) are skipped — so the identity-JWT trust anchor may be fetched over http:// with
issuer-pinning off.

**Why accepted.**
- **Platform-wide convention, not a registry-local choice.** Every `kacho-*` service
  defaults its `<SVC>_AUTH_MODE` to `dev` in Go config (e.g. `kacho-geo`
  `KACHO_GEO_AUTH_MODE default:"dev"`) and hardens via the deploy layer: the umbrella
  prod profile (`kacho-deploy/helm/umbrella/values.prod.yaml`, per-subchart `values.yaml`)
  sets `AUTH_MODE=production` + DB `sslmode=require`. security.md's «любой деплой —
  production-mode» is satisfied by that deploy-profile override, not by the Go default —
  the `dev` default exists purely for local `make dev-up` ergonomics.
- **Blast radius is bounded independently of `AuthMode`.** `validateSecurityConfig`
  (serve.go) fail-closes the *control-plane* regardless of mode: without breakglass, per-RPC
  authz `Check` (IAM addr) **and** mTLS on **both** gRPC listeners (:9090/:9091) are
  mandatory or the process refuses to boot. `AuthMode` toggles only the *data-plane* JWKS
  transport/issuer-pinning strictness and a DB-SSL warning — it never relaxes gRPC authN/authZ.
- **Fail-closed in production.** Under `AUTH_MODE=production[-strict]` the data-plane rejects
  a non-https JWKS URL, an empty issuer, or an unacknowledged plaintext listener at startup
  (`requireSecureJWKSURL` / `requireIssuerPinned` / `requireDataplaneTLSAck`, regression-tested
  in `serve_test.go`), so a real deployment cannot silently run the weak trust anchor or expose
  bearer tokens on cleartext.

**What would revisit this.** A platform decision to flip the corelib/service convention to a
secure-by-Go-default (`AuthMode` default `production`, with an explicit `dev` opt-in for local
stands) — applied uniformly across all `kacho-*` services so registry does not diverge from
its siblings. Until then the deploy profile is the single enforcement point and this default
matches every peer service.

## 9. ~~Register-on-first-push is a best-effort emit; a lost first-push intent is not reconciled~~ — CLOSED (2026-07-30)

> [!note] This is no longer a divergence. Kept as a record of what was accepted and what
> replaced it — not as a standing exemption.
>
> Re-checked against the tree on 2026-08-01: the shape described below does not exist any more.
> Fixed by commit `5d599e64` («право на запись решается существованием РЕСУРСА, а регистрация
> не теряется молча»), which landed **after** this section was last written — so until now the
> document advertised as *accepted* a defect the repository had already closed. That is the
> more dangerous half of a stale divergence list: the next auditor re-accepts it, or someone
> reverts the fix on the document's authority.

**Rule.** data-integrity.md: a state mutation and its outbox emit are atomic / no partial
writes; a lost intent must be recoverable.

**What it was.** On the first successful manifest-PUT of a *new* repo, the data-plane had
already written content to zot and relayed the 2xx before it emitted the repo register-intent
into `registry_outbox`. A failed emit was logged and the client kept its success, and because
the register branch keyed off «does this repo exist», later pushes skipped registration — so an
intent lost to a transient registry-DB error was never re-emitted, and there is no
registry-side reconciler. The contract test pinned *log-and-continue* as intended behaviour.

**Failure mode it had (why it was defensible at the time).** Fail-**closed**, never a leak: the
intent carries exactly the containment and owner tuples, so losing it means the repo
materialises **no** relations at all and the owner's own pull returns existence-hiding
`NOT_FOUND`. The cost was denial of the pusher's own access, not cross-tenant reach. The two
writes also straddle a service boundary (zot's store vs registry Postgres), so a single
transaction was never available (database-per-service, ban #8) — the same exemption as
divergence #3.

**What replaced it.** Registration is now **synchronous and fail-closed** on the first push of
a new repo: the data-plane no longer streams that response, it buffers zot's reply, resolves
the project and makes the register-intent durable **before** relaying success. If the emit
fails, the buffered 2xx is discarded and the client gets `503` — so the push is not reported as
having succeeded, and the client's retry re-enters the create branch instead of being told the
repo already exists. The existence predicate moved from «engine has tags» to durable service
state, which is what makes that retry converge. The contract test now asserts the **opposite**
of the old text and says so in its own header: the previous assertion was pinning the defect.

**Residual, unchanged and still true.** There is still no registry-side reconciler or sweeper
for register-intents. It is no longer load-bearing — an intent can no longer be silently lost,
because a failed emit produces no success and no durable resource — but a general re-emit sweep
remains a legitimate belt-and-braces follow-up rather than a gap.

## 10. `GetRegistryStats` walks the whole namespace live (bounded-concurrency, not paginated)

**Rule.** CWE-770 / OWASP A05: a single request must not fan out unbounded downstream work.

**Divergence.** `internal/clients/zot/distribution.go` (`Stats`, reached via
`InternalRegistryService.GetRegistryStats`, :9091) enumerates every repo in the namespace, then
every tag, then fetches every manifest to sum blob sizes. Work is O(total tags in the namespace)
with no page-size bound or early cutoff.

**Why accepted.**
- **Admin-only, authz-gated Internal surface.** `GetRegistryStats` is on the cluster-internal
  :9091 listener only, and per-RPC `Check` gates it at the viewer tier (`v_get` on
  `registry_registry`, `permission_map.go`) — internal is *not* exempt (security.md). It is not
  reachable by tenants or from the public endpoint.
- **Instantaneous downstream load is already bounded.** Manifest fetches run under an
  `errgroup` capped at `blobScopeConcurrency` (8) — at most 8 concurrent zot round-trips at any
  moment (`blobScopeConcurrency` in the zot client), so a large namespace makes Stats *slow*, not a
  connection-budget spike on shared zot. Each manifest body is additionally read under
  `io.LimitReader(maxManifestBytes)` (`httpclient.go`, this pass) so no single body can OOM the
  decoder.
- **Exact aggregation inherently requires the walk.** The returned `RegistryStats` (repo/tag
  count, unique-blob count, total bytes) is defined as an exact figure; a per-call cap would make
  it silently approximate — a contract change, not a contract-safe hardening.

**What would revisit this.** Serve Stats from a periodically-materialised aggregate (or make it
paginated/streamed) rather than a live full-namespace walk; that removes the O(tags) live fan-out
without changing the exact-count contract. A materialisation component is a follow-up, not a
frozen-contract hardening change.

## 11. `PermissionMap` ScopeFiltered entries retain `Relation`/`Extract` as permission-catalog documentation

**Rule.** CWE-561: no dead code — a field the runtime never reads should not be carried.

**Divergence.** The `ScopeFiltered` entries in `internal/check/permission_map.go` carry
`Relation` + `Extract` + `Permission` like every interceptor-gated entry, yet for a
`ScopeFiltered` entry the authz interceptor (`pkg/authz`, absorbed into this monorepo — the
former `kacho-corelib`) returns `DecisionInternal` **before** it ever calls `entry.Extract` — so
those two fields are never read at runtime on these entries. Real per-repo enforcement for these
RPCs lives in `internal/handler/listauthz.go`, bounded at `repoAuthzConcurrency`.

> **Count re-checked 2026-08-01: ten, not four.** This section was written when the residual
> covered `List` / `ListRepositories` / `ListTags` / `DeleteTag`. The six repository-overlay
> entries added later (`GetRepository`, `CreateRepository`, `UpdateRepository`,
> `DeleteRepository`, `RenameRepository`, `ListReferrers`) carry the same residual, each with
> its own inline comment restating it. `repositoryObject()` is likewise no longer used «only by
> `ListTags`/`DeleteTag`» — seven entries reference it. The class did not change; its extent
> did, which is exactly what a divergence list is supposed to keep honest.

> **What `ScopeFiltered` does *not* skip: identifying the caller.** The interceptor extracts the
> subject **before** the `ScopeFiltered` branch and fail-closes on a missing principal, pinned by
> its own test. `ScopeFiltered` redirects the *object* decision to the handler; it never makes an
> RPC anonymous. Reading this section as «these RPCs bypass authorization» would be wrong in the
> dangerous direction.

**Why accepted.**
- **Intentional, tested catalog documentation, not an accident.** The code comment states the
  retention explicitly («Relation/Extract сохранены как catalog-doc»), and
  `permission_map_test.go` (`TestPermissionMap_List_CatalogRetained`) *asserts* `List` keeps
  `Relation=v_list`. Every entry carrying a uniform `{Permission, Relation, object-extractor}`
  descriptor makes the map a single readable catalog of «which verb/object each RPC conceptually
  governs», with `ScopeFiltered:true` the one flag that redirects enforcement to the handler.
- **Extractor uniformity is load-bearing for the live entries.** `registryObject()` /
  `projectObject()` *are* executed for the interceptor-gated RPCs (`Get`/`Create`/`Update`/
  `Delete`/`ListOperations`/GC/Stats); dropping `Extract` from only the ScopeFiltered entries
  would leave an inconsistent map (some entries with an extractor, some without) for no
  behavioural, wire, or security change.
- **No enforcement risk.** Removing the fields changes neither the interceptor path (which
  ignores them for ScopeFiltered) nor the handler gate (`listauthz.go`), so the residual is
  documentation-shaped; the misleading-reader concern is already mitigated by the extensive
  inline comments pointing at `handler/listauthz.go` as the enforcement site.

**What would revisit this.** A decision to make the map carry *only* the fields the runtime reads
(drop `Relation`/`Extract` on all ten ScopeFiltered entries; `repositoryObject()` itself stays —
seven entries reference it), paired
with updating `TestPermissionMap_List_CatalogRetained` and adding a one-line comment per entry
pointing at `handler/listauthz.go`. A pure catalog-shape change, deferred so the tested uniform
descriptor is not reversed mid-hardening.

---

## 12. Rename: снятие тегов под старым именем — шаг ПОСЛЕ закрепления имени, и его неудача не отменяет перенос

**Что происходит.** Перенос репозитория в движке двухфазный: скопировать все теги
`old→new`, затем снять их под `old`. Порядок в use-case теперь такой: копирование →
закрепление наложения и прав (`RekeyConfig`/`InsertConfig` + intents) → снятие тегов под
`old`. Неудача ПОСЛЕДНЕГО шага логируется под именем события
`registry.rename.purge_failed` и **не** превращает операцию в отказ.

**Почему так, а не «всё или ничего».** Прежний порядок (снять, потом закрепить) делал
сбой посередине потерей адресуемости: часть тегов уже снята под именем, которое знает
тенант и на которое выданы права, а под новым именем лежит полный набор без строки
наложения и без регистрации — туда не доходит НИКТО, включая администратора аккаунта и
облака (в модели все глаголы репозитория читают каскад от родителя, а parent-tuple без
закрепления не эмитится). При этом операция докладывала ОТКАЗ, то есть «ничего не
произошло», а повтор был закрыт терминально: единственный уже скопированный тег делал
целевое имя «занятым» — навсегда, при полностью целом источнике.

**Чем платим.** Если снятие не удалось, под старым именем остаётся набор тегов. Он
**разрегистрирован** (unregister-intent эмитится вместе с закреплением), поэтому тенанту
недоступен; забирается повторным переименованием либо удалением репозитория под старым
именем. Место в хранилище освобождается сборкой мусора движка, как и для любого снятого
тега.

**Что считаем правильным исходом.** Предмет операции — перенос ИМЕНИ, и он состоялся:
имя, права и содержимое сошлись на `new`. Остаток под `old` — мусор, а не потеря:
содержимое адресуемо, права выданы, повтор сходится.

**Что бы это пересмотрело.** Появление у сервиса собственной очереди добивания
(компенсирующий шаг инициатора, как `data-integrity.md` §B12 предписывает для саг): тогда
снятие уезжает в неё и остатка не остаётся вовсе. Отдельная работа — здесь она не нужна
для корректности, только для гигиены хранилища.
