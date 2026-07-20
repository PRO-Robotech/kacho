# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для redesign-поверхности VPC-1 (Network supernet + Subnet placement-anchor).

Источник истины — APPROVED acceptance `docs/specs/sub-phase-VPC-1-network-subnet-acceptance.md`
(сценарии VPC-1-06..46) + сверка с фактическим поведением хендлеров kacho-vpc
(`services/vpc/internal/apps/kacho/api/{network,subnet}/*.go`) на этой ветке
(`redesign/integration`). Все ассёршены — **grounded** на реальные коды/тексты, а не на
target-формулировки дока: где редизайн-цель ещё AS-IS (напр. placement-absent-код,
overlap-регистр «Subnet CIDRs can not overlap» с заглавной, delete-non-empty
«Network <id> is not empty»), кейс локает **фактическую** строку контракта.

Покрываемая redesign-поверхность (happy + negative):
  Network — declared супернет `ipv4CidrBlocks`/`ipv6CidrBlocks` (immutable через Update,
    мутируется verb-pair :add-cidr-blocks / :remove-cidr-blocks); op-in-response (RunSync →
    Operation.done==true сразу); system default-SG в response; two-projection (public Network
    без vrfId); RemoveCidrBlocks блока, покрывающего живую подсеть → FAILED_PRECONDITION
    (hardening: скан ВСЕХ подсетей, не 1-й страницы); dup-name → ALREADY_EXISTS;
    Delete non-empty → FAILED_PRECONDITION.
  Subnet — placementType° server-derived из zoneId XOR regionId (explicit reject both/neither/
    placementType-in-body); ipv4CidrPrimary ⊆ супернет (∉ → InvalidArgument); no-overlap per-network
    (EXCLUDE → FAILED_PRECONDITION); AddCidrBlocks доп. диапазон (вне супернета → op-error
    InvalidArgument); RemoveCidrBlocks primary-anchor → op-error InvalidArgument immutable
    (hardening); immutables (zone_id/network_id); absent/malformed networkId; DhcpOptions снят;
    List-filter whitelist zone_id/network_id; pagination-validate; v6-only edge.

Helpers инжектятся gen.py через namespace модуля (Step, Case, assert_status, assert_grpc_code,
save_from_response, poll_operation_until_done, retry_until_authorized, retry_until_present).

Discipline (testing.md):
  - op-in-response: мутация возвращает HTTP 200 + Operation{done:true}; happy → response/GET,
    negative-в-worker'е → j.error.{code,message} прямо в теле мутации (poll не нужен).
  - sync-negative (валидация ДО создания Operation): HTTP 4xx напрямую. Subnet.Create scope —
    {project, project_id} (грант фикстуры стабилен) → authz-first не маскирует backend-код.
  - read-your-writes: ПЕРВЫЙ Get/Update/Delete/verb СВОЕГО свежего Network/Subnet обёрнут
    retry_until_authorized(retry_on=(403,)) поверх owner-tuple EC-окна. Negatives НЕ оборачиваем.
  - {{runId}}-суффикс на UNIQUE(name); CIDR-октеты — run-random энтропия (vpc1oct из hash(runId))
    вместо фикс-CIDR; per-case fixtures + cleanup (self-contained, `run.sh --service vpc1`).
"""

CASES = []

# ---------------------------------------------------------------------------
# Run-random CIDR-октет: hash(runId) → [1..250]. Стабилен в пределах одного run
# (одинаков для всех кейсов коллекции), различается между прогонами/ветками —
# rerun-safe. Подсети изолированы per-network (EXCLUDE scope = network_id, VPC-1-32),
# сети создаются свежими per-case → фикс-октет в пределах run коллизий не даёт.
# Сетевые супернеты между сетями пересекаться могут (нет cross-network constraint).
# ---------------------------------------------------------------------------
_OCT_PRE = [
    "if (!pm.environment.get('vpc1oct') || pm.environment.get('_vpc1octRun') !== pm.environment.get('runId')) {",
    "  let h = 5381; const s = pm.environment.get('runId') || 'x';",
    "  for (let i = 0; i < s.length; i++) { h = ((h * 33) ^ s.charCodeAt(i)) >>> 0; }",
    "  pm.environment.set('vpc1oct', String(1 + (h % 250)));",
    "  pm.environment.set('_vpc1octRun', pm.environment.get('runId'));",
    "}",
]

_SUPERNET_V4 = "10.{{vpc1oct}}.0.0/16"
_SUPERNET_V6 = "fd00:{{vpc1oct}}::/48"


def _assert_op_in_response():
    """Op-in-response контракт: HTTP 200 + Operation{done:true} прямо в ответе мутации
    (RunSync — worker отработал синхронно; poll не требуется, VPC-1-14/40)."""
    return [
        "pm.test('op-in-response: done:true immediately', () => "
        "pm.expect(pm.response.json().done, JSON.stringify(pm.response.json())).to.eql(true));",
    ]


def _assert_op_error(code, code_name, msg_regex):
    """Negative, поднятый ВНУТРИ worker-fn (RunSync) → embedded в Operation.error;
    HTTP 200 + Operation{done:true, error:{code,message}} прямо в ответе мутации."""
    return [
        *assert_status(200),
        "pm.test('op done:true with embedded error', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.done, JSON.stringify(j)).to.eql(true);",
        "  pm.expect(j.error, JSON.stringify(j)).to.be.an('object');",
        "});",
        f"pm.test('op.error.code {code} ({code_name})', () => "
        f"pm.expect(pm.response.json().error.code, JSON.stringify(pm.response.json())).to.eql({code}));",
        f"pm.test('op.error.message matches {msg_regex}', () => "
        f"pm.expect(pm.response.json().error.message, JSON.stringify(pm.response.json())).to.match({msg_regex}));",
    ]


def _net_create_step(name_suffix, v4=None, v6=None, extra_test=None, want_default_sg=False):
    """Первый шаг кейса: Create Network (op-in-response). Несёт _OCT_PRE.
    Scope authz — {project, project_id}: грант фикстуры стабилен → retry не нужен."""
    body = {"projectId": "{{_suiteProjectId}}", "name": f"v1{name_suffix}-{{{{runId}}}}"}
    if v4 is not None:
        body["ipv4CidrBlocks"] = v4
    if v6 is not None:
        body["ipv6CidrBlocks"] = v6
    test = [
        *assert_status(200),
        *_assert_op_in_response(),
        *save_from_response("j.id", "opId"),
        *save_from_response("j.metadata && j.metadata.networkId", "netId"),
    ]
    if extra_test:
        test += extra_test
    return Step(name="mk-net", method="POST", path="/vpc/v1/networks", body=body,
                pre_script=list(_OCT_PRE), test_script=test)


def _subnet_create_step(name_suffix, extra_body, save_id="subId", extra_test=None):
    """Create Subnet (op-in-response). Scope authz — {project, project_id}: retry не нужен."""
    body = {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
            "name": f"v1{name_suffix}-{{{{runId}}}}", **extra_body}
    test = [
        *assert_status(200),
        *_assert_op_in_response(),
        *save_from_response("j.id", "opId"),
        *save_from_response("j.metadata && j.metadata.subnetId", save_id),
    ]
    if extra_test:
        test += extra_test
    return Step(name="mk-sub", method="POST", path="/vpc/v1/subnets", body=body,
                test_script=test)


def _cleanup_subnet(id_var="subId"):
    # DELETE своей свежей подсети — первый мутирующий доступ → retry на transient 403
    # (owner-tuple EC). Терминальный 200/400/404 — реальный assert (lenient cleanup).
    return retry_until_authorized(
        Step(name="cleanup-sub", method="DELETE", path=f"/vpc/v1/subnets/{{{{{id_var}}}}}",
             test_script=["pm.test('cleanup subnet (200/400/404)', () => "
                          "pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                          *save_from_response("j.id", "opId")]),
        retry_on=(403,))


def _cleanup_net():
    return retry_until_authorized(
        Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=["pm.test('cleanup network (200/400/404)', () => "
                          "pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                          *save_from_response("j.id", "opId")]),
        retry_on=(403,))


# ===========================================================================
# NETWORK — declared супернет, verb-pair, op-in-response, defaults, two-projection
# ===========================================================================

# verifies VPC-1-06
CASES.append(Case(
    id="NET-CR-V1-SUPERNET-OK",
    title="Create Network с declared супернетом ipv4/ipv6CidrBlocks → блоки эхаются на read (F2)",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        _net_create_step("sup", v4=[_SUPERNET_V4], v6=[_SUPERNET_V6]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-supernet", method="GET", path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('ipv4CidrBlocks echoes supernet', () => "
                         "pm.expect(j.ipv4CidrBlocks || []).to.include(pm.variables.replaceIn(" + '"' + _SUPERNET_V4 + '"' + ")));",
                         "pm.test('ipv6CidrBlocks echoes supernet', () => "
                         "pm.expect(j.ipv6CidrBlocks || []).to.include(pm.variables.replaceIn(" + '"' + _SUPERNET_V6 + '"' + ")));"])),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-14
CASES.append(Case(
    id="NET-CR-V1-OP-IN-RESPONSE",
    title="Network.Create statusless op-in-response → Operation{done:true} + metadata.networkId + response.Network сразу (F4)",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        _net_create_step("opr", v4=[_SUPERNET_V4], extra_test=[
            "const j = pm.response.json();",
            "pm.test('metadata.networkId present immediately', () => "
            "pm.expect(j.metadata && j.metadata.networkId, JSON.stringify(j)).to.be.a('string').and.not.empty);",
            "pm.test('result is response (not error), unwraps Network', () => {",
            "  pm.expect(j.error, JSON.stringify(j)).to.be.undefined;",
            "  pm.expect(j.response, JSON.stringify(j)).to.be.an('object');",
            "  pm.expect(j.response.id, JSON.stringify(j)).to.eql(j.metadata.networkId);",
            "});",
        ]),
        # follow-up OperationService.Get → тот же done:true (поллить не требуется).
        retry_until_authorized(Step(name="op-get", method="GET", path="/operations/{{opId}}",
            test_script=[*assert_status(200),
                         "pm.test('operation persists done:true', () => "
                         "pm.expect(pm.response.json().done, JSON.stringify(pm.response.json())).to.eql(true));"])),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-11
CASES.append(Case(
    id="NET-CR-V1-DEFAULT-SG",
    title="Network.Create → system-provisioned default-SG, id эхается в defaultSecurityGroupId° (F3)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        _net_create_step("dsg", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-def-sg", method="GET", path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200),
                         "pm.test('defaultSecurityGroupId populated', () => "
                         "pm.expect(pm.response.json().defaultSecurityGroupId, JSON.stringify(pm.response.json()))"
                         ".to.be.a('string').and.not.empty);"])),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-07
CASES.append(Case(
    id="NET-UPD-V1-SUPERNET-IMMUTABLE",
    title="Network.Update mask=ipv4_cidr_blocks → sync InvalidArgument immutable (супернет мутируется только verb-pair, F2)",
    classes=["STATE", "VAL", "NEG"], priority="P1",
    steps=[
        _net_create_step("supi", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        # PATCH своей свежей сети — retry на transient 403; терминальный 400 = реальный assert.
        retry_until_authorized(Step(name="patch-supernet-immutable", method="PATCH", path="/vpc/v1/networks/{{netId}}",
            body={"updateMask": "ipv4_cidr_blocks", "ipv4CidrBlocks": ["10.99.0.0/16"]},
            test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                         "pm.test('immutable-text after Network.Create', () => "
                         "pm.expect(pm.response.json().message).to.match(/is immutable after Network\\.Create$/));"]),
            retry_on=(403,)),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-20
CASES.append(Case(
    id="NET-UPD-V1-PROJECT-IMMUTABLE",
    title="Network.Update mask=project_id → sync InvalidArgument 'project_id is immutable after Network.Create' (Move снят, F5)",
    classes=["STATE", "VAL", "NEG"], priority="P1",
    steps=[
        _net_create_step("pri", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch-project-immutable", method="PATCH", path="/vpc/v1/networks/{{netId}}",
            body={"updateMask": "project_id", "projectId": "{{_suiteProjectCrossId}}"},
            test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                         "pm.test('verbatim immutable text', () => "
                         "pm.expect(pm.response.json().message).to.eql('project_id is immutable after Network.Create'));"]),
            retry_on=(403,)),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-08
CASES.append(Case(
    id="NET-ACB-V1-GROW-OK",
    title="Network :add-cidr-blocks расширяет declared супернет (op-in-response, оба блока видны, F2)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        _net_create_step("acb", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="add-cidr", method="POST", path="/vpc/v1/networks/{{netId}}:add-cidr-blocks",
            body={"ipv4CidrBlocks": ["10.251.0.0/16"]},
            test_script=[*assert_status(200), *_assert_op_in_response(), *save_from_response("j.id", "opId")]),
            retry_on=(403,)),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-grown", method="GET", path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200),
                         "const c = pm.response.json().ipv4CidrBlocks || [];",
                         "pm.test('original supernet retained', () => "
                         "pm.expect(c).to.include(pm.variables.replaceIn(" + '"' + _SUPERNET_V4 + '"' + ")));",
                         "pm.test('added block present', () => pm.expect(c).to.include('10.251.0.0/16'));"])),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-08
CASES.append(Case(
    id="NET-RCB-V1-SHRINK-OK",
    title="Network :remove-cidr-blocks сужает супернет (op-in-response, блок исчезает, F2)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        _net_create_step("rcb", v4=[_SUPERNET_V4, "10.252.0.0/16"]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="remove-cidr", method="POST", path="/vpc/v1/networks/{{netId}}:remove-cidr-blocks",
            body={"ipv4CidrBlocks": ["10.252.0.0/16"]},
            test_script=[*assert_status(200), *_assert_op_in_response(), *save_from_response("j.id", "opId")]),
            retry_on=(403,)),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-shrunk", method="GET", path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200),
                         "const c = pm.response.json().ipv4CidrBlocks || [];",
                         "pm.test('removed block gone', () => pm.expect(c).to.not.include('10.252.0.0/16'));",
                         "pm.test('other supernet block retained', () => "
                         "pm.expect(c).to.include(pm.variables.replaceIn(" + '"' + _SUPERNET_V4 + '"' + ")));"])),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-09
CASES.append(Case(
    id="NET-CR-V1-SUPERNET-MALFORMED",
    title="Network.Create с невалидным CIDR в супернете (/33) → sync InvalidArgument 'invalid CIDR block' (F2)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        # Scope authz — {project, project_id}: грант стабилен → backend-код не маскируется 403.
        Step(name="cr-bad-supernet", method="POST", path="/vpc/v1/networks", pre_script=list(_OCT_PRE),
             body={"projectId": "{{_suiteProjectId}}", "name": "v1supbad-{{runId}}",
                   "ipv4CidrBlocks": ["10.20.0.0/33"]},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('names invalid CIDR block', () => "
                          "pm.expect(pm.response.json().message.toLowerCase()).to.include('invalid cidr block'));"]),
    ],
))

# verifies VPC-1-10
CASES.append(Case(
    id="NET-RCB-V1-COVERS-SUBNET-FP",
    title="Network :remove-cidr-blocks блока, покрывающего живую подсеть → op-error FAILED_PRECONDITION (hardening: скан ВСЕХ подсетей)",
    classes=["NEG", "CONF", "STATE"], priority="P0",
    steps=[
        _net_create_step("rcbcov", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        # Живая подсеть, нарезанная из супернет-блока 10.oct.0.0/16.
        _subnet_create_step("rcbcov", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        # Попытка удалить единственный блок, из которого нарезана подсеть → op-error 9.
        # Проверка (SupernetBlockCoveringSubnet) — single-query по subnet_cidr_blocks под
        # network row-lock: учитывает ВСЕ подсети (не 1-ю страницу List — hardening-фикс).
        retry_until_authorized(Step(name="remove-covering", method="POST",
            path="/vpc/v1/networks/{{netId}}:remove-cidr-blocks",
            body={"ipv4CidrBlocks": [_SUPERNET_V4]},
            test_script=_assert_op_error(9, "FAILED_PRECONDITION", "/still contains subnets$/")),
            retry_on=(403,)),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-21
CASES.append(Case(
    id="NET-CR-V1-DUP-NAME",
    title="Network.Create дубль name в проекте → sync 409 ALREADY_EXISTS 'Network with name .. already exists' (F5)",
    classes=["NEG", "CONC", "CONF"], priority="P1",
    steps=[
        _net_create_step("dup", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        # Второй Create с тем же name (v1dup-{{runId}}) → sync 409 (UNIQUE(project,name) backstop).
        Step(name="cr-dup", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "v1dup-{{runId}}",
                   "ipv4CidrBlocks": [_SUPERNET_V4]},
             test_script=[*assert_status(409), *assert_grpc_code(6, "ALREADY_EXISTS"),
                          "pm.test('verbatim ALREADY_EXISTS text', () => "
                          "pm.expect(pm.response.json().message).to.match(/^Network with name .* already exists$/));"]),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-18
CASES.append(Case(
    id="NET-DEL-V1-NONEMPTY-FP",
    title="Network.Delete непустой сети (есть подсеть) → sync FAILED_PRECONDITION 'Network .. is not empty' (F5, DB-backstop)",
    classes=["NEG", "CONF", "STATE"], priority="P0",
    steps=[
        _net_create_step("delne", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        _subnet_create_step("delne", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        # DELETE своей сети — retry на transient 403; терминальный 400 (FAILED_PRECONDITION) — assert.
        retry_until_authorized(Step(name="del-nonempty", method="DELETE", path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                         "pm.test('verbatim not-empty text', () => "
                         "pm.expect(pm.response.json().message).to.match(/^Network .* is not empty$/));"]),
            retry_on=(403,)),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-16
CASES.append(Case(
    id="NET-GET-V1-NO-VRFID",
    title="public Network НЕ несёт vrfId/routeDistinguisher/underlay (two-projection field-absence, F4)",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        _net_create_step("novrf", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-no-vrf", method="GET", path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('no vrfId on public Network', () => pm.expect(j).to.not.have.property('vrfId'));",
                         "pm.test('no routeDistinguisher on public Network', () => "
                         "pm.expect(j).to.not.have.property('routeDistinguisher'));"])),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))


# ===========================================================================
# SUBNET — placement-anchor derived, CIDR ⊆ супернет + no-overlap, verb-pair, immutables
# ===========================================================================

# verifies VPC-1-23, VPC-1-40
CASES.append(Case(
    id="SUB-CR-V1-ZONAL-OK",
    title="Subnet.Create ZONAL → placementType° derived 'ZONAL' (голый токен), zoneId set, regionId пуст, ipv4CidrPrimary echoed (F6/F9)",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        _net_create_step("zon", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        _subnet_create_step("zon", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-zonal", method="GET", path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('placementType derived ZONAL (bare token)', () => pm.expect(j.placementType).to.eql('ZONAL'));",
                         "pm.test('zoneId echoed', () => pm.expect(j.zoneId).to.eql(pm.environment.get('existingZoneId')));",
                         "pm.test('regionId empty for ZONAL', () => pm.expect(j.regionId || '').to.eql(''));",
                         "pm.test('ipv4CidrPrimary echoed', () => "
                         "pm.expect(j.ipv4CidrPrimary).to.eql(pm.variables.replaceIn('10.{{vpc1oct}}.0.0/24')));"])),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-24
CASES.append(Case(
    id="SUB-CR-V1-REGIONAL-OK",
    title="Subnet.Create REGIONAL → placementType° derived 'REGIONAL', regionId set, zoneId пуст (anycast, F6)",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        _net_create_step("reg", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        # Резолвим живой region id из geo-каталога (geo public-read exempt — default JWT ок).
        Step(name="resolve-region", method="GET", path="/geo/v1/regions",
             test_script=[*assert_status(200),
                          "const rs = (pm.response.json().regions) || [];",
                          "pm.test('geo has >=1 region', () => pm.expect(rs.length).to.be.above(0));",
                          "if (rs.length) pm.environment.set('existingRegionId', rs[0].id);"]),
        _subnet_create_step("reg", {"regionId": "{{existingRegionId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-regional", method="GET", path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('placementType derived REGIONAL', () => pm.expect(j.placementType).to.eql('REGIONAL'));",
                         "pm.test('regionId echoed', () => pm.expect(j.regionId).to.eql(pm.environment.get('existingRegionId')));",
                         "pm.test('zoneId empty for REGIONAL (anycast)', () => pm.expect(j.zoneId || '').to.eql(''));"])),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-25
CASES.append(Case(
    id="SUB-CR-V1-PLACEMENT-BOTH",
    title="Subnet.Create с обоими zoneId+regionId → sync InvalidArgument 'exactly one of zone_id, region_id must be set' (F6)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        _net_create_step("plb", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        Step(name="cr-both-placement", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "name": "v1plb-{{runId}}",
                   "zoneId": "{{existingZoneId}}", "regionId": "ru-central1",
                   "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('verbatim exactly-one text', () => "
                          "pm.expect(pm.response.json().message).to.eql('exactly one of zone_id, region_id must be set'));"]),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-26
CASES.append(Case(
    id="SUB-CR-V1-PLACEMENT-NEITHER",
    title="Subnet.Create без zoneId и regionId → sync InvalidArgument 'exactly one of zone_id, region_id must be set' (F6)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        _net_create_step("pln", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        Step(name="cr-neither-placement", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "name": "v1pln-{{runId}}",
                   "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('verbatim exactly-one text', () => "
                          "pm.expect(pm.response.json().message).to.eql('exactly one of zone_id, region_id must be set'));"]),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-27
CASES.append(Case(
    id="SUB-CR-V1-PLACEMENTTYPE-BODY",
    title="Subnet.Create с placementType в теле → sync explicit-reject 'placement_type is server-derived; set zone_id or region_id instead' (F6, не silent)",
    classes=["VAL", "NEG", "STATE"], priority="P1",
    steps=[
        _net_create_step("ptb", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        # placementType задан клиентом (даже «совпал бы» с derived ZONAL) → explicit reject.
        Step(name="cr-placementtype-body", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "name": "v1ptb-{{runId}}",
                   "placementType": "ZONAL", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('verbatim server-derived reject', () => "
                          "pm.expect(pm.response.json().message).to.eql("
                          "'placement_type is server-derived; set zone_id or region_id instead'));"]),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-28
CASES.append(Case(
    id="SUB-UPD-V1-ZONE-IMMUTABLE",
    title="Subnet.Update mask=zone_id → sync InvalidArgument 'zone_id is immutable after Subnet.Create' (F6, placement-coherence)",
    classes=["STATE", "VAL", "NEG"], priority="P1",
    steps=[
        _net_create_step("zimm", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        _subnet_create_step("zimm", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch-zone-immutable", method="PATCH", path="/vpc/v1/subnets/{{subId}}",
            body={"updateMask": "zone_id", "zoneId": "{{existingZoneAltId}}"},
            test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                         "pm.test('verbatim immutable text', () => "
                         "pm.expect(pm.response.json().message).to.eql('zone_id is immutable after Subnet.Create'));"]),
            retry_on=(403,)),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-38
CASES.append(Case(
    id="SUB-UPD-V1-NETWORK-IMMUTABLE",
    title="Subnet.Update mask=network_id → sync InvalidArgument 'network_id is immutable after Subnet.Create' (F8, VRF-scoping)",
    classes=["STATE", "VAL", "NEG"], priority="P1",
    steps=[
        _net_create_step("nimm", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        _subnet_create_step("nimm", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch-network-immutable", method="PATCH", path="/vpc/v1/subnets/{{subId}}",
            body={"updateMask": "network_id", "networkId": "{{netId}}"},
            test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                         "pm.test('verbatim immutable text', () => "
                         "pm.expect(pm.response.json().message).to.eql('network_id is immutable after Subnet.Create'));"]),
            retry_on=(403,)),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-30
CASES.append(Case(
    id="SUB-CR-V1-CIDR-OUTSIDE-SUPERNET",
    title="Subnet.Create с ipv4CidrPrimary вне супернета сети → sync InvalidArgument 'subnet CIDR .. is not within any network CIDR block' (F7)",
    classes=["VAL", "NEG", "CONF"], priority="P0",
    steps=[
        _net_create_step("out", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        # 192.168.0.0/24 гарантированно вне 10.oct.0.0/16.
        Step(name="cr-cidr-outside", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "name": "v1out-{{runId}}",
                   "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "192.168.0.0/24"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('names within-supernet violation', () => "
                          "pm.expect(pm.response.json().message).to.match("
                          "/^subnet CIDR 192\\.168\\.0\\.0\\/24 is not within any network CIDR block$/));"]),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-31
CASES.append(Case(
    id="SUB-CR-V1-CIDR-OVERLAP",
    title="Subnet.Create с пересекающимся ipv4CidrPrimary в той же сети → sync FAILED_PRECONDITION 'Subnet CIDRs can not overlap' (F7, EXCLUDE)",
    classes=["NEG", "CONF"], priority="P0",
    steps=[
        _net_create_step("ovl", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        _subnet_create_step("ovla", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}, save_id="subId"),
        poll_operation_until_done(),
        # 10.oct.0.128/25 ⊂ 10.oct.0.0/24 → пересечение → FAILED_PRECONDITION (не ALREADY_EXISTS).
        Step(name="cr-overlap", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "name": "v1ovlb-{{runId}}",
                   "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.128/25"},
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          "pm.test('verbatim overlap text (capital S, AS-IS)', () => "
                          "pm.expect(pm.response.json().message).to.eql('Subnet CIDRs can not overlap'));"]),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-32
CASES.append(Case(
    id="SUB-CR-V1-CIDR-PERNET-ISO",
    title="Подсети РАЗНЫХ сетей могут иметь одинаковый ipv4CidrPrimary (per-network EXCLUDE-изоляция, F7)",
    classes=["CONF", "STATE"], priority="P1",
    steps=[
        # net-A + подсеть 10.oct.5.0/24
        _net_create_step("isoa", v4=[_SUPERNET_V4], extra_test=[*save_from_response("j.metadata && j.metadata.networkId", "netA")]),
        poll_operation_until_done(),
        Step(name="mk-sub-a", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netA}}", "name": "v1isoa-{{runId}}",
                   "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.5.0/24"},
             test_script=[*assert_status(200), *_assert_op_in_response(), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subA")]),
        poll_operation_until_done(),
        # net-B (свежая) + подсеть с ТЕМ ЖЕ CIDR 10.oct.5.0/24 → должна пройти (per-network scope).
        Step(name="mk-net-b", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "v1isob-{{runId}}", "ipv4CidrBlocks": [_SUPERNET_V4]},
             test_script=[*assert_status(200), *_assert_op_in_response(), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netB")]),
        poll_operation_until_done(),
        Step(name="mk-sub-b-same-cidr", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netB}}", "name": "v1isob-{{runId}}",
                   "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.5.0/24"},
             test_script=[*assert_status(200), *_assert_op_in_response(),
                          "pm.test('same CIDR in a DIFFERENT network accepted (no op error)', () => "
                          "pm.expect(pm.response.json().error, JSON.stringify(pm.response.json())).to.be.undefined);",
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subB")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="clean-sub-a", method="DELETE", path="/vpc/v1/subnets/{{subA}}",
            test_script=["pm.test('cleanup subA', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                         *save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="clean-sub-b", method="DELETE", path="/vpc/v1/subnets/{{subB}}",
            test_script=["pm.test('cleanup subB', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                         *save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="clean-net-a", method="DELETE", path="/vpc/v1/networks/{{netA}}",
            test_script=["pm.test('cleanup netA', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                         *save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="clean-net-b", method="DELETE", path="/vpc/v1/networks/{{netB}}",
            test_script=["pm.test('cleanup netB', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                         *save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-34
CASES.append(Case(
    id="SUB-ACB-V1-ADD-OK",
    title="Subnet :add-cidr-blocks доп. диапазон ⊆ супернет (op-in-response; ipv4CidrBlocks°, primary неизменён, F7)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        _net_create_step("acb", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        _subnet_create_step("acb", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="add-sub-cidr", method="POST", path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
            body={"ipv4CidrBlocks": ["10.{{vpc1oct}}.8.0/24"]},
            test_script=[*assert_status(200), *_assert_op_in_response(), *save_from_response("j.id", "opId")]),
            retry_on=(403,)),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-added", method="GET", path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('added range present in ipv4CidrBlocks', () => "
                         "pm.expect(j.ipv4CidrBlocks || []).to.include(pm.variables.replaceIn('10.{{vpc1oct}}.8.0/24')));",
                         "pm.test('primary anchor unchanged', () => "
                         "pm.expect(j.ipv4CidrPrimary).to.eql(pm.variables.replaceIn('10.{{vpc1oct}}.0.0/24')));"])),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-34 (And-clause)
CASES.append(Case(
    id="SUB-ACB-V1-OUTSIDE-SUPERNET",
    title="Subnet :add-cidr-blocks блока вне супернета сети → op-error InvalidArgument 'is not within any network CIDR block' (containment-фикс)",
    classes=["NEG", "CONF", "VAL"], priority="P1",
    steps=[
        _net_create_step("acbout", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        _subnet_create_step("acbout", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        # 172.31.0.0/24 вне 10.oct.0.0/16; проверка containment внутри worker → op-error 3.
        retry_until_authorized(Step(name="add-sub-outside", method="POST", path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
            body={"ipv4CidrBlocks": ["172.31.0.0/24"]},
            test_script=_assert_op_error(3, "INVALID_ARGUMENT", "/is not within any network CIDR block$/")),
            retry_on=(403,)),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1 F7 (Subnet primary-anchor immutable via RemoveCidrBlocks — hardening)
CASES.append(Case(
    id="SUB-RCB-V1-PRIMARY-IMMUTABLE",
    title="Subnet :remove-cidr-blocks primary-anchor (blocks[0]/ipv4CidrPrimary) → op-error InvalidArgument immutable (hardening: смена placement-якоря запрещена)",
    classes=["NEG", "STATE", "CONF"], priority="P0",
    steps=[
        _net_create_step("rcbpri", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        _subnet_create_step("rcbpri", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        # Удаление именно primary-anchor → immutable-reject внутри worker → op-error 3.
        retry_until_authorized(Step(name="remove-primary", method="POST",
            path="/vpc/v1/subnets/{{subId}}:remove-cidr-blocks",
            body={"ipv4CidrBlocks": ["10.{{vpc1oct}}.0.0/24"]},
            test_script=_assert_op_error(3, "INVALID_ARGUMENT", "/^ipv4_cidr_primary is immutable after Subnet\\.Create$/")),
            retry_on=(403,)),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-41
CASES.append(Case(
    id="SUB-CR-V1-NETWORK-NOTFOUND",
    title="Subnet.Create с well-formed-но-отсутствующим networkId → sync NOT_FOUND 'Network .. not found' (F9, direct-read lane, ungated)",
    classes=["NEG", "CONF"], priority="P0",
    steps=[
        # networkId=net+17zeros: валиден по prefix (corevalidate пропускает), но не существует.
        # Subnet.Create scope — {project, project_id} (мой грант) → authz-first не маскирует backend 404.
        Step(name="cr-absent-network", method="POST", path="/vpc/v1/subnets", pre_script=list(_OCT_PRE),
             body={"projectId": "{{_suiteProjectId}}", "networkId": "net00000000000000000",
                   "name": "v1subnf-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"},
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('verbatim Network-not-found text', () => "
                          "pm.expect(pm.response.json().message).to.match(/^Network .* not found$/));"]),
    ],
))

# verifies VPC-1-42
CASES.append(Case(
    id="SUB-CR-V1-NETWORK-MALFORMED",
    title="Subnet.Create с malformed networkId → sync InvalidArgument 'invalid network id ..' первым стейтментом (F9)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="cr-malformed-network", method="POST", path="/vpc/v1/subnets", pre_script=list(_OCT_PRE),
             body={"projectId": "{{_suiteProjectId}}", "networkId": "garbage!!",
                   "name": "v1submf-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('verbatim invalid-id text', () => "
                          "pm.expect(pm.response.json().message).to.eql(\"invalid network id 'garbage!!'\"));"]),
    ],
))

# verifies VPC-1-43
CASES.append(Case(
    id="SUB-CR-V1-DHCP-DROPPED",
    title="Subnet DhcpOptions снят by design — dhcpOptions отсутствует в read; попытка задать в теле игнорируется/reject (F9)",
    classes=["CONF", "NEG"], priority="P2",
    steps=[
        _net_create_step("dhcp", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        # dhcpOptions в теле — reserved-поле: gateway silent-ignore (200) ЛИБО unknown-field (400).
        Step(name="cr-with-dhcp", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "name": "v1dhcp-{{runId}}",
                   "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24",
                   "dhcpOptions": {"domainName": "test.local"}},
             test_script=["pm.test('dhcpOptions accepted-ignored or rejected', () => "
                          "pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        # Если подсеть создана — read НЕ несёт dhcpOptions (field-absence, by design).
        retry_until_authorized(Step(name="get-no-dhcp", method="GET", path="/vpc/v1/subnets/{{subId}}",
            test_script=["pm.test('get 200 or 404 (if dhcp rejected create)', () => "
                         "pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
                         "if (pm.response.code === 200) {",
                         "  pm.test('no dhcpOptions on public Subnet', () => "
                         "pm.expect(pm.response.json()).to.not.have.property('dhcpOptions'));",
                         "}"]),
            retry_on=(403,)),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-45
CASES.append(Case(
    id="SUB-LST-V1-FILTER-ZONE",
    title="Subnet.List filter=zone_id=\"..\" → включает подсеть той зоны; unknown filter-поле → InvalidArgument (whitelist, F9)",
    classes=["FILTER", "CRUD", "VAL"], priority="P1",
    steps=[
        _net_create_step("fltz", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        _subnet_create_step("fltz", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        # Positive: zone_id-фильтр (whitelist) → своя свежая подсеть присутствует (retry read-your-writes).
        retry_until_present(Step(name="list-filter-zone", method="GET",
            path="/vpc/v1/subnets?projectId={{_suiteProjectId}}&pageSize=1000&filter=zone_id%3D%22{{existingZoneId}}%22",
            test_script=[*assert_status(200),
                         "const ids = (Object.values(pm.response.json()).find(v => Array.isArray(v)) || []).map(x => x.id);",
                         "pm.test('zone-filtered list contains own subnet', () => "
                         "pm.expect(ids).to.include(pm.environment.get('subId')));"]), "subId"),
        # Negative: unknown filter-поле → InvalidArgument (whitelist name/placement_type/zone_id/network_id).
        Step(name="list-filter-unknown", method="GET",
             path="/vpc/v1/subnets?projectId={{_suiteProjectId}}&filter=nonexistent_field%3D%22x%22",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-45
CASES.append(Case(
    id="SUB-LST-V1-FILTER-NETWORK",
    title="Subnet.List filter=network_id=\"..\" → включает подсеть той сети (whitelist net_id, F9)",
    classes=["FILTER", "CRUD"], priority="P2",
    steps=[
        _net_create_step("fltn", v4=[_SUPERNET_V4]),
        poll_operation_until_done(),
        _subnet_create_step("fltn", {"zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.{{vpc1oct}}.0.0/24"}),
        poll_operation_until_done(),
        retry_until_present(Step(name="list-filter-network", method="GET",
            path="/vpc/v1/subnets?projectId={{_suiteProjectId}}&pageSize=1000&filter=network_id%3D%22{{netId}}%22",
            test_script=[*assert_status(200),
                         "const ids = (Object.values(pm.response.json()).find(v => Array.isArray(v)) || []).map(x => x.id);",
                         "pm.test('network-filtered list contains own subnet', () => "
                         "pm.expect(ids).to.include(pm.environment.get('subId')));"]), "subId"),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

# verifies VPC-1-44
CASES.append(Case(
    id="SUB-LST-V1-PAGE-VALIDATE",
    title="Subnet.List pageSize>1000 и garbage pageToken → InvalidArgument (авторизованный caller, format-validate, F9)",
    classes=["PAGE", "VAL", "BVA"], priority="P1",
    steps=[
        # Caller имеет грант editor на _suiteProjectId (non-empty grant) → repo-путь валидирует
        # page_size/page_token → 400 (не clamp). NB: под empty-grant subject валидация уходит ПОСЛЕ
        # short-circuit (см. RESULTS.md-заметку) — здесь caller авторизован, поведение детерминировано.
        Step(name="list-pagesize-over-max", method="GET",
             path="/vpc/v1/subnets?projectId={{_suiteProjectId}}&pageSize=10000",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        Step(name="list-token-garbage", method="GET",
             path="/vpc/v1/subnets?projectId={{_suiteProjectId}}&pageSize=10&pageToken=not-a-real-token",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

# verifies VPC-1-46
CASES.append(Case(
    id="SUB-CR-V1-V6-ONLY",
    title="Subnet.Create v6-only (ipv6CidrPrimary, без ipv4CidrPrimary) → 200; ipv6CidrPrimary echoed, ipv4CidrPrimary пуст (F9 edge)",
    classes=["CRUD", "CONF", "BVA"], priority="P2",
    steps=[
        _net_create_step("v6", v6=[_SUPERNET_V6]),
        poll_operation_until_done(),
        _subnet_create_step("v6", {"zoneId": "{{existingZoneId}}", "ipv6CidrPrimary": "fd00:{{vpc1oct}}::/64"}),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-v6only", method="GET", path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('placementType ZONAL', () => pm.expect(j.placementType).to.eql('ZONAL'));",
                         "pm.test('ipv6CidrPrimary echoed', () => "
                         "pm.expect(j.ipv6CidrPrimary).to.eql(pm.variables.replaceIn('fd00:{{vpc1oct}}::/64')));",
                         "pm.test('ipv4CidrPrimary empty (v6-only)', () => pm.expect(j.ipv4CidrPrimary || '').to.eql(''));"])),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))
