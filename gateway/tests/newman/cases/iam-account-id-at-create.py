# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set iam-account-id-at-create — ИДЕНТИФИКАТОР АККАУНТА, УКАЗАННЫЙ ПРИ СОЗДАНИИ, ДОХОДИТ ЧЕРЕЗ КРАЙ.

Сторона платформы (kacho#2984) приёмки службы доступа
`account-id-may-be-supplied-at-create.md` (PRO-Robotech/kaname#549, волна-8
#548 эпика #357), стадия S2: сценарии AID-K1 и AID-K2 уровня K — «снаружи
через край платформы».

ПРЕДМЕТ — ШОВ КРАЯ «ТЕЛО → MESSAGE ЗАПРОСА». Край разбирает тело
`POST /iam/v1/accounts` стабами ПИНЕННОГО модуля службы и неизвестный ключ
отбрасывает (`DiscardUnknown`, `gateway/internal/restmux/strict_enum.go`). При
пине без поля `id` указанный идентификатор молча пропадает: служба получает
пустое поле, чеканит свой, и клиент видит `200` с чужим идентификатором. Отказа
нет ни на одном шаге — поэтому утверждение стоит на ЗНАЧЕНИИ
`metadata.accountId`, а не на статусе. На прежнем пине (`b90430b42272`)
AID-K1 краснеет ровно этим утверждением; на пине `1b9067128e7d` и выше —
зеленеет.

Четыре кейса, каждый в паре с законным близнецом, отличающимся ОДНИМ фактом:

  * `IAM-ACC-ID-K1` — администратор облака (человек) указывает годный X:
    `metadata.accountId = X`, опрос до `done` даёт `response.id = X`, чтение
    `GET /iam/v1/accounts/X` отвечает `200` с `id = X`. Близнец формы —
    `IAM-ACC-ID-K1-FORM` (тот же человек, негодная форма).
  * `IAM-ACC-ID-K1-FORM` — тот же человек, форма вне генератора: `400` / `3`,
    `invalid account id 'acc7M3K9Q2X5V8B4N6T1'`, нарушение поля `id`,
    синхронно, операции нет. Форма судится раньше права (AID-07 службы).
  * `IAM-ACC-ID-K2` — человек без роли на кластере указывает годный X:
    `403` / `7`, `permission denied` — отказ производит СЛУЖБА, а не край:
    `Create` в каталоге прав края `<exempt>` (SELF_SERVICE). Аккаунта X после
    отказа нет. Близнец права — `IAM-ACC-ID-K1` (тот же запрос под
    администратором); близнец поля — `IAM-ACC-ID-K2-GENERATED`.
  * `IAM-ACC-ID-K2-GENERATED` — тот же человек без `id`: операция завершается
    `response`, идентификатор чеканный (форма генератора) и не равен X
    соседнего кейса — ветка генератора не изменилась.

ЛЮДИ — ИЗ ПОСЕВА, СВОИ, А НЕ МАТРИЧНЫЕ (`tests/authz-fixtures/prodseed_matrix.py`):
`jwtCloudAdminHuman` — человек, которому посев выдал `system_admin` глаголом
`InternalClusterService.GrantAdmin` и дождался, что выдача видна модели;
`jwtPlainHuman` — человек без роли на кластере. Машинный администратор
(`jwtBootstrap`) указывать `id` не вправе по роду принципала (Р3 приёмки
службы), поэтому он субъектом здесь не служит. Каждое заведение списывает
место окна темпа заведений личности, и отвергнутая попытка места не списывает —
у каждого человека набор заводит РОВНО один аккаунт.

X выводится из `runId` прогона в алфавит генератора: 17 знаков крокфорда с
префиксом `acc`, свой у каждого кейса.

Техники: классы эквивалентности (`id` указан годный / негодный / не указан),
таблица решений «форма × право вызывающего», угадывание ошибок (поле, молча
отброшенное краем).

Чего модуль НЕ утверждает: запись реестра выданных идентификаторов, повтор
занятого X, отказ машинного администратора — это сущности службы, их дом —
её репозиторий (e2e-flow.md §7а воркспейса), сценарии AID-01…AID-24.
"""

# ЧЬЁ ПОВЕДЕНИЕ УТВЕРЖДАЕТ ЭТОТ МОДУЛЬ (e2e-flow.md §7а, решение владельца 2026-09-12).
ASSERTS_DOMAIN = "gateway"
ASSERTS_REASON = (
    "предмет — шов края «тело → message запроса»: край разбирает тело Create аккаунта "
    "стабами пиненного модуля службы и неизвестный ключ отбрасывает, поэтому указанный "
    "`id` доходит до службы только при пине с этим полем; маршрут `/iam/v1/accounts` — "
    "носитель. Отказ права (403) производит служба, а не край: в каталоге прав края "
    "Create аккаунта `<exempt>`; модуль утверждает, что край его не подменяет."
)

CASES = []

ACCOUNTS = "/iam/v1/accounts"

# Негодная форма — заглавные знаки (генератор чеканит строчные); тот же литерал,
# что у AID-05/AID-07 приёмки службы.
MALFORMED_ACCOUNT_ID = "acc7M3K9Q2X5V8B4N6T1"

_ACC_ID_RE = "/^acc[0-9a-hjkmnp-tv-z]{17}$/"

ADMIN_GUARD = require_env_slot("jwtCloudAdminHuman", "human cloud admin seeded by prodseed_matrix.py")
PLAIN_GUARD = require_env_slot("jwtPlainHuman", "human without a cluster role seeded by prodseed_matrix.py")


def _derive_id(var, salt):
    """Pre-request: X — `acc` + 17 знаков алфавита генератора, выведенных из `runId`."""
    return [
        "// X — свежий идентификатор формы генератора, выведенный из runId прогона.",
        "(function () {",
        "  const A = '0123456789abcdefghjkmnpqrstvwxyz';",
        f"  const src = String(pm.environment.get('runId') || '') + {js_str(':' + salt)};",
        "  let h = 2166136261 >>> 0; let out = '';",
        "  for (let i = 0; i < 17; i++) {",
        "    const s = src + ':' + i;",
        "    for (let k = 0; k < s.length; k++) { h ^= s.charCodeAt(k); h = Math.imul(h, 16777619) >>> 0; }",
        "    out += A[h % 32];",
        "  }",
        f"  pm.environment.set({js_str(var)}, 'acc' + out);",
        "})();",
    ]


def _poll(op_var, auth, name, extra=()):
    """Опрос операции под тем же человеком, что её завёл."""
    step = poll_operation(op_var=op_var, auth=auth, name=name)
    step.test_script = [*step.test_script, *extra]
    return step


def _cleanup(id_var, op_var, auth, name):
    """Уборка: удаление заведённого аккаунта и опрос его операции."""
    return [
        Step(
            name=f"cleanup-{name}",
            method="DELETE",
            path=f"{ACCOUNTS}/{{{{{id_var}}}}}",
            auth=auth,
            pre_script=[
                *ADMIN_GUARD,
                "// OPERATION guard (legal skip): создание отвергнуто — убирать нечего.",
                f"if (!pm.environment.get({js_str(id_var)})) {{ pm.execution.skipRequest(); }}",
            ],
            test_script=[
                *assert_status(200),
                *save_from_response("j.id", op_var),
            ],
        ),
        poll_operation(op_var=op_var, auth=auth, name=f"cleanup-{name}-poll"),
    ]


# ---------------------------------------------------------------------------
# IAM-ACC-ID-K1 — AID-K1: указанный идентификатор доходит через край до службы.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-ACC-ID-K1",
    title="AID-K1: POST /iam/v1/accounts with id=X by a human cloud admin through the edge → "
          "Operation with metadata.accountId = X; done gives response.id = X; GET /accounts/X → 200",
    classes=["CRUD", "CONF"],
    priority="P0",
    steps=[
        Step(
            name="create-with-id",
            method="POST",
            path=ACCOUNTS,
            auth="jwtCloudAdminHuman",
            pre_script=[*ADMIN_GUARD, *_derive_id("aidK1Id", "k1")],
            body={"id": "{{aidK1Id}}", "name": "aidk1-{{runId}}"},
            test_script=[
                *assert_status(200),
                *assert_iam_operation_envelope(),
                *save_from_response("j.id", "aidK1OpId"),
                # Уборка снимает то, что ДЕЙСТВИТЕЛЬНО заведено: при отброшенном поле
                # это чеканный идентификатор, а не X, и он не должен остаться жить.
                *save_from_response("j.metadata && j.metadata.accountId", "aidK1CreatedId"),
                "pm.test('metadata.accountId is the supplied X (the edge did not drop the field)', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j.metadata && j.metadata.accountId, JSON.stringify(j))"
                ".to.eql(pm.environment.get('aidK1Id'));",
                "});",
            ],
        ),
        _poll("aidK1OpId", "jwtCloudAdminHuman", "create-with-id-poll", extra=[
            "pm.test('done gives response.id = X', () => {",
            "  pm.expect(j.response && j.response.id, JSON.stringify(j)).to.eql(pm.environment.get('aidK1Id'));",
            "});",
        ]),
        Step(
            name="get-supplied",
            method="GET",
            path=f"{ACCOUNTS}/{{{{aidK1Id}}}}",
            auth="jwtCloudAdminHuman",
            test_script=[
                *assert_status(200),
                "pm.test('GET /accounts/X answers id = X', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j.id, JSON.stringify(j)).to.eql(pm.environment.get('aidK1Id'));",
                "});",
            ],
        ),
        *_cleanup("aidK1CreatedId", "aidK1DelOpId", "jwtCloudAdminHuman", "k1"),
    ],
))


# ---------------------------------------------------------------------------
# IAM-ACC-ID-K1-FORM — близнец формы: тот же человек, форма вне генератора.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-ACC-ID-K1-FORM",
    title="AID-K1 form twin: POST /iam/v1/accounts with a malformed id by the same human cloud admin → "
          "sync 400 INVALID_ARGUMENT (3), \"invalid account id 'acc7M3K9Q2X5V8B4N6T1'\", field id",
    classes=["VAL", "NEG"],
    priority="P0",
    steps=[
        Step(
            name="create-malformed-id",
            method="POST",
            path=ACCOUNTS,
            auth="jwtCloudAdminHuman",
            pre_script=[*ADMIN_GUARD],
            body={"id": MALFORMED_ACCOUNT_ID, "name": "aidk1f-{{runId}}"},
            test_script=[
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
                *assert_error_message_eql(f"invalid account id '{MALFORMED_ACCOUNT_ID}'"),
                *assert_field_violation("id"),
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# IAM-ACC-ID-K2 — AID-K2: не администратор с `id` получает отказ СЛУЖБЫ.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-ACC-ID-K2",
    title="AID-K2: POST /iam/v1/accounts with id=X by a human without a cluster role → "
          "403 PERMISSION_DENIED (7), \"permission denied\" from the service; account X does not exist",
    classes=["NEG", "AUTHZ", "SEC"],
    priority="P0",
    steps=[
        Step(
            name="create-with-id-denied",
            method="POST",
            path=ACCOUNTS,
            auth="jwtPlainHuman",
            pre_script=[*PLAIN_GUARD, *_derive_id("aidK2Id", "k2")],
            body={"id": "{{aidK2Id}}", "name": "aidk2-{{runId}}"},
            test_script=[
                *assert_status(403),
                *assert_grpc_code(7, "PERMISSION_DENIED"),
                *assert_error_message_eql("permission denied"),
            ],
        ),
        Step(
            name="get-denied-absent",
            method="GET",
            path=f"{ACCOUNTS}/{{{{aidK2Id}}}}",
            auth="jwtCloudAdminHuman",
            pre_script=[*ADMIN_GUARD],
            test_script=[
                *assert_status(404),
                *assert_grpc_code(5, "NOT_FOUND"),
                "pm.test('the refused X was not created', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j.message, JSON.stringify(j))"
                ".to.eql('Account ' + pm.environment.get('aidK2Id') + ' not found');",
                "});",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# IAM-ACC-ID-K2-GENERATED — близнец поля: тот же человек без `id` — генератор.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-ACC-ID-K2-GENERATED",
    title="AID-K2 field twin: POST /iam/v1/accounts without id by the same human → "
          "Operation done with response; the id is minted in the generator form",
    classes=["CRUD"],
    priority="P0",
    steps=[
        Step(
            name="create-without-id",
            method="POST",
            path=ACCOUNTS,
            auth="jwtPlainHuman",
            pre_script=[*PLAIN_GUARD, *_derive_id("aidK2Id", "k2")],
            body={"name": "aidk2g-{{runId}}"},
            test_script=[
                *assert_status(200),
                *assert_iam_operation_envelope(),
                *save_from_response("j.id", "aidK2gOpId"),
                *save_from_response("j.metadata && j.metadata.accountId", "aidK2gId"),
                "pm.test('metadata.accountId is minted in the generator form and is not X', () => {",
                "  const j = pm.response.json();",
                "  const got = j.metadata && j.metadata.accountId;",
                f"  pm.expect(got, JSON.stringify(j)).to.match({_ACC_ID_RE});",
                "  pm.expect(got, JSON.stringify(j)).to.not.eql(pm.environment.get('aidK2Id'));",
                "});",
            ],
        ),
        _poll("aidK2gOpId", "jwtPlainHuman", "create-without-id-poll", extra=[
            "pm.test('done gives response.id = the minted id', () => {",
            "  pm.expect(j.response && j.response.id, JSON.stringify(j)).to.eql(pm.environment.get('aidK2gId'));",
            "});",
        ]),
        # Уборку ведёт администратор облака, а не владелец: право владельца на новый
        # аккаунт приходит в хранилище прав асинхронно (`done` операции — запись
        # закоммичена, а не видна модели), а каскад администратора облака от
        # материализации не зависит.
        *_cleanup("aidK2gId", "aidK2gDelOpId", "jwtCloudAdminHuman", "k2g"),
    ],
))
