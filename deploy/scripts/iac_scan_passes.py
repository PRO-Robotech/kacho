# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Проходы IaC-скана: какой срез дерева каким файлом настроек осматривается.

ЕДИНСТВЕННЫЙ ИСТОЧНИК. Проходы читают три гейта (`assert-iac-scan-covers-every-chart.py`,
`assert-iac-exclusions-still-have-a-subject.py`, `assert-scan-stubs-hide-nothing.py`), и
шаги `scan-type: config` задания trivy в `.github/workflows/security-scan.yml` обязаны
совпадать с ними. Совпадение держит гейт покрытия (`check_workflow_passes`), а не
внимание: шаг, чей срез разошёлся с проходом, — его находка.

ПОЧЕМУ ПРОХОДОВ ДВА. Заглушки ТОЛЬКО ДЛЯ СКАНА (`trivy.yaml`) применяются КО ВСЕМ чартам
прохода: области чарта у `--helm-set` нет. Вендоренный внешний чарт с ЗАКРЫТОЙ схемой
значений (`additionalProperties: false` в `values.schema.json`) отказывает в рендере на
любом незнакомом ему ключе — то есть на любой нашей заглушке, — и сканер пропускает его
молча, с кодом 0. Замер 2026-10-01, trivy 0.70.0, дерево эпика 2914 @706bd9486ca:
`cert-manager-approver-policy-v0.28.0.tgz` (вендорен 007d0adb90b) даёт 9 целей и 8
находок при пустом списке заглушек и НОЛЬ целей при штатных двух; гейт покрытия этого не
видел, потому что архивную форму чарта не знал. Совместить в одном проходе заглушки и
чарт, отвергающий любую заглушку, нельзя by construction — поэтому каталог вендоренных
архивов осматривается своим проходом, без заглушек.

ПУТИ ЦЕЛЕЙ. Trivy пишет `Target` относительно `scan-ref`. Здесь они приводятся к корню
дерева (`normalize`), чтобы гейты судили одно множество. Перечень исключений
(`.trivyignore.yaml`) в CI применяется к выводу trivy КАК ЕСТЬ, то есть для второго
прохода — в форме относительно его корня. Сегодня исключений в каталоге вендоренных нет;
что запись под него обязана быть в этой форме — держится вниманием.
"""
import json
import os
import subprocess
import sys

VENDOR_HOME = "deploy/helm/vendor"
ALWAYS_SKIPPED = (".claude", "**/node_modules")

# (имя, scan-ref, файл настроек, каталоги вне прохода)
PASSES = (
    ("заглушки", ".", "trivy.yaml", ALWAYS_SKIPPED + (VENDOR_HOME,)),
    ("вендоренные", VENDOR_HOME, "trivy-vendored-charts.yaml", ALWAYS_SKIPPED),
)
STUBBED = PASSES[0]


def normalize(ref, target):
    """Цель прохода → путь от корня дерева."""
    return target if ref in (".", "") else ref.rstrip("/") + "/" + target


def run(root, scan_pass, config=None, extra=()):
    """→ разобранный JSON-отчёт прохода. Ignorefile снят: гейты судят сырой вывод.

    `config` подменяет файл настроек прохода (гейт заглушек гоняет проход с
    изменённым списком), срез дерева при этом остаётся тем же.
    """
    _name, ref, own_config, skipped = scan_pass
    env = dict(os.environ)
    env.pop("TRIVY_IGNOREFILE", None)
    cmd = ["trivy", "config", ref, "--config", str(config or own_config),
           "--format", "json", "--quiet", *extra]
    for d in skipped:
        cmd += ["--skip-dirs", d]
    r = subprocess.run(cmd, cwd=root, capture_output=True, text=True, env=env, timeout=900)
    if r.returncode not in (0, 1):
        print("ОТКАЗ: trivy (проход «%s») вышел с кодом %d\n%s"
              % (scan_pass[0], r.returncode, r.stderr[:400]), file=sys.stderr)
        sys.exit(2)
    return json.loads(r.stdout or "{}")


def results(root, scan_pass, config=None, extra=()):
    """→ [(цель от корня, запись Results)] прохода."""
    doc = run(root, scan_pass, config, extra)
    return [(normalize(scan_pass[1], res.get("Target") or ""), res)
            for res in doc.get("Results") or []]


def all_results(root, extra=()):
    """→ (объединение по всем проходам, {имя прохода: число целей})."""
    out, census = [], {}
    for p in PASSES:
        got = results(root, p, extra=extra)
        census[p[0]] = len(got)
        out += got
    return out, census
