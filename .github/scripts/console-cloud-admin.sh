#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# console-cloud-admin.sh — условие прогона проб распорядителя: на СВОЁМ стенде
# конвейера заведён администратор облака, и его удостоверение лежит в секрете,
# который шаг проб читает сам.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Пробы распорядителя над записью человека (`ui-future/e2e/specs/cloud-admin-lane.spec.ts`:
# Ф3-24, Ф12-45) читают удостоверение из `KACHO_CLOUD_ADMIN_EMAIL` и
# `KACHO_CLOUD_ADMIN_PASSWORD`. Подъём `own-up` заводит человека шагом
# `bootstrap-cloud-admin` (kacho#2878), а конвейер консоли поднимает `dev-up`, где
# этого шага нет: условие не создавалось ни на одном прогоне, и две пробы уходили
# в «не выполнилось» по построению. Этот шаг его создаёт — тем же путём продукта
# (deploy/scripts/bootstrap-cloud-admin.sh: регистрация, код из письма приёмника
# стенда, подтверждение; право `system_admin` выдаёт посев бутстрапа службы).
#
# ОТКУДА АДРЕС. Его спрашивают у поднятой службы, а не выписывают: служба выдаёт
# право ровно тому адресу, что стоит в её `KANAME_BOOTSTRAP_ROOT_EMAIL`. Форм у
# этой переменной две (шаблон подчарта kaname, ручка
# `platform.iam.bootstrapRootAdmin`):
#   • ссылка на секрет (`valueFrom.secretKeyRef`, стенд own) — секрет уже несёт
#     адрес и пароль, шаг берёт его как есть;
#   • величина (`value`, стенд dev: человек — фикстура проб) — пароля у стенда
#     нет, и шаг чеканит секрет прогона: адрес — тот, что назвала служба, пароль
#     — случайный, существующий секрет не перечеканивается.
# Переменной нет вовсе — служба первого администратора не объявляет, и условие
# не создано (код 75), а не красное.
#
# ВЕЛИЧИНЫ НЕ ПЕЧАТАЮТСЯ: адрес и пароль идут в секрет и в программу окружением;
# наружу шаг отдаёт только ИМЯ секрета (`secret=` в $GITHUB_OUTPUT).
#
# Коды: 0 — администратор входит, раздел «Система» отвечает; 1 — находка
# (продукт ответил не по контракту); 2 — предпосылки шага; 75 — условие не
# создано.
#
# Использование:
#   bash .github/scripts/console-cloud-admin.sh
#   bash .github/scripts/console-cloud-admin.sh --self-test
set -euo pipefail

NS="${STACK_NAMESPACE:-kacho}"
DEPLOYMENT="${KANAME_DEPLOYMENT:-kaname}"
RUN_SECRET="${KACHO_CLOUD_ADMIN_RUN_SECRET:-console-e2e-cloud-admin}"
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"

log() { printf '=== console-cloud-admin: %s\n' "$1"; }
die() { printf 'ABORT: console-cloud-admin — %s\n' "$1" >&2; exit "${2:-2}"; }

# address_source <json записи env> — решение по форме переменной службы:
#   "secret <имя> <ключ>" | "value" | "absent" | "unknown"
# Чистая функция: самопроверка ниже кормит её всеми формами.
address_source() {
  python3 -c '
import json, sys
raw = sys.argv[1].strip()
if not raw:
    print("absent"); sys.exit(0)
try:
    e = json.loads(raw)
except json.JSONDecodeError:
    print("unknown"); sys.exit(0)
ref = ((e.get("valueFrom") or {}).get("secretKeyRef") or {})
if ref.get("name"):
    print("secret", ref["name"], ref.get("key") or "email")
elif isinstance(e.get("value"), str) and e["value"]:
    print("value")
else:
    print("unknown")
' "$1"
}

if [ "${1:-}" = "--self-test" ]; then
  fail=0
  check() {
    local want="$1" got
    got="$(address_source "$2")"
    if [ "$got" = "$want" ]; then
      printf '  ok   %-40s ← %s\n' "$want" "$3"
    else
      printf '  FAIL ждали «%s», получили «%s» ← %s\n' "$want" "$got" "$3"; fail=1
    fi
  }
  check "secret stand-cloud-admin email" '{"name":"KANAME_BOOTSTRAP_ROOT_EMAIL","valueFrom":{"secretKeyRef":{"name":"stand-cloud-admin","key":"email"}}}' "ссылка на секрет (own)"
  check "secret s email"                 '{"name":"KANAME_BOOTSTRAP_ROOT_EMAIL","valueFrom":{"secretKeyRef":{"name":"s"}}}' "ссылка без ключа — умолчание ручки"
  check "value"                          '{"name":"KANAME_BOOTSTRAP_ROOT_EMAIL","value":"a@example.com"}' "величина (dev)"
  check "absent"                         '' "переменной у службы нет"
  check "unknown"                        '{"name":"KANAME_BOOTSTRAP_ROOT_EMAIL","value":""}' "пустая величина — не адрес"
  check "unknown"                        '{"name":"KANAME_BOOTSTRAP_ROOT_EMAIL","valueFrom":{"fieldRef":{"fieldPath":"x"}}}' "чужая форма ссылки"
  check "unknown"                        'не JSON' "ответ не разобран"
  if [ "$fail" != 0 ]; then
    echo "самопроверка: есть отказы"; exit 1
  fi
  echo "самопроверка: 7 случаев, отказов 0"
  exit 0
fi

for t in kubectl python3 openssl base64; do
  command -v "$t" >/dev/null 2>&1 || die "нет '$t' — шаг исполнить нечем (условие прогона)" 2
done

errf="$(mktemp)"
trap 'rm -f "$errf"' EXIT
entry="$(kubectl -n "$NS" get deploy "$DEPLOYMENT" \
  -o jsonpath='{.spec.template.spec.containers[*].env[?(@.name=="KANAME_BOOTSTRAP_ROOT_EMAIL")]}' 2>"$errf")" ||
  die "развёртывание службы $DEPLOYMENT в ns $NS не прочитано — сервер ответил: $(head -c 200 "$errf")" 75

read -r kind secret key <<<"$(address_source "$entry")"
case "$kind" in
  secret)
    [ "$key" = "email" ] ||
      die "адрес службы лежит в секрете $secret под ключом «$key», а шаг подъёма читает ключ email — форма секрета не та, что у bootstrap-cloud-admin" 1
    log "адрес первого администратора служба берёт из секрета $secret — шаг берёт его как есть"
    ;;
  value)
    secret="$RUN_SECRET"
    if kubectl -n "$NS" get secret "$secret" -o name >/dev/null 2>&1; then
      log "секрет прогона $secret уже есть — не перечеканивается"
    else
      email="$(kubectl -n "$NS" get deploy "$DEPLOYMENT" \
        -o jsonpath='{.spec.template.spec.containers[*].env[?(@.name=="KANAME_BOOTSTRAP_ROOT_EMAIL")].value}')"
      [ -n "$email" ] || die "служба назвала адрес величиной, а чтение величины вернуло пусто" 75
      password="$(openssl rand -hex 24)"
      # Величины уходят в kubectl через stdin манифеста, а не аргументами:
      # аргументы видны в списке процессов.
      EMAIL="$email" PASSWORD="$password" NS="$NS" NAME="$secret" python3 -c '
import base64, json, os
b = lambda s: base64.b64encode(s.encode()).decode()
print(json.dumps({"apiVersion": "v1", "kind": "Secret", "type": "Opaque",
                  "metadata": {"name": os.environ["NAME"], "namespace": os.environ["NS"]},
                  "data": {"email": b(os.environ["EMAIL"]), "password": b(os.environ["PASSWORD"])}}))
' | kubectl create -f - >/dev/null
      unset email password
      log "секрет прогона $secret заведён: адрес — тот, что назвала служба, пароль — случайный"
    fi
    ;;
  absent)
    die "условие не создано: служба $DEPLOYMENT не объявляет первого администратора облака (KANAME_BOOTSTRAP_ROOT_EMAIL нет) — право выдать некому" 75
    ;;
  *)
    die "форма KANAME_BOOTSTRAP_ROOT_EMAIL у службы не распознана — ни ссылка на секрет, ни величина" 1
    ;;
esac

set +e
KACHO_CLOUD_ADMIN_SECRET="$secret" STACK_NAMESPACE="$NS" bash "$ROOT/deploy/scripts/bootstrap-cloud-admin.sh"
rc=$?
set -e
case "$rc" in
  0)
    log "администратор облака входит, раздел «Система» отвечает — удостоверение в секрете $secret"
    [ -n "${GITHUB_OUTPUT:-}" ] && echo "secret=$secret" >> "$GITHUB_OUTPUT"
    exit 0
    ;;
  75) die "условие не создано: шаг подъёма администратора облака не дошёл до продукта (код 75, причина — выше)" 75 ;;
  1)  die "НАХОДКА: шаг подъёма администратора облака — продукт ответил не по контракту (код 1, причина — выше)" 1 ;;
  *)  die "шаг подъёма администратора облака вышел кодом $rc" "$rc" ;;
esac
