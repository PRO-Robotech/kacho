# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: AGPL-3.0-or-later

"""Case-set для собственного REST-фронта службы (KAN-REST-1).

Предмет — СОБСТВЕННАЯ HTTP-поверхность службы, а не край платформы. Вынесенная
отдельным продуктом, служба края не имеет by construction: её REST обязан
существовать сам, и адресуется он ОТДЕЛЬНОЙ переменной окружения.

  {{ownRestBaseUrl}}          — собственный ПУБЛИЧНЫЙ фронт (:9098)
  {{ownInternalRestBaseUrl}}  — собственный ВНУТРЕННИЙ фронт (:9099)

ПОЧЕМУ ОТДЕЛЬНЫЕ ПЕРЕМЕННЫЕ, А НЕ ПЕРЕОПРЕДЕЛЕНИЕ `baseUrl`. Край и собственный
фронт — две разные поверхности, и одно значение на обе сделало бы вердикт
прогона функцией того, чей стенд поднят. Переменные края здесь не трогаются.

ЧТО РАЗЛИЧАЕТ 404 НА ЭТОЙ ПОВЕРХНОСТИ — И ЧТО НЕТ. Промах маршрутизатора и
сокрытие существования несут ОДИН И ТОТ ЖЕ код 5, и это намеренно: различимый
текст сообщал бы, существует ли объект. Поэтому:

  * `code: 5` различителем НЕ является. Промах маршрутизатора отвечает голым
    `"Not Found"` — тела он не видел; владелец называет ресурс и идентификатор;
  * утверждение «внутренний путь отвечает 404» само по себе не доказывает
    НИЧЕГО: тот же 404 придёт от мёртвого слушателя и от опечатки в пути.
    Поэтому каждое такое утверждение идёт В ПАРЕ с положительным контролем на
    том же фронте И с тем же путём на СОСЕДНЕМ фронте.

Coverage:
  IAM-OWNREST-OK-PUBLIC-ALIVE      — КОНТРОЛЬ: публичный фронт обслуживает REST (200)
  IAM-OWNREST-NEG-INTERNAL-ON-PUB  — внутренний путь на публичном фронте → 404, промах маршрутизатора
  IAM-OWNREST-OK-INTERNAL-ON-INT   — тот же путь на внутреннем фронте → НЕ 404 (парный близнец)
  IAM-OWNREST-NEG-UNKNOWN-PATH     — неизвестный путь → 404 + code 5
  IAM-OWNREST-NEG-METHOD-MISMATCH  — известный путь, другой метод → 501 + code 12 (НЕ 405)
  IAM-OWNREST-NEG-BAD-CREDENTIAL   — негодное удостоверение → 401 + code 16, один текст на все причины
  IAM-OWNREST-NEG-SELF-NAMED       — вызывающий называет себя сам → субъект от этого не меняется
  IAM-OWNREST-NEG-GARBAGE-CURSOR   — мусорный курсор → 400 независимо от того, что выдано
  IAM-OWNREST-OK-OPERATION-POLL    — мутация возвращает операцию, и операция читается ТЕМ ЖЕ фронтом

Test-first: кейсы написаны RED-first против фронта, которого не было. Прогон
против неподнятого стенда — «не выполнилось», третья категория: он не
вычитается из вердикта и в зелёное не засчитывается.
"""

CASES = []

_PUBLIC_WHY = ("собственный публичный REST-фронт службы; без него у поверхности нет "
               "адреса, и кейс проверял бы край платформы вместо предмета")
_INTERNAL_WHY = ("собственный внутренний REST-фронт службы; без него пара "
                 "«тот же путь на соседнем фронте» распадается, и любой 404 на "
                 "публичном фронте становится неотличим от мёртвого слушателя")

# Внутренний путь, существование которого доказывается на соседнем фронте.
_INTERNAL_PATH = "/iam/v1/internal/iam:lookupSubject"


def _own(path, why=_PUBLIC_WHY):
    return require_env_url("ownRestBaseUrl", path, why)


def _own_internal(path):
    return require_env_url("ownInternalRestBaseUrl", path, _INTERNAL_WHY)


def _mux_miss(label):
    """Промах МАРШРУТИЗАТОРА, а не владельца.

    Различитель — ТЕКСТ: маршрутизатор отвечает голым `Not Found`, потому что
    тела он не видел; владелец называет ресурс и идентификатор. Свести их
    значило бы засчитать настоящий отказ владельца за изоляцию маршрута.
    """
    return [
        *assert_status(404),
        "const j = pm.response.json();",
        f"pm.test('{label}: grpc code 5 (необходимое условие, но НЕ различитель — "
        f"его несёт и промах маршрутизатора, и сокрытие существования)', () => "
        "pm.expect(j.code, JSON.stringify(j)).to.eql(5));",
        f"pm.test('{label}: это промах МАРШРУТИЗАТОРА — голый Not Found без имени "
        "ресурса; ответ владельца назвал бы ресурс и идентификатор', () => "
        "pm.expect(j.message, JSON.stringify(j)).to.eql('Not Found'));",
    ]


# ───────────────────────────────────────────────────────────────────────────
# КОНТРОЛЬ. Без него ни один 404 ниже ничего не доказывает: он был бы
# удовлетворён фронтом, который просто не поднят.
# ───────────────────────────────────────────────────────────────────────────
CASES.append(Case(
    id="IAM-OWNREST-OK-PUBLIC-ALIVE",
    title="Контроль: собственный публичный фронт обслуживает REST (значит 404 ниже означает «не смаршрутизировано», а не «ничего нет»)",
    classes=["SEC"],
    priority="P0",
    steps=[
        Step(
            name="own-public-front-serves-rest",
            method="GET",
            path="/iam/v1/accounts/{{existingAccountId}}",
            pre_script=_own("/iam/v1/accounts/{{existingAccountId}}"),
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('PUB-ALIVE: тело несёт запрошенный ресурс', () => {",
                "  pm.expect(j, JSON.stringify(j)).to.have.property('id');",
                "  pm.expect(j.id, JSON.stringify(j)).to.eql(pm.environment.get('existingAccountId'));",
                "});",
                "pm.test('PUB-ALIVE: и отметку создания — то есть это ответ владельца, а не заглушка края', () =>",
                "  pm.expect(j, JSON.stringify(j)).to.have.property('createdAt'));",
            ],
        ),
    ],
))

# ───────────────────────────────────────────────────────────────────────────
# РАЗДЕЛЬНОСТЬ ФРОНТОВ. Пара: один путь, два фронта, различие — РОВНО адрес.
# ───────────────────────────────────────────────────────────────────────────
CASES.append(Case(
    id="IAM-OWNREST-NEG-INTERNAL-ON-PUB",
    title="Внутренний путь на ПУБЛИЧНОМ фронте: промах маршрутизатора (недосягаем, а не отклонён по дороге)",
    classes=["NEG", "SEC"],
    priority="P0",
    steps=[
        Step(
            name="internal-path-on-public-front",
            method="POST",
            path=_INTERNAL_PATH,
            body={"externalId": "zit-probe-{{runId}}"},
            pre_script=_own(_INTERNAL_PATH),
            # Удостоверение ГОДНОЕ намеренно: без него отказ пришёл бы раньше
            # маршрутизации, и утверждение стало бы «посторонний не пройдёт»
            # вместо «АУТЕНТИФИЦИРОВАННЫЙ не дотянется до служебной поверхности».
            auth="jwtAccountAdminA",
            test_script=_mux_miss("INT-ON-PUB"),
        ),
    ],
))

CASES.append(Case(
    id="IAM-OWNREST-OK-INTERNAL-ON-INT",
    title="Тот же путь на ВНУТРЕННЕМ фронте обслуживается — парный близнец, отличается ровно адресом фронта",
    classes=["SEC"],
    priority="P0",
    steps=[
        Step(
            name="internal-path-on-internal-front",
            method="POST",
            path=_INTERNAL_PATH,
            body={"externalId": "zit-probe-{{runId}}"},
            pre_script=_own_internal(_INTERNAL_PATH),
            auth="jwtAccountAdminA",
            test_script=[
                *assert_answered("INT-ON-INT"),
                # НЕ 404: обслуженный исход либо отказ, произведённый цепочкой
                # внутреннего слушателя. Ровно это и отличает «маршрута здесь
                # нет» от «слушателя нет вовсе».
                "pm.test('INT-ON-INT: путь на внутреннем фронте НЕ отвечает промахом маршрутизатора — "
                "значит 404 на публичном произведён отсутствием маршрута, а не отсутствием службы', () => {",
                "  const j = pm.response.json();",
                "  const muxMiss = pm.response.code === 404 && j && j.message === 'Not Found';",
                "  pm.expect(muxMiss, JSON.stringify({code: pm.response.code, body: j})).to.eql(false);",
                "});",
            ],
        ),
    ],
))

# ───────────────────────────────────────────────────────────────────────────
# ГРАНИЦА ПОВЕРХНОСТИ И МНОЖЕСТВО СТАТУСОВ.
# ───────────────────────────────────────────────────────────────────────────
CASES.append(Case(
    id="IAM-OWNREST-NEG-UNKNOWN-PATH",
    title="Неизвестный путь на собственном фронте → 404 + code 5",
    classes=["NEG"],
    priority="P1",
    steps=[
        Step(
            name="nonsense-path-on-own-front",
            method="GET",
            path="/iam/v1/zzz-no-such-collection",
            pre_script=_own("/iam/v1/zzz-no-such-collection"),
            auth="jwtAccountAdminA",
            test_script=_mux_miss("UNKNOWN-PATH"),
        ),
    ],
))

CASES.append(Case(
    id="IAM-OWNREST-NEG-METHOD-MISMATCH",
    title="Известный путь, метод которого у него нет → 501 + code 12; 405 не производится НИЧЕМ",
    classes=["NEG"],
    priority="P1",
    steps=[
        Step(
            name="known-path-wrong-method",
            method="DELETE",
            path="/iam/v1/accounts",
            pre_script=_own("/iam/v1/accounts"),
            auth="jwtAccountAdminA",
            test_script=[
                # Утверждение — ПАРА: 501 приходит и от UNIMPLEMENTED обработчика,
                # поэтому одного статуса мало.
                *assert_status(501),
                "const j = pm.response.json();",
                "pm.test('METHOD-MISMATCH: grpc code 12 — это отказ МАРШРУТИЗАТОРА, а не обработчика', () =>",
                "  pm.expect(j.code, JSON.stringify(j)).to.eql(12));",
                "pm.test('METHOD-MISMATCH: и НЕ 405 — такого статуса край не производит ни при каком входе', () =>",
                "  pm.expect(pm.response.code, JSON.stringify(j)).to.not.eql(405));",
            ],
        ),
    ],
))

# ───────────────────────────────────────────────────────────────────────────
# ЛИЧНОСТЬ. Один отказ на все причины; назвать себя самому нельзя.
# ───────────────────────────────────────────────────────────────────────────
CASES.append(Case(
    id="IAM-OWNREST-NEG-BAD-CREDENTIAL",
    title="Негодное удостоверение → 401 + code 16 с единственным текстом: какая половина неверна, ответ не сообщает",
    classes=["NEG", "SEC"],
    priority="P0",
    steps=[
        Step(
            name="malformed-credential",
            method="GET",
            path="/iam/v1/accounts/{{existingAccountId}}",
            pre_script=_own("/iam/v1/accounts/{{existingAccountId}}") + [
                "pm.request.headers.upsert({key: 'Authorization', value: 'Bearer not-a-credential'});",
            ],
            test_script=[
                *assert_status(401),
                "const j = pm.response.json();",
                "pm.test('BAD-CRED: grpc code 16 (UNAUTHENTICATED)', () =>",
                "  pm.expect(j.code, JSON.stringify(j)).to.eql(16));",
                "pm.test('BAD-CRED: текст ЕДИНСТВЕННЫЙ — различимость сообщила бы предъявителю, "
                "какая половина предъявленного неверна', () =>",
                "  pm.expect(j.message, JSON.stringify(j)).to.eql('credential is not accepted'));",
                "pm.environment.set('ownRestRefusalBody', JSON.stringify(j));",
            ],
        ),
        Step(
            name="expired-shaped-credential",
            method="GET",
            path="/iam/v1/accounts/{{existingAccountId}}",
            pre_script=_own("/iam/v1/accounts/{{existingAccountId}}") + [
                "pm.request.headers.upsert({key: 'Authorization', value: 'Bearer eyJhbGciOiJSUzI1NiJ9.e30.bad'});",
            ],
            test_script=[
                *assert_status(401),
                "pm.test('BAD-CRED: второй негодный предъявитель даёт ПОБАЙТОВО тот же отказ', () => {",
                "  const first = pm.environment.get('ownRestRefusalBody');",
                "  pm.expect(first, 'первый отказ не захвачен — сравнивать не с чем').to.be.a('string').and.not.empty;",
                "  pm.expect(JSON.stringify(pm.response.json())).to.eql(first);",
                "});",
            ],
        ),
    ],
))

CASES.append(Case(
    id="IAM-OWNREST-NEG-SELF-NAMED",
    title="Вызывающий называет себя сам — обеими формами: субъект от этого не меняется",
    classes=["NEG", "SEC"],
    priority="P0",
    steps=[
        Step(
            name="bare-and-bridged-principal-headers",
            method="GET",
            path="/iam/v1/authorize:whoAmI",
            pre_script=_own("/iam/v1/authorize:whoAmI") + [
                "// ОБЕ формы, которыми вызывающий мог бы назвать себя. Мостовая",
                "// существует НЕЗАВИСИМО от голой: умолчание библиотеки снимает",
                "// префикс само, поэтому отсутствия голой недостаточно.",
                "pm.request.headers.upsert({key: 'X-Kacho-Principal-Id', value: 'usr-someone-else'});",
                "pm.request.headers.upsert({key: 'X-Kacho-Principal-Type', value: 'user'});",
                "pm.request.headers.upsert({key: 'Grpc-Metadata-X-Kacho-Principal-Id', value: 'usr-someone-else'});",
                "pm.request.headers.upsert({key: 'Grpc-Metadata-X-Kacho-Principal-Type', value: 'user'});",
            ],
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('SELF-NAMED: субъект назван УДОСТОВЕРЕНИЕМ, а не заголовком вызывающего', () => {",
                "  const body = JSON.stringify(j);",
                "  pm.expect(body).to.not.include('usr-someone-else');",
                "});",
            ],
        ),
    ],
))

# ───────────────────────────────────────────────────────────────────────────
# ПОРЯДОК ФОРМЫ И ЗАМЫКАНИЯ. Ответ на негодный ввод не зависит от того, что
# вызывающему выдано.
# ───────────────────────────────────────────────────────────────────────────
CASES.append(Case(
    id="IAM-OWNREST-NEG-GARBAGE-CURSOR",
    title="Мусорный курсор отвергается одинаково у того, кому выдано, и у того, кому не выдано ничего",
    classes=["NEG", "VAL"],
    priority="P1",
    steps=[
        Step(
            name="garbage-cursor-granted-caller",
            method="GET",
            path="/iam/v1/accounts?pageToken=not-a-cursor",
            pre_script=_own("/iam/v1/accounts?pageToken=not-a-cursor"),
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(400),
                "pm.test('GARBAGE-CURSOR: grpc code 3 (INVALID_ARGUMENT)', () =>",
                "  pm.expect(pm.response.json().code, pm.response.text()).to.eql(3));",
            ],
        ),
        Step(
            name="garbage-cursor-ungranted-caller",
            method="GET",
            path="/iam/v1/accounts?pageToken=not-a-cursor",
            pre_script=_own("/iam/v1/accounts?pageToken=not-a-cursor"),
            # Тот же ввод, другой арендатор — различие РОВНО в том, что ему выдано.
            auth="jwtPureNoBindings",
            test_script=[
                *assert_status(400),
                "pm.test('GARBAGE-CURSOR: тот же 400 у того, кому не выдано ничего — "
                "иначе ответ на один негодный ввод зависел бы от прав вызывающего', () =>",
                "  pm.expect(pm.response.json().code, pm.response.text()).to.eql(3));",
            ],
        ),
    ],
))

# ───────────────────────────────────────────────────────────────────────────
# АСИНХРОННЫЙ КОНТРАКТ. Без маршрутов операции клиент получает идентификатор,
# по которому некуда обратиться.
# ───────────────────────────────────────────────────────────────────────────
CASES.append(Case(
    id="IAM-OWNREST-OK-OPERATION-POLL",
    title="Мутация возвращает операцию, и операция читается ТЕМ ЖЕ фронтом (иначе асинхронный контракт неисполним)",
    classes=["CRUD"],
    priority="P0",
    steps=[
        Step(
            name="create-group-through-own-front",
            method="POST",
            path="/iam/v1/groups",
            body={"accountId": "{{accountAId}}", "name": "ownrest-{{runId}}"},
            pre_script=_own("/iam/v1/groups"),
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('OP-POLL: мутация вернула операцию с идентификатором', () => {",
                "  pm.expect(j, JSON.stringify(j)).to.have.property('id');",
                "  pm.expect(j.id, JSON.stringify(j)).to.be.a('string').and.not.empty;",
                "});",
                "pm.environment.set('ownRestOpId', j.id);",
            ],
        ),
        Step(
            name="poll-operation-on-the-same-front",
            method="GET",
            path="/operations/{{ownRestOpId}}",
            pre_script=_own("/operations/{{ownRestOpId}}") + [
                "// Шаг, создавший предмет, обязан был его захватить. Пустая",
                "// переменная означает, что мутация не удалась, — и тогда падать",
                "// обязан ЭТОТ шаг с именем переменной, а не следующий по цепочке.",
                "if (!pm.environment.get('ownRestOpId')) {",
                "  throw new Error('ownRestOpId пуст: создание группы не вернуло операцию — "
                "предмет кейса не создан, и опрашивать нечего');",
                "}",
            ],
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('OP-POLL: операция читается тем же фронтом, что её породил', () =>",
                "  pm.expect(j.id, JSON.stringify(j)).to.eql(pm.environment.get('ownRestOpId')));",
                "pm.test('OP-POLL: и несёт признак завершённости — иначе клиенту некуда обратиться за исходом', () =>",
                "  pm.expect(j, JSON.stringify(j)).to.have.property('done'));",
            ],
        ),
    ],
))
