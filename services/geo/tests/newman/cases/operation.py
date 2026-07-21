# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo OperationService (op-poll) через api-gateway OpsProxy.

Мутации geo (InternalRegion/ZoneService) возвращают Operation{id:'geo…'}; клиент
поллит GET /operations/{id}. OperationService помечен <exempt> (authN-only), затем
per-op ownership-check (BOLA-guard, только создатель читает/отменяет).

## PRODUCT BUG (verified by test) — PRO-Robotech/kacho#55

api-gateway OpsProxy маршрутизирует op по 3-char prefix (gateway/internal/opsproxy/
proxy.go `prefixToBackend`). Prefix 'geo' в карте ОТСУТСТВУЕТ (при том что geo
backend-conn в gateway есть — server.go/geo_director_test.go), поэтому geo op-id
резолвится в `InvalidArgument "invalid operation id"` вместо проксирования в geo.
Следствие: НИ ОДНУ geo admin-мутацию нельзя доткнуть до done через gateway; op.error
(dup/FK/not-found) недоступен; ownership/BOLA-проверка (после resolveBackend)
недостижима. Fix — одна строка (`"geo": "geo"` в prefixToBackend; conn уже есть).

Кейсы ниже с `# verifies #55` — RED сейчас (gateway 400), GREEN после fix. Заявлены
в docs/RESULTS.md «Known failing — product bugs». Остальные — GREEN (opsproxy СВОЮ
валидацию malformed-id делает независимо от geo-routing).

Test-design: NEG/VAL (malformed op-id → InvalidArgument, GREEN), state-transition
(op-poll до done — RED #55), AUTHZ/BOLA (foreign principal → PermissionDenied — RED #55).
"""

CASES = []


# ---------------------------------------------------------------------------
# GOP-GET-VAL-MALFORMED — malformed operation id → 400 InvalidArgument.
# GREEN: OpsProxy validates the id shape (not 20-char, no legacy '_' prefix) and
# rejects with InvalidArgument BEFORE any backend routing — works independent of #55.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GOP-GET-VAL-MALFORMED",
    title="Get operation with malformed id → 400 InvalidArgument 'invalid operation id' (OpsProxy validation)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="get-malformed-op", method="GET", path="/operations/garbage-not-an-op-id",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message: invalid operation id', () => pm.expect(String(pm.response.json().message).toLowerCase()).to.include('invalid operation id'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# GEO-IOP-POLL-ROUTING-OK — RED bug-lock: geo Operation must be pollable to done.
# verifies https://github.com/PRO-Robotech/kacho/issues/55
# Create a region (admin), take the returned geo op-id, poll it. Asserts the
# post-fix contract (NOT 400 InvalidArgument; 200; done). RED today (gateway
# OpsProxy has no 'geo' prefix → 400 'invalid operation id').
# ---------------------------------------------------------------------------
# index: GEO-IOP-POLL-ROUTING-OK
CASES.append(Case(
    id="GEO-IOP-POLL-ROUTING-OK",
    title="[RED #55] Poll a geo Operation to done — gateway OpsProxy must route the 'geo' prefix",
    classes=["STATE", "NEG"], priority="P0",
    steps=[
        Step(name="create-region-for-op", method="POST", path="/geo/v1/regions", internal=True,
             body={"id": "qa-reg-op-{{runId}}", "name": "QA Region Op {{runId}}"},
             test_script=[
                 *assert_operation_envelope(),
                 *save_from_response("j.id", "geoOpId"),
             ]),
        poll_geo_op_red(),
        # best-effort cleanup (its own op is likewise unpollable until #55 fixed).
        Step(name="cleanup", method="DELETE", path="/geo/v1/regions/qa-reg-op-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))


# ---------------------------------------------------------------------------
# GEO-IOP-GET-AUTHZ-BOLA — RED bug-lock: foreign principal must NOT read another's
# operation. verifies https://github.com/PRO-Robotech/kacho/issues/55
# Create as jwtBootstrap, then poll as jwtNoBindings → post-fix ownership-check
# gives PermissionDenied (403). Blocked behind the routing bug today (400 first).
# ---------------------------------------------------------------------------
# index: GEO-IOP-GET-AUTHZ-BOLA
CASES.append(Case(
    id="GEO-IOP-GET-AUTHZ-BOLA",
    title="[RED #55] Foreign principal polling another's geo Operation → PermissionDenied (BOLA owner-scoping)",
    classes=["AUTHZ", "NEG"], priority="P1",
    steps=[
        Step(name="create-region-owned", method="POST", path="/geo/v1/regions", internal=True,
             auth="jwtBootstrap",
             body={"id": "qa-reg-bola-{{runId}}", "name": "QA Region BOLA {{runId}}"},
             test_script=[
                 *assert_operation_envelope(),
                 *save_from_response("j.id", "geoBolaOpId"),
             ]),
        Step(name="poll-as-foreign", method="GET", path="/operations/{{geoBolaOpId}}",
             auth="jwtNoBindings",
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
        Step(name="cleanup", method="DELETE", path="/geo/v1/regions/qa-reg-bola-{{runId}}", internal=True,
             auth="jwtBootstrap",
             test_script=[*assert_operation_envelope()]),
    ],
))
