#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Уборка ПОДЗАПРОСАМИ различает законный ответ и дефект — и падает по своей причине.

# Предмет

Состязательные кейсы (`cases/concurrency.py`) убирают за собой залпом из
`pm.sendRequest`: идентификаторы известны только в рантайме. Сам шаг при этом
ходит на `/healthz`, поэтому НИ ОДНА проверка, судящая шаг по его собственному
ответу, его исход не видит — ни гейт дерева
`internal/repohygiene/artifactgates` `TestCapturedVariableStepCarriesAnAssertion`
(его предмет — захват из СВОЕГО ответа), ни обёртка окна видимости
(`retry_until_authorized` решает по коду ответа ШАГА), ни проход, дописывающий
утверждение шагу удаления (он смотрит на метод шага, а тут GET).

Пять шагов уборки жили в этой слепой зоне: `cleanup-all-subs`, `wait-cleanup`,
`cleanup-addresses`, `cleanup-nics`, `wait-nic-cleanup`. У одного обработчик
ответа был ПУСТ (`() => {}`), у остальных — накапливал и не утверждал ничего.
Острее прочих был первый из адресных: адрес берётся из ОГРАНИЧЕННОГО пула, и
невозвращённый слот пул не пополняет — под параллельным прогоном пул
исчерпывается, следующее выделение отвечает отказом, кейс получает фантомный
ресурс, дальше каскад.

# Что здесь доказывается — и в обе стороны

Проба гоняет НАСТОЯЩИЙ newman по НАСТОЯЩИМ шагам собранной коллекции против
подставного сервера, отдающего ровно объявленные формы ответа.

ПОЛОСА УБОРКИ (`cleanup-addresses`):
  1. законный близнец — 200 и конверт Operation: молчит;
  2. законный близнец — 403 на первых попытках, затем 200 (окно материализации
     owner-tuple): молчит, потому что окно ПЕРЕЖИДАЕТСЯ, а не принимается;
  3. инъекция — 400 и код 9 («ресурс занят»): падает. Здесь это не «уборке не
     повезло»: предмет создан этим же кейсом, ни к чему не привязан, и «занят»
     означает ровно ту утечку, ради которой шаг существует;
  4. инъекция — 500: падает;
  5. инъекция — 200 без конверта Operation: падает;
  6. инъекция — залипший 403 (бюджет исчерпан): падает.

ПОЛОСА ИСХОДА ОПЕРАЦИИ (`wait-addr-cleanup`):
  7. законный близнец — операция `done` без ошибки: молчит;
  8. инъекция — операция `done` С ОШИБКОЙ: падает;
  9. инъекция — накопитель предыдущего шага НЕПОЛОН (обработчик не отстрелил):
     падает СИНХРОННОЕ утверждение о полноте.

ПРЕЖНЯЯ ФОРМА измеряется на тех же дефектах и обязана быть СЛЕПОЙ: ноль
исполненных утверждений. Без этой стороны «мы усилили проверку» осталось бы
заявлением. Прежние скрипты воспроизведены ДОСЛОВНО (сняты из дерева тем же
изменением, что и завело эту пробу).

# Чем это слабее прогона против стенда — названо прямо

Здесь исполняется настоящий newman и настоящий сериализатор коллекции, но
ответы даёт подставной сервер. Доказано: «утверждения различают эти ответы».
НЕ доказано: «продукт отвечает именно так» — это свойство продукта, и его
подтверждает прогон против поднятого стенда.

Запуск: python3 scripts/selftest_burst_cleanup_lane.py   (стенд не нужен, newman нужен)
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

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
COLLECTION = ROOT / "collections" / "concurrency.postman_collection.json"

# Шаги, чей исход эта проба проверяет. Перечень назван ЯВНО: шаблон, не нашедший
# ни одного шага, вышел бы успехом, и «ноль исполненного» стало бы неотличимо от
# «ноль находок».
CLEANUP_STEP = "cleanup-addresses"
AWAIT_STEP = "wait-addr-cleanup"
ALL_CLEANUP_STEPS = ("cleanup-all-subs", "wait-cleanup", "cleanup-addresses",
                     "cleanup-nics", "wait-nic-cleanup", "wait-addr-cleanup")

ASSERT_FORMS = ("pm.test(", "pm.expect(", "pm.response.to.")

# --- ПРЕЖНЯЯ ФОРМА, воспроизведённая дословно --------------------------------
# Снята из дерева тем же изменением, что завело эту пробу. Нужна ровно для одного:
# показать, что перечисленные дефекты она НЕ ВИДЕЛА — не «плохо видела», а не
# исполняла на них ни одного утверждения.
LEGACY_CLEANUP_ADDRESSES = [
    "const ids = JSON.parse(pm.environment.get('cleanupAddrIds') || '[]');",
    "const base = pm.environment.get('baseUrl');",
    "const tok = pm.environment.get('jwtProjectAdminA1');",
    "ids.forEach(id => {",
    "  pm.sendRequest({",
    "    url: base + '/vpc/v1/addresses/' + id,",
    "    method: 'DELETE',",
    "    header: { 'Authorization': 'Bearer ' + tok },",
    "  }, () => {});",
    "});",
]

LEGACY_WAIT_CLEANUP = [
    "const opIds = JSON.parse(pm.environment.get('cleanupOpIds') || '[]');",
    "const base = pm.environment.get('baseUrl');",
    "const tok = pm.environment.get('jwtProjectAdminA1');",
    "let pending = opIds.length;",
    "if (pending === 0) { return; }",
    "const tryOne = (oid, attempt) => {",
    "  pm.sendRequest({",
    "    url: base + '/operations/' + oid,",
    "    method: 'GET',",
    "    header: { 'Authorization': 'Bearer ' + tok },",
    "  }, (err, res) => {",
    "    let j = null; try { j = res.json(); } catch (e) {}",
    "    if ((j && j.done) || attempt >= 10) { pending--; }",
    "    else { setTimeout(() => tryOne(oid, attempt + 1), 400); return; }",
    "  });",
    "};",
    "opIds.forEach(oid => tryOne(oid, 0));",
]

# --- формы ответа подставного сервера ----------------------------------------
DEL_ACCEPTED = (200, {"id": "vpo00000000000000001", "description": "selftest",
                      "done": False, "metadata": {"addressId": "adr00000000000000001"}})
DEL_ACCEPTED_NO_ENVELOPE = (200, {"done": False})
DEL_REFUSED_STATE = (400, {"code": 9, "message": "address adr… is in use by …", "details": []})
DEL_DENIED = (403, {"code": 7, "message": "permission denied", "details": []})
DEL_INTERNAL = (500, {"code": 13, "message": "internal error", "details": []})

OP_DONE_OK = (200, {"id": "vpo00000000000000001", "done": True,
                    "metadata": {"addressId": "adr00000000000000001"}})
OP_DONE_ERROR = (200, {"id": "vpo00000000000000001", "done": True,
                       "error": {"code": 9, "message": "address is in use"},
                       "metadata": {"addressId": "adr00000000000000001"}})

# Режим сервера: (форма DELETE, форма чтения операции, сколько первых DELETE
# отвечают отказом в правах прежде чем полоса сойдётся).
MODES: dict[str, tuple] = {}


class _Handler(http.server.BaseHTTPRequestHandler):
    mode = "del_ok"
    seen_delete = 0

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
        _delete, op, _warm = MODES[_Handler.mode]
        if self.path.startswith("/operations/"):
            return self._send(*op)
        return self._send(200, {"status": "ok"})

    def do_DELETE(self):  # noqa: N802
        delete, _op, warm = MODES[_Handler.mode]
        _Handler.seen_delete += 1
        # `warm` первых удалений отвечают отказом в правах — так выглядит окно
        # материализации owner-tuple, которое шаг обязан ПЕРЕЖДАТЬ, а не принимать.
        if _Handler.seen_delete <= warm:
            return self._send(*DEL_DENIED)
        return self._send(*delete)


def _step_from_tree(name: str) -> dict:
    """Шаг берётся из СОБРАННОЙ коллекции — фикстура привязана к дереву."""
    col = json.loads(COLLECTION.read_text())
    found = []

    def walk(items):
        for it in items:
            if "item" in it:
                walk(it["item"])
                continue
            if it.get("name") == name:
                found.append(it)

    walk(col["item"])
    if len(found) != 1:
        sys.exit(f"selftest: шаг {name!r} не найден однозначно (найдено {len(found)}) — "
                 f"предпосылка пробы сломана, её молчание ничего не доказывает")
    return found[0]


def _step_code(step: dict) -> str:
    for ev in step.get("event", []):
        if ev.get("listen") == "test":
            return "\n".join(ev.get("script", {}).get("exec", []))
    return ""


def _one_step_collection(dst: Path, step: dict, script: list[str] | None = None) -> Path:
    """Одношаговая коллекция вокруг ШАГА ИЗ ДЕРЕВА (или его прежней формы)."""
    item = json.loads(json.dumps(step))
    if script is not None:
        item["event"] = [{"listen": "test",
                          "script": {"type": "text/javascript", "exec": list(script)}}]
    col = {
        "info": {"name": "selftest-burst-cleanup",
                 "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
        "item": [{"name": "SELFTEST-BURST-CLEANUP", "item": [item]}],
    }
    dst.write_text(json.dumps(col, ensure_ascii=False))
    return dst


def _run_newman(collection: Path, report: Path, env: dict) -> dict:
    args = ["newman", "run", str(collection), "--folder", "SELFTEST-BURST-CLEANUP",
            "--reporters", "json", "--reporter-json-export", str(report),
            "--timeout-request", "5000"]
    for k, v in env.items():
        args += ["--env-var", f"{k}={v}"]
    subprocess.run(args, capture_output=True, check=False, text=True, timeout=600)
    if not report.exists():
        sys.exit("selftest: newman не оставил отчёта — это «не выполнилось», а не вердикт")
    run = json.loads(report.read_text())["run"]
    failures = [f"{f.get('error', {}).get('test', '')}: {f.get('error', {}).get('message', '')}"
                for f in run.get("failures", [])]
    return {"assertions": run["stats"]["assertions"], "failures": failures}


def main() -> int:  # noqa: C901 — линейный перечень проб, дробить нечего
    if shutil.which("newman") is None:
        print("selftest: newman не установлен — проба НЕ ИСПОЛНЕНА (это не зелёный вердикт)")
        return 2

    problems: list[str] = []

    # --- предпосылка: ВСЕ шаги подзапросной уборки несут утверждение ----------
    # Без неё проба доказывала бы свойство одного шага и молчала бы о четырёх
    # остальных ровно тогда, когда с них снимут утверждения.
    for name in ALL_CLEANUP_STEPS:
        code = _step_code(_step_from_tree(name))
        if not any(form in code for form in ASSERT_FORMS):
            problems.append(f"шаг {name!r} собранной коллекции не несёт НИ ОДНОГО утверждения — "
                            f"его исход не прочтёт никто")

    cleanup_step = _step_from_tree(CLEANUP_STEP)
    await_step = _step_from_tree(AWAIT_STEP)

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
    base_url = f"http://127.0.0.1:{server.server_address[1]}"
    threading.Thread(target=server.serve_forever, daemon=True).start()

    ids = ["adr00000000000000001", "adr00000000000000002"]
    ops = ["vpo00000000000000001", "vpo00000000000000002"]
    outcomes = [{"id": i, "code": 200} for i in ids]

    env_cleanup = {"baseUrl": base_url, "jwtProjectAdminA1": "selftest-token",
                   "cleanupAddrIds": json.dumps(ids)}
    env_await = dict(env_cleanup)
    env_await.update({"addrCleanupOpIds": json.dumps(ops),
                      "addrCleanupOutcomes": json.dumps(outcomes)})
    env_await_short = dict(env_cleanup)
    env_await_short.update({"addrCleanupOpIds": json.dumps(ops[:1]),
                            "addrCleanupOutcomes": json.dumps(outcomes[:1])})

    runs: dict[str, dict] = {}
    with tempfile.TemporaryDirectory() as tmp:
        tmpd = Path(tmp)
        cleanup_coll = _one_step_collection(tmpd / "cleanup.json", cleanup_step)
        await_coll = _one_step_collection(tmpd / "await.json", await_step)
        legacy_cleanup = _one_step_collection(tmpd / "legacy-cleanup.json", cleanup_step,
                                              LEGACY_CLEANUP_ADDRESSES)
        legacy_await = _one_step_collection(tmpd / "legacy-await.json", await_step,
                                            LEGACY_WAIT_CLEANUP)

        def go(key: str, mode: tuple, collection: Path, env: dict) -> dict:
            MODES[key] = mode
            _Handler.mode = key
            _Handler.seen_delete = 0
            runs[key] = _run_newman(collection, tmpd / f"{key}.json", env)
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
                                f"упасть по своей причине, а это и есть предмет пробы")
            elif not any(needle in f for f in runs[key]["failures"]):
                problems.append(f"шаг упал, но текст падения не называет «{needle}» "
                                f"({runs[key]['failures']}) — диагноз по имени шага вместо текста "
                                f"отказа стоит полного прогона")

        def must_be_blind(key: str, what: str) -> None:
            if runs[key]["assertions"]["total"] != 0:
                problems.append(f"ПРЕЖНЯЯ форма исполнила утверждения на «{what}» "
                                f"({runs[key]['assertions']}) — предмет воспроизведён неверно, "
                                f"и вывод об усилении не обоснован")

        # ---------------- ПОЛОСА УБОРКИ ----------------
        go("del_ok", (DEL_ACCEPTED, OP_DONE_OK, 0), cleanup_coll, env_cleanup)
        must_be_silent("del_ok", "200 и конверт Operation")

        go("del_window", (DEL_ACCEPTED, OP_DONE_OK, 2), cleanup_coll, env_cleanup)
        must_be_silent("del_window", "403 в окне материализации, затем 200")

        go("del_state", (DEL_REFUSED_STATE, OP_DONE_OK, 0), cleanup_coll, env_cleanup)
        must_fail_naming("del_state", "принято", "400 и код 9 — предмет остался жить")

        go("del_internal", (DEL_INTERNAL, OP_DONE_OK, 0), cleanup_coll, env_cleanup)
        must_fail_naming("del_internal", "принято", "500 на удалении")

        go("del_no_envelope", (DEL_ACCEPTED_NO_ENVELOPE, OP_DONE_OK, 0), cleanup_coll, env_cleanup)
        must_fail_naming("del_no_envelope", "принято", "200 без конверта Operation")

        # Залипший отказ в правах: бюджет повтора исчерпывается, и терминальный
        # отказ обязан упасть — иначе окно ожидания стало бы маской.
        go("del_denied", (DEL_DENIED, OP_DONE_OK, 10 ** 6), cleanup_coll, env_cleanup)
        must_fail_naming("del_denied", "принято", "залипший 403 (бюджет исчерпан)")

        # ---------------- ПОЛОСА ИСХОДА ОПЕРАЦИИ ----------------
        go("op_ok", (DEL_ACCEPTED, OP_DONE_OK, 0), await_coll, env_await)
        must_be_silent("op_ok", "операция done без ошибки")

        go("op_error", (DEL_ACCEPTED, OP_DONE_ERROR, 0), await_coll, env_await)
        must_fail_naming("op_error", "БЕЗ ошибки", "операция уборки завершилась ошибкой")

        go("op_short", (DEL_ACCEPTED, OP_DONE_OK, 0), await_coll, env_await_short)
        must_fail_naming("op_short", "отчитался по всем", "накопитель неполон")

        # ---------------- ПРЕЖНЯЯ ФОРМА: СЛЕПА ----------------
        for key, mode, what in (
            ("legacy_state", (DEL_REFUSED_STATE, OP_DONE_OK, 0), "400 и код 9"),
            ("legacy_internal", (DEL_INTERNAL, OP_DONE_OK, 0), "500 на удалении"),
            ("legacy_denied", (DEL_DENIED, OP_DONE_OK, 10 ** 6), "залипший 403"),
        ):
            go(key, mode, legacy_cleanup, env_cleanup)
            must_be_blind(key, what)

        go("legacy_op_error", (DEL_ACCEPTED, OP_DONE_ERROR, 0), legacy_await,
           {**env_await, "cleanupOpIds": json.dumps(ops)})
        must_be_blind("legacy_op_error", "операция завершилась ошибкой")

    server.shutdown()

    print(f"selftest полосы подзапросной уборки: шагов проверено {len(ALL_CLEANUP_STEPS)}, "
          f"коллекция {COLLECTION.name}")
    order = [
        ("del_ok", "уборка: 200 + Operation (близнец)     "),
        ("del_window", "уборка: 403→200, окно (близнец)       "),
        ("del_state", "уборка: 400 + код 9                   "),
        ("del_internal", "уборка: 500                           "),
        ("del_no_envelope", "уборка: 200 без конверта              "),
        ("del_denied", "уборка: залипший 403                  "),
        ("op_ok", "операция: done без ошибки (близнец)   "),
        ("op_error", "операция: done С ОШИБКОЙ              "),
        ("op_short", "операция: накопитель неполон          "),
        ("legacy_state", "ПРЕЖНЯЯ форма на 400 + код 9          "),
        ("legacy_internal", "ПРЕЖНЯЯ форма на 500                  "),
        ("legacy_denied", "ПРЕЖНЯЯ форма на залипшем 403         "),
        ("legacy_op_error", "ПРЕЖНЯЯ форма на ошибке операции      "),
    ]
    for key, label in order:
        st = runs[key]["assertions"]
        print(f"  {label}: утверждений {st['total']}, упало {st['failed']}")
        for f in runs[key]["failures"]:
            print(f"      падение: {f[:160]}")

    if problems:
        print("\nselftest FAIL:")
        for p in problems:
            print(f"  - {p}")
        return 1
    print("\nselftest: OK — уборка подзапросами молчит на двух законных близнецах (принято; "
          "окно материализации пережидается) и падает на четырёх дефектах удаления и двух "
          "дефектах исхода операции, называя предмет в тексте. ПРЕЖНЯЯ форма измерена на "
          "четырёх из них и слепа ко всем: ноль исполненных утверждений. Больше этого замер "
          "не даёт, и заявлять больше нельзя.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
