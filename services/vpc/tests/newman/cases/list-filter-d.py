# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set list-filter-d для kacho-vpc — per-object filtered List.

Black-box проверка: `List<Resource>` отдает ТОЛЬКО доступные объекты (per-object FGA
BatchCheck на id прочитанной страницы, viewer ∪ v_list), НЕ all-or-nothing; read==enforce
(List-видимость == Get-allow); no-leak (объект вне гранта отсутствует в List И Get→404,
НЕ 403).

Pre-conditions (готовит `tests/authz-fixtures/setup.sh` на стенде). Setup патчит
env-файл, добавляя:
  - jwtSubnetSubsetViewer : Bearer subject S с per-object (resourceNames) viewer-грантом
                            на ПОДМНОЖЕСТВО subnet'ов проекта (не на весь проект).
  - jwtNoSubnetGrant      : Bearer subject без какого-либо гранта на subnet'ы проекта.
  - listFilterProjectId   : project, в котором живут seed-subnet'ы.
  - subnetVisibleId       : subnet, входящий в грант S (должен быть виден).
  - subnetHiddenId        : subnet того же проекта, НЕ входящий в грант S (no-leak).

Семантика проверок:
  - SUBNET-LF-D-VISIBLE : S → List subnets содержит subnetVisibleId.
  - SUBNET-LF-D-NOLEAK  : S → List subnets НЕ содержит subnetHiddenId (no-leak).
  - SUBNET-LF-D-GET-404 : S → Get(subnetHiddenId) → 404 NOT_FOUND (НЕ 403; не
                          подтверждаем existence чужого объекта — read==enforce no-leak).
  - SUBNET-LF-D-GET-OK  : S → Get(subnetVisibleId) → 200 (read==enforce: видим в List ⇒ Get-allow).
  - SUBNET-LF-D-NONE    : subject без гранта → List subnets пуст (НЕ весь проект).

Helpers Case/Step/assert_status инжектятся через gen.py namespace.

# requires tests/authz-fixtures/setup.sh (rules-model per-object grant fixtures)
"""

CASES = []


def _list_subnets_step(name, auth, test_script):
    # Bounded read-your-writes retry over the GRANT-tuple materialization window: the
    # per-object subnet grants are seeded by tests/authz-fixtures/setup.sh and their FGA
    # tuples become visible eventually-consistent. The subject's FIRST List can briefly
    # 403 at the authz gate before the grant tuple is visible (same eventual-consistency
    # class as owner-tuple lag). retry_until_authorized retries SELF on 403/404 then runs
    # the real assertion once (a genuine, non-converging deny still FAILS — not masked).
    return retry_until_authorized(Step(
        name=name,
        method="GET",
        path="/vpc/v1/subnets?projectId={{listFilterProjectId}}&pageSize=1000",
        auth=auth,
        test_script=test_script,
    ))


# The List gate must be stated, not alternated over.
#
# These three cases used to open with
#   `pm.expect(pm.response.code).to.be.oneOf([200, 403])`
# and put every invariant behind `if (pm.response.code === 200)`. On this endpoint,
# for an authenticated subject with a well-formed projectId, 200 and 403 are the
# WHOLE outcome space — allow and deny. An assertion that accepts both accepts
# everything, so the case could not fail; and 403 is not some neutral "no fixture
# here" lane, it is the exact condition these cases exist to detect. Measured
# 2026-07-28: every poll came back 403 and all three still reported themselves
# satisfied. The whitelist that sat on top was removed the same day precisely
# because it was subtracting nothing.
#
# The two Get canaries in this file never had the alternation and never needed it:
# they assert `.to.eql(404)` / `.to.eql(200)` outright. This brings List to the same
# footing — the deny is now visible, whatever its cause turns out to be.
_LIST_REACHABLE = [
    "pm.test('List of the granted project is reachable (per-object filtering is what "
    "is under test, so the method gate must not answer for it)', () => "
    "pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.eql(200));",
]


# SUBNET-LF-D-VISIBLE: granted subnet appears in the filtered List.
CASES.append(Case(
    id="SUBNET-LF-D-VISIBLE",
    title="per-object List: subject sees the subnet it was granted (D-40/D-41/D-43)",
    classes=["AUTHZ", "CONF"],
    priority="P0",
    steps=[
        _list_subnets_step(
            "List subnets as subset-viewer",
            "jwtSubnetSubsetViewer",
            [
                *_LIST_REACHABLE,
                "const ids = (pm.response.json().subnets || []).map(s => s.id);",
                "pm.test('[SUBNET-LF-D-VISIBLE] granted subnet present', () => "
                "pm.expect(ids, JSON.stringify(ids)).to.include(pm.environment.get('subnetVisibleId')));",
            ],
        ),
    ],
))

# SUBNET-LF-D-NOLEAK: non-granted subnet of the same project is absent (no-leak).
CASES.append(Case(
    id="SUBNET-LF-D-NOLEAK",
    title="per-object List no-leak: non-granted subnet absent from List (D-44/LST-5)",
    classes=["AUTHZ", "NEG"],
    priority="P0",
    steps=[
        _list_subnets_step(
            "List subnets as subset-viewer (hidden absent)",
            "jwtSubnetSubsetViewer",
            [
                # A 403 is trivially "no leak" — nothing was listed — which is why accepting it
                # made this case unfalsifiable. No-leak is only a statement about a list that
                # was actually produced.
                *_LIST_REACHABLE,
                "const ids = (pm.response.json().subnets || []).map(s => s.id);",
                "pm.test('[SUBNET-LF-D-NOLEAK] hidden subnet absent', () => "
                "pm.expect(ids, JSON.stringify(ids)).to.not.include(pm.environment.get('subnetHiddenId')));",
            ],
        ),
    ],
))

# SUBNET-LF-D-GET-404: Get on non-granted subnet → 404 (no-leak, NOT 403).
CASES.append(Case(
    id="SUBNET-LF-D-GET-404",
    title="per-object no-leak: Get(hidden) → 404 NotFound, not 403 (D-44/LST-5)",
    classes=["AUTHZ", "NEG"],
    priority="P0",
    steps=[
        Step(
            name="Get hidden subnet as subset-viewer",
            method="GET",
            path="/vpc/v1/subnets/{{subnetHiddenId}}",
            auth="jwtSubnetSubsetViewer",
            test_script=[
                "pm.test('[SUBNET-LF-D-GET-404] status 404 (no-leak, not 403)', () => "
                "pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.eql(404));",
                "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
                "pm.test('[SUBNET-LF-D-GET-404] grpc code 5 (NOT_FOUND)', () => "
                "pm.expect(j && j.code, JSON.stringify(j)).to.eql(5));",
            ],
        ),
    ],
))

# SUBNET-LF-D-GET-OK: Get on granted subnet → 200 (read==enforce parity).
CASES.append(Case(
    id="SUBNET-LF-D-GET-OK",
    title="read==enforce: Get(visible) → 200 (parity with List visibility, D-45)",
    classes=["AUTHZ", "CONF"],
    priority="P1",
    steps=[
        Step(
            name="Get visible subnet as subset-viewer",
            method="GET",
            path="/vpc/v1/subnets/{{subnetVisibleId}}",
            auth="jwtSubnetSubsetViewer",
            test_script=[
                "pm.test('[SUBNET-LF-D-GET-OK] status 200', () => "
                "pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.eql(200));",
            ],
        ),
    ],
))

# SUBNET-LF-D-NONE: subject with no subnet grant → empty List (not the whole project).
CASES.append(Case(
    id="SUBNET-LF-D-NONE",
    title="per-object List: no grant → empty list, not whole project (D-44)",
    classes=["AUTHZ", "NEG"],
    priority="P1",
    steps=[
        _list_subnets_step(
            "List subnets as no-grant subject",
            "jwtNoSubnetGrant",
            [
                # N holds the project-level method gate but NO per-object subnet grant → the List
                # must come back EMPTY, which is a different statement from "the List was refused".
                # Only the first one says per-object visibility does not cascade from project
                # scope; accepting a 403 alongside it said nothing at all.
                *_LIST_REACHABLE,
                "const ids = (pm.response.json().subnets || []).map(s => s.id);",
                "pm.test('[SUBNET-LF-D-NONE] empty (no per-object grant → no rows)', () => "
                "pm.expect(ids.length, JSON.stringify(ids)).to.eql(0));",
            ],
        ),
    ],
))
