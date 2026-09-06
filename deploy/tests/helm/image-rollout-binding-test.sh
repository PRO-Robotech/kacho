#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# КАЖДЫЙ workload, работающий на локально собираемом образе, ОБЯЗАН перекатываться
# при смене СОДЕРЖИМОГО этого образа.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ
#
# Локальные образы носят один неизменный тег (`:dev`; имена — deploy/images.txt).
# Пересборка под
# тем же тегом не меняет шаблон пода — значит Kubernetes не видит причины
# перекатывать под, и процесс продолжает работать СТАРЫМ бинарником, пока тег
# утверждает обратное.
#
# На стенде это скрывалось побочным эффектом: подъём двухфазный и меняет настройки
# дважды, поэтому поды перекатывались из-за СМЕНЫ НАСТРОЕК, а не из-за образа.
# Одиночное обновление релиза при неизменных настройках их бы не тронуло — и
# получилось бы ровно то же расхождение «намерение против исхода», что и в
# инциденте с настройками, читаемыми один раз на старте (2026-07-25): хранимое
# обновилось, исполняемое — нет, а «под Ready» этого не опровергает.
#
# ЧЕМ ЭТА ПРОВЕРКА ОТЛИЧАЕТСЯ ОТ «поискать аннотацию по имени» — она
# ПОВЕДЕНЧЕСКАЯ: рендерим стенд ДВАЖДЫ с РАЗНЫМИ идентификаторами содержимого
# образов и требуем, чтобы у каждого такого workload'а шаблон пода РАЗЛИЧАЛСЯ.
# Аннотация, привязанная не к тому значению или отставшая при добавлении сервиса,
# такую проверку не проходит.
#
# ПРЕДМЕТ БЕРЁТСЯ ИЗ ИСТОЧНИКА ИСТИНЫ. Список локально собираемых сервисов
# читается из `SERVICES` в deploy/Makefile — там же, где их собирают. Свой список
# устарел бы молча, и новый сервис остался бы без привязки, а проверка — зелёной.
#
# Офлайновая проверка (кластер не нужен), как и остальные tests/helm/*.
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
MAKEFILE="$DEPLOY_ROOT/Makefile"
DEV="$UMBRELLA/values.dev.yaml"

# ── Три исхода — ОБЩЕЙ реализацией каталога, а не своей копией ───────────────
#
# 0 зелено · 1 находка о дереве · 2 условие не создано (плюс текст самого helm).
# Свой `fatal` категорию РАЗЛИЧАЛ верно и потому был почти прав; неверна была
# ВТОРАЯ половина контракта — текст. `render()` глушила stderr (`2>/dev/null`),
# поэтому оба «рендер сорвался» уезжали читателю голыми: на дереве без собранных
# зависимостей умбреллы «гейт нашёл дефект», «гейт сам сломан» и «условие не
# создано» давали один наблюдаемый результат. Теперь причину называет сам helm.
#
# Утверждений ровно три и число их от состава стендов не зависит, поэтому
# вердикт печатает `outcome_verdict` с объявленным ожиданием.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=3

require_helm
require_python_yaml
require_file_present "$DEV" "values.dev.yaml"
require_file_present "$MAKEFILE" "deploy/Makefile — предмет проверки берётся из него"

# Локально собираемые сервисы — из Makefile, а не из копии списка здесь.
SERVICES="$(sed -n 's/^SERVICES *:= *//p' "$MAKEFILE" | head -1)"
[ -n "$SERVICES" ] || fatal "в deploy/Makefile не найден SERVICES — предпосылка проверки не выполняется"
SERVICES_N="$(wc -w <<<"$SERVICES" | tr -d '[:space:]')"

# Две карты идентификаторов, отличающиеся ВСЕМИ значениями.
mk_ids() { # $1 — суффикс
  echo "global:"
  echo "  kachoImageIds:"
  for s in $SERVICES; do echo "    $s: \"sha256:$1$s\""; done
}

TMPD="$(mktemp -d)"; trap 'rm -rf "$TMPD"' EXIT
mk_ids aaaa >"$TMPD/ids-a.yaml"
mk_ids bbbb >"$TMPD/ids-b.yaml"

# render <файл карты> <куда> <что рендерим> — рендер умбреллы.
#
# Зовётся НЕ из подстановки: `helm_try` и `render_nonempty_or_fatal` пишут в
# глобальные переменные, и из подоболочки ни код возврата helm, ни текст его
# отказа наружу не отдались бы. Успешный, но ПУСТОЙ рендер тоже условие, а не
# «ничего не нашли»: по нему разбор насчитал бы ноль workload'ов и объявил бы
# находкой отсутствие предмета.
render() {
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" -f "$DEV" -f "$1" ${EXTRA_SET:-}
  render_nonempty_or_fatal "$3"
  printf '%s\n' "$HELM_OUT" >"$2"
}

echo "=== $SCRIPT: рендер с двумя разными идентификаторами содержимого образов ==="
render "$TMPD/ids-a.yaml" "$TMPD/a.yaml" "values.dev.yaml + карта идентификаторов A"; ok
render "$TMPD/ids-b.yaml" "$TMPD/b.yaml" "values.dev.yaml + карта идентификаторов B"; ok

SERVICES="$SERVICES" python3 - "$TMPD/a.yaml" "$TMPD/b.yaml" <<'PY'
import os, sys, yaml

services = os.environ["SERVICES"].split()


def load(path):
    with open(path) as fh:
        return [d for d in yaml.safe_load_all(fh) if isinstance(d, dict) and d.get("kind")]


def index(docs):
    out = {}
    for d in docs:
        if d.get("kind") in ("Deployment", "StatefulSet", "DaemonSet"):
            out[(d["kind"], (d.get("metadata") or {}).get("name"))] = d
    return out


def images(w):
    spec = ((w.get("spec") or {}).get("template") or {}).get("spec") or {}
    return [c.get("image", "") for c in
            (spec.get("containers") or []) + (spec.get("initContainers") or [])]


def pod_template(w):
    return (w.get("spec") or {}).get("template") or {}


def local_image_of(w):
    """Первый образ workload'а, который СОБИРАЕТСЯ локально (kacho-<svc>:…).

    Именно собираемые локально несут неизменный тег; образы из реестра
    адресуются своим тегом/дайджестом и к этой проверке отношения не имеют —
    требовать привязки с них значило бы красить за чужой порядок поставки.
    """
    for img in images(w):
        base = img.split("/")[-1]
        for s in services:
            if base.startswith(f"kacho-{s}:") or base.startswith(f"kacho-{s}@"):
                return img
    return None


a, b = index(load(sys.argv[1])), index(load(sys.argv[2]))
subjects, failures = [], []

for key in sorted(a.keys() & b.keys()):
    kind, name = key
    img = local_image_of(a[key])
    if img is None:
        continue
    subjects.append(f"{kind}/{name}")
    if pod_template(a[key]) == pod_template(b[key]):
        failures.append(
            f"{kind}/{name} (образ {img}): содержимое образа различается между двумя "
            f"рендерами, а шаблон пода ПОБАЙТОВО ТОТ ЖЕ → под не перекатится и процесс "
            f"останется на старом бинарнике под тем же тегом."
        )
    else:
        print(f"  OK {kind}/{name}: смена содержимого образа меняет шаблон пода")

# «Ноль находок» обязано быть отличимо от «ноль осмотренного».
print(f"\nworkload'ов на локально собираемых образах: {len(subjects)} "
      f"(сервисов в SERVICES: {len(services)})")

if not subjects:
    print("\nFAIL: ни один workload не попал под проверку — предмета нет.")
    print("      На стенде, который собирает образы локально, это означает сломанное")
    print("      сопоставление, а не отсутствие таких workload'ов.")
    sys.exit(1)

# Каждый локально собираемый сервис обязан иметь СВОЙ workload среди осмотренных:
# иначе сервис, выпавший из рендера профиля, тихо остался бы без привязки.
covered = set()
for key in sorted(a.keys() & b.keys()):
    img = local_image_of(a[key])
    if not img:
        continue
    base = img.split("/")[-1]
    for s in services:
        if base.startswith(f"kacho-{s}:") or base.startswith(f"kacho-{s}@"):
            covered.add(s)
missing = sorted(set(services) - covered)
if missing:
    print(f"\nFAIL: сервисы собираются локально, но их workload'ов нет в рендере "
          f"профиля: {', '.join(missing)}")
    print("      Такой сервис не проверяется вовсе — и привязку у него потерять некому заметить.")
    sys.exit(1)

if failures:
    print()
    for f in failures:
        print(f"FAIL: {f}")
    print()
    print("Починка: добавить в spec.template.metadata.annotations привязку к")
    print("идентификатору содержимого образа, например")
    print('  kacho.cloud/image-id: {{ dig "kachoImageIds" "<svc>" "unset" (.Values.global | default dict) | quote }}')
    print("Значение пишет `make build-services` в helm/umbrella/values.image-ids.yaml.")
    sys.exit(1)

print(f"\n{len(subjects)} workload'ов привязаны к содержимому своих образов")
PY
rc=$?

if [ "${1:-}" != "--self-test" ]; then
  # Третье утверждение — сам разбор двух рендеров. Его находки уже перечислены
  # разборщиком выше; здесь они получают КОД: 1 — находка о дереве. Прежде тот же
  # код 1 приезжал и от сорванного рендера, то есть от условия прогона.
  [ "$rc" -eq 0 ] || fail "$SCRIPT — привязка workload'ов к содержимому их образов нарушена (перечень выше)"
  ok
  outcome_verdict "сервисов в SERVICES: $SERVICES_N"
  # `outcome_verdict` печатает PASS и ВОЗВРАЩАЕТ 0 (выходит он только на находке),
  # тогда как прежний вердикт здесь ВЫХОДИЛ. Без этой строки обычный прогон
  # проваливался бы в ветку самопроверки ниже — и отчитывался бы её вердиктом.
  exit 0
fi

# ─────────────────────────────────────────────────────────────────────────────
# Самопроверка: гейт обязан краснеть на внесённом дефекте И молчать на законной
# конструкции той же формы.
# ─────────────────────────────────────────────────────────────────────────────
echo
echo "=== $SCRIPT: self-test ==="
st=0
[ $rc -eq 0 ] && echo "  ОК  (0) дерево как есть → МОЛЧИТ" \
              || { echo "  ПРОВАЛ (0) дерево как есть уже красное"; st=1; }

# ─────────────────────────────────────────────────────────────────────────────
# ИНЪЕКЦИЯ ИДЁТ В КОПИЮ ДЕРЕВА, А НЕ В ЖИВУЮ РАБОЧУЮ КОПИЮ (#696).
#
# Прежняя редакция снимала резервную копию файла во временный каталог, правила
# файл В ДЕРЕВЕ и возвращала его следующей строкой. Снятие прогона между этими
# двумя строками до возврата не доходит — и уносит резервную копию, из которой
# файл можно было бы вернуть. Дерево остаётся с внесённым дефектом, а гейты
# этого репозитория берут состав корпуса у индекса и рабочей копии: следующий
# читатель получает вердикт о дереве, которого никто не писал.
#
# Гейт находит свой корень по расположению СОБСТВЕННОГО файла, поэтому прогон
# копии гейта из $WORK судит $WORK. Отдельной ручки «какое дерево судить» не
# заводится — уводить гейт с настоящего дерева нечем.
# ─────────────────────────────────────────────────────────────────────────────
WORK="$(mktemp -d)"
trap 'rm -rf "$TMPD" "$WORK"' EXIT
cp -r "$DEPLOY_ROOT/." "$WORK/" || fatal "копия дерева развёртывания не собрана — инъекции некуда идти"
[ -x "$WORK/tests/helm/$SCRIPT" ] || fatal "в копии нет самого гейта ($WORK/tests/helm/$SCRIPT)"

# (A) ИНЪЕКЦИЯ: снять привязку у одного чарта → гейт обязан покраснеть с координатой.
VICTIM_REL="helm/umbrella/charts/kacho-geo/templates/deployment.yaml"
if [ ! -f "$DEPLOY_ROOT/$VICTIM_REL" ]; then
  echo "  ПРОВАЛ (A) не найден чарт для инъекции ($VICTIM_REL)"; st=1
else
  # Правится копия; возврат берётся из живого дерева, которое проба только читает.
  grep -v 'kacho.cloud/image-id' "$DEPLOY_ROOT/$VICTIM_REL" >"$WORK/$VICTIM_REL"
  out="$(bash "$WORK/tests/helm/$SCRIPT" 2>&1)"; ist=$?
  cp "$DEPLOY_ROOT/$VICTIM_REL" "$WORK/$VICTIM_REL"
  if [ $ist -ne 0 ] && [[ "${out,,}" == *geo* ]]; then
    echo "  ОК  (A) снятая привязка → КРАСНЫЙ с координатой"
  else
    echo "  ПРОВАЛ (A) без привязки гейт остался зелёным (exit=$ist)"
    printf '%s\n' "$out" | tail -6 | sed 's/^/      /'; st=1
  fi
fi

# (B) КОНТРОЛЬ: образ ИЗ РЕЕСТРА (не собирается локально) привязки не требует.
#     Без этого случая гейт ловил бы форму, а не существо, и красил бы каждый
#     сторонний образ стека — первый же ложный срабат его бы и отключил.
foreign="$(EXTRA_SET='--set kacho-geo.image=docker.io/prorobotech/kacho-geo:main-abc' bash "$WORK/tests/helm/$SCRIPT" 2>&1)"; fst=$?
if [ $fst -eq 0 ]; then
  echo "  ОК  (B) образ из реестра привязки не требует → МОЛЧИТ"
else
  echo "  ПРОВАЛ (B) сторонний образ покрашен"; printf '%s\n' "$foreign" | tail -5 | sed 's/^/      /'; st=1
fi

echo
[ $st -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
exit $st
