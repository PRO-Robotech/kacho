#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# assert-fixture-role-verbs-exist.py — роль, которую заводит ФИКСТУРА, обязана быть
# построена из глаголов, которые модель прав действительно объявляет.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Сегмент ГЛАГОЛА в Role.Create словарём НЕ закрыт (закрыты модуль и ресурс, см.
# services/iam/internal/apps/kaname/api/role/rules_catalog.go). Поэтому роль с
# несуществующим глаголом создаётся успешно — 200, — а реконсайлер на эмиссии
# отношений такой глагол ПРОПУСКАЕТ (`domain.IsVerbOfType` → continue в
# access_binding/reconcile/tuples.go). Отношения `v_*` за ним не появляется
# никогда, молча и без сигнала автору роли.
#
# Для фикстуры это означает конкретный сорт лжи: положительная половина кейса
# («этот субъект МОЖЕТ») не может пройти ни при каком исправном продукте, потому
# что права не материализовались вовсе. Красным это становится не сразу, а если
# субъекта вообще кто-то использует; роль, которую не использует никто, не даёт
# даже этого — она просто лежит и выглядит осмысленной.
#
# ПОЧЕМУ ГЕЙТ, А НЕ ПРАВКА. Класс уже случался дважды подряд в ОДНОМ операторе:
# соседние строки заводили две роли, одну починили (`update` вместо
# `addTargets`/`removeTargets`), у второй те же дважды-снятые `start`/`stop`
# остались — потому что её предъявителя не использовал никто и краснеть было
# нечему. Починка распространяется ровно настолько, насколько хватает её гейта;
# без гейта восьмое такое определение приедет тем же способом.
#
# ─────────────────────────────────────────────────────────────────────────────
# ИСТОЧНИК ИСТИНЫ — МОДЕЛЬ, А НЕ ВТОРОЙ СПИСОК
#
# Набор глаголов читается из канонической модели прав
# (proto/kaname/cloud/iam/v1/fga_model.fga) — из отношений с приставкой `v_`. Ровно
# та же сторона, которую сторожит гейт дрейфа iam и internal/repohygiene/
# verbvocabulary_test.go. Держать здесь рукописную копию словаря значило бы завести
# ту самую вторую таблицу, которая молча разъедется с первой.
#
# ГЛАГОЛ СВЕРЯЕТСЯ С НАБОРОМ СВОЕГО ТИПА. Здесь стояла предпосылка «сегодня все
# глагольные типы объявляют один и тот же набор, поэтому пару (модуль, ресурс) можно
# не разрешать», и обещание отказаться работать, когда она отпадёт. Предпосылка
# ОТПАЛА (`registry_registry` объявляет `v_create`, остальные 23 типа — нет), и
# правильным ответом оказался не отказ, а уточнение: пара резолвится закрытой
# таблицей прод-кода в тип, глагол ищется в наборе ИМЕННО ЭТОГО типа, а объединение
# осталось запасным путём для пары, которую разрешить не удалось (такая пара
# печатается отдельно — «не смог разрешить» никогда не равно «законно»).
#
# ОДНО ЗАЯВЛЕННОЕ ОСЛАБЛЕНИЕ — глагол, названный РАДИ ЯРУСА. Ярусный кортеж
# выводится из КЛАССА глагола и не зависит от набора типа, поэтому write-глагол вне
# набора — единственный способ построить предъявителя «ярус записи есть, пообъектных
# прав по нему нет». Такое место обязано быть названо поимённо с причиной в
# VERB_FOR_TIER_ONLY; запись, которой больше нечего извинять, — находка. Подробно —
# у самой таблицы.
#
# РАЗБОР — AST, А НЕ ТЕКСТ. Предмет — литерал в вызове/словаре, а не слово в файле.
# Поиск по тексту нашёл бы тот же глагол в комментарии, который его объясняет
# (в разобранных здесь фикстурах такие комментарии есть), и краснел бы на прозе.
#
# Usage:  python3 deploy/scripts/assert-fixture-role-verbs-exist.py
#         python3 deploy/scripts/assert-fixture-role-verbs-exist.py --self-test
#         make -C deploy fixture-role-verbs

import ast
import os
import re
import subprocess
import shutil
import sys
import tempfile

MODEL_REL = "proto/kaname/cloud/iam/v1/fga_model.fga"

# Где живут фикстуры, заводящие роли. Предикат — МЕСТО: посев матрицы прав и
# декларативные кейсы newman. Прод-код и миграции сюда НЕ входят намеренно —
# у 58 системных ролей другая таксономия (их авторитет несут permissions[]+ярусы,
# см. rules_catalog.go systemCtx), и мерить их этим словарём было бы неверно.
FIXTURE_GLOBS = ["tests/authz-fixtures/*.py", "services/*/tests/newman/cases/*.py"]

VERB_WILDCARD = "*"

# Закрытая таблица (module.resource) → тип модели прав. Один источник с прод-кодом:
# гейт читает ТУ ЖЕ карту, по которой строит объекты реконсайлер, поэтому «глагол
# законен» решается набором ЕГО типа, а не общим словарём.
#
# ПРЕДМЕТ — ПАКЕТ, А НЕ ФАЙЛ (задача продукта #1944). Прежде здесь стоял путь к
# `fga_types.go`. Таблица стала ПОРОЖДАЕМОЙ из манифестов модулей (#1092) и
# уехала в `tables_gen.go`; гейт остался на прежнем имени и начал отвечать
# «FATAL: не прочитана закрытая таблица типов» — то есть третьей категорией
# («не выполнилось»), поданной как отказ. Пакет — единица области видимости Go:
# package-level имя в нём ровно одно by construction, поэтому перенос объявления
# между файлами пакета больше ничего здесь не ломает.
TYPES_PKG_REL = "services/iam/internal/authzmap"
TYPES_VAR = "objectTypes"


def repo_root():
    return os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))


# Открывающая часть объявления карты. Тело берётся БАЛАНСОМ СКОБОК, а не
# образцом «до перевода строки и закрывающей»: однострочная запись
# `var X = map[string]string{"a": "b"}` — законная форма Go, и образец,
# знающий лишь многострочную, её не находит. Распознаватель, не знающий одной
# из законных форм, не даёт ни красного, ни зелёного — он МОЛЧИТ, и записанное
# в неизвестной ему форме оказывается вне наблюдения
# (`testing.md` §«Гейт на класс», п. 7). Разбор Go-стороны этого же предмета
# (`internal/repohygiene/pkgvardecl.go`) идёт по узлам и обе формы знает: два
# читателя одного предмета обязаны сходиться по форме, иначе они разойдутся
# молча.
#
# Начало строки (`^` при re.M) обязательно и несёт СМЫСЛ: package-level `var` в
# gofmt-дереве стоит в нулевой колонке, а закомментированное объявление
# начинается с `//`. Без якоря распознаватель находит объявление в ПРОЗЕ,
# объясняющей его переезд, — краснеет на собственном объяснении. Слепая зона
# названа: объявление внутри группы `var ( … )` идёт с отступом и здесь не
# распознаётся; исход такого — ОТКАЗ с объёмом прочитанного, а не молчание.
_MAP_HEAD = r"^var\s+{}\s*=\s*map\[string\]string\s*\{{"


def _map_literal_body(src, var_name):
    """Тело литерала карты var_name, либо None. Балансом скобок."""
    m = re.search(_MAP_HEAD.format(re.escape(var_name)), src, re.M)
    if not m:
        return None
    depth, i = 1, m.end()
    while i < len(src) and depth:
        c = src[i]
        if c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
        i += 1
    if depth:
        return None
    return src[m.end():i - 1]


def object_types(pkg_dir, var_name=TYPES_VAR):
    """({"module.resource": "тип_модели"}, файл, сколько файлов пакета прочитано).

    ПОЧЕМУ ИЗ КОДА, А НЕ СПИСКОМ ЗДЕСЬ. Копия таблицы в гейте разъехалась бы с
    прод-кодом молча: новый ресурс появился бы у реконсайлера и не появился бы
    у проверки, и та начала бы считать законный глагол несуществующим.

    ПОЧЕМУ ПО ПАКЕТУ, А НЕ ПО ФАЙЛУ — см. TYPES_PKG_REL.

    Тестовые файлы не читаются: синтетика проб держит собственные литералы того
    же имени, и счёт их сделал бы предмет функцией числа проб.

    Три отказа, и все три — отказы, а не пустой словарь: пакета нет · объявления
    нет · объявлений больше одного (два места об одном предмете). Возвращается
    (None, причина, сколько прочитано), и вызывающий печатает причину — «не
    найдено» обязано быть отличимо от «не читано».
    """
    try:
        names = sorted(n for n in os.listdir(pkg_dir)
                       if n.endswith(".go") and not n.endswith("_test.go"))
    except OSError:
        return None, f"каталога пакета {pkg_dir} нет", 0
    if not names:
        return None, f"в пакете {pkg_dir} нет не-тестовых файлов Go", 0
    found = []
    for name in names:
        try:
            src = open(os.path.join(pkg_dir, name), encoding="utf-8").read()
        except OSError:
            continue
        body = _map_literal_body(src, var_name)
        if body is not None:
            found.append((name, body))
    if not found:
        return None, (f"в пакете {pkg_dir} нет объявления {var_name} "
                      f"(не-тестовых файлов прочитано {len(names)})"), len(names)
    if len(found) > 1:
        return None, (f"в пакете {pkg_dir} объявление {var_name} встречено "
                      f"{len(found)} раза ({', '.join(n for n, _ in found)}) — "
                      "два места об одном предмете"), len(names)
    decl_file, body = found[0]
    out = {}
    for k, v in re.findall(r'"([^"]+)"\s*:\s*"([^"]+)"', body):
        out[k] = v
    if not out:
        return None, (f"объявление {var_name} в {decl_file} не дало ни одной пары"), len(names)
    return out, decl_file, len(names)


def model_verb_sets(model_path):
    """{тип: {глаголы}} — только глагольные типы, из канонической модели."""
    cur, types = None, {}
    with open(model_path, encoding="utf-8") as fh:
        for line in fh:
            m = re.match(r"^type\s+(\S+)\s*$", line)
            if m:
                cur = m.group(1)
                types[cur] = set()
                continue
            m = re.match(r"^\s+define\s+(v_[A-Za-z0-9_]+)\s*:", line)
            if m and cur:
                types[cur].add(m.group(1)[len("v_"):])
    return {t: v for t, v in types.items() if v}


def _str_list(node):
    """Литерал списка строк -> list; иначе None (не разрешается статически)."""
    if not isinstance(node, (ast.List, ast.Tuple)):
        return None
    out = []
    for el in node.elts:
        if isinstance(el, ast.Constant) and isinstance(el.value, str):
            out.append(el.value)
        else:
            return None
    return out


def role_rule_verbs(path):
    """Все определения правил роли в файле.

    Возвращает (кортежи, число_неразрешимых). Кортеж — (строка, форма, глаголы).
    Две формы, обе реальны в дереве:
      A. словарь-литерал с ключом "verbs" — тело запроса Role.Create;
      B. вызов custom_role(account, name, module, resources, verbs) — посев матрицы,
         где глаголы приезжают ПОЗИЦИОННЫМ аргументом и ключа "verbs" рядом нет.
    Форма B добавлена не впрок: единственное живое определение этого класса на
    момент написания гейта было именно ею, и предикат «по ключу verbs» его не видел.
    """
    with open(path, encoding="utf-8") as fh:
        tree = ast.parse(fh.read(), path)
    found, unresolved = [], 0
    for node in ast.walk(tree):
        if isinstance(node, ast.Dict):
            keys = {k.value for k in node.keys
                    if isinstance(k, ast.Constant) and isinstance(k.value, str)}
            if "verbs" in keys:
                slot = dict(zip([k.value if isinstance(k, ast.Constant) else None
                                 for k in node.keys], node.values))
                verbs = _str_list(slot.get("verbs"))
                mod = slot.get("module")
                mod = mod.value if isinstance(mod, ast.Constant) and isinstance(mod.value, str) else None
                res = _str_list(slot.get("resources"))
                if verbs is None:
                    unresolved += 1
                else:
                    found.append((node.lineno, "rules[]", verbs, mod, res))
        if (isinstance(node, ast.Call) and isinstance(node.func, ast.Name)
                and node.func.id == "custom_role" and len(node.args) >= 5):
            verbs = _str_list(node.args[4])
            a2 = node.args[2]
            mod = a2.value if isinstance(a2, ast.Constant) and isinstance(a2.value, str) else None
            res = _str_list(node.args[3])
            if verbs is None:
                unresolved += 1
            else:
                found.append((node.lineno, "custom_role()", verbs, mod, res))
    return found, unresolved


def list_fixture_files(root):
    """Состав — из индекса git: то же множество, что увидит CI на свежем checkout'е."""
    try:
        out = subprocess.run(["git", "-C", root, "ls-files", "-z", *FIXTURE_GLOBS],
                             capture_output=True, text=True, check=True).stdout
        return sorted(p for p in out.split("\0") if p)
    except (subprocess.CalledProcessError, FileNotFoundError):
        return None


# ─────────────────────────────────────────────────────────────────────────────
# ГЛАГОЛ, НАЗВАННЫЙ РАДИ ЯРУСА, А НЕ РАДИ ПООБЪЕКТНОГО КОРТЕЖА
#
# Реконсайлер эмитит на объект ДВЕ вещи: `v_*` за каждый глагол, который ТИП
# объявляет, и ЯРУСНЫЙ кортеж, выведенный из КЛАССА глаголов правила
# (domain.ResolveVerbsAndTier → verbBackCompatTier: get/list → viewer,
# create/update → editor, delete → admin). Класс читается с АВТОРСКОГО глагола и
# от набора типа не зависит вовсе.
#
# Отсюда конструкция, которая выглядит как дефект и им НЕ является: правило
# называет write-глагол, которого тип не объявляет, — и получает ярус записи БЕЗ
# пообъектного кортежа за него. Для пробы «выдача не шире названного» это не
# случайность, а единственный способ построить предъявителя: любой ОБЪЯВЛЕННЫЙ
# write-глагол материализовал бы свой `v_*` и уничтожил бы отрицательную половину.
#
# Поэтому исключение — ЯВНОЕ, поимённое и с причиной. Оно НЕ ослабляет гейт по
# умолчанию: всякий неназванный здесь глагол вне набора типа остаётся находкой.
# И оно ИСТЕКАЕТ САМО: запись, которой больше нечего извинять (глагол убран, тип
# начал его объявлять, фикстура переехала), — находка, потому что иначе она
# достанется следующему как слепая зона.
#
# Ключ — (файл, "module.resource", глагол): без ресурса запись извиняла бы глагол
# у всех типов сразу, а номер строки дрейфует от любой правки выше.
VERB_FOR_TIER_ONLY = {
    ("tests/authz-fixtures/prodseed_matrix.py", "storage.volumes", "create"):
        "Предъявитель разреза «глагол, а не ярус» (storage AUTHZ-VOL-VERB-CUT-NOT-TIER): "
        "роли нужен ЯРУС ЗАПИСИ на объекте при единственном пообъектном глаголе `v_list`. "
        "`update` вместо `create` эмитил бы `v_update` и обнулил бы отрицательную половину "
        "(«правку и удаление не открывает»), а без write-глагола ярус стал бы viewer — и "
        "проба перестала бы проверять то, ради чего заведена: что object-self RPC гейтятся "
        "ГЛАГОЛОМ, а не ярусом.",
}


def scan(root, files, verb_sets, types_map, fallback, excused=None):
    """Сверка глагола с набором ЕГО типа.

    Пара (module, resource) резолвится закрытой таблицей прод-кода в тип модели, и
    глагол ищется в наборе именно этого типа. Прежняя редакция сверяла с ОБЩИМ
    словарём и держалась на предпосылке «у всех типов один набор»; предпосылка
    отпала, как только у одного типа появились свои глаголы. Общий словарь пропускал
    бы глагол, законный у соседа и несуществующий здесь.

    Нерезолвимая пара (нет module/resources рядом либо пары нет в таблице) НЕ
    молчит: она считается отдельно и печатается — «не смог разрешить» никогда не
    равно «законно».

    excused — карта явных исключений (см. VERB_FOR_TIER_ONLY). Возвращается
    множество ИСПОЛЬЗОВАННЫХ ключей: запись, которая никого не извинила, обязана
    стать находкой у вызывающего.
    """
    # Приведение авторского глагола — ТО ЖЕ, что делает прод-код перед выводом имени
    # отношения (services/iam/internal/domain/rule_verbs.go::NormalizeVerb: обрезка
    # пробелов + складывание регистра). Без него camelCase-глагол фикстуры не совпал
    # бы с приведённым именем в модели, и гейт объявил бы находкой законную запись.
    norm = lambda v: v.strip().lower()
    excused = excused or {}
    findings, triples, unresolved, unmapped, used = [], 0, 0, [], set()

    def split_excused(rel, pair, absent):
        """Разделяет отсутствующие глаголы на извинённые и оставшиеся находкой."""
        rest = []
        for v in absent:
            key = (rel, pair, norm(v))
            if key in excused:
                used.add(key)
            else:
                rest.append(v)
        return rest

    for rel in files:
        got, unres = role_rule_verbs(os.path.join(root, rel))
        unresolved += unres
        for lineno, shape, verbs, mod, res in got:
            triples += 1
            targets = []
            if mod and res:
                for r in res:
                    t = types_map.get(mod + "." + r) if types_map else None
                    if t and t in verb_sets:
                        targets.append((r, verb_sets[t]))
                    else:
                        unmapped.append((rel, lineno, mod + "." + r))
            if not targets:
                # пары нет — судим по объединению, но факт неразрешённости уже записан
                absent = [v for v in verbs if v != VERB_WILDCARD and norm(v) not in fallback]
                if absent:
                    findings.append((rel, lineno, shape, verbs, absent))
                continue
            for r, allowed in targets:
                absent = [v for v in verbs if v != VERB_WILDCARD and norm(v) not in allowed]
                absent = split_excused(rel, (mod or "?") + "." + r, absent)
                if absent:
                    findings.append((rel, lineno, shape + " → " + r, verbs, absent))
    return findings, triples, unresolved, unmapped, used


def run(root, files, verb_sets, label="дерево", excused=None):
    """Общий проход. Возвращает код возврата.

    excused — карта явных исключений; по умолчанию боевая VERB_FOR_TIER_ONLY.
    Параметром она приходит только из самопроверки, которой нужно предъявить
    гейту запись БЕЗ предмета и увидеть, что он на ней краснеет.
    """
    excused = VERB_FOR_TIER_ONLY if excused is None else excused
    if not verb_sets:
        print(f"FATAL: в {MODEL_REL} не нашлось ни одного глагольного типа — "
              f"предпосылка гейта не выполняется, молчание ничего не доказывает.")
        return 2

    # ПРЕДПОСЫЛКИ ОБЩЕГО НАБОРА БОЛЬШЕ НЕТ, и это не деградация, а уточнение.
    # Пока у всех глагольных типов набор совпадал, «глагол существует» решалось без
    # разрешения пары. Как только у одного типа появились свои глаголы, общий словарь
    # стал пропускать глагол, законный у соседа и несуществующий здесь. Теперь глагол
    # сверяется с набором ЕГО типа, а объединение остаётся ТОЛЬКО запасным путём для
    # пары, которую не удалось разрешить, — и такая пара печатается отдельно.
    fallback = set()
    for v in verb_sets.values():
        fallback |= set(v)

    types_map, types_where, types_files = object_types(os.path.join(root, TYPES_PKG_REL))
    if not types_map:
        print(f"FATAL: не прочитана закрытая таблица типов: {types_where}. "
              "Разрешать пару (module, resource) нечем, и молчание ничего не доказывает.")
        return 2
    print(f"перепись источника: пакет {TYPES_PKG_REL}, не-тестовых файлов прочитано "
          f"{types_files}, объявление {TYPES_VAR} в {types_where}, пар {len(types_map)}")

    if files is None:
        print("FATAL: состав фикстур не читается (git ls-files не отработал). "
              "«Нечего проверять» — не «всё хорошо».")
        return 2
    if not files:
        print(f"FAIL: под {FIXTURE_GLOBS} не нашлось НИ ОДНОГО файла. "
              "Ноль находок на нуле прочитанного — это провал, а не чистота.")
        return 1

    findings, triples, unresolved, unmapped, used = scan(
        root, files, verb_sets, types_map, fallback, excused)

    # Перепись — ОТДЕЛЬНОЕ утверждение от вердикта: «0 находок» обязано быть
    # отличимо от «0 прочитанного». Извинённые глаголы считаются ОТДЕЛЬНО: тихое
    # исключение неотличимо от отсутствия предмета.
    print(f"assert-fixture-role-verbs-exist: {label} — прочитано файлов: {len(files)}; "
          f"определений правил роли: {triples}; глаголы не разрешились статически: "
          f"{unresolved}; глагольных типов модели: {len(verb_sets)}; пар в закрытой "
          f"таблице: {len(types_map)}; пар не разрешилось в тип: {len(unmapped)}; "
          f"глаголов извинено как ярусные: {len(used)} из {len(excused)} "
          f"объявленных")
    for key in sorted(used):
        print(f"  извинён (ярус без пообъектного кортежа): {key[0]} [{key[1]}] {key[2]!r}")

    if unmapped:
        # «Не разрешил» — отдельный исход, а не тишина: по нерезолвимой паре глагол
        # сверяется объединением, то есть строже общего словаря не становится.
        seen = sorted({u[2] for u in unmapped})
        print(f"  пары вне закрытой таблицы ({len(seen)}): {seen[:12]}"
              + (" …" if len(seen) > 12 else ""))

    rc = 0

    if findings:
        print(f"\nFAIL: {len(findings)} определен(ий) роли построено из глагола, "
              f"которого модель прав не объявляет ДЛЯ ЭТОГО ТИПА.")
        print("Роль создаётся успешно (сегмент глагола в Role.Create не закрыт "
              "словарём), но пообъектного `v_*` за таким глаголом реконсайлер не "
              "эмитит — молча. Держатель получает по нему НИЧЕГО, и кейс,\n"
              "утверждающий «этот субъект может <глагол>», не может пройти ни при "
              "каком исправном продукте.\n"
              "Если глагол назван РАДИ ЯРУСА (класс глагола даёт ярусный кортеж "
              "независимо от набора типа) — это законно, но обязано быть заявлено "
              "поимённо в VERB_FOR_TIER_ONLY с причиной, а не подразумеваться.\n")
        for rel, lineno, shape, verbs, absent in findings:
            print(f"  {rel}:{lineno}  [{shape}] глаголы={verbs} → нет в модели: {absent}")
        rc = 1

    # ИСКЛЮЧЕНИЕ ЖИВЁТ, ПОКА У НЕГО ЕСТЬ ПРЕДМЕТ. Запись, которая никого не
    # извинила, — находка того же веса, что и незаявленный глагол: она освобождает
    # от гейта конструкцию, которой в дереве уже нет, и достанется следующему как
    # слепая зона. Печатается ВМЕСТЕ с находками выше, а не вместо них.
    stale = sorted(set(excused) - used)
    if stale:
        print(f"\nFAIL: {len(stale)} объявленн(ых) исключени(й) VERB_FOR_TIER_ONLY "
              f"больше нечего извинять — глагол убран, тип начал его объявлять либо "
              f"фикстура переехала. Снимите запись, иначе она станет слепой зоной:")
        for rel, pair, verb in stale:
            print(f"  {rel} [{pair}] {verb!r}")
        rc = 1

    if triples == 0:
        print("FAIL: файлы прочитаны, но определений правил роли в них НЕТ вовсе — "
              "разбор перестал доходить до предмета (сменилась форма вызова?).")
        rc = 1

    if rc == 0:
        print("OK: каждый глагол каждой роли фикстур объявлен моделью прав своего типа "
              "либо заявлен как ярусный с причиной.")
    return rc


# ── САМОПРОВЕРКА: доказательство инъекцией, в обе стороны ────────────────────
#
# Проверяется не «гейт запускается», а «гейт краснеет на внесённом дефекте и
# молчит на ЗАКОННОЙ конструкции той же формы». Без второй половины гейт ловил бы
# форму, а не существо, и первый же ложный срабат его снял бы.
def self_test():
    rc = 0
    root = repo_root()
    verb_sets = model_verb_sets(os.path.join(root, MODEL_REL))
    if not verb_sets:
        print("  ПРОВАЛ модель не читается — самопроверке не на чем стоять")
        return 1
    vocabulary = set.union(*verb_sets.values())
    # Глаголы берутся ИЗ МОДЕЛИ, а не вписываются литералом: инъекция обязана
    # оставаться настоящей, когда словарь однажды изменится.
    legit = sorted(vocabulary)[0]
    absent = "start"  # снят миграцией 0059 вместе с `stop`
    if absent in vocabulary:
        print(f"  ПРОВАЛ {absent!r} снова есть в модели — инъекция перестала быть "
              f"инъекцией, возьми другой снятый глагол")
        return 1

    print("=== инъекция: определение роли из несуществующего глагола ===")
    tmp = tempfile.mkdtemp()
    os.makedirs(os.path.join(tmp, "tests", "authz-fixtures"))
    # Синтетическое дерево несёт НАСТОЯЩУЮ закрытую таблицу пар. Без неё вызовы
    # `run()` ниже отвечали отказом «таблицы нет» (код 2), а самопроверка
    # засчитывала это за «пустой состав — провал» (ждала любой ненулевой): два
    # разных отказа под одним именем, то есть подпроверка проходила по причине,
    # которую не проверяла.
    os.makedirs(os.path.join(tmp, TYPES_PKG_REL), exist_ok=True)
    for _n in os.listdir(os.path.join(root, TYPES_PKG_REL)):
        if _n.endswith(".go") and not _n.endswith("_test.go"):
            shutil.copyfile(os.path.join(root, TYPES_PKG_REL, _n),
                            os.path.join(tmp, TYPES_PKG_REL, _n))

    def write(name, body):
        p = os.path.join(tmp, "tests", "authz-fixtures", name)
        with open(p, "w", encoding="utf-8") as fh:
            fh.write(body)
        return f"tests/authz-fixtures/{name}"

    # ДЕФЕКТ, обе формы — обязаны найтись, с координатой.
    bad_dict = write("bad_dict.py",
                     'BODY = {"module": "loadbalancer", "resources": ["networkLoadBalancers"],\n'
                     f'        "verbs": ["{absent}", "stop"]}}\n')
    bad_call = write("bad_call.py",
                     'def custom_role(a, n, m, r, v): pass\n'
                     'r = custom_role("acc", "nm", "loadbalancer", ["networkLoadBalancers"],\n'
                     f'                ["{absent}"])\n')
    # ЗАКОННЫЙ БЛИЗНЕЦ той же формы — обязан ПРОМОЛЧАТЬ. Без него «краснеет»
    # неотличимо от «краснеет на всём».
    good = write("good.py",
                 f'BODY = {{"module": "loadbalancer", "resources": ["targetGroups"],\n'
                 f'        "verbs": ["{legit}"]}}\n'
                 'def custom_role(a, n, m, r, v): pass\n'
                 f'r = custom_role("acc", "nm", "loadbalancer", ["targetGroups"], ["{legit}"])\n')
    # ПРОЗА, а не код: тот же глагол в комментарии и в строке. Обязан НЕ найтись —
    # иначе гейт читает текст, а не разбор, и краснеет на объяснении самого себя.
    prose = write("prose.py",
                  f'# исторически роль строилась из "{absent}" и "stop" — их больше нет\n'
                  f'DOC = "verbs: [\\"{absent}\\"] — так делать нельзя"\n')

    files = sorted([bad_dict, bad_call, good, prose])
    # Разбор зовётся ТОЙ ЖЕ парой аргументов, что и боевой проход: закрытая
    # таблица пар берётся из дерева, запасной словарь — объединение наборов.
    # Прежняя редакция звала `scan(tmp, files, vocabulary)` — сигнатуру, которой
    # у разбора нет с тех пор, как глагол стали сверять с набором СВОЕГО типа.
    # Самопроверка падала исключением ДО первой инъекции, то есть гейт не мог
    # доказать, что краснеет, — и его зелёный обычный проход ничего не значил.
    types_map, types_where, _ = object_types(os.path.join(root, TYPES_PKG_REL))
    if not types_map:
        print(f"  ПРОВАЛ закрытая таблица не читается ({types_where}) — "
              f"самопроверке не на чем стоять")
        return 1
    findings, triples, _unresolved, _unmapped, _used = scan(
        tmp, files, verb_sets, types_map, vocabulary)
    hit = {f[0] for f in findings}

    for want in (bad_dict, bad_call):
        if want in hit:
            print(f"  ОК  {want} найден: дефект назван координатой")
        else:
            print(f"  ПРОВАЛ {want} НЕ найден — гейт не краснеет на внесённом дефекте")
            rc = 1
    for wantnot, why in ((good, "законная конструкция той же формы"),
                         (prose, "слово есть, определения нет")):
        if wantnot in hit:
            print(f"  ПРОВАЛ {wantnot} принят за дефект — {why}")
            rc = 1
        else:
            print(f"  ОК  {wantnot} промолчал: {why}")

    # Координата обязана быть НОМЕРОМ СТРОКИ определения, а не именем файла: без
    # неё находка не приводит к правке.
    for rel, lineno, _shape, _v, _a in findings:
        if lineno <= 0:
            print(f"  ПРОВАЛ {rel}: находка без строки — координата не названа")
            rc = 1

    # Перепись обязана считать ЗАКОННЫЕ определения тоже, иначе «прочитано» врёт.
    if triples < 4:
        print(f"  ПРОВАЛ перепись насчитала {triples} определений, а их минимум 4 "
              f"(два дефектных + два законных) — разбор не доходит до предмета")
        rc = 1
    else:
        print(f"  ОК  перепись видит все формы: определений {triples}")

    # ── ИСТОЧНИК РАЗРЕШАЕТСЯ ПО ПАКЕТУ — обе стороны (задача продукта #1944) ──
    # Гейт уже ломался ровно здесь: таблица уехала в порождённый файл, а он ждал
    # прежнего имени и отвечал «не прочитана» — «не выполнилось», поданное как
    # отказ. Ниже — четыре прогона по одному синтетическому пакету: находит после
    # переезда · молчит на прозе · отказывает без объявления · отказывает на двух.
    pkgtmp = os.path.join(tmp, "pkgprobe")
    os.makedirs(pkgtmp, exist_ok=True)

    def _pkg_write(name, body):
        with open(os.path.join(pkgtmp, name), "w", encoding="utf-8") as fh:
            fh.write(body)

    def _pkg_rm(name):
        pth = os.path.join(pkgtmp, name)
        if os.path.exists(pth):
            os.remove(pth)

    # ПРОЗА в прежнем файле: имя и «объявление» стоят под комментарием. Читатель,
    # судящий по тексту без разбора формы, взял бы его за источник.
    _pkg_write("fga_types.go",
               "package authzmap\n\n"
               "// objectTypes переехал; строка ниже НЕ объявление.\n"
               '// var objectTypes = map[string]string{"ghost.one": "ghost_one"}\n')
    _pkg_write("tables_gen.go",
               "package authzmap\n\n"
               "var objectTypes = map[string]string{\n"
               '\t"alpha.one": "alpha_one",\n'
               '\t"beta.one":  "beta_one",\n'
               "}\n")
    # Тестовый файл того же имени НЕ читается: иначе объявлений стало бы два.
    _pkg_write("tables_gen_test.go",
               "package authzmap\n\n"
               'var objectTypes = map[string]string{"synthetic.one": "synthetic_one"}\n')

    m_moved, where_moved, files_moved = object_types(pkgtmp)
    if m_moved and where_moved == "tables_gen.go" and files_moved == 2 and len(m_moved) == 2:
        print("  ОК  объявление найдено после переезда внутрь пакета "
              f"(файлов {files_moved}, в {where_moved}, пар {len(m_moved)})")
    else:
        print(f"  ПРОВАЛ после переезда источник не разрешён: {where_moved}")
        rc = 1

    _pkg_rm("tables_gen.go")
    m_gone, where_gone, _f = object_types(pkgtmp)
    if m_gone is None and "прочитано" in where_gone:
        print(f"  ОК  без объявления — отказ с объёмом осмотренного: {where_gone}")
    else:
        print("  ПРОВАЛ проза принята за объявление либо отказ не назвал объём "
              f"прочитанного: {where_gone}")
        rc = 1

    _pkg_write("tables_gen.go",
               "package authzmap\n\n"
               'var objectTypes = map[string]string{"alpha.one": "alpha_one"}\n')
    _pkg_write("fga_types.go",
               "package authzmap\n\n"
               'var objectTypes = map[string]string{"gamma.one": "gamma_one"}\n')
    m_two, where_two, _f = object_types(pkgtmp)
    if m_two is None and "два места" in where_two:
        print("  ОК  два объявления одного имени — отказ, а не произвольное из них")
    else:
        print(f"  ПРОВАЛ два объявления приняты молча: {where_two}")
        rc = 1

    m_absent, where_absent, _f = object_types(os.path.join(pkgtmp, "nosuch"))
    if m_absent is None:
        print("  ОК  пакета нет — отказ, а не пустой словарь")
    else:
        print(f"  ПРОВАЛ несуществующий пакет дал словарь: {where_absent}")
        rc = 1

    # ПУСТОЙ СОСТАВ — провал, а не тишина.
    _empty_rc = run(tmp, [], verb_sets, label="самопроверка/пустой состав")
    if _empty_rc != 1:
        print(f"  ПРОВАЛ пустой состав фикстур дал код {_empty_rc}, а обязан 1 "
              f"(0 — принят за успех; 2 — отказ по ДРУГОЙ причине, то есть "
              f"подпроверка прошла не о том, о чём спрашивала)")
        rc = 1
    else:
        print("  ОК  пустой состав фикстур — провал, а не чистота")

    # Здесь стояла подпроверка «наборы типов разошлись ⇒ гейт обязан ОТКАЗАТЬСЯ
    # работать». Она описывала прежнюю редакцию, которая судила по ОБЩЕМУ словарю и
    # потому теряла право судить, как только у одного типа появлялся свой набор.
    # Сегодня гейт сверяет глагол с набором ЕГО типа, и расхождение наборов — не
    # аварийное условие, а обычное состояние дерева (`registry_registry` объявляет
    # `v_create`, остальные — нет).
    #
    # Подпроверка при этом НЕ краснела и не сообщала о себе: она стояла под условием
    # «все наборы совпадают», которое перестало выполняться, — то есть тихо
    # перестала исполняться в тот же день. Оставить её значило держать в самопроверке
    # утверждение, которое ждёт схождения наборов, чтобы ложно упасть.

    # ── ИСКЛЮЧЕНИЕ «ГЛАГОЛ РАДИ ЯРУСА» — обе стороны ─────────────────────────
    # Механизм ослабляет гейт по построению, поэтому обязан быть проверен строже
    # остального: он должен извинять ровно то, что заявлено, и краснеть, когда
    # извинять больше нечего.
    tier_only = write("tier_only.py",
                      'def custom_role(a, n, m, r, v): pass\n'
                      f'r = custom_role("acc", "nm", "loadbalancer", ["targetGroups"], ["{absent}", "{legit}"])\n')
    key = ("tests/authz-fixtures/tier_only.py", "loadbalancer.targetGroups", absent)

    f_no_exc, _t, _u, _m, used_no = scan(tmp, [tier_only], verb_sets, types_map, vocabulary)
    if f_no_exc and not used_no:
        print("  ОК  без объявления тот же глагол остаётся находкой")
    else:
        print(f"  ПРОВАЛ без объявления глагол {absent!r} не найден — механизм извиняет "
              f"по умолчанию, то есть ослабляет гейт молча")
        rc = 1

    f_exc, _t, _u, _m, used_yes = scan(tmp, [tier_only], verb_sets, types_map, vocabulary,
                                       {key: "самопроверка"})
    if not f_exc and key in used_yes:
        print("  ОК  заявленный ярусный глагол извинён и ПОСЧИТАН")
    else:
        print(f"  ПРОВАЛ заявленное исключение не сработало (находок {len(f_exc)}, "
              f"использовано {len(used_yes)})")
        rc = 1

    # …и извинение выдаётся ИМЕННО заявленной паре, а не любому глаголу файла.
    other = ("tests/authz-fixtures/tier_only.py", "loadbalancer.networkLoadBalancers", absent)
    f_wrongpair, _t, _u, _m, _used = scan(tmp, [tier_only], verb_sets, types_map, vocabulary,
                                          {other: "самопроверка/чужая пара"})
    if f_wrongpair:
        print("  ОК  исключение, выписанное на ДРУГУЮ пару, не извиняет эту")
    else:
        print("  ПРОВАЛ исключение сработало на паре, которой не адресовано — "
              "ключ не различает ресурс")
        rc = 1

    # ИСТЕЧЕНИЕ: запись, которой нечего извинять, обязана уронить гейт с именем.
    stale_rc = run(tmp, [good], verb_sets, label="самопроверка/исключение без предмета",
                   excused={("tests/authz-fixtures/gone.py", "x.y", "start"): "нет предмета"})
    if stale_rc == 1:
        print("  ОК  исключение без предмета — находка, а не тихое послабление")
    else:
        print(f"  ПРОВАЛ исключение без предмета дало код {stale_rc}, а обязано 1")
        rc = 1

    print()
    print("PASS: самопроверка assert-fixture-role-verbs-exist" if rc == 0
          else "FAIL: самопроверка assert-fixture-role-verbs-exist")
    return rc


def main():
    if "--self-test" in sys.argv:
        return self_test()
    root = repo_root()
    return run(root, list_fixture_files(root), model_verb_sets(os.path.join(root, MODEL_REL)))


if __name__ == "__main__":
    sys.exit(main())
