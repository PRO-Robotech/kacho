# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для config-overlay Repository RPC (kacho-registry, RG-1).

Black-box через api-gateway REST — покрывает 6 новых RPC RegistryService
(GetRepository/ListReferrers sync; CreateRepository/UpdateRepository/
DeleteRepository/RenameRepository → Operation), ≥1 happy + ≥1 negative на RPC.

  CreateRepository  — happy(durable-empty, PRIVATE, tagCount=0) · bad-name(400)
  GetRepository     — happy(durable-empty) · absent(404 "repository not found")
  UpdateRepository  — happy(description) · immutable name in mask(400)
  DeleteRepository  — happy(empty→done) · absent(404 existence-hiding)
  RenameRepository  — happy(old→new) · no-op new_name==current(400)
  ListReferrers     — happy(empty→[] 200) · malformed subject_digest(400)

REST-контракт (repository содержит `/` → wildcard-сегмент):
  POST   /registry/v1/registries/{regId}/repositories                 (body)
  GET    /registry/v1/registries/{regId}/repositories/{repo}
  PATCH  /registry/v1/registries/{regId}/repositories/{repo}          (body)
  DELETE /registry/v1/registries/{regId}/repositories/{repo}
  POST   /registry/v1/registries/{regId}/repositories/{repo}:rename   (body)
  GET    /registry/v1/registries/{regId}/repositories/{repo}/referrers?subjectDigest=…

Мутации async (Operation prefix `rop`/`reo`, поллятся через /operations/{opId}).
Само-достаточность: shared setup (`REPO-SETUP`) создаёт общий registry {{repoRegId}};
кейсы работают над ним; финальный `REPO-CLEANUP` сносит его. Изоляция — `-{{runId}}`.

Прим.: исполнение требует зарегистрированных в api-gateway public RPC (отдельный
api-gateway-registrar-срез) — до его merge кейсы генерируются, но зелёными станут
после регистрации маршрутов (Tests трассируются к acceptance RG-1-<Group><NN>).
"""

CASES = []

REG = "/registry/v1/registries"
OP_ENVELOPE = "^(rop|reo)[a-z0-9]+$"


def _reg_base():
    return REG + "/{{repoRegId}}/repositories"


def _create_registry(name_expr, id_var):
    """Setup: Create registry (async Op) → poll → capture id into id_var."""
    # regionId обязателен на Create (REG-1 F4, peer-validate geo) — иначе sync 400.
    body = {"name": name_expr, "projectId": "{{existingProjectId}}",
            "regionId": "{{existingRegionId}}", "description": "RG-1 overlay CI"}
    return [
        Step(name="reg-create", method="POST", path=REG, body=body,
             test_script=[*assert_status(200), *assert_operation_envelope("^(rop|reo)[a-z0-9]+$"),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="reg-capture", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "const r=(pm.response.json().response)||{};",
                          *save_from_response("(j.response&&j.response.id)||''", id_var)]),
        # Read-your-writes warm-up: materialize the registry owner-tuple before any
        # CreateRepository under it. CreateRepository's handler does registryGate(v_create)
        # on the parent registry (repository.go) — the FIRST repo-create under a fresh
        # registry denies/404s until that tuple is visible (register-outbox → drainer →
        # IAM → FGA). Bounded-retry the GetRegistry over the EC window (own fresh resource).
        retry_until_authorized(
            Step(name="reg-warm", method="GET", path=REG + "/{{" + id_var + "}}",
                 test_script=[*assert_status(200)])),
    ]


def _create_repo(repo_expr, extra_asserts=None, body_extra=None):
    """Create repository (async Op) → poll → capture done. Returns list[Step]."""
    body = {"repository": repo_expr, "description": "api service images", "labels": {"team": "core"}}
    if body_extra:
        body.update(body_extra)
    cap = [
        "const r=(pm.response.json().response)||{};",
        "pm.test('op ok', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
    ]
    if extra_asserts:
        cap += extra_asserts
    return [
        Step(name="repo-create", method="POST", path=_reg_base(), body=body,
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="repo-capture", method="GET", path="/operations/{{opId}}", test_script=[*assert_status(200), *cap]),
        # Read-your-writes warm-up: force the per-repo owner-tuple
        # (registry_repository:<reg>/<repo>) to materialize — register-outbox → drainer →
        # IAM RegisterResource → FGA reconciler — before the case's own first repo access.
        # ALL repo RPCs run a per-repo v_* Check IN THE HANDLER (existence-hiding →
        # NOT_FOUND on deny/absent) BEFORE the use-case immutable/mask validation
        # (handler.UpdateRepository: checkRepository → uc.UpdateRepository), so during the
        # EC window even a would-be sync-400 negative (immutable/unknown-mask) returns 404.
        # A single v_get warm-up covers the whole verb-set (per-object FGA Write is atomic
        # all-or-nothing — data-integrity.md), making every later positive read AND negative
        # validation on this repo deterministic. Bounded-retry over own fresh resource only.
        retry_until_authorized(
            Step(name="repo-warm", method="GET", path=_reg_base() + "/" + repo_expr,
                 test_script=[*assert_status(200)])),
    ]


# --- shared setup ----------------------------------------------------------
CASES.append(Case(
    id="REPO-SETUP", title="Setup: create shared registry for overlay Repository cases",
    classes=["CRUD"], priority="P0",
    steps=_create_registry("overlay-reg-{{runId}}", "repoRegId"),
))

# --- CreateRepository ------------------------------------------------------
CASES.append(Case(
    id="REPO-CR-OK", title="CreateRepository durable-empty → Operation → durable, PRIVATE, tagCount=0 (A01)",
    classes=["CRUD"], priority="P0",
    steps=[
        *_create_repo("backend/api-{{runId}}", extra_asserts=[
            "pm.test('visibility PRIVATE (inherited)', () => pm.expect(r.visibility).to.eql('PRIVATE'));",
            "pm.test('tagCount 0 (survives-empty)', () => pm.expect(Number(r.tagCount||0)).to.eql(0));",
            "pm.test('createdAt set', () => pm.expect(r.createdAt||'').to.not.eql(''));",
        ]),
    ],
))

CASES.append(Case(
    id="REPO-CR-NEG-BADNAME", title="CreateRepository malformed name → 400 INVALID_ARGUMENT (A05)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[Step(name="create-bad", method="POST", path=_reg_base(),
                body={"repository": "Bad Name!"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('invalid name text', () => pm.expect((pm.response.json().message||'')).to.include('invalid repository name'));"])],
))

# --- GetRepository ---------------------------------------------------------
CASES.append(Case(
    id="REPO-GET-OK", title="GetRepository durable-empty → 200 (overlay projection, A07)",
    classes=["CRUD"], priority="P0",
    steps=[
        *_create_repo("get/svc-{{runId}}"),
        Step(name="get-repo", method="GET", path=_reg_base() + "/get/svc-{{runId}}",
             test_script=[*assert_status(200),
                          "const j=pm.response.json();",
                          "pm.test('name matches', () => pm.expect(j.name).to.eql('get/svc-'+pm.environment.get('runId')));",
                          "pm.test('visibility PRIVATE', () => pm.expect(j.visibility).to.eql('PRIVATE'));"]),
    ],
))

CASES.append(Case(
    id="REPO-GET-NEG-ABSENT", title="GetRepository absent → 404 \"repository not found\" (existence-hiding, A08)",
    classes=["NEG"], priority="P1",
    steps=[Step(name="get-absent", method="GET", path=_reg_base() + "/ghost/svc-{{runId}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             "pm.test('repository not found text', () => pm.expect(pm.response.json().message).to.eql('repository not found'));"])],
))

# --- UpdateRepository ------------------------------------------------------
CASES.append(Case(
    id="REPO-UPD-OK", title="UpdateRepository description/labels → Operation → Get reflects (A09)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_create_repo("upd/svc-{{runId}}"),
        Step(name="update-repo", method="PATCH", path=_reg_base() + "/upd/svc-{{runId}}",
             body={"description": "api images v2", "labels": {"team": "core", "tier": "gold"},
                   "updateMask": "description,labels"},
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="update-verify", method="GET", path=_reg_base() + "/upd/svc-{{runId}}",
             test_script=[*assert_status(200),
                          "pm.test('description updated', () => pm.expect(pm.response.json().description).to.eql('api images v2'));"]),
    ],
))

CASES.append(Case(
    id="REPO-UPD-NEG-IMMUTABLE", title="UpdateRepository name in mask → 400 immutable text (A11)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[
        *_create_repo("imm/svc-{{runId}}"),
        Step(name="update-immutable", method="PATCH", path=_reg_base() + "/imm/svc-{{runId}}",
             body={"description": "x", "updateMask": "name"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('name immutable text', () => pm.expect(pm.response.json().message).to.eql('name is immutable after Repository.Create'));"]),
    ],
))

# --- DeleteRepository ------------------------------------------------------
CASES.append(Case(
    id="REPO-DEL-OK", title="DeleteRepository empty durable → Operation done → Get 404 (A13)",
    classes=["CRUD", "IDEM"], priority="P1",
    steps=[
        *_create_repo("del/svc-{{runId}}"),
        Step(name="delete-repo", method="DELETE", path=_reg_base() + "/del/svc-{{runId}}",
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="delete-verify", method="GET", path=_reg_base() + "/del/svc-{{runId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

CASES.append(Case(
    id="REPO-DEL-NEG-ABSENT", title="DeleteRepository absent → 404 (existence-hiding, A15)",
    classes=["NEG"], priority="P1",
    steps=[Step(name="delete-absent", method="DELETE", path=_reg_base() + "/nope/svc-{{runId}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             "pm.test('repository not found text', () => pm.expect(pm.response.json().message).to.eql('repository not found'));"])],
))

# --- RenameRepository ------------------------------------------------------
CASES.append(Case(
    id="REPO-REN-OK", title="RenameRepository durable old→new → Get(new) 200, Get(old) 404 (A16)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_create_repo("ren/old-{{runId}}"),
        Step(name="rename-repo", method="POST", path=_reg_base() + "/ren/old-{{runId}}:rename",
             body={"newName": "ren/new-{{runId}}"},
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="rename-verify-new", method="GET", path=_reg_base() + "/ren/new-{{runId}}",
             test_script=[*assert_status(200),
                          "pm.test('new name', () => pm.expect(pm.response.json().name).to.eql('ren/new-'+pm.environment.get('runId')));"]),
        Step(name="rename-verify-old", method="GET", path=_reg_base() + "/ren/old-{{runId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

CASES.append(Case(
    id="REPO-REN-NEG-NOOP", title="RenameRepository new_name==current → 400 (A19)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[
        *_create_repo("noop/svc-{{runId}}"),
        Step(name="rename-noop", method="POST", path=_reg_base() + "/noop/svc-{{runId}}:rename",
             body={"newName": "noop/svc-{{runId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('differ text', () => pm.expect(pm.response.json().message).to.eql('new name must differ from current name'));"]),
    ],
))

# --- ListReferrers ---------------------------------------------------------
CASES.append(Case(
    id="REPO-REF-EMPTY", title="ListReferrers subject без referrer'ов → [] 200 (C03)",
    classes=["CRUD"], priority="P2",
    steps=[
        *_create_repo("ref/svc-{{runId}}"),
        Step(name="referrers-empty", method="GET",
             path=_reg_base() + "/ref/svc-{{runId}}/referrers?subjectDigest=sha256:" + ("e" * 64),
             test_script=[*assert_status(200),
                          "pm.test('empty referrers []', () => pm.expect((pm.response.json().referrers||[]).length).to.eql(0));"]),
    ],
))

CASES.append(Case(
    id="REPO-REF-NEG-BADDIGEST", title="ListReferrers malformed subject_digest → 400 (C04)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[
        *_create_repo("refbad/svc-{{runId}}"),
        Step(name="referrers-bad", method="GET",
             path=_reg_base() + "/refbad/svc-{{runId}}/referrers?subjectDigest=not-a-digest",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('invalid digest text', () => pm.expect(pm.response.json().message).to.include('invalid subject digest'));"]),
    ],
))

# ===========================================================================
# PARITY DOBOR (negatives + edge, iam/vpc-уровень) — доводит overlay-suite до
# полноты поверх happy-каркаса выше. Источник истины — RG-1 overlay acceptance
# (`sub-phase-RG-1-registry-repository-overlay-acceptance.md`, A02/A05/A06/A10/
# A17/A19/C02/X01, D-5). Техники (testing-product-coach): ECP (empty-vs-malformed
# name; existing-vs-absent target), BVA (pageSize — в registry.py), decision-table
# (dup overlay × async/sync), error-guessing (malformed registryId первым стейтментом,
# rename в занятое имя, cross-registry smuggle), CONF (uniform existence-hiding text,
# two-projection no-infra-leak). Self-contained на shared {{repoRegId}}; `-{{runId}}`.
# ===========================================================================

_BADREG = REG + "/not-an-id/repositories"  # malformed registryId для A06


# --- CreateRepository: duplicate + empty-name (A02, A05a) -------------------

# A02 (ECP: имя уже занято overlay-строкой): повторный CreateRepository того же
# (registryId,name) → ALREADY_EXISTS "repository already exists" (DB UNIQUE(registry_id,
# name), ban #10 — не software TOCTOU). Толерантен к sync-409 И async-op-error пути
# (INSERT-race в worker'е), как REG-CR-CONF-ALREADY-EXISTS у Registry.
CASES.append(Case(
    id="REPO-CR-NEG-DUP",  # verifies RG-1-A02
    title="CreateRepository duplicate (registryId,name) → 409 ALREADY_EXISTS \"repository already exists\" (A02, DB UNIQUE)",
    classes=["NEG", "CONF", "IDEM"], priority="P1",
    steps=[
        *_create_repo("dup/svc-{{runId}}"),
        Step(name="create-dup", method="POST", path=_reg_base(),
             body={"repository": "dup/svc-{{runId}}", "description": "dup attempt"},
             test_script=[
                 "pm.test('dup rejected (409 sync or 200 async-error)', () => pm.expect(pm.response.code).to.be.oneOf([200, 409]));",
                 "const j = pm.response.json();",
                 "if (pm.response.code === 409) {",
                 "  pm.test('grpc code 6 (ALREADY_EXISTS)', () => pm.expect(j.code).to.eql(6));",
                 "  pm.test('repository already exists text', () => pm.expect((j.message||'').toLowerCase()).to.include('repository already exists'));",
                 "  pm.environment.set('repoDupSync', '1');",
                 "} else {",
                 "  pm.environment.unset('repoDupSync');",
                 "  if (j.id) pm.environment.set('opId', String(j.id));",
                 "}",
             ]),
        poll_operation_until_done(),
        Step(name="assert-dup-async", method="GET", path="/operations/{{opId}}",
             test_script=[
                 "if (pm.environment.get('repoDupSync') === '1') {",
                 "  pm.test('dup handled synchronously (409)', () => pm.expect(true).to.eql(true));",
                 "} else {",
                 "  const j = pm.response.json();",
                 "  pm.test('async op done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                 "  const blob = (JSON.stringify(j.error||{}) + (pm.environment.get('lastOpError')||'')).toLowerCase();",
                 "  pm.test('async op errored ALREADY_EXISTS', () => pm.expect(blob).to.include('exist'));",
                 "}",
             ]),
    ],
))

# A05a (ECP: пустое имя — отдельный класс от malformed "Bad Name!" в REPO-CR-NEG-BADNAME):
# repository="" → sync 400 "repository is required" (первым стейтментом, до repo-вызова).
CASES.append(Case(
    id="REPO-CR-NEG-EMPTY-NAME",  # verifies RG-1-A05
    title="CreateRepository empty repository → 400 INVALID_ARGUMENT \"repository is required\" (A05, ECP empty)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[Step(name="create-empty-name", method="POST", path=_reg_base(),
                body={"repository": "", "description": "no name"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('repository required text', () => pm.expect((pm.response.json().message||'')).to.include('repository is required'));"])],
))


# --- A06: malformed registryId ПЕРВЫМ стейтментом на ВСЕХ repo-RPC ----------

# A06 (error-guessing + CONF): registryId, не проходящий prefix `reg`, отвергается
# sync INVALID_ARGUMENT "invalid registry id '<X>'" ПЕРВЫМ стейтментом RPC — до
# authz-Check/repo-вызова/Operation. Repository-RPC gateway-`<exempt>` (composite-key
# scope не выразим scope_extractor'ом) → id валидирует registry-side handler
# (`ValidateRegistryID`), поэтому текст — "invalid registry id" (как DeleteTag), НЕ
# generic gateway "invalid resource id". Один класс эквивалентности (malformed own-id)
# × 6 RPC-поверхностей — единый контракт (acceptance A06 «любой из ...»).
CASES.append(Case(
    id="REPO-NEG-BAD-REGID",  # verifies RG-1-A06
    title="Repository RPCs with malformed registryId → 400 \"invalid registry id\" first statement (A06, all 6 RPC)",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        Step(name="get-bad-regid", method="GET", path=_BADREG + "/x/y",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('GetRepository invalid registry id', () => pm.expect((pm.response.json().message||'')).to.include('invalid registry id'));"]),
        Step(name="create-bad-regid", method="POST", path=_BADREG, body={"repository": "x/y"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('CreateRepository invalid registry id', () => pm.expect((pm.response.json().message||'')).to.include('invalid registry id'));"]),
        Step(name="update-bad-regid", method="PATCH", path=_BADREG + "/x/y",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('UpdateRepository invalid registry id', () => pm.expect((pm.response.json().message||'')).to.include('invalid registry id'));"]),
        Step(name="delete-bad-regid", method="DELETE", path=_BADREG + "/x/y",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('DeleteRepository invalid registry id', () => pm.expect((pm.response.json().message||'')).to.include('invalid registry id'));"]),
        Step(name="rename-bad-regid", method="POST", path=_BADREG + "/x/y:rename", body={"newName": "x/z"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('RenameRepository invalid registry id', () => pm.expect((pm.response.json().message||'')).to.include('invalid registry id'));"]),
        Step(name="referrers-bad-regid", method="GET",
             path=_BADREG + "/x/y/referrers?subjectDigest=sha256:" + ("a" * 64),
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('ListReferrers invalid registry id', () => pm.expect((pm.response.json().message||'')).to.include('invalid registry id'));"]),
    ],
))


# --- UpdateRepository: unknown update_mask (A10) ----------------------------

# A10 (error-guessing on FieldMask): update_mask с полем вне known-set
# {description,labels,visibility} → sync 400 INVALID_ARGUMENT (corevalidate.UpdateMask
# отвергает до применения; Operation НЕ создаётся). Отделено от immutable-name (A11,
# REPO-UPD-NEG-IMMUTABLE) — разные ветки switch'а (immutable-check ДО UpdateMask).
CASES.append(Case(
    id="REPO-UPD-NEG-UNKNOWN-MASK",  # verifies RG-1-A10
    title="UpdateRepository unknown updateMask field → 400 INVALID_ARGUMENT (known-set {description,labels,visibility}, A10)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[
        *_create_repo("umask/svc-{{runId}}"),
        Step(name="update-unknown-mask", method="PATCH", path=_reg_base() + "/umask/svc-{{runId}}",
             body={"updateMask": "descriptionx", "description": "ignored"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))


# --- RenameRepository: collision + malformed newName (A17, A19a) ------------

# A17 (ECP: целевое имя занято): rename src→dst, где dst уже существует durable →
# ALREADY_EXISTS "repository already exists" (target overlay UNIQUE, D-5). Толерантен
# к sync-409 И async-op-error (одностейтментная запись под UNIQUE-backstop в worker'е).
# src остаётся под старым именем (rename не применён) — cleanup через registry-cascade.
CASES.append(Case(
    id="REPO-REN-NEG-COLLISION",  # verifies RG-1-A17
    title="RenameRepository into an existing repo name → 409 ALREADY_EXISTS \"repository already exists\" (A17)",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        *_create_repo("col/src-{{runId}}"),
        *_create_repo("col/dst-{{runId}}"),
        Step(name="rename-collision", method="POST", path=_reg_base() + "/col/src-{{runId}}:rename",
             body={"newName": "col/dst-{{runId}}"},
             test_script=[
                 "pm.test('collision rejected (409 sync or 200 async-error)', () => pm.expect(pm.response.code).to.be.oneOf([200, 409]));",
                 "const j = pm.response.json();",
                 "if (pm.response.code === 409) {",
                 "  pm.test('grpc code 6 (ALREADY_EXISTS)', () => pm.expect(j.code).to.eql(6));",
                 "  pm.test('repository already exists text', () => pm.expect((j.message||'').toLowerCase()).to.include('repository already exists'));",
                 "  pm.environment.set('renColSync', '1');",
                 "} else {",
                 "  pm.environment.unset('renColSync');",
                 "  if (j.id) pm.environment.set('opId', String(j.id));",
                 "}",
             ]),
        poll_operation_until_done(),
        Step(name="assert-collision-async", method="GET", path="/operations/{{opId}}",
             test_script=[
                 "if (pm.environment.get('renColSync') === '1') {",
                 "  pm.test('collision handled synchronously (409)', () => pm.expect(true).to.eql(true));",
                 "} else {",
                 "  const j = pm.response.json();",
                 "  pm.test('async op done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                 "  const blob = (JSON.stringify(j.error||{}) + (pm.environment.get('lastOpError')||'')).toLowerCase();",
                 "  pm.test('async op errored ALREADY_EXISTS', () => pm.expect(blob).to.include('exist'));",
                 "}",
             ]),
    ],
))

# A19a (ECP: malformed newName — отдельный класс от no-op newName==current в
# REPO-REN-NEG-NOOP): newName нарушает OCI repo-name grammar → sync 400 "invalid
# repository name '<X>'" (первым стейтментом, до repo/engine-вызова).
CASES.append(Case(
    id="REPO-REN-NEG-BADNAME",  # verifies RG-1-A19
    title="RenameRepository malformed newName → 400 INVALID_ARGUMENT \"invalid repository name\" first statement (A19)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[
        *_create_repo("renbad/svc-{{runId}}"),
        Step(name="rename-badname", method="POST", path=_reg_base() + "/renbad/svc-{{runId}}:rename",
             body={"newName": "Bad Name!"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('invalid repository name text', () => pm.expect((pm.response.json().message||'')).to.include('invalid repository name'));"]),
    ],
))

# D-5 (CONF: cross-registry rename структурно невыразим): RenameRepositoryRequest несёт
# только bare `new_name` — поля целевого реестра НЕТ. Смугленные body-поля
# (registryId/targetRegistryId) на :rename игнорируются (registry_id берётся из URL-пути;
# unknown-поля дропает grpc-gateway) → rename остаётся ВНУТРИ {{repoRegId}}. Локает, что
# ресурс нельзя увести в чужой реестр через body (anti-smuggle).
CASES.append(Case(
    id="REPO-REN-CROSS-REGISTRY-STRUCTURAL",  # verifies RG-1-A16
    title="RenameRepository — smuggled target-registry body fields ignored; rename stays within same registry (D-5)",
    classes=["CONF"], priority="P2",
    steps=[
        *_create_repo("xreg/src-{{runId}}"),
        Step(name="rename-smuggle", method="POST", path=_reg_base() + "/xreg/src-{{runId}}:rename",
             body={"newName": "xreg/dst-{{runId}}",
                   "registryId": "reg00000000smuggled0", "targetRegistryId": "reg00000000smuggled0"},
             test_script=[*assert_status(200), *assert_operation_envelope(OP_ENVELOPE),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="rename-smuggle-verify", method="GET", path=_reg_base() + "/xreg/dst-{{runId}}",
             test_script=[
                 "pm.test('rename op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('renamed within SAME registry (smuggled registryId body-field ignored)', () => pm.expect(j.registryId).to.eql(pm.environment.get('repoRegId')));",
                 "pm.test('new name applied', () => pm.expect(j.name).to.eql('xreg/dst-'+pm.environment.get('runId')));",
             ]),
    ],
))


# --- ListReferrers: absent repo (C02) --------------------------------------

# C02 (existence-hiding): ListReferrers на ОТСУТСТВУЮЩЕМ repo → 404 "repository not
# found" (parity с GetRepository/DeleteRepository absent). Дополняет REPO-REF-EMPTY
# (repo есть, referrer'ов нет → [] 200) и REPO-REF-NEG-BADDIGEST (malformed digest → 400):
# три разных класса на одном RPC (absent-repo / empty-referrers / bad-digest).
CASES.append(Case(
    id="REPO-REF-NEG-ABSENT",  # verifies RG-1-C02
    title="ListReferrers on absent repo → 404 \"repository not found\" (existence-hiding, C02)",
    classes=["NEG"], priority="P1",
    steps=[Step(name="referrers-absent", method="GET",
                path=_reg_base() + "/ghostref/svc-{{runId}}/referrers?subjectDigest=sha256:" + ("b" * 64),
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             "pm.test('repository not found text', () => pm.expect(pm.response.json().message).to.eql('repository not found'));"])],
))

# security.md #6 (CONF: hide-existence byte-identity): absent-repo miss обязан быть
# байт-в-байт "repository not found" ЧЕРЕЗ Get/Delete/ListReferrers — единый uniform
# существование-скрывающий текст (различимый текст = existence-oracle / FGA-type-leak).
# Все три над ОДНИМ absent-именем от editor'а (authorized на реестр, repo отсутствует) —
# детерминированно, без multi-user авторизации.
CASES.append(Case(
    id="REPO-EXISTENCE-HIDING-PARITY",  # verifies RG-1-A08, RG-1-A15, RG-1-C02
    title="Absent repo → byte-identical \"repository not found\" across Get/Delete/ListReferrers (uniform existence-hiding, security.md #6)",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="hide-get-absent", method="GET", path=_reg_base() + "/hide/svc-{{runId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.environment.set('repoMissMsg', pm.response.json().message||'');",
                          "pm.test('Get absent → repository not found', () => pm.expect(pm.environment.get('repoMissMsg')).to.eql('repository not found'));"]),
        Step(name="hide-delete-absent", method="DELETE", path=_reg_base() + "/hide/svc-{{runId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('Delete absent byte-identical to Get miss', () => pm.expect(pm.response.json().message).to.eql(pm.environment.get('repoMissMsg')));"]),
        Step(name="hide-referrers-absent", method="GET",
             path=_reg_base() + "/hide/svc-{{runId}}/referrers?subjectDigest=sha256:" + ("c" * 64),
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('ListReferrers absent byte-identical to Get miss', () => pm.expect(pm.response.json().message).to.eql(pm.environment.get('repoMissMsg')));"]),
    ],
))


# --- X01: public Repository не несёт инфра-полей (two-projection) -----------

# X01 (CONF two-projection): публичный GetRepository несёт ТОЛЬКО tenant-intent + result;
# инфра-поля (engine namespace / bucket / storage-driver / числовой инфра-id) живут
# только в Internal* (:9091, security.md §Инфра-чувствительные данные). GetRegistryStats/
# RegistryStats — Internal-only (нет google.api.http → недостижимы на public REST).
CASES.append(Case(
    id="REPO-GET-NO-INFRA-LEAK",  # verifies RG-1-X01, RG-1-A07
    title="GetRepository → public body carries NO infra fields (engineNamespace/bucketPrefix/numericInfraId, two-projection X01)",
    classes=["CONF"], priority="P1",
    steps=[
        *_create_repo("noinfra/svc-{{runId}}"),
        Step(name="get-no-infra", method="GET", path=_reg_base() + "/noinfra/svc-{{runId}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('no engineNamespace (Internal* only)', () => pm.expect(j).to.not.have.property('engineNamespace'));",
                 "pm.test('no bucketPrefix (Internal* only)', () => pm.expect(j).to.not.have.property('bucketPrefix'));",
                 "pm.test('no numericInfraId (Internal* only)', () => pm.expect(j).to.not.have.property('numericInfraId'));",
                 "pm.test('no storageDriver (Internal* only)', () => pm.expect(j).to.not.have.property('storageDriver'));",
             ]),
    ],
))


# --- cleanup ---------------------------------------------------------------
CASES.append(Case(
    id="REPO-CLEANUP", title="Cleanup: delete shared overlay registry",
    classes=["CRUD"], priority="P3",
    steps=[
        Step(name="reg-delete", method="DELETE", path=REG + "/{{repoRegId}}",
             test_script=["pm.test('delete accepted (200/404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
                          "const j=pm.response.json(); if (j && j.id) pm.environment.set('opId', String(j.id));"]),
        poll_operation_until_done(),
    ],
))
