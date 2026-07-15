#!/usr/bin/env bash
set -euo pipefail
kubectl -n kacho wait --for=condition=ready pod -l app.kubernetes.io/component=controller --timeout=180s

# Используем --resolve чтобы не зависеть от /etc/hosts.
# 200 (api-gateway healthy) / 503 (still starting) / 404 (not deployed) — OK.
CODE=$(curl -sS -o /dev/null -w '%{http_code}' --resolve api.kacho.local:80:127.0.0.1 \
  http://api.kacho.local/ -H 'Host: api.kacho.local' 2>/dev/null || true)
CODE=${CODE:-000}
case "$CODE" in
  200|301|302|404|503) echo "PASS: E6 — ingress responded with $CODE";;
  *) echo "FAIL: E6 — unexpected code [$CODE]"; exit 1;;
esac
