#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# Обязательная полоса CI-PY-1: единая команда для Make, local и CI.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT" || exit 1
work="$(mktemp -d "${TMPDIR:-/tmp}/python-outcomes-integration.XXXXXXXX")" || exit 1
events="${CI_PY_INTEGRATION_EVENTS:-$work/events.jsonl}"
# Новый файл принадлежит ровно этому вызову; старый результат не переиспользуется.
(set -o noclobber; : > "$events") || exit 1
unset KACHO_CI_OUTCOME_FILE KACHO_CI_INVOCATION_ID
export TMPDIR="${TMPDIR:-/tmp}"
rc=0
GOWORK=off go test -tags=ci_integration ./tools/pythonprobes -run '^TestPythonOutcomes' -count=1 -json -timeout=90m > "$events" 2> "$work/go.stderr" || rc=$?
cat "$events"
cat "$work/go.stderr" >&2
verdict=0
python3 .github/scripts/ci_outcomes.py integration-verdict --events "$events" || verdict=$?
printf 'Свидетельство Python outcomes: %s\n' "$events"
[ "$rc" -eq 0 ] && [ "$verdict" -eq 0 ]
