#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Посев «Дано» набора отказов края по сессии (kacho#3334).

Набор `iam-edge-session-refusals` (кейсы LMR-01, LMR-03, Ф12-30, EV-14) читает слоты,
которые заводит этот модуль. ЛЮДЕЙ набор не заводит (гейт
`internal/repohygiene/suitepeopleenrollment_test.go`): люди — регистрацией и письмом,
как у любого человека посева (`verified_human.enroll`), а здесь — их ПОЛОЖЕНИЕ.

  Администратор облака `A` — человек, которому внутренний слушатель выдал `system_admin`
      на кластере; выдача видна модели прав до того, как слот отдан. Носитель — сессия
      уровня «2» (второй фактор заведён кодом по времени, RFC 6238).
  Цель `U` — человек с паролем, ОДНИМ ключом доступа и двумя живыми сессиями («1» и «2»).
      Ключ заводит церемония подставного аутентификатора (`webauthn_authenticator.py`) под
      сессией «2»: регистрация ключа требует уровня «2». Сессия «1» — вход паролем после
      заведения фактора (вход одним паролем при заведённом факторе даёт «1»). Сброс
      ключей РАСХОДУЕТ эту цель: повторный прогон набора на том же посеве даёт другой
      исход, поэтому посев один на прогон и слоты не переживают его.
  Посторонний `O` — человек без выдач на цели; сессия уровня «2» — отказ ему приходит
      по праву, а не по уровню.
  Прохожий `T` — человек, на которого `O` целится «правильным» идентификатором: его
      ничего не расходует, он не цель сброса.
  Ф-н `N` — человек с неподтверждённым адресом (положение Ф-н посева, F6b). Код из
      письма регистрации кладётся в слот: шаг подтверждения исполняет кейс (EV-14), и код
      расходуется им же.

Пара «A против O» одно-фактна: держит ли вызывающий отношение на цель; сессии у обоих
«2». Пара «U до / после» — один человек, один факт: выполнен ли сброс.
"""
from __future__ import annotations

import json
import re
import subprocess

import verified_human as vh
import webauthn_authenticator as wa

KEYS = "/iam/v1/users/{uid}/accessKeys"
KEYS_BEGIN = "/iam/v1/users/{uid}/accessKeys:beginRegistration"

ORIGIN_RE = re.compile(r'origins:\s*\n\s*-\s*"([^"]+)"')


def webauthn_origin(namespace: str, env: dict) -> str:
    """Происхождение, которое служба держит в перечне ключей доступа.

    Берётся у ПОСАДКИ (`kaname-config` стенда), а не выводится из имени: вывод из имени
    был бы вторым вычислением величины, объявленной один раз — при подъёме.
    `SEED_WEBAUTHN_ORIGIN` задаёт её явно (прогон вне стенда-пространства)."""
    if env.get("SEED_WEBAUTHN_ORIGIN"):
        return env["SEED_WEBAUTHN_ORIGIN"]
    out = subprocess.run(["kubectl", "-n", namespace, "get", "cm", "kaname-config", "-o", "json"],
                         capture_output=True, text=True, timeout=60)
    if out.returncode != 0:
        raise vh.Unmet(f"условие не создано: перечень происхождений ключей доступа не прочитан "
                       f"(kubectl -n {namespace} get cm kaname-config: {out.stderr.strip()[:200]})")
    data = (json.loads(out.stdout).get("data") or {}).values()
    for v in data:
        m = ORIGIN_RE.search(v)
        if m:
            return m.group(1)
    raise vh.Finding(f"в kaname-config стенда {namespace} нет перечня происхождений access-keys.origins")


def await_operation(edge, session: str, op_id: str, sleep, budget: float = 40.0) -> dict:
    """Опрос операции тем же носителем, что её начал: операцию читает её автор.

    Бутстрап-удостоверение чужую операцию не читает (ответ без `done`), поэтому
    ожидание под ним исчерпывалось, не видя ни исхода, ни отказа."""
    left = budget
    last = ""
    while left > 0:
        code, _, text = edge.ask("GET", f"/operations/{op_id}", cookies={vh.SESSION_COOKIE: session})
        last = f"{code} {vh.message_of(text)}"
        if code == 200:
            d = json.loads(text or "{}")
            if d.get("done"):
                return d
        sleep(0.5)
        left -= 0.5
    raise vh.Finding(f"операция {op_id} не стала done за {budget:.0f} с (последний ответ {last})")


def register_access_key(edge, session: str, user_id: str, origin: str, sleep, name: str) -> str:
    """Церемония регистрации ключа: испытание → аутентификатор → завершение → исход операции."""
    ck = {vh.SESSION_COOKIE: session}
    code, _, text = edge.ask("POST", KEYS_BEGIN.format(uid=user_id), body={}, cookies=ck)
    if code != 200:
        raise vh.Finding(f"испытание регистрации ключа {user_id}: ждали 200, получили {code} {vh.message_of(text)}")
    ch = json.loads(text)
    rp_id = (ch.get("rp") or {}).get("id") or ""
    if not rp_id or not ch.get("challenge"):
        raise vh.Finding("испытание регистрации ключа не назвало доверяющую сторону либо испытание")
    cred = wa.Authenticator().create(wa.b64_any(ch["challenge"]), rp_id, origin)
    code, _, text = edge.ask("POST", KEYS.format(uid=user_id), body={"name": name, "credential": cred}, cookies=ck)
    if code != 200:
        raise vh.Finding(f"регистрация ключа {user_id}: на годный результат церемонии ждали 200, "
                         f"получили {code} {vh.message_of(text)}")
    op = json.loads(text)
    done = await_operation(edge, session, op.get("id") or "", sleep)
    if done.get("error"):
        raise vh.Finding(f"регистрация ключа {user_id}: операция {op.get('id')} не завершилась успехом: "
                         f"{json.dumps((done or {}).get('error') or 'не done', ensure_ascii=False)[:300]}")
    key_id = ((done.get("metadata") or {}).get("accessKeyId")) or ""
    if not key_id:
        raise vh.Finding("регистрация ключа: операция done без accessKeyId в metadata")
    return key_id


def count_keys(edge, session: str, user_id: str) -> int:
    code, _, text = edge.ask("GET", KEYS.format(uid=user_id), cookies={vh.SESSION_COOKIE: session})
    if code != 200:
        raise vh.Finding(f"перечень ключей {user_id}: ждали 200, получили {code} {vh.message_of(text)}")
    return len(json.loads(text or "{}").get("accessKeys") or [])


def level_of(edge, session: str) -> str:
    """Уровень сессии — ответ края «кто я», а не ожидание посева."""
    code, _, text = edge.ask("GET", vh.ME, cookies={vh.SESSION_COOKIE: session})
    if code != 200:
        raise vh.Finding(f"уровень сессии: ждали 200 на {vh.ME}, получили {code} {vh.message_of(text)}")
    return str(((json.loads(text or "{}").get("session")) or {}).get("assuranceLevel"))


def seed(*, edge, human, grant_cloud_admin, sleep, origin: str, password: str, rid: str,
         ceremony, new_password: str) -> dict:
    """Слоты набора. `human(email, kind)` — заведение человека посева (Р17);
    `grant_cloud_admin(user_id)` — выдача и видимость её модели; `ceremony` — модуль
    `seed_ceremony` (сессия уровня «2», вход паролем)."""
    # Администратор облака.
    a = human(f"prodseed-edge-admin-{rid}@example.com")
    grant_cloud_admin(a.user_id)
    a_s2 = ceremony.level2_session(edge, a.bearer)

    # Посторонний и прохожий.
    o = human(f"prodseed-edge-outsider-{rid}@example.com")
    o_s2 = ceremony.level2_session(edge, o.bearer)
    t = human(f"prodseed-edge-bystander-{rid}@example.com")

    # Цель: ключ — под сессией «2», вторая сессия — вход паролем.
    u = human(f"prodseed-edge-target-{rid}@example.com")
    u_s2 = ceremony.level2_session(edge, u.bearer)
    key_id = register_access_key(edge, u_s2, u.user_id, origin, sleep, f"seed-key-{rid}")
    if count_keys(edge, u_s2, u.user_id) != 1:
        raise vh.Finding("после регистрации у цели ключей не ровно один")
    u_s1 = ceremony.password_login(edge, u.email, password)
    for what, ses, level in (("сессия цели «1»", u_s1, "1"), ("сессия цели «2»", u_s2, "2")):
        got = level_of(edge, ses)
        if got != level:
            raise vh.Finding(f"{what}: уровень {got!r}, ждали «{level}»")

    # Ф-н: код письма взят при заведении.
    n = human(f"prodseed-edge-unverified-{rid}@example.com", vh.KIND_UNVERIFIED)
    if len(n.code) != 10:
        raise vh.Finding("у Ф-н посева нет кода письма регистрации — шаг подтверждения исполнить нечем")

    vh.say(f"отказы края по сессии: администратор {a.user_id} (сессия «2»), цель {u.user_id} "
           f"(ключ {key_id}, сессии «1» и «2»), посторонний {o.user_id}, прохожий {t.user_id}, "
           f"Ф-н {n.user_id}")
    return {
        "edgeAdminSession2": a_s2, "edgeAdminUserId": a.user_id,
        "edgeTargetUserId": u.user_id, "edgeTargetSession1": u_s1, "edgeTargetSession2": u_s2,
        "edgeOutsiderSession2": o_s2, "edgeOutsiderUserId": o.user_id,
        "edgeBystanderUserId": t.user_id,
        "edgeUnverifiedSession": n.bearer, "edgeUnverifiedUserId": n.user_id,
        "edgeUnverifiedConfirmCode": n.code,
        "edgeSeedPassword": password, "edgeSeedNewPassword": new_password,
    }
