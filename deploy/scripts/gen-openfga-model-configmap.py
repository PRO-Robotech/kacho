# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#!/usr/bin/env python3
"""Regenerate the openfga-model-stub ConfigMap from the canonical FGA model.

KAC-127 RC-2b. The canonical authorization model lives in
`kacho-proto/proto/kacho/cloud/iam/v1/fga_model.fga`. This script copies that
DSL into the Helm ConfigMap's `model.fga` block byte-for-byte and writes the
`fga model transform` output into the `model.json` block, so both stay in sync
with the single canonical source.

Usage:
    gen-openfga-model-configmap.py <configmap.yaml> <canonical.fga> [openfga-cli-image]
"""
import json
import os
import subprocess
import sys


def main() -> None:
    if len(sys.argv) < 3:
        sys.exit("usage: gen-openfga-model-configmap.py <configmap.yaml> "
                 "<canonical.fga> [openfga-cli-image]")
    cm = sys.argv[1]
    canonical = sys.argv[2]
    cli_image = sys.argv[3] if len(sys.argv) > 3 else "openfga/cli:v0.7.13"

    if not os.path.exists(canonical):
        sys.exit("canonical fga_model.fga not found: " + canonical)
    if not os.path.exists(cm):
        sys.exit("configmap template not found: " + cm)

    dsl_text = open(canonical).read().rstrip("\n") + "\n"

    # Transform DSL -> compact OpenFGA-JSON via openfga/cli. The image is
    # distroless (no shell) so the transform happens here at commit-time
    # instead of in a runtime init-container.
    out = subprocess.run(
        ["docker", "run", "--rm", "-i", cli_image, "model", "transform",
         dsl_text, "--input-format", "fga", "--output-format", "json"],
        capture_output=True, text=True,
    )
    if out.returncode != 0:
        sys.exit("fga model transform failed: " + out.stderr)
    compact = json.dumps(json.loads(out.stdout), separators=(",", ":"))

    lines = open(cm).read().splitlines()
    data_idx = next(i for i, l in enumerate(lines) if l.rstrip() == "data:")
    end_idx = next(i for i, l in enumerate(lines) if l.strip() == "{{- end }}")

    fga_block = "\n".join(("    " + ln) if ln else ""
                          for ln in dsl_text.rstrip("\n").split("\n"))
    data = [
        "data:",
        "  model.fga: |-",
        fga_block,
        "",
        "  # Pre-transformed OpenFGA-JSON. Generated from the canonical",
        "  # kacho-proto fga_model.fga via `make openfga-model-json`. Do NOT edit",
        "  # by hand — the bootstrap-job consumes this verbatim.",
        "  model.json: |-",
        "    " + compact,
    ]
    res = lines[:data_idx] + data + lines[end_idx:]
    open(cm, "w").write("\n".join(res) + "\n")
    print("openfga-model-stub configmap regenerated "
          "(model.json %d bytes compact)" % len(compact))


if __name__ == "__main__":
    main()