# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Матрица доступа к admin-каталогу DiskType (kacho-storage) — шесть субъектов × три операции.

DiskType — не проектный ресурс, а глобальный admin-каталог, привязанный к кластеру
(`{cluster, *}` в каталоге прав): публичное чтение нужно КАЖДОМУ аутентифицированному
арендатору, потому что `diskTypeId` — обязательный вход Volume.Create; администрирование
каталога — только `system_admin` через InternalDiskTypeService.

Существующий `cases/disk-type.py` проверяет ПОВЕДЕНИЕ каталога (состав сида, тон
not-found, границы страницы) под одной личностью. Он ничего не говорит о том, КОМУ
эта поверхность открыта. Матрица ниже отвечает именно на это и повторяет ту, что у
compute-набора для его собственного DiskType: без неё сжатие раскола блочного хранения
унесло бы единственную живую проверку доступа к каталогу дисковых типов.

Две линии, различаемые намеренно (это разные утверждения, а не оттенки одного):
  * НЕТ учётных данных → 401 / code 16 UNAUTHENTICATED. Каталог публичен для
    аутентифицированных, но не анонимен — «читать может каждый» не означает «без входа».
  * ЕСТЬ учётные данные, нет права → 403 / code 7 PERMISSION_DENIED.
Смешать их в один «не-200» значило бы перестать замечать, что аутентификация отвалилась.

Строка чтения намеренно СТРОГАЯ (ровно 200 для пятерых): каталог кластерный, никакого
project-scope в запросе нет, поэтому здесь нет и того authz-first-порядка, из-за которого
проектные коллекции вынужденно толерантны к 403. Ослаблять её нечем и незачем.

# requires: authz-fixture стенд (authz enforced) с шестью личностями из общего seed'а.
"""

CASES = []

DT = "/storage/v1/diskTypes"
_SEEDED_ID = "block-balanced"   # сид миграции 0004; тот же id, что пинит disk-type.py

# (код, ярлык, env-переменная с bearer'ом). "anonymous" — gen.py снимает заголовок.
SUBJECTS = [
    ("ANON", "anon",       "anonymous"),
    ("NOB",  "no-bind",    "jwtNoBindings"),
    ("PA1",  "proj-adm",   "jwtProjectAdminA1"),
    ("AAA",  "acct-adm-a", "jwtAccountAdminA"),
    ("AAB",  "acct-adm-b", "jwtAccountAdminB"),
    ("INV",  "invitee",    "jwtInvitee"),
]

# Ожидания по линиям. Читать каталог вправе любой аутентифицированный (в том числе
# субъект вообще без единой выдачи — NOB: чтение каталога не выводится из выдач).
# Мутировать каталог не вправе никто из шести: ни один не является system_admin.
EXPECT = {
    "catalog-read":   {"ANON": "UNAUTH", "NOB": "ALLOW", "PA1": "ALLOW",
                       "AAA": "ALLOW", "AAB": "ALLOW", "INV": "ALLOW"},
    "catalog-mutate": {"ANON": "UNAUTH", "NOB": "DENY", "PA1": "DENY",
                       "AAA": "DENY", "AAB": "DENY", "INV": "DENY"},
}


def _allow(case_id):
    """Каталог кластерный → у ALLOW-субъекта ровно 200, без толерантности."""
    return [
        f"pm.test('[{case_id}] ALLOW: status 200', "
        f"() => pm.expect(pm.response.code, pm.response.text()).to.equal(200));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] ALLOW: not an error envelope', "
        f"() => pm.expect(j && j.code, JSON.stringify(j)).to.be.oneOf([undefined, null]));",
    ]


def _deny(case_id):
    """Аутентифицирован, но не system_admin → 403 / code 7 / `permission denied`."""
    return [
        f"pm.test('[{case_id}] DENY: status 403', "
        f"() => pm.expect(pm.response.code, pm.response.text()).to.equal(403));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] DENY: grpc code 7 (PERMISSION_DENIED)', "
        f"() => pm.expect(j && j.code, JSON.stringify(j)).to.equal(7));",
        f"pm.test('[{case_id}] DENY: message contains permission denied', "
        f"() => pm.expect((j && j.message || '').toLowerCase()).to.contain('permission denied'));",
    ]


def _unauth(case_id):
    """Учётных данных нет → 401 / code 16. Отдельно от DENY намеренно: 403 здесь
    означал бы, что анонимный запрос где-то обзавёлся принципалом."""
    return [
        f"pm.test('[{case_id}] UNAUTH: status 401', "
        f"() => pm.expect(pm.response.code, pm.response.text()).to.equal(401));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] UNAUTH: grpc code 16 (UNAUTHENTICATED)', "
        f"() => pm.expect(j && j.code, JSON.stringify(j)).to.equal(16));",
    ]


_VERDICT = {"ALLOW": _allow, "DENY": _deny, "UNAUTH": _unauth}


def _emit(op_code, op_title, scope, method, path, body):
    for code, label, auth in SUBJECTS:
        case_id = f"SDT-{op_code}-{code}"
        verdict = EXPECT[scope][code]
        CASES.append(Case(
            id=case_id,
            title=f"[{scope}] {op_title} субъектом {label} → {verdict}",
            classes=["AUTHZ", "SEC", "CATALOG",
                     "POS" if verdict == "ALLOW" else "NEG"],
            priority="P0",
            steps=[Step(name=f"{op_code.lower()}-{label}", method=method, path=path,
                        body=body, auth=auth,
                        test_script=_VERDICT[verdict](case_id))],
        ))


# Чтение каталога: коллекция и элемент — обе половины публичного read'а.
_emit("LS", "List diskTypes", "catalog-read", "GET", DT, None)
_emit("GT", f"Get diskTypes/{_SEEDED_ID}", "catalog-read", "GET", f"{DT}/{_SEEDED_ID}", None)

# Мутация каталога: тот же collection-путь, что у публичного чтения, но метод POST
# адресует InternalDiskTypeService (system_admin @ cluster). Тело намеренно валидное —
# отказ обязан быть по правам, а не по разбору тела, иначе кейс проверял бы валидацию.
_emit("CR", "Create diskType (admin-каталог)", "catalog-mutate", "POST", DT,
      {"id": "sdt-authz-probe-{{runId}}", "name": "sdt-authz-probe-{{runId}}",
       "description": "newman catalog-authz probe (must never be created)",
       "performanceTier": "balanced"})
