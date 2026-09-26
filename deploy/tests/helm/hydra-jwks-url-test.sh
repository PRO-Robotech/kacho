#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# api-gateway must fetch every key set from a REACHABLE cluster-internal address.
#
# The edge verifies each token against the key set of the issuer it names, and
# the address of every key set is DECLARED by the issuer record
# (`tokenAcceptance.issuerKeySets` → KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS).
# An address the pod cannot reach — localhost, the PUBLIC issuer host — means no
# key set ever arrives and every token is refused.
#
# The previous scalar pin (`hydra.jwksUrl` / `hydra.issuer` →
# KACHO_HYDRA_JWKS_URL / KACHO_HYDRA_ISSUER) is RETIRED: the edge no longer reads
# it and refuses to start without a declared issuer set. The chart therefore
# must not render either variable at all — neither by default nor when a stale
# profile still names the retired keys.
#
# ── ЦЕЛЬ ХОПА ЗАВИСИТ ОТ ПРОФИЛЯ, И ЭТО НЕ ПОСЛАБЛЕНИЕ ───────────────────────
# core-правило #16: iam — ЕДИНСТВЕННЫЙ фасад к провайдеру, ключи верификации
# раздаёт его зеркало (:9097, https, якорь доверия — внутренний CA). Боевой
# профиль на этот маршрут уже переведён; прямой хоп к провайдеру там — обход
# фасада, однажды уже найденный и починенный. Утверждение о боевом профиле
# требует зеркало ИМЕННО по защищённому транспорту и ОТДЕЛЬНО запрещает адрес
# провайдера в любом написании.
#
# ── ПРИНИМАЕТСЯ ТОЛЬКО НАШ ИЗДАТЕЛЬ (#2735) ─────────────────────────────────
# Провайдер личности и его издатель не поднимаются ни на одном стенде (база
# зонта, раздел «ЧУЖОЙ СТЕК ЛИЧНОСТИ НЕ ПОДНИМАЕТСЯ»), поэтому перечень
# принимаемых издателей обоих профилей называет НАШЕГО издателя и не называет
# издателя провайдера ни в одном написании. Прежнее утверждение («перечень
# называет издателя провайдера») снято вместе со своим предметом и заменено
# парой: наш издатель ЕСТЬ (положительная половина) — издателя провайдера НЕТ.
#
# This renders:
#   (1) the api-gateway chart standalone (the source the umbrella vendors via
#       `repository: file://../../../gateway/deploy` in helm/umbrella/Chart.yaml),
#       by default and with the retired keys set, and
#   (2) the umbrella with values.dev.yaml and values.prod.yaml restricted to the
#       api-gateway Deployment.
#
# Offline manifest-assertion harness (no kind cluster). Mirrors tests/helm/*.
set -euo pipefail
# any_line_matches <многострочное значение> <ERE> — как `grep -qE`: истинно, если
# ХОТЬ ОДНА строка значения совпадает с выражением. Построчность важна: у `grep`
# точка не переходит через перевод строки, а у `[[ =~ ]]` на всём значении —
# переходит. Труба убрана из-за ложного отказа на совпадении (задача #658).
any_line_matches() {
  local _l
  while IFS= read -r _l; do
    if [[ "$_l" =~ $2 ]]; then return 0; fi
  done <<<"$1"
  return 1
}

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
MONOREPO="$(cd "$REPO_ROOT/.." && pwd)"

# Три исхода — ОДНОЙ реализацией на весь каталог: 0 зелено · 1 находка о дереве ·
# 2 условие не создано (плюс текст самого helm).
#
# ЗДЕСЬ ЭТО БЫЛО НЕ ФОРМАЛЬНОСТЬЮ, А ЖИВЫМ ДЕФЕКТОМ #1195. Рендер стоял внутри
# ПОДСТАНОВКИ (`DEV="$(helm template … 2>/dev/null)"`) под `set -e`: на дереве без
# собранных зависимостей умбреллы скрипт умирал НА ПРИСВАИВАНИИ, кодом 1 и с НУЛЁМ
# БАЙТ вывода, а собственная диагностика строкой ниже («dep not built? run helm dep
# update») не исполнялась НИКОГДА. Гейт класса этого не видел: его предикат
# исключений считал упоминание `helm dep update` В ТЕКСТЕ СООБЩЕНИЯ признаком
# того, что скрипт собирает зависимости сам (задача #1214).
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=6
require_helm
require_mikefarah_yq
UMBRELLA="$REPO_ROOT/helm/umbrella"
# Путь берётся из Chart.yaml умбреллы, а не пишется рядом второй раз: пока чарт
# шлюза жил соседним репозиторием, тут стояло `../kacho-api-gateway/deploy`, и
# после переезда в монорепу проверка падала первой же строкой. Читаем ОБЪЯВЛЕННЫЙ
# источник — тогда следующий переезд чинит сам себя.
# Значение берётся ЦЕЛИКОМ, первая подходящая строка выбирается уже в bash:
  # `… | grep -m1` выходит по первому совпадению, писатель получает SIGPIPE, и под
  # `pipefail` статус подстановки становится ненулевым (задача #658).
AGW_CANDIDATES="$(sed -nE 's#^[[:space:]]*repository:[[:space:]]*file://\.\./\.\./\.\./(.*)$#\1#p' \
        "$UMBRELLA/Chart.yaml")"
AGW=""
while IFS= read -r _cand; do
  if [[ "$_cand" == *gateway* ]]; then AGW="$_cand"; break; fi
done <<<"$AGW_CANDIDATES"
AGW="$MONOREPO/$AGW"
# Боевой профиль забирает ключи через зеркало iam — единственный фасад к
# провайдеру (core #16), по защищённому транспорту с якорем доверия. Адрес пинится
# здесь ЛИТЕРАЛОМ: вычитывать ожидание из того же профиля, который и рендерится,
# значило бы сверять файл сам с собой.
WANT_PROD="https://kaname-internal.kacho.svc:9097/.well-known/kaname/jwks.json"
# Наш издатель — тот, чьи токены край принимает на обоих профилях.
OUR_ISSUER="https://kaname.kacho.local"
# Написания издателя ПРОВАЙДЕРА: прежний издатель стенда разработки (путь
# полосы раздачи) и публичный издатель боевых профилей.
PROVIDER_ISSUER_SPELLING='/\.ory/hydra/|hydra\.api\.'
# issuer_set_is_ours <перечень> <профиль> — наш издатель назван, издатель
# провайдера — нет. Две половины одного утверждения: без первой пустой перечень
# прошёл бы вторую.
issuer_set_is_ours() {
  local e seen=0
  while IFS= read -r e; do
    [ -n "$e" ] || continue
    if any_line_matches "$e" "$PROVIDER_ISSUER_SPELLING"; then
      fail "$2 перечень принимаемых издателей называет издателя провайдера ($e): провайдер не поднимается ни на одном стенде, и его набор ключей никто не держит (#2735)"
    fi
    [ "$e" = "$OUR_ISSUER" ] && seen=1
  done <<<"$(printf '%s' "$1" | tr ',' '\n')"
  [ "$seen" -eq 1 ] \
    || fail "$2 перечень принимаемых издателей не называет НАШЕГО издателя $OUR_ISSUER (перечень: $1)"
}
# Написания адреса ПРОВАЙДЕРА: любое из них в боевом профиле — обход фасада.
PROVIDER_SPELLING='hydra-public|hydra\.api\.'
# env_val <ENV_NAME> <render> — value of the named container env entry ("" if absent).
env_val() {
  echo "$2" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[].env[] | select(.name==\"$1\") | .value" -
}

[ -d "$AGW" ] \
  || fatal "чарта края нет по пути $AGW (объявлен в $UMBRELLA/Chart.yaml) — судить не о чем"

# no_pin <render> <label> — neither retired scalar-pin variable is rendered.
no_pin() {
  local v
  for v in KACHO_HYDRA_JWKS_URL KACHO_HYDRA_ISSUER; do
    [ -z "$(env_val "$v" "$1")" ] \
      || fail "$2: renders the retired $v — the edge no longer reads it, and a variable nobody reads outlives its subject silently"
  done
}

# ── (1) sibling chart standalone — the retired pin is never rendered ──────────
# Законный близнец и предмет отличаются ОДНИМ фактом: во втором рендере профиль
# ещё называет снятые ключи. Шаблон обязан молчать на обоих.
helm_try ag "$AGW" --set hydra.jwksUrl="http://kacho-umbrella-hydra-public.kacho.svc:4444/.well-known/jwks.json" \
        --set hydra.issuer="https://hydra.api.kacho.cloud"
render_or_fatal "чарт края, заданы снятые ключи hydra.jwksUrl / hydra.issuer"
no_pin "$HELM_OUT" "sibling chart with the retired keys set"; ok

helm_try ag "$AGW"
render_or_fatal "чарт края, умолчание"
no_pin "$HELM_OUT" "sibling chart by default"; ok

# ── (2) umbrella + values.dev.yaml — the actual dev stand ─────────────────────
# `helm template` resolves the file:// api-gateway dep from the vendored .tgz; if
# the dep is stale this still renders the committed chart. Restrict to the
# api-gateway Deployment via --show-only.
helm_try kacho-umbrella "$UMBRELLA" -f "$UMBRELLA/values.dev.yaml" \
        --show-only charts/api-gateway/templates/deployment.yaml
render_or_fatal "умбрелла + values.dev.yaml, шаблон пода края"
DEV="$HELM_OUT"
[ -n "$DEV" ] || fail "рендер шаблона пода края (dev) ПУСТ при успешном helm template"
# Адрес набора объявляется ЗАПИСЬЮ издателя — единственной формой, которую край
# читает.
no_pin "$DEV" "dev"
dks="$(env_val KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS "$DEV")"
[ -n "$dks" ] \
  || fail "dev не объявляет адрес набора — без записи издателя край не поднимется"
ok

# Свойство, ради которого проба написана: КАЖДЫЙ адрес, который край будет
# тянуть, достижим из пода. Запись издателей даёт по адресу на издателя, и
# проверить надо все: достижимый первый и недостижимый второй дают ровно тот
# отказ, который проба обязана ловить.
dev_urls="$(printf '%s' "$dks" | tr ',' '\n' | sed 's/^[^=]*=//')"
while IFS= read -r u; do
  [ -n "$u" ] || continue
  case "$u" in
    *localhost*) fail "dev JWKS URL points at localhost ($u) — gateway pod cannot reach it" ;;
    https://hydra.*) fail "dev JWKS URL points at PUBLIC issuer ($u) — not reachable from the gateway pod" ;;
  esac
done <<EOF_DEV_URLS
$dev_urls
EOF_DEV_URLS
ok

# Перечень принимаемых издателей dev: наш издатель назван, издателя
# провайдера нет (#2735).
dissuers="$(env_val KACHO_API_GATEWAY_TOKEN_ISSUERS "$DEV")"
issuer_set_is_ours "$dissuers" "dev"
ok

# ── (3) umbrella + values.prod.yaml — production-strict makes the verifier
#        mandatory, so the JWKS URL must be the in-cluster address of the iam
#        key-set publisher (core #16: iam is the only facade), over TLS —
#        not the public ingress hairpin and not the provider's own Service.
helm_try kacho-umbrella "$UMBRELLA" -f "$UMBRELLA/values.prod.yaml" \
         --show-only charts/api-gateway/templates/deployment.yaml
render_or_fatal "умбрелла + values.prod.yaml, шаблон пода края"
PROD="$HELM_OUT"
[ -n "$PROD" ] || fail "рендер шаблона пода края (prod) ПУСТ при успешном helm template"
# Проба судит СВОЙСТВО — «материал проверки едет через зеркало iam, по TLS, не
# от провайдера напрямую» — по записи издателей (Ф1б, #926: у каждого
# принимаемого издателя свой набор).
no_pin "$PROD" "prod"
pks="$(env_val KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS "$PROD")"
[ -n "$pks" ] \
  || fail "prod не объявляет адрес набора — без записи издателя край не поднимется"
prod_urls="$(printf '%s' "$pks" | tr ',' '\n' | sed 's/^[^=]*=//')"

seen_mirror=0
while IFS= read -r u; do
  [ -n "$u" ] || continue
  case "$u" in
    https://*) ;;
    *) fail "prod JWKS URL is not TLS ($u) — the material that verifies every bearer's signature travels this hop" ;;
  esac
  if any_line_matches "$u" "$PROVIDER_SPELLING"; then
    fail "prod JWKS URL addresses the provider directly ($u) — that bypasses the iam facade (core #16), a hop already found and closed once"
  fi
  case "$u" in
    "${WANT_PROD%/.well-known/*}"/*) seen_mirror=1 ;;
  esac
done <<EOF_URLS
$prod_urls
EOF_URLS
[ "$seen_mirror" -eq 1 ] \
  || fail "prod: ни один адрес набора не ведёт на зеркало iam (ждали адреса вида ${WANT_PROD%/.well-known/*}/…), объявлено: $prod_urls"
# Якорь доверия обязан быть смонтирован: TLS без проверки сертификата на этом хопе
# читается как настроенная защита, ничего не проверяя.
[[ "$PROD" == *'hydra-jwks-ca'* ]] \
  || fail "prod api-gateway pod carries no trust anchor for the JWKS hop — TLS whose certificate nobody checks leaves substitution open"
# Перечень принимаемых издателей prod: наш издатель назван, издателя
# провайдера нет (#2735).
pissuers="$(env_val KACHO_API_GATEWAY_TOKEN_ISSUERS "$PROD")"
issuer_set_is_ours "$pissuers" "prod"
ok

outcome_verdict "профилей прочитано: 2 (dev, prod) + чарт края отдельно"
