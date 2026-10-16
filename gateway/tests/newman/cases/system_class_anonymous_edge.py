# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set system_class_anonymous_edge — путь анонима и «вошедшего» через край.

Приёмка: PRO-Robotech/kaname docs/engineering/acceptance/anonymous-and-authenticated-are-subject-classes.md
(отпечаток fa4ff26f9b35dc6109f74a5e88996a66d3c21611c10ecbdcd8abe2f6bb21757e), стадия S3,
зеркало kacho#3238. Утверждается РАЗВЁРНУТЫЙ край (П4), второй эшелон: внутрипроцессная
цепочка П3 держит SC-30…35/38/45 Go-пробой, здесь — то, что видно только на стенде.

ЧТО УТВЕРЖДАЕТСЯ:

  * SC-36 — в стволе нет метода с полом «0», и аноним отвергается ВЕЗДЕ: каждая из девяти
    строк каталога `viewer` на `cluster` отвечает анониму `401` / `UNAUTHENTICATED`, а
    вошедшему без собственных выдач (`jwtPureNoBindings`) — `200` через системную выдачу
    чтения справочников. Пара «401 / 200» отличается ровно предъявителем.
  * SC-36 · Р7 / инвариант 5 — отказ анониму не различает «есть» и «нет»: `GET` региона,
    который есть, и региона правильной формы, которого нет, без удостоверения отвечают
    побайтово одним телом.
  * SC-41 — права вошедших после переезда прежние; «Дано» — переезд применён на стенде:
    системная выдача `acbc5d32d66fc8dc4952` несёт субъекта `system_class/authenticated` (SC-40).
  * SC-18 (краевая половина) — принципал ДРУГОГО арендатора читает объект `acc-A`, выданный
    классу `authenticated`, `200`; после отзыва опрос доходит до `403` /
    `PERMISSION_DENIED` (у строки `NetworkService/Get` нет `hide_existence`).

Классы свода: AUTHN, AUTHZ, CONF, NEG. Субъект «вошедший без выдач» — `jwtPureNoBindings`
(никогда не грантуемый, testing-newman layer4a), принципал другого арендатора для SC-18 —
он же: выдач в аккаунте A у него нет by construction.
"""

ASSERTS_DOMAIN = "gateway"
ASSERTS_REASON = (
    "предмет — РУБЕЖ КРАЯ для вызывающего без личности и для класса «вошедший»: отказ "
    "анониму на каждой строке каталога, неразличимость «есть / нет» в отказе и чтение "
    "объекта по выдаче классу через край. Маршруты geo/compute/storage/vpc/iam — носители; "
    "ресурсов этих служб модуль не утверждает."
)

CASES = []

SYSTEM_GRANT = "acbc5d32d66fc8dc4952"
# Системная роль чтения сетей (`vpc.network.view`, правила `[get, list]`).
ROLE_NETWORK_VIEW = "rolfe683216e63311d3f"

# Девять строк каталога `viewer` на `cluster` (перепись SC-36 на 2ec6e1e3d):
# (имя, путь списка, ключ массива ответа, путь Get по id либо None).
CLUSTER_VIEWER_ROWS = [
    ("machine-types", "/compute/v1/machineTypes", "machineTypes", "/compute/v1/machineTypes/"),
    ("regions", "/geo/v1/regions", "regions", "/geo/v1/regions/"),
    ("zones", "/geo/v1/zones", "zones", "/geo/v1/zones/"),
    ("disk-types", "/storage/v1/diskTypes", "diskTypes", "/storage/v1/diskTypes/"),
    ("permission-catalog", "/iam/v1/permissionCatalog", None, None),
]

_SENT_ANONYMOUS = [
    "pm.test('запрос ушёл БЕЗ Authorization (иначе это другой кейс)', () => {",
    "  const _sent = pm.request.headers.get('Authorization');",
    "  pm.expect(_sent === undefined || _sent === null || _sent === '', 'Authorization: ' + _sent).to.eql(true);",
    "});",
]

_REFUSED_401 = [
    *assert_status(401),
    "pm.test('код отказа — UNAUTHENTICATED (16)', () => {",
    "  let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
    "  pm.expect(j.code, pm.response.text()).to.eql(16);",
    "});",
]


def _anon_get(name, path):
    return Step(name=name, method="GET", path=path, mux="public", auth="anonymous",
                test_script=[*_SENT_ANONYMOUS, *_REFUSED_401])


def _signed_in_get(name, path, list_key=None, capture=None):
    script = [*assert_status(200)]
    if list_key:
        script += [
            "pm.test('ответ несёт непустой список', () => {",
            "  const j = pm.response.json();",
            f"  pm.expect(j[{js_str(list_key)}], JSON.stringify(j)).to.be.an('array').that.is.not.empty;",
            "});",
        ]
        if capture:
            script += [*save_from_response(f"j[{js_str(list_key)}][0].id", capture)]
    return Step(name=name, method="GET", path=path, mux="public", auth="jwtPureNoBindings",
                test_script=script)


# ---------------------------------------------------------------------------
# SC-36 — на каждой строке `viewer@cluster` аноним — 401, вошедший — 200.
# ---------------------------------------------------------------------------
for _name, _list, _key, _get in CLUSTER_VIEWER_ROWS:
    _cap = "sc36" + "".join(p.capitalize() for p in _name.split("-")) + "Id"
    _steps = [
        _signed_in_get(f"sc36-{_name}-list-signed-in", _list, _key, _cap if _get else None),
        _anon_get(f"sc36-{_name}-list-anonymous", _list),
    ]
    if _get:
        _steps += [
            _signed_in_get(f"sc36-{_name}-get-signed-in", _get + "{{" + _cap + "}}"),
            _anon_get(f"sc36-{_name}-get-anonymous", _get + "{{" + _cap + "}}"),
        ]
    CASES.append(Case(
        id=f"SC36-ANON-REFUSED-{_name.upper()}",
        title=f"SC-36: {_list} — без Authorization 401/UNAUTHENTICATED, вошедший без выдач 200",
        classes=["AUTHN", "AUTHZ", "NEG"],
        priority="P0",
        steps=_steps,
    ))


# ---------------------------------------------------------------------------
# SC-36 · Р7 — отказ анониму не различает «есть» и «нет».
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="SC36-ANON-REFUSAL-HIDES-EXISTENCE",
    title="SC-36/Р7: GET региона, который есть, и правильной формы, которого нет, без Authorization — "
          "одно тело 401 побайтово",
    classes=["AUTHN", "SEC", "NEG"],
    priority="P0",
    steps=[
        _signed_in_get("hide-list-regions-signed-in", "/geo/v1/regions", "regions", "sc36HideRegionId"),
        Step(
            name="hide-get-existing-anonymous",
            method="GET", path="/geo/v1/regions/{{sc36HideRegionId}}", mux="public", auth="anonymous",
            test_script=[*_SENT_ANONYMOUS, *_REFUSED_401,
                         "pm.environment.set('sc36HideExistingBody', pm.response.text());"],
        ),
        Step(
            name="hide-get-absent-anonymous",
            method="GET", path="/geo/v1/regions/reg00000000000000000", mux="public", auth="anonymous",
            test_script=[
                *_SENT_ANONYMOUS, *_REFUSED_401,
                "pm.test('тело отказа побайтово равно отказу на существующем', () => {",
                "  pm.expect(pm.response.text()).to.eql(pm.environment.get('sc36HideExistingBody'));",
                "});",
                "pm.environment.unset('sc36HideExistingBody');",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# SC-41 — права вошедших после переезда прежние; Дано — SC-40 на стенде.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="SC41-SIGNED-IN-RIGHTS-UNCHANGED-AFTER-MOVE",
    title="SC-41: системная выдача переехала на system_class/authenticated (Дано SC-40); "
          "вошедший без выдач читает /geo/v1/regions 200, без Authorization — 401",
    classes=["AUTHZ", "CONF", "STATE"],
    priority="P0",
    steps=[
        Step(
            name="sc41-given-system-grant-moved",
            method="GET", path=f"/iam/v1/accessBindings/{SYSTEM_GRANT}", mux="public", auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.test('Дано SC-40: субъект системной выдачи — system_class/authenticated', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j.subjectType, JSON.stringify(j)).to.eql('system_class');",
                "  pm.expect(j.subjectId, JSON.stringify(j)).to.eql('authenticated');",
                "  pm.expect(j.subjects, JSON.stringify(j)).to.eql([{type: 'SUBJECT_TYPE_SYSTEM_CLASS', id: 'authenticated'}]);",
                "  pm.expect(j.system, 'system').to.eql(true);",
                "  pm.expect(j.grantedRelation, 'grantedRelation').to.eql('viewer');",
                "});",
            ],
        ),
        _signed_in_get("sc41-regions-signed-in", "/geo/v1/regions", "regions"),
        _anon_get("sc41-regions-anonymous", "/geo/v1/regions"),
    ],
))


# ---------------------------------------------------------------------------
# SC-18 (край) — принципал другого арендатора читает объект, выданный классу.
# ---------------------------------------------------------------------------
# Префикс операции — по домену-владельцу пути (таблица `gateway/internal/opsproxy`):
# создание проекта и выдачи — IAM (`iop`), создание сети — VPC (`enp`). Путь вне
# таблицы роняет генерацию, а не утверждает чужой префикс на стенде.
_OP_PREFIX_BY_PATH = {
    "/iam/v1/projects": ("iop", "IAM"),
    "/iam/v1/accessBindings": ("iop", "IAM"),
    "/vpc/v1/networks": ("enp", "VPC"),
}


def _create(name, path, body, op_var, id_expr, id_var, auth="jwtAccountAdminA"):
    prefix, domain = _OP_PREFIX_BY_PATH[path]
    return [
        Step(
            name=name, method="POST", path=path, body=body, mux="public", auth=auth,
            test_script=[
                *assert_status(200),
                *assert_operation_envelope(prefix, domain),
                *save_from_response("j.id", op_var),
                *save_from_response(id_expr, id_var),
            ],
        ),
        poll_operation(op_var=op_var, auth=auth, name=f"{name}-poll"),
    ]


def _delete(name, path, op_var, guard_var, auth="jwtAccountAdminA"):
    return [
        Step(
            name=name, method="DELETE", path=path, mux="public", auth=auth,
            pre_script=[f"if (!pm.environment.get({js_str(guard_var)})) {{ pm.execution.skipRequest(); }}"],
            test_script=[
                "pm.test('уборка: снято (200) либо уже нет (404)', () => "
                "pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([200, 404]));",
                "if (pm.response.code === 200) {",
                *["  " + s for s in save_from_response("j.id", op_var)],
                "}",
            ],
        ),
        poll_operation(op_var=op_var, auth=auth, name=f"{name}-poll"),
    ]


_NETWORK_GET = "/vpc/v1/networks/{{sc18NetId}}"

CASES.append(Case(
    id="SC18-EDGE-OTHER-TENANT-READS-CLASS-GRANT",
    title="SC-18 край: выдача authenticated на сеть acc-A → принципал вне acc-A читает её 200; "
          "после отзыва опрос до 403/PERMISSION_DENIED",
    classes=["AUTHZ", "CRUD", "STATE"],
    priority="P0",
    steps=[
        *_create("sc18-create-project", "/iam/v1/projects",
                 {"accountId": "{{accountAId}}", "name": "sc18-prj-{{runId}}"},
                 "sc18PrjOpId", "j.metadata && j.metadata.projectId", "sc18PrjId"),
        *_create("sc18-create-network", "/vpc/v1/networks",
                 {"projectId": "{{sc18PrjId}}", "name": "sc18-net-{{runId}}"},
                 "sc18NetOpId", "j.metadata && j.metadata.networkId", "sc18NetId"),
        Step(
            name="sc18-precondition-outsider-denied",
            method="GET", path=_NETWORK_GET, mux="public", auth="jwtPureNoBindings",
            test_script=[
                *assert_status(403),
                "pm.test('предпосылка: без выдачи посторонний не читает (PERMISSION_DENIED)', () => {",
                "  pm.expect(pm.response.json().code, pm.response.text()).to.eql(7);",
                "});",
            ],
        ),
        *_create("sc18-grant-authenticated", "/iam/v1/accessBindings",
                 {"subjects": [{"type": "SUBJECT_TYPE_SYSTEM_CLASS", "id": "authenticated"}],
                  "roleId": ROLE_NETWORK_VIEW, "scopeType": "iam.project", "scopeId": "{{sc18PrjId}}",
                  "target": {"resources": {"resources": [{"type": "vpc.network", "id": "{{sc18NetId}}"}]}}},
                 "sc18AcbOpId", "j.metadata && j.metadata.accessBindingId", "sc18AcbId"),
        Step(
            name="sc18-outsider-reads",
            method="GET", path=_NETWORK_GET, mux="public", auth="jwtPureNoBindings",
            test_script=[
                *assert_status(200),
                "pm.test('тело — та самая сеть', () => pm.expect(pm.response.json().id).to.eql(pm.environment.get('sc18NetId')));",
            ],
        ),
        *_delete("sc18-revoke", "/iam/v1/accessBindings/{{sc18AcbId}}", "sc18AcbDelOpId", "sc18AcbId"),
        Step(
            name="sc18-outsider-denied-after-revoke",
            method="GET", path=_NETWORK_GET, mux="public", auth="jwtPureNoBindings",
            test_script=[
                "if (pm.environment.get('_sc18Started') !== pm.info.requestName) { pm.environment.set('_sc18Count', '0'); pm.environment.set('_sc18Started', pm.info.requestName); }",
                "const c = parseInt(pm.environment.get('_sc18Count') || '0', 10);",
                "if (pm.response.code !== 403 && c < 150) {",
                "  pm.environment.set('_sc18Count', String(c + 1));",
                "  const _w = Date.now(); while (Date.now() - _w < 200) void 0;",
                "  pm.execution.setNextRequest(pm.info.requestName);",
                "  return;",
                "}",
                "pm.environment.unset('_sc18Count'); pm.environment.unset('_sc18Started');",
                *assert_status(403),
                "pm.test('после отзыва — PERMISSION_DENIED', () => pm.expect(pm.response.json().code, pm.response.text()).to.eql(7));",
            ],
        ),
        *_delete("sc18-cleanup-network", _NETWORK_GET, "sc18NetDelOpId", "sc18NetId"),
        *_delete("sc18-cleanup-project", "/iam/v1/projects/{{sc18PrjId}}", "sc18PrjDelOpId", "sc18PrjId"),
    ],
))
