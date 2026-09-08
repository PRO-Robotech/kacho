#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Фикстуры list-filter-d для vpc поверх матрицы служебных учёток (#59).

Пообъектная фильтрация списка требует права на ОДИН объект: субъект S
(subset-viewer) видит РОВНО ОДНУ подсеть проекта, субъект N — ни одной, скрытая
подсеть не выдана никому.

# Чем это выдаётся — и почему больше не кортежами

Здесь стояла прямая запись кортежей в журнал `kaname.fga_outbox`
(`vpc_subnet#v_list/v_get`), и она **перестала действовать**, оставаясь по виду
исправной. Проекция журнала в прямые факты **намеренно пропускает глаголы**
(миграция 0100: «глагол выводится из выдачи и копией не хранится»), поэтому
строка принималась, проекции не возникало, а фикстура об этом не знала: посев
печатал переменные и выходил успехом, а падал через двадцать минут кейс, который
называл виновником список.

Сегодня право на один объект выражается тем же путём, каким его выражает
продукт, — **ролью с перечнем объектов** (`resourceNames`) и привязкой этой роли
на проект. Тот же путь проходит арендатор в консоли; фикстура перестала быть
снисходительнее продукта и заодно перестала зависеть от внутренней формы
хранения.

# Что осталось прежним

Право уровня проекта (`viewer`) выдаётся обоим субъектам — это метод-гейт: без
него список отвечает отказом метода, и пообъектная фильтрация не проверяется
вовсе. Оба субъекта — служебные учётки с токеном RS256.

Читает /tmp/matrix.json (токен запуска + acctA), заводит ресурсы и выдачи и
печатает на стандартный вывод ТОЛЬКО дополнительные фикстуры list-filter.
"""
from __future__ import annotations

import json
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import mint_rs256 as m  # noqa: E402
import prodseed_matrix as pm  # noqa: E402  (reuse helpers: _curl,_await,make_sa,sa_token,etc.)

MATRIX = json.loads(open("/tmp/matrix.json").read())
# Mint a FRESH bootstrap token — the cached jwtBootstrap has a 1h TTL and may have
# expired since the matrix was seeded (silent code=16 "token validation failed").
pm.boot = m.mint_bootstrap()   # rebind module-level boot for helper reuse
boot = pm.boot
acctA = MATRIX["accountAId"]
RID = pm.RID


def _curl_role_with_names(account_id, name, subnet_id):
    """Роль, чьё правило названо ПЕРЕЧНЕМ объектов (`resourceNames`).

    `pm.custom_role` перечня не принимает — он заводит роль на весь тип, а нам
    нужна ровно одна подсеть. Форма правила та же, что у арендатора в консоли,
    и ту же форму проверяет проба паритета в наборе iam.
    """
    listed = pm._curl("GET", f"/iam/v1/roles?accountId={account_id}&pageSize=1000", boot)
    for r in listed.get("roles") or []:
        if r.get("name") == name:
            return r.get("id", "")
    resp = pm._curl("POST", "/iam/v1/roles", boot, {
        "accountId": account_id, "name": name,
        "description": "newman fixture: право на ОДНУ подсеть",
        "rules": [{"module": "vpc", "resources": ["subnet"],
                   "verbs": ["get", "list"], "resourceNames": [subnet_id]}],
    })
    return pm._await(resp, boot, "roleId")


def assert_subject_sees_subnet(token, project_id, subnet_id, must_see):
    """Утверждать, что субъект ВИДИТ (или не видит) подсеть — его собственным чтением.

    Спрашиваем не хранилище, а КРАЙ, под токеном самого субъекта: посев обязан
    утверждать способность, а не форму записи. Проверка внутренней таблицы
    зеленела бы и тогда, когда путь чтения до неё не доходит.

    Материализация выдачи eventually-consistent, поэтому ограниченный повтор —
    законный: он ждёт ОКНА, а не маскирует отказ. Не сошлось за отведённое —
    посев падает громко и называет, чего именно не видно.
    """
    deadline = time.time() + 30
    last = None
    while time.time() < deadline:
        r = pm._curl("GET", f"/vpc/v1/subnets?projectId={project_id}&pageSize=1000",
                     token, base=pm.PUBLIC)
        # ОТКАЗ — НЕ ПУСТАЯ СТРАНИЦА. Помощник края не смотрит на HTTP-статус, и
        # тело отказа (`{"code":7,...}`) читается как список без ключа `subnets`,
        # то есть неотличимо от «прав нет ни на один объект». Пустая страница у
        # 200 несёт ключ `subnets` (пустым массивом либо отсутствующим полем при
        # непустом ответе), поэтому различаем по наличию признака отказа.
        if isinstance(r, dict) and ("code" in r or "message" in r) and "subnets" not in r:
            raise SystemExit(
                f"[prodseed_vpc_ext] край ОТВЕРГ чтение списка подсетей проекта "
                f"{project_id}: {r}. Это отказ, а не пустая страница: без этого "
                "различения посев назвал бы виновником пообъектную фильтрацию, "
                "тогда как не пройден гейт метода.")
        ids = [x.get("id") for x in (r.get("subnets") or [])]
        last = ids
        if (subnet_id in ids) == must_see:
            return
        time.sleep(1)
    raise SystemExit(
        f"[prodseed_vpc_ext] выдача не сошлась за 30с: подсеть {subnet_id} "
        f"{'НЕ видна' if must_see else 'видна, хотя не выдана'}; список вернул {last}.\n"
        "Посев обязан утверждать СПОСОБНОСТЬ: без этой проверки кейсы list-filter-d "
        "утверждали бы видимость, которой нет, и падение назвало бы виновником список.")


# 1) list-filter project + network + visible/hidden subnets (as bootstrap admin).
lf_proj = pm._await(pm._curl("POST", "/iam/v1/projects", boot,
                             {"accountId": acctA, "name": f"ps-lf-{RID}"}), boot, "projectId")
# Сеть объявляет адресный план: сеть без него подсеть не принимает (sync 400) —
# нарезать не из чего. Посев без плана был бы снисходительнее продукта и
# обрушил бы всё, что стоит на подсети (адрес, интерфейс, балансировщик).
lf_net = pm._await(pm._curl("POST", "/vpc/v1/networks", boot,
                            {"projectId": lf_proj, "name": f"ps-lf-net-{RID}",
                             "ipv4CidrBlocks": ["10.193.0.0/16"]}), boot, "networkId")
lf_vis = pm._await(pm._curl("POST", "/vpc/v1/subnets", boot,
                            {"projectId": lf_proj, "networkId": lf_net, "name": f"ps-lf-vis-{RID}",
                             "zoneId": "ru-central1-a", "ipv4CidrPrimary": "10.193.0.0/24"}), boot, "subnetId")
lf_hid = pm._await(pm._curl("POST", "/vpc/v1/subnets", boot,
                            {"projectId": lf_proj, "networkId": lf_net, "name": f"ps-lf-hid-{RID}",
                             "zoneId": "ru-central1-a", "ipv4CidrPrimary": "10.193.1.0/24"}), boot, "subnetId")

# 2) two SA subjects (no AccessBinding grants — pure FGA tuple grants below).
sva_sv = pm.make_sa(acctA, f"ps-lf-sv-{RID}")   # subset-viewer S
sva_ng = pm.make_sa(acctA, f"ps-lf-ng-{RID}")   # no-subnet-grant N
tok_sv = pm.sa_token(sva_sv)
tok_ng = pm.sa_token(sva_ng)

# 3) Выдачи — тем же путём, каким их выражает продукт.
#
#    (а) МЕТОД-ГЕЙТ: право уровня проекта обоим субъектам. Без него список
#        отвечает отказом метода, и пообъектная фильтрация не проверяется вовсе:
#        кейс краснел бы, не дойдя до предмета.
# ИМЯ РОЛИ — НЕ ТО ЖЕ, ЧТО ИМЯ УЧЁТКИ. Форма имени роли не допускает дефиса:
#   custom — ^[a-z][a-z0-9_]{0,40}$,  system — ^roles/[a-z]+\.[a-z]+$
# Соседние фикстуры зовут служебные учётки через дефис, и скопированный оттуда
# стиль дал синхронный отказ на самом первом создании роли. Сервер назвал
# причину дословно — и назвал её потому, что посев больше не глотает отказ.
#        Гейт края для `SubnetService/List` требует отношение УРОВНЯ ОБЛАСТИ —
#        `viewer` на объекте-проекте, — а НЕ глагол на подсети. Роль на весь тип
#        `subnet` его не производит: она даёт `v_get`/`v_list` на КАЖДОЙ подсети
#        проекта. Такая роль (а) метод-гейт не открывает, поэтому список отвечает
#        отказом, и (б) разрушает предмет пункта (б) ниже — субъект видел бы ВСЁ,
#        включая подсеть, которая ему не выдана. Поэтому здесь сеется ровно факт
#        уровня области, тем же путём, каким матрица сеет пол чтения каталога.
pm.seed_fga_tuple(f"service_account:{sva_sv}", "viewer", f"project:{lf_proj}")
pm.seed_fga_tuple(f"service_account:{sva_ng}", "viewer", f"project:{lf_proj}")

#    (б) ПООБЪЕКТНОЕ ПРАВО: роль, чьё правило названо ПЕРЕЧНЕМ объектов
#        (`resourceNames`), привязанная субъекту S на тот же проект. Видимой
#        оказывается ровно одна подсеть; скрытая не названа нигде.
#
#        Прежде здесь стояла прямая запись кортежей в журнал, и она перестала
#        действовать, оставаясь по виду исправной: проекция журнала намеренно
#        пропускает глаголы («глагол выводится из выдачи и копией не хранится»,
#        миграция 0100). Строка принималась, права не возникало, посев молчал.
role_one = _curl_role_with_names(acctA, f"ps_lf_one_{RID.replace('-', '_')}", lf_vis)
pm.grant(sva_sv, role_one, "project", lf_proj)

#    Утверждение СПОСОБНОСТИ, а не факта записи: право обязано быть ВИДНО тому,
#    кому выдано. Журнал, принявший намерение и ничего не материализовавший,
#    выглядит ровно как успешный посев — эту разницу и ловим здесь, в момент
#    посева, а не в чужом кейсе через двадцать минут.
assert_subject_sees_subnet(tok_sv, lf_proj, lf_vis, must_see=True)
assert_subject_sees_subnet(tok_sv, lf_proj, lf_hid, must_see=False)
assert_subject_sees_subnet(tok_ng, lf_proj, lf_vis, must_see=False)

print(json.dumps({
    "listFilterProjectId": lf_proj,
    "subnetVisibleId": lf_vis,
    "subnetHiddenId": lf_hid,
    "jwtSubnetSubsetViewer": tok_sv,
    "jwtNoSubnetGrant": tok_ng,
}))
