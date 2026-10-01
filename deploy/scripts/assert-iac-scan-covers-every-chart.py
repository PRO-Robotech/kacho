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

АРХИВНАЯ ФОРМА ЧАРТА — ТОЖЕ КАНДИДАТ, И РАСПОЗНАЁТСЯ ОНА ПО СОДЕРЖИМОМУ. Первая
редакция знала только каталог с Chart.yaml — и вендоренный внешний чарт
`deploy/helm/vendor/cert-manager-approver-policy-v0.28.0.tgz` (007d0adb90b) выпал из
осмотра незамеченным: его схема значений закрыта, и любая заглушка `trivy.yaml` роняет
его рендер. Следующие три решали «чарт ли архив» по расширению, по глубине Chart.yaml и
по одиночному прогону сканера, и каждая приёмка находила законную форму вне решения;
последняя путала «не чарт» с «не отрендерился». Теперь: архив-чарт — тот, в чьём
оглавлении есть Chart.yaml на любой глубине; его место — `deploy/helm/vendor` (или
`charts/` родителя-каталога), вне — находка; в каталоге вендоренных он обязан дать цели
проходу без заглушек, а ERROR в журнале этого прохода — находка с текстом. Разбор —
комментарий над `ARCHIVE_SUFFIXES`.

ПРОХОДЫ СВЕРЯЮТСЯ С CI. Цели берутся объединением проходов `iac_scan_passes.PASSES`
(заглушки — всё дерево без каталога вендоренных; вендоренные — этот каталог без
заглушек; подчарты зонтика — `deploy/helm/umbrella/charts` без заглушек, с версией
Kubernetes). Шаги `scan-type: config` задания trivy обязаны совпадать с проходами
срезом, и каждый проход обязан иметь гейтовый шаг: `exit-code: '1'`, не под
`continue-on-error`, не под заведомо ложным `if` и не снимаемый отказом предыдущего
шага. Проходы без заглушек самоистекают: срез без отслеживаемого архива — находка.

ПОДЧАРТ ОСМОТРЕН ЧЕРЕЗ РОДИТЕЛЯ, ТОЛЬКО ЕСЛИ РОДИТЕЛЬ ОСМОТРЕН (#2980). У зонтика
послабление, и его подчарты — каталоги и архивы — обязаны дать цели сами. Подчарт,
который со своими умолчаниями не рендерит ничего, признаётся таким по предикату
`renders_nothing` (один для каталога и архива), а не по записи с именем.

ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ. «Ноль непокрытых чартов» обязано быть отличимо от
«ноль прочитанных чартов».
"""
import bz2
import gzip
import lzma
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import time
import zipfile

import yaml

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import gha_expr  # noqa: E402 — разбор выражений `if:` GitHub Actions
import iac_scan_passes  # noqa: E402 — соседний модуль, единственный источник проходов

ROOT = pathlib.Path(__file__).resolve().parents[2]
SCAN_CONFIG = ROOT / "trivy.yaml"
WORKFLOW = ROOT / ".github" / "workflows" / "security-scan.yml"

# Чарты, которым цель не требуется, — с причиной. Причина обязана быть проверяемой
# (см. reason_still_holds), иначе через полгода это будет фольклор.
#
# Здесь стояло послабление зонтика («не рендерится без сборки локальных сабчартов»).
# Предмет снят: зонтик осматривается проходом отрендеренных профилей, который сам
# материализует зависимости (`deploy/scripts/render-umbrella-profiles.sh`). Механизм
# остаётся ради следующей записи и судит её в обе стороны (`reason_still_holds`).
EXEMPT = {}


def git_ls(pattern):
    r = subprocess.run(["git", "ls-files", pattern], cwd=ROOT,
                       capture_output=True, text=True, timeout=120)
    if r.returncode != 0:
        print("ОТКАЗ: git ls-files %s вышел с кодом %d: %s"
              % (pattern, r.returncode, r.stderr[:300]), file=sys.stderr)
        sys.exit(2)
    return [line for line in r.stdout.splitlines() if line.strip()]


def scan_targets(errors):
    """→ (множество целей от корня дерева по ВСЕМ проходам, перепись по проходам).

    Проходы — те же, что шаги `scan-type: config` в CI (`iac_scan_passes.PASSES`;
    совпадение держит `check_workflow_passes` ниже). Чарт обязан дать цель хотя бы в
    одном проходе: заглушки вводят в осмотр наши чарты, а вендоренный чарт с
    закрытой схемой значений осматривается проходом без них.
    """
    if not shutil.which("trivy"):
        print("ОТКАЗ: trivy не найден в PATH — судить не о чем", file=sys.stderr)
        sys.exit(2)
    got, census = iac_scan_passes.all_results(ROOT, extra=("--severity", "CRITICAL,HIGH"),
                                              errors=errors)
    return {target for target, _ in got}, census


def mute_reasons(node):
    """→ причины, по которым шаг или задание заведомо не роняют вердикт.

    Две формы, найденные приёмкой (опыты M1 и M5 на 7b9d560610b): `continue-on-error`,
    который может оказаться истинным, — красный скан задание не роняет; `if`, ложный
    во всех сценариях прогона, — шаг не исполняется вовсе. Вторая редакция узнавала
    ложное только ЦЕЛИКОМ (`false`, `${{ false }}`) и пропускала составное
    `always() && false` (H5). Теперь оба ключа вычисляются разбором (`gha_expr`):
    `continue-on-error`, не заведомо ложный, — находка; `if`, заведомо ложный во всех
    сценариях, — находка; неразборное выражение — находка.
    """
    why = []
    if "continue-on-error" in node:
        coe = node["continue-on-error"]
        try:
            v = gha_expr.value_truth(coe)
        except gha_expr.ParseError as err:
            v = "не разобран (%s)" % err
        if v is not False:
            why.append("continue-on-error: %r — %s" % (
                coe, "красный скан задание не роняет" if v is True else
                "может оказаться истинным (%s)" % v))
    never = gha_expr.never_runs(node.get("if"))
    if never:
        why.append("if: %r — шаг не исполняется ни при каком прогоне (%s)"
                   % (node.get("if"), never))
    return why


# АРХИВ-ЧАРТ РАСПОЗНАЁТСЯ ПО СОДЕРЖИМОМУ, А НЕ ПО ИСХОДУ РЕНДЕРА. Четыре редакции
# подряд гейт решал «чарт ли архив» сам — по расширению, по глубине Chart.yaml, по
# одиночному прогону сканера — и каждая приёмка находила законную форму вне решения.
# Последняя (одиночный прогон) путала «не чарт» с «чарт не отрендерился»: trivy на
# `{{ required … }}` выходит кодом 0 без целей и пишет отказ только в журнал.
# Отсюда форма, закрытая на отказ:
#   * кандидат — всякий отслеживаемый архив по расширению в любом регистре ИЛИ по
#     сигнатуре; ЧАРТ — если в оглавлении (tar любого сжатия, zip) есть Chart.yaml
#     на ЛЮБОЙ глубине; оглавление читается по содержимому, имя не решает ничего;
#     непрочитанный архив — находка: чарт ли он, не установить;
#   * МЕСТО архива-чарта — каталог вендоренных (`iac_scan_passes.VENDOR_HOME`) или
#     `charts/` его родителя-каталога; вне их — находка, в том числе для форм,
#     которых сканер не рендерит вовсе (`.zip`, `.TGZ`, gzip без расширения);
#   * в каталоге вендоренных каждый архив-чарт обязан дать цели проходу без
#     заглушек, а строка ERROR в журнале этого прохода — находка с её текстом.
# Расширения — только ОТБОР кандидатов (вместе с сигнатурой); решает оглавление.
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


# ПРЕДЕЛЫ ЧТЕНИЯ АРХИВА (H4). Архив — чужие байты; гейт не распаковывает его без
# предела: число членов, размер члена и сумма размеров по заголовкам проверяются ДО
# того, как чтение пройдёт дальше заголовка. Превышение — код 2 с именем архива: о
# чарте, которого гейт не дочитал, вердикта нет. Пределы с запасом над деревом:
# крупнейший отслеживаемый архив-чарт — десятки КиБ, сотни членов.
MAX_ARCHIVE_MEMBERS = 20000
MAX_MEMBER_BYTES = 32 << 20
MAX_ARCHIVE_BYTES = 256 << 20


class LimitExceeded(Exception):
    pass


def _bounded_tar_members(t):
    out, total = [], 0
    for m in t:
        if len(out) >= MAX_ARCHIVE_MEMBERS:
            raise LimitExceeded("членов больше %d" % MAX_ARCHIVE_MEMBERS)
        if m.size > MAX_MEMBER_BYTES:
            raise LimitExceeded("член «%s» — %d байт при пределе %d" % (m.name, m.size,
                                                                         MAX_MEMBER_BYTES))
        total += m.size
        if total > MAX_ARCHIVE_BYTES:
            raise LimitExceeded("сумма размеров членов больше %d байт" % MAX_ARCHIVE_BYTES)
        out.append(m)
    return out


def archive_toc(path):
    """→ ("архив", [имена]) | ("поток", None) | ("не прочитан", причина).

    По содержимому: zip, затем tar любого сжатия; не вышло — одиночный сжатый поток
    (gzip/bzip2/xz поверх НЕ tar): оглавления у него нет, и чартом он не бывает.
    Всё прочее — «не прочитан», и это находка, а не «не чарт».
    """
    try:
        if zipfile.is_zipfile(path):
            with zipfile.ZipFile(path) as z:
                infos = z.infolist()
                if len(infos) > MAX_ARCHIVE_MEMBERS:
                    raise LimitExceeded("членов больше %d" % MAX_ARCHIVE_MEMBERS)
                if sum(i.file_size for i in infos) > MAX_ARCHIVE_BYTES:
                    raise LimitExceeded("сумма размеров членов больше %d байт"
                                        % MAX_ARCHIVE_BYTES)
                return "архив", [i.filename for i in infos]
    except (zipfile.BadZipFile, OSError) as err:
        return "не прочитан", "zip: %s" % err
    try:
        with tarfile.open(path, "r:*") as t:
            return "архив", [m.name for m in _bounded_tar_members(t)]
    except tarfile.ReadError as err:
        tar_err = err
    except (OSError, EOFError, tarfile.TarError) as err:
        return "не прочитан", "tar: %s" % err
    for opener in (gzip.open, bz2.open, lzma.open):
        try:
            with opener(path) as fh:
                read = 0
                while True:
                    chunk = fh.read(1 << 20)
                    if not chunk:
                        break
                    read += len(chunk)
                    if read > MAX_ARCHIVE_BYTES:
                        raise LimitExceeded("сжатый поток раскрывается больше чем в %d байт"
                                            % MAX_ARCHIVE_BYTES)
            return "поток", None
        except (OSError, EOFError, lzma.LZMAError):
            continue
    return "не прочитан", "ни zip, ни tar, ни сжатый поток (%s)" % tar_err


def is_chart_toc(names):
    """Chart.yaml на ЛЮБОЙ глубине оглавления (`x/Chart.yaml`, `./x/…`, `wrap/x/…`)."""
    return any(n.rstrip("/").rsplit("/", 1)[-1] == "Chart.yaml" for n in names)


_GUARD = re.compile(r"^\{\{-?\s*if\s+\.Values\.([A-Za-z0-9_.]+)\s*-?\}\}\s*$")
_TEMPLATE_SUFFIXES = (".yaml", ".yml", ".tpl", ".txt", ".json")


def chart_sources(path):
    """→ {путь от корня чарта: текст} для Chart.yaml, values.yaml и templates/** —
    ОДНИМ разбором для обеих форм чарта: каталога и архива. Корень архива — каталог
    самого мелкого Chart.yaml в оглавлении. None — источники не прочитаны."""
    out = {}
    if path.is_dir():
        for f in path.rglob("*"):
            rel = f.relative_to(path).as_posix()
            if f.is_file() and (rel in ("Chart.yaml", "values.yaml")
                                or rel.startswith("templates/")):
                out[rel] = f.read_text(encoding="utf-8", errors="replace")
        return out
    try:
        with tarfile.open(path, "r:*") as t:
            members = [m for m in _bounded_tar_members(t) if m.isfile()]
            roots = sorted((m.name.rsplit("/", 1)[0] if "/" in m.name else ""
                            for m in members if m.name.rstrip("/").rsplit("/", 1)[-1]
                            == "Chart.yaml"), key=lambda r: r.count("/"))
            if not roots:
                return None
            root = roots[0] + "/" if roots[0] else ""
            for m in members:
                if not m.name.startswith(root):
                    continue
                rel = m.name[len(root):]
                if rel in ("Chart.yaml", "values.yaml") or rel.startswith("templates/"):
                    out[rel] = t.extractfile(m).read().decode("utf-8", "replace")
    except (tarfile.TarError, OSError, EOFError):
        return None
    return out


def renders_nothing(src):
    """→ причина либо None: чарт по своим умолчаниям не рендерит ни одного манифеста.

    Один предикат для каталога и архива. Два законных вида: (1) у чарта нет шаблонов
    манифестов (только `_*.tpl`-помощники) либо он `type: library`; (2) КАЖДЫЙ шаблон
    целиком под `{{- if .Values.<ключ> }}`, и каждый такой ключ в values.yaml чарта —
    ложь. Второй вид самоистекает: включили ключ — чарт обязан дать цели. Иная форма
    условия не признаётся: лучше находка, чем угаданное «выключено».
    """
    if src is None:
        return None
    chart = yaml.safe_load(src.get("Chart.yaml") or "") or {}
    if str(chart.get("type") or "") == "library":
        return "библиотека (type: library) — манифестов не рендерит по определению"
    manifests = [k for k in src if k.startswith("templates/") and k.endswith(_TEMPLATE_SUFFIXES)
                 and not k.rsplit("/", 1)[-1].startswith("_") and k.endswith((".yaml", ".yml"))]
    if not manifests:
        return "шаблонов манифестов нет — осматривать нечего"
    values = yaml.safe_load(src.get("values.yaml") or "") or {}
    keys = set()
    for k in manifests:
        lines = [ln.strip() for ln in src[k].splitlines()]
        meaningful = [ln for ln in lines if ln and not ln.startswith("#")
                      and not ln.startswith("{{/*") and not ln.startswith("{{- /*")]
        m = _GUARD.match(meaningful[0]) if meaningful else None
        if not m:
            return None
        keys.add(m.group(1))
    for key in sorted(keys):
        v = values
        for part in key.split("."):
            v = v.get(part) if isinstance(v, dict) else None
        if v is not False:
            return None
    return ("каждый шаблон под условием, ложным в своих умолчаниях: %s"
            % ", ".join(".Values." + k for k in sorted(keys)))


def falls_with_predecessor(step):
    """→ причина либо None: шаг снимается отказом предыдущего шага (или не исполняется
    при успехе).

    Отказ выгрузки SARIF на волне #2977 (run 36867890066) снял следующий за ней
    гейтовый шаг fs — гейт на той ревизии не судил ничего. Вторая редакция судила по
    подстроке (`always()`/`!cancelled()` в тексте) и признавала выжившим
    `!cancelled() && steps.x.outcome == 'success'` (F2). Теперь выражение РАЗБИРАЕТСЯ
    (`gha_expr`) и вычисляется в двух сценариях — все предыдущие успешны и предыдущий
    отказал; выживший — тот, что заведомо исполняется в обоих. Неизвестное (контекст
    прогона) и неразборное — находка: лучше потребовать явной формы, чем угадать.
    """
    ok, why = gha_expr.survives_failure(step.get("if"))
    if ok:
        return None
    return ("if: %s — %s; шаг обязан исполняться при любом исходе предыдущих шагов "
            "(`if: '!cancelled()'`)" % (repr(step.get("if")) if step.get("if") is not None
                                         else "не задан", why))


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
    gated, seen, gate_steps, out = set(), 0, 0, []
    fs_gated = 0
    # Задание целиком, которое заведомо не судит, отнимает вердикт у всех своих шагов.
    job_mute = mute_reasons(job)
    for st in steps:
        w = st.get("with") or {}
        # Любой гейтовый шаг задания — не только IaC: шаг fs снимается упавшей
        # выгрузкой ровно так же, и вердикта по дереву тогда нет ни одного.
        if str(w.get("exit-code") or "") == "1":
            gate_steps += 1
            why = job_mute + mute_reasons(st)
            fall = falls_with_predecessor(st)
            if fall:
                why.append(fall)
            st["__mute"] = why
            if not why and str(w.get("scan-type") or "") == "fs":
                fs_gated += 1
            if why and str(w.get("scan-type") or "") != "config":
                out.append("шаг «%s» (%s) объявлен гейтовым, но заведомо не судит: %s"
                           % (st.get("name") or "?", WORKFLOW.name, "; ".join(why)))
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
            why = st["__mute"]
            if why:
                out.append("шаг «%s» (%s) прохода «%s» объявлен гейтовым, но заведомо не "
                           "судит: %s" % (st.get("name") or "?", WORKFLOW.name, want[key],
                                          "; ".join(why)))
            else:
                gated.add(want[key])
    if not seen:
        out.append("в задании trivy (%s) нет ни одного шага `scan-type: config` — IaC-скан "
                   "в CI не исполняется вовсе" % WORKFLOW.name)
    # Гейт fs держится здесь же (F3): его исполнимость судилась, а СУЩЕСТВОВАНИЕ — нет,
    # и снятый целиком шаг давал ровно то, что упавшая выгрузка на волне #2977, — ни
    # одного вердикта fs, без единой находки.
    # ПОСЛЕ ПЕРВОЙ ВЫГРУЗКИ SARIF КАЖДЫЙ ШАГ ЗАДАНИЯ ИСПОЛНЯЕТСЯ ПРИ ЛЮБОМ ИСХОДЕ (F4, F5).
    # Выгрузка — внешний сервис с собственными отказами (волна #2977); шаг после неё без
    # `!cancelled()` снимается её отказом, и это касается не только гейтов: шаги
    # подготовки (интерпретатор, модули), без которых гейты ниже не исполнятся, и
    # гейт gosec «fail on level=error» снимались ровно так же. Судятся все задания
    # этого файла; гейтовые шаги trivy уже названы выше и здесь не повторяются.
    after_upload = 0
    for jname, jdef in (doc.get("jobs") or {}).items():
        seen_upload = False
        for st in (jdef or {}).get("steps") or []:
            w = st.get("with") or {}
            if seen_upload and not (jname == "trivy" and str(w.get("exit-code") or "") == "1"):
                after_upload += 1
                fall = falls_with_predecessor(st)
                if fall:
                    out.append("задание «%s», шаг «%s» стоит после выгрузки SARIF и снимается "
                               "её отказом: %s" % (jname, st.get("name") or st.get("uses") or "?",
                                                   fall))
            if "upload-sarif" in str(st.get("uses") or ""):
                seen_upload = True
    if not fs_gated:
        out.append("в задании trivy (%s) нет исполняемого гейтового шага `scan-type: fs` "
                   "(`exit-code: '1'`, исполняется при любом исходе предыдущих) — дерево "
                   "не судится сканером зависимостей вовсе" % WORKFLOW.name)
    for name in sorted(set(want.values()) - gated):
        out.append("проход «%s» не судится в CI: у него нет шага с `exit-code: '1'`, "
                   "который исполняется и роняет задание, в %s" % (name, WORKFLOW.name))
    return out, ("  шагов scan-type: config в CI %d; гейтовых шагов задания %d (из них fs %d); "
                 "проходов %d; судимых гейтовым шагом %d; шагов после выгрузки SARIF судимо %d"
                 % (seen, gate_steps, fs_gated, len(want), len(gated), after_upload))


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


def dependency_aliases(parent_dir, name):
    """→ псевдонимы зависимости `name` в Chart.yaml родителя (пусто — без псевдонимов)."""
    try:
        doc = yaml.safe_load((parent_dir / "Chart.yaml").read_text(encoding="utf-8")) or {}
    except OSError:
        return set()
    return {str(d.get("alias")) for d in doc.get("dependencies") or []
            if isinstance(d, dict) and d.get("name") == name and d.get("alias")}


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

    if not shutil.which("trivy"):
        print("ОТКАЗ: trivy не найден в PATH — судить не о чем", file=sys.stderr)
        return 2
    bare_pass = iac_scan_passes.require(iac_scan_passes.BARE_NAME)
    ren_pass = iac_scan_passes.require(iac_scan_passes.RENDERED_NAME)
    for p in (bare_pass, ren_pass):
        doc = yaml.safe_load((ROOT / p[2]).read_text(encoding="utf-8")) or {}
        if ((doc.get("misconfiguration") or {}).get("helm") or {}).get("set"):
            print("ОТКАЗ: проход «%s» (%s) несёт заглушки — его срез существует ровно\n"
                  "       ради прохода без них" % (p[0], p[2]), file=sys.stderr)
            return 2
    t0 = time.monotonic()
    tracked = git_ls("*")
    candidates = [a for a in tracked if is_archive(ROOT / a)]
    home = iac_scan_passes.VENDOR_HOME + "/"
    archives, nested_archives, not_charts, misplaced, unreadable = [], [], [], [], []
    over_limit = []
    for a in candidates:
        try:
            kind, toc = archive_toc(ROOT / a)
        except LimitExceeded as err:
            over_limit.append((a, str(err)))
            continue
        if kind == "не прочитан":
            unreadable.append((a, toc))
        elif kind == "поток" or not is_chart_toc(toc):
            not_charts.append((a, kind))
        elif any(a.startswith(c + "charts/") for c in charts):
            nested_archives.append(a)
        elif a.startswith(home):
            archives.append(a)
        else:
            misplaced.append(a)
    sniff_s = time.monotonic() - t0
    if over_limit:
        for a, why in over_limit:
            print("ОТКАЗ: архив %s не прочитан — превышен предел: %s. Вердикта о нём нет, и\n"
                  "       гейт его не выносит; разберите архив руками или поднимите предел\n"
                  "       отдельным решением с замером" % (a, why), file=sys.stderr)
        return 2
    scan_log = {}
    targets, census = scan_targets(scan_log)
    if not targets:
        print("ОТКАЗ: скан не дошёл НИ ДО ОДНОЙ цели — сканер не отработал",
              file=sys.stderr)
        return 2

    exempt_ok, covered, uncovered, findings = [], [], [], []
    exempt_unjudged = []
    renders_none = []

    def own_hits(d):
        return [t for t in targets if t.startswith(d) and not t.startswith(d + "charts/")]

    # Каталог и архив судятся ОДНИМ предикатом (H7). Прежде каталог без шаблонов
    # узнавался по индексу git (`*/templates/*.yaml`), а архив — по `renders_nothing`:
    # библиотека-каталог молчала, та же библиотека архивом краснела. Теперь обе формы:
    # есть свои цели (своим проходом или через рендер родителя — путь чарта в целях
    # один) — осмотрен; не рендерит ничего по своим умолчаниям — названо; иначе находка.
    for d in charts:
        hit = own_hits(d)

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
        if hit:
            covered.append((d, len(hit)))
            continue
        why = renders_nothing(chart_sources(ROOT / d))
        if why:
            renders_none.append((d, why))
        else:
            uncovered.append(d)

    for a in archives:
        hit = [t for t in targets if t.startswith(a + ":")]
        why = None if hit else renders_nothing(chart_sources(ROOT / a))
        if hit:
            covered.append((a, len(hit)))
        elif why:
            renders_none.append((a, why))
        else:
            uncovered.append(a)

    # Подчарт-архив: осмотрен через родителя, только если родитель осмотрен; иначе
    # обязан дать цели сам. Прежняя редакция считала его «подчартом» и молчала — пять
    # сторонних архивов зонтика (44 цели, 15 находок CRITICAL/HIGH) не видел никто.
    # Через родителя подчарт осмотрен, только если среди целей есть ЕГО шаблоны:
    # trivy, отрендерив родителя, пишет их как `<родитель>charts/<имя подчарта>/…`.
    # Прежняя редакция судила по «у родителя есть цели», и подчарт под родителем без
    # шаблонов (H2) либо выключенный условием родителя оставался без суда.
    # Имя подчарта в рендере — ИМЯ ЧАРТА либо его ПСЕВДОНИМ в зависимостях родителя:
    # один архив `postgresql` развёртывается зонтиком пятью псевдонимами `pg-*`.
    # Подчарт зонтика, которого нет НИ В ОДНОМ отрендеренном профиле, не развёртывается
    # никем (условие зависимости ложно во всех стеках) — он назван, а не судится;
    # включит его профиль — он обязан дать цели.
    rendered_umbrella_subdirs = set()
    for f in iac_scan_passes.rendered_files(ROOT):
        n = iac_scan_passes.normalize(iac_scan_passes.RENDERED_REF, f)
        head = iac_scan_passes.UMBRELLA_CHARTS + "/"
        if n.startswith(head):
            rendered_umbrella_subdirs.add(n[len(head):].split("/", 1)[0])
    sub_archives_via_parent, not_deployed = [], []
    for a in nested_archives:
        parent = max((c for c in charts if a.startswith(c + "charts/")), key=len)
        src = chart_sources(ROOT / a)
        sub_name = str((yaml.safe_load((src or {}).get("Chart.yaml") or "") or {}).get("name") or "")
        names = {sub_name} | dependency_aliases(ROOT / parent, sub_name) if sub_name else set()
        own = [t for t in targets if t.startswith(a + ":")]
        via = [t for t in targets for n in names if t.startswith(parent + "charts/" + n + "/")]
        why = None if own or via else renders_nothing(src)
        if own:
            covered.append((a, len(own)))
        elif via:
            sub_archives_via_parent.append((a, len(via)))
        elif why:
            renders_none.append((a, why))
        elif parent.rstrip("/") == iac_scan_passes.UMBRELLA and not (names & rendered_umbrella_subdirs):
            not_deployed.append((a, sorted(names)))
        else:
            uncovered.append(a)

    for a in misplaced:
        findings.append("%s — архив-чарт вне %s: проход заглушек отвергается внешним чартом "
                        "с закрытой схемой значений, а формы, которых сканер не рендерит "
                        "(.zip, .TGZ, gzip без расширения), не осматриваются нигде. Место "
                        "архива-чарта — каталог вендоренных, в форме .tgz"
                        % (a, iac_scan_passes.VENDOR_HOME))
    for a, why in unreadable:
        findings.append("%s — архив не прочитан (%s): чарт ли он, не установить, и молчать о "
                        "нём гейт не вправе" % (a, why))
    # Строка ERROR о файле, которого git не отслеживает, — местный результат сборки
    # зависимостей (`charts/*.tgz` локальных сабчартов, git-ignored): CI берёт свежий
    # checkout, где этого файла нет, и вердикт о нём отсюда не выносится — он печатается
    # как несудимый, а не роняет гейт у разработчика и не молчит.
    tracked_set = set(tracked)
    log_unjudged = []
    for name in iac_scan_passes.LOG_JUDGED:
        ref = iac_scan_passes.require(name)[1]
        for line in scan_log.get(name, []):
            m = re.search(r'file_path="([^"]+)"', line)
            if m and ref != iac_scan_passes.RENDERED_REF and \
                    iac_scan_passes.normalize(ref, m.group(1)) not in tracked_set:
                log_unjudged.append((name, iac_scan_passes.normalize(ref, m.group(1))))
                continue
            findings.append("проход «%s»: в журнале trivy ERROR — %s" % (name, line))

    # Проход вендоренных без предмета самоистекает, как всякое послабление: проход
    # заглушек этот каталог НЕ осматривает, и оправдание тому — только архив в нём.
    if not archives:
        findings.append("%s — в каталоге нет ни одного отслеживаемого архива чарта, а проход "
                        "заглушек его не осматривает: второй проход ничего не открывает, "
                        "и снимать каталог с первого прохода больше незачем"
                        % iac_scan_passes.VENDOR_HOME)
    wf_findings, wf_census = check_workflow_passes()
    findings += wf_findings

    print("iac-chart-coverage: чартов-каталогов в дереве %d; не рендерят ничего %d (каталоги "
          "и архивы); послаблений %d (не судимо в этом прогоне %d); архивов-чартов в %s %d, вне его %d "
          "(подчартов-архивов %d, архивов не чартов %d, не прочитано %d); кандидатов %d; "
          "целей скана %d (%s); непокрытых %d"
          % (len(charts), len(renders_none), len(exempt_ok),
             len(exempt_unjudged), iac_scan_passes.VENDOR_HOME, len(archives), len(misplaced),
             len(nested_archives), len(not_charts), len(unreadable),
             len(covered) + len(uncovered), len(targets),
             ", ".join("проход «%s» %d" % kv for kv in census.items()), len(uncovered)))
    print(wf_census)
    print("  архивов среди отслеживаемых файлов %d из %d; оглавления прочитаны за %.1f с"
          % (len(candidates), len(tracked), sniff_s))
    for a, kind in not_charts:
        print("  не чарт             %s — %s" % (a, "сжатый поток без оглавления"
                                                  if kind == "поток" else
                                                  "Chart.yaml в оглавлении нет"))
    for d, n in covered:
        print("  осмотрен %2d целей  %s" % (n, d))
    for a, n in sub_archives_via_parent:
        print("  осмотрен %2d целей  %s — через родителя" % (n, a))
    for a, names in not_deployed:
        print("  не развёртывается   %s — ни один профиль не рендерит %s (условие зависимости "
              "ложно во всех стеках)" % (a, ", ".join(names)))
    for d in exempt_ok:
        print("  послабление         %s — %s" % (d, EXEMPT[d]))
    for name, path in log_unjudged:
        print("  НЕ СУДИМО           %s — ERROR прохода «%s» о файле, которого git не "
              "отслеживает (местная сборка зависимостей)" % (path, name))
    for d, why in renders_none:
        print("  не рендерит ничего  %s — %s" % (d, why))
    for d, why in exempt_unjudged:
        print("  НЕ СУДИМО           %s — %s.\n"
              "                      Свойство репозитория отсюда не видно: CI берёт\n"
              "                      свежий checkout, где этих файлов нет." % (d, why))

    for d in uncovered:
        if d in nested_archives:
            findings.append("%s — подчарт-архив НЕ ДАЛ ни одной цели: ни своим проходом "
                            "(«%s»), ни через родителя — среди целей нет его шаблонов; "
                            "сторонний чарт вне скана целиком" % (d, ren_pass[0]))
            continue
        if d in archives:
            findings.append("%s — архив-чарт НЕ ДАЛ ни одной цели проходу «%s» (без "
                            "заглушек): рендер отказал (причина — строкой ERROR выше, если "
                            "сканер её написал) либо форму архива сканер не рендерит — "
                            "место ему в .tgz" % (d, bare_pass[0]))
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
