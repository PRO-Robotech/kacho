# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo authz-матрица (public read + admin CRUD).

GEO-1 landed authz-контракт (сверено с security.md §AuthN+AuthZ ВЕЗДЕ + proto
authz_options + GEO-1 F6):
  * Public read (RegionService/ZoneService Get/List) — отношение `viewer` на
    КЛАСТЕРЕ, которое платформа выдаёт СИСТЕМНОЙ ВЫДАЧЕЙ субъекту «любой
    аутентифицированный» (#893/#895): персонального гранта не нужно, поэтому
    zero-binding читает каталог, но проверка при этом настоящая и её основание
    видно перечислением выдач.
      - anonymous (нет Bearer) → 401 UNAUTHENTICATED (code 16): подстановка
        расширяет отношение на АУТЕНТИФИЦИРОВАННЫХ, а не на всех
        (GEO-1-21; anonymous-full-access запрещён).
      - authenticated zero-binding (jwtNoBindings) → 200 (ambient read, GEO-1-20).
  * Admin CRUD (InternalRegion/ZoneService, `/geo/v1/internal/…` internal listener):
    `system_admin`@cluster (scope_extractor object_type:cluster, GEO-1-22).
      - non-admin (jwtNoBindings) → 403 PERMISSION_DENIED (code 7).
      - jwtBootstrap (system_admin@cluster) → 200 Operation (positive — internal-*.py).

Test-design: ECP (auth-class: anonymous / authenticated-no-grant / admin), AUTHZ
(ambient-read + admin-deny matrix), error-guessing (authN-vs-authZ ordering). Каждый
deny/allow — matched positive/negative живёт в region/zone/internal-* (admin happy).
"""

CASES = []


# ---------------------------------------------------------------------------
# ПОЛОСА РАЗРЕШЁННОГО ЧТЕНИЯ — ПАРА «СТАТУС + ФОРМА», а не отрицание отказа
#
# Здесь у каждого из четырёх ambient-шагов рядом с `assert_status(200)` стояла
# строка «a zero-binding principal is not denied (never 403)». Она не могла упасть
# ОТДЕЛЬНО: 403 ≠ 200, поэтому утверждение о статусе краснело первым и всегда, а
# отрицание лишь повторяло его более слабым способом. Хуже: такое отрицание
# проходит на 400, 500 и 503 — то есть само по себе не отличает исправную систему
# ни от одной поломки. verifies #668.
#
# ПАРА ДЛЯ УСПЕШНОГО ЧТЕНИЯ — ЭТО СТАТУС И ФОРМА. `google.rpc.Status` успешный
# ответ не несёт (его место занимает сам ресурс), поэтому вторым членом пары
# служит форма тела. Взята не произвольная: ровно тот инвариант, ради которого
# публичная поверхность geo и объявлена исключением из project-scope authZ —
# ДВЕ ПРОЕКЦИИ. Публичные `Region`/`Zone` (proto/kacho/cloud/geo/v1/{region,zone}.proto)
# сырого `status` и `infra` НЕ несут; их несут `InternalRegion`/`InternalZone` на
# :9091. Снятый authZ означает, что публичный каталог читает КАЖДЫЙ
# аутентифицированный арендатор, — значит утечка внутренней проекции на этот
# маршрут раздаёт инфраструктурные поля всем сразу.
#
# Проверяются ИМЕННО эти два поля, а не «набор ключей целиком»: строгая сверка
# всего набора краснела бы на законном добавлении публичного поля, то есть
# ловила бы форму, а не существо.
_INTERNAL_ONLY_FIELDS = ("status", "infra")


def two_projection_asserts(case_id, list_key=None):
    """Публичное чтение не несёт внутренней проекции (`status` / `infra`).

    `list_key` задан → проверяется каждый элемент страницы; не задан → сам объект.
    """
    subject = f"(pm.response.json()['{list_key}'] || [])" if list_key else "[pm.response.json()]"
    return [
        f"pm.test('[{case_id}] публичная проекция: ни status, ни infra', () => {{",
        f"  {subject}.forEach(o => {{",
        "    pm.expect(Object.keys(o || {}), 'внутренняя проекция утекла на публичный маршрут: '",
        "      + JSON.stringify(o)).to.not.include('status');",
        "    pm.expect(Object.keys(o || {}), 'внутренняя проекция утекла на публичный маршрут: '",
        "      + JSON.stringify(o)).to.not.include('infra');",
        "  });",
        "});",
    ]



# ---------------------------------------------------------------------------
# anonymous public read → 401 UNAUTHENTICATED (authN required on every listener).
# verifies GEO-1-21
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-GT-AUTHZ-ANON-DENY",
    title="GET /geo/v1/regions as anonymous (no Bearer) → 401 UNAUTHENTICATED (authN required on every listener)",
    classes=["AUTHZ", "NEG"], priority="P1",
    steps=[
        Step(name="list-regions-anon", method="GET", path="/geo/v1/regions", auth="anonymous",
             test_script=[
                 *assert_status(401),
                 *assert_grpc_code(16, "UNAUTHENTICATED"),
             ]),
    ],
))

CASES.append(Case(
    id="GEO-ZON-GT-AUTHZ-ANON-DENY",
    title="GET /geo/v1/zones as anonymous (no Bearer) → 401 UNAUTHENTICATED",
    classes=["AUTHZ", "NEG"], priority="P1",
    steps=[
        Step(name="list-zones-anon", method="GET", path="/geo/v1/zones", auth="anonymous",
             test_script=[
                 *assert_status(401),
                 *assert_grpc_code(16, "UNAUTHENTICATED"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# authenticated zero-binding public read → 200 (ambient read; отношение выдано
# системной выдачей подстановочному субъекту).
# verifies GEO-1-20
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-GT-AUTHZ-AMBIENT-OK",
    title="GET /geo/v1/regions as authenticated jwtNoBindings (zero-binding) → 200 (ambient read; отношение выдано системной выдачей подстановочному субъекту, GEO-1-20)",
    classes=["AUTHZ", "CONF"], priority="P1",
    steps=[
        Step(name="list-regions-noviewer", method="GET", path="/geo/v1/regions", auth="jwtNoBindings",
             test_script=[
                 *assert_status(200),
                 "pm.test('regions is an array', () => pm.expect(pm.response.json().regions).to.be.an('array'));",
                 *two_projection_asserts("GEO-REG-GT-AUTHZ-AMBIENT-OK", "regions"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# non-admin admin CRUD → 403 PERMISSION_DENIED (system_admin required).
# verifies GEO-1-22
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-CR-AUTHZ-NONADMIN-DENY",
    title="InternalRegionService.Create as non-admin jwtNoBindings → 403 PERMISSION_DENIED (system_admin required)",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="create-region-nonadmin", method="POST", path="/geo/v1/internal/regions", internal=True, auth="jwtNoBindings",
             body={"id": "qa-deny-reg-{{runId}}"},
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
    ],
))

CASES.append(Case(
    id="GEO-ZON-CR-AUTHZ-NONADMIN-DENY",
    title="InternalZoneService.Create as non-admin jwtNoBindings → 403 PERMISSION_DENIED (system_admin required)",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="create-zone-nonadmin", method="POST", path="/geo/v1/internal/zones", internal=True, auth="jwtNoBindings",
             body={"id": "qa-any-{{runId}}-a", "regionId": "qa-any-{{runId}}", "status": "UP"},
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
    ],
))


# ===========================================================================
# GEO-1 landed → the scenarios the set was accepted forward-compatible on.
#
# The set predates GEO-1 and covered the contract only at its widest points:
# anonymous List, one ambient List, one internal Create deny. Three edges were
# left for when GEO-1 landed, and each one is where the contract can actually
# break silently:
#   * Get-by-id — a SEPARATE RPC from List, exempt by its own annotation. An
#     exemption applied to List but not to Get (or the reverse) is invisible to
#     a set that only exercises List.
#   * the ambient allow on ZONES — asserted for regions only, though zoneId is
#     what every placement Create actually needs.
#   * the internal listener beyond Create — Update/Delete/GetInternal. GetInternal
#     is the one that returns raw status + infra, i.e. the door the two-projection
#     invariant exists to keep shut; it had no deny case at all.
#
# Subject choice: the ambient-allow cases use jwtPureNoBindings, the dedicated
# never-granted subject. jwtNoBindings is also a grant TARGET in the iam suites,
# so under a parallel run it can transiently hold bindings — which would leave
# "zero-binding principal reads the catalog" proven by a principal that was not
# zero-binding at that moment.
# ===========================================================================


# ---------------------------------------------------------------------------
# anonymous Get-by-id → 401. List was covered; Get — отдельный RPC со своей записью.
# verifies GEO-1-21
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-GET-AUTHZ-ANON-DENY",
    title="GET /geo/v1/regions/{id} as anonymous (no Bearer) → 401 UNAUTHENTICATED — подстановочный "
          "субъект системной выдачи расширяет отношение на аутентифицированных, а не на всех; Get несёт "
          "свою запись каталога, поэтому покрытие List за него не отвечает",
    classes=["AUTHZ", "NEG"], priority="P1",
    steps=[
        Step(name="get-region-anon", method="GET", path="/geo/v1/regions/{{existingRegionId}}", auth="anonymous",
             test_script=[
                 *assert_status(401),
                 *assert_grpc_code(16, "UNAUTHENTICATED"),
                 "pm.test('no catalog payload leaks on the unauthenticated path', () => pm.expect(pm.response.text()).to.not.include('openForPlacement'));",
             ]),
    ],
))

CASES.append(Case(
    id="GEO-ZON-GET-AUTHZ-ANON-DENY",
    title="GET /geo/v1/zones/{id} as anonymous (no Bearer) → 401 UNAUTHENTICATED (same as Region: exempt "
          "from project-scope authZ, still authenticated)",
    classes=["AUTHZ", "NEG"], priority="P1",
    steps=[
        Step(name="get-zone-anon", method="GET", path="/geo/v1/zones/{{existingZoneId}}", auth="anonymous",
             test_script=[
                 *assert_status(401),
                 *assert_grpc_code(16, "UNAUTHENTICATED"),
                 "pm.test('no catalog payload leaks on the unauthenticated path', () => pm.expect(pm.response.text()).to.not.include('openForPlacement'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# authenticated, zero bindings → 200 on every public read surface.
# verifies GEO-1-20
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-ZON-GT-AUTHZ-AMBIENT-OK",
    title="GET /geo/v1/zones as authenticated jwtPureNoBindings (never-granted subject) → 200 ambient read. "
          "Zones matter more than regions here: zoneId is what every placement Create needs, so a tenant "
          "with no bindings yet must be able to read it",
    classes=["AUTHZ", "CONF"], priority="P0",
    steps=[
        Step(name="list-zones-purenobindings", method="GET", path="/geo/v1/zones", auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(200),
                 "pm.test('zones is an array', () => pm.expect(pm.response.json().zones).to.be.an('array'));",
                 *two_projection_asserts("GEO-ZON-GT-AUTHZ-AMBIENT-OK", "zones"),
             ]),
    ],
))

CASES.append(Case(
    id="GEO-REG-GET-AUTHZ-AMBIENT-OK",
    title="GET /geo/v1/regions/{id} as authenticated jwtPureNoBindings → 200 — the exemption covers Get-by-id, "
          "not only List",
    classes=["AUTHZ", "CONF"], priority="P1",
    steps=[
        Step(name="list-pick-ambient", method="GET", path="/geo/v1/regions", auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(200),
                 "pm.test('regions non-empty (geography seeded)', () => pm.expect((pm.response.json().regions||[]).length).to.be.greaterThan(0));",
                 *save_from_response("(pm.response.json().regions.find(r => !String(r.id).startsWith('qa')) || pm.response.json().regions[0]).id", "ambientPickRegionId"),
             ]),
        Step(name="get-region-purenobindings", method="GET", path="/geo/v1/regions/{{ambientPickRegionId}}",
             auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(200),
                 "pm.test('id echoes the requested region', () => pm.expect(pm.response.json().id).to.eql(pm.environment.get('ambientPickRegionId')));",
                 *two_projection_asserts("GEO-REG-GET-AUTHZ-AMBIENT-OK"),
             ]),
    ],
))

CASES.append(Case(
    id="GEO-ZON-GET-AUTHZ-AMBIENT-OK",
    title="GET /geo/v1/zones/{id} as authenticated jwtPureNoBindings → 200 (exemption covers zone Get-by-id)",
    classes=["AUTHZ", "CONF"], priority="P1",
    steps=[
        Step(name="list-pick-ambient", method="GET", path="/geo/v1/zones", auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(200),
                 "pm.test('zones non-empty (geography seeded)', () => pm.expect((pm.response.json().zones||[]).length).to.be.greaterThan(0));",
                 *save_from_response("(pm.response.json().zones.find(z => !String(z.id).startsWith('qa')) || pm.response.json().zones[0]).id", "ambientPickZoneId"),
             ]),
        Step(name="get-zone-purenobindings", method="GET", path="/geo/v1/zones/{{ambientPickZoneId}}",
             auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(200),
                 "pm.test('id echoes the requested zone', () => pm.expect(pm.response.json().id).to.eql(pm.environment.get('ambientPickZoneId')));",
                 *two_projection_asserts("GEO-ZON-GET-AUTHZ-AMBIENT-OK"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# internal listener — anonymous is refused before any authorization decision.
# verifies GEO-1-21 / GEO-1-22
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-CR-AUTHZ-ANON-DENY",
    title="InternalRegionService.Create as anonymous → 401 UNAUTHENTICATED. The internal listener is NOT "
          "exempt from authN: 'internal = trusted' is the forbidden assumption (security.md §AuthN+AuthZ "
          "everywhere)",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="create-region-anon", method="POST", path="/geo/v1/internal/regions", internal=True,
             auth="anonymous",
             body={"id": "qa-anon-reg-{{runId}}"},
             test_script=[
                 *assert_status(401),
                 *assert_grpc_code(16, "UNAUTHENTICATED"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# internal listener — the admin verbs beyond Create are cluster-admin only.
# verifies GEO-1-22
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-UPD-AUTHZ-NONADMIN-DENY",
    title="InternalRegionService.Update as non-admin jwtPureNoBindings → 403 PERMISSION_DENIED "
          "(system_admin@cluster). Create was covered; Update carries its own annotation",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="update-region-nonadmin", method="PATCH", path="/geo/v1/internal/regions/qa-deny-upd-reg-{{runId}}",
             internal=True, auth="jwtPureNoBindings",
             body={"countryCode": "NL", "updateMask": "countryCode"},
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
    ],
))

CASES.append(Case(
    id="GEO-REG-DEL-AUTHZ-NONADMIN-DENY",
    title="InternalRegionService.Delete as non-admin jwtPureNoBindings → 403 PERMISSION_DENIED. Deleting a "
          "region is the most destructive verb on the placement axis — it must be the least reachable",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="delete-region-nonadmin", method="DELETE", path="/geo/v1/internal/regions/qa-deny-del-reg-{{runId}}",
             internal=True, auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
    ],
))

CASES.append(Case(
    id="GEO-ZON-UPD-AUTHZ-NONADMIN-DENY",
    title="InternalZoneService.Update as non-admin jwtPureNoBindings → 403 PERMISSION_DENIED "
          "(system_admin@cluster)",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="update-zone-nonadmin", method="PATCH", path="/geo/v1/internal/zones/qa-deny-upd-{{runId}}-a",
             internal=True, auth="jwtPureNoBindings",
             body={"status": "DOWN"},
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
    ],
))

CASES.append(Case(
    id="GEO-ZON-DEL-AUTHZ-NONADMIN-DENY",
    title="InternalZoneService.Delete as non-admin jwtPureNoBindings → 403 PERMISSION_DENIED",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="delete-zone-nonadmin", method="DELETE", path="/geo/v1/internal/zones/qa-deny-del-{{runId}}-a",
             internal=True, auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# internal listener — GetInternal is the door to raw status + infra.
#
# The id is discovered through the AMBIENT public read the same subject is
# entitled to, so the 403 cannot be excused as "that region does not exist":
# the row is provably there, the subject provably reads its public projection,
# and only the infra-bearing projection is refused.
# verifies GEO-1-22 · two-projection (security.md §infra-sensitive)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-GTI-AUTHZ-NONADMIN-DENY",
    title="InternalRegionService.GetInternal as non-admin jwtPureNoBindings → 403 PERMISSION_DENIED on a "
          "region the same subject just read publicly — raw status + infra stay behind system_admin",
    classes=["AUTHZ", "NEG", "CONF"], priority="P0",
    steps=[
        Step(name="public-read-first", method="GET", path="/geo/v1/regions", auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(200),
                 "pm.test('regions non-empty (geography seeded)', () => pm.expect((pm.response.json().regions||[]).length).to.be.greaterThan(0));",
                 *save_from_response("(pm.response.json().regions.find(r => !String(r.id).startsWith('qa')) || pm.response.json().regions[0]).id", "ambientRegionId"),
             ]),
        Step(name="get-internal-nonadmin", method="GET", path="/geo/v1/internal/regions/{{ambientRegionId}}",
             internal=True, auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
                 "pm.test('denied body carries no infra and no raw status', () => { const b = pm.response.text().toLowerCase(); ['numericinfraid','hostclasses','underlayanchor','capacityhint','failuredomaincount','\"status\"'].forEach(k => pm.expect(b, 'leaked on deny: ' + k).to.not.include(k)); });",
             ]),
    ],
))

CASES.append(Case(
    id="GEO-ZON-GTI-AUTHZ-NONADMIN-DENY",
    title="InternalZoneService.GetInternal as non-admin jwtPureNoBindings → 403 PERMISSION_DENIED on a zone "
          "the same subject just read publicly — ZoneInfra (hostClasses/underlayAnchor/failureDomainCount) "
          "stays behind system_admin",
    classes=["AUTHZ", "NEG", "CONF"], priority="P0",
    steps=[
        Step(name="public-read-first", method="GET", path="/geo/v1/zones", auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(200),
                 "pm.test('zones non-empty (geography seeded)', () => pm.expect((pm.response.json().zones||[]).length).to.be.greaterThan(0));",
                 *save_from_response("(pm.response.json().zones.find(z => !String(z.id).startsWith('qa')) || pm.response.json().zones[0]).id", "ambientZoneId"),
             ]),
        Step(name="get-internal-nonadmin", method="GET", path="/geo/v1/internal/zones/{{ambientZoneId}}",
             internal=True, auth="jwtPureNoBindings",
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
                 "pm.test('denied body carries no infra and no raw status', () => { const b = pm.response.text().toLowerCase(); ['numericinfraid','hostclasses','underlayanchor','capacityhint','failuredomaincount','\"status\"'].forEach(k => pm.expect(b, 'leaked on deny: ' + k).to.not.include(k)); });",
             ]),
    ],
))
