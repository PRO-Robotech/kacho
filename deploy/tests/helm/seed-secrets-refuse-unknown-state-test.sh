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
# 3. ПРОБА НЕ ВИДЕЛА СНЯТИЯ СВОЕГО ПРЕДМЕТА (задача #2844). Прежде отказ
#    сервера отказывал на `get` ВСЕХ секретов сразу, и утверждение требовало
#    лишь «шаг не вышел нулём и объект цел». Сними различение из посева — и
#    красное приходило от СОСЕДНЕГО секрета: dev-prod-secrets.sh с отказом по
#    ключу обёртки, прочитанным как NotFound, выходил 1 на следующем ключе;
#    stack-secrets.sh, читающий любой отказ как «нет», заводил семь секретов
#    баз и выходил 1 на проверке после посева. Объект оставался цел только
#    благодаря атомарному `create`, а его держит утверждение 5. Проба печатала
#    PASS (8 assertions) на обоих посевах без различения.
#    Теперь сервер отказывает на `get` ОДНОГО секрета, и мир судит, что шаг
#    остановился ИМЕННО на нём: после отказа не было ни одного обращения,
#    отказ назвал этот секрет, объект цел, код — по контракту посева. А то, что
#    миры различают, проба доказывает сама: близнецы 9–11 снимают различение из
#    копии посева, и мир обязан покраснеть, назвав посев и мир.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — ПАРАМИ, У КАЖДОГО ОТРИЦАНИЯ ЕСТЬ БЛИЗНЕЦ
#
#   1  находка : (×4, по секрету посева) секрет K существует, `get` K отвечает
#                отказом НЕ NotFound, прочие — NotFound → dev-prod-secrets.sh
#                отказывает, после `get` K не обращается ни к чему, отказ
#                называет K, resourceVersion K не сдвинулся;
#   2  находка : то же для stack-secrets.sh — требуемый секрет базы
#                существует, `get` его отвечает отказом → код 2 (контракт
#                посева), после отказа ни одного обращения (ничего не
#                заведено), отказ называет секрет, объект не тронут;
#   3  близнец : секретов нет (`get` → NotFound) → посев заводит все четыре;
#   4  близнец : секрет существует и `get` его видит → переиспользуется,
#                resourceVersion не сдвинулся;
#   5  гонка   : `get` сказал NotFound, а объект уже заведён (между проверкой и
#                заведением) → создание отвечает AlreadyExists, посев
#                переиспользует, resourceVersion не сдвинулся;
#   6  находка : рендер `external`, ключ второго фактора подключён
#                `optional: true` и отсутствует → stack-secrets.sh его
#                отсутствующим НЕ называет;
#   7  близнец : тот же ключ, та же посадка, ссылка БЕЗ `optional` (копия чарта
#                с одной снятой строкой) → называет;
#   8  близнец : посадка `own` — служба ключ требует → называет;
#   9  близнец : (×4) копия dev-prod-secrets.sh, где отказ `get` K читается как
#                NotFound → мир 1 для K краснеет и называет посев и секрет;
#  10  близнец : копия stack-secrets.sh, где secret_state читает любой отказ
#                как «нет» → мир 2 краснеет и называет посев;
#  11  близнец : оба посева без различения → находки называют ОБА посева.
# Миры 1 и 2 и близнецы 9–11 копят находки, а не обрываются на первой:
# посев, у которого различение снято в двух местах, называется весь.
#
# НОСИТЕЛЬ ПОСАДКИ `external` — КОПИЯ ДЕРЕВА, А НЕ СТЕНД ТАБЛИЦЫ. Посадку `own`
# объявляют все стенды deploy/stacks.txt (#2735), и стенда `external` в таблице
# нет. Утверждения 6 и 7 поэтому идут по копии дерева, в которой у корня
# цепочки `prod` сменён ровно один факт — посадка обеих половин (`own` →
# `external`); всё остальное — рендер той же цепочки тем же helm. Утверждение 8
# — на настоящем стенде `own`.
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
EXPECTED_ASSERTIONS=17

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
#   ok         — отвечает по состоянию (есть → 0; нет → NotFound);
#   refuse-one — `get secret $REFUSE_NAME` отказывает так, как отказывает
#                сеть; прочие отвечают по состоянию. Отказ ОДНОГО секрета — чтобы
#                красное не могло прийти от соседнего;
#   notfound   — `get secret` всегда отвечает NotFound (даже на существующий):
#                так выглядит гонка «проверили — и тут его завели».
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
      refuse-one)
        if [ "$name" = "${REFUSE_NAME:-}" ]; then
          echo "Unable to connect to the server: net/http: TLS handshake timeout" >&2; exit 1
        fi ;;
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

seed() {  # dev-prod-secrets.sh [каталог deploy] в подменённом мире
  local root="${1:-$DEPLOY}"
  OUT="$(env PATH="$BIN:$PATH" KACHO_NAMESPACE=kacho bash "$root/scripts/dev-prod-secrets.sh" 2>&1)"; RC=$?
}
stack() {  # stack-secrets.sh <стек> [каталог deploy] в подменённом мире
  local root="${2:-$DEPLOY}"
  OUT="$(env PATH="$BIN:$PATH" STACK_NAMESPACE=kacho STACK_RELEASE=kacho-umbrella \
         bash "$root/scripts/stack-secrets.sh" "$1" 2>&1)"; RC=$?
}

SEED_FOUR="kaname-jwks-enc-key kaname-second-factor-enc-key kaname-hook-token kaname-bootstrap-sa-key"
# Секрет базы, требуемый стендом own: на нём мир 2 судит stack-secrets.sh.
STACK_REFUSED=kacho-umbrella-pg-iam
export PROBE_CONTEXT=kind-probe

# last_call — последнее обращение к API-серверу в мире («нет» — не было ни одного).
last_call() { tail -n 1 "$STATE/.calls" 2>/dev/null | grep . || echo "нет"; }

# ── МИРЫ «СЕРВЕР ОТКАЗАЛ НА get ОДНОГО СЕКРЕТА» ─────────────────────────────
# refusal_worlds <каталог deploy> <dev-prod|stack|all> [секрет] — по строке на
# мир (секрет сужает миры dev-prod-secrets.sh до одного):
#   ok|<посев>|<мир>            — шаг остановился на отказе, как обещает посев;
#   red|<посев>|<мир>|<что не так>.
# Мир судит ВСЁ, что делает отказ отказом шага, а не одно «вышел не нулём»:
# без различения посев тоже выходит не нулём — на соседнем секрете.
refusal_worlds() {
  local root="$1" which="$2" only="${3:-}" k why
  if [ "$which" != stack ]; then
    for k in ${only:-$SEED_FOUR}; do
      fresh "$k"
      GET_MODE=refuse-one REFUSE_NAME="$k" DEFAULT_PRESENT=0 seed "$root"
      why=""
      [ "$RC" -ne 0 ] || why="$why; вышел 0"
      [ "$(last_call)" = "get secret $k" ] || why="$why; после отказа по $k шаг продолжился (последнее обращение: $(last_call))"
      [[ "$OUT" == *"секрет $k"*"НЕ УСТАНОВЛЕНО"* ]] || why="$why; отказ не назвал $k как секрет, чьё присутствие не установлено"
      [ "$(rv "$k")" = 7 ] || why="$why; resourceVersion $k 7 → $(rv "$k")"
      if [ -n "$why" ]; then echo "red|dev-prod-secrets.sh|сервер отказал на get $k|${why#; } (код $RC)"
      else echo "ok|dev-prod-secrets.sh|сервер отказал на get $k"; fi
    done
  fi
  if [ "$which" != dev-prod ]; then
    fresh "$STACK_REFUSED"
    GET_MODE=refuse-one REFUSE_NAME="$STACK_REFUSED" DEFAULT_PRESENT=0 stack own "$root"
    why=""
    [ "$RC" -eq 2 ] || why="$why; код $RC, а контракт посева для отказа сервера — 2"
    [ "$(last_call)" = "get secret $STACK_REFUSED" ] || why="$why; после отказа по $STACK_REFUSED шаг продолжился (последнее обращение: $(last_call); заведений $(grep -cE '^(create|apply) ' "$STATE/.calls"))"
    [[ "$OUT" == *"секрет $STACK_REFUSED"*"НЕ УСТАНОВЛЕНО"* ]] || why="$why; отказ не назвал $STACK_REFUSED как секрет, чьё присутствие не установлено"
    [ "$(rv "$STACK_REFUSED")" = 7 ] || why="$why; resourceVersion $STACK_REFUSED 7 → $(rv "$STACK_REFUSED")"
    if [ -n "$why" ]; then echo "red|stack-secrets.sh|сервер отказал на get $STACK_REFUSED (стенд own)|${why#; }"
    else echo "ok|stack-secrets.sh|сервер отказал на get $STACK_REFUSED (стенд own)"; fi
  fi
}

# ── 1–2. НАХОДКА: отказ get — отказ шага, на ТОМ ЖЕ секрете, объект цел ────
WORLDS="$(refusal_worlds "$DEPLOY" all)"
[ "$(printf '%s\n' "$WORLDS" | grep -c '^\(ok\|red\)|')" -eq 5 ] \
  || fail "1–2: миров отказа исполнено не 5 (четыре у dev-prod-secrets.sh, один у stack-secrets.sh). Вывод:
$WORLDS"
while IFS='|' read -r verdict seedname world what; do
  [ "$verdict" = red ] && violation "1–2: посев $seedname · мир «$world»: $what — отказ API-сервера прочитан как «секрета нет»"
  ok
done <<<"$WORLDS"

# ── 3. БЛИЗНЕЦ: секретов нет — посев заводит все четыре ────────────────────
fresh
GET_MODE=ok DEFAULT_PRESENT=0 seed
missing=""; for n in $SEED_FOUR; do [ "$(rv "$n")" = 1 ] || missing="$missing $n"; done
[ "$RC" -eq 0 ] && [ -z "$missing" ] \
  || fail "3: на пустом кластере посев вышел $RC, не заведены:${missing:- —}. Вывод:
$OUT"
ok

# ── 4. БЛИЗНЕЦ: секрет есть и виден — переиспользуется ─────────────────────
fresh kaname-jwks-enc-key
GET_MODE=ok DEFAULT_PRESENT=0 seed
[ "$RC" -eq 0 ] && [ "$(rv kaname-jwks-enc-key)" = 7 ] \
  || fail "4: существующий ключ обёртки не переиспользован: код $RC, resourceVersion 7 → $(rv kaname-jwks-enc-key). Вывод:
$OUT"
ok

# ── 5. ГОНКА: get сказал NotFound, объект уже есть — AlreadyExists = переиспользовать
fresh kaname-jwks-enc-key
GET_MODE=notfound DEFAULT_PRESENT=0 seed
[ "$RC" -eq 0 ] && [ "$(rv kaname-jwks-enc-key)" = 7 ] \
  || fail "5: объект, заведённый между проверкой и заведением, перезаписан либо посев отказал: код $RC, resourceVersion 7 → $(rv kaname-jwks-enc-key). Заведение обязано быть атомарным на сервере (create, а не apply). Вывод:
$OUT"
ok

# ── копия дерева с посадкой `external` у корня цепочки prod ────────────────
MIRROR="$WORK/mirror"; mkdir -p "$MIRROR/scripts" "$MIRROR/tests/helm" "$MIRROR/helm"
cp "$DEPLOY/stacks.txt" "$MIRROR/"
cp "$DEPLOY/scripts/stack-secrets.sh" "$DEPLOY/scripts/dev-prod-secrets.sh" "$MIRROR/scripts/"
cp "$DEPLOY/tests/helm/stacks.sh" "$MIRROR/tests/helm/"
cp -r "$UMBRELLA" "$MIRROR/helm/umbrella"
python3 - "$MIRROR/helm/umbrella/values.prod.yaml" <<'PY' || fatal "6: копия боевого профиля не переведена на посадку external — судить утверждения 6 и 7 не на чем"
import re, sys
p = sys.argv[1]; s = open(p).read()
# Обе половины посадки объявлены в корне цепочки ровно по разу; иное число
# значит, что профиль сменил форму, и копия утверждала бы не то.
s2, n = re.subn(r"(?m)^(\s*identityProvider:\s*)own\s*$", r"\1external", s)
assert n == 2, f"объявлений посадки own в корне prod найдено {n}, ожидалось 2"
open(p, "w").write(s2)
PY

# ── 6. НАХОДКА: external, optional-ссылка, секрета нет — не требуется ───────
# Площадка НЕ локальная: предполёт только судит и называет недостающее.
fresh
PROBE_CONTEXT=managed-probe GET_MODE=ok DEFAULT_PRESENT=1 ABSENT="kaname-second-factor-enc-key" stack prod "$MIRROR"
[ "$RC" -eq 0 ] && [[ "$OUT" != *"kaname-second-factor-enc-key"* ]] \
  || fail "6: стек prod в копии с посадкой external: ключ второго фактора подключён optional: true и служба его не требует, а предполёт вышел $RC и назвал его. Вывод:
$OUT"
ok

# ── 7. БЛИЗНЕЦ: та же посадка, ссылка БЕЗ optional — называет ──────────────
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

# ── БЛИЗНЕЦЫ СНЯТОГО ПРЕДМЕТА: миры 1–2 обязаны его видеть ─────────────────
# Копия посевов рядом с настоящими чартами и таблицей стендов; в копии снято
# различение «сервер отказал» / «секрета нет» — ровно одним фактом, форма
# снятия та же, что у находки ревью волны-1: отказ уходит в ветку NotFound.
# Не нашлось места снятия — форма посева сменилась, и близнец утверждал бы не
# то: это находка о пробе, а не молчание.
twin_tree() {  # <имя> — печатает путь копии
  local t="$WORK/twin-$1"
  mkdir -p "$t/scripts" "$t/tests/helm"
  cp "$DEPLOY/scripts/dev-prod-secrets.sh" "$DEPLOY/scripts/stack-secrets.sh" "$t/scripts/"
  cp "$DEPLOY/tests/helm/stacks.sh" "$t/tests/helm/"
  ln -s "$DEPLOY/stacks.txt" "$t/stacks.txt"
  ln -s "$DEPLOY/helm" "$t/helm"
  printf '%s\n' "$t"
}
# unseparate_devprod <файл> <секрет | all> — отказ get секрета читается как NotFound.
unseparate_devprod() {
  python3 - "$1" "$2" "$SEED_FOUR" <<'PY2'
import re
import sys
path, which, four = sys.argv[1], sys.argv[2], sys.argv[3].split()
lines = open(path, encoding="utf-8").read().split("\n")
anchor = 'elif [[ "$out" == *"(NotFound)"* ]]; then'
get = re.compile(r"get secret ([a-z0-9][a-z0-9-]*) -o name")
last, seen = None, []
for i, line in enumerate(lines):
    m = get.search(line)
    if m:
        last = m.group(1)
    if line.strip() == anchor:
        seen.append((i, last))
if sorted(n for _, n in seen) != sorted(four):
    sys.exit(f"ветки NotFound посева называют {[n for _, n in seen]}, проба знает {four}")
for i, n in seen:
    if which in ("all", n):
        lines[i] = lines[i].replace(anchor, "elif true; then")
open(path, "w", encoding="utf-8").write("\n".join(lines))
PY2
}
# unseparate_stack <файл> — secret_state читает любой отказ как «нет».
unseparate_stack() {
  python3 - "$1" <<'PY2'
import sys
path = sys.argv[1]
s = open(path, encoding="utf-8").read()
anchor = '  case "$out" in *"(NotFound)"*) echo absent; return 0 ;; esac\n'
if s.count(anchor) != 1:
    sys.exit(f"различение в secret_state не найдено (строк {s.count(anchor)})")
open(path, "w", encoding="utf-8").write(s.replace(anchor, "  echo absent; return 0\n"))
PY2
}

# ── 9. БЛИЗНЕЦ (×4): dev-prod-secrets.sh без различения в ветке K ──────────
for k in $SEED_FOUR; do
  t="$(twin_tree "devprod-$k")"
  msg="$(unseparate_devprod "$t/scripts/dev-prod-secrets.sh" "$k" 2>&1)" \
    || fail "9: копия dev-prod-secrets.sh без различения по $k не построена — $msg"
  got="$(refusal_worlds "$t" dev-prod "$k")"
  red="$(printf '%s\n' "$got" | grep '^red|' | cut -d'|' -f2,3)"
  [ "$red" = "dev-prod-secrets.sh|сервер отказал на get $k" ] \
    || violation "9: в копии dev-prod-secrets.sh отказ get $k читается как NotFound — ждали красным мир «сервер отказал на get $k» с именем посева, получили: ${got:-пусто}"
  ok
done

# ── 10. БЛИЗНЕЦ: stack-secrets.sh, secret_state без различения ─────────────
t="$(twin_tree stack)"
msg="$(unseparate_stack "$t/scripts/stack-secrets.sh" 2>&1)" \
  || fail "10: копия stack-secrets.sh без различения не построена — $msg"
got="$(refusal_worlds "$t" stack)"
red="$(printf '%s\n' "$got" | grep '^red|' | cut -d'|' -f2)"
[ "$red" = "stack-secrets.sh" ] \
  || violation "10: в копии stack-secrets.sh secret_state читает любой отказ как «нет» — мир отказа обязан покраснеть с именем посева, получили: ${got:-пусто}"
ok

# ── 11. БЛИЗНЕЦ: оба посева без различения — находки называют оба ──────────
t="$(twin_tree both)"
msg="$(unseparate_devprod "$t/scripts/dev-prod-secrets.sh" all 2>&1 && unseparate_stack "$t/scripts/stack-secrets.sh" 2>&1)" \
  || fail "11: копия обоих посевов без различения не построена — $msg"
got="$(refusal_worlds "$t" all)"
named="$(printf '%s\n' "$got" | grep '^red|' | cut -d'|' -f2 | sort | uniq -c | awk '{print $2 "×" $1}' | paste -sd' ')"
[ "$named" = "dev-prod-secrets.sh×4 stack-secrets.sh×1" ] \
  || violation "11: оба посева без различения — ждали красными все пять миров (dev-prod-secrets.sh×4 stack-secrets.sh×1), получили: ${named:-ни одного}"
ok

outcome_verdict "миров отказа 5 (dev-prod-secrets.sh 4, stack-secrets.sh 1) + близнецов снятого различения 6; прочих миров 6"
