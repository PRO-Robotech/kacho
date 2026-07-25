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
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
DEV="$UMBRELLA/values.dev.yaml"
DEVPROD="$UMBRELLA/values.dev-prod.yaml"

fail() { echo "FAIL: $1"; exit 1; }

[ -f "$DEV" ]     || fail "values.dev.yaml не найден ($DEV)"
[ -f "$DEVPROD" ] || fail "values.dev-prod.yaml не найден ($DEVPROD)"

# PyYAML, а НЕ yq: на машинах разработчиков /usr/bin/yq регулярно оказывается
# python-обёрткой над jq вместо mikefarah yq v4. Её синтаксис несовместим,
# вызов молча отдаёт пустой вывод — и проверка, сравнивающая с пустотой,
# «зеленеет», ничего не проверив. Именно этот класс ложного зелёного тут и ловим,
# так что сами на него наступать не будем.
python3 -c 'import yaml' 2>/dev/null || fail "нужен python3 с PyYAML (pip install pyyaml)"

TMPD="$(mktemp -d)"
trap 'rm -rf "$TMPD"' EXIT

# ОБЯЗАТЕЛЬНО перед рендером: сервисные сабчарты вендорятся в charts/*.tgz, и
# `helm template` берёт ИМЕННО их, а не исходники в services/*/deploy. Без
# обновления зависимостей проверка молча аттестовала бы СТАРУЮ копию чарта:
# правку (или её потерю) в исходнике она бы не увидела вовсе. Проверено — без
# этого шага тест оставался зелёным на заведомо сломанном storage-чарте.
# Сбой обновления — КРАСНОЕ: непроверенный исходник это не «всё хорошо».
echo "=== $SCRIPT: helm dep update (иначе рендерится вендоренная копия) ==="
( cd "$UMBRELLA" && helm dep update >/dev/null 2>&1 ) \
  || fail "helm dep update сорвался — сабчарты не обновлены из исходников, проверка НЕ ВЫПОЛНЕНА"

echo "=== $SCRIPT: рендер dev-профиля ==="
helm template kacho-umbrella "$UMBRELLA" -f "$DEV" >"$TMPD/dev.yaml" 2>/dev/null \
  || fail "helm template (dev) сорвался"
echo "=== $SCRIPT: рендер production-профиля ==="
helm template kacho-umbrella "$UMBRELLA" -f "$DEV" -f "$DEVPROD" >"$TMPD/prod.yaml" 2>/dev/null \
  || fail "helm template (dev-prod) сорвался"

# Третий рендер — с ВКЛЮЧЁННЫМИ OPA-сайдкарами. В dev/dev-prod они выключены,
# поэтому потребляемые ими ConfigMap'ы (политика, bundle-сервер, JWKS) в первых
# двух рендерах вообще не появляются, и структурная дыра в них была бы НЕВИДИМА
# до того дня, когда кто-нибудь включит OPA в проде. Проверять надо конфигурацию,
# которую чарт СПОСОБЕН выдать, а не только ту, что включена сегодня.
echo "=== $SCRIPT: рендер с включёнными OPA-сайдкарами ==="
helm template kacho-umbrella "$UMBRELLA" -f "$DEV" -f "$DEVPROD" \
  --set vpc.opa.enabled=true \
  --set compute.opa.enabled=true \
  --set kacho-iam.opaSidecar.enabled=true >"$TMPD/opa.yaml" 2>/dev/null \
  || fail "helm template (opa on) сорвался"

python3 - "$TMPD/dev.yaml" "$TMPD/prod.yaml" "$TMPD/opa.yaml" <<'PY'
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
# Здесь проверяется покрытие: у workload'а должно быть не меньше аннотаций,
# привязанных к содержимому (ключ содержит "checksum"), чем он потребляет
# ConfigMap'ов. Это осознанно СТРУКТУРНАЯ проверка, а не поведенческая — она не
# доказывает, что аннотация хэширует именно «свой» ConfigMap; она ловит то, что
# реально ломается на практике: добавили потребляемый ConfigMap и забыли
# аннотацию (так и появились три латентные дыры под OPA — vpc/compute/iam).
#
# Не-checksum аннотации (`openfga-model-id-rev` и т.п.) НЕ засчитываются: они
# меняются по своей причине и покрытием конфига не являются.
opa_w, opa_cm = index(load(sys.argv[3]))
gaps = []
for key in sorted(opa_w.keys()):
    kind, name = key
    cms = sorted(c for c in consumed_configmaps(opa_w[key]) if c in opa_cm)
    if not cms:
        continue
    binding = [k for k in pod_annotations(opa_w[key]) if "checksum" in k.lower()]
    if len(binding) < len(cms):
        gaps.append(
            f"{kind}/{name}: потребляет {len(cms)} ConfigMap'ов ({', '.join(cms)}), "
            f"а привязанных к содержимому аннотаций всего {len(binding)} "
            f"({', '.join(sorted(binding)) or 'ни одной'})"
        )

if gaps:
    print()
    for g in gaps:
        print(f"FAIL: {g}")
    print()
    print("Каждый потребляемый ConfigMap обязан иметь СВОЮ content-аннотацию —")
    print("иначе его правка не перекатит под и процесс останется со старым содержимым.")
    sys.exit(1)

print("[2/2] OK — включая конфигурации, выключенные в dev (OPA-сайдкары)")
print("\nconfig-rollout-binding: OK")
PY
