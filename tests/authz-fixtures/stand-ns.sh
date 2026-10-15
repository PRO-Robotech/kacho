#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stand-ns.sh — пространство имён стенда для оболочки (задача #2882). Подключается
# через `.`; правило то же, что у stand_ns.py для python, и их равенство на всех
# сочетаниях судит tests/authz-fixtures/stand_namespace_test.py (ось P):
#
#   * задана одна из SETUP_NS / KACHO_NAMESPACE — она и есть пространство стенда;
#   * заданы обе и совпадают — то же значение;
#   * заданы обе и РАСХОДЯТСЯ — отказ (код 2) до первого обращения к кластеру:
#     какую имели в виду, посев не угадывает;
#   * не задана ни одна — kacho, умолчание подъёма стенда.
#
# Своя реализация в оболочке, а не вызов python: setup.sh исполняется пробой
# посадки (internal/repohygiene/seedposturegate_test.go) с заглушкой python3, и
# ответ из заглушки был бы пустым именем.
#
# stand_ns_resolve — ставит и экспортирует STAND_NS, SETUP_NS и KACHO_NAMESPACE
# одним значением, чтобы каждый следующий шаг пришёл к тому же ответу.
stand_ns_resolve() {
  local a="${SETUP_NS:-}" b="${KACHO_NAMESPACE:-}"
  if [ -n "$a" ] && [ -n "$b" ] && [ "$a" != "$b" ]; then
    echo "[stand-ns] FATAL: пространство имён стенда задано двумя переменными по-разному" \
      "(SETUP_NS=$a, KACHO_NAMESPACE=$b); посев не угадывает, какую имели в виду." \
      "Оставьте одну либо задайте обе одинаково." >&2
    return 2
  fi
  STAND_NS="${a:-${b:-kacho}}"
  SETUP_NS="$STAND_NS"
  KACHO_NAMESPACE="$STAND_NS"
  export STAND_NS SETUP_NS KACHO_NAMESPACE
}
