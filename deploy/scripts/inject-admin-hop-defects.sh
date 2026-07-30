#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ЖИВАЯ ИНЪЕКЦИЯ ДЕФЕКТОВ В АДМИНИСТРАТИВНЫЙ ПЕРЕХОД.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ ОТДЕЛЬНЫМ ШАГОМ, А НЕ ЧАСТЬЮ ГЕЙТА ПОСАДКИ
#
# Офлайновая самопроверка (`assert-admin-hop-transport.sh --self-test`) доказывает,
# что ПРЕДИКАТЫ вердикта умеют краснеть на дефекте и молчать на законном входе.
# Она НЕ доказывает, что предикаты подключены к живому стенду теми проводами, о
# которых думает автор. Это доказывает только внесение дефекта В СТЕНД.
#
# Шаг мутирует стенд, поэтому требует МОНОПОЛЬНОГО доступа и обязан восстановить
# исходное состояние, подтвердив это ПОВТОРНЫМ ЗЕЛЁНЫМ ПРОГОНОМ гейта, а не
# заявлением «откатил».
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ИМЕННО ДОКАЗЫВАЕТ ИНЪЕКЦИЯ A (главное здесь)
#
# С терминатором без TLS все отрицательные наблюдения гейта становятся
# «недостижимо/отвергнуто» — то есть ВЫГЛЯДЯТ как «всё запрещено правильно».
# Гейт обязан на этом ПОКРАСНЕТЬ, а не позеленеть: положительная половина не
# подтвердилась, значит отрицательные не засчитываются. Если бы правило пар было
# лишь комментарием, именно этот прогон был бы зелёным над мёртвым переходом.
#
# ЧУЖОЙ ПРОГОН НА СТЕНДЕ ДЕЛАЕТ РЕЗУЛЬТАТ НЕДЕЙСТВИТЕЛЬНЫМ, А НЕ КРАСНЫМ.
# Поэтому шаг заявляет себя единственным писателем и отказывается стартовать,
# если стенд занят либо уже нездоров ДО инъекции.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
NS="${NS:-kacho}"
LOCK_CM="sechat-admin-hop-injection-lock"
HOLDER="${USER:-unknown}@$(hostname 2>/dev/null || echo host)/$$"

RC=0
step()  { echo; echo "── $* ──"; }
ok()    { echo "  ✓ $*"; }
bad()   { echo "  ✗ $*"; RC=1; }
note()  { echo "    $*"; }
invalid() { echo; echo "!!! ПРОГОН НЕДЕЙСТВИТЕЛЕН: $*"; echo "    Это НЕ красный результат — это отсутствие результата."; exit 2; }

command -v kubectl >/dev/null 2>&1 || invalid "нужен kubectl"
command -v helm    >/dev/null 2>&1 || invalid "нужен helm"

# ── ЦЕЛЬ ПИНИТСЯ ПО КЛАСТЕРУ, А НЕ ПО ИМЕНИ КОНТЕКСТА ───────────────────────
ctx="$(kubectl config current-context 2>/dev/null)"
[ "$ctx" = kind-kacho ] || invalid "активный контекст '$ctx' — не kind-kacho"
want_srv="$(kind get kubeconfig --name kacho 2>/dev/null | sed -n 's/^ *server: *//p' | head -1)"
have_srv="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null)"
[ -n "$want_srv" ] || invalid "kind не знает кластера 'kacho' — сверить apiserver НЕ С ЧЕМ"
[ "$want_srv" = "$have_srv" ] || invalid "контекст называется kind-kacho, но ведёт в другой кластер ($have_srv ≠ $want_srv)"

# ── ЕДИНСТВЕННЫЙ ПИСАТЕЛЬ ───────────────────────────────────────────────────
if kubectl -n "$NS" get configmap "$LOCK_CM" >/dev/null 2>&1; then
  who="$(kubectl -n "$NS" get configmap "$LOCK_CM" -o jsonpath='{.data.holder}' 2>/dev/null)"
  since="$(kubectl -n "$NS" get configmap "$LOCK_CM" -o jsonpath='{.data.since}' 2>/dev/null)"
  invalid "стенд уже держит другой прогон инъекций: держит «$who» с $since.
    Снять вручную (убедившись, что тот прогон мёртв):
      kubectl -n $NS delete configmap $LOCK_CM"
fi
kubectl -n "$NS" create configmap "$LOCK_CM" \
  --from-literal=holder="$HOLDER" --from-literal=since="$(date -Is)" >/dev/null 2>&1 \
  || invalid "не удалось взять замок единственного писателя"
release_lock() { kubectl -n "$NS" delete configmap "$LOCK_CM" --ignore-not-found >/dev/null 2>&1; }
trap release_lock EXIT
echo "=== $SCRIPT: единственный писатель — $HOLDER ==="

# ── СТЕНД ОБЯЗАН БЫТЬ ЗДОРОВ ДО ИНЪЕКЦИИ ────────────────────────────────────
# Иначе «покраснело» ничего не доказывает: оно и так было красным.
hstat="$(helm -n "$NS" status kacho-umbrella -o json 2>/dev/null | python3 -c 'import json,sys; print(json.load(sys.stdin)["info"]["status"])' 2>/dev/null)"
[ "$hstat" = deployed ] || invalid "релиз в состоянии '$hstat' — идёт чужая операция helm"

hydra_pod() { kubectl -n "$NS" get pod -l app.kubernetes.io/name=hydra -o jsonpath='{.items[0].metadata.name}' 2>/dev/null; }
ready_pair() { kubectl -n "$NS" get pod -l app.kubernetes.io/name=hydra -o jsonpath='{.items[0].status.containerStatuses[*].ready}' 2>/dev/null; }
hydra_started() { kubectl -n "$NS" get pod "$(hydra_pod)" -o jsonpath='{.status.containerStatuses[?(@.name=="hydra")].state.running.startedAt}' 2>/dev/null; }

step "предусловие: переход исправен ДО инъекции"
if ! bash "$DEPLOY_ROOT/scripts/assert-admin-hop-transport.sh" >/tmp/inj-pre.txt 2>&1; then
  sed 's/^/      /' /tmp/inj-pre.txt | tail -12
  invalid "гейт перехода КРАСЕН ещё до инъекции — доказывать нечего"
fi
ok "гейт перехода зелен до инъекции (значит покраснение ниже вызвано инъекцией)"

CM_NAME="kacho-umbrella-hydra-admin-tls-nginx"
restore_cm() {
  helm template kacho-umbrella "$UMBRELLA" \
    -f "$UMBRELLA/values.dev.yaml" -f "$UMBRELLA/values.dev-prod.yaml" \
    ${IMAGE_IDS:+-f "$IMAGE_IDS"} --namespace "$NS" 2>/dev/null \
    | awk '/^# Source: kacho-umbrella\/templates\/hydra-admin-tls-configmap.yaml/,/^---/' \
    | kubectl -n "$NS" apply -f - >/dev/null 2>&1
}
IMAGE_IDS="$UMBRELLA/values.image-ids.yaml"; [ -f "$IMAGE_IDS" ] || IMAGE_IDS=""

wait_pair() { # <ожидаемое> <секунд>
  local want="$1" secs="$2" i
  for i in $(seq 1 "$secs"); do
    [ "$(ready_pair)" = "$want" ] && return 0
    sleep 1
  done
  return 1
}

# ═════════════════════════════════════════════════════════════════════════════
# ИНЪЕКЦИЯ A: терминатор без TLS.
# ═════════════════════════════════════════════════════════════════════════════
step "A. конфигурация терминатора БЕЗ TLS → гейт обязан покраснеть"
kubectl -n "$NS" create configmap "$CM_NAME" --dry-run=client -o yaml \
  --from-literal=admin-tls.conf='server { listen 4445; location / { proxy_pass http://127.0.0.1:4455; proxy_http_version 1.1; } }' \
  | kubectl -n "$NS" apply -f - >/dev/null 2>&1 || bad "A: не удалось внести дефект"
kubectl -n "$NS" delete pod "$(hydra_pod)" --wait=false >/dev/null 2>&1
sleep 20

if bash "$DEPLOY_ROOT/scripts/assert-admin-hop-transport.sh" >/tmp/inj-a.txt 2>&1; then
  bad "A: гейт ЗЕЛЁН при терминаторе без TLS — он не доказывает шифрование"
  sed 's/^/      /' /tmp/inj-a.txt | tail -12
else
  ok "A: гейт покраснел при терминаторе без TLS"
  grep -E 'SEC-HAT-05|SEC-HAT-06|SEC-HAT-07' /tmp/inj-a.txt | sed 's/^/      /' | head -6
  # ГЛАВНОЕ: отрицательные наблюдения НЕ засчитаны, потому что положительная
  # половина не подтвердилась. Без этого правила прогон был бы зелёным.
  if grep -q 'НЕ ЗАСЧИТЫВАЕТСЯ' /tmp/inj-a.txt; then
    ok "A: отрицательные наблюдения ЯВНО не засчитаны без положительной половины"
    note "именно это отличает «всё запрещено правильно» от «всё лежит»"
  else
    bad "A: гейт покраснел, но не заявил, что отрицательные половины не засчитаны"
  fi
fi

step "A-восстановление"
restore_cm || bad "A: восстановление конфигурации не применилось"
kubectl -n "$NS" delete pod "$(hydra_pod)" --wait=false >/dev/null 2>&1
sleep 10
wait_pair "true true" 120 && ok "A: под снова 2/2 Ready" || bad "A: под не вернулся в Ready"

# ═════════════════════════════════════════════════════════════════════════════
# ИНЪЕКЦИЯ B: сосед убит при ЖИВОМ провайдере → под перестаёт быть Ready.
# ═════════════════════════════════════════════════════════════════════════════
step "B. сосед убит при живом провайдере → под обязан перестать быть Ready"
POD="$(hydra_pod)"
started_before="$(hydra_started)"
wait_pair "true true" 60 >/dev/null || invalid "B: под не Ready перед инъекцией"

# Конфигурация ломается синтаксически: сосед не сможет подняться и будет
# перезапускаться — это ДЕТЕРМИНИРОВАННО «сосед не отвечает», в отличие от
# однократного убийства процесса, после которого контейнер сразу поднимается.
kubectl -n "$NS" create configmap "$CM_NAME" --dry-run=client -o yaml \
  --from-literal=admin-tls.conf='this is not a valid nginx configuration {' \
  | kubectl -n "$NS" apply -f - >/dev/null 2>&1
kubectl -n "$NS" delete pod "$POD" --wait=false >/dev/null 2>&1
sleep 25

pair="$(ready_pair)"
if [ "$pair" = "true true" ]; then
  bad "B: под остался ПОЛНОСТЬЮ Ready при неработающем соседе — готовность к нему не привязана"
else
  ok "B: под НЕ полностью Ready при неработающем соседе (готовность: '$pair')"
  # ПОЛОЖИТЕЛЬНАЯ ПОЛОВИНА: провайдер при этом ЖИВ. Без неё «не Ready» означало
  # бы что угодно, включая «упал весь под».
  P2="$(hydra_pod)"
  hydra_state="$(kubectl -n "$NS" get pod "$P2" -o jsonpath='{.status.containerStatuses[?(@.name=="hydra")].state.running.startedAt}' 2>/dev/null)"
  hydra_restarts="$(kubectl -n "$NS" get pod "$P2" -o jsonpath='{.status.containerStatuses[?(@.name=="hydra")].restartCount}' 2>/dev/null)"
  sc_restarts="$(kubectl -n "$NS" get pod "$P2" -o jsonpath='{.status.containerStatuses[?(@.name=="admin-tls")].restartCount}' 2>/dev/null)"
  if [ -n "$hydra_state" ]; then
    ok "B: провайдер при этом ЖИВ (запущен $hydra_state, перезапусков ${hydra_restarts:-?})"
    note "перезапусков соседа: ${sc_restarts:-?} — не Ready вызвано ИМЕННО соседом"
  else
    bad "B: провайдер не запущен — «не Ready» не доказывает привязку к соседу"
  fi
fi

step "B-восстановление"
restore_cm || bad "B: восстановление конфигурации не применилось"
kubectl -n "$NS" delete pod "$(hydra_pod)" --wait=false >/dev/null 2>&1
sleep 10
wait_pair "true true" 180 && ok "B: под снова 2/2 Ready" || bad "B: под не вернулся в Ready"

# ═════════════════════════════════════════════════════════════════════════════
# ИНЪЕКЦИЯ D: СТАТИЧЕСКИЙ ПУТЬ ЗДОРОВЬЯ У СОСЕДА.
#
# Самый вероятный будущий дефект этого перехода — не поломка, а «улучшение»:
# следующий читатель добавит соседу ответ на путь здоровья, как в образце, с
# которого списан приём. Шифрование останется настоящим, маркер на месте,
# запись в журнале образцовой — и при этом «под готов» перестанет означать
# «провайдер отвечает». Здесь это вносится ВЖИВУЮ и требуется покраснение.
#
# Дефект вносится на путь, которым ходит ПРОБНИК гейта (/health/ready): именно
# его запись гейт ищет по маркеру.
# ═════════════════════════════════════════════════════════════════════════════
step "D. статический ответ соседа на пути здоровья → гейт обязан покраснеть"
kubectl -n "$NS" create configmap "$CM_NAME" --dry-run=client -o yaml \
  --from-literal=admin-tls.conf="log_format kacho_admin_tls '\$request_id ssl_protocol=\$ssl_protocol ssl_cipher=\$ssl_cipher upstream_status=\$upstream_status upstream_time=\$upstream_response_time status=\$status method=\$request_method marker=\$http_x_kacho_probe';
server {
  listen 4445 ssl;
  ssl_certificate /etc/tls/tls.crt;
  ssl_certificate_key /etc/tls/tls.key;
  access_log /dev/stdout kacho_admin_tls;
  location = /health/ready { return 200 'ok'; }
  location / { proxy_pass http://127.0.0.1:4455; proxy_http_version 1.1; }
}" | kubectl -n "$NS" apply -f - >/dev/null 2>&1 || bad "D: не удалось внести дефект"
kubectl -n "$NS" delete pod "$(hydra_pod)" --wait=false >/dev/null 2>&1
sleep 25

if bash "$DEPLOY_ROOT/scripts/assert-admin-hop-transport.sh" >/tmp/inj-d.txt 2>&1; then
  bad "D: гейт ЗЕЛЁН при статическом ответе соседа — «под готов» больше ничего не значит"
  sed 's/^/      /' /tmp/inj-d.txt | tail -12
else
  ok "D: гейт покраснел при статическом ответе соседа"
  grep -E 'SEC-HAT-07|апстрим' /tmp/inj-d.txt | sed 's/^/      /' | head -6
  # ГЛАВНОЕ: покраснеть он обязан ИМЕННО на полях апстрима, а не «вообще».
  # Шифрование в этой инъекции настоящее, поэтому все прежние утверждения
  # зелены — и без чтения полей апстрима прогон был бы ЗЕЛЁНЫМ.
  if grep -q 'сосед ответил САМ' /tmp/inj-d.txt; then
    ok "D: причина названа верно — апстрим по пути пробы не спрошен"
  else
    bad "D: гейт покраснел, но НЕ на полях апстрима — причина не та"
  fi
fi

step "D-восстановление"
restore_cm || bad "D: восстановление конфигурации не применилось"
kubectl -n "$NS" delete pod "$(hydra_pod)" --wait=false >/dev/null 2>&1
sleep 10
wait_pair "true true" 180 && ok "D: под снова 2/2 Ready" || bad "D: под не вернулся в Ready"

# ═════════════════════════════════════════════════════════════════════════════
# ИНЪЕКЦИЯ C: обращение к листенеру провайдера снаружи его пода.
# ═════════════════════════════════════════════════════════════════════════════
step "C. листенер провайдера недостижим снаружи пода — В ПАРЕ с достижимостью изнутри"
if bash "$DEPLOY_ROOT/scripts/assert-admin-hop-transport.sh" >/tmp/inj-c.txt 2>&1; then
  grep -E 'SEC-HAT-03' /tmp/inj-c.txt | sed 's/^/      /'
  ok "C: обе половины подтверждены гейтом на восстановленном стенде"
else
  bad "C: гейт красен после восстановления"
  sed 's/^/      /' /tmp/inj-c.txt | tail -12
fi

# ═════════════════════════════════════════════════════════════════════════════
# ВОССТАНОВЛЕНИЕ ПОДТВЕРЖДАЕТСЯ, А НЕ ЗАЯВЛЯЕТСЯ.
# ═════════════════════════════════════════════════════════════════════════════
step "восстановление стенда подтверждается ПОЛНЫМ гейтом посадки"
if POSTURE_PROFILE=production bash "$DEPLOY_ROOT/scripts/assert-production-posture.sh" >/tmp/inj-post.txt 2>&1; then
  ok "полный гейт посадки ЗЕЛЁН — стенд восстановлен"
  grep -E '^  [A-D]\.' /tmp/inj-post.txt | sed 's/^/      /'
else
  bad "полный гейт посадки КРАСЕН после инъекций — СТЕНД ИСПОРЧЕН"
  note "шаг считается проваленным: восстановление не подтверждено."
  sed 's/^/      /' /tmp/inj-post.txt | tail -20
fi

echo
if [ $RC -eq 0 ]; then
  echo "PASS: $SCRIPT — все инъекции покраснели, стенд восстановлен и подтверждён"
else
  echo "FAIL: $SCRIPT — см. ✗ выше"
fi
exit $RC
