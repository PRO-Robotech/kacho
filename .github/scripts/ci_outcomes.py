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
