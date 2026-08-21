# Known divergences — kacho-api-gateway

Deliberate, reviewed deviations from a general rule or from an audit
recommendation. Each entry states the rule, why the gateway diverges, and why it
is not a defect. New entries are added when an audit flags something we
consciously choose not to "fix".

## 1. Configuration via envconfig struct-tags (not YAML/viper/koanf)

**Rule (evgeniy regime).** Service configuration should be loaded from a
hierarchical YAML file via viper/koanf with a typed nested schema and env-var
overlay, rather than flat environment variables bound through `envconfig` struct
tags.

**Gateway state.** `internal/config/config.go` is populated from ~60 environment
variables via `corelib corecfg.Load` with `envconfig:` struct tags.

**Why this is not a gateway defect.** This is a **workspace-wide platform
convention**, not a gateway-specific choice: every kacho-* service uses
`corelib corecfg.Load` with envconfig tags, and there is no YAML config
infrastructure anywhere in the workspace. Config shape is a horizontal,
cross-cutting concern owned by `kacho-corelib`, and 12-factor env-var config is
the deployment contract the Helm charts and the dev stack are built around.
Migrating a single service to YAML in isolation would fragment the platform and
break the shared `corecfg` loader. If the platform adopts the YAML regime, the
migration is a workspace-wide change to `kacho-corelib`'s config loader (all
services move together) — tracked at the platform level, not here.

**Mitigation for the "easy to mis-set a toggle" concern.** The loader is
fail-fast: mismatched/missing mTLS/authz env vars are caught at process start,
not at request time. The gateway does not silently run with a half-set security
toggle.

**Sub-concern: a misspelled env var is silently ignored, and the external-edge
relaxed-posture check is a WARN, not a fatal, outside prod-labelled envs.**
`envconfig` binds by exact name and ignores names it does not recognise, so the real
knob `KACHO_API_GATEWAY_AUTHZ_ENABLED` typed one letter short — without its trailing
**D** — leaves `AuthZEnabled` at its default. The misspelling is deliberately *not*
reproduced here as a quoted name: a name in backticks reads as a coordinate, and this
one names nothing on purpose. (The same near-miss pair is the documented reason the
freshness hook compares whole env names rather than substrings — a referent one letter
longer would make a dead knob look alive.) Two compensating controls already exist and are
deliberate:

- `validateProductionAuthzConfig` (main.go, keyed on `KACHO_APP_ENV`) **fatally**
  refuses to start a prod-class deploy with disabled/relaxed authz.
- For deploys that forget to set `KACHO_APP_ENV` (empty → dev-class) while
  exposing the **external advertised TLS edge**, main.go emits a loud startup
  `WARN` (SECURITY: external TLS edge enabled with a relaxed auth posture),
  independent of the env label. This is intentionally a WARN and not a fatal:
  the external listener can be legitimately fronted by a dev/local stack with a
  self-signed cert and relaxed auth for iteration, and hard-failing that case
  would break the documented dev workflow. The fatal guard is reserved for the
  explicit prod-class signal (`KACHO_APP_ENV`), which is the contract operators
  set for production. Making the external-edge case fatal regardless of env
  label is a deployment-policy change (would break dev stacks that expose TLS),
  not a gateway-code defect.

Rubric reference: envconfig-vs-YAML (evgeniy); CWE-1188. Contract impact: none.

## 2. One in-process cache intentionally NOT folded into `internal/lrucache`

The audit recommended consolidating the hand-rolled TTL+LRU caches into one
generic primitive. Five now share `internal/lrucache` (authz decision cache,
subject cache, DPoP replay cache, introspection cache, and — as of
sec-hardening-r9b — the `KratosClient` whoami cache). One is **deliberately left
separate** because forcing it onto the primitive would change its semantics, not
just its mechanics:

### 2a. `IdempotencyStore` (internal/middleware/idempotency.go)

- **Divergent semantics:** FIFO insertion-order eviction (NOT LRU — a replay
  read must not extend an idempotency record's lifetime) **plus** an atomic
  admission point (owner / waiter reservations) that has no analogue in a plain
  key/value cache. Its value type also carries HTTP status/body/content-type and
  a body-size cap.
- **Why separate:** the admission path (`Reserve` / `Commit` / `Release` /
  `Await`) and FIFO eviction are the whole point of this component; the generic
  LRU would either need to absorb admission (bloating a general primitive with a
  one-caller concern) or the store would lose its exactly-once guarantee. Kept as
  a focused component.
- **Since #694 the store is an INTERFACE**, and `MemoryIdempotencyStore` is one
  of two implementations; the shared one (`internal/idempotencypg`) is not a
  cache at all. That makes folding it into a cache primitive doubly wrong: the
  admission contract now has to hold across processes, which no in-process
  primitive can express. See §3a.

### Migrated (was 2b): `KratosClient` whoami cache

The whoami cache previously hand-rolled two maps (positive / negative) with
bespoke `evictLocked` / `enforceCapLocked` cap enforcement. It now uses a single
`lrucache.Cache[string, kratosCacheEntry]` where the positive/negative class is a
field on the value (`active`) and the dual TTL (positive 30s, negative 5s) is
expressed per entry via `PutWithTTL`. The attacker-controlled-cookie keyspace is
still bounded by the primitive's hard cap (`kratosCacheMaxEntries`). The earlier
"dual-TTL split-cache needs two primitives" rationale did not hold: one keyed
value + `PutWithTTL` covers it, so the eviction/cap path is now tested exactly
once in `internal/lrucache`.

Rubric reference: kacho-corelib reuse principle. Contract impact: none —
unexported, in-process, no wire/API/DB change.

## 3. DPoP-replay state is still per-pod; idempotency no longer is (#694 landed)

**Rule (project-rule #10).** A within-domain invariant must be enforced at a layer
that spans the whole concurrency domain, not by a per-process software check. For
the edge that domain is the **fleet**, because the shipped chart carries an HPA.

### 3a. `Idempotency-Key` — CLOSED (#694)

This section used to cover both stores and accepted both as residual. Half of it
has a fix, and leaving the acceptance in place would be an exemption that
outlived its subject.

- The store is now an **interface** with one atomic admission point
  (`middleware.IdempotencyStore.Reserve`). Two implementations ship: in-process
  (`MemoryIdempotencyStore`, correct for a fleet of exactly one) and shared
  (`gateway/internal/idempotencypg`, one Postgres table, admission by a single
  `INSERT … ON CONFLICT … RETURNING`, records reaped at TTL).
- **The pairing is enforced, not documented.** The chart derives the declared
  fleet size from the very values that drive the HPA and hands it to the process
  (`KACHO_GATEWAY_FLEET_SIZE`); the process refuses to start when an in-process
  store meets a fleet larger than one
  (`cmd/api-gateway/idempotency_validation.go`), and the chart refuses to render
  such a pairing at all. There is no longer a comment stating a precondition that
  nothing checks.
- **The shipped default now declares one replica** (`autoscaling.maxReplicas: 1`).
  The previous note here argued against exactly that — "do NOT cap maxReplicas to
  1, it trades an edge case for a capacity regression". The argument was sound
  while there was no alternative; there is one now, and it costs two lines
  (`idempotency.store: postgres` + an address). An edge that promises exactly-once
  and does not deliver it is worse than an edge with a lower ceiling, so the
  default was chosen in favour of a promise that holds.
- **Named honestly:** a rolling update briefly runs two pods even at one replica,
  and in that window the in-process store does not give exactly-once. Only the
  shared store closes it.
- **Remaining infra step (not this change):** no umbrella profile provisions a
  database for the edge, so raising the ceiling today means pointing
  `idempotency.dsn` at a store the operator provisions. Adding a `pg-gateway`
  instance to the umbrella is a deployment decision with its own blast radius
  (per-profile blocks, posture assertions, connection arithmetic) and is tracked
  separately.

Proof: `gateway/internal/idempotencypg/store_integration_test.go` builds **two or
more independent replicas over one database** — separate store, separate pool,
separate middleware — and asserts that a repeat landing on another replica
replays the stored answer without calling downstream, and that concurrent
same-key submissions across replicas execute downstream exactly once.

### 3b. DPoP `jti` replay — still per-pod (accepted residual, latent; #909)

`DPoPReplayCache` (RFC 9449 §11.1 anti-replay) remains a per-pod
`lrucache.AddIfAbsent`: correct within one process, not across the fleet. A
captured proof can be presented at most once per replica that has not yet seen
its `jti`, bounded by the `iat`-freshness window (cache TTL = 2× it).

Two things make this different from 3a rather than simply "not done yet":

- it is **latent**: no deployment profile mounts the DPoP validators
  (`authn.enableDpop` is set by no profile), so today the cache is not on any
  live request path. It becomes live the day DPoP is enabled;
- the fix is **cheaper** than 3a's and shaped differently: `jti` needs
  `INSERT … ON CONFLICT DO NOTHING` with a TTL, no lease, no takeover, no stored
  response.

Tracked as #909 so it lands with its own proof rather than riding along here.
That issue carries the predicate for removing THIS section.

Rubric reference: project-rule #10 (concurrency-domain enforcement); CWE-362 /
CWE-294. Contract impact of 3a: the edge now answers `409 ABORTED` to a caller
whose key is held by another in-flight request, and `503 UNAVAILABLE` when the
shared store cannot be reached — both instead of silently executing the mutation
a second time. Requests without `Idempotency-Key` are untouched.

## 4. `main()` is a long composition root (single wiring site, by design)

**Rule (Go clean-code / McCabe).** A ~700-line function with dozens of
`if … { log.Fatalf }` startup branches is a high-cognitive-load signal; an audit
recommended extracting `buildBackends` / `buildExternalListener` /
`buildInternalListener` / `buildHTTPServer` helpers.

**Gateway state.** `cmd/api-gateway/main.go`'s `main()` wires the whole process
inline: backend dials, mTLS creds, IAM clients, JWT/DPoP/introspection setup,
authz middleware, REST mux, internal + external gRPC listeners, HTTP server and
graceful shutdown.

**Why this is not a gateway defect.** The workspace architecture rule
(`.claude/rules/architecture.md`) designates `cmd/<svc>/main.go` as the
**single composition root** — *"единственное место wiring"* — and explicitly
bans wiring/singletons leaking out of `cmd/`. Keeping the wiring literally in one
sequential `main()` is the intended shape: the security-critical **ordering** of
listener/interceptor registration (authz before/after DPoP, internal-vs-external
listener setup) is easiest to audit as one linear top-to-bottom read rather than
scattered across helper constructors that hide the sequence behind call sites.
The function is branch-heavy but not logic-heavy: nearly every branch is a
`log.Fatalf` fail-fast guard, not business logic. Splitting it would move code
without reducing the essential wiring complexity, and would risk exactly the
mis-ordering the audit worries about by making the order implicit. If the wiring
grows further, extraction is revisited — but a long *composition root* is a
deliberate, reviewed shape, not a defect.

Rubric reference: architecture.md (composition root); CWE-1121. Contract impact:
none — no behavior/wire/API/DB change.

## 5. `X-Forwarded-For` trusted by default (`client_ip` for FGA conditions)

**Rule (secure-by-default / CWE-348).** An audit noted that honouring
`X-Forwarded-For` / `X-Real-IP` by default (`KACHO_API_GATEWAY_AUTHZ_TRUSTED_XFF`
default `true`, `…_TRUSTED_PROXY_COUNT` default `1`) is a "less-trusted source"
if the gateway is ever reachable without a trusted L7 hop inserting the rightmost
XFF entry: a header the client controls would then feed a value that
network-scoped authorization conditions are evaluated against — i.e. an input
that must be derived would instead be asserted.

**Gateway state.** The parser reads the forwarded chain **from the right** with
`TRUSTED_PROXY_COUNT` hops, so a client-forged *leftmost* XFF cannot drive
`client_ip` in the intended ingress topology (an L7 LB appends the real peer as
the rightmost entry). The residual risk is only a topology change that removes
the trusted proxy (direct-to-Service / port-forward).

**Why the default stays `true` (not flipped here).** The deployed shape
(`kacho-deploy`) always fronts the gateway with an ingress that appends XFF;
FGA conditions such as `source_ip_in_range(client_ip, …)` depend on that derived
`client_ip`. Flipping the Go default to `false` would silently make `client_ip`
the TCP peer (the ingress pod IP) for the standard deploy, breaking those
conditions, unless the deploy chart is simultaneously changed to set
`…_TRUSTED_XFF=true` — a coordinated cross-repo change (`kacho-deploy`) outside
this repo's blast radius. The knob is first-class and documented on the config
field: operators running the gateway **directly on the wire** MUST set
`KACHO_API_GATEWAY_AUTHZ_TRUSTED_XFF=false` (or `…_TRUSTED_PROXY_COUNT=0`) so the
TCP peer is authoritative. Tightening the default to fail-closed is tracked for
the release that lands the matching `kacho-deploy` overlay change.

Rubric reference: CWE-348 / CWE-290; security.md. Contract impact: none — no
wire/API/DB change; behavior governed by existing env knobs.

## 6. Backend-dial transport mTLS is per-edge opt-in (not startup-enforced); mesh-terminated in the prod profile

**Rule (security.md #1).** Every service→service hop must be mTLS (verified client
cert); plaintext/insecure-gRPC in prod is banned. An audit noted that
`validateProductionAuthzConfig` fails closed on authz/authn posture but never
checks that any backend-dial mTLS edge (`KACHO_API_GATEWAY_MTLS_*_ENABLE`, all
default `false`) is enabled, nor that the external TLS listener is configured
(`KACHO_API_GATEWAY_TLS_LISTEN_ADDR` default empty) — so a prod-class deploy that
forgets the mTLS overlay boots and dials every backend (incl. `iam:9091` /
`AuthorizeService`) over insecure gRPC with no startup error.

**Identity-trust corollary (sec-hardening-r8b).** A follow-up audit sharpened the
concern: the gateway→backend hops carry the gateway-derived trusted identity
headers, and the receiving side trusts what they assert *only because* they arrive
on a verified edge. **The trust assumption is therefore discharged by the transport
security of the hop, and by nothing else** — an unauthenticated hop would leave
those headers as assertions no one verified. This does not change the disposition:
in the shipped profile that security is provided by the service mesh (sidecar
mTLS), which covers the hop regardless of the app-level per-edge flag. **A
mesh-less deployment MUST enable the per-edge flag** (see Compensating controls) —
precisely so the identity headers are never forwarded over a hop whose peer was
not authenticated. That is the operator-facing requirement; the consequence of
ignoring it follows from the sentence above and is not spelled out further here,
since this registry is published.

**Gateway state.** Backend-dial transport is a per-edge overlay
(`gateway/cmd/api-gateway/mtls_config.go`): each edge is independently enabled via its
`MTLS_<EDGE>_ENABLE` flag. The build is **fail-fast when an edge is enabled but its
cert material is missing/partial** (`buildBackendDialCreds` → `gateway/cmd/api-gateway/main.go` `log.Fatalf`), so
the process never comes up half-secured on a configured edge. With every edge disabled
(the flag default), every dial is insecure — identical to dev.

> [!warning] Здесь стояло «боевой профиль не задаёт НИ ОДНОГО такого ключа» — перемерено 2026-08-11
> Профиль задаёт **все семь** и `mtls.enable: true` над ними. Предикат:
> ```sh
> awk '/^api-gateway:/{a=1} a&&/^    edges:/{e=1;next} e&&/^      [a-z]+: (true|false)$/{print} e&&!/^      /{exit}' >   deploy/helm/umbrella/values.prod.yaml
> ```
> → `vpc compute iam nlb geo registry storage`, каждый `true`. Профиль лежит в
> `deploy/helm/umbrella/values.prod.yaml`; прежняя редакция называла его по имени
> репозитория времён полирепо, которого нет.
>
> **Довод ниже опирался именно на это утверждение, и опора исчезла.** Раздел объяснял, что
> жёсткая проверка при старте не заводится, поскольку «сломала бы `values.prod.yaml`, который
> её не включает». Профиль её включает — значит этот конкретный аргумент больше не
> применим, и **вопрос подлежит новому решению, а не наследованию**: остаётся ли отказ в
> старте нежелательным по существу (топология с сеткой, где приложение законно ходит в
> локальный прицеп открытым текстом) — или основание было только в профиле. Записываю
> расхождение, а не выбираю за владельца: снятие или введение гейта безопасности — его
> решение, и оно должно приниматься на верном факте.
>
> Что от прежнего текста остаётся верным без правки: сам размен «сетка против приложения»
> сформулирован корректно, ключи по-прежнему умолчанием выключены, и требование к посадке
> **без** сетки не меняется.

**Why a hard app-level startup guard is NOT added here.** The deployed prod
topology terminates inter-pod transport security at the **service mesh** (sidecar
mTLS), not in the application: the app legitimately dials plaintext to its local
sidecar and the mesh wraps the hop transparently. In that model app-level
`MTLS_*_ENABLE=false` is the correct, secure posture. A startup guard that fatally
required app-level backend mTLS in prod-class envs would break `values.prod.yaml`
(which does not enable it) and every mesh-based deployment — it would hard-code one
deployment-topology assumption (app-terminated mTLS) and reject the other
(mesh-terminated). This is the deliberate reason the **internal listener** guard
(`validateProductionInternalListener`) is asymmetric with backend dials: that
listener enforces an app-level **SPIFFE caller allow-list**, which requires the app
to see the verified client cert — a decision the mesh cannot make for it — so
app-level mTLS there is functionally required; backend dials need only transport
security, which the mesh provides.

**Compensating controls.** (a) fail-fast on any *enabled* edge with missing cert
material; (b) the internal listener hard-requires mTLS + SPIFFE allow-list in prod
(`validateProductionInternalListener`); (c) `main.go` emits a loud startup WARN when
the external advertised TLS edge runs with a relaxed auth posture. Operators who run
the gateway **without a mesh** (direct pod-to-pod) MUST enable the per-edge
`KACHO_API_GATEWAY_MTLS_*_ENABLE` flags + cert material; promoting this to a fatal
guard is tracked for the release that lands a mesh-vs-app transport-policy signal in
`deploy/` (so the guard can distinguish "mesh handles it" from "misconfigured"
instead of over-constraining the prod profile).

Rubric reference: security.md #1; CWE-319 / CWE-1188. Contract impact: none — no
wire/API/DB change; behavior governed by existing per-edge env knobs.

## 7. Internal admin REST listener (`:8081`) has no app-level transport auth (mesh + NetworkPolicy isolated)

**Rule (security.md #1).** Internal listeners are not a trusted zone: every
listener — public AND internal — should enforce mTLS transport plus a per-RPC
authorization decision. An audit noted that the dedicated cluster-internal admin
REST listener (`KACHO_API_GATEWAY_INTERNAL_REST_ADDR`, default `:8081`) — the only
listener that serves `Internal*` REST (addressPools, `:internal` infra-sensitive
Network projections, InternalRegistry/Cluster/Operations,
`InternalUserService.UpsertFromIdentity`) — terminates plaintext HTTP: its origin
is marked purely by listener wrapping (`listenerorigin.InternalConnContext`) and
`<exempt>` Internal RPCs are admitted on it without authN. Unlike the internal
**gRPC** listener (`internal_grpc_security.go`: mandatory mTLS + SPIFFE allow-list
+ production guard), it has no app-level transport authentication.

**Why the internal gRPC listener enforces app-level mTLS but this REST listener
does not.** The asymmetry is deliberate and identical to the one in §6. The
internal gRPC listener makes an app-level **SPIFFE caller allow-list** decision
(only the iam push-drainer identity may flush the authz decision-cache) — that
requires the app itself to see the verified client cert, which a mesh cannot
decide for it, so app-terminated mTLS there is functionally required. The internal
REST listener makes **no such per-caller cert decision**: it is an admin-plane
surface reached by the UI / admin-tooling / `kubectl port-forward`, where
distributing and pinning client certs to browsers and operators is impractical.
Its transport security is therefore provided the same way as every other
backend hop in the shipped profile — by the **service mesh** (sidecar mTLS) — plus
**NetworkPolicy** restricting who can reach `:8081` at all.

**Compensating controls (defence-in-depth, not network-only).**

- The listener shares the one `httpSrv` handler chain, so every request on it
  still traverses `authInterceptor.HTTP` → DPoP → `authzMW.HTTP`. Non-exempt
  `Internal*` REST calls are subject to the same per-RPC authz `Check` as any
  other request; only the small `phaseInternalOriginExempt` set (identity
  bootstrap RPCs that necessarily run before a principal exists) is admitted
  without authN, and those are admitted *only* on the internal-origin-marked
  listener — never on the external edge (fail-closed origin default).
- The infra-sensitive Network `:internal` projections and admin `Internal*`
  surfaces are unreachable from the external listener regardless of NetworkPolicy
  (origin marker fail-closes to external → 404), so a NetworkPolicy miss exposes
  them only to in-cluster peers that can already reach `:8081`, not the internet.

**Why not add app-level mTLS termination here now.** Standing up a second
TLS-terminating `http.Server` (separate `tls.Config` with
`RequireAndVerifyClientCert`) for the admin REST surface hard-codes the
app-terminated-mTLS topology and breaks the mesh profile and the port-forward
admin workflow — the same over-constraint §6 documents for backend dials.
Promoting the internal admin surfaces to app-level mTLS is tracked for the release
that lands the mesh-vs-app transport-policy signal in `kacho-deploy` (so the guard
can tell "mesh handles it" from "misconfigured").

Rubric reference: security.md #1/#6; CWE-306. Contract impact: none — no
wire/API/DB change; posture governed by mesh + NetworkPolicy + the existing
origin-marker fail-closed default.

## 8. Resource-id extractor fails closed to the wildcard scope (no error channel)

**Rule (CWE-390 / observability).** An audit noted that `phaseResource`
(`internal/middleware/authz.go`) calls `Resources.ExtractFromHTTP(...)` /
`ExtractFromProto(...)` discarding the second return (`resourceID, _ = …`), so an
extraction miss is neither logged nor distinguished and could surface to operators
as an opaque `PermissionDenied`.

**Gateway state (why this is by design, not a dropped error).** The extractor's
second return is an `ok bool` documented as "no error", **not** an error value —
and it is `true` on every code path (`resource_extractor.go`). Extraction never
*fails*: a named field that is absent, empty, or on a non-proto request resolves
to the FGA wildcard `"*"` (List/Search scope), which is the intended fail-closed
result — a wildcard on a concrete-resource RPC is denied at the FGA `Check` (no
path), never silently allowed. There is deliberately no empty-id path: the only
branch that could once return `""` was the stdlib-reflect fallback for non-proto
requests, removed in sec-hardening-r8b (the production authz path always hands the
extractor a `proto.Message`), so `resourceID` is now always either a concrete id
or `"*"`.

**Why no extra logging is added.** A wildcard result is indistinguishable from a
legitimate List/Search RPC (which is *supposed* to scope to `"*"`), so logging
"extraction produced a wildcard" would fire on every List call — noise, not
signal. The one genuinely diagnosable input error — a syntactically **malformed**
concrete id — is already logged and surfaced as `InvalidArgument`/400 by the
malformed-id short-circuit (`authz.go`, `corevalidate.ResourceID`), before the FGA
`Check`. The remaining "wildcard → deny" path is a correct authz outcome, not a
maskable failure.

Rubric reference: CWE-390. Contract impact: none — no wire/API/DB change.

## 9. `authz.go` / `auth.go` are single large same-package files (not split by concern)

**Rule (Go clean-code / project-rule #11).** An audit flagged
`internal/middleware/authz.go` (~950 LOC) and `internal/middleware/auth.go`
(~780 LOC) as oversized multi-responsibility files (HTTP + gRPC unary/stream
interceptors, catalog lookup, subject resolution, resource scoping, caching and
Check dispatch each), on the highest-blast-radius security decision path.

**Why this is not treated as a defect.** The two files are already decomposed
*internally* into small, single-purpose phase functions
(`phaseCatalog` → `phaseSubject` → `phaseResource` → `phaseCheck`, plus the
HTTP/gRPC entry adapters), so the cognitive unit is the phase, not the file. The
audit's own proposed fix is a pure **file-move** — one module per phase group plus
separate HTTP and gRPC entry adapters — with no behavior change and identical exported
`AuthzMiddleware` API. The proposed file names are given as prose, not as paths: none of
them exists, and a path in backticks reads as a coordinate the next contributor will go
looking for. Today the phases live together in `gateway/internal/middleware/authz.go`. On the single most security-sensitive code path, a large
mechanical churn that touches every line's location — inflating the review diff
and colliding with other in-flight security branches — carries more regression
and review-miss risk than the maintainability signal it addresses, for zero
behavioral benefit. The decomposition that matters (per-phase functions, each unit
testable) is already present. A physical split is revisited if these files grow a
genuinely new concern rather than another phase of the existing pipeline.

Rubric reference: project-rule #11; CWE-1121. Contract impact: none — no
behavior/wire/API/DB change. (Confidence of the original finding: low.)

## 10. One gRPC FQN bypasses authN as well as authZ (`DefaultPublicAllowlist`)

**Rule (`security.md` §"AuthN+AuthZ ВЕЗДЕ").** Every RPC on every listener runs
authentication and a per-RPC authorization Check. The rule admits exactly two
documented exemptions — the iam JWKS route and the geo public catalog reads.
Anything else that skips a check and is not written down is a silent deviation,
which the rule counts as a violation in its own right.

**Gateway state.** `middleware.DefaultPublicAllowlist()` is consulted by
`phaseAllowlist`, which is **step 1** of `decide()`. Subject extraction — the
phase that answers 401 — is step **5** (allowlist → internal-origin exemption →
override → catalog → subject; the ordinal moved when the internal-origin phase was
inserted, and it is given here only to say "later than the allowlist", which is the
part that matters). An entry on this list therefore returns
ALLOW before any principal is resolved: it waives **authentication and
authorization both**, on every listener including the advertised external TLS
edge. This is strictly stronger than the catalog's `<exempt>`, which still
requires authentication and only skips the FGA Check.

One entry remains, and this section is where its justification is written down
so that it is not silent:

| Entry | What an unauthenticated caller obtains | Why it is accepted |
|---|---|---|
| `grpc.health.v1.Health/Check` | a constant SERVING for the gateway itself; the handler does not read the requested service name | probes carry no bearer token; gating liveness on the authz path couples an IAM outage to a cluster-wide restart loop |

It returns no tenant data, no resource identifiers, nothing scoped to an
individual owner — the answer is identical for every caller. That sameness is
what makes an unauthenticated answer defensible, and it is the property to
re-check before any entry is added.

**Removed — `grpc.health.v1.Health/Watch`.** It was listed beside `Check` on the
reasoning that listing it kept a future implementation from arriving silently
behind a bypass. The reasoning runs backwards. The gateway embeds
`UnimplementedHealthServer`, so `Watch` is answered `Unimplemented` and the entry
waived authentication and authorization for an RPC no caller could reach; and had
`Watch` later been implemented, the pre-placed entry is precisely what would have
let it stream to unauthenticated callers with nobody deciding so. An exemption is
added WITH the thing it exempts, never ahead of it.

Removing it changes no capability: a `Watch` call from the edge previously
reached the health server and got `Unimplemented`; it now gets an authorization
decision (catalog miss ⇒ denied). Nothing could `Watch` before and nothing can
now.

**The gate this produced.** `gateway/cmd/api-gateway/public_allowlist_answered_test.go`
stands the edge's native surface up on an in-memory listener, invokes every
bypass entry that surface serves, and fails the build on one answered
`Unimplemented`. It is deliberately a SECOND gate rather than a widening of
`gateway/internal/middleware/authz_public_allowlist_resolves_test.go`: that one asks whether the
name exists in the served contract and says in its own comment that
served-but-unimplemented is legitimate for its question. It is — for that
question. Reachability is a different question and needed a different probe, with
its own controls in both directions (the edge's own `Check` must not read as
`Unimplemented`; a fabricated method, an unserved service, and the now-moved
reflection FQN all must) and a census that distinguishes "nothing found" from
"nothing invoked".

**Closed — server-reflection moved to the cluster-internal listener.** Earlier
revisions of this section carried two `grpc.reflection.*` entries and left the
question open for the owner, on the grounds that changing it alters operator
tooling behaviour. The owner decided: the advertised edge does not serve
reflection; the cluster-internal listener does, behind mTLS.

What made the decision straightforward is that **disclosure was never the main
cost**. The product repository is public and the proto tree with it, so schema
retrieval added little that `git clone` does not already give. Two things were
real, and neither was worth edge-side `grpcurl`:

- an authN+authZ exemption sitting on the advertised external listener, matching
  neither documented exemption, in direct tension with the rule above;
- a request that costs an anonymous caller one line and the gateway a descriptor
  walk over its whole linked set — free amplification, on the edge.

The move is two changes that must stay together, and a third that removes the
way back:

- `cmd/api-gateway/external_grpc_services.go` — the edge's native gRPC surface is
  now one named list (`Health`, `OperationService`), with no reflection in it.
  `external_grpc_services_test.go` asserts the set exactly, so the next service
  registered at the edge without a decision fails the build rather than shipping.
- `cmd/api-gateway/internal_grpc_listener.go` — reflection is registered there,
  and only when `sec.mtlsEnabled`. That condition is the safety argument: with
  mTLS on, every call including the reflection **stream** must present a verified
  client certificate whose SPIFFE SAN is on the caller allow-list; with mTLS off
  the listener mounts no interceptors at all, so not registering it is the only
  refusal available. `internal_grpc_reflection_test.go` proves all three: served
  to an allow-listed identity, `PermissionDenied` for a verified-but-not-listed
  one, absent entirely in the insecure posture.
- the standalone knob (`internalListener.reflection` →
  `KACHO_API_GATEWAY_INTERNAL_GRPC_REFLECTION`) is **removed, not defaulted off**.
  Its only non-redundant setting was the dangerous one — reflection on, for a
  listener that authorises nobody. `deploy/render_reflection_test.go` renders the
  chart WITH the retired value set and requires that nothing reaches the PodSpec,
  so a stale overlay value is demonstrably inert rather than assumed to be.

**Was anything depending on it?** Checked before the change, not after: every
`grpcurl` call site in the tree (`tests/authz-fixtures/setup.sh`,
`services/iam/tests/newman/crud-fixture/setup.sh`, and the e2e runners that
prepare them) targets a service's own `:9091` internal listener, never the
gateway edge. The ban #6 probes — the cases that assert `Internal*` is
unreachable from outside — are REST, and they assert an HTTP status; they resolve
no symbols and are unaffected.

**Why the list has a gate.** Six further entries were removed in an earlier
change: they named a gRPC `AuthService` / `BackChannelLogoutService` that does
not exist in the contract and never has in this repository. The interactive auth
flow is HTTP (`/iam/v1/auth/…` in `gateway/internal/middleware/session_identity_handler.go`,
`/oauth/logout` in `gateway/internal/handler/logout_handler.go`) and its pre-auth exemption
is `isPublicHTTPPath` in `gateway/internal/middleware/authz_util.go` — a separate list,
unaffected by the removal. An entry naming
nothing is not inert: it reads like a reviewed decision, and the next person
adding a bypass copies its shape. `authz_public_allowlist_resolves_test.go`
resolves every entry against the served contract and fails the build on one that
does not exist, so this list expires on its own instead of inheriting the next
blind spot.

Rubric reference: `security.md` §"AuthN+AuthZ ВЕЗДЕ", §"Internal-vs-external",
§"Публичные артефакты". Contract impact: no wire/API/DB change. Operator-visible
change: `grpcurl` against the advertised endpoint no longer lists or describes;
the same commands work against the internal listener with a client certificate.
