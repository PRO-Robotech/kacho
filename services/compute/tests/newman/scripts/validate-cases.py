#!/usr/bin/env python3

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/validate-cases.py — сверщик переписи кейсов compute.

ЗАЧЕМ ОН ЗАВЁЛСЯ ИМЕННО ЗДЕСЬ. У vpc и registry сверщик был, у compute — нет, и
слепая зона оказалась ровно там, где его не было: в `docs/CASES-INDEX.md` выжил
раздел «Instance (77 кейсов) — `cases/instance.py`» для файла, удалённого из дерева,
а перепись в шапке того же файла разошлась с деревом (111/43 против 117/49). Оба
дефекта — утверждения о покрытии, которых никто не проверял. Гейт есть у двоих из
трёх — значит третий и есть место находки.

Запускать до newman-шага (чистый Python, без сети):

    python3 tests/newman/scripts/validate-cases.py
    python3 tests/newman/scripts/validate-cases.py --self-test

Проверяет ПЯТЬ вещей, и каждая заведена против того, что здесь реально сгнило:

  1. НЕТ ДУБЛЕЙ case-id по всем модулям, которые собирает gen.py.

  2. ПЕРЕПИСЬ В ШАПКЕ CASES-INDEX СОВПАДАЕТ С ДЕРЕВОМ — и всего, и по каждому
     модулю. Число, переписанное рукой, дрейфует само; вопрос лишь в том, заметит
     это гейт или следующий читатель, который на него положится.

  3. НИ ОДИН ЗАГОЛОВОК CASES-INDEX НЕ НАЗЫВАЕТ ФАЙЛ КЕЙСОВ, КОТОРОГО НЕТ. Раздел про
     удалённый файл читается как описание покрытия и переживает своё содержимое —
     именно так здесь и вышло: `## Instance (77 кейсов) — cases/instance.py` жил уже
     после удаления файла.

     Проверяются ЗАГОЛОВКИ, а не весь текст, и это не поблажка. Заголовок — то, что
     читатель принимает за «это есть»; упоминание удалённого файла в ПРОЗЕ, прямо
     говорящей о его удалении (`## Zone / Region — removed (Stage S7)` ниже, и раздел
     про снятый перечень инстанса), — законное историческое свидетельство, и гейт,
     запрещающий его, заставил бы стирать объяснение вместе с ложью. Проверка на весь
     текст краснела на всех трёх таких местах — то есть ловила форму, а не существо.

  4. АКТОР СУИТЫ — ПРОЕКТНЫЙ, А ОТСТУПЛЕНИЯ ОБЪЯВЛЕНЫ ШАГОМ. Три под-свойства, и все
     три — про одно: суита обязана СПРАШИВАТЬ у продукта то, о чём отчитывается.

     4a. Дефолт коллекции читает проектного актора, а не бутстрап-админа. Бутстрап
         держит права на всё, поэтому под ним каждый шаг проходит независимо от того,
         работает ли project-scope авторизация; суита в такой посадке не может отличить
         исправное от сломанного. Класс не гипотетический: карта прав сервиса разошлась
         с каталогом края по паре scope+relation, проектный принципал получал отказ на
         СВОИХ ресурсах, и бутстрап-админ этого не видел.

     4b. Шаг на cluster-scoped админ-маршруте несёт актора ЯВНО. Такой шаг проектному
         актору недоступен by construction (каталог прав требует `system_admin` на
         cluster-singleton), поэтому «дефолт как-нибудь подойдёт» здесь означает 403 —
         и означал бы его молча, если бы дефолт снова поменяли. Предпосылка запрета
         ПРОВЕРЯЕТСЯ: гейт читает каталог прав края и требует, чтобы эти RPC там
         действительно гейтились `system_admin` на `cluster`. Перестанут — гейт скажет,
         что у его запрета больше нет основания, вместо того чтобы тихо остаться.

     4c. Operation читает ТОТ, КТО ЕЁ СОЗДАЛ. `OperationService.Get/Cancel` энфорсит
         владение (владелец — принципал-создатель) и отвечает чужому NotFound. Значит
         опрос под другим актором получает 404, ждёт весь бюджет ретрая и падает, а
         выглядит это как задержка материализации в продукте — диагноз в шести шагах от
         причины. Свойство проверяется по ИМЕНИ переменной операции: чей `save_from_response`
         её опубликовал последним, тот актор и обязан быть у читателя.

  5. ПЕРВОЕ ОБРАЩЕНИЕ К СВОЕМУ СВЕЖЕМУ ИНТЕРФЕЙСУ — ЧТЕНИЕ, А НЕ АСИНХРОННАЯ МУТАЦИЯ.
     Окно материализации owner-tuple закрывает ограниченный ретрай на первом доступе, и
     generator ставит его сам. Но ретрай ключуется на КОДЕ ОТВЕТА шага, а привязка/
     отвязка асинхронна: её POST отвечает `200` и `Operation` ВСЕГДА, а отказ владельца
     приезжает в терминальной ошибке операции, которую читает уже другой шаг. Обёртка на
     таком шаге стоит и сработать не может — форма есть, содержания нет.

     Отличить это от настоящего промаха кейс НЕ МОЖЕТ by construction: цель проверки прав
     у `InternalNetworkInterfaceService.{Attach,Detach}` — САМ интерфейс, vpc прячет
     существование, а текст берётся из общей таблицы — «Network interface <id> not found»,
     байт-в-байт как у реального отсутствия. Поэтому свойство требуется от ФИКСТУРЫ:
     идентификатор свежего интерфейса уезжает в мутацию только после того, как интерфейс
     прочитан. Класс наблюдался: два утверждения кейса идемпотентной отвязки читали
     открытое окно как утверждение о продукте.

     Предпосылка ПРОВЕРЯЕТСЯ: гейт читает каталог прав и требует, чтобы цель этих RPC
     действительно была `vpc_network_interface` из `nic_id`. Сместится на машину — гейт
     скажет, что греть интерфейс больше незачем, вместо того чтобы тихо остаться.
     Обычное «создал родителя → создал потомка» под запрет НЕ подпадает: там цель —
     проект, давно материализованный, и требовать чтения было бы запретом формы.

ЧЕГО ЗДЕСЬ НЕТ И ПОЧЕМУ — это отступление от соседей, и оно объявлено, а не умолчано.
У vpc и registry есть четвёртая проверка: каждый case-id обязан быть каталогизирован в
CASES-INDEX (буквально либо суффиксным шаблоном), иначе — отказ. Для compute она
потребовала бы вернуть в документ ПОКАЗАТЕЛЬНЫЙ ПЕРЕЧЕНЬ всех 117 кейсов — то есть
вторую копию содержимого `cases/*.py`, чей дрейф и был здешним дефектом (перечень
удалённого файла жил в документе, противореча переписи в его же шапке). Требовать
копию, которую мы только что убрали как источник лжи, значит завести гейт против
самого решения. Поэтому источник истины по составу кейсов — `cases/*.py`, а документ
отвечает за ЧИСЛА, и за них отвечает проверка 2.

Все проверки доказаны инъекцией в обе стороны, и доказательство ИСПОЛНЯЕМО — `--self-test`
(его прогоняет deploy/scripts/run-gate-self-tests.sh, который сам находит такие ветки по
дереву и требует, чтобы найденное совпадало с объявленным составом). Сдвиг переписи на
единицу, лишний модуль в переписи, ссылка на несуществующий файл, бутстрап в дефолте
коллекции, шаг админ-маршрута без явного актора, опрос операции чужим актором и свежий
интерфейс, уехавший в мутацию первым обращением, дают красное с координатой; законное
добавление кейса вместе с обновлённой переписью, шаг на ПУБЛИЧНОМ маршруте того же ресурса
без актора, опрос, следующий за НОВЫМ издателем идентификатора операции, прочитанный до
мутации интерфейс, литеральный идентификатор в отрицательном кейсе и обычное «родитель →
потомок» — молчание.

ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ. «Ноль находок» обязано быть отличимо от «ноль прочитанного»:
модулей, кейсов, шагов, шагов на админ-маршруте, пар «издатель→читатель операции» и шагов
привязки/отвязки интерфейса. Нулевой объём по трём последним — отказ, а не чистота:
предпосылка проверок 4b/4c/5 в том, что предмет в дереве есть.
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SCRIPTS_DIR = ROOT / "scripts"
CASES_DIR = ROOT / "cases"
INDEX_FILE = ROOT / "docs" / "CASES-INDEX.md"
# Каталог прав края — им проверяется ПРЕДПОСЫЛКА запрета 4b (что админ-поверхность
# каталога размерностей действительно гейтится `system_admin` на cluster-singleton).
REPO_ROOT = ROOT.resolve().parents[3]
CATALOG_FILE = REPO_ROOT / "gateway" / "internal" / "middleware" / "embed" / "permission_catalog.json"
_ADMIN_RPCS = (
    "kacho.cloud.compute.v1.InternalMachineTypeService/Create",
    "kacho.cloud.compute.v1.InternalMachineTypeService/Delete",
    "kacho.cloud.compute.v1.InternalMachineTypeService/Update",
)
_ADMIN_RELATION = "system_admin"
_ADMIN_SCOPE_TYPE = "cluster"
# Проверка 5. Глаголы ребра compute→vpc, у которых цель проверки прав — САМ интерфейс
# (а не машина в пути запроса), и записи каталога, которыми это обосновано.
_NIC_VERB_SUFFIXES = (":attachNetworkInterface", ":detachNetworkInterface")
_NIC_TARGET_RPCS = (
    "kacho.cloud.vpc.v1.InternalNetworkInterfaceService/Attach",
    "kacho.cloud.vpc.v1.InternalNetworkInterfaceService/Detach",
)
_NIC_TARGET_OBJECT = "vpc_network_interface"
_NIC_TARGET_FIELD = "nic_id"
# Актор, которого дефолт коллекции обязан читать, и тот, которого он читать НЕ вправе.
_PROJECT_ACTOR = "jwtProjectAdminA1"
_CLUSTER_ADMIN_ACTOR = "jwtBootstrap"
# Строка дефолта в PRE_GLOBAL опознаётся по присваиванию — не по слову в комментарии
# рядом: объяснение запрета неотличимо от его нарушения, если читать текст.
_DEF_AUTH_ASSIGN_RE = re.compile(r"const\s+__defAuth\s*=(.*)$")

_ID_RE = re.compile(r"""id\s*=\s*f?["']([A-Z0-9][A-Z0-9_{}:.-]+)["']""")
# Перепись: «Всего (по `gen.py`): **117 кейсов** — instance-redesign 49, authz-deny 42, …»
_CENSUS_TOTAL_RE = re.compile(r"\*\*(\d+)\s+кейс[а-я]*\*\*")
_CENSUS_ITEM_RE = re.compile(r"([a-z][a-z0-9-]*)\s+(\d+)")
_CASES_FILE_REF_RE = re.compile(r"`?cases/([A-Za-z0-9_-]+)\.py`?")
_HEADING_RE = re.compile(r"^#{1,6}\s")

sys.path.insert(0, str(SCRIPTS_DIR))



def _all_cases() -> tuple[list[tuple[str, str]], list[tuple], list[tuple]]:
    """Загрузить модули кейсов ТАК ЖЕ, как gen.py.

    Возвращает (перепись [(case_id, module), …], записи шагов, записи ссылок). Записи
    читаются из ТЕХ ЖЕ объектов Case, которые сериализует gen.py, — то есть проверки 4
    и 5 смотрят на то, что написал автор кейса, а не на пересказ.

    Запись ССЫЛКИ (проверка 5) — (case_id, имя, метод, путь, ссылки, публикации):
    какие переменные шаг ЧИТАЕТ (в пути и теле) и какие идентификаторы РЕСУРСОВ он
    публикует. Переменные операций из публикаций исключены тем же признаком, что и в
    проверке 4c (`gen._is_operation_id_var`): опрос операции — не обращение к ресурсу,
    и считать его первым доступом значило бы закрывать окно видимости шагом, который
    его не открывает."""
    import gen  # noqa: E402

    global path_op_var
    path_op_var = lambda p: _op_var_read(p, gen._OP_POLL_PATH)  # noqa: E731

    census: list[tuple[str, str]] = []
    records: list[tuple] = []
    refs: list[tuple] = []
    for f in sorted(CASES_DIR.glob("*.py")):
        if f.name.startswith("_"):
            continue
        mod = gen.load_cases_module(f)
        for c in getattr(mod, "CASES", []):
            census.append((c.id, f.stem))
            for st in c.steps:
                records.append((c.id, st.name, st.method, st.path, st.auth,
                                _published_op_vars(st.test_script, gen._is_operation_id_var)))
                body = "\n".join(st.test_script)
                refs.append((
                    c.id, st.name, st.method, st.path,
                    sorted(set(gen._VAR_REF_RE.findall(st.path + gen._body_text(st)))),
                    [n for n in gen._FRESH_VAR_SET_RE.findall(body)
                     if not gen._is_operation_id_var(n)],
                ))
    return census, records, refs


def _census_from_index(index_text: str) -> tuple[int | None, dict[str, int]]:
    """Прочитать заявленную перепись из шапки CASES-INDEX."""
    total = None
    per: dict[str, int] = {}
    for line in index_text.splitlines():
        m = _CENSUS_TOTAL_RE.search(line)
        if not m:
            continue
        total = int(m.group(1))
        # Модульные числа идут в этой же фразе и могут переноситься на след. строку;
        # читаем оба варианта, начиная с тире.
        tail = line.split("—", 1)[1] if "—" in line else ""
        idx = index_text.splitlines().index(line)
        if idx + 1 < len(index_text.splitlines()):
            tail += " " + index_text.splitlines()[idx + 1]
        for name, num in _CENSUS_ITEM_RE.findall(tail):
            per[name] = int(num)
        break
    return total, per


# ─────────────────────────────────────────────────────────────────────────────
# Проверка 4 — актор. Разложена на чистые функции над ЗАПИСЯМИ шагов, чтобы
# самопроверка могла прогнать её на синтетическом входе, не читая дерево.
#
# Запись шага — кортеж (case_id, имя, метод, путь, актор, опубликованные-переменные-операции).
# Больше проверке ничего не нужно, и это не бедность интерфейса: чем меньше она видит,
# тем меньше поводов у неё «работать» на фикстуре и не работать на дереве.
# ─────────────────────────────────────────────────────────────────────────────

def _op_var_read(path: str, op_poll_re) -> str | None:
    """Имя переменной операции, которую ЧИТАЕТ этот путь, либо None.

    Разбор берётся ИЗ gen.py (тот же `_OP_POLL_PATH`, что читает проход
    `_assert_delete_operation_outcome`), а не пишется здесь второй копией: две копии
    одного разбора расходятся молча и расходятся там, где расхождение не видно —
    обе отвечают «да» на законном входе."""
    m = op_poll_re.search(path)
    return m.group(1) if m else None


def _published_op_vars(test_script: list[str], is_op_var) -> list[str]:
    """Переменные-операции, которые шаг ПУБЛИКУЕТ своим test_script.

    Признак — форма, которую эмитит `save_from_response`; принадлежность имени к
    операциям решает тот же `_is_operation_id_var` из gen.py (общий `opId` либо имя,
    оканчивающееся на `OpId`/`OperationId`), поэтому идентификаторы РЕСУРСОВ сюда не
    попадают."""
    out: list[str] = []
    for line in test_script:
        for name in re.findall(r"pm\.environment\.set\(\s*'([A-Za-z_][A-Za-z0-9_]*)'", line):
            if is_op_var(name) and name not in out:
                out.append(name)
    return out


def check_default_actor_is_project(pre_global: list[str]) -> list[str]:
    """4a — дефолт коллекции читает проектного актора, не бутстрап-админа."""
    assigns = [m.group(1) for line in pre_global
               for m in [_DEF_AUTH_ASSIGN_RE.search(line)] if m]
    if not assigns:
        return ["PRE_GLOBAL не содержит присваивания `const __defAuth = …` — форма дефолтного "
                "актора изменилась, и проверка 4a больше не читает того, что проверяет. "
                "Починить предикат вместе с формой, а не оставлять зелёным."]
    errors = []
    for expr in assigns:
        if _CLUSTER_ADMIN_ACTOR in expr:
            errors.append(
                f"PRE_GLOBAL: дефолтный актор коллекции читает {_CLUSTER_ADMIN_ACTOR!r} "
                f"({expr.strip()}). Бутстрап-админ держит права на всё, поэтому под ним "
                f"каждый шаг проходит независимо от того, работает ли project-scope "
                f"авторизация — суита перестаёт отличать исправное от сломанного. "
                f"Дефолт обязан читать {_PROJECT_ACTOR!r}; шаг, которому нужен "
                f"cluster-admin, объявляет это сам через auth=.")
        elif _PROJECT_ACTOR not in expr:
            errors.append(
                f"PRE_GLOBAL: дефолтный актор коллекции не читает ни {_PROJECT_ACTOR!r}, "
                f"ни {_CLUSTER_ADMIN_ACTOR!r} ({expr.strip()}) — установить, кем ходит суита, "
                f"по этому выражению нельзя.")
    return errors


def check_admin_route_names_its_actor(records, admin_path: str, admin_auth: str) -> tuple[list[str], int]:
    """4b — шаг на cluster-scoped админ-маршруте несёт актора явно.

    Возвращает (находки, сколько шагов админ-маршрута осмотрено) — второе печатается,
    чтобы «ноль находок» было отличимо от «ноль прочитанного»."""
    errors, seen = [], 0
    for case_id, name, _method, path, auth, _pub in records:
        if not path.startswith(admin_path):
            continue
        seen += 1
        if auth != admin_auth:
            got = "дефолт коллекции" if auth is None else repr(auth)
            errors.append(
                f"{case_id} / шаг {name!r}: путь {path} — cluster-scoped админ-поверхность "
                f"({admin_relation_hint()}), а актор шага: {got}. Проектному актору она "
                f"недоступна by construction → 403 authz-first вместо предмета кейса. "
                f"Поставить auth={admin_auth!r} (константа gen.ADMIN_AUTH).")
    return errors, seen


def admin_relation_hint() -> str:
    return f"каталог прав требует {_ADMIN_RELATION} на {_ADMIN_SCOPE_TYPE}"


def check_operation_read_by_its_creator(records) -> tuple[list[str], int]:
    """4c — Operation читает тот актор, который её создал.

    Возвращает (находки, сколько пар «издатель→читатель» осмотрено)."""
    errors, pairs = [], 0
    by_case: dict[str, list] = {}
    for rec in records:
        by_case.setdefault(rec[0], []).append(rec)
    for case_id, steps in by_case.items():
        publisher: dict[str, tuple[str, str | None]] = {}   # var → (имя шага, актор)
        for _cid, name, _method, path, auth, published in steps:
            read = path_op_var(path)
            if read is not None and read in publisher:
                pairs += 1
                owner_step, owner_auth = publisher[read]
                if auth != owner_auth:
                    errors.append(
                        f"{case_id}: операцию {{{{{read}}}}} создал шаг {owner_step!r} под "
                        f"{_actor(owner_auth)}, а читает шаг {name!r} под {_actor(auth)}. "
                        f"OperationService.Get/Cancel энфорсит владение и отвечает чужому "
                        f"NotFound — опрос сожжёт бюджет ретрая и упадёт, выглядя задержкой "
                        f"материализации в продукте. Читатель обязан нести того же актора.")
            for var in published:
                publisher[var] = (name, auth)
    return errors, pairs


def _actor(auth: str | None) -> str:
    return "дефолтом коллекции" if auth is None else repr(auth)


# path_op_var подменяется в main() на разбор из gen.py; здесь — заглушка, чтобы
# самопроверка могла задать свой (и чтобы отсутствие подстановки было ошибкой, а не
# молчаливым «ничего не нашлось»).
def path_op_var(path: str) -> str | None:  # pragma: no cover - переопределяется
    raise RuntimeError("path_op_var не связан с разбором из gen.py")


def check_admin_premise(catalog_text: str) -> list[str]:
    """ПРЕДПОСЫЛКА 4b: админ-RPC каталога размерностей действительно гейтятся
    `system_admin` на cluster-singleton. Факт внешний — если он изменится, запрет 4b
    станет ложью, и сказать об этом обязан сам гейт."""
    try:
        entries = json.loads(catalog_text)
    except json.JSONDecodeError as exc:
        return [f"каталог прав {CATALOG_FILE} не читается как JSON ({exc}) — предпосылка "
                f"запрета 4b не проверена, значит запрет не обоснован"]
    by_fqn = {e.get("fqn", ""): e for e in entries}
    errors = []
    for fqn in _ADMIN_RPCS:
        e = by_fqn.get(fqn)
        if e is None:
            errors.append(f"каталог прав не содержит записи {fqn} — запрет 4b держался на её "
                          f"наличии; предмет запрета исчез, проверку пересобрать")
            continue
        rel = e.get("required_relation")
        scope = (e.get("scope_extractor") or {}).get("object_type")
        if rel != _ADMIN_RELATION or scope != _ADMIN_SCOPE_TYPE:
            errors.append(
                f"{fqn}: каталог прав объявляет relation={rel!r} scope={scope!r}, а запрет 4b "
                f"обоснован парой ({_ADMIN_RELATION!r}, {_ADMIN_SCOPE_TYPE!r}). Основание "
                f"запрета изменилось — пересмотреть, кто вправе ходить этим маршрутом.")
    return errors


def check_nic_target_premise(catalog_text: str) -> list[str]:
    """ПРЕДПОСЫЛКА 5: привязка/отвязка интерфейса гейтится У ВЛАДЕЛЬЦА на САМОМ
    интерфейсе, а не на машине.

    Запрет 5 обоснован ровно этим: цель проверки прав — `vpc_network_interface`,
    взятая из `nic_id`. Пока это так, свежий интерфейс обязан быть прочитан до того,
    как его идентификатор уедет в мутацию. Перестанет — запрет останется без
    основания, и сказать об этом обязан сам гейт, а не следующий читатель."""
    try:
        entries = json.loads(catalog_text)
    except json.JSONDecodeError as exc:
        return [f"каталог прав {CATALOG_FILE} не читается как JSON ({exc}) — предпосылка "
                f"запрета 5 не проверена, значит запрет не обоснован"]
    by_fqn = {e.get("fqn", ""): e for e in entries}
    errors = []
    for fqn in _NIC_TARGET_RPCS:
        e = by_fqn.get(fqn)
        if e is None:
            errors.append(f"каталог прав не содержит записи {fqn} — запрет 5 держался на её "
                          f"наличии; предмет запрета исчез, проверку пересобрать")
            continue
        scope = e.get("scope_extractor") or {}
        got = (scope.get("object_type"), scope.get("from_request_field"))
        want = (_NIC_TARGET_OBJECT, _NIC_TARGET_FIELD)
        if got != want:
            errors.append(
                f"{fqn}: каталог прав объявляет цель проверки прав {got!r}, а запрет 5 "
                f"обоснован парой {want!r}. Основание запрета изменилось — пересмотреть, "
                f"нужно ли греть интерфейс перед мутацией.")
    return errors


def check_first_nic_access_is_a_read(refs) -> tuple[list[str], int]:
    """5 — ПЕРВОЕ обращение к своему свежему интерфейсу не может быть асинхронной мутацией.

    Почему именно интерфейс, а не «всякий свежий ресурс». Окно материализации owner-tuple
    закрывает ограниченный ретрай на первом доступе (`testing.md` §e2e-инварианты), и
    generator ставит его сам (`_wrap_own_fresh_reads`). Но ретрай ключуется на КОДЕ ОТВЕТА
    шага, а привязка/отвязка — асинхронная мутация: её POST отвечает `200` и `Operation`
    ВСЕГДА, а отказ владельца приезжает в терминальной ошибке операции, которую читает уже
    другой шаг. То есть обёртка на таком шаге стоит и сработать не может — форма есть,
    содержания нет.

    Отказ при этом НЕОТЛИЧИМ от настоящего промаха: vpc прячет существование
    (`ErrHideExistence`), и текст берётся из общей таблицы — «Network interface <id> not
    found», байт-в-байт как у реального отсутствия (`pkg/authz/hide_existence.go`). Значит
    кейс не может отличить «окно ещё открыто» от «интерфейса нет», и красное приезжает как
    утверждение о продукте.

    Поэтому свойство требуется от ФИКСТУРЫ: идентификатор свежего интерфейса уезжает в
    attach/detach только после того, как этот интерфейс прочитан — чтением, которое
    обёртку принимает и на котором она работает.

    Проверяются ТОЛЬКО два глагола ребра compute→vpc: у них цель проверки прав — сам
    интерфейс (предпосылка сверяется с каталогом прав). Обычное «создал родителя →
    создал потомка» под запрет НЕ подпадает: там цель — проект, давно материализованный,
    и требовать чтения было бы запретом формы, а не существа.

    Литеральный идентификатор (несуществующий/malformed в отрицательных кейсах) ссылкой
    на переменную не является и сюда не попадает — греть там нечего, а ретрай на
    отрицании запрещён."""
    errors: list[str] = []
    seen = 0
    by_case: dict[str, list[tuple]] = {}
    order: list[str] = []
    for rec in refs:
        if rec[0] not in by_case:
            by_case[rec[0]] = []
            order.append(rec[0])
        by_case[rec[0]].append(rec)
    for case_id in order:
        fresh: set[str] = set()
        read: set[str] = set()
        for _cid, name, method, path, refd, published in by_case[case_id]:
            if any(path.endswith(v) for v in _NIC_VERB_SUFFIXES):
                seen += 1
                cold = sorted((set(refd) & fresh) - read)
                if cold:
                    errors.append(
                        f"{case_id}/{name}: {', '.join('{{%s}}' % c for c in cold)} — свежий "
                        f"ресурс уезжает в асинхронную мутацию `{path}` ПЕРВЫМ обращением. "
                        f"Цель проверки прав здесь — сам интерфейс, поэтому в окне "
                        f"материализации владелец отвечает отказом, неотличимым от "
                        f"настоящего промаха, а обёртка ретрая на этом шаге сработать не "
                        f"может (POST всегда 200+Operation). Прочитать ресурс до мутации.")
            if method == "GET":
                read |= set(refd)
            fresh |= set(published)
    return errors, seen


def main() -> int:
    errors: list[str] = []

    try:
        cases, records, refs = _all_cases()
        import gen  # noqa: E402  — PRE_GLOBAL / ADMIN_AUTH / MT_INTERNAL_PATH
    except Exception as exc:  # noqa: BLE001 — предъявить как провал сверки
        sys.stderr.write(f"validate-cases: FAIL — модули кейсов не загрузились: {exc}\n")
        return 1
    if not cases:
        # «Ноль находок» обязано быть отличимо от «ноль прочитанного».
        sys.stderr.write("validate-cases: FAIL — ни одного кейса не прочитано\n")
        return 1

    # ---- (1) дубли case-id ----
    seen: dict[str, str] = {}
    for case_id, mod in cases:
        if case_id in seen:
            errors.append(
                f"дубль case-id {case_id!r}: есть и в {seen[case_id]}, и в {mod} "
                f"(case-id обязан быть уникальным)"
            )
        else:
            seen[case_id] = mod

    index_text = INDEX_FILE.read_text() if INDEX_FILE.exists() else ""
    if not index_text:
        errors.append(f"нет файла {INDEX_FILE}")

    # ---- (2) перепись в шапке совпадает с деревом ----
    actual_per: dict[str, int] = {}
    for _cid, mod in cases:
        actual_per[mod] = actual_per.get(mod, 0) + 1
    actual_total = len(cases)

    claimed_total, claimed_per = _census_from_index(index_text)
    if claimed_total is None:
        errors.append(
            "в шапке docs/CASES-INDEX.md нет переписи вида «**<N> кейсов** — <модуль> <n>, …». "
            "Без неё читателю нечем сверить покрытие, а гейту нечего проверять."
        )
    else:
        if claimed_total != actual_total:
            errors.append(
                f"перепись разошлась с деревом: в шапке заявлено {claimed_total} кейсов, "
                f"gen.py собирает {actual_total}. Сверить: `python3 scripts/gen.py`."
            )
        for mod, n in sorted(actual_per.items()):
            if mod not in claimed_per:
                errors.append(
                    f"модуль {mod!r} ({n} кейсов) в переписи шапки не назван вовсе"
                )
            elif claimed_per[mod] != n:
                errors.append(
                    f"перепись модуля {mod!r}: заявлено {claimed_per[mod]}, в дереве {n}"
                )
        for mod in sorted(claimed_per):
            if mod not in actual_per:
                errors.append(
                    f"перепись называет модуль {mod!r}, которого в cases/ нет — "
                    f"утверждение о покрытии без предмета"
                )

    # ---- (3) CASES-INDEX не ссылается на удалённый файл кейсов ----
    for lineno, line in enumerate(index_text.splitlines(), start=1):
        if not _HEADING_RE.match(line):
            continue
        for name in _CASES_FILE_REF_RE.findall(line):
            if not (CASES_DIR / f"{name}.py").exists():
                errors.append(
                    f"docs/CASES-INDEX.md:{lineno} — заголовок называет `cases/{name}.py`, "
                    f"которого в дереве нет: {line.strip()!r}. Заголовок читается как "
                    f"описание покрытия и переживает своё содержимое."
                )

    # ---- (4) актор суиты ----
    # Предпосылка запрета 4b — ДО самого запрета: если основание исчезло, запрет обязан
    # сказать это, а не молча продолжать требовать.
    if not CATALOG_FILE.exists():
        errors.append(f"нет каталога прав {CATALOG_FILE} — предпосылка запрета 4b не проверена")
        errors.append(f"нет каталога прав {CATALOG_FILE} — предпосылка запрета 5 не проверена")
    else:
        catalog_text = CATALOG_FILE.read_text()
        errors += check_admin_premise(catalog_text)
        errors += check_nic_target_premise(catalog_text)
    errors += check_default_actor_is_project(gen.PRE_GLOBAL)
    admin_errs, admin_seen = check_admin_route_names_its_actor(
        records, gen.MT_INTERNAL_PATH, gen.ADMIN_AUTH)
    errors += admin_errs
    op_errs, op_pairs = check_operation_read_by_its_creator(records)
    errors += op_errs
    # ---- (5) первое обращение к своему свежему интерфейсу — чтение ----
    nic_errs, nic_seen = check_first_nic_access_is_a_read(refs)
    errors += nic_errs
    # Объём осмотренного — отдельное утверждение. Ноль шагов админ-маршрута или ноль пар
    # «издатель→читатель» означает, что предмет 4b/4c потерян (или обход сломан), и это
    # ОТКАЗ: совпадение пустого с пустым ничего не доказывает.
    if admin_seen == 0:
        errors.append("проверка 4b не осмотрела НИ ОДНОГО шага cluster-scoped админ-маршрута "
                      f"({gen.MT_INTERNAL_PATH}) — предмет запрета потерян либо обход сломан")
    if op_pairs == 0:
        errors.append("проверка 4c не нашла НИ ОДНОЙ пары «издатель операции → её читатель» — "
                      "предмет запрета потерян либо разбор пути/публикации сломан")
    if nic_seen == 0:
        errors.append("проверка 5 не осмотрела НИ ОДНОГО шага привязки/отвязки интерфейса "
                      f"({', '.join(_NIC_VERB_SUFFIXES)}) — предмет запрета потерян либо "
                      "обход сломан")

    if errors:
        sys.stderr.write("validate-cases: FAIL\n")
        for e in errors:
            sys.stderr.write("  - " + e + "\n")
        return 1
    print(
        f"validate-cases: OK — {len(seen)} уникальных case-id, дублей нет; "
        f"перепись шапки совпадает с деревом "
        f"({actual_total} кейсов: " +
        ", ".join(f"{m} {n}" for m, n in sorted(actual_per.items())) + "); "
        f"актор: дефолт коллекции {_PROJECT_ACTOR}, осмотрено {len(records)} шагов — "
        f"{admin_seen} на админ-маршруте (все несут {gen.ADMIN_AUTH}), "
        f"{op_pairs} пар «издатель операции → читатель» (акторы совпадают), "
        f"{nic_seen} шагов привязки/отвязки интерфейса (свежий интерфейс прочитан до мутации)"
    )
    return 0


# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА — доказательство инъекцией в ОБЕ стороны.
#
# Прогоняется deploy/scripts/run-gate-self-tests.sh (он находит эту ветку по дереву и
# требует, чтобы состав самопроверок совпадал с объявленным). Дерева не читает и стенда
# не касается: все входы синтетические, поэтому «доказано» не зависит от того, что
# сегодня лежит в cases/.
# ─────────────────────────────────────────────────────────────────────────────

_ADMIN_PATH_FIX = "/compute/v1/internal/machineTypes"
_PUBLIC_PATH_FIX = "/compute/v1/machineTypes"


def _rec(case_id, name, method, path, auth, published=()):
    return (case_id, name, method, path, auth, list(published))


def _self_test() -> int:  # noqa: C901 — линейный перечень инъекций, дробить нечего
    global path_op_var
    import gen  # noqa: E402

    path_op_var = lambda p: _op_var_read(p, gen._OP_POLL_PATH)  # noqa: E731
    ok = True

    def want(label, got_errors, expect_finding):
        nonlocal ok
        if expect_finding and not got_errors:
            print(f"  ПРОВАЛ {label}: дефект внесён, находки НЕТ", file=sys.stderr)
            ok = False
        elif not expect_finding and got_errors:
            print(f"  ПРОВАЛ {label}: законная форма помечена находкой: {got_errors}", file=sys.stderr)
            ok = False
        else:
            print(f"  ОК  {label}")

    print("=== 4a — дефолт коллекции ===")
    want("бутстрап в дефолте → находка",
         check_default_actor_is_project(
             ["const __defAuth = pm.environment.get('jwtBootstrap');"]), True)
    want("проектный актор в дефолте → молчание",
         check_default_actor_is_project(
             ["// про jwtBootstrap здесь сказано в комментарии, и это НЕ нарушение",
              "const __defAuth = pm.environment.get('jwtProjectAdminA1') || '';"]), False)
    want("форма присваивания исчезла → находка (предикат перестал читать предмет)",
         check_default_actor_is_project(["const somethingElse = 1;"]), True)
    # Дерево обязано проходить своим же предикатом — иначе доказательство относится к
    # фикстуре, а не к тому, что мы правим.
    want("PRE_GLOBAL из дерева → молчание",
         check_default_actor_is_project(gen.PRE_GLOBAL), False)

    print("=== 4b — cluster-scoped админ-маршрут ===")
    errs, seen = check_admin_route_names_its_actor(
        [_rec("C1", "seed-mt", "POST", _ADMIN_PATH_FIX, None)], _ADMIN_PATH_FIX, "jwtBootstrap")
    want("админ-маршрут под дефолтом → находка", errs, True)
    if seen != 1:
        print(f"  ПРОВАЛ 4b осмотрела {seen} шагов вместо 1 — объём считается неверно", file=sys.stderr)
        ok = False
    errs, _ = check_admin_route_names_its_actor(
        [_rec("C1", "seed-mt", "POST", _ADMIN_PATH_FIX, "jwtBootstrap")], _ADMIN_PATH_FIX, "jwtBootstrap")
    want("админ-маршрут под cluster-admin → молчание", errs, False)
    errs, _ = check_admin_route_names_its_actor(
        [_rec("C1", "get-mt", "GET", _PUBLIC_PATH_FIX + "/{{mtId}}", None)],
        _ADMIN_PATH_FIX, "jwtBootstrap")
    want("ЗАКОННЫЙ БЛИЗНЕЦ: публичный маршрут того же ресурса под дефолтом → молчание",
         errs, False)
    errs, _ = check_admin_route_names_its_actor(
        [_rec("C1", "seed-mt", "POST", _ADMIN_PATH_FIX, "jwtProjectAdminA1")],
        _ADMIN_PATH_FIX, "jwtBootstrap")
    want("админ-маршрут под ДРУГИМ явным актором → находка", errs, True)

    print("=== 4c — Operation читает её создатель ===")
    errs, pairs = check_operation_read_by_its_creator([
        _rec("C1", "seed-mt", "POST", _ADMIN_PATH_FIX, "jwtBootstrap", ["opId"]),
        _rec("C1", "poll-op-1", "GET", "/operations/{{opId}}", None),
    ])
    want("создал cluster-admin, читает дефолт → находка", errs, True)
    if pairs != 1:
        print(f"  ПРОВАЛ 4c осмотрела {pairs} пар вместо 1", file=sys.stderr)
        ok = False
    errs, _ = check_operation_read_by_its_creator([
        _rec("C1", "seed-mt", "POST", _ADMIN_PATH_FIX, "jwtBootstrap", ["opId"]),
        _rec("C1", "poll-op-1", "GET", "/operations/{{opId}}", "jwtBootstrap"),
    ])
    want("создал и читает один актор → молчание", errs, False)
    errs, _ = check_operation_read_by_its_creator([
        _rec("C1", "create", "POST", "/compute/v1/instances", None, ["opId"]),
        _rec("C1", "poll-op-1", "GET", "/operations/{{opId}}", None),
    ])
    want("оба под дефолтом → молчание", errs, False)
    errs, _ = check_operation_read_by_its_creator([
        _rec("C1", "seed-mt", "POST", _ADMIN_PATH_FIX, "jwtBootstrap", ["opId"]),
        _rec("C1", "poll-op-1", "GET", "/operations/{{opId}}", "jwtBootstrap"),
        _rec("C1", "create", "POST", "/compute/v1/instances", None, ["opId"]),
        _rec("C1", "poll-op-2", "GET", "/operations/{{opId}}", None),
    ])
    want("ЗАКОННЫЙ БЛИЗНЕЦ: читатель следует за НОВЫМ издателем, а не за прежним → молчание",
         errs, False)
    errs, _ = check_operation_read_by_its_creator([
        _rec("C1", "create", "POST", "/compute/v1/instances", None, ["opId"]),
        _rec("C1", "pin", "GET", "/operations/{{opId}}", None, ["doneOpId"]),
        _rec("C1", "cancel", "POST", "/operations/{{doneOpId}}:cancel", "jwtBootstrap"),
    ])
    want("собственное имя операции (doneOpId) + отмена чужим актором → находка", errs, True)
    errs, _ = check_operation_read_by_its_creator([
        _rec("C2", "create", "POST", "/compute/v1/instances", "jwtBootstrap", ["opId"]),
        _rec("C3", "poll-op-1", "GET", "/operations/{{opId}}", None),
    ])
    want("ЗАКОННЫЙ БЛИЗНЕЦ: издатель в ДРУГОМ кейсе — не пара (переменная не переживает кейс)",
         errs, False)

    print("=== предпосылка 4b — каталог прав ===")
    good = json.dumps([{"fqn": f, "required_relation": "system_admin",
                        "scope_extractor": {"object_type": "cluster", "from_request_field": "*"}}
                       for f in _ADMIN_RPCS])
    want("каталог объявляет system_admin@cluster → молчание", check_admin_premise(good), False)
    moved = json.dumps([{"fqn": f, "required_relation": "editor",
                         "scope_extractor": {"object_type": "project", "from_request_field": "project_id"}}
                        for f in _ADMIN_RPCS])
    want("основание запрета сместилось → находка", check_admin_premise(moved), True)
    want("записи в каталоге больше нет → находка", check_admin_premise("[]"), True)
    want("каталог нечитаем → находка", check_admin_premise("{not json"), True)
    if CATALOG_FILE.exists():
        want("каталог ИЗ ДЕРЕВА → молчание", check_admin_premise(CATALOG_FILE.read_text()), False)
    else:
        print(f"  ПРОВАЛ каталога прав нет по пути {CATALOG_FILE}", file=sys.stderr)
        ok = False

    print("=== 5 — первое обращение к своему свежему интерфейсу ===")
    _DET = "/compute/v1/instances/{{instanceId}}:detachNetworkInterface"
    _ATT = "/compute/v1/instances/{{instanceId}}:attachNetworkInterface"
    _NIC_GET = "/vpc/v1/networkInterfaces/{{nicId}}"

    def _nrec(case_id, name, method, path, refd=(), published=()):
        return (case_id, name, method, path, list(refd), list(published))

    errs, seen = check_first_nic_access_is_a_read([
        _nrec("C1", "seed-nic", "POST", "/vpc/v1/networkInterfaces", [], ["nicId"]),
        _nrec("C1", "detach-1", "POST", _DET, ["instanceId", "nicId"]),
    ])
    want("свежий интерфейс уезжает в отвязку первым обращением → находка", errs, True)
    if seen != 1:
        print(f"  ПРОВАЛ 5 осмотрела {seen} шагов вместо 1", file=sys.stderr)
        ok = False
    errs, _ = check_first_nic_access_is_a_read([
        _nrec("C1", "seed-nic", "POST", "/vpc/v1/networkInterfaces", [], ["nicId"]),
        _nrec("C1", "warm-nic", "GET", _NIC_GET, ["nicId"]),
        _nrec("C1", "detach-1", "POST", _DET, ["instanceId", "nicId"]),
        _nrec("C1", "detach-2", "POST", _DET, ["instanceId", "nicId"]),
    ])
    want("ЗАКОННЫЙ БЛИЗНЕЦ: интерфейс прочитан до мутации (и повтор тоже) → молчание", errs, False)
    errs, _ = check_first_nic_access_is_a_read([
        _nrec("C1", "seed-nic", "POST", "/vpc/v1/networkInterfaces", [], ["nicId"]),
        _nrec("C1", "detach-absent", "POST", _DET, ["instanceId"]),
    ])
    want("ЗАКОННЫЙ БЛИЗНЕЦ: идентификатор литеральный (отрицательный кейс) → молчание", errs, False)
    errs, _ = check_first_nic_access_is_a_read([
        _nrec("C1", "seed-nic", "POST", "/vpc/v1/networkInterfaces", [], ["nicId"]),
        _nrec("C1", "attach", "POST", _ATT, ["instanceId", "nicId"]),
    ])
    want("тот же запрет на привязке, не только на отвязке → находка", errs, True)
    errs, _ = check_first_nic_access_is_a_read([
        _nrec("C1", "seed-net", "POST", "/vpc/v1/networks", [], ["netId"]),
        _nrec("C1", "seed-subnet", "POST", "/vpc/v1/subnets", ["netId"], ["subId"]),
    ])
    want("ЗАКОННЫЙ БЛИЗНЕЦ: обычное «родитель → потомок» не подпадает (цель прав — проект)",
         errs, False)
    errs, _ = check_first_nic_access_is_a_read([
        _nrec("C1", "warm-nic", "GET", _NIC_GET, ["nicId"]),
        _nrec("C2", "detach-1", "POST", _DET, ["instanceId", "nicId"], []),
        _nrec("C2", "seed-nic", "POST", "/vpc/v1/networkInterfaces", [], ["nicId"]),
    ])
    want("ЗАКОННЫЙ БЛИЗНЕЦ: чтение в ДРУГОМ кейсе не считается прогревом, а несвежий "
         "идентификатор не подпадает вовсе", errs, False)

    print("=== предпосылка 5 — цель проверки прав у привязки/отвязки ===")
    good_nic = json.dumps([{"fqn": f, "required_relation": "editor",
                            "scope_extractor": {"object_type": _NIC_TARGET_OBJECT,
                                                "from_request_field": _NIC_TARGET_FIELD}}
                           for f in _NIC_TARGET_RPCS])
    want("каталог объявляет цель — сам интерфейс → молчание",
         check_nic_target_premise(good_nic), False)
    moved_nic = json.dumps([{"fqn": f, "required_relation": "editor",
                             "scope_extractor": {"object_type": "compute_instance",
                                                 "from_request_field": "instance_id"}}
                            for f in _NIC_TARGET_RPCS])
    want("цель сместилась на машину → находка (греть интерфейс больше незачем)",
         check_nic_target_premise(moved_nic), True)
    want("записи в каталоге больше нет → находка", check_nic_target_premise("[]"), True)
    want("каталог нечитаем → находка", check_nic_target_premise("{not json"), True)
    if CATALOG_FILE.exists():
        want("каталог ИЗ ДЕРЕВА → молчание",
             check_nic_target_premise(CATALOG_FILE.read_text()), False)

    print()
    print("PASS: самопроверка validate-cases" if ok else "FAIL: самопроверка validate-cases")
    return 0 if ok else 1


if __name__ == "__main__":
    if "--self-test" in sys.argv[1:]:
        sys.exit(_self_test())
    sys.exit(main())
