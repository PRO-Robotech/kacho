# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для NetworkService (kacho-vpc).

Covered RPCs:
  Get, List, Create, Update, Delete, Move, ListOperations

Под-перечисления сети (`ListSubnets`/`ListSecurityGroups`/`ListRouteTables`)
СНЯТЫ С КОНТРАКТА как вторые пути к одному ответу; их место заняло сужение
списочного запроса ресурса по `network_id` — см. разбор у NET-LSUB-CRUD-EMPTY.
"""

# Helpers инжектятся gen.py через namespace модуля:
#   Step, Case, assert_status, assert_grpc_code, assert_field_violation,
#   save_from_response, assert_operation_envelope, poll_operation_until_done

CASES = []


def _assert_network_not_empty(blockers: str):
    """Отказ на удалении непустой сети ПЕРЕЧИСЛЯЕТ мешающее по видам и числам.

    Контракт: `Network <id> is not empty (subnets: 2, route tables: 1)` — виды
    с нулём в перечень не попадают, идентификаторы дочерних не печатаются
    никогда. Прежняя редакция четырёх кейсов локала `/^Network .* is not empty$/`
    с якорем конца строки, то есть требовала текста БЕЗ перечисления. Это
    утверждение пережило свой предмет: перечень заведён осознанно, потому что
    без него арендатор выяснял радиус ПЕРЕБОРОМ — снял подсети, повторил,
    получил тот же текст из-за группы правил, повторил снова.

    Утверждение здесь СИЛЬНЕЕ прежнего, а не слабее: сверяется вся строка
    целиком вместе с ожидаемым видом и числом, поэтому и потеря перечня, и
    подмена вида, и лишний вид с нулём — каждое падает по отдельности.
    """
    return [
        "pm.test('отказ перечисляет мешающее: " + blockers + "', () => "
        "pm.expect(pm.response.json().message, pm.response.text()).to.eql("
        "'Network ' + pm.environment.get('netId') + ' is not empty (" + blockers + ")'));",
    ]

# ---------------------------------------------------------------------------
# NET-CR — Create Network
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NET-CR-CRUD-OK",
    title="Create network → Operation → Network в response",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/networks",
            body={
                "projectId": "{{_suiteProjectId}}",
                "name": "net-cr-{{runId}}",
                "description": "newman CRUD-OK",
                "labels": {"suite": "newman"},
            },
            test_script=[
                *assert_status(200),
                *assert_operation_envelope(),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.networkId", "createdNetworkId"),
            ],
        ),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="get-confirms",
            method="GET",
            path="/vpc/v1/networks/{{createdNetworkId}}",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('id matches', () => pm.expect(j.id).to.eql(pm.environment.get('createdNetworkId')));",
                "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteProjectId')));",
                "pm.test('name matches', () => pm.expect(j.name).to.match(/^net-cr-/));",
            ],
        )),
        Step(
            name="cleanup-delete",
            method="DELETE",
            path="/vpc/v1/networks/{{createdNetworkId}}",
            test_script=[*assert_status(200)],
        ),
    ],
))

CASES.append(Case(
    # vpn_id из proto/БД удален — data-plane управляется отдельно. Regression-guard:
    # на публичном GET /vpc/v1/networks/{id} поля `vpnId` быть не должно. Если кто-то
    # по ошибке вернет data-plane-инфу — тест поймает.
    id="NET-GET-NO-VPNID-OK",
    title="GET /vpc/v1/networks/{id} НЕ содержит vpnId (data-plane поле удалено)",
    classes=["CRUD", "CONF"],
    priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-novpn-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "createdNetworkId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-no-vpnid", method="GET", path="/vpc/v1/networks/{{createdNetworkId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('no vpnId on public Network', () => pm.expect(j).to.not.have.property('vpnId'));"])),
        retry_until_authorized(Step(name="cleanup-delete", method="DELETE", path="/vpc/v1/networks/{{createdNetworkId}}",
             test_script=[*assert_status(200)])),
    ],
))

CASES.append(Case(
    id="NET-CR-VAL-PROJECT-REQUIRED",
    title="Create без projectId → rejected (400 InvalidArgument OR 403 authz-first, unscoped)",
    classes=["VAL", "AUTHZ"],
    priority="P0",
    steps=[
        # Unscoped create — оба исхода = «отклонено» (defense-in-depth): gateway
        # scope_extractor fail-closed 403 (no path на unscoped resource, security.md)
        # ЛИБО backend 400 (project_id required) при passthrough. См.
        # assert_unscoped_rejected (gen.py).
        Step(
            name="create-no-project",
            method="POST",
            path="/vpc/v1/networks",
            body={"name": "net-noflder-{{runId}}"},
            test_script=[
                *assert_unscoped_rejected(),
            ],
        ),
    ],
))

CASES.append(Case(
    id="NET-CR-NEG-PROJECT-NOT-FOUND",
    # Создание сети гейтится отношением editor на объекте project, взятом ИЗ ТЕЛА
    # запроса (запись каталога прав vpc.networks.create). У постороннего проекта
    # такого отношения нет ни у одного тенантского субъекта, поэтому край
    # отказывает fail-closed и запрос до сервиса не доходит — исход ровно один.
    #
    # Прежнее `oneOf([200, 403])` принимало и отказ, и его отсутствие, а ветка 200
    # клала в opId пустую строку — следующий шаг опрашивал операцию по пустому
    # идентификатору. Кейс шёл дальше по несозданному ресурсу.
    title="Create с garbage projectId → 403 (край отказывает до сервиса, anti-BOLA)",
    classes=["NEG"],
    priority="P0",
    steps=[
        Step(
            name="create-bad-project",
            method="POST",
            path="/vpc/v1/networks",
            body={"projectId": "{{garbageRmId}}", "name": "net-bf-{{runId}}"},
            test_script=[
                *assert_status(403),
                *assert_grpc_code(7, "PERMISSION_DENIED"),
            ],
        ),
    ],
))

CASES.append(Case(
    id="NET-CR-NEG-DUP-NAME",
    title="Create с duplicate name в project → sync 409 ALREADY_EXISTS",
    classes=["NEG", "CONC"],
    priority="P1",
    steps=[
        Step(
            name="create-first",
            method="POST",
            path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-dup-{{runId}}"},
            test_script=[
                *assert_status(200),
                *assert_operation_envelope(),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.networkId", "createdNetworkId"),
            ],
        ),
        poll_operation_until_done(),
        Step(
            name="create-second-same-name",
            method="POST",
            path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-dup-{{runId}}"},
            test_script=[
                *assert_status(409),
                *assert_grpc_code(6, "ALREADY_EXISTS"),
                "pm.test('mentions already exists', () => pm.expect(pm.response.json().message.toLowerCase()).to.include('already exists'));",
            ],
        ),
        Step(
            name="cleanup-first",
            method="DELETE",
            path="/vpc/v1/networks/{{createdNetworkId}}",
            test_script=[*assert_status(200)],
        ),
    ],
))

# ---------------------------------------------------------------------------
# NET-GET — Get Network
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NET-GET-NEG-NOT-FOUND",
    title="Get malformed id → 400 InvalidArgument 'invalid network id'",
    classes=["NEG", "CONF"],
    priority="P0",
    steps=[
        Step(
            name="get-garbage",
            method="GET",
            path="/vpc/v1/networks/{{garbageId}}",
            # malformed id (нет известного 3-char префикса) → 400 InvalidArgument
            # "invalid network id '<X>'". Проверка family-agnostic.
            test_script=[
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
                "pm.test('mentions invalid id', () => { const m = pm.response.json().message; pm.expect(m).to.include('invalid'); pm.expect(m).to.include('id'); });",
            ],
        ),
    ],
))

CASES.append(Case(
    id="NET-GET-NEG-EMPTY-ID",
    title="Get empty id → 404 (gRPC-gateway routing)",
    classes=["NEG"],
    priority="P2",
    steps=[
        Step(
            name="get-empty",
            method="GET",
            path="/vpc/v1/networks/",
            test_script=[*assert_absent_id_rejected()],
        ),
    ],
))

# ---------------------------------------------------------------------------
# NET-LST — List Networks
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NET-LST-CRUD-OK",
    title="List networks в project → 200 + массив",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(
            name="list",
            method="GET",
            path="/vpc/v1/networks?projectId={{_suiteProjectId}}&pageSize=10",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                # proto3-JSON: пустое repeated поле опускается. Defensive: || [].
                "pm.test('networks is array (or omitted when empty)', () => pm.expect(j.networks || []).to.be.an('array'));",
                "pm.test('nextPageToken is string (or omitted)', () => pm.expect(j.nextPageToken || '').to.be.a('string'));",
            ],
        ),
    ],
))

CASES.append(Case(
    id="NET-LST-VAL-PROJECT-REQUIRED",
    title="List без projectId → rejected (400 InvalidArgument OR 403 authz-first, no cross-project enum)",
    classes=["VAL", "AUTHZ"],
    priority="P0",
    steps=[
        # Unscoped list — gateway authz-first 403 (no path) ЛИБО backend 400. Оба =
        # «отклонено» (нет cross-project enum). См. assert_unscoped_rejected (gen.py).
        Step(
            name="list-no-project",
            method="GET",
            path="/vpc/v1/networks",
            test_script=[
                *assert_unscoped_rejected(),
            ],
        ),
    ],
))

CASES.append(Case(
    id="NET-LST-BVA-PAGESIZE-ZERO",
    title="List pageSize=0 → default applied (200, ≤50 items)",
    classes=["BVA"],
    priority="P2",
    steps=[
        Step(
            name="list-pagesize-0",
            method="GET",
            path="/vpc/v1/networks?projectId={{_suiteProjectId}}&pageSize=0",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('default pagesize applied', () => pm.expect((j.networks || []).length).to.be.at.most(50));",
            ],
        ),
    ],
))

CASES.append(Case(
    id="NET-LST-BVA-PAGESIZE-OVER-MAX",
    title="List pageSize=10000 → InvalidArgument",
    classes=["BVA", "VAL"],
    priority="P2",
    steps=[
        Step(
            name="list-pagesize-huge",
            method="GET",
            path="/vpc/v1/networks?projectId={{_suiteProjectId}}&pageSize=10000",
            test_script=[
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
            ],
        ),
    ],
))

CASES.append(Case(
    id="NET-LST-PAGE-TOKEN-GARBAGE",
    title="List с garbage page_token → InvalidArgument",
    classes=["PAGE", "VAL"],
    priority="P1",
    steps=[
        Step(
            name="list-bad-token",
            method="GET",
            path="/vpc/v1/networks?projectId={{_suiteProjectId}}&pageSize=10&pageToken=not-a-real-token",
            test_script=[
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
            ],
        ),
    ],
))

# ---------------------------------------------------------------------------
# NET-UPD — Update Network
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NET-UPD-CRUD-DESCRIPTION",
    title="Update description через mask → success + новое значение видно",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-upd-{{runId}}"},
            test_script=[
                *assert_status(200),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.networkId", "netId"),
            ],
        ),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="update-desc",
            method="PATCH",
            path="/vpc/v1/networks/{{netId}}",
            body={"updateMask": "description", "description": "patched-desc"},
            test_script=[
                *assert_status(200),
                *save_from_response("j.id", "opId"),
            ],
        )),
        poll_operation_until_done(),
        # read-your-writes: verify GET of the caller's OWN fresh resource can briefly
        # 404 while the owner-tuple materializes under load (opgate removed). Bounded-retry.
        retry_until_authorized(Step(
            name="verify",
            method="GET",
            path="/vpc/v1/networks/{{netId}}",
            test_script=[
                *assert_status(200),
                "pm.test('description updated', () => pm.expect(pm.response.json().description).to.eql('patched-desc'));",
            ],
        )),
        retry_until_authorized(Step(
            name="cleanup",
            method="DELETE",
            path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200)],
        )),
    ],
))

# NB: NET-UPD-STATE-IMMUTABLE-PROJECT генерится helper'ом state_immutable_project("NET", …)
# ниже по файлу — explicit-дубль убран (validate-cases.py: hard-fail на дубль case-id).

CASES.append(Case(
    id="NET-UPD-NEG-NF-INVALID-PREFIX",
    title="Update malformed id → 400 InvalidArgument 'invalid network id'",
    classes=["NEG", "STATE"],
    priority="P1",
    steps=[
        Step(
            name="patch-invalid-prefix",
            method="PATCH",
            path="/vpc/v1/networks/{{garbageId}}",
            body={"updateMask": "description", "description": "x"},
            test_script=[
                # malformed id (нет известного 3-char префикса) → 400 InvalidArgument
                # "invalid network id '<X>'". Проверка family-agnostic.
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
                "pm.test('mentions invalid id', () => { const m = pm.response.json().message; pm.expect(m).to.include('invalid'); pm.expect(m).to.include('id'); });",
            ],
        ),
    ],
))

CASES.append(Case(
    id="NET-UPD-AUTHZ-NF-SYNC",
    title="Update несуществующего id (валидный префикс) → sync 404 от AuthZ-Get",
    classes=["NEG", "AUTHZ"],
    priority="P1",
    steps=[
        Step(
            name="patch-nonexistent",
            method="PATCH",
            path="/vpc/v1/networks/{{garbageVpcId}}",
            body={"updateMask": "description", "description": "x"},
            test_script=[*assert_absent_id_rejected()],
        ),
    ],
))

# ---------------------------------------------------------------------------
# NET-DEL — Delete Network
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NET-DEL-NEG-NF-INVALID-PREFIX",
    title="Delete malformed id → 400 InvalidArgument 'invalid network id'",
    classes=["NEG", "STATE"],
    priority="P1",
    steps=[
        Step(
            name="delete-invalid-prefix",
            method="DELETE",
            path="/vpc/v1/networks/{{garbageId}}",
            test_script=[
                # malformed id (нет известного 3-char префикса) → 400 InvalidArgument
                # "invalid network id '<X>'". Проверка family-agnostic.
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
                "pm.test('mentions invalid id', () => { const m = pm.response.json().message; pm.expect(m).to.include('invalid'); pm.expect(m).to.include('id'); });",
            ],
        ),
    ],
))

CASES.append(Case(
    id="NET-DEL-AUTHZ-NF-SYNC",
    title="Delete несуществующего id (валидный префикс) → sync 404 от AuthZ-Get",
    classes=["NEG", "AUTHZ"],
    priority="P1",
    steps=[
        Step(
            name="delete-nonexistent",
            method="DELETE",
            path="/vpc/v1/networks/{{garbageVpcId}}",
            test_script=[*assert_absent_id_rejected()],
        ),
    ],
))

# ---------------------------------------------------------------------------
# NET-MV — Move Network
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# NET-LSUB / NET-LSG / NET-LRT — child lists
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Три кейса ниже спрашивали под-перечисления сети
# (`/vpc/v1/networks/{id}/{subnets,security_groups,route_tables}`). Эти три метода
# СНЯТЫ С КОНТРАКТА как вторые пути к одному ответу с ДРУГИМ объектом проверки
# прав; замена — сужение списочного запроса ресурса по `network_id`, и она была
# заведена ДО снятия (белый список фильтра). Снятого маршрута край не знает,
# поэтому запрос по нему не доходит до сервиса вовсе: каталог прав не резолвит
# метод и край отказывает fail-closed — `403` с пустым `action` и
# `type: authz.catalog`. То есть шаг падал НЕ на предмете кейса, а на
# несуществующем адресе, и «has at least 1 SG» читалось как «группы по умолчанию
# нет», хотя она есть (проверено на живом стенде: `defaultSecurityGroupId` в
# ответе Create, `defaultForNetwork: true` в списке по фильтру).
#
# Что меняется в утверждении, кроме адреса: замена — список С ПООБЪЕКТНОЙ
# фильтрацией прав, поэтому свежесозданный системный ребёнок появляется в
# странице в окне материализации owner-tuple. Ожидание берётся `retry_until_present`
# по КОНКРЕТНОМУ id (его называет сам ресурс: `defaultSecurityGroupId` /
# `defaultRouteTableId`), а не «пока массив непуст» — иначе кейс сошёлся бы на
# чужой строке.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NET-LSUB-CRUD-EMPTY",
    title="Список подсетей, суженный по network_id, для пустой сети → 200 + пустой массив",
    classes=["CRUD"],
    priority="P2",
    steps=[
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-lsub-{{runId}}"},
            test_script=[
                *assert_status(200),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.networkId", "netId"),
            ],
        ),
        poll_operation_until_done(),
        Step(
            name="list-subnets",
            method="GET",
            path="/vpc/v1/subnets?projectId={{_suiteProjectId}}&pageSize=1000"
                 "&filter=network_id%3D%22{{netId}}%22",
            test_script=[
                *assert_status(200),
                # Пусто — это и есть утверждение: подсетей сети никто не заводил.
                # Ждать здесь нечего (ожидание нужно на ПОЯВЛЕНИЕ своего свежего
                # id, а не на его отсутствие), поэтому retry тут был бы маскировкой.
                "pm.test('пустой список подсетей этой сети', () => "
                "pm.expect(pm.response.json().subnets || [], pm.response.text()).to.eql([]));",
            ],
        ),
        Step(
            name="cleanup",
            method="DELETE",
            path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200)],
        ),
    ],
))

CASES.append(Case(
    id="NET-LSG-CRUD-DEFAULT-SG",
    title="Список групп правил, суженный по network_id → группа по умолчанию присутствует и помечена",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-lsg-{{runId}}"},
            test_script=[
                *assert_status(200),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.networkId", "netId"),
            ],
        ),
        poll_operation_until_done(),
        # Сеть САМА называет свою группу по умолчанию — ждать в списке будем
        # именно её id, а не «какую-нибудь строку»: иначе кейс сошёлся бы на
        # чужой группе того же проекта и остался бы зелёным без своей.
        retry_until_authorized(Step(
            name="net-names-default-sg", method="GET", path="/vpc/v1/networks/{{netId}}",
            test_script=[
                *assert_status(200),
                "pm.test('сеть называет свою группу по умолчанию', () => "
                "pm.expect(pm.response.json().defaultSecurityGroupId, pm.response.text())"
                ".to.be.a('string').and.not.empty);",
                *save_from_response("j.defaultSecurityGroupId", "defSgId"),
            ])),
        retry_until_present(
            Step(
                name="list-sgs",
                method="GET",
                path="/vpc/v1/securityGroups?projectId={{_suiteProjectId}}&pageSize=1000"
                     "&filter=network_id%3D%22{{netId}}%22",
                test_script=[
                    *assert_status(200),
                    "const sgs = pm.response.json().securityGroups || [];",
                    "const def = sgs.filter(s => s.id === pm.environment.get('defSgId'));",
                    "pm.test('группа по умолчанию, названная самой сетью, в списке', () => "
                    "pm.expect(def.length, pm.response.text()).to.eql(1));",
                    "pm.test('она же помечена как группа по умолчанию', () => "
                    "pm.expect(def.length && def[0].defaultForNetwork).to.eql(true));",
                ],
            ),
            "defSgId",
        ),
        Step(
            name="cleanup",
            method="DELETE",
            path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200)],
        ),
    ],
))

CASES.append(Case(
    id="NET-LRT-CRUD-EMPTY",
    title="Список таблиц маршрутизации, суженный по network_id → ровно системная таблица сети, арендаторских нет",
    classes=["CRUD"],
    priority="P2",
    steps=[
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-lrt-{{runId}}"},
            test_script=[
                *assert_status(200),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.networkId", "netId"),
            ],
        ),
        poll_operation_until_done(),
        # Прежнее утверждение звучало «массив» — это не утверждение вовсе: оно
        # выполняется и на пустом, и на любом другом ответе, поэтому пережило бы
        # любую регрессию списка. Сеть провижнит СВОЮ таблицу маршрутизации на
        # Create, значит «пусто» неверно, а верно «ровно она и ничего больше».
        retry_until_authorized(Step(
            name="net-names-default-rt", method="GET", path="/vpc/v1/networks/{{netId}}",
            test_script=[
                *assert_status(200),
                "pm.test('сеть называет свою таблицу маршрутизации', () => "
                "pm.expect(pm.response.json().defaultRouteTableId, pm.response.text())"
                ".to.be.a('string').and.not.empty);",
                *save_from_response("j.defaultRouteTableId", "defRtId"),
            ])),
        retry_until_present(
            Step(
                name="list-rt",
                method="GET",
                path="/vpc/v1/routeTables?projectId={{_suiteProjectId}}&pageSize=1000"
                     "&filter=network_id%3D%22{{netId}}%22",
                test_script=[
                    *assert_status(200),
                    "const rts = pm.response.json().routeTables || [];",
                    "pm.test('ровно одна таблица — системная таблица этой сети', () => {",
                    "  pm.expect(rts.map(r => r.id), pm.response.text())"
                    ".to.eql([pm.environment.get('defRtId')]);",
                    "});",
                ],
            ),
            "defRtId",
        ),
        Step(
            name="cleanup",
            method="DELETE",
            path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200)],
        ),
    ],
))

CASES.append(Case(
    id="NET-LOP-CRUD-OK",
    title="ListOperations возвращает operations для свежесозданной network",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-lop-{{runId}}"},
            test_script=[
                *assert_status(200),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.id", "createOpId"),
                *save_from_response("j.metadata && j.metadata.networkId", "netId"),
            ],
        ),
        poll_operation_until_done(),
        Step(
            name="list-ops",
            method="GET",
            path="/vpc/v1/networks/{{netId}}/operations",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "const ops = j.operations || [];",
                "pm.test('at least 1 op', () => pm.expect(ops.length).to.be.at.least(1));",
                "pm.test('contains create op', () => pm.expect(ops.some(o => o.id === pm.environment.get('createOpId'))).to.eql(true));",
            ],
        ),
        Step(
            name="cleanup",
            method="DELETE",
            path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200)],
        ),
    ],
))

# Расширение: CONF + STATE-unknown-mask (BVA pagination уже есть)
CASES.append(conf_not_found_text("NET", "/vpc/v1/networks", "Network"))
CASES.append(state_update_unknown_mask("NET", "/vpc/v1/networks"))

# Дополнение: STATE immutable project + VAL move-no-dest + BVA pagesize=1
CASES.append(state_immutable_project("NET", "/vpc/v1/networks"))
CASES.append(list_pagesize_1_bva("NET", "/vpc/v1/networks"))

CASES.append(Case(
    id="NET-CR-CONF-PROJECT-NF-TEXT",
    # Тот же отказ, но проверяется ДРУГОЕ его свойство: он не должен сообщать,
    # существует ли названный проект. Прежний заголовок обещал сверку текста
    # «Project ... not found» из операции — а операции в этом сценарии не
    # возникает вовсе, потому что запрос отвергается на краю. Текст, который кейс
    # собирался сверять, недостижим ему по построению.
    title="Create network в garbage project → отказ не раскрывает, существует ли проект",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{garbageRmId}}", "name": "net-confnf-{{runId}}"},
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
                 "const _m = ((pm.response.json() || {}).message || '').toLowerCase();",
                 "pm.test('refusal says permission denied', () => pm.expect(_m).to.contain('permission denied'));",
                 "pm.test('refusal does not disclose project existence', () => pm.expect(_m).to.not.contain('not found'));",
             ]),
    ],
))

# NEG: обращение к дочерним сущностям несуществующей сети.
#
# Операции сети по-прежнему адресуются вложенным путём, и у него сохраняется
# прежний предмет: путь несёт объект `vpc_network`, отношения на несуществующей
# сети нет ни у кого → край отказывает до сервиса.
CASES.append(Case(
    id="NET-LOP-NEG-PARENT-NF",
    title="List operations в несуществующей network → отказ края до сервиса",
    classes=["NEG"], priority="P1",
    steps=[
        Step(name="list-child", method="GET",
             path="/vpc/v1/networks/{{garbageVpcId}}/operations",
             # Вложенный список гейтится отношением v_list на объекте
             # vpc_network из пути. У несуществующей сети такого отношения нет
             # ни у кого, край отказывает до сервиса, и подмены отказа на 404
             # тут не происходит: сокрытие существования включено только для
             # одиночного чтения `/Get` с отношением v_get. Исход один.
             #
             # Прежний список принимал и 200 — то есть заголовок обещал отказ,
             # а утверждение проходило и при выдаче содержимого.
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
    ],
))

# Три соседних кейса той же семьи (`.../subnets`, `.../security_groups`,
# `.../route_tables`) утверждали ровно то же про снятые ныне под-перечисления.
# Их нельзя было оставить как есть: снятого маршрута край не знает, поэтому
# `403` он отдаёт на ЛЮБОЙ идентификатор сети — и на несуществующий, и на
# собственный живой. Утверждение «у несуществующего родителя отказ» выполнялось
# бы тождественно, то есть перестало быть утверждением: предмет (гейт по объекту
# из пути) исчез вместе с методом, а форма проверки осталась бы.
#
# Предмет заменён на предмет ЗАМЕНЫ — сужения списочного запроса по `network_id`.
# У неё своё, проверяемое свойство: неизвестная сеть в фильтре не ошибка и не
# утечка, а пустая страница СВОЕГО проекта. Кейс краснеет на любом другом исходе —
# на отказе (значит фильтр стал вторым гейтом), на непустой странице (значит
# сужение не сузило) и на ошибке разбора.
for _kind, _res in [("LSUB", "subnets"), ("LSG", "securityGroups"), ("LRT", "routeTables")]:
    CASES.append(Case(
        id=f"NET-{_kind}-NEG-PARENT-NF",
        title=f"Список {_res}, суженный по несуществующей сети → 200 + пустая страница (не отказ, не утечка)",
        classes=["NEG"], priority="P1",
        steps=[
            Step(name="list-by-absent-network", method="GET",
                 path=f"/vpc/v1/{_res}?projectId={{{{_suiteProjectId}}}}&pageSize=1000"
                      "&filter=network_id%3D%22{{garbageVpcId}}%22",
                 test_script=[
                     *assert_status(200),
                     f"pm.test('пустая страница, а не отказ и не чужие строки', () => "
                     f"pm.expect(pm.response.json().{_res} || [], pm.response.text()).to.eql([]));",
                 ]),
        ],
    ))

CASES.append(Case(
    id="NET-DEL-CRUD-OK",
    title="Network Delete (CRUD-OK): отдельная positive-проверка happy delete",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-delok-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="delete-happy", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="get-after-delete", method="GET", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

CASES.append(Case(
    id="NET-DEL-CONF-NF-TEXT",
    title="Delete несуществующего Network → точный текст 'Network ... not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="del-nx", method="DELETE", path="/vpc/v1/networks/{{garbageVpcId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="NET-UPD-CONF-NF-TEXT",
    title="Update несуществующего Network → точный текст 'Network ... not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="upd-nx", method="PATCH", path="/vpc/v1/networks/{{garbageVpcId}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

# Exhaustive ECP/BVA расширение
CASES.extend(ecp_name_block("NET", "/vpc/v1/networks", {}))
CASES.extend(ecp_description_block("NET", "/vpc/v1/networks", {}))
CASES.extend(ecp_labels_block("NET", "/vpc/v1/networks", {}))
CASES.extend(updatemask_decision_table("NET", "/vpc/v1/networks"))
CASES.extend(filter_syntax_block("NET", "/vpc/v1/networks"))
CASES.append(pagination_roundtrip("NET", "/vpc/v1/networks"))
CASES.append(idempotency_block("NET", "/vpc/v1/networks", "net-idm-{{runId}}", {}))

# Update happy / perf-baseline / conformance-text / authz-caller-headers
CASES.extend(update_happy_per_field("NET", "/vpc/v1/networks", "/vpc/v1/networks", {"projectId": "{{_suiteProjectId}}"}))
CASES.extend(perf_baseline_block("NET", "/vpc/v1/networks"))
CASES.extend(verbatim_text_pack("NET", "Network", "/vpc/v1/networks"))
CASES.extend(authz_caller_headers_block("NET", "/vpc/v1/networks"))

# cross-project + multi-field + filter-match + invalid types + methods + malformed
CASES.append(update_happy_multi_field("NET", "/vpc/v1/networks", "/vpc/v1/networks", {"projectId": "{{_suiteProjectId}}"}))
CASES.append(cross_project_resource_block("NET", "/vpc/v1/networks", {}))
CASES.append(list_filter_match_block("NET", "/vpc/v1/networks", {"projectId": "{{_suiteProjectId}}"}))
CASES.extend(neg_invalid_types_block("NET", "/vpc/v1/networks", {"projectId": "{{_suiteProjectId}}"}))
CASES.extend(http_method_not_allowed_block("NET", "/vpc/v1/networks"))
CASES.extend(malformed_body_block("NET", "/vpc/v1/networks"))

# AlreadyExists dup-name + update-mask partial + perf-baseline-get + list-total-size + headers/content-type
CASES.append(alreadyexists_dup_name_for("NET", "/vpc/v1/networks", {"projectId": "{{_suiteProjectId}}"}))
CASES.extend(update_mask_partial_block("NET", "/vpc/v1/networks", "/vpc/v1/networks", {"projectId": "{{_suiteProjectId}}"}))
CASES.append(perf_baseline_get_block("NET", "/vpc/v1/networks", {"projectId": "{{_suiteProjectId}}"}))
CASES.extend(list_total_size_check_block("NET", "/vpc/v1/networks"))
CASES.extend(headers_content_type_block("NET", "/vpc/v1/networks", {"projectId": "{{_suiteProjectId}}"}))

# NET-CR-VAL-EXTRA-FIELDS — retired 2026-07-28.
#
# The case sent `unknownField` / `anotherUnknown` on Create and asserted
# `oneOf([200, 400])`. Whatever the edge did with a key CreateNetworkRequest does not
# declare — accept it, drop it, refuse the whole request — the case stayed green, so
# it separated nothing; its own title said as much ("silent ignore (200) или 400").
# The class it gestured at — fixtures shipping keys no message declares — is decided
# statically at commit time by the restmux gate, which names the request, the RPC
# that serves it and the offending key instead of tolerating either answer. The
# neighbouring malformed-body case (NET-CR-VAL-MALFORMED-JSON, generated by
# malformed_body_block) still covers the different question of a body the edge cannot
# parse at all.

# Network-specific edge cases
CASES.append(Case(
    id="NET-LST-FILTER-MULTI-CONDITIONS",
    title="List с filter из несколько условий — multi-condition filter",
    classes=["FILTER"], priority="P3",
    steps=[Step(name="lst-multi", method="GET",
                path="/vpc/v1/networks?projectId={{_suiteProjectId}}&filter=name%3D%22x%22%20AND%20name%3D%22y%22",
                test_script=["pm.test('200 or 400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])],
))

# List/Get edge cases
CASES.append(Case(
    id="NET-LST-PAGE-NEGATIVE-SIZE",
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
                path="/vpc/v1/networks?projectId={{_suiteProjectId}}&pageSize=-1",
                test_script=[
                    *assert_status(400),
                    *assert_grpc_code(3, "INVALID_ARGUMENT"),
                    "pm.test('names the offending field', () => pm.expect(JSON.stringify(pm.response.json())).to.contain('page_size'));",
                ])],
))

CASES.append(Case(
    id="NET-LST-FILTER-SPECIAL-CHARS",
    title="List с filter содержащим спец-символы → 400 или 200",
    classes=["FILTER", "VAL"], priority="P3",
    steps=[Step(name="lst-fsc", method="GET",
                path="/vpc/v1/networks?projectId={{_suiteProjectId}}&filter=name%3D%22%21%40%23%24%25%22",
                test_script=["pm.test('handled', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])],
))

CASES.append(Case(
    id="NET-LST-PAGESIZE-EXACTLY-1000",
    title="List с pageSize=1000 (boundary max) → 200",
    classes=["BVA"], priority="P2",
    steps=[Step(name="lst-max", method="GET",
                path="/vpc/v1/networks?projectId={{_suiteProjectId}}&pageSize=1000",
                test_script=[*assert_status(200)])],
))

CASES.append(Case(
    id="NET-LST-PAGESIZE-1001",
    title="List с pageSize=1001 (over max) → 400",
    classes=["BVA", "VAL"], priority="P1",
    steps=[Step(name="lst-1001", method="GET",
                path="/vpc/v1/networks?projectId={{_suiteProjectId}}&pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="NET-LST-DOUBLE-PROJECT-PARAM",
    title="List с дубликатом projectId param → 200 (last wins) или 400",
    classes=["VAL"], priority="P3",
    steps=[Step(name="lst-dup", method="GET",
                path="/vpc/v1/networks?projectId={{_suiteProjectId}}&projectId={{_suiteProjectCrossId}}&pageSize=10",
                test_script=["pm.test('200 or 400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])],
))

CASES.append(Case(
    id="NET-GET-TRAILING-SLASH",
    title="Get с trailing slash → 404",
    classes=["VAL"], priority="P3",
    steps=[Step(name="get-trail", method="GET", path="/vpc/v1/networks/{{garbageVpcId}}/",
                test_script=["pm.test('non-2xx', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));"])],
))

# === Delete с зависимыми ресурсами (FK RESTRICT) ===

CASES.append(Case(
    id="NET-DEL-NEG-HAS-SUBNETS",
    title="Delete Network c Subnet → FailedPrecondition (FK RESTRICT)",
    classes=["NEG", "CONF", "STATE"], priority="P0",
    steps=[
        Step(name="cr-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-hasub-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        Step(name="cr-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-hasub-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.250.1.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-net-blocked", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 *_assert_network_not_empty("subnets: 1"),
             ])),
        # cleanup в обратном порядке
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NET-DEL-NEG-HAS-ROUTE-TABLE",
    title="Delete Network c RouteTable → FailedPrecondition",
    classes=["NEG", "CONF", "STATE"], priority="P0",
    steps=[
        Step(name="cr-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-hart-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        Step(name="cr-rt", method="POST", path="/vpc/v1/routeTables",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "rt-hart-{{runId}}", "staticRoutes": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.routeTableId", "rtId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-net-blocked", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 *_assert_network_not_empty("route tables: 1"),
             ])),
        Step(name="cleanup-rt", method="DELETE", path="/vpc/v1/routeTables/{{rtId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NET-DEL-NEG-HAS-NONDEFAULT-SG",
    title="Delete Network с НЕ-default SG → FailedPrecondition (RESTRICT FK)",
    classes=["NEG", "CONF", "STATE"], priority="P0",
    steps=[
        Step(name="cr-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-hasg-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        # Создаем дополнительный (non-default) SG
        Step(name="cr-sg", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sg-hasg-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-net-blocked", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 *_assert_network_not_empty("security groups: 1"),
             ])),
        Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NET-DEL-CRUD-ONLY-DEFAULT-SG",
    title="Delete Network у которой есть только default-SG → OK (auto-cleanup default)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        # Создаем network — default SG создается inline в doCreate автоматически
        Step(name="cr-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-defsg-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        # Проверка что default SG действительно создался. Спрашивается список,
        # суженный по `network_id`: под-перечисление сети снято с контракта
        # (см. разбор у NET-LSUB-CRUD-EMPTY), и запрос по снятому адресу не
        # доходил до сервиса — «ни одной группы по умолчанию» означало «край не
        # знает такого маршрута», а не отсутствие группы.
        retry_until_authorized(Step(
            name="net-names-default-sg", method="GET", path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200),
                         *save_from_response("j.defaultSecurityGroupId", "defSgId")])),
        retry_until_present(
            Step(name="check-default-sg", method="GET",
                 path="/vpc/v1/securityGroups?projectId={{_suiteProjectId}}&pageSize=1000"
                      "&filter=network_id%3D%22{{netId}}%22",
                 test_script=[*assert_status(200),
                              "const sgs = pm.response.json().securityGroups || [];",
                              "pm.test('exactly 1 default SG present', () => "
                              "pm.expect(sgs.filter(s => s.defaultForNetwork === true).map(s => s.id), "
                              "pm.response.text()).to.eql([pm.environment.get('defSgId')]));"]),
            "defSgId",
        ),
        # Delete network — должен пройти (default SG автоматически чистится service-кодом)
        retry_until_authorized(Step(name="del-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="assert-success", method="GET", path="/operations/{{opId}}",
             test_script=[
                 "const j = pm.response.json();",
                 "pm.test('done', () => pm.expect(j.done).to.eql(true));",
                 "pm.test('no error — delete with only default-SG succeeds', () => pm.expect(j.error).to.be.undefined);",
             ]),
        Step(name="get-after-del", method="GET", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

CASES.append(Case(
    # После Network.Delete ее default SG обязан исчезнуть — explicit
    # GET /securityGroups/{defSgId} должен дать 404. Здесь проверяется именно
    # life-cycle default SG (NET-DEL-CRUD-ONLY-DEFAULT-SG проверяет лишь, что
    # Network.Delete возвращает 200).
    id="NET-DEL-CRUD-DEFAULT-SG-REMOVED",
    title="Delete Network → ее default SG тоже удалена (explicit GET /securityGroups → 404)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        Step(name="cr-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-defsgrm-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-default-sg-id", method="GET", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('defaultSecurityGroupId populated', () => pm.expect(j.defaultSecurityGroupId, JSON.stringify(j)).to.be.a('string').and.not.empty);",
                          *save_from_response("j.defaultSecurityGroupId", "defSgId")])),
        # Read-your-writes: default SG создаётся сервером вместе с Network; её
        # owner-tuple материализуется eventually-consistent → первый GET свежей
        # default SG без ретрая ловит устойчивый 403/404.
        retry_until_authorized(Step(name="get-default-sg-alive", method="GET", path="/vpc/v1/securityGroups/{{defSgId}}",
             test_script=[*assert_status(200),
                          "pm.test('default SG exists before network.delete', () => pm.expect(pm.response.json().defaultForNetwork).to.eql(true));"])),
        retry_until_authorized(Step(name="del-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="assert-net-deleted", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('net delete op done, no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        # Главная проверка: default SG должна быть удалена вместе с Network.
        Step(name="get-default-sg-gone", method="GET", path="/vpc/v1/securityGroups/{{defSgId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('NOT_FOUND text mentions SG', () => pm.expect(pm.response.json().message || '').to.match(/[Ss]ecurity\\s*[Gg]roup/));"]),
    ],
))

CASES.append(Case(
    # Subnet с NIC блокирует Subnet.Delete (FK RESTRICT) — и, как следствие,
    # Network.Delete (FK RESTRICT subnet→network). NIC-in-subnet вариант контракта
    # network-not-empty.
    id="NET-DEL-NEG-HAS-SUBNET-WITH-NIC",
    title="Delete Network c Subnet, в которой NIC → FailedPrecondition (not empty); cleanup снизу вверх",
    classes=["NEG", "STATE"], priority="P0",
    steps=[
        Step(name="cr-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-subnic-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        Step(name="cr-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-subnic-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.248.3.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="cr-nic", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}", "name": "nic-subnic-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-net-blocked", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 *_assert_network_not_empty("subnets: 1"),
             ])),
        # cleanup снизу вверх: NIC → Subnet → Network
        Step(name="cleanup-nic", method="DELETE", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    # ListOperations не делает repo.Get precondition — history переживает удаление
    # ресурса. Проверяет network /operations после delete.
    id="NET-LISTOPS-AFTER-DELETE-OK",
    title="ListOperations сети после ее удаления → 200, непустой список (Create + Delete)",
    classes=["STATE", "CRUD"], priority="P1",
    steps=[
        Step(name="cr-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "net-listops-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        Step(name="listops-before", method="GET", path="/vpc/v1/networks/{{netId}}/operations",
             test_script=[*assert_status(200), "const j = pm.response.json();",
                          "pm.test('has Create op', () => pm.expect((j.operations||[]).length).to.be.at.least(1));"]),
        retry_until_authorized(Step(name="del-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="listops-after-delete", method="GET", path="/vpc/v1/networks/{{netId}}/operations",
             test_script=[
                 *assert_status(200), "const j = pm.response.json();",
                 "pm.test('history survives delete (Create + Delete)', () => pm.expect((j.operations||[]).length).to.be.at.least(2));",
             ]),
    ],
))

# === Required-field matrix + Immutable matrix для Network ===
# `name` из списка снят: контракт vpc допускает ПУСТОЕ имя
# (`services/vpc/internal/domain/types.go`, RcNameVPC — «empty allowed», залочено
# `types_test.go` кейсом {"empty allowed", "", false}), а сгенерированный сосед
# `NET-CR-VAL-NAME-NULL` прямо утверждает `name=null → 200` (protojson: null = поле
# не задано, то есть ровно «поле убрали»). Две записи одной суиты обещали
# взаимоисключающее, и «убери name → отказ» зеленел только потому, что принимал
# успех. Снято как претензия без основания — тем же решением, каким снят
# `v4CidrBlocks` у подсети.
CASES.extend(required_fields_matrix("NET", "/vpc/v1/networks",
    {"projectId": "{{_suiteProjectId}}", "name": "net-req-{{runId}}"},
    ["projectId"]))
CASES.extend(immutable_fields_matrix("NET", "/vpc/v1/networks",
    ["project_id"]))

# security probes + lifecycle conformance
CASES.extend(security_injection_block("NET", "/vpc/v1/networks", "/vpc/v1/networks",
    {"projectId": "{{_suiteProjectId}}"}))
CASES.append(conformance_lifecycle_pack("NET", "/vpc/v1/networks",
    {"projectId": "{{_suiteProjectId}}"}))

# ---------------------------------------------------------------------------
# NET-APPLY — состояние применения намерения в публичном контракте (kacho#296)
# ---------------------------------------------------------------------------

CASES.append(Case(
    # index: APPLY-05 · APPLY-21 — состояние применения на чтении ресурса
    id="NET-APPLY-STATE-CRUD-OK",
    title="Свежая сеть: applyState виден на Get и отсутствует в ответе операции",
    classes=["CRUD", "CONF"],
    priority="P1",
    steps=[
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-apst-{{runId}}"},
            test_script=[
                *assert_status(200),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.networkId", "netId"),
            ],
        ),
        poll_operation_until_done(),
        # APPLY-21: ответ операции состояния применения НЕ несёт, и это решение,
        # а не поломка заполнителя. В момент завершения операции исполнитель
        # заведомо ещё не отчитался — поле несло бы одно и то же значение всегда
        # и читалось бы как «операция готова, но что-то не так». Положительный
        # контроль стоит следующим шагом: тот же ресурс на Get поле несёт.
        Step(
            name="op-response-carries-no-apply-state",
            method="GET",
            # Маршрут операции объявлен БЕЗ префикса сервиса
            # (`get: "/operations/{operation_id}"` в контракте), поэтому здесь
            # его быть не должно: с префиксом край отвечает 404, и падают все
            # три утверждения шага разом — включая те, что о поле не говорят.
            path="/operations/{{opId}}",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('операция завершена без ошибки', () => {",
                "  pm.expect(j.done, pm.response.text()).to.eql(true);",
                "  pm.expect(j.error, 'операция завершилась отказом').to.be.oneOf([undefined, null]);",
                "});",
                "pm.test('ресурс в ответе операции состояния применения не несёт', () => {",
                "  pm.expect(j.response, 'в ответе операции нет ресурса').to.be.an('object');",
                "  pm.expect(j.response.applyState, JSON.stringify(j.response))"
                ".to.be.oneOf([undefined, null]);",
                "});",
            ],
        ),
        # APPLY-05: положительный контроль ко всему остальному — поле доезжает.
        retry_until_authorized(Step(
            name="get-apply-state",
            method="GET",
            path="/vpc/v1/networks/{{netId}}",
            test_script=[
                *assert_status(200),
                *assert_apply_state_in_flight("NET"),
            ])),
        Step(
            name="cleanup",
            method="DELETE",
            path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200)],
        ),
    ],
))

CASES.append(Case(
    # index: APPLY-12 — список несёт то же состояние, что и одиночное чтение
    id="NET-APPLY-STATE-LIST-PARITY-OK",
    title="Список сетей несёт то же состояние применения, что и Get каждой",
    classes=["CRUD", "CONF"],
    priority="P1",
    steps=[
        Step(
            name="create-a", method="POST", path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-apst-la-{{runId}}"},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.networkId", "netApstA")]),
        poll_operation_until_done(),
        Step(
            name="create-b", method="POST", path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-apst-lb-{{runId}}"},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.networkId", "netApstB")]),
        poll_operation_until_done(),
        Step(
            name="create-c", method="POST", path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": "net-apst-lc-{{runId}}"},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.networkId", "netApstC")]),
        poll_operation_until_done(),
        # Одиночное чтение одной из трёх — эталон, с которым сверяется страница.
        retry_until_authorized(Step(
            name="get-one", method="GET", path="/vpc/v1/networks/{{netApstA}}",
            test_script=[
                *assert_status(200),
                *assert_apply_state_present("NET"),
                "pm.environment.set('netApstAState', JSON.stringify(pm.response.json().applyState));",
            ])),
        # Класс, который закрывает этот кейс: «одно чтение поле заполняет, другое
        # нет». Список — дешёвый и поэтому частый путь; клиент, обновляющий из
        # него своё состояние, прочёл бы пустоту как утрату.
        retry_until_present(Step(
            name="list-parity", method="GET",
            path="/vpc/v1/networks?projectId={{_suiteProjectId}}&pageSize=1000",
            test_script=[
                *assert_status(200),
                "const nets = pm.response.json().networks || [];",
                "const want = ['netApstA', 'netApstB', 'netApstC'].map(v => pm.environment.get(v));",
                "const mine = nets.filter(n => want.indexOf(n.id) >= 0);",
                "pm.test('все три сети на странице', () => "
                "pm.expect(mine.length, pm.response.text()).to.eql(3));",
                "pm.test('каждый элемент страницы несёт состояние применения', () => {",
                "  mine.forEach(n => {",
                "    pm.expect(n.applyState, 'applyState у ' + n.id).to.be.an('object');",
                "    pm.expect(n.applyState.applied, 'applied у ' + n.id).to.be.a('boolean');",
                "    pm.expect(n.applyState.reason, 'reason у ' + n.id).to.be.a('string');",
                "  });",
                "});",
                "pm.test('состояние в списке совпадает с одиночным чтением', () => {",
                "  const one = mine.filter(n => n.id === pm.environment.get('netApstA'))[0];",
                "  pm.expect(JSON.stringify(one.applyState))"
                ".to.eql(pm.environment.get('netApstAState'));",
                "});",
            ]), ["netApstA", "netApstB", "netApstC"]),
        Step(name="cleanup-a", method="DELETE", path="/vpc/v1/networks/{{netApstA}}",
             test_script=[*assert_status(200)]),
        Step(name="cleanup-b", method="DELETE", path="/vpc/v1/networks/{{netApstB}}",
             test_script=[*assert_status(200)]),
        Step(name="cleanup-c", method="DELETE", path="/vpc/v1/networks/{{netApstC}}",
             test_script=[*assert_status(200)]),
    ],
))
