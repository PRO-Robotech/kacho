# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Authz-покрытие RegistryService (kacho-registry) — existence-hiding + verb-tier.

Black-box через api-gateway REST (`/registry/v1/registries`). Проверяет инвариант
безопасности: субъект без нужной verb-relation на существующем ресурсе денается без
раскрытия существования ресурса (никакого `deny_reasons`-оракула наружу, никакого
success-with-data), а `List` не-члена возвращает пустой массив (не 403).

Субъекты приходят из ОБЩЕГО ПОСЕВА ФИКСТУР, а не из закоммиченных окружений:
`tests/authz-fixtures/prodseed_matrix.py` заводит и наблюдателя (`jwtProjectViewerA`),
и негрантнутого чужака (`jwtStranger`), и кладёт их токены в окружение суиты
(`prodrun.sh` → `patch-env.py`). В закоммиченные окружения подписанные токены не
кладутся НИКОГДА, поэтому там эти переменные пусты by design.

Прежняя редакция этой шапки объявляла обратное — «стенд single-user, viewer здесь не
провиженится» — и на этом основании часть кейсов молчала. Обоснование устарело: посев
такого субъекта заводит. Отсутствие фикстуры теперь ОТКАЗ (шаг краснеет и называет
переменную), а не пропуск; прогон суиты без посева — неверная конфигурация, и она
видна сразу.

Диапазоны кодов отказа остаются широкими — отказать может любой из трёх рубежей, и
какой сработает первым, зависит от порядка проверок и от того, заведён ли субъект:

- **Stranger** — dev-JWT с незарегистрированным `sub` gateway трактует как
  UNAUTHENTICATED → HTTP 401 (code 16, "subject: unauthenticated request"), НЕ как
  authenticated-но-denied 404. Поэтому кейсы на ОБЪЕКТНЫХ шагах принимают несколько
  отказных кодов `[401, 403, 404]` — какой рубеж откажет первым, зависит от стенда, —
  но **только отказные**. Успех в этот список не входит.
  СПИСОЧНЫЙ шаг — отдельная полоса, и там диапазон НЕ широкий: у
  `RegistryService/List` call-gate'а нет вовсе (каталог `<exempt>`, в сервисе
  `ScopeFiltered`), поэтому ни 403, ни 404 эта полоса произвести не может. Законных
  исходов ровно два, и различает их ПОСЕВ: незарегистрированный `sub` → 401/16;
  реальный негрантнутый субъект → 200 + ПУСТОЙ массив. Обе формы разведены и обе
  утверждают (`_assert_list_filtered_empty` / `_assert_unauthenticated`, развилка
  `_assert_stranger_list`). Существование фикстуры (её regId) не раскрывается ни в
  одном ответе. Проверка «нет deny_reasons» гейтится на код != 401
  (unauthenticated-тело несёт generic-причину, не resource-existence leak).
- **Viewer-tier** (GET-VIEWER-OK / UPDATE-VIEWER-DENY / DELETE-VIEWER-DENY и их
  repo-близнецы) — требуют субъекта-наблюдателя из посева. При пустом
  `jwtProjectViewerA` кейс ОТКАЗЫВАЕТ (падающее утверждение с именем переменной и
  указанием, чем её заполнить) — не молчит и не зеленеет. Граница «viewer держит
  v_get, но НЕ v_update/v_delete → Update/Delete = NOT_FOUND (existence-hidden)»
  ДОПОЛНИТЕЛЬНО покрыта всегда-исполняемым Go-тестом
  internal/check/viewer_boundary_test.go (реальный corelib authz-interceptor поверх
  registry PermissionMap + fake CheckClient, грантящий ровно v_get). Покрытие в
  другом месте — не основание молчать здесь.

Инвариант existence-hiding (authenticated-ungranted → 404, никогда 403, без leak'а)
отдельно верифицируется GREEN control-plane-сьютом и data-plane-харнессом.

Фикстура: setup создаёт registry от project-editor (сохраняет {{regIdAz}}); кейсы
исполняются от разных субъектов (jwtStranger / jwtProjectViewerA / anonymous);
cleanup удаляет registry от editor ПОСЛЕДНИМ. Изоляция — свой runId; работает в
pre-allocated existingProjectId (env). Мутации async → Operation (poll до done);
read sync (200 напрямую).
"""

CASES = []

REG = "/registry/v1/registries"

# Registry-операции несут id-префикс rop/reo (opsproxy-роутинг в api-gateway).
_OP_PREFIX = "^(rop|reo)[a-z0-9]+$"


def _assert_denied():
    # ОТКАЗ, и только отказ. Диапазон кодов широкий, потому что отказать может любой
    # из трёх рубежей (gateway на неаутентифицированном субъекте → 401; scope-Check →
    # 403; existence-hiding → 404) — какой сработает первым, зависит от порядка
    # проверок и от того, заведён ли субъект на стенде. Все три означают одно: запрос
    # не выполнен. Успех в этот список не входит: принять 200 рядом с 401/403/404
    # значит принять ровно тот исход, ради ловли которого кейс и написан, — то есть
    # не утверждать ничего (testing.md, директива владельца 2026-07-29).
    return ["pm.test('denied (401/403/404), never 200', () => pm.expect(pm.response.code).to.be.oneOf([401, 403, 404]));"]


def _assert_list_filtered_empty(field):
    """ФОРМА 1 — отфильтрованный успех: субъект аутентифицирован, прав нет, страница ПУСТА.

    `RegistryService/List` не имеет call-gate'а на краю (запись каталога `<exempt>`) и
    помечен `ScopeFiltered` в сервисе (`internal/check/permission_map.go`), то есть
    per-RPC Check для него не выполняется вовсе, а отбор делает хендлер построчно.
    Поэтому у аутентифицированного не-члена законный исход ровно один: 200 с ПУСТЫМ
    массивом. 200 с непустым массивом — утечка."""
    return [
        f"pm.test('non-member sees an EMPTY {field} page (scope-filtered rows)', () => {{",
        f"  const _arr = pm.response.json().{field};",
        f"  pm.expect(_arr, JSON.stringify(pm.response.json())).to.be.an('array');",
        f"  pm.expect(_arr.length, 'non-member must see nothing (LEAK!): ' + pm.response.text()).to.eql(0);",
        "});",
    ]


def _assert_unauthenticated():
    """ФОРМА 2 — отказ аутентификации: субъекта нет, край отказывает 401/16.

    Именно отказ, а не «какой-нибудь 4xx»: код 401 и grpc-код 16. Прежний диапазон
    включал 403 и 404 «какой рубеж откажет первым» — но на этой полосе рубежа
    scope-Check'а НЕТ (см. форму 1), а списочный маршрут существует, поэтому ни 403, ни
    404 она произвести не может: они были не терпимостью, а незаполненными местами."""
    return [
        "pm.test('unauthenticated subject is refused (401, grpc 16)', () => {",
        "  pm.expect(pm.response.code, pm.response.text()).to.eql(401);",
        "  let _j; try { _j = pm.response.json(); } catch (e) { _j = null; }",
        "  pm.expect(_j && _j.code, JSON.stringify(_j)).to.eql(16);",
        "});",
    ]


def _assert_stranger_list(field):
    """Развилка между двумя формами — по ТОМУ, ЧТО ЗАСЕЯЛ ПОСЕВ, и обе ветки утверждают.

    Матрица «субъект × право на список» для `jwtStranger` даёт РОВНО два законных
    исхода, и различает их посев, а не поведение продукта:

      - `tests/authz-fixtures/setup.sh` (dev-полоса) кладёт в `jwtStranger` токен с
        НЕЗАРЕГИСТРИРОВАННЫМ `sub` (пользователь намеренно не заводится) → край не
        резолвит принципала → 401/16 → ФОРМА 2;
      - `tests/authz-fixtures/prodseed_matrix.py` (prod-полоса) кладёт токен реального,
        но НИЧЕМ НЕ ГРАНТНУТОГО субъекта → аутентификация проходит, call-gate'а нет,
        построчный отбор оставляет пусто → 200 + [] → ФОРМА 1.

    Поэтому здесь НЕ `oneOf`, принимающий успех рядом с отказом, а развилка, у которой
    КАЖДАЯ ветка несёт полное утверждение своей формы; третий код не проходит ни одну
    (ветка `else` требует именно 401). Что осталось долгом и названо, а не спрятано: две
    полосы посева вкладывают в ОДНУ переменную окружения субъектов разного рода, поэтому
    кейс не может требовать одного исхода. Закрепление смысла `jwtStranger` за одним
    родом субъекта сделало бы утверждение однозначным — это правка посева, общего для
    суит, не этого файла."""
    return [
        "if (pm.response.code === 200) {",
        *["  " + line for line in _assert_list_filtered_empty(field)],
        "} else {",
        *["  " + line for line in _assert_unauthenticated()],
        "}",
    ]


def _deny_leak_gated():
    # Аутентифицированный денай не должен раскрывать authz-причины наружу. На 401
    # (unauthenticated) тело несёт generic "subject unauthenticated" — это НЕ утечка
    # существования ресурса, поэтому проверку гейтим на код != 401.
    return [
        "if (pm.response.code !== 401) {",
        "  pm.test('authenticated deny -> no resource-existence leak', () => pm.expect(JSON.stringify(pm.response.json())).to.not.include('deny_reasons'));",
        "}",
    ]


def _list_never_reveals_regid():
    """Перечисление не должно называть чужой реестр — здесь запрет на эхо ВЕРЕН.

    Разница с чтением по id принципиальная и решает, какая проверка корректна.
    В перечислении вызывающий идентификатор фикстуры НЕ НАЗЫВАЛ: он спросил про
    проект. Значит появление regId в ответе — новое знание, добытое из запроса,
    в котором его не было, то есть утечка.

    В чтении по id всё наоборот: идентификатор пришёл ОТ вызывающего, в URL. Требовать
    его отсутствия в ответе значит требовать, чтобы скрытие отличалось от настоящего
    промаха, — см. `_hiding_is_indistinguishable`.
    """
    return [
        "const _rid = pm.environment.get('regIdAz') || '';",
        "if (_rid) pm.test('a listing never names a registry the caller did not ask about',"
        " () => pm.expect(pm.response.text()).to.not.include(_rid));",
    ]


def _hiding_is_indistinguishable():
    """Скрытие существования проверяется СРАВНЕНИЕМ с настоящим промахом, а не запретом
    на эхо идентификатора.

    Прежняя формулировка требовала, чтобы в теле отказа НЕ БЫЛО regId фикстуры. Это
    требование обратно контракту продукта и, если его выполнить, само создаёт оракул.
    Идентификатор в теле — тот самый, который вызывающий ПОСТАВИЛ В URL: он не сообщает
    ему ничего нового. А тон промаха — `<Resource> <id> not found` — общий для всей
    платформы, и край намеренно воспроизводит его ДОСЛОВНО (`notFoundMessage`,
    `hideExistenceNotFoundFormats`), чтобы «нет доступа» нельзя было отличить от «нет
    ресурса». Ответ БЕЗ идентификатора отличался бы от настоящего промаха — то есть
    сообщал бы, что реестр существует.

    Отличимость и есть предмет проверки. Кейс делает два одинаковых запроса одним и тем
    же чужаком: по СУЩЕСТВУЮЩЕМУ реестру и по well-formed ОТСУТСТВУЮЩЕМУ. Если ответы
    совпадают побайтово после подстановки идентификатора — оракула нет; любое
    расхождение (иной текст, иной код, иные details, нейтральное «not found» вместо
    контрактного тона) означает, что по ответу можно установить существование чужого
    ресурса, и кейс краснеет.
    """
    return [
        "const _rid = pm.environment.get('regIdAz') || '';",
        "const _missBody = pm.environment.get('_azHideMissBody');",
        "const _missCode = pm.environment.get('_azHideMissCode');",
        "pm.test('the absent-id probe recorded a comparison base', () => {",
        "  pm.expect(_rid, 'regIdAz is empty — nothing to compare').to.not.equal('');",
        "  pm.expect(_missBody, 'the well-formed-absent probe did not record a body').to.be.a('string');",
        "});",
        "if (_rid && typeof _missBody === 'string') {",
        "  const _seen = pm.response.text().split(_rid).join('<ID>');",
        "  pm.test('hiding an existing registry is byte-identical to a genuine miss (no existence oracle)',",
        "    () => pm.expect(_seen, 'existing: ' + pm.response.text() + ' | absent: ' + _missBody).to.equal(_missBody));",
        "  pm.test('and indistinguishable by status code too',",
        "    () => pm.expect(String(pm.response.code)).to.equal(_missCode));",
        "}",
        "if (pm.response.code === 404) {",
        "  pm.test('the refusal carries the platform not-found tone with the caller-supplied id',",
        "    () => pm.expect((pm.response.json() || {}).message).to.equal('Registry ' + _rid + ' not found'));",
        "}",
    ]


def _viewer_gate():
    # Viewer-tier кейсы проверяют границу «держит v_get, но НЕ v_update/v_delete» и
    # требуют для этого субъекта-наблюдателя. Субъект приходит из общего посева
    # фикстур (`tests/authz-fixtures/prodseed_matrix.py` заводит его и кладёт токен в
    # `jwtProjectViewerA`); в закоммиченные окружения подписанные токены не кладутся
    # НИКОГДА, поэтому переменная в них пуста by design и заполняется посевом.
    #
    # ОТСУТСТВИЕ ФИКСТУРЫ — ОТКАЗ, А НЕ ПРОПУСК. Здесь последовательно стояли две
    # неверные формы: сперва `pm.test('SKIPPED', () => pm.expect(true).to.eql(true))`
    # — утверждение, которое не может упасть; затем console-note + return вообще без
    # утверждения. Второе опиралось на то, что рядом, в prerequest-скрипте, стоит
    # harness-config-страж (gen.py `_auth_pre_script`), и он краснеет сам. Так и есть
    # — но это делает ЭТУ функцию ловушкой: скопированная в шаг без `auth=<envVar>`,
    # она даст настоящую тишину. Поэтому гейт краснеет СВОИМИ силами и говорит, чего
    # не хватает.
    #
    # Граница дополнительно покрыта всегда-исполняемым Go-тестом
    # internal/check/viewer_boundary_test.go — но покрытие в другом месте не является
    # основанием молчать здесь.
    return [
        "const _viewer = pm.environment.get('jwtProjectViewerA') || '';",
        "if (!_viewer) {",
        "  pm.test('FIXTURE REQUIRED: jwtProjectViewerA (viewer-tier boundary NOT verified)', () => "
        "pm.expect.fail('jwtProjectViewerA is empty: the viewer subject was never seeded, so this case "
        "exercised NO viewer at all. Seed it via tests/authz-fixtures (prodseed_matrix.py writes the "
        "token into the suite env). Absence of a fixture is a refusal, not a skip.'));",
        "  return;",
        "}",
    ]


# Фикстура: создать registry от project-editor → poll → capture {{regIdAz}}.
CASES.append(Case(
    id="REG-AZ-SETUP-FIXTURE",
    title="Setup: Create registry as project-editor → Operation → poll → capture regIdAz (prefix reg)",
    classes=["AZD"], priority="P1",
    steps=[
        Step(name="create-fixture", method="POST", path=REG,
             body={"name": "az-fixture-{{runId}}", "projectId": "{{existingProjectId}}",
                   "regionId": "{{existingRegionId}}", "description": "authz coverage fixture"},
             test_script=[*assert_status(200), *assert_operation_envelope(_OP_PREFIX),
                          *save_operation_id()]),
        poll_operation_until_done(),
        Step(name="capture-fixture-id", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "pm.test('setup op ok', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                          *save_from_response("(j.response&&j.response.id)||''", "regIdAz"),
                          "pm.test('fixture regId captured (prefix reg)', () => pm.expect((pm.environment.get('regIdAz')||'').startsWith('reg')).to.be.true);"]),
        # Read-your-writes warm-up (mirrors registry-repository.py `_create_registry`): force
        # the registry owner-tuple (as project-editor) to materialize — register-outbox →
        # drainer → IAM RegisterResource → FGA reconciler — BEFORE REPO-AZ-SETUP does a
        # CreateRepository under {{regIdAz}} (whose handler runs registryGate(v_create) on this
        # parent registry). Without it the first repo-create can 403/404 on the not-yet-visible
        # parent tuple. Bounded-retry over own fresh resource only (fail-open at budget).
        retry_until_authorized(
            Step(name="reg-warm", method="GET", path=f"{REG}/{{{{regIdAz}}}}",
                 test_script=[*assert_status(200)])),
    ],
))

# Get как jwtStranger на существующем regId. Отказ, и только отказ: у чтения ОДНОГО
# ресурса по id нет «пустого успеха» — 200 здесь означает, что чужак прочитал чужой
# реестр. Плюс главное: отказ на СУЩЕСТВУЮЩЕМ реестре обязан быть неотличим от отказа
# на отсутствующем — иначе по ответу устанавливается существование чужого ресурса.
_ABSENT_REG_ID = "reg-DOESNOTEXIST00000"  # well-formed, заведомо отсутствует (парити с REG-GET-NEG-NOTFOUND)

CASES.append(Case(
    id="REG-AZ-GET-STRANGER-HIDDEN",
    title="Get as jwtStranger: существующий regId и well-formed отсутствующий отвечают побайтово "
          "одинаково (модуло сам id) → denied (401/403/404, never 200), no deny_reasons (gated !=401)",
    classes=["AZD", "NEG"], priority="P0",
    steps=[
        # База сравнения: тот же чужак по заведомо отсутствующему id. Записывается ПЕРВЫМ,
        # чтобы следующий шаг сравнивал с ответом, полученным в этом же прогоне, а не с
        # литералом, который разъедется с продуктом молча.
        Step(name="get-stranger-absent", method="GET", path=f"{REG}/{_ABSENT_REG_ID}", auth="jwtStranger",
             test_script=[*_assert_denied(),
                          f"pm.environment.set('_azHideMissBody', pm.response.text().split('{_ABSENT_REG_ID}').join('<ID>'));",
                          "pm.environment.set('_azHideMissCode', String(pm.response.code));"]),
        Step(name="get-stranger", method="GET", path=f"{REG}/{{{{regIdAz}}}}", auth="jwtStranger",
             test_script=[*_assert_denied(), *_hiding_is_indistinguishable(), *_deny_leak_gated()]),
    ],
))

# Positive control: Get как jwtProjectViewerA → 200 (viewer имеет v_get).
# Fixture-gated: без зарегистрированного viewer-юзера — informational SKIP.
# Retry-on-404 поглощает grant-latency. Окно складывают кэш вердиктов registry
# (ручка KACHO_REGISTRY_AUTHZ_CACHE_TTL) и материализация project-tuple у владельца
# прав; величина здесь НЕ называется — её называет владелец, а бюджет ожидания виден
# на связывании ниже (cap 20 × 500 мс).
CASES.append(Case(
    id="REG-AZ-GET-VIEWER-OK",
    title="Get as jwtProjectViewerA on existing regId → 200 (viewer has v_get) — positive control (fixture-gated)",
    classes=["AZD"], priority="P1",
    steps=[Step(name="get-viewer", method="GET", path=f"{REG}/{{{{regIdAz}}}}", auth="jwtProjectViewerA",
                test_script=[
                    *_viewer_gate(),
                    "const _n = parseInt(pm.environment.get('_azViewerRetry') || '0', 10);",
                    "if (pm.response.code === 404 && _n < 20) {",
                    "  pm.environment.set('_azViewerRetry', String(_n + 1));",
                    "  const _ipd1 = Date.now(); while (Date.now() - _ipd1 < 500) void 0; /* real inter-poll delay: cap 20 x 500ms ~= 10s budget (testing.md) */",
                    "  pm.execution.setNextRequest(pm.info.requestName);",
                    "  return;",
                    "}",
                    "pm.environment.unset('_azViewerRetry');",
                    *assert_status(200),
                    "const j = pm.response.json();",
                    "pm.test('viewer sees fixture (v_get)', () => pm.expect(j.id).to.eql(pm.environment.get('regIdAz')));"])],
))

# List как jwtStranger для {{existingProjectId}}. Ровно два законных исхода, и различает
# их ПОСЕВ, а не продукт: dev-посев кладёт незарегистрированный `sub` → 401/16;
# prod-посев кладёт реального негрантнутого субъекта → 200 + ПУСТОЙ массив (call-gate'а
# у этого RPC нет, отбор построчный). Каждая ветка утверждает свою форму целиком, третий
# код не проходит ни одну. Существование фикстуры не раскрывается ни в одном ответе.
CASES.append(Case(
    id="REG-AZ-LIST-STRANGER-EMPTY",
    title="List as jwtStranger for existingProjectId → 200 + ПУСТОЙ registries (prod-посев) ЛИБО 401/16 (dev-посев); never reveals regId",
    classes=["AZD", "NEG"], priority="P0",
    steps=[Step(name="list-stranger", method="GET", path=f"{REG}?projectId={{{{existingProjectId}}}}", auth="jwtStranger",
                test_script=[
                    *_assert_stranger_list("registries"),
                    *_list_never_reveals_regid(),
                    *_deny_leak_gated()])],
))

# Update как jwtProjectViewerA (нет v_update) → 403/404 existence-hidden; без deny_reasons.
# Fixture-gated: без зарегистрированного viewer-юзера — informational SKIP.
CASES.append(Case(
    id="REG-AZ-UPDATE-VIEWER-DENY",
    title="Update as jwtProjectViewerA (no v_update) → 403/404 (existence-hidden); no deny_reasons (fixture-gated)",
    classes=["AZD", "NEG"], priority="P0",
    steps=[Step(name="update-viewer", method="PATCH", path=f"{REG}/{{{{regIdAz}}}}", auth="jwtProjectViewerA",
                body={"updateMask": "description", "description": "viewer-edit-{{runId}}"},
                test_script=[
                    *_viewer_gate(),
                    "pm.test('denied 403/404 (no v_update)', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
                    *_deny_leak_gated()])],
))

# Delete как jwtProjectViewerA (нет v_delete) → 403/404 existence-hidden; без deny_reasons.
# Fixture-gated: без зарегистрированного viewer-юзера — informational SKIP. Денай
# оставляет ресурс нетронутым → {{regIdAz}} валиден для cleanup.
CASES.append(Case(
    id="REG-AZ-DELETE-VIEWER-DENY",
    title="Delete as jwtProjectViewerA (no v_delete) → 403/404 (existence-hidden); no deny_reasons (fixture-gated)",
    classes=["AZD", "NEG"], priority="P0",
    steps=[Step(name="delete-viewer", method="DELETE", path=f"{REG}/{{{{regIdAz}}}}", auth="jwtProjectViewerA",
                test_script=[
                    *_viewer_gate(),
                    "pm.test('denied 403/404 (no v_delete)', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
                    *_deny_leak_gated()])],
))

# Create как jwtStranger в {{existingProjectId}}. Stranger здесь unauthenticated → 401;
# на многопользовательском стенде — 403 (visible project, no v_create) / 404 (hidden).
# ТОЛЬКО отказ: у создания нет «пустого успеха» — 200 означает, что чужак завёл реестр
# в ЧУЖОМ проекте, то есть ровно тот исход, ради ловли которого кейс существует.
CASES.append(Case(
    id="REG-AZ-CREATE-STRANGER-DENY",
    title="Create as jwtStranger in existingProjectId → denied (401/403/404, never 200); no deny_reasons (gated !=401)",
    classes=["AZD", "NEG"], priority="P0",
    steps=[Step(name="create-stranger", method="POST", path=REG, auth="jwtStranger",
                body={"name": "az-intruder-{{runId}}", "projectId": "{{existingProjectId}}",
                      "regionId": "{{existingRegionId}}"},
                test_script=[*_assert_denied(), *_deny_leak_gated()])],
))

# Update как jwtStranger на существующем regId. Stranger здесь unauthenticated → 401;
# на многопользовательском стенде — 403 (visible project, no v_update) / 404 (hidden).
# Мутация stranger'а НИКОГДА не 200-success (нет v_update); deny_reasons-leak не
# раскрываем (gated !=401 — 401-тело несёт generic-причину, не existence-oracle).
CASES.append(Case(
    id="REG-AZ-UPDATE-STRANGER-DENY",
    title="Update as jwtStranger on existing regId → denied (401/403/404, never 200 success); no deny_reasons (gated !=401)",
    classes=["AZD", "NEG"], priority="P0",
    steps=[Step(name="update-stranger", method="PATCH", path=f"{REG}/{{{{regIdAz}}}}", auth="jwtStranger",
                body={"updateMask": "description", "description": "x"},
                test_script=[
                    "pm.test('denied 401/403/404 (stranger, never 200 success)', () => pm.expect(pm.response.code).to.be.oneOf([401, 403, 404]));",
                    *_deny_leak_gated()])],
))

# Delete как jwtStranger на существующем regId. Тот же денай-диапазон [401/403/404],
# НИКОГДА 200-success (нет v_delete). Денай оставляет фикстуру нетронутой → {{regIdAz}}
# валиден для cleanup. deny_reasons-leak не раскрываем (gated !=401).
CASES.append(Case(
    id="REG-AZ-DELETE-STRANGER-DENY",
    title="Delete as jwtStranger on existing regId → denied (401/403/404, never 200 success); no deny_reasons (gated !=401)",
    classes=["AZD", "NEG"], priority="P0",
    steps=[Step(name="delete-stranger", method="DELETE", path=f"{REG}/{{{{regIdAz}}}}", auth="jwtStranger",
                test_script=[
                    "pm.test('denied 401/403/404 (stranger, never 200 success)', () => pm.expect(pm.response.code).to.be.oneOf([401, 403, 404]));",
                    *_deny_leak_gated()])],
))

# anonymous (без Authorization) Get на существующем regId → 401 AUTHN_REQUIRED.
CASES.append(Case(
    id="REG-AZ-GET-ANON-401",
    title="anonymous Get on existing regId → 401 (authN required, no bearer)",
    classes=["AZD", "NEG"], priority="P0",
    steps=[Step(name="get-anon", method="GET", path=f"{REG}/{{{{regIdAz}}}}", auth="anonymous",
                test_script=[*assert_status(401)])],
))

# ===========================================================================
# Per-repo authz на config-overlay Repository (RG-1) — existence-hiding на
# GetRepository/UpdateRepository/DeleteRepository/CreateRepository (per-repo v_*
# Check в handler'е; deny|absent → uniform NOT_FOUND, security.md). Переиспользует
# {{regIdAz}}; создаёт durable overlay-repo от editor'а, гоняет его от stranger/viewer.
# Тот же single-user-толерантный контракт, что registry-level authz выше (stranger →
# 401 на этом стенде; multi-user CI → 404/403). Registry-cascade сносит repo при
# удалении {{regIdAz}} (REG-AZ-CLEANUP-FIXTURE) → отдельного repo-cleanup не нужно.
# ===========================================================================

_AZ_REPO = REG + "/{{regIdAz}}/repositories"

# Setup: CreateRepository durable overlay repo под {{regIdAz}} от project-editor.
CASES.append(Case(
    id="REPO-AZ-SETUP",
    title="Setup: CreateRepository durable overlay repo under {{regIdAz}} (editor) → poll → capture",
    classes=["AZD"], priority="P1",
    steps=[
        # CreateRepository under {{regIdAz}} runs registryGate(v_create) on the parent
        # registry, whose owner-tuple is eventually-consistent after REG-AZ-SETUP-FIXTURE
        # (now warmed by reg-warm). Wrap the create in the same bounded read-your-writes
        # retry so a transient 403/404 on the not-yet-visible parent tuple is retried, not
        # asserted-once (create→dependent-create over the parent materialization window).
        retry_until_authorized(Step(name="repo-az-create", method="POST", path=_AZ_REPO,
             body={"repository": "az-repo-{{runId}}", "description": "per-repo authz fixture"},
             test_script=[*assert_status(200), *assert_operation_envelope(_OP_PREFIX),
                          *save_operation_id()])),
        poll_operation_until_done(),
        Step(name="repo-az-confirm", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "pm.test('repo setup op ok (no error)', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));"]),
        # Read-your-writes warm-up (mirrors `_create_repo` repo-warm): materialize the CREATOR's
        # per-repo owner-tuple (register-outbox → drainer → IAM → FGA) before downstream cases
        # read it — the sync-registrar (#102) is best-effort, so the creator's own first read
        # can briefly 404 until the tuple lands. This is the creator residual the task targets;
        # the separate viewer-grant EC (REPO-AZ-GET-VIEWER-OK) is intentionally left untouched.
        retry_until_authorized(
            Step(name="repo-az-warm", method="GET", path=_AZ_REPO + "/az-repo-{{runId}}",
                 test_script=[*assert_status(200)])),
    ],
))

# GetRepository как jwtStranger → existence-hidden. Stranger unregistered → 401 на
# single-user стенде; multi-user CI → 404 "repository not found". Принимаем denied-
# диапазон, но НИКОГДА 200-success (не раскрываем repo) и без deny_reasons-leak (!=401).
CASES.append(Case(
    id="REPO-AZ-GET-STRANGER-HIDDEN",
    title="GetRepository as jwtStranger → denied/hidden (401/403/404), never 200-success; no leak",
    classes=["AZD", "NEG"], priority="P0",
    steps=[Step(name="repo-get-stranger", method="GET", path=_AZ_REPO + "/az-repo-{{runId}}", auth="jwtStranger",
                test_script=[
                    "pm.test('denied (401/403/404), never 200 success', () => pm.expect(pm.response.code).to.be.oneOf([401, 403, 404]));",
                    *_deny_leak_gated()])],
))

# GetRepository как jwtProjectViewerA (v_get) → 200 (positive control). Fixture-gated;
# retry-on-404 поглощает grant-latency (кэш вердиктов registry — ручка
# KACHO_REGISTRY_AUTHZ_CACHE_TTL — плюс материализация у владельца прав; величина
# у владельцев, бюджет на связывании ниже).
CASES.append(Case(
    id="REPO-AZ-GET-VIEWER-OK",
    title="GetRepository as jwtProjectViewerA (v_get) → 200 (positive control, fixture-gated)",
    classes=["AZD"], priority="P1",
    steps=[Step(name="repo-get-viewer", method="GET", path=_AZ_REPO + "/az-repo-{{runId}}", auth="jwtProjectViewerA",
                test_script=[
                    *_viewer_gate(),
                    "const _n = parseInt(pm.environment.get('_azRepoViewerRetry') || '0', 10);",
                    "if (pm.response.code === 404 && _n < 20) {",
                    "  pm.environment.set('_azRepoViewerRetry', String(_n + 1));",
                    "  const _ipd2 = Date.now(); while (Date.now() - _ipd2 < 500) void 0; /* real inter-poll delay: cap 20 x 500ms ~= 10s budget (testing.md) */",
                    "  pm.execution.setNextRequest(pm.info.requestName);",
                    "  return;",
                    "}",
                    "pm.environment.unset('_azRepoViewerRetry');",
                    *assert_status(200),
                    "pm.test('viewer sees repo (v_get)', () => pm.expect(pm.response.json().name).to.eql('az-repo-'+pm.environment.get('runId')));"])],
))

# UpdateRepository как jwtProjectViewerA (v_get но НЕ v_update) → existence-hidden
# NOT_FOUND (403/404, НИКОГДА 200 op-envelope). Fixture-gated.
CASES.append(Case(
    id="REPO-AZ-UPDATE-VIEWER-DENY",
    title="UpdateRepository as jwtProjectViewerA (no v_update) → 403/404 (existence-hidden); no leak (fixture-gated)",
    classes=["AZD", "NEG"], priority="P0",
    steps=[Step(name="repo-update-viewer", method="PATCH", path=_AZ_REPO + "/az-repo-{{runId}}", auth="jwtProjectViewerA",
                body={"updateMask": "description", "description": "viewer-edit-{{runId}}"},
                test_script=[
                    *_viewer_gate(),
                    "pm.test('denied 403/404 (no v_update), never 200 op', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
                    *_deny_leak_gated()])],
))

# DeleteRepository как jwtStranger → denied, НИКОГДА 200 op-success (фикстура нетронута).
# Stranger unregistered → 401 здесь; multi-user CI → 404 existence-hidden. repo остаётся.
CASES.append(Case(
    id="REPO-AZ-DELETE-STRANGER-DENY",
    title="DeleteRepository as jwtStranger → denied (401/403/404), never 200 op-success; fixture untouched",
    classes=["AZD", "NEG"], priority="P0",
    steps=[Step(name="repo-delete-stranger", method="DELETE", path=_AZ_REPO + "/az-repo-{{runId}}", auth="jwtStranger",
                test_script=[
                    "pm.test('denied (401/403/404), never 200 op-success', () => pm.expect(pm.response.code).to.be.oneOf([401, 403, 404]));",
                    *_deny_leak_gated()])],
))

# CreateRepository как jwtStranger в {{regIdAz}} (реестр stranger'у невидим) → denied
# (namespace call-gate existence-hiding, X04). Anti-BOLA: чужой не сеет repo в твой реестр.
CASES.append(Case(
    id="REPO-AZ-CREATE-STRANGER-HIDDEN",
    title="CreateRepository as jwtStranger in {{regIdAz}} → denied (401/403/404), never 200 (namespace call-gate, X04)",
    classes=["AZD", "NEG"], priority="P1",
    steps=[Step(name="repo-create-stranger", method="POST", path=_AZ_REPO, auth="jwtStranger",
                body={"repository": "intruder/svc-{{runId}}"},
                test_script=[
                    "pm.test('denied (401/403/404), never 200', () => pm.expect(pm.response.code).to.be.oneOf([401, 403, 404]));",
                    *_deny_leak_gated()])],
))

# Registry hide-existence byte-identity (security.md #6): deny-404 текст на чужом/
# невидимом реестре обязан быть форматно байт-в-байт настоящему well-formed-absent miss
# (иначе existence-oracle / FGA-object-type-leak). Нормализуем id (deny несёт regIdAz,
# miss — absent-id) → сравниваем ФОРМАТ. На single-user стенде stranger → 401 (skip
# byte-identity); multi-user CI → 404. Locks security.md #6 без hardcode текста.
CASES.append(Case(
    id="REG-AZ-HIDE-EXISTENCE-BYTE-IDENTITY",
    title="Registry deny-404 format byte-identical to absent-miss 404 (security.md #6, no existence-oracle)",
    classes=["AZD", "NEG", "CONF"], priority="P1",
    steps=[
        Step(name="reg-miss-absent", method="GET", path=REG + "/reg00000000000000000",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.environment.set('regMissMsg', pm.response.json().message||'');"]),
        Step(name="reg-deny-stranger", method="GET", path=f"{REG}/{{{{regIdAz}}}}", auth="jwtStranger",
             test_script=[
                 # Сравнение форматов возможно ТОЛЬКО когда отказ пришёл как 404 —
                 # именно его байт-идентичность и проверяется. Но раньше при любом
                 # другом коде шаг молчал: ни одного утверждения на шести из семи
                 # возможных форм ответа, включая 200. То есть чужак, ПРОЧИТАВШИЙ
                 # чужой реестр, прошёл бы этот шаг незамеченным. Поэтому сначала —
                 # утверждение, исполняющееся ВСЕГДА: отказ, и только отказ.
                 "pm.test('stranger denied (401/403/404), never 200', () => "
                 "pm.expect(pm.response.code).to.be.oneOf([401, 403, 404]));",
                 "if (pm.response.code !== 404) {",
                 "  console.log('byte-identity comparison applies to the 404 shape only; this stand answered '+pm.response.code+' (authz-first ordering) — the deny/miss format pair is not comparable here');",
                 "  return;",
                 "}",
                 *assert_grpc_code(5, "NOT_FOUND"),
                 "const _absId = 'reg00000000000000000';",
                 "const _regIdAz = pm.environment.get('regIdAz') || '';",
                 "const _norm = (pm.response.json().message||'').split(_regIdAz).join(_absId);",
                 "pm.test('deny-404 format byte-identical to absent-miss (no existence-oracle / no FGA-type leak)', () => pm.expect(_norm).to.eql(pm.environment.get('regMissMsg')));"]),
    ],
))

# Cleanup: удалить фикстуру от project-editor ПОСЛЕДНИМ → poll → Get 404.
#
# Чтение ПОСЛЕ удаления — самая жёсткая проба скрытия существования во всей суите,
# и утверждать в ней один код нельзя. Владелец на настоящем промахе отвечает
# `Registry <id> not found`; всякий иной ответ — «нет доступа», «not found» без
# имени ресурса — отличим, а различимый ответ и есть оракул. Поэтому здесь
# сначала снимается ЭТАЛОН промаха (well-formed отсутствующий id, тот же
# вызывающий), а затем ответ на удалённый ресурс сверяется с ним ПОБАЙТОВО с
# точностью до подстановки id.
#
# Проба стала различающей после снятия внутренней конкуренции: пока отзыв прав не
# успевал доехать до подтверждающего чтения, шаг зеленел по неверной причине.
CASES.append(Case(
    id="REG-AZ-CLEANUP-FIXTURE",
    title="Cleanup: Delete fixture registry as project-editor (LAST) → Operation → poll → Get 404 byte-identical to a genuine miss",
    classes=["AZD", "CONF"], priority="P2",
    steps=[
        # Эталон снимается ДО удаления: тот же вызывающий, тот же ресурс-тип,
        # заведомо отсутствующий well-formed id.
        Step(name="capture-genuine-miss", method="GET", path=REG + "/reg00000000000000000",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "const _m = (pm.response.json().message)||'';",
                          "pm.test('genuine miss names the resource and echoes the id', () => "
                          "pm.expect(_m).to.eql('Registry reg00000000000000000 not found'));",
                          "pm.environment.set('regGenuineMissMsg', _m);"]),
        Step(name="delete-fixture", method="DELETE", path=f"{REG}/{{{{regIdAz}}}}",
             test_script=[*assert_status(200), *assert_operation_envelope(_OP_PREFIX),
                          *save_operation_id()]),
        poll_operation_until_done(),
        Step(name="confirm-deleted", method="GET", path=f"{REG}/{{{{regIdAz}}}}",
             test_script=["pm.test('cleanup op ok', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));",
                          *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          # ТЕКСТ, а не только код: 403 «permission denied» и голое
                          # «not found» отличимы от промаха владельца, и по этому
                          # различию отделяют «нет доступа» от «нет такого».
                          "const _abs = 'reg00000000000000000';",
                          "const _norm = ((pm.response.json().message)||'').split(pm.environment.get('regIdAz')).join(_abs);",
                          "pm.test('deleted-registry 404 is byte-identical to a genuine miss (no existence-oracle)', () => "
                          "pm.expect(_norm).to.eql(pm.environment.get('regGenuineMissMsg')));"]),
    ],
))
