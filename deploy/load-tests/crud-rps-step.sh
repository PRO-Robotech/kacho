#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# crud-rps-step.sh — ступенчатый замер ПРОДУКТОВЫХ операций (чтение и создание
# ресурсов) с одновременным съёмом того, ВО ЧТО они упираются и КАКАЯ ДОЛЯ их
# цены приходится на проверку доступа.
#
# ЧЕМ ОН ОТЛИЧАЕТСЯ ОТ iam-check-rps-step.sh. Тот мерит ОДИН глагол — вопрос о
# доступе. Этот мерит операцию ЦЕЛИКОМ и рядом снимает счётчики самого вопроса,
# поэтому доля «проверка доступа» получается ДЕЛЕНИЕМ ИЗМЕРЕННОГО НА
# ИЗМЕРЕННОЕ, а не оценкой:
#
#   проверок службы на операцию = Δ kacho_iam_authz_check_duration_seconds_count / Δ операций
#   времени службы на операцию  = Δ kacho_iam_authz_check_duration_seconds_sum   / Δ операций
#   проверок КРАЯ на операцию   = Δ kacho_api_gateway_authz_check_decisions_total / Δ операций
#
# ПРОВЕРОК ДВЕ, И СЧИТАТЬ НАДО ОБЕ (#772). На пути чтения по id край решает
# доступ ДО обращения к службе, поэтому «проверок на операцию», выведенное
# только из счётчика службы, занижено — и занижено молча: вторая половина
# просто не участвует в делении. Доли печатаются ПОРОЗНЬ и суммой: у каждой
# стороны свой кеш, и смешение скрыло бы, чей именно промахивается.
#
# Вторая величина — ПРОЦЕССОРНО-СЕТЕВОЕ время внутри iam, а не доля задержки
# края: между ними стоят два сетевых хопа и два кэша. Она нижняя граница цены
# проверки, и так её и надо читать.
#
# ГЕНЕРАТОР МЕРИТСЯ НАРАВНЕ С ПРЕДМЕТОМ (его насыщение снаружи неотличимо от
# насыщения продукта), а ОТКАЗ ИЗМЕРЕНИЯ ПЕЧАТАЕТ NA, а не ноль: ноль — это
# утверждение о предмете, NA — утверждение о приборе.
#
# Использование:
#   ./crud-rps-step.sh <метка> <полоса> <ступени> <длительность> [ключ-токена]
#   ./crud-rps-step.sh r1 net_get "50,100,200,400" 40s jwtProjectEditorA
set -euo pipefail

NS=${NS:-kacho}
# NODE_CTR — контейнер узла kind: через него снимается процессорное время
# участников, в чей образ нельзя войти (см. cpu_usec).
NODE_CTR=${NODE_CTR:-kacho-control-plane}
LABEL="${1:?нужна метка прогона, напр. r1}"
OP="${2:?нужна полоса, напр. net_get}"
STEPS="${3:-50,100,200,400}"
DUR="${4:-40s}"
TOKEN_KEY="${5:-jwtProjectEditorA}"
RUNNER=${RUNNER:-k6-crud-runner}
OUT="${OUT_DIR:-/tmp/claude-1000/crud-load/results}/$LABEL-$OP"
mkdir -p "$OUT"

k() { kubectl -n "$NS" "$@"; }

# cpu_usec <pod> <container> — накопленное процессорное время контейнера.
# Отказ чтения обязан быть ОТЛИЧИМ ОТ НУЛЯ (см. шапку).
cpu_usec() {
  local v
  v=$(k exec "$1" -c "$2" -- sh -c 'awk "/usage_usec/{print \$2}" /sys/fs/cgroup/cpu.stat' 2>/dev/null) || v=""
  case "$v" in ''|*[!0-9]*) v="" ;; esac
  # ЗАПАСНОЙ ПУТЬ — СНЯТИЕ С УЗЛА. Не всякий образ участника несёт ОБОЛОЧКУ, и
  # тогда `exec` в него невозможен В ПРИНЦИПЕ, а величина писалась бы NA на
  # каждой ступени каждого прогона — участник выглядел бы неизмеримым, хотя
  # измерим: его cgroup читается с узла по идентификатору контейнера.
  if [ -z "$v" ] && [ -n "${NODE_CTR:-}" ]; then
    local cid path
    cid=$(k get pod "$1" -o jsonpath="{.status.containerStatuses[?(@.name=='$2')].containerID}" 2>/dev/null | sed 's|.*://||') || cid=""
    if [ -n "$cid" ]; then
      path=$(docker exec "$NODE_CTR" sh -c "find /sys/fs/cgroup -maxdepth 6 -type d -name '*${cid}*' 2>/dev/null | head -1" 2>/dev/null) || path=""
      if [ -n "$path" ]; then
        v=$(docker exec "$NODE_CTR" sh -c "awk '/usage_usec/{print \$2}' '$path/cpu.stat'" 2>/dev/null) || v=""
      fi
    fi
  fi
  case "$v" in ''|*[!0-9]*) echo "NA" ;; *) echo "$v" ;; esac
}

# pod_of <label> — имя первого пода по метке. БЕЗ `grep -m`: скрипт идёт под
# pipefail, а ранний выход grep роняет писателя SIGPIPE, и НАЙДЕННОЕ было бы
# объявлено ненайденным тем вероятнее, чем раньше встретилось совпадение.
# Вердикт о перезапуске/смене состава — общий с прибором проверки доступа.
# shellcheck source=lib/restart-verdict.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/restart-verdict.sh"

pod_of() { k get pod -l "$1" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true; }
iam_pods() { k get pod -l app.kubernetes.io/name=kaname -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'; }
vpc_pods() { k get pod -l app=vpc -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'; }

# authz_counters — ПРИБОР РАЗЛОЖЕНИЯ ЦЕНЫ. Суммарно по всем репликам iam.
authz_counters() {
  local p total_c total_s
  total_c=0; total_s=0
  for p in $(iam_pods); do
    local m
    m=$(k exec "$p" -c kaname -- sh -c 'wget -qO- http://127.0.0.1:9095/metrics 2>/dev/null' 2>/dev/null) || m=""
    if [ -z "$m" ]; then echo "checks=NA sum=NA"; return 0; fi
    local c s
    c=$(echo "$m" | awk '/^kacho_iam_authz_check_duration_seconds_count/{t+=$2} END{printf "%.0f", t+0}')
    s=$(echo "$m" | awk '/^kacho_iam_authz_check_duration_seconds_sum/{t+=$2}   END{printf "%.6f", t+0}')
    total_c=$(awk -v a="$total_c" -v b="$c" 'BEGIN{printf "%.0f", a+b}')
    total_s=$(awk -v a="$total_s" -v b="$s" 'BEGIN{printf "%.6f", a+b}')
  done
  echo "checks=$total_c sum=$total_s"
}

# edge_authz_counters — ВТОРАЯ ПОЛОВИНА той же цены (#772).
#
# На пути чтения по id проверок ДВЕ: одна на крае (он резолвит область и решает
# доступ ДО обращения к службе), вторая в самой службе. Прибор снимал только
# вторую, поэтому всякое «проверок на операцию», выведенное отсюда, было
# занижено — и занижено МОЛЧА: половина просто не участвовала в делении.
#
# Серия края одноимённа по форме (`kacho_api_gateway_authz_*` против
# `kacho_iam_authz_*`), поэтому складывать их законно, а держать порознь —
# обязательно: доля попаданий кеша у них своя, и смешение скрыло бы, чей именно
# кеш промахивается.
#
# Реплика края одна, но перебор всё равно по списку: масштабирование края —
# вопрос настройки, а не построения, и жёсткое «первый под» разошлось бы с
# деревом молча.
edge_authz_counters() {
  local p total_d total_h total_m
  total_d=0; total_h=0; total_m=0
  for p in $(k get pod -l app=api-gateway -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null); do
    local m
    m=$(k exec "$p" -- sh -c 'wget -qO- http://127.0.0.1:9095/metrics 2>/dev/null' 2>/dev/null) || m=""
    if [ -z "$m" ]; then echo "edge_decisions=NA edge_hit=NA edge_miss=NA"; return 0; fi
    local d h ms
    d=$(echo "$m"  | awk '/^kacho_api_gateway_authz_check_decisions_total/{t+=$2} END{printf "%.0f", t+0}')
    h=$(echo "$m"  | awk '/^kacho_api_gateway_authz_cache_total\{result="hit"\}/{t+=$2} END{printf "%.0f", t+0}')
    ms=$(echo "$m" | awk '/^kacho_api_gateway_authz_cache_total\{result="miss"\}/{t+=$2} END{printf "%.0f", t+0}')
    total_d=$(awk -v a="$total_d" -v b="$d"  'BEGIN{printf "%.0f", a+b}')
    total_h=$(awk -v a="$total_h" -v b="$h"  'BEGIN{printf "%.0f", a+b}')
    total_m=$(awk -v a="$total_m" -v b="$ms" 'BEGIN{printf "%.0f", a+b}')
  done
  echo "edge_decisions=$total_d edge_hit=$total_h edge_miss=$total_m"
}

pool_stats() {
  local p
  for p in $(iam_pods); do
    k exec "$p" -c kaname -- sh -c \
      'wget -qO- http://127.0.0.1:9095/metrics 2>/dev/null | grep -E "^kacho_iam_db_pool_" || true' 2>/dev/null | sed "s|^|$p |"
  done
}

# Внешнего движка отношений и его базы в снимке больше нет: они сняты вместе с
# посадкой (S6 эпика #747). Цена решения о доступе теперь целиком лежит на
# `iam:*` и `pgiam`, которые в снимке уже есть, — то есть разложение цены не
# потеряло слагаемого, оно переехало в уже измеряемого участника.
snapshot() {
  local f="$1" gw
  gw=$(pod_of app=api-gateway)
  {
    echo "ts_ns=$(date +%s%N)"
    for p in $(iam_pods); do echo "iam:$p=$(cpu_usec "$p" kaname)"; done
    for p in $(vpc_pods); do echo "vpc:$p=$(cpu_usec "$p" vpc)"; done
    echo "gateway:$gw=$(cpu_usec "$gw" api-gateway)"
    echo "pgiam=$(cpu_usec kacho-umbrella-pg-iam-0 postgresql)"
    echo "pgvpc=$(cpu_usec kacho-umbrella-pg-vpc-0 postgresql)"
    echo "k6=$(cpu_usec $RUNNER k6)"
    echo "authz $(authz_counters)"
    echo "edge_authz $(edge_authz_counters)"
  } > "$f"
}

# restarts — участник, перезапустившийся в ходе прогона, делает ступень «не
# выполнившейся»: её число нельзя истолковывать ни как зелёное, ни как красное.
restarts() {
  k get pod -o jsonpath='{range .items[*]}{.metadata.name}={.status.containerStatuses[0].restartCount}{"\n"}{end}' 2>/dev/null \
    | grep -E 'kaname|pg-iam|pg-vpc|^vpc-|api-gateway' || true
}

VPC_REPL=$(k get deploy vpc -o jsonpath='{.spec.replicas}')
IAM_REPL=$(k get deploy kaname -o jsonpath='{.spec.replicas}')
echo "=== прогон '$LABEL' · полоса=$OP · vpc реплик=$VPC_REPL · iam реплик=$IAM_REPL · ступени=$STEPS · $DUR ==="
{
  echo "label=$LABEL op=$OP steps=$STEPS duration=$DUR token_key=$TOKEN_KEY"
  echo "vpc_replicas=$VPC_REPL iam_replicas=$IAM_REPL"
  k get pod -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.status.containerStatuses[0].imageID}{"\n"}{end}' 2>/dev/null | grep -E 'kaname|^vpc-|api-gateway' || true
} > "$OUT/meta.txt"

# settle — ДОЖДАТЬСЯ ТИШИНЫ ПЕРЕД ЗАМЕРОМ.
#
# Прогрев защищает от холодного старта СВОЕЙ полосы и НЕ защищает от чужого
# хвоста: создание уезжает в воркер, и материализация прав продолжается ещё
# долго после того, как предыдущая полоса отчиталась. Наблюдалось вживую —
# полоса создания сети стартовала при хранилище прав на 3.11 ядра, оставшихся
# от соседней полосы, и дала на 5 запросах в секунду p50 275 мс, то есть число
# о СОСЕДЕ, а не о себе.
#
# ЗА ЧЬЕЙ ТИШИНОЙ СЛЕДИМ. Прежде — за подом внешнего движка отношений. Движок
# снят вместе с посадкой (S6 эпика #747), и хвост материализации теперь целиком
# исполняет сама служба прав, поэтому проба переведена на её под. Оставить её
# нацеленной на снятый под было нельзя: она молча возвращала бы «проверить
# нечем» на каждом прогоне — форма ожидания без ожидания.
#
# Ждём, пока служба прав не успокоится, но НЕ БЕСКОНЕЧНО: не дождались —
# говорим об этом вслух и пишем метку, чтобы ступень читалась с оговоркой, а
# не как чистая.
settle() {
  local limit=${SETTLE_CORES:-0.35} maxwait=${SETTLE_MAXWAIT:-240} quiet=0 waited=0 cid path a b cores
  cid=$(k get pod -l app.kubernetes.io/name=kaname -o jsonpath='{.items[0].status.containerStatuses[0].containerID}' 2>/dev/null | sed 's|.*://||') || cid=""
  if [ -z "$cid" ] || [ -z "${NODE_CTR:-}" ]; then echo "  (тишину проверить нечем — пропускаю)"; return 0; fi
  path=$(docker exec "$NODE_CTR" sh -c "find /sys/fs/cgroup -maxdepth 6 -type d -name '*${cid}*' 2>/dev/null | head -1" 2>/dev/null) || path=""
  [ -z "$path" ] && { echo "  (cgroup службы прав не найден — пропускаю)"; return 0; }
  echo "--- жду тишины службы прав (порог ${limit} ядра) ---"
  while [ "$waited" -lt "$maxwait" ]; do
    a=$(docker exec "$NODE_CTR" sh -c "awk '/usage_usec/{print \$2}' '$path/cpu.stat'" 2>/dev/null) || a=""
    sleep 5; waited=$(( waited + 5 ))
    b=$(docker exec "$NODE_CTR" sh -c "awk '/usage_usec/{print \$2}' '$path/cpu.stat'" 2>/dev/null) || b=""
    [ -z "$a" ] || [ -z "$b" ] && continue
    cores=$(awk -v a="$a" -v b="$b" 'BEGIN{printf "%.2f",(b-a)/5e6}')
    # Две подряд тихие пробы, а не одна: одиночная попадает в паузу между
    # партиями дренажа и объявляет тишину там, где её нет.
    if awk -v c="$cores" -v l="$limit" 'BEGIN{exit !(c<l)}'; then
      quiet=$(( quiet + 1 )); [ "$quiet" -ge 2 ] && { echo "    тихо (${cores} ядра), ждали ${waited} с"; return 0; }
    else quiet=0; fi
  done
  echo "    !! ТИШИНЫ НЕ ДОЖДАЛСЯ за ${maxwait} с (последняя проба ${cores} ядра)"
  echo "стенд не успокоился за ${maxwait} с перед прогоном" > "$OUT/run.notsettled"
  return 0
}
settle

restarts > "$OUT/run.restarts.baseline"

# ПРОГРЕВ, РЕЗУЛЬТАТ КОТОРОГО ВЫБРАСЫВАЕТСЯ: первая ступень после переката мерит
# холодный старт (пулы пусты, кэши проектов и зон не наполнены, планировщик базы
# не видел этих запросов) — и ложится этот артефакт на САМУЮ НИЗКУЮ ступень, то
# есть портит именно ту точку, от которой отсчитывают запас.
if [ "${WARMUP:-1}" = "1" ]; then
  echo "--- прогрев (результат выбрасывается) ---"
  k exec "$RUNNER" -- k6 run --quiet -e OP="$OP" -e TOKEN_KEY="$TOKEN_KEY" \
     -e TARGET_RPS="${WARMUP_RPS:-20}" -e DURATION="${WARMUP_DUR:-20s}" -e RUN_ID="warm$$" \
     ${NET_POOL_PATH:+-e NET_POOL_PATH=$NET_POOL_PATH} \
     /scripts/resource_ops.js > "$OUT/warmup.k6.log" 2>&1 || true
  sleep 5
fi

breaches=0
for rate in ${STEPS//,/ }; do
  tag="rate${rate}"
  echo "--- ступень $rate rps ---"
  restarts       > "$OUT/$tag.restarts.before"
  pool_stats     > "$OUT/$tag.pool.before" 2>/dev/null || true
  snapshot       "$OUT/$tag.cpu.before"

  set +e
  k exec "$RUNNER" -- k6 run --quiet \
      -e OP="$OP" -e TOKEN_KEY="$TOKEN_KEY" -e TARGET_RPS="$rate" -e DURATION="$DUR" \
      -e RUN_ID="${LABEL}${rate}" -e MAX_VUS=$(( rate < 64 ? 128 : rate * 4 )) \
      ${NET_POOL_PATH:+-e NET_POOL_PATH=$NET_POOL_PATH} \
      ${PAGE_SIZE:+-e PAGE_SIZE=$PAGE_SIZE} \
      --summary-export="/out/$tag.json" /scripts/resource_ops.js \
      > "$OUT/$tag.k6.log" 2>&1
  k6rc=$?
  set -e

  snapshot   "$OUT/$tag.cpu.after"
  pool_stats > "$OUT/$tag.pool.after" 2>/dev/null || true
  restarts   > "$OUT/$tag.restarts.after"

  # Сверяем ДВАЖДЫ: с началом ступени И с началом прогона — иначе перезапуск,
  # случившийся МЕЖДУ ступенями, не помечается ничем, а числа соседних ступеней
  # выглядят правдоподобно.
  # Вердикт берётся из общей библиотеки — той же, что у прибора проверки доступа.
  # Сверка целыми файлами читала как перезапуск ЛЮБОЕ расхождение, включая смену
  # состава подов, то есть браковала замер масштабирования by construction.
  # Разбор и границы — в lib/restart-verdict.sh; здесь второй копии не заводится.
  grew_step=$(restart_grew "$OUT/$tag.restarts.before" "$OUT/$tag.restarts.after")
  grew_run=$(restart_grew "$OUT/run.restarts.baseline" "$OUT/$tag.restarts.after")
  if [ -n "$grew_step" ]; then
    echo "INVALID: участник перезапустился в ходе ступени: $grew_step" > "$OUT/$tag.invalid"
    echo "    !! НЕДЕЙСТВИТЕЛЬНО: перезапустился $grew_step — число этой ступени не читать"
  elif [ -n "$grew_run" ]; then
    echo "INVALID: участник перезапустился с начала прогона: $grew_run" > "$OUT/$tag.invalid"
    echo "    !! НЕДЕЙСТВИТЕЛЬНО: с начала прогона перезапустился $grew_run"
  fi
  if composition_changed "$OUT/run.restarts.baseline" "$OUT/$tag.restarts.after"; then
    {
      echo "состав участников изменился с начала прогона"
      echo "было:";  cut -d= -f1 "$OUT/run.restarts.baseline"
      echo "стало:"; cut -d= -f1 "$OUT/$tag.restarts.after"
    } > "$OUT/$tag.composition-changed"
    echo "    ** СОСТАВ ИЗМЕНИЛСЯ с начала прогона (подробности в $tag.composition-changed)."
    echo "       Ваше масштабирование — читайте число; если нет, под мог быть заменён"
    echo "       после падения, и тогда числу верить нельзя."
  fi

  k exec "$RUNNER" -- cat "/out/$tag.json" > "$OUT/$tag.summary.json" 2>/dev/null || echo '{}' > "$OUT/$tag.summary.json"
  echo "k6_exit=$k6rc" > "$OUT/$tag.rc"
  # Легенда кода — ТОЛЬКО при ненулевом исходе. Безусловная строка «исход k6=0
  # (бюджет нарушен ⇒ 99)» читается как утверждение о нарушении; на такой же
  # строке в соседнем приборе споткнулся её собственный автор.
  if [ "$k6rc" -eq 0 ]; then
    echo "    исход k6=0 — бюджет прогона выдержан"
  elif [ "$k6rc" -eq 99 ]; then
    echo "    исход k6=99 — БЮДЖЕТ НАРУШЕН"
  else
    echo "    исход k6=$k6rc — прогон не состоялся (не вердикт о бюджете)"
  fi

  if [ "$k6rc" != "0" ]; then
    breaches=$(( breaches + 1 ))
    if [ "$breaches" -ge 2 ]; then echo "    (лестница остановлена: бюджет нарушен дважды подряд)"; break; fi
  else
    breaches=0
  fi
done
echo "=== готово: $OUT ==="
