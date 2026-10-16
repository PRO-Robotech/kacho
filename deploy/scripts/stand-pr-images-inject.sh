#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stand-pr-images-inject.sh — доказательство инъекцией для стенда проб на образах
# ЛЮБОЙ ревизии ветки запроса (kacho#3260): правило формы тега, выбор ветки
# публикации реестром, ответ реестра, команда сборки в отказе, перекатка одной
# службы.
#
# Каждый механизм проверяется ДВАЖДЫ: на исправном коде утверждения сходятся
# (контроль), а на коде с ОДНИМ внесённым дефектом — краснеют (мутант убит).
# Мутант вносится в ту функцию, которая держит объявленное свойство, а не в
# соседнюю: свойство «404 ≠ молчание реестра» держит `reachable` (раздел G),
# ветку в команде сборки — `build_hint` (H), провенанс перекатки — `cmd_reload`
# (I). Заглушка ставится на ГРАНИЦЕ процесса (curl, kubectl), а не на судимой
# функции: заглушённая функция своих дефектов не показывает.
# Мутант вносится в ПАМЯТИ: скрипт подключается (`.`), функция переопределяется
# правленой копией собственного текста (`declare -f … | sed`), рабочая копия не
# трогается. Мутант, у которого правка не легла (текст функции не изменился),
# считается провалом доказательства, а не «убитым»: иначе устаревший образец sed
# молча превращал бы доказательство в пустое.
#
# Кластер, реестр и сеть не нужны: в разделе D реестр — заглушка `reachable`
# по таблице (судится выбор ветки), в G — заглушка curl, ведущая себя как реестр
# (судится сама `reachable`), в I — заглушка kubectl по сценарию (судится
# `cmd_reload`); история — временный репозиторий. Нужны bash, git, jq и go (читатель имён
# частей продукта собирается из дерева).
#
# Исходы: 0 — все контроли сошлись, все мутанты убиты; 1 — иначе.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
pass=0 fail=0 mutants=0 killed=0 survived=0 unapplied=0

ok()  { pass=$((pass + 1)); printf '  ok     %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf '  ПРОВАЛ %s\n' "$1"; }

# mutate ФУНКЦИЯ SED — переопределить функцию правленой копией; 1, если правка не легла.
mutate() {
  local before after
  before="$(declare -f "$1")" || return 1
  after="$(sed -E "$2" <<<"$before")"
  [ "$before" != "$after" ] || return 1
  eval "$after"
}

# verdict ИМЯ КОД — подвести мутант: 0 — утверждения сошлись (мутант выжил),
# 1 — покраснели (убит), 3 — правка не легла, иное — прогон не выполнился.
verdict() {
  mutants=$((mutants + 1))
  case "$2" in
    0) survived=$((survived + 1)); bad "мутант «$1» ВЫЖИЛ — утверждения его не видят" ;;
    1) killed=$((killed + 1)); ok "мутант «$1» убит" ;;
    3) unapplied=$((unapplied + 1)); bad "мутант «$1» не лёг — образец устарел, доказательство пусто" ;;
    *) unapplied=$((unapplied + 1)); bad "мутант «$1»: прогон не выполнился (код $2) — это не «убит»" ;;
  esac
}

# ── A. ПРАВИЛО ФОРМЫ ТЕГА (gen-managed-image-pins.sh, самопроверка 18–23) ───────
echo "A. имя ветки в теге переводится так, как его переводит сборка"
pins() { # pins [ФУНКЦИЯ SED] — самопроверка порождающего пинов, с мутантом или без
  (
    fn="${1-}" sd="${2-}"
    # Подключение — без аргументов: `.` отдаёт подключённому позиционные
    # параметры вызывающего, и разбор ключей порождающего принял бы их за свои.
    set --
    # shellcheck source=deploy/scripts/gen-managed-image-pins.sh
    . "$HERE/gen-managed-image-pins.sh" || exit 2
    if [ -n "$fn" ]; then mutate "$fn" "$sd" || exit 3; fi
    self_test >"$tmp/pins.out" 2>&1 && exit 0 || exit 1
  )
}
pins; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: самопроверка порождающего зелёная" || { bad "контроль: самопроверка красная (код $rc)"; sed 's/^/      /' "$tmp/pins.out"; }
pins _service_tag_branch 's|"\$\{1//\\//-\}"|"$1"|';                         verdict "службы: слэш не переводится" $?
pins _console_tag_branch "s|tr -c 'a-zA-Z0-9._-' '-'|tr '/' '-'|";         verdict "консоль: переводится только слэш" $?
pins _console_tag_branch 's/ \| cut -c1-60//';                               verdict "консоль: без обрезки до 60" $?
pins want_tag 's/"\$\(_console_tag_branch "\$2"\)" "\$3"/"$(_console_tag_branch "$2")" "${3:0:8}"/'; verdict "консоль: коммит обрезан до восьми" $?

echo "B. --tag-of не судит членства коммита в ветке"
head_sha="$(git -C "$REPO" rev-parse HEAD)"
out="$(bash "$HERE/gen-managed-image-pins.sh" --tag-of kacho-vpc,kacho-ui-future-host --ref "$head_sha" --branch inject/no-such-branch 2>&1)"; rc=$?
want="$(printf 'kacho-vpc\tinject-no-such-branch-%s\nkacho-ui-future-host\tinject-no-such-branch-%s' "${head_sha:0:8}" "$head_sha")"
[ "$rc" -eq 0 ] && [ "$out" = "$want" ] && ok "ветки нет ни на origin, ни в клоне — тег выведен (код 0)" || bad "код $rc, вывод: $out"
bash "$HERE/gen-managed-image-pins.sh" --tag-of kacho-vpc --ref 0000000000000000000000000000000000000000 --branch main >/dev/null 2>&1; rc=$?
[ "$rc" -eq 2 ] && ok "близнец: неразрешимая ревизия — по-прежнему отказ (код 2)" || bad "неразрешимая ревизия: код $rc"

# ── Подключение стенда: определения без исполнения режима ───────────────────────
stand() {
  # shellcheck source=deploy/scripts/stand-ns.sh
  . "$HERE/stand-ns.sh"
}

# ── C. КАНДИДАТКИ В ВЕТКИ ПУБЛИКАЦИИ (image_branch_candidates) ─────────────────
echo "C. ветки-кандидатки выводятся из истории, первой — та, чья голова на origin"
r="$tmp/repo"
git init -q -b main "$r"
c() { git -C "$r" -c user.name=i -c user.email=i@i commit -q --allow-empty -m "$1"; git -C "$r" rev-parse HEAD; }
A="$(c A)"; git -C "$r" update-ref refs/remotes/origin/main "$A"
git -C "$r" checkout -q -b work
B="$(c B)"; git -C "$r" update-ref refs/remotes/origin/feature/x "$B"
C="$(c C)"; git -C "$r" update-ref refs/remotes/origin/later "$C"
git -C "$r" reset -q --hard "$B"
cands() { # cands [ФУНКЦИЯ SED] — «список для B | список для A | явный список»
  (
    stand
    # shellcheck disable=SC2034 # корень читают подключённые функции стенда
    REPO_ROOT="$r"
    if [ "$#" -eq 2 ]; then mutate "$1" "$2" || exit 3; fi
    b="$(image_branch_candidates "$B" | tr '\n' ' ')"
    a="$(image_branch_candidates "$A" | tr '\n' ' ')"
    e="$(IMAGE_BRANCHES='x  y x' image_branch_candidates "$B" | tr '\n' ' ')"
    printf '%s|%s|%s' "$b" "$a" "$e" >"$tmp/cands.out"
    [ "$b" = "feature/x work main later " ] && [ "$a" = "main feature/x later " ] && [ "$e" = "x y " ]
  )
}
cands; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: B → feature/x work main later; A → main feature/x later; явный «x  y x» → x y" || bad "контроль: $(cat "$tmp/cands.out")"
cands image_branch_candidates '/points-at .*refs\/remotes\/origin/d'; verdict "голова на origin не спрошена" $?
cands image_branch_candidates '/echo main/d';                           verdict "ствол не в кандидатках" $?
cands image_branch_candidates 's/ && !seen\[\$0\]\+\+//';             verdict "повторы не сняты" $?

# ── D. ВЫБОР ВЕТКИ РЕЕСТРОМ И КОМАНДА СБОРКИ (pick_image_tags, build_hint) ─────
echo "D. ветка публикации выбирается ответом реестра; нет образа — команда сборки"
names=$'kacho-vpc\nkacho-ui-future-host'
s8="${head_sha:0:8}"
pick() { # pick РЕЕСТР ОШИБКА [ФУНКЦИЯ SED] — код pick_image_tags; итог и отказ — в файлах
  local reg="$1" err="$2"; shift 2
  (
    stand
    # shellcheck disable=SC2034 # корень читают подключённые функции стенда
    REPO_ROOT="$REPO"
    image_branch_candidates() { printf '%s\n' feature/x other; }
    reachable() { [ -z "$err" ] || return 2; case " $reg " in *" $1:$2 "*) return 0 ;; *) return 1 ;; esac; }
    if [ "$#" -eq 2 ]; then mutate "$1" "$2" || exit 3; fi
    pick_image_tags "$head_sha" "$names" 2>"$tmp/pick.err"; rc=$?
    printf '%s|%s' "$IMAGE_BRANCH" "$PICKED_TAGS" >"$tmp/pick.out"
    exit "$rc"
  )
}
P=docker.io/prorobotech
both_other="$P/kacho-vpc:other-$s8 $P/kacho-ui-future-host:other-$head_sha"
svc_only="$P/kacho-vpc:feature-x-$s8"
d_checks() { # d_checks [ФУНКЦИЯ SED] — все утверждения раздела D разом; 0 — сошлись
  local rc
  pick "$both_other" "" "$@"; rc=$?
  [ "$rc" -eq 3 ] && return 3
  [ "$rc" -eq 0 ] && [ "$(cat "$tmp/pick.out")" = "other|$(printf 'kacho-vpc\tother-%s\nkacho-ui-future-host\tother-%s' "$s8" "$head_sha")" ] || return 1
  pick "" "" "$@"; rc=$?
  [ "$rc" -eq 1 ] && grep -q 'gh workflow run docker-build.yml' "$tmp/pick.err" && grep -q 'gh workflow run ui.yml' "$tmp/pick.err" || return 1
  pick "$svc_only" "" "$@"; rc=$?
  [ "$rc" -eq 1 ] && grep -q 'gh workflow run ui.yml' "$tmp/pick.err" && ! grep -q 'docker-build.yml' "$tmp/pick.err" || return 1
  pick "$both_other" "сеть" "$@"; rc=$?
  [ "$rc" -eq 2 ] || return 1
  return 0
}
d_checks; rc=$?
if [ "$rc" -eq 0 ]; then
  ok "контроль: вторая кандидатка с образами выбрана; нет образов — обе команды сборки; нет консоли — только ui.yml; реестр молчит — код 2"
else bad "контроль раздела D (последний вывод: $(cat "$tmp/pick.out"); отказ: $(tr '\n' ' ' <"$tmp/pick.err"))"; fi
d_checks pick_image_tags '/не ответил о \$IMAGE_PREFIX\/\$first/{n;s/return 2/:/;}'; verdict "молчание реестра принято за «образа нет»" $?
d_checks pick_image_tags 's/missing="\$missing \$img";//';                     verdict "образы, кроме первого, не проверяются" $?
d_checks build_hint 's/\[ "\$svc" -eq 0 \] \|\| //';                            verdict "команда сборки служб печатается всегда" $?

# ── E. ЦЕЛИ ПЕРЕКАТКИ ПО ИМЕНИ ОБРАЗА (reload_targets) ─────────────────────────
echo "E. перекатываются ровно контейнеры с образом службы, init-контейнер миграций — тоже"
cat >"$tmp/workloads.json" <<'JSON'
{"items":[
 {"kind":"Deployment","metadata":{"name":"vpc"},"spec":{"template":{"spec":{
   "initContainers":[{"name":"migrate","image":"docker.io/prorobotech/kacho-vpc:old-1"}],
   "containers":[{"name":"vpc","image":"docker.io/prorobotech/kacho-vpc:old-1"}]}}}},
 {"kind":"Deployment","metadata":{"name":"ui-vpc"},"spec":{"template":{"spec":{
   "containers":[{"name":"vpc","image":"docker.io/prorobotech/kacho-ui-future-vpc:old-1"}]}}}},
 {"kind":"Deployment","metadata":{"name":"extras"},"spec":{"template":{"spec":{
   "containers":[{"name":"x","image":"docker.io/prorobotech/kacho-vpc-extras:old-1"}]}}}},
 {"kind":"Deployment","metadata":{"name":"bydigest"},"spec":{"template":{"spec":{
   "containers":[{"name":"vpc","image":"docker.io/prorobotech/kacho-vpc@sha256:0123"}]}}}},
 {"kind":"StatefulSet","metadata":{"name":"pg-vpc"},"spec":{"template":{"spec":{
   "containers":[{"name":"postgresql","image":"docker.io/bitnamilegacy/postgresql:16.4.0"}]}}}}
]}
JSON
targets() {
  (
    stand
    if [ "$#" -eq 2 ]; then mutate "$1" "$2" || exit 3; fi
    got="$(reload_targets kacho-vpc <"$tmp/workloads.json" | LC_ALL=C sort | tr '\t\n' ': ')"
    printf '%s' "$got" >"$tmp/targets.out"
    [ "$got" = "deployment/bydigest:vpc deployment/vpc:migrate deployment/vpc:vpc " ]
  )
}
targets; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: vpc (init и основной), ссылка по отпечатку; консоль, тёзка-с-хвостом и база — не тронуты" || bad "контроль: $(cat "$tmp/targets.out")"
targets reload_targets 's/== \$img\)/| startswith($img))/';                     verdict "сравнение имени приставкой" $?
targets reload_targets 's/\(\$w\.spec\.template\.spec\.initContainers \/\/ \[\]\) \+ //'; verdict "init-контейнер миграций не перекатывается" $?
targets reload_targets 's/sub\("@\.\*\$"; ""\) \| //';                          verdict "ссылка по отпечатку не разобрана" $?

# ── F. ГРАНИЦА ПЕРЕКАТКИ: МЕНЯЛСЯ ЛИ ЧАРТ (reload_chart_changed) ───────────────
echo "F. перекатка образа отказывает, если между ревизиями менялся чарт"
q="$tmp/chart"
git init -q -b main "$q"
w() { mkdir -p "$q/$(dirname "$1")"; echo "$2" >"$q/$1"; git -C "$q" add -A; git -C "$q" -c user.name=i -c user.email=i@i commit -qm "$1"; git -C "$q" rev-parse HEAD; }
F0="$(w services/vpc/internal/a.go 1)"
F1="$(w services/vpc/internal/a.go 2)"
F2="$(w services/vpc/deploy/README.md 2)"
F3="$(w services/vpc/deploy/templates/d.yaml 1)"
F4="$(w deploy/helm/umbrella/values.yaml 1)"
F5="$(w services/vpc/deploy/values.dev.yaml 1)"
chart() {
  (
    stand
    # shellcheck disable=SC2034 # корень читают подключённые функции стенда
    REPO_ROOT="$q"
    if [ "$#" -eq 2 ]; then mutate "$1" "$2" || exit 3; fi
    r=""
    for pair in "$F0 $F1" "$F1 $F2" "$F2 $F3" "$F3 $F4" "$F4 $F5" "$F0 nope"; do
      # shellcheck disable=SC2086 # пара — два слова
      reload_chart_changed $pair >/dev/null 2>&1; r="$r$?"
    done
    printf '%s' "$r" >"$tmp/chart.out"
    [ "$r" = "110002" ]
  )
}
chart; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: код и README — не чарт; шаблон, зонтик, values части — чарт; неразрешимая ревизия — код 2" || bad "контроль: коды $(cat "$tmp/chart.out"), ждали 110002"
chart reload_chart_changed "s/ ':\(glob\)\*\*\/deploy\/templates\/\*\*'//";   verdict "шаблоны части не судятся" $?
chart reload_chart_changed "s/ 'deploy\/helm'//";                              verdict "зонтик не судится" $?
chart reload_chart_changed 's/^( *)return 2$/\1return 1/';                  verdict "отказ сравнения принят за «не менялся»" $?

# ── G. ОТВЕТ РЕЕСТРА (reachable) ───────────────────────────────────────────────
# Заглушка — curl, а не reachable: судится сама функция, которая разводит «образа
# нет» (404) и «реестр не ответил» (предел частоты, 5xx, сеть, учётка). Заглушка
# ведёт себя как реестр: без выданной учётки манифест отвечает 401.
echo "G. реестр: 200 — есть, 404 — нет; предел частоты, 5xx, сеть, учётка — «не ответил»"
reach() { # reach [ФУНКЦИЯ SED] — коды reachable по таблице ответов заглушки curl
  (
    stand
    if [ "$#" -eq 2 ]; then mutate "$1" "$2" || exit 3; fi
    curl() {
      local a url="" auth=""
      for a; do case "$a" in https://*) url="$a" ;; "Authorization: Bearer "*) auth="$a" ;; esac; done
      printf '%s\n' "$url" >>"$tmp/curl.urls"
      case "$url" in
        https://auth.docker.io/token*)
          case "$REG" in tokfail) return 22 ;; toknull) echo '{"token":null}' ;; *) echo '{"token":"t0k"}' ;; esac ;;
        https://registry-1.docker.io/v2/*)
          [ "$auth" = "Authorization: Bearer t0k" ] || { printf 401; return 0; }
          case "$REG" in 000) printf 000; return 28 ;; *) printf '%s' "$REG" ;; esac ;;
        *) return 6 ;;
      esac
    }
    : >"$tmp/curl.urls"
    r=""
    for REG in 200 404 429 500 503 000 tokfail toknull; do
      reachable docker.io/prorobotech/kacho-vpc 3260-x-82eaf634 2>/dev/null; r="$r$?"
    done
    REG=404; reachable ghcr.io/prorobotech/kacho-vpc t 2>/dev/null; r="$r$?"
    printf '%s' "$r" >"$tmp/reach.out"
    [ "$r" = "012222220" ] || exit 1
    grep -Fxq 'https://auth.docker.io/token?service=registry.docker.io&scope=repository:prorobotech/kacho-vpc:pull' "$tmp/curl.urls" &&
      grep -Fxq 'https://registry-1.docker.io/v2/prorobotech/kacho-vpc/manifests/3260-x-82eaf634' "$tmp/curl.urls" &&
      ! grep -q ghcr "$tmp/curl.urls"
  )
}
reach; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: 200→0, 404→1, 429/500/503/сеть/учётка не выдана/учётка null→2, не docker.io→0 без запроса; адреса учётки и манифеста — по образу и тегу" || bad "контроль: коды $(cat "$tmp/reach.out"), ждали 012222220; запросы: $(tr '\n' ' ' <"$tmp/curl.urls")"
reach reachable '/case "\$code" in/,/esac/s/return 2/return 1/'; verdict "X1: предел частоты и 5xx приняты за «образа нет»" $?
reach reachable 's/jq -r \.token\)" \|\| return 2/jq -r .token)" || return 1/'; verdict "X2: отказ в учётке принят за «образа нет»" $?
reach reachable '/case "\$code" in/,/esac/s/return 1/return 2/'; verdict "X3: 404 принят за молчание реестра" $?
reach reachable '/case "\$code" in/,/esac/s/return 0/return 1/'; verdict "200 не признан находкой образа" $?
reach reachable 's/path="\$\{repo#docker\.io\/\}"/path="$repo"/'; verdict "путь образа с именем реестра" $?
reach reachable 's/ -H "Authorization: Bearer \$tok"//'; verdict "манифест спрошен без учётки" $?

# ── H. КОМАНДА СБОРКИ: ВЕТКА С ГОЛОВОЙ SHA (build_hint) ────────────────────────
echo "H. команда сборки называет ветку, чья голова — ревизия (origin первым), и адрес репозитория"
h="$tmp/hint"
git init -q -b main "$h"
hc() { git -C "$h" -c user.name=i -c user.email=i@i commit -q --allow-empty -m "$1"; git -C "$h" rev-parse HEAD; }
H0="$(hc H0)"; H1="$(hc H1)"; H2="$(hc H2)"
git -C "$h" update-ref refs/remotes/origin/feature/x "$H1"
git -C "$h" branch loc "$H1"
git -C "$h" remote add origin https://github.com/PRO-Robotech/kacho.git
hint() { # hint [ФУНКЦИЯ SED] — все утверждения раздела H разом; 0 — сошлись
  (
    stand
    # shellcheck disable=SC2034 # корень читают подключённые функции стенда
    REPO_ROOT="$h"
    if [ "$#" -eq 2 ]; then mutate "$1" "$2" || exit 3; fi
    l='stand-ns:   gh workflow run'
    build_hint "$H1" kacho-vpc 2>"$tmp/hint.1"
    build_hint "$H2" kacho-vpc kacho-ui-future-host 2>"$tmp/hint.2"
    build_hint "$H0" kacho-ui-future-host 2>"$tmp/hint.0"
    git -C "$h" remote set-url origin git@github.com:PRO-Robotech/kacho.git
    build_hint "$H1" kacho-vpc 2>"$tmp/hint.s"
    git -C "$h" remote set-url origin https://github.com/PRO-Robotech/kacho.git
    grep -Fxq "$l docker-build.yml -R PRO-Robotech/kacho --ref feature/x" "$tmp/hint.1" && ! grep -q 'ui\.yml' "$tmp/hint.1" &&
      grep -Fxq "$l docker-build.yml -R PRO-Robotech/kacho --ref main" "$tmp/hint.2" &&
      grep -Fxq "$l ui.yml -R PRO-Robotech/kacho --ref main" "$tmp/hint.2" &&
      grep -Fxq "$l ui.yml -R PRO-Robotech/kacho --ref <ветка на origin, чья голова — ${H0:0:8}>" "$tmp/hint.0" &&
      grep -Fxq "$l docker-build.yml -R PRO-Robotech/kacho --ref feature/x" "$tmp/hint.s"
  )
}
hint; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: голова на origin → --ref feature/x (не местная тёзка loc); голова только местная → main; не голова ничего → заглушка с коротким SHA; https и ssh → PRO-Robotech/kacho" || bad "контроль: $(cat "$tmp"/hint.{1,2,0,s} 2>/dev/null | tr '\n' ' ')"
hint build_hint 's/--ref \$branch"/--ref $sha"/g'; verdict "X4: --ref называет SHA, а не ветку с его головой" $?
hint build_hint 's/(branch="\$\(\{ )git -C "\$REPO_ROOT" for-each-ref --points-at "\$sha" --format=.%\(refname:lstrip=3\). refs\/remotes\/origin/\1true/'; verdict "ветка origin не спрошена — названа местная" $?
hint build_hint 's/for-each-ref --points-at "\$sha" --format=(.%\(refname:lstrip=3\).) refs\/remotes\/origin/for-each-ref --contains "$sha" --format=\1 refs\/remotes\/origin/'; verdict "ветка, содержащая ревизию, выдана за ветку с её головой" $?
hint build_hint 's/; s#\\\.git\$##//'; verdict "хвост .git не снят с адреса репозитория" $?

# ── I. ПЕРЕКАТКА ОДНОЙ СЛУЖБЫ (cmd_reload) ─────────────────────────────────────
# Заглушка — kubectl: кластер отвечает таблицей сценария. Каждый красный
# сценарий отличается от исправного ОДНИМ фактом второго пода: чужая ревизия в
# файле образа, прежний образ при той же ревизии, ноль подов. Законные близнецы —
# исправный и «-dirty» (сборка из грязного дерева той же ревизии).
echo "I. перекатка: провенанс подов после замены образа, пин и сменённый чарт — отказ до записи"
rl="$tmp/rel"
git init -q -b main "$rl"
rw() { mkdir -p "$rl/$(dirname "$1")"; echo "$2" >"$rl/$1"; git -C "$rl" add -A; git -C "$rl" -c user.name=i -c user.email=i@i commit -qm "$1"; git -C "$rl" rev-parse HEAD; }
R0="$(rw services/vpc/internal/a.go 1)"
R1="$(rw services/vpc/internal/a.go 2)"
R2="$(rw services/vpc/deploy/templates/d.yaml 1)"
K_TAG="3260-x-${R1:0:8}"
K_NEW="docker.io/prorobotech/kacho-vpc:$K_TAG"
K_PREV="docker.io/prorobotech/kacho-vpc:other-${R1:0:8}"
reload_one() { # reload_one СЦЕНАРИЙ СЛУЖБА БЫЛО РЕВИЗИЯ [ФУНКЦИЯ SED] — код cmd_reload
  K_SCN="$1" K_SVC="$2" K_WAS="$3" K_REF="$4"; shift 4
  : >"$tmp/kubectl.calls"
  (
    stand
    # shellcheck disable=SC2034 # корень читают подключённые функции стенда
    REPO_ROOT="$rl"
    need_tools() { :; }; guard_context() { :; }; stand_nodes() { :; }
    ns_state() { echo test; }
    # shellcheck disable=SC2034 # итог читает cmd_reload
    pick_image_tags() { PICKED_TAGS="$(printf 'kacho-vpc\t%s' "$K_TAG")" IMAGE_BRANCH=x; }
    k_pod() { printf '{"metadata":{"name":"%s"},"status":{"phase":"Running","containerStatuses":[{"name":"vpc","image":"%s"}]}}' "$1" "$2"; }
    kubectl() {
      printf '%s\n' "$*" >>"$tmp/kubectl.calls"
      case "$*" in
        "get namespace "*) printf '{"metadata":{"labels":{"kacho.io/stand":"test"},"annotations":{"kacho.io/ref":"%s"}}}' "$K_WAS" ;;
        *" get deployment,statefulset,daemonset "*)
          printf '{"items":[{"kind":"Deployment","metadata":{"name":"vpc"},"spec":{"template":{"spec":{"containers":[{"name":"vpc","image":"%s"}]}}}}]}' "$K_PREV" ;;
        *" set image "* | *" annotate "* | *" rollout status "*) : ;;
        *" get pods "*)
          case "$K_SCN" in
            zero)  echo '{"items":[]}' ;;
            stale) printf '{"items":[%s,%s]}' "$(k_pod p1 "$K_NEW")" "$(k_pod p2 "$K_PREV")" ;;
            *)     printf '{"items":[%s,%s]}' "$(k_pod p1 "$K_NEW")" "$(k_pod p2 "$K_NEW")" ;;
          esac ;;
        *" exec p1 "*) case "$K_SCN" in dirty) echo "$R1-dirty" ;; *) echo "$R1" ;; esac ;;
        *" exec p2 "*) case "$K_SCN" in foreign) echo "$R0" ;; *) echo "$R1" ;; esac ;;
        *) return 9 ;;
      esac
    }
    if [ "$#" -eq 2 ]; then mutate "$1" "$2" || exit 3; fi
    cmd_reload t3260-x "$K_SVC" "$K_REF"
  ) >"$tmp/reload.out" 2>&1
}
i_checks() { # i_checks [ФУНКЦИЯ SED] — все утверждения раздела I разом; 0 — сошлись
  local rc got="" scn
  for scn in ok dirty foreign stale zero; do
    reload_one "$scn" vpc "$R0" "$R1" "$@"; rc=$?
    [ "$rc" -eq 3 ] && [ "$#" -eq 2 ] && ! grep -q 'провенанс перекатки' "$tmp/reload.out" && return 3
    got="$got $scn=$rc"
    if [ "$scn" = ok ]; then
      grep -Fxq -- "-n t3260-x set image deployment/vpc vpc=$K_NEW" "$tmp/kubectl.calls" &&
        grep -Fxq -- "-n t3260-x annotate deployment/vpc --overwrite kacho.io/ref=$R1" "$tmp/kubectl.calls" || got="$got(не-записано)"
    fi
  done
  reload_one pin iam "$R0" "$R1" "$@"; rc=$?
  got="$got pin=$rc"; [ -s "$tmp/kubectl.calls" ] && got="$got(кластер-тронут)"
  grep -q 'приезжает пином' "$tmp/reload.out" || got="$got(без-причины)"
  reload_one chart vpc "$R1" "$R2" "$@"; rc=$?
  got="$got chart=$rc"; grep -q ' set image ' "$tmp/kubectl.calls" && got="$got(образ-заменён)"
  printf '%s' "$got" >"$tmp/i.out"
  [ "$got" = " ok=0 dirty=0 foreign=3 stale=3 zero=3 pin=2 chart=2" ]
}
i_checks; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: ok и -dirty → 0 (образ и ревизия записаны); чужая ревизия, прежний образ, подов 0 → 3; пин → 2 без кластера; сменённый чарт → 2 без замены" || bad "контроль:$(cat "$tmp/i.out"); последний вывод: $(tail -3 "$tmp/reload.out" | tr '\n' ' ')"
i_checks cmd_reload 's/\[ "\$agree" -gt 0 \] && //'; verdict "X7: ноль подов принят за сошедшийся провенанс" $?
i_checks cmd_reload 's/if \[ "\$\(normalize_image "\$cimg"\)" != "\$\(normalize_image "\$ref_new"\)" \]; then/if false; then/'; verdict "X8: под на прежнем образе не судится" $?
i_checks cmd_reload 's/if \[ "\$\{rev%-dirty\}" = "\$sha" \]; then/if [ -n "$rev" ]; then/'; verdict "чужая ревизия в файле образа принята" $?
i_checks cmd_reload 's/"\$\{rev%-dirty\}" = /"$rev" = /'; verdict "близнец -dirty не снят — исправный стенд краснеет" $?
i_checks cmd_reload 's/if product_image_is_pinned "\$img"; then/if false; then/'; verdict "пин не судится — перекатка идёт в кластер" $?
i_checks cmd_reload 's/changed="\$\(reload_chart_changed "\$was" "\$sha"\)" \|\| crc=\$\?/changed=""; crc=1/'; verdict "сменённый чарт не судится" $?
i_checks cmd_reload 's/не сошёлся с \$sha" 3/не сошёлся с $sha" 1/'; verdict "расхождение провенанса отдано кодом 1, а не 3" $?

echo
echo "итог: утверждений $((pass + fail)) — сошлось $pass, провалено $fail; мутантов $mutants — убито $killed, выжило $survived, не легло $unapplied"
[ "$fail" -eq 0 ]
