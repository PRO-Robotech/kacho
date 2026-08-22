#!/usr/bin/env bash
#
# identity-hook-credential-source-inject.sh — доказательство того, что проба
# `identity-hook-credential-source-test.sh` СПОСОБНА упасть, и что она молчит на
# законном близнеце. Без этого «зелено» неотличимо от «ничего не проверяет».
#
# Инъекции идут по временным копиям профиля; дерево не трогается.

set -euo pipefail
cd "$(dirname "$0")/../.."

CHART=./helm/umbrella
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"; rm -f "$CHART"/values.inject-*.yaml' EXIT

rc=0
assert() { # имя · ожидание(RED|GREEN) · файл профиля
  local name="$1" want="$2" prof="$3" got
  if IDENTITY_SOURCE_PROFILES="$prof" bash tests/helm/identity-hook-credential-source-test.sh >"$TMP/out" 2>&1
  then got=GREEN; else got=RED; fi
  if [ "$got" = "$want" ]; then
    echo "  ok   $name → $got"
  else
    echo "  ОТКАЗ $name → $got, ожидалось $want"; sed 's/^/       /' "$TMP/out"; rc=1
  fi
}

# (1) законный близнец: дерево как есть — проба обязана МОЛЧАТЬ.
assert "дерево как есть" GREEN values.dev.yaml

# (2) дефект возвращён: подстановки нет вовсе — ссылка остаётся без источника.
python3 - "$CHART/values.dev.yaml" "$CHART/values.inject-noinit.yaml" <<'PY'
import io,sys,re
s=io.open(sys.argv[1],encoding="utf-8").read()
i=s.index("    extraInitContainers: |")
j=s.index("    extraVolumes:", i)
io.open(sys.argv[2],"w",encoding="utf-8").write(s[:i]+s[j:])
PY
assert "подстановки нет" RED values.inject-noinit.yaml

# (3) процесс читает ШАБЛОН, а не отрендеренное — путь --config возвращён назад.
sed 's|/etc/kacho-identity-rendered/kratos.yaml|/etc/kacho-identity/kratos.yaml|' \
  "$CHART/values.dev.yaml" > "$CHART/values.inject-oldpath.yaml"
assert "читает шаблон" RED values.inject-oldpath.yaml

# (4) отказ перестал быть закрытым: проверка пустой величины снята.
python3 - "$CHART/values.dev.yaml" "$CHART/values.inject-open.yaml" <<'PY'
import io,sys
s=io.open(sys.argv[1],encoding="utf-8").read()
s=s.replace('{{- include "kacho.identity.configRenderInitContainer" . | nindent 0 }}',
            '{{- include "kacho.identity.configRenderInitContainer" . | nindent 0 | replace ":?величина" ":-величина" }}')
io.open(sys.argv[2],"w",encoding="utf-8").write(s)
PY
assert "отказ не закрытый" RED values.inject-open.yaml

# (5) законный близнец второго рода: профиль БЕЗ службы личности — не находка,
#     а отсутствие предмета. Проба обязана пройти, сказав об этом.
python3 - "$CHART/values.dev.yaml" "$CHART/values.inject-nokratos.yaml" <<'PY'
import io,sys,re
s=io.open(sys.argv[1],encoding="utf-8").read()
s=re.sub(r"(?m)^(kratos:\n(?:  #[^\n]*\n)*  )enabled: true", r"\1enabled: false", s, count=1)
io.open(sys.argv[2],"w",encoding="utf-8").write(s)
PY
assert "службы личности нет" GREEN values.inject-nokratos.yaml

[ "$rc" -eq 0 ] && echo "OK: проба падает на возвращённом дефекте и молчит на законном"
exit "$rc"
