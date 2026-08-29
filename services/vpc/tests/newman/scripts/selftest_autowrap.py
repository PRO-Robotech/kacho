#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Самопроверка предиката «первый доступ к СВОЕМУ свежему ресурсу обёрнут».

ПРЕДМЕТ. Kachō eventually-consistent: owner-tuple свежесозданного ресурса
материализуется вне мутации, поэтому ПЕРВОЕ обращение создателя к своему
ресурсу может кратко получить 403/404 (`testing.md` §e2e-инварианты). Норма
предписывает КЛИЕНТСКИЙ ограниченный ретрай (`retry_until_authorized`) — и
ТОЛЬКО на положительном первом доступе к своему; негативы, чужие аккаунты и
несуществующие id оборачивать запрещено (там ретрай маскировал бы реальный
отказ).

ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ. Обёртка ставилась вручную, поэтому пропуск
неотличим от решения. Замер по артефактам прогона CI 31002239590 (8 суит,
82 отчёта, 15648 утверждений): из 68 падений полосы видимости (403/404)
**42** пришлись на шаги БЕЗ обёртки вовсе, причём в одном и том же кейсе
соседние шаги той же формы обёрнуты — то есть это пропуск, а не замысел.
Гейт закрывает КЛАСС: предикат `_wrap_own_fresh_reads` в `gen.py` ставит
обёртку по свойству шага, а эта самопроверка доказывает, что предикат
(а) срабатывает на настоящем пропуске и (б) МОЛЧИТ на законном близнеце.

Запуск: python3 scripts/selftest_autowrap.py   (стенд и newman не нужны)
"""

from __future__ import annotations

import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import importlib.util  # noqa: E402

_spec = importlib.util.spec_from_file_location("gen_under_test", HERE / "gen.py")
gen = importlib.util.module_from_spec(_spec)
sys.argv = [sys.argv[0]]  # gen.py не должен разбирать наши аргументы
# Регистрация ДО exec_module: @dataclass резолвит типы через sys.modules[__module__],
# и без неё падает на разборе собственных аннотаций.
sys.modules["gen_under_test"] = gen
_spec.loader.exec_module(gen)

Step = gen.Step
Case = gen.Case

FAILURES: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"{'ok  ' if ok else 'FAIL'}  {name}" + (f"  — {detail}" if detail and not ok else ""))
    if not ok:
        FAILURES.append(name)


def wrapped(step) -> bool:
    return any("_authRetryCount" in ln for ln in step.test_script)


def steps_of(case: Case):
    # Полоса видимости передаётся ЯВНО: тело живёт в общем слое, а окно связывает
    # набор (`gen._rya`). Самопроверка обязана звать ровно то, что зовёт генератор,
    # — иначе она доказывала бы свойство своей копии, а не продукта.
    return gen._wrap_own_fresh_reads(case.steps, gen._rya)


# ---------------------------------------------------------------------------
# 1. ИНЪЕКЦИЯ НАСТОЯЩИМ ВХОДОМ: шаг-положительный первый доступ к своему свежему
#    ресурсу, записанный БЕЗ обёртки, обязан быть обёрнут предикатом.
#    Форма взята с натуры — так выглядели упавшие `del-a1` / `list-used` / `lbs`.
# ---------------------------------------------------------------------------
injected = Case(
    id="SELFTEST-INJECT", title="own fresh read without wrapper", classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}"},
             test_script=[*gen.assert_status(200),
                          *gen.save_from_response("j.metadata && j.metadata.addressId", "stAddrId")]),
        Step(name="del-own", method="DELETE", path="/vpc/v1/addresses/{{stAddrId}}",
             test_script=[*gen.assert_status(200)]),
    ],
)
out = steps_of(injected)
check("инъекция: необёрнутый положительный первый доступ к своему — обёрнут", wrapped(out[1]),
      "шаг остался без ограниченного ретрая — класс не закрыт")
check("инъекция: имя шага стало уникальным (self-retry резолвится в СЕБЯ)",
      out[1].name.startswith("del-own-rya"), out[1].name)
check("инъекция: собственные утверждения шага сохранены целиком",
      all(ln in out[1].test_script for ln in gen.assert_status(200)))

# ---------------------------------------------------------------------------
# 2. ИНЪЕКЦИЯ №4: ОТРИЦАТЕЛЬНАЯ проба СВОЕГО СВЕЖЕГО ресурса.
#
#    Она проверяет не отказ в доступе, а отказ по СУЩЕСТВУ («такой CIDR
#    отвергается»), и упирается в то же окно видимости: приходит 403 вместо
#    ожидаемого 400, и проба падает не по своему предмету. Ровно так упал
#    `add-cidr-v6-hostbits` в прогоне 31044886565.
#
#    Маскировки здесь нет по построению: пережидается только код, который шаг
#    исходом НЕ заявлял. Заявил 403 своим исходом — не оборачивается вовсе
#    (близнец ниже). Прежняя редакция этого пункта требовала «негатив не
#    оборачивается никогда» и потому запрещала ждать код, которого негатив не
#    ждёт, — то есть держала пробу красной по чужой причине.
# ---------------------------------------------------------------------------
neg = Case(
    id="SELFTEST-NEG", title="negative on own fresh resource", classes=["NEG"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/subnets",
             test_script=[*gen.assert_status(200), *gen.save_from_response("j.metadata && j.metadata.subnetId", "stSubId")]),
        Step(name="add-overlapping", method="POST", path="/vpc/v1/subnets/{{stSubId}}:addCidrBlocks",
             test_script=[*gen.assert_status(400)]),
    ],
)
negout = steps_of(neg)[1]
check("инъекция-4: отрицательная проба своего свежего ресурса — обёрнута", wrapped(negout))
check("инъекция-4: пережидается 403 и 404, но НЕ заявленный исход 400",
      "[403,404].includes" in "".join(negout.test_script).replace(" ", ""))

# ---------------------------------------------------------------------------
# 3. ЗАКОННЫЙ БЛИЗНЕЦ №2: шаг с собственной петлёй (поллер операции) — не трогаем,
#    иначе две петли на одном шаге и сломанный резолв имени.
# ---------------------------------------------------------------------------
poll = Case(
    id="SELFTEST-POLL", title="operation poller keeps its own loop", classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/networks",
             test_script=[*gen.assert_status(200), *gen.save_from_response("j.id", "opId")]),
        gen.poll_operation_until_done(),
    ],
)
check("близнец-поллер: шаг со своей петлёй не получает вторую", not wrapped(steps_of(poll)[1]))

# ---------------------------------------------------------------------------
# 4. ЗАКОННЫЙ БЛИЗНЕЦ №3: уже обёрнутый вручную шаг не оборачивается повторно.
# ---------------------------------------------------------------------------
manual = Case(
    id="SELFTEST-MANUAL", title="already wrapped by hand", classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/gateways",
             test_script=[*gen.assert_status(200), *gen.save_from_response("j.metadata && j.metadata.gatewayId", "stGwId")]),
        gen._rya(Step(name="get", method="GET", path="/vpc/v1/gateways/{{stGwId}}",
                                        test_script=[*gen.assert_status(200)])),
    ],
)
got = steps_of(manual)[1]
check("близнец-ручной: повторной обёртки нет",
      sum(1 for ln in got.test_script if "_authRetryStarted" in ln and "!==" in ln) == 1)
check("близнец-ручной: имя не переименовано дважды", got.name.count("-rya") == 1, got.name)

# ---------------------------------------------------------------------------
# 5. ЗАКОННЫЙ БЛИЗНЕЦ №4: обращение к ЧУЖОМУ (не созданному в этом кейсе) id —
#    предикат его не знает и молчит. Так негативы про absent-id остаются строгими.
# ---------------------------------------------------------------------------
foreign = Case(
    id="SELFTEST-FOREIGN", title="unknown id is not our fresh resource", classes=["NEG"], priority="P0",
    steps=[
        Step(name="get-absent", method="GET", path="/vpc/v1/networks/{{netAbsentId}}",
             test_script=[*gen.assert_status(200)]),
    ],
)
check("близнец-чужой: id, не рождённый в этом кейсе, не оборачивается",
      not wrapped(steps_of(foreign)[0]))

# ---------------------------------------------------------------------------
# 6. ИНЪЕКЦИЯ №2: шаг, у которого успешных исходов НЕСКОЛЬКО.
#
#    Уборка своего свежего ресурса часто утверждает не один код, а набор:
#    «удалилось (200) ЛИБО не удалось из-за состояния (400)». Это по-прежнему
#    ПОЛОЖИТЕЛЬНЫЙ первый доступ к своему — 403 в наборе НЕТ, то есть отказ
#    авторизации там исходом не считается и обязан пережидаться так же.
#    Пока предикат смотрел на буквальное `to.eql(200)`, такие шаги были ему
#    невидимы по построению: в суите vpc их 77 из 93 записей с набором исходов.
#    Форма взята с натуры — так записаны упавшие `cleanup-sg` (200|400) и
#    `cleanup-rt` (200|400|404).
# ---------------------------------------------------------------------------
multi = Case(
    id="SELFTEST-MULTI", title="own fresh cleanup with several accepted outcomes",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/securityGroups",
             test_script=[*gen.assert_status(200),
                          *gen.save_from_response("j.metadata && j.metadata.securityGroupId", "stSgId")]),
        Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{stSgId}}",
             test_script=["pm.test('cleanup sg 200 or 400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"]),
    ],
)
mout = steps_of(multi)
check("инъекция-2: шаг с набором исходов {200,400} на своём свежем — обёрнут", wrapped(mout[1]),
      "набор успешных исходов сделал шаг невидимым предикату")
check("инъекция-2: ретрай идёт по 403 И 404 (ни один из них исходом не заявлен)",
      "[403,404].includes" in "".join(mout[1].test_script).replace(" ", ""),
      "".join(ln for ln in mout[1].test_script if "includes" in ln))

# ---------------------------------------------------------------------------
# 7. ИНЪЕКЦИЯ №3: набор исходов СОДЕРЖИТ 404 — по нему ретраить нельзя.
#    404 здесь заявлен как законный исход («уже нет»), поэтому пережидать надо
#    ТОЛЬКО 403. Иначе обёртка жгла бы бюджет на исходе, который кейс принимает,
#    и превращала бы ожидание в ритуал.
# ---------------------------------------------------------------------------
multi404 = Case(
    id="SELFTEST-MULTI-404", title="accepted 404 must not be retried",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/routeTables",
             test_script=[*gen.assert_status(200),
                          *gen.save_from_response("j.metadata && j.metadata.routeTableId", "stRtId")]),
        Step(name="cleanup-rt", method="DELETE", path="/vpc/v1/routeTables/{{stRtId}}",
             test_script=["pm.test('cleanup rt (200/400/404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));"]),
    ],
)
m4 = steps_of(multi404)
body404 = "".join(m4[1].test_script).replace(" ", "")
check("инъекция-3: шаг с набором {200,400,404} на своём свежем — обёрнут", wrapped(m4[1]))
check("инъекция-3: ретрай ТОЛЬКО по 403 — заявленный исход 404 не пережидается",
      "[403].includes" in body404, body404[:160])

# ---------------------------------------------------------------------------
# 8. ЗАКОННЫЙ БЛИЗНЕЦ №5: набор исходов СОДЕРЖИТ 403 (authz-first толерантность
#    негатива). Там отказ — заявленный исход, а не окно: обёртка запрещена,
#    иначе она пережидала бы ровно то, что кейс и проверяет.
# ---------------------------------------------------------------------------
authzfirst = Case(
    id="SELFTEST-AUTHZFIRST", title="403 is an accepted outcome — never wrap",
    classes=["NEG"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/networks",
             test_script=[*gen.assert_status(200),
                          *gen.save_from_response("j.metadata && j.metadata.networkId", "stNetId")]),
        Step(name="cross-account-get", method="GET", path="/vpc/v1/networks/{{stNetId}}",
             test_script=["pm.test('denied', () => pm.expect(pm.response.code).to.be.oneOf([200, 403, 404]));"]),
    ],
)
check("близнец-authz-first: шаг, принимающий 403 исходом, НЕ обёрнут",
      not wrapped(steps_of(authzfirst)[1]))

# ---------------------------------------------------------------------------
# 9. ЗАКОННЫЙ БЛИЗНЕЦ №6: набор gRPC-кодов (`j.code`), а не HTTP. Числа там из
#    другого пространства (5, 9, 10) и на полосу видимости не отображаются —
#    предикат обязан их не читать вовсе, то есть считать набор HTTP-исходов
#    ПУСТЫМ. Шаг при этом оборачивается (утверждать про HTTP-код ему нечего,
#    маскировать нечего), но ждёт ТОЛЬКО 403: у уборки 404 — законное «уже нет».
# ---------------------------------------------------------------------------
grpccodes = Case(
    id="SELFTEST-GRPCCODES", title="grpc code set is not an http outcome set",
    classes=["NEG"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/subnets",
             test_script=[*gen.assert_status(200),
                          *gen.save_from_response("j.metadata && j.metadata.subnetId", "stSub2Id")]),
        Step(name="del-conflict", method="DELETE", path="/vpc/v1/subnets/{{stSub2Id}}",
             test_script=["pm.test('grpc 5 or 9', () => {",
                          "  const j = pm.response.json();",
                          "  pm.expect(j.code, JSON.stringify(j)).to.be.oneOf([5, 9]);",
                          "});"]),
    ],
)
def guard_line(step) -> str:
    """Строка ожидания — только она; собственные утверждения шага сюда не входят
    (в них законно стоит свой набор чисел, и мерить по всему телу значило бы
    спрашивать не о том)."""
    for ln in step.test_script:
        if ".includes(pm.response.code)" in ln:
            return ln.replace(" ", "")
    return ""


gout = steps_of(grpccodes)[1]
gguard = guard_line(gout)
check("близнец-grpc: числа gRPC-кодов не попали в набор HTTP-исходов",
      gguard.startswith("if([403].includes"), gguard[:120])

# ---------------------------------------------------------------------------
# 11. ИНЪЕКЦИЯ №5: ЧТЕНИЕ ждёт 404, а не только 403.
#
#     Полосу задаёт МЕТОД: у мутации отказ виден как 403, у чтения он спрятан
#     под 404 (текст побайтово равен настоящему «не найдено»). Рукописное
#     `retry_on=(403,)` на GET — обёртка, не способная сработать на том коде,
#     который она увидит. Такое место в дереве было ровно одно и падало
#     (`get-no-dhcp`, прогон 31044886565): 404 на первом обращении, ноль
#     повторов. Чинится по построению в `retry_until_authorized`.
# ---------------------------------------------------------------------------
readlane = gen._rya(
    Step(name="get-own", method="GET", path="/vpc/v1/subnets/{{stSubId}}",
         test_script=[*gen.assert_status(200)]),
    retry_on=(403,))
rbody = "".join(readlane.test_script).replace(" ", "")
check("инъекция-5: рукописное ожидание только 403 на ЧТЕНИИ дополняется 404",
      "[403,404].includes" in rbody, rbody[:140])

# ---------------------------------------------------------------------------
# 12. ЗАКОННЫЙ БЛИЗНЕЦ №7: чтение, которое САМО заявляет 404 законным исходом
#     («убедиться, что удалено»). Здесь 404 — предмет пробы, и добавлять его в
#     ожидание нельзя: проба ждала бы ровно то, что проверяет.
# ---------------------------------------------------------------------------
confirmgone = gen._rya(
    Step(name="confirm-deleted", method="GET", path="/vpc/v1/subnets/{{stSubId}}",
         test_script=[*gen.assert_status(404)]),
    retry_on=(403,))
cbody = "".join(confirmgone.test_script).replace(" ", "")
check("близнец-подтверждение-удаления: заявленный 404 в ожидание НЕ добавляется",
      "[403].includes" in cbody, cbody[:140])

# ---------------------------------------------------------------------------
# 13. ИНЪЕКЦИЯ №6: цель проверки прав названа в ТЕЛЕ запроса, а не в адресе.
#
#     Край решает вопрос о правах над объектом, который берёт из ПОЛЯ ЗАПРОСА
#     (`scope_extractor.from_request_field` каталога прав). У создания вложенного
#     ресурса адрес — коллекционный (`/nlb/v1/listeners`), а свежий родитель
#     назван в теле, поэтому условие, читающее только `st.path`, такой шаг не
#     видит ПО ПОСТРОЕНИЮ — сколько бы раз он ни упирался в окно видимости.
#
#     Пропуск здесь не гипотетический и стоит дороже обычного: шаг создаёт
#     ФИКСТУРУ, на которой стоит предмет кейса. Не создалась — кейс идёт дальше
#     по несозданной ссылке, и предмет («удаление отвергается, пока на группу
#     ссылаются») проверяется на группе, на которую никто не ссылается. Продукт
#     отвечает верно, а кейс краснеет утверждением о ссылочной целостности:
#     красное указывает не туда, где дефект, и заводит работу, которой нет
#     предмета.
#
#     Соседние шаги ТОЙ ЖЕ формы в другой суите обёрнуты ВРУЧНУЮ (все создания
#     слушателя в `services/nlb/tests/newman/cases/listener.py`) — то есть это
#     ровно тот пропуск, который предикат и заводился закрыть.
#
#     ЧЕМ НЕ ЛЕЧИТСЯ. Ожиданием на СОСЕДНЕЙ полосе: чтение родителя гейтится
#     отношением `v_get`, а создание вложенного — `editor` (каталог прав,
#     `NetworkLoadBalancerService/Get` против `ListenerService/Create`). Дождаться
#     первого и заключить о втором нельзя: это разные отношения, и прокси-предикат
#     ломается раньше своего предмета.
# ---------------------------------------------------------------------------
bodyscope = Case(
    id="SELFTEST-BODY-SCOPE", title="authz target named in the request body",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create-parent", method="POST", path="/nlb/v1/networkLoadBalancers",
             body={"projectId": "{{_suiteProjectId}}"},
             test_script=[*gen.assert_status(200),
                          *gen.save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "stNlbId")]),
        Step(name="wire-child", method="POST", path="/nlb/v1/listeners",
             body={"loadBalancerId": "{{stNlbId}}", "port": 80},
             test_script=[*gen.assert_status(200)]),
    ],
)
check("инъекция-6: свежая цель прав в ТЕЛЕ — шаг обёрнут", wrapped(steps_of(bodyscope)[1]),
      "цель проверки прав названа в теле, а условие читает только адрес — класс открыт")

# ---------------------------------------------------------------------------
# 14. ЗАКОННЫЙ БЛИЗНЕЦ №8: в теле — ЧУЖОЙ/посеянный id, не рождённый в этом
#     кейсе. Окна видимости у него нет (ресурс существует давно), и обёртка
#     превратила бы отказ по существу в ожидание длиной в бюджет. Предикат
#     обязан молчать — иначе он ловит форму «переменная в теле», а не существо
#     «свежая цель прав».
# ---------------------------------------------------------------------------
bodyforeign = Case(
    id="SELFTEST-BODY-FOREIGN", title="body names a seeded, non-fresh id",
    classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create-parent", method="POST", path="/nlb/v1/networkLoadBalancers",
             body={"projectId": "{{_suiteProjectId}}"},
             test_script=[*gen.assert_status(200),
                          *gen.save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "stNlbId2")]),
        Step(name="wire-foreign", method="POST", path="/nlb/v1/listeners",
             body={"loadBalancerId": "{{existingLbId}}", "port": 80},
             test_script=[*gen.assert_status(200)]),
    ],
)
check("близнец-8: чужой (не рождённый в кейсе) id в теле — НЕ оборачивается",
      not wrapped(steps_of(bodyforeign)[1]))

# ---------------------------------------------------------------------------
# 15. ЗАКОННЫЙ БЛИЗНЕЦ №9: отказ в доступе и есть предмет шага (403 объявлен
#     исходом) — тело называет свежий ресурс, но ждать нечего.
# ---------------------------------------------------------------------------
bodydeny = Case(
    id="SELFTEST-BODY-DENY", title="cross-account deny names a fresh id in the body",
    classes=["NEG"], priority="P0",
    steps=[
        Step(name="create-parent", method="POST", path="/nlb/v1/networkLoadBalancers",
             body={"projectId": "{{_suiteProjectId}}"},
             test_script=[*gen.assert_status(200),
                          *gen.save_from_response("j.metadata && j.metadata.networkLoadBalancerId", "stNlbId3")]),
        Step(name="wire-denied", method="POST", path="/nlb/v1/listeners",
             body={"loadBalancerId": "{{stNlbId3}}", "port": 80},
             test_script=[*gen.assert_status(403)]),
    ],
)
check("близнец-9: 403 объявлен исходом шага — НЕ оборачивается",
      not wrapped(steps_of(bodydeny)[1]))

# ---------------------------------------------------------------------------
# 9а. АДРЕС ОПРОСА ОПЕРАЦИИ: шаг опрашивает ТУ переменную, которую назвали, и
#     `id_expr` без захвата ОТВЕРГАЕТСЯ, а не принимается молча.
#
#     ПРЕДМЕТ. `id_expr` — выражение выбора id РЕСУРСА из `metadata`, и читается
#     оно только вместе с `capture_id_to`. Кейс публичного пула передал его,
#     рассчитывая перенаправить ОПРОС на свою переменную; генератор выражение
#     принял, не прочёл и оставил в адресе литерал `{{opId}}`, которого в той
#     суите не заполняет никто. Три шага опроса не утверждали ничего, пока
#     страж неразрешённой подстановки не отказался отправлять запрос.
#
#     Исходов у такого поля три, и «принять и выбросить» — не один из них.
# ---------------------------------------------------------------------------
try:
    gen.poll_operation_until_done(id_expr="pm.environment.get('adm1PoolOp')")
    check("инъекция: id_expr без capture_id_to отвергается", False,
          "выражение принято молча — вызывающий уверен, что опрос перенаправлен")
except ValueError:
    check("инъекция: id_expr без capture_id_to отвергается", True)

_twin = gen.poll_operation_until_done(capture_id_to="zcAddrId",
                                      id_expr="j.metadata && j.metadata.addressId")
check("близнец-10: id_expr ВМЕСТЕ с захватом — законная пара, проходит",
      _twin.path == "/operations/{{opId}}")

_named = gen.poll_operation_until_done(op_var="adm1PoolOp")
check("op_var меняет АДРЕС опроса", _named.path == "/operations/{{adm1PoolOp}}")
check("op_var меняет и РАННИЙ ВЫХОД — адрес и страж читают одно имя",
      any("pm.environment.get('adm1PoolOp')" in ln for ln in _named.test_script)
      and not any("pm.environment.get('opId')" in ln for ln in _named.test_script))

_default = gen.poll_operation_until_done()
check("близнец-11: умолчание не изменилось — прочие суиты опрашивают opId",
      _default.path == "/operations/{{opId}}"
      and any("pm.environment.get('opId')" in ln for ln in _default.test_script))

# ---------------------------------------------------------------------------
# 9б. АКТОР ОПРОСА ВЫВОДИТСЯ ИЗ ТОГО, КТО ОПЕРАЦИЮ СОЗДАЛ.
#
#     ПРЕДМЕТ. `OperationService.Get` энфорсит владение и отвечает чужому
#     `NotFound`, а не отказом. Опрос под другим актором получает 404, и
#     выглядит это как задержка материализации в продукте — диагноз оказывается
#     в шести шагах от причины. Наблюдалось: администратор облака создавал пул,
#     опрос уходил дефолтным проектным актором, `operation … not found`, и за
#     ним каскадом падали ещё девять шагов, ни один из которых виноват не был.
#
#     Актор опроса — СЛЕДСТВИЕ, а не решение автора кейса, поэтому он выводится.
#     Аргумент можно забыть, и забвение молчаливо; вывод забыть нельзя.
# ---------------------------------------------------------------------------
def _poll_actor(steps):
    for st in gen.normalize_steps(steps):
        if st.name.startswith("poll-op"):
            return st.auth
    return "шага опроса нет"


_admin_issuer = [
    Step(name="create", method="POST", path="/vpc/v1/addressPools", auth="jwtBootstrap",
         body={"name": "p"},
         test_script=[*gen.assert_status(200), *gen.save_from_response("j.id", "opId")]),
    gen.poll_operation_until_done(),
]
check("инъекция: опрос наследует актора издателя операции",
      _poll_actor(_admin_issuer) == "jwtBootstrap",
      f"получено {_poll_actor(_admin_issuer)!r}")

_default_issuer = [
    Step(name="create", method="POST", path="/vpc/v1/networks", body={"name": "n"},
         test_script=[*gen.assert_status(200), *gen.save_from_response("j.id", "opId")]),
    gen.poll_operation_until_done(),
]
check("близнец-12: издатель под умолчанием коллекции — опрос актора НЕ получает",
      _poll_actor(_default_issuer) is None,
      f"получено {_poll_actor(_default_issuer)!r}")

_explicit_actor = [
    Step(name="create", method="POST", path="/vpc/v1/addressPools", auth="jwtBootstrap",
         body={"name": "p"},
         test_script=[*gen.assert_status(200), *gen.save_from_response("j.id", "opId")]),
    gen.poll_operation_until_done(auth="jwtProjectAdminB1"),
]
check("близнец-13: явно заданный актор опроса сильнее вывода — кейс о ЧУЖОЙ операции",
      _poll_actor(_explicit_actor) == "jwtProjectAdminB1",
      f"получено {_poll_actor(_explicit_actor)!r}")

_last_issuer = [
    Step(name="create-admin", method="POST", path="/vpc/v1/addressPools", auth="jwtBootstrap",
         body={"name": "p"},
         test_script=[*gen.assert_status(200), *gen.save_from_response("j.id", "opId")]),
    Step(name="create-tenant", method="POST", path="/vpc/v1/networks", body={"name": "n"},
         test_script=[*gen.assert_status(200), *gen.save_from_response("j.id", "opId")]),
    gen.poll_operation_until_done(),
]
check("близнец-14: берётся ПОСЛЕДНИЙ издатель имени, а не первый",
      _poll_actor(_last_issuer) is None,
      f"получено {_poll_actor(_last_issuer)!r}")

# ---------------------------------------------------------------------------
# 10. ОБЪЁМ ОСМОТРЕННОГО: «ноль находок» обязано быть отличимо от «ноль прочитанного».
# ---------------------------------------------------------------------------
_ALL = (injected, neg, poll, manual, foreign, multi, multi404, authzfirst, grpccodes,
        bodyscope, bodyforeign, bodydeny)
print()
print(f"осмотрено кейсов самопроверки: {len(_ALL)}, шагов: "
      f"{sum(len(c.steps) for c in _ALL)}")

if FAILURES:
    print(f"\nSELFTEST FAIL: {len(FAILURES)} — " + "; ".join(FAILURES), file=sys.stderr)
    sys.exit(1)
print("SELFTEST OK")
