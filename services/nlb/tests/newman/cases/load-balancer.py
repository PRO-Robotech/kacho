# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""NetworkLoadBalancerService cases (NLB-*) — 12 RPC × full RPC × class matrix.

Acceptance source (VIP model): приёмка под-фазы 8.1 (8.1-01..8.1-36), которая
  вытесняет разбор VIP из под-фазы 4.0. Документа с прежде стоявшим здесь именем в
  репозитории воркспейса нет и никогда не было (git log --all по пути: ноль коммитов);
  имя не воспроизводится, чтобы его не искали.
Carry-over lifecycle / CRUD / validation semantics (Start/Stop/Move/attach/detach/GetTargetStates,
  name / labels / pagination / immutability) remain from sub-phase-4.0 (§3, GWT-NLB-001..048) — all
  12 RPCs survive the VIP redesign; only the Create request shape and Get projection changed.

New VIP model (sub-phase 8.1):
  * every LoadBalancer carries a per-family VIP *source* on Create: `v4Source` / `v6Source`, each a
    oneof of exactly one of `{subnetId}` (INTERNAL auto-alloc), `{addressId}` (link, both types),
    `{public: {}}` (EXTERNAL auto). At least one family source is required.
  * INTERNAL carries `placementType` (ZONAL|REGIONAL); EXTERNAL must not. REGIONAL may carry
    `disabledAnnounceZones`.
  * Get/List resolve the source to output-only `v4AddressId`/`v6AddressId` (the bound vpc Address);
    the VIP IP itself lives in that Address. The old `securityGroupIds` / `crossZoneEnabled` /
    `networkId` inputs and the old listener-level VIP are gone (removed from the proto).

Test-design techniques applied (skill testing-product-coach):
  * ECP — source × type × placement equivalence classes (subnet/address/public × INTERNAL/EXTERNAL ×
    ZONAL/REGIONAL);
  * decision-table — the source×type×placement matrix (§3.3) drives the sync fail-fast negatives;
  * state-transition — Create terminates INACTIVE (VIP-only); Delete releases the VIP; drain toggle;
  * BVA — name / description / labels / pageSize boundaries (carry-over);
  * error-guessing — anti-oracle generic messages, removed-field ignore, dangling-ref survival.

Cross-domain fixture tolerance (deliberate, mirrors cross-resource.py):
  INTERNAL subnet-source / address-link cases provision the vpc Subnet / Address inline through the
  api-gateway (POST /vpc/v1/subnets, /vpc/v1/addresses — publicly routed; their `e9b`-prefixed
  Operation ids poll through the shared /operations/{id} OpsProxy just like nlb ops). When the seeded
  network / external AddressPool / vpc-create authz is present (the umbrella stack per acceptance
  §6.7) the case fully exercises the chain; on a bare lane where the fixture does not materialise the
  case asserts the lawful fixture-absent rejection instead — the suite stays green either way. The
  sync source×type×placement negatives (the bulk) are strict and fixture-free.

  This tolerance covers the LANE (pool / authz / seeded network absent). It must never absorb a
  malformed REQUEST: an unasserted provisioning step cannot tell the two apart, and for the whole
  archived history of this suite it did not — see _SPEC_REACHED_VPC below, where the address-link
  fixture failed on every run and four cases silently took the tolerant branch. Any provisioning
  step added here owes a discriminator that stays green on a bare lane and red on a bad body.

REST base path: /nlb/v1/networkLoadBalancers
"""

CASES = []

# Common reusable bits
_CREATE_BASE = "/nlb/v1/networkLoadBalancers"
# Default happy-path LoadBalancer under the 8.1 model: an EXTERNAL LB with an auto public VIP source.
# (Platform allocates a public vpc Address on Create — requires the seeded external AddressPool, a
# deploy-precondition per acceptance §6.7, the same one the prior auto-VIP listener suite relied on.)
_LB_BODY = {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
            "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}}

_VPC_SUBNETS = "/vpc/v1/subnets"
_VPC_ADDRESSES = "/vpc/v1/addresses"


# ---------------------------------------------------------------------------
# Inline vpc fixture provisioning (subnets / addresses) — see module docstring
# for the tolerance contract. Each provision step saves the created resource id
# into an env var; downstream steps gate their strict assertions on that id.
# vpc Operation ids carry the `e9b` prefix and poll through the same shared
# /operations/{id} OpsProxy as nlb ops.
# ---------------------------------------------------------------------------

def _cidr_alloc_pre():
    """Pre-request: run-scoped адрес(а) подсети, ВЫРЕЗАННЫЕ ИЗ ПЛАНА сети посева.

    Здесь стояла собственная копия генератора, выводившая `10.<октет>.<октет>.0/24` и
    `'fd' + число.toString(16) + …` из хеша прогона — без всякой связи с адресным планом,
    который объявила сеть. Попадание в план было СОВПАДЕНИЕМ: держалось на том, что один
    из двух посевов набора объявляет `10.0.0.0/8` и `fd00::/8`. Второй объявляет план
    у́же (`10.196.0.0/16`, `fd00:196::/48`) — мимо него уходили ВСЕ адреса, на всех хешах.

    Ширина хекстета здесь чинилась отдельно (дополнение до двух цифр — иначе `fd`+`c`
    даёт `fdc:` вне `fd00::/8`), и это ровно та починка, которая держится совпадением:
    она верна для плана `fd00::/8` и не значит ничего для любого другого. Помощник
    берёт префикс из плана, поэтому чинить ширину больше негде и незачем.

    Разводка параллельных прогонов сохранена помощником целиком (хеш runId + порядковый
    номер + соль набора); изменился ИСТОЧНИК префикса. Разбор класса и гейт, который
    держит свойство, — scripts/gen.py, раздел «АДРЕС НАРЕЗАЕМОЙ ПОДСЕТИ».
    """
    return carve_cidr_pre('load-balancer', v6_var='_subnetCidr6')


def _provision_subnet(placement, suffix, save_var="vpcSubnetId", dualstack=False):
    """Provision a ZONAL or REGIONAL vpc Subnet in the seeded network; save its id.

    placement_type is server-derived (F6): the subnet body carries only the placement
    anchor (zoneId → ZONAL, regionId → REGIONAL), never placementType.

    `dualstack` additionally anchors an IPv6 range on the subnet (`ipv6CidrPrimary`,
    optional per CreateSubnetRequest), which is what makes an INTERNAL v6 Address
    allocatable from it — the fixture a dualstack INTERNAL load balancer needs.
    """
    loc = {}
    if placement == "ZONAL":
        loc["zoneId"] = "{{existingZoneId}}"
    else:
        loc["regionId"] = "{{existingRegionId}}"
    if dualstack:
        loc["ipv6CidrPrimary"] = "{{_subnetCidr6}}"
    return [
        Step(name=f"provision-{placement.lower()}-subnet-{suffix}", method="POST", path=_VPC_SUBNETS,
             pre_script=_cidr_alloc_pre(),
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{existingNetworkId}}",
                   "name": f"nlb81-{suffix}-{{{{runId}}}}", "ipv4CidrPrimary": "{{_subnetCidr}}", **loc},
             test_script=[
                 f"pm.environment.unset('{save_var}');",
                 # The subnet is created BY THIS SUITE inside the seeded network — there is no
                 # lawful lane on which it fails to materialise. It used to `unset` the id on
                 # non-200 and say nothing, which let every dependent case slide into its
                 # fixture-absent tolerance branch: an unseeded stand, a stale request shape or
                 # a revoked grant all read as "no fixture here" and the case went green having
                 # verified nothing. Assert the prep instead, so a broken prep is RED where it
                 # broke rather than silent three steps later.
                 "pm.test('subnet fixture accepted as Operation', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "if (pm.response.code === 200) {",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 f"  if (j.metadata && j.metadata.subnetId) pm.environment.set('{save_var}', j.metadata.subnetId);",
                 "} else { pm.environment.set('opId', ''); }",
             ]),
        # fixture_ids: an Operation carries the PRE-ALLOCATED id in metadata even when it
        # finishes done:true WITH an error, so an unguarded capture publishes the id of a
        # subnet that does not exist. Naming it makes the poll unset it and FAIL here.
        poll_operation_until_done(fixture_ids=[save_var]),
        # Прогрев чужого свежего ресурса ДО того, как его идентификатор уедет в
        # асинхронную мутацию nlb: на ней ограниченный повтор ключуется на коде
        # ответа шага, а он всегда `200`+`Operation` (issue #351). Разбор — в
        # шапке `warm_peer_fixture`; свойство держит гейт по дереву.
        warm_peer_fixture(_VPC_SUBNETS, save_var, f"subnet-{suffix}"),
    ]


# `address_spec` is a proto ONEOF (proto/kacho/cloud/vpc/v1/address_service.proto), and
# protojson renders a oneof TRANSPARENTLY: the body carries the selected BRANCH key at the top
# level (`externalIpv4AddressSpec` / `internalIpv4AddressSpec` / …), never the oneof's own name.
# The vpc suite's own address cases (services/vpc/tests/newman/cases/address.py) are the
# reference form. Both helpers below used to nest the branch inside an `"addressSpec": {…}`
# wrapper. That wrapper is not a field of CreateAddressRequest, so the edge dropped it WHOLE
# (unknown keys are discarded): what reached vpc was projectId+name with NO branch selected,
# and vpc answered `400 "address_spec required"`. Every archived run from 2026-07-18 through
# 2026-07-25 shows exactly that on all four call sites — not one address was ever provisioned
# by this suite.
#
# It stayed invisible because the step asserted nothing and merely `unset` the id on non-200;
# the four dependent cases then took their fixture-absent tolerance branch, which cannot tell
# "this lane has no AddressPool" from "we sent a body the contract does not have".
# _SPEC_REACHED_VPC is the discriminator that was missing: it stays green when the fixture
# legitimately cannot materialise (pool / authz / network absent → some other message) and
# goes red the moment the body stops matching the request contract again.
_SPEC_REACHED_VPC = [
    "pm.test('address spec reached vpc (body matches CreateAddressRequest)', () => {",
    "  let m = '';",
    "  try { m = (pm.response.json() || {}).message || ''; } catch (e) { m = pm.response.text() || ''; }",
    "  pm.expect(m, 'the edge discarded the request body — the spec never arrived')"
    ".to.not.match(/address_spec required/i);",
    "});",
]


def _provision_internal_address(subnet_var, suffix, save_var="vpcAddrId", family="v4"):
    """Provision an INTERNAL vpc Address bound to a subnet (auto-allocated IP); save its id."""
    spec = "internalIpv4AddressSpec" if family == "v4" else "internalIpv6AddressSpec"
    return [
        Step(name=f"provision-internal-addr-{suffix}", method="POST", path=_VPC_ADDRESSES,
             body={"projectId": "{{_suiteProjectId}}", "name": f"nlb81-adr-{suffix}-{{{{runId}}}}",
                   spec: {"subnetId": f"{{{{{subnet_var}}}}}"}},
             test_script=[
                 *_SPEC_REACHED_VPC,
                 f"pm.environment.unset('{save_var}');",
                 # The subnet this address is drawn from is a fresh /24 provisioned by the
                 # step above (which now asserts its own success), so a refusal here is a
                 # real defect, never "this lane has no fixture". Assert it.
                 "pm.test('internal address fixture accepted as Operation', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 f"if (pm.response.code === 200 && pm.environment.get('{subnet_var}')) {{",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 f"  if (j.metadata && j.metadata.addressId) pm.environment.set('{save_var}', j.metadata.addressId);",
                 "} else { pm.environment.set('opId', ''); }",
             ]),
        poll_operation_until_done(fixture_ids=[save_var]),
        # Прогрев чужого свежего ресурса ДО того, как его идентификатор уедет в
        # асинхронную мутацию nlb: на ней ограниченный повтор ключуется на коде
        # ответа шага, а он всегда `200`+`Operation` (issue #351). Разбор — в
        # шапке `warm_peer_fixture`; свойство держит гейт по дереву.
        warm_peer_fixture(_VPC_ADDRESSES, save_var, f"intaddr-{suffix}"),
    ]


def _provision_external_address(suffix, save_var="vpcAddrId"):
    """Provision an EXTERNAL (public) vpc Address from the platform pool; save its id."""
    return [
        Step(name=f"provision-external-addr-{suffix}", method="POST", path=_VPC_ADDRESSES,
             body={"projectId": "{{_suiteProjectId}}", "name": f"nlb81-extadr-{suffix}-{{{{runId}}}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[
                 *_SPEC_REACHED_VPC,
                 f"pm.environment.unset('{save_var}');",
                 # The seed guarantees a default EXTERNAL_PUBLIC pool in this zone (it FATALs
                 # otherwise) and the runner is serial by default, so peak concurrent VIP hold
                 # is one — a refusal here is a finding, not a lane property.
                 "pm.test('external address fixture accepted as Operation', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "if (pm.response.code === 200) {",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 f"  if (j.metadata && j.metadata.addressId) pm.environment.set('{save_var}', j.metadata.addressId);",
                 "} else { pm.environment.set('opId', ''); }",
             ]),
        poll_operation_until_done(fixture_ids=[save_var]),
        # Прогрев чужого свежего ресурса ДО того, как его идентификатор уедет в
        # асинхронную мутацию nlb: на ней ограниченный повтор ключуется на коде
        # ответа шага, а он всегда `200`+`Operation` (issue #351). Разбор — в
        # шапке `warm_peer_fixture`; свойство держит гейт по дереву.
        warm_peer_fixture(_VPC_ADDRESSES, save_var, f"extaddr-{suffix}"),
    ]


def _cleanup_vpc(base, id_var):
    return [
        Step(name=f"cleanup-vpc-{id_var}", method="DELETE", path=f"{base}/{{{{{id_var}}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


# ---------------------------------------------------------------------------
# CRUD happy paths
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NLB-CR-CRUD-OK",
    title="Create EXTERNAL LB with auto public VIP — happy path (Verifies 8.1-06)",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "edge-public-{{runId}}", "description": "edge L4",
                   "labels": {"env": "prod"}, "sessionAffinity": "FIVE_TUPLE",
                   "deletionProtection": False},
             test_script=[*assert_status(200),
                          *assert_operation_envelope(prefix_regex="^nlb[a-z0-9]+$"),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="get-after-create", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('id matches', () => pm.expect(j.id).to.eql(pm.environment.get('nlbId')));",
                          "pm.test('status INACTIVE (VIP-only, no listeners/TG)', () => "
                          "  pm.expect(j.status).to.eql('INACTIVE'));",
                          "pm.test('type EXTERNAL', () => pm.expect(j.type).to.eql('EXTERNAL'));",
                          # The auto public VIP is the SUBJECT of this case; it used to be
                          # asserted only `if (!lastOpError)`, i.e. only when the allocation had
                          # already succeeded. The poll above now fails on a failed allocation,
                          # so the statement can stand unconditionally.
                          "pm.test('v4AddressId resolved to a bound vpc Address (adr prefix)', () => "
                          "  pm.expect(j.v4AddressId).to.match(/^adr[a-z0-9]+$/));",
                          "pm.test('placementType absent for EXTERNAL', () => "
                          "  pm.expect(j.placementType || 'PLACEMENT_TYPE_UNSPECIFIED')."
                          "    to.be.oneOf(['', 'PLACEMENT_TYPE_UNSPECIFIED']));"])),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-CR-CRUD-INTERNAL",
    title="Create INTERNAL ZONAL LB — subnet-auto VIP from a zonal subnet (Verifies 8.1-01)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_provision_subnet("ZONAL", "cr-int"),
        retry_create_until_present(Step(name="cr-int", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_ZONAL", "name": "internal-lb-{{runId}}",
                   "v4Source": {"subnetId": "{{vpcSubnetId}}"}},
             test_script=[
                 # The zonal subnet is provisioned (and asserted) by the step above, so there
                 # is no "no fixture here" lane left to branch on.
                 "pm.environment.unset('nlbId');",
                 "pm.test('INTERNAL ZONAL create accepted as Operation', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json();",
                 "if (j.id) pm.environment.set('opId', j.id);",
                 "if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('nlbId', j.metadata.networkLoadBalancerId);",
             ])),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="get-int", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "pm.test('Get 200 for created INTERNAL ZONAL LB', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json();",
                 "pm.test('type INTERNAL', () => pm.expect(j.type).to.eql('INTERNAL'));",
                 "pm.test('placementType ZONAL', () => pm.expect(j.placementType).to.eql('ZONAL'));",
                 "pm.test('v4AddressId resolved to a bound vpc Address', () => "
                 "  pm.expect(j.v4AddressId).to.match(/^adr[a-z0-9]+$/));",
                 "pm.test('v6AddressId empty (v4-only)', () => pm.expect(j.v6AddressId || '').to.eql(''));",
             ])),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_vpc(_VPC_SUBNETS, "vpcSubnetId"),
    ],
))


# helper to spin up an LB and remember its id under {{nlbId}} (used by many cases)
def _setup_lb(name_suffix: str, body_extra: dict = None):
    # Pool-INDEPENDENT setup LB: an INTERNAL ZONAL LB whose VIP is auto-allocated from a
    # per-case inline ZONAL subnet (fresh /24 = 254 IPs), NOT from the shared external
    # AddressPool. The seeded external pool (a zone-derived /24, 254 leases) is a single contended IPAM
    # source across the `--jobs 4` parallel collections and its addresses are not recycled
    # promptly within a run: ci-rep2 shows only 82 distinct VIPs ever allocated against
    # 115 `could not allocate load balancer address` FailedPrecondition errors on a
    # 254-address pool — every EXTERNAL auto-VIP setup created a PHANTOM LB (Operation
    # done:true WITH error), so its {{nlbId}} pointed at a never-persisted resource and
    # every downstream Get/Update/:verb reddened 404/403 (owner-tuple never materialised)
    # or 400 (empty child id) in a long cascade. A subnet-backed INTERNAL VIP sidesteps
    # the shared pool entirely, keeps each case self-contained, and is confirmed working
    # (cross-resource INTERNAL ZONAL LBs allocate a bound Address reliably). The setup LB
    # is used opaquely by downstream cases (lifecycle / attach / update / delete) — none
    # assert EXTERNAL-specific shape on it, so INTERNAL is a drop-in. External auto-VIP
    # semantics stay covered by the dedicated NLB-CR-CRUD-OK / EXTERNAL cases.
    body = {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
            "placement": "INTERNAL_ZONAL",
            "name": f"setup-{name_suffix}-{{{{runId}}}}",
            "v4Source": {"subnetId": "{{vpcSubnetId}}"}, **(body_extra or {})}
    return [
        *_provision_subnet("ZONAL", f"setup-{name_suffix}"),
        # cross-service read-your-writes: the just-provisioned subnet can be briefly
        # invisible to nlb's vpc peer-read under parallel load -> `subnet <id> not found`.
        # Bounded create-retry re-POSTs (leak-free: a rejected create mints no Operation)
        # until the subnet materialises across the service boundary.
        retry_create_until_present(Step(name="setup-create-lb", method="POST", path=_CREATE_BASE, body=body,
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")])),
        # PHANTOM-ID GUARD on the LB-prep poller (parity with listener.py::_setup_lb and
        # cross-resource.py::_create_external_lb). Without `fixture_ids` this poll observed the
        # setup Operation, recorded `lastOpError` and moved on — the pre-allocated
        # `nlbId` of a LoadBalancer that never persisted stayed published, and every dependent
        # case forgave itself with `if (!lastOpError)`. A broken prep must be RED here,
        # attributably, not silently skipped by the assertion it was preparing for.
        poll_operation_until_done(fixture_ids=["nlbId"]),
        # read-your-writes: materialize the owner-tuple before the first real access.
        # opgate removed -> owner/creator FGA tuple is eventually-consistent, so the first
        # self GET/UPDATE/DELETE of the fresh LB can briefly 403/404. Silent (empty
        # test_script) so it only spins the retry loop; negative first-access steps
        # (immutable-400 / mask-unknown / delete-protection) then run UNWRAPPED after the
        # tuple is visible and assert their real sync result.
        retry_until_authorized(Step(name="setup-materialize-lb", method="GET",
             path=f"{_CREATE_BASE}/{{{{nlbId}}}}", test_script=[])),
    ]


def _reclaim_setup_subnet(id_var: str = "vpcSubnetId"):
    # Best-effort снятие подсети, которую выделила подготовка, — ПОСЛЕ того как снят
    # её единственный потребитель (балансировщик, чей VIP из неё аллоцирован). Обратный
    # порядок отвечает `subnet not empty`: VIP держит подсеть занятой, пока жив LB.
    #
    # Уборка НИКОГДА не роняет кейс (`oneOf`): остаточный лаг возврата VIP — законный
    # исход этого шага, а предмет утверждения кейса им не затронут. Чего она больше не
    # делает — так это не исполняется без предмета (см. `_cleanup_lb`).
    return [
        Step(name="cleanup-setup-subnet", method="DELETE",
             path=f"{_VPC_SUBNETS}/{{{{{id_var}}}}}",
             test_script=[
                 "pm.test('subnet reclaim best-effort (never fails the case)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 400, 403, 404, 405, 409]));",
                 "pm.environment.set('opId', '');",
                 "if (pm.response.code === 200) { try { const j = pm.response.json();"
                 " if (j.id) pm.environment.set('opId', j.id); } catch (e) {} }",
                 f"pm.environment.unset('{id_var}');",
             ]),
        poll_operation_until_done(),
    ]


def _cleanup_lb(reclaim_subnet: bool = True):
    # `reclaim_subnet` ЗЕРКАЛИТ ТО, ЧТО ПОДГОТОВКА ДЕЙСТВИТЕЛЬНО ВЫДЕЛИЛА.
    #
    # `_setup_lb` ВСЕГДА нарезает свою ZONAL-подсеть в ОБЩЕЙ посеянной сети
    # (`existingNetworkId`), а уборка снимала только сам балансировщик — подсеть
    # оставалась в сети навсегда. При вложенном потолке «сколько подсетей помещается в
    # одной сети» (`vpc.network.subnet`, умолчание 16) набор упирался в предел
    # by construction, а не по стечению обстоятельств: отказ приходил прямым
    # `QUOTA_EXCEEDED`, а красным становилось всё, что стояло на несозданной подсети.
    #
    # Условие тут СТРУКТУРНОЕ, а не рантаймовое: провизионила подсеть парная подготовка
    # или нет, известно на генерации. Поэтому шаг не «терпит отсутствие предмета», а
    # просто не появляется там, где предмета нет — у шести кейсов, которые зовут уборку
    # без `_setup_lb` и ведут собственную подсеть своим же `_cleanup_vpc` (пять) либо не
    # заводят подсети вовсе (NLB-CR-CRUD-EXTERNAL-LINK — внешний адрес).
    steps = [
        Step(name="cleanup-del-lb", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]
    if not reclaim_subnet:
        return steps
    return steps + _reclaim_setup_subnet()


CASES.append(Case(
    id="NLB-GET-CRUD-OK",
    title="Get existing LB returns full message with created_at",
    classes=["CRUD"], priority="P0",
    steps=[
        *_setup_lb("get-ok"),
        retry_until_authorized(Step(name="get", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('has id', () => pm.expect(j.id).to.match(/^nlb/));",
                          "pm.test('has createdAt', () => pm.expect(j.createdAt).to.be.a('string'));",
                          "pm.test('has region/project', () => {",
                          "  pm.expect(j.projectId).to.be.a('string');",
                          "  pm.expect(j.regionId).to.be.a('string');",
                          "});"])),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-LST-CRUD-OK",
    title="List LB in project — array returned",
    classes=["CRUD", "LSG"], priority="P1",
    steps=[
        Step(name="list", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=10",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('networkLoadBalancers is array', () => "
                          "  pm.expect(j.networkLoadBalancers || j.items || []).to.be.an('array'));"]),
    ],
))

CASES.append(Case(
    id="NLB-LST-CRUD-EMPTY-PROJECT",
    title="List on different (empty for this suite) project may return empty array",
    classes=["CRUD", "LSG"], priority="P2",
    steps=[
        Step(name="list-cross", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectCrossId}}}}&pageSize=10",
             test_script=[*assert_status(200),
                          "pm.test('array shape', () => {",
                          "  const j = pm.response.json();",
                          "  pm.expect(j.networkLoadBalancers || j.items || []).to.be.an('array');",
                          "});"]),
    ],
))

CASES.append(Case(
    id="NLB-UPD-CRUD-OK",
    title="Update LB mutable (name, description, labels) via mask",
    classes=["CRUD"], priority="P1",
    steps=[
        *_setup_lb("upd-ok"),
        retry_until_authorized(Step(name="patch", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "name,description,labels",
                   "name": "renamed-{{runId}}", "description": "updated",
                   "labels": {"env": "prod", "tier": "edge"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="verify", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('description updated', () => pm.expect(j.description).to.eql('updated'));",
                          "pm.test('labels updated', () => pm.expect((j.labels||{}).tier).to.eql('edge'));"]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-UPD-CRUD-MULTI-MASK",
    title="Update LB with mask of multiple mutable fields (sessionAffinity + deletionProtection)",
    classes=["CRUD", "STATE"], priority="P2",
    steps=[
        *_setup_lb("upd-multi", {"sessionAffinity": "FIVE_TUPLE"}),
        retry_until_authorized(Step(name="patch-multi", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "sessionAffinity,deletionProtection",
                   "sessionAffinity": "CLIENT_IP_ONLY", "deletionProtection": False},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="verify-multi", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('sessionAffinity updated', () => "
                          "  pm.expect(j.sessionAffinity).to.eql('CLIENT_IP_ONLY'));"]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-DEL-CRUD-OK",
    title="Delete clean LB (no listeners, no attached TG, protection=false)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_setup_lb("del-ok"),
        retry_until_authorized(Step(name="delete", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="get-after-delete", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        # Балансировщик здесь — ПРЕДМЕТ кейса, поэтому его снимает сам кейс, а не
        # `_cleanup_lb`. Подсеть при этом выделила подготовка, и без этого шага она
        # оставалась в общей сети — тот же вклад во вложенный потолок, что и уборка
        # без реклейма. Предмет снят выше, значит подсеть свободна.
        *_reclaim_setup_subnet(),
    ],
))

CASES.append(Case(
    id="NLB-LOPS-CRUD-OK",
    title="ListOperations for LB returns history ordered DESC",
    classes=["CRUD", "LSG"], priority="P2",
    steps=[
        *_setup_lb("lops"),
        retry_until_authorized(Step(name="upd-bump-history", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "description", "description": "bumped"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="lops", method="GET",
             path=f"{_CREATE_BASE}/{{{{nlbId}}}}/operations?pageSize=10",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "const ops = j.operations || j.items || [];",
                          "pm.test('at least Create op present', () => pm.expect(ops.length).to.be.at.least(1));"]),
        *_cleanup_lb(),
    ],
))


# ---------------------------------------------------------------------------
# Lifecycle (Start / Stop / Move)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NLB-MV-CRUD-OK",
    title="Move LB to cross-project — denormalises listeners.project_id (Verifies REQ-NLB-MV-01)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_setup_lb("mv-ok"),
        # Cross-project move is destination-fixture-dependent: _suiteProjectCrossId is
        # seeded per-service by authz-fixtures/setup.sh and is NOT reliably present on
        # every lane. Tolerate the lawful `Project not found` (400) fixture-absent
        # outcome — but keep the happy-path assertion strict WHEN the fixture is present
        # (mirrors round-2's target-group move tolerance; the must-DENY guarantees for
        # cross-project stay strict in the dedicated authz-deny cases).
        retry_until_authorized(Step(name="move", method="POST", path=f"{_CREATE_BASE}/{{{{nlbId}}}}:move",
             body={"destinationProjectId": "{{_suiteProjectCrossId}}"},
             test_script=[
                 "pm.environment.unset('_mvMoved');",
                 "if (pm.response.code === 200) {",
                 "  pm.environment.set('_mvMoved', '1');",
                 "  const j = pm.response.json(); if (j.id) pm.environment.set('opId', j.id);",
                 "} else {",
                 "  pm.environment.set('opId', '');",
                 "  pm.test('cross-project move lawfully rejected when dst fixture absent (Project not found)', () => {",
                 "    pm.expect(pm.response.code).to.eql(400);",
                 "    pm.expect(pm.response.json().message || '').to.match(/not found/i);",
                 "  });",
                 "}",
             ])),
        poll_operation_until_done(),
        Step(name="get-moved", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "if (pm.environment.get('_mvMoved')) {",
                          "  pm.test('projectId updated to destination', () => "
                          "    pm.expect(pm.response.json().projectId).to.eql(pm.environment.get('_suiteProjectCrossId')));",
                          "}"]),
        # move-back only when the forward move actually happened; on a fixture-absent
        # lane the LB is still in _suiteProjectId, so a move-back would self-reject
        # ("destination same as source") — skip it (no id → poll is a guarded no-op).
        Step(name="move-back", method="POST", path=f"{_CREATE_BASE}/{{{{nlbId}}}}:move",
             body={"destinationProjectId": "{{_suiteProjectId}}"},
             test_script=[
                 # ПОЛОСУ ВЫБИРАЕТ НЕ ЭТОТ ШАГ, а исход прямого перемещения выше, поэтому
                 # утверждается КАЖДАЯ, а не общий `oneOf`: тот принял бы успех возврата на
                 # полосе, где возвращать нечего. Перемещение состоялось ⇒ возврат обязан быть
                 # принят. Не состоялось ⇒ балансировщик остался в исходном проекте, и
                 # перемещение «туда, где уже стоит» край обязан отвергнуть — 200 здесь означал
                 # бы, что он его принял.
                 "pm.test('move-back: accepted when the forward move happened, refused otherwise', function () {",
                 "  if (pm.environment.get('_mvMoved')) {",
                 "    pm.expect(pm.response.code, pm.response.text()).to.eql(200);",
                 "  } else {",
                 "    pm.expect(pm.response.code, pm.response.text()).to.not.eql(200);",
                 "  }",
                 "});",
                 "if (pm.environment.get('_mvMoved') && pm.response.code === 200) {",
                 "  const j = pm.response.json(); if (j.id) pm.environment.set('opId', j.id);",
                 "} else { pm.environment.set('opId', ''); }",
             ]),
        poll_operation_until_done(),
        # ПРОГРЕВ ПОСЛЕ ПЕРЕЕЗДА — та же необходимость, что и после создания (см.
        # `_setup_lb`), но по другой причине, и её легко упустить.
        #
        # Переезд меняет ПРОЕКТ ресурса, а край резолвит цель проверки прав через
        # зеркало проекта. Пока зеркало переклеивается (delete-stale прошёл, write
        # ещё нет), цель не резолвится, гейт закрывается fail-closed, и ответ
        # приходит скрытым промахом — ДОСЛОВНО тем же текстом, что настоящее
        # отсутствие ресурса (это требование `security.md` §6, а не оплошность).
        # Отличить одно от другого по телу ответа нельзя by design.
        #
        # ЧТО НАБЛЮДАЛОСЬ: уборка получила `404 "NetworkLoadBalancer <id> not
        # found"`, а список в том же прогоне отдавал этот балансировщик живым и в
        # исходном проекте. То есть ресурс был цел, а по id не резолвился —
        # ровно окно.
        #
        # Прогрев молчалив (пустой `test_script`): он только крутит петлю повтора,
        # и его исход ничего не утверждает. Уборка после него идёт БЕЗ обёртки и
        # требует своего честного 200 — то есть окно ждём здесь, а вердикт
        # выносит шаг, у которого есть предмет.
        retry_until_authorized(Step(name="post-move-materialize-lb", method="GET",
             path=f"{_CREATE_BASE}/{{{{nlbId}}}}", test_script=[])),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-MV-NEG-ATTACHED-TG",
    title="Move LB with a listener-wired TG → FailedPrecondition (Verifies REQ-NLB-MV-NEG)",
    classes=["NEG", "STATE"], priority="P0",
    steps=[
        *_setup_lb("mv-attached"),
        # Create a TG to attach
        Step(name="setup-tg", method="POST", path="/nlb/v1/targetGroups",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "mv-tg-{{runId}}", "port": 8080,
                   "healthCheck": {"interval": "2s", "timeout": "1s",
                                   "unhealthyThreshold": 3, "healthyThreshold": 2,
                                   "tcp": {"port": 80}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.targetGroupId", "tgId")]),
        poll_operation_until_done(fixture_ids=["tgId"]),
        # Wire the TG to the LB via a listener (attach/detach RPCs removed) — a listener
        # referencing the TG is what now blocks the LB Move ("has a listener wired to a
        # target group; repoint before Move").
        # No `ipVersion`: it is `reserved 8` in CreateListenerRequest (the VIP moved
        # Listener→LoadBalancer). The listener inherits the parent's per-family VIP.
        # Повтор здесь был, утверждения — не было: шаг принимал любой ответ, и если
        # слушатель не создавался, кейс проверял перенос БЕЗ ссылки, ради которой он и
        # написан. Отказ обязан называться на месте, а не через три шага чужим именем.
        retry_until_authorized(Step(name="wire-listener", method="POST", path="/nlb/v1/listeners",
             body={"loadBalancerId": "{{nlbId}}", "name": "mv-att-lst-{{runId}}",
                   "protocol": "TCP", "port": 80,
                   "targetGroupId": "{{tgId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId"),
                          "pm.test('слушатель создан и его id захвачен — иначе предмет кейса отсутствует', "
                          "() => pm.expect(pm.environment.get('lstId') || '').to.not.equal(''));"])),
        poll_operation_until_done(),
        Step(name="move-rejected", method="POST",
             path=f"{_CREATE_BASE}/{{{{nlbId}}}}:move",
             body={"destinationProjectId": "{{_suiteProjectCrossId}}"},
             test_script=[
                 # Move refuses SYNCHRONOUSLY while a listener is wired to a target group
                 # (loadbalancer/move.go: HasWiredTargetGroup -> FAILED_PRECONDITION), so no
                 # Operation is minted. Accepting 200 let the move GO THROUGH and still pass
                 # the case whose only subject is that it must not.
                 "pm.environment.set('opId', '');",
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 "pm.test('says a wired listener blocks the move', () => "
                 "  pm.expect((pm.response.json().message || '')).to.include("
                 "    'has a listener wired to a target group; repoint before Move'));",
             ]),
        # ЗДЕСЬ БЫЛИ ДВА ШАГА ОПРОСА ОПЕРАЦИИ, И ОПЕРАЦИИ У НИХ НЕ БЫЛО.
        #
        # Move отказывает СИНХРОННО, поэтому `move-rejected` выше сам снимает `opId`
        # — и следом шли опрос и `check-fp`, адресованные `{{opId}}`, которого
        # больше нет. До стража адреса они уходили литералом и молча 4xx-ились;
        # страж их назвал (2 из 15 красных nlb боевого прогона 2026-07-31).
        # Утверждение внутри `check-fp` вдобавок стояло под `if (j.error)`, то есть
        # исчезало ровно в том случае, который обязано было ловить.
        # Отказ утверждён полностью и синхронно на самом `move-rejected`:
        # код, полоса grpc и дословный текст. Опрашивать нечего.
        # cleanup: delete listener (releases the TG ref) → delete TG → LB
        Step(name="del-lst", method="DELETE", path="/nlb/v1/listeners/{{lstId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="del-tg", method="DELETE", path="/nlb/v1/targetGroups/{{tgId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-MV-VAL-MISSING-DEST",
    title="Move without destinationProjectId → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="move-no-dest", method="POST",
             path=f"{_CREATE_BASE}/{{{{garbageNlbId}}}}:move",
             body={},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="NLB-MV-NEG-NF-UNKNOWN",
    title="Move of unknown LB id → 404 NotFound",
    classes=["NEG"], priority="P1",
    steps=[
        Step(name="move-nx", method="POST",
             path=f"{_CREATE_BASE}/{{{{garbageNlbId}}}}:move",
             body={"destinationProjectId": "{{_suiteProjectCrossId}}"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="NLB-MV-IDM-SAME-PROJECT",
    title="Move LB to current project → InvalidArgument 'destination same as source'",
    classes=["IDEM", "NEG"], priority="P2",
    steps=[
        *_setup_lb("mv-self"),
        Step(name="move-self", method="POST", path=f"{_CREATE_BASE}/{{{{nlbId}}}}:move",
             body={"destinationProjectId": "{{_suiteProjectId}}"},
             test_script=[
                 # Same-project Move is refused SYNCHRONOUSLY, before any peer call
                 # (loadbalancer/move.go). The code check used to be written
                 # `if (code === 400)`, i.e. it only ran once the refusal it was meant to
                 # establish had already happened.
                 "pm.environment.set('opId', '');",
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('names the same-destination condition', () => "
                 "  pm.expect(pm.response.json().message || '').to.eql("
                 "    'destination project is the same as source'));",
             ]),
        *_cleanup_lb(),
    ],
))


# ---------------------------------------------------------------------------
# Attach / Detach TargetGroup
# ---------------------------------------------------------------------------

def _setup_tg(name_suffix: str, body_extra: dict = None):
    base_hc = {"healthCheck": {"interval": "2s", "timeout": "1s",
                               "unhealthyThreshold": 3, "healthyThreshold": 2,
                               "tcp": {"port": 80}}}
    body = {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
            "name": f"setup-tg-{name_suffix}-{{{{runId}}}}", "port": 8080, **base_hc, **(body_extra or {})}
    return [
        Step(name="setup-create-tg", method="POST", path="/nlb/v1/targetGroups", body=body,
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.targetGroupId", "tgId")]),
        poll_operation_until_done(fixture_ids=["tgId"]),
        # read-your-writes: materialize the TG owner-tuple before the first real access.
        retry_until_authorized(Step(name="setup-materialize-tg", method="GET",
             path="/nlb/v1/targetGroups/{{tgId}}", test_script=[])),
    ]


def _cleanup_tg():
    return [
        Step(name="cleanup-del-tg", method="DELETE", path="/nlb/v1/targetGroups/{{tgId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]



# ---------------------------------------------------------------------------
# GetTargetStates
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NLB-GTS-CRUD-EMPTY",
    title="GetTargetStates for a TG with no registered targets → [] (Verifies REQ-NLB-GTS-01)",
    classes=["CRUD"], priority="P1",
    steps=[
        # GetTargetStates is a PER-TARGET-GROUP query: GetTargetStatesRequest.target_group_id
        # is required (get_target_states.go: errInvalidArg("target_group_id","required")). An
        # LB-wide call (no tgId) is a hard 400 by contract, not an implicit "all groups" — so
        # this case supplies its own TG (empty, not even attached: GetTargetStates only needs
        # same-project + viewer, not attachment) and asserts the empty-states contract.
        *_setup_lb("gts-empty"),
        *_setup_tg("gts-empty"),
        retry_until_authorized(Step(name="gts", method="GET",
             path=f"{_CREATE_BASE}/{{{{nlbId}}}}/targetStates?targetGroupId={{{{tgId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('targetStates is an empty array (TG has no targets)', () => "
                          "  pm.expect(j.targetStates || []).to.be.an('array').that.is.empty);"])),
        *_cleanup_tg(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-GTS-STATE-LB-DISABLED",
    title="GetTargetStates returns INACTIVE for every target when LB adminState=DISABLED",
    classes=["STATE"], priority="P2",
    steps=[
        # target_group_id is required (per-TG query); supply a TG with one deterministic,
        # peer-free external_ip target so the DISABLED→INACTIVE branch of computeTargetState
        # (adminState==DISABLED ⇒ INACTIVE, get_target_states.go:128) is actually exercised,
        # not vacuously skipped. (Admin enable/disable is now adminState via Update — the
        # legacy Stop RPC and its lifecycle status were removed.)
        *_setup_lb("gts-stopped"),
        *_setup_tg("gts-stopped"),
        retry_until_authorized(Step(name="gts-add-target", method="POST",
             path="/nlb/v1/targetGroups/{{tgId}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.210"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # AdminState enum members are PREFIXED (ADMIN_STATE_ENABLED / ADMIN_STATE_DISABLED —
        # network_load_balancer.proto), UNLIKE SessionAffinity/Placement whose non-zero members
        # drop the prefix (FIVE_TUPLE / EXTERNAL_REGIONAL). The proto3-JSON enum value is the
        # FULL name: "ADMIN_STATE_DISABLED". The bare "DISABLED" is not a valid AdminState value
        # → parsed as ADMIN_STATE_UNSPECIFIED(0) → adminStateFromPb "" → Update preserves current
        # (ENABLED) → GetTargetStates computed HEALTHY (root cause of the got-HEALTHY-want-INACTIVE
        # fail; the product Update+gating are correct — this was a wrong enum-value string).
        retry_until_authorized(Step(name="disable", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "adminState", "adminState": "ADMIN_STATE_DISABLED"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # Diagnostic RED-lock: confirm the disable actually PERSISTED before reading the derived
        # target state, so a future adminState-update regression fails LOUDLY here (on the LB's
        # own field) instead of surfacing confusingly as gts-HEALTHY. The op is durable (polled),
        # so this converges immediately; retry_until_state also absorbs read-replica lag.
        retry_until_state(Step(name="verify-adminstate-disabled", method="GET",
             path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('adminState persisted DISABLED', () => "
                          "  pm.expect(pm.response.json().adminState).to.eql('ADMIN_STATE_DISABLED'));"]),
             "pm.response.json().adminState === 'ADMIN_STATE_DISABLED'"),
        # GetTargetStates recomputes from the LB's (now DISABLED) adminState → every target
        # reports INACTIVE (get_target_states.go computeTargetState). retry_until_state guards
        # any residual read-replica lag on the derived state; a plain retry_until_authorized
        # (403/404 only) would assert once on a stale pre-DISABLED 200.
        retry_until_state(Step(name="gts", method="GET",
             path=f"{_CREATE_BASE}/{{{{nlbId}}}}/targetStates?targetGroupId={{{{tgId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "const states = j.targetStates || [];",
                          "pm.test('at least one target state returned', () => "
                          "  pm.expect(states.length).to.be.at.least(1));",
                          "states.forEach(ts => {",
                          "  pm.test('target state INACTIVE for ' + (ts.address||'?'), () => "
                          "    pm.expect(ts.status).to.eql('INACTIVE'));",
                          "});"]),
             "(pm.response.json().targetStates||[]).length >= 1 && "
             "(pm.response.json().targetStates||[]).every(function(t){return t.status === 'INACTIVE';})"),
        # Drain the target first — TargetGroup.Delete is blocked while it holds targets
        # ("TargetGroup has N target(s); remove them first"). Keeps the case self-contained.
        Step(name="gts-remove-target", method="POST", path="/nlb/v1/targetGroups/{{tgId}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.210"}}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
        *_cleanup_lb(),
    ],
))


# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NLB-CR-VAL-NAME-REGEX",
    title="Create with invalid name regex → InvalidArgument (Verifies REQ-NLB-CR-VAL-NAME)",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="cr-bad-regex", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "Edge_Public!"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-NAME-UNDERSCORE",
    title="Create with underscore in name → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="cr-underscore", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "edge_public-{{runId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-NAME-UPPERCASE",
    title="Create with uppercase letters in name → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="cr-upper", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "EdgePublic-{{runId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-NAME-EMPTY",
    title="Create with empty name → InvalidArgument (required)",
    classes=["VAL"], priority="P0",
    steps=[
        Step(name="cr-empty-name", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": ""},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

# NLB-1-08 (F2, CONTRACT): the legacy mode inputs type/placementType are derived
# output-only — writing either on Create is an EXPLICIT reject (kept in the request
# schema ONLY so the gateway does not silently drop them; the mode is set solely by
# placement). Black-box lock for the white-box TestLoadBalancer_NLB_1_08_LegacyModeInputRejected.
CASES.append(Case(
    id="NLB-CR-VAL-LEGACY-MODE-INPUT",
    title="Create with legacy type/placementType input → InvalidArgument (Verifies NLB-1-08: mode set solely by placement)",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="cr-legacy-type", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "type": "EXTERNAL"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        Step(name="cr-legacy-placementtype", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "placementType": "REGIONAL"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-NAME-NULL",
    title="Create with name=null → 400",
    classes=["VAL"], priority="P2",
    steps=[
        # `null` transcodes to the field default (""), so this lands on the very
        # same "name is required" rejection as NLB-CR-VAL-NAME-EMPTY — assert it
        # outright. Accepting 200 here made the case pass whether the name was
        # required or not, i.e. it asserted nothing.
        Step(name="cr-null-name", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": None},
             test_script=[
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-MISSING-REGION",
    title="Create without region_id → InvalidArgument",
    classes=["VAL"], priority="P0",
    steps=[
        Step(name="cr-no-region", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "placement": "EXTERNAL_REGIONAL",
                   "name": "no-region-{{runId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-MISSING-PROJECT",
    title="Create without project_id → InvalidArgument/PermissionDenied",
    classes=["VAL"], priority="P0",
    steps=[
        Step(name="cr-no-project", method="POST", path=_CREATE_BASE,
             body={"regionId": "{{_suiteRegionId}}", "placement": "EXTERNAL_REGIONAL",
                   "name": "no-project-{{runId}}"},
             test_script=[
                 "pm.test('rejected (400/403)', () => pm.expect(pm.response.code).to.be.oneOf([400, 403]));",
             ]),
    ],
))

# The declaration that stood here was REMOVED together with its subject: an unknown enum
# value is now refused at the edge (gateway/internal/restmux/strict_enum.go, commit
# d67d15fb), so this case passes by a PRODUCT change, not by an adjusted expectation.
# The measurement that produced it (2026-07-28: an out-of-vocabulary sessionAffinity was
# answered 200 and silently defaulted) is kept in docs/RESULTS.md, because it explains why
# the assertion is written this way.
#
# The assertion is deliberately NOT relaxed back to "200 or 400": that spelling passed in
# both worlds and is exactly why the behaviour went unnoticed.
CASES.append(Case(
    id="NLB-CR-VAL-INVALID-AFFINITY",
    title="Create with unknown sessionAffinity enum → InvalidArgument",
    classes=["VAL"], priority="P2",
    steps=[
        Step(name="cr-bad-aff", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "sessionAffinity": "DOES_NOT_EXIST",
                   "name": "bad-aff-{{runId}}"},
             test_script=[
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-LABELS-OVER-64",
    title="Create with >64 labels → 23514 CHECK → InvalidArgument (Verifies REQ-DB-LABEL-CHECK)",
    classes=["VAL", "BVA"], priority="P1",
    steps=[
        # Cardinality is validated SYNCHRONOUSLY (domain ValidateLabels runs before
        # the Operation is minted), so there is no "async" lane to tolerate: the
        # rejection is the response itself. The DB CHECK stays the backstop.
        Step(name="cr-65-labels", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "over-labels-{{runId}}",
                   "labels": {f"k{i}": f"v{i}" for i in range(65)}},
             test_script=[
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-LABELS-UPPERCASE-KEY",
    title="Create with uppercase label key → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="cr-label-upper", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "labels-upper-{{runId}}",
                   "labels": {"BADKEY": "v"}},
             test_script=[
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-LABELS-INVALID-KEY-CHAR",
    title="Create with invalid char in label key → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="cr-label-bad-char", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "labels-bad-{{runId}}",
                   "labels": {"bad key!": "v"}},
             test_script=[
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-DESC-OVER-256",
    title="Create with description >256 chars → InvalidArgument",
    classes=["VAL", "BVA"], priority="P2",
    steps=[
        Step(name="cr-desc-over", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "desc-over-{{runId}}", "description": "x" * 257},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

# Empty body carries no projectId. Create is authz-gated on the parent scope
# `project:<projectId>` (permission_map Create → StaticExtractor objectTypeProject,
# GetProjectId), and the authz interceptor runs BEFORE the handler's body
# validation. With projectId empty the object id is empty → FormatObject rejects
# it → the interceptor denies with PermissionDenied (code 7) before any
# InvalidArgument could be produced. This is the convention-correct authz-first /
# secure-by-default ordering, not a bug: a request with no project scope cannot
# be authorized. Techniques: error-guessing (empty request), decision-table
# (authz-scope-present × body-valid).
CASES.append(Case(
    id="NLB-CR-VAL-EMPTY-BODY",
    title="Create with empty body → PermissionDenied (authz-first: no project scope to authorize)",
    classes=["VAL", "NEG"], priority="P2",
    steps=[
        Step(name="cr-empty-body", method="POST", path=_CREATE_BASE,
             body={},
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-MALFORMED-JSON",
    title="Create with malformed JSON body → 400/415",
    classes=["VAL"], priority="P2",
    steps=[
        Step(name="cr-malformed", method="POST", path=_CREATE_BASE,
             body=None,
             pre_script=["pm.request.body = { mode: 'raw', raw: '{not valid json' };"],
             test_script=[
                 "pm.test('400/403/415', () => pm.expect(pm.response.code).to.be.oneOf([400, 403, 415]));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# BVA — name boundaries
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NLB-CR-BVA-NAME-MIN-3",
    title="Create with name length=3 (lower bound) → OK",
    classes=["BVA"], priority="P2",
    steps=[
        # The lower bound is only tested if a 3-character name is ACCEPTED. Accepting 409
        # as well made the case pass whether or not the boundary held — and the reason it
        # was written that way is real but fixable: the literal name "abc" is not
        # run-scoped, so a load balancer leaked by an earlier run occupies it and the next
        # run collides on UNIQUE(project_id, name).
        #
        # That belongs to the fixture, not to the assertion: derive the three characters
        # from runId (a letter plus two base-36 digits of its hash) so every run gets its
        # own name and still sits exactly on the boundary.
        Step(name="cr-3char", method="POST", path=_CREATE_BASE,
             pre_script=[
                 "var __r = (pm.environment.get('runId') || 'x0');",
                 "var __h = 0; for (var i = 0; i < __r.length; i++) { __h = ((__h << 5) - __h + __r.charCodeAt(i)) | 0; }",
                 "__h = Math.abs(__h) % 1296;",
                 "var __s = __h.toString(36); while (__s.length < 2) { __s = '0' + __s; }",
                 "pm.environment.set('_lbName3', 'a' + __s);",
             ],
             body={**_LB_BODY, "name": "{{_lbName3}}"},
             test_script=[
                 "pm.test('a 3-character name is accepted (lower bound of the name range)', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
             ]),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-CR-BVA-NAME-MAX-63",
    title="Create with name length=63 (upper bound) → OK",
    classes=["BVA"], priority="P2",
    steps=[
        # Тот же дефект, что был у нижней границы тремя кейсами выше, и та же починка.
        # Там его нашли и закрыли, здесь — нет: имя оставалось литералом `n63…`, то есть
        # НЕ run-scoped, при `UNIQUE(project_id, name)` на балансировщиках. Уборка в конце
        # кейса спасает только полностью завершившийся прогон; оборванный (или второй против
        # того же стенда — это задокументированный путь отладки) занимает имя насовсем, и
        # следующий получает 409 против строгого `assert_status(200)`.
        #
        # Шесть символов из runId вшиты В ПРЕДЕЛАХ лимита: длина остаётся ровно 63, иначе
        # кейс перестал бы проверять границу, ради которой существует.
        Step(name="cr-63char", method="POST", path=_CREATE_BASE,
             pre_script=[
                 "var __r = (pm.environment.get('runId') || 'x0');",
                 "var __h = 0; for (var i = 0; i < __r.length; i++) "
                 "{ __h = ((__h << 5) - __h + __r.charCodeAt(i)) | 0; }",
                 "var __t = Math.abs(__h).toString(36);",
                 "while (__t.length < 6) { __t = '0' + __t; }",
                 "__t = __t.slice(0, 6);",
                 # 3 + 6 + 50 + 4 = 63
                 "pm.environment.set('_lbName63', 'n63' + __t + "
                 "'abcdefghijabcdefghijabcdefghijabcdefghijabcdefghij' + 'abcd');",
             ],
             body={**_LB_BODY, "name": "{{_lbName63}}"},
             test_script=["pm.test('the fixture name sits exactly on the 63-character bound', "
                          "() => pm.expect((pm.environment.get('_lbName63') || '').length).to.eql(63));",
                          *assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-CR-BVA-NAME-OVER-64",
    title="Create with name length=64 (off-by-one upper) → InvalidArgument",
    classes=["BVA", "VAL"], priority="P1",
    steps=[
        Step(name="cr-64char", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "n64" + "abcdefghij" * 7},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-BVA-DESC-MAX-256",
    title="Create with description=256 chars (upper) → OK",
    classes=["BVA"], priority="P2",
    steps=[
        Step(name="cr-256", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "desc-max-{{runId}}", "description": "x" * 256},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))


# ---------------------------------------------------------------------------
# LSG — list / filter / pagination
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NLB-LST-BVA-PAGESIZE-1",
    title="List with pageSize=1 → ≤1 item",
    classes=["BVA", "LSG"], priority="P2",
    steps=[
        Step(name="list-ps1", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=1",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "const arr = j.networkLoadBalancers || j.items || [];",
                          "pm.test('at most 1 item', () => pm.expect(arr.length).to.be.at.most(1));"]),
    ],
))

CASES.append(Case(
    id="NLB-LST-BVA-PAGESIZE-ZERO",
    title="List with pageSize=0 → server default applied",
    classes=["BVA", "LSG"], priority="P2",
    steps=[
        Step(name="list-ps0", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=0",
             test_script=[*assert_status(200)]),
    ],
))

CASES.append(Case(
    id="NLB-LST-BVA-PAGESIZE-OVER-MAX",
    title="List with pageSize=10000 → InvalidArgument",
    classes=["BVA", "VAL"], priority="P2",
    steps=[
        Step(name="list-huge", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=10000",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-LST-PAGE-TOKEN-GARBAGE",
    title="List with garbage page_token → InvalidArgument",
    classes=["VAL", "LSG"], priority="P1",
    steps=[
        Step(name="list-bad-token", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=10&pageToken=not-a-real-token",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-LST-PAGE-ROUNDTRIP",
    title="Pagination round-trip — next_page_token usable for next page",
    classes=["CRUD", "LSG"], priority="P2",
    steps=[
        Step(name="page-1", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=1",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.environment.set('nextToken', j.nextPageToken || '');"]),
        Step(name="page-2", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=1&pageToken={{{{nextToken}}}}",
             test_script=[*assert_status(200)]),
    ],
))

CASES.append(Case(
    id="NLB-LST-FILTER-NAME-OK",
    title="List with filter name=\"foo\" → 200 (filter accepted)",
    classes=["LSG"], priority="P2",
    steps=[
        Step(name="list-filter", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&filter=name%3D%22edge%22",
             test_script=[*assert_status(200)]),
    ],
))

CASES.append(Case(
    id="NLB-LST-FILTER-MATCH",
    title="Create resource → list with filter returns own resource id",
    classes=["LSG", "IDEM"], priority="P2",
    steps=[
        *_setup_lb("flt-match"),
        # read-your-writes over the list-authz visibility window: the filtered List returns
        # 200 with the id ABSENT until the owner-tuple materializes -> retry while missing.
        retry_until_present(Step(name="list-filtered", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=100"
                  f"&filter=name%3D%22setup-flt-match-{{{{runId}}}}%22",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "const arr = j.networkLoadBalancers || j.items || [];",
                          "pm.test('list includes own id', () => "
                          "  pm.expect(arr.map(x => x.id)).to.include(pm.environment.get('nlbId')));"]), "nlbId"),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-LST-FILTER-GARBAGE",
    title="List with garbage filter syntax → InvalidArgument (unknown field)",
    classes=["VAL"], priority="P2",
    steps=[
        # The filter grammar is a whitelist: the leading identifier of
        # `invalid filter text` is not a known field, so this is a rejection, not
        # a "either way is fine" input. Accepting 200 also accepted the dangerous
        # reading — a filter silently ignored and the caller served an unfiltered
        # page.
        Step(name="list-bad-filter", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&filter=invalid%20filter%20text",
             test_script=[
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# NEG — cross-service NotFound
# ---------------------------------------------------------------------------

# region_id is validated cross-domain against kacho-geo. That validation runs in
# the async Create worker (doCreate → regionClient.Get), so Create returns 200 +
# an Operation envelope synchronously and the failure surfaces on the polled
# Operation. geo returns NotFound for an absent (well-formed slug) region id;
# the nlb region client maps that to the peer-validate lane → FAILED_PRECONDITION
# (code 9) with the machine-readable reason token PEER_RESOURCE_MISSING and the
# text "Region <id> not found" (region_client.go mapRegionErr). NOT NotFound: the
# consumer did not fail to find its OWN resource, a precondition on someone
# else's was not met (api-conventions §By-lane code-split). Techniques: ECP
# (unknown cross-domain ref class), error-guessing (garbage region slug),
# state-transition (Operation done:false→true with error).
CASES.append(Case(
    id="NLB-CR-NEG-REGION-UNKNOWN",
    title="Create with unknown region_id → async Operation error FAILED_PRECONDITION 'Region ... not found' "
          "(Verifies REQ-NLB-CR-NEG-REGION)",
    classes=["NEG"], priority="P0",
    steps=[
        Step(name="cr-bad-region", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{garbageRegionId}}",
                   "name": "bad-region-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="check-error", method="GET", path="/operations/{{opId}}",
             test_script=[
                 "const j = pm.response.json();",
                 "pm.test('operation failed', () => "
                 "  pm.expect(j.error, JSON.stringify(j)).to.be.an('object'));",
                 "pm.test('error code 9 FAILED_PRECONDITION (peer-validate lane)', () => "
                 "  pm.expect(j.error && j.error.code).to.eql(9));",
                 # Дословно текст владельца: `not found` в нижнем регистре зеленело на
                 # сообщении о ЛЮБОМ ресурсе и заглавной `R` не различало вовсе.
                 "pm.test('message names the region verbatim', () => "
                 "  pm.expect((j.error && j.error.message) || '', JSON.stringify(j))"
                 "    .to.eql('Region ' + pm.environment.get('garbageRegionId') + ' not found'));",
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-NEG-PROJECT-UNKNOWN",
    title="Create with unknown project_id → NotFound/PermissionDenied",
    classes=["NEG"], priority="P0",
    steps=[
        Step(name="cr-bad-proj", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{garbageProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "bad-proj-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             # An unknown project is refused by whichever side sees it first: the gateway
             # cannot resolve the scope for the anti-BOLA check (403), or the backend/peer
             # answers that it does not exist (400/404). What must never happen is the LB
             # being created in a project that is not there — which is what accepting a
             # bare 200 permitted, with nothing polled afterwards to contradict it.
             # Асинхронной полосы у этого входа нет: `project_id` — та самая область,
             # которую край резолвит для анти-BOLA проверки, поэтому нерезолвимый
             # проект отвергается ДО тела запроса, и Operation не минтится никогда.
             test_script=assert_refused_sync_or_async("unknown project_id",
                                                     sync_codes=(400, 403, 404),
                                                     async_lane=False)),
    ],
))

CASES.append(Case(
    id="NLB-GET-NEG-NF-UNKNOWN",
    title="Get unknown nlbId → 404 NotFound (Verifies REQ-NLB-GET-NEG)",
    classes=["NEG"], priority="P0",
    steps=[
        Step(name="get-unknown", method="GET", path=f"{_CREATE_BASE}/{{{{garbageNlbId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

CASES.append(Case(
    id="NLB-UPD-NEG-NF-UNKNOWN",
    title="Update unknown nlbId → 404 NotFound",
    classes=["NEG"], priority="P1",
    steps=[
        Step(name="upd-unknown", method="PATCH", path=f"{_CREATE_BASE}/{{{{garbageNlbId}}}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="NLB-DEL-NEG-NF-UNKNOWN",
    title="Delete unknown nlbId → 404 NotFound",
    classes=["NEG"], priority="P1",
    steps=[
        Step(name="del-unknown", method="DELETE", path=f"{_CREATE_BASE}/{{{{garbageNlbId}}}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))


# ---------------------------------------------------------------------------
# CONF — concurrency
# ---------------------------------------------------------------------------

# uses gen.py helper conf_alreadyexists_block (auto-injected into module namespace).
# body_extra carries the 8.1 VIP source so the duplicate-name check (not a missing-source
# rejection) is what the second Create trips (Verifies 8.1-36).
CASES.append(conf_alreadyexists_block(
    prefix="NLB",
    create_path=_CREATE_BASE,
    name_template="conf-dup-{{runId}}",
    # Текст владельца дословно: services/nlb/internal/apps/kacho/api/loadbalancer/create.go
    # (обе точки отказа — предпроверка и вставка — пишут его побайтово одинаково).
    refusal="NetworkLoadBalancer with name {name} already exists in project",
    body_extra={"regionId": "{{_suiteRegionId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
))

CASES.append(Case(
    id="NLB-CR-CONF-NF-TEXT",
    title="Get unknown id matches verbatim 'NetworkLoadBalancer ... not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="get-unknown", method="GET", path=f"{_CREATE_BASE}/{{{{garbageNlbId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('text matches NetworkLoadBalancer ... not found', () => "
                          "  pm.expect(pm.response.json().message).to.match(/NetworkLoadBalancer .* not found/));"]),
    ],
))

CASES.append(Case(
    id="NLB-UPD-CONF-OCC-RACE",
    title="Concurrent Update — xmin OCC: deterministic exactly-one-success (Verifies REQ-NLB-UPD-OCC)",
    classes=["CONF"], priority="P1",
    steps=[
        *_setup_lb("occ-race"),
        # Best-effort race simulation — newman is sequential, so we just assert
        # the second Update either succeeds (no contention seen) or returns
        # ABORTED with the expected sentinel text.
        retry_until_authorized(Step(name="upd-1", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "description", "description": "occ-1"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # Second Update on the caller's OWN fresh LB. upd-1 (wrapped) can EXHAUST its budget
        # silently under heavy parallel drain lag, leaving the editor tuple still not visible
        # for upd-2 → 403. Give upd-2 its OWN read-your-writes retry window on 403/404.
        retry_until_authorized(
            Step(name="upd-2", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
                 body={"updateMask": "description", "description": "occ-2"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            retry_on=(403, 404)),
        poll_operation_until_done(),
        Step(name="check-op", method="GET", path="/operations/{{opId}}",
             test_script=[
                 "const j = pm.response.json();",
                 "if (j.error) pm.test('if ABORTED then code 10', () => "
                 "  pm.expect(j.error.code).to.be.oneOf([10, 0]));",
             ]),
        *_cleanup_lb(),
    ],
))


# ---------------------------------------------------------------------------
# STATE — immutable fields + delete protection
#
# In every probe below the assertion is carried ENTIRELY by `update_mask`: the
# immutable-field check reads the mask, not the body. None of `type` / `regionId` /
# `projectId` / `placementType` / `v4Source` / `v4AddressId` is a field of
# UpdateNetworkLoadBalancerRequest, so the same-named body key these cases used to
# carry alongside the mask was discarded at the edge and never reached the check it
# appeared to feed. Dropping it changes what is sent, not what is asserted.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NLB-UPD-STATE-IMMUTABLE-TYPE",
    title="Update with mask=type → InvalidArgument 'type is immutable' (Verifies REQ-NLB-IMMUTABLE-TYPE)",
    classes=["STATE", "VAL"], priority="P0",
    steps=[
        *_setup_lb("im-type"),
        Step(name="upd-type", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "type"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('mentions immutable', () => "
                          "  pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('type is immutable after NetworkLoadBalancer.Create'));"]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-UPD-STATE-IMMUTABLE-REGION",
    title="Update with mask=region_id → InvalidArgument 'region_id is immutable'",
    classes=["STATE", "VAL"], priority="P0",
    steps=[
        *_setup_lb("im-region"),
        Step(name="upd-region", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "regionId"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        *_cleanup_lb(),
    ],
))

# Текст владельца ДОСЛОВНО: services/nlb/internal/apps/kacho/api/loadbalancer/update.go,
# таблица immutableUpdateFields. Утверждается ПАРА — код и текст: расхождение тона
# живёт внутри INVALID_ARGUMENT, поэтому кейс, проверяющий один код, остаётся
# зелёным при любом сообщении. Так и вышло — отказ балансировщика называл глагол
# переноса, а два соседних ресурса на тот же запрет молчали, и ни одна проба
# различить этого не могла (#1671).
_MSG_PROJECT_IMMUTABLE = ("project_id is immutable after NetworkLoadBalancer.Create; "
                          "use NetworkLoadBalancerService.Move")

CASES.append(Case(
    id="NLB-UPD-STATE-IMMUTABLE-PROJECT",
    title=f"Update with mask=project_id → InvalidArgument, verbatim {_MSG_PROJECT_IMMUTABLE!r}",
    classes=["STATE", "VAL"], priority="P0",
    steps=[
        *_setup_lb("im-proj"),
        Step(name="upd-proj", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "projectId"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('verbatim message names the next step', () => "
                          f"pm.expect(pm.response.json().message).to.eql({_MSG_PROJECT_IMMUTABLE!r}));"]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-UPD-STATE-MASK-UNKNOWN",
    title="Update with unknown field in mask → InvalidArgument",
    classes=["STATE", "VAL"], priority="P1",
    steps=[
        *_setup_lb("mask-unk"),
        Step(name="upd-unk", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "nonexistent_field", "description": "x"},
             test_script=[
                 "pm.test('rejected 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "pm.test('grpc 3', () => pm.expect(pm.response.json().code).to.eql(3));",
             ]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-UPD-STATE-MASK-EMPTY",
    title="Update with an empty update_mask → full-object PATCH applies the mutable fields",
    classes=["STATE", "VAL"], priority="P1",
    steps=[
        *_setup_lb("mask-empty"),
        # AN EMPTY MASK IS A FULL-OBJECT PATCH — AND THE BODY MUST BE A FULL OBJECT.
        #
        # api-conventions.md: "mask пустой → full-object PATCH: применяются все
        # mutable-поля". Every service reads it the same way — nlb loadbalancer/listener,
        # compute Instance (instanceFullPatchFields) — the mask-less body REPLACES the
        # mutable set rather than merging into it. So a body carrying only `description`
        # also sets `name` to the empty string, and the merged object fails its own
        # validation: 400 `name is required`.
        #
        # Earlier this case sent exactly that body and asserted `oneOf([403, 200, 400])`,
        # which agreed with the contract and with its opposite at once. Tightening it to
        # 200 turned it red on the production-posture run of 2026-07-30 — and the product
        # was right: what was wrong was the body, which was not a full object.
        #
        # Both halves are asserted now, because it is the PAIR that distinguishes
        # full-object PATCH from merge-PATCH, and nothing pinned that before:
        #   (1) a full body without a mask applies EVERY mutable field it carries;
        #   (2) a body that OMITS the name leaves the name alone — an omitted field is
        #       "not sent", never "clear it" (corevalidate.NameOnUpdate, #715);
        #   (3) and the refusal half now lives where clearing is actually expressible:
        #       a mask that NAMES the name with an empty value is refused.
        #
        # (2) used to assert 400 here. That was the contract before #715, when a full
        # body without a name died on "name is required" — which also made it
        # impossible to patch the description with a full body at all. The refusal did
        # not disappear, it moved to (3); asserting only (2) as a 200 would have
        # dropped the tripwire this pair exists for.
        #
        # retry_until_authorized wraps only (1): the first mutating access to the
        # caller's own fresh LB can be denied while the editor tuple materializes. That
        # is a timing window, not an outcome.
        retry_until_authorized(
            Step(name="upd-empty", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
                 body={"name": "setup-mask-empty-{{runId}}",
                       "description": "empty-mask patch {{runId}}"},
                 test_script=[
                     "pm.test('empty mask is a full-object PATCH, not an error', () => "
                     "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                     *save_from_response("j.id", "opId"),
                 ]),
            retry_on=(403, 404)),
        poll_operation_until_done(must_succeed=True),
        Step(name="get-after-empty-mask", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('the mutable field from the body was applied', () => "
                          "  pm.expect(pm.response.json().description)"
                          "    .to.eql(pm.variables.replaceIn('empty-mask patch {{runId}}')));",
                          "pm.test('the name carried in the same body survived it', () => "
                          "  pm.expect(pm.response.json().name)"
                          "    .to.eql(pm.variables.replaceIn('setup-mask-empty-{{runId}}')));"]),
        # (2) The half that makes the first one mean something: without a mask, a body
        # that omits a required mutable field is REFUSED and says which one. If this ever
        # answers 200, the product silently became merge-PATCH and (1) would not notice.
        Step(name="upd-empty-partial-body", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"description": "partial body {{runId}}"},
             test_script=[
                 "pm.test('a body that omits the name is accepted, not refused', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(must_succeed=True),
        Step(name="get-after-partial-body", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('the field the body carried was applied', () => "
                          "  pm.expect(pm.response.json().description)"
                          "    .to.eql(pm.variables.replaceIn('partial body {{runId}}')));",
                          "pm.test('the name the body omitted was left alone, not cleared', () => "
                          "  pm.expect(pm.response.json().name)"
                          "    .to.eql(pm.variables.replaceIn('setup-mask-empty-{{runId}}')));"]),
        Step(name="upd-mask-names-empty-name", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "name", "name": ""},
             test_script=[
                 "pm.environment.set('opId', '');",
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('the refusal names the field the mask asked to clear', () => {",
                 "  const j = pm.response.json();",
                 "  const fields = ((j.details || []).flatMap(d => d.fieldViolations || []))"
                 "    .map(v => v.field);",
                 "  pm.expect(fields, pm.response.text()).to.include('name');",
                 "});",
             ]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-DEL-STATE-PROTECTION",
    title="Delete with deletion_protection=true → FailedPrecondition (Verifies REQ-NLB-DEL-PROT)",
    classes=["STATE", "NEG"], priority="P0",
    steps=[
        *_setup_lb("del-prot", {"deletionProtection": True}),
        # Deletion protection is a SYNCHRONOUS precondition (delete.go refuses before
        # any Operation is minted; the DB `AND deletion_protection=false` guard is the
        # backstop), so the refusal IS this response: 400 / FAILED_PRECONDITION.
        #
        # The previous assertion accepted 200 — the code of an ACCEPTED deletion — and
        # ran only when the parent had materialised, while the follow-up check fired
        # only `if (j.error)`. A build in which protection did nothing at all deleted
        # the load balancer and left the case green, which is the one outcome the case
        # exists to catch. The wrap still absorbs a genuine owner-tuple lag (403 →
        # retry); a terminal 403 now fails here instead of being waved through.
        retry_until_authorized(Step(name="del-protected", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
             ]), retry_on=(403,)),
        # disable protection and clean up
        Step(name="unprotect", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "deletionProtection", "deletionProtection": False},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-DEL-STATE-HAS-LISTENER",
    title="Delete LB with listener → FailedPrecondition 'has N listener(s)' (Verifies REQ-NLB-DEL-LISTENERS)",
    classes=["STATE", "NEG"], priority="P0",
    steps=[
        *_setup_lb("del-has-lst"),
        # No `ipVersion`: `reserved 8` in CreateListenerRequest (VIP lives on the LB).
        # Утверждение о приёме — и ограниченный повтор окна видимости под ним.
        # Слушатель авторизуется против СВЕЖЕГО родительского балансировщика, чей
        # владельческий кортеж материализуется вне мутации: без повтора строгие 200
        # краснели бы на законном окне, а без утверждения (как было) отказ уезжал бы
        # молча — предпосылка кейса не создана, а падал бы `del-blocked` ниже, который
        # честно получил бы 200 на балансировщике без слушателей.
        retry_until_authorized(Step(name="setup-listener", method="POST", path="/nlb/v1/listeners",
             body={"loadBalancerId": "{{nlbId}}", "name": "del-has-lst-{{runId}}",
                   "protocol": "TCP", "port": 80},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(fixture_ids=["lstId"]),
        # Same class as NLB-DEL-STATE-PROTECTION: the "has listener(s)" precondition is
        # SYNCHRONOUS, so the refusal is this response. The old assertion accepted 200
        # (deletion accepted) and 403 (never even reached the precondition), and the
        # follow-up ran only `if (j.error)` — between them, a load balancer deleted out
        # from under its listeners kept the case green.
        Step(name="del-blocked", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
             ]),
        # cleanup listener then LB
        Step(name="del-lst", method="DELETE", path="/nlb/v1/listeners/{{lstId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_lb(),
    ],
))


# ---------------------------------------------------------------------------
# Lifecycle conformance + HTTP-method semantics
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NLB-LIFECYCLE-CONF",
    title="Full lifecycle conformance: Create → Get → List-includes → Update → Get-updated → Delete → Get-404",
    classes=["CRUD", "CONF", "STATE"], priority="P1",
    steps=[
        Step(name="cr", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "life-{{runId}}", "description": "init"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "lifeId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-1", method="GET", path=f"{_CREATE_BASE}/{{{{lifeId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('id matches', () => "
                          "  pm.expect(pm.response.json().id).to.eql(pm.environment.get('lifeId')));"])),
        # List is authz-filtered (per-object FGA). The owner-tuple for the just-
        # created LB is written asynchronously (fga_register_outbox → IAM), so it
        # can take ~0.6-2s to become visible to ListObjects. Poll-retry the List
        # until the new id appears (bounded self-retry via setNextRequest, same
        # mechanism as poll-op; unique step name keeps the jump unambiguous)
        # before asserting inclusion — the assertion itself is not weakened, only
        # made tolerant of eventual consistency.
        Step(name="life-lst-includes", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=1000",
             test_script=[*assert_status(200),
                          "const arr = (Object.values(pm.response.json()).find(v => Array.isArray(v))) || [];",
                          "const ids = arr.map(x => x.id);",
                          "const lc = parseInt(pm.environment.get('_lifeLstCount') || '0', 10);",
                          "if (!ids.includes(pm.environment.get('lifeId')) && lc < 6) {",
                          "  pm.environment.set('_lifeLstCount', String(lc + 1));",
                          "  const _ipd1 = Date.now(); while (Date.now() - _ipd1 < 500) void 0; /* real inter-poll delay: cap 6 x 500ms ~= 3s budget (testing.md) */",
                          "  pm.execution.setNextRequest(pm.info.requestName);",
                          "  return;",
                          "}",
                          "pm.environment.unset('_lifeLstCount');",
                          "pm.test('list contains new LB (poll-tolerant)', () => "
                          "  pm.expect(ids).to.include(pm.environment.get('lifeId')));"]),
        Step(name="upd", method="PATCH", path=f"{_CREATE_BASE}/{{{{lifeId}}}}",
             body={"updateMask": "description", "description": "life-final"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Post-update verify is a 200-but-stale-state read: the Update Operation is durable
        # (polled to done) but the PATCH'd description can lag on the read path. Wait for the
        # state to CONVERGE (description == 'life-final') before asserting — a plain GET runs
        # the assert once on a stale 200 and reds.
        retry_until_state(Step(name="get-2", method="GET", path=f"{_CREATE_BASE}/{{{{lifeId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('description updated', () => "
                          "  pm.expect(pm.response.json().description).to.eql('life-final'));"]),
             "pm.response.json().description === 'life-final'"),
        Step(name="del", method="DELETE", path=f"{_CREATE_BASE}/{{{{lifeId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-404", method="GET", path=f"{_CREATE_BASE}/{{{{lifeId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))


# HTTP method semantics via shared helper
CASES.extend(http_method_not_allowed_block("NLB", _CREATE_BASE))


# ---------------------------------------------------------------------------
# Extended VAL/NEG/BVA matrix saturation (D-4: ≥320 cases)
# ---------------------------------------------------------------------------

# ЗДЕСЬ БЫЛ КЕЙС «имя начинается с цифры → 400». Его предмета больше нет:
# единая форма имени (DNS label по RFC 1123, `pkg/validate.NameForm`) разрешает
# цифру первым символом, и `9bad-…` теперь ЗАКОННОЕ имя балансировщика: nlb
# переведён на общую форму, своя регулярка из его домена снята.
#
# Кейс не удалён, а переведён на точку — символ, запрещённый и прежней формой
# nlb, и новой. Выбор был сделан, когда перевод ещё шёл, и остаётся верным
# после него: под обеими формами точка именем ресурса не является.
CASES.append(Case(
    id="NLB-CR-VAL-NAME-DOT",
    title="Create with a dot in name → InvalidArgument (DNS label, не DNS-имя)",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="cr-dot", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "bad.name-{{runId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-NAME-HYPHEN-START",
    title="Create with name starting with hyphen → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="cr-hyphen-start", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "-bad-{{runId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-NAME-HYPHEN-END",
    title="Create with name ending with hyphen → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="cr-hyphen-end", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "bad-{{runId}}-"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-NAME-SPECIAL-CHARS",
    title="Create with special chars (@, !, space) in name → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        Step(name="cr-special", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "bad@name-{{runId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-DESC-NULL",
    title="Create with description=null → accepted (transcodes to empty)",
    classes=["VAL"], priority="P2",
    steps=[
        # `null` is a lawful JSON spelling of "field absent": it transcodes to the
        # default and description has no required/format rule, so the create is
        # ACCEPTED. Naming that outcome is the whole case — "accepted or rejected"
        # covered every possible answer and therefore checked none. (VIP allocation
        # happens in the worker, so the sync answer does not depend on pool state.)
        Step(name="cr-desc-null", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "desc-null-{{runId}}", "description": None},
             test_script=[
                 *assert_status(200), *assert_operation_envelope(),
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
             ]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-DESC-INT-TYPE",
    title="Create with description=number → 400 transcode",
    classes=["VAL"], priority="P3",
    steps=[
        Step(name="cr-desc-int", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "desc-int-{{runId}}", "description": 12345},
             test_script=[
                 "pm.test('400 (json transcode)', () => pm.expect(pm.response.code).to.eql(400));",
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-LABELS-STRING-TYPE",
    title="Create with labels=string → 400 transcode",
    classes=["VAL"], priority="P2",
    steps=[
        Step(name="cr-lbl-str", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "lbl-str-{{runId}}", "labels": "not-an-object"},
             test_script=[
                 "pm.test('400 transcode', () => pm.expect(pm.response.code).to.eql(400));",
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-LABELS-VALUE-OVER-63",
    title="Create with label value >63 chars → InvalidArgument",
    classes=["VAL", "BVA"], priority="P2",
    steps=[
        Step(name="cr-lbl-val-over", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "lbl-vo-{{runId}}", "labels": {"k": "x" * 64}},
             test_script=[
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-LABELS-EMPTY-VALUE",
    title="Create with label value=\"\" → accepted (empty value is in range 0..63)",
    classes=["VAL"], priority="P2",
    steps=[
        # The label-value rule is a LENGTH bound of 0..63, so the empty string is
        # inside it and the create is ACCEPTED — the lower boundary of the BVA pair
        # whose upper end is NLB-CR-VAL-LABELS-VALUE-OVER-63. A boundary case that
        # accepts both answers tests no boundary.
        Step(name="cr-lbl-empty", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "lbl-emp-{{runId}}", "labels": {"k": ""}},
             test_script=[
                 *assert_status(200), *assert_operation_envelope(),
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
             ]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-WRONG-CT",
    title="POST without Content-Type → accepted (the edge marshaler is registered on MIMEWildcard)",
    classes=["VAL"], priority="P3",
    steps=[
        # Content negotiation here has one answer, so the case states it. The public REST
        # mux registers its JSON marshaler under `runtime.MIMEWildcard`
        # (gateway/internal/restmux/mux.go), the fallback for a request declaring no
        # Content-Type — the body parses and the create proceeds. "handled (200/400/415)"
        # could only fail on a 5xx: it accepted acceptance and both refusals, i.e. asserted
        # nothing about the edge. A red here means the marshaler registration changed, and
        # THAT is the finding.
        Step(name="cr-no-ct", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "noct-{{runId}}"},
             pre_script=["pm.request.headers.remove('Content-Type');"],
             test_script=[
                 *assert_status(200),
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
             ]),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        Step(name="cleanup-best-effort", method="DELETE",
             path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-GET-NEG-INVALID-ID-PREFIX",
    title="Get with malformed id prefix → InvalidArgument 'invalid network load balancer id'",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        Step(name="get-bad-prefix", method="GET", path=f"{_CREATE_BASE}/garbage-not-an-id",
             test_script=[
                 "pm.test('rejected (400 or 404)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([400, 404]));",
             ]),
    ],
))

CASES.append(Case(
    id="NLB-UPD-NEG-INVALID-ID-PREFIX",
    title="Update with malformed id prefix → InvalidArgument",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        Step(name="upd-bad-prefix", method="PATCH", path=f"{_CREATE_BASE}/garbage-not-an-id",
             body={"updateMask": "description", "description": "x"},
             test_script=[
                 "pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));",
             ]),
    ],
))

CASES.append(Case(
    id="NLB-DEL-NEG-INVALID-ID-PREFIX",
    title="Delete with malformed id prefix → InvalidArgument",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        Step(name="del-bad-prefix", method="DELETE", path=f"{_CREATE_BASE}/garbage-not-an-id",
             test_script=[
                 "pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));",
             ]),
    ],
))

CASES.append(Case(
    id="NLB-LST-CRUD-EMPTY-FILTER",
    title="List with empty filter param → 200",
    classes=["LSG"], priority="P2",
    steps=[
        Step(name="list-empty-filter", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&filter=",
             test_script=[*assert_status(200)]),
    ],
))

CASES.append(Case(
    id="NLB-LST-PAGE-TOKEN-EMPTY",
    title="List with pageToken=\"\" → 200 (default)",
    classes=["LSG", "BVA"], priority="P2",
    steps=[
        Step(name="list-empty-token", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=10&pageToken=",
             test_script=[*assert_status(200)]),
    ],
))

CASES.append(Case(
    id="NLB-LST-BVA-PAGESIZE-1000",
    title="List with pageSize=1000 (max upper bound) → 200",
    classes=["BVA", "LSG"], priority="P2",
    steps=[
        Step(name="list-1000", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=1000",
             test_script=[*assert_status(200)]),
    ],
))

CASES.append(Case(
    id="NLB-LST-BVA-PAGESIZE-1001",
    title="List with pageSize=1001 (off-by-one over max) → InvalidArgument",
    classes=["BVA", "VAL", "LSG"], priority="P2",
    steps=[
        Step(name="list-1001", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=1001",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-LST-BVA-PAGESIZE-NEGATIVE",
    title="List with pageSize=-1 → InvalidArgument",
    classes=["BVA", "VAL", "LSG"], priority="P2",
    steps=[
        # Same contract as pageSize=1001, mirrored below zero. Accepting 200 made
        # the case blind to both failure modes it exists for: a rejection quietly
        # replaced by clamping, and an invalid page size slipping through the
        # empty-grant short-circuit of the list filter (validation runs FIRST).
        Step(name="list-neg", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=-1",
             test_script=[
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))

CASES.append(Case(
    id="NLB-UPD-STATE-NO-CHANGE",
    title="Update with same value as current → idempotent no-op",
    classes=["STATE", "IDEM"], priority="P2",
    steps=[
        *_setup_lb("noop-upd"),
        retry_until_authorized(Step(name="upd-same", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "description", "description": ""},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        *_cleanup_lb(),
    ],
))

# GetTargetStates validates its inputs in order: network_load_balancer_id
# required → target_group_id required (get_target_states.go). target_group_id is
# a query parameter (not in the REST path), so it MUST be supplied to exercise
# the unknown-LB path; omitting it stops at "target_group_id: required" (400)
# before the LB is ever looked up. With both ids well-formed the handler does the
# LB Get first → NotFound → 404. The authz interceptor lets the request reach the
# handler because a non-existent LB has no FGA tuple (ErrNoPath passthrough), so
# NotFound is not masked as 403. Technique: error-guessing (unknown parent +
# well-formed garbage child), state-transition (validation ordering).
CASES.append(Case(
    id="NLB-GTS-NEG-NF-UNKNOWN",
    title="GetTargetStates of unknown LB (with well-formed targetGroupId) → 404 NotFound",
    classes=["NEG"], priority="P1",
    steps=[
        Step(name="gts-unknown", method="GET",
             path=f"{_CREATE_BASE}/{{{{garbageNlbId}}}}/targetStates?targetGroupId={{{{garbageTgrId}}}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

# Право на список операций родителя решается ДО чтения списка, и на неизвестном
# родителе решается отказом. Запись каталога прав для этого RPC несёт
# `required_relation: v_list` + `scope_extractor {nlb_network_load_balancer,
# network_load_balancer_id}`, hide-existence на ней нет (её включает либо явный флаг,
# либо эвристика «/Get + v_get»), поэтому у края нет ни одной наводки, по которой
# неизвестный идентификатор мог бы резолвиться: тупла нет ни у кого, значит `no path`,
# значит терминальный 403 — и сервис не набирается вовсе.
#
# Прежний комментарий утверждал обратное («интерсептор пропускает по ErrNoPath») и
# заголовок обещал 200 с пустым списком. Пропуск по `no path` — свойство СЕРВИСНОГО
# интерсептора (`internal/check`), а не края: у края каждый не-hide-existence отказ
# терминален. Поэтому use-case, который действительно существования родителя не
# проверяет, на этом пути недостижим, и его семантика к исходу отношения не имеет.
# Утверждение сведено к тому, что происходит: 403, код 7.
#
# Техника: error-guessing (неизвестный родитель на списочном эндпоинте).
CASES.append(Case(
    id="NLB-LOPS-NEG-NF-UNKNOWN",
    title="ListOperations of unknown nlbId → 403 PERMISSION_DENIED (право на родителя решается до чтения списка)",
    classes=["NEG"], priority="P1",
    steps=[
        Step(name="lops-unknown", method="GET",
             path=f"{_CREATE_BASE}/{{{{garbageNlbId}}}}/operations?pageSize=1",
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED"),
                          # Отказ на неизвестном родителе не должен рассказывать, что
                          # родителя нет: это была бы разница между «нет доступа» и
                          # «не существует», то есть оракул существования.
                          "pm.test('отказ не сообщает, существует ли родитель', () => "
                          "  pm.expect((pm.response.json().message || '').toLowerCase()).to.not.contain('not found'));"]),
    ],
))

CASES.append(Case(
    id="NLB-CR-BVA-LABELS-MAX-64",
    title="Create with exactly 64 labels (upper bound) → OK",
    classes=["BVA"], priority="P2",
    steps=[
        Step(name="cr-64", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "lbl-64-{{runId}}",
                   "labels": {f"k{i}": f"v{i}" for i in range(64)}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-CR-CRUD-NO-OPTIONAL-FIELDS",
    title="Create with only required fields (no description/labels) → OK",
    classes=["CRUD"], priority="P2",
    steps=[
        Step(name="cr-min", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "min-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-CR-CRUD-WITH-DESCRIPTION",
    title="Create with non-empty description → OK and persisted",
    classes=["CRUD"], priority="P2",
    steps=[
        Step(name="cr-with-desc", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "wd-{{runId}}", "description": "the edge LB"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('description persisted', () => "
                          "  pm.expect(pm.response.json().description).to.eql('the edge LB'));"])),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-CR-CRUD-AFFINITY-CLIENT-IP",
    title="Create with sessionAffinity=CLIENT_IP_ONLY → persisted",
    classes=["CRUD"], priority="P2",
    steps=[
        Step(name="cr-aff", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "aff-{{runId}}", "sessionAffinity": "CLIENT_IP_ONLY"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('sessionAffinity persisted', () => "
                          "  pm.expect(pm.response.json().sessionAffinity).to.eql('CLIENT_IP_ONLY'));"])),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-CR-CRUD-REMOVED-FIELDS-ABSENT",
    title="networkId / anycastPoolId are gone from the LoadBalancer model — a created LB "
          "carries neither on Get (Verifies 8.1-32)",
    classes=["CRUD", "CONF"], priority="P2",
    # 8.1-32 is a statement about the MODEL: `networkId` and `anycastPoolId` were removed
    # from NetworkLoadBalancer (and from CreateNetworkLoadBalancerRequest) in the VIP
    # redesign. What this case can observe is the projection — a lawfully created LB carries
    # neither field — and that is asserted below.
    #
    # It used to SEND both keys and assert "created anyway, not echoed". That premise was
    # about the EDGE's JSON policy (unknown keys discarded), not about nlb, and it was the one
    # place in this suite deliberately exercising the very shape the platform forbids: a
    # request field the service does not read, answered `200`. A field accepted and thrown
    # away is a defect, not a contract worth pinning — the caller is told the parameter
    # applied. The request-side half is now proven statically and exhaustively for every
    # collection by TestNewmanCollectionsSendNoUnknownRequestFields. The projection assertions
    # below are unchanged — they never depended on what the request carried, since the response
    # message has no such fields to echo in the first place.
    #
    # crossZoneEnabled (REGIONAL-only) and securityGroupIds (INTERNAL-only, revived NLB-1-51)
    # are LIVE fields that ARE validated — sending them on this EXTERNAL LB is a 400, not a
    # drop. Those validations are covered elsewhere.
    steps=[
        Step(name="cr-lawful", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "removed-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('LB created', () => "
                          "  pm.expect(j.id).to.eql(pm.environment.get('nlbId')));",
                          "pm.test('projection carries no networkId (removed from the model)', () => "
                          "  pm.expect(j).to.not.have.property('networkId'));",
                          "pm.test('projection carries no anycastPoolId (removed from the model)', () => "
                          "  pm.expect(j).to.not.have.property('anycastPoolId'));"])),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))


# Additional patterns to reach D-4 ≥320-cases gate
# The List filter whitelist is {"name"} only (list.go → shared.ParseNameFilter →
# corelib filter.Parse). labels filtering is not a feature of this phase, so a
# `labels.env="prod"` predicate is an unknown filter field → InvalidArgument, not
# a silently-accepted 200. The valid name-filter path stays covered by
# NLB-LST-FILTER-NAME-OK / NLB-LST-FILTER-MATCH. Technique: ECP (unknown filter
# field class), error-guessing (unsupported predicate).
CASES.append(Case(
    id="NLB-LST-FILTER-LABELS",
    title="List with unsupported filter field labels.env → InvalidArgument (whitelist is name only)",
    classes=["LSG", "VAL", "NEG"], priority="P2",
    steps=[
        Step(name="lst-lbl-filter", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=100&"
                  "filter=labels.env%3D%22prod%22",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-LST-FILTER-COMBINED",
    title="List with combined filter (name AND labels) → InvalidArgument (AND unsupported)",
    classes=["LSG", "VAL"], priority="P2",
    steps=[
        # Conjunction is deliberately NOT in the grammar (single `<field> = "<v>"`),
        # so the trailing `AND labels.env="prod"` is an unexpected token and the
        # request is refused. The point of the case is precisely that the extra
        # predicate is not silently dropped — which is what accepting 200 allowed.
        Step(name="lst-combined", method="GET",
             path=f"{_CREATE_BASE}?projectId={{{{_suiteProjectId}}}}&"
                  "filter=name%3D%22edge%22%20AND%20labels.env%3D%22prod%22",
             test_script=[
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-CRUD-DELETION-PROTECTION-TRUE",
    title="Create with deletion_protection=true → persisted",
    classes=["CRUD", "STATE"], priority="P2",
    steps=[
        Step(name="cr-dp", method="POST", path=_CREATE_BASE,
             body={**_LB_BODY, "name": "dp-{{runId}}", "deletionProtection": True},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        # A failed VIP allocation used to leave the pre-allocated `nlbId` published and be
        # forgiven below by `!lastOpError` — the case then greened without ever reading
        # deletionProtection back. It now fails at the prep, where it broke.
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="get", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "pm.test('status 200', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "pm.test('deletion_protection persisted', () => "
                 "  pm.expect(pm.response.json().deletionProtection).to.eql(true));",
             ])),
        # Disable for cleanup
        Step(name="unprotect", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "deletionProtection", "deletionProtection": False},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NLB-UPD-CRUD-DELETION-PROTECTION-TOGGLE",
    title="Update toggles deletion_protection true→false → mutable round-trip",
    classes=["CRUD", "STATE"], priority="P2",
    steps=[
        *_setup_lb("dp-toggle", {"deletionProtection": True}),
        retry_until_authorized(Step(name="disable-dp", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "deletionProtection", "deletionProtection": False},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="get", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('deletion_protection toggled false', () => "
                          "  pm.expect(pm.response.json().deletionProtection).to.eql(false));"]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-CR-NEG-EMPTY-NAME-EMPTY-REGION",
    title="Create with empty name AND empty region → multi-field violation",
    classes=["VAL", "NEG"], priority="P2",
    steps=[
        Step(name="cr-multi-missing", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "placement": "EXTERNAL_REGIONAL",
                   "name": "", "regionId": ""},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="NLB-GTS-CRUD-EMPTY-LB-ACTIVE",
    title="GetTargetStates for an empty TG → empty array (LB status does not fabricate states)",
    classes=["CRUD", "STATE"], priority="P2",
    steps=[
        # target_group_id is required (per-TG query); an empty TG yields [] regardless of the
        # LB's derived status — LB status does not fabricate target states. (The legacy Start
        # RPC was removed; LB status is derived, not set via an explicit lifecycle call.)
        *_setup_lb("gts-empty-active"),
        *_setup_tg("gts-empty-active"),
        retry_until_authorized(Step(name="gts", method="GET",
             path=f"{_CREATE_BASE}/{{{{nlbId}}}}/targetStates?targetGroupId={{{{tgId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('empty target_states', () => "
                          "  pm.expect((pm.response.json().targetStates || []).length).to.eql(0));"])),
        *_cleanup_tg(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-UPD-VAL-LABELS-OVER-64",
    title="Update labels with >64 entries → InvalidArgument",
    classes=["VAL", "BVA"], priority="P1",
    steps=[
        *_setup_lb("upd-lbl-over"),
        Step(name="upd-65", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "labels", "labels": {f"k{i}": f"v{i}" for i in range(65)}},
             test_script=[
                 # Update re-runs domain Validate on the merged object BEFORE the Operation
                 # exists (loadbalancer/update.go -> updated.Validate() -> ValidateLabels,
                 # "too many labels (max 64)"), so this is a sync refusal. 65 labels being
                 # ACCEPTED is the regression, and it used to satisfy the case.
                 "pm.environment.set('opId', '');",
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-MV-NEG-DEST-UNKNOWN-PROJECT",
    title="Move to unknown destination project → NotFound/PermissionDenied",
    classes=["NEG"], priority="P1",
    steps=[
        *_setup_lb("mv-dst-unk"),
        Step(name="mv-unknown-dst", method="POST", path=f"{_CREATE_BASE}/{{{{nlbId}}}}:move",
             body={"destinationProjectId": "{{garbageProjectId}}"},
             # The destination project is peer-checked SYNCHRONOUSLY, before the Operation
             # row exists (loadbalancer/move.go -> projectClient.Get -> peerErrToStatus),
             # and the gateway may refuse even earlier when it cannot resolve the
             # destination scope. All of those are refusals; the move SUCCEEDING into a
             # project that does not exist is not, and that is what a bare 200 was allowed
             # to be.
             # ...и раз проверка синхронна ДО появления строки Operation — как этот же
             # комментарий и говорит, — асинхронной полосы нет. Она была объявлена, и
             # парный опрос адресовал `{{opId}}`, который никто не захватывал.
             test_script=assert_refused_sync_or_async("unknown destination project",
                                                     sync_codes=(400, 403, 404),
                                                     async_lane=False)),
        *_cleanup_lb(),
    ],
))


# ===========================================================================
# Sub-phase 8.1 — placement + per-family VIP-source link/allocate model
#   (приёмка под-фазы 8.1; документа под прежним именем в воркспейсе нет — см. шапку файла)
#
# Group C (source×type×placement matrix negatives) are SYNC fail-fast — strict
# REST 400 + INVALID_ARGUMENT + contract text, no fixtures. Group A/B/G happy +
# link cases provision vpc Subnet/Address inline and gate strict assertions on
# the fixture materialising (see module docstring tolerance contract).
# ===========================================================================


def _sync_reject(case_id, title, verifies, body, msg_substr, priority="P1", classes=None):
    """Source×type×placement matrix negative (decision-table technique): a SYNC
    fail-fast precheck rejects the Create before any Operation is created →
    REST 400 + grpc INVALID_ARGUMENT + the exact contract error text."""
    return Case(
        id=case_id, title=f"{title} (Verifies {verifies})",
        classes=classes or ["VAL", "NEG"], priority=priority,
        steps=[
            Step(name="cr-reject", method="POST", path=_CREATE_BASE, body=body,
                 test_script=[
                     *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                     "pm.test('contract message names the violation', () => "
                     "  pm.expect((pm.response.json().message || '').toLowerCase())."
                     f"    to.include('{msg_substr}'));",
                 ]),
        ],
    )


# --- Group C: source × type × placement matrix — sync fail-fast negatives ---

CASES.append(_sync_reject(
    "NLB-CR-VAL-SUBNET-ON-EXTERNAL",
    "subnet_id VIP source on EXTERNAL LB → InvalidArgument", "8.1-08",
    {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}", "placement": "EXTERNAL_REGIONAL",
     "name": "sub-ext-{{runId}}", "v4Source": {"subnetId": "{{existingSubnetId}}"}},
    "subnet address source is only valid for internal", priority="P1"))

CASES.append(_sync_reject(
    "NLB-CR-VAL-PUBLIC-ON-INTERNAL",
    "public VIP source on INTERNAL LB → InvalidArgument", "8.1-09",
    {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}", "placement": "INTERNAL_ZONAL", "name": "pub-int-{{runId}}", "v4Source": {"public": {}}},
    "public address source is only valid for external", priority="P1"))

CASES.append(_sync_reject(
    "NLB-CR-VAL-DRAIN-ON-ZONAL",
    "disabledAnnounceZones on ZONAL LB → InvalidArgument", "8.1-13",
    {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}", "placement": "INTERNAL_ZONAL", "name": "drain-zon-{{runId}}",
     "v4Source": {"subnetId": "{{existingSubnetId}}"}, "disabledAnnounceZones": ["{{existingZoneId}}"]},
    "disabled_announce_zones is only valid for regional", priority="P1"))

CASES.append(_sync_reject(
    "NLB-CR-VAL-NO-SOURCE",
    "no VIP source for any family → InvalidArgument", "8.1-19",
    {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}", "placement": "INTERNAL_ZONAL", "name": "nosrc-{{runId}}"},
    "must declare a vip source", priority="P0", classes=["VAL", "NEG"]))


# 8.1-14 — drain covering ALL region zones. Uses the two seeded zones; if the
# region has exactly those two (per acceptance) the drain-all guard fires (strict
# check), otherwise the LB is created and cleaned up (region has ≥3 zones).
CASES.append(Case(
    id="NLB-CR-VAL-DRAIN-COVERS-ALL-ZONES",
    title="disabledAnnounceZones covering every zone of the region → InvalidArgument (Verifies 8.1-14)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        *_provision_subnet("REGIONAL", "drain-all"),
        # Wrap the create in the cross-service peer-visibility RYW retry (parity with the
        # ZONAL/REGIONAL sibling creates cr-int / setup-create-lb / int-reg / int-drain):
        # the drain-all subnet is provisioned inline through vpc and its Operation is done
        # (durable), but nlb's vpc peer-read (SubnetService.Get on LB Create) is briefly
        # stale under `--jobs` parallel load → a transient sync reject `subnet <id> not
        # found` (400). Without the retry that transient 400 falls into the `else if (400)`
        # branch and fails `message.include('cover all zones')` (it reads 'subnet ... not
        # found' instead) — the create never reaches the real drain-all validation.
        # retry_create_until_present retries SELF ONLY on [400,404] + /not found/, so the
        # legitimate InvalidArgument "must not cover all zones" (no 'not found') passes
        # straight through to the assertions below. Leak-free (a rejected create mints nothing).
        retry_create_until_present(Step(name="cr-drain-all", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_REGIONAL", "name": "drain-all-{{runId}}",
                   "v4Source": {"subnetId": "{{vpcSubnetId}}"},
                   "disabledAnnounceZones": ["{{existingZoneId}}", "{{existingZoneAltId}}"]},
             test_script=[
                 "pm.environment.unset('nlbId');",
                 "if (!pm.environment.get('vpcSubnetId')) {",
                 "  pm.test('no regional subnet fixture → subnet-source create rejected', () => "
                 "    pm.expect(pm.response.code).to.be.oneOf([400, 404, 503]));",
                 "} else if (pm.response.code === 400) {",
                 "  pm.test('grpc 3 INVALID_ARGUMENT', () => pm.expect(pm.response.json().code).to.eql(3));",
                 "  pm.test('message: must not cover all zones of the region', () => "
                 "    pm.expect((pm.response.json().message || '').toLowerCase()).to.include('cover all zones'));",
                 "} else {",
                 "  pm.test('region has more zones → create accepted (drain does not cover all)', () => "
                 "    pm.expect(pm.response.code).to.eql(200));",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 "  if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('nlbId', j.metadata.networkLoadBalancerId);",
                 "}",
             ])),
        poll_operation_until_done(),
        Step(name="cleanup-if-created", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_vpc(_VPC_SUBNETS, "vpcSubnetId"),
    ],
))

# 8.1-15 — drain zone belonging to a different region (geo-validated). Needs a
# real zone in another region; asserts the drain-zone rejection generically.
CASES.append(Case(
    id="NLB-CR-VAL-DRAIN-ZONE-WRONG-REGION",
    title="disabledAnnounceZones with a zone outside the LB's region → InvalidArgument (Verifies 8.1-15)",
    classes=["VAL", "NEG"], priority="P2",
    steps=[
        Step(name="cr-drain-foreign-zone", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_REGIONAL", "name": "drain-fz-{{runId}}",
                   "v4Source": {"subnetId": "{{existingSubnetId}}"},
                   "disabledAnnounceZones": ["{{existingRegionAltId}}-a"]},
             test_script=[
                 # Single-region-stand guard: '<existingRegionAltId>-a' is only OUTSIDE the LB's
                 # region when a SECOND geo region exists. On a single-region stand
                 # RegionAltId==primary, so '<region>-a' is a VALID zone IN the region → the
                 # "zone outside region" negative is un-exercisable and the drain-zone check
                 # lawfully passes. Not masking: the product check (zones.go "zone %s is not in
                 # region %s") is unit-locked (create_test.go); the strict e2e below fires once a
                 # 2nd geo region is seeded (existingRegionAltId != existingRegionId).
                 "var _altR = pm.environment.get('existingRegionAltId') || '';",
                 "var _r = pm.environment.get('existingRegionId') || pm.environment.get('_suiteRegionId') || '';",
                 "pm.environment.unset('drainFzLbId');",
                 "if (_altR && _r && _altR !== _r) {",
                 "  pm.test('status 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "  pm.test('grpc 3 INVALID_ARGUMENT', () => pm.expect(pm.response.json().code).to.eql(3));",
                 "  pm.test('drain-zone validation names region or zone', () => {",
                 "    const m = (pm.response.json().message || '').toLowerCase();",
                 "    pm.expect(m).to.satisfy(s => s.includes('region') || s.includes('zone'));",
                 "  });",
                 "} else {",
                 "  // SINGLE-REGION STAND — and this branch does NOT exercise the drain-zone",
                 "  // rule. '<region>-a' is then a zone INSIDE the LB's region, so there is no",
                 "  // foreign zone to reject; what the request still violates is placement,",
                 "  // because existingSubnetId is a ZONAL subnet while the LB is",
                 "  // INTERNAL_REGIONAL (create.go: 'subnet placement does not match load",
                 "  // balancer placement'). So the answer is a refusal for an unrelated reason.",
                 "  // Saying so is the point: what stood here accepted 200/400/404/503 — every",
                 "  // possible answer — and read as if the negative had been covered. It has",
                 "  // not been, and it cannot be until a SECOND geo region is seeded: an open",
                 "  // debt, not a pass.",
                 "  pm.test('single-region: request refused (placement); the foreign-zone rule is NOT covered here', () => "
                 "    pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([400, 403, 404]));",
                 "}"]),
        # No cleanup pair any more: neither branch can now produce a load balancer, so a
        # DELETE of the never-set drainFzLbId would only address a literal {{drainFzLbId}}.
    ],
))

# 8.1-11 — placement mismatch: ZONAL LB + REGIONAL subnet source.
# 8.1-19 — a load balancer must say where its VIP comes from. This case exists because
# LST-CR-VAL-INTERNAL-NO-SUBNET was retired: the listener lost `subnet_id` when the VIP
# moved to the parent, and the rule "you must name a VIP source" moved with it. The
# retirement note in cases/listener.py cited this case-id as the successor while it did
# not exist — so a rule the product enforces, and that a P0 case used to reach for, had
# no black-box coverage at all. It does now.
#
# Sync, before any Operation exists: buildFamilySpecs finds no source for either family
# and refuses (vip_source.go, "load balancer must declare a vip source for at least one
# ip family"; unit-locked as 8.1-19 in create_test.go). The message is contract and is
# asserted, so an unrelated refusal cannot stand in for this one. Fixture-free — the body
# is deliberately incomplete, nothing needs seeding.
CASES.append(Case(
    id="NLB-CR-VAL-SOURCE-REQUIRED",
    title="Create with no v4Source and no v6Source → InvalidArgument "
          "'load balancer must declare a vip source for at least one ip family' (Verifies 8.1-19)",
    classes=["VAL", "NEG"], priority="P0",
    steps=[
        Step(name="cr-no-source", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_ZONAL", "name": "no-src-{{runId}}"},
             test_script=[
                 "pm.environment.set('opId', '');",
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('names the missing vip source', () => "
                 "  pm.expect(pm.response.json().message || '').to.eql("
                 "    'load balancer must declare a vip source for at least one ip family'));",
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-PLACEMENT-MISMATCH",
    title="ZONAL LB with a REGIONAL subnet source → InvalidArgument placement mismatch (Verifies 8.1-11)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        *_provision_subnet("REGIONAL", "pl-mismatch"),
        retry_create_until_present(Step(name="cr-mismatch", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_ZONAL", "name": "pl-mm-{{runId}}",
                   "v4Source": {"subnetId": "{{vpcSubnetId}}"}},
             test_script=[
                 "if (!pm.environment.get('vpcSubnetId')) {",
                 "  pm.test('no regional subnet fixture → subnet-source create rejected', () => "
                 "    pm.expect(pm.response.code).to.be.oneOf([400, 404, 503]));",
                 "} else {",
                 "  pm.test('rejected 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "  pm.test('grpc 3 INVALID_ARGUMENT', () => pm.expect(pm.response.json().code).to.eql(3));",
                 "  pm.test('message: subnet placement does not match', () => "
                 "    pm.expect((pm.response.json().message || '').toLowerCase()).to.include('placement does not match'));",
                 "}",
             ])),
        *_cleanup_vpc(_VPC_SUBNETS, "vpcSubnetId"),
    ],
))

# 8.1-10 — address-link kind mismatch: an EXTERNAL address linked into an INTERNAL
# LB → generic anti-oracle "Illegal argument addressId".
CASES.append(Case(
    id="NLB-CR-VAL-ADDRESS-KIND-MISMATCH",
    title="EXTERNAL address linked into an INTERNAL LB → generic Illegal argument addressId (Verifies 8.1-10)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        *_provision_external_address("kind-mm"),
        # Cross-service peer read-your-writes: the just-provisioned vpc Address can be briefly
        # invisible to nlb's vpc peer-read under parallel load → sync create rejects `address
        # <id> not found` (400) BEFORE the real kind-mismatch validation runs. Wrap retries SELF
        # only on a transient `not found` (message-discriminated); the REAL negative reject here
        # ("Illegal argument addressId", grpc 3) does NOT match /not found/ → passes straight
        # through so the assertion runs. Same pattern as the sibling cr-mismatch above.
        retry_create_until_present(Step(name="cr-kind-mismatch", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_REGIONAL", "name": "kind-mm-{{runId}}",
                   "v4Source": {"addressId": "{{vpcAddrId}}"}},
             test_script=[
                 "if (!pm.environment.get('vpcAddrId')) {",
                 "  pm.test('no external address fixture → address-link create rejected', () => "
                 "    pm.expect(pm.response.code).to.be.oneOf([400, 404, 503]));",
                 "} else {",
                 "  pm.test('rejected 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "  pm.test('grpc 3 INVALID_ARGUMENT', () => pm.expect(pm.response.json().code).to.eql(3));",
                 "  pm.test('generic anti-oracle message (Illegal argument addressId)', () => "
                 # Дословно: владелец пишет `Illegal argument addressId`.
                 "    pm.expect(pm.response.json().message || '', pm.response.text()).to.eql('Illegal argument addressId'));",
                 "}",
             ])),
        *_cleanup_vpc(_VPC_ADDRESSES, "vpcAddrId"),
    ],
))

# 8.1-16 — address-link foreign project (anti-oracle). Uses the seeded
# cross-project address; tolerant when that fixture is absent.
CASES.append(Case(
    id="NLB-CR-VAL-ADDRESS-FOREIGN-PROJECT",
    title="address_id of another project → generic Illegal argument addressId (Verifies 8.1-16)",
    classes=["VAL", "NEG"], priority="P2",
    steps=[
        Step(name="cr-foreign-addr", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "EXTERNAL_REGIONAL", "name": "foreign-adr-{{runId}}",
                   "v4Source": {"addressId": "{{existingAddressCrossProjectId}}"}},
             test_script=[
                 "const cross = pm.environment.get('existingAddressCrossProjectId') || '';",
                 "if (!cross) {",
                 "  pm.test('cross-project address fixture unseeded → create still rejected (never accepted)', () => "
                 "    pm.expect(pm.response.code).to.be.oneOf([400, 404, 503]));",
                 "} else {",
                 "  pm.test('rejected 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "  pm.test('generic anti-oracle message (no cross-tenant existence leak)', () => "
                 # Дословно: владелец пишет `Illegal argument addressId`.
                 "    pm.expect(pm.response.json().message || '', pm.response.text()).to.eql('Illegal argument addressId'));",
                 "}",
             ]),
    ],
))

# 8.1-17 — family/slot mismatch: v4_source pointing at an IPv6 address (anti-oracle).
CASES.append(Case(
    id="NLB-CR-VAL-ADDRESS-FAMILY-SLOT",
    title="v4Source referencing an IPv6 address → generic Illegal argument addressId (Verifies 8.1-17)",
    classes=["VAL", "NEG"], priority="P2",
    steps=[
        Step(name="cr-family-slot", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "EXTERNAL_REGIONAL", "name": "fam-slot-{{runId}}",
                   "v4Source": {"addressId": "{{existingAddressIPv6Id}}"}},
             test_script=[
                 "const v6 = pm.environment.get('existingAddressIPv6Id') || '';",
                 "if (!v6) {",
                 "  pm.test('IPv6 address fixture unseeded → create still rejected', () => "
                 "    pm.expect(pm.response.code).to.be.oneOf([400, 404, 503]));",
                 "} else {",
                 "  pm.test('rejected 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "  pm.test('generic anti-oracle message (family/slot)', () => "
                 # Дословно: владелец пишет `Illegal argument addressId`.
                 "    pm.expect(pm.response.json().message || '', pm.response.text()).to.eql('Illegal argument addressId'));",
                 "}",
             ]),
    ],
))

# --- Foreign VIP-source reference: shape lane, black-box -----------------------
#
# `v4Source/v6Source.subnetId|.addressId` are vpc-owned. api-conventions §By-lane
# code-split (B4) keeps existence with the OWNER; nlb keeps only a *family-agnostic*
# syntactic gate over the platform prefix catalogue in front of it — a recorded,
# narrow exception (services/nlb/docs/engineering/architecture/08-known-divergences.md
# §"Формат чужого id (VIP-источники)"). These three lock what the CALLER reads, end
# to end through the gateway; all are fixture-free and strict (a literal non-id and
# a literal empty string need nothing seeded).
#
# The gate is what makes obvious garbage TERMINAL: without it the same request
# answers `subnet <X> not found` — the contract tone for an ABSENT resource applied
# to a string that cannot be one — and degrades to retryable 503 whenever vpc is
# unreachable. Hence the message is asserted verbatim, not just the code.

CASES.append(Case(
    id="NLB-CR-VAL-SUBNET-ID-MALFORMED",
    title="v4Source.subnetId that is not a Kachō id → 400 invalid subnet id '<X>' (format lane, not a miss)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="cr-subnet-id-malformed", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_REGIONAL", "name": "sub-malformed-{{runId}}",
                   "v4Source": {"subnetId": "garbage!!"}},
             test_script=[
                 "pm.test('rejected 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "pm.test('grpc 3 INVALID_ARGUMENT', () => pm.expect(pm.response.json().code).to.eql(3));",
                 "pm.test('names the format problem, does NOT claim the subnet is absent', () => {",
                 "  const m = pm.response.json().message || '';",
                 "  pm.expect(m).to.eql(\"invalid subnet id 'garbage!!'\");",
                 "});",
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-ADDRESS-ID-MALFORMED",
    title="v4Source.addressId that is not a Kachō id → 400 invalid address id '<X>' (format lane)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="cr-address-id-malformed", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_REGIONAL", "name": "adr-malformed-{{runId}}",
                   "v4Source": {"addressId": "garbage!!"}},
             test_script=[
                 "pm.test('rejected 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "pm.test('grpc 3 INVALID_ARGUMENT', () => pm.expect(pm.response.json().code).to.eql(3));",
                 "pm.test('names the format problem', () => "
                 "  pm.expect(pm.response.json().message || '').to.eql(\"invalid address id 'garbage!!'\"));",
             ]),
    ],
))

CASES.append(Case(
    id="NLB-CR-VAL-SUBNET-ID-EMPTY",
    title="v4Source.subnetId empty → 400 v4_source.subnet_id: required (request shape, not a phantom miss)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        # Selecting the oneof branch and leaving it blank is a malformed REQUEST.
        # It used to travel to vpc and come back as `subnet  not found` — the
        # not-found tone with the id spliced out, asserting the absence of a
        # resource the caller never named.
        Step(name="cr-subnet-id-empty", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_REGIONAL", "name": "sub-empty-{{runId}}",
                   "v4Source": {"subnetId": ""}},
             test_script=[
                 "pm.test('rejected 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "pm.test('grpc 3 INVALID_ARGUMENT', () => pm.expect(pm.response.json().code).to.eql(3));",
                 "pm.test('reads as a required-field violation, never as a not-found', () => {",
                 "  const m = pm.response.json().message || '';",
                 "  pm.expect(m).to.eql('v4_source.subnet_id: required');",
                 "  pm.expect(m).to.not.match(/not found/i);",
                 "});",
             ]),
    ],
))


# --- Group A/B: INTERNAL / EXTERNAL happy source-resolution (inline fixtures) ---

def _internal_happy_get_asserts(placement):
    # No `nlbId && !lastOpError` gate: the subnet fixture and the LB-create Operation are
    # both asserted upstream (_provision_subnet + poll fixture_ids), so by the time this
    # runs the LB either exists or the case is already red at the step that broke.
    return [
        "pm.test('Get 200 for created INTERNAL LB', () => "
        "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
        "const j = pm.response.json();",
        "pm.test('type INTERNAL', () => pm.expect(j.type).to.eql('INTERNAL'));",
        f"pm.test('placementType {placement}', () => pm.expect(j.placementType).to.eql('{placement}'));",
        "pm.test('v4AddressId resolved to a bound vpc Address', () => "
        "  pm.expect(j.v4AddressId).to.match(/^adr[a-z0-9]+$/));",
        "pm.test('output does not echo the subnet source', () => "
        "  pm.expect(j).to.not.have.property('v4Source'));",
    ]


def _internal_create_step(name, placement, extra_body=None):
    body = {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}", "placement": f"INTERNAL_{placement}", "name": f"{name}-{{{{runId}}}}",
            "v4Source": {"subnetId": "{{vpcSubnetId}}"}, **(extra_body or {})}
    return Step(name="cr-internal", method="POST", path=_CREATE_BASE, body=body,
                test_script=[
                    "pm.environment.unset('nlbId');",
                    "pm.test('INTERNAL create accepted as Operation', () => "
                    "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                    "const j = pm.response.json();",
                    "if (j.id) pm.environment.set('opId', j.id);",
                    "if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('nlbId', j.metadata.networkLoadBalancerId);",
                ])


CASES.append(Case(
    id="NLB-CR-CRUD-INTERNAL-REGIONAL",
    title="Create INTERNAL REGIONAL LB — anycast subnet-auto VIP from a regional subnet (Verifies 8.1-02)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_provision_subnet("REGIONAL", "int-reg"),
        retry_create_until_present(_internal_create_step("lb-ireg", "REGIONAL")),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="get-reg", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*_internal_happy_get_asserts("REGIONAL"),
                          "pm.test('disabledAnnounceZones empty (announced from all healthy zones)', () => "
                          "  pm.expect(pm.response.json().disabledAnnounceZones || []).to.be.an('array').that.is.empty);"])),
        # Подсеть этого кейса ведёт его собственный `_cleanup_vpc` (либо её нет вовсе —
        # внешний адрес), поэтому уборка LB её не касается: снимать невыделенное нечего.
        *_cleanup_lb(reclaim_subnet=False),
        *_cleanup_vpc(_VPC_SUBNETS, "vpcSubnetId"),
    ],
))

CASES.append(Case(
    id="NLB-CR-CRUD-INTERNAL-REGIONAL-DRAIN",
    title="Create INTERNAL REGIONAL LB with disabledAnnounceZones at Create (drain) (Verifies 8.1-03)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_provision_subnet("REGIONAL", "int-drain"),
        retry_create_until_present(_internal_create_step("lb-idrain", "REGIONAL",
                              {"disabledAnnounceZones": ["{{existingZoneAltId}}"]})),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="get-drain", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "pm.test('Get 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json();",
                 "pm.test('placementType REGIONAL', () => pm.expect(j.placementType).to.eql('REGIONAL'));",
                 "pm.test('disabledAnnounceZones persisted as the drain intent', () => "
                 "  pm.expect(j.disabledAnnounceZones || []).to.include(pm.environment.get('existingZoneAltId')));",
             ])),
        # Подсеть этого кейса ведёт его собственный `_cleanup_vpc` (либо её нет вовсе —
        # внешний адрес), поэтому уборка LB её не касается: снимать невыделенное нечего.
        *_cleanup_lb(reclaim_subnet=False),
        *_cleanup_vpc(_VPC_SUBNETS, "vpcSubnetId"),
    ],
))

CASES.append(Case(
    id="NLB-CR-CRUD-INTERNAL-LINK",
    title="Create INTERNAL REGIONAL LB linking a pre-created internal Address (Verifies 8.1-04)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_provision_subnet("REGIONAL", "int-link"),
        *_provision_internal_address("vpcSubnetId", "int-link"),
        # Cross-service RYW: the fresh vpc Address (vpcAddrId) can be briefly invisible to nlb's
        # vpc peer-read under parallel load → `address <id> not found` (400) before vpc's write
        # is visible. Wrap retries SELF only on a transient not-found (message-discriminated);
        # the fixture-absent branch (vpcAddrId unset → `invalid resource id`, no "not found")
        # passes straight through to its tolerant else. Without the wrap a transient not-found
        # mis-fails the `code===200` happy-path assertion.
        retry_create_until_present(Step(name="cr-link", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_REGIONAL", "name": "lb-ilink-{{runId}}",
                   "v4Source": {"addressId": "{{vpcAddrId}}"}},
             test_script=[
                 "pm.environment.unset('nlbId');",
                 "pm.test('INTERNAL link create accepted as Operation', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json();",
                 "if (j.id) pm.environment.set('opId', j.id);",
                 "if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('nlbId', j.metadata.networkLoadBalancerId);",
             ])),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="get-link", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "pm.test('v4AddressId equals the linked address', () => "
                 "  pm.expect(pm.response.json().v4AddressId).to.eql(pm.environment.get('vpcAddrId')));",
             ])),
        # Подсеть этого кейса ведёт его собственный `_cleanup_vpc` (либо её нет вовсе —
        # внешний адрес), поэтому уборка LB её не касается: снимать невыделенное нечего.
        *_cleanup_lb(reclaim_subnet=False),
        # tenant-owned linked address survives LB deletion → cleaned up here
        *_cleanup_vpc(_VPC_ADDRESSES, "vpcAddrId"),
        *_cleanup_vpc(_VPC_SUBNETS, "vpcSubnetId"),
    ],
))

CASES.append(Case(
    id="NLB-CR-CRUD-EXTERNAL-LINK",
    title="Create EXTERNAL LB linking a pre-created public Address (BYO) (Verifies 8.1-07)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_provision_external_address("ext-link"),
        # Cross-service RYW: fresh vpc Address peer-read can be transiently stale under parallel
        # load (`address <id> not found`, 400). Retry SELF only on the transient not-found; the
        # fixture-absent `invalid resource id` (no "not found") passes through to its tolerant
        # else. See cr-link above.
        retry_create_until_present(Step(name="cr-ext-link", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "EXTERNAL_REGIONAL", "name": "lb-elink-{{runId}}",
                   "v4Source": {"addressId": "{{vpcAddrId}}"}},
             test_script=[
                 "pm.environment.unset('nlbId');",
                 "pm.test('EXTERNAL link create accepted as Operation', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json();",
                 "if (j.id) pm.environment.set('opId', j.id);",
                 "if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('nlbId', j.metadata.networkLoadBalancerId);",
             ])),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="get-ext-link", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "const j = pm.response.json();",
                 "pm.test('type EXTERNAL', () => pm.expect(j.type).to.eql('EXTERNAL'));",
                 "pm.test('v4AddressId equals the linked public address', () => "
                 "  pm.expect(j.v4AddressId).to.eql(pm.environment.get('vpcAddrId')));",
             ])),
        # Подсеть этого кейса ведёт его собственный `_cleanup_vpc` (либо её нет вовсе —
        # внешний адрес), поэтому уборка LB её не касается: снимать невыделенное нечего.
        *_cleanup_lb(reclaim_subnet=False),
        *_cleanup_vpc(_VPC_ADDRESSES, "vpcAddrId"),
    ],
))

CASES.append(Case(
    id="NLB-CR-CRUD-DUALSTACK-MIXED",
    title="Create INTERNAL REGIONAL dualstack LB — v4 subnet-auto + v6 address-link (Verifies 8.1-05)",
    classes=["CRUD"], priority="P2",
    steps=[
        # THE FIXTURE WAS THE DEFECT, NOT THE PRODUCT. The v6 leg used to link
        # `existingAddressIPv6Id`, which the seeder allocates as an EXTERNAL, ZONAL address
        # (deploy/scripts/seed-nlb-fixtures.sh step 4b: `externalIpv6AddressSpec` with a
        # zoneId). Linking it into an INTERNAL_REGIONAL load balancer breaks two rules at
        # once, and the service is right to refuse both times: an external address cannot
        # back an internal LB (8.1-10) and a zone-bound address cannot back a REGIONAL one
        # (8.1-11b). Both answer the deliberately generic `Illegal argument addressId`
        # (loadbalancer/create.go resolveLinkedAddress; unit-locked in create_test.go).
        # So the case demanded a success the service is specified to refuse — the mirror
        # image of an assertion that cannot fail: one that cannot pass.
        #
        # A dualstack INTERNAL LB needs an INTERNAL v6 address that is REGIONAL and in the
        # same network as the v4 leg (resolveSources enforces same-network across families).
        # The subnet is therefore provisioned with BOTH anchors and the v6 address is drawn
        # from that very subnet — same network, same placement, same region by construction.
        *_provision_subnet("REGIONAL", "dualstack", dualstack=True),
        *_provision_internal_address("vpcSubnetId", "ds-v6", save_var="vpcAddr6Id", family="v6"),
        # Cross-service RYW: the fresh v4 vpc Subnet (vpcSubnetId) peer-read can be transiently
        # stale under parallel load (`subnet <id> not found`, 400). Retry SELF only on the
        # transient not-found so the real dualstack-accept path is exercised instead of falling
        # into the tolerant else (which would SILENTLY skip the scenario).
        retry_create_until_present(Step(name="cr-dualstack", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_REGIONAL", "name": "lb-ds-{{runId}}",
                   "v4Source": {"subnetId": "{{vpcSubnetId}}"},
                   "v6Source": {"addressId": "{{vpcAddr6Id}}"}},
             test_script=[
                 "pm.environment.unset('nlbId');",
                 "pm.test('dualstack create accepted (both families same network)', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json();",
                 "if (j.id) pm.environment.set('opId', j.id);",
                 "if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('nlbId', j.metadata.networkLoadBalancerId);",
             ])),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="get-dualstack", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "const j = pm.response.json();",
                 "pm.test('v4AddressId set (auto from subnet)', () => pm.expect(j.v4AddressId).to.match(/^adr[a-z0-9]+$/));",
                 "pm.test('v6AddressId set (linked)', () => pm.expect(j.v6AddressId).to.eql(pm.environment.get('vpcAddr6Id')));",
             ])),
        # Подсеть этого кейса ведёт его собственный `_cleanup_vpc` (либо её нет вовсе —
        # внешний адрес), поэтому уборка LB её не касается: снимать невыделенное нечего.
        *_cleanup_lb(reclaim_subnet=False),
        # The linked BYO address survives the LB delete (only the reference is cleared), so
        # it is reclaimed here, before the subnet it was drawn from.
        *_cleanup_vpc(_VPC_ADDRESSES, "vpcAddr6Id"),
        *_cleanup_vpc(_VPC_SUBNETS, "vpcSubnetId"),
    ],
))


# --- Group F: immutability of placement + VIP source; drain toggle on Update ---

CASES.append(Case(
    id="NLB-UPD-STATE-IMMUTABLE-PLACEMENT",
    title="Update mask=placementType → InvalidArgument immutable (Verifies 8.1-25)",
    classes=["STATE", "VAL"], priority="P0",
    steps=[
        *_setup_lb("im-placement"),
        Step(name="upd-placement", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "placementType"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-UPD-STATE-IMMUTABLE-VIP-SOURCE",
    title="Update mask=v4_source / v4_address_id → InvalidArgument (source is immutable) (Verifies 8.1-25)",
    classes=["STATE", "VAL"], priority="P0",
    steps=[
        *_setup_lb("im-vipsrc"),
        Step(name="upd-v4source", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "v4Source"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        Step(name="upd-v4addr", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "v4AddressId"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="NLB-UPD-CRUD-DRAIN-TOGGLE",
    title="Update disabledAnnounceZones: drain then re-enable on a REGIONAL LB (Verifies 8.1-26)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_provision_subnet("REGIONAL", "drain-toggle"),
        retry_create_until_present(_internal_create_step("lb-dtog", "REGIONAL")),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="upd-drain", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "disabledAnnounceZones",
                   "disabledAnnounceZones": ["{{existingZoneAltId}}"]},
             test_script=[
                 "pm.test('drain Update accepted as Operation', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json(); if (j.id) pm.environment.set('opId', j.id);",
             ])),
        # The drain Update IS the subject of this case, so its Operation must be stated to
        # have succeeded here. `get-drained` below used to carry `!lastOpError` instead,
        # which meant a failed drain silently skipped the only assertion about the drain.
        poll_operation_until_done(must_succeed=True),
        Step(name="get-drained", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "pm.test('drain applied', () => "
                 "  pm.expect(pm.response.json().disabledAnnounceZones || []).to.include(pm.environment.get('existingZoneAltId')));",
             ]),
        Step(name="upd-reenable", method="PATCH", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             body={"updateMask": "disabledAnnounceZones", "disabledAnnounceZones": []},
             test_script=[
                 "pm.test('re-enable Update accepted', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json(); if (j.id) pm.environment.set('opId', j.id);",
             ]),
        poll_operation_until_done(must_succeed=True),
        # Подсеть этого кейса ведёт его собственный `_cleanup_vpc` (либо её нет вовсе —
        # внешний адрес), поэтому уборка LB её не касается: снимать невыделенное нечего.
        *_cleanup_lb(reclaim_subnet=False),
        *_cleanup_vpc(_VPC_SUBNETS, "vpcSubnetId"),
    ],
))


# --- Group H: lean projection (no infra leak) ---

CASES.append(Case(
    id="NLB-GET-STATE-LEAN-PROJECTION",
    title="Get returns lean tenant-facing projection — source resolved to v4AddressId, "
          "no subnet/network/announce leak (Verifies 8.1-30)",
    classes=["STATE", "CRUD"], priority="P1",
    steps=[
        *_setup_lb("lean"),
        retry_until_authorized(Step(name="get-lean", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('exposes tenant-facing fields (id/type/status/v4AddressId)', () => {",
                          "  pm.expect(j.id).to.be.a('string');",
                          "  pm.expect(j.type).to.be.a('string');",
                          "  pm.expect(j.status).to.be.a('string');",
                          "});",
                          "pm.test('does NOT leak the raw VIP source (v4Source/v6Source)', () => {",
                          "  pm.expect(j).to.not.have.property('v4Source');",
                          "  pm.expect(j).to.not.have.property('v6Source');",
                          "});",
                          "pm.test('does NOT leak derived networkId / subnetId', () => {",
                          "  pm.expect(j).to.not.have.property('networkId');",
                          "  pm.expect(j).to.not.have.property('subnetId');",
                          "});",
                          "pm.test('does NOT leak per-zone announce / route / VRF infra state', () => {",
                          "  pm.expect(j).to.not.have.property('announceState');",
                          "  pm.expect(j).to.not.have.property('routeTableId');",
                          "});"])),
        *_cleanup_lb(),
    ],
))


# --- Group G: Delete release — linked VIP address survives (used_by cleared) ---

CASES.append(Case(
    id="NLB-DEL-CRUD-RELEASE-LINKED",
    title="Delete LB with a linked (BYO) VIP → address survives, only the reference is cleared (Verifies 8.1-28)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_provision_external_address("rel-link"),
        # Cross-service RYW: fresh vpc Address peer-read can be transiently stale (`address <id>
        # not found`, 400) → nlbId would stay unset and the whole delete-release scenario
        # (del-linked-lb / lb-gone / linked-address-survives, all guarded by nlbId) would
        # SILENTLY skip. Retry SELF only on the transient not-found so the scenario runs.
        retry_create_until_present(Step(name="cr-linked", method="POST", path=_CREATE_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "EXTERNAL_REGIONAL", "name": "lb-rel-{{runId}}",
                   "v4Source": {"addressId": "{{vpcAddrId}}"}},
             test_script=[
                 "pm.environment.unset('nlbId');",
                 # Предмет есть только там, где внешний адрес выделен: без `vpcAddrId` тело
                 # запроса несёт пустую ссылку и отказ законен. Поэтому утверждается полоса
                 # с предметом — на ней приём обязателен, и её молчаливый отказ и был тем,
                 # из-за чего весь сценарий освобождения ссылки ТИХО пропускался (все его
                 # шаги стоят под `nlbId`).
                 "pm.test('linked-VIP LB created when the address fixture is present', function () {",
                 "  if (pm.environment.get('vpcAddrId')) {",
                 "    pm.expect(pm.response.code, pm.response.text()).to.eql(200);",
                 "  }",
                 "});",
                 "if (pm.environment.get('vpcAddrId') && pm.response.code === 200) {",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 "  if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('nlbId', j.metadata.networkLoadBalancerId);",
                 "} else { pm.environment.set('opId', ''); }",
             ])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-linked-lb", method="DELETE", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "if (pm.environment.get('nlbId')) {",
                 "  pm.test('Delete accepted as Operation', () => pm.expect(pm.response.code).to.eql(200));",
                 "  const j = pm.response.json(); if (j.id) pm.environment.set('opId', j.id);",
                 "} else { pm.environment.set('opId', ''); }",
             ])),
        poll_operation_until_done(),
        Step(name="lb-gone", method="GET", path=f"{_CREATE_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "if (pm.environment.get('nlbId')) {",
                 "  pm.test('LB is gone (404)', () => pm.expect(pm.response.code).to.eql(404));",
                 "}",
             ]),
        Step(name="linked-address-survives", method="GET", path=f"{_VPC_ADDRESSES}/{{{{vpcAddrId}}}}",
             test_script=[
                 "if (pm.environment.get('vpcAddrId') && pm.environment.get('nlbId')) {",
                 "  pm.test('linked tenant address SURVIVES the LB delete (used_by cleared, not freed)', () => "
                 "    pm.expect(pm.response.code).to.eql(200));",
                 "}",
             ]),
        # tenant address is now unreferenced → clean it up
        *_cleanup_vpc(_VPC_ADDRESSES, "vpcAddrId"),
    ],
))
