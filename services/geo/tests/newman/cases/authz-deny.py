# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo authz-матрица (public read + admin CRUD).

AS-IS authz-контракт geo (сверено с security.md §AuthN+AuthZ ВЕЗДЕ +
proto authz_options):
  * Public read (RegionService/ZoneService Get/List): authN обязателен; authZ —
    `viewer`@cluster (required_relation=viewer, scope_extractor cluster).
      - anonymous (нет Bearer) → 401 UNAUTHENTICATED (code 16).
      - authenticated БЕЗ cluster#viewer (jwtNoBindings) → 403 PERMISSION_DENIED (code 7).
      - jwtBootstrap (system_viewer@cluster) → 200 (positive — region.py/zone.py CONF-OK).
  * Admin CRUD (InternalRegion/ZoneService, internal listener): `system_admin`@cluster.
      - non-admin (jwtNoBindings) → 403 PERMISSION_DENIED (code 7).
      - jwtBootstrap (system_admin@cluster) → 200 Operation (positive — internal-*.py).

Мигрировано из services/iam/tests/newman/cases/geo-read.py: GEO-REG/ZON-GT-AUTHZ-
ANON-DENY (geo больше не «безнадзорный» — реги отключена из iam run.sh).

NOTE: GEO-1-редизайн делает public read project-scope EXEMPT (authN-only), тогда
NOVIEWER-DENY станет 200. GEO-1 merge-gated и НЕ приземлён → лочим AS-IS viewer-
gating (см. docs/RESULTS.md). anonymous→401 остаётся инвариантом и после GEO-1
(EXEMPT снимает authZ, но НЕ authN).

Test-design: ECP (auth-class: anonymous / authenticated-no-grant / admin), AUTHZ
(deny matrix), error-guessing (authN-vs-authZ ordering). Каждый deny — matched
positive живёт в region/zone/internal-* (authenticated-admin happy).
"""

CASES = []


# ---------------------------------------------------------------------------
# anonymous public read → 401 UNAUTHENTICATED (migrated from iam geo-read.py).
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
# authenticated-but-no-viewer public read → 403 PERMISSION_DENIED (AS-IS viewer-gate).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-GT-AUTHZ-NOVIEWER-DENY",
    title="GET /geo/v1/regions as authenticated jwtNoBindings (no cluster#viewer) → 403 PERMISSION_DENIED (AS-IS; GEO-1 will exempt)",
    classes=["AUTHZ", "NEG"], priority="P1",
    steps=[
        Step(name="list-regions-noviewer", method="GET", path="/geo/v1/regions", auth="jwtNoBindings",
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# non-admin admin CRUD → 403 PERMISSION_DENIED (system_admin required).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-CR-AUTHZ-NONADMIN-DENY",
    title="InternalRegionService.Create as non-admin jwtNoBindings → 403 PERMISSION_DENIED (system_admin required)",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="create-region-nonadmin", method="POST", path="/geo/v1/regions", internal=True, auth="jwtNoBindings",
             body={"id": "qa-deny-reg-{{runId}}", "name": "QA Deny Region"},
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
        Step(name="create-zone-nonadmin", method="POST", path="/geo/v1/zones", internal=True, auth="jwtNoBindings",
             body={"id": "qa-deny-zon-{{runId}}", "regionId": "qa-any", "name": "QA Deny Zone", "status": "UP"},
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
             ]),
    ],
))
