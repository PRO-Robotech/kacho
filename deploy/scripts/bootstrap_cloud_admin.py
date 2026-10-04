#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Первый администратор облака стенда — путём продукта (kacho#2878).

ПРЕДМЕТ. Подъём стенда заводит человека, который входит паролем и получает
раздел «Система» консоли. Право администратора облака (`system_admin` на
кластере) выдаёт сама служба доступа — посевом бутстрапа по адресу из
KANAME_BOOTSTRAP_ROOT_EMAIL; эта программа ничего не пишет в базу и прав не
выдаёт. Она проходит тот путь, что человек:

  1. вход паролем на внешнем слушателе края (`/iam/v1/auth/login`). Прошёл —
     человек уже заведён (повторный подъём), регистрации не будет;
  2. отказ входа — регистрация, код из письма приёмника стенда, подтверждение
     (tests/authz-fixtures/verified_human.py — тот же модуль, что у посева
     наборов). Вошедший, но с неподтверждённым адресом, просит письмо и
     подтверждает;
  3. ожидание выдачи: `GET /vpc/v1/addressPools` (чтение раздела «Система»,
     отношение `system_admin` на кластере) на внутреннем слушателе края —
     ждём 200 в пределах бюджета.

Режим `--prove-twin` (доказательство п.2 задачи, не шаг подъёма): владелец
аккаунта, заведённый регистрацией, на том же пути получает 403.

Адрес и пароль приходят ОКРУЖЕНИЕМ (`KACHO_CLOUD_ADMIN_EMAIL`,
`KACHO_CLOUD_ADMIN_PASSWORD`) и не печатаются никогда: сообщения называют
человека словами «администратор облака», а не адресом.

Коды: 0 — администратор входит и раздел отвечает данными; 1 — находка
(продукт ответил не по контракту); 75 — условие не создано (край, приёмник
недостижимы, письмо не дошло).
"""
from __future__ import annotations

import json
import os
import secrets
import sys
import time
import urllib.parse

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "..", "tests", "authz-fixtures"))

import verified_human as vh  # noqa: E402

LOGIN = "/iam/v1/auth/login"
SYSTEM_PATH = "/vpc/v1/addressPools"
GRANT_BUDGET_S = float(os.environ.get("KACHO_CLOUD_ADMIN_GRANT_BUDGET_S", "120"))


def say(msg: str) -> None:
    print(f"[cloud-admin] {msg}", flush=True)


def login(edge, email: str, password: str) -> tuple[int, str, bool | None]:
    token, ctx = vh.form_token(edge, "login")
    status, sc, text = edge.ask("POST", LOGIN, body={"email": email, "password": password, "csrfToken": token},
                                cookies={vh.FORM_COOKIE: ctx})
    if status != 200:
        return status, "", None
    bearer = vh.cookie_value(sc, vh.SESSION_COOKIE)
    if not bearer:
        raise vh.Finding("вход администратора облака: 200 без носителя сессии в Set-Cookie")
    return status, bearer, vh.session_position(text, "вход администратора облака")


def request_letter(edge, bearer: str) -> None:
    token, ctx = vh.form_token(edge, "verify-email", {vh.SESSION_COOKIE: bearer})
    status, _sc, text = edge.ask("POST", vh.VERIFY_REQUEST, body={"csrfToken": token},
                                 cookies={vh.FORM_COOKIE: ctx, vh.SESSION_COOKIE: bearer})
    if status not in (200, 202):
        raise vh.Finding(f"запрос письма подтверждения: ждали 200, получили {status} {vh.message_of(text)}")


def system_read(internal, bearer: str) -> tuple[int, int]:
    status, _sc, text = internal.ask("GET", f"{SYSTEM_PATH}?pageSize=100", cookies={vh.SESSION_COOKIE: bearer})
    rows = -1
    if status == 200:
        try:
            rows = len(json.loads(text or "{}").get("addressPools") or [])
        except (json.JSONDecodeError, AttributeError):
            raise vh.Finding(f"GET {SYSTEM_PATH}: 200 не-JSON") from None
    return status, rows


def ensure_admin(edge, internal, mailbox, email: str, password: str) -> int:
    status, bearer, verified = login(edge, email, password)
    if status == 200:
        say("администратор облака уже заведён — вход паролем прошёл, регистрации нет")
        if verified is False:
            say("адрес не подтверждён — прошу письмо и подтверждаю кодом из него")
            before = frozenset(x["id"] for x in mailbox.letters(email))
            request_letter(edge, bearer)
            letter = _await(mailbox, email, before)
            bearer = vh.confirm(edge, "администратор облака", bearer, vh.code_of(letter["text"]))
    elif status in (401, 403):
        say(f"вход паролем отказан ({status}) — завожу человека регистрацией")
        h = vh.enroll(edge, mailbox, email, password, vh.KIND_VERIFIED, clock=lambda: time.strftime("%H:%M:%S"),
                      sleep=time.sleep, place="deploy/scripts/bootstrap_cloud_admin.py")
        bearer = h.bearer
        say("человек заведён, адрес подтверждён кодом из письма")
    else:
        raise vh.Finding(f"вход паролем: ждали 200 либо отказ 401/403, получили {status}")

    left = GRANT_BUDGET_S
    while True:
        code, rows = system_read(internal, bearer)
        if code == 200:
            say(f"раздел «Система»: GET {SYSTEM_PATH} на внутреннем слушателе → 200, записей {rows}")
            return 0
        if code == 404:
            raise vh.Unmet(f"условие не создано: GET {SYSTEM_PATH} → 404, плоскости администрирования на слушателе нет")
        if left <= 0:
            raise vh.Finding(f"за {GRANT_BUDGET_S:.0f} с право администратора облака не выдано: GET {SYSTEM_PATH} → {code}")
        time.sleep(2.0)
        left -= 2.0


def _await(mailbox, email: str, before: frozenset) -> dict:
    left = vh.LETTER_BUDGET_S
    while True:
        letters = [x for x in mailbox.letters(email) if x["id"] not in before]
        if letters:
            return letters[-1]
        if left <= 0:
            raise vh.Unmet(f"{vh.UNMET_LETTER}: письмо администратору облака не дошло за {vh.LETTER_BUDGET_S:.0f} с")
        time.sleep(1.0)
        left -= 1.0


def prove_twin(edge, internal, mailbox) -> int:
    email = f"owner-{secrets.token_hex(4)}@example.com"
    h = vh.enroll(edge, mailbox, email, secrets.token_hex(16), vh.KIND_VERIFIED,
                  clock=lambda: time.strftime("%H:%M:%S"), sleep=time.sleep, place="prove-twin")
    acct, _proj = vh.own_account_and_project(edge, h, time.sleep)
    say(f"близнец: владелец аккаунта заведён регистрацией (аккаунт найден: {bool(acct)})")
    code, _rows = system_read(internal, h.bearer)
    say(f"близнец: GET {SYSTEM_PATH} на внутреннем слушателе → {code}")
    if code != 403:
        raise vh.Finding(f"близнец: владелец аккаунта получил {code} на чтении раздела «Система», ждали 403")
    return 0


def redact(email: str) -> None:
    """Сообщения общего модуля посева называют человека адресом; здесь адрес
    администратора облака заменяется словами до печати — журнал подъёма его не
    несёт (п.3 задачи)."""
    if not email:
        return
    inner = vh.say
    vh.say = lambda msg: inner(msg.replace(email, "<адрес администратора>"))


def main(argv: list[str]) -> int:
    edge = vh.EdgeHttp(os.environ["KACHO_EDGE_URL"])
    internal = vh.EdgeHttp(os.environ["KACHO_EDGE_INTERNAL_URL"])
    mailbox = vh.Mailbox(os.environ["MAILBOX_URL"])
    try:
        vh.mailbox_precondition(mailbox)
        if "--prove-twin" in argv:
            return prove_twin(edge, internal, mailbox)
        email = os.environ.get("KACHO_CLOUD_ADMIN_EMAIL", "")
        password = os.environ.get("KACHO_CLOUD_ADMIN_PASSWORD", "")
        redact(email)
        if not email or not password:
            raise vh.Unmet("условие не создано: в секрете стенда нет адреса либо пароля администратора облака")
        return ensure_admin(edge, internal, mailbox, email, password)
    except vh.Unmet as e:
        say(str(e).replace(os.environ.get("KACHO_CLOUD_ADMIN_EMAIL", "\0"), "<адрес администратора>"))
        return vh.RC_UNMET
    except vh.Finding as e:
        say("НАХОДКА: " + str(e).replace(os.environ.get("KACHO_CLOUD_ADMIN_EMAIL", "\0"), "<адрес администратора>"))
        return vh.RC_FINDING


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
