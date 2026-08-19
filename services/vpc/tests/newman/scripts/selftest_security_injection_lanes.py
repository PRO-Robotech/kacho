#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Проба внедрения в имя СПОСОБНА УПАСТЬ — и падает по СВОЕЙ причине.

# Предмет

Кейс `*-CR-SEC-*` шлёт в `name` полезную нагрузку (SQLi/XSS/cmd/path/пробел/
union/1000 символов) и до issue #670 утверждал ОДНО:

    pm.test('handled 2xx/4xx', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 413]));

Одно утверждение принимало И успех, И отказ, то есть приняло бы ровно ту
регрессию валидации имени, ради обнаружения которой проба и написана. Исход при
этом УСТАНОВЛЕН: ни одна из семи нагрузок не соответствует самому
разрешительному контракту имени (`corevalidate.NameVPC`), отказ синхронный и
стоит до обращения к соседям, а край переводит `INVALID_ARGUMENT` в `400`
(`api-conventions.md` §«gRPC-код → HTTP-статус»; `413` не производится ни для
одного кода). Поэтому кейс утверждает ПАРУ — статус И код — плюс имя поля.

# Инъекция ПЯТИСТОРОННЯЯ: у каждого утверждения свой производитель

  1. ЗАКОННЫЙ БЛИЗНЕЦ — объявленный отказ (400 + code 3 + fieldViolation "name"):
     утверждения обязаны МОЛЧАТЬ. Без этой стороны «краснеет всегда» неотличимо
     от «краснеет на дефекте»;
  2. РЕГРЕССИЯ ВАЛИДАЦИИ — имя ПРИНЯТО (200 + конверт Operation): кейс обязан
     упасть и НАЗВАТЬ статус. Это тот самый дефект, который прежняя форма
     пропускала;
  3. ПРЕЖНЯЯ ФОРМА на ТОМ ЖЕ ответе — обязана быть ЗЕЛЁНОЙ. Без неё «мы усилили
     утверждение» остаётся заявлением, а не измерением;
  4. ДРУГОЙ КОД ПРИ ТОМ ЖЕ СТАТУСЕ (400 + code 9, `FAILED_PRECONDITION`): падает
     утверждение о коде, а утверждение о статусе молчит. Это и есть довод в
     пользу ПАРЫ: один HTTP-статус не отличает валидацию от состояния ресурса —
     `INVALID_ARGUMENT`, `FAILED_PRECONDITION` и `OUT_OF_RANGE` все дают 400;
  5. ОТКАЗ БЕЗ НАЗВАННОГО ПОЛЯ (400 + code 3, без `BadRequest`): падает
     утверждение о поле. Без этой стороны третье утверждение было бы украшением —
     нельзя показать, что у него есть собственный производитель.

# ШЕСТАЯ сторона: нагрузка ДОШЛА до сервера ровно той, что лежит в коллекции

Пять сторон выше судят об УТВЕРЖДЕНИЯХ. Они остались бы зелёными и в том случае,
если бы newman исказил или потерял саму нагрузку по дороге, — а для нулевого байта
это не праздный вопрос: в JSON он выразим только экранирующей последовательностью,
и на пути «коллекция → newman → HTTP → разбор» его теряет любое звено, читающее
строку как C-строку.

Поэтому подставной сервер ЗАПОМИНАЕТ тело запроса, и проба сверяет полученное с
тем, что стоит в коллекции, ПОБАЙТНО — для кейса `NET-CR-SEC-NULLBYTE` отдельно
(#701: до фикса нагрузка с этим именем несла `x`, ПРОБЕЛ, `y`, и исход совпадал,
поэтому подмена не краснела нигде).

Соответствие ИМЕНИ вида его БАЙТАМ во всех наборах держит гейт по дереву
`internal/repohygiene/artifactgates`
`TestInjectionPayloadCarriesWhatItsNamePromises`; здесь проверяется другое —
что нагрузка доезжает до сервера неискажённой.

# Чем это слабее прогона против стенда — названо прямо

Здесь исполняется НАСТОЯЩИЙ newman и НАСТОЯЩАЯ сгенерированная коллекция, но
ответы даёт подставной сервер. Доказано: «утверждения различают эти пять
ответов». НЕ доказано: «продукт на такой нагрузке отвечает именно так» — это
свойство продукта, и его подтверждают прогон против поднятого стенда и
`*-CR-VAL-NAME-SPECIAL-CHARS` из того же набора (тот же путь, тот же класс
ввода, та же пара). Подставной сервер намеренно НЕ богаче контракта: он отдаёт
ровно те формы, что перечислены выше.

Запуск: python3 scripts/selftest_security_injection_lanes.py   (стенд не нужен, newman нужен)
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
COLLECTION = ROOT / "collections" / "network.postman_collection.json"
CASE_PREFIX = "NET-CR-SEC-SQLI"
# Кейс, чья нагрузка проверяется ПОБАЙТНО (шестая сторона выше).
BYTEWISE_CASE_PREFIX = "NET-CR-SEC-NULLBYTE"

# Объявленный отказ: пара «400 ↔ INVALID_ARGUMENT» + имя поля. Форма детали —
# `google.rpc.BadRequest`, её строит `serviceerr.FromValidation` (пять ресурсов)
# и `coreerrors.Builder.AddFieldViolation` (Gateway) — одинаково.
REFUSAL_BODY = {
    "code": 3,
    "message": "invalid argument",
    "details": [{
        "@type": "type.googleapis.com/google.rpc.BadRequest",
        "fieldViolations": [{
            "field": "name",
            "description": "name must match ^([a-zA-Z]([-_a-zA-Z0-9]{0,61}[a-zA-Z0-9])?)?$",
        }],
    }],
}
# Тот же статус, ДРУГОЙ код: состояние ресурса вместо валидации ввода.
WRONG_CODE_BODY = {"code": 9, "message": "network is not empty", "details": []}
# Отказ, не назвавший поля: пара сходится, деталь отсутствует.
NO_DETAIL_BODY = {"code": 3, "message": "invalid argument", "details": []}
# Регрессия валидации: имя ПРИНЯТО. `RunSync` исполняет воркер синхронно, поэтому
# `done` уже true — именно так выглядит успешная мутация края.
ACCEPTED_OP = {"id": "vpo00000000000000001", "description": "selftest",
               "done": True, "metadata": {"networkId": "net00000000000000001"}}

MODES = {
    "refuse": (400, REFUSAL_BODY),
    "accept": (200, ACCEPTED_OP),
    "wrong_code": (400, WRONG_CODE_BODY),
    "no_detail": (400, NO_DETAIL_BODY),
}

# Прежняя форма утверждения — воспроизведена ДОСЛОВНО (снята из gen.py коммитом
# issue #670), чтобы третья сторона показывала именно её, а не пересказ.
LEGACY_ASSERTS = [
    "pm.test('[LEGACY] not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
    "pm.test('[LEGACY] handled 2xx/4xx', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 413]));",
]


class _Handler(http.server.BaseHTTPRequestHandler):
    mode = "refuse"
    # Тела POST-запросов к создающему пути, в порядке поступления.
    received: list[bytes] = []

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
            return self._send(200, ACCEPTED_OP)
        return self._send(404, {"code": 5, "message": "not found"})

    def do_POST(self):  # noqa: N802
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b""
        if self.path.endswith("/vpc/v1/networks"):
            # Тело запоминается ЦЕЛИКОМ и в байтах: шестая сторона инъекции
            # сверяет дошедшую нагрузку с той, что лежит в коллекции, а разница
            # между пробелом и нулевым байтом видна только побайтно.
            _Handler.received.append(raw)
            return self._send(*MODES[_Handler.mode])
        return self._send(404, {"code": 5, "message": "not found"})

    def do_DELETE(self):  # noqa: N802
        return self._send(200, ACCEPTED_OP)


def _case_folder_name(prefix: str = CASE_PREFIX) -> str:
    coll = json.loads(COLLECTION.read_text())
    for item in coll["item"]:
        if item["name"].startswith(prefix):
            return item["name"]
    sys.exit(f"selftest: кейса {prefix} нет в коллекции — предпосылка пробы сломана, "
             f"молчание ничего не доказывает")


def _collection_payload(folder: str) -> bytes:
    """Нагрузка, ЛЕЖАЩАЯ в коллекции: поле `name` тела создающего шага, в байтах.

    Читается тем же порядком, каким её читает край: сперва коллекция, затем строка
    `raw` как JSON. Иначе сверять было бы не с чем — экранирующая запись и байт это
    разные вещи, и предмет #701 ровно в их различении.
    """
    coll = json.loads(COLLECTION.read_text())
    for item in coll["item"]:
        if item["name"] != folder:
            continue
        for step in item.get("item", []):
            req = step.get("request") or {}
            url = (req.get("url") or {}).get("raw", "")
            raw = (req.get("body") or {}).get("raw")
            if req.get("method") == "POST" and url.endswith("/vpc/v1/networks") and raw:
                return json.loads(raw)["name"].encode()
    sys.exit(f"selftest: в кейсе {folder} нет создающего шага с телом — сверять нечего")


def _legacy_collection(dst: Path, folder: str) -> Path:
    """Коллекция, у которой шаг внедрения несёт ПРЕЖНЮЮ форму утверждения."""
    coll = json.loads(COLLECTION.read_text())
    patched = 0
    for item in coll["item"]:
        if item["name"] != folder:
            continue
        for step in item.get("item", []):
            url = ((step.get("request") or {}).get("url") or {}).get("raw", "")
            if step.get("request", {}).get("method") == "POST" and url.endswith("/vpc/v1/networks"):
                step["event"] = [{"listen": "test",
                                  "script": {"type": "text/javascript",
                                             "exec": list(LEGACY_ASSERTS)}}]
                patched += 1
    if patched != 1:
        sys.exit(f"selftest: шаг внедрения в кейсе не найден однозначно (найдено {patched}) — "
                 f"третья сторона инъекции не построена, вердикт недействителен")
    dst.write_text(json.dumps(coll))
    return dst


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

    folder = _case_folder_name()
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
    base_url = f"http://127.0.0.1:{server.server_address[1]}"
    threading.Thread(target=server.serve_forever, daemon=True).start()

    problems: list[str] = []
    runs: dict[str, dict] = {}
    with tempfile.TemporaryDirectory() as tmp:
        tmpd = Path(tmp)

        # (1) ЗАКОННЫЙ БЛИЗНЕЦ: объявленный отказ — утверждения обязаны молчать.
        _Handler.mode = "refuse"
        runs["refuse"] = _run_newman(COLLECTION, folder, base_url, tmpd / "refuse.json")
        if runs["refuse"]["assertions"]["failed"] != 0:
            problems.append(f"на ОБЪЯВЛЕННОМ ОТКАЗЕ кейс краснеет "
                            f"({runs['refuse']['assertions']['failed']} упавших): "
                            f"{runs['refuse']['failures']}. Такое утверждение краснеет всегда, "
                            f"и его краснота на дефекте ничего не значит")
        if runs["refuse"]["assertions"]["total"] == 0:
            problems.append("на объявленном отказе не исполнилось НИ ОДНОГО утверждения — "
                            "«ноль находок» здесь означает «ноль прочитанного»")

        # (2) ИНЪЕКЦИЯ: имя ПРИНЯТО — кейс обязан упасть И НАЗВАТЬ статус.
        _Handler.mode = "accept"
        runs["accept"] = _run_newman(COLLECTION, folder, base_url, tmpd / "accept.json")
        if runs["accept"]["assertions"]["failed"] == 0:
            problems.append("на ПРИНЯТОМ имени кейс зелёный — утверждение неспособно упасть "
                            "по своей причине, а это и есть предмет issue #670")
        elif not any("status 400" in f for f in runs["accept"]["failures"]):
            problems.append(f"кейс упал, но текст падения НЕ НАЗЫВАЕТ статус: "
                            f"{runs['accept']['failures']}. Диагноз по имени шага вместо текста "
                            f"отказа стоит полного прогона")

        # (3) ПРЕЖНЯЯ ФОРМА на том же ответе — обязана быть ЗЕЛЁНОЙ.
        legacy = _legacy_collection(tmpd / "legacy.json", folder)
        runs["legacy"] = _run_newman(legacy, folder, base_url, tmpd / "legacy-report.json")
        if runs["legacy"]["assertions"]["failed"] != 0:
            problems.append(f"прежняя форма утверждения на ПРИНЯТОМ имени ПОКРАСНЕЛА "
                            f"({runs['legacy']['failures']}) — значит предмет issue #670 "
                            f"воспроизведён неверно и вывод об усилении не обоснован")
        if runs["legacy"]["assertions"]["total"] == 0:
            problems.append("прежняя форма не исполнила ни одного утверждения — сравнивать не с чем")

        # (4) ТОТ ЖЕ СТАТУС, ДРУГОЙ КОД: падает утверждение о коде, не о статусе.
        _Handler.mode = "wrong_code"
        runs["wrong_code"] = _run_newman(COLLECTION, folder, base_url, tmpd / "wrong-code.json")
        if not any("grpc code 3" in f for f in runs["wrong_code"]["failures"]):
            problems.append(f"на 400 с кодом 9 утверждение о КОДЕ не упало "
                            f"({runs['wrong_code']['failures']}) — значит пара не утверждается, "
                            f"и один HTTP-статус выдаётся за различение валидации и состояния")
        if any("status 400" in f for f in runs["wrong_code"]["failures"]):
            problems.append("на 400 с кодом 9 упало утверждение о СТАТУСЕ — оно судит не о том, "
                            "о чём заявляет")

        # (5) ОТКАЗ БЕЗ НАЗВАННОГО ПОЛЯ: падает утверждение о поле.
        _Handler.mode = "no_detail"
        runs["no_detail"] = _run_newman(COLLECTION, folder, base_url, tmpd / "no-detail.json")
        if not any("field violation" in f for f in runs["no_detail"]["failures"]):
            problems.append(f"на отказе без `BadRequest` утверждение о ПОЛЕ не упало "
                            f"({runs['no_detail']['failures']}) — у него нет собственного "
                            f"производителя, то есть оно украшение")

        # (6) НАГРУЗКА ДОШЛА ДО СЕРВЕРА НЕИСКАЖЁННОЙ — кейс с нулевым байтом.
        #
        # Пять сторон выше судят об утверждениях и остались бы зелёными, потеряй
        # newman саму нагрузку. Здесь сверяется то, что ПОЛУЧИЛ сервер, с тем, что
        # ЛЕЖИТ в коллекции, — побайтно.
        nul_folder = _case_folder_name(BYTEWISE_CASE_PREFIX)
        expected = _collection_payload(nul_folder)
        if b"\x00" not in expected:
            problems.append(
                f"в коллекции у кейса {BYTEWISE_CASE_PREFIX} нагрузка БЕЗ нулевого байта "
                f"({expected!r}) — это и есть дефект #701: имя вида обещает одно, "
                f"а на край уходит другое")
        _Handler.mode = "refuse"
        _Handler.received.clear()
        runs["bytewise"] = _run_newman(COLLECTION, nul_folder, base_url, tmpd / "bytewise.json")
        if runs["bytewise"]["assertions"]["failed"] != 0:
            problems.append(f"кейс {BYTEWISE_CASE_PREFIX} краснеет на ОБЪЯВЛЕННОМ отказе "
                            f"({runs['bytewise']['failures']}) — законный исход у него тот же, "
                            f"что и у остальных нагрузок, и смена байтов его не меняет")
        if not _Handler.received:
            problems.append(f"сервер не получил ни одного создающего запроса от кейса "
                            f"{nul_folder!r} — это «не выполнилось», а не вердикт")
        else:
            got = json.loads(_Handler.received[-1].decode()).get("name", "").encode()
            if got != expected:
                problems.append(f"нагрузка исказилась по дороге: в коллекции {expected!r} "
                                f"({expected.hex()}), до сервера дошло {got!r} ({got.hex()})")
            elif b"\x00" not in got:
                problems.append(f"до сервера дошла нагрузка БЕЗ нулевого байта: {got.hex()} — "
                                f"значит звено тракта его теряет, и проба этого класса пуста")

    server.shutdown()

    print(f"selftest пробы внедрения в имя: кейс {folder!r}")
    order = [("refuse", "объявленный отказ (400/3/name)"),
             ("accept", "имя ПРИНЯТО (200)          "),
             ("legacy", "прежняя форма на нём       "),
             ("wrong_code", "400, но код 9              "),
             ("no_detail", "400/3 без BadRequest       "),
             ("bytewise", f"{BYTEWISE_CASE_PREFIX}, побайтно")]
    for key, label in order:
        r = runs.get(key)
        if r is None:
            continue
        print(f"  {label}: утверждений {r['assertions']['total']}, упало {r['assertions']['failed']}")
        for f in r["failures"]:
            print(f"      падение: {f}")
    if runs.get("legacy") and runs["legacy"]["assertions"]["failed"] == 0:
        print("  ↑ прежняя форма ЗЕЛЕНА на принятом имени — предмет issue #670")

    if problems:
        print("\nselftest: FAIL")
        for p in problems:
            print(f"  - {p}")
        return 1
    print("\nselftest: OK — утверждения различают отказ и приём, называют статус в тексте "
          "падения, каждое из трёх имеет своего производителя, прежняя форма дефекта не видела, "
          f"а нагрузка {BYTEWISE_CASE_PREFIX} дошла до сервера побайтно той же")
    return 0


if __name__ == "__main__":
    sys.exit(main())
