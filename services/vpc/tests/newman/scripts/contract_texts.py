#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Текст отказа объявлен ОДИН раз, и всё, что его обещает наружу, цитирует дословно.

ПРЕДМЕТ. Тон сообщений — часть контракта (`api-conventions.md` §Error-format):
арендатор читает текст, а не только код. У такого текста три места жизни, и
только одно из них — источник:

  * ОБЪЯВЛЕНИЕ — константа в прод-коде сервиса. Она одна;
  * СТРАНИЦА АРЕНДАТОРА — обещание продукта. Прозой, но текст отказа в ней
    приводится дословно, иначе арендатор ищет в ответе не то, что придёт;
  * E2E-КЕЙС — единственный из трёх, кто умеет ПОКРАСНЕТЬ.

Расхождение любых двух — это «два места об одном предмете, из которых верно
одно». Наблюдалось в корпусе многократно; здесь оно особенно тихое: страница
устаревает молча, а кейс, утверждающий свой собственный вариант текста,
продолжает зеленеть на нём же.

ЧТО ИМЕННО СВЕРЯЕТСЯ. Не вся строка формата, а её НЕИЗМЕНЯЕМЫЕ куски — то, что
остаётся после выкидывания подстановок (`%s`, `%d`, …). Подставляемое зависит от
запроса (имя поля, присланное значение) и в странице стоять не обязано; ради
этого куски и вычленяются, а не сравнивается строка целиком.

ПОЧЕМУ ЭТОТ ГЕЙТ ЖИВЁТ У НАБОРА КЕЙСОВ. Потому что его зовёт `validate-cases.py`,
который CI гоняет для КАЖДОЙ суиты на каждом PR (`.github/workflows/ci.yaml`,
шаг «validate-cases»). Собственного места в конвейере у проверки документации нет,
а проверка, которую никто не зовёт, — это текст, а не гейт (ровно тот класс,
который здесь и ловится).

ГЕЙТ ПРОВЕРЯЕТ СВОЮ ПРЕДПОСЫЛКУ. Предпосылка — «объявление лежит там, где
записано, и называется так, как записано». Переименовали константу или увезли
файл → это НАХОДКА, а не молчаливый проход: иначе гейт продолжил бы печатать
«ноль расхождений», не прочитав ни одного байта предмета.

Инъекция в обе стороны — `scripts/selftest_contract_texts.py` (стенд не нужен).
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, List, Sequence

# services/vpc — корень сервиса: scripts → newman → tests → vpc.
SERVICE_ROOT = Path(__file__).resolve().parents[3]

# Кусок формата короче этого в сверку не идёт: в строке вида "%s: %s" между
# подстановками стоят разделители, и требовать их дословного присутствия в прозе
# значило бы ловить форму, а не текст обещания.
MIN_INVARIANT_LEN = 12

# Строковый литерал Go в объявлении константы. `(?:[^"\\]|\\.)*` пропускает
# экранированную кавычку внутри литерала — без этого регулярное выражение
# обрезало бы текст на первой же `\"` и сверяло огрызок.
_GO_CONST_RE = r'{name}\s*=\s*"((?:[^"\\]|\\.)*)"'

# Подстановки Go-формата. Флаги/ширина (`%-5s`, `%+d`, `%.2f`) учтены: иначе
# кусок «5s …» уехал бы в неизменяемую часть и не нашёлся бы ни в одном
# артефакте — гейт краснел бы на верном дереве.
_VERB_RE = re.compile(r"%[-+# 0]*[\d.]*[a-zA-Z]")


@dataclass(frozen=True)
class ContractText:
    """Один текст контракта: где объявлен и кто обязан его цитировать."""

    name: str
    declared_in: str  # путь от корня сервиса
    const_name: str
    quoted_by: Sequence[str]  # пути от корня сервиса
    why: str


@dataclass
class Census:
    """Объём осмотренного. «Ноль находок» обязано быть отличимо от «ноль прочитанного»."""

    texts: int = 0
    files_read: int = 0
    bytes_read: int = 0
    invariants: int = 0
    occurrences: Dict[str, int] = field(default_factory=dict)


# ---------------------------------------------------------------------------
# Состав
# ---------------------------------------------------------------------------

CONTRACT_TEXTS: List[ContractText] = [
    ContractText(
        name="subnet-reserved-prefix-overlap",
        # Объявление переехало вместе с производителем отказа: текст и машинный
        # признак полосы (`SUBNET_CIDR_RESERVED`) собирает одна функция, и текст
        # живёт рядом с ней. Копии в use-case больше нет — иначе тон разошёлся бы
        # с признаком молча, ровно там, где деталь читают машиной.
        declared_in="internal/apps/kacho/shared/serviceerr/reservedcidr.go",
        const_name="reservedOverlapFormat",
        quoted_by=(
            "docs/content/api/subnet.mdx",
            "tests/newman/cases/subnet.py",
        ),
        why=(
            "подсеть поверх диапазона, зарезервированного платформой, отвергается "
            "синхронно; арендатор узнаёт об этом ТОЛЬКО из текста отказа — перечень "
            "служебных диапазонов не публикуется, и догадаться по коду 400 нельзя"
        ),
    ),
]


# ---------------------------------------------------------------------------
# Разбор
# ---------------------------------------------------------------------------

def _unescape_go(literal: str) -> str:
    """Go-литерал → текст. Достаточно трёх экранов: кавычка, слэш, перевод строки."""
    return literal.replace('\\"', '"').replace("\\n", "\n").replace("\\\\", "\\")


def invariant_parts(fmt: str) -> List[str]:
    """Неизменяемые куски формата — то, что обязано стоять в каждом артефакте."""
    return [p.strip() for p in _VERB_RE.split(fmt) if len(p.strip()) >= MIN_INVARIANT_LEN]


def declared_text(root: Path, ct: ContractText) -> str:
    """Текст контракта из его ЕДИНСТВЕННОГО объявления. Пусто → предпосылка не выполнена."""
    src = root / ct.declared_in
    if not src.is_file():
        return ""
    m = re.search(_GO_CONST_RE.format(name=re.escape(ct.const_name)), src.read_text())
    return _unescape_go(m.group(1)) if m else ""


# ---------------------------------------------------------------------------
# Аудит
# ---------------------------------------------------------------------------

def audit(root: Path | None = None) -> tuple[List[str], Census]:
    """Находки + перепись. Пустой список находок при нулевой переписи — не «чисто»."""
    root = Path(root) if root is not None else SERVICE_ROOT
    findings: List[str] = []
    census = Census()

    for ct in CONTRACT_TEXTS:
        census.texts += 1
        src = root / ct.declared_in
        if not src.is_file():
            findings.append(
                f"{ct.name}: ПРЕДПОСЫЛКА — объявления нет по адресу {ct.declared_in}. "
                f"Файл переехал или удалён; пока адрес не поправлен, сверять нечего, "
                f"и молчаливый проход означал бы «ноль прочитанного», а не «ноль расхождений»."
            )
            continue
        census.files_read += 1
        census.bytes_read += len(src.read_bytes())

        fmt = declared_text(root, ct)
        if not fmt:
            findings.append(
                f"{ct.name}: ПРЕДПОСЫЛКА — в {ct.declared_in} нет строковой константы "
                f"{ct.const_name}. Её переименовали или сделали не литералом. Текст контракта "
                f"обязан жить одним объявлением — поправь адрес здесь ЛИБО верни объявление."
            )
            continue

        parts = invariant_parts(fmt)
        if not parts:
            findings.append(
                f"{ct.name}: ПРЕДПОСЫЛКА — у {ct.const_name} нет неизменяемого куска длиннее "
                f"{MIN_INVARIANT_LEN} символов (формат: {fmt!r}). Сверять нечего: такой текст "
                f"состоит из подстановок, и цитировать в документации в нём нечего."
            )
            continue
        census.invariants += len(parts)

        for rel in ct.quoted_by:
            dst = root / rel
            if not dst.is_file():
                findings.append(
                    f"{ct.name}: {rel} — файла нет. Он объявлен цитирующим текст контракта "
                    f"({ct.why}); отсутствие адреса — находка, а не пропуск."
                )
                continue
            body = dst.read_text()
            census.files_read += 1
            census.bytes_read += len(body.encode())
            hit = sum(body.count(p) for p in parts)
            census.occurrences[rel] = census.occurrences.get(rel, 0) + hit
            missing = [p for p in parts if p not in body]
            if missing:
                findings.append(
                    f"{ct.name}: {rel} не цитирует текст отказа дословно — нет куска "
                    f"{missing[0]!r}.\n"
                    f"    Объявление: {ct.declared_in} :: {ct.const_name} = {fmt!r}\n"
                    f"    Зачем: {ct.why}.\n"
                    f"    → приведи цитату в соответствие объявлению (а НЕ наоборот: "
                    f"источник — прод-код)."
                )

    return findings, census


def format_census(census: Census) -> str:
    seen = ", ".join(f"{k}×{v}" for k, v in sorted(census.occurrences.items())) or "—"
    return (
        f"contract-texts: осмотрено {census.texts} текст(ов) контракта, "
        f"{census.files_read} файл(ов), {census.bytes_read} байт, "
        f"{census.invariants} неизменяемых куск(ов); цитирований: {seen}"
    )
