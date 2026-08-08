#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# У АННОТАЦИИ ШАБЛОНА ПОДА РОВНО ОДИН ПИСАТЕЛЬ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ
#
# Аннотация шаблона пода — это рычаг переката: сменилось значение → новый
# ReplicaSet → процесс перечитал то, что читает один раз на старте. Поэтому её
# ставят с двух сторон: чарт — когда значение выводится из содержимого релиза,
# и задание внутри релиза — когда значение существует только после обращения к
# уже поднятому компоненту.
#
# Обе стороны сразу — нельзя. Helm 3 сводил манифесты на клиенте и на владение
# полем не смотрел, поэтому двойное владение было незаметно годами. Helm 4
# применяет манифесты на стороне сервера, где владение энфорсится: обновление
# релиза падает конфликтом менеджеров полей — и падает на ЧИСТОМ кластере, где
# заданию есть что записать впервые. Ровно на этом стоял пин helm 3 в трёх
# конвейерах (kacho#3).
#
# ЧТО ЭТА ПРОВЕРКА УТВЕРЖДАЕТ — пересечение двух множеств ПУСТО:
#   • слева  — ключи аннотаций шаблона пода, ОБЪЯВЛЕННЫЕ рендером чартов;
#   • справа — ключи аннотаций шаблона пода, координаты которых встречаются в
#     полезной нагрузке патча внутри скрипта, приезжающего в том же рендере.
#
# ЧИТАЕТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ. Имя аннотации встречается в этом же
# рендере в комментарии RBAC, в комментарии задания и в строке `echo` — то есть
# наивный поиск по имени нашёл бы предмет там, где ничего не патчится, и остался
# бы красным после починки. Справа берутся ТОЛЬКО две формы, в которых координата
# аннотации шаблона пода записывается в ПОЛЕЗНОЙ НАГРУЗКЕ патча — json-pointer
# `/spec/template/metadata/annotations/<ключ>` и стратегическое слияние
# `{"spec":{"template":{"metadata":{"annotations":{…}}}}}`, — а строки,
# начинающиеся с `#`, отбрасываются до поиска.
#
# Гейт НЕ разбирает shell и не устанавливает, в какую команду попала найденная
# координата: он намеренно ошибается в сторону красного. Покрасить может только
# ПЕРЕСЕЧЕНИЕ, а чтобы попасть в него, чарт должен объявить тот же ключ — то есть
# ложный красный требует одновременно и объявления в шаблоне, и координаты патча
# в теле контейнера. Разбирать shell ради этого случая значило бы завести хрупкий
# предикат там, где цена ошибки — лишний вопрос, а не пропущенный дефект.
#
# ПРОФИЛИ — ЧЕТЫРЕ, а не один: объявление может стоять под тумблером, выключенным
# в dev, и тогда однопрофильная проверка его не увидит. Набор тот же, что у
# соседней trusted-forwarder-profiles-test.sh. Известный предел назван честно:
# объявление, спрятанное за тумблером, выключенным во ВСЕХ четырёх, не видно и
# здесь — как не видно оно и рендеру CI.
#
# ЧЕГО ЭТА ПРОВЕРКА НЕ УТВЕРЖДАЕТ (граница названа, чтобы её не приняли шире):
# аннотации САМОГО объекта (`metadata.annotations` Deployment'а) — другое поле,
# переката оно не вызывает и в предмет не входит. Перепись по дереву на момент
# написания: `grep -rn 'kubectl .*annotate' deploy/helm/` даёт три совпадения, из
# них два — вызовы (оба `annotate secret`), третье — проза в комментарии чарта.
#
# Офлайновая проверка (кластер не нужен), как и остальные tests/helm/*.
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"

fatal() { echo "FATAL: $1"; exit 2; }
command -v helm >/dev/null 2>&1 || fatal "helm не найден"
python3 -c 'import yaml' 2>/dev/null || fatal "нужен python3 с PyYAML"

PROFILES="
dev|values.dev.yaml
dev-prod|values.dev.yaml,values.dev-prod.yaml
prod|values.prod.yaml
fe3455|values.prod.yaml,values.fe3455.yaml,values.fe3455-prod.yaml
"

TMPD="$(mktemp -d)"; trap 'rm -rf "$TMPD"' EXIT

echo "=== $SCRIPT: рендер umbrella по профилям ==="
rendered=""
while IFS='|' read -r name files; do
  [ -n "$name" ] || continue
  args=()
  ok=1
  IFS=',' read -ra fl <<<"$files"
  for f in "${fl[@]}"; do
    [ -f "$UMBRELLA/$f" ] || { echo "  пропуск $name: нет $f"; ok=0; break; }
    args+=(-f "$UMBRELLA/$f")
  done
  [ "$ok" = 1 ] || continue
  helm template kacho-umbrella "$UMBRELLA" "${args[@]}" >"$TMPD/$name.yaml" 2>/dev/null \
    || fatal "рендер профиля $name сорвался — проверка НЕ ВЫПОЛНЕНА"
  echo "  отрендерен профиль $name ($files)"
  rendered="$rendered $TMPD/$name.yaml"
done <<<"$PROFILES"

[ -n "$rendered" ] || fatal "ни один профиль не отрендерен — проверка НЕ ВЫПОЛНЕНА"

# shellcheck disable=SC2086
python3 - $rendered <<'PY'
import os
import re
import sys
import yaml

WORKLOAD_KINDS = ("Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob")


def load(path):
    with open(path) as fh:
        return [d for d in yaml.safe_load_all(fh) if isinstance(d, dict) and d.get("kind")]


def pod_spec_holder(doc):
    """spec, внутри которого лежит `template` с шаблоном пода.

    CronJob прячет его на уровень глубже (spec.jobTemplate.spec.template).
    """
    spec = doc.get("spec") or {}
    if doc.get("kind") == "CronJob":
        spec = (spec.get("jobTemplate") or {}).get("spec") or {}
    return spec


def pod_template(doc):
    return pod_spec_holder(doc).get("template") or {}


def declared_annotations(doc):
    ann = (pod_template(doc).get("metadata") or {}).get("annotations") or {}
    return [k for k in ann if isinstance(k, str)]


def script_bodies(doc):
    """Тела контейнеров: command + args. Это и есть исполняемая часть."""
    spec = pod_template(doc).get("spec") or {}
    out = []
    for c in (spec.get("containers") or []) + (spec.get("initContainers") or []):
        for field in ("command", "args"):
            for item in (c.get(field) or []):
                if isinstance(item, str):
                    out.append(item)
    return out


def executable_part(text):
    """Отбрасываем строки-комментарии: гейт обязан читать код, а не прозу."""
    return "\n".join(ln for ln in text.splitlines() if not ln.lstrip().startswith("#"))


def unescape_shell_quotes(text):
    """JSON внутри двойных кавычек shell приезжает с `\\"` — снимаем экранирование."""
    return text.replace('\\"', '"')


def unpointer(seg):
    """json-pointer: `~1` → `/`, `~0` → `~` (порядок расшифровки нормативен)."""
    return seg.replace("~1", "/").replace("~0", "~")


# (a) json-patch: путь до аннотации шаблона пода.
RE_POINTER = re.compile(r'/spec/template/metadata/annotations/([^"\'\s,\]}]+)')
# (b) стратегическое слияние: вложенность spec→template→metadata→annotations.
RE_MERGE_BLOCK = re.compile(
    r'"spec"\s*:\s*\{\s*"template"\s*:\s*\{\s*"metadata"\s*:\s*\{\s*"annotations"\s*:\s*\{(.*?)\}',
    re.S,
)
RE_MERGE_KEY = re.compile(r'"([^"]+)"\s*:')


def patched_annotations(text):
    found = set()
    for seg in RE_POINTER.findall(text):
        found.add(unpointer(seg))
    for block in RE_MERGE_BLOCK.findall(text):
        found.update(RE_MERGE_KEY.findall(block))
    return found


declared = {}   # ключ аннотации → координаты объявления «профиль: Kind/имя»
patched = {}    # ключ аннотации → координаты патча
totals = []

for path in sys.argv[1:]:
    profile = os.path.splitext(os.path.basename(path))[0]
    docs = load(path)
    workloads = [d for d in docs if d.get("kind") in WORKLOAD_KINDS]
    bodies = 0
    d_keys, p_keys = set(), set()
    for d in workloads:
        coord = f'{profile}: {d["kind"]}/{(d.get("metadata") or {}).get("name")}'
        for key in declared_annotations(d):
            declared.setdefault(key, []).append(coord)
            d_keys.add(key)
        for body in script_bodies(d):
            bodies += 1
            text = unescape_shell_quotes(executable_part(body))
            for key in patched_annotations(text):
                patched.setdefault(key, []).append(coord)
                p_keys.add(key)
    totals.append((profile, len(docs), len(workloads), bodies, len(d_keys), len(p_keys)))

# «Ноль находок» обязано быть отличимо от «ноль прочитанного».
print("  профиль            документов workload'ов тел  объявлено патчится")
for t in totals:
    print(f"  {t[0]:<18} {t[1]:>10} {t[2]:>11} {t[3]:>4} {t[4]:>10} {t[5]:>9}")
print(f"  ИТОГО по объединению профилей: объявлено ключей {len(declared)}, "
      f"патчится {len(patched)}" + (f" ({', '.join(sorted(patched))})" if patched else ""))

rc = 0

# ── ПРЕДПОСЫЛКИ ГЕЙТА. Каждая — отдельное утверждение, а не молчаливое допущение.
if not totals:
    print("\nFAIL: не осмотрен ни один профиль.")
    rc = 1
if not any(t[2] for t in totals):
    print("\nFAIL: в рендерах нет ни одного workload'а — осматривать нечего.")
    rc = 1
if not declared:
    print("\nFAIL: чарты не объявили НИ ОДНОЙ аннотации шаблона пода — так не бывает,")
    print("      значит сломан разбор рендера, а не дерево.")
    rc = 1
if not any(t[3] for t in totals):
    print("\nFAIL: не найдено ни одного тела контейнера — правая половина слепа.")
    rc = 1
if not patched:
    print("\nFAIL: изнутри релиза не патчится НИ ОДНА аннотация шаблона пода.")
    print("      Это не «всё хорошо»: у гейта пропал предмет. Либо перекат при смене")
    print("      модели прав перестал работать, либо форма патча сменилась и предикат")
    print("      её больше не узнаёт. Разобрать прежде, чем считать зелёным.")
    rc = 1

# ── СОБСТВЕННО УТВЕРЖДЕНИЕ.
both = sorted(set(declared) & set(patched))
for key in both:
    print()
    print(f"FAIL: у аннотации `{key}` ДВА писателя:")
    print(f"      объявляет чарт → {', '.join(sorted(set(declared[key])))}")
    print(f"      патчит скрипт  → {', '.join(sorted(set(patched[key])))}")
    rc = 1

if both:
    print()
    print("Helm 4 применяет манифесты на стороне сервера, где владение полем")
    print("энфорсится: второй писатель роняет `helm upgrade` конфликтом менеджеров")
    print("полей на этом поле. Починка — снять объявление из шаблона чарта (значение")
    print("приезжает после обращения к поднятому компоненту, то есть чарту его")
    print("взять неоткуда) ЛИБО убрать патч, если значение выводимо из релиза.")
elif rc == 0:
    print("\nпересечение пусто: у каждой аннотации шаблона пода ровно один писатель")

sys.exit(rc)
PY
rc=$?

if [ "${1:-}" != "--self-test" ]; then
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT" || echo "FAILED: $SCRIPT"
  exit $rc
fi

# ─────────────────────────────────────────────────────────────────────────────
# Самопроверка: гейт обязан краснеть на внесённом дефекте И молчать на законных
# конструкциях той же формы — с ОБЕИХ сторон пересечения.
# ─────────────────────────────────────────────────────────────────────────────
echo
echo "=== $SCRIPT: self-test ==="
st=0
[ $rc -eq 0 ] && echo "  ОК  (0) дерево как есть → МОЛЧИТ" \
              || { echo "  ПРОВАЛ (0) дерево как есть уже красное"; st=1; }

IAM_DEP="$UMBRELLA/charts/kacho-iam/templates/deployment.yaml"
GEO_DEP="$UMBRELLA/charts/kacho-geo/templates/deployment.yaml"
JOB="$UMBRELLA/charts/openfga-bootstrap/templates/openfga-bootstrap-job.yaml"

# (A) ИНЪЕКЦИЯ: вернуть в чарт объявление аннотации, которую патчит задание.
if [ ! -f "$IAM_DEP" ]; then
  echo "  ПРОВАЛ (A) не найден чарт для инъекции ($IAM_DEP)"; st=1
else
  bak="$(mktemp)"; cp "$IAM_DEP" "$bak"
  python3 - "$IAM_DEP" <<'PY'
import sys
p = sys.argv[1]
src = open(p).read()
anchor = "        kacho.cloud/config-checksum:"
i = src.index(anchor)
open(p, "w").write(
    src[:i] + '        kacho.cloud/openfga-model-id-rev: "pending"\n' + src[i:]
)
PY
  out="$(bash "$0" 2>&1)"; ist=$?
  cp "$bak" "$IAM_DEP"; rm -f "$bak"
  if [ $ist -ne 0 ] \
     && printf '%s' "$out" | grep -q 'openfga-model-id-rev' \
     && printf '%s' "$out" | grep -q 'kacho-iam'; then
    echo "  ОК  (A) второй писатель → КРАСНЫЙ с ключом и координатой"
  else
    echo "  ПРОВАЛ (A) двойное владение не поймано (exit=$ist)"
    printf '%s\n' "$out" | tail -8 | sed 's/^/      /'; st=1
  fi
fi

# (B) КОНТРОЛЬ СЛЕВА: аннотация, объявленная чартом и никем не патчимая, законна.
#     Без этого случая гейт краснел бы на КАЖДОЙ аннотации шаблона пода — то есть
#     ловил бы форму, а не пересечение, и первый же ложный срабат его бы отключил.
if [ ! -f "$GEO_DEP" ]; then
  echo "  ПРОВАЛ (B) не найден чарт для контроля ($GEO_DEP)"; st=1
else
  bak="$(mktemp)"; cp "$GEO_DEP" "$bak"
  python3 - "$GEO_DEP" <<'PY'
import sys
p = sys.argv[1]
src = open(p).read()
anchor = "      annotations:\n"
i = src.index(anchor) + len(anchor)
open(p, "w").write(src[:i] + '        kacho.cloud/self-test-twin: "x"\n' + src[i:])
PY
  out="$(bash "$0" 2>&1)"; bst=$?
  cp "$bak" "$GEO_DEP"; rm -f "$bak"
  if [ $bst -eq 0 ]; then
    echo "  ОК  (B) объявленная и никем не патчимая аннотация → МОЛЧИТ"
  else
    echo "  ПРОВАЛ (B) законная аннотация покрашена (exit=$bst)"
    printf '%s\n' "$out" | tail -8 | sed 's/^/      /'; st=1
  fi
fi

# (C) КОНТРОЛЬ СПРАВА: координата аннотации, которой не объявляет ни один чарт,
#     законна. Он доказывает сразу две вещи: правая половина эту форму ВИДИТ
#     (ключ назван в объёме осмотренного) и гейт всё равно молчит, потому что
#     ключуется на ПЕРЕСЕЧЕНИИ, а не на факте «координата патча встретилась».
if [ ! -f "$JOB" ]; then
  echo "  ПРОВАЛ (C) не найдено задание для контроля ($JOB)"; st=1
else
  bak="$(mktemp)"; cp "$JOB" "$bak"
  python3 - "$JOB" <<'PY'
import sys
p = sys.argv[1]
src = open(p).read()
anchor = "              REV=$(date +%s)\n"
i = src.index(anchor) + len(anchor)
extra = (
    '              echo "self-test control" \\\n'
    '                "/spec/template/metadata/annotations/kacho.cloud~1self-test-unowned"\n'
)
open(p, "w").write(src[:i] + extra + src[i:])
PY
  out="$(bash "$0" 2>&1)"; cst=$?
  cp "$bak" "$JOB"; rm -f "$bak"
  if [ $cst -eq 0 ] && printf '%s' "$out" | grep -q 'self-test-unowned'; then
    echo "  ОК  (C) координата патча без объявления в чарте → УВИДЕНА и НЕ покрашена"
  else
    echo "  ПРОВАЛ (C) контроль справа не прошёл (exit=$cst)"
    printf '%s\n' "$out" | tail -8 | sed 's/^/      /'; st=1
  fi
fi

echo
[ $st -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
exit $st
