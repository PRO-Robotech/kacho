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
  1а. ВЫЗОВ — ИСПОЛНЯЕМЫЙ, А НЕ НАПИСАННЫЙ. Тело шага читается без строк-
     комментариев и без хвостовых (`.github/scripts/ci_text.py`). Приёмка
     показала цену сырого чтения: шаг, где вызов чистки закрыт решёткой, был
     засчитан чистящим — «выкладок семь, находок ноль», а артефакт уехал бы
     сырым. Закомментированный вызов не делает ничего и обязан читаться как
     его отсутствие.
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

sys.path.insert(0, str(Path(__file__).resolve().parent))
from ci_text import shell_code_only, shell_executable  # noqa: E402 — путь дописан выше

SCRUBBER = "scrub-publication.py"
UPLOAD_ACTION = "actions/upload-artifact"

# ── ВТОРОЙ ПРОИЗВОДИТЕЛЬ АРТЕФАКТОВ, И ОН НЕ ВИДЕН ПЕРВОМУ ПРЕДИКАТУ ────────
#
# Сборщик образов выкладывает СВОЙ артефакт — запись сборки (`*.dockerbuild`) —
# САМ, не обращаясь к `actions/upload-artifact`. Предикат «шаг зовёт выкладку»
# такого производителя не видит вовсе: перечислив связанные выкладки, он
# умалчивал о существовании ЦЕЛОГО ВИДА производителей.
#
# Ни объёма этого вида, ни его доли здесь не приводится, и это не пробел:
# читатель этого файла публичен, а всякая величина о живущем сегодня отвечает
# ему на вопрос «куда смотреть» — правка закрывает канал ВПЕРЁД, тогда как уже
# выложенное живёт до истечения своего срока хранения.
#
# Чистить их нельзя by construction: файл собирает и выкладывает чужое действие
# внутри себя, между «собрал» и «выложил» нашего шага нет. Значит исход ровно
# один — канал ЗАКРЫТЬ: у действия есть ручка `DOCKER_BUILD_RECORD_UPLOAD`, и
# `false` отключает выкладку записи. Сводка задания при этом остаётся.
#
# Требование стоит здесь, а не «в договорённости»: без него следующий сборщик
# откроет канал заново и молча.
BUILD_ACTION = "docker/build-push-action"
RECORD_KNOB = "DOCKER_BUILD_RECORD_UPLOAD"
SARIF_ACTION = "github/codeql-action/upload-sarif"

# ── СЛОВАРЬ ПРОИЗВОДИТЕЛЕЙ ЗАКРЫТ С ОБЕИХ СТОРОН, И ЭТО РЕШЕНИЕ ─────────────
#
# Прежняя редакция знала ДВА имени и молчала обо всём остальном: приёмка завела
# по одному четыре других способа выложить артефакт, и гейт остался зелёным,
# продолжая считать «выкладок 6». Словарь из двух имён — это утверждение о двух
# именах, выданное за утверждение о дереве.
#
# ВЫБРАН ОТКАЗ НА НЕЗНАКОМОМ, А НЕ РАСШИРЕНИЕ СЛОВАРЯ. Довод: словарь устареет
# снова — он стареет от каждого нового действия, — а отказ на незнакомом не
# устаревает никогда. Неизвестный способ выложить это «не смотрели», а не
# «чисто», и цена ошибки здесь односторонняя: канал необратим.
#
# Поэтому КАЖДОЕ действие, встречающееся в объявлениях, обязано иметь явный
# вердикт в этой ведомости. Вердиктов три, словарь их закрыт:
#
#   "no"       — не публикует ничего за пределы прогона;
#   "artifact" — публикует; обязана быть связь с чисткой и погашение её исходом;
#   "record"   — публикует СВОЮ запись внутри себя; канал обязан быть закрыт
#                ручкой (чистить нечем: нашего шага между «собрал» и «выложил»
#                нет).
#
# Ведомость сверяется В ОБЕ СТОРОНЫ: действие без записи — находка (неизвестный
# производитель), запись без действия — тоже находка (пережила свой предмет).
ACTION_VERDICTS = {
    "actions/cache/restore": "no",
    "actions/cache/save": "no",
    "actions/checkout": "no",
    "actions/download-artifact": "no",
    "actions/setup-go": "no",
    "actions/setup-node": "no",
    "actions/setup-python": "no",
    "actions/upload-artifact": "artifact",
    "aquasecurity/trivy-action": "no",
    "azure/setup-helm": "no",
    "azure/setup-kubectl": "no",
    "bufbuild/buf-setup-action": "no",
    "docker/build-push-action": "record",
    "docker/login-action": "no",
    "docker/setup-buildx-action": "no",
    "docker/setup-qemu-action": "no",
    "github/codeql-action/upload-sarif": "artifact",
    "opentofu/setup-opentofu": "no",
}

# ВТОРАЯ ПОЛОВИНА — ПУБЛИКАЦИЯ ИЗ ТЕЛА ШАГА, и она ПРИЗНАНА СЛОВАРЁМ ГЛАГОЛОВ.
# Отказ на незнакомом здесь неприменим by construction: тело шага — произвольная
# оболочка, и «незнакомая команда» это каждая вторая строка дерева. Значит здесь
# остаётся перечень, и он назван честно: он устаревает, и держать его может
# только правка. Перечень закрыт и короток — каждая запись есть способ вынести
# байты наружу прогона.
PUBLISH_VERBS = (
    "gh release upload",
    "gh release create",
    "gh api",
    "actions/upload-artifact",
    "upload-pages-artifact",
    "aws s3 cp",
    "gsutil cp",
    "curl -T",
)

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


class Counts:
    """Объём осмотренного. «Ноль находок» обязано быть отличимо от «ноль прочитанного»."""

    def __init__(self) -> None:
        self.jobs = 0
        self.steps = 0
        self.uploads = 0
        self.builds = 0
        self.verbs = 0
        self.calls = 0
        self.actions: set[str] = set()

    def add(self, other: "Counts") -> None:
        self.jobs += other.jobs
        self.steps += other.steps
        self.uploads += other.uploads
        self.builds += other.builds
        self.verbs += other.verbs
        self.calls += other.calls
        self.actions |= other.actions


def audit_workflow(path: Path, doc: dict) -> tuple[list[str], Counts]:
    """Разбирает одно объявление. Возвращает (находки, перепись)."""
    findings: list[str] = []
    jobs = (doc or {}).get("jobs") or {}
    c = Counts()

    for job_name, spec in jobs.items():
        if not isinstance(spec, dict):
            continue
        c.jobs += 1

        # ВЫЗОВ ДЕЙСТВИЯ НА УРОВНЕ РАБОТЫ — ДВЕРЬ, КОТОРУЮ ВЕДОМОСТЬ НЕ
        # ОСМАТРИВАЛА ВОВСЕ. Работа вида `uses: org/flow@sha` шагов не имеет, и
        # обход по шагам проходил мимо неё молча. На сегодняшнем дереве таких
        # работ ноль — это открытая дверь, а не течь, и закрывается она тем же
        # правилом: неизвестное — находка.
        if spec.get("uses"):
            c.calls += 1
            action = str(spec["uses"]).split("@")[0].strip()
            c.actions.add(action)
            if ACTION_VERDICTS.get(action) != "no":
                findings.append(
                    f"{path.name} / {job_name}: работа целиком делегирована действию "
                    f"«{action}», и шагов у неё нет — ни один шаг чистки туда не "
                    f"вставить. Публикует оно что-нибудь или нет, здесь не знают: "
                    f"внесите вердикт в ACTION_VERDICTS либо снимите делегирование."
                )
            continue

        steps = spec.get("steps") or []
        for i, step in enumerate(steps):
            if not isinstance(step, dict):
                continue
            c.steps += 1
            uses = str(step.get("uses") or "")
            where0 = f"{path.name} / {job_name} / шаг #{i + 1}"

            # ── НЕИЗВЕСТНЫЙ ПРОИЗВОДИТЕЛЬ — НАХОДКА, А НЕ МОЛЧАНИЕ ──────────
            if uses:
                action = uses.split("@")[0].strip()
                c.actions.add(action)
                verdict = ACTION_VERDICTS.get(action)
                if verdict is None:
                    findings.append(
                        f"{where0}: действие «{action}» ведомости НЕ ИЗВЕСТНО. "
                        f"Публикует оно что-нибудь или нет — здесь не знают, а "
                        f"«не знаем» это «не смотрели», а не «чисто». Внесите "
                        f"вердикт (no · artifact · record) в ACTION_VERDICTS."
                    )
                    continue
                if verdict == "no":
                    continue

            # ── ПУБЛИКАЦИЯ ИЗ ТЕЛА ШАГА ────────────────────────────────────
            body = shell_executable(str(step.get("run") or ""))
            if body and not uses:
                # ПРОБЕЛЫ НОРМАЛИЗУЮТСЯ ПЕРЕД СЛИЧЕНИЕМ. Подстрочное сравнение
                # не узнавало собственную запись, написанную с двойным пробелом
                # (`gh  release upload`), — то есть перечень не узнавал сам себя.
                flat = re.sub(r"[ \t]+", " ", body)
                hit = [v for v in PUBLISH_VERBS if re.sub(r"[ \t]+", " ", v) in flat]
                # Сама чистка и этот гейт зовут внешние средства ПО ДЕЛУ: они
                # читают, а не публикуют. Признак — вызов нашего же скрипта.
                if hit and SCRUBBER not in flat and "assert-uploads-are-scrubbed" not in flat:
                    c.verbs += 1
                    findings.append(
                        f"{where0}: тело шага выносит байты наружу прогона "
                        f"({', '.join(hit)}), и чистка его не читала. Либо "
                        f"проведите публикацию через выкладку со своей чисткой, "
                        f"либо снимите её."
                    )
                continue

            # Второй производитель: выкладывает запись сборки САМ.
            if BUILD_ACTION in uses:
                c.builds += 1
                env = step.get("env") or {}
                val = str(env.get(RECORD_KNOB, "")).strip().strip("'\"").lower()
                if val != "false":
                    findings.append(
                        f"{path.name} / {job_name} / шаг #{i + 1}: сборщик образов "
                        f"выкладывает запись сборки САМ, минуя чистку, а "
                        f"`env.{RECORD_KNOB}` у него не выставлен в `false`. "
                        f"Чистить её нечем: между «собрал» и «выложил» нашего шага "
                        f"нет, — значит канал обязан быть закрыт."
                    )
                continue

            if UPLOAD_ACTION not in uses and SARIF_ACTION not in uses:
                continue
            c.uploads += 1
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
                # ПРИЗНАК «ВЫЗОВ ЕСТЬ» ЧИТАЕТ КОД БЕЗ ЛИТЕРАЛОВ И БЕЗ ТЕЛ
                # ВЛОЖЕННЫХ ДОКУМЕНТОВ. Приёмка показала ложное молчание: шаг, где
                # единственное упоминание чистки — строка в кавычках, засчитывался
                # связывающим, и выкладка объявлялась чистой. Выбор назван в шапке
                # ci_text.py: исключение ошибается в безопасную сторону — вызов,
                # написанный целиком внутри кавычек, будет объявлен отсутствующим.
                run = shell_code_only(str(prev.get("run") or ""))
                if SCRUBBER in run and f"--step-id {up_id}" in run:
                    scrub = prev
            if scrub is None:
                findings.append(
                    f"{where} (id={up_id}): выше в работе нет шага, зовущего "
                    f"{SCRUBBER} с `--step-id {up_id}` ИСПОЛНЯЕМОЙ строкой. "
                    f"Вызов внутри кавычек, внутри вложенного документа или под "
                    f"решёткой вызовом не считается: артефакт уедет, не будучи "
                    f"прочитанным ни одним шагом."
                )
                continue
            scrub_id = str(scrub.get("id") or "")
            if not scrub_id:
                findings.append(
                    f"{where} (id={up_id}): у шага чистки нет `id` — его исход "
                    f"нечем прочитать, и выкладку нечем погасить"
                )
                continue

            # АДРЕС ЧИСТКИ СВЕРЯЕТСЯ С МЕСТОМ, ГДЕ ОНА СТОИТ. Ключи `--workflow`
            # и `--job` — это координаты узла, из которого берутся маски. Опечатка
            # в них не ловится ничем до прогона: чистка честно ответит «объявление
            # выкладки не прочитано» (код 3), и артефакт не уедет — то есть цена
            # опечатки будет заплачена прогоном, хотя видна она здесь.
            run_txt = shell_executable(str(scrub.get("run") or ""))
            want_wf = f"--workflow .github/workflows/{path.name}"
            want_job = f"--job {job_name}"
            if want_wf not in run_txt or want_job not in run_txt:
                findings.append(
                    f"{where} (id={up_id}): шаг чистки адресован не своему узлу — "
                    f"ожидались «{want_wf}» и «{want_job}». Маски он взял бы из "
                    f"чужого объявления либо не взял вовсе."
                )
                continue

            # Маска, чьё выражение нечем вычислить, найдёт ноль файлов — и это
            # «не смотрели», а не «чисто». Требование стоит ЗДЕСЬ, потому что
            # прогон покажет его только когда артефакт уже не уедет.
            with_node = step.get("with") or {}
            need = expr_keys(str(with_node.get("path") or with_node.get("sarif_file") or ""))
            have = {re.sub(r"\s+", "", m.group("k"))
                    for m in RE_SET.finditer(shell_executable(str(scrub.get("run") or "")))}
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
    return findings, c


def audit(root: Path) -> tuple[int, str]:
    wf_dir = root / ".github" / "workflows"
    files = sorted(p for p in wf_dir.glob("*.y*ml") if p.is_file())
    out: list[str] = []
    findings: list[str] = []
    total = Counts()

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
        fnd, c = audit_workflow(f, doc if isinstance(doc, dict) else {})
        findings.extend(fnd)
        total.add(c)

    # ВЕДОМОСТЬ СВЕРЯЕТСЯ В ОБРАТНУЮ СТОРОНУ: запись, которой в дереве больше
    # нечего судить, переживает свой предмет и превращается в слепую зону —
    # её принимают за покрытие.
    # Судится ТОЛЬКО настоящее дерево: на синтетическом корне населением служат
    # две-три фикстуры, и «записи без предмета» там по построению почти все.
    # Признак настоящего дерева — наличие каталога версий; он же отличает
    # прогон гейта от его самопроверки.
    stale = sorted(set(ACTION_VERDICTS) - total.actions) if (root / ".git").exists() else []
    if stale and total.actions:
        findings.append(
            "ведомость вердиктов пережила свой предмет: в объявлениях больше нет "
            + ", ".join(stale) + ". Снимите записи — иначе они читаются как покрытие."
        )

    out.append(
        f"перепись: объявлений {len(files)}, работ {total.jobs}, шагов {total.steps}, "
        f"публикующих шагов {total.uploads}, сборок образов {total.builds}, "
        f"различных действий {len(total.actions)} (все с вердиктом), "
        f"публикаций из тела шага {total.verbs}, работ-делегирований {total.calls}, "
        f"находок {len(findings)}"
    )

    # ПРЕДПОСЫЛКА ГЕЙТА. Он утверждает свойство выкладок; выкладок ноль — значит
    # факт о дереве изменился, и запрет стал утверждением ни о чём.
    if total.uploads == 0:
        out.append(
            "НЕ ВЫПОЛНИЛОСЬ: осмотрено объявлений {}, а публикующих шагов не нашлось "
            "ни одного. Предпосылка гейта не выполняется: он судит публикацию, а "
            "судить нечего.".format(len(files))
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

BUILD_OPEN = """
name: проба
jobs:
  работа:
    steps:
      - name: сборка
        uses: docker/build-push-action@0000000000000000000000000000000000000000
        with:
          context: .
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

BUILD_CLOSED = """
name: проба
jobs:
  работа:
    steps:
      - name: сборка
        uses: docker/build-push-action@0000000000000000000000000000000000000000
        env:
          DOCKER_BUILD_RECORD_UPLOAD: "false"
        with:
          context: .
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

WRONG_ADDRESS = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка, адресованная чужой работе
        id: чистка-отчётов
        run: python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job другая-работа --step-id выкладка
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

COMMENTED_OUT = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка только на бумаге
        id: чистка-отчётов
        run: |
          # python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id выкладка
          echo "чистить не будем"
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

TRAILING_COMMENT = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка с хвостовым объяснением
        id: чистка-отчётов
        run: |
          echo "сейчас почистим"   # python3 .github/scripts/scrub-publication.py --step-id выкладка
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

QUOTED_HASH = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка, в чьём теле есть решётка внутри кавычек
        id: чистка-отчётов
        run: |
          echo "правка по задаче #1810"
          python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id выкладка
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

UNKNOWN_ACTION = """
name: проба
jobs:
  работа:
    steps:
      - name: чужой способ выложить
        uses: some-org/publish-things@0000000000000000000000000000000000000000
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

PUBLISH_FROM_BODY = """
name: проба
jobs:
  работа:
    steps:
      - name: выложить мимо выкладки
        run: gh release upload v1 ./out/report.json
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

SARIF_BOUND = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка отчёта сканера
        id: отчёт-scrub
        if: always()
        run: python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id отчёт
      - name: выгрузка отчёта
        id: отчёт
        if: ${{ always() && steps.отчёт-scrub.outcome == 'success' }}
        uses: github/codeql-action/upload-sarif@0000000000000000000000000000000000000000
        with:
          sarif_file: gosec.sarif
"""

SARIF_UNBOUND = """
name: проба
jobs:
  работа:
    steps:
      - name: выгрузка отчёта
        id: отчёт
        if: always()
        uses: github/codeql-action/upload-sarif@0000000000000000000000000000000000000000
        with:
          sarif_file: gosec.sarif
"""

CALL_IN_LITERAL = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка только в кавычках
        id: чистка-отчётов
        run: |
          echo "позвать бы python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id выкладка"
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

CALL_IN_HEREDOC = """
name: проба
jobs:
  работа:
    steps:
      - name: чистка внутри вложенного документа
        id: чистка-отчётов
        run: |
          cat <<'EOF' > памятка.txt
          python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id выкладка
          EOF
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

CALL_WITH_QUOTED_ARG = """
name: проба
jobs:
  работа:
    steps:
      - name: настоящий вызов с аргументом в кавычках
        id: чистка-отчётов
        run: |
          python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job работа --step-id выкладка --set 'matrix.dir=out'
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: ${{ matrix.dir }}/*.json
"""

JOB_LEVEL_USES = """
name: проба
jobs:
  работа:
    uses: some-org/reusable-flow@0000000000000000000000000000000000000000
  вторая:
    steps:
      - name: чистка
        id: чистка-отчётов
        run: python3 .github/scripts/scrub-publication.py --workflow .github/workflows/w.yml --job вторая --step-id выкладка
      - name: выкладка
        id: выкладка
        if: ${{ steps.чистка-отчётов.outcome == 'success' }}
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: отчёты
          path: out/*.json
"""

VERB_DOUBLE_SPACE = """
name: проба
jobs:
  работа:
    steps:
      - name: публикация с двойным пробелом
        run: gh  release  upload v1 ./out/report.json
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

    def run_on(body: str, git: bool = False) -> subprocess.CompletedProcess:
        d = Path(tempfile.mkdtemp())
        (d / ".github" / "workflows").mkdir(parents=True)
        (d / ".github" / "workflows" / "w.yml").write_text(body, encoding="utf-8")
        if git:
            (d / ".git").mkdir()
        return subprocess.run(
            [sys.executable, me, "--root", str(d)], capture_output=True, text=True, check=False
        )

    def case(label: str, body: str, want: int, git: bool = False) -> None:
        nonlocal ok, cases
        cases += 1
        r = run_on(body, git)
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
    case("вызов чистки ЗАКОММЕНТИРОВАН", COMMENTED_OUT, 1)
    case("вызов только в хвостовом комментарии", TRAILING_COMMENT, 1)
    case("решётка внутри кавычек кода не отнимает", QUOTED_HASH, 0)
    case("чистка адресована чужой работе", WRONG_ADDRESS, 1)
    case("вызов ТОЛЬКО внутри кавычек", CALL_IN_LITERAL, 1)
    case("вызов ТОЛЬКО внутри вложенного документа", CALL_IN_HEREDOC, 1)
    case("настоящий вызов с аргументом в кавычках", CALL_WITH_QUOTED_ARG, 0)
    case("работа целиком делегирована действию", JOB_LEVEL_USES, 1)
    case("глагол публикации с двойным пробелом", VERB_DOUBLE_SPACE, 1)
    case("неизвестное действие — находка, а не молчание", UNKNOWN_ACTION, 1)
    case("публикация из тела шага мимо выкладки", PUBLISH_FROM_BODY, 1)
    case("отчёт сканера связан с чисткой", SARIF_BOUND, 0)
    case("отчёт сканера без чистки", SARIF_UNBOUND, 1)
    case("ведомость пережила свой предмет (настоящее дерево)", BOUND, 1, git=True)
    case("сборщик образов выкладывает запись сам", BUILD_OPEN, 1)
    case("сборщик образов: канал записи закрыт", BUILD_CLOSED, 0)
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
