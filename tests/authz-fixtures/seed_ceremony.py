#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""ПОСЕВ ГЕЙТА S3: один человек предъявлен краю тремя классами носителей (kacho#3159).

ПРЕДМЕТ. Приёмка интерактивного входа (`docs/specs/sub-phase-IAM-INT-1-interactive-
login-capability-acceptance.md` воркспейса, §3, входной гейт стадии S3) разрешает
писать сценарии IAM-INT-1-24 и IAM-INT-1-25, только когда посев предъявит ОДНОГО
человека краю всеми тремя классами носителей — сессией браузера, подписанным
предъявителем и базовым удостоверением, выданным ему самому, — и покажет, что все
три доходят до принципала. Один человек делает класс носителя единственным
различающимся фактом пары.

Образец — посев церемонии службы доступа (`PRO-Robotech/kaname:tests/authz-fixtures/
seed_ceremony.py`); отличие одно: там церемония идёт к фронтам службы, здесь — к
КРАЮ платформы, и клиента церемонии посев заводит на ВНУТРЕННЕМ слушателе края
(`{{internalBaseUrl}}`, mTLS листом оператора), потому что глаголы
`InternalInteractiveClientService` край фронтирует, и другой двери у них нет.

ШАГИ — ГЛАГОЛАМИ ПРОДУКТА, И КАЖДЫЙ УТВЕРЖДАЕТ СВОЙ ИСХОД:

  1. конфиденциальный интерактивный клиент — `POST /iam/v1/internal/interactiveClients`
     на внутреннем слушателе под бутстрап-удостоверением (`system_admin` кластера):
     операция `done` без ошибки, способ `client_secret_basic`, непустой секрет;
  2. человек — регистрация на внешнем слушателе, код из письма приёмника стенда,
     подтверждение (`verified_human.enroll`); носитель — сессия уровня «1»;
  3. базовое удостоверение ЕМУ — `POST /iam/v1/users/{id}/tokens` видом
     `CREDENTIAL_KIND_SECRET` (выпуск гейтится `token_issuer`; выпускает служебная
     учётка надзора облака, законный второй держатель); операция `done`, секрет —
     одной строкой; подделка той же марки — последний знак контрольной суммы;
  4. подписанный предъявитель уровня «1» — `GET /iam/v1/authorize` (PKCE S256) с
     печеньем сессии → `302` на адрес возврата с ровно одним кодом и `state`
     дословно → `POST /iam/v1/token` секретом клиента схемой Basic; уровень и
     субъект токена — по его составу;
  5. ВСЕ ТРИ носителя доходят до принципала — `GET /iam/v1/me` каждым: субъект —
     `user:<тот же человек>`;
  6. второй фактор — заведение и подтверждение кодом по времени (RFC 6238):
     сессия перевыпускается уровнем «2», церемония под ней даёт предъявителя
     уровня «2»; оба доходят до того же принципала;
  7. носители уровня «1», выданные ДО заведения фактора, после него живы и по-прежнему
     уровня «1» — иначе сценарий 24 судил бы мёртвого носителя.

ЧТО ПИШЕТСЯ — ключи `MINTED_KEYS` в окружение набора края (`gateway/tests/newman`).
Сценарии 24 и 25 читают их и только их.

ИСХОДЫ — ТРИ, РАЗЛИЧАЮТСЯ КОДОМ:
    0  — гейт пройден: три класса уровня «1» и два уровня «2» доведены до принципала;
    1  — НАХОДКА: продукт ответил не по контракту там, где посев предъявил всё;
   75  — УСЛОВИЕ НЕ СОЗДАНО: поверхность недостижима, материала внутреннего
         слушателя нет, письма нет в срок. Вердикта о дереве нет.

СЕКРЕТЫ НЕ ПЕЧАТАЮТСЯ — ни пароль, ни секрет клиента, ни токены, ни базовый секрет:
журнал прогона публичного репозитория читает кто угодно.

СНЯТИЕ. Клиент церемонии — единственное, что посев заводит вне человека; при отказе
любого следующего шага посев его снимает (`Delete` тем же слушателем). Остальное
созданное (человек и его аккаунт) живёт в пространстве стенда проб и снимается
вместе с ним (`testing.md#fixture-cleanup-mandatory`).

САМОПРОВЕРКА — `--self-test`: подставной край по каждой оси, где инъекция меняет
ОДИН факт против законного мира; объявленные ключи сходятся с записью в обе стороны.
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
import os
import pathlib
import secrets
import socket
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import verified_human as vh  # noqa: E402

RC_FINDING = vh.RC_FINDING
RC_UNMET = vh.RC_UNMET

IC_PATH = "/iam/v1/internal/interactiveClients"
AUTHORIZE = "/iam/v1/authorize"
TOKEN = "/iam/v1/token"
WHO_AM_I = "/iam/v1/me"
SECOND_FACTOR_FORM = "second-factor"
SF_ENROLL = "/iam/v1/auth/second-factor/enroll"
SF_CONFIRM = "/iam/v1/auth/second-factor/confirm"
REDIRECT = "https://api.kacho.local/auth/callback"
SECRET_METHOD = "client_secret_basic"
TOTP_STEP_S = 30
TOTP_DIGITS = 6
# Срок базового удостоверения: дольше любого прогона набора и в пределах потолка
# политики (секрет сверх потолка отвергается, а не урезается молча).
BASIC_TTL_S = 86400

# Ключи окружения набора края, которые этот посев пишет.
MINTED_KEYS = (
    "s3UserId", "s3Email", "s3AccountId",
    "s3SessionL1", "s3BearerL1", "s3Basic", "s3BasicTampered",
    "s3SessionL2", "s3BearerL2",
)
SECRET_KEYS = ("s3SessionL1", "s3BearerL1", "s3Basic", "s3BasicTampered", "s3SessionL2", "s3BearerL2")

Finding = vh.Finding
Unmet = vh.Unmet


def say(msg: str) -> None:
    print(f"[seed-ceremony] {msg}", file=sys.stderr, flush=True)


def b64u(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **kw):  # noqa: D401 — перенаправление — ответ, а не шаг
        return None


class Http:
    """Запрос без следования перенаправлению: `302` точки авторизации — ИСХОД шага."""

    def __init__(self, timeout: float = 30.0):
        self.timeout = timeout

    def __call__(self, method: str, url: str, *, headers: dict | None = None,
                 json_body: dict | None = None, form: dict | None = None,
                 tls: ssl.SSLContext | None = None) -> tuple[int, dict, str]:
        hdrs = {"Accept": "application/json", **(headers or {})}
        data = None
        if json_body is not None:
            data = json.dumps(json_body).encode()
            hdrs["Content-Type"] = "application/json"
        elif form is not None:
            data = urllib.parse.urlencode(form).encode()
            hdrs["Content-Type"] = "application/x-www-form-urlencoded"
        req = urllib.request.Request(url, data=data, method=method, headers=hdrs)
        handlers = [_NoRedirect()]
        if tls is not None:
            handlers.append(urllib.request.HTTPSHandler(context=tls))
        opener = urllib.request.build_opener(*handlers)
        try:
            with opener.open(req, timeout=self.timeout) as r:  # nosec B310 — адрес стенда из окружения
                return r.status, {k.lower(): v for k, v in r.headers.items()}, r.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            return e.code, {k.lower(): v for k, v in e.headers.items()}, e.read().decode("utf-8", "replace")
        except (urllib.error.URLError, OSError, socket.timeout) as e:
            raise Unmet(f"условие не создано: {url.split('?', 1)[0]} недостижим ({e})") from None


def _json(text: str) -> dict:
    try:
        d = json.loads(text or "{}")
    except json.JSONDecodeError:
        return {}
    return d if isinstance(d, dict) else {}


def totp(secret_b32: str, step: int) -> str:
    """Код по времени RFC 6238: HMAC-SHA1, шесть цифр, шаг 30 с."""
    key = base64.b32decode(secret_b32 + "=" * (-len(secret_b32) % 8), casefold=True)
    mac = hmac.new(key, step.to_bytes(8, "big"), hashlib.sha1).digest()
    off = mac[-1] & 0x0F
    value = (int.from_bytes(mac[off:off + 4], "big") & 0x7FFFFFFF) % 10 ** TOTP_DIGITS
    return str(value).zfill(TOTP_DIGITS)


def token_claims(token: str) -> dict:
    """Состав токена без проверки подписи: подпись проверяет край, которому токен
    предъявляется (шаг 5), здесь читается только назначенное выдачей."""
    parts = token.split(".")
    if len(parts) != 3:
        raise Finding("выданный токен — не JWT из трёх частей: уровень и субъект прочесть не из чего")
    try:
        claims = json.loads(base64.urlsafe_b64decode(parts[1] + "=" * (-len(parts[1]) % 4)))
    except (ValueError, json.JSONDecodeError):
        raise Finding("состав выданного токена не разбирается") from None
    if not isinstance(claims, dict):
        raise Finding("состав выданного токена — не объект")
    return claims


def tamper(secret: str) -> str:
    """Подделка ТОЙ ЖЕ марки: портится последний знак (из контрольной суммы, она
    покрывает и идентификатор, и секретную часть) — на заведомо другой знак."""
    last = secret[-1:]
    return secret[:-1] + ("1" if last == "0" else "0")


class Stand:
    """Адреса и транспорт: внешний слушатель края, внутренний (mTLS), бутстрап."""

    def __init__(self, public: str, internal: str, boot: str, http=None, internal_tls=None):
        self.public = public.rstrip("/")
        self.internal = internal.rstrip("/")
        self.boot = boot
        self.http = http or Http()
        self.internal_tls = internal_tls

    def ask_internal(self, method: str, path: str, body: dict | None = None):
        return self.http(method, self.internal + path, headers={"Authorization": "Bearer " + self.boot},
                         json_body=body, tls=self.internal_tls)

    def ask_public(self, method: str, path: str, *, bearer: str = "", session: str = "",
                   body: dict | None = None, form: dict | None = None, headers: dict | None = None):
        h = dict(headers or {})
        if bearer:
            h["Authorization"] = "Bearer " + bearer
        if session:
            h["Cookie"] = f"{vh.SESSION_COOKIE}={session}"
        return self.http(method, self.public + path, headers=h, json_body=body, form=form)


def create_client(stand: Stand, name: str) -> tuple[str, str, str]:
    """Шаг 1 → (id ресурса, clientId, секрет)."""
    code, _, text = stand.ask_internal("POST", IC_PATH, {"name": name, "redirectUris": [REDIRECT]})
    j = _json(text)
    if code != 200 or not j.get("done") or j.get("error"):
        raise Finding(f"заведение клиента церемонии: ждали 200 и операцию done без ошибки, получили "
                      f"{code} {vh.message_of(text)} error={j.get('error')}")
    resp = j.get("response") or {}
    client = resp.get("interactiveClient") or {}
    rid = (j.get("metadata") or {}).get("interactiveClientId") or client.get("id") or ""
    cid, csec = client.get("clientId") or "", resp.get("clientSecret") or ""
    if client.get("tokenEndpointAuthMethod") != SECRET_METHOD or not cid or not csec or not rid:
        raise Finding(f"клиент церемонии заведён не конфиденциальным либо без clientId/секрета/id "
                      f"(способ {client.get('tokenEndpointAuthMethod')!r})")
    return rid, cid, csec


def delete_client(stand: Stand, rid: str) -> int:
    code, _, _ = stand.ask_internal("DELETE", f"{IC_PATH}/{urllib.parse.quote(rid)}")
    return code


def issue_basic(stand: Stand, user: str, suffix: str) -> str:
    """Шаг 3 → базовый секрет человека одной строкой."""
    code, _, text = stand.ask_public("POST", f"/iam/v1/users/{urllib.parse.quote(user)}/tokens", bearer=stand.boot,
                                     body={"userId": user, "name": f"s3-gate-{suffix}",
                                           "description": "носитель класса «базовое удостоверение», гейт S3",
                                           "credentialKind": "CREDENTIAL_KIND_SECRET",
                                           "ttlSeconds": BASIC_TTL_S})
    j = _json(text)
    if code != 200 or not j.get("done") or j.get("error"):
        raise Finding(f"выпуск базового удостоверения человеку: ждали 200 и операцию done без ошибки, "
                      f"получили {code} {vh.message_of(text)} error={j.get('error')}")
    secret = (j.get("response") or {}).get("secret") or ""
    if not isinstance(secret, str) or not secret:
        raise Finding("выпуск базового удостоверения ответил done без секрета")
    return secret


def authorize(stand: Stand, client_id: str, session: str) -> tuple[str, str]:
    verifier = b64u(secrets.token_bytes(32))
    state = b64u(secrets.token_bytes(32))
    challenge = b64u(hashlib.sha256(verifier.encode("ascii")).digest())
    q = urllib.parse.urlencode({"response_type": "code", "client_id": client_id, "redirect_uri": REDIRECT,
                                "scope": "openid", "state": state, "code_challenge": challenge,
                                "code_challenge_method": "S256"})
    code, hdrs, text = stand.ask_public("GET", f"{AUTHORIZE}?{q}", session=session)
    loc = hdrs.get("location", "")
    if code != 302 or not loc:
        raise Finding(f"авторизация: ждали 302 с Location, получили {code} {vh.message_of(text)}")
    base, _, query = loc.partition("?")
    got = urllib.parse.parse_qs(query.split("#")[0])
    if base != REDIRECT or got.get("state") != [state] or len(got.get("code", [])) != 1:
        raise Finding(f"авторизация: перенаправление не на адрес возврата, без ровно одного кода либо без "
                      f"state дословно (error={got.get('error')})")
    return got["code"][0], verifier


def exchange(stand: Stand, client_id: str, secret: str, code: str, verifier: str) -> str:
    basic = base64.b64encode((urllib.parse.quote(client_id, safe="") + ":"
                              + urllib.parse.quote(secret, safe="")).encode()).decode()
    status, _, text = stand.ask_public("POST", TOKEN, headers={"Authorization": "Basic " + basic},
                                       form={"grant_type": "authorization_code", "code": code,
                                             "redirect_uri": REDIRECT, "code_verifier": verifier})
    token = _json(text).get("access_token") or ""
    if status != 200 or not token:
        raise Finding(f"обмен кода: ждали 200 с access_token, получили {status} {vh.message_of(text)}")
    return token


def ceremony_bearer(stand: Stand, client: tuple[str, str], session: str, user: str, level: str) -> str:
    """Шаг 4 (и его повтор под сессией «2»): уровень и субъект — по составу токена."""
    code, verifier = authorize(stand, client[0], session)
    token = exchange(stand, client[0], client[1], code, verifier)
    claims = token_claims(token)
    if str(claims.get("acr")) != level:
        raise Finding(f"церемония под сессией уровня «{level}» выдала токен уровня {claims.get('acr')!r}")
    if claims.get("sub") != user:
        raise Finding("церемония выдала токен не тому человеку, что вошёл")
    return token


def reaches(stand: Stand, what: str, user: str, *, bearer: str = "", session: str = "") -> None:
    """Шаг 5: носитель доходит до принципала — субъект `user:<тот же человек>`."""
    code, _, text = stand.ask_public("GET", WHO_AM_I, bearer=bearer, session=session)
    me = _json(text)
    if code != 200:
        raise Finding(f"{what}: «кто я» на крае — ждали 200, получили {code} {vh.message_of(text)}")
    if me.get("subject") != "user:" + user or me.get("userId") != user:
        raise Finding(f"{what}: край принял носителя, а субъект — не этот человек ({me.get('subject')!r})")


def session_level(stand: Stand, session: str, what: str) -> str:
    code, _, text = stand.ask_public("GET", vh.ME, session=session)
    if code != 200:
        raise Finding(f"{what}: уровень сессии — ждали 200 на {vh.ME}, получили {code} {vh.message_of(text)}")
    return str((_json(text).get("session") or {}).get("assuranceLevel"))


def level2_session(lane, session: str, clock=time.time) -> str:
    """Шаг 6: заведение фактора → подтверждение кодом по времени → сессия «2»."""
    token, ctx = vh.form_token(lane, SECOND_FACTOR_FORM, {vh.SESSION_COOKIE: session})
    cookies = {vh.FORM_COOKIE: ctx, vh.SESSION_COOKIE: session}
    code, _, text = lane.ask("POST", SF_ENROLL, body={"csrfToken": token}, cookies=cookies)
    secret = _json(text).get("secret") or ""
    if code != 200 or not isinstance(secret, str) or not secret:
        raise Finding(f"заведение второго фактора: ждали 200 с секретом, получили {code} {vh.message_of(text)}")
    code, sc, text = lane.ask("POST", SF_CONFIRM,
                              body={"code": totp(secret, int(clock()) // TOTP_STEP_S), "csrfToken": token},
                              cookies=cookies)
    level = (_json(text).get("session") or {}).get("assuranceLevel")
    raised = vh.cookie_value(sc, vh.SESSION_COOKIE)
    if code != 200 or level != "2" or not raised:
        raise Finding(f"подтверждение второго фактора: {code}, уровень {level!r}, носитель "
                      f"{'перевыпущен' if raised else 'НЕ перевыпущен'} — уровня «2» нет")
    return raised


def seed(stand: Stand, lane, enroll_human, suffix: str, clock=time.time, sleep=time.sleep) -> dict:
    """Семь шагов; результат — значения `MINTED_KEYS`. Секреты в журнал не уезжают."""
    rid, cid, csec = create_client(stand, f"s3-gate-{suffix}")
    say("  ok   1 клиент церемонии заведён на внутреннем слушателе края (способ секретом)")
    try:
        h = enroll_human()
        say(f"  ok   2 человек {h.email} заведён регистрацией и подтверждён кодом письма")
        if session_level(stand, h.bearer, "сессия человека") != "1":
            raise Finding("сессия подтверждённого человека — не уровня «1»")
        account, _project = vh.own_account_and_project(lane, h, sleep)
        basic = issue_basic(stand, h.user_id, suffix)
        say("  ok   3 человеку выпущено базовое удостоверение (вид SECRET)")
        bearer1 = ceremony_bearer(stand, (cid, csec), h.bearer, h.user_id, "1")
        say("  ok   4 церемония под сессией «1» выдала подписанного предъявителя уровня «1»")
        for what, kw in (("сессия браузера", {"session": h.bearer}), ("подписанный предъявитель", {"bearer": bearer1}),
                         ("базовое удостоверение", {"bearer": basic})):
            reaches(stand, what, h.user_id, **kw)
        say("  ok   5 все три класса уровня «1» доходят до принципала — один и тот же человек")
        session2 = level2_session(lane, h.bearer, clock)
        bearer2 = ceremony_bearer(stand, (cid, csec), session2, h.user_id, "2")
        reaches(stand, "сессия браузера уровня «2»", h.user_id, session=session2)
        reaches(stand, "подписанный предъявитель уровня «2»", h.user_id, bearer=bearer2)
        say("  ok   6 второй фактор поднял сессию до «2»; оба носителя «2» доходят до принципала")
        if session_level(stand, h.bearer, "сессия «1» после заведения фактора") != "1":
            raise Finding("сессия, выданная до заведения фактора, после него — не уровня «1»")
        reaches(stand, "сессия «1» после заведения фактора", h.user_id, session=h.bearer)
        reaches(stand, "предъявитель «1» после заведения фактора", h.user_id, bearer=bearer1)
        say("  ok   7 носители «1», выданные до фактора, живы и по-прежнему уровня «1»")
    except BaseException:
        # Снятие и на падении: клиент — единственное, что посев заводит вне человека.
        rc = delete_client(stand, rid)
        say(f"  снятие клиента церемонии после отказа: код {rc}")
        raise
    say("ГЕЙТ S3: классов носителей уровня «1» предъявлено 3 из 3, уровня «2» — 2 из 2; "
        "все доходят до одного принципала")
    return {"s3UserId": h.user_id, "s3Email": h.email, "s3AccountId": account,
            "s3SessionL1": h.bearer, "s3BearerL1": bearer1, "s3Basic": basic, "s3BasicTampered": tamper(basic),
            "s3SessionL2": session2, "s3BearerL2": bearer2}


def from_environment(boot: str) -> tuple[Stand, object, object]:
    """Стенд из окружения прогонщика — те же переменные, что у посева матрицы."""
    public = os.environ.get("BASE_URL", "http://localhost:18080")
    internal = os.environ.get("INTERNAL_BASE_URL", "https://localhost:18081")
    mailbox = vh.Mailbox(os.environ.get("MAILBOX_URL", "http://localhost:18025"))
    lane = vh.EdgeHttp(public)
    stand = Stand(public, internal, boot, internal_tls=vh.internal_rest_tls_context())
    return stand, lane, mailbox


# ─────────────────────────── самопроверка ────────────────────────────────────


def _fake_jwt(claims: dict) -> str:
    return "e30." + b64u(json.dumps(claims).encode()) + ".c2ln"


class _World:
    """Законный край в одном процессе; `breaks` меняет ровно один факт."""

    USER = "usr0000000000000s3t"

    def __init__(self, breaks: str = ""):
        self.breaks = breaks
        self.deleted: list[str] = []
        self.sessions = {"sess-1": "1"}
        self.codes: dict[str, str] = {}
        self.calls: list[str] = []

    def http(self, method, url, *, headers=None, json_body=None, form=None, tls=None):
        path = urllib.parse.urlsplit(url).path
        q = urllib.parse.parse_qs(urllib.parse.urlsplit(url).query)
        headers = headers or {}
        self.calls.append(f"{method} {path}")
        auth = headers.get("Authorization", "")
        cookie = headers.get("Cookie", "").split("=", 1)[-1]
        if path == IC_PATH and method == "POST":
            if self.breaks == "internal-down":
                raise Unmet("условие не создано: внутренний слушатель недостижим")
            err = {"code": 9, "message": "x"} if self.breaks == "client-op-error" else None
            body = {"done": True, "metadata": {"interactiveClientId": "ic-1"},
                    "response": {"interactiveClient": {"id": "ic-1", "clientId": "oic-1",
                                                       "tokenEndpointAuthMethod": SECRET_METHOD},
                                 "clientSecret": "client-secret-value"}}
            if err:
                body["error"] = err
            return 200, {}, json.dumps(body)
        if path.startswith(IC_PATH + "/") and method == "DELETE":
            self.deleted.append(path.rsplit("/", 1)[1])
            return 200, {}, "{}"
        if path.endswith("/tokens") and method == "POST":
            return 200, {}, json.dumps({"done": True, "response": {"secret": "kacho_uoc1_basicsecretvalue7"}})
        if path == AUTHORIZE:
            if self.breaks == "authorize-200":
                return 200, {}, "{}"
            code = secrets.token_hex(4)
            self.codes[code] = self.sessions.get(cookie, "")
            return 302, {"location": f"{REDIRECT}?code={code}&state={q['state'][0]}"}, ""
        if path == TOKEN:
            level = self.codes.pop(form["code"], "")
            if self.breaks == "token-acr-1" and level == "2":
                level = "1"
            return 200, {}, json.dumps({"access_token": _fake_jwt({"acr": level, "sub": self.USER})})
        if path == WHO_AM_I:
            user = self.USER
            if self.breaks == "basic-other-user" and auth.startswith("Bearer kacho_uoc1"):
                user = "usr-other"
            return 200, {}, json.dumps({"subject": "user:" + user, "userId": user})
        if path == vh.ME:
            level = self.sessions.get(cookie, "")
            if self.breaks == "l1-raised" and cookie == "sess-1" and "sess-2" in self.sessions:
                level = "2"
            return 200, {}, json.dumps({"session": {"assuranceLevel": level}})
        return 404, {}, "{}"

    # полоса формы входа: csrf, заведение и подтверждение фактора, списки собственности
    def ask(self, method, path, body=None, cookies=None, bearer=""):
        self.calls.append(f"{method} {path.split('?', 1)[0]}")
        if path.startswith(vh.CSRF):
            return 200, [f"{vh.FORM_COOKIE}=f; Path=/"], json.dumps({"csrfToken": "t"})
        if path == SF_ENROLL:
            return 200, [], json.dumps({"secret": "JBSWY3DPEHPK3PXP"})
        if path == SF_CONFIRM:
            self.sessions["sess-2"] = "2"
            return 200, [f"{vh.SESSION_COOKIE}=sess-2; Path=/"], json.dumps({"session": {"assuranceLevel": "2"}})
        if path.startswith(vh.ACCOUNTS):
            return 200, [], json.dumps({"accounts": [{"id": "acc-1", "ownerUserId": self.USER}]})
        if path.startswith(vh.PROJECTS):
            return 200, [], json.dumps({"projects": [{"id": "prj-1", "name": "default"}]})
        return 404, [], "{}"

    def human(self):
        return vh.Human(email="s3@example.test", kind=vh.KIND_VERIFIED, place="self-test",
                        user_id=self.USER, verified=True, bearer="sess-1")


def _outcome(world: _World) -> tuple[str, dict]:
    stand = Stand("http://edge", "https://edge-internal", "boot", http=world.http)
    try:
        return "ok", seed(stand, world, world.human, "st", clock=lambda: 0, sleep=lambda s: None)
    except Finding as e:
        return f"finding: {e}", {}
    except Unmet as e:
        return f"unmet: {e}", {}


def self_test() -> int:
    fails = []
    done = [0]

    def c(name, ok, detail=""):
        done[0] += 1
        print(f"  {'ok ' if ok else 'ОТКАЗ'} {name}" + ("" if ok else f" — {detail}"))
        if not ok:
            fails.append(name)

    print("самопроверка посева гейта S3 (#3159)")
    w = _World()
    out, vals = _outcome(w)
    c("законный мир: гейт пройден, ключей записано столько, сколько объявлено",
      out == "ok" and set(vals) == set(MINTED_KEYS) and all(vals.values()), f"{out} {sorted(vals)}")
    c("законный мир: клиент НЕ снят (наборам он нужен)", not w.deleted, f"снято {w.deleted}")
    c("законный мир: подделка той же марки отличается ровно последним знаком",
      vals.get("s3BasicTampered", "")[:-1] == vals.get("s3Basic", "")[:-1]
      and vals.get("s3BasicTampered") != vals.get("s3Basic"), "")
    c("законный мир: уровни токенов по составу — «1» и «2»",
      token_claims(vals.get("s3BearerL1", "a.e30.c"))["acr"] == "1"
      and token_claims(vals.get("s3BearerL2", "a.e30.c"))["acr"] == "2", "")
    for breaks, want in (("authorize-200", "finding"), ("token-acr-1", "finding"),
                         ("basic-other-user", "finding"), ("l1-raised", "finding"),
                         ("client-op-error", "finding"), ("internal-down", "unmet")):
        w = _World(breaks)
        out, _ = _outcome(w)
        c(f"инъекция {breaks}: исход {want}", out.startswith(want), out)
        if breaks not in ("client-op-error", "internal-down"):
            c(f"инъекция {breaks}: клиент церемонии снят на отказе", w.deleted == ["ic-1"], f"снято {w.deleted}")
    print(f"\nперепись: проверок исполнено {done[0]}, отказов {len(fails)}")
    if not done[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    return 1 if fails else 0


def main() -> int:
    ap = argparse.ArgumentParser(description="посев гейта S3 интерактивного входа (kacho#3159)")
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--minted-keys", action="store_true", help="напечатать объявленные ключи окружения")
    args = ap.parse_args()
    if args.self_test:
        return self_test()
    if args.minted_keys:
        print("\n".join(MINTED_KEYS))
        return 0
    ap.error("модуль зовётся из prodseed_all.py (суита api-gateway); отдельно — --self-test, --minted-keys")
    return 2


if __name__ == "__main__":
    sys.exit(main())
