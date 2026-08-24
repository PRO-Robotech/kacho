#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Прогонщик юнитов: пропускает вывод `go test` насквозь и добавляет ПЕРЕПИСЬ.

ПРЕДМЕТ
-------
`go test ./...` без `-v` печатает `ok` и на пакете, где половина проб ушла в
`t.Skip`. Исходов у прогона три — зелёный, красный и «не выполнилось», — а вывод
различает два. Цена измерена (#1211): гейт приёмок в `internal/repohygiene`,
машинно державший ban #1 («кодировать без APPROVED-приёмки
запрещено»), пропускался на КАЖДОМ прогоне конвейера — ствол не резолвился при
мелком checkout'е — и не отказал ни разу за свою жизнь. Пропуск был невидим,
потому что его негде было увидеть.

ЧТО ДЕЛАЕТ
----------
1. Пропускает КАЖДУЮ строку `Output` насквозь, байт в байт. Диагностика падения
   не сокращается и не переупорядочивается: обрезанный вывод оставляет вердикт и
   уничтожает разбор.
2. Печатает перепись: пакетов · проб исполнено · упало · ПРОПУЩЕНО. «Ноль
   находок» становится отличимо от «ноль прочитанного».
3. Называет КАЖДЫЙ пропуск поимённо, с причиной.
4. СВЕРЯЕТ пропуски в пакетах гейтов дерева (`--watch`, по умолчанию
   `internal/repohygiene`) с ведомостью `--ledger`. Причина вне ведомости —
   находка: прогон краснеет и называет пробу.

ГРАНИЦА ОБЪЯВЛЕНА ЧЕСТНО
------------------------
Сверка охватывает ТОЛЬКО пакеты гейтов дерева. Пропуски остального дерева
печатаются и считаются, но не судятся: `-short` отсекает интеграцию, и ведомость
по именам проб разошлась бы с деревом на первой же новой интеграционной пробе.
Порядок величины — предикатом, а не памятью (число растёт с каждой новой
интеграционной пробой и устаревает молча, поэтому здесь стоит команда, а не
только цифра):

    grep -rn "testing[.]Short()" --include='*_test.go' . | wc -l  # 1252 на 7bb0a6e14

Сплошная сверка по всему дереву — отдельный предмет, и здесь она не сделана и не
изображается сделанной.

ВЕРДИКТ — ДАННЫЕ, А НЕ ВИД ВЫВОДА
---------------------------------
Код возврата: 0 — чисто; 1 — упавшие пробы/пакеты ЛИБО необъявленный пропуск;
2 — перепись беспредметна (проб исполнено ноль). Собственный ненулевой код
ставится и на падении `go test`, чтобы вердикт не зависел от того, включён ли
`pipefail` у вызывающего.

Запуск:
  go test ./... -race -short -json | python3 .github/scripts/go-test-verdict.py
  python3 .github/scripts/go-test-verdict.py --self-test
"""
from __future__ import annotations

import argparse
import io
import json
import os
import re
import sys
from pathlib import Path

DEFAULT_LEDGER = ".github/scripts/gate-skips-allowed.txt"
DEFAULT_WATCH = "internal/repohygiene"
# Сколько последних строк пробы держать в памяти: сообщение пропуска — одна
# строка, восьми хватает с запасом на `t.Log` перед ним.
TAIL_LINES = 8

# Служебные строки `go test -v`: они не являются сообщением пропуска.
MARKER_RE = re.compile(r"^\s*(===\s|---\s|\s*PASS\b|\s*FAIL\b|\s*ok\s|\s*SKIP\b)")
# Префикс `foo_test.go:51: ` перед телом сообщения.
COORD_RE = re.compile(r"^[\w./-]+\.go:\d+:\s*")


def load_ledger(path: Path) -> list[str]:
    """Причины, при которых пропуск законен. Отсутствие файла — отказ: ведомость
    и есть основание сверки, и судить без неё значило бы судить по неизвестному."""
    prefixes: list[str] = []
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.split("#", 1)[0].strip()
        if line:
            prefixes.append(line)
    return prefixes


def skip_reason(lines: list[str]) -> str:
    """Сообщение пропуска — ПОСЛЕДНЯЯ содержательная строка перед `--- SKIP`."""
    body = [ln for ln in lines if ln.strip() and not MARKER_RE.match(ln)]
    if not body:
        return ""
    return COORD_RE.sub("", body[-1].strip())


class Run:
    def __init__(self) -> None:
        self.packages: set[str] = set()
        self.passed = 0
        self.failed: list[str] = []
        self.skipped: list[tuple[str, str, str]] = []  # пакет, проба, причина
        self.pkg_failed: list[str] = []
        self.build_failed: list[str] = []
        self.test_out: dict[tuple[str, str], list[str]] = {}
        # Пробы, у которых было `run` и не было терминального события. Это ТРЕТЬЯ
        # категория: прогон снят по сроку, убит по памяти, оборван — вердикта у них
        # нет НИ У ОДНОЙ, и «ноль упавших» тут не значит «чисто».
        self.started: set[tuple[str, str]] = set()


def consume(stream, out, run: Run) -> None:
    """Читает поток ЦЕЛИКОМ. Ранний выход отдал бы производителю SIGPIPE, и
    найденное было бы объявлено ненайденным."""
    for raw in stream:
        line = raw.rstrip("\n")
        if not line.startswith("{"):
            if line:
                out.write(line + "\n")
            continue
        try:
            ev = json.loads(line)
        except ValueError:
            out.write(line + "\n")
            continue

        pkg = ev.get("Package") or ""
        test = ev.get("Test") or ""
        action = ev.get("Action")

        # ОШИБКИ СБОРКИ ИДУТ СВОЕЙ ПАРОЙ СОБЫТИЙ и НЕ несут поля `Package`:
        # `build-output` (текст компилятора) и `build-fail`, оба с `ImportPath`.
        # Первая редакция их не знала — вердикт получался верным (пакет всё равно
        # падал), а СТРОКА «undefined: …» в лог не попадала вовсе: вердикт есть,
        # разбор невозможен. Замерено настоящим `go test -json` на 1.25.13.
        if action == "build-output":
            out.write(ev.get("Output", ""))
            continue
        if action == "build-fail":
            # Счёт отдельный: за одной ошибкой сборки идёт ЕЩЁ и `fail` пакета,
            # и сложение их в одну строку переписи давало «пакетов упало 2» на
            # одном пакете — число верное по событиям и ложное по предмету.
            run.build_failed.append(ev.get("ImportPath", "<сборка>"))
            continue

        if pkg:
            run.packages.add(pkg)
        if action == "run" and test:
            run.started.add((pkg, test))
        if action == "output":
            text = ev.get("Output", "")
            out.write(text)
            if test:
                # ХВОСТ, А НЕ ВЕСЬ ВЫВОД. Сообщение пропуска — последняя
                # содержательная строка пробы, поэтому держать её вывод целиком
                # незачем: на дереве в 1600 проб это сотни мегабайт в памяти
                # ранера ради одной строки. В ЛОГ уходит всё (выше), в память —
                # хвост.
                buf = run.test_out.setdefault((pkg, test), [])
                buf.append(text.rstrip("\n"))
                if len(buf) > TAIL_LINES:
                    del buf[:-TAIL_LINES]
        elif action == "pass" and test:
            run.passed += 1
            run.started.discard((pkg, test))
            run.test_out.pop((pkg, test), None)
        elif action == "fail":
            (run.failed if test else run.pkg_failed).append(
                f"{pkg} {test}".strip())
            run.started.discard((pkg, test))
            run.test_out.pop((pkg, test), None)
        elif action == "skip" and test:
            run.started.discard((pkg, test))
            run.skipped.append(
                (pkg, test, skip_reason(run.test_out.pop((pkg, test), []))))


def verdict(run: Run, watch: str, ledger: list[str], out) -> int:
    watched = [(p, t, r) for p, t, r in run.skipped if watch in p]
    others = [s for s in run.skipped if watch not in s[0]]
    undeclared = [
        (p, t, r) for p, t, r in watched
        if not any(r.startswith(pref) for pref in ledger)
    ]

    out.write("\n── перепись прогона ─────────────────────────────────────────\n")
    out.write(
        f"пакетов {len(run.packages)} · проб исполнено {run.passed} · "
        f"упало {len(run.failed)} · пакетов упало {len(run.pkg_failed)} · "
        f"сборок упало {len(run.build_failed)} · "
        f"ПРОПУЩЕНО {len(run.skipped)} · "
        f"НЕ ВЫПОЛНИЛОСЬ {len(run.started)}\n")
    out.write(
        f"пропуски у гейтов дерева ({watch}): {len(watched)}; "
        f"в прочих пакетах: {len(others)} "
        f"(ожидаемо: `-short` отсекает интеграцию — её гоняет `make test-integration`)\n")
    for pkg, test, reason in watched:
        out.write(f"  SKIP {pkg} {test} — {reason or '<причина не напечатана>'}\n")
    out.write(f"ведомость: объявлено причин {len(ledger)}, необъявленных пропусков "
              f"{len(undeclared)}\n")

    rc = 0
    if run.failed or run.pkg_failed or run.build_failed:
        out.write("КРАСНО: упавшее выше — вердикт `go test`\n")
        rc = 1

    # ТРЕТЬЯ КАТЕГОРИЯ ОБЪЯВЛЯЕТСЯ, А НЕ ВЫЧИТАЕТСЯ. Проба с `run` и без
    # терминального события — снятая по сроку, убитая по памяти, оборванная
    # вместе с прогоном. Вердикта у неё нет, и «упало 0» про неё не говорит
    # НИЧЕГО. Найдено собственным прогоном: снятый по ходу `make test-unit`
    # печатал «упало 0 … ЧИСТО» над 423 пакетами, из которых последний был убит
    # на середине.
    if run.started:
        sample = sorted(f"{p} {t}" for p, t in run.started)[:5]
        out.write(
            f"НЕ ВЫПОЛНИЛОСЬ: проб начато и не завершено {len(run.started)} "
            f"(например: {'; '.join(sample)}). Это третья категория исхода: "
            f"вердикта у них нет ни у одной, и в успех она не засчитывается\n")
        return 2 if rc == 0 else rc

    # ПОРЯДОК ЗНАЧИМ, и он выведен из самопробы. «Проб исполнено ноль» при
    # УПАВШЕЙ сборке — это красное, а не «не выполнилось»: предмет прочитан, он
    # не собрался. Проверка беспредметности стоит ПОСЛЕ разбора падений, иначе
    # ошибка компиляции объявлялась бы третьей категорией и не чинилась бы как
    # красное.
    if rc == 0 and run.passed == 0 and not run.skipped:
        out.write("ОТКАЗ: проб исполнено ноль — перепись беспредметна, "
                  "«ноль находок» здесь означало бы «ноль прочитанного»\n")
        return 2

    for pkg, test, reason in undeclared:
        out.write(
            f"ОТКАЗ: {pkg} {test} — пропуск с причиной «{reason}» не объявлен "
            f"законным. Пропуск гейта дерева неотличим от его успеха, поэтому "
            f"необъявленный — находка: либо предпосылка не создана (чинится "
            f"настройкой прогона или клона), либо причина законна by construction "
            f"и обязана быть внесена в ведомость\n")
        rc = 1
    if rc == 0:
        out.write("ЧИСТО\n")
    return rc


def run_stream(stream, out, watch: str, ledger: list[str]) -> int:
    run = Run()
    consume(stream, out, run)
    return verdict(run, watch, ledger, out)


# ─── самопроба ──────────────────────────────────────────────────────────────
def _ev(**kw) -> str:
    return json.dumps(kw, ensure_ascii=False)


def _stream(*lines: str):
    return io.StringIO("\n".join(lines) + "\n")


def _case(name: str, lines, ledger, expect_rc: int, expect_sub: str | None):
    buf = io.StringIO()
    rc = run_stream(_stream(*lines), buf, DEFAULT_WATCH, ledger)
    got = buf.getvalue()
    ok = rc == expect_rc and (expect_sub is None or expect_sub in got)
    print(f"  [{'OK ' if ok else 'ОТКАЗ'}] {name}: код {rc} (ждали {expect_rc})")
    if not ok:
        print("    ── вывод ──\n" + "\n".join("    " + x for x in got.splitlines()))
    return ok


GATE_PKG = "github.com/PRO-Robotech/kacho/internal/repohygiene"
OTHER_PKG = "github.com/PRO-Robotech/kacho/services/vpc/internal/repo"


def _skip_lines(pkg: str, test: str, reason: str):
    return [
        _ev(Action="run", Package=pkg, Test=test),
        _ev(Action="output", Package=pkg, Test=test,
            Output=f"=== RUN   {test}\n"),
        _ev(Action="output", Package=pkg, Test=test,
            Output=f"    x_test.go:51: {reason}\n"),
        _ev(Action="output", Package=pkg, Test=test,
            Output=f"--- SKIP: {test} (0.00s)\n"),
        _ev(Action="skip", Package=pkg, Test=test),
    ]


def _pass_lines(pkg: str, test: str):
    return [
        _ev(Action="run", Package=pkg, Test=test),
        _ev(Action="pass", Package=pkg, Test=test),
    ]


def self_test() -> int:
    print("самопроба прогонщика: инъекция в обе стороны по каждой оси")
    led = ["файловая система не поддерживает симлинки"]
    ok = True

    # Ось 1 — необъявленный пропуск у гейта дерева.
    ok &= _case(
        "необъявленный пропуск гейта → красно и назван поимённо",
        _pass_lines(GATE_PKG, "TestA")
        + _skip_lines(GATE_PKG, "TestTrunk", "ствол не разрешается — сравнивать не с чем"),
        led, 1, "TestTrunk")
    ok &= _case(
        "законный близнец: та же форма, причина объявлена → зелено",
        _pass_lines(GATE_PKG, "TestA")
        + _skip_lines(GATE_PKG, "TestSym",
                      "файловая система не поддерживает симлинки: operation not permitted"),
        led, 0, "ЧИСТО")

    # Ось 2 — граница сверки: тот же пропуск вне пакетов гейтов не судится.
    ok &= _case(
        "тот же пропуск в НЕ-гейтовом пакете → зелено (граница объявлена)",
        _pass_lines(GATE_PKG, "TestA")
        + _skip_lines(OTHER_PKG, "TestIntegration", "short mode"),
        led, 0, "в прочих пакетах: 1")

    # Ось 3 — пустая ведомость не превращает идеал в поломку.
    ok &= _case(
        "пустая ведомость и ноль пропусков → зелено",
        _pass_lines(GATE_PKG, "TestA"), [], 0, "ПРОПУЩЕНО 0")

    # Ось 4 — падение видно даже без pipefail у вызывающего.
    ok &= _case(
        "упавшая проба → красно собственным кодом",
        _pass_lines(GATE_PKG, "TestA")
        + [_ev(Action="fail", Package=GATE_PKG, Test="TestB")],
        led, 1, "КРАСНО")
    ok &= _case(
        "упавший ПАКЕТ без упавших проб (срок, паника) → красно",
        _pass_lines(GATE_PKG, "TestA") + [_ev(Action="fail", Package=GATE_PKG)],
        led, 1, "пакетов упало 1")

    # Ось 5 — «не выполнилось» отличимо от «чисто», в ОБЕИХ формах.
    ok &= _case("поток без единой пробы → код 2", [_ev(Action="start", Package=GATE_PKG)],
                led, 2, "перепись беспредметна")
    ok &= _case(
        "оборванный поток: проба начата и не завершена → код 2, не «ЧИСТО»",
        _pass_lines(GATE_PKG, "TestA") + [_ev(Action="run", Package=GATE_PKG, Test="TestB")],
        led, 2, "НЕ ВЫПОЛНИЛОСЬ: проб начато и не завершено 1")
    ok &= _case(
        "законный близнец: та же проба, но завершена → зелено",
        _pass_lines(GATE_PKG, "TestA") + _pass_lines(GATE_PKG, "TestB"),
        led, 0, "ЧИСТО")

    # Ось 6 — вывод проходит насквозь: разбор падения не уничтожается.
    ok &= _case(
        "диагностика падения доезжает байт в байт",
        [_ev(Action="output", Package=GATE_PKG, Test="TestB",
             Output="    a_test.go:9: ожидали X, получили Y\n"),
         _ev(Action="fail", Package=GATE_PKG, Test="TestB")],
        led, 1, "ожидали X, получили Y")

    # Ось 7 — события СБОРКИ: текст компилятора обязан доехать в лог, а не
    # исчезнуть вместе с верным вердиктом.
    ok &= _case(
        "текст компилятора из build-output доезжает",
        [_ev(ImportPath="p [p.test]", Action="build-output",
             Output="internal/x/a.go:3:12: undefined: undefinedSymbol\n"),
         _ev(ImportPath="p [p.test]", Action="build-fail"),
         _ev(Action="output", Package=GATE_PKG,
             Output="FAIL\tp [build failed]\n"),
         _ev(Action="fail", Package=GATE_PKG)],
        led, 1, "undefined: undefinedSymbol")

    # Ось 8 — не-JSON (что бы ни писал инструмент мимо потока) не роняет разбор.
    ok &= _case(
        "строка сборки не в JSON доезжает",
        ["# github.com/PRO-Robotech/kacho/internal/x",
         "x.go:3:2: undefined: Foo",
         _ev(Action="fail", Package=GATE_PKG)],
        led, 1, "undefined: Foo")

    print("самопроба:", "ПРОЙДЕНА" if ok else "ПРОВАЛЕНА")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--ledger", default=DEFAULT_LEDGER)
    ap.add_argument("--watch", default=DEFAULT_WATCH)
    ap.add_argument("--self-test", action="store_true")
    args = ap.parse_args()

    if args.self_test:
        return self_test()

    root = Path(os.environ.get("GITHUB_WORKSPACE") or ".")
    ledger_path = Path(args.ledger)
    if not ledger_path.is_absolute():
        ledger_path = root / args.ledger
    if not ledger_path.exists():
        sys.stderr.write(
            f"ОТКАЗ: ведомость законных пропусков не найдена ({ledger_path}) — "
            f"сверять не с чем, а судить без основания нельзя\n")
        return 2
    ledger = load_ledger(ledger_path)

    sys.stdout.reconfigure(line_buffering=True)
    return run_stream(sys.stdin, sys.stdout, args.watch, ledger)


if __name__ == "__main__":
    sys.exit(main())
