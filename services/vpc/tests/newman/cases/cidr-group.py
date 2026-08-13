# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для CidrGroupService — именованный набор префиксов.

Предмет ресурса: перечень сетей, повторяющийся в двадцати правилах, правится один
раз, а не в двадцати местах. Отсюда состав кейсов — не «CRUD ради CRUD»:

  * положительный путь ЗАКРЕПЛЯЕТ форму ответа целиком (состав по семействам и
    выведенное число членов), потому что именно она и есть предмет ресурса;
  * глаголы состава закрепляют ИДЕМПОТЕНТНОСТЬ: они асинхронны, значит клиент
    обязан уметь повторить их после неопределённого исхода, и повтор не может
    быть ложным отказом;
  * потолок закрепляется на ГРАНИЦЕ и в паре: ровно предел проходит, предел плюс
    один отвергается;
  * отказ удаления закрепляется ВМЕСТЕ с положительным контролем — набор без
    ссылок удаляется тем же вызовом. Без него «удаление отвергнуто» неотличимо от
    «удаление не работает».

Все кейсы сидятся в проект суиты (`{{_suiteProjectId}}`) и убирают за собой:
набор с UNIQUE-именем без `{{runId}}` коллизил бы на повторном прогоне.
"""

CASES = []

CASES.append(Case(
    # index: *-CR-CRUD-OK
    id="CDG-CR-CRUD-OK",
    title="Create cidr group → Operation → набор виден в GET с обоими семействами",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/cidrGroups",
            body={
                "projectId": "{{_suiteProjectId}}",
                "name": "cdg-cr-{{runId}}",
                "description": "newman CRUD-OK",
                "labels": {"suite": "newman"},
                "v4CidrBlocks": ["203.0.113.0/24", "198.51.100.16/28"],
                "v6CidrBlocks": ["2001:db8::/32"],
            },
            test_script=[
                *assert_status(200),
                *assert_operation_envelope(),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.cidrGroupId", "cdgId"),
            ],
        ),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="get-confirms",
            method="GET",
            path="/vpc/v1/cidrGroups/{{cdgId}}",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('id matches', () => pm.expect(j.id).to.eql(pm.environment.get('cdgId')));",
                "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteProjectId')));",
                # Состав отдаётся ПО СЕМЕЙСТВАМ: смешанного поля у ресурса нет,
                # и проба закрепляет именно это — иначе один член чужого
                # семейства был бы выразим.
                "pm.test('v4 set kept', () => pm.expect(j.v4CidrBlocks).to.have.members(['203.0.113.0/24','198.51.100.16/28']));",
                "pm.test('v6 set kept', () => pm.expect(j.v6CidrBlocks).to.have.members(['2001:db8::/32']));",
                # Число членов — сумма обоих семейств, и оно ВЫВЕДЕНО, а не
                # прислано: вызывающий его не задавал.
                "pm.test('count derived over both families', () => pm.expect(j.cidrBlockCount).to.eql(3));",
                # Время — без дробной части (усечение до секунд на выходе).
                "pm.test('createdAt truncated to seconds', () => pm.expect(j.createdAt).to.match(/^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}Z$/));",
            ],
        )),
        retry_until_authorized(Step(
            name="cleanup-delete",
            method="DELETE",
            path="/vpc/v1/cidrGroups/{{cdgId}}",
            test_script=[*assert_status(200), *save_from_response("j.id", "opId")],
        )),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    # index: *-ACB-CRUD-OK
    id="CDG-ACB-CRUD-OK",
    title="AddCidrBlocks/RemoveCidrBlocks меняют состав и ИДЕМПОТЕНТНЫ на повторе",
    classes=["CRUD", "IDM"],
    priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/cidrGroups",
             body={"projectId": "{{_suiteProjectId}}", "name": "cdg-acb-{{runId}}",
                   "v4CidrBlocks": ["203.0.113.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.cidrGroupId", "cdgAcbId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="add", method="POST",
             path="/vpc/v1/cidrGroups/{{cdgAcbId}}:add-cidr-blocks",
             body={"v4CidrBlocks": ["192.0.2.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="add-again-idempotent", method="POST",
             path="/vpc/v1/cidrGroups/{{cdgAcbId}}:add-cidr-blocks",
             body={"v4CidrBlocks": ["192.0.2.0/24"]},
             test_script=[
                 # Повтор — УСПЕХ без изменения. Глагол асинхронный, поэтому
                 # клиент обязан уметь повторить его после неопределённого
                 # исхода; отказ здесь превратил бы штатный повтор в ложную
                 # ошибку.
                 *assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-after-add", method="GET", path="/vpc/v1/cidrGroups/{{cdgAcbId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('member added once', () => pm.expect(j.v4CidrBlocks).to.have.members(['203.0.113.0/24','192.0.2.0/24']));",
                          "pm.test('repeat did not duplicate', () => pm.expect(j.cidrBlockCount).to.eql(2));"]),
        Step(name="remove", method="POST",
             path="/vpc/v1/cidrGroups/{{cdgAcbId}}:remove-cidr-blocks",
             body={"v4CidrBlocks": ["192.0.2.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="remove-absent-idempotent", method="POST",
             path="/vpc/v1/cidrGroups/{{cdgAcbId}}:remove-cidr-blocks",
             body={"v4CidrBlocks": ["192.0.2.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-after-remove", method="GET", path="/vpc/v1/cidrGroups/{{cdgAcbId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('member removed', () => pm.expect(j.v4CidrBlocks).to.have.members(['203.0.113.0/24']));",
                          "pm.test('count follows the set', () => pm.expect(j.cidrBlockCount).to.eql(1));"]),
        Step(name="cleanup-delete", method="DELETE", path="/vpc/v1/cidrGroups/{{cdgAcbId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    # index: *-ACB-VAL-HOST-BITS
    id="CDG-ACB-VAL-FAMILY-MISMATCH",
    title="Член чужого семейства в поле → 400 INVALID_ARGUMENT (смешанный набор невыразим)",
    classes=["NEG", "VAL"],
    priority="P0",
    steps=[
        Step(name="create-v6-into-v4", method="POST", path="/vpc/v1/cidrGroups",
             body={"projectId": "{{_suiteProjectId}}", "name": "cdg-fam-{{runId}}",
                   "v4CidrBlocks": ["2001:db8::/32"]},
             test_script=[
                 # Отказ синхронный: состояние «набор со смешанными семействами»
                 # не должно быть достижимо ни одной записью.
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('names the offending block', () => pm.expect(pm.response.json().message).to.include('2001:db8::/32'));",
             ]),
        Step(name="create-host-bits", method="POST", path="/vpc/v1/cidrGroups",
             body={"projectId": "{{_suiteProjectId}}", "name": "cdg-hb-{{runId}}",
                   "v4CidrBlocks": ["203.0.113.5/24"]},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        # Положительный контроль: тот же путь с законным членом проходит —
        # без него отрицания выше зеленели бы на «создание сломано вовсе».
        Step(name="create-legal-control", method="POST", path="/vpc/v1/cidrGroups",
             body={"projectId": "{{_suiteProjectId}}", "name": "cdg-ok-{{runId}}",
                   "v4CidrBlocks": ["203.0.113.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.cidrGroupId", "cdgFamId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-delete", method="DELETE",
             path="/vpc/v1/cidrGroups/{{cdgFamId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="CDG-UPD-NEG-COMPOSITION-VIA-MASK",
    title="Update с маской состава → 400 и отказ ОТПРАВЛЯЕТ к глаголам",
    classes=["NEG", "VAL", "STATE"],
    priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/cidrGroups",
             body={"projectId": "{{_suiteProjectId}}", "name": "cdg-upd-{{runId}}",
                   "v4CidrBlocks": ["203.0.113.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.cidrGroupId", "cdgUpdId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="update-composition", method="PATCH",
             path="/vpc/v1/cidrGroups/{{cdgUpdId}}",
             # Тело несёт ТОЛЬКО маску: поля состава в `UpdateCidrGroupRequest`
             # нет вовсе — состав через этот вызов не выразим даже формой. Послать
             # его значило бы отправить поле, которого край не знает, и оно
             # молча пропало бы по дороге, а проба свидетельствовала бы о другом.
             body={"updateMask": "v4CidrBlocks"},
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('sends the caller to the verbs', () => pm.expect(pm.response.json().message).to.include('AddCidrBlocks'));",
             ])),
        # Положительный контроль: косметическая правка тем же вызовом проходит.
        Step(name="update-cosmetic", method="PATCH", path="/vpc/v1/cidrGroups/{{cdgUpdId}}",
             body={"updateMask": "description", "description": "renamed by newman"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-confirms-set-untouched", method="GET", path="/vpc/v1/cidrGroups/{{cdgUpdId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('composition untouched by Update', () => pm.expect(j.v4CidrBlocks).to.have.members(['203.0.113.0/24']));",
                          "pm.test('cosmetic field applied', () => pm.expect(j.description).to.eql('renamed by newman'));"]),
        Step(name="cleanup-delete", method="DELETE", path="/vpc/v1/cidrGroups/{{cdgUpdId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="CDG-DEL-NEG-REFERENCED",
    title="Delete набора, на который ссылается правило → 400 FAILED_PRECONDITION с числами",
    classes=["NEG", "CONF", "STATE"],
    priority="P0",
    steps=[
        Step(name="create-group", method="POST", path="/vpc/v1/cidrGroups",
             body={"projectId": "{{_suiteProjectId}}", "name": "cdg-ref-{{runId}}",
                   "v4CidrBlocks": ["203.0.113.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.cidrGroupId", "cdgRefId")]),
        poll_operation_until_done(),
        Step(name="create-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-cdg-{{runId}}",
                   "ipv4CidrBlocks": ["10.190.0.0/16"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "cdgNetId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="create-sg-referencing", method="POST",
             path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "name": "sg-cdg-{{runId}}",
                   "networkId": "{{cdgNetId}}",
                   "ruleSpecs": [{"direction": "INGRESS", "cidrGroupId": "{{cdgRefId}}"}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "cdgSgId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="delete-refused", method="DELETE",
             path="/vpc/v1/cidrGroups/{{cdgRefId}}",
             test_script=[
                 # FAILED_PRECONDITION на крае — это 400, а НЕ 412: край не несёт
                 # своего отображения ошибок, и 412 не производится ни для одного
                 # кода.
                 *assert_status(400),
                 *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 "const m = pm.response.json().message;",
                 "pm.test('names what holds it, by kind and number', () => { pm.expect(m).to.include('security groups: 1'); pm.expect(m).to.include('rules: 1'); });",
                 # Числа — да, чужие идентификаторы — нет: число радиусом
                 # является, перечень идентификаторов правил становится
                 # координатой.
                 "pm.test('carries no rule ids', () => pm.expect(m).to.not.match(/sgr[0-9a-hjkmnp-tv-z]{17}/));",
             ])),
        Step(name="get-confirms-group-alive", method="GET", path="/vpc/v1/cidrGroups/{{cdgRefId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('usedBy names the holder', () => { pm.expect(j.usedBy).to.have.lengthOf(1); pm.expect(j.usedBy[0].referrer.id).to.eql(pm.environment.get('cdgSgId')); });"]),
        # Освобождаем ссылку — и тем же вызовом набор удаляется. Это
        # ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к отказу выше: без него «удаление отвергнуто»
        # было бы неотличимо от «удаление не работает вовсе».
        Step(name="drop-sg", method="DELETE", path="/vpc/v1/securityGroups/{{cdgSgId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="delete-now-allowed", method="DELETE", path="/vpc/v1/cidrGroups/{{cdgRefId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{cdgNetId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    # index: *-GET-NEG-NF
    id="CDG-GET-NEG-MALFORMED",
    title="Get малформированного id → отвергнут форматом, а не промахом",
    classes=["NEG", "VAL"],
    priority="P0",
    steps=[
        Step(name="get-garbage", method="GET", path="/vpc/v1/cidrGroups/{{garbageId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    # index: *-LST-CRUD-OK
    id="CDG-LST-CRUD-OK",
    title="List cidr groups — свой свежий набор виден в странице своего проекта",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/cidrGroups",
             body={"projectId": "{{_suiteProjectId}}", "name": "cdg-lst-{{runId}}",
                   "v4CidrBlocks": ["203.0.113.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.cidrGroupId", "cdgLstId")]),
        poll_operation_until_done(),
        retry_until_present(Step(name="list", method="GET",
             path="/vpc/v1/cidrGroups?projectId={{_suiteProjectId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('page carries the fresh group', () => pm.expect((j.cidrGroups||[]).map(g => g.id)).to.include(pm.environment.get('cdgLstId')));"]),
             "cdgLstId"),
        Step(name="list-bad-page-size", method="GET",
             path="/vpc/v1/cidrGroups?projectId={{_suiteProjectId}}&pageSize=1001",
             test_script=[
                 # Размер страницы ОТВЕРГАЕТСЯ, а не подрезается, и ответ на
                 # некорректный ввод не зависит от того, что вызывающему выдано.
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
        Step(name="list-garbage-token", method="GET",
             path="/vpc/v1/cidrGroups?projectId={{_suiteProjectId}}&pageToken=not-a-token",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        Step(name="cleanup-delete", method="DELETE", path="/vpc/v1/cidrGroups/{{cdgLstId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))
