#!/usr/bin/env python3

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""tests/newman/scripts/gen.py — newman collection generator for the api-gateway suite.

Usage:
    python3 scripts/gen.py                # generate all case files
    python3 scripts/gen.py cluster_admin  # one case file

Source of truth: tests/newman/cases/<name>.py modules, each exporting a CASES
list of Case objects.

Slim adaptation of services/iam/tests/newman/scripts/gen.py — only the helpers the
api-gateway-owned cases need. This suite owns the cluster-RBAC admin surface
(`InternalClusterService`), which lives on the api-gateway CLUSTER-INTERNAL REST
listener and therefore has no home in any per-service suite.

LAYOUT (changed 2026-07-26): collections are written to `collections/`, NOT next to
the case files. The shared CI gate (services/iam/tests/newman/scripts/
assert-suites-green.sh) and the execution-coverage gate both walk
`collections/*.postman_collection.json` against `out/<name>.json`. Emitting to
`cases/` put this suite outside both gates — one of the reasons it could sit
unexecuted without anything going red.

THREE HARNESS INVARIANTS THIS GENERATOR ENFORCES (all three were absent, and their
absence is exactly why a direct run produced 54 requests, zero Authorization headers,
69 failed assertions — and not one word about its own misconfiguration):

  1. A missing subject token FAILS; it does not silently drop the header. Dropping it
     turns "ordinary user must get 403" into "anonymous must get 403" — which still
     passes, for a completely different reason. See `_auth_pre_script`.
  2. A missing base-URL variable FAILS; it does not silently skip. See `require_env_url`.
  3. Every self-retry poll carries a REAL inter-poll delay. newman runs test scripts
     synchronously and honours `setNextRequest` before any `setTimeout`, so a busy-wait
     is the only thing that actually spaces the polls. See `poll_operation`.
"""
from __future__ import annotations

import json
import sys
import uuid
import importlib.util
from pathlib import Path
from dataclasses import dataclass, field
from typing import List, Dict, Optional

ROOT = Path(__file__).resolve().parents[1]
CASES_DIR = ROOT / "cases"
COLLECTIONS_DIR = ROOT / "collections"

# Poll budget for async Operations. The delay is sized FROM the cap rather than
# fixed, so raising the cap cannot silently multiply the worst-case wall-time
# (testing.md: `delay = clamp(30000/cap, 100..500)ms`).
POLL_CAP = 45
POLL_DELAY_MS = max(100, min(500, 30000 // POLL_CAP))


# ---------------------------------------------------------------------------
# Declarative structures
# ---------------------------------------------------------------------------

@dataclass
class Step:
    """One HTTP request inside a case."""
    name: str
    method: str
    path: str  # relative; the mux prefix is added automatically
    body: Optional[Dict] = None
    pre_script: List[str] = field(default_factory=list)
    test_script: List[str] = field(default_factory=list)
    # Per-step auth override.
    #   None              — Authorization header untouched
    #   "anonymous"       — Authorization header stripped (DELIBERATE anonymous case)
    #   "<envVarName>"    — Authorization: Bearer {{envVarName}}; missing value = FAIL
    auth: Optional[str] = None
    # Which api-gateway listener this step targets.
    #   "public"   — {{baseUrl}}          (:8080 cmux, the tenant-facing REST surface)
    #   "internal" — {{internalBaseUrl}}  (:8081 cluster-internal REST listener)
    #   "external" — {{externalBaseUrl}}  (the advertised public TLS endpoint)
    # Internal* RPCs are served ONLY on "internal"; "public" 404s them by design
    # (ban #6), which is what the "external" negatives assert.
    mux: str = "public"


@dataclass
class Case:
    """One test case — may contain multiple steps."""
    id: str
    title: str
    classes: List[str]
    priority: str
    steps: List[Step]


# ---------------------------------------------------------------------------
# Global prerequest: generate a runId once per collection run
# ---------------------------------------------------------------------------

PRE_GLOBAL = [
    "if (!pm.environment.get('runId') || pm.environment.get('runId') === '') {",
    "  const t = Date.now().toString(36);",
    "  const r = Math.floor(Math.random() * 1e9).toString(36);",
    "  pm.environment.set('runId', ('r' + t + r).replace(/[^a-z0-9]/g, '').slice(0, 11));",
    "}",
]


# ---------------------------------------------------------------------------
# pm.* assertion helpers
# ---------------------------------------------------------------------------

def assert_status(code: int) -> List[str]:
    return [
        f"pm.test('status {code}', () => pm.expect(pm.response.code, pm.response.text()).to.eql({code}));",
    ]


def assert_grpc_code(code: int, code_name: str) -> List[str]:
    return [
        f"pm.test('grpc code {code} ({code_name})', () => {{",
        "  const j = pm.response.json();",
        f"  pm.expect(j.code, JSON.stringify(j)).to.eql({code});",
        "});",
    ]


def assert_error_message_eql(expected: str) -> List[str]:
    """Exact-match error message. Error text is part of the Kachō contract
    (api-conventions.md: «Тексты — часть контракта»), so this is `.to.eql`,
    never a substring match."""
    payload = json.dumps(expected)
    return [
        f"pm.test('error message exactly equals {payload}', () => {{",
        "  const j = pm.response.json();",
        f"  pm.expect(j.message, JSON.stringify(j)).to.eql({payload});",
        "});",
    ]


def save_from_response(jsonpath: str, env_var: str) -> List[str]:
    """Save a value from the response into a Postman env var."""
    return [
        "try {",
        "  const j = pm.response.json();",
        f"  const v = ({jsonpath});",
        f"  if (v !== undefined && v !== null) pm.environment.set('{env_var}', String(v));",
        "} catch (e) {}",
    ]


def assert_iam_operation_envelope() -> List[str]:
    """IAM mutations return an Operation whose id carries the `iop` prefix."""
    return [
        "pm.test('IAM Operation envelope returned', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.id, 'operation.id must start with iop: ' + JSON.stringify(j)).to.match(/^iop[a-z0-9]+$/);",
        "  pm.expect(j.done, 'operation.done present').to.be.a('boolean');",
        "});",
    ]


def require_env_url(var: str, path: str, why: str = "") -> List[str]:
    """Pre-request block: point this request at {{<var>}}+path, FAILING if <var> is unset.

    WHY THIS ASSERTS INSTEAD OF ONLY SKIPPING. Two guards look identical and are not
    the same thing:

      * an OPERATION guard — `if (!opId) skipRequest()` — is a LEGAL skip. The create
        under test was refused on purpose, so there is no operation to poll.
      * an ENVIRONMENT guard — `if (!internalBaseUrl) skipRequest()` — is a BROKEN
        HARNESS. The check is still meaningful and still expected to run; the runner
        merely failed to inject the variable.

    newman leaves NO trace of a skipped request, so the second kind used to pass by
    never running. The variable is therefore asserted by name before the skip.
    exec-coverage.py enforces this shape statically: a `skipRequest()` guard that reads
    a `*BaseUrl` variable and carries no `pm.test(` fails the gate.
    """
    reason = f" — {why}" if why else ""
    return [
        f"// HARNESS-CONFIG GUARD — {var} is injected by the newman runner (--env-var).",
        "// Missing value = misconfigured harness, NOT a legal mode: FAIL, then skip.",
        f"const __cfgUrl = pm.environment.get('{var}') || pm.variables.get('{var}') || '';",
        "if (__cfgUrl) {",
        f"  pm.request.url = __cfgUrl + '{path}';",
        "} else {",
        f"  pm.test('harness config: {var} is set{reason}', () => {{",
        f"    pm.expect.fail('{var} is not set — the newman runner "
        "(deploy/scripts/newman-parallel.sh --env-var) did not inject it. This step cannot "
        "run, and a check that cannot run MUST NOT be silently dropped.');",
        "  });",
        "  pm.execution.skipRequest();",
        "}",
    ]


def poll_operation(op_var: str = "opId", auth: str = "jwtBootstrap",
                   name: str = "poll-op") -> Step:
    """Poll /operations/{{<op_var>}} until done, with a REAL inter-poll delay.

    newman executes the test script synchronously and acts on `setNextRequest` before
    any `setTimeout` fires, so a busy-wait immediately before the self-retry is the only
    construct that actually spaces the polls. Without it a 45-iteration loop covers
    ~0.2s of wall-clock and the poller "gives up" while the async tail is perfectly
    healthy (testing.md — the class that looks like a materialization lag but is really
    a probe that never waited).

    OperationService is `<exempt>` in the gateway authz catalog, but kacho-iam still
    rejects unauthenticated callers, so the poll carries a Bearer.
    """
    return Step(
        name=name,
        method="GET",
        path="/operations/{{" + op_var + "}}",
        auth=auth,
        pre_script=[
            "// OPERATION guard (legal skip): the mutation under test was refused on",
            "// purpose, so there is no Operation to poll and nothing to assert.",
            f"if (!pm.environment.get('{op_var}')) {{ pm.execution.skipRequest(); }}",
        ],
        test_script=[
            "pm.test('poll status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
            "const j = pm.response.json();",
            "if (pm.environment.get('_pollStarted') !== pm.info.requestName) {",
            "  pm.environment.set('_pollCount', '0');",
            "  pm.environment.set('_pollStarted', pm.info.requestName);",
            "}",
            "const pc = parseInt(pm.environment.get('_pollCount') || '0', 10);",
            f"if (!j.done && pc < {POLL_CAP}) {{",
            "  pm.environment.set('_pollCount', String(pc + 1));",
            f"  const _ipd = Date.now(); while (Date.now() - _ipd < {POLL_DELAY_MS}) void 0;"
            f"  /* real inter-poll delay: cap {POLL_CAP} x {POLL_DELAY_MS}ms budget (testing.md) */",
            "  pm.execution.setNextRequest(pm.info.requestName);",
            "  return;",
            "}",
            "pm.environment.unset('_pollCount');",
            "pm.environment.unset('_pollStarted');",
            "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
            "pm.test('operation succeeded (no error)', () => {",
            "  pm.expect(j.error, 'op error: ' + JSON.stringify(j.error || {})).to.be.oneOf([undefined, null]);",
            "});",
        ],
    )


# ---------------------------------------------------------------------------
# Postman v2.1 serialization
# ---------------------------------------------------------------------------

_MUX_VAR = {
    "public": "baseUrl",
    "internal": "internalBaseUrl",
    "external": "externalBaseUrl",
}


def _auth_pre_script(auth: str) -> List[str]:
    """JS snippet that sets/clears the Authorization header for one step."""
    if auth == "anonymous":
        return [
            "// per-step auth: DELIBERATE anonymous step",
            "pm.request.headers.remove('Authorization');",
        ]
    return [
        f"// per-step auth: bearer from env '{auth}'",
        f"const __t = pm.environment.get('{auth}') || pm.variables.get('{auth}') || '';",
        "if (__t) {",
        "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});",
        "} else {",
        # HARNESS-CONFIG GUARD. An `auth="<envVar>"` step names the SUBJECT the case is
        # about ("an ordinary user must get 403"). If the fixture seed never wrote that
        # variable, dropping the header does not skip the check — it runs it as
        # ANONYMOUS, i.e. against a different principal entirely. The expectation
        # (401/403) usually still holds, so the case passes FOR THE WRONG REASON and the
        # subject under test is never exercised. That is precisely how this suite could
        # issue 54 header-less requests and still call one of them a 403 test.
        f"  pm.test('harness config: {auth} is set (subject under test)', () => {{",
        f"    pm.expect.fail('{auth} is not set — the authz-fixture seed "
        "(tests/authz-fixtures/setup.sh, or prodseed_all.py under production posture) did "
        "not provide this subject. Running the step anonymously would test a DIFFERENT "
        "principal and pass for the wrong reason.');",
        "  });",
        "  pm.request.headers.remove('Authorization');",
        "}",
    ]


def step_to_postman(step: Step) -> Dict:
    if step.mux not in _MUX_VAR:
        raise ValueError(f"step {step.name!r}: unknown mux {step.mux!r} "
                         f"(expected one of {sorted(_MUX_VAR)})")
    var = _MUX_VAR[step.mux]
    raw = "{{" + var + "}}" + step.path
    item: Dict = {
        "name": step.name,
        "request": {
            "method": step.method,
            "header": [{"key": "Content-Type", "value": "application/json"}],
            "url": {
                "raw": raw,
                "host": ["{{" + var + "}}"],
                "path": [p for p in step.path.split("?")[0].strip("/").split("/") if p],
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
    # Non-public listeners are addressed through a harness variable that MUST be
    # present; require_env_url asserts it by name before skipping.
    if step.mux != "public":
        why = {
            "internal": "Internal* RPCs live ONLY on the cluster-internal REST listener",
            "external": "external-isolation check — Internal* paths must 404 on the "
                        "advertised TLS endpoint",
        }[step.mux]
        pre = require_env_url(var, step.path, why) + pre
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


def build_collection(resource: str, cases: List[Case]) -> Dict:
    return {
        "info": {
            # Deterministic _postman_id (UUIDv5 over the collection name) so a
            # regeneration with no source change produces no diff. A random id
            # here made every regeneration dirty every collection, which meant
            # "generated matches source" could never be checked and a real drift
            # had nowhere to show. Postman only needs this to be stable+unique.
            "_postman_id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"kacho-gateway/newman/{resource}")),
            "name": f"kacho-api-gateway / newman / {resource}",
            "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
        },
        "event": [
            {"listen": "prerequest", "script": {"type": "text/javascript", "exec": PRE_GLOBAL}},
        ],
        "item": [case_to_postman(c) for c in cases],
        "variable": [],
    }


# ---------------------------------------------------------------------------
# Discovery + main
# ---------------------------------------------------------------------------

_INJECTED = {
    "Step": Step,
    "Case": Case,
    "assert_status": assert_status,
    "assert_grpc_code": assert_grpc_code,
    "assert_error_message_eql": assert_error_message_eql,
    "save_from_response": save_from_response,
    "assert_iam_operation_envelope": assert_iam_operation_envelope,
    "require_env_url": require_env_url,
    "poll_operation": poll_operation,
    "POLL_CAP": POLL_CAP,
    "POLL_DELAY_MS": POLL_DELAY_MS,
}


def load_cases_module(path: Path):
    spec = importlib.util.spec_from_file_location(path.stem.replace("-", "_"), path)
    mod = importlib.util.module_from_spec(spec)
    for k, v in _INJECTED.items():
        setattr(mod, k, v)
    spec.loader.exec_module(mod)
    return mod


def main(argv: List[str]) -> int:
    want = set(argv[1:])
    found = sorted(CASES_DIR.glob("*.py"))
    if not found:
        print(f"no case files in {CASES_DIR}", file=sys.stderr)
        return 1
    COLLECTIONS_DIR.mkdir(parents=True, exist_ok=True)
    for f in found:
        res = f.stem
        if res.startswith("__"):
            continue
        if want and res not in want:
            continue
        mod = load_cases_module(f)
        cases = getattr(mod, "CASES", [])
        bad = [type(c).__name__ for c in cases if not isinstance(c, Case)]
        if bad:
            sys.stderr.write(f"[{res}] FAIL — non-Case items in CASES ({bad[:3]}).\n")
            return 1
        ids = [c.id for c in cases]
        dups = {x for x in ids if ids.count(x) > 1}
        if dups:
            sys.stderr.write(f"[{res}] FAIL — duplicate case-id: {sorted(dups)}\n")
            return 1
        col = build_collection(res, cases)
        out = COLLECTIONS_DIR / f"{res}.postman_collection.json"
        out.write_text(json.dumps(col, indent=2, ensure_ascii=False) + "\n")
        print(f"[{res}] {len(cases)} cases → {out.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
