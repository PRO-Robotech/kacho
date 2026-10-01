#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# cert-issuance-policy-render-inject.sh — доказательство того, что правило 5
# пробы рендера (cert-issuance-policy-render-test.sh: «гейт секции E узнаёт
# notify в той форме, в какой его производит дерево») СПОСОБНО покраснеть.
#
# ПОЧЕМУ ОТДЕЛЬНО. Чарта notify в цепочках пока нет (его заводит полоса D1), и
# на дереве правило осматривает ноль нагрузок — зелёный, который ничего не
# говорит о способности покраснеть. Здесь разборщику рендера подаётся
# синтетический рендер с нагрузкой из `charts/notify/` — той формы, какую
# производят соседние чарты, — и сверяется исход.
#
# ОТКУДА ДЕФЕКТЫ — ревью 2915-J1, находка F1: гейт узнавал notify по метке
# `app.kubernetes.io/name=notify`, которую не производит ни один чарт, и на
# notify с меткой соседей читал «не поднят».
#
#   chart-sa-unrecognised     чарт notify поднимает нагрузку под учёткой и
#                             именем, которых гейт не знает → BAD;
#   declaration-disagrees     декларация notify.spiffe.saName ≠ учётке гейта → BAD;
#   gate-label-only           гейт (копия) узнаёт notify только по метке
#                             `app.kubernetes.io/name=notify` — ровно прежний
#                             дефект; нагрузка с меткой соседа `kacho-notify` → BAD.
# Законные близнецы: та же нагрузка под учёткой `kacho-notify` с настоящим
# гейтом → OK без BAD; нагрузка чужого чарта → не считается нагрузкой notify.
#
# Исходов три: 0 — каждый мутант пойман, близнецы зелены; 1 — мутант прошёл
# либо близнец покраснел; 2 — условие не создано.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
PARSER="$HERE/cert-issuance-policy-render.py"
GATE="$HERE/../../scripts/assert-cert-issuance-policy.sh"
for f in "$PARSER" "$GATE"; do
  [ -f "$f" ] || { echo "ОТКАЗ: нет $f — доказывать нечего"; exit 2; }
done
command -v python3 >/dev/null 2>&1 || { echo "ОТКАЗ: нет python3"; exit 2; }
python3 -c 'import yaml' 2>/dev/null || { echo "ОТКАЗ: нет python3-yaml — разборщик не запустится"; exit 2; }
TMP="$(mktemp -d)" || { echo "ОТКАЗ: не создан временный каталог"; exit 2; }
trap 'rm -rf "$TMP"' EXIT

# Мутант гейта — копия с ОДНОЙ подменой; якорь обязан найтись ровно один раз.
python3 - "$GATE" "$TMP/gate-label-only.sh" <<'PY' || { echo "ОТКАЗ: мутант гейта не порождён — опыт не внесён"; exit 2; }
import sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src, encoding="utf-8").read()
a = '    if sa == sa_want or any(name_re.match(n) for n in names if n):'
b = '    if labels.get("app.kubernetes.io/name") == "notify":'
if text.count(a) != 1:
    sys.exit("якорь классификатора найден %d раз (нужен 1)" % text.count(a))
open(dst, "w", encoding="utf-8").write(text.replace(a, b))
PY

workload() { # <источник> <имя> <учётка> <метка имени или ->
  local labels=""
  [ "$4" != - ] && labels="
  labels:
    app.kubernetes.io/name: $4"
  cat <<EOF
---
# Source: kacho-umbrella/charts/$1
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $2$labels
spec:
  template:
    spec:
      serviceAccountName: $3
EOF
}

# run <метка> <ждём: bad|clean> <гейт> <декларация> <ждём нагрузок notify> ; рендер — stdin.
# Рендер подаётся подстановкой процесса, а не трубой: в трубе run шёл бы в
# подоболочке, и счётчик опытов с флагом провала терялись бы молча.
bad=0 n=0
run() {
  local name="$1" want="$2" gate="$3" decl="$4" want_wl="$5" out rc nbad nwl
  n=$((n + 1))
  out="$(python3 "$PARSER" inject kacho-cert-manager kacho "$gate" "$decl" 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "ОТКАЗ: разборщик не состоялся на «$name» (код $rc): $out"; exit 2
  fi
  nbad="$(grep -c '^BAD|' <<<"$out")"; nwl="$(grep -c '^NOTIFYWL|' <<<"$out")"
  if [ "$nwl" != "$want_wl" ]; then
    echo "  ✗ $name: нагрузок notify $nwl, ждали $want_wl"; echo "$out" | sed 's/^/      | /'; bad=1; return
  fi
  case "$want" in
    bad)   if [ "$nbad" -gt 0 ]; then echo "  ✓ мутант $name пойман ($nbad находок)"
           else echo "  ✗ мутант $name ПРОШЁЛ — правило 5 не способно покраснеть на нём"; echo "$out" | sed 's/^/      | /'; bad=1; fi ;;
    clean) if [ "$nbad" -eq 0 ]; then echo "  ✓ близнец $name зелен"
           else echo "  ✗ близнец $name покраснел — красный мутантов ничего не доказывает"; echo "$out" | sed 's/^/      | /'; bad=1; fi ;;
  esac
}

run chart-sa-unrecognised bad "$GATE" "" 1 < <(workload notify/templates/deployment.yaml mailer mailer -)
run declaration-disagrees bad "$GATE" notify-mailer 1 < <(workload notify/templates/deployment.yaml notify-sender notify-mailer kacho-notify)
run gate-label-only bad "$TMP/gate-label-only.sh" "" 1 < <(workload notify/templates/deployment.yaml kacho-notify kacho-notify kacho-notify)
run twin-neighbor-form clean "$GATE" kacho-notify 1 < <(workload notify/templates/deployment.yaml kacho-notify kacho-notify kacho-notify)
run twin-unlabelled clean "$GATE" "" 1 < <(workload notify/templates/deployment.yaml mail-gateway kacho-notify -)
run twin-foreign-chart clean "$GATE" "" 0 < <(workload kacho-vpc/templates/deployment.yaml kacho-vpc kacho-vpc kacho-vpc)

if [ "$bad" -ne 0 ]; then
  echo "FAIL: правило 5 пробы рендера не доказано инъекцией (опытов $n)"
  exit 1
fi
[ "$n" -eq 6 ] || { echo "ОТКАЗ: опытов исполнено $n из 6 — опыт поставлен не целиком"; exit 2; }
echo "PASS: правило 5 краснеет на каждом из 3 мутантов, близнецов 3 зелены (опытов $n)"
