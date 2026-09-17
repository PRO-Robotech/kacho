# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Проект как КОНТЕЙНЕР сети: живая сеть vpc удерживает проект от удаления.

Связка двух доменов через край платформы — дом кейса ЗДЕСЬ, а не в наборе службы
доступа (`e2e-flow.md` §7а: набор службы утверждает её сущности; ресурс чужого
домена он завести не может — регистрация у службы идёт внутренним глаголом
`InternalIAMService.RegisterResource`, который наружу не маршрутизируется).

Это ПУНКТ 1 предиката снятия `PRO-Robotech/kacho#1231` (документ решения
`docs/architecture/project-deletion-and-live-resources.md`, §«Предикат снятия»):
механизм отказа посажен в службе (`PRO-Robotech/kaname#166`, приёмка
`non-empty-project-is-not-deleted.md`, §3.4 и §8), а НАБЛЮДЕНИЕ живого ресурса
чужого домена через край производит только этот кейс. Платформенная половина —
`PRO-Robotech/kacho#2684`.
"""

# Helpers инжектятся gen.py через namespace модуля:
#   Step, Case, assert_status, save_from_response, assert_operation_envelope,
#   poll_operation_until_done, assert_op_refusal_message, js_str

CASES = []

# Один и тот же принципал заводит проект, сеть в нём и удаляет обоих —
# администратор аккаунта. У модели прав его право на проект и на ресурсы в нём
# КАСКАДНОЕ (`project.super_admin: admin from account`, `vpc_network.super_admin:
# super_admin from project`), то есть тенантский сценарий «завёл контейнер, положил
# в него ресурс, снёс» идёт под одной личностью и без выдачи привязок.
_ACTOR = "jwtAccountAdminA"

# Одна сеть числится у службы ТРЕМЯ объектами: владелец регистрирует вместе с ней
# служебную группу безопасности и таблицу маршрутизации
# (`services/vpc/internal/apps/kacho/api/network/create.go`, три
# `ProjectHierarchyItem`). Перечень отказа называет ЗАРЕГИСТРИРОВАННЫЕ объекты, а не
# выполненные действия, — виды в порядке имени, точечными именами каталога
# (`kaname.catalog_resource.dotted`). Это свойство композиции vpc, и держит его
# ЭТОТ кейс: текст арендатора числа «три» не несёт намеренно (приёмка §7.4), а здесь
# оно утверждается дословно, потому что vpc — наш домен и его состав известен.
_HOLDERS = "vpc.network: 1, vpc.routeTable: 1, vpc.securityGroup: 1"

# ОКНО ДОСТАВКИ СНЯТИЯ РЕГИСТРАЦИИ — bounded retry на ПОЛОЖИТЕЛЬНОЙ половине.
#
# Снятие строки зеркала у службы приходит от владельца асинхронно: намерение
# ложится в исходящую очередь vpc тем же коммитом, что удаление сети, и доезжает
# дренажом (LISTEN/NOTIFY, обычно доли секунды). Контракт называет это границей
# прямо: «снятый ресурс может числиться ещё некоторое время». Наблюдать доставку
# через край НЕЧЕМ (кортеж и строка зеркала наружу не видны), поэтому единственный
# наблюдатель — сам исход повторного удаления проекта. Повтор ограничен бюджетом и
# ОТКАЗ НЕ МАСКИРУЕТ: снятие, которое не доехало никогда, даёт красное на последнем
# заходе с текстом того самого отказа. Это тот же класс, что `retry_until_authorized`
# на окне материализации прав, только у обратного глагола.
_UNREG_WINDOW_BUDGET = 20
_UNREG_WINDOW_INTERVAL_MS = 500
# Имя шага повторного удаления берётся В МОМЕНТ ИСПОЛНЕНИЯ (`pm.info.requestName`),
# а не литералом: конвейер генератора оборачивает первое обращение к своему
# свежему ресурсу и переименовывает шаг (`-rya<N>`), поэтому буквальный переход
# указывал бы на имя, которого в коллекции нет, — а newman на неизвестное имя
# ЗАВЕРШАЕТ прогон, и хвост кейса ушёл бы в «не выполнилось» молча.
_RETRY_DELETE_NAME_VAR = "_netHoldRetryDeleteStep"


def _reason_is_reference_in_use() -> str:
    return ("(j.error && (j.error.details || []).some("
            "x => x && x.reason === 'REFERENCE_IN_USE'))")


CASES.append(Case(
    id="NET-PRJ-DL-NEG-NETWORK-HOLDS",
    title="Живая сеть vpc удерживает проект: DELETE /iam/v1/projects/{id} → операция с "
          "FAILED_PRECONDITION «Project <id> is not empty (vpc.network: 1, …)» + "
          "REFERENCE_IN_USE; после снятия сети ТОТ ЖЕ запрос проходит",
    classes=["NEG", "STATE", "CONF"],
    priority="P1",
    steps=[
        # 1. Свежий проект — свой, не общая фикстура: удаление общего проекта
        # сломало бы соседние коллекции, а предмет кейса — именно его удаление.
        Step(
            name="create-holder-project",
            method="POST",
            path="/iam/v1/projects",
            body={
                "accountId": "{{accountAId}}",
                "name": "prj-nethold-{{runId}}",
                "description": "newman: project held by a live vpc network",
            },
            auth=_ACTOR,
            test_script=[
                *assert_status(200),
                *assert_operation_envelope(),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.projectId", "netHoldProjectId"),
            ],
        ),
        # Исход операции утверждает конвейер генератора (`_assert_published_id_outcome`):
        # предвыделенный id лежит в metadata и у операции с ошибкой, и без проверки
        # исхода дальше поехал бы фантомный проект.
        poll_operation_until_done(auth=_ACTOR),
        Step(
            name="get-holder-project",
            method="GET",
            path="/iam/v1/projects/{{netHoldProjectId}}",
            auth=_ACTOR,
            test_script=[
                *assert_status(200),
                "pm.test('фикстура записала id проекта-носителя', () => "
                "  pm.expect(pm.environment.get('netHoldProjectId'), 'netHoldProjectId')"
                "   .to.be.a('string').and.not.empty);",
                "pm.test('и это именно он', () => pm.expect(pm.response.json().id)"
                ".to.eql(pm.environment.get('netHoldProjectId')));",
            ],
        ),
        # 2. Ресурс ЧУЖОГО для службы домена — сеть vpc в этом проекте, через край.
        Step(
            name="create-network-in-holder-project",
            method="POST",
            path="/vpc/v1/networks",
            body={
                "projectId": "{{netHoldProjectId}}",
                "name": "net-hold-{{runId}}",
                "description": "newman: the network that holds its project",
            },
            auth=_ACTOR,
            test_script=[
                *assert_status(200),
                *assert_operation_envelope(),
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.networkId", "netHoldNetworkId"),
            ],
        ),
        poll_operation_until_done(auth=_ACTOR),
        # Первое чтение своей свежей сети — под ограниченным повтором на окне
        # материализации (обёртку ставит конвейер генератора по свойству шага).
        # ЭТО ЖЕ ЧТЕНИЕ — ДОКАЗАТЕЛЬСТВО, ЧТО РЕГИСТРАЦИЯ ДОЕХАЛА: право
        # администратора аккаунта на сеть резолвится каскадом через кортеж
        # `vpc_network#project`, который служба пишет тем же вызовом
        # `RegisterResource`, что и строку зеркала. Пока кортежа нет — 403 и повтор;
        # 200 означает, что строка зеркала уже закоммичена, и отказ ниже спрашивает
        # о доехавшем, а не о доставке.
        Step(
            name="get-network-confirms-registration",
            method="GET",
            path="/vpc/v1/networks/{{netHoldNetworkId}}",
            auth=_ACTOR,
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('сеть лежит в проекте-носителе', () => "
                "  pm.expect(j.projectId, pm.response.text())"
                "   .to.eql(pm.environment.get('netHoldProjectId')));",
            ],
        ),
        # 3. ОТРИЦАНИЕ — удаление проекта с живой сетью отвергается ПО СОСТОЯНИЮ.
        # Полоса одна и она АСИНХРОННАЯ: охрана живёт в операторе удаления, который
        # исполняет worker; синхронного 400 здесь не бывает — приходит конверт
        # операции, отказ приезжает в ней.
        Step(
            name="delete-project-with-live-network",
            method="DELETE",
            path="/iam/v1/projects/{{netHoldProjectId}}",
            auth=_ACTOR,
            test_script=[
                *assert_status(200),
                *assert_operation_envelope(),
                *save_from_response("j.id", "opId"),
            ],
        ),
        poll_operation_until_done(
            auth=_ACTOR,
            must_fail=True,
            must_fail_code=9,
            tail=[
                # Текст — ДОСЛОВНО и целиком (форма сети: «is not empty (<вид>: <число>, …)»):
                # потеря перечня, подмена вида, лишний вид с нулём и недосчёт
                # служебных объектов — каждое падает по отдельности.
                *assert_op_refusal_message(
                    "Project {{netHoldProjectId}} is not empty (" + _HOLDERS + ")",
                    probe="отказ перечисляет удерживающее по видам и числам: " + _HOLDERS),
                # Полоса отказа различима МАШИННО: клиент ключуется на признаке, а не
                # на прозе (api-conventions.md §by-lane code-split). Признак тот же,
                # что у всякого «на ресурс ещё ссылаются» — второго не заводится
                # (приёмка §2.5).
                "pm.test('полоса отказа различима машинно — ErrorInfo.reason REFERENCE_IN_USE', () => "
                "  pm.expect(" + _reason_is_reference_in_use() + ", JSON.stringify(j.error)).to.eql(true));",
            ],
        ),
        # Проект обязан ОСТАТЬСЯ: отказ, после которого контейнер всё равно исчез, —
        # не сработавший запрет, а потерянная строка. И сеть обязана остаться
        # адресуемой через него.
        Step(
            name="project-survived-refused-delete",
            method="GET",
            path="/iam/v1/projects/{{netHoldProjectId}}",
            auth=_ACTOR,
            test_script=[
                *assert_status(200),
                "pm.test('проект на месте после отвергнутого удаления', () => "
                "  pm.expect(pm.response.json().id).to.eql(pm.environment.get('netHoldProjectId')));",
            ],
        ),
        Step(
            name="network-survived-refused-delete",
            method="GET",
            path="/vpc/v1/networks/{{netHoldNetworkId}}",
            auth=_ACTOR,
            test_script=[
                *assert_status(200),
                "pm.test('сеть на месте и всё ещё в этом проекте', () => "
                "  pm.expect(pm.response.json().projectId).to.eql(pm.environment.get('netHoldProjectId')));",
            ],
        ),
        # 4. ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ (он же уборка) — снимаем сеть, и ТОТ ЖЕ запрос
        # проходит. Без этой половины отказ выше зеленел бы и на полностью сломанном
        # удалении проектов. Сеть снимается своим глаголом — служебные группа и
        # таблица уходят вместе с ней тем же коммитом vpc, и снятие всех трёх
        # регистраций ложится в очередь владельца.
        Step(
            name="delete-network",
            method="DELETE",
            path="/vpc/v1/networks/{{netHoldNetworkId}}",
            auth=_ACTOR,
            test_script=[
                *assert_status(200),
                *assert_operation_envelope(),
                *save_from_response("j.id", "opId"),
            ],
        ),
        poll_operation_until_done(auth=_ACTOR),
        Step(
            name="delete-project-after-network-gone",
            method="DELETE",
            path="/iam/v1/projects/{{netHoldProjectId}}",
            auth=_ACTOR,
            test_script=[
                *assert_status(200),
                *assert_operation_envelope(),
                *save_from_response("j.id", "opId"),
                f"pm.environment.set({js_str(_RETRY_DELETE_NAME_VAR)}, pm.info.requestName);",
            ],
        ),
        poll_operation_until_done(
            auth=_ACTOR,
            tail=[
                "// ОКНО ДОСТАВКИ СНЯТИЯ РЕГИСТРАЦИИ (см. шапку модуля): пока служба ещё",
                "// числит снятую сеть, повторяем ТОТ ЖЕ запрос удаления, ограниченно.",
                "// Отказ на последнем заходе остаётся красным — граница контракта",
                "// покрывает секунды, а не «никогда».",
                "if (" + _reason_is_reference_in_use() + ") {",
                "  const _uw = parseInt(pm.environment.get('_unregWait') || '0', 10);",
                f"  if (_uw < {_UNREG_WINDOW_BUDGET}) {{",
                "    pm.environment.set('_unregWait', String(_uw + 1));",
                f"    const _ud = Date.now(); while (Date.now() - _ud < {_UNREG_WINDOW_INTERVAL_MS}) {{ /* unregister-delivery wait */ }}",
                f"    pm.execution.setNextRequest(pm.environment.get({js_str(_RETRY_DELETE_NAME_VAR)}));",
                "    return;",
                "  }",
                "}",
                "pm.environment.unset('_unregWait');",
                f"pm.environment.unset({js_str(_RETRY_DELETE_NAME_VAR)});",
                "pm.test('после снятия сети ТОТ ЖЕ запрос удаления проекта проходит', () => "
                "  pm.expect(j.error && JSON.stringify(j.error), 'operation.error').to.eql(undefined));",
            ],
        ),
        # Проект снят — контейнер без содержимого удаляется, как и обещано.
        Step(
            name="project-gone",
            method="GET",
            path="/iam/v1/projects/{{netHoldProjectId}}",
            auth=_ACTOR,
            test_script=[*assert_status(404)],
        ),
    ],
))
