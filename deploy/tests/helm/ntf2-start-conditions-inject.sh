#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ntf2-start-conditions-inject.sh — доказательство того, что проба условий
# начала NTF-2 (`ntf2-start-conditions.sh`, полоса D0 маршрута issue-2917;
# замысел З29, приёмка NTF-2 Р16) СПОСОБНА различить свои исходы: меняет вывод
# п.5 на слое цепочки `dev`, говорит «не выполнилось» с причиной на отказе
# обёртки, а не «условие не выполнено», и молчит об этом на законном близнеце.
#
# ЗАЧЕМ КОПИЯ ДЕРЕВА ПОД git. Проба меряет РЕВИЗИЮ `<B>` (`git ls-tree`,
# `git show <B>:…` — так команды записаны в Р16), а рендер берёт с диска и
# требует, чтобы диск совпадал с `<B>`. Инъекция поэтому — КОММИТ в отдельной
# копии (`git clone --shared`, объекты общие, ссылки исходного репозитория не
# трогаются), а не правка рабочего дерева: правка без коммита дала бы
# «диск ≠ <B>» вместо утверждаемого исхода. Испытуемые файлы (проба и образец)
# кладутся в копию из РАБОЧЕГО дерева — доказательство судит то, что лежит на
# диске, а не прошлую ревизию. Собранные зависимости зонтика (`charts/*.tgz`,
# вне git) в копию подставляются ссылками.
#
# ПОРЯДОК НЕСУЩИЙ: сначала проверяется вся фикстура (инструменты, общий читатель
# таблицы, обёртка, зависимости зонтика, копия под git), и только потом —
# наличие испытуемого. Обратный порядок дал бы сломанной фикстуре выдать себя за
# отсутствующую пробу.
#
# Случаи:
#   C. контроль: копия как есть — у пп.1–5 исход «выполнено» либо «не выполнено»
#      (не «не выполнилось»); ревизия напечатана и равна HEAD копии; строка
#      цепочки `prod` с образцом `ntf2-p7.yaml` последним; цепочки `dev` и
#      `fe3455` — без образца; контроль п.5 (один `values.yaml` — три пустых
#      поля); п.6 и K напечатаны; итог — семь исходов.
#   I1. в цепочку `dev` добавлен слой, переопределяющий `connectionURI`
#      (CX2-64 (б)) — вывод п.5 меняется: первое поле — значение слоя, исход —
#      «не выполнено»; слой виден в напечатанной цепочке `dev`.
#   I2. таблицы цепочек нет — п.2 и п.5 «не выполнилось» с текстом отказа
#      таблицы, а не «не выполнено». Близнец — контроль C (таблица на месте).
#   I3. образца `ntf2-p7.yaml` в каталоге нет — п.2 «не выполнилось» с текстом
#      отказа обёртки «не файл каталога образцов». Близнец — контроль C.
#   I4. каталога зонтика нет — п.2 «не выполнилось» с текстом отказа обёртки
#      «каталог обязателен». Близнец — контроль C.
#
# Исходы — по контракту `outcome.sh`: 0 зелёный, 1 находка, 2 условие не создано.
set -uo pipefail

INJECT_DIR="$(cd "$(dirname "$0")" && pwd)" || { echo "FATAL: каталог доказательства не разрешается" >&2; exit 2; }
DEPLOY="$(cd "$INJECT_DIR/../.." && pwd)" || { echo "FATAL: каталог deploy не разрешается" >&2; exit 2; }
PROBE_REL="deploy/tests/helm/ntf2-start-conditions.sh"
SAMPLE_REL="deploy/testdata/mail-node/ntf2-p7.yaml"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$INJECT_DIR/outcome.sh" || { echo "FATAL: библиотека исходов не подключилась" >&2; exit 2; }

# ── ФИКСТУРА ─────────────────────────────────────────────────────────────────
require_helm
require_mikefarah_yq
command -v git >/dev/null 2>&1 || fatal "git не найден — копию дерева под git собрать нечем"
command -v go >/dev/null 2>&1 || fatal "go не найден — пины модулей (п.1) пробе разрешать нечем"
require_file_present "$INJECT_DIR/stacks.sh" "общий читатель таблицы стендов"
require_file_present "$INJECT_DIR/lib/render-chain.sh" "шелл-обёртка рендера цепочек NTF1-D9"
require_file_present "$DEPLOY/stacks.txt" "таблица стендов"
# shellcheck source=deploy/tests/helm/premise.sh
. "$INJECT_DIR/premise.sh" || fatal "библиотека предпосылки не подключилась"
premise_chart_deps "ntf2-start-conditions-inject"

REPO="$(git -C "$DEPLOY" rev-parse --show-toplevel 2>/dev/null)" || fatal "дерево не под git — ревизию копии взять неоткуда"
HEAD_SHA="$(git -C "$REPO" rev-parse --verify 'HEAD^{commit}' 2>/dev/null)" || fatal "HEAD не разрешается"

WORK="$(mktemp -d)" || fatal "временный каталог не создан"
trap 'rm -rf "$WORK"' EXIT

# make_copy <имя> — копия дерева на HEAD с испытуемыми файлами из рабочего
# дерева и ссылками на собранные зависимости зонтика; печатает путь копии.
make_copy() {
  local dst="$WORK/$1" f rel
  git clone -q --shared --no-checkout "$REPO" "$dst" >/dev/null 2>"$WORK/clone.err" \
    || { echo "копия $1 не собрана: $(cat "$WORK/clone.err")" >&2; return 2; }
  git -C "$dst" checkout -q --detach "$HEAD_SHA" 2>"$WORK/clone.err" \
    || { echo "копия $1 не выведена на $HEAD_SHA: $(cat "$WORK/clone.err")" >&2; return 2; }
  for rel in "$PROBE_REL" "$SAMPLE_REL"; do
    if [ -f "$REPO/$rel" ]; then
      mkdir -p "$dst/$(dirname "$rel")" && cp "$REPO/$rel" "$dst/$rel" || return 2
    fi
  done
  for f in "$DEPLOY/helm/umbrella/charts/"*; do
    [ -e "$dst/deploy/helm/umbrella/charts/${f##*/}" ] || ln -s "$f" "$dst/deploy/helm/umbrella/charts/${f##*/}"
  done
  printf '%s\n' "$dst"
}

# commit_copy <копия> <сообщение> — все правки копии одним коммитом: проба
# требует, чтобы диск совпадал с ревизией.
commit_copy() {
  if ! git -C "$1" add -A >/dev/null 2>"$WORK/commit.err"; then
    echo "правки копии не добавлены в индекс: $(cat "$WORK/commit.err")" >&2; return 2
  fi
  if ! git -C "$1" -c user.name=ntf2-inject -c user.email=ntf2-inject@invalid \
         -c core.hooksPath=/dev/null -c commit.gpgsign=false \
         commit -q --no-verify --allow-empty -m "$2" >/dev/null 2>"$WORK/commit.err"; then
    echo "коммит копии не сделан: $(cat "$WORK/commit.err")" >&2; return 2
  fi
}

# run_probe <копия> <файл вывода> — код пробы печатается в файл `.rc`.
run_probe() {
  local rc
  bash "$1/$PROBE_REL" >"$2" 2>&1 && rc=0 || rc=$?
  printf '%s\n' "$rc" >"$2.rc"
}

# outcome_of <файл вывода> <пункт> — исход строки пункта (`п.N:` либо `K:`).
outcome_of() {
  sed -n "s/^$2: \\(выполнено\\|не выполнено\\|не выполнилось\\)\\( —.*\\)\\{0,1\\}\$/\\1/p" "$1" | head -n 1
}

CTL="$(make_copy control)" || fatal "копия контроля не собрана"
commit_copy "$CTL" "контроль: испытуемые файлы рабочего дерева" || fatal "копия контроля не закоммичена"
CTL_HEAD="$(git -C "$CTL" rev-parse HEAD)" || fatal "HEAD копии контроля не разрешается"
ok

# ── ИСПЫТУЕМЫЙ ───────────────────────────────────────────────────────────────
[ -f "$CTL/$PROBE_REL" ] || fail "предмет отсутствует: $PROBE_REL — пробы условий начала NTF-2 в дереве нет"
[ -f "$CTL/$SAMPLE_REL" ] || fail "предмет отсутствует: $SAMPLE_REL — образца П7-узла в каталоге образцов нет"
ok

# ── C. контроль ──────────────────────────────────────────────────────────────
run_probe "$CTL" "$WORK/control.out"
echo "контроль: код пробы $(cat "$WORK/control.out.rc")"
case "$(cat "$WORK/control.out.rc")" in 0|1|2) ;; *) fail "C: код пробы $(cat "$WORK/control.out.rc") вне контракта 0/1/2: $(cat "$WORK/control.out")" ;; esac
grep -qx "ревизия: $CTL_HEAD (диск совпадает с ревизией)" "$WORK/control.out" \
  || fail "C: ревизия копии $CTL_HEAD не напечатана либо диск объявлен несовпадающим: $(grep '^ревизия' "$WORK/control.out")"
ok
for p in п.1 п.2 п.3 п.4 п.5; do
  o="$(outcome_of "$WORK/control.out" "$p")"
  case "$o" in
    выполнено|"не выполнено") echo "  контроль $p: $o"; ok ;;
    *) fail "C: у $p на чистой копии исход «${o:-нет строки}», ожидался «выполнено» либо «не выполнено»: $(grep "^$p:" "$WORK/control.out")" ;;
  esac
done
for p in п.6 K; do
  o="$(outcome_of "$WORK/control.out" "$p")"
  [ -n "$o" ] || fail "C: строки исхода $p нет"
  echo "  контроль $p: $o"; ok
done
grep -qE '^  цепочка prod: строка .* -f [^ ]*/ntf2-p7\.yaml; образец последним: ntf2-p7\.yaml$' "$WORK/control.out" \
  || fail "C: строки цепочки prod с образцом ntf2-p7.yaml последним нет: $(grep 'цепочка prod' "$WORK/control.out")"
ok
for c in dev fe3455; do
  line="$(grep -E "^  файлы цепочки $c \\(stacks\\.sh --chain\\): " "$WORK/control.out")"
  [ -n "$line" ] || fail "C: файлы цепочки $c не напечатаны"
  case "$line" in *ntf2-p7*) fail "C: в цепочке $c образец: $line" ;; esac
  ok
done
grep -qE '^  контроль п\.5: один values\.yaml → « \|  \|  \| [^ ]+» — три пустых поля$' "$WORK/control.out" \
  || fail "C: контроль п.5 (один values.yaml — три пустых поля) не напечатан: $(grep 'контроль п.5' "$WORK/control.out")"
ok
grep -qE '^итог: выполнено [0-9]+ · не выполнено [0-9]+ · не выполнилось [0-9]+ \(исходов 7\)$' "$WORK/control.out" \
  || fail "C: итог не называет семь исходов: $(grep '^итог' "$WORK/control.out")"
ok
CTL_P5="$(grep '^п.5:' "$WORK/control.out")"

# ── I1. слой в цепочке dev, переопределяющий connectionURI ──────────────────
INJ_URI="smtp://ntf2-inject-relay.example:2525/"
INJ_LAYER="values.ntf2-inject-dev.yaml"
I1="$(make_copy i1)" || fatal "копия I1 не собрана"
dev_chain="$(bash "$I1/deploy/tests/helm/stacks.sh" --chain dev ,)" || fatal "I1: цепочка dev не прочитана"
printf 'global:\n  kacho:\n    identity:\n      smtp:\n        connectionURI: "%s"\n' "$INJ_URI" \
  >"$I1/deploy/helm/umbrella/$INJ_LAYER" || fatal "I1: слой не записан"
awk -v want="dev:$dev_chain" -v repl="dev:$dev_chain,$INJ_LAYER" '$0 == want {print repl; hit=1; next} {print} END {exit !hit}' \
  "$I1/deploy/stacks.txt" >"$WORK/stacks.i1" || fatal "I1: строка dev в таблице не найдена"
cp "$WORK/stacks.i1" "$I1/deploy/stacks.txt" || fatal "I1: таблица не записана"
commit_copy "$I1" "I1: слой dev переопределяет connectionURI" || fatal "I1: копия не закоммичена"
run_probe "$I1" "$WORK/i1.out"
I1_P5="$(grep '^п.5:' "$WORK/i1.out")"
[ "$I1_P5" != "$CTL_P5" ] || fail "I1: вывод п.5 не изменился от слоя dev: $I1_P5"
case "$I1_P5" in "п.5: не выполнено — поля: $INJ_URI | "*) ;; *) fail "I1: п.5 не назвал адрес слоя и не дал «не выполнено»: $I1_P5" ;; esac
grep -qE "^  файлы цепочки dev \\(stacks\\.sh --chain\\): .*$INJ_LAYER" "$WORK/i1.out" \
  || fail "I1: слой не виден в напечатанной цепочке dev: $(grep 'файлы цепочки dev' "$WORK/i1.out")"
echo "I1: контроль «$CTL_P5» → инъекция «$I1_P5»"
ok

# ── I2–I4. отказ обёртки — «не выполнилось» с текстом причины ────────────────
# refusal_case <имя> <ожидаемая подстрока п.2> <правка копии…>
refusal_case() {
  local name="$1" want="$2" dst o
  shift 2
  dst="$(make_copy "$name")" || fatal "$name: копия не собрана"
  (cd "$dst" && "$@") || fatal "$name: правка копии не применена"
  commit_copy "$dst" "$name" || fatal "$name: копия не закоммичена"
  run_probe "$dst" "$WORK/$name.out"
  o="$(outcome_of "$WORK/$name.out" п.2)"
  [ "$o" = "не выполнилось" ] || fail "$name: п.2 дал «${o:-нет строки}», ожидалось «не выполнилось»: $(grep '^п.2:' "$WORK/$name.out")"
  grep -q "^п.2: не выполнилось — .*$want" "$WORK/$name.out" \
    || fail "$name: п.2 «не выполнилось» без причины «$want»: $(grep '^п.2:' "$WORK/$name.out")"
  echo "$name: $(grep '^п.2:' "$WORK/$name.out" | cut -c1-200)"
  ok
}
refusal_case I2-table "таблица стеков" rm -f deploy/stacks.txt
o="$(outcome_of "$WORK/I2-table.out" п.5)"
[ "$o" = "не выполнилось" ] || fail "I2-table: п.5 без таблицы дал «${o:-нет строки}», ожидалось «не выполнилось»"
ok
refusal_case I3-sample "не файл каталога образцов" rm -f "$SAMPLE_REL"
refusal_case I4-umbrella "каталог обязателен" rm -rf deploy/helm/umbrella
# Близнец I2–I4 — контроль C: та же копия с таблицей, образцом и зонтиком на
# месте дала у п.2 «выполнено» либо «не выполнено» (утверждено выше).

findings_verdict "копий дерева: контроль и инъекций 4 (I1–I4)"
exit 0
