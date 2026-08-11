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

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
MONOREPO="$(cd "$REPO_ROOT/.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
# Путь берётся из Chart.yaml умбреллы, а не пишется рядом второй раз: пока чарт
# шлюза жил соседним репозиторием, тут стояло `../kacho-api-gateway/deploy`, и
# после переезда в монорепу проверка падала первой же строкой. Читаем ОБЪЯВЛЕННЫЙ
# источник — тогда следующий переезд чинит сам себя.
AGW="$(sed -nE 's#^[[:space:]]*repository:[[:space:]]*file://\.\./\.\./\.\./(.*)$#\1#p' \
        "$UMBRELLA/Chart.yaml" | grep -m1 'gateway')"
AGW="$MONOREPO/$AGW"
WANT="http://kacho-umbrella-hydra-public.kacho.svc:4444/.well-known/jwks.json"
# Боевой профиль забирает ключи через зеркало iam — единственный фасад к
# провайдеру (core #16), по защищённому транспорту с якорем доверия. Адрес пинится
# здесь ЛИТЕРАЛОМ: вычитывать ожидание из того же профиля, который и рендерится,
# значило бы сверять файл сам с собой.
WANT_PROD="https://kacho-iam-internal.kacho.svc:9097/.well-known/jwks.json"
# Написания адреса ПРОВАЙДЕРА: любое из них в боевом профиле — обход фасада.
PROVIDER_SPELLING='hydra-public|hydra\.api\.'
N=0
fail() { echo "FAIL: $1"; exit 1; }
ok() { N=$((N + 1)); }

# env_val <ENV_NAME> <render> — value of the named container env entry ("" if absent).
env_val() {
  echo "$2" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[].env[] | select(.name==\"$1\") | .value" -
}

[ -d "$AGW" ] || fail "api-gateway chart not found at $AGW (объявлен в $UMBRELLA/Chart.yaml)"

# ── (1) sibling chart standalone — hydra.jwksUrl drives the env ───────────────
ON="$(helm template ag "$AGW" --set hydra.jwksUrl="$WANT" 2>/dev/null)"
jw="$(env_val KACHO_HYDRA_JWKS_URL "$ON")"
[ -n "$jw" ] || fail "sibling chart did not render KACHO_HYDRA_JWKS_URL env when hydra.jwksUrl set"
[ "$jw" = "$WANT" ] || fail "sibling KACHO_HYDRA_JWKS_URL=$jw (want $WANT)"; ok
case "$jw" in
  *localhost*) fail "sibling JWKS URL points at localhost ($jw) — unreachable in-cluster" ;;
  https://hydra.*) fail "sibling JWKS URL points at the PUBLIC issuer ($jw) — unreachable in-cluster" ;;
esac; ok

# Default (no hydra.jwksUrl) must NOT leak the env — Go config default applies,
# zero regression for overlays that don't opt in.
OFF="$(helm template ag "$AGW" 2>/dev/null)"
[ -z "$(env_val KACHO_HYDRA_JWKS_URL "$OFF")" ] || fail "sibling leaks KACHO_HYDRA_JWKS_URL when hydra.jwksUrl unset"; ok

# ── (2) umbrella + values.dev.yaml — the actual dev stand ─────────────────────
# `helm template` resolves the file:// api-gateway dep from the vendored .tgz; if
# the dep is stale this still renders the committed chart. Restrict to the
# api-gateway Deployment via --show-only.
DEV="$(helm template kacho-umbrella "$UMBRELLA" -f "$UMBRELLA/values.dev.yaml" \
        --show-only charts/api-gateway/templates/deployment.yaml 2>/dev/null)"
[ -n "$DEV" ] || fail "umbrella render of api-gateway deployment is empty (dep not built? run helm dep update)"
djw="$(env_val KACHO_HYDRA_JWKS_URL "$DEV")"
[ -n "$djw" ] || fail "dev stand api-gateway pod has NO KACHO_HYDRA_JWKS_URL env — gateway will fetch unreachable default"
[ "$djw" = "$WANT" ] || fail "dev KACHO_HYDRA_JWKS_URL=$djw (want cluster-internal $WANT)"; ok
case "$djw" in
  *localhost*) fail "dev JWKS URL points at localhost ($djw) — gateway pod cannot reach it" ;;
  https://hydra.*) fail "dev JWKS URL points at PUBLIC issuer ($djw) — not reachable from the gateway pod" ;;
esac; ok

# SEC-J: the verifier does an EXACT-match `iss` check, so the dev gateway issuer
# MUST equal Hydra's dev self.issuer (values.dev.yaml hydra.config.urls.self.issuer
# = http://localhost:28080/.ory/hydra/public/). Without it, KACHO_HYDRA_ISSUER
# derives the unreachable external default → every real login token fails the iss
# check → AUTHN_REQUIRED persists even with a reachable JWKS URL.
DEV_ISSUER="http://localhost:28080/.ory/hydra/public/"
dis="$(env_val KACHO_HYDRA_ISSUER "$DEV")"
[ "$dis" = "$DEV_ISSUER" ] || fail "dev KACHO_HYDRA_ISSUER=$dis (want $DEV_ISSUER matching Hydra dev self.issuer)"; ok

# ── (3) umbrella + values.prod.yaml — production-strict makes the verifier
#        mandatory, so the JWKS URL must be the in-cluster address of the iam
#        MIRROR (core #16: iam is the only facade to the provider), over TLS —
#        not the public ingress hairpin and not the provider's own Service.
#        The expected `iss` stays the public issuer: the provider remains the
#        SIGNER, only key distribution goes through iam.
PROD="$(helm template kacho-umbrella "$UMBRELLA" -f "$UMBRELLA/values.prod.yaml" \
         --show-only charts/api-gateway/templates/deployment.yaml 2>/dev/null)"
[ -n "$PROD" ] || fail "umbrella render of api-gateway deployment (prod) is empty"
pjw="$(env_val KACHO_HYDRA_JWKS_URL "$PROD")"
[ "$pjw" = "$WANT_PROD" ] || fail "prod KACHO_HYDRA_JWKS_URL=$pjw (want the iam mirror $WANT_PROD)"
case "$pjw" in
  https://*) ;;
  *) fail "prod JWKS URL is not TLS ($pjw) — the material that verifies every bearer's signature travels this hop" ;;
esac
if printf '%s' "$pjw" | grep -qE "$PROVIDER_SPELLING"; then
  fail "prod JWKS URL addresses the provider directly ($pjw) — that bypasses the iam facade (core #16), a hop already found and closed once"
fi
# Якорь доверия обязан быть смонтирован: TLS без проверки сертификата на этом хопе
# читается как настроенная защита, ничего не проверяя.
printf '%s\n' "$PROD" | grep -q 'hydra-jwks-ca' \
  || fail "prod api-gateway pod carries no trust anchor for the JWKS hop — TLS whose certificate nobody checks leaves substitution open"
pis="$(env_val KACHO_HYDRA_ISSUER "$PROD")"
[ "$pis" = "https://hydra.api.kacho.cloud" ] || fail "prod KACHO_HYDRA_ISSUER=$pis (want public issuer https://hydra.api.kacho.cloud)"; ok

echo "PASS: $SCRIPT ($N assertions)"
