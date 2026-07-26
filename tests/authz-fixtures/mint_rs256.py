#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Production-mode RS256 token minter for the newman authz seed (Phase C, #59).

Production authN (api-gateway `authn.mode=production-strict`) accepts ONLY
Hydra-signed RS256 Bearers with `aud=https://{API_DOMAIN}` — the HS256 dev-secret
JWTs `setup-jwt.py` mints are inert (401). This module produces real RS256 tokens
through the SAME machinery the platform uses, no dev-bypass, no direct Hydra-admin:

  1. bootstrap admin  — InternalBootstrapTokenService.MintBootstrapToken, called by
     a DIRECT mTLS gRPC dial to kacho-iam :9091 (there is no REST route — see
     `mint_bootstrap`). A cluster `system_admin` ServiceAccount Bearer (acr-EXEMPT),
     the entry point that seeds everything else.
  2. per-subject      — with the admin Bearer, UserTokenService.Issue /
     SAKeyService.Issue provision a per-principal Hydra OAuth client and hand back
     an ES256 (P-256) private key ONCE. We sign a private_key_jwt `client_assertion`
     (RFC 7521/7523) with it and run the OAuth2 client_credentials exchange at Hydra
     (`aud=https://{API_DOMAIN}`) → a per-subject RS256 token whose `kacho_principal_*`
     claims (token-hook enrichment) resolve to that subject's User/SA + its bindings.

Hydra remains the issuer/signer throughout; we only broker exchanges. Requires
PyJWT + cryptography (ES256 signing). Usable as a library (import) or a CLI.

STATUS (Phase C, #59) — UNBLOCKED + PROVEN end-to-end:
  - `mint_bootstrap` (bootstrap admin) — PROVEN: MintBootstrapToken → RS256 →
    api-gateway GET /iam/v1/accounts = 200 (IBT-04). Since the #58 hardening the
    call is gRPC-over-mTLS to iam :9091 with the bootstrap-operator client cert
    (the gateway REST route is gone) — the token it returns is unchanged.
  - `sa_rs256` (per-subject SA) — PROVEN end-to-end against the production-strict
    stand: SAKeyService.Issue (WITH `audience:[https://api.kacho.cloud]` — the
    SA-key resolveAudience honours caller audiences, unlike user-tokens) → sign
    client_assertion → client_credentials exchange → RS256 SA token whose
    token-hook `kacho_principal_type=service_account` enrichment makes it
    acr-EXEMPT (stepup_gate O-1) and reachable on acr=1 resource RPCs. The
    created_by FK blocker (#60) is fixed for SA-keys: an SA-principal caller
    records created_by = the target SA's account owner (see sa_keys handler/
    usecase). The whole vpc `network` newman collection runs GREEN with an
    RS256-SA seed under production-strict (see prodseed_network.py).
  - `user_rs256` (per-subject USER) — DOES NOT authenticate resource RPCs in
    production-strict: a user client_credentials token carries no `acr` (fails the
    acr>=1 floor — user principals are NOT acr-exempt) AND UserTokenService.Issue
    hardcodes the kacho-internal audience (resolveAudience ignores caller audience)
    so its `aud` never matches the gateway ExpectedAudience. User tokens with `acr`
    require interactive OIDC login (Kratos→Hydra) — the "production-user-gated"
    class (#59 follow-up). Machine e2e is SA by nature; use `sa_rs256`.
"""
from __future__ import annotations

import argparse
import base64
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

import jwt as pyjwt


# ── bootstrap-mint transport (direct mTLS gRPC to kacho-iam :9091) ──────────
# MintBootstrapToken has NO REST route — the mint is reachable ONLY over mTLS
# gRPC, and only from a client certificate whose SPIFFE SAN kacho-iam allow-lists
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
# A SECOND, DIFFERENT identity from the bootstrap-operator above. kacho-iam :9091
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


def _post_form(url: str, form: dict, timeout: int = 15) -> tuple[int, dict]:
    data = urllib.parse.urlencode(form).encode()
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
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

    kacho-iam's internal listener requires a verified client certificate in every
    posture (security.md: internal is NOT exempt), so every grpcurl the seed makes
    must present one. This is the identity for the ordinary internal RPCs — NOT for
    the bootstrap-token mint, which admits only the dedicated operator SAN.
    """
    return ensure_client_cert(GATEWAY_CLIENT_SECRET, IAM_INTERNAL_MTLS_CERT, IAM_INTERNAL_MTLS_KEY)


def _refresh_operator_cert(cert_path: str, key_path: str) -> None:
    """Back-compat shim — bootstrap-operator flavour of ensure_client_cert."""
    ensure_client_cert(BOOTSTRAP_OPERATOR_SECRET, cert_path, key_path)


def mint_bootstrap(ttl_seconds: int = 3600, *, grpc_addr: str | None = None,
                   cert: str | None = None, key: str | None = None) -> str:
    """MintBootstrapToken → cluster system_admin SA RS256 Bearer (acr-exempt).

    Transport is a DIRECT mTLS gRPC dial to kacho-iam :9091 — the mint has no REST
    route anywhere (#58 hardening). It mints a cluster `system_admin` Bearer, so it
    cannot be gated by a ReBAC relation (it exists to obtain the FIRST token) and
    must not be gated by network position; the gate is the CALLER'S CERTIFICATE:
    kacho-iam admits only the SPIFFE SANs in `authn.bootstrap-mint.allowed-client-sans`
    (deny-all by default, in every mode). We therefore present the dedicated
    bootstrap-operator client cert, NOT the api-gateway one — the gateway is
    deliberately not a minter.

    `-insecure` skips SERVER-cert verification only (the port-forward changes the
    hostname); the CLIENT cert is still presented and verified by iam — same shape
    as setup.sh's grpcurl calls.

    Overrides: BOOTSTRAP_MINT_MTLS_CERT / _KEY (paths), IAM_INTERNAL_GRPC (addr),
    BOOTSTRAP_OPERATOR_SECRET + KACHO_NAMESPACE (where to pull the material from).
    """
    if not isinstance(ttl_seconds, int):
        # Guard the signature change: this used to take the gateway internal REST
        # base-URL first. A stale positional caller must fail LOUDLY here rather
        # than shipping a URL into the ttl field.
        raise TypeError(
            f"mint_bootstrap(ttl_seconds=int, *, grpc_addr, cert, key) — got {ttl_seconds!r}. "
            "The gateway REST route is gone; the mint is a direct mTLS gRPC call to iam :9091.")

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
            "-d", json.dumps({"ttlSeconds": ttl_seconds}), addr,
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


def sign_client_assertion(client_id: str, private_key_pem: str, key_id: str,
                          assertion_audience: str, ttl_s: int = 120) -> str:
    now = int(time.time())
    claims = {
        "iss": client_id,
        "sub": client_id,
        "aud": assertion_audience,
        "iat": now,
        "exp": now + ttl_s,
        "jti": uuid.uuid4().hex,
    }
    return pyjwt.encode(claims, private_key_pem, algorithm="ES256", headers={"kid": key_id})


def exchange(hydra_token_url: str, assertion: str, api_audience: str, scope: str = "") -> str:
    form = {
        "grant_type": "client_credentials",
        "client_assertion_type": "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
        "client_assertion": assertion,
        "audience": api_audience,
    }
    if scope:
        form["scope"] = scope
    code, body = _post_form(hydra_token_url, form)
    if code != 200 or "access_token" not in body:
        raise RuntimeError(f"Hydra client_credentials exchange failed ({code}): {body}")
    return body["access_token"]


# ── Composed one-shot helpers ───────────────────────────────────────────────
def user_rs256(base_url: str, admin_token: str, user_id: str, created_by_user_id: str,
               hydra_token_url: str, assertion_audience: str, api_audience: str) -> str:
    resp = issue_user_oauth(base_url, admin_token, user_id, created_by_user_id)
    cid, key, kid = _extract_oauth(resp)
    assertion = sign_client_assertion(cid, key, kid, assertion_audience)
    return exchange(hydra_token_url, assertion, api_audience)


def sa_rs256(base_url: str, admin_token: str, sva_id: str, created_by_user_id: str,
             hydra_token_url: str, assertion_audience: str, api_audience: str) -> str:
    resp = issue_sa_oauth(base_url, admin_token, sva_id, created_by_user_id)
    cid, key, kid = _extract_oauth(resp)
    assertion = sign_client_assertion(cid, key, kid, assertion_audience)
    return exchange(hydra_token_url, assertion, api_audience)


# ── CLI ─────────────────────────────────────────────────────────────────────
def main() -> int:
    p = argparse.ArgumentParser(description="Production-mode RS256 token minter (#59)")
    # MintBootstrapToken transport — direct mTLS gRPC to kacho-iam :9091. There is
    # NO gateway REST route (a route on the plain-HTTP internal listener would be a
    # credential-free cluster-admin mint), and the caller is gated on the SPIFFE SAN
    # of the certificate below.
    p.add_argument("--iam-grpc", default=IAM_INTERNAL_GRPC,
                   help="kacho-iam internal gRPC host:port (MintBootstrapToken)")
    p.add_argument("--mtls-cert", default=BOOTSTRAP_MINT_MTLS_CERT,
                   help="bootstrap-operator client certificate (allow-listed SPIFFE SAN)")
    p.add_argument("--mtls-key", default=BOOTSTRAP_MINT_MTLS_KEY,
                   help="bootstrap-operator client key")
    p.add_argument("--base-url", default="http://localhost:18080",
                   help="api-gateway public (UserTokenService/SAKeyService)")
    p.add_argument("--hydra-token-url", default="http://localhost:14444/oauth2/token",
                   help="Hydra public token endpoint POST target (in-cluster / port-forward)")
    p.add_argument("--assertion-audience",
                   default="http://localhost:28080/.ory/hydra/public/oauth2/token",
                   help="client_assertion aud (Hydra self.issuer token endpoint)")
    p.add_argument("--api-audience", default="https://api.kacho.cloud",
                   help="requested token audience (gateway ExpectedAudience)")
    p.add_argument("--mode", choices=["bootstrap", "user", "sa"], required=True)
    p.add_argument("--subject", help="user_id (user) or sva_id (sa)")
    p.add_argument("--created-by", help="created_by_user_id for Issue")
    p.add_argument("--ttl-seconds", type=int, default=3600)
    args = p.parse_args()

    mint_kwargs = {"grpc_addr": args.iam_grpc, "cert": args.mtls_cert, "key": args.mtls_key}
    if args.mode == "bootstrap":
        print(mint_bootstrap(args.ttl_seconds, **mint_kwargs))
        return 0

    admin = mint_bootstrap(args.ttl_seconds, **mint_kwargs)
    created_by = args.created_by or args.subject
    if args.mode == "user":
        print(user_rs256(args.base_url, admin, args.subject, created_by,
                         args.hydra_token_url, args.assertion_audience, args.api_audience))
    else:
        print(sa_rs256(args.base_url, admin, args.subject, created_by,
                       args.hydra_token_url, args.assertion_audience, args.api_audience))
    return 0


if __name__ == "__main__":
    sys.exit(main())
