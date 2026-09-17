#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Independent CI-PY-1 black-box fixtures; invoked by outcomes_test.go.

No result implementation lives here. The assertions read results of the real
producer, Make, shell functions and workflow bodies. sitecustomize changes one
external condition of a CHILD interpreter only: pytest availability or its JUnit
report. It never changes the driver interpreter or manufactures a product verdict.
"""
from __future__ import annotations

import ast
import concurrent.futures
import contextlib
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import time
import uuid

RUNNER = ".github/scripts/run-python-probes.py"
GOOD = "def test_first():\n    assert 1 == 1\n\ndef test_second():\n    assert 2 == 2\n"
PARAM = "import pytest\n@pytest.mark.parametrize('value', [1, 2, 3])\ndef test_parameter(value):\n    assert value > 0\n"
SCRIPT_BAD = "def main():\n    return 1\n\nif __name__ == '__main__':\n    raise SystemExit(main())\n"
SITE = '''import atexit, importlib.abc, json, os, pathlib, sys
mode = os.environ.get("CI_PY_CHILD_FAULT", "")
trace = os.environ.get("CI_PY_FAULT_TRACE", "")
def event(kind, **fields):
    if trace:
        with open(trace, "a") as stream:
            stream.write(json.dumps(dict(kind=kind, pid=os.getpid(), **fields))+"\\n")
if mode == "pytest-absent":
    class WithoutPytest(importlib.abc.MetaPathFinder):
        def find_spec(self, fullname, path=None, target=None):
            if fullname == "pytest" or fullname.startswith("pytest."):
                event("pytest-import-denied", name=fullname)
                raise ModuleNotFoundError("No module named 'pytest'", name="pytest")
    sys.meta_path.insert(0, WithoutPytest())
if mode in ("report-missing", "report-corrupt"):
    def change_report():
        for argument in sys.argv:
            if argument.startswith("--junit-xml=") or argument.startswith("--junitxml="):
                report = pathlib.Path(argument.split("=", 1)[1])
                if report.is_file():
                    before = report.read_bytes()
                    if mode == "report-missing":
                        report.unlink()
                    else:
                        report.write_text("<broken-report>")
                    event(mode, bytes_before=len(before), path=str(report))
    atexit.register(change_report)
'''


class Unavailable(Exception):
    pass


class Case:
    def __init__(self, name):
        self.name, self.errors, self.evidence, self.assertions = name, [], [], 0

    def check(self, predicate, message):
        self.assertions += 1
        if not predicate:
            self.errors.append(message)

    def capture(self, result):
        self.evidence.append(result["evidence"])
        return result

    def record(self, result, producer, category, **counts):
        record = result.get("record")
        self.check(isinstance(record, dict), f"{result['label']}: current outcome record missing/unreadable (rc={result['rc']})")
        if not isinstance(record, dict):
            return None
        self.check(record.get("schema_version") == 1, "unknown schema version")
        self.check(record.get("invocation_id") == result["invocation_id"], "record is not bound to current invocation")
        self.check(record.get("producer") == producer, f"producer {record.get('producer')!r}, expected {producer!r}")
        self.check(record.get("category") == category, f"category {record.get('category')!r}, expected {category!r}")
        expected_unit = {"run-python-probes":"python-probe", "run-python-probes/self-test":"self-test-check", "gate-self-test":"self-test", "ci-local":"local-check"}[producer]
        self.check(record.get("unit") == expected_unit, "counter unit differs from producer's declared unit")
        counters = record.get("counters", {})
        self.check(isinstance(counters, dict), "counters is not an object")
        if not isinstance(counters, dict):
            counters = {}
        for field in ("files", "declarations", "executed", "failed", "skipped", "unmet"):
            self.check(type(counters.get(field)) is int and counters[field] >= 0, f"counter {field} not a nonnegative integer")
        for field, expected in counts.items():
            self.check(counters.get(field) == expected, f"{result['label']}: {field}={counters.get(field)!r}, expected {expected}")
        self.check(isinstance(record.get("findings"), list), "findings absent")
        self.check(isinstance(record.get("unmet_reasons"), list), "unmet reasons absent")
        if producer == "run-python-probes":
            self.check(type(counters.get("pytest_declarations")) is int and type(counters.get("script_entries")) is int, "Python declaration decomposition absent")
            if all(type(counters.get(k)) is int for k in ("declarations", "pytest_declarations", "script_entries")):
                self.check(counters["declarations"] == counters["pytest_declarations"] + counters["script_entries"], "declaration decomposition inconsistent")
        if category == "green":
            self.check(counters.get("executed", 0) > 0, "GREEN without an executed subject")
            self.check(not record.get("findings") and not record.get("unmet_reasons"), "GREEN discarded findings/unmet")
        if category == "unmet":
            self.check(counters.get("failed") == 0 and not record.get("findings"), "unmet manufactured a finding")
        return record


class Harness:
    def __init__(self, root, work):
        self.root, self.work = Path(root), Path(work)
        requested = os.environ.get("CI_PY_EVIDENCE_DIR")
        self.evidence = Path(requested).resolve() / (os.environ.get("CI_PY_GROUP", "run") + "-" + uuid.uuid4().hex[:8]) if requested else self.work / "evidence"
        self.evidence.mkdir(parents=True)
        self.env = {k:v for k,v in os.environ.items() if not k.startswith(("GIT_", "KACHO_CI_", "CI_PY_CHILD_")) and k != "PYTHONPATH"}
        self.env.update(GOWORK="off", TMPDIR=str(self.work), PYTEST_DISABLE_PLUGIN_AUTOLOAD="1", PYTHONDONTWRITEBYTECODE="1")
        self.site = self.work / "child-import-control"
        self.site.mkdir()
        (self.site / "sitecustomize.py").write_text(SITE)
        self.serial = 0
        self.cases = []
        for binary in ("python3", "git", "bash", "make"):
            if not shutil.which(binary):
                raise Unavailable(f"driver prerequisite {binary} unavailable")
        # Harness prerequisites are checked before asking any product question.
        import pytest
        import yaml
        self.versions = {"python":sys.version, "pytest":pytest.__version__, "pyyaml":yaml.__version__}
        self.versions["head"] = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=self.root, env=self.env, text=True).strip()
        subjects = (RUNNER, "deploy/scripts/run-gate-self-tests.sh", "deploy/Makefile", "scripts/ci-local.sh", ".github/workflows/ci.yaml", "tools/pythonprobes/outcomes_test.go", "tools/pythonprobes/testdata/outcomes_driver.py")
        self.versions["subject_sha256"] = {name:hashlib.sha256((self.root / name).read_bytes()).hexdigest() for name in subjects}
        (self.evidence / "provenance.json").write_text(json.dumps(self.versions, indent=2))
        probe = subprocess.run([sys.executable, "-c", "import json,yaml,subprocess; import pytest"], env=self.child_env("pytest-absent"), capture_output=True, text=True)
        if probe.returncode == 0 or "No module named 'pytest'" not in probe.stderr:
            raise Unavailable("absence fixture did not remove exactly pytest while preserving interpreter, yaml and stdlib")

    def case(self, name):
        case = Case(name)
        self.cases.append(case)
        return case

    def child_env(self, fault="", **extra):
        env = dict(self.env)
        env.update(PYTHONPATH=str(self.site), CI_PY_CHILD_FAULT=fault, CI_PY_FAULT_TRACE=str(self.evidence / "fault-events.jsonl"))
        env.update(extra)
        return env

    def run(self, label, argv, cwd=None, fault="", extra=None, timeout=600, initial_record=None):
        self.serial += 1
        directory = self.evidence / f"{self.serial:03d}-{label}"
        directory.mkdir()
        outcome = directory / "outcome.json"
        ident = uuid.uuid4().hex
        env = self.child_env(fault, KACHO_CI_OUTCOME_FILE=str(outcome), KACHO_CI_INVOCATION_ID=ident)
        env.update(extra or {})
        if initial_record is not None:
            outcome.write_text(json.dumps(initial_record))
        started = time.time()
        try:
            proc = subprocess.run(argv, cwd=cwd or self.root, env=env, capture_output=True, text=True, timeout=timeout)
        except (OSError, subprocess.TimeoutExpired) as error:
            raise Unavailable(f"{label}: actual process did not complete: {error}") from error
        (directory / "stdout.txt").write_text(proc.stdout)
        (directory / "stderr.txt").write_text(proc.stderr)
        try:
            record = json.loads(outcome.read_text())
        except (OSError, ValueError):
            record = None
        result = dict(label=label, argv=argv, cwd=str(cwd or self.root), rc=proc.returncode, stdout=proc.stdout, stderr=proc.stderr, record=record, invocation_id=ident, start=started, end=time.time(), evidence=str(directory))
        (directory / "execution.json").write_text(json.dumps(result, ensure_ascii=False, indent=2))
        return result

    def small(self, name, body=GOOD):
        root = self.work / name
        root.mkdir()
        path = root / "tests/newman/scripts/example_test.py"
        path.parent.mkdir(parents=True)
        if body is not None:
            ast.parse(body)
            path.write_text(body)
        for args in (["init", "-q"], ["add", "."]):
            subprocess.run(["git", *args], cwd=root, env=self.env, check=True, capture_output=True)
        return root

    def producer(self, name, root, fault="", script=None):
        return self.run(name, [sys.executable, str(script or self.root / RUNNER), "--root", str(root)], cwd=root, fault=fault)

    def tracked_copy(self, name):
        target = self.work / name
        subprocess.run(["git", "clone", "--shared", "--quiet", str(self.root), str(target)], env=self.env, check=True)
        # Source is current tracked bytes, including an implementation under test;
        # no unrelated untracked workspace files become a fixture input.
        names = subprocess.check_output(["git", "ls-files", "-z"], cwd=self.root, env=self.env).split(b"\0")
        hashes = {}
        for raw in names:
            if not raw:
                continue
            rel = os.fsdecode(raw)
            source, dest = self.root / rel, target / rel
            if not source.is_file():
                if dest.is_file():
                    dest.unlink()
                continue
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, dest)
            hashes[rel] = hashlib.sha256(source.read_bytes()).hexdigest()
        subprocess.run(["git", "add", "."], cwd=target, env=self.env, check=True)
        (self.evidence / f"{name}-source-hashes.json").write_text(json.dumps(hashes, sort_keys=True))
        return target


def require_positive(result, text=None):
    if result["rc"] != 0 or (text is not None and text not in result["stdout"] + result["stderr"]):
        raise Unavailable(f"positive fixture {result['label']} invalid: rc={result['rc']}, capture={result['evidence']}")


def replace_one(path, old, new):
    before = path.read_text()
    if old == new or before.count(old) != 1:
        raise Unavailable(f"fixture delta is not one unique replacement in {path}")
    after = before.replace(old, new, 1)
    if after.replace(new, old, 1) != before:
        raise Unavailable("fixture delta not reversible")
    path.write_text(after)
    return before


def pytest_unmet(case, result, producer, declarations=None):
    expected = dict(failed=0, unmet=1)
    if declarations is not None:
        expected.update(declarations=declarations, executed=0)
    record = case.record(result, producer, "unmet", **expected)
    case.check(result["rc"] == 2, f"{result['label']}: missing pytest rc={result['rc']}, expected distinct code 2")
    case.check("нет pytest" in result["stdout"] + result["stderr"], "missing-pytest reason not printed")
    if record:
        case.check(any(x.get("code") == "pytest-unavailable" for x in record.get("unmet_reasons", [])), "missing pytest reason absent from record")


def producer_cases(h):
    root = h.small("two-probes")
    positive = h.producer("two-probes-positive", root)
    require_positive(positive, "проб")
    missing = h.producer("two-probes-no-pytest", root, "pytest-absent")
    c = h.case("CI_PY_01_missing_pytest"); c.capture(positive); c.capture(missing)
    pytest_unmet(c, missing, "run-python-probes", 2)
    c.check("48 проб" not in missing["stdout"] + missing["stderr"], "historical 48 replaces the actual two declarations")
    c = h.case("CI_PY_02_complete_green"); c.capture(positive)
    c.record(positive, "run-python-probes", "green", declarations=2, executed=2, failed=0, skipped=0, unmet=0, pytest_declarations=2, script_entries=0)
    # Optional CLI must agree with the env interface, while first RED above still
    # executes the old CLI instead of stopping at an unknown new argument.
    direct = h.run("explicit-cli", [sys.executable, str(h.root / RUNNER), "--root", str(root), "--outcome-file", str(h.evidence / "explicit.json"), "--invocation-id", "explicit-invocation"])
    c.capture(direct); c.check(direct["rc"] == 0, "explicit outcome CLI is unavailable")
    if (h.evidence / "explicit.json").is_file():
        c.check(json.loads((h.evidence / "explicit.json").read_text()).get("invocation_id") == "explicit-invocation", "CLI must take precedence over env")
    else:
        c.check(False, "explicit CLI result missing")
    path = root / "tests/newman/scripts/example_test.py"
    before = replace_one(path, "assert 2 == 2", "assert 2 == 3")
    finding = h.producer("actual-assertion-failure", root)
    path.write_text(before)
    c = h.case("CI_PY_03_assertion_finding"); c.capture(positive); c.capture(finding)
    c.check(finding["rc"] == 1, "actual assertion must retain rc1")
    c.check("example_test.py" in finding["stdout"] + finding["stderr"], "failed probe coordinate lost")
    c.record(finding, "run-python-probes", "finding", declarations=2, executed=2, failed=1, unmet=0)
    c = h.case("CI_PY_04_self_test")
    normal = c.capture(h.run("self-test-positive", [sys.executable, str(h.root / RUNNER), "--self-test"]))
    require_positive(normal, "ДОКАЗАНО")
    absent = c.capture(h.run("self-test-no-pytest", [sys.executable, str(h.root / RUNNER), "--self-test"], fault="pytest-absent"))
    c.record(normal, "run-python-probes/self-test", "green", failed=0, unmet=0)
    pytest_unmet(c, absent, "run-python-probes/self-test")
    copy = h.work / "self-test-mutant"; shutil.copytree(h.root / ".github/scripts", copy)
    mutant = copy / "run-python-probes.py"
    replace_one(mutant, '_OK_PROBE = "def test_ok():\\n    assert 1 == 1\\n"', '_OK_PROBE = "def test_ok():\\n    assert 1 == 2\\n"')
    bad = c.capture(h.run("self-test-actual-defect", [sys.executable, str(mutant), "--self-test"]))
    c.check(bad["rc"] == 1, "self-test actual defect must be finding")
    c.record(bad, "run-python-probes/self-test", "finding", unmet=0)
    c = h.case("CI_PY_05_invalid_population")
    for name, body in (("empty", None), ("mute", "VALUE = 1\n"), ("skip", "import pytest\n@pytest.mark.skip(reason='controlled skip')\ndef test_skipped():\n    assert True\n")):
        result = c.capture(h.producer(name, h.small(name, body)))
        c.check(result["rc"] == 1, f"{name} must be a finding")
        c.record(result, "run-python-probes", "finding", unmet=0)
        c.check("нет pytest" not in result["stdout"] + result["stderr"], "population finding mislabeled pytest absence")
    c.capture(positive)
    c = h.case("CI_PY_06_junit_unavailable")
    for fault in ("report-missing", "report-corrupt"):
        result = c.capture(h.producer(fault, root, fault))
        events = [json.loads(x) for x in (h.evidence / "fault-events.jsonl").read_text().splitlines()]
        if not any(x["kind"] == fault and x["bytes_before"] > 0 for x in events):
            raise Unavailable(f"{fault}: real nonempty pytest report was not reached")
        c.check(result["rc"] == 2, f"{fault}: incomplete report must be rc2, got {result['rc']}")
        c.record(result, "run-python-probes", "unmet", executed=0, failed=0)
        c.check("Traceback" not in result["stderr"], "report parsing leaked an unclassified interpreter exception")
    c.capture(positive)
    c = h.case("CI_PY_07_units_and_growth")
    param = h.small("parametrized", PARAM)
    result = c.capture(h.producer("parameter-positive", param)); require_positive(result)
    c.record(result, "run-python-probes", "green", declarations=1, executed=3, failed=0, unmet=0)
    absent = c.capture(h.producer("parameter-absent", param, "pytest-absent")); pytest_unmet(c, absent, "run-python-probes", 1)
    with (param / "tests/newman/scripts/example_test.py").open("a") as stream: stream.write("\ndef test_added():\n    assert True\n")
    grew = c.capture(h.producer("parameter-grown", param)); require_positive(grew)
    c.record(grew, "run-python-probes", "green", declarations=2, executed=4, failed=0, unmet=0)
    c = h.case("CI_PY_08_mixed_finding_unmet")
    mixed = h.small("mixed", GOOD)
    script = mixed / "tests/authz-fixtures/participant_test.py"; script.parent.mkdir(parents=True); script.write_text(SCRIPT_BAD)
    subprocess.run(["git", "add", "."], cwd=mixed, env=h.env, check=True)
    ready = c.capture(h.producer("mixed-all-conditions", mixed))
    c.check(ready["rc"] == 1 and "participant_test.py" in ready["stdout"] + ready["stderr"], "mixed fixture did not reach its independent real finding")
    result = c.capture(h.producer("mixed-no-pytest", mixed, "pytest-absent"))
    c.check(result["rc"] == 1, "finding must dominate rc while retaining unmet")
    record = c.record(result, "run-python-probes", "finding", declarations=3, executed=1, failed=1, unmet=1)
    if record:
        c.check(bool(record.get("findings")) and bool(record.get("unmet_reasons")), "mixed aggregate discarded one category")


def local_summary(result):
    record = result.get("record")
    if isinstance(record, dict) and record.get("producer") == "ci-local":
        counts = record.get("counters", {})
        return counts.get("declarations"), counts.get("failed"), counts.get("unmet")
    match = re.search(r"итог: проверок исполнено (\d+), отказов (\d+), НЕ выполнено (\d+)", result["stdout"])
    return tuple(map(int, match.groups())) if match else None


def checked_tools(h):
    for binary in ("helm", "yq", "node"):
        if not shutil.which(binary):
            raise Unavailable(f"full helm fixture missing {binary}")
    helm = h.run("helm-version", ["helm", "version", "--short"])
    yq = h.run("yq-version", ["yq", "--version"])
    if helm["rc"] or yq["rc"] or not re.search(r"mikefarah|version v?4", yq["stdout"]):
        raise Unavailable("real helm/yq prerequisites are not usable")


def workflow(h):
    import yaml
    combined = {"jobs":{}}
    names = subprocess.check_output(["git", "ls-files", "-z", ".github/workflows"], cwd=h.root, env=h.env).split(b"\0")
    for raw in names:
        if not raw or Path(os.fsdecode(raw)).suffix not in (".yml", ".yaml"):
            continue
        name = os.fsdecode(raw)
        document = yaml.safe_load((h.root / name).read_text())
        if not isinstance(document.get("jobs"), dict):
            raise Unavailable(f"{name}: workflow has no parsed jobs")
        for key, value in document["jobs"].items():
            combined["jobs"][name+":"+key] = value
    if not combined["jobs"]:
        raise Unavailable("workflow walk has no jobs")
    return combined


def shell_tokens(script):
    # Comments are discarded by the shell lexer. This is a call-site census,
    # not a new interpreter: the actual run body below remains the executable.
    lexer = shlex.shlex(script, posix=True, punctuation_chars=";&|()\n")
    lexer.whitespace_split = True
    lexer.whitespace = " \t\r"
    lexer.commenters = "#"
    try:
        return list(lexer)
    except ValueError as error:
        raise Unavailable(f"workflow shell cannot be tokenized: {error}") from error


def caller_inventory(document):
    found = []
    for job_name, job in document["jobs"].items():
        for index, step in enumerate(job.get("steps", [])):
            body = step.get("run", "")
            tokens = shell_tokens(body)
            # The first script after python3 is an executable operand; an echo
            # argument or an installation in another job is not a call.
            commands = [n for n in range(len(tokens)) if n == 0 or all(c in ";&|()\n" for c in tokens[n-1])]
            direct = any(tokens[n] in ("python3", "python") and n + 1 < len(tokens) and tokens[n+1] == RUNNER for n in commands)
            make = any(tokens[n] == "make" and n + 1 < len(tokens) and tokens[n+1] in ("gate-self-test", "helm-manifest-test") for n in commands)
            if direct or make:
                found.append(dict(job=job_name, index=index, step=step, kind="python" if direct else "make"))
    return found


def run_workflow_body(h, root, entry, label, fault="", extra=None):
    step = entry["step"]
    body = step["run"]
    if "${{" in body:
        raise Unavailable(f"{label}: workflow run expression needs an explicit captured input")
    github = h.work / ("step-environment-"+label)
    github.mkdir(exist_ok=True)
    for name in ("env", "output"):
        (github / name).touch()
    env = dict(RUNNER_TEMP=str(github), GITHUB_ENV=str(github / "env"), GITHUB_OUTPUT=str(github / "output"), GITHUB_WORKSPACE=str(root), CI="true")
    env.update({k:str(v) for k,v in step.get("env", {}).items()})
    env.update(extra or {})
    directory = root / step.get("working-directory", ".")
    return h.run(label, ["bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", body], cwd=directory, fault=fault, extra=env, timeout=900)


def github_env_file(path):
    result = {}
    if not path.is_file():
        return result
    lines = path.read_text().splitlines()
    cursor = 0
    while cursor < len(lines):
        line = lines[cursor]; cursor += 1
        if "<<" in line:
            key, end = line.split("<<", 1)
            body = []
            while cursor < len(lines) and lines[cursor] != end:
                body.append(lines[cursor]); cursor += 1
            cursor += 1
            result[key] = "\n".join(body)
        elif "=" in line:
            key, value = line.split("=", 1); result[key] = value
    return result


def ci_steps(h, root, case):
    doc = workflow(h)
    found = caller_inventory(doc)
    python_entries = [x for x in found if x["kind"] == "python"]
    if not python_entries:
        case.check(False, "no actual Python caller in workflow")
        return
    # This original positive body must work before judging the missing collector
    # capability. No new helper or an invented successful pipeline is substituted.
    first = python_entries[0]
    initial = case.capture(run_workflow_body(h, root, first, "CI-direct-positive-baseline"))
    require_positive(initial)
    job = doc["jobs"][first["job"]]
    named = {s.get("id"):s for s in job["steps"] if s.get("id")}
    collector, verdict = named.get("python-probes-collect"), named.get("python-probes-verdict")
    case.check(collector is not None, "CI has no executable python-probes-collect boundary")
    case.check(verdict is not None, "CI has no separate mandatory python-probes-verdict boundary")
    if collector is None or verdict is None:
        absent = case.capture(run_workflow_body(h, root, first, "CI-direct-current-no-pytest", "pytest-absent"))
        case.check("УСЛОВИЕ НЕ СОЗДАНО: нет pytest" in absent["stdout"] + absent["stderr"], "actual CI path loses missing-pytest category")
        case.check(absent["rc"] == 0, "collection boundary does not preserve transport success for explicitly unmet")
        return
    case.check("always()" in str(verdict.get("if", "")), "CI final Python verdict not unconditional")
    # pr_verdict remains a separately executable obligation, not lost by replacing
    # the former multiline step with the collector.
    pr_steps = [s for s in job["steps"] if ".github/scripts/pr_verdict_test.py" in shell_tokens(s.get("run", ""))]
    case.check(bool(pr_steps), "separate pr_verdict_test.py obligation lost")
    for fault in ("", "pytest-absent", "assertion-finding"):
        runner_file = root / RUNNER
        restore = None
        if fault == "assertion-finding":
            restore = replace_one(runner_file, '_OK_PROBE = "def test_ok():\\n    assert 1 == 1\\n"', '_OK_PROBE = "def test_ok():\\n    assert 1 == 2\\n"')
        stage = h.work / ("github-" + (fault or "green")); stage.mkdir()
        env_file, output_file = stage / "env", stage / "output"
        env_file.touch(); output_file.touch()
        env = dict(GITHUB_ENV=str(env_file), GITHUB_OUTPUT=str(output_file), RUNNER_TEMP=str(stage), GITHUB_WORKSPACE=str(root), CI="true")
        try:
            collect = case.capture(run_workflow_body(h, root, {"step":collector}, "CI-collect-"+(fault or "green"), "pytest-absent" if fault == "pytest-absent" else "", env))
            env.update(github_env_file(env_file))
            final = case.capture(run_workflow_body(h, root, {"step":verdict}, "CI-verdict-"+(fault or "green"), extra=env))
        finally:
            if restore is not None:
                runner_file.write_text(restore)
        case.check(final["rc"] == (0 if not fault else 1), f"CI final verdict rc={final['rc']} for {fault or 'green'}")
        if fault == "pytest-absent":
            case.check(collect["rc"] == 0, "unmet collector transport not successful")
            case.check("::" in collect["stdout"] and "УСЛОВИЕ НЕ СОЗДАНО: нет pytest" in collect["stdout"] + collect["stderr"], "collector did not publish explicit unmet annotation")
        elif not fault:
            case.check(collect["rc"] == 0, "legal CI collector not green")
        else:
            case.check("pytest-unavailable" not in collect["stdout"] + collect["stderr"], "genuine finding mislabeled pytest absence")
    # An interrupted collector leaves no current result. Execute the actual final
    # YAML body in a fresh step environment, not a fabricated successful record.
    stage = h.work / "github-no-result"
    stage.mkdir()
    env_file, output_file = stage / "env", stage / "output"
    env_file.touch(); output_file.touch()
    env = dict(GITHUB_ENV=str(env_file), GITHUB_OUTPUT=str(output_file), RUNNER_TEMP=str(stage), GITHUB_WORKSPACE=str(root), CI="true")
    missing = case.capture(run_workflow_body(h, root, {"step":verdict}, "CI-verdict-no-result", extra=env))
    case.check(missing["rc"] == 1, "actual CI final verdict accepts an absent current collector result")


def mixed_helm_case(h, root, case):
    # The full legal Helm run in CI10 already proved this independent self-test.
    # Change one real assertion, while hiding pytest only from child interpreters.
    script = root / ".github/scripts/newman-live.py"
    original = replace_one(script, "st.expected == 2 and st.reported == 2", "st.expected == 3 and st.reported == 2")
    try:
        mixed = case.capture(h.run("full-helm-mixed-finding-and-unmet", ["bash", "scripts/ci-local.sh", "helm"], cwd=root, fault="pytest-absent", timeout=900))
    finally:
        script.write_text(original)
    case.check(mixed["rc"] == 1, "mixed full Helm path must preserve the real independent finding")
    case.check("РАЗОШЛИСЬ" in mixed["stdout"] and "newman-live.py" in mixed["stdout"], "independent real self-test assertion fault was not reached")
    case.check("нет pytest" in mixed["stdout"], "missing child pytest condition was not reached alongside the finding")
    record = case.record(mixed, "ci-local", "finding", declarations=5, failed=1, unmet=1)
    if isinstance(record, dict):
        case.check(bool(record["findings"]), "mixed local result lost the separate finding reason")
        case.check("newman-live" in json.dumps(record["findings"], ensure_ascii=False), "mixed local finding no longer identifies the actual independent failed participant")
        case.check(any(reason.get("code") == "pytest-unavailable" for reason in record["unmet_reasons"] if isinstance(reason, dict)), "mixed local result lost the separate pytest-unavailable reason")


def caller_record_cases(h, root, case, captures):
    reader = root / ".github/scripts/ci_outcomes.py"
    actual = [capture.get("record") for capture in captures]
    case.check(all(isinstance(record, dict) for record in actual), "actual gate-self-test runs have no transport records to validate")
    case.check(reader.is_file(), "approved shared record reader capability is absent")
    if reader.is_file() and all(isinstance(record, dict) for record in actual):
        def read(label, record, expected_id=None, raw=None):
            directory = h.work / ("reader-"+label); directory.mkdir()
            path = directory / "record.json"
            if raw is None:
                path.write_text(json.dumps(record))
            elif raw != "ABSENT":
                path.write_text(raw)
            result = case.capture(h.run("reader-"+label, [sys.executable, str(reader), "read", "--outcome-file", str(path), "--invocation-id", expected_id or record["invocation_id"], "--producer", "gate-self-test"], cwd=directory))
            return result
        for number, record in enumerate(actual):
            legal = read("legal-"+str(number), record)
            case.check(legal["rc"] == 0, "reader rejects its actual current gate record")
            try:
                observed = json.loads(legal["stdout"])
            except ValueError:
                observed = None
            case.check(observed == record, "reader changes or loses the current producer result")
        for mode in ("stale", "foreign", "missing", "corrupt", "inconsistent", "category", "producer", "schema", "missing-counter"):
            record = json.loads(json.dumps(actual[0]))
            original_id = record["invocation_id"]
            raw = None
            if mode in ("stale", "foreign"):
                record["invocation_id"] = mode+"-different-invocation"
            elif mode == "missing":
                raw = "ABSENT"
            elif mode == "corrupt":
                raw = "{invalid"
            elif mode == "inconsistent":
                record["counters"]["failed"] = record["counters"]["executed"]+1
            elif mode == "category":
                record["category"] = "unknown"
            elif mode == "producer":
                record["producer"] = "another-producer"
            elif mode == "schema":
                record["schema_version"] = 999
            else:
                del record["counters"]["unmet"]
            result = read(mode, record, original_id, raw)
            case.check(result["rc"] == 1, mode+" record accepted by the real common reader")
            case.check(not result["stdout"].strip() and bool(result["stderr"].strip()), "invalid record needs a diagnostic, not output usable as a result")
        # Actual independent invocations, distinct current ids and working dirs.
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            futures = [pool.submit(read, "parallel-"+str(i), record) for i,record in enumerate(actual)]
            parallel_results = [f.result() for f in futures]
        case.check(actual[0]["invocation_id"] != actual[1]["invocation_id"], "fixture did not capture distinct real invocation ids")
        case.check(all(result["rc"] == 0 for result in parallel_results), "concurrent current invocations contaminate one another")
    # Full actual path for the subtle false exception: gate-self-test is GREEN,
    # then the manifest recipe fails. Only this one recipe line is introduced.
    makefile = root / "deploy/Makefile"
    original = replace_one(makefile, "helm-manifest-test: gate-self-test\n", "helm-manifest-test: gate-self-test\n\t@echo CI_PY_LATE_MAKE_FINDING >&2; false\n")
    try:
        late = case.capture(h.run("full-helm-late-make-finding", ["bash", "scripts/ci-local.sh", "helm"], cwd=root, timeout=900))
    finally:
        makefile.write_text(original)
    case.check(late["rc"] == 1 and local_summary(late) == (5, 1, 0), "GREEN gate prerequisite masks a later real Make failure")
    case.check("CI_PY_LATE_MAKE_FINDING" in late["stdout"], "late recipe finding not reached after actual prerequisite")
    case.record(late, "ci-local", "finding", declarations=5, executed=5, failed=1, unmet=0)


def chain_cases(h):
    checked_tools(h)
    root = h.tracked_copy("chain-tree")
    base = h.run("full-helm-positive", ["bash", "scripts/ci-local.sh", "helm"], cwd=root, timeout=900)
    require_positive(base)
    if local_summary(base) != (5, 0, 0):
        raise Unavailable(f"full helm positive did not execute declared five checks: {local_summary(base)}, {base['evidence']}")
    c9 = h.case("CI_PY_09_selftests_and_make")
    c9.capture(base)
    direct = c9.capture(h.run("selftest-runner-absent", ["bash", "deploy/scripts/run-gate-self-tests.sh"], cwd=root, fault="pytest-absent", timeout=900))
    pytest_unmet(c9, direct, "gate-self-test")
    made = c9.capture(h.run("make-gate-self-test-absent", ["make", "-C", "deploy", "gate-self-test"], cwd=root, fault="pytest-absent", timeout=900))
    c9.check(made["rc"] == 2, "real Make transport did not preserve recipe refusal")
    c9.record(made, "gate-self-test", "unmet", failed=0, unmet=1)
    c10 = h.case("CI_PY_10_full_helm_chain")
    c10.capture(base); c10.record(base, "ci-local", "green", declarations=5, executed=5, failed=0, unmet=0)
    absent = c10.capture(h.run("full-helm-no-pytest", ["bash", "scripts/ci-local.sh", "helm"], cwd=root, fault="pytest-absent", timeout=900))
    c10.check(absent["rc"] == 0 and local_summary(absent) == (5, 0, 1), f"actual helm path loses third category: rc={absent['rc']} summary={local_summary(absent)}")
    c10.check("нет pytest" in absent["stdout"] and "манифест" in absent["stdout"], "local summary loses named missing Python prerequisite")
    c10.record(absent, "ci-local", "unmet", declarations=5, executed=4, failed=0, unmet=1)
    script = root / RUNNER
    original = replace_one(script, '_OK_PROBE = "def test_ok():\\n    assert 1 == 1\\n"', '_OK_PROBE = "def test_ok():\\n    assert 1 == 2\\n"')
    bad = c10.capture(h.run("full-helm-genuine-finding", ["bash", "scripts/ci-local.sh", "helm"], cwd=root, timeout=900))
    script.write_text(original)
    c10.check(bad["rc"] == 1 and local_summary(bad) == (5, 1, 0), "real selftest defect must remain one failing local check")
    c10.record(bad, "ci-local", "finding", declarations=5, failed=1, unmet=0)
    c9.capture(bad); c9.check("САМОПРОВЕРКА ПРОВАЛЕНА" in bad["stdout"] or "ПРОВАЛ" in bad["stdout"], "genuine selftest failure missing from actual caller")
    mixed_helm_case(h, root, c10)
    c11 = h.case("CI_PY_11_record_authenticity")
    caller_record_cases(h, root, c11, [direct, made])
    c12 = h.case("CI_PY_12_CI_collector_and_final_verdict")
    ci_steps(h, root, c12)


def caller_cases(h):
    document = workflow(h)
    actual = caller_inventory(document)
    c = h.case("CI_PY_13_all_actual_callers")
    c.check(len(actual) > 0, "caller inventory is empty")
    c.check(any(x["kind"] == "python" for x in actual), "ordinary/self-test CI caller not discovered")
    c.check(any("gate-self-test" in shell_tokens(x["step"]["run"]) for x in actual), "Make selftest caller not discovered")
    c.check(any("helm-manifest-test" in shell_tokens(x["step"]["run"]) for x in actual), "manifest prerequisite caller not discovered")
    # The paired inventory controls change one executable fact, not the matching
    # string: a comment and echo argument must not recreate a missing invocation.
    for entry in actual:
        changed = json.loads(json.dumps(document))
        step = changed["jobs"][entry["job"]]["steps"][entry["index"]]
        step["run"] = "\n".join("# " + line for line in step["run"].splitlines())
        c.check(len(caller_inventory(changed)) == len(actual)-1, f"comment impersonates caller {entry['job']}/{entry['index']}")
    for body in ("# python3 "+RUNNER, "echo "+RUNNER, "echo python3 "+RUNNER, "python3 -m pip install pytest"):
        fake = {"jobs":{"fake":{"steps":[{"run":body}]}}}
        c.check(not caller_inventory(fake), "comment/mention/dependency setup counted as execution")
    # Inspect same-job setup, so installing pytest in a neighbouring job cannot
    # discharge the Python execution prerequisite.
    for entry in actual:
        job = document["jobs"][entry["job"]]
        installs = [i for i,s in enumerate(job["steps"][:entry["index"]]) if "pytest" in shell_tokens(s.get("run", "")) and "install" in shell_tokens(s.get("run", ""))]
        c.check(bool(installs), f"{entry['job']}/{entry['index']}: pytest prerequisite not created in its own job")
    checked_tools(h)
    root = h.tracked_copy("caller-tree")
    for number, entry in enumerate(actual):
        result = c.capture(run_workflow_body(h, root, entry, "actual-caller-"+str(number)))
        require_positive(result)
        c.check(result["record"] is not None or entry["kind"] == "python" and "python-probes-collect" == entry["step"].get("id"), f"actual caller {entry['job']}/{entry['index']} does not produce a structured current outcome")
    c.check(any(s.get("id") == "python-probes-verdict" and "always()" in str(s.get("if", "")) for job in document["jobs"].values() for s in job["steps"]), "final Python completeness reader missing from workflow")


def main():
    group, root, work = sys.argv[1:]
    os.environ["CI_PY_GROUP"] = group
    h = None
    try:
        h = Harness(root, work)
        {"producer": producer_cases, "chain": chain_cases, "callers": caller_cases}[group](h)
        report = {"prerequisite_error":"", "cases":[vars(c) for c in h.cases]}
    except Exception as error:
        report = {"prerequisite_error":f"{type(error).__name__}: {error}", "cases":[vars(c) for c in h.cases] if h else []}
    print(json.dumps(report, ensure_ascii=False))


if __name__ == "__main__":
    main()
