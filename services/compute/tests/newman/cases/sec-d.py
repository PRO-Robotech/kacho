# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для SEC-D (kacho-compute): FGA owner-tuple через kaname
(transactional-outbox) + opt-in mTLS.

SEC-D устраняет прямой доступ compute к OpenFGA: на каждый resource Create/Delete
intent owner-tuple пишется строкой в compute_fga_register_outbox В ТОЙ ЖЕ
writer-tx, что и Insert/Delete ресурса; register-drainer применяет его через
InternalIAMService.RegisterResource/UnregisterResource. Публичный контракт
ресурсов НЕ меняется (эпик #8) — эти кейсы гоняют существующие публичные RPC и
проверяют, что after-create per-resource Get резолвится (owner-tuple применён
eventual через IAM), а Delete → Get 404.

Контракт изоляции: каждый case в своём runId, работает внутри pre-allocated
existingProjectId (_suiteProjectId из env); имена суффиксуются {{runId}}.

АКТОР. Дефолт коллекции — ПРОЕКТНЫЙ (`jwtProjectAdminA1`), и здесь это несущее: предмет
кейса — что owner-tuple ресурса ДОЕЗЖАЕТ через очередь регистраций и пообъектная проверка
после этого резолвится. Под бутстрап-админом утверждение было бы вакуумным — кластерный ярус
разрешает плоско, поэтому `Get` прошёл бы и при вовсе не доставленном tuple. Cluster-admin
остаётся только на посеве каталога размерностей (`auth=ADMIN_AUTH`).
Носителем этих проб был Disk; дубль блочного хранения снят (владелец —
kacho-storage), а проверяемое свойство — owner-tuple через outbox — принадлежит
не ему, поэтому пробы переехали на Instance. id-prefix Instance = `ins-`.

mTLS-mismatch (SEC-D-21) и cross-service-owner-down (SEC-D-23) — отдельные
негативы, требующие управления инфраструктурой стенда (peer down / per-edge
TLS-flag); помечены `# requires`-аннотацией и гоняются в dedicated профиле, не в
обычном regression-проходе.
"""

CASES = []

INSTANCES = "/compute/v1/instances"
MT_INT = "/compute/v1/internal/machineTypes"      # admin seed (:8081, ban #6)
_BOOT_STORAGE = {"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts"}


def _seed_mt(suffix):
    """Seed a MachineType (Internal*, :8081) → sets mtId. Instance.Create needs one.

    Актор — cluster-admin (`ADMIN_AUTH`): админ-CRUD каталога размерностей гейтится
    `system_admin` на cluster-singleton, а дефолт коллекции проектный. Опрос Operation несёт
    того же актора — владелец операции есть создавший её принципал."""
    body = {"name": f"mtsd{suffix}{{{{runId}}}}", "family": "STANDARD",
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


# ---------------------------------------------------------------------------
# SEC-D-15 — happy: Create → Operation done → Get показывает ресурс (owner-tuple
# применён eventual через IAM register-drainer) → Delete → Get 404.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SECD-CR-GET-AFTER-TUPLE-OK",
    title="SEC-D-15: Create instance → Operation done → Get показывает ресурс (per-resource Check резолвится, owner-tuple применён через IAM) → Delete → Get 404",
    classes=["CONF", "IDM"], priority="P1",
    steps=[
        *_seed_mt("ok"),
        # acknowledgeUnreachable снимает F5 unreachable-guard: VM без внешнего адреса
        # И без внешнего адреса отвергается sync FAILED_PRECONDITION. Кейс про owner-tuple
        # и per-resource Check, а не про достижимость — гейт снимаем ключом, как в
        # соседних instance-redesign/list-filter.
        Step(name="create", method="POST", path=INSTANCES,
             body={"projectId": "{{_suiteProjectId}}", "name": f"secdins{{{{runId}}}}",
                   "zoneId": "{{existingZoneId}}", "instanceKind": "VM", "machineTypeId": "{{mtId}}",
                   "bootSource": dict(_BOOT_STORAGE),
                   "vmSpec": {"userData": "#cloud-config\n{}",
                              "metadataOptions": {"metadataEndpoint": "ENABLED"}},
                   "acknowledgeUnreachable": True,
                   "networkInterfaceSpecs": [{"subnetId": "{{existingSubnetId}}",
                                              "securityGroupIds": ["{{existingSgId}}"]}],
                   "labels": {"suite": "sec-d"}},
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.instanceId", "instanceId")]),
        poll_operation_until_done(),
        assert_op_success(),
        # per-resource Get: резолвится → owner-tuple зарегистрирован в IAM (раньше
        # best-effort dual-write мог потерять tuple → DENY навсегда; теперь intent
        # durable + retried, окно DENY конечно).
        retry_until_authorized(Step(name="get", method="GET", path=INSTANCES + "/{{instanceId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('id matches & ins- prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('instanceId')); pm.expect(j.id).to.match(/^ins-/); });",
                          "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteProjectId')));",
                          *assert_created_at_seconds()])),
        Step(name="delete", method="DELETE", path=INSTANCES + "/{{instanceId}}",
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        assert_op_success(),
        # после Delete — Get → 404 (unregister-intent тоже записан в writer-tx).
        # HTTP-статус 404, grpc-код в теле = 5 (NOT_FOUND) — не путать (404 — транспорт).
        Step(name="get-after-delete", method="GET", path=INSTANCES + "/{{instanceId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        *_cleanup_mt(),
    ],
))


# ---------------------------------------------------------------------------
# SEC-D negative (deterministic, без управления инфраструктурой): Delete
# несуществующего ресурса. Delete-мутация делает ownership-pre-check (svc.Get)
# ПЕРВЫМ стейтментом (defense-in-depth, зеркалит Get/Update/Delete): well-formed-но-
# отсутствующий id → repo.Get NotFound → sync 404 (grpc code 5) ДО создания
# Operation. Никакого async-op не заводится → orphan unregister-intent тривиально
# отсутствует (нечему записаться). Тот же путь, что get-after-delete happy-кейса.
# ---------------------------------------------------------------------------

# ДВЕ ЗАЩИТИМЫЕ ЛИНИИ, И КАЖДАЯ ПРОВЕРЯЕТСЯ ПО СУЩЕСТВУ — это не ослабление.
#
# Под ПРОЕКТНЫМ актором (а не бутстрап-админом, см. #72) край короткозамыкает
# раньше сервиса: `scope_extractor` не может резолвить target→project для
# несуществующего объекта, поэтому анти-BOLA отвечает fail-closed 403 ДО того, как
# запрос дойдёт до `repo.Get`. Наблюдалось прогоном: код 7 и
# `"no authorization path to the resource"`.
#
# Прежняя редакция требовала строго 404 и была верна ровно для привилегированного
# вызывающего, который видит всё. Толерантность здесь перечисляет РАЗНЫЕ ЛИНИИ, у
# каждой свой производитель, — а не разные исходы одной линии (`testing.md`
# §e2e-инварианты прямо предписывает эту форму и объясняет, почему authz-отказ на
# недоступный объект защитим).
#
# Взаимоисключающих исходов тут нет: обе ветки означают ОТКАЗ, и внутри каждой
# утверждение остаётся полным — код gRPC и предмет сообщения. Пустить `oneOf` без
# разбора по ветке значило бы перестать утверждать вовсе.
CASES.append(Case(
    id="SECD-DEL-NEG-NOT-FOUND",
    title="SEC-D: Delete несуществующего instance → отвергнут (404 own-полосой либо authz-first 403 на крае); Operation не заводится, orphan unregister-intent не пишется",
    classes=["NEG"], priority="P2",
    steps=[
        Step(name="delete-missing", method="DELETE", path=INSTANCES + "/ins-00000000000000000",
             test_script=[
                 "pm.test('rejected: 404 own-lane or authz-first 403', () => "
                 "pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
                 "if (pm.response.code === 404) {",
                 "  pm.test('grpc code 5 (NOT_FOUND)', () => pm.expect(pm.response.json().code).to.eql(5));",
                 # Текст владельца целиком (services/compute/internal/repo/instance_repo.go),
                 # а не общая часть «not found»: её несут 32 разных отказа compute — про
                 # ключ доступа, про группу размещения, — и подмену одного отказа другим
                 # шаг не различал (#1520). Ветка 403 текста не утверждает намеренно:
                 # там отвечает край, и его отказ пинится своим утверждением ниже.
                 "  pm.test('text carries the owner verbatim tone', () => "
                 "pm.expect(pm.response.json().message || '', pm.response.text())"
                 ".to.have.string('Instance ins-00000000000000000 not found'));",
                 "} else {",
                 "  pm.test('grpc code 7 (PERMISSION_DENIED)', () => pm.expect(pm.response.json().code).to.eql(7));",
                 "  pm.test('authz-first: no path to the resource', () => "
                 "pm.expect(JSON.stringify(pm.response.json()).toLowerCase()).to.include('authorization path'));",
                 "}",
             ]),
    ],
))


# requires: kacho-vpc peer down — SEC-D-23 (cross-service NIC IPAM мутация при
# недоступном owner → Operation error UNAVAILABLE). Это синхронная cross-service
# ref-validation на request-path (НЕ FGA-tuple-path, который асинхронный через
# outbox). Гоняется в dedicated chaos-профиле, не в обычном regression-проходе.

# requires: per-edge mTLS mismatch — SEC-D-21 (vpc-client mTLS-on, iam-listener
# mTLS-off → register-drainer вызов завершается UNAVAILABLE, register-intent
# остаётся durable). Требует TLS-flag-управления стендом (SEC-F PKI); dedicated
# mTLS-профиль.
