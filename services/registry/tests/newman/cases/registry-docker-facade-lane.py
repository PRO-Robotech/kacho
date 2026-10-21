# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: the DOCKER lane of the single-facade rule (#59), probed where it lives.

WHAT PROPERTY THIS FILE PINS
============================
`.claude/rules/security.md` §«Production-mode обязателен ВЕЗДЕ» п.4 — iam is the
ONLY facade to the token-signing provider. This file owns one lane of that rule,
the docker one:

    A docker client never chooses its token server. It follows the `realm` of the
    WWW-Authenticate challenge the DATA PLANE hands it. So "the docker token goes
    through iam's handle" is, black-box, exactly two statements: the data plane's
    challenge names the facade handle, and that handle is a live gate at that
    address.

What "live gate" means changed with REG-PUB-1
(`docs/specs/sub-phase-REG-PUB-1-public-registry-by-class-grant-acceptance.md`,
Р9 п. 3, сценарий 52 близнец (б)): the anonymous pull flow is ON in every
deployment profile, so a caller with NO credentials gets `200` and a READ-ONLY
token (`scope = registry:pull`, `aud` = the data plane's service), and only a
caller with UNVERIFIABLE credentials gets the `401` challenge. The lane asserts
both halves; the earlier expectation "anonymous → 401" predates REG-PUB-1 and is
re-asserted, not dropped (see the steps).

The negative half is the point. A challenge naming the provider's `/oauth2/token`
would route every docker client around the facade — with no error anywhere, since
the provider would answer perfectly happily.

WHY THIS LANE LIVES IN THE REGISTRY SUITE AND NOT WITH ITS SEVEN SIBLINGS
=========================================================================
The other IBT lanes (verification, issuance, enrichment, and the provider-surface
negatives) dial only CORE — api-gateway, iam, the signing provider — and every
stand carries those. This one dials the registry DATA PLANE, which is a shard-gated
component: `deploy/e2e-shards.json` deploys `registry` on the `edge` shard only.

Written into the iam suite, the lane therefore asked for a service its own shard
deliberately removes. That is not a lane that occasionally flakes — it is a lane
with no producer on the runner that executes it, and the shape it took was worse
than a red case: the runner opened the port-forward unconditionally, the forward to
a non-existent service exited, and the run was declared INVALID before a single
suite started. Four shards of five reported `0/16 collections` that way (run
31344367968) — none of which had anything to do with this lane.

Moving the lane to the suite that runs where its subject is deployed fixes the
composition rather than the symptom: on the `edge` shard the registry data plane
AND iam (core, so present) are both up, and the transport is opened because a
collection of a running suite asks for it (`deploy/scripts/e2e-optional-transports.py`
derives that from the tree). The pairing "a suite that dials a component transport
runs only on shards declaring that component" is no longer a matter of care — it is
asserted by `deploy/scripts/assert-shard-coverage.py` (check 9).

The case ID keeps its IBT number. It is the one named in issue #59's Scope and in
`docs/specs/sub-phase-IAM-BOOTSTRAP-TOKEN-acceptance.md`; renaming it to a REG-*
number would move the lane and break its traceability in the same commit, and the
number is what the acceptance text can still be checked against.

HOW THE PROBES REACH WHAT THEY PROBE
====================================
Two endpoints here are not the api-gateway and are addressed through their own
base-URL variables, injected by the newman runner (`--env-var`) exactly like
`baseUrl`. A missing variable is a BROKEN HARNESS, never a legal mode:
`require_env_url` fails naming the variable and only then skips, so losing one
turns the suite RED instead of silently deleting a lane.

  {{registryDataPlaneBaseUrl}} the registry data plane (:8080), whose challenge
                               must name the facade handle.
  {{iamRegistryTokenBaseUrl}}  iam docker-token handle listener (:9096), server-TLS
                               with an internal-CA leaf → those steps carry
                               `insecure_tls`. The tunnel's trust chain is not the
                               subject; WHAT IS SERVED is.

Idempotence: the lane creates nothing. Its only fixture is `{{runId}}`, which it
puts in a scope string and a nonsense path so two concurrent runs cannot read each
other's answers. Nothing is left behind.

АДРЕСАТ ПОЛОСЫ СПРАШИВАЕТСЯ У ОБЕИХ ЕЁ СТОРОН, А НЕ ВЫПИСЫВАЕТСЯ (задача #1262)
================================================================================
Здесь четырежды стоял литерал `registry.kacho.local`. Он не краснел — и это было
не признаком исправности, а следствием того, что спросить о нём было НЕКОМУ: все
шаги, где он стоял, суть отрицания (401), а они получаются при ЛЮБОМ имени.
Литерал при этом НЕВЕРЕН: посадка объявляет адресата один раз
(`global.kacho.registry.serviceAud` = `registry.in-cloud.io`), а это имя было
снятым встроенным умолчанием процесса (#1184) — сегодня его нет ни в коде, ни в
одном профиле.

Почему отрицания его не ловили — доказуемо кодом, а не наблюдением: аноним
получал вызов на аутентификацию ДО всякого разбора адресата (анонимного потока
тогда не было), а при негодных учётных данных адресат решается ПОСЛЕ их проверки.
То есть `service=` в тех шагах не участвовал в исходе by construction. После
REG-PUB-1 аноним получает токен, и адресат слышен в его `aud`.

Поэтому имя теперь БЕРЁТСЯ ИЗ ВЫЗОВА полосы данных — ровно там, где берёт его
докер-клиент, — и рядом стоит ПОЛОЖИТЕЛЬНОЕ утверждение, зависящее от адресата:
обе стороны полосы обязаны назвать одно имя. Оно краснеет на расхождении и не
переживает смену имени в посадке, потому что утверждает согласие, а не значение.

ЦЕНА, КОТОРУЮ ЭТО ПРИНЕСЛО, НАЗВАНА ЗДЕСЬ, А НЕ ОБНАРУЖЕНА ПОТОМ
-----------------------------------------------------------------
Захват `ibtServiceAud` попадает под предикат генератора «переменная, записанная
предыдущим шагом, есть свежий ресурс», и шаги, подставляющие её в адрес,
автоматически оборачиваются в ожидание окна материализации прав (`-rya`).
Окна здесь нет: шаги анонимные, ресурса не создаётся, а `ibtServiceAud` — имя,
прочитанное из ответа, а не идентификатор. Обёртка НЕ маскирует (fail-open:
утверждения исполняются на терминальном ответе, вердикт тот же) и на зелёном пути
инертна — ни 200, ни 401 в её полосу не входят. На КРАСНОМ она стоит до 48 с на шаг (80×600мс),
и «advertised but not served» приезжает с этой задержкой. Предикат генератора шире
своего предмета — это отдельная находка, а не предмет этого файла.
"""

def _anon_claims():
    """Тело анонимной выдачи: токен — трёхсегментный JWS, его утверждения — в `_c`.

    Сам токен в сообщения проверок не печатается: на отказе печатаются ключи тела,
    а утверждения сверяются по полям (`aud`, `scope`, `sub`)."""
    return [
        "let _j = null; try { _j = pm.response.json(); } catch (e) { _j = null; }",
        "const _tok = (_j && (_j.token || _j.access_token)) || '';",
        "pm.test('the body carries the token as a three-segment JWS', () => "
        "pm.expect(_tok.split('.').length, 'body keys: ' + JSON.stringify(Object.keys(_j || {}))).to.eql(3));",
        "let _c = null;",
        "try {",
        "  const _cj = require('crypto-js');",
        "  const _b = _tok.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');",
        "  _c = JSON.parse(_cj.enc.Utf8.stringify(_cj.enc.Base64.parse(_b)));",
        "} catch (e) { _c = null; }",
    ]


CASES = []


# ===========================================================================
# IBT-14 — the docker lane is addressed at the FACADE handle.
# ===========================================================================

CASES.append(Case(
    id="IBT-14-DOCKER-CHALLENGE-NAMES-THE-FACADE-HANDLE",
    title="Registry data-plane 401 challenge advertises the iam /iam/token handle (never the provider token endpoint), and that handle is a live gate: read-only anonymous token minted for the data plane's audience, garbage credentials refused with the same realm",
    # Техники: ECP по удостоверению (нет · негодное), один изменённый факт против
    # близнеца (запрошенное действие pull против push; удостоверение нет против
    # негодного), сверка двух сторон полосы (адресат вызова против `aud` токена),
    # контроль слушателя посторонним путём. Приёмка — REG-PUB-1 Р9 п. 3, 52 (б).
    classes=["SEC", "CONF", "NEG", "AUTHN"],
    priority="P0",
    steps=[
        Step(
            name="dataplane-anonymous-challenge",
            method="GET",
            path="/v2/",
            auth="anonymous",
            pre_script=require_env_url(
                "registryDataPlaneBaseUrl", "/v2/",
                "docker lane — the challenge a docker client actually follows"),
            test_script=[
                *assert_answered("registry data-plane challenge"),
                "pm.test('anonymous /v2/ is challenged with 401 (a 200 here would mean the data '",
                "  + 'plane needs no token at all)', () => pm.expect(pm.response.code, pm.response.text()).to.eql(401));",
                "const _wa = pm.response.headers.get('WWW-Authenticate') || '';",
                "pm.test('the challenge is a Bearer token-auth challenge', () => {",
                "  pm.expect(_wa, 'WWW-Authenticate header').to.match(/^Bearer /);",
                "  pm.expect(_wa, _wa).to.match(/service=\"[^\"]+\"/);",
                "});",
                # АДРЕСАТ НЕ ВЫПИСЫВАЕТСЯ, А БЕРЁТСЯ ОТСЮДА — как берёт его докер-клиент.
                # Имя проверялось здесь и раньше, но ЗНАЧЕНИЕ выбрасывалось, а шаги ниже
                # подставляли свой литерал. Захват делает эти шаги зависимыми от того,
                # что полоса данных действительно называет.
                "const _svc = (_wa.match(/service=\"([^\"]+)\"/) || [])[1] || '';",
                "pm.test('the challenge NAMES the audience the docker client will quote back',",
                "  () => pm.expect(_svc, _wa).to.be.a('string').with.length.greaterThan(0));",
                # Пустое не записывается: страж неразрешённой подстановки назовёт пропажу
                # по имени, а пустая строка уехала бы в адрес и вернулась чужим отказом.
                "if (_svc) { pm.environment.set('ibtServiceAud', _svc); }",
                "const _m = _wa.match(/realm=\"([^\"]+)\"/);",
                "pm.test('the challenge names a realm', () => pm.expect(_m && _m[1], _wa).to.be.a('string'));",
                "const _realm = (_m && _m[1]) || '';",
                "const _path = _realm.replace(/^[a-z]+:\\/\\/[^/]+/i, '');",
                "pm.test('the realm PATH is the facade docker-token handle', () => {",
                "  pm.expect(_path, 'realm ' + _realm + ' — the docker client follows this address; "
                "it must be the facade handle').to.eql('/iam/token');",
                "});",
                "pm.test('the realm is NOT the provider token endpoint (that would route every '",
                "  + 'docker client around the facade, silently)', () => {",
                "  pm.expect(_realm.toLowerCase(), _realm).to.not.contain('/oauth2/');",
                "});",
                "pm.environment.set('ibtRealmPath', _path);",
            ],
        ),
        # ── ОБЕ СТОРОНЫ ПОЛОСЫ НАЗЫВАЮТ ОДНО ИМЯ: положительное утверждение ──
        #
        # Шаги ниже прежде ждали 401 на анонимный вход: анонимного потока не было
        # ни в одной посадке. Приёмка REG-PUB-1 (Р9 п. 3, сценарий 52, близнец (б))
        # включила его во всех профилях развёртывания: вход без удостоверения
        # получает `200` и токен ТОЛЬКО ЧТЕНИЯ. Ожидание 401 здесь — от времени до
        # REG-PUB-1, и оно переутверждено, а не ослаблено: каждое прежнее свойство
        # полосы утверждается и теперь, в том шаге, где оно наблюдаемо.
        #
        # ПОЧЕМУ ЗАПРОС БЕЗ `service`. Ручка ЭХАЕТ присланное `service=` в адресат
        # (резолв адресата kaname: пустой параметр — законный вход, адресат берётся
        # из умолчания посадки). Сверять ответ шага, который сам это имя прислал,
        # значило бы утверждать «я прислал X — мне ответили X». Умолчание ручки
        # слышно ТОЛЬКО на запросе без параметра — и теперь оно читается не из
        # вызова на аутентификацию, а из `aud` выданного токена, то есть ровно из
        # того, что сверит приёмная сторона.
        #
        # ЧТО ЭТО ДОКАЗЫВАЕТ. Докер-клиент берёт `service` из вызова плоскости
        # данных, фасад чеканит адресат в `aud`. Разойдись стороны — анонимный
        # токен не примет плоскость данных, и ни одно отрицание этого не заметит.
        # Обе величины СПРАШИВАЮТСЯ у своих сторон: утверждение про согласие, а не
        # про значение, и смену имени в посадке переживает.
        Step(
            name="facade-default-audience-is-the-data-plane-audience",
            method="GET",
            path="/iam/token",
            auth="anonymous",
            insecure_tls=True,
            pre_script=require_env_url(
                "iamRegistryTokenBaseUrl", "/iam/token",
                "докер-полоса — у неё же и спрашиваем, кому она чеканит"),
            test_script=[
                *assert_answered("facade docker-token handle, no service parameter"),
                "pm.test('the handle issues an anonymous token to a caller with no credentials "
                "(REG-PUB-1 Р9 п. 3; 401 = anonymous flow off on this landing)',",
                "  () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                *_anon_claims(),
                "pm.test('BOTH sides of the lane name the SAME audience — the data plane "
                "tells it to the docker client, the facade mints it into aud; this request "
                "named none, so aud is the facade default, not an echo', () => {",
                "  pm.expect(_c && _c.aud, 'facade aud ' + JSON.stringify(_c && _c.aud) + ', data plane says \"'",
                "    + pm.environment.get('ibtServiceAud') + '\"')",
                "    .to.eql([pm.environment.get('ibtServiceAud')]);",
                "});",
            ],
        ),
        Step(
            name="facade-token-handle-serves-the-anonymous-pull-flow",
            method="GET",
            path="/iam/token?service={{ibtServiceAud}}&scope=repository:ibt/{{runId}}:pull",
            auth="anonymous",
            insecure_tls=True,
            pre_script=require_env_url(
                "iamRegistryTokenBaseUrl",
                "/iam/token?service={{ibtServiceAud}}&scope=repository:ibt/{{runId}}:pull",
                "docker lane — the facade handle the challenge points at"),
            test_script=[
                *assert_answered("facade docker-token handle"),
                # 200 и не 404: 404 значило бы, что ручки, которую называет плоскость
                # данных, по этому адресу нет вовсе, и всякий docker pull без входа
                # падал бы непонятным «not found».
                "pm.test('the handle EXISTS at the advertised address and serves the anonymous "
                "pull flow (404 = advertised but not served; 401 = anonymous flow off)', () => "
                "pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                *_anon_claims(),
                # Предмет «только чтения» — объём токена, а не его наличие: приёмка
                # (Р9, расширение поверхности названо) обещает анониму токен только
                # чтения; контракт выдачи kaname (SC-39 (а)) — `scope = registry:pull`.
                "pm.test('the anonymous token is read-only: scope is exactly registry:pull', () => "
                "pm.expect(_c && _c.scope, JSON.stringify(_c)).to.eql('registry:pull'));",
                # Субъект — объявленный принципал закрытой формы (SC-39), а не
                # порождённый id и не пустая строка: такой `sub` плоскость данных
                # переводит в класс `anonymous`, и только его.
                "pm.test('the token subject is the declared anonymous principal of the closed form, "
                "not a generated id', () => pm.expect(_c && _c.sub, JSON.stringify(_c))"
                ".to.be.a('string').and.match(/^[a-z][a-z0-9]*(-[a-z0-9]+)+$/));",
            ],
        ),
        # ── ОТРИЦАТЕЛЬНЫЙ БЛИЗНЕЦ: аноним просит запись — токен всё равно только чтения ──
        #
        # Отличие от шага выше — один факт: запрошенное действие (`push` вместо
        # `pull`). Ручка не обязана отказать — анонимный поток объём запроса не
        # читает, — но обязана не выдать глагола записи: иначе «токен только чтения»
        # держался бы тем, что клиент вежливо попросил чтение.
        Step(
            name="facade-anonymous-push-request-still-gets-read-only",
            method="GET",
            path="/iam/token?service={{ibtServiceAud}}&scope=repository:ibt/{{runId}}:pull,push",
            auth="anonymous",
            insecure_tls=True,
            pre_script=require_env_url(
                "iamRegistryTokenBaseUrl",
                "/iam/token?service={{ibtServiceAud}}&scope=repository:ibt/{{runId}}:pull,push",
                "docker lane — anonymous caller asking for a write verb"),
            test_script=[
                *assert_answered("facade docker-token handle, anonymous push request"),
                "pm.test('the anonymous flow answers a write request with a token, not an error', () => "
                "pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                *_anon_claims(),
                "pm.test('asking for push does not widen the anonymous token: scope is still exactly "
                "registry:pull', () => pm.expect(_c && _c.scope, JSON.stringify(_c)).to.eql('registry:pull'));",
            ],
        ),
        Step(
            name="facade-token-handle-rejects-garbage-credentials",
            method="GET",
            path="/iam/token?service={{ibtServiceAud}}&scope=repository:ibt/{{runId}}:pull",
            auth="anonymous",
            insecure_tls=True,
            pre_script=require_env_url(
                "iamRegistryTokenBaseUrl",
                "/iam/token?service={{ibtServiceAud}}&scope=repository:ibt/{{runId}}:pull",
                "docker lane — the handle must verify credentials, not merely accept them") + [
                # Deliberately unmistakable as a real credential: a plausible-looking one
                # would make a passing substitution indistinguishable from a correct flow
                # ([[realistic-fixture-hides-the-defect-it-feeds]]).
                "pm.request.headers.upsert({key: 'Authorization', value: 'Basic ' +",
                "  CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse(",
                "    'ibt-not-a-real-key:ibt-not-a-real-secret'))});",
            ],
            test_script=[
                *assert_answered("facade docker-token handle, garbage credentials"),
                # Отличие от шага «serves the anonymous pull flow» — один факт:
                # предъявлено негодное удостоверение. Включённый анонимный поток его не
                # подменяет: негодное удостоверение анонимом не становится.
                "pm.test('unverifiable credentials are refused, never brokered into a token',",
                "  () => pm.expect(pm.response.code, pm.response.text()).to.eql(401));",
                "pm.test('no token was handed out', () => {",
                "  let j = null; try { j = pm.response.json(); } catch (e) { j = null; }",
                "  pm.expect(j && (j.token || j.access_token), JSON.stringify(j)).to.be.oneOf([undefined, null]);",
                "});",
                # Вызов на аутентификацию называет ФАСАД: тот же путь ручки, что
                # называет плоскость данных. Прежде это утверждалось на анонимном
                # 401; анонимный вход теперь получает токен, и вызов наблюдаем только
                # на отказе удостоверения.
                "const _wa = pm.response.headers.get('WWW-Authenticate') || '';",
                "pm.test('the handle re-challenges with its own realm, and it is the same handle', () => {",
                "  const m = _wa.match(/realm=\"([^\"]+)\"/);",
                "  pm.expect(m && m[1], 'WWW-Authenticate: ' + _wa).to.be.a('string');",
                "  const p = (m && m[1] || '').replace(/^[a-z]+:\\/\\/[^/]+/i, '');",
                "  pm.expect(p, 'the facade advertises ' + p + ' while the data plane advertises '",
                "    + pm.environment.get('ibtRealmPath')).to.eql(pm.environment.get('ibtRealmPath'));",
                "});",
            ],
        ),
        Step(
            name="facade-token-listener-serves-only-its-handle",
            method="GET",
            path="/ibt-nonsense-{{runId}}",
            auth="anonymous",
            insecure_tls=True,
            pre_script=require_env_url(
                "iamRegistryTokenBaseUrl", "/ibt-nonsense-{{runId}}",
                "docker lane — control proving the answers above are the HANDLE answering, "
                "not a listener that answers every path alike"),
            test_script=[
                *assert_answered("facade token listener, nonsense path"),
                # Without this control, "200 / 401 on /iam/token" is satisfied by a
                # listener that answers every path the same way, including ones that do
                # not exist — i.e. by a handle that is not there.
                "pm.test('a path that is NOT the handle is 404, so the answers above came from the "
                "handle itself', () => pm.expect(pm.response.code, pm.response.text()).to.eql(404));",
            ],
        ),
    ],
))
