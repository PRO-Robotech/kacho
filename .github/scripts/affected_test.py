# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Пробы выбора затронутого (kacho#3148): инъекция настоящим входом дерева — карта и
процессы те же, что исполняет конвейер, — и законный близнец рядом с каждым дефектом.

Исполняются шагом `authz-artifacts` в `ci.yaml` (pytest) и локально:
`python3 -m pytest -q .github/scripts/affected_test.py`.
"""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent


def _load(file: str, name: str):
    spec = importlib.util.spec_from_file_location(name, HERE / file)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[name] = mod
    spec.loader.exec_module(mod)
    return mod


affected = _load("affected.py", "affected")
pr_verdict = _load("pr-verdict.py", "pr_verdict_for_affected")

MAP = affected.load_map()
WORKFLOWS = HERE.parent / "workflows"
NEWMAN_CASE = "services/vpc/tests/newman/cases/subnet.py"
GO_FILE = "services/vpc/internal/handler/subnet.go"


def _local_titles(sel) -> list[str]:
    real = affected.go_packages
    affected.go_packages = lambda files, with_dependents: (
        {affected.ROOT: ["./services/vpc/internal/handler"]}
        if any(f.endswith(".go") for f in files) else {})
    try:
        steps, stands = affected.plan_local(sel, "origin/1266")
    finally:
        affected.go_packages = real
    return [" ".join(s.argv) for s in steps] + [f"STAND {x}" for x in stands]


class Selection(unittest.TestCase):
    def test_newman_case_selects_only_newman_and_its_tree_gates(self):
        sel = affected.select(MAP, [NEWMAN_CASE])
        self.assertFalse(sel.full)
        self.assertEqual({g.id for g in sel.groups},
                         {"go-unit", "tree-gates-newman", "newman-suites", "carry-drift"})
        for job in ("ci.yaml:build-test", "ci.yaml:lint", "ci.yaml:integration",
                    "ci.yaml:proto", "ci.yaml:terraform", "security-scan.yml:gosec",
                    "ui.yml:build", "console-e2e.yml:probes", "production-posture.yml:stand"):
            self.assertNotIn(job, sel.jobs, job)
        local = _local_titles(sel)
        self.assertTrue(any("check-newman-suite-gates.py" in t for t in local), local)
        self.assertTrue(any("artifactgates" in t for t in local), local)
        for alien in ("go vet", "golangci-lint", "helm-lint", "buf lint", "govulncheck"):
            self.assertFalse(any(alien in t for t in local), (alien, local))

    def test_go_package_selects_its_tests_and_linter(self):
        sel = affected.select(MAP, [GO_FILE])
        self.assertFalse(sel.full)
        self.assertIn("go-build", {g.id for g in sel.groups})
        self.assertIn("ci.yaml:lint", sel.jobs)
        local = _local_titles(sel)
        self.assertIn("go test -race -count=1 ./services/vpc/internal/handler", local)
        self.assertIn("golangci-lint run --timeout=10m ./services/vpc/internal/handler/", local)
        self.assertNotIn("ci.yaml:docs-sites", sel.jobs)
        self.assertFalse(any("check-newman-suite-gates" in t for t in local), local)

    def test_empty_diff_is_void_not_green(self):
        with tempfile.NamedTemporaryFile("w", suffix=".txt") as empty, \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(affected.main(["--files-from", empty.name]), affected.VOID)
            self.assertEqual(affected.main(["--files-from", empty.name, "--run"]), affected.VOID)

    def test_selection_map_change_selects_everything(self):
        sel = affected.select(MAP, [".github/ci-paths.json"])
        self.assertTrue(sel.full)
        self.assertEqual(sorted(MAP.all_jobs()), sel.jobs)

    def test_unclaimed_path_selects_everything(self):
        m = affected.PathMap(full_bases=["main"], full_on=[], always=[], detectors={},
                             groups=[affected.Group("g", "", ["a.yml:x"], ["src/**"])])
        self.assertTrue(affected.select(m, ["elsewhere/file"]).full)
        self.assertFalse(affected.select(m, ["src/file"]).full)  # законный близнец

    def test_workflow_file_selects_its_own_jobs(self):
        sel = affected.select(MAP, [".github/workflows/ui.yml"])
        self.assertIn("ui.yml:build", sel.jobs)
        self.assertNotIn("ui.yml:build", affected.select(MAP, [NEWMAN_CASE]).jobs)

    def test_glob(self):
        self.assertTrue(affected.matches("a.go", ["**/*.go"]))
        self.assertTrue(affected.matches("x/y/a.go", ["**/*.go"]))
        self.assertFalse(affected.matches("x/a.gox", ["**/*.go"]))
        self.assertTrue(affected.matches("services/vpc/deploy/a", ["services/*/deploy/**"]))
        self.assertFalse(affected.matches("services/vpc/x/deploy/a", ["services/*/deploy/**"]))


class EventMode(unittest.TestCase):
    def test_only_draft_into_epic_branch_is_targeted(self):
        self.assertFalse(affected.event_mode(MAP, "pull_request", "true", "1266")[0])
        self.assertTrue(affected.event_mode(MAP, "pull_request", "false", "1266")[0])
        self.assertTrue(affected.event_mode(MAP, "pull_request", "true", "main")[0])
        self.assertTrue(affected.event_mode(MAP, "push", "", "")[0])
        self.assertTrue(affected.event_mode(MAP, "workflow_dispatch", "", "")[0])

    def test_detector_on_ready_request_writes_full_without_network(self):
        with tempfile.NamedTemporaryFile("r", suffix=".out") as out:
            env = {"EVENT": "pull_request", "DRAFT": "false", "BASE": "1266",
                   "GITHUB_OUTPUT": out.name}
            old = {k: os.environ.get(k) for k in env}
            os.environ.update(env)
            try:
                with contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(affected.detector(MAP, "ci.yaml"), affected.GREEN)
            finally:
                for k, v in old.items():
                    os.environ.pop(k, None) if v is None else os.environ.__setitem__(k, v)
            self.assertIn("full=true", Path(out.name).read_text())


class Wiring(unittest.TestCase):
    def test_tree_wiring_matches_the_map(self):
        self.assertEqual(affected.check_wiring(MAP, WORKFLOWS), [])

    def _copy(self) -> Path:
        tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, tmp)
        shutil.copytree(WORKFLOWS, tmp / "wf")
        return tmp / "wf"

    def test_job_without_selection_condition_is_found(self):
        wf = self._copy()
        p = wf / "ci.yaml"
        text = p.read_text(encoding="utf-8")
        line = ("    if: ${{ needs.changes.outputs.full == 'true' || contains(fromJSON("
                "needs.changes.outputs.jobs || '[]'), 'lint') }}\n")
        self.assertIn(line, text)
        p.write_text(text.replace(line, ""), encoding="utf-8")
        found = affected.check_wiring(MAP, wf)
        self.assertEqual(len(found), 1, found)
        self.assertIn("ci.yaml:lint", found[0])

    def test_job_missing_from_map_is_found(self):
        m = affected.load_map()
        for g in m.groups:
            g.jobs = [j for j in g.jobs if j != "ci.yaml:docs-sites"]
        m.groups = [g for g in m.groups if g.jobs]
        found = affected.check_wiring(m, WORKFLOWS)
        self.assertTrue(any("ci.yaml:docs-sites" in f and "ни в одной группе" in f
                            for f in found), found)

    def test_map_entry_without_job_is_found(self):
        m = affected.load_map()
        m.groups[0].jobs.append("ci.yaml:no-such-job")
        found = affected.check_wiring(m, WORKFLOWS)
        self.assertTrue(any("ci.yaml:no-such-job" in f and "без предмета" in f for f in found),
                        found)


def _run(name: str, conclusion: str, suite: int, rid: int) -> dict:
    return {"id": rid, "name": name, "status": "completed", "conclusion": conclusion,
            "app": {"slug": "github-actions"}, "check_suite": {"id": suite}}


class DraftVerdict(unittest.TestCase):
    def _declared(self):
        J = pr_verdict.JobName
        return pr_verdict.Declared(checks=(J("ci.yaml: jobs.lint", "golangci-lint", None),
                                           J("ci.yaml: jobs.helm", "helm", None),
                                           J("ci.yaml: jobs.changes", "выбор (ci)", None)),
                                   workflows=1, on_request=1, breaches=())

    def test_draft_with_green_selection_is_red_and_says_n_of_m(self):
        d, drafted = pr_verdict.narrow_to_draft(self._declared(), {
            "full": False, "jobs": ["ci.yaml:lint"], "always": ["ci.yaml:changes"]})
        runs = [_run("golangci-lint", "success", 1, 1), _run("helm", "skipped", 1, 2),
                _run("выбор (ci)", "success", 1, 3)]
        v = pr_verdict.draft_verdict(pr_verdict.decide(runs, d, "", frozenset(
            {"ci.yaml:lint", "ci.yaml:helm"})), drafted)
        self.assertEqual(v.state, pr_verdict.RED)
        self.assertIn("целевой прогон: 2 заданий из 3", v.reason)
        self.assertIn("целевой зелёный", v.reason)

    def test_full_run_does_not_take_a_draft_skip_for_presence(self):
        runs = [_run("golangci-lint", "success", 1, 1), _run("helm", "skipped", 1, 2),
                _run("выбор (ci)", "success", 1, 3)]
        gated = frozenset({"ci.yaml:lint", "ci.yaml:helm"})
        v = pr_verdict.decide(runs, self._declared(), "", gated)
        self.assertEqual(v.state, pr_verdict.NOT_READY)
        self.assertIn("helm", v.missing)
        # законный близнец: прогон с исходом после пропуска — зелено
        v = pr_verdict.decide(runs + [_run("helm", "success", 2, 4)], self._declared(), "", gated)
        self.assertEqual(v.state, pr_verdict.GREEN)


if __name__ == "__main__":
    unittest.main()
