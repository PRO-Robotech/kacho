# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo InternalZoneService admin CRUD (Create/Update/Delete).

Admin-only, cluster-internal REST listener ({{internalBaseUrl}}), `system_admin`@
cluster. Async Operation form; op-poll blocked by PRO-Robotech/kacho#55 → happy-path
подтверждается публичным ZoneService.Get (retry_get_until_found); async op.error
(ghost-region FAILED_PRECONDITION) асертится по side-effect (зона НЕ создана → public
Get 404), точный op.error код/текст задеферен за #55.

Zone-специфика: `status` (UP/DOWN); AS-IS дефолт омитнутого статуса — **UP**
(zone use-case: `if st==Unspecified { st=Up }`, DB DEFAULT 'UP'). GEO-1 инвертирует
в DOWN, но GEO-1 merge-gated и НЕ приземлён → лочим AS-IS UP (IZN-CR-CONF-STATUS-
DEFAULT-UP). regionId зоны в AS-IS МУТАБЕЛЕН (нет immutable-check, нет coupling-
check) — поэтому кейсов на immutable-regionId/coupling здесь НЕТ (surface GEO-1,
не существует).

Источник контракта: internal_catalog_service.proto + zone.proto + services/geo/
internal/{handler/internal.go, apps/kacho/api/zone, repo/kacho/{pg,dberr}}. Сверено
с data-integrity.md (FK RESTRICT zones→regions) + api-conventions.md. self-contained.
"""

CASES = []


# ---------------------------------------------------------------------------
# IZN-CR-CRUD-OK — Create zone (under a fresh region) → Operation; confirm via Get.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-CRUD-OK",
    title="InternalZoneService.Create → 200 Operation; zone materializes with regionId+status (public Get)",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-zreg-crud-{{runId}}", "name": "QA Zone-Region CRUD"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-zreg-crud-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-zone", method="POST", path="/geo/v1/zones", internal=True,
             body={"id": "qa-zon-crud-{{runId}}", "regionId": "qa-zreg-crud-{{runId}}",
                   "name": "QA Zone CRUD {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="get-zone", method="GET",
             path="/geo/v1/zones/qa-zon-crud-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('zone id materialized', () => pm.expect(j.id).to.eql('qa-zon-crud-' + pm.environment.get('runId')));",
                 "pm.test('regionId persisted', () => pm.expect(j.regionId).to.eql('qa-zreg-crud-' + pm.environment.get('runId')));",
                 "pm.test('status UP', () => pm.expect(String(j.status)).to.include('UP'));",
             ])),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/zones/qa-zon-crud-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/regions/qa-zreg-crud-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-CR-VAL-MALFORMED-ID — malformed zone id → SYNC InvalidArgument.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-VAL-MALFORMED-ID",
    title="Create zone with malformed (non-slug) id → sync 400 InvalidArgument (no Operation)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="create-malformed", method="POST", path="/geo/v1/zones", internal=True,
             body={"id": "9-Bad_Zone!", "regionId": "qa-any", "name": "QA Malformed Zone", "status": "UP"},
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message references zone id', () => pm.expect(String(pm.response.json().message)).to.include('zone id'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-CR-NEG-GHOST-REGION-INVARIANT — Create zone referencing an absent regionId →
# FK rejects the insert → zone NOT created (public Get 404). op.error
# FAILED_PRECONDITION deferred behind #55; here we assert the observable side-effect.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-NEG-GHOST-REGION-INVARIANT",
    title="Create zone with absent regionId → FK rejects; zone not created (public Get 404) (FAILED_PRECONDITION deferred behind #55)",
    classes=["NEG"], priority="P0",
    steps=[
        Step(name="create-ghost", method="POST", path="/geo/v1/zones", internal=True,
             body={"id": "qa-zon-ghost-{{runId}}", "regionId": "ru-ghost-region-{{runId}}",
                   "name": "QA Ghost Zone", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        # The async worker FK-fails (23503) → the zone is never committed. It will NEVER
        # resolve (FK deterministically rejects), so public Get is a stable 404 — no retry
        # (a retry_until_found would wrongly wait for a row that never appears).
        Step(name="zone-not-created", method="GET", path="/geo/v1/zones/qa-zon-ghost-{{runId}}",
             test_script=[
                 *assert_status(404),
                 *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('ghost-region zone never materialized', () => pm.expect(pm.response.json().message).to.eql('Zone qa-zon-ghost-' + pm.environment.get('runId') + ' not found'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-CR-CONF-STATUS-DOWN — explicit status=DOWN persists (public Get shows DOWN).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-CONF-STATUS-DOWN",
    title="Create zone with status=DOWN → persisted DOWN (public Get)",
    classes=["CONF", "CRUD"], priority="P1",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-zreg-down-{{runId}}", "name": "QA Zone-Region Down"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-zreg-down-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-zone-down", method="POST", path="/geo/v1/zones", internal=True,
             body={"id": "qa-zon-down-{{runId}}", "regionId": "qa-zreg-down-{{runId}}",
                   "name": "QA Zone Down", "status": "DOWN"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="get-zone-down", method="GET",
             path="/geo/v1/zones/qa-zon-down-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('status DOWN persisted', () => pm.expect(String(pm.response.json().status)).to.include('DOWN'));",
             ])),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/zones/qa-zon-down-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/regions/qa-zreg-down-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-CR-CONF-STATUS-DEFAULT-UP — omitted status coerces to UP (AS-IS default).
# NOTE: GEO-1 redesign inverts fresh-default to DOWN (fail-safe); it is merge-gated
# and NOT landed, so this case locks the AS-IS default-UP contract (see RESULTS.md).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-CONF-STATUS-DEFAULT-UP",
    title="Create zone WITHOUT status → coerced to UP (AS-IS default; GEO-1 will invert to DOWN, not landed)",
    classes=["CONF", "CRUD"], priority="P1",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-zreg-up-{{runId}}", "name": "QA Zone-Region Up"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-zreg-up-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-zone-nostatus", method="POST", path="/geo/v1/zones", internal=True,
             body={"id": "qa-zon-up-{{runId}}", "regionId": "qa-zreg-up-{{runId}}",
                   "name": "QA Zone Default Status"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="get-zone-up", method="GET",
             path="/geo/v1/zones/qa-zon-up-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('omitted status coerced to UP (AS-IS default)', () => pm.expect(String(pm.response.json().status)).to.include('UP'));",
             ])),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/zones/qa-zon-up-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/regions/qa-zreg-up-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-UPD-CRUD-STATUS — Update zone status UP → DOWN (COALESCE partial update).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-UPD-CRUD-STATUS",
    title="InternalZoneService.Update status UP→DOWN → new status materializes (public Get)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-zreg-updst-{{runId}}", "name": "QA Zone-Region UpdSt"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-zreg-updst-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-zone-up", method="POST", path="/geo/v1/zones", internal=True,
             body={"id": "qa-zon-updst-{{runId}}", "regionId": "qa-zreg-updst-{{runId}}",
                   "name": "QA Zone UpdSt", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-zone-up", method="GET",
             path="/geo/v1/zones/qa-zon-updst-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="update-status-down", method="PATCH", path="/geo/v1/zones/qa-zon-updst-{{runId}}", internal=True,
             body={"status": "DOWN"},
             test_script=[*assert_operation_envelope()]),
        # re-read until the async Update commit flips status UP -> DOWN (bounded retry
        # over the worker-commit window; fail-open at budget -> real assert runs once).
        Step(name="verify-status-down", method="GET", path="/geo/v1/zones/qa-zon-updst-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "const cur = String(pm.response.json().status);",
                 "const uc = parseInt(pm.environment.get('_znUpdRetry') || '0', 10);",
                 "if (cur.indexOf('DOWN') === -1 && uc < 20) {",
                 "  pm.environment.set('_znUpdRetry', String(uc + 1));",
                 "  const _d = Date.now(); while (Date.now() - _d < 500) { /* update-commit wait */ }",
                 "  pm.execution.setNextRequest(pm.info.requestName);",
                 "  return;",
                 "}",
                 "pm.environment.unset('_znUpdRetry');",
                 "pm.test('status updated to DOWN', () => pm.expect(cur).to.include('DOWN'));",
             ]),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/zones/qa-zon-updst-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/regions/qa-zreg-updst-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))
