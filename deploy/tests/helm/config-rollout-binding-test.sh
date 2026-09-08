#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Каждый workload, который читает настройки из ConfigMap, ОБЯЗАН перекатываться
# при изменении их СОДЕРЖИМОГО.
#
# ПОЧЕМУ (инцидент 2026-07-25, kacho-storage). Настройки, приезжающие через
# `envFrom: configMapRef` или примонтированный конфиг-файл, читаются процессом
# ОДИН РАЗ — на старте. Правка ConfigMap не меняет pod-template, поэтому
# Kubernetes НЕ перекатывает под, и процесс продолжает жить со СТАРЫМ окружением.
# Перевод стенда в production-посадку переписал настройки storage
# (authMode dev→production, sslMode disable→require) и НЕ ТРОНУЛ процесс: он
# остался в dev с плейнтекстовой БД. Boot-guard (`Config.Validate` →
# refuse-to-start) не сработал — он не может отказать в старте, которого не было.
# Лечится одной аннотацией pod-template, привязанной к содержимому ConfigMap
# (`checksum/config`), — тогда изменение настроек меняет pod-template и под
# перекатывается, а boot-guard снова получает право голоса.
#
# ЧЕМ ЭТА ПРОВЕРКА ОТЛИЧАЕТСЯ ОТ «поискать аннотацию по имени». Наличие
# аннотации НИЧЕГО не доказывает: она может хэшировать не тот шаблон, покрывать
# лишь один из нескольких потребляемых ConfigMap'ов или отстать при добавлении
# нового. Поэтому проверка ПОВЕДЕНЧЕСКАЯ: рендерим стенд ДВАЖДЫ — в dev-профиле
# и в production-профиле — и требуем, чтобы у каждого workload'а, чей ConfigMap
# при этом ИЗМЕНИЛСЯ, ИЗМЕНИЛИСЬ и аннотации pod-template. Если содержимое
# настроек разъехалось, а pod-template побайтово тот же — под НЕ перекатится,
# и это ровно тот дефект, который утёк в прод-посадку.
#
# Офлайновая manifest-проверка (kind-кластер не нужен). Как и остальные tests/helm/*.
set -euo pipefail

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
DEV="$UMBRELLA/values.dev.yaml"
DEVPROD="$UMBRELLA/values.dev-prod.yaml"

# Общий контракт исходов каталога. Здесь стояла СВОЯ копия `fail`, и она
# отдавала код 1 — «находка о дереве» — на КАЖДОЙ предпосылке: нет PyYAML, нет
# профиля на диске, не материализованы зависимости, сорвался рендер. То есть
# третья категория («условие не создано») подавалась как вердикт о дереве —
# ровно дефект #1214. Скрипт был невидим гейту `three-outcomes-distinguishable`,
# потому что тот исключает из ПРОГОНА материализующих зависимости сам, а
# исключение из прогона работало и как освобождение от контракта.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=1

require_helm
# PyYAML, а НЕ yq: на машинах разработчиков /usr/bin/yq регулярно оказывается
# python-обёрткой над jq вместо mikefarah yq v4. Её синтаксис несовместим,
# вызов молча отдаёт пустой вывод — и проверка, сравнивающая с пустотой,
# «зеленеет», ничего не проверив. Именно этот класс ложного зелёного тут и ловим,
# так что сами на него наступать не будем.
require_python_yaml

# ═════════════════════════════════════════════════════════════════════════════
# АНАЛИЗ — ОДИН ЭКЗЕМПЛЯР ИСХОДНИКА для боевого прохода И для самопроверки.
#
# Пока он был вписан heredoc'ом в середину прохода, доказать инъекцией его было
# нечем: самопроверке пришлось бы держать ВТОРУЮ копию логики, а две копии
# расходятся. Теперь обе стороны зовут один и тот же текст.
# ═════════════════════════════════════════════════════════════════════════════
PY_ROLLOUT_BINDING=$(cat <<'PYSRC'
import re
import sys, yaml

def load(path):
    with open(path) as fh:
        return [d for d in yaml.safe_load_all(fh) if isinstance(d, dict) and d.get("kind")]

def index(docs):
    workloads, configmaps = {}, {}
    for d in docs:
        kind = d.get("kind")
        name = (d.get("metadata") or {}).get("name")
        if not name:
            continue
        if kind in ("Deployment", "StatefulSet", "DaemonSet"):
            workloads[(kind, name)] = d
        elif kind == "ConfigMap":
            configmaps[name] = d.get("data") or {}
    return workloads, configmaps

def consumed_configmaps(workload):
    """ConfigMap'ы, из которых под читает настройки — по ВСЕМ путям доставки."""
    spec = ((workload.get("spec") or {}).get("template") or {}).get("spec") or {}
    names = set()
    for c in (spec.get("containers") or []) + (spec.get("initContainers") or []):
        for ef in c.get("envFrom") or []:
            ref = (ef.get("configMapRef") or {}).get("name")
            if ref:
                names.add(ref)
        for e in c.get("env") or []:
            ref = ((e.get("valueFrom") or {}).get("configMapKeyRef") or {}).get("name")
            if ref:
                names.add(ref)
    for v in spec.get("volumes") or []:
        ref = (v.get("configMap") or {}).get("name")
        if ref:
            names.add(ref)
        for src in (v.get("projected") or {}).get("sources") or []:
            ref = (src.get("configMap") or {}).get("name")
            if ref:
                names.add(ref)
    return names

def pod_annotations(workload):
    meta = ((workload.get("spec") or {}).get("template") or {}).get("metadata") or {}
    return meta.get("annotations") or {}

HEX64 = re.compile(r"^[0-9a-f]{64}$")

def content_bindings(workload):
    """Привязки шаблона пода к СОДЕРЖИМОМУ настроек — по всем законным местам.

    (1) Аннотация, чей ключ содержит "checksum", — КРОМЕ хэширующих СЕКРЕТ.
        Аннотация секрета меняется по своей причине и покрытием настроек не
        является; арифметика «привязок не меньше, чем ConfigMap'ов» её
        засчитывала, и у пода провайдера личности счёт сходился ИМЕННО за её
        счёт, пока конфигурация соседа-терминатора не была привязана ничем.
        Заголовок этой проверки об этом и предупреждает: она не доказывает, что
        привязка относится к своему объекту. Правило подсчёта — допускало.

    (2) Переменная окружения контейнера, чьё ЗНАЧЕНИЕ — 64 шестнадцатеричные
        цифры, то есть отпечаток содержимого. Это НЕ поблажка: бывают чарты, не
        пропускающие свои значения через шаблонизацию, и тогда аннотацию
        вычислить негде — единственное доступное место — спецификация самого
        контейнера. Признак строгий: имя подделать легко, 64-hex значение
        появляется только из вычисления.
    """
    out = []
    for k in pod_annotations(workload):
        if "checksum" not in k.lower():
            continue
        if "secret" in k.lower():
            continue
        out.append(k)
    spec = ((workload.get("spec") or {}).get("template") or {}).get("spec") or {}
    for c in (spec.get("containers") or []) + (spec.get("initContainers") or []):
        for e in c.get("env") or []:
            v = e.get("value")
            if isinstance(v, str) and HEX64.match(v):
                out.append(f"{c.get('name')}.env.{e.get('name')}")
    return out

dev_w, dev_cm = index(load(sys.argv[1]))
prod_w, prod_cm = index(load(sys.argv[2]))

# kube-root-ca.crt и подобные ConfigMap'ы кластера в рендере не появляются,
# поэтому отдельный allow-list не нужен: сравниваем только то, что чарт создаёт.
failures, checked, skipped = [], 0, []

for key in sorted(dev_w.keys() & prod_w.keys()):
    kind, name = key
    cms = consumed_configmaps(dev_w[key]) | consumed_configmaps(prod_w[key])
    cms = {c for c in cms if c in dev_cm and c in prod_cm}
    if not cms:
        continue
    drifted = sorted(c for c in cms if dev_cm[c] != prod_cm[c])
    if not drifted:
        skipped.append(f"{kind}/{name} (настройки одинаковы в обоих профилях — доказывать нечего)")
        continue
    checked += 1
    if pod_annotations(dev_w[key]) == pod_annotations(prod_w[key]):
        failures.append(
            f"{kind}/{name}: содержимое {', '.join(drifted)} различается между dev- и "
            f"production-профилем, но аннотации pod-template ИДЕНТИЧНЫ → под НЕ ПЕРЕКАТИТСЯ "
            f"и процесс останется со старыми настройками (boot-guard не отработает)."
        )
    else:
        print(f"  OK {kind}/{name}: {', '.join(drifted)} меняется → pod-template тоже меняется")

for s in skipped:
    print(f"  -- {s}")

if failures:
    print()
    for f in failures:
        print(f"FAIL: {f}")
    print()
    print("Починка: добавить в spec.template.metadata.annotations аннотацию,")
    print("привязанную к СОДЕРЖИМОМУ каждого потребляемого ConfigMap, например")
    print('  checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}')
    sys.exit(1)

if checked == 0:
    print()
    print("FAIL: ни один workload не попал под проверку — сравнивать было нечего.")
    print("      Пустой результат НЕ означает «всё хорошо»: скорее всего профили")
    print("      перестали различаться настройками, и проверка потеряла смысл.")
    sys.exit(1)

print(f"\n[1/2] OK — у всех {checked} workload'ов изменение настроек перекатывает под")

# ── Пасс 2: структурное покрытие (в т.ч. конфигурации, выключенные сегодня) ──
#
# Пасс 1 поведенческий и потому сильный, но видит ТОЛЬКО те ConfigMap'ы, чьё
# содержимое РАЗЪЕХАЛОСЬ между двумя профилями. ConfigMap, одинаковый в обоих
# (политика OPA, JWKS, bundle-конфиг), он пропускает — а привязка нужна и им:
# правка политики обязана перекатывать под ровно так же, как правка режима.
#
# Здесь проверяется ПОКРЫТИЕ: у workload'а должно быть не меньше привязок к
# содержимому, чем он потребляет ConfigMap'ов. Проверка осознанно СТРУКТУРНАЯ —
# она не доказывает, что привязка относится именно к «своему» ConfigMap'у.
opa_w, opa_cm = index(load(sys.argv[3]))
gaps, checked_w, checked_cm = [], 0, 0
for key in sorted(opa_w.keys()):
    kind, name = key
    cms = sorted(c for c in consumed_configmaps(opa_w[key]) if c in opa_cm)
    if not cms:
        continue
    checked_w += 1
    checked_cm += len(cms)
    binding = content_bindings(opa_w[key])
    if len(binding) < len(cms):
        gaps.append(
            f"{kind}/{name}: потребляет {len(cms)} ConfigMap'ов ({', '.join(cms)}), "
            f"а привязок к содержимому всего {len(binding)} "
            f"({', '.join(sorted(binding)) or 'ни одной'})"
        )

if gaps:
    print()
    for g in gaps:
        print(f"FAIL: {g}")
    print()
    print("Каждый потребляемый ConfigMap обязан иметь СВОЮ привязку к содержимому —")
    print("иначе его правка не перекатит под и процесс останется со старым содержимым.")
    print("Аннотация, хэширующая СЕКРЕТ, покрытием настроек НЕ является.")
    print("Если аннотацию добавить нечем (чужой чарт не пропускает свои значения")
    print("через шаблонизацию), отпечаток кладётся в спецификацию контейнера —")
    print("см. templates/_hydra-admin-tls.tpl.")
    sys.exit(1)

# «Ноль находок» обязано быть отличимо от «ноль прочитанного».
if checked_w == 0:
    print()
    print("FAIL: пасс 2 не осмотрел НИ ОДНОГО workload'а с потребляемым ConfigMap.")
    sys.exit(1)
print(f"  осмотрено workload'ов: {checked_w}, потребляемых ConfigMap'ов: {checked_cm}")
print("[2/2] OK — включая конфигурации, выключенные в dev (OPA-сайдкары)")
print("\nconfig-rollout-binding: OK")
PYSRC
)


# ═════════════════════════════════════════════════════════════════════════════
# САМОПРОВЕРКА — БЕЗ helm И БЕЗ КЛАСТЕРА.
#
# Инъекция в обе стороны на СИНТЕТИЧЕСКИХ рендерах: покрытие обязано краснеть
# там, где привязки нет, и молчать там, где она есть — включая законную форму,
# в которой привязка живёт НЕ в аннотации.
# ═════════════════════════════════════════════════════════════════════════════
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: покрытие настроек, инъекции в обе стороны ==="
  rc=0; checked=0
  st="$(mktemp -d)"; trap 'rm -rf "$st"' EXIT

  mk_triple() { # <каталог> <опции для python-генератора…>
    python3 - "$1" "${@:2}" <<'PYGEN'
import sys, os, yaml
d = sys.argv[1]
opt = dict(kv.split("=", 1) for kv in sys.argv[2:])
os.makedirs(d, exist_ok=True)

def wl(name, cms, annotations, env=None):
    return {"apiVersion": "apps/v1", "kind": "Deployment",
            "metadata": {"name": name},
            "spec": {"template": {
                "metadata": {"annotations": annotations},
                "spec": {"containers": [{"name": "main", "env": env or []}],
                         "volumes": [{"name": c, "configMap": {"name": c}} for c in cms]}}}}

def cm(name, val):
    return {"apiVersion": "v1", "kind": "ConfigMap",
            "metadata": {"name": name}, "data": {"k": val}}

# Пасс 1 обязан иметь предмет в ЛЮБОМ случае, иначе он свалится раньше пасса 2
# и мы будем проверять не то. Поэтому в оба профиля кладётся заведомо здоровый
# workload с расходящимся содержимым и расходящейся аннотацией.
base = lambda v: [wl("drifting", ["cm-drift"], {"checksum/config": "sha-" + v}), cm("cm-drift", v)]
yaml.safe_dump_all(base("dev"), open(os.path.join(d, "dev.yaml"), "w"))
yaml.safe_dump_all(base("prod"), open(os.path.join(d, "prod.yaml"), "w"))

ann = {"checksum/config": "a" * 64}
env = []
cms = ["cm-1"]
mode = opt.get("mode", "ok")
if mode == "secret-covers":                 # 2 объекта настроек, второй «покрыт» секретом
    cms = ["cm-1", "cm-2"]; ann["checksum/hydra-secrets"] = "b" * 64
elif mode == "secret-plus-digest":          # то же + настоящий отпечаток в контейнере
    cms = ["cm-1", "cm-2"]; ann["checksum/hydra-secrets"] = "b" * 64
    env = [{"name": "X_CONF_SHA256", "value": "c" * 64}]
elif mode == "no-binding":
    ann = {}
elif mode == "name-not-digest":             # значение похоже на имя, а не на отпечаток
    cms = ["cm-1", "cm-2"]
    env = [{"name": "X_CONF_SHA256", "value": "cm-2"}]

docs = base("prod") + [wl("subject", cms, ann, env)] + [cm(c, "x") for c in cms]
yaml.safe_dump_all(docs, open(os.path.join(d, "opa.yaml"), "w"))
PYGEN
  }

  probe() { # <метка> <ожидание: red|green> <mode>
    local label="$1" want="$2" mode="$3" out code
    checked=$((checked + 1))
    rm -rf "$st/c"; mk_triple "$st/c" "mode=$mode"
    # `set -e` в шапке: присваивание из подстановки с ненулевым кодом оборвало бы
    # прогон на ПЕРВОЙ же инъекции — и самопроверка выглядела бы «прошедшей до
    # инъекций». Условный контекст это отключает.
    if out="$(python3 -c "$PY_ROLLOUT_BINDING" "$st/c/dev.yaml" "$st/c/prod.yaml" "$st/c/opa.yaml" 2>&1)"; then
      code=0
    else
      code=$?
    fi
    if [ "$want" = red ]; then
      if [ $code -ne 0 ]; then echo "  ✓ $label — краснеет"
      else echo "  ✗ $label — НЕ покраснел:"; sed 's/^/      /' <<<"$out"; rc=1; fi
    else
      if [ $code -eq 0 ]; then echo "  ✓ $label — молчит (законная конструкция)"
      else echo "  ✗ $label — покраснел на ЗАКОННОЙ конструкции:"; sed 's/^/      /' <<<"$out"; rc=1; fi
    fi
  }

  echo "-- законные формы привязки --"
  probe "аннотация покрывает единственный ConfigMap" green ok
  probe "отпечаток в спецификации контейнера покрывает второй ConfigMap" green secret-plus-digest

  echo
  echo "-- инъекции --"
  probe "привязки нет вовсе" red no-binding
  probe "контрольная сумма СЕКРЕТА закрывает покрытие настроек" red secret-covers
  probe "значение переменной — имя, а не отпечаток" red name-not-digest

  echo
  echo "инъекций и законных входов проверено: $checked"
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $rc
fi

require_file_present "$DEV"     "values.dev.yaml"
require_file_present "$DEVPROD" "values.dev-prod.yaml"

TMPD="$(mktemp -d)"
trap 'rm -rf "$TMPD"' EXIT

# ОБЯЗАТЕЛЬНО перед рендером: сервисные сабчарты вендорятся в charts/*.tgz, и
# `helm template` берёт ИМЕННО их, а не исходники в services/*/deploy. Без
# обновления зависимостей проверка молча аттестовала бы СТАРУЮ копию чарта:
# правку (или её потерю) в исходнике она бы не увидела вовсе. Проверено — без
# этого шага тест оставался зелёным на заведомо сломанном storage-чарте.
# Сбой обновления — КРАСНОЕ: непроверенный исходник это не «всё хорошо».
# Материализация — у ЕДИНСТВЕННОГО владельца (scripts/helm-umbrella-deps.sh):
# повторный вызов внутри одного прогона в сеть не идёт, а правка исходника
# сабчарта пропуск сбрасывает — то есть свойство, ради которого этот шаг здесь
# стоит, сохранено полностью.
echo "=== $SCRIPT: зависимости умбреллы (иначе рендерится вендоренная копия) ==="
bash "$REPO_ROOT/scripts/helm-umbrella-deps.sh" "$UMBRELLA" \
  || fatal "зависимости не материализованы — сабчарты не обновлены из исходников, проверка НЕ ВЫПОЛНЕНА"

# Рендер идёт через `helm_try`/`render_or_fatal`, а не `helm template … 2>/dev/null`.
# Прежняя форма гасила stderr: отказ приходил кодом 1 и БЕЗ единого слова helm,
# то есть читатель получал вердикт о дереве вместо причины отказа инструмента.
echo "=== $SCRIPT: рендер dev-профиля ==="
helm_try kacho-umbrella "$UMBRELLA" -f "$DEV"
render_or_fatal "dev-профиль"
printf '%s\n' "$HELM_OUT" >"$TMPD/dev.yaml"
echo "=== $SCRIPT: рендер production-профиля ==="
helm_try kacho-umbrella "$UMBRELLA" -f "$DEV" -f "$DEVPROD"
render_or_fatal "production-профиль"
printf '%s\n' "$HELM_OUT" >"$TMPD/prod.yaml"

# Третий рендер — с ВКЛЮЧЁННЫМИ OPA-сайдкарами. В dev/dev-prod они выключены,
# поэтому потребляемые ими ConfigMap'ы (политика, bundle-сервер, JWKS) в первых
# двух рендерах вообще не появляются, и структурная дыра в них была бы НЕВИДИМА
# до того дня, когда кто-нибудь включит OPA в проде. Проверять надо конфигурацию,
# которую чарт СПОСОБЕН выдать, а не только ту, что включена сегодня.
echo "=== $SCRIPT: рендер с включёнными OPA-сайдкарами ==="
helm_try kacho-umbrella "$UMBRELLA" -f "$DEV" -f "$DEVPROD" \
  --set vpc.opa.enabled=true \
  --set compute.opa.enabled=true \
  --set kaname.opaSidecar.enabled=true
render_or_fatal "профиль с включёнными OPA-сайдкарами"
printf '%s\n' "$HELM_OUT" >"$TMPD/opa.yaml"

python3 -c "$PY_ROLLOUT_BINDING" "$TMPD/dev.yaml" "$TMPD/prod.yaml" "$TMPD/opa.yaml" \
  || fail "привязка pod-template к содержимому настроек нарушена (перечень выше)"
ok

outcome_verdict "workload'ов и ConfigMap'ов — переписью выше"
