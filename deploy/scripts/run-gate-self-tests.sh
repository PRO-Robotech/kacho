#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ГЕЙТ НАД ГЕЙТАМИ: каждая написанная самопроверка обязана ИСПОЛНЯТЬСЯ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ
#
# Самопроверка (`--self-test`) — это доказательство, что гейт умеет краснеть на
# внесённом дефекте и молчать на законной конструкции той же формы. Ровно то,
# без чего гейт сам является экземпляром «проверки с формой, но без содержания».
#
# Написаны они были — а исполнялись НИКЕМ. Цель манифест-проверок запускала
# скрипты БЕЗ аргументов (то есть только их обычный проход), в workflow'ах их не
# было вовсе. Доказательство существовало как текст и ни разу не проверялось —
# то самое, ради борьбы с чем его и писали.
#
# ЭТОТ СКРИПТ НЕ ВЕДЁТ СПИСОК РУКАМИ. Он НАХОДИТ самопроверки по исполняемому
# признаку и требует, чтобы найденное и объявленное СОВПАДАЛО:
#   • нашлась самопроверка, которой нет в списке → ПРОВАЛ (новую забыли внести,
#     и она бы тихо не исполнялась — тот же дефект классом ниже);
#   • в списке есть запись, для которой самопроверки больше нет → ПРОВАЛ
#     (исключение живёт, пока у него есть предмет; запись без предмета — находка).
#
# ПРИЗНАК — ИСПОЛНЯЕМАЯ ВЕТКА, А НЕ СЛОВО В ТЕКСТЕ. Слово «--self-test»
# встречается в шапках и строках помощи у файлов, где ветки нет; поиск по слову
# нашёл бы их и потребовал запускать несуществующее. Ищем именно разбор
# аргумента.
#
# ─────────────────────────────────────────────────────────────────────────────
# ОБХОД — ВСЁ ДЕРЕВО, И ПРЕДИКАТ ЗЕРКАЛЕН ПО ФОРМАМ РАЗБОРА
#
# Обе половины поиска унаследовали форму первых увиденных примеров, и обе давали
# слепую зону — «ноль расхождений» означало «ноль из того, что поиск умеет
# видеть»:
#
#   • по МЕСТУ: обход шёл только по deploy/ (scripts/assert-*, tests/helm/*.sh).
#     Самопроверка `.github/scripts/check-newman-suite-gates.py` разбирает
#     аргумент ровно той формой, которую поиск узнаёт, и всё равно была невидима
#     — просто потому, что лежит в другом каталоге;
#   • по ФОРМЕ: узнавались только `[ "$1" = "--self-test" ]` и
#     `"--self-test" in sys.argv`. Третья законная форма — объявление флага в
#     argparse (`ap.add_argument("--self-test", …)`) — не узнавалась вовсе, и
#     самопроверка `.github/scripts/newman-live.py` была невидима даже при
#     обходе всего дерева.
#
# Померено на дереве: узкий предикат по всему дереву находил 10 файлов (9 из них
# в deploy/), широкий «просто слово --self-test» — 12. Разница ровно два файла:
# одна настоящая самопроверка формы argparse и ЭТОТ файл, который слово содержит
# только в прозе, регулярном выражении и строке запуска. Второй попасть в состав
# не должен — на нём и проверяется, что предикат читает разбор, а не текст.
#
# Обход идёт по СОДЕРЖИМОМУ РЕПОЗИТОРИЯ (`git ls-files`): это то же множество,
# что увидит CI на свежем checkout'е, поэтому вердикт воспроизводим и не зависит
# от локального мусора в рабочем дереве.
#
# Сам обход проверяется инъекцией: `--verify-discovery` (см. конец файла). Флаг
# назван НЕ `--self-test` намеренно — иначе предикат нашёл бы этот файл и
# потребовал запускать его из самого себя.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

# any_line_matches <многострочное значение> <ERE> — как `grep -qE`: истинно, если
# ХОТЬ ОДНА строка значения совпадает с выражением. Построчность важна: у `grep`
# точка не переходит через перевод строки, а у `[[ =~ ]]` на всём значении —
# переходит. Труба убрана из-за ложного отказа на совпадении (задача #658).
any_line_matches() {
  local _l
  while IFS= read -r _l; do
    if [[ "$_l" =~ $2 ]]; then return 0; fi
  done <<<"$1"
  return 1
}

# line_in <многострочное значение> <строка> — есть ли СТРОКА ЦЕЛИКОМ в значении.
# Замена `grep -qx`/`grep -qxF`: под `pipefail` труба даёт ложный отказ НА
# СОВПАДЕНИИ, потому что писатель получает SIGPIPE (задача #658). Сравнение
# буквальное — там, где раньше стоял `-x` без `-F`, это СТРОЖЕ, то есть ложного
# зелёного добавить не может.
line_in() { [[ $'\n'"$1"$'\n' == *$'\n'"$2"$'\n'* ]]; }

DEPLOY_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_ROOT/.." && pwd)"
cd "$REPO_ROOT" || exit 2

# Объявленный состав, путями от корня репозитория. Держится СИНХРОННЫМ с
# находкой ниже — расхождение в любую сторону роняет проверку.
DECLARED="
.github/scripts/aggregate-shard-verdicts.py
.github/scripts/assert-build-fetch-matches-imports.py
.github/scripts/assert-console-probes-verdict.py
.github/scripts/assert-default-branch-workflows-can-run.py
.github/scripts/assert-required-contexts-match-jobs.py
.github/scripts/check-newman-suite-gates.py
.github/scripts/check-pinned-tools.sh
.github/scripts/check-volume-mounts.py
.github/scripts/console-run-category.py
.github/scripts/go-test-verdict.py
.github/scripts/install-browser-deps.sh
.github/scripts/install-pinned-browser.sh
.github/scripts/newman-live.py
.github/scripts/run-python-probes.py
deploy/scripts/assert-admin-hop-transport.sh
deploy/scripts/assert-alt-fixtures-are-another.py
deploy/scripts/assert-ban6-external-isolation.py
deploy/scripts/assert-burst-waits-for-materialization.py
deploy/scripts/assert-cocreated-child-is-torn-down.py
deploy/scripts/assert-delete-operation-outcome.py
deploy/scripts/assert-delete-steps-are-asserted.py
deploy/scripts/assert-dialled-transports-have-a-producer.py
deploy/scripts/assert-fixture-role-verbs-exist.py
deploy/scripts/assert-generated-scripts-parse.js
deploy/scripts/assert-identity-account-peak-under-ceiling.py
deploy/scripts/assert-identity-admission-rate-headroom.py
deploy/scripts/assert-legacy-issuer-acceptance-has-a-subject.py
deploy/scripts/assert-machine-minter-has-no-dead-exchange-lane.py
deploy/scripts/assert-metrics-surfaces-answer.sh
deploy/scripts/assert-outbox-autovacuum.sh
deploy/scripts/assert-posture-branches-can-be-taken.py
deploy/scripts/assert-refusal-lane-has-a-reader.py
deploy/scripts/assert-report-readers-use-the-summary.py
deploy/scripts/assert-shard-coverage.py
deploy/scripts/assert-stand-precondition-wiring.py
deploy/scripts/assert-step-up-bearer-matches-catalog.py
deploy/scripts/assert-teardown-frees-parent.py
deploy/scripts/assert-verdict-aggregators-honest.sh
deploy/scripts/assert-waiters-name-their-target.sh
deploy/scripts/assert-wave-scheduler-terminates.sh
deploy/scripts/classify-integration-outcome.sh
deploy/scripts/classify-pg-outside-selection.sh
deploy/scripts/gen-managed-image-pins.sh
deploy/scripts/helm-umbrella-deps.sh
deploy/scripts/remeasure-provider-listener-tls.sh
deploy/scripts/run-injection-proofs.sh
deploy/scripts/stand-provenance.sh
deploy/tests/helm/admin-hop-address-census-test.sh
deploy/tests/helm/admin-hop-pod-shape-test.sh
deploy/tests/helm/admin-hop-port-policy-test.sh
deploy/tests/helm/admin-hop-transport-test.sh
deploy/tests/helm/config-rollout-binding-test.sh
deploy/tests/helm/geo-authz-edge-armed-test.sh
deploy/tests/helm/identity-callback-credential-source-test.sh
deploy/tests/helm/image-rollout-binding-test.sh
deploy/tests/helm/kratos-selfservice-ui-hardening-test.sh
deploy/tests/helm/makefile-destructive-guarded-test.sh
deploy/tests/helm/neighbour-address-form-test.sh
deploy/tests/helm/networkpolicy-egress-test.sh
deploy/tests/helm/outbox-autovacuum-naptime-test.sh
deploy/tests/helm/prerequisite-secrets-test.sh
deploy/tests/helm/secret-material-survives-recreation-test.sh
deploy/tests/helm/three-outcomes-distinguishable-test.sh
deploy/tests/helm/trusted-forwarder-profiles-test.sh
gateway/tests/newman/scripts/selftest_tamper_mutation.py
services/compute/tests/newman/scripts/validate-cases.py
services/iam/tests/newman/scripts/exec-coverage.py
services/iam/tests/newman/scripts/selftest_basic_access_token.py
tests/authz-fixtures/ceremony_credentials.py
tests/authz-fixtures/prodseed_all.py
tests/authz-fixtures/prodseed_ceremony.py
tools/mixedoutcomeaudit/mixed_outcome_audit.py
tools/unreadfieldaudit/unread_field_audit.py
"

# Пять законных форм разбора аргумента --self-test. Все пять — КОД: bash-тест,
# ветка bash-case, проверка членства в argv, объявление флага в argparse, проверка
# членства в argv у node. Слово в прозе, внутри регулярного выражения или в строке
# запуска ни одной из них не является.
#
# Четвёртая форма добавлена вместе с расширением обхода на `*.js`: до этого гейт на
# JS не мог попасть в состав ВООБЩЕ, каким бы образом он ни разбирал аргумент, и его
# доказательство инъекцией существовало где угодно, только не в конвейере.
#
# Пятая — ветка `case`. Слепота была ровно того же рода и держалась на совпадении:
# единственный файл дерева, разбирающий флаг через `case`, разбирает его И
# bash-тестом ниже, поэтому находился второй формой, а не первой. Гейт, у которого
# ЕДИНСТВЕННЫМ разбором была бы ветка `case`, не попал бы в состав вовсе. Ветка
# анкерована началом строки: без анкера строка этого же определения нашла бы саму
# себя, и прогонщик снова красил бы состав своими объяснениями.
SELFTEST_PARSE='= *"--self-test" *\]|^[[:space:]]*(-[A-Za-z-]+\|)*--self-test\)|"--self-test" +in +sys\.argv|add_argument\( *"--self-test"|argv\.includes\([^)]*--self-test'

# ЧИТАЕТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ — и это не предосторожность впрок.
# Первая же редакция обхода по всему дереву нашла САМ ЭТОТ ФАЙЛ: в шапке выше
# перечислены узнаваемые формы, и перечисление неотличимо от объявления. Гейт,
# который красят его собственные объяснения, снимут при первом же ложном
# срабатывании — поэтому строки-комментарии отбрасываются до сравнения.
#
# У JS комментарий начинается ИНАЧЕ, и это не косметика: отбрасывание `#` не трогает
# `// …`, поэтому первый же гейт на JS читался бы вместе со своими объяснениями —
# ровно та слепота, из-за которой сюда добавлена проверка comment-only ниже.
# Правило выбирается по расширению, а не применяется ко всем сразу: срезать `//` в
# shell и python значило бы рубить строки на любом URL и получить ложные пропуски.
executable_lines() {
  case "$1" in
    *.js) sed -e 's|[[:space:]]//.*$||' -e '/^[[:space:]]*\/\//d' "$1" ;;
    *)    sed -e 's/[[:space:]]#[[:space:]].*$//' -e '/^[[:space:]]*#/d' "$1" ;;
  esac
}

# Поиск: файл дерева, несущий РАЗБОР аргумента --self-test.
#
# Принимает корень поиска аргументом — так его можно навести на синтетическое
# дерево и доказать инъекцией, не трогая репозиторий. В синтетическом дереве
# `git ls-files` не работает, поэтому там используется обход файловой системы;
# для репозитория авторитет — версионный контроль.
#
# Сортировка — байтовая (LC_ALL=C). В локали по умолчанию точка в начале имени
# при сличении игнорируется, поэтому `.github/…` уезжал ПОСЛЕ `deploy/…`, а
# объявленный список сортировался иначе — состав «расходился» на одном порядке.
#
# Языки состава — `*.sh`, `*.py`, `*.js`. Перечень не «на всякий случай»: `.js`
# добавлен потому, что первый гейт на нём (assert-generated-scripts-parse.js) оказался
# вне инварианта ПО ПОСТРОЕНИЮ — обход его не видел, поэтому вопрос «а исполняется ли
# его самопроверка» даже не задавался. Зеркальная перепись по остальным домам гейтов
# (tools/, .github/scripts, deploy/tests/helm) показала ещё 26 файлов на Go: они СЮДА
# НЕ ОТНОСЯТСЯ и это не пропуск — каждый пакет tools/ несёт companion `_test.go`, то
# есть доказывает себя обычным `go test`, а не флагом `--self-test`. Другого языка
# гейтов в дереве нет.
list_candidates() {
  local root="$1"
  if [ "$root" = "$REPO_ROOT" ] && git -C "$root" rev-parse --git-dir >/dev/null 2>&1; then
    git -C "$root" ls-files -z '*.sh' '*.py' '*.js'
  else
    ( cd "$root" && find . -type f \( -name '*.sh' -o -name '*.py' -o -name '*.js' \) \
        -not -path './.git/*' -not -path '*/node_modules/*' -not -path '*/vendor/*' \
        -printf '%P\0' )
  fi
}

# count_candidates — ОБЪЁМ ОСМОТРЕННОГО. «Ноль находок» обязано быть отличимо от
# «ноль прочитанного», и это относится к самому обходу тоже: без этого числа
# сообщение «состав совпадает» было бы одинаковым и когда обход прочёл всё дерево,
# и когда он не дошёл ни до одного файла.
count_candidates() {
  list_candidates "${1:-$REPO_ROOT}" | tr -dc '\0' | wc -c | tr -d ' '
}

discover() {
  local root="${1:-$REPO_ROOT}" f
  list_candidates "$root" | while IFS= read -r -d '' f; do
    [ -f "$root/$f" ] || continue
    # Исполняемая часть берётся ПОДСТАНОВКОЙ, а не конвейером в grep. С
    # `pipefail` конвейер `sed … | grep -q` возвращает ОТКАЗ на совпадении:
    # grep выходит по первому попаданию, sed получает SIGPIPE (141), и статус
    # конвейера берётся от него. Файл при этом молча выпадает из состава — тем
    # заметнее, что зависит от РАЗМЕРА файла: на коротких sed успевает
    # дописать и отказа нет. Ровно так пропал самый длинный из гейтов, а
    # синтетические фикстуры (по три строки) дефект не показывали.
    if grep -qE "$SELFTEST_PARSE" <<<"$(executable_lines "$root/$f")"; then
      echo "$f"
    fi
  done | LC_ALL=C sort
}

# ── ИНЪЕКЦИЯ В ОБХОД: доказательство, что предикат читает разбор, а не текст ──
#
# Идёт ДО предусловий (yq/helm): обход к ним не обращается, а требовать
# инструменты для проверки чистого разбора текста значило бы не иметь возможности
# проверить его там, где инструментов нет.
if [ "${1:-}" = "--verify-discovery" ]; then
  echo "=== обход самопроверок: инъекции в синтетическое дерево ==="
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  rc=0
  mkdir -p "$tmp/deploy/scripts" "$tmp/.github/scripts" "$tmp/services/x/tools"

  # ТРИ ЗАКОННЫЕ ФОРМЫ — обязаны найтись, в любом каталоге дерева.
  #
  # Фикстуры СОБИРАЮТСЯ из подстановки, а не пишутся литералом: литерал сделал бы
  # этот файл своей же находкой, и «прогонщик себя не находит» держалось бы на
  # везении. Тот же приём, что и отбрасывание комментариев выше.
  sfx='--self-test'
  { echo '#!/usr/bin/env bash'; printf 'if [ "${1:-}" = "%s" ]; then echo x; fi\n' "$sfx"; } \
    >"$tmp/deploy/scripts/assert-bash-form.sh"
  { echo 'import sys'; printf 'sys.exit(0 if "%s" in sys.argv[1:] else 1)\n' "$sfx"; } \
    >"$tmp/.github/scripts/argv-form.py"
  { echo 'import argparse'; echo 'ap = argparse.ArgumentParser()'
    printf 'ap.add_argument("%s", action="store_true")\n' "$sfx"; } \
    >"$tmp/services/x/tools/argparse-form.py"
  # Четвёртая форма — node. Одиночные кавычки здесь не случайность: в JS так пишут
  # чаще, и предикат обязан узнавать форму, а не полюбившийся ему сорт кавычек.
  { echo "'use strict';"
    printf "if (process.argv.includes('%s')) { process.exit(0); }\n" "$sfx"; } \
    >"$tmp/deploy/scripts/node-form.js"
  # Пятая форма — ветка `case`, включая вариант с соседними флагами слева
  # (`-h|--help|--self-test)`). Проверяется именно она: единственный файл дерева с
  # таким разбором находился ДРУГОЙ формой, поэтому слепота была невидима.
  { echo '#!/usr/bin/env bash'; echo 'case "${1:-}" in'
    printf '  -h|%s) echo x ;;\n' "$sfx"; echo 'esac'; } \
    >"$tmp/deploy/scripts/case-form.sh"

  # НЕ САМОПРОВЕРКИ — слово есть, разбора нет. Обязаны НЕ найтись, иначе прогон
  # потребует запускать несуществующую ветку.
  { echo '#!/usr/bin/env bash'; printf '# Самопроверка: %s (её здесь нет)\n' "$sfx"
    printf 'echo "запусти: bash $0 %s"\n' "$sfx"; } >"$tmp/deploy/scripts/prose-only.sh"
  # Форма, на которой сломалась первая редакция: объяснение узнаваемой формы,
  # записанное КОММЕНТАРИЕМ, — ровно то, чем этот файл красил сам себя.
  { echo '#!/usr/bin/env bash'
    printf '# узнаётся форма: [ "$1" = "%s" ] и "%s" in sys.argv\n' "$sfx" "$sfx"
    printf '# и ветка case: %s) — здесь это тоже объяснение, а не разбор\n' "$sfx"
    echo 'echo ничего'; } >"$tmp/deploy/scripts/comment-only.sh"
  # То же самое ДЛЯ JS, и это не симметрия ради симметрии: комментарий в JS
  # начинается с `//`, а отбрасывание `#` его не трогает. Без этой фикстуры первый
  # же гейт на JS красил бы состав своими собственными объяснениями — и «прогонщик
  # себя не находит» держалось бы только на том, что таких файлов ещё не было.
  { echo "'use strict';"
    printf "// узнаётся форма: process.argv.includes('%s') — здесь её нет\n" "$sfx"
    echo 'console.log("ничего");'; } >"$tmp/deploy/scripts/js-comment-only.js"

  # ДЛИННЫЙ ФАЙЛ, СОВПАДЕНИЕ В НАЧАЛЕ — размерная половина предиката. Короткие
  # фикстуры выше не различают «нашлось» и «нашлось, но пропало по SIGPIPE»:
  # у них чтение успевает завершиться. Здесь после совпадения идёт ещё много
  # строк, поэтому читатель обязан дочитать файл, а не полагаться на удачу.
  { printf 'if [ "${1:-}" = "%s" ]; then echo x; fi\n' "$sfx"
    for i in $(seq 1 4000); do echo "# наполнитель $i"; done; } \
    >"$tmp/deploy/scripts/long-form.sh"

  found="$(discover "$tmp")"
  want=".github/scripts/argv-form.py
deploy/scripts/assert-bash-form.sh
deploy/scripts/case-form.sh
deploy/scripts/long-form.sh
deploy/scripts/node-form.js
services/x/tools/argparse-form.py"
  if [ "$found" = "$want" ]; then
    echo "  ОК  все ПЯТЬ форм разбора найдены, в трёх каталогах, включая длинный файл"
  else
    echo "  ПРОВАЛ состав не тот"; echo "--- найдено:"; printf '%s\n' "$found" | sed 's/^/      /'
    echo "--- ожидалось:"; printf '%s\n' "$want" | sed 's/^/      /'; rc=1
  fi
  for f in deploy/scripts/prose-only.sh deploy/scripts/comment-only.sh \
           deploy/scripts/js-comment-only.js; do
    if line_in "$found" "$f"; then
      echo "  ПРОВАЛ $f принят за самопроверку — предикат читает текст, а не разбор"; rc=1
    else
      echo "  ОК  $f не принят: слово есть, исполняемой ветки нет"
    fi
  done

  # ЭТОТ ФАЙЛ обязан остаться вне состава: он содержит слово в прозе, в
  # регулярном выражении и в строке запуска — но ветки `--self-test` не несёт.
  if line_in "$(discover)" "deploy/scripts/$(basename "$0")"; then
    echo "  ПРОВАЛ прогонщик нашёл САМ СЕБЯ — состав стал бы рекурсивным"; rc=1
  else
    echo "  ОК  прогонщик себя не находит"
  fi

  # РАСХОЖДЕНИЕ В ОБЕ СТОРОНЫ обязано быть красным. Проверяется тем же
  # сравнением, что и в основном проходе.
  real="$(discover)"
  for case in extra gone; do
    case "$case" in
      extra) probe="$(printf '%s\n' "$real" | tail -n +2)"; what="самопроверка есть, в списке НЕТ" ;;
      gone)  probe="$(printf '%s\n%s\n' "$real" "deploy/scripts/gone.sh" | sort)"; what="в списке есть, самопроверки НЕТ" ;;
    esac
    if [ "$real" = "$probe" ]; then
      echo "  ПРОВАЛ ($case) сравнение не различает состав"; rc=1
    else
      echo "  ОК  ($case) расхождение видно: $what"
    fi
  done

  echo
  [ $rc -eq 0 ] && echo "PASS: обход самопроверок" || echo "FAIL: обход самопроверок"
  exit $rc
fi

# ── ПРЕДУСЛОВИЯ ИСПОЛНЯЮТСЯ ЗДЕСЬ, А НЕ ПРЕДПОЛАГАЮТСЯ У ЗАПУСКАЮЩЕГО ────────
#
# Ровно те же два, что и у цели манифест-проверок, и по той же причине —
# проверено на себе при первом в истории прогоне этих самопроверок:
#
# 1. Зависимости чарта. `charts/*.tgz` в git не лежат; на свежем checkout'е
#    рендер падает у всех, КРОМЕ тех проверок, что материализовали зависимости
#    сами. Тогда исход зависит от АЛФАВИТНОГО ПОРЯДКА имён файлов: самопроверка
#    networkpolicy-egress упала «values.prod не рендерится», а после того как
#    шедшая ниже по алфавиту outbox-autovacuum-naptime собрала зависимости —
#    прошла без единой правки. Собираем один раз, заранее, для всех.
#
#    «Один раз» стало правдой только теперь. Прежде здесь стояла своя подкачка,
#    и ещё две проверки несли свою же: за один прогон job'а сеть опрашивалась до
#    десяти раз, а отказ ЛЮБОГО из этих походов красил весь прогон — что и
#    случилось дважды подряд на релизной ветке. Материализация переехала к
#    ЕДИНСТВЕННОМУ владельцу (scripts/helm-umbrella-deps.sh): повторный вызов
#    внутри прогона в сеть не идёт, а причина отказа больше не глотается.
#    Свойство держит гейт deploy/helm_deps_single_owner_test.go.
#
# 2. Тот ли yq. В PATH бывают две разные программы с этим именем; фильтры
#    написаны под mikefarah v4, а python-обёртка над jq молча отдаёт ПУСТО.
#    Одна проверка ловит это сама и отказывается работать — остальные нет.
#
# Отсутствие инструмента — ОТКАЗ, а не пропуск: «не выполнилось» не идёт в зачёт
# «прошло».
if ! command -v yq >/dev/null 2>&1; then
  echo "FATAL: нужен yq (mikefarah v4) — фильтры проверок написаны под него."
  exit 2
fi
# Сравнение — БЕЗ трубы: `… | grep -q` под `set -o pipefail` возвращает ОТКАЗ
# НА СОВПАДЕНИИ (grep выходит по первому попаданию, писатель получает SIGPIPE,
# и `pipefail` поднимает ЕГО статус до статуса конвейера). Задача #658.
YQ_VER="$(yq --version 2>&1 || true)"
if ! any_line_matches "$YQ_VER" 'mikefarah|version v?4'; then
  echo "FATAL: в PATH не тот yq: $(command -v yq) → $(yq --version 2>&1)"
  echo "       Нужен mikefarah yq v4. python-yq (обёртка над jq) на этих фильтрах"
  echo "       молча отдаёт ПУСТО — утверждения пройдут, ничего не сверив."
  exit 2
fi
echo "=== зависимости умбреллы (самопроверки рендерят её; charts/*.tgz не в git) ==="
bash "$DEPLOY_ROOT/scripts/helm-umbrella-deps.sh" "$DEPLOY_ROOT/helm/umbrella" \
  || { echo "FATAL: зависимости не материализованы — рендер будет неполным, проверки НЕ ВЫПОЛНЕНЫ"; exit 2; }
rm -rf "$DEPLOY_ROOT"/helm/umbrella/tmpcharts-*

FOUND="$(discover)"
WANT="$(printf '%s\n' $DECLARED | LC_ALL=C sort)"

if [ "$FOUND" != "$WANT" ]; then
  echo "FAIL: состав самопроверок разошёлся с объявленным."
  echo
  # LC_ALL=C и здесь: оба списка отсортированы байтово, а `comm` без того же
  # правила сличения объявляет их «неотсортированными» и врёт про разницу.
  extra="$(LC_ALL=C comm -23 <(printf '%s\n' "$FOUND") <(printf '%s\n' "$WANT"))"
  gone="$(LC_ALL=C comm -13 <(printf '%s\n' "$FOUND") <(printf '%s\n' "$WANT"))"
  [ -n "$extra" ] && { echo "  самопроверка есть, а в списке её НЕТ (не исполнялась бы):"; printf '    %s\n' $extra; }
  [ -n "$gone" ]  && { echo "  в списке есть, а самопроверки НЕТ (запись без предмета):"; printf '    %s\n' $gone; }
  echo
  echo "Внеси изменение в DECLARED в $(basename "$0") — список существует затем,"
  echo "чтобы забытая самопроверка была видна, а не затем, чтобы его обходить."
  exit 1
fi

count="$(printf '%s\n' "$FOUND" | grep -c . )"
scanned="$(count_candidates)"
# Объём осмотренного печатается ВМЕСТЕ с составом: «найдено и объявлено N» без него
# одинаково читается и когда обход прочёл всё дерево, и когда он не дошёл ни до
# одного файла.
echo "=== самопроверки гейтов: осмотрено файлов (*.sh/*.py/*.js): $scanned; "\
"найдено и объявлено $count, состав совпадает ==="
if [ "$scanned" -eq 0 ]; then
  echo "FAIL: обход не прочитал НИ ОДНОГО файла — совпадение пустого состава с пустым"
  echo "      списком не является доказательством чего-либо."
  exit 1
fi

failed=""
unmet=""
ran=0
for f in $FOUND; do
  echo
  echo "=== $f --self-test ==="
  case "$f" in
    *.py) cmd=(python3 "$f" --self-test) ;;
    *.js) cmd=(node "$f" --self-test) ;;
    *)    cmd=(bash "$f" --self-test) ;;
  esac
  # КОД ВОЗВРАТА БЕРЁТСЯ КАК ДАННЫЕ. Исходов три, и третий — «условие не создано»
  # (код 2: нет инструмента, не собраны зависимости, нет профиля на диске) — не
  # вердикт о дереве: `tests/helm/README.md` §«Три исхода», `e2e-flow.md` §1. Пока
  # здесь стояло `if "${cmd[@]}"`, единица и двойка схлопывались в одно
  # «самопроверка провалена», и отсутствие условия читалось как «гейт не доказал,
  # что умеет краснеть» — то есть как находка о дереве.
  "${cmd[@]}" && rc=0 || rc=$?
  case "$rc" in
    0) ran=$((ran + 1)) ;;
    2) unmet="$unmet $f" ;;
    *) failed="$failed $f" ;;
  esac
done

echo
if [ -n "$unmet" ]; then
  echo "!!! УСЛОВИЕ НЕ СОЗДАНО (код 2):$unmet"
  echo "    Не вердикт о дереве и не «самопроверка провалена»: нет инструмента,"
  echo "    не собраны зависимости умбреллы, нет профиля на диске. В зачёт «прошло»"
  echo "    это не идёт — прогон ниже выйдет кодом 2."
fi
if [ -n "$failed" ]; then
  echo "!!! самопроверки провалены:$failed"
  echo "    Гейт, чья самопроверка красная, не доказал, что умеет краснеть на дефекте —"
  echo "    и его зелёный обычный проход ничего не значит."
  exit 1
fi
[ -z "$unmet" ] || exit 2

# «Ноль находок» обязано быть отличимо от «ноль прочитанного».
if [ "$ran" -eq 0 ]; then
  echo "FAIL: не исполнено НИ ОДНОЙ самопроверки — это провал, а не чистота."
  exit 1
fi
echo "PASS: самопроверки гейтов — $ran/$count зелёные"
