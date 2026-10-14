#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Страж отображения портов узла kind (deploy/kind/assert-port-mappings.sh,
# kacho#3025) различает три исхода — и каждый проверяется НАСТОЯЩИМ скриптом
# против НАСТОЯЩЕГО объявления kind-config.yaml, с подменённым `docker`:
#   (1) узел публикует все объявленные отображения → 0, названо их число;
#   (2) кластер создан до отображения TLS (порт 443 не опубликован) → 1, назван
#       порт и как пересоздать — ровно случай стенда, поднятого прежним объявлением;
#   (3) узла нет → 2: «не опрошено» не выдаётся ни за «сходится», ни за находку.
# Офлайновая проверка, кластер не нужен.
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GUARD="$DEPLOY_ROOT/kind/assert-port-mappings.sh"
CONFIG="$DEPLOY_ROOT/kind/kind-config.yaml"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=3

require_file_present "$GUARD" "страж отображения портов узла kind"
require_file_present "$CONFIG" "объявление кластера kind"

DECLARED="$(grep -c 'containerPort:' "$CONFIG")"
[ "$DECLARED" -gt 0 ] || fatal "в $CONFIG ни одного отображения — предмет исчез"
grep -q 'containerPort: 443' "$CONFIG" || fatal "в $CONFIG нет отображения порта TLS 443 — случай (2) не на чем ставить"

TMPD="$(mktemp -d)"; trap 'rm -rf "$TMPD"' EXIT
mkdir -p "$TMPD/bin"
# Подменённый docker: узел существует, если STUB_NODE=1; `docker port` отвечает
# по объявлению, кроме портов из STUB_UNPUBLISHED.
cat >"$TMPD/bin/docker" <<STUB
#!/usr/bin/env bash
case "\$1" in
  inspect) [ "\${STUB_NODE:-1}" = 1 ] ;;
  port)
    cport="\${3%/tcp}"
    for u in \${STUB_UNPUBLISHED:-}; do [ "\$u" = "\$cport" ] && { echo "no public port" >&2; exit 1; }; done
    hport=\$(awk -v c="\$cport" '/containerPort:/{x=\$NF; next} /hostPort:/ && x==c {print \$NF; exit}' "$CONFIG")
    echo "0.0.0.0:\$hport" ;;
  *) exit 64 ;;
esac
STUB
chmod +x "$TMPD/bin/docker"

run() { PATH="$TMPD/bin:$PATH" bash "$GUARD" stub >"$TMPD/out" 2>&1; echo $?; }

echo "=== $SCRIPT: (1) все отображения опубликованы ==="
rc="$(run)"
[ "$rc" = 0 ] && grep -q "опубликованы все $DECLARED" "$TMPD/out" \
  || { cat "$TMPD/out"; fail "(1) исправный узел не дал 0 с числом отображений (код $rc)"; }
ok

echo "=== $SCRIPT: (2) кластер без отображения TLS ==="
rc="$(STUB_UNPUBLISHED=443 run)"
[ "$rc" = 1 ] && grep -q '443→28443' "$TMPD/out" && grep -q 'make dev-down && make dev-up' "$TMPD/out" \
  || { cat "$TMPD/out"; fail "(2) неопубликованный порт TLS не назван отказом 1 (код $rc)"; }
ok

echo "=== $SCRIPT: (3) узла нет ==="
rc="$(STUB_NODE=0 run)"
[ "$rc" = 2 ] || { cat "$TMPD/out"; fail "(3) отсутствие узла не дало «не опрошено» (код $rc)"; }
ok

outcome_verdict "отображений в объявлении: $DECLARED"
