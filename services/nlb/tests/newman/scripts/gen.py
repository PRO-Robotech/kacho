#!/usr/bin/env python3

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/gen.py — generator of Postman collections from declarative
case-modules under tests/newman/cases/*.py (kacho-nlb).

Usage:
    python3 scripts/gen.py                      # all case modules → collections/<name>.postman_collection.json
    python3 scripts/gen.py load-balancer        # one module
    python3 scripts/gen.py --validate           # delegate to validate-cases.py (dup-id + CASES-INDEX coverage)

NLB-specific helpers live here; case modules see them through the injection table
(no `from gen import ...` — gen.py is loaded by path).
Форму коллекции и вспомогательный слой собирает ОБЩИЙ модуль
`tests/newman/kacholib/gen_shared.py` — один на дерево (#1367, #1377, #1379,
#1474). Здесь объявлено только то, чем ЭТОТ набор отличается: решения формы
(дескриптор `Emit`), решения оркестрации (дескриптор `Run`), таблица впрыска
и собственные помощники набора.

Соседний генератор образцом НЕ является и сверяться с ним не надо: расхождение
между копиями было предметом сведения, а не способом его проверить.
"""
from __future__ import annotations

import functools
import json
import re
import subprocess
import sys
import uuid
import importlib.util
from pathlib import Path
from dataclasses import dataclass, field, replace
from typing import List, Dict, Optional

# --- общий слой генератора (задача #1367) ------------------------------------
# Помощники ниже общие для ВСЕХ наборов newman и живут в дереве в одном
# экземпляре: `tests/newman/kacholib/gen_shared.py`. До сведения каждый набор нёс
# свою копию, и правка помощника стоила восьми правок — «поправил у себя» было
# неотличимо от «поправил везде».
def _kacholib_dir() -> Path:
    """Каталог общего слоя, найденный ВВЕРХ ОТ ЭТОГО ФАЙЛА, а не от cwd.

    Генератор зовут из каталога набора (`python3 scripts/gen.py`), поэтому путь,
    выведенный из текущего каталога, был бы свойством того, ОТКУДА позвали, а не
    того, где лежит дерево.
    """
    for parent in Path(__file__).resolve().parents:
        candidate = parent / "tests" / "newman" / "kacholib"
        if (candidate / "gen_shared.py").is_file():
            return candidate
    raise SystemExit(
        "общий слой генератора не найден: ожидается "
        "<корень>/tests/newman/kacholib/gen_shared.py.\n"
        "Это ОТКАЗ, а не пропуск: без общих помощников генератор собрал бы "
        "коллекции молча и не тем."
    )


sys.path.insert(0, str(_kacholib_dir()))

import gen_shared  # noqa: E402  — модуль нужен целиком: связывание опроса и его счётчик
from gen_shared import (  # noqa: E402  — импорт после провязки sys.path
    generate,
    Run,
    retry_until_authorized,
    retry_until_present,
    _RYA_SEQ,
    _accepted_http_codes,
    _assert_delete_operation_outcome,
    assert_field_violation,
    assert_grpc_code,
    assert_refusal_message,
    assert_refusal_message_contains,
    _assert_published_id_outcome,
    assert_status,
    _asserts_done,
    _asserts_outcome,
    _assigns_env_var,
    _body_text,
    build_collection,
    _carries_assertion,
    case_to_postman,
    _DELETE_ACCEPTED,
    Emit,
    _FRESH_VAR_SET_RE,
    http_method_not_allowed_block,
    _is_operation_id_var,
    _js_code_and_literals,
    js_comment,
    js_regex_src,
    js_str,
    load_cases_module,
    _MUTATION_METHODS,
    _OP_POLL_PATH,
    _PUB_ASSIGN_RE,
    _PUB_BIND_RE,
    _PUB_DECL_RE,
    _PUB_RESERVED,
    _PUB_SET_RE,
    _published_id_outcome_assert,
    _published_resource_vars,
    _REGEX_FLAGS,
    _regex_literal_must_contain_the_whole_pattern,
    _regex_must_parse_in_javascript,
    _REGEX_PARSE_CACHE,
    _reset_captured_operation_id,
    step_to_postman,
    _strip_js_comments,
    _VAR_REF_RE,
    _wrap_own_fresh_reads,
)


ROOT = Path(__file__).resolve().parents[1]
SCRIPTS_DIR = Path(__file__).resolve().parent
CASES_DIR = ROOT / "cases"
OUT_DIR = ROOT / "collections"

# Monotonic sequence for poll-step names within a single collection build.
# poll_operation_until_done() self-retries via pm.execution.setNextRequest(
# pm.info.requestName); newman resolves setNextRequest by request NAME and jumps
# to the FIRST match in the flattened collection. Identically-named "poll-op"
# steps (one per mutation, hundreds per collection) therefore made a mid-suite
# retry jump back to an early folder and skip the folders in between — a plain
# `newman run <collection>` traversed only a fraction of the cases. A per-step
# unique name keeps the self-retry unambiguous so full linear traversal is
# preserved. Reset to 0 at the start of every module load (load_cases_module) so
# names are deterministic per collection.


# ---------------------------------------------------------------------------
# Declarative structures
# ---------------------------------------------------------------------------

@dataclass
class Step:
    """A single HTTP request within a Case."""
    name: str
    method: str
    path: str  # relative; {{baseUrl}} prefix prepended automatically
    body: Optional[Dict] = None
    pre_script: List[str] = field(default_factory=list)
    test_script: List[str] = field(default_factory=list)
    # auth override per-step (None = inherit collection-level default Bearer):
    #   "anonymous"       — strip Authorization header before request
    #   "<envVarName>"    — Authorization: Bearer {{envVarName}} (resolved from env)
    auth: Optional[str] = None


@dataclass
class Case:
    """One test case — may contain multiple sequential steps."""
    id: str        # e.g. NLB-CR-CRUD-OK
    title: str     # human-readable summary
    classes: List[str]   # CRUD / VAL / NEG / BVA / CONF / STATE / IDEM / LSG / AZD
    priority: str        # P0 / P1 / P2 / P3
    steps: List[Step]


# ---------------------------------------------------------------------------
# Global pre-request — runs before every request in every case
# ---------------------------------------------------------------------------

# СТРАЖ НЕРАЗРЕШЁННОГО АДРЕСА — на уровне КОЛЛЕКЦИИ.
#
# Newman подставляет `{{имя}}` только если переменная где-то определена; неопределённую
# он оставляет ЛИТЕРАЛОМ и отправляет как есть. Запрос уходит на адрес вида
# `/operations/{{someOp}}`, сервис честно отвечает `invalid operation id`, а поллер
# крутит на этом весь свой предел — «не done» он читает как «ещё не готово», хотя повтор
# не сойдётся НИКОГДА. Замер боевой посадки 2026-07-30: из 12823 исполнений 992 ушли с
# неразрешённым `{{имя}}`; 791 из них размножили ОДНУ отвергнутую мутацию в десятки
# одинаковых отказов, а 201 не дали НИ ОДНОГО утверждения — исполнялись против шаблона и
# исчезали из вердикта («не выполнилось» тихо шло в зачёт «прошло», testing.md).
#
# Полный разбор класса и доказательство инъекцией — в `services/iam/tests/newman/scripts/gen.py`
# (там же перепись: 2571 опрос операции на 82 коллекции, под стражем helper'а — 188).
#
# ПРЕДИКАТ УЗКИЙ: срабатывает, только когда имя НЕ ОПРЕДЕЛЕНО НИ В ОДНОЙ области.
# Переменная, заданная ПУСТОЙ, — законный негативный кейс: newman подставит её пустой
# строкой, литерала не останется, страж до неё не доберётся by construction.
_URL_VAR_GUARD = [
    "(function () {",
    "  var _u = '';",
    "  try { _u = pm.request.url.toString(); } catch (e) { return; }",
    "  var _all = _u.match(/\\{\\{[A-Za-z0-9_]+\\}\\}/g);",
    "  if (!_all) { return; }",
    "  var _n = null;",
    "  for (var _i = 0; _i < _all.length; _i++) {",
    "    var _c = _all[_i].slice(2, -2);",
    "    if (!pm.variables.has(_c)) { _n = _c; break; }",
    "  }",
    "  if (_n === null) { return; }",
    "  pm.test('предусловие: {{' + _n + '}} не было захвачено — запрос не отправлен', function () {",
    "    pm.expect.fail(_n + ' не определена ни в одной области. Мутация, которая должна была её "
    "захватить, не вернула Operation (была отвергнута), либо захват не состоялся. Отправка ушла бы "
    "литералом и повтор не сошёлся бы никогда.');",
    "  });",
    "  pm.execution.skipRequest();",
    "})();",
]


PRE_GLOBAL = [
    "if (!pm.environment.get('runId') || pm.environment.get('runId') === '') {",
    "  const t = Date.now().toString(36);",
    "  const r = Math.floor(Math.random() * 1e9).toString(36);",
    "  pm.environment.set('runId', (t + r).replace(/[^a-z0-9]/g, '').slice(-10));",
    "}",
    "pm.environment.set('_suiteProjectId', pm.environment.get('existingProjectId'));",
    "pm.environment.set('_suiteProjectCrossId', pm.environment.get('existingProjectCrossId'));",
    "pm.environment.set('_suiteRegionId', pm.environment.get('existingRegionId'));",
    "pm.environment.set('_suiteRegionAltId', pm.environment.get('existingRegionAltId'));",
    "// Default auth: project-editor JWT on project A (sufficient for most happy-path steps).",
    "// Per-step auth= overrides via _auth_pre_script.",
    # СУБЪЕКТ СУИТЫ НЕ ПОДМЕНЯЕТСЯ. Здесь стоял fallback на jwtBootstrap
    # (кластерный администратор) «пока per-subject JWT не засеяны». Подмена молчаливая:
    # при незасеянном jwtProjectEditorA вся суита уезжала под администратора и проверяла
    # КАСКАД ЕГО ПРАВ, а не то, что заявлено (проектный editor). Зелёная суита при этом
    # ничего не говорила о заявленном субъекте, а happy-path 401, который fallback
    # «снимал», был честным сообщением о несделанном посеве.
    "const __defaultJwt = pm.environment.get('jwtProjectEditorA') || pm.variables.get('jwtProjectEditorA') || '';",
    "if (__defaultJwt && !pm.request.headers.has('Authorization')) {",
    "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __defaultJwt});",
    "}",
    # REQUIRED-FIXTURE GUARD: отсутствие фикстуры — ОТКАЗ с именем переменной, а не
    # подмена и не тишина. Пустой идентификатор уезжает в запрос как есть и отвергается
    # на авторизации, поэтому каждый последующий провал называл бы неверную причину.
    "const __needed = ['baseUrl', 'existingProjectId', 'existingRegionId', 'jwtProjectEditorA'];",
    "const __missing = __needed.filter((k) => !(pm.environment.get(k) || pm.variables.get(k) || ''));",
    "if (__missing.length > 0) {",
    "  pm.test('FIXTURE REQUIRED: ' + __missing.join(', '), () => pm.expect.fail('the suite fixture "
    "seed did not run: ' + __missing.join(', ') + ' are empty. Seed via tests/authz-fixtures "
    "(prodrun.sh patches the suite env). Falling back to another subject would test a DIFFERENT "
    "principal and report green about a subject that was never exercised.'));",
    # ...ЗАТЕМ ПРОПУСТИТЬ. Санкционированная форма — утвердить (назвав переменную) и
    # НЕ отправлять; она объявлена в exec-coverage.py (раздел STATIC BANS), эталон —
    # iam gen.py::require_env_url. Этот скрипт — pre-request КОРНЯ коллекции, то есть
    # исполняется перед КАЖДЫМ запросом: без пропуска весь набор уезжал бы без
    # предъявителя, о котором сказано прямо в тексте отказа выше, и каждый шаг
    # засчитывался бы против ДРУГОГО принципала. Утверждение уже отработало, поэтому
    # пропуск остаётся ЗАПИСАННЫМ падением с именем переменной, а не немым.
    "  pm.execution.skipRequest();",
    "}",
    *_URL_VAR_GUARD,
]


# ---------------------------------------------------------------------------
# АДРЕС НАРЕЗАЕМОЙ ПОДСЕТИ — ИЗ ПЛАНА СЕТИ, А НЕ ИЗ СОБСТВЕННОГО ХЕША
# ---------------------------------------------------------------------------
#
# Сеть набора (`existingNetworkId`) приходит из посева, и посев объявляет ей
# адресный план. Подсеть обязана лежать ВНУТРИ него: иначе синхронный отказ
# `subnet CIDR <X> is not within any network CIDR block`.
#
# Здесь стояли ПЯТЬ почти одинаковых копий генератора адреса, собиравших его из
# хеша прогона без всякой связи с планом (`'10.' + октет + '.' + октет + '.0/24'`,
# `'fd' + число.toString(16) + …`). Попадание в план было СОВПАДЕНИЕМ: держалось
# на том, что один из двух посевов набора объявляет `10.0.0.0/8` и `fd00::/8`.
# Второй посев (`tests/authz-fixtures/prodseed_nlb_ext.py`, посадка prodrun)
# объявляет план у́же — `10.196.0.0/16` и `fd00:196::/48`, — и мимо него уходили
# ВСЕ адреса всех пяти копий, на всех хешах. Симптом при этом уезжал далеко от
# причины: отказ получал шаг нарезки, а падали за ним шаги, которые ничего не
# резали, — с сообщением «предусловие: {{…}} не было захвачено».
#
# Теперь адрес ВЫВОДИТСЯ из плана, который посев публикует переменными
# `existingNetworkV4Plan` / `existingNetworkV6Plan`. Посев волен менять план —
# кейсы следуют за ним сами, и второго места об одном предмете не заводится.
#
# Держит это гейт `internal/repohygiene`
# `TestOutOfCaseCarveTakesItsCidrFromThePublishedPlan`: шаг, режущий подсеть в сети
# из посева и не прочитавший её плана, — находка. Гейт закрывает ту слепую зону,
# которую соседний `newmansubnetsupernet_test.go` объявлял числом.
#
# ---------------------------------------------------------------------------
# ПОЗИЦИЯ ВНУТРИ ПЛАНА — ВЫЧИСЛЯЕТСЯ, А НЕ РАЗЫГРЫВАЕТСЯ
# ---------------------------------------------------------------------------
#
# Попадание в план ничего не говорит о том, что ДВА набора одного прогона возьмут
# разные адреса. Пока позиция разыгрывалась хешем, столкновение было вопросом
# вероятности — и вероятность эта измерена, а не предположена: 69 нарезок за
# прогон (перепись по committed-коллекциям), план `10.0.0.0/8` даёт 65 536
# различных /24, симуляция самой формулы на 20 000 runId — **3.40 %** прогонов с
# хотя бы одним столкновением. По прогонам `e2e-newman` за 08-13…08-16 —
# два столкновения на 63 прогона с вердиктом шарда nlb, то есть 3.2 %.
#
# Под ВТОРЫМ посевом (`prodseed_nlb_ext.py`, план `10.196.0.0/16`) в плане всего
# 256 /24 — там жеребьёвка сталкивается практически ВСЕГДА (1 − e^(−69·68/512) ≈
# 99.99 %). То есть у одного и того же кода два режима: «через раз» и «всегда».
#
# Поэтому пространство плана делится на равные полосы — по одной на набор
# (`CIDR_BANDS` ниже), — а позиция внутри полосы задаётся порядковым номером
# нарезки. Столкновение становится невозможным ПО ПОСТРОЕНИЮ. Энтропия прогона
# остаётся и делает СВОЮ работу: она смещает всю сетку целиком, поэтому два
# прогона на одном стенде по-прежнему расходятся, а полосы внутри прогона
# по-прежнему не пересекаются (общий сдвиг по модулю сохраняет непересечение).
#
# Держит это гейт `internal/repohygiene`
# `TestCarvedSubnetsOfOneRunCanNotCollide`.

# CIDR_BANDS — полоса адресного пространства на набор. Таблица ЯВНАЯ, а не хеш от
# имени: хеш вернул бы ту же жеребьёвку, только на пяти значениях вместо 65 536.
# Набор, которого здесь нет, генерацию РОНЯЕТ — молча делить полосу с соседом
# нельзя, иначе непересечение перестаёт быть свойством и становится удачей.
CIDR_BANDS: Dict[str, int] = {
    "authz-deny": 0,
    "cross-resource": 1,
    "listener": 2,
    "load-balancer": 3,
    "placement-coherence": 4,
}

# Номера обязаны быть в точности 0..N-1, и это проверяется ЗДЕСЬ, при импорте.
#
# Ширина полосы — `span / __bands`, где `__bands = len(CIDR_BANDS)`, а позиция берётся
# по модулю `span`. Поэтому номер `>= len` не «выходит за план» (модуль вернёт его
# внутрь) — он ложится ПОВЕРХ чужой полосы: номера остаются различными, а адреса двух
# наборов совпадают снова, то есть возвращается ровно тот дефект, ради которого полосы
# и заведены. Дырка в нумерации даёт то же самое: снять набор из таблицы, не
# перенумеровав остальные, — самая правдоподобная правка этого места.
#
# Проверка стоит при импорте, а не в `carve_cidr_pre`: там она сработала бы только для
# наборов, которые что-то режут, и неверная таблица дожила бы до прогона.
_expected_bands = set(range(len(CIDR_BANDS)))
if set(CIDR_BANDS.values()) != _expected_bands:
    raise ValueError(
        f"gen: номера полос в CIDR_BANDS обязаны быть в точности "
        f"{sorted(_expected_bands)}, а объявлены {sorted(CIDR_BANDS.values())}. "
        f"Номер вне диапазона ложится ПОВЕРХ чужой полосы (позиция считается по "
        f"модулю ширины плана), и два набора одного прогона снова вырезают один /24 — "
        f"«Subnet CIDRs can not overlap» на шаге нарезки и каскад на шагах, которые "
        f"нарезки не делали. Снимая набор, перенумеруйте остальные подряд."
    )

_CARVE_HELPERS = [
    # Энтропия позиции: хеш прогона + порядковый номер нарезки + соль позиции.
    # Детерминирована по (runId, seq), поэтому повтор прогона воспроизводим, а
    # параллельные прогоны расходятся.
    #
    # ПЕРЕМЕШИВАНИЕ ЗДЕСЬ — НЕ КОСМЕТИКА, и оно измерено, а не предположено:
    # коллизия адресов и есть тот блуждающий флейк, разбор которого лежит в шапке
    # `_CIDR_ALLOC_PRE` набора listener. Соседние позиции отличаются лишь
    # постоянным слагаемым, поэтому БЕЗ перемешивания младшие байты двух позиций
    # связаны намертво. Замер на 20000 различных runId при seq=1, план `10.0.0.0/8`
    # (различных /24):
    #     снятая формула              16857   (сетка 220×256)
    #     без перемешивания             256   (обе позиции — один и тот же байт)
    #     с перемешиванием (здесь)    17243   при потолке 17236 для 16 свободных
    #                                         бит — то есть позиции независимы
    # Потолок задаёт ШИРИНА ПЛАНА, а не помощник: под планом `/16` свободен один
    # октет, и различных /24 достижимо ровно 256 — это свойство посева, и названо
    # здесь, чтобы его не искали в генераторе.
    #
    # Множитель выбран так, чтобы произведение оставалось ТОЧНЫМ в double
    # (65599 × 2³¹ < 2⁵³): `Math.imul` в песочнице newman'а недоступен, а молча
    # потерянная точность дала бы смещённый разброс, который ничем не виден.
    "function __mix(v) {",
    "  v = (v ^ (v >>> 13)) & 0x7fffffff;",
    "  v = (v * 65599) % 2147483647;",
    "  return (v ^ (v >>> 11)) & 0x7fffffff;",
    "}",
    "function __ent(k) {",
    "  return __mix(__h + (__seq + 1) * 7919 + k * 104729) & 0xffff;",
    "}",
    # Разбор плана и нарезка ВНУТРИ него. Позиция, целиком накрытая префиксом
    # плана, сохраняется как есть; накрытая частично — по маске; свободная —
    # энтропией. Так адрес лежит внутри плана ЛЮБОЙ ширины, а не только той, под
    # которую его однажды подогнали.
    # Позиция /24 внутри плана: полоса набора + порядковый номер нарезки, всё
    # смещено общим для прогона сдвигом. Арифметика, а не битовые операции: план
    # шире /8 даёт номера до 2²⁴, и `<<` пришлось бы читать со знаком.
    #
    # `'!band'` — ОТДЕЛЬНЫЙ исход, а не пустая строка: «плана нет» и «набор
    # перерос свою полосу» лечатся разным, и общий текст отказа сделал бы их
    # неразличимыми — ровно тот класс, который мы ловим в продукте.
    "function __carve4(plan) {",
    "  var m = /^\\s*(\\d+)\\.(\\d+)\\.(\\d+)\\.(\\d+)\\/(\\d+)\\s*$/.exec(plan || '');",
    "  if (!m) { return ''; }",
    "  var o = [+m[1], +m[2], +m[3], +m[4]], L = +m[5];",
    "  if (L > 24) { return ''; }",
    "  var span = Math.pow(2, 24 - L);",
    "  var width = Math.floor(span / __bands);",
    "  if (width < 1 || __seq > width) { return '!band'; }",
    "  var top = o[0] * 65536 + o[1] * 256 + o[2];",
    "  var net = top - (top % span);",
    "  var pos = (__mix(__runh) % span + __bandIndex * width + (__seq - 1)) % span;",
    "  var v = net + pos;",
    "  return Math.floor(v / 65536) + '.' + (Math.floor(v / 256) % 256) + "
    "'.' + (v % 256) + '.0/24';",
    "}",
    "function __hextets(addr) {",
    "  var parts = String(addr).split('::');",
    "  var head = parts[0] ? parts[0].split(':') : [];",
    "  var tail = (parts.length > 1 && parts[1]) ? parts[1].split(':') : [];",
    "  var all;",
    "  if (parts.length > 1) {",
    "    var fill = 8 - head.length - tail.length;",
    "    if (fill < 0) { return null; }",
    # Развёртка `::` — явным циклом, а не `new Array(n).fill()`: песочница newman'а
    # урезана (по той же причине здесь нет `Math.imul`), и цена отказа — каждая
    # нарезка каждого набора.
    "    var mid = [];",
    "    for (var f = 0; f < fill; f++) { mid.push('0'); }",
    "    all = head.concat(mid).concat(tail);",
    "  } else { all = head; }",
    "  if (all.length !== 8) { return null; }",
    "  var v = [];",
    "  for (var i = 0; i < 8; i++) {",
    "    var n = parseInt(all[i] || '0', 16);",
    "    if (isNaN(n)) { return null; }",
    "    v.push(n & 0xffff);",
    "  }",
    "  return v;",
    "}",
    "function __carve6(plan) {",
    "  var s = String(plan || '').split('/');",
    "  if (s.length !== 2) { return ''; }",
    "  var h = __hextets(s[0]), L = parseInt(s[1], 10);",
    "  if (!h || isNaN(L) || L > 64) { return ''; }",
    "  for (var k = 0; k < 4; k++) {",
    "    var before = k * 16;",
    "    if (L >= before + 16) { continue; }",
    "    var keep = Math.max(0, L - before);",
    "    var mask = keep === 0 ? 0 : ((0xffff << (16 - keep)) & 0xffff);",
    "    h[k] = (h[k] & mask) | (__ent(k + 8) & (~mask & 0xffff));",
    "  }",
    "  return h[0].toString(16) + ':' + h[1].toString(16) + ':' + h[2].toString(16) +",
    "    ':' + h[3].toString(16) + '::/64';",
    "}",
]


def _carve_guard(plan_var: str, cidr_var: str) -> List[str]:
    """Отсутствие/непригодность плана — ОТКАЗ с именем переменной, а не тихий адрес.

    Подстановка запасного литерала здесь была бы тем же дефектом, от которого
    помощник и заводится: адрес, не связанный с планом. Поэтому запрос НЕ уходит,
    а падение называет переменную и путь её появления.
    """
    return [
        # Полоса кончилась — это НЕ «плана нет»: план на месте, набор перерос свою
        # долю. Исходов два (расширить план посева либо разбить набор), и отказ
        # обязан их различать, иначе читатель начнёт чинить не то.
        f"if ({cidr_var} === '!band') {{",
        f"  pm.test({js_str(f'FIXTURE REQUIRED: {plan_var} band exhausted')}, () => pm.expect.fail("
        "'the suite asked for carve #' + __seq + " + js_str(
            f" but its band inside {plan_var} "
            "holds fewer. The plan is split into __bands equal bands, one per suite "
            "(CIDR_BANDS in services/nlb/tests/newman/scripts/gen.py), so that two suites "
            "of one run can never carve the same /24. Widen the plan the seeder declares, "
            "or split the suite — do NOT go back to drawing the position, that is the "
            "collision this band exists to kill.") + "));",
        "  pm.execution.skipRequest();",
        "  return;",
        "}",
        f"if (!{cidr_var}) {{",
        f"  pm.test({js_str(f'FIXTURE REQUIRED: {plan_var}')}, () => pm.expect.fail(" + js_str(
            f"{plan_var} is empty or not a CIDR the suite can carve a subnet from. "
            "The seeder that creates existingNetworkId publishes the address plan it "
            "declared (deploy/scripts/seed-nlb-fixtures.sh, tests/authz-fixtures/"
            "prodseed_nlb_ext.py). Hardcoding an address instead would restore the very "
            "defect this helper exists to kill: a CIDR unrelated to the plan, which lands "
            "inside it only by coincidence.") + "));",
        "  pm.execution.skipRequest();",
        "  return;",
        "}",
    ]


def carve_cidr_pre(scope: str, v4_var: str = "_subnetCidr",
                   v6_var: Optional[str] = None) -> List[str]:
    """Pre-request: run-scoped адрес(а) подсети, вырезанные ИЗ ПЛАНА сети посева.

    `scope` разводит наборы между собой, `__seq` разводит нарезки внутри одного
    набора. Для v4 это разведение ВЫЧИСЛЯЕТСЯ (полоса набора + номер нарезки),
    поэтому столкновение невозможно по построению; для v6 остаётся хеш — там
    свободных бит 56, и предмета у запрета нет (см. шапку `CIDR_BANDS`).
    """
    if scope not in CIDR_BANDS:
        raise KeyError(
            f"gen: набор '{scope}' не имеет полосы в CIDR_BANDS. Полоса — то, чем "
            f"держится непересечение адресов между наборами одного прогона; без "
            f"записи набор делил бы полосу с соседом, и столкновение вернулось бы "
            f"молча. Заведите номер в CIDR_BANDS (services/nlb/tests/newman/"
            f"scripts/gen.py) — он же попадёт в коллекцию и будет проверен гейтом "
            f"TestCarvedSubnetsOfOneRunCanNotCollide."
        )
    out = [
        "(function () {",
        "  var __seq = parseInt(pm.environment.get('_cidrSeq') || '0', 10) + 1;",
        "  pm.environment.set('_cidrSeq', String(__seq));",
        f"  var __bandIndex = {CIDR_BANDS[scope]};",
        f"  var __bands = {len(CIDR_BANDS)};",
        "  var __runOnly = pm.environment.get('runId') || 'x0';",
        f"  var __run = __runOnly + {js_str(f'/{scope}')};",
        "  var __h = 0;",
        "  for (var i = 0; i < __run.length; i++) { __h = ((__h << 5) - __h + __run.charCodeAt(i)) | 0; }",
        "  __h = __h & 0x7fffffff;",
        # Сдвиг сетки берётся из runId БЕЗ набора: он обязан быть общим для всех
        # полос, иначе полосы разъедутся по-разному и перестанут не пересекаться.
        "  var __runh = 0;",
        "  for (var j = 0; j < __runOnly.length; j++) "
        "{ __runh = ((__runh << 5) - __runh + __runOnly.charCodeAt(j)) | 0; }",
        "  __runh = __runh & 0x7fffffff;",
        *["  " + ln for ln in _CARVE_HELPERS],
        "  var __v4 = __carve4(pm.environment.get('existingNetworkV4Plan'));",
        *["  " + ln for ln in _carve_guard("existingNetworkV4Plan", "__v4")],
        f"  pm.environment.set({js_str(v4_var)}, __v4);",
    ]
    if v6_var:
        out += [
            "  var __v6 = __carve6(pm.environment.get('existingNetworkV6Plan'));",
            *["  " + ln for ln in _carve_guard("existingNetworkV6Plan", "__v6")],
            f"  pm.environment.set({js_str(v6_var)}, __v6);",
        ]
    out.append("})();")
    return out


# ---------------------------------------------------------------------------
# Reusable assertion snippets (pm.*) — same names as kacho-vpc
# ---------------------------------------------------------------------------

def assert_unscoped_rejected() -> List[str]:
    """Unscoped create/list (без projectId/parent-scope) — ОТВЕРГНУТ. Два защитимых
    исхода, оба = «отклонено» (defense-in-depth, security.md «authz-first», parity
    с vpc 446e25b / compute 32be094):
      403 PERMISSION_DENIED (code 7) — gateway scope_extractor fail-closed
        «no path: unscoped resource» ДО backend-валидации: нельзя авторизовать
        запрос без scope для anti-BOLA;
      400 INVALID_ARGUMENT  (code 3) — backend «required field» при passthrough.
    Толерантен к обоим. Techniques: ECP (класс «unscoped запрос») + error-guessing
    (authz-vs-validation ordering)."""
    return [
        "pm.test('unscoped rejected (400 InvalidArgument or 403 authz-first)', () => {",
        "  pm.expect(pm.response.code, JSON.stringify(pm.response.json())).to.be.oneOf([400, 403]);",
        "});",
        "pm.test('grpc code 3 (INVALID_ARGUMENT) or 7 (PERMISSION_DENIED)', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.code, JSON.stringify(j)).to.be.oneOf([3, 7]);",
        "});",
    ]


def assert_absent_id_rejected() -> List[str]:
    """Negative-запрос на ОТСУТСТВУЮЩИЙ / malformed id (Get/Update/Delete или
    :verb-action / вложенный list по нему) — ОТВЕРГНУТ. Три защитимых исхода, все
    = «отклонено» (defense-in-depth, security.md «authz-first», parity с vpc):
      403 PERMISSION_DENIED (code 7) — gateway scope_extractor не может резолвить
        target→project для anti-BOLA у несуществующего/битого id → fail-closed ДО
        backend format-check / repo.Get (для МУТАЦИЙ устойчиво, id захардкожен как
        garbage — не из setup, поэтому не зависит от фикстур);
      404 NOT_FOUND (code 5) — well-formed-но-нет: sync AuthZ-Get/repo.Get;
      400 INVALID_ARGUMENT (code 3) — malformed id: corevalidate.ResourceID.
    Толерантен 400|403|404 (code 3|5|7) — семантика негатива (rejected) сохранена
    без ложного провала на корректном authz-first 403 (GATE-RUN #5:
    upd-imm/del-unknown/move-nx/stop-unknown/list-ops возвращали 403 вместо 400/404).
    Techniques: ECP (класс «absent id») + error-guessing (authz-vs-existence ordering)."""
    return [
        "pm.test('absent-id request rejected (400/403/404)', () => {",
        "  pm.expect(pm.response.code, JSON.stringify(pm.response.json())).to.be.oneOf([400, 403, 404]);",
        "});",
        "pm.test('grpc code INVALID_ARGUMENT/NOT_FOUND/PERMISSION_DENIED (3/5/7)', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.code, JSON.stringify(j)).to.be.oneOf([3, 5, 7]);",
        "});",
    ]


def assert_refused_sync_or_async(what: str, sync_codes=(400,), async_lane: bool = True) -> List[str]:
    """The request under test is REFUSED, by whichever of the two lawful lanes decides it.

    Kachō mutations answer with an Operation (ban #9), so a refusal has two shapes and a
    case naming one input as illegal must accept both — and NOTHING else:

      * the sync validator refuses before the Operation exists → `400 INVALID_ARGUMENT`;
      * the worker refuses (a peer that does not resolve, a DB constraint, a state guard)
        → `200` carrying an Operation that finishes WITH an error.

    What this replaces was `oneOf([200, 400])` with nothing after it. Written that way the
    second lane is not checked at all: `200` alone passes, so the case is satisfied by the
    product ACCEPTING exactly the input it exists to prove refused, and a regression that
    drops the guard leaves it green. That is not tolerance of an ordering, it is the
    absence of an assertion.

    Pair this with `poll_operation_until_done(must_fail=True)` as the very next step — that
    is what states the second lane. On the sync lane `opId` is cleared here, so the poll has
    no subject and asserts nothing; on the async lane it holds the Operation to account.
    `what` names the input in the assertion text so a failure is actionable.

    `sync_codes` widens the FIRST lane where the ordering genuinely admits more than one
    refusal — the gateway can deny before the backend looks at the body (`403`), and a
    resource it cannot resolve reads as absent (`404`). Every entry must be a REFUSAL;
    `200` is supplied by this helper as the async lane and belongs nowhere else.

    `async_lane=False` — THE SECOND LANE DOES NOT EXIST FOR THIS INPUT, so naming it
    would be a promise the product cannot break. Use it only where no Operation can be
    minted at all, and say why at the call site. Measured case: an input naming a project
    that does not exist. `project_id` is the scope the edge resolves for the anti-BOLA
    check, so an unresolvable one is denied BEFORE the backend reads the body — there is
    no path on which a worker gets to refuse it later. With the lane declared anyway,
    `opId` stays unset and the paired poll addresses `{{opId}}` as a literal; the address
    guard names it, and rightly — the step had no subject. Two of nlb's fifteen red
    assertions on the production-posture run of 2026-07-31 were exactly that, and the
    other eleven users of this helper DO reach the async lane, which is why the lane is
    parameterised rather than removed.
    """
    codes = ", ".join(str(c) for c in ((*sync_codes, 200) if async_lane else sync_codes))
    named = "/".join(str(c) for c in sync_codes)
    # The title is ENCODED, not pasted. `what` comes from the caller and may legitimately
    # contain an apostrophe; pasted into a single-quoted literal it breaks the string, and
    # the whole step script stops parsing. The step then does not fail — it does not RUN:
    # `pm.test` is never reached, neither is `pm.environment.unset('opId')`, and the paired
    # poller travels on an `opId` left over from an earlier step. This is the sibling of the
    # vpc defect measured 2026-08-03 (10 unparseable scripts there); nlb is where that helper
    # was borrowed from, so it carried the same latent form with no apostrophe to fire it.
    if not async_lane:
        return [
            # `opId` СБРАСЫВАЕТСЯ В ПУСТОЕ, а не снимается.
            #
            # Снятое имя не определено ни в одной области, и страж неразрешённых
            # подстановок (см. _URL_VAR_GUARD) справедливо считает такой шаг находкой:
            # запрос ушёл бы литералом `{{opId}}`. Но здесь отсутствия Operation —
            # ОБЪЯВЛЕННЫЙ и уже проверенный исход синхронного отказа, а не промах
            # захвата. Пустое значение решает обе задачи разом: устаревший
            # идентификатор предыдущего шага не переживает шаг (ради чего снятие и
            # вводилось), имя остаётся определённым, поэтому страж до него не
            # доберётся by construction — ровно тот законный негативный случай,
            # который его собственный комментарий оговаривает, — а парный поллер
            # видит пустую строку, она ложна, и он выходит не утверждая ничего.
            "pm.environment.set('opId', '');",
            f"pm.test({json.dumps(f'{what} refused synchronously ({named}) — no Operation is minted')}, () => "
            f"  pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([{codes}]));",
        ]
    return [
        "pm.environment.set('opId', '');",
        f"pm.test({json.dumps(f'{what} refused (sync {named}, or an Operation that fails)')}, () => "
        f"  pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([{codes}]));",
        "if (pm.response.code === 200) {",
        "  const j = pm.response.json();",
        # An accepted mutation that hands back no Operation leaves nothing to hold to
        # account: the refusal would be asserted against a subject that does not exist.
        "  pm.test('accepted response carries the Operation that must then fail', () => "
        "    pm.expect(j.id, pm.response.text()).to.be.a('string'));",
        "  if (j.id) pm.environment.set('opId', j.id);",
        "}",
    ]


def save_from_response(jsonpath: str, env_var: str) -> List[str]:
    """Сохранить значение из response в env.

    ИДЕНТИФИКАТОР ОПЕРАЦИИ СБРАСЫВАЕТСЯ ПЕРЕД ЗАХВАТОМ — это ЗАМЕНА, а не дозапись.

    Отвергнутая мутация тела с `id` не несёт, поэтому запись ниже не происходит, и
    имя молча сохраняло бы операцию ПРЕДЫДУЩЕГО шага — как правило давно
    завершённую. Опрос подтверждает её `done`, кейс зеленеет БЫСТРО и уверенно, не
    проверив собственную мутацию вовсе.

    СБРОС В ПУСТОЕ, А НЕ СНЯТИЕ ИМЕНИ — и это выбор по порядку исполнения, а не по
    вкусу. Здесь страж неразрешённого адреса (`_URL_VAR_GUARD`) стоит в пред-скрипте
    КОЛЛЕКЦИИ, то есть исполняется раньше любого пред-скрипта шага: снятое имя он
    справедливо считает находкой и роняет опрос — включая тот, чей шаг удаления
    ОБЪЯВИЛ ветку «уже нет» законной (`oneOf([200, 404])`, 222 таких шага в дереве).
    Пустое значение решает задачу гейта (устаревший идентификатор не переживает шаг)
    и не превращает объявленный исход в отказ: имя остаётся определённым, страж до
    него не доберётся by construction, а парный опрос видит пустую строку и выходит,
    не утверждая ничего. Собственный исход мутации при этом НЕ теряется — его
    утверждает сам шаг мутации (гейт `assert-delete-steps-are-asserted.py`:
    1359 из 1359 шагов удаления несут утверждение).

    Ровно эту форму и по этой же причине уже выбрал `assert_refused_sync_or_async`.
    В iam сброс записан СНЯТИЕМ — там страж переехал в конец пред-скрипта каждого
    шага и идёт ПОСЛЕ `_op_id_guard`, который сам решает, находка это или
    санкционированный пропуск уборки.

    Разбор класса с переписью — `services/iam/tests/newman/scripts/gen.py`, где он
    был закрыт первым; гейт по дереву на обе половины пары «удаление → опрос» —
    `deploy/scripts/assert-delete-operation-outcome.py`.
    """
    reset = [f"pm.environment.set({js_str(env_var)}, '');"] if _is_operation_id_var(env_var) else []
    return [
        *reset,
        "try {",
        "  const j = pm.response.json();",
        f"  const v = ({jsonpath});",
        f"  if (v !== undefined && v !== null) pm.environment.set({js_str(env_var)}, String(v));",
        "} catch (e) {}",
    ]


def assert_operation_envelope(prefix_regex: str = "^(nlb|tgr|lst)[a-z0-9]+$") -> List[str]:
    return [
        "pm.test('Operation envelope returned', () => {",
        "  const j = pm.response.json();",
        f"  pm.expect(j.id, 'operation.id').to.match(/{js_regex_src(prefix_regex, where='nlb/assert_operation_envelope/prefix_regex')}/);",
        "  pm.expect(j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
    ]




# ── ВЕДОМОСТЬ ОЖИДАНИЯ: исчерпание бюджета отличимо от его ненадобности ──────
#
# ПРЕДМЕТ (задача #1251). У всякой обёртки ожидания концовка была одна: «повторять
# больше не нужно ЛИБО уже нельзя» — и оба исхода вели в один и тот же сброс
# счётчиков. То есть «прогреть не удалось» и «прогрев не понадобился» давали
# ОДИНАКОВЫЙ след — никакого. У шага, несущего своё утверждение, оно после этого
# падало (fail-open работает), но по падению не читалось, что причина в исчерпании;
# у шага БЕЗ своего утверждения исчерпание проходило вовсе бесследно, а отказ
# доезжал до следующего шага — атрибуция сохранялась, наблюдаемость нет.
#
# Это ровно то окно, из которого вырос исходный разбор: создание получило отказ в
# правах в окне материализации, а журнал обвинил проверку запрета удаления, то есть
# невиновного. Пока два состояния неразличимы, разбор красного начинается с
# гипотезы, а не с факта.
#
# ЧТО ЗАПИСЫВАЕТСЯ — ТРИ СОСТОЯНИЯ, А НЕ ДВА:
#   исчерпание   — переходное состояние ДЕРЖИТСЯ, а бюджет израсходован. Считается
#                  и НАЗЫВАЕТСЯ по имени шага: перечень отвечает на «где именно».
#   понадобился  — состояние ушло, но попытки были. Обновляется НАИБОЛЬШЕЕ число
#                  потраченных попыток: это и есть величина, прямо говорящая, верно
#                  ли выбран бюджет. Её тоже никто не наблюдал.
#   не понадобился — попыток ноль. Не записывается ничего: событие пустое.
#
# ПОЧЕМУ НЕ УТВЕРЖДЕНИЕМ. Утверждение здесь сменило бы ВЕРДИКТ: шаг, чьё окно
# закрылось на попытку позже бюджета, стал бы красным, хотя его предмет исправен, —
# а fail-open заведён ровно затем, чтобы настоящий отказ падал на СВОЁМ шаге и по
# СВОЕМУ предмету. Величина обязана быть видна, но не обязана быть отказом; порог
# по ней — решение прогонщика, а не обёртки. Вакуумное `pm.test`, зелёное всегда,
# запрещено отдельно (`testing.md` §«Гейт на класс»): оно заняло бы слот и не
# сказало бы ничего.
#
# ВИДНО ЭТО В ПРОГОНЕ. Величины уезжают в окружение, а newman кладёт итоговое
# окружение в машинный отчёт (`environment.values`) — оттуда их читает
# `scripts/run.sh` и печатает вместе с числами вердикта. Проверено: величина,
# записанная шагом, доезжает до отчёта.
def _budget_ledger(transient_expr: str, count_var: str, budget: int) -> List[str]:
    """Строки концовки guard'а, разводящие исчерпание бюджета и его ненадобность.

    `transient_expr` — то же выражение переходности, по которому обёртка решала
    повторять; `count_var` — её счётчик попыток. Оба берутся у вызывающего, а не
    воспроизводятся здесь: вторая копия условия разошлась бы с первой молча, и
    ведомость стала бы считать не то, чего ждала обёртка.
    """
    return [
        # `|| 0` СНАРУЖИ parseInt, а не внутри: величина приходит из окружения, а туда
        # её может положить кто угодно — прогонщик через `--env-var`, файл окружения,
        # соседний шаг. `parseInt('что-угодно')` даёт NaN, а NaN+1 остаётся NaN и
        # записывается строкой «NaN»: ведомость с этого места считает молча в никуда,
        # и «ноль исчерпаний» становится неотличимо от сломанного счётчика — ровно тот
        # класс, ради снятия которого она заведена. Поймано инъекцией: харнесс
        # самопроверки отдаёт для незнакомого ключа строку, а не пустоту.
        f"if ({transient_expr} && {count_var} >= {budget}) {{",
        "  const _wbE = (parseInt(pm.environment.get('warmBudgetExhausted'), 10) || 0) + 1;",
        "  pm.environment.set('warmBudgetExhausted', String(_wbE));",
        "  const _wbL = pm.environment.get('warmBudgetExhaustedSteps') || '';",
        "  pm.environment.set('warmBudgetExhaustedSteps',",
        "    (_wbL ? _wbL + ' ' : '') + pm.info.requestName);",
        f"}} else if ({count_var} > 0) {{",
        "  const _wbM = parseInt(pm.environment.get('warmRetryMaxAttempts'), 10) || 0;",
        f"  if ({count_var} > _wbM) {{",
        f"    pm.environment.set('warmRetryMaxAttempts', String({count_var}));",
        "  }",
        "}",
    ]


# Окно видимости прав — РЕШЕНИЕ НАБОРА, а не общего слоя (#1379): путь
# материализации у доменов разный, и одно число за всех было бы решением
# за них. Здесь — у nlb окно замерено и НАМЕРЕННО не поднято (см. шапку `_budget_ledger`). Величина видна
# на связывании, а не в прозе шапки: три копии из шести называли ЧУЖУЮ.
# Голова полосы — общая (#1477). Шаг, чей адрес собран из НЕЗАХВАЧЕННОЙ переменной,
# спрашивает не о ресурсе: окно видимости прав такой адрес не наполнит никогда, а
# отказ по нему приходит кодом ИЗ полосы ожидания — то есть шаг выжигает весь бюджет
# и падает, называя следствие вместо предмета.
_rya = functools.partial(retry_until_authorized,
                        budget=25, interval_ms=500, ledger=_budget_ledger, lane_head=True)
# То же окно у СПИСОЧНОГО ожидания — и то же правило: величину называет НАБОР,
# а не общий слой (#1379). Три копии этой обёртки расходились ещё и телом:
# одна ждала появления ВСЕХ названных имён, другая вела ведомость исчерпания,
# третья не делала ни того, ни другого. Общая форма несёт обе починки, а
# различие набора выражено ЗДЕСЬ — аргументом, видимым на связывании.
_rup = functools.partial(retry_until_present,
                        budget=25, interval_ms=500, ledger=_budget_ledger)


def warm_peer_fixture(base_path: str, id_var: str, suffix: str,
                      auth: Optional[str] = None) -> Step:
    """Прогреть ЧУЖОЙ свежий ресурс чтением — до того, как его идентификатор уедет в
    асинхронную мутацию этого набора.

    Зачем (issue #351). `retry_until_authorized` решает о повторе по КОДУ ОТВЕТА шага,
    а асинхронная мутация отвечает `200` и конвертом `Operation` ВСЕГДА: отказ владельца
    чужого ресурса приезжает терминальной ошибкой ВНУТРИ операции и читается уже другим
    шагом. Значит на такой мутации окно материализации прав у ЧУЖОГО идентификатора не
    закрыто ничем — обёртка на ней закрывает только полосу шлюза (собственную цель
    проверки прав этого RPC).

    Отличить открытое окно от настоящего промаха кейс не может by construction: владелец
    прячет существование и берёт текст из общей таблицы (`security.md` §6), поэтому
    красное приезжает как утверждение о продукте — ровно так и было прочитано в разборе,
    из которого заведён #351.

    Греется ЧТЕНИЕМ, потому что на нём обёртка работает: 403/404 видны кодом ответа,
    ожидание ограничено бюджетом, и ничего не маскируется — постоянный отказ по-прежнему
    роняет посев, и роняет его НА СВОЁМ шаге, а не через три шага на чужом.

    Второй законный исход — повтор по ИСХОДУ операции
    (`poll_operation_until_done(retry_from=…)`); он дороже (перезапускает саму мутацию) и
    применяется там, где чтение чужого ресурса кейсу недоступно.

    Свойство держит гейт по дереву `internal/repohygiene/artifactgates`
    `TestAsyncMutationDoesNotCarryAnUnwarmedPeerId`.
    """
    return _rya(Step(
        name=f"warm-{suffix}", method="GET",
        path=f"{base_path}/{{{{{id_var}}}}}", auth=auth,
        test_script=[*assert_status(200)]))


def retry_until_state(step: Step, converged_expr: str, budget: int = 25,
                      interval_ms: int = 500, retry_on=(403, 404)) -> Step:
    """Wrap the FIRST post-mutation / after-op VERIFY of the caller's OWN fresh resource
    in a bounded read-your-writes retry until the OBSERVED STATE has CONVERGED.

    Operation.done means the mutated resource is DURABLE (api-conventions.md), but the
    read that verifies a specific post-mutation FIELD VALUE can briefly be transient in
    TWO ways: (a) the owner-tuple authz gate returns 403/404 before the tuple
    materialises, OR (b) the read returns 200 but with a STALE field value before the
    write is reflected on the read path (e.g. a PATCH'd field / a status that settles a
    beat after the Operation is durable). retry_until_authorized covers only (a); this
    helper covers BOTH — it retries SELF while the response is a transient 403/404 OR a
    200 whose `converged_expr` (a JS boolean, TRUE once the expected state is observed)
    is still false, spacing attempts by ~interval_ms (busy-wait — newman fires
    setNextRequest before any setTimeout).

    Fail-OPEN at the budget: once spent, the wrapped step's real asserts run exactly once
    on the terminal response — a genuine never-converging state (a real product bug) STILL
    FAILS (never masked, never infinite). Use ONLY on a POSITIVE verify of the caller's
    OWN fresh resource — NEVER a negative / cross-account-deny / absent-id read (a poll
    there would mask a real deny). It is a strict superset of retry_until_authorized:
    converting an authz-only wrap to this never hides anything the authz-retry caught, it
    only ADDS the state-convergence wait. Unique step name (`-st<n>`) keeps the self-retry
    setNextRequest unambiguous (same discipline as retry_until_authorized / poll-op)."""
    retry_set = ",".join(str(c) for c in retry_on)
    guard = [
        "// bounded read-your-writes retry until the caller's OWN post-mutation state converges",
        "// (eventual-consistency): retries SELF on transient 403/404 OR a 200 whose state has",
        "// not yet caught up. Fail-open at budget -> the real asserts run once and FAIL if",
        "// still unconverged (a genuine never-converging state is never masked).",
        "if (pm.environment.get('_stRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_stRetryCount', '0');",
        "  pm.environment.set('_stRetryStarted', pm.info.requestName);",
        "}",
        "const _stc = parseInt(pm.environment.get('_stRetryCount') || '0', 10);",
        "let _converged = false;",
        f"try {{ _converged = !!({converged_expr}); }} catch (e) {{ _converged = false; }}",
        f"const _stTransient = [{retry_set}].includes(pm.response.code) || (pm.response.code === 200 && !_converged);",
        f"if (_stTransient && _stc < {budget}) {{",
        "  pm.environment.set('_stRetryCount', String(_stc + 1));",
        f"  const _std = Date.now(); while (Date.now() - _std < {interval_ms}) {{ /* state-convergence wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        *_budget_ledger("_stTransient", "_stc", budget),
        "pm.environment.unset('_stRetryCount');",
        "pm.environment.unset('_stRetryStarted');",
    ]
    _RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-st{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def retry_create_until_present(step: Step, budget: int = 25, interval_ms: int = 500) -> Step:
    """Wrap a CREATE/POST step that references a peer resource (e.g. a vpc Subnet /
    Address) just provisioned inline in the SAME case, in a bounded read-your-writes
    retry over the *cross-service* visibility window.

    A subnet/address created through vpc returns its Operation done (durable in vpc),
    but the peer read on nlb's side (nlb -> vpc SubnetService.Get during LB/Listener
    Create) is briefly stale under load: the sync create rejects with
    InvalidArgument/NotFound `"subnet <id> not found"` (code 3/5) before vpc's write is
    visible to the nlb peer client. Confirmed under `--jobs 4` parallel collections
    (ci-rep2: placement-coherence create-same-zone/-region + INTERNAL-REGIONAL cr-internal
    reddened on `subnet <id> not found`, while the identical provision->poll->create
    pattern in cross-resource happened to win the race and stayed green). This is a
    textbook cross-service read-your-writes lag -> the CLIENT retries the create; it is
    NOT a server barrier.

    Retries the SAME request (setNextRequest -> self) while the response says the peer
    reference did not resolve, spacing attempts ~interval_ms (busy-wait -- newman fires
    setNextRequest before any setTimeout). TWO discriminators, and the difference between
    them matters:

      * `ErrorInfo.reason == 'PEER_RESOURCE_MISSING'` in `details[]` -- the MACHINE
        discriminator api-conventions.md mandates ("клиент машинно различает линии по
        reason-token ... НЕ парся прозу message"). nlb emits it on the linked-address
        lane, whose prose is deliberately generic anti-oracle text
        ("Illegal argument addressId") and therefore carries no sniffable substring;
      * the legacy prose test (400/404 whose body message contains 'not found') -- the
        subnet lane still answers `"subnet <id> not found"` and is matched by prose only.

    Prose sniffing alone was NOT enough, and that is the whole reason the token branch
    exists: an address whose owner-tuple had not materialised answered with the generic
    text, matched nothing, and the wrapped step burned its single attempt ~0.5s after the
    address was created (CI 30919903252: cr-link / cr-ext-link / cr-linked, one attempt
    each, zero retries).

    A rejected create allocates NOTHING (sync reject before the Operation is even minted),
    so re-POSTing is leak-free and idempotent. budget*interval_ms bounds the wait
    (default 25*500ms = ~12.5s) -- fail-closed: on any other outcome the wrapped step's
    real test_script runs exactly once, and once the budget is spent it ALSO runs on the
    terminal rejection (a genuinely-absent peer still FAILS the real assertions --
    never masked, never infinite).

    Terminal rejections stay terminal by construction: nlb attaches the token ONLY to the
    peer-miss lane, so ownership/family/kind/placement mismatches (same generic prose,
    code 3, no token) are not retried and their negative cases keep failing fast.

    Use ONLY on a create whose peer dependency was provisioned earlier in the SAME case.
    Do NOT wrap negative fixture-absent creates (they legitimately expect the rejection).
    """
    guard = [
        "// bounded read-your-writes retry over the cross-service peer-visibility window",
        "// (vpc subnet/address just provisioned; nlb peer-read briefly stale). Retries",
        "// SELF only while the sync create says the PEER REFERENCE did not resolve:",
        "//   - ErrorInfo.reason == PEER_RESOURCE_MISSING  (machine discriminator; the",
        "//     linked-address lane's prose is generic anti-oracle text on purpose), or",
        "//   - a 400/404 whose message contains 'not found' (subnet lane prose).",
        "// A terminal mismatch carries NEITHER, so negatives still fail on attempt one.",
        "if (pm.environment.get('_crRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_crRetryCount', '0');",
        "  pm.environment.set('_crRetryStarted', pm.info.requestName);",
        "}",
        "const _crc = parseInt(pm.environment.get('_crRetryCount') || '0', 10);",
        "let _crNotFound = false;",
        "try {",
        "  const _crb = pm.response.json();",
        "  const _crPeerMiss = (_crb.details || []).some(d =>"
        " d && d.reason === 'PEER_RESOURCE_MISSING');",
        "  _crNotFound = _crPeerMiss || ([400, 404].includes(pm.response.code)"
        " && /not found/i.test(_crb.message || ''));",
        "} catch (e) {}",
        f"if (_crNotFound && _crc < {budget}) {{",
        "  pm.environment.set('_crRetryCount', String(_crc + 1));",
        f"  const _crd = Date.now(); while (Date.now() - _crd < {interval_ms}) {{ /* peer-visibility wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        *_budget_ledger("_crNotFound", "_crc", budget),
        "pm.environment.unset('_crRetryCount');",
        "pm.environment.unset('_crRetryStarted');",
    ]
    _RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-cr{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def retry_delete_until_released(step: Step, budget: int = 60, interval_ms: int = 500) -> Step:
    """Обернуть УБОРКУ подсети в ограниченный повтор на время освобождения адресов.

    Удаление балансировщика отвечает Operation `done` — предмет мутации долговечен. Но
    возврат выделенных адресов в свободный список идёт СЛЕДОМ и асинхронно (см.
    data-integrity.md §lease-recycle-on-delete), поэтому подсеть в узком окне ещё
    держит их и синхронно отвергает своё удаление: `FAILED_PRECONDITION` с текстом
    "Subnet has allocated internal addresses".

    Это ЗАКОННЫЙ временный отказ, а не дефект: кейс не про него, а про совпадение зон.
    Наблюдался в шарде nlb, где уборка v6-подсети падала ПОСЛЕ успешного удаления
    балансировщика (первое исполнение 200, второе — 400 с этим текстом).

    ПОЧЕМУ ЭТО НЕ ОСЛАБЛЕНИЕ. Повтор различается ПО СУЩЕСТВУ отказа, а не по коду:
    сообщение конкретно и производится ровно одной причиной. Бюджет ограничен, и по его
    исчерпании настоящее утверждение шага исполняется РОВНО ОДИН раз на терминальном
    ответе — подсеть, которая не освобождается никогда (настоящий дефект пула), всё
    равно уронит кейс. Ни маскировки, ни бесконечного ожидания.
    """
    guard = [
        "// SELF-повтор, пока подсеть держит ещё не возвращённые адреса: удаление",
        "// балансировщика долговечно, а возврат аренды в пул идёт следом и асинхронно.",
        "// Любой ДРУГОЙ отказ терминален и роняет шаг с первой попытки.",
        "if (pm.environment.get('_dlRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_dlRetryCount', '0');",
        "  pm.environment.set('_dlRetryStarted', pm.info.requestName);",
        "}",
        "const _dlc = parseInt(pm.environment.get('_dlRetryCount') || '0', 10);",
        "let _dlHeld = false;",
        "try {",
        "  const _dlb = pm.response.json();",
        "  _dlHeld = [400, 409].includes(pm.response.code)"
        " && /allocated internal addresses/i.test(_dlb.message || '');",
        "} catch (e) {}",
        f"if (_dlHeld && _dlc < {budget}) {{",
        "  pm.environment.set('_dlRetryCount', String(_dlc + 1));",
        f"  const _dld = Date.now(); while (Date.now() - _dld < {interval_ms}) {{ /* lease-release wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_dlRetryCount');",
        "pm.environment.unset('_dlRetryStarted');",
        # ДИАГНОСТИКА ТЕРМИНАЛЬНОГО ИСХОДА. Без неё падение уборки печатает только
        # «delete accepted: status 200» — то есть НЕ говорит ни кода, ни сообщения,
        # и разбор упирается в незнание того, что ответил сервер. Именно так и вышло
        # в шарде nlb: чинить пришлось бы гадая, временный это отказ (аренда ещё не
        # вернулась, окно мало) или иной по существу (тогда повтор вообще не при чём
        # и расширять бюджет — значит прятать другой дефект).
        #
        # Печатается ТОЛЬКО на не-200: на успехе шум не нужен. Это не утверждение и
        # ничего не смягчает — само строгое `delete accepted` остаётся ниже и падает
        # как падало.
        "if (pm.response.code !== 200) {",
        "  let _dlm = '';",
        "  try { _dlm = (pm.response.json().message || '').slice(0, 200); } catch (e) { _dlm = '<не JSON>'; }",
        "  console.log('[cleanup] ' + pm.info.requestName + ': код ' + pm.response.code"
        " + ', повторов ' + _dlc + ', сообщение: ' + _dlm);",
        "}",
    ]
    _RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-dl{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def conf_alreadyexists_block(prefix: str, create_path: str, name_template: str,
                              refusal: str,
                              body_extra: Optional[Dict] = None,
                              id_field_pattern: str = "Id") -> Case:
    """CONF: duplicate (project_id, name) on Create returns ALREADY_EXISTS verbatim text.

    NLB pattern: sync 409 on duplicate name (partial UNIQUE in DB). Worker also returns
    error envelope if INSERT race wins both syncs.

    ОТКАЗ ЗДЕСЬ РОВНО ОДИН, И ОН СИНХРОННЫЙ. Проверка дубликата имени стоит в use-case'е
    ДО создания строки операции (`assertNameUnique` в loadbalancer/create.go и
    targetgroup/create.go), поэтому второй Create получает `AlreadyExists` немедленно —
    409, а не конверт операции. Асинхронная полоса в этом кейсе недостижима: первый
    Create дождался `done`, значит его строка закоммичена, значит предпроверка второго её
    видит. Insert в worker'е остаётся атомарным backstop'ом на состязание двух
    ОДНОВРЕМЕННЫХ создателей — но серийный кейс такого состязания не ставит.

    Прежде шаг принимал `oneOf([200, 409])`. Первая редакция утверждала что-либо только в
    ветке 409, поэтому принятый дубль проходил кейс, названный «duplicate → ALREADY_EXISTS».
    Вторая редакция это починила, потребовав ошибку в операции (`must_fail`), — но
    сохранила приём двух исходов там, где код даёт один, и тем оставляла кейс глухим к
    настоящему регрессу: перенос проверки из синхронной части в worker'а сменил бы полосу
    отказа, а утверждение молчало бы. Теперь полоса заявлена: 409, код 6, текст.

    ТЕКСТ — ВЛАДЕЛЬЦА ЦЕЛИКОМ, А НЕ ОБЩАЯ ЧАСТЬ ТОНА (#1520). Заголовок кейса обещает
    «verbatim text», а утверждение до этой правки читало
    `message.toLowerCase()).to.include('already exists')` — общую часть, под которой
    проходит отказ ЛЮБОГО ресурса nlb. Тексты владельцев при этом РАЗНЫЕ по форме
    (`NetworkLoadBalancer with name %s already exists in project` против
    `TargetGroup '%s' already exists in project %s`), то есть подмену одного отказа
    другим кейс не различал ни при каком ответе — ровно то, что заголовок обещал ловить.

    `refusal` — текст владельца дословно, с `{name}` на месте имени (слот подставляется ЗАМЕНОЙ, не `str.format`:
    формат съел бы `{{…}}` подстановки окружения и превратил бы их в литерал). Обязателен и без
    умолчания: умолчание вернуло бы прежнюю слабость молча, а отказ генерации на
    незаданном тексте виден сразу и называет место."""
    body_extra = body_extra or {}
    return Case(
        id=f"{prefix}-CR-CONF-ALREADY-EXISTS",
        title=f"Create duplicate name → sync 409 ALREADY_EXISTS verbatim text",
        classes=["CONF", "NEG", "IDEM"], priority="P1",
        steps=[
            Step(name="create-first", method="POST", path=create_path,
                 body={"projectId": "{{_suiteProjectId}}", "name": name_template, **body_extra},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                              *save_from_response(
                                  "(j.metadata && Object.keys(j.metadata).filter(k => k.endsWith('Id') && k !== 'projectId').map(k => j.metadata[k])[0]) || ''",
                                  "createdId")]),
            # must_succeed: предмет кейса — отказ ВТОРОГО создания, и он имеет смысл
            # только если первое действительно создало строку. Сорванная подготовка
            # обязана краснеть здесь, под своим именем, а не превращать 409 ниже в
            # необъяснимый 200.
            poll_operation_until_done(must_succeed=True),
            Step(name="create-dup", method="POST", path=create_path,
                 body={"projectId": "{{_suiteProjectId}}", "name": name_template, **body_extra},
                 test_script=[
                     # Снимаем opId предыдущей (удавшейся) операции: синхронный отказ
                     # новой операции не создаёт, и поллинг ниже не должен опросить чужую.
                     "pm.environment.unset('opId');",
                     *assert_status(409),
                     *assert_grpc_code(6, "ALREADY_EXISTS"),
                     *assert_refusal_message(refusal.replace("{name}", name_template)),
                 ]),
            Step(name="cleanup-first", method="DELETE", path=f"{create_path}/{{{{createdId}}}}",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
        ],
    )


# ---------------------------------------------------------------------------
# Postman v2.1 serialization
# ---------------------------------------------------------------------------

def _auth_pre_script(auth: str) -> List[str]:
    if auth == "anonymous":
        return [
            "// AZD per-step: anonymous (strip Authorization header)",
            "pm.request.headers.remove('Authorization');",
        ]
    return [
        f"// AZD per-step: bearer from env '{js_comment(auth)}'",
        f"const __t = pm.environment.get({js_str(auth)}) || pm.variables.get({js_str(auth)}) || '';",
        "if (__t) {",
        "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});",
        "} else {",
        # HARNESS-CONFIG GUARD. An `auth="<envVar>"` step names a SUBJECT the case
        # is about ("a never-granted user must see nothing", "a foreign editor must
        # be denied"). If the fixture seed never wrote that variable, silently
        # dropping the header does not skip the check — it runs it as ANONYMOUS,
        # against a different subject entirely. The typical expectation (401/403)
        # then still holds, so the case passes FOR THE WRONG REASON and the subject
        # under test is never exercised. Missing subject = misconfigured harness:
        # FAIL naming the variable, THEN SKIP — the sanctioned shape, declared by
        # services/iam/tests/newman/scripts/exec-coverage.py (section STATIC BANS) and
        # implemented there as gen.py::require_env_url; this generator carries no such
        # helper, so the shape is written out here. Dropping the header and sending
        # anyway is NOT the
        # sanctioned shape: the step still travels, and every OTHER assertion it
        # carries is then scored against a principal the case never named.
        # `pm.execution.skipRequest()` skips exactly one request — its test script
        # does not run either — so nothing of this step is scored, while the
        # pre-request assertion above has ALREADY run and keeps the skip RECORDED as
        # a failure naming the variable, never a mute one. (`auth="anonymous"` is the
        # DELIBERATE anonymous case and takes the branch above — never affected.)
        f"  pm.test({js_str(f'harness config: {auth} is set (subject under test)')}, () => {{",
        "    pm.expect.fail(" + js_str(
            f"{auth} is not set — the authz-fixture seed "
            "(tests/authz-fixtures/setup.sh) did not provide this subject. Running the step "
            "anonymously would test a DIFFERENT principal and pass for the wrong reason.") + ");",
        "  });",
        "  pm.execution.skipRequest();",
        "}",
    ]


# ---------------------------------------------------------------------------
# Module discovery & main
# ---------------------------------------------------------------------------

def _reset_step_name_counters() -> None:
    """Reset every counter that feeds a STEP NAME, before loading a case module.

    A step name must be a function of the CASE, never of the environment. These
    counters live at module scope and only ever grow, so without this reset a
    name would depend on how many case modules were loaded before this one:
    `gen.py <module>` and a full `gen.py` would then emit DIFFERENT names for
    the same case. Two consequences, both observed: regenerating one module
    leaves a tree the full run does not produce (a one-module commit for #307
    needed a 670-line follow-up), and the step names used to diagnose a red run
    do not match between runs.

    Resetting is safe by construction: newman resolves setNextRequest by request
    name WITHIN the collection being run, and one case module produces exactly
    one collection — so uniqueness is only ever required within that scope.

    Held by internal/repohygiene TestGeneratedStepNamesDoNotDependOnHowManyModulesRan,
    which executes this generator in both modes and compares the bytes.
    """
    gen_shared._POLL_SEQ[0] = 0
    _RYA_SEQ[0] = 0
# ─────────────────────────────────────────────────────────────────────────────
# РЕШЕНИЯ НАБОРА, от которых зависит форма коллекции (#1379). Форму собирает
# общий слой; здесь объявлено ТОЛЬКО то, чем этот набор от остальных отличается.
# ─────────────────────────────────────────────────────────────────────────────

def _case_steps(case):
    """Конвейер шагов кейса: обёртка первого доступа к своему свежему ресурсу,
    затем утверждения об исходе удаления, сброс захваченного идентификатора
    операции и утверждение об исходе публикации идентификатора.

    Порядок значим и не переставляется: обёртка возвращает управление из скрипта,
    пока ждёт окна видимости, поэтому утверждения ставятся ПОСЛЕ неё.
    """
    return _assert_published_id_outcome(
        _reset_captured_operation_id(_assert_delete_operation_outcome(
            _wrap_own_fresh_reads(case.steps, _rya))))


# Опрос операции: тело общее (#1475), полоса ПЕРЕЗАПУСКА мутации — решение nlb.
#
# Общий слой не решает, когда набору перезапускать свою мутацию: сосед был виден
# синхронной предпроверке и всё ещё не виден собственному вызову воркера, поэтому
# отказ вышел НА ОПЕРАЦИИ, а не на ответе создания. Различается это по тексту
# отказа соседа — отказ по ёмкости читается иначе и НЕ повторяется никогда.
def poll_operation_until_done(
    fixture_ids: Optional[List[str]] = None,
    must_succeed: bool = False,
    must_fail: bool = False,
    retry_from: Optional[str] = None,
    retry_when: str = "not found",
    retry_budget: int = 8,
    retry_interval_ms: int = 700,
    auth: Optional[str] = None,
) -> Step:
    """Шаг опроса операции набора nlb — общее тело плюс его полоса перезапуска."""
    ids = list(fixture_ids or [])
    if must_fail and (ids or must_succeed):
        raise ValueError("poll_operation_until_done: must_fail contradicts must_succeed/fixture_ids")

    tail: List[str] = []
    if ids:
        # Страж фантома: предварительно выделенный идентификатор операции,
        # завершившейся ОШИБКОЙ, не должен доехать до последующего шага.
        tail += [
            "if (j.error) {",
            *[f"  pm.environment.unset({js_str(v)});" for v in ids],
            "}",
        ]
    if retry_from:
        tail += [
            "// bounded ASYNC-lane read-your-writes re-drive of the create step: the",
            "// peer was visible to the sync precheck but still stale to the worker's",
            "// own peer call, so the failure surfaced on the Operation, not on the",
            "// create response. Discriminated on the peer-miss text — a capacity",
            "// refusal reads differently and is NEVER retried.",
            "if (pm.environment.get('_opRedriveStarted') !== pm.info.requestName) {",
            "  pm.environment.set('_opRedriveCount', '0');",
            "  pm.environment.set('_opRedriveStarted', pm.info.requestName);",
            "}",
            "const _orc = parseInt(pm.environment.get('_opRedriveCount') || '0', 10);",
            "let _opTransient = false;",
            f"try {{ _opTransient = !!j.error && /{js_regex_src(retry_when, where='nlb/poll_operation_until_done/retry_when', flags='i')}/i.test(j.error.message || ''); }} catch (e) {{}}",
            f"if (_opTransient && _orc < {retry_budget}) {{",
            "  pm.environment.set('_opRedriveCount', String(_orc + 1));",
            # У самого шага создания счётчик синхронного повтора привязан к имени
            # запроса и сбрасывается только при смене имени; повторный вход в тот
            # же шаг застал бы бюджет исчерпанным. Снимаем — каждый перезапуск
            # получает своё полное окно.
            "  pm.environment.unset('_crRetryStarted');",
            "  pm.environment.unset('_crRetryCount');",
            "  pm.environment.unset('opId');",
            f"  const _ord = Date.now(); while (Date.now() - _ord < {retry_interval_ms}) {{ /* peer-visibility wait */ }}",
            f"  pm.execution.setNextRequest({js_str(retry_from)});",
            "  return;",
            "}",
            *_budget_ledger("_opTransient", "_orc", retry_budget),
            "pm.environment.unset('_opRedriveCount');",
            "pm.environment.unset('_opRedriveStarted');",
        ]
    if ids:
        tail += [
            "pm.test('fixture operation succeeded (no phantom resource id)', () => "
            "  pm.expect(j.error, JSON.stringify(j.error || {})).to.be.undefined);",
        ]
    elif must_succeed:
        tail += [
            "pm.test('operation succeeded', () => "
            "  pm.expect(j.error, JSON.stringify(j.error || {})).to.be.undefined);",
        ]

    return gen_shared.op_poll_step(
        Step, auth=auth, budget=30, interval_ms=500,
        must_fail=must_fail, tail=tail)


_EMIT = Emit(
    id_slug="kacho-nlb",
    display_name="kacho-nlb / newman",
    pre_global=lambda key: PRE_GLOBAL,
    steps_of=_case_steps,
    auth_pre=_auth_pre_script,
    # Своего признака слушателя у шага этого набора НЕТ: Internal*-поверхности он
    # не трогает, и все шаги идут на публичный адрес. Поле не заводится, пока нет
    # предмета, — ручка «на будущее» есть то же обещание без держателя.
)

# Помощники, доезжающие до модуля кейсов. Перечень — СЛОВАРЬ: он объявлен один
# раз и виден целиком, а не сорока строками `mod.X = X`, каждая из которых
# переживала снятие своего предмета молча.
_INJECTED = {
    "Step": Step,
    "Case": Case,
    "assert_status": assert_status,
    "assert_grpc_code": assert_grpc_code,
    "assert_refusal_message": assert_refusal_message,
    "assert_refusal_message_contains": assert_refusal_message_contains,
    "assert_unscoped_rejected": assert_unscoped_rejected,
    "assert_absent_id_rejected": assert_absent_id_rejected,
    "assert_field_violation": assert_field_violation,
    "assert_refused_sync_or_async": assert_refused_sync_or_async,
    "assert_operation_envelope": assert_operation_envelope,
    "save_from_response": save_from_response,
    "poll_operation_until_done": poll_operation_until_done,
    "retry_until_authorized": _rya,
    "warm_peer_fixture": warm_peer_fixture,
    "retry_until_present": _rup,
    "retry_until_state": retry_until_state,
    "retry_create_until_present": retry_create_until_present,
    "retry_delete_until_released": retry_delete_until_released,
    # Модель кейса у наборов СВОЯ, поэтому общий помощник получает её
    # аргументами, а не берёт из объемлющего модуля.
    "http_method_not_allowed_block": functools.partial(
        http_method_not_allowed_block, Case, Step),
    "conf_alreadyexists_block": conf_alreadyexists_block,
    "carve_cidr_pre": carve_cidr_pre,
}


_RUN = Run(
    root=ROOT,
    cases_dir=CASES_DIR,
    out_dir=OUT_DIR,
    scripts_dir=SCRIPTS_DIR,
    emit=_EMIT,
    case_cls=Case,
    injected=_INJECTED,
    before=_reset_step_name_counters,
    stem_dashes_to_underscores=False,
    per_collection=None,
    after_all=None,
)

# Точка входа — связывание, а не своё тело (#1474). Оркестрация одна на дерево;
# здесь набор связывает СВОИ решения. Имя `main` сохранено: его импортирует
# тонкая обёртка края (`from gen import main`).
main = functools.partial(generate, _RUN)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
