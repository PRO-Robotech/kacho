#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""VPC list-filter-d fixtures on top of the production-mode SA matrix (#59).

Per-object filtered-List needs FGA tuples that the public AccessBinding API cannot
express (vpc_subnet object-scope). Subject S (subset-viewer) gets project#viewer
(method-gate) + vpc_subnet#v_list/v_get on ONE visible subnet; subject N
(no-subnet-grant) gets project#viewer only (List returns 200 empty — the read tier
on the project does NOT cascade visibility onto subnets in the explicit model). The
hidden subnet is granted to nobody (no-leak). Both subjects are RS256 ServiceAccount
principals.

The project-level tuple grants exactly the relation the permission catalog declares
for a top-level project List, which is the whole point of these fixtures: a subject
handed precisely what the catalog states must be able to list. It said `v_list` until
2026-07-29 and the service enforced the read tier `viewer`; since neither relation is
derived from the other, the seeded subjects satisfied one gate and not the other and
the three list cases were red on a live grant. The annotations were corrected, so this
now writes `viewer` — the value has moved, the intent has not.

Reads /tmp/matrix.json (boot token + acctA), seeds the resources + tuples, and
emits ONLY the list-filter extra fixtures on stdout.
"""
from __future__ import annotations

import json
import subprocess
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


def _psql(sql):
    """Выполнить SQL в базе iam и вернуть вывод. Отказ — ГРОМКИЙ.

    Прежняя редакция звала kubectl и выбрасывала результат: ни кода возврата, ни
    stderr. Неверное имя пода, отсутствующий контекст, недоступный кластер — всё
    это выглядело как успешный посев, а обнаруживалось через двадцать минут
    падением сквозной пробы, которая называла виновником список.
    """
    args = ["kubectl", "-n", "kacho", "exec", "kacho-umbrella-pg-iam-0", "-c", "postgresql",
            "--", "sh", "-c",
            f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kacho_iam -h 127.0.0.1 -tAc "{sql}"']
    r = subprocess.run(args, capture_output=True, text=True)
    if r.returncode != 0:
        raise SystemExit(
            f"[prodseed_vpc_ext] ОТКАЗ обращения к базе iam (код {r.returncode}).\n"
            f"  запрос: {sql[:120]}\n"
            f"  stderr: {r.stderr.strip()[:400]}\n"
            "Посев пообъектных выдач НЕ состоялся. Дальше идти нельзя: кейсы "
            "list-filter-d утверждают видимость, которой без этих строк не будет, "
            "и падение назовёт виновником список, а не посев.")
    return r.stdout.strip()


def fga_write(user, relation, obj):
    """Записать прямой факт через журнал iam (триггер проецирует его в relation_fact)."""
    sql = (
        "INSERT INTO kacho_iam.fga_outbox (event_type, payload, created_at) "
        "SELECT 'fga.tuple.write', "
        f"jsonb_build_object('user','{user}','relation','{relation}','object','{obj}'), now() "
        "WHERE NOT EXISTS (SELECT 1 FROM kacho_iam.fga_outbox "
        f"WHERE payload->>'user'='{user}' AND payload->>'relation'='{relation}' AND payload->>'object'='{obj}');"
    )
    _psql(sql)


def assert_fact_visible(user, relation, obj):
    """Утверждать СПОСОБНОСТЬ, а не факт вставки.

    Строка в журнале — это намерение; правом она становится, когда её спроецирует
    триггер в `relation_fact`, откуда её читает форма вердикта. Проверять надо
    именно проекцию: журнал, из которого ничего не спроецировалось, выглядит
    ровно как успешный посев.
    """
    otype, _, oid = obj.partition(":")
    n = _psql(
        "SELECT count(*) FROM kacho_iam.relation_fact "
        f"WHERE object_type='{otype}' AND object_id='{oid}' "
        f"AND relation='{relation}' AND subject='{user}';")
    if n != "1":
        raise SystemExit(
            f"[prodseed_vpc_ext] выдача НЕ материализовалась: {user} {relation} {obj} — "
            f"строк в relation_fact {n!r}, ожидалась 1.\n"
            "Журнал принял намерение, но проекции нет — значит право не действует "
            "ни для одного вопроса, и сквозные кейсы упадут на видимости.")


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

# 3) FGA tuples (service_account subjects).
#    method-gate: both get project#viewer → List returns 200 (not method-403).
fga_write(f"service_account:{sva_sv}", "viewer", f"project:{lf_proj}")
fga_write(f"service_account:{sva_ng}", "viewer", f"project:{lf_proj}")
#    per-object visibility: only S sees the visible subnet; hidden granted to nobody.
fga_write(f"service_account:{sva_sv}", "v_list", f"vpc_subnet:{lf_vis}")
fga_write(f"service_account:{sva_sv}", "v_get", f"vpc_subnet:{lf_vis}")

# Проекция журнала в прямые факты — синхронный триггер, поэтому ждать нечего:
# ждали здесь три секунды во времена, когда строку уносил дренаж во внешнее
# хранилище. Вместо ожидания — утверждение: право обязано быть ВИДНО.
assert_fact_visible(f"service_account:{sva_sv}", "viewer", f"project:{lf_proj}")
assert_fact_visible(f"service_account:{sva_ng}", "viewer", f"project:{lf_proj}")
assert_fact_visible(f"service_account:{sva_sv}", "v_list", f"vpc_subnet:{lf_vis}")
assert_fact_visible(f"service_account:{sva_sv}", "v_get", f"vpc_subnet:{lf_vis}")

print(json.dumps({
    "listFilterProjectId": lf_proj,
    "subnetVisibleId": lf_vis,
    "subnetHiddenId": lf_hid,
    "jwtSubnetSubsetViewer": tok_sv,
    "jwtNoSubnetGrant": tok_ng,
}))
