#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/setup_selftest.sh — самопроверка посева суиты (setup.sh).
#
# ПРЕДМЕТ. Посев публикует id фикстуры в файл окружения. Дефект этого класса
# тихий: посев отчитывается успехом, записывает ПУСТОЕ значение, суита падает
# через двадцать минут и далеко от места, где посев не состоялся. Поэтому
# «опубликовал» обязано означать «непустое значение по каждому имени», а не
# «вызов вернул ноль».
#
# ЧТО ДОКАЗЫВАЕТСЯ, В ОБЕ СТОРОНЫ:
#   * отсутствующая переменная  → отказ, и он НАЗЫВАЕТ имя;
#   * пустая переменная         → отказ (законный близнец: ключ ЕСТЬ, значение
#                                 пустое — предикат «ключ присутствует» здесь
#                                 зеленеет, предикат «значение непусто» обязан
#                                 краснеть);
#   * пробельная переменная     → отказ (та же форма, что пустая);
#   * полный набор              → проход и ТОЧНЫЕ пары на выходе;
#   * имена папок посева читаются из генератора и НЕПУСТЫ — иначе newman получил
#     бы пустую точку входа, не исполнил бы ничего и промолчал.
#
# Стенд и newman не нужны.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NEWMAN_DIR="$(cd "$HERE/.." && pwd)"

# shellcheck source=../setup.sh
source "$NEWMAN_DIR/setup.sh"

if ! declare -F anchor_ids_from_export >/dev/null 2>&1; then
  echo "SELFTEST FAIL: не удалось подгрузить anchor_ids_from_export из setup.sh —" >&2
  echo "  проверять нечего, и это отказ, а не пропуск." >&2
  exit 2
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

FAILURES=0
check() { # <имя> <ok:0|1> [деталь]
  if [[ "$2" == "0" ]]; then
    echo "ok   — $1"
  else
    echo "FAIL — $1${3:+  :: $3}"
    FAILURES=$((FAILURES + 1))
  fi
}

mk_env() { # <файл> <json-массив values>
  printf '{"id":"x","name":"x","values":%s,"_postman_variable_scope":"environment"}\n' "$2" > "$1"
}

# 1. ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Без него всё ниже зеленело бы и на функции,
#    отвергающей вообще всё.
mk_env "$TMP/ok.json" '[{"key":"gwAnchorNetId","value":"net-1"},{"key":"gwAnchorSubId","value":"sub-1"}]'
out="$(anchor_ids_from_export "$TMP/ok.json" gwAnchorNetId gwAnchorSubId 2>"$TMP/err")"
rc=$?
check "полный набор — проход" "$rc" "$(cat "$TMP/err")"
check "на выходе точные пары" \
  "$([[ "$out" == $'gwAnchorNetId=net-1\ngwAnchorSubId=sub-1' ]] && echo 0 || echo 1)" "$out"

# 2. ИНЪЕКЦИЯ: переменной нет вовсе.
mk_env "$TMP/missing.json" '[{"key":"gwAnchorNetId","value":"net-1"}]'
anchor_ids_from_export "$TMP/missing.json" gwAnchorNetId gwAnchorSubId >/dev/null 2>"$TMP/err"
check "отсутствующая переменная — отказ" "$([[ $? -ne 0 ]] && echo 0 || echo 1)"
grep -q "gwAnchorSubId" "$TMP/err"
check "отказ НАЗЫВАЕТ имя" "$?" "$(cat "$TMP/err")"

# 3. ИНЪЕКЦИЯ ЗАКОННЫМ БЛИЗНЕЦОМ: ключ ЕСТЬ, значение пустое. Ровно тот вид,
#    который проходит проверку «ключ присутствует» и роняет суиту позже.
mk_env "$TMP/empty.json" '[{"key":"gwAnchorNetId","value":"net-1"},{"key":"gwAnchorSubId","value":""}]'
anchor_ids_from_export "$TMP/empty.json" gwAnchorNetId gwAnchorSubId >/dev/null 2>"$TMP/err"
check "пустое значение — отказ" "$([[ $? -ne 0 ]] && echo 0 || echo 1)" "$(cat "$TMP/err")"

# 4. Та же форма пробелами — «непусто» не должно означать «непустая строка».
mk_env "$TMP/blank.json" '[{"key":"gwAnchorNetId","value":"net-1"},{"key":"gwAnchorSubId","value":"   "}]'
anchor_ids_from_export "$TMP/blank.json" gwAnchorNetId gwAnchorSubId >/dev/null 2>"$TMP/err"
check "пробельное значение — отказ" "$([[ $? -ne 0 ]] && echo 0 || echo 1)" "$(cat "$TMP/err")"

# 5. Файла выгрузки нет: newman не дошёл до экспорта.
anchor_ids_from_export "$TMP/nope.json" gwAnchorSubId >/dev/null 2>"$TMP/err"
check "отсутствующая выгрузка — отказ" "$([[ $? -ne 0 ]] && echo 0 || echo 1)"

# 6. Мусор вместо JSON — разбор обязан отказать, а не выдать пустоту за набор.
echo "not json at all" > "$TMP/garbage.json"
anchor_ids_from_export "$TMP/garbage.json" gwAnchorSubId >/dev/null 2>"$TMP/err"
check "неразбираемая выгрузка — отказ" "$([[ $? -ne 0 ]] && echo 0 || echo 1)"

# 7. Сборка payload'а для патча.
fixtures_json "$TMP/fx.json" "gwAnchorNetId=net-1" "gwAnchorSubId=sub-1" 2>"$TMP/err"
check "payload собирается" "$?" "$(cat "$TMP/err")"
python3 -c "
import json,sys
d=json.load(open('$TMP/fx.json'))
sys.exit(0 if d=={'gwAnchorNetId':'net-1','gwAnchorSubId':'sub-1'} else 1)"
check "payload несёт ровно те пары" "$?"

fixtures_json "$TMP/fx2.json" "gwAnchorSubId=" >/dev/null 2>&1
check "пустое значение в payload — отказ" "$([[ $? -ne 0 ]] && echo 0 || echo 1)"

fixtures_json "$TMP/fx3.json" "не-пара" >/dev/null 2>&1
check "аргумент без '=' — отказ" "$([[ $? -ne 0 ]] && echo 0 || echo 1)"

# 8. ПРЕДПОСЫЛКА ПОСЕВА: имена точек входа читаются из генератора и непусты.
#    Пустая точка входа — это newman, который не исполнит ничего и не скажет ни
#    слова: посев выглядел бы прошедшим.
for c in GW_ANCHOR_FOLDER ZONES_SETUP_ITEM GW_ANCHOR_NET_VAR GW_ANCHOR_SUBNET_VAR; do
  v="$(gen_const "$c" 2>"$TMP/err")"
  check "константа $c читается из gen.py и непуста" \
    "$([[ -n "$v" ]] && echo 0 || echo 1)" "$(cat "$TMP/err")"
done

# 9. Точка входа якоря обязана СУЩЕСТВОВАТЬ в сгенерированной коллекции — иначе
#    `--folder` называет папку, которой нет, и посев ничего не исполняет.
folder="$(gen_const GW_ANCHOR_FOLDER)"
python3 - "$NEWMAN_DIR/collections/gateway.postman_collection.json" "$folder" <<'PY'
import json, sys
col, folder = sys.argv[1], sys.argv[2]
try:
    items = json.load(open(col)).get("item", [])
except Exception:                              # noqa: BLE001
    sys.exit(2)
hit = [i for i in items if i.get("name") == folder]
if len(hit) != 1 or not hit[0].get("item"):
    sys.exit(1)
sys.exit(0)
PY
check "папка якоря есть в коллекции ровно одна и непуста" "$?" "folder=$folder"

if [[ "$FAILURES" -gt 0 ]]; then
  echo
  echo "SELFTEST FAIL: $FAILURES проверк(и/а) не прошли." >&2
  exit 1
fi
echo
echo "SELFTEST OK — посев отказывает на пустом/отсутствующем id и проходит на полном наборе."
