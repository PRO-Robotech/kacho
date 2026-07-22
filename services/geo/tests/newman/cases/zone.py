# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo public ZoneService (Get/List) через api-gateway.

Публичная read-only поверхность каталога зон: sync, ambient-read (project-scope
EXEMPT — любой аутентифицированный принципал читает каталог, GEO-1-20). GEO-1
landed-контракт: Zone — плоский flat-resource {id, regionId, name, createdAt,
openForPlacement°, placementBlockedReason°}. Сырой admin-флаг `status` вынесен в
two-projection (InternalZone :9091, НЕ на public); единственный публичный
placement-сигнал — derived `openForPlacement°` (= zone.status==UP && region.status==UP).

Инвариант two-projection: public Zone НЕ несёт ни `status`, ни ИНФРА-полей
(numericInfraId/hostClasses/...) — их и асертим (ZON-GET-CONF-NO-INFRA).

Источник контракта: proto/kacho/cloud/geo/v1/zone.proto + zone_service.proto +
services/geo/internal/apps/kacho/api/zone + GEO-1 acceptance (F1/F2). ZoneService.List
несёт фильтры `?regionId`/`?openForPlacement` (GEO-1-24/26) — здесь покрыт базовый
list; фильтр-кейсы — расширяемая поверхность.

Test-design техники: CONF (shape/verbatim-NotFound/createdAt-truncate/no-infra/
no-status), ECP (valid/malformed/absent id), BVA (pageSize 0/1/>max, garbage token),
error-guessing (503/code14 dial regression). Каждый positive → matched negative.
"""

CASES = []


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
# GEO-ZON-GT-CONF-OK — authenticated GET /geo/v1/zones → 200 + seeded zones.
# Мигрировано из services/iam/tests/newman/cases/geo-read.py.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-ZON-GT-CONF-OK",
    title="GET /geo/v1/zones as jwtBootstrap → 200, zones[] non-empty + well-formed (id+openForPlacement), not 503/code14",
    classes=["CONF", "CRUD"], priority="P0",
    steps=[
        Step(
            name="list-zones-auth",
            method="GET",
            path="/geo/v1/zones",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                *_not_no_children_503(),
                "pm.test('zones is a non-empty array', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j.zones, JSON.stringify(j)).to.be.an('array');",
                "  pm.expect(j.zones.length, 'zones non-empty (geography seeded)').to.be.greaterThan(0);",
                "});",
                "pm.test('zones are well-formed (id + openForPlacement present; NO raw status)', () => {",
                "  (pm.response.json().zones || []).forEach(z => {",
                "    pm.expect(z.id, 'zone id: ' + JSON.stringify(z)).to.be.a('string').and.not.empty;",
                "    pm.expect(z.openForPlacement, 'zone openForPlacement: ' + JSON.stringify(z)).to.be.a('boolean');",
                "    pm.expect(z, 'zone must NOT carry raw status: ' + JSON.stringify(z)).to.not.have.property('status');",
                "  });",
                "});",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# ZON-GET-CRUD-OK — resolve an existing zone id from List, then Get it.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-GET-CRUD-OK",
    title="Get existing zone (resolved from List) → 200, flat shape {id,regionId,name,openForPlacement,placementBlockedReason,createdAt}, createdAt truncated",
    classes=["CRUD", "CONF"], priority="P0",
    steps=[
        Step(name="list-pick", method="GET", path="/geo/v1/zones",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('zones non-empty (geography seeded)', () => pm.expect((j.zones||[]).length).to.be.greaterThan(0));",
                 # Resolve a STABLE seed zone, not an arbitrary [0]: the internal-*
                 # admin suites create/delete transient `qa`-prefixed rows in this
                 # GLOBAL (non-project-scoped) catalog concurrently, so [0] can be a
                 # row that vanishes before get-zone. Prefer a non-`qa` seed row.
                 *save_from_response("(j.zones.find(z => !String(z.id).startsWith('qa')) || j.zones[0]).id", "pickZoneId"),
             ]),
        Step(name="get-zone", method="GET", path="/geo/v1/zones/{{pickZoneId}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('id matches resolved', () => pm.expect(j.id).to.eql(pm.environment.get('pickZoneId')));",
                 "pm.test('regionId is a non-empty string', () => pm.expect(j.regionId).to.be.a('string').and.not.empty);",
                 "pm.test('openForPlacement is a boolean (derived signal)', () => pm.expect(j.openForPlacement).to.be.a('boolean'));",
                 "pm.test('placementBlockedReason present', () => pm.expect(j.placementBlockedReason).to.not.be.undefined);",
                 "pm.test('public Zone carries NO raw status (two-projection)', () => pm.expect(j).to.not.have.property('status'));",
                 *assert_createdat_truncated(),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-GET-CONF-NO-INFRA — two-projection: public Zone carries NO infra fields
# (status IS present AS-IS; the invariant that holds now AND post-GEO-1 is: no infra).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-GET-CONF-NO-INFRA",
    title="Public Zone.Get carries NO infra fields (numericInfraId/hostClasses/underlayAnchor/... — two-projection invariant)",
    classes=["CONF"], priority="P1",
    steps=[
        Step(name="list-pick", method="GET", path="/geo/v1/zones",
             test_script=[
                 *assert_status(200),
                 "pm.test('zones non-empty', () => pm.expect((pm.response.json().zones||[]).length).to.be.greaterThan(0));",
                 *save_from_response("(pm.response.json().zones.find(z => !String(z.id).startsWith('qa')) || pm.response.json().zones[0]).id", "pickZoneId"),
             ]),
        Step(name="get-zone", method="GET", path="/geo/v1/zones/{{pickZoneId}}",
             test_script=[
                 *assert_status(200),
                 *assert_no_infra_fields(),
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-GET-VAL-MALFORMED — malformed slug id → sync InvalidArgument (first statement).
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-GET-VAL-MALFORMED",
    title="Get with malformed (non-slug) zone id → 400 InvalidArgument (first statement)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="get-malformed", method="GET", path="/geo/v1/zones/{{malformedId}}",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message references zone id', () => pm.expect(String(pm.response.json().message)).to.include('zone id'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# ZON-GET-NEG-NOTFOUND — well-formed-but-absent id → 404 verbatim contract text.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-GET-NEG-NOTFOUND",
    title="Get well-formed-but-absent zone id → 404 NOT_FOUND, verbatim 'Zone <id> not found'",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        Step(name="get-absent", method="GET", path="/geo/v1/zones/{{garbageZoneId}}",
             test_script=[
                 *assert_status(404),
                 *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('verbatim NotFound text', () => pm.expect(pm.response.json().message).to.eql('Zone ' + pm.environment.get('garbageZoneId') + ' not found'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# Pagination BVA.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="ZON-LST-BVA-PAGESIZE-ZERO",
    title="List pageSize=0 → default applied (200)",
    classes=["BVA", "PAGE"], priority="P2",
    steps=[Step(name="list-ps0", method="GET", path="/geo/v1/zones?pageSize=0",
                test_script=[*assert_status(200)])],
))

CASES.append(Case(
    id="ZON-LST-BVA-PAGESIZE-OVER-MAX",
    title="List pageSize=10000 (>1000 max) → 400 InvalidArgument (rejected, not clamped)",
    classes=["BVA", "VAL"], priority="P1",
    steps=[Step(name="list-ps-huge", method="GET", path="/geo/v1/zones?pageSize=10000",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="ZON-LST-PAGE-TOKEN-GARBAGE",
    title="List with garbage page_token → 400 InvalidArgument",
    classes=["PAGE", "VAL"], priority="P1",
    steps=[Step(name="list-bad-token", method="GET",
                path="/geo/v1/zones?pageSize=10&pageToken=%25%25not-base64%25%25",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="ZON-LST-PAGE-ROUNDTRIP",
    title="Pagination round-trip: pageSize=1 → nextPageToken → next page 200",
    classes=["PAGE", "CRUD"], priority="P2",
    steps=[
        Step(name="list-p1", method="GET", path="/geo/v1/zones?pageSize=1",
             test_script=[*assert_status(200),
                          "const tok = pm.response.json().nextPageToken || '';",
                          "pm.environment.set('zonNextToken', tok);",
                          "pm.test('token is string', () => pm.expect(tok).to.be.a('string'));"]),
        Step(name="list-p2", method="GET", path="/geo/v1/zones?pageSize=1&pageToken={{zonNextToken}}",
             test_script=[*assert_status(200)]),
    ],
))
