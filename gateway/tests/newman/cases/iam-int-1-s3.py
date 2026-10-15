# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set iam-int-1-s3 — вердикт края по полу ступени не зависит от класса носителя.

ЧТО СУДИТСЯ. Сценарии IAM-INT-1-24 и IAM-INT-1-25 одобренной приёмки интерактивного
входа (`docs/specs/sub-phase-IAM-INT-1-interactive-login-capability-acceptance.md`
воркспейса, §3, стадия S3; verifies PRO-Robotech/kacho#2571). Приёмка разрешает их
писать только после входного гейта стадии: посев предъявляет ОДНОГО человека краю
тремя классами носителей — сессией браузера, подписанным предъявителем и базовым
удостоверением, выданным ему самому, — и показывает, что все три доходят до
принципала. Гейт держит посев `tests/authz-fixtures/seed_ceremony.py`
(PRO-Robotech/kacho#3159), он же пишет ключи `s3*`, которые читает этот модуль.
Без них каждый шаг падает с меткой «условие не создано» и не отправляется.

ПРЕДМЕТ И НОСИТЕЛЬ ПОЛА. Глагол с полом «2» — `GroupService.Delete`, с полом «1» —
`GroupService.Get` (каталог прав края, `gateway/internal/middleware/embed/
permission_catalog.json`). Группа заводится в аккаунте самого человека и только
носит пол: модуль не утверждает о группе ничего, кроме того, дошёл ли запрос до
владельца RPC — после трёх отказов группа на месте, после прохода её нет.

ОДИН ФАКТ МЕЖДУ ДВУМЯ ПОЛОВИНАМИ ПАРЫ. Человек, группа и глагол одни; меняется класс
носителя (24.1–24.3, 25.1–25.2) либо пол глагола (24.4). Носители уровня «1» посев
выдал ДО заведения второго фактора и после заведения подтвердил их уровень, поэтому
сценарий 24 судит носителя человека без фактора, а 25 — того же человека с фактором.

ДОСЛОВНЫЕ ТЕКСТЫ ДВУХ ОТКАЗОВ НЕ УТВЕРЖДАЮТСЯ — ОНИ СПОРНЫ МЕЖДУ РЕШЕНИЯМИ. Одобренная
редакция выписывает тело отказа по полу `{"code":16,"message":"insufficient_user_
authentication"}` (24.2) и текст единого отказа полосы базового удостоверения
`credential refused` с `error_description` (24.5). Край отвечает по позднейшим решениям:
текст отказа по полу называет следующий шаг (PRO-Robotech/kaname#511), а единый отказ
`401` — один на все причины и все полосы, без `error_description` (решение Р2 приёмки
KA1, `docs/specs/sub-phase-KA1-edge-refusals-and-call-budgets-acceptance.md` воркспейса,
которая сама называет текст 24.5 переписываемым). Пока редакции не сведены, кейс держит
то, в чём сходятся оба документа и край: код `16`, признаки вызова (`error=
"insufficient_user_authentication"`, `acr_values="2"` у повышения; `error="invalid_token"`
без повышения у базового удостоверения) и побайтовое РАВЕНСТВО отказов, ради которого
сценарии и написаны (24.2, 24.3, 25.2). Расхождение текстов возвращено вопросом к
приёмке (PRO-Robotech/kacho#2571).

Coverage:
  IAM-INT-1-24-STEPUP-REFUSAL-EVERY-CLASS — 24.1 все три носителя «1» на пол «2» → 401, группа
                                            цела; 24.2 сессия и предъявитель — один и тот же отказ
                                            с вызовом повышения; 24.3 базовое удостоверение —
                                            отказ побайтово равен отказу на подделку той же марки;
                                            24.4 те же три на пол «1» → 200; 24.5 текст единого
                                            отказа полосы базового удостоверения
  IAM-INT-1-25-STEPUP-PASS-RAISED-CLASSES — 25.1 сессия и предъявитель «2» на пол «2» → 200,
                                            операция без ошибки; 25.2 базовое удостоверение того же
                                            человека — тот же единый отказ, что в 24.3

Техники: таблица решений (класс носителя × уровень × пол), эквивалентные классы
носителей, парная проба (один изменённый факт), угадывание ошибок (подделка той же
марки неотличима от отказа по полу).
"""

ASSERTS_DOMAIN = "gateway"
ASSERTS_REASON = (
    "предмет — вердикт края по полу ступени на трёх полосах, устанавливающих личность "
    "(сессия, подписанный предъявитель, базовое удостоверение): пол объявляет каталог прав "
    "края, полосы и отказ производит край. Маршрут `/iam/v1/groups` — только носитель пола «2» "
    "(Delete) и «1» (Get); о группе как ресурсе службы модуль не утверждает ничего"
)

CASES = []

GROUPS = "/iam/v1/groups"
SESSION_COOKIE = "kaname_session"

_SEED_WHY = "носитель гейта S3 — посев tests/authz-fixtures/seed_ceremony.py (kacho#3159)"


def _carrier_pre(kind, var):
    """Предъявление носителя шагом: сессия — печеньем, прочие — `Authorization: Bearer`.

    Нет значения — шаг НЕ отправляется, утверждение с меткой третьего исхода называет
    переменную: шаг без носителя проверял бы анонимного вызывающего.
    """
    lines = [*require_env_slot(var, _SEED_WHY),
             f"const __c = pm.environment.get({js_str(var)}) || '';",
             "pm.request.headers.remove('Authorization');",
             "pm.request.headers.remove('Cookie');"]
    if kind == "session":
        lines.append(f"if (__c) pm.request.headers.upsert({{key: 'Cookie', value: {js_str(SESSION_COOKIE + '=')} + __c}});")
    else:
        lines.append("if (__c) pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __c});")
    return lines


def _create_group(slot, carrier_kind, carrier_var):
    """Группа в аккаунте человека — носитель пола; заводится глаголом пола «1»."""
    return [
        Step(
            name=f"create-{slot}",
            method="POST",
            path=GROUPS,
            body={"accountId": "{{s3AccountId}}", "name": f"s3-{slot}-{{{{runId}}}}",
                  "description": "носитель пола для IAM-INT-1-24/25"},
            pre_script=[*require_env_slot("s3AccountId", _SEED_WHY), *_carrier_pre(carrier_kind, carrier_var)],
            no_cookie_jar=True,
            test_script=[
                *assert_answered(f"заведение группы {slot}"),
                *assert_status(200),
                *assert_iam_operation_envelope(),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.groupId", f"{slot}Id"),
                f"pm.test('the Operation names the group id', () => pm.expect(String(pm.environment.get({js_str(slot + 'Id')}) || '')).to.match(/^[a-z0-9-]+$/));",
            ],
        ),
        # Операцию человека читает он сам: чужая операция неотличима от несуществующей
        # (у служебной учётки кластера тоже). Чтение операции пола не несёт.
        poll_operation("opId", auth="s3BearerL1", name=f"poll-create-{slot}"),
    ]


def _floor2_refused(name, slot, kind, var, save_as):
    """Пол «2» носителем уровня «1»: 401; тело, вызов и статус сохраняются для сверки."""
    return Step(
        name=name,
        method="DELETE",
        path=f"{GROUPS}/{{{{{slot}Id}}}}",
        pre_script=_carrier_pre(kind, var),
        no_cookie_jar=True,
        test_script=[
            *assert_answered(name),
            *assert_status(401),
            f"pm.environment.set({js_str(save_as + 'Body')}, pm.response.text());",
            f"pm.environment.set({js_str(save_as + 'Challenge')}, String(pm.response.headers.get('WWW-Authenticate') || ''));",
        ],
    )


def _floor1_ok(name, slot, kind, var):
    return Step(
        name=name,
        method="GET",
        path=f"{GROUPS}/{{{{{slot}Id}}}}",
        pre_script=_carrier_pre(kind, var),
        no_cookie_jar=True,
        test_script=[
            *assert_answered(name),
            *assert_status(200),
            f"pm.test('the group read is the group this case created', () => pm.expect(pm.response.json().id).to.eql(pm.environment.get({js_str(slot + 'Id')})));",
        ],
    )


def _floor2_passes(name, slot, kind, var):
    return [
        Step(
            name=name,
            method="DELETE",
            path=f"{GROUPS}/{{{{{slot}Id}}}}",
            pre_script=_carrier_pre(kind, var),
        no_cookie_jar=True,
            test_script=[
                *assert_answered(name),
                *assert_status(200),
                *assert_iam_operation_envelope(),
                *save_from_response("j.id", "opId"),
            ],
        ),
        poll_operation("opId", auth="s3BearerL1", name=f"poll-{name}"),
        Step(
            name=f"{name}-gone",
            method="GET",
            path=f"{GROUPS}/{{{{{slot}Id}}}}",
            auth="jwtBootstrap",
            test_script=[*assert_answered(f"{name}: группа снята"), *assert_status(404)],
        ),
    ]


_STEP_UP_SHAPE = [
    "let __j; try { __j = JSON.parse(pm.environment.get('s3RefSessionBody') || ''); } catch (e) { __j = {}; }",
    "const __ch = String(pm.environment.get('s3RefSessionChallenge') || '');",
    "pm.test('24.2: the session refusal is code 16 (UNAUTHENTICATED)', () =>",
    "  pm.expect(__j.code, JSON.stringify(__j)).to.eql(16));",
    "pm.test('24.2: the session challenge names insufficient_user_authentication and acr_values=\"2\"', () => {",
    "  pm.expect(__ch).to.include('error=\"insufficient_user_authentication\"');",
    "  pm.expect(__ch).to.include('acr_values=\"2\"');",
    "});",
    "pm.test('24.2: the signed bearer gets byte-for-byte the SAME refusal and challenge as the session', () => {",
    "  pm.expect(pm.environment.get('s3RefBearerBody')).to.eql(pm.environment.get('s3RefSessionBody'));",
    "  pm.expect(pm.environment.get('s3RefBearerChallenge')).to.eql(pm.environment.get('s3RefSessionChallenge'));",
    "});",
]

_BASIC_SHAPE = [
    "pm.test('24.3: the floor refusal of the basic credential is byte-for-byte the refusal of a tampered secret of the same mark', () => {",
    "  pm.expect(pm.environment.get('s3RefBasicBody')).to.eql(pm.environment.get('s3RefTamperedBody'));",
    "  pm.expect(pm.environment.get('s3RefBasicChallenge')).to.eql(pm.environment.get('s3RefTamperedChallenge'));",
    "});",
    "let __b; try { __b = JSON.parse(pm.environment.get('s3RefBasicBody') || ''); } catch (e) { __b = {}; }",
    "pm.test('24.5: the single refusal of the basic lane is code 16 (UNAUTHENTICATED)', () =>",
    "  pm.expect(__b.code, JSON.stringify(__b)).to.eql(16));",
    "pm.test('24.5: its challenge is invalid_token and carries no step-up call', () => {",
    "  const __c = String(pm.environment.get('s3RefBasicChallenge') || '');",
    "  pm.expect(__c).to.include('error=\"invalid_token\"');",
    "  pm.expect(__c).to.not.include('insufficient_user_authentication');",
    "  pm.expect(__c).to.not.include('acr_values');",
    "});",
]


def _verdict_step(name, script):
    """Сверка сохранённых ответов: шаг — чтение группы бутстрапом, предмет — скрипт."""
    return Step(
        name=name,
        method="GET",
        path=f"{GROUPS}/{{{{s3g24Id}}}}",
        auth="jwtBootstrap",
        test_script=[*assert_answered(name), *script],
    )


CASES.append(Case(
    id="IAM-INT-1-24-STEPUP-REFUSAL-EVERY-CLASS",
    title=("IAM-INT-1-24: человек без второго фактора тремя классами носителей на пол «2» — "
           "один вердикт 401, до владельца не доходит ни один; на пол «1» — 200 всеми тремя"),
    classes=["AUTHN", "AUTHZ", "CONF"],
    priority="P1",
    steps=[
        *_create_group("s3g24", "session", "s3SessionL1"),
        # 24.4 — положительный контроль: те же три носителя на пол «1».
        _floor1_ok("24.4-get-session-l1", "s3g24", "session", "s3SessionL1"),
        _floor1_ok("24.4-get-bearer-l1", "s3g24", "bearer", "s3BearerL1"),
        _floor1_ok("24.4-get-basic", "s3g24", "basic", "s3Basic"),
        # 24.1 — пол «2» каждым классом: 401.
        _floor2_refused("24.1-delete-session-l1", "s3g24", "session", "s3SessionL1", "s3RefSession"),
        _floor2_refused("24.1-delete-bearer-l1", "s3g24", "bearer", "s3BearerL1", "s3RefBearer"),
        _floor2_refused("24.1-delete-basic", "s3g24", "basic", "s3Basic", "s3RefBasic"),
        # 24.3 — близнец: подделка той же марки на тот же глагол.
        _floor2_refused("24.3-delete-tampered-basic", "s3g24", "basic", "s3BasicTampered", "s3RefTampered"),
        # 24.1 — ни один отказ не дошёл до владельца: группа на месте.
        _verdict_step("24.1-group-still-there", [
            *assert_status(200),
            "pm.test('24.1: none of the three refusals reached the owner — the group is still there', () =>"
            " pm.expect(pm.response.json().id).to.eql(pm.environment.get('s3g24Id')));",
        ]),
        _verdict_step("24.2-same-refusal-session-bearer", _STEP_UP_SHAPE),
        _verdict_step("24.3-24.5-basic-single-refusal", _BASIC_SHAPE),
        # Снятие носителем «2» — тем, что пол проходит.
        *_floor2_passes("cleanup-delete-s3g24", "s3g24", "session", "s3SessionL2"),
    ],
))


CASES.append(Case(
    id="IAM-INT-1-25-STEPUP-PASS-RAISED-CLASSES",
    title=("IAM-INT-1-25: тот же человек со вторым фактором — сессия и предъявитель «2» проходят "
           "пол «2»; базовое удостоверение получает тот же единый отказ, что в 24.3"),
    classes=["AUTHN", "AUTHZ", "CONF"],
    priority="P1",
    steps=[
        *_create_group("s3g25a", "session", "s3SessionL1"),
        *_create_group("s3g25b", "session", "s3SessionL1"),
        # 25.2 — третий класс на тот же глагол: единый отказ полосы.
        _floor2_refused("25.2-delete-basic", "s3g25b", "basic", "s3Basic", "s3Ref25Basic"),
        _floor2_refused("25.2-delete-tampered-basic", "s3g25b", "basic", "s3BasicTampered", "s3Ref25Tampered"),
        Step(
            name="25.2-same-single-refusal",
            method="GET",
            path=f"{GROUPS}/{{{{s3g25bId}}}}",
            auth="jwtBootstrap",
            test_script=[
                *assert_answered("25.2-same-single-refusal"),
                *assert_status(200),
                "pm.test('25.2: the refused request did not reach the owner — the group is still there', () =>"
                " pm.expect(pm.response.json().id).to.eql(pm.environment.get('s3g25bId')));",
                "pm.test('25.2: the basic credential of the raised human gets byte-for-byte the single refusal of its lane (as a tampered secret of the same mark)', () => {",
                "  pm.expect(pm.environment.get('s3Ref25BasicBody')).to.eql(pm.environment.get('s3Ref25TamperedBody'));",
                "  pm.expect(pm.environment.get('s3Ref25BasicChallenge')).to.eql(pm.environment.get('s3Ref25TamperedChallenge'));",
                "});",
                "pm.test('25.2: the second factor of the human does not raise the basic credential — no step-up call', () =>",
                "  pm.expect(String(pm.environment.get('s3Ref25BasicChallenge') || '')).to.not.include('insufficient_user_authentication'));",
            ],
        ),
        # 25.1 — сессия и предъявитель «2»: 200, операция без ошибки, группы нет.
        *_floor2_passes("25.1-delete-bearer-l2", "s3g25a", "bearer", "s3BearerL2"),
        *_floor2_passes("25.1-delete-session-l2", "s3g25b", "session", "s3SessionL2"),
    ],
))
