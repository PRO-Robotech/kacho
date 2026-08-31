# PRODUCT-REQUIREMENTS — normative spec (KAC-NLB)

This document enumerates the **normative product requirements** (`REQ-*`)
verified by the kacho-nlb newman regression suite. Each requirement maps
1:N to GWT scenarios in `docs/specs/sub-phase-4.0-nlb-acceptance.md` and
1:N to case-ids in `CASES-INDEX.md` (the `Verifies REQ-*` annotations).

The list is consumed by code-review and acceptance gates: any product change
that affects behaviour covered here must be reflected as either an updated
`REQ-*` (and corresponding case) or — if intentionally relaxed — explicitly
noted in `docs/engineering/architecture/08-known-divergences.md` of kacho-nlb.

> Style: **normative wording** (MUST / MUST NOT / SHOULD). Verbatim error
> texts are quoted exactly. Wording mirrors kacho-vpc PRODUCT-REQUIREMENTS.md
> structure for cross-service consistency.

---

> **Sub-phase 8.1 supersession (VIP model).** The VIP-handling requirements below
> (notably REQ-NLB-CR-01/CR-02 and the listener-level VIP REQ-LST-CR-AUTO-VIP /
> REQ-LST-CR-BYO / REQ-LST-CR-INTERNAL) are **superseded** — приёмкой под-фазы 8.1 (модель размещения и источника VIP у балансировщика). **Документа с таким именем в репозитории воркспейса нет и никогда не было** — проверено `git log --all` по пути: ноль коммитов. Приёмки nlb, которые там есть, лежат в `docs/specs/` того репозитория и названы по схеме `sub-phase-NLB-*`; какая из них покрывает этот набор — открытый вопрос, и он записан как долг, а не закрыт догадкой. Прежнее имя не воспроизводится: в обратных кавычках оно читается как существующий документ.
> Under 8.1 the VIP moved from the Listener to the LoadBalancer: every LB carries a
> per-family VIP *source* on Create (`v4Source`/`v6Source` = `{subnetId}`|`{addressId}`|
> `{public}`), plus `placementType` (INTERNAL only) and `disabledAnnounceZones`
> (REGIONAL only); output resolves to `v4AddressId`/`v6AddressId`. `securityGroupIds`,
> `crossZoneEnabled` and `networkId` inputs are removed. The authoritative Given-When-Then
> for the LoadBalancer VIP contract is the 8.1 acceptance (8.1-01..8.1-36), verified by
> the `NLB-CR-*`/`NLB-UPD-STATE-IMMUTABLE-*`/`NLB-DEL-CRUD-RELEASE-*`/`NLB-GET-STATE-LEAN-*`
> cases. Lifecycle / immutability / pagination / authz REQs below remain valid.

## REQ-NLB-* — NetworkLoadBalancer resource

| Req | Statement | GWT source | Tests |
|---|---|---|---|
| **REQ-NLB-CR-01** | Create with valid (project_id, region_id, name, type) MUST succeed and return Operation envelope; resource MUST be persisted with `status='INACTIVE'` when no listeners + no attached TG exist at creation time. | NLB-001 | `NLB-CR-CRUD-OK` |
| **REQ-NLB-CR-02** | Create with `type=INTERNAL` MUST be accepted at the LB level without requiring any subnet/network fields; subnet binding happens at the Listener level. | NLB-002 | `NLB-CR-CRUD-INTERNAL` |
| **REQ-NLB-CR-VAL-NAME** | `name` MUST match the `LbName` domain regex; invalid characters (`_`, `!`, uppercase) MUST be rejected with `INVALID_ARGUMENT` and `BadRequest.field_violations[0].field='name'`. | NLB-003 / NLB-004 | `NLB-CR-VAL-NAME-REGEX`, `NLB-CR-VAL-NAME-UNDERSCORE`, `NLB-CR-VAL-NAME-UPPERCASE` |
| **REQ-NLB-CR-NEG-REGION** | Create with unknown `region_id` MUST fail cross-domain validation against `kacho-geo.RegionService.Get`. Per the by-lane code-split (`api-conventions.md`) a non-existent peer ref is a precondition on someone else's resource, so the failure is `FAILED_PRECONDITION` ("Region \<id\> not found") carrying the reason token `PEER_RESOURCE_MISSING`, and neither `NOT_FOUND` nor `INVALID_ARGUMENT`. Region validation runs in the async Create worker, so it surfaces on the polled Operation's `error` (code 9), not synchronously. The HTTP status is 400 either way, which is why the divergence went unnoticed for so long. | NLB-006 | `NLB-CR-NEG-REGION-UNKNOWN` |
| **REQ-NLB-GET-01** | `Get` for an existing LB MUST return the full LB message with `created_at`/`updated_at` timestamps truncated to seconds. | NLB-010 | `NLB-GET-CRUD-OK` |
| **REQ-NLB-GET-NEG** | `Get` with unknown id MUST return `NOT_FOUND` with `ResourceInfo{resource_type:"NetworkLoadBalancer"}`. | NLB-011 | `NLB-GET-NEG-NF-UNKNOWN` |
| **REQ-NLB-LST-01** | `List` filtered by `project_id` MUST return paginated array; `pageSize=0` MUST apply server default; `pageSize > 1000` MUST return `INVALID_ARGUMENT`. | NLB-012, NLB-013 | `NLB-LST-*` patterns |
| **REQ-NLB-UPD-01** | `Update` with `update_mask` containing mutable fields MUST apply only those fields; full-PATCH (empty mask) MUST be rejected with `INVALID_ARGUMENT` "update_mask is required". | NLB-015, NLB-019 | `NLB-UPD-CRUD-OK`, `NLB-UPD-STATE-MASK-EMPTY` |
| **REQ-NLB-UPD-OCC** | Concurrent `Update` MUST use atomic xmin OCC: exactly one transaction succeeds; loser MUST receive `ABORTED` "concurrent update; please retry". | NLB-021 | `NLB-UPD-CONF-OCC-RACE` |
| **REQ-NLB-IMMUTABLE-TYPE** | `type` MUST be immutable after Create; `update_mask=["type"]` MUST return `INVALID_ARGUMENT` "type is immutable after NetworkLoadBalancer.Create". | NLB-016 | `NLB-UPD-STATE-IMMUTABLE-TYPE` |
> [!note] Сняты требования к глаголам, которых в контракте нет
> `REQ-NLB-LIFE-01` / `-02` описывали `Start` / `Stop`, `REQ-NLB-ATT-01` / `REQ-NLB-DET-01` —
> `AttachTargetGroup` / `DetachTargetGroup`. Ни одного из четырёх глаголов контракт не
> производит: административное состояние выражается полем `adminState`, привязка группы целей —
> полем `Listener.targetGroupId`. Кейсов, названных в этих строках, в наборе **ноль** — то есть
> ведомость требовала покрытия у поведения, которого нет, и это покрытие нельзя было ни
> получить, ни признать недостающим (задача продукта #1617).

| **REQ-NLB-MV-01** | `Move` MUST atomically update `load_balancers.project_id` AND `listeners.project_id` (denorm sync) in the same transaction and emit one `MOVED` outbox event. | NLB-027 | `NLB-MV-CRUD-OK` |
| **REQ-NLB-MV-NEG** | `Move` MUST be rejected with `FAILED_PRECONDITION` when a child listener is wired to a target group: "NetworkLoadBalancer has a listener wired to a target group; repoint before Move". | NLB-029 | `NLB-MV-NEG-ATTACHED-TG` |
| **REQ-NLB-SAME-REGION** | A listener and the target group it wires MUST be in the same `region_id`; mismatch MUST return `FAILED_PRECONDITION`. Enforced on `Listener.Create` and `Listener.Update` (the wiring is `Listener.targetGroupId`; the LB has no direct TG link). | NLB-032 | `LST-UPD-STATE-DEFAULT-TG-REGION-MISMATCH` |
| **REQ-NLB-GTS-01** | `GetTargetStates` MUST return a deterministic state per target: `INITIAL` while `age < interval × healthy_threshold`, `HEALTHY` afterwards, `DRAINING` when target is mid-Phase-A drain, `INACTIVE` when LB.status=STOPPED. | NLB-040, NLB-041 | `NLB-GTS-CRUD-EMPTY`, `NLB-GTS-STATE-LB-STOPPED` |
| **REQ-NLB-DEL-01** | `Delete` MUST succeed when LB has no listeners + no attached TG + `deletion_protection=false`. | NLB-043 | `NLB-DEL-CRUD-OK` |
| **REQ-NLB-DEL-PROT** | `Delete` with `deletion_protection=true` MUST return `FAILED_PRECONDITION` "deletion_protection is enabled; disable via Update before deleting". | NLB-044 | `NLB-DEL-STATE-PROTECTION` |
| **REQ-NLB-DEL-LISTENERS** | `Delete` with ≥1 listener MUST return `FAILED_PRECONDITION` "NetworkLoadBalancer has N listener(s); delete them first" (sync precheck for UX, FK 23503 backstop for race). | NLB-045 | `NLB-DEL-STATE-HAS-LISTENER` |
| **REQ-NLB-DEL-RACE** | Race between Delete and Attach MUST resolve deterministically: Delete fails with `FAILED_PRECONDITION` via FK 23503, no torn state. | NLB-047 | `NLB-DEL-CONF-FK-RACE` |

## REQ-LST-* — Listener resource

| Req | Statement | GWT source | Tests |
|---|---|---|---|
| **REQ-LST-CR-AUTO-VIP** | Create EXTERNAL Listener without `address_id` MUST trigger `vpc.InternalAddressService.AllocateExternalIP` synchronously inside the worker; allocated IP populates `allocated_address`. | LST-001 | `LST-CR-CRUD-AUTO-VIP` |
| **REQ-LST-CR-BYO** | Create with `address_id` MUST validate the address exists, same project, matching ip_version, and atomically CAS `used_by` from "" to "nlb_listener:<lst-id>". | LST-002 | `LST-CR-CRUD-BYO` |
| ~~**REQ-LST-CR-INTERNAL**~~ **(RETIRED — 8.1)** | Was: INTERNAL Listener MUST accept `subnet_id` (required) and allocate an internal IP against that subnet. **No longer a listener requirement**: the VIP moved Listener→LoadBalancer, `subnet_id` left `CreateListenerRequest`, and the listener allocates nothing (`listener_service.proto` `reserved 8, 9`; `Listener` `reserved 12-15`; migration 0028 dropped the columns). An INTERNAL LB now names its VIP source on the parent via `v4Source`/`v6Source`. | superseded by sub-phase-8.1 acceptance | `LST-CR-CRUD-INTERNAL` (positive path only); VIP-source rule → `NLB-CR-VAL-SOURCE-REQUIRED`, `NLB-CR-VAL-PLACEMENT-MISMATCH` |
| ~~**REQ-LST-VAL-INTERNAL-SUBNET**~~ **(RETIRED — 8.1)** | Was: INTERNAL Listener without `subnet_id` MUST return `INVALID_ARGUMENT` "subnet_id is required for INTERNAL load balancer". **The field does not exist**, so a create without it is valid and the service is specified to accept it — the requirement has no subject, not a broken implementation. Its case `LST-CR-VAL-INTERNAL-NO-SUBNET` is retired (rationale in `cases/listener.py`); keeping it would have meant a permanently-red case asserting a refusal correct behaviour cannot give. | superseded by sub-phase-8.1 acceptance | `NLB-CR-VAL-SOURCE-REQUIRED`, `NLB-CR-VAL-PLACEMENT-MISMATCH` |
| **REQ-LST-BYO-USED** | BYO Create with already-used `address_id` MUST return `FAILED_PRECONDITION` "address <id> is already in use by <owner>". Listener MUST NOT be created. | LST-003 | `LST-CR-STATE-BYO-USED` |
| **REQ-LST-BYO-IPV** | BYO with mismatched `ip_version` MUST return `INVALID_ARGUMENT` "address ip_version <X> does not match listener ip_version <Y>". | LST-004 | `LST-CR-VAL-BYO-IP-VERSION-MISMATCH` |
| **REQ-LST-UNIQ-PORT-PROTO** | Duplicate `(load_balancer_id, port, protocol)` MUST return `ALREADY_EXISTS` enforced by UNIQUE constraint in `listeners`. | LST-010 | `LST-CR-CONF-DUP-PORT-PROTO` |
| **REQ-LST-COMP-FREEIP** | If Listener INSERT fails after VIP allocation, worker MUST execute `vpc.FreeIP` compensation before `ops.MarkDone(error)`. | LST-015 | `LST-CR-CONF-VIP-COMPENSATION` |
| **REQ-LST-DEL-AUTO-FREE** | Delete of auto-VIP Listener MUST call `vpc.FreeIP` (returning the IP to its pool) before deleting the row. | LST-022 | `LST-DEL-CRUD-AUTO-VIP-FREE` |
| **REQ-LST-TG-SAME-PROJECT** | The wired `target_group_id` (and its legacy twin `default_target_group_id`) MUST resolve to a TargetGroup of the listener's OWN project, on Create and on Update. A TargetGroup of another project MUST be refused with the SAME response as a non-existent one (hide-existence, no existence-oracle) and MUST NOT be persisted; the composite FK `(default_tg_fk, project_id) → target_groups(id, project_id)` is the atomic backstop. | — | `LST-CR-SEC-TG-CROSS-PROJECT`, `LST-UPD-SEC-TG-CROSS-PROJECT` |

## REQ-TGR-* — TargetGroup resource

| Req | Statement | GWT source | Tests |
|---|---|---|---|
| **REQ-TGR-CR-01** | Create TG MUST accept inline `targets[]` and an embedded `health_check{}` object; persists in `target_groups` + `targets` tables in the same TX. | TGR-001 | `TGR-CR-CRUD-OK` |
| **REQ-TGR-CR-EMPTY** | Create TG with empty `targets[]` MUST succeed (targets can be added later via `AddTargets`). | TGR-002 | `TGR-CR-CRUD-EMPTY-TARGETS` |
| **REQ-TGR-VAL-HC** | `health_check` MUST specify exactly one of {tcp, http, https, grpc}. Multiple or zero MUST return `INVALID_ARGUMENT` "health_check must specify exactly one of: tcp, http, https, grpc". | TGR-003, TGR-004 | `TGR-CR-VAL-HC-MULTIPLE-PROBES`, `TGR-CR-VAL-HC-NONE-SET` |
| **REQ-TGR-LST-01** | List TG filtered by project + region MUST return only matching rows. | TGR-016 | `TGR-LST-CRUD-OK`, `TGR-LST-FILTER-REGION` |
| **REQ-TGR-DEL-01** | Delete TG without attachments + targets MUST succeed and emit `DELETED` outbox event. | TGR-021 | `TGR-DEL-CRUD-OK` |
| **REQ-TGR-DEL-ATTACHED** | Delete TG with ≥1 attached LB MUST return `FAILED_PRECONDITION` "TargetGroup is attached to N load balancer(s); detach first". | TGR-022 | `TGR-DEL-NEG-HAS-ATTACHED-LB` |
| **REQ-TGR-DEL-TARGETS** | Delete TG with ≥1 target row MUST return `FAILED_PRECONDITION` "TargetGroup has N target(s); remove them first via RemoveTargets". | TGR-023 | `TGR-DEL-NEG-HAS-TARGETS` |

## REQ-TGT-* — Target operations (Add / Remove)

| Req | Statement | GWT source | Tests |
|---|---|---|---|
| **REQ-TGT-4WAY-EXACTLY-ONE** | Each target MUST specify exactly one of {instance_id, nic_id, ip_ref, external_ip}. Zero or multiple MUST return `INVALID_ARGUMENT`. | TGT-009, TGT-010 | `TGR-CR-VAL-TARGET-NO-IDENTITY`, `TGR-CR-VAL-TARGET-MULTIPLE-IDENTITY` |
| **REQ-TGT-4WAY-INSTANCE** | AddTargets with `instance_id` variant MUST peer-validate against `compute.InstanceService.Get` and resolve primary IP. | TGT-001 | `TGT-ADD-CRUD-INSTANCE-ID` |
| **REQ-TGT-BOGON** | `external_ip.address` in any bogon range (loopback, unspecified, link-local, multicast, broadcast) MUST be rejected `INVALID_ARGUMENT`. | TGR-011 | `TGT-ADD-VAL-BOGON-LOOPBACK`, `TGR-CR-VAL-TARGET-BOGON-*` (5) |
| **REQ-TGT-IPREF-CIDR** | `ip_ref.address` MUST fall within `ip_ref.subnet_id`'s CIDR; mismatch MUST be rejected. | TGT-004 | `TGT-ADD-VAL-IP-REF-NOT-IN-SUBNET` |
| **REQ-TGT-PEER-INSTANCE** | Unknown `instance_id` MUST cause the operation to fail with `INVALID_ARGUMENT` "target[N].instance_id '<id>' not found"; nothing is committed. | TGT-012 | `TGT-ADD-NEG-INSTANCE-UNKNOWN` |
| **REQ-TGT-PEER-REGION** | Each target's resolved region MUST match `target_group.region_id`. Mismatch MUST be rejected. | TGT-006 | `TGT-ADD-NEG-INSTANCE-REGION-MISMATCH` |
| **REQ-TGT-IDEM-ID** | `AddTargets` with a duplicate identity MUST be a no-op (ON CONFLICT DO NOTHING); no outbox event if zero rows inserted. | TGT-002 | `TGT-ADD-IDEM-DUP-INSTANCE` |
| **REQ-TGT-RM-PHASE-A** | `RemoveTargets` Phase A MUST mark matching targets `status='DRAINING', drain_started_at=now()` synchronously in the worker (latency <500ms) and emit `nlb_target_group:<tg> UPDATED`. | TGT-011 | `TGT-RM-STATE-PHASE-A-DRAINING` |
| **REQ-TGT-RM-PHASE-B** | Background `target_drain_runner` MUST `DELETE` targets with `status='DRAINING' AND drain_started_at < now() - tg.deregistration_delay_seconds * '1 second'::interval`. | TGT-013 | `TGT-RM-STATE-PHASE-B-RUNNER` |
| **REQ-TGT-RM-IDEM** | `RemoveTargets` with identity not currently in the TG MUST be a no-op (0 rows affected, no outbox event). | TGT-012 | `TGT-RM-IDEM-NOT-PRESENT` |

## REQ-OP-* — OperationService

| Req | Statement | GWT source | Tests |
|---|---|---|---|
| **REQ-OP-GET-INFLIGHT** | `Get` for an in-flight op MUST return `done=false` with metadata; eventual poll MUST converge to `done=true`. | OP-001, OP-002 | `OP-GET-CRUD-IN-FLIGHT`, `OP-GET-CRUD-COMPLETED` |
| **REQ-OP-GET-NEG-PREFIX** | `Get` with a malformed opId (unknown prefix, non-Crockford-base32) MUST return `INVALID_ARGUMENT` `invalid operation id "<X>"` (кавычки двойные — глагол `%q`
производителя `gateway/internal/opsproxy`; цитата с одинарными расходилась с ответом
края, #1400) — та же форма, что у остальных доменов: полоса операций общая. | OP-003 | `OP-GET-NEG-NF-INVALID-PREFIX`, `OP-GET-NEG-NF-VALID-PREFIX` |
| **REQ-OP-LST-01** | There is NO project-wide `List` on OperationService — the contract carries only `Get` and `Cancel`, and clients poll `Get(id)`. A collection path that matches no method MUST fail closed at the edge (`PERMISSION_DENIED`, code 7, "catalog: no entry for method"), never a 200 and never a 5xx/mux-404. | OP-004 | `OP-LST-NEG-UNROUTED-FAIL-CLOSED` |
| **REQ-OP-CANCEL-DONE** | `Cancel` on already-done op MUST return `FAILED_PRECONDITION` "operation is already completed". | OP-006 | `OP-CANCEL-STATE-ALREADY-DONE` |

## REQ-AZD-* — Authorization (FGA REBAC)

| Req | Statement | GWT source | Tests |
|---|---|---|---|
| **REQ-AZD-NLB-CR** | Subject with only `viewer` on project MUST be denied `loadbalancer.networkLoadBalancers.create`; verbatim message "permission denied: loadbalancer.networkLoadBalancers.create on project:<id>". | AZD-001 | `AZD-NLB-CR-VIEWER-DENIED` |
| **REQ-AZD-NLB-MV-SCOPE** | `Move` MUST perform scope-conditional Check on BOTH src project AND dst project; failure on dst MUST return `PERMISSION_DENIED` referencing the dst project. | AZD-006 / NLB-028 | `AZD-NLB-MV-SCOPE-DST-DENIED` |
| **REQ-AZD-LST-CR** | Listener.Create requires `editor` on parent LB (cascades via `nlb_listener.load_balancer` relation). Viewer on LB MUST be denied. | AZD-009 | `AZD-LST-CR-VIEWER-DENIED` |
| **REQ-AZD-TGR-ADD** | TG.AddTargets requires `editor` on TG. Viewer MUST be denied. | AZD-008 | `AZD-TGR-ADD-VIEWER-DENIED` |
| **REQ-AZD-OP-CANCEL** | Operation.Cancel MUST verify subject == operation creator (owner scope outside FGA relations); other subjects MUST be denied. | AZD-011 | `AZD-OP-CANCEL-NON-CREATOR-DENIED` |
| **REQ-AZD-FAIL-CLOSED** | When the authorization store is unreachable the edge MUST refuse (fail-closed) — never count "no answer about permissions" as allowed. | AZD-012 | `AUTHZ-FAILCLOSED-OPENFGA-DOWN` (services/iam/tests/newman, own wave via `scripts/run-failclosed.sh`: the condition has to be CREATED, so the case cannot live in a suite that runs against a healthy stand) |
| **REQ-AZD-ANON** | Requests without Authorization header MUST return `UNAUTHENTICATED` (gRPC code 16) BEFORE any FGA Check (auth interceptor precedes authz interceptor). | AZD-027 | `AZD-NLB-CR-ANONYMOUS-UNAUTH` |
| **REQ-AZD-CATALOG** | The `loadbalancer` module and its resources (`networkLoadBalancers`, `listeners`, `targetGroups`) MUST be present and verb-bearing in the live grantable catalogue, and EACH MUST declare the verbs the nlb permission strings are built from — `get`/`list`/`update`/`delete`, and on `targetGroups` also `addTargets`/`removeTargets` — otherwise a `loadbalancer.*` grant compiles to a silent no-op. The verb set belongs to the TYPE (`CatalogResource.verbs`), NOT to the platform-wide intersection `closedVerbs`, whose own contract declares narrowing as normal behaviour (#1128): a verb dropped by one unrelated type leaves the intersection without saying anything about nlb. The compiled per-RPC permission table is a separate artefact, generated from proto and byte-compared by `make permission-catalog-check`. | AZD-019 | `AZD-PERMISSION-CATALOG-COMPLETE` (reads `GET /iam/v1/permissionCatalog`) |
| **REQ-AZD-CUSTOM-ROLE** | A custom `iam.Role` whose `permissions[]` map to `loadbalancer.*` MUST resolve to one of the 3 FGA relations (viewer/editor/owner) using the "narrowest covering" rule (design §6.4). | AZD-017 | `AZD-CUSTOM-ROLE-OPERATOR-START` |
| **REQ-AZD-LIFECYCLE-DEL** | `DELETED` lifecycle event (D-13 stream) MUST cause `openfga.DeleteByObject(<resource>)` within ≤10s; subsequent Check MUST return DecisionNoPath → fail-closed. | AZD-022 | `AZD-LIFECYCLE-DELETED-TUPLE-CLEANUP` |
| **REQ-AZD-CACHE-INVAL** | Revoking an AccessBinding MUST propagate to the FGA Check cache within ≤10s (via `pg_notify('kacho_iam_subjects', '<id>')`); subject's next Check MUST deny. | AZD-016 | `AZD-CACHE-INVALIDATION-REVOKE` |
| **REQ-AZD-OWNER** | The creator of any resource MUST have an `owner` tuple written synchronously (D-11) before the worker commits its TX; failure to write MUST abort the operation. | AZD-021 | `AZD-OWNER-RELATION-CREATOR` |
| **REQ-AZD-INTERNAL-MTLS** | `InternalResourceLifecycleService.Subscribe` MUST only be reachable from mTLS-authenticated identities matching the kacho-iam SPIFFE ID; external clients MUST be rejected. | AZD-025 | `AZD-LIFECYCLE-INTERNAL-MTLS-ONLY` |

## REQ-DB-* — Storage invariants (covered indirectly via newman)

| Req | Statement | GWT source | Tests |
|---|---|---|---|
| **REQ-DB-LABEL-CHECK** | `kacho_labels_valid(labels)` CHECK constraint MUST reject `labels` with >64 pairs (23514 → `INVALID_ARGUMENT`). | DB-002 | `NLB-CR-VAL-LABELS-OVER-64`, `TGR-CR-VAL-LABELS-OVER-64` |
| **REQ-DB-NLB-NAME-UNIQ** | Partial UNIQUE `(project_id, name) WHERE name <> ''` on `load_balancers` MUST enforce duplicate name rejection (23505 → `ALREADY_EXISTS`). | DB-005 / NLB-009 | `NLB-CR-CONF-ALREADY-EXISTS` |
| **REQ-DB-TGR-NAME-UNIQ** | Same as above for `target_groups`. | TGR-014 | `TGR-CR-CONF-ALREADY-EXISTS` |

---

## Style guard (нормативно)

Verbatim error texts in the table above are the **contract**. Any drift —
text differs, code differs, an extra `details[]` slot, a wrong field name
inside `BadRequest.field_violations` — is a **regression**, not a "minor cosmetic
fix". The author of any product PR touching error paths is responsible for
matching the wording exactly.

When a wording change is intentional (because the existing text is genuinely
wrong, ambiguous, or misleading), the workflow is:

1. Open a kacho-nlb GitHub Issue with `label: regression-text-change`.
2. Update this document (the REQ row + tests it references).
3. Update the corresponding case in `tests/newman/cases/*.py`.
4. Merge the product PR with all three changes in lockstep.
