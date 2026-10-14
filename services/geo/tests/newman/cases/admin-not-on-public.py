# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: ban #6 guard — Internal* admin verbs NOT reachable on the public endpoint.

InternalRegionService/InternalZoneService (полная плоскость администрирования с
infra° и GetInternal) живут ТОЛЬКО на cluster-internal REST listener
({{internalBaseUrl}}) под самоописываемым сегментом `/geo/v1/internal/…` (GEO-1 F5;
gateway restmux geoInternalAddr). Публичный {{baseUrl}} их НЕ несёт by design
(security.md §Internal-vs-external, ban #6 — Internal.* не публикуется на external
endpoint).

ПРЕДСТАВИТЕЛЬ ПУБЛИЧНОЙ НОГИ СМЕНЁН (приёмка ADM-1 geo, §Р7, kacho#3092). Прежде
она била `POST /geo/v1/{regions,zones}` и ждала «метода нет»: публичный mux нёс
лишь GET. С ADM-1 на этих парах стоят ПУБЛИЧНЫЕ административные глаголы
(RegionService/ZoneService Create — право system_admin, вход без infra°), и
прежнее ожидание стало ложью. Предмет пары остался прежним — «ВНУТРЕННИЙ глагол
снаружи не достижим», — поэтому нога теперь бьёт сам внутренний адрес
`POST /geo/v1/internal/{regions,zones}` на внешнем слушателе и ждёт 404 «маршрута
нет». Переворот ожидания на прежнем пути вместо смены представителя был бы
нарушением: он утверждал бы другое.

Каждый кейс — контрастная пара: внутренняя мутация ОТВЕРГНута на public (промах
маршрута края «пути нет»: 404 / code 5 / `Not Found`, тело целиком) и ПРИНЯТА на
internal `/geo/v1/internal/…` (200 Operation envelope). Сторож маршрута внешнего
слушателя (kacho#3053) решает эту пару РАНЬШЕ аутентификации и прав, одинаково
для любого вызывающего; 401/403 на этом месте — регресс. Тела публичная нога не
несёт: исход решается парой (метод, путь) до чтения тела.

Test-design: NEG (public routing-miss), CRUD-контраст (internal accepts). self-
contained: internal-created ресурс {{runId}}-суффиксирован + cleanup.
"""

CASES = []


CASES.append(Case(
    id="ANP-REG-CR-NOT-PUBLIC",
    title="InternalRegionService.Create is NOT on the public endpoint (POST /geo/v1/internal/regions on baseUrl → 404; accepted on internal)",
    classes=["NEG", "AUTHZ"], priority="P0",
    steps=[
        # Внутренний адрес на внешнем слушателе — маршрута нет (ни один публичный
        # шаблон его не покрывает). Тела нет: исход решается ПАРОЙ (метод, путь) до
        # всякого чтения тела. Контраст полон: та же мутация ПРИНЯТА на internal-ноге.
        Step(name="create-on-public", method="POST", path="/geo/v1/internal/regions", internal=False,
             test_script=[*assert_edge_route_miss("path")]),
        Step(name="create-on-internal", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-anp-reg-{{runId}}"},
             test_script=[*assert_operation_envelope()]),
        Step(name="cleanup", method="DELETE", path="/geo/v1/internal/regions/qa-anp-reg-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


CASES.append(Case(
    id="ANP-ZON-CR-NOT-PUBLIC",
    title="InternalZoneService.Create is NOT on the public endpoint (POST /geo/v1/internal/zones on baseUrl → 404; accepted on internal)",
    classes=["NEG", "AUTHZ"], priority="P0",
    steps=[
        Step(name="create-region-internal", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-anp-zr-{{runId}}", "status": "UP"},
             test_script=[*assert_operation_envelope()]),
        retry_get_until_found(Step(name="confirm-region", method="GET",
             path="/geo/v1/regions/qa-anp-zr-{{runId}}",
             test_script=[*assert_status(200)])),
        # Внутренний адрес на ПУБЛИЧНОМ слушателе → «пути нет». Тела нет по той же
        # причине: до его чтения не доходит; тот же create с телом ПРИНЯТ на
        # internal-ноге следующим шагом.
        Step(name="create-zone-on-public", method="POST", path="/geo/v1/internal/zones", internal=False,
             test_script=[*assert_edge_route_miss("path")]),
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
