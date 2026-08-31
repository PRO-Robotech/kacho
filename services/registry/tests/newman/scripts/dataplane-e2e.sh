#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/dataplane-e2e.sh — data-plane (:8080) OCI authz corner-case
# harness для kacho-registry. Гоняет реальный Docker Registry v2 / OCI Distribution
# поток (raw HTTP + Bearer-токен из iam /iam/token) против живого стека (fe3455) и
# проверяет authz-инварианты data-plane: 401-challenge без токена, existence-hiding
# (deny → 404), register-on-first-push, запрет деструктивного DELETE на data-plane
# (405, удаление — только control-plane DeleteTag), URL-encoded traversal guard (400).
#
# Удостоверения НЕ чеканятся здесь (строка секрета показывается один раз при выдаче):
# caller передаёт идентификатор удостоверения и саму строку. Harness лишь получает
# identity-JWT у шима /iam/token (Basic <id удостоверения>:<строка секрета>).
#
# КЛЮЧЕВОЙ МАТЕРИАЛ ЭТОЙ ПОЛОСОЙ НЕ ПРИНИМАЕТСЯ (задача #1143): приватная половина
# пары не должна ходить по сети и оседать в конфигурации клиента. Выпустите
# удостоверение вида CREDENTIAL_KIND_SECRET (`SAKeyService.Issue`) и подставьте его
# строку — прежние CLIENT_ID/SA_KEY_PEM полоса отвергает тем же 401, что и неверный
# секрет.
#
# Параметризация (env):
#   REG_TOKEN_URL   базовый URL шима iam /token (:9096), к нему добавляется /iam/token
#   DATAPLANE_URL   базовый URL data-plane OCI-прокси (:8080)
#   CREDENTIAL_ID   идентификатор базового удостоверения (soc…; его же несёт строка)
#   CREDENTIAL_SECRET  строка базового токена доступа (из IssueSAKey вида SECRET, show-once)
#   REGISTRY_ID     id реестра-namespace (reg…) с гранта push/pull на caller-SA
#   ADMIN_JWT       Bearer control-plane для cross-check ListRepositories
#   GATEWAY_URL     базовый URL api-gateway REST для control-plane cross-check
#   RUN             (опц.) суффикс изоляции прогона; иначе 1-й арг; иначе date +%s
#
# Вызов:
#   REG_TOKEN_URL=http://localhost:9096 DATAPLANE_URL=http://localhost:8080 \
#   CREDENTIAL_ID=soc… CREDENTIAL_SECRET=kacho_soc…_… REGISTRY_ID=reg… \
#   ADMIN_JWT="$JWT" GATEWAY_URL=http://localhost:38080 \
#   ./scripts/dataplane-e2e.sh 1720000000
#
# ВЕРДИКТ — ТРИ ИСХОДА, ТРИ ИМЕНИ. Шаг заканчивается одним из трёх способов, и каждый
# считается ОТДЕЛЬНО, потому что складывание любого из них в другой — ровно тот способ,
# которым сквозной прогон замолкает:
#   исполнен + утверждение сказало «да»  — PASS;
#   исполнен + утверждение сказало «нет» — FAIL     (hard-failure);
#   НЕ ИСПОЛНЕН                          — NOT-RUN  (проверка не состоялась).
# NOT-RUN никогда не вычитается и не объясняется: шаг, снятый с прогона, не проверил
# ничего. Поэтому отказ на инициализации загрузки (шаг 4) — hard-failure, а НЕ
# «документированный пропуск»: он снимал с прогона пять шагов (блоб, манифест,
# скачивание, область блоба, список тегов), и прогон при этом печатал PASS.
#
# Отдельной строкой печатается UNVERIFIED — инвариант, условие для которого харнесс
# СОЗДАТЬ НЕ МОЖЕТ by construction (второй SA без грантов: ключ показывается один раз
# и здесь не чеканится). Это открытый долг с числом, а не зачёт: итоговая строка при
# непустом UNVERIFIED слова PASS не содержит.
#
# Exit-код: 0 ТОЛЬКО когда hard-failures==0 И not-run==0.

set -uo pipefail

# ---------------------------------------------------------------------------
# Параметры прогона
# ---------------------------------------------------------------------------
RUN="${RUN:-${1:-}}"
if [[ -z "$RUN" ]]; then
  RUN="$(date +%s 2>/dev/null || echo manual)"
fi

REG_TOKEN_URL="${REG_TOKEN_URL:-http://localhost:9096}"
DATAPLANE_URL="${DATAPLANE_URL:-http://localhost:8080}"
GATEWAY_URL="${GATEWAY_URL:-}"
ADMIN_JWT="${ADMIN_JWT:-}"
SERVICE_AUD="${SERVICE_AUD:-registry.kacho.local}"

# strip trailing slashes для предсказуемой конкатенации URL
REG_TOKEN_URL="${REG_TOKEN_URL%/}"
DATAPLANE_URL="${DATAPLANE_URL%/}"
GATEWAY_URL="${GATEWAY_URL%/}"

REPO="e2e-app-${RUN}"
TAG="v1"

fail_env() { echo "FATAL: missing required env $1" >&2; exit 2; }
[[ -n "${CREDENTIAL_ID:-}" ]]     || fail_env CREDENTIAL_ID
[[ -n "${CREDENTIAL_SECRET:-}" ]] || fail_env CREDENTIAL_SECRET
[[ -n "${REGISTRY_ID:-}" ]] || fail_env REGISTRY_ID
# Cross-check плоскости управления (шаг 10) проверяет register-on-first-push и
# классификацию артефакта — инварианты того же потока, не украшение. Прежде они были
# «опциональными»: пустой ADMIN_JWT/GATEWAY_URL тихо снимал их с прогона — это и есть
# механизм маски. Отсутствие фикстуры обязано быть ОТКАЗОМ, а не пропуском.
[[ -n "${ADMIN_JWT:-}" ]]   || fail_env ADMIN_JWT
[[ -n "${GATEWAY_URL:-}" ]] || fail_env GATEWAY_URL
# Вид удостоверения проверяется ЗДЕСЬ, а не по отказу шима: полоса отвергает
# негодный вид тем же 401, что и неверный секрет (и это правильно — иначе отказ был
# бы оракулом), поэтому «настроен по-старому» и «секрет неверен» с этой стороны
# неотличимы. Отличить их обязан харнесс, у которого строка на руках.
case "$CREDENTIAL_SECRET" in
  kacho_*) : ;;
  *) echo "FATAL: CREDENTIAL_SECRET не несёт марки базового токена доступа (kacho_…).
       Докер-полоса принимает ТОЛЬКО этот вид (#1143); ключевой материал в поле пароля
       снят. Выпустите удостоверение SAKeyService.Issue с credentialKind=CREDENTIAL_KIND_SECRET
       и подставьте показанную один раз строку." >&2; exit 2 ;;
esac
command -v curl    >/dev/null || { echo "FATAL: curl not found"    >&2; exit 2; }
command -v python3 >/dev/null || { echo "FATAL: python3 not found" >&2; exit 2; }

TMP="$(mktemp -d "${TMPDIR:-/tmp}/reg-dpe2e.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
BODY="$TMP/body"
HDR="$TMP/hdr"

HARD_PASSES=0
HARD_FAILS=0
NOT_RUN=0
UNVERIFIED=0

echo "=== kacho-registry data-plane OCI e2e ==="
echo "  run=$RUN  registry=$REGISTRY_ID  repo=$REPO:$TAG"
echo "  token-url=$REG_TOKEN_URL  dataplane=$DATAPLANE_URL"
echo

# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------

# do_req METHOD URL [curl-args…] — тело → $BODY, заголовки → $HDR, echo HTTP-код
# (на сетевом сбое curl печатает "000" через -w; пустой вывод → "000").
do_req() {
  local method="$1" url="$2"; shift 2
  local code
  code="$(curl -sS --path-as-is -o "$BODY" -D "$HDR" -w '%{http_code}' \
    -X "$method" "$url" "$@" 2>/dev/null)"
  echo "${code:-000}"
}

# assert_hard LABEL ACTUAL EXPECTED… — PASS если ACTUAL совпал с одним из EXPECTED;
# иначе FAIL + инкремент HARD_FAILS.
assert_hard() {
  local label="$1" actual="$2"; shift 2
  local e
  for e in "$@"; do
    if [[ "$actual" == "$e" ]]; then
      echo "PASS [hard] $label — HTTP $actual"
      HARD_PASSES=$((HARD_PASSES + 1))
      return 0
    fi
  done
  echo "FAIL [hard] $label — HTTP $actual (expected: $*)"
  HARD_FAILS=$((HARD_FAILS + 1))
  return 1
}

# pass_hard / fail_hard — утверждение, доказанное не HTTP-кодом (заголовок, тело).
pass_hard() { echo "PASS [hard] $*"; HARD_PASSES=$((HARD_PASSES + 1)); }
fail_hard() { echo "FAIL [hard] $*"; HARD_FAILS=$((HARD_FAILS + 1)); }

# not_run LABEL … — шаг, который ДОЛЖЕН был исполниться, не исполнился. Проверки в нём
# не состоялись, поэтому это не пропуск, а провал: считается и роняет exit-код.
not_run() { echo "NOT-RUN $*"; NOT_RUN=$((NOT_RUN + 1)); }

# unverified LABEL … — инвариант, условие для которого харнесс создать НЕ МОЖЕТ by
# construction. Открытый долг с числом: печатается, считается, попадает в итоговую
# строку — и НИКОГДА не читается как «проверено».
unverified() { echo "UNVERIFIED $*"; UNVERIFIED=$((UNVERIFIED + 1)); }

# body_contains STR — 0 если тело $BODY содержит STR.
body_contains() { grep -qF -- "$1" "$BODY"; }

# ---------------------------------------------------------------------------
# 1. Mint identity-JWT: POST {REG_TOKEN_URL}/iam/token?service=… Basic(id:секрет)
# ---------------------------------------------------------------------------
echo "--- 1. mint token (/iam/token, Basic <id>:<строка секрета>) ---"
BASIC="$(printf '%s:%s' "$CREDENTIAL_ID" "$CREDENTIAL_SECRET" | base64 | tr -d '\n\r')"
code="$(do_req POST "${REG_TOKEN_URL}/iam/token?service=${SERVICE_AUD}" \
  -H "Authorization: Basic ${BASIC}")"
assert_hard "token mint" "$code" 200 || true

TOKEN="$(python3 - "$BODY" <<'PY' 2>/dev/null || true
import json, sys
try:
    j = json.load(open(sys.argv[1]))
except Exception:
    print(""); sys.exit(0)
print(j.get("token") or j.get("access_token") or "")
PY
)"
HAVE_TOKEN=0
if [[ -n "$TOKEN" ]]; then
  HAVE_TOKEN=1
  echo "       token acquired (len=${#TOKEN})"
else
  fail_hard "token extraction — empty .token/.access_token"
fi
AUTH=(-H "Authorization: Bearer ${TOKEN}")
echo

# ---------------------------------------------------------------------------
# 2. GET /v2/ БЕЗ токена → 401 + WWW-Authenticate: Bearer realm=…
# ---------------------------------------------------------------------------
echo "--- 2. ping without token → 401 challenge ---"
code="$(do_req GET "${DATAPLANE_URL}/v2/")"
assert_hard "GET /v2/ no-token" "$code" 401 || true
if grep -qiE '^WWW-Authenticate:[[:space:]]*Bearer[[:space:]]+realm=' "$HDR"; then
  pass_hard "WWW-Authenticate Bearer realm present"
else
  fail_hard "WWW-Authenticate Bearer realm missing"
fi
echo

# ---------------------------------------------------------------------------
# 3. GET /v2/ С токеном → 200
# ---------------------------------------------------------------------------
echo "--- 3. ping with token → 200 ---"
if [[ "$HAVE_TOKEN" == 1 ]]; then
  code="$(do_req GET "${DATAPLANE_URL}/v2/" "${AUTH[@]}")"
  assert_hard "GET /v2/ with-token" "$code" 200 || true
else
  not_run "GET /v2/ with-token — токен не получен (шаг 1)"
fi
echo

# ---------------------------------------------------------------------------
# 4. push-init POST /v2/{reg}/{repo}/blobs/uploads/ → 202.
#
#    404 здесь — HARD-FAILURE, а не «документированное existence-hiding». Контракт
#    вызова (см. REGISTRY_ID в заголовке) требует реестр, на котором у caller-SA ЕСТЬ
#    грант push/pull; 404 означает, что фикстура не та, которую харнесс требует. А
#    цена мягкости была не в одной строке: 404 снимал с прогона шаги 5, 5b и 6 —
#    блоб, манифест, скачивание, область блоба и список тегов, — и прогон печатал
#    PASS. Существование-hiding для НЕГРАНТНУТОГО принципала — отдельный инвариант,
#    условие для которого этот харнесс создать не может (шаг 7, UNVERIFIED).
# ---------------------------------------------------------------------------
echo "--- 4. push-init blobs/uploads/ → 202 (404 = hard-failure, не пропуск) ---"
UPLOAD_URL=""
PUSH_SKIP=0
if [[ "$HAVE_TOKEN" == 1 ]]; then
  code="$(do_req POST "${DATAPLANE_URL}/v2/${REGISTRY_ID}/${REPO}/blobs/uploads/" "${AUTH[@]}")"
  if [[ "$code" == 202 ]]; then
    pass_hard "push-init — HTTP 202 (grant present)"
    loc="$(awk 'BEGIN{IGNORECASE=1} /^Location:/ {v=$2; sub(/\r$/,"",v); print v}' "$HDR" | tail -1)"
    if [[ -z "$loc" ]]; then
      fail_hard "push-init — 202 without Location header"
      PUSH_SKIP=1
    elif [[ "$loc" == http* ]]; then
      UPLOAD_URL="$loc"
    else
      UPLOAD_URL="${DATAPLANE_URL}${loc}"
    fi
  elif [[ "$code" == 404 ]]; then
    fail_hard "push-init — HTTP 404: у caller-SA нет v_create в registry_registry:${REGISTRY_ID}. Харнесс требует грантнутый реестр; на 404 шаги 5/5b/6 не исполняются, поэтому это провал, а не пропуск"
    PUSH_SKIP=1
  else
    fail_hard "push-init — HTTP $code (expected 202)"
    PUSH_SKIP=1
  fi
else
  not_run "push-init — токен не получен (шаг 1)"; PUSH_SKIP=1
fi
echo

# ---------------------------------------------------------------------------
# 5. monolithic push: PUT config blob (201) → PUT manifest (201)
# ---------------------------------------------------------------------------
echo "--- 5. monolithic push: config blob + manifest → 201/201 ---"
MANIFEST_OK=0
if [[ "$PUSH_SKIP" == 0 && -n "$UPLOAD_URL" ]]; then
  # config-blob: минимальный OCI image-config (data-plane/zot верифицируют digest,
  # не семантику; register-on-first-push срабатывает на manifest-PUT).
  printf '%s' '{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}' > "$TMP/config.json"
  read -r CFG_DIGEST CFG_SIZE < <(python3 - "$TMP/config.json" <<'PY'
import hashlib, sys
b = open(sys.argv[1], "rb").read()
print("sha256:" + hashlib.sha256(b).hexdigest(), len(b))
PY
)
  # monolithic blob upload (PUT session-URL ?digest=…)
  if [[ "$UPLOAD_URL" == *\?* ]]; then blob_url="${UPLOAD_URL}&digest=${CFG_DIGEST}"; else blob_url="${UPLOAD_URL}?digest=${CFG_DIGEST}"; fi
  code="$(do_req PUT "$blob_url" "${AUTH[@]}" \
    -H "Content-Type: application/octet-stream" --data-binary "@${TMP}/config.json")"
  assert_hard "PUT config blob" "$code" 201 || true

  # OCI image-manifest, config→pushed blob, layers пусто (artifact-style push,
  # как в проверенном live-потоке).
  python3 - "$CFG_DIGEST" "$CFG_SIZE" > "$TMP/manifest.json" <<'PY'
import json, sys
digest, size = sys.argv[1], int(sys.argv[2])
print(json.dumps({
    "schemaVersion": 2,
    "mediaType": "application/vnd.oci.image.manifest.v1+json",
    "config": {
        "mediaType": "application/vnd.oci.image.config.v1+json",
        "digest": digest,
        "size": size,
    },
    "layers": [],
}))
PY
  code="$(do_req PUT "${DATAPLANE_URL}/v2/${REGISTRY_ID}/${REPO}/manifests/${TAG}" "${AUTH[@]}" \
    -H "Content-Type: application/vnd.oci.image.manifest.v1+json" --data-binary "@${TMP}/manifest.json")"
  if assert_hard "PUT manifest" "$code" 201; then MANIFEST_OK=1; fi
else
  not_run "monolithic push (блоб + манифест) — нет сессии загрузки (шаг 4 провалился)"
fi
echo

# ---------------------------------------------------------------------------
# 5b. helm-artifact push: config.mediaType = vnd.cncf.helm.config → образ
#     классифицируется как HELM_CHART (дискриминатор docker vs helm). Тот же
#     blob+manifest путь, что docker push (helm CLI не требуется).
# ---------------------------------------------------------------------------
echo "--- 5b. helm-artifact push (config vnd.cncf.helm.config) → HELM_CHART ---"
HELM_REPO="${REPO}-helm"
HELM_OK=0
if [[ "$PUSH_SKIP" == 0 ]]; then
  hcode="$(do_req POST "${DATAPLANE_URL}/v2/${REGISTRY_ID}/${HELM_REPO}/blobs/uploads/" "${AUTH[@]}")"
  hloc="$(awk 'BEGIN{IGNORECASE=1} /^Location:/ {v=$2; sub(/\r$/,"",v); print v}' "$HDR" | tail -1)"
  if [[ "$hcode" == 202 && -n "$hloc" ]]; then
    [[ "$hloc" == http* ]] && HUP="$hloc" || HUP="${DATAPLANE_URL}${hloc}"
    printf '%s' '{"name":"demo-chart","version":"0.1.0","apiVersion":"v2"}' > "$TMP/helmcfg.json"
    read -r HCFG_DIGEST HCFG_SIZE < <(python3 - "$TMP/helmcfg.json" <<'PY'
import hashlib, sys
b = open(sys.argv[1], "rb").read()
print("sha256:" + hashlib.sha256(b).hexdigest(), len(b))
PY
)
    if [[ "$HUP" == *\?* ]]; then hblob="${HUP}&digest=${HCFG_DIGEST}"; else hblob="${HUP}?digest=${HCFG_DIGEST}"; fi
    code="$(do_req PUT "$hblob" "${AUTH[@]}" -H "Content-Type: application/octet-stream" --data-binary "@${TMP}/helmcfg.json")"
    assert_hard "PUT helm config blob" "$code" 201 || true
    python3 - "$HCFG_DIGEST" "$HCFG_SIZE" > "$TMP/helmmanifest.json" <<'PY'
import json, sys
digest, size = sys.argv[1], int(sys.argv[2])
print(json.dumps({
    "schemaVersion": 2,
    "mediaType": "application/vnd.oci.image.manifest.v1+json",
    "config": {"mediaType": "application/vnd.cncf.helm.config.v1+json", "digest": digest, "size": size},
    "layers": [],
}))
PY
    code="$(do_req PUT "${DATAPLANE_URL}/v2/${REGISTRY_ID}/${HELM_REPO}/manifests/1.0.0" "${AUTH[@]}" \
      -H "Content-Type: application/vnd.oci.image.manifest.v1+json" --data-binary "@${TMP}/helmmanifest.json")"
    if assert_hard "PUT helm manifest" "$code" 201; then HELM_OK=1; fi
  else
    not_run "helm push-init → HTTP $hcode (нет сессии) — классификация артефакта не проверена"
  fi
else
  not_run "helm push — push отключён (шаг 4 провалился)"
fi
echo

# ---------------------------------------------------------------------------
# 6. pull: GET manifest (200) → GET config blob (200) → GET tags/list (200 + tag)
# ---------------------------------------------------------------------------
echo "--- 6. pull: manifest / blob / tags-list → 200 ---"
if [[ "$MANIFEST_OK" == 1 ]]; then
  # register-on-first-push материализует per-object v_get на новом repo асинхронно.
  # Окно видимости складывают ДВА слагаемых, и у каждого свой владелец: кэш вердиктов
  # registry (ручка KACHO_REGISTRY_AUTHZ_CACHE_TTL) и материализация выдачи у владельца
  # прав (величину называет документация IAM). Здесь величина НЕ пишется: она была бы
  # вторым местом об одном предмете и разошлась бы с ручкой молча — бюджет ожидания
  # виден на связывании ниже. Первый pull может дать 404 (existence-hidden, грант ещё
  # не долетел) — poll-retry до 10× по 1.5s, затем финальный assert (#10 grant-latency).
  for _att in $(seq 1 10); do
    code="$(do_req GET "${DATAPLANE_URL}/v2/${REGISTRY_ID}/${REPO}/manifests/${TAG}" "${AUTH[@]}" \
      -H "Accept: application/vnd.oci.image.manifest.v1+json")"
    [[ "$code" == 200 ]] && break
    sleep 1.5
  done
  assert_hard "GET manifest (poll-retry for grant-latency)" "$code" 200 || true

  code="$(do_req GET "${DATAPLANE_URL}/v2/${REGISTRY_ID}/${REPO}/blobs/${CFG_DIGEST}" "${AUTH[@]}")"
  assert_hard "GET config blob (blob-scope in-repo)" "$code" 200 || true

  code="$(do_req GET "${DATAPLANE_URL}/v2/${REGISTRY_ID}/${REPO}/tags/list" "${AUTH[@]}")"
  assert_hard "GET tags/list" "$code" 200 || true
  if body_contains "\"${TAG}\""; then
    pass_hard "tags/list contains ${TAG}"
  else
    fail_hard "tags/list missing ${TAG}"
  fi
else
  not_run "pull (манифест / блоб / список тегов) — манифест не запушен"
fi
echo

# ---------------------------------------------------------------------------
# 7. NEGATIVE existence-hiding
#    (a) push-init на свежий repo БЕЗ Authorization → 401 (hard)
#    (b) non-granted principal → 404 (UNVERIFIED: второй SA здесь не чеканится)
# ---------------------------------------------------------------------------
echo "--- 7. negative existence-hiding ---"
code="$(do_req POST "${DATAPLANE_URL}/v2/${REGISTRY_ID}/${REPO}-noauth/blobs/uploads/")"
assert_hard "push-init no-auth (fresh repo)" "$code" 401 || true
unverified "non-granted principal push/pull → 404 (existence-hiding): требует ВТОРОГО SA без v_* на объекте, а ключ показывается один раз и здесь не чеканится. Условие харнесс создать не может — инвариант остаётся НЕ проверенным (открытый долг), а не зачтённым"
echo

# ---------------------------------------------------------------------------
# 8. direct manifest DELETE → 405 (data-plane запрещает деструктив; удаление —
#    только control-plane DeleteTag)
# ---------------------------------------------------------------------------
echo "--- 8. manifest DELETE → 405 (data-plane forbids destructive) ---"
if [[ "$HAVE_TOKEN" == 1 ]]; then
  code="$(do_req DELETE "${DATAPLANE_URL}/v2/${REGISTRY_ID}/${REPO}/manifests/${TAG}" "${AUTH[@]}")"
  assert_hard "DELETE manifest" "$code" 405 || true
else
  not_run "DELETE manifest — токен не получен (шаг 1)"
fi
echo

# ---------------------------------------------------------------------------
# 9. path-traversal (URL-encoded) → 400
# ---------------------------------------------------------------------------
echo "--- 9. path-traversal ..%2f..%2fetc → 400 ---"
if [[ "$HAVE_TOKEN" == 1 ]]; then
  code="$(do_req GET "${DATAPLANE_URL}/v2/${REGISTRY_ID}/..%2f..%2fetc/manifests/x" "${AUTH[@]}")"
  assert_hard "traversal GET" "$code" 400 || true
else
  not_run "traversal GET — токен не получен (шаг 1)"
fi
echo

# ---------------------------------------------------------------------------
# 10. control-plane cross-check: ListRepositories видит register-on-first-push repo
#     + классификация артефакта. ADMIN_JWT/GATEWAY_URL — ОБЯЗАТЕЛЬНЫ (проверены на
#     старте), поэтому «DOC-skip при пустых переменных» здесь больше не существует.
# ---------------------------------------------------------------------------
echo "--- 10. control-plane cross-check ListRepositories + artifact_type ---"
# at_of REPO — печатает artifact_type репо REPO из последнего тела $BODY (пусто, если нет).
at_of() {
  python3 - "$BODY" "$1" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception:
    print(""); sys.exit(0)
for r in d.get("repositories", []):
    if r.get("name") == sys.argv[2]:
        print(r.get("artifact_type", "")); sys.exit(0)
print("")
PY
}
# poll-retry: register-on-first-push материализует v_list на repo асинхронно. Окно
# видимости — кэш вердиктов registry (ручка KACHO_REGISTRY_AUTHZ_CACHE_TTL) плюс
# материализация выдачи у владельца прав; величину называют их владельцы, здесь
# стоит только БЮДЖЕТ, и он виден на связывании ниже (10× по 1.5s).
# Тянем ListRepositories, пока docker-repo не появится.
DOCKER_AT=""
for _a in $(seq 1 10); do
  code="$(do_req GET "${GATEWAY_URL}/registry/v1/registries/${REGISTRY_ID}/repositories" \
    -H "Authorization: Bearer ${ADMIN_JWT}")"
  DOCKER_AT="$(at_of "$REPO")"
  [[ -n "$DOCKER_AT" ]] && break
  sleep 1.5
done
if [[ "$code" == 200 && -n "$DOCKER_AT" ]]; then
  pass_hard "ListRepositories contains ${REPO} (register-on-first-push visible) — HTTP 200"
  # GWT-1: docker/oci config → CONTAINER_IMAGE.
  if [[ "$DOCKER_AT" == "ARTIFACT_TYPE_CONTAINER_IMAGE" ]]; then
    pass_hard "${REPO} artifact_type = ARTIFACT_TYPE_CONTAINER_IMAGE"
  else
    fail_hard "${REPO} artifact_type = '${DOCKER_AT}' (expected ARTIFACT_TYPE_CONTAINER_IMAGE)"
  fi
  # GWT-10: back-compat — существующие поля не пропали.
  if python3 - "$BODY" "$REPO" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
for r in d.get("repositories", []):
  if r.get("name") == sys.argv[2]:
      sys.exit(0 if all(k in r for k in ("tag_count", "size_bytes", "updated_at")) else 1)
sys.exit(1)
PY
  then
    pass_hard "${REPO} back-compat fields (tag_count/size_bytes/updated_at) present"
  else
    fail_hard "${REPO} back-compat fields missing"
  fi
else
  fail_hard "ListRepositories cross-check — HTTP $code, ${REPO} отсутствует после polling'а (~15s): register-on-first-push не материализовался"
fi
# GWT-2: helm-config образ → HELM_CHART (если helm-push прошёл).
if [[ "$HELM_OK" == 1 ]]; then
  HELM_AT=""
  for _a in $(seq 1 10); do
    code="$(do_req GET "${GATEWAY_URL}/registry/v1/registries/${REGISTRY_ID}/repositories" \
      -H "Authorization: Bearer ${ADMIN_JWT}")"
    HELM_AT="$(at_of "$HELM_REPO")"
    [[ -n "$HELM_AT" ]] && break
    sleep 1.5
  done
  if [[ "$HELM_AT" == "ARTIFACT_TYPE_HELM_CHART" ]]; then
    pass_hard "${HELM_REPO} artifact_type = ARTIFACT_TYPE_HELM_CHART"
  else
    fail_hard "${HELM_REPO} artifact_type = '${HELM_AT}' (expected ARTIFACT_TYPE_HELM_CHART)"
  fi
else
  not_run "helm-classify cross-check — helm push (5b) не прошёл"
fi
echo

# ---------------------------------------------------------------------------
# Итог
# ---------------------------------------------------------------------------
# Вердикт В ЧИСЛАХ. Читать сначала вторую и третью пару: прогон, у которого шаги
# сняты, оставляет остальные счётчики здоровыми на вид.
echo "=== summary: hard-pass=${HARD_PASSES}  hard-fail=${HARD_FAILS}  NOT-RUN=${NOT_RUN}  UNVERIFIED=${UNVERIFIED} ==="
if [[ "$NOT_RUN" -gt 0 ]]; then
  echo "  ^ ${NOT_RUN} шаг(ов) НЕ ИСПОЛНЕНО — эти проверки не состоялись; «не выполнилось» не зачитывается за «прошло»" >&2
fi
if [[ "$UNVERIFIED" -gt 0 ]]; then
  echo "  ^ ${UNVERIFIED} инвариант(ов) харнесс проверить не может by construction — открытый долг, не зачёт" >&2
fi
if [[ "$HARD_FAILS" -gt 0 || "$NOT_RUN" -gt 0 ]]; then
  echo "RESULT: FAIL (${HARD_FAILS} hard-failure(s), ${NOT_RUN} not-run step(s))"
  exit 1
fi
if [[ "$HARD_PASSES" -eq 0 ]]; then
  echo "RESULT: FAIL (0 hard assertions executed — прогон, ничего не спросивший, не зелёный)"
  exit 1
fi
if [[ "$UNVERIFIED" -gt 0 ]]; then
  echo "RESULT: GREEN-WITH-DEBT (${HARD_PASSES} hard assertion(s) green; ${UNVERIFIED} invariant(s) NOT VERIFIED)"
  exit 0
fi
echo "RESULT: PASS (${HARD_PASSES} hard assertion(s) green)"
exit 0
