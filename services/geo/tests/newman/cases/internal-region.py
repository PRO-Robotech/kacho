# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo InternalRegionService admin CRUD (Create/Update/Delete).

Admin-only поверхность каталога регионов, живёт ТОЛЬКО на cluster-internal REST
listener ({{internalBaseUrl}}, :8081) под самоописываемым сегментом
`/geo/v1/internal/regions` (GEO-1 F5; proto google.api.http; gateway restmux
geoInternalAddr) — на публичном {{baseUrl}} её нет by design (ban #6; guard —
admin-not-on-public.py). Гейтится `system_admin`@cluster (jwtBootstrap несёт его;
non-admin deny — authz-deny.py; scope_extractor object_type:cluster).

Синхронно-завершённая Operation (GEO-1 F5, module-geo rule 4): admin-мутации →
`Operation{done:true}` немедленно (config-INSERT без саги; syncop.Commit/Fail);
`metadata` анмаршалится в CreateRegionMetadata{regionId} (id доступен сразу),
`result.response` — полное тело public Region. Клиент разворачивает `.response` —
op-poll не требуется. Здесь материализацию happy-path дополнительно подтверждаем
ПУБЛИЧНЫМ RegionService.Get (retry_get_until_found покрывает любой replica-lag).

DB-detected негативы (dup id PK 23505, delete-non-empty FK RESTRICT 23P01/23503)
приземляются в `Operation.error` (done:true, HTTP 200) — НЕ sync gRPC-ошибка;
sync-4xx отдаёт только pre-write валидация (malformed id, name-required,
countryCode, immutable). Точный op.error-код тут не читаем на wire как отдельный
контракт (envelope-lock), поэтому инвариант проверяем ПО SIDE-EFFECT (public read):
«phantom не создан» / «регион пережил delete под FK RESTRICT».

Источник контракта: proto/kacho/cloud/geo/v1/internal_catalog_service.proto (F5,
GEO-1-16/18/19) + services/geo/internal/{handler/internal.go, apps/kacho/api/region,
apps/kacho/shared/syncop, domain/geo.go}. Сверено с api-conventions.md (мутации →
Operation, sync-reject malformed первым стейтментом, коды) и GEO-1 acceptance.
Test-design: CRUD (create/update/delete happy), VAL (malformed/empty id sync-reject,
GEO-1-19), NEG (dup no-phantom, delete-non-empty RESTRICT GEO-1-18). Каждый positive
→ matched negative. self-contained: ресурсы {{runId}}-суффиксированы + cleanup.
"""

CASES = []


# ---------------------------------------------------------------------------
# IRG-CR-CRUD-OK — Create region → Operation{done:true} envelope; confirm via public Get.
# verifies GEO-1-16
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-CR-CRUD-OK",
    title="InternalRegionService.Create → 200 Operation{geo-id, metadata}; region materializes (public Get)",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-reg-crud-{{runId}}", "name": "QA Region CRUD {{runId}}", "countryCode": "RU", "status": "UP"},
             test_script=[
                 *assert_operation_envelope(),
                 *save_from_response("j.metadata && j.metadata.regionId", "irgCrudRegionId"),
             ]),
        retry_get_until_found(Step(name="get-region", method="GET",
             path="/geo/v1/regions/qa-reg-crud-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('region id materialized', () => pm.expect(j.id).to.eql('qa-reg-crud-' + pm.environment.get('runId')));",
                 "pm.test('name persisted', () => pm.expect(j.name).to.eql('QA Region CRUD ' + pm.environment.get('runId')));",
                 "pm.test('countryCode persisted', () => pm.expect(j.countryCode).to.eql('RU'));",
                 "pm.test('openForPlacement true (region status UP)', () => pm.expect(j.openForPlacement).to.eql(true));",
             ])),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/internal/regions/qa-reg-crud-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IRG-CR-VAL-MALFORMED-ID — malformed slug id → SYNC InvalidArgument (no Operation).
# verifies GEO-1-19
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-CR-VAL-MALFORMED-ID",
    title="Create region with malformed (non-slug) id → sync 400 InvalidArgument (no Operation row written)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="create-malformed", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "9-Bad_Id!", "name": "QA Malformed"},
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message: invalid region id', () => pm.expect(String(pm.response.json().message)).to.include('invalid region id'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# IRG-CR-VAL-EMPTY-ID — empty id → SYNC InvalidArgument "invalid region id ''".
# domain.ValidateID rejects empty id first statement (api-conventions malformed-id).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-CR-VAL-EMPTY-ID",
    title="Create region with empty id → sync 400 InvalidArgument \"invalid region id ''\"",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="create-empty-id", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "", "name": "QA Empty Id"},
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message: invalid region id (empty)', () => pm.expect(String(pm.response.json().message)).to.include('invalid region id'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# IRG-CR-NEG-DUP-INVARIANT — duplicate id (PK) → no phantom (region stays single).
# The dup Create lands in Operation.error (done:true, ALREADY_EXISTS 23505) — HTTP 200
# envelope. We assert the observable INVARIANT: after two Creates of the same id, the
# region still resolves once via public Get (the second Create's PK conflict produced
# no second row and no phantom deletion of the first).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-CR-NEG-DUP-INVARIANT",
    title="Create duplicate region id twice → no phantom; region still resolves once (dup → Operation.error ALREADY_EXISTS)",
    classes=["NEG", "IDM"], priority="P1",
    steps=[
        Step(name="create-first", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-reg-dup-{{runId}}", "name": "QA Region Dup {{runId}}"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="get-after-first", method="GET",
             path="/geo/v1/regions/qa-reg-dup-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-dup", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-reg-dup-{{runId}}", "name": "QA Region Dup Again {{runId}}"},
             test_script=[*assert_operation_envelope()]),
        Step(name="get-still-present", method="GET", path="/geo/v1/regions/qa-reg-dup-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('region survived dup Create (no phantom delete)', () => pm.expect(pm.response.json().id).to.eql('qa-reg-dup-' + pm.environment.get('runId')));",
             ]),
        Step(name="cleanup", method="DELETE", path="/geo/v1/internal/regions/qa-reg-dup-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IRG-DEL-NEG-HASZONES-INVARIANT — Delete a region that owns a zone → FK RESTRICT
# keeps the region (within-service invariant, DB-level). The FAILED_PRECONDITION
# lands in Operation.error (done:true, HTTP 200) → assert the observable side-effect:
# after the delete attempt, the region STILL resolves via public Get.
# verifies GEO-1-18
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-DEL-NEG-HASZONES-INVARIANT",
    title="Delete region that owns a zone → FK RESTRICT keeps it; region still resolves (Operation.error FAILED_PRECONDITION)",
    classes=["NEG", "STATE"], priority="P0",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-reg-del-{{runId}}", "name": "QA Region Del {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-reg-del-{{runId}}",
             test_script=[*assert_status(200)])),
        # zone id MUST be coupling-valid: zone.id == regionId + "-" + <suffix> (GEO-1-29).
        Step(name="create-child-zone", method="POST", path="/geo/v1/internal/zones", internal=True,
             body={"id": "qa-reg-del-{{runId}}-z", "regionId": "qa-reg-del-{{runId}}",
                   "name": "QA Child Zone {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-zone", method="GET",
             path="/geo/v1/zones/qa-reg-del-{{runId}}-z",
             test_script=[*assert_status(200)])),
        Step(name="delete-region-with-zone", method="DELETE", path="/geo/v1/internal/regions/qa-reg-del-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="region-survived", method="GET", path="/geo/v1/regions/qa-reg-del-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('region survived delete (FK RESTRICT held)', () => pm.expect(pm.response.json().id).to.eql('qa-reg-del-' + pm.environment.get('runId')));",
             ]),
        # cleanup — remove child zone first, then the region (order = FK-safe).
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/internal/zones/qa-reg-del-{{runId}}-z", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/internal/regions/qa-reg-del-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IRG-UPD-CRUD-OK — Update region name → Operation envelope; new name via public Get.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-UPD-CRUD-OK",
    title="InternalRegionService.Update name → 200 Operation; new name materializes (public Get)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-reg-upd-{{runId}}", "name": "QA Region Before {{runId}}"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-created", method="GET",
             path="/geo/v1/regions/qa-reg-upd-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="update-name", method="PATCH", path="/geo/v1/internal/regions/qa-reg-upd-{{runId}}", internal=True,
             body={"name": "QA Region After {{runId}}", "updateMask": "name"},
             test_script=[*assert_operation_envelope()]),
        # re-read until the Update commit is visible: the name flips from
        # "QA Region Before" to "QA Region After ...". retry on the STALE name via a
        # 200-scoped self-retry (bounded; fail-open at budget → real assert runs).
        Step(name="verify-name", method="GET", path="/geo/v1/regions/qa-reg-upd-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "const want = 'QA Region After ' + pm.environment.get('runId');",
                 "const cur = pm.response.json().name;",
                 "const uc = parseInt(pm.environment.get('_updRetry') || '0', 10);",
                 "if (cur !== want && uc < 20) {",
                 "  pm.environment.set('_updRetry', String(uc + 1));",
                 "  const _d = Date.now(); while (Date.now() - _d < 500) { /* update-commit wait */ }",
                 "  pm.execution.setNextRequest(pm.info.requestName);",
                 "  return;",
                 "}",
                 "pm.environment.unset('_updRetry');",
                 "pm.test('name updated', () => pm.expect(cur).to.eql(want));",
             ]),
        Step(name="cleanup", method="DELETE", path="/geo/v1/internal/regions/qa-reg-upd-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))
