#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
set -euo pipefail
# Креденшелы КАЖДОЙ базы зонта: подчарт bitnami создаёт секрет `<релиз>-<псевдоним>`.
#
# Перечень назван ПОИМЁННО и держится гейтом дерева
# `deploy/db_credential_probe_covers_every_instance_test.go` в обе стороны: имя,
# которого зонт не объявляет псевдонимом `pg-*`, — находка (проба утверждала бы
# о несуществующем), и псевдоним, здесь не названный, — тоже находка
# (утверждение стало бы половинным и зеленело бы как покрытие).
#
# Прежняя редакция называла три имени из девяти, и одно из трёх —
# `resource-manager` — было упразднено (KAC-124): зонт не рендерит такого
# секрета ни в одном профиле.
#
# Стенд здесь — `make dev-up` (профиль values.dev.yaml), где включены все девять.
for svc in compute geo hydra iam kratos nlb registry storage vpc; do
  kubectl -n kacho get secret "kacho-umbrella-pg-${svc}" >/dev/null
done
echo "PASS: E5 — db-credential secrets present: 9"
