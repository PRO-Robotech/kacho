#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: запрос в ветку ЛИНИИ идёт тем же конвейером, что запрос в ствол, а `push` судит только ствол.

ПРЕДМЕТ (kacho#2807)
--------------------
Правило 2026-09-22 (PRO-Robotech/kacho-workspace#770): задача вливается запросом
в ветку волны, волна — запросом в ветку эпика, и обе ветки названы номером своей
задачи. Процессы были сужены до `main` и по `push`, и по `pull_request`, поэтому
запрос в ветку-номер приходил БЕЗ ЕДИНОГО контекста: у PR #2854 (`2833` → `2796`,
голова 3f5041eb9a5) `gh run list --commit <голова>` вернул ноль прогонов
(2026-09-24). Это не выдаёт себя ничем: у такого запроса нет красного, потому что
нет прогона, а «нет прогона» на странице запроса выглядит так же, как «проверять
нечего».

ПЯТЬ ОСЕЙ, И КАЖДАЯ ЛОМАЕТСЯ СВОИМ СПОСОБОМ, МОЛЧА
--------------------------------------------------
  0. ОБЯЗАТЕЛЬНЫЙ ФАЙЛ ИДЁТ НА ЗАПРОСЕ. Каждый контекст объявления
     `.github/required-contexts.txt` сопоставляется заданию, которое его
     производит, и файл такого задания обязан идти на `pull_request`. Без этой
     оси снятие `pull_request` у обязательного файла выводило бы его из-под оси 1
     молча: файл просто перестал бы быть «идущим на запросе»;
  1. БАЗЫ ЗАПРОСА. `on.pull_request.branches` КАЖДОГО файла, идущего на запросе,
     РАВЕН множеству REVIEW_BASES: ни уже (запрос в линию без вердикта), ни шире
     (прогон на запросе в ветку полосы, ревью, спасения). Сверяется РАВЕНСТВО
     множеств, а не вхождение: вхождение молчало бы на `'**'`;
  2. СТВОЛ ПО `push`. `on.push.branches` равен {main}: вердикт посаженного
     состояния выносится на стволе, а вердикт линии даёт её ЗАПРОС. `push` с
     одними метками (`tags`) — законная форма: идёт только на метки;
  3. ПУТИ НЕ СУЖАЮТ ЗАПРОС. Защита ствола требует контексты ПОИМЁННО, а
     контекст, который не начался, остаётся «ожидается» — ни зелёного, ни
     красного, и слияние стоит;
  4. ЗАДАНИЕ НЕ РАЗЛИЧАЕТ БАЗУ. Условие `if:` задания или шага, читающее базу
     запроса, даёт запросу в линию ДРУГОЙ состав заданий при том же триггере —
     то, что запрещает ось 1, этажом ниже.

ОСЬ 4: СУДИТСЯ ЗВЕНО, А НЕ ПУТЬ
-------------------------------
Образец — гейт службы доступа PRO-Robotech/kaname, `internal/check`
`review_trigger_scope.go` (задача kaname#394, четыре круга ревью); разбор
перенесён сюда без смены правил. У провайдера обращение к полю — одно из трёх:
`.имя`, фильтр `.*` или `[*]`, индекс `[выражение]`, и применяется оно к ЛЮБОМУ
значению слева: корню контекста, другому обращению, группе `(…)`, вызову
функции. Имя поля несёт только само обращение, поэтому разбор идёт по лексемам
и судит каждое обращение, не спрашивая, что стоит слева:

  * звено, НАЗВАННОЕ в тексте (имя после точки или литерал в `[ ]`), читает
    базу, если среди его слов есть слово `base`: `base`, `base_ref`,
    `GITHUB_BASE_REF`, `baseRef`, `pr-base`. Слово, а не подстрока: `database`
    и `DATABASE_REF` — другое слово, `head_ref` — тоже;
  * звено, НЕ названное в тексте (фильтр, индекс выражением), может оказаться
    любым полем своего объекта и считается чтением базы у ЛЮБОГО объекта, кроме
    закрытого перечня SILENT_OWNERS. Перечень держит МОЛЧАЩИХ, а не краснеющих:
    объект, о котором разбор не знает, даёт красное, и неполнота перечня стоит
    красного, а не слепоты.

Строковый литерал вне `[ ]` звеном не является: `'pull_request.base'` — то, с
чем сравнивают. Удвоенная кавычка литерал не закрывает. Цена надаппроксимации —
красное на имени, чьё слово `base` к базе запроса отношения не имеет
(`BASE_IMAGE`); такое условие переписывается, либо объект входит в перечень со
своей причиной: молчание дороже.

YAML: ПСЕВДОНИМ РАЗРЕШАЕТСЯ ДО СУЖДЕНИЯ, НЕДОПУСТИМОЕ — ОТКАЗ
-------------------------------------------------------------
Провайдер разрешает псевдоним `*имя` в узел якоря в любом месте объявления
(замер kaname#394 на @actions/workflow-parser 0.3.61): `if: *якорь`, задание,
`steps` и шаг под псевдонимом исполняются так же, как записанные на месте.
Здесь узлы строит составитель PyYAML: узел псевдонима он заменяет узлом якоря,
и каждое вхождение судится на своём месте. Число разрешённых псевдонимов
считается своим составителем и стоит в переписи. Отказ, а не молчание — на
записях, которых провайдер не принимает (тот же замер): ключ слияния `<<`,
ключ-псевдоним, псевдоним внутрь собственного якоря; и на повторённом ключе
отображения — какой из двух победил бы у провайдера, разбор не гадает.

КАК КОНТЕКСТ СОПОСТАВЛЯЕТСЯ ЗАДАНИЮ (ось 0)
------------------------------------------
Контекст — имя задания ПОСЛЕ подстановки матрицы (шапка
`.github/required-contexts.txt`). Матрица, записанная списками в YAML,
разворачивается, и имя подставляется значениями; матрица выражением
(`fromJSON(needs.….outputs.matrix)`) в дереве не записана, и её вставка
становится образцом `.+`. Совпадение буквальным именем побеждает совпадение
образцом: задание с матрицей выражением и именем `build ${{ … }}` в любом файле
иначе забрало бы себе `build · vet · gofmt` из ci.yaml, и обязательным стал бы
не тот файл. Имя длиннее 100 байт площадка обрезает до 97
байт по границе знака и дописывает `...` — замер на защите ствола 2026-09-24:
контекст `Postgres-пробы вне отбора интеграционной джобы (пропуск =...` ровно
100 байт при имени задания 109 байт; буквальное имя приводится к той же форме.
Контекст, не сопоставленный ни одному заданию, — находка: объявление обещает
контекст, которого не производит никто, и ось 0 о нём ничего сказать не может.

Форму объявления этот гейт НЕ разбирает сам: `parse_declaration` берётся из
держателя объявления `assert-required-contexts-match-jobs.py` (третий разбор
одной формы разошёлся бы с двумя другими молча).

ПОЧЕМУ РАЗБОР УЗЛОВ, А НЕ ПОИСК ПО ПОДСТРОКЕ
-------------------------------------------
Образец `[0-9]+` и слово `main` стоят и в комментариях — там, где фильтр
объяснён. Проверка по подстроке зеленела бы на собственном объяснении. Здесь
читается разобранный YAML, и все законные записи события различаются: `on:
pull_request` (скаляр), `on: [push, pull_request]` (последовательность),
`pull_request:` без тела и с телом, `branches` списком блоком, списком в строку и
одиночным скаляром. Первые три — «любая база», то есть находка оси 1.

ЧЕГО ЭТОТ ГЕЙТ НЕ СУДИТ — СКАЗАНО ПРЯМО
---------------------------------------
  * СЕМАНТИКУ глоба у провайдера: сверяется ЗАПИСЬ фильтра с объявленной, а что
    запись захватывает на origin — перепись веток в шапке `on:` у ci.yaml, то
    есть свойство вне дерева;
  * тела `run:` и выражения вне `if:` (`strategy.matrix`, `continue-on-error`,
    `with:`, `env:`, `outputs`, `runs-on`, `concurrency`, `environment`), хотя
    каждое способно дать запросу в линию другой состав или исход; базу,
    переложенную оболочкой, выходом или входом под именем без слова `base`, и
    объект выше базы, потреблённый функцией ЦЕЛИКОМ
    (`contains(toJSON(github.event.pull_request), 'main')`);
  * многоразовые процессы (`jobs.<id>.uses`) и составные действия
    (`.github/actions/**`) с их `if:` — на 2026-09-24 их в дереве ноль, и гейт
    печатает число `uses:` у заданий, чтобы появление первого было видно;
  * событие `pull_request_target` — это не событие запроса для этого гейта
    (другой контекст исполнения: права базы на чужой голове); `workflow_run`;
  * вид события запроса (`pull_request.types`): его сужение, как и сужение по
    путям, оставляло бы контекст новой головы «ожидается», но базы не
    различает; на 2026-09-24 `types` нет ни у одного файла;
  * поведение гейтов, судящих ДЕЛЬТУ к стволу: на запросе в линию они судят
    дельту всей линии к `main`, а не дельту запроса (названо у триггера ci.yaml);
  * саму защиту ствола: объявление против защиты сверяет
    `assert-required-contexts-match-jobs.py` (по расписанию, правом
    администратора).

ТРИ ИСХОДА
----------
  0 — осмотрено, находок ноль, и перепись названа;
  1 — находки (каждая называет файл и ось);
  2 — ИЗМЕРЕНИЕ НЕ СДЕЛАНО: нет PyYAML, объявлений не прочитано, объявление не
      разобрано или записано формой, которой провайдер не принимает, на запросе
      не идёт ни один файл, условий или звеньев прочитано ноль, объявление
      контекстов пусто. Это НЕ «находок нет».

Запуск:
  python3 .github/scripts/assert-review-trigger-scope.py --self-test
  python3 .github/scripts/assert-review-trigger-scope.py
  python3 .github/scripts/assert-review-trigger-scope.py --rev origin/2796
"""
from __future__ import annotations

import argparse
import functools
import importlib.util
import itertools
import re
import subprocess
import sys
import traceback
from dataclasses import dataclass, field
from pathlib import Path

try:
    import yaml
except ImportError as exc:  # pragma: no cover - в конвейере yaml ставится шагом
    print(f"ИЗМЕРЕНИЕ НЕ СДЕЛАНО: нет PyYAML, разобрать объявления нечем: {exc}")
    sys.exit(2)

TRUNK = "main"

# LINE_PATTERN — форма имени ветки ЛИНИИ (волны, эпика): одна или больше цифр,
# и ничего больше. В фильтре провайдера `[0-9]` — одна цифра, `+` — «один или
# больше предыдущего знака», и образец сопоставляется со ВСЕМ именем ветки.
# `'[0-9]*'` НЕ равносилен ему: `*` берёт любой хвост без косой черты.
LINE_PATTERN = "[0-9]+"

# REVIEW_BASES — базы запроса, на которых файл ОБЯЗАН идти, и только они.
# Файлы процессов несут их копиями (иначе фильтр провайдеру не объявить), и
# разойтись копиям не даёт этот гейт.
REVIEW_BASES = (TRUNK, LINE_PATTERN)

REVIEW_EVENT = "pull_request"
WORKFLOWS_DIR = ".github/workflows"

# SILENT_OWNERS — ЗАКРЫТЫЙ перечень объектов, у которых неизвестное звено
# базой запроса оказаться не может (шапка, «ОСЬ 4»). Значение — где объект
# обязан стоять, чтобы молчать: корнем выражения или полем другого объекта.
# Регистр снят: провайдер регистра имён не различает.
SILENT_OWNERS = {
    # Ключи — имена заданий из `needs:`, значение — {outputs, result}: звено
    # выбирает ИТОГ задания, а не значение. `outputs` в перечень не входит.
    "needs": "context",
    # Ключи — `id` шагов, значение — {outputs, outcome, conclusion}: то же.
    "steps": "context",
    # Метки запроса: у метки поля id, node_id, url, name, description, color,
    # default — поля базы нет.
    "labels": "field",
}

PROVIDER_CUT_BYTES = 100
PROVIDER_CUT_TAIL = "..."


class Unmeasured(Exception):
    """Измерение не сделано. Отличается от «находок нет»."""


# ── РАЗБОР ВЫРАЖЕНИЯ УСЛОВИЯ ─────────────────────────────────────────────────


def names_base(link: str) -> bool:
    """Есть ли среди слов звена слово `base` (без регистра).

    Слова разделены любым знаком, кроме буквы и цифры; вторым разбором — ещё и
    сменой строчной буквы заглавной (`baseRef`). Слово, а не подстрока.
    """
    for camel in (False, True):
        word: list[str] = []
        prev_lower = False
        for ch in link + " ":
            alnum = ch.isalpha() or ch.isdigit()
            if not alnum or (camel and prev_lower and ch.isupper()):
                if "".join(word).lower() == "base":
                    return True
                word = []
            if alnum:
                word.append(ch)
            prev_lower = ch.islower()
    return False


@dataclass(frozen=True)
class Owner:
    """Объект, к которому применяется следующее обращение: названный (root —
    стоит корнем выражения), неизвестный (чем именно — для находки) или никакой."""

    named: str = ""
    root: bool = False
    unknown: str = ""


def _owner_is_silent(on: Owner) -> bool:
    place = SILENT_OWNERS.get(on.named.lower())
    if place is None:
        return False
    return (place == "context") == on.root


_DIGITS = "0123456789"


def _is_ident_start(c: str) -> bool:
    return c == "_" or ("a" <= c <= "z") or ("A" <= c <= "Z")


def _is_ident_char(c: str) -> bool:
    return _is_ident_start(c) or c in _DIGITS or c == "-"


def _scan_ident(s: str, i: int) -> int:
    while i < len(s) and _is_ident_char(s[i]):
        i += 1
    return i


def _scan_number(s: str, i: int) -> int:
    while i < len(s) and (_is_ident_char(s[i]) or s[i] == "."):
        i += 1
    return i


def _skip_spaces(s: str, i: int) -> int:
    while i < len(s) and s[i] in " \t\n\r":
        i += 1
    return i


def _scan_string(s: str, i: int) -> tuple[str, int]:
    """Литерал в одинарных кавычках: текст и позиция за ним. Удвоенная кавычка
    внутри — одна кавычка текста; незакрытый литерал тянется до конца."""
    out: list[str] = []
    j = i + 1
    while j < len(s):
        if s[j] != "'":
            out.append(s[j])
            j += 1
            continue
        if j + 1 < len(s) and s[j + 1] == "'":
            out.append("'")
            j += 2
            continue
        return "".join(out), j + 1
    return "".join(out), len(s)


def condition_reads_base(expr: str) -> tuple[list[str], int]:
    """Какими звеньями условие читает базу запроса (пусто — не читает) и
    сколько звеньев прочитано всего — для переписи."""
    readings: list[str] = []
    links = 0
    unknown_link = Owner(unknown="неизвестного звена")

    def named(link: str) -> None:
        nonlocal links
        links += 1
        if names_base(link):
            readings.append(f"звено `{link}`")

    def unknown_at(on: Owner, link: str) -> None:
        nonlocal links
        links += 1
        if on.unknown:
            readings.append(f"неизвестное звено `{link}` у {on.unknown}")
        elif on.named and not _owner_is_silent(on):
            readings.append(f"неизвестное звено `{link}` у `{on.named}`")

    owner = Owner()
    access = False
    open_frames: list[tuple[Owner, int]] = []
    i, n = 0, len(expr)
    while i < n:
        c = expr[i]
        if c in " \t\n\r":
            i += 1  # пробел лексемой не является и прежней не отменяет
        elif c == "'":
            _, i = _scan_string(expr, i)
            owner, access = Owner(), False
        elif c == "." and access:
            k = _skip_spaces(expr, i + 1)
            if k < n and expr[k] == "*":
                unknown_at(owner, "*")
                owner, access, i = unknown_link, True, k + 1
            elif k < n and _is_ident_start(expr[k]):
                j = _scan_ident(expr, k)
                named(expr[k:j])
                owner, access, i = Owner(named=expr[k:j]), True, j
            else:
                owner, access, i = Owner(), False, k
        elif c == "[":
            k = _skip_spaces(expr, i + 1)
            if k < n and expr[k] == "'":
                lit, e = _scan_string(expr, k)
                e = _skip_spaces(expr, e)
                if e < n and expr[e] == "]":
                    named(lit)
                    owner, access, i = Owner(named=lit), True, e + 1
                    continue
            if k < n and expr[k] == "*":
                e = _skip_spaces(expr, k + 1)
                if e < n and expr[e] == "]":
                    unknown_at(owner, "[*]")
                    owner, access, i = unknown_link, True, e + 1
                    continue
            if k < n and (expr[k] in _DIGITS or expr[k] in "-+"):
                e = _skip_spaces(expr, _scan_number(expr, k + 1))
                if e < n and expr[e] == "]":
                    # Элемент списка судится по имени списка: у элемента
                    # `pull_requests` поле базы есть, у элемента меток — нет.
                    links += 1
                    access, i = True, e + 1
                    continue
            open_frames.append((owner, i))
            owner, access, i = Owner(), False, i + 1
        elif c == "]":
            if open_frames:
                frame_owner, at = open_frames.pop()
                unknown_at(frame_owner, expr[at:i + 1])
            owner, access, i = unknown_link, True, i + 1
        elif c == ")":
            owner, access, i = Owner(unknown="результата группы или вызова"), True, i + 1
        elif _is_ident_start(c):
            j = _scan_ident(expr, i)
            k = _skip_spaces(expr, j)
            if k < n and expr[k] == "(":
                owner, access, i = Owner(), False, j  # имя функции звеном не является
                continue
            named(expr[i:j])
            owner, access, i = Owner(named=expr[i:j], root=True), True, j
        elif c in _DIGITS or c in ".-+":
            owner, access, i = Owner(), False, _scan_number(expr, i + 1)
        else:
            owner, access, i = Owner(), False, i + 1
    return readings, links


# ── РАЗБОР ОБЪЯВЛЕНИЯ ────────────────────────────────────────────────────────


class _CountingLoader(yaml.SafeLoader):
    """Составитель, который считает разрешённые псевдонимы и отказывает на трёх
    записях, которых провайдер не принимает (шапка, «YAML»)."""

    def __init__(self, stream: str) -> None:
        super().__init__(stream)
        self.aliases = 0
        self._open_anchors: set[str] = set()

    def compose_node(self, parent, index):  # noqa: ANN001 - сигнатура PyYAML
        if self.check_event(yaml.AliasEvent):
            ev = self.peek_event()
            line = ev.start_mark.line + 1
            if isinstance(parent, yaml.MappingNode) and index is None:
                raise Unmeasured(f"ключ отображения записан псевдонимом `*{ev.anchor}` (строка {line}): "
                                 "провайдер ключа-псевдонима не разрешает, и разбор не знает, какой ключ судить")
            if ev.anchor in self._open_anchors:
                raise Unmeasured(f"псевдоним `*{ev.anchor}` (строка {line}) ведёт внутрь собственного якоря: "
                                 "провайдер такое объявление не принимает, а разбор не судит бесконечный узел")
            self.aliases += 1
            return super().compose_node(parent, index)
        ev = self.peek_event()
        anchor = getattr(ev, "anchor", None)
        if anchor:
            self._open_anchors.add(anchor)
        try:
            return super().compose_node(parent, index)
        finally:
            if anchor:
                self._open_anchors.discard(anchor)


@functools.lru_cache(maxsize=None)
def _compose(raw: str) -> tuple[yaml.Node, int]:
    """Узел документа и число псевдонимов. Запоминается по тексту: самопроверка
    меняет по одному файлу за подпробу, а прочие разбирать заново незачем —
    суждение узлов не меняет, и общий узел безопасен."""
    loader = _CountingLoader(raw)
    try:
        node = loader.get_single_node()
    except yaml.YAMLError as exc:
        raise Unmeasured(f"объявление не разобрано: {exc}") from exc
    finally:
        loader.dispose()
    if not isinstance(node, yaml.MappingNode):
        raise Unmeasured("объявление не является отображением верхнего уровня")
    _refuse_merge_and_duplicates(node, set())
    return node, loader.aliases


def _refuse_merge_and_duplicates(node: yaml.Node, seen: set[int]) -> None:
    if id(node) in seen:
        return
    seen.add(id(node))
    if isinstance(node, yaml.MappingNode):
        keys: dict[str, int] = {}
        for k, v in node.value:
            line = k.start_mark.line + 1
            if k.tag == "tag:yaml.org,2002:merge":
                raise Unmeasured(f"ключ слияния `<<` (строка {line}): провайдер слияния не применяет и "
                                 "объявление не принимает, а разбор не знает, чьи ключи судить")
            if isinstance(k, yaml.ScalarNode):
                if k.value in keys:
                    raise Unmeasured(f"ключ `{k.value}` повторён (строки {keys[k.value]} и {line}): какой из "
                                     "двух победил бы, разбор не гадает")
                keys[k.value] = line
            _refuse_merge_and_duplicates(k, seen)
            _refuse_merge_and_duplicates(v, seen)
    elif isinstance(node, yaml.SequenceNode):
        for c in node.value:
            _refuse_merge_and_duplicates(c, seen)


def _value(m: yaml.Node | None, key: str) -> yaml.Node | None:
    if not isinstance(m, yaml.MappingNode):
        return None
    for k, v in m.value:
        if isinstance(k, yaml.ScalarNode) and k.value == key:
            return v
    return None


def _scalar(n: yaml.Node | None) -> str:
    return n.value if isinstance(n, yaml.ScalarNode) else ""


def _is_null(n: yaml.Node | None) -> bool:
    return n is None or (isinstance(n, yaml.ScalarNode) and n.tag == "tag:yaml.org,2002:null")


@dataclass
class EventFilter:
    bare: bool = False  # событие названо без тела (скаляром, в последовательности, `~`)
    keys: set[str] = field(default_factory=set)
    branches: list[str] = field(default_factory=list)


def _events_of(on: yaml.Node) -> dict[str, EventFilter]:
    out: dict[str, EventFilter] = {}
    line = on.start_mark.line + 1
    if isinstance(on, yaml.ScalarNode):
        out[on.value] = EventFilter(bare=True)
    elif isinstance(on, yaml.SequenceNode):
        for n in on.value:
            if not isinstance(n, yaml.ScalarNode):
                raise Unmeasured(f"элемент последовательности `on:` не скаляр (строка {n.start_mark.line + 1})")
            out[n.value] = EventFilter(bare=True)
    elif isinstance(on, yaml.MappingNode):
        for k, body in on.value:
            # Тело разбирается только у двух событий, которые гейт судит: у
            # `schedule` оно последовательность, у `workflow_run` — свои ключи, и
            # отказ на их форме был бы отказом не по предмету.
            name = _scalar(k)
            out[name] = _filter_of(name, body) if name in (REVIEW_EVENT, "push") else EventFilter()
    else:
        raise Unmeasured(f"`on:` записан формой, которую разбор не знает (строка {line})")
    return out


def _filter_of(event: str, body: yaml.Node) -> EventFilter:
    if _is_null(body):
        return EventFilter(bare=True)
    if not isinstance(body, yaml.MappingNode):
        raise Unmeasured(f"событие {event!r}: тело не отображение (строка {body.start_mark.line + 1})")
    f = EventFilter()
    for k, v in body.value:
        key = _scalar(k)
        f.keys.add(key)
        if key != "branches":
            continue
        if isinstance(v, yaml.ScalarNode):
            f.branches = [v.value]
        elif isinstance(v, yaml.SequenceNode):
            for b in v.value:
                if not isinstance(b, yaml.ScalarNode):
                    raise Unmeasured(f"событие {event!r}: элемент `branches` не скаляр "
                                     f"(строка {b.start_mark.line + 1})")
                f.branches.append(b.value)
        else:
            raise Unmeasured(f"событие {event!r}: `branches` записан формой, которую разбор не знает "
                             f"(строка {v.start_mark.line + 1})")
    return f


def _q(xs) -> str:  # noqa: ANN001
    return "{" + ", ".join(f"`{x}`" for x in sorted(xs)) + "}"


def _bases() -> str:
    return "{" + ", ".join(REVIEW_BASES) + "}"


def _audit_review_filter(f: EventFilter) -> tuple[list[str], bool]:
    findings: list[str] = []
    if "branches-ignore" in f.keys:
        findings.append("ось 1: фильтр баз запроса записан ИСКЛЮЧЕНИЕМ (`branches-ignore`): всякая новая форма "
                        f"ветки идёт на запросе по умолчанию — нужен `branches` со множеством {_bases()}")
    for k in ("paths", "paths-ignore"):
        if k in f.keys:
            findings.append(f"ось 3: запрос сужен по путям (`{k}`): защита ствола требует контексты ПОИМЁННО, а "
                            "контекст, который не начался, остаётся «ожидается» — и слияние стоит")
    if f.bare or "branches" not in f.keys:
        if "branches-ignore" not in f.keys:
            findings.append("ось 1: запрос не сужен по базе — файл идёт на запросе в ЛЮБУЮ ветку (полосы, ревью, "
                            f"спасения), хотя вердикт нужен только стволу и линии {_bases()}")
        return findings, False
    got, want = set(f.branches), set(REVIEW_BASES)
    missing, extra = want - got, got - want
    if not missing and not extra:
        return findings, True
    why: list[str] = []
    if missing:
        why.append(f"недостаёт {_q(missing)} — запрос туда приходит БЕЗ ЕДИНОГО контекста этого файла")
    if extra:
        why.append(f"лишние {_q(extra)} — захватывают ветки, которые не ствол и не линия")
    findings.append(f"ось 1: базы запроса {_q(f.branches)} ≠ {_bases()}: " + "; ".join(why))
    return findings, False


def _audit_push_filter(f: EventFilter) -> tuple[list[str], bool]:
    if "branches-ignore" in f.keys:
        return ["ось 2: `push` сужен ИСКЛЮЧЕНИЕМ (`branches-ignore`): каждая ветка вне перечня идёт по "
                f"отправке — нужен `branches: [{TRUNK}]`"], True
    if not f.bare and "branches" not in f.keys and ({"tags", "tags-ignore"} & f.keys):
        return [], False  # push только по меткам — законная форма
    if f.bare or "branches" not in f.keys:
        return ["ось 2: `push` не сужен по ветке — файл идёт на каждую отправку в каждую ветку, хотя по "
                f"push судится только ствол — нужен `branches: [{TRUNK}]`"], True
    got = set(f.branches)
    findings: list[str] = []
    if got - {TRUNK}:
        findings.append(f"ось 2: `push` расширен за ствол: {_q(got - {TRUNK})} — вердикт линии даёт её ЗАПРОС, "
                        "а отправка в неё оплачивалась бы вторым полным прогоном того же дерева")
    if TRUNK not in got:
        findings.append(f"ось 2: `push` не идёт в ствол `{TRUNK}` — вердикт посаженного состояния выносить не о чем")
    return findings, True


# ── СОПОСТАВЛЕНИЕ КОНТЕКСТА ЗАДАНИЮ (ось 0) ──────────────────────────────────


def provider_cut(name: str) -> str:
    """Имя в той форме, в какой площадка ставит его контекстом (шапка, «ось 0»)."""
    raw = name.encode("utf-8")
    if len(raw) <= PROVIDER_CUT_BYTES:
        return name
    budget = PROVIDER_CUT_BYTES - len(PROVIDER_CUT_TAIL)
    out, used = [], 0
    for ch in name:
        size = len(ch.encode("utf-8"))
        if used + size > budget:
            break
        out.append(ch)
        used += size
    return "".join(out) + PROVIDER_CUT_TAIL


_EXPR = re.compile(r"\$\{\{(.*?)\}\}", re.S)
_MATRIX_REF = re.compile(r"\s*matrix\.([A-Za-z_][A-Za-z0-9_-]*)\s*")


def _literal_matrix(job: yaml.Node) -> dict[str, list[str]] | None:
    """Матрица, записанная списками скаляров без include/exclude, либо None."""
    matrix = _value(_value(job, "strategy"), "matrix")
    if not isinstance(matrix, yaml.MappingNode):
        return None
    out: dict[str, list[str]] = {}
    for k, v in matrix.value:
        key = _scalar(k)
        if key in ("include", "exclude") or not isinstance(v, yaml.SequenceNode):
            return None
        if not all(isinstance(x, yaml.ScalarNode) for x in v.value):
            return None
        out[key] = [x.value for x in v.value]
    return out


def job_contexts(job_id: str, job: yaml.Node) -> tuple[set[str], list[re.Pattern[str]]]:
    """Буквальные контексты задания и образцы для тех, что в дереве не записаны."""
    template = _scalar(_value(job, "name")) or job_id
    has_matrix = _value(_value(job, "strategy"), "matrix") is not None
    literal = _literal_matrix(job)
    pieces = _EXPR.split(template)  # чётные — текст, нечётные — выражения
    exprs = pieces[1::2]
    if not exprs:
        if has_matrix:
            # Имя без выражений матрицы площадка дополняет значениями в скобках;
            # значения здесь не воспроизводятся — только образец.
            return set(), [re.compile(re.escape(template) + r" \(.+\)")]
        return {provider_cut(template)}, []
    refs = [_MATRIX_REF.fullmatch(e) for e in exprs]
    if literal is not None and all(m and m.group(1) in literal for m in refs):
        keys = list(literal)
        names: set[str] = set()
        for combo in itertools.product(*(literal[k] for k in keys)):
            values = dict(zip(keys, combo))
            out = []
            for idx, p in enumerate(pieces):
                out.append(p if idx % 2 == 0 else values[refs[idx // 2].group(1)])
            names.add(provider_cut("".join(out)))
        return names, []
    rx = "".join(re.escape(p) if idx % 2 == 0 else ".+" for idx, p in enumerate(pieces))
    return set(), [re.compile(rx)]


# ── ВЕРДИКТ О КОРПУСЕ ────────────────────────────────────────────────────────


@dataclass
class Census:
    files: int = 0
    on_review: int = 0
    review_at_line: int = 0
    on_branch_push: int = 0
    conditions: int = 0
    condition_links: int = 0
    aliases: int = 0
    job_uses: int = 0
    declared: int = 0
    matched: int = 0
    required_files: list[str] = field(default_factory=list)

    def __str__(self) -> str:
        return (f"файлов процессов {self.files} · идут на запросе {self.on_review} · из них с базами "
                f"{_bases()} {self.review_at_line} · идут по push в ветки {self.on_branch_push} · "
                f"условий if: осмотрено {self.conditions} · звеньев в них прочитано {self.condition_links} · "
                f"псевдонимов YAML разрешено {self.aliases} · заданий `uses:` {self.job_uses} · "
                f"контекстов объявлено {self.declared}, сопоставлено {self.matched} · обязательных файлов "
                f"{len(self.required_files)} ({', '.join(self.required_files) or '—'})")


def _audit_conditions(rel: str, jobs: yaml.Node | None, census: Census) -> list[str]:
    if jobs is None:
        return []
    if not isinstance(jobs, yaml.MappingNode):
        raise Unmeasured(f"`jobs:` не отображение (строка {jobs.start_mark.line + 1}): ось 4 не знает, где задания")
    findings: list[str] = []

    def judge(where: str, cond: yaml.Node) -> None:
        if not isinstance(cond, yaml.ScalarNode):
            raise Unmeasured(f"{where}: условие `if:` не скаляр (строка {cond.start_mark.line + 1}) — провайдер "
                             "такого не принимает, а разбор не знает, что судить")
        census.conditions += 1
        readings, links = condition_reads_base(cond.value)
        census.condition_links += links
        if readings:
            findings.append(f"ось 4: {where}: условие {cond.value.strip()!r} читает БАЗУ запроса "
                            f"({'; '.join(readings)}) — на запросе в линию состав заданий другой, чем на запросе "
                            "в ствол, при том же триггере")

    for k, job in jobs.value:
        where = f"задание {_scalar(k)}"
        if not isinstance(job, yaml.MappingNode):
            raise Unmeasured(f"{where} не отображение (строка {job.start_mark.line + 1}): ось 4 не знает, где "
                             "его условие")
        if _value(job, "uses") is not None:
            census.job_uses += 1
        cond = _value(job, "if")
        if cond is not None:
            judge(where, cond)
        steps = _value(job, "steps")
        if steps is None:
            continue
        if not isinstance(steps, yaml.SequenceNode):
            raise Unmeasured(f"{where}: `steps` не последовательность (строка {steps.start_mark.line + 1}): ось 4 "
                             "не знает, где условия шагов")
        for si, st in enumerate(steps.value, start=1):
            step_where = f"{where}, шаг {si}"
            if not isinstance(st, yaml.MappingNode):
                raise Unmeasured(f"{step_where} не отображение (строка {st.start_mark.line + 1}): ось 4 не знает, "
                                 "где его условие")
            cond = _value(st, "if")
            if cond is not None:
                judge(step_where, cond)
    return findings


def audit(corpus: dict[str, str], declared: list[str]) -> tuple[list[str], Census]:
    """Вердикт о корпусе. Чистая функция от входа — затем, чтобы способность
    упасть доказывалась подачей входа, а не правкой дерева."""
    census = Census(files=len(corpus), declared=len(declared))
    if not corpus:
        raise Unmeasured("объявлений процессов прочитано ноль — вердикт беспредметен")
    if not declared:
        raise Unmeasured("объявление обязательных контекстов пусто — ось 0 судить не о чем")

    findings: list[str] = []
    on_review: set[str] = set()
    literal_by_file: dict[str, set[str]] = {}
    patterns_by_file: dict[str, list[re.Pattern[str]]] = {}

    for rel in sorted(corpus):
        try:
            doc, aliases = _compose(corpus[rel])
            census.aliases += aliases
            on = _value(doc, "on")
            if on is None:
                raise Unmeasured("в объявлении нет `on:` — процесс не запускается ничем, и молчание разбора было "
                                 "бы вердиктом о пустом")
            events = _events_of(on)
            per_file: list[str] = []
            if REVIEW_EVENT in events:
                census.on_review += 1
                on_review.add(rel)
                fs, at_line = _audit_review_filter(events[REVIEW_EVENT])
                census.review_at_line += int(at_line)
                per_file += fs
            if "push" in events:
                fs, on_branches = _audit_push_filter(events["push"])
                census.on_branch_push += int(on_branches)
                per_file += fs
            jobs = _value(doc, "jobs")
            per_file += _audit_conditions(rel, jobs, census)
            lits: set[str] = set()
            pats: list[re.Pattern[str]] = []
            if isinstance(jobs, yaml.MappingNode):
                for k, job in jobs.value:
                    ls, ps = job_contexts(_scalar(k), job)
                    lits |= ls
                    pats += ps
            literal_by_file[rel], patterns_by_file[rel] = lits, pats
        except Unmeasured as exc:
            raise Unmeasured(f"{rel}: {exc}") from exc
        findings += [f"{rel}: {f}" for f in per_file]

    if census.on_review < 2:
        raise Unmeasured(f"файлов, идущих на запросе ({REVIEW_EVENT}), найдено {census.on_review}: на нуле детектор "
                         "события молчит, на одном свойство «КАЖДЫЙ идёт на запросе в линию» проверяется вырожденно")
    if census.conditions == 0:
        raise Unmeasured("условий `if:` осмотрено ноль — разбор не дошёл до заданий, и молчание оси 4 сказано ни о чём")
    if census.condition_links == 0:
        raise Unmeasured("условия `if:` осмотрены, а звеньев в них прочитано ноль — разбор лексем слеп")

    required: dict[str, list[str]] = {}
    unmatched: list[str] = []
    for ctx in declared:
        files = [f for f in sorted(corpus) if ctx in literal_by_file[f]]
        if not files:
            files = [f for f in sorted(corpus) if any(p.fullmatch(ctx) for p in patterns_by_file[f])]
        if not files:
            unmatched.append(ctx)
            continue
        census.matched += 1
        for f in files:
            required.setdefault(f, []).append(ctx)
    census.required_files = sorted(required)
    for ctx in unmatched:
        findings.append(f"ось 0: контекст {ctx!r} объявлен обязательным, а задания, которое его производит, нет ни "
                        "в одном файле — какой файл обязан идти на запросе, судить нечем")
    for f in census.required_files:
        if f not in on_review:
            names = required[f]
            findings.append(f"{f}: ось 0: файл производит обязательные контексты ({len(names)}: "
                            f"{', '.join(repr(x) for x in names[:3])}{' …' if len(names) > 3 else ''}), а на запросе "
                            "не идёт — запрос и в ствол, и в линию ждёт их вечно")
    if not census.required_files:
        raise Unmeasured("ни один контекст не сопоставлен заданию — ось 0 судила бы пустое")
    return findings, census


# ── ЧТЕНИЕ ДЕРЕВА ────────────────────────────────────────────────────────────


def repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


def _load_declaration_parser():  # noqa: ANN202
    here = Path(__file__).resolve().parent / "assert-required-contexts-match-jobs.py"
    spec = importlib.util.spec_from_file_location("required_contexts_holder", here)
    if spec is None or spec.loader is None:
        raise Unmeasured(f"держатель объявления {here.name} не загружен — форму объявления разбирать нечем")
    mod = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = mod  # разбор аннотаций и dataclass ищут модуль здесь
    spec.loader.exec_module(mod)
    return mod.parse_declaration, mod.DECLARATION_PATH


def _git(root: Path, *args: str) -> str:
    p = subprocess.run(["git", "-C", str(root), *args], capture_output=True, text=True)
    if p.returncode != 0:
        raise Unmeasured(f"git {' '.join(args)}: {p.stderr.strip() or 'код ' + str(p.returncode)}")
    return p.stdout


def read_tree(root: Path, rev: str | None) -> tuple[dict[str, str], list[str]]:
    """Корпус процессов и объявленные контексты. Состав — из индекса git (либо из
    ревизии), а не с диска: посторонний файл рядом на вердикт не влияет."""
    parse_declaration, declaration_path = _load_declaration_parser()
    if rev:
        listed = _git(root, "ls-tree", "--name-only", f"{rev}:{WORKFLOWS_DIR}").split()
        paths = [f"{WORKFLOWS_DIR}/{p}" for p in listed]
    else:
        paths = _git(root, "ls-files", "--", WORKFLOWS_DIR).split()
    paths = [p for p in paths if p.endswith((".yml", ".yaml")) and "/" not in p[len(WORKFLOWS_DIR) + 1:]]
    corpus: dict[str, str] = {}
    for p in paths:
        corpus[p] = _git(root, "show", f"{rev}:{p}") if rev else (root / p).read_text(encoding="utf-8")
    decl = _git(root, "show", f"{rev}:{declaration_path}") if rev else (root / declaration_path).read_text(
        encoding="utf-8")
    return corpus, parse_declaration(decl)


# ── САМОПРОВЕРКА: ИНЪЕКЦИЯ НАСТОЯЩИМ ВХОДОМ ──────────────────────────────────
#
# Корпус читается из дерева, и дефект вносится в КОПИЮ ci.yaml — по одному факту
# за раз. Каждой красной подпробе отвечает законный близнец той же оси, на
# котором гейт молчит; на дереве как есть находок ноль, и это утверждается
# первым. Запись, которую инъекция меняет, обязана быть в файле: инъекция, не
# нашедшая своей записи, — отказ самопроверки, а не её зелёное.

INJECT_REL = f"{WORKFLOWS_DIR}/ci.yaml"
NOT_REQUIRED_REL = f"{WORKFLOWS_DIR}/production-posture.yml"
REVIEW_BLOCK = "  pull_request:\n    branches:\n      - main\n      - '[0-9]+'\n"
PUSH_BLOCK = "  push:\n    branches: [main]\n"
JOBS_ANCHOR = "\njobs:\n"


class SelfTestFailure(Exception):
    pass


def _expect(cond: bool, what: str) -> None:
    if not cond:
        raise SelfTestFailure(what)


def _inject(raw: str, old: str, new: str) -> str:
    _expect(old in raw, f"инъекция беспредметна: записи {old!r} в объявлении нет")
    return raw.replace(old, new, 1)


def self_test(root: Path) -> int:
    corpus0, declared0 = read_tree(root, None)
    for rel in (INJECT_REL, NOT_REQUIRED_REL):
        if rel not in corpus0:
            print(f"ИЗМЕРЕНИЕ НЕ СДЕЛАНО: {rel} не прочитан — инъекцию вносить некуда")
            return 2

    def run(edit=None, rel: str = INJECT_REL, declared=None, extra=None):  # noqa: ANN001, ANN202
        corpus = dict(corpus0)
        if edit is not None:
            corpus[rel] = edit(corpus[rel])
        if extra:
            corpus.update(extra)
        return audit(corpus, declared0 if declared is None else declared)

    def with_job(job_yaml: str):  # noqa: ANN202
        return run(lambda raw: _inject(raw, JOBS_ANCHOR, JOBS_ANCHOR + job_yaml))

    def cond_job(cond: str):  # noqa: ANN202
        return with_job(f"  onlymain:\n    if: {cond}\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n")

    def cond_step(cond: str):  # noqa: ANN202
        return with_job(f"  onlymain:\n    runs-on: ubuntu-latest\n    steps:\n      - if: {cond}\n        run: echo ok\n")

    def refused(fn) -> str:  # noqa: ANN001
        try:
            fn()
        except Unmeasured as exc:
            return str(exc)
        raise SelfTestFailure("форма, которой провайдер не принимает, прошла разбор молча")

    control_findings, control = audit(corpus0, declared0)
    cases: list[tuple[str, object]] = []
    new_rel = f"{WORKFLOWS_DIR}/newflow.yml"

    def case(name: str):  # noqa: ANN202
        def deco(fn):  # noqa: ANN001, ANN202
            cases.append((name, fn))
            return fn
        return deco

    # ── положительный близнец на дереве как есть ──
    @case("дерево как есть: находок ноль, перепись непуста")
    def _():
        _expect(not control_findings, f"на дереве как есть {len(control_findings)} находок: {control_findings}")
        _expect(control.on_review == control.review_at_line, "не все файлы на запросе несут базы линии")
        _expect(control.conditions > 0 and control.condition_links > 0, "ось 4 проверялась бы вырожденно")
        _expect(INJECT_REL in control.required_files, "ci.yaml не опознан обязательным — ось 0 слепа")
        _expect(NOT_REQUIRED_REL not in control.required_files, "production-posture опознан обязательным")
        _expect(control.aliases == 0, "в дереве псевдонимов нет — счёт ниже меряется от нуля")

    # ── ось 1 ──
    @case("ось 1: база линии снята — ровно тот дефект, что закрывает задача")
    def _():
        got, c = run(lambda r: _inject(r, "      - '[0-9]+'\n", ""))
        _expect(len(got) == 1 and INJECT_REL in got[0] and "недостаёт {`[0-9]+`}" in got[0], str(got))
        _expect(c.review_at_line == c.on_review - 1, "перепись не назвала разрыв числом")

    @case("ось 1: фильтр расширен до всех веток")
    def _():
        got, _c = run(lambda r: _inject(r, "      - '[0-9]+'\n", "      - '[0-9]+'\n      - '**'\n"))
        _expect(len(got) == 1 and "лишние {`**`}" in got[0], str(got))

    @case("ось 1: [0-9]* вместо [0-9]+ — захват хвоста")
    def _():
        got, _c = run(lambda r: _inject(r, "      - '[0-9]+'\n", "      - '[0-9]*'\n"))
        _expect(len(got) == 1 and "недостаёт {`[0-9]+`}" in got[0] and "лишние {`[0-9]*`}" in got[0], str(got))

    @case("ось 1: фильтр баз снят целиком — запрос в любую ветку")
    def _():
        got, _c = run(lambda r: _inject(r, REVIEW_BLOCK, "  pull_request:\n"))
        _expect(len(got) == 1 and "в ЛЮБУЮ" in got[0], str(got))

    @case("ось 1: фильтр баз записан исключением")
    def _():
        got, _c = run(lambda r: _inject(r, REVIEW_BLOCK, "  pull_request:\n    branches-ignore:\n      - 'lane/**'\n"))
        _expect(len(got) == 1 and "ИСКЛЮЧЕНИЕМ" in got[0], str(got))

    @case("ось 1, близнец: то же множество потоком, в обратном порядке, в двойных кавычках, main дважды")
    def _():
        for form in ("  pull_request:\n    branches: ['[0-9]+', main]\n",
                     '  pull_request:\n    branches: ["[0-9]+", "main"]\n',
                     "  pull_request:\n    branches:\n      - '[0-9]+'\n      - main\n      - main\n"):
            got, c = run(lambda r, f=form: _inject(r, REVIEW_BLOCK, f))
            _expect(not got, f"законная запись того же множества объявлена нарушением: {form!r}: {got}")
            _expect(c.on_review == c.review_at_line, f"перепись не сошлась на {form!r}")

    @case("ось 1: образец только в комментарии фильтром не считается")
    def _():
        got, _c = run(lambda r: _inject(r, "      - '[0-9]+'\n", "      # - '[0-9]+'\n"))
        _expect(len(got) == 1 and "недостаёт {`[0-9]+`}" in got[0], str(got))

    # ── ось 3 ──
    @case("ось 3: запрос сужен по путям")
    def _():
        got, _c = run(lambda r: _inject(r, REVIEW_BLOCK, REVIEW_BLOCK + "    paths:\n      - 'internal/**'\n"))
        _expect(len(got) == 1 and "ПОИМЁННО" in got[0], str(got))

    # ── ось 2 ──
    @case("ось 2: push расширен на ветки линии")
    def _():
        got, c = run(lambda r: _inject(r, PUSH_BLOCK, "  push:\n    branches: [main, '[0-9]+']\n"))
        _expect(len(got) == 1 and "`push` расширен за ствол: {`[0-9]+`}" in got[0], str(got))
        _expect(c.on_review == c.review_at_line, "ось 2 задела ось 1")

    @case("ось 2: push расширен прежней формой имени задачи KAC-*")
    def _():
        got, _c = run(lambda r: _inject(r, PUSH_BLOCK, '  push:\n    branches: [main, "KAC-*"]\n'))
        _expect(len(got) == 1 and "{`KAC-*`}" in got[0], str(got))

    @case("ось 2: push не сужен по ветке")
    def _():
        got, _c = run(lambda r: _inject(r, PUSH_BLOCK, "  push:\n"))
        _expect(len(got) == 1 and "`push` не сужен по ветке" in got[0], str(got))

    @case("ось 2, близнец: push только по меткам — не находка и не push в ветки")
    def _():
        got, c = run(lambda r: _inject(r, PUSH_BLOCK, "  push:\n    tags:\n      - 'v[0-9]+.[0-9]+.[0-9]+'\n"))
        _expect(not got, f"push по одним меткам объявлен нарушением: {got}")
        _expect(c.on_branch_push == control.on_branch_push - 1, "push по меткам посчитан как push в ветки")

    # ── ось 0 ──
    @case("ось 0: у обязательного файла снят pull_request")
    def _():
        got, c = run(lambda r: _inject(r, REVIEW_BLOCK, ""))
        _expect(any(INJECT_REL in g and "ось 0" in g and "не идёт" in g for g in got), str(got))
        _expect(c.on_review == control.on_review - 1, "перепись не назвала выбывший файл")

    @case("ось 0, близнец: у необязательного файла снят pull_request — ось 0 молчит")
    def _():
        got, c = run(lambda r: _inject(r, REVIEW_BLOCK, ""), rel=NOT_REQUIRED_REL)
        _expect(not any("ось 0" in g for g in got), f"ось 0 назвала необязательный файл: {got}")
        _expect(c.on_review == control.on_review - 1, "перепись не назвала выбывший файл")

    @case("ось 0: объявлен контекст, которого не производит никто")
    def _():
        got, c = run(declared=declared0 + ["контекст без задания"])
        _expect(len(got) == 1 and "контекст без задания" in got[0] and "ось 0" in got[0], str(got))
        _expect(c.matched == control.matched, "несопоставленный контекст засчитан")

    @case("ось 0: задание, производящее обязательный контекст, переименовано")
    def _():
        got, _c = run(lambda r: _inject(r, "    name: golangci-lint\n", "    name: golangci-lint (новое имя)\n"))
        _expect(len(got) == 1 and "'golangci-lint'" in got[0], str(got))

    @case("ось 0, близнец: образец из чужого файла не отнимает контекст у буквального имени")
    def _():
        rival = ("name: соперник\non: workflow_dispatch\njobs:\n  b:\n    name: build ${{ matrix.x }}\n"
                 "    strategy:\n      matrix: ${{ fromJSON(inputs.m) }}\n    runs-on: ubuntu-latest\n"
                 "    steps:\n      - run: echo ok\n")
        got, c = run(extra={new_rel: rival})
        _expect("build · vet · gofmt" in declared0, "контекста, на котором стоит проба, в объявлении нет")
        _expect(not got and new_rel not in c.required_files, f"образец отнял контекст у буквального имени: {got}")

    @case("ось 0, близнец: контекст матрицы выражением сопоставлен образцом")
    def _():
        name = "юниты какой-нибудь-шард"
        _got, c = run(declared=declared0 + [name])
        _expect(c.matched == control.matched + 1, "контекст матрицы выражением не сопоставлен")
        _expect(provider_cut("Postgres-пробы вне отбора интеграционной джобы (пропуск = отказ)")
                == "Postgres-пробы вне отбора интеграционной джобы (пропуск =...", "обрезка не та, что у площадки")
        ls, ps = job_contexts("x", yaml.compose("name: build ${{ matrix.p }}\nstrategy:\n  matrix:\n"
                                                "    p: [a, b]\n", Loader=yaml.SafeLoader))
        _expect(ls == {"build a", "build b"} and not ps, f"матрица списком не развёрнута: {ls} {ps}")

    # ── ось 4 ──
    red_forms = [
        ("через точку", "github.event.pull_request.base.ref == 'main'", "звено `base`"),
        ("base.sha", "github.event.pull_request.base.sha != ''", "звено `base`"),
        ("регистр имён", "GITHUB.EVENT.PULL_REQUEST.BASE.REF == 'main'", "звено `BASE`"),
        ("регистр внутри звена", "github.event.pull_request.bAse.ref == 'main'", "звено `bAse`"),
        ("база очереди слияния", "github.event.merge_group.base_ref == 'refs/heads/main'", "звено `base_ref`"),
        ("двойные кавычки YAML", "\"github.event.pull_request.base.ref == 'main'\"", "звено `base`"),
        ("github.base_ref", "github.base_ref == 'main'", "звено `base_ref`"),
        ("индекс на одном звене", "github.event.pull_request['base'].ref == 'main'", "звено `base`"),
        ("индекс на каждом звене", "github.event['pull_request']['base']['ref'] == 'main'", "звено `base`"),
        ("индекс у base_ref", "github['base_ref'] == 'main'", "звено `base_ref`"),
        ("регистр ключа индекса и ${{ }}", "${{ github.event['pull_request']['BASE']['ref'] == 'main' }}",
         "звено `BASE`"),
        ("пробелы внутри индекса", "github.event[ 'pull_request' ] [ 'base' ].ref == 'main'", "звено `base`"),
        ("группа, затем точка", "(github.event.pull_request).base.ref == 'main'", "звено `base`"),
        ("группа с ИЛИ, затем точка", "(github.event.pull_request || github.event.merge_group).base.ref == 'main'",
         "звено `base`"),
        ("вызов от целого объекта", "fromJSON(toJSON(github.event.pull_request)).base.ref == 'main'",
         "звено `base`"),
        ("группа вокруг всего пути", "(github.event.pull_request.base.ref) == 'main'", "звено `base`"),
        ("фильтр перед base", "contains(github.event.*.base.ref, 'main')", "звено `base`"),
        ("база внутри индекса-выражения", "github.event.pull_request.labels[github.base_ref].name == 'x'",
         "звено `base_ref`"),
        ("окружение", "env.GITHUB_BASE_REF == 'main'", "звено `GITHUB_BASE_REF`"),
        ("выход верблюжьей записью", "needs.prep.outputs.baseRef == 'main'", "звено `baseRef`"),
        ("выход через дефис", "needs.prep.outputs.pr-base == 'main'", "звено `pr-base`"),
        ("фильтр `.*` на месте base", "contains(github.event.pull_request.*.ref, 'main')",
         "неизвестное звено `*` у `pull_request`"),
        ("фильтр `[*]` на месте base", "contains(github.event.pull_request[*].ref, 'main')",
         "неизвестное звено `[*]` у `pull_request`"),
        ("фильтр у корня", "contains(github.*, 'main')", "неизвестное звено `*` у `github`"),
        ("фильтр у окружения", "contains(env.*, 'main')", "неизвестное звено `*` у `env`"),
        ("фильтр у элемента списка запросов", "contains(github.event.workflow_run.pull_requests[0].*.ref, 'main')",
         "неизвестное звено `*` у `pull_requests`"),
        ("индекс выражением", "github.event.pull_request[matrix.side].ref == 'main'",
         "неизвестное звено `[matrix.side]` у `pull_request`"),
        ("индекс вызовом", "github.event.pull_request[format('{0}', 'base')].ref == 'main'",
         "неизвестное звено `[format('{0}', 'base')]` у `pull_request`"),
        ("индекс выражением у группы", "(github.event.pull_request)[matrix.k].ref == 'main'",
         "неизвестное звено `[matrix.k]` у результата группы или вызова"),
        ("индекс за индексом выражением", "github.event[matrix.a][matrix.b].ref == 'main'",
         "неизвестное звено `[matrix.b]` у неизвестного звена"),
        ("фильтр у выходов задания", "contains(needs.prep.outputs.*, 'main')", "неизвестное звено `*` у `outputs`"),
        ("фильтр у события", "contains(github.event.*, 'refs/heads/main')", "неизвестное звено `*` у `event`"),
        ("`steps` полем, а не корнем", "contains(fromJSON(needs.prep.outputs.cfg).steps.*, 'main')",
         "неизвестное звено `*` у `steps`"),
    ]
    for label, cond, link in red_forms:
        @case(f"ось 4: {label}")
        def _(cond=cond, link=link):
            got, _c = cond_job(cond)
            _expect(len(got) == 1 and "задание onlymain:" in got[0] and "читает БАЗУ" in got[0] and link in got[0],
                    f"условие {cond!r}: {got}")

    @case("ось 4: то же чтение базы в условии ШАГА")
    def _():
        got, _c = cond_step("(github.event.pull_request || github.event.merge_group).base.ref == 'main'")
        _expect(len(got) == 1 and "задание onlymain, шаг 1:" in got[0] and "звено `base`" in got[0], str(got))

    twins = [
        ("head через точку", "github.event.pull_request.head.ref == 'lane'"),
        ("github.head_ref", "github.head_ref == 'lane'"),
        ("индекс на head", "github.event.pull_request['head'].ref == 'lane'"),
        ("группа с ИЛИ, затем head", "(github.event.pull_request || github.event.merge_group).head.ref == 'lane'"),
        ("вызов от целого объекта, затем head", "fromJSON(toJSON(github.event.pull_request)).head.ref == 'lane'"),
        ("числовой индекс по меткам", "github.event.pull_request.labels[0].name == 'ci'"),
        ("фильтр у меток", "contains(github.event.pull_request.labels.*.name, 'ci')"),
        ("индекс выражением у меток", "github.event.pull_request.labels[matrix.i].name == 'ci'"),
        ("текст маркера в литерале", "contains(github.event.pull_request.title, 'pull_request.base')"),
        ("литерал с удвоенной кавычкой", "contains(github.event.pull_request.title, 'it''s [''base'']')"),
        ("ключ индекса с удвоенной кавычкой", "github.event.pull_request['head''s'].ref == 'lane'"),
        ("base подстрокой в имени", "vars.DATABASE_REF == 'x'"),
        ("условие по событию", "github.event_name == 'pull_request'"),
        ("фильтр по итогам заданий", "contains(needs.*.result, 'failure')"),
        ("индекс выражением по заданиям", "needs[matrix.job].result == 'success'"),
        ("фильтр по итогам шагов", "contains(steps.*.outcome, 'failure')"),
    ]
    for label, cond in twins:
        @case(f"ось 4, близнец: {label}")
        def _(cond=cond):
            got, c = cond_job(cond)
            _expect(not got, f"условие {cond!r} объявлено читающим базу: {got}")
            _expect(c.conditions == control.conditions + 1, "условие близнеца не осмотрено")

    # ── псевдонимы ──
    reads_base, reads_head = "${{ github.base_ref == 'main' }}", "${{ github.head_ref == 'lane' }}"

    def aliased(anchored: str, on_step: bool) -> str:
        head = f"  onlymain:\n    runs-on: ubuntu-latest\n    env:\n      ONLY_MAIN: &onlymain {anchored}\n"
        if on_step:
            return head + "    steps:\n      - if: *onlymain\n        run: echo ok\n"
        return head + "    if: *onlymain\n    steps:\n      - run: echo ok\n"

    for label, on_step, where in (("условие задания псевдонимом", False, "задание onlymain:"),
                                  ("условие шага псевдонимом", True, "задание onlymain, шаг 1:")):
        @case(f"псевдоним: {label}")
        def _(on_step=on_step, where=where):
            got, c = with_job(aliased(reads_base, on_step))
            _expect(len(got) == 1 and where in got[0] and "звено `base_ref`" in got[0], str(got))
            _expect(c.aliases == control.aliases + 1, "перепись не назвала разрешённый псевдоним")

        @case(f"псевдоним, близнец: {label}, под якорем голова")
        def _(on_step=on_step):
            got, c = with_job(aliased(reads_head, on_step))
            _expect(not got and c.conditions == control.conditions + 1, f"{got}")

    for label, jobs, where in (
        ("задание псевдонимом",
         "  onlymain: &job\n    runs-on: ubuntu-latest\n    if: COND\n    steps:\n      - run: echo ok\n"
         "  onlymaincopy: *job\n", ["задание onlymain:", "задание onlymaincopy:"]),
        ("steps псевдонимом",
         "  onlymain:\n    runs-on: ubuntu-latest\n    steps: &steps\n      - if: COND\n        run: echo ok\n"
         "  onlymaincopy:\n    runs-on: ubuntu-latest\n    steps: *steps\n",
         ["задание onlymain, шаг 1:", "задание onlymaincopy, шаг 1:"]),
        ("шаг псевдонимом",
         "  onlymain:\n    runs-on: ubuntu-latest\n    steps:\n      - &step\n        if: COND\n        run: echo ok\n"
         "  onlymaincopy:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo first\n      - *step\n",
         ["задание onlymain, шаг 1:", "задание onlymaincopy, шаг 2:"]),
    ):
        @case(f"псевдоним: {label} — копия судится на своём месте")
        def _(jobs=jobs, where=where):
            got, c = with_job(jobs.replace("COND", reads_base))
            _expect(len(got) == len(where) and all(w in g for w, g in zip(where, got)), str(got))
            _expect(c.aliases == control.aliases + 1, "псевдоним не посчитан")

        @case(f"псевдоним, близнец: {label}, условие по голове")
        def _(jobs=jobs):
            got, c = with_job(jobs.replace("COND", reads_head))
            _expect(not got and c.conditions == control.conditions + 2, f"{got} {c.conditions}")

    @case("псевдоним: база запроса псевдонимом на ветку ствола, линия снята")
    def _():
        # Якорь обязан стоять РАНЬШЕ псевдонима, поэтому `push` с якорем ставится
        # на место фильтра запроса, а не остаётся там, где он записан в файле.
        def edit(r: str) -> str:
            r = _inject(r, PUSH_BLOCK, "")
            return _inject(r, REVIEW_BLOCK, "  push:\n    branches: [&trunk main]\n"
                                            "  pull_request:\n    branches:\n      - *trunk\n")
        got, _c = run(edit)
        _expect(len(got) == 1 and "недостаёт {`[0-9]+`}" in got[0] and "лишние" not in got[0], str(got))

    # ── отказ на записи, которой провайдер не принимает ──
    job = ("  onlymain: &job\n    runs-on: ubuntu-latest\n    if: ${{ github.head_ref == 'lane' }}\n"
           "    steps:\n      - run: echo ok\n")
    for label, jobs, why in (
        ("ключ слияния", job + "  onlymaincopy:\n    <<: *job\n", "ключ слияния `<<`"),
        ("ключ-псевдоним", "  onlymain:\n    runs-on: ubuntu-latest\n    env:\n      K: &ifkey if\n"
                           "    *ifkey : ${{ github.base_ref == 'main' }}\n    steps:\n      - run: echo ok\n",
         "ключ отображения записан псевдонимом `*ifkey`"),
        ("псевдоним внутрь собственного якоря",
         "  onlymain: &job\n    runs-on: ubuntu-latest\n    services: *job\n    steps:\n      - run: echo ok\n",
         "ведёт внутрь собственного якоря"),
        ("повторённый ключ", "  onlymain:\n    runs-on: ubuntu-latest\n    if: ${{ github.head_ref == 'x' }}\n"
                             "    if: ${{ github.base_ref == 'main' }}\n    steps:\n      - run: echo ok\n",
         "ключ `if` повторён"),
        ("условие отображением", "  onlymain:\n    runs-on: ubuntu-latest\n    if: {base: main}\n"
                                 "    steps:\n      - run: echo ok\n", "задание onlymain: условие `if:` не скаляр"),
        ("задание скаляром", "  onlymain: echo\n", "задание onlymain не отображение"),
        ("steps скаляром", "  onlymain:\n    runs-on: ubuntu-latest\n    steps: echo\n",
         "задание onlymain: `steps` не последовательность"),
        ("шаг скаляром", "  onlymain:\n    runs-on: ubuntu-latest\n    steps:\n      - echo\n",
         "задание onlymain, шаг 1 не отображение"),
    ):
        @case(f"отказ: {label}")
        def _(jobs=jobs, why=why):
            msg = refused(lambda: with_job(jobs))
            _expect(INJECT_REL in msg and why in msg, msg)

    # ── законные записи события ──
    jobs_tail = "jobs:\n  work:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"

    @case("событие: on: pull_request скаляром — любая база")
    def _():
        got, c = run(extra={new_rel: "name: новый\non: pull_request\n" + jobs_tail})
        _expect(len(got) == 1 and new_rel in got[0] and "в ЛЮБУЮ" in got[0], str(got))
        _expect(c.on_review == control.on_review + 1, "файл на запросе не посчитан")

    @case("событие: on: [push, pull_request] — обе оси")
    def _():
        got, _c = run(extra={new_rel: "name: новый\non: [push, pull_request]\n" + jobs_tail})
        joined = "\n".join(got)
        _expect(len(got) == 2 and "в ЛЮБУЮ" in joined and "`push` не сужен по ветке" in joined, str(got))

    @case("событие: branches одиночным скаляром — множество из одного")
    def _():
        got, _c = run(extra={new_rel: "name: новый\non:\n  pull_request:\n    branches: main\n" + jobs_tail})
        _expect(len(got) == 1 and "недостаёт {`[0-9]+`}" in got[0], str(got))

    @case("событие, близнец: файл без запроса — не находка, но в переписи")
    def _():
        got, c = run(extra={new_rel: "name: новый\non: workflow_dispatch\n" + jobs_tail})
        _expect(not got and c.files == control.files + 1 and c.on_review == control.on_review, f"{got}")

    # ── беспредметный вход ──
    @case("беспредметный вход — отказ, а не вердикт")
    def _():
        for label, fn in (
            ("пустой корпус", lambda: audit({}, declared0)),
            ("пустое объявление контекстов", lambda: audit(corpus0, [])),
            ("неразобранное объявление", lambda: audit({**corpus0, new_rel: "{ обрезано"}, declared0)),
            ("объявление без on:", lambda: audit({**corpus0, new_rel: "name: x\njobs: {}\n"}, declared0)),
            ("ни одного файла на запросе", lambda: audit({new_rel: "name: x\non: workflow_dispatch\n" + jobs_tail},
                                                         declared0)),
        ):
            try:
                fn()
            except Unmeasured:
                continue
            raise SelfTestFailure(f"{label} принят за чистый")

    passed = 0
    failed: list[str] = []
    for name, fn in cases:
        try:
            fn()
            passed += 1
        except SelfTestFailure as exc:
            failed.append(f"{name}: {exc}")
        except Exception:  # noqa: BLE001 - поломка пробы — тоже провал, с трассой
            failed.append(f"{name}: {traceback.format_exc()}")
    print(f"самопроверка: подпроб {len(cases)} · прошли {passed} · провалились {len(failed)}")
    for f in failed:
        print(f"  ПРОВАЛ: {f}")
    if not cases:
        return 2
    return 0 if not failed else 1


# ── ТОЧКА ВХОДА ──────────────────────────────────────────────────────────────


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n", 1)[0])
    ap.add_argument("--self-test", action="store_true", help="доказательство инъекцией настоящим входом")
    ap.add_argument("--rev", help="судить ревизию git вместо рабочего дерева (например origin/2796)")
    args = ap.parse_args()
    root = repo_root()
    try:
        if args.self_test:
            return self_test(root)
        corpus, declared = read_tree(root, args.rev)
        findings, census = audit(corpus, declared)
    except Unmeasured as exc:
        print(f"ИЗМЕРЕНИЕ НЕ СДЕЛАНО: {exc}")
        return 2
    print(f"ОБЪЁМ ОСМОТРЕННОГО ({args.rev or 'рабочее дерево'}): {census}")
    if findings:
        print(f"НАХОДОК {len(findings)}:")
        for f in findings:
            print(f"  {f}")
        return 1
    print("находок 0")
    return 0


if __name__ == "__main__":
    sys.exit(main())
