#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Гейт: заглушка IaC-скана обязана только ОТКРЫВАТЬ находки, никогда не гасить.

ПРЕДМЕТ ГЕЙТА. `trivy.yaml` несёт значения ТОЛЬКО ДЛЯ СКАНА — заглушки, снимающие
намеренный отказ рендера у чартов, которые требуют координат от оператора. Без них
такой чарт сканер пропускает целиком, молча и с кодом 0.

Заглушка применяется КО ВСЕМ чартам сразу: области чарта у `--helm-set` нет вовсе
(`trivy config --help`, 0.74.0). Значит всякая заглушка подменяет свой ключ и там,
где его никто не просил. Пока подменяется величина, которую misconfig-проверки не
читают (имя узла базы, имя секрета), это безобидно. Как только подменяется величина,
которую проверка ЧИТАЕТ, — находка исчезает, и отчёт становится описанием
конфигурации, которую никто не развернёт.

ЧЕМ ЭТО ОТЛИЧАЕТСЯ ОТ СОСЕДНЕГО ГЕЙТА. `assert-iac-scan-covers-every-chart.py` ловит
заглушку, которая ВЫБИЛА чарт из осмотра: у чарта пропадают цели. Этот ловит другое —
чарт осмотрен, целей ровно столько же, а находка пропала. Первым это не ловится by
construction: он считает цели, а не находки.

ПРЕДИКАТ — ПОШТУЧНЫЙ, А НЕ «ВСЕ ПРОТИВ НИ ОДНОЙ», и это не педантизм: наивная форма
НЕ РАБОТАЕТ, что показала инъекция, а не рассуждение. Сравнивая полный набор заглушек
с пустым, гейт не видит погашенного у чарта, который БЕЗ заглушек не рендерится вовсе:
находки там нет ни с заглушками, ни без них, и разность пуста. Ровно так первая
редакция осталась зелёной на заведомо гасящей заглушке.

Поэтому скан идёт N+1 раз: полный набор и по разу без КАЖДОЙ заглушки. Для заглушки S
   открывает S = находки(полный) \\ находки(без S)
   гасит     S = находки(без S) \\ находки(полный)
Открывать позволено (ради этого заглушка и стоит), гасить — ни одной.

Замер, из-за которого гейт написан (trivy 0.74.0, дерево линии `release/kaname`):
`image.repository=gcr.io/trivy-scan-only` в списке заглушек оставляет ВСЕ 107 целей
на месте и гасит две настоящие находки KSV-0125 («образы только из доверенных
реестров») — у `services/nlb/deploy` и `services/registry/deploy`. Счёт целей не
меняется ни на единицу, то есть соседний гейт остаётся зелёным. Это и есть класс.

СУДЯТСЯ ВСЕ severity, а не только гейтовые CRITICAL/HIGH. Заглушка, гасящая сегодня
MEDIUM, гасит завтра CRITICAL: переклассификация правила происходит на стороне
апстрима и о нашем пороге не знает.

ЗАГЛУШКА БЕЗ ПРЕДМЕТА — ТОЖЕ НАХОДКА. Если её снятие не меняет НИЧЕГО — ни целей, ни
находок, — она ничего не открывает и подменять величину права не имеет. Тот же
порядок, что у ведомости исключений рядом: запись, которой нечего исключать,
самоистекает.

ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ. «Ноль погашенных находок» обязано быть отличимо от
«ноль прочитанных находок».
"""
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[2]
SCAN_CONFIG = ROOT / "trivy.yaml"


def run_scan(config_path):
    """→ (множество целей, множество находок (цель, правило)) для данного конфига."""
    env = dict(os.environ)
    # Ignorefile здесь ВРЕДЕН: он отфильтровал бы находки ещё до сравнения, и
    # погашенная заглушкой находка была бы неотличима от прощённой записью.
    env.pop("TRIVY_IGNOREFILE", None)
    cmd = [
        "trivy", "config", ".", "--config", str(config_path),
        "--format", "json",
        "--skip-dirs", ".claude", "--skip-dirs", "**/node_modules", "--quiet",
    ]
    r = subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True, env=env, timeout=900)
    if r.returncode not in (0, 1):
        print("ОТКАЗ: trivy вышел с кодом %d\n%s" % (r.returncode, r.stderr[:400]),
              file=sys.stderr)
        sys.exit(2)
    doc = json.loads(r.stdout or "{}")
    results = doc.get("Results") or []
    targets = {res.get("Target") or "" for res in results}
    findings = {
        ((res.get("Target") or ""), (mis.get("ID") or ""))
        for res in results
        for mis in (res.get("Misconfigurations") or [])
        if mis.get("Status") == "FAIL"
    }
    return targets, findings


def scan_with(stub_list):
    """Прогон с ЗАДАННЫМ списком заглушек: остальные настройки файла сохраняются."""
    doc = yaml.safe_load(SCAN_CONFIG.read_text(encoding="utf-8")) or {}
    doc["misconfiguration"]["helm"]["set"] = list(stub_list)
    fh = tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False, encoding="utf-8")
    try:
        yaml.safe_dump(doc, fh, allow_unicode=True)
        fh.close()
        return run_scan(fh.name)
    finally:
        os.unlink(fh.name)


def main():
    if not shutil.which("trivy"):
        print("ОТКАЗ: trivy не найден в PATH — судить не о чем", file=sys.stderr)
        return 2
    if not SCAN_CONFIG.exists():
        print("ОТКАЗ: %s не найден — заглушек нет, и сравнивать нечего с чем"
              % SCAN_CONFIG.name, file=sys.stderr)
        return 2

    doc = yaml.safe_load(SCAN_CONFIG.read_text(encoding="utf-8")) or {}
    helm = ((doc.get("misconfiguration") or {}).get("helm") or {})
    stubs = list(helm.get("set") or [])
    if not stubs:
        print("scan-stubs-hide-nothing: заглушек в %s не объявлено — судить нечего; "
              "механизм ждёт следующей записи" % SCAN_CONFIG.name)
        return 0

    targets_all, findings_all = scan_with(stubs)
    if not targets_all:
        print("ОТКАЗ: скан с заглушками не дошёл НИ ДО ОДНОЙ цели — сканер не отработал",
              file=sys.stderr)
        return 2

    findings, lines, opened_total = [], [], 0
    for stub in stubs:
        rest = [s for s in stubs if s != stub]
        targets_without, findings_without = scan_with(rest)
        hides = sorted(findings_without - findings_all)
        opens = sorted(findings_all - findings_without)
        opened_total += len(opens)
        if not hides and not opens and targets_without == targets_all:
            findings.append("%s — снятие этой заглушки не меняет НИЧЕГО: ни целей, ни "
                            "находок. Она ничего не открывает, а значит подменять "
                            "величину права не имеет" % stub)
            lines.append("  без предмета        %s" % stub)
            continue
        lines.append("  открывает %2d, гасит %d  %s" % (len(opens), len(hides), stub))
        for target, rule in hides:
            findings.append("%s — гасит %s у %s: значение ТОЛЬКО ДЛЯ СКАНА подменило "
                            "величину, которую проверка читает" % (stub, rule, target))

    print("scan-stubs-hide-nothing: заглушек %d; прогонов скана %d; целей %d; находок "
          "%d; открыто заглушками %d; погашено %d"
          % (len(stubs), len(stubs) + 1, len(targets_all), len(findings_all),
             opened_total, sum(1 for f in findings if " — гасит " in f)))
    for line in lines:
        print(line)

    if findings:
        print("\nНАХОДКИ:")
        for f in findings:
            print("  " + f)
        print("\nЗаглушка обязана снимать отказ РЕНДЕРА, а не находку. Задай её ключом,\n"
              "которого проверка не читает (эталон — под-ключ `image.trivyScanOnly`\n"
              "вместо `image.repository`: он делает значение непустым, не подменяя\n"
              "координату образа), либо введи чарт в осмотр иначе. Скан, читающий\n"
              "конфигурацию, которую никто не развернёт, отчитывается не за дерево.")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
