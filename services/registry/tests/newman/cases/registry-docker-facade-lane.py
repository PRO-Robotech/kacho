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

Почему отрицания его не ловят — доказуемо кодом, а не наблюдением: аноним
получает вызов на аутентификацию ДО всякого разбора адресата, а при негодных
учётных данных адресат решается ПОСЛЕ их проверки. То есть `service=` в этих двух
шагах не участвует в исходе by construction.

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
инертна — 401 в её полосу не входит. На КРАСНОМ она стоит до 48 с на шаг (80×600мс),
и «advertised but not served» приезжает с этой задержкой. Предикат генератора шире
своего предмета — это отдельная находка, а не предмет этого файла.
"""

CASES = []


# ===========================================================================
# IBT-14 — the docker lane is addressed at the FACADE handle.
# ===========================================================================

CASES.append(Case(
    id="IBT-14-DOCKER-CHALLENGE-NAMES-THE-FACADE-HANDLE",
    title="Registry data-plane 401 challenge advertises the iam /iam/token handle (never the provider token endpoint), and that handle is a live gate",
    classes=["SEC", "CONF", "NEG"],
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
        # Прежде здесь не было НИ ОДНОГО утверждения, зависящего от адресата: три
        # шага ниже — отрицания (401/404), а они получаются при ЛЮБОМ имени, в том
        # числе заведомо неверном. Литерал `registry.kacho.local`, стоявший в них,
        # неверен и сегодня: посадка объявляет адресата один раз
        # (`global.kacho.registry.serviceAud`, действующее значение —
        # `registry.in-cloud.io`), а это имя было СНЯТЫМ встроенным умолчанием
        # процесса (задача #1184). Ложь была невидима ровно потому, что спросить о
        # ней было некому.
        #
        # ПОЧЕМУ ОТДЕЛЬНЫЙ ШАГ, А НЕ СВЕРКА В ШАГЕ НИЖЕ. Ручка ЭХАЕТ присланное
        # `service=` в свой вызов на аутентификацию (`handler.go`: имя берётся из
        # запроса, умолчание подставляется только когда запрос его не назвал).
        # Сверять ответ шага, который сам это имя и прислал, значило бы утверждать
        # «я прислал X — мне ответили X»: тавтология на месте проверки, то есть
        # ровно тот класс, ради снятия которого правится этот файл. Умолчание
        # ручки слышно ТОЛЬКО на запросе БЕЗ параметра.
        #
        # ЧТО ЭТО ДОКАЗЫВАЕТ. Докер-клиент берёт `service` из вызова полосы данных
        # и предъявляет его фасаду; фасад чеканит его в `aud`. Разойдись стороны —
        # поток сломан у всякого клиента, и ни одно отрицание этого не заметит.
        # Обе величины СПРАШИВАЮТСЯ у своих сторон, поэтому утверждение не
        # переживёт смену имени в посадке: оно про согласие, а не про значение.
        Step(
            name="facade-names-the-same-audience-as-the-data-plane",
            method="GET",
            path="/iam/token",
            auth="anonymous",
            insecure_tls=True,
            pre_script=require_env_url(
                "iamRegistryTokenBaseUrl", "/iam/token",
                "докер-полоса — у неё же и спрашиваем, кому она чеканит"),
            test_script=[
                *assert_answered("facade docker-token handle, no service parameter"),
                # 401 — тот же исход, что у соседних шагов: анонимная выдача не
                # включена. 200 означало бы включённую анонимную выдачу, и тогда
                # адресата надо брать иначе — молчать об этом нельзя.
                "pm.test('the handle challenges an anonymous caller (401)',",
                "  () => pm.expect(pm.response.code, pm.response.text()).to.eql(401));",
                "const _fwa = pm.response.headers.get('WWW-Authenticate') || '';",
                "const _fsvc = (_fwa.match(/service=\"([^\"]+)\"/) || [])[1] || '';",
                "pm.test('the handle NAMES the audience it mints for — its own default, "
                "not an echo: this request named none', () => {",
                "  pm.expect(_fsvc, 'WWW-Authenticate: ' + _fwa).to.be.a('string')",
                "    .with.length.greaterThan(0);",
                "});",
                # Положительное утверждение, зависящее от адресата: разойдись имена —
                # докер-клиент предъявит фасаду имя, которого тот не знает, и получит
                # отказ, неотличимый от неверных учётных данных.
                "pm.test('BOTH sides of the lane name the SAME audience — the data plane "
                "tells it to the docker client, the facade mints it into aud', () => {",
                "  pm.expect(_fsvc, 'facade says \"' + _fsvc + '\", data plane says \"'",
                "    + pm.environment.get('ibtServiceAud') + '\"')",
                "    .to.eql(pm.environment.get('ibtServiceAud'));",
                "});",
            ],
        ),
        Step(
            name="facade-token-handle-is-a-gate",
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
                # 401 and not 404 is the whole assertion: 404 would mean the handle the
                # data plane advertises is not served at that address at all, and every
                # docker client would fail with a confusing "not found" instead of a
                # credential prompt.
                "pm.test('the handle EXISTS and refuses an anonymous caller (404 = advertised but '",
                "  + 'not served; 200 = anonymous issuance)', () => pm.expect(pm.response.code, pm.response.text()).to.eql(401));",
                "const _wa = pm.response.headers.get('WWW-Authenticate') || '';",
                "pm.test('the handle re-challenges with its own realm, and it is the same handle', () => {",
                "  const m = _wa.match(/realm=\"([^\"]+)\"/);",
                "  pm.expect(m && m[1], _wa).to.be.a('string');",
                "  const p = (m && m[1] || '').replace(/^[a-z]+:\\/\\/[^/]+/i, '');",
                "  pm.expect(p, 'the facade advertises ' + p + ' while the data plane advertises '",
                "    + pm.environment.get('ibtRealmPath')).to.eql(pm.environment.get('ibtRealmPath'));",
                "});",
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
                "pm.test('unverifiable credentials are refused, never brokered into a token',",
                "  () => pm.expect(pm.response.code, pm.response.text()).to.eql(401));",
                "pm.test('no token was handed out', () => {",
                "  let j = null; try { j = pm.response.json(); } catch (e) { j = null; }",
                "  pm.expect(j && j.token, JSON.stringify(j)).to.be.oneOf([undefined, null]);",
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
                "docker lane — control proving the 401s above are the HANDLE answering, "
                "not a listener that refuses everything"),
            test_script=[
                *assert_answered("facade token listener, nonsense path"),
                # Without this control, "401 on /iam/token" is satisfied by a listener
                # that 401s every path, including ones that do not exist — i.e. by a
                # handle that is not there.
                "pm.test('a path that is NOT the handle is 404, so the 401s above came from the "
                "handle itself', () => pm.expect(pm.response.code, pm.response.text()).to.eql(404));",
            ],
        ),
    ],
))
