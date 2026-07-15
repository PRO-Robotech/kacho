#!/usr/bin/env bash
set -euo pipefail
make dev-down >/dev/null 2>&1 || true
make dev-up
kubectl -n kacho exec kacho-umbrella-pg-compute-0 -- psql -U compute -d kacho_compute -c "CREATE TABLE t(x int); INSERT INTO t VALUES (1);"
make dev-down
make dev-up
COUNT=$(kubectl -n kacho exec kacho-umbrella-pg-compute-0 -- psql -U compute -d kacho_compute -tAc "SELECT count(*) FROM information_schema.tables WHERE table_name='t'")
[ "$COUNT" = "0" ] || { echo "FAIL: emptyDir not working — table 't' persisted"; exit 1; }
echo "PASS: E9 — emptyDir resets state on rebuild"
