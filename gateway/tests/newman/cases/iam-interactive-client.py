# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set iam-interactive-client — секрет конфиденциального интерактивного клиента: выдан один раз и нигде больше.

Перенесён из набора службы доступа (PRO-Robotech/kaname, коллекция того же имени;
держатель переноса — #2913, сторона службы — PRO-Robotech/kaname#416). Там его не
гонял ни один шаг конвейера: REST набора службы до глаголов
`InternalInteractiveClientService` не доходит, их круг вызывающих — край
(`GatewayFrontedInternalRPCs`). Здесь его гоняет шаг
`гейт — newman зелёный (api-gateway)` (`.github/workflows/e2e-newman.yml`) на стенде
платформы под посадкой `own`.

ЧТО МОДУЛЬ УТВЕРЖДАЕТ — ПЯТЬ ПОЗИЦИЙ ПРИЁМКИ, И ТОЛЬКО ИХ. Приёмка службы
`docs/engineering/acceptance/confidential-interactive-client-secret-shown-once.md`
(PRO-Robotech/kaname), уровень E позиций IC-SECRET-01, 04, 08, 09 и 10: то, что
чёрный ящик видит только через край — внутренний слушатель для пяти глаголов
клиента и координату выдачи внешнего слушателя для предъявления секрета. Сущности
службы сверх этих позиций модуль не переутверждает (e2e-flow.md §7а воркспейса):
прежняя коллекция утверждала форму публичного клиента со способом `none`, и на
посадке `own` её предмета нет — там клиент конфиденциальный.

ПРЕДЪЯВЛЕНИЕ СЕКРЕТА БЕЗ ЦЕРЕМОНИИ ЧЕЛОВЕКА — И ГРАНИЦА ЭТОГО. Полный обмен кода
(IC-SECRET-06) требует кода, выданного точкой авторизации после входа человека, и
в эти пять позиций не входит. Позициям 04, 08, 09 и 10 нужна его КЛИЕНТСКАЯ
половина — годится ли секрет как доказательство клиента, — и она наблюдаема без
кода: координата выдачи сначала проверяет клиента и только потом судит код
(RFC 6749 §3.2.1, §5.2). Поэтому шаг предъявления шлёт `Authorization: Basic` от
пары (`clientId`, секрет) с кодом, который точка авторизации не выдавала никогда,
и исходов у него два, различимых одним фактом — значением секрета:
  верная пара  → `400 {"error":"invalid_grant"}`  — клиент доказан, отвергнут код;
  неверный секрет, снятый клиент, клиент, которого не заводили, →
               `401 {"error":"invalid_client"}` — клиент не доказан.
Каждый кейс, опирающийся на это различие, несёт обе ветки сам, а не ссылается на
замер. Замер стенда посадки `own` 2026-10-01 через внешний слушатель (HTTP/1.1):
ровно эти четыре ответа; `401` двух последних побайтово равны друг другу.

Coverage:
  IAM-IC-SECRET-01-CR-ISSUES-SECRET   — IC-SECRET-01: ответ `Create` — операция done без ошибки,
                                        `CreateInteractiveClientResponse` с клиентом способа
                                        `client_secret_basic` и непустым `clientSecret`; `Get` описывает
                                        ту же строку и секрета не несёт (близнец — 08)
  IAM-IC-SECRET-04-CR-CONF-DUP-NAME   — IC-SECRET-04: второй `Create` того же имени → 409 синхронно,
                                        секрета нет, в списке один клиент, секрет первого по-прежнему
                                        доказывает клиента (близнец — первое заведение)
  IAM-IC-SECRET-08-GT-NO-SECRET       — IC-SECRET-08: `Get` — ресурс со способом `client_secret_basic`
                                        без секрета по ЗНАЧЕНИЮ; в тот же момент секрет доказывает
                                        клиента, а соседний неверный — нет
  IAM-IC-SECRET-09-LS-UP-NO-SECRET    — IC-SECRET-09: (а) страница `List` в проекции `Get` без секрета;
                                        (б) правка `description` без секрета, секрет цел; (в) маска
                                        `clientSecret` → неизвестное поле в деталях; (г) маска
                                        `tokenEndpointAuthMethod` → неизменяемое (близнец (в),(г) — (б))
  IAM-IC-SECRET-10-DL-NO-USABLE-SECRET
                                      — IC-SECRET-10: снятие — эхо ресурса без секрета; прежний секрет →
                                        `invalid_client` побайтово как неверный и как незаведённый;
                                        `Get` → 404 тоном контракта; повтор снятия — тот же исход
                                        (близнец — то же предъявление до снятия)

Техники: переходы состояний (заведён → правлен → снят; предъявление до и после),
таблица решений (маска: известное изменяемое / неизвестное / неизменяемое),
угадывание ошибок (секрет по значению, а не образцом имени: образец
`client_secret` совпадает с законным значением способа `client_secret_basic`;
отказ повторного заведения не трогает материал первого).

Кто вызывает. `jwtBootstrap` — служебная учётка `system_admin` @ `cluster`: запись
каталога прав на всех пяти глаголах требует её, а `Create`/`Update`/`Delete`
объявляют `required_acr_min = "2"`, и машинный принципал — единственный, кого
порог пропускает без церемонии.

Самодостаточность: каждый кейс заводит своего клиента с `{{runId}}` в имени и
снимает его сам; уникальность имени у реестра общекластерная.
"""

# ЧЬЁ ПОВЕДЕНИЕ УТВЕРЖДАЕТ ЭТОТ МОДУЛЬ (e2e-flow.md §7а, решение владельца 2026-09-12).
# Единственный домен по REST-путям — `iam`. Сверяет `tests/newman/scripts/case_home_test.py`.
ASSERTS_DOMAIN = "gateway"
ASSERTS_REASON = (
    "дом по кругу вызывающих: глаголы `InternalInteractiveClientService` фронтирует край "
    "(внутренний слушатель; `GatewayFrontedInternalRPCs`), а секрет предъявляется на "
    "координате выдачи внешнего слушателя края — чёрный ящик видит обе поверхности только "
    "через край. Маршрут `/iam/v1/internal/interactiveClients` — носитель пяти позиций "
    "IC-SECRET (01, 04, 08, 09, 10) посадки own; прочей формы клиента как ресурса службы "
    "модуль не утверждает (решение об исходе — PRO-Robotech/kaname#416)."
)

CASES = []

IC_PATH = "/iam/v1/internal/interactiveClients"

# Адрес возврата, удовлетворяющий контракту: абсолютный, https, с хостом, без
# фрагмента. Резолвиться ему незачем: код туда не доставляется ни в одном кейсе.
REDIRECT = "https://api.kacho.local/auth/callback"

# Код, которого точка авторизации не выдавала никогда. Предмет шагов предъявления —
# клиентская половина обмена, а не код (см. шапку): координата судит клиента раньше
# кода, поэтому ответ различает доказан ли клиент, при любом коде.
_NEVER_ISSUED_CODE = "ic-secret-never-issued-code"
# Проверочное значение PKCE по форме RFC 7636 §4.1 (43 знака). Коду выше оно
# не соответствует, как и никакому другому, — до сверки PKCE дело не доходит.
_VERIFIER = "ic-secret-verifier-0123456789abcdefghijklmnop"

# Идентификатор клиента, которого служба не заводила никогда: форма `oic-` и 17
# знаков крокфорда из одних нулей.
_NEVER_CREATED_CLIENT_ID = "oic-00000000000000000"

_ID_RE = "/^ic-[0-9a-hjkmnp-tv-z]{17}$/"


def _create(name_suffix, v, extra_asserts=()):
    """Заведение клиента K с захватом `id`, `clientId` и секрета S в имена `<v>…`.

    Исход назван самим шагом: на посадке `own` ответ вызова — операция с
    `done = true` (IC-SECRET-01, первое Then), поэтому опрашивать нечего, а
    захваченное — это ровно то, что клиент получил один раз.
    """
    return Step(
        name=f"create-{name_suffix}",
        method="POST",
        path=IC_PATH,
        mux="internal",
        auth="jwtBootstrap",
        body={"name": f"ic-secret-{name_suffix}-{{{{runId}}}}", "redirectUris": [REDIRECT]},
        test_script=[
            *assert_status(200),
            "const j = pm.response.json();",
            "pm.test('Create answers an Operation that is done without an error', () => {",
            "  pm.expect(j.id, JSON.stringify(j)).to.match(/^iop[a-z0-9]+$/);",
            "  pm.expect(j.done, JSON.stringify(j)).to.eql(true);",
            "  pm.expect(j.error, JSON.stringify(j.error || {})).to.be.undefined;",
            "});",
            *save_from_response("j.metadata && j.metadata.interactiveClientId", f"{v}Id"),
            *save_from_response("j.response && j.response.interactiveClient && "
                                "j.response.interactiveClient.clientId", f"{v}ClientId"),
            *save_from_response("j.response && j.response.clientSecret", f"{v}Secret"),
            "pm.test('the one-time secret was captured (every later step compares by its value)', () =>",
            f"  pm.expect(String(pm.environment.get({js_str(v + 'Secret')}) || ''), 'no secret in the Create answer')"
            ".to.have.length.above(0));",
            *extra_asserts,
        ],
    )


# Форма ответа вызова Create по IC-SECRET-01 — утверждается по ТЕЛУ ВЫЗОВА: секрет
# выдаётся ровно в нём, и ни в одном последующем чтении его нет.
_S01_CREATE_ANSWER = [
    "pm.test('IC-SECRET-01: response is a CreateInteractiveClientResponse', () =>",
    "  pm.expect(String((j.response || {})['@type'] || ''), JSON.stringify(j))"
    ".to.match(/\\.CreateInteractiveClientResponse$/));",
    "const k = (j.response || {}).interactiveClient || {};",
    "pm.test('IC-SECRET-01: the client id is the ic- form of 17 crockford characters', () =>",
    f"  pm.expect(k.id, JSON.stringify(j)).to.match({_ID_RE}));",
    "pm.test('IC-SECRET-01: clientId is non-empty and carries the oic- prefix', () =>",
    "  pm.expect(String(k.clientId || ''), JSON.stringify(k)).to.match(/^oic-.+/));",
    "pm.test('IC-SECRET-01: the method is client_secret_basic (a confidential client)', () =>",
    "  pm.expect(k.tokenEndpointAuthMethod, JSON.stringify(k)).to.eql('client_secret_basic'));",
    "pm.test('IC-SECRET-01: grantTypes are exactly authorization_code and refresh_token', () =>",
    "  pm.expect(k.grantTypes, JSON.stringify(k)).to.eql(['authorization_code', 'refresh_token']));",
    "pm.test('IC-SECRET-01: status ACTIVE', () => pm.expect(k.status, JSON.stringify(k)).to.eql('ACTIVE'));",
    *assert_created_at_seconds("((pm.response.json().response || {}).interactiveClient || {}).createdAt"),
    "pm.test('IC-SECRET-01: response.clientSecret is non-empty', () =>",
    "  pm.expect(String((j.response || {}).clientSecret || '')).to.have.length.above(0));",
]


def _basic_pre(v, client_expr, secret_expr):
    """Пред-скрипт предъявления: `Authorization: Basic` от пары по RFC 6749 §2.3.1.

    Значения известны только при прогоне (их выдал `Create`), поэтому заголовок
    собирает пред-скрипт, а не литерал коллекции. Пустое значение — ОТКАЗ по
    имени переменной, а не отправка: предъявление без секрета было бы другой
    веткой (IC-SECRET-06 (а)), и кейс проверил бы не то, что назвал.
    """
    return [
        "const _cj = require('crypto-js');",
        f"const _cid = String({client_expr} || '');",
        f"const _sec = String({secret_expr} || '');",
        "if (!_cid || !_sec) {",
        "  pm.test('the pair to present is known before it is presented', () =>"
        " pm.expect.fail('clientId or secret was not captured by the Create of this case — "
        "the step is not sent, a presentation without a secret is a different branch'));",
        "  pm.execution.skipRequest();",
        "} else {",
        "  const _b = _cj.enc.Base64.stringify(_cj.enc.Utf8.parse("
        "encodeURIComponent(_cid) + ':' + encodeURIComponent(_sec)));",
        "  pm.request.headers.upsert({key: 'Authorization', value: 'Basic ' + _b});",
        "}",
    ]


def _present(name, v, *, client_expr=None, secret_expr=None, expect, save_body_as=None,
             equal_to=()):
    """Шаг предъявления пары на координате выдачи внешнего слушателя края.

    expect = "proven"   → 400 `invalid_grant`: клиент доказан, отвергнут код;
    expect = "refused"  → 401 `invalid_client`: клиент не доказан.
    `equal_to` — имена переменных с телами прежних отказов, которым этот ответ
    обязан быть равен ПОБАЙТОВО (IC-SECRET-10, Р6).
    """
    client_expr = client_expr or f"pm.environment.get({js_str(v + 'ClientId')})"
    secret_expr = secret_expr or f"pm.environment.get({js_str(v + 'Secret')})"
    if expect == "proven":
        verdict = [
            *assert_status(400),
            f"pm.test({js_str(name + ': the client is proven and the never-issued code is what is refused')}, () => {{",
            "  const j = pm.response.json();",
            "  pm.expect(j.error, pm.response.text()).to.eql('invalid_grant');",
            "});",
        ]
    elif expect == "refused":
        verdict = [
            *assert_status(401),
            f"pm.test({js_str(name + ': the client is NOT proven (invalid_client)')}, () => {{",
            "  const j = pm.response.json();",
            "  pm.expect(j.error, pm.response.text()).to.eql('invalid_client');",
            "});",
        ]
    else:
        raise ValueError(f"_present {name!r}: unknown expectation {expect!r}")
    for var in equal_to:
        verdict.append(
            f"pm.test({js_str(name + ': byte-identical to the earlier refusal kept in ' + var)}, () =>"
            f" pm.expect(pm.response.text()).to.eql(String(pm.environment.get({js_str(var)}) || '<not captured>')));")
    if save_body_as:
        verdict.append(f"pm.environment.set({js_str(save_body_as)}, pm.response.text());")
    return Step(
        name=name,
        method="POST",
        path="/iam/v1/token",
        mux="external",
        insecure_tls=True,
        form=[("grant_type", "authorization_code"), ("code", _NEVER_ISSUED_CODE),
              ("redirect_uri", REDIRECT), ("code_verifier", _VERIFIER)],
        pre_script=_basic_pre(v, client_expr, secret_expr),
        test_script=[*assert_answered(name), *verdict],
    )


def _wrong_secret_expr(v):
    """S′ — секрет S с одним изменённым знаком (последним)."""
    s = f"String(pm.environment.get({js_str(v + 'Secret')}) || '')"
    return f"({s}.slice(0, -1) + ({s}.slice(-1) === 'A' ? 'B' : 'A'))"


def _no_secret_in_body(label, v):
    """Тело ответа не несёт S НИ КАК ПОДСТРОКУ — сверка по ЗНАЧЕНИЮ, не по имени."""
    return [
        f"pm.test({js_str(label + ': the body does not carry the secret, compared by its value')}, () => {{",
        f"  const s = String(pm.environment.get({js_str(v + 'Secret')}) || '');",
        "  pm.expect(s, 'the secret of this case was not captured — nothing to compare against')"
        ".to.have.length.above(0);",
        "  pm.expect(pm.response.text().indexOf(s), 'the secret is in the body').to.eql(-1);",
        "});",
    ]


def _cleanup(v):
    path = f"{IC_PATH}/{{{{{v}Id}}}}"
    return Step(
        name=f"cleanup-{v}", method="DELETE", path=path, mux="internal", auth="jwtBootstrap",
        test_script=[
            *assert_status(200),
            "pm.test('teardown delete is done without an operation error', () => {",
            "  const j = pm.response.json();",
            "  pm.expect(j.done, JSON.stringify(j)).to.eql(true);",
            "  pm.expect(j.error, JSON.stringify(j.error || {})).to.be.undefined;",
            "});",
        ],
    )


# ===========================================================================
# IC-SECRET-01 — заведение на посадке own выдаёт секрет в ответе Create.
# ===========================================================================

CASES.append(Case(
    id="IAM-IC-SECRET-01-CR-ISSUES-SECRET",
    title="IC-SECRET-01: Create on the own landing answers CreateInteractiveClientResponse with a "
          "client_secret_basic client and a non-empty clientSecret; Get describes the same row",
    classes=["CRUD", "SEC"],
    priority="P0",
    steps=[
        _create("s01", "icS01", extra_asserts=_S01_CREATE_ANSWER),
        Step(
            name="get-after-create-s01",
            method="GET",
            path=f"{IC_PATH}/{{{{icS01Id}}}}",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "const g = pm.response.json();",
                "pm.test('IC-SECRET-01: Get describes the row Create answered with (id, clientId, method)', () => {",
                "  pm.expect(g.id, JSON.stringify(g)).to.eql(pm.environment.get('icS01Id'));",
                "  pm.expect(g.clientId).to.eql(pm.environment.get('icS01ClientId'));",
                "  pm.expect(g.tokenEndpointAuthMethod).to.eql('client_secret_basic');",
                "});",
                # Близнец — IC-SECRET-08: то же чтение секрета не несёт; отличие в
                # одном факте — читается ответ вызова либо ресурс.
                *_no_secret_in_body("IC-SECRET-01 twin (08)", "icS01"),
            ],
        ),
        _cleanup("icS01"),
    ],
))


# ===========================================================================
# IC-SECRET-04 — несостоявшееся заведение секрета не выдаёт и материал первого не трогает.
# ===========================================================================

CASES.append(Case(
    id="IAM-IC-SECRET-04-CR-CONF-DUP-NAME",
    title="IC-SECRET-04: a second Create under a taken name → sync 409 ALREADY_EXISTS without a "
          "secret; the list holds exactly one client under that name; the first secret still "
          "proves its client",
    classes=["CONF", "NEG", "SEC"],
    priority="P0",
    steps=[
        # Близнец — заведение с новым именем: секрет выдан (отличие в одном факте —
        # имя занято).
        _create("s04", "icS04"),
        _present("present-before-duplicate-s04", "icS04", expect="proven"),
        Step(
            name="create-duplicate-name-s04",
            method="POST",
            path=IC_PATH,
            mux="internal",
            auth="jwtBootstrap",
            body={"name": "ic-secret-s04-{{runId}}", "redirectUris": [REDIRECT]},
            test_script=[
                *assert_status(409),
                *assert_grpc_code(6, "ALREADY_EXISTS"),
                "const j = pm.response.json();",
                "pm.test('IC-SECRET-04: the refusal is synchronous — no Operation comes back', () =>",
                "  pm.expect(j.done, JSON.stringify(j)).to.be.undefined);",
                "pm.test('IC-SECRET-04: no clientSecret in the refusal in any form', () =>",
                "  pm.expect(j.clientSecret, JSON.stringify(j)).to.be.undefined);",
                *_no_secret_in_body("IC-SECRET-04", "icS04"),
            ],
        ),
        Step(
            name="list-by-taken-name-s04",
            method="GET",
            path=IC_PATH + "?filter=name%3D%22ic-secret-s04-{{runId}}%22",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.test('IC-SECRET-04: exactly one client carries the name, and it is the first one', () => {",
                "  const items = pm.response.json().interactiveClients || [];",
                "  pm.expect(items.length, pm.response.text()).to.eql(1);",
                "  pm.expect(items[0].id).to.eql(pm.environment.get('icS04Id'));",
                "});",
            ],
        ),
        _present("present-after-duplicate-s04", "icS04", expect="proven"),
        _cleanup("icS04"),
    ],
))


# ===========================================================================
# IC-SECRET-08 — Get секрета не несёт.
# ===========================================================================

CASES.append(Case(
    id="IAM-IC-SECRET-08-GT-NO-SECRET",
    title="IC-SECRET-08: Get answers the client_secret_basic resource without the secret (compared "
          "by value); at the same moment the secret proves the client and a one-character-off "
          "secret does not",
    classes=["SEC", "NEG"],
    priority="P0",
    steps=[
        _create("s08", "icS08"),
        Step(
            name="get-s08",
            method="GET",
            path=f"{IC_PATH}/{{{{icS08Id}}}}",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "const g = pm.response.json();",
                "pm.test('IC-SECRET-08: the resource of this client, method client_secret_basic', () => {",
                "  pm.expect(g.id, JSON.stringify(g)).to.eql(pm.environment.get('icS08Id'));",
                "  pm.expect(g.clientId).to.eql(pm.environment.get('icS08ClientId'));",
                "  pm.expect(g.tokenEndpointAuthMethod).to.eql('client_secret_basic');",
                "});",
                *_no_secret_in_body("IC-SECRET-08", "icS08"),
                "pm.test('IC-SECRET-08: the resource message has no field for a secret or its verifier', () => {",
                "  pm.expect(g.clientSecret, JSON.stringify(g)).to.be.undefined;",
                "  pm.expect(g.secretVerifier, JSON.stringify(g)).to.be.undefined;",
                "});",
            ],
        ),
        # Положительный контроль: секрет существует и доказывает клиента в тот же
        # момент — его просто нет в ответе чтения. Соседний неверный — не доказывает.
        _present("present-secret-s08", "icS08", expect="proven"),
        _present("present-wrong-secret-s08", "icS08", secret_expr=_wrong_secret_expr("icS08"),
                 expect="refused"),
        _cleanup("icS08"),
    ],
))


# ===========================================================================
# IC-SECRET-09 — List и Update секрета не несут; маской его не задать и способ не переключить.
# ===========================================================================

def _patch(name, v, mask, extra=None, test_script=None):
    body = {"updateMask": mask}
    body.update(extra or {})
    return Step(name=name, method="PATCH", path=f"{IC_PATH}/{{{{{v}Id}}}}", mux="internal",
                auth="jwtBootstrap", body=body, test_script=test_script or [])


CASES.append(Case(
    id="IAM-IC-SECRET-09-LS-UP-NO-SECRET",
    title="IC-SECRET-09: the List page carries the client in the Get projection without the secret; "
          "a description update answers without the secret and leaves it working; the mask cannot "
          "name clientSecret (unknown) nor switch tokenEndpointAuthMethod (immutable)",
    classes=["SEC", "VAL", "NEG"],
    priority="P0",
    steps=[
        _create("s09", "icS09"),
        Step(
            name="get-projection-s09",
            method="GET",
            path=f"{IC_PATH}/{{{{icS09Id}}}}",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.environment.set('icS09GetBody', JSON.stringify(pm.response.json()));",
            ],
        ),
        # (а) страница List.
        Step(
            name="list-s09",
            method="GET",
            path=IC_PATH + "?filter=name%3D%22ic-secret-s09-{{runId}}%22",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.test('IC-SECRET-09 (a): the page carries K in the same projection as Get', () => {",
                "  const items = pm.response.json().interactiveClients || [];",
                "  const k = items.find(c => c.id === pm.environment.get('icS09Id'));",
                "  pm.expect(k, pm.response.text()).to.be.an('object');",
                "  pm.expect(JSON.stringify(k)).to.eql(pm.environment.get('icS09GetBody'));",
                "});",
                *_no_secret_in_body("IC-SECRET-09 (a)", "icS09"),
            ],
        ),
        # (б) маска известного изменяемого поля — близнец (в) и (г).
        _patch("update-description-s09", "icS09", "description",
               {"description": "ic-secret-09-changed"},
               test_script=[
                   *assert_status(200),
                   "const j = pm.response.json();",
                   "pm.test('IC-SECRET-09 (b): the Operation is done without an error', () => {",
                   "  pm.expect(j.done, JSON.stringify(j)).to.eql(true);",
                   "  pm.expect(j.error, JSON.stringify(j.error || {})).to.be.undefined;",
                   "});",
                   "pm.test('IC-SECRET-09 (b): the response is the resource with the new description', () => {",
                   "  pm.expect(j.response && j.response.id, JSON.stringify(j)).to.eql(pm.environment.get('icS09Id'));",
                   "  pm.expect(j.response.description).to.eql('ic-secret-09-changed');",
                   "});",
                   *_no_secret_in_body("IC-SECRET-09 (b)", "icS09"),
               ]),
        _present("present-after-update-s09", "icS09", expect="proven"),
        # (в) маска называет секрет — неизвестное поле, тоном любого неизвестного.
        _patch("update-mask-client-secret-s09", "icS09", "clientSecret",
               test_script=[
                   *assert_status(400),
                   *assert_grpc_code(3, "INVALID_ARGUMENT"),
                   "pm.test('IC-SECRET-09 (c): the status message is the generic invalid argument', () =>",
                   "  pm.expect(pm.response.json().message, pm.response.text()).to.eql('invalid argument'));",
                   *assert_field_violation("update_mask"),
                   "pm.test('IC-SECRET-09 (c): the BadRequest detail names client_secret as unknown', () => {",
                   "  const j = pm.response.json();",
                   "  const det = (j.details || []).find(d => (d['@type'] || '').includes('BadRequest'));",
                   "  const fv = ((det || {}).fieldViolations || [])[0] || {};",
                   "  pm.expect(fv.field, JSON.stringify(j)).to.eql('update_mask');",
                   "  pm.expect(fv.description).to.eql('unknown field in update_mask: client_secret');",
                   "});",
               ]),
        # (г) маска переключает способ — неизменяемое после заведения.
        _patch("update-mask-auth-method-s09", "icS09", "tokenEndpointAuthMethod",
               test_script=[
                   *assert_status(400),
                   *assert_grpc_code(3, "INVALID_ARGUMENT"),
                   "pm.test('IC-SECRET-09 (d): the status message names the method as immutable', () =>",
                   "  pm.expect(pm.response.json().message, pm.response.text())"
                   ".to.eql('token_endpoint_auth_method is immutable after InteractiveClient.Create'));",
                   "pm.test('IC-SECRET-09 (d): the update_mask violation carries the same sentence', () => {",
                   "  const j = pm.response.json();",
                   "  const det = (j.details || []).find(d => (d['@type'] || '').includes('BadRequest'));",
                   "  const fv = ((det || {}).fieldViolations || []).find(x => x.field === 'update_mask') || {};",
                   "  pm.expect(fv.description, JSON.stringify(j))"
                   ".to.eql('token_endpoint_auth_method is immutable after InteractiveClient.Create');",
                   "});",
               ]),
        Step(
            name="get-after-refused-masks-s09",
            method="GET",
            path=f"{IC_PATH}/{{{{icS09Id}}}}",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.test('IC-SECRET-09: the method was not switched by the refused mask', () =>",
                "  pm.expect(pm.response.json().tokenEndpointAuthMethod, pm.response.text())"
                ".to.eql('client_secret_basic'));",
            ],
        ),
        _present("present-after-refused-masks-s09", "icS09", expect="proven"),
        _cleanup("icS09"),
    ],
))


# ===========================================================================
# IC-SECRET-10 — снятие не оставляет годного секрета.
# ===========================================================================

CASES.append(Case(
    id="IAM-IC-SECRET-10-DL-NO-USABLE-SECRET",
    title="IC-SECRET-10: Delete echoes the resource without the secret; afterwards the secret gets "
          "invalid_client byte-identical to a wrong secret and to a never-created client; Get → 404; "
          "a repeated Delete has the same outcome",
    classes=["SEC", "STATE", "IDM"],
    priority="P0",
    steps=[
        _create("s10", "icS10"),
        # Близнец: то же предъявление ДО снятия доказывает клиента. Отличие в одном
        # факте — клиент снят.
        _present("present-before-delete-s10", "icS10", expect="proven"),
        # Эталоны побайтового равенства (Р6): неверный секрет живого клиента и
        # клиент, которого не заводили никогда.
        _present("present-wrong-secret-s10", "icS10", secret_expr=_wrong_secret_expr("icS10"),
                 expect="refused", save_body_as="icS10WrongSecretBody"),
        _present("present-never-created-client-s10", "icS10",
                 client_expr=js_str(_NEVER_CREATED_CLIENT_ID), expect="refused",
                 save_body_as="icS10NeverCreatedBody"),
        Step(
            name="delete-s10",
            method="DELETE",
            path=f"{IC_PATH}/{{{{icS10Id}}}}",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('IC-SECRET-10: the Operation is done without an error', () => {",
                "  pm.expect(j.done, JSON.stringify(j)).to.eql(true);",
                "  pm.expect(j.error, JSON.stringify(j.error || {})).to.be.undefined;",
                "});",
                "pm.test('IC-SECRET-10: the response echoes the removed resource', () =>",
                "  pm.expect(j.response && j.response.id, JSON.stringify(j)).to.eql(pm.environment.get('icS10Id')));",
                *_no_secret_in_body("IC-SECRET-10", "icS10"),
            ],
        ),
        _present("present-after-delete-s10", "icS10", expect="refused",
                 equal_to=("icS10WrongSecretBody", "icS10NeverCreatedBody")),
        Step(
            name="get-after-delete-s10",
            method="GET",
            path=f"{IC_PATH}/{{{{icS10Id}}}}",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(404),
                *assert_grpc_code(5, "NOT_FOUND"),
                "pm.test('IC-SECRET-10: gone, in the contract tone with the id', () =>",
                "  pm.expect(pm.response.json().message, pm.response.text())"
                ".to.eql('InteractiveClient ' + pm.environment.get('icS10Id') + ' not found'));",
            ],
        ),
        Step(
            name="delete-again-s10",
            method="DELETE",
            path=f"{IC_PATH}/{{{{icS10Id}}}}",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('IC-SECRET-10: the repeated Delete has the same outcome (done, no error)', () => {",
                "  pm.expect(j.done, JSON.stringify(j)).to.eql(true);",
                "  pm.expect(j.error, JSON.stringify(j.error || {})).to.be.undefined;",
                "});",
                *_no_secret_in_body("IC-SECRET-10 repeat", "icS10"),
            ],
        ),
    ],
))
