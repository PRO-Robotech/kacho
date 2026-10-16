#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Проба: отказ транспорта посева не выдаёт себя за ответ края (kacho#3246).

ПРИЗНАК, ДЛЯ КОТОРОГО ПРОБА. Посев F8-S7-80 упал `RuntimeError: no op id: {}` на
`POST /iam/v1/internal/cluster/admins`: `{}` был не ответом края, а пустым выводом
`curl`, превращённым в словарь. Проба держит обе стороны:

  A. на петле не слушает никто — `SeedTransportError`, в тексте код `curl`, метод и
     путь; предъявитель в текст не попадает;
  B. сервер принял соединение и закрыл его без ответа — `SeedTransportError`;
  C. сервер ответил 200 с пустым телом — `SeedTransportError` (пустота — обрыв);
  D. БЛИЗНЕЦ A–C: сервер ответил JSON Operation — словарь с `id`, без исключения;
  E. БЛИЗНЕЦ: ответ без полей `{}` ТЕКСТОМ — словарь `{}`, без исключения (законный
     ответ края отличим от обрыва);
  F. ответ-отказ края JSON (`code`/`message`, HTTP 400) — словарь, без исключения:
     отказ продукта судят вызывающие, транспорт его не прячет и не раздувает.

Сеть — только петля 127.0.0.1, свой порт на пробу. Исполнитель —
`.github/scripts/run-python-probes.py` по образцу `tests/authz-fixtures/*_test.py`.
"""

import http.server
import pathlib
import socket
import sys
import threading

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import seed_transport as st  # noqa: E402

FAILURES: list[str] = []
CHECKS = [0]
SECRET = "probe-bearer-must-not-leak"


def check(name, ok, detail=""):
    CHECKS[0] += 1
    print(f"{'ок   ' if ok else 'ОТКАЗ'} {name}" + ("" if ok else f" — {detail}"))
    if not ok:
        FAILURES.append(f"{name}: {detail}")


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def call(url):
    try:
        return st.curl_json("POST", url, SECRET, {"subjectId": "usr0"}, surface="проба"), None
    except st.SeedTransportError as e:
        return None, str(e)


def serve(handler_cls):
    srv = http.server.HTTPServer(("127.0.0.1", 0), handler_cls)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def answering(status, payload):
    class H(http.server.BaseHTTPRequestHandler):
        def do_POST(self):  # noqa: N802
            n = int(self.headers.get("Content-Length") or 0)
            self.rfile.read(n)
            raw = payload.encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def log_message(self, *_a):
            pass
    return H


class Dropping(http.server.BaseHTTPRequestHandler):
    def do_POST(self):  # noqa: N802
        self.close_connection = True
        self.connection.shutdown(socket.SHUT_RDWR)

    def log_message(self, *_a):
        pass


def main():
    path = "/iam/v1/internal/cluster/admins"

    # A. Никто не слушает.
    got, err = call(f"http://127.0.0.1:{free_port()}{path}")
    check("A: никто не слушает — отказ транспорта, а не {}",
          got is None and err is not None and "curl код" in err and f"POST {path}" in err,
          f"ответ {got!r}, отказ {err!r}")
    check("A: предъявитель в текст отказа не попал", err is None or SECRET not in err, err)

    # B. Соединение принято и закрыто без ответа.
    srv = serve(Dropping)
    got, err = call(f"http://127.0.0.1:{srv.server_port}{path}")
    srv.shutdown()
    check("B: обрыв без ответа — отказ транспорта", got is None and err is not None,
          f"ответ {got!r}")

    # C. 200 с пустым телом.
    srv = serve(answering(200, ""))
    got, err = call(f"http://127.0.0.1:{srv.server_port}{path}")
    srv.shutdown()
    check("C: HTTP 200 с пустым телом — отказ", got is None and err is not None and "ПУСТЫМ" in err,
          f"ответ {got!r}, отказ {err!r}")

    # D. Близнец: настоящий Operation.
    srv = serve(answering(200, '{"id":"iamop0000000000000001","done":true}'))
    got, err = call(f"http://127.0.0.1:{srv.server_port}{path}")
    srv.shutdown()
    check("D: Operation края — словарь с id", err is None and (got or {}).get("id") == "iamop0000000000000001",
          f"ответ {got!r}, отказ {err!r}")

    # E. Близнец: `{}` текстом — законный ответ без полей.
    srv = serve(answering(200, "{}"))
    got, err = call(f"http://127.0.0.1:{srv.server_port}{path}")
    srv.shutdown()
    check("E: `{}` текстом — словарь {}, не отказ", err is None and got == {}, f"ответ {got!r}, отказ {err!r}")

    # F. Отказ продукта JSON — вызывающему, как есть.
    srv = serve(answering(400, '{"code":3,"message":"subject_id: required"}'))
    got, err = call(f"http://127.0.0.1:{srv.server_port}{path}")
    srv.shutdown()
    check("F: отказ края JSON — словарь с code, транспорт его не прячет",
          err is None and (got or {}).get("code") == 3, f"ответ {got!r}, отказ {err!r}")

    print(f"\nперепись: проверок исполнено {CHECKS[0]}, отказов {len(FAILURES)}")
    if not CHECKS[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    for f in FAILURES:
        print(f"  ОТКАЗ: {f}")
    return 1 if FAILURES else 0


if __name__ == "__main__":
    sys.exit(main())
