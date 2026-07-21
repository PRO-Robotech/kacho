# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: ban #6 guard — Internal* admin verbs NOT reachable on the public endpoint.

InternalRegionService/InternalZoneService (admin CRUD) живут ТОЛЬКО на cluster-
internal REST listener ({{internalBaseUrl}}); публичный {{baseUrl}} их НЕ несёт by
design (security.md §Internal-vs-external, ban #6 — Internal.* не публикуется на
external endpoint). Публичный mux регистрирует лишь RegionService/ZoneService (GET),
поэтому POST /geo/v1/regions|zones на публичном endpoint не имеет маршрута.

Каждый кейс — контрастная пара (ban #6 proof): та же мутация ОТВЕРГНута на public
(routing-miss: 403/404/405/501, НЕ 200) и ПРИНЯТА на internal (200 Operation
envelope). Это ловит регресс, при котором Internal*-route случайно засветится на
external mux. Источник: gateway/internal/restmux/mux.go (geo public vs geoInternal).

Test-design: NEG (public routing-miss), CRUD-контраст (internal accepts). self-
contained: internal-created ресурс {{runId}}-суффиксирован + cleanup.
"""

CASES = []


CASES.append(Case(
    id="ANP-REG-CR-NOT-PUBLIC",
    title="InternalRegionService.Create is NOT on the public endpoint (POST /geo/v1/regions on baseUrl rejected; accepted on internal)",
    classes=["NEG", "AUTHZ"], priority="P0",
    steps=[
        Step(name="create-on-public", method="POST", path="/geo/v1/regions", internal=False,
             body={"id": "qa-anp-reg-{{runId}}", "name": "QA ANP Region"},
             test_script=[
                 "pm.test('public endpoint rejects Internal Create (routing-miss, not 200)', () => {",
                 "  pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.be.oneOf([403, 404, 405, 501]);",
                 "});",
             ]),
        Step(name="create-on-internal", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-anp-reg-{{runId}}", "name": "QA ANP Region"},
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup", method="DELETE", path="/geo/v1/regions/qa-anp-reg-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


CASES.append(Case(
    id="ANP-ZON-CR-NOT-PUBLIC",
    title="InternalZoneService.Create is NOT on the public endpoint (POST /geo/v1/zones on baseUrl rejected; accepted on internal)",
    classes=["NEG", "AUTHZ"], priority="P0",
    steps=[
        Step(name="create-region-internal", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-anp-zreg-{{runId}}", "name": "QA ANP Zone-Region"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-anp-zreg-{{runId}}",
             test_script=[*assert_status(200)])),
        Step(name="create-zone-on-public", method="POST", path="/geo/v1/zones", internal=False,
             body={"id": "qa-anp-zon-{{runId}}", "regionId": "qa-anp-zreg-{{runId}}",
                   "name": "QA ANP Zone", "status": "UP"},
             test_script=[
                 "pm.test('public endpoint rejects Internal Create (routing-miss, not 200)', () => {",
                 "  pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.be.oneOf([403, 404, 405, 501]);",
                 "});",
             ]),
        Step(name="create-zone-on-internal", method="POST", path="/geo/v1/zones", internal=True,
             body={"id": "qa-anp-zon-{{runId}}", "regionId": "qa-anp-zreg-{{runId}}",
                   "name": "QA ANP Zone", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/zones/qa-anp-zon-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/regions/qa-anp-zreg-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))
