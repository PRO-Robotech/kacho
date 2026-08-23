#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""e2e-ban6-domains.py — ЕДИНЫЙ предикат популяции запрета #6.

ПРЕДМЕТ. У запрета #6 («Internal*-методы не выставляются на внешний листенер»)
предмет есть ровно там, где внутренний метод ГДЕ-ТО обслуживается. Контракт,
который не регистрирует ни один композиционный корень, недостижим на внешнем
листенере by construction — и засчитать это в изоляцию значило бы получить
зелёное из отсутствия. Именно поэтому встречный контроль пробы
(`assert-ban6-external-isolation.py`, CONTROL-COUNTERPART) роняет прогон на
домене, чей метод не подтверждён обслуженным ни на одном внутреннем листенере:
«метода нет на внешнем» неотличимо от «метода нет нигде».

ПОЧЕМУ ОДИН МОДУЛЬ, А НЕ ДВА ПРЕДИКАТА. Гейт покрытия шардов и проба ban #6
спрашивают об ОДНОЙ популяции. Две реализации разошлись бы молча — и разошлись
бы именно там, где расхождение не видно: обе отвечают «да» на очевидном входе.
Замер 2026-08-23, ДО сведения: гейт покрытия обходил каталог домена целиком и
видел 9 доменов, проба адресовала только `proto/kacho/cloud/*/v1/*.proto` и
видела 8 — девятый (`subscription`, чей контракт лежит вне `v1/`) не измерял
НИКТО, и заметить это можно было лишь сложением двух чисел, которых рядом никто
не печатал.

САМОИСТЕЧЕНИЕ ВМЕСТО ВЕДОМОСТИ ПРОЩЁННЫХ. Домен, чей контракт приземлён, но ещё
не провязан ни одним композиционным корнем, НЕ заносится в список исключений:
список пережил бы свой предмет, а снимать его было бы некому. Он ВЫВОДИТСЯ ИЗ
ДЕРЕВА — и в тот день, когда регистрация появится в прод-коде, домен войдёт в
популяцию сам, а гейт покрытия покраснеет, пока его не возьмёт шард. Ведомости,
которую надо помнить, здесь нет ни одной строки.

ОБЪЁМ ОСМОТРЕННОГО ВОЗВРАЩАЕТСЯ ЧИСЛАМИ. «Ноль находок» обязано быть отличимо
от «ноль прочитанного»: перепись несёт число прочитанных .proto, число доменов и
число найденных регистраций, и потребитель обязан их печатать.

ЕДИНИЦА СЧЁТА — ОТСЛЕЖИВАЕМЫЙ git-элемент. Регистрации ищутся `git grep` по
дереву индекса, а не обходом диска: сборка и генерация кладут в рабочую копию
файлы, которых в дереве нет, и счёт по диску давал бы разные ответы до и после
прогона.
"""
from __future__ import annotations

import pathlib
import re
import subprocess

# Каталог доменных контрактов. Обходится ЦЕЛИКОМ, а не по образцу `*/v1/*.proto`:
# раскладка `v1/` — соглашение, а не инвариант, и домен, положивший контракт
# рядом, выпадал бы из популяции молча.
PROTO_BASE = ("proto", "kacho", "cloud")

# Объявление gRPC-службы, чьё имя выводит её из внешнего маршрутизатора
# (`gateway/internal/allowlist` судит по префиксу имени).
SERVICE_DECL = re.compile(r"^\s*service\s+(Internal\w*)\s*\{", re.M)

# Регистрация службы на gRPC-сервере — ОБЕ законные формы, а не одна.
#
# Сгенерированная обёртка `Register…Server(s, srv)` — та, которой пользуется всё
# дерево сегодня; `s.RegisterService(&…_ServiceDesc, srv)` — та же регистрация
# напрямую через `grpc.ServiceRegistrar`. Второй формы в дереве на 2026-08-23 нет
# ни одной, и предикат узнаёт её ИМЕННО ПОЭТОМУ: он обязан отвечать про
# регистрацию, а не про сегодняшнюю привычку её записывать. Иначе провязка,
# сделанная вторым способом, читалась бы как «домена нет в предмете» — то есть
# сужение стало бы маской ровно там, где его нельзя проверить.
#
# Сгенерированные объявления обеих форм живут в `pkg/api/` и из поиска
# исключаются: иначе «зарегистрирован» означало бы «стаб сгенерирован», то есть
# всегда.
#
# Шаблон исполняют ДВА движка — `git grep -E` отбирает строки, Python достаёт из
# них имя, — поэтому в нём нет ни одной конструкции, которая есть только в одном.
# Ленивый квантификатор `*?` из первой редакции ERE не понимает: git grep строку
# не отбирал, и вторая форма молча не находилась ВООБЩЕ. Поймано парой на
# синтетическом дереве, а не чтением.
REGISTER_CALL = re.compile(
    r"Register(Internal\w*)Server\s*\("
    r"|RegisterService\s*\(\s*&\s*[A-Za-z0-9_.]*(Internal\w+)_ServiceDesc")

_CACHE: dict[str, dict] = {}


def _git(root: pathlib.Path, *args: str) -> tuple[int, str]:
    r = subprocess.run(["git", "-C", str(root), *args], capture_output=True, text=True)
    return r.returncode, r.stdout


def internal_services(root: pathlib.Path) -> tuple[dict[str, list[str]], int]:
    """домен → отсортированные имена `service Internal*`; плюс число прочитанных .proto.

    Читается ИЗ ДЕРЕВА, а не списком в коде: список разъехался бы с proto молча, и
    новый домен с Internal-контрактом выпал бы из охвата, не покраснев нигде.
    """
    out: dict[str, list[str]] = {}
    files_read = 0
    base = root.joinpath(*PROTO_BASE)
    if not base.is_dir():
        return out, 0
    for dom in sorted(base.iterdir()):
        if not dom.is_dir():
            continue
        found: set[str] = set()
        for f in sorted(dom.rglob("*.proto")):
            files_read += 1
            found |= set(SERVICE_DECL.findall(f.read_text(encoding="utf-8")))
        if found:
            out[dom.name] = sorted(found)
    return out, files_read


def production_registrations(root: pathlib.Path) -> dict[str, list[str]]:
    """имя `service Internal*` → прод-файлы, регистрирующие его на gRPC-сервере.

    Прод — это НЕ `_test.go` и НЕ `pkg/api/`. Первое отсекается потому, что
    внутрипроцессный харнесс поднимает службу для самого себя и внешнего
    листенера у него нет; второе — потому, что там лежит сгенерированное
    ОБЪЯВЛЕНИЕ функции, а не её вызов.
    """
    rc, out = _git(root, "grep", "-n", "-E", REGISTER_CALL.pattern, "--", "*.go")
    if rc not in (0, 1):
        raise SystemExit(f"FATAL: git grep не отработал в {root} — предпосылка "
                         f"предиката отпала, чинить надо предикат, а не молчать")
    hits: dict[str, list[str]] = {}
    for line in out.splitlines():
        path, _, rest = line.partition(":")
        if path.endswith("_test.go") or path.startswith("pkg/api/"):
            continue
        for groups in REGISTER_CALL.findall(rest):
            name = next((g for g in groups if g), "")
            if not name:
                continue
            hits.setdefault(name, []).append(path)
    for k in hits:
        hits[k] = sorted(set(hits[k]))
    return hits


def census(root: pathlib.Path) -> dict:
    """Перепись популяции ban #6 — предмет, охват и объём осмотренного.

    Ключи:
      services       — домен → имена Internal*-служб его контракта;
      registrations  — имя службы → прод-файлы, её регистрирующие;
      served         — домены, у ban #6 для которых ЕСТЬ предмет (хотя бы одна
                       служба провязана прод-кодом);
      unserved       — домен → службы, чей контракт приземлён, но не провязан:
                       у ban #6 для них предмета НЕТ, и это печатается, а не
                       умалчивается;
      proto_files_read / domains_with_contract / registrations_found —
                       объём осмотренного.
    """
    key = str(root.resolve())
    if key in _CACHE:
        return _CACHE[key]
    services, files_read = internal_services(root)
    regs = production_registrations(root)
    served: set[str] = set()
    unserved: dict[str, list[str]] = {}
    for dom, names in services.items():
        wired = [n for n in names if regs.get(n)]
        if wired:
            served.add(dom)
        else:
            unserved[dom] = list(names)
    result = {
        "services": services,
        "registrations": regs,
        "served": served,
        "unserved": unserved,
        "proto_files_read": files_read,
        "domains_with_contract": len(services),
        "registrations_found": sum(len(v) for v in regs.values()),
    }
    _CACHE[key] = result
    return result


def invalidate() -> None:
    """Забыть переписи — для проб, которые меняют СИНТЕТИЧЕСКОЕ дерево на ходу.

    Кеш законен потому, что состав дерева за прогон не меняется: проба, пишущая в
    дерево, из которого запущена, — сама по себе находка. Синтетическое дерево
    пробы под это правило не подпадает (оно лежит вне репозитория и заводится
    самой пробой), поэтому ей нужен явный способ сказать «я его изменила» — вместо
    того чтобы лезть в приватное имя модуля.
    """
    _CACHE.clear()
