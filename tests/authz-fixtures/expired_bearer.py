# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""expired_bearer.py — УСЛОВИЕ «ПРЕДЪЯВИТЕЛЬ УЖЕ ИСТЁК» для кейса vpc `AUTHZ-APITOK-EXPIRED-GT-A1`.

КТО ЗОВЁТ. `prodseed_vpc_ext.py` — расширение посева набора vpc, которое
`prodseed_all.py` исполняет для суиты vpc; значение уходит в окружение набора
ключом `apiTokenExpired`. Кейс живёт в `services/vpc/tests/newman/cases/authz-sa-apitoken.py`
(перенесён из набора службы доступа задачей #2912).

ПОЧЕМУ НЕ ПОДДЕЛКА. Подписывает и датирует предъявителя выдающий, своим ключом и
своими часами. Скованный харнессом «просроченный токен» проверял бы, что край
отвергает ЧУЖУЮ подпись, а кейс спрашивает другое: отвергает ли край СВОЙ
корректно подписанный предъявитель, у которого прошёл срок. Поэтому условие
создаётся штатным путём продукта: выпуск ключа служебной учётки с коротким
`ttlSeconds` → утверждение клиента → обмен у издателя платформы → ожидание по
стенным часам до `exp` плюс запас.

ПОЧЕМУ `ttlSeconds` — НАША РУЧКА. Выпуск издателя платформы не отдаёт токен,
переживающий свой ключ: срок токена — минимум из объявленного посадкой и остатка
жизни ключа. Заказанный на выпуске срок ключа и есть срок этого одного
предъявителя; общая настройка срока токенов посадки не трогается — она про всех.

ПРЕДПОСЫЛКИ ПРОВЕРЯЮТСЯ ПО ИСХОДУ, СТАДИЯМИ:
  1-выпуск   — ключ выпущен без ошибки, обмен вернул предъявителя с числовым `exp`;
  1б-срок    — `exp - iat` не больше удвоенного заказанного: иначе срез не подействовал,
               и молча ждать умолчание посадки было бы подменой предмета;
  2-контроль — СВЕЖИЙ предъявитель ПРИНЯТ краем: без этого отказ после ожидания
               неотличим от «не работал никогда»;
  3-ожидание — настоящие стенные часы до `exp + SKEW_S`;
  4-край     — истёкший предъявитель обязан получить `401`. Иное — НАХОДКА О ПРОДУКТЕ,
               а не о посеве: предъявитель тогда всё равно отдаётся кейсу, и кейс
               краснеет утверждением, а не пропадает в «условие не создано».

ИСХОДЫ ФУНКЦИИ `mint`: предъявитель (строка) — условие создано (либо стадия 4 дала
находку, названную в журнале); исключение `StageError` — условие НЕ создано, стадия
названа. Вызывающий не отдаёт ключ окружению, и кейс сам скажет «условие не
создано» стражем своего субъекта — третьей категорией, а не вердиктом о продукте.

Ручки: `EXPIRED_BEARER_TTL_S` (заказываемый срок ключа, 20 с), `EXPIRED_BEARER_SKEW_S`
(запас поверх `exp`, 30 с). Журнал — в stderr: stdout расширения посева несёт только
JSON фикстур.
"""
from __future__ import annotations

import base64
import json
import os
import sys
import time
import urllib.error
import urllib.request


class StageError(RuntimeError):
    def __init__(self, stage: str, msg: str):
        super().__init__(f"[{stage}] {msg}")
        self.stage = stage


def log(msg: str) -> None:
    print(f"[expired] {msg}", file=sys.stderr, flush=True)


def claims_of(token: str) -> dict:
    """Claims of a JWS without verifying it — only `exp`/`iat` are read; the edge verifies."""
    if not isinstance(token, str) or token.count(".") < 2:
        return {}
    payload = token.split(".")[1]
    payload += "=" * (-len(payload) % 4)
    try:
        return json.loads(base64.urlsafe_b64decode(payload))
    except (ValueError, json.JSONDecodeError):
        return {}


def probe_edge(base: str, token: str, path: str) -> int:
    """HTTP status the edge answers for this bearer. Never raises on 4xx/5xx."""
    req = urllib.request.Request(base + path, method="GET")
    req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code
    except (urllib.error.URLError, OSError) as e:
        raise StageError("край", f"край недостижим: {e}") from e


def mint(pm, account_id: str, *, probe_path: str = "/iam/v1/accounts?pageSize=1") -> str:
    """Выпустить настоящий предъявитель, переждать его срок и вернуть его.

    `pm` — модуль `prodseed_matrix` (его помощники выпуска и обмена); учётка
    заводится своя, а не берётся у матрицы: предъявитель обязан истечь, и
    переиспользование действующего субъекта оставило бы соседним суитам
    просроченный слот.
    """
    want = int(os.environ.get("EXPIRED_BEARER_TTL_S", "20"))
    skew = int(os.environ.get("EXPIRED_BEARER_SKEW_S", "30"))

    sva = pm.make_sa(account_id, f"ps-apitok-exp-{pm.RID}")
    if not sva:
        raise StageError("1-выпуск", "служебная учётка не заведена")
    kr = pm._curl("POST", f"/iam/v1/serviceAccounts/{sva}/keys", pm.boot,
                  {"serviceAccountId": sva, "audience": [pm.API_AUD], "ttlSeconds": want})
    done = pm._poll(kr.get("id"), pm.boot)
    if not done:
        raise StageError("1-выпуск", f"операция выпуска ключа не дошла до done: {kr}")
    if done.get("error"):
        raise StageError("1-выпуск", f"выпуск ключа завершился ошибкой: {done['error']}")
    _, key, client_id = pm.m._extract_oauth(done.get("response", {}))
    if not client_id:
        raise StageError("1-выпуск", "в ответе выпуска нет идентификатора клиента — "
                                     "подписывать утверждение нечем")
    assertion = pm.m.sign_client_assertion(
        client_id, key, client_id, pm.PLATFORM_ASSERT_AUD,
        token_type=pm.m.CLIENT_ASSERTION_TOKEN_TYPE)
    token = pm.m.exchange_at_platform(pm.PLATFORM_TOKEN_URL, assertion, pm.API_AUD)

    cl = claims_of(token)
    exp, iat = cl.get("exp"), cl.get("iat")
    if not isinstance(exp, int):
        raise StageError("1-выпуск", "у выпущенного предъявителя нет числового `exp` — "
                                     "ждать нечего и утверждать нечего")
    if isinstance(iat, int) and exp - iat > 2 * want:
        raise StageError(
            "1б-срок",
            f"срок выпущенного предъявителя {exp - iat}s, а заказан был {want}s — срез по "
            f"остатку жизни ключа не подействовал; ждать чужое умолчание молча нельзя")
    log(f"1-выпуск: предъявитель получен, iat={iat} exp={exp} (срок назначает выдающий)")

    code = probe_edge(pm.PUBLIC, token, probe_path)
    if code == 401:
        raise StageError("2-контроль", "свежий предъявитель отвергнут краем (401) ещё до "
                                       "ожидания — отказ после ожидания доказывал бы поломку "
                                       "выпуска, а не истечение срока")
    log(f"2-контроль: свежий предъявитель принят краем ({code})")

    target = exp + skew
    started = time.time()
    while time.time() < target:
        time.sleep(min(10.0, max(1.0, target - time.time())))
    log(f"3-ожидание: прошло {time.time() - started:.0f}s по стенным часам (до exp+{skew}s)")

    code = probe_edge(pm.PUBLIC, token, probe_path)
    if code != 401:
        log(f"4-край: НАХОДКА О ПРОДУКТЕ — край принял предъявителя с прошедшим сроком ({code}); "
            f"предъявитель отдаётся кейсу, и кейс покраснеет утверждением")
    else:
        log("4-край: истёкший предъявитель отвергнут краем (401)")
    return token
