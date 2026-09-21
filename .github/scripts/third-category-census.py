#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""third-category-census.py — сколько шагов третью категорию РАЗЛИЧАЮТ и сколько умеют СООБЩИТЬ.

ПРЕДМЕТ (#958, предикат снятия 4)
---------------------------------
Правило корпуса объявляет три исхода — зелёный · красный · «не выполнилось» — и
требует, чтобы третий не вычитался из вердикта и не зачитывался в успех.

Механизм различения в дереве ЕСТЬ и работает: шаги третью категорию узнают и
называют. Дальше она умирала на границе: у прогона два состояния, и «не
выполнилось» отображалось в отказ БЕЗ следа — читающий вердикт не мог отличить
«продукт сломан» от «условие не создано», а лечатся они противоположным.

Эта перепись отвечает на вопрос, который задача просит назвать ЧИСЛОМ, а не
оценкой:

    сколько шагов третью категорию РАЗЛИЧАЮТ — то есть называют её в своей
    исполняемой части;
    сколько из них умеют её СООБЩИТЬ — то есть кладут её в канал, который читает
    сводный вердикт.

КАНАЛ РОВНО ОДИН, И НАЗВАН ОН ЗДЕСЬ
-----------------------------------
Аннотация с заголовком «НЕ ВЫПОЛНИЛОСЬ». Провайдер привязывает её к check-run
работы, а сводный вердикт (`pr-verdict-wait.sh` → `pr-verdict.py`) читает
аннотации не-зелёных проверок и печатает третью категорию ОТДЕЛЬНОЙ СТРОКОЙ.

Почему не код возврата и не вывод шага: до сводного вердикта доезжает ТОЛЬКО
заключение check-run (`success`/`failure`/…) и аннотации. Код возврата шага
виден внутри работы и не виден снаружи; текст журнала — тем более.

ПРЕДПОСЫЛКА ПЕРЕПИСИ ПРОВЕРЯЕТСЯ ЗДЕСЬ ЖЕ
-----------------------------------------
Перепись считает «умеет сообщить» по каналу, у которого ОБЯЗАН быть ЧИТАТЕЛЬ.
Снимут читателя — и число «умеют сообщить» останется прежним, продолжая означать
то, чего больше нет. Поэтому наличие читателя проверяется механически: искомая
метка обязана встречаться в исполняемой части `pr-verdict-wait.sh`.

Коды возврата:
    0 — перепись снята; читатель канала на месте; сообщать умеет хотя бы один
        из различающих;
    1 — находка: различающие есть, а сообщить не умеет НИ ОДИН (третья категория
        до вердикта не доезжает) либо у канала нет читателя;
    2 — судить не по чему: обход пуст.

Запуск:
    python3 .github/scripts/third-category-census.py [--root <корень>]
    python3 .github/scripts/third-category-census.py --self-test
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml

sys.path.insert(0, str(Path(__file__).resolve().parent))
from ci_text import strip_comment_lines  # noqa: E402 — путь дописывается строкой выше

# ── ЕДИНЫЙ ИСТОЧНИК ДВУХ МЕТОК ───────────────────────────────────────────────
# Первая — как третья категория НАЗЫВАЕТСЯ, вторая — как она СООБЩАЕТСЯ. Обе
# перечислены здесь ровно один раз: две копии разошлись бы молча, и перепись
# считала бы одно, а читатель искал другое.
NAMES = ("НЕ ВЫПОЛНИЛОСЬ", "не выполнилось", "условие не создано", "УСЛОВИЕ НЕ СОЗДАНО")
CHANNEL = re.compile(r"::(?:warning|error) title=НЕ ВЫПОЛНИЛОСЬ")
READER = ".github/scripts/pr-verdict-wait.sh"

SCRIPT_REF = re.compile(r"[A-Za-z0-9_./-]+\.(?:sh|py)")


def executable_part(text: str, suffix: str) -> str:
    """Текст без строк-комментариев.

    Читается ИСПОЛНЯЕМАЯ часть, а не файл целиком: этот самый файл называет обе
    метки в своей шапке, и перепись по сырому тексту нашла бы сама себя — тот же
    класс, который корпус ловит у гейтов, читающих слово вместо кода.

    Предикат живёт в ОДНОМ доме (`ci_text.py`) и оттуда же берётся гейтом связи
    выкладок: две копии разошлись бы молча — одна научилась бы видеть хвостовой
    комментарий, другая нет, и обе продолжали бы считать каждая своё.
    """
    if suffix in (".sh", ".py", ".yml", ".yaml"):
        return strip_comment_lines(text)
    return text


def tracked(root: Path) -> list[str]:
    p = subprocess.run(["git", "-C", str(root), "ls-files"], capture_output=True, text=True)
    if p.returncode != 0:
        return [str(f.relative_to(root)) for f in root.rglob("*") if f.is_file()]
    return p.stdout.split()


def script_text(root: Path, rel: str, files: set[str], cache: dict[str, str]) -> str:
    if rel in cache:
        return cache[rel]
    if rel not in files:
        cache[rel] = ""
        return ""
    try:
        body = executable_part((root / rel).read_text(encoding="utf-8"), Path(rel).suffix)
    except (OSError, UnicodeDecodeError):
        body = ""
    cache[rel] = body
    return body


def step_text(root: Path, run: str, files: set[str], cache: dict[str, str]) -> str:
    """Исполняемая часть шага ПЛЮС скриптов, которые он зовёт.

    Один уровень раскрытия, и этого достаточно by construction: шаг зовёт
    владельца исхода напрямую. Глубже — вопрос к самому владельцу, а не к шагу.
    """
    body = executable_part(run, ".sh")
    for m in SCRIPT_REF.finditer(body):
        rel = m.group(0).lstrip("./")
        body += "\n" + script_text(root, rel, files, cache)
    return body


def census(root: Path) -> tuple[int, str]:
    files = set(tracked(root))
    cache: dict[str, str] = {}
    wf_dir = root / ".github" / "workflows"
    wfs = sorted(p for p in wf_dir.glob("*.y*ml") if p.is_file())
    out: list[str] = []

    if not wfs:
        return 2, ("НЕ ВЫПОЛНИЛОСЬ: объявлений процессов не нашлось — обход пуст, "
                   "и число «ноль различающих» ничего не означает.")

    steps_read = 0
    recognise: list[str] = []
    report: list[str] = []

    for wf in wfs:
        try:
            doc = yaml.safe_load(wf.read_text(encoding="utf-8"))
        except yaml.YAMLError:
            out.append(f"{wf.name}: объявление не разбирается — не осмотрено")
            continue
        for job, spec in ((doc or {}).get("jobs") or {}).items():
            if not isinstance(spec, dict):
                continue
            for i, step in enumerate(spec.get("steps") or []):
                if not isinstance(step, dict):
                    continue
                steps_read += 1
                run = str(step.get("run") or "")
                if not run:
                    continue
                body = step_text(root, run, files, cache)
                where = f"{wf.name} / {job} / {step.get('name') or step.get('id') or f'шаг #{i + 1}'}"
                if any(n in body for n in NAMES):
                    recognise.append(where)
                    if CHANNEL.search(body):
                        report.append(where)

    reader_ok = bool(CHANNEL.search(script_text(root, READER, files, cache))) or (
        "НЕ ВЫПОЛНИЛОСЬ" in script_text(root, READER, files, cache)
    )

    out.append(
        f"перепись третьей категории: объявлений {len(wfs)}, шагов прочитано {steps_read}; "
        f"РАЗЛИЧАЮТ {len(recognise)}, из них умеют СООБЩИТЬ {len(report)}"
    )
    out.append(f"канал: аннотация с заголовком «НЕ ВЫПОЛНИЛОСЬ»; читатель: {READER} — "
               f"{'на месте' if reader_ok else 'НЕ НАЙДЕН'}")
    mute = [w for w in recognise if w not in report]
    if mute:
        out.append(f"различают, но сообщить не умеют ({len(mute)}):")
        out.extend(f"  · {w}" for w in mute)
        out.append("    Это не находка сама по себе: шаг вправе не иметь своего канала, "
                   "если категорию сообщает владелец, которого он зовёт. Число названо, "
                   "чтобы «доезжает до вердикта» перестало быть оценкой.")

    if not reader_ok:
        out.append(
            "НАХОДКА: у канала нет читателя. Метка «НЕ ВЫПОЛНИЛОСЬ» больше не ищется в "
            f"{READER}, значит третья категория снова умирает на границе, а это число "
            "продолжало бы означать то, чего нет."
        )
        return 1, "\n".join(out)
    if recognise and not report:
        return 1, "\n".join(out) + (
            "\nНАХОДКА: третью категорию различают, а сообщить не умеет НИ ОДИН шаг — "
            "она не доезжает до вердикта ни по какому каналу."
        )
    if steps_read == 0:
        return 2, "\n".join(out) + "\nНЕ ВЫПОЛНИЛОСЬ: шагов не прочитано ни одного."
    return 0, "\n".join(out)


# ── САМОПРОВЕРКА ─────────────────────────────────────────────────────────────

WF_RECOGNISE_ONLY = """
name: проба
jobs:
  работа:
    steps:
      - name: различает, но молчит
        run: echo "НЕ ВЫПОЛНИЛОСЬ — условие не создано"
"""

WF_REPORTS = """
name: проба
jobs:
  работа:
    steps:
      - name: различает и сообщает
        run: echo "::warning title=НЕ ВЫПОЛНИЛОСЬ (условие не создано)::стенд не поднялся"
"""

WF_NEITHER = """
name: проба
jobs:
  работа:
    steps:
      - name: о третьей категории не знает
        run: echo ok
"""

READER_BODY = 'grep "НЕ ВЫПОЛНИЛОСЬ" annotations.json\n'
READER_MUTE = 'echo "читателя тут больше нет"\n'


def self_test() -> int:
    me = str(Path(__file__).resolve())
    ok = True
    cases = 0

    def build(wf: str, reader: str | None) -> Path:
        d = Path(tempfile.mkdtemp())
        (d / ".github" / "workflows").mkdir(parents=True)
        (d / ".github" / "workflows" / "w.yml").write_text(wf, encoding="utf-8")
        if reader is not None:
            (d / ".github" / "scripts").mkdir(parents=True)
            (d / ".github" / "scripts" / "pr-verdict-wait.sh").write_text(reader, encoding="utf-8")
        return d

    def case(label: str, wf: str, reader: str | None, want_rc: int, want_text: str = "") -> None:
        nonlocal ok, cases
        cases += 1
        d = build(wf, reader)
        r = subprocess.run([sys.executable, me, "--root", str(d)],
                           capture_output=True, text=True, check=False)
        if r.returncode != want_rc:
            print(f"  ПРОВАЛ {label} → код {r.returncode}, ждали {want_rc}")
            print("        " + (r.stdout + r.stderr).replace("\n", "\n        ")[:800])
            ok = False
        elif want_text and want_text not in r.stdout:
            print(f"  ПРОВАЛ {label} → в выводе нет «{want_text}»")
            print("        " + r.stdout.replace("\n", "\n        ")[:800])
            ok = False
        else:
            print(f"  ОК  {label} → код {r.returncode}")

    print("=== third-category-census.py --self-test ===")
    # Законный близнец: есть и различающий, и сообщающий, и читатель.
    case("различает и сообщает, читатель на месте", WF_REPORTS, READER_BODY, 0,
         "РАЗЛИЧАЮТ 1, из них умеют СООБЩИТЬ 1")
    # Инъекция 1: различают, а канала нет ни у кого.
    case("различают, сообщить не умеет никто", WF_RECOGNISE_ONLY, READER_BODY, 1,
         "не доезжает до вердикта")
    # Инъекция 2: канал есть, а читателя нет — число пережило бы свой предмет.
    case("читателя канала нет", WF_REPORTS, READER_MUTE, 1, "у канала нет читателя")
    # Ноль различающих — законное состояние, а не находка: число печатается.
    case("о третьей категории не знает никто", WF_NEITHER, READER_BODY, 0,
         "РАЗЛИЧАЮТ 0")
    # Пустой обход — не зелёное.
    cases += 1
    d = Path(tempfile.mkdtemp())
    (d / ".github" / "workflows").mkdir(parents=True)
    r = subprocess.run([sys.executable, me, "--root", str(d)], capture_output=True, text=True, check=False)
    if r.returncode != 2:
        print(f"  ПРОВАЛ пустой обход → код {r.returncode}, ждали 2")
        ok = False
    else:
        print("  ОК  пустой обход → код 2 (судить не по чему)")

    print(f"осмотрено случаев {cases}; самопроверка: {'PASS' if ok else 'FAIL'}")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--root", default=".")
    ap.add_argument("--self-test", action="store_true")
    a = ap.parse_args()
    if a.self_test:
        return self_test()
    code, text = census(Path(a.root).resolve())
    print(text)
    return code


if __name__ == "__main__":
    sys.exit(main())
