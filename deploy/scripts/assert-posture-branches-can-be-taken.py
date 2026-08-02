#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Ветка на посадке посева обязана быть способной уйти в обе стороны.

ЧТО ИЗМЕРЕНО (ревизия 9c8ffc8e)
-------------------------------
`tests/authz-fixtures/setup.sh` классифицирует посадку и пишет её в
`out/seed-posture`. Классификатор оставляет стоять РОВНО ОДНО значение —
`production`; `dev` отвергается отдельной ветвью, всё прочее не опознано, и обе
ветви завершают процесс. Сам setup.sh это и говорит у безусловной делегации:
«production is the only value classify_posture leaves standing, so a guarded
delegation here would be a branch that cannot be false».

Два его потребителя несли ровно такую ветвь:

    SEED_POSTURE_RAN="$(cat …/out/seed-posture 2>/dev/null || echo dev)"
    if … && [ "$SEED_POSTURE_RAN" != "production" ]; then   # никогда не истинно

Условие ложно ВСЕГДА, обоими путями сразу: посев прошёл — в файле `production`;
посев сорвался — прогон прекращён раньше чтения (`set -e` у одного, явный
`exit 2` у другого). Оба блока досева были недостижимы, а их запасное значение
называло `dev` — посадку, которую сам классификатор ОТВЕРГАЕТ. Форма проверки
есть, предмета нет.

ЧТО ПРОВЕРЯЕТСЯ
---------------
1. ПРОИЗВОДИМОЕ МНОЖЕСТВО — из производителя, а не из головы: значения, которые
   `classify_posture` присваивает и тем оставляет стоять.
2. ЗАПАСНОЕ ЗНАЧЕНИЕ читателя обязано быть производимым. Запасное значение,
   которого производитель не выдаёт, — утверждение о посадке, которой не бывает.
3. СРАВНЕНИЕ на прочитанном значении обязано быть способным уйти в обе стороны.
   Пока производимое значение ОДНО, любое сравнение с ним — константа, а
   охраняемый им блок мёртв.

ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ, И ГЕЙТ САМ ИСТЕКАЕТ. Запрет держится на факте
«производимое значение одно». Появится второе — п.3 замолкает сам, потому что
ветвление станет осмысленным; а если разбор производителя перестанет находить
хоть одно значение, это не «ноль находок», а сломанный разбор: гейт падает и
говорит об этом. Перепись осмотренного печатается по той же причине —
«ноль находок» обязано отличаться от «ноль прочитанного».

ЧИТАЕТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ. Слова `seed-posture`, `production` и
`dev` стоят в шапках и объяснениях (в этой — тоже). Строки-комментарии
отбрасываются до сличения.

Запуск:
    python3 deploy/scripts/assert-posture-branches-can-be-taken.py
    python3 deploy/scripts/assert-posture-branches-can-be-taken.py --self-test
Код возврата: 0 — все ветви на посадке способны уйти в обе стороны; 1 — находка
либо предпосылка не подтвердилась.
"""
from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
import tempfile

# Производитель и артефакт, которым он объявляет посадку.
PRODUCER = "tests/authz-fixtures/setup.sh"
ARTEFACT = "seed-posture"

EXTS = (".sh", ".bash", ".py", ".go", ".yaml", ".yml", ".mk")

# Присваивание, оставляющее значение стоять: `SEED_POSTURE="production"`.
ASSIGNS_POSTURE = re.compile(r"""\bSEED_POSTURE\s*=\s*["']([A-Za-z0-9_-]+)["']""")

# Чтение артефакта с запасным значением: `... || echo dev)`.
READS_WITH_FALLBACK = re.compile(
    r"""seed-posture.*?\|\|\s*echo\s+["']?([A-Za-z0-9_-]+)["']?""")

# Переменная, в которую легло прочитанное: `VAR="$(cat …seed-posture…)"`.
HOLDER = re.compile(r"""^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*["']?\$\(.*seed-posture""")


def strip_comments(text: str) -> str:
    """Отбросить строки-комментарии — признак ищется в исполняемой части."""
    out = []
    for line in text.splitlines():
        stripped = line.lstrip()
        if stripped.startswith("#") or stripped.startswith("//"):
            out.append("")
        else:
            out.append(line)
    return "\n".join(out)


def tracked_files(root: str) -> list[str]:
    """Обход по СОДЕРЖИМОМУ репозитория — то же множество, что увидит CI."""
    try:
        out = subprocess.run(["git", "ls-files"], cwd=root, capture_output=True,
                             text=True, check=True).stdout.split()
    except (subprocess.CalledProcessError, FileNotFoundError):
        out = []
        for dirpath, dirnames, filenames in os.walk(root):
            dirnames[:] = [d for d in dirnames if d not in (".git", "node_modules")]
            for fn in filenames:
                out.append(os.path.relpath(os.path.join(dirpath, fn), root))
    return [p for p in out if p.endswith(EXTS)]


def producible(root: str) -> set[str]:
    """Значения, которые производитель оставляет стоять."""
    path = os.path.join(root, PRODUCER)
    if not os.path.exists(path):
        return set()
    with open(path, encoding="utf-8", errors="replace") as fh:
        body = strip_comments(fh.read())
    vals = set()
    for m in ASSIGNS_POSTURE.finditer(body):
        v = m.group(1)
        # `SEED_POSTURE="${SEED_POSTURE:-auto}"` — не литерал классификации.
        if v == "auto":
            continue
        vals.add(v)
    return vals


def scan(root: str, skip_self: bool = True) -> tuple[set[str], list[str], list[str], int]:
    """→ (производимые, читатели, находки, осмотрено файлов).

    САМ ГЕЙТ ИЗ ПЕРЕПИСИ ИСКЛЮЧЁН, именно и проверяемо. Его шапка ЦИТИРУЕТ
    разобранную ветвь дословно, а самопроверка собирает её же фикстурой — то есть
    воспроизведение дефекта неотличимо от его совершения. Комментарии до сличения
    отбрасываются, но шапка — строковый литерал, а не комментарий, поэтому она бы
    осталась и гейт нашёл бы сам себя (что он и сделал при первом прогоне: три
    находки, все в этом файле).

    Исключение НЕ молчаливое и НЕ вечное: `skip_self=False` даёт тот же обход без
    него, и самопроверка требует, чтобы в этом режиме файл НАХОДИЛСЯ. Перепишут
    шапку так, что цитаты в ней не останется, — у исключения исчезнет предмет, и
    самопроверка об этом скажет вместо того, чтобы тихо унаследовать слепую зону.
    """
    can_be = producible(root)
    readers: set[str] = set()
    findings: list[str] = []
    examined = 0
    myself = os.path.realpath(__file__)

    for rel in tracked_files(root):
        full = os.path.join(root, rel)
        if skip_self and os.path.realpath(full) == myself:
            continue
        try:
            with open(full, encoding="utf-8", errors="replace") as fh:
                raw = fh.read()
        except OSError:
            continue
        examined += 1
        if ARTEFACT not in raw:
            continue
        body = strip_comments(raw)
        if ARTEFACT not in body or rel == PRODUCER:
            continue
        readers.add(rel)

        lines = body.splitlines()
        holders: set[str] = set()
        for i, line in enumerate(lines, start=1):
            # (2) запасное значение обязано быть производимым.
            for m in READS_WITH_FALLBACK.finditer(line):
                fb = m.group(1)
                if fb not in can_be:
                    findings.append(
                        f"{rel}:{i}: запасное значение {fb!r} производитель не выдаёт "
                        f"(производимые: {sorted(can_be) or '—'}) — ветвь на нём не бывает истинной")
            hm = HOLDER.match(line)
            if hm:
                holders.add(hm.group(1))

        # (3) сравнение на прочитанном значении, пока производимое значение одно.
        if len(can_be) <= 1:
            for var in holders:
                cmp_re = re.compile(
                    r"""\[\[?[^\n\]]*\$\{?""" + re.escape(var) + r"""\}?[^\n\]]*"""
                    r"""(?:!=|==|=)\s*["']?([A-Za-z0-9_-]+)""")
                for i, line in enumerate(lines, start=1):
                    m = cmp_re.search(line)
                    if not m:
                        continue
                    findings.append(
                        f"{rel}:{i}: ветвь на ${var} сравнивает с {m.group(1)!r}, а производитель "
                        f"оставляет стоять единственное значение {sorted(can_be)} — "
                        f"условие константно, охраняемый блок недостижим")
    return can_be, sorted(readers), sorted(findings), examined


def run(root: str) -> int:
    can_be, readers, findings, examined = scan(root)

    # Предпосылка: разбор производителя обязан что-то найти.
    if not can_be:
        print(f"ПРЕДПОСЫЛКА НЕ ПОДТВЕРЖДЕНА: в {PRODUCER} не найдено ни одного значения, "
              f"которое классификатор оставляет стоять. Это не «ноль находок», а сломанный "
              f"разбор либо исчезнувший производитель.", file=sys.stderr)
        return 1

    print(f"производимые посадки: {sorted(can_be)}  ·  читателей артефакта "
          f"{ARTEFACT!r}: {len(readers)}  ·  осмотрено файлов: {examined}")
    for r in readers:
        print(f"  читатель: {r}")

    if findings:
        print(f"\nНАХОДОК: {len(findings)}", file=sys.stderr)
        for f in findings:
            print(f"  {f}", file=sys.stderr)
        print("\nВетвь, которая не может уйти в обе стороны, — это форма проверки без "
              "предмета: охраняемый ею блок не исполнится никогда, и ни один прогон об "
              "этом не скажет.", file=sys.stderr)
        return 1
    return 0


def self_test() -> int:
    """Инъекция настоящего дефекта — и законный близнец той же формы."""
    ok = True

    def w(root: str, rel: str, body: str) -> None:
        full = os.path.join(root, rel)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w", encoding="utf-8") as fh:
            fh.write(body)

    # ── ДЕФЕКТ: одно производимое значение, читатель на нём ветвится ──────────
    with tempfile.TemporaryDirectory() as td:
        w(td, PRODUCER,
          'classify_posture() {\n'
          '  case "$1" in\n'
          '    production|production-strict) SEED_POSTURE="production" ;;\n'
          '    dev) refuse_relaxed_posture "$2" ;;\n'
          '    *) exit 1 ;;\n'
          '  esac\n'
          '}\n'
          'echo "$SEED_POSTURE" > "$OUT_DIR/seed-posture"\n')
        # (а) ровно та ветвь, что была в дереве: константное условие + чужое запасное.
        w(td, "deploy/scripts/driver_dead.sh",
          'RAN="$(cat out/seed-posture 2>/dev/null || echo dev)"\n'
          'if [ "$RAN" != "production" ]; then\n'
          '  bash seed-more.sh\n'
          'fi\n')
        # (б) ЗАКОННЫЙ БЛИЗНЕЦ той же формы: слова есть, но только в прозе.
        #     Обязан не попасть ни в читатели, ни в находки — иначе первый же
        #     объясняющий комментарий станет находкой.
        w(td, "deploy/scripts/prose_only.sh",
          '# Раньше здесь читался out/seed-posture и сравнивался с production,\n'
          '# а запасным значением стояло dev. Ветвь убрана.\n'
          'bash seed-more.sh\n')

        can_be, readers, findings, examined = scan(td)
        if can_be != {"production"}:
            print(f"SELF-TEST FAIL: производимые {sorted(can_be)} != ['production']", file=sys.stderr)
            ok = False
        if readers != ["deploy/scripts/driver_dead.sh"]:
            print(f"SELF-TEST FAIL: читатели {readers} != ['deploy/scripts/driver_dead.sh'] "
                  f"(проза не должна считаться читателем)", file=sys.stderr)
            ok = False
        got_fallback = [f for f in findings if "запасное значение" in f]
        got_const = [f for f in findings if "константно" in f]
        if len(got_fallback) != 1:
            print(f"SELF-TEST FAIL: запасное значение не поймано: {findings}", file=sys.stderr)
            ok = False
        if len(got_const) != 1:
            print(f"SELF-TEST FAIL: константная ветвь не поймана: {findings}", file=sys.stderr)
            ok = False
        if run(td) != 1:
            print("SELF-TEST FAIL: внесённый дефект не покраснел", file=sys.stderr)
            ok = False

    # ── ЗАКОННЫЙ БЛИЗНЕЦ: два производимых значения ⇒ ветвление осмысленно ────
    # Та же синтаксическая форма, что и в дефекте. Гейт обязан ЗАМОЛЧАТЬ: он
    # запрещает не ветвление как таковое, а ветвление, которое не может уйти в
    # обе стороны. Без этой половины гейт ловил бы форму, а не существо, и
    # отключился бы на первом же законном случае.
    with tempfile.TemporaryDirectory() as td2:
        w(td2, PRODUCER,
          'classify_posture() {\n'
          '  case "$1" in\n'
          '    production) SEED_POSTURE="production" ;;\n'
          '    relaxed) SEED_POSTURE="relaxed" ;;\n'
          '    *) exit 1 ;;\n'
          '  esac\n'
          '}\n'
          'echo "$SEED_POSTURE" > "$OUT_DIR/seed-posture"\n')
        w(td2, "deploy/scripts/driver_live.sh",
          'RAN="$(cat out/seed-posture 2>/dev/null || echo production)"\n'
          'if [ "$RAN" != "production" ]; then\n'
          '  bash seed-more.sh\n'
          'fi\n')
        can_be2, _, findings2, _ = scan(td2)
        if can_be2 != {"production", "relaxed"}:
            print(f"SELF-TEST FAIL: производимые {sorted(can_be2)} != ['production','relaxed']",
                  file=sys.stderr)
            ok = False
        if findings2:
            print(f"SELF-TEST FAIL: законная ветвь объявлена находкой: {findings2}", file=sys.stderr)
            ok = False
        if run(td2) != 0:
            print("SELF-TEST FAIL: законная конструкция покраснела", file=sys.stderr)
            ok = False

    # ── ПРЕДПОСЫЛКА: производителя нет ⇒ отказ, а не «чисто» ──────────────────
    with tempfile.TemporaryDirectory() as td3:
        w(td3, "deploy/scripts/x.sh", 'echo hi\n')
        if run(td3) != 1:
            print("SELF-TEST FAIL: дерево без производителя объявлено чистым", file=sys.stderr)
            ok = False

    # ── ГЕЙТ СЕБЯ НЕ НАХОДИТ — И У ИСКЛЮЧЕНИЯ ЕСТЬ ПРЕДМЕТ ────────────────────
    # Две половины, и вторая важнее. Первая — что исключение работает. Вторая —
    # что ему ещё есть что исключать: без него файл ОБЯЗАН находиться, иначе
    # исключение пережило свой предмет и осталось бы слепой зоной для следующего.
    repo_root = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
    self_rel = os.path.relpath(os.path.abspath(__file__), repo_root)
    _, live_readers, _, _ = scan(repo_root)
    if self_rel in live_readers:
        print(f"SELF-TEST FAIL: гейт нашёл сам себя ({self_rel})", file=sys.stderr)
        ok = False
    _, unguarded_readers, unguarded_findings, _ = scan(repo_root, skip_self=False)
    if self_rel not in unguarded_readers or not unguarded_findings:
        print(f"SELF-TEST FAIL: у самоисключения не осталось предмета — без него "
              f"{self_rel} не находится. Шапка больше не цитирует разобранную ветвь: "
              f"снимите исключение вместо того, чтобы держать его пустым.", file=sys.stderr)
        ok = False

    print("SELF-TEST OK" if ok else "SELF-TEST FAILED")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--self-test", action="store_true",
                    help="доказать инъекцией, что гейт краснеет на мёртвой ветви и молчит на живой")
    ap.add_argument("--root", default=None, help="корень обхода (по умолчанию — корень репозитория)")
    args = ap.parse_args()
    if args.self_test:
        return self_test()
    root = args.root or os.path.abspath(
        os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
    return run(root)


if __name__ == "__main__":
    sys.exit(main())
