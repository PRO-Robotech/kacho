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
'Error: directory /w/deploy/helm/umbrella/charts/kaname not found'

# ── (е) НАШЕ: пин мёртв. Проверяется ПЕРВЫМ даже когда рядом чужая недоступность.
check "мёртвый пин рядом с чужим отказом" ours \
"can't get a valid version for chart x
Unable to get an update from the \"https://charts.bitnami.com/bitnami\" chart repository"

# ── (ж) НЕОПОЗНАННОЕ остаётся неопознанным: послабление не выдаётся молча.
check "незнакомый отказ" transient 'Error: something else entirely went wrong'

# ── (з) ЧУЖОЕ, форма ТРЕТЬЯ: хост оборвал соединение на скачивании САМОГО АРХИВА.
#        Ровно тот вывод, что 2026-08-24 дал красное на накопительной линии #1523
#        (kacho#1525). Ветка helm здесь ещё другая: она печатает не «репозиторий
#        недостижим», а «не смог скачать <адрес>.tgz», поэтому обе прежние формы
#        мимо — и отказ уезжал в неопознанное.
check "обрыв соединения при скачивании АРХИВА" external \
'Getting updates for unmanaged Helm repositories...
...Successfully got an update from the "https://charts.bitnami.com/bitnami" chart repository
Saving 22 charts
Downloading postgresql from repo https://charts.bitnami.com/bitnami
Error: could not download https://charts.bitnami.com/bitnami/postgresql-13.4.4.tgz: Get "https://charts.bitnami.com/bitnami/postgresql-13.4.4.tgz": read tcp 10.1.0.186:38644->13.225.47.67:443: read: connection reset by peer'

# ── (и) НАШЕ, и послабление выдаваться НЕ ДОЛЖНО: архива по адресу нет.
#        Законный близнец (з): фраза о неудавшемся скачивании ТА ЖЕ, но хост
#        ответил — и ответил, что такого файла у него нет. Это мёртвый пин либо
#        опечатка в адресе; повтор не лечит ни того, ни другого, а признание
#        отказа чужим спрятало бы их навсегда.
check "архива по адресу нет (404) — наш мёртвый пин" transient \
'Saving 22 charts
Downloading postgresql from repo https://charts.bitnami.com/bitnami
Error: could not download https://charts.bitnami.com/bitnami/postgresql-99.99.99.tgz: failed to fetch https://charts.bitnami.com/bitnami/postgresql-99.99.99.tgz : 404 Not Found'

# ── (к) ПОДСКАЗКА ОБ АДРЕСЕ — ДИАГНОСТИКА, А НЕ ДОКАЗАТЕЛЬСТВО.
#
# Проверяется отдельно от класса ровно затем, чтобы их снова не связали. Связаны
# они были: класс считался всеми тремя формами, а ИСХОД (отметка и код 3) — тем,
# извлёкся ли адрес формой ПЕРВОЙ. Формы вторая и третья классифицировались верно
# и всё равно кончались красным «доказательства НЕТ».
hint_case() { # имя, ожидание (текст либо ПУСТО), вывод helm
  local name=$1 want=$2 text=$3
  printf '%s\n' "$text" > "$tmp/log.txt"
  local got; got=$(external_source_hint "$tmp/log.txt")
  local shown=${got:-ПУСТО}
  if [ "$shown" = "$want" ]; then
    pass=$((pass+1)); echo "  [ok] подсказка: $name → $shown"
  else
    fail=$((fail+1)); echo "  [ПРОВАЛ] подсказка: $name → $shown, ждали $want"
  fi
}
hint_case "форма первая называет репозиторий" https://charts.bitnami.com/bitnami \
'...Unable to get an update from the "https://charts.bitnami.com/bitnami" chart repository:
	failed to fetch https://charts.bitnami.com/bitnami/index.yaml : 502 Bad Gateway'
hint_case "форма вторая называет репозиторий" https://k8s.ory.sh/helm/charts \
'Save error occurred:  could not find : chart hydra not found in https://k8s.ory.sh/helm/charts: looks like "https://k8s.ory.sh/helm/charts" is not a valid chart repository or cannot be reached: Get "https://k8s.ory.sh/helm/charts/index.yaml": read tcp 10.1.0.27:33630->185.199.111.153:443: read: connection reset by peer'
hint_case "форма третья называет АРХИВ, а не репозиторий" \
  https://charts.bitnami.com/bitnami/postgresql-13.4.4.tgz \
'Error: could not download https://charts.bitnami.com/bitnami/postgresql-13.4.4.tgz: Get "https://charts.bitnami.com/bitnami/postgresql-13.4.4.tgz": read tcp 10.1.0.186:38644->13.225.47.67:443: read: connection reset by peer'
# Подсказка ПУСТА — и это законный исход, а не отказ: класс уже назван, а исход
# читает КЛАСС. Случай назван здесь именно затем, чтобы пустая подсказка не
# выглядела поводом понизить вердикт до красного.
hint_case "чужая недоступность без разбираемого адреса → подсказки нет" ПУСТО \
'Error: chart x not found in : looks like "" is not a valid chart repository or cannot be reached: net/http: TLS handshake timeout'

echo
echo "инъекция классификатора: утверждений $((pass+fail)), пройдено $pass, провалено $fail"
[ "$fail" -eq 0 ] || exit 1
