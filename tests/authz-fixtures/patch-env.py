#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Merges authz-fixtures.json into newman *.postman_environment.json files.

Usage:
    patch-env.py <fixtures.json> <env1.json> [<env2.json> ...]

Сохраняет старые ключи; добавляет / обновляет ключи из fixtures.
"""
from __future__ import annotations

import json
import pathlib
import sys


def patch_env(env_path: pathlib.Path, fixtures: dict) -> int:
    """Return number of keys added/updated."""
    data = json.loads(env_path.read_text())
    values = data.setdefault("values", [])
    existing = {v["key"]: v for v in values}
    changes = 0
    for key, val in fixtures.items():
        sval = "" if val is None else str(val)
        if key in existing:
            if existing[key].get("value") != sval:
                existing[key]["value"] = sval
                existing[key]["enabled"] = True
                changes += 1
        else:
            values.append({"key": key, "value": sval, "type": "default", "enabled": True})
            changes += 1
    env_path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n")
    return changes


def main() -> int:
    if len(sys.argv) < 3:
        print("usage: patch-env.py <fixtures.json> <env1.json> [<env2.json> ...]", file=sys.stderr)
        return 2
    fixtures_path = pathlib.Path(sys.argv[1])
    fixtures = json.loads(fixtures_path.read_text())
    for env_arg in sys.argv[2:]:
        env_path = pathlib.Path(env_arg)
        if not env_path.exists():
            print(f"[patch-env] SKIP (missing): {env_path}", file=sys.stderr)
            continue
        n = patch_env(env_path, fixtures)
        # display friendly path without crashing on /tmp/ paths outside cwd
        try:
            disp = env_path.relative_to(env_path.cwd()) if env_path.is_absolute() else env_path
        except ValueError:
            disp = env_path
        print(f"[patch-env] {disp}: {n} keys", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
