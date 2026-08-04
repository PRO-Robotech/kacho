#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# registry: кто вправе передавать чужую личность — и видит ли это гейт посадки.
#
# Предмет. Публичный листенер kacho-registry читал заголовки x-kacho-principal-*
# безусловно (grpcsrv.UnaryPrincipalExtract), а внутренний строил доверенную пару с
# ПУСТЫМ списком отправителей, что по контракту corelib означает «доверяем любому
# пиру с проверенным сертификатом». Теперь оба листенера строят доверенную пару со
# списком из конфигурации, а боевой режим на пустом списке не стартует.
#
# Этот файл закрывает то, что Go-тесты закрыть не могут:
#   (1) чарт РЕАЛЬНО отдаёт ручку в окружение пода, и её дефолт непуст (профиль,
#       забывший ручку, не должен отгружать «доверяем любому»);
#   (2) боевой профиль повторяет её явно (правка профиля не может её потерять);
#   (3) гейт посадки ГРАДУИРУЕТ измерение для registry. Именно тут прячется класс
#       «форма без содержания»: сервис может честно отчитываться полем, а вердикт
#       гейта на это поле не смотреть — и красный под печатает галочку.
#
# Offline-харнесс (без кластера): рендер под-чарта + чтение профиля + прогон
# ВЕРДИКТА САМОГО ГЕЙТА (программа jq вынимается из скрипта, а не переписывается
# здесь — копия разъехалась бы с оригиналом и снова ничего не проверяла).
set -euo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_ROOT/.." && pwd)"
CHART="$REPO_ROOT/services/registry/deploy"
PROD="$DEPLOY_ROOT/helm/umbrella/values.prod.yaml"
GATE="$DEPLOY_ROOT/scripts/assert-production-posture.sh"

GATEWAY_SAN="spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"
KNOB="KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS"

N=0
fail() { echo "FAIL: $1"; exit 1; }
ok() { N=$((N + 1)); }

[ -d "$CHART" ] || fail "registry chart not found at $CHART"
[ -f "$PROD" ] || fail "values.prod.yaml not found at $PROD"
[ -f "$GATE" ] || fail "posture gate not found at $GATE"

# env_val <render> — значение ручки в контейнере (пусто, если её нет). Читаем
# текстом, а не через yq: в разных средах под этим именем стоят два разных
# инструмента с несовместимым синтаксисом, и тест не должен зависеть от того,
# какой из них оказался в PATH.
env_val() {
  printf '%s\n' "$1" | grep -A1 -- "- name: $KNOB" | sed -n 's/^ *value: *//p' | head -1 | sed 's/^"//; s/"$//'
}

# yaml_block_value <file> <top-level-key> <leaf-key> — значение leaf-ключа внутри
# блока верхнего уровня (пусто, если ключа там нет).
yaml_block_value() {
  awk -v top="$2:" -v leaf="$3:" '
    $0 ~ "^"top"$" { inblk = 1; next }
    inblk && /^[A-Za-z]/ { inblk = 0 }
    inblk && $1 == leaf { sub(/^[^:]*:[ \t]*/, ""); gsub(/^"|"$/, ""); print; exit }
  ' "$1"
}

# ЗАГЛУШКА УЧЁТНЫХ ДАННЫХ ХРАНИЛИЩА СЛОЁВ — предусловие рендера, а не послабление.
# Чарт ОТКАЗЫВАЕТ в рендере, если у zot нет ни секрета оператора, ни пары
# логин/пароль: развернуть открытое хранилище слоёв он не умеет (см. fail в
# templates/statefulset-zot.yaml). Оба профиля эту пару задают; standalone-рендер —
# нет, поэтому здесь она подставляется тем же способом, каким соседний тест
# подставляет db.password. К предмету проверки (кто вправе передавать чужую
# личность) значения отношения не имеют, и НИ ОДНО утверждение о них не смягчено:
# без них проверка вообще не выполнялась бы.
ZOT_STUB=(--set zot.auth.username=selftest --set zot.auth.password=selftest)

# ── 1. Дефолт чарта непуст и пинит именно gateway ────────────────────────────
# Единственный законный отправитель установлен по графу импортов: заглушки
# pkg/api/kacho/cloud/registry/v1 вне самого сервиса импортирует только
# gateway/internal/restmux, и он же держит адреса ОБОИХ листенеров.
DEFAULT_RENDER="$(helm template registry "$CHART" "${ZOT_STUB[@]}" --show-only templates/deployment.yaml 2>/dev/null)" \
  || fail "registry chart does not render"
def_val="$(env_val "$DEFAULT_RENDER")"
[ -n "$def_val" ] && [ "$def_val" != "null" ] \
  || fail "$KNOB отсутствует у пода ИЛИ пуст по умолчанию: профиль, не задавший ручку, отгрузил бы «доверяем любому пиру с сертификатом» (в боевом режиме — отказ старта)"
[ "$def_val" = "$GATEWAY_SAN" ] \
  || fail "дефолт чарта = '$def_val', ожидается '$GATEWAY_SAN' (профиль без ручки не должен отгружать «доверяем любому»)"
ok

# ── 2. Оператор всё ещё может выразить пустой список ─────────────────────────
# Чарт не решает за стражу: пусто он отрендерит, а откажет в старте боевой режим
# (validateSecurityConfig → requireTrustedForwarders). Так «пусто» остаётся
# наблюдаемым, а не подменяется чартом на дефолт втихую.
EMPTY_RENDER="$(helm template registry "$CHART" "${ZOT_STUB[@]}" --set authz.trustedForwarderSANs="" \
  --show-only templates/deployment.yaml 2>/dev/null)" || fail "render with an empty override failed"
empty_val="$(env_val "$EMPTY_RENDER")"
[ -z "$empty_val" ] || [ "$empty_val" = "null" ] \
  || fail "явно пустая ручка отрендерилась как '$empty_val' — чарт подменяет намерение оператора"
ok

# ── 3. Боевой профиль повторяет ручку явно ───────────────────────────────────
prod_val="$(yaml_block_value "$PROD" registry trustedForwarderSANs)"
[ "$prod_val" = "$GATEWAY_SAN" ] \
  || fail "values.prod.yaml registry.authz.trustedForwarderSANs='$prod_val', ожидается '$GATEWAY_SAN'"
ok

# ── 4. Гейт посадки участвует: registry в списке градуируемых ────────────────
grep -qE '^FORWARDER_NARROWING_REQUIRED=.*\bregistry\b' "$GATE" \
  || fail "registry не входит в FORWARDER_NARROWING_REQUIRED — гейт не станет градуировать измерение, и под, никого не сузивший, напечатает галочку"
ok

# ── 5. …и вердикт гейта действительно смотрит на это поле ────────────────────
# Программу jq вынимаем из самого гейта: переписанная здесь копия разъехалась бы с
# оригиналом, и проверка снова стала бы формой без содержания.
JQPROG="$(awk '/verdict="\$\(printf/{f=1} f{print} /join\(", "\)/{if(f) exit}' "$GATE" \
  | sed "1s/.*jq -r --argjson need_fwd \"\\\$need_fwd\" '//")"
JQPROG="${JQPROG%\')\"}"
[ -n "$JQPROG" ] || fail "не удалось вынуть программу вердикта из $GATE (гейт изменил форму — обнови тест)"

line() { # line <trusted_forwarders-фрагмент>
  printf '{"msg":"boot security posture","service":"registry","auth_mode":"production-strict",'
  printf '"db_sslmode":"require","public_mtls":true,"internal_mtls":true,"authz_check":true%s}' "$1"
}
verdict() { echo "$1" | jq -r --argjson need_fwd "$2" "$JQPROG"; }

# сузивший круг под проходит…
v="$(verdict "$(line ',"trusted_forwarders":true')" true)"
[ -z "$v" ] || fail "под, сузивший круг отправителей, забракован вердиктом: '$v'"
ok

# …не сузивший — валится…
v="$(verdict "$(line ',"trusted_forwarders":false')" true)"
case "$v" in *trusted_forwarders*) ;; *) fail "под, НЕ сузивший круг, прошёл вердикт: '$v'";; esac
ok

# …устаревший бинарь без поля — тоже (fail-closed: неотчитанное не считается сделанным)…
v="$(verdict "$(line '')" true)"
case "$v" in *trusted_forwarders*) ;; *) fail "самоотчёт без поля прошёл вердикт: '$v'";; esac
ok

# …а сервис без этого механизма вердиктом не задет.
v="$(verdict "$(line ',"trusted_forwarders":false')" false)"
[ -z "$v" ] || fail "сервис вне списка градуируемых забракован по чужому измерению: '$v'"
ok

echo "$SCRIPT: OK ($N assertions)"
