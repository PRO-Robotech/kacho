#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Заметка ЗЕЛЁНОГО шага не должна быть неотличима от вердикта.

ПРЕДМЕТ
-------
`actions/setup-go` регистрирует на всё задание сопоставитель вывода (владелец
`go`). Копия его образца лежит рядом — `go-problem-matcher.json`, снята дословно
с пина действия:

    ^\\s*(.+\\.go):(?:(\\d+):(\\d+):)? (.*)

Поля `severity` у сопоставителя НЕТ, поэтому уровень аннотации по умолчанию —
`error`, то есть ОТКАЗ. Совпавшая строка становится аннотацией уровня отказа
независимо от того, каким кодом вышел напечатавший её шаг.

Отсюда класс: гейт печатает заявленное исключение обычным текстом, выходит нулём,
а читатель видит аннотацию УРОВНЯ ОТКАЗА рядом с настоящими отказами того же
задания. Отличить можно только сверив номер шага. Цену заплатили целой полосой:
такую аннотацию прочли как причину красноты и ушли чинить класс, которого нет
(PRO-Robotech/kacho#2542).

ЧТО СУДИТСЯ, А ЧТО НЕТ
----------------------
Судится вывод гейтов, вышедших НУЛЁМ, — их шаг оставляет здесь файл `<svc>.green`.
У красного гейта аннотация уровня отказа законна: шаг и правда упал, координата в
ней ведёт к находке, и глушить её значило бы отнять у находки ровно то, что делает
её починяемой.

ПОЧЕМУ ОБРАЗЕЦ ПОЗИЦИОННО СЛЕП — И ПОЧЕМУ ЭТО ГЛАВНОЕ
------------------------------------------------------
`(.+\\.go)` жадная и съедает ЛЮБОЙ префикс, поэтому «заметка не начинается с
координаты» защитой НЕ является: строка `audit-list-filter: note: …/serve.go:493:21: …`
уже не начиналась с координаты и всё равно поднималась (замер #2542).

Работает другое: жадность всегда садится на ПОСЛЕДНЕЕ `.go` строки. Если
координата стоит ПОСЛЕДНЕЙ, после неё нет `: `, которым образец обязан
продолжиться, — и совпадения нет ни при каком тексте перед ней. Это свойство
построения, а не удачно выбранный разделитель.

ИСХОДЫ ЧИТАЮТСЯ КОДОМ ВОЗВРАТА
------------------------------
  0  чисто        — ни одна строка зелёного гейта не поднимается;
  1  находка      — строка зелёного гейта поднимается до аннотации отказа;
  2  СУДИТЬ НЕЧЕГО — зелёных журналов нет либо они пусты. Не успех: журнал кладёт
                    шаг, и его отсутствие означает, что шаг сломан, а не что чисто.
"""

import argparse
import json
import os
import re
import sys
import tempfile

MATCHER_FILE = "go-problem-matcher.json"
GREEN_SUFFIX = ".green"


def load_matcher(script_dir):
    """Читает образец сопоставителя из копии, снятой с пина действия."""
    path = os.path.join(script_dir, MATCHER_FILE)
    with open(path, encoding="utf-8") as fh:
        doc = json.load(fh)
    patterns = doc["problemMatcher"][0]["pattern"]
    if len(patterns) != 1:
        raise SystemExit(
            "%s: у сопоставителя %d образец(ов), а разбор рассчитан на один — "
            "копия разошлась с оригиналом" % (path, len(patterns))
        )
    return re.compile(patterns[0]["regexp"])


def claimed_lines(matcher, text):
    """Строки, которые сопоставитель поднимет до аннотации уровня отказа."""
    out = []
    for n, line in enumerate(text.splitlines(), 1):
        m = matcher.match(line)
        if m:
            out.append((n, line, m.group(2), m.group(3)))
    return out


def judge(matcher, name, text):
    """Находки одного зелёного журнала плюс число осмотренных строк."""
    findings = []
    for n, line, ln, col in claimed_lines(matcher, text):
        where = "строка %s" % ln if ln else "без номера строки"
        findings.append(
            "%s: гейт ЗЕЛЁН, но его строка #%d поднимается сопоставителем `go` до "
            "аннотации УРОВНЯ ОТКАЗА (%s, столбец %s) — читатель увидит её рядом с "
            "настоящими отказами задания и отличит только сверив номер шага.\n"
            "    %s\n"
            "    Координата обязана стоять ПОСЛЕДНЕЙ: жадное `(.+\\.go)` садится на "
            "последнее `.go` строки, и если после него нет `: `, совпадения нет ни при "
            "каком тексте перед ним. Переносить координату в начало или прятать за "
            "префиксом БЕСПОЛЕЗНО — префикс образец съедает."
            % (name, n, where, col or "—", line.strip())
        )
    return findings, len(text.splitlines())


def judge_dir(matcher, logs):
    """Судит все зелёные журналы каталога. Возвращает (код, строки отчёта)."""
    report = []
    try:
        names = sorted(n for n in os.listdir(logs) if n.endswith(GREEN_SUFFIX))
    except OSError as exc:
        report.append("assert-green-gate-notes: каталог журналов не прочитан (%s) — "
                      "судить нечего, вердикт беспредметен" % exc)
        return 2, report

    findings, examined, claimed = [], 0, 0
    for n in names:
        with open(os.path.join(logs, n), encoding="utf-8", errors="replace") as fh:
            text = fh.read()
        f, lines = judge(matcher, n[: -len(GREEN_SUFFIX)], text)
        findings += f
        examined += lines
        claimed += len(f)

    report.append("assert-green-gate-notes: перепись — зелёных журналов %d, строк "
                  "осмотрено %d, поднимаемых сопоставителем %d"
                  % (len(names), examined, claimed))

    if not names or examined == 0:
        report.append("assert-green-gate-notes: судить НЕЧЕГО — зелёных журналов %d, "
                      "строк в них %d. Это не успех: журнал кладёт шаг, и пустота "
                      "означает, что шаг сломан, а не что вывод чист."
                      % (len(names), examined))
        return 2, report

    report += ["assert-green-gate-notes: " + m for m in findings]
    return (1 if findings else 0), report


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--logs", metavar="DIR",
                    help="каталог журналов ЗЕЛЁНЫХ гейтов (файлы *%s)" % GREEN_SUFFIX)
    ap.add_argument("--self-test", action="store_true",
                    help="доказать способность судить — инъекция в обе стороны")
    args = ap.parse_args(argv)

    matcher = load_matcher(os.path.dirname(os.path.abspath(__file__)))
    if args.self_test:
        return self_test(matcher)
    if not args.logs:
        ap.error("нужен --logs DIR либо --self-test")

    rc, report = judge_dir(matcher, args.logs)
    for line in report:
        print(line, file=sys.stdout if rc == 0 else sys.stderr)
    return rc


# --- доказательство способности упасть -------------------------------------

SELF_TEST_LINES = (
    ("голая форма компилятора",
     "cmd/kacho-registry/serve.go:493:21: NewRegistryHandler can be wired with a nil authorizer",
     True),
    ("та же строка ЗА ПРЕФИКСОМ — префикс НЕ спасает (замер #2542)",
     "audit-list-filter: note: cmd/kacho-registry/serve.go:493:21: NewRegistryHandler can be wired",
     True),
    ("координата ПОСЛЕДНЕЙ — законный близнец, молчит",
     "audit-list-filter: note: NewRegistryHandler can be wired with a nil authorizer — "
     "не фатально, пока отказ ручки закреплён тестом; см. cmd/kacho-registry/serve.go:493:21",
     False),
    ("координата последней, но после неё дописали `: ` — снова поднимается",
     "audit-list-filter: note: см. cmd/kacho-registry/serve.go:493:21: и ещё кое-что",
     True),
    ("необязательная группа строки/столбца — `файл.go: текст` тоже поднимается",
     "audit-list-filter: note: serve.go: authorizer may be nil",
     True),
    ("ведущие пробелы образец допускает",
     "    serve.go:1:2: indented",
     True),
    ("`.go` не на границе имени — близнец, молчит",
     "audit-list-filter: note: serve.golang:1:2: not a Go file",
     False),
    ("обычная строка переписи — молчит",
     "audit-list-filter: examined 8 handler file(s) and 11 composition-root file(s)",
     False),
    ("вердикт гейта — молчит",
     "audit-list-filter: OK",
     False),
)


def self_test(matcher):
    """Инъекция в обе стороны: поднимаемое обязано ловиться, законное — молчать."""
    bad = 0
    for name, line, want in SELF_TEST_LINES:
        got = bool(claimed_lines(matcher, line))
        if got != want:
            bad += 1
        print("%s %-64s поднимается=%s ожидалось=%s"
              % ("ok  " if got == want else "ПРОВАЛ", name, got, want))

    # Обход каталога: находка, чистота и ПУСТОТА — три разных исхода.
    with tempfile.TemporaryDirectory() as tmp:
        rc_void, _ = judge_dir(matcher, tmp)
        if rc_void != 2:
            print("ПРОВАЛ пустой каталог журналов обязан давать 2, получили %d" % rc_void)
            bad += 1
        else:
            print("ok   пустой каталог журналов — «судить нечего», а не успех")

        with open(os.path.join(tmp, "registry.green"), "w", encoding="utf-8") as fh:
            fh.write("audit-list-filter: note: a/b.go:1:2: nil authorizer\n"
                     "audit-list-filter: OK\n")
        rc_hit, report = judge_dir(matcher, tmp)
        if rc_hit != 1 or not any("УРОВНЯ ОТКАЗА" in ln for ln in report):
            print("ПРОВАЛ поднимаемая строка зелёного журнала обязана быть находкой")
            bad += 1
        else:
            print("ok   поднимаемая строка зелёного журнала — находка с координатой")

        with open(os.path.join(tmp, "registry.green"), "w", encoding="utf-8") as fh:
            fh.write("audit-list-filter: note: nil authorizer; см. a/b.go:1:2\n"
                     "audit-list-filter: OK\n")
        rc_ok, _ = judge_dir(matcher, tmp)
        if rc_ok != 0:
            print("ПРОВАЛ законный близнец (координата последней) объявлен находкой")
            bad += 1
        else:
            print("ok   законный близнец с координатой в конце — молчит")

        # Красный гейт журнала не оставляет: его вывод не судится по построению.
        os.rename(os.path.join(tmp, "registry.green"), os.path.join(tmp, "registry.out"))
        rc_red, _ = judge_dir(matcher, tmp)
        if rc_red != 2:
            print("ПРОВАЛ журнал не с суффиксом .green судиться не должен")
            bad += 1
        else:
            print("ok   журнал красного гейта не судится — суффикс решает")

    print("assert-green-gate-notes: самопроверка — утверждений %d, провалов %d"
          % (len(SELF_TEST_LINES) + 4, bad))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
