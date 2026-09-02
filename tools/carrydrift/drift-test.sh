#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Проба гейта «перенос ветки не откатил ствол» — на синтетических графах.
#
# ЗАЧЕМ. Гейт судит посадки в стволе, а сам не был проверен ничем: ни одной
# пробы на `drift.sh` в дереве не было. Три дефекта дожили до боевого прогона
# именно поэтому, и каждый из них здесь стоит отдельным случаем.
#
# УСТРОЙСТВО. Каждый случай строит СВОЙ репозиторий в каталоге на один прогон:
# граф задаётся явно, поэтому вердикт не зависит ни от истории продукта, ни от
# того, что кто-то сегодня влил. Проба обязана быть детерминированной — иначе
# она сама станет тем, что ловит.
#
# ЧТО ДОКАЗЫВАЕТ КАЖДЫЙ СЛУЧАЙ. Отрицание годится только в паре с положительным:
# случай, где гейт обязан ПРОМОЛЧАТЬ, всегда стоит рядом со случаем, где он
# обязан ЗАГОВОРИТЬ. Иначе «молчит» неотличимо от «не работает вовсе».

set -uo pipefail
# line_in <многострочное значение> <строка> — есть ли СТРОКА ЦЕЛИКОМ в значении.
# Замена `grep -qx`/`grep -qxF`: под `pipefail` труба даёт ложный отказ НА
# СОВПАДЕНИИ, потому что писатель получает SIGPIPE (задача #658). Сравнение
# буквальное — там, где раньше стоял `-x` без `-F`, это СТРОЖЕ, то есть ложного
# зелёного добавить не может.
line_in() { [[ $'\n'"$1"$'\n' == *$'\n'"$2"$'\n'* ]]; }
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

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
DRIFT="$HERE/drift.sh"
JUDGE="$HERE/judge.sh"

pass=0
fail=0
cases=0

ok() { pass=$((pass + 1)); echo "  ✓ $1"; }
no() { fail=$((fail + 1)); echo "  ✗ $1"; echo "$2" | sed 's/^/      /'; }

# Репозиторий на один случай. Подпись фиксирована здесь и никогда не берётся из
# настроек машины: проба, зависящая от чужого git-config, красна не по делу.
#
# СУДЬЯ ЗОВЁТ `drift.sh` ПО ОТНОСИТЕЛЬНОМУ ПУТИ, поэтому синтетический репозиторий
# обязан нести оба скрипта — иначе `bash tools/carrydrift/drift.sh` отвечает «нет
# такого файла», судья читает это как отказ разбора и классифицирует посадку, ни
# разу её не сверив. Случай, утверждающий что-либо о РАЗБОРЕ посадки, в таком
# репозитории зеленел бы, не коснувшись предмета.
#
# Скрипты кладутся в рабочую копию и НЕ коммитятся: попав в дерево, они вошли бы
# в дельты ствола и ветки и меняли бы тот самый граф, который случай строит.
# Исключение точечное, по двум путям, — `declared-removals.txt` остаётся
# коммитабельным, он предмет случая 4.
newrepo() {
  local d
  d=$(mktemp -d)
  git -C "$d" init -q -b main
  git -C "$d" config user.email proba@example.invalid
  git -C "$d" config user.name  proba
  git -C "$d" config commit.gpgsign false
  mkdir -p "$d/tools/carrydrift"
  cp "$DRIFT" "$JUDGE" "$d/tools/carrydrift/"
  printf '/tools/carrydrift/drift.sh\n/tools/carrydrift/judge.sh\n' \
    >> "$d/.git/info/exclude"
  echo "$d"
}
commit() { # <repo> <сообщение>
  git -C "$1" add -A
  git -C "$1" commit -qm "$2"
}

# ── Случай 1. Настоящий откат — гейт ОБЯЗАН заговорить ───────────────────────
# Ствол правит файл, ветка его не касается, а результат несёт версию базы.
# Это и есть предмет гейта; без этого случая все прочие ничего не значат.
case_real_rollback() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/shared.txt"; echo "прочее" > "$r/other.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b branch
  echo "правка ветки" > "$r/other.txt"; commit "$r" "ветка правит своё"
  local br; br=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q main
  echo "ПРАВКА СТВОЛА" > "$r/shared.txt"; commit "$r" "ствол правит общий файл"
  local trunk; trunk=$(git -C "$r" rev-parse HEAD)

  # Посадка, вернувшая содержимое базы на файле, которого ветка не касалась.
  git -C "$r" checkout -q -b landed "$br"
  echo "база" > "$r/shared.txt"; commit "$r" "посадка вернула базу"
  local landed; landed=$(git -C "$r" rev-parse HEAD)

  local out rc
  out=$(cd "$r" && DRIFT_DECLARED_REV=HEAD bash "$DRIFT" "$base" "$br" "$trunk" "$landed" 2>&1); rc=$?
  if [ "$rc" -ne 0 ] && [[ "$out" == *"ОТКАТ: shared.txt"* ]]; then
    ok "настоящий откат назван, и координата напечатана"
  else
    no "настоящий откат НЕ пойман (rc=$rc) — гейт не способен упасть" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 2. Законный близнец — ветка сама правила файл ─────────────────────
# Та же форма графа, но файл принадлежит ветке. Гейт обязан промолчать, иначе
# первый же ложный срабат его отключит.
case_branch_owns_file() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/shared.txt"; echo "прочее" > "$r/other.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b branch
  echo "ВЕТКА ПРАВИТ ОБЩИЙ" > "$r/shared.txt"; commit "$r" "ветка правит общий файл"
  local br; br=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q main
  echo "ПРАВКА СТВОЛА" > "$r/shared.txt"; echo "ствол" > "$r/other.txt"; commit "$r" "ствол правит оба"
  local trunk; trunk=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b landed "$br"
  echo "ствол" > "$r/other.txt"; commit "$r" "посадка"
  local landed; landed=$(git -C "$r" rev-parse HEAD)

  local out rc
  out=$(cd "$r" && bash "$DRIFT" "$base" "$br" "$trunk" "$landed" 2>&1); rc=$?
  if [ "$rc" -eq 0 ] && [[ "$out" != *"ОТКАТ"* ]]; then
    ok "файл, который правила сама ветка, находкой не назван"
  else
    no "ложная находка на файле ветки (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 3. ПЕРЕЕЗД содержимого: файл ствола лежит в результате под другим
# именем. Наблюдалось вживую: две линии независимо завели миграции 0087/0088,
# при сведении они стали 0089/0090 — содержимое тождественно, а гейт назвал это
# откатом, потому что сравнивал ПО ПУТИ. Утраты нет, и находки быть не должно.
case_content_moved() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "прочее" > "$r/other.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b branch
  echo "правка ветки" > "$r/other.txt"; commit "$r" "ветка правит своё"
  local br; br=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q main
  printf 'СОДЕРЖИМОЕ МИГРАЦИИ\n' > "$r/0087.sql"; commit "$r" "ствол завёл 0087"
  local trunk; trunk=$(git -C "$r" rev-parse HEAD)

  # Посадка: тот же байт-в-байт файл, но перенумерован — это разрешение
  # конфликта нумерации, а не утрата.
  git -C "$r" checkout -q -b landed "$br"
  printf 'СОДЕРЖИМОЕ МИГРАЦИИ\n' > "$r/0089.sql"; commit "$r" "перенумерован в 0089"
  local landed; landed=$(git -C "$r" rev-parse HEAD)

  local out rc
  out=$(cd "$r" && bash "$DRIFT" "$base" "$br" "$trunk" "$landed" 2>&1); rc=$?
  if [ "$rc" -eq 0 ] && [[ "$out" != *"ОТКАТ"* ]]; then
    ok "переезд содержимого под другим именем находкой не назван"
  else
    no "перенумерация прочитана как откат — утраты нет, а гейт краснеет (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 3б. Близнец к переезду: содержимое ИСЧЕЗЛО совсем ─────────────────
# Тот же граф, но файла нет нигде. Это настоящая утрата, и она обязана краснеть,
# иначе послабление из случая 3 закрыло бы сам предмет гейта.
case_content_truly_gone() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "прочее" > "$r/other.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b branch
  echo "правка ветки" > "$r/other.txt"; commit "$r" "ветка правит своё"
  local br; br=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q main
  printf 'СОДЕРЖИМОЕ МИГРАЦИИ\n' > "$r/0087.sql"; commit "$r" "ствол завёл 0087"
  local trunk; trunk=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b landed "$br"
  commit "$r" "посадка без файла ствола" 2>/dev/null || git -C "$r" commit -q --allow-empty -m "посадка без файла ствола"
  local landed; landed=$(git -C "$r" rev-parse HEAD)

  local out rc
  out=$(cd "$r" && bash "$DRIFT" "$base" "$br" "$trunk" "$landed" 2>&1); rc=$?
  if [ "$rc" -ne 0 ] && [[ "$out" == *"0087.sql"* ]]; then
    ok "исчезнувшее содержимое названо находкой (послабление переезда не всеразрешающее)"
  else
    no "утрата файла НЕ поймана (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 4. Перечень объявленных снятий судится ОТПРАВКОЙ, а не посадкой ───
# Комментарий самого скрипта говорит: «объявление описывает ОТПРАВКУ целиком, и
# каждая посадка в ней судится под ним». Проверка самоистечения делала обратное
# — считала запись пережившей предмет в каждой посадке, где снятия не случилось.
# При непустом перечне это красит ЛЮБУЮ посадку, и вердикт перестаёт зависеть от
# того, что в ней произошло.
case_declared_not_judged_per_landing() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/shared.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b branch
  echo "правка ветки" > "$r/branchfile.txt"; commit "$r" "ветка правит своё"
  local br; br=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q main
  echo "ПРАВКА СТВОЛА" > "$r/shared.txt"; commit "$r" "ствол правит общий файл"
  local trunk; trunk=$(git -C "$r" rev-parse HEAD)

  # Посадка чистая: ничего не откачено. Но перечень несёт запись про ДРУГОЙ
  # файл, снятый в иной посадке той же отправки.
  git -C "$r" checkout -q -b landed "$br"
  echo "ПРАВКА СТВОЛА" > "$r/shared.txt"
  mkdir -p "$r/tools/carrydrift"
  echo "some/other/file.go  снят в другой посадке этой же отправки" \
    > "$r/tools/carrydrift/declared-removals.txt"
  commit "$r" "чистая посадка, перечень непуст"
  local landed; landed=$(git -C "$r" rev-parse HEAD)

  local out rc
  out=$(cd "$r" && DRIFT_DECLARED_REV="$landed" bash "$DRIFT" "$base" "$br" "$trunk" "$landed" 2>&1); rc=$?
  if [ "$rc" -eq 0 ]; then
    ok "чистая посадка остаётся чистой при непустом перечне отправки"
  else
    no "непустой перечень красит чистую посадку (rc=$rc) — вердикт не про эту посадку" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 5. Слияние, у которого сторона ствола — ВТОРОЙ родитель ───────────
# Форма законная и прямо предписанная: `git merge origin/main` из ветки
# сохраняет запись о слиянии, ради которой этот способ и выбран. Судья обязан
# дойти до переписи и напечатать её. Наблюдалось: он умирал молча и выходил
# единицей — «нашлись находки» было неотличимо от «не дошли до проверки».
#
# ССЫЛКА origin/main В ПРОБЕ ОБЯЗАТЕЛЬНА, и это не декорация. Различитель сторон
# читает `git rev-list --first-parent origin/main`; без этой ссылки список пуст,
# все родители объявляются «не на стволе», ветка свапа не исполняется вовсе — и
# проба зеленеет, не коснувшись предмета. Первая редакция этого случая ровно так
# и прошла на заведомо сломанном коде.
case_trunk_is_second_parent() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/a.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b feature
  echo "ветка" > "$r/b.txt"; commit "$r" "ветка правит своё"

  git -C "$r" checkout -q main
  echo "ствол" > "$r/a.txt"; commit "$r" "ствол ушёл вперёд"
  local trunk_before; trunk_before=$(git -C "$r" rev-parse HEAD)

  # Слияние ВНУТРИ ВЕТКИ — ветка догоняет ствол. У этого коммита первый родитель
  # ветка, второй ствол; на первородительскую цепь ствола он не попадает, потому
  # что ветка садится отдельным слиянием сверху. Это и есть форма, на которой
  # судья умирал: свап сторон вычищает единственный элемент списка, внешняя
  # команда возвращает «ничего не нашлось», и шаг падает под своими же флагами.
  git -C "$r" checkout -q feature
  git -C "$r" merge -q --no-edit "$trunk_before" -m "ветка догнала ствол"
  local inner; inner=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q main
  git -C "$r" merge -q --no-ff --no-edit feature -m "посадка ветки"
  local head; head=$(git -C "$r" rev-parse HEAD)
  git -C "$r" update-ref refs/remotes/origin/main "$head"

  # Предпосылка самой пробы: у ВНУТРЕННЕГО слияния сторона ствола обязана быть
  # вторым родителем и лежать на первородительской цепи, а первый родитель — не
  # лежать. Иначе случай проверяет не то, что назван проверять, и зеленеет зря.
  local p1 p2 chain
  read -r _ p1 p2 <<<"$(git -C "$r" rev-list --parents -n1 "$inner")"
  chain=$(git -C "$r" rev-list --first-parent origin/main)
  if [ "$p2" != "$trunk_before" ] \
     || ! line_in "$chain" "$p2" \
     || line_in "$chain" "$p1"; then
    no "предпосылка случая не выполнена: стороны не в нужной форме (p1=$p1 p2=$p2)" ""
    rm -rf "$r"; return
  fi

  local out rc
  out=$(cd "$r" && BEFORE="$base" HEAD_SHA="$head" bash -e -o pipefail "$JUDGE" 2>&1); rc=$?
  if [[ "$out" == *"drift: посадок в прогоне"* ]]; then
    ok "судья дошёл до переписи на слиянии, где ствол — второй родитель"
  else
    no "судья умер, не напечатав переписи (rc=$rc): «находки» неотличимы от «не дошли»" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 6. Область гейта ПУСТА — это не слепота ───────────────────────────
# Ствол успел изменить только те файлы, которые правит и сама ветка. Тогда у
# гейта не остаётся в области НИ ОДНОГО файла — по его же правилу области
# («ветки отличаются на N из них: их судит не этот гейт»). Возвращать было
# нечего by construction, ровно как когда ствол не двигался вовсе.
#
# Прежний судья читал этот исход как слепоту и краснел. Различитель у него был
# УЖЕ условия, которое он различал: он спрашивал «двигался ли ствол», тогда как
# пустой область делает не только неподвижный ствол, но и ствол, вся правка
# которого лежит на файлах ветки. Прокси ломался раньше своего предмета.
#
# Форма не экзотическая, а самая частая: две линии дописывают ОДИН перечень
# (наблюдалось на `proto/declared-breaks.yaml`), и дельта ствола схлопывается в
# один файл, который правят обе. Хуже того, красило это не автора формы: посадка
# уезжала в ствол и краснела у КАЖДОГО, кто потом догонял ствол, — двум
# несвязанным PR продукта разом.
#
# Отдельный довод, проверяемый глазом: при `skipped=3` из 417 гейт молчит и
# сегодня. Краснеть на `skipped=1` из 1 и молчать на `skipped=3` из 417 —
# несогласуемо: дыра области одна и та же, различается лишь её доля.
case_empty_surface_is_not_blindness() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/ledger.txt"; echo "прочее" > "$r/other.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b feature
  printf 'база\nстрока ветки\n' > "$r/ledger.txt"; commit "$r" "ветка дописала перечень"

  git -C "$r" checkout -q main
  printf 'база\nстрока ствола\n' > "$r/ledger.txt"; commit "$r" "ствол дописал ТОТ ЖЕ перечень"
  local trunk_before; trunk_before=$(git -C "$r" rev-parse HEAD)

  # Ветка догоняет ствол. Единственный файл, который менял ствол, — тот же,
  # что правит ветка, поэтому область гейта пуста.
  git -C "$r" checkout -q feature
  git -C "$r" merge -q --no-edit "$trunk_before" -m "ветка догнала ствол" >/dev/null 2>&1 || true
  printf 'база\nстрока ствола\nстрока ветки\n' > "$r/ledger.txt"
  git -C "$r" add -A
  git -C "$r" commit -qm "ветка догнала ствол" >/dev/null 2>&1 \
    || git -C "$r" commit -q --amend --no-edit >/dev/null 2>&1
  local inner; inner=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q main
  git -C "$r" merge -q --no-ff --no-edit feature -m "посадка ветки"
  local head; head=$(git -C "$r" rev-parse HEAD)
  git -C "$r" update-ref refs/remotes/origin/main "$head"

  # Предпосылка случая, без которой он зеленел бы вхолостую: у внутреннего
  # слияния ствол ОБЯЗАН был двигаться (иначе это давно опознанный «ствол не
  # двигался»), и КАЖДЫЙ сдвинутый им файл обязан лежать в правках ветки —
  # иначе область не пуста и предмет случая не построен.
  local p1 p2 mb moved untouched
  read -r _ p1 p2 <<<"$(git -C "$r" rev-list --parents -n1 "$inner")"
  mb=$(git -C "$r" merge-base "$p2" "$p1")
  moved=$(git -C "$r" diff --name-only "$mb" "$p2")
  untouched=$(comm -23 \
    <(printf '%s\n' "$moved" | sort -u) \
    <(git -C "$r" diff --name-only "$(git -C "$r" merge-base "$p2" "$p1")" "$p1" | sort -u))
  if [ -z "$moved" ] || [ -n "$untouched" ]; then
    no "предпосылка случая не выполнена: ствол сдвинул «${moved//$'\n'/,}», вне ветки «${untouched//$'\n'/,}»" ""
    rm -rf "$r"; return
  fi

  local out rc
  out=$(cd "$r" && BEFORE="$base" HEAD_SHA="$head" bash -e -o pipefail "$JUDGE" 2>&1); rc=$?
  if [ "$rc" -eq 0 ] && [[ "$out" != *"не сверил НИ ОДНОГО"* ]]; then
    ok "пустая область гейта не выдана за слепоту"
  else
    no "пустая область прочитана как слепота (rc=$rc) — гейт краснеет там, где судить нечего" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 6б. Близнец: рядом с файлом ветки ЛЕЖИТ настоящий откат ───────────
# Тот же граф, но ствол сдвинул ещё один файл, которого ветка не касалась, и
# посадка вернула на нём базу. Послабление случая 6 не имеет права это съесть:
# область непуста, значит гейт обязан заговорить и назвать координату.
case_rollback_survives_the_empty_surface_relief() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/ledger.txt"; echo "база" > "$r/lone.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b feature
  printf 'база\nстрока ветки\n' > "$r/ledger.txt"; commit "$r" "ветка дописала перечень"

  git -C "$r" checkout -q main
  printf 'база\nстрока ствола\n' > "$r/ledger.txt"
  echo "ПРАВКА СТВОЛА" > "$r/lone.txt"; commit "$r" "ствол правит перечень и свой файл"
  local trunk_before; trunk_before=$(git -C "$r" rev-parse HEAD)

  # Догон, вернувший базу на файле, которого ветка не касалась.
  git -C "$r" checkout -q feature
  git -C "$r" merge -q --no-edit "$trunk_before" -m "ветка догнала ствол" >/dev/null 2>&1 || true
  printf 'база\nстрока ствола\nстрока ветки\n' > "$r/ledger.txt"
  echo "база" > "$r/lone.txt"
  git -C "$r" add -A
  git -C "$r" commit -qm "ветка догнала ствол" >/dev/null 2>&1 \
    || git -C "$r" commit -q --amend --no-edit >/dev/null 2>&1

  git -C "$r" checkout -q main
  git -C "$r" merge -q --no-ff --no-edit feature -m "посадка ветки"
  local head; head=$(git -C "$r" rev-parse HEAD)
  git -C "$r" update-ref refs/remotes/origin/main "$head"

  local out rc
  out=$(cd "$r" && BEFORE="$base" HEAD_SHA="$head" bash -e -o pipefail "$JUDGE" 2>&1); rc=$?
  if [ "$rc" -ne 0 ] && [[ "$out" == *"ОТКАТ: lone.txt"* ]]; then
    ok "откат рядом с файлом ветки по-прежнему назван, и координата напечатана"
  else
    no "послабление пустой области съело настоящий откат (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 6в. Слепота осталась достижимой ──────────────────────────────────
# Признак беспредметности — не «drift.sh вышел двойкой», а его собственное
# machine-readable заявление. Отказ по другой причине (ревизия не разрешается)
# заявления не несёт и обязан по-прежнему читаться как слепота, иначе
# послабление стало бы всеразрешающим: любой сбой гейта сходил бы за «судить
# было нечего».
case_blindness_is_still_reachable() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/a.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)
  git -C "$r" checkout -q -b branch
  echo "ветка" > "$r/b.txt"; commit "$r" "ветка правит своё"
  local br; br=$(git -C "$r" rev-parse HEAD)
  git -C "$r" checkout -q main
  echo "ствол" > "$r/a.txt"; commit "$r" "ствол ушёл вперёд"
  local trunk; trunk=$(git -C "$r" rev-parse HEAD)

  local out rc
  out=$(cd "$r" && bash "$DRIFT" "$base" "$br" "$trunk" 0000000000000000000000000000000000000000 2>&1); rc=$?
  if [ "$rc" -eq 2 ] && ! any_line_matches "$out" '^no-surface:'; then
    ok "отказ не по беспредметности заявления о пустой области не несёт"
  else
    no "гейт заявил пустую область там, где он просто не смог посмотреть (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}


# ── Случай 7. «Полоса → линия» с настоящим откатом — гейт ОБЯЗАН заговорить ──
# Положительный контроль ко всем трём случаям ниже. Пока он красен, молчание
# случая 8 не значит ничего: оно неотличимо от гейта, разучившегося падать.
#
# Форма — штатная посадка полосы в накопительную линию: первый родитель линия,
# второй полоса. Сторона ствола совпадает с первым родителем, и совпадение это
# случайно — именно поэтому рядом стоит случай 8, где она НЕ совпадает.
case_lane_into_line_rollback_is_named() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/shared.txt"; echo "прочее" > "$r/other.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b release/line
  echo "ЛИНИЯ" > "$r/shared.txt"; commit "$r" "линия правит общий файл"
  local line_before; line_before=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b lane "$base"
  echo "правка полосы" > "$r/other.txt"; commit "$r" "полоса правит своё"
  local lane; lane=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q release/line
  git -C "$r" merge -q --no-ff --no-commit lane >/dev/null 2>&1
  echo "база" > "$r/shared.txt"          # ОТКАТ: посадка вернула содержимое базы
  commit "$r" "посадка полосы в линию"
  local head; head=$(git -C "$r" rev-parse HEAD)
  git -C "$r" update-ref refs/remotes/origin/main "$base"
  git -C "$r" update-ref refs/remotes/origin/release/line "$head"

  # Предпосылка случая: посадка обязана быть слиянием ровно с этими сторонами,
  # иначе он проверяет не то, что назван проверять.
  local p1 p2
  read -r _ p1 p2 <<<"$(git -C "$r" rev-list --parents -n1 "$head")"
  if [ "$p1" != "$line_before" ] || [ "$p2" != "$lane" ]; then
    no "предпосылка случая не выполнена: стороны посадки не в нужной форме (p1=$p1 p2=$p2)" ""
    rm -rf "$r"; return
  fi

  local out rc
  out=$(cd "$r" && BEFORE="$base" HEAD_SHA="$head" bash -e -o pipefail "$JUDGE" 2>&1); rc=$?
  if [ "$rc" -ne 0 ] && [[ "$out" == *"ОТКАТ: shared.txt"* ]]; then
    ok "посадка полосы в линию: настоящий откат назван, и координата напечатана"
  else
    no "настоящий откат на посадке полосы в линию НЕ пойман (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 8. «Линия → полоса»: приспособление СВОЕГО файла — молчание ───────
# Полоса, отставшая от линии, вливает линию В СЕБЯ, чтобы догнать, и заодно
# приводит собственный файл к формам, которые линия принесла. У такого слияния
# первый родитель — ПОЛОСА, и порядок родителей означает здесь обратное тому,
# что он означает в случае 7.
#
# Гейт, читающий порядок буквально, объявляет стволом полосу: всякий её
# собственный файл выглядит «ствол изменил, ветка не касалась», а приспособление
# — возвратом. Наблюдалось на посадке `3a66c8574e` линии modules-5: три файла
# названы откаченными, и НИ ОДНОГО из них в стволе не существует вовсе — их
# завела сама полоса.
#
# Различитель по первородительской цепи этого не спасал: он знал одну опорную
# линию, `origin/main`, а накопительная линия в ствол ещё не влита. На всём том
# прогоне он молчал «родителей на линии ствола: 0» и отдавал решение обратно
# порядку родителей — 23 посадки из 24.
case_line_into_lane_adaptation_is_silent() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/common.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b release/line
  echo "ЛИНИЯ" > "$r/common.txt"; commit "$r" "линия ушла вперёд"
  local line; line=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b lane "$base"
  echo "ПОЛОСА" > "$r/lanefile.txt"; commit "$r" "полоса завела свой файл"
  local lane_before; lane_before=$(git -C "$r" rev-parse HEAD)

  # Полоса догоняет линию и приспосабливает СВОЙ файл к её формам.
  git -C "$r" merge -q --no-ff --no-commit release/line >/dev/null 2>&1
  echo "ПОЛОСА, ПРИСПОСОБЛЕНО К ФОРМАМ ЛИНИИ" > "$r/lanefile.txt"
  commit "$r" "полоса догнала линию и приспособила своё"
  local head; head=$(git -C "$r" rev-parse HEAD)
  git -C "$r" update-ref refs/remotes/origin/main "$base"
  git -C "$r" update-ref refs/remotes/origin/release/line "$line"

  # Предпосылка: сторона ствола обязана быть ВТОРЫМ родителем и лежать на
  # опорной линии, а первый родитель — не лежать. Иначе случай зеленел бы, не
  # коснувшись предмета: ровно так прошла первая редакция случая 5.
  local p1 p2 chain
  read -r _ p1 p2 <<<"$(git -C "$r" rev-list --parents -n1 "$head")"
  chain=$(git -C "$r" rev-list --first-parent refs/remotes/origin/release/line)
  if [ "$p1" != "$lane_before" ] || [ "$p2" != "$line" ] \
     || ! line_in "$chain" "$p2" || line_in "$chain" "$p1"; then
    no "предпосылка случая не выполнена: стороны не в нужной форме (p1=$p1 p2=$p2)" ""
    rm -rf "$r"; return
  fi

  local out rc
  out=$(cd "$r" && BEFORE="$base" HEAD_SHA="$head" bash -e -o pipefail "$JUDGE" 2>&1); rc=$?
  # Молчания МАЛО: судья молчит и тогда, когда сторону установить нечем, —
  # а это другой исход, и он тут был бы неверным. Поэтому случай требует
  # молчания ПО ПРАВИЛЬНОЙ ПРИЧИНЕ: стороны обязаны быть определены, и
  # стволом обязан быть взят не первый родитель.
  if [ "$rc" -eq 0 ] && [[ "$out" != *"ОТКАТ"* ]] \
     && [[ "$out" == *"стороны определены по графу"* ]] \
     && [[ "$out" != *"сторону ствола установить нечем"* ]]; then
    ok "слияние линии в полосу: приспособление своего файла находкой не названо"
  else
    no "файл ПОЛОСЫ назван откатом ствола либо сторона не определена (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 9. Сторону ствола установить нечем — ТРЕТЬЯ КАТЕГОРИЯ ─────────────
# Ни один родитель не лежит на опорной линии: две полосы слиты между собой, и
# ни одна из них ещё никуда не влита. Граф на вопрос не отвечает.
#
# Прежний судья в этом месте говорил «берём первого родителя» и ВЫНОСИЛ вердикт
# — то есть решал молча ровно там, где сам объявил, что различить не может.
# Вердикт не выносится: «сторону ствола установить нечем» есть НЕ находка, и
# перепись обязана назвать такие посадки отдельным числом, иначе они растворятся
# в «сверено чисто».
#
# ПОСАДОК В СЛУЧАЕ ДВЕ, и это не украшение. Прогон, где неразличима ЕДИНСТВЕННАЯ
# посадка, — другой исход (случай 11): там гейт не высказался вообще ни о чём.
# Здесь же рядом стоит разрешимая посадка, и случай требует, чтобы соседство
# неразличимой её не съело: сверено чисто обязано остаться единицей.
case_trunk_side_undecidable_is_not_a_finding() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/shared.txt"; echo "прочее" > "$r/other.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b release/line
  echo "ЛИНИЯ" > "$r/shared.txt"; commit "$r" "линия правит общий файл"

  git -C "$r" checkout -q -b lane "$base"
  echo "полоса" > "$r/other.txt"; commit "$r" "полоса правит своё"

  # Посадка первая — разрешимая и чистая.
  git -C "$r" checkout -q release/line
  git -C "$r" merge -q --no-ff --no-edit lane -m "посадка полосы в линию" >/dev/null 2>&1
  local line_tip; line_tip=$(git -C "$r" rev-parse HEAD)

  # Посадка вторая — две полосы, слитые между собой; ни одна не влита никуда.
  git -C "$r" checkout -q -b first "$line_tip"
  echo "первая" > "$r/first.txt"; commit "$r" "первая полоса"
  git -C "$r" checkout -q -b second "$line_tip"
  echo "вторая" > "$r/second.txt"; commit "$r" "вторая полоса"
  git -C "$r" checkout -q first
  git -C "$r" merge -q --no-ff --no-edit second -m "две полосы слиты между собой" >/dev/null 2>&1
  local head; head=$(git -C "$r" rev-parse HEAD)

  git -C "$r" update-ref refs/remotes/origin/main "$base"
  git -C "$r" update-ref refs/remotes/origin/release/line "$line_tip"

  # Предпосылка: у второй посадки ни один родитель не лежит на опорных линиях,
  # а у первой — ровно один. Иначе случай проверяет не то, что назван.
  local p1 p2 chain
  read -r _ p1 p2 <<<"$(git -C "$r" rev-list --parents -n1 "$head")"
  chain="$(git -C "$r" rev-list --first-parent refs/remotes/origin/release/line)"$'\n'"$(git -C "$r" rev-list --first-parent refs/remotes/origin/main)"
  if line_in "$chain" "$p1" || line_in "$chain" "$p2"; then
    no "предпосылка случая не выполнена: сторона всё-таки лежит на опорной линии" ""
    rm -rf "$r"; return
  fi

  local out rc
  out=$(cd "$r" && BEFORE="$base" HEAD_SHA="$head" bash -e -o pipefail "$JUDGE" 2>&1); rc=$?
  if [ "$rc" -eq 0 ] \
     && [[ "$out" == *"сторону ствола установить нечем"* ]] \
     && [[ "$out" != *"ОТКАТ"* ]] \
     && any_line_matches "$out" 'сторона ствола не установлена 1' \
     && any_line_matches "$out" 'сверено чисто 1'; then
    ok "неразличимая сторона названа третьей категорией и сосчитана отдельно"
  else
    no "судья вынес вердикт там, где сторону ствола установить нечем (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 11. НИ ОДНА посадка прогона не получила вердикта ──────────────────
# Близнец к случаю 9 и граница послабления, которое тот вводит. Там неразличимая
# посадка стояла рядом с разрешимой, и гейт высказался о второй. Здесь вердикта
# не получила НИ ОДНА — гейт отработал, ничего не проверив, и остался бы на вид
# зелёным.
#
# Форма не выдумана: ровно так выглядел бы этот гейт, если опорные линии в дереве
# ссылок есть, а работы прогона на них нет — например, накопительные линии
# переименовали, а образец опорных ссылок за ними не пошёл. До починки судья в
# такой раскладке молча «брал первого родителя» и судил не ту сторону; после
# починки он обязан сказать, что не судил ничего, а не промолчать зелёным.
case_nothing_judged_at_all_is_loud() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/a.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b first "$base"
  echo "первая" > "$r/first.txt"; commit "$r" "первая полоса"
  git -C "$r" checkout -q -b second "$base"
  echo "вторая" > "$r/second.txt"; commit "$r" "вторая полоса"
  git -C "$r" checkout -q first
  git -C "$r" merge -q --no-ff --no-edit second -m "две полосы слиты между собой" >/dev/null 2>&1
  local head; head=$(git -C "$r" rev-parse HEAD)
  # Опорная линия ЕСТЬ — иначе сработал бы случай 10, другой предмет.
  git -C "$r" update-ref refs/remotes/origin/main "$base"

  local out rc
  out=$(cd "$r" && BEFORE="$base" HEAD_SHA="$head" bash -e -o pipefail "$JUDGE" 2>&1); rc=$?
  if [ "$rc" -ne 0 ] \
     && [[ "$out" == *"ни одна посадка прогона не получила вердикта"* ]] \
     && [[ "$out" != *"ОТКАТ"* ]]; then
    ok "прогон, в котором не судили ничего, назван вслух, а не выдан за чистый"
  else
    no "гейт вышел зелёным, не вынеся вердикта ни по одной посадке (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 10. Опорных линий НЕТ ВОВСЕ — обход пуст, и гейт падает ───────────
# Близнец к случаю 9 и граница послабления, которое он вводит. Там граф не
# отвечал на вопрос о КОНКРЕТНОЙ посадке; здесь спрашивать не у чего вообще:
# ни `main`, ни накопительной линии в дереве ссылок нет.
#
# Если бы этот исход тоже становился третьей категорией, послабление стало бы
# всеразрешающим: мелкий клон без ссылок дал бы «вердикт не выносится» на КАЖДОЙ
# посадке, и гейт молчал бы всегда, оставаясь на вид рабочим.
case_no_trunk_reference_is_an_empty_traversal() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/a.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b lane "$base"
  echo "полоса" > "$r/lane.txt"; commit "$r" "полоса правит своё"
  git -C "$r" checkout -q -b other "$base"
  echo "другая" > "$r/other.txt"; commit "$r" "другая полоса"
  git -C "$r" checkout -q lane
  git -C "$r" merge -q --no-ff --no-edit other -m "слияние" >/dev/null 2>&1
  local head; head=$(git -C "$r" rev-parse HEAD)

  # Ни одной опорной ссылки: ни удалённой, ни локальной.
  git -C "$r" checkout -q --detach "$head"
  git -C "$r" branch -q -D main lane other >/dev/null 2>&1
  if [ -n "$(git -C "$r" for-each-ref --format='%(refname)' \
              refs/heads/main refs/remotes/origin/main \
              'refs/heads/release/*' 'refs/remotes/origin/release/*')" ]; then
    no "предпосылка случая не выполнена: опорная ссылка всё-таки осталась" ""
    rm -rf "$r"; return
  fi

  local out rc
  out=$(cd "$r" && BEFORE="$base" HEAD_SHA="$head" bash -e -o pipefail "$JUDGE" 2>&1); rc=$?
  if [ "$rc" -ne 0 ] && [[ "$out" == *"опорной линии в дереве ссылок нет"* ]]; then
    ok "пустой обход опорных линий назван, и гейт на нём падает"
  else
    no "гейт судил, не имея ни одной опорной линии (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

# ── Общий граф для случаев 15-17: ЛИНИЯ ИЗ ДВУХ ПОСАДОК, две отправки подряд ─
#
# Это раскладка, ради которой гейт и написан: накопительная линия копит посадки,
# и каждая отправка несёт лишь ПРИРОСТ с прошлого раза. Граф строится один и тот
# же, различается только то, что делает первая посадка со своим файлом.
#
# Возвращает три ревизии через пробел: <C0> <L1> <L2>.
#   mode=declared — первая посадка СНИМАЕТ A.txt, и перечень это объявляет
#   mode=rollback — первая посадка ВОЗВРАЩАЕТ содержимое базы (настоящий откат)
#   mode=clean    — первая посадка ничего не портит
# ledger — содержимое перечня (пустая строка = перечня нет вовсе).
build_two_push_line() { # <repo> <mode> <ledger-line>
  local r=$1 mode=$2 ledger=$3
  echo keep > "$r/keep.txt"
  echo "база" > "$r/shared.txt"
  if [ -n "$ledger" ]; then
    mkdir -p "$r/tools/carrydrift"
    printf '%s\n' "$ledger" > "$r/tools/carrydrift/declared-removals.txt"
  fi
  commit "$r" "C0 база"
  local c0; c0=$(git -C "$r" rev-parse HEAD)

  # Полоса 1 отходит от базы и правит ТОЛЬКО своё.
  git -C "$r" checkout -q -b lane1 "$c0"
  echo l1 > "$r/lane1.txt"; commit "$r" "полоса 1 правит своё"

  # Линия двигается после того, как полоса от неё отошла, — иначе у гейта нет
  # области и вердикт беспредметен.
  git -C "$r" checkout -q -b release/l "$c0"
  case "$mode" in
    declared) echo A > "$r/A.txt" ;;
    *)        echo "ПРАВКА ЛИНИИ" > "$r/shared.txt" ;;
  esac
  commit "$r" "C1 линия двигается"

  # ПОСАДКА 1.
  git -C "$r" merge -q --no-commit --no-ff lane1 >/dev/null 2>&1
  case "$mode" in
    declared) rm -f "$r/A.txt" ;;
    rollback) echo "база" > "$r/shared.txt" ;;
  esac
  git -C "$r" add -A
  git -C "$r" commit -qm "посадка 1 (${mode})"
  local l1; l1=$(git -C "$r" rev-parse HEAD)

  # Полоса 2 отходит от базы; посадка 2 чистая.
  git -C "$r" checkout -q -b lane2 "$c0"
  echo l2 > "$r/lane2.txt"; commit "$r" "полоса 2 правит своё"
  git -C "$r" checkout -q release/l
  git -C "$r" merge -q --no-ff -m "посадка 2 (чистая)" lane2 >/dev/null 2>&1
  local l2; l2=$(git -C "$r" rev-parse HEAD)

  echo "$c0 $l1 $l2"
}

# run_push <repo> <before|-> <head> — прогон судьи так, как его зовёт конвейер:
# рабочая копия стоит на голове отправки, а накопительная линия на неё указывает.
run_push() {
  local r=$1 before=$2 head=$3
  git -C "$r" checkout -q "$head"
  git -C "$r" branch -f release/l "$head"
  [ "$before" = "-" ] && before=""
  (cd "$r" && BEFORE="$before" HEAD_SHA="$head" bash "$JUDGE" 2>&1)
}

# ── Случай 15. Запись перечня живёт, пока жива ПОСАДКА, а не пока её несёт ───
#              диапазон прогона
#
# Перечень объявленных снятий описывает ОТПРАВКУ целиком — так говорит шапка
# `drift.sh`, и так же устроен сбор `declared-used` по всем посадкам. Но
# самоистечение судилось ДИАПАЗОНОМ прогона, а диапазон инкрементальной отправки
# содержит только посадки, добавленные с прошлого раза. Значит запись,
# сработавшая законно, на СЛЕДУЮЩЕЙ отправке объявлялась пережившей предмет и
# роняла гейт кодом 3 — при том что не изменилось ничего.
#
# Следствие: механизм объявления был неприменим на накопительной линии —
# единственной раскладке, ради которой он и нужен (задача #1911).
case_declaration_outlives_the_push_that_used_it() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  local revs; revs=$(build_two_push_line "$r" declared \
    "A.txt  снят решением: линия и ствол завели один предмет разными файлами")
  read -r c0 l1 l2 <<<"$revs"

  local out1 rc1 out2 rc2
  out1=$(run_push "$r" "$c0" "$l1"); rc1=$?
  out2=$(run_push "$r" "$l1" "$l2"); rc2=$?

  if [ "$rc1" -eq 0 ] && [[ "$out1" == *"снято решением: A.txt"* ]] \
     && [ "$rc2" -eq 0 ] && [[ "$out2" != *"ПЕРЕЖИЛА ПРЕДМЕТ"* ]]; then
    ok "запись перечня пережила отправку, в которой сработала"
  else
    no "запись живёт ровно один прогон (rc1=$rc1 rc2=$rc2)" "$out2"
  fi
  rm -rf "$r"
}

# ── Случай 16. Близнец к 15: запись, у которой предмета нет НИ В ОДНОЙ ───────
#              посадке линии, по-прежнему краснеет
#
# Без этого случая починка 15 неотличима от снятия самоистечения вовсе. Запись,
# которой нечего исключать, обязана оставаться находкой: иначе она молча прикроет
# следующее снятие — ровно тот класс, ради которого перечень и заведён.
case_declaration_without_subject_anywhere_still_reds() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  local revs; revs=$(build_two_push_line "$r" clean \
    "never/removed.txt  предмета нет ни в одной посадке этой линии")
  read -r c0 l1 l2 <<<"$revs"

  local out1 rc1 out2 rc2
  out1=$(run_push "$r" "$c0" "$l1"); rc1=$?
  out2=$(run_push "$r" "$l1" "$l2"); rc2=$?

  if [ "$rc1" -eq 3 ] && [ "$rc2" -eq 3 ] \
     && [[ "$out2" == *"ПЕРЕЖИЛА ПРЕДМЕТ"* ]] \
     && [[ "$out2" == *"never/removed.txt"* ]]; then
    ok "запись без предмета во всей линии по-прежнему названа и роняет гейт"
  else
    no "самоистечение перечня перестало работать (rc1=$rc1 rc2=$rc2)" "$out2"
  fi
  rm -rf "$r"
}

# ── Случай 17. НАХОДКА тоже переживает следующую отправку ────────────────────
#
# Та же единица счёта, но цена выше на порядок. Откат, найденный в приросте одной
# отправки, на следующей не пересматривался вовсе: диапазон его больше не
# содержал, и линия зеленела, неся живой откат до самого слияния в ствол.
#
# То есть дефект работал ДВОЙНЫМ ДНОМ: он же и служил единственным способом
# погасить красное — достаточно было толкнуть что угодно ещё раз.
case_finding_outlives_the_push_that_found_it() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  local revs; revs=$(build_two_push_line "$r" rollback "")
  read -r c0 l1 l2 <<<"$revs"

  local out1 rc1 out2 rc2
  out1=$(run_push "$r" "$c0" "$l1"); rc1=$?
  out2=$(run_push "$r" "$l1" "$l2"); rc2=$?

  if [ "$rc1" -eq 1 ] && [[ "$out1" == *"ОТКАТ: shared.txt"* ]] \
     && [ "$rc2" -eq 1 ] && [[ "$out2" == *"ОТКАТ: shared.txt"* ]]; then
    ok "живой откат линии назван и на следующей отправке"
  else
    no "находка живёт ровно один прогон (rc1=$rc1 rc2=$rc2)" "$out2"
  fi
  rm -rf "$r"
}

# ── Случай 18. Охват не зависит от того, каким событием пришёл прогон ────────
#
# `github.event.before` есть у `push` и у `pull_request` с действием
# `synchronize`, но НЕ у `opened`/`reopened`. На боевых прогонах наблюдались обе
# формы: «диапазон прогона: X..Y» и «диапазона нет … судится вершина». То есть
# охват гейта был функцией события, а не того, что в линии накоплено: на
# `opened` судилась ОДНА вершина при любом числе посадок.
case_coverage_does_not_depend_on_the_event() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  local revs; revs=$(build_two_push_line "$r" clean "")
  read -r c0 l1 l2 <<<"$revs"

  local out rc
  out=$(run_push "$r" - "$l2"); rc=$?
  if [ "$rc" -eq 0 ] && [[ "$out" == *"посадок в прогоне 2"* ]]; then
    ok "без диапазона в событии судится вся линия, а не одна вершина"
  else
    no "охват остался функцией события (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

# ── Случай 19. Близнец к 18: НА САМОМ СТВОЛЕ выводить диапазон не из чего ────
#
# Граница послабления, которое вводит случай 18. Когда голова прогона стоит на
# стволе, линии, которую она добавляет к стволу, не существует by construction —
# выведенный диапазон пуст. Тогда и только тогда берётся прирост события, и этот
# путь обязан остаться живым: иначе push в `main` перестал бы судиться вовсе, а
# заметить это было бы нечем — гейт остался бы зелёным.
case_on_the_trunk_itself_the_event_range_is_used() {
  cases=$((cases + 1))
  local r; r=$(newrepo)
  echo "база" > "$r/shared.txt"; commit "$r" "база"
  local base; base=$(git -C "$r" rev-parse HEAD)

  git -C "$r" checkout -q -b lane "$base"
  echo l > "$r/lane.txt"; commit "$r" "полоса правит своё"
  git -C "$r" checkout -q main
  echo "ПРАВКА СТВОЛА" > "$r/shared.txt"; commit "$r" "ствол двигается"
  git -C "$r" merge -q --no-ff -m "посадка в ствол" lane >/dev/null 2>&1
  local head; head=$(git -C "$r" rev-parse HEAD)

  # Голова — сам ствол: `main` указывает на неё, накопительных линий нет.
  local out rc
  out=$(cd "$r" && BEFORE="$base" HEAD_SHA="$head" bash "$JUDGE" 2>&1); rc=$?
  if [ "$rc" -eq 0 ] \
     && [[ "$out" == *"посадок в прогоне 1"* ]] \
     && [[ "$out" == *"прирост события"* ]]; then
    ok "на самом стволе судится прирост события, и это сказано вслух"
  else
    no "прирост события на стволе перестал судиться (rc=$rc)" "$out"
  fi
  rm -rf "$r"
}

echo "проба гейта переноса — синтетические графы"
case_real_rollback
case_branch_owns_file
case_content_moved
case_content_truly_gone
case_declared_not_judged_per_landing
case_trunk_is_second_parent
case_empty_surface_is_not_blindness
case_rollback_survives_the_empty_surface_relief
case_blindness_is_still_reachable
case_lane_into_line_rollback_is_named
case_line_into_lane_adaptation_is_silent
case_trunk_side_undecidable_is_not_a_finding
case_no_trunk_reference_is_an_empty_traversal
case_nothing_judged_at_all_is_loud
case_declaration_outlives_the_push_that_used_it
case_declaration_without_subject_anywhere_still_reds
case_finding_outlives_the_push_that_found_it
case_coverage_does_not_depend_on_the_event
case_on_the_trunk_itself_the_event_range_is_used

echo
echo "перепись: случаев ${cases}; прошло ${pass}; упало ${fail}"
[ "$fail" -eq 0 ] || exit 1
