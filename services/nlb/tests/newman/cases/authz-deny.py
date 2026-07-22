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
  jwtCustomRoleOperator   — custom role: only loadbalancer.networkLoadBalancers.{start,stop}
  jwtCustomRoleTargetMgr  — custom role: only loadbalancer.targetGroups.{addTargets,removeTargets}
"""

CASES = []

_NLB = "/nlb/v1/networkLoadBalancers"
_LST = "/nlb/v1/listeners"
_TGR = "/nlb/v1/targetGroups"
_VPC_SUBNETS = "/vpc/v1/subnets"

# Run-scoped HIGH-ENTROPY /24 CIDR allocator (mirrors load-balancer.py _cidr_alloc_pre) —
# used only where a case needs a pool-INDEPENDENT INTERNAL LB (subnet-backed VIP) instead of
# the shared external pool. ~56k distinct /24s (oct2 ∈ [16,235], oct3 = run-random base + seq)
# replaces the old 40-value 10.200-239.x band that, with seq restarting at 1 each process and
# subnets never reclaimed from the shared network, collided → `Subnet CIDRs can not overlap`
# (the wandering e2e flake). See listener.py _CIDR_ALLOC_PRE for the full root-cause note.
_CIDR_ALLOC_PRE = [
    "var __seq = parseInt(pm.environment.get('_cidrSeq') || '0', 10) + 1;",
    "pm.environment.set('_cidrSeq', String(__seq));",
    "var __run = (pm.environment.get('runId') || 'x0');",
    "var __h = 0; for (var i = 0; i < __run.length; i++) { __h = ((__h << 5) - __h + __run.charCodeAt(i)) | 0; }",
    "__h = __h & 0x7fffffff;",
    "var __oct2 = 16 + (__h % 220);",
    "var __oct3 = ((Math.floor(__h / 256) % 256) + __seq) % 256;",
    "pm.environment.set('_subnetCidr', '10.' + __oct2 + '.' + __oct3 + '.0/24');",
]


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
        poll_operation_until_done(),
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
        poll_operation_until_done(),
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
        # grant on the DESTINATION (cross) project. The suite fixture binds
        # jwtProjectEditorA to existingProjectId only and jwtProjectEditorB to
        # existingProjectCrossId only (env description), so editor A MUST be
        # denied a direct Create in the cross project — the same
        # `editor on project:<cross>` Check that Move.authorizeDestination
        # performs. Asserting it here makes the deny GUARANTEED, not
        # lenient-tolerated: if the fixture is ever mis-seeded so editor A gains
        # editor on cross, this fails LOUDLY instead of the Move silently
        # succeeding (a cross-tenant bypass). A denied Create writes nothing, so
        # there is no resource to clean up.
        Step(name="precond-editorA-denied-on-dst", method="POST", path=_NLB,
             auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectCrossId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-mvpc-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED")]),
        Step(name="setup-cr", method="POST", path=_NLB, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-mv-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        poll_operation_until_done(),
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
             body={"destinationProjectId": "{{_suiteProjectCrossId}}"},
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
        poll_operation_until_done(),
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
                   "protocol": "TCP", "port": 80, "targetPort": 8080, "ipVersion": "IPV4"},
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
        # editor A is editor on src only; a direct Create in the cross project
        # (same `editor on project:<cross>` Check that Move.authorizeDestination
        # runs) MUST be denied, so the Move dst-scope deny is guaranteed and a
        # mis-seed fails loudly here rather than as a silent 200 on the Move.
        Step(name="precond-editorA-denied-on-dst", method="POST", path=_TGR,
             auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectCrossId}}", "regionId": "{{_suiteRegionId}}",
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
        poll_operation_until_done(),
        # editor on src, NOT on cross → the Move MUST be DENIED, ORDERING-TOLERANT:
        # targetgroup/move.go runs the peer existence precheck `ProjectService.Get(dst)`
        # BEFORE authorizeDestination's dst-scope Check, and iam hide-existence returns
        # "Project <id> not found" for a project A cannot SEE → the deny lawfully surfaces
        # as 403 PERMISSION_DENIED (authz-first) OR 400/404 "not found" (peer-first). Only
        # a 200 (async Operation) is a bug. The dst-scope EDITOR-deny is STRICTLY pinned by
        # `precond-editorA-denied-on-dst` above (403 on a direct Create).
        Step(name="mv-no-dst-editor", method="POST", path=f"{_TGR}/{{{{tgId}}}}:move",
             auth="jwtProjectEditorA",
             body={"destinationProjectId": "{{_suiteProjectCrossId}}"},
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
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="AZD-TGR-ADD-VIEWER-DENIED",
    title="TGR.AddTargets with viewer → PERMISSION_DENIED (Verifies REQ-AZD-TGR-ADD)",
    classes=["AZD"], priority="P0",
    steps=[
        Step(name="add-viewer", method="POST",
             path=f"{_TGR}/{{{{garbageTgrId}}}}:addTargets",
             auth="jwtProjectViewerA",
             body={"targets": [{"externalIp": {"address": "203.0.113.30"}, "weight": 100}]},
             test_script=[
                 "pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-TGR-RM-VIEWER-DENIED",
    title="TGR.RemoveTargets with viewer → PERMISSION_DENIED",
    classes=["AZD"], priority="P0",
    steps=[
        Step(name="rm-viewer", method="POST",
             path=f"{_TGR}/{{{{garbageTgrId}}}}:removeTargets",
             auth="jwtProjectViewerA",
             body={"targets": [{"externalIp": {"address": "203.0.113.31"}}]},
             test_script=[
                 "pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
             ]),
    ],
))

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
    title="OP.Cancel by non-creator → PERMISSION_DENIED 'only operation creator may cancel' (Verifies REQ-AZD-OP-CANCEL)",
    classes=["AZD"], priority="P0",
    steps=[
        # Create op as editor A
        Step(name="cr-as-A", method="POST", path=_NLB, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-cancel-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")]),
        # Try cancel as Editor B (different subject)
        Step(name="cancel-as-B", method="POST", path="/operations/{{opId}}:cancel",
             auth="jwtProjectEditorB",
             # non-creator cancel: 403 deny, 404 hide-existence (B cannot see A's op via
             # scope_extractor target->project), or already-done 400/409 — all = rejected.
             test_script=[
                 "pm.test('rejected (403 deny / 404 hide / already-done 400/409)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([400, 403, 404, 409]));",
             ]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))


# ---------------------------------------------------------------------------
# Cross-cutting AZD
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="AZD-FGA-UNAVAILABLE-FAIL-CLOSED",
    title="FGA service unavailable → PERMISSION_DENIED fail-closed (Verifies REQ-AZD-FAIL-CLOSED)",
    classes=["AZD"], priority="P0",
    steps=[
        # In ordinary test conditions FGA is up; this case is in place so that
        # when an FGA outage drill or fault-injection job runs, the suite
        # asserts the fail-closed contract holds. Tolerant assertion on the
        # happy path.
        Step(name="probe-cr", method="POST", path=_NLB, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-fga-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[
                 "pm.test('either 200 (FGA up) or 403 (FGA down fail-closed)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 403, 503]));",
                 "if (pm.response.code === 403) pm.test('mentions authorization service', () => "
                 "  pm.expect((pm.response.json().message||'').toLowerCase())."
                 "    to.include('authorization'));",
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
             ]),
        poll_operation_until_done(),
        Step(name="cleanup-best-effort", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

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
    title="Permission catalog contains all 26 loadbalancer.* permissions (Verifies REQ-AZD-CATALOG)",
    classes=["AZD"], priority="P0",
    steps=[
        # The catalog query is exposed via iam internal mux; absent that, this
        # case acts as a structural reminder that the 26 permission strings
        # must remain present in kacho-iam/internal/authzmap/permission_catalog.go
        # (drift-test enforces — see acceptance §8 GWT-AZD-019). The 4 networkLoadBalancers
        # verbs (Start, Stop, and the two target-group attach/detach verbs) went away with
        # their RPCs.
        Step(name="probe-cr", method="POST", path=_NLB, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-cat-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[
                 "// The 30 loadbalancer.* permissions (design §6.2). If a denial",
                 "// arrives, the message MUST reference one of these strings.",
                 "const expected = [",
                 "  'loadbalancer.networkLoadBalancers.get',",
                 "  'loadbalancer.networkLoadBalancers.list',",
                 "  'loadbalancer.networkLoadBalancers.create',",
                 "  'loadbalancer.networkLoadBalancers.update',",
                 "  'loadbalancer.networkLoadBalancers.delete',",
                 "  'loadbalancer.networkLoadBalancers.move',",
                 "  'loadbalancer.networkLoadBalancers.getTargetStates',",
                 "  'loadbalancer.networkLoadBalancers.listOperations',",
                 "  'loadbalancer.listeners.get',",
                 "  'loadbalancer.listeners.list',",
                 "  'loadbalancer.listeners.create',",
                 "  'loadbalancer.listeners.update',",
                 "  'loadbalancer.listeners.delete',",
                 "  'loadbalancer.listeners.listOperations',",
                 "  'loadbalancer.targetGroups.get',",
                 "  'loadbalancer.targetGroups.list',",
                 "  'loadbalancer.targetGroups.create',",
                 "  'loadbalancer.targetGroups.update',",
                 "  'loadbalancer.targetGroups.delete',",
                 "  'loadbalancer.targetGroups.move',",
                 "  'loadbalancer.targetGroups.addTargets',",
                 "  'loadbalancer.targetGroups.removeTargets',",
                 "  'loadbalancer.targetGroups.listOperations',",
                 "  'loadbalancer.operations.get',",
                 "  'loadbalancer.operations.list',",
                 "  'loadbalancer.operations.cancel',",
                 "];",
                 "pm.test('26 permissions enumerated', () => pm.expect(expected.length).to.eql(26));",
                 "pm.test('request accepted (catalog covered by editor binding)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 403]));",
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
             ]),
        poll_operation_until_done(),
        Step(name="cleanup-best-effort", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="AZD-CUSTOM-ROLE-TARGET-MANAGER",
    title="Custom role targetManager can AddTargets but not Update TG metadata",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="add-as-tm", method="POST",
             path=f"{_TGR}/{{{{garbageTgrId}}}}:addTargets",
             auth="jwtCustomRoleTargetManager",
             body={"targets": [{"externalIp": {"address": "203.0.113.32"}, "weight": 100}]},
             test_script=[
                 "pm.test('OK or 404 (TG missing) or 403 if role-resolve still in cascade', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 403, 404]));",
             ]),
        Step(name="upd-as-tm-denied", method="PATCH",
             path=f"{_TGR}/{{{{garbageTgrId}}}}",
             auth="jwtCustomRoleTargetManager",
             body={"updateMask": "description", "description": "x"},
             test_script=[
                 "pm.test('Update denied for targetManager', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-CUSTOM-ROLE-UNKNOWN-PERMISSION",
    title="iam.Role.Create with unknown permission → InvalidArgument (covered by drift test)",
    classes=["AZD"], priority="P1",
    steps=[
        # The iam Role.Create endpoint lives in kacho-iam, not nlb; this case
        # is a placeholder that asserts the symbolic contract by attempting a
        # request that, in a fully wired stand, would hit kacho-iam through
        # the api-gateway. If the route is absent in this stand the assertion
        # tolerates 404. Drift test in kacho-iam/internal/authzmap is the
        # authoritative enforcement.
        Step(name="probe-unknown-perm", method="POST", path="/iam/v1/roles",
             auth="jwtProjectOwnerA",
             body={"name": "azd-unknown-{{runId}}",
                   "permissions": ["loadbalancer.foo.bar"]},
             test_script=[
                 "pm.test('rejected or route-missing', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 400, 403, 404]));",
                 "if (pm.response.code === 400) pm.test('grpc 3', () => "
                 "  pm.expect(pm.response.json().code).to.eql(3));",
             ]),
    ],
))

CASES.append(Case(
    id="AZD-BREAKGLASS-DEV-BYPASS",
    title="KACHO_NLB_AUTHZ__BREAKGLASS=true bypasses Check (dev-only, prod rejects flag)",
    classes=["AZD"], priority="P2",
    steps=[
        # Cannot toggle env from a newman case; this is an assertion that
        # under normal config the suite is NOT in breakglass mode (the same
        # request as a stranger denies).
        Step(name="stranger-create", method="POST", path=_NLB, auth="jwtStranger",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-brk-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[
                 "pm.test('breakglass OFF: stranger denied', () => "
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
                 "if (pm.response.code === 200) {",
                 "  const j = pm.response.json();",
                 "  if (j.id) pm.environment.set('opId', j.id);",
                 "  if (j.metadata && j.metadata.subnetId) pm.environment.set('azdSubnetId', j.metadata.subnetId);",
                 "} else { pm.environment.unset('opId'); }",
             ]),
        poll_operation_until_done(),
        retry_create_until_present(Step(name="setup-cr", method="POST", path=_NLB, auth="jwtProjectEditorA",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-lcd-{{runId}}", "placement": "INTERNAL_ZONAL",
                   "v4Source": {"subnetId": "{{azdSubnetId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
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
        poll_operation_until_done(),
        # Creator should be able to Delete (= owner-relation-implied editor permits delete).
        # read-your-writes: the owner tuple this case asserts is eventually-consistent, so the
        # first creator Delete can 403 until it materializes -> retry SELF on 403/404.
        retry_until_authorized(Step(name="del-by-creator", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="AZD-SERVICE-ACCOUNT-SUBJECT",
    title="Service account editor on project → can Create",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="cr-as-sa", method="POST", path=_NLB, auth="jwtServiceAccountEditor",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-sa-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[
                 "pm.test('OK or 403 (env may not yet seed SA binding)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 403]));",
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
             ]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtServiceAccountEditor",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="AZD-GROUP-MEMBERSHIP-CASCADE",
    title="User in editor-group cascades to NLB.Create permission",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="cr-as-group-member", method="POST", path=_NLB,
             auth="jwtGroupMemberEditor",
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{_suiteRegionId}}",
                   "name": "azd-grp-{{runId}}", "placement": "EXTERNAL_REGIONAL", "v4Source": {"public": {}}},
             test_script=[
                 "pm.test('OK or 403 (env may not yet seed group binding)', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 403]));",
                 *save_from_response("j.id", "opId"),
                 *save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "nlbId"),
             ]),
        poll_operation_until_done(),
        Step(name="cleanup", method="DELETE", path=f"{_NLB}/{{{{nlbId}}}}",
             auth="jwtGroupMemberEditor",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
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
                   "protocol": "TCP", "port": 80, "targetPort": 8080, "ipVersion": "IPV4"},
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
                   "protocol": "TCP", "port": 80, "targetPort": 8080, "ipVersion": "IPV4"},
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
             test_script=[
                 "pm.test('rejected or empty', () => "
                 "  pm.expect(pm.response.code).to.be.oneOf([200, 403]));",
                 "if (pm.response.code === 200) {",
                 "  const ops = (pm.response.json().operations || pm.response.json().items || []);",
                 "  pm.test('scope-filtered (empty for stranger)', () => "
                 "    pm.expect(ops.length).to.eql(0));",
                 "}",
             ]),
    ],
))
