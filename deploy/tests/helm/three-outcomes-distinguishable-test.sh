#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# three-outcomes-distinguishable-test.sh — НЕНУЛЕВОЙ КОД ОБЯЗАН ПРИЙТИ С ТЕКСТОМ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ЛОВИТ — НА КОНКРЕТНОМ СЛУЧАЕ
#
# Свежий клон: `helm/umbrella/charts/*.tgz` в git не лежат. В этом состоянии
# ШЕСТЬ скриптов каталога отдавали ненулевой код при НУЛЕ БАЙТ вывода (#1195;
# седьмой закрыт раньше — #1187). Механика у всех одна: `DOC="$(render …)"`
# внутри подстановки под `set -e`, а `render` гасила stderr. helm отказывал по
# причине, НЕ относящейся к предмету проверки, прогон умирал на первом же
# рендере, и наблюдаемый результат у трёх РАЗНЫХ состояний совпадал:
#
#   • гейт нашёл дефект в дереве          → надо чинить дерево;
#   • гейт сам сломан                     → надо чинить гейт;
#   • условие для гейта не создано        → надо собрать зависимости.
#
# Читатель шёл искать дефект в дереве, которого там нет. Это третья категория
# исхода — «не выполнилось», — поданная как красный вердикт (`e2e-flow.md` §1).
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ГЕЙТ, А НЕ ШЕСТЬ ПОЧИНЕННЫХ ЭКЗЕМПЛЯРОВ
#
# Починка шести закрывает находку и оставляет класс: седьмой скрипт напишут
# завтра, и он не покраснеет ничем. Предикат, которым класс замерен в #1195,
# исполняется здесь как проверка — и заодно перестаёт быть разрушительным:
# та его редакция СНИМАЛА `charts/*.tgz` в живом дереве, а этот гейт строит
# ЗЕРКАЛО (символьные ссылки на всё, кроме умбреллы; умбрелла — копия без
# архивов) и живой рабочей копии не касается вовсе.
#
# ─────────────────────────────────────────────────────────────────────────────
# ДВА УТВЕРЖДЕНИЯ, И У НИХ РАЗНЫЕ ПОПУЛЯЦИИ
#
#   1. КАЖДЫЙ скрипт каталога: ненулевой код ⇒ вывод НЕПУСТ. Это дословно
#      предикат снятия #1195.
#   2. Скрипт, ОБЪЯВИВШИЙ контракт (сорсит `outcome.sh`): на этом состоянии
#      обязан отдать РОВНО код 2 и передать ТЕКСТ, который сказал сам helm.
#      Объявление читается по ИСПОЛНЯЕМОЙ строке, а не по слову: у каждого
#      потребителя строкой выше стоит комментарий `# shellcheck source=…`, и
#      предикат по тексту засчитал бы за объявление его.
#
# Популяция (2) растёт по мере того, как скрипты переходят на общую реализацию;
# популяция (1) — весь каталог сразу. Скрипты, которые сегодня о причине
# ГОВОРЯТ, но кодом 1, гейт называет числом и именами: они не молчат, поэтому
# находкой не являются, но и контракта пока не исполняют.
#
# ─────────────────────────────────────────────────────────────────────────────
# СЛЕПАЯ ЗОНА НАЗВАНА, А НЕ УМОЛЧАНА
#
# Скрипты, которые материализуют зависимости САМИ, из обхода исключены: иначе
# каждый тянул бы полный `helm dep update` по сети (замер владельца операции:
# 22 объявления, 3 мин 22 с), и минутная проверка стала бы получасовой. Дефекта
# этого класса у них нет by construction — они условие себе создают, — но если
# у такого скрипта сорвётся сама материализация, отвечать за категорию будет
# `scripts/helm-umbrella-deps.sh` (у него код 3 для «источник не ответил»).
# Их имена и число печатаются, чтобы исключение было видно, а не подразумевалось.
#
# Самопроверка: --self-test (инъекции в обе стороны; идут в ЗЕРКАЛО, живой
# рабочей копии не касаются).
set -euo pipefail

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=2

require_helm

# Каталог, откуда берётся ПОПУЛЯЦИЯ. Переопределяется только самопроверкой —
# ей надо подложить синтетические скрипты, не трогая дерево.
SCRIPT_DIR="${THREE_OUTCOMES_SCRIPT_DIR:-$HERE}"

# ── Зеркало дерева БЕЗ собранных зависимостей ────────────────────────────────
# Всё, кроме умбреллы, — символьные ссылки: ноль копирования и ноль расхождения
# с деревом. Умбрелла копируется (65 файлов, ~1 МБ) по одной причине: helm
# ходит по каталогу чарта и на каждую ссылку внутри него печатает
# `walk.go: found symbolic link`, а этот шум попал бы в текст, который гейт
# читает как «что сказал helm».
# ═════════════════════════════════════════════════════════════════════════════
# САМОПРОВЕРКА — гейт обязан краснеть на внесённом дефекте и МОЛЧАТЬ на законном
# близнеце той же формы. Гейт, доказанный с одной стороны, ловит форму, а не
# существо, и его снимут при первом ложном срабатывании.
#
# Инъекции идут в СИНТЕТИЧЕСКУЮ популяцию (временный каталог со ссылками на
# настоящие скрипты плюс подложенные), живой рабочей копии самопроверка не
# касается вовсе.
# ═════════════════════════════════════════════════════════════════════════════
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: молчание ловится, законный близнец молчит ==="
  st_rc=0
  st_checked=0
  ST="$(mktemp -d)"
  trap 'rm -rf "$ST"' EXIT

  st_pop() { # st_pop <каталог> [синтетика…] — популяция = дерево + подложенное
    local dir="$1"; shift
    mkdir -p "$dir"
    local f
    for f in "$HERE"/*-test.sh; do ln -sfn "$f" "$dir/$(basename "$f")"; done
    for f in "$@"; do cp "$f" "$dir/$(basename "$f")"; done
  }

  st_probe() { # st_probe <метка> <ожидаемый код> <обязательная подстрока> <каталог>
    local label="$1" want_rc="$2" want_txt="$3" dir="$4" out rc
    st_checked=$((st_checked + 1))
    out="$(THREE_OUTCOMES_SCRIPT_DIR="$dir" bash "$0" 2>&1)" && rc=0 || rc=$?
    if [ "$rc" -ne "$want_rc" ]; then
      echo "  ✗ $label — код $rc, ожидался $want_rc"
      printf '%s\n' "$out" | sed 's/^/      /'
      st_rc=1; return
    fi
    case "$out" in
      *"$want_txt"*) echo "  ✓ $label — код $rc, назван «$want_txt»" ;;
      *) echo "  ✗ $label — код $rc верен, но в выводе нет «$want_txt»:"
         printf '%s\n' "$out" | sed 's/^/      /'
         st_rc=1 ;;
    esac
  }

  # ── синтетические скрипты: ровно те формы, которые гейт различает ──────────
  mk() { cat >"$ST/$1"; }

  # (A) ДЕФЕКТ #1195 дословно: подстановка под `set -e`, stderr погашен.
  mk zz-silent-test.sh <<'SYN'
#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DOC="$(helm template x "$ROOT/helm/umbrella" -f "$ROOT/helm/umbrella/values.dev.yaml" \
  --show-only templates/namespace.yaml 2>/dev/null)"
echo "$DOC"
SYN

  # (B) ЗАКОННЫЙ БЛИЗНЕЦ той же формы: тот же отказ, но он НАЗВАН. Контракта не
  #     объявляет, поэтому код 1 для него законен — гейт обязан молчать.
  mk zz-speaks-test.sh <<'SYN'
#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
if ! DOC="$(helm template x "$ROOT/helm/umbrella" -f "$ROOT/helm/umbrella/values.dev.yaml" \
  --show-only templates/namespace.yaml 2>&1)"; then
  echo "FAIL: рендер не удался: $DOC"
  exit 1
fi
SYN

  # (C) ВТОРОЙ ЗАКОННЫЙ БЛИЗНЕЦ: слово `outcome.sh` стоит в КОММЕНТАРИИ. Гейт,
  #     читающий текст вместо исполняемой строки, счёл бы это объявлением
  #     контракта и потребовал бы кода 2 — то есть покраснел бы вхолостую.
  mk zz-mentions-test.sh <<'SYN'
#!/usr/bin/env bash
set -euo pipefail
# Здесь могло бы стоять: . "$(dirname "$0")/outcome.sh" — но не стоит.
echo "FAIL: рендер не удался (причина названа)"
exit 1
SYN

  # (D) ОБЪЯВИЛ КОНТРАКТ И НЕ ИСПОЛНИЛ: отдаёт код 1 там, где условие не создано.
  mk zz-contract-code1-test.sh <<'SYN'
#!/usr/bin/env bash
set -euo pipefail
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=1
fail "условие не создано, но названо находкой о дереве"
SYN

  # (E) ОБЪЯВИЛ КОНТРАКТ, КОД ВЕРНЫЙ, А ТЕКСТА helm НЕТ. Без текста читатель
  #     получает голое «render failed» и идёт искать дефект в дереве.
  mk zz-contract-notext-test.sh <<'SYN'
#!/usr/bin/env bash
set -euo pipefail
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=1
fatal "рендер не удался"
SYN

  echo
  echo "-- законный вход: популяция дерева как есть --"
  st_pop "$ST/asis"
  st_probe "дерево как есть → зелено" 0 "PASS: $SCRIPT" "$ST/asis"

  echo
  echo "-- законные близнецы: отказ НАЗВАН (код 1 без объявленного контракта) --"
  st_pop "$ST/twins" "$ST/zz-speaks-test.sh" "$ST/zz-mentions-test.sh"
  st_probe "близнецы → зелено" 0 "PASS: $SCRIPT" "$ST/twins"

  echo
  echo "-- инъекция: молчаливый отказ (дефект #1195) --"
  st_pop "$ST/silent" "$ST/zz-silent-test.sh"
  st_probe "молчит → находка с именем" 1 "zz-silent-test.sh" "$ST/silent"

  echo
  echo "-- инъекция: контракт объявлен, отдан код 1 --"
  st_pop "$ST/c1" "$ST/zz-contract-code1-test.sh"
  st_probe "контракт не исполнен → находка с именем" 1 "zz-contract-code1-test.sh" "$ST/c1"

  echo
  echo "-- инъекция: контракт объявлен, текста helm нет --"
  st_pop "$ST/cn" "$ST/zz-contract-notext-test.sh"
  st_probe "нет текста helm → находка с именем" 1 "zz-contract-notext-test.sh" "$ST/cn"

  echo
  echo "-- предпосылка гейта: пустая популяция НЕ является зелёным вердиктом --"
  mkdir -p "$ST/empty"
  st_probe "популяция пуста → условие не создано" 2 "НОЛЬ скриптов" "$ST/empty"

  echo
  echo "случаев проверено: $st_checked (законных 2, инъекций 3, предпосылка 1)"
  [ "$st_checked" -eq 6 ] || { echo "FAIL: исполнено $st_checked случаев из 6"; st_rc=1; }
  [ $st_rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $st_rc
fi

mirror_except() { # <src> <dst> <пропустить…>
  local src="$1" dst="$2"; shift 2
  local e b skip
  mkdir -p "$dst"
  for e in "$src"/* "$src"/.[!.]*; do
    [ -e "$e" ] || continue
    b="$(basename "$e")"
    for skip in "$@"; do [ "$b" = "$skip" ] && continue 2; done
    ln -sfn "$e" "$dst/$b"
  done
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mirror_except "$REPO_ROOT/.." "$WORK" deploy
mirror_except "$REPO_ROOT" "$WORK/deploy" helm tests
mirror_except "$REPO_ROOT/tests" "$WORK/deploy/tests" helm
# Оснастка каталога (outcome.sh, stacks.sh, .py-помощники) обязана быть рядом
# даже когда популяция переопределена: потребитель сорсит её по `dirname "$0"`.
mirror_except "$HERE" "$WORK/deploy/tests/helm"
if [ "$SCRIPT_DIR" != "$HERE" ]; then
  rm -f "$WORK/deploy/tests/helm/"*-test.sh   # популяцию задаёт вызывающий
  mirror_except "$SCRIPT_DIR" "$WORK/deploy/tests/helm"
fi
mirror_except "$REPO_ROOT/helm" "$WORK/deploy/helm" umbrella
cp -r "$REPO_ROOT/helm/umbrella" "$WORK/deploy/helm/umbrella"
rm -f "$WORK/deploy/helm/umbrella/charts/"*.tgz
[ -d "$WORK/deploy/helm/umbrella/charts" ] \
  || fatal "в зеркале нет каталога сабчартов — зеркало не собралось, судить не о чем"
if compgen -G "$WORK/deploy/helm/umbrella/charts/*.tgz" >/dev/null; then
  fatal "в зеркале остались собранные зависимости — состояние свежего клона не воспроизведено"
fi

# executable_lines <файл> — без строк-комментариев. Гейт, читающий текст,
# засчитал бы за объявление контракта соседний `# shellcheck source=…`.
executable_lines() { sed -e 's/[[:space:]]#[[:space:]].*$//' -e '/^[[:space:]]*#/d' "$1"; }

# ОБА предиката читают тело ЧЕРЕЗ ПЕРЕМЕННУЮ, а не через трубу. `sed … | grep -q`
# под `set -o pipefail` возвращает ОТКАЗ НА СОВПАДЕНИИ: grep выходит по первому
# попаданию, sed получает SIGPIPE, и `pipefail` поднимает ЕГО статус до статуса
# конвейера — а произойдёт это или нет, решает гонка между процессами. Наблюдалось
# здесь же: два прогона подряд на НЕИЗМЕННОМ дереве насчитали 6 и 4
# материализующих зависимости скрипта. Задача #658.
declares_contract() { # сорсит ли скрипт общую реализацию исходов
  # Путь несёт собственную подстановку с пробелом (`. "$(dirname "$0")/outcome.sh"`),
  # поэтому «слово без пробелов» здесь не годится: первая редакция предиката
  # насчитала НОЛЬ объявивших на шести объявивших, и гейт честно отказался
  # судить по вакуумной популяции — своей же проверкой предпосылки.
  local body; body="$(executable_lines "$1")"
  grep -qE '(^|[[:space:]])(\.|source)[[:space:]].*outcome\.sh' <<<"$body"
}
builds_deps() { # материализует ли зависимости САМ
  local body; body="$(executable_lines "$1")"
  grep -qE 'helm[[:space:]]+dep|helm-umbrella-deps' <<<"$body"
}

# helm_text_relayed <вывод> — вывод несёт блок «что сказал helm», и блок НЕ пуст.
# Требуется именно блок, а не подстрока из сообщения helm: привязка к его
# формулировке сделала бы гейт красным на смене версии helm, то есть по причине,
# к дереву отношения не имеющей.
helm_text_relayed() {
  local out="$1" inside=0 line
  case "$out" in *"--- helm отказал:"*) ;; *) return 1 ;; esac
  while IFS= read -r line; do
    case "$line" in
      "--- helm отказал:"*) inside=1; continue ;;
      "--- конец текста helm ---") inside=0; continue ;;
    esac
    if [ "$inside" = 1 ] && [ -n "$line" ] && [ "$line" != "(helm не сказал ничего)" ]; then
      return 0
    fi
  done <<<"$out"
  return 1
}

# ── Обход ────────────────────────────────────────────────────────────────────
scanned=0; builders=0; contract=0
n_green=0; n_two=0; n_one=0; n_silent=0
silent_names=""; builder_names=""; one_names=""; contract_bad=""

# nullglob: без него пустая популяция даёт ЛИТЕРАЛЬНУЮ строку `*-test.sh` как
# имя файла, и гейт «осматривает» один несуществующий скрипт вместо того, чтобы
# сказать «ноль». Проверка собственной предпосылки не должна зависеть от того,
# развернулся ли шаблон имени.
shopt -s nullglob
for t in "$WORK/deploy/tests/helm/"*-test.sh; do
  b="$(basename "$t")"
  [ "$b" = "$SCRIPT" ] && continue          # себя не гоняем: рекурсия
  if builds_deps "$t"; then
    builders=$((builders + 1)); builder_names="$builder_names $b"; continue
  fi
  scanned=$((scanned + 1))
  out="$(cd "$WORK/deploy" && timeout 300 bash "$t" 2>&1)" && rc=0 || rc=$?
  if [ "$rc" -eq 0 ]; then
    n_green=$((n_green + 1))
  elif [ -z "$out" ]; then
    n_silent=$((n_silent + 1)); silent_names="$silent_names $b"
  elif [ "$rc" -eq 2 ]; then
    n_two=$((n_two + 1))
  else
    n_one=$((n_one + 1)); one_names="$one_names $b"
  fi
  if declares_contract "$t"; then
    contract=$((contract + 1))
    if [ "$rc" -ne 2 ]; then
      contract_bad="$contract_bad $b(код=$rc)"
    elif ! helm_text_relayed "$out"; then
      contract_bad="$contract_bad $b(нет текста helm)"
    fi
  fi
done

# ── Объём осмотренного печатается ВСЕГДА ─────────────────────────────────────
# «Ноль находок» обязано быть отличимо от «ноль прочитанного».
echo "  перепись зеркала (зависимости не собраны): скриптов осмотрено $scanned;"
echo "    зелены без зависимостей $n_green · отказали кодом 2 $n_two · отказали кодом 1, но С ТЕКСТОМ $n_one · МОЛЧА $n_silent"
echo "    объявили общий контракт исходов: $contract"
echo "    исключены как материализующие зависимости сами ($builders):$builder_names"
[ -n "$one_names" ] && echo "    говорят о причине, но кодом 1 (контракт не объявлен):$one_names"

# ── Предпосылки САМОГО гейта ─────────────────────────────────────────────────
[ "$scanned" -gt 0 ] || fatal "обход дал НОЛЬ скриптов — судить не о чем; зеркало собрано неверно"
[ "$contract" -gt 0 ] || fatal "ни один скрипт не объявил общий контракт исходов — утверждение 2 было бы вакуумным"

# ── 1. Ненулевой код ⇒ вывод непуст (предикат снятия #1195) ──────────────────
[ "$n_silent" -eq 0 ] || fail "молча отказывают ($n_silent):$silent_names — \
ненулевой код при НУЛЕ БАЙТ вывода неотличим от находки о дереве (#1195)"
ok

# ── 2. Объявивший контракт отдаёт код 2 и ТЕКСТ helm ────────────────────────
[ -z "$contract_bad" ] || fail "объявили контракт исходов, но не исполнили его:$contract_bad"
ok

outcome_verdict "популяция утверждения 2: $contract из $scanned"
