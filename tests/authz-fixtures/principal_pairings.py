#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Which emitted id is the principal of which emitted Bearer — declared, then checked.

WHY THIS IS ITS OWN MODULE. `prodseed_matrix` mints the bootstrap Bearer at IMPORT time,
so importing it dials the stand. A self-test that cannot be run without a cluster is a
self-test nobody runs, and these functions are exactly the part that must be provable
offline. Nothing here touches a network, a file or a clock.

WHAT IT IS FOR. A suite that binds a role to `{{<id>}}` and then reads with
`{{<token>}}` depends on the two naming the SAME principal. That is the one fixture
property such a suite cannot check for itself: from inside the case, a mismatch is
indistinguishable from a grant that has not materialised yet, so it presents as a
timeout — in the wrong place, with the wrong explanation, six steps later.

It is not hypothetical. One channel's subject id was discarded at creation, and the
nearest-looking id in the env — an unrelated user row — got bound instead. The relation
then named a `user` while every request authenticated as a `service_account`, so it could
not resolve at any budget. The invariant had been written down for the neighbouring
channel, as a comment, and honoured there; what was missing was making it DATA, so that
honouring it is checked rather than remembered.
"""
from __future__ import annotations

import base64
import json

# `<id-key>: <token-key>` — the id-key must be the id of the principal the token-key
# authenticates as.
#
# NOT EVERY EMITTED ID BELONGS HERE, AND THE ABSENCE IS MEANINGFUL. `userAAAId` /
# `userAABId` / `userNOBId` / `userINVId` / `userPA1Id` / `userPureNoBindingsId` are
# BINDING TARGETS only — real user rows so the subject-exists trigger resolves. No
# emitted token authenticates as any of them, and none can: a machine harness obtains
# `client_credentials`, i.e. a service account (mint_rs256 says why — a user token needs
# an interactive login to carry `acr`, and the issued one is scoped to an internal
# audience the edge does not accept). Pairing one of them with a `jwt*` key is the defect
# described above, not a gap in this table.
# Имя утверждения о принципале — ОДНО объявление на модуль. Три литерала в
# трёх местах разошлись бы молча, и разошлись бы там, где это не видно.
_PRINCIPAL_CLAIM = "kacho_principal_id"

PRINCIPAL_PAIRINGS = {
    "svaAId": "jwtSAA",
    "svaInviteeId": "jwtInvitee",
    "svaNoGrantId": "jwtSANoGrant",
    "svaPureNoGrantId": "jwtPureNoBindings",
    "svaStorageCreateListOnlyId": "jwtStorageCreateListOnlyA",
}


def token_principal_id(token: str) -> str:
    """Principal id a Bearer authenticates as, or "" if it carries none.

    READS THE SAME SHAPES THE EDGE READS, IN THE SAME ORDER — top level first,
    then the nested `ext_claims` map. Not a nicety: this function exists to
    predict what the edge will make of a token, so any shape the edge honours
    and this one does not turns a SOUND channel into a seed abort, and the abort
    lands on the whole run (measured: run 32669585825, four shards, 0 of 18
    collections executed, five sound service-account channels condemned).

    WHY BOTH SHAPES ARE REAL, i.e. why this is not defensive coding. The platform
    issues principal Bearers by two lanes, and they place the enrichment claims
    differently BY CONSTRUCTION:

      * our own issuer signs them FLAT at the top level — `tokensigner.Sign`
        copies the composed claim map straight into the payload, which is the
        placement the product's own test asserts
        (`TestClaimsComeFromTheSingleDeclarationAndCarryTheClientIdentifier`);
      * the external provider nests them, because the only place in the tree that
        wraps the map under `ext_claims` is the provider's token/refresh hook
        handler, and the provider then carries that map as its own `ext` claim.

    The edge already treats the two as equivalent (`verifiedClaim` in
    gateway/internal/middleware/auth.go: "preferring the top-level claim, then the
    nested ext_claims map"). This function is the harness-side mirror of that rule.

    Deliberately NOT `sub`: on a client_credentials token that is the OAuth client
    id and equals no kacho id at all, so a comparison against it reports EVERY
    pairing broken, the sound ones included. (Measured: the first version of this
    check did exactly that and would have condemned a channel that was correct.)
    """
    if not isinstance(token, str) or token.count(".") < 2:
        return ""
    try:
        payload = token.split(".")[1]
        payload += "=" * (-len(payload) % 4)
        claims = json.loads(base64.urlsafe_b64decode(payload))
    except Exception:
        return ""
    if not isinstance(claims, dict):
        return ""
    # 1. Top level — where OUR issuer states it, and what the edge prefers.
    got = claims.get(_PRINCIPAL_CLAIM, "")
    if isinstance(got, str) and got:
        return got
    # 2. Nested `ext_claims`, either bare or under the provider's `ext` envelope.
    ext = claims.get("ext_claims")
    if not isinstance(ext, dict):
        outer = claims.get("ext")
        ext = outer.get("ext_claims") if isinstance(outer, dict) else None
    if not isinstance(ext, dict):
        return ""
    got = ext.get(_PRINCIPAL_CLAIM, "")
    return got if isinstance(got, str) else ""


def unpaired_principals(fixtures: dict, pairings: dict | None = None) -> list[str]:
    """Declared pairings that do NOT hold — one human-readable line each.

    A declared pair whose BOTH keys are absent is not a finding: that profile simply did
    not emit the channel. A pair with one side present and the other missing IS one —
    half a channel is precisely how the original defect looked from outside.
    """
    table = PRINCIPAL_PAIRINGS if pairings is None else pairings
    broken: list[str] = []
    for id_key, tok_key in table.items():
        have_id, have_tok = id_key in fixtures, tok_key in fixtures
        if not have_id and not have_tok:
            continue
        if not have_id or not have_tok:
            broken.append(
                f"{id_key} ↔ {tok_key}: only {id_key if have_id else tok_key} was emitted")
            continue
        want, got = fixtures[id_key], token_principal_id(fixtures[tok_key])
        if not got:
            broken.append(
                f"{id_key} ↔ {tok_key}: the token carries no principal id (expected {want})")
        elif got != want:
            broken.append(
                f"{id_key} ↔ {tok_key}: bound subject is {want}, "
                f"but the token authenticates as {got}")
    return broken


def make_token(principal_id: str, *, nest: bool = False) -> str:
    """Unsigned JWS-shaped string carrying `principal_id` — for self-tests ONLY.

    Signature segment is the literal `not-a-signature`, which no verifier accepts. The
    point is to keep test fixtures visibly NOT credentials: a realistic-looking token in
    a fixture is how a substitution starts reading like a correct hand-off.
    """
    inner = {_PRINCIPAL_CLAIM: principal_id, "kacho_principal_type": "service_account"}
    claims = {"ext": {"ext_claims": inner}} if nest else {"ext_claims": inner}
    body = base64.urlsafe_b64encode(json.dumps(claims).encode()).decode().rstrip("=")
    return "eyJhbGciOiJub25lIn0." + body + ".not-a-signature"
