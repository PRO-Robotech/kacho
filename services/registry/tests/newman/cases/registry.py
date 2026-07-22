# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для RegistryService (kacho-registry) — полное control-plane покрытие.

Black-box через api-gateway REST (`/registry/v1/registries`), исполняется
umbrella-harness'ом (gen.py DSL: Case/Step/assert_* + poll_operation_until_done).
Покрыт КАЖДЫЙ RPC RegistryService — happy + negative + corner:

  Create            — happy · invalid-name · project-not-found · duplicate(409) · missing-name
  Get               — happy · malformed-id(400) · well-formed-absent(404)
  List              — happy(contains) · garbage-token(400) · pageSize BVA · non-member(empty)
  Update            — happy · unknown-mask(400) · immutable name/projectId(400) · empty-mask · not-found(404)
  ListRepositories  — happy · garbage-token(400) · registry-not-found(404)
  ListTags          — happy(authorized) · garbage-token(400) · unauthorized/absent(404)
  DeleteTag         — absent-tag(idempotent/404) · malformed-id(400)
  Delete            — happy · not-found(404) · double-delete(idempotent)

Мутации (Create/Update/Delete/DeleteTag) — async: возвращают Operation envelope
(op-id prefix `rop`), поллятся через generic `/operations/{{opId}}`; после done —
`response.id` = registry id. Read (Get/List/ListRepositories/ListTags) — sync (200).

Само-достаточность и идемпотентность: shared setup (`REG-CR-CRUD-OK`) создаёт
общий registry и сохраняет `{{regId}}`; read/update-кейсы работают над ним;
delete/duplicate/double-delete создают СВОИ throwaway-реестры и убирают их внутри
кейса; финальный `REG-CLEANUP` сносит `{{regId}}`. Каждый ресурс изолирован
`-{{runId}}`-суффиксом.
"""

CASES = []

REG = "/registry/v1/registries"
OP_ENVELOPE = "^(rop|reo)[a-z0-9]+$"


# ---------------------------------------------------------------------------
# Shared setup / cleanup helpers (self-contained, idempotent)
# ---------------------------------------------------------------------------

def _create_registry(name_expr, id_var, project="{{existingProjectId}}",
                     description="CI images", labels=None):
    """Setup helper: Create registry (async Op) → poll → capture registry id.

    Emits create → poll → capture(GET /operations/{{opId}}). The capture step
    asserts the operation succeeded (ACTIVE, prefix `reg`, endpoint) and saves the
    resource id into env var `id_var` from the operation response. Returns list[Step].
    """
    # regionId — REGIONAL placement-якорь, обязателен на Create (REG-1 F4, peer-validate
    # geo). existingRegionId сидится kacho-deploy (та же geo-фикстура, что nlb/compute).
    body = {"name": name_expr, "projectId": project, "regionId": "{{existingRegionId}}",
            "description": description, "labels": ({"env": "prod"} if labels is None else labels)}
    return [
        Step(name="create-" + id_var, method="POST", path=REG, body=body,
             test_script=[
                 *assert_status(200),
                 *assert_operation_envelope(OP_ENVELOPE),
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(),
        Step(name="capture-" + id_var, method="GET", path="/operations/{{opId}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                 "const r = (pm.response.json().response) || {};",
                 "pm.test('status ACTIVE', () => pm.expect(r.status).to.eql('REGISTRY_STATUS_ACTIVE'));",
                 "pm.test('id prefix reg', () => pm.expect((r.id||'').startsWith('reg')).to.be.true);",
                 "pm.test('endpoint reflects id', () => pm.expect(r.endpoint||'').to.include(r.id||'__no_id__'));",
                 *save_from_response("(j.response&&j.response.id)||''", id_var),
             ]),
        # Read-your-writes warm-up: force the registry owner-tuple to materialize
        # (register-outbox → drainer → IAM RegisterResource → FGA reconciler) before
        # the case's own first Get/Update/Delete. GetRegistry does a gateway scope-Check
        # (registry_registry/registry_id, hide-existence) → 404 until the tuple is
        # visible; bounded-retry over that EC window (own fresh resource only). Once this
        # succeeds, every subsequent access to {{id_var}} is deterministic. (e2e-inv:
        # wrap ONLY the first self-access; never negatives/cross-account.)
        retry_until_authorized(
            Step(name="warm-" + id_var, method="GET", path=REG + "/{{" + id_var + "}}",
                 test_script=[*assert_status(200)])),
    ]


def _delete_registry(id_var, tolerant=False):
    """Cleanup helper: Delete registry {{id_var}} (async Op) → poll to done.

    tolerant=True accepts 200 (Operation) OR 404 (already gone) so a re-run of the
    suite stays green even if a prior step already removed the resource.
    """
    codes = "[200, 404]" if tolerant else "[200]"
    return [
        Step(name="delete-" + id_var, method="DELETE", path=REG + "/{{" + id_var + "}}",
             test_script=[
                 "pm.test('delete accepted', () => pm.expect(pm.response.code).to.be.oneOf(" + codes + "));",
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(),
    ]


# ===========================================================================
# Create
# ===========================================================================

# Shared setup + Create happy: Op → poll → response Registry (prefix reg, ACTIVE,
# endpoint) → Get echoes name/projectId. Saves {{regId}} for the rest of the file.
CASES.append(Case(
    id="REG-CR-CRUD-OK",
    title="Create registry → Operation → poll → Registry(prefix reg, ACTIVE, endpoint) → Get echoes name/projectId",
    classes=["CRUD"], priority="P1",
    steps=[
        *_create_registry("team-images-{{runId}}", "regId"),
        Step(name="get-created", method="GET", path=REG + "/{{regId}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('name matches', () => pm.expect(j.name).to.eql('team-images-'+pm.environment.get('runId')));",
                 "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('existingProjectId')));",
                 "pm.test('status ACTIVE', () => pm.expect(j.status).to.eql('REGISTRY_STATUS_ACTIVE'));",
             ]),
    ],
))

# Create invalid name: uppercase and underscore both violate DNS-safe name → 400.
CASES.append(Case(
    id="REG-CR-NEG-INVALID-NAME",
    title="Create with non-DNS-safe name (uppercase / underscore) → 400 INVALID_ARGUMENT, no Operation",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        Step(name="create-uppercase", method="POST", path=REG,
             body={"name": "TeamImages", "projectId": "{{existingProjectId}}", "regionId": "{{existingRegionId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        Step(name="create-underscore", method="POST", path=REG,
             body={"name": "team_images", "projectId": "{{existingProjectId}}", "regionId": "{{existingRegionId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

# Create into a well-formed-but-absent projectId → rejected. Two defensible codes,
# authz-first (e2e-inv «Authz-first толерантность негативов»): the api-gateway runs the
# project-scope FGA-Check (Create scope=project/project_id) BEFORE the backend peer-
# validate — the caller holds no `editor` grant on a non-existent project, so it fails
# closed with 403 (PERMISSION_DENIED) and never reaches the backend cross-domain reject
# (iam ProjectService.Get NotFound → 400 "project ... not found"). Tolerate [400,403];
# assert the reject is real and text-appropriate to whichever gate fired. NOT wrapped in
# retry (a genuine deny must not be masked).
CASES.append(Case(
    id="REG-CR-NEG-PROJECT-NOTFOUND",
    title="Create with unknown projectId → rejected 403 (authz-first) or 400 (backend not-found)",
    classes=["NEG", "AZ"], priority="P1",
    steps=[Step(name="create-nopr", method="POST", path=REG,
                body={"name": "x-{{runId}}", "projectId": "{{garbageProjectId}}", "regionId": "{{existingRegionId}}"},
                test_script=[
                    "pm.test('rejected authz-first (403) or backend not-found (400)', () => pm.expect(pm.response.code).to.be.oneOf([400, 403]));",
                    "const j = pm.response.json();",
                    "if (pm.response.code === 400) {",
                    "  pm.test('grpc code 3 (INVALID_ARGUMENT)', () => pm.expect(j.code).to.eql(3));",
                    "  pm.test('backend not-found text', () => pm.expect((j.message||'').toLowerCase()).to.include('not found'));",
                    "} else {",
                    "  pm.test('grpc code 7 (PERMISSION_DENIED)', () => pm.expect(j.code).to.eql(7));",
                    "}",
                ])],
))

# Create duplicate (project_id, name): partial UNIQUE → sync 409 ALREADY_EXISTS;
# tolerate an async op-error path (INSERT race) too. Uses its own registry+name.
CASES.append(Case(
    id="REG-CR-CONF-ALREADY-EXISTS",  # index: REG-04
    title="Create duplicate (project_id,name) → 409 ALREADY_EXISTS (sync) or async op-error",
    classes=["CONF", "NEG", "IDEM"], priority="P1",
    steps=[
        *_create_registry("dup-images-{{runId}}", "dupRegId"),
        Step(name="create-dup", method="POST", path=REG,
             body={"name": "dup-images-{{runId}}", "projectId": "{{existingProjectId}}",
                   "regionId": "{{existingRegionId}}",
                   "description": "duplicate attempt", "labels": {"env": "prod"}},
             test_script=[
                 "pm.test('duplicate rejected (409 sync or 200 async-error)', () => pm.expect(pm.response.code).to.be.oneOf([200, 409]));",
                 "const j = pm.response.json();",
                 "if (pm.response.code === 409) {",
                 "  pm.test('grpc code 6 (ALREADY_EXISTS)', () => pm.expect(j.code).to.eql(6));",
                 "  pm.test('mentions already exists', () => pm.expect((j.message||'').toLowerCase()).to.include('already exists'));",
                 "  pm.environment.set('dupSync409', '1');",
                 "} else {",
                 "  pm.environment.unset('dupSync409');",
                 "  if (j.id) pm.environment.set('opId', String(j.id));",
                 "}",
             ]),
        poll_operation_until_done(),
        Step(name="assert-dup-async", method="GET", path="/operations/{{opId}}",
             test_script=[
                 "if (pm.environment.get('dupSync409') === '1') {",
                 "  pm.test('duplicate handled synchronously (409)', () => pm.expect(true).to.eql(true));",
                 "} else {",
                 "  const j = pm.response.json();",
                 "  pm.test('async op done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                 "  const blob = (JSON.stringify(j.error||{}) + (pm.environment.get('lastOpError')||'')).toLowerCase();",
                 "  pm.test('async op errored ALREADY_EXISTS', () => pm.expect(blob).to.include('exist'));",
                 "}",
             ]),
        *_delete_registry("dupRegId", tolerant=True),
    ],
))

# Create missing required name → 400 INVALID_ARGUMENT.
CASES.append(Case(
    id="REG-CR-NEG-MISSING-NAME",  # index: REG-02
    title="Create with no name field → 400 INVALID_ARGUMENT (name required)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[Step(name="create-noname", method="POST", path=REG,
                body={"projectId": "{{existingProjectId}}", "regionId": "{{existingRegionId}}", "description": "no name"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))


# ===========================================================================
# Get
# ===========================================================================

# Get happy — fields match the shared registry.
CASES.append(Case(
    id="REG-GET-CRUD-OK",  # index: REG-05
    title="Get registry → 200, fields (id/name/projectId/status/endpoint) match",
    classes=["CRUD"], priority="P1",
    steps=[Step(name="get-ok", method="GET", path=REG + "/{{regId}}",
                test_script=[
                    *assert_status(200),
                    "const j = pm.response.json();",
                    "pm.test('id matches regId', () => pm.expect(j.id).to.eql(pm.environment.get('regId')));",
                    "pm.test('name matches', () => pm.expect(j.name).to.eql('team-images-'+pm.environment.get('runId')));",
                    "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('existingProjectId')));",
                    "pm.test('status ACTIVE', () => pm.expect(j.status).to.eql('REGISTRY_STATUS_ACTIVE'));",
                    "pm.test('id prefix reg', () => pm.expect((j.id||'').startsWith('reg')).to.be.true);",
                    "pm.test('endpoint reflects id', () => pm.expect(j.endpoint||'').to.include(j.id||'__no_id__'));",
                ])],
))

# Get malformed id → 400 INVALID_ARGUMENT. The gateway authz-edge validates the id
# generically (it does not know the concrete resource type) → "invalid resource id '<X>'".
CASES.append(Case(
    id="REG-GET-NEG-MALFORMED-ID",
    title="Get not-an-id → 400 INVALID_ARGUMENT (\"invalid resource id\")",
    classes=["NEG", "VAL"], priority="P0",
    steps=[Step(name="get-bad", method="GET", path=REG + "/not-an-id",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('invalid id text', () => pm.expect((pm.response.json().message||'')).to.include('invalid resource id'));"])],
))

# Get well-formed-but-absent → 404 NOT_FOUND (existence-hidden, no deny_reasons leak).
CASES.append(Case(
    id="REG-GET-NEG-NOTFOUND",
    title="Get reg-DOESNOTEXIST00000 → 404 NOT_FOUND (existence-hidden, no deny_reasons)",
    classes=["NEG"], priority="P1",
    steps=[Step(name="get-nx", method="GET", path=REG + "/reg-DOESNOTEXIST00000",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             "pm.test('no deny_reasons leak', () => pm.expect(JSON.stringify(pm.response.json())).to.not.include('deny_reasons'));"])],
))


# ===========================================================================
# List
# ===========================================================================

# List happy — project-scoped array; contains created regId (grant-latency tolerant
# via setNextRequest self-retry, mirroring poll_operation_until_done).
CASES.append(Case(
    id="REG-LST-CRUD-OK",
    title="List registries (project-scope) → array, contains created regId (poll-retry on grant-latency)",
    classes=["CRUD"], priority="P1",
    steps=[Step(name="list-contains", method="GET", path=REG + "?projectId={{existingProjectId}}&pageSize=1000",
                test_script=[
                    "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                    "const j = pm.response.json();",
                    "const regs = j.registries || [];",
                    "pm.test('registries is array', () => pm.expect(regs).to.be.an('array'));",
                    "const target = pm.environment.get('regId');",
                    "const found = regs.some(r => r.id === target);",
                    "const rc = parseInt(pm.environment.get('_listRetry') || '0', 10);",
                    "if (!found && rc < 6) {",
                    "  pm.environment.set('_listRetry', String(rc + 1));",
                    "  pm.execution.setNextRequest(pm.info.requestName);",
                    "  return;",
                    "}",
                    "pm.environment.unset('_listRetry');",
                    "pm.test('list contains created regId', () => pm.expect(found, 'regId '+target+' in list').to.be.true);",
                ])],
))

# List garbage page_token → 400 INVALID_ARGUMENT.
CASES.append(Case(
    id="REG-LST-NEG-BAD-TOKEN",  # index: REG-06
    title="List with garbage page_token → 400 INVALID_ARGUMENT",
    classes=["NEG", "VAL"], priority="P1",
    steps=[Step(name="list-bad-token", method="GET",
                path=REG + "?projectId={{existingProjectId}}&pageToken=not-a-b64-token",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# List page_size BVA — 0 → default (50), 1000 → max ok. Both 200.
CASES.append(Case(
    id="REG-LST-BVA-PAGESIZE",  # index: REG-06
    title="List page_size boundary — 0 → default, 1000 → max ok (both 200)",
    classes=["BVA"], priority="P2",
    steps=[
        Step(name="list-ps-0", method="GET", path=REG + "?projectId={{existingProjectId}}&pageSize=0",
             test_script=[*assert_status(200),
                          "pm.test('registries is array (ps=0 default)', () => pm.expect(pm.response.json().registries || []).to.be.an('array'));"]),
        Step(name="list-ps-1000", method="GET", path=REG + "?projectId={{existingProjectId}}&pageSize=1000",
             test_script=[*assert_status(200),
                          "pm.test('registries is array (ps=1000 max)', () => pm.expect(pm.response.json().registries || []).to.be.an('array'));"]),
    ],
))

# List over a project the caller is NOT a member of → 200 scope-filtered array that
# does not leak the caller's own regId (non-member / empty, no cross-tenant leak).
CASES.append(Case(
    id="REG-LST-AZ-NONMEMBER-EMPTY",  # index: REG-06
    title="List over non-member project → 200 array, does not leak own regId",
    classes=["AZ", "NEG"], priority="P1",
    steps=[Step(name="list-cross", method="GET", path=REG + "?projectId={{existingProjectCrossId}}",
                test_script=[
                    *assert_status(200),
                    "const regs = pm.response.json().registries || [];",
                    "pm.test('registries is array', () => pm.expect(regs).to.be.an('array'));",
                    "pm.test('no cross-tenant leak of own regId', () => pm.expect(regs.some(r => r.id === pm.environment.get('regId'))).to.eql(false));",
                ])],
))

# List filter=name — whitelist filter (`corelib filter.Parse(q.Filter, ["name"])`).
# Filtering by the shared registry's exact name returns only matching rows; tolerant of
# grant-latency (the created reg may not be visible yet → array may be empty, but any
# returned item MUST carry the filtered name — proves the whitelist filter is applied).
CASES.append(Case(
    id="REG-LST-FILTER-NAME-OK",  # index: REG-06
    title="List filter=name=\"team-images-{{runId}}\" → 200, returned registries all match the filtered name",
    classes=["CRUD", "VAL"], priority="P1",
    steps=[Step(name="list-filter-name", method="GET",
                path=REG + "?projectId={{existingProjectId}}&filter=name%3D%22team-images-{{runId}}%22",
                test_script=[
                    *assert_status(200),
                    "const regs = pm.response.json().registries || [];",
                    "pm.test('registries is array', () => pm.expect(regs).to.be.an('array'));",
                    "const target = 'team-images-' + pm.environment.get('runId');",
                    "pm.test('filter=name returns only matching registries (grant-latency: array may be empty)', () => regs.forEach(r => pm.expect(r.name, JSON.stringify(r)).to.eql(target)));",
                ])],
))

# List with an unknown filter field (not in the name-whitelist) → 400 INVALID_ARGUMENT:
# `filter.Parse` rejects any field outside `["name"]` before the query runs.
CASES.append(Case(
    id="REG-LST-FILTER-GARBAGE-400",  # index: REG-06
    title="List filter=notafield=x (unknown field, not in name-whitelist) → 400 INVALID_ARGUMENT",
    classes=["NEG", "VAL"], priority="P1",
    steps=[Step(name="list-filter-garbage", method="GET",
                path=REG + "?projectId={{existingProjectId}}&filter=notafield%3Dx",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))


# ===========================================================================
# Update
# ===========================================================================

# Update happy — labels + description via updateMask; poll → Get reflects.
CASES.append(Case(
    id="REG-UPD-CRUD-OK",
    title="Update labels+description (updateMask) → Operation → poll → Get reflects new fields",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="update", method="PATCH", path=REG + "/{{regId}}",
             body={"updateMask": "labels,description",
                   "labels": {"env": "staging", "team": "core"},
                   "description": "staging CI images"},
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-updated", method="GET", path=REG + "/{{regId}}",
             test_script=[
                 "pm.test('update op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('description updated', () => pm.expect(j.description).to.eql('staging CI images'));",
                 "pm.test('label team=core', () => pm.expect((j.labels||{}).team).to.eql('core'));",
             ]),
    ],
))

# Update with unknown update_mask field → 400 INVALID_ARGUMENT (mask whitelist).
CASES.append(Case(
    id="REG-UPD-NEG-UNKNOWN-MASK",  # index: REG-36
    title="Update updateMask=bogus (unknown field) → 400 INVALID_ARGUMENT",
    classes=["NEG", "VAL", "CONF"], priority="P1",
    steps=[Step(name="update-unknown-mask", method="PATCH", path=REG + "/{{regId}}",
                body={"updateMask": "bogus", "description": "ignored"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# Update name via mask → mutable косметический label (REG-1 F2, был immutable до REG-1).
# name — не идентичность (её несёт immutable id, F1); смена name — обычный Update под
# update_mask-дисциплиной. Deep-инвариант «rename name НЕ ломает id/endpoint/pull-URL»
# (REG-1-07) локается в cases/registry-redesign.py (REG-RD-F2-RENAME-STABLE-ID).
CASES.append(Case(
    id="REG-UPD-CRUD-RENAME-NAME",  # index: REG-36
    title="Update updateMask=name → Operation → Get reflects new name (REG-1 F2: name mutable, id stable)",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="update-name", method="PATCH", path=REG + "/{{regId}}",
             body={"updateMask": "name", "name": "team-images-r-{{runId}}"},
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-renamed", method="GET", path=REG + "/{{regId}}",
             test_script=[
                 "pm.test('rename op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('name mutated', () => pm.expect(j.name).to.eql('team-images-r-'+pm.environment.get('runId')));",
                 "pm.test('id unchanged (identity stable across rename)', () => pm.expect(j.id).to.eql(pm.environment.get('regId')));",
             ]),
    ],
))

# Update immutable projectId in mask → 400 INVALID_ARGUMENT ("projectId is immutable after Registry.Create").
CASES.append(Case(
    id="REG-UPD-NEG-IMMUTABLE-PROJECT",  # index: REG-36
    title="Update updateMask=projectId → 400 INVALID_ARGUMENT (projectId immutable)",
    classes=["NEG", "CONF"], priority="P1",
    steps=[Step(name="update-project", method="PATCH", path=REG + "/{{regId}}",
                body={"updateMask": "projectId"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('immutable text', () => pm.expect((pm.response.json().message||'')).to.include('immutable'));"])],
))

# Update empty mask → full-object PATCH (all mutable fields applied); poll → Get reflects.
CASES.append(Case(
    id="REG-UPD-CRUD-EMPTY-MASK",  # index: REG-36
    title="Update with empty updateMask → full-object PATCH (labels+description applied)",
    classes=["CRUD"], priority="P2",
    steps=[
        Step(name="update-empty", method="PATCH", path=REG + "/{{regId}}",
             body={"description": "full-patch-{{runId}}", "labels": {"tier": "gold"}},
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-empty-mask", method="GET", path=REG + "/{{regId}}",
             test_script=[
                 "pm.test('empty-mask op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('description full-patched', () => pm.expect(j.description).to.eql('full-patch-'+pm.environment.get('runId')));",
                 "pm.test('label tier=gold', () => pm.expect((j.labels||{}).tier).to.eql('gold'));",
             ]),
    ],
))

# Update well-formed-but-absent registry → sync NOT_FOUND 404 (existence-hiding).
# RegistryService.Update carries hide_existence=true (f06e01b, security.md #6): an
# authenticated deny — including a non-resolvable/absent target whose owner-tuple the
# gateway scope-Check cannot map to a project — surfaces as opaque NotFound(5)/404, NOT
# 403+deny_reasons and NOT an async Operation. Existence indistinguishable from a real
# miss; no relation-enumeration leak. (Parity with registry-authz update-viewer.)
CASES.append(Case(
    id="REG-UPD-NEG-NOTFOUND",  # index: REG-36
    title="Update well-formed-absent reg → 404 NOT_FOUND (hide-existence, no deny_reasons leak)",
    classes=["NEG", "AZ", "CONF"], priority="P1",
    steps=[
        Step(name="update-nx", method="PATCH", path=REG + "/reg00000000000000000",
             body={"updateMask": "description", "description": "x"},
             test_script=[
                 *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('no deny_reasons/relation leak (existence-hiding)', () => pm.expect(JSON.stringify(pm.response.json())).to.not.match(/deny_reasons|direct relations|lacks relation/));",
             ]),
    ],
))


# ===========================================================================
# ListRepositories (sync projection from zot)
# ===========================================================================

# ListRepositories happy — 200, repositories[] array (empty on a fresh namespace).
CASES.append(Case(
    id="REG-LSTREPO-CRUD-OK",  # index: REG-22
    title="ListRepositories for registry → 200 repositories[] array",
    classes=["CRUD"], priority="P1",
    steps=[Step(name="list-repos", method="GET", path=REG + "/{{regId}}/repositories",
                test_script=[*assert_status(200),
                             "pm.test('repositories is array', () => pm.expect(pm.response.json().repositories || []).to.be.an('array'));"])],
))

# ListRepositories garbage page_token → 400.
CASES.append(Case(
    id="REG-LSTREPO-NEG-BAD-TOKEN",  # index: REG-22
    title="ListRepositories with garbage page_token → 400 INVALID_ARGUMENT",
    classes=["NEG", "VAL"], priority="P2",
    steps=[Step(name="list-repos-bad-token", method="GET",
                path=REG + "/{{regId}}/repositories?pageToken=not-a-b64-token",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# ListRepositories on an absent/unauthorized registry → 200 with an empty repositories[]
# array — Kachō existence-hiding (absent → empty projection, never a 404 leak).
CASES.append(Case(
    id="REG-LSTREPO-NEG-NOTFOUND",  # index: REG-22
    title="ListRepositories for absent registry → 404 NotFound (existence-hiding via NotFound; namespace-scope does not resolve)",
    classes=["NEG"], priority="P2",
    # By-design (#64): ListRepositories gates on the parent registry via namespaceGate
    # (handler listauthz.go). An absent OR unauthorized registry does not resolve the
    # namespace scope → errHideExistence() → NOT_FOUND (code 5). This is byte-identical
    # for absent vs unauthorized (no existence-oracle) and is PARITY with the other
    # registry list-of-children RPCs (ListTags / ListReferrers checkRepo→NotFound) and
    # GetRepository (deny/absent→NotFound), unit-locked by
    # TestHandler_REG22_ListRepositories_NamespaceDeny_NotFound. Convention: a List
    # returns 200-empty when its parent scope RESOLVES but is empty; 404/403 when the
    # parent scope does not resolve (authz-first). (Was 200-empty here — that expectation
    # was masked by the pre-fix GetRepository route-shadow and is corrected to match the
    # implemented, unit-tested contract.)
    steps=[Step(name="list-repos-nx", method="GET", path=REG + "/reg00000000000000000/repositories",
                test_script=[*assert_status(404),
                             "pm.test('grpc code 5 (NOT_FOUND, hide-existence)', () => pm.expect((pm.response.json()||{}).code).to.eql(5));"])],
))


# ListRepositories pageSize > max (1000) → 400 INVALID_ARGUMENT (BVA: отвергается, НЕ
# clamp'ится). Parity с REG-RD-F8-NEG-PAGESIZE-OVERMAX (ListRegistries) — format-validate
# ДО listauthz empty-grant short-circuit (security.md #7 / api-conventions §Pagination).
CASES.append(Case(
    id="REG-LSTREPO-NEG-PAGESIZE-OVERMAX",  # index: REG-22
    title="ListRepositories pageSize=1001 (> max 1000) → 400 INVALID_ARGUMENT (rejected not clamped, BVA)",
    classes=["NEG", "BVA", "VAL"], priority="P2",
    steps=[Step(name="list-repos-ps-overmax", method="GET",
                path=REG + "/{{regId}}/repositories?pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))


# ===========================================================================
# ListTags (sync projection from zot)
# ===========================================================================

# ListTags happy (authorized owner). No pushed image → repo absent, so the routed
# read is 200 (empty tags[]) OR 404 (repo absent) — never 401/403. A real
# 200-with-tags path needs a data-plane push, out of control-plane newman scope.
CASES.append(Case(
    id="REG-LSTTAGS-CRUD-OK",  # index: REG-24
    title="ListTags on own namespace → authorized read routes (200 tags[] or 404 repo-absent, not 401/403)",
    classes=["CRUD"], priority="P1",
    steps=[Step(name="list-tags", method="GET", path=REG + "/{{regId}}/repositories/app-{{runId}}/tags",
                test_script=[
                    "pm.test('authorized read routes (200 or 404, not 401/403)', () => pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
                    "const j = pm.response.json();",
                    "if (pm.response.code === 200) {",
                    "  pm.test('tags is array', () => pm.expect(j.tags || []).to.be.an('array'));",
                    "}",
                ])],
))

# ListTags garbage page_token → rejected. ListTags is EXISTENCE-HIDING (RG-1 overlay
# A08/ListTags parity): the handler runs the per-repo v_list Check on
# registry_repository:<reg>/<repo> (public.go:182, deny/absent → NOT_FOUND) BEFORE the
# adapter decodes the page_token. The probe repo `app-{{runId}}` is never pushed → the
# existence-hiding 404 preempts token-validation. On a VISIBLE repo the bad token would
# surface as 400 INVALID_ARGUMENT. Tolerate [400,404]; invariant — a garbage token never
# yields 200. (Unlike ListRegistries REG-1-31, ListTags is NOT a listauthz short-circuit,
# so security.md #7 format-before-authz does not mandate 400 for a hidden repo.)
CASES.append(Case(
    id="REG-LSTTAGS-NEG-BAD-TOKEN",  # index: REG-24
    title="ListTags garbage page_token → 400 (visible repo) or 404 (existence-hiding preempts on absent repo), never 200",
    classes=["NEG", "VAL"], priority="P2",
    steps=[Step(name="list-tags-bad-token", method="GET",
                path=REG + "/{{regId}}/repositories/app-{{runId}}/tags?pageToken=not-a-b64-token",
                test_script=[
                    "pm.test('rejected: 400 (bad token) or 404 (existence-hiding), never 200', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));",
                    "const j = pm.response.json();",
                    "pm.test('grpc code 3 (INVALID_ARGUMENT) or 5 (NOT_FOUND)', () => pm.expect(j.code).to.be.oneOf([3, 5]));",
                ])],
))

# ListTags by an unauthorized subject (stranger) → existence-hiding: 404 (or 401 if
# anonymous), never 403, no deny_reasons leak.
CASES.append(Case(
    id="REG-LSTTAGS-AZ-NOTFOUND",  # index: REG-24
    title="ListTags by non-member (stranger) → 404/empty (existence-hidden), never 403",
    classes=["NEG", "AZ"], priority="P1",
    steps=[Step(name="list-tags-stranger", method="GET",
                path=REG + "/{{regId}}/repositories/app-{{runId}}/tags", auth="jwtStranger",
                # existence-hiding касается AUTHENTICATED-но-без-грантов (ответ не раскрывает
                # существование чужого repo). Для 401 (unauthenticated) deny_reason "subject
                # unauthenticated" — generic auth-failure, не leak существования → проверку пропускаем.
                test_script=[
                    "pm.test('unauthorized -> 401/404/empty (existence-hidden), never 403', () => pm.expect(pm.response.code).to.be.oneOf([200, 401, 404]));",
                    "pm.test('never 403 (deny -> 404 no-leak)', () => pm.expect(pm.response.code).to.not.eql(403));",
                    "if (pm.response.code !== 401) { pm.test('authenticated deny -> no resource-existence leak', () => pm.expect(JSON.stringify(pm.response.json())).to.not.include('deny_reasons')); }",
                ])],
))


# ListTags pageSize > max (1000) → rejected (BVA: отвергается, НЕ clamp'ится). ListTags
# is existence-hiding: the per-repo v_list Check (public.go:182) fires BEFORE pageSize
# validation, so on the never-pushed probe repo `app-{{runId}}` the existence-hiding 404
# preempts. On a VISIBLE repo pageSize=1001 surfaces as 400 INVALID_ARGUMENT. Tolerate
# [400,404]; invariant — an over-max pageSize is never clamped into a 200 window.
CASES.append(Case(
    id="REG-LSTTAGS-NEG-PAGESIZE-OVERMAX",  # index: REG-24
    title="ListTags pageSize=1001 (>max) → 400 (visible repo) or 404 (existence-hiding preempts), never clamped to 200",
    classes=["NEG", "BVA", "VAL"], priority="P2",
    steps=[Step(name="list-tags-ps-overmax", method="GET",
                path=REG + "/{{regId}}/repositories/app-{{runId}}/tags?pageSize=1001",
                test_script=[
                    "pm.test('rejected: 400 (over-max) or 404 (existence-hiding), never 200', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));",
                    "const j = pm.response.json();",
                    "pm.test('grpc code 3 (INVALID_ARGUMENT) or 5 (NOT_FOUND)', () => pm.expect(j.code).to.be.oneOf([3, 5]));",
                ])],
))


# ===========================================================================
# ListOperations (sync per-resource op-history; v_list@registry_registry)
# ===========================================================================
# RegistryService/ListOperations — история async-операций реестра, фильтр
# resource_id=registry_id, per-resource listauthz (repo-scoped ops дропаются без
# per-repo v_list). Scope-extractored (v_list) → gateway валидирует id + Check.

# ListOperations happy — 200, operations[] array (создание {{regId}} само породило
# op → ожидаем непустой массив, но grant-latency-толерантно: retry на 403/404 своего
# свежего ресурса, затем 200 + array; наличие конкретной op не жёстко ассертим).
CASES.append(Case(
    id="REG-LSTOPS-CRUD-OK",  # index: REG-06
    title="ListOperations for own registry → 200 operations[] array (grant-latency tolerant)",
    classes=["CRUD"], priority="P1",
    steps=[Step(name="list-ops", method="GET", path=REG + "/{{regId}}/operations",
                test_script=[
                    "const _n = parseInt(pm.environment.get('_lstOpsRetry') || '0', 10);",
                    "if ((pm.response.code === 403 || pm.response.code === 404) && _n < 20) {",
                    "  pm.environment.set('_lstOpsRetry', String(_n + 1));",
                    "  const _pd = Date.now(); while (Date.now() - _pd < 500) { /* owner-tuple EC wait */ }",
                    "  pm.execution.setNextRequest(pm.info.requestName);",
                    "  return;",
                    "}",
                    "pm.environment.unset('_lstOpsRetry');",
                    *assert_status(200),
                    "pm.test('operations is array', () => pm.expect(pm.response.json().operations || []).to.be.an('array'));",
                ])],
))

# ListOperations garbage page_token → 400 INVALID_ARGUMENT (format-validate).
CASES.append(Case(
    id="REG-LSTOPS-NEG-BAD-TOKEN",  # index: REG-06
    title="ListOperations with garbage page_token → 400 INVALID_ARGUMENT",
    classes=["NEG", "VAL"], priority="P2",
    steps=[Step(name="list-ops-bad-token", method="GET",
                path=REG + "/{{regId}}/operations?pageToken=not-a-b64-token",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# ListOperations malformed registry id → 400 INVALID_ARGUMENT (scope-extractored →
# gateway валидирует id первым, как Get → "invalid resource id").
CASES.append(Case(
    id="REG-LSTOPS-NEG-MALFORMED-ID",  # index: REG-06
    title="ListOperations with malformed registry id → 400 INVALID_ARGUMENT (gateway id-validate)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[Step(name="list-ops-bad-id", method="GET", path=REG + "/not-an-id/operations",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('invalid id text', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('invalid'));"])],
))

# ListOperations pageSize > max (1000) → 400 (BVA: rejected not clamped).
CASES.append(Case(
    id="REG-LSTOPS-NEG-PAGESIZE-OVERMAX",  # index: REG-06
    title="ListOperations pageSize=1001 (> max 1000) → 400 INVALID_ARGUMENT (rejected not clamped, BVA)",
    classes=["NEG", "BVA", "VAL"], priority="P2",
    steps=[Step(name="list-ops-ps-overmax", method="GET",
                path=REG + "/{{regId}}/operations?pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))


# ===========================================================================
# DeleteTag (async mutation; data-plane push not available in control-plane newman)
# ===========================================================================

# DeleteTag on an absent tag — no pushed image, so either an idempotent async
# Operation (200) or existence-hidden 404. Assert oneOf; on 200 assert Op envelope.
CASES.append(Case(
    id="REG-DELTAG-IDEM-ABSENT",  # index: REG-25
    title="DeleteTag on absent tag → idempotent Operation (200) OR 404 NOT_FOUND (existence-hidden)",
    classes=["IDEM", "NEG"], priority="P2",
    steps=[Step(name="delete-tag-absent", method="DELETE",
                path=REG + "/{{regId}}/repositories/app-{{runId}}/tags/v1",
                test_script=[
                    "pm.test('deltag absent → 200 op or 404', () => pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
                    "const j = pm.response.json();",
                    "if (pm.response.code === 200) {",
                    "  pm.test('async Operation envelope', () => pm.expect((j.id||'')).to.match(/^(rop|reo)[a-z0-9]+$/));",
                    "} else {",
                    "  pm.test('grpc code 5 (NOT_FOUND)', () => pm.expect(j.code).to.eql(5));",
                    "  pm.test('no deny_reasons leak', () => pm.expect(JSON.stringify(j)).to.not.include('deny_reasons'));",
                    "}",
                ])],
))

# DeleteTag with malformed registry id → 400 INVALID_ARGUMENT. DeleteTag — ScopeFiltered
# (gateway authz-edge пропускает id-валидацию), поэтому malformed id ловит уже registry-side
# `validateRegistryID` → `corevalidate.ResourceID("registry",...)` → "invalid registry id '<X>'"
# (в отличие от Get, где id валидирует gateway → generic "invalid resource id").
CASES.append(Case(
    id="REG-DELTAG-NEG-MALFORMED-ID",  # index: REG-25
    title="DeleteTag with malformed registry id → 400 INVALID_ARGUMENT (\"invalid registry id\")",
    classes=["NEG", "VAL"], priority="P1",
    steps=[Step(name="delete-tag-bad-id", method="DELETE",
                path=REG + "/not-an-id/repositories/app/tags/v1",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('invalid id text', () => pm.expect((pm.response.json().message||'')).to.include('invalid registry id'));"])],
))


# ===========================================================================
# Delete
# ===========================================================================

# Delete happy — own throwaway registry (leaves shared {{regId}} intact):
# Operation → poll → Get 404.
CASES.append(Case(
    id="REG-DEL-CRUD-OK",
    title="Delete registry → Operation → poll → Get 404 NOT_FOUND",
    classes=["CRUD"], priority="P1",
    steps=[
        *_create_registry("del-me-{{runId}}", "delRegId"),
        Step(name="delete-del", method="DELETE", path=REG + "/{{delRegId}}",
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-del-deleted", method="GET", path=REG + "/{{delRegId}}",
             test_script=[
                 "pm.test('delete op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                 *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
             ]),
    ],
))

# Delete well-formed-but-absent registry → sync NOT_FOUND 404 (existence-hiding).
# RegistryService.Delete carries hide_existence=true (f06e01b, security.md #6): an
# authenticated deny — including a non-resolvable/absent target whose owner-tuple the
# gateway scope-Check cannot map to a project — surfaces as opaque NotFound(5)/404, NOT
# 403+deny_reasons and NOT an async Operation. Existence indistinguishable from a real
# miss; no relation-enumeration leak. (Parity with registry-authz delete-viewer.)
CASES.append(Case(
    id="REG-DEL-NEG-NOTFOUND",  # index: REG-07
    title="Delete well-formed-absent reg → 404 NOT_FOUND (hide-existence, no deny_reasons leak)",
    classes=["NEG", "AZ", "CONF"], priority="P1",
    steps=[
        Step(name="delete-nx", method="DELETE", path=REG + "/reg00000000000000000",
             test_script=[
                 *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('no deny_reasons/relation leak (existence-hiding)', () => pm.expect(JSON.stringify(pm.response.json())).to.not.match(/deny_reasons|direct relations|lacks relation/));",
             ]),
    ],
))

# Double-delete idempotency — own throwaway registry; first Delete OK, second Delete
# idempotent (200 op / 404 gone / 409 DELETING forward-only).
CASES.append(Case(
    id="REG-DEL-IDEM-DOUBLE",  # index: REG-09
    title="Double Delete → first OK, second idempotent (200/404/409)",
    classes=["IDEM"], priority="P2",
    steps=[
        *_create_registry("dd-{{runId}}", "ddRegId"),
        Step(name="delete-dd-1", method="DELETE", path=REG + "/{{ddRegId}}",
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="delete-dd-2", method="DELETE", path=REG + "/{{ddRegId}}",
             test_script=[
                 "pm.test('second delete idempotent (200/404/409)', () => pm.expect(pm.response.code).to.be.oneOf([200, 404, 409]));",
                 "pm.test('never 403 (deny→404 no-leak)', () => pm.expect(pm.response.code).to.not.eql(403));",
             ]),
    ],
))


# ===========================================================================
# Cleanup — remove the shared registry LAST (keeps the file idempotent).
# ===========================================================================

CASES.append(Case(
    id="REG-CLEANUP",  # index: REG-07
    title="Teardown — Delete shared {{regId}} → poll (tolerant to prior removal)",
    classes=["IDEM"], priority="P3",
    steps=_delete_registry("regId", tolerant=True),
))
