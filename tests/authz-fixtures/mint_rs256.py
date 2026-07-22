#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Production-mode RS256 token minter for the newman authz seed (Phase C, #59).

Production authN (api-gateway `authn.mode=production-strict`) accepts ONLY
Hydra-signed RS256 Bearers with `aud=https://{API_DOMAIN}` — the HS256 dev-secret
JWTs `setup-jwt.py` mints are inert (401). This module produces real RS256 tokens
through the SAME machinery the platform uses, no dev-bypass, no direct Hydra-admin:

  1. bootstrap admin  — InternalBootstrapTokenService.MintBootstrapToken (gateway
     internal :8081). A cluster `system_admin` ServiceAccount Bearer (acr-EXEMPT),
     the entry point that seeds everything else.
  2. per-subject      — with the admin Bearer, UserTokenService.Issue /
     SAKeyService.Issue provision a per-principal Hydra OAuth client and hand back
     an ES256 (P-256) private key ONCE. We sign a private_key_jwt `client_assertion`
     (RFC 7521/7523) with it and run the OAuth2 client_credentials exchange at Hydra
     (`aud=https://{API_DOMAIN}`) → a per-subject RS256 token whose `kacho_principal_*`
     claims (token-hook enrichment) resolve to that subject's User/SA + its bindings.

Hydra remains the issuer/signer throughout; we only broker exchanges. Requires
PyJWT + cryptography (ES256 signing). Usable as a library (import) or a CLI.

STATUS (Phase C, #59):
  - `mint_bootstrap` (bootstrap admin) — PROVEN end-to-end: MintBootstrapToken →
    RS256 → api-gateway GET /iam/v1/accounts = 200 (IBT-04). This is the working
    primitive the deploy-config fixes on redesign/integration unblocked.
  - `user_rs256` / `sa_rs256` (per-subject) — the OAuth flow (Issue → sign
    client_assertion → client_credentials exchange) is correct and reaches Hydra,
    but is BLOCKED at iam: UserTokenService.Issue / SAKeyService.Issue force
    created_by = the CALLER principal, and the bootstrap caller is a ServiceAccount,
    whose id is not a users(id) row → created_by FK (23503) → the Issue Operation
    ends code 9. There is no non-interactive admin path to mint a token FOR another
    principal. Tracked as a product gap in PRO-Robotech/kacho#60; the per-subject
    functions here are ready to drive the seed the moment that lands.
"""
from __future__ import annotations

import argparse
import json
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

import jwt as pyjwt


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
def mint_bootstrap(internal_base_url: str, ttl_seconds: int = 3600) -> str:
    """MintBootstrapToken → cluster system_admin SA RS256 Bearer (acr-exempt)."""
    url = internal_base_url.rstrip("/") + "/iam/v1/internal/bootstrapToken:mint"
    code, body = _post_json(url, {"ttlSeconds": ttl_seconds})
    if code != 200 or "accessToken" not in body:
        raise RuntimeError(f"MintBootstrapToken failed ({code}): {body}")
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
    p.add_argument("--internal-base-url", default="http://localhost:18081",
                   help="api-gateway internal-rest (MintBootstrapToken)")
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

    if args.mode == "bootstrap":
        print(mint_bootstrap(args.internal_base_url, args.ttl_seconds))
        return 0

    admin = mint_bootstrap(args.internal_base_url, args.ttl_seconds)
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
