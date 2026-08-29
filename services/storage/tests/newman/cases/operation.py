# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для OperationService (kacho-storage) — Get + Cancel через api-gateway OpsProxy.

Все storage-операции имеют op-root prefix `sop` (общий для Volume/Snapshot/Image,
декаплен от ресурсного префикса vol/snp/img). api-gateway OpsProxy маршрутизирует
`/operations/{id}` и `/operations/{id}:cancel` по domain-prefix id → backend `storage`.
Клиент поллит OperationService.Get(id) до done=true (Watch RPC не существует).

Покрытие доведено до паритета с compute-набором перед сжатием раскола блочного
хранения: у compute эта поверхность покрыта восемью кейсами, у storage было три.
Не покрыты были ветка `error` в `oneof result` (failed-op), контракт-тон
not-found-сообщения и ВСЯ поверхность Cancel. Удаление compute-дубля без этого
дописывания унесло бы единственные живые проверки этих ветвей.
"""

CASES = []

VOL = "/storage/v1/volumes"
_DEF_SIZE = 10737418240  # 10 GiB


def _vol_body(suffix):
    return {"projectId": "{{_suiteProjectId}}", "name": f"vol-op-{suffix}-{{{{runId}}}}",
            "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
            "sizeBytes": _DEF_SIZE}


CASES.append(Case(
    id="OP-GET-CRUD-OK",
    title="Get done-operation (после Volume.Create) → done=true, response, metadata.volumeId, id prefix sop",
    classes=["CRUD", "CONF"], priority="P1",
    # verifies CS1-S1-01 (§0.1 Operation poll surface — OperationService.Get до done)
    steps=[
        Step(name="create-trigger", method="POST", path=VOL, body=_vol_body("get"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        Step(name="get-op", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('id matches & sop prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('opId')); pm.expect(j.id).to.match(/^sop/); });",
                          "pm.test('done=true', () => pm.expect(j.done).to.eql(true));",
                          "pm.test('has response (no error)', () => { pm.expect(j.response).to.be.an('object'); pm.expect(j.error).to.be.oneOf([undefined, null]); });",
                          "pm.test('metadata.volumeId prefix vol', () => pm.expect(j.metadata && j.metadata.volumeId).to.match(/^vol/));",
                          "pm.test('createdAt present', () => pm.expect(j.createdAt).to.be.a('string'));"]),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="OP-GET-CRUD-FAILED-OP",
    title="Get завершённой failed-operation (Volume.Create с несуществующим diskTypeId) → done=true, error без response, code 9 FAILED_PRECONDITION",
    classes=["CRUD", "NEG", "CONF"], priority="P1",
    # verifies §0.1 (Operation несёт `oneof result {error|response}`; failed-op читается тем
    #   же OperationService.Get). Триггер выбран так, чтобы отказ был АСИНХРОННЫМ:
    #   несуществующий projectId ушёл бы в gateway-scope_extractor и вернул sync-403
    #   (authz-first) — операции бы вообще не возникло. Несуществующий diskTypeId проходит
    #   authz (scope резолвится по валидному projectId) и падает уже в worker'е на
    #   peer/FK-предусловии → ровно та форма, которую этот кейс обязан прочитать.
    steps=[
        Step(name="create-bad-disktype", method="POST", path=VOL,
             body={"projectId": "{{_suiteProjectId}}", "name": "vol-opfail-{{runId}}",
                   "zoneId": "{{existingZoneId}}", "diskTypeId": "block-nope-{{runId}}",
                   "sizeBytes": _DEF_SIZE},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-failed-op", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('done=true', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                          "pm.test('has error, no response (oneof result)', () => { pm.expect(j.error, JSON.stringify(j)).to.be.an('object'); pm.expect(j.response).to.be.oneOf([undefined, null]); });",
                          "pm.test('error.code 9 FAILED_PRECONDITION', () => pm.expect(j.error.code, JSON.stringify(j.error)).to.eql(9));",
                          "pm.test('error.message names the missing DiskType', () => pm.expect(j.error.message || '').to.include('not found'));"]),
        # Cleanup-шага НЕТ, и это осознанно: op несёт pre-allocated metadata.volumeId ДАЖЕ
        # на done+error (id аллоцируется до async-фейла). Тома с этим id не существует —
        # удалять фантом нечего, а протаскивать его id в env было бы ровно тем каскадом
        # ложных диагнозов, от которого предостерегает fixture-дисциплина.
    ],
))

CASES.append(Case(
    id="OP-GET-NEG-NOTFOUND-VALID-PREFIX",
    title="Get well-formed-но-нет sop-opId → 404 NOT_FOUND",
    classes=["NEG"], priority="P1",
    # verifies §0.1 (OpsProxy sop-routing → storage backend; well-formed-но-нет → NotFound)
    steps=[Step(name="get-nx", method="GET", path="/operations/{{garbageStorageOpId}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")])],
))

CASES.append(Case(
    id="OP-GET-CONF-NF-TEXT",
    title="Get well-formed-но-нет sop-opId → сообщение ДОСЛОВНО 'operation <id> not found'",
    classes=["CONF", "NEG"], priority="P1",
    # verifies §0.1 (тон сообщения — часть контракта, api-conventions «<Resource> %s not found»).
    #   Отдельным кейсом от кода: код и текст ломаются независимо, поэтому lock ставится на
    #   уровне обсервабла (сообщение), а не только на gRPC-коде.
    # Утверждается РАВЕНСТВО вычисленному тексту, без приведения регистра: приведение
    # НЕ РАЗЛИЧАЕТ регистр by construction, и расхождение тона края с тоном владельца
    # не могло покраснеть ни в одном прогоне (#1370, #1401). Текст полосы известен
    # целиком — производителей два на всё дерево, и они обязаны совпадать побайтово
    # (`security.md` §Hardening #6).
    steps=[Step(name="get-nx-text", method="GET", path="/operations/{{garbageStorageOpId}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             "pm.test('сообщение дословно равно тексту владельца (и потому несёт сам id)', () => "
                             "  pm.expect(pm.response.json().message).to.eql("
                             "'operation ' + pm.environment.get('garbageStorageOpId') + ' not found'));"])],
))

CASES.append(Case(
    id="OP-GET-NEG-UNKNOWN-PREFIX",
    title="Get opId без known domain-prefix → 400 INVALID_ARGUMENT 'invalid operation id \"<X>\"' (OpsProxy отвергает неизвестный/malformed префикс)",
    classes=["NEG"], priority="P1",
    # verifies §0.1 (OpsProxy prefix-routing guard — неизвестный префикс отвергается;
    #   контракт-текст opsproxy/proxy.go: `invalid operation id %q`, не содержит слова "prefix")
    steps=[Step(name="get-garbage-prefix", method="GET", path="/operations/{{garbageId}}",
                # Имя пробы обещало равенство («message ==»), а тело проверяло вхождение
                # подстроки в нижнем регистре: имя и тело утверждали разное, и верным было
                # имя. Теперь равенство и есть утверждение.
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('сообщение дословно равно тексту края', () => "
                             "  pm.expect(pm.response.json().message).to.eql("
                             "'invalid operation id \"' + pm.environment.get('garbageId') + '\"'));"])],
))

# ---------------------------------------------------------------------------
# Cancel — вторая половина поверхности OperationService. У compute она покрыта
# тремя негативами; у storage её не было вовсе, хотя маршрут `/operations/{id}:cancel`
# общий (OpsProxy маршрутизирует по domain-prefix, sop → storage). Без этих кейсов
# сжатие раскола убрало бы единственную живую проверку cancel-поверхности вместе с
# compute-коллекциями.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="OP-CANCEL-NEG-ALREADY-DONE",
    title="Cancel уже завершённой sop-operation → 400 FAILED_PRECONDITION 'operation <id> already completed'",
    classes=["NEG", "STATE", "CONF"], priority="P1",
    # verifies §0.1 (Cancel — терминальный отказ на завершённой операции: done — конечное
    #   состояние, отменять нечего. Assert и код, и контракт-тон сообщения.)
    steps=[
        Step(name="create-trigger", method="POST", path=VOL, body=_vol_body("cancel"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        # op завершилась успешно — только теперь у cancel есть предмет спора.
        assert_op_success(),
        Step(name="cancel-done", method="POST", path="/operations/{{opId}}:cancel", body={},
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          "pm.test('сообщение дословно равно тексту владельца (и потому несёт сам id)', () => "
                          "  pm.expect(pm.response.json().message).to.eql("
                          "'operation ' + pm.environment.get('opId') + ' already completed'));"]),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="OP-CANCEL-NEG-NOTFOUND",
    title="Cancel well-formed-но-нет sop-opId → 404 NOT_FOUND 'operation <id> not found'",
    classes=["NEG", "CONF"], priority="P1",
    # verifies §0.1 (Cancel по той же by-lane дисциплине, что Get: well-formed-но-нет → NotFound)
    steps=[Step(name="cancel-nx", method="POST", path="/operations/{{garbageStorageOpId}}:cancel", body={},
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             "pm.test('сообщение дословно равно тексту владельца (и потому несёт сам id)', () => "
                             "  pm.expect(pm.response.json().message).to.eql("
                             "'operation ' + pm.environment.get('garbageStorageOpId') + ' not found'));"])],
))

CASES.append(Case(
    id="OP-CANCEL-NEG-UNKNOWN-PREFIX",
    title="Cancel opId без known domain-prefix → 400 INVALID_ARGUMENT 'invalid operation id \"<X>\"'",
    classes=["NEG", "CONF"], priority="P1",
    # verifies §0.1 (malformed-id отвергается ДО маршрутизации к backend'у — на обеих
    #   ops-поверхностях одинаково, иначе cancel стал бы дырой в prefix-guard)
    steps=[Step(name="cancel-garbage-prefix", method="POST", path="/operations/{{garbageId}}:cancel", body={},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('сообщение дословно равно тексту края', () => "
                             "  pm.expect(pm.response.json().message).to.eql("
                             "'invalid operation id \"' + pm.environment.get('garbageId') + '\"'));"])],
))
