# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Cross-resource end-to-end cases (XRES-*) — sub-phase 6.0 S4 (UC-1/UC-2/UC-5).

Acceptance source: функциональная приёмка под-фазы 6.0, §S4. Документа под прежде
  стоявшим здесь именем в репозитории воркспейса нет и никогда не было (git log --all
  по пути: ноль коммитов); имя не воспроизводится.
  (6.0-34 EXTERNAL e2e, 6.0-35 INTERNAL e2e, 6.0-36 teardown bottom-up,
   6.0-37 dangling instance-target graceful read).
Design source: здесь стояла вторая такая же ссылка — на план проектирования под
  именем, которого в воркспейсе тоже никогда не было (ноль коммитов по пути через
  все ссылки). Обе снимались по одной причине, но в прошлый раз строкой ниже — то
  есть радиус правки был один абзац вместо класса. Живое описание сценариев —
  ниже в этом же docstring и в docs/CASES-INDEX.md набора.

These cases orchestrate the standard per-resource RPCs (already covered atomically
by load-balancer.py / listener.py / target-group.py / targets.py) into the full
tenant journeys the platform promises: stand up an L4 NLB "from nothing to
traffic-ready" (control-plane), tear it down bottom-up with correct address
lifecycle, and survive a dangling cross-service target reference on read.

Test-design techniques applied (skill testing-product-coach):
  - use-case / scenario flow (UC-1/UC-2/UC-5 happy journeys, multi-step Operation
    polling on every mutation);
  - state-transition (LB INACTIVE→ACTIVE on attach via lb_status_recompute trigger;
    delete-blocked→empty→deleted; target row survives peer deletion);
  - decision-table (scheme EXTERNAL vs INTERNAL × network_id presence → accept/reject);
  - error-guessing (default_target_group_id pointed at an un-attached TG → composite
    FK FAILED_PRECONDITION; v4 listener + v6 BYO Address → family mismatch; delete LB
    while it still holds a listener → "load balancer is not empty");
  - conformance (camelCase round-trip of S2/S3 fields sessionAffinity / crossZoneEnabled
    / networkId; Operation envelope on every mutation; computed TargetState enum).

Cross-domain fixture tolerance (deliberate, mirrors the rest of the suite):
the api-gateway routes every domain, but the seeded VPC/Compute fixtures
(network, subnet, instance) are not guaranteed on every CI lane — only on the
umbrella stack that runs kacho-deploy/scripts/seed-nlb-fixtures.sh. Steps whose
success depends on a peer fixture therefore assert the nlb-guaranteed contract
strictly (Operation envelope, sync-validation rejects, status transitions driven
by nlb's own DB triggers, graceful-read survival) and gate the peer-dependent
linkage assertions on the resource actually having been created. This keeps the
suite green on a bare lane while fully exercising the chain when fixtures exist.

REST base paths:
  /nlb/v1/networkLoadBalancers   /nlb/v1/listeners   /nlb/v1/targetGroups
"""

CASES = []

_LB_BASE = "/nlb/v1/networkLoadBalancers"
_LST_BASE = "/nlb/v1/listeners"
_TG_BASE = "/nlb/v1/targetGroups"
_VPC_SUBNETS = "/vpc/v1/subnets"

# sub-phase 8.1 note: the INTERNAL LoadBalancer now takes its VIP from a subnet
# source (v4Source.subnetId) + placementType, not from a top-level networkId. The
# removed inputs (networkId / securityGroupIds / crossZoneEnabled) are gone from the
# proto and silently ignored by grpc-gateway. The INTERNAL journey below provisions
# a zonal subnet inline; the listener-level fields still follow sub-phase-4.0 and
# are tracked for a separate listener acceptance.

_HC_TCP = {"interval": "2s", "timeout": "1s",
           "unhealthyThreshold": 3, "healthyThreshold": 2, "tcp": {"port": 8080}}

# Set of TargetState.Status enum strings the computed (control-plane) ramp may
# legitimately return. UNHEALTHY/STATUS_UNSPECIFIED are valid enum members even
# though the deterministic ramp currently emits only the first four — assert the
# member set, not a single literal (the dangling ref must NOT crash or mutate it).
_VALID_TARGET_STATE_JS = (
    "['STATUS_UNSPECIFIED','INITIAL','HEALTHY','UNHEALTHY','DRAINING','INACTIVE']"
)


# ---------------------------------------------------------------------------
# Reusable step fragments
# ---------------------------------------------------------------------------

def _create_external_lb(suffix: str, body_extra: dict = None):
    # sub-phase 8.1: EXTERNAL LB carries an auto public VIP source on Create.
    body = {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
            "placement": "EXTERNAL_REGIONAL", "name": f"xres-{suffix}-{{{{runId}}}}",
            "v4Source": {"public": {}}, **(body_extra or {})}
    # retry_create_until_present ALSO makes the step name unique (`-cr<N>`), which the
    # `retry_from` re-drive below depends on: `setNextRequest` resolves a duplicate name
    # to the FIRST step carrying it, so a shared literal "create-lb" would jump into an
    # earlier case's folder.
    create_lb = retry_create_until_present(
        Step(name="create-lb", method="POST", path=_LB_BASE, body=body,
             test_script=[*assert_status(200),
                          *assert_operation_envelope(prefix_regex="^nlb[a-z0-9]+$"),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]))
    return [
        create_lb,
        # PHANTOM-ID GUARD + async-lane re-drive (parity with listener.py::_setup_lb).
        #
        # A Kachō Operation carries the pre-allocated resource id in `metadata` even when
        # it finishes done:true WITH an error, so an unguarded capture publishes the id of
        # a LoadBalancer that does not exist. Every downstream step then addresses a
        # phantom and the gateway scope_extractor answers 403 — a cascade whose symptom
        # (authz denial on Listener.Create) has nothing to do with its cause (the VIP
        # allocation failed). Exactly what CI run 30135586348 produced:
        # XRES-E2E-DEFAULT-TG-ABSENT-REJ and XRES-E2E-DELETE-LB-NOT-EMPTY both red on
        # `create-listener -> 403` / `delete-blocked -> 403` while the REAL failure was
        # the parent op erroring with "could not allocate load balancer address".
        # Naming `nlbId` makes the poll unset it and fail HERE, attributably.
        poll_operation_until_done(fixture_ids=["nlbId"], retry_from=create_lb.name,
                                  retry_when="not found|allocation unavailable"),
        # read-your-writes: materialize the LB owner-tuple before cross-resource children
        # (Listener.Create / attach-TG) that authorize against editor@nlb_network_load_balancer.
        retry_until_authorized(Step(name="materialize-lb", method="GET",
             path=f"{_LB_BASE}/{{{{nlbId}}}}", test_script=[])),
    ]


def _create_tg(suffix: str):
    body = {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
            "name": f"xres-tg-{suffix}-{{{{runId}}}}", "port": 8080, "healthCheck": _HC_TCP}
    return [
        Step(name="create-tg", method="POST", path=_TG_BASE, body=body,
             test_script=[*assert_status(200),
                          *assert_operation_envelope(prefix_regex="^nlb[a-z0-9]+$"),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.targetGroupId", "tgId")]),
        poll_operation_until_done(),
        # read-your-writes: materialize the TG owner-tuple before attach / add-target.
        retry_until_authorized(Step(name="materialize-tg", method="GET",
             path=f"{_TG_BASE}/{{{{tgId}}}}", test_script=[])),
    ]


def _cleanup_lb():
    return [
        Step(name="cleanup-lb", method="DELETE", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


def _cleanup_tg():
    return [
        Step(name="cleanup-tg", method="DELETE", path=f"{_TG_BASE}/{{{{tgId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


def _cleanup_lst():
    return [
        Step(name="cleanup-lst", method="DELETE", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


# ===========================================================================
# 6.0-34 — UC-1: EXTERNAL NLB from nothing to traffic-ready (control-plane)
# ===========================================================================

CASES.append(Case(
    id="XRES-E2E-EXTERNAL-FULL-FLOW",
    title="UC-1 EXTERNAL NLB full chain: LB→listener(auto v4 VIP)→TG→addTargets→attach"
          "→default_tg→GetTargetStates (Verifies 6.0-34)",
    classes=["CRUD", "STATE"], priority="P0",
    steps=[
        *_create_external_lb("ext-flow", {"sessionAffinity": "FIVE_TUPLE"}),
        # Step 1 assertion: fresh LB has no listener/TG → INACTIVE. The EXTERNAL auto
        # public VIP (v4Source public) is allocated at LB Create → v4AddressId output
        # resolves to a bound vpc Address (sub-phase 8.1: per-family VIP lives on the LB,
        # not the listener). This is the auto-VIP check the get-listener-vip step used to
        # (wrongly) assert on the listener's removed allocated_address field.
        retry_until_authorized(Step(name="get-lb-inactive", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('LB starts INACTIVE (no listener/TG yet)', () => "
                          "  pm.expect(j.status).to.eql('INACTIVE'));",
                          "pm.test('type EXTERNAL', () => pm.expect(j.type).to.eql('EXTERNAL'));",
                          "pm.test('EXTERNAL auto public VIP: v4AddressId resolved to bound vpc Address', () => "
                          "  pm.expect(j.v4AddressId).to.match(/^adr[a-z0-9]+$/));"])),
        # Step 2: listener with auto external VIP. Child-create under the fresh LB is
        # authorized against editor@nlb_network_load_balancer — its owner-tuple is
        # eventually-consistent, so wrap the first child-create in the same bounded
        # read-your-writes retry as materialize-lb (round-4 wrapped update/get/del but
        # left the child-CREATE unwrapped, so a transient 403 reddened the whole chain).
        retry_until_authorized(Step(name="create-listener", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "edge-https-{{runId}}",
                   "protocol": "TCP", "port": 443},
             test_script=[*assert_status(200),
                          *assert_operation_envelope(prefix_regex="^nlb[a-z0-9]+$"),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        # The listener is step 2 of the chain this case exists to walk — its id is consumed
        # by every step after it, so a failed create must fail HERE rather than publish a
        # phantom `lstId` and be forgiven downstream by `if (!lastOpError)`.
        poll_operation_until_done(fixture_ids=["lstId"]),
        # sub-phase 8.1: VIP moved Listener→LoadBalancer; the Listener no longer carries
        # an address (allocated_address reserved). Assert the listener reaches ACTIVE; the
        # auto public VIP itself is verified on the parent LB (get-lb-inactive, v4AddressId).
        retry_until_authorized(Step(name="get-listener-vip", method="GET", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('listener ACTIVE', () => pm.expect(j.status).to.eql('ACTIVE'));"])),
        # Step 3: target group.
        *_create_tg("ext-flow"),
        # Step 4: register an Instance target (peer-validated in worker; tolerated
        # when the seeded Compute instance is absent on a bare lane). Authorized against
        # editor@nlb_target_group — fresh TG owner-tuple lag → bounded retry.
        retry_until_authorized(Step(name="add-instance-target", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"instanceId": "{{existingInstanceId}}", "weight": 100}]},
             test_script=[*assert_status(200),
                          *assert_operation_envelope(prefix_regex="^nlb[a-z0-9]+$"),
                          *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # Step 5: wire the TG into the listener via default_target_group_id — the listener
        # FK is now the TG↔LB link (attach/detach RPCs removed; the TG need only exist).
        Step(name="set-default-tg", method="PATCH", path=f"{_LST_BASE}/{{{{lstId}}}}",
             body={"updateMask": "targetGroupId", "targetGroupId": "{{tgId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        # Wiring the TG into the listener is step 5 of the chain — the mutation under test.
        # `must_succeed` states its outcome here; previously a failed wiring only removed
        # the assertion about it, both from the read below AND from the convergence
        # predicate, so the case converged instantly and greened on the miss.
        poll_operation_until_done(must_succeed=True),
        # set-default-tg is an async Operation (polled to done above) — but the PATCH'd
        # defaultTargetGroupId is a 200-but-stale-state read: the field can lag the durable
        # Operation on the read path. Wait for the state to CONVERGE (defaultTargetGroupId ==
        # tgId) before asserting; retry_until_authorized (403/404 only) would run the assert
        # once on a stale 200 and red.
        retry_until_state(Step(name="verify-default-tg-set", method="GET", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('default_target_group_id resolves to attached TG', () => "
                          "  pm.expect(j.defaultTargetGroupId).to.eql(pm.environment.get('tgId')));"]),
             "pm.response.json().defaultTargetGroupId === pm.environment.get('tgId')"),
        # Linkage: LB recomputed to ACTIVE once it has a listener + attached TG
        # (lb_status_recompute trigger). On a bare lane the listener VIP alloc may
        # fail → LB stays INACTIVE; assert the allowed pair. The status assert is already
        # tolerant (ACTIVE|INACTIVE), so only the transient owner-tuple 403/404 needs the
        # read-your-writes retry (no state to converge past the tolerant pair).
        retry_until_authorized(Step(name="get-lb-after-attach", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('LB status ACTIVE or INACTIVE (listener-VIP dependent)', () => "
                          "  pm.expect(j.status).to.be.oneOf(['ACTIVE', 'INACTIVE']));"])),
        # Step 7: computed target states (control-plane, peer-independent). Structural assert
        # (array + valid enum members) → only the transient owner-tuple 403/404 gate needs the
        # bounded read-your-writes retry.
        retry_until_authorized(Step(name="get-target-states", method="GET",
             path=f"{_LB_BASE}/{{{{nlbId}}}}/targetStates?targetGroupId={{{{tgId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "const states = j.targetStates || [];",
                          "pm.test('targetStates is an array', () => pm.expect(states).to.be.an('array'));",
                          "states.forEach(s => pm.test('target status is a valid enum member', () => "
                          f"  pm.expect(s.status).to.be.oneOf({_VALID_TARGET_STATE_JS})));"])),
        # Teardown (bottom-up; clear the listener default before deleting the TG — FK RESTRICT).
        Step(name="clear-default-tg", method="PATCH", path=f"{_LST_BASE}/{{{{lstId}}}}",
             body={"updateMask": "targetGroupId", "targetGroupId": ""},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="remove-instance-target", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"instanceId": "{{existingInstanceId}}"}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_lst(),
        *_cleanup_lb(),
        *_cleanup_tg(),
    ],
))

create_lb_v6 = retry_create_until_present(
    Step(name="create-lb-v6", method="POST", path=_LB_BASE,
         body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
               "placement": "EXTERNAL_REGIONAL", "name": "xres-ext-v6-{{runId}}",
               "v6Source": {"public": {}}},
         test_script=[*assert_status(200),
                      *assert_operation_envelope(prefix_regex="^nlb[a-z0-9]+$"),
                      *save_from_response("j.id", "opId"),
                      *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]))

CASES.append(Case(
    id="XRES-E2E-EXTERNAL-IPV6-VIP",
    title="UC-1 variant: EXTERNAL LB with auto IPv6 VIP — per-family VIP on LoadBalancer "
          "(Verifies 6.0-34 IPv6)",
    classes=["CRUD"], priority="P1",
    steps=[
        # sub-phase 8.1: the per-family VIP moved Listener→LoadBalancer. An IPv6 VIP is now
        # sourced on the LB via v6Source (the Listener no longer carries an address/family;
        # ip_version/allocated_address were reserved out of listener.proto).
        #
        # This case used to gate its only positive assertion on `!lastOpError`, tolerating
        # "the external-v6 pool may be unseeded on a lane". That tolerance made it
        # VACUOUSLY green: seed-nlb-fixtures.sh created kac-nlb-seed-ext-pool with
        # `v6CidrBlocks: []`, so `v6Source:{public:{}}` could NEVER allocate and the
        # assertion never ran (CI 30135586348: 0/1 v6 allocations vs 36/39 v4). The seed
        # now provisions a v6 block in the same EXTERNAL_PUBLIC pool, so the IPv6 VIP is a
        # real, assertable contract — no gate, and a phantom-id guard on the poll so an
        # allocation failure fails HERE instead of leaking a non-existent nlbId downstream.
        create_lb_v6,
        poll_operation_until_done(fixture_ids=["nlbId"], retry_from=create_lb_v6.name,
                                  retry_when="not found|allocation unavailable"),
        retry_until_authorized(Step(name="get-lb-v6-vip", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('type EXTERNAL', () => pm.expect(j.type).to.eql('EXTERNAL'));",
                 "pm.test('auto IPv6 VIP: v6AddressId resolved to bound vpc Address', () => "
                 "  pm.expect(j.v6AddressId).to.match(/^adr[a-z0-9]+$/));",
             ])),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="XRES-E2E-DEFAULT-TG-ABSENT-REJECTED",
    title="UC-1 negative: set listener default_target_group_id to a well-formed ABSENT TG → "
          "sync reject (existence precheck); default stays empty (Verifies 6.0-34/6.0-02)",
    classes=["NEG", "STATE"], priority="P1",
    # NLB-1c removed the M:N attach-pivot + AttachTargetGroup composite-FK: default_target_group_id
    # is now the single authoritative FK-RESTRICT ref, validated by a SYNC existence + same-region
    # precheck (listener/update.go) BEFORE the Operation. So "un-attached TG" is no longer a
    # rejection trigger (an existing same-region TG is accepted without prior attach); the valid
    # negative now is a well-formed but ABSENT TG → rejected (NOT_FOUND / authz-first), never
    # silently applied. Prev premise (un-attached → FAILED_PRECONDITION composite FK) was stale.
    steps=[
        *_create_external_lb("def-absent"),
        retry_until_authorized(Step(name="create-listener", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "def-lst-{{runId}}",
                   "protocol": "TCP", "port": 80},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        # Point default at a well-formed but ABSENT target_group_id → the sync existence precheck
        # rejects it (NOT_FOUND, or authz-first 403 when the scope_extractor cannot resolve the
        # target→project). Never a 200-apply. NOT wrapped in retry (negative — a poll would mask).
        Step(name="set-default-absent", method="PATCH", path=f"{_LST_BASE}/{{{{lstId}}}}",
             body={"updateMask": "targetGroupId", "targetGroupId": "{{garbageTgrId}}"},
             test_script=[
                 "pm.test('absent default TG rejected (never 200-apply)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([400, 403, 404, 409]));",
             ]),
        retry_until_authorized(Step(name="verify-listener-unchanged", method="GET", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('default_target_group_id NOT applied (stays empty)', () => "
                          "  pm.expect(j.defaultTargetGroupId || '').to.eql(''));"])),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="XRES-E2E-V4-LISTENER-V6-ADDRESS-INVALID",
    title="UC-1 negative: the v4 VIP slot of a load balancer pointed at an IPv6 Address → "
          "generic 'Illegal argument addressId' (family/slot mismatch) (Verifies 6.0-34/6.0-20)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[
        # WHERE THE SUBJECT WENT. This case used to POST a listener and call the result a
        # family mismatch — but sub-phase 8.1 moved the VIP off the listener onto the load
        # balancer and RESERVED `ip_version` in CreateListenerRequest, so the body it was
        # sending (loadBalancerId/name/protocol/port) named no address and no
        # family at all. It was an ordinary, VALID listener create, asserted as
        # `oneOf([403, 200, 400])` — every possible answer — followed by a code check
        # written `if (j.error)`, which only ran when an error it never produced appeared.
        # Nothing about families was being tested, and nothing could fail.
        #
        # The invariant itself is alive; it just lives on the LB now. `v4Source.addressId`
        # is family-checked against the linked vpc Address under the caller's identity and
        # a mismatch answers a deliberately GENERIC message — the anti-oracle rule: the
        # caller must not learn from the wording whether the address exists, belongs to
        # somebody else, or is simply the wrong family
        # (services/nlb/internal/apps/kacho/api/loadbalancer/create.go, resolveLinkedAddress).
        # Sync, before any Operation — so there is nothing to poll.
        #
        # `existingAddressIPv6Id` is a deploy precondition of this suite
        # (deploy/scripts/seed-nlb-fixtures.sh step 4b). It is asserted rather than branched
        # on: an unseeded v6 lane must be RED and named here, not quietly downgraded to a
        # case that checks something else.
        Step(name="cr-lb-v4-slot-v6-address", method="POST", path=_LB_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "EXTERNAL_REGIONAL", "name": "xres-v4v6-{{runId}}",
                   "v4Source": {"addressId": "{{existingAddressIPv6Id}}"}},
             test_script=[
                 "pm.environment.set('opId', '');",
                 "pm.test('seeded IPv6 address fixture is present (precondition)', () => "
                 "  pm.expect(pm.environment.get('existingAddressIPv6Id') || '').to.match(/^adr[a-z0-9]+$/));",
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 # Дословно: владелец пишет `Illegal argument addressId`. Приведение
                 # регистра прятало и заглавную `I`, и заглавную `I` в `addressId`.
                 "pm.test('generic anti-oracle wording (no family/ownership disclosure)', () => "
                 "  pm.expect(pm.response.json().message || '', pm.response.text())"
                 "    .to.eql('Illegal argument addressId'));",
             ]),
    ],
))


# ===========================================================================
# 6.0-35 — UC-2: INTERNAL NLB (private VIP from subnet) end-to-end
# ===========================================================================

CASES.append(Case(
    id="XRES-E2E-INTERNAL-FULL-FLOW",
    title="UC-2 INTERNAL NLB: LB(networkId, CLIENT_IP_ONLY, crossZone=false)"
          "→listener(subnet, internal VIP)→TG→attach→GetTargetStates (Verifies 6.0-35)",
    classes=["CRUD", "STATE"], priority="P0",
    steps=[
        # sub-phase 8.1: provision a zonal subnet inline, then create an INTERNAL LB
        # whose VIP is auto-allocated from that subnet (v4Source.subnetId). Gate the
        # rest of the journey on the subnet + LB actually materialising.
        Step(name="provision-subnet", method="POST", path=_VPC_SUBNETS,
             # Run-scoped /24, вырезанный ИЗ ПЛАНА сети посева: разводка прогонов —
             # как прежде (разбор блуждающего флейка — listener.py, шапка
             # `_CIDR_ALLOC_PRE`), но префикс больше не прошит, а читается из
             # опубликованного плана (scripts/gen.py, «АДРЕС НАРЕЗАЕМОЙ ПОДСЕТИ»).
             pre_script=carve_cidr_pre('cross-resource'),
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{existingNetworkId}}",
                   "name": "xres-int-sub-{{runId}}", "ipv4CidrPrimary": "{{_subnetCidr}}",
                   "zoneId": "{{existingZoneId}}"},
             test_script=[
                 "pm.environment.unset('xresSubnetId');",
                 # Suite-created fixture inside the seeded network — there is no lawful lane
                 # on which it is absent, so say so rather than letting every dependent step
                 # fall into a tolerance branch.
                 "pm.test('internal subnet fixture accepted as Operation', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "if (pm.response.code === 200) {",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 "  if (j.metadata && j.metadata.subnetId) pm.environment.set('xresSubnetId', j.metadata.subnetId);",
                 "} else { pm.environment.set('opId', ''); }",
             ]),
        poll_operation_until_done(fixture_ids=["xresSubnetId"]),
        # Прогрев чужого свежего ресурса ДО того, как его идентификатор уедет в
        # асинхронную мутацию nlb: на ней ограниченный повтор ключуется на коде
        # ответа шага, а он всегда `200`+`Operation` (issue #351). Разбор — в
        # шапке `warm_peer_fixture`; свойство держит гейт по дереву.
        warm_peer_fixture(_VPC_SUBNETS, "xresSubnetId", "xres-subnet"),
        # The just-provisioned vpc subnet can be briefly invisible to nlb's vpc peer-read
        # under parallel load → sync create rejects `subnet <id> not found` (400) BEFORE the
        # Operation is minted. Bounded create-retry re-POSTs (leak-free) until the subnet
        # materialises across the service boundary, so the INTERNAL parent LB is real.
        retry_create_until_present(Step(name="create-internal-lb", method="POST", path=_LB_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_ZONAL", "name": "xres-int-{{runId}}",
                   "sessionAffinity": "CLIENT_IP_ONLY",
                   "v4Source": {"subnetId": "{{xresSubnetId}}"}},
             test_script=[
                 "pm.environment.unset('nlbId');",
                 "pm.test('INTERNAL subnet-source LB accepted as Operation', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json();",
                 "if (j.id) pm.environment.set('opId', j.id);",
                 "if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('nlbId', j.metadata.networkLoadBalancerId);",
             ])),
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="get-internal-lb", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "pm.test('Get 200 for created INTERNAL LB', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json();",
                 "pm.test('type INTERNAL', () => pm.expect(j.type).to.eql('INTERNAL'));",
                 "pm.test('placementType ZONAL', () => pm.expect(j.placementType).to.eql('ZONAL'));",
                 "pm.test('sessionAffinity CLIENT_IP_ONLY round-trips', () => "
                 "  pm.expect(j.sessionAffinity).to.eql('CLIENT_IP_ONLY'));",
                 "pm.test('v4AddressId resolved to a bound vpc Address', () => "
                 "  pm.expect(j.v4AddressId).to.match(/^adr[a-z0-9]+$/));",
             ])),
        retry_until_authorized(Step(name="create-internal-listener", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "int-lst-{{runId}}",
                   "protocol": "TCP", "port": 80},
             test_script=[
                 # Child-create under the fresh INTERNAL LB → editor@lb owner-tuple lag (403)
                 # is what the wrapper retries. The parent LB is now asserted to exist
                 # upstream (fixture_ids on its poll), so "rejected" is no longer one of the
                 # outcomes this step is allowed to shrug at: 403/400/404 all mean the chain
                 # this case exists to walk did not happen.
                 "pm.test('internal listener accepted as Operation', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "pm.environment.unset('lstId');",
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.listenerId", "lstId"),
             ])),
        poll_operation_until_done(fixture_ids=["lstId"]),
        # sub-phase 8.1: VIP moved Listener→LoadBalancer; the Listener no longer carries an
        # address. The INTERNAL subnet-sourced VIP is asserted on the LB above
        # (get-internal-lb, v4AddressId → bound vpc Address). Here just confirm the listener
        # reaches ACTIVE once created.
        retry_until_authorized(Step(name="get-internal-listener-status", method="GET", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[
                 "pm.test('status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const j = pm.response.json();",
                 "pm.test('internal listener ACTIVE', () => pm.expect(j.status).to.eql('ACTIVE'));",
             ])),
        *_create_tg("int-flow"),
        # GetTargetStates is a per-TG query (same-project + viewer) — it needs neither an
        # attach nor a listener wiring (attach/detach RPCs removed).
        # The `xresIntReady === '1'` flag this used to require was set by the very branch that
        # is now unconditional, so the whole query only ran on a lane where nothing had gone
        # wrong — i.e. never on the lanes worth reporting.
        Step(name="get-internal-target-states", method="GET",
             path=f"{_LB_BASE}/{{{{nlbId}}}}/targetStates?targetGroupId={{{{tgId}}}}",
             test_script=[
                 "pm.test('GetTargetStates 200 on ready INTERNAL LB', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const states = pm.response.json().targetStates || [];",
                 "pm.test('targetStates is an array', () => pm.expect(states).to.be.an('array'));",
             ]),
        # Teardown (guarded best-effort).
        Step(name="cleanup-int-listener", method="DELETE", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_lb(),
        *_cleanup_tg(),
        Step(name="cleanup-int-subnet", method="DELETE", path=f"{_VPC_SUBNETS}/{{{{xresSubnetId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="XRES-E2E-INTERNAL-NO-NETWORK-INVALID",
    title="INTERNAL_ZONAL LB with no VIP source → InvalidArgument "
          "(8.1 replaces the old network_id requirement) (Verifies 8.1-19)",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        Step(name="create-internal-bare", method="POST", path=_LB_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "INTERNAL_ZONAL", "name": "int-bare-{{runId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('rejected for missing placement or missing vip source', () => {",
                          "  const m = (pm.response.json().message || '').toLowerCase();",
                          "  pm.expect(m).to.satisfy(s => s.includes('placement_type') || s.includes('vip source'));",
                          "});"]),
    ],
))

CASES.append(Case(
    id="XRES-E2E-EXTERNAL-NO-NETWORK-FIELD",
    title="EXTERNAL LB with a public VIP source binds no network — the removed networkId "
          "is absent from the projection (Verifies 8.1-32)",
    classes=["CRUD", "CONF"], priority="P1",
    # The body used to carry `networkId` (removed from CreateNetworkLoadBalancerRequest in
    # the VIP redesign) to demonstrate that the edge discards it and still returns 200. That
    # asserted an edge JSON policy, not an nlb contract, and it is the exact shape
    # TestNewmanCollectionsSendNoUnknownRequestFields exists to eliminate — a key the server
    # does not read, answered `200`, indistinguishable from one that was applied. The
    # request-side statement is now proven statically for every collection in the tree; what
    # remains here, and is asserted unchanged, is the observable: an EXTERNAL public-VIP LB
    # carries no network binding in its projection.
    steps=[
        Step(name="create-external-public-vip", method="POST", path=_LB_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "EXTERNAL_REGIONAL", "name": "ext-net-{{runId}}",
                   "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        # A phantom LB (Operation done WITH "could not allocate load balancer address") used
        # to be absorbed by the `!lastOpError` guard below, which left this case with zero
        # executed assertions on exactly the lane where something was wrong. The allocation
        # is now stated here — the same disposition XRES-E2E-EXTERNAL-IPV6-VIP already took.
        poll_operation_until_done(fixture_ids=["nlbId"]),
        retry_until_authorized(Step(name="get-no-network", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "pm.test('status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "pm.test('projection carries no networkId (removed from the model)', () => "
                 "  pm.expect(pm.response.json()).to.not.have.property('networkId'));",
             ])),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="XRES-E2E-INTERNAL-SG-FOREIGN-REJECTED",
    title="EXTERNAL LB carrying securityGroupIds → sync 400 INVALID_ARGUMENT "
          "'security_group_ids is only valid for INTERNAL load balancer' (Verifies NLB-1-51/52)",
    classes=["NEG", "VAL", "CONF"], priority="P2",
    # NLB-1b revived securityGroupIds as a LIVE NetworkLoadBalancer field (VIP firewall,
    # NLB-1-51), valid ONLY on INTERNAL (subnet-sourced) LBs — sg_validate.go rejects it on an
    # EXTERNAL public-VIP LB with a sync InvalidArgument BEFORE the Operation. The previous suite
    # premise ("securityGroupIds removed in 8.1, silently ignored → 200") was stale: the field is
    # not removed, it is INTERNAL-scoped and validated. Reject fires before SG existence is peer-
    # checked, so garbage vs real SG id is immaterial here.
    steps=[
        Step(name="create-with-removed-sg", method="POST", path=_LB_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "placement": "EXTERNAL_REGIONAL", "name": "ext-sg-{{runId}}",
                   "securityGroupIds": ["{{garbageSecurityGroupId}}"], "v4Source": {"public": {}}},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          # Дословно: владелец пишет вид ЗАГЛАВНЫМИ (`only valid for INTERNAL`),
                          # и приведение регистра не различало бы `INTERNAL` от `internal`.
                          "pm.test('securityGroupIds rejected on EXTERNAL (INTERNAL-only field)', () => "
                          "  pm.expect(pm.response.json().message || '', pm.response.text())"
                          "    .to.eql('security_group_ids is only valid for INTERNAL load balancer'));"]),
    ],
))


# ===========================================================================
# 6.0-36 — UC-5: bottom-up teardown with correct address lifecycle
# ===========================================================================

CASES.append(Case(
    id="XRES-E2E-TEARDOWN-BOTTOM-UP",
    title="UC-5 bottom-up teardown: clear default → remove target → detach → delete "
          "listener (frees VIP) → delete LB → delete TG; final 404s (Verifies 6.0-36)",
    classes=["CRUD", "STATE"], priority="P0",
    steps=[
        # Build a minimal EXTERNAL stack with an external_ip target (peer-free, so
        # the drain step is deterministic regardless of Compute fixtures).
        *_create_external_lb("teardown"),
        retry_until_authorized(Step(name="create-listener", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "td-lst-{{runId}}",
                   "protocol": "TCP", "port": 80},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        *_create_tg("teardown"),
        retry_until_authorized(Step(name="add-external-target", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.200"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # Wire the TG into the listener (default_target_group_id FK) — the listener is now
        # the sole TG↔LB link (attach/detach RPCs removed).
        Step(name="set-default-tg", method="PATCH", path=f"{_LST_BASE}/{{{{lstId}}}}",
             body={"updateMask": "targetGroupId", "targetGroupId": "{{tgId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Step 1: delete LB while it still owns a listener → rejected ("not empty").
        # Refused SYNCHRONOUSLY (loadbalancer/delete.go precheck -> FAILED_PRECONDITION ->
        # HTTP 400). Accepting 200 would have let the teardown journey pass with its first
        # ordering constraint gone, and the follow-up `if (j.error)` made the code assertion
        # conditional on the refusal it was supposed to establish.
        Step(name="delete-lb-not-empty", method="DELETE", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "pm.environment.set('opId', '');",
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 "pm.test('says the load balancer still owns listeners', () => "
                 "  pm.expect((pm.response.json().message || '')).to.match("
                 "    /NetworkLoadBalancer .+ has listener\\(s\\); delete first/));",
             ]),
        Step(name="lb-still-exists", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('LB survived the rejected delete', () => "
                          "  pm.expect(pm.response.json().id).to.eql(pm.environment.get('nlbId')));"]),
        # Step 2: clear listener default (composite FK must be released first).
        Step(name="clear-default", method="PATCH", path=f"{_LST_BASE}/{{{{lstId}}}}",
             body={"updateMask": "targetGroupId", "targetGroupId": ""},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Step 3: drain the target (2-phase RemoveTargets, peer-independent).
        Step(name="remove-target", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.200"}}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Step 4: delete listener → auto VIP returned via FreeIP (vip_origin=auto).
        Step(name="delete-listener", method="DELETE", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="listener-gone", method="GET", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        # Step 5: delete LB → now empty → succeeds.
        Step(name="delete-lb-empty", method="DELETE", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="lb-gone", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        # Step 6: delete TG (no listener reference, drained) → succeeds.
        # THE PAYOFF of the whole journey: every ordering constraint has been satisfied, so
        # the delete must go through. Accepting a refusal (`oneOf([200, 400, 409])`) meant
        # the teardown could fail at its last step and the case would still report that
        # bottom-up teardown works.
        Step(name="delete-tg", method="DELETE", path=f"{_TG_BASE}/{{{{tgId}}}}",
             test_script=[
                 "pm.test('TG delete accepted once drained and unreferenced', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(must_succeed=True),
        Step(name="tg-gone", method="GET", path=f"{_TG_BASE}/{{{{tgId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

CASES.append(Case(
    id="XRES-E2E-DELETE-LB-NOT-EMPTY-FP",
    title="UC-5 negative: Delete LB that still owns a listener → FAILED_PRECONDITION "
          "'NetworkLoadBalancer <id> has listener(s); delete first' (Verifies 6.0-36 step 1)",
    classes=["NEG", "STATE"], priority="P0",
    steps=[
        *_create_external_lb("del-notempty"),
        retry_until_authorized(Step(name="create-listener", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "ne-lst-{{runId}}",
                   "protocol": "TCP", "port": 80},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        # THE ONE THING THIS CASE IS FOR. The guard is a SYNC precheck — Delete asks
        # whether the LB still has listeners and refuses FAILED_PRECONDITION before an
        # Operation is minted (loadbalancer/delete.go, "Sync prechecks"), which
        # grpc-gateway renders as HTTP 400. There is nothing to poll afterwards.
        #
        # What stood here accepted 200 — the delete going THROUGH — as a pass, and BOTH
        # statements about FAILED_PRECONDITION were conditional on a refusal having already
        # happened, so removing the guard entirely left the case green. A P0 negative whose
        # only subject is a guard cannot be satisfied by that guard being absent. The
        # message is pinned too: the product says which resource blocks and why, and that
        # wording is contract (the title's old paraphrase "load balancer is not empty" was
        # never the text the service returns).
        Step(name="delete-blocked", method="DELETE", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[
                 "pm.environment.set('opId', '');",
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 "pm.test('says the load balancer still owns listeners', () => "
                 "  pm.expect((pm.response.json().message || '')).to.match("
                 "    /NetworkLoadBalancer .+ has listener\\(s\\); delete first/));",
             ]),
        Step(name="lb-survived-the-refusal", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('LB still exists after the refused delete', () => "
                          "  pm.expect(pm.response.json().id).to.eql(pm.environment.get('nlbId')));"]),
        # Cleanup in the lawful order: listener first, then LB.
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))


# ===========================================================================
# 6.0-37 — dangling cross-service instance target survives on read (by-design)
# ===========================================================================

CASES.append(Case(
    id="XRES-DANGLING-INSTANCE-READ-GRACEFUL",
    title="Dangling instance-target: TargetGroup.Get / GetTargetStates survive a "
          "target referencing a (potentially-deleted) Compute Instance without "
          "panic; RemoveTargets drains it peer-independently (Verifies 6.0-37)",
    classes=["STATE", "CRUD"], priority="P0",
    steps=[
        # The nlb read paths (TargetGroup.Get / List / GetTargetStates) are an
        # output-only mirror — source of truth = compute.Instance — and never
        # re-resolve the peer on read (verified: no compute client call in the
        # read use-cases). Reading a TG that holds an instance target therefore
        # exercises the identical code path a dangling reference would hit: the
        # control plane cannot tell a live instance from a deleted one, which IS
        # the graceful-degradation property required by data-integrity.md §4.
        *_create_external_lb("dangling"),
        *_create_tg("dangling"),
        retry_until_authorized(Step(name="add-instance-target", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"instanceId": "{{existingInstanceId}}", "weight": 100}]},
             test_script=[*assert_status(200),
                          *assert_operation_envelope(prefix_regex="^nlb[a-z0-9]+$"),
                          *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # Get(TG) survives: 200, not 404/500, even though the referenced peer is
        # not re-validated here.
        Step(name="get-tg-survives", method="GET", path=f"{_TG_BASE}/{{{{tgId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('TG read survives (not 404/500)', () => "
                          "  pm.expect(j.id).to.eql(pm.environment.get('tgId')));",
                          "pm.test('targets is an array (degraded mirror, not crash)', () => "
                          "  pm.expect(j.targets || []).to.be.an('array'));"]),
        # GetTargetStates is a deterministic, peer-independent computation: it must
        # return 200 and a valid enum status for every stored target — a dangling
        # instance ref cannot turn it into a 500.
        Step(name="get-states-survives", method="GET",
             path=f"{_LB_BASE}/{{{{nlbId}}}}/targetStates?targetGroupId={{{{tgId}}}}",
             test_script=[*assert_status(200),
                          "const states = pm.response.json().targetStates || [];",
                          "pm.test('targetStates is an array', () => pm.expect(states).to.be.an('array'));",
                          "states.forEach(s => pm.test('computed status is a valid enum member', () => "
                          f"  pm.expect(s.status).to.be.oneOf({_VALID_TARGET_STATE_JS})));"]),
        # RemoveTargets resolves by stored identity tuple (no compute call) → it
        # drains a target whose peer may no longer exist.
        Step(name="remove-instance-target", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"instanceId": "{{existingInstanceId}}"}]},
             test_script=[*assert_status(200),
                          *assert_operation_envelope(prefix_regex="^nlb[a-z0-9]+$"),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="XRES-DANGLING-GTS-UNKNOWN-TG-NOTFOUND",
    title="Dangling negative: GetTargetStates for a well-formed but absent "
          "target_group_id → NOT_FOUND (dangling-target tolerance ≠ tolerating a "
          "missing TargetGroup) (Verifies 6.0-37 boundary)",
    classes=["NEG"], priority="P1",
    steps=[
        *_create_external_lb("dangling-neg"),
        # A garbage/absent target_group_id must be REJECTED (missing TargetGroup ≠ dangling
        # target). The gateway scope_extractor cannot resolve garbageTgr->project (anti-BOLA)
        # so it fail-closes 403 authz-first, or the backend repo.Get returns 404 — both are a
        # no-leak reject (never 200 with silently-empty states). Tolerant 400/403/404.
        Step(name="get-states-unknown-tg", method="GET",
             path=f"{_LB_BASE}/{{{{nlbId}}}}/targetStates?targetGroupId={{{{garbageTgrId}}}}",
             test_script=[*assert_absent_id_rejected()]),
        *_cleanup_lb(),
    ],
))
