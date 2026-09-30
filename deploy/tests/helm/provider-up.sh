#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# provider-up.sh — чужой стек личности, поднятый ВНУТРИ рендера пробы (#2735).
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ
#
# Поставщик личности, его издатель и сосед-терминатор административного
# слушателя издателя не поднимаются ни на одном стенде: выключение объявлено в
# базе зонта (helm/umbrella/values.yaml, раздел «ЧУЖОЙ СТЕК ЛИЧНОСТИ НЕ
# ПОДНИМАЕТСЯ»), и ни один профиль его не перекрывает. Шаблоны подчартов и
# настройки их подов в профилях лежат в дереве, пока подчарты не сняты
# физически (#1276), — и пробы формы этих подов судят именно их.
#
# Рендер стенда таблицы их больше не производит, поэтому проба поднимает нужную
# часть стека сама — ОДНИМ фактом поверх настоящей цепочки, и только там, где
# профиль цепочки сам объявляет настройки этой части. Цепочка, их не
# объявившая, поднятой не становится: иначе проба судила бы конфигурацию,
# которую не объявлял никто.
#
# Что это НЕ утверждает: что стенд поднимает поставщика. Живой стенд его не
# поднимает; о живом отсутствии судят гейты посадки own
# (deploy/own_posture_foreign_identity_test.go, живая половина —
# deploy/scripts/assert-admin-hop-transport.sh). Проба, поднявшая часть стека,
# обязана сказать это в своей переписи.
#
# Имя НЕ оканчивается на `-test.sh` намеренно: цель `helm-manifest-test`
# перебирает `tests/helm/*-test.sh`, и библиотека в этом переборе запускалась бы
# как проверка.
#
# Использование — как библиотека:  . "$(dirname "$0")/provider-up.sh"
#   ISSUER_UP_ARGS          → аргументы helm, поднимающие издателя поставщика
#   PROVIDER_UP_ARGS        → издателя И соседа-терминатора его слушателя
#   IDENTITY_STORE_UP_ARGS  → службу личности поставщика и наши карты её настроек
#   EXTERNAL_POSTURE_ARGS   → посадку `external` обеим половинам (служба и край)
#   provider_part_declared <часть> <профиль>… — объявляет ли цепочка настройки
#       части стека; <часть>: terminator | issuer | identity-store.
#       Код 0 — объявляет; 1 — не объявляет; 2 — профили не разобраны.
#   provider_terminator_declared <профиль>… — то же для `terminator`.
#
# Профили передаются ПУТЯМИ, слева направо, как их получает helm; первым
# обязан идти values.yaml зонта — иначе судится не то, что helm сольёт.
#
# Опции оболочки здесь НЕ выставляются — по той же причине, что в stacks.sh.

# shellcheck disable=SC2034  # читается подключающим гейтом
ISSUER_UP_ARGS=(--set hydra.enabled=true)
# shellcheck disable=SC2034
PROVIDER_UP_ARGS=("${ISSUER_UP_ARGS[@]}" --set mtls.hydraAdminTls.enabled=true)
# shellcheck disable=SC2034
IDENTITY_STORE_UP_ARGS=(--set kratos.enabled=true
                        --set kaname.kratos.config.enabled=true
                        --set kaname.kratos.identitySchema.enabled=true)
# shellcheck disable=SC2034
EXTERNAL_POSTURE_ARGS=(--set kaname.config.authn.identityProvider=external
                       --set api-gateway.authn.identityProvider=external)

# provider_part_declared <часть> <профиль>… — слияние профилей слева направо,
# как у helm, и вопрос к ПОБЕДИВШЕМУ значению:
#   terminator      — строка `hydra.deployment.extraContainers` зовёт шаблон
#                     соседа `kacho.hydraAdminTls.sidecar`;
#   issuer          — `hydra.hydra.config` — непустое отображение;
#   identity-store  — `kratos.kratos.config` — непустое отображение.
provider_part_declared() {
  local part="$1"; shift
  python3 - "$part" "$@" <<'PY'
import sys
try:
    import yaml
except ImportError:
    sys.exit(2)

def merge(dst, src):
    for k, v in (src or {}).items():
        if isinstance(v, dict) and isinstance(dst.get(k), dict):
            merge(dst[k], v)
        else:
            dst[k] = v
    return dst

part, paths = sys.argv[1], sys.argv[2:]
if not paths:
    sys.exit(2)
merged = {}
try:
    for path in paths:
        with open(path) as fh:
            merge(merged, yaml.safe_load(fh) or {})
except (OSError, yaml.YAMLError):
    sys.exit(2)

def node(*keys):
    cur = merged
    for k in keys:
        if not isinstance(cur, dict):
            return None
        cur = cur.get(k)
    return cur

if part == "terminator":
    containers = node("hydra", "deployment", "extraContainers")
    sys.exit(0 if isinstance(containers, str) and "kacho.hydraAdminTls.sidecar" in containers else 1)
if part == "issuer":
    cfg = node("hydra", "hydra", "config")
    sys.exit(0 if isinstance(cfg, dict) and cfg else 1)
if part == "identity-store":
    cfg = node("kratos", "kratos", "config")
    sys.exit(0 if isinstance(cfg, dict) and cfg else 1)
# Неизвестная часть — отказ разбора, а не «не объявляет»: опечатка вызывающего
# иначе читалась бы как законное отсутствие предмета.
sys.exit(2)
PY
}

provider_terminator_declared() { provider_part_declared terminator "$@"; }
