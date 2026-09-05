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

ЧТО ЗАПОЛНЯЕТСЯ
---------------
Предъявитель человека, вошедшего паролем (уровень 1), предъявитель того же человека
с поднятым уровнем (уровень 2), их идентификаторы — и `apiTokenExpired`, предъявитель
с УЖЕ прошедшим сроком (стадия 10, делегирует `prodseed_expired_bearer.py`).

Плюс ЛИЧНОСТИ, ЗАВОДЯЩИЕ АККАУНТ (стадия 8г): по одному человеку на каждый слот
`ceremony_credentials.ADMISSION_SLOTS`, каждый — с парой предъявителей и своим
идентификатором. Они существуют потому, что заведение аккаунта списывается с ТЕМПА
личности (три в час на внешний идентификатор входа): восемь заведений волны под одним
человеком давали десять списаний при потолке три. Человек заводит СЕБЕ аккаунт, а не
восемь подряд, — и каждая личность слота заводит ровно один.

ЗДЕСЬ БЫЛ ОТКРЫТЫЙ ДОЛГ, И ОН ЗАКРЫТ — 2026-08-04, ВЫЗОВОМ, А НЕ ПРОЧТЕНИЕМ.
Прежняя редакция утверждала: срок назначает выдающий, «укоротить его на один выпуск
нельзя», поэтому волна идёт 14400 с и в общий прогон не входит. Первая половина верна
и осталась (подделывать срок нельзя — это подменило бы предмет кейса), вторая была
ЛОЖНА. Выдающий принимает per-client `client_credentials_grant_access_token_lifespan`,
и клиенту, которого iam завёл ПОД ЭТУ ПРОБУ, ставится срок в секунды — общая настройка
(`ttl.access_token` выдающего, `KANAME_SAKEY_ACCESSTOKENTTL` сервиса) не трогается.
Замер: 29 с по стенным часам вместо 14400 с, положительный контроль 200 до ожидания,
401 после. Подпись и `exp` по-прежнему ставит выдающий — короче стал срок, а не способ
его назначения.

Отсюда следствие для расписания: собственная волна с ночным прогоном этому кейсу
больше НЕ нужна, и обещать её в комментариях нельзя — обещание места, которого не
требуется, живёт дольше своей причины.

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
import ceremony_credentials as cc  # noqa: E402

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

# ─── СХЕМА ЛИЧНОСТИ, ПО КОТОРОЙ ПИШУТСЯ ПРИЗНАКИ ────────────────────────────
#
# Имя схемы и состав признаков — ОДНО решение, а не два: строгая схема
# (`additionalProperties: false`, `required: [email]`) отвергает любой признак,
# которого в ней нет. Поэтому идентификатор объявлен здесь рядом с признаками,
# а не вписан в тело запроса: сменив схему, соседнюю строку не пропустишь.
#
# Прежде здесь стоял `default` — умолчание ПОСТАВЩИКА, а не наше. Пока
# конфигурацию службы личности никто не монтировал, действовало оно, и посев
# проходил. Как только конфигурация доехала до процесса (задача #904), схемой
# стала наша, а схемы с именем `default` в ней нет — заведение личности начало
# отвечать отказом, и церемония вставала на четвёртой стадии, не доводя
# предъявителя. Ни одна коллекция после этого не запускалась.
#
# Согласие этого имени с чартом держит гейт, а не внимание:
# deploy/identity_config_reaches_the_process_test.go — он читает объявление
# схемы в карте настроек и требует, чтобы посев называл ТУ ЖЕ схему.
IDENTITY_SCHEMA_ID = "kacho_user_v2"

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


def stage_identity(slot: str = "") -> tuple[str, str]:
    """Личность человека у провайдера. `slot` разводит НЕСКОЛЬКИХ людей одного прогона.

    Второй человек нужен не для симметрии: кейс «предъявитель без единой выдачи»
    спрашивает про принципала, у которого НЕТ членства, и главный человек церемонии
    таким быть не может — он владелец своего аккаунта by construction (см.
    `stage_own_account`). Одному предъявителю нельзя одновременно быть владельцем
    известного аккаунта и не иметь выдач; это не настройка, а взаимоисключение,
    поэтому людей двое.
    """
    email = f"ceremony-{slot + '-' if slot else ''}{RID}@kacho.local"
    code, _, body = _req(
        "POST", f"{KRATOS_ADMIN}/admin/identities",
        body={"schema_id": IDENTITY_SCHEMA_ID,
              # `display_name` — единственный необязательный признак схемы;
              # `name.{first,last}` она не знает и отвергает целиком.
              "traits": {"email": email, "display_name": "Ceremony Human"},
              # АДРЕС ПОМЕЧАЕТСЯ ПОДТВЕРЖДЁННЫМ ЗДЕСЬ ЖЕ, и это не послабление.
              #
              # Посадка требует подтверждённого адреса, чтобы вход состоялся:
              # неподтверждённый получает отказ «адрес ещё не подтверждён». В
              # сквозной пробе подтвердить его обычным путём НЕЛЬЗЯ — путь
              # проходит через письмо, которого здесь никто не отправляет и не
              # читает; ждать его значило бы завести в пробе зависимость от
              # почтового ящика.
              #
              # Церемония заводит личность АДМИНИСТРАТИВНЫМ путём, то есть уже
              # выступает стороной, которая ручается за адрес. Отметка о
              # подтверждении — запись этого ручательства, а не обход проверки:
              # сама проверка остаётся включённой и продолжает отвергать
              # личности, заведённые обычным путём и не прошедшие подтверждение.
              "verifiable_addresses": [{
                  "value": email,
                  "via": "email",
                  "verified": True,
                  "status": "completed",
              }],
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


# Бюджет ожидания зеркала. Величина, а не «сколько попыток»: попытки без
# интервала ничего не говорят о времени, а решение здесь принимается именно по
# времени. Тот же порядок, что у `prodseed_matrix.upsert_user` (40 с), с запасом
# на конкурентную нагрузку шарда.
MIRROR_BUDGET_S = 60.0
MIRROR_POLL_S = 0.5

# Исходы вопроса «резолвится ли внешний идентификатор в человека». Перечислены
# ПОЛНОСТЬЮ и различимы между собой — в этом весь смысл выбора сигнала ниже.
_M_READY = "готово"        # 200: строка есть, аутентификация ей разрешена
_M_ABSENT = "нет-строки"   # 404 ОТ ВЛАДЕЛЬЦА: строки пока нет — ждём
_M_REFUSED = "отказ"       # владелец ответил ПРО НАШ subject, и ответил «нет»
_M_BARRED = "не-дают-спросить"  # спросить не дали: нет маршрута / не пустили / не тот метод
_M_UNASKED = "не-спросили"      # 5xx / 429 / транспорт: ответа не получено вовсе

# Опознавательные части сообщений стадии. Определены ОДИН раз и подставляются и в
# текст отказа, и в утверждение самопроверки: иначе проверка «ошибка была» примет
# ЛЮБУЮ ошибку — в том числе ту, что возникла по совсем другой причине. Ровно на
# этом одна из инъекций прошла незамеченной, пока констант не было.
_SAYS_REFUSED = "владелец ОТВЕТИЛ терминально"
_SAYS_NOT_HUMAN = "резолвится не в человека"
_SAYS_ABSENT = "так и не резолвится у владельца"
_SAYS_UNCLASSIFIED = "классификатор вернул исход"

# Диагностика МЯГКОГО ПРОХОДА. Вынесена в константы не ради красоты: это
# единственная часть решения «отдать подсказку, но не ронять посев», которую
# нельзя проверить по возвращаемому значению — оно у мягкого прохода такое же,
# как у довправочного дефекта. Пока констант не было, утверждения самопроверки
# держали ЧТО стадия вернула и МОЛЧАЛИ о том, сказала ли она это вслух: замолчи
# диагностика — все прочие утверждения остались бы зелёными, и «мы пошли вслепую»
# стало бы неотличимо от «мы проверили» (`security.md` §8: мягкий проход обязан
# быть наблюдаемым, иначе мёртвый контроль невидим).
_SAYS_HANDED_ON = "СПРОСИТЬ НЕ УДАЛОСЬ"
_SAYS_RACE_OPEN = "Гонка зеркала этим НЕ закрыта"


def _mirror_lookup(admin: str, ext_id: str) -> tuple[str, str, str]:
    """Спрашивает ВЛАДЕЛЬЦА личности ровно то, что спросит обогащение состава.

    Возвращает `(исход, id-человека-или-пусто, пояснение)`; исход — одна из пяти
    констант выше.

    ПОЧЕМУ ИМЕННО ЭТОТ ВОПРОС. Обогащение состава ищет человека по ВНЕШНЕМУ
    идентификатору и берёт первую строку, которой аутентификация разрешена.
    `InternalIAMService.LookupSubject` по ключу `externalId` — тот же вопрос,
    заданный тому же владельцу: тот же ключ, тот же отбор по состоянию, тот же
    порядок строк. Не приближение к предусловию, а оно само.

    ПОЧЕМУ НЕ ПУБЛИЧНОЕ ЧТЕНИЕ ЧЕЛОВЕКА ПО ЕГО ИДЕНТИФИКАТОРУ. Оно НЕПРИГОДНО как
    сигнал по построению, и это не обходится терпимостью:
      * его отказ НАМЕРЕННО неотличим от настоящего отсутствия — требование
        анти-оракула (`security.md` §6: скрытие существования обязано быть
        байт-в-байт равно настоящему промаху). Значит «ещё нет» и «не моё»
        приходят одним и тем же ответом, и проба, построенная на нём, не
        классифицирует НИЧЕГО, а лишь делает вид;
      * оно спрашивает СТРОГО БОЛЬШЕЕ, чем предусловие: сверх «строка есть» туда
        входят маршрутизация края, запись каталога прав и разрешение области по
        полю запроса. Любая из трёх может измениться, не тронув предусловия, — и
        тогда проба объявляет сорванным посев, условие которого выполнено.
    Чего в этом списке НЕТ — и это замерено, а не предположено: «читать нельзя,
    пока не доехала материализация». Проверено по дереву на `2c619cea`: решение о
    доступе идёт per-object → плоский кластер-админский шлюз → структурный вывод
    из УЖЕ ЗАКОММИЧЕННЫХ строк (`services/iam/internal/service/authorize_service.go`
    `CheckRelation`, `services/iam/internal/authzcascade`), а отказ на крае не
    кэшируется вовсе (`gateway/internal/middleware/authz.go`: в кэш кладётся
    только положительный вердикт). То есть предъявителем этой стадии публичное
    чтение прошло бы — оно лишь не в состоянии СКАЗАТЬ, в каком состоянии оно
    было. Прежняя редакция этого комментария называла причиной очередь и
    отрицательный кэш; оба утверждения о продукте ложны, и держать их здесь
    значило бы завести ловушку из `security.md` §5 (комментарий про безопасность,
    противоречащий коду).
    Внутренняя ручка ничего из этого не тянет: она объявлена освобождённой и
    живёт только на внутреннем листенере, куда стадия и так уже ходит зеркалом.
    """
    try:
        code, _h, body = _req("POST", f"{INTERNAL}/iam/v1/internal/iam:lookupSubject",
                              headers={"Authorization": f"Bearer {admin}"},
                              body={"externalId": ext_id})
    except StageError as exc:  # транспорт: адрес назван внутри
        return _M_UNASKED, "", str(exc)

    if code == 200:
        got = json.loads(body or "{}")
        uid = ((got.get("user") or {}).get("id") or "").strip()
        if uid:
            return _M_READY, uid, ""
        # 200 без человека — это не «ещё нет»: владелец ОТВЕТИЛ, и ответил не тем.
        return _M_REFUSED, "", f"внешний идентификатор {_SAYS_NOT_HUMAN}: {body[:200]}"
    if code == 404:
        # Различаем 404 ВЛАДЕЛЬЦА («такого subject нет») и 404 МАРШРУТИЗАТОРА
        # («такой ручки здесь нет»). Первый называет наш subject в своём тексте,
        # второй назвать его не может — он нашего тела не читал. Без этого
        # различения постоянная ошибка настройки навсегда стала бы «задержкой»
        # (`security.md` §8: мягкий проход обязан отличать настройку от сбоя).
        if ext_id and ext_id in body:
            return _M_ABSENT, "", ""
        return _M_BARRED, "", f"маршрута нет — {body[:200]}"
    if code in (401, 403, 405):
        # «Не пустили спросить» — это НЕ ответ владельца про наш subject, и путать
        # эти два состояния нельзя: первое — настройка периметра, второе —
        # утверждение о человеке. Прежняя редакция сваливала весь 4xx в «отказ» и
        # тем самым делала смену гейта на внутреннем листенере причиной сорванного
        # посева.
        return _M_BARRED, "", f"спросить не дали ({code}) — {body[:200]}"
    if code == 429:
        return _M_UNASKED, "", f"{code} (просят повторить позже): {body[:200]}"
    if 400 <= code < 500:
        return _M_REFUSED, "", f"{code}: {body[:200]}"
    return _M_UNASKED, "", f"{code}: {body[:200]}"


def stage_mirror(admin: str, ext_id: str, email: str) -> str:
    """Зеркало человека в iam — И ОЖИДАНИЕ ТОГО, ЧТО ЕГО НАЙДЁТ ОБОГАЩЕНИЕ.

    ЯВНО, а не хуком: смонтированная на стенде настройка провайдера хука
    провижининга не несёт (в чарте он объявлен — на стенде его нет). Скрипт не
    делает вид, что хук отработал.

    ЗАЧЕМ ЗДЕСЬ ОЖИДАНИЕ. Мутация асинхронна: ответ — конверт операции, а `userId`
    в его метаданных ПРЕДВЫДЕЛЕН, то есть приходит РАНЬШЕ, чем строка человека
    закоммичена (и приходит даже у операции, завершившейся ошибкой). Следующая
    стадия чеканит предъявителя, и обогащение его состава ищет человека по внешнему
    идентификатору. Не найдя — выдаёт СОКРАЩЁННЫЙ состав: это штатное поведение
    первого входа (мирроринг догоняет), а не сбой. Значит, отдав идентификатор до
    коммита, стадия 6 отдаёт гонку: предъявитель выпускается без `kacho_user_id`,
    и посев встаёт на стадии 7 сообщением про ЧУЖОЙ предмет — «предъявитель
    принадлежит не тому человеку: None != …», — по которому ищут регрессию в
    обогащении, которой нет.

    ЖДЁМ РЕЗОЛВ У ВЛАДЕЛЬЦА, А НЕ ЗАВЕРШЕНИЕ ОПЕРАЦИИ — но НЕ по той причине, что
    стояла здесь до 2026-08-06. Прежняя редакция объясняла выбор так: операция
    помечается системной, а прокси операций арендному принципалу системную не
    отдаёт, поэтому опрос сжёг бы бюджет. Для ЭТОГО пути утверждение ЛОЖНО, и это
    измерено, а не переспорено:
      * владельца операции ставит `operations.NewFromContext` — принципал из
        контекста запроса; системный владелец у неё ЗАПАСНОЙ, на случай когда
        принципала нет вовсе (`pkg/operations/helpers.go`, `repo.go` Create);
      * эта стадия шлёт upsert РЕСТом через шлюз (`INTERNAL_BASE_URL`) с
        предъявителем — значит принципал доезжает и попадает в строку операции;
      * прокси операций отказывает арендатору на безымянной и на системной
        операции и при несовпадении владельца — совпадающему владельцу отдаёт
        (`gateway/internal/opsproxy/proxy.go`);
      * эмпирика ТОГО ЖЕ упавшего прогона (31061894576): кейс
        `IAM-INT-OK-INT-USER-UPSERT` делает ровно эту форму — внутренний
        REST-upsert под предъявителем, затем опрос `/operations/{id}` под ним же,
        причём опрос идёт на ПУБЛИЧНЫЙ листенер, — и зелёный; в теле операции
        владельцем стоит не система, а сам вызывающий.
    Ложный вывод приехал от соседа: `prodseed_matrix.upsert_user` ходит в iam
    ПРЯМЫМ gRPC по mTLS, где принципала нет вовсе, и его шапка эту предпосылку
    называет прямо. При переносе сюда вывод скопировали, предпосылку потеряли —
    ровно тот класс, который эта стадия и чинит («два места об одном предмете, из
    которых верно одно»), и ровно та ловушка, которую запрещает `security.md` §5:
    комментарий про безопасность, противоречащий коду, следующий читатель «чинит»
    в сторону комментария.

    ПОЧЕМУ ВСЁ-ТАКИ РЕЗОЛВ, А НЕ ОПРОС — две причины, обе про предмет вопроса.
      * `done` отвечает на ДРУГОЙ вопрос: долговечность предмета мутации. Он
        приходит и у операции, завершившейся ошибкой (см. выше про предвыделенный
        идентификатор), поэтому опрашивающему пришлось бы отдельно проверять
        `error` — и всё равно он не спросил бы того, что нужно следующей стадии:
        «внешний идентификатор резолвится в человека». Резолв — тот же самый
        вопрос, который задаёт обогащение состава.
      * Направление видимости у резолва КОНСЕРВАТИВНОЕ: `LookupSubject` читает
        `Reader` (реплику, если она есть), а обогащение состава — мастер-пул
        (`services/iam/cmd/kaname/serve.go`: `kanamepg.New(pool, slavePool)`
        против `buildHooksMux(pool, …)`). Значит положительный ответ пробы не
        может прийти РАНЬШЕ, чем строку увидит обогащение; у опроса операции
        такого свойства нет.
    Выбор сигнала и отвод публичного чтения — в `_mirror_lookup`.

    ЧТО ВОЗВРАЩАЕТСЯ. Идентификатор, который назвал ВЛАДЕЛЕЦ, а не тот, что лежал
    в метаданных. Метаданные сами объявлены подсказкой «по возможности», и
    расходятся они ровно в той гонке, ради которой стадия и ждёт; сверяет их
    стадия 7 против состава предъявителя, а состав чеканится из ответа владельца.

    ЧТО РОНЯЕТ ПОСЕВ, А ЧТО НЕТ — и почему граница проходит здесь.
      * РОНЯЕТ: владелец ОТВЕТИЛ, и ответил «нет» — терминально (состояние
        личности не допускает аутентификации / запрос неверен) либо «такого
        subject нет» вплоть до исчерпания бюджета. Оба состояния РАЗЛИЧЕНЫ, а не
        предположены, и оба означают, что условие волны не создано: стадия 7 всё
        равно встанет — но сообщением про чужой предмет.
      * НЕ РОНЯЕТ: «спросить не удалось» — ручки по этому адресу нет (постоянная
        настройка: повторять бессмысленно, отвечаем сразу) либо ответа не
        получено вовсе (5xx/транспорт: повторяем в пределах бюджета, вдруг
        временное). Это НЕ ответ «нет», и превращать неполученный ответ в отказ
        значит менять «не выполнилось» на «нечего выполнять»: девять коллекций
        волны не пошли бы из-за смещённого порта или снятого маршрута. Стадия
        громко называет, ЧТО именно ей не удалось, отдаёт идентификатор из
        метаданных и оставляет судьёй стадию 7 — прежнее поведение, не ослабленное.
        ЭТА ВЕТКА — ВОЗВРАТ К ДОВПРАВОЧНОМУ ПОВЕДЕНИЮ, И ОНА НЕ ДОЛЖНА БЫТЬ ТИХОЙ.
        Стадия при этом пойдёт с подсказкой, и волна снова может потерять свои
        коллекции — предупреждением в поток ошибок, но без падения. Поэтому её
        предпосылку — «на здоровом стенде спросить ДАЮТ» — держит не обещание, а
        кейс `IAM-INT-OK-INT-IAM-LOOKUPSUBJECT` того же шарда: он бьёт в тот же
        путь на внутреннем листенере под валидным предъявителем и требует РОВНО
        200 (не `oneOf`), непустой идентификатор и совпадение с ранее заведённым
        человеком — БЕЗУСЛОВНО. Снимут маршрут с внутреннего mux, закроет его
        гейт, перестанет резолвить обработчик — кейс краснеет раньше, чем эта
        ветка успеет стать штатным режимом. Различитель «404 владельца против 404
        маршрутизатора» держит парный `IAM-INT-OK-INT-IAM-LOOKUPSUBJECT-UNKNOWN`:
        он требует, чтобы в 404 владельца стоял ЗАПРОШЕННЫЙ внешний
        идентификатор, — кода `5` для этого мало, его несёт и промах
        маршрутизатора (`{"code":5,"message":"Not Found"}`).
      * На исчерпании бюджета судит ПОСЛЕДНИЙ полученный исход, а не большинство:
        «под конец владелец говорил, что строки нет» и «под конец мы вообще не
        дозвонились» — разные находки, и вторая доказательством не является.
    """
    code, _, body = _req(
        "POST", f"{INTERNAL}/iam/v1/internal/users:upsertFromIdentity",
        headers={"Authorization": f"Bearer {admin}"},
        body={"externalId": ext_id, "email": email, "displayName": "Ceremony Human"})
    if code != 200:
        raise StageError("6-зеркало", f"UpsertFromIdentity отвечает {code}: {body[:300]}")
    op = json.loads(body or "{}")
    hinted = (op.get("metadata") or {}).get("userId")
    if not hinted:
        raise StageError("6-зеркало", f"в metadata нет userId: {body[:300]}")

    started = time.monotonic()
    deadline = started + MIRROR_BUDGET_S
    asks = {_M_ABSENT: 0, _M_UNASKED: 0}
    last, detail = "", ""
    while True:
        outcome, resolved, detail = _mirror_lookup(admin, ext_id)
        last = outcome
        waited = time.monotonic() - started
        total = asks[_M_ABSENT] + asks[_M_UNASKED] + 1

        if outcome == _M_READY:
            drift = "" if resolved == hinted else (
                f"; метаданные называли {hinted} — подсказка разошлась с владельцем, "
                f"дальше идёт ответ владельца")
            print(f"[ceremony] 6-зеркало: человек {ext_id} отражён как {resolved} и "
                  f"РЕЗОЛВИТСЯ у владельца по внешнему идентификатору "
                  f"(ждали {waited:.1f} с, запросов {total}){drift} — хук провижининга "
                  f"на стенде не смонтирован, делаем явно")
            return resolved

        if outcome == _M_REFUSED:
            raise StageError(
                "6-зеркало",
                f"{_SAYS_REFUSED} по внешнему идентификатору {ext_id} "
                f"({detail}). Это не задержка: повтор того же вопроса пройти не может. "
                f"Условие волны не создано — предъявитель чеканился бы без человека")

        if outcome == _M_BARRED:
            return _hand_on(hinted, ext_id,
                            "спросить владельца по этому адресу НЕ ДАЮТ (нет маршрута, "
                            "не пустил гейт, не тот метод) — это НАСТРОЙКА, а не "
                            "задержка, и повтор её не изменит", detail)

        # Корзины «прочее» здесь нет НАМЕРЕННО. Останутся только два исхода,
        # которые действительно стоит повторять; шестой исход, добавленный в
        # классификатор и не решённый здесь, обязан назвать себя сам, а не
        # молча попасть в ожидание (или, того хуже, уронить стадию KeyError'ом
        # без имени предмета).
        if outcome not in asks:
            raise StageError(
                "6-зеркало",
                f"{_SAYS_UNCLASSIFIED} «{outcome}», а стадия не знает, что он "
                f"значит. Его завели и не решили: ждать его, отдавать подсказку или "
                f"останавливать посев — три разных ответа, и молчание не один из них")
        asks[outcome] += 1
        if time.monotonic() >= deadline:
            break
        time.sleep(MIRROR_POLL_S)

    waited = time.monotonic() - started
    if last == _M_UNASKED:
        return _hand_on(hinted, ext_id,
                        f"за {waited:.1f} с ответа так и не получено "
                        f"({asks[_M_UNASKED]} попыт(ок) без ответа, "
                        f"{asks[_M_ABSENT]} с ответом «такого subject нет»)", detail)
    raise StageError(
        "6-зеркало",
        f"человек {ext_id} ({hinted}) {_SAYS_ABSENT} по внешнему "
        f"идентификатору: {asks[_M_ABSENT]} ответ(ов) «такого subject нет» за "
        f"{waited:.1f} с. Строку не произвёл асинхронный работник — идентификатор из "
        f"метаданных предвыделен, поэтому отдать его значило бы отдать фантом, а "
        f"падение уехало бы на стадию 7 с сообщением про чужой предмет")


def _hand_on(hinted: str, ext_id: str, why: str, detail: str) -> str:
    """Ответа НЕ получено — отдаём подсказку из метаданных и говорим это вслух.

    Отдельная функция, потому что это единственное место, где стадия сознательно
    возвращает НЕподтверждённый идентификатор, и оно обязано читаться как одно
    решение, а не как две похожие ветки.
    """
    # Поток вывода сбрасывается ПЕРЕД записью в поток ошибок: иначе предупреждение
    # уезжает выше стадий, которые его объясняют, и читается как относящееся к
    # другому месту. В журнале прогона этот файл уже так и выглядел.
    #
    # Само это предупреждение — ЧАСТЬ РЕШЕНИЯ, а не украшение к нему: без него
    # мягкий проход неотличим от довправочного дефекта, потому что возвращает то
    # же самое значение. Поэтому оно закрыто утверждением самопроверки (сценарии
    # «д»/«д'»/«е»), и утверждение ключуется на константы ниже, а не на прозу.
    sys.stdout.flush()
    print(f"[ceremony] 6-зеркало: {_SAYS_HANDED_ON} по {ext_id} — {why} "
          f"({detail}). Резолв у владельца НЕ подтверждён; отдаём {hinted} из "
          f"метаданных, судит стадия 7. {_SAYS_RACE_OPEN}.",
          file=sys.stderr, flush=True)
    return hinted


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


def stage_own_account(bearer: str) -> str:
    """Аккаунт, которым человек церемонии ВЛАДЕЕТ, — заводится ИМ САМИМ.

    ЗАЧЕМ ОН НУЖЕН ЗДЕСЬ, А НЕ «СЛОЖИТСЯ ПО ХОДУ». Человек церемонии набирает
    аккаунты из коллекций самой волны, поэтому его членство — ДВИЖУЩАЯСЯ величина,
    зависящая от порядка коллекций. Кейс, которому нужен «аккаунт, которым этот
    человек владеет», на такой величине стоять не может: он либо читает чужой
    результат, либо краснеет от перестановки. Здесь аккаунт заводится ПОСЕВОМ,
    один, с известным id, — и предпосылка перестаёт зависеть от расписания.

    Заводится ЧЕЛОВЕКОМ, а не администратором: аккаунт принадлежит пользователю by
    construction, владелец выводится из принципала. Ровно поэтому та же форма
    запроса объявлена в `ceremony_credentials.HUMAN_CALLER_REQUESTS`.
    """
    code, _, body = _req("POST", f"{PUBLIC}/iam/v1/accounts",
                         headers={"Authorization": f"Bearer {bearer}"},
                         body={"name": f"ceremony-own-{RID}"})
    if code != 200:
        raise StageError("8б-аккаунт", f"создание аккаунта человеком отвечает {code}: {body[:300]}")
    op = json.loads(body or "{}")
    # Порядок обязателен: сначала УБЕДИТЬСЯ, что операция не завершилась ошибкой, и
    # только потом читать id из metadata. Kachō кладёт предвыделенный id в metadata
    # ДАЖЕ у операции с ошибкой — чтение без этой проверки даёт фантом, который
    # уезжает в окружение и всплывает чужим каскадом.
    for _ in range(40):
        c2, _, b2 = _req("GET", f"{PUBLIC}/operations/{op.get('id')}",
                         headers={"Authorization": f"Bearer {bearer}"})
        if c2 != 200:
            raise StageError("8б-аккаунт", f"поллинг операции отвечает {c2}: {b2[:200]}")
        done = json.loads(b2 or "{}")
        if done.get("done"):
            if done.get("error"):
                raise StageError("8б-аккаунт", f"создание аккаунта завершилось ошибкой: {done['error']}")
            acc = (done.get("metadata") or {}).get("accountId")
            if not acc:
                raise StageError("8б-аккаунт", f"в metadata нет accountId: {b2[:300]}")
            print(f"[ceremony] 8б-аккаунт: человек владеет {acc} (заведён им самим)")
            return acc
        time.sleep(0.5)
    raise StageError("8б-аккаунт", "операция создания аккаунта не завершилась за отведённое время")


def stage_admission_pool(client_id: str, admin: str) -> dict[str, str]:
    """Личности, заводящие аккаунт: ОДНА личность — ОДИН аккаунт волны.

    ЗАЧЕМ ОНИ ЕСТЬ. Заведение аккаунта списывается с ТЕМПА личности (три в час) и с
    ОБЪЁМА личности (пять одновременно живых); носитель обоих — внешний идентификатор
    входа. Пока вся волна шла под человеком церемонии, восемь её заведений ложились
    на одного носителя, у которого посев уже занял два места, — и семь получали отказ
    по темпу. Отказ был ВЕРЕН; неверна была форма пробы: человек заводит себе аккаунт,
    а не восемь подряд.

    БАЛАНС КАЖДОЙ ЛИЧНОСТИ НАЗВАН ЧИСЛОМ, А НЕ ПОДРАЗУМЕВАЕТСЯ. Зеркало заводит ей
    личный аккаунт первого входа — это +1 и оно безусловно (ветвь вставки окна
    допуска). Проба заводит один — это +2 при потолке 3. Запас — единица; она
    намеренная: одиночное новое заведение в волну помещается, а два подряд роняют
    гейт запаса, то есть видны в обзоре, а не на стенде через сорок минут.

    ПОЧЕМУ УРОВЕНЬ 2 ТОЖЕ. Уборка за собой снимает аккаунт и его дочерний проект, а
    необратимое снятие объявлено чувствительным (`required_acr_min`): без поднятого
    входа проба заводила бы аккаунт и не могла его снять, то есть меняла бы отказ по
    темпу на рост объёма.

    ПЕРЕЧЕНЬ СЛОТОВ НЕ ВЫПИСАН ЗДЕСЬ. Он читается у единственного объявления
    (`ceremony_credentials.ADMISSION_SLOTS`), которое читают и кейсы, и гейты
    потолков: выписанный второй раз, он разошёлся бы с ними молча.
    """
    values: dict[str, str] = {}
    for slot, slug, subject in cc.ADMISSION_SLOTS:
        ident, email = stage_identity(slug)
        logged_in = stage_password_login(email)
        if logged_in != ident:
            raise StageError("8г-личности-заведения",
                             f"слот {slot}: вошёл не тот, кого завели: {logged_in} != {ident}")
        user_id = stage_mirror(admin, logged_in, email)
        lvl1, lvl2 = stage_ceremony(client_id, logged_in)

        # Утверждается ПАРА, а не факт выпуска: предъявитель, которого край не
        # принимает, отдал бы волне слот, выглядящий заполненным. Проверка та же,
        # что у главного человека, — своей копии здесь нет.
        stage_edge_accepts(lvl1)

        # И состав предъявителя обязан называть ТОГО человека, чьё зеркало ждали.
        # Стадия 6 при исчерпании бюджета отдаёт подсказку из метаданных и говорит
        # это вслух — то есть возвращает НЕподтверждённый идентификатор. Без этой
        # сверки такой слот уехал бы в волну, и кейс упал бы утверждением о
        # ВЛАДЕЛЬЦЕ заведённого аккаунта — то есть назвал бы дефектом продукта то,
        # что случилось в посеве. У главного человека эта сверка стоит в main; здесь
        # она своя, потому что слот минтится в другом месте.
        claims = _jwt_payload(lvl1)
        ext_claims = ((claims.get("ext") or {}).get("ext_claims")
                      or claims.get("ext_claims") or {})
        if ext_claims.get("kacho_user_id") != user_id:
            raise StageError(
                "8г-личности-заведения",
                f"слот {slot}: предъявитель принадлежит не тому человеку: "
                f"{ext_claims.get('kacho_user_id')} != {user_id}")

        l1_var, l2_var = cc.admission_bearers(slot)
        values[l1_var] = lvl1
        values[l2_var] = lvl2
        values[cc.admission_user_id_var(slot)] = user_id
        print(f"[ceremony] 8г-личности-заведения: слот {slot} — человек {user_id} "
              f"({subject})")

    print(f"[ceremony] 8г-личности-заведения: заведено личностей {len(cc.ADMISSION_SLOTS)}, "
          f"переменных {len(values)}; каждая заводит РОВНО ОДИН аккаунт волны")
    return values


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

        own_account = stage_own_account(lvl1)

        # ── 8в. ВТОРОЙ человек — намеренно БЕЗ единой выдачи ──────────────────
        # Ему НЕ заводится аккаунт и НЕ выдаётся ничего: это и есть его свойство.
        # Кейс про освобождённый RPC спрашивает, доступен ли он ЛЮБОМУ
        # аутентифицированному — в том числе тому, у кого нет ни одного членства.
        # Подставить сюда главного человека церемонии нельзя: он владелец своего
        # аккаунта, и «список членств пуст» на нём было бы ложью, которая к тому же
        # зеленела бы через раз в зависимости от порядка коллекций.
        nb_ident, nb_email = stage_identity("nobind")
        nb_subject = stage_password_login(nb_email)
        if nb_subject != nb_ident:
            raise StageError("5-вход", f"вошёл не тот, кого завели: {nb_subject} != {nb_ident}")
        nb_user_id = stage_mirror(admin, nb_subject, nb_email)
        nb_lvl1, _nb_lvl2 = stage_ceremony(client_id, nb_subject)
        stage_edge_accepts(nb_lvl1)
        print(f"[ceremony] 8в-без-выдач: второй человек {nb_user_id} заведён и НЕ гранчен")

        pool = stage_admission_pool(client_id, admin)

        stage_write_env({
            **pool,
            "jwtHumanCeremony": lvl1,
            "jwtHumanCeremonyStepUp": lvl2,
            "ceremonyUserId": user_id,
            "ceremonyExternalId": subject,
            "ceremonyEmail": email,
            "ceremonyAccountId": own_account,
            "jwtHumanCeremonyNoBindings": nb_lvl1,
            "ceremonyNoBindingsUserId": nb_user_id,
        })
        print(f"[ceremony] ГОТОВО: предъявитель принадлежит человеку {user_id}")

        # ── 10. предъявитель с ПРОШЕДШИМ сроком ──────────────────────────────
        # Прежде здесь печатался ОТКРЫТЫЙ ДОЛГ с числом: срок назначает выдающий,
        # и «переждать» его означало 14400 с, то есть волну на часы. Это перестало
        # быть верным 2026-08-04 — установлено ВЫЗОВОМ, а не прочтением: выдающий
        # принимает per-client `client_credentials_grant_access_token_lifespan`, и
        # клиенту, заведённому iam ПОД ЭТУ ПРОБУ, можно поставить срок в секунды,
        # не трогая общую настройку. Долга больше нет, поэтому и печатать его
        # нельзя: устаревшее объявление долга — такое же ложное утверждение о
        # продукте, как устаревшая запись «известное красное».
        #
        # Стадия делегирует, а не повторяет: у выпуска остаётся ОДИН владелец
        # (`prodseed_expired_bearer.py`), и его положительный контроль,
        # проверка предпосылки и отказ по стадиям применяются здесь целиком.
        import prodseed_expired_bearer as peb  # noqa: PLC0415

        rc = peb.main()
        if rc != 0:
            raise StageError("10-истёкший",
                             "предъявитель с прошедшим сроком не добыт (стадия названа "
                             "в её собственном выводе выше). Волна условие СОЗДАЁТ — "
                             "не создала, значит отчётов быть не должно.")
        print("[ceremony] 10-истёкший: `apiTokenExpired` заполнен настоящим "
              "предъявителем, пережившим свой срок по стенным часам")
        return 0
    except StageError as e:
        print(f"===== ПОСЕВ ЦЕРЕМОНИИ ВСТАЛ =====\n{e}", file=sys.stderr)
        return 2


# ─── самопроверка стадии 6 ───────────────────────────────────────────────────
# Идентификаторы РАЗНЫЕ намеренно: предвыделенная подсказка из метаданных и
# идентификатор, который называет владелец. На этом различии и стоит вся проба —
# стадия обязана вернуть ВТОРОЙ. Совпадающие значения сделали бы утверждение
# нефальсифицируемым: дефект (отдать подсказку сразу) прошёл бы его.
_ST_HINT = "usrPREALLOCATED000000"
_ST_CANON = "usrOWNERRESOLVED00000"
_ST_EXT = "ext-ceremony-selftest"


def _st_owner_absent() -> tuple[int, str]:
    """404 ВЛАДЕЛЬЦА: он прочитал наш вопрос и назвал наш subject в ответе."""
    return 404, json.dumps({"code": 5,
                            "message": f"subject not found by external_id={_ST_EXT}"})


def _st_route_absent() -> tuple[int, str]:
    """404 МАРШРУТИЗАТОРА: тела он не читал, назвать наш subject не может."""
    return 404, json.dumps({"code": 5, "message": "Not Found"})


def _st_ready() -> tuple[int, str]:
    return 200, json.dumps({"user": {"id": _ST_CANON, "externalId": _ST_EXT}})


def _st_blocked() -> tuple[int, str]:
    return 400, json.dumps({"code": 9, "message": f"identity {_ST_EXT} is blocked"})


def _st_no_answer() -> tuple[int, str]:
    return 503, json.dumps({"code": 14, "message": "repo reader"})


def _st_barred() -> tuple[int, str]:
    """Гейт внутреннего листенера не пустил. Это НЕ ответ про наш subject."""
    return 403, json.dumps({"code": 7, "message": "AUTHZ_DENIED"})


def _st_not_a_human() -> tuple[int, str]:
    """200, но резолвится НЕ человек. Владелец ответил — и ответил не тем."""
    return 200, json.dumps({"serviceAccount": {"id": "svaXYZ"}})


def _defective_stage_mirror(admin: str, ext_id: str, email: str) -> str:
    """ДЕФЕКТ, КОТОРЫЙ ЧИНИТСЯ, — дословно: взять подсказку и пойти дальше.

    Это не карикатура и не «примерно так»: ровно это делала стадия 6 до правки, и
    ровно из-за этого предъявитель чеканился раньше зеркала. Двойник существует,
    чтобы утверждения ниже были проверены В ОБЕ СТОРОНЫ — на нём они обязаны
    краснеть. Если он их проходит, красным объявляется САМА самопроверка: значит
    она перестала различать то, ради чего написана.
    """
    code, _, body = _req(
        "POST", f"{INTERNAL}/iam/v1/internal/users:upsertFromIdentity",
        headers={"Authorization": f"Bearer {admin}"},
        body={"externalId": ext_id, "email": email, "displayName": "Ceremony Human"})
    if code != 200:
        raise StageError("6-зеркало", f"UpsertFromIdentity отвечает {code}: {body[:300]}")
    return (json.loads(body or "{}").get("metadata") or {}).get("userId")


# ─── СЛЕДСТВИЕ, а не возвращаемое значение ───────────────────────────────────
# Утверждения выше локают, ЧТО отдаёт стадия 6. Свойство, ради которого она
# написана, — другое: предъявитель чеканится ПОСЛЕ того, как человек резолвится.
# Это разные вещи, и разница измерена: если перенести чеканку перед ожиданием
# (`stage_ceremony` выше `stage_mirror` в `main`), все четырнадцать утверждений
# остаются ЗЕЛЁНЫМИ — дефект, который здесь чинится, возвращается и никто не
# замечает. Тот же класс, что «юнит на валидатор вместо пробы порядка»
# (api-conventions.md §List: locked ЧТО отвечает гейт и молчит о том, КОГДА он
# исполняется).
#
# Ниже — два замка на само свойство, и они дополняют друг друга:
#   * ДИНАМИЧЕСКИЙ (_st_sequence) — проигрывает последовательность посева на
#     смоделированном мире и сверяет СОСТАВ предъявителя, то есть ровно то
#     утверждение, на котором посев встал в прогоне 31061894576;
#   * СТАТИЧЕСКИЙ (_st_mint_follows_mirror) — разбором AST требует, чтобы в
#     `main` чеканка не опережала ожидание. Динамический замок один этого не
#     удержит: он гоняет стадию НАПРЯМУЮ и о порядке в `main` не знает.
_ST_COMMIT_AT_S = 1.6  # когда строка человека коммитится, по управляемым часам


def _st_sequence(stage, clock: dict) -> tuple[dict, StageError | None]:
    """Проигрывает НАСТОЯЩУЮ последовательность посева: зеркало → чеканка → сверка.

    Мир смоделирован по продукту, а не выдуман:
      * строка человека коммитится в момент `_ST_COMMIT_AT_S` по управляемым
        часам — то есть ПОЗЖЕ, чем мутация вернула конверт операции с
        предвыделенной подсказкой;
      * владелец личности до этого момента отвечает своим 404 («такого subject
        нет»), после — 200 с каноническим идентификатором;
      * ЧЕКАНКА обогащает состав ровно так, как это делает продукт: ищет
        человека по внешнему идентификатору и, НЕ НАЙДЯ, выдаёт СОКРАЩЁННЫЙ
        состав без `kacho_user_id` (`token_hook_handler`: ветка «identity has no
        kacho mirror yet» → `MinimalClaims`). Это не «примерно так»: именно этот
        состав и пришёл в упавшем прогоне.
    Возвращает состав предъявителя и отказ стадии 7, если он случился.
    """
    committed = lambda: clock["t"] >= _ST_COMMIT_AT_S  # noqa: E731

    def fake(method, url, *, body=None, headers=None, timeout=20,
             allow_redirects=False, insecure=False, jar=None):
        if method == "POST" and "upsertFromIdentity" in url:
            return 200, {}, json.dumps({"id": "iopX", "metadata": {"userId": _ST_HINT}})
        if method == "POST" and "iam:lookupSubject" in url:
            return (200, {}, _st_ready()[1]) if committed() else (404, {}, _st_owner_absent()[1])
        raise AssertionError(f"самопроверка не ждала запроса {method} {url}")

    real_req = _req
    globals()["_req"] = fake
    try:
        user_id = stage("adm", _ST_EXT, "a@b.c")
        # Чеканка — здесь, ПОСЛЕ стадии 6, как в `main`.
        claims = {"kacho_user_id": _ST_CANON} if committed() else {}
        if claims.get("kacho_user_id") != user_id:
            return claims, StageError(
                "7-церемония",
                f"предъявитель принадлежит не тому человеку: "
                f"{claims.get('kacho_user_id')} != {user_id}")
        return claims, None
    except StageError as exc:
        return {}, exc
    finally:
        globals()["_req"] = real_req


def _st_mint_follows_mirror() -> tuple[bool, str]:
    """Предъявитель чеканится ТОЛЬКО ТОМУ, чьё зеркало уже дождались — по AST.

    Разбор дерева, а не текста: строка в комментарии или в литерале порядком
    исполнения не является, а regexp этого не различает (testing.md §гейт читает
    исполняемую часть).

    ПРАВИЛО — ПРО СУБЪЕКТА, А НЕ ПРО СЧЁТ ВЫЗОВОВ. У каждой чеканки
    `stage_ceremony(_, S)` обязано найтись ожидание `stage_mirror(_, S, _)` ВЫШЕ
    по исходнику, с ТЕМ ЖЕ именем субъекта. Прежняя редакция считала вызовы
    («перед k-й чеканкой не менее k ожиданий») и на перестановке «дождались A →
    чеканим B → дождались B → чеканим A» молчала: счёт сходился, привязки не
    было — то есть объявление обещало покрытие обоих людей, которого у правила не
    было. Слепая зона измерена инъекцией на копии дерева, а не предположена, и
    правило переписано под то, что замок действительно проверяет.

    КАКОЙ АРГУМЕНТ СЧИТАТЬ СУБЪЕКТОМ, замок берёт из САМИХ определений стадий в
    этом же файле (второй параметр каждой), а не из зашитого номера: поменяется
    подпись — он это увидит и скажет, а не промолчит. Именованный аргумент
    (`ext_id=…`, `subject=…`) резолвится по тому же имени параметра, поэтому
    законная переписка вызова замок не краснит.

    ЧТО ЕЩЁ ТРЕБУЕТСЯ — РЕЗУЛЬТАТ ОЖИДАНИЯ ОБЯЗАН БЫТЬ СВЯЗАН И ПРОЧИТАН. Иначе
    `main` мог бы дождаться зеркала, ВЫБРОСИТЬ ответ владельца и пойти дальше с
    предвыделенной подсказкой: порядок при этом не нарушен, а чинимый дефект
    возвращается. Замок требует, чтобы вызов ожидания стоял значением присваивания
    одному имени и чтобы это имя ниже читалось.

    ОБЪЯВЛЕННАЯ СЛЕПАЯ ЗОНА, РАДИУС НАЗВАН. Замок читает ПОРЯДОК и ПРИВЯЗКУ, а не
    поток данных: если имя присвоено, прочитано, но сверяют в стадии 7 не его, а
    что-то другое, — дерево этого не покажет. Ловит такое само утверждение стадии
    7 на живом стенде (состав предъявителя против возвращённого идентификатора) и
    динамический замок `_st_sequence` на смоделированном мире. Статически эта
    половина не закрыта, и заявлять обратное здесь нельзя.

    Свою предпосылку замок проверяет сам — не найдя предмета (нет `main`, нет
    определений стадий, нет вызовов), он объявляет это находкой, а не чистотой:
    «ноль находок» обязано быть отличимо от «ноль прочитанного».
    """
    import ast  # noqa: PLC0415 — нужен только самопроверке, не посеву

    tree = ast.parse(open(os.path.abspath(__file__), encoding="utf-8").read())
    defs = {n.name: n for n in ast.walk(tree)
            if isinstance(n, ast.FunctionDef) and n.name in ("stage_mirror",
                                                             "stage_ceremony")}
    for want in ("stage_mirror", "stage_ceremony"):
        if want not in defs:
            return False, f"в дереве нет `{want}` — замок не прочитал НИЧЕГО"
    # Имя параметра-субъекта берётся из подписи стадии, а не зашивается номером.
    subj_param = {}
    for name in ("stage_mirror", "stage_ceremony"):
        params = [a.arg for a in defs[name].args.args]
        if len(params) < 2:
            return False, (f"у `{name}` меньше двух параметров ({params}) — замок не "
                           f"может назвать субъект, предпосылка правила отпала")
        subj_param[name] = params[1]
    # ОБЛАСТЬ ЧТЕНИЯ — КАЖДАЯ функция, которая ЧЕКАНИТ, а не одна названная.
    #
    # Прежняя редакция читала только `main`, и это было верно ровно до того дня,
    # когда чеканка появилась во второй функции: посев личностей, заводящих
    # аккаунт, минтит предъявителей в цикле, и `main` о его порядке не знает
    # ничего. Замок при этом остался бы ЗЕЛЁНЫМ — он честно проверял бы `main`,
    # где чеканок две, — то есть перестал бы читать целый вид предмета молча.
    # Имя `main` здесь больше не зашито: область выводится из самих вызовов.
    minting_scopes = [
        fn for fn in ast.walk(tree)
        if isinstance(fn, ast.FunctionDef)
        and fn.name not in ("stage_mirror", "stage_ceremony")
        and any(isinstance(c, ast.Call) and isinstance(c.func, ast.Name)
                and c.func.id == "stage_ceremony" for c in ast.walk(fn))
    ]
    if not minting_scopes:
        return False, ("ни одна функция дерева не чеканит предъявителя — замок не "
                       "прочитал НИЧЕГО, и это находка, а не чистота")

    seen_calls = seen_mirrors = seen_mints = 0
    pair_lines: list[str] = []
    for main_fn in minting_scopes:

        def subject_of(call: ast.Call, fname: str) -> tuple[str, str]:
            """Имя субъекта вызова либо причина, по которой связать его нельзя."""
            arg = None
            if len(call.args) >= 2:
                arg = call.args[1]
            for kw in call.keywords:
                if kw.arg == subj_param[fname]:
                    arg = kw.value
            if arg is None:
                return "", f"субъект не передан (ни позиционно, ни как `{subj_param[fname]}`)"
            if not isinstance(arg, ast.Name):
                return "", (f"субъект задан выражением `{ast.unparse(arg)}`, а не именем — "
                            f"связать его с ожиданием нельзя; свяжите именем")
            return arg.id, ""

        # Ожидание обязано быть присвоено ОДНОМУ имени, и это имя обязано читаться ниже.
        bound: dict[int, str] = {}
        for node in ast.walk(main_fn):
            if isinstance(node, ast.Assign) and isinstance(node.value, ast.Call) \
                    and isinstance(node.value.func, ast.Name) \
                    and node.value.func.id == "stage_mirror" \
                    and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
                bound[node.value.lineno] = node.targets[0].id
        reads: list[tuple[str, int]] = [
            (n.id, n.lineno) for n in ast.walk(main_fn)
            if isinstance(n, ast.Name) and isinstance(n.ctx, ast.Load)]

        calls: list[tuple[str, int, str]] = []      # (стадия, строка, субъект)
        for node in ast.walk(main_fn):
            if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) \
                    and node.func.id in subj_param:
                subj, why = subject_of(node, node.func.id)
                if why:
                    return False, f"строка {node.lineno}, `{node.func.id}`: {why}"
                calls.append((node.func.id, node.lineno, subj))
        calls.sort(key=lambda it: it[1])
        mirrors = [c for c in calls if c[0] == "stage_mirror"]
        mints = [c for c in calls if c[0] == "stage_ceremony"]
        if not mirrors or not mints:
            return False, (f"в `{main_fn.name}` замок не нашёл предмета: ожиданий "
                           f"{len(mirrors)}, чеканок {len(mints)} — функция чеканит "
                           f"предъявителя, не дождавшись ни одного зеркала")

        for _fn, line, subj in mirrors:
            name = bound.get(line)
            if name is None:
                return False, (f"строка {line}: ответ владельца по `{subj}` НЕ присвоен "
                               f"имени — дождались и выбросили, дальше пошла бы подсказка")
            if not any(rid == name and rline > line for rid, rline in reads):
                return False, (f"строка {line}: `{name}` (ответ владельца по `{subj}`) ниже "
                               f"нигде не читается — ожидание есть, следствия у него нет")

        for _fn, line, subj in mints:
            earlier = [m for m in mirrors if m[2] == subj and m[1] < line]
            if not earlier:
                waited = sorted({m[2] for m in mirrors if m[1] < line})
                return False, (f"строка {line}: чеканим предъявителя субъекту `{subj}`, "
                               f"а его зеркала выше НЕ дождались (выше дождались: "
                               f"{waited or 'никого'}) — предъявитель выпускается без "
                               f"человека")
        pairs = ", ".join(f"{s}: ожидание строка {ml}, чеканка строка {cl}"
                          for _f, cl, s in mints
                          for ml in [max(m[1] for m in mirrors if m[2] == s and m[1] < cl)])
        seen_calls += len(calls)
        seen_mirrors += len(mirrors)
        seen_mints += len(mints)
        pair_lines.append(f"{main_fn.name} — {pairs}")

    return True, (f"осмотрено функц(ий) {len(minting_scopes)}, вызовов {seen_calls}: "
                  f"ожиданий {seen_mirrors}, чеканок {seen_mints}; связаны субъекты — "
                  + "; ".join(pair_lines))



def _self_test() -> int:
    """Стадия 6 не отдаёт идентификатор раньше, чем его подтвердил ВЛАДЕЛЕЦ;
    стадия 8г заводит по одной личности на слот и роняет чужой состав.

    Сеть не трогается: подменяется единственная точка выхода `_req`, а вместе с
    ней часы — иначе исчерпание бюджета заняло бы в проверке минуту реального
    времени и никто не стал бы её гонять.

    Каждый случай — пара: сценарий и то, чем он отличается от соседнего. Пять
    исходов `_mirror_lookup` покрыты все, и покрытие это ПЕЧАТАЕТСЯ: «ноль
    находок» обязано быть отличимо от «ноль осмотренного».
    """
    import contextlib  # noqa: PLC0415 — нужны только самопроверке, не посеву
    import io  # noqa: PLC0415

    rc = 0
    print("=== самопроверка стадии 6: зеркало подтверждает ВЛАДЕЛЕЦ, а не метаданные ===")
    real_req, real_sleep, real_monotonic = _req, time.sleep, time.monotonic
    real_lookup = _mirror_lookup
    # Покрытие ИЗМЕРЯЕТСЯ, а не объявляется: исходы снимаются с настоящего
    # классификатора по ходу прогона. Список, который ведут руками, расходится с
    # тем, что действительно исполнилось, — и расходится молча.
    covered: set[str] = set()
    # Объём осмотренного СЧИТАЕТСЯ по ходу, а не выписывается в конце: выписанное
    # число расходится с исполненным ровно тогда, когда сценарий добавили и
    # забыли поправить итог.
    tally = {"сценариев": 0, "утверждений": 0, "двойников": 0}
    broken: list[str] = []

    def install(answers):
        """answers — ответы владельца на вопрос о резолве, по порядку.

        Последний повторяется до конца бюджета: «оно так и осталось» — это
        отдельное состояние, и выразить его конечным списком нельзя.
        """
        seq = list(answers)
        seen = {"asks": 0, "upserts": 0}

        def fake(method, url, *, body=None, headers=None, timeout=20,
                 allow_redirects=False, insecure=False, jar=None):
            if method == "POST" and "upsertFromIdentity" in url:
                seen["upserts"] += 1
                return 200, {}, json.dumps({"id": "iopX",
                                            "metadata": {"userId": _ST_HINT}})
            if method == "POST" and "iam:lookupSubject" in url:
                if (body or {}).get("externalId") != _ST_EXT:
                    raise AssertionError(
                        f"стадия спросила владельца не про тот subject: {body!r}")
                seen["asks"] += 1
                code, payload = seq[min(seen["asks"] - 1, len(seq) - 1)]()
                return code, {}, payload
            raise AssertionError(f"самопроверка не ждала запроса {method} {url}")
        return fake, seen

    def watched_lookup(admin, ext_id):
        outcome, resolved, detail = real_lookup(admin, ext_id)
        covered.add(outcome)
        return outcome, resolved, detail

    def run(answers, stage=stage_mirror, lookup=None):
        """Гоняет стадию на подставленных ответах и УПРАВЛЯЕМЫХ часах.

        Пятым возвращает СКАЗАННОЕ ВСЛУХ (поток ошибок стадии). Без него мягкий
        проход проверить нечем: он возвращает ровно то же значение, что и
        довправочный дефект, и отличается ТОЛЬКО тем, что называет себя.
        """
        tally["сценариев"] += 1
        if stage is _defective_stage_mirror:
            tally["двойников"] += 1
        globals()["_req"], seen = install(answers)
        globals()["_mirror_lookup"] = lookup or watched_lookup
        clock = {"t": 0.0}
        time.monotonic = lambda: clock["t"]
        time.sleep = lambda s: clock.__setitem__("t", clock["t"] + s)
        heard = io.StringIO()
        try:
            with contextlib.redirect_stderr(heard):
                try:
                    out, err = stage("adm", _ST_EXT, "a@b.c"), None
                except StageError as exc:
                    out, err = None, exc
                except AssertionError as exc:
                    # Стадия обратилась не туда или не с тем — договор с подставным
                    # ответчиком нарушен. Это НЕ исход стадии и не может засчитываться
                    # ни одному утверждению ниже: они ключуются на текст конкретного
                    # отказа, а этот текст ни одному из них не подойдёт. Плюс отдельная
                    # именованная строка внизу, чтобы причина не осталась трассировкой.
                    broken.append(str(exc))
                    out, err = None, exc
        finally:
            globals()["_req"] = real_req
            globals()["_mirror_lookup"] = real_lookup
            time.sleep, time.monotonic = real_sleep, real_monotonic
            # Перехваченное возвращается в НАСТОЯЩИЙ поток ошибок: утверждение
            # ловит диагностику, но не должно её проглатывать — иначе журнал
            # прогона потеряет ровно то, ради чего она печатается.
            sys.stderr.write(heard.getvalue())
        return out, err, seen["asks"], clock["t"], heard.getvalue()

    def check(tag, ok, detail):
        nonlocal rc
        tally["утверждений"] += 1
        print(f"  {'ОК ' if ok else 'ПРОВАЛ'} {tag}: {detail}")
        if not ok:
            rc = 1

    def said_aloud(text):
        """Диагностика мягкого прохода сказана — И НАЗЫВАЕТ ПРЕДМЕТ.

        Держит ту часть решения, которую не видно по возвращаемому значению:
        мягкий проход отдаёт ровно то же, что довправочный дефект, и отличается
        ТОЛЬКО тем, что говорит вслух, кого не дождался и что отдал взамен.
        Замолчи он — и «мы пошли вслепую» станет неотличимо от «мы проверили»,
        при том что все прочие утверждения останутся зелёными (эта дыра названа
        приёмкой третьего круга и закрывается здесь).

        Проверяется ПОТОК ОШИБОК, а не вывод: предупреждение, уехавшее в общий
        поток, тонет в журнале прогона — выбор потока тоже часть решения.
        """
        parts = {
            "назвал себя": _SAYS_HANDED_ON in text,
            "назвал человека": _ST_EXT in text,
            "назвал отданное взамен": _ST_HINT in text,
            "сказал, что гонка НЕ закрыта": _SAYS_RACE_OPEN in text,
        }
        missing = [k for k, ok in parts.items() if not ok]
        if missing:
            return False, (f"диагностика мягкого прохода молчит или обеднела — нет: "
                           f"{missing}; поток ошибок: {text.strip()[:140]!r}")
        return True, f"поток ошибок несёт все {len(parts)} части предмета"

    # (а) НАСТОЯЩАЯ ЗАДЕРЖКА: строки ещё нет, потом появилась. Стадия обязана
    #     переждать и вернуть идентификатор ВЛАДЕЛЬЦА, а не подсказку.
    out, err, asks, spent, said = run([_st_owner_absent, _st_owner_absent, _st_ready])
    check("задержка переждана",
          out == _ST_CANON and err is None and asks == 3 and spent > 0,
          f"вернулось {out!r} (владелец назвал {_ST_CANON}), вопросов {asks}, "
          f"ждали {spent:.1f} с")

    # (а') ИНЪЕКЦИЯ: тот же сценарий на ДЕФЕКТЕ. Утверждение (а) обязано на нём
    #      покраснеть — иначе оно не про то, что чинится.
    out_d, err_d, asks_d, _sp_d, said_d = run([_st_owner_absent, _st_owner_absent, _st_ready],
                                  stage=_defective_stage_mirror)
    injected_red = not (out_d == _ST_CANON and err_d is None and asks_d == 3)
    check("инъекция дефекта краснеет",
          injected_red,
          f"довправочная стадия вернула {out_d!r} после {asks_d} вопрос(ов) владельцу "
          f"— утверждение (а) её отвергает" if injected_red else
          "довправочная стадия ПРОШЛА утверждение (а) — проверка потеряла свой предмет")

    # (б) ЗАКОННЫЙ БЛИЗНЕЦ: строка уже есть. Ровно один вопрос, никакого ожидания,
    #     никаких жалоб. Без этой половины отрицание (а') зеленело бы на всём.
    out, err, asks, spent, said = run([_st_ready])
    check("готовое зеркало не заставляют ждать",
          out == _ST_CANON and err is None and asks == 1 and spent == 0,
          f"вернулось {out!r}, вопросов {asks}, ждали {spent:.1f} с")

    # (б') ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА ПАРЫ К «ГОВОРИТ ВСЛУХ» (сценарии д/д'/е ниже):
    #      на подтверждённом резолве предупреждения мягкого прохода быть НЕ
    #      должно. Без этой половины утверждение «сказал вслух» зеленело бы и на
    #      стадии, которая жалуется всегда, — а такая жалоба не несёт информации.
    check("подтверждённый резолв МОЛЧИТ (предупреждения мягкого прохода нет)",
          _SAYS_HANDED_ON not in said and _SAYS_RACE_OPEN not in said,
          f"поток ошибок пуст на {len(said)} символ(ов)" if not said.strip()
          else f"в потоке ошибок лишнее: {said.strip()[:140]!r}")

    # (в) СТРОКИ НЕТ ВСЁ ОКНО: владелец ОТВЕТИЛ, и ответил «нет». Это различённый
    #     отказ — стадия встаёт САМА и называет свой предмет, а не отдаёт фантом.
    out, err, asks, spent, said = run([_st_owner_absent])
    check("строка не появилась — стадия встаёт сама",
          out is None and err is not None and _SAYS_ABSENT in str(err)
          and asks > 1 and spent >= MIRROR_BUDGET_S,
          f"ошибка {'есть' if err else 'НЕТ'}, вопросов {asks}, бюджет израсходован "
          f"{spent:.1f} из {MIRROR_BUDGET_S:.0f} с")

    # (г) ТЕРМИНАЛЬНЫЙ ОТВЕТ ВЛАДЕЛЬЦА (личность есть, аутентификация запрещена).
    #     Повтор того же вопроса пройти не может — бюджет на него не тратится.
    out, err, asks, spent, said = run([_st_blocked])
    check("терминальный отказ не переждают",
          out is None and err is not None and _SAYS_REFUSED in str(err)
          and asks == 1 and spent == 0,
          f"вопросов {asks}, ждали {spent:.1f} с, ошибка: {str(err)[:90]}")

    # (д) РУЧКИ ПО ЭТОМУ АДРЕСУ НЕТ — 404 маршрутизатора, а не владельца. Это
    #     НАСТРОЙКА: «спросить не удалось» ≠ «ответили нет». Посев не роняем,
    #     иначе смещённый порт стоил бы волне всех её коллекций; отдаём подсказку
    #     и говорим вслух, что гонка не закрыта. ЭТА ветка заменила прежнюю «403»,
    #     которой не могло прийти по построению.
    out, err, asks, spent, said = run([_st_route_absent])
    check("404 маршрутизатора отличают от 404 владельца",
          out == _ST_HINT and err is None and asks == 1 and spent == 0,
          f"вернулось {out!r} (подсказка), вопросов {asks}, ждали {spent:.1f} с — "
          f"посев не сорван")
    check("мягкий проход (нет маршрута) ГОВОРИТ ВСЛУХ", *said_aloud(said))

    # (д') ВТОРАЯ ФОРМА ТОГО ЖЕ: гейт не пустил спросить. Отличается от (г) не
    #      кодом, а ПРЕДМЕТОМ: там владелец сказал «нет» про человека, здесь до
    #      владельца не дошли. Свалить их в одну корзину значило бы сделать смену
    #      гейта причиной сорванного посева.
    out, err, asks, spent, said = run([_st_barred])
    check("«не пустили спросить» ≠ «владелец ответил нет»",
          out == _ST_HINT and err is None and asks == 1 and spent == 0,
          f"вернулось {out!r} (подсказка), вопросов {asks} — посев не сорван")

    # (е) ОТВЕТА НЕ ПОЛУЧЕНО ВОВСЕ: 5xx повторяют в пределах бюджета (вдруг
    #     временное), но неполученный ответ отказом не объявляют.
    out, err, asks, spent, said = run([_st_no_answer])
    check("неполученный ответ не выдают за отказ",
          out == _ST_HINT and err is None and asks > 1 and spent >= MIRROR_BUDGET_S,
          f"вернулось {out!r} (подсказка), вопросов {asks}, израсходовано {spent:.1f} с")
    # ВТОРОЙ производитель мягкого прохода — исчерпание бюджета без ответа. У него
    # своя строка вызова `_hand_on`, поэтому и утверждение своё: замолчать они
    # могут по отдельности.
    check("мягкий проход (ответа не получено) ГОВОРИТ ВСЛУХ", *said_aloud(said))

    # (ж) СМЕШАННОЕ ОКНО, ПАРА В ОБЕ СТОРОНЫ. Судит ПОСЛЕДНИЙ полученный исход,
    #     а не большинство: «под конец не дозвонились» доказательством отказа не
    #     является, «под конец владелец сказал нет» — является. Без второй
    #     половины первая зеленела бы и на правиле «никогда не роняем».
    out, err, asks, spent, said = run([_st_owner_absent, _st_no_answer])
    check("смешанное окно, последний исход — «не дозвонились»",
          out == _ST_HINT and err is None,
          f"вернулось {out!r}, ошибка {err}, вопросов {asks} — посев не сорван")

    out, err, asks, spent, said = run([_st_no_answer, _st_owner_absent])
    check("смешанное окно, последний исход — «строки нет»",
          out is None and err is not None and _SAYS_ABSENT in str(err),
          f"вернулось {out!r}, ошибка {'есть' if err else 'НЕТ'}, вопросов {asks} "
          f"— посев остановлен")

    # (з) ВЛАДЕЛЕЦ ОТВЕТИЛ 200, НО НЕ ЧЕЛОВЕКОМ. Это не «ещё нет»: он ответил, и
    #     ответил не тем. Ждать тут нечего — внешний идентификатор резолвится в
    #     чужую сущность, и предъявитель принадлежал бы не человеку.
    out, err, asks, spent, said = run([_st_not_a_human])
    check("200 не о человеке — это ответ, а не задержка",
          out is None and err is not None and _SAYS_NOT_HUMAN in str(err)
          and asks == 1 and spent == 0,
          f"вопросов {asks}, ждали {spent:.1f} с, ошибка: {str(err)[:110]}")

    # (и) ШЕСТОЙ ИСХОД, КОТОРОГО СТАДИЯ НЕ ЗНАЕТ. Заводят исход в классификаторе,
    #     а решить его в стадии забывают — стадия обязана назвать это, а не
    #     положить неизвестное в ожидание и не упасть без имени предмета.
    out, err, asks, spent, said = run([_st_ready],
                                lookup=lambda _a, _e: ("шестой-исход", "", "выдуман пробой"))
    check("неизвестный исход называет себя",
          out is None and err is not None and _SAYS_UNCLASSIFIED in str(err)
          and "шестой-исход" in str(err),
          f"ошибка: {str(err)[:120]}")

    # (к) СЛЕДСТВИЕ, ПАРА В ОБЕ СТОРОНЫ. Утверждения выше говорят, ЧТО стадия
    #     вернула; это — о том, ЧЕМ становится предъявитель. Красная половина
    #     воспроизводит подпись упавшего прогона дословно («… не тому человеку:
    #     None != …»), зелёная требует, чтобы после правки состав нёс человека.
    #     Без красной половины зелёная зеленела бы и на дефекте.
    def run_seq(stage):
        tally["сценариев"] += 1
        if stage is _defective_stage_mirror:
            tally["двойников"] += 1
        clock = {"t": 0.0}
        real_sleep2, real_monotonic2 = time.sleep, time.monotonic
        time.monotonic = lambda: clock["t"]
        time.sleep = lambda s: clock.__setitem__("t", clock["t"] + s)
        try:
            return _st_sequence(stage, clock), clock["t"]
        finally:
            time.sleep, time.monotonic = real_sleep2, real_monotonic2

    (claims_d, err_d), spent_d = run_seq(_defective_stage_mirror)
    check("довправочная последовательность роняет стадию 7 её собственной подписью",
          err_d is not None and "не тому человеку" in str(err_d)
          and "None !=" in str(err_d) and not claims_d,
          f"ошибка: {str(err_d)[:110]}" if err_d else
          "довправочная последовательность ПРОШЛА — проверка потеряла предмет")

    (claims_f, err_f), spent_f = run_seq(stage_mirror)
    check("после правки состав предъявителя несёт человека",
          err_f is None and claims_f.get("kacho_user_id") == _ST_CANON
          and spent_f >= _ST_COMMIT_AT_S,
          f"состав {claims_f}, ошибка {err_f}, ждали {spent_f:.1f} с "
          f"(строка коммитится на {_ST_COMMIT_AT_S} с)")

    # (л) ПОРЯДОК И ПРИВЯЗКА В `main`, а не только поведение стадии. Динамический
    #     замок выше гоняет стадию НАПРЯМУЮ и о `main` ничего не знает: перенос
    #     чеканки перед ожиданием оставлял ВСЕ прочие утверждения зелёными —
    #     измерено инъекцией, не предположено. Утверждение названо тем, что замок
    #     ПРОВЕРЯЕТ: не «чеканка не первая», а «чеканим тому, кого дождались».
    order_ok, order_detail = _st_mint_follows_mirror()
    check("предъявителя чеканят ТОМУ, чьё зеркало дождались (AST, все чеканящие функции)",
          order_ok, order_detail)

    # ПРЕДПОСЫЛКА САМОЙ ПРОВЕРКИ (1/2): стадия спрашивала ровно то, что обещала —
    # владельца, по ВНЕШНЕМУ идентификатору. Иначе проба меряет чужой вопрос.
    check("стадия спрашивала обещанное",
          not broken,
          "договор с подставным ответчиком соблюдён во всех прогонах" if not broken
          else f"нарушений {len(broken)}: {broken[0][:120]}")

    # ПРЕДПОСЫЛКА САМОЙ ПРОВЕРКИ (2/2): покрыты ВСЕ исходы классификатора. Появится
    # шестой — эта строка покраснеет раньше, чем он окажется непроверенным.
    declared = {_M_READY, _M_ABSENT, _M_REFUSED, _M_BARRED, _M_UNASKED}
    check("покрыты все исходы классификатора",
          covered == declared,
          f"осмотрено {len(covered)} из {len(declared)}: {sorted(covered)}"
          + ("" if covered == declared else f"; не покрыто {sorted(declared - covered)}"))

    # ── СТАДИЯ 8г: личности, заводящие аккаунт ───────────────────────────────
    #
    # Сеть здесь не трогается тем же приёмом, что выше: подменяются сами стадии, а
    # не их внутренности. Предмет проверки — не HTTP, а ТРИ свойства стадии:
    # (а) слоты берутся у объявления, а не выписаны здесь; (б) имена переменных
    # строятся его же помощниками; (в) предъявитель, чей состав называет ДРУГОГО
    # человека, стадию РОНЯЕТ, а не уезжает в волну.
    print("=== самопроверка стадии 8г: одна личность — один аккаунт ===")
    real_stages = (stage_identity, stage_password_login, stage_mirror,
                   stage_ceremony, stage_edge_accepts, _jwt_payload)
    minted: list[str] = []
    try:
        globals()["stage_identity"] = lambda slot="": (f"ident-{slot}", f"{slot}@x")
        globals()["stage_password_login"] = lambda email: f"ident-{email.split('@')[0]}"
        globals()["stage_mirror"] = lambda _a, ext, _e: f"usr-{ext}"
        globals()["stage_ceremony"] = lambda _c, subj: (f"l1-{subj}", f"l2-{subj}")
        globals()["stage_edge_accepts"] = lambda bearer: minted.append(bearer)
        globals()["_jwt_payload"] = lambda b: {
            "ext_claims": {"kacho_user_id": "usr-" + b[len("l1-"):]}}

        pool = stage_admission_pool("клиент", "админ")
        slots = list(cc.ADMISSION_SLOTS)
        want = set()
        for slot, _slug, _subject in slots:
            lvl1, lvl2 = cc.admission_bearers(slot)
            want |= {lvl1, lvl2, cc.admission_user_id_var(slot)}
        tally["сценариев"] += 1
        check("слоты взяты у объявления, а не выписаны в посеве",
              len(slots) > 0 and len(pool) == 3 * len(slots),
              f"слот(ов) {len(slots)}, переменных {len(pool)}")
        check("имена переменных строит объявление", set(pool) == want,
              f"лишние {sorted(set(pool) - want)}, недостающие {sorted(want - set(pool))}")
        check("край спрошен о КАЖДОМ выданном предъявителе", len(minted) == len(slots),
              f"проверок края {len(minted)} при слотах {len(slots)}")
        first_slot = slots[0][0]
        check("идентификатор человека отдан вместе с предъявителем",
              pool[cc.admission_user_id_var(first_slot)].startswith("usr-"),
              f"{first_slot}: {pool[cc.admission_user_id_var(first_slot)]}")

        # ЗАКОННЫЙ БЛИЗНЕЦ УЖЕ ПРОВЕРЕН ВЫШЕ (состав совпал — стадия прошла).
        # Теперь дефект во плоти: состав называет ДРУГОГО человека.
        globals()["_jwt_payload"] = lambda _b: {
            "ext_claims": {"kacho_user_id": "usr-кто-то-другой"}}
        try:
            stage_admission_pool("клиент", "админ")
            check("состав, называющий другого человека, роняет стадию", False,
                  "прошло молча — слот уехал бы в волну, и падение досталось бы кейсу")
        except StageError as exc:
            tally["двойников"] += 1
            check("состав, называющий другого человека, роняет стадию",
                  "принадлежит не тому человеку" in str(exc), str(exc)[:120])
    finally:
        (globals()["stage_identity"], globals()["stage_password_login"],
         globals()["stage_mirror"], globals()["stage_ceremony"],
         globals()["stage_edge_accepts"], globals()["_jwt_payload"]) = real_stages

    print()
    print(f"объём осмотренного: {tally['утверждений']} утверждени(й), "
          f"{tally['сценариев']} прогон(ов) стадии 6 (из них {tally['двойников']} — "
          f"на довправочном двойнике), {len(declared)} исход(ов) классификатора "
          f"покрыто {len(covered)}")
    label = ("стадия 6 ждёт подтверждения ВЛАДЕЛЬЦА; стадия 8г даёт одной "
             "личности один аккаунт")
    print(("PASS: " if rc == 0 else "FAIL: ") + label)
    return rc


if __name__ == "__main__":
    if "--self-test" in sys.argv[1:]:
        sys.exit(_self_test())
    sys.exit(main())
