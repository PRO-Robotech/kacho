#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Process fault injection for RG1.1; never restores the subject's configuration."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

import yaml

command, *args = sys.argv[1:]
config = Path(os.environ["RG11_CONFIG"])
state = Path(os.environ["RG11_STATE"])
mode = os.environ["RG11_FAULT"]


def record(kind, **values):
    with (state / "events.jsonl").open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(dict(kind=kind, **values), ensure_ascii=False) + "\n")


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def forward():
    os.execv(os.environ["RG11_REAL_" + command.upper()], [command, *args])


if command == "mktemp":
    if mode == "create_failure":
        record("create_failure")
        print("RG11 fixture: mktemp refused", file=sys.stderr)
        sys.exit(73)
    result = subprocess.run([os.environ["RG11_REAL_MKTEMP"], *args], capture_output=True)
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    if result.returncode == 0:
        snapshot = result.stdout.decode().strip()
        (state / "snapshot-path").write_text(snapshot)
        record("created", path=snapshot)
    sys.exit(result.returncode)

if command == "cp":
    # Paths are read from the actual external call, not the product's variable names.
    operands = [arg for arg in args if arg != "--"]
    if len(operands) != 2:
        raise RuntimeError("unexpected cp call: " + repr(args))
    source, target = operands
    if Path(source) == config:
        if mode in ("copy_failure", "partial_snapshot"):
            if mode == "partial_snapshot":
                Path(target).write_bytes(config.read_bytes()[:31])
            record(mode, path=target)
            print("RG11 fixture: cp refused", file=sys.stderr)
            sys.exit(74)
        result = subprocess.run([os.environ["RG11_REAL_CP"], *args])
        if result.returncode == 0:
            record("prepared", path=target, sha256=digest(target))
        sys.exit(result.returncode)
    if Path(target) == config:
        record("restore_attempt", path=source)
        if mode == "restore_failure":
            record("restore_failure", path=source)
            print("RG11 fixture: cp refused", file=sys.stderr)
            sys.exit(74)
    forward()

if command == "python3":
    gate = Path(args[0]).name if args else ""
    if gate not in ("assert-scan-stubs-hide-nothing.py", "assert-iac-scan-covers-every-chart.py"):
        forward()
    content = yaml.safe_load(config.read_text()) or {}
    stubs = ((content.get("misconfiguration") or {}).get("helm") or {}).get("set") or []
    original = yaml.safe_load((state / "original").read_text())
    original_stubs = original["misconfiguration"]["helm"]["set"]
    suppressed = [*original_stubs, "image.repository=gcr.io/trivy-scan-only"]
    if stubs == original_stubs:
        phase, code, output = "original", 0, "погашено 0"
    elif stubs == suppressed:
        phase, code, output = "suppressed", 1, "KSV-0125 services/nlb/deploy/templates/deployment.yaml"
    elif stubs == ["trivyStubsProbe=true"]:
        phase, code, output = "no_subject", 1, "не меняет НИЧЕГО"
    elif stubs == []:
        phase, code, output = "empty", 0, "судить нечего"
    else:
        record("invalid_fixture_input", stubs=stubs)
        raise RuntimeError("unexpected actual scan input")
    if gate == "assert-iac-scan-covers-every-chart.py":
        if phase != "suppressed":
            raise RuntimeError("coverage probe did not receive the suppressing injection")
        code, output = 0, "coverage fixture: targets preserved"
    elif mode == "assertion_failure" and phase == "suppressed" and not (state / "fault-used").exists():
        (state / "fault-used").write_text("once")
        code = 2
    record("scan", gate=gate, phase=phase, code=code, sha256=digest(config))
    print(output)
    sys.exit(code)

raise RuntimeError("unexpected fixture command: " + command)
