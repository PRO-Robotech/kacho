# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Concurrency burst-кейсы для kacho-vpc (класс `CONC`).

Newman не делает deterministic race-condition (1000+ горутин) — это integration-территория
(`internal/repo/*_integration_test.go` уже покрыты testcontainers race-сценарии).
Тут — **best-effort burst** через `pm.sendRequest` + counter: N HTTP-запросов уходят
почти-одновременно с одного newman-runner'а, server обрабатывает в реальной race-window.

Что верифицируется:
- `SUB-CR-CONC-OVERLAP-BURST` — EXCLUDE constraint `subnets_no_overlap_v4` race-defense
- `ADR-CR-CONC-BURST-ALLOC` — IPAM allocator выдает уникальные IP под burst
- `NIC-ATTACH-CONC-BURST` — atomic CAS `network_interfaces.used_by_id`
- `NIC-CR-CONC-MAC-UNIQUE` — UNIQUE constraint `network_interfaces_mac_address_key`
  под burst (`crypto/rand`-MAC + retry on collision)
- `SG-URL-CONC-OCC-CONFLICT` — `xmin`-based OCC в SecurityGroup.UpdateRules

Все эти инварианты держатся на DB-уровне (FK/UNIQUE/EXCLUDE/CAS/xmin); software-side
TOCTOU здесь запрещен (race-prone).
"""

CASES = []


# ---------------------------------------------------------------------------
# Burst helper — pm.sendRequest × N с counter и Promise-style waitForAll.
#
# Postman runtime sandbox: `pm.sendRequest` принимает callback (err, res).
# Newman ждет пока все pending callbacks отстреляют до завершения test_script.
# Прозрачно для нас: запускаем N sends в for-loop, накапливаем results в массив,
# в конце (when counter==N) делаем asserts.
# ---------------------------------------------------------------------------

def _burst_block_js(n: int, method: str, path: str, body_js: str, results_var: str = "burstResults"):
    """Возвращает строки JS для test_script: N parallel sends, накапливает [{code,body}]
    в pm.environment[results_var]. body_js — JS-выражение, формирующее body per-iteration
    (имеет переменную `i` для индекса)."""
    return [
        f"const N = {n};",
        "const base = pm.environment.get('baseUrl');",
        "const tok = pm.environment.get('jwtProjectAdminA1');",
        "const results = [];",
        "let done = 0;",
        "for (let i = 0; i < N; i++) {",
        f"  const body = {body_js};",
        "  pm.sendRequest({",
        f"    url: base + `{path}`,",
        f"    method: '{method}',",
        "    header: {",
        "      'Authorization': 'Bearer ' + tok,",
        "      'Content-Type': 'application/json',",
        "    },",
        "    body: { mode: 'raw', raw: JSON.stringify(body) },",
        "  }, (err, res) => {",
        "    let parsed = null;",
        "    try { parsed = res ? res.json() : null; } catch (e) {}",
        "    results.push({ code: res ? res.code : 0, body: parsed, err: err ? String(err) : null });",
        "    done++;",
        "    if (done === N) {",
        f"      pm.environment.set('{results_var}', JSON.stringify(results));",
        "    }",
        "  });",
        "}",
    ]


def _poll_op_js(op_id_var: str, result_var: str, max_tries: int = 8):
    """Polling одной Operation по env-переменной, результат в `result_var` env:
    {done: bool, error: {code,message}|null, response: any}."""
    return [
        f"const _opId = pm.environment.get('{op_id_var}');",
        "if (!_opId) {",
        f"  pm.environment.set('{result_var}', JSON.stringify({{done:false,error:null,response:null}}));",
        "} else {",
        "  let _tries = 0;",
        f"  const _MAX = {max_tries};",
        "  const _step = () => {",
        "    pm.sendRequest({",
        "      url: pm.environment.get('baseUrl') + '/operations/' + _opId,",
        "      method: 'GET',",
        "      header: { 'Authorization': 'Bearer ' + pm.environment.get('jwtProjectAdminA1') },",
        "    }, (err, res) => {",
        "      let j = null; try { j = res.json(); } catch (e) {}",
        "      if (j && j.done) {",
        f"        pm.environment.set('{result_var}', JSON.stringify({{done:true,error:j.error||null,response:j.response||null}}));",
        "      } else if (++_tries < _MAX) {",
        "        setTimeout(_step, 500);",
        "      } else {",
        f"        pm.environment.set('{result_var}', JSON.stringify({{done:false,error:null,response:null}}));",
        "      }",
        "    });",
        "  };",
        "  _step();",
        "}",
    ]


# ---------------------------------------------------------------------------
# УБОРКА ПОДЗАПРОСАМИ: СОСТАВ ЗАКОННЫХ ИСХОДОВ НАЗВАН, А НЕ ПОДОБРАН
# ---------------------------------------------------------------------------
# Состязательный кейс убирает за собой не отдельными шагами, а залпом из
# `pm.sendRequest`: идентификаторы известны только в рантайме (их вернул список
# либо резолв операций). Шаг при этом ходит на `/healthz`, поэтому НИ ОДНА
# проверка, судящая шаг по его СОБСТВЕННОМУ ответу, его исход не видит —
# ни гейт дерева `TestCapturedVariableStepCarriesAnAssertion`, ни обёртка окна
# видимости (`retry_until_authorized` решает по коду ответа ШАГА), ни проход,
# дописывающий утверждение шагу удаления (`_assert_delete_operation_outcome`
# смотрит на метод шага, а он здесь GET).
#
# Значит исход обязан назвать сам шаг. Ниже — состав, который считается законным,
# и довод по каждой строке. Состав УЖЕ, чем у `assert_cleanup_delete`, и это не
# придирка: там предмет уборки мог не существовать вовсе (кейс про поведение
# Create), а здесь его существование УТВЕРЖДЕНО предыдущим шагом — списком
# подсетей, «все 5 адресов созданы», «10 интерфейсов резолвнуто».
#
# СИНХРОННЫЙ ОТВЕТ УДАЛЕНИЯ
#
#   200 + непустой `id`  — удаление ПРИНЯТО, чеканена Operation. Единственный
#                          исход, после которого имеет смысл читать операцию.
#   403                  — окно материализации owner-tuple: право `v_delete` на
#                          свежесозданный объект ещё не видно. ПЕРЕХОДНОЕ
#                          состояние, а не исход: пережидается повтором в
#                          пределах бюджета (25×500 мс ≈ 12.5 с — тот же бюджет,
#                          что у `retry_until_authorized`). Терминальный 403 —
#                          КРАСНОЕ: предмет остался жить.
#   404                  — две причины неотличимы by construction: то же окно,
#                          поданное как hide-existence (`security.md` §6 требует
#                          от отказа байт-идентичности настоящему «не найдено»),
#                          и «ресурса уже нет». Поэтому 404 тоже пережидается, а
#                          терминальный — КРАСНОЕ: предмет создан ЭТИМ кейсом, и
#                          исчезнуть сам он не мог.
#   400 (любой код), 409, 5xx, ответа нет — КРАСНОЕ. Отдельно про 400 с кодом 9
#                          («ресурс занят»): у этих кейсов подсети пусты, адреса
#                          ни к чему не привязаны, интерфейсы не приаттачены —
#                          то есть «занят» здесь означает ровно ту утечку, ради
#                          которой шаг существует. Принять его значило бы принять
#                          дефект.
#
# ИСХОД ОПЕРАЦИИ УБОРКИ
#
#   done=true без `error` — успех.
#   done=true с `error`   — КРАСНОЕ: воркер отказался убирать.
#   не done за бюджет     — КРАСНОЕ. «Неизвестный исход — не то же самое, что
#                           успешный» (дословно из `poll_operation_until_done`).
#                           Бюджет 30×500 мс ≈ 15 с — покрытие асинхронного
#                           хвоста, то же, что у общего опроса.
#   ответ опроса не 200   — пережидается тем же бюджетом, терминальный — КРАСНОЕ.
#
# ЧЕГО В СОСТАВЕ НЕТ. Толерантного `oneOf`, принимающего и приём, и отказ:
# он не читает НИ ОДНУ полосу и потому принял бы и залипший отказ в правах, и
# смену контракта удаления. Повтора всей пробы. И молчания при нулевом числе
# предметов: ноль предметов законен только там, где предыдущий шаг его допускает
# (список подсетей — `at.most(1)`), и он всё равно назван числом в следующем шаге.
#
# ПОЧЕМУ УТВЕРЖДЕНИЕ СТОИТ ВНУТРИ ОБРАБОТЧИКА. Это форма, уже принятая деревом
# (`cases/operation.py` `poll-and-verify-shape`, `cases/internal-pool.py`
# `verify-1..3`, `cases/subnet.py` `poll-fail-v6`): newman дожидается
# незавершённых обработчиков до конца шага, и упавшее там утверждение попадает в
# `run.stats.assertions` наравне с синхронным. Падение при этом называет
# КОНКРЕТНЫЙ идентификатор, а не «уборка не удалась».
#
# ПОЧЕМУ ЭТОГО МАЛО И РЯДОМ СТОИТ СИНХРОННОЕ УТВЕРЖДЕНИЕ. Обработчик, который не
# отстрелил вовсе (запрос завис, ответа нет), утверждения не исполнит, и шаг
# зеленел бы «по отсутствию проверок» — третья категория исхода, зачтённая в
# «прошло». Поэтому уборка публикует НАКОПИТЕЛЬ, а следующий шаг СИНХРОННО
# требует, чтобы в нём было столько же записей, сколько было предметов.


def _cleanup_burst_js(ids_var: str, path_prefix: str, what: str,
                      ops_var: str, outcomes_var: str,
                      budget: int = 25, interval_ms: int = 500):
    """Залп DELETE по идентификаторам из `ids_var`, с утверждением исхода КАЖДОГО.

    Публикует `ops_var` (идентификаторы принятых Operation) и `outcomes_var`
    (по записи на предмет) — их читает и сверяет по числу шаг ожидания.
    """
    w = js_str(what)
    return [
        f"const _cuIds = JSON.parse(pm.environment.get({js_str(ids_var)}) || '[]');",
        "const _cuBase = pm.environment.get('baseUrl');",
        "const _cuTok = pm.environment.get('jwtProjectAdminA1');",
        "const _cuOps = [];",
        "const _cuOutcomes = [];",
        "const _cuTotal = _cuIds.length;",
        "let _cuPending = _cuTotal;",
        # Синхронное утверждение: исполняется ВСЕГДА, поэтому шаг не может
        # оказаться зелёным «по отсутствию проверок» — в том числе когда
        # обработчики не отстрелили ни разу.
        f"pm.test('уборка (' + {w} + '): перечень предметов прочитан (' + _cuTotal + ')', () => "
        f"  pm.expect(_cuIds, pm.environment.get({js_str(ids_var)}) || '').to.be.an('array'));",
        "const _cuPublish = () => {",
        f"  pm.environment.set({js_str(ops_var)}, JSON.stringify(_cuOps));",
        f"  pm.environment.set({js_str(outcomes_var)}, JSON.stringify(_cuOutcomes));",
        "};",
        "if (_cuTotal === 0) { _cuPublish(); }",
        "const _cuOne = (id, attempt) => {",
        "  pm.sendRequest({",
        f"    url: _cuBase + {js_str(path_prefix)} + '/' + id,",
        "    method: 'DELETE',",
        "    header: { 'Authorization': 'Bearer ' + _cuTok },",
        "  }, (err, res) => {",
        "    const code = res ? res.code : 0;",
        # Окно материализации owner-tuple: 403 у мутации, 404 у скрытого
        # существования. ПЕРЕХОДНОЕ состояние — пережидается; терминальное
        # роняет утверждение ниже (fail-closed, никогда не бесконечно).
        f"    if ((code === 403 || code === 404) && attempt < {budget}) {{",
        f"      setTimeout(() => _cuOne(id, attempt + 1), {interval_ms});",
        "      return;",
        "    }",
        "    let body = null; try { body = res ? res.json() : null; } catch (e) {}",
        "    const seen = JSON.stringify({code: code, body: body, "
        "      err: err ? String(err) : null, attempts: attempt + 1});",
        f"    pm.test('уборка (' + {w} + ') ' + id + ': принято — 200 и конверт Operation', () => {{",
        "      pm.expect(code, seen).to.eql(200);",
        "      pm.expect(body && body.id, seen).to.be.a('string').and.to.have.length.above(0);",
        "    });",
        "    if (body && body.id) _cuOps.push(body.id);",
        "    _cuOutcomes.push({id: id, code: code});",
        "    if (--_cuPending === 0) {",
        "      _cuPublish();",
        f"      pm.test('уборка (' + {w} + '): ответ получен по всем ' + _cuTotal + ' предметам', () => "
        "        pm.expect(_cuOutcomes.length, JSON.stringify(_cuOutcomes)).to.eql(_cuTotal));",
        "    }",
        "  });",
        "};",
        "_cuIds.forEach(id => _cuOne(id, 0));",
    ]


def _await_cleanup_ops_js(ids_var: str, ops_var: str, outcomes_var: str, what: str,
                          budget: int = 30, interval_ms: int = 500):
    """Ожидание операций уборки с утверждением ИСХОДА каждой.

    Первое утверждение СИНХРОННОЕ и закрывает случай «обработчик предыдущего шага
    не отстрелил»: накопителей столько же, сколько было предметов.
    """
    w = js_str(what)
    return [
        f"const _awIds = JSON.parse(pm.environment.get({js_str(ids_var)}) || '[]');",
        f"const _awOutcomes = JSON.parse(pm.environment.get({js_str(outcomes_var)}) || 'null');",
        f"const _awOps = JSON.parse(pm.environment.get({js_str(ops_var)}) || 'null');",
        f"pm.test('уборка (' + {w} + '): предыдущий шаг отчитался по всем ' + _awIds.length + "
        "  ' предметам', () => {",
        "  pm.expect(_awOutcomes, 'накопитель исходов уборки').to.be.an('array');",
        "  pm.expect(_awOutcomes.length, JSON.stringify(_awOutcomes)).to.eql(_awIds.length);",
        "  pm.expect(_awOps, 'накопитель операций уборки').to.be.an('array');",
        "  pm.expect(_awOps.length, JSON.stringify(_awOps)).to.eql(_awIds.length);",
        "});",
        "const _awBase = pm.environment.get('baseUrl');",
        "const _awTok = pm.environment.get('jwtProjectAdminA1');",
        "const _awList = Array.isArray(_awOps) ? _awOps : [];",
        "const _awOne = (oid, attempt) => {",
        "  pm.sendRequest({",
        "    url: _awBase + '/operations/' + oid,",
        "    method: 'GET',",
        "    header: { 'Authorization': 'Bearer ' + _awTok },",
        "  }, (err, res) => {",
        "    const code = res ? res.code : 0;",
        "    let j = null; try { j = res ? res.json() : null; } catch (e) {}",
        "    if (code === 200 && j && j.done) {",
        f"      pm.test('операция уборки (' + {w} + ') ' + oid + ': завершилась БЕЗ ошибки', () => "
        "        pm.expect(j.error && JSON.stringify(j.error), JSON.stringify(j)).to.eql(undefined));",
        "      return;",
        "    }",
        f"    if (attempt < {budget}) {{",
        f"      setTimeout(() => _awOne(oid, attempt + 1), {interval_ms});",
        "      return;",
        "    }",
        # Бюджет исчерпан. Исход НЕИЗВЕСТЕН — и это не то же самое, что успешный.
        f"    pm.test('операция уборки (' + {w} + ') ' + oid + ': прочитана и завершилась "
        f"в пределах бюджета', () => {{",
        "      pm.expect(code, res ? res.text() : 'ответа не пришло').to.eql(200);",
        "      pm.expect(j && j.done, JSON.stringify(j)).to.eql(true);",
        "    });",
        "  });",
        "};",
        "_awList.forEach(oid => _awOne(oid, 0));",
    ]


# Setup helpers — Network + Subnet для тестов, нуждающихся в parent.

def _setup_net(suffix):
    # Сеть объявляет план адресов: гонка режет из него подсети (10.250.*), а сеть без
    # объявленного плана подсеть не принимает вовсе — тогда все три участника гонки
    # получают синхронный отказ, победителей ноль, и проба состязания проверяет не то,
    # ради чего написана. Тело burst-запросов строится СКРИПТОМ, поэтому первая
    # редакция гейта фикстур этого места не видела; ветвь скриптовой нарезки добавлена
    # тем же изменением.
    return [
        Step(
            name="setup-net", method="POST", path="/vpc/v1/networks",
            body={"projectId": "{{_suiteProjectId}}", "name": f"conc-{suffix}-net-{{{{runId}}}}",
                  "ipv4CidrBlocks": ["10.0.0.0/8"]},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.networkId", "netId")],
        ),
        poll_operation_until_done(),
    ]


def _setup_subnet(suffix, cidr="10.250.0.0/24"):
    return [
        Step(
            name="setup-sub", method="POST", path="/vpc/v1/subnets",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": f"conc-{suffix}-sub-{{{{runId}}}}", "zoneId": "{{existingZoneId}}",
                  "ipv4CidrPrimary": cidr},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.subnetId", "subId")],
        ),
        poll_operation_until_done(),
    ]


# Cleanup DELETE of the caller's OWN just-created parent (subnet/network). Under a
# concurrency burst the owner-tuple can still be mid-materialization when cleanup fires,
# so the authz gate briefly returns 403 ("lacks relation v_delete ... no direct relations
# granted") — a read-your-writes lag, NOT a dependent-resource block (that would be a
# tolerated FAILED_PRECONDITION 400). Lenient retry_on=(403,) rides out the tuple lag;
# the real assertion then runs, and a genuinely-stuck deny still FAILS after budget
# (fail-closed). Not retrying 404: a cleanup 404 means already-gone, not lag.
#
# Обе полосы уборки ЧИТАЮТСЯ, а не принимаются скопом (`assert_cleanup_delete`):
# 200 обязан нести конверт Operation, 400 — код 9. Прежнее `oneOf([200, 400])`
# приняло бы и залипший 403, и смену контракта удаления на код 3.

def _cleanup_subnet():
    return retry_until_authorized(Step(
        name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
        test_script=[
            *assert_cleanup_delete("подсеть", "в подсети остались адреса или интерфейсы"),
            *save_from_response("j.id", "opId"),
        ],
    ), retry_on=(403,))


def _cleanup_net():
    return retry_until_authorized(Step(
        name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
        test_script=[
            *assert_cleanup_delete("сеть", "в сети остались подсети, таблицы маршрутизации или группы"),
            *save_from_response("j.id", "opId"),
        ],
    ), retry_on=(403,))


# ===========================================================================
# CASE 1 — SUB-CR-CONC-OVERLAP-BURST
# EXCLUDE constraint subnets_no_overlap_v4 защищает от parallel overlap-Create.
# ===========================================================================

CASES.append(Case(
    id="SUB-CR-CONC-OVERLAP-BURST",
    title="3 parallel Create Subnet same CIDR same Network → ровно 1 succeeds (EXCLUDE race-defense)",
    classes=["CONC", "NEG"],
    priority="P0",
    steps=[
        *_setup_net("ov"),
        Step(
            name="burst-create-overlap", method="POST", path="/vpc/v1/networks",  # path не используется в burst
            test_script=[
                # Каждый из 3 sends — на одну и ту же Network с одинаковым CIDR.
                *_burst_block_js(
                    3, "POST", "/vpc/v1/subnets",
                    body_js=(
                        "({projectId: pm.environment.get('_suiteProjectId'), "
                        "networkId: pm.environment.get('netId'), "
                        "name: `conc-ov-sub-${pm.environment.get('runId')}-${i}`, "
                        # placement_type — server-derived (F6): в тело не передаётся. zoneId
                        # задаёт placement-anchor → server-derives ZONAL. Без zoneId/regionId
                        # Create отвергся бы sync 400 «exactly one of zone_id, region_id must
                        # be set» → нет Operation → burst-assert краснеет.
                        "zoneId: pm.environment.get('existingZoneId'), "
                        # VPC-1 F7: Create takes the immutable primary anchor
                        # ipv4CidrPrimary (single) — identical anchor across the 3
                        # burst creates → network-scoped overlap EXCLUDE lets exactly
                        # one win (the retired v4_cidr_blocks key was dropped, so the
                        # bursts previously created CIDR-less subnets that never raced).
                        "ipv4CidrPrimary: '10.250.40.0/24'})"
                    ),
                ),
            ],
        ),
        Step(
            name="poll-and-assert", method="GET", path="/healthz",
            test_script=[
                # Wait briefly для отстрела всех async callbacks → парсим results.
                "setTimeout(() => {", "}, 100);",
                "const results = JSON.parse(pm.environment.get('burstResults') || '[]');",
                "pm.test('all 3 burst responses captured', () => pm.expect(results.length).to.eql(3));",
                # Subnet.Create держит ДВА уровня overlap-защиты: sync fail-fast
                # pre-check (checkSubnetCIDROverlap, Reader-TX — subnet/create.go) И
                # атомарный DB-EXCLUDE backstop (subnets_no_overlap_v4) в async worker'е.
                # Под burst'ом эти два уровня РЕЙСЯТ: если проигравший create стартует
                # ПОСЛЕ того как победитель уже закоммитил row, его sync pre-check ловит
                # overlap ПЕРВЫМ → sync 400 (code 9 «Subnet CIDRs can not overlap»), и
                # Operation для него не создаётся. Если все три прошли sync-check до
                # первого commit'а — все получают Operation (200), а overlap решает
                # EXCLUDE в worker'е (async op-error). ОБА исхода легитимны и НИКОГДА не
                # INTERNAL. (Раньше кейс требовал строго «все 3 → 200 async», что ложно
                # краснело под параллельной нагрузкой, когда pre-check выигрывал гонку.)
                "pm.test('each response is 200 (op) OR 400 sync-overlap-reject — never INTERNAL', () => results.forEach(r => {",
                "  if (r.code === 200) return;",
                "  pm.expect(r.code, JSON.stringify(r)).to.eql(400);",
                "  pm.expect(r.body && r.body.code, JSON.stringify(r)).to.eql(9);",
                "  pm.expect(String((r.body && r.body.message) || ''), JSON.stringify(r)).to.match(/can not overlap/i);",
                "}));",
                "const opIds = results.filter(r => r.code === 200).map(r => r.body && r.body.id).filter(Boolean);",
                "const syncRejects = results.filter(r => r.code === 400).length;",
                "pm.environment.set('burstOpIds', JSON.stringify(opIds));",
                "pm.environment.set('burstSyncRejects', String(syncRejects));",
                # Каждый 200 несёт opId; каждый sync-400 — нет. Их сумма = 3.
                "pm.test('opIds == 200-count (sync rejects carry no opId)', () => pm.expect(opIds.length).to.eql(3 - syncRejects));",
            ],
        ),
        Step(
            name="resolve-ops", method="GET", path="/healthz",
            test_script=[
                # Poll all 3 ops в одном test_script через counter.
                "const opIds = JSON.parse(pm.environment.get('burstOpIds') || '[]');",
                "const base = pm.environment.get('baseUrl');",
                "const tok = pm.environment.get('jwtProjectAdminA1');",
                "const ops = [];",
                "let pending = opIds.length;",
                "if (pending === 0) { pm.environment.set('burstOpsResolved', '[]'); }",
                "const tryOne = (oid, attempt) => {",
                "  pm.sendRequest({",
                "    url: base + '/operations/' + oid,",
                "    method: 'GET',",
                "    header: { 'Authorization': 'Bearer ' + tok },",
                "  }, (err, res) => {",
                "    let j = null; try { j = res.json(); } catch (e) {}",
                "    if (j && j.done) {",
                "      ops.push({id: oid, done: true, hasError: !!j.error, errCode: j.error ? j.error.code : null});",
                "      if (--pending === 0) pm.environment.set('burstOpsResolved', JSON.stringify(ops));",
                "    } else if (attempt < 12) {",
                "      setTimeout(() => tryOne(oid, attempt + 1), 500);",
                "    } else {",
                "      ops.push({id: oid, done: false, hasError: false, errCode: null});",
                "      if (--pending === 0) pm.environment.set('burstOpsResolved', JSON.stringify(ops));",
                "    }",
                "  });",
                "};",
                "opIds.forEach(oid => tryOne(oid, 0));",
            ],
        ),
        Step(
            name="assert-distribution", method="GET", path="/healthz",
            test_script=[
                "const ops = JSON.parse(pm.environment.get('burstOpsResolved') || '[]');",
                "const syncRejects = parseInt(pm.environment.get('burstSyncRejects') || '0', 10);",
                # Резолвятся все Operation'ы, которые были СОЗДАНЫ (== число 200-ответов).
                "pm.test('all created ops resolved (done=true)', () => {",
                "  pm.expect(ops.length).to.eql(3 - syncRejects);",
                "  ops.forEach(o => pm.expect(o.done, JSON.stringify(o)).to.eql(true));",
                "});",
                "const ok = ops.filter(o => !o.hasError).length;",
                "const opFailed = ops.filter(o => o.hasError).length;",
                # Инвариант race-defense (EXCLUDE + sync pre-check): из 3 identical-CIDR
                # create'ов РОВНО 1 выигрывает; остальные 2 проигрывают — каждый либо
                # sync-reject (400), либо async op-error (9/10). Победителей == 1;
                # проигравших (sync + async) == 2. Это НЕ ослабление EXCLUDE-контракта:
                # ровно одна подсеть выживает независимо от того, где отсёкся проигравший.
                "pm.test('exactly 1 winner + 2 losers (sync-reject OR async EXCLUDE)', () => {",
                "  pm.expect(ok, `ok=${ok} opFailed=${opFailed} syncRejects=${syncRejects} ops=${JSON.stringify(ops)}`).to.eql(1);",
                "  pm.expect(opFailed + syncRejects, `opFailed=${opFailed} syncRejects=${syncRejects}`).to.eql(2);",
                "});",
                # Проигравшие async-транзакции fail-closed одним из двух ЗАКОННЫХ кодов, но
                # НИКОГДА INTERNAL(13): 9 (FailedPrecondition) — чистый 23P01
                # exclusion_violation (проигравший увидел уже закоммиченный ряд), либо
                # 10 (Aborted) — 40001/40P01 serialization/deadlock под gist-EXCLUDE
                # burst'ом (retryable-конфликт, замаплен через ErrConflict, не INTERNAL).
                "pm.test('async failures: FailedPrecondition (9) or Aborted (10) — never INTERNAL/13', () => {",
                "  const failedCodes = ops.filter(o => o.hasError).map(o => o.errCode);",
                "  failedCodes.forEach(c => pm.expect(c, `code=${c}`).to.be.oneOf([9, 10]));",
                "});",
            ],
        ),
        # Cleanup: можем не знать который subId выжил — list по net и удалить все.
        #
        # Список подсетей СЕТИ спрашивается сужением списочного запроса по
        # `network_id`. Под-перечисление сети (`/vpc/v1/networks/{id}/subnets`)
        # снято с контракта как второй путь к одному ответу; края такого маршрута
        # не знает и отказывает fail-closed 403 (`type: authz.catalog`, пустой
        # `action`), поэтому шаг падал не на своём предмете. Хуже того — падал
        # ИМЕННО тот шаг, который несёт поведенческий замок EXCLUDE («две
        # пересекающиеся подсети не переживают»), то есть инвариант гонки
        # переставал проверяться вовсе.
        Step(
            name="list-and-cleanup", method="GET",
            path="/vpc/v1/subnets?projectId={{_suiteProjectId}}&pageSize=1000"
                 "&filter=network_id%3D%22{{netId}}%22",
            test_script=[
                *assert_status(200),
                "const subs = (pm.response.json().subnets || []);",
                # Прямой behavioral-lock EXCLUDE-инварианта: сколько бы create'ов ни
                # ушло, в сети НИКОГДА не переживают ДВЕ пересекающиеся подсети. (at.most
                # вместо eql — read-your-writes лаг может кратко не показать победителя,
                # но 2+ overlapping subnets = реальный провал EXCLUDE.)
                "pm.test('never 2 overlapping subnets survive (EXCLUDE holds)', () => pm.expect(subs.length).to.be.at.most(1));",
                "pm.environment.set('subToCleanupIds', JSON.stringify(subs.map(s => s.id)));",
            ],
        ),
        Step(
            name="cleanup-all-subs", method="GET", path="/healthz",
            test_script=_cleanup_burst_js(
                ids_var="subToCleanupIds", path_prefix="/vpc/v1/subnets",
                what="подсети гонки", ops_var="cleanupOpIds",
                outcomes_var="cleanupOutcomes"),
        ),
        # Уборка обязана ДОЕХАТЬ до удаления сети: сеть с живой подсетью отвечает
        # `FAILED_PRECONDITION`, и прежняя редакция этого шага — «дождались как
        # получилось, ничего не утверждаем» — превращала отказ уборки в тихий
        # переход к следующему шагу.
        Step(
            name="wait-cleanup", method="GET", path="/healthz",
            test_script=_await_cleanup_ops_js(
                ids_var="subToCleanupIds", ops_var="cleanupOpIds",
                outcomes_var="cleanupOutcomes", what="подсети гонки"),
        ),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))


# ===========================================================================
# CASE 2 — ADR-CR-CONC-BURST-ALLOC
# IPAM allocator → 5 parallel external Address.Create → 5 distinct IPs.
# ===========================================================================

CASES.append(Case(
    id="ADR-CR-CONC-BURST-ALLOC",
    title="5 parallel external Address.Create → 5 distinct IPs (UNIQUE pool slot defense)",
    classes=["CONC"],
    priority="P0",
    steps=[
        Step(
            name="burst-create-external", method="GET", path="/healthz",
            test_script=[
                *_burst_block_js(
                    5, "POST", "/vpc/v1/addresses",
                    body_js=(
                        "({projectId: pm.environment.get('_suiteProjectId'), "
                        "name: `conc-adr-${pm.environment.get('runId')}-${i}`, "
                        "externalIpv4AddressSpec: {zoneId: pm.environment.get('existingZoneId')}})"
                    ),
                ),
            ],
        ),
        Step(
            name="check-sync-200", method="GET", path="/healthz",
            test_script=[
                "const results = JSON.parse(pm.environment.get('burstResults') || '[]');",
                "pm.test('5 sync 200', () => {",
                "  pm.expect(results.length).to.eql(5);",
                "  results.forEach(r => pm.expect(r.code, JSON.stringify(r)).to.eql(200));",
                "});",
                "pm.environment.set('burstOpIds', JSON.stringify(results.map(r => r.body && r.body.id).filter(Boolean)));",
            ],
        ),
        Step(
            name="resolve-and-collect-ips", method="GET", path="/healthz",
            test_script=[
                "const opIds = JSON.parse(pm.environment.get('burstOpIds') || '[]');",
                "const base = pm.environment.get('baseUrl');",
                "const tok = pm.environment.get('jwtProjectAdminA1');",
                "const collected = [];",
                "let pending = opIds.length;",
                "const tryOne = (oid, attempt) => {",
                "  pm.sendRequest({",
                "    url: base + '/operations/' + oid,",
                "    method: 'GET',",
                "    header: { 'Authorization': 'Bearer ' + tok },",
                "  }, (err, res) => {",
                "    let j = null; try { j = res.json(); } catch (e) {}",
                "    if (j && j.done) {",
                "      const addrId = j.metadata && j.metadata.addressId;",
                "      const errCode = j.error ? j.error.code : null;",
                "      const ipv4 = j.response && j.response.externalIpv4Address ? j.response.externalIpv4Address.address : null;",
                "      collected.push({opId: oid, addrId, errCode, ipv4});",
                "      if (--pending === 0) pm.environment.set('burstAddrCollected', JSON.stringify(collected));",
                "    } else if (attempt < 12) {",
                "      setTimeout(() => tryOne(oid, attempt + 1), 500);",
                "    } else {",
                "      collected.push({opId: oid, addrId: null, errCode: null, ipv4: null, timeout: true});",
                "      if (--pending === 0) pm.environment.set('burstAddrCollected', JSON.stringify(collected));",
                "    }",
                "  });",
                "};",
                "opIds.forEach(oid => tryOne(oid, 0));",
            ],
        ),
        Step(
            name="assert-unique-ips", method="GET", path="/healthz",
            test_script=[
                "const items = JSON.parse(pm.environment.get('burstAddrCollected') || '[]');",
                "pm.test('5 ops resolved', () => pm.expect(items.length).to.eql(5));",
                "const ipv4s = items.filter(i => i.ipv4).map(i => i.ipv4);",
                "pm.test('all allocated IPs are unique (no duplicate slot)', () => {",
                "  const unique = new Set(ipv4s);",
                "  pm.expect(unique.size, `ipv4s=${JSON.stringify(ipv4s)}`).to.eql(ipv4s.length);",
                "});",
                "pm.test('all 5 succeeded (default pool has free slots)', () => {",
                "  const ok = items.filter(i => i.addrId).length;",
                "  pm.expect(ok, `items=${JSON.stringify(items)}`).to.eql(5);",
                "});",
                "pm.environment.set('cleanupAddrIds', JSON.stringify(items.map(i => i.addrId).filter(Boolean)));",
            ],
        ),
        # АДРЕС ИЗ ОГРАНИЧЕННОГО ПУЛА: невозвращённый адрес пул не пополняет.
        # Прежняя редакция этого шага несла ПУСТОЙ обработчик ответа (`() => {}`),
        # то есть слот молча утекал: под параллельным прогоном пул исчерпывается,
        # следующее выделение отвечает отказом, кейс получает фантомный ресурс —
        # и дальше каскад (`data-integrity.md` §Lease-recycle-on-delete,
        # `testing.md` §«утечка → пул растёт, контракты списков плывут»).
        Step(
            name="cleanup-addresses", method="GET", path="/healthz",
            test_script=_cleanup_burst_js(
                ids_var="cleanupAddrIds", path_prefix="/vpc/v1/addresses",
                what="адреса гонки", ops_var="addrCleanupOpIds",
                outcomes_var="addrCleanupOutcomes"),
        ),
        # «Удаление принято» и «адрес высвобожден» — РАЗНЫЕ утверждения: первое
        # говорит о синхронном ответе, второе об исходе воркера, который и снимает
        # слот. Кейс кончался на первом, поэтому отказ воркера был невидим.
        #
        # ГРАНИЦА, названная прямо: здесь утверждается «операция удаления
        # завершилась без ошибки», а НЕ «слот снова выделяется». Второе
        # доказывается повторным выделением, и это предмет отдельного кейса
        # исчерпания пула (`cases/internal-pool.py`).
        Step(
            name="wait-addr-cleanup", method="GET", path="/healthz",
            test_script=_await_cleanup_ops_js(
                ids_var="cleanupAddrIds", ops_var="addrCleanupOpIds",
                outcomes_var="addrCleanupOutcomes", what="адреса гонки"),
        ),
    ],
))


# ===========================================================================
# CASE 3 — NIC-CR-CONC-MAC-UNIQUE
# 10 parallel Create NIC same subnet → 10 distinct MACs (UNIQUE network_interfaces_mac_address_key).
# ===========================================================================

CASES.append(Case(
    id="NIC-CR-CONC-MAC-UNIQUE",
    title="10 parallel Create NIC → 10 distinct MACs (UNIQUE constraint + crypto/rand retry)",
    classes=["CONC"],
    priority="P1",
    steps=[
        *_setup_net("macu", ),
        *_setup_subnet("macu", "10.250.50.0/24"),
        Step(
            name="burst-create-nic", method="GET", path="/healthz",
            test_script=[
                *_burst_block_js(
                    10, "POST", "/vpc/v1/networkInterfaces",
                    body_js=(
                        "({projectId: pm.environment.get('_suiteProjectId'), "
                        "subnetId: pm.environment.get('subId'), "
                        "name: `conc-macu-${pm.environment.get('runId')}-${i}`})"
                    ),
                ),
            ],
        ),
        Step(
            name="resolve-ops-collect-mac", method="GET", path="/healthz",
            test_script=[
                "const results = JSON.parse(pm.environment.get('burstResults') || '[]');",
                "const opIds = results.map(r => r.body && r.body.id).filter(Boolean);",
                "const base = pm.environment.get('baseUrl');",
                "const tok = pm.environment.get('jwtProjectAdminA1');",
                "const collected = [];",
                "let pending = opIds.length;",
                "const tryOne = (oid, attempt) => {",
                "  pm.sendRequest({",
                "    url: base + '/operations/' + oid,",
                "    method: 'GET',",
                "    header: { 'Authorization': 'Bearer ' + tok },",
                "  }, (err, res) => {",
                "    let j = null; try { j = res.json(); } catch (e) {}",
                "    if (j && j.done) {",
                "      const nicId = j.metadata && j.metadata.networkInterfaceId;",
                "      const mac = j.response && j.response.macAddress;",
                "      const errCode = j.error ? j.error.code : null;",
                "      collected.push({nicId, mac, errCode});",
                "      if (--pending === 0) pm.environment.set('nicCollected', JSON.stringify(collected));",
                "    } else if (attempt < 12) {",
                "      setTimeout(() => tryOne(oid, attempt + 1), 500);",
                "    } else {",
                "      collected.push({nicId: null, mac: null, errCode: null, timeout: true});",
                "      if (--pending === 0) pm.environment.set('nicCollected', JSON.stringify(collected));",
                "    }",
                "  });",
                "};",
                "opIds.forEach(oid => tryOne(oid, 0));",
            ],
        ),
        Step(
            name="assert-mac-unique", method="GET", path="/healthz",
            test_script=[
                "const items = JSON.parse(pm.environment.get('nicCollected') || '[]');",
                "pm.test('10 NICs resolved', () => pm.expect(items.filter(i => i.nicId).length).to.eql(10));",
                "const macs = items.map(i => i.mac).filter(Boolean);",
                "pm.test('all 10 MACs are unique (UNIQUE constraint + crypto/rand retry)', () => {",
                "  const unique = new Set(macs);",
                "  pm.expect(unique.size, `macs=${JSON.stringify(macs)}`).to.eql(macs.length);",
                "  pm.expect(unique.size).to.eql(10);",
                "});",
                "pm.test('every MAC format 0e:xx:xx:xx:xx:xx', () => {",
                "  macs.forEach(m => pm.expect(m, m).to.match(/^0e:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}$/));",
                "});",
                "pm.environment.set('cleanupNicIds', JSON.stringify(items.map(i => i.nicId).filter(Boolean)));",
            ],
        ),
        Step(
            name="cleanup-nics", method="GET", path="/healthz",
            test_script=_cleanup_burst_js(
                ids_var="cleanupNicIds", path_prefix="/vpc/v1/networkInterfaces",
                what="интерфейсы гонки", ops_var="nicCleanupOpIds",
                outcomes_var="nicCleanupOutcomes"),
        ),
        # Дальше идёт удаление ПОДСЕТИ, в которой эти интерфейсы жили: подсеть с
        # живым интерфейсом отвечает отказом по состоянию. Значит утверждение об
        # исходе уборки интерфейсов — предусловие следующего шага, а не украшение.
        Step(
            name="wait-nic-cleanup", method="GET", path="/healthz",
            test_script=_await_cleanup_ops_js(
                ids_var="cleanupNicIds", ops_var="nicCleanupOpIds",
                outcomes_var="nicCleanupOutcomes", what="интерфейсы гонки"),
        ),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))


# ===========================================================================
# CASE 4 — NIC-ATTACH-CONC-BURST
# 5 parallel AttachToInstance same NIC → 1 win + 4 FailedPrecondition (CAS).
# ===========================================================================


# ===========================================================================
# CASE 5 — SG-URL-CONC-OCC-CONFLICT
# 2 parallel UpdateRules same SG → 1 OK + 1 Aborted (xmin OCC).
# ===========================================================================

CASES.append(Case(
    id="SG-URL-CONC-OCC-CONFLICT",
    title="2 parallel UpdateRules same SG → 1 OK + 1 Aborted/FailedPrecondition (xmin OCC)",
    classes=["CONC", "STATE"],
    priority="P0",
    steps=[
        *_setup_net("occ"),
        Step(
            name="create-sg", method="POST", path="/vpc/v1/securityGroups",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "conc-occ-sg-{{runId}}"},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")],
        ),
        poll_operation_until_done(),
        # Залп адресует СВЕЖИЙ объект в собственном URL, а не коллекцию в
        # project-scope, — значит он упирается в окно материализации owner-tuple
        # и обязан начаться ПОСЛЕ него. Обёртку ставить некуда: залп идёт из
        # скрипта через pm.sendRequest, шаг ходит на /healthz, и предикат
        # автообёртки его URL не видит по построению. Поэтому окно пережидает
        # отдельный шаг — обычное первое чтение своего свежего ресурса, которое
        # предикат обёртывает сам.
        #
        # Без него залп получал отказ ещё на крае: ни одна операция не
        # создавалась, occResolved оставался пустым, и кейс, рассчитанный на
        # гонку, краснел с ok=0 conflict=0 — то есть НЕ проверял ничего.
        # Ослабления здесь нет: утверждения про xmin-OCC ниже не тронуты, а
        # залп впервые доходит до сервиса.
        Step(
            name="await-sg-authorized", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
            test_script=[*assert_status(200)],
        ),
        Step(
            name="burst-update-rules", method="GET", path="/healthz",
            test_script=[
                # Два параллельных UpdateRules: один добавляет rule A, другой — rule B,
                # на одной и той же row → один из них должен fail на xmin-check.
                # UpdateRules REST-контракт: PATCH /securityGroups/{id}/rules с телом
                # {additionRuleSpecs:[...]} (НЕ POST :update-rules / {additions} — такого
                # маршрута нет → 404, opId пуст, occResolved=[] → assert-occ краснел).
                *_burst_block_js(
                    2, "PATCH", "/vpc/v1/securityGroups/${pm.environment.get('sgId')}/rules",
                    body_js=(
                        "({additionRuleSpecs: [{description: `conc-rule-${i}`, direction: 'EGRESS', "
                        "protocolName: 'ANY', cidrBlocks: {v4CidrBlocks: [`10.99.${i}.0/24`]}}]})"
                    ),
                ),
            ],
        ),
        Step(
            name="resolve-occ-ops", method="GET", path="/healthz",
            test_script=[
                "const results = JSON.parse(pm.environment.get('burstResults') || '[]');",
                "const opIds = results.map(r => r.body && r.body.id).filter(Boolean);",
                "const base = pm.environment.get('baseUrl');",
                "const tok = pm.environment.get('jwtProjectAdminA1');",
                "const ops = [];",
                "let pending = opIds.length;",
                "const tryOne = (oid, attempt) => {",
                "  pm.sendRequest({",
                "    url: base + '/operations/' + oid,",
                "    method: 'GET',",
                "    header: { 'Authorization': 'Bearer ' + tok },",
                "  }, (err, res) => {",
                "    let j = null; try { j = res.json(); } catch (e) {}",
                "    if (j && j.done) {",
                "      ops.push({done: true, hasError: !!j.error, errCode: j.error ? j.error.code : null});",
                "      if (--pending === 0) pm.environment.set('occResolved', JSON.stringify(ops));",
                "    } else if (attempt < 12) {",
                "      setTimeout(() => tryOne(oid, attempt + 1), 500);",
                "    } else {",
                "      ops.push({done: false, hasError: false, errCode: null});",
                "      if (--pending === 0) pm.environment.set('occResolved', JSON.stringify(ops));",
                "    }",
                "  });",
                "};",
                "opIds.forEach(oid => tryOne(oid, 0));",
            ],
        ),
        # Читаем финальный набор правил SG: сколько наших conc-rule-* реально осело.
        # Это timing-независимый детектор lost-update: при исправном xmin-OCC число
        # осевших правил ВСЕГДА равно числу ok-операций (overlap → ok=1/present=1;
        # сериализовано → ok=2/present=2). Если OCC сломан (оба ok, но second-writer
        # затёр набор) → present<ok → assert ниже краснеет.
        Step(
            name="fetch-sg-rules-after-occ", method="GET", path="/vpc/v1/securityGroups/{{sgId}}",
            test_script=[
                "const j = pm.response.json();",
                "pm.test('sg fetch ok', () => pm.expect(pm.response.code).to.eql(200));",
                "const rules = (j && j.rules) || [];",
                "const present = rules.filter(r => r && typeof r.description === 'string' "
                "&& /^conc-rule-\\d+$/.test(r.description)).length;",
                "pm.environment.set('occRulesPresent', String(present));",
            ],
        ),
        Step(
            name="assert-occ", method="GET", path="/healthz",
            test_script=[
                "const ops = JSON.parse(pm.environment.get('occResolved') || '[]');",
                "pm.test('2 occ ops resolved', () => pm.expect(ops.length).to.eql(2));",
                "const ok = ops.filter(o => !o.hasError && o.done).length;",
                "const conflict = ops.filter(o => o.hasError && (o.errCode === 9 || o.errCode === 10)).length;",
                "const present = parseInt(pm.environment.get('occRulesPresent') || '-1', 10);",
                "// gRPC 9=FailedPrecondition, 10=Aborted. Race может разрешаться обоими способами:",
                "// если xmin-CAS попал — второй writer получает 0 rows → ErrFailedPrecondition; либо Aborted.",
                "// Оба исхода валидны (overlap → 1 OK+1 conflict; сериализовано → 2 OK, оба",
                "// правила осели). НО обе операции успешными с ПОТЕРЕЙ одного правила",
                "// (second-writer-wins) — недопустимо: ключевой assert — present === ok.",
                "pm.test('xmin OCC: no lost update — rules present === ok (1|2), conflict surfaced as 9/10', () => {",
                "  pm.expect(ok + conflict, `ok=${ok} conflict=${conflict} ops=${JSON.stringify(ops)}`).to.eql(2);",
                "  pm.expect(ok, 'at least one must succeed').to.be.at.least(1);",
                "  pm.expect(present, `lost update: rules present=${present} != ok=${ok} (second-writer-wins?)`).to.eql(ok);",
                "});",
            ],
        ),
        Step(
            name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
            test_script=[
                *assert_cleanup_delete("группа безопасности", "группа ещё названа интерфейсом"),
                *save_from_response("j.id", "opId"),
            ],
        ),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))
