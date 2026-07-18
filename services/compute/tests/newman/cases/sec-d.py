# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для SEC-D (kacho-compute): FGA owner-tuple через kacho-iam
(transactional-outbox) + opt-in mTLS.

SEC-D устраняет прямой доступ compute к OpenFGA: на каждый resource Create/Delete
intent owner-tuple пишется строкой в compute_fga_register_outbox В ТОЙ ЖЕ
writer-tx, что и Insert/Delete ресурса; register-drainer применяет его через
InternalIAMService.RegisterResource/UnregisterResource. Публичный контракт
ресурсов НЕ меняется (эпик #8) — эти кейсы гоняют существующие публичные RPC и
проверяют, что after-create per-resource Get резолвится (owner-tuple применён
eventual через IAM), а Delete → Get 404.

Контракт изоляции: каждый case в своём runId, работает внутри pre-allocated
existingProjectId (_suiteFolderId из env); имена суффиксуются {{runId}}.
id-prefix Disk = `epd`.

mTLS-mismatch (SEC-D-21) и cross-service-owner-down (SEC-D-23) — отдельные
негативы, требующие управления инфраструктурой стенда (peer down / per-edge
TLS-flag); помечены `# requires`-аннотацией и гоняются в dedicated профиле, не в
обычном regression-проходе.
"""

CASES = []

DISKS = "/compute/v1/disks"
_DEF_SIZE = 10737418240  # 10 GiB


# ---------------------------------------------------------------------------
# SEC-D-15 — happy: Create → Operation done → Get показывает ресурс (owner-tuple
# применён eventual через IAM register-drainer) → Delete → Get 404.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SECD-CR-GET-AFTER-TUPLE-OK",
    title="SEC-D-15: Create disk → Operation done → Get показывает ресурс (per-resource Check резолвится, owner-tuple применён через IAM) → Delete → Get 404",
    classes=["CONF", "IDM"], priority="P1",
    steps=[
        Step(name="create", method="POST", path=DISKS,
             body={"projectId": "{{_suiteFolderId}}", "name": f"secd-disk-{{{{runId}}}}",
                   "zoneId": "{{existingZoneId}}", "size": _DEF_SIZE,
                   "labels": {"suite": "sec-d"}},
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.diskId", "diskId")]),
        poll_operation_until_done(),
        assert_op_success(),
        # per-resource Get: резолвится → owner-tuple зарегистрирован в IAM (раньше
        # best-effort dual-write мог потерять tuple → DENY навсегда; теперь intent
        # durable + retried, окно DENY конечно).
        retry_until_authorized(Step(name="get", method="GET", path=f"{DISKS}/{{{{diskId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('id matches & epd prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('diskId')); pm.expect(j.id).to.match(/^epd/); });",
                          "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteFolderId')));",
                          "pm.test('status READY', () => pm.expect(j.status).to.eql('READY'));",
                          *assert_created_at_seconds()])),
        Step(name="delete", method="DELETE", path=f"{DISKS}/{{{{diskId}}}}",
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        assert_op_success(),
        # после Delete — Get → 404 (unregister-intent тоже записан в writer-tx).
        # HTTP-статус 404, grpc-код в теле = 5 (NOT_FOUND) — не путать (404 — транспорт).
        Step(name="get-after-delete", method="GET", path=f"{DISKS}/{{{{diskId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
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

CASES.append(Case(
    id="SECD-DEL-NEG-NOT-FOUND",
    title="SEC-D: Delete несуществующего disk → sync 404 NOT_FOUND (ownership pre-check фиксирует отсутствие ДО Operation; orphan unregister-intent не пишется)",
    classes=["NEG"], priority="P2",
    steps=[
        Step(name="delete-missing", method="DELETE", path=f"{DISKS}/epd00000000000000000",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('text mentions not found', () => pm.expect((pm.response.json().message || '').toLowerCase()).to.include('not found'));"]),
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
