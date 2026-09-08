#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""ПРОФИЛЬ НЕ ОБЪЯВЛЯЕТ ПОСАДКУ, КОТОРУЮ ДЕРЕВО НЕ УМЕЕТ ПОДНЯТЬ (задача #2101).

─────────────────────────────────────────────────────────────────────────────
ПРЕДМЕТ: «РЕНДЕРИТСЯ» НЕ РАВНО «ПОДНИМАЕТСЯ»

Посадка личности объявляется значением в файле значений, а исполняется
требованиями полосы. Требования делятся на две стадии, и они не
взаимозаменяемы: ПОСАДОЧНАЯ читает значения настройки, ПОЛНОТА ПРОВЯЗКИ читает
СОБРАННЫЕ ОБЪЕКТЫ. Профиль влияет только на первую.

Отсюда класс, который шаблонизация не ловит НИКОГДА: профиль объявляет посадку,
чарт рендерится, манифесты валидны, страж посадки доволен — а процесс отказывает
в пуске на стадии провязки, потому что объектов, которых полоса требует, в
композиционном корне нет вовсе. «Зелёный рендер» тут не свидетельство: он
измеряет другую стадию.

Сегодня такова полоса `own`: композиционный корень объявляет провязанность
СВОИХ способов входа человека и СВОЕЙ сессии ЛИТЕРАЛЬНЫМ `false` (проверка
хранит их координату и требует, чтобы литерал был на месте, — см. ниже), а три
требования полосы читают именно их. Значит неисполнимы ВСЕ значения профиля, а
не какие-то выбранные: это свойство построения, а не недонастройки.

─────────────────────────────────────────────────────────────────────────────
ЧЕГО ЭТА ПРОВЕРКА НЕ ДЕЛАЕТ, И ЭТО СКАЗАНО ПРЯМО

Она НЕ закрывает #2101 и не претендует. Предикат той задачи — одно из двух:
полоса поднимается (тогда её сценарий получает держателя) либо пункт снят с
объёма приёмки новым кругом. Оба исхода лежат ВНЕ дерева развёртывания: первый
в композиционном корне службы прав, второй в приёмке `KAN-AUTHN-1`.

Здесь заводится ровно то, чего у класса не было: ДЕРЖАТЕЛЬ на стороне
развёртывания. Пока полосу нельзя поднять, объявить её профилем нельзя — и
узнаётся это правкой файла значений, а не подъёмом стенда.

ВЕДОМОСТЬ САМОИСТЕКАЕТ. Запись «полосу поднять нельзя» держится не памятью, а
предпосылкой: как только композиционный корень перестанет объявлять эти
величины литералом, у записи пропадёт предмет — и это НАХОДКА, требующая
пересудить полосу, а не тихое продление. Ровно тот порядок, каким корпус
требует обращаться со всяким послаблением.

ПУСТАЯ ВЕДОМОСТЬ — ЦЕЛЬ, А НЕ ПОЛОМКА. Когда непубличных полос не останется,
проверка проходит, объявляя перепись; падать на достижении собственной цели
она не вправе.

ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ, ПУСТОЙ ОБХОД — ОТКАЗ.

Коды возврата: 0 — находок нет; 1 — находка; 2 — предпосылки нет.
"""
import pathlib
import re
import sys

try:
    import yaml
except ImportError:  # pragma: no cover
    print("ОТКАЗ: нет модуля yaml — судить не о чем", file=sys.stderr)
    sys.exit(2)

ROOT = pathlib.Path(__file__).resolve().parents[2]
UMBRELLA = ROOT / "deploy/helm/umbrella"

# Где посадку объявляет КАЖДАЯ из двух половин. Половин две, и это не
# дублирование: посадку читают два процесса, у каждого свой файл значений.
HALVES = {
    "iam": ("kaname", "config", "authn", "identityProvider"),
    "gateway": ("api-gateway", "authn", "identityProvider"),
}
SUBCHART_DEFAULTS = {
    "iam": (UMBRELLA / "charts/kaname/values.yaml", ("config", "authn", "identityProvider")),
    "gateway": (ROOT / "gateway/deploy/values.yaml", ("authn", "identityProvider")),
}

# ВЕДОМОСТЬ: посадка → чем доказано, что дерево её не поднимает.
#
# `anchor` — файл композиционного корня; `literals` — объявления, которые обязаны
# быть литеральными, чтобы запись оставалась верной. Предпосылка проверяется на
# КАЖДОМ прогоне: пропала — находка, а не молчаливое продление.
NOT_RAISABLE = {
    "own": {
        "anchor": ROOT / "services/iam/cmd/kaname/laneposture.go",
        "literals": ["HumanCredentialsWired: false", "HumanSessionsWired:    false"],
        "why": ("композиционный корень объявляет провязанность СВОИХ способов входа "
                "человека и СВОЕЙ сессии литеральным `false`, а три требования полосы "
                "читают именно их: отказ приходит со стадии ПРОВЯЗКИ, на которую "
                "профиль не влияет"),
        "issue": "#2101",
    },
}


def read_nested(path, keys):
    """→ значение по пути ключей либо '' («не объявлено»)."""
    if not path.is_file():
        return ""
    try:
        cur = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    except yaml.YAMLError:
        return ""  # профиль с шаблонными вставками — граница предиката
    for k in keys:
        if not isinstance(cur, dict) or k not in cur:
            return ""
        cur = cur[k]
    return cur if isinstance(cur, str) else ""


def premise_holds(entry):
    """→ (верна, пояснение). Ведомость обязана проверять СВОЮ предпосылку."""
    anchor = entry["anchor"]
    if not anchor.is_file():
        return False, "якоря %s больше нет" % anchor.relative_to(ROOT)
    text = anchor.read_text(encoding="utf-8")
    missing = [lit for lit in entry["literals"]
               if not re.search(re.escape(lit), text)]
    if missing:
        return False, ("в %s больше нет литерал(а/ов) %s"
                       % (anchor.relative_to(ROOT), ", ".join(repr(m) for m in missing)))
    return True, ""


def judge(declarations, not_raisable, premise_of):
    """ТЕЛО проверки — вынесено, чтобы инъекция звала то же, что дерево.

    declarations — [(источник, половина, значение)]
    not_raisable — {посадка: запись ведомости}
    premise_of   — посадка → (верна, пояснение)
    → (перепись, находки)
    """
    findings = []
    declaring = 0

    for posture, entry in sorted(not_raisable.items()):
        ok, why = premise_of(posture)
        if not ok:
            findings.append(
                "ведомость: запись о посадке %r потеряла предпосылку (%s). Полосу надо "
                "ПЕРЕСУДИТЬ — возможно, она уже поднимается, и запись пора снять вместе "
                "с задачей %s" % (posture, why, entry.get("issue", "")))

    for source, half, value in declarations:
        if not value:
            continue  # половина посадку не объявляет — наследует базовое значение
        declaring += 1
        entry = not_raisable.get(value)
        if entry is None:
            continue
        findings.append(
            "%s (%s): объявлена посадка %r, которую дерево НЕ УМЕЕТ ПОДНЯТЬ — %s. "
            "Рендер этого не покажет: он измеряет другую стадию. Предмет — %s"
            % (source, half, value, entry["why"], entry.get("issue", "")))

    census = {
        "осмотрено объявлений": len(declarations),
        "объявляют посадку": declaring,
        "полос в ведомости неподъёмных": len(not_raisable),
    }
    return census, findings


def collect_declarations():
    out = []
    for p in sorted(UMBRELLA.glob("values*.yaml")):
        for half, keys in HALVES.items():
            out.append((str(p.relative_to(ROOT)), half, read_nested(p, keys)))
    for half, (path, keys) in sorted(SUBCHART_DEFAULTS.items()):
        rel = str(path.relative_to(ROOT)) if path.is_file() else str(path)
        out.append((rel + " (базовое значение подчарта)", half, read_nested(path, keys)))
    return out


# ─────────────────────────────────────────────────────────────────────────────
def self_test():
    rc = 0
    good = {"own": {"why": "…", "issue": "#2101"}}

    def case(want, label, decls, ledger, premise, needle=None):
        nonlocal rc
        _, f = judge(decls, ledger, premise)
        got = "находка" if f else "молчит"
        ok = got == want and (needle is None or any(needle in x for x in f))
        print("  %s %s → %s%s" % ("ОК " if ok else "ПРОВАЛ", label, got,
                                  "" if ok else " (%s)" % (f or "пусто")))
        if not ok:
            rc = 1

    holds = lambda _p: (True, "")          # noqa: E731
    broken = lambda _p: (False, "литерала больше нет")  # noqa: E731

    print("=== assert-declared-posture-is-raisable --self-test ===")
    print("  --- законные близнецы: проверка обязана МОЛЧАТЬ")
    case("молчит", "профиль объявляет подъёмную посадку",
         [("values.dev.yaml", "iam", "external"), ("values.dev.yaml", "gateway", "external")],
         good, holds)
    case("молчит", "профиль не объявляет посадку вовсе (наследует базовое)",
         [("values.dev.yaml", "iam", ""), ("values.dev.yaml", "gateway", "")],
         good, holds)
    case("молчит", "ПУСТАЯ ведомость — цель достигнута, а не поломка",
         [("values.dev.yaml", "iam", "own")], {}, holds)

    print("  --- инъекция по каждой оси (по одному факту за раз)")
    case("находка", "ось 1: профиль объявил неподъёмную посадку",
         [("values.dev.yaml", "iam", "own")], good, holds,
         needle="НЕ УМЕЕТ ПОДНЯТЬ")
    case("находка", "ось 1б: базовое значение подчарта объявило неподъёмную",
         [("charts/kaname/values.yaml (базовое значение подчарта)", "iam", "own")],
         good, holds, needle="НЕ УМЕЕТ ПОДНЯТЬ")
    case("находка", "ось 2: запись ведомости потеряла предпосылку",
         [("values.dev.yaml", "iam", "external")], good, broken,
         needle="потеряла предпосылку")

    print("  --- предпосылка ведомости на ЖИВОМ дереве (обе стороны)")
    for posture, entry in sorted(NOT_RAISABLE.items()):
        ok, why = premise_holds(entry)
        print("  %s предпосылка записи %r: %s" % ("ОК " if ok else "ПРОВАЛ", posture,
                                                  "на месте" if ok else why))
        if not ok:
            rc = 1
        broken_entry = dict(entry, literals=["ЗаведомоОтсутствующийЛитерал: false"])
        ok2, _ = premise_holds(broken_entry)
        print("  %s инъекция: подменённый литерал предпосылку РУШИТ"
              % ("ОК " if not ok2 else "ПРОВАЛ"))
        if ok2:
            rc = 1

    print("=== самопроверка: %s ===" % ("ОК" if rc == 0 else "ПРОВАЛ"))
    return rc


def main():
    if "--self-test" in sys.argv:
        sys.exit(self_test())

    if not UMBRELLA.is_dir():
        print("ОТКАЗ: %s не найден — предпосылки нет" % UMBRELLA, file=sys.stderr)
        sys.exit(2)

    declarations = collect_declarations()
    if not declarations:
        print("ОТКАЗ: обход пуст — файлов значений не найдено, проверка судила бы о "
              "непрочитанном", file=sys.stderr)
        sys.exit(2)

    census, findings = judge(declarations, NOT_RAISABLE,
                             lambda p: premise_holds(NOT_RAISABLE[p]))

    print("перепись: " + " · ".join("%s %d" % (k, v) for k, v in census.items()))
    for posture, entry in sorted(NOT_RAISABLE.items()):
        ok, why = premise_holds(entry)
        print("  неподъёмна %r: предпосылка %s (якорь %s), предмет %s"
              % (posture, "на месте" if ok else "ПРОПАЛА — " + why,
                 entry["anchor"].relative_to(ROOT), entry.get("issue", "")))

    if findings:
        print("\nНАХОДОК %d:" % len(findings))
        for f in findings:
            print("  · " + f)
        sys.exit(1)
    print("находок нет")


if __name__ == "__main__":
    main()
