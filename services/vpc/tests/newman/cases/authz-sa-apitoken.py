# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set authz-sa-apitoken — доступ НЕ-человеческого субъекта к ресурсам vpc через край.

Перенесён из набора службы доступа (PRO-Robotech/kaname, коллекция того же имени;
держатель переноса — #2912, сторона службы — PRO-Robotech/kaname#415). Там его не
гонял ни один шаг конвейера; дом по предмету — этот набор: 20 из 30 запросов идут
в `vpc`, и половина ALLOW-позиций определена семантикой vpc (списки
`NetworkService.List` закрыты `viewer` на запрошенном проекте). Здесь его гоняет
шаг `гейт — newman зелёный (vpc)` (`.github/workflows/e2e-newman.yml`).
Утверждения перенесены без изменения строгости.

Расширяет default-deny матрицу `cases/authz-deny.py` двумя НЕ-человеческими
классами субъектов:

  Модель 5 — Service Account: предъявитель служебной учётки
    (`kaname_principal_type=service_account`, `sub=<svaId>`), выданный посевом
    через обмен ключа учётки у издателя платформы.

  Модель 6 — API token: долгоживущий предъявитель, привязанный к принципалу и
    области. Покрываются valid-in-scope / out-of-scope / revoked / expired /
    malformed.

Почему отдельный модуль, а не расширение `authz-deny.py`: у субъектов моделей 1–4
единый путь получения предъявителя, у служебной учётки и api-токена он свой
(обмен ключа, выпуск и отзыв), и расхождение путей — ровно то, что здесь
проверяется.

Семантика утверждений (паритет с `authz-deny.py`):
  - DENY        → 403 + grpc 7 (PERMISSION_DENIED) + "permission denied";
                  одиночное чтение (`GET /res/{id}`) — 404 + grpc 5 (сокрытие существования)
  - ALLOW       → ИСХОД по форме запроса: чтение — 200 и id запрошенного; список — 200 и
                  конверт выдачи; мутация — 200 и конверт Operation vpc (`enp…`)
  - UNAUTH      → 401 + grpc 16 (UNAUTHENTICATED) — revoked / expired / malformed
  - EMPTY       → 200 + пустой список (сужение списка iam для не-члена)

  Списки неоднородны по службам: сужаемые членством списки iam
  (`ServiceAccount.List ?accountId=…`) отдают не-члену 200 и пустую страницу, а
  `vpc.NetworkService.List ?projectId=…` закрыт `viewer` на запрошенном проекте:
  без него — 403 (запрет перечисления чужого проекта, CWE-862), а не пустая
  страница.

ЧТО ЗДЕСЬ ДЕЛАЮТ ШАГИ ПО `iam`. Десять кейсов адресуют маршруты службы доступа
(учётная запись, выдачи прав, служебная учётка, её ключи, роли). Их предмет — тот
же субъект модели 5/6 на РУБЕЖЕ ПРАВ КРАЯ: отказ производит проверка прав края по
каталогу до обращения к владельцу ресурса, и матрица без них не отличала бы
«у субъекта нет прав на iam» от «рубеж пропускает всё, кроме vpc». Сущностей
службы (форму учётной записи, роли, выдачи) модуль не утверждает ни одним шагом —
только отказ субъекту без права.

Предусловия — посев `tests/authz-fixtures/setup.sh` (делегирует
`prodseed_all.py`): `jwtSAA` (учётка A с выдачей vpc-editor на project-A1),
`jwtSANoGrant` (учётка без выдач), `apiTokenValid` / `apiTokenRevoked` /
`apiTokenMalformed` / `apiTokenExpired`, `svaAId` / `svaNoGrantId`, `seedNetworkA1Id` /
`seedNetworkB1Id`, `accountAId`, `projectA1Id` / `projectA2Id` / `projectB1Id`,
`userAAAId`.

Техники: таблица решений (субъект × проект/аккаунт × глагол), классы
эквивалентности предъявителя (действующий / отозванный / истёкший / битый),
угадывание ошибок (самоэскалация: выдать себе роль, выпустить себе ключ, завести
широкую роль).
"""

# ЧЬЁ ПОВЕДЕНИЕ УТВЕРЖДАЕТ ЭТОТ МОДУЛЬ (e2e-flow.md §7а воркспейса). Домены по
# REST-путям — `vpc` и `iam`: связка. Предмет — доступ НЕ-человеческого субъекта к
# ресурсам vpc через край; утверждается поведение vpc и рубежа прав края под
# выдачей службы, а не сама выдача.

CASES = []

# System role ids — source of truth = migration 0008_role_catalog_kac122.sql
# (deterministic `rol` + substr(md5(<name>),1,17); the legacy
# `rol00000000000000<tail>` ids from migration 0001 are DELETEd by 0008). A
# binding with a non-existent role_id fails the worker FAILED_PRECONDITION —
#
ROLE_ADMIN = "rol21232f297a57a5a74"   # md5('admin')[:17] — global super-admin

# ---------------------------------------------------------------------------
# Субъекты моделей 5-6.
#   code  — короткий идентификатор (в case-id)
#   label — человекочитаемое имя
#   auth  — имя env-var с токеном (Step.auth)
# ---------------------------------------------------------------------------

SA_GRANTED = ("SAA", "service-account-A (vpc-editor on project-A1)", "jwtSAA")
SA_NOGRANT = ("SANG", "service-account-no-grant", "jwtSANoGrant")
API_VALID = ("APIV", "api-token-valid (in-scope vpc.* on project-A1)", "apiTokenValid")
API_REVOKED = ("APIR", "api-token-revoked", "apiTokenRevoked")
API_MALFORMED = ("APIM", "api-token-malformed", "apiTokenMalformed")


# ---------------------------------------------------------------------------
# Assert-блоки (parity с authz-deny.py — те же decision-классы).
# ---------------------------------------------------------------------------

def deny_asserts(case_id):
    return [
        f"pm.test('[{case_id}] DENY: status 403', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(403));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] DENY: grpc code 7 (PERMISSION_DENIED)', () => pm.expect(j && j.code, JSON.stringify(j)).to.equal(7));",
        f"pm.test('[{case_id}] DENY: message contains permission denied', () => pm.expect((j && j.message || '').toLowerCase()).to.contain('permission denied'));",
    ]


def _is_single_resource_get(path):
    # Single-resource Get: last path segment is a concrete id (a `{{var}}` or a
    # literal resource id) with no ?query. A List (?projectId=…) is not a single
    # read and a denied List stays PermissionDenied (403), not hidden as 404.
    if "?" in path:
        return False
    last = path.rstrip("/").rsplit("/", 1)[-1]
    if last.startswith("{{") and last.endswith("}}"):
        return True
    return len(last) >= 20 and last[:3].isalpha() and last[3:].isalnum()


def read_deny_asserts(case_id):
    # BUG-2 hide-existence: a denied single-resource read (Get) on a verb-bearing
    # resource is surfaced as NotFound (404 / code 5), never PermissionDenied — no
    # enumeration / existence leak. Token-scope and cross-account read denials are
    # all FGA read-denies → 404.
    return [
        f"pm.test('[{case_id}] READ-DENY: status 404 (hide existence)', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(404));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] READ-DENY: grpc code 5 (NOT_FOUND, not 7)', () => pm.expect(j && j.code, JSON.stringify(j)).to.equal(5));",
        f"pm.test('[{case_id}] READ-DENY: no deny_reasons leak', () => pm.expect(JSON.stringify(j || {{}}).toLowerCase()).to.not.include('deny_reasons'));",
    ]


# ALLOW-ПОЛОСЫ: ИСХОД ОДИН, УСТАНОВЛЕННЫЙ, УТВЕРЖДАЕТСЯ ПАРОЙ.
#
# Здесь стояло «код не 403» и «код не 16» на все пять ALLOW-позиций. Отрицание
# проходит на любом другом ответе — на отказе валидации, на 500, на 503, — то есть
# не отличает исправную систему ни от одной поломки, кроме подписанной 403. Заодно
# шаг, у которого все утверждения о статусе отрицательные, читается гейтами дерева
# как ПРОБА ОТКАЗА и выпадает из их рассмотрения. verifies #668.
#
# Полосы выбираются ПО ФОРМЕ ЗАПРОСА (та же раскладка, что в cases/authz-deny.py):
#   read — `GET /res/{id}` sync → 200 + `id` ответа равен запрошенному;
#   list — `GET /res?scope=` sync → 200 + конверт выдачи (только объявленные поля);
#   op   — мутация async → 200 + конверт Operation. Эта суита мутирует ресурс vpc,
#          поэтому префикс идентификатора операции — `enp` (`ids.PrefixOperationVPC`),
#          а не iam-шный `iop`.
_VPC_OPERATION_PREFIX = "enp"


def _allow_id_expr(path):
    last = path.rstrip("/").rsplit("/", 1)[-1]
    if last.startswith("{{") and last.endswith("}}"):
        return f"pm.environment.get('{last[2:-2]}')"
    return f"'{last}'"


def _allow_list_key(path):
    return path.split("?")[0].rstrip("/").rsplit("/", 1)[-1]


def allow_asserts(case_id, method, path):
    """ALLOW: субъект с правом получает ИСХОД, а не «не отказ»."""
    if method == "GET" and _is_single_resource_get(path):
        return [
            f"pm.test('[{case_id}] ALLOW: HTTP 200 (чтение разрешено)', () => "
            "pm.expect(pm.response.code, pm.response.text()).to.equal(200));",
            "let _j; try { _j = pm.response.json(); } catch(e) { _j = null; }",
            f"pm.test('[{case_id}] ALLOW: ответ — запрошенный ресурс, а не конверт отказа', () => {{",
            "  pm.expect(_j, 'тело не разобралось как JSON: ' + pm.response.text()).to.be.an('object');",
            f"  pm.expect(_j.id, JSON.stringify(_j)).to.equal({_allow_id_expr(path)});",
            "});",
        ]
    if method == "GET":
        key = _allow_list_key(path)
        return [
            f"pm.test('[{case_id}] ALLOW: HTTP 200 (перечисление разрешено)', () => "
            "pm.expect(pm.response.code, pm.response.text()).to.equal(200));",
            "let _j; try { _j = pm.response.json(); } catch(e) { _j = null; }",
            f"pm.test('[{case_id}] ALLOW: конверт выдачи, а не конверт отказа', () => {{",
            "  pm.expect(_j, 'тело не разобралось как JSON: ' + pm.response.text()).to.be.an('object');",
            f"  pm.expect(Object.keys(_j).filter(k => ['{key}', 'nextPageToken'].indexOf(k) < 0),",
            "    'посторонний ключ в конверте выдачи: ' + pm.response.text()).to.eql([]);",
            f"  pm.expect(_j['{key}'] === undefined || Array.isArray(_j['{key}']),",
            f"    'поле {key} присутствует и не является массивом: ' + pm.response.text()).to.equal(true);",
            "});",
        ]
    # Образец собирается ЗДЕСЬ и целиком уезжает в проверку `js_regex_src`:
    # между косыми чертами стоит КОД, а не текст, и негодный образец сломал бы не
    # утверждение, а СИНТАКСИС порождаемого файла — там, где автор значения его не
    # увидит (#1209). Проверяется весь литерал, а не подставляемый кусок: кусок,
    # безупречный сам по себе, способен сменить смысл соседям (`a|b` в `^a|b…$`).
    op_id_pattern = f"^{_VPC_OPERATION_PREFIX}[a-z0-9]+$"
    return [
        f"pm.test('[{case_id}] ALLOW: HTTP 200 (мутация принята)', () => "
        "pm.expect(pm.response.code, pm.response.text()).to.equal(200));",
        "let _j; try { _j = pm.response.json(); } catch(e) { _j = null; }",
        f"pm.test('[{case_id}] ALLOW: конверт Operation', () => {{",
        f"  pm.expect(_j && _j.id, 'operation.id: ' + pm.response.text()).to.match(/{js_regex_src(op_id_pattern, where='vpc/authz-sa-apitoken/allow_asserts/operation-id')}/);",
        "  pm.expect(_j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
    ]


def unauth_asserts(case_id):
    return [
        f"pm.test('[{case_id}] UNAUTH: status 401', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(401));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] UNAUTH: grpc code 16 (UNAUTHENTICATED)', () => pm.expect(j && j.code, JSON.stringify(j)).to.equal(16));",
    ]


def empty_asserts(case_id, list_key):
    return [
        f"pm.test('[{case_id}] EMPTY: status 200', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(200));",
        "const body = pm.response.json();",
        f"pm.test('[{case_id}] EMPTY: zero {list_key} (scope-filter)', () => pm.expect((body && body.{list_key} || []).length).to.equal(0));",
    ]


def emit(case_id, title, decision, method, path, body, subject, list_key="networks"):
    """Разворачивает один кейс. decision ∈ {DENY, ALLOW, UNAUTH, EMPTY}."""
    code, label, auth = subject
    if decision == "DENY":
        # BUG-2 hide-existence: a denied single-resource read (Get) → 404/code-5,
        # not 403. Mutations (Create/Delete) and List stay 403/code-7.
        asserts = read_deny_asserts(case_id) if (method == "GET" and _is_single_resource_get(path)) else deny_asserts(case_id)
        cls = ["AUTHZ", "NEG"]
    elif decision == "ALLOW":
        asserts = allow_asserts(case_id, method, path)
        cls = ["AUTHZ", "POS"]
    elif decision == "UNAUTH":
        asserts = unauth_asserts(case_id)
        cls = ["AUTHZ", "NEG", "AUTHN"]
    elif decision == "EMPTY":
        asserts = empty_asserts(case_id, list_key)
        cls = ["AUTHZ", "SCOPE"]
    else:
        raise ValueError(f"unknown decision {decision} for {case_id}")
    CASES.append(Case(
        id=case_id,
        title=f"[{decision}] {title} as {label}",
        classes=cls,
        priority="P1",
        steps=[Step(name=method.lower(), method=method, path=path,
                    body=body, auth=auth, test_script=asserts)],
    ))


# ===========================================================================
# МОДЕЛЬ 5 — Service Account (предъявитель служебной учётки, выданный обменом ключа).
# ===========================================================================
#
# SA-A имеет AccessBinding(vpc-editor) на project-A1 (выдан в setup-фазе).
# SA-A токен получен обменом ключа учётки у издателя платформы → claims содержат
# kaname_principal_type=service_account, sub=<svaAId>.
#
# Decision table (SA-A — grant=vpc-editor@project-A1):
#   resource в project-A1 (own)         → ALLOW  (binding покрывает)
#   resource в project-A2 / project-B1  → DENY   (нет binding; per-resource Get/Create)
#   account-A / account-B уровень       → DENY   (project-scoped grant ≠ account)
#   list ?projectId=A1                  → ALLOW
#   list ?projectId=A2 / ?projectId=B1  → DENY   (vpc.NetworkService.List is a
#                                          project-viewer-GATED List RPC: the caller
#                                          must hold `viewer` on `project:<projectId>`
#                                          to enumerate its networks; no grant on the
#                                          queried project → 403 PermissionDenied, NOT a
#                                          200-empty scope-filter. This is a deliberate
#                                          anti-cross-project-enumeration gate owned by
#                                          kacho-vpc (permission_map.go NetworkService/List
#                                          required_relation=viewer@project; locked by the
#                                          CWE-862 regression test
#                                          permission_map_networklist_test.go). Only a
#                                          caller WITH project-viewer but no per-network
#                                          grant sees 200-empty — that is not this case.)
#   self-modify / escalate              → DENY   (SA не может grant'ить себе роли)
# ---------------------------------------------------------------------------

# SA-A-1: SA-A с grant → 200 на разрешенный ресурс в его Project (VPC Network GET).
emit("AUTHZ-SA-NET-GT-A1", "Get seed-network in project-A1 (granted resource)",
     "ALLOW", "GET", "/vpc/v1/networks/{{seedNetworkA1Id}}", None, SA_GRANTED)

# SA-A-2: SA-A list networks в своем Project → ALLOW.
emit("AUTHZ-SA-NET-LS-A1", "List networks ?projectId=A1 (own project)",
     "ALLOW", "GET", "/vpc/v1/networks?projectId={{projectA1Id}}", None, SA_GRANTED)

# SA-A-3: SA-A create network в своем Project → ALLOW (не PermissionDenied;
#         реальный код может быть 200-async — happy-path валидируют vpc-тесты).
emit("AUTHZ-SA-NET-CR-A1", "Create network in project-A1 (own project)",
     "ALLOW", "POST", "/vpc/v1/networks",
     {"projectId": "{{projectA1Id}}", "name": "authz-sa-net-{{runId}}"}, SA_GRANTED)

# SA-A-4: SA-A list networks в cross-project того же account (A2, без binding).
# vpc.NetworkService.List is project-viewer-GATED: the caller must hold `viewer`
# on `project:A2` to enumerate its networks. SA-A has viewer only on project-A1,
# so listing project-A2 → 403 PermissionDenied (anti-cross-project-enumeration,
# CWE-862; owned by kacho-vpc permission_map, not a 200-empty scope-filter). Same
# semantics as B1 below.
emit("AUTHZ-SA-NET-LS-A2-DENY", "List networks ?projectId=A2 (cross-project, no project-viewer) → 403 gated List",
     "DENY", "GET", "/vpc/v1/networks?projectId={{projectA2Id}}", None, SA_GRANTED)

# SA-A-5: SA-A без grant на ДРУГОЙ Project (B1, cross-account) → DENY.
emit("AUTHZ-SA-NET-GT-B1", "Get seed-network in project-B1 (cross-account, no grant)",
     "DENY", "GET", "/vpc/v1/networks/{{seedNetworkB1Id}}", None, SA_GRANTED)

emit("AUTHZ-SA-NET-CR-B1", "Create network in project-B1 (cross-account, no grant)",
     "DENY", "POST", "/vpc/v1/networks",
     {"projectId": "{{projectB1Id}}", "name": "authz-sa-net-{{runId}}"}, SA_GRANTED)

# SA-A-6: SA-A list networks в cross-account project (B1) — no project-viewer on B1
#         → 403 PermissionDenied (project-viewer-gated List, anti-enumeration),
#         НЕ список чужих networks и НЕ distinguishing 200-empty.
emit("AUTHZ-SA-NET-LS-B1-DENY", "List networks ?projectId=B1 (cross-account, no project-viewer) → 403 gated List",
     "DENY", "GET", "/vpc/v1/networks?projectId={{projectB1Id}}", None, SA_GRANTED)

# SA-A-7: project-scoped grant не дает account-уровневых прав → Get account-A DENY.
emit("AUTHZ-SA-ACCT-GT-A", "Get account-A (project-scoped grant ≠ account-level)",
     "DENY", "GET", "/iam/v1/accounts/{{accountAId}}", None, SA_GRANTED)

# SA-A-8: SA-A пытается изменить account-A → DENY.
emit("AUTHZ-SA-ACCT-UP-A", "Update account-A (no account-level grant)",
     "DENY", "PATCH", "/iam/v1/accounts/{{accountAId}}",
     {"name": "x", "updateMask": "name"}, SA_GRANTED)

# --- Negative: SA self-modify / privilege escalation ---

# SA-A-9: SA-A пытается выдать СЕБЕ iam.admin на account-A (escalation) → DENY.
emit("AUTHZ-SA-ESC-SELF-ADMIN", "Self-grant iam.admin on account-A (escalation)",
     "DENY", "POST", "/iam/v1/accessBindings",
     {"subjectType": "service_account", "subjectId": "{{svaAId}}",
      "roleId": ROLE_ADMIN,
      "scopeType":"iam.account","scopeId":"{{accountAId}}","target":{"allInScope":{}}}, SA_GRANTED)

# SA-A-10: SA-A пытается выдать себе vpc-admin на project-B1 (cross-account escalation).
emit("AUTHZ-SA-ESC-SELF-VPC-B1", "Self-grant vpc-admin on project-B1 (cross-account escalation)",
     "DENY", "POST", "/iam/v1/accessBindings",
     {"subjectType": "service_account", "subjectId": "{{svaAId}}",
      "roleId": ROLE_ADMIN,
      "scopeType":"iam.project","scopeId":"{{projectB1Id}}","target":{"allInScope":{}}}, SA_GRANTED)

# SA-A-11: SA-A пытается self-modify собственную SA-row (поднять привилегии
#          через rename / labels) — у SA нет iam-write на свой Account → DENY.
emit("AUTHZ-SA-ESC-SELF-MODIFY", "Self-modify own ServiceAccount row (escalation prep)",
     "DENY", "PATCH", "/iam/v1/serviceAccounts/{{svaAId}}",
     {"description": "escalated", "updateMask": "description"}, SA_GRANTED)

# SA-A-12: SA-A пытается выпустить себе НОВЫЙ SA-key (расширить доступы) → DENY.
#          SAKeyService.Issue требует iam-write на Account SA — у SA-A нет.
emit("AUTHZ-SA-ESC-ISSUE-KEY", "Issue new SA-key for self (escalation prep)",
     "DENY", "POST", "/iam/v1/serviceAccounts/{{svaAId}}/keys",
     {"serviceAccountId": "{{svaAId}}", "description": "self-issued",
      "createdByUserId": "{{userAAAId}}"}, SA_GRANTED)

# SA-A-13: SA-A пытается создать custom Role с broad rules → DENY
#          (cluster/account role-mutate недоступен SA-субъекту).
# RBAC rules model: authored field is `rules`. Module/resource
# wildcard `*` is system-only, so the legacy super-shaped iam.*.* / vpc.*.*
# intent is expressed as concrete resources with verb-wildcard `verbs:["*"]` —
# still a broad/escalation-shaped role. authz (SA cannot role-mutate) DENYs first.
emit("AUTHZ-SA-ESC-CUSTOM-ROLE", "Create custom Role with broad iam/vpc rules (escalation prep)",
     "DENY", "POST", "/iam/v1/roles",
     {"accountId": "{{accountAId}}", "name": "sa-hack-role-{{runId}}",
      "rules": [
          {"module": "iam", "resources": ["user", "role", "account"], "verbs": ["*"]},
          {"module": "vpc", "resources": ["network", "subnet"], "verbs": ["*"]},
      ]}, SA_GRANTED)

# --- SA without any grant (SANG) — должен быть полностью DENY ---

# SA-NG-1: SA без grant'ов на own-project ресурс → DENY.
emit("AUTHZ-SANG-NET-GT-A1", "Get seed-network in project-A1 (no grants at all)",
     "DENY", "GET", "/vpc/v1/networks/{{seedNetworkA1Id}}", None, SA_NOGRANT)

# SA-NG-2: SA без grant'ов — list networks project-A1: no project-viewer → 403
#          (project-viewer-gated List; a no-grant SA cannot even enumerate the project).
emit("AUTHZ-SANG-NET-LS-A1-DENY", "List networks ?projectId=A1 (no grants, no project-viewer) → 403 gated List",
     "DENY", "GET", "/vpc/v1/networks?projectId={{projectA1Id}}", None, SA_NOGRANT)

# SA-NG-3: SA без grant'ов — create network → DENY.
emit("AUTHZ-SANG-NET-CR-A1", "Create network in project-A1 (no grants)",
     "DENY", "POST", "/vpc/v1/networks",
     {"projectId": "{{projectA1Id}}", "name": "authz-sang-net-{{runId}}"}, SA_NOGRANT)

# SA-NG-4: SA без grant'ов — list serviceAccounts ?accountId → EMPTY.
emit("AUTHZ-SANG-SA-LS-A-EMPTY", "List serviceAccounts ?accountId=A (no grants) → scope-filter empty",
     "EMPTY", "GET", "/iam/v1/serviceAccounts?accountId={{accountAId}}", None, SA_NOGRANT,
     list_key="serviceAccounts")


# ===========================================================================
# МОДЕЛЬ 6 — API token (static long-lived; valid / out-of-scope / revoked /
#            expired / malformed).
# ===========================================================================
#
# apiTokenValid — статический API-token, scope = vpc.* on project-A1.
#   in-scope ресурс  → ALLOW
#   out-of-scope     → DENY (token валиден, но scope не покрывает)
# apiTokenRevoked   — отозван через SAKeyService.Revoke → 401 UNAUTHENTICATED
# apiTokenMalformed — синтаксически битый → 401 UNAUTHENTICATED
# (expired-вариант — apiTokenExpired, минтится setup'ом с exp в прошлом.)
# ---------------------------------------------------------------------------

# API-1: API-token valid + in-scope → 200 OK (не PermissionDenied/Unauthenticated).
emit("AUTHZ-APITOK-NET-GT-A1", "Get seed-network in project-A1 (valid, in-scope token)",
     "ALLOW", "GET", "/vpc/v1/networks/{{seedNetworkA1Id}}", None, API_VALID)

# API-2: API-token valid + in-scope list → ALLOW.
emit("AUTHZ-APITOK-NET-LS-A1", "List networks ?projectId=A1 (valid, in-scope token)",
     "ALLOW", "GET", "/vpc/v1/networks?projectId={{projectA1Id}}", None, API_VALID)

# API-3: API-token valid но out-of-scope ресурс (project-B1) → DENY.
emit("AUTHZ-APITOK-NET-GT-B1", "Get seed-network in project-B1 (valid token, out-of-scope)",
     "DENY", "GET", "/vpc/v1/networks/{{seedNetworkB1Id}}", None, API_VALID)

# API-4: API-token valid но out-of-scope domain — IAM account (scope только vpc.*) → DENY.
emit("AUTHZ-APITOK-ACCT-GT-A", "Get account-A (valid token, scope=vpc.* only)",
     "DENY", "GET", "/iam/v1/accounts/{{accountAId}}", None, API_VALID)

# API-5: API-token valid but out-of-scope — list ?projectId=B1: token scope is
#        vpc.* on project-A1, no viewer on project-B1 → 403 PermissionDenied
#        (project-viewer-gated List, anti-cross-project-enumeration).
emit("AUTHZ-APITOK-NET-LS-B1-DENY", "List networks ?projectId=B1 (out-of-scope, no project-viewer) → 403 gated List",
     "DENY", "GET", "/vpc/v1/networks?projectId={{projectB1Id}}", None, API_VALID)

# API-6: API-token revoked → 401 UNAUTHENTICATED (на in-scope ресурсе — revoke
#        бьет authn-слой раньше authz).
emit("AUTHZ-APITOK-REVOKED-GT-A1", "Get seed-network in project-A1 (revoked token)",
     "UNAUTH", "GET", "/vpc/v1/networks/{{seedNetworkA1Id}}", None, API_REVOKED)

# API-7: API-token revoked — list тоже 401 (не EMPTY: revoke = authn-fail).
emit("AUTHZ-APITOK-REVOKED-LS-A1", "List networks ?projectId=A1 (revoked token)",
     "UNAUTH", "GET", "/vpc/v1/networks?projectId={{projectA1Id}}", None, API_REVOKED)

# API-8: API-token malformed (битый JWS) → 401 UNAUTHENTICATED.
emit("AUTHZ-APITOK-MALFORMED-GT-A1", "Get seed-network in project-A1 (malformed token)",
     "UNAUTH", "GET", "/vpc/v1/networks/{{seedNetworkA1Id}}", None, API_MALFORMED)

# API-9: API-token expired → 401 UNAUTHENTICATED (exp в прошлом).
emit("AUTHZ-APITOK-EXPIRED-GT-A1", "Get seed-network in project-A1 (expired token)",
     "UNAUTH", "GET", "/vpc/v1/networks/{{seedNetworkA1Id}}", None,
     ("APIE", "api-token-expired", "apiTokenExpired"))

# API-10: API-token revoked пытается мутировать → 401 (authn-fail раньше authz).
emit("AUTHZ-APITOK-REVOKED-CR", "Create network with revoked token",
     "UNAUTH", "POST", "/vpc/v1/networks",
     {"projectId": "{{projectA1Id}}", "name": "authz-rev-net-{{runId}}"}, API_REVOKED)

# API-11: malformed token пытается мутировать → 401.
emit("AUTHZ-APITOK-MALFORMED-CR", "Create network with malformed token",
     "UNAUTH", "POST", "/vpc/v1/networks",
     {"projectId": "{{projectA1Id}}", "name": "authz-mal-net-{{runId}}"}, API_MALFORMED)

# --- Negative: API-token escalation ---

# API-12: valid API-token пытается выдать себе расширенный grant → DENY
#         (token scope vpc.* не включает iam.accessBinding.create).
emit("AUTHZ-APITOK-ESC-SELF-ADMIN", "Self-grant iam.admin via valid API token (escalation)",
     "DENY", "POST", "/iam/v1/accessBindings",
     {"subjectType": "service_account", "subjectId": "{{svaAId}}",
      "roleId": ROLE_ADMIN,
      "scopeType":"iam.account","scopeId":"{{accountAId}}","target":{"allInScope":{}}}, API_VALID)
