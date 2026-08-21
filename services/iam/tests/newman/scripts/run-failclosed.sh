#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# run-failclosed.sh — СОЗДАЁТ условие, которое нужно коллекции authz-failclosed,
# гоняет её и возвращает стенд как было.
#
# ЗАЧЕМ ОН ЕСТЬ. Кейс «шлюз отказывает, когда источник вердикта недоступно» ссылался
# на этот файл в собственном комментарии, а файла не существовало нигде в дереве.
# Условие создать было нечем, поэтому кейс держали ИСКЛЮЧЕНИЕМ в гейте — то есть
# инвариант «нет ответа о правах ⇒ отказ, никогда не 200» не проверялся ни разу,
# и выглядело это как зелёная суита. Кейсу, которому нужна недоступная
# зависимость, полагается собственная волна, создающая это условие, а не
# исключение (testing.md).
#
# ЧТО ДЕЛАЕТ:
#   1. находит рабочую нагрузку источника вердикта (по имени НЕ гадает — ищет);
#   2. сворачивает её в ноль и ДОЖИДАЕТСЯ, что подов не осталось;
#   3. выжидает кэш решений шлюза (LRU, TTL 5с) — иначе ответ придёт из кэша и
#      кейс проверит кэш вместо отказа;
#   4. гоняет коллекцию, пишет out/authz-failclosed.json|.cli|.rc;
#   5. ВОЗВРАЩАЕТ прежнее число реплик и ждёт готовности, даже если прогон упал
#      (trap): следующий шаг работает на исправном стенде.
#
# ЧЕГО НЕ ДЕЛАЕТ: не прощает. Не нашли нагрузку, не свернулась, не поднялась —
# скрипт падает и отчёта не остаётся, поэтому гейт докладывает
# `authz-failclosed(no-report)` и роняет прогон. «Не смогли создать условие» —
# это открытый долг, а не зелёная суита.
#
# Запуск: cwd = services/iam/tests/newman
#   [SETUP_NS=kacho] [GW_PORT=18080] ./scripts/run-failclosed.sh

set -euo pipefail
cd "$(dirname "$0")/.."

NS="${SETUP_NS:-kacho}"
DELAY="${DELAY:-100}"
STEM="authz-failclosed"
COL="collections/${STEM}.postman_collection.json"
ENV_FILE="environments/local.postman_environment.json"

# Кэш решений слоя авторизации шлюза — LRU с TTL 5с (internal/middleware/authz.go).
# Ждём с запасом: ответ из кэша означал бы, что кейс проверил кэш, а не отказ.
CACHE_SETTLE="${FAILCLOSED_CACHE_SETTLE:-12}"
SCALE_TIMEOUT="${FAILCLOSED_SCALE_TIMEOUT:-90}"

for tool in kubectl newman jq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FATAL: '$tool' not found in PATH" >&2; exit 1; }
done
[ -f "$COL" ] || { echo "FATAL: нет коллекции $COL — запусти scripts/gen.py" >&2; exit 1; }
[ -f "$ENV_FILE" ] || { echo "FATAL: нет env $ENV_FILE — его пишет fixture-seed" >&2; exit 1; }

# ─── 1. найти рабочую нагрузку, дающую ВЕРДИКТ ────────────────────────────────
# Предмет волны — «источник вердикта недоступен», и он пережил смену формы.
# Прежде вердикт давал внешний движок прав, и сворачивалась ЕГО нагрузка. Теперь
# вердикт вычисляет сам iam в своей базе, поэтому сворачивается iam: для края и
# сервисов это ровно то же условие — спросить некого. Утверждения кейсов не
# менялись (503 / gRPC 14, и НИКОГДА 200 с пустым списком): контракт
# fail-closed — свойство продукта, а не движка.
#
# Имя ИЩЕТСЯ, а не пишется: оно зависит от имени релиза чарта, и хардкод молча
# промахнулся бы при переименовании — скрипт «отработал бы», не создав условия.
# Исключены явно: край консоли (`ui-iam` — статика, вердикта не даёт) и
# бутстрап-задания.
mapfile -t CANDIDATES < <(
  kubectl -n "$NS" get deploy -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null \
    | grep -E '(^|-)iam$' | grep -v -- 'ui-' | grep -v -- 'bootstrap' || true
)
if [ "${#CANDIDATES[@]}" -ne 1 ]; then
  echo "FATAL: в namespace '$NS' ожидалась ровно одна Deployment источника вердикта, найдено ${#CANDIDATES[@]}: ${CANDIDATES[*]:-(нет)}" >&2
  echo "       условие для authz-failclosed не создано — отчёта не будет, и гейт это назовёт" >&2
  exit 1
fi
DEPLOY="${CANDIDATES[0]}"
WAS="$(kubectl -n "$NS" get deploy "$DEPLOY" -o jsonpath='{.spec.replicas}')"
[ -n "$WAS" ] || WAS=1
echo "[failclosed] источник вердикта: deploy/$DEPLOY (реплик сейчас: $WAS)"

restore() {
  local rc=$?
  echo "[failclosed] возвращаю deploy/$DEPLOY → replicas=$WAS"
  kubectl -n "$NS" scale deploy "$DEPLOY" --replicas="$WAS" >/dev/null 2>&1 || true
  # Ждём готовности: следующий шаг прогона работает на этом же стенде, и
  # оставить его без источника вердикта значило бы обменять один честный красный на
  # каскад чужих.
  kubectl -n "$NS" rollout status deploy/"$DEPLOY" --timeout="${SCALE_TIMEOUT}s" || {
    echo "FATAL: источник вердикта не вернулось в строй за ${SCALE_TIMEOUT}s — стенд повреждён, дальнейшие суиты недостоверны" >&2
    exit 1
  }
  # Кэш решений держит отказы ровно так же, как разрешения: без выжидания
  # следующая суита получила бы 503 из кэша уже на исправном стенде.
  sleep "$CACHE_SETTLE"
  echo "[failclosed] стенд восстановлен"
  exit "$rc"
}
trap restore EXIT

# ─── 2. свернуть в ноль и дождаться, что подов не осталось ───────────────────
echo "[failclosed] сворачиваю deploy/$DEPLOY → replicas=0"
kubectl -n "$NS" scale deploy "$DEPLOY" --replicas=0
deadline=$((SECONDS + SCALE_TIMEOUT))
while :; do
  live="$(kubectl -n "$NS" get deploy "$DEPLOY" -o jsonpath='{.status.replicas}' 2>/dev/null || echo "")"
  [ -z "$live" ] && live=0
  [ "$live" -eq 0 ] && break
  if [ "$SECONDS" -ge "$deadline" ]; then
    echo "FATAL: источник вердикта не свернулось за ${SCALE_TIMEOUT}s (осталось реплик: $live) — условие не создано" >&2
    exit 1
  fi
  sleep 2
done
echo "[failclosed] реплик не осталось; жду ${CACHE_SETTLE}s (кэш решений шлюза)"
sleep "$CACHE_SETTLE"

# ─── 3. прогон ровно одной коллекции ────────────────────────────────────────
mkdir -p out
set +e
newman run "$COL" \
  -e "$ENV_FILE" \
  --delay-request "$DELAY" \
  --reporters cli,json \
  --reporter-json-export "out/${STEM}.json" \
  ${EXTRA_NEWMAN_ARGS:-} 2>&1 | tee "out/${STEM}.cli"
rc=${PIPESTATUS[0]}
set -e
echo "$rc" > "out/${STEM}.rc"

if [ "$rc" -ne 0 ]; then
  echo "[failclosed] newman вернул $rc — разбор в out/${STEM}.cli; гейт вынесет вердикт по отчёту" >&2
fi
exit "$rc"
