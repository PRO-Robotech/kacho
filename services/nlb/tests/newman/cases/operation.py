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
    title="Cancel an already-done op → 400/409 FailedPrecondition 'operation is already completed'",
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
                 "pm.test('rejected with FailedPrecondition', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([400, 409]));",
                 "if (pm.response.code === 400 || pm.response.code === 409) {",
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
