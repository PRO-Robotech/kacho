# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""OperationService cases (OP-*) — kacho-nlb opsproxy through api-gateway."""

CASES = []


# -- OP-GET-CRUD-IN-FLIGHT — happy path: Get returns in-flight op then polls to done
CASES.append(Case(
    id="OP-GET-CRUD-IN-FLIGHT",
    title="Get just-created operation eventually polls to done=true",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="trigger-create", method="POST", path="/nlb/v1/networkLoadBalancers",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "opget-inflight-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
                          "pm.test('done initially false or true', () => {"
                          "  const j = pm.response.json();"
                          "  pm.expect(typeof j.done).to.eql('boolean');"
                          "});"]),
        Step(name="get-op-immediate", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('has metadata', () => pm.expect(j.metadata).to.be.an('object'));",
                          "pm.test('id matches', () => pm.expect(j.id).to.eql(pm.environment.get('opId')));"]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path="/nlb/v1/networkLoadBalancers/{{nlbId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))


# -- OP-GET-CRUD-COMPLETED — completed op shape
CASES.append(Case(
    id="OP-GET-CRUD-COMPLETED",
    title="Get of completed Create-LB op returns done=true with response payload",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/nlb/v1/networkLoadBalancers",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "opget-done-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),
        Step(name="get-op-done", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('done=true', () => pm.expect(j.done).to.eql(true));",
                          "pm.test('has response or error', () => pm.expect(j.response || j.error).to.exist);",
                          "if (j.response) pm.test('metadata has networkLoadBalancerId', "
                          "  () => pm.expect(j.metadata && j.metadata.networkLoadBalancerId).to.match(/^nlb/));"]),
        Step(name="cleanup", method="DELETE", path="/nlb/v1/networkLoadBalancers/{{nlbId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))


# -- OP-GET-NEG-NF-INVALID-PREFIX — opsproxy verbatim-YC: malformed id → InvalidArgument
CASES.append(Case(
    id="OP-GET-NEG-NF-INVALID-PREFIX",
    title="Get with malformed opId (no known prefix) → 400 INVALID_ARGUMENT 'invalid operation id'",
    classes=["NEG"], priority="P0",
    steps=[
        Step(name="get-garbage", method="GET", path="/operations/{{garbageInvalidOpId}}",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('mentions invalid operation id', () => "
                 "  pm.expect(pm.response.json().message.toLowerCase()).to.include('invalid operation id'));",
             ]),
    ],
))


# -- OP-GET-NEG-NF-VALID-PREFIX — well-formed prefix but no row
CASES.append(Case(
    id="OP-GET-NEG-NF-VALID-PREFIX",
    title="Get of well-formed but missing opId → 404 NOT_FOUND",
    classes=["NEG"], priority="P1",
    steps=[
        # garbageOpId is a non-existent op handle: a malformed value → 400 (invalid
        # operation id), a well-formed-but-absent value → 404, and an unresolvable
        # scope → 403 authz-first — all = rejected, no leak. Tolerant per api-conventions.
        Step(name="get-missing", method="GET", path="/operations/{{garbageOpId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))


# -- OP-LST-NEG-UNROUTED-FAIL-CLOSED — путь без записи в каталоге прав отказывает fail-closed
CASES.append(Case(
    id="OP-LST-NEG-UNROUTED-FAIL-CLOSED",  # index: *-LST-NEG-UNROUTED-FAIL-CLOSED
    title="Коллекция /operations (RPC в контракте нет) → 403 fail-closed по отсутствию записи в каталоге прав",
    classes=["NEG", "SEC"], priority="P1",
    steps=[
        # У сервиса операций в контракте ровно два метода — `Get` и `Cancel`
        # (operation_service.proto); списка по проекту нет ни в контракте, ни в таблице
        # REST-маршрутов края (там только `/operations/{id}` и `/operations/{id}:cancel`).
        # Значит предмет этого кейса — не «список операций», а поведение края на пути,
        # которому не соответствует ни один метод: имя метода не резолвится, записи в
        # каталоге прав нет, и край отказывает fail-closed — код 7, «catalog: no entry for
        # method» (инвариант security.md #4). Отказ выносится ДО маршрутизации
        # grpc-gateway, поэтому 5xx и mux-404 здесь невозможны by construction.
        #
        # Прежде кейс назывался «List operations in project — returns array» и принимал
        # `oneOf([200, 403])` «на случай, если метод каталогизирован». Каталогизировать
        # нечего: метода нет. Утверждение, принимавшее оба исхода, не могло упасть ни на
        # одном — в том числе на настоящем регрессе, когда неизвестный путь начал бы
        # отвечать 200. Заявлен один исход, тот, который есть.
        #
        # Если проектный список операций когда-нибудь появится в контракте — он придёт со
        # своим методом, своей записью в каталоге и своим кейсом; этот останется стражем
        # умолчания для НЕизвестных путей.
        Step(name="list-ops-unrouted", method="GET",
             path="/operations?projectId={{_suiteProjectId}}&pageSize=10",
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
                 # Отказ по неизвестному методу не должен раскрывать ничего о проекте,
                 # названном в запросе.
                 "pm.test('отказ не упоминает проект из запроса', () => "
                 "  pm.expect(pm.response.text()).to.not.contain(pm.environment.get('_suiteProjectId')));",
             ]),
    ],
))


# -- OP-CANCEL-STATE-ALREADY-DONE — Cancel on already-done op → FailedPrecondition
CASES.append(Case(
    id="OP-CANCEL-STATE-ALREADY-DONE",
    title="Cancel an already-done op → 400 FailedPrecondition 'operation is already completed'",
    classes=["STATE", "NEG"], priority="P1",
    steps=[
        Step(name="create-fast", method="POST", path="/nlb/v1/networkLoadBalancers",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "opcanc-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),
        Step(name="cancel-done", method="POST", path="/operations/{{opId}}:cancel",
             test_script=[
                 # 409 снят: производителя у него на этой полосе нет. Отмена
                 # эмитит InvalidArgument/NotFound/FailedPrecondition/Internal,
                 # что по таблице края даёт 400/404/400/500; 409 приходит только
                 # от ALREADY_EXISTS и ABORTED, которых здесь не бывает. Допуск
                 # на небывалый исход не краснеет никогда, а тот же файл ниже
                 # (OP-CANCEL-IDEMPOTENT) этот довод и формулирует — оставить его
                 # значило бы держать в одном файле два взаимоисключающих
                 # утверждения. Соседи по предикату (compute, storage) ждут 400.
                 "pm.test('rejected with FailedPrecondition', () => "
                 "  pm.expect(pm.response.code).to.eql(400));",
                 "if (pm.response.code === 400) {",
                 "  pm.test('grpc code 9 (FAILED_PRECONDITION)', () => "
                 "    pm.expect(pm.response.json().code).to.eql(9));",
                 "  pm.test('mentions already completed', () => "
                 "    pm.expect((pm.response.json().message||'').toLowerCase()).to.include('already'));",
                 "}",
             ]),
        Step(name="cleanup", method="DELETE", path="/nlb/v1/networkLoadBalancers/{{nlbId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))


# -- OP-CANCEL-IDEMPOTENT — повторная отмена возвращает ТО ЖЕ, что первая
#
# ПОЧЕМУ ЭТОТ КЕЙС ЕСТЬ. До сведения арендаторской полосы операции в общий слой
# nlb отвечал на повторную отмену уже отменённой операции FAILED_PRECONDITION,
# тогда как шесть остальных доменов отвечали успехом: их `CancelOwned`
# идемпотентен на уже отменённой. Расхождение было объявлено осознанным и
# держалось на ложной посылке о паритете с vpc/compute. Сведение сняло его в
# пользу шести — и это единственная смена наблюдаемого поведения, поэтому у неё
# обязан быть кейс, а не только комментарий.
#
# ПОЧЕМУ УТВЕРЖДАЕТСЯ СОВПАДЕНИЕ ИСХОДОВ, А НЕ КОНКРЕТНЫЙ КОД. Успеть отменить
# операцию ДО её завершения — гонка, и кейс, требующий выигрыша в ней, был бы
# нестабильным по построению. Идемпотентность выражается иначе и точно: второй
# вызов обязан вернуть ТО ЖЕ, что первый, каким бы тот ни оказался.
#
# ГРАНИЦА ЭТОГО КЕЙСА НАЗВАНА ВСЛУХ. Если операция удаления успевает завершиться
# до первой отмены, обе отмены отвечают отказом «уже завершена», и кейс зеленеет,
# НЕ ПРОВЕРИВ изменившуюся ветвь (идемпотентный успех на уже отменённой). Скрывать
# это нельзя: шаг ПЕЧАТАЕТ достигнутую ветвь в отчёт, поэтому «зелено» отличимо от
# «зелено и проверено». Ветвь, до которой кейс может не дойти, закреплена там, где
# она детерминирована: `pkg/operations/operationspb` —
# TestCancelReturnsTheOperationWhenRepoAcceptsRecancel.
#
# ПОЧЕМУ ОТМЕНЯЕТСЯ ОПЕРАЦИЯ УДАЛЕНИЯ, А НЕ СОЗДАНИЯ. Операция несёт
# предвыделенный идентификатор ресурса в metadata ДАЖЕ когда завершилась
# ошибкой, поэтому публиковать его с операции, исход которой мы намеренно
# оставляем неопределённым, значит заводить фантом. У операции удаления
# публиковать нечего: ресурс уже создан и его идентификатор известен.
CASES.append(Case(
    id="OP-CANCEL-IDEMPOTENT",
    title="Повторная отмена возвращает тот же исход, что первая (идемпотентность)",
    classes=["IDM", "STATE"], priority="P1",
    steps=[
        Step(name="create-for-idem", method="POST", path="/nlb/v1/networkLoadBalancers",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "opidem-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),

        # Операция, которую отменяем. Идентификатора ресурса она не публикует.
        Step(name="delete-for-idem", method="DELETE", path="/nlb/v1/networkLoadBalancers/{{nlbId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "idemOpId")]),

        Step(name="cancel-first", method="POST", path="/operations/{{idemOpId}}:cancel",
             test_script=[
                 # 409 в допуск НЕ входит: полоса отмены эмитит только
                 # InvalidArgument/NotFound/FailedPrecondition/Internal, то есть
                 # производителя у 409 нет, а допуск на небывалый исход не
                 # краснеет никогда.
                 "pm.test('исход первой отмены — законный', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                 "pm.environment.set('cancelFirstCode', String(pm.response.code));",
             ]),
        Step(name="cancel-second", method="POST", path="/operations/{{idemOpId}}:cancel",
             test_script=[
                 "const first = pm.environment.get('cancelFirstCode');",
                 "pm.test('повторная отмена вернула тот же исход, что первая', () => "
                 "  pm.expect(String(pm.response.code)).to.eql(first));",
                 "if (pm.response.code === 200) {",
                 "  pm.test('и ту же операцию', () => "
                 "    pm.expect(pm.response.json().id).to.eql(pm.environment.get('idemOpId')));",
                 "}",
                 # Достигнутая ветвь — в отчёт: «зелено» обязано быть отличимо от
                 # «зелено и проверило изменившееся поведение».
                 "console.log(pm.response.code === 200 "
                 "  ? 'OP-CANCEL-IDEMPOTENT: достигнута ветвь ИДЕМПОТЕНТНОГО УСПЕХА (изменившаяся)' "
                 "  : 'OP-CANCEL-IDEMPOTENT: достигнута ветвь ОТКАЗА «уже завершена» — "
                 "изменившаяся ветвь НЕ проверена этим прогоном');",
             ]),

        # Уборка: отменённое удаление могло не состояться, поэтому повторяем его
        # и терпим уже-удалённое. Терпимость здесь не маска — она НАЗВАНА, и названа
        # тем же маркером, что у остальных уборок суиты: `best-effort (never fails the
        # case)`. Маркер — соглашение корпуса, а не украшение: он виден в отчёте
        # прогона и он же служит признаком заявленной терпимости для гейта честности
        # утверждений. Вторая форма записи того же завела бы два места об одном
        # предмете, и распознавателю пришлось бы держать незамкнутое множество
        # синонимов — поэтому русская проза остаётся, а маркер стоит рядом с ней.
        #
        # СВЯЗАТЬ исход уборки с исходом отмены НЕЛЬЗЯ, и это свойство продукта, а не
        # недосмотр кейса: отмена помечает ЗАПИСЬ операции (`done=true`, CANCELLED) и
        # не откатывает работу исполнителя — `pkg/operations` прямо оговаривает, что
        # параллельный `MarkDone` после отмены попадает на тот же CAS и остаётся
        # no-op. Значит удаление доезжает независимо от того, удалась ли отмена, и
        # ресурс к этому шагу законно может как существовать, так и нет. Утверждение
        # «уборка вернула то, что следует из исхода отмены» было бы ложным.
        Step(name="cleanup-idem", method="DELETE", path="/nlb/v1/networkLoadBalancers/{{nlbId}}",
             test_script=[
                 "pm.test('уборка best-effort (never fails the case): удалено либо уже отсутствует', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
                 "if (pm.response.code === 200) { "
                 "  const j = pm.response.json(); if (j && j.id) pm.environment.set('opId', j.id); }",
             ]),
    ],
))
