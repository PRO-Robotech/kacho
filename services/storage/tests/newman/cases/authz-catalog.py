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

Три линии отказа, различаемые намеренно (это разные утверждения, а не оттенки одного):
  * НЕТ учётных данных на ОПУБЛИКОВАННОМ маршруте (GET коллекции и элемента) → 401 /
    code 16 UNAUTHENTICATED. Каталог публичен для аутентифицированных, но не анонимен —
    «читать может каждый» не означает «без входа».
  * Административная мутация (POST коллекции) у внешнего слушателя не опубликована
    вовсе: InternalDiskTypeService живёт только на внутреннем слушателе (ban #6). Сторож
    маршрута внешнего слушателя (kacho#3053) отвечает на неё раньше аутентификации и
    прав — одинаково для всех шести, включая анонима: путь совпал с публичным шаблоном
    (GET), метод — нет, и край отдаёт промах grpc-gateway «метода нет» 501 / code 12
    `Method Not Allowed`, тот же, что на метод, которого нет нигде. Прежние 401 и 403
    на этой строке называли внутренний метод снаружи поимённо; это и снято.
  * ЕСТЬ учётные данные, нет права на ОПУБЛИКОВАННОМ маршруте → 403 / code 7 — в этой
    матрице такой строки нет: опубликованное здесь только чтение, и оно открыто всем
    опознанным.
Смешать их в один «не-200» значило бы перестать замечать, что аутентификация отвалилась
либо что внутренний метод снова виден снаружи.

Строка чтения намеренно СТРОГАЯ (ровно 200 для пятерых): каталог кластерный, никакого
project-scope в запросе нет, поэтому здесь нет и того authz-first-порядка, из-за которого
проектные коллекции вынужденно толерантны к 403. Ослаблять её нечем и незачем.

# requires: authz-fixture стенд (authz enforced) с шестью личностями из общего seed'а.
"""

CASES = []

DT = "/storage/v1/diskTypes"
# Цель админских отрицаний — ЛЮБОЙ адрес админской полосы, а не конкретный класс.
# Прежде здесь стоял слаг посева миграции; посев снят, и пин на него означал бы
# отрицание, чья цель не существует, — отказ пришёл бы «не найдено» вместо
# «не ваше», и кейс зеленел бы не тем.
_ABSENT_ID = "block-newman-nx-{{runId}}"

# Класс, который СОЗДАЁТ посев стенда. Читающие кейсы обязаны обращаться к
# существующему: публичное чтение каталога отдаёт 200 всякому опознанному
# субъекту, и на отсутствующем id проверка прав подменяется проверкой наличия —
# кейс зеленел бы (или краснел) не о том.
_PRESENT_ID = "block-balanced"

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
# Мутации каталога у внешнего слушателя нет ни для кого: ответ «метода нет» не
# зависит от субъекта, поэтому строка одна на всех шестерых, аноним в том числе.
EXPECT = {
    "catalog-read":   {"ANON": "UNAUTH", "NOB": "ALLOW", "PA1": "ALLOW",
                       "AAA": "ALLOW", "AAB": "ALLOW", "INV": "ALLOW"},
    "catalog-mutate": {"ANON": "NOROUTE", "NOB": "NOROUTE", "PA1": "NOROUTE",
                       "AAA": "NOROUTE", "AAB": "NOROUTE", "INV": "NOROUTE"},
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


def _noroute(case_id):
    """Метод не опубликован у внешнего слушателя → промах grpc-gateway «метода нет»:
    501 / code 12 / `Method Not Allowed` / пустые details — тело целиком
    (общий производитель утверждения — `assert_edge_route_miss`, gen_shared).

    Тело сверяется ЦЕЛИКОМ: этот ответ производит сторож маршрута тем же
    производителем, что промах publicMux, и он обязан быть одним и тем же для
    внутреннего метода и для метода, которого нет нигде. Любая добавка — имя
    метода, право, причина в details — снова назвала бы внутреннее снаружи."""
    return assert_edge_route_miss("method")


def _unauth(case_id):
    """Учётных данных нет на опубликованном маршруте → 401 / code 16. Отдельно от отказа
    по правам намеренно: 403 здесь означал бы, что анонимный запрос где-то обзавёлся
    принципалом."""
    return [
        f"pm.test('[{case_id}] UNAUTH: status 401', "
        f"() => pm.expect(pm.response.code, pm.response.text()).to.equal(401));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] UNAUTH: grpc code 16 (UNAUTHENTICATED)', "
        f"() => pm.expect(j && j.code, JSON.stringify(j)).to.equal(16));",
    ]


_VERDICT = {"ALLOW": _allow, "NOROUTE": _noroute, "UNAUTH": _unauth}


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
_emit("GT", f"Get diskTypes/{_PRESENT_ID}", "catalog-read", "GET", f"{DT}/{_PRESENT_ID}", None)

# Мутация каталога: тот же collection-путь, что у публичного чтения, но метод POST
# адресует InternalDiskTypeService (system_admin @ cluster), которого у внешнего
# слушателя нет. Тело намеренно валидное — отказ обязан быть по маршруту, а не по
# разбору тела, иначе кейс проверял бы валидацию.
_emit("CR", "Create diskType (admin-каталог)", "catalog-mutate", "POST", DT,
      {"id": "sdt-authz-probe-{{runId}}", "name": "sdt-authz-probe-{{runId}}",
       "description": "newman catalog-authz probe (must never be created)",
       "tier": "BALANCED"})
