# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set iam-project-edge-format — ФОРМА ИДЕНТИФИКАТОРА, КОТОРУЮ СУДИТ КРАЙ ДО ПРОВЕРКИ ПРАВ.

Перенесён из набора службы доступа (PRO-Robotech/kaname, коллекция того же имени;
держатель переноса — #2912, сторона службы — PRO-Robotech/kaname#415). Там кейс
не гонял ни один шаг конвейера: на собственном фронте службы у пары нет
производителя. Здесь его гоняет шаг `гейт — newman зелёный (api-gateway)`
(`.github/workflows/e2e-newman.yml`).

ПРЕДМЕТ — ШАГ КОРОТКОГО ЗАМЫКАНИЯ КРАЯ. Край судит ПРИСТАВКУ идентификатора в пути
до проверки прав: строка, у которой ни первые три знака, ни сегмент до первого
дефиса не входят в каталог приставок платформы, получает `400` / `3` без обращения
к службе (приёмка службы `non-empty-project-is-not-deleted.md`, IAM-PNE-1-08, Н4 и
Н14). Собственный фронт службы такого шага не несёт и на том же входе отвечает
`403` / `7` от проверки прав — это замер службы 2026-09-16, и он же причина, по
которой утверждение живёт у края: переписанное под `403`, оно перестало бы быть
утверждением о форме.

ПАРА, А НЕ ОДИНОЧНОЕ ОТРИЦАНИЕ. При переносе к отказу добавлен законный близнец,
отличающийся РОВНО одним фактом — известна ли краю приставка: `prj123` (приставка
`prj` каталога, длина не та) край пропускает к проверке прав, и тот отвечает
`403` / `7`. Без близнеца «отказ по форме» неотличим от края, отвергающего любой
идентификатор в этом пути; с ним видно, что короткое замыкание судит именно
приставку. Замер стенда посадки `own` 2026-10-01 через публичный слушатель:
`qqq-not-a-project` → `400 {"code":3,"message":"invalid resource id 'qqq-not-a-project'"}`,
`prj123` → `403 {"code":7,"message":"permission denied: iam.projects.delete",…}`.

Техники: классы эквивалентности входа (приставка вне каталога / приставка каталога
неверной длины), угадывание ошибок (синхронный отказ без конверта операции).

Чего модуль НЕ утверждает: как отвечает на тот же вход собственный фронт службы и
как служба удаляет проект — это сущности службы, их дом — её репозиторий
(e2e-flow.md §7а воркспейса).
"""

# ЧЬЁ ПОВЕДЕНИЕ УТВЕРЖДАЕТ ЭТОТ МОДУЛЬ (e2e-flow.md §7а, решение владельца 2026-09-12).
# Единственный домен по REST-путям — `iam`, и маршрут здесь НОСИТЕЛЬ. Сверяет
# `tests/newman/scripts/case_home_test.py`.
ASSERTS_DOMAIN = "gateway"
ASSERTS_REASON = (
    "предмет — короткое замыкание КРАЯ по приставке идентификатора в пути: оно стоит в "
    "крае ДО проверки прав и отвечает 400/3 без обращения к службе; собственный фронт "
    "службы этого шага не несёт и на том же входе отвечает 403/7. Маршрут "
    "`/iam/v1/projects/{id}` — носитель: удаления проекта как ресурса службы модуль не "
    "утверждает ни одним шагом."
)

CASES = []

# ---------------------------------------------------------------------------
# IAM-PRJ-DL-NEG-MALFORMED-PREFIX — IAM-PNE-1-08: неправильная форма идентификатора
# отвергается СИНХРОННО, операция не создаётся.
#
# Идентификатор известной приставки не той длины (`prj123`) и идентификатор чужого
# семейства правильной формы край ПРОПУСКАЕТ к проверке прав, — поэтому такие входы
# в отказ не входят: «Тогда» было бы строже производителя (приёмка §0.2в, Н4, Н14).
# Первый из них стоит здесь законным близнецом.
#
# СТРОГОСТЬ ПАРЫ ОСЛАБЛЯТЬ ЗАПРЕЩЕНО (testing.md: «malformed-id … не ослаблять»):
# `oneOf` был бы допуском на исход, которого край на этом входе не производит.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-PRJ-DL-NEG-MALFORMED-PREFIX",
    title="Delete /projects/{string whose prefix is unknown to the platform} → sync 400 "
          "INVALID_ARGUMENT (3) from the edge; a known prefix of the wrong length passes on to "
          "the permission check (403)",
    classes=["NEG", "VAL"],
    priority="P1",
    steps=[
        Step(
            name="delete-malformed-prefix",
            method="DELETE",
            path="/iam/v1/projects/qqq-not-a-project",
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
                # Конверта операции нет: отказ синхронный, проверка формы стоит
                # раньше создания операции.
                "pm.test('no operation envelope on a sync refusal', () => {"
                " const j = pm.response.json();"
                " pm.expect(j.id, JSON.stringify(j)).to.be.undefined; });",
                "pm.test('the refusal names the identifier the caller sent', () => {"
                " const j = pm.response.json();"
                " pm.expect(String(j.message || ''), JSON.stringify(j))"
                ".to.eql(\"invalid resource id 'qqq-not-a-project'\"); });",
            ],
        ),
        # ЗАКОННЫЙ БЛИЗНЕЦ: тот же глагол, тот же предъявитель, приставка `prj` из
        # каталога. Отличие в одном факте — приставка известна краю, поэтому шаг
        # короткого замыкания пропускает запрос дальше, к проверке прав.
        Step(
            name="delete-known-prefix-wrong-length",
            method="DELETE",
            path="/iam/v1/projects/prj123",
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(403),
                *assert_grpc_code(7, "PERMISSION_DENIED"),
                "pm.test('a known prefix is not answered by the format short-circuit', () => {"
                " const j = pm.response.json();"
                " pm.expect(String(j.message || ''), JSON.stringify(j))"
                ".to.not.include('invalid resource id'); });",
            ],
        ),
    ],
))
