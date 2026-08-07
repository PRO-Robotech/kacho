#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Гейт: шаг, за которым идёт поллер операции, не вправе УДАЛЯТЬ `opId`.

ПРЕДМЕТ. Поллер умеет «нечего поллить» — его первая строка `if (!get('opId')) return`.
Но этот guard живёт в post-скрипте шага, а страж неразрешённых подстановок
(`_URL_VAR_GUARD`) — в pre-скрипте КОРНЯ коллекции и потому исполняется раньше.
Для него УДАЛЁННАЯ переменная «не определена ни в одной области», и он честно роняет
шаг, не дав поллеру дойти до своего guard'а. Переменная, заданная ПУСТОЙ, — законный
вход: newman подставит пустую строку, литерала не останется, страж до неё не дойдёт
by construction (так сказано в его собственном комментарии).

Наблюдалось прогоном 31204778717: best-effort уборка чужой целевой группы вернула 403
(отзыв прав успел материализоваться), операции не возникло, и поллер уронил кейс, чьи
содержательные утверждения были зелёными. Симптом плавающий — 403 приходит не всегда.

ПРЕДИКАТ. В исходнике кейса ищется пара «в теле шага есть `unset('opId')`» и «сразу за
этим шагом стоит `poll_operation_until_done(`». Обратное направление (`set('opId', '')`)
законно и гейтом не трогается.

ГРАНИЦА, НАЗВАННАЯ ЧЕСТНО. Разбор текстовый, а не AST: кейсы — питон, но интересующая
конструкция живёт внутри СТРОКОВЫХ литералов JS, и AST питона до неё не доходит. Отсюда
два следствия, которые надо знать: (1) `unset` в комментарии JS будет засчитан; (2) шаг,
собранный не литералом, а вызовом хелпера, гейту не виден. Поэтому гейт печатает объём
осмотренного — «ноль находок» отличимо от «ноль прочитанного».
"""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[5]
UNSET = re.compile(r"unset\(\s*['\"]opId['\"]\s*\)")
POLL = re.compile(r"poll_operation_until_done\s*\(")
SET_EMPTY = re.compile(r"set\(\s*['\"]opId['\"]\s*,\s*['\"]['\"]\s*\)")


def scan(path):
    """→ [(строка, фрагмент)] для шагов, удаляющих opId перед поллером."""
    lines = path.read_text(encoding="utf-8").splitlines()
    out = []
    for i, ln in enumerate(lines):
        if not UNSET.search(ln):
            continue
        # Ищем ближайший последующий poll_operation_until_done в пределах шага:
        # граница шага — закрывающая `]),` конструкции Step(...). Смотрим вперёд
        # до 25 строк, этого хватает на самый длинный test_script в дереве.
        window = lines[i + 1: i + 26]
        joined = "\n".join(window)
        if not POLL.search(joined):
            continue
        # Если рядом же opId задаётся пустым — шаг уже исправен.
        if SET_EMPTY.search("\n".join(lines[max(0, i - 3): i + 4])):
            continue
        out.append((i + 1, ln.strip()[:90]))
    return out


def main():
    cases = sorted(ROOT.glob("services/*/tests/newman/cases/*.py")) + sorted(
        ROOT.glob("gateway/tests/newman/cases/*.py")
    )
    if not cases:
        print("ОТКАЗ: ни одного файла кейсов не найдено — судить не о чем", file=sys.stderr)
        return 2

    findings = []
    unsets = 0
    for p in cases:
        text = p.read_text(encoding="utf-8")
        unsets += len(UNSET.findall(text))
        for lineno, frag in scan(p):
            findings.append((p.relative_to(ROOT), lineno, frag))

    print(
        "opid-guard: осмотрено файлов кейсов %d; вхождений unset('opId') %d; "
        "находок %d" % (len(cases), unsets, len(findings))
    )
    for rel, lineno, frag in findings:
        print("  %s:%d: opId УДАЛЯЕТСЯ перед поллером — задавайте пустым: %s" % (rel, lineno, frag))
    if findings:
        print(
            "\nШаг, который может не создать операцию, обязан ставить opId ПУСТЫМ.\n"
            "Удалённая переменная роняет шаг на страже подстановок ДО того, как поллер\n"
            "дойдёт до своего guard'а «нечего поллить»."
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
