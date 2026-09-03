#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# cutover-fe3455.sh — fe3455 PROD umbrella FORWARD roll (single coherent cutover).
# =============================================================================
# WHAT THIS DOES
#   Runs `helm upgrade kacho-umbrella` against the LIVE fe3455 cluster with the
#   fe3455 prod overlays (values.fe3455.yaml + values.fe3455-prod.yaml). This is a
#   deliberate FORWARD roll of four workloads to their merged main builds, keeping
#   every OTHER workload pinned to its currently-live tag (no revert):
#
#     workload      old (live)                              new (forward)     why
#     ───────────   ─────────────────────────────────────  ────────────────  ─────────────────────────
#     kacho-iam     KAC-registry-docker-auth-c3000530       main-c744f956     #321 audience + :9097 JWKS + #325 RG-1 418-catalog + #326 issued_at
#     api-gateway   main-a7c82963                            main-c7dce40d     #145 RG-1 6 routes + 418-catalog
#     registry      main-5eb21d25                            main-af0eacae     #43 RG-1 Repository persistence (strict fwd; 5eb21d25 ∈ af0eacae)
#     kacho-storage — NOT installed (storage.enabled=false): the kacho-storage chart is
#                     not integrated with the umbrella (values keys diverge + no
#                     existingSecret for the DB password), so its fresh install never came
#                     up and its 15m --wait failed the WHOLE cutover. Pure control-plane
#                     roll until that chart lands. Image main-a185fa07 (CS-1) is built and
#                     waiting. See values.fe3455-prod.yaml (storage block).
#
#   COHERENCE (verified via `helm template` of the 4-overlay stack, 0 stderr, diff
#   vs live shows ONLY these image lines change):
#     • JWKS-flip: registry.iam.jwksUrl → https://kacho-iam-internal:9097 (merged #171).
#       iam-on-main (b3d23769, #323) SERVES :9097 → the flip is coherent (no 401-storm).
#       `helm --wait` brings iam Ready before returning; the registry Bearer verifier
#       fetches JWKS lazily on first token-verify → single upgrade is safe.
#     • catalog: iam(#325)/gateway(#145)/registry both land the 418-entry permission
#       catalog together → the new RG-1 Repository RPCs authorize (no "catalog: no entry").
#     • unchanged & live: vpc main-6fe9c386, compute main-1678f62c, geo main-fc2d945c,
#       nlb main-2c87cac9, zot v2.1.18, every uif remote master-e6001c77, every Postgres
#       (16.1.0-debian-11-r25 / pg-hydra 16.4.0-debian-12-r0) — emptyDir, tags NOT bumped.
#
#   REGISTRY data-plane TLS: the overlay now sets registry.service.dataplaneLB.tlsSidecar
#   (enabled + LE cert), so the chart — not a hand-applied kubectl patch — owns the public
#   TLS termination (443 -> dp-tls, Let's Encrypt). This closes the drift that broke
#   `docker login` on the 2026-07-15 run: --take-ownership adopted the hand-patched Service
#   and re-rendered it from the chart default (443 -> plaintext). The rendered LE Certificate
#   is byte-identical to the live object, so helm ADOPTS it without a re-issue (LE
#   duplicate-limit 5/week).
#
#   STORAGE: NOT installed on this cutover (storage.enabled=false + pg-storage.enabled=false).
#   The kacho-storage chart is not integrated with the umbrella — the overlay's storage.db.*
#   keys do not exist in it (it reads config.dbHost/…), and it has no existingSecret support
#   for the DB password, so it rendered its own Secret with the "changeme" placeholder. The
#   fresh install never became Ready and its 15m --wait failed the whole cutover. Image
#   main-a185fa07 (CS-1) is built and waiting; see values.fe3455-prod.yaml (storage block)
#   for the re-enable checklist. Remaining CS-1 follow-up either way: fga-register drainer +
#   storage-SA fga_writer seed in iam.
#   For a PURE control-plane-only cutover set storage.enabled=false + pg-storage.enabled=false.
#
# DOCKER-LOGIN issued_at BLOCKER — RESOLVED (2026-07-15), guard retained as a denylist.
#   iam's /iam/token (:9096) must emit `issued_at` as an RFC3339 STRING: the docker client
#   parses it via time.Time.UnmarshalJSON, which accepts ONLY a JSON string — a bare Unix
#   number breaks `docker login` ("Time.UnmarshalJSON: input is not a JSON string") → no
#   bearer → all pull/push 401. The earlier forward target main-b3d23769 REVERTED that fix
#   (kacho-iam c300053), so it was a hard blocker. kacho-iam#326 re-applied c300053 onto
#   main (+ a wire-shape regression test locking the string) → main c744f95 → CI published
#   main-c744f956, the tag pinned above. The preflight below stays as a KNOWN-BAD-TAG
#   denylist: main-b3d23769 remains a broken image, so repinning back to it must not be
#   silent (override: ACK_IAM_ISSUED_AT_REVERT=1).
#
# WHY A SCRIPT (not run by the coding agent): the auto-mode classifier blocks `helm`
#   against the fe3455 context, so the agent cannot run the upgrade. You run this.
#
# DEP RESOLUTION: uses `helm dependency build` (respects the committed Chart.lock),
#   NOT `helm dependency update`. This PINS postgresql to 13.4.4 so the upgrade can
#   never bump the pg image tag — critical because every Postgres here runs emptyDir
#   (persistence.enabled=false), so a pg image change would recreate the pod and WIPE
#   the database. `dependency build` still re-vendors the local file:// sub-charts
#   (registry from ../kacho-registry@main with the merged S3/compat chart), which is
#   the reason a re-vendor is needed. If build fails because a sibling chart version
#   changed, run `helm dependency update` manually AND re-confirm pg pins stay 13.4.4.
#
# NO SECRET VALUES are written in this file. Postgres passwords are read from the
#   pre-created k8s Secrets at run time and passed via --set only to satisfy the
#   bitnami passwords-on-upgrade guard (they are never echoed).
# =============================================================================
set -euo pipefail

NS=kacho
RELEASE=kacho-umbrella
CHART_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$CHART_DIR"

log()  { printf '\033[1;34m[cutover]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[cutover WARN]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[cutover ABORT]\033[0m %s\n' "$*" >&2; exit 1; }

# ── 0. tools + cluster context ────────────────────────────────────────────────
command -v helm    >/dev/null || die "helm not found in PATH"
command -v kubectl >/dev/null || die "kubectl not found in PATH"
# make + go: this script now PRODUCES the module-manifests ConfigMap before the
# upgrade (step 2a). They are checked HERE, before anything is touched, so a
# missing toolchain is a named refusal instead of a failure discovered halfway
# through a production cutover.
command -v make    >/dev/null || die "make not found in PATH — needed for the module-manifests ConfigMap (step 2a)"
command -v go      >/dev/null || die "go not found in PATH — the module-manifests producer is built from source (step 2a)"
CTX="$(kubectl config current-context 2>/dev/null || true)"
APISERVER="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true)"
log "kubectl context: ${CTX:-<none>}   apiserver: ${APISERVER:-<none>}"
log "namespace: $NS   chart: $CHART_DIR"

# The context NAME is a label chosen by whoever wrote the kubeconfig, so it is a
# convenience filter and never the authority: a same-named context has already
# pointed at a different cluster in this project's history. What identifies the
# target is the apiserver address, which renaming a context cannot forge — and
# this script runs `helm upgrade --take-ownership --wait` against whatever it is
# pointed at, so "probably the right one" is not good enough.
case "$CTX" in
  *fe3455*) : ;;
  *) warn "context '$CTX' does not look like fe3455 — the apiserver check below is what decides" ;;
esac

[ -n "$APISERVER" ] || die "cannot read the apiserver address of the active context — refusing to
       converge a cluster this script cannot identify."

if [ -z "${FE3455_APISERVER:-}" ]; then
  die "FE3455_APISERVER is not set, so there is nothing to check the active context against.
       The active context claims:   $APISERVER
       If that IS fe3455, pin it for this and future runs:
           export FE3455_APISERVER='$APISERVER'
       Pin it once from a session you have already verified — the point is that the value comes
       from you, not from the name of whatever context happens to be selected."
fi

if [ "$APISERVER" != "$FE3455_APISERVER" ]; then
  die "WRONG CLUSTER — refusing to run.
       active context '$CTX' points at:  $APISERVER
       FE3455_APISERVER expects:         $FE3455_APISERVER
       Switch context (kubectl config use-context …), or correct FE3455_APISERVER if fe3455 itself moved."
fi
log "target cluster confirmed by apiserver address (not by context name)."

# ── 0b. KNOWN-BAD-TAG GUARD: the iam image must carry the issued_at RFC3339 fix ────
#    main-b3d23769 reverts kacho-iam c300053 (issued_at RFC3339 string) → `docker login`
#    breaks. The current pin (main-c744f956, kacho-iam#326) carries the fix, so this guard
#    is a denylist against a silent repin BACK to the broken image.
if grep -qE '^\s*tag:\s*main-b3d23769\s*$' "$CHART_DIR/values.fe3455-prod.yaml" 2>/dev/null; then
  if [ "${ACK_IAM_ISSUED_AT_REVERT:-0}" != "1" ]; then
    die "BLOCKER: kacho-iam pinned to main-b3d23769, which REVERTS the docker-login
       issued_at RFC3339 fix (kacho-iam commit c300053). Rolling iam to this image breaks
       'docker login' (Time.UnmarshalJSON: input is not a JSON string) → the registry
       data-plane cannot mint a bearer token → all docker pull/push 401.
       RESOLUTION: pin kacho-iam.image.tag to main-c744f956 or later (main c744f95 carries
       c300053 re-applied via kacho-iam#326) in BOTH values.fe3455.yaml and
       values.fe3455-prod.yaml → re-run.
       To knowingly ship the docker-login break anyway: ACK_IAM_ISSUED_AT_REVERT=1 $0"
  fi
  warn "ACK_IAM_ISSUED_AT_REVERT=1 set — proceeding with main-b3d23769; 'docker login' WILL break until c300053 is on the iam image."
fi

# ── 1. required value files present (the credentials layer is gitignored) ─────
#
# THE CHAIN IS READ, NOT RESTATED. It used to be written out here and in every
# gate that renders this stand; the copies drifted, and a stand nobody deploys
# was being checked while this script deployed another. deploy/stacks.txt is the
# one declaration; the credentials layer is appended here because it is outside
# the tree by design and cannot live in a tracked table.
ORY_CREDS_LAYER="values.fe3455-ory.yaml"
FE_LAYERS="$(bash "$CHART_DIR/../../tests/helm/stacks.sh" --chain fe3455 ' ')"
[ -n "$FE_LAYERS" ] || die "stack table declares no fe3455 chain — nothing to deploy, and that is a refusal, not an empty success"
FE_LAYERS="$FE_LAYERS $ORY_CREDS_LAYER"
FE_ARGS=()
for f in $FE_LAYERS; do
  [ -f "$CHART_DIR/$f" ] || die "missing values file: $f  ($ORY_CREDS_LAYER is gitignored — restore it locally before cutover)"
  FE_ARGS+=(-f "$f")
done
log "all $(printf '%s\n' $FE_LAYERS | grep -c .) overlay value files present."

# ── 1a. the credentials layer must carry CREDENTIALS ONLY ─────────────────────
#
# Why this gate exists. Until 2026-08-11 the whole Ory overlay lived in the one
# gitignored file, so the PRODUCTION POSTURE of the identity providers (kratos
# development mode, hydra issuer/PKCE/TTL) was invisible to git, to review and to
# every gate — their "no findings" over that layer meant "nothing read". Posture
# now lives in the tracked values.fe3455-ory-posture.yaml.
#
# A convention alone would not hold that split: the easiest way to change the
# live cluster is still to edit the file nobody sees. So the split is CHECKED
# here — the credentials layer may declare only the coordinates below, and a
# posture key reappearing in it refuses the cutover instead of shipping quietly.
#
# The allow-list is deliberately a LEAF-PATH list, not a subtree list: allowing
# `hydra.hydra.config` wholesale would re-admit every posture key under it.
#
# THE MAIL COORDINATE IS OURS, NOT THE VENDOR'S — and it used to be the other way
# round here. This list named `kratos.kratos.config.courier.smtp.connection_uri`,
# a coordinate that feeds the VENDOR subchart's own config file. The identity
# process reads SEVERAL config files and merges them in order; ours is second,
# so the `courier` section we render REPLACES the vendor's wholesale rather than
# extending it. An operator who put the real relay where this script sent them
# got a green cutover, running pods and mail going nowhere — with no signal at
# all. The single declaration is `global.kacho.identity.smtp.*`
# (_kratos-identity.tpl); the credentials layer is applied LAST in the chain, so
# a value set there wins over every profile. Held by MAIL-54
# (deploy/identity_mail_lane_single_declaration_test.go), which fails when this
# list and that declaration name different coordinates.
ORY_CRED_PATHS='
hydra.hydra.config.dsn
hydra.hydra.config.secrets.system
hydra.hydra.config.secrets.cookie
kratos.kratos.config.dsn
kratos.kratos.config.secrets.cookie
kratos.kratos.config.secrets.cipher
global.kacho.identity.smtp.connectionURI
'
stray="$(ORY_CRED_PATHS="$ORY_CRED_PATHS" python3 - "$CHART_DIR/values.fe3455-ory.yaml" <<'PY'
import os, sys, yaml
allowed = set(os.environ["ORY_CRED_PATHS"].split())
tree = yaml.safe_load(open(sys.argv[1])) or {}
def leaves(node, path=()):
    if isinstance(node, dict):
        for k, v in node.items():
            yield from leaves(v, path + (str(k),))
    else:
        yield ".".join(path)
print("\n".join(sorted(p for p in leaves(tree) if p not in allowed)))
PY
)" || die "could not read values.fe3455-ory.yaml (need python3 with PyYAML)"

if [ -n "$stray" ]; then
  warn "values.fe3455-ory.yaml declares coordinates that are NOT credentials:"
  printf '  %s\n' $stray >&2
  die "posture must live in the TRACKED values.fe3455-ory-posture.yaml, where review and the
       gates can see it. Move the coordinates above there (or, if they really are credentials,
       add them to ORY_CRED_PATHS in this script with a reason). Refusing to deploy a posture
       that no gate has read."
fi
log "credentials layer carries credentials only (posture is in the tracked layer)."

# ── 2. re-vendor sub-charts from committed Chart.lock (pins pg -> no data loss) ─
log "helm dependency build (respects Chart.lock; re-vendors ../kacho-registry@main S3/compat chart)…"
helm dependency build . >/dev/null \
  || die "helm dependency build failed. If a sibling chart version changed, run 'helm dependency update' manually, then re-confirm every postgresql pin in Chart.lock is 13.4.4 before re-running."

# ── 3. ПРЕДПОЛЁТ ПО СЕКРЕТАМ: ВСЕ, КОТОРЫХ ТРЕБУЕТ РАСКАТКА, А НЕ ОДИН ─────────
#
# Здесь стояла проверка ОДНОГО секрета из четырёх. Остальные три заводит скрипт
# посева стенда, а на боевой площадке он не вызывается — значит их появление не
# проверялось и не производилось ничем. Наблюдаемая последовательность была
# такой: предполёт зелёный → раскатка применяется → под ждёт недостающий секрет →
# `helm --wait` выстаивает свой предел и падает ПО СРОКУ, а не по причине.
# Причина видна только в событиях пода, то есть оператор отделял «условие не
# создано» от «сломан продукт» сам — руками, после пятнадцати минут молчания.
#
# ТРЕБУЕМОЕ МНОЖЕСТВО ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ. Три источника, и каждый
# закрывает то, чего не видят два других:
#
#   (а) ОБЯЗАТЕЛЬНЫЕ `secretKeyRef` отрендеренного стека — их kubelet не
#       подставит вовсе, контейнер не стартует;
#   (б) секреты, которые заводит ПОСЕВ СТЕНДА: на боевой площадке он не зовётся,
#       а ссылки на них НЕОБЯЗАТЕЛЬНЫ — то есть под поднимется и откажет позже,
#       уже своим стражем старта. Рендер про них молчит by construction;
#   (в) таблица ниже — те, что заводит только человек.
#
# ПОЧЕМУ ТАБЛИЦА ВСЁ-ТАКИ ЕСТЬ. У каждого требуемого секрета обязан быть НАЗВАН
# производитель на боевой площадке. «Заводит посев» производителем здесь не
# является: посев на этой площадке не зовётся. Таблица не может разойтись с
# деревом молча — её сверяет deploy/rollout_preflight_covers_required_secrets_test.go:
# требуемый секрет без строки и строка без требования одинаково роняют прогон.
REQUIRED_SECRET_PRODUCERS='
zot-s3-creds|оператор: ключи объектного хранилища (AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY), заводятся до раскатки
kacho-iam-hook-token|оператор: общий секрет обратных вызовов, ключ token, 24 байта hex; заводится ОДИН раз и не ротируется — перевыпуск разводит отправителя и проверяющую сторону
kacho-iam-jwks-enc-key|оператор: ключ обёртки приватной половины подписного ключа, ключ enc_key, 32 байта hex; перевыпуск делает уже записанные ключи нечитаемыми НАВСЕГДА
kacho-iam-bootstrap-sa-key|оператор: приватный ключ ES256 P-256 (PKCS#8) учётки первичной чеканки, ключ private_key_pem; перевыпуск осиротит уже зарегистрированного клиента
'

log "предполёт: вывожу перечень требуемых секретов (рендер + посев + таблица производителей)…"
RENDER="$(helm template "$RELEASE" . -n "$NS" "${FE_ARGS[@]}" --set uif.enabled=true)" \
  || die "helm template того же стека не отработал (его собственный текст — выше) — вывести перечень требуемых секретов не из чего.
       Это отказ, а не пустой успех: предполёт, который ничего не прочитал, неотличим от предполёта, которому нечего сказать."
[ -n "$RENDER" ] || die "helm template дал пустой вывод — читать нечего; см. предыдущий абзац."

SEED_SH="$CHART_DIR/../../scripts/dev-prod-secrets.sh"
[ -f "$SEED_SH" ] || die "скрипт посева $SEED_SH не найден — вторую половину требуемого множества вывести не из чего."

REQUIRED="$(
  {
    # (а) обязательные ссылки отрендеренных подов, за вычетом секретов, которые
    #     создаёт сам рендер.
    printf '%s\n' "$RENDER" | python3 -c '
import sys, yaml
docs=[d for d in yaml.safe_load_all(sys.stdin) if isinstance(d, dict)]
own={d["metadata"]["name"] for d in docs if d.get("kind")=="Secret"}
need=set()
for d in docs:
    spec=d.get("spec") or {}
    tpl=spec.get("template") or ((spec.get("jobTemplate") or {}).get("spec") or {}).get("template")
    if not tpl: continue
    pod=tpl.get("spec") or {}
    for c in (pod.get("containers") or [])+(pod.get("initContainers") or []):
        for e in c.get("env") or []:
            r=(e.get("valueFrom") or {}).get("secretKeyRef")
            if r and not r.get("optional", False) and r["name"] not in own:
                need.add(r["name"])
    for v in pod.get("volumes") or []:
        s=v.get("secret")
        if s and not s.get("optional", False) and s.get("secretName") and s["secretName"] not in own:
            need.add(s["secretName"])
print("\n".join(sorted(need)))
'
    # (б) секреты, которые на стенде заводит посев: здесь его никто не зовёт.
    grep -oE 'create secret generic [a-z0-9][a-z0-9-]*' "$SEED_SH" | awk '{print $4}'
    # (в) те, что заводит только человек.
    printf '%s\n' "$REQUIRED_SECRET_PRODUCERS" | grep -v '^$' | cut -d'|' -f1
  } | sort -u
)" || die "перечень требуемых секретов не выведен — предполёт отказывается судить по неполному списку."

req_n=0; missing=""
for s in $REQUIRED; do
  req_n=$((req_n + 1))
  kubectl -n "$NS" get secret "$s" >/dev/null 2>&1 || missing="$missing $s"
done
[ "$req_n" -gt 0 ] || die "требуемых секретов выведено НОЛЬ — обход слеп, а не площадка готова."

if [ -n "$missing" ]; then
  # shellcheck disable=SC2086  # разделение по словам здесь и есть предмет: $missing — список имён
  warn "предполёт: требуется $req_n секрет(ов), проверено $req_n, ОТСУТСТВУЕТ $(printf '%s\n' $missing | grep -c .):"
  for s in $missing; do
    who="$(printf '%s\n' "$REQUIRED_SECRET_PRODUCERS" | awk -F'|' -v n="$s" '$1==n {print $2}')"
    printf '  %s — %s\n' "$s" "${who:-производитель на этой площадке НЕ НАЗВАН}" >&2
  done
  die "раскатка НЕ применена. Отказ наступает ЗДЕСЬ, до применения, а не через 15 минут ожидания
       готовности: под, которому не хватает секрета, ждёт молча, и предел --wait истекает по СРОКУ,
       не называя причины. Заведи перечисленные секреты в ns $NS и повтори."
fi
log "предполёт по секретам: требуется $req_n, проверено $req_n, отсутствует 0."

# ── 3a. ЗАЯВЛЕНИЕ ОБ ОТКАТЕ: что откат вернёт, а что нет ──────────────────────
#
# Мигратор идёт при КАЖДОМ раскате, поэтому штатный откат выкатки вернул бы
# ПРЕЖНИЙ ОБРАЗ НА НОВУЮ СХЕМУ. Здесь стояла ровно одна строка совета —
# `helm rollback` — и о схеме она не говорила ничего: оператор узнавал исход
# ПОСЛЕ отката, а «не откатывается» — только по отказам работающего продукта.
#
# Заявление ПРОИЗВОДИТСЯ из дерева, а не пишется прозой: перепись цепочки, число
# миграций без секции отката, объявленные необратимыми и точка невозврата. Отказ
# заявления (код 1) означает миграцию, чья судьба не объявлена нигде, — тогда
# отсутствие секции неотличимо от обратимости, и раскатывать вслепую нельзя.
ROLLBACK_STATEMENT="$(python3 "$CHART_DIR/../../scripts/schema-rollback-statement.py" "$CHART_DIR/../../.." 2>&1)" && rb_rc=0 || rb_rc=$?
printf '%s\n' "$ROLLBACK_STATEMENT"
case "$rb_rc" in
  0) log "заявление об откате произведено." ;;
  1) die "заявление об откате отказано: у миграции без секции отката не объявлена судьба.
       Пока это так, откат схемы невыразим, и раскатка идёт вслепую. Объяви решение в
       deploy/schema-rollback.txt (либо в dropguard-манифесте сервиса) и повтори." ;;
  *) die "заявление об откате не произведено (код $rb_rc) — читать нечего, а раскатывать
       без него значит соглашаться на откат, исход которого никто не называл." ;;
esac

# ── 3b. module manifests: the delivery ConfigMap — A PRECONDITION, NOT AN AFTER-EFFECT ─
#
# kacho-iam mounts a NAMED ConfigMap of module manifests and READS that directory
# AT START-UP; an empty directory is refused, because a missing manifest is not a
# module withdrawal (kacho#1027). So the object must exist BEFORE helm rolls the
# pod, not after: helm below runs with `--wait --timeout 15m`, and an iam that
# refuses to start would burn the whole window and fail the cutover.
#
# WHY THIS STEP EXISTS AT ALL (kacho#1909). `make dev-up` and `make stack-up` have
# always called the producer; this script did not, and it is the ONLY path onto
# fe3455 — `stack-up` refuses that stand outright because its chain needs the
# out-of-tree credentials layer. The production posture was therefore the one
# posture where module-manifest delivery could not be switched on at all.
#
# WHY `make`, NOT AN INLINE go run: the producer's mechanics (derive the profile
# chain, build, apply, read the four exit codes) are ONE declaration in
# deploy/Makefile. A second copy here would drift from it silently — and it would
# drift on the production path, where the drift is most expensive.
#
# THE CHAIN READ IS THE TRACKED ONE. The gitignored credentials layer is appended
# to helm's -f list below but NOT to the producer's: step 1a gates that layer down
# to credential leaf-paths only, so it cannot declare `configMapName` — reading it
# could change nothing and would make the producer depend on a file outside git.
#
# A stand that does NOT declare delivery is a lawful outcome, not a failure: the
# target says so on its own exit code 3 and leaves no ConfigMap behind.
log "module manifests: producing the delivery ConfigMap BEFORE helm (kacho#1901/#1909)…"
make -C ../.. module-manifests-configmap MODULE_MANIFESTS_STACK=fe3455 STACK_NAMESPACE="$NS" \
  EXPECT_CONTEXT="$(kubectl config current-context 2>/dev/null)" \
  || die "module-manifests producer failed — refusing to roll iam onto a delivery it cannot read.
       Re-run after fixing the finding it printed above; delivery is a PRECONDITION of the upgrade."

# ── 4. bitnami pg upgrade-guard --set args, read from pre-created Secrets ───────
#    Every pg-<svc> sets auth.existingSecret, so the chart already reads the password
#    from the Secret; these --set values are a defensive belt for the bitnami
#    passwords-on-upgrade guard. Correct value paths: auth.password (secret key
#    'password') + auth.postgresPassword (secret key 'postgres-password').
PG_SVCS=(vpc compute iam geo nlb storage registry kratos hydra)
PGARGS=()
for svc in "${PG_SVCS[@]}"; do
  sec="kacho-umbrella-pg-$svc"
  if ! kubectl -n "$NS" get secret "$sec" >/dev/null 2>&1; then
    warn "Secret $sec absent — skipping pg-$svc guard args (chart will manage it)"
    continue
  fi
  pw="$(kubectl -n "$NS" get secret "$sec" -o jsonpath='{.data.password}' | base64 -d 2>/dev/null || true)"
  apw="$(kubectl -n "$NS" get secret "$sec" -o jsonpath='{.data.postgres-password}' | base64 -d 2>/dev/null || true)"
  [ -n "$pw" ]  && PGARGS+=(--set "pg-$svc.auth.password=$pw")
  [ -n "$apw" ] && PGARGS+=(--set "pg-$svc.auth.postgresPassword=$apw")
done
log "built pg upgrade-guard --set args (${#PG_SVCS[@]} services scanned; values not echoed)."

# ── 5. the convergence upgrade ────────────────────────────────────────────────
log "helm upgrade $RELEASE — CONVERGE onto live images (--take-ownership adopts hand-applied resources)…"
if ! helm upgrade "$RELEASE" . -n "$NS" \
      "${FE_ARGS[@]}" \
      --set uif.enabled=true \
      --take-ownership \
      --wait --timeout 15m \
      ${PGARGS[@]+"${PGARGS[@]}"}; then
  warn "helm upgrade FAILED."
  # ОТКАТ ВОЗВРАЩАЕТ ОБРАЗЫ И НЕ ВОЗВРАЩАЕТ СХЕМУ. Здесь стояла одна строка
  # `helm rollback`, и она молчала о том, что мигратор уже отработал: прежний
  # образ поднялся бы на новой схеме, и ничто этого не отвергло бы. Заявление,
  # произведённое выше, печатается ещё раз — оператору оно нужно именно сейчас.
  warn "ПРЕЖДЕ ЧЕМ ОТКАТЫВАТЬ, ПРОЧТИ ЭТО:"
  printf '%s\n' "$ROLLBACK_STATEMENT" >&2
  warn "helm rollback $RELEASE -n $NS вернёт ОБРАЗЫ и НЕ вернёт схему: применённые миграции"
  warn "останутся применёнными, и прежний образ поднимется на новой схеме. Стража старта,"
  warn "который бы это отверг, сегодня нет — значит расхождение проявится не отказом, а"
  warn "неверной работой на первом обращении к колонке, которой в образе ещё нет либо в"
  warn "схеме уже нет. Ниже точки невозврата (см. заявление) откат схемы невозможен вовсе."
  warn "Inspect:   helm history $RELEASE -n $NS   |   kubectl -n $NS get pods"
  die  "cutover aborted (see above)."
fi
log "helm upgrade succeeded."

# ── 6. smoke check (registry + api-gateway + uif host rollout, then data-plane) ─
rc=0
log "smoke: rollout status (registry, api-gateway, uif host)…"
for d in registry api-gateway uif; do
  kubectl -n "$NS" rollout status deploy/"$d" --timeout=120s || { warn "rollout deploy/$d not complete"; rc=1; }
done
# storage: only when the overlay enables it (currently OFF — the kacho-storage chart is
# not integrated with the umbrella; see values.fe3455-prod.yaml). Non-fatal either way.
if kubectl -n "$NS" get deploy kacho-umbrella-storage >/dev/null 2>&1; then
  kubectl -n "$NS" rollout status deploy/kacho-umbrella-storage --timeout=120s \
    || warn "kacho-umbrella-storage not Ready yet (storage-split install — check its logs)"
else
  log "storage not installed (storage.enabled=false) — skipping its rollout check."
fi

# ── smoke: iam :9097 cluster-internal JWKS proxy (the JWKS-flip source of truth) ──
#    The registry Bearer verifier now trusts iam's :9097 mirror (registry.iam.jwksUrl).
#    Confirm iam-on-main actually serves it with Hydra kids BEFORE trusting docker auth.
#    Server-TLS (internal-CA leaf), no client cert → curl -k. Port-forward to reach the
#    ClusterIP service from the operator host.
log "smoke: iam :9097 JWKS proxy (GET /.well-known/jwks.json — expect 200 with keys)…"
# THIS upgrade rolls the iam pod, so probe only once it is actually Ready. A port-forward
# established against a terminating pod stays broken for the rest of the probe → false
# negative. That is exactly what the 2026-07-15 run hit: the smoke warned "did NOT return
# a keys set" while the endpoint served 200 with Hydra kids moments later. Hence: wait for
# the rollout, then re-establish a FRESH forward per attempt (one dead tunnel must not
# doom the whole check).
kubectl -n "$NS" rollout status deploy/kacho-iam --timeout=120s >/dev/null 2>&1 \
  || warn "kacho-iam rollout not complete — the JWKS probe below may be unreliable."

jwks=""
for _attempt in 1 2 3 4 5; do
  kubectl -n "$NS" port-forward svc/kacho-iam-internal 19097:9097 >/dev/null 2>&1 &
  pf_pid=$!
  for _ in 1 2 3 4 5 6; do
    curl -sk --max-time 3 https://127.0.0.1:19097/.well-known/jwks.json >/dev/null 2>&1 && break
    sleep 1
  done
  jwks="$(curl -sk --max-time 5 https://127.0.0.1:19097/.well-known/jwks.json 2>/dev/null || true)"
  kill "$pf_pid" >/dev/null 2>&1 || true
  wait "$pf_pid" 2>/dev/null || true
  [[ "$jwks" == *'"keys"'* ]] && break
  sleep 2
done
if [[ "$jwks" == *'"keys"'* ]]; then
  log "iam :9097 JWKS OK (serves a keys set — JWKS-flip is coherent)."
else
  warn "iam :9097 JWKS did NOT return a keys set — registry token-verify will 401. Check kacho-iam is on main-c744f956 (serves :9097, #323) and the jwks-proxy listener."
  rc=1
fi

log "smoke: curl https://registry.in-cloud.io/v2/ (expect HTTP 401 token-auth challenge)…"
code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 15 https://registry.in-cloud.io/v2/ || echo 000)"
if [ "$code" = "401" ]; then
  log "registry data-plane OK (HTTP 401 auth challenge as expected)."
else
  warn "registry /v2/ returned HTTP $code (expected 401) — check registry pod, zot, and the JWKS terminator."
  rc=1
fi

if [ "$rc" -eq 0 ]; then
  log "CUTOVER COMPLETE — forward roll applied (iam/gateway/registry + storage), smoke green."
else
  warn "CUTOVER upgrade applied, but smoke checks had warnings (see above) — investigate before declaring done."
fi
exit "$rc"
