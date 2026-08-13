#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/setup.sh — посев суиты: фикстуры, которые обязаны существовать ДО
# первого кейса, и чьи id читает окружение.
#
# Сегодня посев ровно один — ЯКОРЬ РАЗМЕЩЕНИЯ СУИТЫ ШЛЮЗОВ (сеть + зональная
# подсеть с IPv4). Шлюз без подсети не создаётся вовсе, поэтому почти каждый кейс
# суиты `gateway` читает `gwAnchorSubId`.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ ЭТОТ ФАЙЛ СУЩЕСТВУЕТ
#
# Якорь заводился ПЕРВЫМ КЕЙСОМ коллекции. newman исполняет только названную
# точку входа, поэтому прогон одиночного кейса (`--folder <кейс>`) якоря не
# получал: тело запроса уезжало с неразрешённым `{{gwAnchorSubId}}`, кейс падал
# по чужой причине, и виноватым выглядел невиновный. То есть кейс, зелёный в
# полной суите, был красен ровно в том режиме, в котором его отлаживают.
#
# Теперь якорь — setup-папка коллекции (`gen.py`: GW_ANCHOR_FOLDER), а этот файл
# гоняет ЕЁ ЖЕ и выгружает полученные id в файл окружения. Объявление якоря
# остаётся ОДНО: здесь нет ни своего создания сети, ни своего создания подсети,
# ни своей петли опроса операции — разойтись с суитой этому файлу негде.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ЗДЕСЬ СЧИТАЕТСЯ УСПЕХОМ
#
# Не «newman отработал», а ЧИСЛА: посев исполнил запросы, все утверждения прошли,
# ни один запрос не остался без ответа — и в выгруженном окружении лежат НЕПУСТЫЕ
# id. Вердикт выносит та же функция, что судит прогон суиты (`scripts/run.sh`
# `aggregate_verdict`), — второй реализации вердикта здесь нет.
#
# Пустой id — отдельный исход, и он ОТКАЗ, а не «ну ладно»: молча записанное в
# окружение пустое значение уезжает в тела запросов, и суита падает далеко от
# места, где посев не состоялся. Это ровно тот класс, из-за которого фикстуры
# обязаны проверять исход, а не факт вызова.
#
# Usage:
#   ./setup.sh                       # посеять и пропатчить окружение
#   ENV=environments/other.json ./setup.sh
#
# Требует: newman в PATH, python3, доступный край по baseUrl из окружения
# (локально — port-forward на 18080) и заполненные фикстурой токены.

set -uo pipefail

NEWMAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$NEWMAN_DIR/../../../.." && pwd)"
PATCH_ENV_PY="$REPO_ROOT/tests/authz-fixtures/patch-env.py"

# ---------------------------------------------------------------------------
# Чистые функции (их гоняет scripts/setup_selftest.sh — без стенда и без newman)
# ---------------------------------------------------------------------------

# anchor_ids_from_export <exported-env.json> <var>...
#
# Печатает `<var>=<значение>` по строке на переменную. Возвращает 1, если файла
# нет, он не разбирается, переменной нет или её значение ПУСТО.
#
# Пустое значение — не «мягкий» случай: окружение с пустым id неотличимо для
# суиты от посева, которого не было, но при этом выглядит как успешно
# записанное. Отказ здесь дешевле красной суиты через двадцать минут.
anchor_ids_from_export() {
  local export_file="$1"; shift
  [[ -f "$export_file" ]] || {
    echo "[setup] выгруженного окружения нет: $export_file" >&2
    return 1
  }
  python3 - "$export_file" "$@" <<'PY'
import json, sys
path, names = sys.argv[1], sys.argv[2:]
try:
    data = json.loads(open(path).read())
except Exception as exc:                      # noqa: BLE001
    print(f"[setup] выгруженное окружение не разбирается: {exc}", file=sys.stderr)
    sys.exit(1)
values = {v.get("key"): v.get("value") for v in data.get("values", [])}
missing = [n for n in names if not str(values.get(n) or "").strip()]
if missing:
    print("[setup] посев не опубликовал: " + ", ".join(missing), file=sys.stderr)
    print("[setup]   пустой id — это НЕ выполнилось, а не «прошло»: записанный в", file=sys.stderr)
    print("[setup]   окружение, он уехал бы в тела запросов и уронил суиту далеко", file=sys.stderr)
    print("[setup]   от места, где посев не состоялся.", file=sys.stderr)
    sys.exit(1)
for n in names:
    print(f"{n}={values[n]}")
PY
}

# fixtures_json <out-file> <var=value>...
#
# Собирает payload для patch-env.py. Отдельной функцией, потому что её проверяет
# самопроверка: пара, потерянная здесь, дала бы окружение без якоря при зелёном
# посеве.
fixtures_json() {
  local out="$1"; shift
  python3 - "$out" "$@" <<'PY'
import json, sys
out, pairs = sys.argv[1], sys.argv[2:]
data = {}
for p in pairs:
    if "=" not in p:
        print(f"[setup] не пара ключ=значение: {p!r}", file=sys.stderr)
        sys.exit(1)
    k, v = p.split("=", 1)
    if not v.strip():
        print(f"[setup] пустое значение у {k!r}", file=sys.stderr)
        sys.exit(1)
    data[k] = v
if not data:
    print("[setup] нечего патчить — ноль пар", file=sys.stderr)
    sys.exit(1)
open(out, "w").write(json.dumps(data, indent=2, ensure_ascii=False) + "\n")
PY
}

# gen_const <ИМЯ> — значение константы из scripts/gen.py.
#
# Имена папок посева и переменных объявлены в генераторе и читаются оттуда:
# литерал в оболочке и литерал в генераторе разошлись бы МОЛЧА — newman на
# ненайденную точку входа ничего не утверждает, и посев выглядел бы прошедшим,
# не исполнив ни одного запроса.
gen_const() {
  python3 -c "import sys; sys.path.insert(0, '$NEWMAN_DIR/scripts'); sys.argv=[sys.argv[0]]; import gen; print(getattr(gen, '$1'))"
}

# ---------------------------------------------------------------------------
# Посев
# ---------------------------------------------------------------------------

main() {
  cd "$NEWMAN_DIR" || exit 2

  ENV="${ENV:-environments/local.postman_environment.json}"
  if [[ ! -f "$ENV" && -f "${ENV%.json}.template.json" ]]; then
    cp "${ENV%.json}.template.json" "$ENV"
    echo "[setup] создан $ENV из шаблона (креды допишет fixture-seed)" >&2
  fi
  [[ -f "$ENV" ]] || { echo "[setup] нет файла окружения: $ENV" >&2; exit 1; }

  command -v newman >/dev/null 2>&1 || {
    echo "[setup] FATAL: newman не найден в PATH — посев не выполнялся." >&2
    echo "[setup]        Это отказ ОСНАСТКИ, а не свойство продукта; ставится" >&2
    echo "[setup]        через npm install -g newman." >&2
    exit 2
  }

  local collection="collections/gateway.postman_collection.json"
  [[ -f "$collection" ]] || {
    echo "[setup] нет коллекции $collection — сначала python3 scripts/gen.py" >&2
    exit 1
  }

  local folder zones net_var sub_var
  folder="$(gen_const GW_ANCHOR_FOLDER)" || exit 2
  zones="$(gen_const ZONES_SETUP_ITEM)" || exit 2
  net_var="$(gen_const GW_ANCHOR_NET_VAR)" || exit 2
  sub_var="$(gen_const GW_ANCHOR_SUBNET_VAR)" || exit 2

  mkdir -p out
  local stem="setup-gw-anchor"
  local export_env="out/${stem}.env.json"
  rm -f "out/${stem}.json" "out/${stem}.rc" "$export_env"

  echo "[setup] посев якоря: $folder (зоны резолвятся элементом «$zones»)" >&2
  # Резолв зон идёт ВМЕСТЕ с якорем и первым: без него подсеть создавалась бы в
  # зоне из закоммиченного умолчания окружения, которой на стенде может не быть,
  # и отказ читался бы как дефект продукта («unknown zone id»).
  newman run "$collection" \
    -e "$ENV" \
    --folder "$zones" \
    --folder "$folder" \
    --export-environment "$export_env" \
    --reporters cli,json \
    --reporter-json-export "out/${stem}.json"
  local rc=$?
  echo "$rc" > "out/${stem}.rc"

  # Вердикт — той же функцией, что судит прогон суиты. `run.sh` при `source` свой
  # main не запускает (guard по BASH_SOURCE).
  # shellcheck source=scripts/run.sh
  source "$NEWMAN_DIR/scripts/run.sh"
  if ! aggregate_verdict "out" "$stem"; then
    echo "[setup] FAIL: посев не прошёл по числам (см. таблицу выше)." >&2
    exit 1
  fi

  local pairs
  pairs="$(anchor_ids_from_export "$export_env" "$net_var" "$sub_var")" || {
    echo "[setup] FAIL: окружение не патчится — посев не опубликовал id." >&2
    exit 1
  }

  local fixtures="out/${stem}.fixtures.json"
  # shellcheck disable=SC2086 — pairs намеренно разбивается по строкам на аргументы
  fixtures_json "$fixtures" $pairs || exit 1

  [[ -f "$PATCH_ENV_PY" ]] || {
    echo "[setup] FATAL: нет $PATCH_ENV_PY — патчить окружение нечем." >&2
    exit 2
  }
  python3 "$PATCH_ENV_PY" "$fixtures" "$ENV" || exit 1

  echo "[setup] DONE — $ENV несёт якорь:" >&2
  echo "$pairs" | sed 's/^/[setup]   /' >&2
  echo "[setup] теперь одиночный кейс гоняется так:" >&2
  echo "[setup]   newman run $collection -e $ENV --folder GW-CR-CRUD-OK" >&2
}

# main запускается только при прямом вызове; при `source` (самопроверка) — нет.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
