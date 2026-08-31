# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# schema-rollback-statement.py — ЗАЯВЛЕНИЕ ОБ ОТКАТЕ СХЕМЫ, произведённое из
# дерева, а не написанное прозой.
#
# ЗАЧЕМ. Мигратор исполняется при КАЖДОМ раскате. Значит штатный откат выкатки
# возвращает ПРЕЖНИЙ ОБРАЗ НА НОВУЮ СХЕМУ — и до этой работы ничто такого
# состояния не отвергало и даже не называло: единственным советом по откату была
# одна строка `helm rollback`, о схеме не говорившая ничего. Разрыв тихий: раскат
# зелёный, под поднимается, отказ приходит на первом обращении к колонке, которой
# в образе ещё нет либо в схеме уже нет.
#
# ЧТО ЭТОТ СКРИПТ ПРОИЗВОДИТ. По каждому сервису — перепись цепочки и вердикт:
#   миграций осмотрено · без секции отката · объявлено необратимыми · неучтённых
# плюс ТОЧКУ НЕВОЗВРАТА: наибольшую версию, ниже которой откат схемы невозможен,
# потому что данные сняты и восстановить их нечем.
#
# ЧТО ОН НЕ ПРОИЗВОДИТ И НЕ ПРИТВОРЯЕТСЯ. Он не откатывает схему и не судит
# ЖИВУЮ базу: он читает дерево. Совместимость запущенного образа с applied-
# версией — предмет стража старта сервиса, и такого стража сегодня нет; это
# сказано вслух здесь, чтобы «заявление есть» не читалось как «проверка есть».
#
# ИСХОДЫ: 0 — заявление произведено; 1 — есть миграция без секции отката, чья
# судьба не объявлена ни dropguard-манифестом, ни ведомостью (то есть отсутствие
# секции неотличимо от обратимости); 2 — корпус пуст, и это отказ, а не успех.

import json
import os
import re
import sys

VERSION = re.compile(r"^(\d+)_")
DOWN = "+goose Down"

LEDGER = "deploy/schema-rollback.txt"
LEDGER_ROW = re.compile(r"^([a-z0-9-]+)\|([^|]+)\|(необратима|откат-не-нужен)\|(\S.*)$")


def migration_dirs(root):
    """Каталоги миграций выводятся обходом, а не выписываются: новый сервис
    попадает под заявление сам, снятый уходит вместе со своим каталогом."""
    out = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in (".git", "node_modules", "vendor")]
        if not any(f.endswith(".sql") for f in filenames):
            continue
        if os.path.basename(dirpath) != "migrations" and os.path.basename(
            os.path.dirname(dirpath)
        ) != "migrations":
            continue
        out.append(dirpath)
    return sorted(set(out))


def owner_of(root, path):
    rel = os.path.relpath(path, root).replace(os.sep, "/")
    parts = rel.split("/")
    if parts[0] == "services" and len(parts) > 1:
        return parts[1]
    if parts[0] == "gateway":
        return "gateway"
    return "/".join(parts[:-1])


def read_ledger(root):
    rows = {}
    p = os.path.join(root, LEDGER)
    if not os.path.exists(p):
        return rows, p
    with open(p, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            m = LEDGER_ROW.match(line)
            if not m:
                print("ОТКАЗ|строка ведомости не разобрана: %s" % line, file=sys.stderr)
                sys.exit(1)
            rows[(m.group(1), m.group(2))] = (m.group(3), m.group(4))
    return rows, p


def main():
    root = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), "..", "..")
    root = os.path.abspath(root)
    ledger, ledger_path = read_ledger(root)

    dirs = migration_dirs(root)
    if not dirs:
        print("ОТКАЗ: каталогов миграций не найдено ни одного — обход пуст, "
              "и это отказ, а не успех: заявление об откате не о чем делать", file=sys.stderr)
        return 2

    total = no_down = declared = unaccounted = 0
    used = set()
    lines = []
    for d in dirs:
        owner = owner_of(root, d)
        label = os.path.relpath(d, root).replace(os.sep, "/")
        drops = {}
        dg = os.path.join(d, "dropguard.json")
        if os.path.exists(dg):
            with open(dg, encoding="utf-8") as fh:
                for r in json.load(fh).get("drops", []):
                    drops.setdefault(int(r["version"]), []).append(r.get("table", "?"))

        files = sorted(f for f in os.listdir(d) if f.endswith(".sql"))
        svc_total = len(files)
        svc_nodown, svc_decl, svc_bad = 0, [], []
        for f in files:
            with open(os.path.join(d, f), encoding="utf-8") as fh:
                body = fh.read()
            if DOWN in body:
                continue
            svc_nodown += 1
            m = VERSION.match(f)
            ver = int(m.group(1)) if m else -1
            if ver in drops:
                svc_decl.append("%s (снятия объявлены dropguard: %s)" % (f, ", ".join(drops[ver])))
                continue
            key = (owner, f)
            if key in ledger:
                used.add(key)
                svc_decl.append("%s (ведомость: %s)" % (f, ledger[key][0]))
                continue
            svc_bad.append(f)

        point = max(drops) if drops else None
        for f, (verdict, _why) in ((k[1], v) for k, v in ledger.items() if k[0] == owner):
            if verdict == "необратима":
                m = VERSION.match(f)
                if m:
                    point = max(point or 0, int(m.group(1)))

        total += svc_total
        no_down += svc_nodown
        declared += len(svc_decl)
        unaccounted += len(svc_bad)
        lines.append(
            "  %-38s миграций %3d · без секции отката %d · объявлено необратимыми %d · неучтённых %d · "
            "точка невозврата %s"
            % (label, svc_total, svc_nodown, len(svc_decl), len(svc_bad),
               point if point is not None else "нет")
        )
        for b in svc_bad:
            lines.append("      НЕУЧТЕНА: %s — секции отката нет, и решение не записано нигде" % b)

    stale = [k for k in ledger if k not in used]

    print("ЗАЯВЛЕНИЕ ОБ ОТКАТЕ СХЕМЫ (произведено из дерева, не из прозы)")
    print("  Мигратор идёт при КАЖДОМ раскате. `helm rollback` возвращает ОБРАЗЫ и НЕ")
    print("  возвращает схему: применённые миграции остаются применёнными. Прежний образ")
    print("  поднимется на новой схеме, и НИЧТО этого не отвергнет — стража старта,")
    print("  сверяющего версию схемы с той, которую образ умеет обслуживать, сегодня нет.")
    print("")
    print("\n".join(lines))
    print("")
    print("ИТОГО: миграций %d · без секции отката %d · объявлено необратимыми %d · неучтённых %d"
          % (total, no_down, declared, unaccounted))
    if total == 0:
        print("ОТКАЗ: прочитано ноль миграций — «ноль находок» здесь неотличимо от "
              "«ноль прочитанного»", file=sys.stderr)
        return 2

    rc = 0
    for k in stale:
        print("ОТКАЗ: ведомость %s называет %s/%s, а у этой миграции секция отката есть либо "
              "самой миграции больше нет — строка пережила свой предмет"
              % (ledger_path, k[0], k[1]), file=sys.stderr)
        rc = 1
    if unaccounted:
        print("ОТКАЗ: %d миграци(й) без секции отката, чья судьба не объявлена ни dropguard-"
              "манифестом, ни ведомостью %s. Отсутствие секции неотличимо от обратимости, и "
              "оператор узнаёт исход ПОСЛЕ отката" % (unaccounted, LEDGER), file=sys.stderr)
        rc = 1
    return rc


if __name__ == "__main__":
    sys.exit(main())
