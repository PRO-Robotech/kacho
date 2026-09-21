#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# cilium-policy-dry-run.sh — рендер сетевых политик и их проверка, с ТРЕМЯ
# РАЗЛИЧИМЫМИ ИСХОДАМИ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ (#2038)
#
# Рецепт `cilium-policy-dry-run` в deploy/Makefile был записан так:
#
#     command -v kubeconform >/dev/null && kubeconform … || echo "(kubeconform not installed…)"
#
# Форма `A && B || C` даёт `C` не только когда нет `A`, но и когда ОТКАЗАЛО `B`:
# `||` срабатывает на любом ненулевом коде левой части. Значит при УСТАНОВЛЕННОМ
# инструменте и УПАВШЕЙ валидации рецепт печатал «kubeconform not installed» и
# выходил нулём.
#
# Это худшая форма класса «мягкий проход при отказе не отличает настройку от
# сбоя»: сообщение называет НЕ ТУ причину. Читатель идёт ставить инструмент,
# которого не хватает только на бумаге, а найденное нарушение остаётся
# ненайденным. Проверка при этом присутствует, исполняется и не отказала ни разу
# за свою жизнь.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ СКРИПТ, А НЕ РЕЦЕПТ
#
# 1. Рецепт make исполняется построчно оболочкой, и различение исходов внутри
#    него выражается ровно теми `&&`/`||`, которые этот дефект и породили.
# 2. Код возврата через make НЕ ПРОХОДИТ: make отвечает своей двойкой на любой
#    упавший рецепт. Значит категорию обязан называть СЛОВОМ тот, кто её
#    наблюдал, а звать его можно и без make — напрямую, с настоящими кодами.
# 3. Логику в рецепте нельзя прогнать. Здесь она прогоняется: `--self-test`
#    запускает НАСТОЯЩИЙ этот скрипт, подменяя ровно один факт на случай.
#
# ─────────────────────────────────────────────────────────────────────────────
# ИСХОДЫ
#
#   0 — ЗЕЛЕНО: манифесты отрендерены и валидация их приняла;
#   1 — КРАСНОЕ: рендер не состоялся ЛИБО валидация отвергла манифесты. В обоих
#       случаях причина названа своим именем — это разные отказы;
#   3 — НЕ ВЫПОЛНИЛОСЬ: условие не создано. Нет helm либо нет kubeconform:
#       вердикта о манифестах не вынесено НИ ОДНОГО, и в зачёт «прошло» это не
#       идёт. Сообщение об установке печатается ТОЛЬКО здесь — то есть только
#       когда инструмента действительно нет.
#
# Запуск:
#   deploy/scripts/cilium-policy-dry-run.sh [--out <файл>]
#   deploy/scripts/cilium-policy-dry-run.sh --self-test

set -uo pipefail

NOT_EXECUTED=3

DEPLOY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${CILIUM_POLICY_OUT:-/tmp/cilium-policies.yaml}"
SELFTEST=0

while [ $# -gt 0 ]; do
  case "$1" in
    --self-test) SELFTEST=1; shift ;;
    --out)       OUT="${2:-}"; shift 2 ;;
    *) echo "FATAL: неизвестный ключ '$1'"; exit 2 ;;
  esac
done

# ─────────────────────────────────────────────────────────────────────────────
render_and_check() {
  # ── ПРЕДПОСЫЛКА 1: рендерить нечем ────────────────────────────────────────
  if ! command -v helm > /dev/null 2>&1; then
    echo "НЕ ВЫПОЛНИЛОСЬ — УСЛОВИЕ НЕ СОЗДАНО: нет helm, рендерить манифесты нечем."
    echo "    Поставьте helm (https://helm.sh/docs/intro/install/). Вердикта о"
    echo "    политиках здесь НЕТ — это не «замечаний нет»."
    return "$NOT_EXECUTED"
  fi

  echo "Phase 10: rendering CiliumNetworkPolicy manifests..."
  local rc=0
  helm template kacho-umbrella "$DEPLOY_ROOT/helm/umbrella" \
      -f "$DEPLOY_ROOT/helm/umbrella/values.prod.yaml" \
      --set cilium.enabled=true \
      --show-only charts/cilium/templates/network-policies.yaml \
      --show-only charts/cilium/templates/cilium-mtls-enforce.yaml \
      > "$OUT" 2> "$OUT.err" || rc=$?
  if [ "$rc" -ne 0 ]; then
    # РЕНДЕР — НАШ ОТКАЗ, А НЕ НЕСОЗДАННОЕ УСЛОВИЕ: чарт в этом дереве, и его
    # нерендеримость есть находка о дереве.
    echo "КРАСНОЕ: рендер политик не состоялся (helm template, код $rc) — это отказ"
    echo "    НАШЕГО чарта, а не отсутствие инструмента:"
    sed 's/^/    | /' "$OUT.err" | head -20
    return 1
  fi
  rm -f "$OUT.err"
  echo "✓ Rendered to $OUT ($(wc -l < "$OUT") lines)"

  # ── ПРЕДПОСЫЛКА 2: проверять нечем ────────────────────────────────────────
  # Сообщение об установке стоит ЗДЕСЬ И ТОЛЬКО ЗДЕСЬ. В прежней записи оно
  # печаталось и при упавшей валидации — то есть называло не ту причину.
  if ! command -v kubeconform > /dev/null 2>&1; then
    echo "НЕ ВЫПОЛНИЛОСЬ — УСЛОВИЕ НЕ СОЗДАНО: kubeconform не установлен."
    echo "    Поставьте его (brew install kubeconform, либо релизы проекта), и"
    echo "    повторите. Манифесты отрендерены, но НЕ ПРОВЕРЕНЫ: вердикта о них"
    echo "    нет ни одного, и ноль находок здесь означает «не смотрели»."
    return "$NOT_EXECUTED"
  fi

  rc=0
  kubeconform -skip CiliumNetworkPolicy,CiliumClusterwideNetworkPolicy "$OUT" || rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "КРАСНОЕ: ВАЛИДАЦИЯ ОТВЕРГЛА манифесты (kubeconform, код $rc)."
    echo "    Инструмент установлен и отработал — причина в манифестах выше, а не"
    echo "    в установке. Прежняя запись рецепта печатала здесь «kubeconform not"
    echo "    installed» и выходила нулём: сообщение называло не ту причину."
    return 1
  fi
  echo "ЗЕЛЕНО: манифесты отрендерены и валидация их приняла."
  return 0
}

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА: инъекция в обе стороны, по одному подменённому факту на случай.
#
# Подменяются ТОЛЬКО инструменты (PATH из подставных), потому что предмет пробы —
# РАЗЛИЧЕНИЕ ИСХОДОВ, а не содержимое чарта. Сказано прямо: о самих политиках эта
# проба не утверждает ничего; о них судит настоящий прогон цели.
if [ "$SELFTEST" = "1" ]; then
  ok=0
  cases=0
  SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
  BIN="$(mktemp -d)"
  trap 'rm -rf "$BIN"' EXIT

  stub() { # имя код [что печатает]
    cat > "$BIN/$1" <<STUB
#!/usr/bin/env bash
prev=""
for a in "\$@"; do
  if [ "\$prev" = "--show-only" ] || [ "\$a" = "template" ]; then :; fi
  prev="\$a"
done
printf '%s\n' "${3:-}"
exit $2
STUB
    chmod +x "$BIN/$1"
  }

  run_case() { # ждём-код метка искомое-в-выводе [запрещённое-в-выводе]
    cases=$((cases + 1))
    local want="$1" label="$2" need="$3" forbid="${4:-}"
    local log; log="$(mktemp)"
    local got=0
    PATH="$BIN" bash "$SELF" --out "$BIN/out.yaml" > "$log" 2>&1 || got=$?
    if [ "$got" != "$want" ]; then
      echo "  ПРОВАЛ $label → код $got (ждали $want)"; sed 's/^/        /' "$log" | head -8; ok=1
    else
      echo "  ОК  $label → код $got"
    fi
    if [ -n "$need" ] && ! grep -qF "$need" "$log"; then
      echo "  ПРОВАЛ $label → в выводе нет «$need»"; ok=1
    fi
    if [ -n "$forbid" ] && grep -qF "$forbid" "$log"; then
      echo "  ПРОВАЛ $label → в выводе есть «$forbid», а причина здесь ДРУГАЯ"; ok=1
    fi
    rm -f "$log"
  }

  echo "=== cilium-policy-dry-run.sh --self-test ==="
  # Базовые подставные: оба инструмента есть и согласны.
  cp "$(command -v bash)" "$BIN/bash" 2>/dev/null || true
  for t in sed wc grep rm printf cat mktemp; do
    src="$(command -v "$t" 2>/dev/null)" && [ -n "$src" ] && cp "$src" "$BIN/" 2>/dev/null
  done

  # (1) КОНТРОЛЬ: оба инструмента есть, валидация принимает → зелено.
  stub helm 0 "apiVersion: cilium.io/v2"
  stub kubeconform 0 "Summary: 2 resources found - Valid: 2"
  run_case 0 "оба инструмента есть, валидация приняла" "ЗЕЛЕНО"

  # (2) ИНЪЕКЦИЯ: инструмент ЕСТЬ, валидация ОТВЕРГЛА → красное, и текст про
  #     валидацию, а НЕ про установку. Это ровно та подмена из тела задачи.
  stub kubeconform 1 "invalid: policy.yaml"
  run_case 1 "инструмент есть, валидация упала" "ВАЛИДАЦИЯ ОТВЕРГЛА" "не установлен"

  # (3) ЗАКОННЫЙ БЛИЗНЕЦ: инструмента НЕТ → сообщение про установку и третья
  #     категория. Без этой половины правка превратила бы отсутствие инструмента
  #     в находку о манифестах.
  rm -f "$BIN/kubeconform"
  run_case "$NOT_EXECUTED" "инструмента нет" "kubeconform не установлен" "ВАЛИДАЦИЯ ОТВЕРГЛА"

  # (4) РЕНДЕР НЕ СОСТОЯЛСЯ → красное, и причина названа рендером, а не
  #     валидацией: это НАШ чарт, и его нерендеримость — находка о дереве.
  stub kubeconform 0 "Summary: 2 resources found - Valid: 2"
  stub helm 1 ""
  run_case 1 "рендер упал" "рендер политик не состоялся" "не установлен"

  # (5) РЕНДЕРИТЬ НЕЧЕМ → третья категория, а не находка.
  rm -f "$BIN/helm"
  run_case "$NOT_EXECUTED" "нет helm" "нет helm" "КРАСНОЕ"

  echo "осмотрено случаев $cases; самопроверка: $([ $ok -eq 0 ] && echo PASS || echo FAIL)"
  exit $ok
fi

render_and_check
exit $?
