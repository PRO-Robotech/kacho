#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# seed-secrets-refuse-unknown-state-test.sh — посев секретов стенда не читает
# отказ API-сервера как «секрета нет» и не требует секрета, которого посадка не
# читает (задача #2803).
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# 1. ОТКАЗ `get` ЧИТАЛСЯ КАК «СЕКРЕТА НЕТ». И `scripts/stack-secrets.sh`, и
#    `scripts/dev-prod-secrets.sh` решали, есть ли секрет, кодом возврата
#    `kubectl get secret`. Любой отказ — срок, сеть, RBAC — давал ненулевой код,
#    величина чеканилась заново и уходила `kubectl apply`, а он существующий
#    объект ПЕРЕЗАПИСЫВАЕТ. Для ключей обёртки перевыпуск необратим: записанное
#    прежним ключом больше не открывается, и отказ при этом тихий.
#
# 2. ТРЕБУЕМОСТЬ В ОБХОД ПОСАДКИ. Перечень требуемых брал ВСЕ имена, которые
#    заводит посев, безусловно. Под посадкой `external` ключ обёртки секретов
#    второго фактора подключён `optional: true`, и служба его не требует; шапка
#    посева обещает, что такой стенд поднимается и без него, а предполёт
#    отказывал.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — ПАРАМИ, У КАЖДОГО ОТРИЦАНИЯ ЕСТЬ БЛИЗНЕЦ
#
#   1  находка : секрет существует, `get` отвечает отказом НЕ NotFound →
#                dev-prod-secrets.sh отказывает, resourceVersion не сдвинулся;
#   2  близнец : секретов нет (`get` → NotFound) → посев заводит все четыре;
#   3  близнец : секрет существует и `get` его видит → переиспользуется,
#                resourceVersion не сдвинулся;
#   4  гонка   : `get` сказал NotFound, а объект уже заведён (между проверкой и
#                заведением) → создание отвечает AlreadyExists, посев
#                переиспользует, resourceVersion не сдвинулся;
#   5  находка : то же для stack-secrets.sh — требуемый секрет существует,
#                `get` отвечает отказом → шаг отказывает, объект не тронут;
#   6  находка : рендер `external`, ключ второго фактора подключён
#                `optional: true` и отсутствует → stack-secrets.sh его
#                отсутствующим НЕ называет;
#   7  близнец : тот же ключ, та же посадка, ссылка БЕЗ `optional` (копия чарта
#                с одной снятой строкой) → называет;
#   8  близнец : посадка `own` — служба ключ требует → называет.
#
# КЛАСТЕРА ЗДЕСЬ НЕТ. `kubectl` и `kind` подменены двойниками в PATH: двойник
# держит состояние объектов на диске (имя → resourceVersion) и отвечает ровно
# теми текстами, которыми отвечает API-сервер (`Error from server (NotFound)`,
# `(AlreadyExists)`). `helm` настоящий: требуемое множество выводится из рендера
# цепочки deploy/stacks.txt, как на подъёме.
set -uo pipefail

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$DEPLOY/helm/umbrella"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=8

require_helm
require_python_yaml
require_umbrella_charts "$UMBRELLA"
command -v openssl >/dev/null 2>&1 || fatal "нет openssl — посев чеканит им величины (условие прогона)"

# ── ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ПРЕДПОСЫЛКИ: обе цепочки рендерятся ─────────────
# Требуемое множество посев выводит из рендера. Без этого контроля отказ рендера
# внутри посева (дерево без собранных зависимостей) читался бы как отказ посева —
# то есть как находка о дереве, которого здесь не было.
# shellcheck source=deploy/tests/helm/stacks.sh
. "$HERE/stacks.sh"
OWN_ARGS="$(stacks_args own "$UMBRELLA")" || fatal "стек own: цепочка профилей не прочитана из stacks.txt"
PROD_ARGS="$(stacks_args prod "$UMBRELLA")" || fatal "стек prod: цепочка профилей не прочитана из stacks.txt"
# shellcheck disable=SC2086  # цепочка `-f a -f b` обязана разбиться на слова
helm_try kacho-umbrella "$UMBRELLA" $OWN_ARGS
render_or_fatal "стек own (предпосылка: из его рендера посев выводит требуемое)"
# shellcheck disable=SC2086
helm_try kacho-umbrella "$UMBRELLA" $PROD_ARGS
render_or_fatal "стек prod (предпосылка: из его рендера посев выводит требуемое)"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/seed-secrets-probe.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
BIN="$WORK/bin"; mkdir -p "$BIN"

# ── ДВОЙНИК kubectl ─────────────────────────────────────────────────────────
# Состояние: $STATE/<имя>.rv — объект есть, внутри его resourceVersion.
# Режим get — $GET_MODE:
#   ok       — отвечает по состоянию (есть → 0; нет → NotFound);
#   refuse   — любой `get secret` отказывает так, как отказывает сеть;
#   notfound — `get secret` всегда отвечает NotFound (даже на существующий):
#              так выглядит гонка «проверили — и тут его завели».
# $DEFAULT_PRESENT=1 — всякий секрет, кроме перечисленных в $ABSENT, считается
# существующим без файла состояния (для суждения о требуемом множестве).
cat > "$BIN/kubectl" <<'KUBECTL'
#!/usr/bin/env bash
set -uo pipefail
ns=""; args=()
while [ $# -gt 0 ]; do
  case "$1" in
    -n|--namespace) ns="$2"; shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done
set -- "${args[@]}"
present() {
  [ -f "$STATE/$1.rv" ] && return 0
  if [ "${DEFAULT_PRESENT:-0}" = 1 ]; then
    case " ${ABSENT:-} " in *" $1 "*) return 1 ;; esac
    return 0
  fi
  return 1
}
manifest_name() { python3 -c 'import sys,yaml; print(yaml.safe_load(sys.stdin)["metadata"]["name"])'; }
case "$1 ${2:-}" in
  "config current-context") echo "$PROBE_CONTEXT"; exit 0 ;;
  "config view") echo "https://127.0.0.1:65530"; exit 0 ;;
  "get namespace") exit 0 ;;
  "get secret")
    name="$3"
    echo "get secret $name" >> "$STATE/.calls"
    case "${GET_MODE:-ok}" in
      refuse) echo "Unable to connect to the server: net/http: TLS handshake timeout" >&2; exit 1 ;;
      notfound) echo "Error from server (NotFound): secrets \"$name\" not found" >&2; exit 1 ;;
    esac
    if present "$name"; then
      case " $* " in *" -o name "*|*"-oname"*) echo "secret/$name" ;; esac
      case "$*" in *"jsonpath={.data.password}"*) printf '%s' "cHJvYmUtcGFzc3dvcmQ=" ;; esac
      exit 0
    fi
    echo "Error from server (NotFound): secrets \"$name\" not found" >&2; exit 1 ;;
  "create secret"|"create namespace")
    # клиентская сборка манифеста (--dry-run=client -o yaml) — состояния не трогает
    case "$*" in *"--dry-run=client"*) ;; *) echo "двойник: заведение без --dry-run не ожидалось: $*" >&2; exit 97 ;; esac
    kind="Secret"; [ "$2" = namespace ] && kind="Namespace"
    name="$4"; [ "$2" = namespace ] && name="$3"
    printf 'apiVersion: v1\nkind: %s\nmetadata:\n  name: %s\n' "$kind" "$name"; exit 0 ;;
  "create -f")
    name="$(manifest_name)"
    echo "create $name" >> "$STATE/.calls"
    if [ -f "$STATE/$name.rv" ]; then
      echo "Error from server (AlreadyExists): error when creating \"STDIN\": secrets \"$name\" already exists" >&2; exit 1
    fi
    echo 1 > "$STATE/$name.rv"; echo "secret/$name created"; exit 0 ;;
  "apply -f")
    name="$(manifest_name)"
    echo "apply $name" >> "$STATE/.calls"
    if [ -f "$STATE/$name.rv" ]; then echo $(( $(cat "$STATE/$name.rv") + 1 )) > "$STATE/$name.rv"; else echo 1 > "$STATE/$name.rv"; fi
    echo "secret/$name configured"; exit 0 ;;
esac
echo "двойник kubectl: неожиданный вызов: $*" >&2
exit 98
KUBECTL
cat > "$BIN/kind" <<'KIND'
#!/usr/bin/env bash
case "$*" in
  "get kubeconfig --name probe") printf 'clusters:\n- cluster:\n    server: https://127.0.0.1:65530\n' ;;
  *) exit 1 ;;
esac
KIND
chmod +x "$BIN/kubectl" "$BIN/kind"

# fresh <имя…> — новое состояние; перечисленные объекты существуют с rv=7.
fresh() {
  STATE="$WORK/state.$RANDOM$RANDOM"; mkdir -p "$STATE"; : > "$STATE/.calls"
  local n; for n in "$@"; do echo 7 > "$STATE/$n.rv"; done
  export STATE
}
rv() { cat "$STATE/$1.rv" 2>/dev/null || echo "нет"; }

seed() {  # dev-prod-secrets.sh в подменённом мире
  OUT="$(env PATH="$BIN:$PATH" KACHO_NAMESPACE=kacho bash "$DEPLOY/scripts/dev-prod-secrets.sh" 2>&1)"; RC=$?
}
stack() {  # stack-secrets.sh <стек> [каталог deploy] в подменённом мире
  local root="${2:-$DEPLOY}"
  OUT="$(env PATH="$BIN:$PATH" STACK_NAMESPACE=kacho STACK_RELEASE=kacho-umbrella \
         bash "$root/scripts/stack-secrets.sh" "$1" 2>&1)"; RC=$?
}

SEED_FOUR="kaname-jwks-enc-key kaname-second-factor-enc-key kaname-hook-token kaname-bootstrap-sa-key"
export PROBE_CONTEXT=kind-probe

# ── 1. НАХОДКА: get отказал не NotFound — посев отказывает, объект цел ──────
fresh kaname-jwks-enc-key
GET_MODE=refuse DEFAULT_PRESENT=0 seed
[ "$RC" -ne 0 ] && [ "$(rv kaname-jwks-enc-key)" = 7 ] \
  || fail "1: dev-prod-secrets.sh при отказе get (не NotFound) вышел $RC, resourceVersion ключа обёртки 7 → $(rv kaname-jwks-enc-key). Отказ API-сервера прочитан как «секрета нет», величина перечеканена поверх существующей. Вывод:
$OUT"
ok

# ── 2. БЛИЗНЕЦ: секретов нет — посев заводит все четыре ────────────────────
fresh
GET_MODE=ok DEFAULT_PRESENT=0 seed
missing=""; for n in $SEED_FOUR; do [ "$(rv "$n")" = 1 ] || missing="$missing $n"; done
[ "$RC" -eq 0 ] && [ -z "$missing" ] \
  || fail "2: на пустом кластере посев вышел $RC, не заведены:${missing:- —}. Вывод:
$OUT"
ok

# ── 3. БЛИЗНЕЦ: секрет есть и виден — переиспользуется ─────────────────────
fresh kaname-jwks-enc-key
GET_MODE=ok DEFAULT_PRESENT=0 seed
[ "$RC" -eq 0 ] && [ "$(rv kaname-jwks-enc-key)" = 7 ] \
  || fail "3: существующий ключ обёртки не переиспользован: код $RC, resourceVersion 7 → $(rv kaname-jwks-enc-key). Вывод:
$OUT"
ok

# ── 4. ГОНКА: get сказал NotFound, объект уже есть — AlreadyExists = переиспользовать
fresh kaname-jwks-enc-key
GET_MODE=notfound DEFAULT_PRESENT=0 seed
[ "$RC" -eq 0 ] && [ "$(rv kaname-jwks-enc-key)" = 7 ] \
  || fail "4: объект, заведённый между проверкой и заведением, перезаписан либо посев отказал: код $RC, resourceVersion 7 → $(rv kaname-jwks-enc-key). Заведение обязано быть атомарным на сервере (create, а не apply). Вывод:
$OUT"
ok

# ── 5. НАХОДКА: stack-secrets.sh при отказе get — отказ шага, объект цел ────
fresh kacho-umbrella-pg-iam
GET_MODE=refuse DEFAULT_PRESENT=0 stack own
[ "$RC" -ne 0 ] && [ "$(rv kacho-umbrella-pg-iam)" = 7 ] \
  || fail "5: stack-secrets.sh при отказе get вышел $RC, resourceVersion секрета базы 7 → $(rv kacho-umbrella-pg-iam). Вывод:
$OUT"
ok

# ── 6. НАХОДКА: external, optional-ссылка, секрета нет — не требуется ───────
# Площадка НЕ локальная: предполёт только судит и называет недостающее.
fresh
PROBE_CONTEXT=managed-probe GET_MODE=ok DEFAULT_PRESENT=1 ABSENT="kaname-second-factor-enc-key" stack prod
[ "$RC" -eq 0 ] && [[ "$OUT" != *"kaname-second-factor-enc-key"* ]] \
  || fail "6: стек prod (посадка external): ключ второго фактора подключён optional: true и служба его не требует, а предполёт вышел $RC и назвал его. Вывод:
$OUT"
ok

# ── 7. БЛИЗНЕЦ: та же посадка, ссылка БЕЗ optional — называет ──────────────
MIRROR="$WORK/mirror"; mkdir -p "$MIRROR/scripts" "$MIRROR/tests/helm" "$MIRROR/helm"
cp "$DEPLOY/stacks.txt" "$MIRROR/"
cp "$DEPLOY/scripts/stack-secrets.sh" "$DEPLOY/scripts/dev-prod-secrets.sh" "$MIRROR/scripts/"
cp "$DEPLOY/tests/helm/stacks.sh" "$MIRROR/tests/helm/"
cp -r "$UMBRELLA" "$MIRROR/helm/umbrella"
TPL="$MIRROR/helm/umbrella/charts/kaname/templates/deployment.yaml"
python3 - "$TPL" <<'PY' || fatal "7: копия шаблона службы доступа не подготовлена — снимать optional не на чем"
import sys
p = sys.argv[1]; s = open(p).read()
anchor = "- name: KANAME_SECOND_FACTOR_ENC_KEY"
i = s.index(anchor)
j = s.index("optional: true", i)
line_start = s.rindex("\n", 0, j) + 1
line_end = s.index("\n", j) + 1
assert s[line_start:line_end].strip() == "optional: true"
s = s[:line_start] + s[line_end:]
open(p, "w").write(s)
PY
fresh
PROBE_CONTEXT=managed-probe GET_MODE=ok DEFAULT_PRESENT=1 ABSENT="kaname-second-factor-enc-key" stack prod "$MIRROR"
[ "$RC" -ne 0 ] && [[ "$OUT" == *"kaname-second-factor-enc-key"* ]] \
  || fail "7: ссылка на ключ второго фактора БЕЗ optional, секрета нет — предполёт вышел $RC и его не назвал: близнец утверждения 6 не различает. Вывод:
$OUT"
ok

# ── 8. БЛИЗНЕЦ: посадка own — служба ключ требует — называет ───────────────
fresh
PROBE_CONTEXT=managed-probe GET_MODE=ok DEFAULT_PRESENT=1 ABSENT="kaname-second-factor-enc-key" stack own
[ "$RC" -ne 0 ] && [[ "$OUT" == *"kaname-second-factor-enc-key"* ]] \
  || fail "8: стек own (служба требует ключ второго фактора), секрета нет — предполёт вышел $RC и его не назвал. Вывод:
$OUT"
ok

[ "$N" -eq "$EXPECTED_ASSERTIONS" ] || fail "выполнено $N утверждений из $EXPECTED_ASSERTIONS"
echo "PASS: $SCRIPT ($N assertions) — dev-prod-secrets.sh 4 мира, stack-secrets.sh 4 мира (own/prod/копия prod без optional)"
