# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для REDESIGN-поверхности RegistryService (kacho-registry, REG-1).

Black-box через api-gateway REST (`/registry/v1/registries`). Локает
production-инкремент REG-1 поверх уже id-based `Registry` — три ортогональные
фичи + identity-контракт как acceptance-инвариант. Источник истины —
`docs/specs/sub-phase-REG-1-registry-repository-acceptance.md` (REG-1-01..32).

Покрытая redesign-поверхность (happy + negative на каждый новый/изменённый элемент):

  F1/F2 identity  — id immutable (в update_mask → INVALID_ARGUMENT, REG-1-04);
                    rename name НЕ меняет id/endpoint/pull-URL (REG-1-07);
                    field-absence globalSlug/displayName/top-level visibility (REG-1-02).
  F4 region       — regionId required (омит → INVALID_ARGUMENT, REG-1-11); несуществующий
                    → FAILED_PRECONDITION peer-validate geo (REG-1-12); placementType
                    always-REGIONAL + zoneId отсутствует (REG-1-10); regionId/placementType
                    immutable (REG-1-14).
  F5 visibility   — defaultRepositoryVisibility сид Repository (REG-1-15); →PUBLIC
                    admin-gate не-admin → PERMISSION_DENIED (REG-1-16).
  F6 repo-key     — registryId immutable у Repository (REG-1-19).
  F7 lifecycle    — DURABLE default (REG-1-21); EPHEMERAL opt-in (REG-1-22); output-only
                    (в update_mask → INVALID_ARGUMENT, REG-1-24).
  F8 hardening    — empty-mask full-PATCH: immutable из тела silently ignored (REG-1-28);
                    pageSize > max → INVALID_ARGUMENT (REG-1-31).

Test-design техники (testing-product-coach): ECP (regionId существует/нет/омит;
lifecycle DURABLE/EPHEMERAL/UNSPECIFIED), BVA (pageSize max+1), decision-table
(defaultRepositoryVisibility × admin-tier), state-invariant (id/endpoint стабильны
через rename name), error-guessing (immutable-поля в mask, output-only lifecycle в mask).

Дисциплина (testing.md): read-your-writes — первый Get/GetRepository своего свежего
ресурса обёрнут bounded-retry на 403/404 (owner-tuple EC); negatives НЕ обёрнуты.
regionId сидится kacho-deploy (`existingRegionId`, та же geo-фикстура, что nlb/compute);
UNIQUE(project,name) изолирован `-{{runId}}`-суффиксом; cleanup свой.

Не покрыто control-plane newman'ом (по конструкции — вынесено в integration, отмечено в
RESULTS «not black-box-testable»): REG-1-13 geo-down UNAVAILABLE (нельзя погасить geo
из black-box); REG-1-23 auto-promote register-on-first-push ephemeral (overlay-less repo
требует data-plane push); REG-1-25 concurrent lifecycle-CAS (concurrency → integration);
REG-1-30 INTERNAL-no-leak (симуляция DB-ошибки → integration); REG-1-20 ACTIVE-guard
DELETING (racy окно в black-box → integration).
"""

CASES = []

REG = "/registry/v1/registries"
# Registry-мутации несут op-id префикс rop/reo (opsproxy-роутинг api-gateway).
OP_ENVELOPE = "^(rop|reo)[a-z0-9]+$"


# ---------------------------------------------------------------------------
# Self-contained setup / cleanup helpers (idempotent, {{runId}}-isolated)
# ---------------------------------------------------------------------------

def _create_registry(name_expr, id_var, region="{{existingRegionId}}"):
    """Setup helper: Create REGIONAL registry (async Op) → poll → capture id + assert
    placement echo. Reads the created Registry from the Operation RESPONSE (EC-free —
    the op envelope is always readable, no owner-tuple lag)."""
    body = {"name": name_expr, "projectId": "{{existingProjectId}}",
            "regionId": region, "description": "REG-1 redesign coverage"}
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
                 "pm.test('setup op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                 "const r = (pm.response.json().response) || {};",
                 "pm.test('id prefix reg', () => pm.expect((r.id||'').startsWith('reg')).to.be.true);",
                 *save_from_response("(j.response&&j.response.id)||''", id_var),
             ]),
        # Read-your-writes warm-up: materialize the registry owner-tuple before the case's
        # first Update/CreateRepository under it. Update carries hide_existence + gateway
        # scope-Check; CreateRepository does registryGate(v_create) — both deny/404 until the
        # tuple is visible (register-outbox → drainer → IAM → FGA). Bounded-retry the
        # GetRegistry over that EC window (own fresh resource; never negatives).
        retry_until_authorized(
            Step(name="warm-" + id_var, method="GET", path=REG + "/{{" + id_var + "}}",
                 test_script=[*assert_status(200)])),
    ]


def _delete_registry(id_var):
    """Cleanup helper: Delete registry {{id_var}} (async Op) → poll; tolerant to prior removal."""
    return [
        Step(name="delete-" + id_var, method="DELETE", path=REG + "/{{" + id_var + "}}",
             test_script=[
                 "pm.test('delete accepted (200/404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
                 "const j = pm.response.json(); if (j && j.id) pm.environment.set('opId', String(j.id));",
             ]),
        poll_operation_until_done(),
    ]


def _get_self_with_retry(name, path, tests):
    """Get of OWN fresh resource with bounded retry on transient 403/404 (owner-tuple
    materialization is eventually-consistent, testing.md). Budget 20 × ~500ms busy-wait
    ≈ 10s covers the read-your-writes window; then the real assertions run (fail-open by
    budget). Wrap ONLY the first self-access — never negatives/cross-account."""
    return Step(name=name, method="GET", path=path, test_script=[
        "const _rc = parseInt(pm.environment.get('_rdRetry') || '0', 10);",
        "if ((pm.response.code === 403 || pm.response.code === 404) && _rc < 20) {",
        "  pm.environment.set('_rdRetry', String(_rc + 1));",
        "  const _pd = Date.now(); while (Date.now() - _pd < 500) { /* EC read-your-writes wait */ }",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_rdRetry');",
        *tests,
    ])


def _create_repo(reg_var, repo_expr, capture_tests, body_extra=None):
    """CreateRepository (async Op) → poll → capture. Asserts on the Operation RESPONSE
    (EC-free — the created Repository is carried in op.response, no repo owner-tuple lag).
    capture_tests run with `r` bound to op.response."""
    body = {"repository": repo_expr, "description": "redesign repo"}
    if body_extra:
        body.update(body_extra)
    return [
        Step(name="repo-create", method="POST", path=REG + "/{{" + reg_var + "}}/repositories",
             body=body,
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="repo-capture", method="GET", path="/operations/{{opId}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('createRepo op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                 "const r = (pm.response.json().response) || {};",
                 *capture_tests,
             ]),
        # Read-your-writes warm-up: materialize the per-repo owner-tuple
        # (registry_repository:<reg>/<repo>) before a follow-up UpdateRepository negative.
        # UpdateRepository's handler runs the per-repo v_update Check BEFORE the use-case
        # immutable/output-only reject (handler.UpdateRepository → uc.UpdateRepository), so
        # during the EC window a would-be sync-400 (lifecycle/registryId immutable) returns
        # 404. A v_get warm-up covers the whole verb-set (atomic per-object FGA Write —
        # data-integrity.md), making the later negative deterministic. Own fresh repo only.
        retry_until_authorized(
            Step(name="repo-warm", method="GET",
                 path=REG + "/{{" + reg_var + "}}/repositories/" + repo_expr,
                 test_script=[*assert_status(200)])),
    ]


# ===========================================================================
# Shared setup — one REGIONAL registry for the read/identity/visibility cases.
# ===========================================================================

CASES.append(Case(
    id="REG-RD-SETUP",
    title="Setup: create shared REGIONAL registry {{rdRegId}} (regionId, REG-1 F4)",
    classes=["CRUD"], priority="P0",
    steps=_create_registry("redesign-{{runId}}", "rdRegId"),
))


# ===========================================================================
# F4 — regionId (peer-validate geo) + placementType always-REGIONAL
# ===========================================================================

# REG-1-01/10 (happy): Get shared reg → regionId echoed, placementType REGIONAL const,
# zoneId absent (regional-anycast). First self-access → authz-retry (owner-tuple EC).
CASES.append(Case(
    id="REG-RD-F4-PLACEMENT-REGIONAL",  # verifies REG-1-01, REG-1-10
    title="Get REGIONAL registry → regionId echoed, placementType REGIONAL, zoneId absent (anycast)",
    classes=["CRUD", "CONF"], priority="P0",
    steps=[_get_self_with_retry("get-placement", REG + "/{{rdRegId}}", [
        *assert_status(200),
        "const j = pm.response.json();",
        "pm.test('regionId echoes existingRegionId', () => pm.expect(j.regionId).to.eql(pm.environment.get('existingRegionId')));",
        "pm.test('placementType REGIONAL (const, not a choice)', () => pm.expect(j.placementType).to.eql('REGIONAL'));",
        "pm.test('zoneId absent (regional-anycast, zone-independent)', () => pm.expect(j.zoneId === undefined || j.zoneId === '').to.be.true);",
    ])],
))

# REG-1-11 (negative): regionId омитнут на Create → sync 400 INVALID_ARGUMENT
# "regionId is required" (own-field validation первым стейтментом; op не создаётся).
CASES.append(Case(
    id="REG-RD-F4-NEG-REGION-REQUIRED",  # verifies REG-1-11
    title="Create without regionId → 400 INVALID_ARGUMENT (\"regionId is required\"), no Operation",
    classes=["NEG", "VAL"], priority="P0",
    steps=[Step(name="create-noregion", method="POST", path=REG,
                body={"name": "noregion-{{runId}}", "projectId": "{{existingProjectId}}",
                      "description": "missing region"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('regionId required text', () => pm.expect((pm.response.json().message||'')).to.include('regionId is required'));"])],
))

# REG-1-12 (negative): несуществующий regionId → peer-validate geo (RegionService.Get)
# → FAILED_PRECONDITION (code 9). by-lane: foreign id не существует у владельца → НЕ
# NOT_FOUND (consumer не «не нашёл своё», а «предусловие на чужой ресурс не выполнено»).
# grpc-gateway маппит FAILED_PRECONDITION → HTTP 400 (толерантно к версии gateway).
CASES.append(Case(
    id="REG-RD-F4-NEG-REGION-NOTFOUND",  # verifies REG-1-12
    title="Create with nonexistent regionId → FAILED_PRECONDITION (code 9) peer-validate geo",
    classes=["NEG"], priority="P1",
    steps=[Step(name="create-badregion", method="POST", path=REG,
                body={"name": "badregion-{{runId}}", "projectId": "{{existingProjectId}}",
                      "regionId": "{{garbageRegionId}}", "description": "dangling region"},
                test_script=[
                    "pm.test('rejected 4xx (FAILED_PRECONDITION → 400)', () => pm.expect(pm.response.code).to.be.oneOf([400, 409, 412, 422]));",
                    "pm.test('grpc code 9 (FAILED_PRECONDITION, peer-validate lane)', () => pm.expect(pm.response.json().code).to.eql(9));",
                    "pm.test('region not found text', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('region').and.to.include('not found'));",
                ])],
))

# REG-1-14 (negative): regionId / placementType immutable после Create → sync 400
# "regionId is immutable after Registry.Create" / "placementType is immutable ..."
# (immutable-switch ДО corevalidate.UpdateMask; перенос региона сломал бы storage-locality).
CASES.append(Case(
    id="REG-RD-F4-NEG-REGION-IMMUTABLE",  # verifies REG-1-14
    title="Update updateMask=regionId / placementType → 400 INVALID_ARGUMENT (immutable placement anchor)",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        Step(name="update-region", method="PATCH", path=REG + "/{{rdRegId}}",
             body={"updateMask": "regionId", "regionId": "{{existingRegionAltId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('regionId immutable text', () => pm.expect(pm.response.json().message).to.eql('regionId is immutable after Registry.Create'));"]),
        Step(name="update-placement", method="PATCH", path=REG + "/{{rdRegId}}",
             body={"updateMask": "placementType", "placementType": "REGIONAL"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('placementType immutable text', () => pm.expect(pm.response.json().message).to.eql('placementType is immutable after Registry.Create'));"]),
    ],
))


# ===========================================================================
# F1 / F2 — identity: id immutable, URL/pull by id, field-absence, rename-safe
# ===========================================================================

# REG-1-02: field-absence — no globalSlug (ban #15), no displayName, no top-level
# visibility (authoritative gate is per-repo Repository.visibility), no infra-полей
# (two-projection, live only in Internal* :9091). Locks that these fields never
# reappear on the public surface.
CASES.append(Case(
    id="REG-RD-F1-FIELD-ABSENCE",  # verifies REG-1-02
    title="Get registry → body has NO globalSlug/displayName/top-level visibility/infra fields",
    classes=["CONF"], priority="P1",
    steps=[_get_self_with_retry("get-absence", REG + "/{{rdRegId}}", [
        *assert_status(200),
        "const j = pm.response.json();",
        "pm.test('no globalSlug (ban #15: URL carries immutable id, not a slug)', () => pm.expect(j).to.not.have.property('globalSlug'));",
        "pm.test('no displayName (UI pretty-name lives in labels)', () => pm.expect(j).to.not.have.property('displayName'));",
        "pm.test('no top-level visibility (authoritative gate is per-repo)', () => pm.expect(j).to.not.have.property('visibility'));",
        "pm.test('no infra engineNamespace (two-projection, Internal* only)', () => pm.expect(j).to.not.have.property('engineNamespace'));",
        "pm.test('no infra bucketPrefix (two-projection)', () => pm.expect(j).to.not.have.property('bucketPrefix'));",
        "pm.test('no infra numericInfraId (two-projection)', () => pm.expect(j).to.not.have.property('numericInfraId'));",
    ])],
))

# REG-1-04 (negative): id immutable — операции смены нет. (b) id в update_mask → sync
# 400 "id is immutable after Registry.Create". (a) :rename-verb на Registry НЕ
# зарегистрирован → маршрут не резолвится (толерантно к gateway 400/404/405/501).
CASES.append(Case(
    id="REG-RD-F1-NEG-ID-IMMUTABLE",  # verifies REG-1-04
    title="Update updateMask=id → 400 (id immutable); POST :rename → route absent (no id-rename)",
    classes=["NEG", "CONF"], priority="P0",
    steps=[
        Step(name="update-id", method="PATCH", path=REG + "/{{rdRegId}}",
             body={"updateMask": "id", "id": "reg00000000hacked00"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('id immutable text', () => pm.expect(pm.response.json().message).to.eql('id is immutable after Registry.Create'));"]),
        Step(name="post-rename-absent", method="POST", path=REG + "/{{rdRegId}}:rename",
             body={"name": "renamed-{{runId}}"},
             test_script=[
                 # No :rename verb exists on RegistryService (id is immutable, ban #15). The
                 # path resolves to no RPC — grpc-gateway returns a routing 404 (or 405/501);
                 # a stale/variant router may 403 (authz-gate a wrong-method match). Any of
                 # these confirms the invariant: id-rename is NEVER a successful (200) op.
                 "pm.test('no :rename verb on Registry (never 200 success)', () => pm.expect(pm.response.code).to.be.oneOf([400, 403, 404, 405, 501]));",
             ]),
    ],
))

# REG-1-06/07 (key happy): rename name via Update НЕ меняет id/endpoint (derived by id).
# Own throwaway registry → self-contained. endpoint° = <host>/<id> — стабилен через rename.
CASES.append(Case(
    id="REG-RD-F2-RENAME-STABLE-ID",  # verifies REG-1-06, REG-1-07
    title="Rename name via Update → name changes, id/endpoint unchanged (URL/pull addressed by immutable id)",
    classes=["CRUD", "CONF"], priority="P0",
    steps=[
        *_create_registry("rename-old-{{runId}}", "rnRegId"),
        _get_self_with_retry("get-before-rename", REG + "/{{rnRegId}}", [
            *assert_status(200),
            "const j = pm.response.json();",
            "pm.environment.set('rnEndpointBefore', j.endpoint||'');",
            "pm.test('endpoint derived by id (contains id)', () => pm.expect(j.endpoint||'').to.include(pm.environment.get('rnRegId')));",
        ]),
        Step(name="rename", method="PATCH", path=REG + "/{{rnRegId}}",
             body={"updateMask": "name", "name": "rename-new-{{runId}}"},
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-after-rename", method="GET", path=REG + "/{{rnRegId}}",
             test_script=[
                 "pm.test('rename op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('name mutated', () => pm.expect(j.name).to.eql('rename-new-'+pm.environment.get('runId')));",
                 "pm.test('id unchanged (identity anchor stable)', () => pm.expect(j.id).to.eql(pm.environment.get('rnRegId')));",
                 "pm.test('endpoint unchanged (derived by id, not name)', () => pm.expect(j.endpoint||'').to.eql(pm.environment.get('rnEndpointBefore')));",
             ]),
        *_delete_registry("rnRegId"),
    ],
))


# ===========================================================================
# F5 — defaultRepositoryVisibility (rename default_visibility) + admin-gate
# ===========================================================================

# REG-1-15 (happy): registry defaultRepositoryVisibility==PRIVATE (fail-safe default);
# CreateRepository без явного visibility → repo наследует visibility==PRIVATE.
CASES.append(Case(
    id="REG-RD-F5-INHERIT-PRIVATE",  # verifies REG-1-15
    title="defaultRepositoryVisibility PRIVATE seeds new Repository.visibility when visibility omitted",
    classes=["CRUD"], priority="P1",
    steps=[
        _get_self_with_retry("get-default-vis", REG + "/{{rdRegId}}", [
            *assert_status(200),
            "pm.test('defaultRepositoryVisibility PRIVATE (fail-safe default)', () => pm.expect(pm.response.json().defaultRepositoryVisibility).to.eql('PRIVATE'));",
        ]),
        *_create_repo("rdRegId", "inherit/vis-{{runId}}", [
            "pm.test('new repo inherits visibility PRIVATE', () => pm.expect(r.visibility).to.eql('PRIVATE'));",
        ]),
    ],
))

# REG-1-16 (negative + positive-control): caller has v_update but NOT registry admin
# (project-editor materializes v_* but not `admin` relation — flat Contract-A). Update
# defaultRepositoryVisibility→PUBLIC → sync 403 PERMISSION_DENIED (any-path-to-PUBLIC
# admin-gate, D-6; НЕ existence-hiding — caller уже доказал v_update). Тот же caller с
# updateMask=description (без visibility) → Operation OK (editor-путь узко не сломан).
CASES.append(Case(
    id="REG-RD-F5-NEG-PUBLIC-ADMIN-GATE",  # verifies REG-1-16
    title="Non-admin drives defaultRepositoryVisibility→PUBLIC → 403 PERMISSION_DENIED; description-only Update → OK",
    classes=["NEG", "AZ"], priority="P0",
    steps=[
        Step(name="update-default-public", method="PATCH", path=REG + "/{{rdRegId}}",
             body={"updateMask": "defaultRepositoryVisibility", "defaultRepositoryVisibility": "PUBLIC"},
             test_script=[
                 *assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED"),
                 "pm.test('names required capability (registry admin)', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('registry admin'));",
             ]),
        Step(name="update-description-ok", method="PATCH", path=REG + "/{{rdRegId}}",
             body={"updateMask": "description", "description": "editor-path-{{runId}}"},
             test_script=[
                 *assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                 "pm.test('editor description-path not broken by narrow admin-gate', () => pm.expect(pm.response.json().id||'').to.match(/^(rop|reo)[a-z0-9]+$/));",
             ]),
    ],
))


# ===========================================================================
# F7 — Repository.lifecycle output-only enum {DURABLE, EPHEMERAL}
# ===========================================================================

# REG-1-21 (happy): explicit CreateRepository без lifecycle → DURABLE by default
# (explicit intent-create = сохранить каркас, survives-empty); tagCount 0.
CASES.append(Case(
    id="REG-RD-F7-CREATE-DURABLE",  # verifies REG-1-21
    title="CreateRepository (no lifecycle) → lifecycle DURABLE by default (survives-empty), tagCount 0",
    classes=["CRUD"], priority="P0",
    steps=_create_repo("rdRegId", "durable/svc-{{runId}}", [
        "pm.test('lifecycle DURABLE (explicit intent-create default)', () => pm.expect(r.lifecycle).to.eql('DURABLE'));",
        "pm.test('tagCount 0 (durable-empty survives)', () => pm.expect(Number(r.tagCount||0)).to.eql(0));",
    ]),
))

# REG-1-22 (happy/variant): explicit CreateRepository lifecycle=EPHEMERAL → EPHEMERAL
# (register-on-first-push semantics — предсказуемый эксплицитный рычаг).
CASES.append(Case(
    id="REG-RD-F7-CREATE-EPHEMERAL",  # verifies REG-1-22
    title="CreateRepository lifecycle=EPHEMERAL → lifecycle EPHEMERAL (opt-in overrides DURABLE default)",
    classes=["CRUD"], priority="P1",
    steps=_create_repo("rdRegId", "ephemeral/svc-{{runId}}", [
        "pm.test('lifecycle EPHEMERAL (explicit opt-in)', () => pm.expect(r.lifecycle).to.eql('EPHEMERAL'));",
    ], body_extra={"lifecycle": "EPHEMERAL"}),
))

# REG-1-24 (negative): lifecycle output-only — в UpdateRepository.update_mask → sync 400
# (система авторитетно управляет lifecycle; тот же класс, что tagCount/createdAt).
# Требует существующий durable repo (per-repo v_update Check ДО use-case immutable-reject).
CASES.append(Case(
    id="REG-RD-F7-NEG-LIFECYCLE-READONLY",  # verifies REG-1-24
    title="UpdateRepository updateMask=lifecycle → 400 INVALID_ARGUMENT (lifecycle output-only, system-managed)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[
        *_create_repo("rdRegId", "lifemask/svc-{{runId}}", [
            "pm.test('lifecycle DURABLE (setup)', () => pm.expect(r.lifecycle).to.eql('DURABLE'));",
        ]),
        Step(name="update-lifecycle-mask", method="PATCH",
             path=REG + "/{{rdRegId}}/repositories/lifemask/svc-{{runId}}",
             body={"updateMask": "lifecycle", "lifecycle": "EPHEMERAL"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('lifecycle read-only text', () => pm.expect(pm.response.json().message).to.eql('lifecycle is read-only (system-managed)'));"]),
    ],
))


# ===========================================================================
# F6 — Repository natural-key (registryId, name): registryId immutable
# ===========================================================================

# REG-1-19 (negative): registryId immutable у Repository → UpdateRepository
# updateMask=registryId → sync 400 (cross-registry move структурно невыразим). Требует
# существующий repo (per-repo v_update Check ДО use-case immutable-reject).
CASES.append(Case(
    id="REG-RD-F6-NEG-REGISTRYID-IMMUTABLE",  # verifies REG-1-19
    title="UpdateRepository updateMask=registryId → 400 INVALID_ARGUMENT (registryId immutable, natural key)",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        *_create_repo("rdRegId", "regidmask/svc-{{runId}}", [
            "pm.test('repo created (setup)', () => pm.expect(r.name||'').to.include('regidmask/svc-'));",
        ]),
        Step(name="update-registryid-mask", method="PATCH",
             path=REG + "/{{rdRegId}}/repositories/regidmask/svc-{{runId}}",
             body={"updateMask": "registryId", "description": "move attempt"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('registryId immutable text', () => pm.expect(pm.response.json().message).to.eql('registryId is immutable after Repository.Create'));"]),
    ],
))


# ===========================================================================
# F8 — Update-дисциплина + hardening
# ===========================================================================

# REG-1-28 (lock): пустой update_mask → full-object PATCH mutable; immutable из тела
# (id/regionId/placementType) silently игнорируются. Own throwaway → self-contained.
CASES.append(Case(
    id="REG-RD-F8-EMPTY-MASK-IMMUTABLE-IGNORED",  # verifies REG-1-28
    title="Empty updateMask → mutable applied (description/name), immutable (id/regionId) in body silently ignored",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        *_create_registry("emptymask-{{runId}}", "emRegId"),
        Step(name="update-emptymask", method="PATCH", path=REG + "/{{emRegId}}",
             body={"description": "patched-{{runId}}", "labels": {"team": "pay"},
                   "name": "emptymask-new-{{runId}}",
                   "regionId": "{{garbageRegionId}}", "id": "reg00000000hacked00"},
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-emptymask", method="GET", path=REG + "/{{emRegId}}",
             test_script=[
                 "pm.test('empty-mask op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('mutable description applied', () => pm.expect(j.description).to.eql('patched-'+pm.environment.get('runId')));",
                 "pm.test('mutable name applied (F2 cosmetic label)', () => pm.expect(j.name).to.eql('emptymask-new-'+pm.environment.get('runId')));",
                 "pm.test('immutable id silently ignored (unchanged)', () => pm.expect(j.id).to.eql(pm.environment.get('emRegId')));",
                 "pm.test('immutable regionId silently ignored (unchanged)', () => pm.expect(j.regionId).to.eql(pm.environment.get('existingRegionId')));",
             ]),
        *_delete_registry("emRegId"),
    ],
))

# REG-1-31 (negative): pageSize > max (1000) → INVALID_ARGUMENT (отвергается, НЕ
# clamp'ится) — format-validate ДО listauthz empty-grant short-circuit (security.md #7).
CASES.append(Case(
    id="REG-RD-F8-NEG-PAGESIZE-OVERMAX",  # verifies REG-1-31
    title="List pageSize=1001 (> max 1000) → 400 INVALID_ARGUMENT (rejected not clamped, format-validate before authz)",
    classes=["NEG", "BVA", "VAL"], priority="P1",
    steps=[Step(name="list-pagesize-overmax", method="GET",
                path=REG + "?projectId={{existingProjectId}}&pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))


# ===========================================================================
# Cleanup — remove the shared redesign registry LAST (keeps the module idempotent).
# ===========================================================================

CASES.append(Case(
    id="REG-RD-CLEANUP",
    title="Teardown — Delete shared {{rdRegId}} → poll (tolerant to prior removal)",
    classes=["IDEM"], priority="P3",
    steps=_delete_registry("rdRegId"),
))
