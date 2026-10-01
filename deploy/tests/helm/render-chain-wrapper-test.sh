#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# render-chain-wrapper-test.sh — шелл-обёртка рендера цепочек
# (`lib/render-chain.sh`, функция `render_chain_args`) и проба образцов слоя
# оператора `prod` (NTF-1, полоса D9; замысел З28 «Образец узла — в фикстуре
# гейта», Д48, CX1-86 (б), CX1-87, CX1-89, CX1-92, N9, N11 · CX2-58, N12, N16).
#
# Что утверждается:
#   A. обёртка — только библиотека: исполнение по пути → код 2 и текст
#      «библиотека, подключайте source»; `source` и вызов → код 0; режим
#      в индексе — 100644 (N9);
#   B. ось каталога: пустой каталог либо не каталог → отказ «каталог
#      обязателен»; прежняя форма `render_chain_args prod <образец>` → отказ
#      «`<образец>` — не каталог»; новая форма → строка цепочки (CX1-92 (а), N16);
#   C. контракт (1)–(4), тот же набор, что у обёртки Go
#      (`deploy/stacks_render_wrapper_test.go`);
#   D. каждый `-f` цепочки `prod` от обёртки — существующий файл из каталога вне
#      зонтика (абсолютный каталог) и из зонтика (`.`); инъекция — элемент от
#      корня репозитория → красный с его именем;
#   E. отказ таблицы доезжает: таблицы нет → код ≠ 0 и пустой вывод; близнец —
#      таблица на месте → код 0 (CX1-89 (в));
#   F. проба образцов: обход КАЖДОГО файла каталога (число и имена), рендер
#      `prod` с каждым через обёртку → код 0 с именем образца; отрицания формы —
#      адрес формы `values.dev.yaml` → отказ стража почтовой полосы с именем
#      приёмника; у образца с `credentialSecret` — половина пары → отказ.
#
# Исходы — по контракту `outcome.sh`: 0 зелёный, 1 находка, 2 условие не создано.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$DEPLOY/helm/umbrella"
LIB="$HERE/lib/render-chain.sh"
SAMPLES="$DEPLOY/testdata/mail-node"
RELEASE="kacho-umbrella"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"

require_file_present "$LIB" "шелл-обёртка рендера цепочек"
require_dir_present "$SAMPLES" "каталог образцов слоя оператора"
require_file_present "$HERE/stacks.sh" "общий читатель таблицы стендов"

# shellcheck source=deploy/tests/helm/stacks.sh
. "$HERE/stacks.sh"
# shellcheck source=deploy/tests/helm/lib/render-chain.sh
. "$LIB"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# ── A. только библиотека ─────────────────────────────────────────────────────
out="$(bash "$LIB" 2>&1)" && rc=0 || rc=$?
[ "$rc" -eq 2 ] || fail "A: исполнение библиотеки по пути дало код $rc, ожидался 2"
case "$out" in *"библиотека, подключайте source"*) ;; *) fail "A: исполнение по пути без текста «библиотека, подключайте source»: $out" ;; esac
ok
out="$(bash -c '. "$1"; render_chain_args dev "$2" >/dev/null' _ "$LIB" "$UMBRELLA" 2>&1)" && rc=0 || rc=$?
[ "$rc" -eq 0 ] || fail "A: source и вызов дали код $rc: $out"
ok
# Индекс читается по НАСТОЯЩЕМУ положению библиотеки (`pwd -P`): зеркало дерева
# у гейта трёх исходов собрано ссылками вне git, и логический путь индекса не имеет.
LIBREAL="$(cd "$(dirname "$LIB")" && pwd -P)" || fatal "A: каталог библиотеки не разрешается"
if command -v git >/dev/null 2>&1 && git -C "$LIBREAL" rev-parse --git-dir >/dev/null 2>&1; then
  mode="$(git -C "$LIBREAL" ls-files -s -- render-chain.sh)" \
    || fatal "A: git ls-files не ответил"
  mode="${mode%% *}"
  [ "$mode" = "100644" ] || fail "A: режим библиотеки в индексе «${mode:-нет в индексе}», ожидался 100644"
  ok
else
  fatal "A: дерево не под git — режим библиотеки в индексе прочитать неоткуда"
fi

# ── B. ось каталога ──────────────────────────────────────────────────────────
expect_refusal() { # <метка> <подстрока stderr> <аргументы…>
  local label="$1" want="$2" o e rc
  shift 2
  o="$(render_chain_args "$@" 2>"$WORK/err")" && rc=0 || rc=$?
  e="$(cat "$WORK/err")"
  [ "$rc" -ne 0 ] || fail "$label: принято (вывод: $o)"
  [ -z "$o" ] || fail "$label: отказ с непустым выводом «$o»"
  case "$e" in *"$want"*) ;; *) fail "$label: отказ без «$want»: $e" ;; esac
  ok
}
expect_refusal "B: пустой каталог" "каталог обязателен" prod "" operator.yaml
expect_refusal "B: не каталог" "каталог обязателен" prod "$SAMPLES/operator.yaml" operator.yaml
expect_refusal "B: прежняя форма" "\`operator.yaml\` — не каталог" prod operator.yaml
args="$(render_chain_args prod "$UMBRELLA" operator.yaml)" || fail "B: новая форма отказала"
[ -n "$args" ] || fail "B: новая форма отдала пустую строку"
ok

# ── C. контракт (1)–(4) ──────────────────────────────────────────────────────
names="$(stacks_names)" || fatal "C: таблица стендов не читается"
others=0
for s in $names; do
  [ "$s" = prod ] && continue
  base="$(stacks_args "$s" "$UMBRELLA")" || fatal "C: stacks_args $s отказал"
  got="$(render_chain_args "$s" "$UMBRELLA" operator.yaml)" || fail "C(1): обёртка отказала на $s"
  [ "$got" = "$base" ] || fail "C(1): цепочка $s: обёртка «$got», общий читатель «$base»"
  others=$((others + 1)); ok
done
[ "$others" -gt 0 ] || fail "C(1): цепочек, кроме prod, ноль — случай не исполнен"
base="$(stacks_args prod "$UMBRELLA")" || fatal "C: stacks_args prod отказал"
got="$(render_chain_args prod "$UMBRELLA" operator.yaml)" || fail "C(2): обёртка отказала на prod"
[ "$got" = "$base -f $SAMPLES/operator.yaml" ] \
  || fail "C(2): prod «$got», ожидалось «$base -f $SAMPLES/operator.yaml»"
ok
expect_refusal "C(3): prod без образца" "образец обязателен для prod" prod "$UMBRELLA"
expect_refusal "C(3): prod с пустым именем" "образец обязателен для prod" prod "$UMBRELLA" ""
for bad in . ../mail-node/operator.yaml absent.yaml ../notify-standalone/values.yaml; do
  expect_refusal "C(4): «$bad»" "в каталоге: " prod "$UMBRELLA" "$bad"
done
# Подкаталог — во временной копии оснастки: в каталоге дерева его нет, и
# заводить его ради пробы значило бы править вход чужих гейтов.
copy_kit() { # <корень копии> — deploy/{stacks.txt,tests/helm/{stacks.sh,lib},testdata/mail-node}
  mkdir -p "$1/tests/helm/lib" "$1/testdata/mail-node"
  cp "$HERE/stacks.sh" "$1/tests/helm/stacks.sh"
  cp "$LIB" "$1/tests/helm/lib/render-chain.sh"
  cp "$SAMPLES/operator.yaml" "$1/testdata/mail-node/operator.yaml"
  cp "$DEPLOY/stacks.txt" "$1/stacks.txt"
}
copy_kit "$WORK/sub"
mkdir -p "$WORK/sub/testdata/mail-node/nested"
o="$(bash -c '. "$1"; render_chain_args prod "$2" nested' _ "$WORK/sub/tests/helm/lib/render-chain.sh" "$UMBRELLA" 2>"$WORK/err")" && rc=0 || rc=$?
[ "$rc" -ne 0 ] && [ -z "$o" ] || fail "C(4): подкаталог принят (код $rc, вывод «$o»)"
grep -q "в каталоге: operator.yaml" "$WORK/err" || fail "C(4): отказ на подкаталоге без перечня: $(cat "$WORK/err")"
ok

# ── D. элементы разрешаются соединением вызывающего ─────────────────────────
missing_elements() { # <строка аргументов> — печатает отсутствующие элементы `-f`
  local w prev="" miss=""
  # shellcheck disable=SC2086
  for w in $1; do
    if [ "$prev" = "-f" ] && [ ! -f "$w" ]; then miss="$miss $w"; fi
    prev="$w"
  done
  printf '%s' "${miss# }"
}
args="$(cd "$WORK" && render_chain_args prod "$UMBRELLA" operator.yaml)" || fail "D: обёртка отказала вне зонтика"
miss="$(cd "$WORK" && missing_elements "$args")"
[ -z "$miss" ] || fail "D: вне зонтика с абсолютным каталогом не разрешаются: $miss"
ok
args="$(cd "$UMBRELLA" && render_chain_args prod . operator.yaml)" || fail "D: обёртка отказала из зонтика"
miss="$(cd "$UMBRELLA" && missing_elements "$args")"
[ -z "$miss" ] || fail "D: из зонтика с «.» не разрешаются: $miss"
ok
injected="$(cd "$UMBRELLA" && stacks_args prod .) -f deploy/testdata/mail-node/operator.yaml" \
  || fatal "D: stacks_args prod отказал"
miss="$(cd "$UMBRELLA" && missing_elements "$injected")"
[ "$miss" = "deploy/testdata/mail-node/operator.yaml" ] \
  || fail "D: инъекция «элемент от корня репозитория» не дала красного с его именем (нашлось «$miss»)"
ok

# ── E. отказ таблицы доезжает ────────────────────────────────────────────────
copy_kit "$WORK/notable"
rm -f "$WORK/notable/stacks.txt"
o="$(bash -c '. "$1"; render_chain_args prod "$2" operator.yaml' _ "$WORK/notable/tests/helm/lib/render-chain.sh" "$UMBRELLA" 2>"$WORK/err")" && rc=0 || rc=$?
[ "$rc" -ne 0 ] && [ -z "$o" ] || fail "E: таблицы нет, а обёртка дала код $rc и вывод «$o»"
ok
copy_kit "$WORK/table"
o="$(bash -c '. "$1"; render_chain_args prod "$2" operator.yaml' _ "$WORK/table/tests/helm/lib/render-chain.sh" "$UMBRELLA" 2>"$WORK/err")" && rc=0 || rc=$?
[ "$rc" -eq 0 ] && [ -n "$o" ] || fail "E: близнец с таблицей дал код $rc: $(cat "$WORK/err")"
ok

# ── F. проба образцов ────────────────────────────────────────────────────────
require_helm
require_mikefarah_yq
samples="$(render_chain_samples)" || fail "F: перечень образцов не прочитан"
count="$(printf '%s\n' "$samples" | grep -c .)"
echo "образцов в каталоге: $count ($(printf '%s' "$samples" | tr '\n' ' '))"
rendered=0
for s in $samples; do
  args="$(render_chain_args prod . "$s")" || fail "F: обёртка отказала на образце $s"
  # shellcheck disable=SC2086
  (cd "$UMBRELLA" && helm template "$RELEASE" . -n kacho $args) >/dev/null 2>"$WORK/err" && HELM_RC=0 || HELM_RC=$?
  RENDERS=$((RENDERS + 1))
  HELM_ERR="$(grep -vE 'WARNING: Kubernetes configuration' "$WORK/err" || true)"
  render_or_fatal "prod + образец $s"
  echo "  prod отрендерен с образцом $s (код 0)"
  ok

  user="$(yq -r '.global.kacho.identity.smtp.connectionURI // ""' "$SAMPLES/$s")" \
    || fatal "F: адрес образца $s не прочитан"
  user="${user#*://}"; case "$user" in *@*) user="${user%%@*}@" ;; *) user="" ;; esac
  # Адрес формы `values.dev.yaml` — приёмник ЭТОГО релиза. Имя пользователя
  # образца сохраняется: меняется ровно один факт — узел.
  # shellcheck disable=SC2086
  (cd "$UMBRELLA" && helm template "$RELEASE" . -n kacho $args \
      --set-string "global.kacho.identity.smtp.connectionURI=smtp://${user}{{ .Release.Name }}-mailpit:1025/") \
      >/dev/null 2>"$WORK/err" && HELM_RC=0 || HELM_RC=$?
  RENDERS=$((RENDERS + 1)); HELM_ERR="$(cat "$WORK/err")"
  render_must_fail_because "приёмник \"$RELEASE-mailpit\"" "prod + $s, адрес формы values.dev.yaml" \
    "F: образец $s с адресом приёмника стенда принят рендером prod — проба не судит форму образца"
  ok

  cred="$(yq -r '.global.kacho.identity.smtp.credentialSecret.name // ""' "$SAMPLES/$s")" \
    || fatal "F: удостоверение образца $s не прочитано"
  if [ -n "$cred" ]; then
    # shellcheck disable=SC2086
    (cd "$UMBRELLA" && helm template "$RELEASE" . -n kacho $args \
        --set "global.kacho.identity.smtp.credentialSecret=null") \
        >/dev/null 2>"$WORK/err" && HELM_RC=0 || HELM_RC=$?
    RENDERS=$((RENDERS + 1)); HELM_ERR="$(cat "$WORK/err")"
    render_must_fail_because "ИСТОЧНИК удостоверения не объявлен" "prod + $s без credentialSecret" \
      "F: образец $s без половины пары принят рендером prod"
    ok
  fi
  rendered=$((rendered + 1))
done
[ "$rendered" -eq "$count" ] || fail "F: отрендерено $rendered образцов из $count"

findings_verdict "цепочек не prod в контракте: $others; образцов: $count"
exit 0
