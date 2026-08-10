#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ПЕРЕМЕР ПРЕДПОСЫЛКИ ТЕРМИНАТОРА — ИСПОЛНЯЕМЫЙ АРТЕФАКТ, А НЕ ПРОЗА.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ
#
# Терминатор TLS перед административным листенером провайдера существует ТОЛЬКО
# потому, что развёрнутая версия провайдера не читает объявление TLS для
# ОТДЕЛЬНОГО листенера. Это измерено (координата — deploy/PROVIDER-LISTENER-PREMISE.md),
# а не выведено из документации: до этого в дереве подряд стояли два
# взаимоисключающих объяснения, и оба были неверны.
#
# Предмет может измениться ТОЛЬКО вместе с версией провайдера. Поэтому гейт
# посадки сверяет живую версию с записанной и при расхождении НАЗЫВАЕТ КОМАНДУ
# ЗАПУСКА ЭТОГО СКРИПТА. Без исполняемого перемера «сужение частоты проверки»
# выродилось бы в отказ от проверки: записанная прозой процедура деградирует в
# комментарий, который никто не запускает и который однажды начинает описывать
# не то.
#
# ЧТО ДЕЛАЕТ. Поднимает ОДНОРАЗОВУЮ нагрузку на НАЗВАННОЙ версии образа с
# объявленным `serve.admin.tls`, выданным сертификатом и смонтированным
# секретом — и смотрит, чем отвечает административный порт:
#
#   ошибка уровня протокола («wrong version number») ⇒ порт ответил ОТКРЫТЫМ
#     ТЕКСТОМ ⇒ ПРЕДПОСЫЛКА ДЕРЖИТСЯ, терминатор нужен;
#   рукопожатие состоялось                          ⇒ версия объявление ЧИТАЕТ
#     ⇒ ПРЕДПОСЫЛКА МЕРТВА, терминатор подлежит снятию, работа возвращается в
#     acceptance.
#
# САМОПРОВЕРКА БЕЗ КЛАСТЕРА кормит классификатор двумя синтетическими
# наблюдениями и требует ИХ РАЗЛИЧИТЬ — иначе «перемерили, держится»
# неотличимо от «скрипт ничего не измерил».
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PREMISE_DOC="$DEPLOY_ROOT/PROVIDER-LISTENER-PREMISE.md"
NS="${NS:-kacho}"

# ── ЧТЕНИЕ ЗАПИСАННОЙ КООРДИНАТЫ ────────────────────────────────────────────
# Определены ДО самопроверки: она проверяет предпосылку самого перемера —
# что координату вообще есть чем прочитать.
recorded_image() { grep -oE 'oryd/hydra:v[0-9][^`|[:space:]]*' "$PREMISE_DOC" | head -1; }
recorded_chart() { grep -oE 'hydra-[0-9]+\.[0-9]+\.[0-9]+' "$PREMISE_DOC" | head -1; }

# ── ПРЕДИКАТ ВЕРДИКТА — ЧИСТАЯ ФУНКЦИЯ НАД ТЕКСТОМ ──────────────────────────
#
# Три исхода, и «прочее» — ОТДЕЛЬНЫЙ именованный исход, а не корзина: перемер,
# не понявший, что он увидел, не вправе подтверждать предпосылку.
classify_listener_answer() {
  local out="$1"
  if grep -qiE 'wrong version number|record layer failure|unknown protocol|http request' <<<"$out"; then
    echo premise-holds; return           # ответил открытым текстом
  fi
  if grep -qiE 'SSL connection using TLS|Server certificate|SSL certificate problem|unable to get local issuer|certificate verify failed' <<<"$out"; then
    # И успешное рукопожатие, И «TLS есть, доверия нет» одинаково доказывают,
    # что на порту ГОВОРЯТ ПО TLS: предпосылка мертва в обоих случаях.
    echo premise-dead; return
  fi
  echo unclassified
}

# ── САМОПРОВЕРКА ────────────────────────────────────────────────────────────
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: классификатор перемера против двух наблюдений ==="
  rc=0; checked=0
  expect() { # <метка> <ожидаемо> <наблюдение>
    local label="$1" want="$2" obs="$3" got
    checked=$((checked + 1))
    got="$(classify_listener_answer "$obs")"
    if [ "$got" = "$want" ]; then echo "  ✓ $label → $got"
    else echo "  ✗ $label → получено '$got', ожидалось '$want'"; rc=1; fi
  }
  expect "порт ответил открытым текстом (ошибка уровня протокола)" premise-holds \
    'curl: (35) error:0A00010B:SSL routines::wrong version number'
  expect "рукопожатие состоялось" premise-dead \
    '* SSL connection using TLSv1.3 / TLS_AES_256_GCM_SHA384'
  expect "TLS есть, доверия нет — на порту ВСЁ РАВНО говорят по TLS" premise-dead \
    'curl: (60) SSL certificate problem: unable to get local issuer certificate'
  expect "неузнанное наблюдение не подтверждает предпосылку" unclassified \
    'нечто, ни на что не похожее'

  # РАЗЛИЧЕНИЕ — то, ради чего самопроверка и существует.
  checked=$((checked + 1))
  a="$(classify_listener_answer 'curl: (35) wrong version number')"
  b="$(classify_listener_answer '* SSL connection using TLSv1.3')"
  if [ "$a" != "$b" ]; then
    echo "  ✓ два наблюдения РАЗЛИЧЕНЫ ('$a' ≠ '$b') — перемер что-то измеряет"
  else
    echo "  ✗ наблюдения НЕ различены — «перемерили, держится» неотличимо от «ничего не измерили»"
    rc=1
  fi

  # ПРЕДПОСЫЛКА САМОГО ПЕРЕМЕРА: запись координаты обязана существовать и быть
  # разбираемой, иначе сверять живую версию НЕ С ЧЕМ.
  checked=$((checked + 1))
  if [ -f "$PREMISE_DOC" ] && grep -qE 'oryd/hydra:v[0-9]' "$PREMISE_DOC" && grep -qE 'hydra-[0-9]+\.[0-9]+\.[0-9]+' "$PREMISE_DOC"; then
    echo "  ✓ координата предпосылки читается: $(recorded_image 2>/dev/null || grep -oE 'oryd/hydra:v[^`]*' "$PREMISE_DOC" | head -1)"
  else
    echo "  ✗ координата предпосылки не разобрана — сверять живую версию НЕ С ЧЕМ"
    rc=1
  fi

  echo
  echo "синтетических наблюдений проверено: $checked"
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $rc
fi

IMAGE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --image) IMAGE="${2:-}"; shift 2 ;;
    -h|--help)
      echo "usage: $SCRIPT --image <образ провайдера>"
      echo "       перемеряет, читает ли названная версия объявление TLS отдельного листенера"
      exit 0 ;;
    *) echo "неизвестный аргумент: $1"; exit 2 ;;
  esac
done
[ -z "$IMAGE" ] && IMAGE="$(recorded_image)"
[ -z "$IMAGE" ] && { echo "FATAL: образ не назван и не прочитан из $PREMISE_DOC"; exit 2; }

command -v kubectl >/dev/null 2>&1 || { echo "FATAL: нужен kubectl"; exit 2; }
ctx="$(kubectl config current-context 2>/dev/null)"
[ "$ctx" = kind-kacho ] || { echo "ABORT: контекст '$ctx' — не kind-kacho"; exit 2; }

echo "=== $SCRIPT: перемер предпосылки на $IMAGE ==="
echo "  записано: образ $(recorded_image), чарт $(recorded_chart)"
echo

WL=sechat-premise-remeasure
kubectl -n "$NS" delete pod "$WL" --ignore-not-found --wait=true >/dev/null 2>&1

# Одноразовая нагрузка: провайдер той же версии, административный листенер с
# ОБЪЯВЛЕННЫМ TLS и смонтированным сертификатом. Секрет берётся тот же, что у
# стенда, — иначе перемер измерял бы отсутствие материала, а не поведение версии.
SECRET="${ADMIN_TLS_SECRET:-kacho-umbrella-hydra-admin-tls}"
if ! kubectl -n "$NS" get secret "$SECRET" >/dev/null 2>&1; then
  echo "FATAL: секрета $SECRET нет — перемер измерял бы отсутствие материала,"
  echo "       а не поведение версии. Это НЕ «предпосылка держится»."
  exit 2
fi

cat <<EOF | kubectl -n "$NS" apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Pod
metadata:
  name: $WL
  labels: { app.kubernetes.io/part-of: kacho, kacho.cloud/component: premise-remeasure }
spec:
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    seccompProfile: { type: RuntimeDefault }
  volumes:
    - { name: tls, secret: { secretName: $SECRET } }
    - { name: cfg, emptyDir: {} }
  initContainers:
    - name: write-config
      image: docker.io/library/busybox:1.36
      imagePullPolicy: IfNotPresent
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: { drop: ["ALL"] }
      command: ["sh","-c"]
      args:
        - |
          cat > /cfg/hydra.yaml <<'CFG'
          serve:
            public:
              port: 4444
            admin:
              port: 4445
              tls:
                cert:
                  path: /tls/tls.crt
                key:
                  path: /tls/tls.key
          urls:
            self:
              issuer: https://premise-remeasure.invalid
          secrets:
            system:
              - premise-remeasure-throwaway-secret-32
          CFG
      volumeMounts:
        - { name: cfg, mountPath: /cfg }
  containers:
    - name: hydra
      image: $IMAGE
      imagePullPolicy: IfNotPresent
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: { drop: ["ALL"] }
      args: ["serve","admin","--config","/cfg/hydra.yaml"]
      env:
        - { name: DSN, value: "memory" }
      volumeMounts:
        - { name: tls, mountPath: /tls, readOnly: true }
        - { name: cfg, mountPath: /cfg, readOnly: true }
EOF

echo "  ждём запуска нагрузки…"
for _ in $(seq 1 60); do
  ph="$(kubectl -n "$NS" get pod "$WL" -o jsonpath='{.status.phase}' 2>/dev/null)"
  [ "$ph" = Running ] && break
  [ "$ph" = Failed ] && break
  sleep 2
done
LOGS="$(kubectl -n "$NS" logs "$WL" -c hydra 2>&1 | tail -30)"
POD_IP="$(kubectl -n "$NS" get pod "$WL" -o jsonpath='{.status.podIP}' 2>/dev/null)"

if [ "$ph" != Running ] || [ -z "$POD_IP" ]; then
  echo "  ✗ нагрузка не поднялась (phase=$ph) — перемер НЕ ВЫПОЛНЕН."
  echo "    «не выполнилось» не идёт в зачёт ни «держится», ни «мертва»."
  printf '%s\n' "$LOGS" | sed 's/^/      /'
  kubectl -n "$NS" delete pod "$WL" --ignore-not-found --wait=false >/dev/null 2>&1
  exit 2
fi

OBS="$(kubectl -n "$NS" run sechat-premise-probe --rm -i --restart=Never --quiet \
  --image=docker.io/alpine/k8s:1.36.2 --image-pull-policy=IfNotPresent \
  --overrides='{"spec":{"securityContext":{"runAsNonRoot":true,"runAsUser":65532,"seccompProfile":{"type":"RuntimeDefault"}},"containers":[{"name":"p","image":"docker.io/alpine/k8s:1.36.2","imagePullPolicy":"IfNotPresent","securityContext":{"allowPrivilegeEscalation":false,"readOnlyRootFilesystem":true,"capabilities":{"drop":["ALL"]}},"command":["sh","-c"],"args":["curl -sS -v -k -m 8 -o /dev/null https://'"$POD_IP"':4445/health/ready 2>&1"]}]}}' \
  2>&1)"

VERDICT="$(classify_listener_answer "$OBS")"
echo
echo "  собственная строка провайдера про административный листенер:"
printf '%s\n' "$LOGS" | grep -iE 'admin|4445' | tail -3 | sed 's/^/      /'
echo "  наблюдение рукопожатия: $VERDICT"
kubectl -n "$NS" delete pod "$WL" --ignore-not-found --wait=false >/dev/null 2>&1

case "$VERDICT" in
  premise-holds)
    echo
    echo "ПРЕДПОСЫЛКА ДЕРЖИТСЯ на $IMAGE: административный порт отвечает открытым"
    echo "текстом при объявленном и смонтированном сертификате. Терминатор нужен."
    echo
    echo "Обнови координату в $PREMISE_DOC:"
    echo "  образ $IMAGE, дата $(date +%Y-%m-%d), исход «предпосылка держится»."
    exit 0 ;;
  premise-dead)
    echo
    echo "ПРЕДПОСЫЛКА МЕРТВА на $IMAGE: на административном порту говорят по TLS —"
    echo "версия объявление ЧИТАЕТ."
    echo
    echo "ТЕРМИНАТОР ПОДЛЕЖИТ СНЯТИЮ. Обходной путь, у которого больше нет предмета,"
    echo "— находка, а не наследство. Работа возвращается в acceptance: снятие"
    echo "терминатора меняет адрес перехода у всех потребителей и трогает пробы пода."
    exit 1 ;;
  *)
    echo
    echo "ПЕРЕМЕР НЕ ДАЛ УЗНАВАЕМОГО ИСХОДА — это НЕ подтверждение предпосылки."
    printf '%s\n' "$OBS" | tail -15 | sed 's/^/      /'
    exit 2 ;;
esac
