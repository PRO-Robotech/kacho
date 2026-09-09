#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ПРОБА: решение прогонщика о КЛИЕНТСКОМ ЛИСТЕ собственного REST-фронта.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Внутренний REST-фронт службы на боевой посадке требует ПРОВЕРЕННОГО клиентского
# листа: рукопожатие без него не состоится ВОВСЕ — запрос не уходит, и прогонщик
# видит «ответа нет» вместо отказа по существу. Значит харнесс обязан решать три
# разных вопроса, и путать их дорого:
#
#   ребро ВЗАИМНОЕ, лист есть   → лист подать, адрес инъектировать;
#   ребро ОДНОСТОРОННЕЕ         → лист НЕ подавать (поданный, он ничего не
#                                 доказывает), адрес инъектировать;
#   ребро ВЗАИМНОЕ, листа нет   → адрес НЕ инъектировать. Инъектировав его, мы
#                                 получили бы вердикт о ПРОДУКТЕ на предмете,
#                                 которого харнесс не создал; не инъектировав —
#                                 кейс скажет третьим исходом сам
#                                 (gen.py::require_env_url).
#
# РЕШЕНИЙ ТРИ, А ПОСАДОК ЧЕТЫРЕ, и это не описка: одностороннее ребро приходит в
# ДВУХ формах — режим односторонний при поднятом транспорте и транспорта нет
# вовсе. Вторая форма отвечает «нет» BY CONSTRUCTION (сертификата не бывает там,
# где нет рукопожатия), и не прогнав её, мы не отличили бы верное решение от
# случайного совпадения с ним.
#
# ПРОБА ГОНЯЕТ НАСТОЯЩУЮ ФУНКЦИЮ, А НЕ ЕЁ ПЕРЕСКАЗ. Блок собственных фронтов
# вырезается из файла прогонщика и исполняется под ПОДСТАВНЫМ kubectl. Пересказ
# доказывал бы согласие пробы с самой собой.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ЭТОГО НЕ ДЕЛАЕТ СОСЕДНИЙ ГЕЙТ
#
# `assert-own-front-address-is-read.py` судит ФОРМУ: адрес читается у посадки, а
# решение о листе не вычитывается из журнала процесса. Он не запускает ничего и
# потому не может сказать, ЧТО прогонщик сделает на конкретной посадке. Здесь
# наоборот: форма не судится вовсе, судится ИСХОД на четырёх посадках.
#
# ─────────────────────────────────────────────────────────────────────────────
# «НОЛЬ НАХОДОК» ОТЛИЧИМО ОТ «НОЛЬ ПРОЧИТАННОГО»
#
# Перепись печатается всегда. Прогонщик, у которого блок не вырезался, — НАХОДКА,
# а не молчание: значит форма записи пробе неизвестна, и всё, что ею записано,
# стоит вне наблюдения. Пустой обход — отказ.
#
# Самопроверка: `--self-test` (синтетический прогонщик, решающий по СТАРОМУ
# правилу, обязан быть найден; законный близнец обязан молчать).
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DEFAULT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ROOT="$ROOT_DEFAULT"
SELF_TEST=0
while [ $# -gt 0 ]; do
  case "$1" in
    --root) ROOT="$2"; shift 2 ;;
    --self-test) SELF_TEST=1; shift ;;
    *) echo "ОТКАЗ: неизвестный аргумент «$1»" >&2; exit 1 ;;
  esac
done

# WORK — ВНЕ ЛЮБОГО РЕПОЗИТОРИЯ: подставной инструмент и вырезанные блоки не
# должны попадаться обходчикам дерева, а инструмент, заводящий своё дерево, не
# должен находить чужой индекс обходом вверх.
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

RUNNERS_SEEN=0
BLOCKS_CUT=0
POSTURES_RUN=0
FINDINGS=()

# ─── ПОДСТАВНОЙ kubectl ──────────────────────────────────────────────────────
# Отвечает ровно тем, чем отвечает посадка: разобранными объектами и наличием
# секрета. Посадка задаётся двумя переменными, и каждая меняет РОВНО один факт.
write_stub_kubectl() {  # <tls: true|false> <режим или пусто> <секрет: yes|no>
  local tls="$1" mode="$2" secret="$3"
  mkdir -p "$WORK/bin"
  {
    echo '#!/usr/bin/env bash'
    echo 'args="$*"'
    echo 'case "$args" in'
    echo '  *"-o json"*)'
    echo '    cat <<J'
    echo '{"items":[{"kind":"Service","metadata":{"name":"kaname-internal"},'
    echo '  "spec":{"ports":[{"name":"http-rest-int","port":9099}]}},'
    echo ' {"kind":"Deployment","metadata":{"name":"kaname"},'
    echo '  "spec":{"template":{"spec":{"containers":[{"name":"kaname","env":['
    printf '     {"name":"KANAME_INTERNALREST_SERVER_MTLS_ENABLE","value":"%s"}' "$tls"
    [ -n "$mode" ] && printf ',\n     {"name":"KANAME_INTERNALREST_SERVER_MTLS_CLIENTAUTHMODE","value":"%s"}' "$mode"
    echo ']}]}}}}]}'
    echo 'J'
    echo '    exit 0 ;;'
    if [ "$secret" = yes ]; then
      echo '  *"get secret"*"-o jsonpath"*) printf eA== ; exit 0 ;;'
      echo '  *"get secret"*) exit 0 ;;'
    else
      echo '  *"get secret"*) echo "Error from server (NotFound)" >&2; exit 1 ;;'
    fi
    echo '  *port-forward*) sleep 5 & exit 0 ;;'
    echo 'esac'
    echo 'exit 0'
  } > "$WORK/bin/kubectl"
  chmod +x "$WORK/bin/kubectl"
}

# ─── ВЫРЕЗАНИЕ НАСТОЯЩЕГО БЛОКА ──────────────────────────────────────────────
cut_block() {  # <файл прогонщика> <куда>
  awk '/^OWN_FRONT_ENV_ARGS=\(\)/{f=1} /^own_front_forward public/{f=0} f' "$1" > "$2"
  grep -q 'own_front_forward()' "$2"
}

# ─── ОДНА ПОСАДКА ────────────────────────────────────────────────────────────
# Печатает «<лист ДА|НЕТ> <адрес ДА|НЕТ>».
run_posture() {  # <блок> <производитель> <tls> <режим> <секрет>
  write_stub_kubectl "$3" "$4" "$5"
  cat > "$WORK/drive.sh" <<'DRV'
set -uo pipefail
PATH="$WORK/bin:$PATH"
NS=kacho
PF_PIDS=(); PF_WHAT=(); TMP_DIRS=()
. "$BLOCK"
own_front_forward internal 19099 ownInternalRestBaseUrl >/dev/null 2>&1
# Массива у прогонщика может не быть ВОВСЕ — это законный исход «лист не подан»,
# и он обязан читаться словом «НЕТ», а не пустотой: пустая находка не называет,
# что именно произошло, и её первым делом спишут на поломку самой пробы.
declare -p OWN_FRONT_TLS_ARGS >/dev/null 2>&1 || OWN_FRONT_TLS_ARGS=()
declare -p OWN_FRONT_ENV_ARGS >/dev/null 2>&1 || OWN_FRONT_ENV_ARGS=()
printf '%s %s\n' \
  "$( [ "${#OWN_FRONT_TLS_ARGS[@]}" -gt 0 ] && echo ДА || echo НЕТ )" \
  "$( [ "${#OWN_FRONT_ENV_ARGS[@]}" -gt 0 ] && echo ДА || echo НЕТ )"
DRV
  WORK="$WORK" BLOCK="$1" SCRIPT_DIR="$2" bash "$WORK/drive.sh" 2>/dev/null
}

# ─── ЧЕТЫРЕ ПОСАДКИ, каждая — ОДИН изменённый факт против соседней ───────────
# посадка|tls|режим|секрет|ожидаемый лист|ожидаемый адрес|за что отвечает
POSTURES=(
  "ребро ВЗАИМНОЕ, лист в посадке есть|true|mutual|yes|ДА|ДА|лист подаётся там, где рукопожатие его требует"
  "ребро ОДНОСТОРОННЕЕ (тот же фронт под транспортом)|true|server-tls-only|yes|НЕТ|ДА|лист НЕ подаётся там, где он ничего не доказывает"
  "ребро ВЗАИМНОЕ, листа в посадке нет|true|mutual|no|НЕТ|НЕТ|адрес НЕ инъектируется: это «условие не создано», а не вердикт о продукте"
  "фронт открытым текстом|false|mutual|yes|НЕТ|ДА|рукопожатия нет вовсе — сертификата не бывает"
)

audit_runner() {  # <относительный путь> <абсолютный путь> <каталог производителя>
  local rel="$1" abs="$2" prod="$3" blk="$WORK/blk.sh"
  RUNNERS_SEEN=$((RUNNERS_SEEN + 1))
  if ! cut_block "$abs" "$blk"; then
    FINDINGS+=("$rel: блок собственных фронтов не вырезался — форма записи пробе НЕИЗВЕСТНА, и решение о листе стоит вне наблюдения")
    return
  fi
  BLOCKS_CUT=$((BLOCKS_CUT + 1))
  local row name tls mode secret want_leaf want_addr why got
  for row in "${POSTURES[@]}"; do
    IFS='|' read -r name tls mode secret want_leaf want_addr why <<<"$row"
    POSTURES_RUN=$((POSTURES_RUN + 1))
    got="$(run_posture "$blk" "$prod" "$tls" "$mode" "$secret")"
    if [ "$got" != "$want_leaf $want_addr" ]; then
      FINDINGS+=("$rel · $name: ждали «лист $want_leaf, адрес $want_addr», получили «${got:-пусто}» — $why")
    fi
  done
}

# ─── САМОПРОВЕРКА ────────────────────────────────────────────────────────────
self_test() {
  local fails=0 tmp="$WORK/st" ; mkdir -p "$tmp/deploy/scripts"
  cp "$ROOT_DEFAULT/deploy/scripts/own-rest-front-address.py" "$tmp/deploy/scripts/"

  # ИНЪЕКЦИЯ: прогонщик решает по СТАРОМУ правилу — «есть секрет ⇒ подаём».
  # Отличается от законного близнеца РОВНО одним фактом: чем принимается решение.
  cat > "$tmp/deploy/scripts/newman-old.sh" <<'OLD'
OWN_FRONT_ENV_ARGS=()
OWN_FRONT_TLS_ARGS=()
own_front_forward() {
  local front="$1" local_port="$2" var="$3" addr scheme port svc
  addr="$(python3 "$SCRIPT_DIR/own-rest-front-address.py" "$front" --namespace "$NS")" || return 0
  scheme="$(echo "$addr" | cut -d'|' -f1)"; port="$(echo "$addr" | cut -d'|' -f2)"
  svc="$(echo "$addr" | cut -d'|' -f3)"
  kubectl -n "$NS" port-forward "svc/$svc" "$local_port:$port" >/dev/null 2>&1 &
  PF_PIDS+=($!)
  OWN_FRONT_ENV_ARGS+=(--env-var "$var=$scheme://127.0.0.1:$local_port")
  if kubectl -n "$NS" get secret api-gateway-client-tls >/dev/null 2>&1; then
    OWN_FRONT_TLS_ARGS=(--ssl-client-cert /x --ssl-client-key /y)
  fi
}
own_front_forward public 1 a
OLD
  # ЗАКОННЫЙ БЛИЗНЕЦ: настоящий прогонщик дерева.
  cp "$ROOT_DEFAULT/deploy/scripts/newman-parallel.sh" "$tmp/deploy/scripts/"
  # СЛЕПОТА: файл прогонщика без блока вовсе.
  echo '# прогонщик без собственных фронтов' > "$tmp/deploy/scripts/newman-blind.sh"

  local out rc
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$tmp" 2>&1)"; rc=$?
  _st() { if [ "$2" = 1 ]; then echo "  ok   $1"; else echo "  FAIL $1: $3"; fails=$((fails + 1)); fi; }

  echo "ось 1 — старое правило («есть секрет ⇒ подаём») находится"
  _st "инъекция даёт находку" \
      "$(grep -qc 'newman-old.sh' <<<"$out" && echo 1 || echo 0)" "$out"
  _st "и находка называет ПОСАДКУ, на которой правило неверно" \
      "$(grep -q 'ОДНОСТОРОННЕЕ' <<<"$out" && echo 1 || echo 0)" "$out"
  _st "код возврата ненулевой" "$([ "$rc" != 0 ] && echo 1 || echo 0)" "rc=$rc"

  echo "ось 2 — законный близнец (прогонщик дерева) МОЛЧИТ"
  _st "о нём находок нет" \
      "$(grep -q 'newman-parallel.sh' <<<"$out" && echo 0 || echo 1)" "$out"

  echo "ось 3 — слепота пробы объявляется находкой, а не молчанием"
  _st "прогонщик без блока — находка" \
      "$(grep -q 'newman-blind.sh' <<<"$out" && echo 1 || echo 0)" "$out"

  echo "ось 4 — перепись печатается и непуста"
  _st "объём осмотренного назван" \
      "$(grep -q 'перепись:' <<<"$out" && echo 1 || echo 0)" "$out"

  echo "ось 5 — на дереве без прогонщиков вердикт БЕСПРЕДМЕТЕН, а не зелен"
  mkdir -p "$WORK/empty/deploy/scripts"
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$WORK/empty" 2>&1)"; rc=$?
  _st "пустой обход — отказ" "$([ "$rc" != 0 ] && echo 1 || echo 0)" "rc=$rc / $out"

  echo
  if [ "$fails" -gt 0 ]; then
    echo "ОТКАЗ: провалено утверждений $fails из 7" >&2; return 1
  fi
  echo "ЧИСТО: 7 утверждений — проба способна упасть на старом правиле, смолчать на дереве и объявить свою слепоту"
  return 0
}

[ "$SELF_TEST" = 1 ] && { self_test; exit $?; }

for f in "$ROOT"/deploy/scripts/newman-*.sh; do
  [ -e "$f" ] || continue
  audit_runner "deploy/scripts/$(basename "$f")" "$f" "$ROOT/deploy/scripts"
done

echo "перепись: прогонщиков осмотрено $RUNNERS_SEEN · блоков вырезано $BLOCKS_CUT · посадок прогнано $POSTURES_RUN"
if [ "$RUNNERS_SEEN" -eq 0 ]; then
  echo "ОТКАЗ: прогонщиков не прочитано — вердикт беспредметен" >&2; exit 1
fi
if [ "${#FINDINGS[@]}" -gt 0 ]; then
  echo "НАХОДКИ (${#FINDINGS[@]}):" >&2
  for x in "${FINDINGS[@]}"; do echo "  $x" >&2; done
  exit 1
fi
echo "ЧИСТО: решение о клиентском листе верно на каждой из ${#POSTURES[@]} посадок у каждого прогонщика"
