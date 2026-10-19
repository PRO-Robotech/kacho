#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Владелец стенда проб — носитель уровня «2» путём человека (kacho#3281).

ПРЕДМЕТ. Пробы стенда (`stand-ns.sh`) работают от имени владельца аккаунта:
выдают доступ, снимают выдачи. Каталог прав ставит мутациям доступа пол
уверенности «2» (`required_acr_min` у `AccessBindingService.Create`, `Update`,
`Delete`), а вход паролем даёт сессию «1». Прежний посев владельца отдавал
сессию входа паролем, и каждая полоса проб поднимала уровень у себя сама.

Здесь уровень «2» достигается ТЕМ ЖЕ вторым фактором, что у человека: фактор по
времени заводится полосой входа (`second-factor/enroll` → `confirm`), повторный
посев входит паролем и повышает сессию кодом фактора (`step-up`, способ
`totp`). Пол каталога не снижается, обходной ручки уровня у посева нет.

ФОРМА ПОСЕВА.
  * занятость — один владелец на стенд: адрес, пароль, идентификатор и секрет
    фактора — в файле состояния (0600, `STAND_OWNER_STATE`), его путь лежит в
    рабочем каталоге стенда. Повторный прогон входит тем же владельцем и сверяет
    личность ДОСЛОВНО (идентификатор из «кто я»), второго не заводит;
  * без цели у конфликта: состояние есть, а вход, личность, фактор или уровень
    расходятся с ним — отказ (код 1), а не молчаливое «уже есть»;
  * утверждает СПОСОБНОСТЬ, а не наличие строки: владелец ВЫДАЁТ доступ в своём
    проекте носителем посева (`POST /iam/v1/accessBindings` → операция
    завершена без ошибки) и снимает выданное тем же носителем.

Секрет фактора, пароль, адрес и носитель в журнал не попадают: журнал называет
владельца словами. Носитель печатается в stdout ТОЛЬКО режимом `token`, и
только он — для подстановки `$(…)`.

РЕЖИМЫ
  (без флага)     посев + утверждение способности; stdout пуст
  --token         посев + утверждение способности; stdout — носитель уровня «2»
  --prove-twin    обе стороны пола (держатель предиката kacho#3281): носитель
                  посева проходит `Create` без отдельного шага повышения; тот же
                  запрос носителем входа паролем (уровень «1», меняется ровно
                  уровень) отвергнут полом — 401 · code 16 и текст края; выданное
                  снимается тем же сценарием, в том числе на падении
  --self-test     самопроверка без стенда: отказы различаются, секреты не
                  печатаются, файл состояния — 0600

Окружение (`stand-ns.sh forward` кладёт его в probe.env): KACHO_CONSOLE_URL —
фронт консоли, путь человека: он отдаёт полосу входа и API (`/iam/v1/`,
`/operations/`); соединение — на KACHO_CONSOLE_HOST_IP, лист сверяется по имени
консоли и корню NODE_EXTRA_CA_CERTS. Без консоли — KACHO_EDGE_URL (внешний
слушатель края; под TLS корень — KACHO_EDGE_CA). KACHO_CONSOLE_MAILBOX_URL либо
MAILBOX_URL — приёмник писем стенда; STAND_OWNER_STATE — файл состояния.

Коды: 0 — посеяно и способность подтверждена; 1 — находка (продукт ответил не
по контракту либо состояние разошлось с продуктом); 75 — условие не создано
(край, приёмник недостижимы, письмо не дошло).
"""
from __future__ import annotations

import json
import os
import secrets
import ssl
import sys
import time
import urllib.parse

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "..", "tests", "authz-fixtures"))

import verified_human as vh  # noqa: E402
from seed_ceremony import SF_CONFIRM, SF_ENROLL, SECOND_FACTOR_FORM, TOTP_STEP_S, totp  # noqa: E402

LOGIN = "/iam/v1/auth/login"
STEP_UP = "/iam/v1/auth/step-up"
STEP_UP_FORM = "step-up"
BINDINGS = "/iam/v1/accessBindings"
ROLES = "/iam/v1/roles"
OPERATIONS = "/operations"
# Роль выдачи — системная роль чтения сетей: её правила `[get, list]` не дают
# владельцу ничего сверх того, что у него уже есть в своём проекте, поэтому
# проба способности не расширяет ничьих прав даже на время своей жизни.
GRANT_ROLE_NAME = "vpc.network.view"
OP_BUDGET_S = float(os.environ.get("STAND_OWNER_OP_BUDGET_S", "90"))
LEVEL = "2"
STATE_MODE = 0o600


def say(msg: str) -> None:
    print(f"[stand-owner] {msg}", file=sys.stderr, flush=True)


# ─────────────────────────── состояние ────────────────────────────────────────


def load_state(path: str) -> dict:
    if not os.path.exists(path):
        return {}
    mode = os.stat(path).st_mode & 0o777
    if mode != STATE_MODE:
        raise vh.Finding(f"файл состояния владельца открыт шире 0600 (права {mode:o}) — "
                         "секрет фактора мог быть прочитан; посев на нём не продолжается")
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def save_state(path: str, state: dict) -> None:
    """Запись целиком через временный файл того же каталога: прерванная запись
    не оставляет полусостояния, права 0600 стоят с момента создания."""
    os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
    tmp = f"{path}.{os.getpid()}.part"
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, STATE_MODE)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump(state, f)
    os.replace(tmp, path)


# ─────────────────────────── полоса входа ─────────────────────────────────────


def _json(text: str) -> dict:
    try:
        d = json.loads(text or "{}")
    except json.JSONDecodeError:
        return {}
    return d if isinstance(d, dict) else {}


def password_login(edge, email: str, password: str) -> str:
    token, ctx = vh.form_token(edge, "login")
    code, sc, text = edge.ask("POST", LOGIN, body={"email": email, "password": password, "csrfToken": token},
                              cookies={vh.FORM_COOKIE: ctx})
    bearer = vh.cookie_value(sc, vh.SESSION_COOKIE)
    if code != 200 or not bearer:
        raise vh.Finding(f"вход владельца паролем: ждали 200 с носителем, получили {code} {vh.message_of(text)}")
    return bearer


def session_level(edge, bearer: str, what: str) -> str:
    code, _sc, text = edge.ask("GET", vh.ME, cookies={vh.SESSION_COOKIE: bearer})
    if code != 200:
        raise vh.Finding(f"{what}: «кто я» — ждали 200, получили {code} {vh.message_of(text)}")
    return str((_json(text).get("session") or {}).get("assuranceLevel"))


def fresh_step(state: dict, clock, sleep) -> int:
    """Шаг кода по времени, ещё не предъявленный этим владельцем: код одного шага
    принимается один раз, и повторный посев внутри того же шага ждёт следующего —
    ждётся условие (смена шага), а не время наугад."""
    step = int(clock()) // TOTP_STEP_S
    last = int(state.get("lastStep") or -1)
    if step <= last:
        sleep((last + 1) * TOTP_STEP_S - clock() + 0.5)
        step = int(clock()) // TOTP_STEP_S
    return step


def enroll_factor(edge, state: dict, state_path: str, bearer: str, clock, sleep) -> str:
    """Фактор по времени: секрет из ответа заведения сохраняется ДО подтверждения —
    прерванный посев не теряет секрет уже заведённого фактора."""
    token, ctx = vh.form_token(edge, SECOND_FACTOR_FORM, {vh.SESSION_COOKIE: bearer})
    cookies = {vh.FORM_COOKIE: ctx, vh.SESSION_COOKIE: bearer}
    code, _sc, text = edge.ask("POST", SF_ENROLL, body={"csrfToken": token}, cookies=cookies)
    secret = _json(text).get("secret") or ""
    if code != 200 or not isinstance(secret, str) or not secret:
        raise vh.Finding(f"заведение второго фактора владельца: ждали 200 с секретом, получили {code} "
                         f"{vh.message_of(text)}")
    state["totpSecret"] = secret
    save_state(state_path, state)
    step = fresh_step(state, clock, sleep)
    code, sc, text = edge.ask("POST", SF_CONFIRM, body={"code": totp(secret, step), "csrfToken": token},
                              cookies=cookies)
    state["lastStep"] = step
    level = (_json(text).get("session") or {}).get("assuranceLevel")
    raised = vh.cookie_value(sc, vh.SESSION_COOKIE)
    if code != 200 or level != LEVEL or not raised:
        raise vh.Finding(f"подтверждение второго фактора владельца: {code}, уровень {level!r}, носитель "
                         f"{'перевыпущен' if raised else 'НЕ перевыпущен'} — уровня «2» нет")
    state["factorConfirmed"] = True
    save_state(state_path, state)
    return raised


def step_up(edge, state: dict, state_path: str, bearer: str, clock, sleep) -> str:
    token, ctx = vh.form_token(edge, STEP_UP_FORM, {vh.SESSION_COOKIE: bearer})
    step = fresh_step(state, clock, sleep)
    code, sc, text = edge.ask("POST", STEP_UP,
                              body={"method": "totp", "code": totp(state["totpSecret"], step), "csrfToken": token},
                              cookies={vh.FORM_COOKIE: ctx, vh.SESSION_COOKIE: bearer})
    state["lastStep"] = step
    save_state(state_path, state)
    level = (_json(text).get("session") or {}).get("assuranceLevel")
    raised = vh.cookie_value(sc, vh.SESSION_COOKIE)
    if code != 200 or level != LEVEL or not raised:
        raise vh.Finding(f"повышение сессии владельца кодом фактора: {code}, уровень {level!r} — "
                         f"{vh.message_of(text)}")
    return raised


# ─────────────────────────── посев ────────────────────────────────────────────


def human_enroller(edge, mailbox, sleep=time.sleep):
    """Заведение владельца путём человека: регистрация, код письма, собственность."""
    def enroll() -> tuple[str, str, str, str, str, str]:
        email = f"stand-owner-{secrets.token_hex(4)}@example.com"
        password = secrets.token_urlsafe(18)
        redact(email)
        h = vh.enroll(edge, mailbox, email, password, vh.KIND_VERIFIED, clock=lambda: time.strftime("%H:%M:%S"),
                      sleep=sleep, place="deploy/scripts/stand_owner.py")
        acct, proj = vh.own_account_and_project(edge, h, sleep)
        return email, password, h.user_id, h.bearer, acct, proj
    return enroll


def ensure_owner(edge, enroll, state_path: str, clock=time.time, sleep=time.sleep) -> tuple[dict, str]:
    """Владелец стенда и его носитель уровня «2». Возвращает (состояние, носитель)."""
    state = load_state(state_path)
    if not state.get("email"):
        email, password, user_id, bearer, acct, proj = enroll()
        state = {"email": email, "password": password, "userId": user_id, "accountId": acct, "projectId": proj}
        save_state(state_path, state)
        say(f"владелец заведён регистрацией, адрес подтверждён кодом письма: userId {user_id}")
        bearer = enroll_factor(edge, state, state_path, bearer, clock, sleep)
        say("второй фактор владельца заведён и подтверждён — сессия поднята до «2»")
    else:
        redact(state["email"])
        bearer = password_login(edge, state["email"], state["password"])
        user_id, verified = vh.who_am_i(edge, "владелец", bearer)
        if user_id != state.get("userId"):
            raise vh.Finding(f"владелец из состояния вошёл другим субъектом ({user_id} != {state.get('userId')})")
        if verified is not True:
            raise vh.Finding("владелец из состояния вошёл с неподтверждённым адресом")
        say(f"владелец из состояния вошёл тем же субъектом ({user_id}) — второго не завожу")
        if not state.get("totpSecret") or not state.get("factorConfirmed"):
            raise vh.Finding("в состоянии владельца нет подтверждённого фактора — посев прерван между "
                             "заведением и подтверждением; стенд проб поднимается заново")
        if session_level(edge, bearer, "вход владельца паролем") != "1":
            raise vh.Finding("вход владельца паролем при заведённом факторе — не уровня «1»")
        bearer = step_up(edge, state, state_path, bearer, clock, sleep)
        say("сессия владельца поднята до «2» кодом его фактора")
    if session_level(edge, bearer, "носитель посева") != LEVEL:
        raise vh.Finding("носитель посева — не уровня «2» по ответу «кто я»")
    return state, bearer


# ─────────────────────────── способность ──────────────────────────────────────


def grant_role(edge, bearer: str) -> str:
    code, _sc, text = edge.ask("GET", f"{ROLES}?pageSize=1000", cookies={vh.SESSION_COOKIE: bearer})
    if code != 200:
        raise vh.Finding(f"перечень ролей: ждали 200, получили {code} {vh.message_of(text)}")
    ids = [r.get("id") for r in _json(text).get("roles") or [] if r.get("name") == GRANT_ROLE_NAME]
    if len(ids) != 1 or not ids[0]:
        raise vh.Finding(f"роль {GRANT_ROLE_NAME}: в перечне найдено {len(ids)}, ждали ровно одну")
    return ids[0]


def binding_body(state: dict, role_id: str) -> dict:
    return {"subjects": [{"type": "SUBJECT_TYPE_USER", "id": state["userId"]}], "roleId": role_id,
            "scopeType": "iam.project", "scopeId": state["projectId"], "target": {"allInScope": {}}}


def await_operation(edge, bearer: str, op: dict, what: str, sleep) -> dict:
    oid = op.get("id")
    if not oid:
        raise vh.Finding(f"{what}: ответ без id операции")
    left = OP_BUDGET_S
    while True:
        code, _sc, text = edge.ask("GET", f"{OPERATIONS}/{oid}", cookies={vh.SESSION_COOKIE: bearer})
        if code != 200:
            raise vh.Finding(f"{what}: опрос операции {oid} — {code} {vh.message_of(text)}")
        d = _json(text)
        if d.get("done"):
            if d.get("error"):
                raise vh.Finding(f"{what}: операция {oid} завершилась ошибкой {json.dumps(d['error'])[:300]}")
            return d
        if left <= 0:
            raise vh.Finding(f"{what}: операция {oid} не завершилась за {OP_BUDGET_S:.0f} с")
        sleep(1.0)
        left -= 1.0


def create_binding(edge, bearer: str, body: dict) -> tuple[int, dict, str]:
    code, _sc, text = edge.ask("POST", BINDINGS, body=body, cookies={vh.SESSION_COOKIE: bearer})
    return code, _json(text), text


def revoke_binding(edge, bearer: str, binding_id: str, sleep) -> None:
    code, _sc, text = edge.ask("DELETE", f"{BINDINGS}/{binding_id}", cookies={vh.SESSION_COOKIE: bearer})
    if code != 200:
        raise vh.Finding(f"снятие выдачи {binding_id}: ждали 200, получили {code} {vh.message_of(text)}")
    await_operation(edge, bearer, _json(text), f"снятие выдачи {binding_id}", sleep)
    code, _sc, text = edge.ask("GET", f"{BINDINGS}/{binding_id}", cookies={vh.SESSION_COOKIE: bearer})
    if code != 404:
        raise vh.Finding(f"выдача {binding_id} после снятия отвечает {code}, ждали 404")


def granted_and_revoked(edge, bearer: str, state: dict, sleep) -> str:
    """Способность владельца: выдача носителем посева заводится и снимается. Возвращает
    идентификатор (уже снятой) выдачи."""
    code, op, text = create_binding(edge, bearer, binding_body(state, grant_role(edge, bearer)))
    if code != 200:
        raise vh.Finding(f"выдача доступа носителем посева: ждали 200 и операцию, получили {code} "
                         f"{vh.message_of(text)} — посев не дал владельцу уровня, который требует каталог")
    done = await_operation(edge, bearer, op, "выдача доступа носителем посева", sleep)
    bid = (done.get("metadata") or {}).get("accessBindingId") or (done.get("response") or {}).get("id")
    if not bid:
        raise vh.Finding("выдача доступа: операция завершена без accessBindingId")
    revoke_binding(edge, bearer, bid, sleep)
    return bid


# Отказ полом уверенности на пути человека — дословно тем, что отдаёт край
# (замер на стенде проб: носитель сессии уровня «1» на `POST /iam/v1/accessBindings`).
FLOOR_STATUS = 401
FLOOR_CODE = 16
FLOOR_TEXT = ("authentication level is insufficient: step up with a second factor, "
              "or present a credential of another kind")


def floor_refusal(code: int, text: str) -> bool:
    """Отказ полом: статус, код и текст края — все три, текст побайтно."""
    body = _json(text)
    return code == FLOOR_STATUS and body.get("code") == FLOOR_CODE and body.get("message") == FLOOR_TEXT


def prove_twin(edge, enroll, state_path: str, clock=time.time, sleep=time.sleep) -> int:
    state, bearer2 = ensure_owner(edge, enroll, state_path, clock, sleep)
    body = binding_body(state, grant_role(edge, bearer2))
    bearer1 = password_login(edge, state["email"], state["password"])
    if session_level(edge, bearer1, "близнец: вход паролем") != "1":
        raise vh.Finding("близнец: вход паролем — не уровня «1»; один факт против носителя посева не выделен")
    code, op, text = create_binding(edge, bearer1, body)
    if not floor_refusal(code, text):
        if code == 200 and op.get("id"):
            done = await_operation(edge, bearer2, op, "близнец: выдача, которой быть не должно", sleep)
            bid = (done.get("metadata") or {}).get("accessBindingId")
            if bid:
                revoke_binding(edge, bearer2, bid, sleep)
        raise vh.Finding(f"близнец: носитель уровня «1» получил {code} {vh.message_of(text)} — пола «2» нет")
    say(f"близнец: тот же запрос носителем уровня «1» → {code} · code {FLOOR_CODE} · «{FLOOR_TEXT}» — отказ полом")
    bid = granted_and_revoked(edge, bearer2, state, sleep)
    say(f"носитель посева: выдача {bid} заведена без шага повышения (операция done без error) и снята, "
        "чтение снятой → 404")
    return 0


# ─────────────────────────── вход ─────────────────────────────────────────────


_REDACTED: list[str] = []


def redact(email: str) -> None:
    """Адрес владельца в журнал не попадает: общий модуль посева называет человека
    адресом, здесь он заменяется словами до печати."""
    if not email or email in _REDACTED:
        return
    _REDACTED.append(email)
    inner = vh.say
    vh.say = lambda msg: inner(msg.replace(email, "<адрес владельца>"))


def scrub(text: str) -> str:
    for e in _REDACTED:
        text = text.replace(e, "<адрес владельца>")
    return text


class _NamedContext(ssl.SSLContext):
    """Лист фронта сверяется по ИМЕНИ стенда: проброс отвечает на 127.0.0.1, а лист
    выписан на имя консоли, поэтому имя сверки берётся из её адреса, а не из адреса
    соединения. Цепочка и имя проверяются оба."""

    stand_host = ""

    def wrap_socket(self, sock, *args, server_hostname=None, **kwargs):  # noqa: D401
        return super().wrap_socket(sock, *args, server_hostname=self.stand_host or server_hostname, **kwargs)


def _verifying(ca_env: tuple[str, ...], what: str) -> _NamedContext:
    ca = next((os.environ[e] for e in ca_env if os.environ.get(e)), "")
    if not ca:
        raise vh.Unmet(f"условие не создано: {what} под TLS, а корня его листа нет ({' / '.join(ca_env)})")
    ctx = _NamedContext(ssl.PROTOCOL_TLS_CLIENT)
    ctx.load_verify_locations(cafile=ca)
    return ctx


def stand_front() -> tuple[str, ssl.SSLContext | None]:
    """Адрес и TLS фронта, через который посев идёт путём человека."""
    console = os.environ.get("KACHO_CONSOLE_URL", "")
    if console:
        u = urllib.parse.urlparse(console)
        ip = os.environ.get("KACHO_CONSOLE_HOST_IP") or u.hostname
        ctx = _verifying(("NODE_EXTRA_CA_CERTS",), "фронт консоли")
        ctx.stand_host = u.hostname or ""
        return f"{u.scheme}://{ip}:{u.port or 443}", ctx
    edge = os.environ["KACHO_EDGE_URL"]
    if not edge.startswith("https"):
        return edge, None
    return edge, _verifying(("KACHO_EDGE_CA",), "край")


def main(argv: list[str]) -> int:
    try:
        base, tls = stand_front()
        edge = vh.EdgeHttp(base, tls=tls)
        mailbox = vh.Mailbox(os.environ.get("KACHO_CONSOLE_MAILBOX_URL") or os.environ["MAILBOX_URL"])
        state_path = os.environ["STAND_OWNER_STATE"]
        vh.mailbox_precondition(mailbox)
        enroll = human_enroller(edge, mailbox)
        if "--prove-twin" in argv:
            return prove_twin(edge, enroll, state_path)
        state, bearer = ensure_owner(edge, enroll, state_path)
        bid = granted_and_revoked(edge, bearer, state, time.sleep)
        say(f"способность подтверждена: владелец выдал доступ в своём проекте без шага повышения "
            f"и снял выданное ({bid})")
        if "--token" in argv:
            print(bearer)
        return 0
    except KeyError as e:
        say(f"НЕ ВЫПОЛНИЛОСЬ: условие не создано — нет переменной окружения {e}")
        return vh.RC_UNMET
    except vh.Unmet as e:
        say("НЕ ВЫПОЛНИЛОСЬ: " + scrub(str(e)))
        return vh.RC_UNMET
    except vh.Finding as e:
        say("НАХОДКА: " + scrub(str(e)))
        return vh.RC_FINDING


# ─────────────────────────── самопроверка ─────────────────────────────────────
#
# Подставной край: законный мир и инъекции, каждая меняет ОДИН факт. Секреты
# законного мира — заметные строки, и вывод каждого прогона сверяется с ними: ни
# одна не обязана попасть в журнал.

_SECRET = "JBSWY3DPEHPK3PXP"
_PASSWORD = "pw-selftest-Zq81"
_EMAIL = "stand-owner-selftest@example.com"


class _World:
    def __init__(self, breaks: str = ""):
        self.breaks = breaks
        self.n = 0
        self.factor = False
        self.used_steps: set[int] = set()
        self.bindings: dict[str, bool] = {}
        self.calls: list[tuple[str, str, int]] = []

    def _bearer(self, level: str) -> str:
        self.n += 1
        return f"s{level}-{self.n}"

    @staticmethod
    def _level(cookies: dict | None) -> str:
        b = (cookies or {}).get(vh.SESSION_COOKIE, "")
        return b[1] if b.startswith("s") and len(b) > 1 else ""

    def ask(self, method, path, body=None, cookies=None, bearer=""):
        out = self._ask(method, path.split("?", 1)[0], body or {}, cookies or {})
        self.calls.append((method, path.split("?", 1)[0], out[0]))
        return out

    def _ask(self, method, path, body, cookies):
        ok = lambda d, sc=(): (200, list(sc), json.dumps(d))  # noqa: E731
        level = self._level(cookies)
        if path == vh.CSRF:
            return ok({"csrfToken": "t"}, [f"{vh.FORM_COOKIE}=f; Path=/"])
        if path == LOGIN:
            if body.get("password") != _PASSWORD:
                return 401, [], "{}"
            return ok({}, [f"{vh.SESSION_COOKIE}={self._bearer('1')}; Path=/"])
        if path == vh.ME:
            if not level:
                return 401, [], "{}"
            uid = "usrOTHER" if self.breaks == "other-subject" else "usrOWNER"
            return ok({"user": {"id": uid}, "session": {"emailVerified": True, "assuranceLevel": level}})
        if path == SF_ENROLL:
            self.factor = True
            return ok({"secret": _SECRET})
        if path in (SF_CONFIRM, STEP_UP):
            step = next(s for s in range(int(_CLOCK[0]) // TOTP_STEP_S - 1, int(_CLOCK[0]) // TOTP_STEP_S + 2)
                        if totp(_SECRET, s) == body.get("code"))
            if step in self.used_steps:
                return 400, [], json.dumps({"message": "code already used"})
            self.used_steps.add(step)
            got = "1" if (self.breaks == "step-up-stays-1" and path == STEP_UP) else "2"
            return ok({"session": {"assuranceLevel": got}}, [f"{vh.SESSION_COOKIE}={self._bearer(got)}; Path=/"])
        if path == ROLES:
            return ok({"roles": [{"id": "rolNETVIEW", "name": GRANT_ROLE_NAME}, {"id": "rolX", "name": "x"}]})
        if path == BINDINGS and method == "POST":
            if level != "2" and self.breaks == "other-refusal":
                return 403, [], json.dumps({"code": 7, "message": "permission denied"})
            if level != "2" and self.breaks != "no-floor":
                return FLOOR_STATUS, [], json.dumps({"code": FLOOR_CODE, "message": FLOOR_TEXT})
            bid = f"acb{len(self.bindings) + 1}"
            self.bindings[bid] = True
            return ok({"id": f"op-c-{bid}"})
        if path.startswith(BINDINGS + "/"):
            bid = path.rsplit("/", 1)[1]
            if method == "DELETE":
                if level != "2":
                    return 403, [], "{}"
                self.bindings[bid] = False
                return ok({"id": f"op-d-{bid}"})
            return (200, [], "{}") if self.bindings.get(bid) else (404, [], "{}")
        if path.startswith(OPERATIONS + "/"):
            oid = path.rsplit("/", 1)[1]
            return ok({"id": oid, "done": True, "metadata": {"accessBindingId": oid.split("-", 2)[2]}})
        return 404, [], "{}"


_CLOCK = [1_700_000_000.0]


def _clock() -> float:
    return _CLOCK[0]


def _sleep(s: float) -> None:
    _CLOCK[0] += max(s, 0.0)


def _enroll():
    return _EMAIL, _PASSWORD, "usrOWNER", "s1-0", "accOWNER", "prjOWNER"


def _run(world, fn, *args) -> tuple[str, str]:
    import contextlib
    import io

    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        try:
            fn(world, *args)
            verdict = "ok"
        except vh.Finding:
            verdict = "finding"
    return verdict, out.getvalue() + err.getvalue()


def self_test() -> int:
    import tempfile

    fails: list[str] = []
    printed = []

    def seed_twice(world, path):
        ensure_owner(world, _enroll, path, _clock, _sleep)
        _CLOCK[0] += 1
        _state, b = ensure_owner(world, _enroll, path, _clock, _sleep)
        granted_and_revoked(world, b, load_state(path), _sleep)

    def twin(world, path):
        prove_twin(world, _enroll, path, _clock, _sleep)

    cases = [
        ("законный мир: заведение, повторный вход с повышением, способность", "", seed_twice, "ok", None),
        ("законный мир: обе стороны пола", "", twin, "ok", None),
        ("пола нет: носитель «1» выдаёт доступ", "no-floor", twin, "finding", None),
        ("носитель «1» отвергнут не полом, а правом", "other-refusal", twin, "finding", None),
        ("повторный вход другим субъектом", "other-subject", seed_twice, "finding", None),
        ("повышение не подняло уровень", "step-up-stays-1", seed_twice, "finding", None),
        ("файл состояния открыт шире 0600", "", seed_twice, "finding", 0o644),
    ]
    for title, breaks, fn, want, mode in cases:
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "owner.json")
            if mode is not None:
                save_state(path, {"email": _EMAIL, "password": _PASSWORD, "userId": "usrOWNER",
                                  "totpSecret": _SECRET, "factorConfirmed": True, "projectId": "prjOWNER"})
                os.chmod(path, mode)
            world = _World(breaks)
            got, text = _run(world, fn, path)
            printed.append(text)
            if got != want:
                fails.append(f"{title}: ждали {want}, получили {got}")
            if want == "ok":
                st = os.stat(path).st_mode & 0o777
                if st != STATE_MODE:
                    fails.append(f"{title}: файл состояния с правами {st:o}, ждали 600")
                if load_state(path).get("totpSecret") != _SECRET:
                    fails.append(f"{title}: секрет фактора не сохранён в состоянии")
                if any(world.bindings.values()):
                    fails.append(f"{title}: выданное сценарием не снято")
            print(f"  {'ok ' if got == want else 'FAIL'} {title}: {got}")
    leaked = [s for s in (_SECRET, _PASSWORD) if any(s in t for t in printed)]
    if leaked:
        fails.append(f"в выводе посева найдено {len(leaked)} секретов законного мира")
    print(f"stand_owner self-test: случаев {len(cases)}, отказов {len(fails)}")
    for f in fails:
        print(f"  FAIL {f}")
    return 1 if fails else 0


if __name__ == "__main__":
    if "--self-test" in sys.argv[1:]:
        sys.exit(self_test())
    sys.exit(main(sys.argv[1:]))
