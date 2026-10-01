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

АРХИВНАЯ ФОРМА ЧАРТА — ТОЖЕ КАНДИДАТ, И ЧАРТ ЛИ ОН, РЕШАЕТ СКАНЕР. Первая редакция
знала только каталог с Chart.yaml — и вендоренный внешний чарт
`deploy/helm/vendor/cert-manager-approver-policy-v0.28.0.tgz` (007d0adb90b) выпал из
осмотра незамеченным: его схема значений закрыта, и любая заглушка `trivy.yaml` роняет
его рендер. Вторая и третья угадывали «чарт ли архив» сами — по расширению, по глубине
Chart.yaml — и каждая приёмка находила законную форму вне угаданного. Теперь кандидат —
всякий отслеживаемый архив (по содержимому или расширению в любом регистре), и каждый
прогоняется сканером в одиночку, проходом без заглушек: целей больше нуля — чарт, и он
обязан быть покрыт. Архив внутри каталога другого чарта — подчарт и кандидатом не является.

ПРОХОДОВ ДВА, И ОНИ СВЕРЯЮТСЯ С CI. Цели берутся объединением проходов
`iac_scan_passes.PASSES` (заглушки — всё дерево без каталога вендоренных; вендоренные —
этот каталог без заглушек). Шаги `scan-type: config` задания trivy обязаны совпадать с
проходами срезом, и каждый проход обязан иметь гейтовый шаг: иначе гейт судил бы о
покрытии, которого CI не исполняет. Гейтовый — `exit-code: '1'` и при этом ни шаг, ни
задание не стоят под `continue-on-error` (кроме заведомо ложного) и под заведомо ложным
`if`: такой шаг объявлен судящим, а вердикта не выносит. Проход вендоренных самоистекает: каталог без
отслеживаемого архива — находка.

ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ. «Ноль непокрытых чартов» обязано быть отличимо от
«ноль прочитанных чартов».
"""
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import time

import yaml

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import iac_scan_passes  # noqa: E402 — соседний модуль, единственный источник проходов

ROOT = pathlib.Path(__file__).resolve().parents[2]
SCAN_CONFIG = ROOT / "trivy.yaml"
WORKFLOW = ROOT / ".github" / "workflows" / "security-scan.yml"

# Чарты, которым цель не требуется, — с причиной. Причина обязана быть проверяемой
# (см. reason_still_holds), иначе через полгода это будет фольклор.
EXEMPT = {
    "deploy/helm/umbrella/": "не рендерится без сборки ЛОКАЛЬНЫХ сабчартов из "
                             "исходников — они в git не вендорятся by construction",
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
    """→ (множество целей от корня дерева по ВСЕМ проходам, перепись по проходам).

    Проходы — те же, что шаги `scan-type: config` в CI (`iac_scan_passes.PASSES`;
    совпадение держит `check_workflow_passes` ниже). Чарт обязан дать цель хотя бы в
    одном проходе: заглушки вводят в осмотр наши чарты, а вендоренный чарт с
    закрытой схемой значений осматривается проходом без них.
    """
    if not shutil.which("trivy"):
        print("ОТКАЗ: trivy не найден в PATH — судить не о чем", file=sys.stderr)
        sys.exit(2)
    got, census = iac_scan_passes.all_results(ROOT, extra=("--severity", "CRITICAL,HIGH"))
    return {target for target, _ in got}, census


_STATIC_FALSE = re.compile(r"^\s*(\$\{\{\s*)?(false|0|null|''|\"\")(\s*\}\})?\s*$")


def static_false(value):
    """Значение ключа `if`/`continue-on-error`, ложное при ЛЮБОМ прогоне.

    Судится только то, что ложно без вычисления: `false`, `0`, `null`, пустая
    строка — голые или в `${{ }}`. Выражение, зависящее от прогона
    (`!cancelled()`, `matrix.*`), ложным заранее не считается.
    """
    if value is False or value is None or value == 0:
        return True
    return isinstance(value, str) and bool(_STATIC_FALSE.match(value))


def mute_reasons(node):
    """→ причины, по которым шаг или задание заведомо не роняют вердикт.

    Две формы, найденные приёмкой гейта (опыты M1 и M5 на 7b9d560610b): шаг с
    `exit-code: '1'` под `continue-on-error`, отличным от заведомо ложного, —
    красный скан задание не роняет; шаг под `if`, заведомо ложным, — не
    исполняется вовсе.
    """
    why = []
    if "continue-on-error" in node and not static_false(node["continue-on-error"]):
        why.append("continue-on-error: %r — красный скан задание не роняет"
                   % (node["continue-on-error"],))
    if node.get("if") is not None and static_false(node["if"]):
        why.append("if: %r — шаг не исполняется ни при каком прогоне" % (node["if"],))
    return why


# КАНДИДАТ В АРХИВ-ЧАРТ ОТБИРАЕТСЯ ШИРОКО, А ЧАРТОМ ЕГО ПРИЗНАЁТ СКАНЕР. Две редакции
# подряд угадывали «чарт ли это» сами — по расширению, потом по глубине `Chart.yaml`
# внутри, — и обе приёмки нашли законную форму, которую сканер рендерит, а гейт нет
# (`.tar.gz` и `.tar`; затем `wrap/<чарт>/Chart.yaml` и `./<чарт>/…`). Отсюда форма:
# отбор кандидатов не решает ничего, кроме «это архив» — по содержимому ИЛИ по
# расширению в любом регистре, — а решает одиночный прогон сканера на каждом
# кандидате в одноразовом каталоге, проходом без заглушек. Целей больше нуля —
# чарт, и он обязан быть покрыт; ноль — не чарт для сканера, перечисляется отдельно.
ARCHIVE_SUFFIXES = (".tgz", ".tar", ".gz", ".tbz", ".tbz2", ".bz2", ".txz", ".xz",
                    ".zip", ".zst", ".tzst")


def is_archive(path):
    """Архив ли файл — по расширению (любой регистр) ИЛИ по сигнатуре содержимого."""
    if path.name.lower().endswith(ARCHIVE_SUFFIXES):
        return True
    try:
        with open(path, "rb") as fh:
            head = fh.read(263)
    except OSError:
        return False
    return (head[:2] == b"\x1f\x8b"            # gzip
            or head[:3] == b"BZh"               # bzip2
            or head[:6] == b"\xfd7zXZ\x00"      # xz
            or head[:4] == b"PK\x03\x04"         # zip
            or head[:4] == b"\x28\xb5\x2f\xfd"  # zstd
            or head[257:262] == b"ustar")       # tar


def chart_targets_alone(path, bare_pass):
    """→ число целей, которое сканер даёт этому архиву, лежащему В ОДИНОЧКУ.

    Одноразовый каталог, имя файла сохранено (сканер различает формы по имени),
    файл настроек — прохода без заглушек: вопрос «чарт ли это для сканера», а не
    «рендерится ли он с нашими заглушками».
    """
    with tempfile.TemporaryDirectory() as tmp:
        shutil.copyfile(path, pathlib.Path(tmp) / path.name)
        probe = ("одиночный архив", ".", str(ROOT / bare_pass[2]), ())
        return len(iac_scan_passes.results(tmp, probe))


def check_workflow_passes():
    """→ (находки, строка переписи): шаги `scan-type: config` задания trivy против проходов.

    Срез шага — (scan-ref, trivy-config, skip-dirs сверх тех, что гейты снимают
    всегда). Шаг, чей срез не совпал ни с одним проходом, — находка: CI осматривал
    бы не то, о чём судят гейты. Проход без ГЕЙТОВОГО шага (`exit-code: '1'`) — тоже
    находка: срез, о покрытии которого здесь вынесен вердикт, в CI не судился бы.
    """
    if not WORKFLOW.exists():
        print("ОТКАЗ: %s не найден — сверять проходы скана не с чем"
              % WORKFLOW.relative_to(ROOT), file=sys.stderr)
        sys.exit(2)
    doc = yaml.safe_load(WORKFLOW.read_text(encoding="utf-8")) or {}
    job = ((doc.get("jobs") or {}).get("trivy") or {})
    steps = job.get("steps") or []
    want = {(ref, cfg, frozenset(set(skip) - set(iac_scan_passes.ALWAYS_SKIPPED))): name
            for name, ref, cfg, skip in iac_scan_passes.PASSES}
    gated, seen, out = set(), 0, []
    # Задание целиком, которое заведомо не судит, отнимает вердикт у всех своих шагов.
    job_mute = mute_reasons(job)
    for st in steps:
        w = st.get("with") or {}
        if str(w.get("scan-type") or "") != "config":
            continue
        seen += 1
        skip = frozenset(d.strip() for d in str(w.get("skip-dirs") or "").split(",") if d.strip())
        key = (str(w.get("scan-ref") or "."), str(w.get("trivy-config") or ""), skip)
        if key not in want:
            out.append("шаг «%s» (%s) — срез scan-ref=%s, trivy-config=%s, skip-dirs=%s не "
                       "совпадает ни с одним проходом `iac_scan_passes.PASSES`"
                       % (st.get("name") or "?", WORKFLOW.name, key[0], key[1] or "—",
                          ",".join(sorted(skip)) or "—"))
        elif str(w.get("exit-code") or "") == "1":
            why = job_mute + mute_reasons(st)
            if why:
                out.append("шаг «%s» (%s) прохода «%s» объявлен гейтовым, но заведомо не "
                           "судит: %s" % (st.get("name") or "?", WORKFLOW.name, want[key],
                                          "; ".join(why)))
            else:
                gated.add(want[key])
    if not seen:
        out.append("в задании trivy (%s) нет ни одного шага `scan-type: config` — IaC-скан "
                   "в CI не исполняется вовсе" % WORKFLOW.name)
    for name in sorted(set(want.values()) - gated):
        out.append("проход «%s» не судится в CI: у него нет шага с `exit-code: '1'`, "
                   "который исполняется и роняет задание, в %s" % (name, WORKFLOW.name))
    return out, ("  шагов scan-type: config в CI %d; проходов %d; судимых гейтовым шагом %d"
                 % (seen, len(want), len(gated)))


def local_dependency_names(chart_yaml):
    """→ множество имён зависимостей, чей источник ЛОКАЛЬНЫЙ (`file://…`).

    Разбор построчный: читаются пары «- name:» и следующий за ней «repository:»
    того же элемента. Разбор YAML гейт теперь берёт для сверки шагов CI
    (`check_workflow_passes`); перевод этой функции на него — не предмет правки,
    которая его ввела.
    """
    names, cur, in_deps = set(), None, False
    for raw in chart_yaml.read_text(encoding="utf-8").splitlines():
        s = raw.strip()
        if s.startswith("#"):
            continue  # проза шапки объявлением не является
        if raw.startswith("dependencies:"):
            in_deps = True
            continue
        if in_deps and raw and not raw[0].isspace():
            break  # вышли из блока зависимостей
        if not in_deps:
            continue
        if s.startswith("- name:"):
            cur = s.split(":", 1)[1].strip().strip("\"'")
        elif s.startswith("repository:") and cur:
            if s.split(":", 1)[1].strip().strip("\"'").startswith("file://"):
                names.add(cur)
    return names


def archive_chart_name(archive):
    """`vpc-1.0.0.tgz` → `vpc`; `cert-manager-v1.16.5.tgz` → `cert-manager`.

    Имя чарта дефис содержать вправе, версия — нет, поэтому режется ПОСЛЕДНИЙ
    дефис, а не первый. Иначе `cert-manager` читался бы как `cert`, и
    вендоренный внешний архив попал бы в счёт локальных.
    """
    stem = archive[: -len(".tgz")] if archive.endswith(".tgz") else archive
    return stem.rsplit("-", 1)[0] if "-" in stem else stem


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
    local_deps = local_dependency_names(chart_yaml)
    if not local_deps:
        return False, ("чарт больше не объявляет ЛОКАЛЬНЫХ сабчартов — сборка из "
                       "исходников ему не нужна, и рендер обязан удаваться")
    tracked = git_ls(chart_dir + "charts/*.tgz")
    tracked_local = sorted(a for a in tracked
                           if archive_chart_name(a.rsplit("/", 1)[-1]) in local_deps)
    if tracked_local:
        return False, ("локальные сабчарты провендорены В GIT (%d .tgz: %s) — рендер "
                       "обязан удаваться, а вендоренная копия исходника означала бы "
                       "«правка есть в файле, а в стенд не попала»"
                       % (len(tracked_local),
                          ", ".join(a.rsplit("/", 1)[-1] for a in tracked_local)))
    on_disk = [f for f in ((ROOT / chart_dir / "charts").glob("*.tgz")
                           if (ROOT / chart_dir / "charts").is_dir() else [])
               if archive_chart_name(f.name) in local_deps]
    if on_disk:
        return None, ("в рабочем дереве лежат %d .tgz локальных сабчартов, которых git "
                      "не отслеживает, — это местный результат материализации" % len(on_disk))
    return True, ""


def main():
    if not SCAN_CONFIG.exists():
        print("ОТКАЗ: %s не найден — без него часть чартов не рендерится, и гейт\n"
              "       мерил бы не то множество, что скан в CI." % SCAN_CONFIG.name,
              file=sys.stderr)
        return 2

    for _name, _ref, cfg, _skip in iac_scan_passes.PASSES:
        if not (ROOT / cfg).exists():
            print("ОТКАЗ: файл настроек прохода «%s» (%s) не найден — trivy подхватил бы\n"
                  "       корневой trivy.yaml сам, и проход судил бы не тот срез" % (_name, cfg),
                  file=sys.stderr)
            return 2

    charts = sorted({c[: -len("Chart.yaml")] for c in git_ls("*Chart.yaml")})
    if not charts:
        print("ОТКАЗ: в дереве не найдено ни одного Chart.yaml — судить не о чем",
              file=sys.stderr)
        return 2

    templates = git_ls("*/templates/*.yaml")
    # Архивная форма чарта (см. `chart_targets_alone`). Архив внутри каталога
    # другого чарта — его подчарт: сканер относит его к родителю и отдельной целью
    # не заводит (на этом дереве `deploy/helm/umbrella/charts/*.tgz` не дают своих
    # целей ни с заглушками, ни без), поэтому одиночному прогону не подлежит.
    if not shutil.which("trivy"):
        print("ОТКАЗ: trivy не найден в PATH — судить не о чем", file=sys.stderr)
        return 2
    bare_pass = iac_scan_passes.require(iac_scan_passes.BARE_NAME)
    bare_doc = yaml.safe_load((ROOT / bare_pass[2]).read_text(encoding="utf-8")) or {}
    if ((bare_doc.get("misconfiguration") or {}).get("helm") or {}).get("set"):
        print("ОТКАЗ: проход «%s» (%s) несёт заглушки — одиночный прогон архива по нему\n"
              "       отвечал бы «рендерится ли с заглушками», а не «чарт ли это»"
              % (bare_pass[0], bare_pass[2]), file=sys.stderr)
        return 2
    t0 = time.monotonic()
    tracked = git_ls("*")
    candidates = [a for a in tracked if is_archive(ROOT / a)]
    archives, nested_archives, not_charts = [], [], []
    for a in candidates:
        if any(a.startswith(c) for c in charts):
            nested_archives.append(a)
        elif chart_targets_alone(ROOT / a, bare_pass) > 0:
            archives.append(a)
        else:
            not_charts.append(a)
    sniff_s = time.monotonic() - t0
    targets, census = scan_targets()
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

    for a in archives:
        hit = [t for t in targets if t.startswith(a + ":")]
        if hit:
            covered.append((a, len(hit)))
        else:
            uncovered.append(a)

    # Проход вендоренных без предмета самоистекает, как всякое послабление: проход
    # заглушек этот каталог НЕ осматривает, и оправдание тому — только архив в нём.
    home = iac_scan_passes.VENDOR_HOME + "/"
    if not any(a.startswith(home) for a in archives):
        findings.append("%s — в каталоге нет ни одного отслеживаемого архива чарта, а проход "
                        "заглушек его не осматривает: второй проход ничего не открывает, "
                        "и снимать каталог с первого прохода больше незачем"
                        % iac_scan_passes.VENDOR_HOME)
    wf_findings, wf_census = check_workflow_passes()
    findings += wf_findings

    print("iac-chart-coverage: чартов в дереве %d; из них без шаблонов %d, подчартов %d, "
          "послаблений %d (не судимо в этом прогоне %d); архивов-чартов вне чартов %d (подчартов-"
          "архивов %d, архивов не чартов %d); кандидатов %d; целей скана %d (%s); непокрытых %d"
          % (len(charts), len(no_templates), len(nested), len(exempt_ok),
             len(exempt_unjudged), len(archives), len(nested_archives), len(not_charts),
             len(covered) + len(uncovered), len(targets),
             ", ".join("проход «%s» %d" % kv for kv in census.items()), len(uncovered)))
    print(wf_census)
    print("  архивов среди отслеживаемых файлов %d из %d; одиночных прогонов сканера %d; "
          "отбор и прогоны %.1f с" % (len(candidates), len(tracked),
                                      len(archives) + len(not_charts), sniff_s))
    for a in not_charts:
        print("  не чарт для сканера %s — одиночный прогон без заглушек: целей 0" % a)
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
        if d in archives:
            findings.append("%s — архив чарта НЕ ДАЛ сканеру ни одной цели. Если он лежит вне "
                            "%s, его осматривает проход заглушек, а внешний чарт с закрытой "
                            "схемой значений отвергает любую заглушку и выпадает из осмотра "
                            "молча; если внутри — рендер отказал и без заглушек"
                            % (d, iac_scan_passes.VENDOR_HOME))
            continue
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
