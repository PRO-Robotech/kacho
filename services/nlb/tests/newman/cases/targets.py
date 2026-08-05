# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Targets sub-resource cases (TGT-*) — AddTargets / RemoveTargets.

Acceptance: docs/specs/sub-phase-4.0-nlb-acceptance.md §6 (GWT-TGT-001..016).
Design §4.3 (4-way identity oneof + bogon check + per-target peer-validate).
Design §4.4 (2-phase RemoveTargets drain — Phase A immediate + Phase B runner).

REST: /nlb/v1/targetGroups/{target_group_id}:addTargets   (POST)
      /nlb/v1/targetGroups/{target_group_id}:removeTargets (POST)
"""

CASES = []

_TG_BASE = "/nlb/v1/targetGroups"

_HC = {"interval": "2s", "timeout": "1s",
       "unhealthyThreshold": 3, "healthyThreshold": 2, "tcp": {"port": 80}}

_TG_BODY = {"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
            # Required top-level backend port (NLB-1b F6-co-req, CreateTargetGroupRequest.port).
            "port": 8080,
            "healthCheck": _HC, "deregistrationDelay": "300s",
            "slowStart": "30s"}


def _setup_tg(name_suffix: str, dereg_seconds: int = 300):
    return [
        Step(name="setup-cr-tg", method="POST", path=_TG_BASE,
             body={**_TG_BODY, "name": f"tgt-{name_suffix}-{{{{runId}}}}",
                   "deregistrationDelay": f"{dereg_seconds}s"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.targetGroupId", "tgId")]),
        poll_operation_until_done(),
        # read-your-writes: materialize the TG owner-tuple (eventually-consistent after
        # opgate removal) before the first :addTargets / :removeTargets self-access.
        retry_until_authorized(Step(name="setup-materialize-tg", method="GET",
             path=f"{_TG_BASE}/{{{{tgId}}}}", test_script=[])),
    ]


def _cleanup_tg(leftover_targets=None):
    """Уборка группы целей. `leftover_targets` — то, что кейс в ней ОСТАВИЛ.

    Продукт отказывается удалять непустую группу (`FailedPrecondition`
    «TargetGroup has N target(s); remove them first via RemoveTargets») — это
    его объявленный контракт, у него есть собственный отрицательный кейс
    `TGR-DEL-NEG-HAS-TARGETS`. Значит уборка обязана сперва СЛИТЬ группу, иначе
    она получает законный отказ и группа живёт до конца стенда.

    Имя `best-effort` этого не оправдывало: пока шаг не нёс утверждения, отказ
    был не виден, и «best-effort» читалось как решение, хотя было умолчанием.
    Прогон CI 31053251941 показал отказ на `TGT-RM-STATE-SIBLING-STAYS-ACTIVE`,
    где после снятия одной из двух целей вторая остаётся по замыслу кейса.

    Кейс называет остаток САМ, а не выводится опросом: список известен ему по
    построению, поэтому уборка остаётся детерминированной и не зависит от того,
    что вернёт чтение. Забытый остаток теперь краснеет сразу — утверждение
    шага удаления это ловит (`assert-delete-steps-are-asserted.py`).
    """
    drain = []
    if leftover_targets:
        drain = [
            Step(name="cleanup-drain-targets", method="POST",
                 path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
                 body={"targets": list(leftover_targets)},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
        ]
    return [
        *drain,
        Step(name="cleanup-tg", method="DELETE", path=f"{_TG_BASE}/{{{{tgId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


# ---------------------------------------------------------------------------
# AddTargets — 4-way identity matrix
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="TGT-ADD-CRUD-INSTANCE-ID",
    title="AddTargets variant 1: instance_id (Verifies REQ-TGT-4WAY-INSTANCE)",
    classes=["CRUD"], priority="P0",
    steps=[
        *_setup_tg("add-inst"),
        retry_until_authorized(Step(name="add-inst", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"instanceId": "{{existingInstanceId}}", "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm-cleanup", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"instanceId": "{{existingInstanceId}}"}]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-CRUD-NIC-ID",
    title="AddTargets variant 2: nic_id",
    classes=["CRUD"], priority="P0",
    steps=[
        *_setup_tg("add-nic"),
        retry_until_authorized(Step(name="add-nic", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"nicId": "{{existingNicId}}", "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm-cleanup", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"nicId": "{{existingNicId}}"}]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-CRUD-IP-REF",
    title="AddTargets variant 3: ip_ref{subnet_id, address}",
    classes=["CRUD"], priority="P0",
    steps=[
        *_setup_tg("add-ipref"),
        retry_until_authorized(Step(name="add-ipref", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"ipRef": {"subnetId": "{{existingSubnetId}}",
                                          "address": "10.180.0.5"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm-cleanup", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"ipRef": {"subnetId": "{{existingSubnetId}}",
                                          "address": "10.180.0.5"}}]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-CRUD-EXTERNAL-IP",
    title="AddTargets variant 4: external_ip{address}",
    classes=["CRUD"], priority="P0",
    steps=[
        *_setup_tg("add-ext"),
        retry_until_authorized(Step(name="add-ext", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.10"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm-cleanup", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.10"}}]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-CRUD-MIXED-IDENTITIES",
    title="AddTargets with all 4 variants in single call",
    classes=["CRUD"], priority="P1",
    steps=[
        *_setup_tg("add-mixed"),
        retry_until_authorized(Step(name="add-mixed", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [
                 {"instanceId": "{{existingInstanceId}}", "weight": 100},
                 {"nicId": "{{existingNicId}}", "weight": 100},
                 {"ipRef": {"subnetId": "{{existingSubnetId}}", "address": "10.180.0.6"},
                  "weight": 50},
                 {"externalIp": {"address": "203.0.113.11"}, "weight": 100},
             ]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm-cleanup", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [
                 {"instanceId": "{{existingInstanceId}}"},
                 {"nicId": "{{existingNicId}}"},
                 {"ipRef": {"subnetId": "{{existingSubnetId}}", "address": "10.180.0.6"}},
                 {"externalIp": {"address": "203.0.113.11"}},
             ]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))


# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="TGT-ADD-VAL-EMPTY-LIST",
    title="AddTargets with targets=[] → InvalidArgument 'at least one target is required'",
    classes=["VAL"], priority="P1",
    steps=[
        *_setup_tg("add-empty"),
        Step(name="add-empty", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": []},
             test_script=[
                 # Empty-list guard is synchronous (add_targets.go:80, before any
                 # Operation is created) → always InvalidArgument/400. A 200 here
                 # would be the validation regression this case exists to catch.
                 "pm.test('rejected sync 400', () => "
                 "  pm.expect(pm.response.code).to.eql(400));",
             ]),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-VAL-WEIGHT-NEGATIVE",
    title="AddTargets weight=-1 → InvalidArgument",
    classes=["VAL", "BVA"], priority="P1",
    steps=[
        *_setup_tg("w-neg"),
        Step(name="add-w-neg", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.20"}, "weight": -1}]},
             test_script=[
                 # weight bounds are validated synchronously (domain Target.Validate
                 # via add_targets.go:89, before Operation creation) → always 400.
                 "pm.test('rejected sync 400', () => pm.expect(pm.response.code).to.eql(400));",
             ]),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-VAL-WEIGHT-OVER",
    title="AddTargets weight=1001 → InvalidArgument",
    classes=["VAL", "BVA"], priority="P1",
    steps=[
        *_setup_tg("w-over"),
        Step(name="add-w-over", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.21"}, "weight": 1001}]},
             test_script=[
                 # weight bounds are validated synchronously (domain Target.Validate
                 # via add_targets.go:89, before Operation creation) → always 400.
                 "pm.test('rejected sync 400', () => pm.expect(pm.response.code).to.eql(400));",
             ]),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-BVA-WEIGHT-MIN-0",
    title="AddTargets weight=0 → OK (drain semantics)",
    classes=["BVA"], priority="P2",
    steps=[
        *_setup_tg("w-min"),
        retry_until_authorized(Step(name="add-w-0", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.22"}, "weight": 0}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.22"}}]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-BVA-WEIGHT-MAX-1000",
    title="AddTargets weight=1000 → OK (upper bound)",
    classes=["BVA"], priority="P2",
    steps=[
        *_setup_tg("w-max"),
        retry_until_authorized(Step(name="add-w-1000", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.23"}, "weight": 1000}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.23"}}]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-VAL-BOGON-LOOPBACK",
    title="AddTargets external_ip=127.0.0.1 → InvalidArgument (bogon)",
    classes=["VAL"], priority="P0",
    steps=[
        *_setup_tg("bogon-add"),
        Step(name="add-bogon", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "127.0.0.1"}, "weight": 100}]},
             test_script=[
                 # SYNC-VALIDATE lane. The bogon classes are refused by
                 # domain.Target.Validate -> TargetExternalIP.Validate (domain/target.go,
                 # classifyBogon), which add_targets.go:96 runs BEFORE the Operation is
                 # minted -> INVALID_ARGUMENT, i.e. 400 / grpc code 3, always.
                 # The previous `oneOf([200, 400])` also accepted the ACCEPTANCE this case
                 # exists to prove impossible, so a regression removing the bogon check
                 # left it green. Same assertion as the green TGR-CR-VAL-TARGET-BOGON-*
                 # block, which drives the identical domain check from the Create path.
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-VAL-IP-REF-NOT-IN-SUBNET",
    title="AddTargets ip_ref outside subnet CIDR → InvalidArgument (Verifies REQ-TGT-IPREF-CIDR)",
    classes=["VAL"], priority="P0",
    steps=[
        *_setup_tg("ipref-out"),
        # WORKER lane. The sync validator only parses the address and requires a
        # non-empty subnet_id (domain/target.go TargetIPRef.Validate); membership of the
        # subnet CIDR needs the peer's CIDR blocks, so it is decided by the worker
        # (add_targets.go validateIPRefTarget -> addressInAnyCIDR) and surfaces on the
        # Operation. `200` alone is therefore NOT a pass: what used to follow this step
        # was a bare poll, which asserts only `done` — satisfied just as well by an
        # Operation that SUCCEEDED, i.e. by the product accepting an out-of-CIDR address.
        Step(name="add-out", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"ipRef": {"subnetId": "{{existingSubnetId}}",
                                          "address": "10.99.99.99"}, "weight": 100}]},
             test_script=assert_refused_sync_or_async("ip_ref.address outside the subnet CIDR")),
        poll_operation_until_done(must_fail=True),
        *_cleanup_tg(),
    ],
))


# ---------------------------------------------------------------------------
# Peer-validate failures
# ---------------------------------------------------------------------------
#
# LANE (whole section). Nothing here is decided synchronously: AddTargets validates
# only the request SHAPE before minting the Operation — target_group_id present and
# well-formed, list non-empty and within MaxTargetsPerGroup, then per-target
# domain.Target.Validate (the 4-way oneof, the weight bound, the external_ip bogon
# classes). Whether an instance / NIC / subnet EXISTS, and whether it sits in the
# target group's region, is answered by peers and is therefore checked by the WORKER
# (add_targets.go doAdd -> validateTargetPeer -> validateInstanceTarget /
# validateNicTarget / validateIPRefTarget). So the lawful refusal here is `200`
# carrying an Operation that FAILS.
#
# Which is exactly why `oneOf([200, 400, 404])` followed by a bare
# `poll_operation_until_done()` asserted nothing: `200` satisfied the first statement
# on its own, and the poll only asserted `done` — true of a SUCCEEDED Operation too.
# A regression that stopped peer-validating targets altogether would have kept every
# case in this section green while the target group quietly accepted a member that
# does not exist. `assert_refused_sync_or_async` + `must_fail=True` states both lanes.

CASES.append(Case(
    id="TGT-ADD-NEG-INSTANCE-UNKNOWN",
    title="AddTargets unknown instance_id → InvalidArgument 'not found' (Verifies REQ-TGT-PEER-INSTANCE)",
    classes=["NEG"], priority="P1",
    steps=[
        *_setup_tg("inst-nx"),
        Step(name="add-inst-nx", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"instanceId": "epdinstdoesnotexist0", "weight": 100}]},
             test_script=assert_refused_sync_or_async("unknown instance_id")),
        poll_operation_until_done(must_fail=True),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-NEG-NIC-UNKNOWN",
    title="AddTargets unknown nic_id → InvalidArgument",
    classes=["NEG"], priority="P1",
    steps=[
        *_setup_tg("nic-nx"),
        # retry ONLY on 403 (fresh-TG editor owner-tuple read-your-writes lag) — the
        # legitimate NotFound (unknown nic) 404 is the EXPECTED outcome and must NOT be
        # retried away, so retry_on is narrowed to (403,).
        retry_until_authorized(Step(name="add-nic-nx", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"nicId": "e9bnicdoesnotexist00", "weight": 100}]},
             test_script=assert_refused_sync_or_async("unknown nic_id")), retry_on=(403,)),
        poll_operation_until_done(must_fail=True),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-NEG-SUBNET-UNKNOWN",
    title="AddTargets ip_ref with unknown subnet_id → InvalidArgument",
    classes=["NEG"], priority="P1",
    steps=[
        *_setup_tg("sub-nx"),
        Step(name="add-sub-nx", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"ipRef": {"subnetId": "e9bsubdoesnotexist00",
                                          "address": "10.0.0.5"}, "weight": 100}]},
             test_script=assert_refused_sync_or_async("unknown ip_ref.subnet_id")),
        poll_operation_until_done(must_fail=True),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    # FIXTURE TRUTH (не маскировка, а декларация): `absentInstanceCrossRegionId` — это
    # НЕСУЩЕСТВУЮЩИЙ id, а не засеянный инстанс в другом регионе. Cross-REGION Instance
    # непроизводим на односорегионном стенде (geo baseline сеет только ru-central1;
    # prodseed_nlb_ext.py прямым текстом оставляет instance-target deps незасеянными).
    # Поэтому кейс реально проверяет отказ по НЕИЗВЕСТНОМУ peer'у, а не region-mismatch;
    # переименование env-ключа `existing*`→`absent*` убирает вид «значение засеяно».
    # Настоящее покрытие REQ-TGT-PEER-REGION требует второго geo-региона (follow-up).
    id="TGT-ADD-NEG-INSTANCE-REGION-MISMATCH",
    title="AddTargets instance in different region → InvalidArgument (Verifies REQ-TGT-PEER-REGION)",
    classes=["NEG"], priority="P0",
    steps=[
        *_setup_tg("inst-region-mismatch"),
        Step(name="add-inst-other-region", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"instanceId": "{{absentInstanceCrossRegionId}}", "weight": 100}]},
             test_script=assert_refused_sync_or_async("instance_id that does not resolve in the region")),
        poll_operation_until_done(must_fail=True),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    # Тот же fixture-truth, что и у TGT-ADD-NEG-INSTANCE-REGION-MISMATCH выше:
    # `absentNicCrossRegionId` — несуществующий id (даже префикс не `nic`), а не NIC из
    # другого региона. Кейс проверяет отказ по неизвестному peer'у.
    id="TGT-ADD-NEG-NIC-REGION-MISMATCH",
    title="AddTargets NIC in different region → InvalidArgument",
    classes=["NEG"], priority="P1",
    steps=[
        *_setup_tg("nic-region-mismatch"),
        Step(name="add-nic-other-region", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"nicId": "{{absentNicCrossRegionId}}", "weight": 100}]},
             test_script=assert_refused_sync_or_async("nic_id that does not resolve in the region")),
        poll_operation_until_done(must_fail=True),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-NEG-SUBNET-REGION-MISMATCH",
    title="AddTargets ip_ref.subnet in different region → InvalidArgument",
    classes=["NEG"], priority="P1",
    steps=[
        *_setup_tg("sub-region-mismatch"),
        Step(name="add-sub-other-region", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"ipRef": {"subnetId": "{{existingSubnetCrossRegionId}}",
                                          "address": "10.0.0.5"}, "weight": 100}]},
             test_script=assert_refused_sync_or_async("ip_ref.subnet_id in another region")),
        poll_operation_until_done(must_fail=True),
        *_cleanup_tg(),
    ],
))


# ---------------------------------------------------------------------------
# IDEM / STATE
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="TGT-ADD-IDEM-DUP-INSTANCE",
    title="AddTargets repeat same instance_id → ON CONFLICT DO NOTHING (Verifies REQ-TGT-IDEM-ID)",
    classes=["IDEM"], priority="P1",
    steps=[
        *_setup_tg("dup-inst"),
        retry_until_authorized(Step(name="add-1", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"instanceId": "{{existingInstanceId}}", "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="add-2-dup", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"instanceId": "{{existingInstanceId}}", "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="rm-cleanup", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"instanceId": "{{existingInstanceId}}"}]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-IDEM-DUP-IP-REF",
    title="AddTargets repeat same ip_ref → no duplicate row",
    classes=["IDEM"], priority="P1",
    steps=[
        *_setup_tg("dup-ipref"),
        retry_until_authorized(Step(name="add-1", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"ipRef": {"subnetId": "{{existingSubnetId}}",
                                          "address": "10.180.0.30"}, "weight": 50}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="add-2-dup", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"ipRef": {"subnetId": "{{existingSubnetId}}",
                                          "address": "10.180.0.30"}, "weight": 50}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="rm", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"ipRef": {"subnetId": "{{existingSubnetId}}",
                                          "address": "10.180.0.30"}}]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-IDEM-DUP-EXTERNAL-IP",
    title="AddTargets repeat same external_ip → no duplicate",
    classes=["IDEM"], priority="P2",
    steps=[
        *_setup_tg("dup-ext"),
        retry_until_authorized(Step(name="add-1", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.40"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="add-2", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.40"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="rm", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.40"}}]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-IDEM-PROMOTE-DRAINING",
    title="Re-add DRAINING target → re-promoted to ACTIVE (ON CONFLICT DO UPDATE)",
    classes=["IDEM", "STATE"], priority="P1",
    steps=[
        *_setup_tg("promote-draining"),
        retry_until_authorized(Step(name="add", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.50"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm-phase-a", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.50"}}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="re-add", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.50"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="rm-cleanup", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.50"}}]},
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-ADD-STATE-TG-DELETING",
    title="AddTargets when TG status=DELETING → FailedPrecondition",
    classes=["STATE", "NEG"], priority="P1",
    steps=[
        Step(name="add-deleting-proxy", method="POST",
             path=f"{_TG_BASE}/{{{{garbageTgrId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.60"}, "weight": 100}]},
             test_script=[*assert_absent_id_rejected()]),
    ],
))


# ---------------------------------------------------------------------------
# RemoveTargets — 2-phase drain
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="TGT-RM-STATE-PHASE-A-DRAINING",
    title="RemoveTargets Phase A: DRAINING-mark + drain_started_at set (Verifies REQ-TGT-RM-PHASE-A)",
    classes=["STATE"], priority="P0",
    steps=[
        *_setup_tg("phase-a", dereg_seconds=300),
        retry_until_authorized(Step(name="add", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.70"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm-phase-a", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.70"}}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Утверждения БЕЗУСЛОВНЫ. Прежняя редакция обходилась `if (draining)`:
        # при исчезнувшей цели она молча не утверждала ничего, а заголовок кейса
        # при этом обещал проверить и пометку слива, и момент его начала.
        # Проверить их было НЕЧЕМ — публичная проекция цели состояния не несла
        # вовсе. Теперь несёт, и кейс спрашивает ровно то, что называет.
        Step(name="verify-still-present-as-draining", method="GET",
             path=f"{_TG_BASE}/{{{{tgId}}}}",
             test_script=[*assert_status(200),
                          "const tgts = pm.response.json().targets || [];",
                          "const draining = tgts.find(t => t.externalIp && t.externalIp.address === '203.0.113.70');",
                          "pm.test('removed target is still listed until the delay elapses', () => "
                          "  pm.expect(draining, JSON.stringify(tgts)).to.be.an('object'));",
                          "pm.test('removed target reports DRAINING', () => "
                          "  pm.expect((draining || {}).status, JSON.stringify(tgts)).to.eql('DRAINING'));",
                          "pm.test('removed target reports when draining started', () => "
                          "  pm.expect((draining || {}).drainStartedAt, JSON.stringify(tgts)).to.be.a('string'));"]),
        *_cleanup_tg(),
    ],
))

# Парная половина к предыдущему кейсу: нетронутая цель остаётся ACTIVE и без
# момента слива. Без неё утверждение «DRAINING» зеленело бы и в мире, где
# состояние проставляется всем подряд.
CASES.append(Case(
    id="TGT-RM-STATE-SIBLING-STAYS-ACTIVE",
    title="RemoveTargets Phase A: untouched sibling target keeps reporting ACTIVE",
    classes=["STATE"], priority="P1",
    steps=[
        *_setup_tg("sibling-active", dereg_seconds=300),
        retry_until_authorized(Step(name="add-two", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.71"}, "weight": 100},
                               {"externalIp": {"address": "203.0.113.72"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm-one", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.71"}}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="verify-sibling-active", method="GET", path=f"{_TG_BASE}/{{{{tgId}}}}",
             test_script=[*assert_status(200),
                          "const tgts = pm.response.json().targets || [];",
                          "const kept = tgts.find(t => t.externalIp && t.externalIp.address === '203.0.113.72');",
                          "pm.test('untouched target is still listed', () => "
                          "  pm.expect(kept, JSON.stringify(tgts)).to.be.an('object'));",
                          "pm.test('untouched target reports ACTIVE', () => "
                          "  pm.expect((kept || {}).status, JSON.stringify(tgts)).to.eql('ACTIVE'));",
                          # `null`, НЕ `undefined`: публичный mux маршалит с
                          # EmitUnpopulated=true (gateway/internal/restmux/strict_enum.go),
                          # то есть незаполненный timestamp ПРИСУТСТВУЕТ в теле как null —
                          # это объявленный tenant-facing контракт, а не пропуск. Прежнее
                          # `to.be.undefined` противоречило ему и краснело на верном ответе.
                          # Оба значения означают «момента слива нет»; настоящий timestamp
                          # по-прежнему роняет проверку, поэтому утверждение не ослаблено.
                          "pm.test('untouched target has no drain moment', () => "
                          "  pm.expect((kept || {}).drainStartedAt, JSON.stringify(tgts))"
                          "    .to.satisfy(v => v === null || v === undefined));"]),
        # Нетронутый сосед — ПРЕДМЕТ этого кейса, поэтому он остаётся в группе, и
        # уборка обязана слить его сама: непустую группу продукт удалять
        # отказывается (его объявленный контракт, см. TGR-DEL-NEG-HAS-TARGETS).
        # Прогон CI 31053251941 показал здесь ровно этот отказ — до утверждения на
        # шаге удаления он был не виден, и группа переживала каждый прогон.
        *_cleanup_tg(leftover_targets=[{"externalIp": {"address": "203.0.113.72"}}]),
    ],
))

# Состояние слива — output-only: на пути, где цель СОЗДАЁТСЯ, оно отвергается с
# именем поля. Принять его молча значило бы пообещать возможность добавить цель
# уже сливающейся, которой нет.
CASES.append(Case(
    id="TGT-ADD-VAL-STATUS-OUTPUT-ONLY",
    title="AddTargets rejects output-only target.status, naming the field",
    classes=["NEG", "VAL"], priority="P1",
    steps=[
        *_setup_tg("status-out-only"),
        retry_until_authorized(Step(name="add-with-status", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.73"}, "weight": 100,
                                "status": "DRAINING"}]},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('violation names targets[0].status', () => {",
                          "  const d = (pm.response.json().details || []).find(x => (x.fieldViolations || []).length);",
                          "  const fields = ((d || {}).fieldViolations || []).map(v => v.field);",
                          "  pm.expect(fields, JSON.stringify(pm.response.json()))"
                          "    .to.satisfy(fs => fs.some(f => String(f).endsWith('targets[0].status')));",
                          "});"]),
             retry_on=(403,)),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-RM-IDEM-NOT-PRESENT",
    title="RemoveTargets for absent identity → no-op idempotent (Verifies REQ-TGT-RM-IDEM)",
    classes=["IDEM"], priority="P1",
    steps=[
        *_setup_tg("rm-noop"),
        retry_until_authorized(Step(name="rm-absent", method="POST",
             path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.99"}}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        *_cleanup_tg(),
    ],
))

CASES.append(Case(
    id="TGT-RM-STATE-PHASE-B-RUNNER",
    title="RemoveTargets Phase B: after dereg_delay drain runner DELETEs row (Verifies REQ-TGT-RM-PHASE-B)",
    classes=["STATE"], priority="P1",
    steps=[
        # Use tiny dereg=1 so Phase B fires quickly inside test window.
        *_setup_tg("phase-b", dereg_seconds=1),
        retry_until_authorized(Step(name="add", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:addTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.80"}, "weight": 100}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="rm", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": [{"externalIp": {"address": "203.0.113.80"}}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # The Phase-A→B drain transition is async (drain runner ticks ~10s), so a single
        # read races the runner: right after removeTargets the row can still read ACTIVE.
        # Bounded self-poll (setNextRequest, busy-wait ~700ms, budget 30 ≈ 21s) waits for
        # the row to reach a drained shape — absent OR DRAINING OR INACTIVE — then asserts
        # once. A row that stays ACTIVE past the budget is a genuine runner failure and
        # reds (never masked, never infinite). Techniques: state-transition (ACTIVE→DRAINING
        # →deleted) + eventual-consistency polling.
        Step(name="poll-tg-after-drain", method="GET", path=f"{_TG_BASE}/{{{{tgId}}}}",
             test_script=["const tgts = pm.response.json().targets || [];",
                          "const t = tgts.find(x => x.externalIp && x.externalIp.address === '203.0.113.80');",
                          "const drained = (!t || t.status === 'DRAINING' || t.status === 'INACTIVE');",
                          "const _dpc = parseInt(pm.environment.get('_drainPoll') || '0', 10);",
                          "if (pm.response.code === 200 && !drained && _dpc < 30) {",
                          "  pm.environment.set('_drainPoll', String(_dpc + 1));",
                          "  const _dd = Date.now(); while (Date.now() - _dd < 700) { /* drain-runner tick wait */ }",
                          "  pm.execution.setNextRequest(pm.info.requestName);",
                          "  return;",
                          "}",
                          "pm.environment.unset('_drainPoll');",
                          *assert_status(200),
                          "pm.test('row absent or DRAINING/INACTIVE after drain runner (eventually consistent)', () => "
                          "  pm.expect(drained, JSON.stringify(tgts)).to.be.true);"]),
        *_cleanup_tg(),
    ],
))


# HTTP method semantics — collection-level endpoints belong to TargetGroupService;
# Targets has no collection endpoint of its own. Reuse the TGR collection paths.
CASES.extend(http_method_not_allowed_block("TGT", "/nlb/v1/targetGroups"))


# ---------------------------------------------------------------------------
# Extended matrix
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="TGT-RM-VAL-EMPTY-LIST",
    title="RemoveTargets with empty targets[] → InvalidArgument",
    classes=["VAL"], priority="P1",
    steps=[
        *_setup_tg("rm-empty"),
        Step(name="rm-empty", method="POST", path=f"{_TG_BASE}/{{{{tgId}}}}:removeTargets",
             body={"targets": []},
             test_script=[
                 # SYNC-VALIDATE lane, exactly like the AddTargets twin above: the
                 # empty-list guard sits in remove_targets.go:65, before the Operation is
                 # minted, so there is no async lane to tolerate and no Operation to poll
                 # (the poll that used to follow had no subject on this path). Message is
                 # contract — pinned by remove_targets_test.go:165.
                 *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test(\"message is 'at least one target is required'\", () => "
                 "  pm.expect(pm.response.json().message || '', pm.response.text())"
                 "    .to.include('at least one target is required'));",
                 "pm.environment.unset('opId');",
             ]),
        *_cleanup_tg(),
    ],
))
