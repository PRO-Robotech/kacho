# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Проходы IaC-скана: какой срез дерева каким файлом настроек осматривается.

ЕДИНСТВЕННЫЙ ИСТОЧНИК. Проходы читают три гейта (`assert-iac-scan-covers-every-chart.py`,
`assert-iac-exclusions-still-have-a-subject.py`, `assert-scan-stubs-hide-nothing.py`), и
шаги `scan-type: config` задания trivy в `.github/workflows/security-scan.yml` обязаны
совпадать с ними. Совпадение держит гейт покрытия (`check_workflow_passes`), а не
внимание: шаг, чей срез разошёлся с проходом, — его находка.

ПОЧЕМУ ВЕНДОРЕННЫЕ — ОТДЕЛЬНЫМ ПРОХОДОМ. Заглушки ТОЛЬКО ДЛЯ СКАНА (`trivy.yaml`) применяются КО ВСЕМ чартам
прохода: области чарта у `--helm-set` нет. Вендоренный внешний чарт с ЗАКРЫТОЙ схемой
значений (`additionalProperties: false` в `values.schema.json`) отказывает в рендере на
любом незнакомом ему ключе — то есть на любой нашей заглушке, — и сканер пропускает его
молча, с кодом 0. Замер 2026-10-01, trivy 0.70.0, дерево эпика 2914 @706bd9486ca:
`cert-manager-approver-policy-v0.28.0.tgz` (вендорен 007d0adb90b) даёт 9 целей и 8
находок при пустом списке заглушек и НОЛЬ целей при штатных двух; гейт покрытия этого не
видел, потому что архивную форму чарта не знал. Совместить в одном проходе заглушки и
чарт, отвергающий любую заглушку, нельзя by construction — поэтому каталог вендоренных
архивов осматривается своим проходом, без заглушек.

ТРЕТИЙ ПРОХОД — ПОДЧАРТЫ-АРХИВЫ ЗОНТИКА (#2980). Сторонние подчарты в
`deploy/helm/umbrella/charts/` осматривались только через родителя, а родитель снят с
осмотра послаблением (не рендерится без сборки локальных сабчартов): пять архивов, 44
цели и 15 находок CRITICAL/HIGH не видел ни один проход (замер 2026-10-01, trivy 0.70.0).
Проход без заглушек, с версией Kubernetes узла стенда (`trivy-umbrella-subcharts.yaml`):
без неё cert-manager и ingress-nginx отказывают в рендере требованием `kubeVersion`.
ГРАНИЦА ПРОХОДА: подчарт рендерится со СВОИМИ умолчаниями, а не со значениями зонтика —
у trivy нет области чарта ни для `--helm-set`, ни для `--helm-values`, а `postgresql`
стоит в зонтике пятью псевдонимами с разными значениями. Осмотр отвечает «что несёт
сторонний чарт», а не «что развёрнуто»; это сказано и в шапке файла настроек.

ПУТИ ЦЕЛЕЙ. Trivy пишет `Target` относительно `scan-ref`. Здесь они приводятся к корню
дерева (`normalize`), чтобы гейты покрытия судили одно множество. Перечень исключений
(`.trivyignore.yaml`) в CI применяется к выводу trivy КАК ЕСТЬ — в форме относительно
корня прохода (`cert-manager-v1.16.5.tgz:templates/rbac.yaml`), — и гейт исключений
сверяет записи с ТОЙ ЖЕ формой (`results(..., raw=True)`), а не с приведённой.
"""
import json
import os
import re
import resource
import subprocess
import sys

# ПРЕДЕЛ ПАМЯТИ СКАНЕРА (H4). Архив-чарт — чужие байты: рендер, раздувающий память,
# не должен ронять раннер и чужие задания. Сканер запускается под RLIMIT_AS; превышение
# — код 2 с именем прохода и последнего файла из журнала, а не зависание и не «зелено».
# Замер 2026-10-01, trivy 0.70.0, проход подчартов зонтика: 2048 МиБ — исполняется,
# 512 МиБ — отказ рантайма; умолчание взято с запасом вдвое над рабочим значением.
MEMORY_MIB_ENV = "KACHO_IAC_TRIVY_MEMORY_MIB"
MEMORY_MIB_DEFAULT = 4096
_FILE_PATH = re.compile(r'file_path="([^"]+)"')

VENDOR_HOME = "deploy/helm/vendor"
ALWAYS_SKIPPED = (".claude", "**/node_modules")

# Проходы называются по ИМЕНИ, а не по позиции в перечне: потребитель, взявший
# «второй элемент», после перестановки молча судил бы чужим срезом.
STUBBED_NAME = "заглушки"
BARE_NAME = "вендоренные"  # проход БЕЗ заглушек
RENDERED_NAME = "профили зонтика"  # отрендеренные профили, как они развёртываются
UMBRELLA = "deploy/helm/umbrella"
UMBRELLA_CHARTS = UMBRELLA + "/charts"
# scan-ref прохода профилей — каталог ВНЕ checkout'а, куда шаг конвейера кладёт рендер
# (`deploy/scripts/render-umbrella-profiles.sh`). Внутри дерева его осмотрел бы и проход
# заглушек. Гейты рендерят в свой временный каталог тем же скриптом (`rendered_dir`).
RENDERED_REF = "${{ runner.temp }}/iac-rendered"
RENDER_SCRIPT = "deploy/scripts/render-umbrella-profiles.sh"

# (имя, scan-ref, файл настроек, каталоги вне прохода)
PASSES = (
    # Зонтик целиком — вне прохода заглушек: он осматривается отрендеренными
    # профилями, а с материализованными зависимостями отрендерился бы и здесь — с
    # заглушками и базовыми значениями, то есть не тем, что развёртывается.
    (STUBBED_NAME, ".", "trivy.yaml", ALWAYS_SKIPPED + (VENDOR_HOME, UMBRELLA)),
    (BARE_NAME, VENDOR_HOME, "trivy-vendored-charts.yaml", ALWAYS_SKIPPED),
    (RENDERED_NAME, RENDERED_REF, "trivy-rendered-profiles.yaml", ()),
)
# Проходы, чей журнал судится: строка ERROR в нём — находка.
LOG_JUDGED = (BARE_NAME, RENDERED_NAME)

_RENDERED = {}
# Каталог релиза вне зонтика → архив чарта в дереве (`releases.tsv` рендера).
_RELEASES = {}


def rendered_dir(root):
    """→ каталог отрендеренных профилей (рендер один раз на процесс). Отказ рендера —
    код 2: вердикта о профилях нет."""
    key = str(root)
    if key not in _RENDERED:
        import atexit
        import shutil
        import tempfile
        out = tempfile.mkdtemp(prefix="iac-rendered-")
        atexit.register(shutil.rmtree, out, True)
        r = subprocess.run(["bash", os.path.join(str(root), RENDER_SCRIPT), out],
                           capture_output=True, text=True, timeout=900)
        if r.returncode != 0:
            print("ОТКАЗ: профили зонтика не отрендерены (код %d):\n%s"
                  % (r.returncode, r.stderr[-800:]), file=sys.stderr)
            sys.exit(2)
        _RENDERED[key] = out
        with open(os.path.join(out, "releases.tsv"), encoding="utf-8") as fh:
            for line in fh:
                if "\t" in line:
                    k, v = line.rstrip("\n").split("\t", 1)
                    _RELEASES[k] = v
    return _RENDERED[key]


def rendered_files(root):
    """→ отрендеренные файлы в форме цели прохода профилей (`<стек>/<чарт>/…`)."""
    base = rendered_dir(root)
    out = []
    for d, _dirs, files in os.walk(base):
        for f in files:
            rel = os.path.relpath(os.path.join(d, f), base).replace(os.sep, "/")
            if "/" in rel:  # служебные `stacks.tsv`/`releases.tsv` лежат в корне
                out.append(rel)
    return sorted(out)


def require(name):
    """→ проход по имени; его нет — ОТКАЗ (код 2) с именем, а не исключение."""
    for p in PASSES:
        if p[0] == name:
            return p
    print("ОТКАЗ: прохода «%s» нет в `iac_scan_passes.PASSES` (есть: %s) — судить "
          "не о чем: гейт опирается на него поимённо"
          % (name, ", ".join(p[0] for p in PASSES) or "—"), file=sys.stderr)
    sys.exit(2)


def normalize(ref, target):
    """Цель прохода → путь от корня дерева. Цель профиля (`<стек>/<чарт>/<путь>`)
    приводится к пути в зонтике: `deploy/helm/umbrella/<путь>`; цель релиза вне зонтика
    (`release-<имя>/<чарт>/<путь>`) — к архиву чарта в дереве: `<архив>:<путь>`."""
    if ref == RENDERED_REF:
        parts = target.split("/", 2)
        if len(parts) == 3 and parts[0] in _RELEASES:
            return _RELEASES[parts[0]] + ":" + parts[2]
        return UMBRELLA + "/" + parts[2] if len(parts) == 3 else UMBRELLA + "/" + target
    return target if ref in (".", "") else ref.rstrip("/") + "/" + target


def run(root, scan_pass, config=None, extra=(), errors=None):
    """→ разобранный JSON-отчёт прохода. Ignorefile снят: гейты судят сырой вывод.

    `config` подменяет файл настроек прохода (гейт заглушек гоняет проход с
    изменённым списком), срез дерева при этом остаётся тем же.

    `errors` — список, куда складываются строки журнала уровня ERROR. Тогда прогон
    идёт БЕЗ `--quiet`: отказ рендера чарта trivy печатает только в журнал
    («[helm scanner] Failed to render Chart files»), выходя кодом 0 и не заводя ни
    одной цели, — с `--quiet` он неотличим от «чарта здесь нет». Замер приёмки
    8735f0eb7c7: архив с `{{ required … }}` гейт объявил «не чартом».
    """
    _name, ref, own_config, skipped = scan_pass
    env = dict(os.environ)
    env.pop("TRIVY_IGNOREFILE", None)
    # Журнал читается ВСЕГДА (без `--quiet`): в нём имя файла, на котором сканер
    # отказал, — без него предел памяти назвал бы проход, но не архив.
    if ref == RENDERED_REF:
        ref = rendered_dir(root)
        own_config = os.path.join(str(root), own_config)
    cmd = ["trivy", "config", ref, "--config", str(config or own_config),
           "--format", "json", "--skip-version-check", *extra]
    for d in skipped:
        cmd += ["--skip-dirs", d]
    try:
        mib = int(os.environ.get(MEMORY_MIB_ENV) or MEMORY_MIB_DEFAULT)
    except ValueError:
        print("ОТКАЗ: %s=%r — не число МиБ" % (MEMORY_MIB_ENV, os.environ.get(MEMORY_MIB_ENV)),
              file=sys.stderr)
        sys.exit(2)

    def limit():
        resource.setrlimit(resource.RLIMIT_AS, (mib << 20, mib << 20))

    r = subprocess.run(cmd, cwd=root, capture_output=True, text=True, env=env, timeout=900,
                       preexec_fn=limit)
    if r.returncode not in (0, 1):
        seen = _FILE_PATH.findall(r.stderr)
        print("ОТКАЗ: trivy (проход «%s») вышел с кодом %d при пределе памяти %d МиБ (%s); "
              "последний файл в журнале: %s\n%s"
              % (scan_pass[0], r.returncode, mib, MEMORY_MIB_ENV,
                 seen[-1] if seen else "журнал файла не назвал", r.stderr[-400:]),
              file=sys.stderr)
        sys.exit(2)
    if errors is not None:
        errors += [line.strip() for line in r.stderr.splitlines()
                   if "\tERROR\t" in line or "\tFATAL\t" in line]
    return json.loads(r.stdout or "{}")


def results(root, scan_pass, config=None, extra=(), errors=None, raw=False):
    """→ [(цель, запись Results)] прохода; цель — от корня дерева либо, при `raw`,
    как её пишет trivy (относительно scan-ref: форма, к которой CI применяет
    перечень исключений)."""
    doc = run(root, scan_pass, config, extra, errors)
    return [((res.get("Target") or "") if raw
             else normalize(scan_pass[1], res.get("Target") or ""), res)
            for res in doc.get("Results") or []]


def all_results(root, extra=(), errors=None, raw=False):
    """→ (объединение по всем проходам, {имя прохода: число целей}).

    `errors` — словарь {имя прохода: [строки ERROR журнала]}; задан — прогоны идут
    без `--quiet` (см. `run`).
    """
    out, census = [], {}
    for p in PASSES:
        log = None
        if errors is not None:
            log = errors.setdefault(p[0], [])
        got = results(root, p, extra=extra, errors=log, raw=raw)
        census[p[0]] = len(got)
        out += got
    return out, census
