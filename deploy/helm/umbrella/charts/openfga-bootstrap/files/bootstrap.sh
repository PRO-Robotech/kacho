#!/bin/sh
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ПЕРВИЧНАЯ УСТАНОВКА МОДЕЛИ ПРАВ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ЭТО ФАЙЛ, А НЕ ТЕЛО В ШАБЛОНЕ
#
# Тело, доступное только через рендер чарта, проверяется исключительно на глаз:
# ни один тест не может его ИСПОЛНИТЬ и утверждать исход. Файл монтируется в
# задание через `.Files.Get`, а его исход утверждает
# `deploy/tests/helm/bootstrap-refusal-classified-test.sh`, гоняющий этот самый
# скрипт против подставного хранилища прав.
#
# ─────────────────────────────────────────────────────────────────────────────
# ОТКАЗ ВНЕШНЕЙ ЗАВИСИМОСТИ: НАСТРОЙКА ИЛИ СБОЙ — РАЗНЫЕ ИСХОДЫ
#
# У шага две внешние зависимости: хранилище прав и API кластера. Их отказ бывает
# двух природ, и различить их обязан сам шаг:
#
#   • НАСТРОЙКА — ответ, который повтором не исправится НИКОГДА: не тот адрес,
#     не тот эндпоинт, не тот формат модели, отказ в правах у задания. Такой
#     ответ ГРОМКИЙ и роняет установку;
#   • СБОЙ — сторона сейчас не отвечает (нет ответа, 5xx, конфликт записи).
#     Здесь допустим ограниченный повтор и мягкий проход, но он ОБЯЗАН нести
#     счётчик, иначе он невидим.
#
# Не различать их — значит сделать постоянную неправильную настройку штатным
# режимом: шаг присутствует, исполняется на каждой установке и не отказывает
# никогда. Ровно это здесь и было: запись связи шла с `|| echo "... (OK)"`, то
# есть ЛЮБОЙ отказ печатался как «уже существует» и установка выходила успехом.
#
# ─────────────────────────────────────────────────────────────────────────────
# ИТОГ ПЕЧАТАЕТСЯ ВСЕГДА
#
# «Ноль отказов за всю жизнь шага» обязано быть отличимо от «шаг ни разу не
# исполнялся» — иначе оба выглядят как тишина. Поэтому на ЛЮБОМ выходе (успех,
# отказ, быстрый пропуск) печатается строка ИТОГ с числами попыток, успехов,
# законных повторов и мягких проходов.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПЕРЕМЕННЫЕ ОКРУЖЕНИЯ
#
#   OPENFGA_URL, STORE_NAME, STORE_SECRET_NAME, MODEL_SECRET_NAME, NAMESPACE,
#   CLUSTER_SINGLETON_ID, CONSUMER_DEPLOYMENTS  — приезжают из шаблона задания.
#   MODEL_DIR                                   — каталог с model.fga/model.json.
#   BOOTSTRAP_HEALTH_ATTEMPTS/_SLEEP            — ожидание готовности.
#   BOOTSTRAP_WRITE_ATTEMPTS/_SLEEP/_TIMEOUT    — повтор записи при СБОЕ.
# ─────────────────────────────────────────────────────────────────────────────
set -eu

OPENFGA_URL="${OPENFGA_URL:-http://kacho-umbrella-openfga:8080}"
STORE_NAME="${STORE_NAME:-kacho-store}"
STORE_SECRET_NAME="${STORE_SECRET_NAME:-kacho-iam-openfga-store}"
MODEL_SECRET_NAME="${MODEL_SECRET_NAME:-openfga-model-id}"
NAMESPACE="${NAMESPACE:-kacho}"
CLUSTER_SINGLETON_ID="${CLUSTER_SINGLETON_ID:-cluster_kacho_root}"

MODEL_DIR="${MODEL_DIR:-/etc/openfga-model}"
HEALTH_ATTEMPTS="${BOOTSTRAP_HEALTH_ATTEMPTS:-60}"
HEALTH_SLEEP="${BOOTSTRAP_HEALTH_SLEEP:-5}"
WRITE_ATTEMPTS="${BOOTSTRAP_WRITE_ATTEMPTS:-5}"
WRITE_SLEEP="${BOOTSTRAP_WRITE_SLEEP:-3}"
WRITE_TIMEOUT="${BOOTSTRAP_WRITE_TIMEOUT:-15}"

# ─── Счётчики шага. Печатаются на КАЖДОМ выходе (см. trap ниже). ─────────────
WRITES_ATTEMPTED=0
WRITES_OK=0
WRITES_DUP=0
WRITES_SOFT=0
WRITE_STEP_REACHED=no

summary() {
  echo "[bootstrap] ИТОГ установки прав: write_step_reached=${WRITE_STEP_REACHED}" \
       "writes_attempted=${WRITES_ATTEMPTED} writes_ok=${WRITES_OK}" \
       "writes_duplicate=${WRITES_DUP} writes_soft_pass=${WRITES_SOFT}"
  if [ "${WRITES_SOFT}" -gt 0 ]; then
    echo "[bootstrap] ВНИМАНИЕ: ${WRITES_SOFT} связ(и) НЕ записаны — хранилище прав не ответило."
    echo "[bootstrap] Права по ним не разрешатся, пока установку не повторят."
  fi
}
trap summary EXIT

# die_misconfig <что> <код> <ответ> — отказ, доказывающий НАСТРОЙКУ.
#
# Текст адресован оператору, поднимающему стенд: он обязан называть предмет
# прямо, иначе стенд не поднять (см. `security.md`, три места из-под запрета на
# operational-детали — текст отказа при refuse-to-start одно из них).
die_misconfig() {
  echo "[bootstrap] ОТКАЗ УСТАНОВКИ: ${1} — сторона ответила HTTP ${2}."
  echo "[bootstrap] Такой ответ повтором не исправится: это НАСТРОЙКА, а не сбой."
  echo "[bootstrap] Проверьте адрес хранилища прав (OPENFGA_URL=${OPENFGA_URL}),"
  echo "[bootstrap] формат модели и права задания на запись."
  echo "[bootstrap] Ответ стороны: $(printf '%s' "${3}" | tr -d '\n' | cut -c1-400)"
  exit 1
}

echo "[bootstrap] using bundled kubectl + jq (alpine/k8s image)"

MODEL_DSL="${MODEL_DIR}/model.fga"
MODEL_JSON="${MODEL_DIR}/model.json"
if [ ! -s "${MODEL_DSL}" ]; then
  echo "[bootstrap] FATAL: ${MODEL_DSL} missing or empty"
  exit 1
fi
if [ ! -s "${MODEL_JSON}" ]; then
  echo "[bootstrap] FATAL: ${MODEL_JSON} missing or empty"
  exit 1
fi
# sha256 of the DSL — fast-path idempotency key (busybox sha256sum).
DSL_SHA=$(sha256sum "${MODEL_DSL}" | awk '{print $1}')
echo "[bootstrap] DSL sha256 = ${DSL_SHA}"
echo "[bootstrap] JSON model size = $(wc -c < "${MODEL_JSON}") bytes"

# ─── Step A: ждём готовности хранилища прав ─────────────────────────────────
#
# Ожидание различает те же две природы: ответ 4xx означает, что мы стучимся не
# туда, и ждать пять минут бессмысленно — такой ответ роняет установку сразу.
echo "[bootstrap] waiting for OpenFGA ${OPENFGA_URL}/healthz ..."
i=0
HEALTH_CODE=000
while [ "$i" -lt "${HEALTH_ATTEMPTS}" ]; do
  HEALTH_BODY_FILE="$(mktemp)"
  HEALTH_CODE=$(curl -s -o "${HEALTH_BODY_FILE}" -w '%{http_code}' --max-time 10 \
    "${OPENFGA_URL}/healthz") || HEALTH_CODE=000
  HEALTH_BODY="$(cat "${HEALTH_BODY_FILE}")"
  rm -f "${HEALTH_BODY_FILE}"
  case "${HEALTH_CODE}" in
    2*)
      echo "[bootstrap] OpenFGA ready"
      break ;;
    000 | 5*)
      : ;;  # СБОЙ: ещё не поднялось — ждём
    *)
      die_misconfig "проверка готовности ${OPENFGA_URL}/healthz" "${HEALTH_CODE}" "${HEALTH_BODY}" ;;
  esac
  sleep "${HEALTH_SLEEP}"
  i=$((i + 1))
done
case "${HEALTH_CODE}" in
  2*) : ;;
  *)
    echo "[bootstrap] ОТКАЗ УСТАНОВКИ: хранилище прав не ответило за ${HEALTH_ATTEMPTS} попыток"
    echo "[bootstrap] (последний код HTTP ${HEALTH_CODE}, адрес ${OPENFGA_URL})."
    exit 1 ;;
esac

# ─── Step B: хранилище — найти существующее либо создать ────────────────────
#
# Здесь мягкого прохода НЕТ, и это не строгость ради строгости: пустой ответ
# неотличим от «хранилища нет», поэтому поглощённый отказ перечисления заставил
# бы шаг создать ВТОРОЕ хранилище — с нулём моделей и нулём связей, при том что
# первое живо. Отказ перечисления обязан ронять установку, какой бы природы он
# ни был.
echo "[bootstrap] looking up store '${STORE_NAME}'..."
STORES_FILE="$(mktemp)"
STORES_CODE=$(curl -s -o "${STORES_FILE}" -w '%{http_code}' --max-time 15 \
  "${OPENFGA_URL}/stores") || STORES_CODE=000
case "${STORES_CODE}" in
  2*) : ;;
  000 | 5*)
    echo "[bootstrap] ОТКАЗ УСТАНОВКИ: перечисление хранилищ не удалось (HTTP ${STORES_CODE})."
    echo "[bootstrap] Продолжать нельзя: пустой ответ неотличим от «хранилища нет»,"
    echo "[bootstrap] и шаг создал бы второе хранилище поверх живого. Повторите установку."
    rm -f "${STORES_FILE}"
    exit 1 ;;
  *)
    STORES_BODY="$(cat "${STORES_FILE}")"
    rm -f "${STORES_FILE}"
    die_misconfig "перечисление хранилищ ${OPENFGA_URL}/stores" "${STORES_CODE}" "${STORES_BODY}" ;;
esac
STORE_ID=$(jq -r --arg n "${STORE_NAME}" '.stores[]? | select(.name==$n) | .id' <"${STORES_FILE}")
rm -f "${STORES_FILE}"

if [ -z "${STORE_ID}" ]; then
  echo "[bootstrap] store not found — creating"
  CREATE_FILE="$(mktemp)"
  CREATE_CODE=$(curl -s -o "${CREATE_FILE}" -w '%{http_code}' --max-time 15 \
    -XPOST "${OPENFGA_URL}/stores" \
    -H 'content-type: application/json' \
    -d "{\"name\":\"${STORE_NAME}\"}") || CREATE_CODE=000
  CREATE_BODY="$(cat "${CREATE_FILE}")"
  rm -f "${CREATE_FILE}"
  case "${CREATE_CODE}" in
    2*) : ;;
    *)  die_misconfig "создание хранилища '${STORE_NAME}'" "${CREATE_CODE}" "${CREATE_BODY}" ;;
  esac
  STORE_ID=$(printf '%s' "${CREATE_BODY}" | jq -r '.id // empty')
  if [ -z "${STORE_ID}" ]; then
    echo "[bootstrap] ОТКАЗ УСТАНОВКИ: создание хранилища вернуло ответ без идентификатора."
    exit 1
  fi
  echo "[bootstrap] created store id=${STORE_ID}"
else
  echo "[bootstrap] reusing existing store id=${STORE_ID}"
fi

# Persist store-id Secret (no-op if already present with same value).
kubectl -n "${NAMESPACE}" create secret generic "${STORE_SECRET_NAME}" \
  --from-literal=store_id="${STORE_ID}" \
  --dry-run=client -o yaml | kubectl apply -f -

# ─── Step C: idempotency check — compare DSL sha256 with Secret annotation
CURRENT_DSL_SHA=$(kubectl -n "${NAMESPACE}" get secret "${MODEL_SECRET_NAME}" \
  -o jsonpath='{.metadata.annotations.kacho-deploy/last-applied-dsl-sha256}' 2>/dev/null || true)
CURRENT_MODEL_ID=$(kubectl -n "${NAMESPACE}" get secret "${MODEL_SECRET_NAME}" \
  -o jsonpath='{.data.current}' 2>/dev/null | base64 -d 2>/dev/null || true)

# Пропуск законен ТОЛЬКО если названная модель ЕСТЬ в ЭТОМ хранилище.
#
# Прежде условие спрашивало лишь про совпадение хеша и непустоту записи в
# секрете — то есть про СВОЮ ПРЕЖНЮЮ ОТМЕТКУ, а не про предмет. На кластере без
# постоянных томов хранилище рождается заново при каждом перезапуске своей базы,
# а секрет переживает это, — и установка печатала «OK (no-op)» над хранилищем,
# где моделей ноль. Права после этого не разрешаются НИ У КОГО, при том что
# задание вышло успехом, а все поды готовы.
MODEL_PRESENT=""
if [ -n "${CURRENT_MODEL_ID}" ]; then
  MODEL_PRESENT=$(curl -sf --max-time 10 \
    "${OPENFGA_URL}/stores/${STORE_ID}/authorization-models/${CURRENT_MODEL_ID}" \
    2>/dev/null | jq -r '.authorization_model.id // empty' || true)
fi

if [ "${CURRENT_DSL_SHA}" = "${DSL_SHA}" ] && [ -n "${CURRENT_MODEL_ID}" ] && [ -n "${MODEL_PRESENT}" ]; then
  echo "[bootstrap] DSL sha256 matches И модель ${CURRENT_MODEL_ID} есть в хранилище ${STORE_ID} -> skip-write"
  # Still ensure Secret has store_id reference for kacho-iam consumers (no-op if equal).
  kubectl -n "${NAMESPACE}" annotate secret "${MODEL_SECRET_NAME}" \
    "kacho-deploy/last-bootstrap-at=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --overwrite >/dev/null
  # Итог печатается и здесь (trap): writes_attempted=0 при
  # write_step_reached=no читается как «шаг записи не понадобился», а не как
  # «шаг отработал без единого отказа».
  echo "[bootstrap] OK (no-op)"
  exit 0
fi

# ─── Step D: write new authorization-model ──────────────────────────
if [ "${CURRENT_DSL_SHA}" = "${DSL_SHA}" ] && [ -n "${CURRENT_MODEL_ID}" ] && [ -z "${MODEL_PRESENT}" ]; then
  echo "[bootstrap] отметка называет модель ${CURRENT_MODEL_ID}, которой в хранилище ${STORE_ID} НЕТ — пишем заново"
fi
echo "[bootstrap] writing new authorization model (DSL changed or first install)..."
MODEL_FILE="$(mktemp)"
MODEL_CODE=$(curl -s -o "${MODEL_FILE}" -w '%{http_code}' --max-time "${WRITE_TIMEOUT}" \
  -XPOST "${OPENFGA_URL}/stores/${STORE_ID}/authorization-models" \
  -H 'content-type: application/json' \
  -d @"${MODEL_JSON}") || MODEL_CODE=000
MODEL_BODY="$(cat "${MODEL_FILE}")"
rm -f "${MODEL_FILE}"
case "${MODEL_CODE}" in
  2*) : ;;
  000 | 5*)
    echo "[bootstrap] ОТКАЗ УСТАНОВКИ: запись модели прав не удалась (HTTP ${MODEL_CODE})."
    echo "[bootstrap] Сторона не ответила — повторите установку."
    exit 1 ;;
  *)
    die_misconfig "запись модели прав" "${MODEL_CODE}" "${MODEL_BODY}" ;;
esac
MODEL_ID=$(printf '%s' "${MODEL_BODY}" | jq -r '.authorization_model_id // empty')
if [ -z "${MODEL_ID}" ] || [ "${MODEL_ID}" = "null" ]; then
  echo "[bootstrap] FATAL: WriteAuthorizationModel returned no model_id"
  exit 1
fi
echo "[bootstrap] new authorization_model_id=${MODEL_ID}"

# ─── Step E: patch / create Secret openfga-model-id ─────────────────
kubectl -n "${NAMESPACE}" create secret generic "${MODEL_SECRET_NAME}" \
  --from-literal=current="${MODEL_ID}" \
  --from-literal=store_id="${STORE_ID}" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "${NAMESPACE}" annotate secret "${MODEL_SECRET_NAME}" \
  "kacho-deploy/last-applied-dsl-sha256=${DSL_SHA}" \
  "kacho-deploy/last-bootstrap-at=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --overwrite >/dev/null

# ─── Step F: also write store_id+model_id into legacy compound secret
# (kacho-iam reads this at startup; preserves E3 baseline contract).
kubectl -n "${NAMESPACE}" create secret generic "${STORE_SECRET_NAME}" \
  --from-literal=store_id="${STORE_ID}" \
  --from-literal=authorization_model_id="${MODEL_ID}" \
  --dry-run=client -o yaml | kubectl apply -f -

# ─── Step G: bump rolling-restart annotation on consumer Deployments ─
# Annotation triggers re-roll because Pods read env from Secret on start.
#
# ЭТО ЕДИНСТВЕННЫЙ ПИСАТЕЛЬ ПОЛЯ (kacho#3). Ни один чарт не вправе объявлять
# `kacho.cloud/openfga-model-id-rev` в шаблоне пода: helm 4 применяет манифесты
# на стороне сервера, где владение полем энфорсится, и второй писатель роняет
# обновление релиза конфликтом менеджеров полей. Держит это
# deploy/tests/helm/podtemplate-annotation-single-owner-test.sh.
#
# ОТКАЗ ПАТЧА РОНЯЕТ УСТАНОВКУ. Отсутствие развёртывания уже отсеяно проверкой
# выше, поэтому отказ здесь означает права задания или чужого владельца поля —
# то есть настройку. Поглощённый, он оставлял бы потребителей на прежнем
# идентификаторе модели: поды готовы, задание успешно, права не разрешаются.
REV=$(date +%s)
for DEP in ${CONSUMER_DEPLOYMENTS:-}; do
  if kubectl -n "${NAMESPACE}" get deployment "${DEP}" >/dev/null 2>&1; then
    echo "[bootstrap] bumping kacho.cloud/openfga-model-id-rev on ${DEP}..."
    PATCH_ERR="$(mktemp)"
    if kubectl -n "${NAMESPACE}" patch deployment "${DEP}" \
         --type=json \
         -p="[{\"op\":\"add\",\"path\":\"/spec/template/metadata/annotations/kacho.cloud~1openfga-model-id-rev\",\"value\":\"${REV}\"}]" \
         >/dev/null 2>"${PATCH_ERR}"; then
      rm -f "${PATCH_ERR}"
    elif kubectl -n "${NAMESPACE}" patch deployment "${DEP}" \
           -p "{\"spec\":{\"template\":{\"metadata\":{\"annotations\":{\"kacho.cloud/openfga-model-id-rev\":\"${REV}\"}}}}}" \
           >/dev/null 2>>"${PATCH_ERR}"; then
      rm -f "${PATCH_ERR}"
    else
      echo "[bootstrap] ОТКАЗ УСТАНОВКИ: отметка переката не проставлена на ${DEP}."
      echo "[bootstrap] Развёртывание существует (проверено выше), значит отказ — это"
      echo "[bootstrap] права задания на patch deployments либо чужой владелец поля."
      echo "[bootstrap] Ответ API кластера: $(tr -d '\n' <"${PATCH_ERR}" | cut -c1-400)"
      rm -f "${PATCH_ERR}"
      exit 1
    fi
  else
    echo "[bootstrap] deployment ${DEP} not present yet (likely pre-install); skipping restart-bump"
  fi
done

# ─── Step H: связи, материализующие singleton-объект кластера ───────────────
#
# fga_write <ярлык> <тело> — ТРИ исхода, четвёртого нет:
#
#   • принято (2xx)                       → успех, счётчик writes_ok;
#   • «такая связь уже есть» (400 с этим  → законный повтор, счётчик
#     кодом в теле)                          writes_duplicate;
#   • всё остальное С ОТВЕТОМ             → НАСТРОЙКА: громко и роняем.
#
# Отсутствие ответа (сеть/таймаут), 5xx и конфликт записи (409) — СБОЙ:
# ограниченный повтор, затем мягкий проход со счётчиком writes_soft_pass.
# Отказ в правах СБОЕМ не является: повтор идентичного запроса не пройдёт.
WRITE_STEP_REACHED=yes
fga_write() {
  _label="$1"
  _payload="$2"
  _attempt=1
  WRITES_ATTEMPTED=$((WRITES_ATTEMPTED + 1))
  while :; do
    _bf="$(mktemp)"
    _code=$(curl -s -o "${_bf}" -w '%{http_code}' --max-time "${WRITE_TIMEOUT}" \
      -XPOST "${OPENFGA_URL}/stores/${STORE_ID}/write" \
      -H 'content-type: application/json' \
      -d "${_payload}") || _code=000
    _body="$(cat "${_bf}")"
    rm -f "${_bf}"
    case "${_code}" in
      2*)
        WRITES_OK=$((WRITES_OK + 1))
        echo "[bootstrap] ${_label}: принято (HTTP ${_code})"
        return 0 ;;
      400)
        if printf '%s' "${_body}" | grep -q 'already exists'; then
          WRITES_DUP=$((WRITES_DUP + 1))
          echo "[bootstrap] ${_label}: такая связь уже есть — законный повтор (HTTP 400)"
          return 0
        fi
        die_misconfig "запись связи «${_label}»" "${_code}" "${_body}" ;;
      000 | 409 | 5*)
        if [ "${_attempt}" -lt "${WRITE_ATTEMPTS}" ]; then
          echo "[bootstrap] ${_label}: сторона не ответила (HTTP ${_code}), попытка ${_attempt}/${WRITE_ATTEMPTS}"
          _attempt=$((_attempt + 1))
          sleep "${WRITE_SLEEP}"
          continue
        fi
        WRITES_SOFT=$((WRITES_SOFT + 1))
        echo "[bootstrap] ВНИМАНИЕ: связь «${_label}» НЕ записана за ${WRITE_ATTEMPTS} попыток (HTTP ${_code})."
        echo "[bootstrap] Это учтено счётчиком writes_soft_pass — установка продолжается."
        return 0 ;;
      *)
        die_misconfig "запись связи «${_label}»" "${_code}" "${_body}" ;;
    esac
  done
}

echo "[bootstrap] writing singleton tuple cluster:${CLUSTER_SINGLETON_ID}#system_viewer@user:bootstrap_marker"
fga_write "cluster:${CLUSTER_SINGLETON_ID}#system_viewer@user:bootstrap_marker" "{
  \"authorization_model_id\":\"${MODEL_ID}\",
  \"writes\":{\"tuple_keys\":[{
    \"user\":\"user:bootstrap_marker\",
    \"relation\":\"system_viewer\",
    \"object\":\"cluster:${CLUSTER_SINGLETON_ID}\"
  }]}
}"

# ─── Step H2 (KAC-228): cluster viewer for ALL authenticated users ──
# FGA model: cluster.viewer = [user, user:*, service_account] ...
# Без этого tuple любой cluster-scoped reference-RPC (geo.Region/Zone,
# storage.DiskType, compute.MachineType .list/.get, required_relation=viewer
# on cluster:cluster_kacho_root) возвращал 403 для любого юзера — UI dashboard /
# NLB region-picker падали. user:* делает справочные данные читаемыми любым
# authenticated subject'ом (tenant-facing).
echo "[bootstrap] writing tuple cluster:${CLUSTER_SINGLETON_ID}#viewer@user:*"
fga_write "cluster:${CLUSTER_SINGLETON_ID}#viewer@user:*" "{
  \"authorization_model_id\":\"${MODEL_ID}\",
  \"writes\":{\"tuple_keys\":[{
    \"user\":\"user:*\",
    \"relation\":\"viewer\",
    \"object\":\"cluster:${CLUSTER_SINGLETON_ID}\"
  }]}
}"

echo "[bootstrap] OK"
