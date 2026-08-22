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

### 3b. DPoP `jti` replay — closed (#909)

Однократность предъявления доказательства владения (RFC 9449 §11.1) держится
хранилищем **вне процесса**: та же общая запись, что держит ключ однократности
(`Idempotency-Key`), и допуск — один атомарный оператор. Освобождение снято
вместе со своим предметом, а не переписано в настоящем времени.

Пара «хранилище + флот» проверяется при старте одним условием на оба предмета:
запись видна всем репликам либо реплика одна. Проба — на восьми независимых
репликах над одной базой: допущено ровно одно предъявление, остальные семь
отвергнуты.

