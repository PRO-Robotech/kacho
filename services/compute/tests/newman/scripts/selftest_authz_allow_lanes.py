#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""ALLOW-полоса матрицы authz-deny СПОСОБНА УПАСТЬ — и падает, НАЗЫВАЯ отказ.

# Предмет

Кейс `AUTHZ-INSTANCE-CR-OWN-PA1` шлёт Create машины субъектом, у которого право
ЕСТЬ, и объявляет единственный законный исход: `400` + `code 9`
(`FAILED_PRECONDITION`) + контрактный текст стража недостижимости. Установлено по
коду владельца: тело матрицы — VM без внешнего адреса и без снятия стража, а
`ValidateCreateInstanceReq` (F5,
`services/compute/internal/apps/kacho/api/instance/instance.go`) отвергает такой
вход СИНХРОННО, до создания Operation. Отображение кода в статус задаёт библиотека
края (`runtime.HTTPStatusFromCode`), поэтому `FAILED_PRECONDITION` — это 400.

Инъекция подаёт отказ, который край даёт на РЕГРЕССИИ ПРАВ — то самое, ради чего
матрица и написана:

    HTTP 403 + {"code": 7, "message": "permission denied"}

и, второй стороной, УСПЕХ мутации:

    HTTP 200 + конверт Operation

Второй важен не меньше: прежняя форма («не 403 и не 401») зеленела на успехе так
же охотно, как на отказе валидации, — то есть кейс не отличал «страж сработал» от
«машина создана», хотя тело кейса машину создавать не должно и не может.

# Инъекция ДВУСТОРОННЯЯ, и третья сторона — самая важная

  1. здоровый ответ (400 + code 9 + текст стража) → утверждения проходят;
  2. отказ в правах И, отдельно, успех мутации → утверждение ПАДАЕТ, называя
     полученное;
  3. ПРЕЖНЯЯ форма на УСПЕХЕ — ЗЕЛЁНАЯ. Она ловила только 403/401, поэтому
     созданная машина проходила мимо неё молча. Это и есть предмет issue #668.

# Чем это слабее прогона против стенда — названо прямо

Исполняется НАСТОЯЩИЙ newman по НАСТОЯЩЕЙ сгенерированной коллекции, но ответы даёт
подставной сервер. Доказано: «утверждение различает эти ответы». НЕ доказано:
«продукт на этом входе отвечает именно так» — это свойство продукта, и его
подтверждает прогон против поднятого стенда (плюс уже зелёный кейс того же набора
`INST-RD-CR-VAL-UNREACHABLE-GUARD` на идентичном теле).

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
CASE_PREFIX = "AUTHZ-INSTANCE-CR-OWN-PA1"

GUARD_MESSAGE = ("VM will be RUNNING but unreachable (no external address); "
                 "set acknowledgeUnreachable:true to proceed")
OK_GUARD = {"code": 9, "message": GUARD_MESSAGE, "details": []}
DENIED = {"code": 7, "message": "permission denied", "details": []}
CREATED_OP = {"id": "cop00000000000000001", "description": "selftest", "done": False,
              "metadata": {"instanceId": "ins-00000000000000001"}}

# Что обязано прозвучать в тексте падения на каждой из двух инъекций.
DENIED_TOKEN = "permission denied"
CREATED_TOKEN = "cop00000000000000001"

# Прежняя форма утверждения — воспроизведена ДОСЛОВНО. Снята из cases/authz-deny.py
# коммитом issue #668.
LEGACY_ALLOW_ASSERTS = [
    "pm.test('[LEGACY] ALLOW: not 403 PermissionDenied', () => pm.expect(pm.response.code, "
    "'unexpected 403: ' + pm.response.text()).to.not.equal(403));",
    "pm.test('[LEGACY] ALLOW: not 401 Unauthenticated', () => pm.expect(pm.response.code, "
    "'unexpected 401: ' + pm.response.text()).to.not.equal(401));",
    "let _j; try { _j = pm.response.json(); } catch(e) { _j = null; }",
    "pm.test('[LEGACY] ALLOW: not grpc PERMISSION_DENIED (7)', () => pm.expect(_j && _j.code, "
    "JSON.stringify(_j)).to.not.equal(7));",
    "pm.test('[LEGACY] ALLOW: not Unauthenticated (16)', () => pm.expect(_j && _j.code, "
    "JSON.stringify(_j)).to.not.equal(16));",
]

_MODES = {
    "ok": (400, OK_GUARD),
    "denied": (403, DENIED),
    "created": (200, CREATED_OP),
}


class _Handler(http.server.BaseHTTPRequestHandler):
    mode = "ok"  # 'ok' | 'denied' | 'created'

    def log_message(self, *_args):
        return

    def _send(self, code: int, payload: dict) -> None:
        raw = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_POST(self):  # noqa: N802
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)
        if self.path.endswith("/compute/v1/instances"):
            return self._send(*_MODES[_Handler.mode])
        return self._send(404, {"code": 5, "message": "not found"})

    def do_GET(self):  # noqa: N802
        return self._send(200, dict(CREATED_OP, done=True))


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
                                      "exec": list(LEGACY_ALLOW_ASSERTS)}})
            step["event"] = events
            patched += 1
    if patched != 1:
        sys.exit(f"selftest: шаг Create не найден однозначно (найдено {patched}) — "
                 f"третья сторона инъекции не построена, вердикт недействителен")
    dst.write_text(json.dumps(coll))
    return dst


def _run_newman(collection: Path, folder: str, base_url: str, report: Path) -> dict:
    subprocess.run(
        ["newman", "run", str(collection), "--folder", folder,
         "--env-var", f"baseUrl={base_url}",
         "--env-var", f"internalBaseUrl={base_url}",
         "--env-var", "projectA1Id=prj00000000000000001",
         "--env-var", "projectB1Id=prj00000000000000002",
         "--env-var", "existingProjectId=prj00000000000000001",
         "--env-var", "existingProjectCrossId=prj00000000000000002",
         "--env-var", "jwtProjectAdminA1=selftest-token",
         "--env-var", "jwtBootstrap=selftest-token",
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

        # (1) ЗАКОННЫЙ БЛИЗНЕЦ: страж предусловия сработал — утверждения обязаны молчать.
        _Handler.mode = "ok"
        ok = _run_newman(COLLECTION, folder, base_url, tmpd / "ok.json")
        if ok["assertions"]["failed"] != 0:
            problems.append(f"на ЗДОРОВОМ ответе кейс краснеет ({ok['assertions']['failed']} "
                            f"упавших): {ok['failures']}. Такое утверждение краснеет всегда, "
                            f"и его краснота на дефекте ничего не значит")
        if ok["assertions"]["total"] == 0:
            problems.append("на здоровом ответе не исполнилось НИ ОДНОГО утверждения — "
                            "«ноль находок» здесь означает «ноль прочитанного»")

        # (2а) ИНЪЕКЦИЯ РЕГРЕССИЕЙ ПРАВ — предмет самой матрицы.
        _Handler.mode = "denied"
        denied = _run_newman(COLLECTION, folder, base_url, tmpd / "denied.json")
        if denied["assertions"]["failed"] == 0:
            problems.append("на ОТКАЗЕ В ПРАВАХ кейс зелёный — матрица не ловит то, "
                            "ради чего написана")
        elif not any(DENIED_TOKEN in f for f in denied["failures"]):
            problems.append(f"кейс упал, но текст падения НЕ НАЗЫВАЕТ отказ "
                            f"({DENIED_TOKEN!r}): {denied['failures']}")

        # (2б) ИНЪЕКЦИЯ УСПЕХОМ — то, чего прежняя форма не видела вовсе.
        _Handler.mode = "created"
        created = _run_newman(COLLECTION, folder, base_url, tmpd / "created.json")
        if created["assertions"]["failed"] == 0:
            problems.append("на УСПЕХЕ мутации кейс зелёный — утверждение не отличает "
                            "«страж сработал» от «машина создана»")
        elif not any(CREATED_TOKEN in f for f in created["failures"]):
            problems.append(f"кейс упал, но текст падения НЕ НАЗЫВАЕТ полученный ответ "
                            f"({CREATED_TOKEN!r}): {created['failures']}")

        # (3) ПРЕЖНЯЯ ФОРМА на УСПЕХЕ — обязана быть ЗЕЛЁНОЙ. Доказательство предмета #668.
        legacy = _legacy_collection(tmpd / "legacy.json", folder)
        legacy_run = _run_newman(legacy, folder, base_url, tmpd / "legacy-report.json")
        if legacy_run["assertions"]["failed"] != 0:
            problems.append(f"прежняя форма на успехе мутации ПОКРАСНЕЛА "
                            f"({legacy_run['failures']}) — значит предмет issue #668 "
                            f"воспроизведён неверно и вывод об усилении не обоснован")
        if legacy_run["assertions"]["total"] == 0:
            problems.append("прежняя форма не исполнила НИ ОДНОГО утверждения — "
                            "её зелень означала бы «не проверяли», а не «не увидела дефект»")

    server.shutdown()

    print(f"selftest ALLOW-полосы матрицы: кейс {folder!r}")
    print(f"  страж предусловия (400): утверждений {ok['assertions']['total']}, "
          f"упало {ok['assertions']['failed']}")
    print(f"  отказ в правах    (403): утверждений {denied['assertions']['total']}, "
          f"упало {denied['assertions']['failed']}")
    for f in denied["failures"]:
        print(f"      падение: {f}")
    print(f"  успех мутации     (200): утверждений {created['assertions']['total']}, "
          f"упало {created['assertions']['failed']}")
    for f in created["failures"]:
        print(f"      падение: {f}")
    print(f"  прежняя форма на успехе: утверждений {legacy_run['assertions']['total']}, "
          f"упало {legacy_run['assertions']['failed']}  ← ЗЕЛЕНО НА ДЕФЕКТЕ (предмет #668)")

    if problems:
        print("\nselftest: FAIL")
        for p in problems:
            print(f"  - {p}")
        return 1
    print("\nselftest: OK — утверждение различает страж предусловия, отказ в правах и "
          "успех мутации; прежняя форма успеха не видела")
    return 0


if __name__ == "__main__":
    sys.exit(main())
