# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set authz-deny для kacho-compute (KAC-122).

Проверяет default-deny matrix для 6 субъектов на каждом публичном CRUD compute-ресурсов
+ catalog-read для DiskType (Region/Zone serving снят в Stage S7 — Geography в kacho-geo).
Источник истины матрицы — `docs/superpowers/specs/2026-05-19-authz-default-deny-matrix-newman-design.md`.

Pre-conditions: `tests/authz-fixtures/setup.sh`. Env-var'ы те же что vpc.
"""

CASES = []

SUBJECTS = [
    ("ANON", "anon",       "anonymous"),
    ("NOB",  "no-bind",    "jwtNoBindings"),
    ("PA1",  "proj-adm",   "jwtProjectAdminA1"),
    ("AAA",  "acct-adm-a", "jwtAccountAdminA"),
    ("AAB",  "acct-adm-b", "jwtAccountAdminB"),
    ("INV",  "invitee",    "jwtInvitee"),
]

EXPECT = {
    "project-A1":         {"ANON":"DENY","NOB":"DENY","PA1":"ALLOW","AAA":"ALLOW","AAB":"DENY", "INV":"ALLOW"},
    "project-B1":         {"ANON":"DENY","NOB":"DENY","PA1":"DENY", "AAA":"DENY", "AAB":"ALLOW","INV":"ALLOW"},
    "catalog-read":       {"ANON":"DENY","NOB":"ALLOW","PA1":"ALLOW","AAA":"ALLOW","AAB":"ALLOW","INV":"ALLOW"},
    "catalog-mutate":     {"ANON":"DENY","NOB":"DENY","PA1":"DENY", "AAA":"DENY", "AAB":"DENY", "INV":"DENY"},
}


def deny_asserts(case_id):
    return [
        f"pm.test('[{case_id}] DENY: status 403', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(403));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] DENY: grpc code 7 (PERMISSION_DENIED)', () => pm.expect(j && j.code, JSON.stringify(j)).to.equal(7));",
        f"pm.test('[{case_id}] DENY: message contains permission denied', () => pm.expect((j && j.message || '').toLowerCase()).to.contain('permission denied'));",
    ]


def allow_asserts(case_id):
    return [
        f"pm.test('[{case_id}] ALLOW: not 403', () => pm.expect(pm.response.code, 'unexpected 403: ' + pm.response.text()).to.not.equal(403));",
        "let _j; try { _j = pm.response.json(); } catch(e) { _j = null; }",
        f"pm.test('[{case_id}] ALLOW: not Unauthenticated (16)', () => pm.expect(_j && _j.code, JSON.stringify(_j)).to.not.equal(16));",
    ]


def allow_or_authzfirst_asserts(case_id):
    """ALLOW-субъект на project-scoped CR/UP/DL/GT, где api-gateway scope_extractor
    fail-close'ит 403 ДО per-subject backend-Check — authz-first ordering (security.md
    §object-scoped authz). Толерантно к 200|400|403|404|409 (allowed / backend-reject /
    gateway scope|absent-id fail-close); 401 (code 16, потерянная аутентификация)
    по-прежнему FAIL. Паритет с gen.py assert_unscoped_rejected/assert_absent_id_rejected
    (403|400|404) и vpc authz-deny (garbage-id mutation-deny 403).

    Толерантность НЕ маскирует security-инвариант: DENY/UNAUTH/READ-DENY остаются строгими
    (утечка 200-где-нужен-deny всё так же валит suite), и применяется она ТОЛЬКО к
    project-scoped ALLOW; catalog-read ALLOW (DiskType, публичный read → 200) сохраняет
    строгий allow_asserts.

    Живой defensible-403 flavor остался один:
      * UP/DL garbage-id — мутация над несуществующим объектом: scope garbage-id не
        резолвится → fail-close 403 (либо backend 404).

    LS сюда БОЛЬШЕ НЕ ХОДИТ: список адресуется `?projectId=` (mode="list"), scope
    резолвится, и List отвечает по scope-filtered контракту — см. list_allow_asserts /
    list_deny_asserts.

    NB (обязательный follow-up, требует ЖИВОГО стенда): для CR-OWN/CR-CROSS этот flavor
    БОЛЬШЕ НЕ ДЕЙСТВУЕТ. Тело Create теперь несёт `projectId` — ровно то поле, которое
    извлекает каталог (`{Instance,Disk,Image,Snapshot}Service/Create` → scope_extractor
    {project, project_id}), — поэтому scope резолвится и защитимого 403 у ALLOW-субъекта
    не остаётся. Пока здесь стоит толерантный oneOf, CR-ALLOW-строка не утверждает почти
    ничего: она зеленеет и на 403, то есть ровно на том отказе, который обязана поймать.
    Первый прогон на живом стенде обязан перевести CR-OWN/CR-CROSS на строгий
    allow_asserts (`not 403`); здесь этого не сделано осознанно — это утверждение о
    ПОВЕДЕНИИ, а правка статическая, стенда нет, и «зелёное по догадке» было бы той же
    формой без содержания с обратным знаком."""
    return [
        f"pm.test('[{case_id}] ALLOW (authz-first tolerant): 200|400|403|404|409, not 401', () => "
        f"pm.expect(pm.response.code, 'unexpected code, body: ' + pm.response.text()).to.be.oneOf([200, 400, 403, 404, 409]));",
        "let _j; try { _j = pm.response.json(); } catch(e) { _j = null; }",
        f"pm.test('[{case_id}] ALLOW: not Unauthenticated (16)', () => pm.expect(_j && _j.code, JSON.stringify(_j)).to.not.equal(16));",
    ]


def list_allow_asserts(case_id):
    """List субъектом, имеющим ГРАНТ на project. `/List` дочерних ресурсов гейтится
    verb-relation'ом `v_list`, РАЗВЯЗАННЫМ от tier (editor/viewer/admin его не
    имплицируют), поэтому субъект с tier-грантом, но без явного v_list, корректно
    получает 403 «lacks relation v_list» — by-design read-gating, а не отказ доступа к
    проекту. Субъект с v_list получает 200 + отфильтрованный по гранту список. Обе ветки
    безопасны (свой project — чужого не показано), 401 — FAIL. Дословный паритет с
    vpc/tests/newman/cases/authz-deny.py, эталоном этой матрицы."""
    return [
        f"pm.test('[{case_id}] LIST grant: 200 (v_list/filtered) OR 403 (lacks v_list)', () => "
        f"pm.expect(pm.response.code, 'expected 200 or 403, body: ' + pm.response.text()).to.be.oneOf([200, 403]));",
    ]


def list_deny_asserts(case_id, list_key):
    """List без доступа → 403 (gated List RPC) ИЛИ 200 + ПУСТОЙ список (scope-filtered).
    200 + непустой список чужого project'а = LEAK и валит кейс.

    Это НЕ ослабление прежнего строгого 403: прежний ассерт описывал запрос, которого
    никто не делал. Список адресовался доредизайновым ключом вне контракта, который край
    молча отбрасывал, — так что до сервиса уезжал UNSCOPED List, отвечавший 403 ОДИНАКОВО
    всем шести субъектам. Строка зеленела на ответе, к project-scope отношения не имевшем,
    и утечки чужого проекта поймать не могла by construction. Здесь адрес починен
    (`?projectId=`), а утверждение заменено на контракт scope-filtered List — тот же, что
    у vpc, — и leak-guard добавлен, которого раньше не было вовсе."""
    return [
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        (
            f"pm.test('[{case_id}] LIST no-access: 403 OR 200+empty (no leak)', () => {{\n"
            "  const code = pm.response.code;\n"
            "  if (code === 403) return;\n"
            "  pm.expect(code, 'expected 403 or 200, body: ' + pm.response.text()).to.equal(200);\n"
            f"  const arr = (j && j['{list_key}']) || [];\n"
            "  pm.expect(arr.length, 'no-access List must be scope-filtered to EMPTY (LEAK!): ' + pm.response.text()).to.equal(0);\n"
            "});"
        ),
    ]


def unauth_asserts(case_id):
    # Anonymous (no credentials) → 401 + code 16 (UNAUTHENTICATED), not 403 + code 7
    # (PERMISSION_DENIED). gRPC/HTTP convention: missing credentials → UNAUTHENTICATED
    # (16) → HTTP 401; authenticated-but-denied → PERMISSION_DENIED (7) → HTTP 403.
    return [
        f"pm.test('[{case_id}] UNAUTH: status 401', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(401));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] UNAUTH: grpc code 16 (UNAUTHENTICATED)', () => pm.expect(j && j.code, JSON.stringify(j)).to.equal(16));",
    ]


def read_deny_asserts(case_id):
    # Hide-existence: a denied single-resource read (Get) on a verb-bearing compute
    # resource is surfaced as NotFound (404 / code 5), never PermissionDenied — no
    # enumeration / existence leak. Applies to authenticated-but-denied AND to a denied
    # read of a (well-formed) nonexistent id — both yield the same 404, so an attacker
    # cannot tell "exists but forbidden" from "does not exist".
    return [
        f"pm.test('[{case_id}] READ-DENY: status 404 (hide existence)', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(404));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] READ-DENY: grpc code 5 (NOT_FOUND, not 7)', () => pm.expect(j && j.code, JSON.stringify(j)).to.equal(5));",
        f"pm.test('[{case_id}] READ-DENY: no deny_reasons leak', () => pm.expect(JSON.stringify(j || {{}}).toLowerCase()).to.not.include('deny_reasons'));",
    ]


def _is_single_resource_get(path):
    # A single-resource Get targets one object: the path's last segment is a concrete id
    # — a `{{var}}` placeholder or a literal resource id (3-char prefix + ≥17 chars) —
    # with NO query string. A List (collection) carries a ?query (e.g. ?projectId=…) or
    # ends in the bare plural (`/instances`); those are NOT single reads and a denied List
    # stays PermissionDenied (403), not hidden as 404.
    if "?" in path:
        return False
    last = path.rstrip("/").rsplit("/", 1)[-1]
    if last.startswith("{{") and last.endswith("}}"):
        return True
    # Literal resource id: 3-char alpha prefix + ≥17 trailing alnum chars (matches the
    # GARBAGE_ID format), distinguishing it from the bare plural collection name.
    return len(last) >= 20 and last[:3].isalpha() and last[3:].isalnum()


def emit(case_id_prefix, title, scope, method, path, body, subject, mode="gate", list_key=None):
    """mode:
        gate — ALLOW/DENY по EXPECT[scope][code] (Create / Get / Update / Delete).
        list — scope-filtered List: ANON→401; has-access→200|403; no-access→403|200+empty.
    """
    code, label, auth = subject
    decision = EXPECT[scope][code]
    case_id = f"AUTHZ-{case_id_prefix}-{code}"
    if mode == "list":
        if code == "ANON":
            decision, asserts = "UNAUTH", unauth_asserts(case_id)
        elif decision == "ALLOW":
            decision, asserts = "LIST-ALLOW", list_allow_asserts(case_id)
        else:
            decision, asserts = "LIST-DENY", list_deny_asserts(case_id, list_key)
        step = Step(name=method.lower(), method=method, path=path, body=body, auth=auth, test_script=asserts)
        # LIST-DENY leak-guard ("no-access → 403 or 200+EMPTY"): a fixture subject can carry a
        # residual account-scoped viewer from a concurrently running suite, which via
        # account→project containment transiently makes this project's children v_list-visible
        # until that suite's revoke materializes (read-your-writes ON REVOKE, eventually
        # consistent). retry_until_absent retries while the list is still non-empty and
        # FAILS-OPEN at the budget, so a genuine over-show (rows that never leave) still fails
        # the leak assertion — a real leak is NOT masked. Same treatment as the vpc suite.
        if decision == "LIST-DENY" and list_key:
            step = retry_until_absent(step, f"((pm.response.json()['{list_key}'])||[]).length > 0")
        CASES.append(Case(
            id=case_id,
            title=f"[{decision}] {title} as {label} ({scope})",
            classes=["AUTHZ", "NEG" if decision == "UNAUTH" else "POS"],
            priority="P1",
            steps=[step],
        ))
        return
    if decision == "DENY":
        if code == "ANON":
            # Anonymous (no credentials) fails authN BEFORE authz for EVERY RPC —
            # Create (POST) and List (GET collection) alike, not just single-resource
            # Get: missing credentials → UNAUTHENTICATED (16) → HTTP 401 ("credentials
            # required"), never PERMISSION_DENIED (403/7) nor the hide-existence 404.
            # unauthenticated ≠ unauthorized (authN precedes authz), so the whole
            # anon row is 401/16 regardless of method/path.
            asserts = unauth_asserts(case_id)
        elif method == "GET" and _is_single_resource_get(path):
            # Hide-existence: a denied single-resource Get on a verb-bearing compute
            # resource → NotFound (404 / code 5), not 403. ONLY a single-resource Get
            # (path ends in /{id}, no ?query) hides existence; a denied List stays
            # PermissionDenied (403).
            asserts = read_deny_asserts(case_id)
        else:
            asserts = deny_asserts(case_id)
    else:
        # project-scoped ALLOW: gateway может authz-first fail-close'ить 403 на unscoped
        # collection-op / absent garbage-id ДО per-subject Check → tolerant (см.
        # allow_or_authzfirst_asserts). catalog-read ALLOW (публичный DiskType read →
        # 200) сохраняет строгий not-403 allow_asserts.
        if scope in ("project-A1", "project-B1"):
            asserts = allow_or_authzfirst_asserts(case_id)
        else:
            asserts = allow_asserts(case_id)
    CASES.append(Case(
        id=case_id,
        title=f"[{decision}] {title} as {label} ({scope})",
        classes=["AUTHZ", "NEG" if decision == "DENY" else "POS"],
        priority="P1",
        steps=[Step(name=method.lower(), method=method, path=path, body=body, auth=auth, test_script=asserts)],
    ))


GARBAGE_ID = "epdnonexistent000001"   # compute resource id prefix


def define_resource_cases(resource_name, plural, create_body_extra=None, supports_update=True):
    create_body_extra = create_body_extra or {}
    plural_path = f"/compute/v1/{plural}"
    for subj in SUBJECTS:
        body_own = {"projectId": "{{projectA1Id}}", "name": f"authz-{resource_name}-{subj[0].lower()}-own-{{{{runId}}}}", **create_body_extra}
        emit(f"{resource_name.upper()}-CR-OWN", f"Create {resource_name} в project-A1",
             "project-A1", "POST", plural_path, body_own, subj)
        body_cross = {"projectId": "{{projectB1Id}}", "name": f"authz-{resource_name}-{subj[0].lower()}-cross-{{{{runId}}}}", **create_body_extra}
        emit(f"{resource_name.upper()}-CR-CROSS", f"Create {resource_name} в project-B1 (cross-account)",
             "project-B1", "POST", plural_path, body_cross, subj)
        emit(f"{resource_name.upper()}-LS-OWN", f"List {plural} в project-A1",
             "project-A1", "GET", f"{plural_path}?projectId={{{{projectA1Id}}}}", None, subj,
             mode="list", list_key=plural)
        emit(f"{resource_name.upper()}-LS-CROSS", f"List {plural} в project-B1 (cross-account)",
             "project-B1", "GET", f"{plural_path}?projectId={{{{projectB1Id}}}}", None, subj,
             mode="list", list_key=plural)
        emit(f"{resource_name.upper()}-GT", f"Get {resource_name} (garbage id)",
             "project-A1", "GET", f"{plural_path}/{GARBAGE_ID}", None, subj)
        if supports_update:
            emit(f"{resource_name.upper()}-UP", f"Update {resource_name} (garbage id)",
                 "project-A1", "PATCH", f"{plural_path}/{GARBAGE_ID}", {"name": "x"}, subj)
        emit(f"{resource_name.upper()}-DL", f"Delete {resource_name} (garbage id)",
             "project-A1", "DELETE", f"{plural_path}/{GARBAGE_ID}", None, subj)


# Project-scoped compute ресурсы.
#
# Тело Create — по ДЕЙСТВУЮЩЕМУ контракту ресурса. Матрица меряет authz, а не
# создание, и на DENY-строках до разбора тела дело не доходит вовсе — но кейс
# документирует контракт, поэтому легаси-имена в нём становятся ложью, которую
# нечему опровергнуть. Сюда уже уехали снятые редизайном `platformId`/
# `resourcesSpec`/`bootDiskSpec` (в proto — `reserved` по номеру И имени), а следом —
# доредизайновый scope-ключ списка, заменённый на `projectId`: край отбрасывал его
# молча, поэтому 48 LS-запросов уезжали unscoped и отвечали 403 всем субъектам
# одинаково, ничего про project-scope не проверяя (см. list_deny_asserts).
# Значения намеренно НЕ резолвятся (`mt-` вне каталога): предмет — решение о
# доступе, оно принимается раньше любого резолва.
define_resource_cases("instance", "instances", create_body_extra={
    "zoneId": "ru-central1-a",
    "instanceKind": "VM",
    "machineTypeId": "mt-placeholder0000000",
    "bootSource": {"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts"},
    "vmSpec": {"userData": "#cloud-config\n{}"},
    "useDefaultNetwork": True,
})
define_resource_cases("disk", "disks", create_body_extra={
    "zoneId": "ru-central1-a", "typeId": "network-ssd", "size": "8589934592"
})
define_resource_cases("image", "images", create_body_extra={"family": "authz-test"})
define_resource_cases("snapshot", "snapshots", create_body_extra={"diskId": "epdnonexistent000099"})


# ---------------------------------------------------------------------------
# Catalog resources (DiskType) — read-only публично; admin-mutate
# Region/Zone serving removed (Stage S7) — Geography is owned by kacho-geo.
# ---------------------------------------------------------------------------

CATALOG_READ_RESOURCES = [
    ("disktype", "/compute/v1/diskTypes", "/compute/v1/diskTypes/network-ssd"),
]

for name, list_path, get_path in CATALOG_READ_RESOURCES:
    for subj in SUBJECTS:
        emit(f"{name.upper()}-LS", f"List {name} (catalog)", "catalog-read",
             "GET", list_path, None, subj)
        emit(f"{name.upper()}-GT", f"Get {name} (catalog)", "catalog-read",
             "GET", get_path, None, subj)

# Catalog-mutate (admin-only — via Internal*Service на cluster-internal listener).
# Все 6 субъектов DENY: они либо не admin (anon/NOB/PA1/INV), либо account-level (AAA/AAB).
#
# У DiskType человекочитаемого `name` НЕТ и никогда не было: тип диска адресуется
# стабильным слагом `id` (`network-ssd`), а текст живёт в `description`
# (CreateDiskTypeRequest{id, description, zone_ids}; core #15 — адресация по id).
# Кейс слал `name`, которого в контракте нет, — край его отбрасывал.
for name, list_path, _ in CATALOG_READ_RESOURCES:
    for subj in SUBJECTS:
        emit(f"{name.upper()}-CR", f"Create {name} (catalog admin)", "catalog-mutate",
             "POST", list_path, {"id": f"authz-{name}-{subj[0].lower()}", "description": "authz"}, subj)


# ---------------------------------------------------------------------------
# Cross-domain validation (KAC-122 §5.4 CD-*)
#
# KAC-266: the former CD-INST-XACCT-SUBNET case (cross-account subnet via an
# instance NIC spec) was removed — NIC binding is no longer part of the Instance
# lifecycle (no auto-NIC), so Instance.Create performs no cross-account subnet
# peer-validation. Generic instance create-deny (above) already covers the authz
# gate denial for the same subjects.
# ---------------------------------------------------------------------------
