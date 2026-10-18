# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Чтение реестра без входа одной выдачей роли классу субъектов — край чёрным ящиком.

Приёмка: `docs/specs/sub-phase-REG-PUB-1-public-registry-by-class-grant-acceptance.md`
воркспейса, редакция 10, SHA-256 `a582030ed6c8e6ed3d1ef720910fec90eb815b9c6fd9703c920913af4a00956f`
(задача PRO-Robotech/kacho#3274). Признака «публичный» у реестра нет (Р2): что выдано,
то и видно. Здесь — сценарии S1 (02, 03), S2 (10, 11, 12, 17, 18, 30, 31) и стендовая
часть S3 (13, 14, 20–27, 29). REG-PUB-1-01 — гейты дерева Go, REG-PUB-1-28 — integration
пакета `dataplane`, 50 и 51 — шаг с настоящим клиентом `docker` (S4); край их не создаёт.

ПРЕДУСЛОВИЯ СТЕНДА, БЕЗ КОТОРЫХ КЕЙСЫ КРАСНЫЕ ПО ПОСТРОЕНИЮ (порядок Р9 приёмки):
  - роль `registry.public.read` в каталоге (S1, `kacho#3251`) — для 02, 03 и всего, что
    выдаёт её;
  - анонимный поток плоскости данных: ручка анонимного субъекта обеих сторон и образ
    службы доступа с полосой анонимного токена (S3, S4) — для 13, 14, 20–27, 29.
Без них шаги посева называют отказ сами («условие не создано»), а не роняют сценарий
чужим текстом.

Посев П2. Принципалы — из `tests/authz-fixtures/prodseed_matrix.py`:
  - выдающий `jwtAccountAdminA` — администратор аккаунта A, право выдать на проекте A1,
    токен служебной учётки (SC-27 kaname); его учётка — `svaAccountAdminAId`;
  - редактор `jwtProjectEditorA` — editor на A1, права выдать нет;
  - посторонний `jwtStranger` — вошедший без выдач в аккаунте A.
Реестры X и Y — в `{{existingProjectId}}`, создаёт выдающий. Репозитории `X/app`, `Y/app`
заливаются через плоскость данных базовым удостоверением выдающего (вид SECRET,
`SAKeyService.Issue`) до любой выдачи; `Y/pub` заводится `CreateRepository` с
`visibility=PUBLIC` (поштучная публикация, D-7) и заливается так же.

«Выдача ПЧ» — тело приёмки дословно: роль `registry.public.read`
(`rolb1325b011aa17211d`) классу `anonymous` на проекте с целью X.

«Материализовано» — опрос раз в 200 мс с пределом 32 с (окно SC-13 kaname 30 с плюс окно
кеша вердиктов registry 2 с) до ожидаемого ответа. Ожидается УСЛОВИЕ, а не время:
признак материализации выдачи классу — чтение реестра посторонним (говорящий с
личностью несёт класс `anonymous`, Р10).

Классы свода (`testing-newman.md#qa-classes-per-method`). Предмет — не метод registry, а
действие выдачи на чтение: AUTHZ (кто вправе открыть, кто читает открытое, кто не
читает), STATE (выдача → открыт, отзыв → закрыт, удаление реестра выдачу не трогает),
CONF (тело роли; ответ реестра не несёт `visibility`), NEG (анонимный край, правка
посторонним, соседний реестр, запись анонимом). Неприменимы: VAL и BVA (у registry нет
поля или ручки публичности — Р2, вход не меняется), PAGE и FILTER (страница и фильтр
List не меняются; каталог плоскости данных обходится по `Link` до конца — 23), IDM и
CONC (повтор активной выдачи — SC-06 kaname, вне registry).

Уборка заводится ДО создания: выдачи снимаются своими кейсами, Y, роли и ключ выдающего —
REG-PUB-1-CLEANUP; X снимает REG-PUB-1-17.
"""

import json

CASES = []

REG = "/registry/v1/registries"
GRANTS = "/iam/v1/accessBindings"
ROLES = "/iam/v1/roles"

# Операции registry несут префикс rop/reo (маршрутизация opsproxy края).
_OP_PREFIX = "^(rop|reo)[a-z0-9]+$"

ISSUER = "jwtAccountAdminA"
EDITOR = "jwtProjectEditorA"
STRANGER = "jwtStranger"

PUBLIC_READ_ROLE = "rolb1325b011aa17211d"

# Транспорты, которые передаёт прогонщик (`--env-var`).
DP = "registryDataPlaneBaseUrl"
TOKEN_LANE = "iamRegistryTokenBaseUrl"

# Окно материализации: 160 опросов × 200 мс = 32 с.
_MAT_CAP = 160
_MAT_DELAY_MS = 200

_X = "{{regPubX}}"
_Y = "{{regPubY}}"

_MANIFEST_MT = "application/vnd.oci.image.manifest.v1+json"


def _grant_body(role_id, subject_class="anonymous", target="{{regPubX}}"):
    """Тело «выдача ПЧ» приёмки (П2); роль, класс и цель — параметры близнецов."""
    return {
        "subjects": [{"type": "SUBJECT_TYPE_SYSTEM_CLASS", "id": subject_class}],
        "roleId": role_id,
        "scopeType": "iam.project",
        "scopeId": "{{existingProjectId}}",
        "target": {"resources": {"resources": [{"type": "registry.registries", "id": target}]}},
    }


def _js(v):
    return json.dumps(v, ensure_ascii=False)


def _json_or_null():
    return ["let _j; try { _j = pm.response.json(); } catch (e) { _j = null; }"]


def _bearer_from(var):
    """Заголовок Bearer из переменной, выставленной этой же коллекцией."""
    return [f"pm.request.headers.upsert({{key: 'Authorization', value: 'Bearer ' + (pm.environment.get('{var}') || '')}});"]


def _dp(path, token_var=None):
    """Пред-скрипт шага плоскости данных: адрес из транспорта и, если назван, токен."""
    pre = require_env_url(DP, path, "плоскость данных реестра")
    if token_var:
        pre += _bearer_from(token_var)
    return pre


def _poll_until(name, method, path, auth, cond_js, what, pre=None):
    """Опрос до УСЛОВИЯ с настоящей паузой между запросами (окно материализации П2).

    Условие не сошлось за окно — шаг краснеет утверждением `what` и печатает
    последний ответ: «не дошло» отличимо от «не спрашивали»."""
    key = "_rp_" + name.replace("-", "_")
    return Step(
        name=name, method=method, path=path, auth=auth, pre_script=list(pre or []),
        test_script=[
            *_json_or_null(),
            f"const _ok = Boolean({cond_js});",
            f"const _n = parseInt(pm.environment.get('{key}') || '0', 10);",
            f"if (!_ok && _n < {_MAT_CAP}) {{",
            f"  pm.environment.set('{key}', String(_n + 1));",
            "  const _pd = Date.now();",
            f"  while (Date.now() - _pd < {_MAT_DELAY_MS}) {{ /* настоящая пауза между опросами */ }}",
            "  pm.execution.setNextRequest(pm.info.requestName);",
            "  return;",
            "}",
            f"pm.environment.unset('{key}');",
            f"pm.test({_js(what)}, () => pm.expect(_ok, pm.response.code + ' ' + pm.response.text()).to.eql(true));",
        ],
    )


def _create_registry(suffix, var):
    return [
        Step(name=f"create-registry-{suffix}", method="POST", path=REG, auth=ISSUER,
             body={"name": f"regpub-{suffix}-{{{{runId}}}}", "projectId": "{{existingProjectId}}",
                   "regionId": "{{existingRegionId}}", "description": "REG-PUB-1 fixture"},
             test_script=[*assert_status(200), *assert_operation_envelope(_OP_PREFIX), *save_operation_id()]),
        poll_operation_until_done(auth=ISSUER, capture_id_to=var,
                                  id_expr="(j.response && j.response.id) || ''"),
        Step(name=f"registry-captured-{suffix}", method="GET", path=f"{REG}/{{{{{var}}}}}", auth=ISSUER,
             test_script=[
                 f"pm.test({_js('Дано П2: реестр ' + suffix + ' создан, id захвачен (префикс reg)')}, () => "
                 f"pm.expect(pm.environment.get('{var}') || '').to.match(/^reg[a-z0-9]+$/));",
                 *assert_status(200)]),
    ]


def _iam_mutation(name, method, path, body, op_var, what, auth=ISSUER):
    """Мутация iam: синхронно Operation; иначе условие сценария не создано, и шаг
    называет это сам — отказ ниже есть отказ ПОСЕВА, а не сценария."""
    return Step(name=name, method=method, path=path, auth=auth, body=body,
                test_script=[
                    *_json_or_null(),
                    f"pm.test({_js(what)}, () => {{",
                    "  pm.expect(pm.response.code, pm.response.text()).to.eql(200);",
                    "  pm.expect(_j && _j.id, pm.response.text()).to.be.a('string').and.not.empty;",
                    "});",
                    f"pm.environment.unset('{op_var}');",
                    f"if (_j && _j.id) pm.environment.set('{op_var}', _j.id);",
                ])


def _grant(name, body, var, auth=ISSUER):
    """Выдача: синхронно Operation, опрос до done без error, захват id привязки."""
    op_var = "_op_" + var
    return [
        _iam_mutation(f"grant-{name}", "POST", GRANTS, body, op_var,
                      f"Дано П2: выдача {name} принята (200 и Operation) — иначе условие сценария не создано",
                      auth=auth),
        poll_operation_until_done(auth=auth, op_var=op_var, capture_id_to=var,
                                  id_expr="(j.response && j.response.id) || ''"),
    ]


def _revoke(name, var, auth=ISSUER):
    op_var = "_op_revoke_" + var
    return [
        _iam_mutation(f"revoke-{name}", "DELETE", f"{GRANTS}/{{{{{var}}}}}", None, op_var,
                      f"отзыв выдачи {name} принят (200 и Operation)", auth=auth),
        poll_operation_until_done(auth=auth, op_var=op_var),
        Step(name=f"revoke-{name}-no-error", method="GET", path=f"/operations/{{{{{op_var}}}}}", auth=auth,
             test_script=[*assert_status(200), *_json_or_null(),
                          f"pm.test({_js('отзыв ' + name + ': done без error')}, () => {{",
                          "  pm.expect(_j && _j.done, pm.response.text()).to.eql(true);",
                          "  pm.expect(_j && _j.error, pm.response.text()).to.not.exist;",
                          "});"]),
    ]


def _stranger_reads_x(name, what):
    """Признак материализации выдачи классу: посторонний читает X (Р10)."""
    return _poll_until(f"materialized-{name}", "GET", f"{REG}/{_X}", STRANGER,
                       "pm.response.code === 200 && _j && _j.id === pm.environment.get('regPubX')", what)


def _create_role(name, rules, var, what):
    op_var = "_op_" + var
    return [
        _iam_mutation(f"create-role-{name}", "POST", ROLES,
                      {"accountId": "{{accountAId}}", "name": f"regpub{name.replace('-', '')}{{{{runId}}}}",
                       "description": "REG-PUB-1 fixture role", "rules": rules},
                      op_var, what),
        poll_operation_until_done(auth=ISSUER, op_var=op_var, capture_id_to=var,
                                  id_expr="(j.response && j.response.id) || (j.metadata && j.metadata.roleId) || ''"),
    ]


# ---------------------------------------------------------------------------
# Плоскость данных: вызов на аутентификацию, токены, заливка образа.
# ---------------------------------------------------------------------------

def _challenge():
    """Имя службы полосы и путь её ручки берутся ИЗ ВЫЗОВА плоскости данных — там же,
    где их берёт докер-клиент; литерал адресата здесь не пишется."""
    return Step(
        name="dp-challenge", method="GET", path="/v2/", auth="anonymous",
        pre_script=_dp("/v2/"),
        test_script=[
            *assert_answered("плоскость данных реестра, вызов на аутентификацию"),
            "pm.test('Дано П2: /v2/ без удостоверения — 401 с вызовом Bearer', () => "
            "pm.expect(pm.response.code, pm.response.text()).to.eql(401));",
            "const _wa = pm.response.headers.get('WWW-Authenticate') || '';",
            "const _svc = (_wa.match(/service=\"([^\"]+)\"/) || [])[1] || '';",
            "const _realm = (_wa.match(/realm=\"([^\"]+)\"/) || [])[1] || '';",
            "pm.test('Дано П2: вызов называет службу полосы и ручку /iam/token', () => {",
            "  pm.expect(_svc, _wa).to.be.a('string').with.length.greaterThan(0);",
            "  pm.expect(_realm.replace(/^[a-z]+:\\/\\/[^/]+/i, ''), _wa).to.eql('/iam/token');",
            "});",
            "if (_svc) { pm.environment.set('regPubSvc', _svc); }",
        ],
    )


def _token(name, scopes, var, basic):
    """Токен полосы `/iam/token`: базовым удостоверением выдающего либо анонимно."""
    path = "/iam/token?service={{regPubSvc}}&" + "&".join("scope=" + s for s in scopes)
    pre = require_env_url(TOKEN_LANE, path, "полоса выдачи токена докер-клиенту")
    who = "базовое удостоверение выдающего" if basic else "аноним без Authorization"
    if basic:
        pre += ["pm.request.headers.upsert({key: 'Authorization', value: 'Basic ' + (pm.environment.get('regPubBasic') || '')});"]
    return Step(
        name=f"token-{name}", method="GET", path=path, auth="anonymous", insecure_tls=True, pre_script=pre,
        test_script=[
            *assert_answered("полоса выдачи токена"),
            *_json_or_null(),
            f"pm.test({_js('Дано П2: токен полосы выдан (' + who + ') — иначе условие сценария не создано')}, () => {{",
            "  pm.expect(pm.response.code, pm.response.text()).to.eql(200);",
            "  pm.expect(_j && (_j.token || _j.access_token), pm.response.text()).to.be.a('string').and.not.empty;",
            "});",
            f"pm.environment.unset('{var}');",
            f"if (_j && (_j.token || _j.access_token)) pm.environment.set('{var}', _j.token || _j.access_token);",
        ],
    )


def _anon_token(name, repos):
    """Анонимный токен П2: без Authorization, `service` — из вызова плоскости данных."""
    return _token(f"anon-{name}", [f"repository:{r}:pull" for r in repos] + ["registry:catalog:*"],
                  "regPubAnonTok", basic=False)


def _upload_blob(label, repo_path, kind):
    """Блоб двумя шагами протокола: открыть загрузку, затем PUT с дайджестом.

    Содержимое строится в пред-скрипте и дайджест считается от тех же байтов, что
    уходят телом (ASCII: длина строки = размер). Конфиг ссылается на дайджест слоя,
    поэтому слой заливается первым."""
    var = "_rpUploadLoc"
    if kind == "layer":
        content_js = "'regpub-layer ' + pm.environment.get('runId') + ' " + label + "'"
    else:
        content_js = ("JSON.stringify({architecture: 'amd64', os: 'linux', config: {}, "
                      "rootfs: {type: 'layers', diff_ids: ['sha256:' + pm.environment.get('_rpLayerDigest')]}})")
    return [
        Step(name=f"push-{label}-{kind}-open", method="POST", path=f"/v2/{repo_path}/blobs/uploads/",
             auth="anonymous", pre_script=_dp(f"/v2/{repo_path}/blobs/uploads/", "_rpPushTok"),
             test_script=[
                 *assert_answered("открытие загрузки блоба"),
                 f"pm.test({_js('Дано П2: загрузка ' + kind + ' ' + label + ' открыта (202)')}, () => "
                 "pm.expect(pm.response.code, pm.response.text()).to.eql(202));",
                 "const _loc = (pm.response.headers.get('Location') || '').replace(/^[a-z]+:\\/\\/[^/]+/i, '');",
                 f"pm.environment.unset('{var}');",
                 f"if (_loc) pm.environment.set('{var}', _loc);",
             ]),
        Step(name=f"push-{label}-{kind}-put", method="PUT", path=f"/v2/{repo_path}/blobs/uploads/",
             auth="anonymous", body={"replaced": "pre-request"},
             pre_script=[
                 *_dp(f"/v2/{repo_path}/blobs/uploads/", "_rpPushTok"),
                 f"const _content = {content_js};",
                 "const _hex = CryptoJS.SHA256(_content).toString(CryptoJS.enc.Hex);",
                 f"pm.environment.set('_rp{kind.capitalize()}Digest', _hex);",
                 f"pm.environment.set('_rp{kind.capitalize()}Size', String(_content.length));",
                 f"const _l = pm.environment.get('{var}') || '';",
                 f"const _b = pm.environment.get('{DP}') || '';",
                 "if (_b && _l) {",
                 "  pm.request.url = _b + _l + (_l.indexOf('?') >= 0 ? '&' : '?') + 'digest=' + encodeURIComponent('sha256:' + _hex);",
                 "}",
                 "pm.request.body.update({mode: 'raw', raw: _content});",
                 "pm.request.headers.upsert({key: 'Content-Type', value: 'application/octet-stream'});",
             ],
             test_script=[
                 *assert_answered("завершение загрузки блоба"),
                 f"pm.test({_js('Дано П2: блоб ' + kind + ' ' + label + ' залит (201)')}, () => "
                 "pm.expect(pm.response.code, pm.response.text()).to.eql(201));",
             ]),
    ]


def _push(label, reg_var, repo, tag, digest_var):
    """Залить образ `<реестр>/<repo>:<tag>` базовым удостоверением выдающего."""
    repo_path = f"{{{{{reg_var}}}}}/{repo}"
    manifest_js = (
        "JSON.stringify({schemaVersion: 2, mediaType: '" + _MANIFEST_MT + "', "
        "config: {mediaType: 'application/vnd.oci.image.config.v1+json', "
        "digest: 'sha256:' + pm.environment.get('_rpConfigDigest'), "
        "size: parseInt(pm.environment.get('_rpConfigSize'), 10)}, "
        "layers: [{mediaType: 'application/vnd.oci.image.layer.v1.tar', "
        "digest: 'sha256:' + pm.environment.get('_rpLayerDigest'), "
        "size: parseInt(pm.environment.get('_rpLayerSize'), 10)}]})"
    )
    return [
        _token(f"push-{label}", [f"repository:{repo_path}:pull,push"], "_rpPushTok", basic=True),
        *_upload_blob(label, repo_path, "layer"),
        *_upload_blob(label, repo_path, "config"),
        Step(name=f"push-{label}-manifest", method="PUT", path=f"/v2/{repo_path}/manifests/{tag}",
             auth="anonymous", body={"replaced": "pre-request"},
             pre_script=[
                 *_dp(f"/v2/{repo_path}/manifests/{tag}", "_rpPushTok"),
                 f"const _m = {manifest_js};",
                 f"pm.environment.set('{digest_var}', 'sha256:' + CryptoJS.SHA256(_m).toString(CryptoJS.enc.Hex));",
                 "pm.request.body.update({mode: 'raw', raw: _m});",
                 f"pm.request.headers.upsert({{key: 'Content-Type', value: '{_MANIFEST_MT}'}});",
             ],
             test_script=[
                 *assert_answered("заливка манифеста"),
                 f"pm.test({_js('Дано П2: манифест ' + label + ':' + tag + ' залит (201)')}, () => "
                 "pm.expect(pm.response.code, pm.response.text()).to.eql(201));",
                 "const _dcd = pm.response.headers.get('Docker-Content-Digest') || '';",
                 f"pm.test({_js('Дано П2: плоскость данных назвала тот же дайджест манифеста ' + label)}, () => "
                 f"pm.expect(_dcd, 'Docker-Content-Digest').to.eql(pm.environment.get('{digest_var}')));",
             ]),
    ]


def _anon_get(name, path, status, what, extra=()):
    """Чтение плоскости данных анонимным токеном; статус — утверждение сценария."""
    return Step(name=name, method="GET", path=path, auth="anonymous", pre_script=_dp(path, "regPubAnonTok"),
                test_script=[
                    *assert_answered("плоскость данных, анонимный токен"),
                    f"pm.test({_js(what)}, () => pm.expect(pm.response.code, pm.response.text()).to.eql({status}));",
                    *extra,
                ])


def _name_unknown(scenario):
    return [
        *_json_or_null(),
        f"pm.test({_js(scenario + ': errors[0].code = NAME_UNKNOWN')}, () => "
        "pm.expect(_j && _j.errors && _j.errors[0] && _j.errors[0].code, pm.response.text()).to.eql('NAME_UNKNOWN'));",
    ]


def _anon_poll(name, path, cond_js, what):
    return _poll_until(name, "GET", path, "anonymous", cond_js, what, pre=_dp(path, "regPubAnonTok"))


# ---------------------------------------------------------------------------
# Дано П2: реестры, репозитории, роли, удостоверение выдающего.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-SETUP",
    title="Дано П2: реестры X и Y, образы X/app, Y/app, Y/pub (поштучно PUBLIC), роли rol-reg-get и rol-reg-edit",
    classes=["CRUD"], priority="P0",
    steps=[
        *_create_registry("x", "regPubX"),
        *_create_registry("y", "regPubY"),
        # Базовое удостоверение выдающего: строка секрета показывается один раз — здесь.
        Step(name="issue-issuer-secret", method="POST", path="/iam/v1/serviceAccounts/{{svaAccountAdminAId}}/keys",
             auth=ISSUER,
             body={"serviceAccountId": "{{svaAccountAdminAId}}", "description": "REG-PUB-1 push {{runId}}",
                   "credentialKind": "CREDENTIAL_KIND_SECRET"},
             test_script=[
                 *_json_or_null(),
                 "const _r = (_j && _j.response) || {};",
                 "const _kid = _r.clientId || _r.keyId || (_r.key && _r.key.id) || '';",
                 "pm.test('Дано П2: удостоверение выдающего выпущено (done, секрет и id в ответе) — иначе условие не создано', () => {",
                 "  pm.expect(pm.response.code, pm.response.text()).to.eql(200);",
                 "  pm.expect(_j && _j.done, 'done').to.eql(true);",
                 "  pm.expect(_r.secret, 'secret').to.be.a('string').and.not.empty;",
                 "  pm.expect(_kid, 'key id').to.be.a('string').and.not.empty;",
                 "});",
                 "pm.environment.unset('regPubKeyId');",
                 "if (_kid) pm.environment.set('regPubKeyId', _kid);",
                 "if (_kid && _r.secret) pm.environment.set('regPubBasic', "
                 "CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse(_kid + ':' + _r.secret)));",
             ]),
        _challenge(),
        *_push("x-app", "regPubX", "app", "latest", "regPubXAppDigest"),
        *_push("y-app", "regPubY", "app", "latest", "regPubYAppDigest"),
        Step(name="create-repository-y-pub", method="POST", path=f"{REG}/{_Y}/repositories", auth=ISSUER,
             body={"repository": "pub", "visibility": "PUBLIC"},
             test_script=[*assert_status(200), *assert_operation_envelope(_OP_PREFIX), *save_operation_id()]),
        poll_operation_until_done(auth=ISSUER),
        Step(name="repository-y-pub-created", method="GET", path=f"{REG}/{_Y}/repositories/pub", auth=ISSUER,
             test_script=[*assert_status(200), *_json_or_null(),
                          "pm.test('Дано П2: Y/pub опубликован поштучно (visibility PUBLIC)', () => "
                          "pm.expect(_j && _j.visibility, pm.response.text()).to.eql('PUBLIC'));"]),
        *_push("y-pub", "regPubY", "pub", "seed", "regPubYPubDigest"),
        *_create_role("rol-reg-get",
                      [{"module": "registry", "resources": ["registries"], "verbs": ["get", "list"]}],
                      "rolRegGet", "Дано П2: роль rol-reg-get создана (200 и Operation)"),
        *_create_role("rol-reg-edit",
                      [{"module": "registry", "resources": ["registries"], "verbs": ["get", "list", "update"]}],
                      "rolRegEdit", "Дано П2: роль rol-reg-edit создана (200 и Operation)"),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-03 — роль даёт только чтение (тело роли через край).
# ---------------------------------------------------------------------------
_ROLE_SHAPE = [
    "const _r0 = (_j && _j.rules && _j.rules[0]) || {};",
    "const _c0 = (_r0.children && _r0.children[0]) || null;",
]

CASES.append(Case(
    id="REG-PUB-1-03",
    title="GET роли registry.public.read → rules[0] = registry/registries/[get,list], children[0] = repositories/[get,list], "
          "прочие ключи children[0] пусты; близнецы — роль без вложенного правила и выдача пишущей роли",
    classes=["CONF", "AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="get-public-read-role", method="GET", path=f"{ROLES}/{PUBLIC_READ_ROLE}", auth=ISSUER,
             test_script=[*assert_status(200), *_json_or_null(), *_ROLE_SHAPE,
                          "pm.test('REG-PUB-1-03: rules — длины 1', () => pm.expect((_j && _j.rules || []).length, pm.response.text()).to.eql(1));",
                          "pm.test('REG-PUB-1-03: rules[0].module = registry', () => pm.expect(_r0.module).to.eql('registry'));",
                          "pm.test('REG-PUB-1-03: rules[0].resources = [registries]', () => pm.expect(_r0.resources).to.eql(['registries']));",
                          "pm.test('REG-PUB-1-03: rules[0].verbs = [get, list]', () => pm.expect(_r0.verbs).to.eql(['get', 'list']));",
                          "pm.test('REG-PUB-1-03: rules[0].children — длины 1', () => pm.expect((_r0.children || []).length, pm.response.text()).to.eql(1));",
                          "pm.test('REG-PUB-1-03: children[0].resources = [repositories]', () => pm.expect(_c0 && _c0.resources).to.eql(['repositories']));",
                          "pm.test('REG-PUB-1-03: children[0].verbs = [get, list]', () => pm.expect(_c0 && _c0.verbs).to.eql(['get', 'list']));",
                          "pm.test('REG-PUB-1-03: children[0].module пуст', () => pm.expect(_c0 && _c0.module, JSON.stringify(_c0)).to.eql(''));",
                          "pm.test('REG-PUB-1-03: children[0].children пуст', () => pm.expect(_c0 && _c0.children, JSON.stringify(_c0)).to.eql([]));",
                          "pm.test('REG-PUB-1-03: children[0].resourceNames пуст', () => pm.expect(_c0 && _c0.resourceNames, JSON.stringify(_c0)).to.eql([]));",
                          "pm.test('REG-PUB-1-03: children[0].matchLabels пуст', () => pm.expect(_c0 && _c0.matchLabels, JSON.stringify(_c0)).to.eql({}));"]),
        # Близнец формы: та же проба различает роль без вложенного правила.
        Step(name="get-rol-reg-get-twin", method="GET", path=f"{ROLES}/{{{{rolRegGet}}}}", auth=ISSUER,
             test_script=[*assert_status(200), *_json_or_null(), *_ROLE_SHAPE,
                          "pm.test('REG-PUB-1-03 близнец: у rol-reg-get rules[0].children пуст — проба различает вложенное правило', () => "
                          "pm.expect((_r0.children || []).length, pm.response.text()).to.eql(0));"]),
        # Близнец выдачи: та же выдача ролью с пишущим глаголом.
        Step(name="grant-rol-reg-edit-refused", method="POST", path=GRANTS, auth=ISSUER,
             body=_grant_body("{{rolRegEdit}}"),
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          *assert_refusal_message(
                              "Role {{rolRegEdit}} grants verbs beyond get and list; a system class subject accepts read-only roles",
                              "REG-PUB-1-03 близнец: текст SC-21 kaname дословно"),
                          *_json_or_null(),
                          "pm.test('REG-PUB-1-03 близнец: Operation не создана', () => "
                          "pm.expect(_j && _j.done, pm.response.text()).to.eql(undefined));"]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-10 — до выдачи посторонний реестра не видит.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-10",
    title="Выдач классам на X нет → посторонний GET X → 404/5 (закрытый неотличим от несуществующего)",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="stranger-get-x-closed", method="GET", path=f"{REG}/{_X}", auth=STRANGER,
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-18 — редактор проекта реестр не открывает.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-18",
    title="Редактор шлёт выдачу ПЧ → синхронно 403/7 с текстом отказа выдачи человеку без права; Operation нет; "
          "посторонний GET X → 404",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        # База сравнения: тот же редактор выдаёт ту же роль ЧЕЛОВЕКУ — отказ в праве выдать.
        Step(name="editor-grants-human-baseline", method="POST", path=GRANTS, auth=EDITOR,
             body={"subjects": [{"type": "SUBJECT_TYPE_USER", "id": "{{plainHumanUserId}}"}],
                   "roleId": PUBLIC_READ_ROLE, "scopeType": "iam.project", "scopeId": "{{existingProjectId}}",
                   "target": {"resources": {"resources": [{"type": "registry.registries", "id": _X}]}}},
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED"), *_json_or_null(),
                          "pm.environment.set('_regPubHumanDenyMsg', (_j && _j.message) || '');"]),
        Step(name="editor-grants-anonymous", method="POST", path=GRANTS, auth=EDITOR,
             body=_grant_body(PUBLIC_READ_ROLE),
             test_script=[*assert_status(403), *assert_grpc_code(7, "PERMISSION_DENIED"), *_json_or_null(),
                          "pm.test('REG-PUB-1-18: текст отказа тот же, что у выдачи человеку без права (SC-20)', () => "
                          "pm.expect(_j && _j.message, pm.response.text()).to.eql(pm.environment.get('_regPubHumanDenyMsg')));",
                          "pm.test('REG-PUB-1-18: Operation не создана', () => "
                          "pm.expect(_j && _j.done, pm.response.text()).to.eql(undefined));"]),
        Step(name="stranger-get-x-after-editor", method="GET", path=f"{REG}/{_X}", auth=STRANGER,
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-21 — закрытый реестр аноним не отличает от несуществующего.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-21",
    title="Без выдачи ПЧ: аноним GET /v2/X/app/manifests/latest и /v2/X/nonexistent → оба 404 NAME_UNKNOWN, тела побайтово равны",
    classes=["AUTHZ", "NEG", "SEC"], priority="P0",
    steps=[
        _anon_token("closed", [f"{_X}/app"]),
        _anon_get("anon-x-app-closed", f"/v2/{_X}/app/manifests/latest", 404,
                  "REG-PUB-1-21: закрытый X/app → 404",
                  [*_name_unknown("REG-PUB-1-21"), "pm.environment.set('_regPubClosedBody', pm.response.text());"]),
        _anon_get("anon-x-nonexistent", f"/v2/{_X}/nonexistent-{{{{runId}}}}/manifests/latest", 404,
                  "REG-PUB-1-21: несуществующий репозиторий → 404",
                  [*_name_unknown("REG-PUB-1-21"),
                   "pm.test('REG-PUB-1-21: тело побайтово равно телу закрытого X/app', () => "
                   "pm.expect(pm.response.text()).to.eql(pm.environment.get('_regPubClosedBody')));"]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-25 — без токена вызов на вход, негодный токен анонимом не становится.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-25",
    title="GET /v2/X/app/manifests/latest без Authorization → 401 Bearer realm/service без error=, UNAUTHORIZED; "
          "испорченный токен → 401 error=\"invalid_token\"",
    classes=["AUTHN", "NEG", "SEC"], priority="P0",
    steps=[
        Step(name="no-token-challenge", method="GET", path=f"/v2/{_X}/app/manifests/latest", auth="anonymous",
             pre_script=_dp(f"/v2/{_X}/app/manifests/latest"),
             test_script=[*assert_answered("плоскость данных без токена"), *assert_status(401), *_json_or_null(),
                          "const _wa = pm.response.headers.get('WWW-Authenticate') || '';",
                          "pm.test('REG-PUB-1-25: вызов Bearer с realm и service, без error=', () => {",
                          "  pm.expect(_wa, _wa).to.match(/^Bearer realm=\"[^\"]+\",service=\"[^\"]+\"/);",
                          "  pm.expect(_wa, _wa).to.not.contain('error=');",
                          "});",
                          "pm.test('REG-PUB-1-25: errors[0].code = UNAUTHORIZED', () => "
                          "pm.expect(_j && _j.errors && _j.errors[0] && _j.errors[0].code, pm.response.text()).to.eql('UNAUTHORIZED'));"]),
        Step(name="garbage-token-refused", method="GET", path=f"/v2/{_X}/app/manifests/latest", auth="anonymous",
             pre_script=[*_dp(f"/v2/{_X}/app/manifests/latest"),
                         "pm.request.headers.upsert({key: 'Authorization', value: 'Bearer regpub-not-a-token'});"],
             test_script=[*assert_answered("плоскость данных, испорченный токен"), *assert_status(401),
                          "const _wa = pm.response.headers.get('WWW-Authenticate') || '';",
                          "pm.test('REG-PUB-1-25: негодное удостоверение — error=\"invalid_token\", анонимом не становится (SC-33)', () => "
                          "pm.expect(_wa, _wa).to.contain('error=\"invalid_token\"'));"]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-27 — поштучная публикация в закрытом реестре работает как прежде.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-27",
    title="Без выдачи ПЧ: аноним GET /v2/Y/pub/manifests/seed → 200; /v2/Y/pub/tags/list → 404 NAME_UNKNOWN; "
          "близнец — неопубликованный Y/app → 404",
    classes=["AUTHZ", "STATE"], priority="P0",
    steps=[
        _anon_token("ypub", [f"{_Y}/pub", f"{_Y}/app"]),
        _anon_get("anon-y-pub-manifest", f"/v2/{_Y}/pub/manifests/seed", 200,
                  "REG-PUB-1-27: поштучно опубликованный манифест → 200"),
        _anon_get("anon-y-pub-tags", f"/v2/{_Y}/pub/tags/list", 404,
                  "REG-PUB-1-27: перечень тегов публикация не открывает (только v_get) → 404",
                  _name_unknown("REG-PUB-1-27")),
        _anon_get("anon-y-app-unpublished", f"/v2/{_Y}/app/manifests/latest", 404,
                  "REG-PUB-1-27 близнец: неопубликованный Y/app → 404", _name_unknown("REG-PUB-1-27 близнец")),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-13 — роль без вложенного правила открывает реестр, но не репозитории.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-13",
    title="Выдача классу anonymous роли rol-reg-get с целью X → посторонний GET X 200; аноним /v2/X/app → 404 NAME_UNKNOWN",
    classes=["AUTHZ", "STATE"], priority="P1",
    steps=[
        *_grant("rol-reg-get-anonymous-x", _grant_body("{{rolRegGet}}"), "regPubGrantGet"),
        _stranger_reads_x("rol-reg-get", "REG-PUB-1-13: класс получил v_get на реестр — посторонний читает X (200)"),
        _anon_token("reg-get", [f"{_X}/app"]),
        _anon_get("anon-x-app-reg-get", f"/v2/{_X}/app/manifests/latest", 404,
                  "REG-PUB-1-13: без вложенного правила репозиторий классу закрыт → 404",
                  _name_unknown("REG-PUB-1-13")),
        *_revoke("rol-reg-get-anonymous-x", "regPubGrantGet"),
        _poll_until("stranger-x-closed-after-reg-get", "GET", f"{REG}/{_X}", STRANGER,
                    "pm.response.code === 404", "REG-PUB-1-13 уборка: после отзыва X снова закрыт постороннему"),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-14 — выдача классу «вошедшие» не открывает анониму.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-14",
    title="Выдача ПЧ классу authenticated → посторонний X/repositories 200 с app; аноним /v2/X/app → 404 NAME_UNKNOWN",
    classes=["AUTHZ", "STATE"], priority="P1",
    steps=[
        *_grant("authenticated-x", _grant_body(PUBLIC_READ_ROLE, subject_class="authenticated"), "regPubGrantAuth"),
        _poll_until("stranger-sees-app-authenticated", "GET", f"{REG}/{_X}/repositories", STRANGER,
                    "pm.response.code === 200 && _j && (_j.repositories || []).some(r => r.name === 'app')",
                    "REG-PUB-1-14: выдача вошедшим действует — посторонний видит X/app (SC-69)"),
        _anon_token("authenticated", [f"{_X}/app"]),
        _anon_get("anon-x-app-authenticated", f"/v2/{_X}/app/manifests/latest", 404,
                  "REG-PUB-1-14: класс authenticated анониму не открывает → 404", _name_unknown("REG-PUB-1-14")),
        *_revoke("authenticated-x", "regPubGrantAuth"),
        _poll_until("stranger-x-closed-after-authenticated", "GET", f"{REG}/{_X}", STRANGER,
                    "pm.response.code === 404", "REG-PUB-1-14 уборка: после отзыва X снова закрыт постороннему"),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-02 — роль доставлена: выдача ПЧ принимается.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-02",
    title="Выдача ПЧ от выдающего → Operation done без error; GET выдачи → roleId и субъект system_class:anonymous; "
          "близнец — несуществующая роль → 400/9 'Role rolb0000000000000000 not found'",
    classes=["AUTHZ", "CONF", "NEG"], priority="P0",
    steps=[
        Step(name="grant-unknown-role-refused", method="POST", path=GRANTS, auth=ISSUER,
             body=_grant_body("rolb0000000000000000"),
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          *assert_refusal_message("Role rolb0000000000000000 not found",
                                                  "REG-PUB-1-02 близнец: роли нет — текст дословно"),
                          *_json_or_null(),
                          "pm.test('REG-PUB-1-02 близнец: Operation не создана', () => "
                          "pm.expect(_j && _j.done, pm.response.text()).to.eql(undefined));"]),
        *_grant("pub-x", _grant_body(PUBLIC_READ_ROLE), "regPubGrantPub"),
        Step(name="get-grant-pub-x", method="GET", path=f"{GRANTS}/{{{{regPubGrantPub}}}}", auth=ISSUER,
             test_script=[*assert_status(200), *_json_or_null(),
                          f"pm.test('REG-PUB-1-02: roleId = {PUBLIC_READ_ROLE}', () => "
                          f"pm.expect(_j && _j.roleId, pm.response.text()).to.eql('{PUBLIC_READ_ROLE}'));",
                          "pm.test('REG-PUB-1-02: субъект — класс anonymous', () => "
                          "pm.expect((_j && _j.subjects) || [], pm.response.text())"
                          ".to.deep.include({type: 'SUBJECT_TYPE_SYSTEM_CLASS', id: 'anonymous'}));"]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-11 — выдача ПЧ открывает реестр и его репозитории постороннему.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-11",
    title="Выдача ПЧ материализована → посторонний: GET X 200 без ключа visibility; X/repositories 200 с app; "
          "List проекта — ровно X (Р10)",
    classes=["AUTHZ", "STATE", "CONF"], priority="P0",
    steps=[
        _stranger_reads_x("pub-x", "REG-PUB-1-11: выдача ПЧ материализована — посторонний читает X (200)"),
        Step(name="stranger-get-x-open", method="GET", path=f"{REG}/{_X}", auth=STRANGER,
             test_script=[*assert_status(200), *_json_or_null(),
                          "pm.test('REG-PUB-1-11: id = X', () => pm.expect(_j && _j.id).to.eql(pm.environment.get('regPubX')));",
                          "pm.test('REG-PUB-1-11: ключа visibility в ответе нет (поля нет, Р2)', () => "
                          "pm.expect(_j || {}, pm.response.text()).to.not.have.property('visibility'));"]),
        _poll_until("stranger-list-x-repositories", "GET", f"{REG}/{_X}/repositories", STRANGER,
                    "pm.response.code === 200 && _j && (_j.repositories || []).some(r => r.name === 'app')",
                    "REG-PUB-1-11: X/repositories → 200, в странице app"),
        Step(name="stranger-list-project", method="GET", path=f"{REG}?projectId={{{{existingProjectId}}}}&pageSize=1000",
             auth=STRANGER,
             test_script=[*assert_status(200), *_json_or_null(),
                          "pm.test('REG-PUB-1-11: в странице ровно X, Y нет (Р10)', () => "
                          "pm.expect(((_j && _j.registries) || []).map(r => r.id), pm.response.text())"
                          ".to.eql([pm.environment.get('regPubX')]));"]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-30 — аноним на API-крае отвергается до решения.
# ---------------------------------------------------------------------------
def _anon_edge_vs_closed(path_x, path_y, label):
    return [
        Step(name=f"anon-edge-baseline-y-{label}", method="GET", path=path_y, auth="anonymous",
             test_script=[*assert_status(401), *assert_grpc_code(16, "UNAUTHENTICATED"),
                          f"pm.environment.set('_regPubAnonY_{label}', pm.response.text().split(pm.environment.get('regPubY')).join('<ID>'));"]),
        Step(name=f"anon-edge-x-{label}", method="GET", path=path_x, auth="anonymous",
             test_script=[*assert_status(401), *assert_grpc_code(16, "UNAUTHENTICATED"),
                          f"pm.test({_js('REG-PUB-1-30 ' + label + ': тело побайтово равно ответу на закрытый Y')}, () => "
                          "pm.expect(pm.response.text().split(pm.environment.get('regPubX')).join('<ID>'))"
                          f".to.eql(pm.environment.get('_regPubAnonY_{label}')));"]),
    ]


CASES.append(Case(
    id="REG-PUB-1-30",
    title="Аноним без Authorization: GET X и X/repositories открытого X → 401/16, тело равно ответу на закрытый Y; "
          "близнец — посторонний → 200",
    classes=["AUTHN", "NEG"], priority="P0",
    steps=[
        *_anon_edge_vs_closed(f"{REG}/{_X}", f"{REG}/{_Y}", "get"),
        *_anon_edge_vs_closed(f"{REG}/{_X}/repositories", f"{REG}/{_Y}/repositories", "repositories"),
        Step(name="stranger-twin-get-x", method="GET", path=f"{REG}/{_X}", auth=STRANGER,
             test_script=[*assert_status(200)]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-31 — выдача ПЧ не даёт правки и не открывает соседний реестр.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-31",
    title="Посторонний: PATCH X → 404/5, description X прежний; GET Y → 404/5; близнец — PATCH от выдающего применён",
    classes=["AUTHZ", "NEG"], priority="P0",
    steps=[
        Step(name="issuer-reads-x-description", method="GET", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=[*assert_status(200), *_json_or_null(),
                          "pm.environment.set('_regPubXDescr', (_j && _j.description) || '');"]),
        Step(name="stranger-patch-x", method="PATCH", path=f"{REG}/{_X}", auth=STRANGER,
             body={"description": "d-{{runId}}", "updateMask": "description"},
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        Step(name="issuer-x-description-unchanged", method="GET", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=[*assert_status(200), *_json_or_null(),
                          "pm.test('REG-PUB-1-31: description X не изменился', () => "
                          "pm.expect((_j && _j.description) || '', pm.response.text()).to.eql(pm.environment.get('_regPubXDescr')));"]),
        Step(name="stranger-get-y-closed", method="GET", path=f"{REG}/{_Y}", auth=STRANGER,
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        Step(name="issuer-patch-x-twin", method="PATCH", path=f"{REG}/{_X}", auth=ISSUER,
             body={"description": "d-{{runId}}", "updateMask": "description"},
             test_script=[*assert_status(200), *assert_operation_envelope(_OP_PREFIX), *save_operation_id()]),
        poll_operation_until_done(auth=ISSUER),
        Step(name="issuer-x-description-applied", method="GET", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=["pm.test('REG-PUB-1-31 близнец: правка выдающего — done без error', () => "
                          "pm.expect(pm.environment.get('lastOpError') || '').to.eql(''));",
                          *assert_status(200), *_json_or_null(),
                          "pm.test('REG-PUB-1-31 близнец: description применён', () => "
                          "pm.expect(_j && _j.description, pm.response.text()).to.eql('d-' + pm.environment.get('runId')));"]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-20 — аноним тянет образ открытого реестра, включая репозиторий после выдачи.
# ---------------------------------------------------------------------------
def _anon_pulls(label, repo, digest_var):
    """Манифест и блоб слоя анонимом; дайджест тела манифеста равен дайджесту заливки."""
    return [
        _anon_poll(f"anon-{label}-manifest", f"/v2/{_X}/{repo}/manifests/latest",
                   "pm.response.code === 200",
                   f"REG-PUB-1-20: аноним тянет манифест X/{repo} (200 в окне П2)"),
        _anon_get(f"anon-{label}-manifest-digest", f"/v2/{_X}/{repo}/manifests/latest", 200,
                  f"REG-PUB-1-20: манифест X/{repo} → 200",
                  ["const _body = pm.response.text();",
                   f"pm.test({_js('REG-PUB-1-20: дайджест тела манифеста X/' + repo + ' равен дайджесту заливки')}, () => "
                   f"pm.expect('sha256:' + CryptoJS.SHA256(_body).toString(CryptoJS.enc.Hex)).to.eql(pm.environment.get('{digest_var}')));",
                   "let _mf = null; try { _mf = JSON.parse(_body); } catch (e) { _mf = null; }",
                   "const _ld = (_mf && _mf.layers && _mf.layers[0] && _mf.layers[0].digest) || '';",
                   "pm.environment.unset('_regPubLayerOf');",
                   "if (_ld) pm.environment.set('_regPubLayerOf', _ld);"]),
        _anon_get(f"anon-{label}-blob", f"/v2/{_X}/{repo}/blobs/{{{{_regPubLayerOf}}}}", 200,
                  f"REG-PUB-1-20: блоб слоя X/{repo} → 200"),
    ]


CASES.append(Case(
    id="REG-PUB-1-20",
    title="Выдача ПЧ: аноним тянет манифест и блоб X/app и X/late (залит ПОСЛЕ выдачи) — 200, дайджест равен заливке",
    classes=["AUTHZ", "STATE"], priority="P0",
    steps=[
        *_push("x-late", "regPubX", "late", "latest", "regPubXLateDigest"),
        _anon_token("open", [f"{_X}/app", f"{_X}/late"]),
        *_anon_pulls("x-app", "app", "regPubXAppDigest"),
        *_anon_pulls("x-late", "late", "regPubXLateDigest"),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-22 — аноним перечисляет теги открытого репозитория, а не чужого.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-22",
    title="Аноним GET /v2/X/app/tags/list → 200 с latest; близнец — Y/app → 404 NAME_UNKNOWN",
    classes=["AUTHZ", "NEG"], priority="P1",
    steps=[
        _anon_token("tags", [f"{_X}/app", f"{_Y}/app"]),
        _anon_get("anon-x-app-tags", f"/v2/{_X}/app/tags/list", 200, "REG-PUB-1-22: теги открытого X/app → 200",
                  [*_json_or_null(),
                   "pm.test('REG-PUB-1-22: tags содержит latest', () => "
                   "pm.expect((_j && _j.tags) || [], pm.response.text()).to.include('latest'));"]),
        _anon_get("anon-y-app-tags", f"/v2/{_Y}/app/tags/list", 404, "REG-PUB-1-22 близнец: Y вне цели выдачи → 404",
                  _name_unknown("REG-PUB-1-22 близнец")),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-23 — каталог анонима: репозитории открытых реестров и только они.
# ---------------------------------------------------------------------------
_CATALOG_WALK = [
    *_json_or_null(),
    "pm.test('REG-PUB-1-23: страница каталога → 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
    "const _acc = JSON.parse(pm.environment.get('_regPubCatalogAcc') || '[]').concat((_j && _j.repositories) || []);",
    "pm.environment.set('_regPubCatalogAcc', JSON.stringify(_acc));",
    "const _link = pm.response.headers.get('Link') || '';",
    "const _next = (_link.match(/<([^>]+)>;\\s*rel=\"next\"/) || [])[1] || '';",
    "const _pages = parseInt(pm.environment.get('_regPubCatalogPages') || '0', 10);",
    # Это ОБХОД СТРАНИЦ, а не ожидание: следующая страница уже есть у сервера,
    # пауза перед ней ничего бы не дождалась. Предел — 200 страниц по 1000 имён.
    "if (_next && _pages < 200) {",
    "  pm.environment.set('_regPubCatalogPages', String(_pages + 1));",
    "  pm.environment.set('_regPubCatalogNext', _next.replace(/^[a-z]+:\\/\\/[^/]+/i, ''));",
    "  pm.execution.setNextRequest(pm.info.requestName);",
    "  return;",
    "}",
    "pm.environment.unset('_regPubCatalogNext');",
    "pm.environment.unset('_regPubCatalogPages');",
]


def _catalog_step(name, token_var):
    return Step(name=name, method="GET", path="/v2/_catalog?n=1000", auth="anonymous",
                pre_script=[*_dp("/v2/_catalog?n=1000", token_var),
                            "const _nx = pm.environment.get('_regPubCatalogNext') || '';",
                            f"const _bs = pm.environment.get('{DP}') || '';",
                            "if (_nx && _bs) { pm.request.url = _bs + _nx; }"],
                test_script=list(_CATALOG_WALK))


def _repo_names(*pairs):
    return "[" + ", ".join(f"pm.environment.get('{v}') + '/{r}'" for v, r in pairs) + "]"


CASES.append(Case(
    id="REG-PUB-1-23",
    title="Аноним обходит /v2/_catalog по Link до конца → есть X/app и X/late, нет Y/app и Y/pub; "
          "близнец — обход выдающим содержит Y/app",
    classes=["AUTHZ", "PAGE"], priority="P1",
    steps=[
        _anon_token("catalog", [f"{_X}/app"]),
        Step(name="catalog-reset-anon", method="GET", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=[*assert_status(200), "pm.environment.set('_regPubCatalogAcc', '[]');"]),
        _catalog_step("anon-catalog-walk", "regPubAnonTok"),
        Step(name="anon-catalog-judged", method="GET", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=[*assert_status(200),
                          "const _seen = JSON.parse(pm.environment.get('_regPubCatalogAcc') || '[]');",
                          f"const _must = {_repo_names(('regPubX', 'app'), ('regPubX', 'late'))};",
                          f"const _never = {_repo_names(('regPubY', 'app'), ('regPubY', 'pub'))};",
                          "pm.test('REG-PUB-1-23: каталог анонима содержит X/app и X/late', () => "
                          "_must.forEach(n => pm.expect(_seen, n).to.include(n)));",
                          "pm.test('REG-PUB-1-23: каталог анонима не содержит Y/app и Y/pub (публикация даёт только v_get)', () => "
                          "_never.forEach(n => pm.expect(_seen, n).to.not.include(n)));"]),
        _token("catalog-issuer", ["registry:catalog:*"], "_rpIssuerCatalogTok", basic=True),
        Step(name="catalog-reset-issuer", method="GET", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=[*assert_status(200), "pm.environment.set('_regPubCatalogAcc', '[]');"]),
        _catalog_step("issuer-catalog-walk", "_rpIssuerCatalogTok"),
        Step(name="issuer-catalog-judged", method="GET", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=[*assert_status(200),
                          "const _seen = JSON.parse(pm.environment.get('_regPubCatalogAcc') || '[]');",
                          f"const _y = {_repo_names(('regPubY', 'app'))}[0];",
                          "pm.test('REG-PUB-1-23 близнец: каталог выдающего содержит Y/app', () => "
                          "pm.expect(_seen, _y).to.include(_y));"]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-24 — аноним не пишет ни в существующий, ни в новый репозиторий.
# ---------------------------------------------------------------------------
_DENIED = [
    *_json_or_null(),
    "pm.test('REG-PUB-1-24: errors[0].code = DENIED', () => "
    "pm.expect(_j && _j.errors && _j.errors[0] && _j.errors[0].code, pm.response.text()).to.eql('DENIED'));",
    "pm.test('REG-PUB-1-24: текст отказа записи', () => "
    "pm.expect(_j && _j.errors && _j.errors[0] && _j.errors[0].message, pm.response.text())"
    ".to.eql('requested access to the resource is denied'));",
]


def _anon_write(name, method, path, what, pre_extra=(), body=None):
    return Step(name=name, method=method, path=path, auth="anonymous", body=body,
                pre_script=[*_dp(path, "regPubAnonTok"), *pre_extra],
                test_script=[*assert_answered("плоскость данных, запись анонимом"),
                             f"pm.test({_js(what)}, () => pm.expect(pm.response.code, pm.response.text()).to.eql(403));",
                             *_DENIED])


CASES.append(Case(
    id="REG-PUB-1-24",
    title="Аноним: POST загрузки в X/app и X/new, PUT манифеста X/app:anon → 403 DENIED; тега и репозитория не появилось; "
          "DELETE манифеста → 405; близнец — POST загрузки выдающим → 202",
    classes=["AUTHZ", "NEG", "SEC"], priority="P0",
    steps=[
        _anon_token("write", [f"{_X}/app"]),
        _anon_write("anon-upload-x-app", "POST", f"/v2/{_X}/app/blobs/uploads/",
                    "REG-PUB-1-24: загрузка анонимом в X/app → 403"),
        _anon_write("anon-upload-x-new", "POST", f"/v2/{_X}/new-{{{{runId}}}}/blobs/uploads/",
                    "REG-PUB-1-24: загрузка анонимом в новый репозиторий → 403"),
        Step(name="anon-reads-x-app-manifest-body", method="GET", path=f"/v2/{_X}/app/manifests/latest",
             auth="anonymous", pre_script=_dp(f"/v2/{_X}/app/manifests/latest", "regPubAnonTok"),
             test_script=[*assert_status(200), "pm.environment.set('_regPubXAppManifest', pm.response.text());"]),
        _anon_write("anon-put-manifest", "PUT", f"/v2/{_X}/app/manifests/anon-{{{{runId}}}}",
                    "REG-PUB-1-24: заливка манифеста анонимом → 403", body={"replaced": "pre-request"},
                    pre_extra=["pm.request.body.update({mode: 'raw', raw: pm.environment.get('_regPubXAppManifest') || ''});",
                               f"pm.request.headers.upsert({{key: 'Content-Type', value: '{_MANIFEST_MT}'}});"]),
        Step(name="anon-delete-manifest", method="DELETE", path=f"/v2/{_X}/app/manifests/latest", auth="anonymous",
             pre_script=_dp(f"/v2/{_X}/app/manifests/latest", "regPubAnonTok"),
             test_script=[*assert_answered("плоскость данных, удаление анонимом"),
                          "pm.test('REG-PUB-1-24: удаление на плоскости данных → 405', () => "
                          "pm.expect(pm.response.code, pm.response.text()).to.eql(405));"]),
        _token("write-issuer", [f"repository:{_X}/app:pull,push"], "_rpPushTok", basic=True),
        Step(name="issuer-x-app-tags", method="GET", path=f"/v2/{_X}/app/tags/list", auth="anonymous",
             pre_script=_dp(f"/v2/{_X}/app/tags/list", "_rpPushTok"),
             test_script=[*assert_status(200), *_json_or_null(),
                          "pm.test('REG-PUB-1-24: тега anon-<runId> у X/app нет', () => "
                          "pm.expect((_j && _j.tags) || [], pm.response.text()).to.not.include('anon-' + pm.environment.get('runId')));"]),
        Step(name="issuer-upload-twin", method="POST", path=f"/v2/{_X}/app/blobs/uploads/", auth="anonymous",
             pre_script=_dp(f"/v2/{_X}/app/blobs/uploads/", "_rpPushTok"),
             test_script=[*assert_answered("плоскость данных, загрузка выдающим"),
                          "pm.test('REG-PUB-1-24 близнец: загрузка выдающим открыта (202)', () => "
                          "pm.expect(pm.response.code, pm.response.text()).to.eql(202));"]),
        Step(name="catalog-reset-write", method="GET", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=[*assert_status(200), "pm.environment.set('_regPubCatalogAcc', '[]');"]),
        _token("write-issuer-catalog", ["registry:catalog:*"], "_rpIssuerCatalogTok", basic=True),
        _catalog_step("issuer-catalog-walk-write", "_rpIssuerCatalogTok"),
        Step(name="write-catalog-judged", method="GET", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=[*assert_status(200),
                          "const _seen = JSON.parse(pm.environment.get('_regPubCatalogAcc') || '[]');",
                          "const _new = pm.environment.get('regPubX') + '/new-' + pm.environment.get('runId');",
                          "pm.test('REG-PUB-1-24: каталог не содержит X/new-<runId>', () => "
                          "pm.expect(_seen, _new).to.not.include(_new));"]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-12 и REG-PUB-1-26 — отзыв закрывает реестр и снимает чтение со всех репозиториев.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="REG-PUB-1-12",
    title="Отзыв выдачи ПЧ → done; опрос GET X постороннего доходит до 404/5 в окне П2",
    classes=["STATE", "AUTHZ"], priority="P0",
    steps=[
        Step(name="stranger-x-open-before-revoke", method="GET", path=f"{REG}/{_X}", auth=STRANGER,
             test_script=[*assert_status(200)]),
        *_revoke("pub-x", "regPubGrantPub"),
        _poll_until("stranger-x-closed-after-revoke", "GET", f"{REG}/{_X}", STRANGER,
                    "pm.response.code === 404 && _j && _j.code === 5",
                    "REG-PUB-1-12: после отзыва посторонний получает 404/5 в окне П2"),
    ],
))

CASES.append(Case(
    id="REG-PUB-1-26",
    title="После отзыва (REG-PUB-1-12) аноним: X/app и X/late доходят до 404 NAME_UNKNOWN; выдающий X/app → 200",
    classes=["STATE", "AUTHZ"], priority="P0",
    steps=[
        _anon_token("revoked", [f"{_X}/app", f"{_X}/late"]),
        _anon_poll("anon-x-app-revoked", f"/v2/{_X}/app/manifests/latest",
                   "pm.response.code === 404 && _j && _j.errors && _j.errors[0] && _j.errors[0].code === 'NAME_UNKNOWN'",
                   "REG-PUB-1-26: X/app анониму закрыт (404 NAME_UNKNOWN) в окне П2"),
        _anon_poll("anon-x-late-revoked", f"/v2/{_X}/late/manifests/latest",
                   "pm.response.code === 404 && _j && _j.errors && _j.errors[0] && _j.errors[0].code === 'NAME_UNKNOWN'",
                   "REG-PUB-1-26: X/late анониму закрыт (404 NAME_UNKNOWN) в окне П2"),
        _token("revoked-issuer", [f"repository:{_X}/app:pull"], "_rpPushTok", basic=True),
        Step(name="issuer-x-app-after-revoke", method="GET", path=f"/v2/{_X}/app/manifests/latest", auth="anonymous",
             pre_script=_dp(f"/v2/{_X}/app/manifests/latest", "_rpPushTok"),
             test_script=[*assert_answered("плоскость данных, выдающий после отзыва"),
                          "pm.test('REG-PUB-1-26: выдающий по-прежнему тянет X/app (200)', () => "
                          "pm.expect(pm.response.code, pm.response.text()).to.eql(200));"]),
    ],
))

# ---------------------------------------------------------------------------
# REG-PUB-1-17 и REG-PUB-1-29 — удаление реестра выдачу не отзывает; аноним получает отказ.
# ---------------------------------------------------------------------------
def _empty_repo(label, repo):
    return [
        Step(name=f"delete-tag-{label}", method="DELETE", path=f"{REG}/{_X}/repositories/{repo}/tags/latest",
             auth=ISSUER,
             test_script=[*assert_status(200), *assert_operation_envelope(_OP_PREFIX), *save_operation_id()]),
        poll_operation_until_done(auth=ISSUER),
        Step(name=f"delete-repository-{label}", method="DELETE", path=f"{REG}/{_X}/repositories/{repo}", auth=ISSUER,
             test_script=["pm.test('Дано REG-PUB-1-17: тег снят (done без error)', () => "
                          "pm.expect(pm.environment.get('lastOpError') || '').to.eql(''));",
                          *assert_status(200), *assert_operation_envelope(_OP_PREFIX), *save_operation_id()]),
        poll_operation_until_done(auth=ISSUER),
    ]


CASES.append(Case(
    id="REG-PUB-1-17",
    title="Выдача ПЧ на X; X опустошён и удалён → выдача существует с прежним id X (Р7); аноним /v2/X/app доходит до 404, "
          "тело равно телу REG-PUB-1-21 (REG-PUB-1-29); отзыв проходит",
    classes=["STATE", "CRUD", "AUTHZ"], priority="P1",
    steps=[
        *_grant("pub-x-before-delete", _grant_body(PUBLIC_READ_ROLE), "regPubGrantDel"),
        _stranger_reads_x("pub-x-before-delete", "Дано REG-PUB-1-17: выдача материализована (посторонний читает X)"),
        *_empty_repo("x-app", "app"),
        *_empty_repo("x-late", "late"),
        Step(name="delete-registry-x", method="DELETE", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=["pm.test('Дано REG-PUB-1-17: X опустошён (done без error)', () => "
                          "pm.expect(pm.environment.get('lastOpError') || '').to.eql(''));",
                          *assert_status(200), *assert_operation_envelope(_OP_PREFIX), *save_operation_id()]),
        poll_operation_until_done(auth=ISSUER),
        Step(name="registry-x-gone", method="GET", path=f"{REG}/{_X}", auth=ISSUER,
             test_script=["pm.test('Дано REG-PUB-1-17: X удалён (done без error)', () => "
                          "pm.expect(pm.environment.get('lastOpError') || '').to.eql(''));",
                          *assert_status(404)]),
        Step(name="grant-survives-registry", method="GET", path=f"{GRANTS}/{{{{regPubGrantDel}}}}", auth=ISSUER,
             test_script=[*assert_status(200), *_json_or_null(),
                          "const _t = (((_j || {}).target || {}).resources || {}).resources || [];",
                          "pm.test('REG-PUB-1-17: выдача существует, цель — прежний id X (Р7)', () => "
                          "pm.expect(_t.map(r => r.id), pm.response.text()).to.eql([pm.environment.get('regPubX')]));"]),
        # REG-PUB-1-29: кортежи репозитория сняты удалением, кортежи реестра — работником удаления.
        _anon_token("deleted", [f"{_X}/app"]),
        _anon_poll("anon-x-app-after-delete", f"/v2/{_X}/app/manifests/latest",
                   "pm.response.code === 404 && pm.response.text() === pm.environment.get('_regPubClosedBody')",
                   "REG-PUB-1-29: после удаления X аноним получает 404 с телом, побайтово равным REG-PUB-1-21"),
        *_revoke("pub-x-after-delete", "regPubGrantDel"),
    ],
))

# ---------------------------------------------------------------------------
# Уборка: Y (теги и репозитории), роли, удостоверение выдающего.
# ---------------------------------------------------------------------------
def _y_repo_cleanup(repo, tag):
    return [
        Step(name=f"cleanup-tag-y-{repo}", method="DELETE", path=f"{REG}/{_Y}/repositories/{repo}/tags/{tag}",
             auth=ISSUER,
             test_script=[*assert_status(200), *assert_operation_envelope(_OP_PREFIX), *save_operation_id()]),
        poll_operation_until_done(auth=ISSUER),
        Step(name=f"cleanup-repository-y-{repo}", method="DELETE", path=f"{REG}/{_Y}/repositories/{repo}",
             auth=ISSUER,
             test_script=[*assert_status(200), *assert_operation_envelope(_OP_PREFIX), *save_operation_id()]),
        poll_operation_until_done(auth=ISSUER),
    ]


def _delete_role(var):
    op_var = "_op_del_" + var
    return [
        _iam_mutation(f"cleanup-role-{var}", "DELETE", f"{ROLES}/{{{{{var}}}}}", None, op_var,
                      f"уборка: удаление роли {var} принято (200 и Operation)"),
        poll_operation_until_done(auth=ISSUER, op_var=op_var),
    ]


CASES.append(Case(
    id="REG-PUB-1-CLEANUP",
    title="Уборка: теги и репозитории Y, реестр Y, роли rol-reg-get и rol-reg-edit, удостоверение выдающего; GET Y → 404",
    classes=["CRUD"], priority="P2",
    steps=[
        *_y_repo_cleanup("app", "latest"),
        *_y_repo_cleanup("pub", "seed"),
        Step(name="cleanup-registry-y", method="DELETE", path=f"{REG}/{_Y}", auth=ISSUER,
             test_script=[*assert_status(200), *assert_operation_envelope(_OP_PREFIX), *save_operation_id()]),
        poll_operation_until_done(auth=ISSUER),
        Step(name="registry-y-gone", method="GET", path=f"{REG}/{_Y}", auth=ISSUER,
             test_script=["pm.test('уборка: Y удалён (done без error)', () => "
                          "pm.expect(pm.environment.get('lastOpError') || '').to.eql(''));",
                          *assert_status(404)]),
        *_delete_role("rolRegGet"),
        *_delete_role("rolRegEdit"),
        _iam_mutation("cleanup-issuer-secret", "DELETE",
                      "/iam/v1/serviceAccounts/{{svaAccountAdminAId}}/keys/{{regPubKeyId}}", None,
                      "_op_del_regPubKey", "уборка: удостоверение выдающего отозвано (200 и Operation)"),
        poll_operation_until_done(auth=ISSUER, op_var="_op_del_regPubKey"),
    ],
))
