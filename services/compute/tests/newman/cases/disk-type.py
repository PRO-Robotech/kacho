# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для DiskTypeService (kacho-compute) — read-only справочник.

Covered RPCs (public): Get, List. Admin CRUD (Create/Update/Delete) is the Internal
InternalDiskTypeService (ban #6) — not part of the public surface. Seed: network-hdd,
network-ssd, network-ssd-nonreplicated, network-ssd-io-m3 (internal/migrations/0001_initial.sql).
"""

CASES = []

DT = "/compute/v1/diskTypes"
_SEEDED = ["network-hdd", "network-ssd", "network-ssd-nonreplicated", "network-ssd-io-m3"]


CASES.append(Case(
    id="DT-LST-CRUD-OK",
    title="List diskTypes → ≥4 типов, содержит network-ssd / network-hdd; у каждого seeded-типа zoneIds непустой",
    classes=["CRUD"], priority="P1",
    steps=[Step(name="list", method="GET", path=DT,
                test_script=[*assert_status(200),
                             "const j = pm.response.json();",
                             "pm.test('diskTypes is array', () => pm.expect(j.diskTypes || []).to.be.an('array'));",
                             "const ids = (j.diskTypes || []).map(x => x.id);",
                             "pm.test('at least 4 disk types', () => pm.expect(ids.length).to.be.at.least(4));",
                             "pm.test('contains network-ssd', () => pm.expect(ids).to.include('network-ssd'));",
                             "pm.test('contains network-hdd', () => pm.expect(ids).to.include('network-hdd'));",
                             # zoneIds is asserted ONLY on the migration-seeded reference types.
                             # DiskType.Create is an admin Internal RPC (ban #6) that permits a
                             # type with empty zone_ids (DB DEFAULT '[]'); an admin/test-created
                             # zoneless type is a legit catalog state, so a blanket "every type
                             # has zoneIds" is over-strict and flakes on it. The seeded reference
                             # set (0001_initial.sql) is guaranteed multi-zone.
                             f"const seeded = {_SEEDED!r};",
                             "pm.test('each seeded reference type has non-empty zoneIds', () => (j.diskTypes || []).filter(t => seeded.includes(t.id)).forEach(t => pm.expect((t.zoneIds || []).length, t.id).to.be.at.least(1)));"])],
))

CASES.append(Case(
    id="DT-GET-CRUD-OK",
    title="Get network-ssd → id == network-ssd, zoneIds содержит ru-central1-a",
    classes=["CRUD"], priority="P1",
    steps=[Step(name="get", method="GET", path=f"{DT}/{{{{existingDiskTypeId}}}}",
                test_script=[*assert_status(200),
                             "const j = pm.response.json();",
                             "pm.test('id matches', () => pm.expect(j.id).to.eql(pm.environment.get('existingDiskTypeId')));",
                             "pm.test('zoneIds non-empty', () => pm.expect((j.zoneIds || []).length).to.be.at.least(1));",
                             "pm.test('zoneIds contains existingZoneId', () => pm.expect(j.zoneIds || []).to.include(pm.environment.get('existingZoneId')));"])],
))

CASES.append(Case(
    id="DT-GET-CRUD-HDD-OK",
    title="Get network-hdd (другой seeded тип) → id matches",
    classes=["CRUD"], priority="P2",
    steps=[Step(name="get-hdd", method="GET", path=f"{DT}/network-hdd",
                test_script=[*assert_status(200),
                             "pm.test('id == network-hdd', () => pm.expect(pm.response.json().id).to.eql('network-hdd'));"])],
))

CASES.append(Case(
    id="DT-GET-NEG-NOTFOUND",
    title="Get garbage diskTypeId → 404 NOT_FOUND",
    classes=["NEG"], priority="P0",
    steps=[Step(name="get-nx", method="GET", path=f"{DT}/garbage-disk-type-xyz",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")])],
))

CASES.append(Case(
    id="DT-GET-CONF-NF-TEXT",
    title="Get garbage diskTypeId → текст содержит 'not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[Step(name="get-nx", method="GET", path=f"{DT}/garbage-disk-type-xyz",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             # contract tone (api-conventions): "<Resource> %s not found"
                             "pm.test('text mentions not found', () => pm.expect((pm.response.json().message || '').toLowerCase()).to.include('not found'));"])],
))

CASES.append(Case(
    id="DT-LST-BVA-PAGESIZE-1",
    title="List diskTypes pageSize=1 → ≤1 item",
    classes=["BVA", "PAGE"], priority="P2",
    steps=[Step(name="ps1", method="GET", path=f"{DT}?pageSize=1",
                test_script=[*assert_status(200),
                             "pm.test('at most 1 item', () => pm.expect((pm.response.json().diskTypes || []).length).to.be.at.most(1));"])],
))

CASES.append(Case(
    id="DT-LST-BVA-PAGESIZE-ZERO",
    title="List diskTypes pageSize=0 → default applied (200)",
    classes=["BVA", "PAGE"], priority="P2",
    steps=[Step(name="ps0", method="GET", path=f"{DT}?pageSize=0",
                test_script=[*assert_status(200)])],
))

CASES.append(Case(
    id="DT-LST-BVA-PAGESIZE-OVER-1001",
    title="List diskTypes pageSize=1001 → 400 InvalidArgument",
    classes=["BVA", "VAL"], priority="P1",
    steps=[Step(name="ps1001", method="GET", path=f"{DT}?pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="DT-LST-PAGE-TOKEN-GARBAGE",
    title="List diskTypes с garbage pageToken → 400 InvalidArgument или 200 (справочник мал)",
    classes=["PAGE", "VAL"], priority="P2",
    steps=[Step(name="bad-token", method="GET", path=f"{DT}?pageSize=2&pageToken=not-a-real-token",
                # probe-needed: возможно справочник игнорирует pageToken; allow 200|400
                test_script=["pm.test('200 or 400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])],
))

CASES.append(Case(
    # DiskType.Create is an admin-only Internal RPC (InternalDiskTypeService, ban #6),
    # exposed on the cluster-internal listener (:9091) and bridged to POST
    # /compute/v1/diskTypes ONLY via the api-gateway internal mux. It is NOT read-only:
    # the earlier `POST {id:"newman-fake-type"}` DID materialize a persistent zoneless
    # disk type through the umbrella's combined mux (200 first run → 409 ALREADY_EXISTS
    # on reruns) and polluted DT-LST-CRUD-OK. This negative therefore does NOT mutate:
    # empty id must be rejected by the admin Create's input validation ("id required" →
    # INVALID_ARGUMENT, 400) BEFORE any insert. On a public-only mux (no internal bridge)
    # the route is absent (ban #6) → 404. Both are correct product behaviour; neither
    # creates a row, so the case is deterministic across reruns and never pollutes.
    id="DT-CR-NEG-EMPTY-ID",
    title="POST /compute/v1/diskTypes with empty id → 400 INVALID_ARGUMENT (admin Create validates) или 404 (route not on this mux, ban #6)",
    classes=["VAL", "NEG"], priority="P3",
    steps=[Step(name="cr-dt-empty", method="POST", path=DT, body={"id": ""},
                test_script=[
                    "pm.test('rejected — no mutation', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));",
                    "if (pm.response.code === 400) { pm.test('INVALID_ARGUMENT (id required)', () => pm.expect(pm.response.json().code).to.eql(3)); }"])],
))
