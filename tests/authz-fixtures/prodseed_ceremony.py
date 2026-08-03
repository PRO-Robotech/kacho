#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""prodseed_ceremony.py — ПОСЕВ ВОЛНЫ ЦЕРЕМОНИИ: предъявитель, принадлежащий человеку.

Это артефакт, которого ждёт `ceremony_credentials.CEREMONY_SEED`. Пока его не было,
волна церемонии не могла создать своё условие и честно печатала открытый долг с числом
(`ceremony_credentials.py --debt`), а восемь коллекций шли под МАШИННЫМ принципалом —
то есть отвечали не на тот вопрос, который кейс задаёт.

ЧТО ЗДЕСЬ ПРОИСХОДИТ НА САМОМ ДЕЛЕ, И ГДЕ ГРАНИЦА ЧЕСТНОСТИ
------------------------------------------------------------
Пароль проверяет ПРОВАЙДЕР, а не этот скрипт. Стадия 5 выполняет настоящий вход
паролем через нативный поток провайдера личности; если пароль неверен, поток
отвечает отказом и посев встаёт. Только ПОСЛЕ того, как провайдер подтвердил
пароль, стадия 7 подтверждает запрос входа у выдающего токены — то есть скрипт
играет роль консоли входа, а не роль проверяющего пароль. Это штатная форма Ory:
консоль входа — твоё приложение, оно аутентифицирует человека и затем подтверждает
запрос административной ручкой. Подмены здесь нет; подменой было бы подтвердить
запрос входа, НЕ спросив пароль, и такой ветки в этом файле не существует.

ПОЧЕМУ КОНСОЛЬЮ РАБОТАЕТ ЭТОТ СКРИПТ, А НЕ РАЗВЁРНУТАЯ КОНСОЛЬ
---------------------------------------------------------------
Развёрнутый провайдер личности на этом стенде не умеет участвовать в передаче
запроса входа: в смонтированной настройке нет блока, объявляющего адрес выдающего
токены, и обращение с параметром запроса входа он отвергает своей же диагностикой
(«отказываюсь разбирать … не задан»). Это НЕ предположение — проверено вызовом.
Поэтому подтверждение идёт административной ручкой выдающего, а вход паролем —
нативным потоком провайдера, который в этой настройке работает.

РАСХОЖДЕНИЕ ЧАРТА СО СТЕНДОМ, КОТОРОЕ ЗДЕСЬ ПРИХОДИТСЯ ОБХОДИТЬ (стадия 6)
---------------------------------------------------------------------------
В репозитории объявлен хук провижининга на событиях входа и регистрации — он
зеркалит личность человека в iam. На стенде смонтирована ДРУГАЯ настройка
провайдера, в которой этого хука нет вовсе. Поэтому зеркало здесь создаётся явным
вызовом, и стадия 6 об этом ГОВОРИТ вслух: иначе следующий читатель решит, что хук
отработал, хотя он не мог отработать. Когда смонтированная настройка догонит чарт,
стадия 6 станет избыточной — и обязана быть снята, а не оставлена «на всякий
случай».

ЧТО ЗАПОЛНЯЕТСЯ, А ЧТО ОСТАЁТСЯ ДОЛГОМ С ЧИСЛОМ
------------------------------------------------
Заполняется: предъявитель человека, вошедшего паролем (уровень 1), и предъявитель
того же человека с поднятым уровнем (уровень 2), плюс их идентификаторы.

НЕ заполняется `apiTokenExpired` — предъявитель с УЖЕ истёкшим сроком. Срок
назначает и подписывает выдающий; на этом стенде срок предъявителя — часы, а
укоротить его на один выпуск нельзя. Подделать — значит вернуться ровно к тому, от
чего волна и уводит. Поэтому это остаётся ОТКРЫТЫМ ДОЛГОМ С ЧИСЛОМ (приёмка
IAM-INT-1, сценарий 33), и посев печатает его числом, а не умалчивает.

Запуск:  SETUP_NS=kacho python3 tests/authz-fixtures/prodseed_ceremony.py
Коды:    0 — предъявители получены и проверены краем
         2 — стадия не прошла (стадия названа в сообщении)
"""
from __future__ import annotations

import base64
import hashlib
import json
import os
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import mint_rs256 as m  # noqa: E402

# ─── адреса ──────────────────────────────────────────────────────────────────
PUBLIC = os.environ.get("BASE_URL", "http://localhost:18080")
INTERNAL = os.environ.get("INTERNAL_BASE_URL", "http://localhost:18081")
KRATOS_PUBLIC = os.environ.get("KRATOS_PUBLIC_URL", "http://localhost:24433")
KRATOS_ADMIN = os.environ.get("KRATOS_ADMIN_URL", "http://localhost:24434")
HYDRA_PUBLIC = os.environ.get("HYDRA_PUBLIC_URL", "http://localhost:24444")
HYDRA_ADMIN = os.environ.get("HYDRA_ADMIN_URL", "https://localhost:24445")
API_AUDIENCE = os.environ.get("API_AUDIENCE", "https://api.kacho.cloud")
REDIRECT_URI = os.environ.get("CEREMONY_REDIRECT_URI", "https://api.kacho.cloud/auth/callback")

# Выдающий объявляет себя адресом, который с хоста не резолвится, и КАЖДЫЙ его
# `redirect_to` несёт этот префикс. Переписываем на адрес, по которому он реально
# отвечает. Это свойство стенда (проброс портов), а не продукта.
ISSUER_PREFIX = os.environ.get(
    "HYDRA_ISSUER_PREFIX", "https://localhost:28080/.ory/hydra/public")

# Тот же разрыв у провайдера личности: его публичный базовый адрес — путь под
# ingress, а мы ходим прямо в под.
KRATOS_PATH_PREFIX = os.environ.get("KRATOS_PATH_PREFIX", "/.ory/kratos/public")

RID = str(int(time.time()))
PASSWORD = "Ceremony-" + secrets.token_urlsafe(18)

ENV_FILES = [
    "services/iam/tests/newman/environments/local.postman_environment.json",
]

# Корень монорепо — ДВА уровня вверх от `tests/authz-fixtures/`, а не один.
# Промах на один уровень здесь не остался немым только потому, что стадия 9
# ПРОВЕРЯЕТ существование файла и называет путь; без такой проверки посев
# «успешно» писал бы окружение мимо цели, и волна читала бы пустые слоты.
_ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))


class StageError(RuntimeError):
    """Отказ с НАЗВАННОЙ стадией — сценарий 27 требует именно этого."""

    def __init__(self, stage: str, detail: str):
        super().__init__(f"[стадия {stage}] {detail}")
        self.stage = stage


def _req(method: str, url: str, *, body=None, headers=None, timeout=20,
         allow_redirects=False, insecure=False, jar=None):
    """Возвращает (код, заголовки, тело-как-текст). Редиректы НЕ следуются по
    умолчанию: в потоке кода вся суть — в `Location`, и проглотить его значит
    потерять предмет."""
    data = None
    hdrs = dict(headers or {})
    if body is not None:
        data = json.dumps(body).encode()
        hdrs.setdefault("Content-Type", "application/json")
    ctx = None
    if insecure or url.startswith("https://"):
        import ssl
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
    # Cookie переносим ВРУЧНУЮ, а не штатной банкой, и это не каприз: выдающий
    # объявляет себя по https, поэтому его cookie сессии помечены `Secure`, а мы
    # ходим к нему по проброшенному порту открытым текстом — штатная банка такие
    # cookie не отдаёт обратно by construction, и защита от межсайтовой подделки
    # отвергает согласие раньше, чем его успевают подтвердить. Признак `Secure`
    # здесь описывает канал БРАУЗЕРА, а канал здесь — проброс до пода.
    if jar is not None and jar:
        hdrs["Cookie"] = "; ".join(f"{k}={v}" for k, v in jar.items())
    req = urllib.request.Request(url, data=data, headers=hdrs, method=method)

    class _NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *a, **kw):
            return None

    opener_args = [] if allow_redirects else [_NoRedirect]
    if ctx is not None:
        opener_args.append(urllib.request.HTTPSHandler(context=ctx))
    opener = urllib.request.build_opener(*opener_args)
    def _absorb(headers):
        if jar is None:
            return
        for raw in headers.get_all("Set-Cookie") or []:
            first = raw.split(";", 1)[0].strip()
            if "=" in first:
                k, v = first.split("=", 1)
                jar[k] = v

    try:
        with opener.open(req, timeout=timeout) as resp:
            _absorb(resp.headers)
            return resp.status, dict(resp.headers), resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        _absorb(e.headers)
        return e.code, dict(e.headers), e.read().decode("utf-8", "replace")
    except Exception as e:  # noqa: BLE001 — сетевой отказ обязан назвать адрес
        raise StageError("транспорт", f"{method} {url}: {e}") from None


def _post_form(url: str, form: dict, timeout=20):
    data = urllib.parse.urlencode(form).encode()
    req = urllib.request.Request(
        url, data=data, method="POST",
        headers={"Content-Type": "application/x-www-form-urlencoded"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, json.loads(resp.read() or b"{}")
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")
        try:
            return e.code, json.loads(raw or "{}")
        except ValueError:
            return e.code, {"raw": raw}


def _kratos_url(action: str) -> str:
    """Адрес отправки формы, приведённый к тому, по которому мы реально ходим.

    Провайдер личности объявляет свой публичный базовый адрес ОТНОСИТЕЛЬНЫМ путём
    под префиксом входа (`serve.public.base_url`), поэтому в потоке приезжает не
    «куда постучаться», а «куда постучался бы браузер через ingress». Мы ходим
    прямо в под через проброс, где этого префикса нет.

    Обрабатываются обе формы — относительная и абсолютная: какая именно придёт,
    зависит от настройки, а не от нашего кода, и молча не сработать здесь значит
    встать на середине церемонии.
    """
    path = action
    if "://" in action:
        parsed = urllib.parse.urlparse(action)
        path = parsed.path + (f"?{parsed.query}" if parsed.query else "")
    for prefix in (KRATOS_PATH_PREFIX, "/.ory/kratos/public"):
        if prefix and path.startswith(prefix):
            path = path[len(prefix):]
            break
    if not path.startswith("/"):
        path = "/" + path
    return KRATOS_PUBLIC.rstrip("/") + path


def _jwt_payload(tok: str) -> dict:
    part = tok.split(".")[1]
    part += "=" * (-len(part) % 4)
    return json.loads(base64.urlsafe_b64decode(part))


# ─── стадии ──────────────────────────────────────────────────────────────────
def stage_preflight() -> None:
    checks = [
        ("провайдер личности (публичный)", f"{KRATOS_PUBLIC}/health/ready"),
        ("провайдер личности (админ)", f"{KRATOS_ADMIN}/admin/health/ready"),
        ("выдающий токены (публичный)", f"{HYDRA_PUBLIC}/health/ready"),
        ("выдающий токены (админ)", f"{HYDRA_ADMIN}/health/ready"),
    ]
    for what, url in checks:
        code, _, _ = _req("GET", url, timeout=10)
        if code != 200:
            raise StageError("1-преполёт", f"{what} по {url} отвечает {code}, а не 200")
    print(f"[ceremony] 1-преполёт: 4 из 4 адресов отвечают")


def stage_admin_token() -> str:
    try:
        tok = m.mint_bootstrap()
    except Exception as e:  # noqa: BLE001
        raise StageError("2-администратор", str(e)) from None
    print("[ceremony] 2-администратор: получен предъявитель уровня кластера")
    return tok


def stage_interactive_client(admin: str) -> tuple[str, str]:
    """Клиент интерактивного входа — через ШТАТНУЮ ручку продукта (S1), не мимо неё.

    Существующий переиспользуется: клиент — ресурс с уникальным именем, и плодить
    по одному на прогон значит засорять выдающего.
    """
    code, _, body = _req("GET", f"{INTERNAL}/iam/v1/internal/interactiveClients",
                         headers={"Authorization": f"Bearer {admin}"})
    if code == 200:
        for c in (json.loads(body or "{}").get("interactiveClients") or []):
            if c.get("status") == "ACTIVE" and c.get("clientId"):
                print(f"[ceremony] 3-клиент: переиспользован {c['id']}")
                return c["id"], c["clientId"]
    name = f"ceremony-{RID}"
    code, _, body = _req("POST", f"{INTERNAL}/iam/v1/internal/interactiveClients",
                         headers={"Authorization": f"Bearer {admin}"},
                         body={"name": name, "redirectUris": [REDIRECT_URI],
                               "description": "ceremony wave bearer producer"})
    if code != 200:
        raise StageError("3-клиент", f"Create отвечает {code}: {body[:400]}")
    op = json.loads(body or "{}")
    ic_id = (op.get("metadata") or {}).get("interactiveClientId")
    if not ic_id:
        raise StageError("3-клиент", f"в metadata нет interactiveClientId: {body[:300]}")
    # Идентификатор из `metadata` пре-аллоцирован и приезжает ДАЖЕ на упавшей
    # операции — читать его, не проверив исход, значит выдать фантом (класс
    # известен, testing.md). Поэтому дожидаемся ресурса, а не доверяем metadata.
    deadline = time.time() + 60
    while time.time() < deadline:
        code, _, body = _req("GET", f"{INTERNAL}/iam/v1/internal/interactiveClients/{ic_id}",
                             headers={"Authorization": f"Bearer {admin}"})
        if code == 200:
            c = json.loads(body or "{}")
            if c.get("clientId"):
                print(f"[ceremony] 3-клиент: заведён {ic_id}")
                return ic_id, c["clientId"]
        time.sleep(2)
    raise StageError("3-клиент", f"{ic_id} не стал читаемым за 60с — Create не довёл")


def stage_identity() -> tuple[str, str]:
    email = f"ceremony-{RID}@kacho.local"
    code, _, body = _req(
        "POST", f"{KRATOS_ADMIN}/admin/identities",
        body={"schema_id": "default",
              "traits": {"email": email, "name": {"first": "Ceremony", "last": "Human"}},
              "credentials": {"password": {"config": {"password": PASSWORD}}}})
    if code not in (200, 201):
        raise StageError("4-личность", f"создание личности отвечает {code}: {body[:300]}")
    ident = json.loads(body or "{}")
    print(f"[ceremony] 4-личность: заведена {ident['id']}")
    return ident["id"], email


def stage_password_login(email: str) -> str:
    """НАСТОЯЩИЙ вход паролем. Пароль проверяет провайдер; неверный — отказ."""
    code, _, body = _req("GET", f"{KRATOS_PUBLIC}/self-service/login/api",
                         headers={"Accept": "application/json"})
    if code != 200:
        raise StageError("5-вход", f"инициализация потока входа отвечает {code}: {body[:300]}")
    flow = json.loads(body or "{}")
    action = (flow.get("ui") or {}).get("action")
    if not action:
        raise StageError("5-вход", "поток входа не назвал адрес отправки")
    action = _kratos_url(action)
    code, _, body = _req("POST", action,
                         headers={"Accept": "application/json"},
                         body={"method": "password", "identifier": email,
                               "password": PASSWORD})
    if code != 200:
        raise StageError("5-вход", f"вход паролем отвергнут ({code}): {body[:300]}")
    sess = json.loads(body or "{}")
    ident_id = ((sess.get("session") or {}).get("identity") or {}).get("id")
    if not ident_id:
        raise StageError("5-вход", "вход прошёл, но сессия не назвала личность")
    print(f"[ceremony] 5-вход: пароль ПРОВЕРЕН провайдером, сессия у {ident_id}")
    return ident_id


def stage_mirror(admin: str, ext_id: str, email: str) -> str:
    """Зеркало человека в iam.

    ЯВНО, а не хуком: смонтированная на стенде настройка провайдера хука
    провижининга не несёт (в чарте он объявлен — на стенде его нет). Скрипт не
    делает вид, что хук отработал.
    """
    code, _, body = _req(
        "POST", f"{INTERNAL}/iam/v1/internal/users:upsertFromIdentity",
        headers={"Authorization": f"Bearer {admin}"},
        body={"externalId": ext_id, "email": email, "displayName": "Ceremony Human"})
    if code != 200:
        raise StageError("6-зеркало", f"UpsertFromIdentity отвечает {code}: {body[:300]}")
    op = json.loads(body or "{}")
    user_id = (op.get("metadata") or {}).get("userId")
    if not user_id:
        raise StageError("6-зеркало", f"в metadata нет userId: {body[:300]}")
    print(f"[ceremony] 6-зеркало: человек {ext_id} отражён как {user_id} "
          f"(хук провижининга на стенде не смонтирован — делаем явно)")
    return user_id


def _ceremony_once(client_id: str, subject: str, acr: str, amr: list[str]) -> str:
    # Одна банка на ОДНУ церемонию: шаги авторизации связаны сессией выдающего,
    # и общая банка между двумя церемониями склеила бы их в одну.
    jar: dict[str, str] = {}
    verifier = secrets.token_urlsafe(64)
    challenge = base64.urlsafe_b64encode(
        hashlib.sha256(verifier.encode()).digest()).decode().rstrip("=")
    q = urllib.parse.urlencode({
        "client_id": client_id, "response_type": "code",
        "scope": "openid offline_access", "redirect_uri": REDIRECT_URI,
        "state": secrets.token_urlsafe(16), "audience": API_AUDIENCE,
        "code_challenge": challenge, "code_challenge_method": "S256",
    })
    code, hdrs, body = _req("GET", f"{HYDRA_PUBLIC}/oauth2/auth?{q}", jar=jar)
    loc = hdrs.get("Location", "")
    if "login_challenge=" not in loc:
        raise StageError("7-церемония", f"запрос авторизации не дал запроса входа "
                                        f"({code}): {loc or body[:200]}")
    lc = urllib.parse.parse_qs(urllib.parse.urlparse(loc).query)["login_challenge"][0]

    code, _, body = _req(
        "PUT", f"{HYDRA_ADMIN}/admin/oauth2/auth/requests/login/accept?"
               f"{urllib.parse.urlencode({'login_challenge': lc})}",
        body={"subject": subject, "remember": False, "acr": acr, "amr": amr})
    if code != 200:
        raise StageError("7-церемония", f"подтверждение входа отвергнуто ({code}): {body[:300]}")
    nxt = json.loads(body)["redirect_to"].replace(ISSUER_PREFIX, HYDRA_PUBLIC)

    code, hdrs, body = _req("GET", nxt, jar=jar)
    loc = hdrs.get("Location", "")
    if "consent_challenge=" not in loc:
        raise StageError("7-церемония", f"после входа не пришёл запрос согласия "
                                        f"({code}): {loc or body[:200]}")
    cc = urllib.parse.parse_qs(urllib.parse.urlparse(loc).query)["consent_challenge"][0]

    code, _, body = _req(
        "PUT", f"{HYDRA_ADMIN}/admin/oauth2/auth/requests/consent/accept?"
               f"{urllib.parse.urlencode({'consent_challenge': cc})}",
        body={"grant_scope": ["openid", "offline_access"],
              "grant_access_token_audience": [API_AUDIENCE], "remember": False})
    if code != 200:
        raise StageError("7-церемония", f"согласие отвергнуто ({code}): {body[:300]}")
    nxt = json.loads(body)["redirect_to"].replace(ISSUER_PREFIX, HYDRA_PUBLIC)

    code, hdrs, body = _req("GET", nxt, jar=jar)
    loc = hdrs.get("Location", "")
    if "code=" not in loc:
        raise StageError("7-церемония", f"согласие не выдало код ({code}): {loc or body[:200]}")
    auth_code = urllib.parse.parse_qs(urllib.parse.urlparse(loc).query)["code"][0]

    st, tok = _post_form(f"{HYDRA_PUBLIC}/oauth2/token", {
        "grant_type": "authorization_code", "code": auth_code,
        "redirect_uri": REDIRECT_URI, "client_id": client_id,
        "code_verifier": verifier})
    if st != 200 or "access_token" not in tok:
        raise StageError("7-церемония", f"обмен кода отвергнут ({st}): {tok}")
    return tok["access_token"]


def stage_ceremony(client_id: str, subject: str) -> tuple[str, str]:
    lvl1 = _ceremony_once(client_id, subject, "1", ["pwd"])
    lvl2 = _ceremony_once(client_id, subject, "2", ["pwd", "totp"])
    print("[ceremony] 7-церемония: получено 2 предъявителя (уровень 1 и уровень 2)")
    return lvl1, lvl2


def stage_edge_accepts(bearer: str) -> None:
    """Край ПРИНИМАЕТ предъявителя — то есть аудитория и подпись сошлись.

    Утверждается ПАРА, иначе «200» не отличить от «край не спрашивает»:
      * с предъявителем — не 401;
      * без предъявителя — 401.
    Проба идёт по ручке, освобождённой от порога уровня: иначе положительная
    половина смешала бы «край не принял предъявителя» с «край не дочитал уровень»,
    а это разные вещи, и вторая — отдельная находка.
    """
    code_anon, _, _ = _req("GET", f"{PUBLIC}/iam/v1/accounts")
    if code_anon != 401:
        raise StageError("8-край", f"контроль сломан: БЕЗ предъявителя край отвечает "
                                   f"{code_anon}, а обязан 401")
    code, _, body = _req("GET", f"{PUBLIC}/iam/v1/accounts",
                         headers={"Authorization": f"Bearer {bearer}"})
    if code == 401:
        raise StageError("8-край", f"край НЕ принял предъявителя церемонии: {body[:300]}")
    print(f"[ceremony] 8-край: без предъявителя 401, с предъявителем {code} — принят")


def stage_write_env(values: dict) -> None:
    import tempfile
    written = 0
    # Значения кладём в НАСТОЯЩИЙ файл, а не в `/dev/stdin`: patch-env читает путь
    # как обычный файл, и подстановка потока сработала бы через раз в зависимости
    # от того, как его открыли. Проверка «через раз» — это отсутствие проверки.
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as fh:
        json.dump(values, fh)
        fixtures_path = fh.name
    try:
        for rel in ENV_FILES:
            path = os.path.join(_ROOT, rel)
            if not os.path.exists(path):
                raise StageError("9-окружение",
                                 f"нет файла окружения {rel} — его пишет посев фикстур")
            rc = subprocess.run(
                [sys.executable, os.path.join(_ROOT, "tests/authz-fixtures/patch-env.py"),
                 fixtures_path, path],
                text=True, capture_output=True)
            if rc.returncode != 0:
                raise StageError("9-окружение", f"patch-env упал на {rel}: {rc.stderr[:300]}")
            written += 1
    finally:
        os.unlink(fixtures_path)
    print(f"[ceremony] 9-окружение: обновлено файлов {written}, ключей {len(values)}")


def _expired_debt() -> str:
    """Долг по `apiTokenExpired`, посчитанный ТЕМ ЖЕ предикатом, что и волна."""
    try:
        sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
        import ceremony_credentials as cc
        res = cc.scan(_ROOT)
        steps = res["by_var"].get("apiTokenExpired", 0)
        asserts = sum(e["asserts"] for rel, e in res["collections"].items()
                      if "env:apiTokenExpired" in e["reasons"])
        return f"apiTokenExpired — {steps} шаг / {asserts} утверждени(й)."
    except Exception as e:  # noqa: BLE001
        # Молчать нельзя: непосчитанный долг неотличим от закрытого.
        return f"apiTokenExpired — число НЕ ПОСЧИТАНО ({e})."


def main() -> int:
    print(f"[ceremony] посев церемонии, прогон {RID}")
    try:
        stage_preflight()
        admin = stage_admin_token()
        _ic_id, client_id = stage_interactive_client(admin)
        ident_id, email = stage_identity()
        subject = stage_password_login(email)
        if subject != ident_id:
            raise StageError("5-вход", f"вошёл не тот, кого завели: {subject} != {ident_id}")
        user_id = stage_mirror(admin, subject, email)
        lvl1, lvl2 = stage_ceremony(client_id, subject)
        stage_edge_accepts(lvl1)

        claims1 = _jwt_payload(lvl1)
        ext = (claims1.get("ext") or {}).get("ext_claims") or claims1.get("ext_claims") or {}
        if ext.get("kacho_user_id") != user_id:
            raise StageError("7-церемония",
                             f"предъявитель принадлежит не тому человеку: "
                             f"{ext.get('kacho_user_id')} != {user_id}")

        stage_write_env({
            "jwtHumanCeremony": lvl1,
            "jwtHumanCeremonyStepUp": lvl2,
            "ceremonyUserId": user_id,
            "ceremonyExternalId": subject,
            "ceremonyEmail": email,
        })
        print(f"[ceremony] ГОТОВО: предъявитель принадлежит человеку {user_id}")
        # Число долга берётся у ОБЪЯВЛЕНИЯ, а не пишется здесь литералом: иначе у
        # одного факта появляется второе место, и оно разъезжается с первым. Свой
        # литерал уже разошёлся — приёмка считала утверждения другим предикатом,
        # чем перепись по дереву, и цифры не совпали.
        print(f"[ceremony] ОТКРЫТЫЙ ДОЛГ (не заполнено, не замаскировано): "
              f"{_expired_debt()} Срок назначает выдающий, укоротить его на один "
              f"выпуск нельзя; нужна волна «выпустить и переждать».")
        return 0
    except StageError as e:
        print(f"===== ПОСЕВ ЦЕРЕМОНИИ ВСТАЛ =====\n{e}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
