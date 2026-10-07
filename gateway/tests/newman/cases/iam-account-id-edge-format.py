# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set iam-account-id-edge-format — ФОРМА ИДЕНТИФИКАТОРА АККАУНТА, КОТОРУЮ СУДИТ КРАЙ.

Перенесён из набора службы доступа (PRO-Robotech/kaname, коллекция того же имени;
держатель переноса — #2939, сторона службы — PRO-Robotech/kaname#415). Там модуль
не гонял ни один шаг конвейера: на собственном фронте службы у пары нет
производителя. Здесь его гоняет шаг `гейт — newman зелёный (api-gateway)`
(`.github/workflows/e2e-newman.yml`).

Два кейса, вынесенные службой при переезде своих модулей на собственный фронт
(PRO-Robotech/kaname#398):

  * `IAM-AB-SIA-12-EDGE-LIST-BY-ACCOUNT` — шаг паритета с `ListByAccount` из
    `IAM-AB-SIA-12-MALFORMED-NEG` (приёмка службы
    `subject-grants-within-an-account.md`, IAM-AB-SIA-12);
  * `IAM-ID2-NEG-FORM-EDGE-ACCOUNT-ID` — половина «идентификатор аккаунта судит
    край» из `IAM-ID2-NEG-FORM` (IAM-ID-2-04).

ПРЕДМЕТ — ШАГ КОРОТКОГО ЗАМЫКАНИЯ КРАЯ ПО ФОРМЕ. У обоих глаголов идентификатор
аккаунта стоит в ПУТИ и служит ЦЕЛЬЮ АВТОРИЗАЦИИ: в каталоге прав края у
`AccessBindingService/ListByAccount` и `MembershipService/List` извлекатель области
`object_type: account`, `from_request_field: account_id`. Для такой записи край
проверяет форму идентификатора ДО проверки прав и отвечает `400` / `3`,
`invalid resource id '<X>'`, не обращаясь к службе
(`gateway/internal/middleware/authz.go`, шаг 5b «Malformed-id short-circuit»).
Годный по форме, но несуществующий идентификатор тот же шаг пропускает к проверке
прав, и та отвечает `403` / `7` — защита от чтения существования. Собственный
фронт службы шага формы не несёт и на негодном входе отвечает `403` / `7` (замер
службы 2026-09-30, PRO-Robotech/kaname#398): переписанное под `403`, утверждение
перестало бы быть утверждением о форме — поэтому оно живёт у края.

ПАРА, А НЕ ОДИНОЧНОЕ ОТРИЦАНИЕ. В каждом кейсе отказ стоит рядом с законным
близнецом, отличающимся РОВНО одним фактом — годна ли форма идентификатора: тот же
глагол, тот же маршрут, тот же предъявитель, идентификатор `acc` верной длины,
аккаунта под которым нет. Без близнеца «отказ по форме» неотличим от края,
отвергающего ЛЮБОЙ идентификатор аккаунта в этом пути.

ОТЛИЧИЯ ОТ ИСХОДНИКА — по одному доводу на каждое; строгость и «Тогда» исходных
шагов не изменены:

  * у `IAM-AB-SIA-12-EDGE-LIST-BY-ACCOUNT` в исходнике близнеца не было, он
    добавлен (та же форма, что у `iam-project-edge-format.py` по #2912). Исход
    близнеца выведен из устройства края (шаг 5b пропускает годную форму к проверке
    прав; отношение `v_list` у несуществующего аккаунта не выводится ничем) и
    стоит в паре с замеренным контролем второго кейса на соседнем маршруте той же
    области;
  * вызывающий без прав во втором кейсе — `jwtPureNoBindings`, выделенный
    никогда-не-выданный субъект наборов платформы (testing-newman, слой 4a), а не
    `jwtNoBindings` исходника: шаг утверждает исход «у вызывающего без единой
    выдачи», и держит это свойство именно этот слот окружения края;
  * сверка побайтового равенства тел сперва требует, чтобы тело было захвачено:
    незахваченное тело сравнивалось бы с `undefined`, и красное назвало бы не ту
    причину.

Техники: классы эквивалентности входа (форма вне канона / годная форма
несуществующего аккаунта), таблица решений «форма × права вызывающего» во втором
кейсе, угадывание ошибок (строка вне канона в позиции цели авторизации).

Чего модуль НЕ утверждает: как на тот же вход отвечает собственный фронт службы
и что служба отдаёт по аккаунту — это сущности службы, их дом — её репозиторий
(e2e-flow.md §7а воркспейса). Должен ли собственный фронт замыкать по форме до
проверки прав — предмет PRO-Robotech/kaname#168.
"""

# ЧЬЁ ПОВЕДЕНИЕ УТВЕРЖДАЕТ ЭТОТ МОДУЛЬ (e2e-flow.md §7а, решение владельца 2026-09-12).
# Единственный домен по REST-путям — `iam`, и маршруты здесь НОСИТЕЛИ. Сверяет
# `tests/newman/scripts/case_home_test.py`.
ASSERTS_DOMAIN = "gateway"
ASSERTS_REASON = (
    "предмет — короткое замыкание КРАЯ по форме идентификатора аккаунта, стоящего "
    "целью авторизации в пути: оно идёт в крае ДО проверки прав и отвечает 400/3 без "
    "обращения к службе; собственный фронт службы этого шага не несёт и на том же входе "
    "отвечает 403/7. Маршруты `/iam/v1/accounts/{id}/accessBindings` и "
    "`/iam/v1/accounts/{id}/memberships` — носители: ни выдач, ни членств как ресурсов "
    "службы модуль не утверждает ни одним шагом."
)

CASES = []

# Годный по форме, но несуществующий аккаунт — законный близнец рубежа формы
# (тот же литерал, что у исходника и у `iam-membership-read.py` службы). Тело из
# одних нулей отличимо от выпущенного идентификатора на глаз.
ABSENT_ACCOUNT = "acc00000000000000000"

# Имя переменной, в которую первый шаг второго кейса кладёт тело отказа.
_MALFORMED_BODY_VAR = "mbrMalformedAcctBody"


def _labelled_grpc_code(code, label):
    """Код rpc.Status с подписью, которая называет, ЗАЧЕМ шаг его ждёт.

    HTTP-статус утверждается отдельным `assert_status`: пара, а не одно из двух.
    Неразобранное тело даёт пустой объект, и утверждение о коде не выполняется.
    """
    return [
        f"pm.test({js_str(label)}, () => {{",
        "  let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
        f"  pm.expect(j.code, pm.response.text()).to.eql({code});",
        "});",
    ]


def _not_the_format_refusal(label):
    """Ответ близнеца дан НЕ шагом формы: в тексте нет отказа по идентификатору."""
    return [
        f"pm.test({js_str(label)}, () => {{",
        "  let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
        "  pm.expect(String(j.message || ''), pm.response.text())"
        ".to.not.include('invalid resource id');",
        "});",
    ]


# ---------------------------------------------------------------------------
# IAM-AB-SIA-12-EDGE-LIST-BY-ACCOUNT — у `ListByAccount` `account_id` — ЦЕЛЬ
# АВТОРИЗАЦИИ, и край судит её форму ДО модели прав, иначе отказ «пути нет»
# замаскировал бы 400 под 403. Отвечает КРАЙ своим нейтральным именем ресурса:
# `invalid resource id '<X>'`. Нейтральное имя — предмет задачи #1932: появится
# словарь имён — шаг покраснеет и позовёт к себе.
#
# СТРОГОСТЬ ПАРЫ ОСЛАБЛЯТЬ ЗАПРЕЩЕНО (testing.md: «malformed-id … не ослаблять»):
# `oneOf` был бы допуском на исход, которого край на этом входе не производит.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-AB-SIA-12-EDGE-LIST-BY-ACCOUNT",
    title="IAM-AB-SIA-12: GET /accounts/not-an-id/accessBindings → sync 400 INVALID_ARGUMENT (3), "
          "\"invalid resource id 'not-an-id'\" from the edge; a well-formed absent account on the "
          "same route passes on to the permission check (403)",
    classes=["NEG", "VAL"],
    priority="P0",
    steps=[
        Step(
            name="sia-malformed-parity-with-list-by-account",
            method="GET",
            path="/iam/v1/accounts/not-an-id/accessBindings?pageSize=100",
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
                "pm.test('у цели авторизации отказ формы производит край, и текст его', () =>",
                "  pm.expect(pm.response.json().message, pm.response.text())",
                "    .to.eql(\"invalid resource id 'not-an-id'\"));",
            ],
        ),
        # ЗАКОННЫЙ БЛИЗНЕЦ: тот же глагол, тот же предъявитель, тот же маршрут;
        # отличие в одном факте — форма идентификатора годна. Шаг короткого
        # замыкания пропускает запрос дальше, к проверке прав, а у предъявителя
        # нет отношения к аккаунту, которого нет.
        Step(
            name="sia-wellformed-absent-account-reaches-the-model",
            method="GET",
            path=f"/iam/v1/accounts/{ABSENT_ACCOUNT}/accessBindings?pageSize=100",
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(403),
                *assert_grpc_code(7, "PERMISSION_DENIED"),
                *_not_the_format_refusal(
                    "a well-formed account id is not answered by the format short-circuit"),
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# IAM-ID2-NEG-FORM-EDGE-ACCOUNT-ID (IAM-ID-2-04) — негодный accountId отвергает
# КРАЙ, до модели прав: исход не зависит от выданного вызывающему.
#
# Таблица решений «форма × права»: (негодна, держатель права) → 400;
# (негодна, без единой выдачи) → 400 с тем же телом; (годна, держатель права
# ДРУГОГО аккаунта) → 403. Третья строка — законный близнец первой.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-ID2-NEG-FORM-EDGE-ACCOUNT-ID",
    title="Malformed account id is refused by the EDGE before the permission model: 400 for the "
          "rights holder and for a caller without a single grant, bodies equal; a well-formed "
          "absent account passes on to the model (403)",
    classes=["NEG", "VAL"],
    priority="P0",
    steps=[
        Step(
            name="malformed-account-id-rights-holder",
            method="GET",
            path="/iam/v1/accounts/not-an-account/memberships",
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(400),
                *_labelled_grpc_code(3, "негодный accountId — отказ КРАЯ, до модели прав"),
                f"pm.environment.set({js_str(_MALFORMED_BODY_VAR)}, pm.response.text());",
            ],
        ),
        Step(
            name="malformed-account-id-no-rights",
            method="GET",
            path="/iam/v1/accounts/not-an-account/memberships",
            auth="jwtPureNoBindings",
            test_script=[
                *assert_status(400),
                "pm.test("
                + js_str("исход НЕ является функцией прав: у вызывающего без единой выдачи "
                         "ответ тот же, потому что права не спрашиваются вовсе")
                + ", () => {",
                f"  const held = pm.environment.get({js_str(_MALFORMED_BODY_VAR)});",
                "  pm.expect(held, 'тело отказа держателю права не захвачено — сравнивать не с чем')"
                ".to.be.a('string').and.not.empty;",
                "  pm.expect(pm.response.text()).to.eql(held);",
                "});",
            ],
        ),
        Step(
            name="control-wellformed-absent-account-reaches-the-model",
            method="GET",
            path=f"/iam/v1/accounts/{ABSENT_ACCOUNT}/memberships",
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(403),
                *_labelled_grpc_code(
                    7,
                    "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: well-formed accountId рубеж формы ПРОХОДИТ — "
                    "иначе отрицание зеленело бы на крае, отвергающем ВСЯКИЙ accountId"),
            ],
        ),
    ],
))
