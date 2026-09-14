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
# Оси делятся на две группы, и вторая появилась позже первой.
#
# ГРУППА 1 — КЛАССЫ: форма имени · членство в SAN · тот ли это пир · сосед,
# объявленный ОТСУТСТВУЮЩИМ. У каждого класса есть законный близнец, меняющий
# ровно один факт, — иначе «зелено» неотличимо от «ничего не проверяет».
#
# ГРУППА 2 — ФОРМЫ ЗАПИСИ, И ИХ ЧЕТЫРЕ. Форма, о которой распознаватель не
# знает, даёт НЕ красное и не зелёное, а МОЛЧАНИЕ, поэтому каждая доказывается
# отдельной инъекцией:
#
#   А. обе половины — переменные окружения контейнера (compute, registry);
#   Б. адрес — ключ `data` у ConfigMap, доезжающий через `envFrom`, имя — env
#      (storage);
#   В. адрес — ключ внутри файла настроек, смонтированного томом, имя — env
#      (vpc);
#   Г. обе половины — ключи внутри файла настроек (nlb).
#
# Прежняя редакция знала только форму А, и три потребителя домена величин из
# пяти молчали в счётчике «адреса ребра рядом нет».

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

# cert <san> — документ сертификата пира, с которым сверяется имя.
cert() {
  local san="$1"
  echo "apiVersion: cert-manager.io/v1"
  echo "kind: Certificate"
  echo "metadata:"
  echo "  name: peer-tls"
  echo "spec:"
  echo "  dnsNames:"
  echo "    - $san"
  echo "    - $san.cluster.local"
  echo "    - kaname-internal.kacho.svc"
}

# doc <имя-для-сверки> [адрес] [san] — ФОРМА А: обе половины переменными окружения.
doc() {
  local servername="$1" addr="${2:-}" san="${3:-vpc.kacho.svc}"
  {
    cert "$san"
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

# doc_emptyaddr <имя-для-сверки> — адрес ребра объявлен и ПУСТ.
doc_emptyaddr() {
  local servername="$1"
  {
    cert "vpc.kacho.svc"
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
    echo "            - name: KACHO_COMPUTE_VPC_GRPC_ADDR"
    echo "              value: \"\""
  } >"$TMP/in.yaml"
}

# doc_envfrom <имя-для-сверки> <адрес> — ФОРМА Б: адрес приезжает `envFrom`.
doc_envfrom() {
  local servername="$1" addr="$2"
  {
    cert "vpc.kacho.svc"
    echo "---"
    echo "apiVersion: v1"
    echo "kind: ConfigMap"
    echo "metadata:"
    echo "  name: probe-config"
    echo "data:"
    echo "  KACHO_COMPUTE_VPC_GRPC_ADDR: \"$addr\""
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
    echo "          envFrom:"
    echo "            - configMapRef:"
    echo "                name: probe-config"
    echo "          env:"
    echo "            - name: KACHO_COMPUTE_VPC_MTLS_SERVERNAME"
    echo "              value: \"$servername\""
  } >"$TMP/in.yaml"
}

# doc_cfgaddr <имя-для-сверки> <адрес> — ФОРМА В: адрес в файле настроек, имя в env.
doc_cfgaddr() {
  local servername="$1" addr="$2"
  {
    cert "vpc.kacho.svc"
    echo "---"
    echo "apiVersion: v1"
    echo "kind: ConfigMap"
    echo "metadata:"
    echo "  name: probe-config"
    echo "data:"
    echo "  config.yaml: |"
    echo "    extapi:"
    echo "      vpc:"
    echo "        addr: \"$addr\""
    echo "---"
    echo "apiVersion: apps/v1"
    echo "kind: Deployment"
    echo "metadata:"
    echo "  name: compute"
    echo "spec:"
    echo "  template:"
    echo "    spec:"
    echo "      volumes:"
    echo "        - name: config"
    echo "          configMap:"
    echo "            name: probe-config"
    echo "      containers:"
    echo "        - name: compute"
    echo "          env:"
    echo "            - name: KACHO_COMPUTE_VPC_MTLS_SERVERNAME"
    echo "              value: \"$servername\""
  } >"$TMP/in.yaml"
}

# doc_cfgboth <имя-для-сверки> <адрес> — ФОРМА Г: обе половины в файле настроек.
doc_cfgboth() {
  local servername="$1" addr="$2"
  {
    cert "vpc.kacho.svc"
    echo "---"
    echo "apiVersion: v1"
    echo "kind: ConfigMap"
    echo "metadata:"
    echo "  name: probe-config"
    echo "data:"
    echo "  config.yaml: |"
    echo "    extapi:"
    echo "      vpc:"
    echo "        addr: \"$addr\""
    echo "    mtls:"
    echo "      vpc:"
    echo "        servername: \"$servername\""
  } >"$TMP/in.yaml"
}

# doc_absent <имя-для-сверки> — сосед объявлен ОТСУТСТВУЮЩИМ, имя при этом задано.
#
# Второе ребро (vpc, пара сходится) держит обход НЕПУСТЫМ у близнеца: иначе
# «имени нет» и «распознаватель ослеп» дали бы одно и то же зелёное.
doc_absent() {
  local servername="${1:-}"
  {
    cert "vpc.kacho.svc"
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
    echo "              value: \"vpc.kacho.svc\""
    echo "            - name: KACHO_COMPUTE_VPC_GRPC_ADDR"
    echo "              value: \"vpc.kacho.svc:9090\""
    echo "            - name: KACHO_COMPUTE_QUOTA_AUTHORITY"
    echo "              value: \"not-deployed\""
    if [ -n "$servername" ]; then
      echo "            - name: KACHO_COMPUTE_QUOTA_AUTHORITY_MTLS_SERVERNAME"
      echo "              value: \"$servername\""
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

# assert_says <подстрока> <зачем> — текст находки ПОСЛЕДНЕГО входа обязан
# называть предмет своим словом. Без этого ось «сосед отсутствует» зеленела бы на
# сообщении про «другого пира» — верное слово о неверной причине, и оператор
# искал бы опечатку в имени вместо лишней половины пары.
assert_says() {
  local want="$1" why="$2" out
  out="$(python3 "$AUDIT" "$TMP/in.yaml" синтетика)" || {
    echo "  ✗ текст находки: разборщик отказал — ось не выполнена"
    FAILED=$((FAILED + 1)); return
  }
  N=$((N + 1))
  if grep -F -q "$want" <<<"$out"; then
    echo "  ✓ текст находки называет «$want»"
  else
    echo "  ✗ текст находки НЕ называет «$want» — $why"
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

echo "── ось Г: сосед объявлен ОТСУТСТВУЮЩИМ, а имя для сверки задано ──"
doc_absent "kaname-internal.kacho.svc"
assert "имя при адресе 'not-deployed' — находка" RED
assert_says "соседа в этой" "отказ обязан назвать ОТСУТСТВИЕ соседа, а не «другого пира»"
doc_absent ""
assert "близнец: адрес 'not-deployed' БЕЗ имени — молчит (законная посадка)" GREEN

echo "── ось Д: адрес объявлен и ПУСТ ──"
doc_emptyaddr "vpc.kacho.svc"
assert "пустой адрес при заданном имени — находка" RED
assert_says "хоста в" "пустой набор хостов делал бы отношение выполнимым подстановкой"
doc "vpc.kacho.svc" "vpc.kacho.svc:9090"
assert "близнец: адрес с хостом — молчит" GREEN

echo "── формы записи: их четыре, и каждая доказывается отдельно ──"
doc "kacho-geo.kacho.svc" "vpc.kacho.svc:9090"
assert "форма А (env + env): расхождение найдено" RED
doc_envfrom "kacho-geo.kacho.svc" "vpc.kacho.svc:9090"
assert "форма Б (адрес через envFrom): расхождение найдено" RED
doc_envfrom "vpc.kacho.svc" "vpc.kacho.svc:9090"
assert "форма Б, близнец: пара сходится — молчит" GREEN
doc_cfgaddr "kacho-geo.kacho.svc" "vpc.kacho.svc:9090"
assert "форма В (адрес в файле настроек): расхождение найдено" RED
doc_cfgaddr "vpc.kacho.svc" "vpc.kacho.svc:9090"
assert "форма В, близнец: пара сходится — молчит" GREEN
doc_cfgboth "kacho-geo.kacho.svc" "vpc.kacho.svc:9090"
assert "форма Г (обе половины в файле настроек): расхождение найдено" RED
doc_cfgboth "vpc.kacho.svc" "vpc.kacho.svc:9090"
assert "форма Г, близнец: пара сходится — молчит" GREEN

echo "── антимаски: состояния, находкой НЕ являющиеся ──"
doc "vpc.kacho.svc" "vpc.kacho.svc.cluster.local:9090" "vpc.kacho.svc"
assert "адрес полной формой, имя короткой — ОДИН пир, не находка" GREEN
doc "vpc.kacho.svc" "" "vpc.kacho.svc"
assert "адреса ребра рядом нет — не находка (и это отдельная величина переписи)" GREEN

echo
echo "── объём осмотренного ──"
echo "  форм записи доказано: 4 (А env+env · Б envFrom · В файл настроек+env · Г файл настроек)"
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
