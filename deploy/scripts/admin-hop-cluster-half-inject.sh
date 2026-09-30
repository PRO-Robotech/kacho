#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# admin-hop-cluster-half-inject.sh — доказательство инъекцией: самопроверка гейта
# перехода (`assert-admin-hop-transport.sh --self-test`) держит ПОДКЛЮЧЕНИЕ его
# кластерной половины к общим функциям чтения и суждения (kacho#2845).
#
# Функции суждения самопроверка судила и раньше — и оставалась зелёной, когда
# кластерная половина переставала ими пользоваться: мутант ревью волны kacho#2795
# (M7, половина на прежнем счёте строк конвейером) проходил её 50 из 50. Здесь
# каждое подключение рвётся по одному, в копии гейта, и требуется:
#   • мутант — самопроверка КРАСНАЯ (код 1) и находка называет кластерную половину;
#   • невредимая копия — самопроверка ЗЕЛЁНАЯ (код 0) — законный близнец.
#
# Якорь мутанта ищется ДОСЛОВНО и обязан встретиться ровно один раз. Не нашёлся —
# инъекция устарела вместе с правкой гейта, и это провал с именем мутанта, а не
# «мутировать нечего»: промолчавшая инъекция неотличима от прошедшей.
#
# Код 2 самопроверки — «условие не создано» (нет jq): здесь он передаётся тем же
# кодом и в зачёт «прошло» не идёт.
set -uo pipefail
cd "$(dirname "$0")" || exit 1
GATE="assert-admin-hop-transport.sh"

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
pass=0; fail=0; unmet=0

# mutate <мутант> <куда> — копия гейта с одним разорванным подключением.
mutate() {
  python3 - "$GATE" "$1" "$2" <<'PY'
import sys
src, kind, out = sys.argv[1:4]
text = open(src, encoding="utf-8").read()
MUTANTS = {
    # M7 ревью: поды провайдера — прежним счётом строк конвейером, мимо read_provider_pods.
    # Селектор двойнику kubectl безразличен; мутант меняет ровно способ счёта.
    "M7-pods": (
        '  read_provider_pods\n  PROVIDER_PODS="$PODS_CENSUS"\n',
        '  PROVIDER_PODS="$(kubectl -n "$NS" get pods -l app.kubernetes.io/name -o name 2>/dev/null | grep -c .)"\n',
    ),
    # окружение потребителей — счётом напечатанного, мимо consumer_addr_census.
    "M7-consumers": (
        '  CONSUMER_ADDRS="$(consumer_addr_census "$consumer_rc" "${#CONSUMER_OBJECTS[@]}" "$consumer_counts")"\n',
        '  CONSUMER_ADDRS="${consumer_counts#* }"\n',
    ),
    # суждение — своим сравнением с нулём, мимо own_absence_verdict.
    "M7-verdict": (
        '  case "$(own_absence_verdict "$IAM_POSTURE" "$EDGE_POSTURE" "$PROVIDER_PODS" "$CONSUMER_ADDRS")" in\n',
        '  case "$(if [ "$PROVIDER_PODS" -ne 0 ] 2>/dev/null; then echo provider-present; else echo ok; fi)" in\n',
    ),
}
if kind != "intact":
    anchor, repl = MUTANTS[kind]
    n = text.count(anchor)
    if n != 1:
        sys.exit(f"якорь мутанта {kind} встретился {n} раз(а), нужен ровно 1 — инъекция устарела")
    text = text.replace(anchor, repl)
open(out, "w", encoding="utf-8").write(text)
PY
}

# expect <мутант> <ожидаемый код самопроверки> <подстрока находки | ->
expect() {
  local kind="$1" want="$2" needle="$3" copy out code
  copy="$tmp/$kind/$GATE"
  mkdir -p "$tmp/$kind"
  if ! out="$(mutate "$kind" "$copy" 2>&1)"; then
    fail=$((fail + 1)); echo "  [ПРОВАЛ] $kind: $out"; return
  fi
  out="$(bash "$copy" --self-test 2>&1)" && code=0 || code=$?
  if [ "$code" = 2 ]; then
    unmet=$((unmet + 1)); echo "  [НЕ ВЫПОЛНЕНО] $kind: самопроверка сообщила «условие не создано»"
    printf '%s\n' "$out" | tail -3 | sed 's/^/      /'; return
  fi
  if [ "$code" != "$want" ] || { [ "$needle" != - ] && ! grep -qF -- "$needle" <<<"$out"; }; then
    fail=$((fail + 1))
    echo "  [ПРОВАЛ] $kind: самопроверка → код $code, ждали $want${needle:+ с «$needle»}"
    printf '%s\n' "$out" | tail -4 | sed 's/^/      /'; return
  fi
  pass=$((pass + 1)); echo "  [ok] $kind: самопроверка → код $code"
}

echo "=== подключение кластерной половины гейта перехода: инъекция в обе стороны ==="
expect intact 0 "PASS: $GATE --self-test"
expect M7-pods 1 "✗ кластерная половина"
expect M7-consumers 1 "✗ кластерная половина"
expect M7-verdict 1 "✗ кластерная половина"

echo
echo "инъекция подключения: утверждений $((pass + fail + unmet)), пройдено $pass, провалено $fail, не выполнено $unmet"
[ "$fail" -eq 0 ] || exit 1
[ "$unmet" -eq 0 ] || exit 2
