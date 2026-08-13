# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для OperationService (kacho-compute) — Get / Cancel.

Все compute-операции имеют prefix `epd` (PrefixOperationCompute == PrefixInstance);
api-gateway OpsProxy маршрутизирует /operations/{id} по первым 3 символам id → backend `compute`.

АКТОР. Дефолт коллекции — ПРОЕКТНЫЙ (`jwtProjectAdminA1`), и для Operation это не косметика:
`OperationService.Get/Cancel` энфорсит ВЛАДЕНИЕ (владелец = принципал, создавший операцию), а
чужому отвечает NotFound. То есть под проектным актором кейсы проверяют, что тенант читает и
отменяет СВОИ операции, а не что кластерный админ читает любые. Два отступления объявлены на
месте: посев каталога размерностей (`system_admin` на cluster-singleton) и кейс
`OP-GET-CRUD-FAILED-OP`, чей спусковой крючок проектному актору недоступен by construction.
"""

CASES = []

# Operation is exercised through Instance.Create/Delete. It used to ride on
# compute's own Disk resource; that duplicate is retired (kacho-storage owns block
# storage), and the Operation contract belongs to neither — so the probe moved to
# the resource compute still owns.
INSTANCES = "/compute/v1/instances"
MT_INT = "/compute/v1/internal/machineTypes"      # admin seed (:8081, ban #6)
_BOOT_STORAGE = {"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts"}


def _seed_mt(suffix):
    """Seed a MachineType (Internal*, :8081) → sets mtId. Instance.Create needs one.

    Актор — cluster-admin (`ADMIN_AUTH`): админ-CRUD каталога размерностей гейтится
    `system_admin` на cluster-singleton, а дефолт коллекции проектный. Опрос Operation несёт
    того же актора — владелец операции есть создавший её принципал."""
    body = {"name": f"mtop{suffix}{{{{runId}}}}", "family": "STANDARD",
            "effectiveResources": {"vCpu": 2, "memoryMib": 8192, "gpus": 0},
            "availableZones": ["{{existingZoneId}}", "{{existingZoneAltId}}"], "status": "AVAILABLE"}
    return [Step(name=f"seed-mt-{suffix}", method="POST", path=MT_INT, body=body, internal=True,
                 auth=ADMIN_AUTH,
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                              *save_from_response("j.metadata && j.metadata.machineTypeId", "mtId")]),
            poll_operation_until_done(auth=ADMIN_AUTH), assert_op_success(auth=ADMIN_AUTH)]


def _cleanup_mt():
    return [Step(name="cleanup-mt", method="DELETE", path=MT_INT + "/{{mtId}}", internal=True,
                 auth=ADMIN_AUTH, test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(auth=ADMIN_AUTH)]


def _inst_body(suffix, project="{{_suiteProjectId}}", mt="{{mtId}}"):
    # acknowledgeUnreachable снимает F5 unreachable-guard: создание VM без внешнего адреса и
    # ssh-ключей И без внешнего адреса — sync FAILED_PRECONDITION ("VM will be RUNNING
    # but unreachable … set acknowledgeUnreachable:true"). Эти кейсы про Operation-
    # семантику, а не про достижимость, поэтому снимаем гейт ключом — так же, как
    # соседние instance-redesign (_vm_body) и list-filter (_instance_body).
    return {"projectId": project, "name": f"insop{suffix}{{{{runId}}}}",
            "zoneId": "{{existingZoneId}}", "instanceKind": "VM", "machineTypeId": mt,
            "bootSource": dict(_BOOT_STORAGE),
            "vmSpec": {"userData": "#cloud-config\n{}",
                       "metadataOptions": {"metadataEndpoint": "ENABLED"}},
            "acknowledgeUnreachable": True,
            "networkInterfaceSpecs": [{"subnetId": "{{existingSubnetId}}",
                                       "securityGroupIds": ["{{existingSgId}}"]}]}


CASES.append(Case(
    id="OP-GET-CRUD-OK",
    title="Get свежесозданной operation (после Instance.Create) → done=true, has response, metadata.instanceId",
    classes=["CRUD"], priority="P1",
    steps=[
        *_seed_mt("get"),
        Step(name="create-trigger", method="POST", path=INSTANCES,
             body=_inst_body("get"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.instanceId", "instanceId")]),
        poll_operation_until_done(),
        Step(name="get-op", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('id matches & epd prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('opId')); pm.expect(j.id).to.match(/^epd/); });",
                          "pm.test('done=true', () => pm.expect(j.done).to.eql(true));",
                          "pm.test('has response (no error)', () => { pm.expect(j.response).to.be.an('object'); pm.expect(j.error).to.be.oneOf([undefined, null]); });",
                          "pm.test('metadata has instanceId (ins-...)', () => pm.expect(j.metadata && j.metadata.instanceId).to.match(/^ins-/));",
                          "pm.test('createdAt present', () => pm.expect(j.createdAt).to.be.a('string'));"]),
        Step(name="cleanup", method="DELETE", path=INSTANCES + "/{{instanceId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_mt(),
    ],
))

# АКТОР ЗДЕСЬ CLUSTER-ADMIN, И ЭТО СВОЙСТВО МОДЕЛИ, А НЕ ПОСЛАБЛЕНИЕ.
#
# Предмет кейса — конверт Operation с ошибкой: что асинхронный сбой доезжает до клиента как
# `done=true` + `error.code`, а не как пустой успех. Спусковой крючок — создание в ЧУЖОМ,
# несуществующем проекте, и вот его-то проектный актор построить не может ПО ПОСТРОЕНИЮ:
# `InstanceService/Create` гейтится `editor` на `project:<projectId>`, извлекаемом из тела
# запроса, поэтому на проект без выдачи край отвечает отказом ДО того, как Operation вообще
# заведётся (anti-BOLA, и это правильно). У бутстрапа же решение о доступе не зависит от
# существования цели — кластерный ярус разрешает плоско, — поэтому дойти до асинхронного
# промаха может только он.
#
# Почему это не маскировка: утверждения кейса не ослаблены ни на одно, и об авторизации он
# не утверждает ничего — отказ в доступе проектному актору на чужой проект уже проверяет
# матрица `authz-deny` кейсом `AUTHZ-INSTANCE-CR-CROSS-PA1` (строка DENY: строгие 403 + код
# 7). Здесь спрашивают о другом, и спрашивают тем принципалом, которым спросить возможно.
CASES.append(Case(
    id="OP-GET-CRUD-FAILED-OP",
    title="Get завершённой failed-operation (Instance.Create в garbage project) → done=true, has error code 5",
    classes=["CRUD", "NEG"], priority="P1",
    steps=[
        # # requires peer-validation enabled
        *_seed_mt("fail"),
        Step(name="create-bad", method="POST", path=INSTANCES, auth=ADMIN_AUTH,
             body=_inst_body("fail", project="{{garbageRmId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth=ADMIN_AUTH),
        Step(name="get-op", method="GET", path="/operations/{{opId}}", auth=ADMIN_AUTH,
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('done=true', () => pm.expect(j.done).to.eql(true));",
                          "pm.test('has error (no response)', () => { pm.expect(j.error).to.be.an('object'); pm.expect(j.response).to.be.oneOf([undefined, null]); });",
                          "pm.test('error.code 5 NOT_FOUND', () => pm.expect(j.error.code).to.eql(5));",
                          "pm.test('error.message non-empty', () => pm.expect(j.error.message).to.be.a('string').and.length.greaterThan(0));"]),
    ],
))

CASES.append(Case(
    id="OP-GET-NEG-NOTFOUND-VALID-PREFIX",
    title="Get несуществующего opId с правильным epd-префиксом → 404 NOT_FOUND",
    classes=["NEG"], priority="P1",
    steps=[Step(name="get-nx", method="GET", path="/operations/{{garbageComputeId}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")])],
))

CASES.append(Case(
    id="OP-GET-CONF-NF-TEXT",
    title="Get несуществующего epd-opId → текст содержит 'not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[Step(name="get-nx", method="GET", path="/operations/{{garbageComputeId}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             # probe-needed: точный verbatim YC text — предполагаем "Operation <id> not found"
                             "pm.test('text mentions not found', () => pm.expect((pm.response.json().message || '').toLowerCase()).to.include('not found'));"])],
))

CASES.append(Case(
    id="OP-GET-NEG-UNKNOWN-PREFIX",
    title="Get opId без known 3-char prefix (garbage id) → 400 InvalidArgument 'invalid operation id'",
    classes=["NEG"], priority="P0",
    steps=[Step(name="get-garbage-prefix", method="GET", path="/operations/{{garbageId}}",
                # OpsProxy в api-gateway отвергает id без known 3-char prefix → 400 InvalidArgument.
                # Наблюдаемый контракт (api-conventions «malformed id → invalid <res> id <X>»):
                # message = `invalid operation id "<X>"` (не слово 'prefix' — это внутренняя причина,
                # не текст ответа). Проверяем конвенциональный текст, а не термин реализации.
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('mentions invalid operation id (malformed-id convention)', () => pm.expect((pm.response.json().message || '').toLowerCase()).to.include('invalid operation id'));"])],
))

# Отмена ЗАВЕРШЁННОЙ операции отвергается, и это ровно один исход — установлено по коду,
# а не по догадке о том, как ведут себя чужие реализации.
#
# `CancelOwned` — один CAS `UPDATE … WHERE done = false`; ноль строк классифицируется
# повторным чтением под тем же предикатом владения. Идемпотентный 200 предусмотрен ТОЛЬКО
# для операции, уже ОТМЕНЁННОЙ ранее (у неё код ошибки — «отменена»); терминальный успех
# или сбой дают `ErrAlreadyDone`, и обработчик переводит это в `FAILED_PRECONDITION`
# «operation <id> already completed». Здесь операция завершилась успехом, отменённой она
# не была — значит идемпотентная ветка недостижима.
#
# Прежнее `oneOf([200, 400])` под заголовком «FailedPrecondition (или 200 idempotent)»
# принимало и отказ, и приём отмены завершённой операции: упасть на предмете кейса оно не
# могло. Заявлен код, а вместе с ним и текст — иначе кейс прошёл бы на любом постороннем
# `FAILED_PRECONDITION` (например на «инстанс не в том состоянии»).
CASES.append(Case(
    id="OP-CANCEL-NEG-ALREADY-DONE",
    title="Cancel завершённой operation (Instance.Create уже done) → 400 FAILED_PRECONDITION 'already completed'",
    classes=["NEG", "STATE"], priority="P1",
    steps=[
        *_seed_mt("cancel"),
        Step(name="create-trigger", method="POST", path=INSTANCES,
             body=_inst_body("cancel"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.instanceId", "instanceId")]),
        poll_operation_until_done(),
        # Предмет кейса — отказ в отмене ЗАВЕРШЁННОЙ операции, поэтому её идентификатор
        # фиксируется отдельной переменной: шаги ниже перезаписывают `opId` своими
        # операциями, и без фиксации отмена ушла бы не туда.
        Step(name="pin-done-op", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "pm.test('операция действительно завершена', () => pm.expect(pm.response.json().done, pm.response.text()).to.eql(true));",
                          "pm.test('и завершена НЕ отменой (иначе отмена была бы идемпотентной)', () => "
                          "  pm.expect((pm.response.json().error||{}).code || 0).to.not.eql(1));",
                          *save_from_response("j.id", "doneOpId")]),
        Step(name="cancel-done", method="POST", path="/operations/{{doneOpId}}:cancel", body={},
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          "pm.test('текст называет причину — операция уже завершена', () => "
                          "  pm.expect((pm.response.json().message || '').toLowerCase()).to.include('already completed'));"]),
        Step(name="cleanup", method="DELETE", path=INSTANCES + "/{{instanceId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="OP-CANCEL-NEG-NOTFOUND",
    title="Cancel несуществующего epd-opId → 404 NOT_FOUND",
    classes=["NEG"], priority="P1",
    steps=[Step(name="cancel-nx", method="POST", path="/operations/{{garbageComputeId}}:cancel", body={},
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")])],
))

CASES.append(Case(
    id="OP-CANCEL-NEG-UNKNOWN-PREFIX",
    title="Cancel opId без known prefix → 400 InvalidArgument 'prefix'",
    classes=["NEG"], priority="P2",
    steps=[Step(name="cancel-garbage", method="POST", path="/operations/{{garbageId}}:cancel", body={},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))
