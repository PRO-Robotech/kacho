# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для DiskTypeService (kacho-storage) — stage S2 CS1-S2-*.

DiskType — admin-каталог: public read (DiskTypeService.Get/List, sync) +
admin CRUD (InternalDiskTypeService.Create/Update/Delete/SetLifecycle) на :9091
(internal-mux only, ban #6). DiskType.id — admin-assigned slug, НЕ генерируемый
префикс.

ПОСЕВА НЕТ — и это решение, а не пробел. Класс не может быть предложен арендатору
раньше, чем администратор объявит, ЧЕМ он обслуживается: класс без действующей
ревизии привязки к кластеру данных — обещание ёмкости, которой никто не дал.
Прежняя редакция этого файла пинила пять посеянных слагов миграции; посев снят
вместе с миграцией, и утверждения о нём стали ложными. Здесь они не ослаблены, а
ПЕРЕАДРЕСОВАНЫ: содержательная сторона («созданный класс читается публично со
своим ярусом, обращением и способностями») проверяется там, где достижима
административная полоса, — internal/repo/pg/disk_type_integration_test.go,
TestDiskTypeCreateUpdateAdmin · TestDiskTypeCatalogEmptyAfterSeedRemoval ·
TestDiskTypePolicyRoundTrip · TestDiskTypeCapabilitiesIntersectActiveBindings.

Почему не засеять каталог отсюда: эти коллекции ходят ТОЛЬКО на внешний слушатель
(тот же предел, что у attach в internal-volume.py), а административная полоса
живёт на :9091. Кейс, которому нужно недоступное здесь условие, маски не получает
— он получает исполнителя, который это условие создаёт, и тот назван выше
поимённо.

Ярус переехал из свободной строки в закрытый словарь: поле `tier`
(CAPACITY/BALANCED/FAST/SINGLE/IO_MAX), прежнее `performanceTier` снято с
контракта вместе с номером и именем. Проба, продолжавшая слать снятое поле,
поймана гейтом тела запросов на крае — не глазами.

Black-box coverage:
  - public read (List/Get/NotFound) — CS1-S2-01.
  - INV-7a (admin CRUD Internal-only, отсутствует на external endpoint) — CS1-S2-04.
Не-black-box (integration-only, НЕ здесь): admin Create/Update/Delete happy +
FK-RESTRICT delete-in-use (CS1-S2-02/03/05) — доступны только на :9091 mTLS через
internal-mux + per-RPC system_admin Check → покрываются integration-тестами
(testcontainers) и internal-mux ручным прогоном, не external newman.
"""

CASES = []

DT = "/storage/v1/diskTypes"

# Закрытый словарь яруса. Публичное чтение обязано отдавать значение ИЗ НЕГО либо
# не отдавать ярус вовсе (необъявленный ярус законен). Свободная строка здесь —
# находка: именно ею инфра-подробность утекала сквозь гейт, перечисляющий имена.
_TIERS = ["PERFORMANCE_TIER_UNSPECIFIED", "CAPACITY", "BALANCED", "FAST", "SINGLE", "IO_MAX"]
_LIFECYCLES = ["ACTIVE", "DEPRECATED", "RETIRED"]


# ---------------------------------------------------------------------------
# CS1-S2-01 — public read (List / Get / NotFound)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="DT-LST-CRUD-OK",
    title="List diskTypes → массив; у КАЖДОЙ записи tier и lifecycle из закрытых словарей, zoneIds array, limits — целые границы, capabilities — только булевы, и НИ ОДНОГО инфра-поля",
    classes=["CRUD", "CONF", "SEC"], priority="P1",
    # verifies CS1-S2-01, STOR-P-69
    #
    # Пустой каталог — законный ответ (посева нет), поэтому длина не утверждается.
    # Утверждается СВОЙСТВО КАЖДОЙ записи, и оно различающее: ярус свободной
    # строкой роняет кейс, как ронял бы адрес пула или имя пространства имён.
    #
    # `capabilities` и `limits` — поля политики, добавленные вместе с ярусом.
    # Способности проверяются на ТИП: они выводятся пересечением действующих
    # ревизий привязки, и любое НЕбулево значение здесь означало бы, что на
    # публичную поверхность просочилось содержимое ревизии, а не вывод из неё.
    # Границы — целые числа (int64 приезжает строкой), потому что по ним
    # энфорсится размер тома: нечисловая граница сделала бы проверку размера
    # тождественно-истинной.
    steps=[Step(name="list", method="GET", path=DT,
                test_script=[*assert_status(200),
                             "const j = pm.response.json();",
                             "pm.test('diskTypes is array', () => pm.expect(j.diskTypes || []).to.be.an('array'));",
                             f"const TIERS = {_TIERS!r}.map(String);",
                             f"const LIFE = {_LIFECYCLES!r}.map(String);",
                             "const CAPS = ['snapshots','cloneFromSnapshot','cloneFromImage','onlineGrow','multiAttach','encryptionAtRest'];",
                             "const LIMS = ['minSizeBytes','maxSizeBytes','sizeStepBytes'];",
                             "(j.diskTypes || []).forEach(t => {",
                             "  pm.test('tier is from the closed dictionary: ' + t.id, () => pm.expect(TIERS).to.include(t.tier === undefined ? 'PERFORMANCE_TIER_UNSPECIFIED' : String(t.tier)));",
                             "  pm.test('lifecycle is from the closed dictionary: ' + t.id, () => pm.expect(LIFE).to.include(String(t.lifecycle)));",
                             "  pm.test('zoneIds is array: ' + t.id, () => pm.expect(t.zoneIds || []).to.be.an('array'));",
                             "  pm.test('capabilities — только объявленные булевы способности: ' + t.id, () => {",
                             "    const c = t.capabilities || {};",
                             "    Object.keys(c).forEach(k => { pm.expect(CAPS, 'посторонний ключ способностей ' + k).to.include(k); pm.expect(c[k], k).to.be.a('boolean'); });",
                             "  });",
                             "  pm.test('limits — целые границы размера: ' + t.id, () => {",
                             "    const l = t.limits || {};",
                             "    Object.keys(l).forEach(k => { pm.expect(LIMS, 'посторонний ключ границ ' + k).to.include(k); pm.expect(Number(l[k]), k + '=' + l[k]).to.be.a('number'); pm.expect(Number.isFinite(Number(l[k])), k).to.eql(true); });",
                             "  });",
                             "  pm.test('no infra field leaks: ' + t.id, () => ['backendId','backendObject','backendNamespace','endpoint','credentialsRef','locator','poolName','qos','revision'].forEach(k => pm.expect(t, k).to.not.have.property(k)));",
                             "});",
                             "pm.test('performanceTier is gone from the contract', () => (j.diskTypes || []).forEach(t => pm.expect(t, t.id).to.not.have.property('performanceTier')));"])],
))

CASES.append(Case(
    id="DT-GET-CRUD-OK",
    title="Get класса, ВЗЯТОГО ИЗ СПИСКА → те же поля, что отдал список (id/tier/lifecycle/zoneIds); пустой каталог — находка, а не зелёный",
    classes=["CRUD", "CONF"], priority="P1",
    # verifies CS1-S2-01
    #
    # Прежняя редакция пинила посеянный слаг. Посев снят, и пин стал ссылкой в
    # пустоту. Здесь id БЕРЁТСЯ ИЗ ОТВЕТА — так проба переживает любой состав
    # каталога и по-прежнему утверждает содержательное: элемент и коллекция
    # говорят об одном ресурсе одно и то же.
    #
    # Пустой каталог НЕ пропускается молча. На поднятом стенде он означает, что
    # шаг подъёма `seed-storage` не отработал, и тогда каждое создание тома
    # ответит отказом — то есть это состояние стенда, а не «нечего проверять».
    steps=[Step(name="list-for-id", method="GET", path=DT,
                test_script=[*assert_status(200),
                             "const j = pm.response.json();",
                             "const ts = j.diskTypes || [];",
                             "pm.test('каталог не пуст (иначе стенд не обслуживает тома — см. deploy seed-storage)', () => pm.expect(ts.length, 'классов в каталоге').to.be.at.least(1));",
                             "if (ts.length) {",
                             "  pm.environment.set('dtProbeId', ts[0].id);",
                             "  pm.environment.set('dtProbeTier', ts[0].tier === undefined ? 'PERFORMANCE_TIER_UNSPECIFIED' : String(ts[0].tier));",
                             "  pm.environment.set('dtProbeLifecycle', String(ts[0].lifecycle));",
                             "  pm.environment.set('dtProbeCaps', JSON.stringify(ts[0].capabilities || {}));",
                             "  pm.environment.set('dtProbeLimits', JSON.stringify(ts[0].limits || {}));",
                             "}"]),
           Step(name="get", method="GET", path=f"{DT}/{{{{dtProbeId}}}}",
                test_script=[*assert_status(200),
                             "const j = pm.response.json();",
                             "pm.test('id совпадает со списочным', () => pm.expect(j.id).to.eql(pm.environment.get('dtProbeId')));",
                             "pm.test('tier совпадает со списочным', () => pm.expect(j.tier === undefined ? 'PERFORMANCE_TIER_UNSPECIFIED' : String(j.tier)).to.eql(pm.environment.get('dtProbeTier')));",
                             "pm.test('lifecycle совпадает со списочным', () => pm.expect(String(j.lifecycle)).to.eql(pm.environment.get('dtProbeLifecycle')));",
                             # Способности и границы — политика класса, и элемент обязан
                             # говорить о ней ТО ЖЕ, что коллекция. Расхождение здесь
                             # означало бы две проекции одного факта: арендатор читает
                             # список, планирует по нему, а карточка отвечает иначе.
                             "pm.test('capabilities совпадают со списочными', () => pm.expect(JSON.stringify(j.capabilities || {})).to.eql(pm.environment.get('dtProbeCaps')));",
                             "pm.test('limits совпадают со списочными', () => pm.expect(JSON.stringify(j.limits || {})).to.eql(pm.environment.get('dtProbeLimits')));",
                             "pm.test('zoneIds is array', () => pm.expect(j.zoneIds || []).to.be.an('array'));"])],
))

CASES.append(Case(
    id="DT-GET-NEG-NOTFOUND",
    title="Get block-nope → 404 NOT_FOUND 'DiskType block-nope not found' (тон контракта, id в тексте)",
    classes=["NEG", "CONF"], priority="P0",
    # verifies CS1-S2-01
    steps=[Step(name="get-nx", method="GET", path=f"{DT}/block-nope",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             "pm.test('message includes \"DiskType block-nope not found\"', () => pm.expect((pm.response.json().message || ''), JSON.stringify(pm.response.json())).to.include('DiskType block-nope not found'));"])],
))

CASES.append(Case(
    id="DT-LST-BVA-PAGESIZE-OVER-MAX",
    title="List diskTypes pageSize=1001 (> max 1000) → 400 INVALID_ARGUMENT",
    classes=["BVA", "VAL", "PAGE"], priority="P1",
    # verifies CS1-S2-01
    steps=[Step(name="ps-over", method="GET", path=f"{DT}?pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# ---------------------------------------------------------------------------
# CS1-S2-04 — admin CRUD Internal-only: absent on external endpoint (INV-7a)
# ---------------------------------------------------------------------------

# INV-7a. ВАЖНО: прежний текст здесь утверждал, что admin CRUD на external ДОСТИЖИМ и
# держится одним лишь authz-каталогом. Это больше не так и было дефектом, пока было так:
# диспетчер решает по паре (метод, путь), таблица внутренних биндингов выводится из
# proto-дескрипторов, и все три админ-метода на внешнем слушателе отбиваются маршрутом.
# Публичное чтение того же пути при этом сохранено — путь один, методы разные.
#
# Ожидание сужено соответственно: 401/403/404 и НИКОГДА 200. Не строгий 404, потому что
# authz-прослойка обёрнута СНАРУЖИ диспетчера — аутентифицированный не-админ получит 403,
# неаутентифицированный 401, и до 404-гейта они не доходят. 400/405/501 стали
# недостижимы: валидация тела за отбитым маршрутом не выполняется.
#
# Тело запроса остаётся non-destructive (пустой id на создании, несуществующая цель на
# изменении и удалении): прежняя форма слала валидное тело и на успехе МУТИРОВАЛА посев.
# Это свойство кейса сохраняем независимо от того, отбивает ли маршрут — проверка не
# должна разрушать стенд, даже когда она красная.
# Набор отказов включает 405/501 — это ФОРМА ОТКАЗА ВНЕШНЕГО ЛИСТЕНЕРА, а не послабление.
#
# Запрос, классифицированный как internal, но пришедший на внешний листенер, отдаётся
# publicMux'у, и ответ производит сам grpc-gateway — ровно как если бы админской
# поверхности не существовало вовсе: 404 там, где путь неизвестен, и 501/405 там, где путь
# ПУБЛИЧНО существует под другим методом. Коллекция diskTypes — как раз этот случай:
# публичное чтение живёт на том же пути, что админский CRUD. Отдельный `http.NotFound`
# здесь стоял раньше и был снят намеренно: он отличался Content-Type и телом, то есть сам
# отвечал на вопрос «есть ли тут админский путь» (см. gateway/internal/restmux/mux.go и
# external_refusal_shape_test.go).
#
# Утверждение при этом НЕ ослаблено: все пять кодов — отказы, 200 по-прежнему запрещён,
# и именно 200 (мутация) — то, что кейс существует ловить. Эталон набора — geo
# `admin-not-on-public.py`, где стоит [403, 404, 405, 501] и суита зелёная.
CASES.append(Case(
    id="DT-CR-NEG-EXTERNAL-ABSENT",
    title="POST /storage/v1/diskTypes empty-id на external → rejected (admin Create Internal-only, ban #6): 400/403/404 — НИКОГДА 200-mutation",
    classes=["SEC", "NEG", "AUTHZ"], priority="P0",
    # verifies CS1-S2-04 (INV-7a). Non-destructive: пустой id не даёт вставки даже
    #   в том случае, если маршрут вдруг окажется забриджен на внешний край.
    #
    # ВЕТКА НА 400 СНЯТА (#1404), и снята она не как «лишняя проверка».
    #
    # Шаг нёс ДВА утверждения об одном исходе, противоречащих друг другу: допуск
    # 400 не перечисляет, а ветка его пинила. Ветка исполнима только ВМЕСТЕ с
    # падением допуска — то есть по отчёту нельзя было сказать, какое из двух
    # выражает намерение автора (`e2e-flow.md` §3: утверждение, принимающее
    # взаимоисключающие исходы, — отсутствие утверждения).
    #
    # Разрешается это не расширением допуска, а тем, ЧТО ЗНАЧИЛ БЫ здесь 400.
    # `POST /storage/v1/diskTypes` — привязка admin-only `InternalDiskTypeService`
    # (public по этому пути только `GET`), и край классифицирует её по ПАРЕ
    # (метод, путь) как internal-маршрут: на внешнем слушателе он не резолвится.
    # Значит до бэкенда запрос не доходит, а `INVALID_ARGUMENT` производит
    # ВАЛИДАЦИЯ БЭКЕНДА — то есть 400 на этом шаге означал бы, что маршрут
    # забриджен наружу и authz его пропустил, и от вставки спасла одна лишь
    # пустота идентификатора. Это ровно тот исход, который кейс объявляет
    # недопустимым (ban #6), а не «тоже отказ»: пинить его зелёным утверждением
    # значило бы отчитываться об успехе на своей находке.
    steps=[Step(name="cr-external", method="POST", path=DT, body={"id": ""},
                test_script=[
                    "pm.test('admin Create not usable on external (no 200 mutation)', () => pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([401, 403, 404, 405, 501]));"])],
))

CASES.append(Case(
    id="DT-UPD-NEG-EXTERNAL-ABSENT",
    title="PATCH /storage/v1/diskTypes/<nonexistent> на external → rejected (admin Update Internal-only): 401/403/404/405/501 — НИКОГДА 200/мутация каталога",
    classes=["SEC", "NEG", "AUTHZ"], priority="P0",
    # verifies CS1-S2-04 (INV-7a). Non-destructive: цель run-scoped и заведомо
    #   несуществующая — каталог не мутируется даже если маршрут когда-нибудь
    #   забриджат. Ни один класс каталога здесь не назван: его состав заводит шаг
    #   подъёма стенда, и литерал был бы утверждением о посеве, которого нет.
    steps=[Step(name="upd-external", method="PATCH", path=f"{DT}/block-newman-nx-{{{{runId}}}}",
                body={"name": "block-hacked"},
                test_script=["pm.test('admin Update not usable on external (no 200 mutation)', () => pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([401, 403, 404, 405, 501]));"])],
))

CASES.append(Case(
    id="DT-DEL-NEG-EXTERNAL-ABSENT",
    title="DELETE /storage/v1/diskTypes/<nonexistent> на external → rejected (admin Delete Internal-only): 401/403/404/405/501 — НИКОГДА 200/удаление класса",
    classes=["SEC", "NEG", "AUTHZ"], priority="P0",
    # verifies CS1-S2-04 (INV-7a). Non-destructive: цель run-scoped и заведомо
    #   несуществующая. Прежняя форма называла класс каталога поимённо и на успехе
    #   УДАЛЯЛА его — проверка, разрушающая стенд, недопустима и в красном виде.
    steps=[Step(name="del-external", method="DELETE", path=f"{DT}/block-newman-nx-{{{{runId}}}}",
                test_script=["pm.test('admin Delete not usable on external (no 200 mutation)', () => pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([401, 403, 404, 405, 501]));"])],
))

# ---------------------------------------------------------------------------
# CS1-S2-01 (add) — List garbage page_token parity. DiskType.List — cursor
#   (created_at,id) с decodePageToken -> ErrInvalidArg (400) на битом токене
#   (парити с Volume/Snapshot/Image, у DiskType отсутствовал). Техника: negative
#   opaque-token декод.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="DT-LST-PAGE-TOKEN-GARBAGE",
    title="List diskTypes с garbage pageToken -> 400 INVALID_ARGUMENT (decodePageToken -> ErrInvalidArg; парити с Volume/Snapshot/Image)",
    classes=["PAGE", "VAL", "NEG"], priority="P1",
    # verifies CS1-S2-01
    steps=[Step(name="bad-token", method="GET", path=f"{DT}?pageSize=10&pageToken=not-a-real-token",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))
