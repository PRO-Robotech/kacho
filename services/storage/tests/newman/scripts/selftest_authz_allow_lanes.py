#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""ALLOW-полоса набора authz СПОСОБНА УПАСТЬ — и падает, НАЗЫВАЯ отказ.

# Предмет

Кейс `AUTHZ-VOL-LIST-OWN-ALLOW-NOLEAK` перечисляет тома проекта, на который у
субъекта есть `viewer`, и объявляет единственный законный исход: `200` + конверт
выдачи (`volumes` / `nextPageToken`). Способность этого утверждения покраснеть
нельзя вывести из чтения — её надо ПОКАЗАТЬ, подав тот самый ответ, который
продукт даёт на отказе НЕ по правам:

    HTTP 400 + {"code": 3, "message": "page_size must be in [0..1000] ..."}

Пара «400 ↔ INVALID_ARGUMENT» не выдумана: отказ производит `validate.PageSize`
(`pkg/validate/validate.go`), который зовётся на этом самом пути чтения
(`services/storage/internal/apps/kacho/api/volume/volume.go`, `List`), а
отображение кода в статус задаёт библиотека края (`runtime.HTTPStatusFromCode`;
край собирается БЕЗ `WithErrorHandler`, см. `api-conventions.md`
§«gRPC-код → HTTP-статус»).

Отказ выбран именно НЕ-403: прежняя форма утверждения ловила ровно 403, поэтому
инъекция отказом в правах ничего бы не различила. Здесь предмет обратный —
показать полосу, на которой прежняя форма была слепа.

# Инъекция ДВУСТОРОННЯЯ, и третья сторона — самая важная

  1. здоровый ответ → утверждения проходят (иначе «краснеет всегда» неотличимо от
     «краснеет на дефекте»);
  2. ответ с отказом валидации → утверждение ПАДАЕТ, и текст падения НАЗЫВАЕТ отказ
     (иначе отчёт называет виновником невиновного);
  3. ПРЕЖНЯЯ форма утверждения («код не 403», «код не 16») на ТОМ ЖЕ ответе —
     ЗЕЛЁНАЯ. Это и есть предмет issue #668: отрицание не отличало исправную
     систему от той поломки, ради которой кейс написан.

# Чем это слабее прогона против стенда — названо прямо

Здесь исполняется НАСТОЯЩИЙ newman и НАСТОЯЩАЯ сгенерированная коллекция, но ответы
даёт подставной сервер. Значит доказано: «утверждение различает эти два ответа». НЕ
доказано: «продукт на этом входе отвечает именно так» — это свойство продукта, и его
подтверждает прогон против поднятого стенда. Подставной сервер намеренно НЕ богаче
контракта: он отдаёт ровно те две формы, что объявлены выше.

Запуск: python3 scripts/selftest_authz_allow_lanes.py   (стенд не нужен, newman нужен)
"""

from __future__ import annotations

import http.server
import json
import shutil
import subprocess
import sys
import tempfile
import threading
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
COLLECTION = ROOT / "collections" / "authz.postman_collection.json"
CASE_PREFIX = "AUTHZ-VOL-LIST-OWN-ALLOW-NOLEAK"
PROJECT_A1 = "prj00000000000000001"

# Здоровый ответ чтения: конверт выдачи, ровно из объявленных полей.
OK_PAGE = {"volumes": [{"id": "vol00000000000000001", "projectId": PROJECT_A1,
                        "name": "selftest"}], "nextPageToken": ""}

# Отказ владельца НЕ по правам — форма, объявленная продуктом (см. шапку).
REFUSAL_BODY = {
    "code": 3,
    "message": "page_size must be in [0..1000] (0 means default)",
    "details": [{"@type": "type.googleapis.com/google.rpc.BadRequest",
                 "fieldViolations": [{"field": "page_size",
                                      "description": "page_size must be in [0..1000]"}]}],
}
REFUSAL_TOKEN = "page_size must be in [0..1000]"

# Прежняя форма утверждения — воспроизведена ДОСЛОВНО, чтобы третья сторона инъекции
# показывала именно её, а не пересказ. Снята из cases/authz.py коммитом issue #668.
LEGACY_ALLOW_ASSERTS = [
    "pm.test('[LEGACY] ALLOW: not 403', () => pm.expect(pm.response.code, "
    "'unexpected 403: ' + pm.response.text()).to.not.equal(403));",
    "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
    "pm.test('[LEGACY] not Unauthenticated (16)', () => pm.expect(j && j.code, "
    "JSON.stringify(j)).to.not.equal(16));",
]


class _Handler(http.server.BaseHTTPRequestHandler):
    mode = "ok"  # 'ok' | 'refusal'

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
        if "/storage/v1/volumes" in self.path:
            if _Handler.mode == "refusal":
                return self._send(400, REFUSAL_BODY)
            return self._send(200, OK_PAGE)
        return self._send(404, {"code": 5, "message": "not found"})


def _case_folder_name() -> str:
    coll = json.loads(COLLECTION.read_text())
    for item in coll["item"]:
        if item["name"].startswith(CASE_PREFIX):
            return item["name"]
    sys.exit(f"selftest: кейса {CASE_PREFIX} нет в коллекции — предпосылка пробы сломана, "
             f"молчание ничего не доказывает")


def _legacy_collection(dst: Path, folder: str) -> Path:
    """Коллекция, у которой шаг перечисления несёт ПРЕЖНЮЮ форму утверждения."""
    coll = json.loads(COLLECTION.read_text())
    patched = 0
    for item in coll["item"]:
        if item["name"] != folder:
            continue
        for step in item.get("item", []):
            events = [e for e in step.get("event", []) if e.get("listen") != "test"]
            events.append({"listen": "test",
                           "script": {"type": "text/javascript",
                                      "exec": list(LEGACY_ALLOW_ASSERTS)}})
            step["event"] = events
            patched += 1
    if patched != 1:
        sys.exit(f"selftest: шаг перечисления в кейсе не найден однозначно (найдено {patched}) — "
                 f"третья сторона инъекции не построена, вердикт недействителен")
    dst.write_text(json.dumps(coll))
    return dst


def _run_newman(collection: Path, folder: str, base_url: str, report: Path) -> dict:
    subprocess.run(
        ["newman", "run", str(collection), "--folder", folder,
         "--env-var", f"baseUrl={base_url}",
         "--env-var", f"projectA1Id={PROJECT_A1}",
         "--env-var", "projectB1Id=prj00000000000000002",
         "--env-var", "jwtProjectAdminA1=selftest-token",
         "--reporters", "json", "--reporter-json-export", str(report),
         "--timeout-request", "5000"],
        capture_output=True, check=False, text=True, timeout=180)
    if not report.exists():
        sys.exit("selftest: newman не оставил отчёта — это «не выполнилось», а не вердикт")
    run = json.loads(report.read_text())["run"]
    failures = [f.get("error", {}).get("message", "") for f in run.get("failures", [])]
    return {"assertions": run["stats"]["assertions"], "failures": failures}


def main() -> int:
    if shutil.which("newman") is None:
        print("selftest: newman не установлен — проба НЕ ИСПОЛНЕНА (это не зелёный вердикт)")
        return 2

    folder = _case_folder_name()
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
    base_url = f"http://127.0.0.1:{server.server_address[1]}"
    threading.Thread(target=server.serve_forever, daemon=True).start()

    problems: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        tmpd = Path(tmp)

        # (1) ЗАКОННЫЙ БЛИЗНЕЦ: здоровый ответ — утверждения обязаны молчать.
        _Handler.mode = "ok"
        ok = _run_newman(COLLECTION, folder, base_url, tmpd / "ok.json")
        if ok["assertions"]["failed"] != 0:
            problems.append(f"на ЗДОРОВОМ ответе кейс краснеет ({ok['assertions']['failed']} "
                            f"упавших): {ok['failures']}. Такое утверждение краснеет всегда, "
                            f"и его краснота на дефекте ничего не значит")
        if ok["assertions"]["total"] == 0:
            problems.append("на здоровом ответе не исполнилось НИ ОДНОГО утверждения — "
                            "«ноль находок» здесь означает «ноль прочитанного»")

        # (2) ИНЪЕКЦИЯ: отказ НЕ по правам — утверждение обязано упасть И НАЗВАТЬ его.
        _Handler.mode = "refusal"
        bad = _run_newman(COLLECTION, folder, base_url, tmpd / "refusal.json")
        if bad["assertions"]["failed"] == 0:
            problems.append("на ответе с ОТКАЗОМ кейс зелёный — утверждение неспособно "
                            "упасть по своей причине")
        elif not any(REFUSAL_TOKEN in f for f in bad["failures"]):
            problems.append(f"кейс упал, но текст падения НЕ НАЗЫВАЕТ отказ "
                            f"({REFUSAL_TOKEN!r}): {bad['failures']}. Диагноз по имени шага "
                            f"вместо текста отказа стоит полного прогона")

        # (3) ПРЕЖНЯЯ ФОРМА на том же ответе — обязана быть ЗЕЛЁНОЙ. Это доказательство
        #     предмета issue #668, а не украшение: без него «мы усилили утверждение»
        #     остаётся заявлением.
        legacy = _legacy_collection(tmpd / "legacy.json", folder)
        legacy_run = _run_newman(legacy, folder, base_url, tmpd / "legacy-report.json")
        if legacy_run["assertions"]["failed"] != 0:
            problems.append(f"прежняя форма утверждения на ответе с отказом ПОКРАСНЕЛА "
                            f"({legacy_run['failures']}) — значит предмет issue #668 "
                            f"воспроизведён неверно и вывод об усилении не обоснован")
        if legacy_run["assertions"]["total"] == 0:
            problems.append("прежняя форма не исполнила НИ ОДНОГО утверждения — "
                            "её зелень означала бы «не проверяли», а не «не увидела дефект»")

    server.shutdown()

    print(f"selftest authz ALLOW-полос: кейс {folder!r}")
    print(f"  здоровый ответ        : утверждений {ok['assertions']['total']}, "
          f"упало {ok['assertions']['failed']}")
    print(f"  ответ с отказом       : утверждений {bad['assertions']['total']}, "
          f"упало {bad['assertions']['failed']}")
    for f in bad["failures"]:
        print(f"      падение: {f}")
    print(f"  прежняя форма на нём  : утверждений {legacy_run['assertions']['total']}, "
          f"упало {legacy_run['assertions']['failed']}  ← ЗЕЛЕНО НА ДЕФЕКТЕ (предмет #668)")

    if problems:
        print("\nselftest: FAIL")
        for p in problems:
            print(f"  - {p}")
        return 1
    print("\nselftest: OK — утверждение различает здоровый ответ и отказ, "
          "называет отказ в тексте падения, а прежняя форма его не видела")
    return 0


if __name__ == "__main__":
    sys.exit(main())
