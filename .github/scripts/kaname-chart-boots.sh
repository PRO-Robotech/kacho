#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# kaname-chart-boots.sh — ПОСТАВЛЯЕМЫЙ ЧАРТ СЛУЖБЫ ДОСТУПА ДЕЙСТВИТЕЛЬНО
# ПОДНИМАЕТСЯ: установка и готовность, а не рендер.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ (#2472)
#
# Единственный артефакт, который получает ставящий Kaname отдельно, — каталог
# `services/iam/deploy`. До этого гейта он был доказан РЕНДЕРОМ и стражем
# старта, и оба вердикта области подъёма не покрывают: страж рендера сам
# объявляет, что пода он не поднимает, а страж старта отказывает уже в кластере
# — то есть ровно там, куда рендер не доезжает.
#
# `helm install` + rollout-ready в дереве БЫЛ, но поднимал ЗОНТИЧНЫЙ стенд, а не
# этот чарт. Перенести его вердикт сюда нельзя: чартов с именем службы два, и
# наборы их ключей пересекаются меньше чем наполовину (замер 2026-09-10 по
# `release/kaname-tail`: листовых ключей у поставляемого 42, у подчарта зонта
# 128, общих 16; шаблонов 4 против 13). Зелёный зонт о поставке не утверждает
# НИЧЕГО.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЛОС ДВЕ, И У КАЖДОЙ СВОЙ ВЕРДИКТ СО СВОЕЙ ОБЛАСТЬЮ
#
#   cluster (умолчание)  — `helm install` в kind + rollout-ready. Судит ВСЁ:
#                          объекты, тома, ссылки на секреты, пробы готовности,
#                          миграции, старт процесса.
#   process              — та же цепочка профилей, но исполняется РЕНДЕР её
#                          настроек: миграции и процесс поднимаются
#                          контейнерами, готовность спрашивается у той самой
#                          двери, которую называет проба чарта.
#
# Вторая полоса НЕ ЗАМЕНЯЕТ первую и не смеет читаться как её вердикт: она не
# судит ни томов, ни ссылок на секреты, ни того, что kubelet доходит до пробы.
# Она отвечает на другой вопрос — «поднимается ли ПРОЦЕСС от этого профиля», —
# и отвечает там, где кластера в этой среде получить нельзя (предел inotify,
# отсутствие прав). Обе полосы печатают область своего вердикта отдельной
# строкой: зелёное, прочитанное шире сделанного, и есть та самая маска.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ОБЪЕКТЫ ВОКРУГ ЗАВОДИТ СКРИПТ, А НЕ ЧАРТ
#
# Чарт создаёт ровно три объекта и НЕ заводит ни базы, ни секретов, ни образа —
# это его объявленное свойство, а не упущение. Значит роль скрипта здесь — роль
# ОПЕРАТОРА: он заводит то, что в чужом облаке заводит ставящий, и ровно по тем
# координатам, которые называет боевой профиль. Всё, что подставляется сверх
# профиля, печатается: подстановка, не названная вслух, превращает вердикт о
# поставке в вердикт о нашем стенде.
#
# УДОСТОВЕРЯЮЩИХ ЦЕНТРОВ ДВА, и это не украшение стенда. Якорь ПОСТАВЩИКА
# личности и круг клиентских листов службы — разные величины (#2487), и
# совпадение их центров есть свойство НАШЕЙ установки, а не свойство мира.
# Разными их делает именно этот скрипт: профиль, склеивающий их одной
# координатой, здесь не поднимется.
#
# ─────────────────────────────────────────────────────────────────────────────
# ИСХОДОВ ЧЕТЫРЕ, И ТРЕТИЙ НЕ ЕСТЬ ВЕРДИКТ
#
#   0  поднялся, посадка подтверждена;
#   1  НАХОДКА — не поднялся либо поднялся не в той посадке;
#   2  условие не создано (нет инструмента, нет демона, кластер не заводится) —
#      «не выполнилось» в зачёт «прошло» не идёт;
#   3  прогон недействителен (кончилось место) — вердикта нет ни у одного шага,
#      включая прошедшие.
#
# Локально:      bash .github/scripts/kaname-chart-boots.sh --without-cluster
# В конвейере:   bash .github/scripts/kaname-chart-boots.sh
# Самопроверка:  bash .github/scripts/kaname-chart-boots.sh --self-test
# Инъекция:      KANAME_CHART_BOOTS_INJECT=drop-trust-domain bash … --without-cluster
set -uo pipefail

EXIT_OK=0
EXIT_FINDING=1
EXIT_VOID=2
EXIT_INVALID=3

TAG="${KANAME_CHART_BOOTS_TAG:-kaname-chart-boots}"
CLUSTER="${KANAME_CHART_BOOTS_CLUSTER:-$TAG}"
NS="${KANAME_CHART_BOOTS_NS:-kaname-boot}"
RELEASE="${KANAME_CHART_BOOTS_RELEASE:-kaname}"
IMAGE="${KANAME_CHART_BOOTS_IMAGE:-$TAG:local}"
KEEP="${KANAME_CHART_BOOTS_KEEP:-0}"
ROLLOUT_TIMEOUT="${KANAME_CHART_BOOTS_ROLLOUT_TIMEOUT:-300s}"
PG_IMAGE="${KANAME_CHART_BOOTS_PG_IMAGE:-mirror.gcr.io/library/postgres:16-alpine}"
# Инъекция — ИМЯ ОДНОГО подменяемого факта. Пусто — не подменяется ничего.
INJECT="${KANAME_CHART_BOOTS_INJECT:-}"
LANE="cluster"

say() { printf '%s\n' "$*"; }
hdr() { printf '\n=== %s\n' "$*"; }

# disk_floor_ok — место, без которого прогон недействителен, а не красен.
disk_floor_ok() {
  local free
  free="$(df -BG --output=avail / 2>/dev/null | tail -1 | tr -dc '0-9')"
  [ -n "$free" ] || return 0
  [ "$free" -ge "${KANAME_CHART_BOOTS_DISK_FLOOR_GB:-6}" ]
}

# ── самопроверка: классификатор различает исходы, а вход установки исполним ──
#
# Она НЕ поднимает кластера и идёт за секунды. Её предмет — способность скрипта
# отличить «не выполнилось» от красного (неспособный ошибиться классификатор
# отвечал бы всегда одно) и ИСПОЛНИМОСТЬ той командной строки, которой полоса
# ставит чарт: набор `--set` проверяется РЕНДЕРОМ той же цепочки профилей.
self_test() {
  local ok=0 cases=0 void=0 self
  # ПУТЬ К СЕБЕ РАЗРЕШАЕТСЯ В АБСОЛЮТНЫЙ. Прогонщик самопроверок
  # (deploy/scripts/run-gate-self-tests.sh) зовёт скрипт путём ОТ КОРНЯ дерева, и
  # относительный `$0`, переданный в подоболочку, зависел бы от её рабочего
  # каталога — то есть самопроверка была бы функцией места вызова.
  self="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"

  check() { # check <подпись> <ожидаемый код> <фактический код>
    cases=$((cases + 1))
    if [ "$2" = "$3" ]; then say "  ОК  $1 — код $3"; else ok=1; say "  ПРОВАЛ  $1 — код $3, ждали $2"; fi
  }
  # undecidable — случай, у которого НЕТ ПРЕДМЕТА в этой среде.
  #
  # Он не идёт ни в «прошло», ни в «провалено»: самопроверка, зачитавшая
  # неразрешимый случай за зелёный, есть та самая маска, против которой она и
  # написана. Итог тогда — код 2, и прогонщик читает его как «условие не
  # создано», а не как вердикт о дереве.
  undecidable() { void=1; say "  БЕЗ ПРЕДМЕТА  $1"; }

  local rc bin bash_abs
  bash_abs="$(command -v bash)"
  # ОДИН подменяемый факт: круг доступных инструментов. Оболочка остаётся
  # достижимой намеренно — иначе подменялось бы два факта сразу, и красное
  # приходило бы от соседа.
  #
  # Случай РАЗРЕШИМ ВСЕГДА: круг инструментов подменяет сама самопроверка, и от
  # состава задания он не зависит.
  bin="$(mktemp -d)"; ln -s "$bash_abs" "$bin/bash"
  ( PATH="$bin" "$bash_abs" "$self" >/dev/null 2>&1 ); rc=$?
  rm -rf "$bin"
  check "инструментов нет → условие не создано, а не находка" "$EXIT_VOID" "$rc"

  # МЕСТО проверяется ПЕРВОЙ ступенью скрипта, поэтому этот случай тоже разрешим
  # всегда — ни один инструмент для него не нужен.
  ( KANAME_CHART_BOOTS_DISK_FLOOR_GB=999999999 "$bash_abs" "$self" >/dev/null 2>&1 ); rc=$?
  check "места нет → прогон недействителен, а не находка" "$EXIT_INVALID" "$rc"

  # ЗАКОННЫЙ БЛИЗНЕЦ обоих коротких выходов: при исполнимых предпосылках скрипт
  # коротко НЕ выходит, а идёт ставить. Проверяется сухим прогоном полосы
  # ПРОЦЕССА: её круг инструментов уже, и демона контейнеров сухой прогон не
  # касается вовсе — то есть случай разрешим в задании, где кластера нет.
  #
  # Круг называется ЗДЕСЬ и сверяется с тем, что требует сам скрипт: разойдясь,
  # они разошлись бы молча — самопроверка объявляла бы случай разрешимым там, где
  # он не разрешим, и падала бы вместо того, чтобы честно сказать «без предмета».
  local lacking=""
  for t in docker helm openssl python3; do
    command -v "$t" >/dev/null 2>&1 || lacking="$lacking $t"
  done
  if [ -n "$lacking" ]; then
    undecidable "законный близнец коротких выходов не проверен: нет инструментов полосы процесса —$lacking"
  else
    ( KANAME_CHART_BOOTS_DRY_RUN=1 "$bash_abs" "$self" --without-cluster >/dev/null 2>&1 ); rc=$?
    check "предпосылки на месте → короткого выхода НЕТ" "$EXIT_OK" "$rc"
  fi

  # ИСПОЛНИМОСТЬ ВХОДА. Рендер подъёмом не является и вердикта о нём не даёт —
  # но командная строка установки, которая НЕ РЕНДЕРИТСЯ, не поднимет ничего
  # ни при каком кластере, и узнавать об этом через четверть часа стенда
  # незачем.
  if command -v helm >/dev/null 2>&1; then
    ( render_install_chain >/dev/null 2>&1 ); rc=$?
    check "цепочка установки рендерится тем же набором --set" 0 "$rc"
    # Подменяется переменная СКРИПТА, а не окружения: окружение прочитано при
    # старте, и заданная тут переменная до `set_args` не доехала бы — проверка
    # зеленела бы, ничего не подменив.
    ( INJECT=drop-image render_install_chain >/dev/null 2>&1 ); rc=$?
    check "снятая координата образа роняет ту же цепочку" 1 "$rc"
  else
    undecidable "исполнимость входа не проверена: helm недоступен"
  fi

  say ""
  say "случаев проверено: $cases"
  if [ "$ok" -ne 0 ]; then
    say "FAIL: самопроверка владельца подъёма поставляемого чарта"
    return 1
  fi
  if [ "$void" -ne 0 ]; then
    say "УСЛОВИЕ НЕ СОЗДАНО: часть случаев в этой среде неразрешима — в зачёт «прошло» это не идёт"
    return $EXIT_VOID
  fi
  say "PASS: самопроверка владельца подъёма поставляемого чарта"
  return 0
}

# ── разбор вызова ───────────────────────────────────────────────────────────
case "${1:-}" in
  --self-test) SELF_TEST=1 ;;
  --without-cluster) LANE="process" ;;
  "") ;;
  *) say "неизвестный ключ ${1}"; exit $EXIT_VOID ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART="$ROOT/services/iam/deploy"

# set_args — что подставляется СВЕРХ профиля. Это координаты чужого кластера:
# в чужом облаке их называет оператор, здесь — этот скрипт в его роли.
set_args() {
  printf '%s\n' \
    "--set" "image=$IMAGE" \
    "--set" "imagePullPolicy=Never" \
    "--set" "db.host=$1"
  case "$INJECT" in
    "") ;;
    # ИНЪЕКЦИЯ МЕНЯЕТ РОВНО ОДИН ФАКТ против законного близнеца выше.
    drop-trust-domain) printf '%s\n' "--set" "authn.trustDomain=" ;;
    drop-db-ssl)       printf '%s\n' "--set" "repository.postgres.sslMode=disable" ;;
    drop-image)        printf '%s\n' "--set" "image=" ;;
    legal-twin)        printf '%s\n' "--set" "logger.level=DEBUG" ;;
    *) return 1 ;;
  esac
}

# render_install_chain — рендер РОВНО той цепочки и того набора `--set`, которыми
# полоса ставит чарт. Второго набора здесь нет намеренно: разойдясь, они
# разошлись бы молча, и самопроверка утверждала бы про другую командную строку.
render_install_chain() {
  local args=()
  mapfile -t args < <(set_args "kaname-pg.$NS.svc") || return 2
  helm template "$RELEASE" "$CHART" -n "$NS" \
    -f "$CHART/values.yaml" -f "$CHART/values.prod.yaml" "${args[@]}"
}

if [ "${SELF_TEST:-0}" = "1" ]; then self_test; exit $?; fi

# ── предпосылки ─────────────────────────────────────────────────────────────
#
# ПОРЯДОК НЕСУЩИЙ, и каждая ступень стоит там, где её предмет РАЗРЕШИМ.
#
#   1. МЕСТО — первым. «Прогон недействителен» есть утверждение о самом прогоне,
#      и оно не зависит от того, какие инструменты нашлись: на исчерпанном диске
#      вердикта не будет ни у одной ступени, включая прошедшие. Проверка места
#      стояла ТРЕТЬЕЙ, и разрешить её было нельзя, не имея всех инструментов, —
#      то есть недействительность прогона наступала «по разрешению» доступности
#      входа, что переворачивает отношение между ними.
#   2. ИНСТРУМЕНТЫ — их круг зависит от полосы: `kind`/`kubectl` нужны только
#      полосе кластера.
#   3. ЧАРТ на диске.
#   4. СУХОЙ ПРОГОН выходит ЗДЕСЬ, до опроса демона контейнеров: он к демону не
#      обращается, и требовать его значило бы объявить предпосылкой то, чего шаг
#      не касается. Ровно на этом ложном требовании самопроверка перестала бы
#      быть разрешимой в задании, где демона нет.
#   5. ДЕМОН — последним, когда дальше действительно идёт работа.
if ! disk_floor_ok; then
  say "ПРОГОН НЕДЕЙСТВИТЕЛЕН: свободного места меньше порога — вердикта не будет ни у одного шага"
  exit $EXIT_INVALID
fi
need="docker helm openssl python3"
[ "$LANE" = "cluster" ] && need="$need kind kubectl"
missing=""
for t in $need; do command -v "$t" >/dev/null 2>&1 || missing="$missing $t"; done
if [ -n "$missing" ]; then
  say "УСЛОВИЕ НЕ СОЗДАНО: нет инструментов —$missing"
  say "  Это не вердикт о дереве: «не выполнилось» в зачёт «прошло» не идёт."
  exit $EXIT_VOID
fi
if [ ! -f "$CHART/Chart.yaml" ]; then
  say "УСЛОВИЕ НЕ СОЗДАНО: поставляемого чарта нет по пути $CHART"
  exit $EXIT_VOID
fi
if [ "${KANAME_CHART_BOOTS_DRY_RUN:-0}" = "1" ]; then
  say "сухой прогон: предпосылки на месте, чарт $CHART, полоса $LANE"
  exit $EXIT_OK
fi
if ! docker info >/dev/null 2>&1; then
  say "УСЛОВИЕ НЕ СОЗДАНО: демон контейнеров не отвечает"
  exit $EXIT_VOID
fi

WORK="$(mktemp -d)"
cleanup() {
  local rc=$?
  if [ "$KEEP" = "1" ]; then
    say "оставлено по просьбе вызывающего (KANAME_CHART_BOOTS_KEEP=1)"
  else
    kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
    docker rm -f "$TAG-pg" "$TAG-svc" >/dev/null 2>&1 || true
    docker network rm "$TAG-net" >/dev/null 2>&1 || true
  fi
  rm -rf "$WORK"
  exit $rc
}
trap cleanup EXIT

# ── образ службы ────────────────────────────────────────────────────────────
hdr "образ службы"
if [ "${KANAME_CHART_BOOTS_SKIP_BUILD:-0}" = "1" ] && docker image inspect "$IMAGE" >/dev/null 2>&1; then
  say "образ $IMAGE уже собран — сборка пропущена по просьбе вызывающего"
elif ! docker build -f "$ROOT/services/iam/Dockerfile" -t "$IMAGE" \
      --build-arg KACHO_IMAGE_REVISION="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo unknown)" \
      "$ROOT/services/iam" > "$WORK/build.log" 2>&1; then
  say "УСЛОВИЕ НЕ СОЗДАНО: образ службы не собрался — вердикта о чарте это не даёт"
  tail -25 "$WORK/build.log" | sed 's/^/    /'
  exit $EXIT_VOID
else
  say "образ собран: $IMAGE"
fi

# ── материал, который в чужом облаке заводит оператор ───────────────────────
hdr "объекты оператора: два удостоверяющих центра, три листа"
cd "$WORK" || exit $EXIT_VOID
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -keyout ca.key -out ca.crt \
  -subj "/CN=$TAG-internal-ca" >/dev/null 2>&1
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -keyout provider-ca.key -out provider-ca.crt \
  -subj "/CN=$TAG-provider-ca" >/dev/null 2>&1
leaf() { # leaf <имя> <назначение> <SAN>
  openssl req -newkey rsa:2048 -nodes -keyout "$1.key" -out "$1.csr" -subj "/CN=$1" >/dev/null 2>&1
  printf 'subjectAltName=%s\nextendedKeyUsage=%s\n' "$3" "$2" > "$1.ext"
  openssl x509 -req -in "$1.csr" -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out "$1.crt" -days 2 -extfile "$1.ext" >/dev/null 2>&1
}
SVC_SANS="DNS:$RELEASE,DNS:$RELEASE.$NS,DNS:$RELEASE.$NS.svc,DNS:$RELEASE.$NS.svc.cluster.local,DNS:$TAG-svc,DNS:localhost"
leaf server serverAuth "$SVC_SANS"
leaf client clientAuth "URI:spiffe://example.invalid/ns/kacho/sa/kacho-api-gateway,DNS:$RELEASE.$NS.svc"
leaf pg serverAuth "DNS:kaname-pg,DNS:kaname-pg.$NS.svc,DNS:$TAG-pg"
mkdir -p srv cli prov pgtls
cp server.crt srv/tls.crt; cp server.key srv/tls.key; cp ca.crt srv/ca.crt
cp client.crt cli/tls.crt; cp client.key cli/tls.key; cp ca.crt cli/ca.crt
cp provider-ca.crt prov/ca.crt
cp pg.crt pgtls/server.crt; cp pg.key pgtls/server.key
chmod 644 srv/* cli/* prov/* pgtls/*
say "заведено: центров 2, листов 3"

DB_PASSWORD="$TAG-not-a-real-password"
HOOK_SECRET="$TAG-not-a-real-hook-secret"
JWKS_KEY="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

# pg_boot_command — запуск Postgres с ШИФРОВАННЫМ каналом. Открытая база
# превратила бы вердикт в утверждение о другой посадке: боевой профиль объявляет
# `sslMode: require`. Ключ копируется и перевыдаётся владельцу — Postgres
# отказывается стартовать с чужим по владельцу ключом, и это его правило, не наше.
PG_BOOT='mkdir -p /pgtls && cp /pgtls-in/server.crt /pgtls-in/server.key /pgtls/ && chown postgres:postgres /pgtls/server.* && chmod 600 /pgtls/server.key && exec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file=/pgtls/server.crt -c ssl_key_file=/pgtls/server.key'

# ── ПОЛОСА «process»: миграции и процесс контейнерами ───────────────────────
if [ "$LANE" = "process" ]; then
  hdr "полоса process: рендер цепочки"
  if ! render_install_chain > "$WORK/render.yaml" 2> "$WORK/render.err"; then
    say "НАХОДКА: цепочка values.yaml + values.prod.yaml не рендерится тем набором координат, которым её ставят"
    head -25 "$WORK/render.err" | sed 's/^/    /'
    exit $EXIT_FINDING
  fi
  python3 - "$WORK" "$DB_PASSWORD" "$HOOK_SECRET" "$JWKS_KEY" <<'PY'
import sys, yaml
work, dbpw, hook, jwks = sys.argv[1:5]
docs = [d for d in yaml.safe_load_all(open(work + "/render.yaml")) if d]
cm = [d for d in docs if d["kind"] == "ConfigMap"][0]
dep = [d for d in docs if d["kind"] == "Deployment"][0]
open(work + "/config.yaml", "w").write(cm["data"]["config.yaml"])
# Значения секретов подставляются ЗДЕСЬ и только здесь: в кластере их подаёт
# ссылка на объект Secret, и полоса process эту ссылку не судит — что и сказано
# её областью.
known = {"KANAME_DB_PASSWORD": dbpw, "KANAME_HOOK_TOKEN": hook, "KANAME_JWKS_ENC_KEY": jwks}
lines, unresolved = [], []
for e in dep["spec"]["template"]["spec"]["containers"][0]["env"]:
    if "value" in e:
        lines.append("%s=%s" % (e["name"], e["value"]))
    elif e["name"] in known:
        lines.append("%s=%s" % (e["name"], known[e["name"]]))
    else:
        unresolved.append(e["name"])
open(work + "/env.list", "w").write("\n".join(lines) + "\n")
open(work + "/unresolved", "w").write("\n".join(unresolved))
PY
  if [ -s "$WORK/unresolved" ]; then
    say "НАХОДКА: профиль называет секретные переменные, которых полоса не знает:"
    sed 's/^/    /' "$WORK/unresolved"
    say "    Заведите их значение в скрипте либо снимите из профиля."
    exit $EXIT_FINDING
  fi
  say "рендер разобран: настроек 1 файл, переменных $(wc -l < "$WORK/env.list")"

  hdr "полоса process: база"
  docker network create "$TAG-net" >/dev/null 2>&1
  docker run -d --name "$TAG-pg" --network "$TAG-net" \
    -e POSTGRES_PASSWORD="$DB_PASSWORD" -e POSTGRES_USER=iam -e POSTGRES_DB=kaname \
    -v "$WORK/pgtls:/pgtls-in:ro" --entrypoint sh "$PG_IMAGE" -c "$PG_BOOT" >/dev/null 2>&1
  ready=0
  for _ in $(seq 1 40); do
    if docker exec "$TAG-pg" pg_isready -U iam -d kaname >/dev/null 2>&1; then ready=1; break; fi
    sleep 2
  done
  if [ "$ready" != "1" ]; then
    say "УСЛОВИЕ НЕ СОЗДАНО: база стенда не поднялась — вердикта о чарте это не даёт"
    docker logs "$TAG-pg" 2>&1 | tail -15 | sed 's/^/    /'
    exit $EXIT_VOID
  fi
  say "база поднята, канал шифрован"

  # АДРЕС БАЗЫ, ОБЪЯВЛЕННЫЙ ПРОФИЛЕМ, здесь не резолвится: в кластере это имя
  # объекта Service. Подменяется ОДНА строка настроек и печатается вслух.
  sed -i "s#kaname-pg\.$NS\.svc#$TAG-pg#g" "$WORK/config.yaml"
  say "подменено сверх профиля: адрес базы → $TAG-pg (в кластере это имя Service)"

  mounts=(-v "$WORK/config.yaml:/etc/kaname/config.yaml:ro"
          -v "$WORK/srv:/etc/kaname/tls/server:ro"
          -v "$WORK/cli:/etc/kaname/tls/client:ro"
          -v "$WORK/prov:/etc/kaname/tls/provider:ro")

  hdr "полоса process: миграции"
  if ! docker run --rm --network "$TAG-net" --env-file "$WORK/env.list" "${mounts[@]}" \
        --entrypoint /usr/local/bin/kaname-migrator "$IMAGE" up > "$WORK/migrate.log" 2>&1; then
    say "НАХОДКА: миграции не прошли на настройках боевого профиля"
    tail -20 "$WORK/migrate.log" | sed 's/^/    /'
    exit $EXIT_FINDING
  fi
  say "миграции прошли: $(grep -c ' OK ' "$WORK/migrate.log") шагов"

  hdr "полоса process: старт"
  docker run -d --name "$TAG-svc" --network "$TAG-net" --env-file "$WORK/env.list" \
    "${mounts[@]}" "$IMAGE" serve >/dev/null 2>&1
  # Дверь готовности — ТА ЖЕ, которую называет проба чарта: её порт выводится из
  # рендера, а не выписывается здесь.
  PROBE_PORT="$(python3 -c '
import sys,yaml
docs=[d for d in yaml.safe_load_all(open(sys.argv[1])) if d]
dep=[d for d in docs if d["kind"]=="Deployment"][0]
c=dep["spec"]["template"]["spec"]["containers"][0]
name=c["readinessProbe"]["httpGet"]["port"]
print([p["containerPort"] for p in c["ports"] if p["name"]==name][0])' "$WORK/render.yaml")"
  say "дверь готовности из рендера: $PROBE_PORT"
  code=""
  for _ in $(seq 1 45); do
    code="$(docker run --rm --network "$TAG-net" mirror.gcr.io/curlimages/curl:latest \
      -sk -o /dev/null -w '%{http_code}' "https://$TAG-svc:$PROBE_PORT/readyz" 2>/dev/null)"
    [ "$code" = "200" ] && break
    # Сравнение БЕЗ внешнего процесса: `grep -q` выходит до конца входа,
    # писатель слева получает SIGPIPE, и под `pipefail` найденное объявляется
    # ненайденным (гейт internal/repohygiene, задача #658).
    running="$(docker inspect -f '{{.State.Running}}' "$TAG-svc" 2>/dev/null)"
    [[ "$running" == *true* ]] || break
    sleep 2
  done
  findings=0
  if [ "$code" = "200" ]; then
    say "  готовность: /readyz ответил 200 на той двери, которую читает проба чарта"
  else
    say "НАХОДКА: /readyz не ответил 200 (получено «$code») — процесс от этого профиля не поднимается"
    docker logs "$TAG-svc" 2>&1 | tail -40 | sed 's/^/    /'
    findings=$((findings + 1))
  fi
  LOG="$(docker logs "$TAG-svc" 2>&1)"
else
  # ── ПОЛОСА «cluster»: установка чарта в kind ──────────────────────────────
  hdr "кластер"
  kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
  if ! kind create cluster --name "$CLUSTER" --wait 120s > "$WORK/kind.log" 2>&1; then
    say "УСЛОВИЕ НЕ СОЗДАНО: кластер не поднялся — вердикта о чарте это не даёт"
    tail -20 "$WORK/kind.log" | sed 's/^/    /'
    exit $EXIT_VOID
  fi
  KCTX="kind-$CLUSTER"
  k() { kubectl --context "$KCTX" "$@"; }
  if ! kind load docker-image "$IMAGE" --name "$CLUSTER" >/dev/null 2>&1; then
    say "УСЛОВИЕ НЕ СОЗДАНО: образ не загрузился в узел кластера"; exit $EXIT_VOID
  fi
  say "кластер $CLUSTER поднят, образ загружен"
  k create namespace "$NS" >/dev/null

  hdr "секреты и база, которые в чужом облаке заводит оператор"
  k -n "$NS" create secret generic kaname-server-tls \
    --from-file=tls.crt=srv/tls.crt --from-file=tls.key=srv/tls.key --from-file=ca.crt=srv/ca.crt >/dev/null
  k -n "$NS" create secret generic kaname-client-tls \
    --from-file=tls.crt=cli/tls.crt --from-file=tls.key=cli/tls.key --from-file=ca.crt=cli/ca.crt >/dev/null
  k -n "$NS" create secret generic kaname-provider-ca --from-file=ca.crt=prov/ca.crt >/dev/null
  k -n "$NS" create secret generic kaname-db --from-literal=password="$DB_PASSWORD" >/dev/null
  k -n "$NS" create secret generic kaname-authn \
    --from-literal=hook-shared-secret="$HOOK_SECRET" \
    --from-literal=jwks-encryption-key-hex="$JWKS_KEY" >/dev/null
  k -n "$NS" create secret generic kaname-pg-tls \
    --from-file=server.crt=pgtls/server.crt --from-file=server.key=pgtls/server.key >/dev/null
  cat > pg.yaml <<YAML
apiVersion: v1
kind: Service
metadata: { name: kaname-pg, namespace: $NS }
spec:
  selector: { app: kaname-pg }
  ports: [{ port: 5432, targetPort: 5432 }]
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: kaname-pg, namespace: $NS }
spec:
  replicas: 1
  selector: { matchLabels: { app: kaname-pg } }
  template:
    metadata: { labels: { app: kaname-pg } }
    spec:
      volumes:
        - { name: tls, secret: { secretName: kaname-pg-tls } }
        - { name: data, emptyDir: {} }
      containers:
        - name: postgres
          image: $PG_IMAGE
          command: ["sh","-c","$PG_BOOT"]
          env:
            - { name: POSTGRES_PASSWORD, value: $DB_PASSWORD }
            - { name: POSTGRES_USER, value: iam }
            - { name: POSTGRES_DB, value: kaname }
            - { name: PGDATA, value: /var/lib/postgresql/data/pgdata }
          ports: [{ containerPort: 5432 }]
          volumeMounts:
            - { name: tls, mountPath: /pgtls-in, readOnly: true }
            - { name: data, mountPath: /var/lib/postgresql/data }
          readinessProbe:
            exec: { command: ["pg_isready","-U","iam","-d","kaname"] }
            initialDelaySeconds: 3
            periodSeconds: 3
YAML
  k apply -f pg.yaml >/dev/null
  if ! k -n "$NS" rollout status deploy/kaname-pg --timeout=180s > "$WORK/pg.log" 2>&1; then
    say "УСЛОВИЕ НЕ СОЗДАНО: база стенда не поднялась — вердикта о чарте это не даёт"
    k -n "$NS" describe deploy/kaname-pg 2>&1 | sed -n '/Events:/,$p' | head -20 | sed 's/^/    /'
    exit $EXIT_VOID
  fi
  say "секретов 6, база поднята, канал шифрован"

  hdr "УСТАНОВКА поставляемого чарта"
  args=()
  mapfile -t args < <(set_args "kaname-pg.$NS.svc") || { say "неизвестная инъекция $INJECT"; exit $EXIT_VOID; }
  say "цепочка: values.yaml + values.prod.yaml"
  say "подставлено сверх профиля: ${args[*]}"
  if ! helm --kube-context "$KCTX" install "$RELEASE" "$CHART" -n "$NS" \
        -f "$CHART/values.yaml" -f "$CHART/values.prod.yaml" "${args[@]}" \
        > "$WORK/install.log" 2>&1; then
    say "НАХОДКА: поставляемый чарт НЕ УСТАНАВЛИВАЕТСЯ цепочкой values.yaml + values.prod.yaml"
    tail -25 "$WORK/install.log" | sed 's/^/    /'
    exit $EXIT_FINDING
  fi
  say "установка прошла"

  hdr "ГОТОВНОСТЬ"
  findings=0
  if ! k -n "$NS" rollout status "deploy/$RELEASE" --timeout="$ROLLOUT_TIMEOUT" > "$WORK/rollout.log" 2>&1; then
    say "НАХОДКА: под службы не дошёл до готовности — чарт ставится, но не поднимается"
    k -n "$NS" get pods -o wide 2>&1 | sed 's/^/    /'
    k -n "$NS" describe "deploy/$RELEASE" 2>&1 | sed -n '/Events:/,$p' | head -20 | sed 's/^/    /'
    for p in $(k -n "$NS" get pods -l "app=$RELEASE" -o name 2>/dev/null); do
      for c in migrate "$RELEASE"; do
        say "    ·· $p/$c (текущий) ··"
        k -n "$NS" logs "$p" -c "$c" --tail=40 2>&1 | sed 's/^/      /'
        say "    ·· $p/$c (предыдущая попытка) ··"
        k -n "$NS" logs "$p" -c "$c" --previous --tail=40 2>&1 | sed 's/^/      /'
      done
    done
    exit $EXIT_FINDING
  fi
  say "под готов (rollout complete)"
  POD="$(k -n "$NS" get pods -l "app=$RELEASE" -o jsonpath='{.items[0].metadata.name}')"
  LOG="$(k -n "$NS" logs "$POD" -c "$RELEASE" --tail=-1 2>/dev/null)"
  PGPOD="$(k -n "$NS" get pods -l app=kaname-pg -o jsonpath='{.items[0].metadata.name}')"
  DB_SSL="$(k -n "$NS" exec "$PGPOD" -- psql -U iam -d kaname -tAc \
    "select count(*) from pg_stat_ssl s join pg_stat_activity a using (pid) where a.usename='iam' and s.ssl" 2>/dev/null | tr -dc '0-9')"
fi

# ── ПОСАДКА ПОДТВЕРЖДАЕТСЯ ТЕМ, ЧТО ОБЪЯВИЛ САМ ПРОЦЕСС ─────────────────────
#
# «Под Ready» доказательством посадки НЕ является: под, который не
# перезапускался, выглядит так же, как под, поднятый в другой посадке. Поэтому
# спрашивается процесс — что он объявил при старте, — а в полосе кластера ещё и
# БАЗА: шифрование со стороны, которую настройкой нашего пода не подделать.
hdr "посадка"
if [[ "$LOG" == *production-strict* ]]; then
  say "  процесс объявил посадку: production-strict"
else
  say "НАХОДКА: процесс не объявил боевой посадки при старте — вердикт о ней вынести нечем"
  findings=$((findings + 1))
fi
# `grep -c` вход дочитывает до конца, поэтому SIGPIPE здесь не возникает;
# нулевой счёт даёт код 1, и он гасится намеренно — ноль есть ответ, а не отказ.
plain="$(printf '%s' "$LOG" | grep -c '"tls":false' || true)"
if [ "${plain:-0}" -eq 0 ]; then
  say "  поверхностей в открытом тексте: 0"
else
  say "НАХОДКА: поверхностей в открытом тексте $plain — боевая посадка это запрещает"
  findings=$((findings + 1))
fi
if [ "$LANE" = "cluster" ]; then
  if [ -n "${DB_SSL:-}" ] && [ "${DB_SSL:-0}" -ge 1 ]; then
    say "  база подтверждает шифрование: шифрованных соединений службы $DB_SSL"
  else
    say "НАХОДКА: база не видит НИ ОДНОГО шифрованного соединения службы (получено «${DB_SSL:-}»)"
    findings=$((findings + 1))
  fi
fi

hdr "перепись"
say "осмотрено: чарт $CHART · цепочка 2 профиля · полоса $LANE · находок $findings"
if [ "$findings" -ne 0 ]; then exit $EXIT_FINDING; fi
if [ "$LANE" = "cluster" ]; then
  say "ВЕРДИКТ (область: установка, тома, ссылки на секреты, пробы, миграции, старт):"
  say "  поставляемый чарт ставится цепочкой values.yaml + values.prod.yaml и доходит до готовности"
else
  say "ВЕРДИКТ (область: настройки рендера, миграции, старт процесса, дверь готовности):"
  say "  процесс поднимается от настроек, которые производит эта цепочка"
  say "  НЕ СУДИТ: томов, ссылок на объекты Secret, того, что до пробы доходит kubelet."
  say "  Эти вопросы закрывает полоса cluster — читать этот вердикт шире сделанного нельзя."
fi
exit $EXIT_OK
