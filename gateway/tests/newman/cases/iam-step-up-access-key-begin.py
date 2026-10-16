# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set iam-step-up-access-key-begin — КРАЙ РЕТРАНСЛИРУЕТ ВЫДАЧУ ИСПЫТАНИЯ ПОВЫШЕНИЯ КЛЮЧОМ.

Сторона платформы (kacho#3230) приёмки F8-S7 воркспейса
(`docs/specs/sub-phase-F8-S7-console-security-settings-and-step-up-methods-acceptance.md`,
одобрена на отпечатке `559ae688…`): сценарий F8-S7-80, DoD §8 п.3 — сквозная
проба края второго эшелона (`00-kacho-core` `ban12-second-echelon`).

ПРЕДМЕТ — ЗАПИСЬ РЕТРАНСЛЯЦИИ КРАЯ `POST /iam/v1/auth/step-up/access-key/begin`
(`gateway/internal/middleware/login_lane_paths.go`, глагол
`step-up-access-key-begin`, цель — слушатель формы службы) и её решение до
подтверждения адреса — парой со `step-up`. Сам ответ производит служба
(SUA-21 её приёмки `step-up-with-any-enrolled-method.md`, PRO-Robotech/kaname#827);
край его не меняет.

Два кейса — пара, отличающаяся ОДНИМ фактом: подтверждён ли адрес человека.

  * `F8-S7-80-IAM-STEPUP-AKBEGIN-NEG-NO-KEY` — Ф-п без ключа: ответ службы
    ретранслирован краем без изменения — `400`, `code` 9,
    `ACCESS_KEY_NOT_REGISTERED`, «no access key is registered: register an
    access key first». До записи ретрансляции путь краем не обслуживался, и
    этот кейс краснел промахом маршрута, а не отказом службы.
  * `F8-S7-80-IAM-STEPUP-AKBEGIN-AUTHZ-UNVERIFIED` — Ф-н тем же запросом: край
    закрывает запись до подтверждения — `403`, `code` 7, `EMAIL_NOT_VERIFIED`;
    тело побайтово равно ответу края на `POST /iam/v1/auth/step-up` того же
    человека в том же состоянии (F8-S7-80, «And»).

Положительной ветви `200` с `publicKey.allowCredentials` здесь нет: ключ
заводит только церемония браузера, она — за браузерной F8-S7-21 на стенде (п.4
DoD приёмки). Отказ службы на Ф-п — и есть положительный контроль ретрансляции:
его производит служба, а не край.

ЛЮДИ — ИЗ ПОСЕВА (`tests/authz-fixtures/verified_human.py`, место заведения —
`tests/authz-fixtures/prodseed_matrix.py`): случай людей не заводит (гейт
`internal/repohygiene/suitepeopleenrollment_test.go`, вид 3). Слоты —
носители сессии `kaname_session`: `stepUpVerifiedHumanSession` (Ф-п, ключей
ноль) и `stepUpUnverifiedHumanSession` (Ф-н). Ни один кейс ключа не заводит,
поэтому повторный прогон тем же посевом даёт тот же исход.

ПЕЧЕНЬЯ НЕСУТСЯ ЯВНО: признак формы и его печенье `kaname_form` захватываются
шагом выдачи признака и кладутся заголовком `Cookie` следующим шагом вместе с
носителем сессии. Банка печений прогонщика у каждого шага снята
(`explicit_cookies`): служба выдаёт `kaname_form` только запросу, который его
не несёт, и печенье Ф-п из общей банки, уехав в выдачу признака Ф-н, сняло бы
выдачу печенья второму человеку (kacho#3230). `Authorization` у шагов снят: предъявитель здесь — сессия, и
только она.

Техники: классы эквивалентности (адрес подтверждён / не подтверждён; ключей
ноль), таблица решений «положение подтверждения × запись края», угадывание
ошибок (край отвечает своим текстом вместо ретрансляции; запись открыта до
подтверждения).

Чего модуль НЕ утверждает: выдачу испытания при заведённом ключе, отказы
`ACCESS_KEYS_DISABLED`, `CHALLENGE_*`, счёт попыток — это сущности службы, их
дом — её репозиторий (e2e-flow.md §7а воркспейса), сценарии SUA-03…SUA-24.
"""

# ЧЬЁ ПОВЕДЕНИЕ УТВЕРЖДАЕТ ЭТОТ МОДУЛЬ (e2e-flow.md §7а, решение владельца 2026-09-12).
ASSERTS_DOMAIN = "gateway"
ASSERTS_REASON = (
    "предмет — запись ретрансляции края `step-up-access-key-begin` и её решение до "
    "подтверждения адреса парой со `step-up`; отказ `ACCESS_KEY_NOT_REGISTERED` "
    "производит служба, и модуль утверждает, что край его ретранслирует без изменения, "
    "а закрытая до подтверждения запись отвечает тем же отказом, что `step-up`."
)

CASES = []

CSRF = "/iam/v1/auth/csrf"
BEGIN = "/iam/v1/auth/step-up/access-key/begin"
STEP_UP = "/iam/v1/auth/step-up"

# Вид признака формы — один на глагол (контракт службы, полоса формы).
FORM_BEGIN = "step-up-access-key-begin"
FORM_STEP_UP = "step-up"

NO_KEY_TEXT = "no access key is registered: register an access key first"
NOT_VERIFIED_TEXT = ("email address is not verified: confirm it with the code from the letter "
                     "(POST /iam/v1/auth/verify-email/confirm)")

VERIFIED = "stepUpVerifiedHumanSession"
UNVERIFIED = "stepUpUnverifiedHumanSession"


def _slot_guard(var, who):
    return require_env_slot(var, f"{who} seeded by prodseed_matrix.py (F8-S7-80)")


def _with_cookies(*pairs):
    """Явный `Cookie` из захваченных переменных; отсутствующая — не шлётся."""
    parts = " + ".join(
        f"(pm.environment.get({js_str(v)}) ? {js_str(name)} + '=' + pm.environment.get({js_str(v)}) + '; ' : '')"
        for name, v in pairs)
    return [
        f"const __cookie = ({parts}).replace(/; $/, '');",
        "if (__cookie) { pm.request.headers.upsert({key: 'Cookie', value: __cookie}); }",
    ]


def _csrf_step(name, form, session_var, token_var, cookie_var):
    """Выдача признака формы носителем сессии: шаг подготовки утверждает свой исход."""
    return Step(
        name=name,
        method="GET",
        path=f"{CSRF}?form={form}",
        auth="anonymous",
        explicit_cookies=True,
        pre_script=[
            *_slot_guard(session_var, "session bearer of the step-up human"),
            # Прежние значения не переживают шаг: признак чужого шага не уйдёт дальше.
            f"pm.environment.set({js_str(token_var)}, '');",
            f"pm.environment.set({js_str(cookie_var)}, '');",
            *_with_cookies(("kaname_session", session_var)),
        ],
        test_script=[
            *assert_answered(f"csrf {form}"),
            *assert_status(200),
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
            f"  if (__sc.length === 1) {{ pm.environment.set({js_str(cookie_var)}, "
            "__sc[0].value.split(';')[0].slice('kaname_form='.length)); }",
            "}",
        ],
    )


def _post_step(name, path, session_var, token_var, cookie_var, test_script):
    return Step(
        name=name,
        method="POST",
        path=path,
        auth="anonymous",
        explicit_cookies=True,
        body={"csrfToken": "{{" + token_var + "}}"},
        pre_script=[
            *_slot_guard(session_var, "session bearer of the step-up human"),
            # Признака нет — шаг выдачи уже покраснел и назвал причину; запрос без
            # признака проверял бы отказ формы, а не запись края.
            f"if (!pm.environment.get({js_str(token_var)})) {{ pm.execution.skipRequest(); }}",
            *_with_cookies(("kaname_session", session_var), ("kaname_form", cookie_var)),
        ],
        test_script=[*assert_answered(name), *test_script],
    )


def _error_info_reason(reason):
    return [
        f"pm.test({js_str(f'details[] carries ErrorInfo with reason {reason}')}, () => {{",
        "  let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
        "  const infos = (Array.isArray(j.details) ? j.details : [])"
        ".filter(d => d && d['@type'] === 'type.googleapis.com/google.rpc.ErrorInfo');",
        "  pm.expect(infos.length, JSON.stringify(j)).to.eql(1);",
        f"  pm.expect(infos[0].reason, JSON.stringify(j)).to.eql({js_str(reason)});",
        "});",
    ]


def _refusal_shape():
    """Сверка с интерфейсом: форма `google.rpc.Status`, JSON, носитель не выдан."""
    return [
        "pm.test('content-type is application/json', () => "
        "pm.expect(pm.response.headers.get('Content-Type') || '').to.match(/^application\\/json/));",
        "pm.test('body keys are exactly code, message, details', () => {",
        "  let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
        "  pm.expect(Object.keys(j).sort(), JSON.stringify(j)).to.eql(['code', 'details', 'message']);",
        "});",
        "pm.test('no kaname_session cookie is issued on a refusal', () => "
        "pm.expect(pm.response.headers.all().filter(h => h.key.toLowerCase() === 'set-cookie' "
        "&& h.value.startsWith('kaname_session=')).length, JSON.stringify(pm.response.headers.all())).to.eql(0));",
    ]


# ---------------------------------------------------------------------------
# F8-S7-80 — Ф-п без ключа: отказ службы ретранслирован краем без изменения.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="F8-S7-80-IAM-STEPUP-AKBEGIN-NEG-NO-KEY",
    title="F8-S7-80: verified human without an access key — POST /iam/v1/auth/step-up/access-key/begin "
          "through the edge → the service refusal relayed unchanged: 400 FAILED_PRECONDITION (9), "
          "ACCESS_KEY_NOT_REGISTERED",
    classes=["NEG", "CONF", "STATE"],
    priority="P0",
    steps=[
        _csrf_step("csrf-begin-verified", FORM_BEGIN, VERIFIED, "f8s780PToken", "f8s780PForm"),
        _post_step("begin-verified-no-key", BEGIN, VERIFIED, "f8s780PToken", "f8s780PForm", [
            *assert_status(400),
            *assert_grpc_code(9, "FAILED_PRECONDITION"),
            *assert_error_message_eql(NO_KEY_TEXT),
            *_error_info_reason("ACCESS_KEY_NOT_REGISTERED"),
            *_refusal_shape(),
        ]),
    ],
))


# ---------------------------------------------------------------------------
# F8-S7-80, близнец — Ф-н тем же запросом: запись закрыта до подтверждения,
# как у `step-up`, и ответ побайтово равен ответу `step-up`.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="F8-S7-80-IAM-STEPUP-AKBEGIN-AUTHZ-UNVERIFIED",
    title="F8-S7-80 twin: human with an unverified address — the same request → 403 PERMISSION_DENIED (7), "
          "EMAIL_NOT_VERIFIED; body byte-equal to the edge answer on POST /iam/v1/auth/step-up",
    classes=["AUTHZ", "NEG", "CONF"],
    priority="P0",
    steps=[
        _csrf_step("csrf-step-up-unverified", FORM_STEP_UP, UNVERIFIED, "f8s780NStepUpToken", "f8s780NStepUpForm"),
        _post_step("step-up-unverified", STEP_UP, UNVERIFIED, "f8s780NStepUpToken", "f8s780NStepUpForm", [
            *assert_status(403),
            *assert_grpc_code(7, "PERMISSION_DENIED"),
            # Эталон для побайтового сравнения ниже — тело ответа края на `step-up`.
            "pm.environment.set('f8s780NStepUpBody', pm.response.text());",
        ]),
        _csrf_step("csrf-begin-unverified", FORM_BEGIN, UNVERIFIED, "f8s780NToken", "f8s780NForm"),
        _post_step("begin-unverified", BEGIN, UNVERIFIED, "f8s780NToken", "f8s780NForm", [
            *assert_status(403),
            *assert_grpc_code(7, "PERMISSION_DENIED"),
            *assert_error_message_eql(NOT_VERIFIED_TEXT),
            *_error_info_reason("EMAIL_NOT_VERIFIED"),
            *_refusal_shape(),
            "pm.test('body is byte-equal to the edge answer on POST /iam/v1/auth/step-up in the same state', () => {",
            "  const ref = pm.environment.get('f8s780NStepUpBody') || '';",
            "  pm.expect(ref, 'reference step-up body was not captured').to.not.be.empty;",
            "  pm.expect(pm.response.text()).to.eql(ref);",
            "});",
        ]),
    ],
))
