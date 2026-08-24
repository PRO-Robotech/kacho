# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set basic-access-token — БАЗОВЫЙ ТОКЕН ДОСТУПА (задача #1142).

Приёмка BAT-1, сценарии BAT-1-09 (выдача), BAT-1-26 (предъявление на крае),
BAT-1-42 (отзыв доходит до предъявления), BAT-1-16/17 (срок).

ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ — И ЧЕГО НЕ ДОКАЗЫВАЕТ ЛЮБАЯ ПОЛОВИНА
------------------------------------------------------------
Вопрос ставится СКВОЗЬ ОБЕ СТОРОНЫ: выдали → предъявили → прошло → отозвали →
предъявили → отказ. Две пробы по половине («отзыв записался» и «отказ
приходит») не доказывают ничего: каждая сторона исправна, а вместе они про
разный предмет. Контроль, действующий на ВЫДАЧЕ и не действующий на
ПРЕДЪЯВЛЕНИИ, отзывом не является — у долгоживущего секрета цена такого
промаха равна его сроку.

Положительный контроль стоит в ТОМ ЖЕ прогоне и ПЕРЕД отрицанием: без него
«отказ после отзыва» было бы верно и о полосе, которая не работает вовсе.

ФОРМА ОТВЕТА ВЫДАЧИ У ЭТОГО ВИДА
--------------------------------
`secret` непуст; `privateKeyPem` / `publicKeyPem` / `algorithm` пусты ВСЕ.
Утверждается ПАРА — что есть и чего нет: одно «секрет непуст» зеленело бы на
ответе, который вдобавок отдал ключевой материал, то есть на том самом
смешении двух видов, ради разведения которого фаза и заведена.

СРОК ЗАПОЛНЕН ВСЕГДА. «Бессрочно» у этого вида не выражается ни одним входом:
ноль означает «срок не назван» и разрешается умолчанием политики.
"""

CASES = []

# ---------------------------------------------------------------------------
# BAT-1-09 + BAT-1-26 + BAT-1-42 — один кейс, потому что предмет один:
# удостоверение живёт от выдачи до отзыва, и разрезать это на три кейса значило
# бы утверждать три половины.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-BAT-SECRET-LIFECYCLE-OK",
    title=(
        "Базовый секрет выдаётся одной строкой, предъявляется краю как "
        "Authorization: Bearer, и ПОСЛЕ отзыва перестаёт проходить — вопрос "
        "поставлен сквозь обе стороны"
    ),
    classes=["CRUD", "SEC", "CONF"],
    priority="P0",
    steps=[
        Step(
            name="issue-basic-secret",
            method="POST",
            path="/iam/v1/users/{{userAAAId}}/tokens",
            auth="jwtAccountAdminAStepUp",
            body={
                "userId": "{{userAAAId}}",
                "name": "bat-{{runId}}",
                "description": "базовый секрет, сквозной кейс",
                "credentialKind": "CREDENTIAL_KIND_SECRET",
                "ttlSeconds": 2592000,
            },
            test_script=[
                *assert_answered("выдача базового секрета"),
                *assert_status(200),
                # Выдача этого вида завершается НА ПУТИ ЗАПРОСА: секрет
                # показывается один раз, и второго чтения у него нет.
                "pm.test('операция завершена в ответе самого Issue (done=true)', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j.done, 'operation.done — секрет показывается ОДИН раз,'",
                "    + ' второго чтения у него не существует').to.eql(true);",
                "  pm.expect(j.error, 'operation.error').to.be.undefined;",
                "});",
                "const _r = pm.response.json().response || {};",
                # ПАРА: что есть и чего нет.
                "pm.test('ответ несёт непустой секрет объявленной формы', () => {",
                "  pm.expect(_r.secret, 'response.secret').to.be.a('string')",
                "    .and.to.match(/^kacho_uoc_[0-9a-hjkmnp-tv-z]{17}_[0-9a-hjkmnp-tv-z]{32}$/);",
                "});",
                "pm.test('ответ вида SECRET НЕ несёт ключевого материала — ни в одном поле', () => {",
                "  pm.expect(_r.privateKeyPem || '', 'response.privateKeyPem').to.eql('');",
                "  pm.expect(_r.publicKeyPem || '', 'response.publicKeyPem').to.eql('');",
                "  pm.expect(_r.algorithm || '', 'response.algorithm').to.eql('');",
                "});",
                "pm.test('строка удостоверения объявляет свой вид и НЕСЁТ срок', () => {",
                "  pm.expect(_r.token && _r.token.credentialKind, 'token.credentialKind')",
                "    .to.eql('CREDENTIAL_KIND_SECRET');",
                "  pm.expect(_r.token && _r.token.expiresAt, 'token.expiresAt — бессрочного'",
                "    + ' секрета не бывает ни в каком написании').to.be.a('string')",
                "    .with.length.greaterThan(0);",
                "});",
                # Строка САМА называет своё удостоверение — второго имени нет.
                "pm.test('строка называет ТО ЖЕ удостоверение, что и ответ', () => {",
                "  const _parts = String(_r.secret).split('_');",
                "  const _id = _parts.slice(1, _parts.length - 1).join('_');",
                "  pm.expect(_id, 'идентификатор из строки').to.eql(_r.token.id);",
                "});",
                "pm.environment.set('batSecret', _r.secret);",
                "pm.environment.set('batCredId', _r.token.id);",
            ],
        ),
        # ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ — до отзыва, в том же прогоне. Обёрнут повтором
        # на 401/403: это ПЕРВЫЙ доступ к своему свежему удостоверению, и окно
        # материализации прав здесь законно.
        retry_until_authorized(Step(
            name="present-before-revoke",
            method="GET",
            path="/iam/v1/me",
            auth="batSecret",
            test_script=[
                *assert_answered("предъявление до отзыва"),
                "pm.test('живой базовый секрет принят краем как Bearer', () => {",
                "  pm.expect(pm.response.code, 'HTTP — 401 здесь означает, что полоса'",
                "    + ' секрета не приняла годную строку; 503 — что авторитет не ответил.'",
                "    + ' Тело: ' + pm.response.text()).to.eql(200);",
                "});",
                "pm.test('принципалом стал ВЛАДЕЛЕЦ удостоверения, а не кто-то ещё', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(JSON.stringify(j)).to.include(pm.environment.get('userAAAId'));",
                "});",
            ],
        )),
        Step(
            name="revoke-basic-secret",
            method="DELETE",
            path="/iam/v1/users/{{userAAAId}}/tokens/{{batCredId}}",
            auth="jwtAccountAdminAStepUp",
            test_script=[
                *assert_answered("отзыв базового секрета"),
                *assert_status(200),
                *assert_operation_envelope(),
                "pm.environment.set('opId', pm.response.json().id);",
            ],
            op_var="opId",
        ),
        poll_operation_until_done(auth="jwtAccountAdminAStepUp"),
        # ОТРИЦАНИЕ. Повтором НЕ оборачивается: повтор здесь маскировал бы
        # ровно тот дефект, ради которого кейс написан. Вместо повтора —
        # опрос ДО ОТКАЗА с дедлайном «окно вердикта плюс запас»: проба ждёт
        # УСЛОВИЯ, а не времени, и пауза длиной в окно закрепила бы конкретную
        # задержку вместо ГРАНИЦЫ, краснея на реализации, отвергающей быстрее.
        Step(
            name="present-after-revoke",
            method="GET",
            path="/iam/v1/me",
            auth="batSecret",
            pre_script=[
                "const _n = Number(pm.environment.get('_batRevokeTries') || 0);",
                "pm.environment.set('_batRevokeTries', _n + 1);",
            ],
            test_script=[
                *assert_answered("предъявление после отзыва"),
                # Дедлайн выведен из объявленного окна вердикта (5 с), а не
                # подобран: 16 попыток по 500 мс покрывают 8 с — окно плюс запас.
                "const _tries = Number(pm.environment.get('_batRevokeTries') || 1);",
                "const _DEADLINE = 16;",
                "if (pm.response.code === 200 && _tries < _DEADLINE) {",
                "  const _x = Date.now(); while (Date.now() - _x < 500) void 0;",
                "  postman.setNextRequest(pm.info.requestName);",
                "} else {",
                "  pm.environment.unset('_batRevokeTries');",
                "  pm.test('отозванный секрет ОТВЕРГНУТ на предъявлении в пределах'",
                "    + ' объявленного окна', () => {",
                "    pm.expect(pm.response.code, 'HTTP после отзыва — 200 означает, что'",
                "      + ' контроль действует на ВЫДАЧЕ и не действует на ПРЕДЪЯВЛЕНИИ:'",
                "      + ' выданное продолжает проходить до истечения СРОКА, и это'",
                "      + ' состояние не сходится само. Попыток: ' + _tries)",
                "      .to.be.oneOf([401, 403]);",
                "  });",
                "}",
            ],
        ),
    ],
))

# ---------------------------------------------------------------------------
# BAT-1-16 — срок сверх потолка ОТВЕРГАЕТСЯ, а не урезается молча.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-BAT-SECRET-TTL-ABOVE-CEILING-NEG",
    title="Срок базового секрета сверх потолка политики отвергается с именем поля, а не урезается",
    classes=["VAL", "NEG", "BVA"],
    priority="P1",
    steps=[
        Step(
            name="issue-above-ceiling",
            method="POST",
            path="/iam/v1/users/{{userAAAId}}/tokens",
            auth="jwtAccountAdminAStepUp",
            body={
                "userId": "{{userAAAId}}",
                "name": "bat-over-{{runId}}",
                "credentialKind": "CREDENTIAL_KIND_SECRET",
                # Потолок объявлен политикой как 90 суток; здесь — 91.
                "ttlSeconds": 7862400,
            },
            test_script=[
                *assert_answered("срок сверх потолка"),
                "pm.test('срок сверх потолка ОТВЕРГНУТ, а не применён урезанным', () => {",
                "  pm.expect(pm.response.code, 'HTTP — 200 означает, что вызывающий получил'",
                "    + ' успех при НЕПРИМЕНЁННОМ параметре: он уверен, что его срок'",
                "    + ' действует. Тело: ' + pm.response.text()).to.be.oneOf([400, 403]);",
                "});",
            ],
        ),
        # ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: срок РОВНО на потолке принимается — граница
        # включительна и проверена обеими сторонами. Без него «отвергнут» было
        # бы верно и о валидаторе, отвергающем всякий срок.
        Step(
            name="issue-at-ceiling",
            method="POST",
            path="/iam/v1/users/{{userAAAId}}/tokens",
            auth="jwtAccountAdminAStepUp",
            body={
                "userId": "{{userAAAId}}",
                "name": "bat-edge-{{runId}}",
                "credentialKind": "CREDENTIAL_KIND_SECRET",
                "ttlSeconds": 7776000,
            },
            test_script=[
                *assert_answered("срок ровно на потолке"),
                *assert_status(200),
                "pm.test('срок РОВНО на потолке принят — граница включительна', () => {",
                "  const _r = pm.response.json().response || {};",
                "  pm.expect(_r.secret, 'response.secret').to.be.a('string')",
                "    .with.length.greaterThan(0);",
                "});",
            ],
        ),
    ],
))
