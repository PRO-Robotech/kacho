#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Манифесты модулей kaname собираются из НЕСКОЛЬКИХ ConfigMap, подобранных по
# метке, в один каталог доставки — и ни один файл не подменяет другой молча
# (kacho#3356).
#
# ПРЕДМЕТ. Прежде источник у каталога был ОДИН — `manifests.configMapName`. Модуль,
# чей манифест поставляет другой чарт, положить его в этот объект не мог: объект
# порождает производитель манифестов дерева, а не поставщик модуля. Селектор
# `manifests.selector` подбирает дополнительные источники по метке, и проба судит
# четыре свойства исходом рендера, а не объявлением:
#
#   1. обратная совместимость: без селектора рендер РАВЕН золотому файлу,
#      снятому до правки (`testdata/kaname-module-manifests/*.golden.json`) —
#      и для единого ConfigMap, и для посадки без доставки;
#   2. подбор: в каталог попадают ключи КАЖДОГО подобранного источника и
#      прежнего единого, а неподобранные (без метки, с иным значением метки) — нет;
#      одинаковые ключи двух источников приезжают ДВУМЯ файлами — подмены нет;
#   3. отпечаток: правка содержимого любого подобранного источника (и единого)
#      меняет отпечаток шаблона пода, правка неподобранного — не меняет;
#   4. отказы: селектор без чтения кластера, названный единый источник, которого
#      в кластере нет, и двоичные данные в источнике — отказ рендера с причиной,
#      а не молчаливо урезанный каталог.
#
# Чего проба НЕ утверждает. Что два манифеста ОДНОГО модуля из двух источников
# отвергаются — предмет процесса: обход каталога доставки отказывает в старте и
# называет оба файла (kaname, `internal/manifest/moduleset.go`, `ErrModuleCollision`;
# проба там же — TestDeliveryRefusesAModuleNameThatCollidesWithAnAlreadyDeclaredOne).
# Здесь держится ПРЕДПОСЫЛКА того отказа: оба файла доезжают до каталога под
# разными именами, и имя файла называет источник.
#
# КАК ЧИТАЕТСЯ КЛАСТЕР БЕЗ КЛАСТЕРА. Подбор — `lookup` helm; офлайн он пуст.
# Ветку «прочитан» судит узел `kube-api-fixture.py`: helm ходит к нему своим
# клиентом (`--dry-run=server`), а подменён лишь источник объектов.
#
# Офлайновая проверка, как и остальные tests/helm/*: рендерит сам чарт службы,
# зависимости умбреллы ей не нужны.
set -euo pipefail

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
CHART="$REPO_ROOT/helm/umbrella/charts/kaname"
GOLDEN="$HERE/testdata/kaname-module-manifests"
NS=kaname-probe
LABEL_KEY="kaname.in-cloud.io/module-manifest"
DIGEST_KEY="kacho.cloud/module-manifests-selected-digest"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=12

require_helm
require_python_yaml
require_file_present "$CHART/Chart.yaml" "чарт службы kaname"
require_file_present "$GOLDEN/legacy.golden.json" "золотой файл единого источника"
require_file_present "$GOLDEN/none.golden.json" "золотой файл посадки без доставки"

WORK="$(mktemp -d)"
FIXTURE_PID=""
cleanup() {
  [ -n "$FIXTURE_PID" ] && kill "$FIXTURE_PID" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

# ── выжимка рендера: всё, чем доставка видна поду и процессу ────────────────
# Одна реализация на все утверждения и на снятие золотого файла: две копии
# выжимки разошлись бы, и сравнение с золотым судило бы не тот предмет.
PY_EXTRACT=$(cat <<'PYSRC'
import json, sys, yaml
docs = [d for d in yaml.safe_load_all(sys.stdin) if isinstance(d, dict)]
dep = [d for d in docs if d.get("kind") == "Deployment"]
cfg = [d for d in docs if d.get("kind") == "ConfigMap" and "config.yaml" in (d.get("data") or {})]
if len(dep) != 1 or len(cfg) != 1:
    sys.stderr.write("выжимка: Deployment %d, ConfigMap настроек %d — ожидалось по одному\n" % (len(dep), len(cfg)))
    sys.exit(3)
tpl = dep[0]["spec"]["template"]
ann = {k: v for k, v in (tpl["metadata"].get("annotations") or {}).items() if "module-manifests" in k}
vols = [v for v in tpl["spec"].get("volumes") or [] if v.get("name") == "module-manifests"]
mounts = {}
for c in (tpl["spec"].get("initContainers") or []) + tpl["spec"]["containers"]:
    ms = [m for m in c.get("volumeMounts") or [] if m.get("name") == "module-manifests"]
    if ms:
        mounts[c["name"]] = ms
conf = yaml.safe_load(cfg[0]["data"]["config.yaml"]) or {}
print(json.dumps({"annotations": ann, "volumes": vols, "mounts": mounts,
                  "config_manifests": conf.get("manifests")}, sort_keys=True, indent=2))
PYSRC
)
extract() { python3 -c "$PY_EXTRACT"; }

# Значения проб пишутся файлами: ключ метки содержит точки, и `--set` его разрезал бы.
values() { # values <имя> <configMapName> <selector-json|-> <required>
  {
    echo "manifests:"
    echo "  configMapName: \"$2\""
    echo "  required: $4"
    [ "$3" = "-" ] || echo "  selector: $3"
  } > "$WORK/$1.yaml"
  echo "$WORK/$1.yaml"
}
SEL="{\"$LABEL_KEY\": \"true\"}"
V_LEGACY="$(values legacy kacho-module-manifests - true)"
V_NONE="$(values none "" - false)"
V_SEL="$(values sel kacho-module-manifests "$SEL" true)"
V_SEL_ONLY="$(values selonly "" "$SEL" true)"

offline() { helm_try kaname "$CHART" -n "$NS" -f "$1"; }
online()  { helm_try kaname "$CHART" -n "$NS" -f "$1" --dry-run=server --disable-openapi-validation \
              --kubeconfig "$WORK/kubeconfig" --kube-context fixture; }

# ── 1. обратная совместимость: рендер без селектора равен снятому до правки ─
offline "$V_LEGACY"; render_nonempty_or_fatal "единый источник, офлайн"
GOT="$(printf '%s\n' "$HELM_OUT" | extract)" || fatal "выжимка рендера единого источника не состоялась"
[ "$GOT" = "$(cat "$GOLDEN/legacy.golden.json")" ] || {
  diff <(printf '%s\n' "$GOT") "$GOLDEN/legacy.golden.json" >&2 || true
  fail "единый источник без селектора рендерится НЕ так, как до правки (выше — разница с золотым файлом)"; }
ok

offline "$V_NONE"; render_nonempty_or_fatal "посадка без доставки, офлайн"
GOT="$(printf '%s\n' "$HELM_OUT" | extract)" || fatal "выжимка рендера посадки без доставки не состоялась"
[ "$GOT" = "$(cat "$GOLDEN/none.golden.json")" ] || {
  diff <(printf '%s\n' "$GOT") "$GOLDEN/none.golden.json" >&2 || true
  fail "посадка без доставки рендерится НЕ так, как до правки (выше — разница с золотым файлом)"; }
ok

# ── 4а. селектор без чтения кластера — отказ, а не урезанный каталог ─────────
offline "$V_SEL"
render_must_fail_because "кластер не прочитал" "селектор, офлайн" \
  "селектор объявлен, рендер кластера не читал — и прошёл: каталог доставки молча остался без подобранных источников"
ok

# ── узел API: объекты пробы, порт, kubeconfig ───────────────────────────────
cm() { # cm <имя> <значение метки|-> <ключ=значение>… → объект ConfigMap JSON
  python3 - "$@" <<'PYSRC'
import json, sys
name, label, pairs = sys.argv[1], sys.argv[2], sys.argv[3:]
meta = {"name": name, "namespace": "kaname-probe"}
if label != "-":
    meta["labels"] = {"kaname.in-cloud.io/module-manifest": label}
data = dict(p.split("=", 1) for p in pairs)
print(json.dumps({"apiVersion": "v1", "kind": "ConfigMap", "metadata": meta,
                  "data": {k: v.replace("\\n", "\n") for k, v in data.items()}}))
PYSRC
}
objects() { # objects <ConfigMap JSON>… → файл объектов узла
  python3 -c 'import json,sys; print(json.dumps({"kaname-probe": [json.loads(a) for a in sys.argv[1:]]}))' "$@" \
    > "$WORK/objects.json"
}
LEGACY="$(cm kacho-module-manifests - 'iam.yaml=module: iam\n' 'vpc.yaml=module: vpc\n')"
REGISTRY="$(cm registry-platform-manifests true 'manifest.yaml=module: registry\n')"
ACME="$(cm acme-manifests true 'manifest.yaml=module: acme\n')"
UNLABELED="$(cm unrelated-config - 'manifest.yaml=module: stray\n')"
OPTED_OUT="$(cm opted-out false 'manifest.yaml=module: other\n')"
objects "$LEGACY" "$REGISTRY" "$ACME" "$UNLABELED" "$OPTED_OUT"

python3 "$HERE/kube-api-fixture.py" "$WORK/objects.json" "$WORK/port" &
FIXTURE_PID=$!
for _ in $(seq 1 50); do [ -s "$WORK/port" ] && break; sleep 0.1; done
[ -s "$WORK/port" ] || fatal "узел API пробы не начал слушать за 5 с — условие не создано"
cat > "$WORK/kubeconfig" <<EOF
apiVersion: v1
kind: Config
clusters: [{name: fixture, cluster: {server: "http://127.0.0.1:$(cat "$WORK/port")"}}]
users: [{name: fixture, user: {token: probe}}]
contexts: [{name: fixture, context: {cluster: fixture, user: fixture, namespace: $NS}}]
current-context: fixture
EOF

# ── 1б. без селектора кластер не читается: прочитанный кластер ничего не меняет ─
online "$V_LEGACY"; render_nonempty_or_fatal "единый источник, кластер прочитан"
GOT="$(printf '%s\n' "$HELM_OUT" | extract)" || fatal "выжимка не состоялась"
[ "$GOT" = "$(cat "$GOLDEN/legacy.golden.json")" ] || {
  diff <(printf '%s\n' "$GOT") "$GOLDEN/legacy.golden.json" >&2 || true
  fail "без селектора рендер зависит от содержимого кластера — посадка без подбора обязана рендериться как прежде"; }
ok

# ── 2. подбор: источники, ключи, отсутствие подмены ─────────────────────────
PY_JUDGE=$(cat <<'PYSRC'
import json, sys
got = json.load(sys.stdin)
mode = sys.argv[1]
def src(name, *keys):
    return {"configMap": {"name": name, "items": [{"key": k, "path": "%s__%s" % (name, k)} for k in keys]}}
want = [src("acme-manifests", "manifest.yaml")]
if mode == "with-legacy":
    want.append(src("kacho-module-manifests", "iam.yaml", "vpc.yaml"))
want.append(src("registry-platform-manifests", "manifest.yaml"))
vols = got["volumes"]
errs = []
if len(vols) != 1 or "projected" not in vols[0]:
    errs.append("том module-manifests не проецируемый либо не один: %s" % json.dumps(vols))
else:
    srcs = vols[0]["projected"]["sources"]
    if srcs != want:
        errs.append("источники тома:\n  есть   %s\n  ждали  %s" % (json.dumps(srcs), json.dumps(want)))
    paths = [i["path"] for s in srcs for i in s["configMap"].get("items", [])]
    if len(paths) != len(set(paths)):
        errs.append("пути тома повторяются — один файл молча подменил бы другой: %s" % paths)
    if any(s["configMap"].get("optional") for s in srcs):
        errs.append("источник объявлен optional — исчезнувший источник урезал бы каталог молча")
if not got["mounts"]:
    errs.append("каталог доставки не смонтирован ни одному контейнеру")
if not (got["config_manifests"] or {}).get("dir"):
    errs.append("manifests.dir процесса пуст — подобранное не читается")
if not got["annotations"].get(sys.argv[2]):
    errs.append("шаблон пода не несёт отпечатка подобранного (%s)" % sys.argv[2])
print("\n".join(errs))
PYSRC
)
judge() { printf '%s\n' "$HELM_OUT" | extract | python3 -c "$PY_JUDGE" "$1" "$DIGEST_KEY"; }

online "$V_SEL"; render_nonempty_or_fatal "селектор и единый источник, кластер прочитан"
ERRS="$(judge with-legacy)" || fatal "суд выжимки не состоялся"
[ -z "$ERRS" ] || fail "подбор вместе с единым источником: $ERRS"
ok
DIGEST_BASE="$(printf '%s\n' "$HELM_OUT" | extract | python3 -c 'import json,sys; print(json.load(sys.stdin)["annotations"].get(sys.argv[1],""))' "$DIGEST_KEY")"

online "$V_SEL_ONLY"; render_nonempty_or_fatal "только селектор, кластер прочитан"
ERRS="$(judge selector-only)" || fatal "суд выжимки не состоялся"
[ -z "$ERRS" ] || fail "подбор без единого источника: $ERRS"
ok

# ── 3. отпечаток: подобранное меняет, неподобранное — нет ────────────────────
digest_with() { # digest_with <ConfigMap JSON>… → отпечаток рендера V_SEL
  objects "$@"; online "$V_SEL"; render_nonempty_or_fatal "отпечаток"
  printf '%s\n' "$HELM_OUT" | extract | python3 -c 'import json,sys; print(json.load(sys.stdin)["annotations"].get(sys.argv[1],""))' "$DIGEST_KEY"
}
[ -n "$DIGEST_BASE" ] || fail "отпечаток подобранного пуст"
D="$(digest_with "$LEGACY" "$(cm registry-platform-manifests true 'manifest.yaml=module: registry\nnote: edited\n')" "$ACME" "$UNLABELED" "$OPTED_OUT")"
[ "$D" != "$DIGEST_BASE" ] || fail "правка подобранного источника не изменила отпечаток — под не перекатится"
ok
D="$(digest_with "$(cm kacho-module-manifests - 'iam.yaml=module: iam\nnote: edited\n' 'vpc.yaml=module: vpc\n')" "$REGISTRY" "$ACME" "$UNLABELED" "$OPTED_OUT")"
[ "$D" != "$DIGEST_BASE" ] || fail "правка единого источника под селектором не изменила отпечаток"
ok
# Близнец: правка того, что НЕ подобрано, отпечаток не трогает — иначе краснота
# выше неотличима от отпечатка, меняющегося от всего кластера.
D="$(digest_with "$LEGACY" "$REGISTRY" "$ACME" "$(cm unrelated-config - 'manifest.yaml=edited\n')" "$OPTED_OUT")"
[ "$D" = "$DIGEST_BASE" ] || fail "правка НЕподобранного ConfigMap изменила отпечаток — отпечаток судит не подбор"
ok
D="$(digest_with "$LEGACY" "$REGISTRY" "$ACME" "$UNLABELED" "$OPTED_OUT")"
[ "$D" = "$DIGEST_BASE" ] || fail "отпечаток одного и того же содержимого не детерминирован"
ok

# ── 4б. единый источник назван, а в кластере его нет ────────────────────────
objects "$REGISTRY" "$ACME"
online "$V_SEL"
render_must_fail_because "нет в пространстве имён" "селектор, единого источника нет" \
  "единый источник назван и отсутствует, а рендер прошёл: каталог молча остался без его манифестов"
ok

# ── 4в. двоичные данные в подобранном источнике ─────────────────────────────
BIN="$(python3 -c 'import json,sys; o=json.loads(sys.argv[1]); o["binaryData"]={"blob.bin":"AAE="}; print(json.dumps(o))' "$ACME")"
objects "$LEGACY" "$REGISTRY" "$BIN"
online "$V_SEL"
render_must_fail_because "binaryData" "селектор, двоичные данные" \
  "двоичные данные подобранного источника не отвергнуты: ключ binaryData молча не доехал бы до каталога"
ok

outcome_verdict "источников в пробах: 5 (единый, два подобранных, без метки, с иным значением метки)"
exit 0
