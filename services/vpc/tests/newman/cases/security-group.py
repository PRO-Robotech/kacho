# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для SecurityGroupService."""

CASES = []


def _assert_rule_target_unusable(field: str, target_var: str):
    """Цель правила непригодна — ОДИН текст на оба исхода резолва.

    Прежняя редакция двух кейсов ждала `'security group rule can only reference a
    security group in the same network'`. Этот текст СНЯТ ОСОЗНАННО: два разных
    текста («такой группы нет» против «группа в чужой сети») были
    существование-оракулом — по тону отказа вызывающий устанавливал, существует
    ли группа, которой он не владеет. Теперь обе ветви отвечают формой
    НАСТОЯЩЕГО промаха группы, которую производит слой хранения и которой же
    край скрывает отказ в доступе:
    `Security group SecurityGroup.Id(value=<id>) not found`.

    Утверждать «текст равен ожидаемому» здесь мало: ровно ту же строку вернул бы
    отказ на любой чужой идентификатор. Поэтому утверждений три, и вместе они
    описывают именно снятое свойство:

      1. текст называет ТОТ id, который прислал вызывающий (его собственный
         ввод, не чужой объект) — иначе отказ не адресный;
      2. отказ НЕ сообщает, что дело в сети: отсутствие «same network»/«network»
         в тексте — это и есть свойство неразличимости, ради которого тексты
         сводились. Без этого утверждения возврат прежней формулировки прошёл бы
         молча;
      3. поле отказа названо вызывающему (`assert_field_violation` у места
         вызова) — машинный дискриминатор, который об чужом объекте не сообщает.

    Парный положительный контроль — отдельный кейс «цель в ТОЙ ЖЕ сети → 200»
    (SG-NET-08): без него «отвергнуто» было бы неотличимо от «отвергается всё».
    """
    return [
        "pm.test('отказ называет присланный id, формой настоящего промаха', () => "
        "pm.expect(pm.response.json().message, pm.response.text()).to.eql("
        "'Security group SecurityGroup.Id(value=' + pm.environment.get('" + target_var + "') "
        "+ ') not found'));",
        "pm.test('отказ НЕ выдаёт, что цель существует в чужой сети', () => "
        "pm.expect(pm.response.json().message.toLowerCase()).to.not.include('network'));",
    ]


def _net_steps(suffix="sg"):
    return [
        Step(name="pre-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": f"sg-{suffix}-net-{{{{runId}}}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
    ]


def _cleanup_net():
    # DELETE of the caller's OWN fresh network. Its v_delete/owner tuple materializes
    # eventually-consistent (registrar + fga_outbox drain + reconciler), so under PARALLEL
    # load the cleanup DELETE races the drain and 403s single-shot (the wrapped Create can
    # pass permissively before the tuple lands). Bounded read-your-writes retry SELF on 403
    # until authorized; the strict 200 assertion is preserved (SG already deleted → the net
    # deletes cleanly). Fail-closed at the budget (a genuine deny still fails).
    return retry_until_authorized(
        Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        retry_on=(403,))


def _cleanup_net_lenient():
    # См. route-table.py::_cleanup_net_lenient — wrap'нутый Create мог пройти permissive'но
    # (ресурс создан) → DELETE сети блокируется FK RESTRICT (400). Оба исхода ОК.
    # retry_on=(403,): DELETE своей свежей сети может краснеть 403, пока owner-tuple
    # материализуется (eventual-consistency после opgate) — ретраим ТОЛЬКО этот транзиент.
    return retry_until_authorized(
        Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=["pm.test('cleanup net (200 or 400 if child leaked)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")]),
        retry_on=(403,))


CASES.append(Case(
    id="SG-CR-VAL-RULE-NO-TARGET",
    title="Create SG с правилом БЕЗ цели → 400, отказ называет rule_specs[0].target",
    classes=["NEG", "VAL"],
    priority="P1",
    steps=[
        *_net_steps("notgt"),
        Step(
            name="create-rule-without-target",
            method="POST",
            path="/vpc/v1/securityGroups",
            # Контракт обещает «ровно одна цель» аннотацией на `oneof target`, но
            # энфорсера у обещания не было: правило без цели ПРИНИМАЛОСЬ,
            # сохранялось и возвращалось при чтении. Оно описывает «разрешить
            # трафик... куда?» — и по закрытой модели не разрешает ничего, то есть
            # вызывающий получал успех на правиле, которое не делает написанного.
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sg-notgt-{{runId}}",
                  # Порты у спецификации правила лежат ВЛОЖЕННЫМ объектом `ports`, а
                  # не полями верхнего уровня: первая редакция этого кейса послала их
                  # верхним уровнем, и гейт неизвестных полей коллекций её поймал.
                  # Здесь порты вообще не заданы — «любой порт», и предмет кейса это не
                  # затрагивает: он про ЦЕЛЬ.
                  "ruleSpecs": [{"direction": "INGRESS", "protocolName": "ANY"}]},
            test_script=[
                *assert_status(400),
                # Отказ обязан НАЗЫВАТЬ поле: «пришло 400» истинно и при отказе по
                # направлению, порту или протоколу, поэтому предмета оно не утверждает.
                *assert_field_violation("rule_specs[0].target"),
            ],
        ),
        # Положительный контроль: то же правило С целью проходит. Без него отказ
        # зеленел бы на реализации, отвергающей ЛЮБОЕ правило.
        Step(
            name="create-rule-with-target",
            method="POST",
            path="/vpc/v1/securityGroups",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sg-tgt-{{runId}}",
                  "ruleSpecs": [{"direction": "INGRESS", "protocolName": "ANY",
                                 "cidrBlocks": {"v4CidrBlocks": ["10.0.0.0/8"]}}]},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")],
        ),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE",
             path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SG-CR-CRUD-OK",
    title="Create SG + Get",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_net_steps("cr"),
        Step(name="create", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-cr-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200),
                          "pm.test('id matches', () => pm.expect(pm.response.json().id).to.eql(pm.environment.get('sgId')));",
                          # APPLY-11: чтение ресурса со строкой намерения несёт
                          # состояние применения.
                          *assert_apply_state_in_flight("SG")])),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    # network_id обязателен на Create: Create без networkId → sync 400
    # INVALID_ARGUMENT "network_id required", Operation НЕ создается.
    # verifies SG-NET-01
    id="SG-NET-01-NEG-CREATE-NO-NETWORK",
    title="Create SG без networkId → sync 400 INVALID_ARGUMENT 'network_id required'",
    classes=["VAL", "NEG"],
    priority="P0",
    steps=[
        Step(name="create-no-net", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "name": "sg-nonet-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('verbatim text network_id required', () => pm.expect(pm.response.json().message).to.eql('network_id required'));"]),
    ],
))

CASES.append(Case(
    id="SG-CR-WITH-NETWORK-OK",
    title="Create SG c networkId → success → get → networkId echoed",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_net_steps("withnet"),
        Step(name="create-with-net", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-withnet-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-with-net", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200),
                          "pm.test('networkId echoed', () => pm.expect(pm.response.json().networkId).to.eql(pm.environment.get('netId')));"])),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    # filter=network_id="<id>" — SG в net-A матчится, SG в другой сети net-B — нет.
    # network_id обязателен, «unbound SG» не существует, поэтому negative-сторона
    # фильтра проверяется SG из ДРУГОЙ сети (а не network-less SG).
    id="SG-LIST-FILTER-NETWORK-OK",
    title="List?filter=network_id=\"<netA>\" — SG net-A present, SG net-B absent",
    classes=["CRUD", "FILTER"],
    priority="P2",
    steps=[
        *_net_steps("fltneta"),  # net-A → {{netId}}
        Step(name="pre-net-b", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "sg-fltnetb-net-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netBId")]),
        poll_operation_until_done(),
        Step(name="create-in-a", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-flta-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgAId")]),
        poll_operation_until_done(),
        Step(name="create-in-b", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netBId}}",
                   "name": "sg-fltb-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgBId")]),
        poll_operation_until_done(),
        retry_until_present(Step(name="list-by-network", method="GET",
             path="/vpc/v1/securityGroups?projectId={{_suiteProjectId}}&pageSize=1000&filter=network_id%3D%22{{netId}}%22",
             test_script=[*assert_status(200),
                          "const ids = (pm.response.json().securityGroups || []).map(s => s.id);",
                          "pm.test('SG in net-A present', () => pm.expect(ids).to.include(pm.environment.get('sgAId')));",
                          "pm.test('SG in net-B absent', () => pm.expect(ids).to.not.include(pm.environment.get('sgBId')));"]),
             "sgAId"),
        Step(name="cleanup-sg-a", method="DELETE", path="/vpc/v1/securityGroups/{{sgAId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup-sg-b", method="DELETE", path="/vpc/v1/securityGroups/{{sgBId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup-net-b", method="DELETE", path="/vpc/v1/networks/{{netBId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SG-GET-NEG-NF",
    title="Get malformed id → 400 InvalidArgument 'invalid security group id'",
    classes=["NEG"],
    priority="P0",
    steps=[
        Step(name="get-garbage", method="GET", path="/vpc/v1/securityGroups/{{garbageId}}",
             test_script=[
                 # malformed id (нет известного 3-char префикса) → 400 InvalidArgument
                 # "invalid security group id '<X>'". Проверка family-agnostic.
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('mentions invalid id', () => { const m = pm.response.json().message; pm.expect(m).to.include('invalid'); pm.expect(m).to.include('id'); });",
             ]),
    ],
))

CASES.append(Case(
    id="SG-LST-CRUD-OK",
    title="List SG в project → 200",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(name="list", method="GET", path="/vpc/v1/securityGroups?projectId={{_suiteProjectId}}",
             test_script=[*assert_status(200),
                          "pm.test('securityGroups array', () => pm.expect(pm.response.json().securityGroups || []).to.be.an('array'));"]),
    ],
))

CASES.append(Case(
    id="SG-LST-VAL-PROJECT-REQUIRED",
    title="List без project → InvalidArgument",
    classes=["VAL", "AUTHZ"],
    priority="P0",
    steps=[
        Step(name="list-noproject", method="GET", path="/vpc/v1/securityGroups",
             test_script=[*assert_unscoped_rejected()]),
    ],
))

CASES.append(Case(
    id="SG-UPD-AUTHZ-NF-SYNC",
    title="Update несуществующего → sync 404",
    classes=["NEG", "AUTHZ"],
    priority="P1",
    steps=[
        Step(name="patch-nx", method="PATCH", path="/vpc/v1/securityGroups/{{garbageVpcId}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="SG-DEL-AUTHZ-NF-SYNC",
    title="Delete несуществующего → sync 404",
    classes=["NEG", "AUTHZ"],
    priority="P1",
    steps=[
        Step(name="del-nx", method="DELETE", path="/vpc/v1/securityGroups/{{garbageVpcId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="SG-URL-CRUD-OK",
    title="UpdateRules: добавить правило",
    classes=["CRUD", "STATE"],
    priority="P1",
    steps=[
        *_net_steps("url"),
        Step(name="create-sg", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-url-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="update-rules", method="PATCH", path="/vpc/v1/securityGroups/{{sgId}}/rules",
             body={
                 "additionRuleSpecs": [
                     {"description": "ingress-tcp-22",
                      "direction": "INGRESS",
                      "ports": {"fromPort": 22, "toPort": 22},
                      "protocolName": "tcp",
                      "cidrBlocks": {"v4CidrBlocks": ["0.0.0.0/0"]}}
                 ]
             },
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-sg", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200),
                          "pm.test('has 1 rule', () => pm.expect((pm.response.json().rules || []).length).to.be.at.least(1));"])),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SG-LOP-CRUD-OK",
    title="ListOperations SG",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_net_steps("lop"),
        Step(name="create-sg", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-lop-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        Step(name="list-ops", method="GET", path="/vpc/v1/securityGroups/{{sgId}}/operations",
             test_script=[*assert_status(200),
                          "pm.test('at least 1 op', () => pm.expect((pm.response.json().operations || []).length).to.be.at.least(1));"]),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

# Расширение
CASES.extend(crud_list_bva_block("SG", "/vpc/v1/securityGroups"))
CASES.append(conf_not_found_text("SG", "/vpc/v1/securityGroups", "Security group"))
CASES.append(state_update_unknown_mask("SG", "/vpc/v1/securityGroups"))

CASES.append(Case(
    id="SG-UR-NEG-RULE-NF",
    title="UpdateRule малформированного rule_id → sync 400 'Invalid rule id <id>'",
    classes=["NEG"], priority="P1",
    steps=[
        *_net_steps("ur"),
        Step(name="create-sg", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-ur-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        # Малформированный rule_id → синхронный 400 InvalidArgument
        # "Invalid rule id <ruleId>" (не Operation).
        retry_until_authorized(Step(name="ur-bad-rule-id", method="PATCH",
             path="/vpc/v1/securityGroups/{{sgId}}/rules/nonexistent-rule-id",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('verbatim text', () => pm.expect(pm.response.json().message).to.eql('Invalid rule id nonexistent-rule-id'));"])),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SG-UPD-CRUD-OK",
    title="Update SG description",
    classes=["CRUD"], priority="P1",
    steps=[
        *_net_steps("upd"),
        Step(name="create-sg", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-upd-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch", method="PATCH", path="/vpc/v1/securityGroups/{{sgId}}",
             body={"updateMask": "description", "description": "upd-newman"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

# Дополнение: STATE immutable project + VAL move-no-dest + BVA pagesize=1
CASES.append(state_immutable_project("SG", "/vpc/v1/securityGroups"))
CASES.append(list_pagesize_1_bva("SG", "/vpc/v1/securityGroups"))

CASES.append(Case(
    id="SG-CR-CONF-NET-NF-TEXT",
    title="Create SG в garbage network → точный текст 'Network ... not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{garbageVpcId}}",
                   "name": "sg-confnf-{{runId}}", "ruleSpecs": []},
             test_script=[
                 *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('verbatim Network ... not found', () => pm.expect(pm.response.json().message).to.match(/^Network .* not found$/));",
             ]),
    ],
))

CASES.append(Case(
    id="SG-UPD-CONF-NF-TEXT",
    title="Update несуществующего → точный текст 'Security group ... not found' text",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="patch-nx", method="PATCH",
             path="/vpc/v1/securityGroups/{{garbageVpcId}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="SG-DEL-CONF-NF-TEXT",
    title="Delete несуществующего → точный текст 'Security group ... not found' text",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="del-nx", method="DELETE",
             path="/vpc/v1/securityGroups/{{garbageVpcId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="SG-DEL-CRUD-OK",
    title="SG Delete happy",
    classes=["CRUD"], priority="P1",
    steps=[
        *_net_steps("delok"),
        Step(name="create-sg", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-delok-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-happy", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SG-UR-CRUD-OK",
    title="UpdateRule (single) — добавить rule, обновить description",
    classes=["CRUD"], priority="P1",
    steps=[
        *_net_steps("urok"),
        Step(name="create-sg", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-urok-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="add-rule", method="PATCH", path="/vpc/v1/securityGroups/{{sgId}}/rules",
             body={"additionRuleSpecs": [
                 {"description": "init", "direction": "INGRESS",
                  "ports": {"fromPort": 80, "toPort": 80}, "protocolName": "tcp",
                  "cidrBlocks": {"v4CidrBlocks": ["0.0.0.0/0"]}}
             ]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # Read-your-writes: и add-rule (PATCH), и этот GET гейтятся одним и тем же
        # per-object authz-Check в interceptor'е, но owner-tuple свежего SG
        # материализуется eventually-consistent → первый доступ может поймать 403/404.
        # Оборачиваем ретраем как первый read-доступ к свежему SG.
        retry_until_authorized(Step(name="get-sg-rule-id", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200),
                          *save_from_response("(j.rules && j.rules[0] && j.rules[0].id) || ''", "ruleId")])),
        Step(name="ur", method="PATCH", path="/vpc/v1/securityGroups/{{sgId}}/rules/{{ruleId}}",
             body={"updateMask": "description", "description": "updated"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SG-URL-AUTHZ-NF-SYNC",
    title="UpdateRules несуществующего SG → sync 404 от AuthZ-Get",
    classes=["NEG", "AUTHZ", "VAL"], priority="P1",
    steps=[
        Step(name="url-nx", method="PATCH", path="/vpc/v1/securityGroups/{{garbageVpcId}}/rules",
             body={"additionRuleSpecs": []},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="SG-UR-AUTHZ-NF-SYNC",
    title="UpdateRule несуществующего SG → sync 404 от AuthZ-Get",
    classes=["NEG", "AUTHZ", "VAL"], priority="P1",
    steps=[
        Step(name="ur-nx", method="PATCH",
             path="/vpc/v1/securityGroups/{{garbageVpcId}}/rules/any-rule-id",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="SG-LOP-NEG-PARENT-NF",
    title="ListOperations несуществующего SG → 200 или 404",
    classes=["NEG"], priority="P2",
    steps=[
        Step(name="lop-nx", method="GET",
             path="/vpc/v1/securityGroups/{{garbageVpcId}}/operations",
             test_script=["pm.test('200/403/404', () => pm.expect(pm.response.code).to.be.oneOf([200, 403, 404]));"]),
    ],
))

def _sg_wrap(prefix, suffix, inner_case):
    uniq = inner_case.id.lower().replace("-","")[-12:]
    return Case(
        id=inner_case.id, title=inner_case.title, classes=inner_case.classes,
        priority=inner_case.priority,
        steps=[*_net_steps(uniq), *inner_case.steps, _cleanup_net_lenient()],
    )

_sg_body = {"networkId": "{{netId}}", "ruleSpecs": []}
for c in ecp_name_block("SG", "/vpc/v1/securityGroups", _sg_body):
    CASES.append(_sg_wrap("SG", "ecpn", c))
for c in ecp_description_block("SG", "/vpc/v1/securityGroups", _sg_body):
    CASES.append(_sg_wrap("SG", "ecpd", c))
for c in ecp_labels_block("SG", "/vpc/v1/securityGroups", _sg_body):
    CASES.append(_sg_wrap("SG", "ecpl", c))
CASES.extend(updatemask_decision_table("SG", "/vpc/v1/securityGroups"))
CASES.extend(filter_syntax_block("SG", "/vpc/v1/securityGroups"))
CASES.append(pagination_roundtrip("SG", "/vpc/v1/securityGroups"))

for c in update_happy_per_field("SG", "/vpc/v1/securityGroups", "/vpc/v1/securityGroups",
    {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "ruleSpecs": []}):
    CASES.append(_sg_wrap("SG", "v7", c))

CASES.extend(perf_baseline_block("SG", "/vpc/v1/securityGroups"))
CASES.extend(verbatim_text_pack("SG", "SecurityGroup", "/vpc/v1/securityGroups", text_template="Security group SecurityGroup.Id(value={id}) not found"))
CASES.extend(authz_caller_headers_block("SG", "/vpc/v1/securityGroups"))

# Перемещения ресурса между проектами в контракте vpc НЕТ — RPC снят целиком, и
# ни один кейс набора его не зовёт (проверено поиском по cases/: ноль вхождений).
# Прежняя редакция этого комментария объясняла, почему одна проверка перемещения
# неприменима к SG, и добавляла, что её образец «остаётся валиден для прочих
# ресурсов». Второе пережило свой предмет: прочих тоже нет.

CASES.append(_sg_wrap("SG", "v8m",
    update_happy_multi_field("SG", "/vpc/v1/securityGroups", "/vpc/v1/securityGroups",
        {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "ruleSpecs": []})))
CASES.append(_sg_wrap("SG", "v8f",
    list_filter_match_block("SG", "/vpc/v1/securityGroups",
        {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "ruleSpecs": []})))
for c in neg_invalid_types_block("SG", "/vpc/v1/securityGroups",
    {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "ruleSpecs": []}):
    CASES.append(_sg_wrap("SG", "v8nt", c))
CASES.extend(http_method_not_allowed_block("SG", "/vpc/v1/securityGroups"))
CASES.extend(malformed_body_block("SG", "/vpc/v1/securityGroups"))

CASES.append(_sg_wrap("SG", "v9d",
    alreadyexists_dup_name_for("SG", "/vpc/v1/securityGroups",
        {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "ruleSpecs": []})))
for c in update_mask_partial_block("SG", "/vpc/v1/securityGroups", "/vpc/v1/securityGroups",
    {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "ruleSpecs": []}):
    CASES.append(_sg_wrap("SG", "v9p", c))
CASES.append(_sg_wrap("SG", "v9pf",
    perf_baseline_get_block("SG", "/vpc/v1/securityGroups",
        {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "ruleSpecs": []})))
CASES.extend(list_total_size_check_block("SG", "/vpc/v1/securityGroups"))

# v10: SG-specific rule validation
#
# Заголовок каждого кейса обещает ОДИН исход, и утверждение проверяет ровно его.
# Прежде на всю пятёрку стояло `oneOf([200, 400])` под меткой «rejected sync or
# async»: и приём правила, и отказ в нём проходили одинаково, поэтому ни один из
# четырёх отрицательных кейсов не мог упасть — независимо от того, что делает
# продукт.
#
# Что делает продукт (services/vpc/internal/apps/kacho/api/securitygroup):
#   * `validateSGRule` вызывается СИНХРОННО, до создания операции, и проверяет
#     направление, диапазон портов, протокол, описание, метки и CIDR-блоки →
#     отказ виден кодом 400 и называет поле;
#   * неизвестное значение перечисления направления не доходит даже до сервиса:
#     его отвергает разбор тела на краю — тоже 400;
#   * граница порта вне `0-65535` (кроме `-1` = «любой», который обязан стоять на
#     обеих границах сразу), перевёрнутый диапазон, имя протокола вне реестра
#     IANA и номер протокола вне `0-255` отвергаются здесь же.
#
# До 2026-07-31 последняя строка была неверна: порты и протокол не проверялись
# нигде, и три кейса ниже стояли красными как заявленный дефект продукта
# (kacho#103). Замер в боевой посадке подтвердил их живьём, дефект закрыт,
# декларация в docs/RESULTS.md снята вместе с предметом.
# Отрицательный кейс, у которого отказ ЕСТЬ, но поле не названо, оставляет
# вызывающего с «что-то не так» — поэтому там, где поле известно, кейс требует
# его дословно (`names_field`). У направления поля нет: тело отвергает разбор на
# краю, до сервиса запрос не доходит, и придумывать ему имя было бы враньём.
for case_id, rule, expect_ok, names_field in [
    ("SG-URL-VAL-PORT-NEG", {"fromPort": -2, "toPort": 22}, False, "from_port"),
    ("SG-URL-VAL-PORT-OVER-65535", {"fromPort": 65536, "toPort": 65536}, False, "from_port"),
    ("SG-URL-VAL-PORT-ANY-MINUS-1", {"fromPort": -1, "toPort": -1}, True, None),
    ("SG-URL-VAL-DIRECTION-UNKNOWN", {"fromPort": 80, "toPort": 80, "direction": "DIAGONAL"}, False, None),
    ("SG-URL-VAL-PROTOCOL-UNKNOWN", {"fromPort": 80, "toPort": 80, "protocolName": "klingon"}, False, "protocol_name"),
    # `protocol` — oneof из двух ветвей, и выбранная ветка с нулевым значением
    # своего домена не то же самое, что невыбранная. Номер 0 занят в реестре
    # IANA (HOPOPT), но «протокол не задан» лежит в хранилище тем же нулём,
    # поэтому принять его значило бы сохранить правило как «любой протокол» —
    # ШИРЕ, чем просил вызывающий. Отказ синхронный и называет поле; HOPOPT
    # остаётся выразимым по имени.
    ("SG-URL-VAL-PROTOCOL-NUMBER-ZERO", {"fromPort": 80, "toPort": 80, "protocolNumber": 0}, False, "protocol_number"),
    ("SG-URL-VAL-PROTOCOL-NAME-EMPTY", {"fromPort": 80, "toPort": 80, "protocolName": ""}, False, "protocol_name"),
    # Парная положительная половина: то же имя, выбранное непустым, проходит —
    # иначе отрицательные кейсы выше зеленели бы и при полностью сломанной
    # ветке протокола.
    ("SG-URL-VAL-PROTOCOL-NAME-HOPOPT", {"fromPort": 80, "toPort": 80, "protocolName": "hopopt"}, True, None),
    ("SG-URL-VAL-PROTOCOL-NUMBER-OK", {"fromPort": 80, "toPort": 80, "protocolNumber": 17}, True, None),
]:
    rule_full = {"description": "test", "direction": rule.pop("direction", "INGRESS"),
                 "ports": {"fromPort": rule["fromPort"], "toPort": rule["toPort"]},
                 "cidrBlocks": {"v4CidrBlocks": ["0.0.0.0/0"]}}
    # Ветка oneof выбирается ровно одна: положить рядом и имя, и номер значило
    # бы отправить запрос, у которого выигрывает последний записанный ключ, —
    # то есть кейс проверял бы не то, что называет.
    if "protocolNumber" in rule:
        rule_full["protocolNumber"] = rule.pop("protocolNumber")
    else:
        rule_full["protocolName"] = rule.pop("protocolName", "tcp")
    inner = Case(
        id=case_id, title=f"UpdateRules rule field: {case_id}",
        classes=["VAL", "STATE"] + (["NEG"] if not expect_ok else []),
        priority="P1",
        steps=[
            Step(name="create-sg", method="POST", path="/vpc/v1/securityGroups",
                 body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                       "name": f"sg-r-{case_id.lower()[-6:]}-{{{{runId}}}}", "ruleSpecs": []},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                              *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
            poll_operation_until_done(),
            # Mutation on the caller's OWN fresh SG rules — editor tuple can still be draining
            # under parallel load → gateway authz-first 403 BEFORE backend rule validation.
            # Retry SELF on 403 until authorized, THEN the PATCH reaches the backend and the
            # real negative assertion (200 op-started | 400 sync-reject) runs — NOT masked.
            retry_until_authorized(
                Step(name="update-rule-bad", method="PATCH", path="/vpc/v1/securityGroups/{{sgId}}/rules",
                     body={"additionRuleSpecs": [rule_full]},
                     test_script=(
                         [*assert_status(200), *save_from_response("j.id", "opId")]
                         if expect_ok else
                         [*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]
                         # Читается ИМЕННО поле нарушения, а не тело целиком:
                         # подстрока по всему ответу нашла бы `from_port` и в
                         # тексте отказа про to_port, то есть проходила бы на
                         # противоположной границе.
                         + ([f"pm.test('violation field ends with {names_field}', () => {{",
                             "  const d = (pm.response.json().details || []).find(x => (x.fieldViolations || []).length);",
                             "  const fields = ((d || {}).fieldViolations || []).map(v => v.field);",
                             f"  pm.expect(fields, JSON.stringify(pm.response.json())).to.satisfy(fs => fs.some(f => String(f).endsWith('{names_field}')));",
                             "});"]
                            if names_field else [])
                     )),
                retry_on=(403,)),
        ] + ([poll_operation_until_done()] if expect_ok else []) + [
            retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
                 test_script=[*save_from_response("j.id", "opId")])),
            poll_operation_until_done(),
        ],
    )
    CASES.append(_sg_wrap("SG", "v10r" + case_id[-5:].lower(), inner))

# v11 edge cases
CASES.append(Case(
    id="SG-LST-PAGE-NEGATIVE-SIZE",
    # Размер страницы вне [0..1000] ОТВЕРГАЕТСЯ, а не подменяется умолчанием
    # (конвенция pagination). Исход ровно один, поэтому и утверждение одно:
    # прежнее `oneOf([200, 400])` под заголовком «rejected or default» проходило
    # и при отказе, и при его отсутствии — то есть не отделяло соблюдение
    # контракта от нарушения. Проверка детерминирована: у прогона есть субъект,
    # поэтому ранний выход по пустому субъекту не срабатывает и валидация
    # страницы выполняется всегда.
    title="List с pageSize=-1 → 400 InvalidArgument (отвергается, не clamp\'ится)",
    classes=["BVA", "VAL"], priority="P2",
    steps=[Step(name="lst-neg", method="GET",
                path="/vpc/v1/securityGroups?projectId={{_suiteProjectId}}&pageSize=-1",
                test_script=[
                    *assert_status(400),
                    *assert_grpc_code(3, "INVALID_ARGUMENT"),
                    "pm.test('names the offending field', () => pm.expect(JSON.stringify(pm.response.json())).to.contain('page_size'));",
                ])],
))

CASES.append(Case(
    id="SG-LST-FILTER-SPECIAL-CHARS",
    title="List с filter содержащим спец-символы → 400 или 200",
    classes=["FILTER", "VAL"], priority="P3",
    steps=[Step(name="lst-fsc", method="GET",
                path="/vpc/v1/securityGroups?projectId={{_suiteProjectId}}&filter=name%3D%22%21%40%23%24%25%22",
                test_script=["pm.test('handled', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])],
))

CASES.append(Case(
    id="SG-LST-PAGESIZE-EXACTLY-1000",
    title="List с pageSize=1000 (boundary max) → 200",
    classes=["BVA"], priority="P2",
    steps=[Step(name="lst-max", method="GET",
                path="/vpc/v1/securityGroups?projectId={{_suiteProjectId}}&pageSize=1000",
                test_script=[*assert_status(200)])],
))

CASES.append(Case(
    id="SG-LST-PAGESIZE-1001",
    title="List с pageSize=1001 (over max) → 400",
    classes=["BVA", "VAL"], priority="P1",
    steps=[Step(name="lst-1001", method="GET",
                path="/vpc/v1/securityGroups?projectId={{_suiteProjectId}}&pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="SG-LST-DOUBLE-PROJECT-PARAM",
    title="List с дубликатом projectId param → 200 (last wins) или 400",
    classes=["VAL"], priority="P3",
    steps=[Step(name="lst-dup", method="GET",
                path="/vpc/v1/securityGroups?projectId={{_suiteProjectId}}&projectId={{_suiteProjectCrossId}}&pageSize=10",
                test_script=["pm.test('200 or 400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])],
))

CASES.append(Case(
    id="SG-GET-TRAILING-SLASH",
    title="Get с trailing slash → 404",
    classes=["VAL"], priority="P3",
    steps=[Step(name="get-trail", method="GET", path="/vpc/v1/securityGroups/{{garbageVpcId}}/",
                test_script=["pm.test('non-2xx', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));"])],
))

CASES.append(Case(
    id="SG-DEL-STATE-DEFAULT-SG",
    title="Delete default-SG напрямую → должен fail (нельзя delete default SG в обход)",
    classes=["NEG", "STATE"], priority="P1",
    steps=[
        Step(name="cr-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-defsg-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        # Идентификатор группы по умолчанию называет САМА СЕТЬ (`defaultSecurityGroupId`).
        #
        # Прежняя редакция спрашивала под-перечисление сети
        # (`/vpc/v1/networks/{id}/security_groups`). Этот метод СНЯТ С КОНТРАКТА —
        # второй путь к одному ответу с другим объектом проверки прав, — и края
        # такого маршрута не знает: каталог прав не резолвит метод, край
        # отказывает fail-closed `403` с пустым `action` и `type: authz.catalog`.
        # `retry_on=(403,404)` крутил этот отказ весь бюджет, потому что читал его
        # как окно материализации owner-tuple: снятый адрес и не материализуется
        # никогда. Дальше шаг падал дважды (403 и «must have default SG»), а
        # следующий шаг — предусловием: `defaultSgId` не захвачена, запрос не
        # отправлен, и его исполнение вообще не попадало в отчёт
        # (ASSERTED-NOT-EXECUTED). Одна причина, четыре наблюдаемых следствия.
        #
        # Замена не «другой список», а более узкое утверждение: сеть отдаёт id
        # своей группы по умолчанию сама, поэтому и ждать нечего, и спутать её с
        # чужой группой того же проекта нельзя.
        retry_until_authorized(
            Step(name="get-default-sg-id", method="GET",
                 path="/vpc/v1/networks/{{netId}}",
                 test_script=[*assert_status(200),
                              "const j = pm.response.json();",
                              "pm.test('сеть называет свою группу по умолчанию', () => "
                              "pm.expect(j.defaultSecurityGroupId, pm.response.text())"
                              ".to.be.a('string').and.not.empty);",
                              "pm.environment.set('defaultSgId', j.defaultSecurityGroupId || '');"]),
            retry_on=(403, 404)),
        # Она действительно группа по умолчанию — иначе следующий шаг запрещал бы
        # удаление обычной группы и был бы зелёным не по своему предмету.
        retry_until_authorized(
            Step(name="default-sg-is-flagged", method="GET",
                 path="/vpc/v1/securityGroups/{{defaultSgId}}",
                 test_script=[*assert_status(200),
                              "pm.test('помечена как группа по умолчанию', () => "
                              "pm.expect(pm.response.json().defaultForNetwork).to.eql(true));"]),
            retry_on=(403, 404)),
        # ПРЕДМЕТ КЕЙСА — ОТКАЗ, и он утверждается как отказ.
        #
        # Прежняя редакция принимала `oneOf([200, 400, 409])` и на этом
        # заканчивала: единственное утверждение кейса проходило и когда группа
        # по умолчанию УДАЛЯЛАСЬ, и когда удаление отвергалось, — то есть кейс
        # не мог покраснеть на том, ради запрета чего написан. Ниже — три
        # решения, каждое взято из продукта, а не из соображений удобства:
        #
        #   • ПОЛОСА ОДНА. Защита живёт в СИНХРОННОЙ пред-проверке use-case'а
        #     (`securitygroup.DeleteSecurityGroupUseCase.Execute` читает
        #     `DefaultForNetwork` ДО `operations.NewFromContext`), поэтому
        #     Operation здесь не чеканится ВООБЩЕ. Второй, асинхронной полосы у
        #     этого входа нет by construction → `async_lane=False`.
        #   • 409 СНЯТ. `ALREADY_EXISTS`/`ABORTED` на этом пути ничем не
        #     производятся; исход без производителя — не терпимость, а строка,
        #     которая не покраснеет никогда (тот же класс, что снятое ожидание
        #     412 в `api-conventions.md`).
        #   • ТОН — ЧАСТЬ КОНТРАКТА, поэтому утверждается дословно, вместе с
        #     кодом gRPC. HTTP 400 здесь — механическое следствие
        #     `FAILED_PRECONDITION` по таблице края, а не отдельное решение.
        #
        # Парный опрос операции снят вместе с полосой: опрашивать нечего, а
        # прежний безусловный `GET /operations/{{opId}}` на пустом
        # идентификаторе уезжал в `/operations/` и 27 раз получал 403, после
        # чего кейс падал сообщением «expected undefined to deeply equal true»
        # — про авторизацию, которой здесь нет вовсе. (Единица счёта — элемент
        # `run.executions` отчёта прогона 31061894576: индексы 422-448, то есть
        # один опрос операции и 26 повторов чтения результата; 449 — уже уборка
        # сети. Та же цифра стоит в шапке
        # `deploy/scripts/assert-refusal-lane-has-a-reader.py`: два места об
        # одном предмете обязаны совпадать, и первая редакция здесь их развела.)
        Step(name="del-default-sg", method="DELETE",
             path="/vpc/v1/securityGroups/{{defaultSgId}}",
             test_script=[
                 *assert_refused_sync_or_async(
                     "Delete of the network's default SG",
                     sync_codes=(400,), async_lane=False),
                 *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 "pm.test('contract tone: default SG refusal names the reason', () => "
                 "pm.expect(pm.response.json().message, pm.response.text())"
                 ".to.eql('default security group cannot be deleted'));",
             ]),
        # cleanup — пытаемся удалить network в любом состоянии. DELETE of the caller's OWN
        # fresh network → v_delete owner-tuple lag can 403 under parallel load; retry SELF on
        # 403 until authorized (the [200,404] outcome assertion is preserved).
        retry_until_authorized(
            Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
                 test_script=["pm.test('cleanup attempted', () => pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
                              *save_from_response("j.id", "opId")]),
            retry_on=(403,)),
        poll_operation_until_done(),
    ],
))

# SG, привязанный к NIC через security_group_ids[], нельзя удалить. Within-service
# refcheck реализован в repo `securityGroupWriter.Delete`: ВНУТРИ writer-TX
# `SELECT id … FOR UPDATE` + `EXISTS(security_group_ids @> jsonb_build_array($id))`
# → FailedPrecondition (code 9) «security group is in use by network interface(s)».
# Проверка+DELETE в одной TX (не TOCTOU).
#
# ЗДЕСЬ СТОЯЛА ПОМЕТКА `# verifies …/kacho-vpc/issues/27`, И ОНА ПЕРЕЖИЛА СВОЙ
# ПРЕДМЕТ. Пометка означает «кейс ожидаемо КРАСНЫЙ, пока дефект открыт» (rule #13),
# то есть выкупает его из «всё обязано быть зелёным». Дефект закрыт: отказ
# реализован (`internal/repo/kacho/pg/security_group.go`) и доказан тремя
# интеграционными тестами против настоящей СУБД — `TestCQRS_SG_Delete_
# BlockedByNICReference`, `…_Concurrent_Referenced_AllBlocked`,
# `…_Concurrent_Unreferenced_ExactlyOne` (прогон 2026-08-01: 3/3 PASS, 61.8 с).
# Соседняя строка этого же комментария и таблица в docs/RESULTS.md обе называли
# дефект исправленным — то есть пометка противоречила тексту вокруг себя.
#
# Тикет при этом ОТКРЫТ (`PRO-Robotech/kacho-vpc#27`, state=OPEN на 2026-08-01), и
# в этом суть: срок жизни пометки был привязан к СОСТОЯНИЮ ТИКЕТА, а тикет — не
# дефект. Гейт `services/storage/tools/auditknownfailing` снимает запись по
# ЗАКРЫТОМУ тикету и потому не снял бы эту НИКОГДА. Устаревшее «известно, что
# красное» опаснее устаревшего красного: второе зовёт чинить, первое — не чинить.
CASES.append(Case(
    id="SG-DEL-NEG-NIC-ATTACHED",
    title="Delete SG, прилинкованного к NIC через security_group_ids → FailedPrecondition (verifies #27, fixed)",
    classes=["NEG", "STATE", "CONF"], priority="P0",
    steps=[
        Step(name="cr-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "sg-nicatt-net-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        Step(name="cr-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-nicatt-sub-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.249.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="cr-sg", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-nicatt-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        Step(name="cr-nic", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-sgatt-{{runId}}", "securityGroupIds": ["{{sgId}}"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        Step(name="assert-nic-created", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('NIC create op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        # Главная проверка: SG.Delete должна быть отвергнута.
        #
        # ПОЛОСА ЗДЕСЬ ОДНА, И ОНА АСИНХРОННАЯ. Отказ живёт в writer-TX репозитория
        # (`SELECT … FOR UPDATE` + `EXISTS(security_group_ids @> …)`), то есть его
        # выносит ВОРКЕР: синхронная фаза `DeleteSecurityGroupUseCase.Execute` для
        # well-formed не-default SG всегда чеканит Operation и отвечает `200`.
        # Прежняя редакция объявляла законной ещё и синхронную `400`, у которой на
        # этом входе НЕТ ПРОИЗВОДИТЕЛЯ, — и объявление было не безобидным: на этой
        # ветке Operation не чеканится, `opId` сбрасывается в пустое, а следующий
        # шаг `assert-sg-delete-blocked` уезжает на `/operations/` без сегмента.
        # Полоса без производителя не «терпимость к порядку», а объявленный путь,
        # который никто не читает (мерка
        # `deploy/scripts/assert-refusal-lane-has-a-reader.py`).
        retry_until_authorized(Step(name="del-sg-attached", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('Operation minted (the refusal is the worker\\'s to make)', () => "
                 "pm.expect(pm.response.json().id, pm.response.text()).to.be.a('string'));",
                 *save_from_response("j.id", "opId"),
             ])),
        poll_operation_until_done(),
        Step(name="assert-sg-delete-blocked", method="GET", path="/operations/{{opId}}",
             test_script=[
                 "const j = pm.response.json();",
                 "pm.test('sg delete op completed', () => pm.expect(j.done).to.eql(true));",
                 # SG.Delete SG'а, прилинкованного к NIC через security_group_ids[],
                 # обязана отвергаться FAILED_PRECONDITION (code 9). Объявление
                 # known-failing СНЯТО вместе с предметом: within-service refcheck
                 # реализован в writer-TX репозитория (SELECT … FOR UPDATE +
                 # EXISTS(security_group_ids @> …)), покрыт integration-тестами.
                 # Утверждение как было безусловным, так и остаётся: skip запрещён.
                 "pm.test('SG.Delete NIC-attached must fail FAILED_PRECONDITION (verifies #27)', () => {",
                 "    pm.expect(j.error, 'expected op error, got: ' + JSON.stringify(j)).to.be.an('object');",
                 "    pm.expect(j.error.code, 'expected FAILED_PRECONDITION(9)').to.eql(9);",
                 "});",
             ]),
        # Cleanup: сначала detach SG из NIC (PATCH securityGroupIds=[]),
        # затем удаление снизу вверх. Если кейс красный (refcheck нет),
        # SG уже удалена — detach/cleanup-sg просто no-op'ит.
        #
        # ИМЯ ШАГА НАЗЫВАЕТ ЕГО РОЛЬ, а не только глагол, и это требование, а не
        # оформление: предмет кейса — ОТКАЗ удаления, а этот шаг принимает и
        # успех, и отказ. Отличить «уборка вправе принять оба исхода» от «кейс
        # зеленеет на том, что запрещает» можно ровно по одному признаку — по
        # тому, объявил ли шаг себя уборкой. Прежнее имя `detach-sg-from-nic`
        # называло действие и молчало о роли, поэтому шаг попадал в находки
        # мерки `tools/mixedoutcomeaudit`, и справедливо.
        Step(name="cleanup-detach-sg-from-nic", method="PATCH", path="/vpc/v1/networkInterfaces/{{nicId}}",
             body={"updateMask": "securityGroupIds", "securityGroupIds": []},
             test_script=["pm.test('cleanup detach (200 / 400 / 404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup-nic", method="DELETE", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=["pm.test('cleanup nic (200 / 400 / 404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=["pm.test('cleanup sg (200 / 400 / 404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                          *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=["pm.test('cleanup sub (200 / 400 / 404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                          *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=["pm.test('cleanup net (200 / 400 / 404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

# `name` снят из списка — см. разбор у NET (контракт допускает пустое имя,
# `SG-CR-VAL-NAME-NULL` утверждает обратное тому, что обещал этот кейс).
for c in required_fields_matrix("SG", "/vpc/v1/securityGroups",
    {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
     "name": "sg-req-{{runId}}", "ruleSpecs": []},
    ["projectId", "networkId"]):
    CASES.append(_sg_wrap("SG", "req", c))
CASES.extend(immutable_fields_matrix("SG", "/vpc/v1/securityGroups",
    ["project_id", "network_id"]))

for c in security_injection_block("SG", "/vpc/v1/securityGroups", "/vpc/v1/securityGroups",
    {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}", "ruleSpecs": []}):
    CASES.append(_sg_wrap("SG", "sec", c))


# ===========================================================================
# SecurityGroup: network_id mandatory+immutable + same-network SG-rules.
#   Контракт детерминирован sync fast-fail: отказы — синхронные 4xx, Operation
#   НЕ создается. Concurrency и migration backfill покрываются integration-тестами,
#   в newman не воспроизводятся.
# ===========================================================================


def _two_net_steps(suffix):
    """net-A → {{netId}}, net-B → {{netBId}} в _suiteProjectId. Для cross-network SG-rule кейсов."""
    return [
        *_net_steps(suffix + "a"),  # net-A → {{netId}}
        Step(name="pre-net-b", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": f"sg-{suffix}b-net-{{{{runId}}}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netBId")]),
        poll_operation_until_done(),
    ]


def _cleanup_net_b():
    return Step(name="cleanup-net-b", method="DELETE", path="/vpc/v1/networks/{{netBId}}",
                test_script=[*save_from_response("j.id", "opId")])


# Create SG с валидным networkId → Operation → done (happy).
CASES.append(Case(
    # verifies SG-NET-02
    id="SG-NET-02-CREATE-OK",
    title="Create SG c валидным networkId → Operation done → networkId echoed (happy)",
    classes=["CRUD"], priority="P0",
    steps=[
        *_net_steps("net02"),
        Step(name="create", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-net02-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        Step(name="assert-op-ok", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('create op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        retry_until_authorized(Step(name="get", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200),
                          "pm.test('networkId echoed', () => pm.expect(pm.response.json().networkId).to.eql(pm.environment.get('netId')));"])),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))


# Create SG с несуществующим (well-formed) networkId → sync 404.
CASES.append(Case(
    # verifies SG-NET-03
    # well-formed enp… id, которого нет → sync fast-fail NOT_FOUND "Network <id> not found".
    # Отличается от SG-CR-CONF-NET-NF-TEXT (garbageVpcId) тем, что фиксирует sync-путь:
    # Operation НЕ создается.
    id="SG-NET-03-NEG-NETWORK-NOTFOUND",
    title="Create SG с well-formed несуществующим networkId → sync 404 'Network ... not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="create-nf-net", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "enp00000000000000000",
                   "name": "sg-net03-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('verbatim Network ... not found', () => pm.expect(pm.response.json().message).to.match(/^Network .* not found$/));"]),
    ],
))


# Update mask=network_id (на реальной SG) → sync 400 INVALID_ARGUMENT.
CASES.append(Case(
    # verifies SG-NET-04
    # network_id не в known-mask validateSGUpdate ({name,description,labels,rule_specs}) → unknown-field
    # → sync INVALID_ARGUMENT (immutable+mandatory). На реальной SG детерминированный 400, не 404 от
    # AuthZ-Get (в отличие от generic SG-UPD-STATE-IMMUTABLE-NETWORK-ID на garbage id).
    id="SG-NET-04-NEG-UPDATE-MASK-NETWORK",
    title="Update реальной SG с mask=network_id → sync 400 INVALID_ARGUMENT (immutable)",
    classes=["STATE", "VAL", "NEG"], priority="P1",
    steps=[
        *_net_steps("net04"),
        Step(name="create", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-net04-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch-mask-network", method="PATCH", path="/vpc/v1/securityGroups/{{sgId}}",
             # Mask-only: `networkId` is not a field of UpdateSecurityGroupRequest,
             # so the mask is the probe; a body value would be dropped by the edge.
             # verify-unchanged below still confirms the group kept its network.
             #
             # Путь маски — В ФОРМЕ КОНТРАКТА REST (`networkId`). Прежняя редакция
             # писала его змеиным (`network_id`), и край отвергал такой запрос ещё
             # при разборе тела: `FieldMask.paths contains invalid path` — protojson
             # принимает пути маски только в camelCase. Кейс был ЗЕЛЁНЫМ, ни разу не
             # дойдя до стража маски: 400 приходил, но от чужого производителя, и
             # снятие самого стража его бы не уронило. Поэтому утверждается не только
             # код, но и то, что ответил ИМЕННО страж — нарушение по полю `update_mask`
             # с именем непринятого пути.
             body={"updateMask": "networkId"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('ответил страж маски, а не разбор тела', () => {",
                          "  const d = (pm.response.json().details || []).find(x => (x.fieldViolations || []).length);",
                          "  pm.expect(d, pm.response.text()).to.be.an('object');",
                          "  const v = d.fieldViolations.find(x => x.field === 'update_mask');",
                          "  pm.expect(v, pm.response.text()).to.be.an('object');",
                          "  pm.expect(v.description).to.include('network_id');",
                          "});"])),
        retry_until_authorized(Step(name="verify-unchanged", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200),
                          "pm.test('networkId unchanged', () => pm.expect(pm.response.json().networkId).to.eql(pm.environment.get('netId')));"])),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))


# Create SG с SG-target rule на SG из другой сети → 400 + field_violations.
CASES.append(Case(
    # verifies SG-NET-07
    # SG-target rule (oneof target = security_group_id) на SG из net-B при создании SG в net-A →
    # sync INVALID_ARGUMENT + BadRequest.field_violations[].field="rule_specs[0].security_group_id".
    id="SG-NET-07-NEG-RULE-CROSS-NETWORK-CREATE",
    title="Create SG в net-A с SG-rule на SG из net-B → 400 INVALID_ARGUMENT same-network + field_violations",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    steps=[
        *_two_net_steps("net07"),
        # target SG в net-B
        Step(name="create-target-b", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netBId}}",
                   "name": "sg-net07-tgt-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgBId")]),
        poll_operation_until_done(),
        # Create SG в net-A с rule, таргетящим SG из net-B → отказ
        Step(name="create-cross-net", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-net07-{{runId}}",
                   "ruleSpecs": [{"direction": "INGRESS", "securityGroupId": "{{sgBId}}"}]},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          *_assert_rule_target_unusable("rule_specs[0].security_group_id", "sgBId"),
                          *assert_field_violation("rule_specs[0].security_group_id")]),
        Step(name="cleanup-target-b", method="DELETE", path="/vpc/v1/securityGroups/{{sgBId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_net_b(),
        _cleanup_net(),
    ],
))


# Create SG с SG-target rule на SG из той же сети → OK.
# Здесь стояло объявление known-failing: оно утверждало, что `toPb` сериализует
# ТОЛЬКО ветку `CidrBlocks`, а `SecurityGroupId`/`PredefinedTarget` роняет в
# `Target=nil`. На этой ревизии это неверно — сериализуются все три ветки
# target-oneof (`internal/dto/toproto/security_group.go`), и цепочка «запрос →
# хранение → чтение» замкнута целиком. Запись СНЯТА 2026-08-01 вместе с предметом
# (разбор — в docs/RESULTS.md); кейс обычный, без освобождений.
CASES.append(Case(
    # verifies SG-NET-08
    id="SG-NET-08-RULE-SAME-NETWORK-OK",
    title="Create SG в net-A с SG-rule на SG из той же net-A → Operation done, rule сохранен (happy)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_net_steps("net08"),
        # target SG в той же net-A
        Step(name="create-target-a", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-net08-tgt-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgAId")]),
        poll_operation_until_done(),
        Step(name="create-same-net", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-net08-{{runId}}",
                   "ruleSpecs": [{"direction": "INGRESS", "securityGroupId": "{{sgAId}}"}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        Step(name="assert-op-ok", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('same-network rule create op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        retry_until_authorized(Step(name="get", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200),
                          "const rules = pm.response.json().rules || [];",
                          "pm.test('rule targets same-network SG', () => pm.expect(rules.map(r => r.securityGroupId)).to.include(pm.environment.get('sgAId')));"])),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="cleanup-target-a", method="DELETE", path="/vpc/v1/securityGroups/{{sgAId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))


# UpdateRules добавляет SG-rule cross-network → 400 + field_violations.
CASES.append(Case(
    # verifies SG-NET-09
    # addition_rule_specs[0].security_group_id таргетит SG из другой сети → sync INVALID_ARGUMENT,
    # field="addition_rule_specs[0].security_group_id". Набор правил не изменен (атомарная замена не применена).
    id="SG-NET-09-NEG-RULE-CROSS-NETWORK-UPDATERULES",
    title="UpdateRules SG(net-A) добавляет SG-rule на SG из net-B → 400 INVALID_ARGUMENT same-network + field_violations",
    classes=["VAL", "NEG", "STATE"], priority="P1",
    steps=[
        *_two_net_steps("net09n"),
        Step(name="create-target-b", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netBId}}",
                   "name": "sg-net09n-tgt-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgBId")]),
        poll_operation_until_done(),
        Step(name="create-sg-a", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-net09n-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="update-rules-cross", method="PATCH", path="/vpc/v1/securityGroups/{{sgId}}/rules",
             body={"additionRuleSpecs": [{"direction": "INGRESS", "securityGroupId": "{{sgBId}}"}]},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          *_assert_rule_target_unusable("addition_rule_specs[0].security_group_id", "sgBId"),
                          *assert_field_violation("addition_rule_specs[0].security_group_id")])),
        retry_until_authorized(Step(name="verify-no-rules", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200),
                          "pm.test('rules unchanged (none added)', () => pm.expect((pm.response.json().rules || []).length).to.eql(0));"])),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="cleanup-target-b", method="DELETE", path="/vpc/v1/securityGroups/{{sgBId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_net_b(),
        _cleanup_net(),
    ],
))


# UpdateRules добавляет SG-rule same-network → OK.
# Объявление known-failing СНЯТО 2026-08-01 вместе с предметом — та же причина,
# что и у SG-NET-08: `toPb` сериализует все три ветки target-oneof. Кейс обычный.
CASES.append(Case(
    # verifies SG-NET-09
    # Positive per-endpoint через UpdateRules: same-network SG-target → done.
    id="SG-NET-09-RULE-SAME-NETWORK-UPDATERULES-OK",
    title="UpdateRules SG(net-A) добавляет SG-rule на SG из той же net-A → Operation done, rule виден (happy)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_net_steps("net09p"),
        Step(name="create-target-a", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-net09p-tgt-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgAId")]),
        poll_operation_until_done(),
        Step(name="create-sg", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-net09p-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="update-rules-same", method="PATCH", path="/vpc/v1/securityGroups/{{sgId}}/rules",
             body={"additionRuleSpecs": [{"direction": "INGRESS", "securityGroupId": "{{sgAId}}"}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="assert-op-ok", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('same-network updateRules op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        # Read-your-writes: owner-tuple свежего SG материализуется
        # eventually-consistent → оборачиваем первый read-доступ ретраем.
        retry_until_authorized(Step(name="get", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*assert_status(200),
                          "const rules = pm.response.json().rules || [];",
                          "pm.test('rule targets same-network SG', () => pm.expect(rules.map(r => r.securityGroupId)).to.include(pm.environment.get('sgAId')));"])),
        retry_until_authorized(Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="cleanup-target-a", method="DELETE", path="/vpc/v1/securityGroups/{{sgAId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))
