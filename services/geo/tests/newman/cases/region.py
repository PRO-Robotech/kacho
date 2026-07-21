# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo public RegionService reads through the api-gateway REST mux.

Black-box coverage of the public Region read surface (`GET /geo/v1/regions`,
`GET /geo/v1/regions/{id}` → `RegionService.List` / `RegionService.Get`) exercised
end-to-end through the shared api-gateway public REST listener ({{baseUrl}}), using
the authz-fixtures dev JWTs. geo is the leaf platform-topology catalog (Region/Zone,
owner of the placement axis) — global cluster-scoped, NOT project-scoped.

Source of truth — APPROVED acceptance `docs/specs/sub-phase-GEO-1-region-zone-redesign-acceptance.md`
and `.claude/rules/api-conventions.md`.

Probe note (deployed contract as of redesign/integration @ 8f3dca1 — the branch CI
runs): the GEO-1 redesign (EXEMPT ambient-read, two-projection status-move to
Internal, `/geo/v1/internal/…` admin paths, `countryCode°`/`openForPlacement°`/
`openZoneCountHint°`, malformed-id text flip) is NOT yet landed here. The public
`Region` is `{id, name, createdAt}`; public reads are gated `viewer`@`cluster`
(NOT yet EXEMPT), so `jwtBootstrap` (seeded admin/viewer-floor) reads 200 while an
anonymous caller gets 401. These cases therefore lock the invariants that are STABLE
across the redesign boundary (status/leak-code, not-found text, pagination-validate,
authN-required, infra/host-class field-absence) with forward-compatible assertions;
redesign-only deltas (GEO-1-20 zero-binding→200, exact malformed text, status
projection) are deferred and tracked in docs/TEST-PLAN.md §"Deferred — GEO-1 redesign".

Test-design techniques applied (per `testing-product-coach`):
  - ECP (equivalence): valid seeded id vs well-formed-absent vs malformed-charset id;
    authenticated-viewer vs anonymous input class.
  - BVA (boundaries): pageSize 0 (→default), pageSize>max (→reject); page_token
    garbage vs round-trip.
  - CONF (conformance): response shape vs `Region`/`ListRegionsResponse` contract
    (camelCase, flat resource); verbatim NotFound text `"Region <id> not found"`.
  - error-guessing: two-projection leak probe (host-class/infra NotContains on the
    serialized body); admin-write verb on the public endpoint must never mutate.
  - state-transition N/A — reads are sync, no Operation envelope.

Per positive there is ≥1 matched negative (NotFound / malformed / anon-deny /
admin-not-on-public). Test-only — no prod code touched (ban #13).
"""

CASES = []


# ---------------------------------------------------------------------------
# REG-LST-CRUD-OK — happy List: 200 + non-empty regions[] of well-formed items.
# (geo catalog is admin-seeded by the umbrella — at least one region exists.)
# verifies GEO-1-25 (List item carries full public projection; id/name present)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-LST-CRUD-OK",
    title="GET /geo/v1/regions (viewer) → 200, regions[] non-empty + well-formed (id/name present)",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        Step(name="list-regions", method="GET", path="/geo/v1/regions",
             test_script=[
                 *assert_status(200),
                 "pm.test('regions is a non-empty array', () => {",
                 "  const j = pm.response.json();",
                 "  pm.expect(j.regions, JSON.stringify(j)).to.be.an('array');",
                 "  pm.expect(j.regions.length, 'seeded catalog non-empty').to.be.greaterThan(0);",
                 "});",
                 "pm.test('each region item is well-formed (id + name present)', () => {",
                 "  const j = pm.response.json();",
                 "  (j.regions || []).forEach(r => {",
                 "    pm.expect(r.id, 'region id: ' + JSON.stringify(r)).to.be.a('string').and.not.empty;",
                 "    pm.expect(r, 'region name present: ' + JSON.stringify(r)).to.have.property('name');",
                 "  });",
                 "});",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-GET-CRUD-OK — happy Get: discover a real id from List, then Get it → 200.
# Self-contained (no hard-coded per-deploy literal id): List → capture → Get.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-GET-CRUD-OK",
    title="GET /geo/v1/regions/{id} (viewer) → 200, id echoes, createdAt present",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        Step(name="list-for-id", method="GET", path="/geo/v1/regions",
             test_script=[
                 *assert_status(200),
                 *save_from_response("j.regions && j.regions[0] && j.regions[0].id", "regId"),
                 "pm.test('captured a seeded region id', () => pm.expect(pm.environment.get('regId')).to.be.a('string').and.not.empty);",
             ]),
        Step(name="get-region", method="GET", path="/geo/v1/regions/{{regId}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('id echoes the requested region', () => pm.expect(j.id).to.eql(pm.environment.get('regId')));",
                 "pm.test('createdAt present (truncated timestamp on wire)', () => pm.expect(j).to.have.property('createdAt'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-GET-NEG-NOTFOUND — well-formed-but-absent id → 404 NOT_FOUND, verbatim text.
# The not-found text "Region <id> not found" is the STABLE contract tone (ungated,
# already-landed per acceptance) — pinned verbatim.
# verifies GEO-1-35 (geo-direct read of absent region → NOT_FOUND "Region <id> not found")
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-GET-NEG-NOTFOUND",
    title="GET /geo/v1/regions/{absent well-formed slug} → 404 NOT_FOUND 'Region <id> not found'",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        Step(name="get-absent", method="GET", path="/geo/v1/regions/nonexistent-region-{{runId}}",
             test_script=[
                 *assert_status(404),
                 *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('verbatim text: Region <id> not found', () => "
                 "pm.expect(pm.response.json().message).to.match(/^Region .* not found$/));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-GET-VAL-MALFORMED — malformed (non-slug) id → 400 INVALID_ARGUMENT, first
# statement (format-check before repo resolve). Uppercase `INVALID` fails the slug
# charset `^[a-z][a-z0-9-]*$`. The 400/code-3 is the STABLE contract; the exact
# message text is mid-redesign (AS-IS "must be a lowercase slug …" → target "invalid
# region id …"), so the text assertion is tolerant of BOTH plus a no-leak check.
# verifies GEO-1-31 (malformed slug → INVALID_ARGUMENT first statement; code part)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-GET-VAL-MALFORMED",
    title="GET /geo/v1/regions/{malformed non-slug id} → 400 INVALID_ARGUMENT (no pgx/SQL leak)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="get-malformed", method="GET", path="/geo/v1/regions/9bad-region",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message mentions invalid/slug region id (contract tone, tolerant across redesign)', () => "
                 "pm.expect(pm.response.json().message).to.match(/(lowercase slug|invalid .*region.*id|region id)/i));",
                 "pm.test('no pgx/SQL/panic leak in message', () => {",
                 "  const m = String(pm.response.json().message || '').toLowerCase();",
                 "  ['sqlstate','pgx','panic','goroutine'].forEach(t => pm.expect(m, 'leaked ' + t).to.not.include(t));",
                 "});",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-LST-BVA-PAGESIZE-ZERO — pageSize=0 → 200 (server applies default). Boundary.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-LST-BVA-PAGESIZE-ZERO",
    title="GET /geo/v1/regions?pageSize=0 → 200 (default page size applied)",
    classes=["BVA", "PAGE"], priority="P2",
    steps=[
        Step(name="list-ps0", method="GET", path="/geo/v1/regions?pageSize=0",
             test_script=[*assert_status(200)]),
    ],
))


# ---------------------------------------------------------------------------
# REG-LST-BVA-PAGESIZE-OVER-MAX — pageSize>1000 → 400 INVALID_ARGUMENT (rejected,
# NOT clamped) with a page_size field violation.
# verifies GEO-1-27 (pageSize>1000 → INVALID_ARGUMENT, rejected not clamped)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-LST-BVA-PAGESIZE-OVER-MAX",
    title="GET /geo/v1/regions?pageSize=10000 → 400 INVALID_ARGUMENT (rejected, not clamped)",
    classes=["BVA", "VAL", "PAGE"], priority="P1",
    steps=[
        Step(name="list-ps-over", method="GET", path="/geo/v1/regions?pageSize=10000",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 *assert_field_violation("page_size"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-LST-PAGE-BADTOKEN — garbage page_token → 400 INVALID_ARGUMENT. Format-validate
# happens before the authz short-circuit (the authorized viewer reaches the backend
# decode). Stable across the redesign.
# verifies GEO-1-27 (garbage page_token → INVALID_ARGUMENT before authz short-circuit)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-LST-PAGE-BADTOKEN",
    title="GET /geo/v1/regions?pageToken=<garbage> → 400 INVALID_ARGUMENT",
    classes=["PAGE", "VAL", "NEG"], priority="P1",
    steps=[
        Step(name="list-bad-token", method="GET",
             path="/geo/v1/regions?pageSize=10&pageToken=not-a-real-token%25%25%25",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-LST-PAGE-ROUNDTRIP — pageSize=1 → capture nextPageToken → re-list with it → 200.
# Property: an opaque cursor token round-trips without error.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-LST-PAGE-ROUNDTRIP",
    title="Pagination round-trip: list pageSize=1 → follow nextPageToken → 200",
    classes=["PAGE", "BVA"], priority="P2",
    steps=[
        Step(name="list-p1", method="GET", path="/geo/v1/regions?pageSize=1",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('page has at most 1 region', () => pm.expect((j.regions || []).length).to.be.at.most(1));",
                 "pm.environment.set('regNextToken', j.nextPageToken || '');",
                 "pm.test('nextPageToken is a string', () => pm.expect(j.nextPageToken || '').to.be.a('string'));",
             ]),
        Step(name="list-p2", method="GET", path="/geo/v1/regions?pageSize=1&pageToken={{regNextToken}}",
             test_script=[*assert_status(200)]),
    ],
))


# ---------------------------------------------------------------------------
# REG-GET-CONF-NO-INFRA — two-projection / capacity-anonymization security-lock:
# the public Region body carries NO infra / placement / host-class fields. These
# live only in the Internal projection (:9091). ungated security invariant.
# verifies GEO-1-05 (host-class physically not on public), GEO-1-33 (no placementType/scope)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-GET-CONF-NO-INFRA",
    title="GET /geo/v1/regions/{id} public body has NO infra/host-class/placement fields",
    classes=["CONF", "SEC"], priority="P0",
    steps=[
        Step(name="list-for-id", method="GET", path="/geo/v1/regions",
             test_script=[
                 *assert_status(200),
                 *save_from_response("j.regions && j.regions[0] && j.regions[0].id", "regInfraId"),
             ]),
        Step(name="get-region", method="GET", path="/geo/v1/regions/{{regInfraId}}",
             test_script=[
                 *assert_status(200),
                 *assert_body_notcontains_infra(),
             ]),
        # The list body (all items) must also be leak-free — belt-and-suspenders.
        Step(name="list-nocontains", method="GET", path="/geo/v1/regions?pageSize=100",
             test_script=[
                 *assert_status(200),
                 *assert_body_notcontains_infra(),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-LST-AUTHZ-ANON-DENY — anonymous (no Bearer) → 401 UNAUTHENTICATED. authN is
# mandatory on every listener; project-scope EXEMPT (redesign) removes authZ scope,
# NOT authN. anonymous-full-access is forbidden. Stable across the redesign.
# verifies GEO-1-21 (unauthenticated → UNAUTHENTICATED; EXEMPT ≠ anonymous)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-LST-AUTHZ-ANON-DENY",
    title="GET /geo/v1/regions as anonymous (no Bearer) → 401 UNAUTHENTICATED",
    classes=["AUTHZ", "NEG"], priority="P1",
    steps=[
        Step(name="list-anon", method="GET", path="/geo/v1/regions", auth="anonymous",
             test_script=[
                 *assert_status(401),
                 *assert_grpc_code(16, "UNAUTHENTICATED"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-CR-AUTHZ-ADMIN-NOT-PUBLIC — admin write verb on the public endpoint must NOT
# mutate. Region admin-CRUD is InternalRegionService (Internal-only, security.md
# ban #6, gated system_admin). A non-admin tenant POSTing to the public
# /geo/v1/regions must be rejected (401/403/404/501) — NEVER 200/mutation. Defends
# the Internal-vs-external split at the REST boundary (black-box guard for GEO-1-17/22).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-CR-AUTHZ-ADMIN-NOT-PUBLIC",
    title="POST /geo/v1/regions (admin write) as non-admin on public endpoint → rejected, never 200",
    classes=["AUTHZ", "NEG", "SEC"], priority="P0",
    steps=[
        Step(name="admin-write-public", method="POST", path="/geo/v1/regions",
             auth="jwtNoBindings",
             body={"id": "hacked-region-{{runId}}", "name": "should-not-be-created-{{runId}}"},
             test_script=[
                 "pm.test('admin write on public endpoint rejected (401/403/404/501), never 200', () => "
                 "pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.be.oneOf([401, 403, 404, 501]));",
             ]),
    ],
))
