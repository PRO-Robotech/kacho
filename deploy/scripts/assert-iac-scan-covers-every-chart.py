#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Гейт: каждый чарт дерева обязан ПОПАДАТЬ в цели IaC-скана.

ПРЕДМЕТ ГЕЙТА. `trivy config` читает чарт, отрендерив его. Если рендер не удался —
не хватило обязательного значения, не подтянуты зависимости, — сканер ПРОПУСКАЕТ
чарт целиком: молча, кодом возврата 0, без единой строки в отчёте. Снаружи это
неотличимо от «чарт проверен и чист». Отсюда вся ценность гейта скана становится
условной: он защищает ровно ту часть дерева, до которой дошёл, а сколько это —
нигде не сказано.

Замер, из-за которого гейт написан (2026-08-09, перемерен 2026-08-10, trivy
0.64.1): в целях скана было 86 файлов, и среди них НИ ОДНОГО из
`services/registry/deploy` и `services/nlb/deploy`. Оба чарта намеренно ОТКАЗЫВАЮТ
в рендере без секрета оператора — правильная защита, у которой оказался невидимый
побочный эффект. Двадцать манифестов (9 + 11, чарты рендерятся целиком), включая
StatefulSet хранилища слоёв, не проверялись вовсе; введённые в осмотр, они дали 10
новых целей — 86 → 96 — и на первом же прогоне настоящую находку. Числа 20 и 10
измерены РАЗНЫМИ предикатами (отрендеренные манифесты против `Results[].Target`) и
одно из другого не выводится: цель заводится только на манифест, к которому
применима хоть одна проверка.

ПРЕДИКАТ. Кандидат — отслеживаемый чарт, у которого есть СВОИ шаблоны и который не
вложен в другой чарт (подчарт сканер относит к родителю, отдельной целью он не
станет никогда). Каждый кандидат обязан дать хотя бы одну цель. Чарт без шаблонов
кандидатом не является: осматривать в нём нечего, и требовать от него цель значило
бы требовать несуществующего.

ПОСЛАБЛЕНИЕ РОВНО ОДНО И ОНО САМОИСТЕКАЕТ. Зонтичный чарт не рендерится без
`helm dependency update` (его зависимости в git не вендорятся — и правильно), а
запускать сборку зависимостей внутри гейта значило бы сделать вердикт зависимым от
сети. Поэтому он назван поимённо, вместе с причиной, и проверяется В ОБЕ СТОРОНЫ:
если он вдруг ОКАЗАЛСЯ осмотрен — послаблению больше нечего послаблять, и это
находка; если его причина перестала быть правдой (зависимости провендорены, а цели
всё равно нет) — тоже находка, потому что дальше причина была бы фольклором.

ПРИЧИНА МЕРЯЕТСЯ ПО GIT, А НЕ ПО ДИСКУ — ИНАЧЕ ГЕЙТ ЧИТАЕТ ЧУЖОЙ ЧЕРНОВИК КАК
СВОЙСТВО ДЕРЕВА. Формулировка послабления говорит «зависимости В GIT не
вендорятся», и предикат обязан спрашивать ровно об этом. Первая редакция
спрашивала файловую систему — и краснела на КАЖДОЙ машине, где кто-нибудь
запускал `helm dependency update`: его делают и `make dev-up`, и первый же шаг
прогонщика самопроверок, а результат лежит в `charts/*.tgz` и объявлен
git-ignored (`deploy/.gitignore`). Гейт при этом печатал «зависимости
провендорены» там, где git не отслеживает ни одного такого файла, то есть
противоречил той самой причине, которую цитировал. Вердикт, зависящий от
местного черновика, воспроизводимым не бывает: CI видит свежий checkout, где
черновика нет, а разработчик — красное на пустом месте и снимает проверку.

Отсюда три исхода вместо двух, и третий не «устарело»:

  * `.tgz` ОТСЛЕЖИВАЕТСЯ git — причина перестала быть правдой: находка;
  * `.tgz` лежит на диске, но git его не отслеживает — это местный результат
    сборки зависимостей. Послабление в этом прогоне НЕ СУДИТСЯ и печатается как
    несудимое: свойство репозитория отсюда не видно;
  * `.tgz` нет вовсе — причина верна, и тогда работает вторая половина: чарт,
    который всё-таки попал в цели, послаблению больше нечего послаблять.

ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ. «Ноль непокрытых чартов» обязано быть отличимо от
«ноль прочитанных чартов».
"""
import json
import os
import pathlib
import shutil
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
SCAN_CONFIG = ROOT / "trivy.yaml"

# Чарты, которым цель не требуется, — с причиной. Причина обязана быть проверяемой
# (см. reason_still_holds), иначе через полгода это будет фольклор.
EXEMPT = {
    "deploy/helm/umbrella/": "не рендерится без `helm dependency update` — "
                             "зависимости в git не вендорятся",
}


def git_ls(pattern):
    r = subprocess.run(["git", "ls-files", pattern], cwd=ROOT,
                       capture_output=True, text=True, timeout=120)
    if r.returncode != 0:
        print("ОТКАЗ: git ls-files %s вышел с кодом %d: %s"
              % (pattern, r.returncode, r.stderr[:300]), file=sys.stderr)
        sys.exit(2)
    return [line for line in r.stdout.splitlines() if line.strip()]


def scan_targets():
    """→ множество целей, до которых сканер дошёл (с тем же конфигом, что и в CI)."""
    if not shutil.which("trivy"):
        print("ОТКАЗ: trivy не найден в PATH — судить не о чем", file=sys.stderr)
        sys.exit(2)
    env = dict(os.environ)
    env.pop("TRIVY_IGNOREFILE", None)
    cmd = [
        "trivy", "config", ".", "--config", SCAN_CONFIG.name,
        "--severity", "CRITICAL,HIGH", "--format", "json",
        "--skip-dirs", ".claude", "--skip-dirs", "**/node_modules", "--quiet",
    ]
    r = subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True, env=env, timeout=900)
    if r.returncode not in (0, 1):
        print("ОТКАЗ: trivy вышел с кодом %d\n%s" % (r.returncode, r.stderr[:400]),
              file=sys.stderr)
        sys.exit(2)
    doc = json.loads(r.stdout or "{}")
    return {(res.get("Target") or "") for res in doc.get("Results") or []}


def reason_still_holds(chart_dir):
    """→ (вердикт, пояснение). Вердикт: True — причина верна, False — больше не
    верна (находка), None — в этом прогоне НЕ СУДИМА.

    Единица измерения — то, что увидит CI на свежем checkout'е, то есть git.
    Файловая система на машине разработчика несёт ещё и результат
    `helm dependency update` (его делает `make dev-up` и первый шаг прогонщика
    самопроверок), и он git-ignored. Меряя диск, гейт объявлял бы «зависимости
    провендорены» ровно там, где репозиторий их не вендорит, — противореча
    формулировке собственного послабления.
    """
    chart_yaml = ROOT / chart_dir / "Chart.yaml"
    if not chart_yaml.exists():
        return False, "чарта по этому пути больше нет"
    declares_deps = any(line.startswith("dependencies:")
                        for line in chart_yaml.read_text(encoding="utf-8").splitlines())
    if not declares_deps:
        return False, "чарт больше не объявляет зависимостей"
    tracked = git_ls(chart_dir + "charts/*.tgz")
    if tracked:
        return False, ("зависимости провендорены В GIT (%d .tgz) — рендер обязан удаваться"
                       % len(tracked))
    on_disk = list((ROOT / chart_dir / "charts").glob("*.tgz")) \
        if (ROOT / chart_dir / "charts").is_dir() else []
    if on_disk:
        return None, ("в рабочем дереве лежат %d .tgz, которых git не отслеживает, — "
                      "это местный результат `helm dependency update`" % len(on_disk))
    return True, ""


def main():
    if not SCAN_CONFIG.exists():
        print("ОТКАЗ: %s не найден — без него часть чартов не рендерится, и гейт\n"
              "       мерил бы не то множество, что скан в CI." % SCAN_CONFIG.name,
              file=sys.stderr)
        return 2

    charts = sorted({c[: -len("Chart.yaml")] for c in git_ls("*Chart.yaml")})
    if not charts:
        print("ОТКАЗ: в дереве не найдено ни одного Chart.yaml — судить не о чем",
              file=sys.stderr)
        return 2

    templates = git_ls("*/templates/*.yaml")
    targets = scan_targets()
    if not targets:
        print("ОТКАЗ: скан не дошёл НИ ДО ОДНОЙ цели — сканер не отработал",
              file=sys.stderr)
        return 2

    no_templates, nested, exempt_ok, covered, uncovered, findings = [], [], [], [], [], []
    exempt_unjudged = []

    for d in charts:
        has_templates = any(t.startswith(d + "templates/") for t in templates)
        is_nested = any(d != p and d.startswith(p) for p in charts)
        hit = [t for t in targets if t.startswith(d)]

        if d in EXEMPT:
            holds, why = reason_still_holds(d)
            if holds is False:
                findings.append("%s — причина послабления («%s») больше не верна: %s"
                                % (d, EXEMPT[d], why))
            elif holds is None:
                # Третий исход. Рабочее дерево несёт местную сборку зависимостей,
                # которой нет в репозитории: отсюда не видно, что увидит CI, и
                # вердикт о послаблении не выносится ни в какую сторону.
                exempt_unjudged.append((d, why))
            elif hit:
                findings.append("%s — послабление объявлено, но чарт ОСМОТРЕН (%d целей): "
                                "исключать больше нечего" % (d, len(hit)))
            else:
                exempt_ok.append(d)
            continue
        if not has_templates:
            no_templates.append(d)
            continue
        if is_nested:
            nested.append(d)
            continue
        if hit:
            covered.append((d, len(hit)))
        else:
            uncovered.append(d)

    print("iac-chart-coverage: чартов в дереве %d; из них без шаблонов %d, подчартов %d, "
          "послаблений %d (не судимо в этом прогоне %d); кандидатов %d; целей скана %d; "
          "непокрытых %d"
          % (len(charts), len(no_templates), len(nested), len(exempt_ok),
             len(exempt_unjudged), len(covered) + len(uncovered), len(targets),
             len(uncovered)))
    for d, n in covered:
        print("  осмотрен %2d целей  %s" % (n, d))
    for d in no_templates:
        print("  без шаблонов        %s — осматривать нечего" % d)
    for d in nested:
        print("  подчарт             %s — сканер относит его к родителю" % d)
    for d in exempt_ok:
        print("  послабление         %s — %s" % (d, EXEMPT[d]))
    for d, why in exempt_unjudged:
        print("  НЕ СУДИМО           %s — %s.\n"
              "                      Свойство репозитория отсюда не видно: CI берёт\n"
              "                      свежий checkout, где этих файлов нет." % (d, why))

    for d in uncovered:
        findings.append("%s — чарт с шаблонами НЕ ДАЛ сканеру ни одной цели: скорее всего "
                        "рендер отказал (не хватает значения или зависимости), и «ноль "
                        "находок» по нему означает «ноль прочитанного»" % d)

    if findings:
        print("\nНАХОДКИ:")
        for f in findings:
            print("  " + f)
        print("\nПочини рендер (обычно — заглушка ТОЛЬКО ДЛЯ СКАНА в %s) либо, если чарт\n"
              "осматривать нельзя по существу, заведи послабление ЗДЕСЬ, с причиной,\n"
              "которую можно проверить. Молча оставленный чарт вне осмотра — это скан,\n"
              "который отчитывается за дерево, не прочитав его часть." % SCAN_CONFIG.name)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
