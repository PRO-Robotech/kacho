#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# lint-service-charts.sh — сервисный чарт обязан РЕНДЕРИТЬСЯ, а не только разбираться.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Шаг конвейера гонял по сервисным чартам голый `helm lint`. Три чарта из девяти
# ОСОЗНАННО отказывают в рендере, пока не названы координаты ЧУЖОГО кластера, —
# у kaname их четыре (образ, узел базы, имя секрета с паролем, ключ в нём), у
# kacho-nlb пароль базы, у registry учётные данные хранилища слоёв. Отказ
# правильный: непустое умолчание называло бы объект НАШЕЙ установки, читалось бы
# настройкой и отказало бы уже в кластере — на вытягивании образа или на
# недостижимой базе.
#
# Гейт обязан такой отказ УДОВЛЕТВОРИТЬ, а не наказывать за fail-closed дизайн, —
# ровно та же норма, что у .github/scripts/check-volume-mounts.py (поле
# `required` в его таблице чартов). Здесь она применена ко второму месту, где
# сервисные чарты рендерятся.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ОДНОГО `helm lint` НЕДОСТАТОЧНО — ИЗМЕРЕНО, А НЕ ПРЕДПОЛОЖЕНО
#
# helm 4 видит ошибки РАЗБОРА и слеп к ошибкам ИСПОЛНЕНИЯ. Контроль в обе
# стороны на копии чарта services/vpc/deploy (helm v4.2.4):
#
#   нетронутая копия                       → код 0
#   {{ fail "…" }}      (отказ рендера)    → код 0   ← слеп
#   {{ zzNoSuchFunc }}  (ошибка разбора)   → код 1   ← видит
#   невалидный YAML на выходе              → код 0   ← слеп
#
# Отсюда следствие, из-за которого этот файл и написан: «0 chart(s) failed» у
# трёх отказывающих чартов НЕ ОЗНАЧАЕТ НИЧЕГО — они не рендерились ни разу, а
# полный текст их отказа печатался в лог строкой `funcMap fail` на уровне INFO и
# читался как красное. Подать координаты и оставить один `helm lint` значило бы
# КУПИТЬ МОЛЧАНИЕ: шум ушёл бы, способность упасть не появилась. Поэтому здесь
# `helm template` рядом с `helm lint`, и он-то и выносит вердикт о рендере.
#
# ─────────────────────────────────────────────────────────────────────────────
# ВЕДОМОСТЬ САМОИСТЕКАЕТ В ОБЕ СТОРОНЫ
#
# Запись даётся ровно тому чарту, который БЕЗ неё отказывает. Обе стороны —
# находка:
#   • чарт отказывает голым, записи НЕТ  → находка (его рендер не проверялся бы);
#   • запись есть, а чарт рендерится голым → находка (исключению нечего
#     исключать — координата перестала быть обязательной, и запись переживает
#     свой предмет).
# Ведомость не заменяет решения владельца чарта: снял обязательность — снимай и
# запись, тем же изменением.
#
# ЗНАЧЕНИЯ ЗАВЕДОМО СИНТЕТИЧЕСКИЕ и не описывают ни один наш стенд: домен
# `.invalid` не резолвится by construction (RFC 2606), остальное несёт префикс
# `zz-lint-`. Правдоподобное значение здесь выдавало бы себя за рабочую
# настройку — тот же довод, что у `render-guard-not-a-real-password` в таблице
# check-volume-mounts.py.
#
# Локально:     bash .github/scripts/lint-service-charts.sh
# Самопроверка: bash .github/scripts/lint-service-charts.sh --self-test
set -uo pipefail

# ── ведомость: <путь чарта>|<аргументы helm> ────────────────────────────────
LEDGER='services/iam/deploy|--set image=zz-lint.invalid/kaname:zz-lint --set db.host=zz-lint-postgres.zz-lint.svc.invalid --set db.passwordSecretName=zz-lint-db --set db.passwordSecretKey=zz-lint-password
services/nlb/deploy|--set db.password=zz-lint-not-a-real-password
services/registry/deploy|--set zot.auth.password=zz-lint-not-a-real-password'

# charts <корень> — перечень ВЫВОДИТСЯ из индекса git, а не выписывается.
#
# Отбор по глубине обязателен: pathspec `services/*/deploy/Chart.yaml` в git
# пересекает `/`, поэтому без него в перечень попадают ещё семь чартов сайтов
# документации (`services/<svc>/docs/deploy`), которых шаг никогда не судил.
# Шелловский шаблон `/` не пересекает — потому прежняя форма их и не видела.
charts() {
  git -C "$1" ls-files -- \
      'services/*/deploy/Chart.yaml' 'gateway/deploy/Chart.yaml' 'ui-future/deploy/Chart.yaml' 2>/dev/null |
    grep -E '^(services/[^/]+/deploy|gateway/deploy|ui-future/deploy)/Chart\.yaml$' |
    sed 's#/Chart\.yaml$##'
}

# coords <ведомость> <чарт> — аргументы записи либо пусто.
coords() { printf '%s\n' "$1" | awk -F'|' -v c="$2" '$1 == c { print $2; found = 1 } END { exit !found }'; }

# sweep <корень> <ведомость> — обход и вердикт. 0 — чисто, 1 — находка, 2 — не выполнилось.
#
# ЧИСЛО ДОКУМЕНТОВ ПЕЧАТАЕТСЯ ПО КАЖДОМУ ЧАРТУ намеренно: «рендер прошёл» и
# «рендер что-то произвёл» — разные утверждения, и первое зеленеет на чарте без
# единого шаблона. Такой в дереве есть (`services/geo/deploy` — только Chart.yaml
# и values.yaml, ноль документов), и голым `helm lint` он был неотличим от
# работающего. Гейт на этом НЕ падает: пустой чарт — отдельный предмет с
# отдельным владельцем, и чинить его здесь значило бы вести в одном изменении две
# линии. Но невидимым он больше не будет.
sweep() {
  local root="$1" ledger="$2" n=0 supplied=0 bad=0 docs=0 d c args out rc

  while IFS= read -r c; do
    [ -n "$c" ] || continue
    n=$((n + 1))
    args="$(coords "$ledger" "$c")" || args=""

    # 1. Разбор. Единственное, что `helm lint` умеет находить (см. замер выше).
    # shellcheck disable=SC2086
    out="$(helm lint "$root/$c" $args 2>&1)"; rc=$?
    if [ $rc -ne 0 ]; then
      bad=$((bad + 1)); echo "НАХОДКА: $c — helm lint отверг чарт"; echo "$out" | sed 's/^/    /'
      continue
    fi

    # 2. Рендер. Вердикт о том, что чарт собирается, выносит он, а не lint.
    # shellcheck disable=SC2086
    out="$(helm template zz-lint "$root/$c" $args 2>&1 >/dev/null)"; rc=$?
    if [ $rc -ne 0 ]; then
      bad=$((bad + 1))
      if [ -n "$args" ]; then
        echo "НАХОДКА: $c — не рендерится ДАЖЕ с координатами ведомости ($args)"
      else
        echo "НАХОДКА: $c — не рендерится, а координат ведомость ему не даёт."
        echo "    Чарт вправе отказывать без координат чужого кластера — тогда заведите ему"
        echo "    запись в ведомости этого гейта. Отказ чарта:"
      fi
      echo "$out" | sed 's/^/    /' | head -12
      continue
    fi

    d="$(helm template zz-lint "$root/$c" $args 2>/dev/null | grep -c '^kind:')"
    docs=$((docs + d))

    [ -n "$args" ] || { echo "  ок      $c — документов $d"; continue; }

    # 3. Самоистечение: запись живёт, пока БЕЗ неё чарт отказывает.
    supplied=$((supplied + 1))
    if helm template zz-lint "$root/$c" >/dev/null 2>&1; then
      bad=$((bad + 1))
      echo "НАХОДКА: $c — запись ведомости пережила свой предмет: чарт рендерится и БЕЗ координат."
      echo "    Снимите запись тем же изменением, которым снята обязательность."
      continue
    fi
    echo "  ок      $c — документов $d (координат подано ведомостью)"
  done <<< "$(charts "$root")"

  # «Ноль находок» обязано быть отличимо от «ноль прочитанного».
  if [ "$n" -eq 0 ]; then
    echo "FAIL: не осмотрено НИ ОДНОГО чарта — обходить было нечего, вердикт беспредметен"
    return 1
  fi
  echo "осмотрено: чартов $n · документов $docs · из них потребовали координат $supplied · находок $bad"
  [ "$bad" -eq 0 ] || return 1
  return 0
}

# ── самопроверка: инъекция в обе стороны на синтетическом дереве ────────────
mkchart() { # mkchart <корень> <путь> <тело шаблона>
  mkdir -p "$1/$2/templates"
  printf 'apiVersion: v2\nname: %s\nversion: 0.0.0\n' "$(basename "$(dirname "$1/$2")")zz" > "$1/$2/Chart.yaml"
  printf 'zz: ""\n' > "$1/$2/values.yaml"
  printf '%s\n' "$3" > "$1/$2/templates/cm.yaml"
  ( cd "$1" && git add -A >/dev/null 2>&1 )
}

self_test() {
  command -v helm >/dev/null 2>&1 || { echo "FATAL: нужен helm — «не выполнилось» не идёт в зачёт «прошло»"; return 2; }
  local t ok=0 cases=0
  t="$(mktemp -d)"; trap 'rm -rf "$t"' RETURN
  git -C "$t" init -q >/dev/null 2>&1
  git -C "$t" config user.email zz@zz.invalid >/dev/null 2>&1
  git -C "$t" config user.name zz >/dev/null 2>&1

  local plain='apiVersion: v1
kind: ConfigMap
metadata:
  name: zz
data:
  a: "b"'
  local needy='{{- if not .Values.zzKey }}{{ fail "нужна координата zzKey" }}{{- end }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: zz
data:
  a: "{{ .Values.zzKey }}"'

  mkchart "$t" "services/plain/deploy" "$plain"
  mkchart "$t" "services/needy/deploy" "$needy"

  check() { # check <подпись> <ожидаемый код> <ведомость> <искомая строка>
    cases=$((cases + 1))
    local out rc
    out="$(sweep "$t" "$3" 2>&1)"; rc=$?
    # Сравнение БЕЗ внешнего процесса: `grep -q` выходит до конца входа, писатель
    # слева получает SIGPIPE, и под `pipefail` найденное объявляется ненайденным
    # (гейт internal/repohygiene TestPipefailVerdictNeverComesFromAPipe, задача #658).
    if [ "$rc" = "$2" ] && [[ "$out" == *"$4"* ]]; then
      echo "  ОК  $1 — код $rc, назван «$4»"
    else
      ok=1; echo "  ПРОВАЛ  $1 — код $rc (ждали $2), искали «$4»:"; printf '%s\n' "$out" | sed 's/^/      /'
    fi
  }

  # Отрицание в паре с положительным: без контроля «красное всегда» неотличимо от работы.
  check "контроль: обоим чартам координат хватает" 0 \
        'services/needy/deploy|--set zzKey=zz-lint-value' 'осмотрено: чартов 2 · документов 2 · из них потребовали координат 1 · находок 0'
  check "чарт отказывает, записи НЕТ" 1 '' 'координат ведомость ему не даёт'
  check "запись есть, а предмета у неё НЕТ" 1 \
        'services/plain/deploy|--set zz=zz-lint-value' 'пережила свой предмет'
  check "координат записи НЕ хватает" 1 \
        'services/needy/deploy|--set zzOther=zz-lint-value' 'не рендерится ДАЖЕ с координатами'

  # Пустой обход — находка, а не зелёное.
  local e; e="$(mktemp -d)"; git -C "$e" init -q >/dev/null 2>&1
  cases=$((cases + 1))
  local out rc; out="$(sweep "$e" '' 2>&1)"; rc=$?
  if [ "$rc" = 1 ] && [[ "$out" == *"НИ ОДНОГО чарта"* ]]; then
    echo "  ОК  пустой обход → находка, а не зелёное — код $rc"
  else
    ok=1; echo "  ПРОВАЛ  пустой обход — код $rc: $out"
  fi
  rm -rf "$e"

  echo
  echo "случаев проверено: $cases"
  [ "$ok" -eq 0 ] && { echo "PASS: самопроверка гейта сервисных чартов"; return 0; }
  echo "FAIL: самопроверка гейта сервисных чартов"; return 1
}

cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 2

if [ "${1:-}" = "--self-test" ]; then
  self_test; exit $?
fi

command -v helm >/dev/null 2>&1 || {
  echo "FATAL: нужен helm — без него не будет отрендерен ни один чарт, а «не выполнилось»"
  echo "       не идёт в зачёт «прошло»."
  exit 2
}

sweep "$PWD" "$LEDGER"
