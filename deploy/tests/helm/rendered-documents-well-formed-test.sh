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
# ВТОРОЕ СВОЙСТВО ТОГО ЖЕ КЛАССА — ИМЯ ПЕРЕМЕННОЙ В КОНТЕЙНЕРЕ ВСТРЕЧАЕТСЯ ОДИН
# РАЗ. Тот же механизм: `helm template` печатает текст и на две записи с одним
# именем не говорит ничего, а apiserver отвечает «hides previous definition of
# "…", which may be dropped when using apply» — и побеждает та запись, которую
# выбрал порядок блоков шаблона, а не тот, кто правил профиль. Для переменной
# посадки (режим окружения, включение mTLS, пути к ключу, круг законных
# отправителей) это значит, что действующее значение выбирается не там, где его
# объявляли. Измерено на боевой цепочке 2026-08-05: пять таких пар в контейнере
# края, шаблонный шаг сборки при этом зелёный.
#
# ПОЧЕМУ СЮДА ДОБАВЛЕНА БОЕВАЯ ЦЕПОЧКА. Профили values.prod.yaml и наложения
# площадки не рендерил НИ ОДИН манифест-гейт: обе цепочки, которые CI
# действительно поднимает, идут через values.dev. Пять пар выше жили ровно в той
# цепочке, которую никто не читал, — то есть предмет проверки был там, куда
# проверка не смотрела.
#
# ЧТО ЭТО НЕ ЛОВИТ (границы, чтобы гейт не выглядел шире, чем он есть): он не
# судит о СОДЕРЖИМОМ полей, не проверяет схему объекта и не заменяет
# `helm install` из dev-prod-up. Он утверждает ровно два свойства — «каждый
# отрендеренный документ несёт apiVersion и kind» и «имя переменной в контейнере
# не повторяется» — и заявляет, сколько документов и контейнеров осмотрел, чтобы
# «ноль находок» отличалось от «ноль прочитанного».
#
# ПРИЧИНУ повтора этот гейт не называет — он видит только следствие в рендере.
# Ту сторону держит декларативная проверка deploy/posture_parity_test.go: она
# читает ОБЪЯВЛЕНИЯ и запрещает объявлять родовым пробросом переменную, которую
# чарт печатает сам. Две проверки не дублируют друг друга: декларативная ловит и
# тот случай, когда дубликата в рендере нет (ручка выключена, защита доехала
# пробросом, а проверки связности рядом с ручкой не исполнились ни разу), а эта —
# и тот, когда два собственных блока чарта напечатали одно имя без всякого
# проброса.
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
import sys, yaml, collections
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

# Второе свойство: имя переменной в контейнере встречается один раз.
def pod_template(d):
    spec = d.get('spec') or {}
    if d.get('kind') in ('Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Job'):
        return spec.get('template')
    if d.get('kind') == 'CronJob':
        return ((spec.get('jobTemplate') or {}).get('spec') or {}).get('template')
    return None

seen_containers = 0
for d in objs:
    tmpl = pod_template(d)
    if not isinstance(tmpl, dict):
        continue
    pspec = tmpl.get('spec') or {}
    for section in ('initContainers', 'containers'):
        for c in pspec.get(section) or []:
            if not isinstance(c, dict):
                continue
            seen_containers += 1
            counts = collections.Counter(
                e.get('name') for e in (c.get('env') or []) if isinstance(e, dict))
            for env_name, n in sorted(counts.items()):
                if n > 1:
                    who = (d.get('metadata') or {}).get('name', '<без имени>')
                    bad.append(
                        f"{d.get('kind')}/{who} {section}/{c.get('name')}: переменная "
                        f"{env_name} объявлена {n} раз(а) — действующее значение выберет "
                        f"порядок блоков шаблона, а не профиль")

# Перепись — отдельное утверждение: «ноль находок» обязано отличаться от
# «ноль прочитанного».
print(f"  профиль {label}: осмотрено {len(objs)} документ(ов) из {len(docs)} кусков, "
      f"контейнеров {seen_containers}")
if not objs:
    print(f"  !!! профиль {label}: ноль документов — гейту нечего было проверять")
    sys.exit(1)
if not seen_containers:
    print(f"  !!! профиль {label}: ноль контейнеров — обход подов ничего не прочитал, "
          f"а не рендер стал чистым")
    sys.exit(1)
for b in bad:
    print(f"  !!! профиль {label}: {b}")
sys.exit(1 if bad else 0)
PY
  rc=$?
  rm -f "$out"
  return $rc
}

echo "=== документ несёт apiVersion и kind; имя переменной в контейнере одно ==="

check_profile "dev" -f "$CHART/values.dev.yaml" || fail=1
check_profile "production (values.dev-prod поверх dev)" \
  -f "$CHART/values.dev.yaml" -f "$CHART/values.dev-prod.yaml" || fail=1
# Боевые профили. Слоями они НЕ ложатся на dev: values.prod.yaml рендерится сам
# по себе, а наложение площадки — поверх него (deploy/helm/umbrella/cutover-*.sh
# зовёт ровно эту цепочку). Ни одна из двух до сих пор не рендерилась ни одним
# манифест-гейтом.
check_profile "боевой (values.prod)" -f "$CHART/values.prod.yaml" || fail=1
check_profile "боевой + площадка (values.prod + fe3455 + fe3455-prod)" \
  -f "$CHART/values.prod.yaml" -f "$CHART/values.fe3455.yaml" \
  -f "$CHART/values.fe3455-prod.yaml" || fail=1

if [ "$fail" -ne 0 ]; then
  echo
  # ОДИНАРНЫЕ кавычки: в двойных bash ИСПОЛНЯЕТ обратные кавычки, и диагностика
  # сама печатала «-}}: command not found» вместо того, что называет причину.
  echo 'ПРОВАЛ.'
  echo '  Документ без apiVersion/kind. Обычная причина — правый срез'
  echo '  `-}}` у последнего действия преамбулы: он склеивает первый ключ'
  echo '  тела с предыдущей литеральной строкой (как правило с комментарием).'
  echo '  Убери дефис в закрывающих скобках последнего действия перед телом.'
  echo
  echo '  Повторное имя переменной. Обычная причина — одна и та же настройка'
  echo '  объявлена и первоклассной ручкой чарта, и родовым пробросом'
  echo '  (extraEnv/env) в профиле. Убери объявление из проброса: причину этого'
  echo '  класса держит deploy/posture_parity_test.go, здесь видно следствие.'
  exit 1
fi

echo "OK: во всех четырёх профилях документы полны, повторных имён переменных нет"
