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
CEREMONY_SEED_REL = "tests/authz-fixtures/prodseed_ceremony.py"
COLLECTION_GLOBS = (
    "gateway/tests/newman/collections/*.json",
    "services/*/tests/newman/collections/*.json",
)

# Уровень предъявителя. Имена — те же, что кладёт в окружение посев церемонии;
# предпосылка 2 проверяет, что он их всё ещё кладёт.
BEARER_LEVEL = {
    "jwtHumanCeremony": 1,
    "jwtHumanCeremonyStepUp": 2,
}

# REST-сегмент ресурса → gRPC-служба. Держится узким НАМЕРЕННО: шаг, чью ручку
# сопоставить не удалось, гейт не судит и честно исключает из переписи, а не
# засчитывает как пройденный.
RESOURCE_SERVICE = {
    "accounts": "AccountService",
    "projects": "ProjectService",
    "roles": "RoleService",
    "accessBindings": "AccessBindingService",
    "serviceAccounts": "ServiceAccountService",
    "users": "UserService",
    "groups": "GroupService",
}
METHOD_VERB = {"POST": "Create", "PATCH": "Update", "DELETE": "Delete"}

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


def _fqn(method: str, raw_url: str) -> str | None:
    m = re.match(r"^/iam/v1/([A-Za-z]+)(/([^/?]+))?", raw_url)
    if not m:
        return None
    svc = RESOURCE_SERVICE.get(m.group(1))
    if not svc:
        return None
    verb = "Get" if method == "GET" and m.group(3) else METHOD_VERB.get(method)
    if not verb:
        return None
    return f"kacho.cloud.iam.v1.{svc}/{verb}"


def load_catalog(root: str) -> dict:
    with open(os.path.join(root, CATALOG_REL), encoding="utf-8") as fh:
        entries = json.load(fh)
    return {e["fqn"]: str(e.get("required_acr_min", "")) for e in entries}


def scan(root: str, catalog: dict) -> dict:
    findings: list[str] = []
    files_read = leaves_read = mapped = 0
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
                lvl = BEARER_LEVEL.get(_bearer_var(item) or "")
                if lvl is None:
                    continue  # машинный / анонимный — порог его не касается
                req = item.get("request") or {}
                url = req.get("url")
                raw = url.get("raw") if isinstance(url, dict) else str(url or "")
                fqn = _fqn(req.get("method") or "", re.sub(r"^\{\{baseUrl\}\}", "", raw or ""))
                if not fqn or fqn not in catalog:
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
            "leaves_read": leaves_read, "mapped": mapped}


def check_premises(root: str, catalog: dict) -> list[str]:
    bad = []
    if not catalog:
        bad.append(f"каталог {CATALOG_REL} пуст или не читается — утверждать не на чем")
    elif not any(v == "2" for v in catalog.values()):
        bad.append(
            f"в каталоге НЕТ ни одной записи с поднятым порогом — предмет запрета исчез; "
            f"«находок нет» тут означало бы «искать было нечего»"
        )
    seed = os.path.join(root, CEREMONY_SEED_REL)
    try:
        with open(seed, encoding="utf-8") as fh:
            src = fh.read()
    except OSError:
        bad.append(f"посев церемонии {CEREMONY_SEED_REL} не читается — "
                   f"предъявителей, которых требует гейт, может уже не быть")
        return bad
    for name in BEARER_LEVEL:
        if f'"{name}"' not in src:
            bad.append(
                f"посев церемонии больше не кладёт {name!r} — требовать его значит "
                f"требовать несуществующего; уровни надо пересобрать, а не молчать"
            )
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
    print(f"осмотрено: {res['files_read']} коллекц(ий), {res['leaves_read']} шаг(ов); "
          f"сопоставлено с каталогом под человеческим предъявителем: {res['mapped']}")
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

    # Ручки берутся ИЗ КАТАЛОГА, а не вписываются литералом: инъекция обязана
    # остаться настоящей, когда пороги однажды пересмотрят.
    sensitive = next((f for f, v in sorted(catalog.items())
                      if v == "2" and f.startswith("kacho.cloud.iam.v1.AccountService/Delete")), None)
    routine = next((f for f, v in sorted(catalog.items())
                    if v != "2" and f.startswith("kacho.cloud.iam.v1.AccountService/Create")), None)
    if not sensitive or not routine:
        print("  ПРОВАЛ в каталоге не нашлось пары «чувствительная/обычная» на одном "
              "ресурсе — инъекцию не на чем построить")
        return 1

    tmp = tempfile.mkdtemp()
    coll_dir = os.path.join(tmp, "services", "selftest", "tests", "newman", "collections")
    os.makedirs(coll_dir)
    # Предпосылки берутся из НАСТОЯЩЕГО дерева — подделывать их в песочнице значило бы
    # проверять гейт против фикстуры, а не против мира.
    os.makedirs(os.path.join(tmp, os.path.dirname(CATALOG_REL)))
    os.symlink(os.path.join(root, CATALOG_REL), os.path.join(tmp, CATALOG_REL))
    os.makedirs(os.path.join(tmp, os.path.dirname(CEREMONY_SEED_REL)))
    os.symlink(os.path.join(root, CEREMONY_SEED_REL), os.path.join(tmp, CEREMONY_SEED_REL))

    def write(name, items):
        with open(os.path.join(coll_dir, name), "w", encoding="utf-8") as fh:
            json.dump(_collection(items), fh)

    print("=== инъекция: чувствительная ручка обычным предъявителем ===")
    write("defect.json", [_step("delete-undergraded", "DELETE",
                                "/iam/v1/accounts/{{x}}", "jwtHumanCeremony")])
    res = scan(tmp, catalog)
    if len(res["findings"]) != 1 or "delete-undergraded" not in res["findings"][0]:
        print(f"  ПРОВАЛ дефект не найден либо без координаты: {res['findings']}")
        rc = 1
    else:
        print(f"  OK найден с координатой: {res['findings'][0]}")

    print("=== инъекция: обычная ручка ПОДНЯТЫМ предъявителем (перебор) ===")
    write("defect.json", [_step("create-overgraded", "POST",
                                "/iam/v1/accounts", "jwtHumanCeremonyStepUp")])
    res = scan(tmp, catalog)
    if len(res["findings"]) != 1 or "create-overgraded" not in res["findings"][0]:
        print(f"  ПРОВАЛ перебор уровня не найден: {res['findings']}")
        rc = 1
    else:
        print(f"  OK перебор найден: {res['findings'][0]}")

    print("=== законные близнецы той же формы — гейт обязан ПРОМОЛЧАТЬ ===")
    write("defect.json", [
        # тот же маршрут, верный уровень
        _step("delete-correct", "DELETE", "/iam/v1/accounts/{{x}}", "jwtHumanCeremonyStepUp"),
        _step("create-correct", "POST", "/iam/v1/accounts", "jwtHumanCeremony"),
        # МАШИННЫЙ предъявитель на чувствительной ручке — освобождён от порога
        _step("delete-machine", "DELETE", "/iam/v1/accounts/{{x}}", "jwtAccountAdminA"),
        # анонимный шаг — предъявителя нет вовсе
        _step("delete-anon", "DELETE", "/iam/v1/accounts/{{x}}"),
        # ПРОЗА: имя предъявителя только в описании. Обязан НЕ найтись, иначе гейт
        # читает текст и краснеет на объяснении самого себя.
        _step("delete-prose", "DELETE", "/iam/v1/accounts/{{x}}", "jwtAccountAdminA",
              description="раньше здесь стоял jwtHumanCeremony без поднятого уровня"),
    ])
    res = scan(tmp, catalog)
    if res["findings"]:
        print(f"  ПРОВАЛ гейт краснеет на законном: {res['findings']}")
        rc = 1
    else:
        print(f"  OK промолчал на {res['mapped']} сопоставленных законных шагах")

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
