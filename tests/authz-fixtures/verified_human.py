#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Человек посева наборов — тем путём, что проходит человек (приёмка F6b, Р17; kacho#2901).

ПРЕДМЕТ. Решение владельца 2026-09-27: дальше экранов регистрации и входа человек
проходит только с подтверждённым адресом почты. Человек, заведённый в обход
регистрации, подтвердить адрес не может никогда — такого вида у продукта нет, — а
пометка адреса подтверждённым иначе, чем кодом из письма, закрыла бы ровно то, что
судят наборы. Поэтому посев заводит каждого человека наборов так:

  1. заведение — `GET /iam/v1/auth/csrf?form=register`, затем
     `POST /iam/v1/auth/register` на ВНЕШНЕМ слушателе края, тем путём, что консоль;
     носитель сессии регистрации (B1) — у посева; момент ответа T0 печатается;
  2. письмо — у приёмника писем стенда читается письмо на этот адрес, принятое после
     T0, и код берётся из его тела; письмо ровно одно;
  3. подтверждение (только вид Ф-п) — `GET /iam/v1/auth/csrf?form=verify-email-confirm`
     и `POST /iam/v1/auth/verify-email/confirm` с `{"code","csrfToken"}` носителем B1;
     новый носитель B2 — из `Set-Cookie` ответа;
  4. утверждение — `GET /iam/v1/auth/me` носителем B2 (у Ф-н — B1): положение сессии
     и идентификатор человека — из этого ответа.

ПРАВИЛА, БЕЗ КОТОРЫХ ПУТЬ ПЕРЕСТАЁТ БЫТЬ ПУТЁМ ЧЕЛОВЕКА.

  · Письма посев не просит: `POST /iam/v1/auth/verify-email` в его переписи нет.
    Первое письмо ставит регистрация той же транзакцией (Р9 службы), и посев,
    просящий письмо, сделал бы зелёным продукт, в котором первое письмо не уходит.
  · Письма нет в срок — «условие не создано: письмо подтверждения не дошло до
    приёмника» с адресом и T0 (исход `Unmet`, код 75), и прогон относит наборы к «не
    выполнилось», а не к красным.
  · Отказ продукта там, где посев предъявил всё, чего требует контракт, — находка
    (`Finding`, код 1), а не «условие не создано».
  · Значения — с поверхностей человека: код из письма, положение и идентификаторы из
    ответов края. Ни одно не читается из хранилища или очереди службы.

Чтение приёмника — единственная поверхность посева, которой у человека нет: её место
у человека занимает его почтовый ящик. Она читает то, что служба уже сдала почтовому
узлу, и ничего не пишет службе.

Модуль не знает ни стенда, ни окружения: край и приёмник передаются объектами, часы и
ожидание — функциями. Поэтому самопроверка (`verified_human_test.py`) исполняет его
целиком против дублёров, без стенда.
"""
from __future__ import annotations

import http.cookies
import inspect
import json
import os
import re
import socket
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field

RC_FINDING = 1
RC_UNMET = 75

CSRF = "/iam/v1/auth/csrf"
REGISTER = "/iam/v1/auth/register"
VERIFY_REQUEST = "/iam/v1/auth/verify-email"
VERIFY_CONFIRM = "/iam/v1/auth/verify-email/confirm"
ME = "/iam/v1/auth/me"
ACCOUNTS = "/iam/v1/accounts"
PROJECTS = "/iam/v1/projects"
SESSION_COOKIE = "kaname_session"
FORM_COOKIE = "kaname_form"

# Виды человека фикстуры (Р17). Третьего нет.
KIND_VERIFIED = "Ф-п"
KIND_UNVERIFIED = "Ф-н"

# Срок ожидания письма. Письмо регистрации служба ставит той же транзакцией, что
# заводит человека, а сдаёт узлу дренаж очереди; величина — верхняя граница этого
# дренажа на стенде, а не выдумка о службе. Переопределяется окружением.
LETTER_BUDGET_S = float(os.environ.get("SEED_LETTER_BUDGET_S", "90"))
LETTER_POLL_S = 1.0
# Окно материализации владельческих прав: `Operation.done` означает долговечность,
# а не видимость (e2e-flow.md, «only-materialization-wait»). Это единственное
# ожидание, кроме письма.
MATERIALIZATION_BUDGET_S = float(os.environ.get("SEED_MATERIALIZATION_BUDGET_S", "40"))

# Код письма — две группы по пять знаков алфавита Крокфорда через дефис (форма для
# письма у службы: `RecoveryCodeValue.Letter`, код подтверждения — та же форма, Р7).
CODE_RE = re.compile(r"\b([0-9A-HJKMNP-TV-Z]{5})-([0-9A-HJKMNP-TV-Z]{5})\b")
CODE_LINE = "Код подтверждения:"

UNMET_LETTER = "условие не создано: письмо подтверждения не дошло до приёмника"
UNMET_MAILBOX = "условие не создано: приёмник писем не читается"


class Unmet(Exception):
    """Условие посева не создано — вердикта о продукте нет (код 75)."""


class Finding(Exception):
    """Продукт ответил не по контракту там, где посев предъявил требуемое (код 1)."""


def say(msg: str) -> None:
    print(f"[verified-human] {msg}", flush=True, file=sys.stderr)


def message_of(text: str) -> str:
    try:
        d = json.loads(text or "{}")
        return json.dumps({k: d.get(k) for k in ("code", "message") if k in d}, ensure_ascii=False)[:300]
    except (json.JSONDecodeError, AttributeError):
        return (text or "")[:300]


def cookie_value(set_cookies: list[str], name: str) -> str:
    for sc in set_cookies:
        c = http.cookies.SimpleCookie()
        try:
            c.load(sc)
        except http.cookies.CookieError:
            head = sc.split(";", 1)[0]
            if head.startswith(name + "="):
                return head[len(name) + 1:]
            continue
        if name in c:
            return c[name].value
    return ""


# ─────────────────────────── поверхности ─────────────────────────────────────


def internal_rest_tls_context() -> ssl.SSLContext:
    """TLS-контекст ВНУТРЕННЕГО REST-слушателя края (kacho#3131).

    Слушатель — mTLS и только он: сервер проверяется по УЦ установки
    (INTERNAL_REST_CA, сверка имени включена), клиент предъявляет лист
    операторской личности (INTERNAL_REST_CERT / INTERNAL_REST_KEY). Производитель
    переменных — deploy/scripts/lib/internal-rest-client.sh. Материала нет — это
    «условие не создано», а не повод идти открытым текстом.
    """
    ca, cert, key = (os.environ.get(k, "") for k in ("INTERNAL_REST_CA", "INTERNAL_REST_CERT", "INTERNAL_REST_KEY"))
    if not (ca and cert and key):
        raise Unmet("условие не создано: нет клиентского материала внутреннего слушателя края "
                    "(INTERNAL_REST_CA / INTERNAL_REST_CERT / INTERNAL_REST_KEY)")
    ctx = ssl.create_default_context(cafile=ca)
    ctx.load_cert_chain(cert, key)
    return ctx


class EdgeHttp:
    """Слушатель края. Ведёт перепись обращений: (метод, путь, код).

    tls — контекст TLS для слушателя под mTLS (внутренний: internal_rest_tls_context);
    None — умолчание urllib.
    """

    def __init__(self, base_url: str, timeout: float = 30.0, tls: ssl.SSLContext | None = None):
        self.base = base_url.rstrip("/")
        self.timeout = timeout
        self.tls = tls
        self.calls: list[tuple[str, str, int]] = []

    def ask(self, method: str, path: str, body: dict | None = None,
            cookies: dict | None = None, bearer: str = "") -> tuple[int, list[str], str]:
        hdrs = {"Accept": "application/json"}
        data = None
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            hdrs["Content-Type"] = "application/json"
        if cookies:
            hdrs["Cookie"] = "; ".join(f"{k}={v}" for k, v in cookies.items() if v)
        if bearer:
            hdrs["Authorization"] = f"Bearer {bearer}"
        req = urllib.request.Request(self.base + path, data=data, method=method, headers=hdrs)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout, context=self.tls) as r:  # nosec B310 — адрес стенда из окружения
                out = (r.status, r.headers.get_all("Set-Cookie") or [], r.read().decode("utf-8", "replace"))
        except urllib.error.HTTPError as e:
            out = (e.code, e.headers.get_all("Set-Cookie") or [], e.read().decode("utf-8", "replace"))
        except (urllib.error.URLError, OSError, socket.timeout) as e:
            raise Unmet(f"условие не создано: край по адресу {self.base} недостижим ({e})") from None
        self.calls.append((method, path.split("?", 1)[0], out[0]))
        return out


class Mailbox:
    """Поверхность чтения приёмника писем стенда (HTTP-слушатель приёмника).

    Читает принятое и ничего не пишет. Адрес — `MAILBOX_URL`, его открывает
    прогонщик пробросом к службе приёмника (deploy/scripts/newman-parallel.sh).
    """

    def __init__(self, base_url: str, timeout: float = 15.0):
        self.base = base_url.rstrip("/")
        self.timeout = timeout

    def _get(self, path: str) -> dict:
        req = urllib.request.Request(self.base + path, headers={"Accept": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=self.timeout, context=self.tls) as r:  # nosec B310 — адрес стенда из окружения
                raw = r.read().decode("utf-8", "replace")
                status = r.status
        except urllib.error.HTTPError as e:
            raise Unmet(f"{UNMET_MAILBOX}: {self.base}{path} ответил {e.code}") from None
        except (urllib.error.URLError, OSError, socket.timeout) as e:
            raise Unmet(f"{UNMET_MAILBOX}: {self.base} ({e})") from None
        try:
            d = json.loads(raw)
        except json.JSONDecodeError:
            raise Unmet(f"{UNMET_MAILBOX}: {self.base}{path} ответил {status} не-JSON") from None
        if not isinstance(d, dict):
            raise Unmet(f"{UNMET_MAILBOX}: {self.base}{path} ответил не объектом")
        return d

    def total(self) -> int:
        d = self._get("/api/v1/messages?limit=1")
        n = d.get("messages_count", d.get("total"))
        if not isinstance(n, int):
            raise Unmet(f"{UNMET_MAILBOX}: {self.base} — в ответе нет числа писем")
        return n

    def letters(self, address: str) -> list[dict]:
        """Письма на адрес, в порядке приёма: [{"id","created","text"}]."""
        q = urllib.parse.quote(f'to:"{address}"')
        d = self._get(f"/api/v1/search?query={q}&limit=50")
        out = []
        for m in d.get("messages") or []:
            to = [str((r or {}).get("Address", "")).lower() for r in (m.get("To") or [])]
            if address.lower() not in to:
                continue
            body = self._get(f"/api/v1/message/{urllib.parse.quote(str(m.get('ID', '')))}")
            out.append({"id": m.get("ID"), "created": m.get("Created", ""), "text": str(body.get("Text", ""))})
        out.sort(key=lambda x: x["created"])
        return out


def mailbox_precondition(mailbox) -> int:
    """F6b-53: поверхность чтения отвечает — печать адреса, числа писем и исхода."""
    n = mailbox.total()
    say(f"предусловие: поверхность чтения приёмника {mailbox.base} — писем прочитано {n}; условие создано")
    return n


def code_of(text: str) -> str:
    """Код из тела письма: первая форма кода ПОСЛЕ строки «Код подтверждения:»."""
    at = text.find(CODE_LINE)
    if at < 0:
        return ""
    m = CODE_RE.search(text, at + len(CODE_LINE))
    return (m.group(1) + m.group(2)) if m else ""


# ─────────────────────────── человек ─────────────────────────────────────────


@dataclass
class Human:
    email: str
    kind: str
    place: str
    t0: str = ""
    user_id: str = ""
    verified: bool | None = None
    bearer: str = ""
    account_id: str = ""
    project_id: str = ""
    slots: list[str] = field(default_factory=list)


def form_token(edge, form: str, cookies: dict | None = None) -> tuple[str, str]:
    status, sc, text = edge.ask("GET", f"{CSRF}?form={form}", cookies=cookies)
    if status != 200:
        raise Finding(f"признак формы {form!r}: ждали 200, получили {status} {message_of(text)}")
    try:
        token = json.loads(text or "{}").get("csrfToken")
    except json.JSONDecodeError:
        token = None
    ctx = cookie_value(sc, FORM_COOKIE)
    if not isinstance(token, str) or not token or not ctx:
        raise Finding(f"признак формы {form!r}: 200 без csrfToken строкой либо без печенья {FORM_COOKIE}")
    return token, ctx


def session_position(text: str, what: str) -> bool:
    try:
        view = json.loads(text or "{}").get("session")
    except (json.JSONDecodeError, AttributeError):
        view = None
    v = view.get("emailVerified") if isinstance(view, dict) else None
    if not isinstance(v, bool):
        raise Finding(f"{what}: ответ не называет положение сессии (session.emailVerified логическим значением нет)")
    return v


def register(edge, email: str, password: str, clock) -> tuple[str, str]:
    token, ctx = form_token(edge, "register")
    status, sc, text = edge.ask("POST", REGISTER, body={"email": email, "password": password, "csrfToken": token},
                                cookies={FORM_COOKIE: ctx})
    t0 = clock()
    if status != 200:
        raise Finding(f"регистрация {email}: на годный вход ждали 200, получили {status} {message_of(text)}")
    b1 = cookie_value(sc, SESSION_COOKIE)
    if not b1:
        raise Finding(f"регистрация {email}: 200 без носителя {SESSION_COOKIE} в Set-Cookie")
    if session_position(text, f"регистрация {email}") is not False:
        raise Finding(f"регистрация {email}: session.emailVerified не false — адрес подтверждён без кода")
    return b1, t0


def await_letter(mailbox, email: str, t0: str, sleep, budget: float = LETTER_BUDGET_S,
                 before: frozenset = frozenset()) -> dict:
    """Письмо, принятое ПОСЛЕ ответа регистрации: письма, лежавшие у приёмника до
    неё (`before`), в счёт не идут. Писем после — ровно одно."""
    deadline_left = budget
    while True:
        letters = [x for x in mailbox.letters(email) if x["id"] not in before]
        if letters:
            if len(letters) != 1:
                raise Finding(f"писем на {email} после регистрации {len(letters)}, ждали ровно одно")
            return letters[0]
        if deadline_left <= 0:
            raise Unmet(f"{UNMET_LETTER}: адрес {email}, T0 {t0}, ждали {budget:.0f} с")
        sleep(LETTER_POLL_S)
        deadline_left -= LETTER_POLL_S


def confirm(edge, email: str, b1: str, code: str) -> str:
    token, ctx = form_token(edge, "verify-email-confirm", {SESSION_COOKIE: b1})
    status, sc, text = edge.ask("POST", VERIFY_CONFIRM, body={"code": code, "csrfToken": token},
                                cookies={FORM_COOKIE: ctx, SESSION_COOKIE: b1})
    if status != 200:
        raise Finding(f"предъявление кода из письма {email}: ждали 200, получили {status} {message_of(text)} — "
                      "продукт отказал на коде из письма")
    if session_position(text, f"предъявление кода {email}") is not True:
        raise Finding(f"предъявление кода {email}: 200, а session.emailVerified не true")
    b2 = cookie_value(sc, SESSION_COOKIE)
    if not b2 or b2 == b1:
        raise Finding(f"предъявление кода {email}: 200 без НОВОГО носителя {SESSION_COOKIE} (Р10 службы)")
    return b2


def who_am_i(edge, email: str, bearer: str) -> tuple[str, bool]:
    status, _sc, text = edge.ask("GET", ME, cookies={SESSION_COOKIE: bearer})
    if status != 200:
        raise Finding(f"«кто я» {email}: ждали 200, получили {status} {message_of(text)}")
    try:
        user = json.loads(text or "{}").get("user") or {}
    except json.JSONDecodeError:
        user = {}
    uid = user.get("id") if isinstance(user, dict) else ""
    if not isinstance(uid, str) or not uid:
        raise Finding(f"«кто я» {email}: носитель сессии не назвал человека ({message_of(text)})")
    return uid, session_position(text, f"«кто я» {email}")


def enroll(edge, mailbox, email: str, password: str, kind: str, *, clock, sleep,
           place: str = "", letter_budget: float = LETTER_BUDGET_S) -> Human:
    """Шаги 1–4 Р17. Вид объявлен у места заведения; Ф-н — без шага 3."""
    if kind not in (KIND_VERIFIED, KIND_UNVERIFIED):
        raise ValueError(f"вид человека {kind!r} — видов два: {KIND_VERIFIED}, {KIND_UNVERIFIED}")
    h = Human(email=email, kind=kind, place=place or caller_place())
    before = frozenset(x["id"] for x in mailbox.letters(email))
    b1, h.t0 = register(edge, email, password, clock)
    say(f"{email}: зарегистрирован, T0 {h.t0}")
    letter = await_letter(mailbox, email, h.t0, sleep, letter_budget, before)
    code = code_of(letter["text"])
    if len(code) != 10:
        raise Finding(f"письмо на {email} принято приёмником, но кода подтверждения в его теле нет")
    bearer = b1
    if kind == KIND_VERIFIED:
        bearer = confirm(edge, email, b1, code)
    h.user_id, h.verified = who_am_i(edge, email, bearer)
    h.bearer = bearer
    return h


def caller_place() -> str:
    """Файл и строка места заведения — первый кадр вне этого модуля."""
    here = os.path.abspath(__file__)
    for fr in inspect.stack()[1:]:
        if os.path.abspath(fr.filename) != here:
            return f"{os.path.basename(fr.filename)}:{fr.lineno}"
    return "?"


def _list(edge, path: str, key: str, bearer_cookie: str = "", bearer: str = "") -> list[dict]:
    cookies = {SESSION_COOKIE: bearer_cookie} if bearer_cookie else None
    status, _sc, text = edge.ask("GET", path, cookies=cookies, bearer=bearer)
    if status != 200:
        raise Finding(f"GET {path.split('?', 1)[0]}: ждали 200, получили {status} {message_of(text)}")
    try:
        rows = json.loads(text or "{}").get(key) or []
    except json.JSONDecodeError:
        rows = []
    return [r for r in rows if isinstance(r, dict)]


def own_account_and_project(edge, h: Human, sleep, *, admin_bearer: str = "",
                            budget: float = MATERIALIZATION_BUDGET_S) -> tuple[str, str]:
    """Аккаунт человека — запись `GET /iam/v1/accounts`, чей `ownerUserId` равен
    `user.id`; проект — `GET /iam/v1/projects` с `accountId` и `filter` по имени
    `default`. У Ф-п спрашивает его носитель; у Ф-н — администратор стенда: носитель
    Ф-н получает отказ положения (Р17)."""
    cookie = h.bearer if h.kind == KIND_VERIFIED else ""
    bearer = "" if cookie else admin_bearer
    left = budget
    while True:
        acct = next((a.get("id", "") for a in _list(edge, f"{ACCOUNTS}?pageSize=1000", "accounts", cookie, bearer)
                     if a.get("ownerUserId") == h.user_id), "")
        if acct:
            q = urllib.parse.urlencode({"accountId": acct, "filter": 'name="default"'})
            proj = next((p.get("id", "") for p in _list(edge, f"{PROJECTS}?{q}", "projects", cookie, bearer)
                         if p.get("name") == "default"), "")
            if proj:
                return acct, proj
        if left <= 0:
            raise Finding(f"{h.email}: за {budget:.0f} с край не назвал аккаунт человека {h.user_id} "
                          "с проектом default — регистрация объявила успех, а собственности нет")
        sleep(1.0)
        left -= 1.0


# ─────────────────────────── перепись людей (F6b-51) ──────────────────────────


def census(humans: list[Human], fixtures: dict) -> list[str]:
    """Печать переписи и находки. Слоты — ключи окружения, несущие идентификатор
    либо адрес человека. Находка — Ф-п с `emailVerified` не true, Ф-н с не false,
    и пустая перепись."""
    for h in humans:
        h.slots = sorted(k for k, v in fixtures.items() if isinstance(v, str) and v and v in (h.user_id, h.email))
    m = sum(1 for h in humans if h.kind == KIND_VERIFIED)
    u = sum(1 for h in humans if h.kind == KIND_UNVERIFIED)
    say(f"перепись людей посева: N={len(humans)} · {KIND_VERIFIED} M={m} · {KIND_UNVERIFIED} U={u}")
    findings = []
    if not humans:
        findings.append("перепись людей посева пуста: N=0 — посев не завёл ни одного человека")
    for h in humans:
        say(f"  {h.email} · {h.kind} · emailVerified={h.verified} · {h.place} · слоты {','.join(h.slots) or '—'}")
        want = h.kind == KIND_VERIFIED
        if h.verified is not want:
            findings.append(f"{h.email}: объявлен {h.kind}, emailVerified: {str(h.verified).lower()}")
    if len(humans) != m + u:
        findings.append(f"перепись: N={len(humans)} не равно M+U={m + u}")
    return findings


# ─────────────────────────── самопроверка (F6b-50, F6b-47, F6b-51, F6b-53) ────
#
# Без стенда: край и приёмник — настоящие HTTP-слушатели на 127.0.0.1, и посев
# ходит к ним тем же транспортом (`EdgeHttp`, `Mailbox`), что к стенду. Каждая
# инъекция меняет ОДИН факт против законного близнеца (а).
#
# ГРАНИЦА: форма ответов приёмника здесь — та, что описана у `Mailbox`; что
# приёмник стенда отвечает именно ею, самопроверка не доказывает — это держит
# прогон посева на стенде (F6b-53, предусловие печатает число прочитанных писем).

LETTER_CODE = "7K3QZ-M4XRT"


def _letter_text(code: str) -> str:
    return ("Подтвердите адрес почты, чтобы продолжить работу.\r\n\r\n"
            f"{CODE_LINE}\r\n\r\n    {code}\r\n\r\n"
            "Код действует 30 мин. с момента отправки и применяется один раз.\r\n")


class _Doubles:
    """Край и приёмник по случаю: письмо после регистрации есть/нет; ответ на код."""

    def __init__(self, letters_on_register: int = 1, confirm: str = "ok", letter_text: str = ""):
        import threading
        from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

        self.letters: list[dict] = []
        self.calls: list[tuple[str, str]] = []
        self.presented: list[str] = []
        self.verified = False
        self.letters_on_register = letters_on_register
        self.confirm_answer = confirm
        self.letter_text = letter_text or _letter_text(LETTER_CODE)
        dbl = self

        class Edge(BaseHTTPRequestHandler):
            def log_message(self, *a):  # noqa: D401 — тишина
                return

            def _send(self, code, body, cookies=()):
                raw = json.dumps(body).encode()
                self.send_response(code)
                self.send_header("Content-Type", "application/json")
                for c in cookies:
                    self.send_header("Set-Cookie", c)
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

            def _cookies(self):
                c = http.cookies.SimpleCookie()
                c.load(self.headers.get("Cookie", ""))
                return {k: v.value for k, v in c.items()}

            def do_GET(self):  # noqa: N802
                path, _, query = self.path.partition("?")
                dbl.calls.append(("GET", path))
                ck = self._cookies()
                if path == CSRF:
                    form = urllib.parse.parse_qs(query).get("form", [""])[0]
                    return self._send(200, {"csrfToken": f"tok-{form}"},
                                      [f"{FORM_COOKIE}=ctx-{form}; Path=/; HttpOnly; Secure; SameSite=Strict"])
                if path == ME:
                    b = ck.get(SESSION_COOKIE, "")
                    if b not in ("B1", "B2"):
                        return self._send(200, {"user": None})
                    return self._send(200, {"user": {"id": "usr0000000000000seed1"},
                                            "session": {"emailVerified": b == "B2" and dbl.verified}})
                if path == ACCOUNTS:
                    return self._send(200, {"accounts": [{"id": "acc-other", "ownerUserId": "usr-other"},
                                                         {"id": "acc-seed1", "ownerUserId": "usr0000000000000seed1"}]})
                if path == PROJECTS:
                    q = urllib.parse.parse_qs(query)
                    ok = q.get("accountId") == ["acc-seed1"] and q.get("filter") == ['name="default"']
                    return self._send(200, {"projects": [{"id": "prj-seed1", "name": "default"}] if ok else []})
                return self._send(404, {"code": 5, "message": "not found"})

            def do_POST(self):  # noqa: N802
                n = int(self.headers.get("Content-Length", "0"))
                body = json.loads(self.rfile.read(n) or b"{}")
                dbl.calls.append(("POST", self.path))
                ck = self._cookies()
                if self.path == REGISTER:
                    if ck.get(FORM_COOKIE) != "ctx-register" or body.get("csrfToken") != "tok-register":
                        return self._send(403, {"code": 7, "message": "form token rejected"})
                    for i in range(dbl.letters_on_register):
                        dbl.letters.append({"ID": f"m{len(dbl.letters) + 1}", "To": [{"Address": body["email"]}],
                                            "Created": f"2026-09-28T10:00:0{i}Z", "Text": dbl.letter_text})
                    return self._send(200, {"session": {"emailVerified": False, "assuranceLevel": "aal1"}},
                                      [f"{SESSION_COOKIE}=B1; Path=/; HttpOnly; Secure"])
                if self.path == VERIFY_CONFIRM:
                    if ck.get(FORM_COOKIE) != "ctx-verify-email-confirm" or ck.get(SESSION_COOKIE) != "B1":
                        return self._send(403, {"code": 7, "message": "form token rejected"})
                    dbl.presented.append(str(body.get("code", "")))
                    good = str(body.get("code", "")).replace("-", "").upper() == LETTER_CODE.replace("-", "")
                    if dbl.confirm_answer == "ok" and good:
                        dbl.verified = True
                        return self._send(200, {"session": {"emailVerified": True}},
                                          [f"{SESSION_COOKIE}=B2; Path=/; HttpOnly; Secure"])
                    return self._send(401, {"code": 16, "message": "authentication failed", "details": []})
                return self._send(404, {"code": 5, "message": "not found"})

        class Mail(BaseHTTPRequestHandler):
            def log_message(self, *a):  # noqa: D401
                return

            def _send(self, body):
                raw = json.dumps(body).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

            def do_GET(self):  # noqa: N802
                path, _, query = self.path.partition("?")
                if path == "/api/v1/messages":
                    return self._send({"total": len(dbl.letters), "messages_count": len(dbl.letters),
                                       "messages": dbl.letters[:1]})
                if path == "/api/v1/search":
                    q = urllib.parse.parse_qs(query).get("query", [""])[0]
                    want = q.removeprefix("to:").strip('"').lower()
                    hits = [m for m in dbl.letters if any(r["Address"].lower() == want for r in m["To"])]
                    return self._send({"messages": hits, "messages_count": len(hits)})
                if path.startswith("/api/v1/message/"):
                    mid = path.rsplit("/", 1)[1]
                    m = next((x for x in dbl.letters if x["ID"] == mid), None)
                    return self._send({"ID": mid, "Text": m["Text"] if m else ""})
                self.send_response(404)
                self.end_headers()

        self._servers = []
        for handler in (Edge, Mail):
            srv = ThreadingHTTPServer(("127.0.0.1", 0), handler)
            threading.Thread(target=srv.serve_forever, daemon=True).start()
            self._servers.append(srv)
        self.edge_url = f"http://127.0.0.1:{self._servers[0].server_address[1]}"
        self.mail_url = f"http://127.0.0.1:{self._servers[1].server_address[1]}"

    def close(self):
        for s in self._servers:
            s.shutdown()
            s.server_close()


def _free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def _self_test() -> int:
    checks = [0]
    failures: list[str] = []

    def check(name: str, ok: bool, detail: str = "") -> None:
        checks[0] += 1
        print(f"  {'ok ' if ok else 'ОТКАЗ'} {name}" + (f" — {detail}" if detail and not ok else ""))
        if not ok:
            failures.append(f"{name}: {detail}")

    clock = lambda: "2026-09-28T10:00:00Z"  # noqa: E731 — управляемые часы
    slept = []
    sleep = slept.append
    addr = "prodseed-selftest@example.com"

    def run(dbl, kind=KIND_VERIFIED, budget=3.0):
        edge = EdgeHttp(dbl.edge_url)
        try:
            h = enroll(edge, Mailbox(dbl.mail_url), addr, "Seed-Selftest-2026!x", kind,
                       clock=clock, sleep=sleep, place="selftest:1", letter_budget=budget)
            return h, None, edge
        except (Unmet, Finding) as e:
            return None, e, edge

    posts = lambda dbl, p: sum(1 for c in dbl.calls if c == ("POST", p))  # noqa: E731

    # (а) законный близнец: письмо после регистрации есть — Ф-п подтверждён кодом письма.
    dbl = _Doubles()
    h, err, edge = run(dbl)
    check("F6b-50 (а): посев завершается без исхода-исключения", err is None, repr(err))
    check("F6b-50 (а): предъявление кода ровно одно, код — из письма приёмника",
          dbl.presented == [LETTER_CODE.replace("-", "")], f"предъявлено {dbl.presented}")
    check("F6b-46: запроса письма в переписи нет", posts(dbl, VERIFY_REQUEST) == 0, f"{dbl.calls}")
    check("F6b-46: перепись — регистрация, подтверждение, «кто я» по порядку",
          [c for c in dbl.calls if c[1] in (REGISTER, VERIFY_CONFIRM, ME)]
          == [("POST", REGISTER), ("POST", VERIFY_CONFIRM), ("GET", ME)], f"{dbl.calls}")
    check("F6b-46: «кто я» новым носителем — emailVerified true, идентификатор из ответа",
          bool(h) and h.verified is True and h.user_id == "usr0000000000000seed1" and h.bearer == "B2",
          f"{h}")
    if h:
        acct, proj = own_account_and_project(edge, h, sleep)
        check("F6b-46: аккаунт — запись с ownerUserId человека, проект — default этого аккаунта",
              (acct, proj) == ("acc-seed1", "prj-seed1"), f"{acct} {proj}")
    dbl.close()

    # (б) письма нет до конца ожидания — «условие не создано», письма посев не просит.
    dbl = _Doubles(letters_on_register=0)
    h, err, _ = run(dbl)
    check("F6b-50 (б): исход — условие не создано, а не находка", isinstance(err, Unmet), repr(err))
    check("F6b-50 (б): текст называет условие, адрес и T0",
          isinstance(err, Unmet) and UNMET_LETTER in str(err) and addr in str(err) and clock() in str(err),
          str(err))
    check("F6b-50 (б): край не получил ни запроса письма, ни предъявления кода",
          posts(dbl, VERIFY_REQUEST) == 0 and posts(dbl, VERIFY_CONFIRM) == 0, f"{dbl.calls}")
    dbl.close()

    # (в) письмо есть, край отвечает на код 401 — находка о продукте, не «условие не создано».
    dbl = _Doubles(confirm="401")
    h, err, _ = run(dbl)
    check("F6b-50 (в): исход — находка", isinstance(err, Finding), repr(err))
    check("F6b-50 (в): находка называет адрес и ответ края",
          isinstance(err, Finding) and addr in str(err) and "401" in str(err), str(err))
    dbl.close()

    # F6b-47: Ф-н — те же шаги без предъявления кода.
    dbl = _Doubles()
    h, err, _ = run(dbl, kind=KIND_UNVERIFIED)
    check("F6b-47: Ф-н заведён, «кто я» носителем регистрации — emailVerified false",
          err is None and h.verified is False and h.bearer == "B1", f"{err!r} {h}")
    check("F6b-47: у Ф-н предъявления кода нет, письмо у приёмника ровно одно",
          posts(dbl, VERIFY_CONFIRM) == 0 and len(dbl.letters) == 1, f"{dbl.calls}")
    dbl.close()

    # Два письма после регистрации — находка (ждали ровно одно).
    dbl = _Doubles(letters_on_register=2)
    _, err, _ = run(dbl)
    check("F6b-46: писем после регистрации два — находка", isinstance(err, Finding) and "ровно одно" in str(err),
          repr(err))
    dbl.close()

    # Письмо без строки кода — находка, а не «не дошло».
    dbl = _Doubles(letter_text="Подтвердите адрес почты.\r\n")
    _, err, _ = run(dbl)
    check("F6b-46: письмо без кода — находка", isinstance(err, Finding) and "кода" in str(err), repr(err))
    dbl.close()

    # F6b-51: перепись людей и её близнец — Ф-п без шага подтверждения.
    good = Human(email="a@example.com", kind=KIND_VERIFIED, place="selftest:1", user_id="usrA", verified=True)
    unv = Human(email="b@example.com", kind=KIND_UNVERIFIED, place="selftest:2", user_id="usrB", verified=False)
    f = census([good, unv], {"userAAAId": "usrA", "userBId": "usrB", "clusterTargetEmail": "a@example.com"})
    check("F6b-51: перепись согласных видов — находок нет", f == [], f"{f}")
    check("F6b-51: слоты окружения названы у человека", good.slots == ["clusterTargetEmail", "userAAAId"],
          f"{good.slots}")
    bad = Human(email="c@example.com", kind=KIND_VERIFIED, place="selftest:3", user_id="usrC", verified=False)
    f = census([good, bad], {})
    check("F6b-51 близнец: Ф-п с emailVerified false — находка с адресом",
          any("c@example.com" in x and "объявлен Ф-п" in x for x in f), f"{f}")
    check("F6b-51: пустая перепись — находка, а не чистота", census([], {}) != [], "")

    # F6b-53: поверхность чтения приёмника и её близнец — адрес без слушателя.
    dbl = _Doubles()
    try:
        n = mailbox_precondition(Mailbox(dbl.mail_url))
        check("F6b-53: предусловие печатает число прочитанных писем", n == 0, f"{n}")
    except Unmet as e:
        check("F6b-53: предусловие на отвечающей поверхности создаётся", False, str(e))
    dbl.close()
    dead = f"http://127.0.0.1:{_free_port()}"
    try:
        mailbox_precondition(Mailbox(dead, timeout=2))
        check("F6b-53 близнец: адрес без слушателя — условие не создано", False, "предусловие прошло")
    except Unmet as e:
        check("F6b-53 близнец: адрес без слушателя — условие не создано с адресом",
              UNMET_MAILBOX in str(e) and dead in str(e), str(e))

    print(f"\nперепись: проверок исполнено {checks[0]}, отказов {len(failures)}")
    if not checks[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    return 1 if failures else 0


if __name__ == "__main__":
    import argparse

    ap = argparse.ArgumentParser(description="человек посева наборов по Р17 приёмки F6b")
    ap.add_argument("--self-test", action="store_true",
                    help="исполнить исходы посева против дублёров края и приёмника; стенда не трогает")
    args = ap.parse_args()
    if args.self_test:
        sys.exit(_self_test())
    ap.print_help()
    sys.exit(2)
