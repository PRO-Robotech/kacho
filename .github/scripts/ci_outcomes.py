#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Общий результат CI: находка и отсутствие исполнения не подменяют друг друга."""
from __future__ import annotations

from dataclasses import dataclass, field
import json
import os
from pathlib import Path
import tempfile


UNITS = {
    "run-python-probes": "python-probe",
    "run-python-probes/self-test": "self-test-check",
    "gate-self-test": "self-test",
    "ci-local": "local-check",
}


@dataclass
class Outcome:
    producer: str
    files: int = 0
    declarations: int = 0
    executed: int = 0
    failed: int = 0
    skipped: int = 0
    pytest_declarations: int = 0
    script_entries: int = 0
    findings: list[dict[str, str]] = field(default_factory=list)
    unmet_reasons: list[dict[str, str]] = field(default_factory=list)

    def finding(self, code: str, coordinate: str, message: str) -> None:
        self.findings.append({"producer": self.producer, "code": code,
                              "coordinate": coordinate, "message": message})

    def unmet(self, code: str) -> None:
        self.unmet_reasons.append({"producer": self.producer, "code": code})

    @property
    def category(self) -> str:
        if self.failed or self.skipped or self.findings:
            return "finding"
        return "unmet" if self.unmet_reasons else "green"

    @property
    def returncode(self) -> int:
        return {"green": 0, "finding": 1, "unmet": 2}[self.category]

    def record(self, invocation_id: str) -> dict:
        counters = {key: getattr(self, key) for key in
                    ("files", "declarations", "executed", "failed", "skipped")}
        counters["unmet"] = len(self.unmet_reasons)
        if self.producer == "run-python-probes":
            counters.update(pytest_declarations=self.pytest_declarations,
                            script_entries=self.script_entries)
            if self.declarations != self.pytest_declarations + self.script_entries:
                raise ValueError("inconsistent declarations")
        if any(type(value) is not int or value < 0 for value in counters.values()):
            raise ValueError("invalid counters")
        if self.failed > self.executed:
            raise ValueError("failed exceeds executed")
        if self.category == "green" and self.executed == 0:
            raise ValueError("empty execution is not green")
        if not invocation_id:
            raise ValueError("invocation id is required")
        return {"schema_version": 1, "invocation_id": invocation_id,
                "producer": self.producer, "category": self.category,
                "unit": UNITS[self.producer], "counters": counters,
                "findings": self.findings, "unmet_reasons": self.unmet_reasons}

    def write(self, path: Path, invocation_id: str) -> None:
        """Читатель видит либо всю новую запись, либо прежнюю со старым ID."""
        payload = json.dumps(self.record(invocation_id), ensure_ascii=False) + "\n"
        fd, temporary = tempfile.mkstemp(prefix=".ci-outcome-", dir=path.parent)
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as stream:
                stream.write(payload)
            os.replace(temporary, path)
        finally:
            Path(temporary).unlink(missing_ok=True)


def read(path: Path, invocation_id: str, producer: str) -> dict:
    """Проверка границы: код Make сам по себе не объясняет отказ рецепта."""
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate field")
            result[key] = value
        return result

    data = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique)
    if not isinstance(data, dict) or set(data) != {
        "schema_version", "invocation_id", "producer", "category", "unit",
        "counters", "findings", "unmet_reasons",
    }:
        raise ValueError("invalid record fields")
    if (type(data["schema_version"]) is not int or data["schema_version"] != 1
            or not invocation_id or data["invocation_id"] != invocation_id
            or producer not in UNITS or data["producer"] != producer
            or data["unit"] != UNITS[producer]):
        raise ValueError("foreign record")
    counts = data["counters"]
    expected = {"files", "declarations", "executed", "failed", "skipped", "unmet"}
    if producer == "run-python-probes":
        expected |= {"pytest_declarations", "script_entries"}
    if not isinstance(counts, dict) or set(counts) != expected:
        raise ValueError("invalid counters")
    if any(type(v) is not int or v < 0 for v in counts.values()):
        raise ValueError("invalid count")
    if counts["failed"] > counts["executed"]:
        raise ValueError("failed exceeds executed")
    if producer == "run-python-probes" and counts["declarations"] != counts["pytest_declarations"] + counts["script_entries"]:
        raise ValueError("inconsistent declarations")
    for key, fields in (("findings", {"producer", "code", "coordinate", "message"}),
                        ("unmet_reasons", {"producer", "code"})):
        if not isinstance(data[key], list):
            raise ValueError("invalid reasons")
        for item in data[key]:
            if not isinstance(item, dict) or set(item) != fields or any(
                    not isinstance(value, str) or not value for value in item.values()):
                raise ValueError("invalid reason")
    if counts["unmet"] != len(data["unmet_reasons"]):
        raise ValueError("inconsistent unmet")
    category = "finding" if counts["failed"] or counts["skipped"] or data["findings"] else (
        "unmet" if data["unmet_reasons"] else "green")
    if data["category"] != category:
        raise ValueError("inconsistent category")
    if category == "green" and (not counts["executed"] or not counts["declarations"]
                                 or counts["executed"] < counts["declarations"]):
        raise ValueError("empty green")
    if producer in {"gate-self-test", "ci-local", "run-python-probes/self-test"}:
        if counts["executed"] > counts["declarations"]:
            raise ValueError("inconsistent execution")
        if category == "green" and counts["executed"] != counts["declarations"]:
            raise ValueError("incomplete green")
    return data


def shell_record(args) -> int:
    """Shell передаёт измеренные единицы; причины ребёнка сохраняются отдельно."""
    outcome = Outcome(args.producer, files=args.files, declarations=args.declarations,
                      executed=args.executed, failed=len(args.failure))
    for coordinate in args.failure:
        outcome.finding("check-failed", coordinate, "проверка дала находку")
    for coordinate in args.unmet:
        outcome.unmet("condition-unmet:" + coordinate)
    if args.child_file:
        child = read(Path(args.child_file), args.child_id, args.child_producer)
        # failed считает единицы родителя, а вложенная причина сохраняет
        # конкретный гейт, который произвёл находку.
        outcome.findings.extend(child["findings"])
        # Одна отсутствующая предпосылка ребёнка не превращается в отсутствие
        # всех ещё не перечисленных pytest instances.
        if child["unmet_reasons"]:
            outcome.unmet_reasons = [item for item in outcome.unmet_reasons
                                     if item["code"] != "condition-unmet:" + args.child_coordinate]
            outcome.unmet_reasons.extend(child["unmet_reasons"])
    outcome.write(Path(args.outcome_file), args.invocation_id)
    return 0



def integration_verdict(path: Path) -> None:
    """Имена семейства из принятого маршрута, факты только из текущего JSONL."""
    parents = {"TestPythonOutcomesProducer", "TestPythonOutcomesChain", "TestPythonOutcomesCallers"}
    expected = {f"CI_PY_{number:02d}_" for number in range(1, 14)}
    started, passed, scenarios = set(), set(), set()
    package_passed = False
    for line in path.read_text(encoding="utf-8").splitlines():
        event = json.loads(line)
        if not isinstance(event, dict):
            raise ValueError("invalid test event")
        action, name = event.get("Action"), event.get("Test")
        if action in {"fail", "skip"}:
            raise ValueError("test failure or skip")
        if not name:
            if action == "pass" and event.get("Package", "").endswith("/tools/pythonprobes"):
                package_passed = True
            continue
        if name.split("/")[0] not in parents:
            raise ValueError("foreign test family")
        if action == "run":
            if name in started:
                raise ValueError("duplicate test")
            started.add(name)
        elif action == "pass":
            if name not in started or name in passed:
                raise ValueError("unbound terminal event")
            passed.add(name)
            if "/" in name:
                prefix = name.split("/", 1)[1][:9]
                if prefix not in expected or prefix in scenarios:
                    raise ValueError("unexpected scenario")
                number = int(prefix[6:8])
                owner = "TestPythonOutcomesProducer" if number <= 8 else (
                    "TestPythonOutcomesChain" if number <= 12 else "TestPythonOutcomesCallers")
                if name.split("/")[0] != owner:
                    raise ValueError("foreign scenario owner")
                scenarios.add(prefix)
    if not package_passed or started != passed or not parents <= passed or scenarios != expected or len(passed) != 16:
        raise ValueError("incomplete test family")
    print("Python outcomes: родителей 3/3, сценариев 13/13 PASS, пропусков 0")

def main() -> int:
    import argparse
    import sys

    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    reader = commands.add_parser("read")
    for name in ("outcome-file", "invocation-id", "producer"):
        reader.add_argument("--" + name, required=True)
    reader.add_argument("--format", choices=("json", "status"), default="json")
    writer = commands.add_parser("write-shell")
    for name in ("outcome-file", "invocation-id", "producer"):
        writer.add_argument("--" + name, required=True)
    for name in ("files", "declarations", "executed"):
        writer.add_argument("--" + name, required=True, type=int)
    writer.add_argument("--failure", action="append", default=[])
    writer.add_argument("--unmet", action="append", default=[])
    writer.add_argument("--child-file")
    writer.add_argument("--child-id")
    writer.add_argument("--child-producer")
    writer.add_argument("--child-coordinate", default="")
    complete = commands.add_parser("require-complete")
    complete.add_argument("--directory", required=True)
    complete.add_argument("--invocation-id", required=True)
    integration = commands.add_parser("integration-verdict")
    integration.add_argument("--events", required=True)
    args = parser.parse_args()
    try:
        if args.command == "integration-verdict":
            integration_verdict(Path(args.events))
            return 0
        if args.command == "write-shell":
            return shell_record(args)
        if args.command == "require-complete":
            for name, producer in (("ordinary", "run-python-probes"),
                                   ("self-test", "run-python-probes/self-test")):
                result = read(Path(args.directory) / (name + ".json"), args.invocation_id, producer)
                if result["category"] != "green":
                    raise ValueError("incomplete Python verdict")
            print("Python: обычные пробы и самопроверка исполнены полностью")
            return 0
        result = read(Path(args.outcome_file), args.invocation_id, args.producer)
        if args.format == "status":
            pytest_missing = any(item["code"] == "pytest-unavailable" for item in result["unmet_reasons"])
            print(result["category"], int(bool(result["unmet_reasons"])), int(pytest_missing))
        else:
            print(json.dumps(result, ensure_ascii=False))
        return 0
    except (OSError, ValueError, KeyError, TypeError):
        print("ОТКАЗ: результат отсутствует, непригоден или неполон", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
