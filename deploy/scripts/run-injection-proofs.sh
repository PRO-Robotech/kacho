#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ГЕЙТ НАД ДОКАЗАТЕЛЬСТВАМИ: каждое доказательство инъекцией обязано ИСПОЛНЯТЬСЯ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ
#
# Доказательство инъекцией — единственное, что отличает работающий гейт от гейта,
# ПОТЕРЯВШЕГО способность краснеть. Само по себе присутствие такого скрипта в
# дереве не гарантирует ничего: он может сломаться, устареть вместе с фикстурой
# или начать зеленеть вхолостую — и это будет незаметно ровно потому, что его
# никто не запускает.
#
# Класс наблюдался в этом дереве дважды за сутки: гейт, зелёный и ЛЖИВЫЙ
# (распознаватель не видел стандартной формы записи), и гейт, чьё утверждение
# было верно только из-за узости популяции. Оба нашлись потому, что кто-то
# прогнал инъекцию РУКАМИ.
#
# Соседний `run-gate-self-tests.sh` держит ту же норму для доказательств, живущих
# ВЕТКОЙ `--self-test` внутри самого гейта. Здесь — вторая форма: доказательство
# ОТДЕЛЬНЫМ ФАЙЛОМ. Две формы, два обходчика, одна норма; сводить их в один
# предикат нельзя — у форм разные признаки (разбор аргумента против имени файла)
# и разные способы запуска.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПЕРЕЧЕНЬ ВЫВОДИТСЯ ИЗ ДЕРЕВА, А НЕ ВЫПИСЫВАЕТСЯ
#
# Выписанный перечень оставил бы следующее заведённое доказательство вне вызова —
# ровно тот дефект, который здесь закрывают. Обход идёт по СОДЕРЖИМОМУ
# РЕПОЗИТОРИЯ (`git ls-files`): это то же множество, что увидит конвейер на
# свежем checkout'е, поэтому вердикт воспроизводим и не зависит от локального
# мусора в рабочем дереве. Объявленный состав (`DECLARED`) существует затем,
# чтобы забытое доказательство было ВИДНО, и сверяется в ОБЕ стороны.
#
# ПРИЗНАК — ФОРМА ИМЕНИ, И ЭТО НАЗВАНО ЧЕСТНО. Признак «исполняемая ветка», как у
# соседа, здесь неприменим: доказательство отдельным файлом — обычный скрипт, у
# него нет ни флага, ни ветки, по которой его можно опознать. Значит предикат
# меряет СОГЛАШЕНИЕ ОБ ИМЕНОВАНИИ, а такое соглашение обязано держаться с ДВУХ
# сторон — иначе доказательство, названное иначе, тихо выпадет из обхода. Вторая
# сторона — зеркальная перепись ниже: файл, у которого «inject» в имени есть, а
# формы доказательства нет, обязан быть ОПОЗНАН явно.
#
# Формы доказательства две, и обе живые: `<тема>-inject.sh` (десять файлов) и
# `<тема>_inject.sh` (один, `gateway/deploy/revocation_authority_inject.sh` —
# подчёркивание там от соседних Go-файлов пакета). Форма `inject-<тема>.sh`
# доказательством НЕ является: так называются внесения дефектов в ЖИВОЙ стенд, и
# запускать их обходом было бы разрушительно.
#
# ─────────────────────────────────────────────────────────────────────────────
# ИСХОДОВ ТРИ
#
#   0 — все доказательства зелены;
#   1 — доказательство ПРОВАЛЕНО либо состав разошёлся с объявленным. Гейт, чьё
#       доказательство красное, не доказал, что умеет краснеть на дефекте, — и
#       его зелёный обычный проход ничего не значит;
#   2 — УСЛОВИЕ НЕ СОЗДАНО: нет helm, не материализованы зависимости умбреллы,
#       доказательство отказалось этим же кодом. Не вердикт о дереве и в зачёт
#       «прошло» не идёт (`e2e-flow.md` §1).
#
# Самопроверка: --self-test (инъекции в обе стороны, в синтетическое дерево).
set -uo pipefail

# ── Помощники без труб ───────────────────────────────────────────────────────
# `… | grep -q` под `set -o pipefail` возвращает ОТКАЗ НА СОВПАДЕНИИ: grep выходит
# по первому попаданию, писатель получает SIGPIPE, и статус конвейера берётся от
# него. Задача #658.
line_in() { [[ $'\n'"$1"$'\n' == *$'\n'"$2"$'\n'* ]]; }

DEPLOY_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_ROOT/.." && pwd)"

# Объявленный состав, путями от корня репозитория. Держится СИНХРОННЫМ с находкой
# ниже — расхождение в любую сторону роняет проверку.
DECLARED="
deploy/load-tests/restart-verdict-inject.sh
deploy/scripts/deps-failure-class-inject.sh
deploy/tests/helm/identity-hook-credential-source-inject.sh
deploy/tests/helm/identity-mail-lane-guard-inject.sh
deploy/tests/helm/identity-mail-lane-runtime-inject.sh
deploy/tests/helm/machine-credential-posture-inject.sh
deploy/tests/helm/outcome-contract-inject.sh
gateway/deploy/revocation_authority_inject.sh
scripts/ci-local-outcome-inject.sh
scripts/hooks/install-inject.sh
scripts/hooks/prepush-groups-inject.sh
scripts/hooks/prepush-range-inject.sh
services/nlb/tests/newman/scripts/selftest-assertions-inject.sh
"

# ВЕДОМОСТЬ ЗЕРКАЛЬНОЙ ПЕРЕПИСИ: файлы, у которых «inject» в имени есть, а формы
# доказательства нет. Каждая запись несёт ПРЕДМЕТ (почему это не доказательство);
# предикат снятия у неё общий и механический — запись, которой больше нечего
# исключать (файла нет либо он переименован в форму доказательства), объявляется
# находкой. Пустая ведомость — законное состояние и НЕ поломка.
NOT_A_PROOF="
deploy/scripts/inject-admin-hop-defects.sh|вносит дефекты в ЖИВОЙ стенд, а не доказывает гейт; запускается целью admin-hop-injection под стражем контекста
deploy/scripts/run-injection-proofs.sh|это ОБХОДЧИК доказательств, а не доказательство; слово в имени от предмета обхода. Запускать его собой значило бы рекурсию
gateway/scripts/inject-session-cutoff-defects.sh|вносит дефекты в ЖИВОЙ стенд обрыва сессии; запускается вручную при разборе, не обходом
"

# ── Обход ────────────────────────────────────────────────────────────────────
# Принимает корень аргументом — так его можно навести на синтетическое дерево и
# доказать инъекцией, не трогая репозиторий. В синтетическом дереве `git ls-files`
# не работает, поэтому там используется обход файловой системы; для репозитория
# авторитет — версионный контроль.
#
# Сортировка байтовая (LC_ALL=C): в локали по умолчанию точка в начале имени при
# сличении игнорируется, и объявленный список «расходился» бы на одном порядке.
list_candidates() {
  local root="$1"
  if [ "$root" = "$REPO_ROOT" ] && git -C "$root" rev-parse --git-dir >/dev/null 2>&1; then
    git -C "$root" ls-files -z '*.sh'
  else
    ( cd "$root" && find . -type f -name '*.sh' \
        -not -path './.git/*' -not -path '*/node_modules/*' -not -path '*/vendor/*' \
        -printf '%P\0' )
  fi
}

# count_candidates — ОБЪЁМ ОСМОТРЕННОГО. «Ноль находок» обязано быть отличимо от
# «ноль прочитанного», и это относится к самому обходу тоже.
count_candidates() { list_candidates "${1:-$REPO_ROOT}" | tr -dc '\0' | wc -c | tr -d ' '; }

is_proof_name() { # форма ИМЕНИ доказательства: <тема>-inject.sh либо <тема>_inject.sh
  case "$1" in
    *-inject.sh|*_inject.sh) return 0 ;;
    *) return 1 ;;
  esac
}

mentions_inject_name() { # «inject» в имени есть — в любой форме
  case "$1" in *inject*) return 0 ;; *) return 1 ;; esac
}

discover() { # доказательства
  local root="${1:-$REPO_ROOT}" f
  list_candidates "$root" | while IFS= read -r -d '' f; do
    is_proof_name "$(basename "$f")" && echo "$f"
  done | LC_ALL=C sort
}

discover_mirror() { # зеркальная перепись: «inject» в имени, формы доказательства нет
  local root="${1:-$REPO_ROOT}" f b
  list_candidates "$root" | while IFS= read -r -d '' f; do
    b="$(basename "$f")"
    mentions_inject_name "$b" && ! is_proof_name "$b" && echo "$f"
  done | LC_ALL=C sort
}

# Ведомость переопределяется ТОЛЬКО самопроверкой: без этого зеркальную половину
# нельзя прогнать на синтетическом дереве, и «пустая ведомость — не поломка»
# осталось бы утверждением о коде, а не о поведении гейта.
ledger_paths() {
  printf '%s\n' "${INJECTION_PROOFS_LEDGER-$NOT_A_PROOF}" \
    | sed -e '/^[[:space:]]*$/d' -e 's/|.*$//' | LC_ALL=C sort
}

# ── Достижимость из конвейера ────────────────────────────────────────────────
# Этот обходчик закрывает «доказательство лежит и не вызывается». Ровно тот же
# дефект возможен ЭТАЖОМ ВЫШЕ: шаг, зовущий обходчик, снимут — и десять
# доказательств снова станут немыми, молча. Поэтому обходчик требует, чтобы его
# СОБСТВЕННАЯ цель вызывалась из объявлений конвейера, и требует этого от дерева,
# а не от памяти.
#
# Читается ИСПОЛНЯЕМАЯ часть объявления, а не текст: имя цели встречается и в
# объяснениях, и проверка по подстроке зеленела бы на собственном комментарии.
MAKE_TARGET="injection-proofs"
workflow_invokes() { # <корень> <имя цели> — зовёт ли хоть одно объявление конвейера
  local root="$1" target="$2" f body
  for f in "$root"/.github/workflows/*.yml "$root"/.github/workflows/*.yaml; do
    [ -f "$f" ] || continue
    body="$(sed -e 's/[[:space:]]#[[:space:]].*$//' -e '/^[[:space:]]*#/d' "$f")"
    if grep -qE "(^|[^A-Za-z0-9_-])make([[:space:]]+-C[[:space:]]+[^[:space:]]+)?[[:space:]]+${target}([^A-Za-z0-9_-]|\$)" <<<"$body"; then
      return 0
    fi
  done
  return 1
}

# ── ИНЪЕКЦИЯ В ОБХОД ────────────────────────────────────────────────────────
# Идёт ДО предпосылок (helm, зависимости): обход к ним не обращается, а требовать
# инструмент для проверки чистого разбора имён значило бы не иметь возможности
# проверить его там, где инструмента нет.
if [ "${1:-}" = "--self-test" ]; then
  echo "=== обход доказательств инъекцией: инъекции в синтетическое дерево ==="
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  rc=0
  checked=0
  probe() { checked=$((checked + 1)); }

  mkdir -p "$tmp/deploy/tests/helm" "$tmp/deploy/scripts" "$tmp/gateway/deploy" "$tmp/tools"

  # ── ЗАКОННЫЕ ФОРМЫ ДОКАЗАТЕЛЬСТВА — обязаны найтись, в ЛЮБОМ каталоге дерева.
  #    Имена собираются из подстановки, а не пишутся литералом: литерал сделал бы
  #    ЭТОТ файл своей же находкой, и «обходчик себя не находит» держалось бы на
  #    везении.
  sfx='inject'
  printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp/deploy/tests/helm/zz-alpha-$sfx.sh"
  printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp/gateway/deploy/zz_beta_$sfx.sh"
  printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp/tools/zz-gamma-$sfx.sh"
  # ── НЕ ДОКАЗАТЕЛЬСТВА: «inject» в имени есть, формы нет. Обязаны попасть в
  #    зеркальную перепись, а не в состав запускаемых.
  printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp/deploy/scripts/$sfx-zz-defects.sh"
  printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp/deploy/scripts/zz-${sfx}or.sh"
  # ── ПОСТОРОННИЙ: слова в имени нет вовсе. Ни туда, ни туда.
  printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp/deploy/scripts/zz-plain.sh"

  found="$(discover "$tmp")"
  want="deploy/tests/helm/zz-alpha-inject.sh
gateway/deploy/zz_beta_inject.sh
tools/zz-gamma-inject.sh"
  probe
  if [ "$found" = "$want" ]; then
    echo "  ОК  обе формы имени найдены, в трёх РАЗНЫХ каталогах"
  else
    echo "  ПРОВАЛ состав доказательств не тот"
    echo "--- найдено:"; printf '%s\n' "$found" | sed 's/^/      /'
    echo "--- ожидалось:"; printf '%s\n' "$want" | sed 's/^/      /'; rc=1
  fi

  # Зеркало: не-доказательства обязаны быть ОПОЗНАНЫ, а не пропущены молча.
  mirror="$(discover_mirror "$tmp")"
  for f in "deploy/scripts/$sfx-zz-defects.sh" "deploy/scripts/zz-${sfx}or.sh"; do
    probe
    if line_in "$mirror" "$f"; then
      echo "  ОК  $f опознан зеркальной переписью, а не принят за доказательство"
    else
      echo "  ПРОВАЛ $f выпал из обеих переписей — доказательство, названное иначе, стало бы немым"; rc=1
    fi
  done
  probe
  if line_in "$mirror" "deploy/scripts/zz-plain.sh" || line_in "$found" "deploy/scripts/zz-plain.sh"; then
    echo "  ПРОВАЛ посторонний файл попал в перепись"; rc=1
  else
    echo "  ОК  посторонний файл не попал ни в одну перепись"
  fi

  # ЭТОТ ФАЙЛ обязан остаться вне состава: он содержит слово в прозе, в именах
  # функций и в фикстурах — но формы имени доказательства не несёт.
  probe
  if line_in "$(discover)" "deploy/scripts/$(basename "$0")"; then
    echo "  ПРОВАЛ обходчик нашёл САМ СЕБЯ — состав стал бы рекурсивным"; rc=1
  else
    echo "  ОК  обходчик себя не находит"
  fi

  # РАСХОЖДЕНИЕ СОСТАВА видно в ОБЕ стороны — тем же сравнением, что в основном проходе.
  real="$(discover)"
  for kind in extra gone; do
    probe
    case "$kind" in
      extra) other="$(printf '%s\n' "$real" | tail -n +2)"; what="доказательство есть, в списке НЕТ" ;;
      gone)  other="$(printf '%s\n%s\n' "$real" "deploy/scripts/gone-inject.sh" | LC_ALL=C sort)"; what="в списке есть, доказательства НЕТ" ;;
    esac
    if [ "$real" = "$other" ]; then
      echo "  ПРОВАЛ ($kind) сравнение не различает состав"; rc=1
    else
      echo "  ОК  ($kind) расхождение видно: $what"
    fi
  done

  # ВЕДОМОСТЬ ЗЕРКАЛА: запись без предмета — находка; ПУСТАЯ ведомость — не поломка.
  probe
  real_mirror="$(discover_mirror)"
  real_ledger="$(ledger_paths)"
  if [ "$real_mirror" = "$real_ledger" ]; then
    echo "  ОК  ведомость зеркала совпала с деревом ($(printf '%s\n' "$real_ledger" | grep -c .) записей)"
  else
    echo "  ПРОВАЛ ведомость зеркала разошлась с деревом"; rc=1
  fi
  probe
  if [ "$real_mirror" = "$(printf '%s\n%s\n' "$real_ledger" "deploy/scripts/gone-injector.sh" | LC_ALL=C sort)" ]; then
    echo "  ПРОВАЛ запись без предмета не отличается от ведомости"; rc=1
  else
    echo "  ОК  запись ведомости без предмета отличима"
  fi

  # ── ДОСТИЖИМОСТЬ ИЗ КОНВЕЙЕРА: обе стороны ────────────────────────────────
  # Слева — объявление, которое цель зовёт; справа — объявление, где имя цели
  # стоит только в КОММЕНТАРИИ. Без правой стороны проверка ловила бы слово, а не
  # вызов, и зеленела бы на собственном объяснении.
  mkdir -p "$tmp/wfyes/.github/workflows" "$tmp/wfno/.github/workflows"
  {  echo 'jobs:'; echo '  gates:'; echo '    steps:'
     printf '      - run: make -C deploy %s\n' "$MAKE_TARGET"; } >"$tmp/wfyes/.github/workflows/ci.yaml"
  {  echo 'jobs:'; echo '  gates:'; echo '    steps:'
     printf '      # здесь мог бы зваться make -C deploy %s — но не зовётся\n' "$MAKE_TARGET"
     echo '      - run: make -C deploy something-else'; } >"$tmp/wfno/.github/workflows/ci.yaml"
  probe
  if workflow_invokes "$tmp/wfyes" "$MAKE_TARGET"; then
    echo "  ОК  вызов цели из объявления конвейера опознан"
  else
    echo "  ПРОВАЛ вызов цели не опознан — проверка достижимости слепа"; rc=1
  fi
  probe
  if workflow_invokes "$tmp/wfno" "$MAKE_TARGET"; then
    echo "  ПРОВАЛ имя цели В КОММЕНТАРИИ принято за вызов — читается текст, а не объявление"; rc=1
  else
    echo "  ОК  имя цели в комментарии за вызов не принято"
  fi
  probe
  if workflow_invokes "$REPO_ROOT" "$MAKE_TARGET"; then
    echo "  ОК  цель $MAKE_TARGET вызывается конвейером этого дерева"
  else
    echo "  ПРОВАЛ цель $MAKE_TARGET не вызывается ни одним объявлением конвейера"; rc=1
  fi

  # ── ВЕРДИКТ: три исхода доказательства обязаны РАЗЛИЧАТЬСЯ вызывающим ──────
  # Это вторая половина предмета: мало найти доказательство — его ОТКАЗ обязан
  # ронять прогон, а «условие не создано» обязано не зачитываться в успех.
  verdict_probe() { # <метка> <ожидаемый код> <каталог дерева> <подстрока> [ведомость]
    probe
    local label="$1" want_rc="$2" tree="$3" want_txt="$4" out got
    out="$(INJECTION_PROOFS_TREE="$tree" INJECTION_PROOFS_DECLARED="$(discover "$tree")" \
            INJECTION_PROOFS_LEDGER="${5-}" bash "$0" 2>&1)" && got=0 || got=$?
    if [ "$got" -ne "$want_rc" ]; then
      echo "  ПРОВАЛ $label — код $got, ожидался $want_rc"; printf '%s\n' "$out" | sed 's/^/      /'; rc=1; return
    fi
    case "$out" in
      *"$want_txt"*) echo "  ОК  $label — код $got, назван «$want_txt»" ;;
      *) echo "  ПРОВАЛ $label — код $got верен, но в выводе нет «$want_txt»"
         printf '%s\n' "$out" | sed 's/^/      /'; rc=1 ;;
    esac
  }

  mkdir -p "$tmp/green/deploy/scripts" "$tmp/red/deploy/scripts" "$tmp/unmet/deploy/scripts"
  printf '#!/usr/bin/env bash\necho "инъекция зелена"\nexit 0\n' >"$tmp/green/deploy/scripts/zz-ok-inject.sh"
  printf '#!/usr/bin/env bash\necho "инъекция зелена"\nexit 0\n' >"$tmp/red/deploy/scripts/zz-ok-inject.sh"
  printf '#!/usr/bin/env bash\necho "гейт не покраснел на внесённом дефекте"\nexit 1\n' \
    >"$tmp/red/deploy/scripts/zz-broken-inject.sh"
  printf '#!/usr/bin/env bash\necho "инъекция зелена"\nexit 0\n' >"$tmp/unmet/deploy/scripts/zz-ok-inject.sh"
  printf '#!/usr/bin/env bash\necho "FATAL: нет инструмента"\nexit 2\n' \
    >"$tmp/unmet/deploy/scripts/zz-unmet-inject.sh"

  verdict_probe "все зелены → зелено"                0 "$tmp/green" "PASS: доказательства инъекцией"
  verdict_probe "доказательство провалено → красное" 1 "$tmp/red"   "zz-broken-inject.sh"
  verdict_probe "условие не создано → код 2"         2 "$tmp/unmet" "zz-unmet-inject.sh"

  # ЗЕРКАЛО НА УРОВНЕ ГЕЙТА, обе стороны. Слева — дерево, где не-доказательств нет
  # вовсе: ведомость пуста, и это ЦЕЛЬ, а не поломка. Справа — то же дерево плюс
  # файл со словом в имени и без формы: он обязан быть назван, а не пропущен.
  # Проба «все зелены → зелено» выше уже прогоняет левую сторону (ведомость там
  # пуста), поэтому здесь остаётся правая.
  mkdir -p "$tmp/mirror/deploy/scripts"
  printf '#!/usr/bin/env bash\necho "инъекция зелена"\nexit 0\n' >"$tmp/mirror/deploy/scripts/zz-ok-inject.sh"
  printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp/mirror/deploy/scripts/zz-${sfx}or-2.sh"
  verdict_probe "не-доказательство не опознано → красное" 1 "$tmp/mirror" \
    "zz-${sfx}or-2.sh"
  verdict_probe "то же дерево с ведомостью → зелено" 0 "$tmp/mirror" \
    "PASS: доказательства инъекцией" "deploy/scripts/zz-${sfx}or-2.sh|синтетика самопроверки"

  # ПУСТОЙ обход — НЕ зелёный вердикт: совпадение пустого состава с пустым списком
  # не доказывает ничего.
  mkdir -p "$tmp/blind/deploy"
  probe
  out="$(INJECTION_PROOFS_TREE="$tmp/blind" INJECTION_PROOFS_DECLARED="" \
          INJECTION_PROOFS_LEDGER="" bash "$0" 2>&1)" && got=0 || got=$?
  case "$got:$out" in
    2:*"НИ ОДНОГО доказательства"*) echo "  ОК  пустой обход → условие не создано, а не зелено" ;;
    *) echo "  ПРОВАЛ пустой обход дал код $got:"; printf '%s\n' "$out" | sed 's/^/      /'; rc=1 ;;
  esac

  echo
  echo "случаев проверено: $checked"
  [ "$checked" -eq 18 ] || { echo "ПРОВАЛ исполнено $checked случаев из 18"; rc=1; }
  [ $rc -eq 0 ] && echo "PASS: обход доказательств инъекцией" || echo "FAIL: обход доказательств инъекцией"
  exit $rc
fi

# ── Корень обхода. Переопределяется ТОЛЬКО самопроверкой ────────────────────
TREE="${INJECTION_PROOFS_TREE:-$REPO_ROOT}"

# ── ПРЕДПОСЫЛКИ ИСПОЛНЯЮТСЯ ЗДЕСЬ, А НЕ ПРЕДПОЛАГАЮТСЯ У ЗАПУСКАЮЩЕГО ───────
# Отсутствие инструмента — ОТКАЗ (код 2), а не пропуск: «не выполнилось» не идёт
# в зачёт «прошло». Требование общее на весь набор, хотя helm нужен не каждому
# доказательству: смешивать «этот не смог» и «этому не понадобилось» в одном
# прогоне значило бы отдавать код, о котором нельзя сказать, к чему он относится.
if [ "$TREE" = "$REPO_ROOT" ]; then
  if ! command -v helm >/dev/null 2>&1; then
    echo "FATAL: нужен helm — часть доказательств рендерит умбреллу, прогон НЕ ВЫПОЛНЕН"
    exit 2
  fi
  echo "=== зависимости умбреллы (доказательства рендерят её; charts/*.tgz не в git) ==="
  bash "$DEPLOY_ROOT/scripts/helm-umbrella-deps.sh" "$DEPLOY_ROOT/helm/umbrella" \
    || { echo "FATAL: зависимости не материализованы — рендер был бы неполным, прогон НЕ ВЫПОЛНЕН"; exit 2; }
  rm -rf "$DEPLOY_ROOT"/helm/umbrella/tmpcharts-*
fi

FOUND="$(discover "$TREE")"
# shellcheck disable=SC2086
WANT="$(printf '%s\n' ${INJECTION_PROOFS_DECLARED-$DECLARED} | LC_ALL=C sort)"

if [ "$FOUND" != "$WANT" ]; then
  echo "FAIL: состав доказательств инъекцией разошёлся с объявленным."
  echo
  extra="$(LC_ALL=C comm -23 <(printf '%s\n' "$FOUND") <(printf '%s\n' "$WANT"))"
  gone="$(LC_ALL=C comm -13 <(printf '%s\n' "$FOUND") <(printf '%s\n' "$WANT"))"
  [ -n "$extra" ] && { echo "  доказательство есть, а в списке его НЕТ (не исполнялось бы):"; printf '    %s\n' $extra; }
  [ -n "$gone" ]  && { echo "  в списке есть, а доказательства НЕТ (запись без предмета):"; printf '    %s\n' $gone; }
  echo
  echo "Внеси изменение в DECLARED в $(basename "$0") — список существует затем,"
  echo "чтобы забытое доказательство было видно, а не затем, чтобы его обходить."
  exit 1
fi

# ── Собственная достижимость из конвейера ───────────────────────────────────
# Иначе дефект повторяется этажом выше: шаг снимут, обходчик перестанет
# вызываться, и одиннадцать доказательств снова станут немыми — молча.
if [ "$TREE" = "$REPO_ROOT" ]; then
  workflow_invokes "$REPO_ROOT" "$MAKE_TARGET" || {
    echo "FAIL: цель $MAKE_TARGET не вызывается НИ ОДНИМ объявлением конвейера."
    echo "      Тогда этот обходчик исполняется только руками, а одиннадцать доказательств"
    echo "      инъекцией снова немы — ровно тот дефект, который он закрывает."
    exit 1
  }
fi

# ── Зеркальная перепись: соглашение об именовании держится с ДВУХ сторон ────
# ПУСТАЯ ведомость при пустом зеркале — законное состояние и НЕ поломка: это
# цель, а не отсутствие проверки. Падать на достигнутой цели значит толкать
# держать запись ради зелёного.
MIRROR="$(discover_mirror "$TREE")"
LEDGER="$(ledger_paths)"
if [ "$MIRROR" != "$LEDGER" ]; then
  echo "FAIL: зеркальная перепись разошлась с ведомостью."
  unknown="$(LC_ALL=C comm -23 <(printf '%s\n' "$MIRROR") <(printf '%s\n' "$LEDGER"))"
  stale="$(LC_ALL=C comm -13 <(printf '%s\n' "$MIRROR") <(printf '%s\n' "$LEDGER"))"
  [ -n "$unknown" ] && { echo "  «inject» в имени есть, формы доказательства нет, и файл не опознан:";
    printf '    %s\n' $unknown
    echo "  Либо это доказательство — назови его <тема>-inject.sh, и обход подхватит его сам;"
    echo "  либо это не доказательство — внеси в NOT_A_PROOF с причиной."; }
  [ -n "$stale" ] && { echo "  запись ведомости потеряла предмет (файла нет либо он переименован):";
    printf '    %s\n' $stale
    echo "  Исключение живёт, пока у него есть предмет; запись без предмета — находка."; }
  exit 1
fi

count="$(printf '%s\n' "$FOUND" | grep -c .)"
scanned="$(count_candidates "$TREE")"
mirrored="$(printf '%s\n' "${MIRROR-}" | grep -c .)"
echo "=== доказательства инъекцией: осмотрено файлов (*.sh): $scanned; найдено и объявлено $count; опознано не-доказательств $mirrored ==="
if [ "$scanned" -eq 0 ] || [ "$count" -eq 0 ]; then
  echo "FATAL: обход не нашёл НИ ОДНОГО доказательства (осмотрено файлов $scanned)."
  echo "       Совпадение пустого состава с пустым списком не доказывает ничего:"
  echo "       обход ослеп либо дерево не то. Это не «всё зелено»."
  exit 2
fi

failed=""
unmet=""
ran=0
for f in $FOUND; do
  echo
  echo "=== $f ==="
  # КОД ВОЗВРАТА БЕРЁТСЯ КАК ДАННЫЕ. Исходов три, и третий — «условие не создано»
  # — не вердикт о дереве (`tests/helm/README.md` §«Три исхода», `e2e-flow.md` §1).
  ( cd "$TREE" && bash "$f" ) && rc=0 || rc=$?
  case "$rc" in
    0) ran=$((ran + 1)) ;;
    2) unmet="$unmet $f" ;;
    *) failed="$failed $f" ;;
  esac
done

echo
if [ -n "$unmet" ]; then
  echo "!!! УСЛОВИЕ НЕ СОЗДАНО (код 2):$unmet"
  echo "    Не вердикт о дереве и не «доказательство провалено»: нет инструмента,"
  echo "    не собраны зависимости, нет профиля на диске. В зачёт «прошло» это не"
  echo "    идёт — прогон ниже выйдет кодом 2."
fi
if [ -n "$failed" ]; then
  echo "!!! доказательства провалены:$failed"
  echo "    Гейт, чьё доказательство красное, не доказал, что умеет краснеть на"
  echo "    дефекте, — и его зелёный обычный проход ничего не значит."
  exit 1
fi
[ -z "$unmet" ] || exit 2

# «Ноль находок» обязано быть отличимо от «ноль прочитанного».
if [ "$ran" -eq 0 ]; then
  echo "FAIL: не исполнено НИ ОДНОГО доказательства — это провал, а не чистота."
  exit 1
fi
echo "PASS: доказательства инъекцией — $ran/$count зелёные"
