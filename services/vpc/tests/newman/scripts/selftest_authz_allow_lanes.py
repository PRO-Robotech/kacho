#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""ALLOW-полоса набора authz-deny СПОСОБНА УПАСТЬ — и падает, НАЗЫВАЯ пересечение.

# Предмет

Кейс `AUTHZ-SUBNET-CR-OWN-*` режет подсеть и объявляет единственный законный исход:
`200` + конверт Operation, завершившаяся без ошибки. Проверка «способна ли она
покраснеть» не может быть выведена из чтения — её надо ПОКАЗАТЬ, подав тот самый
ответ, который продукт даёт на пересечение адресов:

    HTTP 400 + {"code": 9, "message": "Subnet CIDRs can not overlap"}

Пара «400 ↔ FAILED_PRECONDITION» здесь не выдумана: код отказа объявлен
`services/vpc/internal/apps/kacho/api/subnet/create.go` (`checkSubnetCIDROverlap`,
синхронно — ДО создания Operation), а отображение кода в статус задаёт библиотека
края (`runtime.HTTPStatusFromCode`; край собирается БЕЗ `WithErrorHandler`, см.
`api-conventions.md` §«gRPC-код → HTTP-статус»).

# Инъекция ДВУСТОРОННЯЯ, и третья сторона — самая важная

  1. здоровый ответ → утверждения проходят (иначе «краснеет всегда» неотличимо от
     «краснеет на дефекте»);
  2. ответ с пересечением → утверждение ПАДАЕТ, и текст падения НАЗЫВАЕТ пересечение
     (иначе отчёт называет виновником невиновного);
  3. ПРЕЖНЯЯ форма утверждения («код не 403 и не 401») на ТОМ ЖЕ ответе с пересечением
     — ЗЕЛЁНАЯ. Это и есть предмет issue #505: отрицание не отличало исправную систему
     от той поломки, ради которой кейс написан.

# Чем это слабее прогона против стенда — названо прямо

Здесь исполняется НАСТОЯЩИЙ newman и НАСТОЯЩАЯ сгенерированная коллекция, но ответы
даёт подставной сервер. Значит доказано: «утверждение различает эти два ответа». НЕ
доказано: «продукт на пересечении отвечает именно так» — это свойство продукта, и его
подтверждает прогон против поднятого стенда. Подставной сервер намеренно НЕ богаче
контракта: он отдаёт ровно те две формы, что объявлены выше.

Слой 1 (невозможность самого пересечения) этой пробой НЕ доказывается и доказан быть
не может: он держится построением — нарезка идёт в сети, созданной этим же кейсом,
а `checkSubnetCIDROverlap` перечисляет подсети фильтром `{ProjectID, NetworkID}`,
поэтому в момент нарезки множество пусто. Проба показывает лишь, что если бы
пересечение случилось, кейс бы его НАЗВАЛ.

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
CASE_PREFIX = "AUTHZ-SUBNET-CR-OWN-PA1"

# Ответ владельца на пересечение адресов — форма, объявленная продуктом (см. шапку).
OVERLAP_BODY = {"code": 9, "message": "Subnet CIDRs can not overlap", "details": []}
OVERLAP_TOKEN = "Subnet CIDRs can not overlap"

# Здоровый ответ мутации: `RunSync` исполняет воркер синхронно, поэтому `done` уже true.
OK_OP = {"id": "vpo00000000000000001", "description": "selftest",
         "done": True, "metadata": {"networkId": "net00000000000000001"}}
OK_SUBNET_OP = {"id": "vpo00000000000000002", "description": "selftest",
                "done": True, "metadata": {"subnetId": "sub00000000000000001"}}

# Прежняя форма утверждения — воспроизведена ДОСЛОВНО, чтобы третья сторона инъекции
# показывала именно её, а не пересказ. Снята из этого файла коммитом issue #505.
LEGACY_ALLOW_ASSERTS = [
    "pm.test('[LEGACY] ALLOW: not 403 PermissionDenied', () => pm.expect(pm.response.code, "
    "'unexpected 403 with body: ' + pm.response.text()).to.not.equal(403));",
    "pm.test('[LEGACY] ALLOW: not 401 Unauthenticated', () => pm.expect(pm.response.code, "
    "'unexpected 401 with body: ' + pm.response.text()).to.not.equal(401));",
]


class _Handler(http.server.BaseHTTPRequestHandler):
    subnet_mode = "ok"  # 'ok' | 'overlap'

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
            return self._send(200, OK_OP)
        return self._send(404, {"code": 5, "message": "not found"})

    def do_POST(self):  # noqa: N802
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)
        if self.path.endswith("/vpc/v1/networks"):
            return self._send(200, OK_OP)
        if self.path.endswith("/vpc/v1/subnets"):
            if _Handler.subnet_mode == "overlap":
                return self._send(400, OVERLAP_BODY)
            return self._send(200, OK_SUBNET_OP)
        return self._send(404, {"code": 5, "message": "not found"})


def _case_folder_name() -> str:
    coll = json.loads(COLLECTION.read_text())
    for item in coll["item"]:
        if item["name"].startswith(CASE_PREFIX):
            return item["name"]
    sys.exit(f"selftest: кейса {CASE_PREFIX} нет в коллекции — предпосылка пробы сломана, "
             f"молчание ничего не доказывает")


def _legacy_collection(dst: Path, folder: str) -> Path:
    """Коллекция, у которой шаг нарезки несёт ПРЕЖНЮЮ форму утверждения."""
    coll = json.loads(COLLECTION.read_text())
    patched = 0
    for item in coll["item"]:
        if item["name"] != folder:
            continue
        for step in item.get("item", []):
            url = ((step.get("request") or {}).get("url") or {}).get("raw", "")
            if step.get("request", {}).get("method") == "POST" and url.endswith("/vpc/v1/subnets"):
                step["event"] = [{"listen": "test",
                                  "script": {"type": "text/javascript",
                                             "exec": list(LEGACY_ALLOW_ASSERTS)}}]
                patched += 1
    if patched != 1:
        sys.exit(f"selftest: шаг нарезки в кейсе не найден однозначно (найдено {patched}) — "
                 f"третья сторона инъекции не построена, вердикт недействителен")
    dst.write_text(json.dumps(coll))
    return dst


def _run_newman(collection: Path, folder: str, base_url: str, report: Path) -> dict:
    subprocess.run(
        ["newman", "run", str(collection), "--folder", folder,
         "--env-var", f"baseUrl={base_url}",
         "--env-var", "projectA1Id=prj00000000000000001",
         "--env-var", "projectB1Id=prj00000000000000002",
         "--env-var", "zoneA=zoneselftest0000001",
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
        _Handler.subnet_mode = "ok"
        ok = _run_newman(COLLECTION, folder, base_url, tmpd / "ok.json")
        if ok["assertions"]["failed"] != 0:
            problems.append(f"на ЗДОРОВОМ ответе кейс краснеет ({ok['assertions']['failed']} "
                            f"упавших): {ok['failures']}. Такое утверждение краснеет всегда, "
                            f"и его краснота на дефекте ничего не значит")
        if ok["assertions"]["total"] == 0:
            problems.append("на здоровом ответе не исполнилось НИ ОДНОГО утверждения — "
                            "«ноль находок» здесь означает «ноль прочитанного»")

        # (2) ИНЪЕКЦИЯ: пересечение адресов — утверждение обязано упасть И НАЗВАТЬ его.
        _Handler.subnet_mode = "overlap"
        bad = _run_newman(COLLECTION, folder, base_url, tmpd / "overlap.json")
        if bad["assertions"]["failed"] == 0:
            problems.append("на ответе с ПЕРЕСЕЧЕНИЕМ кейс зелёный — утверждение неспособно "
                            "упасть по своей причине")
        elif not any(OVERLAP_TOKEN in f for f in bad["failures"]):
            problems.append(f"кейс упал, но текст падения НЕ НАЗЫВАЕТ пересечение "
                            f"({OVERLAP_TOKEN!r}): {bad['failures']}. Диагноз по имени шага "
                            f"вместо текста отказа стоит полного прогона")

        # (3) ПРЕЖНЯЯ ФОРМА на том же ответе — обязана быть ЗЕЛЁНОЙ. Это доказательство
        #     предмета issue #505, а не украшение: без него «мы усилили утверждение»
        #     остаётся заявлением.
        legacy = _legacy_collection(tmpd / "legacy.json", folder)
        legacy_run = _run_newman(legacy, folder, base_url, tmpd / "legacy-report.json")
        if legacy_run["assertions"]["failed"] != 0:
            problems.append(f"прежняя форма утверждения на ответе с пересечением ПОКРАСНЕЛА "
                            f"({legacy_run['failures']}) — значит предмет issue #505 "
                            f"воспроизведён неверно и вывод об усилении не обоснован")

    server.shutdown()

    print(f"selftest authz ALLOW-полос: кейс {folder!r}")
    print(f"  здоровый ответ        : утверждений {ok['assertions']['total']}, "
          f"упало {ok['assertions']['failed']}")
    print(f"  ответ с пересечением  : утверждений {bad['assertions']['total']}, "
          f"упало {bad['assertions']['failed']}")
    for f in bad["failures"]:
        print(f"      падение: {f}")
    print(f"  прежняя форма на нём  : утверждений {legacy_run['assertions']['total']}, "
          f"упало {legacy_run['assertions']['failed']}  ← ЗЕЛЕНО НА ДЕФЕКТЕ (предмет #505)")

    if problems:
        print("\nselftest: FAIL")
        for p in problems:
            print(f"  - {p}")
        return 1
    print("\nselftest: OK — утверждение различает здоровый ответ и пересечение, "
          "называет пересечение в тексте падения, а прежняя форма его не видела")
    return 0


if __name__ == "__main__":
    sys.exit(main())
