#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ПОДНЯТЫЙ СТЕНД — ЭТО КРАЙ, КОТОРЫЙ САМ СКАЗАЛ «ГОТОВ» ПОСЛЕ КОНЦА ЗАМЕНЫ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ (kacho#2901, сборка #2908, прогон 36600900186, шард edge)
#
# Фаза 3 `dev-up` заново раскатывает службу доступа и край и перезапускает базы.
# Дальше стояли только `rollout status … || true`, и сразу за ними — посевы. На
# прогоне 36600900186 это выглядело так (журнал края и события стенда):
#
#   17:01:06  старый под службы доступа снят; соединение края к её внутреннему
#             адресу рвётся, новое соединение 20 с висит на установлении;
#   17:01:13  оба `rollout status` — «successfully rolled out»: Ready пода края
#             держится по прошлой пробе, условие его пробы отстаёт на период;
#   17:01:18  собственный `/readyz` края — 503 (критичная зависимость не отвечает);
#   17:01:25  посев стартует;
#   17:01:27  подтверждение адреса отвергнуто 401: край не получил ответа о сессии
#             и по записанному решению отказал; `/readyz` — снова 503;
#   17:01:37  `/readyz` — 200. На десять секунд позже, чем было нужно.
#
# Ни «Ready пода», ни «rollout завершён» не говорят, СВЯЗАН ли край со службой
# доступа: первое — вывод из прошлой пробы, второе — о числе реплик. Говорит это
# только ответ самого края, и только полученный ПОСЛЕ того, как заменённые поды
# ушли: пока старый под жив, край отвечает «готов» через соединение, которое
# вот-вот оборвётся.
#
# ─────────────────────────────────────────────────────────────────────────────
# ДОКАЗАТЕЛЬСТВО — ИСХОД НАСТОЯЩЕЙ ЦЕЛИ ПРОТИВ ПОДСТАВНОГО kubectl, И ПРОВЯЗКА
#
#   • цель `wait-edge-ready` исполняется с подставным `kubectl` первым в PATH, по
#     режиму на каждую ситуацию; красный режим обязан назвать ПРИЧИНУ — иначе
#     «цели нет вовсе» прочиталось бы тем же красным, что «цель отказала верно»;
#   • зелёный режим обязан показать, что край действительно СПРОШЕН, — иначе
#     зелёное означало бы «спрашивать не стали»;
#   • провязка `dev-up` судится предикатом над рецептом: ожидание стоит после
#     фазы 3 и до первого посева и не погашено `|| true`. Предикат доказан на
#     трёх близнецах, изготовленных из НАСТОЯЩЕГО Makefile: без ожидания, с
#     погашенным ожиданием, с ожиданием после посева.
#
# Кластер не нужен: подставной `kubectl` отвечает сам.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$(basename "$0")"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
# Семь режимов подставного kubectl · одно утверждение «край спрошен» · провязка
# на настоящем рецепте и на трёх близнецах.
EXPECTED_ASSERTIONS=12

good() { echo "  ✓ $1"; }

command -v make >/dev/null 2>&1 || fatal "нет make — цель wait-edge-ready запускать нечем"
command -v jq >/dev/null 2>&1 || fatal "нет jq — ожидание края разбирает ответ кластера им"
require_file_present "$DEPLOY_ROOT/Makefile" "Makefile стенда"

TMP="$(mktemp -d)" || fatal "не создан временный каталог — подставной kubectl положить некуда"
trap 'rm -rf "$TMP"' EXIT

# ── Подставной kubectl. Режим — KSTUB_MODE, счётчики обращений — в KSTUB_STATE.
#
# Состояние нужно режимам, где ответ меняется со временем: замена заканчивается
# на третьем опросе, край отвечает «готов» один раз и перестаёт. Время здесь —
# номер обращения, а не часы: прогон детерминирован.
mkdir -p "$TMP/bin"
cat >"$TMP/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
args="$*"
st="${KSTUB_STATE:?}"
bump() { local n; n=$(( $(cat "$st/$1" 2>/dev/null || echo 0) + 1 )); echo "$n" >"$st/$1"; echo "$n"; }
echo "$args" >>"$st/calls"

edge='{"metadata":{"name":"api-gateway-new","labels":{"app":"api-gateway"}},"spec":{"containers":[{"name":"api-gateway","ports":[{"name":"cmux","containerPort":8080},{"name":"tls","containerPort":8443}]}]},"status":{"phase":"Running"}}'
idp='{"metadata":{"name":"kaname-new","labels":{"app":"kaname"}},"spec":{"containers":[{"name":"kaname"}]},"status":{"phase":"Running"}}'
old='{"metadata":{"name":"kaname-old","labels":{"app":"kaname"},"deletionTimestamp":"2026-09-29T17:01:06Z"},"spec":{"containers":[{"name":"kaname"}]},"status":{"phase":"Running"}}'
pods() { local IFS=,; echo "{\"items\":[$*]}"; }
ready()   { echo '{"status":"ok"}'; exit 0; }
unready() { echo "Error from server (ServiceUnavailable): the server is currently unable to handle the request" >&2; exit 1; }

mode="${KSTUB_MODE:-}"
if [ "$mode" = unreachable ]; then
  echo "E: The connection to the server localhost:8080 was refused" >&2
  exit 1
fi

case "$args" in
  *"get deployment api-gateway -o json"*)
    echo '{"spec":{"selector":{"matchLabels":{"app":"api-gateway"}}}}'; exit 0 ;;
  *"get pods -o json"*)
    n="$(bump pods)"
    case "$mode" in
      healthy|readyz-never|lone-ok) pods "$edge" "$idp" ;;
      no-edge) pods "$idp" ;;
      terminating-forever) pods "$edge" "$idp" "$old" ;;
      terminating-then-gone)
        if [ "$n" -le 2 ]; then pods "$edge" "$idp" "$old"; else : >"$st/gone"; pods "$edge" "$idp"; fi ;;
      *) echo "KSTUB_MODE не задан" >&2; exit 3 ;;
    esac
    exit 0 ;;
  *"get --raw /api/v1/namespaces/kacho/pods/api-gateway-new:8080/proxy/readyz"*)
    n="$(bump readyz)"
    case "$mode" in
      healthy|terminating-forever) ready ;;
      readyz-never) unready ;;
      lone-ok) [ "$n" -eq 1 ] && ready; unready ;;
      terminating-then-gone)
        # Пока старый под жив, край отвечает «готов» через соединение к нему;
        # после его ухода — два ответа «не готов» (соединение переустанавливается),
        # затем «готов».
        [ -e "$st/gone" ] || ready
        g="$(bump readyz-after-gone)"; [ "$g" -le 2 ] && unready; ready ;;
      *) unready ;;
    esac ;;
  *"get pods -o wide"*) echo "NAME READY STATUS"; exit 0 ;;
  *" logs "*) echo "(журнал края подставного kubectl)"; exit 0 ;;
  *) echo "подставной kubectl: неожиданный вызов: $args" >&2; exit 3 ;;
esac
STUB
chmod +x "$TMP/bin/kubectl"

run_target() { # <режим> → код возврата цели; вывод — в $TMP/out.<режим>
  local mode="$1" code
  mkdir -p "$TMP/state.$mode"
  (cd "$DEPLOY_ROOT" && PATH="$TMP/bin:$PATH" KSTUB_MODE="$mode" KSTUB_STATE="$TMP/state.$mode" \
    timeout 120 make --no-print-directory wait-edge-ready \
      EDGE_READY_ATTEMPTS=12 EDGE_READY_INTERVAL=0 EDGE_READY_STREAK=3) >"$TMP/out.$mode" 2>&1
  code=$?
  return $code
}

# probe <метка> <режим> green
# probe <метка> <режим> red <образец причины>
probe() {
  local label="$1" mode="$2" want="$3" why="${4:-}" got
  ok
  if run_target "$mode"; then got=green; else got=red; fi
  if [ "$got" != "$want" ]; then
    violation "$label — цель дала $got, а обязана $want. Вывод цели:"
    sed 's/^/      /' "$TMP/out.$mode" | tail -15
    return
  fi
  if [ "$want" = red ] && ! grep -qE "$why" "$TMP/out.$mode"; then
    violation "$label — красное, но причина не названа (ждали /$why/): красный по чужой причине не доказывает ничего. Вывод цели:"
    sed 's/^/      /' "$TMP/out.$mode" | tail -15
    return
  fi
  good "$label — $got${why:+, причина названа}"
}

echo "=== $SCRIPT: ожидание края против подставного kubectl ==="

probe "спросить не удалось (кластера/контекста нет)"          unreachable           red   'НЕ ПРОЧИТАН'
probe "подов края ноль"                                       no-edge               red   'подов края 0'
probe "заменённый под не уходит, край отвечает «готов»"       terminating-forever   red   'kaname-old'
probe "край отвечает «не готов» всегда"                       readyz-never          red   'api-gateway-new: /readyz'
probe "край ответил «готов» один раз и перестал"              lone-ok               red   'мерцание'
probe "замена закончена, край готов"                          healthy               green
probe "замена идёт, затем край переустанавливает соединение"  terminating-then-gone green

# ── Зелёное обязано быть ОТВЕТОМ КРАЯ, а не отсутствием вопроса.
ok
asked="$(grep -c 'proxy/readyz' "$TMP/state.healthy/calls" 2>/dev/null || true)"
if [ "${asked:-0}" -ge 3 ]; then
  good "край спрошен: обращений к /readyz $asked (требовалось подряд 3)"
else
  violation "зелёное без вопроса: обращений к /readyz края ${asked:-0}, а подряд требовалось 3"
fi

# ── Провязка dev-up: ожидание после фазы 3, до первого посева, не погашено.
#
# Предикат читает рецепт цели `dev-up` (строки продолжения с табуляции) и
# различает четыре исхода. Командой ожидания считается строка, НАЧИНАЮЩАЯСЯ с
# `$(MAKE)` и называющая цель, — упоминание в `echo` вызовом не является.
wiring() { # <Makefile> → печатает wired | missing | masked | order:<пояснение> | unread
  awk '
    /^dev-up:/ { inrec = 1; next }
    inrec && !/^\t/ { exit }
    inrec {
      n++
      if ($0 ~ /helm upgrade /) p3 = n
      if ($0 ~ /^\t[ \t]*\$\(MAKE\)/ && $0 ~ /[ \t]wait-edge-ready([ \t;]|$)/) { if (!w) { w = n; wl = $0 } }
      if (!s && $0 ~ /^\t[ \t]*\$\(MAKE\)/ && $0 ~ /[ \t]seed-[a-z-]+/) s = n
    }
    END {
      if (n == 0) { print "unread"; exit }
      if (!w) { print "missing"; exit }
      if (wl ~ /\|\|/) { print "masked"; exit }
      if (!p3 || w < p3) { print "order:до фазы 3 (строка " w " рецепта, фаза 3 — " p3 ")"; exit }
      if (!s || w > s) { print "order:после первого посева (строка " w " рецепта, посев — " s ")"; exit }
      print "wired"
    }' "$1"
}

MK="$DEPLOY_ROOT/Makefile"
real="$(wiring "$MK")"
[ "$real" != unread ] || fatal "рецепт dev-up не прочитан в $MK — судить провязку нечем"
ok
if [ "$real" = wired ]; then
  good "dev-up ждёт край после фазы 3 и до первого посева, отказ не погашен"
else
  violation "провязка dev-up: $real"
fi

# Близнецы — из НАСТОЯЩЕГО Makefile, по одной правке на близнеца. Сравниваются
# с его нормализованной копией: у файла нет завершающего перевода строки, awk его
# дописывает, и побайтное сравнение с оригиналом назвало бы «правкой» то, что
# правкой не является.
awk 1 "$MK" >"$TMP/mk.real"
awk '!(/^\t[ \t]*\$\(MAKE\)/ && /[ \t]wait-edge-ready([ \t;]|$)/)' "$TMP/mk.real" >"$TMP/mk.missing"
sed -E '/^\t[ \t]*\$\(MAKE\).*[ \t]wait-edge-ready/ s/wait-edge-ready;/wait-edge-ready || true;/' "$TMP/mk.real" >"$TMP/mk.masked"
awk '
  /^\t[ \t]*\$\(MAKE\)/ && /[ \t]wait-edge-ready([ \t;]|$)/ && !held { held = $0; next }
  { print }
  held && !placed && /^\t[ \t]*\$\(MAKE\)/ && /[ \t]seed-[a-z-]+/ { print held; placed = 1 }
' "$TMP/mk.real" >"$TMP/mk.order"

twin() { # <метка> <файл> <ожидаемый исход-префикс>
  local label="$1" file="$2" want="$3" got
  ok
  if cmp -s "$file" "$TMP/mk.real"; then
    violation "близнец «$label» совпал с настоящим Makefile — правка не легла, предикат не испытан"
    return
  fi
  got="$(wiring "$file")"
  case "$got" in
    "$want"*) good "близнец «$label»: предикат отвечает $got" ;;
    *) violation "близнец «$label»: предикат ответил «$got», а обязан «$want…»" ;;
  esac
}
twin "ожидание снято"             "$TMP/mk.missing" missing
twin "ожидание погашено || true"  "$TMP/mk.masked"  masked
twin "ожидание после посева"      "$TMP/mk.order"   "order:после первого посева"

echo
echo "проверок исполнено: $N из $EXPECTED_ASSERTIONS"
outcome_verdict "режимов подставного kubectl: 7; близнецов провязки: 3"
