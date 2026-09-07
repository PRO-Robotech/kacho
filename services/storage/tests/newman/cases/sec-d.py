# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для SEC-D (kacho-storage): owner-hierarchy tuple через kaname
(transactional-outbox) на всех трёх ресурсах, которые его эмитят.

storage не ходит в OpenFGA напрямую. На каждый Create/Delete Volume/Snapshot/Image
intent owner-tuple `project:<proj> #project @storage_<t>:<id>` пишется строкой в
`kacho_storage.fga_register_outbox` В ТОЙ ЖЕ writer-tx, что и вставка/удаление
ресурса; register-drainer применяет его через InternalIAMService.RegisterResource /
UnregisterResource. Из этого tuple iam-реконсайлер материализует per-object v_*
и containment target→project, на котором держится gateway scope_extractor
({storage_volume,volume_id} / {storage_snapshot,snapshot_id} / {storage_image,image_id}).
Публичный контракт ресурсов при этом не меняется — кейсы гоняют обычные public RPC.

ПОЧЕМУ auth=jwtAccountAdminA, а НЕ дефолтный jwtBootstrap (это не стилистика).
Дефолт набора — cluster-admin, а решение о доступе для cluster-admin принимается
каскадом ДО того, как что-либо спрашивается про сам объект. Кейс, прогнанный под
cluster-admin, вернул бы 200 и при полностью НЕ материализованном owner-tuple —
то есть был бы формой проверки без содержания: удаление compute-дубля прошло бы
«зелёным» ровно в том сценарии, ради которого этот файл и написан. jwtAccountAdminA —
владелец аккаунта storage-суиты и НЕ cluster-admin, поэтому обе линии, по которым
он может получить доступ к объекту (прямой per-object v_*, либо `super_admin from
project`), одинаково требуют, чтобы структурная связь объект→проект уже
существовала. 200 на Get под этой личностью — утверждение о материализации.

К поведенческому наблюдению добавлена ПРЯМАЯ проба tuple: internal
`POST /iam/v1/internal/iam:check` на cluster-internal REST-листенере
({{internalBaseUrl}}, :18081 — ban #6, на публичном :18080 маршрута нет). Она
отвечает на вопрос «есть ли отношение», а не «пустил ли gateway», поэтому
регрессия на стороне register-drainer'а видна даже если публичный путь случайно
пройдёт другой дорогой. Проба ОБЯЗАТЕЛЬНА: отсутствие {{internalBaseUrl}} — это
провал харнесса, а не повод тихо пропустить проверку.

Отдельно закрыт путь UNregister: после Delete отношение обязано ИСЧЕЗНУТЬ.
Односторонняя проверка (только «появилось») пропустила бы залипший grant на
удалённый ресурс — over-grant, который на публичном пути не виден вовсе.

Контракт изоляции: каждый case в своём runId, работает внутри pre-allocated
existingProjectId (_suiteProjectId из env); имена суффиксуются {{runId}}; свои
ресурсы кейс удаляет за собой. id-префиксы: Volume `vol`, Snapshot `snp`, Image `img`.

# requires: umbrella-стенд (kacho-storage + kaname + kacho-geo за gateway) и
# {{internalBaseUrl}} (инжектится deploy/scripts/newman-e2e.sh; в шаблоне env есть
# дефолт :18081). mTLS-mismatch и iam-peer-down — инфраструктурные негативы
# (dedicated chaos/TLS-профиль), здесь их нет намеренно.
"""

CASES = []

VOL = "/storage/v1/volumes"
SNP = "/storage/v1/snapshots"
IMG = "/storage/v1/images"

_OWNER = "jwtAccountAdminA"      # владелец аккаунта storage-суиты, НЕ cluster-admin
_PROBE = "jwtBootstrap"          # internal Check требует system_viewer-floor
_SUBJ = "user:{{userAAAId}}"     # субъект, под которым создаются ресурсы

_SRC_SIZE = 10737418240  # 10 GiB


def _internal_check_url():
    """Перевести шаг на cluster-internal REST-листенер api-gateway.

    gen.py эмитит {{baseUrl}}<path> (публичный :18080), где Internal*-маршрутов нет
    и быть не должно (ban #6) → без переадресации проба получила бы 404-страницу и
    упала бы на первом pm.response.json() как «JSONError», маскируя реальный предмет
    кейса. Пустой {{internalBaseUrl}} — ошибка конфигурации харнесса, и она обязана
    быть громкой: молчаливый пропуск здесь означал бы, что вся прямая проба tuple
    исчезла, а набор остался зелёным.

    ФОРМА — САНКЦИОНИРОВАННАЯ: утвердить (назвав переменную), затем ПРОПУСТИТЬ
    (`gen.py::require_env_url`). Прежняя редакция утверждала — и всё равно
    ОТПРАВЛЯЛА шаг: без переадресации он уезжал на публичный листенер, где
    Internal*-маршрута нет, и остальные утверждения шага оценивались по ответу,
    к предмету кейса отношения не имеющему. `pm.execution.skipRequest()`
    пропускает ровно один запрос (его test-script тоже не исполняется), поэтому
    ни одно утверждение шага не засчитывается; утверждение выше уже отработало,
    значит пропуск остаётся ЗАПИСАННЫМ падением с именем переменной, а не немым.
    """
    return [
        "const intBase = pm.environment.get('internalBaseUrl') || pm.variables.get('internalBaseUrl') || '';",
        "if (intBase) {",
        "  pm.request.url = intBase + '/iam/v1/internal/iam:check';",
        "} else {",
        "  pm.test('harness config: internalBaseUrl is set (internal Check probe target)', () => {",
        "    pm.expect.fail('internalBaseUrl is empty — the internal iam:check probe has no target; "
        "inject it (deploy/scripts/newman-e2e.sh) instead of running without the probe');",
        "  });",
        "  pm.execution.skipRequest();",
        "}",
    ]


def _check_step(name, obj_type, id_var, expect_allowed, budget=90, interval_ms=500):
    """Прямая проба отношения `v_get` субъекта-создателя на объекте ресурса.

    expect_allowed=True  — owner-tuple применён (Create-путь register).
    expect_allowed=False — tuple снят (Delete-путь unregister).

    Обе стороны eventually-consistent (outbox → drainer → реконсайлер), поэтому
    петля bounded-retry с РЕАЛЬНОЙ паузой между попытками: newman исполняет
    test-script синхронно и вызывает setNextRequest ДО любого setTimeout, поэтому
    busy-wait — единственный способ действительно разнести пробы. Без него
    `budget` итераций укладываются в доли секунды и петля «сдаётся», не подождав
    ни разу. По исчерпании бюджета — обычный assert (fail-open по времени,
    fail-closed по вердикту): проверка падает, а не тихо проходит.

    БЮДЖЕТ РАЗМЕРЕН ПО СОБСТВЕННОМУ СРОКУ ПОЧИНКИ ПЛАТФОРМЫ, А НЕ НА ГЛАЗОК.
    Прежние 20×500 мс = 10 с были КОРОЧЕ, чем период выметающего прохода
    реконсайлера iam (KANAME_RECONCILE_SWEEP_INTERVAL_MS, умолчание 30 с), то
    есть проба требовала снятия быстрее, чем система обязуется чинить состояние.
    Утверждать так значит утверждать необъявленный срок, а не свойство.

    Померено на боевой посадке 2026-07-31 (журнал изменений OpenFGA, отметки
    времени самой СУБД): у тома `v_get` владельца снялся за 294 мс, у снимка — за
    11.2 с, у образа — за 10.7 с. Асимметрия не в коде storage (выдача намерения
    во всех трёх — 3-5 мс, один и тот же путь в одной транзакции с удалением
    строки), а в объёме материализации: посев держит в проекте суиты столько
    субъектов с выдачами, что на КАЖДЫЙ ресурс пишется 43 tuple, и партия занимает
    те же ~10 с. Снятие идёт следом за ней. Проверено, что снятие ДОШЛО: спустя
    часы ни строки зеркала, ни одного tuple по всем трём объектам не осталось.

    Утверждение при этом не ослаблено ни на йоту: исход по-прежнему один
    («стало запрещено»), альтернативной ветки нет, тихого пропуска нет. Бюджет
    расходуется ТОЛЬКО когда условие ещё не выполнено, поэтому на зелёном прогоне
    он не стоит ничего. Проба остаётся различающей — сосед-том сходится за
    сотни миллисекунд, а невыполненное снятие не сойдётся никогда (реконсайлер
    level-triggered: пока строка зеркала жива, он ре-материализует доступ вечно).

    Ответ deny НЕ содержит поля `allowed` (только `reason`), поэтому вердикт
    формулируется как `allowed !== true`, а не `allowed === false`.
    """
    want = "true" if expect_allowed else "false"
    # ИМЯ переменной прогона больше НЕ выводится из ПРОЗЫ (#1220). Прежде ключ
    # собирался из подписи шага — `_ck_{name.replace('-','_')}`, — и это давало
    # два тихих вреда сразу: подпись с апострофом рвала СИНТАКСИС коллекции (а
    # newman пишет отказ разбора в testScripts, не в assertions.failed, поэтому
    # шаг отчитывался НУЛЁМ упавших утверждений), а отображение `-`→`_` было
    # неоднозначным — `tuple-present-vol` и `tuple_present_vol` сходились в ОДИН
    # счётчик, и две пробы молча делили один бюджет повторов. Экранировать тут
    # нечего: имя либо годно, либо файл не разбирается. Поэтому исход — снятие
    # подстановки: ключ выводится из `id_var`, который именем УЖЕ является по
    # контракту шва (его же читает `pm.environment.get` двумя строками ниже), а
    # годность проверяется при генерации и называет место.
    counter = ("_ck_" + js_name(id_var, where="storage/sec-d/_check_step/id_var")
               + ("_present" if expect_allowed else "_withdrawn"))
    return Step(
        name=name, method="POST", path="/iam/v1/internal/iam:check", auth=_PROBE,
        body={"subjectId": _SUBJ, "relation": "v_get",
              "object": f"{obj_type}:{{{{{id_var}}}}}"},
        pre_script=_internal_check_url(),
        test_script=[
            "let j; try { j = pm.response.json(); } catch (e) { j = null; }",
            f"const settled = (pm.response.code === 200) && ((j && j.allowed === true) === {want});",
            f"if (pm.environment.get('{counter}_started') !== pm.info.requestName) "
            f"{{ pm.environment.set('{counter}', '0'); pm.environment.set('{counter}_started', pm.info.requestName); }}",
            f"const n = parseInt(pm.environment.get('{counter}') || '0', 10);",
            f"if (!settled && n < {budget}) {{",
            f"  pm.environment.set('{counter}', String(n + 1));",
            f"  const _w = Date.now(); while (Date.now() - _w < {interval_ms}) {{ /* materialization wait */ }}",
            "  pm.execution.setNextRequest(pm.info.requestName);",
            "  return;",
            "}",
            f"pm.environment.unset('{counter}'); pm.environment.unset('{counter}_started');",
            f"pm.test('{name}: Check reachable (200)', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
            (f"pm.test('{name}: owner-tuple materialized (v_get allowed)', "
             f"() => pm.expect(j && j.allowed, JSON.stringify(j)).to.eql(true));"
             if expect_allowed else
             f"pm.test('{name}: owner-tuple withdrawn (v_get no longer allowed)', "
             f"() => pm.expect(j && j.allowed, JSON.stringify(j)).to.not.eql(true));"),
        ])


def _lifecycle_case(case_id, title, base_path, obj_type, id_var, id_prefix,
                    create_body, pre_steps=(), post_cleanup=()):
    """Полный SEC-D-цикл одного ресурса: Create → op done+success → Get владельцем
    (не cluster-admin'ом) → прямая проба tuple → Delete → Get 404 → проба снятия."""
    # Между косыми чертами стоит КОД, а не текст: знаки образца значимы. Образец
    # собирается здесь ЦЕЛИКОМ и целиком же проверяется при генерации
    # (`js_regex_src`) — негодный префикс роняет генерацию с именем места, а не
    # рвёт СИНТАКСИС коллекции там, где автор значения этого не увидит (#1209).
    id_pattern = f"^{id_prefix}"
    return Case(
        id=case_id, title=title,
        classes=["SECD", "CONF", "IDM", "AUTHZ"], priority="P0",
        steps=[
            *pre_steps,
            Step(name="create", method="POST", path=base_path, body=create_body, auth=_OWNER,
                 test_script=[*assert_status(200), *assert_operation_envelope(),
                              *save_from_response("j.id", "opId"),
                              *save_from_response(f"j.metadata && j.metadata.{id_var}", id_var)]),
            poll_operation_until_done(auth=_OWNER),
            # op.error проверяется ДО того, как id из metadata пойдёт дальше: metadata
            # несёт pre-allocated id даже у операции, завершившейся ошибкой, поэтому без
            # этого шага все последующие пробы шли бы против фантома.
            assert_op_success(auth=_OWNER),
            retry_until_authorized(
                Step(name="get-after-create", method="GET", path=f"{base_path}/{{{{{id_var}}}}}",
                     auth=_OWNER,
                     test_script=[*assert_status(200),
                                  "const j = pm.response.json();",
                                  f"pm.test('id matches & {id_prefix} prefix', () => {{ "
                                  f"pm.expect(j.id).to.eql(pm.environment.get('{id_var}')); "
                                  f"pm.expect(j.id).to.match(/{js_regex_src(id_pattern, where='storage/_lifecycle_case/id_prefix')}/); }});",
                                  "pm.test('projectId matches the suite project', "
                                  "() => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteProjectId')));",
                                  *assert_created_at_seconds()])),
            _check_step(f"tuple-present-{id_prefix}", obj_type, id_var, expect_allowed=True),
            Step(name="delete", method="DELETE", path=f"{base_path}/{{{{{id_var}}}}}", auth=_OWNER,
                 test_script=[*assert_status(200), *assert_operation_envelope(),
                              *save_from_response("j.id", "opId")]),
            poll_operation_until_done(auth=_OWNER),
            assert_op_success(auth=_OWNER),
            # HTTP 404 — транспорт; grpc-код в теле = 5 (NOT_FOUND). Не путать.
            Step(name="get-after-delete", method="GET", path=f"{base_path}/{{{{{id_var}}}}}",
                 auth=_OWNER,
                 test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
            _check_step(f"tuple-withdrawn-{id_prefix}", obj_type, id_var, expect_allowed=False),
            *post_cleanup,
        ],
    )


def _delete_source(name, base_path, id_var):
    """Удалить вспомогательный источник (том/снимок), созданный ради ресурса кейса."""
    return [
        Step(name=name, method="DELETE", path=f"{base_path}/{{{{{id_var}}}}}", auth=_OWNER,
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth=_OWNER),
    ]


def _source_volume(suffix, out_var="secdSrcVolumeId"):
    """Том-источник снимка/образа, ДОВЕДЁННЫЙ до пригодности.

    Снимок снимается только с готового тома, захват образа берёт только готовый
    источник, а `Operation.done` означает лишь «строка закоммичена»: готовность
    объявляет сверщик, увидев объект у плоскости данных. Без ожидания фикстура
    этих кейсов не собиралась бы, и предметом падения оказался бы не owner-tuple,
    ради которого они написаны.
    """
    return [
        Step(name=f"pre-vol-{suffix}", method="POST", path=VOL, auth=_OWNER,
             body={"projectId": "{{_suiteProjectId}}", "name": f"secd-src-{suffix}-{{{{runId}}}}",
                   "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
                   "sizeBytes": _SRC_SIZE},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", out_var)]),
        poll_operation_until_done(auth=_OWNER),
        assert_op_success(auth=_OWNER),
        wait_until_ready_step(f"pre-vol-{suffix}-ready", f"{VOL}/{{{{{out_var}}}}}",
                              ready="AVAILABLE", subject="Том-источник", auth=_OWNER),
    ]


# ---------------------------------------------------------------------------
# Volume — register/unregister owner-tuple на storage_volume.
# ---------------------------------------------------------------------------

CASES.append(_lifecycle_case(
    case_id="SECD-VOL-CR-GET-AFTER-TUPLE-OK",
    title="SEC-D: Create volume → op done → владелец (не cluster-admin) читает свой том, "
          "tuple на storage_volume материализован → Delete → Get 404 и tuple снят",
    # index: SECD-VOL-CR-GET-AFTER-TUPLE-OK
    base_path=VOL, obj_type="storage_volume", id_var="volumeId", id_prefix="vol",
    create_body={"projectId": "{{_suiteProjectId}}", "name": "secd-vol-{{runId}}",
                 "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
                 "sizeBytes": _SRC_SIZE, "labels": {"suite": "sec-d"}},
))


# ---------------------------------------------------------------------------
# Snapshot — register/unregister owner-tuple на storage_snapshot.
# ---------------------------------------------------------------------------

CASES.append(_lifecycle_case(
    case_id="SECD-SNP-CR-GET-AFTER-TUPLE-OK",
    title="SEC-D: Create snapshot → op done → владелец читает свой снимок, "
          "tuple на storage_snapshot материализован → Delete → Get 404 и tuple снят",
    # index: SECD-SNP-CR-GET-AFTER-TUPLE-OK
    base_path=SNP, obj_type="storage_snapshot", id_var="snapshotId", id_prefix="snp",
    create_body={"projectId": "{{_suiteProjectId}}", "sourceVolumeId": "{{secdSrcVolumeId}}",
                 "name": "secd-snap-{{runId}}", "labels": {"suite": "sec-d"}},
    pre_steps=_source_volume("snap"),
    post_cleanup=_delete_source("cleanup-src-vol", VOL, "secdSrcVolumeId"),
))


# ---------------------------------------------------------------------------
# Image — register/unregister owner-tuple на storage_image. Образ регионален
# (anycast), поэтому несёт regionId, а не zoneId.
# ---------------------------------------------------------------------------

CASES.append(_lifecycle_case(
    case_id="SECD-IMG-CR-GET-AFTER-TUPLE-OK",
    title="SEC-D: Create image → op done → владелец читает свой образ, "
          "tuple на storage_image материализован → Delete → Get 404 и tuple снят",
    # index: SECD-IMG-CR-GET-AFTER-TUPLE-OK
    base_path=IMG, obj_type="storage_image", id_var="imageId", id_prefix="img",
    create_body={"projectId": "{{_suiteProjectId}}", "regionId": "{{existingRegionId}}",
                 "sourceVolumeId": "{{secdImgSrcVolumeId}}", "name": "secd-img-{{runId}}",
                 "labels": {"suite": "sec-d"}},
    pre_steps=_source_volume("img", out_var="secdImgSrcVolumeId"),
    post_cleanup=_delete_source("cleanup-img-src-vol", VOL, "secdImgSrcVolumeId"),
))


# ---------------------------------------------------------------------------
# Негатив: Delete отсутствующего ресурса не заводит Operation, а значит не может
# оставить осиротевший unregister-intent.
#
# Отказ приходит РАНЬШЕ бэкенда. Резолв цели в проект идёт через ту же
# структурную связь, которую регистрирует SEC-D; у несуществующего id её нет,
# поэтому gateway отвечает fail-closed отказом в правах ещё до вызова storage.
# Для настоящего предмета кейса это усиление, а не ослабление: до сервиса запрос
# вообще не доходит, значит записать unregister-intent физически некому.
#
# Ожидание здесь — контракт ОТКАЗА storage (403 / code 7 / `permission denied`,
# существование цели не раскрывается; см. шапку cases/authz.py), а НЕ 404. Оно
# детерминировано именно потому, что кейс идёт под обычным владельцем аккаунта:
# под cluster-admin запрос проскочил бы каскадом до бэкенда и вернул 404 — то есть
# «404-ожидание» держалось бы исключительно на супер-доступе прогонщика.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SECD-DEL-NEG-NOT-FOUND",
    title="SEC-D: Delete отсутствующего volume обычным владельцем → отказ на gateway "
          "(403 / code 7 / permission denied) ДО бэкенда: Operation не заводится, "
          "осиротевший unregister-intent записать некому",
    classes=["SECD", "NEG", "AUTHZ"], priority="P2",
    # index: SECD-DEL-NEG-NOT-FOUND
    steps=[
        Step(name="delete-missing", method="DELETE", path=f"{VOL}/{{{{garbageStorageId}}}}",
             auth=_OWNER,
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED"),
                          "pm.test('message says permission denied (existence not revealed)', "
                          "() => pm.expect((pm.response.json().message || '').toLowerCase()).to.include('permission denied'));"]),
    ],
))
