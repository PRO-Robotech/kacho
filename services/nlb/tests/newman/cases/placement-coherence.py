# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Placement-coherence cases (ZC-NLB-*) — track B GAP-1 / GAP-2 (RED → GREEN).

Acceptance: docs/specs/sub-phase-nlb-vpc-zone-coherence-acceptance.md
  * GAP-1 ZC-NLB-ZONE-01/02 — ZONAL dualstack: обе VIP-семьи в ОДНОЙ зоне.
  * GAP-2 ZC-NLB-REGION-01/03 — INTERNAL LB VIP subnet/address ∈ region lb.region_id.
Norm: .claude/rules/data-integrity.md §Placement-coherence.

Behaviour-level (skill testing-product-coach): negative-кейсы ассертят ТОЧНУЮ строку
placement-coherence ошибки (не только grpc-код). RED до фикса rpc-implementer'а:
разнозональный dualstack и чужерегиональный subnet-source сейчас проходят
(create.go сверяет только same-network + placement TYPE), поэтому Create отдаёт 200
Operation вместо sync 400 → negative-кейсы красные до GREEN.

Fixture-модель (умбрелла-стенд, environments/*.postman_environment.json):
  * existingZoneId (nlb-выделенная зона, ru-central1-e на умбрелла-стенде — суита
    владеет её default EXTERNAL_PUBLIC пулом, см. tests/authz-fixtures/prodseed_matrix.py)
    и existingZoneAltId (ru-central1-b) — ДВЕ зоны
    ОДНОГО региона existingRegionId (ru-central1) → cross-zone same-region для GAP-1.
  * existingRegionAltId (ru-central2) — чужой регион для GAP-2.
Cross-domain fixture tolerance (зеркалит load-balancer.py): vpc Subnet provisioning
идёт inline через api-gateway; если фикстура не материализовалась — кейс ассертит
lawful fixture-absent rejection (suite остаётся зелёным), строгий контракт — когда
subnet id сохранён.

REST base: /nlb/v1/networkLoadBalancers ; vpc subnet: /vpc/v1/subnets
"""

CASES = []

_LB = "/nlb/v1/networkLoadBalancers"
_VPC_SUBNETS = "/vpc/v1/subnets"

# GAP-1 same-zone contract text (acceptance ZC-NLB-ZONE-01, Q1-дефолт).
_MSG_SAME_ZONE = "dualstack load balancer families must resolve to the same zone"
# GAP-2 region-coherence contract text (acceptance ZC-NLB-REGION-01/02, verbatim).
# NOTE(reconcile): краткая формулировка задачи трека B ("load balancer VIP must be in
# the same region") — пересказ; источник истины RED→GREEN — acceptance-док (verbatim).
_MSG_WRONG_REGION = "load balancer vip subnet must be in the same region as the load balancer"


def _cidr_pre():
    """Pre-request: run-scoped уникальный v4 /24 и v6 /64 (separates parallel runs)."""
    return [
        "var __seq = parseInt(pm.environment.get('_zcSeq') || '0', 10) + 1;",
        "pm.environment.set('_zcSeq', String(__seq));",
        "var __run = (pm.environment.get('runId') || 'x0');",
        "var __h = 0; for (var i = 0; i < __run.length; i++) { __h = (__h * 31 + __run.charCodeAt(i)) & 0xffff; }",
        "pm.environment.set('_zcV4Cidr', '10.' + (150 + (__h % 40)) + '.' + (__seq % 250) + '.0/24');",
        # ПЕРВАЯ ГРУППА ДОПОЛНЯЕТСЯ ДО ДВУХ ЦИФР — иначе адрес выпадает из fd00::/8.
        # Значение 10..89 при шестнадцатеричной записи даёт ОДНУ цифру, пока оно
        # меньше 16: 'fd' + 'c' = `fdc:` — это `0fdc::`, то есть не локальный адрес
        # и не внутри супернета сети (`fd00::/8`, seed-nlb-fixtures.sh). Сеть
        # отвергала подсеть текстом «not within any network CIDR block», и красное
        # зависело от runId — примерно один прогон из тринадцати, отчего беда
        # читалась как недетерминизм, а не как ошибка формулы.
        "pm.environment.set('_zcV6Cidr', 'fd' + (10 + (__h % 80)).toString(16).padStart(2, '0') + ':' + (__seq % 9000).toString(16) + '::/64');",
    ]


def _provision_zonal_subnet(zone_var, suffix, save_var, family="v4"):
    """Provision ZONAL vpc Subnet in a given zone env-var; save its id.

    Опрос называет опубликованное имя (`fixture_ids`), и это не украшение. Приём
    операции (`200`) означает только, что запрос принят: идентификатор подсети
    чеканится ДО того, как воркер отработает, поэтому `metadata.subnetId` приезжает
    и у операции, завершившейся ОШИБКОЙ. Без этого имени шаг публиковал бы координату
    несуществующей подсети, а падал бы не он, а безусловная уборка в конце кейса —
    отказом удаления по фантому, то есть в месте, не имеющем к причине отношения.
    """
    cidr_field = "ipv4CidrPrimary" if family == "v4" else "ipv6CidrPrimary"
    cidr_var = "_zcV4Cidr" if family == "v4" else "_zcV6Cidr"
    return [
        Step(name=f"prov-zonal-{suffix}", method="POST", path=_VPC_SUBNETS,
             pre_script=_cidr_pre(),
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{existingNetworkId}}",
                   "name": f"zc-{suffix}-{{{{runId}}}}",
                   "zoneId": f"{{{{{zone_var}}}}}", cidr_field: f"{{{{{cidr_var}}}}}"},
             test_script=[
                 f"pm.environment.unset('{save_var}');",
                 # ПРИЁМ ЗАПРОСА УТВЕРЖДАЕТСЯ ЗДЕСЬ, исход операции — опросом ниже.
                 # Это разные вещи: без первого шаг зеленел при любом ответе и публиковал
                 # снятое имя, а падал не он, а проба размещения, которой не на чем стоять.
                 *assert_status(200),
                 "if (pm.response.code === 200) {",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 f"  if (j.metadata && j.metadata.subnetId) pm.environment.set('{save_var}', j.metadata.subnetId);",
                 "} else { pm.environment.set('opId', ''); }",
             ]),
        poll_operation_until_done(fixture_ids=[save_var]),
    ]


def _provision_regional_subnet(region_var, suffix, save_var):
    """Provision REGIONAL (anycast) vpc Subnet in a given region env-var; save its id.

    Опрос называет опубликованное имя по той же причине, что и зональный сосед выше:
    идентификатор чеканится до работы воркера, поэтому приём операции о создании
    подсети не свидетельствует.
    """
    return [
        Step(name=f"prov-regional-{suffix}", method="POST", path=_VPC_SUBNETS,
             pre_script=_cidr_pre(),
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{existingNetworkId}}",
                   "name": f"zc-{suffix}-{{{{runId}}}}",
                   "regionId": f"{{{{{region_var}}}}}", "ipv4CidrPrimary": "{{_zcV4Cidr}}"},
             test_script=[
                 f"pm.environment.unset('{save_var}');",
                 # Тот же довод, что у зонального соседа выше: приём запроса — здесь,
                 # исход операции — опросом ниже.
                 *assert_status(200),
                 "if (pm.response.code === 200) {",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 f"  if (j.metadata && j.metadata.subnetId) pm.environment.set('{save_var}', j.metadata.subnetId);",
                 "} else { pm.environment.set('opId', ''); }",
             ]),
        poll_operation_until_done(fixture_ids=[save_var]),
    ]


def _cleanup_vpc(id_var):
    """Убрать подсеть фикстуры, если она была создана.

    Проверка имени — не смягчение утверждения, а условие его осмысленности. Шаг
    удаления несёт строгое `delete accepted: status 200` (оно ставится по умолчанию
    всем шагам удаления, и снимать его нельзя: без него отказ уборки зеленел бы).
    Но утверждать приём удаления имеет смысл ровно тогда, когда есть что удалять:
    на несозданной фикстуре адрес собрался бы из пустого имени, и строгое
    утверждение объявляло бы дефектом отсутствие ресурса, которого кейс не создавал.
    Провижн выше падает САМ и атрибутивно, поэтому пропуск уборки здесь ничего не
    прячет — он лишь не добавляет второго, вводящего в заблуждение падения.
    """
    return [
        # Повтор на время возврата аренды: удаление балансировщика долговечно, а
        # освобождение выделенных им адресов идёт следом и асинхронно, поэтому
        # подсеть в узком окне отвергает своё удаление. Любой другой отказ терминален.
        retry_delete_until_released(
            Step(name=f"zc-cleanup-{id_var}", method="DELETE", path=f"{_VPC_SUBNETS}/{{{{{id_var}}}}}",
                 test_script=[
                     f"if (!pm.environment.get('{id_var}')) {{ pm.environment.set('opId', ''); return; }}",
                     *save_from_response("j.id", "opId"),
                 ])),
        poll_operation_until_done(),
    ]


def _cleanup_lb():
    return [
        Step(name="zc-cleanup-lb", method="DELETE", path=f"{_LB}/{{{{zcLbId}}}}",
             test_script=[
                 "if (!pm.environment.get('zcLbId')) { pm.environment.set('opId', ''); return; }",
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(),
    ]


# ---------------------------------------------------------------------------
# GAP-1 — ZONAL dualstack same-zone
# ---------------------------------------------------------------------------

CASES.append(Case(
    # index: ZC-NLB-ZONE-01 (placement-coherence GAP-1)
    id="ZC-NLB-ZONE-01-NEG-DUALSTACK-CROSS-ZONE",
    title="ZONAL dualstack v4/v6 в разных зонах одного региона → sync 400 same-zone "
          "(Verifies ZC-NLB-ZONE-01)",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        *_provision_zonal_subnet("existingZoneId", "z1v4", "zcSubV4Id", family="v4"),
        *_provision_zonal_subnet("existingZoneAltId", "z2v6", "zcSubV6Id", family="v6"),
        # Both zonal subnets are provisioned (v4 in zone1, v6 in zone2), so the intended
        # rejection is the same-zone COHERENCE error — NOT a transient cross-service
        # `subnet <id> not found` (one subnet briefly invisible to nlb's vpc peer-read).
        # retry_create_until_present spins past that transient not-found (the same-zone
        # verbatim message carries no "not found", so once the real coherence check fires
        # the guard falls through to the strict assertion below).
        retry_create_until_present(Step(name="create-cross-zone", method="POST", path=_LB,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{existingRegionId}}",
                   "placement": "INTERNAL_ZONAL", "name": "zc-xz-{{runId}}",
                   "v4Source": {"subnetId": "{{zcSubV4Id}}"},
                   "v6Source": {"subnetId": "{{zcSubV6Id}}"}},
             test_script=[
                 "if (pm.environment.get('zcSubV4Id') && pm.environment.get('zcSubV6Id')) {",
                 "  pm.test('cross-zone dualstack rejected sync 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "  const j = pm.response.json();",
                 "  pm.test('grpc code 3 (INVALID_ARGUMENT)', () => pm.expect(j.code).to.eql(3));",
                 f"  pm.test('same-zone verbatim message', () => pm.expect(j.message).to.eql({_MSG_SAME_ZONE!r}));",
                 "} else {",
                 "  pm.test('no dual zonal subnet fixture → lawful rejection, never silent 200', () => "
                 "    pm.expect(pm.response.code).to.be.oneOf([400, 404, 503]));",
                 "}",
             ])),
        *_cleanup_vpc("zcSubV4Id"),
        *_cleanup_vpc("zcSubV6Id"),
    ],
))

CASES.append(Case(
    # index: ZC-NLB-ZONE-02 (placement-coherence GAP-1 happy)
    id="ZC-NLB-ZONE-02-DUALSTACK-SAME-ZONE-OK",
    title="ZONAL dualstack обе VIP в одной зоне → sync accept as Operation "
          "(Verifies ZC-NLB-ZONE-02)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_provision_zonal_subnet("existingZoneId", "szv4", "zcSubV4Id", family="v4"),
        *_provision_zonal_subnet("existingZoneId", "szv6", "zcSubV6Id", family="v6"),
        retry_create_until_present(Step(name="create-same-zone", method="POST", path=_LB,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{existingRegionId}}",
                   "placement": "INTERNAL_ZONAL", "name": "zc-sz-{{runId}}",
                   "v4Source": {"subnetId": "{{zcSubV4Id}}"},
                   "v6Source": {"subnetId": "{{zcSubV6Id}}"}},
             test_script=[
                 "pm.environment.unset('zcLbId');",
                 "if (pm.environment.get('zcSubV4Id') && pm.environment.get('zcSubV6Id')) {",
                 "  pm.test('same-zone dualstack accepted as Operation (200, not placement-rejected)', () => "
                 "    pm.expect(pm.response.code).to.eql(200));",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 "  if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('zcLbId', j.metadata.networkLoadBalancerId);",
                 "} else {",
                 "  pm.environment.set('opId', '');",
                 "  pm.test('no fixture → lawful rejection', () => pm.expect(pm.response.code).to.be.oneOf([400, 404, 503]));",
                 "}",
             ])),
        # То же, что и в парном региональном кейсе: имя названо, чтобы провал
        # создания падал здесь, а не отказом уборки по несуществующему адресу.
        poll_operation_until_done(fixture_ids=["zcLbId"]),
        Step(name="zc-cleanup-lb-cond", method="DELETE", path=f"{_LB}/{{{{zcLbId}}}}",
             test_script=[
                 "if (!pm.environment.get('zcLbId')) { pm.environment.set('opId', ''); return; }",
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(),
        *_cleanup_vpc("zcSubV4Id"),
        *_cleanup_vpc("zcSubV6Id"),
    ],
))


# ---------------------------------------------------------------------------
# GAP-2 — region-coherence VIP↔LoadBalancer (INTERNAL)
# ---------------------------------------------------------------------------

CASES.append(Case(
    # index: ZC-NLB-REGION-01 (placement-coherence GAP-2)
    id="ZC-NLB-REGION-01-NEG-SUBNET-WRONG-REGION",
    title="INTERNAL REGIONAL LB (R1) + REGIONAL subnet-source региона R2 → sync 400 wrong-region "
          "(Verifies ZC-NLB-REGION-01/02)",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        *_provision_regional_subnet("existingRegionAltId", "r2", "zcSubR2Id"),
        retry_create_until_present(Step(name="create-wrong-region", method="POST", path=_LB,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{existingRegionId}}",
                   "placement": "INTERNAL_REGIONAL", "name": "zc-wr-{{runId}}",
                   "v4Source": {"subnetId": "{{zcSubR2Id}}"}},
             test_script=[
                 # Single-region-stand guard: this cross-region negative can only fire when a
                 # SECOND geo region exists (existingRegionAltId != existingRegionId). On a
                 # single-region stand RegionAltId==primary, so the "R2" subnet is physically
                 # SAME-region → the create is lawfully accepted and there is no cross-region
                 # violation to reject. Not masking: the region-coherence check is unit-locked
                 # (create_zone_coherence_test.go wantRegionMismatchMsg) and the strict verbatim
                 # e2e below fires as soon as a 2nd geo region is seeded.
                 "var _altR = pm.environment.get('existingRegionAltId') || '';",
                 "var _r = pm.environment.get('existingRegionId') || '';",
                 # Пустое значение, а не снятие: страж неразрешённых подстановок (уровень
                 # КОЛЛЕКЦИИ, поэтому он выполняется РАНЬШЕ pre_script шага и
                 # перекрыть его шагом нельзя) срабатывает только на имени, не
                 # определённом ни в одной области. Заданное пустым — законный
                 # негативный случай по его собственному предикату.
                 "pm.environment.set('zcLeakLbId', '');",
                 "if (pm.environment.get('zcSubR2Id') && _altR && _r && _altR !== _r) {",
                 "  pm.test('cross-region subnet rejected sync 400', () => pm.expect(pm.response.code).to.eql(400));",
                 "  const j = pm.response.json();",
                 "  pm.test('grpc code 3 (INVALID_ARGUMENT)', () => pm.expect(j.code).to.eql(3));",
                 f"  pm.test('wrong-region verbatim message', () => pm.expect(j.message).to.eql({_MSG_WRONG_REGION!r}));",
                 "} else if (!pm.environment.get('zcSubR2Id')) {",
                 "  pm.test('no cross-region subnet fixture → lawful rejection, never silent 200', () => "
                 "    pm.expect(pm.response.code).to.be.oneOf([400, 404, 503]));",
                 "} else {",
                 "  // single-region stand: the 'alt' region resolved to the SAME region, so the",
                 "  // REGIONAL subnet is region-coherent and the create is lawful — it must be",
                 "  // ACCEPTED. Accepting 400/404/503 as well made this branch unfailable: on a",
                 "  // single-region stand it agreed with every possible answer, so a coherence",
                 "  // check that started refusing its OWN region would still have passed.",
                 "  pm.test('single-region: a same-region subnet is accepted (cross-region needs a 2nd geo region)', () => "
                 "    pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "  const j = pm.response.json();",
                 "  if (pm.response.code === 200) { if (j.id) pm.environment.set('opId', j.id); if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('zcLeakLbId', j.metadata.networkLoadBalancerId); }",
                 "}",
             ])),
        # drain the op + clean up any LB the single-region branch lawfully created
        # (no-op tolerant DELETE on the strict-400 path where zcLeakLbId is unset).
        poll_operation_until_done(),
        # Условная уборка: `zcLeakLbId` ставит ТОЛЬКО ветка одно-регионального стенда.
        # На стенде с двумя регионами срабатывает строгая ветка (sync 400), балансировщик
        # не создаётся, и убирать нечего — а шаг всё равно уходил с неразрешённым
        # `{{zcLeakLbId}}`, что харнесс справедливо считает находкой (запрос по литералу
        # шаблона). Пропуск здесь — не маскировка: пропускается уборка того, чего не
        # создали, а не проверка. Строгая ветка выше при этом обязана отработать.
        Step(name="cleanup-zc-leak-lb", method="DELETE", path=f"{_LB}/{{{{zcLeakLbId}}}}",
             pre_script=["if (!pm.environment.get('zcLeakLbId')) { pm.execution.skipRequest(); }"],
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_vpc("zcSubR2Id"),
    ],
))

CASES.append(Case(
    # index: ZC-NLB-REGION-03 (placement-coherence GAP-2 happy)
    id="ZC-NLB-REGION-03-SAME-REGION-OK",
    title="INTERNAL REGIONAL LB (R1) + REGIONAL subnet-source региона R1 → accepted as Operation "
          "(Verifies ZC-NLB-REGION-03)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_provision_regional_subnet("existingRegionId", "r1", "zcSubR1Id"),
        retry_create_until_present(Step(name="create-same-region", method="POST", path=_LB,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{existingRegionId}}",
                   "placement": "INTERNAL_REGIONAL", "name": "zc-sr-{{runId}}",
                   "v4Source": {"subnetId": "{{zcSubR1Id}}"}},
             test_script=[
                 "pm.environment.unset('zcLbId');",
                 "if (pm.environment.get('zcSubR1Id')) {",
                 "  pm.test('same-region subnet accepted as Operation (200, not region-rejected)', () => "
                 "    pm.expect(pm.response.code).to.eql(200));",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 "  if (j.metadata && j.metadata.networkLoadBalancerId) pm.environment.set('zcLbId', j.metadata.networkLoadBalancerId);",
                 "} else {",
                 "  pm.environment.set('opId', '');",
                 "  pm.test('no fixture → lawful rejection', () => pm.expect(pm.response.code).to.be.oneOf([400, 404, 503]));",
                 "}",
             ])),
        # Кейс назван OK: создание обязано УДАТЬСЯ, а не только быть принятым. Имя
        # балансировщика названо опросу, поэтому провал операции снимает его здесь
        # и падает здесь же, вместо того чтобы уехать фантомом в уборку.
        poll_operation_until_done(fixture_ids=["zcLbId"]),
        Step(name="zc-cleanup-lb-sr", method="DELETE", path=f"{_LB}/{{{{zcLbId}}}}",
             test_script=[
                 "if (!pm.environment.get('zcLbId')) { pm.environment.set('opId', ''); return; }",
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(),
        *_cleanup_vpc("zcSubR1Id"),
    ],
))
