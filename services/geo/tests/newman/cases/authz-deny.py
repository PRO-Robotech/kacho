# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo authz-матрица (public read + admin CRUD).

GEO-1 landed authz-контракт (сверено с security.md §AuthN+AuthZ ВЕЗДЕ + proto
authz_options + GEO-1 F6):
  * Public read (RegionService/ZoneService Get/List) — **project-scope EXEMPT**
    (authN-only, GEO-1-20): у 4 read-RPC снят required_relation+scope_extractor →
    любой АУТЕНТИФИЦИРОВАННЫЙ принципал (в т.ч. zero-binding) читает каталог.
      - anonymous (нет Bearer) → 401 UNAUTHENTICATED (code 16) — EXEMPT снимает
        authZ, но НЕ authN (GEO-1-21; anonymous-full-access запрещён).
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
# authenticated zero-binding public read → 200 (ambient read; project-scope EXEMPT).
# verifies GEO-1-20
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-GT-AUTHZ-AMBIENT-OK",
    title="GET /geo/v1/regions as authenticated jwtNoBindings (zero-binding) → 200 (ambient read; project-scope EXEMPT, GEO-1-20)",
    classes=["AUTHZ", "CONF"], priority="P1",
    steps=[
        Step(name="list-regions-noviewer", method="GET", path="/geo/v1/regions", auth="jwtNoBindings",
             test_script=[
                 *assert_status(200),
                 "pm.test('ambient read: zero-binding principal is NOT denied (no 403)', () => pm.expect(pm.response.code).to.not.eql(403));",
                 "pm.test('regions is an array', () => pm.expect(pm.response.json().regions).to.be.an('array'));",
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
             body={"id": "qa-deny-reg-{{runId}}", "name": "QA Deny Region {{runId}}"},
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
             body={"id": "qa-any-{{runId}}-a", "regionId": "qa-any-{{runId}}", "name": "QA Deny Zone {{runId}}", "status": "UP"},
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
    ],
))
