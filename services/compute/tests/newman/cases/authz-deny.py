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


# Единственный законный исход ALLOW-полосы Create — СИНХРОННЫЙ отказ предусловия,
# и он УСТАНОВЛЕН по коду владельца, а не угадан.
#
# Тело Create этой матрицы (см. `define_resource_cases`) — машина вида VM без
# внешнего адреса и без снятия стража недостижимости. `ValidateCreateInstanceReq`
# (`services/compute/internal/apps/kacho/api/instance/instance.go`, F5) отвергает
# такой вход СИНХРОННО — до создания Operation — кодом `FAILED_PRECONDITION` и
# дословно этим текстом; отображение кода в статус задаёт библиотека края
# (`runtime.HTTPStatusFromCode`, край собирается без `WithErrorHandler`), поэтому
# `FAILED_PRECONDITION` — это **400**, а не 412 (`api-conventions.md`
# §«gRPC-код → HTTP-статус»). Тот же исход утверждает и уже зелёный кейс того же
# набора `INST-RD-CR-VAL-UNREACHABLE-GUARD` (cases/instance-redesign.py) на
# идентичном теле — то есть полоса подтверждена не только чтением.
#
# ПОЧЕМУ ЭТО НЕ ПОДМЕНА ПРЕДМЕТА. Предмет матрицы — решение о ДОСТУПЕ, и он
# сохранён строго: до валидатора доходит только запрос, который край ПРОПУСТИЛ, а
# мутацию без права край отвергает `403` (полоса DENY ниже). Значит 400+9 на этом
# входе может получить ровно тот субъект, которому доступ дан. Отличие ALLOW от
# DENY остаётся однозначным, но теперь оно утверждается ИСХОДОМ, а не его
# отрицанием.
#
# ПОЧЕМУ ПРЕЖНЕЕ УТВЕРЖДЕНИЕ НЕ БЫЛО УТВЕРЖДЕНИЕМ. Здесь стояло «не 403 и не 401»
# (плюс те же два отрицания по коду `google.rpc.Status`) с оговоркой, что
# downstream-валидация вправе ответить чем угодно. Отрицание проходит на успехе,
# на отказе валидации, на 500 и на 503 — то есть не отличает исправную систему ни
# от одной поломки, кроме той единственной, что подписана 403. Заодно шаг,
# у которого ВСЕ утверждения о статусе отрицательные, читается гейтами дерева
# (`internal/repohygiene`) как ПРОБА ОТКАЗА и выпадает из их рассмотрения.
# Полоса переведена на утверждение-пару задачей kacho#668.
#
# ЕСЛИ ТЕЛО CREATE ПОМЕНЯЕТСЯ — ПОМЕНЯЕТСЯ И ЭТА ПОЛОСА. Связь названа здесь
# намеренно: полоса привязана к КОНКРЕТНОМУ входу, и это её свойство, а не изъян.
# Добавили `acknowledgeUnreachable` либо внешний адрес — вход проходит F5, и
# законным исходом становится другой отказ (следующий по порядку резолв ссылки);
# тогда правится и утверждение, осознанно, а не подгоняется под зелёный.
_ALLOW_PRECONDITION_MESSAGE = ("VM will be RUNNING but unreachable (no external address); "
                               "set acknowledgeUnreachable:true to proceed")


def allow_asserts(case_id):
    """ALLOW-полоса Create: край ПРОПУСТИЛ, владелец упёрся в НАЗВАННОЕ предусловие.

    Утверждается ПАРА — HTTP-статус и `code` из `google.rpc.Status` — плюс
    контрактный тон отказа. Разбор, почему исход один и почему предмет матрицы при
    этом сохранён, — в комментарии над этой функцией.
    """
    return [
        f"pm.test('[{case_id}] ALLOW→предусловие: HTTP 400', () => "
        f"pm.expect(pm.response.code, pm.response.text()).to.equal(400));",
        "let _j; try { _j = pm.response.json(); } catch(e) { _j = null; }",
        f"pm.test('[{case_id}] ALLOW→предусловие: grpc code 9 (FAILED_PRECONDITION)', () => "
        f"pm.expect(_j && _j.code, JSON.stringify(_j)).to.equal(9));",
        f"pm.test('[{case_id}] ALLOW→предусловие: контрактный тон отказа', () => "
        f"pm.expect(_j && _j.message, JSON.stringify(_j)).to.equal('{_ALLOW_PRECONDITION_MESSAGE}'));",
    ]


def list_allow_asserts(case_id, list_key):
    """List субъектом, у которого ЕСТЬ доступ к project'у → 200 + отфильтрованный список.

    ИСХОД ОДИН, установлен по матрице «субъект × право на список». `InstanceService/List`
    — единственный `/List`, который вызывает эта суита, — гейтится отношением `viewer` на
    `project:<projectId>` (запись каталога прав `{project, project_id}`; тот же scope и
    отношение в `internal/check/permission_map.go`). В модели `project.viewer` выводится
    из `editor` → `admin` → `super_admin`, а `super_admin` — из `admin from account`.
    Значит каждый ALLOW-субъект матрицы держит `viewer`: PA1 и INV — прямым
    `editor @ project-A1`, AAA — через `admin @ account-A`, AAB и INV на project-B1 —
    через `admin @ account-B`.

    ПРЕЖНЯЯ ТОЛЕРАНТНОСТЬ ССЫЛАЛАСЬ НА ЧУЖУЮ ПОЛОСУ. Она принимала `200 ИЛИ 403` и
    объясняла 403 отношением `v_list`, «развязанным от tier». В compute `v_list` гейтит
    под-списки НА РЕСУРСЕ (`ListOperations` и родственные), а не project-scope List — то
    есть строка принимала ровно тот отказ, ради ловли которого написана. Внутреннее
    подтверждение, не требующее модели: строки Create для этого же субъекта и project'а
    уже требуют «не 403», а Create гейтится `editor` — отношением сильнее, чем нужный
    списку `viewer`; поэтому и «грант мог не доехать» здесь не защита, он ронял бы
    сначала Create. Та же форма — у vpc (`services/vpc/tests/newman/cases/authz-deny.py`,
    `list_allow_asserts`); там свойство держит не звание эталона, а проба инъекции
    `services/vpc/tests/newman/scripts/selftest_authz_allow_lanes.py` — здесь ей
    отвечает `scripts/selftest_authz_allow_lanes.py` этого набора."""
    return [
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] LIST grant: 200 (scope-filtered page)', () => "
        f"pm.expect(pm.response.code, 'expected 200, body: ' + pm.response.text()).to.equal(200));",
        f"pm.test('[{case_id}] LIST grant: ответ несёт список', () => "
        f"pm.expect((j && j['{list_key}']) || [], JSON.stringify(j)).to.be.an('array'));",
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


def emit(case_id_prefix, title, scope, method, path, body, subject, mode="gate", list_key=None):
    """mode:
        gate — ALLOW/DENY по EXPECT[scope][code]: субъект адресует РЕЗОЛВИМЫЙ scope
               (Create несёт `projectId`), поэтому решение о доступе принимается по
               матрице и каждая ветка требует СВОЕГО исхода: ANON→401, DENY→403+7,
               ALLOW→не 403 и не 401.
        list — call-gate `viewer` на `project:<projectId>` + сужение результата:
               ANON→401; has-access→200 + список (403 невозможен, см.
               list_allow_asserts); no-access→403 ЛИБО 200 + ПУСТОЙ список.
        nf   — object-scoped Get по garbage-id: 404 + code 5 (hide-existence) для ЛЮБОГО
               аутентифицированного субъекта, ANON→401.
        deny — object-scoped Update/Delete по garbage-id: 403 + code 7 для ЛЮБОГО
               аутентифицированного субъекта, ANON→401.

    Почему garbage-id адресуется отдельными режимами, а не строкой матрицы. У этих шагов
    объекта не существует, поэтому исход по построению НЕ зависит от прав вызывающего:
    Get прячет существование (404 одинаков для «нет доступа» и «нет объекта» —
    anti-enumeration), а мутация не резолвит scope и fail-close'ится 403. Раньше они
    ходили через матрицу, и ALLOW-ветка вынуждена была принимать И успех, И отказ
    (`oneOf([200,400,403,404,409])`) — то есть не утверждала ничего и не могла упасть на
    регрессии прав. Режим определяется тем, что кейс реально спрашивает, и каждый режим
    требует ОДНОГО исхода. Та же раскладка полос — в
    `services/vpc/tests/newman/cases/authz-deny.py`; звания эталона у неё нет, свойство
    держит проба инъекции рядом с ней."""
    code, label, auth = subject
    case_id = f"AUTHZ-{case_id_prefix}-{code}"
    if mode in ("nf", "deny"):
        if code == "ANON":
            decision, asserts = "UNAUTH", unauth_asserts(case_id)
        elif mode == "nf":
            decision, asserts = "NF", read_deny_asserts(case_id)
        else:
            decision, asserts = "DENY", deny_asserts(case_id)
        CASES.append(Case(
            id=case_id,
            title=f"[{decision}] {title} as {label} ({scope})",
            classes=["AUTHZ", "NEG"],
            priority="P1",
            steps=[Step(name=method.lower(), method=method, path=path, body=body,
                        auth=auth, test_script=asserts)],
        ))
        return
    decision = EXPECT[scope][code]
    if mode == "list":
        if code == "ANON":
            decision, asserts = "UNAUTH", unauth_asserts(case_id)
        elif decision == "ALLOW":
            decision, asserts = "LIST-ALLOW", list_allow_asserts(case_id, list_key)
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
            # anon row is 401/16 regardless of method/path. Метка кейса обязана назвать
            # ИМЕННО этот исход: до правки шаг утверждал 401, а назывался `[DENY]` —
            # заголовок обещал одно, утверждение требовало другого.
            decision, asserts = "UNAUTH", unauth_asserts(case_id)
        else:
            asserts = deny_asserts(case_id)
    else:
        # ALLOW на резолвимом scope: Create несёт `projectId` — ровно то поле, которое
        # извлекает каталог (`InstanceService/Create` → scope_extractor {project,
        # project_id}), — поэтому scope резолвится и защитимого authz-first 403 у
        # ALLOW-субъекта не остаётся. Утверждение называет ИСХОД (400+9 на предусловии
        # F5), а не его отрицание: 403 здесь = регрессия прав и валит кейс.
        asserts = allow_asserts(case_id)
    CASES.append(Case(
        id=case_id,
        title=f"[{decision}] {title} as {label} ({scope})",
        classes=["AUTHZ", "POS" if decision == "ALLOW" else "NEG"],
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
        # Garbage-id шаги: объекта нет, поэтому исход по построению одинаков для ВСЕХ
        # аутентифицированных субъектов — Get прячет существование (404/5), мутация не
        # резолвит scope и fail-close'ится (403/7). Режимы nf/deny требуют этого ОДНОГО
        # исхода вместо прежней ALLOW-строки, принимавшей и успех, и отказ.
        emit(f"{resource_name.upper()}-GT", f"Get {resource_name} (garbage id)",
             "project-A1", "GET", f"{plural_path}/{GARBAGE_ID}", None, subj, mode="nf")
        if supports_update:
            emit(f"{resource_name.upper()}-UP", f"Update {resource_name} (garbage id)",
                 "project-A1", "PATCH", f"{plural_path}/{GARBAGE_ID}", {"name": "x"}, subj,
                 mode="deny")
        emit(f"{resource_name.upper()}-DL", f"Delete {resource_name} (garbage id)",
             "project-A1", "DELETE", f"{plural_path}/{GARBAGE_ID}", None, subj, mode="deny")


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
# Disk / Image / Snapshot arms retired with compute's duplicate block storage —
# kacho-storage owns Volume/Image/Snapshot and carries the deny-matrix for its own.


# ---------------------------------------------------------------------------
# Catalog resources — none left in compute's deny-matrix.
#
# DiskType went with the block-storage duplicate (kacho-storage is the owner and
# carries the catalog deny-matrix for it); Region/Zone serving was removed at Stage
# S7 (Geography is owned by kacho-geo). MachineType has its own suite.
# ---------------------------------------------------------------------------


# ---------------------------------------------------------------------------
# Cross-domain validation (KAC-122 §5.4 CD-*)
#
# KAC-266: the former CD-INST-XACCT-SUBNET case (cross-account subnet via an
# instance NIC spec) was removed — NIC binding is no longer part of the Instance
# lifecycle (no auto-NIC), so Instance.Create performs no cross-account subnet
# peer-validation. Generic instance create-deny (above) already covers the authz
# gate denial for the same subjects.
# ---------------------------------------------------------------------------
