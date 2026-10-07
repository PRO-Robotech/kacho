#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ntf2-start-conditions.sh — проба условий начала NTF-2 над базой `<B>`
# (приёмка NTF-2 Р16; замысел issue-2917 З29; полоса D0 маршрута issue-2917).
#
# Использование:  bash deploy/tests/helm/ntf2-start-conditions.sh [<ревизия>]
# (по умолчанию — HEAD дерева, в котором лежит проба). Вывод прикладывается к
# задаче NTF-2 на базе старта и на базе запроса волны (Р16).
#
# ЭТО НЕ ГЕЙТ. Имя не оканчивается на `-test.sh` намеренно: условия начала на
# базе бывают не выполнены законно (NTF-1 ещё не влита), и цель
# `helm-manifest-test` краснела бы на верном дереве. Способность пробы различать
# исходы держит `ntf2-start-conditions-inject.sh`.
#
# ИСХОДОВ У КАЖДОГО ПУНКТА ТРИ: «выполнено», «не выполнено» и «не выполнилось»
# (команда не дала ответа: отказ git, helm, обёртки, контроль не сошёлся). Третий
# не зачитывается ни в один из первых двух и печатается с причиной. Код выхода —
# по контракту каталога (`outcome.sh`): 0 — все пункты «выполнено»; 1 — есть
# «не выполнено» и нет «не выполнилось»; 2 — есть «не выполнилось».
#
# ЧТО МЕРЯЕТСЯ И ОТКУДА.
#   ревизия — `<B>`; рендер (п.2) и чтение таблицы стендов идут с ДИСКА, поэтому
#     печатается, совпадает ли диск с `<B>`; не совпадает — пп.2, 5 «не
#     выполнилось» (мерили бы не `<B>`);
#   п.1 — `git ls-tree -d <B> services/notify`; каталоги `notify/feed`,
#     `notify/spec` в corelib-пине kacho и в corelib-пине kaname (kaname — по пину
#     kacho в `<B>`); типы `notification_namespace`, `notification_feed` в модели
#     kaname (`proto/kaname/cloud/iam/v1/fga_model.fga` того же пина);
#   п.2 — рендер КАЖДОЙ цепочки `deploy/stacks.txt` (команды приёмки §1.6):
#     посадка края, ручка полосы формы, рабочие объекты поставщика; `prod` —
#     только шелл-обёрткой NTF1-D9 с образцом П7-узла `ntf2-p7.yaml` (Д48, CX2-58,
#     CX2-66, CX2-67), строка цепочки печатается с образцом последним;
#   п.3 — пути полосы формы края (§1.4);
#   п.4 — подчарты поставщика в зонтике (условие только полосы стража почты):
#     зависимости поставщика и записи `charts/` с его именем; ноль — исход, когда
#     обход прочитал отметки, зависимости и записи `charts/` (после kacho#1276
#     поставщика в зонтике нет, и ноль — ожидаемый ответ, а не «не прочитано»);
#   п.5 — узел почты цепочки `dev`: команда Р16 п.5 над `values.yaml` и файлами
#     `stacks.sh --chain dev` (CX2-64), контроль — один `values.yaml`;
#   п.6 и признак K — читают чарт `notify` и перечень источников NTF-1 (Е13
#     замысла: NTF1-D1, NTF1-D2, NTF1-D1s). Предпосылка меряется и печатается;
#     исполнения у них в этой пробе нет, и исход у них — «не выполнилось».
#
# ФАЙЛОВ ЦЕПОЧКИ ПРОБА НЕ ВЫПИСЫВАЕТ: цепочку отдают общий читатель `stacks.sh`
# и обёртка `render_chain_args` — вторую копию цепочки `stack_table_test.go`
# назвал бы находкой. Строка обёртки берётся ПРИСВАИВАНИЕМ с проверкой кода:
# подстановка аргументом `helm` код отказа теряет (держит
# `internal/repohygiene/rosterreaderexitcode_test.go`).
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)" || { echo "FATAL: каталог пробы не разрешается" >&2; exit 2; }
DEPLOY="$(cd "$HERE/../.." && pwd)" || { echo "FATAL: каталог deploy не разрешается" >&2; exit 2; }
UMBRELLA="$DEPLOY/helm/umbrella"
SAMPLE="ntf2-p7.yaml"
RELEASE="r"
CORELIB_MOD="github.com/PRO-Robotech/corelib"
KANAME_MOD="github.com/PRO-Robotech/kaname"
LANE_KNOB="KACHO_API_GATEWAY_IAM_LOGIN_LANE_URL"
POSTURE_KNOB="KACHO_API_GATEWAY_IDENTITY_PROVIDER"
# Признак репозитория поставщика чужой службы личности — тот же, по которому
# состав чужого выводит гейт `TestOwnPostureRaisesNoForeignIdentityService`
# (`foreignIdentityRepoMark`, deploy/own_posture_foreign_identity_test.go).
# Имён подчартов поставщика проба не выписывает: они выводятся из зонтика на <B>
# (`provider_census`), как у гейта, по отметкам единственного дома имени
# поставщика `internal/identityvendor` той же ревизии (тот же словарь у потолка
# привязок и у рендерной пробы `TestNoStackRendersAnIdentityVendorObject`), —
# выписанный перечень расходился бы со словарём молча, а литерал имени в
# развёртывании — единица потолка привязок к снятому поставщику. Слово
# поставщика узнаётся, как у рендерной пробы (`vendorWordIn`): отметка —
# подстрокой без учёта регистра, бренд — с границей буквы с обеих сторон.
VENDOR_HOME="internal/identityvendor"

for f in "$HERE/stacks.sh" "$HERE/lib/render-chain.sh"; do
  [ -r "$f" ] || { echo "FATAL: $f не читается — общий читатель таблицы либо обёртка рендера отсутствуют" >&2; exit 2; }
done
# shellcheck source=deploy/tests/helm/stacks.sh
. "$HERE/stacks.sh"
# shellcheck source=deploy/tests/helm/lib/render-chain.sh
. "$HERE/lib/render-chain.sh"

WORK="$(mktemp -d)" || { echo "FATAL: временный каталог не создан" >&2; exit 2; }
trap 'rm -rf "$WORK"' EXIT

HELD=0; NOT_HELD=0; NOT_RUN=0
# outcome <пункт> <исход> <подробность>
outcome() {
  case "$2" in
    выполнено) HELD=$((HELD + 1)) ;;
    "не выполнено") NOT_HELD=$((NOT_HELD + 1)) ;;
    *) NOT_RUN=$((NOT_RUN + 1)) ;;
  esac
  printf '%s: %s — %s\n' "$1" "$2" "$3"
}
nr() { outcome "$1" "не выполнилось" "$2"; }

# ── ревизия ──────────────────────────────────────────────────────────────────
command -v git >/dev/null 2>&1 || { echo "FATAL: git не найден — ревизию базы взять неоткуда" >&2; exit 2; }
REPO="$(git -C "$DEPLOY" rev-parse --show-toplevel 2>/dev/null)" \
  || { echo "FATAL: $DEPLOY не под git — ревизию базы взять неоткуда" >&2; exit 2; }
B="$(git -C "$REPO" rev-parse --verify "${1:-HEAD}^{commit}" 2>/dev/null)" \
  || { echo "FATAL: ревизия «${1:-HEAD}» не разрешается" >&2; exit 2; }
DISK_OK=0
# Несовпадение — и правленый отслеживаемый файл, и НЕОТСЛЕЖИВАЕМЫЙ неигнорируемый:
# новый образец или профиль, ещё не закоммиченный, рендер прочитал бы с диска.
if changed="$(git -C "$REPO" diff --name-only "$B" -- 2>"$WORK/err")" \
   && untracked="$(git -C "$REPO" ls-files --others --exclude-standard 2>>"$WORK/err")"; then
  changed="$(printf '%s\n%s\n' "$changed" "$untracked" | grep . || true)"
  if [ -z "$changed" ]; then
    DISK_OK=1
    echo "ревизия: $B (диск совпадает с ревизией)"
  else
    echo "ревизия: $B (диск отличается от ревизии: $(printf '%s\n' "$changed" | grep -c .) файлов, первые: $(printf '%s\n' "$changed" | head -n 5 | tr '\n' ' '))"
  fi
else
  echo "ревизия: $B (сверка диска с ревизией отказала: $(cat "$WORK/err"))"
fi
DISK_REASON="диск не совпадает с ревизией $B — рендер и таблица стендов с диска мерили бы не <B>"

# ── п.1 NTF-1 влита ──────────────────────────────────────────────────────────
# mod_version <go.mod текстом> <модуль> — версия из require (строкой либо блоком).
mod_version() {
  printf '%s\n' "$1" | awk -v m="$2" '
    $1 == "require" && $2 == m { print $3; exit }
    $1 == m && $2 ~ /^v/        { print $2; exit }'
}
# mod_replaced <go.mod текстом> <модуль> — 0, если модуль подменён `replace`.
mod_replaced() {
  printf '%s\n' "$1" | awk -v m="$2" '
    $1 == "replace" && $2 == "(" { inblk = 1; next }
    inblk && $1 == ")"           { inblk = 0; next }
    ($1 == "replace" && $2 == m) || (inblk && $1 == m) { hit = 1 }
    END { exit !hit }'
}
# mod_dir <модуль> <версия> — каталог модуля в кэше (скачивается при нужде).
mod_dir() {
  local js dir
  js="$(cd "$WORK" && GOFLAGS=-mod=mod GO111MODULE=on go mod download -json "$1@$2" 2>&1)" \
    || { echo "go mod download $1@$2 отказал: $(printf '%s' "$js" | tr '\n' ' ' | cut -c1-300)" >&2; return 2; }
  dir="$(printf '%s\n' "$js" | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",\{0,1\}$/\1/p' | head -n 1)"
  [ -n "$dir" ] && [ -d "$dir" ] || { echo "go mod download $1@$2 не назвал каталог модуля" >&2; return 2; }
  printf '%s\n' "$dir"
}
has_dir() { if [ -d "$1" ]; then echo есть; else echo нет; fi; }

cond1() {
  local out n ctl gomod cv cdir kv kdir kgomod kcv kcdir fga ns feed
  out="$(git -C "$REPO" ls-tree -d "$B" services/notify 2>"$WORK/err")" || { nr п.1 "git ls-tree services/notify отказал: $(cat "$WORK/err")"; return; }
  n="$(printf '%s' "$out" | grep -c . || true)"
  out="$(git -C "$REPO" ls-tree -d "$B" services/vpc 2>"$WORK/err")" || { nr п.1 "git ls-tree services/vpc отказал: $(cat "$WORK/err")"; return; }
  ctl="$(printf '%s' "$out" | grep -c . || true)"
  [ "$ctl" -ge 1 ] || { nr п.1 "контроль: git ls-tree -d $B services/vpc пуст — команда не видит заведомо существующего каталога"; return; }
  command -v go >/dev/null 2>&1 || { nr п.1 "go не найден — пины corelib и kaname разрешать нечем"; return; }
  gomod="$(git -C "$REPO" show "$B:go.mod" 2>"$WORK/err")" || { nr п.1 "go.mod на $B не читается: $(cat "$WORK/err")"; return; }
  for m in "$CORELIB_MOD" "$KANAME_MOD"; do
    if mod_replaced "$gomod" "$m"; then nr п.1 "$m в go.mod $B подменён replace — пин не версия, и каталог модуля этой пробой не разрешается"; return; fi
  done
  cv="$(mod_version "$gomod" "$CORELIB_MOD")"; [ -n "$cv" ] || { nr п.1 "пина $CORELIB_MOD в go.mod $B нет"; return; }
  kv="$(mod_version "$gomod" "$KANAME_MOD")"; [ -n "$kv" ] || { nr п.1 "пина $KANAME_MOD в go.mod $B нет"; return; }
  cdir="$(mod_dir "$CORELIB_MOD" "$cv" 2>"$WORK/err")" || { nr п.1 "$(cat "$WORK/err")"; return; }
  kdir="$(mod_dir "$KANAME_MOD" "$kv" 2>"$WORK/err")" || { nr п.1 "$(cat "$WORK/err")"; return; }
  [ -r "$kdir/go.mod" ] || { nr п.1 "go.mod kaname $kv не читается"; return; }
  kgomod="$(cat "$kdir/go.mod")"
  if mod_replaced "$kgomod" "$CORELIB_MOD"; then nr п.1 "$CORELIB_MOD в go.mod kaname $kv подменён replace"; return; fi
  kcv="$(mod_version "$kgomod" "$CORELIB_MOD")"; [ -n "$kcv" ] || { nr п.1 "пина $CORELIB_MOD в go.mod kaname $kv нет"; return; }
  kcdir="$(mod_dir "$CORELIB_MOD" "$kcv" 2>"$WORK/err")" || { nr п.1 "$(cat "$WORK/err")"; return; }
  for d in "$cdir" "$kcdir"; do
    [ -d "$d/subscription" ] || { nr п.1 "контроль: в $d нет заведомо существующего пакета subscription — разрешён не тот каталог"; return; }
  done
  fga="$kdir/proto/kaname/cloud/iam/v1/fga_model.fga"
  [ -r "$fga" ] || { nr п.1 "модель kaname $kv не читается: $fga"; return; }
  grep -qx 'type project' "$fga" || { nr п.1 "контроль: в модели kaname $kv нет заведомо существующего типа project — разбор модели не видит типов"; return; }
  ns="$(grep -cx 'type notification_namespace' "$fga" || true)"
  feed="$(grep -cx 'type notification_feed' "$fga" || true)"
  local detail
  detail="services/notify: записей $n (контроль services/vpc: $ctl); corelib-пин kacho $cv: notify/feed $(has_dir "$cdir/notify/feed"), notify/spec $(has_dir "$cdir/notify/spec"); kaname-пин kacho $kv → corelib-пин kaname $kcv: notify/feed $(has_dir "$kcdir/notify/feed"), notify/spec $(has_dir "$kcdir/notify/spec"); модель kaname $kv: notification_namespace $ns, notification_feed $feed (контроль type project: есть)"
  if [ "$n" -ge 1 ] && [ -d "$cdir/notify/feed" ] && [ -d "$cdir/notify/spec" ] \
     && [ -d "$kcdir/notify/feed" ] && [ -d "$kcdir/notify/spec" ] && [ "$ns" -ge 1 ] && [ "$feed" -ge 1 ]; then
    outcome п.1 выполнено "$detail"
  else
    outcome п.1 "не выполнено" "$detail"
  fi
}

# ── п.2 посадка own во всех цепочках ─────────────────────────────────────────
# vendor_dict <объявление> — строки объявления `internal/identityvendor` на <B>
# (переменная `marks` либо константа `brand`), по строке, в нижнем регистре.
# Объявление читает разбор `go doc` над файлом той ревизии в отдельном модуле,
# а не поиск по тексту исходника.
vendor_dict() {
  local d="$WORK/identityvendor" out
  if [ ! -f "$d/go.mod" ]; then
    mkdir -p "$d" || { echo "каталог разбора словаря не создан" >&2; return 2; }
    git -C "$REPO" show "$B:$VENDOR_HOME/identityvendor.go" >"$d/identityvendor.go" 2>"$WORK/err" \
      || { echo "словарь поставщика $VENDOR_HOME на $B не читается: $(cat "$WORK/err")" >&2; return 2; }
    printf 'module ntf2probe/identityvendor\n\ngo 1.22\n' >"$d/go.mod" || { echo "go.mod разбора словаря не записан" >&2; return 2; }
  fi
  out="$(cd "$d" && GOWORK=off GOFLAGS='' go doc -u . "$1" 2>&1)" \
    || { echo "go doc $1 словаря поставщика на $B отказал: $(printf '%s' "$out" | tr '\n' ' ' | cut -c1-300)" >&2; return 2; }
  printf '%s\n' "$out" | awk -v sym="$1" '
    $1 ~ /^(var|const)$/ && $2 == sym && $3 == "=" { d = 1 }
    d { while (match($0, /"[^"]*"/)) { print tolower(substr($0, RSTART + 1, RLENGTH - 2)); $0 = substr($0, RSTART + RLENGTH) } }
    d && ($0 ~ /}/ || $1 == "const") { exit }' | grep .
}
# vendor_word <строка> — 0, если строка называет поставщика: отметка подстрокой
# без учёта регистра либо бренд с границей буквы (как `vendorWordIn`).
vendor_word() {
  local s="${1,,}" m
  for m in $MARKS; do [[ "$s" == *"$m"* ]] && return 0; done
  [[ "$s" =~ (^|[^a-z])${BRAND}([^a-z]|$) ]]
}

# provider_census — подчарты поставщика на <B>, как их выводят гейты посадки и
# рендера: зависимости зонтика, чьё имя, псевдоним (имя, видимое значениям) или
# репозиторий называет поставщика, их базы `pg-<имя>`,
# записи `charts/` с отметкой в имени записи либо в `name` своего Chart.yaml
# (каталог и имя разные законно, #2759) и записи `charts/<имя>-…` зависимостей
# поставщика. Заполняет PROVIDER_SET (имена подчартов для рендера),
# PROVIDER_ENTRIES (найденное, для п.4) и PROVIDER_CENSUS (объём обхода).
# Ноль найденного — исход, когда обход прочитал отметки, зависимости и записи
# `charts/`; пустой обход — отказ: «чужого нет» неотличимо от «не прочитано».
PROVIDER_SET=""; PROVIDER_ENTRIES=""; PROVIDER_CENSUS=""; PROVIDER_ERR=""; MARKS=""; BRAND=""
provider_census() {
  local chart deps all line name repo shown nd=0 ne=0 listing type path base sub prov="" hit p
  MARKS="$(vendor_dict marks 2>"$WORK/perr")" || { PROVIDER_ERR="$(cat "$WORK/perr")"; return 1; }
  [ -n "$MARKS" ] || { PROVIDER_ERR="в $VENDOR_HOME на $B не прочитано ни одной отметки — поставщика узнавать нечем"; return 1; }
  BRAND="$(vendor_dict brand 2>"$WORK/perr")" || { PROVIDER_ERR="$(cat "$WORK/perr")"; return 1; }
  # Бренд встаёт в выражение границы как есть: допустимы только буквы.
  [[ "$BRAND" =~ ^[a-z]+$ ]] || { PROVIDER_ERR="бренд словаря $VENDOR_HOME на $B — «$BRAND», а не одно слово из букв"; return 1; }
  chart="$(git -C "$REPO" show "$B:deploy/helm/umbrella/Chart.yaml" 2>&1)" || { PROVIDER_ERR="Chart.yaml зонтика на $B не читается: $chart"; return 1; }
  deps="$(printf '%s\n' "$chart" | yq -r '.dependencies[] | [(.alias // .name // ""), (.name // ""), (.repository // "")] | join("	")' 2>&1)" \
    || { PROVIDER_ERR="зависимости зонтика не разобраны: $deps"; return 1; }
  all="$(printf '%s\n' "$deps" | cut -f1)"
  while IFS='	' read -r shown name repo; do
    [ -n "$shown$name$repo" ] || continue
    nd=$((nd + 1))
    if vendor_word "$shown" || vendor_word "$name" || vendor_word "$repo"; then
      prov="$prov$shown"$'\n'
      PROVIDER_SET="$PROVIDER_SET$shown"$'\n'; PROVIDER_ENTRIES="${PROVIDER_ENTRIES}зависимость $shown"$'\n'
      if [[ $'\n'"$all"$'\n' == *$'\n'"pg-$shown"$'\n'* ]]; then
        PROVIDER_SET="${PROVIDER_SET}pg-$shown"$'\n'; PROVIDER_ENTRIES="${PROVIDER_ENTRIES}зависимость pg-$shown"$'\n'
      fi
    fi
  done <<<"$deps"
  [ "$nd" -ge 1 ] || { PROVIDER_ERR="в Chart.yaml зонтика на $B не прочитано ни одной зависимости — состав чужого взять неоткуда"; return 1; }
  listing="$(git -C "$REPO" ls-tree "$B" deploy/helm/umbrella/charts/ 2>&1)" || { PROVIDER_ERR="записи charts/ на $B не читаются: $listing"; return 1; }
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    ne=$((ne + 1))
    read -r _ type _ <<<"${line%%	*}"; path="${line#*	}"; base="${path##*/}"
    sub=""
    if [ "$type" = tree ]; then
      # Каталог без читаемого Chart.yaml — не подчарт, и имени у него нет.
      sub="$(git -C "$REPO" show "$B:$path/Chart.yaml" 2>/dev/null | yq -r '.name // ""' 2>/dev/null)" || sub=""
    fi
    hit=0
    if vendor_word "$base" || { [ -n "$sub" ] && vendor_word "$sub"; }; then hit=1; fi
    for p in $prov; do case "$base" in "$p"-*) hit=1 ;; esac; done
    [ "$hit" = 1 ] || continue
    PROVIDER_ENTRIES="${PROVIDER_ENTRIES}charts/$base"$'\n'
    # Объекты необъявленного подчарта helm подписывает его `name`.
    if [ -n "$sub" ] && [[ $'\n'"$all"$'\n' != *$'\n'"$sub"$'\n'* ]]; then PROVIDER_SET="$PROVIDER_SET$sub"$'\n'; fi
  done <<<"$listing"
  [ "$ne" -ge 1 ] || { PROVIDER_ERR="в deploy/helm/umbrella/charts/ на $B записей 0 — подчарт без объявления искать негде"; return 1; }
  PROVIDER_SET="$(printf '%s' "$PROVIDER_SET" | grep . | sort -u || true)"
  PROVIDER_ENTRIES="$(printf '%s' "$PROVIDER_ENTRIES" | grep . | sort -u || true)"
  PROVIDER_CENSUS="отметок словаря $(printf '%s\n' "$MARKS" | grep -c .) и бренд, зависимостей зонтика $nd, записей charts/ $ne"
  return 0
}

# Распознаватели рендера. Принадлежность объекта — `# Source:` под подчартом.
provider_objects() { # <рендер> [<имена подчартов по строке>] — рабочих объектов поставщика
  local set="${2-$PROVIDER_SET}" re
  set="$(printf '%s\n' "$set" | grep . || true)"
  [ -n "$set" ] || { awk 'END { print 0 }' "$1"; return; }
  re="/charts/($(printf '%s\n' "$set" | tr '\n' '|' | sed 's/|$//'))/"
  awk -v re="$re" '
    function flush() { if (src ~ re && kind ~ /^(Deployment|StatefulSet|Job)$/) c++; src = ""; kind = "" }
    /^---/ { flush(); next }
    /^# Source: / { src = $3; next }
    /^kind: / { kind = $2; next }
    END { flush(); print c + 0 }' "$1"
}
env_values() { # <рендер> <имя переменной> — значения переменной окружения, по строке
  yq -r ".. | select(tag == \"!!map\" and .name == \"$2\") | .value // \"\"" "$1"
}
render_objects() { grep -c '^# Source: ' "$1" || true; }

cond2_control() { # распознаватели обязаны видеть то, что считают
  # Имя подчарта синтетики своё, а не из выведенного набора: распознаватель
  # доказывается и тогда, когда поставщика в зонтике нет (после kacho#1276).
  local syn="$WORK/synthetic.yaml" v n first="ntf2-control-provider"
  printf '%s\n' '---' "# Source: kacho-umbrella/charts/$first/templates/deployment.yaml" 'kind: Deployment' \
    'spec:' '  template:' '    spec:' '      containers:' '        - env:' \
    "            - name: $POSTURE_KNOB" '              value: "foreign"' \
    '---' '# Source: kacho-umbrella/charts/api-gateway/templates/deployment.yaml' 'kind: Deployment' \
    '---' "# Source: kacho-umbrella/charts/$first/templates/configmap.yaml" 'kind: ConfigMap' >"$syn"
  n="$(provider_objects "$syn" "$first")"
  v="$(env_values "$syn" "$POSTURE_KNOB" 2>"$WORK/err")" || { echo "yq отказал на синтетике: $(cat "$WORK/err")"; return 1; }
  [ "$n" = 1 ] && [ "$v" = foreign ] || { echo "синтетика: объектов поставщика $n (ожидался 1), посадка «$v» (ожидалась foreign)"; return 1; }
  echo "синтетика: Deployment под charts/$first/ → объектов поставщика 1, ConfigMap там же и Deployment края — 0; посадка foreign"
}

cond2() {
  local names n args err rc out objs post lane prov bad="" chains=0 last ctl
  local -A chain_args=()
  [ "$DISK_OK" = 1 ] || { nr п.2 "$DISK_REASON"; return; }
  # Сначала СТРОКИ всех цепочек: отказ таблицы и обёртки — первым и со своим
  # текстом, а не подменённый отказом того, что стоит дальше.
  names="$(stacks_names 2>"$WORK/err")" || { nr п.2 "общий читатель таблицы отказал: $(cat "$WORK/err")"; return; }
  for n in $names; do
    if [ "$n" = prod ]; then
      args="$(render_chain_args prod "$UMBRELLA" "$SAMPLE" 2>"$WORK/err")" \
        || { nr п.2 "обёртка рендера отказала на prod: $(cat "$WORK/err")"; return; }
      last="${args##* }"; last="${last##*/}"
      [ "$last" = "$SAMPLE" ] || { nr п.2 "обёртка не дописала образец последним: $args"; return; }
      echo "  цепочка prod: строка $args; образец последним: $last"
    else
      args="$(render_chain_args "$n" "$UMBRELLA" 2>"$WORK/err")" \
        || { nr п.2 "обёртка рендера отказала на $n: $(cat "$WORK/err")"; return; }
    fi
    chain_args[$n]="$args"
  done
  command -v helm >/dev/null 2>&1 || { nr п.2 "helm не найден"; return; }
  [[ "$(yq --version 2>/dev/null)" == *mikefarah* ]] || { nr п.2 "в PATH не mikefarah yq"; return; }
  [ -z "$PROVIDER_ERR" ] || { nr п.2 "подчарты поставщика не выведены: $PROVIDER_ERR"; return; }
  echo "  подчарты поставщика на $B: $(printf '%s\n' "$PROVIDER_SET" | grep . | tr '\n' ' ' | sed 's/ $//' | grep . || echo нет) ($PROVIDER_CENSUS)"
  ctl="$(cond2_control)" || { nr п.2 "контроль распознавателей: $ctl"; return; }
  echo "  контроль п.2: $ctl"
  for n in $names; do
    args="${chain_args[$n]}"
    out="$WORK/chain-$n.yaml"
    # Цепочка дробится на слова намеренно: это перечень `-f <файл>`.
    # shellcheck disable=SC2086
    (cd "$UMBRELLA" && helm template "$RELEASE" . -f values.yaml $args) >"$out" 2>"$WORK/err" && rc=0 || rc=$?
    err="$(grep -v 'WARNING: Kubernetes configuration' "$WORK/err" || true)"
    [ "$rc" -eq 0 ] || { nr п.2 "helm template цепочки $n отказал (код $rc): $(printf '%s' "$err" | tr '\n' ' ' | cut -c1-400)"; return; }
    objs="$(render_objects "$out")"
    [ "$objs" -ge 1 ] || { nr п.2 "рендер цепочки $n пуст при коде 0 — судить нечего"; return; }
    post="$(env_values "$out" "$POSTURE_KNOB" 2>"$WORK/err")" || { nr п.2 "yq отказал на рендере $n: $(cat "$WORK/err")"; return; }
    post="$(printf '%s\n' "$post" | grep . | sort -u | tr '\n' ',' | sed 's/,$//')"
    lane="$(env_values "$out" "$LANE_KNOB" | grep -c . || true)"
    prov="$(provider_objects "$out")"
    echo "  цепочка $n: объектов $objs; посадка края ${post:-нет}; ручка полосы формы $lane; рабочих объектов поставщика $prov"
    [ "$post" = own ] && [ "$lane" = 1 ] && [ "$prov" = 0 ] || bad="$bad $n"
    chains=$((chains + 1))
  done
  [ "$chains" -ge 1 ] || { nr п.2 "цепочек ноль — обходить нечего"; return; }
  if [ -z "$bad" ]; then
    outcome п.2 выполнено "цепочек $chains: посадка own, ручка 1, объектов поставщика 0 в каждой"
  else
    outcome п.2 "не выполнено" "цепочек $chains; не own либо ручка ≠ 1 либо поставщик поднят:$bad"
  fi
}

# ── п.3 край ретранслирует verify-email и verify-email/confirm ───────────────
cond3() {
  local f="gateway/internal/middleware/login_lane_paths.go" out paths n a b neg
  out="$(git -C "$REPO" grep -hoE '"/iam/v1/auth/[a-z0-9/_:-]*"' "$B" -- "$f" 2>"$WORK/err")" \
    || { nr п.3 "git grep путей полосы в $f на $B не дал ни строки либо отказал: $(cat "$WORK/err")"; return; }
  paths="$(printf '%s\n' "$out" | sort -u)"
  n="$(printf '%s\n' "$paths" | grep -c . || true)"
  a="$(printf '%s\n' "$paths" | grep -cxF '"/iam/v1/auth/verify-email"' || true)"
  b="$(printf '%s\n' "$paths" | grep -cxF '"/iam/v1/auth/verify-email/confirm"' || true)"
  neg="$(printf '%s\n' "$paths" | grep -cxF '"/iam/v1/auth/register/confirm"' || true)"
  local detail="путей полосы в $f: $n; verify-email $a, verify-email/confirm $b (контроль: register/confirm $neg)"
  if [ "$a" = 1 ] && [ "$b" = 1 ]; then outcome п.3 выполнено "$detail"; else outcome п.3 "не выполнено" "$detail"; fi
}

# ── п.4 подчартов поставщика в зонтике нет (условие полосы стража почты) ─────
# Команда Р16 п.4 — счёт записей `charts/` поставщика; здесь к ней добавлены
# объявленные зависимости поставщика (архив внешней зависимости в дереве не
# лежит). Имена — отметки словаря, а не выписанный перечень.
cond4() {
  local k detail
  [ -z "$PROVIDER_ERR" ] || { nr п.4 "подчарты поставщика не выведены: $PROVIDER_ERR"; return; }
  k="$(printf '%s\n' "$PROVIDER_ENTRIES" | grep -c . || true)"
  detail="подчартов поставщика в зонтике: $k$( [ "$k" = 0 ] || printf ' (%s)' "$(printf '%s\n' "$PROVIDER_ENTRIES" | tr '\n' ' ' | sed 's/ $//')") ($PROVIDER_CENSUS; только для полосы, правящей страж почты, DoD п.14)"
  if [ "$k" = 0 ]; then outcome п.4 выполнено "$detail"; else outcome п.4 "не выполнено" "$detail"; fi
}

# ── п.5 узел полосы стенда объявлен (цепочка dev) ────────────────────────────
# Выражение Р16 п.5 дословно; `$i` — переменная yq, а не оболочки.
# shellcheck disable=SC2016
P5_EXPR='. as $i ireduce ({}; . * $i) | [.global.kacho.identity.smtp.connectionURI // "", .global.kacho.identity.smtp.trustAnchorSecret.name // "", .global.kacho.identity.smtp.trustAnchorSecret.key // "", .mailpit.tlsSecretName // ""] | join(" | ")'
cond5() {
  local chain f files=() fields ctl suffix tpl uri name key tls
  [ "$DISK_OK" = 1 ] || { nr п.5 "$DISK_REASON"; return; }
  [[ "$(yq --version 2>/dev/null)" == *mikefarah* ]] || { nr п.5 "в PATH не mikefarah yq"; return; }
  chain="$(stacks_chain dev ' ' 2>"$WORK/err")" || { nr п.5 "общий читатель таблицы отказал на цепочке dev: $(cat "$WORK/err")"; return; }
  echo "  файлы цепочки dev (stacks.sh --chain): $chain"
  git -C "$REPO" show "$B:deploy/helm/umbrella/values.yaml" >"$WORK/p5-base.yaml" 2>"$WORK/err" \
    || { nr п.5 "values.yaml зонтика на $B не читается: $(cat "$WORK/err")"; return; }
  files=("$WORK/p5-base.yaml")
  for f in $chain; do
    git -C "$REPO" show "$B:deploy/helm/umbrella/$f" >"$WORK/p5-$f" 2>"$WORK/err" \
      || { nr п.5 "файл цепочки dev $f на $B не читается: $(cat "$WORK/err")"; return; }
    files+=("$WORK/p5-$f")
  done
  fields="$(yq eval-all -r "$P5_EXPR" "${files[@]}" 2>"$WORK/err")" || { nr п.5 "yq отказал: $(cat "$WORK/err")"; return; }
  ctl="$(yq eval-all -r "$P5_EXPR" "$WORK/p5-base.yaml" 2>"$WORK/err")" || { nr п.5 "yq отказал на контроле: $(cat "$WORK/err")"; return; }
  echo "  контроль п.5: один values.yaml → «$ctl» — три пустых поля"
  case "$ctl" in " |  |  | "*) ;; *) nr п.5 "контроль: один values.yaml дал «$ctl», а не три пустых поля — исход п.5 не приписывается цепочке dev"; return ;; esac
  # Имя сервиса приёмника — из помощника шаблона приёмника на <B>, не константой.
  tpl="$(git -C "$REPO" show "$B:deploy/helm/umbrella/templates/mail-receiver.yaml" 2>"$WORK/err")" \
    || { nr п.5 "шаблон приёмника на $B не читается: $(cat "$WORK/err")"; return; }
  suffix="$(printf '%s\n' "$tpl" | awk '/define "kacho.mailReceiver.fullname"/ {d = 1; next} d && /printf "%s[^"]*" \.Release\.Name/ {
      match($0, /printf "%s[^"]*"/); s = substr($0, RSTART + 10, RLENGTH - 11); print s; exit }')"
  [ -n "$suffix" ] || { nr п.5 "имя сервиса приёмника из помощника kacho.mailReceiver.fullname на $B не выводится"; return; }
  IFS='|' read -r uri name key tls <<<"$fields"
  uri="${uri% }"; name="${name# }"; name="${name% }"; key="${key# }"; key="${key% }"; tls="${tls# }"
  local uri_re="^smtps?://([^@/]+@)?\\{\\{ *\\.Release\\.Name *\\}\\}${suffix}:[0-9]+/?\$"
  if [[ "$uri" =~ $uri_re ]] \
     && [ -n "$name" ] && [ "$name" = "$tls" ] && [ "$key" = ca.crt ]; then
    outcome п.5 выполнено "поля: $fields"
  else
    outcome п.5 "не выполнено" "поля: $fields"
  fi
}

# ── п.6 и признак K — читают чарт notify и перечень источников NTF-1 ─────────
cond6k() {
  local chart deps notify fe
  chart="$(git -C "$REPO" show "$B:deploy/helm/umbrella/Chart.yaml" 2>"$WORK/err")" \
    || { nr п.6 "Chart.yaml зонтика на $B не читается: $(cat "$WORK/err")"; nr K "Chart.yaml зонтика на $B не читается"; return; }
  deps="$(printf '%s\n' "$chart" | yq -r '.dependencies[] | (.alias // .name)' 2>"$WORK/err")" \
    || { nr п.6 "зависимости зонтика не разобраны: $(cat "$WORK/err")"; nr K "зависимости зонтика не разобраны"; return; }
  notify="$(printf '%s\n' "$deps" | grep -cx notify || true)"
  fe="$(stacks_chain fe3455 ' ' 2>"$WORK/err")" || fe="не прочитана: $(cat "$WORK/err")"
  echo "  файлы цепочки fe3455 (stacks.sh --chain): $fe"
  nr п.6 "исполняется после Е13 замысла (NTF1-D1 чарт notify, NTF1-D2 узел fe3455, NTF1-D1s); на $B зависимость notify в зонтике: $notify"
  nr K "записи перечня источников notify читаются из рендера чарта notify NTF1-D1/NTF1-D2 (Е13); на $B зависимость notify в зонтике: $notify"
}

provider_census || true

cond1
cond2
cond3
cond4
cond5
cond6k

echo "итог: выполнено $HELD · не выполнено $NOT_HELD · не выполнилось $NOT_RUN (исходов $((HELD + NOT_HELD + NOT_RUN)))"
if [ "$NOT_RUN" -gt 0 ]; then exit 2; fi
if [ "$NOT_HELD" -gt 0 ]; then exit 1; fi
exit 0
