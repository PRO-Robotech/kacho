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
newrepo() {
  local d
  d=$(mktemp -d)
  git -C "$d" init -q -b main
  git -C "$d" config user.email proba@example.invalid
  git -C "$d" config user.name  proba
  git -C "$d" config commit.gpgsign false
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
  if [ "$rc" -ne 0 ] && echo "$out" | grep -q "ОТКАТ: shared.txt"; then
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
  if [ "$rc" -eq 0 ] && ! echo "$out" | grep -q "ОТКАТ"; then
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
  if [ "$rc" -eq 0 ] && ! echo "$out" | grep -q "ОТКАТ"; then
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
  if [ "$rc" -ne 0 ] && echo "$out" | grep -q "0087.sql"; then
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
     || ! echo "$chain" | grep -q "^${p2}$" \
     || echo "$chain" | grep -q "^${p1}$"; then
    no "предпосылка случая не выполнена: стороны не в нужной форме (p1=$p1 p2=$p2)" ""
    rm -rf "$r"; return
  fi

  local out rc
  out=$(cd "$r" && BEFORE="$base" HEAD_SHA="$head" bash -e -o pipefail "$JUDGE" 2>&1); rc=$?
  if echo "$out" | grep -q "drift: посадок в прогоне"; then
    ok "судья дошёл до переписи на слиянии, где ствол — второй родитель"
  else
    no "судья умер, не напечатав переписи (rc=$rc): «находки» неотличимы от «не дошли»" "$out"
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

echo
echo "перепись: случаев ${cases}; прошло ${pass}; упало ${fail}"
[ "$fail" -eq 0 ] || exit 1
