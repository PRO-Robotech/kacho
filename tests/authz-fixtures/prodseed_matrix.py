#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Production-mode SA-principal matrix seed for the newman regression suites (#59).

Generalises `prodseed_network.py` from the single vpc `network` collection to the
whole 6-subject authz matrix + per-service resource deps. EVERY authenticating
token is a Hydra-signed RS256 ServiceAccount token (acr-exempt, api-audience) — no
HS256 dev-bypass, no interactive OIDC. The authz-deny EXPECT matrices are purely
grant-semantic (editor/viewer/admin/no-grant/cross-account/cross-project), so each
"subject" slot is backed by a ServiceAccount with the exact bindings the matrix
assumes; the principal being an SA rather than a human User does not change the
FGA relation resolved.

WHAT IS NOT MINTED HERE, AND WHY — stated per credential, because the previous blanket
sentence was WRONG for half of them and that error cost nine assertions that never ran:

  - `apiTokenValid` / `apiTokenRevoked` / `apiTokenMalformed` — minted (see the block in
    `seed()`). These are SERVICE-ACCOUNT KEYS, not human credentials; the case-file says
    so itself. Lumping them with the step-up token was the mistake.
  - `apiTokenExpired` — not minted here, and not fakeable: the provider signs the token
    and owns its lifetime, so "already expired" cannot be produced at seed time. It needs
    its own wave that CREATES the condition (issue, then outwait the lifetime) — that wave
    now EXISTS: `services/iam/tests/newman/scripts/run-expired-bearer.sh`. It is not part
    of the default parallel run because it takes as long as a token lives; until it has
    actually been run, its assertions stay a counted open debt, not a mask.
    THE LIFETIME IS 14400s (4h), NOT 900s — this line said 900s and that was wrong.
    Measured three independent ways (2026-08-04): `exp - iat` on four issued tokens; the
    provider's own config (`ttl.access_token: 4h`); and the absence of a per-client
    override on this stand (`KANAME_SAKEY_ACCESSTOKENTTL` unset →
    `IssueSAKeyUseCase.accessTokenLifespan()` returns "" → the provider default applies).
    The magnitude is the whole point for whoever schedules the wave: "outwait 15 minutes"
    and "outwait 4 hours" are different decisions. Note that
    `IssueSAKeyRequest.ttl_seconds` does NOT shorten it — that field bounds the KEY's own
    expiry row, not the `access_token_lifespan` stamped on the OAuth client, so it cannot
    be used to make the wave quicker.

THE OTHER STRUCTURAL LIMIT, MEASURED 2026-07-30 — no human principal reaches the edge.
An Account is owned by a User by construction, so a case that CREATES an account needs a
human caller. There is no machine path to one, and this is not for want of trying:
`UserTokenService.Issue` accepts no audience (the request message has no such field) and
the use-case's `AudiencePrefix` is never set by the composition root, so the issued
client carries an EMPTY audience whitelist. Measured end-to-end on the production-posture
stand: requesting the api audience at the exchange is refused ("Requested audience ... has
not been whitelisted"); exchanging without one yields `aud: []`, which the edge rejects
401. Separately, a client_credentials token carries no `acr`, and 236 of the 294 catalog
entries require acr >= 1. Account.Create itself is `<exempt>` so acr is not its blocker —
the audience is. Every case asserting "the caller is user X" therefore stays RED under
this seed, and that is a counted debt, not something to paper over here.

  The denominator moves whenever the catalog does, so recheck it rather than trusting
  this line; it has already been stale twice. Command:
      python3 -c "import json;d=json.load(open('gateway/internal/middleware/embed/permission_catalog.json'));\
  print(sum(1 for e in d if str(e.get('required_acr_min','')) in ('1','2')),'of',len(d))"
  History of this sentence: it read "292 of the 357", then "236 of the 300"; both were
  true of the catalog they were written against and false by the time they were read.
  The twin sentence in prodseed_all.py carried the "292 of the 357" reading and was
  corrected to match in the same change that added this note — so neither file still
  states it, and a reader sent looking for it there would find nothing.

Usage:
    prodseed_matrix.py [--deps vpc,compute,storage,registry,nlb,iam] > fixtures.json

Emits a superset fixtures dict on stdout; patch-env.py merges it into any suite env.
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import mint_rs256 as m  # noqa: E402
# Declared id↔token pairings + their check. Own module because THIS one mints the
# bootstrap Bearer at import time — a checker that cannot be imported without a
# cluster cannot be self-tested, and this is the part that must be provable offline.
from principal_pairings import unpaired_principals  # noqa: E402

# Endpoints. Defaults match the port-forwards the newman drivers establish
# (deploy/scripts/newman-{e2e,parallel}.sh); every one is env-overridable so the
# same seeder works behind a different forward map without editing the file.
INTERNAL = os.environ.get("INTERNAL_BASE_URL", "http://localhost:18081")
PUBLIC = os.environ.get("BASE_URL", "http://localhost:18080")
IAM_GRPC = os.environ.get("IAM_INTERNAL_GRPC", "localhost:19091")
# ЗДЕСЬ СТОЯЛИ АДРЕС ОБМЕНА У ПРЕЖНЕГО ИЗДАТЕЛЯ И АДРЕСАТ ЕГО УТВЕРЖДЕНИЯ —
# у них не осталось читателя (задача #1120).
#
# Ключ служебной учётки на переведённом контуре зеркала у прежнего издателя не
# заводит, поэтому обменять его там нельзя ни при каком входе: клиента с таким
# именем он не знает. Обе величины поэтому сняты, а не оставлены «на случай» —
# оставленная величина читается следующим как действующая полоса.
# Requested token audience = api-gateway ExpectedAudience ("https://"+APIDomain).
API_AUD = os.environ.get("API_AUDIENCE", "https://api.kacho.cloud")
# ── ВТОРАЯ ПОЛОСА КРАЯ: НАШ издатель (задача #1014) ───────────────────────
# Адресат УТВЕРЖДЕНИЯ у нашей полосы — идентификатор издателя, а не адрес
# эндпоинта (проверяющий сверяет его с `signer.Issuer()`); адрес обмена — наш
# `POST /iam/v1/token` на поверхности выдачи. Обе величины живут в mint_rs256,
# здесь только читаются, чтобы объявление осталось в ОДНОМ месте.
PLATFORM_ASSERT_AUD = m.PLATFORM_ASSERTION_AUDIENCE
PLATFORM_TOKEN_URL = m.PLATFORM_TOKEN_URL
# GATEWAY-identity client cert for the iam :9091 grpcurl calls (NOT the mint's
# operator identity — see mint_rs256.ensure_iam_internal_cert).
MTLS_CERT = m.IAM_INTERNAL_MTLS_CERT
MTLS_KEY = m.IAM_INTERNAL_MTLS_KEY
PG_IAM_POD = os.environ.get("PG_IAM_POD", "kacho-umbrella-pg-iam-0")
KACHO_NS = os.environ.get("KACHO_NAMESPACE", "kacho")

ROLE_ADMIN = "rol21232f297a57a5a74"  # md5('admin')[:17] -> FGA admin
ROLE_EDIT = "rolde95b43bceeb4b998"   # md5('edit')[:17]  -> FGA editor
ROLE_VIEW = "rol1bda80f2be4d3658e"   # md5('view')[:17]  -> FGA viewer

RID = str(int(time.time()))[-6:]

# Zones of the seeded geo baseline. `e` is the nlb-DEDICATED zone (the suite that owns
# a v6-carrying default EXTERNAL_PUBLIC pool); it must exist before
# deploy/scripts/seed-nlb-fixtures.sh will seed that pool, and keeping it OUT of zone
# `a` is what lets the vpc case ADR-CR-EXT-V6-FAMILY-FALLTHROUGH keep asserting that
# zone `a` has no v6-capable pool. This module is now the SOLE owner of that table —
# the dev-path block it used to mirror is gone along with the symmetric-minting branch.
ZONE_SUFFIXES = tuple(os.environ.get("SEED_ZONES", "a,b,c,d,e").split(","))
NLB_ZONE = os.environ.get("NLB_ZONE", "ru-central1-e")
# The second region — the value `existingRegionAltId` carries. It is seeded below,
# with one zone, precisely so that "the other region" is one; see _seed_geo_catalog.
ALT_REGION = os.environ.get("SEED_ALT_REGION", "ru-central2")


def _curl(method, path, token, body=None, base=PUBLIC):
    args = ["curl", "-sS", "-m", "20", "-X", method, "-H", "Content-Type: application/json"]
    if token:
        args += ["-H", f"Authorization: Bearer {token}"]
    if body is not None:
        args += ["--data", json.dumps(body)]
    args.append(base + path)
    out = subprocess.run(args, capture_output=True, text=True).stdout
    try:
        return json.loads(out or "{}")
    except json.JSONDecodeError:
        return {"raw": out}


def _poll(op_id, token, budget=40):
    deadline = time.time() + budget
    while time.time() < deadline:
        d = _curl("GET", f"/operations/{op_id}", token)
        if d.get("done"):
            return d
        time.sleep(0.4)
    return {}


def _await(resp, token, key):
    op_id = resp.get("id")
    if not op_id:
        raise RuntimeError(f"no op id: {resp}")
    done = _poll(op_id, token)
    if done.get("error"):
        raise RuntimeError(f"op {op_id} errored: {done['error']}")
    return (done.get("metadata") or {}).get(key, "")


def _psql(sql):
    """Run a read-only query against the iam DB from inside its own pod."""
    args = ["kubectl", "-n", KACHO_NS, "exec", PG_IAM_POD, "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kaname -h 127.0.0.1 -tAc "{sql}"']
    return subprocess.run(args, capture_output=True, text=True).stdout.strip()


def upsert_user(ext_id, wait_s=40):
    """InternalUserService.UpsertFromIdentity → user id, waited to DURABILITY.

    The returned Operation is NOT pollable from here, and that is CORRECT, not a defect:
    the call arrives on the cluster-internal listener over mTLS, so no end-user principal
    is forwarded and the Operation is stamped `system/bootstrap`. The gateway's ops-proxy
    ownership check then refuses to let ANY tenant principal — our ServiceAccount admin
    included — read a system-owned Operation, because that would be a cross-tenant BOLA
    (gateway/internal/opsproxy, TestOpsProxy_Get_OwnershipCheck_DeniesTenantReadingSystem
    OwnedOp). Polling it anyway just burns the whole budget and then continues blind.

    So wait on what we actually need instead: the `users` row itself. `metadata.userId` is
    PRE-ALLOCATED and comes back even when the async worker later fails, so returning it
    unchecked would hand out a phantom id — the same class as reading an id out of an
    Operation without checking `error` first. The row appearing IS the durability signal
    (and migration 0049 rejects an AccessBinding whose subject User does not exist, which
    is exactly what a phantom id would produce three steps later).
    """
    body = json.dumps({"externalId": ext_id, "email": ext_id, "displayName": ext_id})
    args = ["grpcurl", "-insecure", "-cert", MTLS_CERT, "-key", MTLS_KEY,
            "-d", body, IAM_GRPC, "kacho.cloud.iam.v1.InternalUserService/UpsertFromIdentity"]
    out = subprocess.run(args, capture_output=True, text=True).stdout
    d = json.loads(out or "{}")
    uid = (d.get("metadata") or {}).get("userId", "")
    if not uid:
        return ""
    deadline = time.time() + wait_s
    while time.time() < deadline:
        if _psql(f"SELECT 1 FROM kaname.users WHERE id='{uid}' LIMIT 1;") == "1":
            return uid
        time.sleep(1)
    raise RuntimeError(f"user {ext_id} ({uid}) never became durable in {wait_s}s — "
                       f"the upsert worker failed; the id would be a phantom")


def db_lookup(ext_id):
    """Discover a user's personal account + default project (ids the production
    upsert created; every real auth stays RS256)."""
    sql = (f"SET search_path=kaname,public; "
           f"SELECT a.id||'|'||p.id FROM accounts a "
           f"JOIN users u ON u.id=a.owner_user_id "
           f"JOIN projects p ON p.account_id=a.id AND p.name='default' "
           f"WHERE u.external_id='{ext_id}' LIMIT 1;")
    args = ["kubectl", "-n", KACHO_NS, "exec", PG_IAM_POD, "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kaname -h 127.0.0.1 -tAc "{sql}"']
    for _ in range(25):
        out = subprocess.run(args, capture_output=True, text=True).stdout.strip()
        row = next((ln for ln in out.splitlines() if "|" in ln), None)
        if row:
            acct, proj = row.split("|")
            return acct.strip(), proj.strip()
        time.sleep(1)
    raise RuntimeError(f"db_lookup({ext_id}) empty after retries")


def make_sa(account_id, name):
    r = _curl("POST", "/iam/v1/serviceAccounts", boot, {"accountId": account_id, "name": name})
    return _await(r, boot, "serviceAccountId")


def grant(sva, role_id, scope_type, scope_id):
    """Выдача: субъект — служебная учётка, роль на области.

    ОТКАЗ ГРОМКИЙ, и это главное в этой функции. Прежняя редакция читала `id`
    из ответа и, не найдя его, **молча возвращалась**: отвергнутая выдача была
    неотличима от созданной. Наблюдалось вживую — роли заведены, селекторы
    записаны верно, привязок в базе **ноль**, а посев отчитался успехом и упал
    через двадцать минут в чужом кейсе, назвав виновником список.

    Молчание здесь дороже обычного: выдача — это ПРЕДМЕТ фикстуры. Не создав
    её, посев готовит субъекта, у которого нет права, и всякое утверждение о
    видимости после этого проверяет не продукт, а собственную поломку.
    """
    # ОБЛАСТЬ НАЗЫВАЕТСЯ ТОЧЕЧНЫМ ИМЕНЕМ. Сервер принимает только `iam.project` /
    # `iam.account` / `iam.cluster` и на голом `project` отвечает
    # `Illegal argument scopeType "project"` — синхронно, до всякой записи.
    #
    # Приводим здесь, а не у вызывающих, по замеру: из четырёх вызовов этого
    # помощника три слали голое имя и один точечное. Три выдачи не создавались
    # НИКОГДА, и это было незаметно, потому что отказ проглатывался (см. выше).
    # Починка у каждого вызывающего снимает симптом до первого нового вызова.
    dotted = scope_type if "." in scope_type else f"iam.{scope_type}"
    rb = _curl("POST", "/iam/v1/accessBindings", boot, {
        "subjectType": "service_account", "subjectId": sva, "roleId": role_id,
        "scopeType": dotted, "scopeId": scope_id, "target": {"allInScope": {}}})
    if not isinstance(rb, dict) or not rb.get("id"):
        raise SystemExit(
            f"[prodseed] выдача ОТВЕРГНУТА: субъект {sva}, роль {role_id}, "
            f"область {dotted}:{scope_id}.\n"
            f"  ответ края: {rb}\n"
            "Посев без выдачи готовит субъекта БЕЗ ПРАВА — дальше идти нельзя: "
            "кейсы утверждали бы видимость, которой нет, а падение назвало бы "
            "виновником список.")
    _poll(rb["id"], boot)


def custom_role(account_id, name, module, resources, verbs):
    """Create (or find) a custom Role in account_id and return its id.

    The nlb suite has cases about CUSTOM roles — "a role granting addTargets lets its
    holder add targets, and nothing else". They need a role that exists. The dev path
    (setup.sh §13d) creates two; this path used to hand those slots a deliberately
    ungranted subject, which turned the positive half of each case into a permanent
    denial. A case that says a subject CAN do something must be given a subject that
    can, or the invariant is not being checked at all.

    Idempotent: an existing role of the same name in the account is reused, so a
    re-seed neither collides on the name nor multiplies roles.
    """
    listed = _curl("GET", f"/iam/v1/roles?accountId={account_id}&pageSize=1000", boot)
    for r in listed.get("roles") or []:
        if r.get("name") == name:
            return r.get("id", "")
    resp = _curl("POST", "/iam/v1/roles", boot, {
        "accountId": account_id, "name": name,
        "description": "newman fixture: narrow custom role",
        "rules": [{"module": module, "resources": list(resources), "verbs": list(verbs)}],
    })
    return _await(resp, boot, "roleId")


def sa_token_with_key(sva):
    """Выдать ключ служебной учётке и обменять его У НАШЕГО издателя → (токен, id ключа).

    ПОЛОСА ОДНА, И ЭТО ФАКТ О ПРОДУКТЕ, А НЕ ВЫБОР ПОСЕВА (задача #1120). Выдача
    ключа на переведённом контуре зеркала у прежнего издателя не заводит, поэтому
    обменять ключ там нельзя ни при каком входе — клиента с таким именем он не
    знает. Прежде полос было две, и посев держал обе.

    КЛИЕНТОМ НАЗЫВАЕТСЯ СТРОКА НАШЕГО РЕЕСТРА. Утверждение называет себя `iss`/`sub`
    именем клиента, а резолвер нашего проверяющего читает СВОИ таблицы по нашему
    идентификатору (`keyId`). Зеркальная колонка на этом пути не участвует вовсе.

    ID КЛЮЧА ВОЗВРАЩАЕТСЯ, потому что ОТЗЫВ — часть контракта, который проверяют
    кейсы: токен, чей ключ снят, обязан перестать приниматься. Снять ключ, id
    которого не сохранили, нечем.

    ОТКАЗ ПОДНИМАЕТСЯ, А НЕ ПРОГЛАТЫВАЕТСЯ. Пустая величина в посеве доехала бы до
    пробы отказом доступа — то есть вердиктом о продукте там, где предмет оснастка
    либо профиль (перечень объявленных адресатов).
    """
    kr = _curl("POST", f"/iam/v1/serviceAccounts/{sva}/keys", boot,
               {"serviceAccountId": sva, "audience": [API_AUD]})
    done = _poll(kr.get("id"), boot)
    if done.get("error"):
        raise RuntimeError(f"SA-key issue errored for {sva}: {done['error']}")
    _, key, registry_client_id = m._extract_oauth(done.get("response", {}))
    assertion = m.sign_client_assertion(
        registry_client_id, key, registry_client_id, PLATFORM_ASSERT_AUD,
        token_type=m.CLIENT_ASSERTION_TOKEN_TYPE)
    return (m.exchange_at_platform(PLATFORM_TOKEN_URL, assertion, API_AUD),
            registry_client_id)


def sa_token(sva):
    """Тот же выпуск, когда id ключа вызывающему не нужен."""
    return sa_token_with_key(sva)[0]


def grant_user(uid, role_id, scope_type, scope_id):
    """Выдача, субъект которой — ЧЕЛОВЕК, а не машина.

    Отдельная функция, а не параметр у `grant`: у той вид субъекта зашит, и все
    её вызывающие — служебные учётки. Область называется точечным именем по той
    же причине, что и там (сервер отвергает голое `project`/`account`).

    Отказ громкий по той же причине, что у `grant`: выдача — предмет фикстуры, и
    молчаливо не созданная готовит субъекта без права.
    """
    dotted = scope_type if "." in scope_type else f"iam.{scope_type}"
    rb = _curl("POST", "/iam/v1/accessBindings", boot, {
        "subjectType": "user", "subjectId": uid, "roleId": role_id,
        "scopeType": dotted, "scopeId": scope_id, "target": {"allInScope": {}}})
    if not isinstance(rb, dict) or not rb.get("id"):
        raise SystemExit(
            f"[prodseed] выдача ОТВЕРГНУТА: субъект user:{uid}, роль {role_id}, "
            f"область {dotted}:{scope_id}.\n  ответ края: {rb}")
    _poll(rb["id"], boot)


def user_platform_token(uid, created_by):
    """Персональный токен пользователя, обменянный у НАШЕГО издателя (#1121).

    Полоса та же, что у `sa_platform_token`; отличие — субъект: человек, а не
    машина. Разбор и проверка «одно имя, а не два» — в godoc
    `m.user_platform_token`.
    """
    return m.user_platform_token(PUBLIC, boot, uid, created_by,
                                 PLATFORM_TOKEN_URL, API_AUD, PLATFORM_ASSERT_AUD)


CLUSTER_ROOT_OBJECT = "cluster:cluster_root"


def seed_fga_tuple(fga_subject, relation, obj):
    """Посеять факт отношения (<fga_subject> #<relation> @obj) через журнал iam.

    Проекция журнала намеренно пропускает ГЛАГОЛЫ (`v_*`, миграция 0100): глагол
    выводится из выдачи и копией не хранится. Здесь сеются отношения УРОВНЯ
    ОБЛАСТИ (`viewer`, `system_viewer`), и их проекция принимает.

    Кластерный случай и его обоснование — в seed_fga_cluster ниже.
    """
    sql = (
        "INSERT INTO kaname.fga_outbox (event_type, payload, created_at) "
        "SELECT 'fga.tuple.write', jsonb_build_object("
        f"'user','{fga_subject}','relation','{relation}','object','{obj}'), now() "
        "WHERE NOT EXISTS (SELECT 1 FROM kaname.fga_outbox "
        f"WHERE payload->>'user'='{fga_subject}' AND payload->>'relation'='{relation}' "
        f"AND payload->>'object'='{obj}');"
    )
    args = ["kubectl", "-n", KACHO_NS, "exec", PG_IAM_POD, "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kaname -h 127.0.0.1 -tAc "{sql}"']
    subprocess.run(args, capture_output=True, text=True)


def seed_fga_cluster(fga_subject, relation):
    """Seed a cluster-scope FGA tuple (<fga_subject> #<relation> @cluster_root)
    deterministically via kaname.fga_outbox → drainer → OpenFGA (idempotent
    WHERE NOT EXISTS), mirroring the sanctioned dev-mode setup.sh 5a/5c seeds.

    Why (cluster-viewer FLOOR, #64/#62): the admin-curated GLOBAL catalog reads —
    compute DiskTypeService.Get/List, geo Region/Zone — gate `viewer@cluster`
    (scope_extractor object_type=cluster). `viewer` derives from `system_viewer` /
    `system_admin` (any_admin), NEVER from an account/project grant. A tenant SA with
    only project/account bindings therefore fails the catalog read with
    "get lacks relation viewer on cluster:cluster_root" — yet EVERY authenticated
    tenant must read the catalog to launch placement-scoped resources (compute
    authz-deny EXPECTs catalog-read = ALLOW for every non-anon subject). Grant each
    matrix SA `system_viewer@cluster` so the floor is satisfied; it grants ONLY the
    global-catalog read floor (no project/account resource access), so DENY matrices
    (project-scope, cross-account, catalog-MUTATE admin-only) are unaffected."""
    seed_fga_tuple(fga_subject, relation, CLUSTER_ROOT_OBJECT)


def subject(account_id, name, grants=()):
    """Create an SA in account_id, apply grants [(role, scope_type, scope_id)], mint token."""
    sva = make_sa(account_id, name)
    for role_id, st, sid in grants:
        grant(sva, role_id, st, sid)
    # cluster-viewer FLOOR: every matrix SA must satisfy `viewer@cluster` for the
    # global-catalog reads (DiskType/geo). system_viewer → viewer via any_admin cascade.
    seed_fga_cluster(f"service_account:{sva}", "system_viewer")
    return sva, sa_token(sva)


# ── org structure ───────────────────────────────────────────────────────────
boot = m.mint_bootstrap()
# Шаг, создающий ПРЕДМЕТ всего посева, несёт собственное утверждение: край
# ПРИНЯЛ бутстрап-удостоверение. Без него отказ края проявляется через два-три
# шага и обвиняет невиновного — то, что честно сделало своё дело при
# отсутствующем предмете (задача #1119).
m.assert_bootstrap_accepted_by_the_edge(PUBLIC, boot)


def _seed_bootstrap_root_cluster():
    """Deterministic system_admin + system_viewer @cluster for the bootstrap ROOT
    user (KANAME_BOOTSTRAP_ROOT_EMAIL, default admin@prorobotech.ru), mirroring
    dev-mode setup.sh 5a/5c. The bootstrap SA principal already holds system_admin
    @cluster via migration 0058 (deterministic), but the root USER's grant is
    seeded by the ≤180s RunBootstrapAdmin reconciler (racy on a fresh stand) and it
    never gets system_viewer. Best-effort: skip silently if the user is not yet
    provisioned (never fails the seed run)."""
    email = "admin@prorobotech.ru"
    sql = f"SELECT id FROM kaname.users WHERE external_id='{email}' LIMIT 1;"
    args = ["kubectl", "-n", KACHO_NS, "exec", PG_IAM_POD, "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kaname -h 127.0.0.1 -tAc "{sql}"']
    out = subprocess.run(args, capture_output=True, text=True).stdout.strip()
    uid = next((ln.strip() for ln in out.splitlines() if ln.strip().startswith("usr")), "")
    if not uid:
        return
    for rel in ("system_admin", "system_viewer"):
        seed_fga_cluster(f"user:{uid}", rel)


def _seed_geo_catalog():
    """Seed the admin-curated geo baseline via the geo Internal admin RPC (:18081, gated
    system_admin@cluster = jwtBootstrap): region ru-central1 with one zone per
    ZONE_SUFFIXES (default a,b,c,d,e), plus ALT_REGION with a single zone. Zone count is
    NOT written out here -- it is whatever ZONE_SUFFIXES says, and an earlier revision of
    this docstring named four zones while the tuple listed five.

    Greenfield/fresh stands have NO geo baseline: geo goose-migrations create the schema
    but seed no rows -> every compute/nlb/vpc create fails "Zone/Region not found"
    (peer-validate geo.{Zone,Region}Service.Get, fail-closed) -> all downstream Get/List
    404. The compute->geo data-migration Helm-job does NOT cover this and is not a no-op
    either: compute dropped its Region/Zone tables, and a dropped table answers 42P01, so
    the Job would fail -- which is why it is disabled in every shipped profile.

    A STAND raised by `make dev-up` now seeds the same baseline itself
    (deploy/scripts/geo-baseline.sql, target `make seed-geo`), so on such a stand this
    function is a confirmed no-op; it stays because a newman run must not DEPEND on how
    the stand was raised. Idempotent: re-create of an existing row -> AlreadyExists,
    tolerated; the confirm-loops pass immediately when already seeded. `existingZoneId`
    etc. in the fixtures dict below already name these ids."""
    _curl("POST", "/geo/v1/internal/regions", boot,
          {"id": "ru-central1", "status": "UP"}, base=INTERNAL)
    for _ in range(20):  # region durable before zones (zones.region_id FK RESTRICT -> regions.id)
        if _curl("GET", "/geo/v1/regions/ru-central1", boot).get("id") == "ru-central1":
            break
        time.sleep(0.5)
    for z in ZONE_SUFFIXES:
        _curl("POST", "/geo/v1/internal/zones", boot,
              {"id": f"ru-central1-{z}", "regionId": "ru-central1",
               "status": "UP"}, base=INTERNAL)
    for _ in range(30):  # zones durable (peer-validate consumers read them on request-path)
        if len((_curl("GET", "/geo/v1/zones", boot).get("zones") or [])) >= len(ZONE_SUFFIXES):
            break
        time.sleep(0.5)

    # A SECOND region, because "the other region" has to BE another region.
    #
    # Every negative case about placement coherence needs a region that is not the
    # primary one, and `existingRegionAltId` is the handle they all reach for. It used
    # to hold `ru-central1` — the primary — so the "cross-region" fixtures were built
    # in the SAME region and the refusals they assert could never fire. On the
    # production-posture run of 2026-07-30 that showed up as a listener accepting a
    # cross-region repoint; the repoint was same-region and lawfully accepted, while
    # the check that would have caught a real one had no producer for its own input.
    #
    # One zone is seeded with it: a region with no zone cannot host a zonal fixture,
    # and a case that needs one would then fail for the wrong reason. No address pool
    # is seeded here — nothing allocates a VIP in this region, and a pool that nobody
    # draws from is exactly the kind of fixture that outlives its subject.
    _curl("POST", "/geo/v1/internal/regions", boot,
          {"id": ALT_REGION, "status": "UP"}, base=INTERNAL)
    for _ in range(20):
        if _curl("GET", f"/geo/v1/regions/{ALT_REGION}", boot).get("id") == ALT_REGION:
            break
        time.sleep(0.5)
    _curl("POST", "/geo/v1/internal/zones", boot,
          {"id": f"{ALT_REGION}-a", "regionId": ALT_REGION,
           "status": "UP"}, base=INTERNAL)
    for _ in range(20):
        if _curl("GET", f"/geo/v1/zones/{ALT_REGION}-a", boot).get("id") == f"{ALT_REGION}-a":
            break
        time.sleep(0.5)


def _seed_address_pool(zone_id, name, v4, v6=()):
    """Seed a default AddressPool (IPAM source for external VIPs: nlb external LB VIP
    alloc + vpc Address EXTERNAL create). Greenfield stands have no pool -> vpc
    address-EXTERNAL / nlb external creates fail (no default pool for zone). Cluster-level
    admin resource (Internal admin :18081, system_admin@cluster = jwtBootstrap; Create
    returns the pool sync, not an Operation). Idempotent: AlreadyExists / CIDR-overlap on
    re-seed -> reuse the existing named pool. Requires the geo zone seeded first (pool
    .zone_id references the geo catalog). Newman prerequisite, not a migration -- same
    layer as _seed_geo_catalog."""
    r = _curl("POST", "/vpc/v1/addressPools", boot,
              {"name": name, "description": "seed external VIP pool",
               "kind": "EXTERNAL_PUBLIC", "zoneId": zone_id,
               "v4CidrBlocks": list(v4), "v6CidrBlocks": list(v6)}, base=INTERNAL)
    pid = r.get("id", "")
    if not pid:  # AlreadyExists / CIDR-overlap -> reuse the existing named pool
        lst = _curl("GET", "/vpc/v1/addressPools?pageSize=200", boot, base=INTERNAL)
        pid = next((p.get("id", "") for p in (lst.get("pools") or [])
                    if p.get("name") == name), "")
    if pid:  # make it the default IPAM source for its (zone, kind)
        _curl("PATCH", f"/vpc/v1/addressPools/{pid}", boot,
              {"updateMask": "isDefault", "isDefault": True}, base=INTERNAL)
    return pid


def _seed_network(project_id, name):
    """A pre-existing vpc Network in `project_id` (seedNetworkA1Id / seedNetworkB1Id —
    the GET-probe targets of the authz-deny matrix). Best-effort: a failure leaves the
    key empty and prodseed_all drops empty keys rather than blanking a committed one.

    The address plan is declared because subnets ARE carved in these two networks by
    the vpc authz-deny suite: a network that declares no supernet refuses subnets of
    that family outright (sync 400 — there is nothing to carve from). A planless seed
    would be more permissive than the product and would take down every probe standing
    on a subnet, with the failure surfacing far from its cause.
    """
    try:
        return _await(_curl("POST", "/vpc/v1/networks", boot,
                            {"projectId": project_id, "name": name,
                             "ipv4CidrBlocks": ["10.0.0.0/8"],
                             "ipv6CidrBlocks": ["fd00::/8"]}), boot, "networkId")
    except RuntimeError:
        return ""


def seed() -> dict:
    """Provision the production-mode matrix and return the fixtures dict.

    Every authenticating token in the result is a Hydra-signed RS256 ServiceAccount
    Bearer obtained through the iam facade — MintBootstrapToken (mTLS gRPC, iam :9091)
    for the admin, then SAKeyService.Issue + a private_key_jwt client_assertion
    exchange for each subject. Nothing here is minted by the harness.
    """
    _seed_bootstrap_root_cluster()
    _seed_geo_catalog()
    # v4-only default EXTERNAL_PUBLIC pool for zone `a` (the vpc suite's IPAM source).
    # DELIBERATELY NOT named `kac-nlb-seed-ext-pool`: deploy/scripts/seed-nlb-fixtures.sh
    # declares itself the SOLE author of that name and RECLAIMS (un-defaults, then
    # deletes) any pool carrying it that sits outside the nlb-dedicated zone — so the
    # old name made the two seeders fight over one row. v6 stays empty in zone `a`: the
    # vpc case ADR-CR-EXT-V6-FAMILY-FALLTHROUGH asserts a v6 request there finds no
    # v6-capable pool and fails FailedPrecondition.
    _seed_address_pool("ru-central1-a", "kac-seed-ext-pool-a", ["198.51.100.0/24"])

    owner_a = f"prodseed-owner-a-{RID}@example.com"
    owner_b = f"prodseed-owner-b-{RID}@example.com"
    usr_owner_a = upsert_user(owner_a)
    usr_owner_b = upsert_user(owner_b)
    acctA, projA1 = db_lookup(owner_a)
    acctB, projB1 = db_lookup(owner_b)

    # AccessBinding-subject users (userNOBId/userINVId/userAAAId/userAABId/userPA1Id).
    # The iam newman cases reference these as subjectId when creating AccessBindings and
    # as ownerUserId / reviewerUserId. Migration 0049 (access_binding_subject_exists)
    # rejects a Create whose subject User does not exist → the stale hardcoded env values
    # (usr… baked into local.postman_environment.json by the dev-mode setup.sh) do NOT
    # exist in a fresh production-mode iam DB, so Create fails FAILED_PRECONDITION
    # ("referenced resource not found") and every downstream Get/Delete/revoke cascades
    # (404/403). Seed REAL users here and emit their ids so prod-mode binding cases resolve
    # a live subject. userAAAId/userAABId map to the owner users (accountAId is owned by
    # userAAAId — iam-account.py ownerUserId assertions); NOB/INV/PA1 are plain users.
    usr_nob = upsert_user(f"prodseed-nob-{RID}@example.com")
    usr_inv = upsert_user(f"prodseed-inv-{RID}@example.com")
    usr_pa1 = upsert_user(f"prodseed-pa1-{RID}@example.com")
    # kacho-iam#276 — the NEVER-granted leak-guard subject. `userNOBId` is used DOUBLY
    # (grant TARGET in the access-binding suites, leak-guard VICTIM in the see-nothing
    # probes), so the guards read this dedicated user/SA instead. Seeded as a real user
    # (the id is asserted) whose token slot is the no-grant SA below.
    usr_pure_nob = upsert_user(f"prodseed-pure-nob-{RID}@example.com")
    projA2 = _await(_curl("POST", "/iam/v1/projects", boot,
                          {"accountId": acctA, "name": f"prodseed-a2-{RID}"}), boot, "projectId")

    P = "iam.project"
    A = "iam.account"

    # ── subjects (all SA-principals) ────────────────────────────────────────────
    sva_editorA, tok_editorA = subject(acctA, f"ps-ed-a-{RID}",
                                       [(ROLE_EDIT, P, projA1), (ROLE_EDIT, P, projA2)])
    # jwtSAA / svaAId is a SEPARATE service account, editor on projA1 ONLY.
    # Reusing the A1+A2 editor above would silently OVER-GRANT it: the iam suite
    # documents this slot as "service-account-A (vpc-editor on project-A1)" and asserts
    # AUTHZ-SA-NET-LS-A2-DENY — that the same SA is DENIED listing in the cross project.
    # With an A2 grant that DENY turns 200 and the case stops testing anything.
    # svaAId must stay the id OF this token's principal: rbac-subject-channel-equivalence
    # binds roles to `svaAId` and then reads as `jwtSAA`.
    sva_saA, tok_saA = subject(acctA, f"ps-sa-a-{RID}", [(ROLE_EDIT, P, projA1)])
    sva_nogrant, tok_nogrant = subject(acctA, f"ps-nogrant-{RID}")
    # A SECOND, distinct never-granted SA for the leak-guards: sharing one no-grant token
    # between "subject under test" and "leak-guard victim" is exactly the doubly-used
    # subject #276 removed on the dev path.
    sva_purenob, tok_purenob = subject(acctA, f"ps-purenob-{RID}")
    _, tok_viewerA = subject(acctA, f"ps-view-a-{RID}", [(ROLE_VIEW, P, projA1)])
    # Custom-role subjects. Their cases assert what a NARROW role does and does not
    # allow — "may addTargets, may not update the group's metadata" — which only means
    # something when the role exists and is bound. Mirrors the dev path (setup.sh §13d).
    # Глаголы роли берутся из СЛОВАРЯ МОДЕЛИ, а не из имён RPC — и словарь у
    # `nlb_target_group` теперь ШИРЕ канонического CRUD (NLB-TGT-1): тип объявляет
    # `addTargets`/`removeTargets` собственными отношениями, надмножествами `update`.
    #
    # Здесь временно стоял `["update"]`, и рядом было записано, что различение
    # «может добавлять цели, но не может править саму группу» НЕВЫРАЗИМО, поскольку
    # оба глагола суть одно отношение. Это было верно тогда и неверно теперь: ровно
    # это утверждение и было предметом под-фазы. Обход снят, а не переименован —
    # роль снова грантит то, по чему названа, и её положительная половина проверяет
    # право, ради которого роль существует.
    #
    # Операторской роли здесь БОЛЬШЕ НЕТ, и это решение, а не переименование.
    # Она строилась из `networkLoadBalancers.{start,stop}` — глаголов, снятых
    # миграцией 0059, когда административное включение/выключение переехало в поле
    # `NetworkLoadBalancer.admin_state`. Выразить её пост-0059 можно было бы только
    # через `update`, а роль с одним лишь `update` на группах целей теперь есть ниже
    # и служит парным контролем, а не дублем. Прежнюю роль не использовал ни один
    # шаг — её предъявитель упоминался только в перечне субъектов.
    role_tm = custom_role(acctA, f"ps_nlb_targetmgr_{RID}", "loadbalancer",
                          ["targetGroups"], ["addTargets", "removeTargets"])
    _, tok_crTargetMgr = subject(acctA, f"ps-cr-tm-{RID}",
                                 [(role_tm, P, projA1)] if role_tm else [])
    # Роль, называющая РОВНО `create` и `list` на storage.volumes — предъявитель, на
    # котором видно, что выдача не шире названного. Ни одна ярусная роль этого
    # различия не делает: `edit` несёт get/list/update (+delete), `view` — get/list,
    # и ни одна не позволяет спросить «перечислять можно, читать и менять — нет».
    #
    # Почему в роли ДВА глагола, а не один. Реконсайлер материализует на каждый
    # объект в области привязки те названные глаголы, которые ТИП объявляет, И ярусный
    # кортеж, выведенный из КЛАССА глаголов (`create` — запись ⇒ `editor`). Из этой
    # пары `storage_volume` объявляет только `list`: пообъектного `v_create` у него
    # нет — создание тома авторизуется ярусом записи на проекте, а не глаголом на
    # самом томе, которого в момент решения ещё не существует. `v_list`
    # нужен как ПОЛОЖИТЕЛЬНАЯ половина: он гейтит object-self `ListOperations`,
    # поэтому по нему видно, что материализация до этого объекта дошла. Без него
    # «отказано на Get/Update/Delete» было бы неотличимо от «привязка не доехала
    # вовсе» — отрицание зеленело бы на сломанной фикстуре.
    # `create` здесь — ЗАЯВЛЕННОЕ исключение, а не недосмотр: гейт
    # deploy/scripts/assert-fixture-role-verbs-exist.py требует, чтобы каждый глагол
    # роли объявлялся типом, и этот — не объявляется намеренно. Запись с причиной
    # лежит в его таблице VERB_FOR_TIER_ONLY и истекает сама: уберут глагол отсюда —
    # гейт покраснеет НА ЗАПИСИ, которой больше нечего извинять.
    role_storage_creator = custom_role(acctA, f"ps_storage_crlist_{RID}", "storage",
                                       ["volumes"], ["create", "list"])
    sva_storcr, tok_storageCreatorA = subject(acctA, f"ps-stor-cl-{RID}",
                                              [(role_storage_creator, P, projA1)] if role_storage_creator else [])
    # ПАРНЫЙ КОНТРОЛЬ к предыдущей роли: только `update` на тех же группах целей.
    #
    # Без него отказ держателю управления составом в правке группы неотличим от
    # «этот субъект вообще ничего не может», а разрешение держателю `update`
    # управлять составом (ветвь `or v_update` модели) вообще некому предъявить.
    # Штатная роль редактора этого Given не строит: она объявляет правило `*.*`,
    # то есть в матрице неразличима от администратора по этому вопросу.
    role_tgupd = custom_role(acctA, f"ps_nlb_tgupdater_{RID}", "loadbalancer",
                             ["targetGroups"], ["update"])
    _, tok_crTgUpdater = subject(acctA, f"ps-cr-tgupd-{RID}",
                                 [(role_tgupd, P, projA1)] if role_tgupd else [])
    _, tok_adminA = subject(acctA, f"ps-adm-a-{RID}", [(ROLE_ADMIN, A, acctA)])
    _, tok_adminB = subject(acctB, f"ps-adm-b-{RID}", [(ROLE_ADMIN, A, acctB)])
    # The id is KEPT, not discarded. A suite that binds a role to a subject and then
    # reads with a token needs the id OF THAT TOKEN'S PRINCIPAL; an underscore here
    # published a token whose principal nothing named, and the nearest-looking id in the
    # env (`userINVId`, an unrelated user row) got bound instead. The tuple then names a
    # user while every request authenticates as a service account, so the relation cannot
    # resolve — ever. See PRINCIPAL_PAIRINGS below, which now asserts the property the
    # comment on `svaAId` had been stating for one channel only.
    sva_invitee, tok_invitee = subject(acctA, f"ps-inv-{RID}", [(ROLE_ADMIN, A, acctB), (ROLE_EDIT, P, projA1)])
    # editor on cross project A2 ONLY (nlb cross-tenant move tier)
    _, tok_editorCrossA2 = subject(acctA, f"ps-ed-a2-{RID}", [(ROLE_EDIT, P, projA2)])
    _, tok_ownerA = subject(acctA, f"ps-own-a-{RID}", [(ROLE_ADMIN, P, projA1)])

    seed_net_a1 = _seed_network(projA1, f"ps-seed-net-a1-{RID}")
    seed_net_b1 = _seed_network(projB1, f"ps-seed-net-b1-{RID}")

    # ── статические API-токены ───────────────────────────────────────────────
    #
    # ЧТО ЭТО НА САМОМ ДЕЛЕ. В отладочной посадке эти четыре предъявителя ковались
    # харнессом (HS256 своим секретом, в том числе один — «просроченный» и один — с
    # заведомо чужой подписью). В боевой посадке ковать их нечем и не нужно: кейс сам
    # объявляет, что за ними стоит `SAKeyService` — выпуск ключа служебной учётки и его
    # ОТЗЫВ. То есть три из четырёх выражаются штатным продуктовым путём, и здесь они им
    # и выражаются, а не подделкой.
    #
    # ПОЧЕМУ ЭТО НЕ ОСЛАБЛЕНИЕ. Прежняя запись посева объявляла всю четвёрку
    # «требующей настоящего интерактивного входа» и не ковала НИ ОДНОГО — вместе со
    # `jwtAccountAdminAStepUp`, которому интерактивный вход действительно нужен. Это
    # было верно про повышенный вход и неверно про ключи служебной учётки: они машинные
    # по природе. Из-за одной формулировки девять утверждений не исполнялись ни разу.
    #
    # ЧТО ОСТАЁТСЯ ДОЛГОМ И ПОЧЕМУ ИМЕННО ОНО. `apiTokenExpired` требует предъявителя,
    # чей срок УЖЕ истёк. Подписывает токены провайдер, срок жизни задаёт он же, и
    # укоротить его на один выпуск нельзя. Подделать — значит вернуться ровно к тому, что
    # здесь убирается. Значит этому кейсу нужна своя волна, создающая условие (выпустить и
    # переждать срок) — она ЕСТЬ: `services/iam/tests/newman/scripts/run-expired-bearer.sh`.
    # В общий параллельный прогон она не входит: идёт столько, сколько живёт предъявитель.
    #
    # СРОК ТЕПЕРЬ ЗАДАЁМ МЫ, И ЭТО СМЕНИЛО ВЕЛИЧИНУ (задача #1120). Здесь стояло
    # 14400 с (4 ч) — умолчание прежнего издателя, замеренное 2026-08-04. Ключ
    # служебной учётки больше не обменивается у него вовсе, а наш выпуск берёт срок
    # из объявления посадки (`authn.client-token.token-ttl`, 15m в профилях dev-prod
    # и prod) и УКОРАЧИВАЕТ его до остатка жизни самого ключа. Тому, кто ставит волну
    # в расписание, эта величина и есть смысл записи; перемерять её надо по профилю
    # стенда, а не по этой строке.
    #
    # СЛЕДСТВИЕ, КОТОРОЕ ВАЖНЕЕ ЧИСЛА: `IssueSAKeyRequest.ttl_seconds` теперь СРЕЗАЕТ
    # и срок токена — наш выпуск не выдаёт токен, переживающий свой ключ. Прежде это
    # поле ограничивало только сам ключ.
    #
    # ОТЗЫВ ПРОВЕРЯЕТСЯ ПО-НАСТОЯЩЕМУ. `apiTokenRevoked` — это выпущенный токен, чей
    # ключ затем снят `SAKeyService.Revoke`. Если край такой предъявитель всё ещё
    # принимает, кейс покраснеет — и это будет находка о ПРОДУКТЕ, а не о посеве. Именно
    # поэтому отдельная служебная учётка: снятие ключа не должно задеть ни `jwtSAA`, ни
    # действующий `apiTokenValid`.
    tok_apivalid, _ = sa_token_with_key(sva_saA)
    sva_apirev, _ = subject(acctA, f"ps-apitok-rev-{RID}", [(ROLE_EDIT, P, projA1)])
    tok_apirev, key_apirev = sa_token_with_key(sva_apirev)
    _curl("DELETE", f"/iam/v1/serviceAccounts/{sva_apirev}/keys/{key_apirev}", boot)

    # ПРЕДЪЯВИТЕЛЬ НАШЕЙ ЧЕКАНКИ ДЛЯ СУИТЫ КРАЯ (#1014).
    #
    # Слот остаётся отдельным, хотя полоса выдачи ключа теперь одна: суита края
    # сравнивает издателя ЭТОГО предъявителя с издателем соседней полосы
    # (`jwtBootstrap`, чеканит прежний издатель) и требует, чтобы они разошлись.
    # Держать сравнение на слоте, который кто-то однажды переиспользует под другой
    # предмет, значило бы менять смысл кейса чужой правкой.
    #
    # Учётка та же, что у `jwtSAA`, а ключ выдаётся СВОЙ (ключи аддитивны), поэтому
    # снятие ключа у `apiTokenRevoked` — другая учётка — этого предъявителя не
    # задевает.
    tok_platform = sa_token(sva_saA)

    # ТА ЖЕ полоса, ДРУГОЙ субъект — человек (#1121). Держатся обе, потому что
    # край выводит принципала из утверждений токена, а у человека и машины они
    # разные (`kaname_principal_type`): полоса, доказанная машиной, о человеке не
    # говорит ничего.
    #
    # Субъект СВОЙ, а не один из матричных: выпуск персонального токена — это
    # мутация над `iam_user`, и вешать её на пользователя, чью видимость
    # утверждают чужие кейсы, значило бы менять их фикстуру ради этой.
    usr_utok = upsert_user(f"prodseed-utok-{RID}@example.com")
    acct_utok, _ = db_lookup(f"prodseed-utok-{RID}@example.com")
    grant_user(usr_utok, ROLE_ADMIN, A, acct_utok)
    # Тот же пол каталога, что у матричных субъектов: без `viewer@cluster`
    # глобальный справочник читать нельзя, и 200 на маршруте края ничего бы не
    # сказал о полосе.
    seed_fga_cluster(f"user:{usr_utok}", "system_viewer")
    tok_user_platform = user_platform_token(usr_utok, usr_utok)

    fixtures = {
        "jwtBootstrap": boot,
        # `iss` — наш издатель, подпись ES256, ключ из нашего же реестра. Слот
        # читает суита края (gateway/tests/newman).
        "jwtPlatformIssuer": tok_platform,
        # Предъявитель второй полосы, чей принципал — ЧЕЛОВЕК: персональный токен,
        # обменянный у нашего издателя. Слот читает суита края.
        "jwtUserTokenPlatformIssuer": tok_user_platform,
        "userTokenPlatformUserId": usr_utok,
        # no-grant slots
        "jwtNoBindings": tok_nogrant,
        "jwtSANoGrant": tok_nogrant,
        "jwtStranger": tok_nogrant,
        # dedicated never-granted leak-guard subject (#276)
        "jwtPureNoBindings": tok_purenob,
        # editor @ A1 (+A2) slots
        "jwtProjectAdminA1": tok_editorA,
        "jwtProjectEditorA": tok_editorA,
        "jwtSAA": tok_saA,
        "jwtServiceAccountEditor": tok_saA,
        # viewer @ A1
        "jwtProjectViewerA": tok_viewerA,
        # account-admin A / B
        "jwtAccountAdminA": tok_adminA,
        "jwtAccountAdminB": tok_adminB,
        # Повышенный вход (`required_acr_min=2`, RFC 9470) — тот же предъявитель
        # account-admin, и это НЕ подделка. Правило повышения одно на платформу
        # (`grpcsrv.EvaluateStepUp`, к нему приходят ОБА энфорсера — StepUpGate края и
        # внутренний acr-floor iam), и его ПЕРВАЯ ветка снимает порог интерактивности с
        # машинного принципала целиком: `PrincipalType == "service_account"` → allow, до
        # всякого сравнения acr. Предъявителю здесь не нужен acr=2 — ему нужно БЫТЬ
        # служебной учёткой, а он ею и является (client_credentials + claim от
        # token-hook). Проверено вызовом, а не прочтением: `SAKeyService.Issue` — та самая
        # ручка с порогом 2 — отвечает 200 на этот токен.
        #
        # ЧТО ЗДЕСЬ БЫЛО. Слот стоял пустым, потому что запись посева объявляла его
        # «требующим настоящего интерактивного входа, не куемым никаким машинным путём».
        # Это фольклор: соседний абзац выше уже снимал ровно такую формулировку с трёх
        # ключей служебной учётки, но четвёртый оставил — предположение про acr никто не
        # перепроверил по коду правила. Ценой был не только сам слот: пустой предъявитель
        # даёт 401, мутация не возвращает Operation, и дальше сыплется каскад захватов.
        #
        # ПОЧЕМУ ЭТО НЕ ОСЛАБЛЕНИЕ. Ни один кейс не проверяет САМ порог повышения — это
        # объявленный долг с числом (1 инвариант, 2 ручки; см. врезку «NOT COVERED HERE»
        # в cases/iam-service-account.py), а объявление порога держит Go-гейт над
        # каталогом прав. Во всех кейсах, читающих этот слот, повышенный вход — СРЕДСТВО
        # ДОСТУПА к ручке, а не предмет утверждения (редактирование секрета ключа,
        # disable/enable учётки, block/unblock пользователя). Появится волна, умеющая
        # предъявить человеческую сессию с acr и без — порог получит собственную пробу, и
        # ей понадобится СВОЙ слот, а не этот.
        "jwtAccountAdminAStepUp": tok_adminA,
        # invitee (admin@acctB + editor@projA1)
        "jwtInvitee": tok_invitee,
        # editor @ B / cross project A2
        "jwtProjectEditorB": tok_editorCrossA2,
        # project-owner (admin) @ A1
        "jwtProjectOwnerA": tok_ownerA,
        # group-member behaves as editor@A1 (the group cascade itself is exercised by the
        # iam suite; here the slot only needs a subject that can create and clean up).
        "jwtGroupMemberEditor": tok_editorA,
        # Custom-role slots are GRANTED now, each through its own narrow role. They used
        # to carry the never-granted token, justified as "their denial asserts hold" —
        # true of the negative halves and fatal to the positive ones: the case saying
        # "targetManager may addTargets" could only ever see 403, so the verb its role
        # exists to grant was never once exercised.
        "jwtCustomRoleTargetManager": tok_crTargetMgr,
        # storage.volumes {create,list} @ A1 и БОЛЬШЕ НИЧЕГО — предъявитель разреза
        # «глагол, а не ярус» (cases/authz.py, AUTHZ-VOL-VERB-*)
        "jwtStorageCreateListOnlyA": tok_storageCreatorA,
        # Держатель ТОЛЬКО `update` на группах целей — парный контроль к предыдущему
        # (NLB-TGT-1): он управляет составом по ветви `or v_update` модели И правит
        # саму группу; держатель управления составом — только первое.
        "jwtCustomRoleTgUpdater": tok_crTgUpdater,
        # ids
        "accountAId": acctA,
        "accountBId": acctB,
        "existingProjectId": projA1,
        "projectA1Id": projA1,
        "existingProjectCrossId": projA2,
        "projectA2Id": projA2,
        "projectB1Id": projB1,
        "existingAccountId": acctA,
        "svaAId": sva_saA,
        "svaNoGrantId": sva_nogrant,
        "svaPureNoGrantId": sva_purenob,
        "svaStorageCreateListOnlyId": sva_storcr,
        # Принципал, стоящий за `jwtInvitee`. Публикуется затем, чтобы кейс, который
        # ЧИТАЕТ этим токеном, имел чем связать привязку. См. PRINCIPAL_PAIRINGS.
        "svaInviteeId": sva_invitee,
        # AccessBinding-subject / ownerUserId users (must EXIST — migration 0049).
        #
        # ЭТИ ИДЕНТИФИКАТОРЫ — ТОЛЬКО ЦЕЛИ ПРИВЯЗКИ, НЕ ПРИНЦИПАЛЫ. Ни одному из них
        # посев не выдаёт токена и выдать не может: машинный харнесс получает только
        # `client_credentials`, то есть служебную учётку (см. шапку mint_rs256 —
        # пользовательский токен требует интерактивного входа и без него не несёт acr).
        # Связывать любой из них с каким-либо `jwt*` — значит записать отношение на
        # субъект, которым ни один запрос не аутентифицируется.
        "userAAAId": usr_owner_a,
        "userAABId": usr_owner_b,
        "userNOBId": usr_nob,
        "userINVId": usr_inv,
        "userPA1Id": usr_pa1,
        "userPureNoBindingsId": usr_pure_nob,
        # pre-existing GET-probe networks of the authz-deny matrix
        "seedNetworkA1Id": seed_net_a1,
        "seedNetworkB1Id": seed_net_b1,
        # zones / regions (admin-curated geo catalog)
        "existingZoneId": "ru-central1-a",
        "existingZoneAltId": "ru-central1-b",
        "zoneA": "ru-central1-a",
        "zoneB": "ru-central1-b",
        "zoneC": "ru-central1-c",
        "zoneD": "ru-central1-d",
        "existingRegionId": "ru-central1",
        "existingRegionAltId": ALT_REGION,
        # статические API-токены (см. блок выше). `apiTokenExpired` НЕ выдаётся
        # намеренно — его условие посевом не создаётся, и подделка вернула бы ровно ту
        # ложь, ради ухода от которой боевая посадка и заводилась.
        "apiTokenValid": tok_apivalid,
        "apiTokenRevoked": tok_apirev,
        # Синтаксически битый JWS — два сегмента вместо трёх. Единственный из четвёрки,
        # который посадкой не определяется вовсе: «это не токен» верно везде.
        "apiTokenMalformed": "eyJhbGciOiJIUzI1NiJ9.bm90LWEtcmVhbC10b2tlbg",
        "baseUrl": PUBLIC,
        "internalBaseUrl": INTERNAL,
    }
    broken = unpaired_principals(fixtures)
    if broken:
        raise RuntimeError(
            "seed contract breach — declared principal pairings do not hold:\n  "
            + "\n  ".join(broken)
            + "\nA case that binds a role to <id> and then reads with <token> can never "
              "resolve if the token authenticates as a different subject: the relation "
              "names one principal and every request carries another. Fix the seed, not "
              "the case — the case has no way to see this and fails as a materialisation "
              "timeout six steps later."
        )
    return fixtures


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--deps", default="", help="comma list: vpc,compute,storage,registry,nlb")
    ap.parse_args()
    print(json.dumps(seed()))
