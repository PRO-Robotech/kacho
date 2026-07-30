# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""ListenerService cases (LST-*).

Acceptance: docs/specs/sub-phase-4.0-nlb-acceptance.md §4 (GWT-LST-001..026).
Design: 2026-05-23-kacho-nlb-design.md §3.3, §4.5 (VIP auto + BYO + compensation).

REST: /nlb/v1/listeners
"""

CASES = []

_LST_BASE = "/nlb/v1/listeners"
_LB_BASE = "/nlb/v1/networkLoadBalancers"
_VPC_SUBNETS = "/vpc/v1/subnets"

# Run-scoped, HIGH-ENTROPY /24 allocator for the per-case INTERNAL subnet.
#
# ROOT CAUSE it fixes (wandering e2e flake): the previous formula was
# `10.{200 + hash(runId)%40}.{_cidrSeq}.0/24` — only 40 possible second-octet values
# (10.200-239.x) shared with load-balancer/cross-resource/authz-deny, and `_cidrSeq`
# RESTARTS at 1 in every newman process (each collection is its own process; runId is
# NOT persisted back to the env file). These subnets are created in the SHARED seeded
# `existingNetworkId` and were NEVER cleaned up, so they accumulate permanently. Any new
# run whose `hash(runId)%40` landed on an already-saturated band collided at oct3=1,2,3…
# → vpc `Subnet CIDRs can not overlap` (FailedPrecondition) on `setup-subnet` → the whole
# INTERNAL LB chain cascaded (nlbId unset → child listener Create `invalid resource id
# '{{nlbId}}'` → 18-assertion red). Which runId-hash landed on a saturated band drifted
# run-to-run → the flake "wandered".
#
# The FIX spreads allocation across ~56k distinct /24s with a run-random base for BOTH
# octets (not seq-from-1), so distinct runs land in distinct /24s and collision with
# subnets leaked by prior/concurrent runs is improbable rather than guaranteed. Paired
# with best-effort subnet reclaim in _cleanup_lb() (bounds accumulation). Java-style
# 31-bit string hash (`(h<<5)-h` kept 32-bit via `|0`) — no Math.imul, newman-sandbox-safe.
_CIDR_ALLOC_PRE = [
    "var __seq = parseInt(pm.environment.get('_cidrSeq') || '0', 10) + 1;",
    "pm.environment.set('_cidrSeq', String(__seq));",
    "var __run = (pm.environment.get('runId') || 'x0');",
    "var __h = 0; for (var i = 0; i < __run.length; i++) { __h = ((__h << 5) - __h + __run.charCodeAt(i)) | 0; }",
    "__h = __h & 0x7fffffff;",
    # oct2 ∈ [16,235] (220 run-scoped values); oct3 = run-random base (high bits) + seq
    # (separates subnets within one run). ~56k distinct /24 vs the old 40×{1..30} band.
    "var __oct2 = 16 + (__h % 220);",
    "var __oct3 = ((Math.floor(__h / 256) % 256) + __seq) % 256;",
    "pm.environment.set('_subnetCidr', '10.' + __oct2 + '.' + __oct3 + '.0/24');",
]

# NOTE (sub-phase 8.1 VIP model): the parent LoadBalancer now carries a per-family
# VIP *source* on Create (v4Source public/subnet/address). This helper produces a
# valid new-model parent LB (EXTERNAL → auto public VIP; INTERNAL → auto VIP from an
# inline zonal subnet) so the Listener cases have a lawful LB to attach to. The
# Listener-level fields exercised below (subnetId / addressId / ipVersion /
# allocatedAddress) still follow the sub-phase-4.0 listener contract — the 8.1
# acceptance covers only the LoadBalancer resource, so per-listener VIP semantics
# are out of scope here and tracked for a separate listener acceptance/rewrite.


def _setup_lb(name_suffix: str, lb_type: str = "INTERNAL"):
    # Default parent LB is INTERNAL (subnet-backed VIP from a per-case inline /24) rather
    # than EXTERNAL auto-public-VIP: the shared external AddressPool is contended across the
    # `--jobs 4` parallel collections and exhausts mid-run ("could not allocate load balancer
    # address"), leaving an EXTERNAL parent a PHANTOM LB whose owner-tuple never materialises
    # -> the child Listener.Create then 403s (editor@lb) and every listener CRUD reds in a
    # cascade. An INTERNAL parent is pool-independent, so the child listener flow actually
    # runs. Cases whose semantics REQUIRE an external parent (BYO external address) pass
    # lb_type="EXTERNAL" explicitly.
    if lb_type == "INTERNAL":
        # cross-service read-your-writes: the just-provisioned subnet can be briefly
        # invisible to nlb's vpc peer-read under parallel load -> `subnet <id> not found`.
        # Bounded create-retry re-POSTs (leak-free) until the subnet materialises, so the
        # parent INTERNAL LB is real (not phantom) before the child listener flow.
        #
        # The SAME window can land on the WORKER instead of the sync precheck: the
        # subnet is visible to `SubnetService.Get` (precheck passes, 200 + Operation)
        # but still stale to the worker's own vpc address-allocate, which then fails
        # the Operation. The create-step wrapper cannot see that — hence the
        # `retry_from` re-drive on the poll below, plus `fixture_ids` so a spent
        # budget FAILS here instead of publishing the pre-allocated (phantom) LB id
        # into `nlbId` and cascading into unrelated 403/404 on the child listener.
        setup_lb = retry_create_until_present(Step(name="setup-lb", method="POST", path=_LB_BASE,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": f"lst-{name_suffix}-{{{{runId}}}}", "placement": "INTERNAL_ZONAL", "v4Source": {"subnetId": "{{lstSubnetId}}"}},
             test_script=[
                 "pm.environment.unset('nlbId');",
                 "if (pm.environment.get('lstSubnetId')) {",
                 "  pm.test('parent INTERNAL LB created', () => pm.expect(pm.response.code).to.eql(200));",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 "  if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('nlbId', j.metadata.networkLoadBalancerId);",
                 "} else { pm.environment.unset('opId'); }",
             ]))
        return [
            Step(name="setup-subnet", method="POST", path=_VPC_SUBNETS,
                 pre_script=_CIDR_ALLOC_PRE,
                 body={"projectId": "{{_suiteProjectId}}", "networkId": "{{existingNetworkId}}",
                       "name": f"lst-sub-{name_suffix}-{{{{runId}}}}", "ipv4CidrPrimary": "{{_subnetCidr}}",
                       "zoneId": "{{existingZoneId}}"},
                 test_script=[
                     "pm.environment.unset('lstSubnetId');",
                     "if (pm.response.code === 200) {",
                     "  const j = pm.response.json();",
                     "  if (j.id) pm.environment.set('opId', j.id);",
                     "  if (j.metadata && j.metadata.subnetId) pm.environment.set('lstSubnetId', j.metadata.subnetId);",
                     "} else { pm.environment.unset('opId'); }",
                 ]),
            poll_operation_until_done(fixture_ids=["lstSubnetId"]),
            setup_lb,
            # `allocation unavailable` is the OTHER transient async-lane text: nlb now
            # bounded-retries a hide-existence deny on its OWN freshly-allocated vpc
            # Address (the per-object owner-tuple is eventually consistent) and, if that
            # window still has not closed, answers UNAVAILABLE "load balancer address
            # allocation unavailable" instead of the old, factually WRONG capacity text.
            # Both retried texts are peer-transient; the capacity refusal
            # ("could not allocate load balancer address") reads differently and is
            # still NEVER re-driven.
            poll_operation_until_done(fixture_ids=["nlbId"], retry_from=setup_lb.name,
                                      retry_when="not found|allocation unavailable"),
            # read-your-writes: materialize the PARENT LB owner-tuple before the child
            # Listener.Create (which is authorized against editor@nlb_network_load_balancer)
            # -> avoids a spurious 403 on the fresh LB whose tuple is eventually-consistent.
            retry_until_authorized(Step(name="setup-materialize-lb", method="GET",
                 path=f"{_LB_BASE}/{{{{nlbId}}}}", test_script=[])),
        ]
    # EXTERNAL_LINKED — parent LB whose VIP is a tenant-owned (BYO) Address linked via
    # `v4Source.addressId`. BYO addressing lives on the LoadBalancer: the Listener has no
    # address of its own (see LST-CR-CRUD-BYO), so the only place a BYO binding can be
    # exercised — and asserted — is the parent.
    v4_source = {"addressId": "{{existingAddressId}}"} if lb_type == "EXTERNAL_LINKED" else {"public": {}}
    return [
        # EXTERNAL parent provisions NO subnet — clear any stale lstSubnetId carried over
        # from a prior INTERNAL case so the best-effort subnet reclaim in _cleanup_lb() is a
        # no-op here (it must not delete another case's subnet).
        Step(name="setup-lb", method="POST", path=_LB_BASE,
             pre_script=["pm.environment.unset('lstSubnetId');"],
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": f"lst-{name_suffix}-{{{{runId}}}}", "placement": "EXTERNAL_REGIONAL",
                   "v4Source": v4_source},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        # No async re-drive here: the EXTERNAL lane draws from the shared public
        # AddressPool, whose exhaustion is a genuine capacity refusal (opaque text)
        # and must NOT be retried away. `fixture_ids` still applies — a failed
        # allocation must fail HERE, not publish the pre-allocated (phantom) LB id.
        poll_operation_until_done(fixture_ids=["nlbId"]),
        # read-your-writes: materialize the PARENT LB owner-tuple before Listener.Create.
        retry_until_authorized(Step(name="setup-materialize-lb", method="GET",
             path=f"{_LB_BASE}/{{{{nlbId}}}}", test_script=[])),
    ]


def _cleanup_lb():
    return [
        Step(name="cleanup-lb", method="DELETE", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Best-effort reclaim of the per-case INTERNAL subnet (lstSubnetId), now that its only
        # referrer — the parent LB whose VIP was allocated from it — is deleted (VIP recycled on
        # LB delete → subnet becomes empty → deletable). Leaking it accumulated subnets in the
        # shared network and exhausted the /24 space → 'Subnet CIDRs can not overlap' on later
        # runs (the wandering flake this suite fixes). Guarded + fully tolerant: when no subnet
        # was provisioned this case (EXTERNAL setup unset lstSubnetId), the DELETE hits the
        # collection path and 4xx's harmlessly (opId cleared → the poll no-ops); a transient
        # 'subnet not empty' during VIP free-lag is also tolerated (residual leak covered by the
        # widened CIDR entropy). It NEVER fails the case.
        Step(name="cleanup-lst-subnet", method="DELETE", path=f"{_VPC_SUBNETS}/{{{{lstSubnetId}}}}",
             test_script=[
                 "pm.test('subnet reclaim best-effort (never fails the case)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 400, 403, 404, 405, 409]));",
                 "pm.environment.unset('opId');",
                 "if (pm.response.code === 200) { try { const j = pm.response.json();"
                 " if (j.id) pm.environment.set('opId', j.id); } catch (e) {} }",
                 "pm.environment.unset('lstSubnetId');",
             ]),
        poll_operation_until_done(),
    ]


def _cleanup_lst():
    return [
        Step(name="cleanup-lst", method="DELETE", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


# ---------------------------------------------------------------------------
# CRUD
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="LST-CR-CRUD-AUTO-VIP",
    title="Create EXTERNAL Listener with auto VIP allocation (Verifies REQ-LST-CR-AUTO-VIP)",
    classes=["CRUD"], priority="P0",
    steps=[
        # Exercises EXTERNAL auto-public-VIP listener semantics → parent must be EXTERNAL.
        # Pool-dependent (external AddressPool) — tracked under the systemic external-pool finding.
        *_setup_lb("auto-vip", lb_type="EXTERNAL"),
        # Child Listener.Create is authorized against editor@nlb_network_load_balancer,
        # whose owner-tuple is eventually-consistent after the parent LB Create. round-4
        # wrapped the setup GET/UPDATE/DELETE but left the child-CREATE unwrapped, so a
        # transient 403 reddened the whole listener CRUD chain — wrap it too (fail-closed).
        retry_until_authorized(Step(name="cr-lst", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "http-{{runId}}",
                   "protocol": "TCP", "port": 80, "targetPort": 8080,
                   "proxyProtocolV2": False},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        # status ACTIVE is a 200-but-stale-state read: it settles after the create Operation
        # is durable, so wait for it to CONVERGE (not just for the owner-tuple 403/404 gate) —
        # retry_until_authorized would assert once on a transient pre-ACTIVE 200 and red.
        retry_until_state(Step(name="get", method="GET", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          # sub-phase 8.1: the per-family VIP moved Listener->LoadBalancer; the
                          # Listener no longer carries an address (allocated_address / ip_version
                          # reserved 12-15 in listener.proto). The listener reaches ACTIVE; the
                          # EXTERNAL auto public VIP is asserted on the parent LB below
                          # (v4AddressId output -> bound vpc Address).
                          "pm.test('status ACTIVE', () => pm.expect(j.status).to.eql('ACTIVE'));"]),
             "pm.response.json().status === 'ACTIVE'"),
        # The EXTERNAL auto public VIP (v4AddressId) resolves onto the LB as the VIP binds;
        # wait for it to CONVERGE to a bound vpc Address rather than asserting once on a
        # transient empty-VIP 200.
        retry_until_state(Step(name="get-lb-vip", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('EXTERNAL parent auto public VIP: v4AddressId resolved to bound vpc Address', () => "
                          "  pm.expect(j.v4AddressId).to.match(/^adr[a-z0-9]+$/));"]),
             "/^adr[a-z0-9]+$/.test(pm.response.json().v4AddressId || '')"),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))

# BYO (tenant-owned Address) is a LOAD-BALANCER-level binding: the VIP is anchored on
# the LB (`v4Source.addressId` → `v4AddressId°`) and the Listener is a (port, protocol)
# on it, carrying no address of its own. This case pins BOTH halves of that contract on
# a live BYO parent.
#
# It used to send `ipVersion`/`addressId` in the LISTENER body — keys the gateway
# silently drops (removed from listener.proto, reserved 12-15) — and then asserted
# `addressId matches BYO` INSIDE `if (pm.response.json().addressId)`, a field that never
# comes back. The guard was never entered, so the assertion never ran: vacuously green.
CASES.append(Case(
    id="LST-CR-CRUD-BYO",
    title="Listener on a BYO-address parent LB: VIP binds on the LB, listener carries no address "
          "(Verifies REQ-LST-CR-BYO)",
    classes=["CRUD"], priority="P0",
    steps=[
        # BYO external address → parent must be EXTERNAL and LINK it (address kind must
        # match the LB scheme). Pool-independent: the address is pre-seeded, not drawn
        # from the contended external AddressPool.
        *_setup_lb("byo", lb_type="EXTERNAL_LINKED"),
        retry_until_authorized(Step(name="cr-byo", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "byo-{{runId}}",
                   "protocol": "TCP", "port": 80, "targetPort": 8080},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        # (a) the Listener projection carries NO address fields — they are not part of the
        # contract, and a request that sends them changes nothing.
        retry_until_authorized(Step(name="get-byo", method="GET", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('listener carries no address fields (VIP lives on the LB)', () => {",
                          "  pm.expect(j).to.not.have.property('addressId');",
                          "  pm.expect(j).to.not.have.property('allocatedAddress');",
                          "  pm.expect(j).to.not.have.property('ipVersion');",
                          "  pm.expect(j).to.not.have.property('subnetId');",
                          "});"])),
        # (b) the BYO binding IS observable — on the parent LB, and it is exactly the
        # address the tenant linked (this is the assertion the old case never ran).
        retry_until_state(Step(name="get-lb-byo-vip", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('parent LB v4AddressId is the linked BYO address', () => "
                          "  pm.expect(j.v4AddressId).to.eql(pm.environment.get('existingAddressId')));"]),
             "!!pm.response.json().v4AddressId"),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-CRUD-INTERNAL",
    title="Create INTERNAL Listener with subnet_id (Verifies REQ-LST-CR-INTERNAL)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_setup_lb("int", lb_type="INTERNAL"),
        retry_until_authorized(Step(name="cr-int", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "int-{{runId}}",
                   "protocol": "TCP", "port": 80, "targetPort": 8080},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-GET-CRUD-OK",
    title="Get existing Listener returns full message",
    classes=["CRUD"], priority="P0",
    steps=[
        *_setup_lb("get-ok"),
        retry_until_authorized(Step(name="cr", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "getok-{{runId}}",
                   "protocol": "TCP", "port": 81, "targetPort": 8081},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          # grpc-gateway serialises the int32 port as a JSON string ('81');
                          # Number()-coerce so the assertion is transport-encoding-agnostic.
                          "pm.test('port matches', () => pm.expect(Number(j.port)).to.eql(81));",
                          "pm.test('protocol matches', () => pm.expect(j.protocol).to.eql('TCP'));"])),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-LST-CRUD-OK",
    title="List Listeners by load_balancer_id",
    classes=["CRUD", "LSG"], priority="P1",
    steps=[
        *_setup_lb("list"),
        Step(name="lst", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&loadBalancerId={{{{nlbId}}}}&pageSize=10",
             test_script=[*assert_status(200),
                          "pm.test('listeners array', () => "
                          "  pm.expect(pm.response.json().listeners || pm.response.json().items || []).to.be.an('array'));"]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-UPD-CRUD-OK",
    title="Update Listener mutable fields (name, proxy_protocol_v2)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_setup_lb("upd-ok"),
        retry_until_authorized(Step(name="cr", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "upd-{{runId}}",
                   "protocol": "TCP", "port": 82, "targetPort": 8082},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        # FieldMask JSON paths are lowerCamelCase (proto3 JSON); a snake_case path
        # (proxy_protocol_v2) is rejected sync as InvalidArgument "FieldMask.paths
        # contains invalid path". Mutable listener fields per UpdateListenerRequest:
        # name/description/labels/proxyProtocolV2/defaultTargetGroupId.
        retry_until_authorized(Step(name="upd", method="PATCH", path=f"{_LST_BASE}/{{{{lstId}}}}",
             body={"updateMask": "name,proxyProtocolV2",
                   "name": "https-{{runId}}", "proxyProtocolV2": True},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))

# Deleting a Listener releases NO address: the VIP is the parent LoadBalancer's and is
# released by ITS delete / compensation / free_ip_runner. The two former cases here
# (`*-DEL-CRUD-AUTO-VIP-FREE` / `*-DEL-CRUD-BYO-CLEAR-REF`) claimed to exercise a
# listener-level FreeIP-vs-ClearReference branch that the service never had — a Listener
# has no address_id, so that branch was unreachable and has been removed. Merged into one
# case that asserts the property that IS real.
CASES.append(Case(
    id="LST-DEL-CRUD-OK",
    title="Delete Listener — parent LB keeps its VIP (release belongs to the LoadBalancer)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_setup_lb("del-ok", lb_type="EXTERNAL"),
        retry_until_authorized(Step(name="cr", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "del-ok-{{runId}}",
                   "protocol": "TCP", "port": 83, "targetPort": 8083},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        # Pin the parent VIP BEFORE the delete so the post-delete assertion compares
        # against an observed value, not against "something non-empty".
        retry_until_state(Step(name="get-lb-vip-before", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "pm.environment.set('_lbVipBefore', pm.response.json().v4AddressId || '');"]),
             "!!pm.response.json().v4AddressId"),
        retry_until_authorized(Step(name="del", method="DELETE", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="get-lb-vip-after", method="GET", path=f"{_LB_BASE}/{{{{nlbId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('parent LB VIP survives listener delete', () => "
                          "  pm.expect(pm.response.json().v4AddressId).to.eql(pm.environment.get('_lbVipBefore')));"]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-LOPS-CRUD-OK",
    title="ListOperations for Listener returns history",
    classes=["CRUD", "LSG"], priority="P2",
    steps=[
        *_setup_lb("lops"),
        retry_until_authorized(Step(name="cr", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "lops-{{runId}}",
                   "protocol": "TCP", "port": 85, "targetPort": 8085},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="lops", method="GET",
             path=f"{_LST_BASE}/{{{{lstId}}}}/operations?pageSize=10",
             test_script=[*assert_status(200),
                          "const ops = (pm.response.json().operations || pm.response.json().items || []);",
                          "pm.test('at least 1 op', () => pm.expect(ops.length).to.be.at.least(1));"])),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))


# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="LST-CR-VAL-PORT-ZERO",
    title="Create Listener with port=0 → InvalidArgument 'port must be in [1, 65535]'",
    classes=["VAL", "BVA"], priority="P1",
    steps=[
        *_setup_lb("port-0"),
        # Even a sync-validation NEGATIVE goes through the gateway editor@lb Check first, so a
        # fresh-LB owner-tuple lag can pre-empt it with a 403 before the port=0 InvalidArgument
        # is ever produced. Wrapping retries ONLY that transient 403/404 — the real 400 assertion
        # still runs (fail-closed on a terminal 403), so the negative is not masked or weakened.
        retry_until_authorized(Step(name="cr-p0", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "p0-{{runId}}",
                   "protocol": "TCP", "port": 0, "targetPort": 8080},
             # Product IS correct: Listener.Create sync-validates port=0 (LbPortFromProto
             # → InvalidArgument "port must be in range [1, 65535]"), but the gateway
             # editor@lb authz gate runs first, so a genuine owner-tuple lag on a REAL
             # parent can pre-empt the 400 with a transient 403 — that is what the wrap
             # absorbs. A phantom parent is a different thing entirely and is no longer
             # this step's problem: `_setup_lb` polls with `fixture_ids=["nlbId"]`, so a
             # failed VIP allocation fails THERE. The `if (!lastOpError)` that used to sit
             # here forgave it instead, and with it the port=0 statement — the only
             # assertion in the case — simply did not run. Its sibling LST-CR-VAL-PORT-OVER
             # has always asserted the same 400 unwrapped.
             test_script=[
                 "pm.test('status 400', () => pm.expect(pm.response.code, pm.response.text()).to.eql(400));",
                 "pm.test('grpc code 3 (INVALID_ARGUMENT)', () => { const j = pm.response.json();"
                 " pm.expect(j.code, JSON.stringify(j)).to.eql(3); });",
             ])),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-VAL-PORT-OVER",
    title="Create Listener with port=65536 → InvalidArgument",
    classes=["VAL", "BVA"], priority="P1",
    steps=[
        *_setup_lb("port-over"),
        retry_until_authorized(Step(name="cr-po", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "po-{{runId}}",
                   "protocol": "TCP", "port": 65536, "targetPort": 8080},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-VAL-PORT-NEGATIVE",
    title="Create Listener with port=-1 → InvalidArgument",
    classes=["VAL", "BVA"], priority="P2",
    steps=[
        *_setup_lb("port-neg"),
        # SYNC-VALIDATE lane, identical to the two siblings above: -1 survives the
        # int32-overflow narrowing (domain.LbPortFromProto) and is then refused by
        # LbPort.Validate inside listener.Validate() — create.go:134/160, before the
        # Operation is minted. So the outcome is the same 400 / grpc 3 that
        # LST-CR-VAL-PORT-ZERO and -PORT-OVER already assert on this very code path;
        # there was never a reason for this one to be the tolerant member of the trio.
        # The `200` it used to accept was the acceptance the title calls invalid, and
        # the `403` is what the retry wrapper absorbs — asserting it defeats the wrap.
        retry_until_authorized(Step(name="cr-pn", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "pn-{{runId}}",
                   "protocol": "TCP", "port": -1, "targetPort": 8080},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-VAL-UNSUPPORTED-PROTOCOL",
    title="Create Listener with protocol=HTTP (not a value of the enum) → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        *_setup_lb("bad-proto"),
        # The assertion used to read `oneOf([403, 400, 200])` — it accepted the
        # SUCCESS its own title calls a rejection, so it could not fail and never
        # did. That is why the swallow behind it went unseen: `protocol:"HTTP"` is
        # not a value of the enum, the edge dropped it to the zero value, the
        # listener was created with whatever the service defaults to, and the case
        # reported green. The edge now refuses an enum value outside the
        # dictionary, so the outcome is deterministic: 400 / INVALID_ARGUMENT.
        # `retry_until_authorized` still absorbs the read-your-writes 403 window on
        # the freshly created parent balancer.
        retry_until_authorized(Step(name="cr-http", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "http-{{runId}}",
                   "protocol": "HTTP", "port": 80, "targetPort": 8080},
             test_script=[
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ])),
        *_cleanup_lb(),
    ],
))

# RETIRED: LST-CR-VAL-INTERNAL-NO-SUBNET — the subject left the product, it did not break.
#
# Sub-phase 8.1 moved the VIP from the Listener to the LoadBalancer, and `subnet_id` left
# `CreateListenerRequest` with it (`reserved 8, 9` / `reserved "ip_version",
# "address_spec"`; migration 0028 dropped the columns behind them, and `domain.Listener`
# carries none). What the case sent was therefore an ORDINARY, VALID listener create,
# which the service accepts and is specified to accept: `subnet_id` is not "missing", it
# is not a field.
#
# It could not be repaired by tightening the assertion. `oneOf([403, 200, 400])` hid the
# problem — the create returned 200 and the case passed — but demanding a refusal would
# have made it permanently RED against correct behaviour, i.e. a "known failing" entry
# waiting to be born, and those outlive their fixes and start lying about the product
# (testing.md §«E2E НИКОГДА не пропускаются»). A case with no subject is retired.
#
# WHERE THE RULE LIVES NOW — AND IT HAD TO BE WRITTEN, NOT POINTED AT. "You must say
# where the VIP comes from" is enforced on the PARENT: LoadBalancer.Create refuses a body
# declaring no source for either family (vip_source.go, "load balancer must declare a vip
# source for at least one ip family"; sync, before any Operation exists; unit 8.1-19).
# The first version of this note cited `NLB-CR-VAL-SOURCE-REQUIRED` as the successor —
# and that case DID NOT EXIST anywhere in the tree. A retirement justified by a successor
# that does not exist is a deletion with a footnote, so the successor was written:
# NLB-CR-VAL-SOURCE-REQUIRED now lives in cases/load-balancer.py and is red-proven.
# Alongside it, NLB-CR-VAL-PLACEMENT-MISMATCH covers a source incoherent with the
# placement. The positive listener-on-INTERNAL-parent path stays covered by
# LST-CR-CRUD-INTERNAL above.

CASES.append(Case(
    id="LST-CR-VAL-NAME-REGEX",
    title="Create Listener with invalid name regex → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        *_setup_lb("bad-name"),
        retry_until_authorized(Step(name="cr-bad-name", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "Bad_Name!",
                   "protocol": "TCP", "port": 80, "targetPort": 8080},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])),
        *_cleanup_lb(),
    ],
))


# ---------------------------------------------------------------------------
# BVA — port boundaries
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="LST-CR-BVA-PORT-MIN-1",
    title="Create Listener with port=1 (lower bound) → OK",
    classes=["BVA"], priority="P2",
    steps=[
        *_setup_lb("port-1"),
        retry_until_authorized(Step(name="cr-p1", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "p1-{{runId}}",
                   "protocol": "TCP", "port": 1, "targetPort": 8080},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-BVA-PORT-MAX-65535",
    title="Create Listener with port=65535 (upper bound) → OK",
    classes=["BVA"], priority="P2",
    steps=[
        *_setup_lb("port-max"),
        retry_until_authorized(Step(name="cr-pmax", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "pmax-{{runId}}",
                   "protocol": "TCP", "port": 65535, "targetPort": 8080},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))


# ---------------------------------------------------------------------------
# State / NEG / CONF — BYO collisions, port collisions, VIP compensation
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="LST-CR-NEG-LB-UNKNOWN",
    title="Create Listener for unknown load_balancer_id → NotFound",
    classes=["NEG"], priority="P0",
    steps=[
        Step(name="cr-no-lb", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{garbageNlbId}}", "name": "nolb-{{runId}}",
                   "protocol": "TCP", "port": 80, "targetPort": 8080},
             test_script=[*assert_unscoped_rejected()]),
    ],
))

CASES.append(Case(
    id="LST-CR-CONF-DUP-PORT-PROTO",
    title="Duplicate (lb_id, port, protocol) → ALREADY_EXISTS (Verifies REQ-LST-UNIQ-PORT-PROTO)",
    classes=["CONF", "NEG"], priority="P0",
    steps=[
        *_setup_lb("dup-pp"),
        retry_until_authorized(Step(name="cr-1", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "pp1-{{runId}}",
                   "protocol": "TCP", "port": 86, "targetPort": 8086},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        # Second create is the duplicate under test — the parent-tuple is already warm from
        # cr-1, but wrap for symmetry so a late tuple-eviction can't red the ALREADY_EXISTS.
        #
        # WORKER lane. Nothing on the sync path looks for a duplicate: Create validates the
        # request, reads the parent LB and mints the Operation, and it is the worker's
        # INSERT that meets the UNIQUE on (load_balancer_id, port, protocol) — create.go
        # doCreate -> Listeners().Insert -> 23505 -> ALREADY_EXISTS. So the refusal REQ-LST-
        # UNIQ-PORT-PROTO names arrives on the Operation, and `200` is only the envelope
        # carrying it. Previously `200` closed the case by itself and the bare poll that
        # followed asserted `done` — which the DUPLICATE HAVING BEEN CREATED satisfies
        # equally well. `sync_codes=(409,)` keeps the lawful shape should the constraint
        # ever be prechecked synchronously; it is a refusal either way, and 200 is held to
        # account by the poll below.
        retry_until_authorized(Step(name="cr-2-dup", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "pp2-{{runId}}",
                   "protocol": "TCP", "port": 86, "targetPort": 8086},
             test_script=assert_refused_sync_or_async(
                 "duplicate (load_balancer_id, port, protocol)", sync_codes=(409,)))),
        poll_operation_until_done(must_fail=True),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-CONF-VIP-COMPENSATION",
    title="VIP allocated but INSERT fails → FreeIP compensation (Verifies REQ-LST-COMP-FREEIP)",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        # SYNC-VALIDATE lane. An unresolvable `defaultTargetGroupId` is refused by
        # `prevalidateTargetGroup` (create.go:123 → tg_ref.go `lookupWiredTargetGroup`)
        # BEFORE the Operation is minted: FAILED_PRECONDITION, i.e. 400 / grpc code 9,
        # with the actionable text pinned by target_group_id_test.go:53. Deterministic —
        # `garbageTgrId` resolves for nobody.
        #
        # The old assertion was named 'rejected or accepted' and listed five codes; it is
        # hard to write a statement that says less. It admitted the acceptance, every
        # refusal, and the authz denial the wrapper exists to absorb, so no product
        # behaviour could have failed it.
        #
        # SUBJECT NOTICE (for the owner, not a silent retirement). The case TITLE claims
        # FreeIP compensation after a post-VIP-allocation INSERT failure — REQ-LST-COMP-
        # FREEIP. A Listener no longer allocates a VIP at all: it is a (port, protocol) on
        # the parent LB's anycast VIP, Create is "чистый INSERT строки-листенера ... без
        # acquireVIP-саги и без обращения к vpc" (create.go:25-30), and migration 0028
        # dropped the address columns as dead by construction. So there is no compensation
        # branch left to exercise, exactly as with the already-removed
        # LST-DEL-CRUD-AUTO-VIP-FREE / -BYO-CLEAR-REF (see the note above LST-DEL-CRUD-OK).
        # What the case DOES exercise — an unresolvable TG reference is refused, and
        # refused synchronously — is real and worth asserting, so it is asserted here
        # rather than the case being dropped on my own authority. REQ-LST-COMP-FREEIP
        # itself needs an owner decision: retire it, or re-home it on the LoadBalancer
        # where the VIP saga and its compensation now live.
        *_setup_lb("vip-comp"),
        retry_until_authorized(Step(name="cr-likely-fail", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "vipc-{{runId}}",
                   "protocol": "TCP", "port": 87, "targetPort": 8087, "defaultTargetGroupId": "{{garbageTgrId}}"},
             test_script=[
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 "pm.test('refusal guides the caller to create the TargetGroup first', () => "
                 "  pm.expect(pm.response.json().message || '', pm.response.text())"
                 "    .to.include('create the TargetGroup first'));",
                 "pm.environment.unset('opId');",
             ])),
        *_cleanup_lb(),
    ],
))


# ---------------------------------------------------------------------------
# STATE — immutable fields on Update
# ---------------------------------------------------------------------------

def _immutable_listener_case(case_id: str, mask: str) -> Case:
    """Probe: naming an immutable field in `update_mask` is rejected.

    The assertion is carried ENTIRELY by the mask — `corevalidate.UpdateMask` reads
    `update_mask`, not the body. These cases used to also carry a same-named body key
    (`{"updateMask": "protocol", "protocol": "UDP"}`), but none of `loadBalancerId` /
    `protocol` / `port` is a field of UpdateListenerRequest, so the key was discarded by
    the edge and never reached the check it appeared to feed. Dropping it changes what is
    sent, not what is asserted.
    """
    return Case(
        id=case_id,
        title=f"Update Listener with mask={mask!r} → InvalidArgument (immutable)",
        classes=["STATE", "VAL"], priority="P0",
        steps=[
            Step(name="upd-imm", method="PATCH",
                 path=f"{_LST_BASE}/{{{{garbageLstId}}}}",
                 body={"updateMask": mask},
                 test_script=[*assert_absent_id_rejected()]),
        ],
    )


CASES.append(_immutable_listener_case("LST-UPD-STATE-IMMUTABLE-LB-ID", "loadBalancerId"))
CASES.append(_immutable_listener_case("LST-UPD-STATE-IMMUTABLE-PROTOCOL", "protocol"))
CASES.append(_immutable_listener_case("LST-UPD-STATE-IMMUTABLE-PORT", "port"))
# ipVersion / addressId are NOT listed here: they were removed from the Listener contract
# (listener.proto reserved 12-15) together with the columns behind them, so "immutable
# <field>" is no longer a statement about this resource — the mask entry is simply an
# unknown field. Their LoadBalancer-level counterparts (v4Source/v6Source immutability)
# are covered by the NLB suite.

CASES.append(Case(
    id="LST-UPD-STATE-DEFAULT-TG-REGION-MISMATCH",
    title="Update default_target_group_id to TG in different region → FailedPrecondition",
    classes=["STATE", "NEG"], priority="P1",
    steps=[
        *_setup_lb("def-tg-region"),
        retry_until_authorized(Step(name="cr", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "dtgr-{{runId}}",
                   "protocol": "TCP", "port": 88, "targetPort": 8088},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        # Make a TG in alt region
        Step(name="setup-tg-alt", method="POST", path="/nlb/v1/targetGroups",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionAltId}}",
                   "name": "dtgr-alt-{{runId}}", "port": 8080,
                   "healthCheck": {"interval": "2s", "timeout": "1s",
                                   "unhealthyThreshold": 3, "healthyThreshold": 2,
                                   "tcp": {"port": 80}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.targetGroupId", "tgAltId")]),
        # THE FIXTURE MUST SUCCEED, LOUDLY. A Kachō Operation carries the pre-allocated
        # id in `metadata` even when it finishes with an error, so a create that failed
        # ("Region not found") still hands the case a phantom `tgAltId` — and the case
        # then quietly tests a different thing. must_succeed + fixture_ids reddens here,
        # at the step that actually broke.
        poll_operation_until_done(must_succeed=True, fixture_ids=["tgAltId"]),
        # THE PRECONDITION NOW HAS A PRODUCER, AND THAT IS THE WHOLE POINT.
        #
        # This case ran on every production-posture run and could not fail for its own
        # reason: `existingRegionAltId` held the PRIMARY region, so the "alt-region" TG
        # was created in the SAME region and the repoint was lawfully accepted — 200,
        # with an Operation. It read as a product accepting a cross-region repoint. The
        # product was never asked. Nine env declarations carried the same collapse and
        # no seeder created a second region at all; both seeders now do
        # (prodseed_matrix.py / setup.sh), and deploy/scripts/assert-alt-fixtures-are-another.py
        # keeps the pair distinct AND its value produced.
        #
        # So the outcome is stated exactly, with no alternative branch to fall into.
        # Naming a TG in `update_mask` makes Update resolve it BEFORE minting the
        # Operation (update.go → lookupWiredTargetGroup), so the answer is synchronous:
        # the TG exists, in this project, in another region → FAILED_PRECONDITION
        # "default target group region … does not match listener region …" → 400 / grpc 9
        # (pinned unit-side by update_test.go). A 404 here would mean the fixture did not
        # persist — that is now caught above, by the fixture's own step, instead of being
        # accepted here as if it were the refusal under test.
        #
        # retry_on=(403,) ONLY: the first self-UPDATE of a freshly created listener can
        # be denied while the owner tuple materializes. That is a timing window, not an
        # outcome.
        retry_until_authorized(Step(name="upd-default-tg-mismatch", method="PATCH", path=f"{_LST_BASE}/{{{{lstId}}}}",
             body={"updateMask": "defaultTargetGroupId", "defaultTargetGroupId": "{{tgAltId}}"},
             test_script=[
                 "pm.test('cross-region repoint refused synchronously (400)', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(400));",
                 *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 "pm.test('the refusal names the region mismatch', () => {",
                 "  const m = pm.response.json().message || '';",
                 "  pm.expect(m, pm.response.text()).to.match(/does not match listener region/);",
                 "});",
                 "pm.environment.unset('opId');",
             ]), retry_on=(403,)),
        Step(name="cleanup-tg-alt", method="DELETE", path="/nlb/v1/targetGroups/{{tgAltId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))


# ---------------------------------------------------------------------------
# NEG — Get/List/Delete/ListOps not-found
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="LST-GET-NEG-NF-UNKNOWN",
    title="Get unknown listener_id → 404 NotFound",
    classes=["NEG"], priority="P1",
    steps=[
        Step(name="get-unknown", method="GET", path=f"{_LST_BASE}/{{{{garbageLstId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

CASES.append(Case(
    id="LST-DEL-NEG-NF-UNKNOWN",
    title="Delete unknown listener_id → 404 NotFound",
    classes=["NEG"], priority="P1",
    steps=[
        Step(name="del-unknown", method="DELETE", path=f"{_LST_BASE}/{{{{garbageLstId}}}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    # index: *-LST-NEG-LB-UNKNOWN (list narrowed by a parent id that does not resolve)
    id="LST-LST-NEG-LB-UNKNOWN",
    title="List narrowed by an unknown load_balancer_id → 200 with NO listeners "
          "(the filter matches nothing; it must not fall back to the whole project)",
    classes=["NEG", "LSG"], priority="P1",
    steps=[
        # `load_balancer_id` is an OPTIONAL FILTER on a project-scoped list, not a parent
        # path segment: List requires `project_id` and passes `load_balancer_id` straight
        # into the repo filter without an existence check (list.go:47-61). There is no
        # parent to be "not found", so the 404 the old title promised was never
        # producible — and the 200 it accepted alongside was accepted UNEXAMINED, which
        # is the half that could actually go wrong: a filter that is dropped rather than
        # applied answers 200 with every listener in the project. That is the regression
        # this case is positioned to catch, so that is what it now asserts.
        Step(name="lst-unknown-lb", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&loadBalancerId={{{{garbageNlbId}}}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('no listeners are returned for a load balancer that does not exist', () => {",
                 "  const j = pm.response.json();",
                 "  pm.expect(j.listeners || [], pm.response.text()).to.be.an('array').that.is.empty;",
                 "});",
             ]),
    ],
))

CASES.append(Case(
    id="LST-LST-BVA-PAGESIZE-OVER-MAX",
    title="List with pageSize=10000 → InvalidArgument",
    classes=["BVA", "VAL", "LSG"], priority="P2",
    steps=[
        Step(name="lst-huge", method="GET",
             path=f"{_LST_BASE}?loadBalancerId={{{{garbageNlbId}}}}&pageSize=10000",
             test_script=[
                 "pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));",
             ]),
    ],
))


# HTTP-method semantics
CASES.extend(http_method_not_allowed_block("LST", _LST_BASE))


# ---------------------------------------------------------------------------
# Extended VAL/NEG/BVA matrix saturation
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="LST-CR-VAL-NAME-NUMERIC-START",
    title="Create with name starting with digit → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        *_setup_lb("name-digit"),
        retry_until_authorized(Step(name="cr-digit", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "9bad-{{runId}}",
                   "protocol": "TCP", "port": 80, "targetPort": 8080},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-VAL-NAME-HYPHEN-START",
    title="Create with name starting with hyphen → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        *_setup_lb("name-hyp"),
        retry_until_authorized(Step(name="cr-hyp", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "-bad-{{runId}}",
                   "protocol": "TCP", "port": 80, "targetPort": 8080},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-VAL-TARGET-PORT-ZERO",
    title="Create with target_port=0 → InvalidArgument",
    classes=["VAL", "BVA"], priority="P1",
    steps=[
        *_setup_lb("tp-0"),
        # NAMED negative (task round-4b): the fresh-LB editor-tuple lag was pre-empting the
        # target_port=0 InvalidArgument with a 403. Wrap retries only the transient 403/404 so
        # the real InvalidArgument assertion runs — the negative is preserved, not weakened.
        # SYNC-VALIDATE lane: target_port goes through the SAME LbPort.Validate as port
        # (listener.Validate() combines l.Port and l.TargetPort — domain/listener.go:37),
        # so 0 is refused before the Operation exists → 400 / grpc 3, as the green
        # LST-CR-VAL-PORT-ZERO asserts for the sibling field. The wrap absorbs only the
        # transient fresh-LB 403; naming 403 (or 200) in the assertion would have made
        # both the wrap and the negative pointless — which is what it did.
        retry_until_authorized(Step(name="cr-tp-0", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "tp0-{{runId}}",
                   "protocol": "TCP", "port": 80, "targetPort": 0},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-VAL-TARGET-PORT-OVER",
    title="Create with target_port=65536 → InvalidArgument",
    classes=["VAL", "BVA"], priority="P1",
    steps=[
        *_setup_lb("tp-over"),
        # SYNC-VALIDATE lane — same statement as LST-CR-VAL-TARGET-PORT-ZERO, upper end:
        # 65536 clears the int32 narrowing and is refused by LbPort.Validate (range
        # [1, 65535]) inside listener.Validate(), before the Operation is minted.
        retry_until_authorized(Step(name="cr-tp-o", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "tpo-{{runId}}",
                   "protocol": "TCP", "port": 80, "targetPort": 65536},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])),
        *_cleanup_lb(),
    ],
))

# LST-CR-VAL-IPV-UNKNOWN — NOT retired: RE-POINTED at the control it was always about.
#
# `ip_version` is gone from the contract (`reserved 8`), so the field the case named no
# longer exists and its body was a plain valid create asserted as `oneOf([403, 200, 400])`
# — every possible answer. The first pass at this file retired it alongside
# LST-CR-VAL-INTERNAL-NO-SUBNET, on the stated grounds that "an unknown enum name is
# discarded by the edge anyway". That was true until 2026-07-29 and is now FALSE, which
# is exactly why retiring it was the wrong call: the edge gained the opposite behaviour
# days earlier (gateway/internal/restmux/strict_enum.go). protojson used to discard an
# unknown enum VALUE NAME as silently as an unknown key, leaving the service a zero value
# it could not tell from "unset" — so the caller was answered success for a setting the
# server never made. Unknown KEYS are still discarded on purpose (that boundary carries
# the update-mask clause), so re-pointing at a dead field would still test nothing.
#
# But the question — "is a value outside the enum dictionary refused?" — is about the
# request vocabulary, not about VIP sources, and `protocol` is a LIVE required enum on
# CreateListenerRequest. So the case asks it there. Retiring the only case-id whose
# subject is that control, while the control had no black-box probe anywhere in the tree,
# would have removed end-to-end coverage of something landed days before.

CASES.append(Case(
    id="LST-CR-VAL-IPV-UNKNOWN",
    title="Create with an out-of-dictionary enum value (protocol) → InvalidArgument at the edge",
    classes=["VAL"], priority="P1",
    steps=[
        *_setup_lb("enum-unk"),
        # The value is a string absent from `Listener.Protocol`
        # (PROTOCOL_UNSPECIFIED | TCP | UDP). Numeric forms stay legal — proto3 enums are
        # open and narrowing them would change the contract rather than fix a defect — so
        # only the string dictionary is asserted.
        #
        # No read-your-writes wrapper: this is a body-parse refusal at the edge, so the
        # request never reaches the per-object authz gate and there is no fresh-tuple
        # window to absorb.
        Step(name="cr-enum-unk", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "enum-{{runId}}",
                   "protocol": "DOES_NOT_EXIST", "port": 80, "targetPort": 8080},
             test_script=[
                 "pm.environment.unset('opId');",
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('names the field whose enum value was refused', () => "
                 "  pm.expect((pm.response.json().message || '')).to.include("
                 "    'invalid value for enum field protocol'));",
             ]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-CRUD-IPV6",
    title="Create with ip_version=IPV6 → OK",
    classes=["CRUD"], priority="P1",
    steps=[
        # The MIRROR of the defect fixed across this file: a case whose title promises
        # SUCCESS accepting refusals. 'OK or InsufficientPool' let 400, 409 and 403 all
        # pass, so the one thing it exists to prove — that this create is accepted — was
        # the only outcome not required. The listener draws from no pool at all (the VIP
        # is the parent LB's), so 'InsufficientPool' has no mechanism behind it; and the
        # request is shape-identical to the green LST-CR-CRUD-INTERNAL, which asserts a
        # strict 200. `must_succeed=True` closes the same hole one step later: a bare poll
        # asserts only `done`, which an Operation that FAILED satisfies just as well.
        #
        # SUBJECT NOTICE (owner decision, not silently retired — cf. LST-CR-VAL-IPV-UNKNOWN,
        # retired above): `ip_version` is `reserved 8` on CreateListenerRequest, so this
        # body no longer sends it and the case now says only "a valid listener create
        # succeeds" — already covered by LST-CR-CRUD-INTERNAL. It is kept, asserting
        # honestly, until the owner decides whether an IPv6 listener case belongs on the
        # LoadBalancer (v6Source) instead.
        *_setup_lb("ipv6"),
        retry_until_authorized(Step(name="cr-ipv6", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "v6-{{runId}}",
                   "protocol": "TCP", "port": 80, "targetPort": 8080},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(must_succeed=True),
        Step(name="cleanup-best-effort", method="DELETE", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-CRUD-PROXY-PROTO-V2",
    title="Create with proxy_protocol_v2=true → OK",
    classes=["CRUD"], priority="P2",
    steps=[
        *_setup_lb("pp2"),
        retry_until_authorized(Step(name="cr-pp2", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "pp2-{{runId}}",
                   "protocol": "TCP", "port": 90, "targetPort": 9090,
                   "proxyProtocolV2": True},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('proxy_protocol_v2 persisted', () => "
                          "  pm.expect(pm.response.json().proxyProtocolV2).to.eql(true));"])),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-UPD-CRUD-DEFAULT-TG-CLEAR",
    title="Update default_target_group_id to \"\" → cleared",
    classes=["CRUD", "STATE"], priority="P2",
    steps=[
        *_setup_lb("def-tg-clear"),
        retry_until_authorized(Step(name="cr", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "dtgc-{{runId}}",
                   "protocol": "TCP", "port": 91, "targetPort": 9091},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        # Same mirror-defect as LST-CR-CRUD-IPV6: 'accepted or no-op' accepted the 400 that
        # would mean the clear was REFUSED. Clearing is unambiguous on the sync path — an
        # applied TG field with an empty value drops the reference and skips the resolve
        # entirely (update.go:177-192, `if tg == "" → next.DefaultTargetGroupID = {}`), so
        # there is no branch that lawfully refuses this and nothing for the tolerance to
        # cover. 403 is the read-your-writes window the wrapper already absorbs.
        retry_until_authorized(Step(name="upd-clear", method="PATCH", path=f"{_LST_BASE}/{{{{lstId}}}}",
             body={"updateMask": "defaultTargetGroupId", "defaultTargetGroupId": ""},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(must_succeed=True),
        # …and the case is named for the CLEAR, which it never checked: it asserted the
        # response code of the mutation and stopped, so a service that accepted the PATCH
        # and kept the reference passed. Read it back (same statement as the green
        # `get-after-refused-repoint` in LST-UPD-SEC-TG-CROSS-PROJECT).
        retry_until_authorized(Step(name="get-after-clear", method="GET",
             path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('the target group reference is actually cleared', () => {",
                 "  const j = pm.response.json();",
                 "  pm.expect(j.targetGroupId || j.defaultTargetGroupId || '', pm.response.text()).to.eql('');",
                 "});",
             ])),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-GET-NEG-INVALID-ID-PREFIX",
    title="Get with malformed id prefix → InvalidArgument",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        Step(name="get-bad-prefix", method="GET", path=f"{_LST_BASE}/garbage-not-an-id",
             test_script=[
                 "pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));",
             ]),
    ],
))


CASES.append(Case(
    id="LST-LST-FILTER-NAME",
    title="List Listeners with filter name=\"x\" → 200, and every row returned is named \"x\"",
    classes=["LSG"], priority="P2",
    steps=[
        # The request had to be corrected before the assertion could mean anything. It
        # carried no `projectId`, and List requires one (list.go:47-50), so it could only
        # ever answer 400 "project_id required" — while its title promised 200 and its
        # `oneOf([200, 400, 404])` accepted that 400 as a pass. The name filter, the whole
        # subject of the case, was never reached. Scoping it to the suite project (as the
        # green LST-LST-LSG-PROJECT-SCOPED-OK does) is what puts the filter on the path.
        #
        # The garbage `loadBalancerId` is dropped for the same reason: it emptied the page
        # by itself, so the filter could not have been observed doing anything. Now the
        # suite's own listeners are in scope, and the assertion is falsifiable — a filter
        # that is parsed and ignored returns them, and every one of them fails the name
        # check (shared.ParseNameFilter → repo `name =` predicate, list.go:52-60).
        Step(name="lst-filter", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&filter=name%3D%22x%22",
             test_script=[
                 *assert_status(200),
                 "pm.test('the name filter is applied, not merely accepted', () => {",
                 "  const rows = pm.response.json().listeners || [];",
                 "  pm.expect(rows, pm.response.text()).to.be.an('array');",
                 "  rows.forEach(r => pm.expect(r.name, pm.response.text()).to.eql('x'));",
                 "});",
             ]),
    ],
))

CASES.append(Case(
    id="LST-LST-PAGE-ROUNDTRIP",
    title="Pagination round-trip on listeners",
    classes=["CRUD", "LSG", "BVA"], priority="P2",
    steps=[
        *_setup_lb("page-rt"),
        Step(name="page-1", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&loadBalancerId={{{{nlbId}}}}&pageSize=1",
             test_script=[*assert_status(200),
                          "pm.environment.set('lstNextToken', pm.response.json().nextPageToken || '');"]),
        Step(name="page-2", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&loadBalancerId={{{{nlbId}}}}&pageSize=1&pageToken={{{{lstNextToken}}}}",
             test_script=[*assert_status(200)]),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-CR-VAL-MALFORMED-JSON",
    title="Create Listener with malformed JSON body → 400/415",
    classes=["VAL"], priority="P2",
    steps=[
        Step(name="cr-malformed", method="POST", path=_LST_BASE, body=None,
             pre_script=["pm.request.body = { mode: 'raw', raw: '{not json' };"],
             test_script=[
                 "pm.test('400/403/415', () => pm.expect(pm.response.code).to.be.oneOf([400, 403, 415]));",
             ]),
    ],
))

CASES.append(Case(
    id="LST-CR-VAL-EMPTY-BODY",
    title="Create Listener with empty body → InvalidArgument",
    classes=["VAL"], priority="P2",
    steps=[
        Step(name="cr-empty", method="POST", path=_LST_BASE, body={},
             test_script=[*assert_unscoped_rejected()]),
    ],
))

CASES.append(Case(
    id="LST-CR-CRUD-UDP-PROTOCOL",
    title="Create Listener with protocol=UDP → OK",
    classes=["CRUD"], priority="P1",
    steps=[
        *_setup_lb("udp"),
        retry_until_authorized(Step(name="cr-udp", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "udp-{{runId}}",
                   "protocol": "UDP", "port": 53, "targetPort": 53},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))


# ===========================================================================
# Listener List-pagination + project-scope parity (LST-LST-*) and malformed-id
# / not-found parity (LST-UPD-*/LST-DEL-*).
#
# WHY (gap vs siblings): the NLB & TargetGroup List/Update/Delete suites already
# carry the STRICT project-scoped pagination-BVA set (garbage token / pageSize
# max / max+1 / 0 / -1 / empty token) and the FULL Get+Update+Delete malformed-id
# trio + Update-not-found. The Listener suite historically had only a WEAK
# garbage-parent OVER-MAX list (`?loadBalancerId={{garbageNlbId}}&pageSize=10000`,
# tolerating 404 — the missing parent masks whether page_size was even validated)
# and a Get-only malformed case. These cases close that asymmetry.
#
# Contract sources (Kachō conventions — own product, no foreign-cloud framing):
#   * ListListenersRequest is PROJECT-scoped (`project_id` required, KAC-229 parity
#     with NLB/TG List; `load_balancer_id` optional filter), `page_size <= 1000`,
#     `page_token <= 100` — proto constraints IDENTICAL to the already-green NLB/TG
#     List suites, so the strict assertions below are grounded in proven-green
#     sibling behaviour on this build.
#   * api-conventions.md §Pagination/filter + §Gotcha "List: валидация pagination
#     ДО listauthz empty-grant short-circuit"; security.md hardening inv-7:
#     page_size/page_token are validated to InvalidArgument BEFORE the listauthz
#     empty-grant short-circuit — an authorized editor scoping their own project
#     must get a strict 400 (never a masked 200-empty).
#   * api-conventions.md §Error-format + §Gotcha "Malformed-id — ПЕРВЫМ стейтментом
#     RPC": a malformed listener id on Update/Delete is rejected synchronously as
#     InvalidArgument. Tolerant 400/404 mirrors the green NLB/TG malformed trio
#     (`NLB-/TGR-{GET,UPD,DEL}-NEG-INVALID-ID-PREFIX`) — malformed id does not 403
#     on this build.
#
# Test-design techniques (skill testing-product-coach): BVA (page_size min-1 / 0 /
# max / max+1), ECP (valid-scope vs garbage-token vs malformed-id vs absent-id
# input classes), error-guessing (pagination-validate-before-authz ordering;
# authz-first-vs-existence ordering on an absent-id mutation). All cases are
# self-contained, fixture-free, pool-independent → idempotent and parallel-safe
# (single GET/PATCH/DELETE, no setup/cleanup, no {{runId}} resources leaked).
# ===========================================================================

CASES.append(Case(
    # index: LST-LST-LSG-PROJECT-SCOPED-OK
    id="LST-LST-LSG-PROJECT-SCOPED-OK",
    title="List listeners project-scoped (no loadBalancerId filter) → 200 + listeners array "
          "(KAC-229 project-scope parity). Technique: ECP (valid project-scoped list class)",
    classes=["LSG", "CRUD"], priority="P1",
    steps=[
        Step(name="lst-project-scoped", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=10",
             test_script=[
                 *assert_status(200),
                 "pm.test('listeners is an array', () => {",
                 "  const j = pm.response.json();",
                 "  pm.expect(j.listeners || [], JSON.stringify(j)).to.be.an('array');",
                 "});",
             ]),
    ],
))

CASES.append(Case(
    id="LST-LST-PAGE-TOKEN-GARBAGE",
    title="List listeners with garbage pageToken → 400 InvalidArgument (validated before "
          "listauthz empty-grant short-circuit). Techniques: ECP (garbage-token class) + "
          "error-guessing (pagination-validate-before-authz ordering)",
    classes=["VAL", "LSG"], priority="P1",
    steps=[
        Step(name="lst-bad-token", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=10&pageToken=not-a-real-token",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="LST-LST-BVA-PAGESIZE-1001",
    title="List listeners with pageSize=1001 (off-by-one over max) → 400 InvalidArgument "
          "(validated before empty-grant short-circuit). Technique: BVA (max+1)",
    classes=["BVA", "VAL", "LSG"], priority="P2",
    steps=[
        Step(name="lst-1001", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=1001",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="LST-LST-BVA-PAGESIZE-1000",
    title="List listeners with pageSize=1000 (max upper bound) → 200. Technique: BVA (max)",
    classes=["BVA", "LSG"], priority="P2",
    steps=[
        Step(name="lst-1000", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=1000",
             test_script=[*assert_status(200)]),
    ],
))

CASES.append(Case(
    id="LST-LST-BVA-PAGESIZE-ZERO",
    title="List listeners with pageSize=0 → 200 (server default applied). "
          "Techniques: BVA (0→default) + ECP (unset-size class)",
    classes=["BVA", "LSG"], priority="P2",
    steps=[
        Step(name="lst-zero", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=0",
             test_script=[*assert_status(200)]),
    ],
))

CASES.append(Case(
    id="LST-LST-PAGE-TOKEN-EMPTY",
    title="List listeners with pageToken=\"\" → 200 (empty token = first page). "
          "Technique: BVA (empty-string token boundary)",
    classes=["LSG", "BVA"], priority="P2",
    steps=[
        Step(name="lst-empty-token", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=10&pageToken=",
             test_script=[*assert_status(200)]),
    ],
))

CASES.append(Case(
    id="LST-LST-BVA-PAGESIZE-NEGATIVE",
    title="List listeners with pageSize=-1 → 400 InvalidArgument (rejected, never clamped). "
          "Technique: BVA (min-1 / below-range)",
    classes=["BVA", "VAL", "LSG"], priority="P2",
    steps=[
        # SYNC-VALIDATE lane, and it is not a matter of opinion: `shared.ValidatePagination`
        # refuses `pageSize < 0` in its FIRST statement (pagination.go:31) — the same
        # statement, the same `if`, that refuses `> 1000` for the green
        # LST-LST-BVA-PAGESIZE-1001 above. list.go:64 runs it before the repo is touched.
        # There is no coercion branch: only `0` becomes the server default, and it does so
        # further down, in corevalidate.PageSize, which -1 never reaches.
        #
        # So the "or coerced to default (200)" half of the old title described behaviour
        # the product does not have, and the assertion that encoded it accepted the very
        # outcome api-conventions forbids ("page_size вне [0..1000] → InvalidArgument;
        # отвергается, не clamp'ится"). CASES-INDEX has said `→ InvalidArgument` for this
        # id all along — the case simply did not assert it.
        Step(name="lst-neg", method="GET",
             path=f"{_LST_BASE}?projectId={{{{_suiteProjectId}}}}&pageSize=-1",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="LST-UPD-NEG-INVALID-ID-PREFIX",
    title="Update listener with malformed id prefix → InvalidArgument (format-check first "
          "statement). Techniques: ECP (malformed-id class) + error-guessing (sync-format "
          "reject before repo.Get)",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        Step(name="upd-bad-prefix", method="PATCH", path=f"{_LST_BASE}/garbage-not-an-id",
             body={"updateMask": "description", "description": "x"},
             test_script=[
                 "pm.test('rejected (400 InvalidArgument or 404)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([400, 404]));",
             ]),
    ],
))

CASES.append(Case(
    id="LST-DEL-NEG-INVALID-ID-PREFIX",
    title="Delete listener with malformed id prefix → InvalidArgument (format-check first "
          "statement). Techniques: ECP (malformed-id class) + error-guessing",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        Step(name="del-bad-prefix", method="DELETE", path=f"{_LST_BASE}/garbage-not-an-id",
             test_script=[
                 "pm.test('rejected (400 InvalidArgument or 404)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([400, 404]));",
             ]),
    ],
))

CASES.append(Case(
    id="LST-UPD-NEG-NF-UNKNOWN",
    title="Update well-formed-but-absent listener id → rejected (NotFound / authz-first). "
          "Techniques: ECP (absent-id class) + error-guessing (authz-vs-existence ordering)",
    classes=["NEG"], priority="P1",
    steps=[
        # Mirrors LST-DEL-NEG-NF-UNKNOWN: a well-formed but non-existent listener id on a
        # mutation is a defensible reject — 404 (repo.Get miss) OR 403 (gateway
        # scope_extractor cannot resolve target→project for the absent id, authz-first
        # anti-BOLA) OR 400. Tolerant per api-conventions authz-first ordering; never 200.
        Step(name="upd-unknown", method="PATCH", path=f"{_LST_BASE}/{{{{garbageLstId}}}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))


# ---------------------------------------------------------------------------
# SEC — cross-project TargetGroup wiring (BOLA / CWE-639)
# ---------------------------------------------------------------------------
#
# `targetGroupId` (and its legacy twin `defaultTargetGroupId`) is a caller-supplied
# object the per-RPC gateway Check does NOT scope: CreateListener is scoped on
# `loadBalancerId`, UpdateListener on `listenerId`. Wiring a TargetGroup owned by
# ANOTHER project must be refused — otherwise this LB would forward live traffic to
# the other project's targets, and the victim's TG would become undeletable
# (FK RESTRICT). The refusal must look exactly like "no such target group": a
# distinct code/message would confirm the foreign TG exists (existence-oracle).

def _cross_project_tg_setup(suffix: str):
    """Create a TargetGroup in the suite's CROSS project (`_suiteProjectCrossId`).

    Fixture-tolerant: when the cross project is unseeded on this stand the Create
    is rejected and `tgCrossId` stays UNSET, so the wiring step below sends an
    unresolvable reference — which must be rejected all the same. Either way the
    assertion "the listener is never wired to a foreign TG" holds.
    """
    return [
        Step(name="setup-tg-cross", method="POST", path="/nlb/v1/targetGroups",
             body={"projectId": "{{_suiteProjectCrossId}}", "regionId": "{{_suiteRegionId}}",
                   "name": f"xtg-{suffix}-{{{{runId}}}}", "port": 8080,
                   "healthCheck": {"interval": "2s", "timeout": "1s",
                                   "unhealthyThreshold": 3, "healthyThreshold": 2,
                                   "tcp": {"port": 80}}},
             test_script=[
                 "pm.environment.unset('opId');",
                 "pm.environment.set('tgCrossId', 'tgrabsent0000000000x');",
                 "if (pm.response.code === 200) {",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 "  if (j.metadata && j.metadata.targetGroupId) "
                 "    pm.environment.set('tgCrossId', j.metadata.targetGroupId);",
                 "}",
             ]),
        poll_operation_until_done(),
    ]


def _cross_project_tg_cleanup():
    return [
        Step(name="cleanup-tg-cross", method="DELETE", path="/nlb/v1/targetGroups/{{tgCrossId}}",
             test_script=[
                 "pm.test('cross-project TG reclaim best-effort (never fails the case)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 400, 403, 404, 409]));",
                 "pm.environment.unset('opId');",
                 "if (pm.response.code === 200) { try { const j = pm.response.json();"
                 " if (j.id) pm.environment.set('opId', j.id); } catch (e) {} }",
                 "pm.environment.unset('tgCrossId');",
             ]),
        poll_operation_until_done(),
    ]


# The rejection must not disclose the foreign project — asserted on the body.
_ASSERT_NO_PROJECT_LEAK = [
    "pm.test('rejection does not disclose the foreign project id', () => "
    "  pm.expect(pm.response.text()).to.not.include(pm.environment.get('_suiteProjectCrossId')));",
]

CASES.append(Case(
    id="LST-CR-SEC-TG-CROSS-PROJECT",
    title="Create listener wiring a TargetGroup of another project → rejected, never wired",
    classes=["NEG", "AZD"], priority="P0",
    steps=[
        *_setup_lb("xtg-cr"),
        *_cross_project_tg_setup("cr"),
        # NOT wrapped in retry_until_authorized: this is a negative — a retry loop
        # would only mask the deny it is meant to observe.
        Step(name="cr-lst-cross-tg", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "xtg-{{runId}}",
                   "protocol": "TCP", "port": 8443, "targetPort": 8080,
                   "targetGroupId": "{{tgCrossId}}"},
             test_script=[
                 "pm.test('cross-project target group is refused (never 200)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([400, 403, 404, 409]));",
                 *_ASSERT_NO_PROJECT_LEAK,
             ]),
        # Same guard on the legacy wiring field — both map to the same reference.
        Step(name="cr-lst-cross-tg-legacy", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "xtgl-{{runId}}",
                   "protocol": "TCP", "port": 8444, "targetPort": 8080,
                   "defaultTargetGroupId": "{{tgCrossId}}"},
             test_script=[
                 "pm.test('cross-project target group is refused via the legacy field too', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([400, 403, 404, 409]));",
                 *_ASSERT_NO_PROJECT_LEAK,
             ]),
        *_cross_project_tg_cleanup(),
        *_cleanup_lb(),
    ],
))

CASES.append(Case(
    id="LST-UPD-SEC-TG-CROSS-PROJECT",
    title="Repoint listener to a TargetGroup of another project → rejected, reference unchanged",
    classes=["NEG", "AZD", "STATE"], priority="P0",
    steps=[
        *_setup_lb("xtg-upd"),
        retry_until_authorized(Step(name="cr", method="POST", path=_LST_BASE,
             body={"loadBalancerId": "{{nlbId}}", "name": "xtgu-{{runId}}",
                   "protocol": "TCP", "port": 8445, "targetPort": 8080},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.listenerId", "lstId")])),
        poll_operation_until_done(),
        *_cross_project_tg_setup("upd"),
        # The listener itself already exists and was read back above, so the only
        # lawful outcomes here are refusals — retry_on=(403,) covers the owner-tuple
        # lag on the listener object without masking the TG refusal (403 is also a
        # lawful refusal, so the budget simply expires and the assertion still runs).
        Step(name="upd-cross-tg", method="PATCH", path=f"{_LST_BASE}/{{{{lstId}}}}",
             body={"updateMask": "targetGroupId", "targetGroupId": "{{tgCrossId}}"},
             test_script=[
                 "pm.test('cross-project repoint is refused (never 200)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([400, 403, 404, 409]));",
                 *_ASSERT_NO_PROJECT_LEAK,
             ]),
        retry_until_authorized(Step(name="get-after-refused-repoint", method="GET",
             path=f"{_LST_BASE}/{{{{lstId}}}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('the refused repoint left no target group wired', () => {",
                 "  const j = pm.response.json();",
                 "  pm.expect(j.targetGroupId || j.defaultTargetGroupId || '').to.eql('');",
                 "});",
             ])),
        *_cross_project_tg_cleanup(),
        *_cleanup_lst(),
        *_cleanup_lb(),
    ],
))
