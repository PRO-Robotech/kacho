# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для OperationService (kacho-storage) — Get через api-gateway OpsProxy.

Все storage-операции имеют op-root prefix `sop` (общий для Volume/Snapshot,
декаплен от ресурсного префикса vol/snp). api-gateway OpsProxy маршрутизирует
GET /operations/{id} по первым 3 символам id → backend `storage`. Клиент поллит
OperationService.Get(id) до done=true (Watch RPC не существует).
"""

CASES = []

VOL = "/storage/v1/volumes"
_DEF_SIZE = 10737418240  # 10 GiB


def _vol_body(suffix):
    return {"projectId": "{{_suiteFolderId}}", "name": f"vol-op-{suffix}-{{{{runId}}}}",
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
    id="OP-GET-NEG-NOTFOUND-VALID-PREFIX",
    title="Get well-formed-но-нет sop-opId → 404 NOT_FOUND",
    classes=["NEG"], priority="P1",
    # verifies §0.1 (OpsProxy sop-routing → storage backend; well-formed-но-нет → NotFound)
    steps=[Step(name="get-nx", method="GET", path="/operations/{{garbageStorageOpId}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")])],
))

CASES.append(Case(
    id="OP-GET-NEG-UNKNOWN-PREFIX",
    title="Get opId без known 3-char prefix → 400 INVALID_ARGUMENT 'prefix' (OpsProxy отвергает неизвестный префикс)",
    classes=["NEG"], priority="P1",
    # verifies §0.1 (OpsProxy prefix-routing guard — неизвестный префикс отвергается)
    steps=[Step(name="get-garbage-prefix", method="GET", path="/operations/{{garbageId}}",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('mentions prefix', () => pm.expect((pm.response.json().message || '').toLowerCase()).to.include('prefix'));"])],
))
