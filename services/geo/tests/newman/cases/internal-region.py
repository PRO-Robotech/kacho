# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo InternalRegionService admin CRUD (Create/Update/Delete).

Admin-only поверхность каталога регионов, живёт ТОЛЬКО на cluster-internal REST
listener ({{internalBaseUrl}}, :8081) — на публичном {{baseUrl}} её нет by design
(ban #6; guard — admin-not-on-public.py). Гейтится `system_admin`@cluster
(jwtBootstrap несёт его; non-admin deny — authz-deny.py).

Async-форма (AS-IS): Create/Update/Delete возвращают Operation{done:false}. geo
Operation-id НЕ маршрутизируется api-gateway OpsProxy (PRO-Robotech/kacho#55) →
op-poll даёт 400 'invalid operation id'. Поэтому happy-path НЕ поллит Operation, а
подтверждает МАТЕРИАЛИЗАЦИЮ мутации через ПУБЛИЧНЫЙ RegionService.Get (async-worker
commit window покрыт retry_get_until_found). Явный op-poll RED-lock — operation.py.

Наблюдаемость async-ошибок (dup ALREADY_EXISTS, delete-non-empty FAILED_PRECONDITION)
доставляется ТОЛЬКО через Operation.error → тоже заблокировано #55. Поэтому эти
инварианты асертятся ПО SIDE-EFFECT (наблюдаемо через public read): «phantom не
создан» / «регион пережил delete под FK RESTRICT» — БЕЗ чтения op.error. Точный
код/текст op.error задеферен за #55 (см. docs/RESULTS.md «Deferred behind #55»).

Источник контракта: proto/kacho/cloud/geo/v1/internal_catalog_service.proto +
services/geo/internal/{handler/internal.go, apps/kacho/api/region, repo/kacho/{pg,dberr}}.
Сверено с api-conventions.md (мутации → Operation, sync-reject malformed первым
стейтментом, коды). Test-design: CRUD (create/update/delete happy), VAL (malformed/
empty id sync-reject), NEG (dup no-phantom, delete-non-empty RESTRICT). Каждый
positive → matched negative. self-contained: ресурсы {{runId}}-суффиксированы + cleanup.
"""

CASES = []


# ---------------------------------------------------------------------------
# IRG-CR-CRUD-OK — Create region → Operation envelope; confirm via public Get.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-CR-CRUD-OK",
    title="InternalRegionService.Create → 200 Operation{geo-id, metadata}; region materializes (public Get)",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-reg-crud-{{runId}}", "name": "QA Region CRUD {{runId}}"},
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
             ])),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/regions/qa-reg-crud-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IRG-CR-VAL-MALFORMED-ID — malformed slug id → SYNC InvalidArgument (no Operation).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-CR-VAL-MALFORMED-ID",
    title="Create region with malformed (non-slug) id → sync 400 InvalidArgument (no Operation row written)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="create-malformed", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "9-Bad_Id!", "name": "QA Malformed"},
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message references region id', () => pm.expect(String(pm.response.json().message)).to.include('region id'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# IRG-CR-VAL-EMPTY-ID — empty id → SYNC InvalidArgument 'region id is required'.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-CR-VAL-EMPTY-ID",
    title="Create region with empty id → sync 400 InvalidArgument 'region id is required'",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="create-empty-id", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "", "name": "QA Empty Id"},
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message: region id is required', () => pm.expect(String(pm.response.json().message)).to.include('region id is required'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# IRG-CR-NEG-DUP-INVARIANT — duplicate id (PK) → no phantom (region stays single).
# The ALREADY_EXISTS op.error code is unobservable via op-poll (#55) → we assert the
# observable INVARIANT: after two Creates of the same id, the region still resolves
# once via public Get (the second Create's FK/PK conflict produced no second row and
# no phantom deletion of the first).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-CR-NEG-DUP-INVARIANT",
    title="Create duplicate region id twice → no phantom; region still resolves once (ALREADY_EXISTS deferred behind #55)",
    classes=["NEG", "IDM"], priority="P1",
    steps=[
        Step(name="create-first", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-reg-dup-{{runId}}", "name": "QA Region Dup {{runId}}"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="get-after-first", method="GET",
             path="/geo/v1/regions/qa-reg-dup-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-dup", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-reg-dup-{{runId}}", "name": "QA Region Dup Again"},
             test_script=[*assert_operation_envelope()]),
        Step(name="get-still-present", method="GET", path="/geo/v1/regions/qa-reg-dup-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('region survived dup Create (no phantom delete)', () => pm.expect(pm.response.json().id).to.eql('qa-reg-dup-' + pm.environment.get('runId')));",
             ]),
        Step(name="cleanup", method="DELETE", path="/geo/v1/regions/qa-reg-dup-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IRG-DEL-NEG-HASZONES-INVARIANT — Delete a region that owns a zone → FK RESTRICT
# keeps the region (within-service invariant, DB-level). The FAILED_PRECONDITION
# op.error is unobservable via op-poll (#55) → assert the observable side-effect:
# after the delete attempt, the region STILL resolves via public Get.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IRG-DEL-NEG-HASZONES-INVARIANT",
    title="Delete region that owns a zone → FK RESTRICT keeps it; region still resolves (FAILED_PRECONDITION deferred behind #55)",
    classes=["NEG", "STATE"], priority="P0",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-reg-del-{{runId}}", "name": "QA Region Del {{runId}}"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-reg-del-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-child-zone", method="POST", path="/geo/v1/zones", internal=True,
             body={"id": "qa-zon-del-{{runId}}", "regionId": "qa-reg-del-{{runId}}",
                   "name": "QA Child Zone", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-zone", method="GET",
             path="/geo/v1/zones/qa-zon-del-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="delete-region-with-zone", method="DELETE", path="/geo/v1/regions/qa-reg-del-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="region-survived", method="GET", path="/geo/v1/regions/qa-reg-del-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('region survived delete (FK RESTRICT held)', () => pm.expect(pm.response.json().id).to.eql('qa-reg-del-' + pm.environment.get('runId')));",
             ]),
        # cleanup — remove child zone first, then the region (order = FK-safe).
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/zones/qa-zon-del-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/regions/qa-reg-del-{{runId}}", internal=True,
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
        Step(name="create-region", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-reg-upd-{{runId}}", "name": "QA Region Before"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-created", method="GET",
             path="/geo/v1/regions/qa-reg-upd-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="update-name", method="PATCH", path="/geo/v1/regions/qa-reg-upd-{{runId}}", internal=True,
             body={"name": "QA Region After {{runId}}"},
             test_script=[*assert_operation_envelope()]),
        # re-read until the async Update commit is visible: the name flips from
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
        Step(name="cleanup", method="DELETE", path="/geo/v1/regions/qa-reg-upd-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))
