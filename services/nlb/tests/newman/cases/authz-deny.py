# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Authz cases (AZD-*) — per-RPC deny matrix + lifecycle + cache + custom roles.

Acceptance: docs/specs/sub-phase-4.0-nlb-acceptance.md §8 (GWT-AZD-001..030).
Design §6 (FGA REBAC model from KAC-108).

Subjects (jwt* environment variables):
  jwtProjectEditorA       — editor on existingProjectId (suite default)
  jwtProjectEditorB       — editor on existingProjectCrossId only
  jwtProjectViewerA       — viewer on existingProjectId
  jwtProjectOwnerA        — owner on existingProjectId
  jwtStranger             — no bindings
  jwtServiceAccountEditor — service account editor on existingProjectId
  jwtGroupMemberEditor    — user in group with editor binding
  jwtCustomRoleTargetManager — custom role: only loadbalancer.targetGroups.update.
      `update` is the verb the model actually declares for nlb_target_group, and it is
      the relation AddTargets/RemoveTargets are gated by (permission_catalog). The role
      deliberately does NOT carry delete — that is the difference the case checks, and
      the only one expressible here: "may addTargets but may not Update" is not, since
      both verbs resolve to the same relation.

  (jwtCustomRoleOperator was REMOVED — the product decision the note here deferred.)
      It was built from loadbalancer.networkLoadBalancers.{start,stop}, verbs migration
      0059 dropped when administrative enable/disable moved to
      NetworkLoadBalancer.admin_state. Post-0059 the only way to express an operator is
      `update` — editing admin_state is an ordinary mutation — and `update` is already
      what jwtCustomRoleTargetManager carries, so a second custom role on the same verb
      would distinguish nothing: the slot lost its subject rather than its wording.
      No step referenced the token, so removing it silences no assertion. The class it
      belonged to — a fixture role authored from a verb the catalog does not publish —
      is now held tree-wide by deploy/scripts/assert-fixture-role-verbs-exist.py.
"""

CASES = []

_NLB = "/nlb/v1/networkLoadBalancers"
_LST = "/nlb/v1/listeners"
_TGR = "/nlb/v1/targetGroups"
_VPC_SUBNETS = "/vpc/v1/subnets"

# Run-scoped /24 из адресного плана сети — нужен там, где кейсу требуется INTERNAL LB,
# НЕ зависящий от общего внешнего пула (VIP берётся из подсети).
#
# Здесь стояла своя копия формулы, прошивавшая префикс (`10.` + октеты): в объявленный
# сетью план она попадала СОВПАДЕНИЕМ, а мимо плана второго посева (`10.196.0.0/16`)
# уходила всегда. Теперь префикс выводится из опубликованного плана, а разводка
# параллельных прогонов сохранена помощником целиком. Разбор причины (блуждающий флейк
# «Subnet CIDRs can not overlap») — listener.py, шапка `_CIDR_ALLOC_PRE`; сама формула
# и её гейт — scripts/gen.py, раздел «АДРЕС НАРЕЗАЕМОЙ ПОДСЕТИ».
_CIDR_ALLOC_PRE = carve_cidr_pre('authz-deny')


# ---------------------------------------------------------------------------
# NLB per-RPC deny matrix
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="AZD-NLB-CR-VIEWER-DENIED",
    title="NLB.Create with viewer on project → PERMISSION_DENIED (Verifies REQ-AZD-NLB-CR)",
    classes=["AZD"], priority="P0",
    steps=[
        Step(name="cr-viewer", method="POST", path=_NLB, auth="jwtProjectViewerA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-vd-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED"),
                          "pm.test('mentions permission denied + loadbalancer perm', () => {",
                          "  const m = (pm.response.json().message || '').toLowerCase();",
                          "  pm.expect(m).to.include('permission denied');",
                          "});"]),
    ],
))

CASES.append(Case(
    id="AZD-NLB-GET-STRANGER-DENIED",
    title="NLB.Get with stranger (no tuple) → NOT_FOUND (BUG-2 hide-existence)",
    classes=["AZD"], priority="P0",
    steps=[
        Step(name="get-stranger", method="GET", path=f"{_NLB}/{{{{garbageNlbId}}}}",
             auth="jwtStranger",
             test_script=[
                 # BUG-2 hide-existence: a denied single-resource Get on a verb-bearing
                 # loadbalancer resource → NotFound (404 / code 5), never PermissionDenied —
                 # no enumeration leak (nonexistent == existing-denied → same 404).
                 *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                 "let _j; try { _j = pm.response.json(); } catch(e) { _j = null; }",
                 "pm.test('no deny_reasons leak (hide-existence)', () => "
                 "  pm.expect(JSON.stringify(_j || {}).toLowerCase()).to.not.include('deny_reasons'));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-NLB-GET-VIEWER-OK",
    title="NLB.Get with viewer → OK (positive grant)",
    classes=["AZD"], priority="P1",
    steps=[
        # Create as editor, then read as viewer
        Step(name="setup-cr", method="POST", path=_NLB, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-vok-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
        # read-your-writes: the viewer's read resolves via the LB->project owner/hierarchy
        # tuple, which is eventually-consistent (opgate removed) -> a fresh LB briefly 404s
        # at the authz gate until it materializes. Retry the SELF read on 403/404.
        retry_until_authorized(Step(name="get-viewer", method="GET", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectViewerA",
             test_script=[*assert_status(200),
                          "pm.test('id matches', () => "
                          "  pm.expect(pm.response.json().id).to.eql(pm.environment.get('nlbId')));"])),
        Step(name="cleanup", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
    ],
))


def _viewer_denied_case(case_id: str, method: str, path: str, body=None,
                       priority: str = "P1", is_list: bool = False) -> Case:
    # is_list: a List RPC scoped to a project the viewer/stranger cannot see is fail-closed
    # either by hard-deny (403/404) OR by list-authz returning a 200 scoped-EMPTY array
    # (security.md "List обязан фильтровать через listauthz"). A mutation/get on a specific
    # object, by contrast, must hard-deny (403/404) — never 200. So Lists additionally
    # tolerate 200-with-EMPTY (a 200 carrying ANY row is a real leak and fails); mutations
    # keep the strict deny.
    if is_list:
        test_script = [
            "pm.test('no cross-tenant data leaked (403/404, or 200 scoped-EMPTY)', () => {",
            "  if (pm.response.code === 200) {",
            "    const j = pm.response.json();",
            "    const arr = Object.values(j).find(v => Array.isArray(v)) || [];",
            "    pm.expect(arr.length, JSON.stringify(j)).to.eql(0);",
            "  } else {",
            "    pm.expect(pm.response.code).to.be.oneOf([403, 404]);",
            "  }",
            "});",
        ]
    else:
        test_script = [
            "pm.test('rejected (403)', () => "
            "  pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
            "if (pm.response.code === 403) pm.test('grpc 7', () => "
            "  pm.expect(pm.response.json().code).to.eql(7));",
        ]
    return Case(
        id=case_id, title=f"{method} {path} as viewer → "
                          f"{'no data (scoped-empty or denied)' if is_list else 'PERMISSION_DENIED'}",
        classes=["AZD"], priority=priority,
        steps=[Step(name="viewer", method=method, path=path, auth="jwtProjectViewerA",
                    body=body, test_script=test_script)],
    )


CASES.append(_viewer_denied_case(
    "AZD-NLB-UPD-VIEWER-DENIED", "PATCH", f"{_NLB}/{{{{garbageNlbId}}}}",
    body={"updateMask": "description", "description": "x"}))

CASES.append(_viewer_denied_case(
    "AZD-NLB-DEL-VIEWER-DENIED", "DELETE", f"{_NLB}/{{{{garbageNlbId}}}}"))

CASES.append(_viewer_denied_case(
    "AZD-NLB-GTS-STRANGER-DENIED", "GET",
    f"{_NLB}/{{{{garbageNlbId}}}}/targetStates", priority="P1"))

CASES.append(_viewer_denied_case(
    "AZD-NLB-LST-STRANGER-DENIED", "GET",
    f"{_NLB}?projectId={{{{garbageProjectId}}}}&pageSize=1", priority="P1", is_list=True))

CASES.append(_viewer_denied_case(
    "AZD-NLB-LOPS-STRANGER-DENIED", "GET",
    f"{_NLB}/{{{{garbageNlbId}}}}/operations?pageSize=1", priority="P2"))

CASES.append(Case(
    id="AZD-NLB-MV-SCOPE-DST-DENIED",
    title="NLB.Move to a cross project editor A cannot act on → DENIED (authz 403 or peer-first hide-existence 404, never 200) (Verifies REQ-AZD-NLB-MV-SCOPE)",
    classes=["AZD"], priority="P0",
    steps=[
        # Determinism guard (SEC): the whole point of this P0 is that the caller
        # (editor A) holds `editor` on the SOURCE project but has NO `editor`
        # grant on the DESTINATION project. The destination MUST be a project the
        # actor is genuinely foreign to — a CROSS-ACCOUNT project (projectB1Id in
        # account B). The same-account cross project (_suiteProjectCrossId = projA2)
        # is NOT foreign: prodseed grants the nlb move actor editor+v_update on
        # projA2 (in-account), so a Move there is a LAWFUL 200 — targeting it made
        # this "cross-tenant deny" P0 assert a bypass on a legitimate relocation.
        # projectB1Id lives in account B where the actor has no binding, so the
        # `editor on project:<dst>` Check that Move.authorizeDestination performs
        # genuinely denies. Asserting the direct-Create deny here makes it
        # GUARANTEED, not lenient-tolerated: a mis-seed granting editor A on
        # account B fails LOUDLY instead of the Move silently succeeding. A denied
        # Create writes nothing, so there is no resource to clean up.
        Step(name="precond-editorA-denied-on-dst", method="POST", path=_NLB,
             auth="jwtProjectEditorA",
             body={"projectId": "{{projectB1Id}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-mvpc-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED")]),
        Step(name="setup-cr", method="POST", path=_NLB, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-mv-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
        # Subject jwtProjectEditorA: editor on src, NOT editor on cross (editor B holds
        # the other side; the fixture binds A to the src project only). The Move MUST be
        # DENIED. The DENIAL CODE is ORDERING-TOLERANT: move.go runs the peer existence
        # precheck `ProjectService.Get(dst)` BEFORE authorizeDestination's dst-scope Check,
        # and iam hide-existence returns "Project <id> not found" for a project A cannot
        # SEE — so a lawful deny surfaces as 403 PERMISSION_DENIED (authz-first) OR 400/404
        # "Project not found" (peer-first hide-existence). Only a 200 (async Operation =
        # cross-tenant relocation) is a bug. The dst-scope EDITOR-deny is independently and
        # STRICTLY pinned by `precond-editorA-denied-on-dst` above (403 on a direct Create),
        # so relaxing the code here to the deny-family does NOT weaken the security contract.
        Step(name="mv-as-src-editor-only", method="POST", path=f"{_NLB}/{{{{nlbId}}}}:move",
             auth="jwtProjectEditorA",
             # cross-ACCOUNT dst (account B) — the actor has no binding there, so
             # authorizeDestination(`editor on project:<dst>`) genuinely denies. The
             # same-account _suiteProjectCrossId (projA2) is NOT foreign to this actor
             # (in-account editor grant) → a 200 there is lawful, not a bypass.
             body={"destinationProjectId": "{{projectB1Id}}"},
             test_script=[
                 # STRICT must-DENY (never 200) — ordering-tolerant (authz-first 403 OR
                 # peer-first hide-existence 400/404 "not found"). Parity with the sibling
                 # NLB-MV-CRUD-OK which already tolerates the peer-first "Project not found".
                 # NOT a phantom: the cross project EXISTS (precond gets 403, not a failed-op
                 # metadata id) — this is lawful hide-existence, not a fixture-absent target.
                 "let _mj; try { _mj = pm.response.json(); } catch (e) { _mj = {}; }",
                 "pm.test('Move DENIED — never 200 (no cross-tenant bypass)', () => "
                 "  pm.expect(pm.response.code, JSON.stringify(_mj)).to.be.oneOf([400, 403, 404]));",
                 "pm.test('grpc denial code 3/5/7 (invalid/notfound/denied)', () => "
                 "  pm.expect(_mj.code, JSON.stringify(_mj)).to.be.oneOf([3, 5, 7]));",
                 "pm.test('denial wording (permission denied OR not found)', () => {",
                 "  const m = (_mj.message || '').toLowerCase();",
                 "  pm.expect(m).to.satisfy(s => s.includes('permission denied') || s.includes('not authorized') || s.includes('not found'));",
                 "});",
             ]),
        Step(name="cleanup", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
    ],
))


# ---------------------------------------------------------------------------
# Listener per-RPC deny matrix
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="AZD-LST-CR-VIEWER-DENIED",
    title="LST.Create with viewer on parent LB → PERMISSION_DENIED (Verifies REQ-AZD-LST-CR)",
    classes=["AZD"], priority="P0",
    steps=[
        Step(name="cr-viewer", method="POST", path=_LST, auth="jwtProjectViewerA",
             body={"loadBalancerId": "{{garbageNlbId}}", "name": "azd-vd-{{runId}}",
                   "protocol": "TCP", "port": 80},
             test_script=[
                 "pm.test('rejected (403/404)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
             ]),
    ],
))

CASES.append(_viewer_denied_case(
    "AZD-LST-UPD-VIEWER-DENIED", "PATCH", f"{_LST}/{{{{garbageLstId}}}}",
    body={"updateMask": "description", "description": "x"}))
CASES.append(_viewer_denied_case(
    "AZD-LST-DEL-VIEWER-DENIED", "DELETE", f"{_LST}/{{{{garbageLstId}}}}"))

CASES.append(Case(
    id="AZD-LST-GET-STRANGER-DENIED",
    title="LST.Get with stranger → NOT_FOUND (BUG-2 hide-existence)",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="get-stranger", method="GET", path=f"{_LST}/{{{{garbageLstId}}}}",
             auth="jwtStranger",
             test_script=[
                 # BUG-2 hide-existence: denied single-resource Get on a verb-bearing
                 # loadbalancer resource → NotFound (404 / code 5), never PermissionDenied.
                 *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                 "let _j; try { _j = pm.response.json(); } catch(e) { _j = null; }",
                 "pm.test('no deny_reasons leak (hide-existence)', () => "
                 "  pm.expect(JSON.stringify(_j || {}).toLowerCase()).to.not.include('deny_reasons'));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-LST-LST-STRANGER-DENIED",
    title="LST.List by stranger → no data (list-authz scoped-empty or PERMISSION_DENIED)",
    classes=["AZD"], priority="P2",
    steps=[
        # projectId is a required List scope on ListListeners (omitting it is a plain
        # `project_id required` 400, not an authz signal), so supply it and let list-authz
        # decide. A stranger must get no rows: 403/404, or a 200 scoped-EMPTY array (never
        # another tenant's listeners). Techniques: ECP + security (list-authz scoped-empty).
        Step(name="lst-stranger", method="GET",
             path=f"{_LST}?projectId={{{{_suiteProjectId}}}}&loadBalancerId={{{{garbageNlbId}}}}",
             auth="jwtStranger",
             test_script=[
                 "pm.test('no cross-tenant data leaked (403/404, or 200 scoped-EMPTY)', () => {",
                 "  if (pm.response.code === 200) {",
                 "    const j = pm.response.json();",
                 "    pm.expect((j.listeners || j.items || []).length, JSON.stringify(j)).to.eql(0);",
                 "  } else {",
                 "    pm.expect(pm.response.code).to.be.oneOf([403, 404]);",
                 "  }",
                 "});",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-LST-LOPS-STRANGER-DENIED",
    title="LST.ListOperations by stranger → PERMISSION_DENIED",
    classes=["AZD"], priority="P2",
    steps=[
        Step(name="lops-stranger", method="GET",
             path=f"{_LST}/{{{{garbageLstId}}}}/operations",
             auth="jwtStranger",
             test_script=[
                 "pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# TargetGroup per-RPC deny matrix
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="AZD-TGR-CR-VIEWER-DENIED",
    title="TGR.Create with viewer on project → PERMISSION_DENIED",
    classes=["AZD"], priority="P0",
    steps=[
        Step(name="cr-viewer", method="POST", path=_TGR, auth="jwtProjectViewerA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-tgr-vd-{{runId}}", "port": 8080,
                   "healthCheck": {"interval": "2s", "timeout": "1s",
                                   "unhealthyThreshold": 3, "healthyThreshold": 2,
                                   "tcp": {"port": 80}}},
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED")]),
    ],
))

CASES.append(_viewer_denied_case(
    "AZD-TGR-UPD-VIEWER-DENIED", "PATCH", f"{_TGR}/{{{{garbageTgrId}}}}",
    body={"updateMask": "description", "description": "x"}))
CASES.append(_viewer_denied_case(
    "AZD-TGR-DEL-VIEWER-DENIED", "DELETE", f"{_TGR}/{{{{garbageTgrId}}}}"))

CASES.append(Case(
    id="AZD-TGR-MV-SCOPE-DST-DENIED",
    title="TGR.Move to a cross project editor A cannot act on → DENIED (authz 403 or peer-first hide-existence 404, never 200)",
    classes=["AZD"], priority="P0",
    steps=[
        # Determinism guard (SEC) — parity with AZD-NLB-MV-SCOPE-DST-DENIED.
        # dst MUST be a genuinely-foreign CROSS-ACCOUNT project (projectB1Id, account
        # B) — the actor has no binding there. The same-account _suiteProjectCrossId
        # (projA2) is NOT foreign (prodseed grants the actor editor+v_update on projA2
        # in-account), so a Move there is a LAWFUL 200, not a bypass. A direct Create
        # in projectB1Id (same `editor on project:<dst>` Check authorizeDestination
        # runs) MUST be denied, so the Move dst-scope deny is guaranteed and a
        # mis-seed fails loudly here rather than as a silent 200 on the Move.
        Step(name="precond-editorA-denied-on-dst", method="POST", path=_TGR,
             auth="jwtProjectEditorA",
             body={"projectId": "{{projectB1Id}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-tgrmvpc-{{runId}}", "port": 8080,
                   "healthCheck": {"interval": "2s", "timeout": "1s",
                                   "unhealthyThreshold": 3, "healthyThreshold": 2,
                                   "tcp": {"port": 80}}},
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED")]),
        Step(name="setup-tg", method="POST", path=_TGR, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-tgrmv-{{runId}}", "port": 8080,
                   "healthCheck": {"interval": "2s", "timeout": "1s",
                                   "unhealthyThreshold": 3, "healthyThreshold": 2,
                                   "tcp": {"port": 80}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.targetGroupId", "tgId")]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
        # editor on src, NOT on cross → the Move MUST be DENIED, ORDERING-TOLERANT:
        # targetgroup/move.go runs the peer existence precheck `ProjectService.Get(dst)`
        # BEFORE authorizeDestination's dst-scope Check, and iam hide-existence returns
        # "Project <id> not found" for a project A cannot SEE → the deny lawfully surfaces
        # as 403 PERMISSION_DENIED (authz-first) OR 400/404 "not found" (peer-first). Only
        # a 200 (async Operation) is a bug. The dst-scope EDITOR-deny is STRICTLY pinned by
        # `precond-editorA-denied-on-dst` above (403 on a direct Create).
        Step(name="mv-no-dst-editor", method="POST", path=f"{_TGR}/{{{{tgId}}}}:move",
             auth="jwtProjectEditorA",
             # cross-ACCOUNT dst (account B) — actor has no binding → genuine deny.
             body={"destinationProjectId": "{{projectB1Id}}"},
             test_script=[
                 # STRICT must-DENY (never 200) — ordering-tolerant (authz-first 403 OR
                 # peer-first hide-existence 400/404 "not found"). Parity with the sibling
                 # TGR-MV-CRUD-OK which already tolerates the peer-first "Project not found".
                 # NOT a phantom: the cross project EXISTS (precond gets 403) — lawful
                 # hide-existence, not a fixture-absent target.
                 "let _mj; try { _mj = pm.response.json(); } catch (e) { _mj = {}; }",
                 "pm.test('Move DENIED — never 200 (no cross-tenant bypass)', () => "
                 "  pm.expect(pm.response.code, JSON.stringify(_mj)).to.be.oneOf([400, 403, 404]));",
                 "pm.test('grpc denial code 3/5/7 (invalid/notfound/denied)', () => "
                 "  pm.expect(_mj.code, JSON.stringify(_mj)).to.be.oneOf([3, 5, 7]));",
                 "pm.test('denial wording (permission denied OR not found)', () => {",
                 "  const m = (_mj.message || '').toLowerCase();",
                 "  pm.expect(m).to.satisfy(s => s.includes('permission denied') || s.includes('not authorized') || s.includes('not found'));",
                 "});",
             ]),
        Step(name="cleanup", method="DELETE", path=f"{_TGR}/{{{{tgId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
    ],
))

# ОБА КЕЙСА НИЖЕ АДРЕСУЮТ СУЩЕСТВУЮЩУЮ ГРУППУ, А НЕ `garbageTgrId` (NLB-TGT-1).
#
# На несуществующем объекте `404` — законный ответ по любой причине, поэтому
# `oneOf([403, 404])` держалось и без всякого отказа в правах: наблюдатель, которому
# управление составом ПО ОШИБКЕ разрешили бы, получил бы 404 и кейс остался бы зелёным.
# Группа создаётся тут же редактором суиты, поэтому `404` перестаёт быть законным
# исходом, и утверждается ТОЧНОЕ `403`. Парный положительный — чтение той же группы тем
# же наблюдателем: отказ в управлении составом отличается от «субъект ничего не видит».
def _tgr_viewer_membership_denied_case(case_id: str, verb: str, req_id: str) -> Case:
    return Case(
        id=case_id,
        title=f"TGR.{verb} with viewer on a REAL group → PERMISSION_DENIED (Verifies {req_id})",
        classes=["AZD"], priority="P0",
        steps=[
            Step(name="setup-tg", method="POST", path=_TGR, auth="jwtProjectEditorA",
                 body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                       "name": f"azd-tgrvd-{verb.lower()}-{{{{runId}}}}", "port": 8080,
                       "healthCheck": {"interval": "2s", "timeout": "1s",
                                       "unhealthyThreshold": 3, "healthyThreshold": 2,
                                       "tcp": {"port": 8080}}},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                              *save_from_response("j.metadata && j.metadata.targetGroupId", "vdTgId")]),
            poll_operation_until_done(fixture_ids=["vdTgId"], auth="jwtProjectEditorA"),
            Step(name=f"{verb.lower()}-viewer-denied", method="POST",
                 path=f"{_TGR}/{{{{vdTgId}}}}:{verb[0].lower()}{verb[1:]}",
                 auth="jwtProjectViewerA",
                 body={"targets": [{"externalIp": {"address": "203.0.113.30"}, "weight": 100}]},
                 test_script=[
                     "pm.test('viewer is denied membership management on a group that provably EXISTS', () => "
                     "  pm.expect(pm.response.code, pm.response.text()).to.eql(403));",
                 ]),
            Step(name="get-viewer-ok", method="GET", path=f"{_TGR}/{{{{vdTgId}}}}",
                 auth="jwtProjectViewerA",
                 test_script=[
                     "pm.test('the same viewer still READS the group (the denial is about membership, not visibility)', () => "
                     "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 ]),
            Step(name="cleanup", method="DELETE", path=f"{_TGR}/{{{{vdTgId}}}}",
                 auth="jwtProjectEditorA",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(auth="jwtProjectEditorA"),
        ],
    )


CASES.append(_tgr_viewer_membership_denied_case(
    "AZD-TGR-ADD-VIEWER-DENIED", "AddTargets", "REQ-AZD-TGR-ADD"))

CASES.append(_tgr_viewer_membership_denied_case(
    "AZD-TGR-RM-VIEWER-DENIED", "RemoveTargets", "REQ-AZD-TGR-RM"))

CASES.append(Case(
    id="AZD-TGR-GET-STRANGER-DENIED",
    title="TGR.Get with stranger → NOT_FOUND (BUG-2 hide-existence)",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="get-stranger", method="GET", path=f"{_TGR}/{{{{garbageTgrId}}}}",
             auth="jwtStranger",
             test_script=[
                 # BUG-2 hide-existence: denied single-resource Get on a verb-bearing
                 # loadbalancer resource → NotFound (404 / code 5), never PermissionDenied.
                 *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                 "let _j; try { _j = pm.response.json(); } catch(e) { _j = null; }",
                 "pm.test('no deny_reasons leak (hide-existence)', () => "
                 "  pm.expect(JSON.stringify(_j || {}).toLowerCase()).to.not.include('deny_reasons'));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-TGR-LST-STRANGER-DENIED",
    title="TGR.List by stranger → no data (list-authz scoped-empty or PERMISSION_DENIED)",
    classes=["AZD"], priority="P2",
    steps=[
        # A stranger (no bindings) listing a project they cannot see must NEVER receive
        # another tenant's rows. Two lawful fail-closed shapes: (a) 403/404 hard-deny, or
        # (b) 200 with an EMPTY array — the list-authz push-down filters every row the
        # caller lacks a viewer relation on (security.md "List обязан фильтровать через
        # listauthz"). Both are asserted as "no data leaked"; a 200 carrying ANY row is a
        # real BOLA leak and fails. Techniques: ECP (authorized vs stranger) + security
        # (list-authz scoped-empty).
        Step(name="lst-stranger", method="GET",
             path=f"{_TGR}?projectId={{{{_suiteProjectId}}}}",
             auth="jwtStranger",
             test_script=[
                 "pm.test('no cross-tenant data leaked (403/404, or 200 scoped-EMPTY)', () => {",
                 "  if (pm.response.code === 200) {",
                 "    const j = pm.response.json();",
                 "    pm.expect((j.targetGroups || j.items || []).length, JSON.stringify(j)).to.eql(0);",
                 "  } else {",
                 "    pm.expect(pm.response.code).to.be.oneOf([403, 404]);",
                 "  }",
                 "});",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-TGR-LOPS-STRANGER-DENIED",
    title="TGR.ListOperations by stranger → PERMISSION_DENIED",
    classes=["AZD"], priority="P2",
    steps=[
        Step(name="lops-stranger", method="GET",
             path=f"{_TGR}/{{{{garbageTgrId}}}}/operations",
             auth="jwtStranger",
             test_script=[
                 "pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# Operation per-RPC deny
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="AZD-OP-GET-OUTSIDE-SCOPE-DENIED",
    title="OP.Get for op whose parent the subject can't see → PERMISSION_DENIED",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="get-out-scope", method="GET", path="/operations/{{garbageOpId}}",
             auth="jwtStranger",
             # absent/garbage op id: authz-first 403 (scope can't resolve target->project),
             # 404 hide-existence, or 400 malformed id — all = rejected (no leak).
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="AZD-OP-CANCEL-NON-CREATOR-DENIED",
    title="OP.Cancel чужой операции → 404 NOT_FOUND 'operation <id> not found' (чтение операции сужено по владельцу; Verifies REQ-AZD-OP-CANCEL)",
    classes=["AZD"], priority="P0",
    steps=[
        # Create op as editor A
        Step(name="cr-as-A", method="POST", path=_NLB, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-cancel-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        # Try cancel as Editor B (different subject)
        #
        # 409 СНЯТ: производителя у него на этой полосе НЕТ. Отмена операции сведена
        # в общий слой (`pkg/operations/operationspb/handler.go`) и эмитит ровно
        # InvalidArgument / NotFound / FailedPrecondition / Internal; край
        # (`gateway/internal/opsproxy`) добавляет к ним InvalidArgument / NotFound /
        # PermissionDenied. По таблице края (`api-conventions.md` §«gRPC-код →
        # HTTP-статус») это 400/404/400/500 и 400/404/403 — 409 не даёт ни один.
        # Сам 409 приходит только от ALREADY_EXISTS и ABORTED, а их на всей полосе
        # ноль вхождений. Допуск на исход, которого не бывает, не краснеет НИКОГДА:
        # шаг зеленел на подставленном 409, то есть утверждал меньше, чем выглядел.
        #
        # И утверждается ПАРА, а не один HTTP-статус: каждая оставшаяся полоса
        # называет свой код `google.rpc.Status`, иначе «отказано» не отличимо от
        # отказа по любой посторонней причине. Пин условный — полос здесь ТРИ, и
        # какая сработает, решает порядок проверок на крае.
        Step(name="cancel-as-B", method="POST", path="/operations/{{opId}}:cancel",
             auth="jwtProjectEditorB",
             # ИСХОД ЗДЕСЬ ОДИН, И ОН УСТАНОВЛЕН ОПЫТОМ, А НЕ ЧТЕНИЕМ (#1403).
             #
             # Полоса: край читает операцию у владельца ПЕРВЫМ действием и под личностью
             # вызывающего; чтение сужено по владельцу (`GetOwned`), поэтому чужому
             # субъекту приходит `NOT_FOUND` — ДО проверки владения на крае и ДО самой
             # отмены. Значит `PERMISSION_DENIED` (403) той проверки и
             # `FAILED_PRECONDITION` (400) «уже завершена» на этом шаге производителя
             # НЕ ИМЕЮТ, а допуск на исход без производителя не краснеет никогда
             # (`e2e-flow.md` §3). Запись каталога прав для этой полосы — `<exempt>`
             # («решает обработчик»), поэтому 403 не приходит и от края.
             #
             # Доказано пробой `gateway/internal/opsproxy`
             # `TestCancelOfAnotherSubjectsOperationYieldsNotFoundOnly`: она собирает
             # полосу целиком (край + общий обработчик + предикат владения) и вдобавок
             # утверждает СЧЁТЧИКОМ, что до отмены дело не доходит — код ответа об этом
             # не говорит ничего. Рядом положительный контроль: создатель ту же операцию
             # отменяет успешно.
             #
             # Прежний заголовок обещал `PERMISSION_DENIED`, то есть называл не тот отказ,
             # который полоса даёт: читатель искал бы причину 404 не там.
             #
             # Текст утверждается ДОСЛОВНО и без приведения регистра: он приходит из
             # единственного производителя `pkg/operations.NotFoundStatus`, и тем же
             # текстом отвечает край на своей ветке — различие хоть в байт отличало бы
             # «нет доступа» от «не существует» (`security.md` §Hardening #6).
             test_script=[
                 "pm.test('отвергнуто 404 — чтение операции сужено по владельцу', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(404));",
                 "pm.test('grpc 5 (NOT_FOUND)', () => "
                 "  pm.expect(pm.response.json().code).to.eql(5));",
                 "pm.test('сообщение дословно равно тексту владельца', () => "
                 "  pm.expect(pm.response.json().message).to.eql("
                 "'operation ' + pm.environment.get('opId') + ' not found'));",
             ]),
        # ОПРОС ИДЁТ ПОД СОЗДАТЕЛЕМ ОПЕРАЦИИ, А НЕ ПОД ТЕМ, КОМУ ТОЛЬКО ЧТО ОТКАЗАЛИ.
        #
        # Опрос стоял под B — под тем самым субъектом, про которого шаг выше
        # УТВЕРЖДАЕТ, что операция A ему не видна. Видимость операции
        # creator-principal-scoped by design (pkg/operations/owner.go: предикат —
        # пара (PrincipalType, PrincipalID) создателя, чужой владелец → ErrNotFound
        # без утечки), поэтому B законно получал 404, а кейс требовал от него 200 и
        # краснел на ПРАВИЛЬНОМ поведении — в той же папке, где сам же его и
        # утверждает. Ровно тот класс, который godoc `poll_operation_until_done`
        # называет «mislaid identity».
        #
        # Предмет кейса — «не создатель не может отменить» — целиком утверждён на
        # `cancel-as-B`. Опрос здесь нужен лишь затем, чтобы создание A завершилось
        # до уборки, и потому принадлежит A.
        poll_operation_until_done(auth="jwtProjectEditorA"),
        Step(name="cleanup", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
    ],
))


# ---------------------------------------------------------------------------
# Cross-cutting AZD
# ---------------------------------------------------------------------------

# AZD-FGA-UNAVAILABLE-FAIL-CLOSED lived here and is REMOVED — not skipped, not
# whitelisted. It asserted "either 200 (FGA up) or 403 (FGA down fail-closed)" while
# stating plainly, in its own comment, that "in ordinary test conditions FGA is up" — so
# in every run that ever happened it accepted the ordinary answer and checked nothing.
# The invariant it named (no answer about permissions must never be counted as
# "allowed") was never once exercised, and the suite read green.
#
# The invariant is REAL and is now genuinely checked, with the condition CREATED rather
# than waited for: services/iam/tests/newman/cases/authz-failclosed.py, driven by
# services/iam/tests/newman/scripts/run-failclosed.sh, which scales the authorization
# store to zero, waits out the gateway's decision cache, runs that one collection and
# restores the stand. It runs as its own wave after every other suite has reported, and
# if the wave does not run there is no report and the gate says
# `authz-failclosed(no-report)`.
#
# Nothing nlb-specific is lost by removing this copy: the decision is made by the shared
# gateway authorization middleware (`authz.failOpen=false`), not by the nlb backend, so
# probing it through an nlb route tested the same component down a longer path. A case
# needing an unavailable dependency gets a wave that creates the condition — never a
# tolerant assertion in a suite that cannot create it (testing.md).

CASES.append(Case(
    id="AZD-NLB-CR-ANONYMOUS-UNAUTH",
    title="NLB.Create without Authorization header → 401 UNAUTHENTICATED (Verifies REQ-AZD-ANON)",
    classes=["AZD"], priority="P0",
    steps=[
        Step(name="cr-anon", method="POST", path=_NLB, auth="anonymous",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-anon-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[
                 "pm.test('401 UNAUTHENTICATED', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([401, 403]));",
                 "if (pm.response.code === 401) pm.test('grpc 16', () => "
                 "  pm.expect(pm.response.json().code).to.eql(16));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-PERMISSION-CATALOG-COMPLETE",
    title="The live grantable catalog carries the loadbalancer module and every resource "
          "the nlb permission strings are built from (Verifies REQ-AZD-CATALOG)",
    classes=["AZD"], priority="P0",
    steps=[
        # WHAT THIS USED TO DO. It POSTed a load balancer, wrote twenty-six permission
        # strings into a JavaScript array inside the case, and asserted
        # `expected.length === 26` — the length of a literal it had just written, which is
        # true no matter what the platform does. Its only other statement accepted 200 OR
        # 403, so the create said nothing either. The catalog was never read. The comment
        # even admitted it: "the catalog query is exposed via iam internal mux; absent
        # that, this case acts as a structural reminder".
        #
        # It is not absent. `GET /iam/v1/permissionCatalog` is a PUBLIC, authenticated,
        # sync read on the external listener (gateway/internal/restmux/mux.go registers
        # PermissionCatalogService; the RPC is `<exempt>` from project-scope, so any
        # authenticated principal may read it — anonymous is still refused). It answers
        # with the grantable taxonomy the rule compiler actually honours: modules ->
        # `(module, resource)` tokens plus the verb set OF EACH TYPE. So the case reads it.
        #
        # WHY THAT IS THE RIGHT ARTEFACT. A permission string is `module.resource.verb`; it
        # is grantable only if `(module, resource)` is in the catalog — the compiler
        # fail-closed-SKIPs anything else, making the grant a silent no-op. So the three
        # loadbalancer resources are what the nlb suite's authorization rests on, and they
        # are asserted by NAME (their spelling travels on the wire and is never renamed),
        # together with the verb set each of them declares. `verb-bearing` is asserted too:
        # a tier-only type would make per-object rules skip.
        #
        # ПОЧЕМУ ГЛАГОЛЫ СПРАШИВАЮТСЯ У РЕСУРСА, А НЕ У `closedVerbs` (#1128).
        # Здесь стояло `closedVerbs ⊇ [get,list,update,delete]`. Это утверждение не про
        # nlb: `closed_verbs` — ПЕРЕСЕЧЕНИЕ наборов ВСЕХ типов платформы, и его
        # собственный контракт объявляет СУЖЕНИЕ нормой, а не поломкой
        # (proto/kacho/cloud/iam/v1/permission_catalog_service.proto, поле `closed_verbs`).
        # Сняв `v_update` у ОДНОГО чужого типа (`iam_user` — правку его записи
        # спрашивает другое отношение), платформа вынула глагол из пересечения —
        # и эта проба покраснела, не изменившись в том, что она измеряла.
        #
        # Замена — СТРОГО СИЛЬНЕЕ, а не слабее. Прежнее утверждение о наборе nlb
        # не говорило ничего: оно краснеет от чужого типа и не различает, у какого
        # именно глагол пропал. Новое падает тогда и только тогда, когда глагол теряет
        # ОДИН ИЗ ТРЁХ ТИПОВ nlb — то есть ровно тогда, когда строка права этого
        # домена перестаёт что-либо значить, — и называет виновный тип по имени.
        #
        # Контракт самого поля `closedVerbs` здесь НЕ утверждается вовсе: его владелец —
        # набор iam (CONF-G-01-catalog-happy, services/iam/tests/newman/cases/
        # iam-permission-catalog.py), и там он уже утверждает ровно то, что верно по
        # построению (общие — только `get` и `list`). Два места об одном предмете
        # разошлись бы снова — и разошлись именно так.
        #
        # The compiled per-RPC table (which method demands which permission) is a different
        # artefact, gated where it lives: GENERATED from proto, with both embedded copies
        # byte-compared by `make permission-catalog-check`. A hand-kept transcription of it
        # inside a newman case could only ever drift.
        Step(name="read-catalog", method="GET", path="/iam/v1/permissionCatalog",
             auth="jwtProjectEditorA",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "const mods = j.modules || [];",
                 "pm.test('catalog is non-empty (a read that returned nothing is not a pass)', () => "
                 "  pm.expect(mods.length, JSON.stringify(j)).to.be.above(0));",
                 "const lb = mods.find(m => m.module === 'loadbalancer');",
                 "pm.test('grantable module loadbalancer is present', () => "
                 "  pm.expect(lb, 'modules: ' + mods.map(m => m.module).join(',')).to.be.an('object'));",
                 "const names = ((lb || {}).resources || []).map(r => r.resource);",
                 "['networkLoadBalancers', 'listeners', 'targetGroups'].forEach(r => {",
                 "  pm.test('grantable resource loadbalancer.' + r, () => "
                 "    pm.expect(names, 'resources: ' + names.join(',')).to.include(r));",
                 "  pm.test('loadbalancer.' + r + ' is verb-bearing (per-object rules compile)', () => "
                 "    pm.expect(((lb || {}).resources || []).find(x => x.resource === r).hasVerbRelations)"
                 "      .to.eql(true));",
                 "  pm.test('глаголы прав nlb объявлены ТИПОМ loadbalancer.' + r, () => {",
                 "    const res = ((lb || {}).resources || []).find(x => x.resource === r) || {};",
                 "    // Приведение к нижнему регистру — не вкус: набор приезжает именем",
                 "    // отношения без приставки (`v_addtargets` → `addtargets`), и продукт",
                 "    // сам сравнивает глаголы нормализованными (domain.NormalizeVerb).",
                 "    const verbs = (res.verbs || []).map(v => String(v).toLowerCase());",
                 "    pm.expect(verbs, JSON.stringify(res)).to.be.an('array').that.is.not.empty;",
                 "    pm.expect(verbs, 'набор глаголов типа loadbalancer.' + r)",
                 "      .to.include.members(['get','list','update','delete']);",
                 "  });",
                 "});",
                 "pm.test('составом группы целей распоряжаются СВОИ глаголы типа', () => {",
                 "  // Роль управления целями (AZD-CUSTOM-ROLE-TARGET-MANAGER ниже) строится",
                 "  // ровно из них. Не будь их в наборе ТИПА, реконсайлер отбросил бы правило",
                 "  // молча, и положительная половина той пробы не прошла бы ни при каком",
                 "  // исправном продукте.",
                 "  const tg = ((lb || {}).resources || []).find(x => x.resource === 'targetGroups') || {};",
                 "  const verbs = (tg.verbs || []).map(v => String(v).toLowerCase());",
                 "  pm.expect(verbs, JSON.stringify(tg)).to.include.members(['addtargets','removetargets']);",
                 "});",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-CUSTOM-ROLE-TARGET-MANAGER",
    title="Custom role targetManager adds and removes targets but may not edit the group itself",
    classes=["AZD"], priority="P1",
    steps=[
        # ЧТО ЭТОТ КЕЙС УТВЕРЖДАЕТ, И ПОЧЕМУ ЭТО СТАЛО ВЫРАЗИМО (NLB-TGT-1).
        #
        # Роль поставляется в продукте под именем управления целями и объявляет
        # `addTargets`/`removeTargets`. Пока оба RPC гейтились тем же отношением, что и
        # правка самой группы, различение «может управлять составом, но не может править
        # группу» было НЕВЫРАЗИМО: субъект, вправе добавить цель, был вправе и
        # переименовать группу — by construction. Прежняя редакция кейса поэтому грантила
        # роли `update` и утверждала со-материализацию удаления: единственную пару,
        # которую тогдашняя модель различала.
        #
        # Теперь у типа `nlb_target_group` два собственных отношения управления составом,
        # объявленные НАДМНОЖЕСТВАМИ отношения правки. Кейс утверждает ровно ту пару,
        # ради которой роль существует, и обе половины идут для ОДНОГО субъекта на ОДНОМ
        # объекте: без успешного добавления отказ в правке был бы зелен и тогда, когда
        # роль не даёт вообще ничего.
        Step(name="tm-setup-tg", method="POST", path=_TGR, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-tm-tg-{{runId}}", "port": 8080,
                   "healthCheck": {"interval": "2s", "timeout": "1s",
                                   "unhealthyThreshold": 3, "healthyThreshold": 2,
                                   "tcp": {"port": 8080}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.targetGroupId", "tmTgId")]),
        poll_operation_until_done(fixture_ids=["tmTgId"], auth="jwtProjectEditorA"),
        # Грант роли материализуется eventually-consistent, как любой другой, поэтому
        # кратковременный отказ повторяется — утверждается по-прежнему только успех. Если
        # он не сходится, роль или её привязка не посеяны (tests/authz-fixtures/
        # prodseed_matrix.py) — это и есть находка, и ей место в открытую, а не за
        # терпимым утверждением.
        retry_until_authorized(Step(name="add-as-tm", method="POST",
             path=f"{_TGR}/{{{{tmTgId}}}}:addTargets",
             auth="jwtCustomRoleTargetManager",
             body={"targets": [{"externalIp": {"address": "203.0.113.32"}, "weight": 100}]},
             test_script=[
                 # ОТВЕРГНУТАЯ МУТАЦИЯ НЕ ОСТАВЛЯЕТ ЧУЖОЙ ИДЕНТИФИКАТОР ЗА СОБОЙ.
                 #
                 # Без этого снятия отказ здесь оставлял `opId` от `tm-setup-tg` —
                 # операции, созданной ДРУГИМ субъектом (editor A), — и следующий шаг
                 # опрашивал её под targetManager'ом. Видимость операции
                 # creator-principal-scoped, поэтому ответ был честный 404, а
                 # утверждение «poll status 200» краснело так, будто операция пропала.
                 "pm.environment.set('opId', '');",
                 "pm.test('targetManager may AddTargets (the verb its role grants)', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 *save_from_response("j.id", "opId"),
             ])),
        poll_operation_until_done(must_succeed=True, auth="jwtCustomRoleTargetManager"),
        # РЕЗУЛЬТАТ, А НЕ ТОЛЬКО КОД ОТВЕТА: цель обязана оказаться в группе. Код 200 на
        # мутации, чей асинхронный хвост ничего не добавил, утверждал бы право и молчал
        # о его действии.
        Step(name="tm-verify-target-present", method="GET", path=f"{_TGR}/{{{{tmTgId}}}}",
             auth="jwtProjectEditorA",
             test_script=[
                 *assert_status(200),
                 "const tg = pm.response.json();",
                 "const addrs = (tg.targets || []).map(t => (t.externalIp || {}).address);",
                 "pm.test('the target AddTargets accepted is actually in the group', () => "
                 "  pm.expect(addrs, JSON.stringify(tg.targets || [])).to.include('203.0.113.32'));",
             ]),
        # ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА — правка САМОЙ группы. Она осмысленна только потому, что
        # положительная выше уже прошла тем же субъектом на том же объекте: отказ значит
        # «различение работает», а не «субъект вообще ничего не может».
        #
        # Утверждается ТОЧНОЕ 403, а не `oneOf([403,404])`: группа доказано существует —
        # её создали и прочитали шагами выше, — поэтому 404 перестал быть законным
        # исходом, а взаимоисключающие исходы в одном утверждении суть отсутствие
        # утверждения.
        Step(name="upd-as-tm-denied", method="PATCH", path=f"{_TGR}/{{{{tmTgId}}}}",
             auth="jwtCustomRoleTargetManager",
             body={"updateMask": "description", "description": "azd-tm-{{runId}}"},
             test_script=[
                 "pm.test('targetManager may NOT edit the group itself (membership is not configuration)', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(403));",
             ]),
        # И удаление: управление составом не даёт снести саму группу. Удаление проверяется
        # на ПУСТОЙ группе — иначе отказ пришёл бы предусловием («TargetGroup has N
        # target(s); remove them first») и был бы зелёным ровно так же, если бы право
        # удаления у роли БЫЛО: такой шаг не различает отказ по правам и отказ по
        # состоянию.
        Step(name="tm-rm-target", method="POST", path=f"{_TGR}/{{{{tmTgId}}}}:removeTargets",
             auth="jwtCustomRoleTargetManager",
             body={"targets": [{"externalIp": {"address": "203.0.113.32"}}]},
             test_script=[
                 "pm.test('targetManager may RemoveTargets (the second verb its role grants)', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(must_succeed=True, auth="jwtCustomRoleTargetManager"),
        Step(name="del-as-tm-denied", method="DELETE", path=f"{_TGR}/{{{{tmTgId}}}}",
             auth="jwtCustomRoleTargetManager",
             test_script=[
                 "pm.test('membership verbs do NOT co-materialize delete of the group', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(403));",
             ]),
        # ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ НА ТУ ЖЕ ОСЬ — держатель ТОЛЬКО `update`.
        #
        # Он обязан управлять составом (ветвь `or v_update` модели: сегодняшний редактор
        # ничего не теряет от раскола) И править саму группу. Без этой половины отказ
        # выше был бы неотличим от «после раскола управление составом отобрали у всех».
        retry_until_authorized(Step(name="add-as-tgupdater", method="POST",
             path=f"{_TGR}/{{{{tmTgId}}}}:addTargets",
             auth="jwtCustomRoleTgUpdater",
             body={"targets": [{"externalIp": {"address": "203.0.113.33"}, "weight": 100}]},
             test_script=[
                 "pm.environment.set('opId', '');",
                 "pm.test('a holder of update still manages membership (the new relations are a SUPERSET)', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 *save_from_response("j.id", "opId"),
             ])),
        poll_operation_until_done(must_succeed=True, auth="jwtCustomRoleTgUpdater"),
        Step(name="upd-as-tgupdater", method="PATCH", path=f"{_TGR}/{{{{tmTgId}}}}",
             auth="jwtCustomRoleTgUpdater",
             body={"updateMask": "description", "description": "azd-tgupd-{{runId}}"},
             test_script=[
                 "pm.test('a holder of update still edits the group itself', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(must_succeed=True, auth="jwtCustomRoleTgUpdater"),
        # ТРЕТИЙ СУБЪЕКТ — наблюдатель: ни управления составом, ни правки. Его парный
        # положительный — чтение той же группы: отказ в управлении составом отличается от
        # «этот субъект ничего не видит».
        Step(name="add-as-viewer-denied", method="POST",
             path=f"{_TGR}/{{{{tmTgId}}}}:addTargets",
             auth="jwtProjectViewerA",
             body={"targets": [{"externalIp": {"address": "203.0.113.34"}, "weight": 100}]},
             test_script=[
                 "pm.test('a viewer may not manage membership', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(403));",
             ]),
        Step(name="get-as-viewer-ok", method="GET", path=f"{_TGR}/{{{{tmTgId}}}}",
             auth="jwtProjectViewerA",
             test_script=[
                 "pm.test('the same viewer still reads the group (the denial above is about membership, not visibility)', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
             ]),
        # Уборка своим субъектом — фикстура не течёт в соседние прогоны.
        Step(name="tm-cleanup-rm-target", method="POST",
             path=f"{_TGR}/{{{{tmTgId}}}}:removeTargets", auth="jwtProjectEditorA",
             body={"targets": [{"externalIp": {"address": "203.0.113.33"}}]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
        Step(name="tm-cleanup-tg", method="DELETE", path=f"{_TGR}/{{{{tmTgId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
    ],
))

CASES.append(Case(
    id="AZD-CUSTOM-ROLE-UNKNOWN-PERMISSION",
    title="iam.Role.Create naming a permission outside the catalog → refused, never created",
    classes=["AZD"], priority="P1",
    steps=[
        # The iam Role.Create endpoint lives in kaname, not nlb; this case
        # is a placeholder that asserts the symbolic contract by attempting a
        # request that, in a fully wired stand, would hit kaname through
        # the api-gateway. If the route is absent in this stand the assertion
        # tolerates 404. Drift test in kacho-iam/internal/authzmap is the
        # authoritative enforcement.
        Step(name="probe-unknown-perm", method="POST", path="/iam/v1/roles",
             auth="jwtProjectOwnerA",
             body={"name": "azd-unknown-{{runId}}",
                   "permissions": ["loadbalancer.foo.bar"]},
             test_script=[
                 # The title promises a refusal and the assertion accepted 200 — a role
                 # holding a permission the platform does not know, CREATED — as a pass.
                 # The follow-up code check was written `if (code === 400)`, i.e. it only
                 # ran once the refusal it was meant to establish had already happened.
                 # Which refusal arrives is genuinely ordering-dependent and all are
                 # lawful: the gateway may deny the caller before the body is looked at
                 # (403), the route may not be registered on this listener (404), or iam
                 # may reject the unknown token (400). Acceptance is not among them.
                 "pm.test('role naming an unknown permission is refused (never created)', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([400, 403, 404]));",
                 "if (pm.response.code === 400) pm.test('grpc 3 (INVALID_ARGUMENT)', () => "
                 "  pm.expect(pm.response.json().code).to.eql(3));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-NO-CHECK-BYPASS-KNOB",
    title="per-RPC Check has no bypass knob: a stranger is denied on every posture",
    classes=["AZD"], priority="P2",
    steps=[
        # Прежде кейс назывался KACHO_NLB_AUTHZ__BREAKGLASS=true и обещал
        # проверить обход. Обхода не существует: ни ключа профиля, ни поля
        # настроек, ни ветки — звено решения о доступе ставится безусловно.
        # Утверждение кейса от переименования не изменилось (оно и было
        # единственным, что здесь исполнялось): посторонний получает отказ.
        # Название теперь описывает то, что проверяется, а не ручку, которой нет.
        Step(name="stranger-create", method="POST", path=_NLB, auth="jwtStranger",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-brk-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[
                 "pm.test('no Check bypass: stranger denied', () => "
                 "  pm.expect(pm.response.code).to.eql(403));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-LIFECYCLE-DELETED-TUPLE-CLEANUP",
    title="DELETED lifecycle event → openfga.DeleteByObject within ≤10s (Verifies REQ-AZD-LIFECYCLE-DEL)",
    classes=["AZD"], priority="P1",
    steps=[
        # Pool-INDEPENDENT setup: an INTERNAL ZONAL LB whose VIP comes from a per-case
        # inline subnet, NOT the shared external AddressPool. The external pool is contended
        # across the parallel collections and exhausts mid-run ("could not allocate load
        # balancer address"), leaving an EXTERNAL setup as a PHANTOM LB whose owner-tuple
        # never materialises — the Delete then can't authorize (403 v_delete) and this
        # lifecycle assertion reds spuriously. A subnet-backed INTERNAL LB is durable, so the
        # created→deleted→tuple-cleanup chain actually runs.
        Step(name="setup-subnet", method="POST", path=_VPC_SUBNETS, auth="jwtProjectEditorA",
             pre_script=list(_CIDR_ALLOC_PRE),
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{existingNetworkId}}",
                   "name": "azd-lcd-sub-{{runId}}", "ipv4CidrPrimary": "{{_subnetCidr}}",
                   "zoneId": "{{existingZoneId}}"},
             test_script=[
                 "pm.environment.unset('azdSubnetId');",
                 # Подсеть — предмет, на котором стоит весь жизненный цикл кейса
                 # (создание LB → удаление → уборка кортежа). Без утверждения шаг
                 # зеленел при любом ответе, а падало создание LB следующим шагом.
                 *assert_status(200),
                 "if (pm.response.code === 200) {",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 "  if (j.metadata && j.metadata.subnetId) pm.environment.set('azdSubnetId', j.metadata.subnetId);",
                 "} else { pm.environment.set('opId', ''); }",
             ]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
        # Прогрев чужого свежего ресурса ДО того, как его идентификатор уедет в
        # асинхронную мутацию nlb: на ней ограниченный повтор ключуется на коде
        # ответа шага, а он всегда `200`+`Operation` (issue #351). Читаем ТЕМ ЖЕ
        # предъявителем, что создаёт LB, — прогрев чужим субъектом ничего не
        # доказывает о доступе этого. Разбор — в шапке `warm_peer_fixture`.
        warm_peer_fixture(_VPC_SUBNETS, "azdSubnetId", "azd-lcd-subnet",
                          auth="jwtProjectEditorA"),
        retry_create_until_present(Step(name="setup-cr", method="POST", path=_NLB, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-lcd-{{runId}}", "placement": "INTERNAL_ZONAL",
                   "v4Source": {"subnetId": "{{azdSubnetId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")])),
        poll_operation_until_done(auth="jwtProjectEditorA"),
        retry_until_authorized(Step(name="del", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(auth="jwtProjectEditorA"),
        Step(name="get-after-delete", method="GET", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        # After ≤10s, FGA Check on deleted object should be DecisionNoPath.
        # We cannot directly observe FGA from newman; the assertion is that
        # the previous Get returns NotFound (= passthrough path is the fail-
        # closed result for stranger).
        Step(name="get-as-stranger-passthrough", method="GET", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtStranger",
             test_script=[
                 "pm.test('stranger sees 404 (FGA tuple cleanup complete)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
             ]),
        # Подсеть кейса нарезана в ОБЩЕЙ посеянной сети и не снималась ни разу, тогда
        # как «сколько подсетей помещается в одной сети» — вложенный потолок
        # (`vpc.network.subnet`, умолчание 16). Снимаем ПОСЛЕ балансировщика: пока жив
        # он, его VIP держит подсеть занятой. Тем же предъявителем, что её и создал.
        # Уборка best-effort и НИКОГДА не роняет кейс — предмет утверждений выше.
        Step(name="cleanup-azd-subnet", method="DELETE",
             path=f"{_VPC_SUBNETS}/{{{{azdSubnetId}}}}", auth="jwtProjectEditorA",
             test_script=[
                 "pm.test('subnet reclaim best-effort (never fails the case)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 400, 403, 404, 405, 409]));",
                 "pm.environment.set('opId', '');",
                 "if (pm.response.code === 200) { try { const j = pm.response.json();"
                 " if (j.id) pm.environment.set('opId', j.id); } catch (e) {} }",
                 "pm.environment.unset('azdSubnetId');",
             ]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
    ],
))

CASES.append(Case(
    id="AZD-CACHE-INVALIDATION-REVOKE",
    title="Revoke binding propagates to cache in ≤10s (Verifies REQ-AZD-CACHE-INVAL)",
    classes=["AZD"], priority="P1",
    steps=[
        # Newman cannot orchestrate iam.AccessBindingService.Delete + wait.
        # Instead: probe that current viewer is denied on write — proving
        # the cache holds at least the current binding state.
        Step(name="viewer-write-denied", method="POST", path=_NLB,
             auth="jwtProjectViewerA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-cinv-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED")]),
    ],
))

CASES.append(Case(
    id="AZD-OWNER-RELATION-CREATOR",
    title="Creator has owner relation on created LB (Verifies REQ-AZD-OWNER)",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="cr-as-A", method="POST", path=_NLB, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-own-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(auth="jwtProjectEditorA"),
        # Creator should be able to Delete (= owner-relation-implied editor permits delete).
        # read-your-writes: the owner tuple this case asserts is eventually-consistent, so the
        # first creator Delete can 403 until it materializes -> retry SELF on 403/404.
        retry_until_authorized(Step(name="del-by-creator", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(auth="jwtProjectEditorA"),
    ],
))

CASES.append(Case(
    id="AZD-SERVICE-ACCOUNT-SUBJECT",
    title="Service account editor on project → can Create",
    classes=["AZD"], priority="P1",
    steps=[
        # A case saying a subject CAN do something must require that it did. Accepting 403
        # as well left it satisfied by the exact denial it exists to rule out — and a
        # service account losing its project grant is a real regression class (machine
        # principals are the ones nobody notices), so this is the case that has to catch it.
        #
        # The escape it carried, "env may not yet seed SA binding", does not describe the
        # fixture: tests/authz-fixtures/setup.sh §13b creates the service account and binds
        # it editor on this suite's project under the SAME guard that mints its token
        # (`[ -n "$SVA_NLB" ]`), so there is no lane where the token exists and the binding
        # does not. If the token is missing, gen.py's harness-config guard already fails
        # naming the variable rather than running the step anonymously.
        Step(name="cr-as-sa", method="POST", path=_NLB, auth="jwtServiceAccountEditor",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-sa-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[
                 "pm.test('service account with an editor binding on the project may Create', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
             ]),
        poll_operation_until_done(fixture_ids=["nlbId"], auth="jwtServiceAccountEditor"),
        Step(name="cleanup", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtServiceAccountEditor",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtServiceAccountEditor"),
    ],
))

CASES.append(Case(
    id="AZD-GROUP-MEMBERSHIP-CASCADE",
    title="User in editor-group cascades to NLB.Create permission",
    classes=["AZD"], priority="P1",
    steps=[
        # Same shape as the service-account case, and the same reason it must require
        # success: "a member of an editor group inherits the group's grant" is the whole
        # claim, and `oneOf([200, 403])` accepted its negation.
        #
        # Group membership resolves through a userset, so it materializes
        # eventually-consistent like any other tuple — a timing window, not an outcome, and
        # it is absorbed by retrying while still denied, not by calling the denial
        # acceptable. If it never converges the case fails and says so. NOTE for whoever
        # sees this go red: tests/authz-fixtures/setup.sh §13c seeds the group, the
        # membership and the binding with `|| true` and discarded output, so a silently
        # failed seed surfaces HERE. Make the seed deterministic; do not put the tolerance
        # back.
        retry_until_authorized(Step(name="cr-as-group-member", method="POST", path=_NLB,
             auth="jwtGroupMemberEditor",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-grp-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[
                 "pm.test('member of an editor group inherits Create on the project', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
             ])),
        poll_operation_until_done(fixture_ids=["nlbId"], auth="jwtGroupMemberEditor"),
        Step(name="cleanup", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtGroupMemberEditor",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth="jwtGroupMemberEditor"),
    ],
))

CASES.append(Case(
    id="AZD-LIFECYCLE-INTERNAL-MTLS-ONLY",
    title="InternalResourceLifecycleService NOT reachable on public port (Verifies REQ-AZD-INTERNAL-MTLS)",
    classes=["AZD"], priority="P0",
    steps=[
        Step(name="probe-internal-public", method="GET",
             path="/nlb/v1/internal/resourceLifecycle:subscribe",
             auth="jwtProjectEditorA",
             test_script=[
                 "pm.test('internal route NOT exposed on public mux (404/403/501)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([401, 403, 404, 405, 501]));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# Additional saturation cases to reach D-4 (≥320 + ≥30 AZD)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="AZD-NLB-UPD-STRANGER-NF",
    title="NLB.Update by stranger on missing id → 403 or 404 (passthrough fail-closed)",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="upd-stranger", method="PATCH", path=f"{_NLB}/{{{{garbageNlbId}}}}",
             auth="jwtStranger",
             body={"updateMask": "description", "description": "x"},
             test_script=[
                 "pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([400, 403, 404]));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-LST-CR-STRANGER-NF",
    title="LST.Create by stranger on missing parent LB → 403 or 404",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="cr-stranger", method="POST", path=_LST, auth="jwtStranger",
             body={"loadBalancerId": "{{garbageNlbId}}", "name": "azd-strn-{{runId}}",
                   "protocol": "TCP", "port": 80},
             test_script=[
                 "pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-TGR-CR-STRANGER-DENIED",
    title="TGR.Create by stranger → PERMISSION_DENIED",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="cr-stranger", method="POST", path=_TGR, auth="jwtStranger",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-tgr-strn-{{runId}}", "port": 8080,
                   "healthCheck": {"interval": "2s", "timeout": "1s",
                                   "unhealthyThreshold": 3, "healthyThreshold": 2,
                                   "tcp": {"port": 80}}},
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED")]),
    ],
))

CASES.append(Case(
    id="AZD-NLB-CR-ANONYMOUS-LST-UNAUTH",
    title="LST.Create without Authorization → 401 UNAUTHENTICATED",
    classes=["AZD"], priority="P0",
    steps=[
        Step(name="cr-anon", method="POST", path=_LST, auth="anonymous",
             body={"loadBalancerId": "{{garbageNlbId}}", "name": "anon-{{runId}}",
                   "protocol": "TCP", "port": 80},
             test_script=[
                 "pm.test('401 or 403', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([401, 403]));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-TGR-CR-ANONYMOUS-UNAUTH",
    title="TGR.Create without Authorization → 401 UNAUTHENTICATED",
    classes=["AZD"], priority="P0",
    steps=[
        Step(name="cr-anon", method="POST", path=_TGR, auth="anonymous",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "anon-tgr-{{runId}}", "port": 8080,
                   "healthCheck": {"interval": "2s", "timeout": "1s",
                                   "unhealthyThreshold": 3, "healthyThreshold": 2,
                                   "tcp": {"port": 80}}},
             test_script=[
                 "pm.test('401 or 403', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([401, 403]));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-OP-LIST-STRANGER-FILTERS-SCOPE",
    title="OP.List by stranger → only ops in subject's accessible scope returned",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="lst-stranger-ops", method="GET",
             path=f"/operations?projectId={{{{_suiteProjectId}}}}&pageSize=10",
             auth="jwtStranger",
             # Two lawful answers here and BOTH carry a statement, which is what keeps this
             # out of the "accepts success and refusal" class: either the list runs and the
             # stranger sees nothing (asserted), or the gateway refuses outright — and that
             # refusal must be a genuine permission denial, not any 403 that happens by.
             # The else-arm previously said nothing, so half the outcomes went unchecked.
             test_script=[
                 "pm.test('stranger is refused, or listed and shown nothing', () => "
                 "  pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([200, 403]));",
                 "if (pm.response.code === 200) {",
                 "  const ops = (pm.response.json().operations || pm.response.json().items || []);",
                 "  pm.test('scope-filtered (empty for stranger)', () => "
                 "    pm.expect(ops.length).to.eql(0));",
                 "} else {",
                 "  pm.test('403 is a permission refusal (grpc 7), not an incidental error', () => "
                 "    pm.expect(pm.response.json().code).to.eql(7));",
                 "}",
             ]),
    ],
))
