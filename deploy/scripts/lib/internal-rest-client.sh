#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# internal-rest-client.sh — клиентский материал ВНУТРЕННЕГО REST-слушателя края
# (Internal* REST, Service-порт `internal-rest`, kacho#3131).
#
# Слушатель — mTLS и только он: клиентский лист обязателен, цепочка — к УЦ
# установки, URI-имя листа — в круге края (KACHO_API_GATEWAY_INTERNAL_REST_CLIENT_SANS).
# Лист этого круга — операторская личность `kacho-internal-rest-operator`;
# его выпускает умбрелла (templates/bootstrap-operator-certificate.yaml) в
# секрет ниже, и никакой под его не монтирует — инструменты оператора берут
# его из кластера, как лист чеканки.
#
# Один производитель на всех потребителей: прогонщики newman, посев, подъём
# администратора облака. Контракт — три переменные окружения, их читают и
# оболочка (curl), и python (tests/authz-fixtures/verified_human.py
# internal_rest_tls_context):
#
#   INTERNAL_REST_CA    — УЦ установки (проверка серверного листа слушателя);
#   INTERNAL_REST_CERT  — лист оператора;
#   INTERNAL_REST_KEY   — его ключ.
#
# Открытого текста у слушателя нет: секрета нет — функция отказывает, и
# вызывающий отказывает тоже, а не идёт открытым текстом.
#
# Адрес проброса (https://127.0.0.1 / https://localhost) проходит сверку имени
# серверного листа ТОЛЬКО там, где лист несёт эти имена: ключ
# api-gateway.internalRest.loopbackSAN (умолчание чарта — false) включён в
# профилях умбреллы values.dev.yaml (цепочки dev, dev-prod, prorobotech,
# a8f60d) и values.own-stand.yaml (цепочка own). На боевых цепочках (prod,
# fe3455) этих имён нет, и вызов пробросом получит отказ рукопожатия — ходить
# туда по имени Service. Обе стороны судит
# gateway/deploy/internal_rest_mtls_render_test.go. Проверка сервера при этом
# не ослабляется: УЦ и сверка имени — прежние.

INTERNAL_REST_OPERATOR_SECRET="${INTERNAL_REST_OPERATOR_SECRET:-kacho-internal-rest-operator-client-tls}"

# internal_rest_client_leaf <namespace> <каталог> — пишет ca.crt, tls.crt,
# tls.key из секрета оператора и экспортирует INTERNAL_REST_{CA,CERT,KEY}.
# 0 — материал есть; 1 — секрета нет (слушатель недостижим, это не «пойти
# открытым текстом»).
internal_rest_client_leaf() {
  local ns="$1" dir="$2" k
  kubectl -n "$ns" get secret "$INTERNAL_REST_OPERATOR_SECRET" >/dev/null 2>&1 || return 1
  mkdir -p "$dir"
  for k in ca.crt tls.crt tls.key; do
    kubectl -n "$ns" get secret "$INTERNAL_REST_OPERATOR_SECRET" \
      -o jsonpath="{.data.${k//./\\.}}" | base64 -d > "$dir/$k" || return 1
    [ -s "$dir/$k" ] || return 1
  done
  chmod 600 "$dir"/ca.crt "$dir"/tls.crt "$dir"/tls.key
  export INTERNAL_REST_CA="$dir/ca.crt" INTERNAL_REST_CERT="$dir/tls.crt" INTERNAL_REST_KEY="$dir/tls.key"
  return 0
}

# internal_rest_curl_args — аргументы curl для внутреннего слушателя в массив
# INTERNAL_REST_CURL_ARGS. Материала нет — отказ (1), массив пуст.
internal_rest_curl_args() {
  INTERNAL_REST_CURL_ARGS=()
  [ -n "${INTERNAL_REST_CA:-}" ] && [ -n "${INTERNAL_REST_CERT:-}" ] && [ -n "${INTERNAL_REST_KEY:-}" ] || return 1
  # shellcheck disable=SC2034  # массив читает вызывающий (источник библиотеки)
  INTERNAL_REST_CURL_ARGS=(--cacert "$INTERNAL_REST_CA" --cert "$INTERNAL_REST_CERT" --key "$INTERNAL_REST_KEY")
}

# internal_rest_newman_args <локальный порт> <каталог> — аргументы newman в
# массив INTERNAL_REST_NEWMAN_ARGS: УЦ установки — дополнительным якорем
# (проверка сервера остаётся строгой), лист оператора — СПИСКОМ по адресу
# внутреннего слушателя, а не глобальным `--ssl-client-cert`: глобальный лист
# ушёл бы и на другие адреса, а список с совпавшим адресом главнее глобального.
internal_rest_newman_args() {
  local port="$1" dir="$2"
  INTERNAL_REST_NEWMAN_ARGS=()
  [ -n "${INTERNAL_REST_CA:-}" ] && [ -n "${INTERNAL_REST_CERT:-}" ] && [ -n "${INTERNAL_REST_KEY:-}" ] || return 1
  cat > "$dir/internal-rest-client-certs.json" <<JSON
[
  {
    "name": "kacho-internal-rest-operator",
    "matches": ["https://localhost:${port}/*", "https://127.0.0.1:${port}/*"],
    "key": {"src": "${INTERNAL_REST_KEY}"},
    "cert": {"src": "${INTERNAL_REST_CERT}"}
  }
]
JSON
  # shellcheck disable=SC2034  # массив читает вызывающий (источник библиотеки)
  INTERNAL_REST_NEWMAN_ARGS=(--ssl-extra-ca-certs "$INTERNAL_REST_CA"
                             --ssl-client-cert-list "$dir/internal-rest-client-certs.json")
}
