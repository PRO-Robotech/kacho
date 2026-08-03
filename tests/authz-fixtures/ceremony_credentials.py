#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Единственное машиночитаемое объявление: какие предъявители машинный посев
выковать НЕ может, и какие коллекции из-за этого идут ВОЛНОЙ ЦЕРЕМОНИИ.

ЗАЧЕМ ОТДЕЛЬНЫЙ ФАЙЛ, а не строка в прогонщике.
Перечень, выписанный руками, в этом дереве уже разъезжался с деревом: один список
репозиториев жил в трёх копиях, и в одной не хватало сервиса — механизм печатал
«skip» и выходил успехом. Сегодня тот же перечень существует в ЧЕТЫРЁХ местах и
ни одно из них не читается машиной:

  * `prodseed_all.py`  — прозой в докстроке («Deliberately NOT minted: …»);
  * `prodseed_matrix.py` — прозой в докстроке, с разбором по каждому предъявителю;
  * приёмка IAM-INT-1 §1.7 — множеством `U={…}` внутри команды пересчёта;
  * прогонщик — тем, что он ЭТИ коллекции просто гоняет вместе со всеми.

Проза не исполняется, поэтому расхождение между ней и деревом не может покраснеть.
Здесь объявление одно, и у него есть читатели: волна церемонии, вычитание
делегированных коллекций у прогонщика iam и гейт, требующий, чтобы у каждой
записи был предмет.

ЧТО ЗДЕСЬ ОБЪЯВЛЕНО — ДВА РАЗНЫХ ОСНОВАНИЯ, и их нельзя складывать в одно:

  1. `CEREMONY_ONLY_ENV` — ПЕРЕМЕННАЯ ОКРУЖЕНИЯ, значение которой машинный посев
     не может произвести. Признак шага — его предъявитель берётся из этой
     переменной.
  2. `HUMAN_CALLER_REQUESTS` — ФОРМА ЗАПРОСА, которой человеческий вызывающий
     нужен структурно, независимо от того, какую переменную шаг назвал. Аккаунт
     принадлежит пользователю by construction, поэтому его создание не бывает
     машинным, и отдельной переменной под это нет.

Второе основание существует именно потому, что первое его не видит: критерий по
переменной покрывает меньше половины класса (замер приёмки — 87 упавших
утверждений из 161). Волна, отобранная только по переменным, оставила бы
остальное там же, где оно и было.

Использование (машинно, из shell и из Python):
    python3 ceremony_credentials.py --stems  --suite services/iam/tests/newman
    python3 ceremony_credentials.py --report --root .
    python3 ceremony_credentials.py --verify --root .     # гейт: у записи есть предмет
"""
from __future__ import annotations

import argparse
import glob
import json
import os
import re
import sys

# ─────────────────────────────────────────────────────────────────────────────
# 1. Предъявители, которых машинный посев выковать НЕ МОЖЕТ
# ─────────────────────────────────────────────────────────────────────────────
# Ключ — имя переменной окружения newman. Значение — ПОЧЕМУ посев не может её
# заполнить, в терминах продукта, а не харнесса. Причина обязательна: запись без
# причины через полгода неотличима от забытой.
#
# Каждая запись обязана иметь ПРЕДМЕТ — хотя бы один шаг в дереве, который её
# называет. Запись, которой больше нечего отбирать, — находка (`--verify`), а не
# безобидный остаток: её унаследует следующий читатель как действующее ограничение.
CEREMONY_ONLY_ENV: dict[str, str] = {
    "jwtAccountAdminAStepUp": (
        "предъявитель человека, поднявшего уровень аутентификации вторым фактором. "
        "Машинный принципал уровня не несёт вовсе (у client_credentials-токена нет "
        "соответствующего утверждения), а поднять его можно только церемонией входа."
    ),
    "apiTokenExpired": (
        "предъявитель с ИСТЁКШИМ сроком. Срок назначает и подписывает провайдер, "
        "поэтому «уже истёкший» на момент посева не производится в принципе: нужна "
        "волна, которая условие СОЗДАЁТ — выпустить и переждать срок."
    ),
}

# ─────────────────────────────────────────────────────────────────────────────
# 2. Формы запроса, которым человеческий вызывающий нужен СТРУКТУРНО
# ─────────────────────────────────────────────────────────────────────────────
# Здесь не переменная, а форма: у такого шага может стоять любой предъявитель, и
# всё равно нужен человек. Регулярное выражение прикладывается к сериализованному
# `request.url` коллекции (метод сверяется отдельно).
HUMAN_CALLER_REQUESTS: list[dict[str, str]] = [
    {
        "id": "account-create",
        "method": "POST",
        "url_re": r'/iam/v1/accounts(?:"|\?)|"accounts"\s*\]',
        "why": (
            "аккаунт принадлежит пользователю by construction — у создания аккаунта "
            "нет машинного вызывающего, и владелец выводится из принципала, а не из тела."
        ),
    },
]

# ─────────────────────────────────────────────────────────────────────────────
# 3. Кто СОЗДАЁТ условие волны
# ─────────────────────────────────────────────────────────────────────────────
# Посев церемонии — артефакт стадии S2 (церемония входа доведена до предъявителя,
# который край принимает). Путь объявлен ЗДЕСЬ и читается всеми тремя сторонами:
# волной, планировщиком волн и гейтом. Пока файла нет, условие создать нечем —
# и это ОТКРЫТЫЙ ДОЛГ С ЧИСЛОМ (`--debt`), а не зелёная суита и не молчание.
#
# Предикат «файл появился» — ВНЕШНИЙ по отношению к этому изменению: его
# выполняет чужая стадия. Поэтому самоистечение здесь не может быть отменено
# собственной правкой (класс «самоистечение, обнулённое своим же изменением»).
CEREMONY_SEED = "tests/authz-fixtures/prodseed_ceremony.py"

_BEARER_RE = re.compile(r"bearer from env '([^']+)'")
_COLLECTION_GLOBS = (
    "gateway/tests/newman/collections/*.json",
    "services/*/tests/newman/collections/*.json",
)


def _walk(items, path):
    for x in items:
        if "item" in x:
            yield from _walk(x["item"], path + [x.get("name", "")])
        else:
            yield x, path


def _scripts(item):
    out = {}
    for ev in item.get("event", []) or []:
        listen = ev.get("listen")
        exec_ = ((ev.get("script") or {}).get("exec")) or []
        if listen:
            out[listen] = exec_
    return out


def _bearer_var(scripts) -> str | None:
    # Предъявитель объявляется первой строкой пре-скрипта, который пишет gen.py
    # (`// bearer from env '<var>'`). Читаем только начало блока: дальше в том же
    # скрипте встречаются другие имена переменных, и они предъявителем не являются.
    for line in (scripts.get("prerequest") or [])[:3]:
        m = _BEARER_RE.search(line)
        if m:
            return m.group(1)
    return None


def scan(root: str = ".") -> dict:
    """Обойти коллекции дерева и вернуть разбор по КАЖДОМУ основанию отдельно.

    Возвращает: {"collections": {path: {...}}, "by_var": {...}, "by_shape": {...},
                 "files_read": N, "leaves_read": N}
    Число прочитанных файлов и листьев возвращается ВСЕГДА — «ноль находок»
    обязано быть отличимо от «ноль прочитанного».
    """
    per_coll: dict[str, dict] = {}
    by_var: dict[str, int] = {v: 0 for v in CEREMONY_ONLY_ENV}
    by_shape: dict[str, int] = {s["id"]: 0 for s in HUMAN_CALLER_REQUESTS}
    files_read = 0
    leaves_read = 0

    files = []
    for g in _COLLECTION_GLOBS:
        files.extend(glob.glob(os.path.join(root, g)))
    for f in sorted(set(files)):
        try:
            with open(f, encoding="utf-8") as fh:
                doc = json.load(fh)
        except (OSError, ValueError):
            continue
        files_read += 1
        rel = os.path.relpath(f, root)
        for item, path in _walk(doc.get("item", []) or [], []):
            leaves_read += 1
            scripts = _scripts(item)
            asserts = sum(l.count("pm.test(") for block in scripts.values() for l in block)
            case = path[0] if path else (item.get("name") or "")

            var = _bearer_var(scripts)
            if var in CEREMONY_ONLY_ENV:
                by_var[var] += 1
                e = per_coll.setdefault(rel, _empty_entry())
                e["steps"] += 1
                e["asserts"] += asserts
                e["cases"].add(case)
                e["reasons"].add(f"env:{var}")

            req = item.get("request") or {}
            raw_url = json.dumps(req.get("url"))
            for shape in HUMAN_CALLER_REQUESTS:
                if req.get("method") != shape["method"]:
                    continue
                if re.search(shape["url_re"], raw_url):
                    by_shape[shape["id"]] += 1
                    e = per_coll.setdefault(rel, _empty_entry())
                    e["steps"] += 1
                    e["asserts"] += asserts
                    e["cases"].add(case)
                    e["reasons"].add(f"shape:{shape['id']}")
                    break

    for e in per_coll.values():
        e["cases"] = sorted(e["cases"])
        e["reasons"] = sorted(e["reasons"])
    return {
        "collections": per_coll,
        "by_var": by_var,
        "by_shape": by_shape,
        "files_read": files_read,
        "leaves_read": leaves_read,
    }


def _empty_entry() -> dict:
    return {"steps": 0, "asserts": 0, "cases": set(), "reasons": set()}


def stems_for_suite(suite_dir: str, root: str = ".") -> list[str]:
    """Имена коллекций (stem) внутри одной суиты, которые идут волной церемонии.

    Отбор — по дереву, на КАЖДОМ вызове: перечень не хранится и потому не может
    разойтись с коллекциями, которые gen.py выпустил в этот раз.
    """
    res = scan(root)
    want = os.path.normpath(os.path.join(suite_dir, "collections"))
    stems = []
    for rel in res["collections"]:
        if os.path.normpath(os.path.dirname(rel)) == want:
            base = os.path.basename(rel)
            for suffix in (".postman_collection.json", ".json"):
                if base.endswith(suffix):
                    stems.append(base[: -len(suffix)])
                    break
    return sorted(set(stems))


def verify(root: str = ".") -> tuple[int, list[str]]:
    """Гейт: у каждой записи объявления обязан быть ПРЕДМЕТ в дереве.

    Проверяется в обе стороны:
      * запись, не отобравшая ни одного шага, — находка (истекла сама);
      * ноль прочитанных файлов — ОТКАЗ, а не «чисто».
    """
    problems: list[str] = []
    res = scan(root)
    if res["files_read"] == 0:
        problems.append(
            "прочитано НОЛЬ коллекций — объявление не проверено ни против чего. "
            "Это отказ, а не 'предмет у всех записей есть'."
        )
        return 2, problems
    for var, n in res["by_var"].items():
        if n == 0:
            problems.append(
                f"CEREMONY_ONLY_ENV['{var}'] не отбирает ни одного шага в дереве "
                f"({res['leaves_read']} листьев в {res['files_read']} коллекциях) — "
                f"записи больше нечего отбирать. Снять её или назвать шаг, ради "
                f"которого она стоит."
            )
    for shape in HUMAN_CALLER_REQUESTS:
        if res["by_shape"][shape["id"]] == 0:
            problems.append(
                f"HUMAN_CALLER_REQUESTS['{shape['id']}'] не отбирает ни одного шага "
                f"— форма запроса из дерева ушла, а запись осталась."
            )
    return (1 if problems else 0), problems


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--root", default=".", help="корень монорепо")
    ap.add_argument("--suite", help="каталог суиты (…/tests/newman) — печатать её stem'ы")
    ap.add_argument("--stems", action="store_true", help="печатать stem'ы по одному в строке")
    ap.add_argument("--report", action="store_true", help="печатать разбор с числами")
    ap.add_argument("--json", action="store_true", help="печатать разбор как JSON")
    ap.add_argument("--verify", action="store_true", help="гейт: у каждой записи есть предмет")
    ap.add_argument("--seed-path", action="store_true", help="печатать объявленный путь посева церемонии")
    ap.add_argument("--seed-exists", action="store_true",
                    help="код 0, если посев церемонии есть в дереве; 1 — если нет")
    ap.add_argument("--debt", action="store_true",
                    help="печатать ОТКРЫТЫЙ ДОЛГ с числами (условие волны создать нечем)")
    a = ap.parse_args(argv)

    if a.seed_path:
        print(os.path.join(a.root, CEREMONY_SEED))
        return 0

    if a.seed_exists:
        return 0 if os.path.exists(os.path.join(a.root, CEREMONY_SEED)) else 1

    if a.debt:
        res = scan(a.root)
        steps = sum(e["steps"] for e in res["collections"].values())
        asserts = sum(e["asserts"] for e in res["collections"].values())
        cases = sum(len(e["cases"]) for e in res["collections"].values())
        print("ОТКРЫТЫЙ ДОЛГ: волна церемонии не может создать своё условие —")
        print(f"  посева нет по объявленному пути {CEREMONY_SEED} (артефакт стадии S2).")
        print(f"  НЕ ПРОВЕРЯЕТСЯ: {steps} шаг(ов), {cases} кейс(ов), {asserts} утверждени(й) "
              f"в {len(res['collections'])} коллекц(иях).")
        print("  Эти шаги сейчас исполняются машинным принципалом — то есть проходят или")
        print("  падают НЕ по тому основанию, которое кейс описывает. Долг сосчитан и назван;")
        print("  он НЕ вычитается ни из какого вердикта и НЕ засчитывается зелёным.")
        for rel, e in sorted(res["collections"].items()):
            print(f"      {e['steps']:3d} шаг / {e['asserts']:4d} утв  {os.path.basename(rel)}"
                  f"  [{', '.join(e['reasons'])}]")
        return 0

    if a.verify:
        rc, problems = verify(a.root)
        res = scan(a.root)
        print(f"объявление церемонии: {len(CEREMONY_ONLY_ENV)} переменн(ых) + "
              f"{len(HUMAN_CALLER_REQUESTS)} форм(а) запроса")
        print(f"осмотрено: {res['files_read']} коллекц(ий), {res['leaves_read']} шаг(ов)")
        for p in problems:
            print(f"  НАХОДКА: {p}", file=sys.stderr)
        if rc == 0:
            print("у каждой записи объявления есть предмет в дереве.")
        return rc

    if a.stems:
        if not a.suite:
            print("--stems требует --suite <…/tests/newman>", file=sys.stderr)
            return 2
        for s in stems_for_suite(a.suite, a.root):
            print(s)
        return 0

    res = scan(a.root)
    if a.json:
        print(json.dumps(res, ensure_ascii=False, indent=2, sort_keys=True))
        return 0

    # --report (по умолчанию)
    total_steps = sum(e["steps"] for e in res["collections"].values())
    total_asserts = sum(e["asserts"] for e in res["collections"].values())
    total_cases = sum(len(e["cases"]) for e in res["collections"].values())
    print(f"осмотрено: {res['files_read']} коллекц(ий), {res['leaves_read']} шаг(ов)")
    print(f"класс «нужен человеческий вызывающий»: {total_steps} шаг(ов), "
          f"{total_cases} кейс(ов), {total_asserts} утверждени(й), "
          f"{len(res['collections'])} коллекц(ий)")
    print()
    print("по переменной предъявителя:")
    for var, n in sorted(res["by_var"].items()):
        print(f"    {n:4d}  {var}")
    print("по форме запроса:")
    for sid, n in sorted(res["by_shape"].items()):
        print(f"    {n:4d}  {sid}")
    print()
    print("коллекции волны церемонии:")
    for rel, e in sorted(res["collections"].items()):
        print(f"    {e['steps']:3d} шаг / {e['asserts']:4d} утв  {rel}  [{', '.join(e['reasons'])}]")
    return 0


if __name__ == "__main__":
    sys.exit(main())
