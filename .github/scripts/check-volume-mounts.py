#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Анализатор одного helm-рендера: каждый volumeMount обязан иметь свой volume.

Читает манифесты со stdin, печатает найденные расхождения (пусто = чисто),
код возврата 1 при находках, 2 при ошибке разбора.

Вынесен из bash-обёртки отдельным файлом: inline-python в шелле уже дал два дефекта —
`python3 - <<'PY' <<<"$out"` (второй redirect перекрывает первый, python читает ДАННЫЕ
как скрипт) и молчаливое «✓» при падении анализатора.
"""
import sys

import yaml

WORKLOADS = ("Deployment", "StatefulSet", "Job", "DaemonSet", "CronJob")


def pod_spec(doc):
    """Достаёт podSpec из любого workload-типа (CronJob вложен глубже)."""
    spec = doc.get("spec") or {}
    if doc.get("kind") == "CronJob":
        spec = ((spec.get("jobTemplate") or {}).get("spec")) or {}
    tmpl = spec.get("template") or {}
    return tmpl.get("spec") or {}


def main() -> int:
    try:
        docs = list(yaml.safe_load_all(sys.stdin))
    except yaml.YAMLError as e:
        print(f"не удалось разобрать YAML: {e}", file=sys.stderr)
        return 2

    bad = []
    for d in docs:
        if not isinstance(d, dict) or d.get("kind") not in WORKLOADS:
            continue
        sp = pod_spec(d)
        vols = {v["name"] for v in (sp.get("volumes") or []) if isinstance(v, dict) and "name" in v}
        containers = (sp.get("containers") or []) + (sp.get("initContainers") or [])
        for c in containers:
            mounts = {m["name"] for m in (c.get("volumeMounts") or []) if isinstance(m, dict) and "name" in m}
            missing = mounts - vols
            if missing:
                name = (d.get("metadata") or {}).get("name", "?")
                bad.append(f"{d['kind']} {name} / контейнер {c.get('name', '?')}: "
                           f"монт без тома {sorted(missing)} (есть тома: {sorted(vols) or 'нет'})")

    for line in bad:
        print(line)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
