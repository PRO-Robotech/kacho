#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Полоса ambient-чтения СПОСОБНА УПАСТЬ — и падает на утёкшей внутренней проекции.

# Предмет

Публичное чтение каталога geo объявлено ИСКЛЮЧЕНИЕМ из project-scope authZ
(`security.md` §«задокументированное исключение: geo public-read»): каталог читает
КАЖДЫЙ аутентифицированный арендатор. Цена исключения — вторая проекция: публичные
`Region`/`Zone` (`proto/kacho/cloud/geo/v1/{region,zone}.proto`) сырого `status` и
`infra` НЕ несут, их несут `InternalRegion`/`InternalZone` на :9091. Утечка
внутренней проекции на этот маршрут раздаёт инфраструктурные поля всем сразу.

До issue #668 у каждого ambient-шага рядом с `assert_status(200)` стояла строка
«a zero-binding principal is not denied (never 403)». Она не могла упасть ОТДЕЛЬНО
(403 ≠ 200 — утверждение о статусе краснеет первым) и не отличала исправную систему
ни от одной поломки: отрицание проходит на 400, 500, 503 — и на 200 с внутренней
проекцией в теле тоже.

Инъекция подаёт ровно этот ответ:

    HTTP 200 + {"zones": [{..., "status": "UP", "infra": {"numericInfraId": 42}}]}

# Инъекция ДВУСТОРОННЯЯ, и третья сторона — самая важная

  1. публичная проекция → утверждения проходят;
  2. внутренняя проекция на публичном маршруте → утверждение ПАДАЕТ, и текст
     падения НАЗЫВАЕТ утёкшее тело;
  3. ПРЕЖНЯЯ форма («never 403») на ТОМ ЖЕ ответе — ЗЕЛЁНАЯ. Это и есть предмет
     issue #668.

# Чем это слабее прогона против стенда — названо прямо

Исполняется НАСТОЯЩИЙ newman по НАСТОЯЩЕЙ сгенерированной коллекции, но ответы даёт
подставной сервер. Доказано: «утверждение различает эти два ответа». НЕ доказано:
«сервис отдаёт публичную проекцию» — это свойство продукта, и его подтверждает
прогон против поднятого стенда (плюс Go-пробы двух проекций в самом сервисе).

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
COLLECTION = ROOT / "collections" / "authz-deny.postman_collection.json"
CASE_PREFIX = "GEO-ZON-GT-AUTHZ-AMBIENT-OK"

PUBLIC_ZONE = {"id": "ru-central1-a", "regionId": "ru-central1", "name": "Zone A",
               "createdAt": "2026-08-18T00:00:00Z", "openForPlacement": True}
# То же самое ПЛЮС два поля внутренней проекции — ровно то, что публичный маршрут
# отдавать не вправе (`InternalZone.status` / `InternalZone.infra`).
LEAKED_ZONE = dict(PUBLIC_ZONE, status="UP", infra={"numericInfraId": 42,
                                                    "hostClasses": ["c1"]})
LEAK_TOKEN = "внутренняя проекция утекла"

# Прежняя форма утверждения — воспроизведена ДОСЛОВНО. Снята из cases/authz-deny.py
# коммитом issue #668.
LEGACY_ASSERTS = [
    "pm.test('[LEGACY] status 200', () => pm.expect(pm.response.code, "
    "JSON.stringify(pm.response.text())).to.eql(200));",
    "pm.test('[LEGACY] a zero-binding principal is not denied (never 403)', () => "
    "pm.expect(pm.response.code).to.not.eql(403));",
    "pm.test('[LEGACY] zones is an array', () => "
    "pm.expect(pm.response.json().zones).to.be.an('array'));",
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
        if self.path.startswith("/geo/v1/zones"):
            zone = LEAKED_ZONE if _Handler.mode == "leak" else PUBLIC_ZONE
            return self._send(200, {"zones": [zone]})
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
        sys.exit(f"selftest: шаг чтения не найден однозначно (найдено {patched}) — "
                 f"третья сторона инъекции не построена, вердикт недействителен")
    dst.write_text(json.dumps(coll))
    return dst


def _run_newman(collection: Path, folder: str, base_url: str, report: Path) -> dict:
    subprocess.run(
        ["newman", "run", str(collection), "--folder", folder,
         "--env-var", f"baseUrl={base_url}",
         "--env-var", f"internalBaseUrl={base_url}",
         "--env-var", "jwtBootstrap=selftest-token",
         "--env-var", "jwtPureNoBindings=selftest-token",
         "--env-var", "jwtNoBindings=selftest-token",
         "--env-var", "existingRegionId=ru-central1",
         "--env-var", "existingZoneId=ru-central1-a",
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

        _Handler.mode = "ok"
        ok = _run_newman(COLLECTION, folder, base_url, tmpd / "ok.json")
        if ok["assertions"]["failed"] != 0:
            problems.append(f"на ПУБЛИЧНОЙ проекции кейс краснеет ({ok['assertions']['failed']} "
                            f"упавших): {ok['failures']}. Такое утверждение краснеет всегда, "
                            f"и его краснота на дефекте ничего не значит")
        if ok["assertions"]["total"] == 0:
            problems.append("на публичной проекции не исполнилось НИ ОДНОГО утверждения — "
                            "«ноль находок» здесь означает «ноль прочитанного»")

        _Handler.mode = "leak"
        bad = _run_newman(COLLECTION, folder, base_url, tmpd / "leak.json")
        if bad["assertions"]["failed"] == 0:
            problems.append("на УТЁКШЕЙ внутренней проекции кейс зелёный — утверждение "
                            "неспособно упасть по своей причине")
        elif not any(LEAK_TOKEN in f for f in bad["failures"]):
            problems.append(f"кейс упал, но текст падения НЕ НАЗЫВАЕТ утечку "
                            f"({LEAK_TOKEN!r}): {bad['failures']}. Диагноз по имени шага "
                            f"вместо текста отказа стоит полного прогона")

        legacy = _legacy_collection(tmpd / "legacy.json", folder)
        legacy_run = _run_newman(legacy, folder, base_url, tmpd / "legacy-report.json")
        if legacy_run["assertions"]["failed"] != 0:
            problems.append(f"прежняя форма на утёкшей проекции ПОКРАСНЕЛА "
                            f"({legacy_run['failures']}) — значит предмет issue #668 "
                            f"воспроизведён неверно и вывод об усилении не обоснован")
        if legacy_run["assertions"]["total"] == 0:
            problems.append("прежняя форма не исполнила НИ ОДНОГО утверждения — "
                            "её зелень означала бы «не проверяли», а не «не увидела дефект»")

    server.shutdown()

    print(f"selftest ambient-полос geo: кейс {folder!r}")
    print(f"  публичная проекция    : утверждений {ok['assertions']['total']}, "
          f"упало {ok['assertions']['failed']}")
    print(f"  внутренняя проекция   : утверждений {bad['assertions']['total']}, "
          f"упало {bad['assertions']['failed']}")
    for f in bad["failures"]:
        print(f"      падение: {f}")
    print(f"  прежняя форма на ней  : утверждений {legacy_run['assertions']['total']}, "
          f"упало {legacy_run['assertions']['failed']}  ← ЗЕЛЕНО НА ДЕФЕКТЕ (предмет #668)")

    if problems:
        print("\nselftest: FAIL")
        for p in problems:
            print(f"  - {p}")
        return 1
    print("\nselftest: OK — утверждение различает публичную и внутреннюю проекцию, "
          "называет утечку в тексте падения, а прежняя форма её не видела")
    return 0


if __name__ == "__main__":
    sys.exit(main())
