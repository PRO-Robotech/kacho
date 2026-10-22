#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Подъём стенда с включённым подбором манифестов по метке проходит предполёт
# (kacho#3356, SD-3356-1).
#
# ПРЕДМЕТ. Цель `stack-up` (deploy/Makefile) и подъём стенда во внешнем кластере
# (`scripts/stand-ns.sh up`) перед `helm upgrade` зовут предполёт
# `scripts/stack-secrets.sh`: он рендерит ТУ ЖЕ цепочку офлайн (`helm template`
# без чтения кластера) и выводит из рендера требуемые секреты. Подбор манифестов
# (`kaname.manifests.selector`) — свойство рендера, читающего кластер (`lookup`),
# и офлайн-рендер с ним прежде ОТКАЗЫВАЛ: подъём стенда с селектором обрывался на
# предполёте, раньше `helm upgrade`. Тот же отказ ждал каждый офлайн-рендер
# стека — предрендер стенда (`stand-ns.sh`), рендер стеков конвейера, предполёт
# площадки (`helm/umbrella/cutover-fe3455.sh`).
#
# Почему не `--dry-run=server` в предполёте. Рендер шаблона, читающий кластер,
# спрашивает сервер о КАЖДОМ отрендеренном объекте (замер узлом
# `kube-api-fixture.py`: GET по каждому объекту чарта сверх обнаружения), а
# предполёт стенда во внешнем кластере рендерит без переноса в пространство
# стенда — вопросы ушли бы об объектах чужого релиза. И предрендер стенда идёт ДО
# того, как пространство и единый источник манифестов заведены: читать там
# нечего. Поэтому решение — в чарте: офлайн-рендер с селектором проходит формой,
# которую не установить (проба формы — kaname-module-manifests-select-test.sh §5).
#
# Что утверждается:
#   1. контроль: предполёт цепочки dev-prod без селектора проходит — мир пробы
#      исправен, и краснота ниже принадлежит селектору;
#   2. тот же предполёт с селектором проходит и требует тех же секретов;
#   3. офлайн-рендер той же цепочки с селектором даёт службе доступа форму,
#      которую не установить: единственный источник тома — объект-метка.
#
# Кластер подменён двойником kubectl: он отвечает «есть» на вопрос о каждом
# секрете и отказывает на любом ином обращении, так что предполёт ничего не
# заводит. helm — настоящий: судится рендер дерева.
set -uo pipefail

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$DEPLOY/helm/umbrella"
STACK=dev-prod
OFFLINE_SOURCE="kaname-offline-render-not-installable"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=3

require_helm
require_python_yaml
command -v openssl >/dev/null 2>&1 || fatal "нет openssl — предполёт его требует, и без него он отказал бы раньше рендера"
# Зависимости умбреллы — вопрос их владельца (тот же, что у premise.sh), но ответ
# — исходом этого каталога: «условие не создано» обязано нести перепись
# (three-outcomes-distinguishable-test.sh), а пропуск premise.sh её не печатает.
DEPS_RC=0
DEPS_OUT="$(bash "$DEPLOY/scripts/helm-umbrella-deps.sh" --check 2>&1)" || DEPS_RC=$?
case "$DEPS_RC" in
  0) ;;
  2) printf '%s\n' "$DEPS_OUT" >&2
     fatal "зависимости умбреллы не материализованы — рендер цепочки невозможен (собрать: bash deploy/scripts/helm-umbrella-deps.sh)" ;;
  *) printf '%s\n' "$DEPS_OUT" >&2
     fail "владелец вопроса о зависимостях сам отказал кодом $DEPS_RC — это не «условие не создано»" ;;
esac

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

BIN="$WORK/bin"; mkdir -p "$BIN"
cat > "$BIN/kubectl" <<'KUBECTL'
#!/usr/bin/env bash
# Двойник кластера: секрет есть всегда, иное обращение — отказ с кодом 98.
set -uo pipefail
echo "$*" >> "$CALLS"
ns=""; args=()
while [ $# -gt 0 ]; do
  case "$1" in -n|--namespace) ns="$2"; shift 2 ;; *) args+=("$1"); shift ;; esac
done
set -- "${args[@]}"
case "$1 ${2:-}" in
  "get namespace") exit 0 ;;
  "get secret")
    case "$*" in *"-o name"*) echo "secret/$3"; exit 0 ;; esac ;;
esac
echo "двойник kubectl: неожиданный вызов: $*" >&2; exit 98
KUBECTL
chmod +x "$BIN/kubectl"

# Отпечаток доставки — тот слой, который подъём кладёт рядом с цепочкой.
MM="$WORK/module-manifests.yaml"
printf 'global:\n  kachoModuleManifests:\n    digest: "probe"\n' > "$MM"
SEL="$WORK/selector.yaml"
printf 'kaname:\n  manifests:\n    selector:\n      kaname.in-cloud.io/module-manifest: "true"\n' > "$SEL"

ALLOWED_CALL='^(-n [^ ]+ )?get (namespace|secret [^ ]+ -o name)'
preflight() { # preflight [-f <слой>…] → OUT, RC, CALLS
  CALLS="$WORK/calls.$RANDOM"; : > "$CALLS"
  OUT="$(env PATH="$BIN:$PATH" CALLS="$CALLS" STACK_NAMESPACE=kacho STACK_RELEASE=kacho-umbrella \
    bash "$DEPLOY/scripts/stack-secrets.sh" "$STACK" -f "$MM" "$@" 2>&1)"; RC=$?
}
expect_pass() { # expect_pass <что>
  if [ "$RC" -ne 0 ] || [[ "$OUT" != *"отсутствует 0"* ]]; then
    printf '%s\n' "$OUT" | tail -n 25 >&2
    fail "$1: предполёт вышел кодом $RC (ждали 0 и «отсутствует 0»; хвост вывода выше)"
  fi
  # Обращение вне вопроса о секрете — двойник отказал бы кодом 98, и предполёт
  # вышел бы не нулём; запись обращений называет его, если отказ проглочен.
  if grep -qvE "$ALLOWED_CALL" "$CALLS"; then
    fail "$1: предполёт обратился к кластеру не вопросом о секрете: $(grep -vE "$ALLOWED_CALL" "$CALLS" | head -3 | tr '\n' ';')"
  fi
  REQ="$(printf '%s\n' "$OUT" | sed -n 's/.*требуется \([0-9]*\),.*/\1/p' | tail -1)"
}

# ── 1. контроль: цепочка без селектора ──────────────────────────────────────
preflight
expect_pass "цепочка $STACK без селектора"
REQ_BASE="$REQ"
[ -n "$REQ_BASE" ] || fatal "перепись предполёта не назвала числа требуемых секретов — сверять нечем"
ok

# ── 2. та же цепочка с селектором ───────────────────────────────────────────
preflight -f "$SEL"
expect_pass "цепочка $STACK с селектором"
[ "$REQ" = "$REQ_BASE" ] || fail "селектор изменил число требуемых секретов: $REQ против $REQ_BASE без него — подбор манифестов секретов не заводит"
ok

# ── 3. офлайн-рендер цепочки с селектором: форма службы доступа ─────────────
ARGS="$(bash "$HERE/stacks.sh" --args "$STACK" "$UMBRELLA")" || fatal "цепочка $STACK не прочитана"
# shellcheck disable=SC2086 # цепочка — слова `-f <файл>`
helm_try kacho-umbrella "$UMBRELLA" -n kacho $ARGS -f "$MM" -f "$SEL"
[ "$HELM_RC" -eq 0 ] || { helm_said "$STACK с селектором, офлайн"
  fail "офлайн-рендер цепочки $STACK с селектором отказал — предрендер стенда и рендер стеков конвейера обрываются"; }
render_nonempty_or_fatal "$STACK с селектором, офлайн"
NAMES="$(printf '%s\n' "$HELM_OUT" | python3 -c '
import json, sys, yaml
seen = []
for d in yaml.safe_load_all(sys.stdin):
    if not isinstance(d, dict) or d.get("kind") != "Deployment":
        continue
    for v in d["spec"]["template"]["spec"].get("volumes") or []:
        if v.get("name") == "module-manifests":
            srcs = (v.get("projected") or {}).get("sources")
            seen.append([s.get("configMap", {}).get("name") for s in srcs] if srcs is not None else v)
print(json.dumps(seen))
')" || fatal "разбор рендера цепочки не состоялся"
[ "$NAMES" = "[[\"$OFFLINE_SOURCE\"]]" ] || fail "офлайн-рендер цепочки $STACK с селектором: тома module-manifests $NAMES, ждали ровно один с источником [$OFFLINE_SOURCE] — офлайн-рендер, несущий иное, установился бы с каталогом, которого не читал"
ok

outcome_verdict "цепочка $STACK; требуемых секретов $REQ_BASE с селектором и без"
exit 0
