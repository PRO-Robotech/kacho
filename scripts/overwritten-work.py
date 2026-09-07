#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# overwritten-work.py — ПРЕДИКАТ ПОЛНОТЫ ведомости отката.
#
# ЧТО ОН СПРАШИВАЕТ. Массовое изменение (переезд контракта, переименование,
# пересборка раскладки) собирается инструментом на КАКОЙ-ТО базе и коммитится
# целиком. Если база отстала, изменение молча возвращает дерево к ней —
# и вместе с ним снимает всю работу, посаженную между базой и им самим.
# Вопрос скрипта ровно один: КАКУЮ ПОСАЖЕННУЮ РАБОТУ СНЯЛО ЭТО ИЗМЕНЕНИЕ.
#
# ПОЧЕМУ НЕ СЧЁТ ПОТЕРЯННЫХ СТРОК. Он уже был построен и оказался недостаточен
# по двум осям сразу, и обе измерены:
#   * он привязывал потерю к НЕПОСРЕДСТВЕННОМУ предшественнику файла, поэтому
#     работа, поверх которой успел лечь кто-то третий, ему не видна. Замер:
#     у `a30639d5a2` в коммите 39 файлов, перепись строк назвала 29;
#     у `765dddbdef` — 4 файла против названного одного;
#   * перезапись, ИЗМЕНИВШАЯ строку, а не удалившая её, под него не подпадала.
#
# ЧТО ОН СЧИТАЕТ ВМЕСТО СТРОК — ИМЕНА. Всякая посаженная работа заводит
# ИМЕНОВАННЫЕ сущности: объявление Go, функцию оболочки, определение Python,
# цель сборки, ручку окружения, подпробу, поле структуры, файл. Имя — это то,
# чем работа предъявляет себя дереву, и снятие работы снимает её имена.
# Имя ищется ПО ВСЕМУ ДЕРЕВУ, а не в своём файле, поэтому переезд координаты
# потерей НЕ является: перенесённая функция остаётся найденной.
#
# ЧЕМ ЭТО НЕ ЯВЛЯЕТСЯ. Это не рукописный перечень предшественников: перечень
# ВЫВОДИТСЯ из пары «изменение и его родитель», и второго места об одном
# предмете не заводится.
#
# ОБЪЯВЛЕННОЕ ПРЕОБРАЗОВАНИЕ. Массовое изменение вправе переименовывать —
# ради этого оно и делается. Поэтому имя, чья НОРМАЛИЗОВАННАЯ форма жива после
# изменения, потерей не считается: `--rename kacho=kaname` объявляет, что
# именно изменение имело право сделать. Всё, что осталось после нормализации,
# изменение сняло СВЕРХ объявленного — и обязано быть адъюдицировано.
#
# ЧЕГО ОН НЕ ВИДИТ — НАЗВАНО ЧИСЛОМ, А НЕ ОГОВОРКОЙ.
#
#   1. РАБОТУ, НЕ ЗАВОДЯЩУЮ ИМЕНИ. Изменённое значение константы, поправленное
#      условие внутри существующей функции, добавленная запись в словарь — всё
#      это снимается бесследно для счёта имён. Замер на известном случае:
#      `de7684c553` потерял 210 строк словаря направления, а имён — ОДНО
#      (`neg_case`). Предшественник назван верно, величина потери занижена
#      на два порядка. Предикат отвечает «КТО пострадал», а не «НАСКОЛЬКО».
#
#      ХУЖЕ ТОГО, предшественник бывает НЕ НАЗВАН ВОВСЕ. Работа, не заводящая
#      ни одного имени, невидима by construction: построчный разбор — другая
#      единица счёта — нашёл в том же окне ЧЕТВЕРЫХ, кого именной счёт не
#      назвал, и среди них `534996d979` (32 строки прозы, имён ноль). Часть
#      его работы восстановлена попутно, часть — нет. Отсюда предел, который
#      обязан звучать в отчёте дословно: полнота ведомости, доказанная этим
#      прогоном, есть ПОЛНОТА ПО СЧЁТУ ИМЁН, а не полнота вообще.
#
#   2. ВЫПОТРОШЕННОЕ ТЕЛО ПРИ ЖИВОМ ИМЕНИ. Сверяется присутствие объявления,
#      а не его содержание: функция, оставшаяся в дереве с откаченным телом,
#      здесь молчит. Эта ось закрывается счётом строк, и счёт строк — предмет
#      отдельный, со своими пределами (см. выше).
#
#   3. ЛОЖНЫЕ СРАБАТЫВАНИЯ НА НАМЕРЕННОМ РЕФАКТОРИНГЕ САМОГО ИЗМЕНЕНИЯ.
#      Изменение вправе снять имя осознанно, и от отката это формой неотличимо.
#      Замер на известном случае: 14 привязанных предшественников, из них
#      ОДИН ложный — `2171a6690a` с именем `ourPackagePrefix`, которое
#      изменение заменило функцией `isOurPackage` над объявленным множеством
#      корней. Это адъюдицируется человеком, поэтому скрипт — ПРИБОР ДЛЯ
#      РАЗБОРА, а не гейт, замыкающий отправку fail-closed.
#
#   4. ПОГЛОЩЕНИЕ НОРМАЛИЗАЦИЕЙ. Ключ `--rename` снимает законную правку
#      координат — и тем же движением способен спрятать честно снятое имя,
#      чья нормализованная форма живёт у изменения по СВОЕЙ причине. Предел
#      измеряется, а не оговаривается: подозрительным считается поглощение,
#      у которого нормализованная форма жила и ДО изменения, и это число
#      печатается переписью отдельной строкой. На известном случае оно равно
#      0 из 280 — то есть ни одно поглощение там не было спорным.
#
# КОНТРОЛЬ НА ПОПУЛЯЦИИ, ГДЕ ОТВЕТ ИЗВЕСТЕН — И ЧТО ОН ДОКАЗЫВАЕТ, А ЧТО НЕТ.
# «Ведомость полна» этот прогон НЕ доказывает; он доказывает, что она ПОЛНА ПО
# СЧЁТУ ИМЁН. Работа без имени остаётся вне его наблюдения (предел 1), и на
# известном случае такая работа в окне ЕСТЬ. На `d46aaa7280` предикат находит
# ВСЕ ДВЕНАДЦАТЬ предметов, известных по разбору задач #2166, #2170 и #2172, —
# включая тот девятый (`6fc1bcaeaa`), который в перепись строк не попал вовсе
# и нашёлся побочным замечанием чужой полосы. Он там не крайний, а ТРЕТИЙ
# по величине потери: 25 имён и 3 файла.
#
# ИСХОДОВ ЧЕТЫРЕ, И ОНИ РАЗНЫЕ:
#   0 — сверх объявленного преобразования изменение не сняло ни одного имени;
#   1 — НАХОДКА: сняты имена, перечень с привязкой к предшественникам;
#       СЮДА ЖЕ пустой обход — «о дереве не прочитано ничего» вердиктом не
#       является и молча за ноль находок не выдаётся;
#   2 — скрипт позван неверно;
#   3 — НЕ ВЫПОЛНИЛОСЬ: спросить не удалось (нет git, ревизия не разрешается).
#
# ПЕРЕПИСЬ ПЕЧАТАЕТСЯ ВСЕГДА. «Ноль находок» и «ноль прочитанного» обязаны
# быть различимы, и различает их только объём осмотренного.

import argparse
import collections
import re
import subprocess
import sys

# ── Извлечение имён: по одному разбору на язык ──────────────────────────────
# Каждый образец ловит ОБЪЯВЛЕНИЕ, а не употребление: предмет счёта — то, что
# работа ЗАВЕЛА, а не то, на что она сослалась.
EXTRACTORS = [
    # (маска путей, [(регулярное выражение, номер группы)])
    (["*.go"], [
        (re.compile(r"^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)"), 1),
        (re.compile(r"^type\s+([A-Za-z_]\w*)"), 1),
        (re.compile(r"^(?:const|var)\s+([A-Za-z_]\w*)"), 1),
        (re.compile(r"^\t([A-Z]\w*)\s+[\[\*A-Za-z]"), 1),          # поле структуры
        (re.compile(r"""t\.Run\(\s*["`]([^"`]{4,})["`]"""), 1),      # имя подпробы
    ]),
    (["*.sh", "*.bash"], [
        (re.compile(r"^\s*(?:function\s+)?([a-z_][a-z0-9_]{3,})\s*\(\)\s*\{"), 1),
        (re.compile(r"^\s*(?:readonly|declare -r)\s+([A-Za-z_]\w{3,})="), 1),
    ]),
    (["*.py"], [
        (re.compile(r"^\s*def\s+([A-Za-z_]\w*)"), 1),
        (re.compile(r"^\s*class\s+([A-Za-z_]\w*)"), 1),
    ]),
    (["Makefile", "*.mk"], [
        (re.compile(r"^([a-zA-Z0-9][a-zA-Z0-9_.-]{3,}):(?!=)"), 1),
    ]),
]

# Ручка окружения — именованная сущность в любом языке, включая чарты и
# объявления конвейера, поэтому её образец идёт по всему дереву отдельно.
KNOB_GLOBS = ["*.go", "*.sh", "*.py", "*.mk", "Makefile", "*.yaml", "*.yml",
              "*.tpl", "*.tf", "*.env", "Dockerfile*"]
KNOB = re.compile(r"\b((?:KACHO|KANAME)_[A-Z0-9_]{3,})\b")

MIN_LEN = 4  # односимвольные и короткие имена не различают предмет


def git(*args, check=True):
    """Вызов git. Отказ — это НЕ ВЫПОЛНИЛОСЬ, а не пустой ответ."""
    p = subprocess.run(("git",) + args, capture_output=True, text=True,
                       errors="replace")
    if check and p.returncode not in (0, 1):  # 1 у git grep = «не нашлось»
        raise RuntimeError(f"git {' '.join(args)}: {p.stderr.strip()}")
    return p.stdout


def _matches(path, globs):
    import fnmatch
    base = path.rsplit("/", 1)[-1]
    return any(fnmatch.fnmatch(base, g) for g in globs)


def names_at(rev, all_paths):
    """Множество объявленных имён во ВСЁМ дереве ревизии + объём осмотренного.

    Объём считается по ОДНОМУ перечню дерева, а не по маске каждого разбора:
    маска-путь у `ls-tree` и у `grep` ведут себя по-разному, и счёт по второй
    давал ноль при исправном разборе — то есть перепись противоречила бы
    собственному результату.
    """
    names = set()
    files_seen = set()
    for globs, patterns in EXTRACTORS:
        pathspec = [f":(glob,top)**/{g}" for g in globs] + \
                   [f":(glob,top){g}" for g in globs]
        out = git("grep", "-I", "-h", "-E", "--no-color",
                  "-e", r".", rev, "--", *pathspec, check=False)
        for line in out.split("\n"):
            for rx, grp in patterns:
                m = rx.search(line)
                if m:
                    n = m.group(grp)
                    if len(n) >= MIN_LEN:
                        names.add(n)
        files_seen.update(p for p in all_paths if _matches(p, globs))

    pathspec = [f":(glob,top)**/{g}" for g in KNOB_GLOBS] + \
               [f":(glob,top){g}" for g in KNOB_GLOBS]
    out = git("grep", "-I", "-h", "-o", "-E", "--no-color",
              "-e", KNOB.pattern.replace(r"\b", ""), rev, "--", *pathspec,
              check=False)
    for line in out.split("\n"):
        for m in KNOB.finditer(line):
            names.add(m.group(1))
    files_seen.update(p for p in all_paths if _matches(p, KNOB_GLOBS))
    return names, len(files_seen)


def paths_at(rev):
    return set(f for f in git("ls-tree", "-r", "--name-only", rev).split("\n") if f)


def normalize(name, renames):
    for src, dst in renames:
        name = name.replace(src, dst)
        name = name.replace(src.upper(), dst.upper())
        name = name.replace(src.capitalize(), dst.capitalize())
    return name


DECL_HINT = [rx for _, pats in EXTRACTORS for rx, _ in pats]


def declaration_sites(name, rev):
    """Файл и строка, где имя ОБЪЯВЛЕНО в этой ревизии.

    Употребление от объявления отличается тем же разбором, которым имя и
    добывалось: иначе привязка досталась бы всякому, кто на имя лишь сослался.
    """
    out = git("grep", "-n", "-w", "-F", "-e", name, rev, check=False)
    sites = []
    for line in out.split("\n"):
        # формат: <rev>:<путь>:<строка>:<содержимое>
        parts = line.split(":", 3)
        if len(parts) < 4:
            continue
        _, path, lineno, body = parts
        if not lineno.isdigit():
            continue
        if any(rx.search(body) for rx in DECL_HINT) or KNOB.search(body):
            sites.append((path, int(lineno)))
    return sites


def adders(token, rev_parent, is_path):
    """ВСЕ коммиты, написавшие снятое, — по разбору происхождения строки.

    ПОЧЕМУ НЕ `git log -S`. Он называет всякий коммит, у которого ЧИСЛО вхождений
    имени изменилось, — то есть и того, кто имя снял, и того, кто мимоходом
    тронул соседнюю строку. Замер на известном случае: такая привязка дала
    длинный хвост коммитов месячной давности по одному имени в каждом, и
    настоящие предшественники в нём тонули.

    ПОЧЕМУ НЕ ОДИН КОММИТ. Привязка к последнему — та самая ошибка, из-за
    которой прежняя перепись занижала: `KANAME_TREE_ROOT` заводят и
    `de7ed0589d`, и `9886c7d1d0`; привязка к последнему прятала первого целиком.
    Разбор происхождения КАЖДОГО места объявления возвращает обоих.
    """
    if is_path:
        out = git("log", "--format=%H", "--diff-filter=A", "-1",
                  rev_parent, "--", token, check=False)
        return [h for h in out.split("\n") if h]
    seen = []
    for path, lineno in declaration_sites(token, rev_parent):
        out = git("blame", "-l", f"-L{lineno},{lineno}", "--porcelain",
                  rev_parent, "--", path, check=False)
        head = out.split("\n", 1)[0].split(" ")[0] if out else ""
        if len(head) == 40 and head not in seen:
            seen.append(head)
    return seen


def main():
    ap = argparse.ArgumentParser(
        description="какую посаженную работу сняло массовое изменение",
        epilog="исходы: 0 чисто · 1 находка (и пустой обход) · 2 позван неверно "
               "· 3 не выполнилось")
    ap.add_argument("rev", help="массовое изменение (ревизия)")
    ap.add_argument("--rename", action="append", default=[], metavar="СТАРОЕ=НОВОЕ",
                    help="объявленное преобразование; имя, чья нормализованная "
                         "форма жива после изменения, потерей не считается")
    ap.add_argument("--since", default=None, metavar="РЕВИЗИЯ",
                    help="начало ОКНА РИСКА — работа, посаженная после этой "
                         "ревизии, помечается как возможный непреднамеренный "
                         "откат (умолчание: rev~400). Привязка от окна НЕ "
                         "зависит: она идёт разбором происхождения строки")
    ap.add_argument("--quiet-names", action="store_true",
                    help="печатать только перечень предшественников")
    args = ap.parse_args()

    renames = []
    for r in args.rename:
        if "=" not in r:
            print(f"ошибка: --rename ждёт СТАРОЕ=НОВОЕ, получено {r!r}",
                  file=sys.stderr)
            return 2
        src, dst = r.split("=", 1)
        renames.append((src, dst))

    try:
        rev = git("rev-parse", "--verify", f"{args.rev}^{{commit}}").strip()
        parent = git("rev-parse", "--verify", f"{rev}^").strip()
        # Умолчание окна риска — 400 коммитов назад, но КОРОТКАЯ ИСТОРИЯ
        # это не «спросить не удалось»: у дерева младше окна началом служит
        # его корень. Прежняя редакция роняла здесь код 3, и пустой обход
        # (контроль C доказательства) до своей ветки не доходил ВОВСЕ —
        # то есть отказ разрешения ревизии выдавал себя за отсутствие предмета.
        if args.since:
            since = git("rev-parse", "--verify", f"{args.since}^{{commit}}").strip()
        else:
            since = subprocess.run(
                ("git", "rev-parse", "--verify", f"{rev}~400"),
                capture_output=True, text=True).stdout.strip()
            if not since:
                roots = git("rev-list", "--max-parents=0", rev).split()
                since = roots[-1] if roots else parent
    except Exception as e:                                   # noqa: BLE001
        print(f"НЕ ВЫПОЛНИЛОСЬ: ревизия не разрешается — {e}", file=sys.stderr)
        return 3

    subject = git("log", "-1", "--format=%h %ci %s", rev).strip()
    print(f"изменение: {subject}")
    print(f"родитель:  {git('log', '-1', '--format=%h %ci %s', parent).strip()}")

    try:
        paths_parent, paths_rev = paths_at(parent), paths_at(rev)
        n_parent, files_parent = names_at(parent, paths_parent)
        n_rev, files_rev = names_at(rev, paths_rev)
    except Exception as e:                                   # noqa: BLE001
        print(f"НЕ ВЫПОЛНИЛОСЬ: дерево не зачитывается — {e}", file=sys.stderr)
        return 3

    # ── Пустой обход — отказ, а не ноль находок ─────────────────────────────
    if not n_parent or not files_parent:
        print(f"перепись: файлов у родителя {files_parent}, имён {len(n_parent)}")
        print("НАХОДКА: обход пуст — о дереве не прочитано ничего, "
              "вердикт беспредметен", file=sys.stderr)
        return 1

    lost_names = {n for n in (n_parent - n_rev)
                  if normalize(n, renames) not in n_rev}
    lost_paths = {p for p in (paths_parent - paths_rev)
                  if normalize(p, renames) not in paths_rev}

    print(f"перепись: файлов у родителя {files_parent} → у изменения {files_rev}; "
          f"имён {len(n_parent)} → {len(n_rev)}")
    absorbed = (n_parent - n_rev) - lost_names
    # ПОГЛОЩЕНИЕ, КОТОРОЕ НАДО ВИДЕТЬ. Нормализация снимает законную правку
    # координат — и ровно тем же движением способна СПРЯТАТЬ честно снятое имя,
    # чья нормализованная форма живёт у изменения по своей причине. Признак
    # подозрения: эта форма жила и у РОДИТЕЛЯ, то есть её присутствие после
    # изменения следом переименования данного имени не является.
    absorbed_suspect = {n for n in absorbed if normalize(n, renames) in n_parent}
    print(f"перепись: снято имён {len(n_parent - n_rev)}, из них объявленным "
          f"преобразованием объяснено {len(absorbed)}, "
          f"осталось {len(lost_names)}")
    print(f"перепись: поглощений нормализацией {len(absorbed)}, из них "
          f"подозрительных {len(absorbed_suspect)} "
          f"(нормализованная форма жила и до изменения)")
    print(f"перепись: снято файлов {len(paths_parent - paths_rev)}, "
          f"осталось после нормализации {len(lost_paths)}")
    print(f"перепись: окно риска {since[:10]}..{parent[:10]}")

    if not lost_names and not lost_paths:
        print("✓ сверх объявленного преобразования изменение не сняло ничего")
        return 0

    # ── Привязка: кто завёл снятое ─────────────────────────────────────────
    by_commit = collections.defaultdict(lambda: {"names": [], "paths": []})
    orphan = {"names": [], "paths": []}
    for kind, tokens in (("names", sorted(lost_names)), ("paths", sorted(lost_paths))):
        for tok in tokens:
            hits = adders(tok, parent, is_path=(kind == "paths"))
            if not hits:
                orphan[kind].append(tok)
            for h in hits:
                by_commit[h][kind].append(tok)

    print(f"\nНАХОДКА: изменение сняло {len(lost_names)} имён и "
          f"{len(lost_paths)} файлов сверх объявленного преобразования.")
    print(f"предшественников затронуто: {len(by_commit)}\n")

    window = set()
    try:
        window = {h for h in git("log", "--format=%H",
                                 f"{since}..{parent}").split("\n") if h}
    except Exception:                                        # noqa: BLE001
        pass

    rows = sorted(by_commit.items(),
                  key=lambda kv: -(len(kv[1]["names"]) + len(kv[1]["paths"])))
    in_window = sum(1 for h, _ in rows if h in window)
    print(f"из них внутри окна риска: {in_window} "
          f"(работа, посаженная между началом окна и изменением)\n")
    for h, d in rows:
        meta = git("log", "-1", "--format=%h %ci %s", h).strip()
        mark = "◆" if h in window else " "
        print(f"  {mark} {meta}")
        print(f"      снято имён {len(d['names']):3d}, файлов {len(d['paths']):2d}")
        if not args.quiet_names:
            for n in d["names"][:8]:
                print(f"        · {n}")
            if len(d["names"]) > 8:
                print(f"        · … ещё {len(d['names']) - 8}")
            for p in d["paths"][:4]:
                print(f"        · {p}")
            if len(d["paths"]) > 4:
                print(f"        · … ещё {len(d['paths']) - 4} файлов")
        print()

    if orphan["names"] or orphan["paths"]:
        print(f"  происхождение не установлено: имён {len(orphan['names'])}, "
              f"файлов {len(orphan['paths'])} — объявление не разобрано либо "
              f"строка не разрешается в коммит")
    print("\n◆ — предшественник внутри окна риска: его работа посажена после "
          "начала окна.\n    Метка утверждает ТОЛЬКО это. Намеренное снятие от "
          "непреднамеренного она\n    не отличает: на двух чужих массовых "
          "изменениях ею помечены 15 и 5\n    заведомо намеренных снятий. "
          "Различает — адъюдикация, а не метка.")
    return 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print("НЕ ВЫПОЛНИЛОСЬ: прервано", file=sys.stderr)
        sys.exit(3)
