#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Production-posture seed for EVERY newman suite — the counterpart of setup.sh.

HISTORY, in the past tense on purpose. `setup.sh` USED TO seed a stand whose api-gateway
ran `authn.mode=dev`: it forged HS256 Bearers from a signing literal shared with the tree
and handed one to each matrix subject. Against PRODUCTION posture that was inert by
design — the gateway accepts only Hydra-signed RS256, and iam's internal listener demands
a verified client certificate — so it died on its third step (Account.Create → 401) and
the whole regression suite proved nothing about the posture we actually ship.

That region no longer exists. `setup.sh` is now a posture classifier that refuses `dev`
outright and ends in an unconditional `exec` of THIS module; the symmetric-minting path
and its script were deleted, and the deletion is held by
`internal/repohygiene/sharedsigningliteral_test.go` (with a paired injection proof).
The paragraph above is kept because it names what the delegation below is FOR — not
because any of it is still reachable. Do not read it as a description of the harness.

This module is the production path setup.sh delegates to. Nothing is forged here:

  * the admin Bearer comes from iam `InternalBootstrapTokenService.MintBootstrapToken`,
    reached by a direct mTLS gRPC dial to kaname :9091 with the dedicated
    bootstrap-operator client certificate (the mint has no REST route, and the caller's
    SPIFFE SAN IS the credential);
  * every subject Bearer comes from iam `SAKeyService.Issue` — iam provisions the Hydra
    OAuth client and returns an ES256 private key ONCE; we sign a private_key_jwt
    `client_assertion` with it and run the standard OAuth2 client_credentials exchange.
    That last hop is the one sanctioned direct-Hydra call (RFC 7521/7523 client flow);
    issuance, client lifecycle and JWKS all stay behind the iam facade.

WHY EVERY SUBJECT IS A ServiceAccount, not a User. Two independent product facts, both
verified in the tree rather than assumed:
  1. `IssueUserTokenUseCase.resolveAudience` takes NO caller audience — it always emits
     the kacho-internal `<prefix>/user/<id>`, which can never equal the gateway's
     ExpectedAudience (`https://<APIDomain>`);
  2. a client_credentials token carries no `acr`, and 236 of the 294 catalogued RPCs
     declare `required_acr_min` ≥ 1 (remeasured; this line read "292 of the 357"
     against a catalog two retirements ago — recheck it rather than quoting it, the
     command is in prodseed_matrix.py). `StepUpGate.Check` exempts principals whose
     `kacho_principal_type` is exactly `service_account` and NEVER a user.
A human User principal with an `acr` requires the interactive Kratos→Hydra login, which
a machine harness cannot drive. So each matrix slot is backed by a ServiceAccount
carrying the exact bindings that slot assumes — the FGA relation resolved is identical,
only the principal class differs.

Deliberately NOT minted (left at whatever the env already carries, so their cases fail
loudly instead of being faked): `jwtAccountAdminAStepUp` and the static `apiToken*`
family. Those need a real step-up/interactive credential — see the report.
"""
from __future__ import annotations

import argparse
import base64
import json
import os
import pathlib
import shutil
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request

HERE = pathlib.Path(__file__).resolve().parent
REPO = HERE.parent.parent
sys.path.insert(0, str(HERE))

# Suites whose newman env this seed patches. Order is irrelevant (each patch is a
# key-merge into its own file). Kept in step with deploy/scripts/newman-parallel.sh's
# SERVICES list — a suite missing here silently keeps its committed placeholder tokens
# and reds wholesale under production authN.
ALL_SERVICES = ("iam", "vpc", "compute", "nlb", "storage", "registry", "geo", "api-gateway")

NS = os.environ.get("KACHO_NAMESPACE", os.environ.get("SETUP_NS", "kacho"))
HYDRA_PF_PORT = int(os.environ.get("HYDRA_PUBLIC_PORT", "14444"))
HYDRA_SVC = os.environ.get("HYDRA_PUBLIC_SVC", "kacho-umbrella-hydra-public")


def log(msg: str) -> None:
    print(f"[prodseed] {msg}", file=sys.stderr, flush=True)


# ── prerequisites: client certificates + the Hydra token-endpoint forward ────
def ensure_certs() -> None:
    """Provision BOTH client identities the seed needs on the internal port.

    kaname :9091 requires a verified client certificate in EVERY posture
    (security.md: «Internal (:9091) НЕ освобождён»), so a seed that dials it must carry
    one. Two DISTINCT leaves, deliberately not interchangeable:

      * `kacho-bootstrap-operator-client-tls` — the ONLY SAN iam allow-lists for
        MintBootstrapToken. The gateway identity is deliberately excluded: the gateway
        fronts tenant traffic, so "is the api-gateway" must never be a licence to mint
        a cluster admin.
      * `api-gateway-client-tls` — the identity for the ordinary internal RPCs the seed
        drives (InternalUserService.UpsertFromIdentity, InternalIAMService.LookupSubject).

    Material is copied out of the cluster's own Secrets to 0600 files under /tmp for the
    duration of the run. Nothing is generated, nothing is written into the repository,
    and both are re-extracted every run: cert-manager regenerates the internal CA on a
    fresh `dev-up`, so a leaf left over from a previous stand is signed by the OLD CA and
    the handshake fails permanently (a retry only repeats it).
    """
    import mint_rs256 as m

    ok_op = m.ensure_client_cert(m.BOOTSTRAP_OPERATOR_SECRET,
                                 m.BOOTSTRAP_MINT_MTLS_CERT, m.BOOTSTRAP_MINT_MTLS_KEY,
                                 namespace=NS)
    ok_gw = m.ensure_client_cert(m.GATEWAY_CLIENT_SECRET,
                                 m.IAM_INTERNAL_MTLS_CERT, m.IAM_INTERNAL_MTLS_KEY,
                                 namespace=NS)
    if not ok_op:
        raise SystemExit(
            f"[prodseed] FATAL: no bootstrap-operator client certificate. MintBootstrapToken "
            f"is gated on the caller's certificate; expected secret "
            f"'{m.BOOTSTRAP_OPERATOR_SECRET}' in ns/{NS} (the umbrella issues it when "
            f"mtls.bootstrapOperator is enabled) or BOOTSTRAP_MINT_MTLS_CERT/_KEY pointing "
            f"at pre-extracted material.")
    if not ok_gw:
        raise SystemExit(
            f"[prodseed] FATAL: no client certificate for kaname :9091. Expected secret "
            f"'{m.GATEWAY_CLIENT_SECRET}' in ns/{NS} or IAM_INTERNAL_GRPC_MTLS_CERT/_KEY.")
    # Корень внутреннего удостоверяющего — для обмена у НАШЕГО издателя (#1014).
    # Материал ПУБЛИЧНЫЙ и нужен ровно затем, чтобы проверка подписи сертификата
    # слушателя выдачи осталась на месте: снять её было бы послаблением, а не
    # обходом несовпадающего имени (SAN листа — имена служб, проброс идёт на петлю).
    ok_ca = m.ensure_iam_server_ca(namespace=NS)
    if not ok_ca:
        raise SystemExit(
            f"[prodseed] FATAL: нет корня внутреннего удостоверяющего. Обмен у нашего "
            f"издателя (POST /iam/v1/token) идёт по TLS, и проверять подпись листа нечем; "
            f"ожидался секрет '{m.IAM_SERVER_SECRET}' в ns/{NS} либо IAM_SERVER_CA_FILE, "
            f"указывающий на заранее извлечённый корень.")
    log(f"client certs ready: operator={m.BOOTSTRAP_MINT_MTLS_CERT} gateway={m.IAM_INTERNAL_MTLS_CERT} "
        f"iam-ca={m.IAM_SERVER_CA_FILE}")


def _hydra_serves(port: int) -> bool:
    """Does Hydra actually ANSWER on this port — not merely: is the port bound?

    A `kubectl port-forward` left over from before a rollout keeps its listening socket
    but its backend connection is gone: `connect()` succeeds and the first request dies
    `[Errno 111] Connection refused`. A TCP-level probe calls that healthy and the seed
    then fails 8 minutes later, mid-token-exchange, looking like an auth defect (observed
    exactly once, right after the production helm upgrade re-rolled Hydra). Ask the
    endpoint a question instead: the discovery document is unauthenticated and cheap.
    """
    try:
        with urllib.request.urlopen(
                f"http://127.0.0.1:{port}/.well-known/openid-configuration", timeout=3) as r:
            return r.status == 200
    except (urllib.error.URLError, OSError, ValueError):
        return False


def ensure_hydra_forward() -> subprocess.Popen | None:
    """Make Hydra's public token endpoint reachable for the client_credentials exchange.

    Hydra public is a ClusterIP Service with no ingress route on this stand, so the final
    OAuth2 hop needs a forward. Idempotent: a forward that genuinely serves is reused and
    None is returned, so the caller never kills a forward it does not own.
    """
    if _hydra_serves(HYDRA_PF_PORT):
        log(f"hydra token endpoint answers on :{HYDRA_PF_PORT} (reusing existing forward)")
        return None
    if not shutil.which("kubectl"):
        raise SystemExit("[prodseed] FATAL: kubectl not found and Hydra public is not forwarded")
    if _port_is_bound(HYDRA_PF_PORT):
        raise SystemExit(
            f"[prodseed] FATAL: :{HYDRA_PF_PORT} is bound but Hydra does not answer on it — "
            f"a stale port-forward (its backend pod was re-rolled). Kill it and re-run; a "
            f"seed started against it dies mid token-exchange and reads like an auth defect.")
    log(f"port-forward {HYDRA_SVC} :{HYDRA_PF_PORT} → 4444")
    proc = subprocess.Popen(
        ["kubectl", "-n", NS, "port-forward", f"svc/{HYDRA_SVC}", f"{HYDRA_PF_PORT}:4444"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(40):
        if _hydra_serves(HYDRA_PF_PORT):
            return proc
        time.sleep(0.5)
    proc.terminate()
    raise SystemExit(f"[prodseed] FATAL: Hydra did not answer on :{HYDRA_PF_PORT}")


def _port_is_bound(port: int) -> bool:
    with socket.socket() as s:
        s.settimeout(1.0)
        return s.connect_ex(("127.0.0.1", port)) == 0


# ── env patching ────────────────────────────────────────────────────────────
def env_path(svc: str) -> pathlib.Path:
    # The gateway's own suite does not live under services/ — mirrors suite_dir() in
    # deploy/scripts/newman-parallel.sh.
    root = REPO / "gateway" if svc == "api-gateway" else REPO / "services" / svc
    return root / "tests" / "newman" / "environments" / "local.postman_environment.json"


def patch(fixtures: dict, paths: list[pathlib.Path]) -> None:
    """Merge fixtures into newman envs, DROPPING empty values.

    An empty value means a sub-seed did not produce its id. Writing it would blank a
    committed placeholder and turn a partial seed into a suite-wide cascade of
    "invalid resource id" — the failure then reads like a product bug.
    """
    nonempty = {k: v for k, v in fixtures.items() if v not in (None, "")}
    dropped = sorted(set(fixtures) - set(nonempty))
    if dropped:
        log(f"    not emitted (left as-is in env): {', '.join(dropped)}")
    tmp = HERE / "out" / "_patch.json"
    tmp.parent.mkdir(parents=True, exist_ok=True)
    tmp.write_text(json.dumps(nonempty, indent=2))

    # СВЕЖЕЕ ДЕРЕВО НЕ СОДЕРЖИТ ЭТИХ ФАЙЛОВ, И ЭТО НАМЕРЕННО: конкретное окружение
    # newman'а хранит выданные предъявители, поэтому оно вне git. В git рядом лежит
    # ШАБЛОН — из него окружение и разворачивается.
    #
    # Пока разворачивания здесь не было, посев на свежем дереве падал так: список
    # существующих файлов оказывался ПУСТ, `patch-env.py` получал ноль путей и отвечал
    # СВОЕЙ СПРАВКОЙ ПО ИСПОЛЬЗОВАНИЮ с кодом 2. То есть посев сообщал «утилита вызвана
    # неверно» там, где на деле «окружение ещё не развёрнуто». Страж прогонщика при этом
    # честно объявлял прогон недействительным — но причина в его выводе была подменена, и
    # читатель шёл разбираться с аргументами вызова.
    ready: list[pathlib.Path] = []
    created = 0
    for p in paths:
        if not p.exists():
            template = p.with_name(p.name.replace(".json", ".template.json"))
            if not template.exists():
                continue
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(template.read_text())
            created += 1
        ready.append(p)
    if created:
        log(f"    развёрнуто из шаблона (свежее дерево): {created} окружений")

    # НОЛЬ ОКРУЖЕНИЙ — ЭТО ОТКАЗ, А НЕ ПУСТАЯ РАБОТА. Молча «пропатчить ничего» значит
    # отдать суитам окружение без единого выданного предъявителя, а такой прогон
    # отчитается сплошными отказами доступа, неотличимыми от продуктового регресса.
    if not ready:
        raise SystemExit(
            "посев: не найдено ни одного окружения newman и ни одного шаблона рядом "
            f"с ними (искали: {', '.join(str(p) for p in paths)}). Разворачивать нечего, "
            "а прогон против такого стенда результата не даёт.")

    subprocess.run([sys.executable, str(HERE / "patch-env.py"), str(tmp),
                    *[str(p) for p in ready]], check=True)


# ── what version control will actually do with the files we just wrote ──────
#
# This used to be a printed CLAIM ("the env files are git-tracked, do not `git add -A`").
# The claim was FALSE in this tree — .gitignore has carried a rule for both
# `services/*/tests/newman/environments/local.postman_environment.json` and the gateway's
# copy since they were untracked — so the message sent the next reader looking for these
# files in history, where they are not, and described a hazard that no longer had that
# shape. A statement about the repository is not a check; it goes stale the moment the
# repository changes and nothing notices.
#
# So it is MEASURED instead: git is asked about each path we actually wrote. That cannot
# drift, and it keeps the real hazard armed — if any of these ever becomes tracked, the
# next production seed writes a live Hydra-signed bearer into a file `git add -A` would
# commit, and this says so by name instead of by assumption.
def env_disposition(paths: list[pathlib.Path], root: pathlib.Path | None = None) -> dict:
    """Ask git how it treats each path: 'tracked' | 'ignored' | 'untracked-not-ignored'.

    'tracked' is the dangerous one. 'ignored' is the safe state this tree is in.
    'untracked-not-ignored' is nearly as dangerous — `git add -A` would pick it up.
    Paths git cannot answer about (no repository) come back as 'unknown'.
    """
    root = root or REPO
    out: dict[str, str] = {}
    for p in paths:
        arg = str(p)
        tracked = subprocess.run(["git", "-C", str(root), "ls-files", "--error-unmatch", arg],
                                 capture_output=True, text=True)
        if tracked.returncode == 0:
            out[arg] = "tracked"
            continue
        if "not a git repository" in (tracked.stderr or "").lower():
            out[arg] = "unknown"
            continue
        ignored = subprocess.run(["git", "-C", str(root), "check-ignore", "-q", arg],
                                 capture_output=True, text=True)
        out[arg] = "ignored" if ignored.returncode == 0 else "untracked-not-ignored"
    return out


def report_env_disposition(paths: list[pathlib.Path], root: pathlib.Path | None = None) -> int:
    """Print the measured disposition. Returns the number of committable paths."""
    disp = env_disposition(paths, root)
    committable = sorted(k for k, v in disp.items() if v in ("tracked", "untracked-not-ignored"))
    ignored = [k for k, v in disp.items() if v == "ignored"]
    unknown = [k for k, v in disp.items() if v == "unknown"]
    log(f"env files written: {len(disp)}  (ignored by git: {len(ignored)}, "
        f"committable: {len(committable)}, unknown: {len(unknown)})")
    if committable:
        log("WARNING: these now hold LIVE Hydra-signed bearers AND git would commit them:")
        for k in committable:
            log(f"           {k}  [{disp[k]}]")
        log("         commit by explicit path only — never `git add -A`.")
    return len(committable)


# ── per-service extension seeders ───────────────────────────────────────────
def run_ext(svc: str, project_id: str, project_cross_id: str) -> dict:
    """Run tests/authz-fixtures/prodseed_<svc>_ext.py, if one exists.

    The extension seeders provision resource dependencies and object-scope FGA tuples
    the public AccessBinding API cannot express. They read /tmp/matrix.json for the base
    matrix and honour PRODSEED_PROJECT_ID / PRODSEED_PROJECT_CROSS_ID so their resources
    land in the SAME project the suite env points at.
    """
    ext = HERE / f"prodseed_{svc}_ext.py"
    if not ext.exists():
        return {}
    log(f"    extension seeder: {ext.name}")
    env = dict(os.environ)
    env["PRODSEED_PROJECT_ID"] = project_id
    env["PRODSEED_PROJECT_CROSS_ID"] = project_cross_id
    proc = subprocess.run([sys.executable, str(ext)], capture_output=True, text=True, env=env)
    if proc.returncode != 0:
        # A failed extension is NOT fatal for the run: the base matrix is still valid and
        # the affected cases fail on their own missing fixture, which is honest. Surface
        # the reason loudly so it is not mistaken for a product defect.
        log(f"    WARN {ext.name} failed (rc={proc.returncode}): {proc.stderr.strip()[-400:]}")
        return {}
    try:
        return json.loads(proc.stdout or "{}")
    except json.JSONDecodeError:
        log(f"    WARN {ext.name} produced non-JSON output")
        return {}


_NLB_ID_KEYS = ("existingNetworkId", "existingSubnetId", "existingInstanceId",
                "existingNicId", "existingExternalAddressId", "existingAddressIPv6Id",
                "existingZoneId", "existingRegionId",
                # Адресный план сети едет ВМЕСТЕ с её идентификатором: набор режет
                # подсети внутри объявленного плана, а не выводит адрес сам. Ключ,
                # не названный здесь, посев напишет, а этот перенос молча выбросит —
                # и кейсы получат отказ фикстуры вместо адреса.
                "existingNetworkV4Plan", "existingNetworkV6Plan")


def seed_nlb_resources(boot: str, base_url: str, internal_url: str, project_id: str,
                       out_dir: pathlib.Path) -> dict:
    """Delegate the nlb fixture set to its own seeder, authenticated with the RS256 admin.

    deploy/scripts/seed-nlb-fixtures.sh is the SOLE author of the nlb-dedicated zone's
    AddressPool + subnet + instance + NIC set (it reclaims foreign-zone twins of its own
    pool name). Re-implementing any of it here would create a second author for the same
    cluster-wide default slot — the exact collision class the script's header documents.
    It only needs a Bearer, so it works unchanged in production once handed an RS256 one:
    the RS256 admin is BOTH the project grantor and the cluster-singleton admin here (the
    dev path splits them only because its project grantor is an account-scoped user).
    """
    script = REPO / "deploy" / "scripts" / "seed-nlb-fixtures.sh"
    if not script.exists():
        return {}
    log("    nlb fixture seeder (RS256 admin Bearer)")
    seeded = out_dir / "nlb-seeded-ids.env"
    env = dict(os.environ)
    env.update({"BASE_URL": base_url, "INTERNAL_BASE_URL": internal_url,
                "JWT": boot, "ADMIN_JWT": boot,
                "existingProjectId": project_id, "OUT_FILE": str(seeded),
                "NLB_ZONE_ID": os.environ.get("NLB_ZONE", "ru-central1-e")})
    log_path = out_dir / "nlb-seed.log"
    with log_path.open("w") as fh:
        proc = subprocess.run(["bash", str(script)], stdout=fh, stderr=subprocess.STDOUT, env=env)
    if proc.returncode != 0:
        log(f"    WARN seed-nlb-fixtures.sh rc={proc.returncode} — see {log_path}")
    if not seeded.exists():
        return {}
    got = {}
    for line in seeded.read_text().splitlines():
        if "=" not in line or line.startswith("#"):
            continue
        k, _, v = line.partition("=")
        if k in _NLB_ID_KEYS and v:
            # The nlb env names the BYO-VIP handle `existingAddressId`; the seeder
            # writes it as `existingExternalAddressId`.
            got["existingAddressId" if k == "existingExternalAddressId" else k] = v
    return got


def _self_test() -> int:
    """Injection in both directions for `env_disposition` — the measurement that
    replaced a printed claim about version control.

    The claim it replaced said the env files were tracked; the tree's .gitignore says
    otherwise, and nothing noticed for as long as the sentence existed. A measurement
    can only be trusted if it distinguishes the two states, so both are built here in a
    throwaway repository and asserted:

      * tracked / untracked-but-not-ignored → the hazard the warning is FOR (loud);
      * ignored                             → the state this tree is in (quiet).

    The census is asserted too: three paths in, three verdicts out. "No committable
    paths" must be distinguishable from "nothing was looked at".
    """
    import tempfile

    ok = True
    with tempfile.TemporaryDirectory() as td:
        root = pathlib.Path(td)
        subprocess.run(["git", "-C", td, "init", "-q"], check=True)
        subprocess.run(["git", "-C", td, "config", "user.email", "t@example.invalid"], check=True)
        subprocess.run(["git", "-C", td, "config", "user.name", "t"], check=True)
        (root / "envs").mkdir()
        for n in ("tracked.json", "ignored.json", "loose.json"):
            (root / "envs" / n).write_text("{}\n")
        (root / ".gitignore").write_text("envs/ignored.json\n")
        subprocess.run(["git", "-C", td, "add", ".gitignore", "envs/tracked.json"], check=True)
        subprocess.run(["git", "-C", td, "commit", "-qm", "fixture"], check=True)

        paths = [root / "envs" / n for n in ("tracked.json", "ignored.json", "loose.json")]
        disp = env_disposition(paths, root=root)
        want = {str(paths[0]): "tracked", str(paths[1]): "ignored",
                str(paths[2]): "untracked-not-ignored"}
        if disp != want:
            print(f"SELF-TEST FAIL: disposition {disp} != {want}", file=sys.stderr)
            ok = False
        # Census: every path handed in comes back with a verdict — "zero committable"
        # must not be reachable by looking at nothing.
        if len(disp) != len(paths):
            print(f"SELF-TEST FAIL: examined {len(disp)} of {len(paths)} paths", file=sys.stderr)
            ok = False
        n = report_env_disposition(paths, root=root)
        if n != 2:
            print(f"SELF-TEST FAIL: committable count {n} != 2", file=sys.stderr)
            ok = False
        # The quiet direction on its own: an ignored path alone must report nothing
        # committable — otherwise the warning fires always and stops being read.
        if report_env_disposition([paths[1]], root=root) != 0:
            print("SELF-TEST FAIL: an ignored path was reported as committable", file=sys.stderr)
            ok = False

    # ── principal pairings: injection in both directions ─────────────────────
    #
    # The seed refuses to emit a fixture set whose declared `<id> ↔ <token>` pairings do
    # not hold. That refusal is only worth anything if it can fire — and only SAFE if it
    # stays quiet on a sound set, since it aborts the whole run. Both directions are
    # asserted here, on synthetic tokens, with no stand involved.
    #
    # `principal_pairings` is imported directly (not through `prodseed_matrix`, which
    # mints the bootstrap Bearer at import time and would need a cluster to load).
    import principal_pairings as pp

    sound = {
        "svaAId": "sva_a", "jwtSAA": pp.make_token("sva_a"),
        # nested `ext.ext_claims` — the shape the provider's token hook actually emits;
        # both nestings must resolve, or the check condemns a sound channel.
        "svaInviteeId": "sva_inv", "jwtInvitee": pp.make_token("sva_inv", nest=True),
    }
    if pp.unpaired_principals(sound, {"svaAId": "jwtSAA", "svaInviteeId": "jwtInvitee"}):
        print("SELF-TEST FAIL: a SOUND pairing set was reported broken — this check "
              "aborts the seed, so a false positive stops every suite", file=sys.stderr)
        ok = False

    # The defect itself: the token authenticates as somebody else than the bound subject.
    # This is the shape that produced six cases' worth of "not found" after a full poll
    # budget, six steps away from its cause.
    mismatched = dict(sound, svaInviteeId="usr_someone_else")
    got = pp.unpaired_principals(mismatched, {"svaAId": "jwtSAA", "svaInviteeId": "jwtInvitee"})
    if len(got) != 1 or "usr_someone_else" not in got[0] or "sva_inv" not in got[0]:
        print(f"SELF-TEST FAIL: mismatch not caught, or the finding does not NAME both "
              f"the bound subject and the token's principal: {got}", file=sys.stderr)
        ok = False

    # Half a channel — one key emitted, the other not. This is how the original defect
    # looked from outside, so it must not read as "channel absent".
    if len(pp.unpaired_principals({"svaAId": "sva_a"}, {"svaAId": "jwtSAA"})) != 1:
        print("SELF-TEST FAIL: a half-emitted pairing was not reported", file=sys.stderr)
        ok = False
    # A channel this profile did not emit at all is NOT a finding — otherwise the check
    # fires on every partial seed and gets removed as noise.
    if pp.unpaired_principals({}, {"svaAId": "jwtSAA"}):
        print("SELF-TEST FAIL: an unemitted channel was reported as a breach", file=sys.stderr)
        ok = False

    # The claim-name half. Reading `sub` instead of `kacho_principal_id` reports EVERY
    # pairing broken, sound ones included — measured, not imagined: the first version of
    # this check did exactly that. A token whose principal claim is absent must be
    # reported as carrying none, never silently accepted.
    if pp.token_principal_id(pp.make_token("sva_a")) != "sva_a":
        print("SELF-TEST FAIL: the principal claim is not read from ext_claims", file=sys.stderr)
        ok = False
    for junk in ("", "not.a.token", "a.b", "eyJhbGciOiJub25lIn0.e30.x"):
        if pp.token_principal_id(junk) != "":
            print(f"SELF-TEST FAIL: {junk!r} yielded a principal id", file=sys.stderr)
            ok = False

    # ── THE PLACEMENT HALF: both issuing lanes, not just the provider's ──────
    #
    # WHY THIS DIRECTION EXISTS. Every assertion above uses `make_token`, which
    # nests — the shape the external provider emits. That made the whole self-test
    # blind to the lane the platform now issues on ITSELF, and the blindness was
    # not theoretical: it aborted four shards with zero of eighteen collections
    # executed, naming five SOUND service-account channels as breaches
    # (run 32669585825). A check that condemns the sound is worse than absent —
    # it stops the run and points at the wrong thing.
    #
    # WHY BOTH SHAPES ARE LEGITIMATE, i.e. why widening the reader is a fix and
    # not a relaxation: our own issuer signs the composed claims FLAT at the top
    # level (asserted by the product itself in
    # `TestClaimsComeFromTheSingleDeclarationAndCarryTheClientIdentifier`), the
    # provider nests them, and the EDGE already treats the two as equivalent —
    # top level first, then nested (`verifiedClaim`, gateway auth middleware).
    # This reader predicts what the edge will make of a token, so it must read
    # exactly what the edge reads.
    def _flat(pid, **extra):
        """A token of OUR issuance: `kacho_*` at the top level, nothing nested."""
        claims = {"iss": "https://iam.kacho.local", "sub": "usr_owner_of_the_key",
                  "kacho_principal_type": "service_account",
                  "kacho_principal_id": pid, **extra}
        body = base64.urlsafe_b64encode(json.dumps(claims).encode()).decode().rstrip("=")
        return "eyJhbGciOiJub25lIn0." + body + ".not-a-signature"

    if pp.token_principal_id(_flat("sva_flat")) != "sva_flat":
        print("SELF-TEST FAIL: the FLAT placement of our own issuer is not read — "
              "every channel on that lane would be condemned as carrying no "
              "principal, and the seed aborts the whole run", file=sys.stderr)
        ok = False

    # The sound flat channel stays quiet — the direction that actually failed.
    if pp.unpaired_principals({"svaAId": "sva_f", "jwtSAA": _flat("sva_f")},
                              {"svaAId": "jwtSAA"}):
        print("SELF-TEST FAIL: a SOUND channel on the platform's own issuance lane "
              "was reported broken", file=sys.stderr)
        ok = False

    # ...and the defect is STILL caught on that same lane. Widening the reader must
    # not cost the property the reader exists for: a token authenticating as
    # somebody other than the bound subject remains a finding that NAMES both.
    got = pp.unpaired_principals({"svaAId": "sva_bound", "jwtSAA": _flat("sva_other")},
                                 {"svaAId": "jwtSAA"})
    if len(got) != 1 or "sva_bound" not in got[0] or "sva_other" not in got[0]:
        print(f"SELF-TEST FAIL: on the flat lane a mismatched principal is no longer "
              f"caught, or the finding does not name both: {got}", file=sys.stderr)
        ok = False

    # Precedence, stated rather than left to accident: when a token carries BOTH
    # placements, the top level wins — because that is the edge's order. A reader
    # that resolved the other way would judge the seed against a principal the
    # request will not speak as.
    both = _flat("sva_top", ext_claims={"kacho_principal_id": "sva_nested"})
    if pp.token_principal_id(both) != "sva_top":
        print("SELF-TEST FAIL: with both placements present the reader disagrees "
              "with the edge about which one wins", file=sys.stderr)
        ok = False

    # Census, so "no findings" stays distinguishable from "nothing examined".
    print(f"principal pairings: {len(pp.PRINCIPAL_PAIRINGS)} declared, "
          f"both placements exercised (flat = our issuer, nested = provider)")

    # And the live tree: whatever the answer, it must be MEASURED, not assumed. This
    # prints it, so a change of disposition is visible in the self-test output too.
    live = env_disposition([env_path(s) for s in ALL_SERVICES])
    print(f"live tree: {len(live)} env paths examined -> "
          f"{ {v: sum(1 for x in live.values() if x == v) for v in sorted(set(live.values()))} }")
    print("SELF-TEST OK" if ok else "SELF-TEST FAILED")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description="production-posture newman seed (all suites)")
    ap.add_argument("--services", default=os.environ.get("SERVICES", " ".join(ALL_SERVICES)),
                    help="whitespace/comma separated suite list to patch")
    ap.add_argument("--no-patch-env", action="store_true",
                    help="emit fixtures on stdout without touching the newman envs")
    ap.add_argument("--self-test", action="store_true",
                    help="prove env_disposition tells tracked from ignored; touches no stand")
    args = ap.parse_args()
    if args.self_test:
        return _self_test()
    services = [s for s in args.services.replace(",", " ").split() if s]

    ensure_certs()
    forward = ensure_hydra_forward()
    try:
        # Imported AFTER the prerequisites: prodseed_matrix mints the bootstrap Bearer at
        # import time, which needs the operator certificate on disk and iam :9091 reachable.
        import prodseed_matrix as pm

        log("minting the matrix (iam MintBootstrapToken → SAKeyService.Issue → OAuth2)")
        fixtures = pm.seed()
        boot = fixtures["jwtBootstrap"]
        out_dir = pathlib.Path(os.environ.get("OUT_DIR", str(HERE / "out")))
        out_dir.mkdir(parents=True, exist_ok=True)
        (out_dir / "authz-fixtures.json").write_text(json.dumps(fixtures, indent=2) + "\n")
        # /tmp/matrix.json is the handoff the extension seeders (and prodrun.sh) read.
        pathlib.Path("/tmp/matrix.json").write_text(json.dumps(fixtures))
        log(f"matrix seeded: acctA={fixtures['accountAId']} projA1={fixtures['projectA1Id']}")

        if args.no_patch_env:
            print(json.dumps(fixtures))
            return 0

        # Shared keys first (every suite), then per-suite extensions on top.
        patch(fixtures, [env_path(s) for s in services])

        proj = fixtures["projectA1Id"]
        proj_cross = fixtures["projectA2Id"]
        nlb_ids = {}
        if "nlb" in services:
            nlb_ids = seed_nlb_resources(boot, fixtures["baseUrl"],
                                         fixtures["internalBaseUrl"], proj, out_dir)
        for svc in services:
            if svc == "nlb":
                # prodseed_nlb_ext.py exists for the STANDALONE prodrun.sh flow, which does
                # not run seed-nlb-fixtures.sh. Here the shell seeder already ran, and it is
                # the placement-coherent author: it seeds into the nlb-DEDICATED zone, while
                # the extension seeds into zone `a`. Running both would hand the suite a set
                # whose ids straddle two zones — the exact incoherence seed-nlb-fixtures.sh's
                # own header documents. Dev parity too: setup.sh runs only the shell seeder.
                extra = dict(nlb_ids)
            else:
                extra = run_ext(svc, proj, proj_cross)
            if extra:
                (out_dir / f"{svc}-fixtures.json").write_text(json.dumps(extra, indent=2) + "\n")
                patch(extra, [env_path(svc)])
        # In this posture the tokens written into the env files are genuine Hydra-signed
        # cluster credentials with a real lifetime — not the well-known dev HMAC strings
        # the dev path leaves behind. Whether that is a hazard depends on what version
        # control does with those files, so it is MEASURED, never asserted.
        report_env_disposition([env_path(s) for s in services])
        log("DONE")
        return 0
    finally:
        if forward is not None:
            forward.terminate()


if __name__ == "__main__":
    sys.exit(main())
