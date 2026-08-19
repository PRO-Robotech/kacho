#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# iam-check-rps-step.sh — ступенчатый замер пропускной способности
# InternalIAMService.Check с одновременным съёмом того, ВО ЧТО он упирается.
#
# Число без ответа «что упёрлось» бесполезно: непонятно, что чинить. Поэтому
# каждая ступень снимает не только задержку, но и процессорное время КАЖДОГО
# участника пути (служба прав · хранилище прав · их базы · сам генератор
# нагрузки), состояние пула соединений службы и число соединений в базе.
#
# ГЕНЕРАТОР НАГРУЗКИ МЕРИТСЯ НАРАВНЕ С ПРЕДМЕТОМ — и это не педантизм: k6 живёт
# на том же узле, и его собственное насыщение выглядит СНАРУЖИ как насыщение
# службы. Ступень, на которой генератор упёрся в свой предел, обязана быть
# отличима от ступени, на которой упёрся продукт.
#
# Использование:
#   ./iam-check-rps-step.sh <метка> <ступени> <длительность> [повторов]
#   ./iam-check-rps-step.sh r1 "100,200,400,800,1600" 60s 1
set -euo pipefail

NS=kacho
LABEL="${1:?нужна метка прогона, напр. r1}"
STEPS="${2:-100,200,400,800,1600}"
DUR="${3:-60s}"
REPEATS="${4:-1}"
# REP_START — с какого номера нумеровать повторы. Длинный прогон приходится
# резать на куски (оболочка убивает фоновые задачи), а метки повторов обязаны
# не сталкиваться, иначе второй кусок затрёт первый.
REP_START="${REP_START:-1}"
ALLOW_RATIO="${ALLOW_RATIO:-0.9}"
OUT="${OUT_DIR:-/tmp/claude-1000/rps-work/results}/$LABEL"
mkdir -p "$OUT"

k() { kubectl -n "$NS" "$@"; }

# cpu_usec <pod> <container> — накопленное процессорное время контейнера.
#
# ОТКАЗ ЧТЕНИЯ ОБЯЗАН БЫТЬ ОТЛИЧИМ ОТ НУЛЯ. Прежняя редакция глушила отказ через
# `|| echo 0`, и «не измерено» становилось неотличимо от «не потратило». Это не
# теоретическая опасность: образ хранилища прав распространяется БЕЗ ОБОЛОЧКИ,
# поэтому `exec` в него невозможен в принципе, и снимок писал ему ровно ноль на
# каждой ступени каждого прогона. В разборе это выглядело как участник, который
# под нагрузкой не тратит процессор вовсе, — то есть как факт, а не как пробел.
# Теперь пробел называет себя сам, и его видно в таблице как NA.
cpu_usec() {
  local v
  v=$(k exec "$1" -c "$2" -- sh -c 'awk "/usage_usec/{print \$2}" /sys/fs/cgroup/cpu.stat' 2>/dev/null) || v=""
  case "$v" in
    ''|*[!0-9]*) echo "NA" ;;
    *)           echo "$v" ;;
  esac
}

iam_pods()  { k get pod -l app.kubernetes.io/name=kacho-iam -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'; }
openfga_pod(){ k get pod -o name | grep -m1 'openfga-[0-9a-f]' | cut -d/ -f2; }

# snapshot <файл> — состояние всех участников в один момент.
snapshot() {
  local f="$1" of; of=$(openfga_pod)
  {
    echo "ts_ns=$(date +%s%N)"
    for p in $(iam_pods); do echo "iam:$p=$(cpu_usec "$p" kacho-iam)"; done
    echo "openfga:$of=$(cpu_usec "$of" openfga)"
    echo "pgiam=$(cpu_usec kacho-umbrella-pg-iam-0 postgresql)"
    echo "pgfga=$(cpu_usec kacho-umbrella-pg-openfga-0 postgresql)"
    echo "k6=$(cpu_usec k6-iam-runner k6)"
  } > "$f"
}

# pool_stats — показатели пула соединений службы прав, со ВСЕХ реплик.
# Читаются с внутреннего порта метрик, суммарно по репликам.
pool_stats() {
  for p in $(iam_pods); do
    k exec "$p" -c kacho-iam -- sh -c \
      'wget -qO- http://127.0.0.1:9095/metrics 2>/dev/null | grep -E "^kacho_iam_db_pool_|^kacho_iam_authz_check_duration_seconds_count|^kacho_iam_shadow" || true' \
      2>/dev/null | sed "s|^|$p |"
  done
}

# restarts — счётчики перезапусков участников пути. Ступень, на которой хоть
# один из них перезапустился, НЕ является ни зелёной, ни красной: это «не
# выполнилось», и её число нельзя истолковывать. Наблюдалось вживую: база службы
# прав под насыщением перестала отвечать на собственную пробу живости и была
# убита, а счётчик процессорного времени обнулился — в разборе это выглядело как
# ОТРИЦАТЕЛЬНАЯ загрузка процессора, то есть как явная бессмыслица. Не будь
# счётчика перезапусков, тот же прогон дал бы правдоподобное, но ложное число.
restarts() {
  k get pod -o jsonpath='{range .items[*]}{.metadata.name}={.status.containerStatuses[0].restartCount}{"\n"}{end}' 2>/dev/null \
    | grep -E 'kacho-iam|pg-iam|openfga' || true
}

pg_conns() {
  local pw; pw=$(k get secret kacho-umbrella-pg-iam -o jsonpath='{.data.postgres-password}' 2>/dev/null | base64 -d || true)
  k exec kacho-umbrella-pg-iam-0 -c postgresql -- sh -c \
    "PGPASSWORD='$pw' psql -U postgres -d kacho_iam -t -A -F'|' -c \
     \"select count(*), count(*) filter (where state='active'), count(*) filter (where wait_event_type='Lock') from pg_stat_activity where datname='kacho_iam';\"" 2>/dev/null || echo "NA"
}

REPLICAS=$(k get deploy kacho-iam -o jsonpath='{.spec.replicas}')
echo "=== прогон '$LABEL': реплик службы прав=$REPLICAS · ступени=$STEPS · длительность=$DUR · повторов=$REPEATS ==="
echo "replicas=$REPLICAS" > "$OUT/meta.txt"
echo "steps=$STEPS duration=$DUR repeats=$REPEATS allow_ratio=$ALLOW_RATIO" >> "$OUT/meta.txt"
k get pod -l app.kubernetes.io/name=kacho-iam -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.status.containerStatuses[0].imageID}{"\n"}{end}' >> "$OUT/meta.txt"

# ПРОГРЕВ, РЕЗУЛЬТАТ КОТОРОГО ВЫБРАСЫВАЕТСЯ. Первая ступень после переката или
# смены числа реплик меряет не продукт, а холодный старт: пулы пусты, соединения
# к хранилищу прав ещё не установлены, планировщик базы не видел этих запросов.
# Наблюдалось прямо здесь: сразу после масштабирования ступень 100 rps дала p99
# 374 мс и 0.77% отказов, а следующая за ней ступень 200 rps — 4 мс и ноль. Без
# явного прогрева этот артефакт неотличим от находки, и он ложится на САМУЮ
# низкую ступень, то есть портит именно ту точку, от которой отсчитывают запас.
restarts > "$OUT/run.restarts.baseline"

if [ "${WARMUP:-1}" = "1" ]; then
  echo "--- прогрев (результат выбрасывается) ---"
  k exec k6-iam-runner -- k6 run --quiet -e TARGET_RPS=200 -e DURATION=30s      -e ALLOW_RATIO="$ALLOW_RATIO" -e MAX_VUS=600 /scripts/internal_check.js      > "$OUT/warmup.k6.log" 2>&1 || true
  sleep 5
fi

for rep in $(seq "$REP_START" $(( REP_START + REPEATS - 1 ))); do
breaches=0
for rate in ${STEPS//,/ }; do
  tag="rate${rate}_rep${rep}"
  echo "--- ступень $rate rps (повтор $rep) ---"
  restarts > "$OUT/$tag.restarts.before"
  pool_stats > "$OUT/$tag.pool.before" 2>/dev/null || true
  snapshot "$OUT/$tag.cpu.before"
  pg_conns > "$OUT/$tag.pg.before"

  set +e
  k exec k6-iam-runner -- k6 run --quiet \
      -e TARGET_RPS="$rate" -e DURATION="$DUR" -e ALLOW_RATIO="$ALLOW_RATIO" \
      -e MAX_VUS=$(( rate < 64 ? 64 : (rate * 3) )) \
      --summary-export="/out/$tag.json" /scripts/internal_check.js \
      > "$OUT/$tag.k6.log" 2>&1
  k6rc=$?
  set -e

  snapshot "$OUT/$tag.cpu.after"
  pg_conns > "$OUT/$tag.pg.after"
  pool_stats > "$OUT/$tag.pool.after" 2>/dev/null || true
  restarts > "$OUT/$tag.restarts.after"
  # Сверяем ДВАЖДЫ: с началом ступени и с началом ПРОГОНА. Только первая сверка
  # была здесь раньше, и она пропускает перезапуск, случившийся МЕЖДУ ступенями:
  # «до» и «после» такой ступени совпадают между собой, поэтому метка не
  # ставится, а числа соседних ступеней выглядят правдоподобно. Наблюдалось
  # вживую — база службы прав была убита (код 137) в ходе серии, и ни одного
  # файла .invalid не появилось.
  if ! diff -q "$OUT/$tag.restarts.before" "$OUT/$tag.restarts.after" >/dev/null 2>&1; then
    echo "INVALID: участник пути перезапустился в ходе ступени" > "$OUT/$tag.invalid"
    echo "    !! НЕДЕЙСТВИТЕЛЬНО: перезапуск участника — число этой ступени не читать"
  elif ! diff -q "$OUT/run.restarts.baseline" "$OUT/$tag.restarts.after" >/dev/null 2>&1; then
    echo "INVALID: участник пути перезапустился с начала прогона" > "$OUT/$tag.invalid"
    echo "    !! НЕДЕЙСТВИТЕЛЬНО: перезапуск между ступенями — прогон не сравним с началом"
  fi
  k exec k6-iam-runner -- cat "/out/$tag.json" > "$OUT/$tag.summary.json" 2>/dev/null || echo '{}' > "$OUT/$tag.summary.json"
  echo "k6_exit=$k6rc" > "$OUT/$tag.rc"
  echo "    исход k6=$k6rc (порог задержки нарушен ⇒ 99)"
  # Ранняя остановка: точка насыщения найдена, дальше лестница только загоняет
  # базу в отказ (и, как выяснилось, под пробу живости). Две подряд нарушенные
  # ступени — достаточное свидетельство, третья ничего не добавляет.
  if [ "$k6rc" != "0" ]; then
    breaches=$(( breaches + 1 ))
    if [ "$breaches" -ge 2 ]; then
      echo "    (лестница остановлена: бюджет нарушен дважды подряд)"
      break
    fi
  else
    breaches=0
  fi
done
done
echo "=== готово: $OUT ==="
