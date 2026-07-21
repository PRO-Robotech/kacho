# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: ban #6 guard — Internal* admin verbs NOT reachable on the public endpoint.

InternalRegionService/InternalZoneService (admin CRUD) живут ТОЛЬКО на cluster-
internal REST listener ({{internalBaseUrl}}) под самоописываемым сегментом
`/geo/v1/internal/…` (GEO-1 F5; gateway restmux geoInternalAddr). Публичный
{{baseUrl}} их НЕ несёт by design (security.md §Internal-vs-external, ban #6 —
Internal.* не публикуется на external endpoint). Публичный mux регистрирует лишь
RegionService/ZoneService (GET), поэтому POST на публичный endpoint не имеет
мутирующего маршрута.

Каждый кейс — контрастная пара (ban #6 proof): та же мутация ОТВЕРГНута на public
(routing-miss: 403/404/405/501, НЕ 200) и ПРИНЯТА на internal `/geo/v1/internal/…`
(200 Operation envelope). Это ловит регресс, при котором Internal*-route случайно
засветится на external mux. Источник: gateway/internal/restmux/mux.go (geo public
vs geoInternal) + geo_test.go (TestGeo_S5_InternalPathsRejectedOnExternal).

Test-design: NEG (public routing-miss), CRUD-контраст (internal accepts). self-
contained: internal-created ресурс {{runId}}-суффиксирован + cleanup.
"""

CASES = []


CASES.append(Case(
    id="ANP-REG-CR-NOT-PUBLIC",
    title="InternalRegionService.Create is NOT on the public endpoint (POST /geo/v1/regions on baseUrl rejected; accepted on internal /geo/v1/internal/regions)",
    classes=["NEG", "AUTHZ"], priority="P0",
    steps=[
        # public mux has only RegionService GET — a mutating POST has no route.
        Step(name="create-on-public", method="POST", path="/geo/v1/regions", internal=False,
             body={"id": "qa-anp-reg-{{runId}}", "name": "QA ANP Region {{runId}}"},
             test_script=[
                 "pm.test('public endpoint rejects Internal Create (routing-miss, not 200)', () => {",
                 "  pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.be.oneOf([403, 404, 405, 501]);",
                 "});",
             ]),
        Step(name="create-on-internal", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-anp-reg-{{runId}}", "name": "QA ANP Region {{runId}}"},
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup", method="DELETE", path="/geo/v1/internal/regions/qa-anp-reg-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


CASES.append(Case(
    id="ANP-ZON-CR-NOT-PUBLIC",
    title="InternalZoneService.Create is NOT on the public endpoint (POST /geo/v1/zones on baseUrl rejected; accepted on internal /geo/v1/internal/zones)",
    classes=["NEG", "AUTHZ"], priority="P0",
    steps=[
        Step(name="create-region-internal", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-anp-zr-{{runId}}", "name": "QA ANP Zone-Region {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-anp-zr-{{runId}}",
             test_script=[*assert_status(200)])),
        # coupling-valid zone id, but posted at the PUBLIC endpoint → routing-miss.
        Step(name="create-zone-on-public", method="POST", path="/geo/v1/zones", internal=False,
             body={"id": "qa-anp-zr-{{runId}}-a", "regionId": "qa-anp-zr-{{runId}}",
                   "name": "QA ANP Zone {{runId}}", "status": "UP"},
             test_script=[
                 "pm.test('public endpoint rejects Internal Create (routing-miss, not 200)', () => {",
                 "  pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.be.oneOf([403, 404, 405, 501]);",
                 "});",
             ]),
        Step(name="create-zone-on-internal", method="POST", path="/geo/v1/internal/zones", internal=True,
             body={"id": "qa-anp-zr-{{runId}}-a", "regionId": "qa-anp-zr-{{runId}}",
                   "name": "QA ANP Zone {{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/internal/zones/qa-anp-zr-{{runId}}-a", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/internal/regions/qa-anp-zr-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))
