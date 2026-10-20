# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set iam-edge-session-refusals — ОТКАЗЫ КРАЯ ПО СЕССИИ: сброс способов входа и адрес.

Сторона платформы (kacho#3334), сквозные пробы края второго эшелона (`00-kacho-core`
`ban12-second-echelon`) по четырём позициям приёмок:

  * LMR-01 — сброс ключей доступа администратором облака (приёмка службы
    `cloud-administrator-resets-login-methods`, LMR-01): обе прежние сессии цели на любом
    пути получают `401`, `code` 16, печенье гаснет; близнец — та же сессия ДО сброса.
  * LMR-03 — тот же глагол: кривой идентификатор отвечает края синхронно и побайтово
    одинаково администратору облака и постороннему; посторонний на правильный
    идентификатор получает `403` с `reason` `AUTHZ_DENIED`.
  * Ф12-30 — то же для `:resetSecondFactor` (приёмка службы
    `second-factor-totp-and-recovery-codes`, Р10, Ф12-30).
  * EV-14 — смена пароля сессией без подтверждённого адреса: `403`, тело — значение
    `addressRefusalBody` края побайтово (`gateway/internal/middleware/address_refusal.go`);
    шаг, названный текстом отказа (`verify-email/confirm`), снимает отказ: новый носитель
    получает `200` (приёмка службы `access-beyond-login-needs-a-verified-address`, EV-14).

ЛЮДИ — ИЗ ПОСЕВА (`tests/authz-fixtures/seed_edge_refusals.py`, место заведения —
`prodseed_matrix.py`): случай людей не заводит (гейт `suitepeopleenrollment_test.go`).
Слоты — носители сессии `kaname_session`:

  edgeAdminSession2 · администратор облака, сессия уровня «2»
  edgeTargetUserId, edgeTargetSession1, edgeTargetSession2 · цель сброса: один ключ,
      две живые сессии; LMR-01 её РАСХОДУЕТ — повторный прогон на том же посеве даёт
      другой исход, посев один на прогон
  edgeOutsiderSession2 · посторонний, сессия «2» (отказ по праву, не по уровню)
  edgeBystanderUserId · прохожий: правильный идентификатор для постороннего; не цель
  edgeUnverifiedSession, edgeUnverifiedConfirmCode, edgeSeedPassword, edgeSeedNewPassword ·
      человек Ф-н и код его письма; EV-14 расходует код

ПЕЧЕНЬЯ НЕСУТСЯ ЯВНО: у каждого шага банка печений прогонщика снята (`explicit_cookies`),
`Cookie` кладёт пред-скрипт. Общая банка уносила бы печенье одного человека в запрос
другого — и кейс судил бы не того, кого назвал (kacho#3230).

Техники: классы эквивалентности (форма идентификатора × держатель отношения × уровень
сессии), близнец по одному факту (до / после сброса; кривой / правильный идентификатор;
шаг подтверждения не исполнен / исполнен), побайтовое равенство отказов (оракул
существования не открывается), угадывание ошибок (глагол гасит не все сессии либо не
все пути; край отвечает своим текстом вместо значения службы).

Чего модуль НЕ утверждает: события аудита, отсечку, пароль и фактор цели (уровень службы
в её репозитории — `tests/authz-fixtures` не читает хранилища), вход снятым ключом.
"""

# ЧЬЁ ПОВЕДЕНИЕ УТВЕРЖДАЕТ ЭТОТ МОДУЛЬ (e2e-flow.md §7а, решение владельца 2026-09-12).
ASSERTS_DOMAIN = "gateway"
ASSERTS_REASON = (
    "предмет — решения КРАЯ по сессии: проверка формы идентификатора раньше решения по "
    "каталогу прав (`invalid resource id`), отказ по праву (`AUTHZ_DENIED`), погашение "
    "носителя на отказе по отсечке (`401`, печенье снято) и отказ адреса на смене пароля, "
    "значение которого край произносит своим текстом (`addressRefusalBody`); исход самого "
    "сброса производит служба, и модуль утверждает его только как причину изменения "
    "следующего ответа края."
)

CASES = []

OP_PREFIX = "iop"

RESET_KEYS = "resetAccessKeys"
RESET_2F = "resetSecondFactor"

# Идентификатор вне формы идентификаторов продукта: ни префикса, ни кодировки.
BAD_ID = "not-an-id"
# Тело отказа края — `google.rpc.Status` без `details`; край пишет его кодировщиком JSON,
# поэтому за закрывающей скобкой стоит ровно один перевод строки (измерено пробой стенда:
# `Content-Length` на байт больше самого JSON). Он — часть значения.
BAD_ID_BODY = '{"code":3,"message":"invalid resource id \'' + BAD_ID + '\'"}\n'

# Значение отказа адреса — побайтово `addressRefusalBody` края
# (`gateway/internal/middleware/address_refusal.go`: `addressRefusalText`, `addressRefusalReason`,
# `addressRefusalDomain`; порядок ключей и отсутствие `metadata` — часть значения).
ADDRESS_REFUSAL_BODY = (
    '{"code":7,"message":"email address is not verified: confirm it with the code from the letter '
    '(POST /iam/v1/auth/verify-email/confirm)","details":[{"@type":"type.googleapis.com/'
    'google.rpc.ErrorInfo","reason":"EMAIL_NOT_VERIFIED","domain":"iam.kaname.cloud"}]}'
)

CSRF = "/iam/v1/auth/csrf"
PASSWORD = "/iam/v1/auth/password"
VERIFY_CONFIRM = "/iam/v1/auth/verify-email/confirm"

ADMIN = "edgeAdminSession2"
TARGET = "edgeTargetUserId"
TARGET_S1 = "edgeTargetSession1"
TARGET_S2 = "edgeTargetSession2"
OUTSIDER = "edgeOutsiderSession2"
BYSTANDER = "edgeBystanderUserId"
UNVERIFIED = "edgeUnverifiedSession"
CONFIRM_CODE = "edgeUnverifiedConfirmCode"
PASSWORD_NOW = "edgeSeedPassword"
PASSWORD_NEW = "edgeSeedNewPassword"


def _cookies_pre(*pairs):
    """`Cookie` из слотов посева; пустой слот — НЕ шлётся (шаг покраснеет утверждением)."""
    parts = " + ".join(
        f"(pm.environment.get({js_str(v)}) ? {js_str(n)} + '=' + pm.environment.get({js_str(v)}) + '; ' : '')"
        for n, v in pairs)
    return [
        f"const __cookie = ({parts}).replace(/; $/, '');",
        "if (__cookie) { pm.request.headers.upsert({key: 'Cookie', value: __cookie}); }",
    ]


def _guards(*slots):
    out = []
    for s in slots:
        out += require_env_slot(s, "seeded by prodseed_matrix.py (kacho#3334)")
    return out


def _body_keys(*keys):
    return [
        f"pm.test({js_str('body keys are exactly ' + ', '.join(sorted(keys)))}, () => {{",
        "  let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
        f"  pm.expect(Object.keys(j).sort(), JSON.stringify(j)).to.eql({sorted(keys)!r});",
        "});",
    ]


def _json_content_type():
    return [
        "pm.test('content-type is application/json', () => "
        "pm.expect(pm.response.headers.get('Content-Type') || '').to.match(/^application\\/json/));",
    ]


def _session_step(name, path, session_var, test_script, method="GET", body=None):
    """Запрос под сессией из слота, печенье — явно."""
    return Step(
        name=name, method=method, path=path, auth="anonymous", explicit_cookies=True, body=body,
        pre_script=[*_guards(session_var), *_cookies_pre(("kaname_session", session_var))],
        test_script=[*assert_answered(name), *test_script],
    )


def _error_info(reason, domain=None):
    dom = (f"  pm.expect(infos[0].domain, JSON.stringify(j)).to.eql({js_str(domain)});" if domain else "")
    return [
        f"pm.test({js_str(f'details[] carries ErrorInfo with reason {reason}')}, () => {{",
        "  let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
        "  const infos = (Array.isArray(j.details) ? j.details : [])"
        ".filter(d => d && d['@type'] === 'type.googleapis.com/google.rpc.ErrorInfo');",
        "  pm.expect(infos.length, JSON.stringify(j)).to.eql(1);",
        f"  pm.expect(infos[0].reason, JSON.stringify(j)).to.eql({js_str(reason)});",
        dom,
        "});",
    ]


def _cookie_cleared():
    """Носитель погашен: `Set-Cookie` снимает `kaname_session` (пустое значение, `Max-Age=0`)."""
    return [
        "pm.test('Set-Cookie clears kaname_session (empty value, Max-Age=0)', () => {",
        "  const sc = pm.response.headers.all()"
        ".filter(h => h.key.toLowerCase() === 'set-cookie' && h.value.startsWith('kaname_session='));",
        "  pm.expect(sc.length, JSON.stringify(pm.response.headers.all())).to.eql(1);",
        "  pm.expect(sc[0].value.split(';')[0], sc[0].value).to.eql('kaname_session=');",
        "  pm.expect(sc[0].value, sc[0].value).to.match(/Max-Age=0/);",
        "});",
    ]


def _bearer_challenge():
    """Край зовёт на аутентификацию (RFC 6750 §3): носитель недействителен."""
    return [
        "pm.test('WWW-Authenticate challenges with invalid_token', () => "
        "pm.expect(pm.response.headers.get('Www-Authenticate') || '', JSON.stringify(pm.response.headers.all()))"
        ".to.eql('Bearer realm=\"kacho\", error=\"invalid_token\"'));",
    ]


def _no_session_cookie_issued():
    return [
        "pm.test('no kaname_session cookie is issued on this refusal', () => "
        "pm.expect(pm.response.headers.all().filter(h => h.key.toLowerCase() === 'set-cookie' "
        "&& h.value.startsWith('kaname_session=')).length, JSON.stringify(pm.response.headers.all())).to.eql(0));",
    ]


def _poll_by_session(op_var, session_var, name, extra_checks):
    """Опрос операции ТЕМ ЖЕ носителем, что её начал.

    Операцию читает её автор: бутстрап-удостоверение (`poll_operation` набора) чужую
    операцию не видит — край отвечает ему `404 operation … not found` до конца предела
    опроса (измерено пробой стенда). Тот же предел и та же настоящая пауза между
    опросами, что у `poll_operation`: busy-wait прямо перед `setNextRequest`.
    """
    return Step(
        name=name, method="GET", path="/operations/{{" + op_var + "}}",
        auth="anonymous", explicit_cookies=True,
        pre_script=[
            *_guards(session_var),
            # OPERATION guard (законный пропуск): мутация отвергнута намеренно — опрашивать нечего.
            f"if (!pm.environment.get({js_str(op_var)})) {{ pm.execution.skipRequest(); }}",
            *_cookies_pre(("kaname_session", session_var)),
        ],
        test_script=[
            "pm.test('poll status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
            "const j = pm.response.json();",
            "if (pm.environment.get('_pollStarted') !== pm.info.requestName) {",
            "  pm.environment.set('_pollCount', '0');",
            "  pm.environment.set('_pollStarted', pm.info.requestName);",
            "}",
            "const pc = parseInt(pm.environment.get('_pollCount') || '0', 10);",
            f"if (!j.done && pc < {POLL_CAP}) {{",
            "  pm.environment.set('_pollCount', String(pc + 1));",
            f"  const _ipd = Date.now(); while (Date.now() - _ipd < {POLL_DELAY_MS}) void 0;"
            "  /* настоящая пауза между опросами */",
            "  pm.execution.setNextRequest(pm.info.requestName);",
            "  return;",
            "}",
            "pm.environment.unset('_pollCount');",
            "pm.environment.unset('_pollStarted');",
            "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
            "pm.test('operation succeeded (no error)', () => {",
            "  pm.expect(j.error, 'op error: ' + JSON.stringify(j.error || {})).to.be.oneOf([undefined, null]);",
            "});",
            *extra_checks,
        ])


# ---------------------------------------------------------------------------
# LMR-01 — сброс ключей: обе прежние сессии цели гаснут на любом пути.
# ---------------------------------------------------------------------------

# Три пути разных полос: форма входа («кто я» сессии), идентичность края, ресурс под правом.
LMR01_PATHS = [
    ("auth-me", "/iam/v1/auth/me"),
    ("me", "/iam/v1/me"),
    ("accounts", "/iam/v1/accounts?pageSize=1"),
]


def _lmr01_steps():
    steps = []
    # Близнец: ДО сброса обе сессии на каждом пути живы — 200.
    for label, path in LMR01_PATHS:
        for sess_label, sess in (("s1", TARGET_S1), ("s2", TARGET_S2)):
            steps.append(_session_step(
                f"before-{label}-{sess_label}", path, sess,
                [*assert_status(200)]))
    # Контроль области: администратор видит у цели один ключ.
    steps.append(_session_step(
        "before-admin-lists-keys", "/iam/v1/users/{{" + TARGET + "}}/accessKeys", ADMIN,
        [*assert_status(200),
         "pm.test('target has exactly one access key before the reset', () => {",
         "  const j = pm.response.json();",
         "  pm.expect((j.accessKeys || []).length, JSON.stringify(j)).to.eql(1);",
         "});"]))
    # Сброс.
    steps.append(Step(
        name="reset-access-keys", method="POST",
        path="/iam/v1/users/{{" + TARGET + "}}:" + RESET_KEYS, body={},
        auth="anonymous", explicit_cookies=True,
        pre_script=[*_guards(ADMIN, TARGET), *_cookies_pre(("kaname_session", ADMIN))],
        test_script=[
            *assert_answered("reset-access-keys"),
            *assert_status(200),
            *assert_operation_envelope(OP_PREFIX, "IAM"),
            *save_from_response("j.id", "lmr01OpId"),
        ]))
    steps.append(_poll_by_session(
        "lmr01OpId", ADMIN, "poll-reset",
        ["pm.test('operation response is the target User', () => {",
         "  const j = pm.response.json();",
         "  pm.expect((j.response || {}).id, JSON.stringify(j)).to.eql(pm.environment.get(" + js_str(TARGET) + "));",
         "});"]))
    # ПОСЛЕ сброса: обе сессии на каждом пути — 401, code 16, печенье погашено.
    for label, path in LMR01_PATHS:
        for sess_label, sess in (("s1", TARGET_S1), ("s2", TARGET_S2)):
            steps.append(_session_step(
                f"after-{label}-{sess_label}", path, sess,
                [*assert_status(401), *assert_grpc_code(16, "UNAUTHENTICATED"),
                 *assert_error_message_eql("authentication failed"),
                 *_error_info("AUTHN_REQUIRED", "kaname.cloud.iam.v1"),
                 *_body_keys("code", "message", "details"),
                 *_json_content_type(), *_cookie_cleared(), *_bearer_challenge()]))
    # Контроль области: погашены сессии цели, а не все — администратор жив и видит ноль ключей.
    steps.append(_session_step(
        "after-admin-lists-keys", "/iam/v1/users/{{" + TARGET + "}}/accessKeys", ADMIN,
        [*assert_status(200),
         "pm.test('target has no access key after the reset', () => {",
         "  const j = pm.response.json();",
         "  pm.expect(j.accessKeys || [], JSON.stringify(j)).to.eql([]);",
         "});"]))
    return steps


CASES.append(Case(
    id="LMR-01-EDGE-SESSIONS-DEAD-AFTER-RESET",
    title="LMR-01: cloud administrator (session 2) resets a human's access keys → both prior sessions of "
          "the human get 401/16 with the cookie cleared on every path; twin: the same sessions before the "
          "reset get 200",
    classes=["AUTHZ", "AUTHN", "STATE", "CONF"],
    priority="P0",
    steps=_lmr01_steps(),
))


# ---------------------------------------------------------------------------
# LMR-03 / Ф12-30 — кривой идентификатор и посторонний, оба глагола.
# ---------------------------------------------------------------------------

def _id_refusal_steps(verb, tag):
    cap = f"{tag}BadIdBody"
    return [
        # Администратор облака на кривой идентификатор: синхронный отказ края.
        Step(
            name=f"{tag}-admin-bad-id", method="POST", path=f"/iam/v1/users/{BAD_ID}:{verb}", body={},
            auth="anonymous", explicit_cookies=True,
            pre_script=[*_guards(ADMIN), *_cookies_pre(("kaname_session", ADMIN))],
            test_script=[
                *assert_answered(f"{tag}-admin-bad-id"),
                *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                *assert_error_message_eql(f"invalid resource id '{BAD_ID}'"),
                *_json_content_type(), *_body_keys("code", "message"),
                "pm.test('body is byte-equal to the edge refusal value', () => "
                f"pm.expect(pm.response.text()).to.eql({js_str(BAD_ID_BODY)}));",
                f"pm.environment.set({js_str(cap)}, pm.response.text());",
            ]),
        # Посторонний на тот же кривой идентификатор: побайтово тот же отказ.
        Step(
            name=f"{tag}-outsider-bad-id", method="POST", path=f"/iam/v1/users/{BAD_ID}:{verb}", body={},
            auth="anonymous", explicit_cookies=True,
            pre_script=[*_guards(OUTSIDER), *_cookies_pre(("kaname_session", OUTSIDER))],
            test_script=[
                *assert_answered(f"{tag}-outsider-bad-id"),
                *assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                *assert_error_message_eql(f"invalid resource id '{BAD_ID}'"),
                "pm.test('outsider body is byte-equal to the cloud administrator body', () => {",
                f"  const ref = pm.environment.get({js_str(cap)}) || '';",
                "  pm.expect(ref, 'reference body was not captured').to.not.be.empty;",
                "  pm.expect(pm.response.text()).to.eql(ref);",
                "});",
            ]),
        # Близнец по одному факту (форма идентификатора): посторонний на ПРАВИЛЬНЫЙ — отказ по праву.
        Step(
            name=f"{tag}-outsider-right-id", method="POST",
            path="/iam/v1/users/{{" + BYSTANDER + "}}:" + verb, body={},
            auth="anonymous", explicit_cookies=True,
            pre_script=[*_guards(OUTSIDER, BYSTANDER), *_cookies_pre(("kaname_session", OUTSIDER))],
            test_script=[
                *assert_answered(f"{tag}-outsider-right-id"),
                *assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED"),
                *_error_info("AUTHZ_DENIED", "kaname.cloud.iam.v1"),
                *_json_content_type(), *_no_session_cookie_issued(),
            ]),
    ]


CASES.append(Case(
    id="LMR-03-EDGE-RESET-ACCESS-KEYS-BAD-ID",
    title="LMR-03: :resetAccessKeys with a malformed id → sync 400/3 \"invalid resource id\", byte-equal "
          "for the cloud administrator and an outsider; the outsider on a well-formed id → 403 AUTHZ_DENIED",
    classes=["VAL", "NEG", "AUTHZ", "CONF"],
    priority="P0",
    steps=_id_refusal_steps(RESET_KEYS, "lmr03"),
))

CASES.append(Case(
    id="F12-30-EDGE-RESET-SECOND-FACTOR-BAD-ID",
    title="Ф12-30: :resetSecondFactor with a malformed id → sync 400/3 \"invalid resource id\", byte-equal "
          "for the cloud administrator and an outsider; the outsider on a well-formed id → 403 AUTHZ_DENIED",
    classes=["VAL", "NEG", "AUTHZ", "CONF"],
    priority="P0",
    steps=_id_refusal_steps(RESET_2F, "f1230"),
))


# ---------------------------------------------------------------------------
# EV-14 — отказ адреса называет шаг, и названный шаг снимает отказ.
# ---------------------------------------------------------------------------

def _form_token_step(name, form, session_var, token_var, form_cookie_var):
    return Step(
        name=name, method="GET", path=f"{CSRF}?form={form}", auth="anonymous", explicit_cookies=True,
        pre_script=[
            *_guards(session_var),
            f"pm.environment.set({js_str(token_var)}, '');",
            f"pm.environment.set({js_str(form_cookie_var)}, '');",
            *_cookies_pre(("kaname_session", session_var)),
        ],
        test_script=[
            *assert_answered(name), *assert_status(200),
            f"pm.test({js_str(f'csrf {form}: csrfToken is a non-empty string')}, () => {{",
            "  let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
            "  pm.expect(j.csrfToken, JSON.stringify(j)).to.be.a('string').and.not.empty;",
            f"  pm.environment.set({js_str(token_var)}, j.csrfToken);",
            "});",
            "{",
            "  const __sc = pm.response.headers.all()"
            ".filter(h => h.key.toLowerCase() === 'set-cookie' && h.value.startsWith('kaname_form='));",
            f"  pm.test({js_str(f'csrf {form}: response sets the kaname_form cookie')}, () => "
            "pm.expect(__sc.length, JSON.stringify(pm.response.headers.all())).to.eql(1));",
            f"  if (__sc.length === 1) {{ pm.environment.set({js_str(form_cookie_var)}, "
            "__sc[0].value.split(';')[0].slice('kaname_form='.length)); }",
            "}",
        ])


def _ev14_steps():
    return [
        _form_token_step("csrf-password-n1", "password", UNVERIFIED, "ev14Token1", "ev14Form1"),
        Step(
            name="password-unverified", method="POST", path=PASSWORD,
            body={"currentPassword": "{{" + PASSWORD_NOW + "}}", "newPassword": "{{" + PASSWORD_NEW + "}}",
                  "csrfToken": "{{ev14Token1}}"},
            auth="anonymous", explicit_cookies=True,
            pre_script=[
                *_guards(UNVERIFIED, PASSWORD_NOW, PASSWORD_NEW),
                "if (!pm.environment.get('ev14Token1')) { pm.execution.skipRequest(); }",
                *_cookies_pre(("kaname_session", UNVERIFIED), ("kaname_form", "ev14Form1")),
            ],
            test_script=[
                *assert_answered("password-unverified"),
                *assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED"),
                *_json_content_type(),
                "pm.test('body is byte-equal to addressRefusalBody of the edge', () => "
                f"pm.expect(pm.response.text()).to.eql({js_str(ADDRESS_REFUSAL_BODY)}));",
                *_no_session_cookie_issued(),
            ]),
        _form_token_step("csrf-confirm", "verify-email-confirm", UNVERIFIED, "ev14ConfirmToken", "ev14ConfirmForm"),
        Step(
            name="verify-email-confirm", method="POST", path=VERIFY_CONFIRM,
            body={"code": "{{" + CONFIRM_CODE + "}}", "csrfToken": "{{ev14ConfirmToken}}"},
            auth="anonymous", explicit_cookies=True,
            pre_script=[
                *_guards(UNVERIFIED, CONFIRM_CODE),
                "if (!pm.environment.get('ev14ConfirmToken')) { pm.execution.skipRequest(); }",
                "pm.environment.set('ev14Session2', '');",
                *_cookies_pre(("kaname_session", UNVERIFIED), ("kaname_form", "ev14ConfirmForm")),
            ],
            test_script=[
                *assert_answered("verify-email-confirm"),
                *assert_status(200),
                "pm.test('session.emailVerified is true', () => {",
                "  const j = pm.response.json();",
                "  pm.expect((j.session || {}).emailVerified, JSON.stringify(j)).to.eql(true);",
                "});",
                "{",
                "  const __sc = pm.response.headers.all()"
                ".filter(h => h.key.toLowerCase() === 'set-cookie' && h.value.startsWith('kaname_session='));",
                "  pm.test('a NEW kaname_session carrier is issued', () => {",
                "    pm.expect(__sc.length, JSON.stringify(pm.response.headers.all())).to.eql(1);",
                "    const v = __sc[0].value.split(';')[0].slice('kaname_session='.length);",
                "    pm.expect(v).to.not.be.empty;",
                f"    pm.expect(v).to.not.eql(pm.environment.get({js_str(UNVERIFIED)}));",
                "    pm.environment.set('ev14Session2', v);",
                "  });",
                "}",
            ]),
        _form_token_step("csrf-password-n2", "password", "ev14Session2", "ev14Token2", "ev14Form2"),
        Step(
            name="password-verified", method="POST", path=PASSWORD,
            body={"currentPassword": "{{" + PASSWORD_NOW + "}}", "newPassword": "{{" + PASSWORD_NEW + "}}",
                  "csrfToken": "{{ev14Token2}}"},
            auth="anonymous", explicit_cookies=True,
            pre_script=[
                *_guards("ev14Session2"),
                "if (!pm.environment.get('ev14Token2')) { pm.execution.skipRequest(); }",
                *_cookies_pre(("kaname_session", "ev14Session2"), ("kaname_form", "ev14Form2")),
            ],
            test_script=[
                *assert_answered("password-verified"),
                *assert_status(200),
                "pm.test('the same request after the named step: session.emailVerified is true', () => {",
                "  const j = pm.response.json();",
                "  pm.expect((j.session || {}).emailVerified, JSON.stringify(j)).to.eql(true);",
                "});",
                "pm.test('no address refusal in the body', () => "
                "pm.expect(pm.response.text()).to.not.include('EMAIL_NOT_VERIFIED'));",
            ]),
    ]


CASES.append(Case(
    id="EV-14-EDGE-ADDRESS-REFUSAL-NAMES-THE-STEP",
    title="EV-14: POST /iam/v1/auth/password with an unconfirmed address → 403, body byte-equal to the edge "
          "addressRefusalBody; after verify-email/confirm the NEW carrier gets 200",
    classes=["AUTHZ", "STATE", "CONF"],
    priority="P0",
    steps=_ev14_steps(),
))
