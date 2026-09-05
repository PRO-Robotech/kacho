#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Production-mode RS256 token minter for the newman authz seed (Phase C, #59).

Production authN (api-gateway `authn.mode=production-strict`) accepts ONLY
Hydra-signed RS256 Bearers with `aud=https://{API_DOMAIN}`. Symmetric HS256 Bearers
are inert against it (401), which is why the harness has no minter of its own any
more: this module produces real RS256 tokens through the SAME machinery the platform
uses, no dev-bypass, no direct Hydra-admin:

  1. bootstrap admin  — InternalBootstrapTokenService.MintBootstrapToken, called by
     a DIRECT mTLS gRPC dial to kaname :9091 (there is no REST route — see
     `mint_bootstrap`). A cluster `system_admin` ServiceAccount Bearer (acr-EXEMPT),
     the entry point that seeds everything else.
  2. per-subject      — with the admin Bearer, UserTokenService.Issue /
     SAKeyService.Issue заводят строку в НАШЕМ реестре и отдают ES256 (P-256)
     приватный ключ ОДИН раз. Мы подписываем им утверждение клиента
     (private_key_jwt, RFC 7521/7523) и обмениваем его У НАШЕГО ИЗДАТЕЛЯ
     (`POST /iam/v1/token`, `aud=https://{API_DOMAIN}`) → токен субъекта нашей
     чеканки, чьи утверждения `kacho_principal_*` резолвятся в его User/SA и
     привязки.

ОБА ШАГА ЧЕКАНИМ МЫ, И ЭТО НЕ ДЕТАЛЬ (задачи #1119, #1120, #1121). Шаг 1: iam
подписывает бутстрап-удостоверение своим ключом из ключницы, издателем стоит наш
`authn.token-signing.issuer`. Шаг 2: выдача больше не заводит зеркала клиента у
внешнего поставщика ни для служебной учётки (#1120), ни для человека (#1121),
поэтому обменивается ключ только у нас. К поставщику этот модуль не идёт НИ ОДНИМ
вызовом. Читателю, разбирающему отказ любого из шагов, идти в журналы поставщика
бесполезно — его там нет; смотреть надо издателя токена (`iss`) и наш набор
проверочных ключей.

Живой контур прежнего издателя в дереве ОСТАЛСЯ, и он не здесь: интерактивный вход
человека (`authorization_code` + PKCE, посев церемонии). Он и есть предмет
требования держать прежнего издателя принятым на крае — см.
`deploy/scripts/assert-legacy-issuer-acceptance-has-a-subject.py`.

Requires PyJWT + cryptography (ES256 signing). Usable as a library (import) or a CLI.

STATUS (Phase C, #59) — UNBLOCKED + PROVEN end-to-end:
  - `mint_bootstrap` (bootstrap admin) — PROVEN: MintBootstrapToken → RS256 →
    api-gateway GET /iam/v1/accounts = 200 (IBT-04). Since the #58 hardening the
    call is gRPC-over-mTLS to iam :9091 with the bootstrap-operator client cert
    (the gateway REST route is gone) — the token it returns is unchanged.
  - `sa_platform_token` (per-subject SA) — ключ служебной учётки, обменянный у
    НАШЕГО издателя. Прежний помощник той же роли обменивал его у
    внешнего поставщика и СНЯТ вместе со своим предметом: выдача ключа больше не
    заводит там клиента (#1120), поэтому обмен у поставщика отвечает
    `invalid_client` при любом входе, а помощник, который не может сработать
    никогда, — хуже отсутствующего. Снята вместе с ним и его величина
    (`ASSERTION_AUDIENCE`) — читателя у неё не осталось.

    Осталось верным и не изменилось: SAKeyService.Issue принимает
    `audience:[https://api.kacho.cloud]` (resolveAudience служебного ключа
    считается с адресатами вызывающего); утверждение `kacho_principal_type=
    service_account` от обогащения делает токен acr-EXEMPT (stepup_gate O-1) и
    достижимым на ручках с acr=1. Блокер `created_by` (#60) для служебных ключей
    закрыт: вызывающий-машина записывает `created_by` = владелец аккаунта целевой
    учётки (см. обработчик/use-case sa_keys). Коллекция vpc `network` проходит
    ЗЕЛЕНО на машинном посеве под production-strict (см. prodseed_network.py).
  - `user_platform_token` (per-subject USER) — персональный токен пользователя,
    обменянный у НАШЕГО издателя. Прежний помощник той же роли обменивал его у
    внешнего поставщика и СНЯТ вместе со своим предметом: выпуск персонального
    токена больше не заводит там клиента (#1121), поэтому обмен у поставщика
    отвечает `invalid_client` при любом входе, а помощник, который не может
    сработать никогда, — хуже отсутствующего.

    Осталось верным и не изменилось: человеческий предъявитель НЕ освобождён от
    порога повышения, поэтому ручки с `required_acr_min=2` им не вызвать —
    для них нужен интерактивный вход. Машинная сквозная проба по природе
    служебная; для неё `sa_platform_token`.
"""
from __future__ import annotations

import argparse
import base64
import json
import os
import shutil
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

import jwt as pyjwt


# ── bootstrap-mint transport (direct mTLS gRPC to kaname :9091) ──────────
# MintBootstrapToken has NO REST route — the mint is reachable ONLY over mTLS
# gRPC, and only from a client certificate whose SPIFFE SAN kaname allow-lists
# (`authn.bootstrap-mint.allowed-client-sans`). Defaults mirror the ports the
# newman drivers port-forward (deploy/scripts/newman-{e2e,parallel}.sh) and the
# Secret the umbrella issues for exactly this purpose.
IAM_INTERNAL_GRPC = os.environ.get("IAM_INTERNAL_GRPC", "localhost:19091")
BOOTSTRAP_OPERATOR_SECRET = os.environ.get(
    "BOOTSTRAP_OPERATOR_SECRET", "kacho-bootstrap-operator-client-tls")
BOOTSTRAP_OPERATOR_NS = os.environ.get("KACHO_NAMESPACE", "kacho")
_OPERATOR_CERT_DIR = os.environ.get("BOOTSTRAP_OPERATOR_CERT_DIR", "/tmp/kacho-bootstrap-operator")
BOOTSTRAP_MINT_MTLS_CERT = os.environ.get(
    "BOOTSTRAP_MINT_MTLS_CERT", os.path.join(_OPERATOR_CERT_DIR, "client.crt"))
BOOTSTRAP_MINT_MTLS_KEY = os.environ.get(
    "BOOTSTRAP_MINT_MTLS_KEY", os.path.join(_OPERATOR_CERT_DIR, "client.key"))


# ── gateway-identity client cert (gateway-fronted internal RPCs) ────────────
# A SECOND, DIFFERENT identity from the bootstrap-operator above. kaname :9091
# admits the gateway SAN for the ordinary internal RPCs the seed drives through
# grpcurl (InternalUserService.UpsertFromIdentity, InternalIAMService.LookupSubject)
# but deliberately NOT for the bootstrap-token mint. Keep the two apart: pointing
# the mint at this cert is the exact "fix" the #58 hardening exists to prevent.
GATEWAY_CLIENT_SECRET = os.environ.get("GATEWAY_CLIENT_SECRET", "api-gateway-client-tls")
_GATEWAY_CERT_DIR = os.environ.get("IAM_MTLS_CERT_DIR", "/tmp/iam-mtls")
IAM_INTERNAL_MTLS_CERT = os.environ.get(
    "IAM_INTERNAL_GRPC_MTLS_CERT", os.path.join(_GATEWAY_CERT_DIR, "client.crt"))
IAM_INTERNAL_MTLS_KEY = os.environ.get(
    "IAM_INTERNAL_GRPC_MTLS_KEY", os.path.join(_GATEWAY_CERT_DIR, "client.key"))


# ── HTTP helpers ────────────────────────────────────────────────────────────
def _post_json(url: str, payload: dict, bearer: str | None = None, timeout: int = 15) -> tuple[int, dict]:
    data = json.dumps(payload).encode()
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    if bearer:
        req.add_header("Authorization", "Bearer " + bearer)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        try:
            return e.code, json.loads(body or "{}")
        except json.JSONDecodeError:
            return e.code, {"raw": body}


def _post_form(url: str, form: dict, timeout: int = 15,
               context: ssl.SSLContext | None = None) -> tuple[int, dict]:
    data = urllib.parse.urlencode(form).encode()
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")
    try:
        with urllib.request.urlopen(req, timeout=timeout, context=context) as r:
            return r.status, json.loads(r.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        try:
            return e.code, json.loads(body or "{}")
        except json.JSONDecodeError:
            return e.code, {"raw": body}


def _get_json(url: str, bearer: str | None = None, timeout: int = 15) -> tuple[int, dict]:
    req = urllib.request.Request(url, method="GET")
    if bearer:
        req.add_header("Authorization", "Bearer " + bearer)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        try:
            return e.code, json.loads(body or "{}")
        except json.JSONDecodeError:
            return e.code, {"raw": body}


# ── Step 1: bootstrap admin RS256 Bearer ────────────────────────────────────
def ensure_client_cert(secret: str, cert_path: str, key_path: str,
                       namespace: str | None = None) -> bool:
    """Best-effort: (re)extract a kubernetes.io/tls client cert from the cluster.

    ALWAYS re-extracts when kubectl can read the Secret, deliberately: after a fresh
    `dev-up` cert-manager regenerates the internal CA, so a cert left from a PRIOR
    stand is signed by the OLD CA and the iam :9091 handshake rejects it — a
    persistent, NON-transient failure that a retry only repeats (the same trap
    prodrun.sh documents for the gateway cert). If kubectl or the Secret is
    unavailable (CI without cluster access, pre-extracted material), leave whatever
    is on disk alone and let the caller's existence check decide.

    The material is written 0600 OUTSIDE the repository (default /tmp/…): the seed
    borrows the cluster's own leaf for the length of the run, it never carries key
    material in git.

    Returns True when the on-disk pair is usable (freshly written or pre-existing).
    """
    ns = namespace or BOOTSTRAP_OPERATOR_NS
    if shutil.which("kubectl"):
        probe = subprocess.run(
            ["kubectl", "-n", ns, "get", "secret", secret],
            capture_output=True, text=True)
        if probe.returncode == 0:
            # Read BOTH halves before writing EITHER: a half-refreshed pair (new
            # cert, stale key) fails the handshake in a way that reads like an authz
            # problem.
            material = []
            ok = True
            for jsonpath in (r"{.data.tls\.crt}", r"{.data.tls\.key}"):
                out = subprocess.run(
                    ["kubectl", "-n", ns, "get", "secret", secret, "-o", f"jsonpath={jsonpath}"],
                    capture_output=True, text=True)
                if out.returncode != 0 or not out.stdout.strip():
                    ok = False
                    break
                material.append(base64.b64decode(out.stdout))
            if ok:
                os.makedirs(os.path.dirname(cert_path) or ".", exist_ok=True)
                os.makedirs(os.path.dirname(key_path) or ".", exist_ok=True)
                for path, blob in zip((cert_path, key_path), material):
                    with open(path, "wb") as fh:
                        fh.write(blob)
                    os.chmod(path, 0o600)
    return all(os.path.exists(p) and os.path.getsize(p) > 0 for p in (cert_path, key_path))


def ensure_iam_internal_cert() -> bool:
    """Provision the GATEWAY-identity client cert used for the internal :9091 RPCs.

    kaname's internal listener requires a verified client certificate in every
    posture (security.md: internal is NOT exempt), so every grpcurl the seed makes
    must present one. This is the identity for the ordinary internal RPCs — NOT for
    the bootstrap-token mint, which admits only the dedicated operator SAN.
    """
    return ensure_client_cert(GATEWAY_CLIENT_SECRET, IAM_INTERNAL_MTLS_CERT, IAM_INTERNAL_MTLS_KEY)


def _refresh_operator_cert(cert_path: str, key_path: str) -> None:
    """Back-compat shim — bootstrap-operator flavour of ensure_client_cert."""
    ensure_client_cert(BOOTSTRAP_OPERATOR_SECRET, cert_path, key_path)


# ── АДРЕСАТ УТВЕРЖДЕНИЯ У ПРЕЖНЕГО ИЗДАТЕЛЯ СНЯТ ВМЕСТЕ СО СВОЕЙ ПОЛОСОЙ ─────
# Здесь стоял `ASSERTION_AUDIENCE` — `aud` утверждения, адресованного прежнему
# издателю. Его единственным читателем была полоса обмена у поставщика, снятая
# вместе с предметом (#1120): выдача ключа служебной учётки больше не заводит
# зеркала клиента у поставщика, поэтому обмен там отвечает отказом опознания при
# любом входе. Величина, оставшаяся без читателя, — не «про запас», а имя,
# которое следующий читатель примет за действующую настройку.
#
# Адресат утверждения НАШЕЙ полосы — `PLATFORM_ASSERTION_AUDIENCE` ниже, и он
# читается тем же `sign_client_assertion`: техника обмена (private_key_jwt +
# `client_credentials`) никуда не делась, сменился адресат.


def mint_bootstrap(*, grpc_addr: str | None = None,
                   cert: str | None = None, key: str | None = None) -> str:
    """MintBootstrapToken → cluster system_admin SA RS256 Bearer (acr-exempt).

    Transport is a DIRECT mTLS gRPC dial to kaname :9091 — the mint has no REST
    route anywhere (#58 hardening). It mints a cluster `system_admin` Bearer, so it
    cannot be gated by a ReBAC relation (it exists to obtain the FIRST token) and
    must not be gated by network position; the gate is the CALLER'S CERTIFICATE:
    kaname admits only the SPIFFE SANs in `authn.bootstrap-mint.allowed-client-sans`
    (deny-all by default, in every mode). We therefore present the dedicated
    bootstrap-operator client cert, NOT the api-gateway one — the gateway is
    deliberately not a minter.

    `-insecure` skips SERVER-cert verification only (the port-forward changes the
    hostname); the CLIENT cert is still presented and verified by iam — same shape
    as setup.sh's grpcurl calls.

    Overrides: BOOTSTRAP_MINT_MTLS_CERT / _KEY (paths), IAM_INTERNAL_GRPC (addr),
    BOOTSTRAP_OPERATOR_SECRET + KACHO_NAMESPACE (where to pull the material from).
    """
    # NO REQUEST FIELDS, AND NO TTL PARAMETER.
    #
    # `MintBootstrapTokenRequest` is empty: tag 1 held `ttl_seconds` and is now a
    # tombstone (`reserved 1; reserved "ttl_seconds"`) — the lifetime belongs to the
    # issuer's client configuration, so a per-request value only ever changed the
    # number in the RESPONSE, understating the expiry of a cluster-admin credential.
    # See proto/kacho/cloud/iam/v1/internal_bootstrap_token_service.proto.
    #
    # This function kept sending `{"ttlSeconds": N}` after that removal, so every
    # attempt died at the request encoder — `has no known field named ttlSeconds` —
    # and the PRODUCTION seed could not obtain its first token at all. It stayed
    # invisible because the production seed path only runs on a production-posture
    # stand, and CI raises the stand in dev posture, where setup.sh forges its own
    # HS256 bearers and never reaches this call.
    #
    # There is no `ttl_seconds` parameter any more rather than an ignored one: a
    # parameter that changes nothing still promises something. Dropping it also
    # preserves what the old TypeError guard was for — a stale positional caller
    # (this used to take a base-URL first) now fails on arity, by construction.
    addr = grpc_addr or IAM_INTERNAL_GRPC
    cert_path = cert or BOOTSTRAP_MINT_MTLS_CERT
    key_path = key or BOOTSTRAP_MINT_MTLS_KEY
    _refresh_operator_cert(cert_path, key_path)
    for path, what in ((cert_path, "certificate"), (key_path, "key")):
        if not os.path.exists(path) or os.path.getsize(path) == 0:
            raise RuntimeError(
                f"bootstrap-operator client {what} missing at {path}. MintBootstrapToken is "
                f"gated on the caller's client certificate; provision it with "
                f"`kubectl -n {BOOTSTRAP_OPERATOR_NS} get secret {BOOTSTRAP_OPERATOR_SECRET}` "
                f"(issued by the umbrella when mtls.enabled) or point "
                f"BOOTSTRAP_MINT_MTLS_CERT/_KEY at it.")
    if not shutil.which("grpcurl"):
        raise RuntimeError(
            "grpcurl not found in PATH — required to call iam :9091 (the mint has no REST route). "
            "Install: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest")

    args = ["grpcurl", "-insecure", "-max-time", "20",
            "-cert", cert_path, "-key", key_path,
            "-d", "{}", addr,
            "kacho.cloud.iam.v1.InternalBootstrapTokenService/MintBootstrapToken"]
    proc = subprocess.run(args, capture_output=True, text=True, timeout=45)
    if proc.returncode != 0:
        raise RuntimeError(
            f"MintBootstrapToken failed (grpcurl rc={proc.returncode}) at {addr}: "
            f"{proc.stderr.strip() or proc.stdout.strip()}")
    try:
        body = json.loads(proc.stdout or "{}")
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"MintBootstrapToken returned non-JSON from {addr}: {exc}") from None
    if "accessToken" not in body:
        raise RuntimeError(f"MintBootstrapToken response carries no accessToken: keys={list(body)}")
    return body["accessToken"]


# ── Step 2: per-subject OAuth material (Issue → poll Operation) ──────────────
def _poll_operation(base_url: str, admin_token: str, op_id: str, budget_s: int = 30) -> dict:
    url = base_url.rstrip("/") + "/operations/" + op_id
    deadline = time.time() + budget_s
    last = {}
    while time.time() < deadline:
        code, body = _get_json(url, bearer=admin_token)
        last = body
        if code == 200 and body.get("done"):
            if body.get("error"):
                raise RuntimeError(f"operation {op_id} errored: {body['error']}")
            return body.get("response", {})
        time.sleep(0.5)
    raise RuntimeError(f"operation {op_id} not done in {budget_s}s: {last}")


def issue_user_oauth(base_url: str, admin_token: str, user_id: str, created_by_user_id: str) -> dict:
    """UserTokenService.Issue → {clientId, privateKeyPem, keyId, algorithm}."""
    url = base_url.rstrip("/") + f"/iam/v1/users/{user_id}/tokens"
    payload = {
        "userId": user_id,
        "description": "production-newman RS256 seed",
        "ttlSeconds": 0,
        "createdByUserId": created_by_user_id,
        "name": "newman-rs256-" + uuid.uuid4().hex[:8],
    }
    code, body = _post_json(url, payload, bearer=admin_token)
    if code != 200 or "id" not in body:
        raise RuntimeError(f"UserTokenService.Issue failed ({code}) for {user_id}: {body}")
    return _poll_operation(base_url, admin_token, body["id"])


def issue_sa_oauth(base_url: str, admin_token: str, sva_id: str, created_by_user_id: str) -> dict:
    """SAKeyService.Issue → {clientId, privateKeyPem, keyId, algorithm}."""
    url = base_url.rstrip("/") + f"/iam/v1/serviceAccounts/{sva_id}/keys"
    payload = {
        "serviceAccountId": sva_id,
        "description": "production-newman RS256 seed",
        "createdByUserId": created_by_user_id,
    }
    code, body = _post_json(url, payload, bearer=admin_token)
    if code != 200 or "id" not in body:
        raise RuntimeError(f"SAKeyService.Issue failed ({code}) for {sva_id}: {body}")
    return _poll_operation(base_url, admin_token, body["id"])


# ── Step 3: sign the client_assertion + client_credentials exchange ─────────
def _extract_oauth(resp: dict) -> tuple[str, str, str]:
    """Pull (client_id, private_key_pem, key_id) from an Issue-Operation response.

    The response is an unpacked IssueUserTokenResponse / IssueSAKeyResponse — fields
    may sit at the top level or nested under `token`; be tolerant to both.
    """
    def pick(*keys):
        for k in keys:
            if resp.get(k):
                return resp[k]
        tok = resp.get("token") or {}
        for k in keys:
            if tok.get(k):
                return tok[k]
        return ""
    client_id = pick("clientId", "client_id", "oauthClientId", "hydraClientId")
    private_key = pick("privateKeyPem", "private_key_pem")
    key_id = pick("keyId", "key_id")
    if not (client_id and private_key and key_id):
        raise RuntimeError(f"Issue response missing oauth material: keys={list(resp.keys())}")
    return client_id, private_key, key_id


# Тип утверждения, который ТРЕБУЕТ наш проверяющий. Внешний поставщик его не
# требует и не запрещает, но объявлять тип ему мы не начинаем: полосы обязаны
# отличаться ровно тем, чем отличаются, и лишняя правка чужой полосы означала бы
# менять то, что сегодня работает, без предмета.
CLIENT_ASSERTION_TOKEN_TYPE = "client-authentication+jwt"


def sign_client_assertion(client_id: str, private_key_pem: str, key_id: str,
                          assertion_audience: str, ttl_s: int = 120,
                          token_type: str | None = None) -> str:
    """Подписать `client_assertion` (RFC 7521/7523) выданным приватным ключом.

    `token_type` — ЗАГОЛОВОЧНЫЙ `typ`. Пусто ⇒ не объявляем: полоса внешнего
    поставщика работает без него и работала так всегда. НАШ проверяющий тип
    ТРЕБУЕТ (`token-type-mismatch` — отдельный исход его закрытого словаря), и
    без него обмен отвергается ещё до сверки подписи. Измерено вызовом:
    `POST /iam/v1/token` без `typ` отвечает `401 invalid_client`, а журнал iam
    называет исход прямо.
    """
    now = int(time.time())
    claims = {
        "iss": client_id,
        "sub": client_id,
        "aud": assertion_audience,
        "iat": now,
        "exp": now + ttl_s,
        "jti": uuid.uuid4().hex,
    }
    headers = {"kid": key_id}
    if token_type:
        headers["typ"] = token_type
    return pyjwt.encode(claims, private_key_pem, algorithm="ES256", headers=headers)


# ── Обмен у НАШЕГО издателя — ВТОРАЯ полоса края (задача #1014) ────────────
#
# Полоса ВНЕШНЕГО поставщика (`exchange` выше) и полоса НАШЕГО издателя
# отличаются ровно двумя вещами, и обе — величины, а не код:
#
#   * адресат УТВЕРЖДЕНИЯ (`aud` внутри client_assertion) — у нас это
#     ИДЕНТИФИКАТОР ИЗДАТЕЛЯ, а не адрес эндпоинта. Проверяющий сверяет его с
#     `signer.Issuer()`, и адрес эндпоинта отвергается как несовпадение;
#   * адрес обмена — наш `POST /iam/v1/token` на поверхности выдачи (:9096).
#
# Всё остальное — ключ, подпись, форма запроса — то же самое, и это не
# совпадение: обе полосы обслуживает ОДИН выданный ключ. Поэтому здесь нет
# второй подписи и второго разбора ответа операции; переиспользуются те же.
PLATFORM_ASSERTION_AUDIENCE = os.environ.get(
    "PLATFORM_ASSERTION_AUDIENCE", "https://iam.kacho.local")
# Умолчание — порт, который прогонщики newman пробрасывают ВСЕГДА
# (deploy/scripts/newman-{parallel,e2e}.sh, IAM_REGTOKEN_PORT).
PLATFORM_TOKEN_URL = os.environ.get(
    "PLATFORM_TOKEN_URL", "https://127.0.0.1:19096/iam/v1/token")

IAM_SERVER_SECRET = os.environ.get("IAM_SERVER_SECRET", "kaname-server-tls")
_IAM_SERVER_CA_DIR = os.environ.get("IAM_SERVER_CA_DIR", "/tmp/kaname-server-ca")
IAM_SERVER_CA_FILE = os.environ.get(
    "IAM_SERVER_CA_FILE", os.path.join(_IAM_SERVER_CA_DIR, "ca.crt"))


def ensure_iam_server_ca(namespace: str | None = None) -> bool:
    """Вынуть корень внутреннего удостоверяющего из Secret слушателя выдачи.

    Тот же порядок и та же причина, что у `ensure_client_cert`: свежий подъём
    перевыпускает корень, поэтому лежащий с прошлого стенда не годится и повтор
    его не чинит. Материал ПУБЛИЧНЫЙ (только `ca.crt`), пишется 0600 вне дерева.
    """
    ns = namespace or BOOTSTRAP_OPERATOR_NS
    if shutil.which("kubectl"):
        out = subprocess.run(
            ["kubectl", "-n", ns, "get", "secret", IAM_SERVER_SECRET,
             "-o", r"jsonpath={.data.ca\.crt}"],
            capture_output=True, text=True)
        if out.returncode == 0 and out.stdout.strip():
            os.makedirs(os.path.dirname(IAM_SERVER_CA_FILE), mode=0o700, exist_ok=True)
            with open(IAM_SERVER_CA_FILE, "wb") as fh:
                os.chmod(IAM_SERVER_CA_FILE, 0o600)
                fh.write(base64.b64decode(out.stdout.strip()))
    return os.path.exists(IAM_SERVER_CA_FILE)


def platform_tls_context(ca_file: str | None = None) -> ssl.SSLContext:
    """TLS к нашему слушателю выдачи через локальный проброс.

    ПРОВЕРКА ПОДПИСИ СЕРТИФИКАТА ОСТАЁТСЯ, ПРОВЕРКА ИМЕНИ — СНЯТА, и это не
    послабление ради удобства, а следствие измеренного факта: у листа
    `kaname-server-tls` в SAN ТОЛЬКО имена служб кластера
    (`kaname.kacho.svc…`), адреса петли там нет и быть не может, а проброс
    ходит на `127.0.0.1`. Снятие ПОДПИСИ приняло бы любой сертификат, включая
    самоподписанный посторонним, — вот это было бы послаблением; здесь пир
    по-прежнему обязан предъявить лист НАШЕГО внутреннего удостоверяющего.

    Отсутствие корня — ОТКАЗ, а не переход на непроверяемое соединение.
    """
    path = ca_file or IAM_SERVER_CA_FILE
    if not os.path.exists(path):
        raise RuntimeError(
            f"корень внутреннего удостоверяющего не найден ({path}): обмен у нашего "
            f"издателя идёт по TLS, и проверять подпись нечем. Секрет {IAM_SERVER_SECRET} "
            "читается из кластера — проверьте доступ kubectl либо задайте IAM_SERVER_CA_FILE")
    ctx = ssl.create_default_context(cafile=path)
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_REQUIRED
    return ctx


def exchange_at_platform(token_url: str, assertion: str, api_audience: str,
                         scope: str = "", ca_file: str | None = None) -> str:
    """`client_credentials` у НАШЕГО издателя → токен нашей чеканки (ES256).

    Отказ поднимается ИСКЛЮЧЕНИЕМ вместе с телом ответа: пустая величина в
    посеве превратилась бы в отказ доступа на стороне пробы, то есть в вердикт
    о продукте там, где предмет — оснастка.
    """
    form = {
        "grant_type": "client_credentials",
        "client_assertion_type": "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
        "client_assertion": assertion,
        "audience": api_audience,
    }
    if scope:
        form["scope"] = scope
    code, body = _post_form(token_url, form, context=platform_tls_context(ca_file))
    if code != 200 or "access_token" not in body:
        raise RuntimeError(
            f"обмен у нашего издателя не состоялся ({code}) на {token_url}: {body}. "
            "Полоса включена профилем (iam authn.client-token/token-signing) и требует, "
            "чтобы запрошенный адресат стоял в объявленном перечне адресатов")
    return body["access_token"]


# ── Composed one-shot helpers ───────────────────────────────────────────────
def user_platform_token(base_url: str, admin_token: str, user_id: str,
                        created_by_user_id: str, token_url: str, api_audience: str,
                        assertion_audience: str | None = None) -> str:
    """Персональный токен пользователя → утверждение НАШЕМУ издателю → токен НАШЕЙ чеканки.

    Полоса та же, что у `sa_platform_token`, и субъект — другой: человек, а не
    машина. Держать обе нужно именно поэтому — принципал у них разный, и краю
    он приезжает разными утверждениями (`kacho_principal_type` = `user` против
    `service_account`).

    ОДНО ИМЯ, А НЕ ДВА (#1121). Выпуск персонального токена больше не заводит
    клиента у внешнего поставщика, поэтому `clientId` и `keyId` в ответе несут
    ОДНО значение — идентификатор строки нашего реестра, и подписывается им же.
    Прежде `clientId` принадлежал поставщику и нашим издателем не разрешался
    вовсе; проверка ниже — не украшение, а ровно та величина, ради которой
    задача заведена, и она обязана упасть, если два имени вернутся.

    ОТКАЗ ПОДНИМАЕТСЯ, А НЕ ПРОГЛАТЫВАЕТСЯ: пустая величина в посеве доехала бы
    до пробы отказом доступа — вердиктом о продукте там, где предмет оснастка.
    """
    resp = issue_user_oauth(base_url, admin_token, user_id, created_by_user_id)
    client_id, key, registry_client_id = _extract_oauth(resp)
    if client_id != registry_client_id:
        raise RuntimeError(
            "выпуск персонального токена вернул ДВА имени: clientId="
            f"{client_id!r} против keyId={registry_client_id!r}. Наш издатель "
            "разрешает клиента по строке реестра, поэтому первое имя не годится "
            "для обмена ни при каком входе")
    assertion = sign_client_assertion(
        registry_client_id, key, registry_client_id,
        assertion_audience or PLATFORM_ASSERTION_AUDIENCE,
        token_type=CLIENT_ASSERTION_TOKEN_TYPE)
    return exchange_at_platform(token_url, assertion, api_audience)


def sa_platform_token(base_url: str, admin_token: str, sva_id: str,
                      created_by_user_id: str, token_url: str, api_audience: str,
                      assertion_audience: str | None = None) -> str:
    """Ключ служебной учётки → утверждение НАШЕМУ издателю → токен НАШЕЙ чеканки.

    КЛИЕНТОМ ЗДЕСЬ НАЗЫВАЕТСЯ СТРОКА НАШЕГО РЕЕСТРА. Резолвер нашего проверяющего
    читает СВОИ таблицы по нашему идентификатору (`keyId`), поэтому именно он и
    подписывается в `iss`/`sub`. Измерено вызовом: утверждение, назвавшееся чужим
    идентификатором, отвергается как «клиент не разрешается».

    ЭТО ЕДИНСТВЕННАЯ ПОЛОСА ОБМЕНА КЛЮЧА СЛУЖЕБНОЙ УЧЁТКИ (задача #1120). Прежде
    одна выдача заводила ДВЕ записи — клиента у внешнего поставщика и строку у
    нас, — и обменять ключ можно было у обоих. На переведённом контуре зеркала у
    поставщика не заводится, и `clientId` в ответе выдачи называет ту же строку
    нашего реестра, что и `keyId`.
    """
    resp = issue_sa_oauth(base_url, admin_token, sva_id, created_by_user_id)
    _, key, registry_client_id = _extract_oauth(resp)
    assertion = sign_client_assertion(
        registry_client_id, key, registry_client_id,
        assertion_audience or PLATFORM_ASSERTION_AUDIENCE,
        token_type=CLIENT_ASSERTION_TOKEN_TYPE)
    return exchange_at_platform(token_url, assertion, api_audience)


# ── сквозная проверка: край ПРИНИМАЕТ наше бутстрап-удостоверение ───────────
def assert_bootstrap_accepted_by_the_edge(base_url: str, token: str) -> dict:
    """Предъявляет бутстрап-удостоверение краю и ПАДАЕТ, если тот его не принял.

    ЗАЧЕМ ОТДЕЛЬНЫМ ШАГОМ, а не «дальше по посеву само выяснится». Отказ края на
    первом же предъявлении сегодня проявляется через два-три шага и обвиняет
    невиновного: падает то, что честно сделало своё дело при отсутствующем
    предмете. Шаг, создающий ПРЕДМЕТ всего посева, обязан нести собственное
    утверждение.

    ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — исход, а не факт вызова. Токен, который край
    отверг, неотличим от невыпущенного по всему, что можно спросить у iam:
    удостоверение выдано, подпись стоит, срок идёт. Различает их только
    предъявление.

    Утверждение стало нужнее с задачи #1119: подпись теперь НАША, и вместе с ней
    к нам переехали три величины, которые край сверяет, — издатель, набор
    проверочных ключей и объявленный тип токена. Расхождение любой из них
    производит ровно этот отказ.
    """
    url = base_url.rstrip("/") + "/iam/v1/accounts"
    status, body = _get_json(url, bearer=token)
    if status != 200:
        raise RuntimeError(
            "край НЕ принял бутстрап-удостоверение: GET {} ответил {} {}\n"
            "сверьте три величины, которые край сравнивает с токеном: издателя "
            "(`iss` против api-gateway.tokenAcceptance.issuers), адрес нашего "
            "набора ключей (issuerKeySets) и адресат (`aud` против домена API)".format(
                url, status, body))
    return body


# ── CLI ─────────────────────────────────────────────────────────────────────
def main() -> int:
    p = argparse.ArgumentParser(description="Production-mode RS256 token minter (#59)")
    # MintBootstrapToken transport — direct mTLS gRPC to kaname :9091. There is
    # NO gateway REST route (a route on the plain-HTTP internal listener would be a
    # credential-free cluster-admin mint), and the caller is gated on the SPIFFE SAN
    # of the certificate below.
    p.add_argument("--iam-grpc", default=IAM_INTERNAL_GRPC,
                   help="kaname internal gRPC host:port (MintBootstrapToken)")
    p.add_argument("--mtls-cert", default=BOOTSTRAP_MINT_MTLS_CERT,
                   help="bootstrap-operator client certificate (allow-listed SPIFFE SAN)")
    p.add_argument("--mtls-key", default=BOOTSTRAP_MINT_MTLS_KEY,
                   help="bootstrap-operator client key")
    p.add_argument("--base-url", default="http://localhost:18080",
                   help="api-gateway public (UserTokenService/SAKeyService)")
    p.add_argument("--api-audience", default="https://api.kacho.cloud",
                   help="requested token audience (gateway ExpectedAudience)")
    p.add_argument("--mode", choices=["bootstrap", "bootstrap-verify", "user", "sa"], required=True,
                   help="bootstrap — напечатать удостоверение; bootstrap-verify — "
                        "напечатать И утвердить, что край его принял")
    p.add_argument("--subject", help="user_id (user) or sva_id (sa)")
    p.add_argument("--created-by", help="created_by_user_id for Issue")
    # `--ttl-seconds` removed with the request field it fed: the issuer owns the
    # lifetime, and a flag that changes nothing is a promise nobody keeps.
    args = p.parse_args()

    mint_kwargs = {"grpc_addr": args.iam_grpc, "cert": args.mtls_cert, "key": args.mtls_key}
    if args.mode == "bootstrap":
        print(mint_bootstrap(**mint_kwargs))
        return 0
    if args.mode == "bootstrap-verify":
        tok = mint_bootstrap(**mint_kwargs)
        assert_bootstrap_accepted_by_the_edge(args.base_url, tok)
        print(tok)
        return 0

    admin = mint_bootstrap(**mint_kwargs)
    created_by = args.created_by or args.subject
    if args.mode == "user":
        # Персональный токен обменивается у НАШЕГО издателя, и другого пути у него
        # нет: клиента у внешнего поставщика выпуск не заводит (#1121).
        print(user_platform_token(args.base_url, admin, args.subject, created_by,
                                  PLATFORM_TOKEN_URL, args.api_audience))
    else:
        # Ключ служебной учётки обменивается У НАШЕГО издателя и только у него
        # (задача #1120): зеркала клиента у поставщика больше не заводится.
        # Ручки прежней полосы сняты вместе с ней: пока они разбирались, но не
        # читались ни одной веткой, командная строка обещала выбор, которого нет.
        # Теперь такой ввод ОТВЕРГАЕТСЯ разбором аргументов, а не проглатывается.
        print(sa_platform_token(args.base_url, admin, args.subject, created_by,
                                PLATFORM_TOKEN_URL, args.api_audience))
    return 0


if __name__ == "__main__":
    sys.exit(main())
