#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# seed-secrets-refuse-unknown-state-test.sh — посев секретов стенда не читает
# отказ API-сервера как «секрета нет» (задача #2803).
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
# 2. ТРЕБУЕМОСТЬ В ОБХОД ПОСАДКИ — снята вместе со второй посадкой службы
#    (kaname#363, kacho#2818). Прежде под посадкой `external` ключ обёртки
#    секретов второго фактора службой не требовался, и предполёт обязан был его
#    не называть; посадка у службы одна, и ключ требуется на каждом стенде —
#    это утверждение 8.
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
# 4. ТРИ СЛЕПОТЫ ТОЙ ЖЕ ПРОБЫ (задача #2844, круг 2):
#    · отказ был ОДНОГО текста (срок). Ветка, читающая `(Forbidden)` как «нет»,
#      давала PASS, хотя RBAC шапки посевов называют классом отказа сами. Теперь
#      каждый мир идёт на трёх классах — срок, сеть, RBAC, — и близнецы 12–13
#      снимают различение ровно для RBAC;
#    · «после отказа ни одного обращения» судилось по записи, в которую двойник
#      писал только get, create -f и apply -f. Любой иной глагол падал в
#      «неожиданный вызов» без записи: delete после отказа давал PASS, то есть
#      преходящий отказ API стирал ключевой материал под зелёной пробой. Теперь
#      двойник пишет КАЖДОЕ обращение к серверу до выбора ветки и исполняет
#      delete; близнецы 14–15 доказывают, что глагол после отказа виден;
#    · конструктор близнецов искал различение дословным текстом, и законное
#      переписывание (кавычки сняты вокруг $out) давало FAIL с кодом находки о
#      посеве. Теперь он узнаёт различение по форме (утверждение 16), а форма,
#      которой он не узнал, — «не выполнилось» (код 2), а не красное.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — ПАРАМИ, У КАЖДОГО ОТРИЦАНИЯ ЕСТЬ БЛИЗНЕЦ
#
#   1  находка : (×4 секрета × 3 класса отказа) секрет K существует, `get` K
#                отвечает отказом НЕ NotFound, прочие — NotFound → dev-prod-secrets.sh
#                отказывает, после `get` K не обращается ни к чему, отказ
#                называет K, resourceVersion K не сдвинулся;
#   2  находка : (×3 класса) то же для stack-secrets.sh — требуемый секрет базы
#                существует, `get` его отвечает отказом → код 2 (контракт
#                посева), после отказа ни одного обращения (ничего не
#                заведено), отказ называет секрет, объект не тронут;
#   3  близнец : секретов нет (`get` → NotFound) → посев заводит все четыре;
#   4  близнец : секрет существует и `get` его видит → переиспользуется,
#                resourceVersion не сдвинулся;
#   5  гонка   : `get` сказал NotFound, а объект уже заведён (между проверкой и
#                заведением) → создание отвечает AlreadyExists, посев
#                переиспользует, resourceVersion не сдвинулся;
#   8  находка : стек own — служба ключ второго фактора требует, секрета нет →
#                называет (утверждений 6 и 7 о посадке `external` больше нет);
#   9  близнец : (×4) копия dev-prod-secrets.sh, где отказ `get` K читается как
#                NotFound → мир 1 для K краснеет на каждом классе и называет
#                посев и секрет;
#  10  близнец : копия stack-secrets.sh, где secret_state читает любой отказ
#                как «нет» → мир 2 краснеет на каждом классе и называет посев;
#  11  близнец : оба посева без различения → находки называют ОБА посева;
#  12  близнец : dev-prod-secrets.sh читает `(Forbidden)` как NotFound →
#                красны РОВНО миры RBAC, срок и сеть зелены;
#  13  близнец : stack-secrets.sh, отдельная ветка `(Forbidden)` → «нет» →
#                красен РОВНО мир RBAC;
#  14  близнец : dev-prod-secrets.sh стирает секрет (delete) после отказа →
#                мир 1 краснеет по каждому секрету, называя delete;
#  15  близнец : то же для stack-secrets.sh в цикле требуемых;
#  16  законный: кавычки вокруг $out сняты → все 15 миров зелены, близнецы
#                строятся.
# Миры 1 и 2 и близнецы 9–16 копят находки, а не обрываются на первой:
# посев, у которого различение снято в двух местах, называется весь. Миры
# независимы и идут параллельно.
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
EXPECTED_ASSERTIONS=30

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
#   refuse-one — `get secret $REFUSE_NAME` отказывает ТЕКСТОМ КЛАССА
#                $REFUSE_CLASS (timeout — срок, network — сеть, rbac — Forbidden),
#                прочие отвечают по состоянию. Отказ ОДНОГО секрета — чтобы
#                красное не могло прийти от соседнего; класс без текста — код 99;
#   notfound   — `get secret` всегда отвечает NotFound (даже на существующий):
#                так выглядит гонка «проверили — и тут его завели».
# $DEFAULT_PRESENT=1 — всякий секрет, кроме перечисленных в $ABSENT, считается
# существующим без файла состояния (для суждения о требуемом множестве).
# ЗАПИСЬ — КАЖДОЕ обращение к API-серверу, строкой «глагол вид имя» в
# $STATE/.calls, ДО выбора ветки: «после отказа ни одного обращения» судится по
# последней строке, и глагол, которого двойник не исполняет (он отвечает кодом
# 98), обязан стоять в ней так же, как get (задача #2844, круг 2: delete после
# отказа давал PASS). Не пишутся только клиентские вызовы — config и сборка
# манифеста --dry-run=client: к серверу они не ходят. `create -f` и `apply -f`
# пишутся в своей ветке — имя у них из манифеста.
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
  "config "*|"create -f"|"apply -f") ;;
  *) case " $* " in *" --dry-run=client "*) ;; *) echo "$1 ${2:-} ${3:-}" >> "$STATE/.calls" ;; esac ;;
esac
refusal_text() {  # <имя> — текст, которым API-сервер отказывает классом $REFUSE_CLASS
  case "${REFUSE_CLASS:-}" in
    timeout) echo "Unable to connect to the server: net/http: TLS handshake timeout" ;;
    network) echo "The connection to the server 127.0.0.1:65530 was refused - did you specify the right host or port?" ;;
    rbac) echo "Error from server (Forbidden): secrets \"$1\" is forbidden: User \"system:serviceaccount:kacho:probe\" cannot get resource \"secrets\" in API group \"\" in the namespace \"kacho\"" ;;
    *) return 1 ;;
  esac
}
case "$1 ${2:-}" in
  "config current-context") echo "$PROBE_CONTEXT"; exit 0 ;;
  "config view") echo "https://127.0.0.1:65530"; exit 0 ;;
  "get namespace") exit 0 ;;
  "get secret")
    name="$3"
    case "${GET_MODE:-ok}" in
      refuse-one)
        if [ "$name" = "${REFUSE_NAME:-}" ]; then
          refusal_text "$name" >&2 || { echo "двойник: класс отказа не задан или неизвестен: ${REFUSE_CLASS:-пусто}" >&2; exit 99; }
          exit 1
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
  "delete secret")
    rm -f -- "${STATE:?}/${3:?}.rv"; echo "secret \"$3\" deleted"; exit 0 ;;
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
  STATE="$(mktemp -d "$WORK/state.XXXXXX")"; : > "$STATE/.calls"
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
# Классы отказа — те, что шапки посевов называют сами: срок, сеть, RBAC. Мир идёт
# на КАЖДОМ классе: различение, которое верно лишь для одного текста отказа (ветка,
# читающая `(Forbidden)` как «нет»), на остальных двух зеленело бы.
REFUSAL_CLASSES="timeout network rbac"
# refusal_worlds <каталог deploy> <dev-prod|stack|all> [секрет] [классы] — по
# строке на мир (секрет сужает миры dev-prod-secrets.sh до одного, классы — до
# перечисленных):
#   ok|<посев>|<класс>|<секрет>              — шаг остановился на отказе, как обещает посев;
#   red|<посев>|<класс>|<секрет>|<что не так>.
# Мир судит ВСЁ, что делает отказ отказом шага, а не одно «вышел не нулём»:
# без различения посев тоже выходит не нулём — на соседнем секрете.
world_devprod() {  # <каталог deploy> <класс> <секрет>
  local root="$1" c="$2" k="$3" why=""
  fresh "$k"
  GET_MODE=refuse-one REFUSE_NAME="$k" REFUSE_CLASS="$c" DEFAULT_PRESENT=0 seed "$root"
  [ "$RC" -ne 0 ] || why="$why; вышел 0"
  [ "$(last_call)" = "get secret $k" ] || why="$why; после отказа по $k шаг продолжился (последнее обращение: $(last_call))"
  [[ "$OUT" == *"секрет $k"*"НЕ УСТАНОВЛЕНО"* ]] || why="$why; отказ не назвал $k как секрет, чьё присутствие не установлено"
  [ "$(rv "$k")" = 7 ] || why="$why; resourceVersion $k 7 → $(rv "$k")"
  if [ -n "$why" ]; then echo "red|dev-prod-secrets.sh|$c|$k|${why#; } (код $RC)"
  else echo "ok|dev-prod-secrets.sh|$c|$k"; fi
}
world_stack() {  # <каталог deploy> <класс>
  local root="$1" c="$2" why=""
  fresh "$STACK_REFUSED"
  GET_MODE=refuse-one REFUSE_NAME="$STACK_REFUSED" REFUSE_CLASS="$c" DEFAULT_PRESENT=0 stack own "$root"
  [ "$RC" -eq 2 ] || why="$why; код $RC, а контракт посева для отказа сервера — 2"
  [ "$(last_call)" = "get secret $STACK_REFUSED" ] || why="$why; после отказа по $STACK_REFUSED шаг продолжился (последнее обращение: $(last_call); заведений $(grep -cE '^(create|apply) ' "$STATE/.calls"))"
  [[ "$OUT" == *"секрет $STACK_REFUSED"*"НЕ УСТАНОВЛЕНО"* ]] || why="$why; отказ не назвал $STACK_REFUSED как секрет, чьё присутствие не установлено"
  [ "$(rv "$STACK_REFUSED")" = 7 ] || why="$why; resourceVersion $STACK_REFUSED 7 → $(rv "$STACK_REFUSED")"
  if [ -n "$why" ]; then echo "red|stack-secrets.sh|$c|$STACK_REFUSED|${why#; }"
  else echo "ok|stack-secrets.sh|$c|$STACK_REFUSED"; fi
}
# Миры независимы (у каждого своё состояние), поэтому идут ПАРАЛЛЕЛЬНО; вывод
# собирается в порядке запуска, и мир, не напечатавший строки, — не «зелёный», а
# недостача в счёте строк у вызывающего.
refusal_worlds() {
  local root="$1" which="$2" only="${3:-}" classes="${4:-$REFUSAL_CLASSES}" k c i=0 j d
  d="$(mktemp -d "$WORK/worlds.XXXXXX")"
  for c in $classes; do
    if [ "$which" != stack ]; then
      for k in ${only:-$SEED_FOUR}; do i=$((i + 1)); world_devprod "$root" "$c" "$k" > "$d/$i" & done
    fi
    if [ "$which" != dev-prod ]; then i=$((i + 1)); world_stack "$root" "$c" > "$d/$i" & fi
  done
  wait
  for ((j = 1; j <= i; j++)); do cat "$d/$j"; done
}
# reds <вывод миров> — красные миры «посев|класс|секрет», по строке, отсортированы.
reds() { printf '%s\n' "$1" | grep '^red|' | cut -d'|' -f2-4 | sort; }
# expect_reds <посев> <классы> <секреты> — какими ОБЯЗАНЫ быть красные миры.
expect_reds() {
  local c k; for c in $2; do for k in $3; do echo "$1|$c|$k"; done; done | sort
}

# ── 1–2. НАХОДКА: отказ get — отказ шага, на ТОМ ЖЕ секрете, объект цел ────
WORLDS="$(refusal_worlds "$DEPLOY" all)"
[ "$(printf '%s\n' "$WORLDS" | grep -c '^\(ok\|red\)|')" -eq 15 ] \
  || fail "1–2: миров отказа исполнено не 15 (по три класса: четыре секрета у dev-prod-secrets.sh, один у stack-secrets.sh). Вывод:
$WORLDS"
while IFS='|' read -r verdict seedname class secret what; do
  [ "$verdict" = red ] && violation "1–2: посев $seedname · сервер отказал ($class) на get $secret: $what — отказ API-сервера прочитан как «секрета нет»"
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

# ── 8. НАХОДКА: стек own — служба ключ требует — называет ─────────────────
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
# КОНСТРУКТОР БЛИЗНЕЦОВ узнаёт различение по ФОРМЕ, а не по написанию: ветка
# NotFound посева — строка `elif … (NotFound) …; then` (кавычки и пробелы любые),
# в secret_state — единственная строка тела с `(NotFound)`. Не узнал — это не
# находка о посеве: близнец не построен, различение миров не доказано, и исход
# такой пробы — «не выполнилось» (код 2), а не красное (задача #2844, круг 2:
# снятые кавычки вокруг $out давали FAIL с кодом находки).
cat > "$WORK/twin.py" <<'PY2'
import re
import sys

NF_BRANCH = re.compile(r"^(\s*)elif\b.*\(NotFound\).*;\s*then\s*$")
GET = re.compile(r"get secret ([a-z0-9][a-z0-9-]*)\b")


def unrecognized(why):
    sys.stderr.write(why + "\n")
    sys.exit(3)


def devprod_branches(lines, four):
    """(номер строки, секрет) каждой ветки NotFound; секрет — последний get выше."""
    last, seen = None, []
    for i, line in enumerate(lines):
        m = GET.search(line)
        if m:
            last = m.group(1)
        if NF_BRANCH.match(line):
            seen.append((i, last))
    if sorted(n for _, n in seen) != sorted(four):
        unrecognized(f"ветки NotFound посева называют {[n for _, n in seen]}, проба знает {four}")
    return seen


def stack_notfound(lines):
    """Номер единственной строки тела secret_state, различающей NotFound."""
    start = next((i for i, l in enumerate(lines) if re.match(r"^secret_state\(\)\s*\{", l)), None)
    if start is None:
        unrecognized("функция secret_state не найдена")
    end = next((i for i in range(start + 1, len(lines)) if lines[i].startswith("}")), len(lines))
    hits = [i for i in range(start, end) if "(NotFound)" in lines[i] and "absent" in lines[i]]
    if len(hits) != 1:
        unrecognized(f"различение в secret_state не найдено (строк {len(hits)})")
    return hits[0]


verb, path = sys.argv[1], sys.argv[2]
lines = open(path, encoding="utf-8").read().split("\n")
if verb == "unseparate-devprod":       # <секрет | all>: отказ get читается как NotFound
    for i, n in devprod_branches(lines, sys.argv[4].split()):
        if sys.argv[3] in ("all", n):
            lines[i] = NF_BRANCH.match(lines[i]).group(1) + "elif true; then"
elif verb == "respell-devprod":         # то же поведение: кавычки вокруг $out сняты
    for i, _ in devprod_branches(lines, sys.argv[3].split()):
        lines[i] = lines[i].replace('"$out"', "$out")
elif verb == "respell-stack":
    i = stack_notfound(lines)
    lines[i] = lines[i].replace('"$out"', "$out")
elif verb == "unseparate-stack":       # secret_state читает любой отказ как «нет»
    i = stack_notfound(lines)
    lines[i] = "  echo absent; return 0"
elif verb == "forbidden-devprod":      # ветка NotFound принимает и Forbidden
    for i, _ in devprod_branches(lines, sys.argv[3].split()):
        lines[i] = re.sub(r";\s*then\s*$", ' || [[ "$out" == *"(Forbidden)"* ]]; then', lines[i])
elif verb == "forbidden-stack":        # отдельная ветка case: Forbidden → «нет»
    i = stack_notfound(lines)
    lines.insert(i + 1, '  case "$out" in *"(Forbidden)"*) echo absent; return 0 ;; esac')
elif verb == "delete-devprod":         # отказ шага сперва стирает секрет
    start = next((i for i, l in enumerate(lines) if re.match(r"^refuse_unknown\(\)\s*\{", l)), None)
    end = None if start is None else next((i for i in range(start, len(lines)) if re.match(r"^\s*exit\s+1\s*$", lines[i])), None)
    if end is None:
        unrecognized("выход refuse_unknown не найден")
    lines.insert(end, '  kubectl -n "$NS" delete secret "$1" >/dev/null 2>&1 || true')
elif verb == "delete-stack":           # отказ secret_state в цикле требуемых сперва стирает секрет
    hit = next((i for i, l in enumerate(lines) if re.search(r'secret_state "\$s"\)"?\s*\|\|\s*die\b', l)), None)
    if hit is None:
        unrecognized("отказ secret_state в цикле требуемых не найден")
    lines[hit] = re.sub(r"\|\|\s*die\b", '|| { kubectl -n "$NS" delete secret "$s" >/dev/null 2>&1; false; } || die', lines[hit], count=1)
else:
    sys.exit(f"конструктор близнецов: неизвестный глагол {verb}")
open(path, "w", encoding="utf-8").write("\n".join(lines))
PY2
unseparate_devprod()           { python3 "$WORK/twin.py" unseparate-devprod "$1" "$2" "$SEED_FOUR"; }
unseparate_stack()             { python3 "$WORK/twin.py" unseparate-stack "$1"; }
respell_devprod()              { python3 "$WORK/twin.py" respell-devprod "$1" "$SEED_FOUR"; }
respell_stack()                { python3 "$WORK/twin.py" respell-stack "$1"; }
forbidden_as_absent_devprod()  { python3 "$WORK/twin.py" forbidden-devprod "$1" "$SEED_FOUR"; }
forbidden_as_absent_stack()    { python3 "$WORK/twin.py" forbidden-stack "$1"; }
delete_after_refusal_devprod() { python3 "$WORK/twin.py" delete-devprod "$1"; }
delete_after_refusal_stack()   { python3 "$WORK/twin.py" delete-stack "$1"; }
# unbuilt <текст> — близнец не построен: утверждение не исполнено, находкой не
# считается; итог решается после всех утверждений (см. конец файла).
UNBUILT=0
unbuilt() { echo "  ⊘ $1" >&2; UNBUILT=$((UNBUILT + 1)); }

# ── 9. БЛИЗНЕЦ (×4): dev-prod-secrets.sh без различения в ветке K ──────────
for k in $SEED_FOUR; do
  t="$(twin_tree "devprod-$k")"
  if ! msg="$(unseparate_devprod "$t/scripts/dev-prod-secrets.sh" "$k" 2>&1)"; then
    unbuilt "9: копия dev-prod-secrets.sh без различения по $k не построена — $msg"; continue
  fi
  got="$(refusal_worlds "$t" dev-prod "$k")"
  [ "$(reds "$got")" = "$(expect_reds dev-prod-secrets.sh "$REFUSAL_CLASSES" "$k")" ] \
    || violation "9: в копии dev-prod-secrets.sh отказ get $k читается как NotFound — ждали красными миры отказа по $k на каждом классе с именем посева, получили: ${got:-пусто}"
  ok
done

# ── 10. БЛИЗНЕЦ: stack-secrets.sh, secret_state без различения ─────────────
t="$(twin_tree stack)"
if ! msg="$(unseparate_stack "$t/scripts/stack-secrets.sh" 2>&1)"; then
  unbuilt "10: копия stack-secrets.sh без различения не построена — $msg"
else
  got="$(refusal_worlds "$t" stack)"
  [ "$(reds "$got")" = "$(expect_reds stack-secrets.sh "$REFUSAL_CLASSES" "$STACK_REFUSED")" ] \
    || violation "10: в копии stack-secrets.sh secret_state читает любой отказ как «нет» — мир отказа обязан покраснеть на каждом классе с именем посева, получили: ${got:-пусто}"
  ok
fi

# ── 11. БЛИЗНЕЦ: оба посева без различения — находки называют оба ──────────
t="$(twin_tree both)"
if ! msg="$(unseparate_devprod "$t/scripts/dev-prod-secrets.sh" all 2>&1 && unseparate_stack "$t/scripts/stack-secrets.sh" 2>&1)"; then
  unbuilt "11: копия обоих посевов без различения не построена — $msg"
else
  got="$(refusal_worlds "$t" all)"
  named="$(printf '%s\n' "$got" | grep '^red|' | cut -d'|' -f2 | sort | uniq -c | awk '{print $2 "×" $1}' | paste -sd' ')"
  [ "$named" = "dev-prod-secrets.sh×12 stack-secrets.sh×3" ] \
    || violation "11: оба посева без различения — ждали красными все пятнадцать миров (dev-prod-secrets.sh×12 stack-secrets.sh×3), получили: ${named:-ни одного}"
  ok
fi

# ── 12. БЛИЗНЕЦ: dev-prod-secrets.sh читает `(Forbidden)` как NotFound ──────
# Снят ОДИН класс отказа: ветка NotFound принимает и Forbidden. Красными обязаны
# стать ровно миры RBAC — по одному на секрет; срок и сеть остаются зелены.
t="$(twin_tree devprod-forbidden)"
if ! msg="$(forbidden_as_absent_devprod "$t/scripts/dev-prod-secrets.sh" 2>&1)"; then
  unbuilt "12: копия dev-prod-secrets.sh с Forbidden как NotFound не построена — $msg"
else
  got="$(refusal_worlds "$t" dev-prod)"
  [ "$(reds "$got")" = "$(expect_reds dev-prod-secrets.sh rbac "$SEED_FOUR")" ] \
    || violation "12: в копии dev-prod-secrets.sh отказ RBAC читается как NotFound — ждали красными ровно миры RBAC по каждому секрету, получили: ${got:-пусто}"
  ok
fi

# ── 13. БЛИЗНЕЦ: stack-secrets.sh, отдельная ветка `(Forbidden)` → «нет» ────
t="$(twin_tree stack-forbidden)"
if ! msg="$(forbidden_as_absent_stack "$t/scripts/stack-secrets.sh" 2>&1)"; then
  unbuilt "13: копия stack-secrets.sh с Forbidden как «нет» не построена — $msg"
else
  got="$(refusal_worlds "$t" stack)"
  [ "$(reds "$got")" = "$(expect_reds stack-secrets.sh rbac "$STACK_REFUSED")" ] \
    || violation "13: в копии stack-secrets.sh secret_state читает отказ RBAC как «нет» — ждали красным ровно мир RBAC, получили: ${got:-пусто}"
  ok
fi

# ── 14–15. БЛИЗНЕЦЫ: после отказа посев обращается к серверу иным глаголом ──
# Глагол, которого двойник не исполняет (delete), обязан быть виден так же, как
# get: «после отказа ни одного обращения» судится по ЗАПИСИ ВСЕХ обращений.
# Класс отказа здесь один — предмет не класс, а запись.
t="$(twin_tree devprod-delete)"
if ! msg="$(delete_after_refusal_devprod "$t/scripts/dev-prod-secrets.sh" 2>&1)"; then
  unbuilt "14: копия dev-prod-secrets.sh с delete после отказа не построена — $msg"
else
  got="$(refusal_worlds "$t" dev-prod "" timeout)"
  [ "$(reds "$got")" = "$(expect_reds dev-prod-secrets.sh timeout "$SEED_FOUR")" ] \
    && [ "$(printf '%s\n' "$got" | grep -c '^red|.*последнее обращение: delete secret ')" -eq 4 ] \
    || violation "14: в копии dev-prod-secrets.sh отказ шага стирает секрет (delete) — ждали красными миры по каждому секрету с обращением delete после отказа, получили: ${got:-пусто}"
  ok
fi
t="$(twin_tree stack-delete)"
if ! msg="$(delete_after_refusal_stack "$t/scripts/stack-secrets.sh" 2>&1)"; then
  unbuilt "15: копия stack-secrets.sh с delete после отказа не построена — $msg"
else
  got="$(refusal_worlds "$t" stack "" timeout)"
  [ "$(reds "$got")" = "$(expect_reds stack-secrets.sh timeout "$STACK_REFUSED")" ] \
    && [[ $'\n'"$got" == *$'\n'"red|"*"последнее обращение: delete secret "* ]] \
    || violation "15: в копии stack-secrets.sh отказ шага стирает секрет (delete) — ждали красным мир с обращением delete после отказа, получили: ${got:-пусто}"
  ok
fi

# ── 16. ЗАКОННОЕ ПЕРЕПИСЫВАНИЕ: кавычки вокруг $out сняты ────────────────────
# Поведение то же, текст другой. Миры обязаны остаться зелены, а конструктор
# близнецов — узнать различение: красное здесь было бы краснотой ЗАКОННОГО
# близнеца с кодом находки о посеве.
t="$(twin_tree respelled)"
if ! msg="$(respell_devprod "$t/scripts/dev-prod-secrets.sh" 2>&1 && respell_stack "$t/scripts/stack-secrets.sh" 2>&1)"; then
  unbuilt "16: копия посевов с кавычками, снятыми вокруг \$out, не построена — $msg"
else
  got="$(refusal_worlds "$t" all)"
  built="$(cp "$t/scripts/dev-prod-secrets.sh" "$WORK/respelled-dp.sh" && cp "$t/scripts/stack-secrets.sh" "$WORK/respelled-st.sh" \
           && unseparate_devprod "$WORK/respelled-dp.sh" all 2>&1 && unseparate_stack "$WORK/respelled-st.sh" 2>&1 && echo built)"
  [ -z "$(reds "$got")" ] && [ "$(printf '%s\n' "$got" | grep -c '^ok|')" -eq 15 ] && [ "${built##*$'\n'}" = built ] \
    || violation "16: посевы с кавычками, снятыми вокруг \$out, — ждали 15 зелёных миров и построенных близнецов, получили: миры ${got:-пусто}; конструктор: ${built:-пусто}"
  ok
fi

# Близнец, которого конструктор не построил, — утверждение НЕ ИСПОЛНЕНО. При
# находках о посеве вердикт — они (код 1); без них — «не выполнилось» (код 2):
# форма посева пробе не узнана, и о различении миров сказать нечего.
if [ "$UNBUILT" -gt 0 ]; then
  [ "$VIOLATIONS" -gt 0 ] && fail "$SCRIPT — находок $VIOLATIONS (перечень выше); кроме того близнецов не построено $UNBUILT"
  fatal "$SCRIPT — близнецов не построено $UNBUILT (перечень выше): форма посева конструктору близнецов не узнана, и что миры видят снятие различения, НЕ ДОКАЗАНО. Это отказ инструмента пробы, а не находка о посеве"
fi
outcome_verdict "миров отказа 15 (по классам срок, сеть, RBAC: dev-prod-secrets.sh 12, stack-secrets.sh 3) + близнецов 11 (снятое различение 6, класс RBAC 2, глагол после отказа 2, законное переписывание 1); прочих миров 6"
