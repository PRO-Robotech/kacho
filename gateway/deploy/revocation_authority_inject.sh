#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# revocation_authority_inject.sh — доказательство того, что стражи НАШЕГО
# авторитета отзыва СПОСОБНЫ упасть, и что рядом с ними молчит законный близнец.
#
# Проверяется ПАРА на каждой оси. Одностороннее доказательство («вернул дефект —
# покраснело») зеленеет на гейте, который краснеет на всём; поэтому у каждой оси
# есть вход, на котором гейт ОБЯЗАН смолчать.
#
# Правит дерево и возвращает его обратно через trap. Прерывание восстанавливает
# так же, как штатный выход.
set -euo pipefail

cd "$(dirname "$0")/../.."
UMB=deploy/helm/umbrella
TPL=gateway/deploy/templates/deployment.yaml
BK=$(mktemp -d)
cp -a "$UMB" "$BK/umbrella"
cp -a "$TPL" "$BK/deployment.yaml"
restore() { rm -rf "$UMB"; cp -a "$BK/umbrella" "$UMB"; cp -a "$BK/deployment.yaml" "$TPL"; rm -rf "$BK"; }
trap restore EXIT

PKG=./gateway/deploy/...
RUN='TestStacks_AcceptingOurIssuerNameTheRevocationAuthority|TestChart_EmitsEveryDeclaredTokenAcceptanceKnob|TestChart_TokenLaneEnvIsWiredToItsOwnKnob'
pass=0; fail=0
out=""

run() { out=$(go test "$PKG" -count=1 -run "$RUN" 2>&1) && return 0 || return 1; }

# want=red|green ; needle — что обязано прозвучать в красном
assert() {
  local label="$1" want="$2" needle="${3:-}"
  if run; then got=green; else got=red; fi
  if [ "$got" != "$want" ]; then
    printf 'ОТКАЗ  %-58s ожидалось %s, получено %s\n' "$label" "$want" "$got"; fail=$((fail+1)); return
  fi
  if [ -n "$needle" ] && ! grep -qF -- "$needle" <<<"$out"; then
    printf 'ОТКАЗ  %-58s красное не называет %q\n' "$label" "$needle"; fail=$((fail+1)); return
  fi
  printf 'ok     %-58s %s\n' "$label" "$got"; pass=$((pass+1))
}

blank_overlay() { # $1 — yaml-хвост под api-gateway накладки prorobotech
  restore_files
  python3 - "$1" <<'PY'
import sys
p="deploy/helm/umbrella/values.prorobotech.yaml"
s=open(p).read()
s=s.replace("api-gateway:\n","api-gateway:\n"+sys.argv[1],1)
open(p,"w").write(s)
PY
}
restore_files() { rm -rf "$UMB"; cp -a "$BK/umbrella" "$UMB"; cp -a "$BK/deployment.yaml" "$TPL"; }

echo "── ось 1: адрес авторитета, погашенный НАКЛАДКОЙ ──"
blank_overlay '  tokenAcceptance:
    revocationUrl: ""
'
assert 'накладка гасит адрес — краснеет и называет стенд' red 'стенд prorobotech'

echo "── ось 1, законный близнец: ОТКАТ полосы (наш издатель снят целиком) ──"
blank_overlay '  tokenAcceptance:
    issuers: "https://localhost:28080/.ory/hydra/public"
    issuerKeySets: "https://localhost:28080/.ory/hydra/public=https://kaname-internal.kacho.svc:9097/.well-known/jwks.json"
    platformIssuer: ""
    revocationUrl: ""
'
assert 'откат полосы — молчит' green

echo "── ось 2: клиентская пара хопа, погашенная НАКЛАДКОЙ наполовину ──"
blank_overlay '  tokenAcceptance:
    revocationClientCert:
      keyFile: ""
'
assert 'половина пары — краснеет и называет стенд' red 'стенд prorobotech'

echo "── ось 3: шаблон перестаёт эмитить НАШЕГО ИЗДАТЕЛЯ (суффикс) ──"
restore_files
sed -i 's/name: KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER$/name: KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER_X/' "$TPL"
assert 'ручка объявлена, не эмитится — краснеет с именем' red 'config объявляет KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER,'
assert 'она же: эмитится имя, которого config не читает' red 'KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER_X'

echo "── ось 3: шаблон перестаёт эмитить АДРЕС авторитета ──"
restore_files
sed -i 's/name: KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL$/name: KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL_X/' "$TPL"
assert 'адрес объявлен, не эмитится — краснеет' red 'config объявляет KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL,'

echo "── ось 4: АДРЕС перевешен на соседний (выведен из чужого) ──"
restore_files
python3 - <<'PY2'
p="gateway/deploy/templates/deployment.yaml"
s=open(p).read()
s=s.replace("""            - name: KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL
              value: {{ .Values.tokenAcceptance.revocationUrl | quote }}""",
"""            - name: KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL
              value: {{ .Values.hydra.introspectionUrl | quote }}""",1)
open(p,"w").write(s)
PY2
assert 'адрес выведен из соседнего — краснеет с именем' red 'KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL берёт значение из'

echo "── ось 3, законный близнец: чистое дерево ──"
restore_files
assert 'дерево как есть — молчит' green

echo
echo "утверждений: пройдено $pass · отказов $fail"
[ "$fail" -eq 0 ]
