#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""assert-uploads-are-scrubbed.py — новая выкладка не обходит чистку.

ПРЕДМЕТ (#1810, предикат снятия 4)
----------------------------------
Чистка содержимого (`.github/scripts/scrub-publication.py`) снимает причину
ровно до того момента, пока в дереве не заведётся ВОСЬМАЯ выкладка — без неё.
Сама чистка этого не заметит: её не позвали, и молчание не позвавшего
неотличимо от молчания чистого.

Поэтому связь держится не вниманием, а этим гейтом. Он разбирает объявления
процессов и требует от КАЖДОЙ выкладки двух вещей:

  1. ЕСТЬ СВОЯ ЧИСТКА. В той же работе, ВЫШЕ по порядку, стоит шаг, зовущий
     `scrub-publication.py` и называющий ИМЕННО этот `--step-id`. Маски путей
     чистка берёт из того же узла, что и выкладка, поэтому «чистят одно,
     выкладывают другое» невозможно by construction.
  2. ВЫКЛАДКА ПОГАШЕНА ЕЁ ИСХОДОМ. Условие выкладки обязано быть в точности
     условием чистки плюс `steps.<id>.outcome == 'success'`. Это и есть
     fail-closed: чистка не сошлась (находка), не выполнилась (код 3) или
     пропущена — публикации НЕТ. Сошлась — есть.

Второе требование несущее, а не украшение к первому. Шаг чистки, чей исход
никто не читает, — это форма без содержания: он краснеет, работа краснеет, а
артефакт уезжает СЫРЫМ, потому что у выкладки стоит `if: always()`.

ПОЧЕМУ ИСКЛЮЧЕНИЙ НЕТ НИ ОДНОГО
-------------------------------
Соблазн — освободить выкладки, «в которых удостоверений не бывает»: собранные
сайты документации, опись шардов, корпус фаззера. Отказано: такое освобождение
есть утверждение о СОДЕРЖИМОМ, сделанное без чтения содержимого, — ровно тот
класс, который здесь и чинится. Чистка читает байты и стоит дёшево; ведомость
освобождённых стоила бы дороже с первой же строки, которую в неё допишут.

Коды возврата:
    0 — каждая выкладка дерева связана со своей чисткой и погашена её исходом;
    1 — находки (перечислены с координатами);
    2 — судить не по чему: объявлений ноль либо ни одной выкладки не найдено.
        Это НЕ зелёное: пустой обход вердиктом не является.

Запуск:
    python3 .github/scripts/assert-uploads-are-scrubbed.py [--root <корень>]
    python3 .github/scripts/assert-uploads-are-scrubbed.py --self-test
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml

SCRUBBER = "scrub-publication.py"
UPLOAD_ACTION = "actions/upload-artifact"

# Выражение объявления внутри маски пути (`${{ matrix.dir }}`) вычисляет
# провайдер. Чистка читает маску из файла и видит его дословно, поэтому значение
# ей подаёт вызывающий — ключом `--set`. Не подал — чистка честно ответит «не
# выполнилось», и выкладки не будет; гейт ловит это РАНЬШЕ прогона.
RE_EXPR = re.compile(r"\$\{\{(?P<e>[^}]*)\}\}")
RE_SET = re.compile(r"--set\s+['\"]?(?P<k>[^='\"]+)=")


def expr_keys(text: str) -> set[str]:
    return {re.sub(r"\s+", "", m.group("e")) for m in RE_EXPR.finditer(text or "")}


def normalise(cond: object) -> str:
    """Условие шага в сравнимом виде: без обёртки выражения и лишних пробелов."""
    if cond is None:
        return ""
    s = str(cond).strip()
    if s.startswith("${{") and s.endswith("}}"):
        s = s[3:-2]
    s = s.replace('"', "'")
    return re.sub(r"\s+", " ", s).strip()


class Finding(str):
    pass


def audit_workflow(path: Path, doc: dict) -> tuple[list[str], int, int, int]:
    """Разбирает одно объявление. Возвращает (находки, работ, шагов, выкладок)."""
    findings: list[str] = []
    jobs = (doc or {}).get("jobs") or {}
    njobs = nsteps = nuploads = 0

    for job_name, spec in jobs.items():
        if not isinstance(spec, dict):
            continue
        njobs += 1
        steps = spec.get("steps") or []
        for i, step in enumerate(steps):
            if not isinstance(step, dict):
                continue
            nsteps += 1
            if UPLOAD_ACTION not in str(step.get("uses") or ""):
                continue
            nuploads += 1
            where = f"{path.name} / {job_name} / шаг #{i + 1}"
            up_id = str(step.get("id") or "")
            if not up_id:
                findings.append(
                    f"{where}: у выкладки нет `id` — чистке нечего называть "
                    f"ключом --step-id, и связь недоказуема"
                )
                continue

            # Чистка ИМЕННО этой выкладки — выше по порядку в той же работе.
            scrub = None
            for prev in steps[:i]:
                if not isinstance(prev, dict):
                    continue
                run = str(prev.get("run") or "")
                if SCRUBBER in run and f"--step-id {up_id}" in run:
                    scrub = prev
            if scrub is None:
                findings.append(
                    f"{where} (id={up_id}): выше в работе нет шага, зовущего "
                    f"{SCRUBBER} с `--step-id {up_id}`. Артефакт уедет, "
                    f"не будучи прочитанным ни одним шагом."
                )
                continue
            scrub_id = str(scrub.get("id") or "")
            if not scrub_id:
                findings.append(
                    f"{where} (id={up_id}): у шага чистки нет `id` — его исход "
                    f"нечем прочитать, и выкладку нечем погасить"
                )
                continue

            # Маска, чьё выражение нечем вычислить, найдёт ноль файлов — и это
            # «не смотрели», а не «чисто». Требование стоит ЗДЕСЬ, потому что
            # прогон покажет его только когда артефакт уже не уедет.
            need = expr_keys(str((step.get("with") or {}).get("path") or ""))
            have = {re.sub(r"\s+", "", m.group("k")) for m in RE_SET.finditer(str(scrub.get("run") or ""))}
            missing = sorted(need - have)
            if missing:
                findings.append(
                    f"{where} (id={up_id}): маска выкладки несёт выражения "
                    f"{', '.join(missing)}, а шаг чистки их значений не подаёт "
                    f"(`--set <выражение>=…`). Чистка прочитала бы маску дословно, "
                    f"не нашла бы ни одного файла и ответила «не выполнилось»."
                )
                continue

            want = f"steps.{scrub_id}.outcome == 'success'"
            got = normalise(step.get("if"))
            base = normalise(scrub.get("if"))
            expect = f"{base} && {want}" if base else want
            if got != expect:
                findings.append(
                    f"{where} (id={up_id}): условие выкладки не погашено исходом "
                    f"чистки.\n      ожидалось: {expect}\n      объявлено:  "
                    f"{got or '(условия нет)'}\n      Без этого шаг чистки краснеет, "
                    f"а артефакт уезжает сырым: у выкладки своё условие."
                )
    return findings, njobs, nsteps, nuploads


def audit(root: Path) -> tuple[int, str]:
    wf_dir = root / ".github" / "workflows"
    files = sorted(p for p in wf_dir.glob("*.y*ml") if p.is_file())
    out: list[str] = []
    findings: list[str] = []
    njobs = nsteps = nuploads = 0

    if not files:
        return 2, (
            f"НЕ ВЫПОЛНИЛОСЬ: в {wf_dir} нет ни одного объявления процесса — "
            f"обход пуст, и ноль находок здесь ничего не означает."
        )

    for f in files:
        try:
            doc = yaml.safe_load(f.read_text(encoding="utf-8"))
        except yaml.YAMLError as exc:
            findings.append(f"{f.name}: объявление не разбирается ({exc.__class__.__name__}) — не осмотрено")
            continue
        fnd, j, s, u = audit_workflow(f, doc if isinstance(doc, dict) else {})
        findings.extend(fnd)
        njobs += j
        nsteps += s
        nuploads += u

    out.append(
        f"перепись: объявлений {len(files)}, работ {njobs}, шагов {nsteps}, "
        f"выкладок {nuploads}, находок {len(findings)}"
    )

    # ПРЕДПОСЫЛКА ГЕЙТА. Он утверждает свойство выкладок; выкладок ноль — значит
    # факт о дереве изменился, и запрет стал утверждением ни о чём.
    if nuploads == 0:
        out.append(
            "НЕ ВЫПОЛНИЛОСЬ: осмотрено объявлений {}, а выкладок не нашлось ни одной. "
            "Предпосылка гейта не выполняется: он судит выкладки, а судить нечего.".format(len(files))
        )
        return 2, "\n".join(out)

    if findings:
        out.append("НАХОДКИ:")
        out.extend(f"  · {x}" for x in findings)
        out.append(
            "Исходы два: связать выкладку с чисткой ТЕМ ЖЕ изменением, которым она "
            "заведена, либо снять выкладку. Третьего («пока оставим») нет: артефакт "
            "публичного репозитория удалением не отзывается."
        )
        return 1, "\n".join(out)

    out.append("каждая выкладка дерева читается чисткой и погашена её исходом")
    return 0, "\n".join(out)


# ─── САМОПРОВЕРКА ────────────────────────────────────────────────────────────

BOUND = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка
        id: чистка-отчётов
        run: python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id выкладка
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

UNBOUND = """
name: проба
jobs:
  работа:
    steps:
      - name: выкладка
        id: выкладка
        if: always()
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

NOT_GATED = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка
        id: чистка-отчётов
        run: python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id выкладка
      - name: выкладка
        id: выкладка
        if: always()
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

FOREIGN_SCRUB = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка ЧУЖОЙ выкладки
        id: чистка-отчётов
        run: python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id другая
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

BOUND_WITH_CONDITION = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка
        id: чистка-отчётов
        if: ${{ !cancelled() }}
        run: python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id выкладка
      - name: выкладка
        id: выкладка
        if: ${{ !cancelled() && steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

EXPR_UNSET = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка
        id: чистка-отчётов
        run: python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id выкладка
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: ${{ matrix.dir }}/out/*.json
"""

EXPR_SET = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка
        id: чистка-отчётов
        run: python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id выкладка --set 'matrix.dir=${{ matrix.dir }}'
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: ${{ matrix.dir }}/out/*.json
"""

NO_UPLOADS = """
name: проба
jobs:
  работа:
    steps:
      - name: ничего не выкладывает
        run: echo ok
"""


def self_test() -> int:
    me = str(Path(__file__).resolve())
    ok = True
    cases = 0

    def run_on(body: str) -> subprocess.CompletedProcess:
        d = Path(tempfile.mkdtemp())
        (d / ".github" / "workflows").mkdir(parents=True)
        (d / ".github" / "workflows" / "w.yml").write_text(body, encoding="utf-8")
        return subprocess.run(
            [sys.executable, me, "--root", str(d)], capture_output=True, text=True, check=False
        )

    def case(label: str, body: str, want: int) -> None:
        nonlocal ok, cases
        cases += 1
        r = run_on(body)
        if r.returncode == want:
            print(f"  ОК  {label} → код {r.returncode}")
        else:
            print(f"  ПРОВАЛ {label} → код {r.returncode}, ждали {want}")
            print("        " + (r.stdout + r.stderr).replace("\n", "\n        ")[:900])
            ok = False

    print("=== assert-uploads-are-scrubbed.py --self-test ===")
    # Законный близнец: связано и погашено — молчание.
    case("связано и погашено", BOUND, 0)
    case("связано, условие с оговоркой", BOUND_WITH_CONDITION, 0)
    # Инъекции, по одной на каждое утверждение гейта.
    case("выкладка без чистки", UNBOUND, 1)
    case("чистка есть, исход не читается", NOT_GATED, 1)
    case("чистка называет ЧУЖОЙ шаг", FOREIGN_SCRUB, 1)
    case("выражение в маске без --set", EXPR_UNSET, 1)
    case("выражение в маске с --set", EXPR_SET, 0)
    # Предпосылка: судить нечего — это не зелёное.
    case("выкладок ноль — предпосылка гейта не выполняется", NO_UPLOADS, 2)

    print(f"осмотрено случаев {cases}; самопроверка: {'PASS' if ok else 'FAIL'}")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--root", default=".")
    ap.add_argument("--self-test", action="store_true")
    a = ap.parse_args()
    if a.self_test:
        return self_test()
    code, text = audit(Path(a.root).resolve())
    print(text)
    return code


if __name__ == "__main__":
    sys.exit(main())
