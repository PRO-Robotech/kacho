#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# deps-failure-class-inject.sh — доказательство инъекцией для различения
# «наша поломка» / «чужая недоступность» / «неопознано».
#
# Классификатор выдаёт ПОСЛАБЛЕНИЕ: отказ, признанный чужим, превращает красное
# в «условие не создано». Послабление, выданное слишком широко, маскирует наши
# находки навсегда — поэтому проверяются ОБЕ стороны по каждой оси, и особенно
# та, где послабление выдаваться НЕ должно.
set -uo pipefail
cd "$(dirname "$0")" || exit 1
. lib/deps-failure-class.sh

tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
pass=0; fail=0

check() { # имя, ожидаемый класс, текст вывода helm
  local name=$1 want=$2 text=$3
  printf '%s\n' "$text" > "$tmp/log.txt"
  local got; got=$(classify_deps_failure "$tmp/log.txt")
  if [ "$got" = "$want" ]; then
    pass=$((pass+1)); echo "  [ok] $name → $got"
  else
    fail=$((fail+1)); echo "  [ПРОВАЛ] $name → $got, ждали $want"
  fi
}

# ── (а) ЧУЖОЕ, форма ПЕРВАЯ: индекс репозитория не обновился.
check "недоступен индекс репозитория" external \
'Error: no cached repo found: open /root/.cache/helm/repository/x-index.yaml: no such file
Unable to get an update from the "https://charts.bitnami.com/bitnami" chart repository'

# ── (б) ЧУЖОЕ, форма ВТОРАЯ: хост оборвал соединение на СКАЧИВАНИИ.
#        Ровно та форма, что 2026-08-21 падала красным на PR #887.
check "обрыв соединения при скачивании" external \
'Save error occurred:  could not find : chart hydra not found in https://k8s.ory.sh/helm/charts: looks like "https://k8s.ory.sh/helm/charts" is not a valid chart repository or cannot be reached: Get "https://k8s.ory.sh/helm/charts/index.yaml": read tcp 10.1.0.27:33630->185.199.111.153:443: read: connection reset by peer'

# ── (в) ЧУЖОЕ, форма ВТОРАЯ, другая сетевая причина.
check "таймаут рукопожатия при скачивании" external \
'Error: chart x not found in https://example.invalid/charts: looks like "https://example.invalid/charts" is not a valid chart repository or cannot be reached: Get "https://example.invalid/charts/index.yaml": net/http: TLS handshake timeout'

# ── (г) НАШЕ, и послабление выдаваться НЕ ДОЛЖНО: опечатка в адресе.
#        Фраза о недостижимости репозитория здесь ТА ЖЕ, но сетевой причины нет —
#        хост честно ответил, что такого репозитория у него нет. Повтор это не
#        лечит, и признание отказа чужим спрятало бы опечатку навсегда.
check "опечатка в адресе репозитория" transient \
'Error: chart x not found in https://charts.example.com/typo: looks like "https://charts.example.com/typo" is not a valid chart repository or cannot be reached: error unmarshaling JSON: json: cannot unmarshal string'

# ── (д) НАШЕ: локального сабчарта нет.
check "локальный сабчарт отсутствует" ours \
'Error: directory /w/deploy/helm/umbrella/charts/kacho-iam not found'

# ── (е) НАШЕ: пин мёртв. Проверяется ПЕРВЫМ даже когда рядом чужая недоступность.
check "мёртвый пин рядом с чужим отказом" ours \
"can't get a valid version for chart x
Unable to get an update from the \"https://charts.bitnami.com/bitnami\" chart repository"

# ── (ж) НЕОПОЗНАННОЕ остаётся неопознанным: послабление не выдаётся молча.
check "незнакомый отказ" transient 'Error: something else entirely went wrong'

echo
echo "инъекция классификатора: утверждений $((pass+fail)), пройдено $pass, провалено $fail"
[ "$fail" -eq 0 ] || exit 1
