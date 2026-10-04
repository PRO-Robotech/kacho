#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# bootstrap-cloud-admin.sh — шаг подъёма стенда: первый администратор облака
# заведён путём продукта и входит паролем (kacho#2878).
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ДЕЛАЕТ
#
# Адрес и пароль читаются из объекта Secret, который называет профиль стенда
# (`kaname.platform.iam.bootstrapRootAdmin.secretName`; на kind его чеканит
# stack-secrets.sh, на площадке заводит оператор). Тот же адрес служба доступа
# получает ссылкой на этот секрет (KANAME_BOOTSTRAP_ROOT_EMAIL) и посевом
# бутстрапа выдаёт `system_admin` на кластере, как только человек с этим адресом
# заведён. Человека заводит bootstrap_cloud_admin.py — регистрацией, кодом из
# письма приёмника стенда и подтверждением, то есть тем путём, что проходит
# человек; в базу никто не пишет.
#
# Повторный подъём человека не заводит заново: вход паролем проходит, и шаг
# только убеждается, что раздел «Система» отвечает данными. Секрет не
# перечеканивается (create_generic переиспользует существующий).
#
# ВЕЛИЧИНЫ СЕКРЕТА НЕ ПЕЧАТАЮТСЯ: они читаются в переменные окружения и
# передаются программе окружением, не аргументами (аргументы видны в списке
# процессов). Как войти оператору — deploy/README.md, «Администратор облака
# стенда».
#
# Пробросы портов поднимаются этим шагом и снимаются им же (trap), в том числе
# на отказе.
#
# Использование:
#   bash scripts/bootstrap-cloud-admin.sh [--prove-twin]
#     --prove-twin — доказательство п.2: владелец аккаунта, заведённый
#                    регистрацией, на пути раздела «Система» получает 403.
#
# Коды: 0 — администратор входит, раздел отвечает; 1 — находка; 2 — предпосылки
# шага (нет kubectl/python3, секрет не объявлен); 75 — условие не создано.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
NS="${STACK_NAMESPACE:-kacho}"
RELEASE="${STACK_RELEASE:-kacho-umbrella}"
SECRET="${KACHO_CLOUD_ADMIN_SECRET:-stand-cloud-admin}"

log() { printf '=== bootstrap-cloud-admin: %s\n' "$1"; }
die() { printf 'ABORT: bootstrap-cloud-admin — %s\n' "$1" >&2; exit "${2:-2}"; }

for t in kubectl python3; do
  command -v "$t" >/dev/null 2>&1 || die "нет '$t' — шаг исполнить нечем (условие прогона)" 2
done

# Свободный локальный порт — у системы, а не выбором наугад.
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'; }

PF_PIDS=()
cleanup() {
  local p
  for p in "${PF_PIDS[@]}"; do kill "$p" 2>/dev/null; wait "$p" 2>/dev/null; done
}
trap cleanup EXIT INT TERM

# forward <svc> <удалённый порт> — поднимает проброс и кладёт локальный порт в
# FWD_PORT. Не через подстановку `$(…)`: она исполняется в подоболочке, и pid
# проброса, записанный там в PF_PIDS, до trap не доходит — проброс пережил бы
# шаг (так и наблюдалось на первой ноге own).
FWD_PORT=""
forward() {
  local svc="$1" port="$2" lp _
  lp="$(free_port)"
  kubectl -n "$NS" port-forward "svc/$svc" "$lp:$port" >/dev/null 2>&1 &
  PF_PIDS+=("$!")
  for _ in $(seq 1 40); do
    if python3 -c "import socket,sys; s=socket.socket(); s.settimeout(0.5); sys.exit(s.connect_ex(('127.0.0.1',$lp)))" 2>/dev/null; then
      FWD_PORT="$lp"; return 0
    fi
    sleep 0.5
  done
  return 1
}

if [ "${1:-}" != "--prove-twin" ]; then
  # Чтение величин: ответ сервера, три исхода — есть · NotFound · отказ.
  out="$(kubectl -n "$NS" get secret "$SECRET" -o name 2>&1)" || {
    case "$out" in
      *"(NotFound)"*) die "секрета $SECRET в ns $NS нет — его заводит stack-secrets.sh (kind) либо оператор (площадка)" 75 ;;
      *) die "есть ли секрет $SECRET, не установлено — сервер ответил отказом" 75 ;;
    esac
  }
  KACHO_CLOUD_ADMIN_EMAIL="$(kubectl -n "$NS" get secret "$SECRET" -o jsonpath='{.data.email}' | base64 -d)"
  KACHO_CLOUD_ADMIN_PASSWORD="$(kubectl -n "$NS" get secret "$SECRET" -o jsonpath='{.data.password}' | base64 -d)"
  export KACHO_CLOUD_ADMIN_EMAIL KACHO_CLOUD_ADMIN_PASSWORD
fi

forward api-gateway 8080 || die "проброс к внешнему слушателю края не поднялся" 75
gw="$FWD_PORT"
forward api-gateway 8081 || die "проброс к внутреннему слушателю края не поднялся" 75
gwi="$FWD_PORT"
forward "$RELEASE-mailpit" 8025 || die "проброс к приёмнику писем стенда не поднялся" 75
mb="$FWD_PORT"
log "пробросы: край (внешний, внутренний) и приёмник писем подняты; снимаются на выходе"

KACHO_EDGE_URL="http://127.0.0.1:$gw" \
KACHO_EDGE_INTERNAL_URL="http://127.0.0.1:$gwi" \
MAILBOX_URL="http://127.0.0.1:$mb" \
  python3 "$HERE/bootstrap_cloud_admin.py" "$@"
rc=$?
log "исход: код $rc"
exit "$rc"
