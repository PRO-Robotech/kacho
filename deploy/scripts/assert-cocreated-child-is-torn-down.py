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

ПРЕДПОСЫЛКИ — У КАЖДОЙ ЕСТЬ ХОЗЯИН-ПАРА, И СУДЯТСЯ ОНИ ПО СВОЕЙ ПАРЕ
--------------------------------------------------------------------
Запрет держится на трёх фактах, и все три — о ПАРЕ «аккаунт → проект `default`»:

  P1 `CreateAccountMetadata` объявляет `default_project_id` (иначе клиенту неоткуда
     узнать идентификатор потомка);
  P2 use-case создания аккаунта вставляет строку проекта в своей writer-транзакции
     (иначе саги нет);
  P3 удаление аккаунта отказывает, пока в нём есть проект (иначе снимать нечего).

ЭТО ДЕРЕВО ПАРУ БОЛЬШЕ НЕ ПРОИЗВОДИТ, и здесь записан исход. Линия выноса службы
доступа отдельным продуктом унесла `services/iam` целиком — отслеживаемых файлов
под ним ноль, — поэтому P2 и P3 спрашивали о ЧУЖОМ дереве и отвечали «НЕТ» с
выводом «сага снята или переписана». Вывод был ЛОЖНЫМ: сага жива, у неё сменился
адрес. Отказ, называющий не тот предмет, посылает читателя искать не там.

Теперь предпосылка судится, когда её КООРДИНАТА в этом дереве есть, и объявляется
вне дерева только при ОБОИХ условиях: файла здесь нет И пара не производится
переписью. Одного условия мало — иначе «вне дерева» стало бы маской на живой паре.
Следствие: P1 судится и после разреза (контракт службы лежит в `proto/kaname/…`,
им владеет платформа), P2 и P3 названы вне дерева и вернутся вместе с каталогом,
без правки гейта.

ПЕРЕПИСЬ САГ — половина, которую это дерево ПРОИЗВОДИТ, и она осталась вооружена:
гейт обходит все `create.go` под `services/*/internal/apps/` и ищет use-case'ы,
вставляющие строки ЧУЖИХ ресурсов. Найденная пара, которой гейт не знает, —
ПАДЕНИЕ с требованием научить его: так новая сага не заводится молча. Замер:
осмотрено 13 файлов, у 12 распознан писатель.

У переписи ДВЕ НОВЫЕ ОСИ, и заведены они тем же изменением, что записало исход
выше, — потому что без них запись о снятой саге не краснеет НИКОГДА:

  * ЗАПИСЬ БЕЗ ПРЕДМЕТА — use-case пары в дереве ЕСТЬ, а объявленной чужой вставки
    он не делает: сага снята, запись её пережила. Находка;
  * ПАРА ВНЕ ДЕРЕВА — use-case'а пары здесь нет вовсе. Не находка и не «чисто»:
    предмет существует, но у другого продукта. Называется поимённо, печатается
    переписью и истекает сама — вернётся каталог, и пара снова судится обеими
    осями.

Пара «аккаунт → выдача» из переписи исключена с проверяемым основанием:
`Account.Delete` вычищает выдачи аккаунта сам, и это читается в его исходнике.

ОБХОД КОЛЛЕКЦИЙ ОСТАЁТСЯ ВООРУЖЁННЫМ, и это не формальность. Коллекции этого
дерева ходят к тому же краю, что и коллекции службы: заведи любая из них аккаунт,
сага потомка приедет вместе с ним, и находка покраснеет. Мягче стала РОВНО ОДНА
предпосылка обхода — «аккаунтов создано ноль», — и только пока в дереве нет ни
одной пары с уборкой НА КЛИЕНТЕ (значение `None`). Замер: 57 коллекций, 1895
кейсов, 7222 шага, аккаунтов создано 0, клиентских пар 0.

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
    """→ (незнакомые пары, оснóвы без предмета, записи без предмета,
        пары вне дерева, файлов, файлов с писателем).

    «Оснóва без предмета» — запись исключения, чьё обоснование больше не читается
    в дереве: она объявляет, что уборкой потомка занимается удаление родителя, а
    вызова этого удаления в `delete.go` родителя нет. Исключение живёт, пока у
    него есть предмет (`testing.md` §«Гейт на класс», п.5), поэтому такая запись —
    находка, а не примечание.

    «ЗАПИСЬ БЕЗ ПРЕДМЕТА» — та же дисциплина, но об ИНОМ: пара объявлена, её
    use-case в дереве ЕСТЬ, а чужой вставки, ради которой запись заведена, он
    больше не делает. Сага снята, запись её пережила. Ось заведена тем же
    изменением, которым снята запись о службе доступа: без неё запись о снятой
    саге не краснеет НИКОГДА — обход идёт по файлам дерева и о паре, которой в
    дереве нет, не спрашивает вовсе.
    (Наблюдалось: запись `(iam, account)` пережила снятие каталога службы и не
    дала ни красного, ни зелёного.)

    «ПАРА ВНЕ ДЕРЕВА» — use-case'а пары в этом дереве нет вовсе. Это НЕ находка и
    НЕ «чисто»: предмет существует, но у другого продукта, и судить его здесь
    нечем. Называется поимённо, печатается переписью и истекает сам — появится
    каталог, и пара снова судится обеими осями выше.
    """
    unknown, stale, unproduced = [], [], []
    produced = {}
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
        produced[(svc, res)] = set(foreign)
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

    absent = sorted(p for p in KNOWN_COCREATED if p not in produced)
    for (svc, res), writers_declared in sorted(KNOWN_COCREATED.items()):
        if (svc, res) not in produced:
            continue
        for w in sorted(writers_declared):
            if w not in produced[(svc, res)]:
                unproduced.append((svc, res, w))
    return unknown, stale, unproduced, absent, n_files, n_with_writer


def premises(root):
    """→ список (ХОЗЯИН-пара, имя, выполнена?, где искали).

    У КАЖДОЙ ПРЕДПОСЫЛКИ ЕСТЬ ХОЗЯИН, и это не оформление. Все три ниже описывают
    ОДНУ пару — сагу аккаунта, — и держались они на допущении «её производитель в
    этом дереве». Допущение снято линией выноса службы доступа: каталога
    `services/iam` в дереве нет ни одним отслеживаемым файлом. Предпосылки при
    этом не стали ложными — они перестали быть НАШИМИ, и спрашивать их здесь
    значит спрашивать о чужом дереве.

    Поэтому предпосылка судится ровно тогда, когда её пара ПРОИЗВОДИТСЯ этим
    деревом (перепись саг это и отвечает). Пара вне дерева — предпосылки
    печатаются с пометкой и в вердикт не идут; вернётся каталог — вернутся и они,
    без правки гейта.

    Прежняя редакция объявляла все три безусловно и на разрезе отвечала
    «НЕТ P2 … / НЕТ P3 …» с выводом «сага снята или переписана». Вывод был
    ЛОЖНЫМ: сага жива, у неё сменился адрес — дерево. Находка, называющая не тот
    предмет, посылает читателя искать не там.
    """
    def has(rel, pattern):
        try:
            return re.search(pattern, open(os.path.join(root, rel), encoding="utf-8").read()) is not None
        except OSError:
            return False
    acc = ("iam", "account")
    return [
        (acc, "P1 CreateAccountMetadata объявляет default_project_id",
         has("proto/kaname/cloud/iam/v1/account.proto",
             r"message CreateAccountMetadata\b[\s\S]{0,600}?\bstring default_project_id\b"),
         "proto/kaname/cloud/iam/v1/account.proto"),
        (acc, "P2 сага создания аккаунта вставляет проект в своей транзакции",
         has("services/iam/internal/apps/kaname/api/account/create.go",
             r"w\.ProjectsW\(\)\.Insert\("),
         "services/iam/internal/apps/kaname/api/account/create.go"),
        (acc, "P3 удаление аккаунта отказывает, пока в нём есть проект",
         has("services/iam/internal/repo/kaname/pg/account_repo.go",
             r"NOT EXISTS \(SELECT 1 FROM projects"),
         "services/iam/internal/repo/kaname/pg/account_repo.go"),
    ]


def premise_verdict(root, prem, absent):
    """→ (строки для печати, код возврата, сколько предпосылок СУДИЛОСЬ).

    ЕДИНСТВЕННЫЙ производитель вердикта о предпосылках, и зовут его ОБА пути —
    прод и самопроверка. Второй кодек здесь запрещён: он расходится с первым
    молча и именно там, где расхождение не видно.

    Предпосылка объявляется ВНЕ ДЕРЕВА ровно когда сошлись ОБА условия: её файла
    здесь нет И её пара этим деревом не производится. Одного условия
    недостаточно — иначе «вне дерева» становится маской: одного удалённого файла
    хватило бы, чтобы предпосылка ЖИВОЙ пары перестала спрашиваться молча.
    """
    rows, rc, judged = [], 0, 0
    for pair, name, ok, where in prem:
        here = os.path.exists(os.path.join(root, where))
        if not here and pair in absent:
            rows.append(("ВНЕ ", name, where,
                         f" — координаты в дереве нет, и пара {pair[0]}/{pair[1]} "
                         f"этим деревом не производится"))
            continue
        judged += 1
        rows.append(("ОК " if ok else "НЕТ", name, where,
                     "" if here else "  ← координаты нет, а пара производится деревом"))
        if not ok:
            rc = 1
    return rows, rc, judged


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
        unknown, _stale, _up, _ab, n_files, n_writer = census_sagas(tmp)
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
        unknown, _stale, _up, _ab, n_files, n_writer = census_sagas(tmp)
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
        _unknown, stale, _up, _ab, _nf, _nw = census_sagas(tmp)
        if any(s[2] == "AccessBindings" for s in stale):
            print("  ОК  основа исключения без предмета НАЗВАНА (уборка потомка исчезла)")
        else:
            print(f"  ПРОВАЛ исчезнувшая уборка потомка не замечена: {stale}")
            rc = 1
        # и зеркальная сторона: как только вызов появляется — молчит.
        with open(os.path.join(d, "delete.go"), "w", encoding="utf-8") as fh:
            fh.write("package account\nfunc g(){ shared.RevokeBindingsInScope(ctx, b); "
                     "w.AccountsW().Delete(ctx, id) }\n")
        _unknown, stale, _up, _ab, _nf, _nw = census_sagas(tmp)
        if not stale:
            print("  ОК  та же запись при живом предмете молчит")
        else:
            print(f"  ПРОВАЛ живое исключение объявлено протухшим: {stale}")
            rc = 1

    # (л) ЗАПИСЬ БЕЗ ПРЕДМЕТА: use-case пары в дереве ЕСТЬ, а объявленной чужой
    #     вставки он не делает — сага снята, запись её пережила. Ось заведена тем
    #     же изменением, которым снята запись о службе доступа: без неё такая
    #     запись не краснеет никогда, потому что обход идёт по файлам дерева.
    with tempfile.TemporaryDirectory() as tmp:
        d = os.path.join(tmp, "services", "vpc", "internal", "apps", "kacho", "api", "gateway")
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "create.go"), "w", encoding="utf-8") as fh:
            fh.write("package gateway\nfunc f(){ w.GatewaysW().Insert(ctx, g) }\n")
        _u, _s, unproduced, _ab, _nf, _nw = census_sagas(tmp)
        if any(x == ("vpc", "gateway", "Addresses") for x in unproduced):
            print("  ОК  запись без предмета НАЗВАНА: пара в дереве есть, объявленной "
                  "вставки нет")
        else:
            print(f"  ПРОВАЛ запись, пережившая свою сагу, не замечена: {unproduced}")
            rc = 1
        # ЗАКОННЫЙ БЛИЗНЕЦ: та же пара, вставка на месте — ось молчит. Без него
        # «краснеет» было бы неотличимо от «краснеет на всякой объявленной паре».
        with open(os.path.join(d, "create.go"), "w", encoding="utf-8") as fh:
            fh.write("package gateway\nfunc f(){ w.GatewaysW().Insert(ctx, g); "
                     "w.AddressesW().Insert(ctx, a) }\n")
        with open(os.path.join(d, "delete.go"), "w", encoding="utf-8") as fh:
            fh.write("package gateway\nfunc g(){ w.AddressesW().DeleteGuarded(ctx, id) }\n")
        _u, stale2, unproduced2, _ab, _nf, _nw = census_sagas(tmp)
        if not unproduced2 and not stale2:
            print("  ОК  та же пара при живой саге и живой уборке — молчит")
        else:
            print(f"  ПРОВАЛ живая пара объявлена протухшей: {unproduced2} / {stale2}")
            rc = 1

    # (л2) АНТИ-МАСКА ПРЕДПОСЫЛОК: координаты нет, а пара ПРОИЗВОДИТСЯ деревом —
    #      это находка, а не «вне дерева». Без этой стороны одного удалённого файла
    #      хватило бы, чтобы предпосылка живой пары перестала спрашиваться молча.
    fake = [(("iam", "account"), "Px выдуманная", False, "нет/такого/файла.go")]
    with tempfile.TemporaryDirectory() as tmp:
        rows, prem_rc, judged = premise_verdict(tmp, fake, absent=[])
        if prem_rc == 1 and judged == 1 and rows[0][0] == "НЕТ":
            print("  ОК  пропавшая координата при ЖИВОЙ паре — находка, а не «вне дерева»")
        else:
            print(f"  ПРОВАЛ анти-маска предпосылок не сработала: {rows} rc={prem_rc}")
            rc = 1
        # ЗАКОННЫЙ БЛИЗНЕЦ: та же пропавшая координата, но пара вне дерева.
        rows, prem_rc, judged = premise_verdict(tmp, fake, absent=[("iam", "account")])
        if prem_rc == 0 and judged == 0 and rows[0][0] == "ВНЕ ":
            print("  ОК  та же пропавшая координата при паре ВНЕ дерева — не находка")
        else:
            print(f"  ПРОВАЛ пара вне дерева всё ещё судится: {rows} rc={prem_rc}")
            rc = 1

    # (м) ПАРА ВНЕ ДЕРЕВА — не находка и не «чисто»: предмет у другого продукта.
    #     Обе стороны: каталога нет — пара названа вне дерева; каталог есть — пара
    #     судится обычными осями. Без второй половины «вне дерева» стало бы маской:
    #     ею накрылась бы любая пара, включая ту, чей код здесь.
    with tempfile.TemporaryDirectory() as tmp:
        d = os.path.join(tmp, "services", "zz", "internal", "apps", "kacho", "api", "widget")
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "create.go"), "w", encoding="utf-8") as fh:
            fh.write("package widget\nfunc f(){ w.WidgetsW().Insert(ctx, x) }\n")
        _u, _s, unproduced3, absent3, _nf, _nw = census_sagas(tmp)
        if set(absent3) == set(KNOWN_COCREATED) and not unproduced3:
            print(f"  ОК  пара вне дерева названа ({len(absent3)}) и находкой не стала")
        else:
            print(f"  ПРОВАЛ вне-дерева не различено: вне {absent3}, без предмета {unproduced3}")
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

    # ПЕРЕПИСЬ САГ ИДЁТ ПЕРВОЙ, и порядок несёт смысл: она отвечает, какие
    # объявленные пары это дерево ПРОИЗВОДИТ, а от этого зависит, чьи предпосылки
    # здесь вообще спрашиваются. Обратный порядок спрашивал бы о чужом дереве и
    # выносил приговор по его отсутствию.
    unknown, stale, unproduced, absent, n_go, n_go_writer = census_sagas(root)
    print(f"=== перепись саг: осмотрено create.go {n_go}, с распознанным писателем "
          f"{n_go_writer}, объявленных пар {len(KNOWN_COCREATED)}, из них вне этого "
          f"дерева {len(absent)}, незнакомых сопутствующих вставок {len(unknown)}, "
          f"оснóв без предмета {len(stale)}, записей без предмета {len(unproduced)} ===")
    if n_go == 0 or n_go_writer == 0:
        print("FAIL: обход use-case'ов создания не нашёл ни одного вызова писателя —")
        print("      идиома изменилась, и перепись саг больше ничего не измеряет.")
        return 1
    for svc, res in absent:
        print(f"  ВНЕ ДЕРЕВА {svc}/{res}: use-case'а создания в этом дереве нет — "
              f"пара судится там, где живёт её код")
    if unproduced:
        print("FAIL: объявленная пара БОЛЬШЕ НЕ ПРОИЗВОДИТСЯ: use-case в дереве есть,")
        print("      а чужой вставки, ради которой запись заведена, он не делает.")
        print("      Сага снята — снимите и запись, иначе она переживёт свой предмет")
        print("      и достанется следующему как слепая зона.")
        for svc, res, w in unproduced:
            print(f"    {svc}/{res}: объявлено w.{w}W().Insert(, в дереве такой вставки нет")
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

    # ПРЕДПОСЫЛКИ. Судится та, чья КООРДИНАТА в этом дереве есть: координата —
    # факт дерева, а выполненность предпосылки — утверждение о её содержимом, и
    # смешивать их нельзя. Предпосылка объявляется вне дерева ровно когда СОШЛИСЬ
    # оба: её файла здесь нет И её пара этим деревом не производится.
    #
    # ОБА УСЛОВИЯ, А НЕ ОДНО — иначе «вне дерева» становится маской: одного
    # удалённого файла хватило бы, чтобы предпосылка живой пары перестала
    # спрашиваться молча. При живой паре пропавшая координата остаётся НАХОДКОЙ.
    #
    # Следствие, которое надо назвать: контракт службы доступа лежит в ЭТОМ
    # дереве (`proto/kaname/…`), поэтому P1 судится и после разреза — платформа
    # по-прежнему владеет контрактом. Уехали только координаты реализации.
    print("=== предпосылки запрета (пропала любая при живой паре — предмета нет) ===")
    prem = premises(root)
    rows, prem_rc, n_judged = premise_verdict(root, prem, absent)
    for mark, name, where, note in rows:
        print(f"  {mark} {name}  [{where}]" + (f"{note}" if note else ""))
    print(f"  (предпосылок судилось {n_judged} из {len(prem)}; вне дерева "
          f"{len(prem) - n_judged})")
    rc = rc or prem_rc
    if rc:
        print("FAIL: предпосылка исчезла — сага сопутствующего потомка снята или")
        print("      переписана. Гейт обязан быть перенацелен или удалён вместе с ней,")
        print("      а не оставлен зелёным на предмете, которого больше нет.")
        return 1

    # КЛИЕНТСКИЕ ПАРЫ — те, чью уборку делает КЕЙС (значение None) и чей
    # производитель в этом дереве. Только они требуют, чтобы обход коллекций
    # что-то НАШЁЛ: без них «аккаунтов создано 0» означает не поломку обхода, а
    # отсутствие предмета, и читать это надо по-разному.
    client_pairs = sorted(
        (svc, res, w)
        for (svc, res), ws in KNOWN_COCREATED.items()
        for w, why in ws.items()
        if why is None and (svc, res) not in absent)

    findings, (cols, cases, steps, born, dels) = scan(root)
    print(f"=== уборка против саги: осмотрено коллекций {cols}, кейсов {cases}, "
          f"шагов {steps}; аккаунтов создано кейсами {born}, удалений аккаунта "
          f"по имени {dels}; клиентских пар в этом дереве {len(client_pairs)} ===")
    if cols == 0:
        print("FAIL: обход не нашёл НИ ОДНОЙ коллекции — «ноль находок» стало")
        print("      неотличимо от «ноль прочитанного».")
        return 1
    if born == 0:
        if client_pairs:
            print("FAIL: коллекции прочитаны, а аккаунтов, созданных кейсом, НЕТ —")
            print("      предмет запрета потерян либо форма захвата в генераторе изменилась.")
            for svc, res, w in client_pairs:
                print(f"      клиентская пара {svc}/{res} → {w} требует, чтобы обход её нашёл")
            return 1
        print("  БЕСПРЕДМЕТНО: клиентских пар в этом дереве нет, и ни одна коллекция")
        print("      аккаунтов не заводит. Обход ОСТАЁТСЯ вооружённым — находка ниже")
        print("      краснеет при любой паре: коллекция дерева ходит к тому же краю, и")
        print("      заведи она аккаунт, сага потомка приедет вместе с ним.")

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
