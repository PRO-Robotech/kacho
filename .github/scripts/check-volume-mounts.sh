#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# check-volume-mounts.sh — каждый volumeMount обязан иметь свой volume, во ВСЕХ
# комбинациях тумблеров чарта.
#
# ЗАЧЕМ. `helm template` этого НЕ ловит, и `helm lint` тоже: манифест с монтом на
# несуществующий том рендерится успешно и валится только на apiserver — уже при деплое:
#   Deployment.apps "compute" is invalid:
#     spec.template.spec.containers[0].volumeMounts[0].name: Not found: "tmp"
#
# Поймано на себе 2026-07-16 при добавлении /tmp под readOnlyRootFilesystem: секция
# `volumes:` у compute была гейтована `{{- if or .Values.opa.enabled .Values.mtls.enable }}`,
# и при обоих false (ровно профиль umbrella-фазы 1, MTLS_OFF) том исчезал, а монт на него
# оставался. Standalone `helm template services/compute/deploy` проблему НЕ показывал —
# там ДЕФОЛТНЫЕ values, где opa/mtls включены. Поэтому здесь проверяется МАТРИЦА
# тумблеров, а не дефолт: гейт ловит ровно то, чего не видит обычный рендер.
#
# Локально: bash .github/scripts/check-volume-mounts.sh
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1

ANALYZER="$(dirname "${BASH_SOURCE[0]}")/check-volume-mounts.py"
rc=0

# verify <label> <chart-dir> [--set ...]
verify() {
  local label="$1" chart="$2"; shift 2
  local out bad st

  out=$(helm template t "$chart" "$@" 2>&1)
  if grep -qiE '^Error|unexpected \{\{' <<<"$out"; then
    echo "  ✗ $label — рендер упал:"; grep -iE '^Error|unexpected' <<<"$out" | head -2 | sed 's/^/      /'
    rc=1; return
  fi

  bad=$(printf '%s' "$out" | python3 "$ANALYZER" 2>&1); st=$?

  # Падение самого анализатора — ПРОВАЛ, а не «✓». Иначе пустой вывод читается как «нет
  # находок», и гейт зеленеет на собственной поломке — ровно тот класс, который он ловит.
  if [ "$st" -eq 2 ] || { [ "$st" -ne 0 ] && [ "$st" -ne 1 ]; }; then
    echo "  ✗ $label — анализатор упал (exit $st):"; sed 's/^/      /' <<<"$bad"; rc=1; return
  fi
  if [ "$st" -eq 1 ]; then
    echo "  ✗ $label:"; sed 's/^/      /' <<<"$bad"; rc=1; return
  fi
  echo "  ✓ $label"
}

# Матрица тумблеров, ГЕЙТЯЩИХ секции volumes/volumeMounts — именно их комбинации
# расходятся с дефолтом чарта.
for opa in false true; do
  for mtls in false true; do
    verify "vpc (opa=$opa mtls=$mtls)"     services/vpc/deploy \
      --set image=x --set name=vpc --set opa.enabled="$opa" --set mtls.enable="$mtls"
    verify "compute (opa=$opa mtls=$mtls)" services/compute/deploy \
      --set image=x --set name=compute --set opa.enabled="$opa" --set mtls.enable="$mtls"
  done
done

for tls in false true; do
  for mtls in false true; do
    verify "gateway (tls=$tls mtls=$mtls)" gateway/deploy \
      --set image=x --set name=api-gateway --set tls.enabled="$tls" --set mtls.enable="$mtls"
  done
done

for mig in false true; do
  verify "storage (migrator=$mig)" services/storage/deploy \
    --set image.repository=x --set image.tag=y --set migrator.enabled="$mig"
done

exit $rc
