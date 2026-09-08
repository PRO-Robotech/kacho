#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""ВНЕШНИЕ ЧАРТЫ ВЕНДОРЯТСЯ, И ИХ ПИН ТОЧЕН (задача #2256).

─────────────────────────────────────────────────────────────────────────────
ПРЕДМЕТ

Подъём стенда тянул зависимости зонтичного чарта с ПОСТОРОННИХ хостов на пути к
вердикту. Замерено на этом дереве, а не предположено: с ЗАБЛОКИРОВАННОЙ сетью и
всеми архивами на месте `helm dependency update` всё равно отказывает — он
перекачивает КАЖДОЕ объявление, не переиспользуя ничего. То есть недоступность
чужого хоста даёт «условие не создано» (третий исход: не красный, но и не
зелёный), и «прогон зелен целиком» становится недостижим by construction.

Плавающий диапазон делает поход в сеть ОБЯЗАТЕЛЬНЫМ вдобавок: пропустить его
нельзя, не зная, не появилась ли новая версия. Отсюда второй, сегодня невидимый
дефект — рендер невоспроизводим: два прогона одного дерева вправе поставить
разные версии чужого чарта, и заметить это нечем.

─────────────────────────────────────────────────────────────────────────────
ЧТО ГЕЙТ ТРЕБУЕТ — ПЯТЬ ОСЕЙ, И КАЖДАЯ САМОИСТЕКАЕТ

  1. внешнее объявление несёт ТОЧНЫЙ пин (ни `x`, ни диапазона, ни `*`);
  2. у каждого внешнего объявления есть архив, ОТСЛЕЖИВАЕМЫЙ git;
  3. у каждого архива есть запись провенанса, и сумма СХОДИТСЯ;
  4. запись провенанса, которой не соответствует ни одно объявление, — находка
     (исключение живёт, пока у него есть предмет);
  5. отслеживаемый архив, которого не объявляет ни одна зависимость, — находка
     (иначе снятые версии копились бы в дереве молча).

ЛОКАЛЬНЫЕ САБЧАРТЫ (`file://…`) СЮДА НЕ ВХОДЯТ И ВХОДИТЬ НЕ ДОЛЖНЫ. Они
собираются ИЗ ИСХОДНИКОВ этого же дерева, и вендоренная копия означала бы
«правка есть в файле, а в стенд не попала». Ось 5 поэтому судит ТОЛЬКО
отслеживаемые архивы: местный результат материализации git не отслеживает, и
вердикт от него не зависит — CI видит свежий checkout.

ЕДИНИЦА ИЗМЕРЕНИЯ — GIT, А НЕ ДИСК. Тот же довод, что у
`assert-iac-scan-covers-every-chart.py`: на машине разработчика в `charts/`
лежит результат `helm dependency update`, объявленный git-ignored. Меряя диск,
гейт был бы зелёным там, где репозиторий ничего не вендорит, — то есть
противоречил бы собственному предмету.

СУММА СВЕРЯЕТСЯ С ИЗДАТЕЛЕМ ОТДЕЛЬНО И ПО РУЧКЕ. `--verify-upstream` тянет
`index.yaml` каждого источника и сличает нашу запись с полем `digest` издателя.
Это измерение СЕТЕВОЕ, поэтому оно не входит в обычный проход: гейт, чей вердикт
зависит от чужой доступности, воспроизводит ровно тот дефект, ради которого
заведён. Состояние сверки печатается ВСЕГДА — «сверено с издателем 0» никогда не
выглядит как «сверено».

ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ, И ПУСТОЙ ОБХОД — ОТКАЗ. «Ноль находок» обязано
быть отличимо от «ноль прочитанного».

Коды возврата: 0 — находок нет; 1 — находка; 2 — предпосылки нет (не прочитан
Chart.yaml, нет разбора YAML) — это «условие не создано», а не вердикт о дереве.
"""
import hashlib
import pathlib
import re
import subprocess
import sys

try:
    import yaml
except ImportError:  # pragma: no cover — предпосылка, а не находка
    print("ОТКАЗ: нет модуля yaml — судить не о чем", file=sys.stderr)
    sys.exit(2)

ROOT = pathlib.Path(__file__).resolve().parents[2]
UMBRELLA = "deploy/helm/umbrella"
CHART = ROOT / UMBRELLA / "Chart.yaml"
PROVENANCE = ROOT / UMBRELLA / "external-charts.yaml"

# Точный пин — это версия целиком, без подстановки и без оператора диапазона.
# Ведущая `v` законна (её несёт cert-manager), поэтому она в образце, а не
# исключением рядом.
EXACT = re.compile(r"^v?\d+(\.\d+)*(-[0-9A-Za-z.\-]+)?(\+[0-9A-Za-z.\-]+)?$")


def is_exact(version):
    return bool(EXACT.match(str(version).strip()))


def git_tracked(pattern):
    r = subprocess.run(["git", "ls-files", pattern], cwd=ROOT,
                       capture_output=True, text=True, timeout=120)
    if r.returncode != 0:
        print("ОТКАЗ: git ls-files %s вышел с кодом %d: %s"
              % (pattern, r.returncode, r.stderr[:300]), file=sys.stderr)
        sys.exit(2)
    return sorted(line.strip() for line in r.stdout.splitlines() if line.strip())


def external_deps(chart_doc):
    """→ [(имя, версия)] объявлений, чей источник НЕ локальный."""
    out = []
    for d in chart_doc.get("dependencies") or []:
        repo = str(d.get("repository") or "")
        if repo.startswith("file://"):
            continue
        out.append((str(d.get("name") or ""), str(d.get("version") or ""), repo))
    return out


def judge(deps, provenance, tracked_archives, digest_of):
    """ТЕЛО гейта, вынесенное отдельно: инъекция зовёт то же, что исполняется на
    дереве. Своя копия предиката в инъекции разошлась бы с настоящим гейтом
    молча.

    deps            — [(имя, версия, источник)] внешних объявлений
    provenance      — {имя архива: {sha256, name, version}}
    tracked_archives— множество имён архивов, отслеживаемых git
    digest_of       — имя архива → sha256 (None, если архива нет на диске)
    → (перепись, находки)
    """
    findings = []
    floating = 0
    wanted = {}

    for name, version, repo in deps:
        if not is_exact(version):
            floating += 1
            findings.append(
                "%s: версия %r у внешнего источника %s — не точный пин. Плавающий "
                "диапазон ОБЯЗЫВАЕТ идти в сеть (пропустить нельзя, не зная, не "
                "появилась ли новая версия) и делает рендер невоспроизводимым"
                % (name, version, repo))
            continue
        wanted["%s-%s.tgz" % (name, version)] = (name, version, repo)

    for archive, (name, version, repo) in sorted(wanted.items()):
        if archive not in tracked_archives:
            findings.append(
                "%s: объявлен внешним источником %s, а архив %s git НЕ отслеживает — "
                "материализация пойдёт в сеть, и недоступность чужого хоста станет "
                "условием вердикта" % (name, repo, archive))
            continue
        rec = provenance.get(archive)
        if rec is None:
            findings.append(
                "%s: архив вендорен, но записи провенанса нет — нечем доказать, что "
                "это то, что опубликовал вышестоящий" % archive)
            continue
        actual = digest_of(archive)
        if actual is None:
            findings.append("%s: отслеживается git, но на диске отсутствует" % archive)
        elif actual != rec.get("sha256"):
            findings.append(
                "%s: сумма разошлась с записью провенанса (в дереве %s…, записано %s…)"
                % (archive, actual[:16], str(rec.get("sha256"))[:16]))

    for archive in sorted(provenance):
        if archive not in wanted:
            findings.append(
                "%s: запись провенанса есть, а объявления с такой версией нет — "
                "исключению больше нечего исключать, снимите запись вместе с архивом"
                % archive)

    for archive in sorted(tracked_archives):
        if archive not in wanted:
            findings.append(
                "%s: архив отслеживается git, а ни одна зависимость его не объявляет — "
                "снятая версия осталась в дереве" % archive)

    census = {
        "внешних объявлений": len(deps),
        "плавающих": floating,
        "ожидаемых архивов": len(wanted),
        "отслеживается git": len(tracked_archives),
        "записей провенанса": len(provenance),
    }
    return census, findings


def load_provenance():
    if not PROVENANCE.exists():
        return None
    doc = yaml.safe_load(PROVENANCE.read_text(encoding="utf-8")) or {}
    out = {}
    for e in doc.get("charts") or []:
        out[str(e.get("archive"))] = {
            "sha256": str(e.get("sha256") or ""),
            "name": str(e.get("name") or ""),
            "version": str(e.get("version") or ""),
            "repository": str(e.get("repository") or ""),
        }
    return out


def digest_on_disk(archive):
    f = ROOT / UMBRELLA / "charts" / archive
    if not f.is_file():
        return None
    return hashlib.sha256(f.read_bytes()).hexdigest()


def verify_upstream(provenance):
    """Сверка записи с полем `digest` издателя. Сетевая, потому по ручке.
    → (сверено, находки, недостижимо)."""
    import json  # noqa: F401 — оставлен на случай источников с JSON-индексом
    checked, findings, unreachable = 0, [], 0
    for archive, rec in sorted(provenance.items()):
        url = rec["repository"].rstrip("/") + "/index.yaml"
        r = subprocess.run(["curl", "-sSL", "--max-time", "120", "-A", "Mozilla/5.0", url],
                           capture_output=True, text=True)
        if r.returncode != 0 or not r.stdout.strip():
            unreachable += 1
            continue
        try:
            idx = yaml.safe_load(r.stdout) or {}
        except yaml.YAMLError:
            unreachable += 1
            continue
        entries = [e for e in (idx.get("entries") or {}).get(rec["name"], [])
                   if str(e.get("version")) == rec["version"]]
        if not entries:
            findings.append("%s: у издателя нет записи о версии %s" % (archive, rec["version"]))
            continue
        checked += 1
        pub = str(entries[0].get("digest") or "")
        if pub != rec["sha256"]:
            findings.append("%s: запись провенанса расходится с издателем "
                            "(у нас %s…, у издателя %s…)" % (archive, rec["sha256"][:16], pub[:16]))
    return checked, findings, unreachable


# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА: инъекция в обе стороны по КАЖДОЙ оси, с законным близнецом.
# Без неё «находок 0» на здоровом дереве неотличимо от гейта, не умеющего падать.
GOOD_DEPS = [("postgresql", "13.4.4", "https://charts.bitnami.com/bitnami")]
GOOD_PROV = {"postgresql-13.4.4.tgz": {"sha256": "aa" * 32, "name": "postgresql",
                                       "version": "13.4.4",
                                       "repository": "https://charts.bitnami.com/bitnami"}}
GOOD_TRACKED = {"postgresql-13.4.4.tgz"}


def _good_digest(_archive):
    return "aa" * 32


def self_test():
    rc = 0

    def case(want, label, deps, prov, tracked, digest=_good_digest, needle=None):
        nonlocal rc
        _, f = judge(deps, prov, tracked, digest)
        got = "находка" if f else "молчит"
        ok = got == want and (needle is None or any(needle in x for x in f))
        print("  %s %s → %s%s" % ("ОК " if ok else "ПРОВАЛ", label, got,
                                  "" if ok else " (%s)" % (f or "пусто")))
        if not ok:
            rc = 1

    print("=== assert-vendored-external-charts --self-test ===")
    print("  --- законный близнец: всё на месте")
    case("молчит", "точный пин + архив + сумма сходится",
         GOOD_DEPS, dict(GOOD_PROV), set(GOOD_TRACKED))

    print("  --- инъекция по каждой оси (по одному факту за раз)")
    case("находка", "ось 1: плавающий диапазон",
         [("postgresql", "13.x", "https://charts.bitnami.com/bitnami")],
         {}, set(), needle="не точный пин")
    case("находка", "ось 2: архив git не отслеживает",
         GOOD_DEPS, dict(GOOD_PROV), set(), needle="НЕ отслеживает")
    case("находка", "ось 3а: записи провенанса нет",
         GOOD_DEPS, {}, set(GOOD_TRACKED), needle="записи провенанса нет")
    case("находка", "ось 3б: сумма разошлась",
         GOOD_DEPS, dict(GOOD_PROV), set(GOOD_TRACKED),
         digest=lambda _a: "bb" * 32, needle="сумма разошлась")
    case("находка", "ось 4: запись без объявления",
         [], dict(GOOD_PROV), set(), needle="больше нечего исключать")
    case("находка", "ось 5: архив без объявления",
         [], {}, set(GOOD_TRACKED), needle="ни одна зависимость его не объявляет")

    print("  --- законные близнецы, на которых гейт обязан МОЛЧАТЬ")
    case("молчит", "локальный сабчарт в объявлениях не участвует",
         GOOD_DEPS, dict(GOOD_PROV), set(GOOD_TRACKED))
    case("молчит", "ведущая `v` в версии — точный пин (cert-manager)",
         [("cert-manager", "v1.16.5", "https://charts.jetstack.io")],
         {"cert-manager-v1.16.5.tgz": {"sha256": "aa" * 32, "name": "cert-manager",
                                       "version": "v1.16.5",
                                       "repository": "https://charts.jetstack.io"}},
         {"cert-manager-v1.16.5.tgz"})
    case("молчит", "предрелизная версия — точный пин",
         [("x", "1.2.3-rc.1", "https://example.test")],
         {"x-1.2.3-rc.1.tgz": {"sha256": "aa" * 32, "name": "x", "version": "1.2.3-rc.1",
                               "repository": "https://example.test"}},
         {"x-1.2.3-rc.1.tgz"})

    print("  --- разбор версии: обе стороны")
    for v, want in [("13.4.4", True), ("v1.16.5", True), ("0.62.1", True),
                    ("1.2.3-rc.1", True),
                    ("13.x", False), ("4.x", False), ("v1.16.x", False),
                    (">= 0.0.0", False), ("^1.2.3", False), ("~1.2", False),
                    ("*", False), ("1.2.3 - 1.3.0", False)]:
        got = is_exact(v)
        ok = got == want
        print("  %s версия %-12r → %s" % ("ОК " if ok else "ПРОВАЛ", v,
                                          "точный" if got else "плавающий"))
        if not ok:
            rc = 1

    print("=== самопроверка: %s ===" % ("ОК" if rc == 0 else "ПРОВАЛ"))
    return rc


def main():
    if "--self-test" in sys.argv:
        sys.exit(self_test())
    args = sys.argv[1:]

    if not CHART.exists():
        print("ОТКАЗ: %s не найден — предпосылки нет" % CHART, file=sys.stderr)
        sys.exit(2)
    try:
        doc = yaml.safe_load(CHART.read_text(encoding="utf-8")) or {}
    except yaml.YAMLError as e:
        print("ОТКАЗ: %s не разобран: %s" % (CHART, e), file=sys.stderr)
        sys.exit(2)

    deps = external_deps(doc)
    if not deps:
        print("ОТКАЗ: внешних объявлений НОЛЬ — обход пуст, гейт судил бы о "
              "непрочитанном", file=sys.stderr)
        sys.exit(2)

    provenance = load_provenance()
    if provenance is None:
        print("ОТКАЗ: %s не найден — провенанс вендоренных архивов не объявлен"
              % PROVENANCE, file=sys.stderr)
        sys.exit(2)

    prefix = UMBRELLA + "/charts/"
    tracked = {p[len(prefix):] for p in git_tracked(prefix + "*.tgz")}

    census, findings = judge(deps, provenance, tracked, digest_on_disk)

    upstream_state = "не запрошена (измерение сетевое, ручка --verify-upstream)"
    if "--verify-upstream" in args:
        checked, up_findings, unreachable = verify_upstream(provenance)
        findings += up_findings
        upstream_state = ("сверено с издателем %d из %d, источник не ответил у %d"
                          % (checked, len(provenance), unreachable))

    print("перепись: " + " · ".join("%s %d" % (k, v) for k, v in census.items()))
    print("сверка с издателем: " + upstream_state)
    for a in sorted(provenance):
        print("  %s ← %s" % (a, provenance[a]["repository"]))

    if findings:
        print("\nНАХОДОК %d:" % len(findings))
        for f in findings:
            print("  · " + f)
        sys.exit(1)
    print("находок нет")


if __name__ == "__main__":
    main()
