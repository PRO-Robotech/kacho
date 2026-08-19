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
# Локальные образы носят один неизменный тег (`kacho-<svc>:dev`). Пересборка под
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

fatal() { echo "FATAL: $1"; exit 2; }
command -v helm >/dev/null 2>&1 || fatal "helm не найден"
python3 -c 'import yaml' 2>/dev/null || fatal "нужен python3 с PyYAML"
[ -f "$DEV" ] || fatal "values.dev.yaml не найден ($DEV)"
[ -f "$MAKEFILE" ] || fatal "deploy/Makefile не найден — предмет проверки берётся из него"

# Локально собираемые сервисы — из Makefile, а не из копии списка здесь.
SERVICES="$(sed -n 's/^SERVICES *:= *//p' "$MAKEFILE" | head -1)"
[ -n "$SERVICES" ] || fatal "в deploy/Makefile не найден SERVICES — предпосылка проверки не выполняется"

# Две карты идентификаторов, отличающиеся ВСЕМИ значениями.
mk_ids() { # $1 — суффикс
  echo "global:"
  echo "  kachoImageIds:"
  for s in $SERVICES; do echo "    $s: \"sha256:$1$s\""; done
}

TMPD="$(mktemp -d)"; trap 'rm -rf "$TMPD"' EXIT
mk_ids aaaa >"$TMPD/ids-a.yaml"
mk_ids bbbb >"$TMPD/ids-b.yaml"

render() { # $1 — файл карты, $2 — куда
  helm template kacho-umbrella "$UMBRELLA" -f "$DEV" -f "$1" ${EXTRA_SET:-} >"$2" 2>/dev/null
}

echo "=== $SCRIPT: рендер с двумя разными идентификаторами содержимого образов ==="
render "$TMPD/ids-a.yaml" "$TMPD/a.yaml" || fatal "рендер (A) сорвался — проверка НЕ ВЫПОЛНЕНА"
render "$TMPD/ids-b.yaml" "$TMPD/b.yaml" || fatal "рендер (B) сорвался — проверка НЕ ВЫПОЛНЕНА"

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
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT" || echo "FAILED: $SCRIPT"
  exit $rc
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

# (A) ИНЪЕКЦИЯ: снять привязку у одного чарта → гейт обязан покраснеть с координатой.
VICTIM="$DEPLOY_ROOT/helm/umbrella/charts/kacho-geo/templates/deployment.yaml"
if [ ! -f "$VICTIM" ]; then
  echo "  ПРОВАЛ (A) не найден чарт для инъекции ($VICTIM)"; st=1
else
  bak="$(mktemp)"; cp "$VICTIM" "$bak"
  grep -v 'kacho.cloud/image-id' "$bak" >"$VICTIM"
  out="$(bash "$0" 2>&1)"; ist=$?
  cp "$bak" "$VICTIM"; rm -f "$bak"
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
foreign="$(EXTRA_SET='--set kacho-geo.image=docker.io/prorobotech/kacho-geo:main-abc' bash "$0" 2>&1)"; fst=$?
if [ $fst -eq 0 ]; then
  echo "  ОК  (B) образ из реестра привязки не требует → МОЛЧИТ"
else
  echo "  ПРОВАЛ (B) сторонний образ покрашен"; printf '%s\n' "$foreign" | tail -5 | sed 's/^/      /'; st=1
fi

echo
[ $st -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
exit $st
