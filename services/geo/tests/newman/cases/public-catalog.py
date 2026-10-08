# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: административные глаголы каталога размещения на ПУБЛИЧНЫХ
RegionService/ZoneService (приёмка sub-phase-ADM-1-geo-placement-catalog-admin,
kacho#3092; сценарии 01–07, 09, 10, 12, 13, 16 и по одному кейсу отображения на
каждый код отказа — DoD п.4).

Все запросы — на ВНЕШНИЙ слушатель ({{baseUrl}}), если шаг не помечен
`internal=True`. Субъекты: `admin` — коллекционный jwtBootstrap (system_admin @
cluster); `tenant` — jwtPureNoBindings (без кластерных кортежей, кроме пола
system_viewer для чтения справочника); `anon` — без принципала.

Мутации каталога завершаются в самом ответе (Operation done=true): успех и отказ
хранилища решает `error` внутри 200-конверта (assert_operation_envelope /
assert_operation_failed), опрос операции не нужен и не утверждается.

Синхронные отказы формы входа (сценарии 08, 11) утверждает integration-проба
службы обеими сторонами каждой оси
(services/geo/internal/repo/kacho/pg/public_catalog_integration_test.go); здесь —
один кейс отображения INVALID_ARGUMENT → 400 (e2e-flow no-reassert-unit-property).

Конструируемость: каждый кейс заводит СВОИ регион и зону с суффиксом {{runId}} и
снимает их последними шагами — шаги снятия исполняются и после упавшего
утверждения (прогон без --bail), поэтому мусора кейс не оставляет. Базовый
каталог стенда (ru-central1) предметом мутаций не является; он лишь адресуется
отказами анонима, которые до службы не доходят.
"""

CASES = []

TENANT = "jwtPureNoBindings"


def _region(rid, status="UP", country=None):
    body = {"id": rid, "status": status}
    if country:
        body["countryCode"] = country
    return body


def _create_region(name, rid, status="UP", country=None):
    return Step(name=name, method="POST", path="/geo/v1/regions",
                body=_region(rid, status, country),
                test_script=[*assert_operation_envelope()])


def _create_zone(name, zid, rid, status="UP"):
    return Step(name=name, method="POST", path="/geo/v1/zones",
                body={"id": zid, "regionId": rid, "status": status},
                test_script=[*assert_operation_envelope()])


def _delete(name, path):
    return Step(name=name, method="DELETE", path=path, test_script=[*assert_operation_envelope()])


def _forbidden(name, method, path, body=None):
    return Step(name=name, method=method, path=path, auth=TENANT, body=body,
                test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED")])


def _op_error_message(template):
    """Текст отказа хранилища в Operation.error — равенством, с подстановкой runId."""
    js = "'" + template.replace("{{runId}}", "' + pm.environment.get('runId') + '") + "'"
    return [
        "pm.test('Operation.error.message is the owner verbatim tone', () => {",
        "  const j = pm.response.json();",
        f"  pm.expect((j.error || {{}}).message, JSON.stringify(j.error)).to.eql({js});",
        "});",
    ]


def _no_keys(expr, *keys):
    lines = [f"pm.test({js_str('public body carries no ' + '/'.join(keys))}, () => {{",
             f"  const o = {expr} || {{}};"]
    for k in keys:
        lines.append(f"  pm.expect(Object.keys(o), JSON.stringify(o)).to.not.include({js_str(k)});")
    lines.append("});")
    return lines


# ---------------------------------------------------------------------------
# ADM-1-GEO-01 — регион создаётся администратором; арендатору — отказ, и региона нет.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-01",
    title="POST /geo/v1/regions: tenant → 403 and no region; admin → 200 Operation done, region readable by tenant",
    classes=["CRUD", "AUTHZ"], priority="P0",
    steps=[
        _forbidden("tenant-create", "POST", "/geo/v1/regions", _region("adm1g-c1-{{runId}}", country="RU")),
        Step(name="tenant-get-absent", method="GET", path="/geo/v1/regions/adm1g-c1-{{runId}}", auth=TENANT,
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        Step(name="admin-create", method="POST", path="/geo/v1/regions",
             body=_region("adm1g-c1-{{runId}}", country="RU"),
             test_script=[
                 *assert_operation_envelope(),
                 "pm.test('metadata.regionId and response.id name the region', () => {",
                 "  const j = pm.response.json(); const want = 'adm1g-c1-' + pm.environment.get('runId');",
                 "  pm.expect(j.metadata.regionId, JSON.stringify(j.metadata)).to.eql(want);",
                 "  pm.expect((j.response || {}).id, JSON.stringify(j.response)).to.eql(want);",
                 "});",
             ]),
        retry_get_until_found(Step(name="tenant-get", method="GET", path="/geo/v1/regions/adm1g-c1-{{runId}}",
             auth=TENANT,
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('countryCode RU', () => pm.expect(j.countryCode).to.eql('RU'));",
                 "pm.test('openForPlacement true', () => pm.expect(j.openForPlacement).to.eql(true));",
                 "pm.test('createdAt non-empty', () => pm.expect(String(j.createdAt || '')).to.not.eql(''));",
             ])),
        _delete("cleanup-region", "/geo/v1/regions/adm1g-c1-{{runId}}"),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-02 — зона создаётся администратором в регионе; арендатору — отказ.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-02",
    title="POST /geo/v1/zones: tenant → 403 and no zone; admin → 200 Operation done, zone open NONE",
    classes=["CRUD", "AUTHZ"], priority="P0",
    steps=[
        _create_region("admin-create-region", "adm1g-c2-{{runId}}"),
        _forbidden("tenant-create-zone", "POST", "/geo/v1/zones",
                   {"id": "adm1g-c2-{{runId}}-a", "regionId": "adm1g-c2-{{runId}}", "status": "UP"}),
        Step(name="get-zone-absent", method="GET", path="/geo/v1/zones/adm1g-c2-{{runId}}-a",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        Step(name="admin-create-zone", method="POST", path="/geo/v1/zones",
             body={"id": "adm1g-c2-{{runId}}-a", "regionId": "adm1g-c2-{{runId}}", "status": "UP"},
             test_script=[
                 *assert_operation_envelope(),
                 "pm.test('response.id names the zone', () => pm.expect((pm.response.json().response || {}).id)"
                 ".to.eql('adm1g-c2-' + pm.environment.get('runId') + '-a'));",
             ]),
        retry_get_until_found(Step(name="tenant-get-zone", method="GET", path="/geo/v1/zones/adm1g-c2-{{runId}}-a",
             auth=TENANT,
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('regionId', () => pm.expect(j.regionId).to.eql('adm1g-c2-' + pm.environment.get('runId')));",
                 "pm.test('openForPlacement true', () => pm.expect(j.openForPlacement).to.eql(true));",
                 "pm.test('placementBlockedReason NONE', () => pm.expect(j.placementBlockedReason).to.eql('NONE'));",
             ])),
        _delete("cleanup-zone", "/geo/v1/zones/adm1g-c2-{{runId}}-a"),
        _delete("cleanup-region", "/geo/v1/regions/adm1g-c2-{{runId}}"),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-03 — правка статуса региона видна в обеих проекциях.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-03",
    title="PATCH /geo/v1/regions/{id} status: tenant → 403 unchanged; admin DOWN → zone REGION_DOWN; UP → NONE",
    classes=["CRUD", "AUTHZ"], priority="P0",
    steps=[
        _create_region("create-region", "adm1g-c3-{{runId}}"),
        _create_zone("create-zone", "adm1g-c3-{{runId}}-a", "adm1g-c3-{{runId}}"),
        _forbidden("tenant-patch", "PATCH", "/geo/v1/regions/adm1g-c3-{{runId}}",
                   {"status": "DOWN", "updateMask": "status"}),
        Step(name="get-region-unchanged", method="GET", path="/geo/v1/regions/adm1g-c3-{{runId}}",
             test_script=[*assert_status(200),
                          "pm.test('openForPlacement still true', () => pm.expect(pm.response.json().openForPlacement).to.eql(true));"]),
        Step(name="admin-patch-down", method="PATCH", path="/geo/v1/regions/adm1g-c3-{{runId}}",
             body={"status": "DOWN", "updateMask": "status"},
             test_script=[*assert_operation_envelope(),
                          "pm.test('response.openForPlacement false', () => pm.expect((pm.response.json().response || {}).openForPlacement).to.not.eql(true));"]),
        Step(name="get-zone-region-down", method="GET", path="/geo/v1/zones/adm1g-c3-{{runId}}-a",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('zone closed', () => pm.expect(j.openForPlacement).to.not.eql(true));",
                          "pm.test('reason REGION_DOWN', () => pm.expect(j.placementBlockedReason).to.eql('REGION_DOWN'));"]),
        Step(name="admin-patch-up", method="PATCH", path="/geo/v1/regions/adm1g-c3-{{runId}}",
             body={"status": "UP", "updateMask": "status"},
             test_script=[*assert_operation_envelope()]),
        Step(name="get-zone-none", method="GET", path="/geo/v1/zones/adm1g-c3-{{runId}}-a",
             test_script=[*assert_status(200),
                          "pm.test('reason NONE again', () => pm.expect(pm.response.json().placementBlockedReason).to.eql('NONE'));"]),
        _delete("cleanup-zone", "/geo/v1/zones/adm1g-c3-{{runId}}-a"),
        _delete("cleanup-region", "/geo/v1/regions/adm1g-c3-{{runId}}"),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-04 — правка статуса зоны.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-04",
    title="PATCH /geo/v1/zones/{id} status: tenant → 403 still NONE; admin DOWN → ZONE_DOWN",
    classes=["CRUD", "AUTHZ"], priority="P0",
    steps=[
        _create_region("create-region", "adm1g-c4-{{runId}}"),
        _create_zone("create-zone", "adm1g-c4-{{runId}}-a", "adm1g-c4-{{runId}}"),
        _forbidden("tenant-patch-zone", "PATCH", "/geo/v1/zones/adm1g-c4-{{runId}}-a",
                   {"status": "DOWN", "updateMask": "status"}),
        Step(name="get-zone-still-none", method="GET", path="/geo/v1/zones/adm1g-c4-{{runId}}-a",
             test_script=[*assert_status(200),
                          "pm.test('reason NONE', () => pm.expect(pm.response.json().placementBlockedReason).to.eql('NONE'));"]),
        Step(name="admin-patch-zone", method="PATCH", path="/geo/v1/zones/adm1g-c4-{{runId}}-a",
             body={"status": "DOWN", "updateMask": "status"},
             test_script=[*assert_operation_envelope()]),
        Step(name="get-zone-down", method="GET", path="/geo/v1/zones/adm1g-c4-{{runId}}-a",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('zone closed', () => pm.expect(j.openForPlacement).to.not.eql(true));",
                          "pm.test('reason ZONE_DOWN', () => pm.expect(j.placementBlockedReason).to.eql('ZONE_DOWN'));"]),
        _delete("cleanup-zone", "/geo/v1/zones/adm1g-c4-{{runId}}-a"),
        _delete("cleanup-region", "/geo/v1/regions/adm1g-c4-{{runId}}"),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-05 — удаление зоны и региона.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-05",
    title="DELETE zone/region: tenant → 403 zone stays; admin → done; reads → 404 verbatim",
    classes=["CRUD", "AUTHZ"], priority="P0",
    steps=[
        _create_region("create-region", "adm1g-c5-{{runId}}"),
        _create_zone("create-zone", "adm1g-c5-{{runId}}-a", "adm1g-c5-{{runId}}"),
        _forbidden("tenant-delete-zone", "DELETE", "/geo/v1/zones/adm1g-c5-{{runId}}-a"),
        Step(name="get-zone-stays", method="GET", path="/geo/v1/zones/adm1g-c5-{{runId}}-a",
             test_script=[*assert_status(200)]),
        _delete("admin-delete-zone", "/geo/v1/zones/adm1g-c5-{{runId}}-a"),
        _delete("admin-delete-region", "/geo/v1/regions/adm1g-c5-{{runId}}"),
        Step(name="get-zone-gone", method="GET", path="/geo/v1/zones/adm1g-c5-{{runId}}-a",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('verbatim NotFound text', () => pm.expect(pm.response.json().message)"
                          ".to.eql('Zone adm1g-c5-' + pm.environment.get('runId') + '-a not found'));"]),
        Step(name="get-region-gone", method="GET", path="/geo/v1/regions/adm1g-c5-{{runId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-06 — регион с зонами не удаляется; без зон — удаляется.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-06",
    title="DELETE region with a zone → Operation.error 9 'region <id> is not empty', region stays; without zones → deleted",
    classes=["NEG", "CRUD"], priority="P1",
    steps=[
        _create_region("create-region", "adm1g-ne-{{runId}}"),
        _create_zone("create-zone", "adm1g-ne-{{runId}}-a", "adm1g-ne-{{runId}}"),
        Step(name="delete-non-empty", method="DELETE", path="/geo/v1/regions/adm1g-ne-{{runId}}",
             test_script=[*assert_operation_failed(9, "FAILED_PRECONDITION"),
                          *_op_error_message("region adm1g-ne-{{runId}} is not empty")]),
        Step(name="get-region-stays", method="GET", path="/geo/v1/regions/adm1g-ne-{{runId}}",
             test_script=[*assert_status(200)]),
        _delete("delete-zone", "/geo/v1/zones/adm1g-ne-{{runId}}-a"),
        _delete("delete-region", "/geo/v1/regions/adm1g-ne-{{runId}}"),
        Step(name="get-region-gone", method="GET", path="/geo/v1/regions/adm1g-ne-{{runId}}",
             test_script=[*assert_status(404)]),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-07 — повторное создание того же id.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-07",
    title="POST /geo/v1/regions twice with the same id → second Operation.error 6 'Region <id> already exists'",
    classes=["NEG", "IDM"], priority="P1",
    steps=[
        _create_region("create-first", "adm1g-dup-{{runId}}"),
        Step(name="create-dup", method="POST", path="/geo/v1/regions", body=_region("adm1g-dup-{{runId}}"),
             test_script=[*assert_operation_failed(6, "ALREADY_EXISTS"),
                          *_op_error_message("Region adm1g-dup-{{runId}} already exists")]),
        _delete("cleanup-region", "/geo/v1/regions/adm1g-dup-{{runId}}"),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-08 — отображение INVALID_ARGUMENT → 400 (одна нога; обе стороны оси —
# integration-проба службы).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-08",
    title="POST /geo/v1/regions with id 'Bad_Id' → sync 400 INVALID_ARGUMENT \"invalid region id 'Bad_Id'\"",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="create-malformed", method="POST", path="/geo/v1/regions", body={"id": "Bad_Id"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          *assert_refusal_message("invalid region id 'Bad_Id'")]),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-09 — зона в несуществующем регионе.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-09",
    title="POST /geo/v1/zones in a missing region → Operation.error 9 'violates a reference constraint'; twin passes once the region exists",
    classes=["NEG", "CRUD"], priority="P1",
    steps=[
        Step(name="create-zone-ghost", method="POST", path="/geo/v1/zones",
             body={"id": "adm1g-gh-{{runId}}-a", "regionId": "adm1g-gh-{{runId}}", "status": "UP"},
             test_script=[*assert_operation_failed(9, "FAILED_PRECONDITION"),
                          *_op_error_message("Zone adm1g-gh-{{runId}}-a violates a reference constraint")]),
        Step(name="get-zone-absent", method="GET", path="/geo/v1/zones/adm1g-gh-{{runId}}-a",
             test_script=[*assert_status(404)]),
        _create_region("create-region", "adm1g-gh-{{runId}}"),
        _create_zone("create-zone-twin", "adm1g-gh-{{runId}}-a", "adm1g-gh-{{runId}}"),
        _delete("cleanup-zone", "/geo/v1/zones/adm1g-gh-{{runId}}-a"),
        _delete("cleanup-region", "/geo/v1/regions/adm1g-gh-{{runId}}"),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-10 — правка несуществующего региона.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-10",
    title="PATCH a missing region → Operation.error 5 'Region <id> not found'; twin on an existing region passes",
    classes=["NEG", "CRUD"], priority="P1",
    steps=[
        Step(name="patch-missing", method="PATCH", path="/geo/v1/regions/adm1g-none-{{runId}}",
             body={"status": "UP", "updateMask": "status"},
             test_script=[*assert_operation_failed(5, "NOT_FOUND"),
                          *_op_error_message("Region adm1g-none-{{runId}} not found")]),
        _create_region("create-twin", "adm1g-some-{{runId}}", status="DOWN"),
        Step(name="patch-existing", method="PATCH", path="/geo/v1/regions/adm1g-some-{{runId}}",
             body={"status": "UP", "updateMask": "status"},
             test_script=[*assert_operation_envelope()]),
        _delete("cleanup-region", "/geo/v1/regions/adm1g-some-{{runId}}"),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-12 — внутренние пути снаружи не обслуживаются, изнутри — обслуживаются.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-12",
    title="POST /geo/v1/internal/{regions,zones} on the external listener → 404 edge route miss; on the internal listener → accepted",
    classes=["NEG", "AUTHZ"], priority="P0",
    steps=[
        Step(name="ext-internal-regions", method="POST", path="/geo/v1/internal/regions",
             test_script=[*assert_edge_route_miss("path")]),
        Step(name="ext-internal-zones", method="POST", path="/geo/v1/internal/zones",
             test_script=[*assert_edge_route_miss("path")]),
        Step(name="int-create-region", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "adm1g-int-{{runId}}"},
             test_script=[*assert_operation_envelope()]),
        Step(name="int-cleanup-region", method="DELETE", path="/geo/v1/internal/regions/adm1g-int-{{runId}}",
             internal=True, test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-13 — публичные ответы не несут сырого статуса и infra; полная проекция —
# только внутри.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-13",
    title="public Create response and Get bodies carry no infra/status; internal GetInternal → status DOWN, numericInfraId 0/absent",
    classes=["CONF", "SEC"], priority="P0",
    steps=[
        Step(name="create-region", method="POST", path="/geo/v1/regions", body=_region("adm1g-pj-{{runId}}"),
             test_script=[*assert_operation_envelope(),
                          *_no_keys("pm.response.json().response", "infra", "status"),
                          *assert_no_infra_fields("pm.response.json().response")]),
        Step(name="create-zone", method="POST", path="/geo/v1/zones",
             body={"id": "adm1g-pj-{{runId}}-a", "regionId": "adm1g-pj-{{runId}}", "status": "DOWN"},
             test_script=[*assert_operation_envelope(),
                          *_no_keys("pm.response.json().response", "infra", "status")]),
        Step(name="get-region", method="GET", path="/geo/v1/regions/adm1g-pj-{{runId}}",
             test_script=[*assert_status(200), *_no_keys("pm.response.json()", "infra", "status"),
                          *assert_no_infra_fields()]),
        Step(name="get-zone", method="GET", path="/geo/v1/zones/adm1g-pj-{{runId}}-a",
             test_script=[*assert_status(200), *_no_keys("pm.response.json()", "infra", "status"),
                          *assert_no_infra_fields()]),
        Step(name="int-get-zone", method="GET", path="/geo/v1/internal/zones/adm1g-pj-{{runId}}-a", internal=True,
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('raw status DOWN on the internal projection', () => pm.expect(j.status).to.eql('DOWN'));",
                          "pm.test('numericInfraId absent or 0 (public create assigns none)', () => {",
                          "  const v = (j.infra || {}).numericInfraId;",
                          "  pm.expect(v === undefined || String(v) === '0', JSON.stringify(j.infra)).to.eql(true);",
                          "});"]),
        _delete("cleanup-zone", "/geo/v1/zones/adm1g-pj-{{runId}}-a"),
        _delete("cleanup-region", "/geo/v1/regions/adm1g-pj-{{runId}}"),
    ],
))


# ---------------------------------------------------------------------------
# ADM-1-GEO-16 — аноним не проходит; аутентифицированный — проходит до проверки права.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ADM-1-GEO-16",
    title="anonymous POST/PATCH/DELETE on public geo admin paths → 401; tenant → 403; admin → accepted",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="anon-create", method="POST", path="/geo/v1/regions", auth="anonymous",
             body=_region("adm1g-an-{{runId}}"),
             test_script=[*assert_status(401), *assert_grpc_code(16, "UNAUTHENTICATED")]),
        Step(name="anon-patch", method="PATCH", path="/geo/v1/regions/ru-central1", auth="anonymous",
             body={"status": "DOWN", "updateMask": "status"},
             test_script=[*assert_status(401), *assert_grpc_code(16, "UNAUTHENTICATED")]),
        Step(name="anon-delete-zone", method="DELETE", path="/geo/v1/zones/ru-central1-a", auth="anonymous",
             test_script=[*assert_status(401), *assert_grpc_code(16, "UNAUTHENTICATED")]),
        _forbidden("tenant-create", "POST", "/geo/v1/regions", _region("adm1g-an-{{runId}}")),
        _create_region("admin-create", "adm1g-an-{{runId}}"),
        _delete("cleanup-region", "/geo/v1/regions/adm1g-an-{{runId}}"),
    ],
))
