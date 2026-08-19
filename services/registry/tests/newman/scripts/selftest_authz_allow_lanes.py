#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Полоса скрытия существования СПОСОБНА УПАСТЬ — и падает на ОТКРЫВШЕМСЯ гейте.

# Предмет

Кейс `REG-LSTTAGS-AZ-NOTFOUND` перечисляет теги чужого репозитория субъектом без
единого гранта и объявляет единственный законный исход: `404` + `code 5` + текст
владельца `repository not found`. Установлено по коду, а не угадано: запись
каталога прав объявляет метод `scope_filtered`, поэтому решение принимает сервис;
`RegistryHandler.ListTags` (`services/registry/internal/handler/public.go`) судит
формат страницы, затем зовёт `checkRepo(v_list)`, а тот на отказе отвечает
`errRepoHideExistence()` — `NotFound "repository not found"`
(`services/registry/internal/handler/listauthz.go`).

Инъекция подаёт ровно тот ответ, который прежняя форма утверждения объявляла
ЗАКОННЫМ и который на деле есть НАСТОЯЩАЯ УТЕЧКА:

    HTTP 200 + {"tags": [], "nextPageToken": ""}

То есть гейт `v_list` пропустил субъекта, у которого нет ни одного отношения. Ответ
пуст, поэтому «утечки содержимого» в нём не видно — и именно поэтому прежняя
редакция его принимала («200 допустим только как пустая страница»). Пустота здесь
ничего не доказывает: пустой ответ отдал бы и открытый гейт на пустом репозитории.

# Инъекция ДВУСТОРОННЯЯ, и третья сторона — самая важная

  1. здоровый ответ (404 + текст владельца) → утверждения проходят;
  2. открывшийся гейт (200 + пустая страница) → утверждение ПАДАЕТ, и текст падения
     НАЗЫВАЕТ полученный ответ;
  3. ПРЕЖНЯЯ форма (`oneOf([200, 401, 404])` + «never 403» + условная проверка
     пустоты) на ТОМ ЖЕ ответе — ЗЕЛЁНАЯ. Это и есть предмет issue #668.

# Чем это слабее прогона против стенда — названо прямо

Исполняется НАСТОЯЩИЙ newman по НАСТОЯЩЕЙ сгенерированной коллекции, но ответы даёт
подставной сервер. Доказано: «утверждение различает эти два ответа». НЕ доказано:
«продукт отвечает именно так» — это свойство продукта, и его подтверждает прогон
против поднятого стенда.

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
COLLECTION = ROOT / "collections" / "registry.postman_collection.json"
CASE_PREFIX = "REG-LSTTAGS-AZ-NOTFOUND"

# Здоровый ответ: скрытие существования текстом владельца, байт в байт.
OK_HIDDEN = {"code": 5, "message": "repository not found", "details": []}
# Открывшийся гейт: пустая страница вместо отказа. Прежняя форма звала это законным.
LEAK_PAGE = {"tags": [], "nextPageToken": ""}
LEAK_TOKEN = '"tags"'

# Прежняя форма утверждения — воспроизведена ДОСЛОВНО. Снята из cases/registry.py
# коммитом issue #668.
LEGACY_ASSERTS = [
    "pm.test('[LEGACY] unauthorized -> 401/404/200-empty (existence-hidden), never 403', "
    "() => pm.expect(pm.response.code).to.be.oneOf([200, 401, 404]));",
    "pm.test('[LEGACY] never 403 (deny -> 404 no-leak)', () => "
    "pm.expect(pm.response.code).to.not.eql(403));",
    "if (pm.response.code === 200) {",
    "  const _t = pm.response.json().tags || [];",
    "  pm.test('[LEGACY] tags is array', () => pm.expect(_t).to.be.an('array'));",
    "  pm.test('[LEGACY] stranger receives NO tags of a foreign repository', () => "
    "pm.expect(_t.length).to.eql(0));",
    "  pm.test('[LEGACY] no tag payload leaked (digest/size)', () => "
    "pm.expect(pm.response.text()).to.not.include('digest'));",
    "}",
    "if (pm.response.code !== 401) { pm.test('[LEGACY] authenticated deny -> no resource-existence leak', "
    "() => pm.expect(JSON.stringify(pm.response.json())).to.not.include('deny_reasons')); }",
]


class _Handler(http.server.BaseHTTPRequestHandler):
    mode = "ok"  # 'ok' | 'leak'

    def log_message(self, *_args):
        return

    def _send(self, code: int, payload: dict) -> None:
        raw = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):  # noqa: N802
        if self.path.endswith("/tags"):
            if _Handler.mode == "leak":
                return self._send(200, LEAK_PAGE)
            return self._send(404, OK_HIDDEN)
        return self._send(404, {"code": 5, "message": "not found"})


def _case_folder_name() -> str:
    coll = json.loads(COLLECTION.read_text())
    for item in coll["item"]:
        if item["name"].startswith(CASE_PREFIX):
            return item["name"]
    sys.exit(f"selftest: кейса {CASE_PREFIX} нет в коллекции — предпосылка пробы сломана, "
             f"молчание ничего не доказывает")


def _legacy_collection(dst: Path, folder: str) -> Path:
    coll = json.loads(COLLECTION.read_text())
    patched = 0
    for item in coll["item"]:
        if item["name"] != folder:
            continue
        for step in item.get("item", []):
            events = [e for e in step.get("event", []) if e.get("listen") != "test"]
            events.append({"listen": "test",
                           "script": {"type": "text/javascript",
                                      "exec": list(LEGACY_ASSERTS)}})
            step["event"] = events
            patched += 1
    if patched != 1:
        sys.exit(f"selftest: шаг перечисления не найден однозначно (найдено {patched}) — "
                 f"третья сторона инъекции не построена, вердикт недействителен")
    dst.write_text(json.dumps(coll))
    return dst


def _run_newman(collection: Path, folder: str, base_url: str, report: Path) -> dict:
    subprocess.run(
        ["newman", "run", str(collection), "--folder", folder,
         "--env-var", f"baseUrl={base_url}",
         "--env-var", "regId=reg00000000000000001",
         "--env-var", "runId=selftest",
         "--env-var", "jwtStranger=selftest-token",
         "--env-var", "jwtBootstrap=selftest-token",
         # Страж посева коллекции требует эти имена ДО первого запроса: пустой
         # идентификатор он справедливо считает несостоявшимся посевом, а не пропуском.
         "--env-var", "existingProjectId=prj00000000000000001",
         "--env-var", "existingRegionId=ru-central1",
         "--env-var", "jwtProjectEditorA=selftest-token",
         "--reporters", "json", "--reporter-json-export", str(report),
         "--timeout-request", "5000"],
        capture_output=True, check=False, text=True, timeout=300)
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

        _Handler.mode = "ok"
        ok = _run_newman(COLLECTION, folder, base_url, tmpd / "ok.json")
        if ok["assertions"]["failed"] != 0:
            problems.append(f"на ЗДОРОВОМ ответе кейс краснеет ({ok['assertions']['failed']} "
                            f"упавших): {ok['failures']}. Такое утверждение краснеет всегда, "
                            f"и его краснота на дефекте ничего не значит")
        if ok["assertions"]["total"] == 0:
            problems.append("на здоровом ответе не исполнилось НИ ОДНОГО утверждения — "
                            "«ноль находок» здесь означает «ноль прочитанного»")

        _Handler.mode = "leak"
        bad = _run_newman(COLLECTION, folder, base_url, tmpd / "leak.json")
        if bad["assertions"]["failed"] == 0:
            problems.append("на ОТКРЫВШЕМСЯ ГЕЙТЕ кейс зелёный — утверждение неспособно "
                            "упасть по своей причине")
        elif not any(LEAK_TOKEN in f for f in bad["failures"]):
            problems.append(f"кейс упал, но текст падения НЕ НАЗЫВАЕТ полученный ответ "
                            f"({LEAK_TOKEN!r}): {bad['failures']}. Диагноз по имени шага "
                            f"вместо текста отказа стоит полного прогона")

        legacy = _legacy_collection(tmpd / "legacy.json", folder)
        legacy_run = _run_newman(legacy, folder, base_url, tmpd / "legacy-report.json")
        if legacy_run["assertions"]["failed"] != 0:
            problems.append(f"прежняя форма на открывшемся гейте ПОКРАСНЕЛА "
                            f"({legacy_run['failures']}) — значит предмет issue #668 "
                            f"воспроизведён неверно и вывод об усилении не обоснован")
        if legacy_run["assertions"]["total"] == 0:
            problems.append("прежняя форма не исполнила НИ ОДНОГО утверждения — "
                            "её зелень означала бы «не проверяли», а не «не увидела дефект»")

    server.shutdown()

    print(f"selftest полосы скрытия существования: кейс {folder!r}")
    print(f"  здоровый ответ (404)  : утверждений {ok['assertions']['total']}, "
          f"упало {ok['assertions']['failed']}")
    print(f"  открывшийся гейт (200): утверждений {bad['assertions']['total']}, "
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
    print("\nselftest: OK — утверждение различает скрытие существования и открывшийся "
          "гейт, называет полученный ответ, а прежняя форма его не видела")
    return 0


if __name__ == "__main__":
    sys.exit(main())
