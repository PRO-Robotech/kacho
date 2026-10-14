#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# guard-destructive без терминала: ЯВНОЕ подтверждение `CONFIRM`, отказ по умолчанию.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ (kacho#3064)
#
# `stack-up STACK=a8f60d` стережётся `guard-destructive`, а тот принимал
# подтверждение ТОЛЬКО вводом с терминала. Выкатка рабочего стенда исполнителем,
# у которого терминала нет, упиралась в «подтверждение ввести некому» — и
# единственным способом пройти оставалось набрать цепочку helm рукой, то есть
# ровно то, от чего `stack-up` и заведён.
#
# Ручка `CONFIRM` подтверждает операцию БЕЗ терминала, и подтверждает её
# дословно: значение обязано быть `<TOKEN>@<контекст>`, где контекст — тот, что
# разрешил живой kubectl. Поэтому:
#   • незаданная ручка — прежнее поведение: без терминала ОТКАЗ;
#   • токен без контекста, токен другой операции, контекст другого кластера —
#     ОТКАЗ, а не «спросим с терминала»: объявленное и неверное подтверждение
#     хуже отсутствующего, молча переходить к вводу оно не вправе;
#   • ровно `<TOKEN>@<контекст>` — подтверждено, цель исполняется.
#
# Кластер не нужен: подставной `kubectl` отвечает сам.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$(basename "$0")"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
# Проб ровно восемь: семь значений ручки плюс утверждение о ТЕКСТЕ отказа на
# неверном подтверждении.
EXPECTED_ASSERTIONS=8

good() { echo "  ✓ $1"; }

command -v make >/dev/null 2>&1 || fatal "нет make — цель guard-destructive запускать нечем"
require_file_present "$DEPLOY_ROOT/Makefile" "Makefile стенда"

TMP="$(mktemp -d)" || fatal "не создан временный каталог — подставной kubectl положить некуда"
trap 'rm -rf "$TMP"' EXIT

CTX="stub-cluster/stub-client"
mkdir -p "$TMP/bin"
cat >"$TMP/bin/kubectl" <<STUB
#!/usr/bin/env bash
case "\$*" in
  "config current-context") echo "$CTX" ;;
  *"context.cluster"*) echo "stub-cluster" ;;
  *"cluster.server"*) echo "https://stub.invalid:6443" ;;
  *"context.namespace"*) echo "" ;;
  *) exit 0 ;;
esac
STUB
chmod +x "$TMP/bin/kubectl"

N_RUN=0
run_guard() { # <значение CONFIRM | -unset-> → код возврата цели; stdin не терминал
  local val="$1" out code
  N_RUN=$((N_RUN+1))
  if [ "$val" = "-unset-" ]; then
    out="$(cd "$DEPLOY_ROOT" && env -u CONFIRM PATH="$TMP/bin:$PATH" \
          make --no-print-directory guard-destructive OP="probe-op" TOKEN="UP-probe" WHAT="проба" </dev/null 2>&1)"
  else
    out="$(cd "$DEPLOY_ROOT" && PATH="$TMP/bin:$PATH" \
          make --no-print-directory guard-destructive OP="probe-op" TOKEN="UP-probe" WHAT="проба" CONFIRM="$val" </dev/null 2>&1)"
  fi
  code=$?
  printf '%s\n' "$out" >"$TMP/out.$N_RUN"
  return $code
}

probe() { # <метка> <ожидание: red|green> <значение>
  local label="$1" want="$2" val="$3" got
  ok
  if run_guard "$val"; then got=green; else got=red; fi
  if [ "$got" = "$want" ]; then
    good "$label — $got, как и требуется"
  else
    violation "$label — цель дала $got, а обязана $want. Вывод цели:"
    sed 's/^/      /' "$TMP/out.$N_RUN"
  fi
}

echo "=== $SCRIPT: подтверждение guard-destructive без терминала ==="

probe "ручка не задана, терминала нет"            red   "-unset-"
probe "ручка задана пустой"                        red   ""
probe "только токен, без контекста"                red   "UP-probe"
probe "токен другой операции"                      red   "WIPE-IAM@$CTX"
probe "контекст другого кластера"                  red   "UP-probe@other/ctx"
probe "контекст с хвостом"                         red   "UP-probe@$CTX-x"
probe "ровно <TOKEN>@<контекст>"                   green "UP-probe@$CTX"
wrong_run=$((N_RUN-2))

ok
if grep -q 'CONFIRM' "$TMP/out.$wrong_run" 2>/dev/null && grep -q 'UP-probe@' "$TMP/out.$wrong_run" 2>/dev/null; then
  good "отказ на неверном подтверждении называет ручку и ожидаемую форму значения"
else
  violation "отказ на неверном подтверждении не называет ручку и форму — оператор не узнает, что ввести:"
  sed 's/^/      /' "$TMP/out.$wrong_run" 2>/dev/null | tail -6
fi

echo
echo "проверок исполнено: $N из $EXPECTED_ASSERTIONS"
outcome_verdict "значений ручки CONFIRM: 7 (незадана, пусто, токен, чужой токен, чужой контекст, хвост, верное)"
