#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Уборка снимает потомка, которого завёл САМ ПРОДУКТ вместе с родителем.

ПРЕДМЕТ. Сага создания ресурса может закоммитить в своей же транзакции строку
ДРУГОГО ресурса. Такой потомок кейсу не принадлежит — кейс его не создавал, не
называл и в исходнике не видит; он узнаёт о нём только из метаданных операции
создания родителя. Если удаление родителя гейтится ссылочной целостностью
(`data-integrity.md` ban #10), а сага-потомок не снят, уборка получает ЗАКОННЫЙ
`FAILED_PRECONDITION`, родитель переживает прогон, и красным становится кейс, а
не продукт.

ЧЕМ ЭТОТ ГЕЙТ ОТЛИЧАЕТСЯ ОТ СОСЕДНЕГО ПО КЛАССУ
(`assert-teardown-frees-parent.py`). Проверка «уборка снимает потомка, КОТОРОГО
КЕЙС ЗАВЁЛ САМ» строит тройку (кейс, потомок, родитель) из
подстановки родителя в ТЕЛО шага, создающего потомка. Здесь такой подстановки
нет by construction: потомок создаётся ТЕМ ЖЕ шагом, что и родитель, и в теле
этого шага родителя ещё нет — его идентификатор только предвыделяется. Поэтому
сага-потомок невидим предикату «ссылка объявлена телом», и его нужно искать по
второй половине пары — по полю метаданных.

ЕДИНСТВЕННЫЙ СЕГОДНЯШНИЙ ЭКЗЕМПЛЯР — аккаунт и его проект `default`.
`Account.Create` co-commit'ит проект `default` в writer-транзакции аккаунта
(redesign-2026 F2), его идентификатор приезжает клиенту в
`CreateAccountMetadata.default_project_id` ещё до `done`, а `Account.Delete`
отказывается сносить аккаунт, пока в нём есть хоть один проект. Значит кейс,
удаляющий аккаунт, который он же и создал, обязан снять проект саги — либо
осознанно проверять сам отказ.

ПОЧЕМУ ЭТОТ ГЕЙТ ЗАВЕДЁН ТОЛЬКО СЕЙЧАС. Коллекция, где промах живёт, входила в
девятку, которая на предыдущем прогоне не запускалась вовсе (сорванный посев
волны церемонии): дефект не новый — он стал ВИДЕН впервые, когда шаг удаления
начал утверждать ИСХОД операции (операция, завершившаяся ошибкой, тоже `done`).

ЧТО ПРОВЕРЯЕТСЯ (единица счёта — ШАГ удаления аккаунта, созданного коллекцией)
------------------------------------------------------------------------------
  • РОДИТЕЛЬ — переменная, в которую шаг `POST …/iam/v1/accounts` кладёт
    `metadata.accountId`.
  • ПОТОМОК САГИ — переменная, в которую ТОТ ЖЕ шаг кладёт
    `metadata.defaultProjectId`.
  • НАХОДКА — коллекция доходит до `DELETE …/iam/v1/accounts/{{родитель}}`, а
    проект саги к этому моменту либо не захвачен вовсе, либо захвачен и не удалён.
  • НЕ находка — аккаунт, который никто не удаляет (утечка роста, другой предмет),
    и кейс, который УТВЕРЖДАЕТ отказ (`error.code == 9` + текст про проекты):
    там непустой аккаунт и есть предмет проверки.

ОБЛАСТЬ — КОЛЛЕКЦИЯ И ЕЁ ПОРЯДОК, А НЕ ОТДЕЛЬНЫЙ КЕЙС. Окружение newman одно на
прогон коллекции, поэтому уборка законно живёт в ДРУГОМ кейсе: `IAM-ACC-DL-CRUD-OK`
сносит проект саги аккаунта, созданного предыдущим кейсом той же коллекции.
Покейсный предикат объявил бы этот законный порядок находкой либо (что хуже)
пропустил бы обратный — снос аккаунта РАНЬШЕ его проекта. Поэтому обход идёт по
коллекции в порядке исполнения, и вопрос задаётся в момент удаления родителя.

Освобождение задано ПРЕДИКАТОМ на собственных утверждениях кейса, а не списком
имён: списку нечему протухать, потому что списка нет.

ПРЕДПОСЫЛКИ — ГЕЙТ ПРОВЕРЯЕТ ИХ САМ И ПАДАЕТ, КОГДА ОНИ ИСЧЕЗЛИ
---------------------------------------------------------------
Запрет держится на трёх фактах о дереве. Пропадёт любой — предмета у гейта
больше нет, и он обязан сказать об этом падением, а не тихо зеленеть:

  P1 `CreateAccountMetadata` объявляет `default_project_id` (иначе клиенту неоткуда
     узнать идентификатор потомка);
  P2 use-case создания аккаунта вставляет строку проекта в своей writer-транзакции
     (иначе саги нет);
  P3 удаление аккаунта отказывает, пока в нём есть проект (иначе снимать нечего).

Плюс ПЕРЕПИСЬ САГ: гейт обходит все `create.go` под `services/*/internal/apps/`
и ищет use-case'ы, вставляющие строки ЧУЖИХ ресурсов. Найденная пара, которой
гейт не знает, — ПАДЕНИЕ с требованием научить его: так новая сага не заводится
молча. Пара «аккаунт → выдача» из переписи исключена с проверяемым основанием:
`Account.Delete` вычищает выдачи аккаунта сам, и это читается в его исходнике.

ПОЧЕМУ ЧИТАЮТСЯ СГЕНЕРИРОВАННЫЕ КОЛЛЕКЦИИ. Исполняется коллекция; помощники
уборки у каждой суиты свои, и проверка по исходникам видела бы только знакомые
ей формы. Чинить при этом надо ИСХОДНИК кейса — сгенерированное затрётся
следующим прогоном генератора.
"""
import json
import os
import re
import sys
import tempfile

# ─── формы, задаваемые генератором (`save_from_response`) ────────────────────
# Захват поля метаданных операции в переменную окружения. Если форма изменится,
# счётчик «аккаунтов, созданных кейсом» упадёт в ноль и основной проход обязан
# упасть на проверке предпосылки — «ноль находок» не должно быть неотличимо от
# «ноль прочитанного».
def _capture_re(field: str) -> re.Pattern:
    return re.compile(
        r"const v = \(j\.metadata && j\.metadata\." + field + r"\);\s*\n"
        r"\s*if \(v !== undefined[^\n]*\n?[^\n]*"
        r"pm\.environment\.set\(\s*'(?P<var>[A-Za-z0-9_]+)'", re.M)


CAPTURE_ACCOUNT = _capture_re("accountId")
CAPTURE_DEFAULT_PROJECT = _capture_re("defaultProjectId")

DELETE_ACCOUNT = re.compile(r"/iam/v1/accounts/\{\{([A-Za-z0-9_]+)\}\}\s*$")
DELETE_PROJECT = re.compile(r"/iam/v1/projects/\{\{([A-Za-z0-9_]+)\}\}\s*$")
CREATE_ACCOUNT = re.compile(r"/iam/v1/accounts(\?|$)")

# Кейс, чей ПРЕДМЕТ — сам отказ: он утверждает код 9 и текст про проекты.
# Обе половины обязаны быть в ОДНОМ скрипте, иначе совпадение случайно.
ASSERTS_REFUSAL_CODE = re.compile(r"j\.error && j\.error\.code[^\n]*to\.eql\(9\)")
ASSERTS_REFUSAL_TEXT = re.compile(r"contains projects")


def _raw_url(req):
    u = req.get("url")
    return u.get("raw", "") if isinstance(u, dict) else (u or "")


def _test_code(item):
    return "\n".join(
        "\n".join(ev.get("script", {}).get("exec", []) or [])
        for ev in (item.get("event") or []) if ev.get("listen") == "test")


def _leaves(items):
    for it in items:
        if "item" in it:
            yield from _leaves(it["item"])
        else:
            yield it


def scan(root):
    """→ (находки, (коллекций, кейсов, шагов, созданных аккаунтов, удалённых аккаунтов))."""
    files = []
    for dirpath, _dirs, names in os.walk(root):
        if os.path.basename(dirpath) != "collections":
            continue
        files += [os.path.join(dirpath, n) for n in sorted(names)
                  if n.endswith(".postman_collection.json")]
    files.sort()

    findings = []
    n_cases = n_steps = n_acc_created = n_acc_deleted = 0
    for path in files:
        try:
            coll = json.load(open(path, encoding="utf-8"))
        except (OSError, ValueError) as exc:
            print(f"НЕ ПРОЧИТАНО {path}: {exc}", file=sys.stderr)
            continue
        suite = path.split("/tests/newman/")[0].split(os.sep)[-1]
        cname = os.path.basename(path).replace(".postman_collection.json", "")
        # Состояние живёт на КОЛЛЕКЦИЮ: окружение newman одно на её прогон,
        # а порядок кейсов в массиве и есть порядок исполнения.
        born = {}                       # аккаунт-переменная -> (кейс, шаг, проект|None)
        deleted_projects = set()
        for case in coll.get("item", []):
            if "item" not in case:
                continue
            n_cases += 1
            steps = list(_leaves(case["item"]))
            n_steps += len(steps)
            case_name = case.get("name", "?").split(" —")[0]
            case_code = "\n".join(_test_code(st) for st in steps)
            refusal_asserted = bool(ASSERTS_REFUSAL_CODE.search(case_code)
                                    and ASSERTS_REFUSAL_TEXT.search(case_code))
            for st in steps:
                req = st.get("request", {}) or {}
                url, method = _raw_url(req), req.get("method", "")
                code = _test_code(st)
                if method == "DELETE":
                    m = DELETE_PROJECT.search(url)
                    if m:
                        deleted_projects.add(m.group(1))
                        continue
                    m = DELETE_ACCOUNT.search(url)
                    if not m:
                        continue
                    n_acc_deleted += 1
                    acc_var = m.group(1)
                    if acc_var not in born or refusal_asserted:
                        continue
                    origin_case, origin_step, prj_var = born[acc_var]
                    if prj_var is not None and prj_var in deleted_projects:
                        continue          # уборка полная и ВОВРЕМЯ
                    findings.append({
                        "suite": suite, "collection": cname, "case": case_name,
                        "account": acc_var, "project": prj_var,
                        "origin_case": origin_case, "step": origin_step,
                    })
                    continue
                if method != "POST" or not CREATE_ACCOUNT.search(url):
                    continue
                acc = CAPTURE_ACCOUNT.search(code)
                if not acc:
                    continue
                n_acc_created += 1
                prj = CAPTURE_DEFAULT_PROJECT.search(code)
                born[acc.group("var")] = (case_name, st.get("name", "?"),
                                          prj.group("var") if prj else None)
    return findings, (len(files), n_cases, n_steps, n_acc_created, n_acc_deleted)


# ─── перепись саг: какие use-case'ы создания вставляют ЧУЖИЕ ресурсы ─────────
WRITER_INSERT = re.compile(r"\bw\.([A-Za-z]+?)W?\(\)\.Insert\(")
# Удаление называется не только `Delete`. У охраняемого снятия свой глагол
# (`DeleteGuarded` — снимает строку и отдаёт её содержимое, чтобы вызывающий вернул
# аренду в пул), и предикат, знающий одно имя, объявлял бы уборку отсутствующей там,
# где она есть. Ровно это и произошло с адресом шлюза: уборка написана, а гейт её
# не читал. Расширение узкое — суффикс после `Delete`, а не любое имя: «удалить» и
# «пометить» разными глаголами остаются разными.
WRITER_DELETE = re.compile(r"\bw\.([A-Za-z]+?)W?\(\)\.Delete[A-Za-z]*\(")

# Уборка потомка бывает НЕ прямым вызовом писателя, а общей функцией дренажа: у
# выдач она одна на проект и на аккаунт, потому что оба снимают их одинаково и
# разойтись двум копиям нельзя. Гейт обязан признавать обе формы — иначе законный
# перевод на общий дренаж читается как ИСЧЕЗНУВШАЯ уборка, а исключение при живом
# предмете объявляется протухшим. Наблюдалось: перевод удаления аккаунта и проекта
# на `shared.RevokeBindingsInScope` (задача #792) покраснел здесь при верном коде.
SHARED_TEARDOWN = {
    "AccessBindings": re.compile(r"\bshared\.RevokeBindingsInScope\("),
}

# Известные гейту сопутствующие потомки: (сервис, ресурс-родитель) → {писатель: почему}.
# Значение «None» означает «за уборку отвечает КЛИЕНТ» — такую пару гейт проверяет
# по коллекциям; строка — записанное основание, почему клиенту делать нечего.
KNOWN_COCREATED = {
    ("vpc", "gateway"): {
        "Addresses":
            "Gateway.Delete возвращает адрес и его аренду в пул (releaseGatewayAddress "
            "в services/vpc/internal/apps/kacho/api/gateway/delete.go): снимает ссылку, "
            "удаляет строку охраняемым снятием и возвращает IP в свободный список пула",
    },
    ("iam", "account"): {
        "Projects": None,
        "AccessBindings":
            "Account.Delete вычищает выдачи аккаунта сам (shared.RevokeBindingsInScope "
            "в services/iam/internal/apps/kaname/api/account/delete.go)",
    },
}


def _is_own_writer(writer: str, res_dir: str) -> bool:
    """Писатель принадлежит ТОМУ ЖЕ ресурсу, что и каталог use-case'а?

    Сравнение идёт от КАТАЛОГА (он по конвенции в единственном числе) к имени
    писателя (во множественном), а не наоборот: обратное направление требует
    угадывать единственное число, и на `Addresses` в каталоге `address` оно
    промахивается — `address` само оканчивается на «s». Первый прогон дал ровно
    этот ложный срабат, и он же закреплён самопроверкой (и).
    """
    own = re.sub(r"[^a-z0-9]", "", res_dir.lower())
    return writer.lower() in (own, own + "s", own + "es")


def census_sagas(root):
    """→ (незнакомые пары, оснóвы без предмета, файлов, файлов с писателем).

    «Оснóва без предмета» — запись исключения, чьё обоснование больше не читается
    в дереве: она объявляет, что уборкой потомка занимается удаление родителя, а
    вызова этого удаления в `delete.go` родителя нет. Исключение живёт, пока у
    него есть предмет (`testing.md` §«Гейт на класс», п.5), поэтому такая запись —
    находка, а не примечание.
    """
    unknown, stale = [], []
    n_files = n_with_writer = 0
    base = os.path.join(root, "services")
    for dirpath, _dirs, names in os.walk(base):
        if "create.go" not in names or "/internal/apps/" not in dirpath.replace(os.sep, "/") + "/":
            continue
        path = os.path.join(dirpath, "create.go")
        rel = os.path.relpath(path, root).replace(os.sep, "/")
        if "/internal/apps/" not in rel:
            continue
        n_files += 1
        try:
            src = open(path, encoding="utf-8").read()
        except OSError:
            continue
        writers = sorted({m.group(1) for m in WRITER_INSERT.finditer(src)})
        if writers:
            n_with_writer += 1
        svc = rel.split("/")[1]
        res = os.path.basename(dirpath)
        foreign = [w for w in writers if not _is_own_writer(w, res)]
        known = KNOWN_COCREATED.get((svc, res), {})
        del_src = ""
        del_path = os.path.join(dirpath, "delete.go")
        if os.path.exists(del_path):
            try:
                del_src = open(del_path, encoding="utf-8").read()
            except OSError:
                del_src = ""
        del_writers = {m.group(1) for m in WRITER_DELETE.finditer(del_src)}
        # вторая законная форма — общий дренаж; см. SHARED_TEARDOWN
        for writer, pat in SHARED_TEARDOWN.items():
            if pat.search(del_src):
                del_writers.add(writer)
        for w in foreign:
            if w not in known:
                unknown.append((svc, res, w, rel))
            elif known[w] is not None and w not in del_writers:
                stale.append((svc, res, w, os.path.relpath(del_path, root).replace(os.sep, "/")))
    return unknown, stale, n_files, n_with_writer


def premises(root):
    """→ список (имя, выполнена?, где искали)."""
    def has(rel, pattern):
        try:
            return re.search(pattern, open(os.path.join(root, rel), encoding="utf-8").read()) is not None
        except OSError:
            return False
    return [
        ("P1 CreateAccountMetadata объявляет default_project_id",
         has("proto/kaname/cloud/iam/v1/account.proto",
             r"message CreateAccountMetadata\b[\s\S]{0,600}?\bstring default_project_id\b"),
         "proto/kaname/cloud/iam/v1/account.proto"),
        ("P2 сага создания аккаунта вставляет проект в своей транзакции",
         has("services/iam/internal/apps/kaname/api/account/create.go",
             r"w\.ProjectsW\(\)\.Insert\("),
         "services/iam/internal/apps/kaname/api/account/create.go"),
        ("P3 удаление аккаунта отказывает, пока в нём есть проект",
         has("services/iam/internal/repo/kaname/pg/account_repo.go",
             r"NOT EXISTS \(SELECT 1 FROM projects"),
         "services/iam/internal/repo/kaname/pg/account_repo.go"),
    ]


# ─── самопроверка: краснеет на настоящем промахе, молчит на законных близнецах ─
def _collection(case_name, steps):
    return _collection_of([(case_name, steps)])


def _collection_of(cases):
    return {"info": {"name": "t", "schema": ""},
            "item": [{"name": n, "item": s} for n, s in cases]}


def _capture_lines(pairs):
    out = ["try {", "  const j = pm.response.json();"]
    for field, var in pairs:
        out += [f"  const v = (j.metadata && j.metadata.{field});",
                "  if (v !== undefined && v !== null) "
                f"pm.environment.set('{var}', String(v));"]
    out.append("} catch (e) {}")
    return out


def _step(name, method, path, captures=None, extra=None):
    it = {"name": name,
          "request": {"method": method, "url": {"raw": "{{baseUrl}}" + path}}}
    exec_ = []
    if captures:
        # каждая пара — свой блок, как их печатает генератор
        for field, var in captures:
            exec_ += _capture_lines([(field, var)])
    if extra:
        exec_ += extra
    if exec_:
        it["event"] = [{"listen": "test", "script": {"exec": exec_}}]
    return it


def _write(tmp, name, coll):
    d = os.path.join(tmp, "services", "x", "tests", "newman", "collections")
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, name + ".postman_collection.json"), "w",
              encoding="utf-8") as fh:
        json.dump(coll, fh)


_MK_ACC_BOTH = [("accountId", "aAcc"), ("defaultProjectId", "aPrj")]
_MK_ACC_ONLY = [("accountId", "aAcc")]
_REFUSAL = [
    "const j = pm.response.json();",
    "pm.test('error code 9 (FAILED_PRECONDITION)', () => "
    "pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql(9));",
    "pm.test('error text includes \"contains projects\"', () => "
    "pm.expect((j.error && j.error.message || '').toLowerCase(), "
    "JSON.stringify(j)).to.include('contains projects'));",
]


def self_test() -> int:
    rc = 0
    print("=== самопроверка: инъекция настоящего промаха + законные близнецы ===")

    def run(name, steps):
        with tempfile.TemporaryDirectory() as tmp:
            _write(tmp, "t", _collection(name, steps))
            return scan(tmp)

    def run_cases(cases):
        with tempfile.TemporaryDirectory() as tmp:
            _write(tmp, "t", _collection_of(cases))
            return scan(tmp)

    # (а) НАСТОЯЩИЙ промах: аккаунт создан и удаляется, проект саги не захвачен.
    found, (cols, _c, _s, born, dels) = run("CASE-LEAK", [
        _step("mk-acc", "POST", "/iam/v1/accounts", _MK_ACC_ONLY),
        _step("rm-acc", "DELETE", "/iam/v1/accounts/{{aAcc}}"),
    ])
    named = [f for f in found if f["account"] == "aAcc" and f["project"] is None]
    if cols == 1 and born == 1 and dels == 1 and named:
        print("  ОК  инъекция: гейт краснеет и НАЗЫВАЕТ координату "
              f"({named[0]['case']} :: {{{{{named[0]['account']}}}}})")
    else:
        print(f"  ПРОВАЛ инъекция не поймана: коллекций {cols}, аккаунтов {born}, "
              f"находок {len(found)}")
        rc = 1

    # (б) ПОЛУМЕРА: проект саги ЗАХВАЧЕН, но не удалён — по-прежнему находка.
    found, _m = run("CASE-CAPTURED-NOT-DELETED", [
        _step("mk-acc", "POST", "/iam/v1/accounts", _MK_ACC_BOTH),
        _step("rm-acc", "DELETE", "/iam/v1/accounts/{{aAcc}}"),
    ])
    if [f for f in found if f["project"] == "aPrj"]:
        print("  ОК  захват без удаления находкой остаётся — гейт судит по УБОРКЕ, "
              "а не по захвату")
    else:
        print(f"  ПРОВАЛ захваченный, но не снятый потомок пропущен: {found}")
        rc = 1

    # (в) ЗАКОННЫЙ БЛИЗНЕЦ: проект саги снят ПЕРЕД аккаунтом.
    found, (cols, _c, _s, born, dels) = run("CASE-CLEAN", [
        _step("mk-acc", "POST", "/iam/v1/accounts", _MK_ACC_BOTH),
        _step("rm-prj", "DELETE", "/iam/v1/projects/{{aPrj}}"),
        _step("rm-acc", "DELETE", "/iam/v1/accounts/{{aAcc}}"),
    ])
    if cols == 1 and born == 1 and dels == 1 and not found:
        print("  ОК  законный близнец: та же форма, уборка полная — гейт молчит")
    else:
        print(f"  ПРОВАЛ близнец дал {len(found)} находок")
        rc = 1

    # (в2) ЗАКОННЫЙ БЛИЗНЕЦ ЧЕРЕЗ КЕЙС: уборка живёт в СОСЕДНЕМ кейсе той же
    #      коллекции (окружение newman общее) — это форма `IAM-ACC-DL-CRUD-OK`.
    found, _m = run_cases([
        ("CASE-CREATE", [_step("mk-acc", "POST", "/iam/v1/accounts", _MK_ACC_BOTH)]),
        ("CASE-DELETE", [_step("rm-prj", "DELETE", "/iam/v1/projects/{{aPrj}}"),
                         _step("rm-acc", "DELETE", "/iam/v1/accounts/{{aAcc}}")]),
    ])
    if not found:
        print("  ОК  уборка в соседнем кейсе той же коллекции — законна, гейт молчит")
    else:
        print(f"  ПРОВАЛ межкейсовая уборка принята за промах: {found}")
        rc = 1

    # (в3) ПОРЯДОК ЗНАЧИМ: тот же набор шагов, но проект снимается ПОСЛЕ аккаунта —
    #      находка. Без учёта порядка предикат «где-то в коллекции есть удаление»
    #      зеленел бы на перевёрнутой уборке, то есть ровно на дефекте.
    found, _m = run_cases([
        ("CASE-CREATE", [_step("mk-acc", "POST", "/iam/v1/accounts", _MK_ACC_BOTH)]),
        ("CASE-DELETE-ACC", [_step("rm-acc", "DELETE", "/iam/v1/accounts/{{aAcc}}")]),
        ("CASE-DELETE-PRJ", [_step("rm-prj", "DELETE", "/iam/v1/projects/{{aPrj}}")]),
    ])
    if [f for f in found if f["case"] == "CASE-DELETE-ACC"]:
        print("  ОК  перевёрнутый порядок (аккаунт раньше проекта) остаётся находкой")
    else:
        print(f"  ПРОВАЛ порядок не учтён: {found}")
        rc = 1

    # (г) ЗАКОННЫЙ БЛИЗНЕЦ: предмет кейса — САМ ОТКАЗ на непустом аккаунте.
    found, _m = run("CASE-REFUSAL-IS-THE-SUBJECT", [
        _step("mk-acc", "POST", "/iam/v1/accounts", _MK_ACC_ONLY),
        _step("rm-acc", "DELETE", "/iam/v1/accounts/{{aAcc}}"),
        _step("await", "GET", "/operations/{{opId}}", extra=_REFUSAL),
    ])
    if not found:
        print("  ОК  кейс, УТВЕРЖДАЮЩИЙ отказ, освобождён своим же утверждением")
    else:
        print(f"  ПРОВАЛ кейс про отказ принят за промах: {found}")
        rc = 1

    # (д) ЗАКОННЫЙ БЛИЗНЕЦ: аккаунт создан и НЕ удаляется — другой предмет.
    found, _m = run("CASE-NO-DELETE", [
        _step("mk-acc", "POST", "/iam/v1/accounts", _MK_ACC_ONLY),
    ])
    if not found:
        print("  ОК  аккаунт без удаления — не предмет этого гейта")
    else:
        print(f"  ПРОВАЛ кейс без удаления аккаунта дал находку: {found}")
        rc = 1

    # (е) ПОЛОВИНА ОСВОБОЖДЕНИЯ НЕ ОСВОБОЖДАЕТ: код 9 без текста про проекты
    #     (напр. отказ совсем другого предусловия) кейс не выводит из-под запрета.
    found, _m = run("CASE-HALF-EXEMPTION", [
        _step("mk-acc", "POST", "/iam/v1/accounts", _MK_ACC_ONLY),
        _step("rm-acc", "DELETE", "/iam/v1/accounts/{{aAcc}}"),
        _step("await", "GET", "/operations/{{opId}}", extra=[
            "const j = pm.response.json();",
            "pm.test('code 9', () => pm.expect(j.error && j.error.code, "
            "JSON.stringify(j)).to.eql(9));"]),
    ])
    if found:
        print("  ОК  освобождение требует ОБЕИХ половин утверждения")
    else:
        print("  ПРОВАЛ половинчатое утверждение освободило кейс")
        rc = 1

    # (ж) ПРЕДПОСЫЛКА ОБХОДА: пустое дерево даёт ноль ПРОЧИТАННОГО.
    with tempfile.TemporaryDirectory() as tmp:
        _found, (cols, _c, _s, born, _d) = scan(tmp)
        if (cols, born) == (0, 0):
            print("  ОК  пустое дерево даёт ноль прочитанного — основной проход обязан падать")
        else:
            print(f"  ПРОВАЛ пустое дерево дало {cols} коллекций / {born} аккаунтов")
            rc = 1

    # (з) ПЕРЕПИСЬ САГ видит незнакомую пару и называет её.
    with tempfile.TemporaryDirectory() as tmp:
        d = os.path.join(tmp, "services", "zz", "internal", "apps", "kacho", "api", "widget")
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "create.go"), "w", encoding="utf-8") as fh:
            fh.write("package widget\nfunc f(){ w.WidgetsW().Insert(ctx, x); "
                     "w.SprocketsW().Insert(ctx, y) }\n")
        unknown, _stale, n_files, n_writer = census_sagas(tmp)
        if n_files == 1 and n_writer == 1 and any(u[2] == "Sprockets" for u in unknown):
            print("  ОК  перепись саг: новая сопутствующая вставка НАЗВАНА, а не проглочена")
        else:
            print(f"  ПРОВАЛ перепись: файлов {n_files}, с писателем {n_writer}, "
                  f"незнакомых {unknown}")
            rc = 1

    # (и) ЗАКОННЫЙ БЛИЗНЕЦ ПЕРЕПИСИ: писатель во множественном числе — СВОЙ ресурс.
    #     Форма `w.AddressesW()` в каталоге `address` живёт в дереве и чужой вставкой
    #     не является; без нормализации числа гейт краснел бы на ней (проверено —
    #     первый же прогон дал ровно этот ложный срабат).
    with tempfile.TemporaryDirectory() as tmp:
        d = os.path.join(tmp, "services", "zz", "internal", "apps", "kacho", "api", "address")
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "create.go"), "w", encoding="utf-8") as fh:
            fh.write("package address\nfunc f(){ w.AddressesW().Insert(ctx, x) }\n")
        unknown, _stale, n_files, n_writer = census_sagas(tmp)
        if (n_files, n_writer, unknown) == (1, 1, []):
            print("  ОК  свой ресурс во множественном числе чужой вставкой не считается")
        else:
            print(f"  ПРОВАЛ множественное число принято за чужой ресурс: {unknown}")
            rc = 1

    # (к) ОСНОВА ИСКЛЮЧЕНИЯ ПРОВЕРЯЕТСЯ, А НЕ ПРИНИМАЕТСЯ НА СЛОВО: запись говорит,
    #     что потомка снимает удаление родителя; в дереве такого вызова нет.
    with tempfile.TemporaryDirectory() as tmp:
        d = os.path.join(tmp, "services", "iam", "internal", "apps", "kacho", "api", "account")
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "create.go"), "w", encoding="utf-8") as fh:
            fh.write("package account\nfunc f(){ w.AccountsW().Insert(ctx, a); "
                     "w.ProjectsW().Insert(ctx, p); w.AccessBindingsW().Insert(ctx, b) }\n")
        with open(os.path.join(d, "delete.go"), "w", encoding="utf-8") as fh:
            fh.write("package account\nfunc g(){ w.AccountsW().Delete(ctx, id) }\n")
        _unknown, stale, _nf, _nw = census_sagas(tmp)
        if any(s[2] == "AccessBindings" for s in stale):
            print("  ОК  основа исключения без предмета НАЗВАНА (уборка потомка исчезла)")
        else:
            print(f"  ПРОВАЛ исчезнувшая уборка потомка не замечена: {stale}")
            rc = 1
        # и зеркальная сторона: как только вызов появляется — молчит.
        with open(os.path.join(d, "delete.go"), "w", encoding="utf-8") as fh:
            fh.write("package account\nfunc g(){ shared.RevokeBindingsInScope(ctx, b); "
                     "w.AccountsW().Delete(ctx, id) }\n")
        _unknown, stale, _nf, _nw = census_sagas(tmp)
        if not stale:
            print("  ОК  та же запись при живом предмете молчит")
        else:
            print(f"  ПРОВАЛ живое исключение объявлено протухшим: {stale}")
            rc = 1

    print()
    print("PASS: гейт «уборка снимает потомка САГИ»" if rc == 0
          else "FAIL: гейт «уборка снимает потомка САГИ»")
    return rc


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        return self_test()

    root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))

    rc = 0
    print("=== предпосылки запрета (пропала любая — предмета нет) ===")
    for name, ok, where in premises(root):
        print(f"  {'ОК ' if ok else 'НЕТ'} {name}  [{where}]")
        if not ok:
            rc = 1
    if rc:
        print("FAIL: предпосылка исчезла — сага сопутствующего потомка снята или")
        print("      переписана. Гейт обязан быть перенацелен или удалён вместе с ней,")
        print("      а не оставлен зелёным на предмете, которого больше нет.")
        return 1

    unknown, stale, n_go, n_go_writer = census_sagas(root)
    print(f"=== перепись саг: осмотрено create.go {n_go}, с распознанным писателем "
          f"{n_go_writer}, незнакомых сопутствующих вставок {len(unknown)}, "
          f"оснóв без предмета {len(stale)} ===")
    if n_go == 0 or n_go_writer == 0:
        print("FAIL: обход use-case'ов создания не нашёл ни одного вызова писателя —")
        print("      идиома изменилась, и перепись саг больше ничего не измеряет.")
        return 1
    if stale:
        print("FAIL: исключение осталось без предмета — оно объявляет, что потомка")
        print("      снимает удаление родителя, а вызова этого удаления в дереве нет.")
        print("      Либо уборка переехала (и потомок теперь на клиенте — значение None),")
        print("      либо запись пора убрать вместе с её предметом.")
        for svc, res, w, rel in stale:
            print(f"    {svc}/{res}: обоснование ссылается на w.{w}W().Delete(, "
                  f"которого нет в {rel}")
        return 1
    if unknown:
        print("FAIL: use-case создания вставляет строку ЧУЖОГО ресурса, о которой гейт")
        print("      не знает. Установить, снимает ли её удаление родителя; если нет —")
        print("      добавить пару в KNOWN_COCREATED со значением None и научить обход")
        print("      её REST-пути, иначе новая сага заведётся молча.")
        for svc, res, w, rel in unknown:
            print(f"    {svc}/{res}: w.{w}W().Insert(  [{rel}]")
        return 1

    findings, (cols, cases, steps, born, dels) = scan(root)
    print(f"=== уборка против саги: осмотрено коллекций {cols}, кейсов {cases}, "
          f"шагов {steps}; аккаунтов создано кейсами {born}, удалений аккаунта "
          f"по имени {dels} ===")
    if cols == 0 or born == 0:
        print("FAIL: обход не нашёл ни коллекций, ни аккаунтов, созданных кейсом —")
        print("      предмет запрета потерян либо форма захвата в генераторе изменилась.")
        return 1

    if findings:
        print(f"FAIL: {len(findings)} кейс(ов) удаляют аккаунт, не сняв проект его саги.")
        print("      Продукт откажет ЗАКОННО ('contains projects'), аккаунт переживёт")
        print("      прогон, а красным станет уборка кейса, а не поведение продукта.")
        for f in findings:
            miss = ("проект саги вообще не захвачен" if f["project"] is None
                    else f"проект саги {{{{{f['project']}}}}} захвачен, но к этому "
                         "моменту не удалён")
            print(f"    {f['suite']}/{f['collection']} :: {f['case']}")
            print(f"        удаляет аккаунт {{{{{f['account']}}}}}, заведённый шагом "
                  f"'{f['step']}' (кейс {f['origin_case']}); {miss}")
        print()
        print("Чинить в ИСХОДНИКЕ кейса (*/tests/newman/cases/*.py): захватить")
        print("`metadata.defaultProjectId` на шаге создания аккаунта и снять этот проект")
        print("ПЕРЕД удалением аккаунта. Правка сгенерированной коллекции будет затёрта")
        print("следующим прогоном генератора.")
        return 1

    print("PASS: каждый кейс, удаляющий созданный им аккаунт, снимает и проект его саги")
    return 0


if __name__ == "__main__":
    sys.exit(main())
