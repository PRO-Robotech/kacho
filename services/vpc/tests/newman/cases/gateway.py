# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для GatewayService.

ЯКОРЬ РАЗМЕЩЕНИЯ СУИТЫ — В ПОСЕВЕ, А НЕ В КЕЙСЕ. Шлюз без подсети не создаётся
ВОВСЕ (`subnetId` обязателен, он же якорь размещения), а NAT-шлюз обязан стоять в
подсети, несущей IPv4, поэтому почти каждый кейс ниже читает `{{gwAnchorSubId}}`.
Заводит его setup-папка коллекции `_SETUP-GW-ANCHOR` (объявлена в
`scripts/gen.py`, исполняется первой в полном прогоне) — не кейс.

ПРОГОН ОДИНОЧНОГО КЕЙСА. `--folder <кейс>` исполняет только названную точку
входа, поэтому окружение для него готовится заранее, ОДНОЙ командой:

    ./setup.sh && newman run collections/gateway.postman_collection.json \\
        -e environments/local.postman_environment.json --folder GW-CR-CRUD-OK

`setup.sh` гоняет ту же самую setup-папку и выгружает id якоря в файл окружения,
поэтому объявление якоря остаётся одно, а отладка одиночного кейса больше не
упирается в фикстуру, которой в её точке входа нет. Без этого шага тело запроса
уезжает с неразрешённым `{{gwAnchorSubId}}` и кейс падает, называя виновником
невиновного.
"""

CASES = []

CASES.append(Case(
    id="GW-CR-CRUD-OK",
    title="Create gateway + Get",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/gateways",
             body={"projectId": "{{_suiteProjectId}}", "name": "gw-cr-{{runId}}",
                   "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.gatewayId", "gwId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path="/vpc/v1/gateways/{{gwId}}",
             test_script=[*assert_status(200),
                          "pm.test('id matches', () => pm.expect(pm.response.json().id).to.eql(pm.environment.get('gwId')));"])),
        retry_until_authorized(Step(name="cleanup", method="DELETE", path="/vpc/v1/gateways/{{gwId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="GW-CR-VAL-PROJECT-REQUIRED",
    title="Create без project → InvalidArgument",
    classes=["VAL"],
    priority="P0",
    steps=[
        Step(name="cr", method="POST", path="/vpc/v1/gateways",
             body={"name": "gw-nf-{{runId}}", "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"},
             test_script=[*assert_unscoped_rejected()]),
    ],
))

CASES.append(Case(
    id="GW-GET-NEG-NF",
    title="Get malformed id → 400 InvalidArgument 'invalid gateway id'",
    classes=["NEG"],
    priority="P0",
    steps=[
        Step(name="get-garbage", method="GET", path="/vpc/v1/gateways/{{garbageId}}",
             test_script=[
                 # malformed id (нет известного 3-char префикса)
                 # → 400 InvalidArgument "invalid gateway id '<X>'". Проверка family-agnostic.
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('mentions invalid id', () => { const m = pm.response.json().message; pm.expect(m).to.include('invalid'); pm.expect(m).to.include('id'); });",
             ]),
    ],
))

CASES.append(Case(
    id="GW-LST-CRUD-OK",
    title="List gateways",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(name="list", method="GET", path="/vpc/v1/gateways?projectId={{_suiteProjectId}}",
             test_script=[*assert_status(200),
                          "pm.test('gateways array', () => pm.expect(pm.response.json().gateways || []).to.be.an('array'));"]),
    ],
))

CASES.append(Case(
    id="GW-LST-VAL-PROJECT-REQUIRED",
    title="List без project → InvalidArgument",
    classes=["VAL", "AUTHZ"],
    priority="P0",
    steps=[
        Step(name="list-noproject", method="GET", path="/vpc/v1/gateways",
             test_script=[*assert_unscoped_rejected()]),
    ],
))

CASES.append(Case(
    id="GW-UPD-AUTHZ-NF-SYNC",
    title="Update несуществующего → sync 404",
    classes=["NEG", "AUTHZ"],
    priority="P1",
    steps=[
        Step(name="patch-nx", method="PATCH", path="/vpc/v1/gateways/{{garbageVpcId}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="GW-DEL-AUTHZ-NF-SYNC",
    title="Delete несуществующего → sync 404",
    classes=["NEG", "AUTHZ"],
    priority="P1",
    steps=[
        Step(name="del-nx", method="DELETE", path="/vpc/v1/gateways/{{garbageVpcId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="GW-LOP-CRUD-OK",
    title="ListOperations Gateway",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(name="create-gw", method="POST", path="/vpc/v1/gateways",
             body={"projectId": "{{_suiteProjectId}}", "name": "gw-lop-{{runId}}",
                   "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.gatewayId", "gwId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="list-ops", method="GET", path="/vpc/v1/gateways/{{gwId}}/operations",
             test_script=[*assert_status(200),
                          "pm.test('at least 1 op', () => pm.expect((pm.response.json().operations || []).length).to.be.at.least(1));"])),
        retry_until_authorized(Step(name="cleanup", method="DELETE", path="/vpc/v1/gateways/{{gwId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ],
))

# Расширение
CASES.extend(crud_list_bva_block("GW", "/vpc/v1/gateways"))
CASES.append(conf_not_found_text("GW", "/vpc/v1/gateways", "Gateway"))
CASES.append(state_update_unknown_mask("GW", "/vpc/v1/gateways"))

CASES.append(Case(
    id="GW-UPD-CRUD-OK",
    title="Update Gateway description",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="create-gw", method="POST", path="/vpc/v1/gateways",
             body={"projectId": "{{_suiteProjectId}}", "name": "gw-upd-{{runId}}",
                   "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.gatewayId", "gwId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch", method="PATCH", path="/vpc/v1/gateways/{{gwId}}",
             body={"updateMask": "description", "description": "upd-newman"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup", method="DELETE", path="/vpc/v1/gateways/{{gwId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ],
))

# Дополнение: STATE immutable project + VAL move-no-dest + BVA pagesize=1
CASES.append(state_immutable_project("GW", "/vpc/v1/gateways"))
CASES.append(list_pagesize_1_bva("GW", "/vpc/v1/gateways"))

CASES.append(Case(
    id="GW-UPD-CONF-NF-TEXT",
    title="Update несуществующего → verbatim 'Gateway ... not found' text",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="patch-nx", method="PATCH",
             path="/vpc/v1/gateways/{{garbageVpcId}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="GW-DEL-CONF-NF-TEXT",
    title="Delete несуществующего → verbatim 'Gateway ... not found' text",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="del-nx", method="DELETE",
             path="/vpc/v1/gateways/{{garbageVpcId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="GW-DEL-CRUD-OK",
    title="Gateway Delete happy",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/gateways",
             body={"projectId": "{{_suiteProjectId}}", "name": "gw-delok-{{runId}}",
                   "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.gatewayId", "gwId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-happy", method="DELETE", path="/vpc/v1/gateways/{{gwId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ],
))

# GW-CR-NEG-PROJECT-NF — P0-негатив, который прежде не утверждал отказ НИ В ОДНОЙ
# из двух своих ветек: `oneOf([200, 403])` принимал оба исхода, ветка 200 лишь
# запоминала id операции, ветка else записывала пустую строку — и следом поллилась
# операция, которой не существует. То есть кейс занимал слот P0 и не мог упасть.
#
# Исход здесь ровно один и он детерминирован: создание шлюза авторизуется
# отношением `editor` на ПРОЕКТЕ из тела запроса. У несуществующего проекта нет ни
# одной записи в модели прав, поэтому проверка на краю отказывает fail-closed ДО
# сервиса — асинхронная операция не создаётся вовсе, и поллить нечего.
CASES.append(Case(
    id="GW-CR-NEG-PROJECT-NF",
    title="Create Gateway с nonexistent project → 403 fail-closed на краю (операция не создаётся)",
    classes=["NEG", "CONF"], priority="P0",
    steps=[
        Step(name="create-bad-project", method="POST", path="/vpc/v1/gateways",
             body={"projectId": "{{garbageRmId}}", "name": "gw-fnf-{{runId}}",
                   "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"},
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
                 # Отказ не сообщает, существует ли названный проект: иначе он же и
                 # есть оракул существования (см. security.md hide-existence).
                 "pm.test('отказ не подтверждает и не опровергает существование проекта', () => {",
                 "  const m = String(pm.response.json().message || '');",
                 "  pm.expect(m, 'текст отказа').to.not.contain('not found');",
                 "});",
             ]),
    ],
))

CASES.append(Case(
    id="GW-LOP-NEG-PARENT-NF",
    title="ListOperations несуществующего Gateway → 200 или 404",
    classes=["NEG"], priority="P2",
    steps=[
        Step(name="lop-nx", method="GET",
             path="/vpc/v1/gateways/{{garbageVpcId}}/operations",
             test_script=["pm.test('200/403/404', () => pm.expect(pm.response.code).to.be.oneOf([200, 403, 404]));"]),
    ],
))

# Gateway — единственный VPC-ресурс со СТРОГИМ контрактом имени
# (`corevalidate.NameGateway`: только строчные буквы, цифры и дефис, без
# заглавных и подчёркиваний). Это by-design, объявленное независимо от прогона:
# см. `services/vpc/docs/engineering/architecture/07-known-divergences.md` §22.
CASES.extend(ecp_name_block("GW", "/vpc/v1/gateways", {"natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.extend(ecp_description_block("GW", "/vpc/v1/gateways", {"natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.extend(ecp_labels_block("GW", "/vpc/v1/gateways", {"natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.extend(updatemask_decision_table("GW", "/vpc/v1/gateways"))
CASES.extend(filter_syntax_block("GW", "/vpc/v1/gateways"))
CASES.append(pagination_roundtrip("GW", "/vpc/v1/gateways"))

CASES.extend(update_happy_per_field("GW", "/vpc/v1/gateways", "/vpc/v1/gateways",
    {"projectId": "{{_suiteProjectId}}", "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.extend(perf_baseline_block("GW", "/vpc/v1/gateways"))
CASES.extend(verbatim_text_pack("GW", "Gateway", "/vpc/v1/gateways"))
CASES.extend(authz_caller_headers_block("GW", "/vpc/v1/gateways"))

CASES.append(update_happy_multi_field("GW", "/vpc/v1/gateways", "/vpc/v1/gateways",
    {"projectId": "{{_suiteProjectId}}", "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.append(cross_project_resource_block("GW", "/vpc/v1/gateways", {"natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.append(list_filter_match_block("GW", "/vpc/v1/gateways",
    {"projectId": "{{_suiteProjectId}}", "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.extend(neg_invalid_types_block("GW", "/vpc/v1/gateways",
    {"projectId": "{{_suiteProjectId}}", "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.extend(http_method_not_allowed_block("GW", "/vpc/v1/gateways"))
CASES.extend(malformed_body_block("GW", "/vpc/v1/gateways"))

# NB: имена Gateway НЕ уникальны — дубль-имя создается успешно;
# поэтому generated alreadyexists_dup_name_for("GW", ...) здесь не применяется.
CASES.extend(update_mask_partial_block("GW", "/vpc/v1/gateways", "/vpc/v1/gateways",
    {"projectId": "{{_suiteProjectId}}", "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.append(perf_baseline_get_block("GW", "/vpc/v1/gateways",
    {"projectId": "{{_suiteProjectId}}", "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.extend(list_total_size_check_block("GW", "/vpc/v1/gateways"))
CASES.extend(headers_content_type_block("GW", "/vpc/v1/gateways",
    {"projectId": "{{_suiteProjectId}}", "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))

# v10 Gateway-specific
CASES.append(Case(
    id="GW-CR-VAL-MISSING-TYPE",
    title="Create Gateway без ветви вида → 400 InvalidArgument 'gateway: required'",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-notype", method="POST", path="/vpc/v1/gateways",
                body={"projectId": "{{_suiteProjectId}}", "name": "gw-nt-{{runId}}",
                      "subnetId": "{{gwAnchorSubId}}"},
                test_script=[
                    # Ветвь вида обязательна и отвергается ИМЕНЕМ ПОЛЯ. Якорь в теле
                    # ЕСТЬ — иначе кейс не отличал бы отказ по виду от отказа по якорю
                    # и зеленел бы на любом из двух.
                    *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                    "pm.test('отказ называет поле вида', () => "
                    "pm.expect(String(pm.response.json().message || '')).to.eql('gateway: required'));",
                ])],
))

CASES.append(Case(
    id="GW-LST-FILTER-EMPTY",
    title="List Gateway с пустым filter expression → 200 (filter optional)",
    classes=["FILTER", "CRUD"], priority="P2",
    steps=[Step(name="lst-empty-filter", method="GET",
                path="/vpc/v1/gateways?projectId={{_suiteProjectId}}&filter=",
                test_script=[*assert_status(200)])],
))

CASES.append(Case(
    id="GW-GET-WITH-QUERY-PARAMS",
    title="Get Gateway с дополнительными query params → 200 (ignored)",
    classes=["VAL", "CRUD"], priority="P3",
    steps=[Step(name="get-extra-params", method="GET",
                path="/vpc/v1/gateways/{{garbageVpcId}}?extra=ignored&another=param",
                test_script=["pm.test('404 with NOT_FOUND', () => pm.expect(pm.response.code).to.eql(404));"])],
))

CASES.append(Case(
    id="GW-LST-FILTER-CASE-SENSITIVITY",
    title="Filter case-sensitivity на name field",
    classes=["FILTER"], priority="P3",
    steps=[Step(name="lst-case", method="GET",
                path="/vpc/v1/gateways?projectId={{_suiteProjectId}}&filter=name%3D%22NONEXISTENT-UPPER%22",
                test_script=[*assert_status(200),
                             "pm.test('no matches', () => pm.expect((pm.response.json().gateways || []).length).to.eql(0));"])],
))

# v11 edge cases
CASES.append(Case(
    id="GW-LST-PAGE-NEGATIVE-SIZE",
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
                path="/vpc/v1/gateways?projectId={{_suiteProjectId}}&pageSize=-1",
                test_script=[
                    *assert_status(400),
                    *assert_grpc_code(3, "INVALID_ARGUMENT"),
                    "pm.test('names the offending field', () => pm.expect(JSON.stringify(pm.response.json())).to.contain('page_size'));",
                ])],
))

CASES.append(Case(
    id="GW-LST-FILTER-SPECIAL-CHARS",
    # Исход УСТАНОВЛЕН, а не «как получится»: `name="!@#$%"` разбирается штатно —
    # поле из белого списка, значение в кавычках, хвоста нет, — поэтому запрос
    # уходит параметром и страница приходит пустой. Спец-символы значения на
    # разбор не влияют вовсе: они внутри кавычек. Прежнее `oneOf([200, 400])`
    # принимало и отказ, то есть ту самую регрессию разбора, ради которой кейс и
    # написан.
    title="List с filter из спец-символов → 200 и пустая страница",
    classes=["FILTER", "VAL"], priority="P3",
    steps=[Step(name="lst-fsc", method="GET",
                path="/vpc/v1/gateways?projectId={{_suiteProjectId}}&filter=name%3D%22%21%40%23%24%25%22",
                test_script=assert_empty_page("значение из спец-символов именем ни у кого не является"))],
))

CASES.append(Case(
    id="GW-LST-PAGESIZE-EXACTLY-1000",
    title="List с pageSize=1000 (boundary max) → 200",
    classes=["BVA"], priority="P2",
    steps=[Step(name="lst-max", method="GET",
                path="/vpc/v1/gateways?projectId={{_suiteProjectId}}&pageSize=1000",
                test_script=[*assert_status(200)])],
))

CASES.append(Case(
    id="GW-LST-PAGESIZE-1001",
    title="List с pageSize=1001 (over max) → 400",
    classes=["BVA", "VAL"], priority="P1",
    steps=[Step(name="lst-1001", method="GET",
                path="/vpc/v1/gateways?projectId={{_suiteProjectId}}&pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="GW-LST-DOUBLE-PROJECT-PARAM",
    # «last wins» не происходит НИКОГДА: `project_id` — скалярное поле запроса, и
    # `runtime.PopulateQueryParameters` отвергает второе значение целиком
    # (`too many values for field "project_id"`), а сгенерированный обработчик края
    # переводит это в `InvalidArgument`. Проверка прав до отказа доходит и его не
    # опережает: она читает первое значение (`url.Values.Get`), а оно — свой же
    # проект актора. Значит исход один: 400 и код 3.
    title="List с дубликатом projectId param → 400 InvalidArgument (край отвергает второе значение)",
    classes=["VAL"], priority="P3",
    steps=[Step(name="lst-dup", method="GET",
                path="/vpc/v1/gateways?projectId={{_suiteProjectId}}&projectId={{_suiteProjectCrossId}}&pageSize=10",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('сообщение называет поле, из-за которого отказ', () => pm.expect(String(pm.response.json().message || ''), pm.response.text()).to.contain('project_id'));"])],
))

CASES.append(Case(
    id="GW-GET-TRAILING-SLASH",
    title="Get с trailing slash → 404",
    classes=["VAL"], priority="P3",
    steps=[Step(name="get-trail", method="GET", path="/vpc/v1/gateways/{{garbageVpcId}}/",
                test_script=["pm.test('non-2xx', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));"])],
))

# `name` снят из списка — см. разбор у NET (контракт допускает пустое имя,
# `GW-CR-VAL-NAME-NULL` утверждает обратное тому, что обещал этот кейс).
CASES.extend(required_fields_matrix("GW", "/vpc/v1/gateways",
    {"projectId": "{{_suiteProjectId}}", "name": "gw-req-{{runId}}",
     "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"},
    ["projectId"]))
CASES.extend(immutable_fields_matrix("GW", "/vpc/v1/gateways",
    ["project_id"]))

CASES.extend(security_injection_block("GW", "/vpc/v1/gateways", "/vpc/v1/gateways",
    {"projectId": "{{_suiteProjectId}}", "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))
CASES.append(conformance_lifecycle_pack("GW", "/vpc/v1/gateways",
    {"projectId": "{{_suiteProjectId}}", "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"}))

CASES.append(Case(
    id="GW-CR-VAL-MISSING-ANCHOR",
    # index: GW-ANCHOR-01
    title="Create Gateway без subnetId → 400 InvalidArgument 'subnet_id: required'",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-noanchor", method="POST", path="/vpc/v1/gateways",
                body={"projectId": "{{_suiteProjectId}}", "name": "gw-na-{{runId}}",
                      "natGatewaySpec": {}},
                test_script=[
                    # Ветвь вида в теле ЕСТЬ — отказ обязан быть про якорь, а не про вид.
                    *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                    "pm.test('отказ называет поле якоря', () => "
                    "pm.expect(String(pm.response.json().message || '')).to.eql('subnet_id: required'));",
                ])],
))

CASES.append(Case(
    id="GW-CR-CONF-EGRESS-ONLY-NEEDS-V6",
    # index: GW-ANCHOR-01
    title="Шлюз «только исход» в подсети без IPv6 → отказ по состоянию якоря",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="cr-eo-on-v4", method="POST", path="/vpc/v1/gateways",
             body={"projectId": "{{_suiteProjectId}}", "name": "gw-eo-{{runId}}",
                   "subnetId": "{{gwAnchorSubId}}", "egressOnlyGatewaySpec": {}},
             test_script=[*assert_refused_sync_or_async(
                 "шлюз «только исход» в подсети без IPv6", sync_codes=(400, 403, 404))]),
        poll_operation_until_done(must_fail=True),
    ],
))

# ---------------------------------------------------------------------------
# Внешний адрес шлюза трансляции
# ---------------------------------------------------------------------------
# Предусловие этих кейсов — пул по умолчанию для зоны якоря; его заводит посев
# коллекции (`_SETUP-POOL`, `scripts/gen.py`), а не кейс. Без пула отказ был бы
# «нет доступного внешнего адреса IPv4», и кейс винил бы невиновного.

CASES.append(Case(
    id="GW-CR-NAT-EXTERNAL-ADDRESS-OK",
    title="Шлюз трансляции несёт внешний адрес, и адрес называет шлюз в ответ",
    classes=["CRUD", "CONF"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/gateways",
             body={"projectId": "{{_suiteProjectId}}", "name": "gw-extaddr-{{runId}}",
                   "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.gatewayId", "gwXaId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="get-gateway", method="GET", path="/vpc/v1/gateways/{{gwXaId}}",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('ветвь трансляции присутствует', () => "
                "pm.expect(j.natGateway, pm.response.text()).to.be.an('object'));",
                "pm.test('шлюз трансляции несёт внешний адрес', () => "
                "pm.expect(String((j.natGateway || {}).addressId || ''), pm.response.text())"
                ".to.match(/^adr/));",
                *save_from_response("(j.natGateway || {}).addressId", "gwXaAddrId"),
            ])),
        # Адрес — самостоятельный ресурс, и владельцу шлюза он ЧИТАЕМ: без права
        # на него идентификатор в контракте был бы координатой в никуда.
        retry_until_authorized(Step(
            name="get-address", method="GET", path="/vpc/v1/addresses/{{gwXaAddrId}}",
            test_script=[
                *assert_status(200),
                "const a = pm.response.json();",
                "pm.test('внешний IPv4', () => {",
                "  pm.expect(a.type, pm.response.text()).to.eql('EXTERNAL');",
                "  pm.expect(a.ipVersion, pm.response.text()).to.eql('IPV4');",
                "});",
                "pm.test('адресу выдан IP', () => pm.expect("
                "String(((a.externalIpv4Address || {}).address) || ''), pm.response.text())"
                ".to.not.be.empty);",
                "pm.test('адрес помечен занятым', () => "
                "pm.expect(a.used, pm.response.text()).to.eql(true));",
                # Обратная сторона привязки: владелец адреса называет ИМЕННО этот шлюз.
                "pm.test('адрес называет свой шлюз', () => {",
                "  const refs = (a.usedBy || []).map(u => (u.referrer || {}));",
                "  pm.expect(refs.map(r => r.id), pm.response.text())"
                ".to.include(pm.environment.get('gwXaId'));",
                "  pm.expect(refs.map(r => r.type), pm.response.text())"
                ".to.include('vpc_gateway');",
                "});",
            ])),
        Step(name="cleanup", method="DELETE", path="/vpc/v1/gateways/{{gwXaId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Аренда возвращена: адрес, заказанный шлюзом, уходит вместе с ним.
        Step(name="address-gone", method="GET", path="/vpc/v1/addresses/{{gwXaAddrId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="GW-NEG-EXTERNAL-ADDRESS-NOT-DELETABLE-WHILE-BOUND",
    title="Адрес, занятый шлюзом, не удаляется — сначала снимите шлюз",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/gateways",
             body={"projectId": "{{_suiteProjectId}}", "name": "gw-bound-{{runId}}",
                   "natGatewaySpec": {}, "subnetId": "{{gwAnchorSubId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.gatewayId", "gwBdId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="get-gateway", method="GET", path="/vpc/v1/gateways/{{gwBdId}}",
            test_script=[*assert_status(200),
                         *save_from_response("(pm.response.json().natGateway || {}).addressId",
                                             "gwBdAddrId")])),
        # ОТРИЦАНИЕ: пока шлюз жив, его адрес не отдать.
        Step(name="delete-address-refused", method="DELETE",
             path="/vpc/v1/addresses/{{gwBdAddrId}}",
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          "pm.test('отказ называет занятость', () => "
                          "pm.expect(String(pm.response.json().message || ''))"
                          ".to.match(/in use/));"]),
        # ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к нему: сняв шлюз, тот же путь проходит —
        # значит отказ выше был про привязку, а не про то, что удаление адреса
        # не работает вовсе.
        Step(name="delete-gateway", method="DELETE", path="/vpc/v1/gateways/{{gwBdId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="address-released", method="GET", path="/vpc/v1/addresses/{{gwBdAddrId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))
