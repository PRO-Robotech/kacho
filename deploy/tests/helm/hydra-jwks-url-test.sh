#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# api-gateway must resolve a REACHABLE cluster-internal Hydra JWKS URL.
#
# Bug: the api-gateway auth path validates Hydra-issued RS256 login tokens by
# fetching Hydra's JWKS. cfg.ResolvedHydraJWKSURL() reads KACHO_HYDRA_JWKS_URL
# (explicit) and otherwise derives `{HydraIssuer}/.well-known/jwks.json` — whose
# default issuer (`https://hydra.api.kacho.cloud`) is NOT reachable in-cluster
# (Hydra self.issuer in dev is `http://localhost:28080/...`). With no env set the
# gateway pod fetches an unreachable URL → JWKS load fails → Hydra tokens are
# never validated → WhoAmI/Account/Project return code 16 AUTHN_REQUIRED.
#
# The dev stand's in-cluster Hydra PUBLIC Service is `kacho-umbrella-hydra-public`
# (release `kacho-umbrella`), port 4444, JWKS path `/.well-known/jwks.json`
# (verified: `helm template ... charts/hydra/templates/service-public.yaml`).
#
# ── ЦЕЛЬ ХОПА ЗАВИСИТ ОТ ПРОФИЛЯ, И ЭТО НЕ ПОСЛАБЛЕНИЕ ───────────────────────
# core-правило #16: iam — ЕДИНСТВЕННЫЙ фасад к провайдеру, ключи верификации
# раздаёт его зеркало (:9097, https, якорь доверия — внутренний CA). Боевой
# профиль на этот маршрут уже переведён; прямой хоп к провайдеру там — обход
# фасада, однажды уже найденный и починенный. Утверждение о боевом профиле
# поэтому не ослаблено, а ПЕРЕНАЦЕЛЕНО и усилено: теперь оно требует зеркало
# ИМЕННО по защищённому транспорту и ОТДЕЛЬНО запрещает адрес провайдера в любом
# написании. Профиль dev остаётся на прямом внутрикластерном адресе провайдера —
# это его текущее состояние, и утверждение о нём не тронуто.
#
# This renders BOTH:
#   (1) the api-gateway chart standalone (the source the umbrella vendors via
#       `repository: file://../../../gateway/deploy` in helm/umbrella/Chart.yaml)
#       with values that set hydra.jwksUrl, and
#   (2) the umbrella with values.dev.yaml (the actual dev stand) restricted to
#       the api-gateway Deployment.
# It asserts the rendered KACHO_HYDRA_JWKS_URL is the cluster-internal endpoint the
# profile is supposed to use — never localhost, never the public `hydra.<domain>`
# issuer, and in production never the provider's own address.
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
EXPECTED_ASSERTIONS=7
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
WANT="http://kacho-umbrella-hydra-public.kacho.svc:4444/.well-known/jwks.json"
# Боевой профиль забирает ключи через зеркало iam — единственный фасад к
# провайдеру (core #16), по защищённому транспорту с якорем доверия. Адрес пинится
# здесь ЛИТЕРАЛОМ: вычитывать ожидание из того же профиля, который и рендерится,
# значило бы сверять файл сам с собой.
WANT_PROD="https://kaname-internal.kacho.svc:9097/.well-known/jwks.json"
# Написания адреса ПРОВАЙДЕРА: любое из них в боевом профиле — обход фасада.
PROVIDER_SPELLING='hydra-public|hydra\.api\.'
# env_val <ENV_NAME> <render> — value of the named container env entry ("" if absent).
env_val() {
  echo "$2" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[].env[] | select(.name==\"$1\") | .value" -
}

[ -d "$AGW" ] \
  || fatal "чарта края нет по пути $AGW (объявлен в $UMBRELLA/Chart.yaml) — судить не о чем"

# ── (1) sibling chart standalone — hydra.jwksUrl drives the env ───────────────
helm_try ag "$AGW" --set hydra.jwksUrl="$WANT"
render_or_fatal "чарт края, hydra.jwksUrl задан"
ON="$HELM_OUT"
jw="$(env_val KACHO_HYDRA_JWKS_URL "$ON")"
[ -n "$jw" ] || fail "sibling chart did not render KACHO_HYDRA_JWKS_URL env when hydra.jwksUrl set"
[ "$jw" = "$WANT" ] || fail "sibling KACHO_HYDRA_JWKS_URL=$jw (want $WANT)"; ok
case "$jw" in
  *localhost*) fail "sibling JWKS URL points at localhost ($jw) — unreachable in-cluster" ;;
  https://hydra.*) fail "sibling JWKS URL points at the PUBLIC issuer ($jw) — unreachable in-cluster" ;;
esac; ok

# Default (no hydra.jwksUrl) must NOT leak the env — Go config default applies,
# zero regression for overlays that don't opt in.
helm_try ag "$AGW"
render_or_fatal "чарт края, умолчание"
OFF="$HELM_OUT"
[ -z "$(env_val KACHO_HYDRA_JWKS_URL "$OFF")" ] || fail "sibling leaks KACHO_HYDRA_JWKS_URL when hydra.jwksUrl unset"; ok

# ── (2) umbrella + values.dev.yaml — the actual dev stand ─────────────────────
# `helm template` resolves the file:// api-gateway dep from the vendored .tgz; if
# the dep is stale this still renders the committed chart. Restrict to the
# api-gateway Deployment via --show-only.
helm_try kacho-umbrella "$UMBRELLA" -f "$UMBRELLA/values.dev.yaml" \
        --show-only charts/api-gateway/templates/deployment.yaml
render_or_fatal "умбрелла + values.dev.yaml, шаблон пода края"
DEV="$HELM_OUT"
[ -n "$DEV" ] || fail "рендер шаблона пода края (dev) ПУСТ при успешном helm template"
# ФОРМ ОБЪЯВЛЕНИЯ ДВЕ, СВОЙСТВО ОДНО — то же, что для боевого профиля ниже.
# Здесь проба требовала одиночную ручку ИМЕНЕМ и потому краснела на верной
# посадке: третья фаза (#899) объявляет обоих издателей записью, а одиночную
# ручку снимает — держать обе значило бы иметь два объявления одного предмета,
# и страж старта на этом отказывает.
djw="$(env_val KACHO_HYDRA_JWKS_URL "$DEV")"
dks="$(env_val KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS "$DEV")"
if [ -n "$djw" ] && [ -n "$dks" ]; then
  fail "dev объявляет адрес набора ДВАЖДЫ (одиночная ручка и запись издателей) — старшинство между ними не назначается молча"
fi
if [ -z "$djw" ] && [ -z "$dks" ]; then
  fail "dev не объявляет адрес набора НИ ОДНОЙ формой — край выведет недостижимое умолчание, и ни один токен не проверится"
fi
ok

# Свойство, ради которого проба написана: КАЖДЫЙ адрес, который край будет
# тянуть, достижим из пода. Одиночная ручка даёт один адрес, запись издателей —
# по одному на издателя, и проверить надо все: достижимый первый и недостижимый
# второй дают ровно тот отказ, который проба обязана ловить.
if [ -n "$dks" ]; then
  dev_urls="$(printf '%s' "$dks" | tr ',' '\n' | sed 's/^[^=]*=//')"
else
  dev_urls="$djw"
fi
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

# SEC-J: the verifier does an EXACT-match `iss` check, so the dev gateway issuer
# MUST equal Hydra's dev self.issuer (values.dev.yaml hydra.config.urls.self.issuer
# = http://localhost:28080/.ory/hydra/public/). Without it, KACHO_HYDRA_ISSUER
# derives the unreachable external default → every real login token fails the iss
# check → AUTHN_REQUIRED persists even with a reachable JWKS URL.
DEV_ISSUER="http://localhost:28080/.ory/hydra/public/"
dis="$(env_val KACHO_HYDRA_ISSUER "$DEV")"
dissuers="$(env_val KACHO_API_GATEWAY_TOKEN_ISSUERS "$DEV")"
if [ -n "$dis" ]; then
  [ "$dis" = "$DEV_ISSUER" ] || fail "dev KACHO_HYDRA_ISSUER=$dis (want $DEV_ISSUER matching Hydra dev self.issuer)"
else
  # Издатель назван записью перечня — его строка обязана совпадать ДОСЛОВНО,
  # включая завершающий слеш: `iss` сверяется целиком, и лишний символ здесь
  # означает отказ каждому живому токену.
  case ",$dissuers," in
    *",$DEV_ISSUER,"*) ;;
    *) fail "dev не называет издателя провайдера ни ручкой, ни записью перечня (перечень: $dissuers) — токены прежней чеканки перестанут приниматься" ;;
  esac
fi
ok

# ── (3) umbrella + values.prod.yaml — production-strict makes the verifier
#        mandatory, so the JWKS URL must be the in-cluster address of the iam
#        MIRROR (core #16: iam is the only facade to the provider), over TLS —
#        not the public ingress hairpin and not the provider's own Service.
#        The expected `iss` stays the public issuer: the provider remains the
#        SIGNER, only key distribution goes through iam.
helm_try kacho-umbrella "$UMBRELLA" -f "$UMBRELLA/values.prod.yaml" \
         --show-only charts/api-gateway/templates/deployment.yaml
render_or_fatal "умбрелла + values.prod.yaml, шаблон пода края"
PROD="$HELM_OUT"
[ -n "$PROD" ] || fail "рендер шаблона пода края (prod) ПУСТ при успешном helm template"
# ФОРМ ОБЪЯВЛЕНИЯ ДВЕ, СВОЙСТВО ОДНО. Адрес набора ключей объявляется либо
# прежней одиночной ручкой, либо записью издателей (Ф1б, #926: платформа
# принимает ДВУХ издателей, и у каждого свой набор). Проба обязана судить
# СВОЙСТВО — «материал проверки едет через зеркало iam, по TLS, не от
# провайдера напрямую», — а не имя ручки: иначе она переживает свой предмет и
# краснеет на верной посадке. Ровно это и произошло, когда запись издателей
# пришла, а проба продолжала требовать снятую ручку.
pjw="$(env_val KACHO_HYDRA_JWKS_URL "$PROD")"
pks="$(env_val KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS "$PROD")"
if [ -n "$pjw" ] && [ -n "$pks" ]; then
  fail "prod объявляет адрес набора ДВАЖДЫ (одиночная ручка и запись издателей) — два объявления об одном предмете, из которых верно одно"
fi
if [ -z "$pjw" ] && [ -z "$pks" ]; then
  fail "prod не объявляет адрес набора НИ ОДНОЙ формой — краю нечем проверять подписи, и это не будет видно до первого предъявления"
fi

# Перечень адресов, которые край реально будет тянуть: одиночная ручка даёт
# один, запись издателей — по одному на издателя.
if [ -n "$pks" ]; then
  prod_urls="$(printf '%s' "$pks" | tr ',' '\n' | sed 's/^[^=]*=//')"
else
  prod_urls="$pjw"
fi

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
# Издатель — та же история двух форм: либо прежняя одиночная ручка, либо
# перечень принимаемых издателей. Публичный издатель обязан быть назван в той
# форме, которая действует.
pis="$(env_val KACHO_HYDRA_ISSUER "$PROD")"
pissuers="$(env_val KACHO_API_GATEWAY_TOKEN_ISSUERS "$PROD")"
if [ -n "$pis" ]; then
  [ "$pis" = "https://hydra.api.kacho.cloud" ] || fail "prod KACHO_HYDRA_ISSUER=$pis (want public issuer https://hydra.api.kacho.cloud)"
elif [ -n "$pissuers" ]; then
  case ",$pissuers," in
    *,https://hydra.api.kacho.cloud,*) ;;
    *) fail "prod перечень принимаемых издателей не называет публичного издателя https://hydra.api.kacho.cloud: $pissuers" ;;
  esac
else
  fail "prod не объявляет издателя НИ ОДНОЙ формой — токен принимается без сверки того, кто его выпустил"
fi; ok

outcome_verdict "профилей прочитано: 2 (dev, prod) + чарт края отдельно"
