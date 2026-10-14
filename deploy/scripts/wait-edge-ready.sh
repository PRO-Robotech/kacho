#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# wait-edge-ready.sh — КРАЙ ГОТОВ, КОГДА ЭТО СКАЗАЛ САМ КРАЙ, ПОСЛЕ КОНЦА ЗАМЕНЫ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ (kacho#2901, прогон 36600900186, шард edge)
#
# Фаза 3 `dev-up` заново раскатывает службу доступа и край и перезапускает базы.
# `rollout status` отвечает «выкачено», как только новые реплики доступны, а
# условие Ready пода края — вывод из ПРОШЛОЙ пробы готовности (период 10 с, три
# неудачи до снятия). Ни то, ни другое не говорит, связан ли край со службой
# доступа СЕЙЧАС. На названном прогоне старый под службы доступа ушёл в 17:01:06,
# соединение края к её внутреннему адресу 20 с висело на установлении, оба
# `rollout status` в 17:01:13 ответили «выкачено», собственный `/readyz` края в
# 17:01:18 и 17:01:27 отвечал 503, посев стартовал в 17:01:25 — и первое же
# обращение браузерной полосы получило 401: край по записанному решению отказывает,
# когда служба доступа не ответила о сессии.
#
# ЧТО СЧИТАЕТСЯ ГОТОВНОСТЬЮ. Две вещи, в этом порядке, на одном и том же опросе:
#   1. в пространстве имён НЕТ подов в завершении — замена закончилась. Пока
#      заменённый под жив, край отвечает «готов» через соединение к нему, и это
#      соединение оборвётся после ответа;
#   2. КАЖДЫЙ работающий под края отвечает на свой `/readyz` 200. Этот ответ край
#      даёт, только когда отвечают его критичные зависимости (служба доступа, оба
#      её адреса), — то есть это ровно то условие, которое нужно посеву.
# Обе вещи обязаны держаться STREAK опросов подряд: один ответ «готов» между
# двумя «не готов» — мерцание, а не готовность.
#
# ЧИТАЕТСЯ ЧЕРЕЗ ПРОКСИ API-СЕРВЕРА (`get --raw …/pods/<под>:<порт>/proxy/readyz`):
# без проброса порта, без исполнения в контейнере (в образе края оболочки нет) и с
# того же адреса узла, с которого ходит проба готовности кубелета. Порт берётся из
# спецификации пода по имени `cmux` — тот, на который смотрит его проба готовности.
#
# ИСХОДЫ: 0 — край готов (печатается перепись: подов края, опросов подряд, попытка);
#         1 — край не готов за отведённые попытки: условие прогона НЕ создано,
#             печатается последняя причина и диагностика;
#         2 — вызван неверно либо нет jq.
# «Спросить не удалось» и «подов края ноль» готовностью не являются НИКОГДА.
#
# ДОРОГА ВОПРОСА — EDGE_READY_VIA (kacho#3102). Умолчание `proxy` — прокси
# API-сервера, как выше. На управляемом кластере прокси API-сервера до адресов
# подов не доходит (замер на внешнем кластере: `504 Gateway Timeout` у пода
# РАБОЧЕГО стенда так же, как у тестового), и вопрос там задаётся `port-forward`:
# проброс к тому же порту `cmux` того же пода через кубелет. Предмет вопроса и
# исходы те же; меняется только дорога, и выбирает её вызывающий, знающий кластер.
#
# Проба: deploy/tests/helm/edge-ready-before-seed-test.sh.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

usage() {
  echo "usage: $0 <namespace> <deployment> <attempts> <interval-seconds> <streak>" >&2
  exit 2
}
[ "$#" -eq 5 ] || usage
ns="$1" deploy="$2" attempts="$3" interval="$4" streak_need="$5"
case "$attempts" in '' | *[!0-9]*) usage ;; esac
case "$streak_need" in '' | *[!0-9]*) usage ;; esac
case "$interval" in '' | *[!0-9.]*) usage ;; esac
[ "$attempts" -ge 1 ] && [ "$streak_need" -ge 1 ] || usage
command -v jq >/dev/null 2>&1 || {
  echo "wait-edge-ready: нет jq — ответ кластера разбирать нечем" >&2
  exit 2
}

kc() { kubectl -n "$ns" "$@" --request-timeout=10s; }

VIA="${EDGE_READY_VIA:-proxy}"
case "$VIA" in proxy | port-forward) ;; *) echo "wait-edge-ready: EDGE_READY_VIA=$VIA — допустимы proxy и port-forward" >&2; exit 2 ;; esac

# ask_ready ПОД ПОРТ — ответ /readyz пода; код 0 — ответ 200.
ask_ready() {
  if [ "$VIA" = proxy ]; then
    kc get --raw "/api/v1/namespaces/$ns/pods/$1:$2/proxy/readyz" 2>&1
    return
  fi
  local log pf lp code rc=1
  log="$(mktemp)"
  kubectl -n "$ns" port-forward "pod/$1" ":$2" >"$log" 2>&1 &
  pf=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    lp="$(sed -nE 's/^Forwarding from 127\.0\.0\.1:([0-9]+) .*/\1/p' "$log" | head -1)"
    [ -n "$lp" ] && break
    sleep 0.5
  done
  if [ -z "$lp" ]; then
    first_line "$(cat "$log")"
  else
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 8 "http://127.0.0.1:$lp/readyz" || true)"
    echo "HTTP $code"
    [ "$code" = 200 ] && rc=0
  fi
  kill "$pf" 2>/dev/null; wait "$pf" 2>/dev/null
  rm -f "$log"
  return "$rc"
}
first_line() { printf '%s\n' "$1" | sed -n '1p'; }

# opros → 0, если на этом опросе край готов; иначе причина — в $why.
why=""
edge_names=""
opros() {
  local pods dep sel terminating edge name port answer bad=""
  if ! pods="$(kc get pods -o json 2>&1)" || ! jq -e '.items | type == "array"' >/dev/null 2>&1 <<<"$pods"; then
    why="список подов ns $ns НЕ ПРОЧИТАН: $(first_line "$pods")"
    return 1
  fi
  if ! dep="$(kc get deployment "$deploy" -o json 2>&1)" ||
    ! sel="$(jq -ce '.spec.selector.matchLabels | select(type == "object" and length > 0)' 2>/dev/null <<<"$dep")"; then
    why="селектор Deployment $deploy НЕ ПРОЧИТАН: $(first_line "$dep")"
    return 1
  fi
  terminating="$(jq -r '[.items[] | select(.metadata.deletionTimestamp != null) | .metadata.name] | join(" ")' <<<"$pods")"
  if [ -n "$terminating" ]; then
    why="замена не закончена — поды в завершении: $terminating"
    return 1
  fi
  edge="$(jq -r --argjson sel "$sel" '
    .items[]
    | select(.status.phase == "Running")
    | select(.metadata.labels as $l | $sel | to_entries | all(.value == ($l[.key] // null)))
    | [.metadata.name, ([.spec.containers[].ports[]? | select(.name == "cmux") | .containerPort][0] // "" | tostring)]
    | @tsv' <<<"$pods")"
  if [ -z "$edge" ]; then
    why="подов края 0 (Deployment $deploy, селектор $sel): готовность спрашивать не у кого"
    return 1
  fi
  edge_names=""
  while IFS=$'\t' read -r name port; do
    edge_names="${edge_names:+$edge_names }$name"
    if [ -z "$port" ]; then
      bad="${bad:+$bad; }$name: у пода нет порта cmux — спрашивать /readyz некуда"
      continue
    fi
    if ! answer="$(ask_ready "$name" "$port")"; then
      bad="${bad:+$bad; }$name: /readyz не 200 — $(first_line "$answer")"
    fi
  done <<<"$edge"
  if [ -n "$bad" ]; then
    why="$bad"
    return 1
  fi
  return 0
}

echo "=== край: ждём, пока замена закончится и край САМ ответит «готов» ($streak_need опроса подряд, попыток до $attempts) ==="
streak=0
said=""
for ((i = 1; i <= attempts; i++)); do
  if opros; then
    streak=$((streak + 1))
    if [ "$streak" -ge "$streak_need" ]; then
      echo "=== край готов: подов в завершении 0; подов края $(wc -w <<<"$edge_names") ($edge_names) ответили /readyz 200 подряд $streak раз(а); попытка $i из $attempts ==="
      exit 0
    fi
  else
    if [ "$streak" -gt 0 ]; then
      why="$why (после $streak ответа «готов» подряд — мерцание, а не готовность)"
    fi
    streak=0
    if [ "$why" != "$said" ]; then
      echo "  … $why"
      said="$why"
    fi
  fi
  [ "$i" -lt "$attempts" ] && sleep "$interval"
done

echo "!!! край НЕ ГОТОВ за $attempts попыток: требовалось $streak_need опроса подряд с ответом 200, достигнуто $streak."
echo "    Последняя причина: $why"
echo "    Это условие прогона, которое НЕ создано: посев и пробы против такого стенда"
echo "    вердикта о продукте не дают."
echo "=== поды ns $ns ==="
kubectl -n "$ns" get pods -o wide 2>&1 || true
for name in $edge_names; do
  echo "=== журнал $name (последние 40 строк) ==="
  kubectl -n "$ns" logs "$name" --tail=40 2>&1 || true
done
exit 1
