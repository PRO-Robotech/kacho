# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo public RegionService (Get/List) через api-gateway.

Публичная read-only поверхность каталога регионов: sync, гейтится `viewer`@cluster
(jwtBootstrap несёт system_viewer). Region — плоский flat-resource {id, name,
createdAt} (AS-IS: инфра-полей нет by construction — two-projection инвариант).

Источник контракта (AS-IS, IMPLEMENTED — НЕ GEO-1-redesign, который merge-gated и
НЕ приземлён): proto/kacho/cloud/geo/v1/region.proto + region_service.proto,
services/geo/internal/apps/kacho/api/region + repo/kacho/{pg,dberr}. Сверено с
.claude/rules/api-conventions.md (flat-resource, коды, тон ошибок, timestamp-
truncate, pagination). GEO-1 (openForPlacement°/countryCode°/two-projection-move-
of-status) в этом суите НЕ покрывается — surface не существует (см. docs/RESULTS.md).

Test-design техники:
  * CONF (conformance): shape ListRegionsResponse/Region (camelCase, flat), verbatim
    NotFound-текст, createdAt truncate-to-seconds, two-projection (нет infra на public).
  * ECP: valid-existing vs malformed vs well-formed-absent id; authenticated-viewer
    vs anonymous (negative — см. authz-deny.py).
  * BVA: pageSize границы 0 / 1 / >max(10000); garbage page_token.
  * error-guessing: 503/grpc-code-14 regression (gateway→geo mTLS/DNS dial bug,
    мигрировано из iam geo-read.py — асертим NEGATIVE о failure mode).

Каждый positive (Get/List happy) имеет matched negative (malformed/absent/pagesize)
в этом же файле; anonymous-deny — в authz-deny.py.
"""

CASES = []


# Shared regression guard (мигрировано из iam geo-read.py): gateway→geo dial bug
# (wrong DNS host + disabled mTLS edge) отдавал 503 / grpc code 14 "no children to
# pick from". Асертим NEGATIVE о failure mode на КАЖДОМ authenticated read.
def _not_no_children_503():
    return [
        "pm.test('REGRESSION: not 503 (gateway->geo no-children dial)', () => {",
        "  pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.not.equal(503);",
        "});",
        "pm.test('REGRESSION: body not grpc code 14 (UNAVAILABLE no-children)', () => {",
        "  let j; try { j = pm.response.json(); } catch (e) { j = null; }",
        "  if (j && typeof j.code !== 'undefined') {",
        "    pm.expect(j.code, JSON.stringify(j)).to.not.equal(14);",
        "  }",
        "});",
    ]


# ---------------------------------------------------------------------------
# GEO-REG-GT-CONF-OK — authenticated GET /geo/v1/regions → 200 + seeded regions.
# Мигрировано из services/iam/tests/newman/cases/geo-read.py (geo больше не
# «безнадзорный»). Primary read-happy + no-children-503 regression.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-REG-GT-CONF-OK",
    title="GET /geo/v1/regions as jwtBootstrap → 200, regions[] non-empty + well-formed, not 503/code14",
    classes=["CONF", "CRUD"], priority="P0",
    steps=[
        Step(
            name="list-regions-auth",
            method="GET",
            path="/geo/v1/regions",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                *_not_no_children_503(),
                "pm.test('regions is a non-empty array', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j.regions, JSON.stringify(j)).to.be.an('array');",
                "  pm.expect(j.regions.length, 'regions non-empty (geography seeded)').to.be.greaterThan(0);",
                "});",
                "pm.test('regions are well-formed (id present)', () => {",
                "  (pm.response.json().regions || []).forEach(r => {",
                "    pm.expect(r.id, 'region id: ' + JSON.stringify(r)).to.be.a('string').and.not.empty;",
                "  });",
                "});",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# REG-GET-CRUD-OK — resolve an existing region id from List, then Get it → 200,
# flat-resource shape + createdAt truncate-to-seconds.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-GET-CRUD-OK",
    title="Get existing region (resolved from List) → 200, flat shape {id,name,createdAt}, createdAt truncated",
    classes=["CRUD", "CONF"], priority="P0",
    steps=[
        Step(name="list-pick", method="GET", path="/geo/v1/regions",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('regions non-empty (geography seeded)', () => pm.expect((j.regions||[]).length).to.be.greaterThan(0));",
                 *save_from_response("j.regions[0].id", "pickRegionId"),
             ]),
        Step(name="get-region", method="GET", path="/geo/v1/regions/{{pickRegionId}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('id matches resolved', () => pm.expect(j.id).to.eql(pm.environment.get('pickRegionId')));",
                 "pm.test('name is a string', () => pm.expect(j.name).to.be.a('string'));",
                 *assert_createdat_truncated(),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-GET-CONF-NO-INFRA — two-projection: public Region carries NO infra fields.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-GET-CONF-NO-INFRA",
    title="Public Region.Get carries NO infra fields (numericInfraId/infra/hostClasses/... — two-projection invariant)",
    classes=["CONF"], priority="P1",
    steps=[
        Step(name="list-pick", method="GET", path="/geo/v1/regions",
             test_script=[
                 *assert_status(200),
                 "pm.test('regions non-empty', () => pm.expect((pm.response.json().regions||[]).length).to.be.greaterThan(0));",
                 *save_from_response("pm.response.json().regions[0].id", "pickRegionId"),
             ]),
        Step(name="get-region", method="GET", path="/geo/v1/regions/{{pickRegionId}}",
             test_script=[
                 *assert_status(200),
                 *assert_no_infra_fields(),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-GET-VAL-MALFORMED — malformed slug id → sync InvalidArgument (first statement).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-GET-VAL-MALFORMED",
    title="Get with malformed (non-slug) region id → 400 InvalidArgument (first statement, before repo.Get)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="get-malformed", method="GET", path="/geo/v1/regions/{{malformedId}}",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message references region id', () => pm.expect(String(pm.response.json().message)).to.include('region id'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-GET-VAL-ID-TOOLONG — over-length id → InvalidArgument (BVA on id length).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-GET-VAL-ID-TOOLONG",
    title="Get with over-length region id (64 chars) → 400 InvalidArgument",
    classes=["VAL", "BVA", "NEG"], priority="P2",
    steps=[
        Step(name="get-toolong", method="GET", path="/geo/v1/regions/" + ("a" * 64),
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# REG-GET-NEG-NOTFOUND — well-formed-but-absent id → 404 verbatim contract text.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-GET-NEG-NOTFOUND",
    title="Get well-formed-but-absent region id → 404 NOT_FOUND, verbatim 'Region <id> not found'",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        Step(name="get-absent", method="GET", path="/geo/v1/regions/{{garbageRegionId}}",
             test_script=[
                 *assert_status(404),
                 *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('verbatim NotFound text', () => pm.expect(pm.response.json().message).to.eql('Region ' + pm.environment.get('garbageRegionId') + ' not found'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# Pagination BVA (ECP/BVA on pageSize + page_token) — validated BEFORE any authz
# short-circuit (geo List has no per-object listauthz; validation always reached).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-LST-BVA-PAGESIZE-ZERO",
    title="List pageSize=0 → default applied (200)",
    classes=["BVA", "PAGE"], priority="P2",
    steps=[Step(name="list-ps0", method="GET", path="/geo/v1/regions?pageSize=0",
                test_script=[*assert_status(200)])],
))

CASES.append(Case(
    id="REG-LST-BVA-PAGESIZE-OVER-MAX",
    title="List pageSize=10000 (>1000 max) → 400 InvalidArgument (rejected, not clamped)",
    classes=["BVA", "VAL"], priority="P1",
    steps=[Step(name="list-ps-huge", method="GET", path="/geo/v1/regions?pageSize=10000",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="REG-LST-PAGE-TOKEN-GARBAGE",
    title="List with garbage page_token → 400 InvalidArgument",
    classes=["PAGE", "VAL"], priority="P1",
    steps=[Step(name="list-bad-token", method="GET",
                path="/geo/v1/regions?pageSize=10&pageToken=%25%25not-base64%25%25",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="REG-LST-BVA-PAGESIZE-ONE",
    title="List pageSize=1 → at most 1 region (BVA lower bound)",
    classes=["BVA", "PAGE"], priority="P2",
    steps=[Step(name="list-ps1", method="GET", path="/geo/v1/regions?pageSize=1",
                test_script=[*assert_status(200),
                             "pm.test('at most 1 region', () => pm.expect((pm.response.json().regions||[]).length).to.be.at.most(1));"])],
))

CASES.append(Case(
    id="REG-LST-PAGE-ROUNDTRIP",
    title="Pagination round-trip: pageSize=1 → nextPageToken → next page 200",
    classes=["PAGE", "CRUD"], priority="P2",
    steps=[
        Step(name="list-p1", method="GET", path="/geo/v1/regions?pageSize=1",
             test_script=[*assert_status(200),
                          "const tok = pm.response.json().nextPageToken || '';",
                          "pm.environment.set('regNextToken', tok);",
                          "pm.test('token is string', () => pm.expect(tok).to.be.a('string'));"]),
        Step(name="list-p2", method="GET", path="/geo/v1/regions?pageSize=1&pageToken={{regNextToken}}",
             test_script=[*assert_status(200)]),
    ],
))
