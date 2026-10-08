#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stand-ns.sh — стенд проб в СВОЁМ пространстве имён общего кластера (kacho#3102).
#
#   stand-ns.sh up NS [STACK] [REF]   поднять стек STACK (умолчание dev-prod) от ревизии REF
#   stand-ns.sh down NS               снять пространство целиком; идемпотентно
#   stand-ns.sh census [--expired]    перечень тестовых пространств; --expired снимает просроченные
#   stand-ns.sh forward NS            проброс консоли, края и приёмника писем + файл окружения проб
#   stand-ns.sh unforward NS          снять проброс
#   stand-ns.sh self-test             проба правил имени, срока и порта (кластера не требует)
#
# Зовут его цели `make -C deploy stand-ns-up | stand-ns-down | stand-ns-census`.
#
# ── ЗАЧЕМ ──────────────────────────────────────────────────────────────────────
#
# Сквозные пробы консоли и стендовые пробы поднимали локальный kind: стек `dev-up`
# ставится на кластер ЦЕЛИКОМ, в пространство `kacho`, и второй экземпляр рядом с
# рабочим стендом поставить было нечем. Решение владельца 2026-10-08: стенды проб —
# в своих пространствах имён внешнего кластера, и задача, их поднявшая, их же и
# снимает.
#
# ── ЧТО ЗНАЧИТ «ТОТ ЖЕ СТЕК» ───────────────────────────────────────────────────
#
# Цепочка — из deploy/stacks.txt (умолчание `dev-prod`, тот же состав, что
# поднимает конвейерный «свой стенд» последней фазой `make dev-up`). Сверх неё
# стенд накладывает ровно три слоя, и ни один не трогает посадку:
#
#   images.yaml   образы частей дерева — ОПУБЛИКОВАННЫЕ для ревизии REF
#                 (узлов kind нет, исполнить можно только то, что лежит в реестре);
#   stand.yaml    приспособление к кластеру, ИЗМЕРЕННОЕ у него: cert-manager не
#                 ставится (переиспользуется существующий), контроллер входа не
#                 ставится, тома — только если у кластера есть класс по умолчанию;
#                 и адрес консоли этого стенда;
#   перенос       пост-обработчик рендера (tools/standns): адреса соседей,
#                 издатели и корень внутреннего CA — в пространство стенда.
#
# Общекластерного стенд не ставит и не снимает: перенос отказывает на любом
# общекластерном виде, кроме СВОИХ издателей CA (они помечены и снимаются вместе
# со стендом).
#
# ── ИМЯ, МЕТКИ, СРОК ──────────────────────────────────────────────────────────
#
#   имя        t<номер задачи>-<коротко>, например t3102-probe; не длиннее 40
#              знаков (суффикс имён корня CA — само имя, предел имени — 63)
#   метки      kacho.io/task=<номер>, kacho.io/stand=test
#   аннотации  kacho.io/expires=<RFC3339> (умолчание — через STAND_TTL_HOURS=12 ч),
#              kacho.io/ref=<коммит образов>, kacho.io/stack=<цепочка>
#
# Пространство `kacho` — рабочий стенд: имя не проходит правила, и это проверяется
# ДО обращения к кластеру.
#
# ── КЛАСТЕР НАЗЫВАЕТСЯ ЯВНО ──────────────────────────────────────────────────
#
# Меняющие режимы требуют STAND_APISERVER — адрес apiserver'а кластера, с которым
# kubectl говорит на самом деле (scripts/stand-cluster-pin.sh): цель пишет в
# кластер активного контекста, и «наверное, тот» выбором кластера не является.
# Подтверждения человеком НЕ спрашивается: пространство своё, помеченное, и
# пишется только в него и в помеченные свои объекты.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_ROOT/.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
RELEASE="${STACK_RELEASE:-kacho-umbrella}"
WORK_ROOT="${KACHO_STAND_NS_WORKDIR:-$DEPLOY_ROOT/.stand-ns}"
TTL_HOURS="${STAND_TTL_HOURS:-12}"
IMAGE_PREFIX="${STAND_IMAGE_PREFIX:-docker.io/prorobotech}"
IMAGE_BRANCHES="${STAND_IMAGE_BRANCHES:-1266 main}"
UP_TIMEOUT="${STAND_TIMEOUT:-15m}"
HOST_SUFFIX="kacho.test"
LABEL_STAND="kacho.io/stand"
LABEL_TASK="kacho.io/task"
LABEL_STAND_NS="kacho.io/stand-ns"
ANN_EXPIRES="kacho.io/expires"

log()  { printf '=== stand-ns: %s\n' "$*"; }
warn() { printf 'stand-ns: %s\n' "$*" >&2; }
die()  { printf 'ABORT: stand-ns — %s\n' "$1" >&2; exit "${2:-1}"; }

# ── ПРАВИЛА БЕЗ КЛАСТЕРА ─────────────────────────────────────────────────────

# ns_valid NS — 0, если имя по правилу t<номер>-<коротко>; печатает номер задачи.
ns_valid() {
  local ns="$1"
  [ "${#ns}" -le 40 ] || return 1
  [[ "$ns" =~ ^t([0-9]+)-[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || return 1
  printf '%s' "${BASH_REMATCH[1]}"
}

# ns_port NS — первый из трёх портов проброса (консоль, край, приёмник писем).
# Выводится из имени, а не выбирается: он входит в происхождение консоли, а
# происхождение запекается в настройки службы доступа при подъёме.
ns_port() {
  local sum
  sum="$(printf '%s' "$1" | cksum | awk '{print $1}')"
  printf '%s' "$(( 20000 + (sum % 6000) * 3 ))"
}

ns_host() { printf '%s.%s' "$1" "$HOST_SUFFIX"; }

# expired_at RFC3339 NOW_EPOCH — 0, если срок истёк; 2, если срок не читается.
expired_at() {
  local t
  t="$(date -u -d "$1" +%s 2>/dev/null)" || return 2
  [ "$t" -le "$2" ]
}

self_test() {
  local pass=0 fail=0
  ok()  { pass=$((pass + 1)); printf '  ok   %s\n' "$1"; }
  bad() { fail=$((fail + 1)); printf '  FAIL %s\n' "$1"; }
  if [ "$(ns_valid t3102-probe)" = 3102 ]; then ok "t3102-probe → задача 3102"; else bad "t3102-probe не принят"; fi
  for n in kacho t-probe t3102 T3102-probe t3102-Probe t3102-probe- "t3102-$(printf 'x%.0s' {1..40})" kacho-t1-x; do
    if ns_valid "$n" >/dev/null; then bad "«$n» принят"; else ok "«$n» отвергнут"; fi
  done
  local p; p="$(ns_port t3102-probe)"
  if [ "$p" -ge 20000 ] && [ $((p + 2)) -le 37999 ] && [ "$p" = "$(ns_port t3102-probe)" ]; then
    ok "порт t3102-probe = $p, устойчив"; else bad "порт $p вне [20000, 37999]"; fi
  if [ "$(ns_port t3102-probe)" != "$(ns_port t896-heavy-probe)" ]; then ok "соседние имена — разные порты"; else bad "порты совпали"; fi
  local now; now="$(date -u +%s)"
  if expired_at "2000-01-01T00:00:00Z" "$now"; then ok "срок в прошлом — истёк"; else bad "прошлый срок не истёк"; fi
  if expired_at "2999-01-01T00:00:00Z" "$now"; then bad "будущий срок истёк"; else ok "срок в будущем — жив"; fi
  local rc=0; expired_at "не дата" "$now" || rc=$?
  if [ "$rc" -eq 2 ]; then ok "нечитаемый срок — код 2, не «истёк»"; else bad "нечитаемый срок прочитан (код $rc)"; fi
  printf 'самопроверка stand-ns: прошло %d, провалено %d\n' "$pass" "$fail"
  [ "$fail" -eq 0 ]
}

# ── КЛАСТЕР ──────────────────────────────────────────────────────────────────

need_tools() {
  local t
  for t in kubectl helm python3 openssl jq curl go git; do
    command -v "$t" >/dev/null 2>&1 || die "нет '$t' — исполнить нечем (условие прогона, а не находка)" 2
  done
}

# guard_context — кластер пинится АДРЕСОМ apiserver'а, а не именем контекста:
# имя — ярлык автора kubeconfig, одноимённый контекст уже вёл в другой кластер.
# Авторитет — объявление STAND_APISERVER при вызове (адрес площадки в дерево не
# пишется), сверку делает тот же страж, что у гейта посадки
# (scripts/stand-cluster-pin.sh). Имя контекста запоминается ПОСЛЕ сверки — его
# просит цель доставки манифестов модулей.
guard_context() {
  [ -n "${STAND_APISERVER:-}" ] || die "STAND_APISERVER не задан, а цель пишет в кластер активного контекста.
       Объяви адрес apiserver'а кластера, на который ставится стенд:
         STAND_APISERVER=\"\$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')\"" 2
  bash "$HERE/stand-cluster-pin.sh" || die "активный кластер не объявленный (выше)" 2
  STAND_CTX="$(kubectl config current-context 2>/dev/null)"
}

# ns_state NS — печатает absent | test | foreign; код 2 — кластер не ответил.
ns_state() {
  local out
  if out="$(kubectl get namespace "$1" -o json --request-timeout=30s 2>&1)"; then
    if [ "$(jq -r --arg l "$LABEL_STAND" '.metadata.labels[$l] // ""' <<<"$out")" = test ]; then
      echo test
    else
      echo foreign
    fi
    return 0
  fi
  case "$out" in *"(NotFound)"*) echo absent; return 0 ;; esac
  printf '%s\n' "$out" >&2
  return 2
}

build_tool() {
  local out
  out="${TMPDIR:-/tmp}/kacho-stand-ns.$(id -u).$(printf '%s' "$REPO_ROOT" | sha256sum | cut -c1-16)"
  mkdir -p "$out" || return 1
  ( cd "$REPO_ROOT" && go build -o "$out/stand-ns" ./tools/standns/cmd/stand-ns ) >&2 || {
    warn "tools/standns не собрался — переносить рендер нечем"; return 1; }
  STAND_NS_BIN="$out/stand-ns"
}

# ca_namespace — пространство ресурсов cert-manager, ИЗМЕРЕННОЕ у его контроллера.
ca_namespace() {
  local js
  js="$(kubectl get deploy -A -l app.kubernetes.io/name=cert-manager,app.kubernetes.io/component=controller -o json --request-timeout=30s)" || return 2
  jq -r '
    [.items[] | . as $d
     | ([.spec.template.spec.containers[].args[]? | select(startswith("--cluster-resource-namespace="))
         | sub("^--cluster-resource-namespace="; "")][0] // "$(POD_NAMESPACE)") as $v
     | (if $v == "$(POD_NAMESPACE)" then $d.metadata.namespace else $v end)] | unique | .[]' <<<"$js"
}

has_default_storage_class() {
  kubectl get storageclass -o json --request-timeout=30s | jq -e \
    '[.items[] | select(.metadata.annotations["storageclass.kubernetes.io/is-default-class"] == "true")] | length > 0' >/dev/null
}

# ── ПОДЪЁМ ───────────────────────────────────────────────────────────────────

# Квота и пределы пространства. Числа — по замеру стенда dev-prod на этом
# кластере (kacho#3102, комментарий задачи «замер подъёма»): сумма запросов
# поднятого стенда и пик потребления, с запасом на один перекат (helm --wait
# поднимает новый под рядом со старым). Балансировщиков ноль: вход стенда
# пробрасывается, площадка его не публикует.
quota_manifest() {
  cat <<EOF
apiVersion: v1
kind: ResourceQuota
metadata:
  name: stand
  namespace: $1
  labels: {$LABEL_STAND: test}
spec:
  hard:
    requests.cpu: "4"
    requests.memory: 10Gi
    limits.memory: 40Gi
    pods: "60"
    services.loadbalancers: "0"
    services.nodeports: "0"
    persistentvolumeclaims: "20"
    requests.storage: 40Gi
---
apiVersion: v1
kind: LimitRange
metadata:
  name: stand
  namespace: $1
  labels: {$LABEL_STAND: test}
spec:
  limits:
    - type: Container
      defaultRequest: {cpu: 10m, memory: 32Mi}
      default: {memory: 512Mi}
      max: {memory: 8Gi}
EOF
}

stand_overlay() {
  local ns="$1" cans="$2" persist="$3" port="$4"
  local host; host="$(ns_host "$ns")"
  cat <<EOF
# generated by deploy/scripts/stand-ns.sh — не редактировать, не коммитить
# Приспособление цепочки к кластеру, ИЗМЕРЕННОЕ у него, и адрес консоли стенда $ns.
cert-manager:
  enabled: false
ingress-nginx:
  enabled: false
mtls:
  internalCA:
    caNamespace: $cans
global:
  kacho:
    identity:
      appBaseURL: "https://$host:$port"
      webauthnRpId: "$host"
uif:
  subscriptionStream:
    ingress:
      enabled: true
  publicFront:
    enabled: true
    service:
      type: ClusterIP
    tls:
      secretName: console-public-tls
      certificate:
        create: true
        namesFromOrigin: true
        issuerRef:
          name: kacho-internal-ca
          kind: ClusterIssuer
    acme:
      enabled: false
EOF
  if [ "$persist" = off ]; then
    # Класса томов по умолчанию у кластера нет: заявка на том висела бы в Pending
    # вечно. Базы — псевдонимы подчарта postgresql в Chart.yaml зонтика (перечень
    # выводится, а не выписывается); хранилище слоёв реестра — своей ручкой.
    # Пропущенную заявку ловит предрендер ниже (persistent_claims).
    local pg
    for pg in $(python3 -c 'import sys,yaml; [print(d.get("alias") or d["name"]) for d in yaml.safe_load(open(sys.argv[1]))["dependencies"] if d["name"]=="postgresql"]' "$UMBRELLA/Chart.yaml"); do
      printf '%s:\n  primary:\n    persistence:\n      enabled: false\n  readReplicas:\n    persistence:\n      enabled: false\n' "$pg"
    done
    printf 'registry:\n  zot:\n    storage:\n      persistence: false\n'
  fi
}

# subchart_defaults — умолчания подчартов-частей дерева под их ключом в зонтике.
# Цепочка `dev` объявляет образы консоли НЕ в слоях, а наследует из умолчаний
# подчарта (`ui-future/deploy/values.yaml`); сложение одних слоёв их не видит, и
# консоль осталась бы на `:dev`, которого в реестре нет. Поэтому первым слоем
# идут умолчания каждого локального подчарта — ровно как их складывает helm.
subchart_defaults() {
  python3 - "$UMBRELLA" <<'PY'
import os, sys, yaml
root = sys.argv[1]
chart = yaml.safe_load(open(os.path.join(root, "Chart.yaml")))
out = {}
for dep in chart.get("dependencies") or []:
    repo = dep.get("repository") or ""
    if repo.startswith("file://"):
        path = os.path.normpath(os.path.join(root, repo[len("file://"):]))
    else:
        path = os.path.join(root, "charts", dep["name"])
    vf = os.path.join(path, "values.yaml")
    if not os.path.isfile(vf):
        continue
    out[dep.get("alias") or dep["name"]] = yaml.safe_load(open(vf)) or {}
if not out:
    sys.exit("ни одного локального подчарта не прочитано — умолчания складывать не из чего")
yaml.safe_dump(out, sys.stdout, sort_keys=True)
PY
}

images_overlay() {
  local sha="$1" out="$2"; shift 2
  local names branch tags refs="" img tag
  names="$("$STAND_NS_BIN" images -list "$@")" || return 2
  for branch in $IMAGE_BRANCHES; do
    if tags="$(bash "$HERE/gen-managed-image-pins.sh" --tag-of "$(tr '\n' ',' <<<"$names")" --ref "$sha" --branch "$branch" 2>/dev/null)"; then
      IMAGE_BRANCH="$branch"; break
    fi
    tags=""
  done
  [ -n "$tags" ] || { warn "коммит $sha не входит ни в одну из веток публикации ($IMAGE_BRANCHES) — образов этой ревизии нет"; return 2; }
  while IFS=$'\t' read -r img tag; do
    [ -n "$img" ] || continue
    refs="$refs $img=$IMAGE_PREFIX/$img:$tag"
    reachable "$IMAGE_PREFIX/$img" "$tag" || { warn "образ $IMAGE_PREFIX/$img:$tag в реестре НЕ найден — стенд ушёл бы в ImagePullBackOff"; return 2; }
  done <<<"$tags"
  log "образы ревизии $sha (ветка публикации $IMAGE_BRANCH): $(wc -l <<<"$tags") частей дерева, каждая найдена в реестре"
  "$STAND_NS_BIN" images -refs "$refs" "$@" >"$out"
}

# reachable REPO TAG — манифест образа отвечает. Проверяется только docker.io:
# у прочих реестров вопрос задаёт сам кластер при подъёме, и это печатается.
reachable() {
  local repo="$1" tag="$2" path tok code
  case "$repo" in
    docker.io/*) path="${repo#docker.io/}" ;;
    *) warn "досягаемость $repo:$tag НЕ проверена (не docker.io) — ответит кластер"; return 0 ;;
  esac
  tok="$(curl -fsS --max-time 20 "https://auth.docker.io/token?service=registry.docker.io&scope=repository:$path:pull" | jq -r .token)" || return 1
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 -I -H "Authorization: Bearer $tok" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json' \
    "https://registry-1.docker.io/v2/$path/manifests/$tag")"
  [ "$code" = 200 ]
}

# ref_guard SHA — образы ревизии SHA вправе исполнять чарт рабочей копии, только
# если между ними не менялся ни один вход сборки образов: иначе стенд назывался
# бы стендом этой копии, а исполнял бы другой код.
ref_guard() {
  local sha="$1" head changed
  head="$(git -C "$REPO_ROOT" rev-parse HEAD)"
  [ "$sha" = "$head" ] && return 0
  git -C "$REPO_ROOT" merge-base --is-ancestor "$sha" "$head" || {
    warn "ревизия образов $sha не предок рабочей копии $head — чарт и образы из разных историй"; return 1; }
  changed="$(git -C "$REPO_ROOT" diff --name-only "$sha" "$head" | grep -vE '(^|/)deploy/|^docs/|\.md$|_test\.go$|^ui-future/e2e/|^tools/standns/' || true)"
  if [ -n "$changed" ]; then
    warn "между ревизией образов $sha и рабочей копией $head менялись входы сборки образов:"
    printf '%s\n' "$changed" | sed 's/^/  /' >&2
    warn "образы ревизии этого кода не несут; подними стенд от ревизии, для которой они опубликованы"
    return 1
  fi
  log "чарт рабочей копии $head поверх образов $sha: между ними менялись только развёртывание, доки и пробы"
}

cmd_up() {
  local ns="$1" stack="${2:-dev-prod}" ref="${3:-HEAD}" task state cans persist port sha start work chain
  task="$(ns_valid "$ns")" || die "имя «$ns» не по правилу t<номер задачи>-<коротко> (≤ 40 знаков, DNS-1123). Пространство kacho — рабочий стенд, цель его не трогает" 2
  need_tools; guard_context
  start="$(date +%s)"
  chain="$(bash "$DEPLOY_ROOT/tests/helm/stacks.sh" --args "$stack" "$UMBRELLA")" && [ -n "$chain" ] ||
    die "цепочка «$stack» в deploy/stacks.txt не прочитана — стенд без цепочки сел бы на умолчания чарта" 2
  sha="$(git -C "$REPO_ROOT" rev-parse --verify "${ref}^{commit}" 2>/dev/null)" || die "ревизия «$ref» не разрешается в коммит" 2
  ref_guard "$sha" || die "ревизия образов и рабочая копия расходятся (выше)" 2
  build_tool || die "инструмент переноса не собран" 2

  state="$(ns_state "$ns")" || die "состояние пространства $ns НЕ ПРОЧИТАНО — кластер не ответил" 2
  [ "$state" != foreign ] || die "пространство $ns существует и НЕ помечено $LABEL_STAND=test — оно чужое, стенд в него не ставится" 1
  cans="$(ca_namespace)" || die "контроллер cert-manager на кластере не прочитан" 2
  [ "$(wc -w <<<"$cans")" = 1 ] || die "пространство ресурсов cert-manager не установлено однозначно («$cans»): стенд cert-manager не ставит, он переиспользует ровно один существующий" 2
  if has_default_storage_class; then persist=on; else persist=off; fi
  port="$(ns_port "$ns")"
  log "кластер: cert-manager переиспользуется (ресурсы в ns $cans); класс томов по умолчанию: $persist; контроллер входа не ставится"

  local expires; expires="$(date -u -d "+${TTL_HOURS} hours" +%Y-%m-%dT%H:%M:%SZ)"
  kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f - >/dev/null || die "пространство $ns не заведено" 1
  kubectl label namespace "$ns" --overwrite >/dev/null \
    "$LABEL_STAND=test" "$LABEL_TASK=$task" \
    pod-security.kubernetes.io/warn=restricted pod-security.kubernetes.io/warn-version=latest \
    pod-security.kubernetes.io/audit=restricted pod-security.kubernetes.io/audit-version=latest || die "метки $ns не поставлены" 1
  kubectl annotate namespace "$ns" --overwrite >/dev/null \
    "$ANN_EXPIRES=$expires" "kacho.io/ref=$sha" "kacho.io/stack=$stack" || die "аннотации $ns не поставлены" 1
  log "пространство $ns: задача #$task, срок $expires"
  quota_manifest "$ns" | kubectl apply -f - >/dev/null || die "квота и пределы $ns не применены" 1

  work="$WORK_ROOT/$ns"; mkdir -p "$work"
  stand_overlay "$ns" "$cans" "$persist" "$port" >"$work/stand.yaml"
  subchart_defaults >"$work/subchart-defaults.yaml" || die "умолчания подчартов не прочитаны" 2
  local layers=("$work/subchart-defaults.yaml" "$UMBRELLA/values.yaml") f
  for f in $(bash "$DEPLOY_ROOT/tests/helm/stacks.sh" --chain "$stack" ' '); do layers+=("$UMBRELLA/$f"); done
  images_overlay "$sha" "$work/images.yaml" "${layers[@]}" || die "накладка образов ревизии $sha не выведена" 2

  bash "$HERE/helm-umbrella-deps.sh" >/dev/null || die "зависимости зонтичного чарта не материализованы" 2
  make -C "$DEPLOY_ROOT" --no-print-directory module-manifests-configmap \
    MODULE_MANIFESTS_STACK="$stack" STACK_NAMESPACE="$ns" EXPECT_CONTEXT="$STAND_CTX" || die "манифесты модулей не доставлены" 1
  local mm="$UMBRELLA/values.module-manifests.yaml"
  local extra=(-f "$work/images.yaml" -f "$work/stand.yaml" -f "$mm" --set cert-manager.enabled=false)
  # shellcheck disable=SC2206 # цепочка — слова `-f <файл>`, разбор по словам и нужен
  local args=($chain "${extra[@]}")
  STACK_NAMESPACE="$ns" STACK_RELEASE="$RELEASE" bash "$HERE/stack-secrets.sh" "$stack" "${extra[@]}" ||
    die "предусловные секреты стенда не созданы" 1

  # Предрендер тем же входом и тем же переносом: отказ переноса и заявка на том
  # без класса видны ДО применения, а не через предел ожидания helm.
  HELM_PLUGINS="$DEPLOY_ROOT/helm/plugins" KACHO_STAND_NS_BIN="$STAND_NS_BIN" \
    helm template "$RELEASE" "$UMBRELLA" -n "$ns" "${args[@]}" \
      --post-renderer kacho-stand-ns \
      --post-renderer-args "-namespace=$ns" --post-renderer-args "-ca-namespace=$cans" >"$work/render.yaml" ||
    die "предрендер стенда отказал (текст выше)" 2
  if [ "$persist" = off ]; then
    local claims
    claims="$(python3 -c '
import sys, yaml
for d in yaml.safe_load_all(open(sys.argv[1])):
    if not d: continue
    if d.get("kind") == "PersistentVolumeClaim" or (d.get("spec") or {}).get("volumeClaimTemplates"):
        print("  %s/%s" % (d["kind"], d["metadata"]["name"]))' "$work/render.yaml")"
    [ -z "$claims" ] || die "у кластера нет класса томов по умолчанию, а рендер заявляет тома — их заявки висели бы в Pending:
$claims" 2
  fi

  log "helm: цепочка $stack + образы $sha + стенд, перенос в $ns"
  HELM_PLUGINS="$DEPLOY_ROOT/helm/plugins" KACHO_STAND_NS_BIN="$STAND_NS_BIN" \
    helm upgrade --install "$RELEASE" "$UMBRELLA" -n "$ns" "${args[@]}" \
      --post-renderer kacho-stand-ns \
      --post-renderer-args "-namespace=$ns" --post-renderer-args "-ca-namespace=$cans" \
      --wait --timeout "$UP_TIMEOUT" 2>&1 | tee "$work/helm.log"
  [ "${PIPESTATUS[0]}" -eq 0 ] || die "helm upgrade --install отказал (журнал $work/helm.log)" 1

  # Прокси API-сервера на управляемом кластере до подов не доходит — вопрос
  # задаётся пробросом (scripts/wait-edge-ready.sh, EDGE_READY_VIA).
  EDGE_READY_VIA=port-forward bash "$HERE/wait-edge-ready.sh" "$ns" api-gateway 90 2 3 || die "край стенда не ответил готовностью" 1
  KACHO_NS="$ns" bash "$HERE/seed-geo-baseline.sh" || die "посев каталога geo не прошёл" 1
  KACHO_NS="$ns" POSTURE_SKIP="${POSTURE_SKIP:-}" bash "$HERE/seed-storage-catalog.sh" || die "посев каталога хранения не прошёл" 1
  KACHO_NS="$ns" POSTURE_SKIP="${POSTURE_SKIP:-}" bash "$HERE/seed-vpc-address-pools.sh" || die "посев полосы адресов не прошёл" 1
  NS="$ns" POSTURE_SKIP="${POSTURE_SKIP:-}" POSTURE_PROFILE=production bash "$HERE/assert-production-posture.sh" 2>&1 | tee "$work/posture.log"
  [ "${PIPESTATUS[0]}" -eq 0 ] || die "боевая посадка стенда не доказана (журнал $work/posture.log)" 1
  bash "$HERE/stand-provenance.sh" --namespace "$ns" --expect "$sha" 2>&1 | tee "$work/provenance.log"
  local prc="${PIPESTATUS[0]}"
  [ "$prc" -eq 0 ] || die "провенанс стенда не сходится с ревизией $sha (код $prc, журнал $work/provenance.log)" 1

  log "стенд $ns ПОДНЯТ за $(( $(date +%s) - start )) с: цепочка $stack, образы $sha, срок $expires"
  log "консоль https://$(ns_host "$ns"):$port · край https://127.0.0.1:$((port + 1)) · приёмник http://127.0.0.1:$((port + 2))"
  log "проброс и окружение проб:  bash deploy/scripts/stand-ns.sh forward $ns"
}

# ── ПРОБРОС ──────────────────────────────────────────────────────────────────

cmd_forward() {
  local ns="$1" port work host lp
  ns_valid "$ns" >/dev/null || die "имя «$ns» не по правилу" 2
  [ "$(ns_state "$ns")" = test ] || die "тестового пространства $ns нет" 1
  port="$(ns_port "$ns")"; host="$(ns_host "$ns")"; work="$WORK_ROOT/$ns"; mkdir -p "$work"
  cmd_unforward "$ns" >/dev/null 2>&1
  kubectl -n "$ns" get secret console-public-tls -o 'jsonpath={.data.ca\.crt}' | base64 -d >"$work/console-ca.pem"
  grep -q 'BEGIN CERTIFICATE' "$work/console-ca.pem" || die "лист консоли (console-public-tls) не выпущен — проброс бессмыслен" 1
  # Браузер проб доверяет ЛИСТУ (отпечаток SPKI, ui-future/e2e/stand-tls-trust.ts),
  # node — корню: у стенда это разные сертификаты, лист выпущен своим CA стенда.
  kubectl -n "$ns" get secret console-public-tls -o 'jsonpath={.data.tls\.crt}' | base64 -d |
    awk '/BEGIN CERTIFICATE/{n++} n==1' >"$work/console-leaf.pem"
  grep -q 'END CERTIFICATE' "$work/console-leaf.pem" || die "лист консоли не прочитан из console-public-tls" 1
  local pair svc rport
  for pair in "ui-public:443:$port" "api-gateway:8443:$((port + 1))" "$RELEASE-mailpit:8025:$((port + 2))"; do
    IFS=: read -r svc rport lp <<<"$pair"
    nohup kubectl -n "$ns" port-forward "svc/$svc" "$lp:$rport" >"$work/forward-$svc.log" 2>&1 &
    echo $! >>"$work/forward.pids"
  done
  # Спрашивается оболочка консоли (`/`), а не `/healthz`: на порту внешнего входа
  # точка здоровья намеренно отвечает 404 (ui-future/deploy, configmap-nginx.yaml) —
  # её читают пробы кубелета на внутреннем порту, не пользователь.
  local code
  for _ in $(seq 1 30); do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 --cacert "$work/console-ca.pem" \
      --resolve "$host:$port:127.0.0.1" "https://$host:$port/" || true)"
    [ "$code" = 200 ] && break
    sleep 2
  done
  [ "$code" = 200 ] || die "консоль через проброс не ответила (последний код $code, журналы $work/forward-*.log)" 1
  cat >"$work/probe.env" <<EOF
export KACHO_CONSOLE_URL=https://$host:$port
export KACHO_CONSOLE_HOST_IP=127.0.0.1
export KACHO_CONSOLE_CA=$work/console-leaf.pem
export NODE_EXTRA_CA_CERTS=$work/console-ca.pem
export KACHO_CONSOLE_MAILBOX_URL=http://127.0.0.1:$((port + 2))
export KACHO_EDGE_URL=https://127.0.0.1:$((port + 1))
EOF
  log "проброс поднят; окружение проб: . $work/probe.env"
}

cmd_unforward() {
  local ns="$1" work="$WORK_ROOT/$1" pid
  [ -f "$work/forward.pids" ] || return 0
  while read -r pid; do kill "$pid" 2>/dev/null || true; done <"$work/forward.pids"
  rm -f "$work/forward.pids"
  log "проброс $ns снят"
}

# ── СНЯТИЕ ───────────────────────────────────────────────────────────────────

# leftovers NS — помеченные объекты стенда ВНЕ его пространства (издатели CA,
# корень CA и его секрет в пространстве ресурсов cert-manager).
leftovers() {
  local cans
  kubectl get clusterissuer -l "$LABEL_STAND_NS=$1" -o name --request-timeout=30s 2>/dev/null
  for cans in $(ca_namespace 2>/dev/null); do
    kubectl -n "$cans" get certificate,secret -l "$LABEL_STAND_NS=$1" -o name --request-timeout=30s 2>/dev/null | sed "s|^|-n $cans |"
  done
}

cmd_down() {
  local ns="$1" state left
  ns_valid "$ns" >/dev/null || die "имя «$ns» не по правилу t<номер>-<коротко>; пространство kacho цель не снимает никогда" 2
  need_tools; guard_context
  cmd_unforward "$ns" >/dev/null 2>&1
  state="$(ns_state "$ns")" || die "состояние пространства $ns НЕ ПРОЧИТАНО — кластер не ответил; «не прочитал» не равно «нет»" 2
  [ "$state" != foreign ] || die "пространство $ns НЕ помечено $LABEL_STAND=test — оно не стенд проб, и снимать его эта цель не вправе" 1
  if [ "$state" = test ] && helm status "$RELEASE" -n "$ns" >/dev/null 2>&1; then
    log "helm uninstall $RELEASE -n $ns (уносит свои издатели CA и корень CA)"
    helm uninstall "$RELEASE" -n "$ns" --wait --timeout 5m || warn "helm uninstall отказал — помеченное снимается ниже по метке"
  fi
  left="$(leftovers "$ns")"
  if [ -n "$left" ]; then
    log "помеченное вне пространства: $(wc -l <<<"$left") объект(ов) — снимаю"
    while read -r line; do
      [ -n "$line" ] || continue
      # shellcheck disable=SC2086 # строка — «[-n ns] вид/имя», разбор по словам и нужен
      kubectl delete $line --ignore-not-found --wait=true --timeout=120s >/dev/null || warn "не снят: $line"
    done <<<"$left"
  fi
  if [ "$state" = test ]; then
    log "kubectl delete namespace $ns (тома и всё прочее — вместе с ним)"
    kubectl delete namespace "$ns" --wait=true --timeout=10m >/dev/null || warn "удаление $ns не завершилось за 10 мин"
  fi
  for _ in $(seq 1 60); do
    state="$(ns_state "$ns")" || state=unknown
    [ "$state" = absent ] && break
    sleep 5
  done
  [ "$state" = absent ] || die "пространство $ns не исчезло (состояние: $state)" 1
  left="$(leftovers "$ns")"
  [ -z "$left" ] || die "после снятия осталось помеченное вне пространства:
$left" 1
  rm -rf "${WORK_ROOT:?}/$ns"
  log "стенд $ns снят: пространства нет, помеченного вне него 0"
}

# ── ПЕРЕПИСЬ ─────────────────────────────────────────────────────────────────

cmd_census() {
  local expired_mode=0 js now rows n=0 nexp=0 name task exp age state
  [ "${1:-}" = --expired ] && expired_mode=1
  need_tools
  [ "$expired_mode" = 0 ] || guard_context
  js="$(kubectl get namespace -l "$LABEL_STAND=test" -o json --request-timeout=30s)" || die "перечень пространств НЕ ПРОЧИТАН — кластер не ответил" 2
  now="$(date -u +%s)"
  rows="$(jq -r --arg t "$LABEL_TASK" --arg e "$ANN_EXPIRES" \
    '.items[] | [.metadata.name, (.metadata.labels[$t] // "-"), (.metadata.annotations[$e] // "-"), .metadata.creationTimestamp] | @tsv' <<<"$js")"
  printf '%-32s %-7s %-8s %-22s %s\n' ПРОСТРАНСТВО ЗАДАЧА ВОЗРАСТ СРОК СОСТОЯНИЕ
  local doomed=""
  while IFS=$'\t' read -r name task exp created; do
    [ -n "$name" ] || continue
    n=$((n + 1))
    age="$(( (now - $(date -u -d "$created" +%s)) / 60 ))м"
    expired_at "$exp" "$now"
    case $? in
      0) state="ИСТЁК"; nexp=$((nexp + 1)); doomed="$doomed $name" ;;
      1) state="жив" ;;
      *) state="СРОК НЕ ЧИТАЕТСЯ — не снимается автоматически" ;;
    esac
    printf '%-32s %-7s %-8s %-22s %s\n' "$name" "$task" "$age" "$exp" "$state"
  done <<<"$rows"
  local orphans
  orphans="$(kubectl get clusterissuer -o json --request-timeout=30s | jq -r --arg l "$LABEL_STAND_NS" \
    '.items[] | select(.metadata.labels[$l] != null) | "\(.metadata.labels[$l]) clusterissuer/\(.metadata.name)"')"
  local o oc=0
  while read -r o; do
    [ -n "$o" ] || continue
    [ "$(ns_state "${o%% *}" 2>/dev/null)" = absent ] || continue
    oc=$((oc + 1)); warn "сирота: ${o#* } (стенд ${o%% *} снят, объект остался)"
  done <<<"$orphans"
  echo "тестовых пространств $n, истёкших $nexp, сирот вне пространств $oc"
  if [ "$expired_mode" = 1 ]; then
    for name in $doomed; do ( cmd_down "$name" ) || warn "истёкший $name не снят"; done
  fi
}

case "${1:-}" in
  up)        shift; [ -n "${1:-}" ] || die "up: не названо пространство (NS=t<задача>-<коротко>)" 2; cmd_up "$@" ;;
  down)      shift; [ -n "${1:-}" ] || die "down: не названо пространство" 2; cmd_down "$1" ;;
  census)    shift; cmd_census "$@" ;;
  forward)   shift; [ -n "${1:-}" ] || die "forward: не названо пространство" 2; cmd_forward "$1" ;;
  unforward) shift; [ -n "${1:-}" ] || die "unforward: не названо пространство" 2; cmd_unforward "$1" ;;
  self-test) self_test ;;
  *) die "режим не назван: up | down | census | forward | unforward | self-test" 2 ;;
esac
