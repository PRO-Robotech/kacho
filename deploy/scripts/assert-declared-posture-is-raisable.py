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

Сегодня такова полоса `own`: композиционный корень объявляет ЛИТЕРАЛАМИ
провязанность своих способов входа человека и своей сессии, пустой перечень
предъявимых уровней доверия и безусловную стройку дороги к внешнему поставщику
вместе с записью зеркала его ключей (проверка хранит их координату и требует,
чтобы литералы были на месте, — см. ведомость ниже). Требования полосы читают
именно эти наблюдения, поэтому неисполнимы ВСЕ значения профиля, а не какие-то
выбранные: это свойство построения, а не недонастройки.

ЧИСЛО ОТКАЗОВ ЗДЕСЬ НЕ ВЫПИСАНО — намеренно. Прежняя редакция говорила «три
требования», и это устарело молча: таблица требований выросла, отказов стало
пять, а текст остался. Величина, у которой нет владельца, стареет вместе с
предметом; её место — вывод самой полосы, а не проза о ней.

─────────────────────────────────────────────────────────────────────────────
КОРНЕЙ ДВА, И КАЖДЫЙ СУДИТСЯ ОТДЕЛЬНО

Посадку объявляют профили ДВУХ корней: чарт ПРОДУКТА (поставка, ради которой
службу выносят отдельно) и ЗОНТ платформы (стенды монорепо). Ключи у них разные
— чарт продукта несёт `authn.identityProvider` верхним уровнем, зонт прячет его
под секцией службы, — поэтому ключи едут ВМЕСТЕ с путём.

Прежняя редакция читала только зонт: профиль чарта продукта мог объявить
неподъёмную посадку, и проверка молчала. Инъекция одного факта показывает это
дословно — объявление `own` в `services/iam/deploy/values.prod.yaml` находила
проба композиционного корня и НЕ находила эта проверка. Перепись печатается ПО
КАЖДОМУ корню: одно число на оба скрывает ровно тот случай, ради которого
проверка расширена.

Отсутствие ОДНОГО корня гасит ЕГО, а не проверку целиком: погашенная проверка
перестала бы судить второй корень — то есть ровно тот, ради которого написана.

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
import tempfile

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
PRODUCT_CHART_DIR_REL = "services/iam/deploy"
# Что обязано стоять в находке о профиле чарта продукта — координата файла.
PRODUCT_CHART_FINDING_NEEDLE = "values.prod.yaml"

# Базовые значения подчартов — умолчание всякого стенда, не назвавшего полосу
# сам, поэтому они считаются профилями наравне с остальными. Путь службы задан
# ОТНОСИТЕЛЬНО зонта: он и есть подчарт, и синтетическое дерево самопроверки
# получает его тем же выражением, что и живое.
SUBCHART_KANAME_REL = "charts/kaname/values.yaml"
SUBCHART_KANAME_KEYS = ("config", "authn", "identityProvider")
GATEWAY_VALUES = ROOT / "gateway/deploy/values.yaml"
GATEWAY_KEYS = ("authn", "identityProvider")

# ВЕДОМОСТЬ: посадка → чем доказано, что дерево её не поднимает.
#
# `anchor` — файл композиционного корня; `literals` — объявления, которые обязаны
# быть литеральными, чтобы запись оставалась верной. Предпосылка проверяется на
# КАЖДОМ прогоне: пропала — находка, а не молчаливое продление.
NOT_RAISABLE = {
    "own": {
        "anchor": ROOT / "services/iam/cmd/kaname/laneposture.go",
        # КАЖДОЕ наблюдение корня, которым полоса сегодня заблокирована. Перечень
        # обязан быть ПОЛНЫМ, а не удобным: премиса, названная подмножеством,
        # молчит ровно тогда, когда блокирующий набор поменялся, — то есть тогда,
        # когда полосу и надо пересудить. Пара «поле: значение»; выравнивание
        # между ними задаёт gofmt и меняет длина соседнего имени, поэтому пробелы
        # здесь не значимы (иначе запись теряла бы предпосылку от чужой правки).
        "literals": [
            "HumanCredentialsWired: false",
            "HumanSessionsWired: false",
            "PresentableACRs: nil",
            "ProviderAdminHopBuilt: true",
            "ProviderKeySetMirrorPublished: true",
        ],
        "why": ("композиционный корень объявляет ЛИТЕРАЛАМИ и провязанность своих "
                "способов входа человека и своей сессии, и пустой перечень предъявимых "
                "уровней доверия, и безусловную стройку дороги к внешнему поставщику "
                "вместе с записью зеркала его ключей; требования полосы читают именно "
                "эти наблюдения, поэтому отказ приходит со стадии ПРОВЯЗКИ, на которую "
                "профиль не влияет. Число отказов здесь НЕ выписано: оно меняется вместе "
                "с таблицей требований, а выписанное — молча стареет"),
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


def literal_re(lit):
    """Пара «поле: значение» → образец, безразличный к выравниванию gofmt."""
    field, _, value = lit.partition(":")
    return re.escape(field.strip()) + r":\s*" + re.escape(value.strip())


def premise_holds(entry):
    """→ (верна, пояснение). Ведомость обязана проверять СВОЮ предпосылку."""
    anchor = entry["anchor"]
    if not anchor.is_file():
        return False, "якоря %s больше нет" % anchor.relative_to(ROOT)
    text = anchor.read_text(encoding="utf-8")
    missing = [lit for lit in entry["literals"] if not re.search(literal_re(lit), text)]
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


def values_files_in(directory):
    """Файлы значений каталога по возрастанию имени; каталога нет → пусто."""
    if not directory.is_dir():
        return []
    return sorted(f for f in directory.iterdir()
                  if f.is_file() and f.name.startswith("values") and f.name.endswith(".yaml"))


def collect_declarations(product_dir=None, umbrella_dir=None, gateway_values=None):
    """Объявления посадки ОБОИХ корней плюс перепись по каждому.

    Корней два, и это не дублирование: чарт ПРОДУКТА — профили той поставки, ради
    которой службу выносят отдельно; ЗОНТ платформы — профили стендов монорепо.
    Ключи у корней РАЗНЫЕ (чарт продукта несёт `authn.identityProvider` верхним
    уровнем, зонт — под секцией службы), поэтому ключи едут ВМЕСТЕ с путём, а не
    выбираются по имени корня в месте чтения: второй перечень ключей разошёлся бы
    с первым молча.

    Отсутствие ОДНОГО корня гасит ЕГО, а не проверку целиком: погашенная проверка
    перестала бы судить и второй корень — то есть ровно тот, ради которого она
    написана.

    → (объявления, перепись по корням, оговорки)
    """
    product_dir = ROOT / PRODUCT_CHART_DIR_REL if product_dir is None else product_dir
    umbrella_dir = UMBRELLA if umbrella_dir is None else umbrella_dir
    gateway_values = GATEWAY_VALUES if gateway_values is None else gateway_values

    out, census, notes = [], [], []

    seen = 0
    for f in values_files_in(product_dir):
        out.append((str(_rel(f)), "iam", read_nested(f, ("authn", "identityProvider"))))
        seen += 1
    census.append(("чарт продукта", seen))
    if seen == 0:
        notes.append("чарт продукта: УСЛОВИЕ НЕ СОЗДАНО (не находка) — каталога %s нет"
                     % _rel(product_dir))

    seen = 0
    for f in values_files_in(umbrella_dir):
        for half, keys in sorted(HALVES.items()):
            out.append((str(_rel(f)), half, read_nested(f, keys)))
        seen += 1
    for path, keys, half in ((umbrella_dir / SUBCHART_KANAME_REL, SUBCHART_KANAME_KEYS, "iam"),
                             (gateway_values, GATEWAY_KEYS, "gateway")):
        if path.is_file():
            out.append((str(_rel(path)) + " (базовое значение подчарта)", half,
                        read_nested(path, keys)))
            seen += 1
    census.append(("зонт платформы", seen))
    if seen == 0:
        notes.append("зонт платформы: УСЛОВИЕ НЕ СОЗДАНО (не находка) — каталога %s нет"
                     % _rel(umbrella_dir))

    return out, census, notes


def _rel(path):
    """Координата от корня дерева; вне дерева — как есть."""
    try:
        return path.relative_to(ROOT)
    except ValueError:
        return path


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

    print("  --- КОРНИ: оба читаются, и каждый судится (задача #2101)")
    decls, roots, _ = collect_declarations()
    by_root = dict(roots)
    for name in ("чарт продукта", "зонт платформы"):
        if by_root.get(name, 0) > 0:
            print("  ОК  корень [%s]: осмотрено файлов значений %d" % (name, by_root[name]))
        else:
            print("  ПРОВАЛ корень [%s] НЕ ЧИТАЕТСЯ: из %d объявлений ни одно не пришло "
                  "оттуда — профиль этого корня объявил бы неподъёмную посадку, и "
                  "проверка промолчала бы" % (name, len(decls)))
            rc = 1

    print("  --- инъекция в СИНТЕТИЧЕСКОЕ дерево: производитель входа вызываем")
    with tempfile.TemporaryDirectory() as tmp:
        tmp = pathlib.Path(tmp)
        prod, umb = tmp / "product", tmp / "umbrella"
        (prod).mkdir()
        (umb / SUBCHART_KANAME_REL).parent.mkdir(parents=True)
        gw = tmp / "gateway-values.yaml"
        gw.write_text("authn:\n  identityProvider: external\n", encoding="utf-8")
        (umb / SUBCHART_KANAME_REL).write_text(
            "config:\n  authn:\n    identityProvider: external\n", encoding="utf-8")

        def run(product_posture):
            (prod / "values.prod.yaml").write_text(
                "authn:\n  identityProvider: %s\n" % product_posture, encoding="utf-8")
            d, _, _ = collect_declarations(product_dir=prod, umbrella_dir=umb,
                                           gateway_values=gw)
            _, f = judge(d, good, holds)
            return f

        # законный близнец: тот же корень, подъёмная посадка — обязан МОЛЧАТЬ
        f = run("external")
        print("  %s чарт продукта объявил подъёмную посадку → %s"
              % ("ОК " if not f else "ПРОВАЛ", "молчит" if not f else f))
        if f:
            rc = 1

        # инъекция ОДНОГО факта: та же строка, неподъёмная посадка
        f = run("own")
        hit = any(PRODUCT_CHART_FINDING_NEEDLE in x for x in f)
        print("  %s чарт продукта объявил неподъёмную посадку → %s"
              % ("ОК " if hit else "ПРОВАЛ", "находка с координатой" if hit else (f or "молчит")))
        if not hit:
            rc = 1

        # отсутствие ОДНОГО корня гасит ЕГО, а не проверку: продуктовый корень
        # обязан по-прежнему судиться, иначе в самостоятельном клоне у класса
        # не осталось бы держателя вовсе.
        (prod / "values.prod.yaml").write_text(
            "authn:\n  identityProvider: own\n", encoding="utf-8")
        d, roots2, notes2 = collect_declarations(product_dir=prod,
                                                 umbrella_dir=tmp / "нет-такого",
                                                 gateway_values=tmp / "нет-такого.yaml")
        _, f = judge(d, good, holds)
        ok = bool(f) and dict(roots2).get("зонт платформы") == 0 and bool(notes2)
        print("  %s зонта нет → продуктовый корень всё равно судится, второй назван "
              "оговоркой → %s" % ("ОК " if ok else "ПРОВАЛ",
                                  "находка + оговорка" if ok else (f, roots2, notes2)))
        if not ok:
            rc = 1

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

    declarations, roots, notes = collect_declarations()
    if not declarations:
        print("ОТКАЗ: обход пуст — ни один корень не прочитан, проверка судила бы о "
              "непрочитанном", file=sys.stderr)
        sys.exit(2)

    census, findings = judge(declarations, NOT_RAISABLE,
                             lambda p: premise_holds(NOT_RAISABLE[p]))

    # Перепись ПО КАЖДОМУ корню: «ноль прочитанного у корня» обязано быть отличимо
    # от «корень ничего не объявляет». Одно число на оба корня скрывает ровно тот
    # случай, ради которого проверка расширена (задача #2101).
    for name, n in roots:
        print("перепись корня [%s]: осмотрено файлов значений %d" % (name, n))
    for n in notes:
        print("  " + n)
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
