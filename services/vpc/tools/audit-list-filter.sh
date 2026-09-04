#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# audit-list-filter.sh — CI gate for kacho-vpc's public List<Resource>: the page
# a caller receives must be narrowed to the rows that caller may see (per-object,
# kacho-iam BatchCheck), not merely to their project.
#
# This is a THIN wrapper. What is checked, and why the analysis parses the tree
# instead of searching its text, is documented on pkg/listfiltergate; how this
# service is laid out is documented on services/vpc/tools/auditlistfilter. Only
# the invocation lives here.
#
# Why the analysis is no longer this file. The previous edition collected candidates
# with `grep -rl 'func .* List(' --include='handler.go'` — it recognised a resource by
# a FILE being called handler.go — and then searched that file and its sibling
# list.go for the filter's name. Renaming handler.go removed the resource from its
# view entirely, and a text search cannot tell a call from a sentence describing one
# (nor from an interface method declaration), so deleting the filter while leaving
# its comment kept the gate green. On a tree without internal/apps/kacho/api it
# printed a message and exited 0, so "could not find the tree" and "the tree is
# clean" were the same verdict.
#
# Arguments are forwarded as-is:
#   --allow=<resource>  exclude a cluster-catalog resource; an entry with nothing
#                       left to exclude is itself a finding.
#   --root=<dir>        audit another tree (used by the gate's own tests).

set -euo pipefail

# The service root is resolved BEFORE changing directory, and passed explicitly:
# `go run ./services/…` has to be issued from the module root, so the default
# "audit the current directory" would otherwise audit the repository instead of this
# service. A --root given by the caller appears later on the command line and wins.
SERVICE_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$(dirname "$0")/../../.."

exec go run ./services/vpc/tools/auditlistfilter/cmd/audit-list-filter \
  --root="$SERVICE_ROOT" "$@"
