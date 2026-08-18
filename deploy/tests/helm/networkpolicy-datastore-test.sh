#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# INFRA sec-hardening r5b — per-datastore Postgres ingress-allowlist NetworkPolicy.
#
# Each backing Postgres StatefulSet (bitnami `postgresql` sub-chart, aliased
# pg-<svc>, pod label app.kubernetes.io/name=pg-<svc>) is otherwise reachable on
# :5432 from every pod in the namespace (bitnami networkPolicy default = off).
# templates/networkpolicy-datastore.yaml adds a per-pg ingress allowlist so the
# pg pod implicitly denies all other ingress (CIS Kubernetes 5.3.2 / OWASP
# A05:2021 — lateral movement to DB credentials).
#
# Asserts:
#   1. Default-off: no datastore NetworkPolicy renders with base/dev values.
#   2. Opt-in: `networkPolicy.datastore.enabled=true` renders one Ingress
#      NetworkPolicy per instance, each selecting its pg pod (primary) and
#      allowing :5432 only from the declared consumer selectors.
#   3. Each policy is Ingress-only and scoped to a single pg pod (self-contained).
#
# Offline; contracts unchanged (helm-only). Mirrors tests/helm/*-test.sh.
set -euo pipefail
# line_in <многострочное значение> <строка> — есть ли СТРОКА ЦЕЛИКОМ в значении.
# Замена `grep -qx`/`grep -qxF`: под `pipefail` труба даёт ложный отказ НА
# СОВПАДЕНИИ, потому что писатель получает SIGPIPE (задача #658). Сравнение
# буквальное — там, где раньше стоял `-x` без `-F`, это СТРОЖЕ, то есть ложного
# зелёного добавить не может.
line_in() { [[ $'\n'"$1"$'\n' == *$'\n'"$2"$'\n'* ]]; }

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
DEV="$UMBRELLA/values.dev.yaml"
TMPL="templates/networkpolicy-datastore.yaml"
N=0
fail() { echo "FAIL: $1"; exit 1; }
ok() { N=$((N + 1)); }

# yq MUST be mikefarah v4. On many machines /usr/bin/yq is the python jq-wrapper
# of the SAME NAME: its filter syntax is incompatible, so it errors to stderr and
# prints NOTHING on stdout. Assertions shaped like `[ -z "$(yq ... 2>/dev/null)" ]`
# then pass VACUOUSLY — the check reports success having verified nothing at all.
# That false-green is the exact class these hardening tests exist to prevent, so
# detect the impostor explicitly instead of trusting `command -v`.
command -v yq >/dev/null 2>&1 || fail "yq not installed (mikefarah yq v4 required)"
# Сравнение — БЕЗ трубы: `… | grep -q` под `set -o pipefail` возвращает ОТКАЗ
# НА СОВПАДЕНИИ (grep выходит по первому попаданию, писатель получает SIGPIPE,
# и `pipefail` поднимает ЕГО статус до статуса конвейера). Задача #658.
YQ_VER="$(yq --version 2>&1 || true)"
[[ "${YQ_VER,,}" == *mikefarah* ]] || fail \
  "wrong 'yq' on PATH ($(command -v yq)): '$(yq --version 2>&1 | head -1)'. \
mikefarah yq v4 is required — the python-yq jq wrapper emits empty output on these \
filters, which would make the assertions below pass without checking anything."

render() {
  helm template kacho-umbrella "$UMBRELLA" -f "$DEV" "$@" \
    --show-only "$TMPL" 2>/dev/null
}

# ── 1. Default-off: nothing renders ──────────────────────────────────────────
OFF=$(render || true)
[ -z "$(echo "$OFF" | yq 'select(.kind == "NetworkPolicy") | .metadata.name' 2>/dev/null)" ] \
  || fail "datastore NetworkPolicy rendered while networkPolicy.datastore.enabled=false"
ok

# ── 2. Opt-in: one Ingress NetworkPolicy per pg instance ─────────────────────
ON=$(render --set networkPolicy.datastore.enabled=true)
[ -n "$ON" ] || fail "no datastore NetworkPolicy rendered with datastore.enabled=true"

# every rendered doc is a NetworkPolicy scoped to a single pg pod on :5432, ingress-only
COUNT=$(echo "$ON" | yq eval-all 'select(.kind == "NetworkPolicy") | .metadata.name' - | grep -c . || true)
[ "$COUNT" -ge 4 ] || fail "expected >=4 datastore NetworkPolicies, got $COUNT"
ok

check_instance() {
  local pg="$1" want_from="$2"
  local doc
  doc=$(echo "$ON" | yq eval-all "select(.kind == \"NetworkPolicy\" and .metadata.name == \"${pg}-ingress-allowlist\")" -)
  [ -n "$doc" ] || fail "$pg: NetworkPolicy ${pg}-ingress-allowlist not rendered"
  [ "$(echo "$doc" | yq '.spec.podSelector.matchLabels."app.kubernetes.io/name"')" = "$pg" ] \
    || fail "$pg: podSelector not scoped to app.kubernetes.io/name=$pg"
  [ "$(echo "$doc" | yq '.spec.podSelector.matchLabels."app.kubernetes.io/component"')" = "primary" ] \
    || fail "$pg: podSelector not scoped to component=primary"
  [ "$(echo "$doc" | yq '.spec.policyTypes | join(",")')" = "Ingress" ] \
    || fail "$pg: policyTypes must be Ingress-only"
  [ "$(echo "$doc" | yq '.spec.ingress[0].ports[0].port')" = "5432" ] \
    || fail "$pg: ingress port != 5432"
  local from_labels
  from_labels="$(echo "$doc" | yq '.spec.ingress[0].from[].podSelector.matchLabels | to_entries | .[] | .key + "=" + .value')"
  line_in "$from_labels" "$want_from" \
    || fail "$pg: ingress from does not include $want_from"
  ok
}

check_instance pg-vpc "app=vpc"
check_instance pg-compute "app=compute"
check_instance pg-iam "app=kacho-iam"
check_instance pg-geo "app=kacho-geo"
check_instance pg-openfga "app.kubernetes.io/name=openfga"
check_instance pg-nlb "app=kacho-nlb"

echo "$SCRIPT: all green ($N assertions)"
