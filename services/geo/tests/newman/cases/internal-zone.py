# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo InternalZoneService admin CRUD (Create/Update/Delete).

Admin-only, cluster-internal REST listener ({{internalBaseUrl}}) под сегментом
`/geo/v1/internal/zones` (GEO-1 F5; proto google.api.http; gateway restmux
geoInternalAddr), `system_admin`@cluster. Синхронно-завершённая Operation
(done:true немедленно, syncop.Commit/Fail); happy-path материализацию
подтверждаем публичным ZoneService.Get (retry_get_until_found покрывает
replica-lag); DB-detected негатив (ghost-region FK 23503) приземляется в
Operation.error (200) → инвариант «зона не создана» асертим по side-effect
(public Get 404).

GEO-1 landed-контракт (сверено с proto/handler/domain на redesign/integration):
  * Public Zone НЕ несёт сырой `status` (two-projection — вынесен в InternalZone);
    единственный публичный placement-сигнал — derived `openForPlacement°`
    (= zone.status==UP && region.status==UP) + `placementBlockedReason°`
    (zone.status==DOWN⇒ZONE_DOWN; иначе region.status==DOWN⇒REGION_DOWN; иначе NONE).
  * Fresh zone без status → DOWN (fail-safe, GEO-1-12), НЕ UP.
  * Coupling (GEO-1-29): zone.id ДОЛЖЕН быть `regionId + "-" + <suffix>` (строгий
    startsWith(regionId+"-")) — валидируется первым стейтментом ДО FK-резолва.
  * regionId зоны IMMUTABLE после Create (GEO-1-32) — здесь не покрыт update-мутацией.

Источник контракта: internal_catalog_service.proto + zone.proto + geo_common.proto
(GeoStatus/PlacementBlockedReason enums) + services/geo/internal/{handler/internal.go,
apps/kacho/api/zone, domain/geo.go}. Сверено с data-integrity.md (FK RESTRICT
zones→regions), api-conventions.md, GEO-1 acceptance (F2/F3/F4/F8). self-contained.
"""

CASES = []


# ---------------------------------------------------------------------------
# IZN-CR-CRUD-OK — Create zone (region UP + zone UP) → Operation; confirm openForPlacement.
# verifies GEO-1-06
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-CRUD-OK",
    title="InternalZoneService.Create (region UP, zone UP) → 200 Operation; zone materializes openForPlacement=true (public Get)",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-zr-crud-{{runId}}", "name": "QA Zone-Region CRUD {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-zr-crud-{{runId}}",
             test_script=[*assert_status(200)])),
        # coupling: zone.id == regionId + "-a" (GEO-1-29 strict startsWith(regionId+"-")).
        Step(name="create-zone", method="POST", path="/geo/v1/internal/zones", internal=True,
             body={"id": "qa-zr-crud-{{runId}}-a", "regionId": "qa-zr-crud-{{runId}}",
                   "name": "QA Zone CRUD {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="get-zone", method="GET",
             path="/geo/v1/zones/qa-zr-crud-{{runId}}-a",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('zone id materialized', () => pm.expect(j.id).to.eql('qa-zr-crud-' + pm.environment.get('runId') + '-a'));",
                 "pm.test('regionId persisted', () => pm.expect(j.regionId).to.eql('qa-zr-crud-' + pm.environment.get('runId')));",
                 "pm.test('openForPlacement true (zone UP && region UP)', () => pm.expect(j.openForPlacement).to.eql(true));",
                 "pm.test('placementBlockedReason NONE', () => pm.expect(String(j.placementBlockedReason)).to.eql('NONE'));",
                 "pm.test('public Zone carries NO raw status (two-projection)', () => pm.expect(j).to.not.have.property('status'));",
             ])),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/internal/zones/qa-zr-crud-{{runId}}-a", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/internal/regions/qa-zr-crud-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-CR-VAL-MALFORMED-ID — malformed zone id → SYNC InvalidArgument (first statement).
# verifies GEO-1-31
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-VAL-MALFORMED-ID",
    title="Create zone with malformed (non-slug) id → sync 400 InvalidArgument (no Operation)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="create-malformed", method="POST", path="/geo/v1/internal/zones", internal=True,
             body={"id": "9-Bad_Zone!", "regionId": "qa-any", "name": "QA Malformed Zone", "status": "UP"},
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message: invalid zone id', () => pm.expect(String(pm.response.json().message)).to.include('invalid zone id'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-CR-NEG-COUPLING — coupling violated (zone.id not prefixed by its regionId) →
# SYNC InvalidArgument first statement (GEO-1-29, before any FK resolve).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-NEG-COUPLING",
    title="Create zone whose id is not prefixed by its regionId → sync 400 InvalidArgument 'must be prefixed by its regionId'",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="create-uncoupled", method="POST", path="/geo/v1/internal/zones", internal=True,
             body={"id": "qa-other-{{runId}}-a", "regionId": "qa-zr-x-{{runId}}",
                   "name": "QA Uncoupled Zone {{runId}}", "status": "UP"},
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message: must be prefixed by its regionId', () => pm.expect(String(pm.response.json().message)).to.include('must be prefixed by its regionId'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-CR-NEG-GHOST-REGION-INVARIANT — Create a coupling-valid zone whose regionId does
# NOT exist → FK rejects the insert (Operation.error, done:true) → zone NOT created
# (public Get 404). Assert the observable side-effect (within-service FK RESTRICT).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-NEG-GHOST-REGION-INVARIANT",
    title="Create zone with absent (but coupling-valid) regionId → FK rejects; zone not created (public Get 404)",
    classes=["NEG"], priority="P0",
    steps=[
        # coupling holds (id prefixed by regionId), but the region is never created →
        # FK 23503 rejects the write; the async Operation.error carries the failure.
        Step(name="create-ghost", method="POST", path="/geo/v1/internal/zones", internal=True,
             body={"id": "qa-ghost-{{runId}}-a", "regionId": "qa-ghost-{{runId}}",
                   "name": "QA Ghost Zone {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        # The zone is never committed (FK deterministically rejects), so public Get is a
        # stable 404 — no retry (retry_until_found would wrongly wait for a row that never appears).
        Step(name="zone-not-created", method="GET", path="/geo/v1/zones/qa-ghost-{{runId}}-a",
             test_script=[
                 *assert_status(404),
                 *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('ghost-region zone never materialized', () => pm.expect(pm.response.json().message).to.eql('Zone qa-ghost-' + pm.environment.get('runId') + '-a not found'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-CR-CONF-STATUS-DOWN — explicit zone status=DOWN → openForPlacement=false,
# placementBlockedReason=ZONE_DOWN (region UP). verifies GEO-1-08
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-CONF-STATUS-DOWN",
    title="Create zone status=DOWN (region UP) → openForPlacement=false, placementBlockedReason=ZONE_DOWN (public Get)",
    classes=["CONF", "CRUD"], priority="P1",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-zr-down-{{runId}}", "name": "QA Zone-Region Down {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-zr-down-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-zone-down", method="POST", path="/geo/v1/internal/zones", internal=True,
             body={"id": "qa-zr-down-{{runId}}-a", "regionId": "qa-zr-down-{{runId}}",
                   "name": "QA Zone Down {{runId}}", "status": "DOWN"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="get-zone-down", method="GET",
             path="/geo/v1/zones/qa-zr-down-{{runId}}-a",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('openForPlacement false (zone DOWN)', () => pm.expect(j.openForPlacement).to.eql(false));",
                 "pm.test('placementBlockedReason ZONE_DOWN', () => pm.expect(String(j.placementBlockedReason)).to.eql('ZONE_DOWN'));",
             ])),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/internal/zones/qa-zr-down-{{runId}}-a", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/internal/regions/qa-zr-down-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-CR-CONF-STATUS-DEFAULT-DOWN — omitted status coerces to DOWN (GEO-1-12 fail-safe,
# inverted from the pre-redesign UP default). Region UP + zone default DOWN →
# openForPlacement=false, placementBlockedReason=ZONE_DOWN.
# verifies GEO-1-12
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-CR-CONF-STATUS-DEFAULT-DOWN",
    title="Create zone WITHOUT status → coerced to DOWN (fail-safe); openForPlacement=false (public Get)",
    classes=["CONF", "CRUD"], priority="P1",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-zr-dflt-{{runId}}", "name": "QA Zone-Region Dflt {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-zr-dflt-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-zone-nostatus", method="POST", path="/geo/v1/internal/zones", internal=True,
             body={"id": "qa-zr-dflt-{{runId}}-a", "regionId": "qa-zr-dflt-{{runId}}",
                   "name": "QA Zone Default Status {{runId}}"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="get-zone-dflt", method="GET",
             path="/geo/v1/zones/qa-zr-dflt-{{runId}}-a",
             test_script=[
                 *assert_status(200),
                 "pm.test('omitted status → fresh DOWN → openForPlacement false', () => pm.expect(pm.response.json().openForPlacement).to.eql(false));",
             ])),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/internal/zones/qa-zr-dflt-{{runId}}-a", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/internal/regions/qa-zr-dflt-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# IZN-UPD-CRUD-STATUS — Update zone status UP → DOWN via Internal Update;
# openForPlacement flips true → false (public Get). verifies GEO-1-15
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="IZN-UPD-CRUD-STATUS",
    title="InternalZoneService.Update status UP→DOWN → openForPlacement flips true→false (public Get)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-zr-upd-{{runId}}", "name": "QA Zone-Region Upd {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-zr-upd-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-zone-up", method="POST", path="/geo/v1/internal/zones", internal=True,
             body={"id": "qa-zr-upd-{{runId}}-a", "regionId": "qa-zr-upd-{{runId}}",
                   "name": "QA Zone Upd {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-zone-up", method="GET",
             path="/geo/v1/zones/qa-zr-upd-{{runId}}-a",
             test_script=[
                 *assert_status(200),
                 "pm.test('openForPlacement true before update', () => pm.expect(pm.response.json().openForPlacement).to.eql(true));",
             ])),
        Step(name="update-status-down", method="PATCH", path="/geo/v1/internal/zones/qa-zr-upd-{{runId}}-a", internal=True,
             body={"status": "DOWN", "updateMask": "status"},
             test_script=[*assert_operation_envelope()]),
        # re-read until the Update commit flips openForPlacement true -> false (bounded retry
        # over the commit window; fail-open at budget -> real assert runs once).
        Step(name="verify-status-down", method="GET", path="/geo/v1/zones/qa-zr-upd-{{runId}}-a",
             test_script=[
                 *assert_status(200),
                 "const ofp = pm.response.json().openForPlacement;",
                 "const uc = parseInt(pm.environment.get('_znUpdRetry') || '0', 10);",
                 "if (ofp !== false && uc < 20) {",
                 "  pm.environment.set('_znUpdRetry', String(uc + 1));",
                 "  const _d = Date.now(); while (Date.now() - _d < 500) { /* update-commit wait */ }",
                 "  pm.execution.setNextRequest(pm.info.requestName);",
                 "  return;",
                 "}",
                 "pm.environment.unset('_znUpdRetry');",
                 "pm.test('status updated to DOWN → openForPlacement false', () => pm.expect(ofp).to.eql(false));",
             ]),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/internal/zones/qa-zr-upd-{{runId}}-a", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/internal/regions/qa-zr-upd-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))
