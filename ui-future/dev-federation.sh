#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Локальный федеративный стенд консоли на Linux/WSL.
#
# Почему отдельный скрипт, а не dev-federation.ps1: тот исполняется только
# PowerShell'ом и называет ЧЕТЫРЕ удалённых модуля, тогда как в дереве их
# ВОСЕМЬ. Перечень здесь ВЫВОДИТСЯ из host/vite.config.ts (см. remotes()),
# а не выписывается: рукописный список уже разошёлся с деревом однажды.
#
# Хост исполняется в режиме разработки, удалённые модули — собранными
# артефактами: @originjs/vite-plugin-federation читает remoteEntry.js, а он
# появляется только после сборки. Поэтому у каждого модуля два процесса —
# пересборка по изменению исходника и раздача dist.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOGS="${KACHO_UI_LOGS:-$ROOT/.dev-logs}"
HOST_PORT=5174

mkdir -p "$LOGS"

# Перечень удалённых модулей выводится из объявления хоста, а не из памяти.
mapfile -t REMOTES < <(
  grep -oE '^\s+[a-z]+: process\.env\.KACHO_[A-Z]+_REMOTE' "$ROOT/host/vite.config.ts" |
    sed -E 's/^\s+([a-z]+):.*/\1/'
)

if ((${#REMOTES[@]} == 0)); then
  echo "ОТКАЗ: в host/vite.config.ts не найдено ни одного удалённого модуля." >&2
  echo "Перечень выводится из объявления хоста; ноль — это отказ, а не пустой стенд." >&2
  exit 2
fi

echo "Удалённых модулей объявлено: ${#REMOTES[@]} — ${REMOTES[*]}"

PIDS=()
cleanup() {
  echo
  echo "Останавливаю ${#PIDS[@]} процессов стенда..."
  for pid in "${PIDS[@]:-}"; do
    [[ -n "${pid:-}" ]] && kill -- "-$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT INT TERM

port_of() { # dashboard -> 4175, vpc -> 4176, ...
  node -e "const p=require('$ROOT/$1/package.json');const m=/--port\s+(\d+)/.exec(p.scripts.preview||'');if(!m){console.error('нет порта у preview в $1');process.exit(1)}console.log(m[1])"
}

free_port() {
  local port=$1
  local holder
  holder=$(ss -ltnp 2>/dev/null | grep -oP "127\.0\.0\.1:$port\b.*pid=\K\d+" | head -1 || true)
  [[ -z "$holder" ]] && holder=$(ss -ltnp 2>/dev/null | grep -oP "0\.0\.0\.0:$port\b.*pid=\K\d+" | head -1 || true)
  if [[ -n "$holder" ]]; then
    local cwd
    cwd=$(readlink "/proc/$holder/cwd" 2>/dev/null || echo "?")
    # Освобождаем только СВОИ порты: процесс, чей рабочий каталог лежит в этом
    # дереве, либо чей каталог удалён (осиротевшая рабочая копия).
    if [[ "$cwd" == "$ROOT"* || "$cwd" == *"(deleted)" ]]; then
      echo "  освобождаю порт $port (процесс $holder, $cwd)"
      kill "$holder" 2>/dev/null || true
      sleep 0.5
    else
      echo "ОТКАЗ: порт $port занят посторонним процессом $holder ($cwd)." >&2
      echo "Это чужое состояние — останови его сам либо смени порт." >&2
      exit 1
    fi
  fi
}

spawn() { # имя команда...
  local name=$1; shift
  setsid "$@" > "$LOGS/$name.log" 2>&1 < /dev/null &
  PIDS+=("$!")
}

echo
echo "== Сборка удалённых модулей =="
build_pids=()
for m in "${REMOTES[@]}"; do
  ( cd "$ROOT/$m" && npm run build ) > "$LOGS/build-$m.log" 2>&1 &
  build_pids+=("$!")
done
failed=0
for i in "${!build_pids[@]}"; do
  if ! wait "${build_pids[$i]}"; then
    echo "  ОТКАЗ сборки: ${REMOTES[$i]} — см. $LOGS/build-${REMOTES[$i]}.log" >&2
    failed=1
  else
    echo "  собран: ${REMOTES[$i]}"
  fi
done
((failed)) && exit 1

echo
echo "== Проверка артефактов =="
for m in "${REMOTES[@]}"; do
  entry="$ROOT/$m/dist/assets/remoteEntry.js"
  if [[ ! -f "$entry" ]]; then
    echo "ОТКАЗ: $m собрался, но $entry отсутствует." >&2
    exit 1
  fi
  echo "  $m: $(stat -c%s "$entry") байт"
done

echo
echo "== Запуск =="
free_port "$HOST_PORT"
for m in "${REMOTES[@]}"; do
  p=$(port_of "$m")
  free_port "$p"
  spawn "watch-$m"   env -C "$ROOT/$m" npm run dev:remote:watch
  spawn "preview-$m" env -C "$ROOT/$m" npm run preview
  echo "  $m: пересборка + раздача на :$p"
done
spawn "host" env -C "$ROOT/host" npm run dev

echo
echo "== Ожидание готовности =="
deadline=$((SECONDS + 90))
for m in "${REMOTES[@]}"; do
  p=$(port_of "$m")
  until curl -sf -o /dev/null --max-time 2 "http://127.0.0.1:$p/assets/remoteEntry.js"; do
    ((SECONDS > deadline)) && { echo "ОТКАЗ: $m не поднялся на :$p — см. $LOGS/preview-$m.log" >&2; exit 1; }
    sleep 1
  done
  echo "  готов: $m (:$p)"
done
until curl -sf -o /dev/null --max-time 2 "http://127.0.0.1:$HOST_PORT/"; do
  ((SECONDS > deadline)) && { echo "ОТКАЗ: хост не поднялся на :$HOST_PORT — см. $LOGS/host.log" >&2; exit 1; }
  sleep 1
done

echo
echo "Консоль: http://localhost:$HOST_PORT"
echo "Журналы: $LOGS"
echo "Стенд kind достигается через ./proxies.sh (шлюз :8080, kratos :4433, ui :4300, hydra :4444)."
echo "Ctrl+C — остановить всё."
wait
