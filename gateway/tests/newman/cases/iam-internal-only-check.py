# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set iam-internal-only-check — Internal*-службы недостижимы на внешнем слушателе края.

Перенесён из набора службы доступа (PRO-Robotech/kaname, коллекция того же имени;
держатель переноса — #2912, сторона службы — PRO-Robotech/kaname#415). Там кейсы
не гонял ни один шаг конвейера: предмет — маршрутная таблица ВНЕШНЕГО слушателя
края (:8443), и у автономного стенда службы этого слушателя нет. Здесь их гоняет
шаг `гейт — newman зелёный (api-gateway)` (`.github/workflows/e2e-newman.yml`).
Утверждения перенесены без изменения строгости; сменилась только адресация
слушателя — объявлением шага (`mux`), а не переписыванием адреса.

  InternalIAMService / InternalUserService / InternalInteractiveClientService /
  InternalSessionRevocationsService
  должны быть доступны ТОЛЬКО на cluster-internal listener — на api-gateway это
  выделенный `internal-rest` listener (:8081), {{internalBaseUrl}} в прогонщике.
  ПУБЛИЧНЫЙ cmux ({{baseUrl}}) НЕ отдаёт /iam/v1/internal/* — 404 by design
  (ban #6). Те же пути должны быть недостижимы и на advertised external TLS
  listener ({{externalBaseUrl}}) — см. разбор ниже: часть путей доказывает это
  mux-промахом 404, часть — только fail-closed отказом, и кейс различает их.

Coverage:
  IAM-INT-NEG-EXT-REST-ALIVE           — CONTROL: external listener отдаёт REST (200 на публичном пути);
                                         без него любой 404 ниже ничего не значит
  IAM-INT-NEG-EXT-IAM-LOOKUPSUBJECT    — InternalIAMService.LookupSubject → 404 mux-miss на external
  IAM-INT-NEG-EXT-IAM-CHECK            — InternalIAMService.Check → 404 mux-miss на external
  IAM-INT-NEG-EXT-UNBOUND-NEVER-SUCCEEDS
                                       — SessionRevocations.{Revoke,IsRevoked,ListByUser} /
                                         ForceLogout / InternalUserService.Get: НИКОГДА не 2xx на
                                         external + пин к absent-path контролю. НЕ доказывает
                                         route-изоляцию (см. «TWO FAMILIES» ниже) и прямо это заявляет
  IAM-INT-OK-INT-IAM-LOOKUPSUBJECT     — LookupSubject человека посева по его externalId → РОВНО 200
                                         на internal и тот же id (positive control)
  IAM-INT-OK-INT-IAM-LOOKUPSUBJECT-UNKNOWN
                                       — неизвестный external_id → 404 ВЛАДЕЛЬЦА: текст называет
                                         запрошенный ключ. Код 5 различителем НЕ является — его
                                         несёт и промах mux
  IAM-INT-OK-INT-IAM-CHECK             — InternalIAMService.Check → 200 на internal
  IAM-INT-NEG-EXT-IC-LIST              — InternalInteractiveClientService.List → 404 mux-miss на external
  IAM-INT-NEG-EXT-IC-CREATE            — InternalInteractiveClientService.Create → 404 mux-miss на external
  IAM-INT-OK-INT-IC-LIST               — тот же путь на internal → 200 со списком (positive control)

Перечень выше сверяет с объявленным в модуле гейт дерева
`internal/repohygiene/casecoverageblock_test.go`
(`TestCaseCoverageBlockMatchesWhatTheModuleDeclares`).

ЧЕГО ИЗ КОЛЛЕКЦИИ СЛУЖБЫ ЗДЕСЬ НЕТ — ТРИ КЕЙСА, И У КАЖДОГО СВОЙ ДОВОД. Коллекция
службы несла ещё отрицание и два положительных контроля на ГЛАГОЛЕ ЗАВЕДЕНИЯ
ЧЕЛОВЕКА ХУКОМ ПОСТАВЩИКА (`InternalUserService.UpsertFromIdentity`). Решение
владельца 2026-09-27 (приёмка F6b, F6b-52; #2901): людей наборов платформы заводит
только посев и только регистрацией, а адрес хука в дереве проб платформы не
стоит ни в какой форме — это держит гейт
`internal/repohygiene/suitepeopleenrollment_test.go`. Поэтому:
  * положительный контроль и его повтор заводили человека хуком — вид, которого у
    продукта нет; роль положительного контроля семейства BOUND несут
    LOOKUPSUBJECT, CHECK и IC-LIST ниже;
  * отрицание на внешнем слушателе для адреса хука держит белым ящиком перепись
    края `gateway/internal/restmux/external_isolation_test.go` (пара `POST` этого
    адреса стоит в ней); чёрным ящиком изоляцию семейства BOUND доказывают три
    оставшихся адреса и IC-LIST/IC-CREATE.
Положительный контроль LookupSubject переутверждён на человеке ПОСЕВА: его
`externalId` читается у края, а не создаётся хуком.

ЗАМЕР СТЕНДА ПЕРЕД ПЕРЕНОСОМ (посадка `own`, 2026-10-01, внешний слушатель через
проброс, HTTP/1.1, предъявитель — `jwtAccountAdminA`): три BOUND-пути →
`404 {"code":5, "message":"Not Found", "details":[]}`; UNBOUND-пути и `/kaname-no-such-route-*`
→ одинаковый `403` fail-closed каталога; `GET /geo/v1/regions` → `200 {"regions":[…]}`;
List и Create интерактивного клиента под `jwtBootstrap` → тот же `404 Not Found`.
На внутреннем слушателе: LookupSubject человека посева по его `externalId`
(`own:sub-…`) → `200 {"user":{…}}` с его id, ненайденного →
`404 "subject not found by external_id=…"`, Check → `200 {"allowed":true}`, List
клиентов → `200`.

Why no black-box POSITIVE revoke→IsRevoked case:
  InternalSessionRevocationsService is gRPC-only on the service's internal
  listener — the api-gateway does NOT front it on its REST mux. There is
  therefore no HTTP surface to drive revoke→IsRevoked black-box through Newman;
  the closed loop is covered white-box by the service in its own repository
  (PRO-Robotech/kaname). The same applies to the USER-LEVEL revoke-all gate
  (ForceLogout / Revoke(revoke_all_user_tokens)). The black-box contract that
  remains here is the external-isolation NEGATIVE: these internal RPCs must never
  appear on the advertised external TLS endpoint.

Environment requirements:
  {{baseUrl}}          — PUBLIC api-gateway cmux. Used for the operations poll
                         (public OpsProxy). Does NOT serve /iam/v1/internal/* (404 by design).
  {{internalBaseUrl}}  — api-gateway dedicated cluster-internal REST listener
                         (`internal-rest` :8081). The POSITIVE controls go here
                         (`mux="internal"`); Internal* RPCs are served ONLY here.
  {{externalBaseUrl}}  — the ADVERTISED EXTERNAL TLS LISTENER of the api-gateway
                         (:8443). deploy/scripts/newman-{e2e,parallel}.sh forward it
                         and inject it as `--env-var externalBaseUrl`. Must NOT
                         expose Internal* paths.

WHY externalBaseUrl IS THE :8443 LISTENER AND NOT THE INGRESS HOSTNAME
----------------------------------------------------------------------
Ban #6 is a property of the LISTENER — which routes it serves — not of the DNS
name used to find it. The advertised hostname does not resolve on a runner, kind
publishes only node:80, and the Ingress in front of the listener speaks GRPCS, so
EVERY REST path through it answers 502 — a "404 on external" behind a uniform 502
would be vacuous. So the probes address the TLS listener directly, over a
forwarded port. The certificate names the gateway's in-cluster identity and
cannot match 127.0.0.1, so those steps carry `insecure_tls=True` (per-item
`strictSSL:false`, visible in the generated collection) — the tunnel's trust
chain is not the subject; the route table is.

WHAT AN UNAUTHENTICATED PROBE CANNOT SEE
----------------------------------------
On this listener authN and the authz catalog run BEFORE the REST mux. Without a
token EVERY path answers alike, so an unauthenticated probe cannot tell an
isolated route from a typo, and the 404 these cases assert can never arrive. All
external probes therefore carry a valid Bearer. That also makes the claim
stronger: an AUTHENTICATED external caller still cannot reach Internal*.

TWO FAMILIES, AND ONLY ONE OF THEM DISCRIMINATES
------------------------------------------------
  BOUND REST paths — `/iam/v1/internal/iam:lookupSubject`, `iam:check`,
  `/iam/v1/internal/interactiveClients`:
      internal 200 / 404-from-service / 400-from-service   (the route is real)
      external 404 {"code":5,"message":"Not Found"}         (mux miss)
  These carry the evidence: the path demonstrably works somewhere, and is
  demonstrably absent from the external mux. The body is checked too — a mux miss
  is grpc-gateway's ROUTING error, the bare {code:5, message:"Not Found"}, and a
  service-level miss NAMES the resource and the id. Conflating them would let a
  real 404-from-iam pass as isolation.

  UNBOUND fully-qualified paths — InternalSessionRevocationsService/{Revoke,
  IsRevoked,ListByUser}, InternalIAMService/ForceLogout, and GET
  /iam/v1/internal/users/{id}: 403 on the external listener, byte-identical to a
  nonsense path. They have no catalog entry, so the fail-closed authz gate answers
  before the mux is consulted. So family B asserts what it can actually witness —
  NEVER 2xx — and pins its code against a nonsense-path control fired at the same
  listener in the same case. If a route ever appears, the 2xx assertion fires; if
  the fail-closed behaviour changes, the pin fires.

  IAM-INT-NEG-EXT-REST-ALIVE is the other half. Without a positive control on the
  SAME endpoint proving it serves REST at all, every 404 below would be satisfied
  by an endpoint that was simply down.

Техники: таблица решений (слушатель × семейство пути × предъявитель), классы
эквивалентности (BOUND / UNBOUND / заведомо отсутствующий путь), угадывание ошибок
(промах маршрутизатора против промаха владельца — различитель текст, а не код).

Test-first note (strict TDD): negative (external) cases pass only when the probe
is ANSWERED and the answer is the expected one — an unreachable endpoint is a RED
harness, never a green check. Do not weaken assertions, and in particular do not
reintroduce `if (pm.response.code === undefined) return;`.
"""

# ЧЬЁ ПОВЕДЕНИЕ УТВЕРЖДАЕТ ЭТОТ МОДУЛЬ (e2e-flow.md §7а, решение владельца 2026-09-12).
# Домены по REST-путям — `iam` и `geo`: оба здесь НОСИТЕЛИ. Предмет — рубеж края
# (ban #6): Internal*-методы доступны только на cluster-internal слушателе края и
# недостижимы на внешнем адресе. Производитель — край платформы.
ASSERTS_DOMAIN = "gateway"
ASSERTS_REASON = (
    "предмет — РУБЕЖ КРАЯ (ban #6): Internal*-методы доступны только на cluster-internal "
    "слушателе края и недостижимы на внешнем адресе. Производитель — маршрутная таблица "
    "края; маршруты iam и geo здесь носители, а не предмет."
)

CASES = []

# ---------------------------------------------------------------------------
# Unbound REST paths for Internal* RPCs that carry NO `google.api.http` binding.
#
# Only two InternalIAMService RPCs are annotated (`:lookupSubject`, `:check`);
# ReadTuples / SessionRevocations.Revoke / .IsRevoked / ForceLogout are not.
#
# The first of the four used to be InternalAuthorizeService/WriteTuples. That RPC
# was retired from the contract (PRO-Robotech/kaname#788, zero callers), and a probe aimed at a
# method that no longer exists asserts nothing: it would pass because there is no
# such method, not because ban #6 holds — the exact defect this block's own note
# below describes as "the form of an isolation check and none of its substance".
# ReadTuples is its live sibling on the same service, equally unbound.
# grpc-gateway is generated with `generate_unbound_methods=true`, so the route
# these four actually answer on is the fully-qualified default form below — and
# it is served ONLY by the cluster-internal sub-mux (ban #6).
#
# The earlier `/iam/v1/internal/authorize:writeTuples`-style paths were invented:
# no binding of that shape exists on ANY listener, so "404 on external" held for
# a path that 404s everywhere. The probe had the form of an isolation check and
# none of its substance. Same shape as the storage suite's *-EXTERNAL-ABSENT
# cases, which target this default form for exactly this reason.
# ---------------------------------------------------------------------------

# Путь InternalAuthorizeService/WriteTuples здесь БОЛЬШЕ НЕ ПРОБУЕТСЯ: службы
# администрирования хранилища отношений не существует (стадия S6 эпика PRO-Robotech/kaname#747 сняла
# её вместе с внешним движком прав). Проба по этому адресу стала бы дублем
# бессмысленного контроля `/zzz` — тем самым «утверждением, которое не может
# упасть», ради отличия от которого весь этот блок и написан.
_UNBOUND_SR_REVOKE = "/kaname.cloud.iam.v1.InternalSessionRevocationsService/Revoke"
_UNBOUND_SR_ISREVOKED = "/kaname.cloud.iam.v1.InternalSessionRevocationsService/IsRevoked"
_UNBOUND_SR_LISTBYUSER = "/kaname.cloud.iam.v1.InternalSessionRevocationsService/ListByUser"
_UNBOUND_FORCE_LOGOUT = "/kaname.cloud.iam.v1.InternalIAMService/ForceLogout"

# ---------------------------------------------------------------------------
# Внешний слушатель адресуется ОБЪЯВЛЕНИЕМ шага (`mux="external"`), а не
# переписыванием адреса в пред-скрипте: генератор края сам ставит страж
# переменной `{{externalBaseUrl}}` (`require_env_url` — отказ по имени
# переменной, затем пропуск), поэтому потерянная переменная краснеет, а не
# уменьшает набор молча.
# ---------------------------------------------------------------------------


def _external_step(name, method, path, body=None, auth="jwtAccountAdminA", test_script=None):
    """An external-endpoint probe: authenticated, TLS-verification off for the
    forwarded port, addressed to {{externalBaseUrl}}, and always asserting it
    was ANSWERED before it asserts anything about the answer."""
    return Step(
        name=name,
        method=method,
        path=path,
        body=body,
        auth=auth,
        mux="external",
        insecure_tls=True,
        test_script=test_script or [],
    )


# ===========================================================================
# CONTROL: the external endpoint SERVES REST at all
#
# Every negative below reads "this path is not there". That sentence is only
# worth something if the endpoint answers when a path IS there. Without this
# control, an endpoint that was simply down would satisfy all eight negatives —
# which is materially what happened while the advertised host did not resolve.
# ===========================================================================

CASES.append(Case(
    id="IAM-INT-NEG-EXT-REST-ALIVE",
    title="Control: the advertised external TLS listener serves REST (so a 404 below means 'not routed', not 'nothing there')",
    classes=["SEC"],
    priority="P0",
    steps=[
        _external_step(
            name="public-path-on-external",
            method="GET",
            path="/geo/v1/regions",
            test_script=[
                *assert_answered("EXT-ALIVE"),
                "// A PUBLIC path on the SAME listener as every negative below. If this is not",
                "// 200, the negatives prove nothing and must not be read as isolation.",
                "pm.test('EXT-ALIVE: public REST path answers 200 on the external listener', () =>",
                "  pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                "pm.test('EXT-ALIVE: and it is a real REST body, not an edge error page', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j, JSON.stringify(j)).to.have.property('regions');",
                "});",
            ],
        ),
    ],
))


# ===========================================================================
# FAMILY A — BOUND Internal* REST paths. These DISCRIMINATE.
#
# The path is proven real by the positive controls further down (same path, the
# internal-rest listener, 200 / service-level 4xx). Here it must be a
# grpc-gateway MUX MISS: status 404 AND grpc-gateway's own ROUTING body. The body
# matters — a service-level "not found" NAMES the resource and the id, and
# accepting it here would let a genuine iam 404 masquerade as route isolation.
# ===========================================================================

def _mux_miss_assertions(label: str, leak_expr: str = None, leak_desc: str = None):
    out = [
        *assert_answered(label),
        f"pm.test('{label}: status 404 — path is not routed on the external mux', () =>",
        f"  pm.expect(pm.response.code, pm.response.text()).to.eql(404));",
        "// Discriminate a MUX miss from a SERVICE miss. An unrouted path is answered by",
        "// grpc-gateway's ROUTING error — the bare {code:5, message:'Not Found'}, the same",
        "// answer a nonsense path gets on this listener. iam's own NOT_FOUND always names",
        "// the resource and the id ('<Resource> <id> not found', contract tone), so any",
        "// other message here would mean the request REACHED the service.",
        "//",
        "// This used to ask whether the body was JSON AT ALL, because the hidden route was",
        "// answered by a second producer in plain text. That difference was itself an",
        "// existence-oracle for the admin surface and has been removed; the discriminator",
        "// is the message, not the content type.",
        f"pm.test('{label}: 404 is a mux miss, not a service-level NOT_FOUND', () => {{",
        "  let j = null;",
        "  try { j = pm.response.json(); } catch (e) { j = null; }",
        "  pm.expect((j || {}).message, 'any other message here would mean the request REACHED the service')",
        "    .to.eql('Not Found');",
        "});",
    ]
    if leak_expr:
        out += [
            f"pm.test('{label}: no {leak_desc} in body', () => {{",
            "  let j = null;",
            "  try { j = pm.response.json(); } catch (e) { j = null; }",
            f"  pm.expect({leak_expr}, '{leak_desc} must not be in the response').to.be.undefined;",
            "});",
        ]
    return out


CASES.append(Case(
    id="IAM-INT-NEG-EXT-IAM-LOOKUPSUBJECT",
    title="InternalIAMService.LookupSubject on the external TLS listener → 404 mux miss (internal-only, ban #6)",
    classes=["NEG", "SEC"],
    priority="P0",
    steps=[
        _external_step(
            name="lookup-subject-on-external",
            method="POST",
            path="/iam/v1/internal/iam:lookupSubject",
            body={"externalId": "zit-anything"},
            test_script=_mux_miss_assertions("EXT-LOOKUPSUBJ", "(j || {}).subjectId", "subjectId"),
        ),
    ],
))


CASES.append(Case(
    id="IAM-INT-NEG-EXT-IAM-CHECK",
    title="InternalIAMService.Check on the external TLS listener → 404 mux miss (internal-only, ban #6)",
    classes=["NEG", "SEC"],
    priority="P0",
    steps=[
        _external_step(
            name="iam-check-on-external",
            method="POST",
            # CheckRequest names the object field `object` and takes a TYPED FGA
            # string; there is no `objectId` (that belongs to ExpandAccessRequest).
            path="/iam/v1/internal/iam:check",
            body={"subjectId": "user:usr00000000000000abc", "relation": "viewer",
                  "object": "account:acc00000000000abc"},
            test_script=_mux_miss_assertions("EXT-IAM-CHECK", "(j || {}).allowed", "allowed verdict"),
        ),
    ],
))


# ===========================================================================
# FAMILY B — UNBOUND / by-id paths. These do NOT discriminate, and the case
# says so out loud rather than implying otherwise.
#
# Measured on all three listeners: 403, byte-identical to a nonsense path,
# because no catalog entry exists and the fail-closed authz gate answers before
# the mux. So the honest assertions are:
#   1. it was ANSWERED (else the harness is broken);
#   2. it NEVER succeeds — no 2xx, on the external listener, ever;
#   3. its code equals the nonsense-path control taken at the same listener in
#      the same case — the explicit admission that this family cannot tell
#      "isolated" from "misspelt", recorded where a reader will see it.
# If a route ever appears for these, (2) fires. If fail-closed changes, (3) fires.
# ===========================================================================

_UNBOUND_PROBES = [
    ("EXT-SR-REVOKE", "InternalSessionRevocationsService.Revoke", _UNBOUND_SR_REVOKE,
     {"userId": "usr00000000000000abc", "tokenJti": "leak-jti", "reason": "x"}),
    ("EXT-SR-ISREVOKED", "InternalSessionRevocationsService.IsRevoked", _UNBOUND_SR_ISREVOKED,
     {"tokenJti": "leak-jti"}),
    # ListByUser — единственный глагол этой службы, чей ОТВЕТ несёт сведения о
    # человеке (какие его сессии сняли, когда и почему), и до PRO-Robotech/kaname#1140 он один из
    # трёх здесь отсутствовал. Наблюдаемая арендатором поверхность у него ровно
    # одна — «снаружи недостижим», и утверждать надо именно её: REST-привязки у
    # службы нет by construction (`google.api.http` в её proto — ноль), поэтому
    # положительной чёрноящичной полосы для «человек видит свою историю» здесь
    # не существует, и подделывать её нельзя. Сужение круга держателей
    # утверждается там, где оно происходит, — белым ящиком в репозитории службы
    # (PRO-Robotech/kaname), а не чёрным ящиком края.
    ("EXT-SR-LISTBYUSER", "InternalSessionRevocationsService.ListByUser", _UNBOUND_SR_LISTBYUSER,
     {"userId": "usr00000000000000abc", "pageSize": 10}),
    ("EXT-FORCELOGOUT", "InternalIAMService.ForceLogout", _UNBOUND_FORCE_LOGOUT,
     {"userId": "usr00000000000000abc", "reason": "x"}),
]

_unbound_steps = [
    # The control FIRST: its code is what the probes are pinned against.
    _external_step(
        name="nonsense-path-control-on-external",
        method="GET",
        path="/kaname-no-such-route-{{runId}}",
        test_script=[
            *assert_answered("EXT-CONTROL"),
            "// A path that certainly does not exist, on the same listener, with the same",
            "// credentials. Whatever the edge answers here is what 'unreachable' looks like",
            "// WITHOUT any isolation being involved — the baseline the probes below are",
            "// measured against.",
            "pm.test('EXT-CONTROL: a certainly-absent path does not succeed', () =>",
            "  pm.expect(pm.response.code, pm.response.text()).to.be.above(399));",
            "pm.environment.set('_extAbsentCode', String(pm.response.code));",
        ],
    ),
]

for _label, _rpc, _path, _body in _UNBOUND_PROBES:
    _unbound_steps.append(_external_step(
        name=f"{_label.lower()}-on-external",
        method="POST",
        path=_path,
        body=_body,
        test_script=[
            *assert_answered(_label),
            f"pm.test('{_label}: {_rpc} NEVER succeeds on the external listener', () => {{",
            "  pm.expect(pm.response.code, pm.response.text()).to.not.be.oneOf([200, 201, 202, 204]);",
            "});",
            f"pm.test('{_label}: indistinguishable from an absent path (this probe does NOT "
            f"prove route isolation — see the family-B note)', () => {{",
            "  const baseline = pm.environment.get('_extAbsentCode');",
            "  pm.expect(String(pm.response.code), 'diverged from the absent-path baseline: "
            "the edge changed behaviour and this probe needs re-deriving').to.eql(baseline);",
            "});",
        ],
    ))

_unbound_steps.append(_external_step(
    name="internal-user-get-by-id-on-external",
    method="GET",
    path="/iam/v1/internal/users/usr00000000000000abc",
    test_script=[
        *assert_answered("EXT-USER-GET"),
        "pm.test('EXT-USER-GET: InternalUserService.Get NEVER succeeds on the external listener', () => {",
        "  pm.expect(pm.response.code, pm.response.text()).to.not.be.oneOf([200, 201, 202, 204]);",
        "});",
        "pm.test('EXT-USER-GET: no user id leak', () => {",
        "  let j = null;",
        "  try { j = pm.response.json(); } catch (e) { j = null; }",
        "  pm.expect((j || {}).id, 'id must not be in the response').to.be.undefined;",
        "});",
        "pm.test('EXT-USER-GET: indistinguishable from an absent path (does NOT prove route isolation)', () => {",
        "  pm.expect(String(pm.response.code)).to.eql(pm.environment.get('_extAbsentCode'));",
        "});",
    ],
))

CASES.append(Case(
    id="IAM-INT-NEG-EXT-UNBOUND-NEVER-SUCCEEDS",
    title="Unbound Internal* RPCs never succeed on the external TLS listener (fail-closed; NOT a route-isolation proof — see case notes)",
    classes=["NEG", "SEC"],
    priority="P0",
    steps=_unbound_steps,
))


# ===========================================================================
# POSITIVE CONTROL: Internal-only paths ARE reachable on the internal listener
# These cases run against {{internalBaseUrl}} — the api-gateway cluster-internal
# REST listener (`internal-rest`, :8081), declared per step with `mux="internal"`.
# ===========================================================================

# ---------------------------------------------------------------------------
# IAM-INT-OK-INT-IAM-LOOKUPSUBJECT
# InternalIAMService.LookupSubject on internal → 200 for a LIVE subject.
#
# Субъект — человек ПОСЕВА (`userAAAId`), заведённый регистрацией у края: его
# `externalId` читается шагом-фикстурой, а не создаётся хуком поставщика (довод —
# в шапке). Исход один — 200 и тот же id: 404 здесь был бы тем самым дефектом,
# ради которого кейс написан. Отличие сервисного 404 от промаха маршрутизатора
# проверяет соседний кейс на НЕизвестном идентификаторе, где 404 и есть предмет.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-INT-OK-INT-IAM-LOOKUPSUBJECT",
    title="InternalIAMService.LookupSubject on cluster-internal listener — the seed person's "
          "externalId resolves to that person → 200",
    classes=["CRUD", "SEC"],
    priority="P1",
    steps=[
        # ФИКСТУРА, а не предмет: внешний идентификатор живого человека посева.
        Step(
            name="read-seed-person-external-id",
            method="GET",
            path="/iam/v1/users/{{userAAAId}}",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "pm.test('fixture: the seed person carries an externalId to look up', () =>",
                "  pm.expect(String(j.externalId || ''), JSON.stringify(j)).to.have.length.above(0));",
                *save_from_response("j.externalId", "_intLookupExternalId"),
            ],
        ),
        Step(
            name="lookup-subject-on-internal",
            method="POST",
            path="/iam/v1/internal/iam:lookupSubject",
            body={"externalId": "{{_intLookupExternalId}}"},
            mux="internal",
            # internal-rest listener enforces authN — send a valid JWT.
            auth="jwtAccountAdminA",
            test_script=[
                *assert_status(200),
                "const j = pm.response.json();",
                "// LookupSubject returns {user: {id: '...', ...}} or {serviceAccount: {...}}.",
                "const subjectId = (j.user && j.user.id) || (j.serviceAccount && j.serviceAccount.id) || j.subjectId;",
                "pm.test('INT-LOOKUPSUBJ: subjectId present', () => pm.expect(subjectId, 'subject id must be set').to.be.a('string').with.length.greaterThan(0));",
                "pm.test('INT-LOOKUPSUBJ: subjectId is the seed person whose externalId was asked', () =>",
                "  pm.expect(subjectId, JSON.stringify(j)).to.eql(pm.environment.get('userAAAId')));",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# IAM-INT-OK-INT-IAM-LOOKUPSUBJECT-UNKNOWN
# LookupSubject for a nonexistent externalId → 404 from the SERVICE, not a
# mux-404 (path not found).
#
# Что здесь на самом деле различает одно от другого — ТЕКСТ, а не код. Прежняя
# редакция этого заголовка (и единственное утверждение под ним) объявляла
# различителем `code == 5`; замер по живому проводу того же прогона показывает,
# что промах маршрутизатора несёт РОВНО ТОТ ЖЕ код:
#   владелец      → {"code":5,"message":"subject not found by external_id=zit-…"}
#   маршрутизатор → {"code":5,"message":"Not Found","details":[]}
# Владелец называет в тексте наш ключ, потому что он его читал; маршрутизатор
# назвать его не может — тела он не видел. На этом различии стоит классификатор
# посева церемонии (он переехал в репозиторий службы вместе с её набором,
# PRO-Robotech/kaname#398; исходы «нет-строки» против «не-дают-спросить»), и без
# утверждения ниже его предпосылка не была прибита ничем: постоянная ошибка
# настройки навсегда читалась бы как задержка.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-INT-OK-INT-IAM-LOOKUPSUBJECT-UNKNOWN",
    title="InternalIAMService.LookupSubject for unknown externalId → 404 ВЛАДЕЛЬЦА (текст называет запрошенный external_id; код 5 несёт и промах mux)",
    classes=["NEG", "SEC"],
    priority="P1",
    steps=[
        Step(
            name="lookup-unknown-on-internal",
            method="POST",
            path="/iam/v1/internal/iam:lookupSubject",
            body={"externalId": "zit-nonexistent-{{runId}}"},
            # Internal* → internal-rest listener ({{internalBaseUrl}}).
            mux="internal",
            # internal-rest listener enforces authN — send a valid JWT.
            auth="jwtAccountAdminA",
            test_script=[
                "pm.test('INT-LOOKUPSUBJ-UNK: status 404', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(404));",
                "const j = pm.response.json();",
                "pm.test('INT-LOOKUPSUBJ-UNK: grpc code 5 (NOT_FOUND, необходимое условие — но НЕ различитель)', () => pm.expect(j.code, JSON.stringify(j)).to.equal(5));",
                # РАЗЛИЧИТЕЛЬ: ответ владельца называет ЗАПРОШЕННЫЙ ключ, промах
                # маршрутизатора назвать его не может. Спрошенное берём из самого
                # запроса, а не из литерала: иначе утверждение начнёт проверять
                # чужую строку в тот день, когда литерал поменяют.
                "const asked = ((pm.request.body && pm.request.body.raw) ? JSON.parse(pm.request.body.raw).externalId : '');",
                "pm.test('INT-LOOKUPSUBJ-UNK: 404 ВЛАДЕЛЬЦА называет запрошенный external_id (mux-404 несёт тот же код 5, но нашего ключа не знает)', () => {",
                "  pm.expect(asked, 'кейс обязан знать, о чём спросил').to.be.a('string').and.not.empty;",
                "  pm.expect(j.message || '', JSON.stringify(j)).to.include(asked);",
                "});",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# IAM-INT-OK-INT-IAM-CHECK
# InternalIAMService.Check on internal → valid response (200 allowed/denied or 404 not found)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="IAM-INT-OK-INT-IAM-CHECK",
    title="InternalIAMService.Check on cluster-internal listener → 200 (allowed=true or false)",
    classes=["CRUD", "SEC"],
    priority="P1",
    steps=[
        Step(
            name="iam-check-on-internal",
            method="POST",
            path="/iam/v1/internal/iam:check",
            # CheckRequest is an FGA triple: `subject_id` and `object` are TYPED
            # strings ("user:<usr…>" / "account:<acc…>"), and the object field is
            # named `object` — there is no `objectId`. The previous body sent
            # `objectId`, which the edge discards, so `object` arrived empty and the
            # required-check rejected the call: this "OK" probe never reached the PDP
            # at all, it only ever measured a 400 that the tolerant assertion accepted.
            body={
                "subjectId": "user:{{userAAAId}}",
                "relation": "viewer",
                "object": "account:{{accountAId}}",
            },
            # Internal* → internal-rest listener ({{internalBaseUrl}}).
            mux="internal",
            # internal-rest listener enforces authN — send a valid JWT.
            auth="jwtAccountAdminA",
            test_script=[
                "// A well-formed triple always resolves: the PDP answers 200 with a boolean",
                "// verdict (allowed true or false). 4xx is NOT tolerated — tolerating 400 is",
                "// exactly what kept the discarded-key defect invisible.",
                "pm.test('INT-IAM-CHECK: status 200 (PDP answered)', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                "const j = pm.response.json();",
                "pm.test('INT-IAM-CHECK: allowed is a boolean verdict', () => pm.expect(j.allowed, JSON.stringify(j)).to.be.a('boolean'));",
            ],
        ),
    ],
))



# ===========================================================================
# InternalInteractiveClientService — the interactive-login client, IAM-INT-1-11.
#
# WHAT KEEPS IT OFF THE OUTSIDE, STATED PRECISELY. The route IS mounted on both
# multiplexers: registration walks the pair in ONE loop, `Internal*` included.
# Isolation is the DISPATCHER's doing — `isInternalRoute` classifies the (method,
# path) pair and answers 404 on external origin, hiding existence. Saying "it is
# not mounted" would send the next reader to fix the registration instead of the
# classifier.
#
# WHY THE PROBE CARRIES `jwtBootstrap` AND NOT A TENANT SUBJECT. Bootstrap is the
# cluster `system_admin` ServiceAccount — the ONE principal that is authorised for
# this surface and acr-exempt for its mutations. Probing with anybody else would
# leave "not routed" indistinguishable from "not allowed", and the second is not
# what ban #6 is about. Refused for the RIGHT reason is the whole assertion.
#
# The three cases here are the newman half of scenario 11. The behavioural census
# on the tree side already carries the same pair — gateway/internal/restmux/external_isolation_test.go
# holds `{"POST", "/iam/v1/internal/interactiveClients"}` — and this file is the
# black-box census; neither is a second copy of the other, and no third one is
# started beside them.
# ===========================================================================

_IC_PATH = "/iam/v1/internal/interactiveClients"

CASES.append(Case(
    id="IAM-INT-NEG-EXT-IC-LIST",
    title="InternalInteractiveClientService.List on the external TLS listener → 404 mux miss "
          "(internal-only, ban #6) — refused even to the principal authorised for it",
    classes=["NEG", "SEC"],
    priority="P0",
    steps=[
        _external_step(
            name="ic-list-on-external",
            method="GET",
            path=_IC_PATH,
            auth="jwtBootstrap",
            test_script=_mux_miss_assertions(
                "EXT-IC-LIST", "(j || {}).interactiveClients", "interactiveClients page"),
        ),
    ],
))


CASES.append(Case(
    id="IAM-INT-NEG-EXT-IC-CREATE",
    title="InternalInteractiveClientService.Create on the external TLS listener → 404 mux miss "
          "(internal-only, ban #6) — no client is registered at the provider from the outside",
    classes=["NEG", "SEC"],
    priority="P0",
    steps=[
        _external_step(
            name="ic-create-on-external",
            method="POST",
            path=_IC_PATH,
            auth="jwtBootstrap",
            body={
                "name": "iaclient-ext-{{runId}}",
                "redirectUris": ["https://api.kacho.local/auth/callback"],
            },
            # The leak expression is the Operation id: if one came back, a client was
            # registered at the identity provider through the advertised endpoint —
            # the outcome ban #6 exists to prevent, not merely a routing surprise.
            test_script=_mux_miss_assertions(
                "EXT-IC-CREATE", "(j || {}).metadata", "Operation metadata (a client was registered)"),
        ),
    ],
))


CASES.append(Case(
    id="IAM-INT-OK-INT-IC-LIST",
    title="InternalInteractiveClientService.List on the cluster-internal listener → 200 with a page "
          "(positive control: the two 404s above mean 'not routed here', not 'nowhere at all')",
    classes=["CRUD", "SEC"],
    priority="P0",
    steps=[
        Step(
            name="ic-list-on-internal",
            method="GET",
            path=_IC_PATH,
            mux="internal",
            # system_admin @ cluster — the catalog gate on all five RPCs. A tenant-tier
            # subject gets a correct 403 here, which would make this control assert
            # nothing about routing.
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.test('INT-IC-LIST: the internal listener serves the page', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j, JSON.stringify(j)).to.be.an('object');",
                "  // An empty cluster is a legal page: the assertion is that the ROUTE",
                "  // answers with the response message, not that anything was created.",
                "  const items = j.interactiveClients === undefined ? [] : j.interactiveClients;",
                "  pm.expect(items, 'interactiveClients must be a page, absent meaning empty').to.be.an('array');",
                "});",
            ],
        ),
    ],
))


