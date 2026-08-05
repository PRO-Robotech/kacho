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
# services/iam/internal/apps/kacho/api/role/rules_catalog.go). Поэтому роль с
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
# (proto/kacho/cloud/iam/v1/fga_model.fga) — из отношений с приставкой `v_`. Ровно
# та же сторона, которую сторожит гейт дрейфа iam и internal/repohygiene/
# verbvocabulary_test.go. Держать здесь рукописную копию словаря значило бы завести
# ту самую вторую таблицу, которая молча разъедется с первой.
#
# ПРЕДПОСЫЛКА, КОТОРУЮ ГЕЙТ ПРОВЕРЯЕТ ЗА СОБОЙ. Сегодня ВСЕ глагольные типы
# объявляют один и тот же набор, поэтому «глагол существует» решается без
# разрешения пары (модуль, ресурс) в тип. Это факт о дереве, а не закон: как только
# хотя бы один тип объявит свой набор иначе, проверка «по общему словарю» станет
# слабее правды и начнёт пропускать глагол, законный у соседнего типа и
# несуществующий у этого. Гейт это сам обнаруживает и ОТКАЗЫВАЕТСЯ работать,
# называя, что именно нужно дописать. Послабление истекает само.
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

MODEL_REL = "proto/kacho/cloud/iam/v1/fga_model.fga"

# Где живут фикстуры, заводящие роли. Предикат — МЕСТО: посев матрицы прав и
# декларативные кейсы newman. Прод-код и миграции сюда НЕ входят намеренно —
# у 58 системных ролей другая таксономия (их авторитет несут permissions[]+ярусы,
# см. rules_catalog.go systemCtx), и мерить их этим словарём было бы неверно.
FIXTURE_GLOBS = ["tests/authz-fixtures/*.py", "services/*/tests/newman/cases/*.py"]

VERB_WILDCARD = "*"

# Закрытая таблица (module.resource) → тип модели прав. Один источник с прод-кодом:
# гейт читает ТУ ЖЕ карту, по которой строит объекты реконсайлер, поэтому «глагол
# законен» решается набором ЕГО типа, а не общим словарём.
TYPES_REL = "services/iam/internal/authzmap/fga_types.go"


def repo_root():
    return os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))


def object_types(types_path):
    """{"module.resource": "тип_модели"} — из закрытой таблицы прод-кода.

    ПОЧЕМУ ИЗ КОДА, А НЕ СПИСКОМ ЗДЕСЬ. Копия таблицы в гейте разъехалась бы с
    прод-кодом молча: новый ресурс появился бы у реконсайлера и не появился бы
    у проверки, и та начала бы считать законный глагол несуществующим.
    """
    try:
        src = open(types_path, encoding="utf-8").read()
    except OSError:
        return None
    m = re.search(r"var objectTypes = map\[string\]string\{(.*?)\n\}", src, re.S)
    if not m:
        return None
    out = {}
    for k, v in re.findall(r'"([^"]+)"\s*:\s*"([^"]+)"', m.group(1)):
        out[k] = v
    return out or None


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


def scan(root, files, verb_sets, types_map, fallback):
    """Сверка глагола с набором ЕГО типа.

    Пара (module, resource) резолвится закрытой таблицей прод-кода в тип модели, и
    глагол ищется в наборе именно этого типа. Прежняя редакция сверяла с ОБЩИМ
    словарём и держалась на предпосылке «у всех типов один набор»; предпосылка
    отпала, как только у одного типа появились свои глаголы. Общий словарь пропускал
    бы глагол, законный у соседа и несуществующий здесь.

    Нерезолвимая пара (нет module/resources рядом либо пары нет в таблице) НЕ
    молчит: она считается отдельно и печатается — «не смог разрешить» никогда не
    равно «законно».
    """
    # Приведение авторского глагола — ТО ЖЕ, что делает прод-код перед выводом имени
    # отношения (services/iam/internal/domain/rule_verbs.go::NormalizeVerb: обрезка
    # пробелов + складывание регистра). Без него camelCase-глагол фикстуры не совпал
    # бы с приведённым именем в модели, и гейт объявил бы находкой законную запись.
    norm = lambda v: v.strip().lower()
    findings, triples, unresolved, unmapped = [], 0, 0, []
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
                if absent:
                    findings.append((rel, lineno, shape + " → " + r, verbs, absent))
    return findings, triples, unresolved, unmapped


def run(root, files, verb_sets, label="дерево"):
    """Общий проход. Возвращает код возврата."""
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

    types_map = object_types(os.path.join(root, TYPES_REL))
    if not types_map:
        print(f"FATAL: не прочитана закрытая таблица типов {TYPES_REL} — "
              "разрешать пару (module, resource) нечем, и молчание ничего не доказывает.")
        return 2

    if files is None:
        print("FATAL: состав фикстур не читается (git ls-files не отработал). "
              "«Нечего проверять» — не «всё хорошо».")
        return 2
    if not files:
        print(f"FAIL: под {FIXTURE_GLOBS} не нашлось НИ ОДНОГО файла. "
              "Ноль находок на нуле прочитанного — это провал, а не чистота.")
        return 1

    findings, triples, unresolved, unmapped = scan(root, files, verb_sets, types_map, fallback)

    # Перепись — ОТДЕЛЬНОЕ утверждение от вердикта: «0 находок» обязано быть
    # отличимо от «0 прочитанного».
    print(f"assert-fixture-role-verbs-exist: {label} — прочитано файлов: {len(files)}; "
          f"определений правил роли: {triples}; глаголы не разрешились статически: "
          f"{unresolved}; глагольных типов модели: {len(verb_sets)}; пар в закрытой "
          f"таблице: {len(types_map)}; пар не разрешилось в тип: {len(unmapped)}")
    if unmapped:
        # «Не разрешил» — отдельный исход, а не тишина: по нерезолвимой паре глагол
        # сверяется объединением, то есть строже общего словаря не становится.
        seen = sorted({u[2] for u in unmapped})
        print(f"  пары вне закрытой таблицы ({len(seen)}): {seen[:12]}"
              + (" …" if len(seen) > 12 else ""))

    if findings:
        print(f"\nFAIL: {len(findings)} определен(ий) роли построено из глагола, "
              f"которого модель прав не объявляет.")
        print("Такая роль создаётся успешно (сегмент глагола в Role.Create не закрыт "
              "словарём), но отношения `v_*` за несуществующим глаголом реконсайлер "
              "не эмитит — молча. Держатель роли не получает НИЧЕГО, а кейс,\n"
              "утверждающий «этот субъект может», не может пройти ни при каком "
              "исправном продукте.\n")
        for rel, lineno, shape, verbs, absent in findings:
            print(f"  {rel}:{lineno}  [{shape}] глаголы={verbs} → нет в модели: {absent}")
        return 1

    if triples == 0:
        print("FAIL: файлы прочитаны, но определений правил роли в них НЕТ вовсе — "
              "разбор перестал доходить до предмета (сменилась форма вызова?).")
        return 1

    print("OK: каждый глагол каждой роли фикстур объявлен моделью прав.")
    return 0


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
    os.makedirs(os.path.join(tmp, os.path.dirname(TYPES_REL)), exist_ok=True)
    shutil.copyfile(os.path.join(root, TYPES_REL), os.path.join(tmp, TYPES_REL))

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
    types_map = object_types(os.path.join(root, TYPES_REL))
    if not types_map:
        print(f"  ПРОВАЛ закрытая таблица {TYPES_REL} не читается — "
              f"самопроверке не на чем стоять")
        return 1
    findings, triples, _unresolved, _unmapped = scan(tmp, files, verb_sets, types_map, vocabulary)
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

    # ПУСТОЙ СОСТАВ — провал, а не тишина.
    _empty_rc = run(tmp, [], verb_sets, label="самопроверка/пустой состав")
    if _empty_rc != 1:
        print(f"  ПРОВАЛ пустой состав фикстур дал код {_empty_rc}, а обязан 1 "
              f"(0 — принят за успех; 2 — отказ по ДРУГОЙ причине, то есть "
              f"подпроверка прошла не о том, о чём спрашивала)")
        rc = 1
    else:
        print("  ОК  пустой состав фикстур — провал, а не чистота")

    # ПРЕДПОСЫЛКА: гейт обязан ОТКАЗАТЬСЯ, если наборы типов разошлись.
    split = dict(verb_sets)
    split["__synthetic_divergent_type"] = {legit}
    if len(set(map(frozenset, verb_sets.values()))) == 1:
        if run(tmp, [good], split, label="самопроверка/разошедшиеся наборы") == 2:
            print("  ОК  разошедшиеся наборы типов — отказ работать, а не тихое ослабление")
        else:
            print("  ПРОВАЛ наборы типов разошлись, а гейт продолжил судить по общему словарю")
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
