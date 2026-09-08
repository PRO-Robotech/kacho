#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# servername-checked-against-the-peer-inject.sh — ДОКАЗАТЕЛЬСТВО того, что
# `servername-checked-against-the-peer-test.sh` СПОСОБЕН упасть, и что он МОЛЧИТ
# на законном близнеце. Без второй половины «зелено» неотличимо от «ничего не
# проверяет».
#
# Кормится ТА ЖЕ функция разбора (`servername-peer-audit.py`), которую гейт
# применяет к настоящим рендерам, — поэтому доказанное на синтетике верно для
# дерева. Дерево не трогается: синтетические документы живут во временном
# каталоге.
#
# Каждая ось меняет РОВНО ОДИН факт против своего близнеца: иначе неизвестно,
# какой из двух дал красное.
#
# ОСЕЙ ПЯТЬ, по одной на класс, который гейт обязан ловить, плюс две
# «антимаски» — состояния, которые находкой быть НЕ ДОЛЖНЫ, иначе у гейта
# появятся ложные срабатывания, и его отключат первым.

set -uo pipefail

INJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
AUDIT="$INJECT_DIR/servername-peer-audit.py"

command -v python3 >/dev/null 2>&1 || { echo "SKIP: нет python3 — доказательство НЕ ВЫПОЛНЕНО (это не красное)"; exit 2; }
python3 -c 'import yaml' 2>/dev/null || { echo "SKIP: нет PyYAML — доказательство НЕ ВЫПОЛНЕНО (это не красное)"; exit 2; }
[ -r "$AUDIT" ] || { echo "ОТКАЗ: разборщика $AUDIT нет — доказывать нечего"; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

N=0
FAILED=0

# doc <имя-для-сверки> [адрес] [san] — синтетический рендер из двух документов:
# сертификата пира и пода, объявляющего имя для сверки (и, если задан, адрес).
doc() {
  local servername="$1" addr="${2:-}" san="${3:-vpc.kacho.svc}"
  {
    echo "apiVersion: cert-manager.io/v1"
    echo "kind: Certificate"
    echo "metadata:"
    echo "  name: peer-tls"
    echo "spec:"
    echo "  dnsNames:"
    echo "    - $san"
    echo "    - $san.cluster.local"
    echo "---"
    echo "apiVersion: apps/v1"
    echo "kind: Deployment"
    echo "metadata:"
    echo "  name: compute"
    echo "spec:"
    echo "  template:"
    echo "    spec:"
    echo "      containers:"
    echo "        - name: compute"
    echo "          env:"
    echo "            - name: KACHO_COMPUTE_VPC_MTLS_SERVERNAME"
    echo "              value: \"$servername\""
    if [ -n "$addr" ]; then
      echo "            - name: KACHO_COMPUTE_VPC_GRPC_ADDR"
      echo "              value: \"$addr\""
    fi
  } >"$TMP/in.yaml"
}

# assert <имя оси> <RED|GREEN> — прогнать разбор и сверить исход.
#
# RED здесь означает «есть хотя бы одна находка», GREEN — «находок ноль». Обход
# при этом обязан быть НЕПУСТ: разбор, не нашедший ни одного имени, зелен по той
# же причине, по какой зелено чистое дерево, — и это надо различать.
assert() {
  local name="$1" want="$2" out got seen
  out="$(python3 "$AUDIT" "$TMP/in.yaml" синтетика)" || {
    echo "  ✗ $name: разборщик отказал — ось не выполнена"
    FAILED=$((FAILED + 1)); return
  }
  seen="$(sed -n 's/^SCOPE \([0-9]*\).*/\1/p' <<<"$out")"
  if [ "${seen:-0}" -eq 0 ]; then
    echo "  ✗ $name: обход ПУСТ — ось вакуумна, распознаватель не нашёл ни одного имени"
    FAILED=$((FAILED + 1)); return
  fi
  if grep -q '^FINDING ' <<<"$out"; then got=RED; else got=GREEN; fi
  N=$((N + 1))
  if [ "$got" = "$want" ]; then
    echo "  ✓ $name: ждали $want — получили $want"
  else
    echo "  ✗ $name: ждали $want — получили $got"
    grep '^FINDING ' <<<"$out" | sed 's/^/      /'
    FAILED=$((FAILED + 1))
  fi
}

echo "── ось А: форма имени ──"
doc "vpc.kacho.svc.cluster.local" "" "vpc.kacho.svc"
assert "полная форма с доменом кластера — находка" RED
doc "vpc.kacho.svc" "" "vpc.kacho.svc"
assert "близнец: короткая служебная форма — молчит" GREEN

echo "── ось Б: членство в SAN ──"
doc "vpc.kacho.svc:9090" "" "vpc.kacho.svc"
assert "приписанный порт — находка (в SAN такого имени нет)" RED
doc "vpc-totally-fake.kacho.svc" "" "vpc.kacho.svc"
assert "выдуманная приставка — находка" RED
doc "vpc.kacho.svc" "" "vpc.kacho.svc"
assert "близнец: имя из SAN — молчит" GREEN

echo "── ось В: тот ли это пир ──"
doc "kacho-geo.kacho.svc" "vpc.kacho.svc:9090" "kacho-geo.kacho.svc"
assert "имя ЧУЖОГО пира при объявленном адресе ребра — находка" RED
doc "vpc.kacho.svc" "vpc.kacho.svc:9090" "vpc.kacho.svc"
assert "близнец: имя того пира, к кому идёт ребро — молчит" GREEN

echo "── антимаски: состояния, находкой НЕ являющиеся ──"
doc "vpc.kacho.svc" "vpc.kacho.svc.cluster.local:9090" "vpc.kacho.svc"
assert "адрес полной формой, имя короткой — ОДИН пир, не находка" GREEN
doc "vpc.kacho.svc" "" "vpc.kacho.svc"
assert "адреса ребра рядом нет — не находка (и это отдельная величина переписи)" GREEN

echo
echo "── объём осмотренного ──"
echo "  осей выполнено: $N; провалено: $FAILED"
if [ "$N" -eq 0 ]; then
  echo "ОТКАЗ: не выполнено НИ ОДНОЙ оси — «доказано» здесь означало бы «не проверено»"
  exit 1
fi
if [ "$FAILED" -gt 0 ]; then
  echo "ОТКАЗ: осей провалено $FAILED — гейт не доказан"
  exit 1
fi
echo "PASS: servername-checked-against-the-peer-inject.sh ($N осей, каждая с законным близнецом)"
