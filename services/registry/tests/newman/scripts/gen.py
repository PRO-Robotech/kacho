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

The generator is intentionally a near-mirror of kacho-vpc/tests/newman/scripts/gen.py
(KAC-VPC convention). NLB-specific helpers and the unified poll_operation_until_done
step live here so case modules only import the high-level Case / Step / helpers via
the module namespace (no `from gen import ...` because gen.py is loaded by path).
"""
from __future__ import annotations

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

from gen_shared import (  # noqa: E402  — импорт после провязки sys.path
    _MUTATION_METHODS,
    _OP_POLL_PATH,
    _PUB_SET_RE,
    _accepted_http_codes,
    _assert_delete_operation_outcome,
    _asserts_done,
    _asserts_outcome,
    _assigns_env_var,
    _body_text,
    _carries_assertion,
    _js_code_and_literals,
    _published_id_outcome_assert,
    _regex_literal_must_contain_the_whole_pattern,
    _reset_captured_operation_id,
    _strip_js_comments,
    assert_field_violation,
    js_comment,
    js_str,
)


_REGEX_FLAGS = "dgimsuvy"
_REGEX_PARSE_CACHE: Dict[tuple, str] = {}


def js_regex_src(pattern: str, *, where: str, flags: str = "") -> str:
    r"""ОБРАЗЕЦ вызывающего внутри литерала регулярного выражения (#1202).

    Здесь вызывающий даёт КОД, а не текст: знаки выражения значимы, и
    сериализатор строки (`js_str`) СМЕНИЛ БЫ СМЫСЛ — образец перестал бы
    совпадать. Поэтому образец возвращается ДОСЛОВНО, а исход у него другой:
    он проверяется ПРИ ГЕНЕРАЦИИ, и негодный роняет её С ИМЕНЕМ МЕСТА.

    ПОЧЕМУ ЭТО НЕ ВИДНО В ВЕРДИКТЕ. Негодный образец ломает не текст, а
    СИНТАКСИС порождаемого файла, которого автор значения не видит. newman
    пишет отказ разбора в `testScripts`, а НЕ в `assertions.failed`: шаг с
    неразобранным скриптом даёт НОЛЬ упавших утверждений и отчитывается зелёным
    по этой величине. Третья категория исхода, зачтённая в «прошло».

    ПРОВЕРОК ДВЕ, И ОДНОЙ НЕ ХВАТАЕТ — ЭТО ИЗМЕРЕНО, А НЕ ПРЕДПОЛОЖЕНО.
    `new Function("return /" + образец + "/;")` на образце
    `x/; process.exit(1); //` разбирается УСПЕШНО: литерал закрылся на первом же
    разделителе, а хвост стал КОДОМ. То есть проверка «разбирается ли» пропускает
    ровно ту подмену, ради которой заведена. Поэтому:

      1. ОХВАТ — литерал обязан вобрать ВЕСЬ образец. Это лексический разбор
         тела выражения, свой, без движка: спросить движок «где кончился
         литерал» можно только исполнив собранную строку, а исполнять чужой код
         в генераторе нельзя;
      2. РАЗБИРАЕМОСТЬ — грамматику судит НАСТОЯЩИЙ движок, тот самый, который
         будет исполнять литерал. Питонов `re` — другой язык: он не знает ни
         `\p{L}`, ни именованных групп JavaScript, и отвергал бы законное.

    Порядок именно такой: охват доказан ДО того, как строка попадает в node,
    поэтому подмена туда не доезжает by construction.

    ЧЕМ ДЕРЖИТСЯ. Проба
    `services/iam/tests/newman/scripts/js_regex_literal_test.py` — одна на все
    генераторы: перепись по дереву (каждая подстановка в литерал выражения несёт
    ЗАПИСАННЫЙ исход), инъекция негодным образцом (обязан упасть, назвав место) и
    положительный контроль законным (обязан пройти молча и остаться ДОСЛОВНЫМ).
    """
    if not isinstance(pattern, str) or pattern == "":
        raise ValueError(
            f"{where}: образец регулярного выражения пуст. Пустой литерал `//` —"
            f" это КОММЕНТАРИЙ JavaScript, а не выражение: остаток строки станет"
            f" прозой, и утверждение не исполнится вовсе")
    unknown = sorted({f for f in flags if f not in _REGEX_FLAGS})
    if unknown or len(set(flags)) != len(flags):
        raise ValueError(
            f"{where}: негодные флаги выражения {flags!r}"
            + (f" — неизвестны: {unknown}" if unknown else " — флаг повторён"))
    _regex_literal_must_contain_the_whole_pattern(pattern, where)
    _regex_must_parse_in_javascript(pattern, flags, where)
    return pattern


def _regex_must_parse_in_javascript(pattern: str, flags: str, where: str) -> None:
    """Грамматику судит движок, который литерал и будет исполнять."""
    key = (pattern, flags)
    verdict = _REGEX_PARSE_CACHE.get(key)
    if verdict is None:
        driver = ("const a=JSON.parse(process.argv[1]);"
                  "try{new Function('return /'+a.p+'/'+a.f+';');"
                  "process.stdout.write('OK');}"
                  "catch(e){process.stdout.write('ERR '+e.message);}")
        payload = json.dumps({"p": pattern, "f": flags})
        try:
            proc = subprocess.run(["node", "-e", driver, payload],
                                  capture_output=True, text=True, timeout=60)
        except (OSError, subprocess.SubprocessError) as exc:
            raise ValueError(
                f"{where}: образец проверить НЕЧЕМ — node не запускается ({exc})."
                f" Это «ноль прочитанного», а не «ноль находок»: генерация"
                f" отказывает, а не пропускает непроверенный образец") from None
        verdict = (proc.stdout.strip() if proc.returncode == 0
                   else f"ERR node {proc.returncode}: {proc.stderr[:200]}")
        _REGEX_PARSE_CACHE[key] = verdict
    if verdict != "OK":
        raise ValueError(
            f"{where}: образец /{pattern}/{flags} не разбирается как регулярное"
            f" выражение JavaScript — {verdict}")

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
_poll_seq = 0

# Monotonic counter for retry_until_authorized / retry_until_present wrapped steps.
# Each wrapped step gets a globally-unique `-rya<n>`/`-lst<n>` suffix so its
# setNextRequest(pm.info.requestName) self-retry always resolves to ITSELF (newman
# resolves a name to the FIRST item bearing it) — same hazard poll_operation_until_done
# avoids via unique poll-op-<n>. NOT reset per module: global uniqueness is the goal.
_RYA_SEQ = [0]


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
    # Слушатель на внутреннем CA (iam :9096 — server-TLS листовым сертификатом
    # внутреннего центра). Предмет пробы — ЧТО он отдаёт, а не чья цепочка доверия
    # у туннеля, поэтому проверка цепочки для такого шага снимается ЯВНО и только
    # там, где это записано кейсом.
    insecure_tls: bool = False


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
    "const __defaultJwt = pm.environment.get('jwtProjectEditorA') || pm.variables.get('jwtProjectEditorA') || '';",
    "if (__defaultJwt && !pm.request.headers.has('Authorization')) {",
    "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __defaultJwt});",
    "}",
    # REQUIRED-FIXTURE GUARD. Идентификаторы проекта/региона и токен по умолчанию
    # приходят из посева фикстур (tests/authz-fixtures → patch-env.py); в
    # закоммиченных окружениях они пусты by design. Пустое значение НЕ даёт пропуска
    # — оно уезжает в URL как `projectId=`, и запрос отвергается на авторизации до
    # всякой проверки предмета: суита краснеет каскадом, а причина («посев не
    # выполнялся») в отчёте не названа НИГДЕ. Поэтому отсутствие фикстуры — отказ,
    # названный по имени, на ПЕРВОМ же шаге прогона.
    "const __needed = ['baseUrl', 'existingProjectId', 'existingRegionId', 'jwtProjectEditorA'];",
    "const __missing = __needed.filter((k) => !(pm.environment.get(k) || pm.variables.get(k) || ''));",
    "if (__missing.length > 0) {",
    "  pm.test('FIXTURE REQUIRED: ' + __missing.join(', '), () => pm.expect.fail('the suite fixture "
    "seed did not run: ' + __missing.join(', ') + ' are empty. Seed via tests/authz-fixtures "
    "(prodrun.sh patches the suite env). An empty id is not a skip — it is sent as-is and the "
    "request is refused on authorization, so every later failure names the wrong cause.'));",
    # ...ЗАТЕМ ПРОПУСТИТЬ. Санкционированная форма — утвердить (назвав переменную) и
    # НЕ отправлять; она объявлена в exec-coverage.py (раздел STATIC BANS), эталон —
    # iam gen.py::require_env_url. Этот скрипт — pre-request КОРНЯ коллекции, то есть
    # исполняется перед КАЖДЫМ запросом: без пропуска весь набор уезжал бы с пустым
    # идентификатором, о котором в тексте отказа выше сказано прямо — «отправляется
    # как есть, и каждая последующая ошибка называет неверную причину». Утверждение
    # уже отработало, поэтому пропуск остаётся ЗАПИСАННЫМ падением с именем
    # переменной, а не немым.
    "  pm.execution.skipRequest();",
    "}",
    *_URL_VAR_GUARD,
]


# ---------------------------------------------------------------------------
# Reusable assertion snippets (pm.*) — same names as kacho-vpc
# ---------------------------------------------------------------------------

def assert_status(code: int) -> List[str]:
    return [
        f"pm.test({js_str(f'status {code}')}, () => pm.expect(pm.response.code).to.eql({code}));",
    ]


def assert_grpc_code(code: int, code_name: str) -> List[str]:
    return [
        f"pm.test({js_str(f'grpc code {code} ({code_name})')}, () => {{",
        "  const j = pm.response.json();",
        f"  pm.expect(j.code, JSON.stringify(j)).to.eql({code});",
        "});",
    ]


def assert_answered(label: str) -> List[str]:
    """Утверждать, что ответ ВООБЩЕ ПРИШЁЛ, прежде чем утверждать о нём хоть что-то.

    Запрос, умерший до завершения HTTP-обмена (DNS, отказ соединения, TLS,
    таймаут), всё равно доводит newman до test-скрипта — с ПУСТЫМ ответом, где
    `pm.response.code` равен `undefined`. Проба, начинающаяся с «если кода нет —
    выходим», записывает в этом случае ПРОЙДЕННОЕ утверждение для проверки,
    которой не было: «я не смог дозвониться» и «мне отказали» — разные находки, и
    доказательством служит только вторая.

    Поэтому первое, что утверждает проба, — что ей ответили. Не ответили ⇒
    красное, с именем шага; и все последующие утверждения тоже красные, потому
    что `undefined` не равен ожидаемому статусу. Это и есть замысел: проверка,
    которая не состоялась, не вправе отчитаться успехом.

    Канон и полный разбор класса — `services/iam/tests/newman/scripts/gen.py`
    (там же перепись восьми негативов ban #6, проживших так неизвестно сколько).
    """
    return [
        f"pm.test({js_str(f'{label}: запрос получил ОТВЕТ (пусто ⇒ транспорт, а не поведение)')}, () => {{",
        "  pm.expect(pm.response, 'ответа нет вовсе — сеть/TLS/таймаут, а не отказ сервиса')"
        ".to.not.be.undefined;",
        "  pm.expect(pm.response.code, 'HTTP-кода нет — обмен не завершился').to.be.a('number');",
        "});",
    ]


def require_env_url(var: str, path: str, why: str = "") -> List[str]:
    """Pre-request: направить запрос на `{{<var>}}` + path и УПАСТЬ, если var не задана.

    Две одинаковые с виду стражи — разные по существу:

      * страж ОПЕРАЦИИ (`if (!opId) skipRequest()`) — законный пропуск: мутацию
        отвергли намеренно, опрашивать нечего, ничего не потеряно;
      * страж ОКРУЖЕНИЯ (`if (!someBaseUrl) skipRequest()`) — СЛОМАННЫЙ ХАРНЕСС:
        проверка по-прежнему осмысленна и по-прежнему ожидается, просто прогонщик
        не передал адрес (`--env-var`).

    newman не оставляет от пропущенного запроса НИЧЕГО — ни утверждения, ни
    отказа, ни записи об исполнении, — поэтому второй род проходил тем, что
    никогда не выполнялся. Значит отсутствие переменной здесь УТВЕРЖДАЕТСЯ: суита
    краснеет с именем переменной вместо того, чтобы молча сжаться. Запрос после
    этого всё равно пропускается — отправлять его не на тот слушатель значило бы
    насыпать каскад путающих 404 поверх уже названного отказа.

    Путь резолвится ЯВНО (`pm.variables.replaceIn`) до присваивания: присваивание
    `pm.request.url` заменяет разобранный Url построенной здесь строкой, и
    полагаться на то, что newman подставит `{{…}}` внутрь только что
    перезаписанного адреса, значило бы полагаться на порядок, который здесь никем
    не закреплён. На пути без шаблонов это тождественная функция.
    """
    reason = f" — {why}" if why else ""
    return [
        f"// HARNESS-CONFIG GUARD — {js_comment(var)} передаёт прогонщик (--env-var).",
        "// Нет значения = харнесс сломан, а НЕ законный режим: сперва упасть, потом пропустить.",
        f"const __cfgUrl = pm.environment.get({js_str(var)}) || pm.variables.get({js_str(var)}) || '';",
        "if (__cfgUrl) {",
        f"  pm.request.url = __cfgUrl + pm.variables.replaceIn({js_str(path)});",
        "} else {",
        f"  pm.test({js_str(f'harness config: {var} задана{reason}')}, () => {{",
        "    pm.expect.fail(" + js_str(
            f"{var} не задана — прогонщик "
            "(deploy/scripts/newman-parallel.sh --env-var) её не передал. Шаг исполнен быть не может, "
            "а проверка, которая не может исполниться, НЕ ВПРАВЕ быть молча выброшенной.") + ");",
        "  });",
        "  pm.execution.skipRequest();",
        "}",
    ]


def _is_operation_id_var(env_var: str) -> bool:
    """Держит ли это имя идентификатор Operation — то есть читает ли его шаг опроса?

    Соглашение об именах едино на всё дерево: общий `opId` либо собственное имя
    кейса, оканчивающееся на `OpId`/`OperationId`. Идентификаторы РЕСУРСОВ под него
    не подпадают намеренно — их сохраняют один раз и читают много шагов спустя,
    тогда как устаревание опасно ровно у того, что потребляется следующим запросом.
    """
    return env_var == "opId" or env_var.endswith("OpId") or env_var.endswith("OperationId")


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


def save_operation_id() -> List[str]:
    """Capture THIS response's Operation id into `opId`, clearing any previous one first.

    STALE-opId GUARD. `save_from_response` writes only when the field is present, so a
    step that fails (404/403/400 — no `id` in the body) silently leaves the PREVIOUS
    step's `opId` in the environment. The `poll_operation_until_done()` that follows then
    polls an unrelated, already-done Operation and the capture step asserts against
    ANOTHER resource's payload — reporting a second, misleading failure that buries the
    real one.

    Observed in CI run 30135586348, REG-RD-F5-INHERIT-PRIVATE: `repo-create` 404'd for
    its whole retry budget, after which `poll-op-5` cheerfully polled the Operation left
    over from `delete-rnRegId` and `repo-capture` reported
    "new repo inherits visibility PRIVATE: expected undefined to deeply equal 'PRIVATE'"
    — a symptom with nothing to do with the cause.

    Every call site that captures an operation id means "the operation THIS response
    minted", so clearing first is strictly correct: no case legitimately wants to poll a
    previous step's operation.
    """
    return ["pm.environment.unset('opId');", *save_from_response("j.id", "opId")]


def assert_operation_envelope(prefix_regex: str = "^(nlb|tgr|lst)[a-z0-9]+$") -> List[str]:
    return [
        "pm.test('Operation envelope returned', () => {",
        "  const j = pm.response.json();",
        f"  pm.expect(j.id, 'operation.id').to.match(/{js_regex_src(prefix_regex, where='registry/assert_operation_envelope/prefix_regex')}/);",
        "  pm.expect(j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
    ]


def poll_operation_until_done() -> Step:
    """Reusable poll step with up-to-30 setNextRequest retries spaced ~500ms apart;
    guards on empty opId. Budget*interval ≈ 15s covers the async-op tail instead of
    hammering back-to-back (~15ms/poll) which never waits for the op (Koren #1).

    Each emitted step carries a unique name (`poll-op-<n>`) so the
    setNextRequest self-retry is unambiguous under `newman run <collection>`
    (see `_poll_seq` note): a duplicate "poll-op" name would make newman resolve
    the retry jump to the first such step and skip intervening folders."""
    global _poll_seq
    _poll_seq += 1
    return Step(
        name=f"poll-op-{_poll_seq}",
        method="GET",
        path="/operations/{{opId}}",
        test_script=[
            # Nothing to poll: the preceding step was refused synchronously and minted no
            # Operation. This is the ONLY case in which the step asserts nothing.
            "if (!pm.environment.get('opId')) {",
            "  pm.environment.unset('_pollCount');",
            "  return;",
            "}",
            # A REFUSED OPERATION READ IS RED. The early return used to sit ABOVE this line,
            # so the assertion could not fail by construction — only the responses it already
            # accepts ever reached it. Any non-200 on GET /operations/{id} (403 on somebody
            # else's operation, 404, 5xx) counted as a passed step while the outcome of the
            # mutation the step exists for stayed UNKNOWN. Unknown is not the same as fine.
            "pm.test('poll status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
            # The assertion above has already said no; leave before json() throws on an error
            # body — a script exception would hide the very failure it reports (and lands in
            # the fourth outcome category of assert-suites-green.sh, not in `failed`).
            "if (pm.response.code !== 200) {",
            "  pm.environment.unset('_pollCount');",
            "  return;",
            "}",
            "const j = pm.response.json();",
            "const pc = parseInt(pm.environment.get('_pollCount') || '0', 10);",
            # Poll budget raised 6→30 to match the Koren-1 baseline of the other
            # suites; with the ~500ms inter-poll delay below this covers ~15s.
            "if (!j.done && pc < 60) {",
            "  pm.environment.set('_pollCount', String(pc + 1));",
            # Real inter-poll delay (~500ms) between retries. newman runs test scripts
            # synchronously and fires setNextRequest before any setTimeout callback, so a
            # busy-wait is the only way to actually space out polls; 30*0.5s ≈ 15s then
            # covers the async-op tail (p95 3s / max 10s) instead of hammering back-to-back
            # (~15ms/poll via --delay-request 15) which never waits for the op (Koren #1).
            "  const _pd = Date.now(); while (Date.now() - _pd < 500) { /* inter-poll delay ~500ms (Koren #1) */ }",
            "  pm.execution.setNextRequest(pm.info.requestName);",
            "  return;",
            "}",
            "pm.environment.unset('_pollCount');",
            "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
            "if (j.error) pm.environment.set('lastOpError', JSON.stringify(j.error));",
            "else pm.environment.unset('lastOpError');",
            "if (j.response) pm.environment.set('lastOpResponse', JSON.stringify(j.response));",
        ],
    )


def retry_until_authorized(step: Step, budget: int = 80, interval_ms: int = 600,
                           retry_on=(403, 404)) -> Step:
    """Wrap the FIRST access of the caller's OWN just-created resource in a bounded
    read-your-writes retry over the owner-tuple materialization window.

    Kachō is eventually-consistent (api-conventions.md Operation.done = DURABLE, not
    downstream side-effect visibility). A registry/repository owner-tuple materialises
    via register-outbox → drainer → IAM RegisterResource → FGA reconciler (registry
    `internal/clients/iam/register_applier.go`). Until it is visible, the FIRST
    post-create Get/Update/Delete/Rename of the fresh resource can briefly return 403
    (PERMISSION_DENIED) or 404 (existence-hiding deny) at the per-repo v_* Check /
    gateway scope gate — a textbook read-your-writes lag; the CLIENT retries, it is
    NOT a server barrier.

    Retries the SAME request (setNextRequest -> self) while the response code is in
    `retry_on` (default 403/404), spacing attempts by ~interval_ms (busy-wait — newman
    fires setNextRequest before any setTimeout). budget*interval_ms bounds the wait
    (default 25*500ms ≈ 12s) — fail-closed: on any other code the wrapped step's real
    test_script runs exactly once, and once the budget is spent it ALSO runs on the
    terminal 403/404 (a genuine, non-converging deny still FAILS the real assertions —
    never masked, never infinite).

    Use ONLY on the first access of the caller's OWN fresh resource. Do NOT wrap
    negative / cross-account-deny / absent-id steps (a poll there would mask a real
    deny). The counter/started env-vars are request-name-scoped (step names are
    globally unique after serialization) so the loop never bleeds across cases/steps.

    ГРАНИЦА, КОТОРУЮ ЭТА ОБЁРТКА НЕ ПЕРЕХОДИТ (issue #351). Решение о повторе
    принимается по КОДУ ОТВЕТА шага, поэтому закрыта ровно одна полоса — СИНХРОННЫЙ
    отказ этого запроса (шлюз не разрешил цель проверки прав → 403; чтение скрытого
    ресурса → 404). У АСИНХРОННОЙ мутации отказ ВЛАДЕЛЬЦА чужого ресурса приезжает
    иначе: шаг отвечает `200` и конвертом `Operation`, а отказ лежит терминальной
    ошибкой ВНУТРИ операции и читается уже другим шагом — сюда он не попадает НИКОГДА.
    Обёртка на таком шаге не инертна (полосу шлюза она по-прежнему закрывает), но
    читать её как «окно видимости здесь закрыто» — ошибка: чужой свежий идентификатор
    обязан быть либо ПРОЧИТАН до мутации (`retry_until_authorized(GET <владелец>/<id>)`
    — форма, посаженная PR #350), либо прикрыт повтором по ИСХОДУ операции
    (`poll_operation_until_done(retry_from=…)`). Свойство держит по всему дереву гейт
    `internal/repohygiene/artifactgates`
    `TestAsyncMutationDoesNotCarryAnUnwarmedPeerId`.
    """
    # Полоса, по которой приходит окно видимости, задаётся МЕТОДОМ, а не вкусом
    # автора: у мутации отказ виден как 403, а у ЧТЕНИЯ он спрятан под 404
    # (hide-existence: текст отказа побайтово равен настоящему «не найдено», см.
    # `security.md`). Поэтому рукописное `retry_on=(403,)` на GET — обёртка,
    # которая не может сработать на том коде, который она увидит: форма есть,
    # содержания нет. Такое место в дереве нашлось (vpc1 `get-no-dhcp`: 404 на
    # первом же обращении, ретрай не сработал ни разу, шаг упал). Чинится по
    # построению здесь, а не перечнем в кейсах; 404, названный шагом законным
    # исходом, в retry_on не попадает вовсе — его отсекает вызывающий.
    # Ожидание НИКОГДА не включает код, который шаг сам объявил приемлемым
    # исходом: иначе проба пережидала бы ровно то, ради чего написана, и жгла бы
    # бюджет на успехе. Нормализация здесь, а не у вызывающих: рукописные
    # обёртки этого не делали (5 мест в дереве ждали заявленный ими же 404).
    _acc = _accepted_http_codes("\n".join(step.test_script))
    retry_on = tuple(c for c in retry_on if c not in _acc)
    if step.method == "GET" and 404 not in retry_on and 404 not in _acc:
        retry_on = tuple(retry_on) + (404,)
    # То же и у УДАЛЕНИЯ, и по той же причине — только код приходит не из
    # hide-existence чтения, а из утверждения по умолчанию: шаг удаления без
    # собственного утверждения получит при сериализации `delete accepted: status 200`
    # (_DELETE_ACCEPTED), и 404 для него — падение, а не законное «уже нет».
    # Держим это ЗДЕСЬ, а не у вызывающих: рукописное `retry_on=(403,)` на таком шаге
    # даёт обёртку, которая не ждёт единственный код, на котором шаг упадёт, — форма
    # ожидания есть, содержания нет. 404, названный шагом законным исходом, сюда
    # по-прежнему не попадает: его отсекает `_acc`.
    if (step.method == "DELETE" and not _carries_assertion(list(step.test_script))
            and 404 not in retry_on and 404 not in _acc):
        retry_on = tuple(retry_on) + (404,)
    if not retry_on:
        # Ждать нечего: все коды полосы видимости объявлены исходами. Обёртка
        # выродилась бы в петлю, которая не может сработать, — не ставим её.
        return step
    # ЗДЕСЬ ЖЕ — граница, которую этот отказ ставить обёртку НЕ ловит (issue #351).
    # Условие выше отсекает вырожденный случай «ждать нечего», и это верно только
    # для полосы, ВИДИМОЙ КОДОМ ОТВЕТА. У асинхронной мутации есть вторая полоса —
    # отказ ВЛАДЕЛЬЦА чужого ресурса внутри `Operation`, — и она не видна отсюда ни
    # при каком `retry_on`: обёртка ставится, читается как закрытое окно и им не
    # является. Молча этот случай не проходит: эмитируемый комментарий ниже называет
    # свою полосу вслух, а свойство по дереву держит гейт
    # `internal/repohygiene/artifactgates` TestAsyncMutationDoesNotCarryAnUnwarmedPeerId.
    retry_set = ",".join(str(c) for c in retry_on)
    guard = [
        "// bounded read-your-writes retry over the owner-tuple materialization window",
        "// (eventual-consistency); retries SELF only on 403/404 of own fresh resource.",
        "// ПОЛОСА — синхронный отказ ЭТОГО шага. У асинхронной мутации отказ ВЛАДЕЛЬЦА",
        "// приезжает внутри Operation и сюда НЕ попадает: нужен прогрев чтением.",
        "",
        "// ЖДАТЬ МОЖНО ТОЛЬКО ПРАВА, А НЕ ИМЯ. Если адрес шага собран из переменной,",
        "// которую предыдущий шаг не захватил, в пути стоит пустой сегмент либо сама",
        "// подстановка — и запрос спрашивает НЕ О РЕСУРСЕ. Окно видимости прав такой",
        "// адрес не наполнит никогда, поэтому повтор здесь — ожидание, которое не может",
        "// сработать: та самая форма без содержания, которую эта функция уже отказывается",
        "// строить выше (вырожденный retry_on). Хуже того, отказ по пустому адресу",
        "// приходит кодом 403 — он в полосе ожидания, и шаг выжигает ВЕСЬ бюджет.",
        "// Замер прогона 31951162447, часть registry: 1863 запроса из 3903 (48%) ушли",
        "// по пустому сегменту в 23 обёрнутых шагах — около 18 минут стенда на вопросы",
        "// ни о чём, при этом ни одно утверждение о продукте не проверялось.",
        "// Отказ здесь называет ПРЕДМЕТ (переменная не захвачена), а не следствие",
        "// («ожидал 200, получил 403») — падение уборки виновника не называет.",
        "const _rpath = ((pm.request.url && pm.request.url.path) || []).map(function (s) {",
        "  return String(s && s.value !== undefined ? s.value : s);",
        "});",
        "const _rblank = _rpath.some(function (v) { return v === '' || /\\{\\{[^}]*\\}\\}/.test(v); });",
        "if (_rblank) {",
        "  pm.environment.unset('_authRetryCount');",
        "  pm.environment.unset('_authRetryStarted');",
        "  pm.test('step addresses a captured variable', function () {",
        "    pm.expect.fail('адрес шага собран из НЕЗАХВАЧЕННОЙ переменной (/' + _rpath.join('/') +",
        "      '): предыдущий шаг не захватил её, повторять нечего — ждать можно право на " +
        "существующее имя, а не появление самого имени');",
        "  });",
        "}",
        "if (pm.environment.get('_authRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_authRetryCount', '0');",
        "  pm.environment.set('_authRetryStarted', pm.info.requestName);",
        "}",
        "const _arc = parseInt(pm.environment.get('_authRetryCount') || '0', 10);",
        f"if (!_rblank && [{retry_set}].includes(pm.response.code) && _arc < {budget}) {{",
        "  pm.environment.set('_authRetryCount', String(_arc + 1));",
        f"  const _ard = Date.now(); while (Date.now() - _ard < {interval_ms}) {{ /* owner-tuple materialization wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_authRetryCount');",
        "pm.environment.unset('_authRetryStarted');",
    ]
    _RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-rya{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def retry_until_present(step: Step, id_env_var: str, budget: int = 40,
                        interval_ms: int = 600) -> Step:
    """Bounded retry a LIST step until the caller's OWN fresh resource id appears in
    the returned array (read-your-writes over the list-authz visibility window —
    owner-tuple eventual-consistency). The list returns 200 with the id ABSENT until
    the tuple materialises, so retry_until_authorized (403/404) does not apply — we
    retry while the id is missing. Fail-open after budget: the real assertion then runs
    once and FAILS if still absent (never masked, never infinite). Use ONLY on a list
    of the caller's OWN just-created resource."""
    guard = [
        "// bounded read-your-writes retry until own fresh id is present in the list",
        "// (eventual-consistency); retries SELF while id absent.",
        "if (pm.environment.get('_lstRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_lstRetryCount', '0');",
        "  pm.environment.set('_lstRetryStarted', pm.info.requestName);",
        "}",
        "const _lrc = parseInt(pm.environment.get('_lstRetryCount') || '0', 10);",
        "let _present = false;",
        "try { const _arr = Object.values(pm.response.json()).find(v => Array.isArray(v)) || [];"
        " _present = _arr.map(x => x.id).includes(pm.environment.get('" + id_env_var + "')); } catch (e) {}",
        f"if (pm.response.code === 200 && !_present && _lrc < {budget}) {{",
        "  pm.environment.set('_lstRetryCount', String(_lrc + 1));",
        f"  const _lrd = Date.now(); while (Date.now() - _lrd < {interval_ms}) {{ /* list-visibility wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_lstRetryCount');",
        "pm.environment.unset('_lstRetryStarted');",
    ]
    _RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-lst{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def http_method_not_allowed_block(prefix: str, base_path: str) -> List[Case]:
    """HTTP method semantics: PUT/DELETE on collection endpoint → not-allowed status."""
    return [
        Case(
            id=f"{prefix}-METHOD-PUT-NOT-ALLOWED",
            title="PUT on List endpoint → 403/404/405/501",
            classes=["VAL", "NEG"], priority="P3",
            steps=[Step(name="put-list", method="PUT", path=base_path,
                        body={"projectId": "{{_suiteProjectId}}"},
                        test_script=["pm.test('not allowed (403/404/405/501)', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));"])],
        ),
        Case(
            id=f"{prefix}-METHOD-DELETE-LIST",
            title="DELETE on List endpoint (no id) → 403/404/405/501",
            classes=["VAL", "NEG"], priority="P3",
            steps=[Step(name="del-list", method="DELETE", path=base_path,
                        test_script=["pm.test('not allowed (403/404/405/501)', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));"])],
        ),
    ]


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


# Утверждение о том, что удаление ПРИНЯТО. Ровно одно и однозначное: `oneOf`
# со взаимоисключающими исходами утверждением не является (testing.md).
_DELETE_ACCEPTED = [
    "// УТВЕРЖДЕНИЕ ПО УМОЛЧАНИЮ для шага удаления: без него шаг зеленел бы и на",
    "// отказе, а следующий опрос уехал бы на opId предыдущей операции.",
    "pm.test('delete accepted: status 200', () => "
    "pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
]


_PUB_BIND_RE = re.compile(r"\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=")
# Объявление БЕЗ инициализатора (`let j;`) и присваивание отдельным оператором
# (`j = pm.response.json()`). Форма `let j; try { j = pm.response.json(); } catch (e)
# { j = null; }` — самая частая запись безопасного разбора тела в этом корпусе, и
# `_PUB_BIND_RE` её не узнаёт вовсе: она требует `=` В ОБЪЯВЛЕНИИ. Пока узнавалось
# только объявление-с-инициализатором, цепочка происхождения рвалась на первом
# звене, и проход не видел ни публикации, ни всего, что от этого имени
# производилось дальше. Тот же распознаватель и по той же причине расширен в гейте
# `internal/repohygiene/artifactgates` — проход и гейт обязаны считать ОДНО И ТО ЖЕ,
# иначе они разойдутся на первом же шаге, записанном не по канону.
_PUB_DECL_RE = re.compile(r"\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*[;,]")
# Имя непосредственно перед `=`: `a.b = c` отсекается предшествующей точкой,
# `==`/`===`/`=>` — заглядыванием вперёд, `+=`/`!==`/`>=` — тем, что между именем и
# `=` у них стоит оператор.
_PUB_ASSIGN_RE = re.compile(r"(?:^|[^.\w$])([A-Za-z_$][\w$]*)\s*=(?![=>])")
# Слова, за которыми `имя =` связыванием значения не является. Перечень закрытый:
# «что-нибудь похожее на ключевое слово» отсекло бы имя, начинающееся так же.
_PUB_RESERVED = frozenset((
    "if", "for", "while", "switch", "return", "function", "const", "let", "var",
    "catch", "typeof", "new", "delete", "void", "in", "of",
))


def _published_resource_vars(src: str, op_var: str) -> List[str]:
    """Имена окружения, которым шаг присваивает значение ИЗ `metadata` операции.

    Одного вхождения слова `metadata` в скрипт мало: один и тот же шаг захватывает
    и идентификатор ОПЕРАЦИИ (`j.id`), и идентификатор РЕСУРСА
    (`j.metadata.<res>Id`), причём оба — через локальную `const v` в СВОЁМ блоке
    (`save_from_response`). Без учёта области видимости ручка операции сошла бы за
    координату ресурса, и проход дописывал бы защиту там, где публиковать нечего.

    `op_var` — имя, которым цепочка адресует саму операцию (берётся из адреса
    опроса). Оно исключается: это ручка, а не координата ресурса.
    """
    code, lits = _js_code_and_literals(src)
    depth, cur = [0] * (len(code) + 1), 0
    for i, ch in enumerate(code):
        depth[i] = cur
        if ch == "{":
            cur += 1
        elif ch == "}":
            cur -= 1
    depth[len(code)] = cur

    binds = []  # (offset, depth, name, derived)

    def visible(at: int, expr: str) -> bool:
        for off, d, name, derived in binds:
            if off >= at or not derived:
                continue
            if any(depth[k] < d for k in range(off, at)):
                continue  # блок объявления уже закрыт — имя не видно
            if re.search(r"\b" + re.escape(name) + r"\b", expr):
                return True
        return False

    # ОБЛАСТЬ ВИДИМОСТИ БЕРЁТСЯ У ОБЪЯВЛЕНИЯ, А НЕ У ПРИСВАИВАНИЯ. `let j;` стоит на
    # верхнем уровне скрипта, а значение ему присваивают внутри `try { … }` — то
    # есть глубже. Считать глубиной связывания глубину присваивания значило бы
    # закрывать имя вместе с блоком `try`, и все последующие чтения `j` оказались бы
    # «вне области» — ровно наоборот тому, как это работает в JavaScript.
    decl_depth = {}
    for m in _PUB_DECL_RE.finditer(code):
        decl_depth[m.group(1)] = depth[m.start()]
    for m in _PUB_BIND_RE.finditer(code):
        decl_depth[m.group(1)] = depth[m.start()]

    sites = []  # (offset, depth, name, expr_at)
    for m in _PUB_BIND_RE.finditer(code):
        sites.append((m.start(), depth[m.start()], m.group(1), m.end()))
    for m in _PUB_ASSIGN_RE.finditer(code):
        name = m.group(1)
        if name in _PUB_RESERVED:
            continue
        at = m.start(1)
        # Объявление-с-инициализатором уже учтено выше: `const v = …` матчится и
        # сюда. Считать его дважды безвредно для вердикта, но смещение связывания
        # разошлось бы на длину `const `, а от смещения зависит проверка
        # «объявлено ДО использования».
        head = code[:at].rstrip()
        if head.endswith(("const", "let", "var")) and len(head) < at:
            tail = "const" if head.endswith("const") else ("let" if head.endswith("let") else "var")
            j = len(head) - len(tail)
            if j == 0 or not (code[j - 1].isalnum() or code[j - 1] in "_$"):
                continue
        sites.append((at, decl_depth.get(name, 0), name, m.end()))
    sites.sort(key=lambda s: s[0])

    for off, d, name, expr_at in sites:
        semi = code.find(";", expr_at)
        expr = code[expr_at:semi if semi >= 0 else len(code)]
        binds.append((off, d, name, "metadata" in expr or visible(off, expr)))

    def arg_tail(pos: int) -> str:
        lvl = 1
        for k in range(pos, len(code)):
            if code[k] == "(":
                lvl += 1
            elif code[k] == ")":
                lvl -= 1
                if lvl == 0:
                    return code[pos:k]
        return code[pos:]

    names: List[str] = []
    for m in _PUB_SET_RE.finditer(code):
        name = lits[int(m.group(1))]
        if not name or name == op_var or name in names:
            continue
        expr = arg_tail(m.end())
        if "metadata" in expr or visible(m.start(), expr):
            names.append(name)
    return sorted(names)


def _assert_published_id_outcome(steps: List[Step]) -> List[Step]:
    """Опубликовал идентификатор ресурса из metadata — назови ИСХОД операции.

    Операция несёт предвыделенный идентификатор в `metadata` ДАЖЕ когда завершилась
    ошибкой: он чеканится до того, как отработает воркер. Шаг, сохранивший
    `metadata.<res>Id`, и опрос, утверждающий только `done`, вместе публикуют
    координату ресурса, которого нет, — `done` у провалившейся операции такой же
    `true`. Дальше по этой координате идут привязки прав (край отвечает успехом) и
    межсервисные запросы (владелец отвечает «не найдено»), и падает не тот шаг,
    который ошибся: симптом к причине отношения не имеет.

    Ставится ПО СВОЙСТВУ шага, а не по перечню имён: ручная пометка неотличима от
    решения не помечать, и класс возвращался ровно так — закрыт в одном кейсе,
    через несколько часов проявился в соседнем.

    ОПРОС ПРИНАДЛЕЖИТ ТОМУ, ЧЬЮ ОПЕРАЦИЮ ЧИТАЕТ, а не просто предыдущей мутации.
    Между созданием и его опросом законно стоит другая мутация — отмена той же
    операции (`/operations/{{opId}}:cancel`), — и правило «последняя мутация»
    отдало бы опрос ей, оставив создание без единого читателя исхода. Поэтому
    опрос отходит ближайшей предшествующей мутации, которая ПРИСВАИВАЕТ имя,
    стоящее в адресе опроса; если такой нет — ближайшей предшествующей мутации.

    Вопрос задаётся ОДИН НА ЦЕПОЧКУ: если исход называет сам шаг мутации (так
    устроена синхронная операция без опроса вовсе) или ЛЮБОЙ её опрос — успехом
    или отказом, — дописывать нечего. Иначе проход дописал бы «операция успешна»
    кейсу, чей ПРЕДМЕТ — отказ операции, и кейс утверждал бы обе взаимоисключающие
    вещи разом.

    Держит свойство по дереву гейт `internal/repohygiene`
    `TestPublishedResourceIdIsGuardedByOperationOutcome` — он читает
    СГЕНЕРИРОВАННЫЕ коллекции, поэтому правка мимо генератора его не обходит.
    """
    out = list(steps)
    muts = [i for i, st in enumerate(out)
            if st.method in _MUTATION_METHODS and not _OP_POLL_PATH.search(st.path)]
    chains = {i: [] for i in muts}
    for idx, st in enumerate(out):
        if st.method != "GET":
            continue
        m = _OP_POLL_PATH.search(st.path)
        if not m:
            continue
        owner = None
        for i in muts:
            if i >= idx:
                break
            if _assigns_env_var("\n".join(out[i].test_script), m.group(1)):
                owner = i
        if owner is None:
            owner = max((i for i in muts if i < idx), default=None)
        if owner is not None:
            chains[owner].append(idx)
    for sidx, polls in chains.items():
        if not polls:
            continue  # операцию никто не опрашивает — вписать утверждение некуда
        op_var = "opId"
        for k in polls:
            m = _OP_POLL_PATH.search(out[k].path)
            if m:
                op_var = m.group(1)
        own = "\n".join(out[sidx].test_script)
        names = _published_resource_vars(own, op_var)
        if not names:
            continue
        if _asserts_outcome(_strip_js_comments(own)):
            # Исход назван самой мутацией — так устроена СИНХРОННАЯ операция
            # (`done:true` в ответе, опрашивать нечего). Утверждать второй раз
            # нечего, но снять опубликованное имя на ошибке всё равно надо.
            out[sidx] = replace(out[sidx], test_script=list(out[sidx].test_script)
                                + _published_id_outcome_assert(names, False, need_assert=False))
            continue
        code = "\n".join(_strip_js_comments("\n".join(out[k].test_script)) for k in polls)
        if _asserts_outcome(code):
            continue
        k = polls[0]
        out[k] = replace(out[k], test_script=list(out[k].test_script)
                         + _published_id_outcome_assert(names, not _asserts_done(code)))
    return out


def step_to_postman(step: Step) -> Dict:
    item: Dict = {
        "name": step.name,
        "request": {
            "method": step.method,
            "header": [{"key": "Content-Type", "value": "application/json"}],
            "url": {
                "raw": "{{baseUrl}}" + step.path,
                "host": ["{{baseUrl}}"],
                "path": [p for p in step.path.strip("/").split("/") if p],
            },
        },
    }
    if step.insecure_tls:
        item["protocolProfileBehavior"] = {"strictSSL": False}
    if step.body is not None:
        item["request"]["body"] = {
            "mode": "raw",
            "raw": json.dumps(step.body, ensure_ascii=False),
            "options": {"raw": {"language": "json"}},
        }
    pre = list(step.pre_script)
    if step.auth is not None:
        pre = _auth_pre_script(step.auth) + pre
    events = []
    if pre:
        events.append({"listen": "prerequest", "script": {"type": "text/javascript", "exec": pre}})
    # Шаг удаления без собственного утверждения получает утверждение по умолчанию
    # (см. _DELETE_ACCEPTED выше). Ставится в КОНЕЦ — после обёртки ожидания.
    test_exec = list(step.test_script)
    if step.method == "DELETE" and not _carries_assertion(test_exec):
        test_exec = test_exec + _DELETE_ACCEPTED
    if test_exec:
        events.append({"listen": "test", "script": {"type": "text/javascript", "exec": test_exec}})
    if events:
        item["event"] = events
    return item


# --- Класс: первый доступ к СВОЕМУ свежему ресурсу без ограниченного ретрая ---
#
# Обёртка `retry_until_authorized` ставилась ВРУЧНУЮ, поэтому её пропуск был
# неотличим от решения не оборачивать. Замер по артефактам прогона CI
# 31002239590 (8 суит, 82 отчёта, 15648 утверждений, 151 падение): из 68
# падений полосы видимости (403/404) **42** пришлись на шаги, у которых обёртки
# не было ВОВСЕ, при том что соседние шаги той же формы в тех же кейсах
# обёрнуты — то есть пропуск, а не замысел.
#
# Предикат ставит обёртку ПО СВОЙСТВУ шага, а не по списку имён, и потому
# закрывает класс, а не перечисленные экземпляры. Четыре условия — все
# обязательны:
#   1. шаг УТВЕРЖДАЕТ УСПЕХ — то есть 200 входит в набор исходов, которые он
#      принимает, а 403 в него НЕ входит. Набор читается и из `to.eql(200)`, и
#      из `to.be.oneOf([...])` над `pm.response.code`: уборка своего свежего
#      ресурса сплошь записана вторым способом («удалилось 200 ЛИБО состояние
#      не позволило 400»), и пока предикат смотрел на буквальное `to.eql(200)`,
#      такие шаги были ему невидимы ПО ПОСТРОЕНИЮ — в суите vpc это 77 записей
#      из 93. Шаг, принимающий 403 своим исходом (authz-first толерантность
#      негатива), не оборачивается никогда: там отказ и есть проверяемое, а
#      ретрай маскировал бы его (`testing.md` — «НЕ оборачивать: negatives,
#      cross-account deny»). Пережидаются ТОЛЬКО те коды полосы видимости,
#      которых шаг исходом не заявлял: если 404 заявлен («уже нет»), ретрай
#      идёт лишь по 403, иначе обёртка жгла бы бюджет на принятом исходе;
#   2. адрес шага ссылается на переменную, РОЖДЁННУЮ РАНЕЕ В ЭТОМ ЖЕ КЕЙСЕ
#      (её published предыдущий шаг). Чужой/заранее известный id предикату
#      неизвестен — значит absent-id-негативы остаются строгими;
#   3. у шага НЕТ собственной петли (`setNextRequest`) — поллер операции ведёт
#      свою и переименован под себя; вторая петля сломала бы резолв имени;
#   4. шаг ещё не обёрнут вручную (идемпотентность).
#
# ЧТО ЗДЕСЬ ДОКАЗАНО, А ЧТО НЕТ (`PRO-Robotech/kacho#1277`).
#
# ПРОВЯЗАННОСТЬ предиката в ЭТОМ генераторе держит гейт дерева
# `internal/repohygiene/artifactgates/newmanfreshreadwrap_test.go`: генератор, у
# которого есть `retry_until_authorized`, но нет предиката в сериализации, — его
# находка.
#
# РАБОТОСПОСОБНОСТЬ предиката — инъекция настоящего пропуска и законные близнецы,
# на которых он обязан молчать, — доказана ТОЛЬКО для копии vpc:
# `services/vpc/tests/newman/scripts/selftest_autowrap.py` (её зовут `ci.yaml` и
# `e2e-newman.yml`). ЭТА копия ею НЕ ПОКРЫТА: своей самопроверки у набора нет.
# Правя предикат здесь, проверяй его сам — обещания, что правка проверена в обе
# стороны, тут нет.
#
# Прежняя редакция обещала обратное: она называла `scripts/selftest_autowrap.py`,
# то есть путь относительно ЭТОГО набора, где такого файла нет. Утверждение было
# не о стиле, а о ДОКАЗАННОСТИ, поэтому читатель, правящий предикат, не стал бы
# проверять сам. Держит правду гейт
# `internal/repohygiene/newmanproofclaim_test.go`: утверждение о доказательстве
# обязано называть координату, которая резолвится.
_FRESH_VAR_SET_RE = re.compile(
    r"pm\.(?:environment|collectionVariables|globals)\.set\(\s*['\"]([A-Za-z_][A-Za-z0-9_]*)['\"]"
)
_VAR_REF_RE = re.compile(r"\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}")


def _wrap_own_fresh_reads(steps: List[Step], rename: bool = True) -> List[Step]:
    """Обернуть положительные первые обращения к своему свежему ресурсу.

    Возвращает НОВЫЙ список шагов; исходные Step не мутируются.

    `rename=False` — для генератора, который САМ делает имена шагов глобально
    уникальными при сериализации (iam: `<case-id> :: <шаг>`) и переписывает
    буквальные переходы `setNextRequest('<сосед>')` по БАЗОВЫМ именам. Там
    переименование обёрткой сломало бы резолв такого перехода, а нужды в нём
    нет: `pm.info.requestName` резолвится в итоговое имя, уже уникальное.
    """
    fresh: set = set()
    out: List[Step] = []
    for st in steps:
        body = "\n".join(st.test_script)
        self_looped = "setNextRequest" in body
        already = "_authRetryCount" in body or "_absRetryCount" in body
        accepted = _accepted_http_codes(body) if st.test_script else set()
        # Шаг УДАЛЕНИЯ без собственного утверждения ПОЛУЧИТ утверждение по
        # умолчанию — `delete accepted: status 200` (_DELETE_ACCEPTED) — и получит его
        # ПОСЛЕ этой обёртки, при сериализации. Решать, чего ждать, надо по
        # скрипту, который шаг ПОНЕСЁТ, а не по тому, который он несёт сейчас:
        # иначе решение принимается на предпосылке, которую следующий же проход
        # отменяет, и обёртка не ждёт тот единственный код, на котором шаг упадёт.
        if st.method == "DELETE" and not _carries_assertion(list(st.test_script)):
            accepted = accepted | {200}
        # Ждать можно ТОЛЬКО код, который шаг исходом не заявлял, — тогда
        # ожидание ничего не маскирует по построению: если 403/404 названы
        # приемлемым исходом, шаг про них и спрашивает, и ретрай там запрещён.
        # Требования «шаг обязан ждать 200» больше нет: отрицательная проба
        # СВОЕГО СВЕЖЕГО ресурса («такой CIDR отвергается») тоже упирается в
        # окно видимости и получает 403 вместо ожидаемого 400 — то есть падает
        # не по своему предмету. Чужой аккаунт, посеянный и несуществующий id
        # под правило не подпадают: они не рождены в этом кейсе.
        # Шаг без единого утверждения и НЕ удаляющий ждёт только 403: у такого
        # чтения 404 часто законное «уже нет», и жечь на нём бюджет незачем. У удаления
        # это больше не так: там 404 роняет утверждение по умолчанию.
        retry_on = tuple(c for c in (403, 404) if c not in accepted)
        if not accepted:
            retry_on = (403,)
        if st.test_script and not self_looped and not already and 403 not in accepted and retry_on:
            # Цель проверки прав называется адресом ЛИБО ПОЛЕМ ЗАПРОСА: край берёт
            # объект из `scope_extractor.from_request_field` каталога прав, и у
            # создания вложенного ресурса адрес коллекционный, а свежий родитель
            # стоит в теле. Условие, читавшее только `st.path`, такой шаг не видело
            # ПО ПОСТРОЕНИЮ — и это не мелочь: пропущенный шаг обычно СОЗДАЁТ
            # фикстуру, на которой стоит предмет кейса, поэтому его отказ уезжает
            # не в «фикстура не создалась», а в красное утверждение о предмете
            # (наблюдалось на удалении группы целей: ссылки не возникло, продукт
            # верно разрешил удаление, а кейс отчитался о сломанной ссылочной
            # целостности). Ждать на СОСЕДНЕЙ полосе нельзя: чтение родителя
            # гейтится одним отношением, создание вложенного — другим.
            if set(_VAR_REF_RE.findall(st.path + _body_text(st))) & fresh:
                w = retry_until_authorized(st, retry_on=retry_on)
                st = replace(w, name=st.name) if not rename else w
                body = "\n".join(st.test_script)
        for name in _FRESH_VAR_SET_RE.findall(body):
            fresh.add(name)
        out.append(st)
    return out


def case_to_postman(case: Case) -> Dict:
    tags = [f"class:{c}" for c in case.classes] + [f"priority:{case.priority}"]
    return {
        "name": f"{case.id} — {case.title}",
        "description": " | ".join(tags),
        "item": [step_to_postman(s) for s in
                 _assert_published_id_outcome(
                     _reset_captured_operation_id(_assert_delete_operation_outcome(_wrap_own_fresh_reads(case.steps))))],
    }


def build_collection(service: str, cases: List[Case]) -> Dict:
    return {
        "info": {
            # Deterministic _postman_id (UUIDv5 over the collection name) so a
            # regeneration with no source change produces no diff. A random id
            # here made every regeneration dirty every collection, which meant
            # "generated matches source" could never be checked and a real drift
            # had nowhere to show. Postman only needs this to be stable+unique.
            "_postman_id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"kacho-registry/newman/{service}")),
            "name": f"kacho-registry / newman / {service}",
            "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
        },
        "event": [
            {
                "listen": "prerequest",
                "script": {"type": "text/javascript", "exec": PRE_GLOBAL},
            },
        ],
        "item": [case_to_postman(c) for c in cases],
        "variable": [],
    }


# ---------------------------------------------------------------------------
# Module discovery & main
# ---------------------------------------------------------------------------

def _reset_step_name_counters() -> None:
    """Reset every counter that feeds a STEP NAME, before loading a case module.

    A step name must be a function of the CASE, never of the environment. These
    counters live at module scope and only ever grow, so without this reset a
    name would depend on how many case modules were loaded before this one, and
    `gen.py <module>` would emit different names than a full `gen.py` for the
    same case — leaving a tree the full run does not produce, and step names
    that do not match between runs when a red run is being diagnosed.

    Resetting is safe by construction: newman resolves setNextRequest by request
    name WITHIN the collection being run, and one case module produces exactly
    one collection — so uniqueness is only ever required within that scope.

    Held by internal/repohygiene TestGeneratedStepNamesDoNotDependOnHowManyModulesRan.
    """
    global _poll_seq
    _poll_seq = 0
    _RYA_SEQ[0] = 0


def load_cases_module(path: Path):
    _reset_step_name_counters()
    spec = importlib.util.spec_from_file_location(path.stem, path)
    mod = importlib.util.module_from_spec(spec)
    # Inject helpers into the module's namespace so case files don't import gen.
    mod.Step = Step
    mod.Case = Case
    mod.assert_status = assert_status
    mod.assert_grpc_code = assert_grpc_code
    mod.assert_field_violation = assert_field_violation
    mod.assert_answered = assert_answered
    mod.require_env_url = require_env_url
    mod.assert_operation_envelope = assert_operation_envelope
    mod.save_from_response = save_from_response
    mod.save_operation_id = save_operation_id
    mod.poll_operation_until_done = poll_operation_until_done
    mod.retry_until_authorized = retry_until_authorized
    mod.retry_until_present = retry_until_present
    mod.http_method_not_allowed_block = http_method_not_allowed_block
    # Помощники экранирования — тем же впрыском (#1209): декларация тоже
    # порождает JavaScript, и вторая копия предиката разошлась бы с первой молча.
    mod.js_regex_src = js_regex_src
    spec.loader.exec_module(mod)
    return mod


def _check_duplicate_ids() -> int:
    seen: Dict[str, str] = {}
    dups: List[str] = []
    for f in sorted(CASES_DIR.glob("*.py")):
        if f.name.startswith("_"):
            continue
        mod = load_cases_module(f)
        for c in getattr(mod, "CASES", []):
            if c.id in seen:
                dups.append(f"  - {c.id!r}: {seen[c.id]} and {f.name}")
            else:
                seen[c.id] = f.name
    if dups:
        sys.stderr.write("gen: FAIL — duplicate case-id (must be unique across all modules):\n")
        sys.stderr.write("\n".join(dups) + "\n")
        return 1
    return 0


def main(argv: List[str]) -> int:
    args = argv[1:]
    if "--validate" in args:
        import runpy
        sys.argv = [str(SCRIPTS_DIR / "validate-cases.py")]
        runpy.run_path(str(SCRIPTS_DIR / "validate-cases.py"), run_name="__main__")
        return 0  # validate-cases.py calls sys.exit itself

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    want = set(args)
    found = sorted(f for f in CASES_DIR.glob("*.py") if not f.name.startswith("_"))
    if not found:
        print(f"no case files in {CASES_DIR}")
        return 1
    if _check_duplicate_ids() != 0:
        return 1
    total = 0
    for f in found:
        svc = f.stem
        if want and svc not in want:
            continue
        mod = load_cases_module(f)
        cases = getattr(mod, "CASES", [])
        col = build_collection(svc, cases)
        out = OUT_DIR / f"{svc}.postman_collection.json"
        out.write_text(json.dumps(col, indent=2, ensure_ascii=False))
        print(f"[{svc}] {len(cases)} cases → {out.relative_to(ROOT)}")
        total += len(cases)
    print(f"total: {total} cases")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
