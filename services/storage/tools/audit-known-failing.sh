#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# audit-known-failing.sh — CI gate: запись «известное красное» не переживает свой
# фикс, а состав сюиты не отстаёт от сюиты.
#
# Тонкая обёртка; что именно проверяется и почему исключение обязано истекать само —
# на пакете tools/auditknownfailing (godoc).
#
# Одно измерение (жив ли тикет) требует трекера. Без него гейт НЕ падает, но и не
# проходит молча: каждая непроверенная запись печатается координатой и считается в
# переписи — «проверка не выполнялась» обязано быть отличимо от «проверка прошла».

set -euo pipefail
cd "$(dirname "$0")/.."

exec go run ./tools/auditknownfailing/cmd/audit-known-failing --root tests/newman "$@"
