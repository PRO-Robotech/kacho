#!/usr/bin/env python3

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/gen.py — generator of Postman collections from declarative
case-modules under tests/newman/cases/*.py (kacho-nlb).

Usage:
    python3 scripts/gen.py                      # all case modules → collections/<name>.postman_collection.json
    python3 scripts/gen.py load-balancer        # one module
    python3 scripts/gen.py --validate           # delegate to validate-cases.py (dup-id + CASES-INDEX coverage)

The generator is intentionally a near-mirror of kacho-vpc/tests/newman/scripts/gen.py
(KAC-VPC convention). NLB-specific helpers and the unified poll_operation_until_done
step live here so case modules only import the high-level Case / Step / helpers via
the module namespace (no `from gen import ...` because gen.py is loaded by path).
"""
from __future__ import annotations

import json
import sys
import uuid
import importlib.util
from pathlib import Path
from dataclasses import replace
from typing import List, Dict, Optional

ROOT = Path(__file__).resolve().parents[1]
SCRIPTS_DIR = Path(__file__).resolve().parent
CASES_DIR = ROOT / "cases"
OUT_DIR = ROOT / "collections"

# --- shared canonical helper-namespace (H0): import, NOT copy ---
# Step/Case + assert_*/save_from_response/poll/retry come from the single source
# tests/newman/shared/harness.py. nlb keeps only its resource-specific blocks +
# thin wrappers below (op-id regex, poll opId-guard/no-retry-comment) + the
# nlb-specific retry_create_until_present helper (which shares harness._RYA_SEQ so
# its -cr<N> suffix stays interleaved with the canonical -lst/-rya counters).
# The per-collection poll-op-<n> counter (harness._POLL_SEQ) is reset in
# load_cases_module (as the old local _poll_seq was). repo root = ROOT.parents[3].
sys.path.insert(0, str(ROOT.parents[3] / "tests" / "newman" / "shared"))
import harness  # noqa: E402
from harness import (  # noqa: E402
    Step, Case,
    assert_status, assert_grpc_code, assert_transcode_error, assert_field_violation,
    assert_unscoped_rejected, assert_absent_id_rejected, save_from_response,
    retry_until_present, retry_until_authorized, retry_until_absent,
)
from harness import assert_operation_envelope as _assert_operation_envelope  # noqa: E402
from harness import poll_operation_until_done as _poll_operation_until_done  # noqa: E402


# ---------------------------------------------------------------------------
# Global pre-request — runs before every request in every case
# ---------------------------------------------------------------------------

PRE_GLOBAL = [
    "if (!pm.environment.get('runId') || pm.environment.get('runId') === '') {",
    "  const t = Date.now().toString(36);",
    "  const r = Math.floor(Math.random() * 1e9).toString(36);",
    "  pm.environment.set('runId', (t + r).replace(/[^a-z0-9]/g, '').slice(-10));",
    "}",
    "pm.environment.set('_suiteProjectId', pm.environment.get('existingProjectId'));",
    "pm.environment.set('_suiteProjectCrossId', pm.environment.get('existingProjectCrossId'));",
    "pm.environment.set('_suiteRegionId', pm.environment.get('existingRegionId'));",
    "pm.environment.set('_suiteRegionAltId', pm.environment.get('existingRegionAltId'));",
    "// Default auth: project-editor JWT on project A (sufficient for most happy-path steps).",
    "// Per-step auth= overrides via _auth_pre_script.",
    "// fallback на jwtBootstrap (cluster-admin) пока per-subject JWT не засеяны setup.sh —",
    "// снимает булк 401 на happy-path; authz-специфичные шаги (per-step auth=) остаются точны.",
    "const __defaultJwt = pm.environment.get('jwtProjectEditorA') || pm.variables.get('jwtProjectEditorA') || pm.environment.get('jwtBootstrap') || '';",
    "if (__defaultJwt && !pm.request.headers.has('Authorization')) {",
    "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __defaultJwt});",
    "}",
]


# ---------------------------------------------------------------------------
# Reusable assertion snippets (pm.*) — same names as kacho-vpc
# ---------------------------------------------------------------------------

def assert_operation_envelope(prefix_regex: str = "^(nlb|tgr|lst)[a-z0-9]+$") -> List[str]:
    """nlb op-id default regex `^(nlb|tgr|lst)…` (call-sites pass `^nlb…` explicitly
    for LB, `^tgr…`/`^lst…` for other resources). (assert_status/grpc_code/
    field_violation/unscoped/absent-id/save_from_response/transcode_error come from
    harness verbatim.)"""
    return _assert_operation_envelope(prefix_regex)


def retry_create_until_present(step: Step, budget: int = 25, interval_ms: int = 500) -> Step:
    """Wrap a CREATE/POST step that references a peer resource (e.g. a vpc Subnet /
    Address) just provisioned inline in the SAME case, in a bounded read-your-writes
    retry over the *cross-service* visibility window.

    A subnet/address created through vpc returns its Operation done (durable in vpc),
    but the peer read on nlb's side (nlb -> vpc SubnetService.Get during LB/Listener
    Create) is briefly stale under load: the sync create rejects with
    InvalidArgument/NotFound `"subnet <id> not found"` (code 3/5) before vpc's write is
    visible to the nlb peer client. Confirmed under `--jobs 4` parallel collections
    (ci-rep2: placement-coherence create-same-zone/-region + INTERNAL-REGIONAL cr-internal
    reddened on `subnet <id> not found`, while the identical provision->poll->create
    pattern in cross-resource happened to win the race and stayed green). This is a
    textbook cross-service read-your-writes lag -> the CLIENT retries the create; it is
    NOT a server barrier.

    Retries the SAME request (setNextRequest -> self) while the response is a
    `<something> not found` rejection (400/404 whose body message contains 'not found'),
    spacing attempts ~interval_ms (busy-wait -- newman fires setNextRequest before any
    setTimeout). A rejected create allocates NOTHING (sync reject before the Operation is
    even minted), so re-POSTing is leak-free and idempotent. budget*interval_ms bounds
    the wait (default 30*400ms = ~12s) -- fail-closed: on any other outcome the wrapped
    step's real test_script runs exactly once, and once the budget is spent it ALSO runs
    on the terminal not-found (a genuinely-absent peer still FAILS the real assertions --
    never masked, never infinite).

    Use ONLY on a create whose peer dependency was provisioned earlier in the SAME case.
    Do NOT wrap negative fixture-absent creates (they legitimately expect the rejection).
    """
    guard = [
        "// bounded read-your-writes retry over the cross-service peer-visibility window",
        "// (vpc subnet/address just provisioned; nlb peer-read briefly stale). Retries",
        "// SELF only while the sync create is a transient '<peer> not found' rejection.",
        "if (pm.environment.get('_crRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_crRetryCount', '0');",
        "  pm.environment.set('_crRetryStarted', pm.info.requestName);",
        "}",
        "const _crc = parseInt(pm.environment.get('_crRetryCount') || '0', 10);",
        "let _crNotFound = false;",
        "try { _crNotFound = [400, 404].includes(pm.response.code)"
        " && /not found/i.test(pm.response.json().message || ''); } catch (e) {}",
        f"if (_crNotFound && _crc < {budget}) {{",
        "  pm.environment.set('_crRetryCount', String(_crc + 1));",
        f"  const _crd = Date.now(); while (Date.now() - _crd < {interval_ms}) {{ /* peer-visibility wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_crRetryCount');",
        "pm.environment.unset('_crRetryStarted');",
    ]
    # Shares the SAME shared counter as the canonical -lst/-rya helpers so the
    # -cr<N> step-name suffix stays interleaved (byte-identity of collections).
    harness._RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-cr{harness._RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def poll_operation_until_done() -> Step:
    """nlb poll: opId-guard on, budget 30, no inline retry-comment, per-collection
    `poll-op-<n>` counter (harness._POLL_SEQ, reset in load_cases_module). Delegates
    to shared harness."""
    return _poll_operation_until_done(budget=30, opid_guard=True,
                                      retry_comment=False, name_counter=True)


def http_method_not_allowed_block(prefix: str, base_path: str) -> List[Case]:
    """HTTP method semantics: PUT/DELETE on collection endpoint → not-allowed status."""
    return [
        Case(
            id=f"{prefix}-METHOD-PUT-NOT-ALLOWED",
            title="PUT on List endpoint → 403/404/405/501",
            classes=["VAL", "NEG"], priority="P3",
            steps=[Step(name="put-list", method="PUT", path=base_path,
                        body={"projectId": "{{_suiteProjectId}}"},
                        test_script=["pm.test('not allowed (403/404/405/501)', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));"])],
        ),
        Case(
            id=f"{prefix}-METHOD-DELETE-LIST",
            title="DELETE on List endpoint (no id) → 403/404/405/501",
            classes=["VAL", "NEG"], priority="P3",
            steps=[Step(name="del-list", method="DELETE", path=base_path,
                        test_script=["pm.test('not allowed (403/404/405/501)', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));"])],
        ),
    ]


def conf_alreadyexists_block(prefix: str, create_path: str, name_template: str,
                              body_extra: Optional[Dict] = None,
                              id_field_pattern: str = "Id") -> Case:
    """CONF: duplicate (project_id, name) on Create returns ALREADY_EXISTS verbatim text.

    NLB pattern: sync 409 on duplicate name (partial UNIQUE in DB). Worker also returns
    error envelope if INSERT race wins both syncs."""
    body_extra = body_extra or {}
    return Case(
        id=f"{prefix}-CR-CONF-ALREADY-EXISTS",
        title=f"Create duplicate name → 409 ALREADY_EXISTS verbatim text",
        classes=["CONF", "NEG", "IDEM"], priority="P1",
        steps=[
            Step(name="create-first", method="POST", path=create_path,
                 body={"projectId": "{{_suiteProjectId}}", "name": name_template, **body_extra},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                              *save_from_response(
                                  "(j.metadata && Object.keys(j.metadata).filter(k => k.endsWith('Id') && k !== 'projectId').map(k => j.metadata[k])[0]) || ''",
                                  "createdId")]),
            poll_operation_until_done(),
            Step(name="create-dup", method="POST", path=create_path,
                 body={"projectId": "{{_suiteProjectId}}", "name": name_template, **body_extra},
                 test_script=[
                     "pm.test('rejected (sync 409 or async error)', () => pm.expect(pm.response.code).to.be.oneOf([200, 409]));",
                     "if (pm.response.code === 409) {",
                     "  pm.test('grpc code 6 (ALREADY_EXISTS)', () => pm.expect(pm.response.json().code).to.eql(6));",
                     "  pm.test('mentions already exists', () => pm.expect(pm.response.json().message.toLowerCase()).to.include('already exists'));",
                     "}",
                 ]),
            Step(name="cleanup-first", method="DELETE", path=f"{create_path}/{{{{createdId}}}}",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
        ],
    )


# ---------------------------------------------------------------------------
# Postman v2.1 serialization
# ---------------------------------------------------------------------------

def _auth_pre_script(auth: str) -> List[str]:
    if auth == "anonymous":
        return [
            "// AZD per-step: anonymous (strip Authorization header)",
            "pm.request.headers.remove('Authorization');",
        ]
    return [
        f"// AZD per-step: bearer from env '{auth}'",
        f"const __t = pm.environment.get('{auth}') || pm.variables.get('{auth}') || '';",
        "if (__t) {",
        "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});",
        "} else {",
        "  pm.request.headers.remove('Authorization');",
        "}",
    ]


def step_to_postman(step: Step) -> Dict:
    item: Dict = {
        "name": step.name,
        "request": {
            "method": step.method,
            "header": [{"key": "Content-Type", "value": "application/json"}],
            "url": {
                "raw": "{{baseUrl}}" + step.path,
                "host": ["{{baseUrl}}"],
                "path": [p for p in step.path.strip("/").split("/") if p],
            },
        },
    }
    if step.body is not None:
        item["request"]["body"] = {
            "mode": "raw",
            "raw": json.dumps(step.body, ensure_ascii=False),
            "options": {"raw": {"language": "json"}},
        }
    pre = list(step.pre_script)
    if step.auth is not None:
        pre = _auth_pre_script(step.auth) + pre
    events = []
    if pre:
        events.append({"listen": "prerequest", "script": {"type": "text/javascript", "exec": pre}})
    if step.test_script:
        events.append({"listen": "test", "script": {"type": "text/javascript", "exec": step.test_script}})
    if events:
        item["event"] = events
    return item


def case_to_postman(case: Case) -> Dict:
    tags = [f"class:{c}" for c in case.classes] + [f"priority:{case.priority}"]
    return {
        "name": f"{case.id} — {case.title}",
        "description": " | ".join(tags),
        "item": [step_to_postman(s) for s in case.steps],
    }


def build_collection(service: str, cases: List[Case]) -> Dict:
    return {
        "info": {
            "_postman_id": str(uuid.uuid4()),
            "name": f"kacho-nlb / newman / {service}",
            "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
        },
        "event": [
            {
                "listen": "prerequest",
                "script": {"type": "text/javascript", "exec": PRE_GLOBAL},
            },
        ],
        "item": [case_to_postman(c) for c in cases],
        "variable": [],
    }


# ---------------------------------------------------------------------------
# Module discovery & main
# ---------------------------------------------------------------------------

def load_cases_module(path: Path):
    # Reset the shared poll-step counter so each collection's poll-op-<n> names are
    # deterministic (stable across regenerations) rather than depending on how
    # many modules were loaded before this one. NB: harness._RYA_SEQ (the -lst/-rya/-cr
    # counter) is intentionally NOT reset — it is global across the run, matching the
    # original nlb behaviour where _RYA_SEQ was module-global while _poll_seq was reset.
    harness._POLL_SEQ[0] = 0
    spec = importlib.util.spec_from_file_location(path.stem, path)
    mod = importlib.util.module_from_spec(spec)
    # Inject helpers into the module's namespace so case files don't import gen.
    mod.Step = Step
    mod.Case = Case
    mod.assert_status = assert_status
    mod.assert_grpc_code = assert_grpc_code
    mod.assert_transcode_error = assert_transcode_error
    mod.assert_unscoped_rejected = assert_unscoped_rejected
    mod.assert_absent_id_rejected = assert_absent_id_rejected
    mod.assert_field_violation = assert_field_violation
    mod.assert_operation_envelope = assert_operation_envelope
    mod.save_from_response = save_from_response
    mod.poll_operation_until_done = poll_operation_until_done
    mod.retry_until_authorized = retry_until_authorized
    mod.retry_until_present = retry_until_present
    mod.retry_until_absent = retry_until_absent
    mod.retry_create_until_present = retry_create_until_present
    mod.http_method_not_allowed_block = http_method_not_allowed_block
    mod.conf_alreadyexists_block = conf_alreadyexists_block
    spec.loader.exec_module(mod)
    return mod


def _check_duplicate_ids() -> int:
    seen: Dict[str, str] = {}
    dups: List[str] = []
    for f in sorted(CASES_DIR.glob("*.py")):
        if f.name.startswith("_"):
            continue
        mod = load_cases_module(f)
        for c in getattr(mod, "CASES", []):
            if c.id in seen:
                dups.append(f"  - {c.id!r}: {seen[c.id]} and {f.name}")
            else:
                seen[c.id] = f.name
    if dups:
        sys.stderr.write("gen: FAIL — duplicate case-id (must be unique across all modules):\n")
        sys.stderr.write("\n".join(dups) + "\n")
        return 1
    return 0


def main(argv: List[str]) -> int:
    args = argv[1:]
    if "--validate" in args:
        import runpy
        sys.argv = [str(SCRIPTS_DIR / "validate-cases.py")]
        runpy.run_path(str(SCRIPTS_DIR / "validate-cases.py"), run_name="__main__")
        return 0  # validate-cases.py calls sys.exit itself

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    want = set(args)
    found = sorted(f for f in CASES_DIR.glob("*.py") if not f.name.startswith("_"))
    if not found:
        print(f"no case files in {CASES_DIR}")
        return 1
    if _check_duplicate_ids() != 0:
        return 1
    total = 0
    for f in found:
        svc = f.stem
        if want and svc not in want:
            continue
        mod = load_cases_module(f)
        cases = getattr(mod, "CASES", [])
        col = build_collection(svc, cases)
        out = OUT_DIR / f"{svc}.postman_collection.json"
        out.write_text(json.dumps(col, indent=2, ensure_ascii=False))
        print(f"[{svc}] {len(cases)} cases → {out.relative_to(ROOT)}")
        total += len(cases)
    print(f"total: {total} cases")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
