# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set authz-deny для kacho-vpc.

Проверяет default-deny matrix для 6 классов субъектов на каждом публичном CRUD
каждого VPC-ресурса — против ЖИВОГО object-scoped authz (api-gateway → FGA), не
dev-mode anonymous→full-access.

Pre-conditions: `tests/authz-fixtures/setup.sh` создает фикстуры (accounts,
projects, users, bindings, seed networks) и патчит env-файл:
  - jwt*               : Bearer-токены (no-bindings / proj-admin-a1 / account-admin-a/b / invitee)
  - accountAId / Bid
  - projectA1Id / A2Id / B1Id
  - seedNetworkA1Id / seedNetworkB1Id

Реальные гранты фикстуры (источник истины — setup.sh):
  - PA1 → editor   @ project:A1
  - AAA → admin    @ account:A   (каскад на project:A1)
  - AAB → admin    @ account:B   (каскад на project:B1)
  - INV → admin    @ account:B (каскад на project:B1) + editor @ project:A1
          (KAC-125 invite-flow: AAA приглашает INV в account-A editor'ом на
          project-A1) → INV имеет доступ к ОБОИМ project-A1 и project-B1.
  - NOB → грантов нет. Субъект-код "NOB" биндится к `jwtPureNoBindings` — ВЫДЕЛЕННЫЙ
    never-granted principal (kacho-iam#276 fix), который НИ ОДНА suite не грантит
    (setup.sh: ни ensure_binding, ни 4b-cleanup его не трогают). Раньше здесь стоял
    `jwtNoBindings`, который iam access-binding suites РЕАЛЬНО грантят `view@account-A/-B`
    на время своего прогона → под параллельным fan-out'ом account→project containment
    транзитно авторизовывал NOB → AUTHZ-*-LS-{OWN,CROSS}-NOB ложно краснели, и это
    вычиталось из вердикта списком освобождений прогонщика. Список СНЯТ целиком
    (`services/iam/tests/newman/scripts/assert-suites-green.sh`: вычитания больше нет),
    а с pure-субъектом эти LIST-DENY leak-guard'ы строгие и зелёные без всяких
    освобождений. verifies kacho-iam#276)

Контракт ответов (api-gateway authz middleware, см. kacho-api-gateway):
  - Анонимный запрос (нет токена) → 401 UNAUTHENTICATED (grpc 16) ВЕЗДЕ.
  - Object-scoped READ (`/Get`) на запрещенный/несуществующий ресурс → существование
    скрывается: 404 NOT_FOUND (grpc 5). `/Get`-deny одинаков для «нет такого» и
    «есть-но-не-твой» (anti-enumeration).
  - Object-scoped MUTATION (`/Update`, `/Delete`) на запрещенный/несуществующий
    ресурс → 403 PERMISSION_DENIED (grpc 7). Existence не скрывается для мутаций
    (deny одинаков для существующего и нет → утечки тоже нет).
  - `/List` — call-gate `viewer` на `project:<projectId>` (все семь top-level List
    этой суиты, паритет закреплён в permission_map сервиса) + сужение результата до
    видимого subject'у набора. ЕСТЬ доступ → 200 + отфильтрованная страница (403 здесь
    невозможен: `viewer` выводится из tier-гранта, см. `list_allow_asserts`). НЕТ
    доступа → 403 ЛИБО 200 + ПУСТОЙ список. 200 + чужие ресурсы = LEAK (валит тест).
    Отношение `v_list`, развязанное от tier, гейтит под-списки НА РЕСУРСЕ
    (`ListSubnets`/`ListSecurityGroups`/`ListRouteTables`/`ListOperations`) — их эта
    суита не вызывает.
  - Create-child / админ-only RPC без нужного гранта → 403.

ALLOW-полоса утверждает ПАРУ, а не отрицание (issue #505). У каждой ALLOW-позиции
ОДИН законный исход, установленный по матрице «субъект × право» и по контракту
ресурса; полос три, у каждой свой производитель в дереве (разбор — `allow_steps`):

  - `created`        → `200` + конверт Operation, завершившаяся БЕЗ ошибки;
  - `carve`          → то же, но подсеть режется в сети, созданной ЭТИМ ЖЕ кейсом;
  - `peer_missing`   → синхронный `404` + `code 5` + `"<Вид> <id> not found"`, где вид
                       называет ресурс, чья ссылка не резолвится (сеть у подсети,
                       таблицы и группы; подсеть у адреса);
  - `anchor_missing` → `200` + Operation, завершившаяся `code 5` +
                       `"Subnet <id> not found"`.

Полоса выбирается ПО ТОМУ, ГДЕ ВЛАДЕЛЕЦ ЧИТАЕТ ССЫЛКУ, — до создания операции или
внутри неё. Разница не выводится из вида ресурса и не угадывается: у шлюза и
интерфейса подсеть читается в `doCreate`, у адреса — в `Execute`. Ошибка на этом
месте стоила прогона и была названа ТЕКСТОМ отказа (см. комментарий у адреса).

Это НЕ толерантность: полосы принадлежат разным кейсам, а не одному, и внутри
кейса выбор исхода не оставлен открытым. Прежнее «код не 403 и не 401» принимало
успех, отказ по пересечению адресов и отказ по исчерпанию пула одинаково — то есть
не отличало исправную систему от той поломки, ради которой суита написана.

Helpers Case/Step инжектятся через gen.py namespace.
"""

CASES = []

SUBJECTS = [
    # code, label, auth (None→anonymous, иначе env-var-name)
    ("ANON", "anon",       "anonymous"),
    # kacho-iam#276: NOB → dedicated never-granted `jwtPureNoBindings` (см. docstring).
    ("NOB",  "no-bind",    "jwtPureNoBindings"),
    ("PA1",  "proj-adm",   "jwtProjectAdminA1"),
    ("AAA",  "acct-adm-a", "jwtAccountAdminA"),
    ("AAB",  "acct-adm-b", "jwtAccountAdminB"),
    ("INV",  "invitee",    "jwtInvitee"),
]

# scope-class → subject-code → expected ('ALLOW'/'DENY'). Отражает РЕАЛЬНЫЕ гранты
# фикстуры (см. docstring), а не «кому хотелось бы».
EXPECT = {
    # project-A1: editor у PA1; account-A admin (AAA) каскадит на A1; INV — editor
    # @ project-A1 через KAC-125 invite-flow.
    "project-A1":          {"ANON":"DENY","NOB":"DENY","PA1":"ALLOW","AAA":"ALLOW","AAB":"DENY", "INV":"ALLOW"},
    # project-B1: account-B admin (AAB, INV) каскадит на B1.
    "project-B1":          {"ANON":"DENY","NOB":"DENY","PA1":"DENY", "AAA":"DENY", "AAB":"ALLOW","INV":"ALLOW"},
    # AddressPool — admin-only (cluster system_admin): ни один из 6 субъектов его не
    # несет → DENY у всех аутентифицированных; ANON → 401.
    "addresspool-admin-only": {"ANON":"DENY","NOB":"DENY","PA1":"DENY","AAA":"DENY","AAB":"DENY","INV":"DENY"},
}


def deny_asserts(case_id):
    """Аутентифицированный субъект без доступа → 403 PERMISSION_DENIED (grpc 7)."""
    return [
        f"pm.test('[{case_id}] DENY: status 403', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(403));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] DENY: grpc code 7 (PERMISSION_DENIED)', () => pm.expect(j && j.code, JSON.stringify(j)).to.equal(7));",
        f"pm.test('[{case_id}] DENY: message contains permission denied', () => pm.expect((j && j.message || '').toLowerCase()).to.contain('permission denied'));",
    ]


def unauth_asserts(case_id):
    """Anonymous (нет токена) → 401 UNAUTHENTICATED (grpc 16)."""
    return [
        f"pm.test('[{case_id}] UNAUTH: status 401', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(401));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] UNAUTH: grpc code 16 (UNAUTHENTICATED)', () => pm.expect(j && j.code, JSON.stringify(j)).to.equal(16));",
    ]


def notfound_asserts(case_id):
    """Object-scoped `/Get`-deny → existence-hiding: 404 NOT_FOUND (grpc 5).
    Одинаков для «нет такого» и «есть-но-не-твой» (anti-enumeration)."""
    return [
        f"pm.test('[{case_id}] NF: status 404 (read-deny existence-hiding)', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(404));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] NF: grpc code 5 (NOT_FOUND)', () => pm.expect(j && j.code, JSON.stringify(j)).to.equal(5));",
    ]


# Заведомо НЕРЕЗОЛВЯЩАЯСЯ подсеть: форма верна (prefix `sub` ∈ каталог
# `ids.KnownPrefixes()`, поэтому `corevalidate.ResourceID` пропускает), строки нет.
# Ею адресуются якорь шлюза, якорь интерфейса и область внутреннего адреса — три
# ALLOW-полосы, чей единственный законный исход установлен ниже.
NONEXISTENT_SUBNET_ID = "subnonexistent000001"

# Заведомо нерезолвящаяся сеть — тем же построением. Стоит в теле DENY-полосы
# подсети: там предмет кейса — отказ КРАЯ, до сервиса запрос не доходит вовсе,
# и ссылка на ОБЩУЮ посевную сеть читалась бы как нарезка в ней, каковой не является.
NONEXISTENT_NETWORK_ID = "netnonexistent000001"

# Адрес подсети, которую ALLOW-полоса режет в СВОЕЙ сети, созданной этим же кейсом.
# Литерал безопасен BY CONSTRUCTION, и это свойство продукта, а не удача: ограничение
# непересечения у подсетей — `EXCLUDE USING gist (network_id WITH =, …)`, а sync-проверка
# `checkSubnetCIDROverlap` перечисляет подсети фильтром `{ProjectID, NetworkID}`. В сети,
# созданной шагом выше, этот набор ПУСТ — сравнивать не с чем ни внутри прогона, ни между
# прогонами. Ниже плана сети (`FIXTURE_NETWORK_SUPERNET` = 10.0.0.0/8, дописывается
# генератором) и вне зарезервированного платформой (169.254.0.0/16, fe80::/10).
CARVED_SUBNET_CIDR = "10.0.0.0/16"


def allow_created_asserts(case_id):
    """ALLOW, полоса «прошло И СДЕЛАЛО»: 200 + Operation.

    ИСХОД ОДИН, и он установлен по матрице «субъект × право» и по контракту ресурса,
    а не принят на веру. Субъект этой полосы держит `editor` на целевом проекте (PA1 и
    INV — прямым грантом, AAA и AAB — через `admin @ account`, см. docstring файла),
    тело кейса полеверно и все его ссылки резолвятся, поэтому край пропускает, сервис
    принимает и отвечает конвертом Operation. Отказать здесь нечему.

    ПОЧЕМУ ПРЕЖНЕЕ УТВЕРЖДЕНИЕ БЫЛО НЕ УТВЕРЖДЕНИЕМ. Здесь стояло «код НЕ 403 и НЕ 401»
    с оговоркой, что исход валидации вне предмета кейса. Отрицание принимает и успех, и
    отказ по пересечению адресов, и отказ по исчерпанию пула — то есть не отличает
    исправную систему от той поломки, ради которой кейс написан. Хуже: `expectsRefusal`
    (гейты дерева `newmansubnetsupernet_test.go` / `newmancarveplananchor_test.go`)
    классифицирует шаг по его утверждениям о статусе, и шаг, у которого все они
    отрицательные, читался как ПРОБА ОТКАЗА — поэтому оба гейта эти нарезки не
    рассматривали вовсе. Одно слабое утверждение ослепило две независимые проверки.

    ПАРА, А НЕ ОДИН СТАТУС. Успешная мутация `google.rpc.Status` не несёт — его место в
    конверте занимает `oneof result`. Поэтому парой здесь служат СТАТУС и ФОРМА ответа
    (конверт Operation), а исход самой операции утверждает парный `poll_operation_until_done`
    следующим шагом: он требует `done` и ОТСУТСТВИЕ `error`. Без него 200 остаётся верным и
    для операции, завершившейся отказом, — `RunSync` помечает её ошибкой и возвращает
    успех транспорта (`pkg/operations/worker.go`).
    """
    return [
        f"pm.test('[{case_id}] ALLOW: HTTP 200 (мутация принята)', () => "
        f"pm.expect(pm.response.code, pm.response.text()).to.equal(200));",
        *assert_operation_envelope(),
        *save_from_response("j.id", "opId"),
    ]


def allow_peer_missing_asserts(case_id, ref_expr, resource="Network"):
    """ALLOW, полоса «прошло И УПЁРЛОСЬ В НАЗВАННУЮ ССЫЛКУ» — СИНХРОННО.

    Пара: HTTP **404** и `code` **5** (`NOT_FOUND`) из `google.rpc.Status`, плюс
    контрактный тон отказа. Установлено по коду владельца, а не по догадке:
    `CreateSubnet`/`CreateRouteTable`/`CreateSecurityGroup` читают родительскую сеть
    в `Execute` — ДО создания Operation — и на «сеть чужого проекта» отвечают тем же
    `NotFound "Network <id> not found"`, что и на несуществующую (без оракула
    существования). Значит отказ приходит СИНХРОННО, Operation не появляется, и
    опрашивать нечего.

    ЭТО НЕ ТОЛЕРАНТНОСТЬ. Полоса одна, производитель один, исход один. Кейс сохраняет
    свой предмет — решение о правах: 404 по этому адресу может получить ТОЛЬКО запрос,
    который край пропустил (мутации край отказывает `403`, существование он скрывает
    лишь на чтении, см. docstring файла). Отличие ALLOW от DENY здесь строгое.

    `ref_expr` — ВЫРАЖЕНИЕ, дающее тот же идентификатор, что ушёл в теле запроса
    (`pm.environment.get('seedNetworkA1Id')` либо литерал). Подстановка `{{…}}` работает
    в теле и адресе, но НЕ в скрипте, поэтому текст собирается из того же источника, что
    и запрос, — двух мест об одном предмете не заводится.
    """
    return [
        f"pm.test('[{case_id}] ALLOW→miss: HTTP 404', () => "
        f"pm.expect(pm.response.code, pm.response.text()).to.equal(404));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] ALLOW→miss: grpc code 5 (NOT_FOUND)', () => "
        f"pm.expect(j && j.code, JSON.stringify(j)).to.equal(5));",
        f"pm.test('[{case_id}] ALLOW→miss: контрактный тон отказа', () => "
        f"pm.expect(j && j.message, JSON.stringify(j)).to.equal("
        f"'{resource} ' + {ref_expr} + ' not found'));",
    ]


def list_allow_asserts(case_id, list_key):
    """List субъектом, у которого ЕСТЬ доступ к project'у → 200 + отфильтрованный список.

    ИСХОД ОДИН, и это установлено по матрице «субъект × право на список», а не принято
    на веру. Все семь top-level `/List` этой суиты (networks / subnets / addresses /
    routeTables / securityGroups / gateways / networkInterfaces) гейтятся отношением
    `viewer` на `project:<projectId>` — и на краю (permission_catalog), и в сервисе
    (`internal/check/permission_map.go`, где это записано как паритет всех
    семи). В модели `project.viewer` выводится из `editor`, `editor` из `admin`, `admin`
    из `super_admin`, а `project.super_admin` — из `admin from account`. Значит каждый
    ALLOW-субъект матрицы отношение `viewer` держит: PA1 и INV — прямым
    `editor @ project-A1`, AAA — через `admin @ account-A`, AAB и INV на project-B1 —
    через `admin @ account-B`. Отказать здесь нечему.

    ПОЧЕМУ ПРЕЖНЯЯ ТОЛЕРАНТНОСТЬ БЫЛА ЛОЖНОЙ. Утверждение принимало `200 ИЛИ 403`, а
    обоснование ссылалось на `v_list`, «развязанный от tier». Такая полоса в vpc есть,
    но она НЕ здесь: `v_list` гейтит под-списки НА РЕСУРСЕ (`NetworkService/ListSubnets`,
    `.../ListSecurityGroups`, `.../ListRouteTables`, `.../ListOperations`) — ни один из
    них эта суита не вызывает. Так утверждение принимало ровно тот отказ, ради ловли
    которого строка существует, и упасть на регрессии прав не могло.

    Тот же вывод подтверждается изнутри суиты, без обращения к модели: строки Create для
    ЭТОГО ЖЕ субъекта и project'а уже требуют «не 403», а Create гейтится `editor` —
    отношением СТРОГО СИЛЬНЕЕ, чем нужный списку `viewer`. Материализация, которой
    хватает на Create, покрывает и List; поэтому «а вдруг грант ещё не доехал» тоже не
    защищает 403 — он ронял бы сначала Create."""
    return [
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] LIST grant: 200 (scope-filtered page)', () => "
        f"pm.expect(pm.response.code, 'expected 200, body: ' + pm.response.text()).to.equal(200));",
        f"pm.test('[{case_id}] LIST grant: ответ несёт список', () => "
        f"pm.expect((j && j['{list_key}']) || [], JSON.stringify(j)).to.be.an('array'));",
    ]


def list_deny_asserts(case_id, list_key):
    """List БЕЗ доступа к project'у → 403 (call-gate) ИЛИ 200 + ПУСТОЙ список.

    Вторая форма помощника, парная к `list_allow_asserts`. Здесь два исхода законны по
    построению — какой из двух рубежей ответит первым, зависит от того, гейтится ли
    конкретный RPC на краю или в сервисе, — но КАЖДАЯ ветка несёт утверждение: отказ
    либо пустая страница. 200 + непустой список чужого project'а = LEAK (валит тест).
    Это не смешение успеха и отказа: обе ветки утверждают «чужого не видно»."""
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


# ---------------------------------------------------------------------------
# УБОРКА СОЗДАННОГО — только на полосах, чей УСТАНОВЛЕННЫЙ исход «ресурс появился»
# ---------------------------------------------------------------------------
#
# ПРЕДМЕТ (найден параллельной линией разбора красноты квот, коммит a6f6cd15).
# Кейсы полосы ALLOW шлют настоящий Create, и часть из них создаёт настоящий ресурс
# в ПОСЕЯННОЙ сети, которая живёт дольше прогона. Подсеть, таблица маршрутизации и
# группа безопасности считаются ВЛОЖЕННЫМ потолком «сколько детей в одной сети»
# (умолчание 16), поэтому незакрытый ребёнок оставался там навсегда, а отказ по
# пределу доставался не тому кейсу, который перебрал, а тому, который встал следом.
# Тем же порядком за девять суток был исчерпан общий внешний пул `100.64.0.0/24`.
#
# ЧТО ИЗМЕНИЛОСЬ ПОСЛЕ #505 И ПОЧЕМУ УСЛОВИЕ ДРУГОЕ. Прежнее условие уборки —
# `mode="gate" and decision=="ALLOW" and method=="POST"` — было лучшим доступным
# ПРИБЛИЖЕНИЕМ, пока все ALLOW-кейсы были неразличимы: нельзя было сказать, появится
# ресурс или нет, поэтому уборка ставилась на все и терпела любой исход. Теперь у
# каждой полосы исход ОБЪЯВЛЕН, и приближение заменяется фактом:
#
#   `created` / `carve` — ресурс появляется, и это УТВЕРЖДАЕТСЯ → уборка нужна;
#   `peer_missing`      — синхронный 404, Operation не создаётся вовсе → снимать нечего;
#   `anchor_missing`    — Operation завершается отказом, строки нет → снимать нечего.
#
# Тащить уборку на две последние полосы значило бы оставить шаг, у которого предмета
# нет ни при каком входе: 45 лишних запросов за прогон и мнимая уборка, которую
# следующий читатель примет за настоящую.
#
# ПОЧЕМУ ЗАХВАТ ИДЁТ ЧЕРЕЗ ОПРОС, А НЕ ИЗ ТЕЛА ОТВЕТА. Параллельная линия захватывала
# предварительно выделенный id из синхронного ответа и оговаривала, что `capture_id_to`
# неприменим: он УТВЕРЖДАЕТ отсутствие ошибки операции и покраснел бы на законном тогда
# исходе «ресурс не появился». После #505 это рассуждение переворачивается: на этих двух
# полосах отсутствие ошибки утверждается и так, поэтому `capture_id_to` — не помеха, а
# ровно то, что нужно: фантомный id становится невозможен by construction.
#
# ПОЧЕМУ УБОРКА СТРОГАЯ, А НЕ BEST-EFFORT. Там же уборка не роняла кейс — и это было
# верно, пока «ресурс не появился» оставалось законным исходом. Теперь не остаётся:
# удаляет тот же субъект, который создал; сеть-якорь освобождается ОТ СВОЕЙ подсети
# первой; собственные системные потомки сети (default-SG, default-RT) из проверки
# пустоты владельцем исключены и снимаются его же транзакцией. Утверждение и опрос
# шаг получает от генератора (`_DELETE_ACCEPTED` + `_assert_delete_operation_outcome`) —
# то есть тем же механизмом, что и всё дерево, а не своей копией.
_RECLAIM_VAR = "azReclaimId"


def _reclaim(resource_path, id_var, auth, name="reclaim"):
    """Снять созданное и дождаться операции снятия.

    Актор передаётся явно: удаляет тот же субъект, что создавал, и
    `OperationService.Get` энфорсит владение операцией.

    Шаг захватывает СВОЙ идентификатор операции: без этого парный опрос уехал бы на
    `opId` операции СОЗДАНИЯ — давно завершённой — и подтвердил бы чужой успех
    (`_DELETE_ACCEPTED` называет этот класс прямо). Утверждение о статусе и об исходе
    операции дописывает генератор, поэтому здесь их нет: своя копия разошлась бы с
    деревом молча.
    """
    return [
        Step(name=name, method="DELETE",
             path=f"{resource_path}/{{{{{id_var}}}}}", auth=auth,
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


def allow_steps(cid, method, path, body, auth, lane, anchor_project_var=None,
                anchor_name=None, anchor_var=None, peer_ref_expr=None,
                peer_ref_resource="Network"):
    """Шаги ALLOW-полосы. ТРИ полосы, у каждой ОДИН исход и СВОЙ производитель.

    Разные полосы — не «разные статусы одного исхода»: у каждой свой код-производитель
    в дереве, названный в её помощнике. Толерантности между ними нет и быть не может.

      created       — тело полеверно и все ссылки резолвятся → 200 + Operation без ошибки;
      carve         — то же, но подсеть режется в СЕТИ, СОЗДАННОЙ ЭТИМ ЖЕ КЕЙСОМ;
      peer_missing  — тело намеренно ссылается на сеть ЧУЖОГО проекта → sync 404/5;
      anchor_missing— тело намеренно ссылается на нерезолвящуюся подсеть → 200 + Operation,
                      завершившаяся `NOT_FOUND "Subnet <id> not found"`.

    Первые две ПОРОЖДАЮТ ресурс — и потому несут снятие (см. §«Уборка созданного»
    выше). Две последние не порождают ничего ни при каком входе, поэтому снятия у них
    нет: шаг, у которого предмета не бывает, — находка, а не запас.

    ПОЧЕМУ ДВЕ ПОСЛЕДНИЕ ПОЛОСЫ НЕ СОЗДАЮТ НИЧЕГО — И ЭТО ВЫБОР, А НЕ УСТУПКА. Их предмет
    (решение о правах) наблюдаем в самом ответе, а сама постановка не требует ни якоря
    размещения, ни аренды из общего пула. Прежняя редакция брала внешний адрес из
    умолчательного `AddressPool` зоны A: пул общий и на 256 адресов, суита его не сеет и
    ничего не убирает — то есть исход ALLOW-полосы зависел от того, кто прогонялся раньше,
    и утекало пять адресов за прогон. Утверждать по такому исходу нечего, а исчерпание
    ровно то, что старое отрицание «не 403» и проглатывало.
    """
    if lane == "peer_missing":
        return [Step(name=method.lower(), method=method, path=path, body=body, auth=auth,
                     test_script=allow_peer_missing_asserts(cid, peer_ref_expr,
                                                            resource=peer_ref_resource))]
    if lane == "anchor_missing":
        return [
            Step(name=method.lower(), method=method, path=path, body=body, auth=auth,
                 test_script=allow_created_asserts(cid)),
            poll_operation_until_done(
                must_fail=True, must_fail_code=5,
                must_fail_message=f"Subnet {NONEXISTENT_SUBNET_ID} not found"),
        ]
    if lane == "carve":
        # СВОЯ СЕТЬ НА КЕЙС — тем же субъектом, что режет в ней подсеть.
        #
        # Здесь стояла нарезка фиксированного `10.99.0.0/16` в ОБЩЕЙ посевной сети
        # `{{seedNetworkA1Id}}`. Ограничение непересечения действует В ПРЕДЕЛАХ СЕТИ, и
        # сеть эта переживает прогон, поэтому сталкивались и три ALLOW-субъекта между
        # собой внутри одного прогона, и каждый следующий прогон со всеми предыдущими.
        # Столкновение поглощалось отрицанием «не 403» и потому было ненаблюдаемо.
        #
        # НЕПЕРЕСЕЧЕНИЕ ТЕПЕРЬ НЕВОЗМОЖНО BY CONSTRUCTION, а не маловероятно: предмет
        # проверки — множество подсетей ЭТОЙ сети — пуст в момент нарезки. Жребий по
        # `runId` (разведение позиции хешем, как у набора nlb) дал бы вероятность, а
        # разбиение по свежей сети даёт невозможность; между прогонами это и есть
        # разница между «почти никогда» и «никогда».
        #
        # Якорь несёт СВОЁ утверждение и опрос: шаг, создающий предмет кейса без
        # утверждения, при отказе оставляет переменную пустой, кейс идёт дальше по
        # несозданному ресурсу и падает через два шага, обвиняя невиновного.
        # `capture_id_to` кладёт идентификатор ТОЛЬКО после проверки `op.error`.
        #
        # План сети (`ipv4CidrBlocks`) здесь не выписан: его дописывает генератор
        # (`_declare_supernet_where_a_subnet_is_carved`) каждой сети кейса, который
        # режет подсеть с адресом, — один механизм на всё дерево вместо копии.
        return [
            Step(name="anchor-net", method="POST", path="/vpc/v1/networks",
                 body={"projectId": "{{" + anchor_project_var + "}}", "name": anchor_name},
                 auth=auth,
                 test_script=[
                     f"pm.test('[{cid}] якорь: HTTP 200', () => "
                     f"pm.expect(pm.response.code, pm.response.text()).to.equal(200));",
                     *assert_operation_envelope(),
                     *save_from_response("j.id", "opId"),
                 ]),
            poll_operation_until_done(capture_id_to=anchor_var),
            Step(name=method.lower(), method=method, path=path, body=body, auth=auth,
                 test_script=allow_created_asserts(cid)),
            poll_operation_until_done(capture_id_to=_RECLAIM_VAR),
            # ПОРЯДОК СНЯТИЯ — ОБРАТНЫЙ ПОРЯДКУ ВЫДЕЛЕНИЯ (`data-integrity.md`
            # §«Порядок компенсации»): сеть с тенантской подсетью владелец удалять
            # отказывается («Network <id> is not empty»), поэтому сперва подсеть.
            # Собственные системные потомки сети (default-SG, default-RT) из проверки
            # пустоты владельцем ИСКЛЮЧЕНЫ и снимаются его же транзакцией — их
            # отдельно снимать не надо и нельзя.
            *_reclaim(path, _RECLAIM_VAR, auth),
            *_reclaim("/vpc/v1/networks", anchor_var, auth, name="reclaim-anchor"),
        ]
    return [
        Step(name=method.lower(), method=method, path=path, body=body, auth=auth,
             test_script=allow_created_asserts(cid)),
        poll_operation_until_done(capture_id_to=_RECLAIM_VAR),
        *_reclaim(path, _RECLAIM_VAR, auth),
    ]


def emit(case_id_prefix, title, scope, method, path, body, subject, mode="gate", list_key=None,
         allow_lane="created", allow_body=None, anchor_project_var=None, anchor_name=None,
         anchor_var=None, peer_ref_expr=None, peer_ref_resource="Network"):
    """mode:
        gate — стандартный ALLOW/DENY по EXPECT[scope][code] (Create / admin-only).
        list — scope-filtered List: ANON→401; иначе has-access→200, no-access→403|200+empty.
        nf   — object-scoped `/Get` на garbage id → 404 (existence-hiding); ANON→401.
        deny — object-scoped `/Update|/Delete` на garbage id → 403; ANON→401.

    `allow_lane` / `allow_body` читаются ТОЛЬКО при `mode="gate"` и решении ALLOW —
    у DENY-полосы запрос до сервиса не доходит, и тело её исхода не меняет.
    """
    code, label, auth = subject
    cid = f"AUTHZ-{case_id_prefix}-{code}"
    if mode == "list":
        if code == "ANON":
            decision, asserts = "UNAUTH", unauth_asserts(cid)
        else:
            access = EXPECT[scope][code]
            if access == "ALLOW":
                decision, asserts = "LIST-ALLOW", list_allow_asserts(cid, list_key)
            else:
                decision, asserts = "LIST-DENY", list_deny_asserts(cid, list_key)
    elif mode == "nf":
        decision = "UNAUTH" if code == "ANON" else "NF"
        asserts = unauth_asserts(cid) if code == "ANON" else notfound_asserts(cid)
    elif mode == "deny":
        decision = "UNAUTH" if code == "ANON" else "DENY"
        asserts = unauth_asserts(cid) if code == "ANON" else deny_asserts(cid)
    else:  # gate
        decision = EXPECT[scope][code]
        if decision == "DENY":
            asserts = unauth_asserts(cid) if code == "ANON" else deny_asserts(cid)
        else:
            # ALLOW-полоса собирается своими шагами: у неё есть исход, который надо
            # утвердить парой, и у части полос — собственный якорь.
            steps = allow_steps(cid, method, path, allow_body if allow_body is not None else body,
                                auth, allow_lane, anchor_project_var=anchor_project_var,
                                anchor_name=anchor_name, anchor_var=anchor_var,
                                peer_ref_expr=peer_ref_expr,
                                peer_ref_resource=peer_ref_resource)
            CASES.append(Case(
                id=cid,
                title=f"[ALLOW] {title} as {label} ({scope})",
                classes=["AUTHZ", "POS"],
                priority="P1",
                steps=steps,
            ))
            return

    is_pos = decision in ("ALLOW", "LIST-ALLOW", "NF", "LIST-DENY")
    step = Step(name=method.lower(), method=method, path=path, body=body, auth=auth, test_script=asserts)
    # LIST-DENY leak-guard ("no-access → 403 or 200+EMPTY"): a SHARED fixture subject can carry a
    # residual/concurrent account-scoped viewer from another suite (iam access-binding grants NOB a
    # ROLE_VIEW@account), which via account→project containment transiently makes THIS project's child
    # resources v_list-visible → the deny-list returns rows for a beat until that suite's revoke
    # materializes (read-your-writes ON REVOKE, eventually-consistent). Wrap in retry_until_absent:
    # retries SELF while the list is still non-empty, FAIL-OPEN at the budget so a GENUINE over-show
    # hole (rows never leave the deny-list) still FAILS the leak assertion — a real leak is NOT masked.
    # (Root fix is per-suite subject-isolation: a dedicated no-binding subject no other suite grants.)
    if decision == "LIST-DENY" and list_key:
        step = retry_until_absent(step, f"((pm.response.json()['{list_key}'])||[]).length > 0")
    CASES.append(Case(
        id=cid,
        title=f"[{decision}] {title} as {label} ({scope})",
        classes=["AUTHZ", "POS" if is_pos else "NEG"],
        priority="P1",
        steps=[step],
    ))


# ---------------------------------------------------------------------------
# RESOURCES — определение CRUD-эндпоинтов VPC
# ---------------------------------------------------------------------------

# Для Get/Update/Delete используем well-formed-но-несуществующий id: prefix `enp`
# распознается как валидный (api-gateway не отдает 400 InvalidArgument), длина 20 →
# id проходит формат-валидацию gateway'я и доходит до FGA Check → deny.
#   `/Get`            → existence-hiding 404 (NF).
#   `/Update|/Delete` → 403 (мутация-deny, existence НЕ скрывается).
GARBAGE_ID = "enpnonexistent000001"


def define_resource_cases(resource_name, plural, create_body_extra=None, supports_update=True,
                          cross_body_extra=None, own_lane="created", cross_lane="created",
                          own_peer_ref_expr=None, cross_peer_ref_expr=None,
                          peer_ref_resource="Network"):
    """Генерирует authz-проверки для одного project-scoped VPC ресурса.

    `own_lane`/`cross_lane` объявляют ЕДИНСТВЕННЫЙ законный исход ALLOW-полосы для
    каждой позиции — по контракту ресурса, а не по вкусу; разбор каждой полосы —
    в `allow_steps`. `cross_body_extra` задаётся отдельно там, где тело
    cross-позиции обязано отличаться от own (у подсети own-полоса режет в СВОЕЙ
    сети, а не в посевной).
    """
    create_body_extra = create_body_extra or {}
    cross_body_extra = create_body_extra if cross_body_extra is None else cross_body_extra
    plural_path = f"/vpc/v1/{plural}"

    for subj in SUBJECTS:
        code = subj[0]
        anchor_var = f"authz{resource_name.title().replace('-', '')}AnchorNet{code}"
        anchor_name = f"authz-{resource_name}-anchor-{code.lower()}-{{{{runId}}}}"

        # === Create в own project A1 (editor-scope) ===
        body_own = {"projectId": "{{projectA1Id}}", "name": f"authz-{resource_name}-{code.lower()}-own-{{{{runId}}}}", **create_body_extra}
        # Тело ALLOW-полосы нарезки отличается ОДНИМ полем — сетью, созданной этим же
        # кейсом. У DENY-полосы такой сети нет и быть не может (её субъекту создание
        # запрещено), поэтому там стоит заведомо нерезолвящаяся ссылка: запрос до
        # сервиса не доходит, и ссылка на общую посевную сеть читалась бы как нарезка
        # в ней — то есть как ровно тот дефект, который снят.
        allow_body_own = ({**body_own, "networkId": "{{" + anchor_var + "}}"}
                          if own_lane == "carve" else None)
        emit(f"{resource_name.upper()}-CR-OWN", f"Create {resource_name} в project-A1", "project-A1",
             "POST", plural_path, body_own, subj, mode="gate",
             allow_lane=own_lane, allow_body=allow_body_own,
             anchor_project_var="projectA1Id", anchor_name=anchor_name, anchor_var=anchor_var,
             peer_ref_expr=own_peer_ref_expr, peer_ref_resource=peer_ref_resource)

        # === Create в cross-account project B1 ===
        body_cross = {"projectId": "{{projectB1Id}}", "name": f"authz-{resource_name}-{code.lower()}-cross-{{{{runId}}}}", **cross_body_extra}
        emit(f"{resource_name.upper()}-CR-CROSS", f"Create {resource_name} в project-B1 (cross-account)", "project-B1",
             "POST", plural_path, body_cross, subj, mode="gate",
             allow_lane=cross_lane, anchor_project_var="projectB1Id",
             anchor_name=anchor_name, anchor_var=anchor_var,
             peer_ref_expr=cross_peer_ref_expr, peer_ref_resource=peer_ref_resource)

        # === List в own project (scope-filtered) ===
        emit(f"{resource_name.upper()}-LS-OWN", f"List {plural} в project-A1", "project-A1",
             "GET", f"{plural_path}?projectId={{{{projectA1Id}}}}", None, subj, mode="list", list_key=plural)

        # === List в cross-account project (scope-filtered) ===
        emit(f"{resource_name.upper()}-LS-CROSS", f"List {plural} в project-B1 (cross-account)", "project-B1",
             "GET", f"{plural_path}?projectId={{{{projectB1Id}}}}", None, subj, mode="list", list_key=plural)

        # === Get garbage-id → existence-hiding 404 ===
        emit(f"{resource_name.upper()}-GT-OWN", f"Get {resource_name} (well-formed nonexistent id)", "project-A1",
             "GET", f"{plural_path}/{GARBAGE_ID}", None, subj, mode="nf")

        if supports_update:
            # === Update garbage-id → mutation-deny 403 ===
            emit(f"{resource_name.upper()}-UP-OWN", f"Update {resource_name} (well-formed nonexistent id)", "project-A1",
                 "PATCH", f"{plural_path}/{GARBAGE_ID}", {"name": "x"}, subj, mode="deny")

        # === Delete garbage-id → mutation-deny 403 ===
        emit(f"{resource_name.upper()}-DL-OWN", f"Delete {resource_name} (well-formed nonexistent id)", "project-A1",
             "DELETE", f"{plural_path}/{GARBAGE_ID}", None, subj, mode="deny")


# Network — тело самодостаточно, обе позиции создают на самом деле.
define_resource_cases("network", "networks")

# Subnet. OWN-полоса режет в СВОЕЙ сети, созданной этим же кейсом (полоса `carve`,
# разбор — в `allow_steps`); CROSS-полоса намеренно ссылается на сеть ЧУЖОГО проекта и
# получает синхронный `404 NOT_FOUND "Network <id> not found"` — тот же ответ, что и на
# несуществующую сеть, без оракула существования (`subnet/create.go`, BOLA-guard).
define_resource_cases(
    "subnet", "subnets",
    create_body_extra={"networkId": NONEXISTENT_NETWORK_ID, "zoneId": "{{zoneA}}",
                       "ipv4CidrPrimary": CARVED_SUBNET_CIDR},
    own_lane="carve",
    cross_body_extra={"networkId": "{{seedNetworkA1Id}}", "zoneId": "{{zoneA}}",
                      "ipv4CidrPrimary": CARVED_SUBNET_CIDR},
    cross_lane="peer_missing",
    cross_peer_ref_expr="pm.environment.get('seedNetworkA1Id')",
)

# Address. Здесь стояла внешняя IPv4-область (`externalIpv4AddressSpec` в зоне A) —
# аренда из ОБЩЕГО умолчательного пула, который эта суита не сеет и не убирает:
# ALLOW-полоса зависела от того, кто прогонялся раньше, и утекало пять адресов за
# прогон из пула на 256. Внутренняя область с заведомо нерезолвящейся подсетью даёт
# ОДИН установленный исход, ничего не занимает и ничего не оставляет.
#
# ПОЛОСА СИНХРОННАЯ, А НЕ ВНУТРИ ОПЕРАЦИИ — И УСТАНОВИЛ ЭТО ПРОГОН, А НЕ ЧТЕНИЕ.
# Здесь стояло `anchor_missing` (200 + операция с ошибкой) по аналогии со шлюзом и
# интерфейсом: у обоих подсеть читается в `doCreate`. У адреса — нет.
# `assertSubnetOwned` зовётся ДВАЖДЫ: из `applyAddressSpec` (внутри операции) и,
# РАНЬШЕ, прямо из `Execute` (`address/create.go:221`) — до создания Operation.
# Первое чтение кода нашло только вызов из `applyAddressSpec`, потому что искало
# место вызова `applyAddressSpec`, а не самой проверки; вторая координата в поиск не
# попала. Ошибку назвал сквозной прогон, и назвал ТЕКСТОМ отказа, а не именем шага:
# `{"code":5,"message":"Subnet subnonexistent000001 not found"}: expected 404 to
# equal 200` — ровно пять кейсов, ровно этот ресурс. Утверждение сделало то, ради
# чего заведено: разошлось с продуктом и назвало, в чём.
define_resource_cases("address", "addresses", create_body_extra={
    "internalIpv4AddressSpec": {"subnetId": NONEXISTENT_SUBNET_ID}
}, own_lane="peer_missing", cross_lane="peer_missing",
    own_peer_ref_expr=f"'{NONEXISTENT_SUBNET_ID}'",
    cross_peer_ref_expr=f"'{NONEXISTENT_SUBNET_ID}'",
    peer_ref_resource="Subnet")

# RouteTable / SecurityGroup — CIDR не несут, имена несут токен прогона, поэтому
# OWN-полоса создаёт по-настоящему в посевной сети проекта A1. CROSS-полоса ссылается
# на ту же сеть из проекта B1 — сеть чужого проекта, тот же синхронный 404/5.
define_resource_cases(
    "route-table", "routeTables",
    create_body_extra={"networkId": "{{seedNetworkA1Id}}"},
    cross_lane="peer_missing",
    cross_peer_ref_expr="pm.environment.get('seedNetworkA1Id')",
)
define_resource_cases(
    "security-group", "securityGroups",
    create_body_extra={"networkId": "{{seedNetworkA1Id}}"},
    cross_lane="peer_missing",
    cross_peer_ref_expr="pm.environment.get('seedNetworkA1Id')",
)

# Gateway. Ветвь вида и якорь размещения — поля ЖИВОГО контракта; прежняя ветвь снята
# с резервированием номера и имени, и её имя здесь не воспроизводится.
#
# ЯКОРЬ ЗАВЕДОМО НЕРЕЗОЛВЯЩИЙСЯ — И ТЕПЕРЬ ЭТО УТВЕРЖДАЕТСЯ, А НЕ ТЕРПИТСЯ. Прежде
# рядом стояло объяснение: раз утверждение суиты проверяет лишь отсутствие отказа в
# правах, телу нужна верная ФОРМА полей, а не резолвящиеся значения. Основание ушло
# вместе с тем утверждением: ALLOW-полоса больше не отрицание, поэтому нерезолвящаяся
# подсеть перестала быть уступкой и стала ПРЕДМЕТОМ — операция обязана завершиться
# `NOT_FOUND "Subnet <id> not found"` (`gateway/create.go`, `allocateExternalAddress`),
# и этот текст закреплён.
#
# ЧТО ЭТО ЛОВИТ. Тело обязано быть полеверным: край разбирает его строго, и незнакомое
# поле отвергается ДО решения о правах — тогда кейс зеленел бы, ни разу не спросив про
# права, то есть перестал бы быть тем, чем назван. Ровно это здесь и случилось: снятие
# ветви прошло по кейсам шлюза и маршрутов, но не по этому файлу, и гейт тел коллекций
# нашёл 12 вхождений. Радиус берётся по имени снятого поля, а не по диффу, в котором его
# заметили.
define_resource_cases("gateway", "gateways", create_body_extra={
    "natGatewaySpec": {},
    "subnetId": NONEXISTENT_SUBNET_ID,
}, own_lane="anchor_missing", cross_lane="anchor_missing")

# NetworkInterface. Здесь под именем подсети стоял идентификатор СЕТИ
# (`{{seedNetworkA1Id}}`) — соседний кейс шлюза называл это «написать неправду ради
# значения, которое кейс не читает». Кейс его теперь читает: якорь заведомо
# нерезолвящийся и объявлен таковым, исход установлен (`networkinterface/create.go`,
# `doCreate` → `Subnets().Get`).
define_resource_cases("nic", "networkInterfaces", create_body_extra={
    "subnetId": NONEXISTENT_SUBNET_ID,
}, own_lane="anchor_missing", cross_lane="anchor_missing")


# ---------------------------------------------------------------------------
# AddressPool — admin-only (cluster system_admin); все 6 субъектов DENY
# ---------------------------------------------------------------------------

APL_GARBAGE_ID = "aplnonexistent000001"

for subj in SUBJECTS:
    # Create AddressPool — admin-only: каждый аутентифицированный без system_admin → 403.
    emit("APL-CR", "Create AddressPool (admin-only)", "addresspool-admin-only",
         "POST", "/vpc/v1/addressPools",
         {"name": f"authz-apl-{subj[0].lower()}-{{{{runId}}}}",
          "kind": "EXTERNAL_PUBLIC",
          "zoneId": "{{zoneA}}",
          "v4CidrBlocks": ["198.51.100.0/24"]}, subj, mode="gate")
    # Update/Delete nonexistent AddressPool — admin-only мутация: non-admin → 403.
    emit("APL-UP", "Update AddressPool (admin-only, nonexistent id)", "addresspool-admin-only",
         "PATCH", f"/vpc/v1/addressPools/{APL_GARBAGE_ID}", {"name": "x"}, subj, mode="gate")
    emit("APL-DL", "Delete AddressPool (admin-only, nonexistent id)", "addresspool-admin-only",
         "DELETE", f"/vpc/v1/addressPools/{APL_GARBAGE_ID}", None, subj, mode="gate")


# ---------------------------------------------------------------------------
# Cross-domain / data-leak cases
# ---------------------------------------------------------------------------

# AddressPool.List на public endpoint — admin-only (cluster system_admin): non-admin
# субъект НЕ может перечислить инфраструктурные пулы (anti data-leak). 403 у всех.
for subj in SUBJECTS:
    emit("DATA-LEAK-APL-LS", "AddressPool.List на public listener (infra data-leak guard)",
         "addresspool-admin-only", "GET", "/vpc/v1/addressPools", None, subj, mode="gate")

# Create Subnet в project-A1 со ссылкой на network из cross-account project-B1.
# Authz-граница здесь — право создавать subnet в project-A1 (editor-scope): субъекты
# без A1-доступа → 403; с доступом край пропускает, и владелец отвечает синхронным
# `404 NOT_FOUND "Network <id> not found"` — сеть чужого проекта неотличима от
# несуществующей (BOLA-guard `subnet/create.go`, без оракула существования).
#
# Прежде эта строка называла исход «отбивается peer-validation downstream, не в этой
# суите» и не утверждала о нём ничего — то есть кейс принимал и его, и успех, и любой
# другой отказ, кроме 403/401. Нарезки здесь нет by construction: до проверки адресов
# запрос не доходит, поэтому адрес — часть формы, а не позиция в чьём-то плане.
for subj in SUBJECTS:
    emit("CD-SUBNET-XACCT", "Create Subnet ссылающийся на network из cross-account project",
         "project-A1", "POST", "/vpc/v1/subnets",
         {"projectId":"{{projectA1Id}}","name": f"cd-{subj[0].lower()}-{{{{runId}}}}",
          "networkId":"{{seedNetworkB1Id}}","zoneId":"{{zoneA}}",
          "ipv4CidrPrimary": CARVED_SUBNET_CIDR}, subj, mode="gate",
         allow_lane="peer_missing",
         peer_ref_expr="pm.environment.get('seedNetworkB1Id')")
