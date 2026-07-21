# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo public ZoneService reads through the api-gateway REST mux.

Black-box coverage of the public Zone read surface (`GET /geo/v1/zones`,
`GET /geo/v1/zones/{id}` → `ZoneService.List` / `ZoneService.Get`) through the shared
api-gateway public REST listener ({{baseUrl}}). Mirrors cases/region.py; Zone is the
zonal subdivision of a Region (the placement-axis leaf carries `regionId`).

Source of truth — APPROVED acceptance `docs/specs/sub-phase-GEO-1-region-zone-redesign-acceptance.md`
and `.claude/rules/api-conventions.md`.

Probe note (deployed contract as of redesign/integration @ 8f3dca1 — see
cases/region.py header for the full AS-IS vs GEO-1-redesign delta): the public
`Zone` is `{id, regionId, status, name, createdAt}` and reads are gated
`viewer`@`cluster` (NOT yet EXEMPT). `jwtBootstrap` (viewer-floor) reads 200,
anonymous → 401. Assertions lock the invariants stable across the redesign
boundary; the raw `status` field's projection (public in AS-IS → Internal after
GEO-1) is NOT asserted either way (forward-compatible); redesign-only deltas are
deferred and tracked in docs/TEST-PLAN.md §"Deferred — GEO-1 redesign".

Test-design techniques applied (per `testing-product-coach`):
  - ECP: valid seeded id vs well-formed-absent vs malformed-charset id;
    authenticated-viewer vs anonymous.
  - BVA: pageSize 0 / >max; page_token garbage / round-trip.
  - CONF: `Zone`/`ListZonesResponse` shape (camelCase, flat, regionId present);
    verbatim NotFound text `"Zone <id> not found"`.
  - error-guessing: infra/host-class leak probe; admin-write verb on public endpoint.

Per positive there is ≥1 matched negative. Test-only — no prod code touched (ban #13).
"""

CASES = []


# ---------------------------------------------------------------------------
# ZON-LST-CRUD-OK — happy List: 200 + non-empty zones[] of well-formed items.
# verifies GEO-1-24 (List item carries full public projection: id/regionId/name present)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-LST-CRUD-OK",
    title="GET /geo/v1/zones (viewer) → 200, zones[] non-empty + well-formed (id/regionId/name)",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        Step(name="list-zones", method="GET", path="/geo/v1/zones",
             test_script=[
                 *assert_status(200),
                 "pm.test('zones is a non-empty array', () => {",
                 "  const j = pm.response.json();",
                 "  pm.expect(j.zones, JSON.stringify(j)).to.be.an('array');",
                 "  pm.expect(j.zones.length, 'seeded catalog non-empty').to.be.greaterThan(0);",
                 "});",
                 "pm.test('each zone item is well-formed (id + regionId + name present)', () => {",
                 "  const j = pm.response.json();",
                 "  (j.zones || []).forEach(z => {",
                 "    pm.expect(z.id, 'zone id: ' + JSON.stringify(z)).to.be.a('string').and.not.empty;",
                 "    pm.expect(z.regionId, 'zone regionId: ' + JSON.stringify(z)).to.be.a('string').and.not.empty;",
                 "    pm.expect(z, 'zone name present: ' + JSON.stringify(z)).to.have.property('name');",
                 "  });",
                 "});",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-GET-CRUD-OK — happy Get: discover a real id from List, then Get it → 200.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-GET-CRUD-OK",
    title="GET /geo/v1/zones/{id} (viewer) → 200, id echoes, regionId + createdAt present",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        Step(name="list-for-id", method="GET", path="/geo/v1/zones",
             test_script=[
                 *assert_status(200),
                 *save_from_response("j.zones && j.zones[0] && j.zones[0].id", "zonId"),
                 "pm.test('captured a seeded zone id', () => pm.expect(pm.environment.get('zonId')).to.be.a('string').and.not.empty);",
             ]),
        Step(name="get-zone", method="GET", path="/geo/v1/zones/{{zonId}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('id echoes the requested zone', () => pm.expect(j.id).to.eql(pm.environment.get('zonId')));",
                 "pm.test('regionId present (zone belongs to a region)', () => pm.expect(j.regionId).to.be.a('string').and.not.empty);",
                 "pm.test('createdAt present (truncated timestamp on wire)', () => pm.expect(j).to.have.property('createdAt'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-GET-NEG-NOTFOUND — well-formed-but-absent id → 404 NOT_FOUND, verbatim text
# "Zone <id> not found" (stable contract tone, ungated/already-landed).
# verifies GEO-1-31/35 (geo-direct read of absent zone → NOT_FOUND "Zone <id> not found")
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-GET-NEG-NOTFOUND",
    title="GET /geo/v1/zones/{absent well-formed slug} → 404 NOT_FOUND 'Zone <id> not found'",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        Step(name="get-absent", method="GET", path="/geo/v1/zones/nonexistent-zone-{{runId}}",
             test_script=[
                 *assert_status(404),
                 *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('verbatim text: Zone <id> not found', () => "
                 "pm.expect(pm.response.json().message).to.match(/^Zone .* not found$/));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-GET-VAL-MALFORMED — malformed (non-slug) id → 400 INVALID_ARGUMENT, first
# statement. 400/code-3 stable; message text tolerant across the AS-IS→redesign flip.
# verifies GEO-1-31 (malformed slug → INVALID_ARGUMENT first statement; code part)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-GET-VAL-MALFORMED",
    title="GET /geo/v1/zones/{malformed non-slug id} → 400 INVALID_ARGUMENT (no pgx/SQL leak)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="get-malformed", method="GET", path="/geo/v1/zones/9bad-zone",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message mentions invalid/slug zone id (contract tone, tolerant across redesign)', () => "
                 "pm.expect(pm.response.json().message).to.match(/(lowercase slug|invalid .*zone.*id|zone id)/i));",
                 "pm.test('no pgx/SQL/panic leak in message', () => {",
                 "  const m = String(pm.response.json().message || '').toLowerCase();",
                 "  ['sqlstate','pgx','panic','goroutine'].forEach(t => pm.expect(m, 'leaked ' + t).to.not.include(t));",
                 "});",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-LST-BVA-PAGESIZE-ZERO — pageSize=0 → 200 (default applied). Boundary.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-LST-BVA-PAGESIZE-ZERO",
    title="GET /geo/v1/zones?pageSize=0 → 200 (default page size applied)",
    classes=["BVA", "PAGE"], priority="P2",
    steps=[
        Step(name="list-ps0", method="GET", path="/geo/v1/zones?pageSize=0",
             test_script=[*assert_status(200)]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-LST-BVA-PAGESIZE-OVER-MAX — pageSize>1000 → 400 INVALID_ARGUMENT (rejected).
# verifies GEO-1-27 (pageSize>1000 → INVALID_ARGUMENT, rejected not clamped)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-LST-BVA-PAGESIZE-OVER-MAX",
    title="GET /geo/v1/zones?pageSize=10000 → 400 INVALID_ARGUMENT (rejected, not clamped)",
    classes=["BVA", "VAL", "PAGE"], priority="P1",
    steps=[
        Step(name="list-ps-over", method="GET", path="/geo/v1/zones?pageSize=10000",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 *assert_field_violation("page_size"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-LST-PAGE-BADTOKEN — garbage page_token → 400 INVALID_ARGUMENT.
# verifies GEO-1-27 (garbage page_token → INVALID_ARGUMENT before authz short-circuit)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-LST-PAGE-BADTOKEN",
    title="GET /geo/v1/zones?pageToken=<garbage> → 400 INVALID_ARGUMENT",
    classes=["PAGE", "VAL", "NEG"], priority="P1",
    steps=[
        Step(name="list-bad-token", method="GET",
             path="/geo/v1/zones?pageSize=10&pageToken=not-a-real-token%25%25%25",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-LST-PAGE-ROUNDTRIP — pageSize=1 → capture nextPageToken → re-list with it → 200.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-LST-PAGE-ROUNDTRIP",
    title="Pagination round-trip: list pageSize=1 → follow nextPageToken → 200",
    classes=["PAGE", "BVA"], priority="P2",
    steps=[
        Step(name="list-p1", method="GET", path="/geo/v1/zones?pageSize=1",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('page has at most 1 zone', () => pm.expect((j.zones || []).length).to.be.at.most(1));",
                 "pm.environment.set('zonNextToken', j.nextPageToken || '');",
                 "pm.test('nextPageToken is a string', () => pm.expect(j.nextPageToken || '').to.be.a('string'));",
             ]),
        Step(name="list-p2", method="GET", path="/geo/v1/zones?pageSize=1&pageToken={{zonNextToken}}",
             test_script=[*assert_status(200)]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-GET-CONF-NO-INFRA — two-projection / capacity-anonymization security-lock:
# the public Zone body carries NO infra / placement / host-class fields (those live
# only in the Internal projection :9091). NB: the raw `status` field is NOT asserted
# here — its projection moves public→Internal across the redesign; this lock targets
# only the infra/host-class/placement fields (leak-free in BOTH states).
# verifies GEO-1-05 (host-class physically not on public), GEO-1-33 (no placementType/scope)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-GET-CONF-NO-INFRA",
    title="GET /geo/v1/zones/{id} public body has NO infra/host-class/placement fields",
    classes=["CONF", "SEC"], priority="P0",
    steps=[
        Step(name="list-for-id", method="GET", path="/geo/v1/zones",
             test_script=[
                 *assert_status(200),
                 *save_from_response("j.zones && j.zones[0] && j.zones[0].id", "zonInfraId"),
             ]),
        Step(name="get-zone", method="GET", path="/geo/v1/zones/{{zonInfraId}}",
             test_script=[
                 *assert_status(200),
                 *assert_body_notcontains_infra(),
             ]),
        Step(name="list-nocontains", method="GET", path="/geo/v1/zones?pageSize=100",
             test_script=[
                 *assert_status(200),
                 *assert_body_notcontains_infra(),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-LST-AUTHZ-ANON-DENY — anonymous (no Bearer) → 401 UNAUTHENTICATED.
# verifies GEO-1-21 (unauthenticated → UNAUTHENTICATED; EXEMPT ≠ anonymous)
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-LST-AUTHZ-ANON-DENY",
    title="GET /geo/v1/zones as anonymous (no Bearer) → 401 UNAUTHENTICATED",
    classes=["AUTHZ", "NEG"], priority="P1",
    steps=[
        Step(name="list-anon", method="GET", path="/geo/v1/zones", auth="anonymous",
             test_script=[
                 *assert_status(401),
                 *assert_grpc_code(16, "UNAUTHENTICATED"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-CR-AUTHZ-ADMIN-NOT-PUBLIC — admin write verb on the public endpoint must NOT
# mutate. Zone admin-CRUD is InternalZoneService (Internal-only, ban #6, system_admin).
# Non-admin POST to public /geo/v1/zones → rejected (401/403/404/501), never 200.
# black-box guard for GEO-1-17/22 (Internal-vs-external split at the REST boundary).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-CR-AUTHZ-ADMIN-NOT-PUBLIC",
    title="POST /geo/v1/zones (admin write) as non-admin on public endpoint → rejected, never 200",
    classes=["AUTHZ", "NEG", "SEC"], priority="P0",
    steps=[
        Step(name="admin-write-public", method="POST", path="/geo/v1/zones",
             auth="jwtNoBindings",
             body={"id": "hacked-zone-{{runId}}", "regionId": "nonexistent-region-{{runId}}",
                   "name": "should-not-be-created-{{runId}}"},
             test_script=[
                 "pm.test('admin write on public endpoint rejected (401/403/404/501), never 200', () => "
                 "pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.be.oneOf([401, 403, 404, 501]));",
             ]),
    ],
))
