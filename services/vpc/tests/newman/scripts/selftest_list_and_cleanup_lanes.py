#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Утверждения списочного чтения и уборки СПОСОБНЫ УПАСТЬ — и падают по своей причине.

# Предмет (issue #698)

Две полосы шагов несли одно утверждение, принимавшее взаимоисключающие исходы:

    pm.test('handled', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));

  * СПИСОЧНЫЙ ФИЛЬТР (`*-LST-SEC-FILTER-SQLI`, `*-LST-FILTER-SPECIAL-CHARS`).
    Исход здесь УСТАНОВЛЕН, а не неопределён: `pkg/filter`.`Parse` разбирает
    `name="a' OR 1=1--"` штатно — поле в белом списке, значение в кавычках,
    хвоста нет, — значение уезжает ПАРАМЕТРОМ запроса, и страница приходит
    пустой. `400` производится только негодным СИНТАКСИСОМ выражения, которого
    эта нагрузка не содержит. То есть прежняя запись перечисляла исход, которого
    на этом входе не бывает, и одновременно приняла бы настоящую регрессию
    разбора.
  * УБОРКА ФИКСТУРЫ (`cleanup-*`). Здесь производителей действительно два —
    кейс, чей предмет поведение Create, мог ресурс создать, а мог и нет, — но
    «два производителя» не означает «любой ответ сойдёт»: прежняя запись не
    читала НИ ОДНУ из полос и потому приняла бы и залипший отказ в правах (403,
    поданный краем), и смену контракта удаления на код 3.

# Инъекция ДВУСТОРОННЯЯ у КАЖДОГО утверждения

Полосы проверяются раздельно, и у каждой стороны свой производитель ответа.
Мок намеренно НЕ богаче контракта: он отдаёт ровно перечисленные формы.

СПИСОЧНАЯ ПОЛОСА (реальный кейс `NET-LST-SEC-FILTER-SQLI` из собранной коллекции):
  1. законный близнец — 200 и ПУСТАЯ страница: утверждения обязаны МОЛЧАТЬ;
  2. инъекция смысла — 200 и НЕПУСТАЯ страница (значение фильтра не доехало,
     список вернул всё): обязано упасть утверждение о пустоте;
  3. инъекция разбора — 400 на законном выражении: обязано упасть утверждение
     о статусе;
  4. инъекция формы — 200 и ДВА массива верхнего уровня: обязано упасть
     утверждение о составе ответа;
  5. ПРЕЖНЯЯ ФОРМА на ответах (2) и (3) — обязана быть ЗЕЛЁНОЙ. Без неё
     «мы усилили утверждение» остаётся заявлением, а не измерением.

ПОЛОСА УБОРКИ (шаг собирается ТЕМ ЖЕ помощником `assert_cleanup_delete`, что и
всё дерево, — копия его текста здесь не воспроизводится):
  1. законный близнец A — 200 и конверт Operation: молчит;
  2. законный близнец B — 400 и код 9 (`FAILED_PRECONDITION`): молчит;
  3. инъекция — 403 (залипший отказ в правах): падает ветка отказа;
  4. инъекция — 400 и код 3 (валидация вместо состояния): падает утверждение
     о коде, а утверждение о статусе — нет;
  5. инъекция — 200 без `id` (удаление «принято», конверта нет): падает
     утверждение о конверте;
  6. ПРЕЖНЯЯ ФОРМА — измерена на всех трёх дефектах, и результат назван как есть:
     403 она ЛОВИЛА (он вне перечня 200|400), а (4) и (5) — нет. То есть усиление
     не «сделало проверку строже вообще», а закрыло ровно два дефекта, которые
     прежняя запись пропускала by construction; заявлять больше было бы
     украшением.

# Чем это слабее прогона против стенда — названо прямо

Здесь исполняется НАСТОЯЩИЙ newman и НАСТОЯЩИЙ сериализатор коллекции, но
ответы даёт подставной сервер. Доказано: «утверждения различают эти ответы».
НЕ доказано: «продукт отвечает именно так» — это свойство продукта, и его
подтверждают прогон против поднятого стенда и соседние кейсы разбора фильтра
(`*-LST-FILTER-NAME-OK`, `*-LST-FILTER-BAD-SYNTAX`).

Запуск: python3 scripts/selftest_list_and_cleanup_lanes.py   (стенд не нужен, newman нужен)
"""

from __future__ import annotations

import http.server
import importlib.util
import json
import shutil
import subprocess
import sys
import tempfile
import threading
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
COLLECTION = ROOT / "collections" / "network.postman_collection.json"
LIST_CASE_PREFIX = "NET-LST-SEC-FILTER-SQLI"

sys.path.insert(0, str(HERE))
_spec = importlib.util.spec_from_file_location("gen_under_test", HERE / "gen.py")
gen = importlib.util.module_from_spec(_spec)
sys.argv = [sys.argv[0]]
sys.modules["gen_under_test"] = gen
_spec.loader.exec_module(gen)

# Прежняя форма — воспроизведена ДОСЛОВНО (снята из дерева коммитом issue #698),
# чтобы сторона «прежняя форма зелена» показывала именно её, а не пересказ.
LEGACY_LIST = [
    "pm.test('[LEGACY] not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
    "pm.test('[LEGACY] handled', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
]
LEGACY_CLEANUP = [
    "pm.test('[LEGACY] cleanup net (200 or 400 if child leaked)', () => "
    "pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
]

# --- формы ответа, которые отдаёт подставной сервер --------------------------
EMPTY_PAGE = (200, {"networks": [], "nextPageToken": ""})
NONEMPTY_PAGE = (200, {"networks": [{"id": "net00000000000000001", "name": "x"}], "nextPageToken": ""})
PARSE_REFUSED = (400, {"code": 3, "message": "Bad expression at column 1. Unknown field: \"name\"", "details": []})
TWO_ARRAYS = (200, {"networks": [], "warnings": [], "nextPageToken": ""})

DELETE_ACCEPTED = (200, {"id": "vpo00000000000000001", "description": "selftest", "done": True,
                         "metadata": {"networkId": "net00000000000000001"}})
DELETE_REFUSED_STATE = (400, {"code": 9, "message": "Network net… is not empty (subnets: 1)", "details": []})
DELETE_DENIED = (403, {"code": 7, "message": "permission denied", "details": []})
DELETE_REFUSED_VALIDATION = (400, {"code": 3, "message": "invalid network id", "details": []})
DELETE_ACCEPTED_NO_ENVELOPE = (200, {"done": True})

MODES: dict[str, tuple[int, dict]] = {}


class _Handler(http.server.BaseHTTPRequestHandler):
    mode = "empty"

    def log_message(self, *_args):  # тишина: вывод пробы — её собственный
        return

    def _send(self, code: int, payload: dict) -> None:
        raw = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):  # noqa: N802 — имя задано базовым классом
        if self.path.startswith("/geo/v1/zones"):
            return self._send(200, {"zones": [{"id": "zoneselftest0000001", "status": "UP"}]})
        if self.path.startswith("/operations/"):
            return self._send(*DELETE_ACCEPTED)
        if self.path.startswith("/vpc/v1/networks"):
            return self._send(*MODES[_Handler.mode])
        return self._send(404, {"code": 5, "message": "not found"})

    def do_DELETE(self):  # noqa: N802
        return self._send(*MODES[_Handler.mode])


def _list_case_folder() -> str:
    coll = json.loads(COLLECTION.read_text())
    for item in coll["item"]:
        if item["name"].startswith(LIST_CASE_PREFIX):
            return item["name"]
    sys.exit(f"selftest: кейса {LIST_CASE_PREFIX} нет в коллекции — предпосылка пробы "
             f"сломана, молчание ничего не доказывает")


def _legacy_list_collection(dst: Path, folder: str) -> Path:
    """Коллекция, у которой шаг фильтра несёт ПРЕЖНЮЮ форму утверждения."""
    coll = json.loads(COLLECTION.read_text())
    patched = 0
    for item in coll["item"]:
        if item["name"] != folder:
            continue
        for step in item.get("item", []):
            step["event"] = [{"listen": "test",
                              "script": {"type": "text/javascript", "exec": list(LEGACY_LIST)}}]
            patched += 1
    if patched != 1:
        sys.exit(f"selftest: шаг фильтра не найден однозначно (найдено {patched}) — "
                 f"сторона «прежняя форма» не построена, вердикт недействителен")
    dst.write_text(json.dumps(coll))
    return dst


def _cleanup_collection(dst: Path, legacy: bool) -> tuple[Path, str]:
    """Одношаговая коллекция вокруг ШАГА УБОРКИ, собранного настоящим помощником.

    Скрипт берётся из `gen.assert_cleanup_delete`, а сериализация — из
    `gen.build_collection`: проверяется то, что уезжает в дерево, а не копия.
    """
    script = (list(LEGACY_CLEANUP) if legacy
              else gen.assert_cleanup_delete("сеть", "в сети остались подсети"))
    case = gen.Case(
        id="SELFTEST-CLEANUP-LANE",
        title="selftest: полоса уборки",
        classes=["NEG"], priority="P3",
        steps=[gen.Step(name="cleanup-net", method="DELETE",
                        path="/vpc/v1/networks/net00000000000000001",
                        test_script=script)],
    )
    # Сериализация берётся у ДЕСКРИПТОРА набора, а не у общей функции: после
    # сведения хребта решения набора (`emit`) — её первый аргумент, и звать
    # её напрямую значит собирать коллекцию НЕ теми решениями, что уезжают
    # в дерево, — то есть проверять копию вместо предмета.
    coll = gen._RUN.collection("selftest", [case])
    # `build_collection` приклеивает служебные папки (`_SETUP-*`) ПЕРЕД кейсами,
    # поэтому папка ищется по идентификатору, а не по индексу: индекс — догадка
    # о чужой раскладке, и первая же вставка служебного шага её ломает.
    names = [it["name"] for it in coll["item"] if it["name"].startswith(case.id)]
    if len(names) != 1:
        sys.exit(f"selftest: папка кейса {case.id} не найдена однозначно (найдено {len(names)}) — "
                 f"полоса уборки не построена, вердикт недействителен")
    dst.write_text(json.dumps(coll, ensure_ascii=False))
    return dst, names[0]


def _run_newman(collection: Path, folder: str, base_url: str, report: Path) -> dict:
    subprocess.run(
        ["newman", "run", str(collection), "--folder", folder,
         "--env-var", f"baseUrl={base_url}",
         "--env-var", "existingProjectId=prj00000000000000001",
         "--env-var", "existingProjectCrossId=prj00000000000000002",
         "--env-var", "existingZoneId=zoneselftest0000001",
         "--env-var", "jwtProjectAdminA1=selftest-token",
         "--reporters", "json", "--reporter-json-export", str(report),
         "--timeout-request", "5000"],
        capture_output=True, check=False, text=True, timeout=180)
    if not report.exists():
        sys.exit("selftest: newman не оставил отчёта — это «не выполнилось», а не вердикт")
    run = json.loads(report.read_text())["run"]
    failures = [f"{f.get('error', {}).get('test', '')}: {f.get('error', {}).get('message', '')}"
                for f in run.get("failures", [])]
    return {"assertions": run["stats"]["assertions"], "failures": failures}


def main() -> int:
    if shutil.which("newman") is None:
        print("selftest: newman не установлен — проба НЕ ИСПОЛНЕНА (это не зелёный вердикт)")
        return 2

    list_folder = _list_case_folder()
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
    base_url = f"http://127.0.0.1:{server.server_address[1]}"
    threading.Thread(target=server.serve_forever, daemon=True).start()

    problems: list[str] = []
    runs: dict[str, dict] = {}
    with tempfile.TemporaryDirectory() as tmp:
        tmpd = Path(tmp)

        def go(key: str, response: tuple[int, dict], collection: Path, folder: str) -> dict:
            MODES[key] = response
            _Handler.mode = key
            runs[key] = _run_newman(collection, folder, base_url, tmpd / f"{key}.json")
            return runs[key]

        def must_be_silent(key: str, what: str) -> None:
            if runs[key]["assertions"]["failed"] != 0:
                problems.append(f"на ЗАКОННОМ близнеце ({what}) шаг краснеет "
                                f"({runs[key]['failures']}) — утверждение краснеет всегда, и его "
                                f"краснота на дефекте ничего не значит")
            if runs[key]["assertions"]["total"] == 0:
                problems.append(f"на законном близнеце ({what}) не исполнилось НИ ОДНОГО "
                                f"утверждения — «ноль находок» здесь означает «ноль прочитанного»")

        def must_fail_naming(key: str, needle: str, what: str) -> None:
            if runs[key]["assertions"]["failed"] == 0:
                problems.append(f"на ИНЪЕКЦИИ ({what}) шаг зелёный — утверждение неспособно "
                                f"упасть по своей причине, а это и есть предмет issue #698")
            elif not any(needle in f for f in runs[key]["failures"]):
                problems.append(f"шаг упал, но текст падения не называет «{needle}» "
                                f"({runs[key]['failures']}) — диагноз по имени шага вместо текста "
                                f"отказа стоит полного прогона")

        # ---------------- СПИСОЧНАЯ ПОЛОСА ----------------
        go("empty", EMPTY_PAGE, COLLECTION, list_folder)
        must_be_silent("empty", "200 и пустая страница")

        go("nonempty", NONEMPTY_PAGE, COLLECTION, list_folder)
        must_fail_naming("nonempty", "страница пуста", "фильтр не сузил выборку")

        go("parse_refused", PARSE_REFUSED, COLLECTION, list_folder)
        must_fail_naming("parse_refused", "status 200", "законное выражение отвергнуто")

        go("two_arrays", TWO_ARRAYS, COLLECTION, list_folder)
        must_fail_naming("two_arrays", "страница пуста", "форма списочного ответа сменилась")

        legacy_list = _legacy_list_collection(tmpd / "legacy-list.json", list_folder)
        for key, what in (("nonempty", "фильтр не сузил выборку"),
                          ("parse_refused", "законное выражение отвергнуто")):
            _Handler.mode = key
            r = _run_newman(legacy_list, list_folder, base_url, tmpd / f"legacy-{key}.json")
            runs[f"legacy-{key}"] = r
            if r["assertions"]["total"] == 0:
                problems.append(f"прежняя форма ({what}) не исполнила ни одного утверждения — "
                                f"сравнивать не с чем")
            elif r["assertions"]["failed"] != 0:
                problems.append(f"прежняя форма ПОКРАСНЕЛА на ответе «{what}» ({r['failures']}) — "
                                f"значит предмет issue #698 воспроизведён неверно и вывод об "
                                f"усилении не обоснован")

        # ---------------- ПОЛОСА УБОРКИ ----------------
        cu_coll, cu_folder = _cleanup_collection(tmpd / "cleanup.json", legacy=False)
        go("del_ok", DELETE_ACCEPTED, cu_coll, cu_folder)
        must_be_silent("del_ok", "200 и конверт Operation")

        go("del_state", DELETE_REFUSED_STATE, cu_coll, cu_folder)
        must_be_silent("del_state", "400 и код 9 — отказ по состоянию")

        go("del_denied", DELETE_DENIED, cu_coll, cu_folder)
        must_fail_naming("del_denied", "отказ по СОСТОЯНИЮ", "залипший отказ в правах (403)")

        go("del_validation", DELETE_REFUSED_VALIDATION, cu_coll, cu_folder)
        must_fail_naming("del_validation", "отказ по СОСТОЯНИЮ", "400 с кодом 3 вместо 9")

        go("del_no_envelope", DELETE_ACCEPTED_NO_ENVELOPE, cu_coll, cu_folder)
        must_fail_naming("del_no_envelope", "принято", "200 без конверта Operation")

        legacy_cu, legacy_cu_folder = _cleanup_collection(tmpd / "legacy-cleanup.json", legacy=True)
        # Прежняя форма измеряется на ВСЕХ трёх дефектах, а не на удобных двух.
        # Ожидание по каждому названо отдельно: 403 она ловила (он вне перечня
        # 200|400), а 400/3 и 200-без-конверта пропускала by construction.
        for key, what, must_be_blind in (("del_denied", "залипший отказ в правах", False),
                                         ("del_validation", "400 с кодом 3", True),
                                         ("del_no_envelope", "200 без конверта", True)):
            _Handler.mode = key
            r = _run_newman(legacy_cu, legacy_cu_folder, base_url, tmpd / f"legacy-{key}-cu.json")
            runs[f"legacy-cu-{key}"] = r
            if r["assertions"]["total"] == 0:
                problems.append(f"прежняя форма уборки ({what}) не исполнила ни одного "
                                f"утверждения — сравнивать не с чем")
            elif must_be_blind and r["assertions"]["failed"] != 0:
                problems.append(f"прежняя форма уборки ПОКРАСНЕЛА на «{what}» ({r['failures']}) — "
                                f"значит этот дефект она видела, и вывод об усилении на нём "
                                f"не обоснован")
            elif not must_be_blind and r["assertions"]["failed"] == 0:
                problems.append(f"прежняя форма уборки ЗЕЛЕНА на «{what}», хотя код вне перечня "
                                f"200|400 — предмет воспроизведён неверно")

    server.shutdown()

    print(f"selftest полос списка и уборки: списочный кейс {list_folder!r}")
    order = [
        ("empty", "список: 200 и пустая страница (близнец)"),
        ("nonempty", "список: 200 и НЕпустая страница    "),
        ("parse_refused", "список: 400 на законном выражении  "),
        ("two_arrays", "список: два массива в ответе       "),
        ("legacy-nonempty", "список: прежняя форма на (2)       "),
        ("legacy-parse_refused", "список: прежняя форма на (3)       "),
        ("del_ok", "уборка: 200 + Operation (близнец)  "),
        ("del_state", "уборка: 400 + код 9 (близнец)      "),
        ("del_denied", "уборка: 403                        "),
        ("del_validation", "уборка: 400 + код 3                "),
        ("del_no_envelope", "уборка: 200 без конверта           "),
        ("legacy-cu-del_denied", "уборка: прежняя форма на 403       "),
        ("legacy-cu-del_validation", "уборка: прежняя форма на 400/3     "),
        ("legacy-cu-del_no_envelope", "уборка: прежняя форма на 200 без id"),
    ]
    for key, label in order:
        st = runs[key]["assertions"]
        print(f"  {label}: утверждений {st['total']}, упало {st['failed']}")
        for f in runs[key]["failures"]:
            print(f"      падение: {f}")

    if problems:
        print("\nselftest FAIL:")
        for p in problems:
            print(f"  - {p}")
        return 1
    print("\nselftest: OK — оба утверждения различают законный ответ и дефект и называют "
          "предмет в тексте падения. Прежняя форма измерена на ПЯТИ инъекциях: слепа к "
          "четырём (непустая страница, 400 на законном выражении, 400 с кодом 3, 200 без "
          "конверта) и видела одну (403 — он вне перечня 200|400). Больше этого замер не "
          "даёт, и заявлять больше нельзя.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
