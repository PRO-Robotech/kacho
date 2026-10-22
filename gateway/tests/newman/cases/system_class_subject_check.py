# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set system_class_subject_check — выдача системному классу на сети платформы.

Приёмка: PRO-Robotech/kaname docs/engineering/acceptance/anonymous-and-authenticated-are-subject-classes.md,
редакция 7 (отпечаток 2b89df3e893c46598a098caa5b034264583d2fa53c3f529e0f90aad06af44b14),
DoD S1, пункт «SC-10, 11, 16, 18»: сквозной эшелон этих ID — на стенде платформы kacho,
задача PRO-Robotech/kacho#3256, с теми же утверждениями, что integration-пробы службы
доступа `system_class_s1_integration_test.go`. На автономном стенде службы они не
конструируемы: «Дано» каждого — сеть, ЗАРЕГИСТРИРОВАННАЯ её владельцем, а владелец сети
есть только на стенде платформы.

ПОЧЕМУ ДОМ — ПЛАТФОРМА (e2e-flow.md §7а). Предмет — СВЯЗКА службы доступа с чужим
доменом: выдача классу субъектов на объекте службы сетей, который служба сетей
зарегистрировала у службы доступа. Сущности службы доступа (форма выдачи, словарь
классов, отказы на имя класса — SC-01…09, 14, 17) модуль не переутверждает: их держит
набор службы доступа.

ЧТО УТВЕРЖДАЕТСЯ (пары «статус + исход» на каждом вопросе):

  * SC-10 — выдача классу `authenticated` на сети: вопрос анонима `allowed = false`,
    `denyReasons` пуст («пути нет»); близнецы — человек и учётка `allowed = true`.
  * SC-11 — выдача классу `anonymous`: вошедший человек и аноним `allowed = true`;
    группа без своих выдач `allowed = false` (группа классов не получает). «И»
    исключения Р2 (человек с неподтверждённым адресом) краем не задаётся — приёмка
    оставляет его integration-пробе с причиной.
  * SC-16 — развёртка `expandAccess` от кластерного администратора: человек прямой
    выдачи в ней есть ровно одним элементом, элемента вида `SUBJECT_TYPE_SYSTEM_CLASS`
    нет, `truncated = false`; близнец — перечень принципалов до классовой выдачи и после
    неё побайтово один.
  * SC-18 — выдача `authenticated` на сеть арендатора A: человек и учётка арендатора B
    без выдач в A `allowed = true`; соседняя сеть того же проекта закрыта; после отзыва
    выдачи опрос доходит до `allowed = false` у обоих. Краевую половину (чтение самой
    сети постороннему) держит модуль края `system_class_anonymous_edge` (kacho#3238).

«ДАНО» НА СТЕНДЕ ПЛАТФОРМЫ против принципалов приёмки:

  acc-A — `accountAId`, распорядитель `jwtAccountAdminA`; prj-A, obj-1, obj-2 — проект и
  сети, которые КАЖДЫЙ кейс заводит сам и снимает (самодостаточность, `{{runId}}`);
  usr-B — `userNOBId` (человек посева без выдач); sva-A — `svaAId` (учётка аккаунта A без
  выдач на сетях кейса); acc-B — `accountBId`, usr-C — `userAABId` (владелец аккаунта B,
  выдач в A нет); sva-B — учётка, которую кейс SC-18 заводит в аккаунте B предъявителем
  `jwtAccountAdminB` и снимает; usr-K — `jwtBootstrap` (кластерный администратор: вопросы
  `Check` о других субъектах и развёртка). Роль только чтения — системная
  `vpc.network.view` (`rolfe683216e63311d3f`, правила `[get, list]`).

РАСХОЖДЕНИЕ С БУКВОЙ SC-16, НАЗВАННОЕ, А НЕ СПРЯТАННОЕ. Приёмка ждёт `principals` ровно
из одного элемента. На стенде платформы у аккаунта A есть выдачи уровня аккаунта
(владелец, распорядитель посева), и они делают своих держателей читателями любой сети
аккаунта — развёртка честно называет и их. Свойство, ради которого стоит SC-16, —
«класс принципалом развёртки не становится и ответа не меняет», — утверждается
побайтовым равенством перечня до и после классовой выдачи и отсутствием элемента
класса; «ровно один» держит integration-проба службы на чистом мире. Вопрос к приёмке
вынесен в возврат полосы.

ОЖИДАНИЕ — ТОЛЬКО ОКНО МАТЕРИАЛИЗАЦИИ ПРАВ (e2e-flow.md#only-materialization-wait):
`Operation.done` выдачи — запись, а не видимость факта в модели. Первый вопрос после
выдачи (и после отзыва) повторяется до ожидаемого ответа с настоящей паузой и конечным
пределом; по исчерпании падает само утверждение. Вопросы-отрицания, заданные ПОСЛЕ
материализации, не повторяются.

Классы свода: AUTHZ, CONF, NEG, STATE, CRUD. Техники: таблица решений (вид субъекта ×
класс выдачи), переход состояний (до выдачи → выдача → отзыв), эквивалентные классы
вызывающих (свой арендатор, чужой, аноним, группа), близнец с одним изменённым фактом.
"""

ASSERTS_DOMAIN = "vpc"
ASSERTS_REASON = (
    "предмет — связка службы доступа с объектом службы сетей: выдача системному классу на "
    "сети, зарегистрированной её владельцем, и ответ модели прав о ней. Маршруты iam — "
    "носители вопроса, сущностей службы доступа модуль не утверждает."
)

CASES = []

# Системная роль чтения сетей (`vpc.network.view`, правила `[get, list]`).
ROLE_NETWORK_VIEW = "rolfe683216e63311d3f"

# Предел окна материализации: 150 вопросов с паузой 200 мс — 30 с. Тот же бюджет, что у
# опроса отзыва в `system_class_anonymous_edge` (SC-18 краевой половины).
MATERIALIZE_CAP = 150
MATERIALIZE_DELAY_MS = 200

# Префикс операции — по домену-владельцу пути (таблица `gateway/internal/opsproxy`):
# мутации IAM — `iop`, сети — `enp`. Путь вне таблицы роняет генерацию.
_OP_PREFIX_BY_PATH = {
    "/iam/v1/projects": "iop",
    "/iam/v1/accessBindings": "iop",
    "/iam/v1/groups": "iop",
    "/iam/v1/serviceAccounts": "iop",
    "/vpc/v1/networks": "enp",
}


def _op_envelope(prefix):
    return [
        f"pm.test({js_str(f'конверт Operation, id с префиксом {prefix}')}, () => {{",
        "  const j = pm.response.json();",
        f"  pm.expect(j.id, JSON.stringify(j)).to.match(/^{prefix}[a-z0-9]+$/);",
        "  pm.expect(j.done, 'done').to.be.a('boolean');",
        "});",
    ]


def _create(name, path, body, op_var, id_expr, id_var, auth="jwtAccountAdminA", pre=()):
    """Мутация заведения: 200 + Operation, опрос до done без error, id ресурса."""
    prefix = _OP_PREFIX_BY_PATH[path]
    return [
        Step(
            name=name, method="POST", path=path, body=body, mux="public", auth=auth, pre_script=list(pre),
            test_script=[
                *assert_status(200),
                *_op_envelope(prefix),
                *save_from_response("j.id", op_var),
                *save_from_response(id_expr, id_var),
            ],
        ),
        poll_operation(op_var=op_var, auth=auth, name=f"{name}-poll"),
    ]


def _delete(name, path, op_var, guard_var, auth="jwtAccountAdminA"):
    """Уборка: снято (200) либо уже нет (404); опрос снятия до done без error."""
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


def _project(tag):
    return _create(f"{tag}-create-project", "/iam/v1/projects",
                   {"accountId": "{{accountAId}}", "name": f"{tag}-prj-{{{{runId}}}}"},
                   f"{tag}PrjOpId", "j.metadata && j.metadata.projectId", f"{tag}PrjId")


def _network(tag, n):
    return _create(f"{tag}-create-network-{n}", "/vpc/v1/networks",
                   {"projectId": "{{" + tag + "PrjId}}", "name": f"{tag}-net{n}-{{{{runId}}}}"},
                   f"{tag}Net{n}OpId", "j.metadata && j.metadata.networkId", f"{tag}Net{n}Id")


def _grant(tag, subjects, net_var, acb_var, pre=()):
    return _create(f"{tag}-grant-{acb_var}", "/iam/v1/accessBindings",
                   {"subjects": subjects, "roleId": ROLE_NETWORK_VIEW, "scopeType": "iam.project",
                    "scopeId": "{{" + tag + "PrjId}}",
                    "target": {"resources": {"resources": [{"type": "vpc.network", "id": "{{" + net_var + "}}"}]}}},
                   f"{acb_var}OpId", "j.metadata && j.metadata.accessBindingId", acb_var, pre=pre)


def _klass(name):
    return {"type": "SUBJECT_TYPE_SYSTEM_CLASS", "id": name}


def _user(var):
    return {"type": "SUBJECT_TYPE_USER", "id": "{{" + var + "}}"}


def _check_body(subject, net_var):
    return {"subject": subject, "resource": {"type": "vpc_network", "id": "{{" + net_var + "}}"},
            "action": "vpc.networks.get"}


def _allowed_is(label, allowed):
    want = "true" if allowed else "false"
    return [
        *assert_status(200),
        f"pm.test({js_str(f'{label}: allowed = {want}')}, () => {{",
        "  const j = pm.response.json();",
        f"  pm.expect(j.allowed === true, JSON.stringify(j)).to.eql({want});",
        "});",
    ]


def check(name, subject, net_var, allowed, label, extra=(), pre=()):
    """`AuthorizeService.Check` от кластерного администратора, ОДИН вопрос без повтора."""
    return Step(name=name, method="POST", path="/iam/v1/authorize:check", body=_check_body(subject, net_var),
                mux="public", auth="jwtBootstrap", pre_script=list(pre),
                test_script=[*_allowed_is(label, allowed), *extra])


def check_until(name, subject, net_var, allowed, label, pre=()):
    """Первый вопрос после выдачи или отзыва: повтор до ожидаемого ответа в окне материализации.

    Повторяется ТОЛЬКО ответ 200 с иным `allowed`: любой другой статус — сразу к
    утверждениям (отказ края материализацией не объясняется).
    """
    want = "true" if allowed else "false"
    loop = [
        "if (pm.environment.get('_mzStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_mzCount', '0');",
        "  pm.environment.set('_mzStarted', pm.info.requestName);",
        "}",
        "const _mc = parseInt(pm.environment.get('_mzCount') || '0', 10);",
        "let _mj = {}; try { _mj = pm.response.json(); } catch (e) { _mj = {}; }",
        f"if (pm.response.code === 200 && (_mj.allowed === true) !== {want} && _mc < {MATERIALIZE_CAP}) {{",
        "  pm.environment.set('_mzCount', String(_mc + 1));",
        f"  const _mw = Date.now(); while (Date.now() - _mw < {MATERIALIZE_DELAY_MS}) void 0;",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_mzCount');",
        "pm.environment.unset('_mzStarted');",
    ]
    return Step(name=name, method="POST", path="/iam/v1/authorize:check", body=_check_body(subject, net_var),
                mux="public", auth="jwtBootstrap", pre_script=list(pre),
                test_script=[*loop, *_allowed_is(label, allowed)])


def _seed_slots(*names):
    pre = []
    for n in names:
        pre += require_env_slot(n, "субъект вопроса приёмки")
    return pre


def _cleanup(tag, nets, acbs=(), extra=()):
    steps = []
    for acb in acbs:
        steps += _delete(f"{tag}-cleanup-{acb}", "/iam/v1/accessBindings/{{" + acb + "}}", f"{acb}DelOpId", acb)
    steps += list(extra)
    for n in nets:
        steps += _delete(f"{tag}-cleanup-network-{n}", "/vpc/v1/networks/{{" + tag + f"Net{n}Id}}}}",
                         f"{tag}Net{n}DelOpId", f"{tag}Net{n}Id")
    # Проект — последним и с ожиданием окна доставки снятия регистрации: `done`
    # удаления сети не означает, что служба доступа уже не числит сеть и её служебную
    # группу безопасности (kacho#3300, шапка `delete_project_until_released`).
    steps += delete_project_until_released(f"{tag}-cleanup-project", f"{tag}PrjId", f"{tag}PrjDelOpId")
    return steps


# ---------------------------------------------------------------------------
# SC-10 — аноним не получает выданного «вошедшим».
# ---------------------------------------------------------------------------
_t = "sc10"
CASES.append(Case(
    id="SC10-ANONYMOUS-DOES-NOT-INHERIT-AUTHENTICATED",
    title="SC-10: выдача authenticated на сеть платформы → Check анонима allowed=false, denyReasons пуст; "
          "близнецы человек и учётка allowed=true",
    classes=["AUTHZ", "NEG", "CONF"],
    priority="P0",
    steps=[
        *_project(_t),
        *_network(_t, 1),
        *_grant(_t, [_klass("authenticated")], "sc10Net1Id", "sc10AcbId"),
        check_until("sc10-given-materialized-user", "user:{{userNOBId}}", "sc10Net1Id", True,
                              "Дано: выдача материализована — человек без выдач читает", pre=_seed_slots("userNOBId")),
        check("sc10-anonymous", "system_class:anonymous", "sc10Net1Id", False,
              "SC-10: аноним не выводит authenticated", extra=[
                  "pm.test('SC-10: denyReasons пуст («пути нет»)', () => {",
                  "  const j = pm.response.json();",
                  "  pm.expect(j.denyReasons || [], JSON.stringify(j)).to.eql([]);",
                  "});",
              ]),
        check("sc10-twin-service-account", "service_account:{{svaAId}}", "sc10Net1Id", True,
                        "SC-10 близнец: учётка", pre=_seed_slots("svaAId")),
        *_cleanup(_t, [1], ["sc10AcbId"]),
    ],
))


# ---------------------------------------------------------------------------
# SC-11 — вошедший получает выданное анониму; группа — нет.
# ---------------------------------------------------------------------------
_t = "sc11"
CASES.append(Case(
    id="SC11-SIGNED-IN-INHERITS-ANONYMOUS",
    title="SC-11: выдача anonymous на сеть платформы → человек и аноним allowed=true; "
          "группа без выдач allowed=false",
    classes=["AUTHZ", "NEG", "CONF"],
    priority="P0",
    steps=[
        *_project(_t),
        *_network(_t, 1),
        *_create("sc11-create-group", "/iam/v1/groups",
                 {"accountId": "{{accountAId}}", "name": "sc11-grp-{{runId}}"},
                 "sc11GrpOpId", "j.metadata && j.metadata.groupId", "sc11GrpId"),
        *_grant(_t, [_klass("anonymous")], "sc11Net1Id", "sc11AcbId"),
        check_until("sc11-signed-in-user", "user:{{userNOBId}}", "sc11Net1Id", True,
                              "SC-11: вошедший читает выданное анониму", pre=_seed_slots("userNOBId")),
        check("sc11-anonymous", "system_class:anonymous", "sc11Net1Id", True,
              "SC-11: аноним читает выданное анониму"),
        check("sc11-group-negative", "group:{{sc11GrpId}}#member", "sc11Net1Id", False,
              "SC-11 отрицание: группа классов не получает"),
        *_cleanup(_t, [1], ["sc11AcbId"], extra=_delete("sc11-cleanup-group", "/iam/v1/groups/{{sc11GrpId}}",
                                                       "sc11GrpDelOpId", "sc11GrpId")),
    ],
))


# ---------------------------------------------------------------------------
# SC-16 — развёртка прав классов не перечисляет.
# ---------------------------------------------------------------------------
_t = "sc16"
_EXPAND = ("/iam/v1/accessBindings:expandAccess?objectType=vpc_network&objectId={{sc16Net1Id}}"
           "&relation=v_get")


def _expand(name, save_as=None, compare_with=None):
    script = [
        *assert_status(200),
        "const j = pm.response.json();",
        "const _p = j.principals || [];",
        "pm.test('SC-16: человек прямой выдачи в развёртке ровно одним элементом', () => {",
        "  const u = _p.filter(p => p.type === 'SUBJECT_TYPE_USER' && p.id === pm.environment.get('userNOBId'));",
        "  pm.expect(u.length, JSON.stringify(j)).to.eql(1);",
        "});",
        "pm.test('SC-16: элемента вида SUBJECT_TYPE_SYSTEM_CLASS нет', () => {",
        "  pm.expect(_p.filter(p => p.type === 'SUBJECT_TYPE_SYSTEM_CLASS'), JSON.stringify(j)).to.eql([]);",
        "  pm.expect(_p.filter(p => p.id === 'authenticated' || p.id === 'anonymous'), JSON.stringify(j)).to.eql([]);",
        "});",
        "pm.test('SC-16: truncated = false', () => pm.expect(j.truncated === true, JSON.stringify(j)).to.eql(false));",
    ]
    if save_as:
        script.append(f"pm.environment.set({js_str(save_as)}, JSON.stringify(_p));")
    if compare_with:
        script += [
            "pm.test('SC-16 близнец: перечень принципалов побайтово тот же, что без классовой выдачи', () => {",
            f"  pm.expect(JSON.stringify(_p)).to.eql(pm.environment.get({js_str(compare_with)}));",
            "});",
            f"pm.environment.unset({js_str(compare_with)});",
        ]
    return Step(name=name, method="GET", path=_EXPAND, mux="public", auth="jwtBootstrap", test_script=script)


CASES.append(Case(
    id="SC16-EXPAND-ACCESS-DOES-NOT-LIST-CLASSES",
    title="SC-16: expandAccess на сеть платформы — человек прямой выдачи есть, элемента класса нет, "
          "truncated=false; перечень до и после выдачи authenticated побайтово один",
    classes=["AUTHZ", "CONF", "STATE"],
    priority="P0",
    steps=[
        *_project(_t),
        *_network(_t, 1),
        *_grant(_t, [_user("userNOBId")], "sc16Net1Id", "sc16UserAcbId", pre=_seed_slots("userNOBId")),
        check_until("sc16-given-user-materialized", "user:{{userNOBId}}", "sc16Net1Id", True,
                    "Дано: прямая выдача человеку материализована"),
        _expand("sc16-expand-before-class", save_as="sc16PrincipalsBefore"),
        *_grant(_t, [_klass("authenticated")], "sc16Net1Id", "sc16ClassAcbId"),
        check_until("sc16-given-class-materialized", "service_account:{{svaAId}}", "sc16Net1Id", True,
                              "Дано: классовая выдача материализована — учётка без выдач читает", pre=_seed_slots("svaAId")),
        _expand("sc16-expand-after-class", compare_with="sc16PrincipalsBefore"),
        *_cleanup(_t, [1], ["sc16ClassAcbId", "sc16UserAcbId"]),
    ],
))


# ---------------------------------------------------------------------------
# SC-18 — «вошедший» другого арендатора читает выданное классу в арендаторе A.
# ---------------------------------------------------------------------------
_t = "sc18q"
CASES.append(Case(
    id="SC18-OTHER-TENANT-SIGNED-IN-READS-NAMED-OBJECT",
    title="SC-18: выдача authenticated на сеть арендатора A → человек и учётка арендатора B allowed=true; "
          "соседняя сеть закрыта; после отзыва опрос доходит до allowed=false у обоих",
    classes=["AUTHZ", "NEG", "STATE"],
    priority="P0",
    steps=[
        *_project(_t),
        *_network(_t, 1),
        *_network(_t, 2),
        *_create("sc18q-create-sva-b", "/iam/v1/serviceAccounts",
                 {"accountId": "{{accountBId}}", "name": "sc18q-svab-{{runId}}"},
                 "sc18qSvaOpId", "j.metadata && j.metadata.serviceAccountId", "sc18qSvaBId",
                 auth="jwtAccountAdminB", pre=_seed_slots("accountBId")),
        check("sc18q-precondition-user-c-denied", "user:{{userAABId}}", "sc18qNet1Id", False,
                        "предпосылка: у человека арендатора B нет выдач в A", pre=_seed_slots("userAABId")),
        *_grant(_t, [_klass("authenticated")], "sc18qNet1Id", "sc18qAcbId"),
        check_until("sc18q-user-c-reads", "user:{{userAABId}}", "sc18qNet1Id", True,
                    "SC-18: человек арендатора B"),
        check("sc18q-sva-b-reads", "service_account:{{sc18qSvaBId}}", "sc18qNet1Id", True,
              "SC-18: учётка арендатора B"),
        check("sc18q-neighbour-closed", "user:{{userAABId}}", "sc18qNet2Id", False,
              "SC-18: выдача названного объекта не открывает соседний"),
        *_delete("sc18q-revoke", "/iam/v1/accessBindings/{{sc18qAcbId}}", "sc18qAcbDelOpId", "sc18qAcbId"),
        check_until("sc18q-user-c-after-revoke", "user:{{userAABId}}", "sc18qNet1Id", False,
                    "SC-18 близнец: после отзыва человек арендатора B"),
        check_until("sc18q-sva-b-after-revoke", "service_account:{{sc18qSvaBId}}", "sc18qNet1Id", False,
                    "SC-18 близнец: после отзыва учётка арендатора B"),
        *_cleanup(_t, [1, 2], extra=_delete("sc18q-cleanup-sva-b", "/iam/v1/serviceAccounts/{{sc18qSvaBId}}",
                                            "sc18qSvaDelOpId", "sc18qSvaBId", auth="jwtAccountAdminB")),
    ],
))
