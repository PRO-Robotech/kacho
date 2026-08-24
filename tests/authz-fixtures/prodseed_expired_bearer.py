#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""prodseed_expired_bearer.py — ПОСЕВ ВОЛНЫ ИСТЁКШЕГО ПРЕДЪЯВИТЕЛЯ.

Создаёт условие, которого машинный посев создать не мог: предъявителя, чей срок УЖЕ
истёк. Способ ровно один и он не является подделкой — ВЫПУСТИТЬ настоящий предъявитель
штатным продуктовым путём (`SAKeyService.Issue` → client_assertion → обмен у выдающего)
и ПЕРЕЖДАТЬ его срок по стенным часам.

ПОЧЕМУ НЕ ПОДДЕЛКА И ПОЧЕМУ ЭТО ВАЖНО. Подписывает предъявителя выдающий, своим ключом.
Скованный харнессом «просроченный токен» проверял бы, что край отвергает ЧУЖУЮ подпись, —
а кейс спрашивает другое: отвергает ли край СВОЙ СОБСТВЕННЫЙ, корректно подписанный
предъявитель, у которого прошёл срок. Это разные утверждения, и второе — то, ради чего
кейс существует.

СКОЛЬКО ЖДАТЬ — БЕРЁТСЯ У САМОГО ПРЕДЪЯВИТЕЛЯ, А НЕ ИЗ КОНСТАНТЫ.
Ожидание вычисляется из поля `exp` выпущенного токена. Литеральное число здесь было бы
ложью с коротким сроком годности, и это не гипотеза: в дереве такое число уже жило и уже
разошлось со стендом — два места объявляли срок предъявителя равным 900 с, тогда как
замер давал 14400 с.

СРОК ЗАКАЗЫВАЕТСЯ ПРИ ВЫПУСКЕ КЛЮЧА (задача #1120).
Ключ служебной учётки обменивается только у НАШЕГО издателя, а наш выпуск не отдаёт
токен, переживающий свой ключ: срок токена — минимум из объявленного посадкой и остатка
жизни ключа. Поэтому `ttlSeconds` на выпуске и есть ручка короткого предъявителя, волна
идёт десятки секунд, и никакой посторонней настройки трогать не требуется.

ЗДЕСЬ БЫЛА ПРАВКА СРОКА У ПРЕЖНЕГО ИЗДАТЕЛЯ — административным вызовом его клиенту.
Клиента у него больше не заводится вовсе, поэтому править нечего и незачем. Общая
настройка (`authn.client-token.token-ttl`) НЕ трогается: она про всех, а нужен один.

ЧТО ОСТАЛОСЬ НЕПРИКОСНОВЕННЫМ: способ назначения срока. Подписывает и датирует
выдающий; харнесс не собирает «просроченный» токен и не двигает часы. Кейс спрашивает,
отвергает ли край СВОЙ СОБСТВЕННЫЙ корректно подписанный предъявитель после срока, — и
спрашивает ровно это.

ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ ПО ИСХОДУ. Если правка клиента не подействует, `exp - iat`
останется умолчанием выдающего, и волна ушла бы ждать часы, ничего не сказав. Поэтому
короткость доказывается САМИМ токеном, а расхождение — отказ с названной стадией, а не
молчаливое долгое ожидание.

ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ОБЯЗАТЕЛЕН И СНИМАЕТСЯ ДО ОЖИДАНИЯ. Сразу после выпуска тот же
предъявитель предъявляется краю и обязан быть ПРИНЯТ. Без этого «отвергнут» после
ожидания неотличимо от «не работал никогда»: неверная аудитория, неудавшийся обмен,
пустая строка — каждая из этих поломок даёт тот же отказ и выдала бы себя за исполненный
инвариант.

Запуск:  SETUP_NS=kacho python3 tests/authz-fixtures/prodseed_expired_bearer.py
         SKEW_S=30            — запас поверх `exp` (по умолчанию 30 с)
         EXPIRED_BEARER_TTL_S — срок заказываемого ключа в секундах (по умолчанию 20)
         DRY_PROBE=1          — САМОПРОВЕРКА: выпустить, снять положительный контроль и
                                выйти БЕЗ ожидания и БЕЗ записи в окружение. Доказывает,
                                что путь до края живой и что проба читает настоящий
                                вердикт; инвариант при этом НЕ объявляется проверенным.
Коды:    0 — предъявитель истёк и записан в окружение (либо самопроверка прошла)
         2 — стадия не прошла (стадия названа в сообщении)
"""
from __future__ import annotations

import base64
import json
import os
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

_HERE = os.path.dirname(os.path.abspath(__file__))
_ROOT = os.path.abspath(os.path.join(_HERE, "..", ".."))
sys.path.insert(0, _HERE)

ENV_FILES = ["services/iam/tests/newman/environments/local.postman_environment.json"]
SKEW_S = int(os.environ.get("SKEW_S", "30"))
DRY_PROBE = os.environ.get("DRY_PROBE", "") not in ("", "0", "false")


class StageError(RuntimeError):
    def __init__(self, stage: str, msg: str):
        super().__init__(f"[{stage}] {msg}")
        self.stage = stage


def claims_of(token: str) -> dict:
    """Claims of a JWS without verifying it — we only need `exp`, and the edge is the
    one that verifies. Returns {} when the string is not a JWS at all."""
    if not isinstance(token, str) or token.count(".") < 2:
        return {}
    payload = token.split(".")[1]
    payload += "=" * (-len(payload) % 4)
    try:
        return json.loads(base64.urlsafe_b64decode(payload))
    except Exception:
        return {}


def env_value(key: str) -> str:
    """One value from the suite env written by the matrix seed."""
    path = os.path.join(_ROOT, ENV_FILES[0])
    if not os.path.exists(path):
        return ""
    try:
        with open(path, encoding="utf-8") as fh:
            for v in (json.load(fh) or {}).get("values", []):
                if v.get("key") == key:
                    return str(v.get("value") or "")
    except Exception:
        return ""
    return ""


def probe_edge(token: str, path: str) -> int:
    """HTTP status the edge answers for this bearer. Never raises on 4xx/5xx."""
    base = os.environ.get("PUBLIC_BASE", "http://localhost:18080")
    req = urllib.request.Request(base + path, method="GET")
    req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code
    except Exception as e:  # noqa: BLE001 — сеть/порт: назвать стадию, а не проглотить
        raise StageError("3-край", f"край недостижим: {e}") from e


def write_env(values: dict) -> None:
    written = 0
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as fh:
        json.dump(values, fh)
        fixtures_path = fh.name
    try:
        for rel in ENV_FILES:
            path = os.path.join(_ROOT, rel)
            if not os.path.exists(path):
                raise StageError("5-окружение",
                                 f"нет файла окружения {rel} — его пишет посев фикстур")
            rc = subprocess.run(
                [sys.executable, os.path.join(_HERE, "patch-env.py"), fixtures_path, path],
                text=True, capture_output=True)
            if rc.returncode != 0:
                raise StageError("5-окружение", f"patch-env упал на {rel}: {rc.stderr[:300]}")
            written += 1
    finally:
        os.unlink(fixtures_path)
    print(f"[expired] 5-окружение: обновлено файлов {written}, ключей {len(values)}")


def issue_short_lived(pm, sva: str) -> tuple[str, str]:
    """Выпустить предъявителя ШТАТНЫМ путём с КОРОТКИМ сроком.

    СРОК ЗАКАЗЫВАЕТСЯ ПРИ ВЫПУСКЕ КЛЮЧА, а не правится потом у постороннего
    (задача #1120). Наш выпуск не отдаёт токен, переживающий свой ключ: срок
    токена — минимум из объявленного посадкой и остатка жизни ключа
    (`client_token.Issue`, шаг «срок клиента»). Значит `ttlSeconds` на выпуске
    ключа и есть ручка короткого предъявителя, и ручка эта НАША.

    ЧТО ЗДЕСЬ БЫЛО. Срок правился административным вызовом прежнему издателю —
    его клиенту, заведённому под эту пробу. Клиента у него больше не заводится
    вовсе, поэтому и править нечего: полоса обмена ключа служебной учётки одна, и
    она наша.

    ГРАНИЦА ЧЕСТНОСТИ ГДЕ БЫЛА, ТАМ И ОСТАЛАСЬ. Подпись и `exp` ставит ВЫПУСКАЮЩИЙ,
    своим ключом и своими часами; харнесс «просроченный» токен не собирает и часы
    не двигает. Кейс спрашивает, отвергает ли край СВОЙ СОБСТВЕННЫЙ, корректно
    подписанный предъявитель после срока, — и спрашивает ровно это.

    ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ ПО ИСХОДУ, А НЕ ПО ОБЪЯВЛЕНИЮ: если бы срез не
    подействовал, `exp - iat` осталось бы объявлением посадки (минуты), и волна
    ушла бы ждать их молча. Порог — вдвое от заказанного: этого хватает, чтобы
    отличить секунды от умолчания, и не хватает, чтобы придраться к округлению.
    """
    want = int(os.environ.get("EXPIRED_BEARER_TTL_S", "20"))
    kr = pm._curl("POST", f"/iam/v1/serviceAccounts/{sva}/keys", pm.boot,
                  {"serviceAccountId": sva, "audience": [pm.API_AUD],
                   "ttlSeconds": want})
    done = pm._poll(kr.get("id"), pm.boot)
    if done.get("error"):
        raise StageError("1-выпуск", f"выпуск ключа завершился ошибкой: {done['error']}")
    _, key, registry_client_id = pm.m._extract_oauth(done.get("response", {}))
    if not registry_client_id:
        raise StageError("1-выпуск", "в ответе выпуска нет идентификатора клиента — "
                                     "подписывать утверждение нечем")
    print(f"[expired] 1б-срок: ключу {registry_client_id} заказан срок {want}s; "
          f"объявление посадки НЕ тронуто — оно про всех, а нужен один")

    assertion = pm.m.sign_client_assertion(
        registry_client_id, key, registry_client_id, pm.PLATFORM_ASSERT_AUD,
        token_type=pm.m.CLIENT_ASSERTION_TOKEN_TYPE)
    token = pm.m.exchange_at_platform(pm.PLATFORM_TOKEN_URL, assertion, pm.API_AUD)

    cl = claims_of(token)
    exp, iat = cl.get("exp"), cl.get("iat")
    if isinstance(exp, int) and isinstance(iat, int):
        got_life = exp - iat
        if got_life > 2 * want:
            raise StageError(
                "1б-срок",
                f"срок выпущенного предъявителя {got_life}s, а заказан был {want}s — "
                f"срез по остатку жизни ключа не подействовал. Ждать {got_life}s молча "
                f"было бы подменой предмета: волна обязана создавать своё условие, "
                f"а не смиряться с чужим умолчанием.")
    return token, registry_client_id


def main() -> int:
    try:
        # ── 1. выпуск настоящего предъявителя штатным путём ───────────────────
        # Импорт матричного посева чеканит бутстрап-предъявитель на импорте (так он
        # устроен и это задокументировано у него) — поэтому импорт стоит ВНУТРИ main,
        # чтобы `--help`-подобное чтение файла не дёргало стенд.
        acct = env_value("accountAId")
        if not acct:
            raise StageError("1-выпуск",
                             "в окружении нет `accountAId` — матричный посев ещё не "
                             "отработал (tests/authz-fixtures/setup.sh). Заводить свой "
                             "аккаунт здесь нельзя: он принадлежит человеку by construction.")

        import prodseed_matrix as pm  # noqa: PLC0415

        # СВОЯ служебная учётка, а не чужая: предъявитель этой волны обязан истечь, и
        # переиспользование действующего субъекта матрицы означало бы, что после волны
        # у соседних суит окажется просроченный слот.
        sva = pm.make_sa(acct, f"ps-apitok-exp-{pm.RID}")
        token, _kid = issue_short_lived(pm, sva)
        if not token:
            raise StageError("1-выпуск", "обмен не вернул предъявителя")

        cl = claims_of(token)
        exp, iat = cl.get("exp"), cl.get("iat")
        if not isinstance(exp, int):
            raise StageError("1-выпуск",
                             "у выпущенного предъявителя нет числового `exp` — ждать нечего "
                             "и утверждать нечего")
        life = (exp - iat) if isinstance(iat, int) else None
        print(f"[expired] 1-выпуск: предъявитель получен; iat={iat} exp={exp} "
              f"срок={life}s (СРОК НАЗНАЧАЕТ ВЫДАЮЩИЙ, здесь он только прочитан)")

        # ── 2. положительный контроль ДО ожидания ────────────────────────────
        probe_path = os.environ.get("PROBE_PATH", "/iam/v1/accounts?pageSize=1")
        code = probe_edge(token, probe_path)
        if code == 401:
            raise StageError("2-контроль",
                             "свежий предъявитель отвергнут краем (401) ЕЩЁ ДО ожидания — "
                             "значит последующий отказ доказывал бы поломку выпуска, "
                             "а не истечение срока")
        print(f"[expired] 2-контроль: свежий предъявитель ПРИНЯТ краем ({code}) — "
              f"отказ после ожидания будет говорить именно об истечении")

        if DRY_PROBE:
            print("[expired] САМОПРОВЕРКА (DRY_PROBE=1): путь до края живой, проба читает "
                  "настоящий вердикт. Ожидание НЕ выполнялось, окружение НЕ трогалось, "
                  "инвариант НЕ объявляется проверенным.")
            return 0

        # ── 3. НАСТОЯЩЕЕ ожидание по стенным часам ───────────────────────────
        target = exp + SKEW_S
        started = time.time()
        need = target - started
        print(f"[expired] 3-ожидание: ждать {need:.0f}s (до exp+{SKEW_S}s). "
              f"Это НАСТОЯЩИЕ стенные часы: срок предъявителя нельзя ни укоротить, "
              f"ни подделать, поэтому волна идёт столько, сколько живёт токен.")
        while time.time() < target:
            left = target - time.time()
            time.sleep(min(30.0, max(1.0, left)))
            if int(left) % 300 < 30:
                print(f"[expired]   … осталось {left:.0f}s", flush=True)
        waited = time.time() - started
        print(f"[expired] 3-ожидание: ФАКТИЧЕСКИ прошло {waited:.0f}s "
              f"(объявленный срок {life}s + запас {SKEW_S}s)")

        # ── 4. предъявитель обязан быть отвергнут ────────────────────────────
        code = probe_edge(token, probe_path)
        print(f"[expired] 4-край: истёкший предъявитель → {code}")
        if code != 401:
            raise StageError("4-край",
                             f"край принял предъявителя с прошедшим сроком ({code}) — "
                             f"это НАХОДКА О ПРОДУКТЕ, а не о посеве")

        # ── 5. запись в окружение ────────────────────────────────────────────
        write_env({"apiTokenExpired": token})
        print(f"[expired] ГОТОВО: `apiTokenExpired` — настоящий предъявитель, "
              f"переждавший свой срок ({waited:.0f}s по часам)")
        return 0
    except StageError as e:
        print(f"===== ПОСЕВ ИСТЁКШЕГО ПРЕДЪЯВИТЕЛЯ НЕ ПРОШЁЛ =====\n{e}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
