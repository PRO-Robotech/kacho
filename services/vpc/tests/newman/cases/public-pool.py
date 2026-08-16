# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для ПУБЛИЧНОГО AddressPoolService (под-фаза ADM-1, стадия S1).

Предмет — наблюдение владельца на живом стенде: раздел администрирования консоли
отвечал `404`, потому что управление пулами адресов жило ТОЛЬКО на внутреннем
крае, а браузер на внутренний край не ходит. S1 ставит публичный глагол на ТОТ ЖЕ
канонический путь и закрывает вызывающего без права, а не место вызова.

Отличие от `internal-pool.py`, который остаётся и НЕ переписывается:
  * там `internal=True` — запрос идёт на cluster-internal listener (:8081),
    мутация отвечает РЕСУРСОМ синхронно;
  * здесь запрос идёт на публичный cmux (:8080), мутация отвечает `Operation`
    (запрет 9), а чтение синхронно.

Оба набора живут рядом ровно столько, сколько живёт окно расширения (S1→S3).
Стадия S3 снимает внутренний глагол — и вместе с ним `internal-pool.py` и
кейс эквивалентности ниже: проба снимается ВМЕСТЕ со своим предметом.

Parallel-safety — те же правила, что у `internal-pool.py`, и по тем же причинам
(EXCLUDE по CIDR глобален на вид пула, partial-UNIQUE на `(zone,kind)` при
`is_default`). Этот набор берёт СВОЙ блок `100.103.0.0/16`, не пересекающийся ни
с блоком набора internal-pool (`100.100.0.0/16`), ни с блоком набора адресов
(`100.101.0.0/16`), ни с посевом nlb (`100.102.<октет зоны>.0/24`). Пулы
создаются без `is_default`, поэтому в partial-UNIQUE не участвуют вовсе и
serial-очередь этому набору не нужна.

REST через край — тело camelCase.
"""

CASES = []

POOLS = "/vpc/v1/addressPools"


# ---------------------------------------------------------------------------
# Класс 1. Административный глагол достижим снаружи (сценарии ADM-1-01, 03)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="ADM-1-01-PUB-POOL-CREATE-OK",
    title="Администратор создаёт пул с ВНЕШНЕГО края → Operation → пул читается",
    classes=["CRUD", "CONF"], priority="P0",
    steps=[
        # Шаг создания УТВЕРЖДАЕТ СВОЙ ИСХОД, а не только захватывает id.
        # Operation несёт пред-выделенный идентификатор ДАЖЕ на `done=true` с
        # `error`, поэтому «переменная непуста» ресурса не доказывает.
        Step(name="create", method="POST", path=POOLS,
             auth="jwtBootstrap",
             body={"name": "adm1-pub-{{runId}}", "kind": "EXTERNAL_PUBLIC",
                   "zoneId": "{{zoneA}}",
                   "v4CidrBlocks": ["100.103.7.0/24"], "v6CidrBlocks": []},
             test_script=[*assert_status(200),
                          *assert_operation_envelope(),
                          "const j = pm.response.json();",
                          "pm.test('мутация отвечает Operation, а не ресурсом', () => "
                          "pm.expect(j).to.have.property('id'));",
                          "pm.environment.set('adm1PoolOp', j.id);",
                          "pm.environment.set('adm1PoolId', "
                          "(j.metadata && (j.metadata.addressPoolId || j.metadata.address_pool_id)) || '');"]),
        poll_operation_until_done(id_expr="pm.environment.get('adm1PoolOp')"),
        Step(name="get-after-create", method="GET", path=POOLS + "/{{adm1PoolId}}",
             auth="jwtBootstrap",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('id заполнен', () => pm.expect(j.id).to.match(/^apl/));",
                          "pm.test('имя совпадает', () => pm.expect(j.name).to.eql('adm1-pub-' + pm.environment.get('runId')));",
                          "pm.test('createdAt заполнен', () => pm.expect(j.createdAt).to.be.a('string'));",
                          "pm.test('блоки echoed', () => pm.expect(j.v4CidrBlocks).to.eql(['100.103.7.0/24']));"]),
    ],
))


CASES.append(Case(
    id="ADM-1-03-PUB-POOL-TENANT-DENIED",
    title="Арендатор без права администратора не читает админский инвентарь; администратор читает",
    classes=["NEG", "CONF"], priority="P0",
    steps=[
        # ОТРИЦАНИЕ И ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ — В ОДНОМ КЕЙСЕ.
        #
        # Межсценарная пара распадается при любом частичном прогоне (--folder,
        # шардирование, снятый по времени набор), и тогда «отказ на всё»
        # выглядит исправным: ресурс, недоступный НИКОМУ, удовлетворяет
        # отрицанию полностью.
        Step(name="tenant-list-denied", method="GET", path=POOLS,
             auth="jwtPureNoBindings",
             test_script=[*assert_status(403),
                          *assert_grpc_code(7, "PERMISSION_DENIED"),
                          "pm.test('это отказ, а не пустая страница', () => "
                          "pm.expect(pm.response.json()).to.not.have.property('pools'));"]),
        Step(name="tenant-get-denied", method="GET", path=POOLS + "/{{adm1PoolId}}",
             auth="jwtPureNoBindings",
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED")]),
        Step(name="admin-list-ok", method="GET", path=POOLS,
             auth="jwtBootstrap",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('администратор видит свой пул', () => pm.expect("
                          "(j.pools || []).map(p => p.id)).to.include(pm.environment.get('adm1PoolId')));"]),
    ],
))


# ---------------------------------------------------------------------------
# Класс 3. Полосы отказа (сценарий ADM-1-11)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="ADM-1-11-PUB-POOL-LANES",
    title="Негодный по форме id → 400/INVALID_RESOURCE_ID; корректный, но отсутствующий → 404/RESOURCE_NOT_FOUND",
    classes=["NEG", "VAL", "CONF"], priority="P1",
    steps=[
        # Обе формы корректного id — слитная и дефисная. Router классифицирует их
        # АДДИТИВНО, и полоса не вправе зависеть от формы.
        Step(name="absent-legacy-form", method="GET", path=POOLS + "/apl00000000000000000",
             auth="jwtBootstrap",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('контрактный тон', () => pm.expect(pm.response.json().message)"
                          ".to.eql('AddressPool apl00000000000000000 not found'));",
                          "pm.test('признак полосы прямого чтения', () => pm.expect(JSON.stringify("
                          "pm.response.json().details || [])).to.include('RESOURCE_NOT_FOUND'));"]),
        Step(name="absent-hyphen-form", method="GET", path=POOLS + "/apl-00000000000000000",
             auth="jwtBootstrap",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('полоса не зависит от формы id', () => pm.expect(JSON.stringify("
                          "pm.response.json().details || [])).to.include('RESOURCE_NOT_FOUND'));"]),
        Step(name="malformed", method="GET", path=POOLS + "/not-an-id",
             auth="jwtBootstrap",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('признак полосы формата', () => pm.expect(JSON.stringify("
                          "pm.response.json().details || [])).to.include('INVALID_RESOURCE_ID'));"]),
    ],
))


# ---------------------------------------------------------------------------
# Класс 7. Окно расширения: два пути записи в одну таблицу (сценарий ADM-1-19)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="ADM-1-19-PUB-POOL-EQUIVALENCE",
    title="Публичный и внутренний глаголы эквивалентны по объявленным полям, пока живы оба",
    classes=["CONF"], priority="P0",
    steps=[
        # ЕДИНСТВЕННАЯ проверка такого рода во всей приёмке, и она обязательна на
        # всё время окна S1→S3.
        #
        # Два пути записи в одну таблицу — это класс двойной записи. Расхождение
        # между ними (разная валидация, разные умолчания, разный набор
        # заполняемых полей) не проявляется НИ В ОДНОМ сценарии, который смотрит
        # на один путь. Наблюдаемым оно становится ровно тогда, когда консоль уже
        # переведена на публичный путь, а оператор ещё пользуется внутренним.
        #
        # Утверждается РАВЕНСТВО ПО ОБЪЯВЛЕННЫМ ПОЛЯМ, а не присутствие id:
        # присутствие удовлетворяется любым непустым ответом.
        Step(name="create-public", method="POST", path=POOLS,
             auth="jwtBootstrap",
             body={"name": "adm1-eq-pub-{{runId}}", "kind": "EXTERNAL_PUBLIC",
                   "zoneId": "{{zoneA}}",
                   "v4CidrBlocks": ["100.103.8.0/24"], "v6CidrBlocks": []},
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          "const j = pm.response.json();",
                          "pm.environment.set('adm1EqOp', j.id);",
                          "pm.environment.set('adm1EqPubId', "
                          "(j.metadata && (j.metadata.addressPoolId || j.metadata.address_pool_id)) || '');"]),
        poll_operation_until_done(id_expr="pm.environment.get('adm1EqOp')"),
        # Созданное ПУБЛИЧНЫМ глаголом видно ВНУТРЕННИМ чтением — те же поля.
        Step(name="read-internal", method="GET", path=POOLS + "/{{adm1EqPubId}}", internal=True,
             auth="jwtBootstrap",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.environment.set('adm1EqInternalBody', JSON.stringify({"
                          "id: j.id, name: j.name, kind: j.kind, zoneId: j.zoneId,"
                          " v4CidrBlocks: j.v4CidrBlocks || [], v6CidrBlocks: j.v6CidrBlocks || []}));",
                          "pm.test('внутреннее чтение видит созданное публичным глаголом', () => "
                          "pm.expect(j.id).to.eql(pm.environment.get('adm1EqPubId')));"]),
        Step(name="read-public", method="GET", path=POOLS + "/{{adm1EqPubId}}",
             auth="jwtBootstrap",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "const pub = JSON.stringify({id: j.id, name: j.name, kind: j.kind,"
                          " zoneId: j.zoneId, v4CidrBlocks: j.v4CidrBlocks || [],"
                          " v6CidrBlocks: j.v6CidrBlocks || []});",
                          "pm.test('оба пути отдают ОДИН ресурс по объявленным полям', () => "
                          "pm.expect(pub).to.eql(pm.environment.get('adm1EqInternalBody')));"]),
        # Обратное направление: созданное ВНУТРЕННИМ глаголом видно публичным.
        # Без него эквивалентность доказывалась бы в одну сторону, а расходятся
        # пути записи именно в ту, которую не проверили.
        Step(name="create-internal", method="POST", path=POOLS, internal=True,
             auth="jwtBootstrap",
             body={"name": "adm1-eq-int-{{runId}}", "kind": "EXTERNAL_PUBLIC",
                   "zoneId": "{{zoneA}}",
                   "v4CidrBlocks": ["100.103.9.0/24"], "v6CidrBlocks": []},
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('внутренний глагол отвечает РЕСУРСОМ синхронно', () => "
                          "pm.expect(j.id).to.match(/^apl/));",
                          "pm.environment.set('adm1EqIntId', j.id);"]),
        Step(name="read-public-of-internal", method="GET", path=POOLS + "/{{adm1EqIntId}}",
             auth="jwtBootstrap",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('публичное чтение видит созданное внутренним глаголом', () => "
                          "pm.expect(j.name).to.eql('adm1-eq-int-' + pm.environment.get('runId')));",
                          "pm.test('доменные поля те же', () => pm.expect(j.v4CidrBlocks).to.eql(['100.103.9.0/24']));"]),
        # Уборка за собой — обоих пулов, обоими путями.
        Step(name="cleanup-pub", method="DELETE", path=POOLS + "/{{adm1EqPubId}}",
             auth="jwtBootstrap", test_script=[*assert_status(200)]),
        Step(name="cleanup-int", method="DELETE", path=POOLS + "/{{adm1EqIntId}}", internal=True,
             auth="jwtBootstrap", test_script=[*assert_status(200)]),
    ],
))


CASES.append(Case(
    id="ADM-1-01-PUB-POOL-CLEANUP",
    title="Уборка пула набора — публичным глаголом, с проверкой исхода операции",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="delete", method="DELETE", path=POOLS + "/{{adm1PoolId}}",
             auth="jwtBootstrap",
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          "pm.environment.set('adm1DelOp', pm.response.json().id);"]),
        poll_operation_until_done(id_expr="pm.environment.get('adm1DelOp')"),
        Step(name="get-after-delete", method="GET", path=POOLS + "/{{adm1PoolId}}",
             auth="jwtBootstrap",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))
