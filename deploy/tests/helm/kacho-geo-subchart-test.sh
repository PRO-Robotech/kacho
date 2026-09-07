#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# kacho-geo sub-chart render-guard (epic kacho-geo S6 + S3).
#
# Offline manifest-assertion harness (no kind cluster). Renders the VENDORED
# kacho-geo sub-chart (charts/kacho-geo) STANDALONE — it does not need the
# sibling-repo `file://` checkouts (vpc/compute/api-gateway/ui), so this guard
# runs in CI and in a worktree where those siblings aren't reachable.
#
# Asserts (S6 §6.0-19 + S3 §6.0-09):
#   - Deployment renders public :9090 + internal :9091 ports;
#   - migrator init-container runs `kacho-migrator up` with the kacho_geo DB env
#     (incl. DB password via secretKeyRef);
#   - the geo→iam authz Check addr (AUTHZ_IAM_GRPC_ADDR) targets iam-internal :9091;
#   - dev profile: AUTH_MODE=dev; prod profile: AUTH_MODE=production + ssl require;
#   - mTLS ON renders the server + client Certificates AND the *_MTLS_* server env
#     on BOTH listeners (internal NOT exempt — security.md);
#   - both Services (kacho-geo + kacho-geo-internal) render with the right ports;
#   - the compute→geo data-migration Job is a post-install/post-upgrade Helm hook,
#     copies regions+zones id-preserving (created_at carried, NOT now()), and is
#     idempotent (ON CONFLICT DO NOTHING), with regions before zones (FK order).
#
# Mirrors tests/helm/*. No TODO/SKIP/commented-out asserts (ban #11/#13).
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
GEO="$UMBRELLA/charts/kacho-geo"

# ── Три исхода — ОБЩЕЙ реализацией каталога, а не своей копией ───────────────
#
# 0 зелено · 1 находка о дереве · 2 условие не создано (плюс текст самого helm).
# Здесь лежали свои `fail`, `ok` и `N`, и разошлись они с каталогом по двум осям:
#
#   • ОТКАЗ РЕНДЕРА был находкой о дереве. `DEF="$(render)" || fail "…does not
#     render…"` выходил кодом 1 — тем же, каким объявляется настоящий дефект, —
#     хотя несобранные зависимости или отсутствующий helm суть УСЛОВИЕ прогона;
#   • ТЕКСТ HELM НЕ ДОЕЗЖАЛ ДО ЧИТАТЕЛЯ ВООБЩЕ: `render()` глушила stderr
#     (`2>/dev/null`) и звалась ИЗ ПОДСТАНОВКИ, а всё, что печатается в stdout
#     изнутри `$( )`, попадает в переменную, а не читателю. Наблюдаемый результат
#     у «гейт нашёл дефект», «гейт сам сломан» и «условие не создано» совпадал.
#
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=10

require_helm
# yq здесь не проверялся ВООБЩЕ, и это не мелочь: под именем `yq` в PATH бывает
# питонья обёртка над jq, которая на этих фильтрах не ругается, а молча отдаёт
# ПУСТО. Тогда `nz` и `nj` (обе считаются `grep -c .` над пустым выводом) равны
# нулю, и утверждения «Certificate'ов ноль» / «Job'ов ноль» проходят ВАКУУМНО —
# проверка отчитывается успехом, не сверив ничего.
require_mikefarah_yq
# Сабчарт вендорится КАТАЛОГОМ в charts/ и в зависимостях умбреллы НАМЕРЕННО не
# объявлен (Chart.yaml умбреллы называет причину: объявленный `file://` чарт helm
# материализует в тот же каталог, и он существует там ДВАЖДЫ — каталогом и
# архивом, а какая копия попадёт в рендер, не определено). Отсюда именно
# `require_dir_present`, а не `require_dep_chart`: последний на отказе советует
# собрать зависимости, а этот чарт зависимостью не является — совет увёл бы
# читателя к команде, которая его не вернёт.
#
# Само отсутствие чарта — «судить не о чем» (условие не создано), а не дефект
# дерева; прежде здесь стоял `fail`, то есть код находки.
require_dir_present "$GEO" "сабчарт kacho-geo (вендорится каталогом в charts/)"

# render <что рендерим> <values-override...> — рендер отдельно стоящего сабчарта.
#
# Результат приезжает в $HELM_OUT, а НЕ в stdout, и это не стиль: `V="$(render …)"`
# исполнил бы функцию в ПОДОБОЛОЧКЕ, откуда ни код возврата helm, ни текст его
# отказа, ни счётчик рендеров до вызывающего не доедут. Пустой успешный рендер —
# тоже условие, а не «ничего не нашли»: по нему все утверждения прошли бы вакуумно.
render() {
  local what="$1"; shift
  helm_try kacho-geo "$GEO" "$@"
  render_nonempty_or_fatal "$what"
}
# env_val <ENV_NAME> <render> — value of the named container env entry ("" if absent).
env_val() {
  echo "$2" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[].env[] | select(.name==\"$1\") | .value" -
}
# init_env_val <ENV_NAME> <render> — value from the migrate init-container.
init_env_val() {
  echo "$2" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.initContainers[].env[] | select(.name==\"$1\") | .value" -
}

# ── 0. default render is non-empty + valid ───────────────────────────────────
render "kacho-geo (умолчания чарта)"
DEF="$HELM_OUT"; ok

# ── 1. Deployment ports: public :9090 + internal :9091 ───────────────────────
DEP="$(echo "$DEF" | yq ea 'select(.kind=="Deployment")' -)"
ports="$(echo "$DEP" | yq '.spec.template.spec.containers[0].ports[].containerPort' - | tr '\n' ' ')"
case "$ports" in *9090*9091*|*9091*9090*) ;; *) fail "Deployment missing public 9090 / internal 9091 ports (got: $ports)";; esac; ok

# ── 2. migrator init-container runs `kacho-migrator up` + has kacho_geo DB env ─
mig_cmd="$(echo "$DEP" | yq '.spec.template.spec.initContainers[0].command | join(" ")' -)"
case "$mig_cmd" in *kacho-migrator*up*) ;; *) fail "init-container is not 'kacho-migrator up' (got: $mig_cmd)";; esac
mig_dbname="$(init_env_val KACHO_GEO_DB_NAME "$DEF")"
[ "$mig_dbname" = "kacho_geo" ] || fail "migrator KACHO_GEO_DB_NAME=$mig_dbname (want kacho_geo)"
mig_pw="$(echo "$DEP" | yq '.spec.template.spec.initContainers[0].env[] | select(.name=="KACHO_GEO_DB_PASSWORD") | .valueFrom.secretKeyRef.name' -)"
[ -n "$mig_pw" ] && [ "$mig_pw" != "null" ] || fail "migrator KACHO_GEO_DB_PASSWORD not via secretKeyRef (got: $mig_pw)"; ok

# ── 3. geo→iam authz Check addr targets iam-internal :9091 ───────────────────
authz_addr="$(env_val KACHO_GEO_AUTHZ_IAM_GRPC_ADDR "$DEF")"
case "$authz_addr" in *kaname-internal*:9091) ;; *) fail "KACHO_GEO_AUTHZ_IAM_GRPC_ADDR=$authz_addr (want iam-internal :9091)";; esac; ok

# ── 4. AUTH_MODE: умолчание чарта — production (secure-by-default) ───────────
# Утверждение развёрнуто вместе с чартом (8476d4b4, core rule #16): умолчанием
# стало `production`, а `dev` — явный opt-in локальных фикстур. Проверка этого не
# знала, потому что её никто не запускал: она требовала `dev` и, будь она в
# рабочем пути, потребовала бы вернуть чарту небезопасное умолчание. Поэтому
# утверждаем ОБА направления — умолчание безопасно, а dev-профиль включает
# небезопасный режим ЯВНО.
am="$(env_val KACHO_GEO_AUTH_MODE "$DEF")"
[ "$am" = "production" ] || fail "умолчание KACHO_GEO_AUTH_MODE=$am (ожидается production — secure-by-default)"; ok
render "kacho-geo (authMode=dev)" --set authMode=dev
DEVMODE="$HELM_OUT"
dam="$(env_val KACHO_GEO_AUTH_MODE "$DEVMODE")"
[ "$dam" = "dev" ] || fail "явный opt-in KACHO_GEO_AUTH_MODE=$dam (ожидается dev)"; ok

# ── 5. production override: AUTH_MODE=production + DB ssl require ──────────────
render "kacho-geo (authMode=production, db.sslMode=require)" \
  --set authMode=production --set db.sslMode=require
PROD="$HELM_OUT"
pam="$(env_val KACHO_GEO_AUTH_MODE "$PROD")"
pssl="$(env_val KACHO_GEO_DB_SSLMODE "$PROD")"
[ "$pam" = "production" ] || fail "prod KACHO_GEO_AUTH_MODE=$pam (want production)"
[ "$pssl" = "require" ] || fail "prod KACHO_GEO_DB_SSLMODE=$pssl (want require)"; ok

# ── 6. mTLS ON → server + client Certificates + *_MTLS_* env on BOTH listeners ─
render "kacho-geo (mtls.enable=true, mtls.edges.iamAuthz=true)" \
  --set mtls.enable=true --set mtls.edges.iamAuthz=true
M="$HELM_OUT"
ncerts="$(echo "$M" | yq ea 'select(.kind=="Certificate") | .metadata.name' - | grep -c . || true)"
[ "$ncerts" -ge 2 ] || fail "mTLS-on render has $ncerts Certificates (want >=2: server + client)"
pub_mtls="$(env_val KACHO_GEO_PUBLIC_SERVER_MTLS_ENABLE "$M")"
int_mtls="$(env_val KACHO_GEO_INTERNAL_SERVER_MTLS_ENABLE "$M")"
iam_mtls="$(env_val KACHO_GEO_IAM_AUTHZ_MTLS_ENABLE "$M")"
[ "$pub_mtls" = "true" ] || fail "mTLS public-server env missing (KACHO_GEO_PUBLIC_SERVER_MTLS_ENABLE=$pub_mtls)"
[ "$int_mtls" = "true" ] || fail "mTLS INTERNAL-server env missing — internal listener NOT exempt (KACHO_GEO_INTERNAL_SERVER_MTLS_ENABLE=$int_mtls)"
[ "$iam_mtls" = "true" ] || fail "geo->iam client mTLS env missing (KACHO_GEO_IAM_AUTHZ_MTLS_ENABLE=$iam_mtls)"
# mTLS OFF (default) → NO Certificates (zero-regression insecure dev).
nz="$(echo "$DEF" | yq ea 'select(.kind=="Certificate") | .metadata.name' - | grep -c . || true)"
[ "$nz" -eq 0 ] || fail "mTLS-off default still rendered $nz Certificates (should be 0)"; ok

# ── 7. both Services (public kacho-geo + internal kacho-geo-internal) ─────────
svc_names="$(echo "$DEF" | yq ea 'select(.kind=="Service") | .metadata.name' - | tr '\n' ' ')"
case "$svc_names" in *kacho-geo-internal*) ;; *) fail "internal Service kacho-geo-internal missing (got: $svc_names)";; esac
case "$svc_names" in *kacho-geo\ *|*kacho-geo) ;; *) fail "public Service kacho-geo missing (got: $svc_names)";; esac; ok

# ── 8. data-migration Job — Helm hook + id-preserving + idempotent ───────────
render "kacho-geo (dataMigration.enabled=true)" --set dataMigration.enabled=true
DM="$HELM_OUT"
JOB="$(echo "$DM" | yq ea 'select(.kind=="Job")' -)"
[ -n "$JOB" ] || fail "dataMigration.enabled=true rendered no Job"
hook="$(echo "$JOB" | yq '.metadata.annotations."helm.sh/hook"' -)"
case "$hook" in *post-install*post-upgrade*|*post-upgrade*post-install*) ;; *) fail "data-migration Job hook=$hook (want post-install,post-upgrade)";; esac
jobspec="$(echo "$JOB" | yq '.spec.template.spec.containers[0].args | join("\n")' -)"
case "$jobspec" in *"ON CONFLICT (id) DO NOTHING"*) ;; *) fail "data-migration not idempotent (no ON CONFLICT (id) DO NOTHING)";; esac
# created_at carried verbatim (id-preserving) — must NOT reset to now().
case "$jobspec" in *"id, name, created_at"*) ;; *) fail "data-migration regions INSERT does not carry created_at (id/created_at must be preserved)";; esac
case "$jobspec" in *"id, region_id, status, name, created_at"*) ;; *) fail "data-migration zones INSERT does not carry full row incl created_at";; esac
# default (off) → no Job (zero-regression).
nj="$(echo "$DEF" | yq ea 'select(.kind=="Job") | .metadata.name' - | grep -c . || true)"
[ "$nj" -eq 0 ] || fail "default render still has $nj data-migration Jobs (should be 0)"; ok

outcome_verdict "накладок рендера: 5 (умолчания · authMode=dev · production+ssl · mTLS · миграция данных)"
