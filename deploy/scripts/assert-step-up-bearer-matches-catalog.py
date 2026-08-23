#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Шаг под ЧЕЛОВЕЧЕСКИМ предъявителем обязан нести тот уровень входа, которого
требует КАТАЛОГ для его ручки.

─────────────────────────────────────────────────────────────────────────────
ЗАЧЕМ

Часть ручек объявлена чувствительной: `required_acr_min = "2"` (необратимое
удаление, выдача прав). Порог этот действует НЕ на всех: машинный принципал от
него освобождён by design (служба не проходит интерактивный вход). Пока
соответствующие шаги набора шли машинным предъявителем, порог не проверялся ни
разу — и это было не видно, потому что «не проверялся» и «проверен и пройден»
выглядят одинаково: зелёный шаг.

Он стал видим ровно в тот момент, когда шаги перевели на предъявителя,
принадлежащего человеку: 21 шаг из 53 сопоставимых потребовал поднятого уровня,
которого у них не было. Это и есть предмет гейта — не «кто-то ошибся однажды», а
свойство, которое ломается КАЖДЫЙ раз, когда пишут следующий шаг: уровень надо
знать, а знание живёт в каталоге, не в голове.

ПОЧЕМУ ГЕЙТ, А НЕ «ПОЧИНИЛИ И ЛАДНО». Починка распространяется ровно настолько,
насколько хватает её гейта. Двадцать один экземпляр закрыт правкой; двадцать
второй напишут завтра, и он снова будет отвечать 401 в середине фикстуры — то
есть отказом, который читается как «сервис не поднялся» или «материализация не
доехала», а не как «не тот уровень входа».

─────────────────────────────────────────────────────────────────────────────
ЧТО ИМЕННО УТВЕРЖДАЕТСЯ

Для каждого шага коллекции, чей предъявитель — человеческий:

    (уровень предъявителя == поднятый)  ⟺  (каталог требует acr >= 2 для ручки)

Обе стороны эквивалентности значимы, и вторая не менее первой:
  • недобор уровня → шаг получит отказ в аутентификации и НЕ проверит того, ради
    чего написан;
  • перебор уровня (поднятый там, где хватает обычного) → шаг перестаёт
    проверять, что поднятый уровень ручке НЕ требуется. Гейт, ловящий только
    недобор, разрешил бы «поставь везде самый высокий» — а это ровно тот способ
    сделать проверку неспособной упасть.

МАШИННЫЕ предъявители под утверждение НЕ подпадают: они от порога освобождены,
поэтому требовать от них уровня было бы требованием несуществующего.

─────────────────────────────────────────────────────────────────────────────
ПРЕДПОСЫЛКИ, КОТОРЫЕ ГЕЙТ ПРОВЕРЯЕТ САМ

Запрет обоснован фактами о дереве, и факты меняются. Гейт заявляет их вслух и
ПАДАЕТ, если они перестали держаться, — иначе он тихо превратится в ложь:

  1. каталог читается и содержит хотя бы одну чувствительную запись. Иначе
     утверждать нечего, и «находок нет» означало бы «нечего было искать»;
  2. посев церемонии по-прежнему производит ОБА предъявителя. Если поднятого
     больше нет, требовать его — значит требовать несуществующего;
  3. перепись объявляется числами (коллекций прочитано, листьев обойдено, шагов
     сопоставлено). «Ноль находок» обязано быть отличимо от «ноль прочитанного».

ЧИТАЕТСЯ СТРУКТУРА, А НЕ ТЕКСТ. Имя предъявителя встречается в описаниях кейсов
и в комментариях; поиск по слову находил бы объяснение гейта и краснел на нём.
Предъявитель берётся оттуда же, откуда его берёт волна церемонии, — из первой
строки пре-скрипта, которую пишет генератор.

Самопроверка: python3 deploy/scripts/assert-step-up-bearer-matches-catalog.py --self-test
"""
from __future__ import annotations

import glob
import json
import os
import re
import sys
import tempfile

CATALOG_REL = "gateway/internal/middleware/embed/permission_catalog.json"
ROUTE_TABLE_REL = "gateway/internal/middleware/rest_route_table_gen.go"
CEREMONY_SEED_REL = "tests/authz-fixtures/prodseed_ceremony.py"
CEREMONY_DECL_REL = "tests/authz-fixtures/ceremony_credentials.py"
COLLECTION_GLOBS = (
    "gateway/tests/newman/collections/*.json",
    "services/*/tests/newman/collections/*.json",
)

# Уровень предъявителя ЧЕЛОВЕКА ЦЕРЕМОНИИ. Имена — те же, что кладёт в окружение
# посев церемонии; предпосылка 2 проверяет, что он их всё ещё кладёт.
#
# ЭТО НЕ ВЕСЬ НАБОР. Люди, заводящие аккаунт, приходят СЛОТАМИ из объявления
# церемонии (`ADMISSION_SLOTS`), и их имена здесь не выписываются: выписанный
# перечень отстал бы от объявления МОЛЧА, а «отстал» тут значит «шаги слота под
# гейт не подпадают» — то есть гейт перестал бы судить ровно то, ради чего заведён,
# и выглядел бы при этом зелёным. Полный набор строит `bearer_levels()`.
CEREMONY_BEARER_LEVEL = {
    "jwtHumanCeremony": 1,
    "jwtHumanCeremonyStepUp": 2,
}

# REST-сегмент ресурса → gRPC-служба. Держится узким НАМЕРЕННО: шаг, чью ручку
# сопоставить не удалось, гейт не судит и честно исключает из переписи, а не
# засчитывает как пройденный.
# Маршрут шага разрешается ПОРОЖДЁННОЙ таблицей края, а не рукописным словарём.
#
# ЗДЕСЬ СТОЯЛ СЛОВАРЬ ИЗ СЕМИ РЕСУРСОВ И ТРЁХ ГЛАГОЛОВ, а адрес разбирался
# регуляркой на ДВА сегмента. Для под-ресурса это давало неверный ответ молча:
# `DELETE /iam/v1/users/{id}/tokens/{tid}` (отзыв персонального удостоверения,
# порог 2) опознавался как `UserService/Delete` (порог 1) — то есть верный шаг
# объявлялся расхождением; а `POST /iam/v1/users/{id}/tokens` (выпуск, порог 2)
# давал имя, которого в каталоге нет вовсе, и выпадал из суждения БЕЗ СЛЕДА.
# Второе хуже первого: ручка с поднятым порогом — ровно тот предмет, ради
# которого гейт заведён, и он её не видел.
#
# Таблица `generatedRestRoutes` порождается из тех же аннотаций `google.api.http`,
# по которым маршрутизирует сам край, и её свежесть held отдельным гейтом того же
# задания (`make -C gateway rest-route-table-check`). Значит второго места об
# одном предмете больше нет: разойтись с краем разбору нечем.
_ROUTE_RE = re.compile(
    r'\{Method:\s*"([A-Z]+)",\s*Template:\s*"([^"]+)",\s*FQN:\s*"([^"]+)"\}')

_BEARER_RE = re.compile(r"bearer from env '([^']+)'")


def repo_root() -> str:
    return os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))


def _walk(items, path):
    for x in items:
        if "item" in x:
            yield from _walk(x["item"], path + [x.get("name", "")])
        else:
            yield x, path


def _bearer_var(item) -> str | None:
    """Предъявитель шага — из первой строки пре-скрипта, которую пишет генератор.

    Читается ТОЛЬКО начало блока: дальше в том же скрипте встречаются другие имена
    переменных, и предъявителем они не являются.
    """
    for ev in item.get("event", []) or []:
        if ev.get("listen") != "prerequest":
            continue
        for line in ((ev.get("script") or {}).get("exec") or [])[:3]:
            m = _BEARER_RE.search(line)
            if m:
                return m.group(1)
    return None


def load_routes(root: str) -> list[tuple[str, str, str]]:
    """Порождённая таблица края: (HTTP-метод, шаблон пути, gRPC-FQN)."""
    try:
        with open(os.path.join(root, ROUTE_TABLE_REL), encoding="utf-8") as fh:
            return _ROUTE_RE.findall(fh.read())
    except OSError:
        return []


def _segments(path: str) -> list[str]:
    return [seg for seg in path.split("?", 1)[0].split("/") if seg]


def _template_matches(template: str, segs: list[str]) -> bool:
    """Шаблон маршрута против сегментов адреса шага.

    Место поля (`{user_id}`) накрывает РОВНО ОДИН сегмент, поэтому длины совпадают
    и разбор однозначен: под-ресурс и его родитель различаются числом сегментов, а
    не порядком перебора. Суффикс-действие (`{id}:addCidrBlocks`) сверяется
    дословно — иначе шаг действия совпал бы с обычной правкой того же ресурса.
    """
    tsegs = _segments(template)
    if len(tsegs) != len(segs):
        return False
    for want, got in zip(tsegs, segs):
        if want.startswith("{") and want.endswith("}"):
            continue
        if "{" in want and ":" in want:
            if not got.endswith(":" + want.split(":", 1)[1]):
                return False
            continue
        if want != got:
            return False
    return True


def _step_path(template: str) -> str:
    """Адрес шага по шаблону маршрута: места полей — под подстановку.

    Суффикс-действие СОХРАНЯЕТСЯ (`{group_id}:addMember` → `{{x}}:addMember`).
    Первая редакция этой сборки его теряла, и построенный адрес совпадал с
    обычной правкой того же ресурса — то есть проба строила НЕ ту форму, которую
    собиралась проверить. Поймано собственной пробой под-ресурса, а не чтением.
    """
    out = []
    for seg in _segments(template):
        if seg.startswith("{") and seg.endswith("}"):
            out.append("{{x}}")
        elif "{" in seg and ":" in seg:
            out.append("{{x}}:" + seg.split(":", 1)[1])
        else:
            out.append(seg)
    return "/" + "/".join(out)


def _fqn(method: str, raw_url: str, routes) -> str | None:
    segs = _segments(raw_url)
    if not segs:
        return None
    for rmethod, template, fqn in routes:
        if rmethod == method and _template_matches(template, segs):
            return fqn
    return None


def load_declaration(root: str):
    """Объявление церемонии как модуль. Единственный источник имён слотов."""
    import importlib.util  # noqa: PLC0415 — нужен только здесь

    path = os.path.join(root, CEREMONY_DECL_REL)
    spec = importlib.util.spec_from_file_location("kacho_ceremony_decl_stepup", path)
    if spec is None or spec.loader is None:
        return None
    mod = importlib.util.module_from_spec(spec)
    try:
        spec.loader.exec_module(mod)
    except Exception:  # noqa: BLE001 — предпосылка, а не логика: судит check_premises
        return None
    return mod


def bearer_levels(root: str) -> dict[str, int]:
    """Все человеческие предъявители и их уровни — ВЫВЕДЕННЫЕ, а не выписанные.

    Человек церемонии плюс пара на каждый слот заведения. Слот, добавленный в
    объявление, попадает под гейт САМ; выписанный перечень пришлось бы править
    вторым заходом, и забытая правка была бы неотличима от «шагов нет».
    """
    levels = dict(CEREMONY_BEARER_LEVEL)
    decl = load_declaration(root)
    for slot, _slug, _subject in getattr(decl, "ADMISSION_SLOTS", ()) or ():
        lvl1, lvl2 = decl.admission_bearers(slot)
        levels[lvl1] = 1
        levels[lvl2] = 2
    return levels


def load_catalog(root: str) -> dict:
    with open(os.path.join(root, CATALOG_REL), encoding="utf-8") as fh:
        entries = json.load(fh)
    return {e["fqn"]: str(e.get("required_acr_min", "")) for e in entries}


def scan(root: str, catalog: dict) -> dict:
    findings: list[str] = []
    levels = bearer_levels(root)
    routes = load_routes(root)
    files_read = leaves_read = mapped = undeclared = 0
    for g in COLLECTION_GLOBS:
        for f in sorted(glob.glob(os.path.join(root, g))):
            try:
                with open(f, encoding="utf-8") as fh:
                    doc = json.load(fh)
            except (OSError, ValueError):
                continue
            files_read += 1
            rel = os.path.relpath(f, root)
            for item, _path in _walk(doc.get("item", []) or [], []):
                leaves_read += 1
                lvl = levels.get(_bearer_var(item) or "")
                if lvl is None:
                    continue  # машинный / анонимный — порог его не касается
                req = item.get("request") or {}
                url = req.get("url")
                raw = url.get("raw") if isinstance(url, dict) else str(url or "")
                fqn = _fqn(req.get("method") or "",
                           re.sub(r"^\{\{baseUrl\}\}", "", raw or ""), routes)
                if not fqn or fqn not in catalog:
                    continue
                # ПОРОГ, КОТОРОГО КАТАЛОГ НЕ ОБЪЯВЛЯЕТ, ГЕЙТОМ НЕ СУДИТСЯ.
                #
                # Пустое значение — не «уровень 1», а ОТСУТСТВИЕ требования: край
                # на нём проходит открыто (`stepup_gate.go`: «An empty
                # RequiredACRMin means NO step-up requirement — Check fails OPEN»).
                # Требовать от такого шага обычного уровня значит утверждать
                # правило, которого продукт не заводил, — и утверждение это
                # НЕИСПОЛНИМО: опрос операции наследует предъявителя той мутации,
                # которая операцию породила, а операция принципал-областная и
                # чужому отвечает 404. Гейт, требующий понизить уровень поллера,
                # требовал бы сломать шаг.
                #
                # Число несудимых печатается отдельной строкой: «не судили» обязано
                # быть отличимо от «расхождений нет».
                if catalog[fqn] not in ("1", "2"):
                    undeclared += 1
                    continue
                mapped += 1
                needs_two = catalog[fqn] == "2"
                has_two = lvl == 2
                if needs_two and not has_two:
                    findings.append(
                        f"{rel}: шаг {item.get('name')!r} зовёт {fqn} "
                        f"(каталог требует поднятого входа), а идёт обычным предъявителем"
                    )
                elif has_two and not needs_two:
                    findings.append(
                        f"{rel}: шаг {item.get('name')!r} зовёт {fqn} "
                        f"(поднятый вход НЕ требуется), а идёт поднятым — тогда шаг "
                        f"перестаёт проверять, что порога здесь нет"
                    )
    return {"findings": findings, "files_read": files_read,
            "leaves_read": leaves_read, "mapped": mapped,
            "undeclared": undeclared, "routes": len(routes),
            "bearers": len(levels)}


def check_premises(root: str, catalog: dict) -> list[str]:
    bad = []
    if not catalog:
        bad.append(f"каталог {CATALOG_REL} пуст или не читается — утверждать не на чем")
    elif not any(v == "2" for v in catalog.values()):
        bad.append(
            f"в каталоге НЕТ ни одной записи с поднятым порогом — предмет запрета исчез; "
            f"«находок нет» тут означало бы «искать было нечего»"
        )
    # Таблица маршрутов — источник сопоставления. Пустая либо неразобранная
    # означает, что НИ ОДИН шаг не будет сопоставлен, а гейт при этом промолчит:
    # «нечего судить» стало бы неотличимо от «всё сошлось».
    routes = load_routes(root)
    if not routes:
        bad.append(
            f"таблица маршрутов {ROUTE_TABLE_REL} пуста или не разобрана — ни один шаг "
            f"не будет сопоставлен с каталогом, и молчание гейта означало бы «не читал»")
    elif not any(len(_segments(t)) >= 4 for _m, t, _f in routes):
        # Контроль СПОСОБНОСТИ, а не наличия конкретного имени: под-ресурс — ровно
        # та форма адреса, на которой прежний разбор давал неверный ответ молча.
        # Разбор, потерявший её, вернул бы слепое пятно, оставаясь зелёным.
        bad.append(
            f"в {ROUTE_TABLE_REL} не разобрано ни одного маршрута ПОД-РЕСУРСА "
            f"(≥4 сегментов) — разбор потерял ту форму адреса, ради которой он здесь")
    seed = os.path.join(root, CEREMONY_SEED_REL)
    try:
        with open(seed, encoding="utf-8") as fh:
            src = fh.read()
    except OSError:
        bad.append(f"посев церемонии {CEREMONY_SEED_REL} не читается — "
                   f"предъявителей, которых требует гейт, может уже не быть")
        return bad
    for name in CEREMONY_BEARER_LEVEL:
        if f'"{name}"' not in src:
            bad.append(
                f"посев церемонии больше не кладёт {name!r} — требовать его значит "
                f"требовать несуществующего; уровни надо пересобрать, а не молчать"
            )
    # Предъявители СЛОТОВ посев не пишет литералами — он строит их из объявления.
    # Поэтому предпосылка здесь другая и проверяет ровно ту связь, на которой всё
    # держится: посев ЧИТАЕТ объявление, а объявление называет хотя бы один слот.
    decl = load_declaration(root)
    slots = getattr(decl, "ADMISSION_SLOTS", None)
    if not slots:
        bad.append(
            f"в {CEREMONY_DECL_REL} нет `ADMISSION_SLOTS` — люди, заводящие аккаунт, "
            f"под гейт не подпадут, и он замолчит о них, оставаясь зелёным")
    elif "ADMISSION_SLOTS" not in src or "admission_bearers" not in src:
        bad.append(
            f"посев церемонии не читает `ADMISSION_SLOTS`/`admission_bearers` из "
            f"{CEREMONY_DECL_REL} — предъявителей слотов он не выдаёт, а гейт их ждёт")
    return bad


def run(root: str) -> int:
    catalog = load_catalog(root)
    premises = check_premises(root, catalog)
    if premises:
        print("=== ПРЕДПОСЫЛКА ГЕЙТА БОЛЬШЕ НЕ ДЕРЖИТСЯ ===")
        for p in premises:
            print(f"  ПРОВАЛ {p}")
        return 2
    res = scan(root, catalog)
    print(f"осмотрено: {res['files_read']} коллекц(ий), {res['leaves_read']} шаг(ов), "
          f"маршрутов в таблице края {res['routes']}; сопоставлено с каталогом под "
          f"человеческим предъявителем: {res['mapped']}; "
          f"не судимо (порог не объявлен): {res['undeclared']}")
    if res["mapped"] == 0:
        print("  ПРОВАЛ ни один шаг не сопоставлен — это «ничего не прочитано», "
              "а не «расхождений нет»")
        return 2
    if res["findings"]:
        print(f"=== РАСХОЖДЕНИЙ С КАТАЛОГОМ: {len(res['findings'])} ===")
        for f in res["findings"]:
            print(f"  ПРОВАЛ {f}")
        return 1
    print("OK: уровень входа каждого сопоставленного шага совпадает с каталогом.")
    return 0


# ─────────────────────────────────────────────────────────────────────────────
# Самопроверка: гейт обязан покраснеть на внесённом дефекте, назвать координату
# и ПРОМОЛЧАТЬ на законных конструкциях той же формы.
# ─────────────────────────────────────────────────────────────────────────────

def _collection(items) -> dict:
    return {"info": {"name": "self-test", "schema": ""}, "item": items}


def _step(name, method, path, bearer=None, description=""):
    item = {
        "name": name,
        "request": {"method": method, "url": {"raw": "{{baseUrl}}" + path},
                    "description": description},
    }
    if bearer:
        item["event"] = [{"listen": "prerequest",
                          "script": {"exec": [f"// bearer from env '{bearer}'"]}}]
    return item


def self_test() -> int:
    rc = 0
    root = repo_root()
    catalog = load_catalog(root)
    premises = check_premises(root, catalog)
    if premises:
        for p in premises:
            print(f"  ПРОВАЛ предпосылка: {p}")
        return 1

    # Ручки и ИХ АДРЕСА берутся из каталога и порождённой таблицы, а не вписываются
    # литералом: инъекция обязана остаться настоящей, когда пороги пересмотрят, а
    # маршрут переименуют. Прежде адрес стоял литералом (`/iam/v1/accounts`), и
    # инъекция проверяла ровно ту форму адреса, на которой разбор и так работал.
    routes = load_routes(root)
    if not routes:
        print(f"  ПРОВАЛ таблица маршрутов {ROUTE_TABLE_REL} не разобрана — "
              f"инъекцию не на чем построить")
        return 1

    def path_of(fqn: str, min_segments: int = 0) -> str | None:
        """Адрес шага для ручки: шаблон маршрута с местами полей под подстановку."""
        for _m, template, f in routes:
            if f != fqn or len(_segments(template)) < min_segments:
                continue
            return _step_path(template)
        return None

    def pick(level: str, min_segments: int = 0):
        for f, v in sorted(catalog.items()):
            if v != level:
                continue
            path = path_of(f, min_segments)
            if not path:
                continue
            method = next(m for m, t, ff in routes
                          if ff == f and len(_segments(t)) >= min_segments)
            return f, method, path
        return None, None, None

    sensitive, sens_method, sens_path = pick("2")
    routine, rout_method, rout_path = pick("1")
    if not sensitive or not routine:
        print("  ПРОВАЛ в каталоге не нашлось пары ручек с ОБЪЯВЛЕННЫМИ порогами "
              "2 и 1 — инъекцию не на чем построить")
        return 1
    print(f"  инъекция строится на: чувствительная {sensitive} ({sens_method} {sens_path}), "
          f"обычная {routine} ({rout_method} {rout_path})")

    tmp = tempfile.mkdtemp()
    coll_dir = os.path.join(tmp, "services", "selftest", "tests", "newman", "collections")
    os.makedirs(coll_dir)
    # Предпосылки берутся из НАСТОЯЩЕГО дерева — подделывать их в песочнице значило бы
    # проверять гейт против фикстуры, а не против мира.
    os.makedirs(os.path.join(tmp, os.path.dirname(CATALOG_REL)))
    os.symlink(os.path.join(root, CATALOG_REL), os.path.join(tmp, CATALOG_REL))
    os.makedirs(os.path.join(tmp, os.path.dirname(ROUTE_TABLE_REL)), exist_ok=True)
    os.symlink(os.path.join(root, ROUTE_TABLE_REL), os.path.join(tmp, ROUTE_TABLE_REL))
    os.makedirs(os.path.join(tmp, os.path.dirname(CEREMONY_SEED_REL)))
    os.symlink(os.path.join(root, CEREMONY_SEED_REL), os.path.join(tmp, CEREMONY_SEED_REL))
    os.symlink(os.path.join(root, CEREMONY_DECL_REL), os.path.join(tmp, CEREMONY_DECL_REL))

    def write(name, items):
        with open(os.path.join(coll_dir, name), "w", encoding="utf-8") as fh:
            json.dump(_collection(items), fh)

    print("=== предъявители СЛОТОВ попадают в набор уровней, а не выпадают из него ===")
    # Положительный контроль к самому выводу: пропусти гейт слот, и его шаги
    # перестали бы судиться МОЛЧА — он остался бы зелёным, ничего не прочитав.
    levels = bearer_levels(tmp)
    decl_live = load_declaration(tmp)
    slots = list(getattr(decl_live, "ADMISSION_SLOTS", ()) or ())
    missing = [b for slot, _s, _p in slots
               for b in decl_live.admission_bearers(slot) if b not in levels]
    if not slots:
        print("  ПРОВАЛ объявление не отдало ни одного слота — выводить нечего")
        rc = 1
    elif missing:
        print(f"  ПРОВАЛ предъявители слотов вне набора уровней: {missing}")
        rc = 1
    else:
        print(f"  OK слот(ов) {len(slots)}, предъявителей в наборе {len(levels)} "
              f"(человек церемонии + пара на слот)")

    print("=== предпосылка: объявления слотов нет — гейт обязан СКАЗАТЬ, а не молчать ===")
    os.remove(os.path.join(tmp, CEREMONY_DECL_REL))
    bad_decl = check_premises(tmp, catalog)
    if not any("ADMISSION_SLOTS" in b for b in bad_decl):
        print(f"  ПРОВАЛ пропажа объявления слотов прошла молча: {bad_decl}")
        rc = 1
    else:
        print("  OK пропажа объявления слотов названа предпосылкой")
    os.symlink(os.path.join(root, CEREMONY_DECL_REL), os.path.join(tmp, CEREMONY_DECL_REL))

    print("=== инъекция: чувствительная ручка обычным предъявителем ===")
    write("defect.json", [_step("delete-undergraded", sens_method,
                                sens_path, "jwtHumanCeremony")])
    res = scan(tmp, catalog)
    if len(res["findings"]) != 1 or "delete-undergraded" not in res["findings"][0]:
        print(f"  ПРОВАЛ дефект не найден либо без координаты: {res['findings']}")
        rc = 1
    else:
        print(f"  OK найден с координатой: {res['findings'][0]}")

    print("=== инъекция: обычная ручка ПОДНЯТЫМ предъявителем (перебор) ===")
    write("defect.json", [_step("create-overgraded", rout_method,
                                rout_path, "jwtHumanCeremonyStepUp")])
    res = scan(tmp, catalog)
    if len(res["findings"]) != 1 or "create-overgraded" not in res["findings"][0]:
        print(f"  ПРОВАЛ перебор уровня не найден: {res['findings']}")
        rc = 1
    else:
        print(f"  OK перебор найден: {res['findings'][0]}")

    print("=== законные близнецы той же формы — гейт обязан ПРОМОЛЧАТЬ ===")
    write("defect.json", [
        # тот же маршрут, верный уровень
        _step("delete-correct", sens_method, sens_path, "jwtHumanCeremonyStepUp"),
        _step("create-correct", rout_method, rout_path, "jwtHumanCeremony"),
        # МАШИННЫЙ предъявитель на чувствительной ручке — освобождён от порога
        _step("delete-machine", sens_method, sens_path, "jwtAccountAdminA"),
        # анонимный шаг — предъявителя нет вовсе
        _step("delete-anon", sens_method, sens_path),
        # ПРОЗА: имя предъявителя только в описании. Обязан НЕ найтись, иначе гейт
        # читает текст и краснеет на объяснении самого себя.
        _step("delete-prose", sens_method, sens_path, "jwtAccountAdminA",
              description="раньше здесь стоял jwtHumanCeremony без поднятого уровня"),
    ])
    res = scan(tmp, catalog)
    if res["findings"]:
        print(f"  ПРОВАЛ гейт краснеет на законном: {res['findings']}")
        rc = 1
    elif res["mapped"] == 0:
        # Молчание на НУЛЕ сопоставленных — не свидетельство. Прежняя редакция
        # печатала «OK промолчал на 0» и засчитывала это в успех: ровно та форма
        # без содержания, которую гейт заведён ловить.
        print("  ПРОВАЛ ни один законный шаг не сопоставлен — молчание вакуумно")
        rc = 1
    else:
        print(f"  OK промолчал на {res['mapped']} сопоставленных законных шагах")

    # ── ПОД-РЕСУРС: ровно тот класс, на котором прежний разбор молча лгал ─────
    #
    # Пара выводится из дерева, а не выписывается: нужен маршрут ПОД-РЕСУРСА,
    # чей объявленный порог ОТЛИЧАЕТСЯ от порога его родительского маршрута того
    # же метода. Именно на такой паре ошибка разбора наблюдаема: сопоставив
    # под-ресурс с родителем, гейт объявляет верный шаг расхождением, а неверный —
    # законным. Пары нет — сказать об этом, а не пропустить молча: без неё
    # утверждение ниже вакуумно.
    print("=== под-ресурс сопоставляется СО СВОИМ маршрутом, а не с родительским ===")
    pair = None
    # Вложенный под-ресурс — ровно та форма, на которой разбор лгал, поэтому он
    # рассматривается ПЕРВЫМ; суффикс-действие остаётся запасным вариантом, чтобы
    # проба не исчезла, если вложенных пар в дереве однажды не станет.
    ordered = sorted(routes, key=lambda r: ":" in _segments(r[1])[-1])
    for method, template, fqn in ordered:
        tsegs = _segments(template)
        if len(tsegs) < 4 or catalog.get(fqn) not in ("1", "2"):
            continue
        for pmethod, ptemplate, pfqn in routes:
            psegs = _segments(ptemplate)
            if pmethod != method or len(psegs) >= len(tsegs):
                continue
            if psegs != tsegs[:len(psegs)] or catalog.get(pfqn) not in ("1", "2"):
                continue
            if catalog[pfqn] != catalog[fqn]:
                pair = (method, template, fqn, ptemplate, pfqn)
                break
        if pair:
            break
    if not pair:
        print("  ПРОВАЛ в дереве нет под-ресурсного маршрута, чей порог отличается от "
              "родительского — утверждение о разборе под-ресурса не на чем построить")
        rc = 1
    else:
        method, template, fqn, ptemplate, pfqn = pair
        path = _step_path(template)
        right = "jwtHumanCeremonyStepUp" if catalog[fqn] == "2" else "jwtHumanCeremony"
        wrong = "jwtHumanCeremony" if catalog[fqn] == "2" else "jwtHumanCeremonyStepUp"
        print(f"  пара: {method} {template} → {fqn} (порог {catalog[fqn]}) "
              f"над {ptemplate} → {pfqn} (порог {catalog[pfqn]})")

        # (а) верный уровень ПОД-РЕСУРСА — гейт обязан промолчать. Прежний разбор
        #     краснел здесь, потому что мерил порогом родителя.
        write("defect.json", [_step("subresource-correct", method, path, right)])
        res = scan(tmp, catalog)
        if res["findings"]:
            print(f"  ПРОВАЛ верный под-ресурсный шаг объявлен расхождением: {res['findings']}")
            rc = 1
        elif res["mapped"] != 1:
            print(f"  ПРОВАЛ под-ресурсный шаг вообще не сопоставлен (mapped={res['mapped']}) — "
                  f"это слепое пятно, а не согласие")
            rc = 1
        else:
            print("  OK верный под-ресурсный шаг сопоставлен со своим маршрутом и не оговорён")

        # (б) неверный уровень — красное, и координата называет ПОД-РЕСУРС.
        write("defect.json", [_step("subresource-wrong", method, path, wrong)])
        res = scan(tmp, catalog)
        if len(res["findings"]) != 1 or fqn not in res["findings"][0]:
            print(f"  ПРОВАЛ дефект под-ресурса не найден либо назван чужим именем: "
                  f"{res['findings']}")
            rc = 1
        elif pfqn in res["findings"][0]:
            print(f"  ПРОВАЛ находка называет РОДИТЕЛЬСКУЮ ручку: {res['findings'][0]}")
            rc = 1
        else:
            print(f"  OK найден и назван своим именем: {res['findings'][0]}")

    # ── ручка БЕЗ объявленного порога не судится, но и не исчезает из переписи ──
    print("=== порог не объявлен — гейт не судит, но говорит, скольких не судил ===")
    silent = next((f for f, v in sorted(catalog.items())
                   if v not in ("1", "2") and path_of(f)), None)
    if not silent:
        print("  ПРОВАЛ в каталоге нет ручки без объявленного порога — "
              "утверждать не на чем")
        rc = 1
    else:
        smethod = next(m for m, _t, ff in routes if ff == silent)
        write("defect.json", [_step("undeclared-stepup", smethod, path_of(silent),
                                    "jwtHumanCeremonyStepUp")])
        res = scan(tmp, catalog)
        if res["findings"]:
            print(f"  ПРОВАЛ гейт судит порог, которого каталог не объявлял: {res['findings']}")
            rc = 1
        elif res["undeclared"] < 1:
            print("  ПРОВАЛ несудимый шаг не попал в перепись — «не судили» стало "
                  "неотличимо от «расхождений нет»")
            rc = 1
        else:
            print(f"  OK {silent} не судится, и это названо числом "
                  f"(не судимо: {res['undeclared']})")

    print("=== предпосылка: таблица маршрутов пуста — гейт обязан СКАЗАТЬ ===")
    os.remove(os.path.join(tmp, ROUTE_TABLE_REL))
    with open(os.path.join(tmp, ROUTE_TABLE_REL), "w", encoding="utf-8") as fh:
        fh.write("package middleware\n\nvar generatedRestRoutes = []restRoute{}\n")
    bad_routes = check_premises(tmp, catalog)
    if not any(ROUTE_TABLE_REL in b for b in bad_routes):
        print(f"  ПРОВАЛ пропажа таблицы маршрутов прошла молча: {bad_routes}")
        rc = 1
    else:
        print("  OK пустая таблица маршрутов названа предпосылкой")
    os.remove(os.path.join(tmp, ROUTE_TABLE_REL))
    os.symlink(os.path.join(root, ROUTE_TABLE_REL), os.path.join(tmp, ROUTE_TABLE_REL))

    print("=== предпосылка: посев без поднятого предъявителя обязан РОНЯТЬ гейт ===")
    os.remove(os.path.join(tmp, CEREMONY_SEED_REL))
    with open(os.path.join(tmp, CEREMONY_SEED_REL), "w", encoding="utf-8") as fh:
        fh.write('values = {"jwtHumanCeremony": lvl1}\n')
    bad = check_premises(tmp, catalog)
    if not any("jwtHumanCeremonyStepUp" in b for b in bad):
        print(f"  ПРОВАЛ исчезновение предъявителя не замечено: {bad}")
        rc = 1
    else:
        print("  OK предпосылка проверяется и падает")

    print("ИТОГ САМОПРОВЕРКИ: " + ("ПРОВАЛ" if rc else "OK"))
    return rc


def main() -> int:
    if "--self-test" in sys.argv:
        return self_test()
    return run(repo_root())


if __name__ == "__main__":
    sys.exit(main())
