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

Каждый кейс — контрастная пара (ban #6 proof): мутация ОТВЕРГНута на public
(промах маршрута края «метода нет»: 501 / code 12 / `Method Not Allowed`, тело целиком)
и ПРИНЯТА на internal `/geo/v1/internal/…`
(200 Operation envelope). Это ловит регресс, при котором Internal*-route случайно
засветится на external mux. Источник: gateway/internal/restmux/mux.go (geo public
vs geoInternal) + geo_test.go (TestGeo_S5_InternalPathsRejectedOnExternal).

Публичная нога тела НЕ несёт: её исход решается парой (метод, путь) ДО чтения тела,
поэтому тело там ничего не утверждало бы, а лишь выглядело бы как отказ по существу.
Сторож маршрута внешнего слушателя (kacho#3053) решает эту пару РАНЬШЕ аутентификации
и прав: путь `/geo/v1/regions` (`/zones`) публично существует под GET, поэтому POST
на нём — «путь есть, метода нет», одинаково для любого вызывающего. Прежний допуск
403/404/405/501 держался на том, что слой прав отвечал раньше маршрута; 401/403 на
этом месте теперь — регресс (`assert_edge_route_miss`).

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
        # Тела нет: исход решается ПАРОЙ (метод, путь) до всякого чтения тела, поэтому
        # одинаковый create-payload на публичной ноге ничего к контрасту не добавил бы —
        # он лишь создавал бы впечатление, что край его рассмотрел и отверг по существу.
        # Контраст остаётся полным: та же мутация ПРИНЯТА на internal-ноге ниже.
        Step(name="create-on-public", method="POST", path="/geo/v1/regions", internal=False,
             test_script=[*assert_edge_route_miss("method")]),
        Step(name="create-on-internal", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-anp-reg-{{runId}}"},
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
             body={"id": "qa-anp-zr-{{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-anp-zr-{{runId}}",
             test_script=[*assert_status(200)])),
        # Posted at the PUBLIC endpoint → routing-miss. Тела нет по той же причине:
        # на публичном крае этой пары (метод, путь) не существует, разбирать нечего.
        # «Валидность» зоны здесь ничего не проверяла бы — до её чтения не доходит;
        # тот же create с телом ПРИНЯТ на internal-ноге следующим шагом.
        Step(name="create-zone-on-public", method="POST", path="/geo/v1/zones", internal=False,
             test_script=[*assert_edge_route_miss("method")]),
        Step(name="create-zone-on-internal", method="POST", path="/geo/v1/internal/zones", internal=True,
             body={"id": "qa-anp-zr-{{runId}}-a", "regionId": "qa-anp-zr-{{runId}}",
                   "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-zone", method="DELETE", path="/geo/v1/internal/zones/qa-anp-zr-{{runId}}-a", internal=True,
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup-region", method="DELETE", path="/geo/v1/internal/regions/qa-anp-zr-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))
