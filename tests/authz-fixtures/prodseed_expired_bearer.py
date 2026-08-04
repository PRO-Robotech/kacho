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

СРОК УКОРАЧИВАЕТСЯ — У ОДНОГО КЛИЕНТА, ЗАВЕДЁННОГО ПОД ЭТУ ПРОБУ (2026-08-04).
Прежняя редакция этого файла утверждала, что «укоротить срок на один выпуск нельзя, а
править настройку выдающего ради пробы — значит менять посадку стенда всем остальным»,
и выводила отсюда волну на ЧАСЫ с собственным расписанием. Утверждение опровергнуто
ВЫЗОВОМ: у выдающего есть per-client `client_credentials_grant_access_token_lifespan`,
и он действует. `issue_short_lived` ставит его КЛИЕНТУ ЭТОЙ ПРОБЫ (клиент заводит iam,
штатным выпуском ключа), после чего волна идёт десятки секунд. Общая настройка
(`ttl.access_token` выдающего, `KACHO_IAM_SAKEY_ACCESSTOKENTTL` сервиса) НЕ трогается —
менять её значило бы менять посадку всем, и вот это по-прежнему запрещено.

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


def hydra_admin_patch_lifespan(client_id: str, lifespan: str) -> str:
    """Поставить ЭТОМУ клиенту короткий срок предъявителя. Возвращает то, что
    выдающий сохранил (для сверки), либо поднимает StageError.

    ГРАНИЦА ЧЕСТНОСТИ — ГДЕ ОНА ПРОХОДИТ. Подпись и `exp` по-прежнему ставит
    ВЫДАЮЩИЙ, своим ключом и своими часами; здесь меняется ровно одна его
    настройка — срок предъявителя ОДНОГО клиента, заведённого под эту пробу.
    Кейс спрашивает «отвергает ли край СВОЙ СОБСТВЕННЫЙ, корректно подписанный
    предъявитель, у которого прошёл срок», и это утверждение остаётся тем же:
    короче стал срок, а не способ его назначения. Подделкой было бы собрать
    «просроченный» токен харнессом — такой ветки здесь нет.

    ПОЧЕМУ КЛИЕНТ ОБЯЗАН БЫТЬ ЗАВЕДЁН ЧЕРЕЗ iam, а не напрямую у выдающего:
    проверено вызовом (2026-08-04) — выдающий зовёт хук обогащения у iam, и на
    клиенте, которого iam не заводил, хук отвечает отказом, а обмен возвращает
    `access_denied … Token hook responded with HTTP status code: 403`. То есть
    «свой» предъявитель может произвести только штатный продуктовый путь.

    ПОЧЕМУ ИМЕННО `client_credentials_grant_access_token_lifespan`: у выдающего
    версии 26 поля `access_token_lifespan` на клиенте НЕТ — оно принимается и
    молча отбрасывается (проверено вызовом: создание с ним и чтение обратно даёт
    пусто; PATCH по grant-специфичному пути сохраняется). Общая настройка
    (`ttl.access_token` у выдающего, `KACHO_IAM_SAKEY_ACCESSTOKENTTL` у сервиса)
    НЕ трогается: она про всех, а нам нужен один.
    """
    base = os.environ.get("HYDRA_ADMIN_URL", "https://localhost:24445").rstrip("/")
    url = f"{base}/admin/clients/{client_id}"
    body = json.dumps([{"op": "replace",
                        "path": "/client_credentials_grant_access_token_lifespan",
                        "value": lifespan}]).encode()
    req = urllib.request.Request(url, data=body, method="PATCH")
    req.add_header("Content-Type", "application/json")
    ctx = None
    if url.startswith("https://"):
        import ssl  # noqa: PLC0415
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
    try:
        with urllib.request.urlopen(req, timeout=20, context=ctx) as r:
            saved = json.loads(r.read() or b"{}")
    except Exception as e:  # noqa: BLE001 — назвать стадию, а не проглотить
        raise StageError("1б-срок",
                         f"выдающий не принял правку срока для клиента {client_id}: {e}") from e
    got = saved.get("client_credentials_grant_access_token_lifespan")
    if got != lifespan:
        raise StageError("1б-срок",
                         f"выдающий сохранил срок {got!r} вместо {lifespan!r} — "
                         f"поле не действует на этой версии, и ожидание молча стало бы "
                         f"часами. Это отказ, а не повод ждать.")
    return got


def issue_short_lived(pm, sva: str) -> tuple[str, str]:
    """Выпустить предъявителя ШТАТНЫМ путём, но от клиента с КОРОТКИМ сроком.

    Порядок обязателен: ключ выпускает iam (иначе хук обогащения отвергнет обмен),
    срок правится ДО обмена (после обмена он на уже выпущенный токен не влияет),
    и короткость ДОКАЗЫВАЕТСЯ самим токеном, а не тем, что правка «принята».
    """
    lifespan = os.environ.get("EXPIRED_BEARER_LIFESPAN", "10s")
    kr = pm._curl("POST", f"/iam/v1/serviceAccounts/{sva}/keys", pm.boot,
                  {"serviceAccountId": sva, "audience": [pm.API_AUD]})
    done = pm._poll(kr.get("id"), pm.boot)
    if done.get("error"):
        raise StageError("1-выпуск", f"выпуск ключа завершился ошибкой: {done['error']}")
    cid, key, kid = pm.m._extract_oauth(done.get("response", {}))
    if not cid:
        raise StageError("1-выпуск", "в ответе выпуска нет client_id — править срок нечему")

    saved = hydra_admin_patch_lifespan(cid, lifespan)
    print(f"[expired] 1б-срок: клиенту {cid} (заведён iam под ЭТУ пробу) выставлен "
          f"срок предъявителя {saved}; общая настройка выдающего НЕ тронута")

    assertion = pm.m.sign_client_assertion(cid, key, kid, pm.ASSERT_AUD)
    token = pm.m.exchange(pm.HYDRA_TOKEN, assertion, pm.API_AUD)

    # ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ ПО ИСХОДУ, А НЕ ПО ОБЪЯВЛЕНИЮ. «Правка принята» и
    # «токен действительно короткий» — разные утверждения; если бы мы поверили
    # первому, а выдающий поле проигнорировал, волна ушла бы ждать 4 часа, ничего
    # об этом не сказав. Порог — вдвое от заказанного: этого хватает, чтобы
    # отличить секунды от умолчания провайдера, и не хватает, чтобы придраться к
    # округлению.
    cl = claims_of(token)
    exp, iat = cl.get("exp"), cl.get("iat")
    want = int(lifespan.rstrip("s")) if lifespan.endswith("s") and lifespan[:-1].isdigit() else None
    if isinstance(exp, int) and isinstance(iat, int) and want:
        got_life = exp - iat
        if got_life > 2 * want:
            raise StageError(
                "1б-срок",
                f"срок выпущенного предъявителя {got_life}s, а заказан был {want}s — "
                f"правка клиента не подействовала на выпуск. Ждать {got_life}s молча "
                f"было бы подменой предмета: волна обязана создавать своё условие, "
                f"а не смиряться с чужим умолчанием.")
    return token, kid


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
