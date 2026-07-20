# TEST-PLAN — method × class coverage matrix (kacho-registry)

Normative coverage plan for the kacho-registry regression suite across its three
surfaces: control-plane RegistryService (CRUD), the OCI data-plane auth-proxy, and the
IAM `/iam/token` ↔ Hydra token-exchange. The production-readiness gate (acceptance §12 /
functional-gate REG-TX-22) requires every **REQUIRED** cell to be covered and green on the
live stack.

Case-ids for each cell are catalogued in `CASES-INDEX.md`. The **authz** module
(`cases/registry-authz.py`) and the **data-plane harness** (`scripts/dataplane-e2e.sh`)
are being authored concurrently — cells they own are marked `▢ pending` until the file
lands, then flip to ✅ when green.

## Legend
- ✅ — covered by ≥1 case/scenario, expected green
- ▢ — planned, file/scenario pending (see CASES-INDEX status)
- ⚪ — not applicable for this method
- 🔬 — covered indirectly by Go integration tests (not newman/harness)

Columns: **happy** (positive path), **negative** (error/precondition), **corner**
(validation / boundary / immutability / idempotency), **authz** (existence-hiding,
listauthz, grant-latency, owner-tuple), **data-plane** (exercised over the OCI `/v2/`
surface or token-exchange).

---

## 1. RegistryService — control-plane public (:9090 via api-gateway REST)

| Method | happy | negative | corner | authz | data-plane |
|---|---|---|---|---|---|
| `Create` (async) | ✅ REG-CR-CRUD-OK | ✅ REG-CR-NEG-INVALID-NAME / -PROJECT-NOTFOUND | ▢ dup-name ALREADY_EXISTS (REG-04) | ▢ REG-CR-AZ-NO-GRANT-DENIED / -HIDDEN-PROJECT-NF | ⚪ |
| `Get` (sync) | ✅ (via REG-CR-CRUD-OK get) | ✅ REG-GET-NEG-MALFORMED-ID / -NOTFOUND | ⚪ | ▢ REG-GET-AZ-EXISTENCE-HIDING | ⚪ |
| `List` (sync) | ✅ REG-LST-CRUD-OK | ⚪ | ⚪ | ▢ REG-LST-AZ-OWNER-SEES-OWN / -CROSS-TENANT-NOLEAK | ⚪ |
| `Update` (async) | ✅ REG-UPD-CRUD-OK | ✅ REG-UPD-NEG-IMMUTABLE-NAME | ✅ immutable name/project via mask | ▢ REG-UPD-AZ-NO-GRANT-NF | ⚪ |
| `Delete` (async) | ✅ REG-DEL-CRUD-OK | ▢ non-empty FP (REG-08) | ▢ idempotent CAS (REG-09) | ▢ REG-DEL-AZ-NO-GRANT-NF | ⚪ |
| `ListRepositories` (sync) | ▢ REG-22 | ⚪ | ⚪ | ▢ per-repo row-filter (REG-22) | ✅ zot projection |
| `ListTags` (sync) | ▢ REG-24 | ⚪ | ⚪ | ▢ per-repo row-filter (REG-24) | ✅ zot projection |
| `DeleteTag` (async) | ▢ REG-25 | ⚪ | ▢ unregister-on-last-tag | ▢ `v_delete` gate | ✅ DP-DELETETAG-VDELETE |
| — anonymous (all) | ⚪ | ▢ REG-AZ-ANON-UNAUTH (401) | ⚪ | ▢ fail-closed | ⚪ |

## 2. InternalRegistryService — admin (:9091, mTLS-only)

`TriggerGarbageCollection` / `GetRegistryStats` are cluster-internal (never on the external
endpoint, ban #6) and carry infra-projection data. **Not** exercised through newman
(no public REST); covered by Go integration tests + the mTLS-restriction invariant
(part of REG-26 / REG-38). Cells: 🔬 for functional internals, ▢ for the mTLS-only
data-plane invariant assertion.

## 3. Data-plane OCI auth-proxy — `registry.kacho.local` (Docker Registry v2 / OCI 1.1)

| Surface op | happy | negative | corner | authz | data-plane |
|---|---|---|---|---|---|
| `GET /v2/` handshake | ▢ TX-DOCKER-LOGIN-HAPPY | ▢ DP-HANDSHAKE-ANON-401 | ⚪ | ▢ fail-closed | ✅ |
| push (new repo) | ▢ DP-PUSH-NEW-VCREATE | ▢ DP-PUSH-NOAUTH-404 | ▢ DP-PUSH-IDEMPOTENT | ▢ `v_create@registry_registry` | ✅ |
| push (existing repo) | ▢ DP-PUSH-OVERWRITE-VUPDATE | ▢ DP-PUSH-EXISTING-NO-VUPDATE-404 | ▢ verb-decoupling | ▢ `v_update@registry_repository` | ✅ |
| pull | ▢ DP-PULL-VGET-200 | ▢ DP-PULL-NOAUTH-404 | ⚪ | ▢ `v_get` existence-hiding | ✅ |
| blob (per-repo scope) | ⚪ | ▢ DP-BLOB-EXISTENCE-PERREPO-404 | ▢ DP-CROSS-REPO-BLOB-MOUNT-GUARD | ▢ two-Check exfil-guard | ✅ |
| path parsing | ⚪ | ▢ DP-PATH-TRAVERSAL-REJECT (raw + `%2e%2e`) | ⚪ | ⚪ | ✅ |
| `_catalog` / `tags/list` | ▢ | ⚪ | ⚪ | ▢ DP-CATALOG / DP-TAGS-LIST per-repo filter | ✅ |
| HTTP `DELETE` method | ⚪ | ▢ DP-DELETE-METHOD-405 | ⚪ | ⚪ | ✅ |

## 4. Token-exchange — IAM `/iam/token` shim + Hydra federation (Variant H)

| Flow | happy | negative | corner | authz | data-plane |
|---|---|---|---|---|---|
| docker `private_key_jwt` shim | ▢ TX-DOCKER-LOGIN-HAPPY | ▢ TX-DOCKER-ANON-401 / -INVALID-SAKEY-401 / -AUDIENCE-401 | ▢ TX-TOKEN-RATE-LIMIT | ▢ TX-IDENTITY-ONLY-CHECK | ✅ |
| k8s `jwt-bearer` | ▢ TX-K8S-JWT-BEARER-HAPPY | ▢ TX-K8S-NO-TRUSTED-SUBJECT / -BADTOKEN / -AUDIENCE-MISMATCH | ⚪ | ▢ TX-IDENTITY-ONLY-CHECK | ✅ |
| `SAKeyService.Issue` | ▢ TX-SAKEY-ISSUE-STANDARD / -FEDERATED | ▢ TX-SAKEY-ISSUE-VALIDATION-AUTHZ | ⚪ | ▢ authz on Issue | ⚪ |
| `SAKeyService.Revoke` | ▢ | ▢ TX-SAKEY-REVOKE (deny after revoke) | ⚪ | ⚪ | ✅ |
| data-plane JWKS verify | ▢ TX-DP-HYDRA-JWKS-SWITCH | ▢ TX-DP-JWKS-UNAVAIL-FAILCLOSED / TX-HYDRA-MINT-UNAVAIL-FAILCLOSED | ▢ kid-rotation refetch, cache-TTL | ⚪ | ✅ |
| live functional-gate | ▢ TX-E2E-LIVE-GATE | ▢ (negatives in same run) | ⚪ | ▢ | ✅ |

---

## 5. Auth model — existence-hiding (normative)

The whole suite validates one principle: **a subject learns nothing about resources it
cannot access.** Deny is indistinguishable from absence.

**Control-plane (per-RPC `InternalIAMService.Check`, verb-bearing).**
- Verb relations are decoupled from the tier (anti-`#241`): `v_get`/`v_list`/`v_create`/
  `v_update`/`v_delete` on `registry_registry` and `registry_repository`.
- `Get`/`Update`/`Delete` of a resource without the required verb → **sync `NOT_FOUND`**
  (deny→404, `corelib ErrHideExistence`), **never `PERMISSION_DENIED`**; the async
  `Operation` is **not** created and no state changes.
- `Create` is authorized on the **parent project** (`create-child = editor-tier on parent`,
  `v_create@iam_project`, because `registry_registry:<new-id>` does not exist yet):
  - deny on a **visible member** project → `PERMISSION_DENIED` (membership is not secret);
  - deny on a **hidden/non-member** project → `NOT_FOUND` (existence-hiding on the parent).
- `List` → listauthz filter (`viewer ∪ v_list`); cross-tenant rows never appear (CI-gate
  `make audit-list-filter`, read==enforce).
- anonymous → `401 UNAUTHENTICATED` fail-closed on every RPC (ban: no unauthenticated 2xx).

**Data-plane (per-request Check, Variant B, existence-hiding to blob-level).**
- push into a **new** repo → `v_create@registry_registry`; push into an **existing** repo →
  `v_update@registry_repository` (a subject with only namespace `v_create` cannot push
  layers into someone else's existing repo — decoupling guard).
- pull → `v_get`; deny → `404` (not 403). Per-repo **blob-scope**: another repo's blob is
  unreachable → `404`. Cross-repo blob mount requires **two** Checks (source + target).
- `_catalog` / `tags/list` → per-repo listauthz row-filter (not namespace-level).
- HTTP `DELETE` → `405` before zot; deletion only via `v_delete`-gated `DeleteTag`.
- `GET /v2/` without a token → `401` fail-closed; peer (iam/Check/zot) unavailable →
  fail-closed for mutations.

**Token / identity (Hydra Variant H).**
- Tokens are **identity-only**; authorization is always the per-request Check above.
- docker: `/iam/token` `private_key_jwt` shim → Hydra `client_credentials`; anon → `401 +
  WWW-Authenticate` (docker-CLI contract). k8s: `jwt-bearer` with an exact-subject trust-grant.
- data-plane verifies **Hydra JWKS**; JWKS unavailable / unknown-kid → fail-closed +
  kid-miss refetch (bounded cache-TTL); `alg`-guard (no `none`).

---

## 6. How to run

Prereqs: `python3`, `newman` (`npm i -g newman`), a live api-gateway (fe3455 or kind-stand),
and — for the data-plane harness — the `docker` CLI, DNS/ingress to `registry.kacho.local`,
a valid SA-key, and a reachable Hydra.

### 6.1 Control-plane (registry CRUD + authz) via newman

```bash
cd tests/newman

# 1) validate case uniqueness + catalogue (pure Python, no network)
python3 scripts/validate-cases.py

# 2) regenerate Postman collections from cases/*.py
python3 scripts/gen.py

# 3) run against the live stack (fe3455)
./scripts/run.sh --env environments/fe3455.postman_environment.json --service registry

# REG-1 redesign surface (regional placement / defaultRepositoryVisibility / lifecycle / id-lock):
./scripts/run.sh --env environments/fe3455.postman_environment.json --service registry-redesign

# authz + config-overlay Repository collections:
./scripts/run.sh --env environments/fe3455.postman_environment.json --service registry-authz
./scripts/run.sh --env environments/fe3455.postman_environment.json --service registry-repository

# all registry collections (no --service): registry, registry-redesign, registry-repository, registry-authz
./scripts/run.sh --env environments/kind-stand.postman_environment.json
```

> **REG-1 redesign coverage** — `cases/registry-redesign.py` (17 cases, verifies
> `REG-1-01..31`) locks the REG-1 production increment: `regionId` peer-validate geo +
> `placementType` REGIONAL const (F4), `defaultRepositoryVisibility` seed + admin-gate (F5),
> `Repository.lifecycle` DURABLE/EPHEMERAL output-only (F7), and identity-lock (id immutable,
> rename-name-preserves-id, field-absence). Requires geo seeded with `existingRegionId`
> (kacho-deploy geo fixture, shared with nlb/compute). Acceptance:
> `docs/specs/sub-phase-REG-1-registry-repository-acceptance.md`.

`run.sh` writes `out/<service>.json` (newman JSON reporter), `out/<service>.cli`, and
`out/summary.txt`. Use `--delay <ms>` to give async Operation workers time; use
`--bail` to stop on first failure.

### 6.2 Data-plane + token-exchange harness

```bash
cd tests/newman

# docker push/pull through authz + IAM /iam/token shim + Hydra federation
./scripts/dataplane-e2e.sh --env environments/fe3455.postman_environment.json
#   (drives docker login/push/pull + raw-HTTP /v2/ and /iam/token; requires the
#    docker CLI, registry.kacho.local reachability, an SA-key, and live Hydra)
```

The harness is the **functional-gate** for REG-TX-22: unit/integration green ≠ works.
Report its outcome into `RESULTS.md` alongside the newman summary.

### 6.3 Environment variables (registry-specific)

| Env var | Role |
|---|---|
| `baseUrl` | api-gateway REST entry (fe3455 forward / kind NodePort) |
| `existingProjectId` | project where `jwtProjectEditorA` holds registry create/edit rights |
| `existingProjectCrossId` | second project for cross-tenant no-leak / hidden-project tests |
| `jwtProjectEditorA` | subject with `v_create`/`v_update` in `existingProjectId` |
| `jwtProjectViewerA` | viewer (read-only) in `existingProjectId` |
| `jwtStranger` | subject with no bindings (existence-hiding target) |
| `jwtServiceAccountEditor` | SA subject for owner-tuple / SA-key flows |
| `runId` | per-run isolation suffix (set by `run.sh`) |
| `saKeyStandard` / `saKeyFederated` | data-plane: docker + k8s SA-keys (harness only) |
| `registryHost` | data-plane: `registry.kacho.local` ingress host (harness only) |
| `hydraTokenUrl` | data-plane: Hydra `/oauth2/token` (harness only) |

---

## 7. Out-of-scope (explicitly NOT covered by newman/harness)

- InternalRegistryService GC execution internals and host/placement stats (mTLS-only,
  integration-tested; the public surface must never expose them — ban #6).
- zot storage HA / S3 failover and real garbage-collection reclaim (infra, REG-44).
- OCI 1.1 Referrers **signature/SBOM verification** semantics (Referrers API is a reserved
  slot — REG-42 asserts routing/presence, not signature trust).
- Rate-limit tuning thresholds (REG-43 asserts a limit exists and returns 429, not exact QPS).
- kubelet image-credential-provider packaging (roadmap R-3; the exchange mechanism is tested,
  the plugin binary is not).
