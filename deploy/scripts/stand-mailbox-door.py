#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""ДВЕРЬ ЧТЕНИЯ ПИСЕМ СТЕНДА ПРОБ: форма приёмника собственного стенда службы
личности поверх приёмника писем стенда платформы.

ПРЕДМЕТ. Сквозные наборы службы личности (её репозиторий, `tests/newman`) и
посевы её «Дано» читают письма не HTTP-интерфейсом приёмника стенда платформы,
а двумя дверями приёмника своего стенда:

  · `GET /messages?to=<адрес>` — письма адресату в порядке приёма,
    `{"messages": [{"from", "to", "receivedAt", "data"}]}`, `data` — письмо
    целиком, как принято;
  · `GET /codes?to=<адрес>&after=<строка>` — по письму на элемент, в порядке
    приёма: первая непустая строка после строки, равной `after`, либо null —
    `{"codes": [...]}`. Строку-заголовок называет вызывающий: формы писем службы
    дверь не знает.

Без этой формы набор ключей доступа на стенде проб не получает ни одного кода
подтверждения, ни один человек набора не входит, и сотня с лишним шагов стоит
«условие не создано» (kaname#684). Дверь — та же форма, что у приёмника
собственного стенда службы; своего разбора писем она не заводит.

ЧЕГО НЕТ НАМЕРЕННО. Задержки приёма (`/hold`) нет: она — свойство почтового
узла, а не чтения, и приёмник стенда платформы её не умеет; кейс, которому она
нужна, на этом стенде остаётся «условие не создано», а не зелёным по пустому.

ОТКАЗ ПРИЁМНИКА — НЕ ПУСТОЙ ПЕРЕЧЕНЬ. Не ответил приёмник стенда — дверь
отвечает 502 с текстом, а не `{"messages": []}`: пустой перечень читается
набором как «письма ещё нет» и превращает обрыв проброса в ожидание до предела.

ЗАПУСК: `stand-mailbox-door.py <адрес приёмника> --port-file <файл>` — слушает
127.0.0.1 на свободном порту и пишет его в файл (порты стендов выделены по три
подряд, и соседний занят соседним стендом). Поднимает и снимает его проброс
стенда (`stand-ns.sh forward` / `unforward`).

САМОПРОВЕРКА: `--self-test` — 0 (доказано), 1 (провалено). Поднимает подставной
приёмник платформы и дверь над ним, судит обе формы, порядок, точное равенство
адресата, отказы 400/404 и 502 на оборванном приёмнике.
"""

import argparse
import json
import os
import sys
import tempfile
import threading
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PAGE = 200
TIMEOUT_S = 15


class UpstreamDown(Exception):
    pass


def code_after(data: str, heading: str):
    """Первая непустая строка после строки, равной `heading`; нет такой — None."""
    lines = data.replace("\r\n", "\n").split("\n")
    for i, line in enumerate(lines):
        if line.strip() == heading.strip():
            for nxt in lines[i + 1:]:
                if nxt.strip():
                    return nxt.strip()
            return None
    return None


class Upstream:
    """HTTP-интерфейс приёмника писем стенда платформы."""

    def __init__(self, base: str):
        self.base = base.rstrip("/")

    def _get(self, path: str) -> bytes:
        try:
            with urllib.request.urlopen(self.base + path, timeout=TIMEOUT_S) as r:
                return r.read()
        except (urllib.error.URLError, OSError, ValueError) as e:
            raise UpstreamDown(f"приёмник стенда не ответил: {type(e).__name__}") from None

    def letters(self, to: str) -> list:
        """Письма адресату `to` (точное равенство, без регистра) в порядке приёма."""
        want = to.strip().lower()
        found, start = [], 0
        while True:
            q = urllib.parse.urlencode({"query": f'to:"{want}"', "start": start, "limit": PAGE})
            try:
                doc = json.loads(self._get("/api/v1/search?" + q))
            except json.JSONDecodeError:
                raise UpstreamDown("приёмник стенда ответил не JSON") from None
            page = doc.get("messages")
            if not isinstance(page, list):
                raise UpstreamDown("приёмник стенда ответил без перечня messages")
            found.extend(page)
            start += len(page)
            total = doc.get("messages_count")
            if not page or not isinstance(total, int) or start >= total:
                break
        out = []
        # Поиск отдаёт новые первыми; порядок приёма — обратный, по времени создания.
        for m in sorted(found, key=lambda m: (m.get("Created") or "", m.get("ID") or "")):
            rcpts = [(x.get("Address") or "").strip().lower()
                     for key in ("To", "Cc", "Bcc") for x in (m.get(key) or [])]
            if want not in rcpts:
                continue
            raw = self._get(f"/api/v1/message/{urllib.parse.quote(m['ID'], safe='')}/raw")
            out.append({"from": (m.get("From") or {}).get("Address", ""), "to": rcpts,
                        "receivedAt": m.get("Created"), "data": raw.decode("utf-8", "replace")})
        return out


def make_handler(up: Upstream):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, fmt, *args):  # журнал двери не несёт адресов писем
            return

        def _send(self, code: int, doc: dict) -> None:
            body = json.dumps(doc).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self) -> None:  # noqa: N802 — имя задаёт http.server
            u = urllib.parse.urlsplit(self.path)
            q = urllib.parse.parse_qs(u.query)
            if u.path == "/healthz":
                self._send(200, {"status": "ok"})
                return
            if u.path not in ("/messages", "/codes"):
                self._send(404, {"message": "not found"})
                return
            to = q.get("to", [""])[0]
            if not to:
                self._send(400, {"message": "Illegal argument to: required"})
                return
            after = q.get("after", [""])[0]
            if u.path == "/codes" and not after:
                self._send(400, {"message": "Illegal argument after: required"})
                return
            try:
                letters = up.letters(to)
            except UpstreamDown as e:
                self._send(502, {"message": str(e)})
                return
            if u.path == "/messages":
                self._send(200, {"messages": letters})
            else:
                self._send(200, {"codes": [code_after(m["data"], after) for m in letters]})

    return Handler


def serve(upstream: str, port_file: str) -> None:
    srv = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(Upstream(upstream)))
    tmp = port_file + ".tmp"
    with open(tmp, "w") as f:
        f.write(str(srv.server_address[1]))
    os.replace(tmp, port_file)
    srv.serve_forever()


# ─────────────────────────── самопроверка ────────────────────────────────────


def self_test() -> int:
    heading = "Код подтверждения:"
    msgs = [  # как отдаёт поиск приёмника: новые первыми
        {"ID": "m3", "Created": "2026-01-01T00:00:03Z", "From": {"Address": "noreply@x.test"},
         "To": [{"Address": "a@x.test"}], "raw": "Subject: s\r\n\r\nтекст\r\n" + heading + "\r\n\r\n333333\r\n"},
        {"ID": "m2", "Created": "2026-01-01T00:00:02Z", "From": {"Address": "noreply@x.test"},
         "To": [{"Address": "aa@x.test"}], "raw": heading + "\n999999\n"},
        {"ID": "m1", "Created": "2026-01-01T00:00:01Z", "From": {"Address": "noreply@x.test"},
         "To": [{"Address": "A@x.test"}], "raw": "без кода\n"},
    ]
    alive = {"on": True}

    class Fake(BaseHTTPRequestHandler):
        def log_message(self, *a):
            return

        def do_GET(self):  # noqa: N802
            if not alive["on"]:
                self.send_response(500); self.end_headers(); return
            u = urllib.parse.urlsplit(self.path)
            q = urllib.parse.parse_qs(u.query)
            if u.path == "/api/v1/search":
                start, limit = int(q["start"][0]), int(q["limit"][0])
                term = q["query"][0].split('"')[1]
                hit = [m for m in msgs if any(term in t["Address"].lower() for t in m["To"])]
                page = [{k: v for k, v in m.items() if k != "raw"} for m in hit[start:start + limit]]
                body = json.dumps({"messages": page, "messages_count": len(hit)}).encode()
            elif u.path.startswith("/api/v1/message/") and u.path.endswith("/raw"):
                mid = urllib.parse.unquote(u.path.split("/")[4])
                body = next(m["raw"] for m in msgs if m["ID"] == mid).encode()
            else:
                self.send_response(404); self.end_headers(); return
            self.send_response(200); self.end_headers(); self.wfile.write(body)

    fake = ThreadingHTTPServer(("127.0.0.1", 0), Fake)
    threading.Thread(target=fake.serve_forever, daemon=True).start()
    global PAGE
    PAGE = 1  # перелистывание судится на странице в одно письмо
    door = ThreadingHTTPServer(("127.0.0.1", 0),
                               make_handler(Upstream(f"http://127.0.0.1:{fake.server_address[1]}")))
    threading.Thread(target=door.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{door.server_address[1]}"

    def get(path):
        try:
            with urllib.request.urlopen(base + path, timeout=10) as r:
                return r.status, json.loads(r.read())
        except urllib.error.HTTPError as e:
            return e.code, json.loads(e.read() or b"{}")

    ok = fail = 0

    def check(cond, what):
        nonlocal ok, fail
        if cond:
            ok += 1; print(f"  ok   {what}")
        else:
            fail += 1; print(f"  FAIL {what}")

    after = urllib.parse.quote(heading)
    st, d = get("/messages?to=a@x.test")
    check(st == 200 and [m["receivedAt"] for m in d["messages"]] == ["2026-01-01T00:00:01Z", "2026-01-01T00:00:03Z"],
          "/messages: письма адресату по порядку приёма, перелистыванием, без регистра")
    check(st == 200 and all(m["to"] != ["aa@x.test"] for m in d["messages"]),
          "/messages: адресат с тем же окончанием (aa@) не попадает — равенство, не подстрока")
    st, d = get(f"/codes?to=a@x.test&after={after}")
    check((st, d) == (200, {"codes": [None, "333333"]}), "/codes: по письму на элемент, null у письма без заголовка")
    st, d = get("/codes?to=a@x.test&after=" + urllib.parse.quote("Иной заголовок:"))
    check((st, d) == (200, {"codes": [None, None]}), "/codes: чужой заголовок — null, а не первая строка письма")
    st, _ = get("/codes?to=a@x.test")
    check(st == 400, "/codes без after — 400")
    st, _ = get("/messages")
    check(st == 400, "/messages без to — 400")
    st, _ = get("/hold")
    check(st == 404, "задержки приёма у двери нет — 404")
    st, _ = get("/healthz")
    check(st == 200, "/healthz — 200")
    alive["on"] = False
    st, d = get("/messages?to=a@x.test")
    check(st == 502 and "messages" not in d, "оборванный приёмник — 502, а не пустой перечень")
    alive["on"] = True

    with tempfile.TemporaryDirectory() as td:
        pf = os.path.join(td, "port")
        t = threading.Thread(target=serve, args=(f"http://127.0.0.1:{fake.server_address[1]}", pf), daemon=True)
        t.start()
        for _ in range(50):
            if os.path.exists(pf):
                break
            threading.Event().wait(0.1)
        port = open(pf).read().strip() if os.path.exists(pf) else ""
        code = 0
        if port.isdigit():
            with urllib.request.urlopen(f"http://127.0.0.1:{port}/healthz", timeout=5) as r:
                code = r.status
        check(code == 200, "serve: свободный порт записан в файл и слушается")

    door.shutdown(); fake.shutdown()
    print(f"самопроверка двери писем: прошло {ok}, провалено {fail}")
    return 0 if fail == 0 else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("upstream", nargs="?", help="адрес HTTP-интерфейса приёмника писем стенда")
    ap.add_argument("--port-file", help="куда записать порт двери")
    ap.add_argument("--self-test", action="store_true")
    a = ap.parse_args()
    if a.self_test:
        return self_test()
    if not a.upstream or not a.port_file:
        ap.error("нужны адрес приёмника и --port-file")
    serve(a.upstream, a.port_file)
    return 0


if __name__ == "__main__":
    sys.exit(main())
