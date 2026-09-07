#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Гейт: адрес собственного фронта прогонщик ЧИТАЕТ, а не выписывает.

ПРЕДМЕТ. Порт слушателя, его транспорт и имя Service объявляет ПОСАДКА. Прогонщик,
выписавший их у себя, заводит второе место об одном предмете — и разойдётся оно
МОЛЧА: прогон продолжит идти по прежнему адресу и назовёт чужой ответ ответом
фронта. Заметить это по вердикту нельзя.

ЧТО ИМЕННО УТВЕРЖДАЕТСЯ, тремя предикатами:

  1. прогонщик, называющий переменную фронта, ЗОВЁТ единственного производителя
     адреса (`own-rest-front-address.py`);
  2. значение переменной СОБИРАЕТСЯ из того, что производитель вернул: в нём
     стоит подстановка, а не выписанная схема. Схема здесь не формальность —
     на стенде разработки фронты открыты, на боевой посадке под TLS, и
     обращение открытым текстом к слушателю под TLS ответа не даёт ВОВСЕ;
  3. производитель в дереве ОДИН: второй разошёлся бы с первым молча.

ПОЧЕМУ СХЕМА СУДИТСЯ ПО ФОРМЕ, А НЕ ПО НОМЕРУ ПОРТА. Запрет «не писать 9098»
устарел бы вместе с чартом и перестал бы что-либо запрещать, ничем этого не
показав. Форма «значение начинается с выписанной схемы» от номеров не зависит.

Перепись печатает объём осмотренного: «ноль находок» обязано быть отличимо от
«ноль прочитанного».

Самопроверка: `--self-test`.
"""
from __future__ import annotations

import argparse
import pathlib
import re
import sys

REPO = pathlib.Path(__file__).resolve().parents[2]
PRODUCER_NAME = "own-rest-front-address.py"
FRONT_VARS = ("ownRestBaseUrl", "ownInternalRestBaseUrl")

# ФОРМ ЗАПИСИ ДВЕ, И ОБЕ ЗАКОННЫ. Прямая — имя переменной проб стоит в строке;
# косвенная — имя пришло параметром помощника, и в аргументе стоит подстановка
# (`--env-var "$var=..."`). Распознаватель, знающий только первую, на втором виде
# МОЛЧИТ: не красное и не зелёное, а невидимость — то есть ровно то, ради чего
# гейт заведён, уходит из-под наблюдения. Поэтому вторая форма распознаётся
# наравне, а слепота распознавателя названа находкой (см. ниже).
RE_ASSIGN_DIRECT = re.compile(r"(?P<var>ownRestBaseUrl|ownInternalRestBaseUrl)=(?P<val>[^\"'\s]*)")
# Косвенная форма судится ШИРЕ предмета — по всем присвоениям через подстановку в
# файле, который фронты называет. Сузить её до «только фронтов» нечем: имя пришло
# параметром, и статически оно неизвестно. Правило от этого не портится (оно верно
# для любого адреса, собираемого прогонщиком), но перепись считает надмножество —
# поэтому её строка так и называется, а не выдаёт число за «присвоений фронта».
RE_ASSIGN_INDIRECT = re.compile(r"--env-var\s+\"\$\{?(?P<var>\w+)\}?=(?P<val>[^\"]*)\"")
# Выписанная схема в начале значения — то, что запрещено.
RE_LITERAL_SCHEME = re.compile(r"^https?://")


def audit(files, producers):
    findings, census = [], {"прогонщиков осмотрено": 0, "присвоений по подстановке осмотрено": 0,
                            "производителей адреса": len(producers)}
    if len(producers) != 1:
        findings.append(
            f"производителей адреса фронта {len(producers)}, а должен быть ровно один: "
            f"второй разойдётся с первым молча ({', '.join(sorted(producers)) or 'ни одного'})")

    for path, text in files:
        census["прогонщиков осмотрено"] += 1
        # Судится ИСПОЛНЯЕМАЯ часть: имя переменной встречается и в пояснениях,
        # и гейт по подстроке краснел бы на собственном объяснении.
        code = "\n".join(l for l in text.splitlines() if not l.lstrip().startswith("#"))
        names = [v for v in FRONT_VARS if v in code]
        if not names:
            continue
        if PRODUCER_NAME not in code:
            findings.append(
                f"{path}: называет {', '.join(names)}, но производителя адреса "
                f"({PRODUCER_NAME}) не зовёт — значит адрес выписан")
        here = 0
        for rx in (RE_ASSIGN_DIRECT, RE_ASSIGN_INDIRECT):
            for m in rx.finditer(code):
                here += 1
                census["присвоений по подстановке осмотрено"] += 1
                val = m.group("val")
                if RE_LITERAL_SCHEME.match(val):
                    findings.append(
                        f"{path}: {m.group('var')} собирается из ВЫПИСАННОЙ схемы ({val!r}); "
                        f"транспорт слушателя объявляет посадка и меняется вместе с профилем")
                elif "$" not in val:
                    findings.append(
                        f"{path}: {m.group('var')} присвоено значение без подстановки ({val!r}) — "
                        f"адрес не читается у посадки")
        if here == 0:
            # СЛЕПОТА РАСПОЗНАВАТЕЛЯ — НАХОДКА, А НЕ МОЛЧАНИЕ. Прогонщик назвал
            # переменную фронта, а ни одного присвоения не нашлось: значит форма
            # записи распознавателю неизвестна, и всё, что ею записано, стоит вне
            # наблюдения. Зелёное здесь означало бы «не искал».
            findings.append(
                f"{path}: называет {', '.join(names)}, но распознаватель не нашёл ни одного "
                f"присвоения — форма записи ему неизвестна, и записанное ею вне наблюдения")
    return census, findings


def collect(root):
    scripts = root / "deploy" / "scripts"
    files = [(p.relative_to(root).as_posix(), p.read_text(encoding="utf-8"))
             for p in sorted(scripts.glob("newman-*.sh"))]
    producers = [p.relative_to(root).as_posix() for p in sorted(root.rglob(PRODUCER_NAME))]
    return files, producers


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--root", default=str(REPO))
    ap.add_argument("--self-test", action="store_true")
    args = ap.parse_args(argv)
    if args.self_test:
        return self_test()

    files, producers = collect(pathlib.Path(args.root))
    census, findings = audit(files, producers)
    print("перепись: " + " · ".join(f"{k} {v}" for k, v in census.items()))
    if census["прогонщиков осмотрено"] == 0:
        print("ОТКАЗ: прогонщиков не прочитано — вердикт беспредметен", file=sys.stderr)
        return 1
    if findings:
        print(f"НАХОДКИ ({len(findings)}):", file=sys.stderr)
        for f in findings:
            print("  " + f, file=sys.stderr)
        return 1
    print("ЧИСТО: адрес собственного фронта читается у посадки, а не выписан")
    return 0


GOOD = ('addr="$(python3 "$D/own-rest-front-address.py" public)"\n'
        'ARGS+=(--env-var "ownRestBaseUrl=$scheme://127.0.0.1:$local_port")\n'
        'ARGS+=(--env-var "ownInternalRestBaseUrl=$scheme://127.0.0.1:$other_port")\n')


def self_test():
    fails = []

    def check(name, ok, detail=""):
        print(f"  {'ok  ' if ok else 'FAIL'} {name}" + ("" if ok else f": {detail}"))
        if not ok:
            fails.append(f"{name}: {detail}")

    one = ["deploy/scripts/own-rest-front-address.py"]

    print("ось 1 — производитель зовётся")
    c, f = audit([("r.sh", GOOD)], one)
    check("контроль: молчание", not f, str(f))
    check("контроль: предмет осмотрен", c["присвоений по подстановке осмотрено"] == 2, str(c))
    _, f = audit([("r.sh", GOOD.replace('python3 "$D/own-rest-front-address.py" public', 'echo 9098'))], one)
    check("инъекция: производителя не зовут — находка",
          any("не зовёт" in x for x in f), str(f))

    print("ось 2 — схема выписана открытым текстом")
    _, f = audit([("r.sh", GOOD.replace("ownRestBaseUrl=$scheme://", "ownRestBaseUrl=http://"))], one)
    check("инъекция: находка", any("ВЫПИСАННОЙ схемы" in x for x in f), str(f))
    _, f = audit([("r.sh", GOOD)], one)
    check("законный близнец: та же строка с подстановкой — молчание", not f, str(f))

    print("ось 3 — значение без подстановки вовсе")
    _, f = audit([("r.sh", GOOD.replace("ownRestBaseUrl=$scheme://127.0.0.1:$local_port",
                                        "ownRestBaseUrl=kaname:9098"))], one)
    check("инъекция: находка", any("без подстановки" in x for x in f), str(f))

    print("ось 4 — производителей не один")
    _, f = audit([("r.sh", GOOD)], one + ["tools/own-rest-front-address.py"])
    check("инъекция: находка", any("а должен быть ровно один" in x for x in f), str(f))
    _, f = audit([("r.sh", GOOD)], [])
    check("инъекция: ни одного — тоже находка", any("ни одного" in x for x in f), str(f))

    print("ось 5 — КОСВЕННАЯ форма записи распознаётся наравне с прямой")
    indirect = ('addr="$(python3 "$D/own-rest-front-address.py" public)"\n'
                'own() { ARGS+=(--env-var "$var=$scheme://127.0.0.1:$port"); }\n'
                'own public 19098 ownRestBaseUrl\n')
    c, f = audit([("r.sh", indirect)], one)
    check("контроль: молчание", not f, str(f))
    check("и присвоение СОСЧИТАНО, а не пропущено", c["присвоений по подстановке осмотрено"] == 1, str(c))
    _, f = audit([("r.sh", indirect.replace("$var=$scheme://", "$var=https://"))], one)
    check("инъекция в косвенной форме: находка", any("ВЫПИСАННОЙ схемы" in x for x in f), str(f))

    print("ось 6 — слепота распознавателя объявляется находкой")
    blind = ('addr="$(python3 "$D/own-rest-front-address.py" public)"\n'
             '# переменная названа, присвоение записано формой, которой гейт не знает\n'
             'echo ownRestBaseUrl\n')
    _, f = audit([("r.sh", blind)], one)
    check("инъекция: находка, а не зелёное «не искал»",
          any("форма записи ему неизвестна" in x for x in f), str(f))

    print("ось 7 — имя переменной в ПОЯСНЕНИИ находкой не является")
    _, f = audit([("r.sh", "# ownRestBaseUrl=http://пример-из-пояснения\n" + GOOD)], one)
    check("законный близнец: молчание", not f, str(f))

    print()
    if fails:
        print(f"ОТКАЗ: провалено утверждений {len(fails)} из 13", file=sys.stderr)
        for x in fails:
            print("  " + x, file=sys.stderr)
        return 1
    print("ЧИСТО: 13 утверждений, гейт способен упасть и способен смолчать по каждой оси")
    return 0


if __name__ == "__main__":
    sys.exit(main())
