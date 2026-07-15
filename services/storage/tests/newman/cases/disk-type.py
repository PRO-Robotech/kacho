# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для DiskTypeService (kacho-storage) — stage S2 CS1-S2-*.

DiskType — admin-каталог: public read (DiskTypeService.Get/List, sync) +
admin CRUD (InternalDiskTypeService.Create/Update/Delete) на :9091 (internal-mux
only, ban #6). DiskType.id — admin-assigned slug (напр. block-balanced), НЕ
генерируемый префикс.

Seed (миграция 0004): block-standard, block-balanced, block-fast, block-single,
block-io-max — все с zone_ids=[] (не ограничены зонами), performance_tier =
standard/balanced/fast/single/io-max.

Black-box coverage:
  - public read (List/Get/NotFound) — CS1-S2-01.
  - INV-7a (admin CRUD Internal-only, отсутствует на external endpoint) — CS1-S2-04.
Не-black-box (integration-only, НЕ здесь): admin Create/Update/Delete happy +
FK-RESTRICT delete-in-use (CS1-S2-02/03/05) — доступны только на :9091 mTLS через
internal-mux + per-RPC system_admin Check → покрываются integration-тестами
(testcontainers) и internal-mux ручным прогоном, не external newman.
"""

CASES = []

DT = "/storage/v1/diskTypes"
_SEEDED = ["block-standard", "block-balanced", "block-fast", "block-single", "block-io-max"]


# ---------------------------------------------------------------------------
# CS1-S2-01 — public read (List / Get / NotFound)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="DT-LST-CRUD-OK",
    title="List diskTypes → ≥5 seeded slug'ов (block-standard/-balanced/-fast/-single/-io-max), у каждого performanceTier + zoneIds array",
    classes=["CRUD"], priority="P1",
    # verifies CS1-S2-01
    steps=[Step(name="list", method="GET", path=DT,
                test_script=[*assert_status(200),
                             "const j = pm.response.json();",
                             "pm.test('diskTypes is array', () => pm.expect(j.diskTypes || []).to.be.an('array'));",
                             "const ids = (j.diskTypes || []).map(x => x.id);",
                             "pm.test('at least 5 seeded types', () => pm.expect(ids.length).to.be.at.least(5));",
                             "['block-standard','block-balanced','block-fast','block-single','block-io-max'].forEach(s => pm.test('contains ' + s, () => pm.expect(ids).to.include(s)));",
                             "pm.test('each has performanceTier + zoneIds array', () => (j.diskTypes || []).forEach(t => { pm.expect(t.performanceTier, t.id).to.be.a('string'); pm.expect(t.zoneIds || [], t.id).to.be.an('array'); }));"])],
))

CASES.append(Case(
    id="DT-GET-CRUD-OK",
    title="Get block-balanced → id=block-balanced, performanceTier=balanced, zoneIds array",
    classes=["CRUD", "CONF"], priority="P1",
    # verifies CS1-S2-01
    steps=[Step(name="get", method="GET", path=f"{DT}/block-balanced",
                test_script=[*assert_status(200),
                             "const j = pm.response.json();",
                             "pm.test('id == block-balanced', () => pm.expect(j.id).to.eql('block-balanced'));",
                             "pm.test('performanceTier == balanced', () => pm.expect(j.performanceTier).to.eql('balanced'));",
                             "pm.test('zoneIds is array', () => pm.expect(j.zoneIds || []).to.be.an('array'));"])],
))

CASES.append(Case(
    id="DT-GET-NEG-NOTFOUND",
    title="Get block-nope → 404 NOT_FOUND 'DiskType block-nope not found'",
    classes=["NEG", "CONF"], priority="P0",
    # verifies CS1-S2-01
    steps=[Step(name="get-nx", method="GET", path=f"{DT}/block-nope",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             "pm.test('message includes \"DiskType block-nope not found\"', () => pm.expect((pm.response.json().message || ''), JSON.stringify(pm.response.json())).to.include('DiskType block-nope not found'));"])],
))

CASES.append(Case(
    id="DT-LST-BVA-PAGESIZE-OVER-MAX",
    title="List diskTypes pageSize=1001 (> max 1000) → 400 INVALID_ARGUMENT",
    classes=["BVA", "VAL", "PAGE"], priority="P1",
    # verifies CS1-S2-01
    steps=[Step(name="ps-over", method="GET", path=f"{DT}?pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# ---------------------------------------------------------------------------
# CS1-S2-04 — admin CRUD Internal-only: absent on external endpoint (INV-7a)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="DT-CR-NEG-EXTERNAL-ABSENT",
    title="POST /storage/v1/diskTypes на external → route absent (admin Create Internal-only :9091, ban #6) → 404/405/501",
    classes=["SEC", "NEG", "AUTHZ"], priority="P0",
    # verifies CS1-S2-04 (INV-7a: admin CRUD not routed on external mux)
    steps=[Step(name="cr-external", method="POST", path=DT,
                body={"id": "block-newman-fake", "name": "block-newman-fake", "performanceTier": "balanced"},
                test_script=["pm.test('admin Create not on external endpoint', () => pm.expect(pm.response.code).to.be.oneOf([404, 405, 501]));"])],
))

CASES.append(Case(
    id="DT-UPD-NEG-EXTERNAL-ABSENT",
    title="PATCH /storage/v1/diskTypes/block-balanced на external → route absent (admin Update Internal-only) → 404/405/501",
    classes=["SEC", "NEG", "AUTHZ"], priority="P0",
    # verifies CS1-S2-04 (INV-7a)
    steps=[Step(name="upd-external", method="PATCH", path=f"{DT}/block-balanced",
                body={"name": "block-hacked"},
                test_script=["pm.test('admin Update not on external endpoint', () => pm.expect(pm.response.code).to.be.oneOf([404, 405, 501]));"])],
))

CASES.append(Case(
    id="DT-DEL-NEG-EXTERNAL-ABSENT",
    title="DELETE /storage/v1/diskTypes/block-balanced на external → route absent (admin Delete Internal-only) → 404/405/501",
    classes=["SEC", "NEG", "AUTHZ"], priority="P0",
    # verifies CS1-S2-04 (INV-7a)
    steps=[Step(name="del-external", method="DELETE", path=f"{DT}/block-balanced",
                test_script=["pm.test('admin Delete not on external endpoint', () => pm.expect(pm.response.code).to.be.oneOf([404, 405, 501]));"])],
))
