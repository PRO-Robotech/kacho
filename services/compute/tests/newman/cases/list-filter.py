# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для D-consumer (§11, D-40..D-47) — per-object filtered List в kacho-compute.

Источник истины — docs/specs/rbac-rules-model-2026-acceptance.md под-фаза D
(LST-1..6, D-40..D-47); workspace issue PRO-Robotech/kacho-workspace#111.

Что проверяем (black-box через api-gateway, реальный iam + OpenFGA в стенде):

  - D-40/D-45 read==enforce (happy): авторизованный субъект (jwtProjectAdminA1)
    видит СВОИ объекты в List. Это и есть RED→GREEN-пара D-consumer:
    до фикса compute слал в iam.ListObjects action="compute.instances.read"
    (verb "read"), который iam-сервер НЕ мапит на relation → отвечает
    InvalidArgument → compute оборачивает в Unavailable (503) → КАЖДЫЙ List
    ломается при list-filter.enabled=true. После фикса verb="list" → iam мапит
    на "viewer" (та же relation, что per-RPC Check для Get == read==enforce) →
    List возвращает 200 и доступные объекты.

  - D-44 no-leak (negative): well-formed-но-отсутствующий instanceId →
    Get == 404 NOT_FOUND (НЕ 403 PERMISSION_DENIED — existence не
    подтверждается) и объект отсутствует в List. read==enforce: List-видимость
    == Check-allow поверх тех же materialized tuples + scope_grant.

  - D-44 cross-account no-leak: jwtAccountAdminB НЕ видит instance проекта A1
    в своём scope (per-object фильтрация, не all-or-nothing leak).

Pre-conditions: tests/authz-fixtures/setup.sh (те же JWT/проекты, что authz-deny).
Требует list-filter.enabled=true на стенде (KACHO_COMPUTE_LIST_FILTER_ENABLED).
"""

CASES = []

INSTANCES = "/compute/v1/instances"
MT_INT = "/compute/v1/internal/machineTypes"   # admin seed (:8081, ban #6)


def _seed_mt(suffix, id_var="mtId"):
    """Seed a MachineType via InternalMachineTypeService.Create (:8081) so the instance
    Create can resolve machineTypeId in the catalog (doCreate → resolveMachineType;
    catalog empty on the stand). Uses the DEFAULT bootstrap cluster-admin Bearer (the
    Internal admin-CRUD is system_admin — jwtProjectAdminA1 cannot seed it), so NO per-step
    auth override. Sets id_var to the mt- id. Mirrors instance-redesign _seed_mt (COMP-1 F7);
    {{runId}}-unique name (UNIQUE(name) cluster-wide)."""
    nm = f"lfmt{suffix}{{{{runId}}}}"
    body = {"name": nm, "family": "STANDARD",
            "effectiveResources": {"vCpu": 2, "memoryMib": 8192, "gpus": 0},
            "availableZones": ["{{existingZoneId}}", "{{existingZoneAltId}}"], "status": "AVAILABLE"}
    return [Step(name=f"lf-seed-mt-{suffix}", method="POST", path=MT_INT, body=body, internal=True,
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                              *save_from_response("j.metadata && j.metadata.machineTypeId", id_var)]),
            poll_operation_until_done(), assert_op_success()]


def _cleanup_mt(suffix, id_var="mtId"):
    """Delete the seeded MachineType (default bootstrap admin auth, as _seed_mt)."""
    return [Step(name=f"lf-cleanup-mt-{suffix}", method="DELETE", path=MT_INT + "/{{" + id_var + "}}",
                 internal=True, test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done()]


def _instance_body(name_suffix, project_var, mt="{{mtId}}"):
    # COMP-1 redesign contract (mirrors instance-redesign _vm_body happy-path):
    #   instanceKind=VM · machineTypeId (single sizing channel) · bootSource{type,id} ·
    #   vmSpec (kind-matching spec-arm). Legacy platformId/resourcesSpec/bootDiskSpec are
    #   RESERVED in CreateInstanceRequest (ban #2) → the old body 400'd 'instanceKind is
    #   required'. NIC via networkInterfaceSpecs (existence-validate → COMP-2);
    #   sshPublicKeys lifts the F5 unreachable-guard (VM reachable).
    return {
        "projectId": f"{{{{{project_var}}}}}",
        "name": f"lf-inst-{name_suffix}-{{{{runId}}}}",
        "zoneId": "{{existingZoneId}}",
        "instanceKind": "VM",
        "machineTypeId": mt,
        "bootSource": {"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts"},
        "vmSpec": {"userData": "#cloud-config\n{}",
                   "metadataOptions": {"metadataEndpoint": "ENABLED", "metadataTokenRequired": True}},
        "networkInterfaceSpecs": [{"subnetId": "{{existingSubnetId}}", "securityGroupIds": ["{{existingSgId}}"]}],
        "sshPublicKeys": ["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexampledeadbeefkey lf@team"],
    }


# ---------------------------------------------------------------------------
# D-40/D-45 — read==enforce happy: owner sees own instance in (filtered) List.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="LF-INST-LST-READ-ENFORCE-OWNER-SEES-OWN",
    title="[D-40/D-45] PA1 создаёт instance в project-A1 и видит его в filtered List (read==enforce, verb→viewer)",
    classes=["AUTHZ", "POS", "LST"], priority="P0",
    steps=[
        *_seed_mt("own"),
        Step(name="create-own", method="POST", path=INSTANCES,
             body=_instance_body("own", "projectA1Id"), auth="jwtProjectAdminA1",
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.instanceId", "lfInstanceId")]),
        poll_operation_until_done(auth="jwtProjectAdminA1"), assert_op_success(auth="jwtProjectAdminA1"),
        # filtered List as the SAME (authorized) subject → 200 + own instance visible.
        # retry_until_present: read-your-writes over owner-tuple/list-authz materialization
        # (opgate removed → EC); wraps ONLY the owner's positive self-list (fail-open at budget
        # → a genuine over-hide still FAILS). Negatives below stay single-shot.
        retry_until_present(Step(name="list-own", method="GET",
             path=f"{INSTANCES}?projectId={{{{projectA1Id}}}}&pageSize=1000",
             auth="jwtProjectAdminA1",
             test_script=[*assert_status(200),
                          "const insts = pm.response.json().instances || [];",
                          "pm.test('[D-45] filtered List returns 200 (not 503/InvalidArgument from broken verb)', () => pm.expect(pm.response.code).to.eql(200));",
                          "const mine = insts.find(x => x.id === pm.environment.get('lfInstanceId'));",
                          "pm.test('[D-40] owner sees own instance in filtered List (read==enforce)', () => pm.expect(mine, JSON.stringify(insts.map(i=>i.id))).to.be.an('object'));"]),
            "lfInstanceId"),
        # cleanup.
        Step(name="del-own", method="DELETE", path=f"{INSTANCES}/{{{{lfInstanceId}}}}",
             auth="jwtProjectAdminA1",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtProjectAdminA1"),
        *_cleanup_mt("own"),
    ],
))


# ---------------------------------------------------------------------------
# D-44 — no-leak: well-formed-but-absent id → 404 (NOT 403), not in List.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="LF-INST-GET-NOLEAK-404-NOT-403",
    title="[D-44] PA1 Get well-formed-но-отсутствующего instanceId → 404 NOT_FOUND (no-leak, не 403)",
    classes=["AUTHZ", "NEG", "LST"], priority="P0",
    steps=[
        Step(name="get-absent", method="GET", path=f"{INSTANCES}/{{{{garbageComputeId}}}}",
             auth="jwtProjectAdminA1",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('[D-44] no-leak: NOT_FOUND, not PERMISSION_DENIED', () => pm.expect(pm.response.json().code).to.not.eql(7));"]),
    ],
))


# ---------------------------------------------------------------------------
# Over-show leak guard — subject-source: list-filter обязан брать subject из
# request Principal (x-kacho-principal-*), а НЕ из несуществующих x-kacho-subject*
# заголовков. До фикса subject="" → bypass-all → List возвращал ВСЕ объекты
# проекта мимо list-authz (existence+metadata leak).
#
# Проверка: jwtPureNoBindings — аутентифицированный субъект БЕЗ грантов в project-A1.
# Его List project-A1 обязан быть пустым (fail-closed), а instance, созданный
# PA1, не должен в нём появиться. RED при subject-source bug (bypass-all утекал
# instance), GREEN после фикса (principal-based subject → пустой allow-list).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="LF-INST-LST-OVERSHOW-LEAK-GUARD",
    title="[leak] jwtPureNoBindings List project-A1 → instance PA1 не виден (subject из principal, fail-closed)",
    classes=["AUTHZ", "NEG", "LST"], priority="P0",
    steps=[
        *_seed_mt("leak"),
        Step(name="create-a1-pa1", method="POST", path=INSTANCES,
             body=_instance_body("leak", "projectA1Id"), auth="jwtProjectAdminA1",
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.instanceId", "lfLeakInstanceId")]),
        poll_operation_until_done(auth="jwtProjectAdminA1"), assert_op_success(auth="jwtProjectAdminA1"),
        # Authenticated-but-not-granted subject lists project-A1. The handler
        # derives subject from the principal and consults the filter (empty
        # allow-list) → MUST NOT leak the PA1 instance. A non-empty result here
        # is the over-show leak.
        #
        # kacho-iam#276 root-cause fix — this reads jwtPureNoBindings, a DEDICATED subject
        # that NO suite EVER grants (setup.sh), instead of the doubly-used jwtNoBindings
        # (which the iam access-binding suites grant `view@account-A`, so under the parallel
        # fan-out account→project containment transiently made project-A1 instances
        # v_list-visible to NOB → false leak). A guaranteed binding-free principal makes this
        # a STRICT single-shot guard (no retry-mask): the PA1 instance MUST be absent — a
        # GENUINE over-show hole still FAILS the assertion honestly.
        Step(name="list-a1-as-pure-nob", method="GET",
             path=f"{INSTANCES}?projectId={{{{projectA1Id}}}}&pageSize=1000",
             auth="jwtPureNoBindings",
             test_script=[
                 "pm.test('[leak] response is not a server error (fail-closed, not 5xx)', () => pm.expect(pm.response.code).to.be.oneOf([200, 403]));",
                 "const insts = (pm.response.json().instances) || [];",
                 "pm.test('[leak] PA1 instance NOT leaked to a never-granted subject', () => pm.expect(insts.map(x=>x.id)).to.not.include(pm.environment.get('lfLeakInstanceId')));",
             ]),
        # cleanup as owner.
        Step(name="del-a1-leak", method="DELETE", path=f"{INSTANCES}/{{{{lfLeakInstanceId}}}}",
             auth="jwtProjectAdminA1",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtProjectAdminA1"),
        *_cleanup_mt("leak"),
    ],
))


# ---------------------------------------------------------------------------
# D-44 — cross-account no-leak: AAB не видит instance проекта A1 в своём scope.
# (AAB List своего project-B1 → не содержит A1-объект; A1-проект для AAB — DENY,
#  поэтому per-object изоляция проверяется тем, что A1-instance не утекает в B1.)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="LF-INST-LST-CROSS-ACCOUNT-NO-LEAK",
    title="[D-44] AAB List instances project-B1 → instance проекта A1 не виден (per-object изоляция)",
    classes=["AUTHZ", "NEG", "LST"], priority="P1",
    steps=[
        *_seed_mt("xacct"),
        # PA1 создаёт instance в A1.
        Step(name="create-a1", method="POST", path=INSTANCES,
             body=_instance_body("xacct", "projectA1Id"), auth="jwtProjectAdminA1",
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.instanceId", "lfXacctInstanceId")]),
        poll_operation_until_done(auth="jwtProjectAdminA1"), assert_op_success(auth="jwtProjectAdminA1"),
        # AAB листит СВОЙ project-B1 → A1-instance не должен присутствовать.
        Step(name="list-b1-as-aab", method="GET",
             path=f"{INSTANCES}?projectId={{{{projectB1Id}}}}&pageSize=1000",
             auth="jwtAccountAdminB",
             test_script=[*assert_status(200),
                          "const ids = (pm.response.json().instances || []).map(x => x.id);",
                          "pm.test('[D-44] cross-account: A1 instance not leaked into B1 List', () => pm.expect(ids).to.not.include(pm.environment.get('lfXacctInstanceId')));"]),
        # cleanup as owner.
        Step(name="del-a1", method="DELETE", path=f"{INSTANCES}/{{{{lfXacctInstanceId}}}}",
             auth="jwtProjectAdminA1",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtProjectAdminA1"),
        *_cleanup_mt("xacct"),
    ],
))
