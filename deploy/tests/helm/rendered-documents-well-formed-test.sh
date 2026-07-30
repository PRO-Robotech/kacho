#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# КАЖДЫЙ ОТРЕНДЕРЕННЫЙ ДОКУМЕНТ — ДОКУМЕНТ: несёт apiVersion и kind.
#
# ЧТО ЭТО ЛОВИТ. `helm template` НЕ валидирует вывод — он печатает текст. Поэтому
# документ, у которого первый ключ съеден комментарием, проходит рендер без
# единого слова и падает только на `helm upgrade`, уже на кластере:
#
#   error validating "": error validating data: apiVersion not set
#
# Механика (реальный дефект, 2026-07-30, hydra-admin-certificate.yaml): каждое
# действие вида `{{- ... -}}` срезает перевод строки И слева, И справа. Когда
# преамбула файла — цепочка таких действий, весь блок сворачивается на последний
# литеральный текст файла, то есть на строку SPDX-комментария, а правый срез
# последнего действия убирает перевод строки перед первым ключом тела:
#
#   # SPDX-License-Identifier: BUSL-1.1apiVersion: cert-manager.io/v1
#
# `apiVersion` оказывается ВНУТРИ комментария. Документ синтаксически валиден как
# YAML (это просто отображение без одного ключа), поэтому ни рендер, ни `yq`, ни
# `yamllint` тут ничего не скажут — вопрос не к YAML, а к тому, что документ
# больше не описывает объект.
#
# ПОЧЕМУ ГЕЙТ НУЖЕН ИМЕННО ПО ПРОФИЛЯМ. Тот шаблон гейтирован на
# `mtls.hydraAdminTls.enabled`, который включён ТОЛЬКО в боевом наложении. Значит
# в отладочном профиле он не рендерится вовсе, и дефект существовал ровно там,
# где его не проверяли, — на профиле, который и обязан подниматься
# (security.md §«values.prod ОБЯЗАН реально boots, не только render-ится»).
# Поэтому проверяются ОБА профиля, и каждый — отдельным утверждением.
#
# ЧТО ЭТО НЕ ЛОВИТ (границы, чтобы гейт не выглядел шире, чем он есть): он не
# судит о СОДЕРЖИМОМ полей, не проверяет схему объекта и не заменяет
# `helm install` из dev-prod-up. Он утверждает ровно одно свойство — «каждый
# отрендеренный документ несёт apiVersion и kind» — и заявляет, сколько
# документов осмотрел, чтобы «ноль находок» отличалось от «ноль прочитанного».
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHART="$(cd "$HERE/../.." && pwd)/helm/umbrella"

command -v helm >/dev/null 2>&1 || { echo "ERROR: нужен helm"; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "ERROR: нужен python3"; exit 1; }

# Идентификаторы образов приезжают генерируемым файлом, которого в git нет
# (`make build-services`). Гейт не должен зависеть от того, собирали ли образы:
# если файла нет — рендерим без него.
IMG_VALUES="$CHART/values.image-ids.yaml"
IMG_ARG=()
[ -f "$IMG_VALUES" ] && IMG_ARG=(-f "$IMG_VALUES")

fail=0

# Проверяем документы одного профиля. $1 — метка, далее -f аргументы.
check_profile() {
  local label="$1"; shift
  local out rc
  out="$(mktemp)"
  if ! helm template kacho-umbrella "$CHART" -n kacho "$@" "${IMG_ARG[@]}" >"$out" 2>/tmp/rdwf-render.err; then
    echo "  !!! профиль $label: сам рендер не прошёл"
    sed -n '1,5p' /tmp/rdwf-render.err | sed 's/^/      /'
    rm -f "$out"
    return 1
  fi

  python3 - "$out" "$label" <<'PY'
import sys, yaml
path, label = sys.argv[1], sys.argv[2]
docs = list(yaml.safe_load_all(open(path, encoding='utf-8')))
# None-документы — это отделённые `---` пустые куски (шаблон под выключенным
# условием). Они не объекты и предметом утверждения не являются.
objs = [d for d in docs if isinstance(d, dict)]
bad = []
for d in objs:
    missing = [k for k in ('apiVersion', 'kind') if k not in d]
    if missing:
        name = (d.get('metadata') or {}).get('name', '<без имени>')
        bad.append(f"{d.get('kind', '<без kind>')}/{name}: нет {'+'.join(missing)}")
# Перепись — отдельное утверждение: «ноль находок» обязано отличаться от
# «ноль прочитанного».
print(f"  профиль {label}: осмотрено {len(objs)} документ(ов) из {len(docs)} кусков")
if not objs:
    print(f"  !!! профиль {label}: ноль документов — гейту нечего было проверять")
    sys.exit(1)
for b in bad:
    print(f"  !!! профиль {label}: {b}")
sys.exit(1 if bad else 0)
PY
  rc=$?
  rm -f "$out"
  return $rc
}

echo "=== каждый отрендеренный документ несёт apiVersion и kind ==="

check_profile "dev" -f "$CHART/values.dev.yaml" || fail=1
check_profile "production (values.dev-prod поверх dev)" \
  -f "$CHART/values.dev.yaml" -f "$CHART/values.dev-prod.yaml" || fail=1

if [ "$fail" -ne 0 ]; then
  echo
  # ОДИНАРНЫЕ кавычки: в двойных bash ИСПОЛНЯЕТ обратные кавычки, и диагностика
  # сама печатала «-}}: command not found» вместо того, что называет причину.
  echo 'ПРОВАЛ: документ без apiVersion/kind. Обычная причина — правый срез'
  echo '        `-}}` у последнего действия преамбулы: он склеивает первый ключ'
  echo '        тела с предыдущей литеральной строкой (как правило с комментарием).'
  echo '        Убери дефис в закрывающих скобках последнего действия перед телом.'
  exit 1
fi

echo "OK: во всех профилях каждый документ несёт apiVersion и kind"
