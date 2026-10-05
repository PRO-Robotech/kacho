#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# КАРТА ИДЕНТИФИКАТОРОВ СОДЕРЖИМОГО ОБРАЗОВ ПОКРЫВАЕТ КАЖДЫЙ ОБРАЗ, КОТОРЫЙ
# СОБИРАЕТ СТЕНД, — И ЗНАЧЕНИЯ В НЕЙ ПИШЕТ ТОТ, КТО ОБРАЗ СОБРАЛ (kacho#3026).
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ
#
# Все локальные образы носят неизменный тег `:dev`, и под перекатывается на
# пересобранный образ только потому, что шаблон пода несёт идентификатор
# содержимого из карты `global.kachoImageIds` (helm/umbrella/values.image-ids.yaml).
# Шаблонную половину судит image-rollout-binding-test.sh: идентификатор каждого
# образа доезжает до шаблона ровно одного workload'а. Эта проверка — вторая
# половина: значение в карте ВООБЩЕ ПОЯВЛЯЕТСЯ и следует за содержимым.
#
# Обе половины нужны вместе. Шаблон, читающий ключ, которого никто не пишет,
# получает «unset» на каждом подъёме — то есть константу, — и под снова не
# перекатывается; именно так консоль оставалась на прежней сборке после
# `make own-up`: её собирает `build-ui`, а карту писал только `build-services`.
#
# ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — ИСХОД РЕЦЕПТОВ, А НЕ ИХ ТЕКСТ. Настоящие цели
# `build-services` и `build-ui` исполняются make с подменёнными `docker` и `kind`
# (сборка и загрузка в кластер — не предмет; предмет — что рецепт пишет в карту):
#   (1) после обеих целей карта несёт ровно ключи SERVICES и `ui-future/<модуль>`
#       для каждого модуля UI_PROJECTS, и у каждого — идентификатор СВОЕГО образа;
#   (2) одна `build-services` не стирает ключи консоли, одна `build-ui` — ключи
#       служб: цели вызываются и порознь (dev-up при BUILD_UI=0, ручная пересборка);
#   (3) пересобранный модуль консоли меняет ровно свой ключ;
#   (4) неизменное содержимое даёт побайтово ту же карту — иначе каждый подъём
#       перекатывал бы поды без причины.
#
# Предмет берётся из Makefile (SERVICES, объявление UI_PROJECTS), а не из копии
# списков здесь. Карта пишется во временный файл (IMAGE_IDS_VALUES переопределён
# аргументом make): живое дерево проба не трогает.
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
MAKEFILE="$DEPLOY_ROOT/Makefile"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=4

require_python_yaml
require_file_present "$MAKEFILE" "deploy/Makefile — предмет проверки берётся из него"
command -v make >/dev/null || fatal "make не найден — рецепты исполнять нечем"

SERVICES="$(sed -n 's/^SERVICES *:= *//p' "$MAKEFILE" | head -1)"
[ -n "$SERVICES" ] || fatal "в deploy/Makefile не найден SERVICES — предпосылка проверки не выполняется"
UI_DECL="$(grep -m1 '^UI_PROJECTS *:=' "$MAKEFILE")"
[ -n "$UI_DECL" ] || fatal "в deploy/Makefile не найдено объявление UI_PROJECTS"
# shellcheck disable=SC2016  # `$(UI_PROJECTS)` раскрывает make, а не оболочка
UI_PROJECTS="$(printf '%s\n__ui_projects:\n\t@echo $(UI_PROJECTS)\n' "$UI_DECL" \
  | make --no-print-directory -s -C "$DEPLOY_ROOT" -f - __ui_projects)" \
  || fatal "объявление UI_PROJECTS не вычислилось make"
[ -n "$UI_PROJECTS" ] || fatal "UI_PROJECTS вычислился пустым — пустой предмет есть условие, а не «всё покрыто»"

TMPD="$(mktemp -d)"; trap 'rm -rf "$TMPD"' EXIT
MAP="$TMPD/values.image-ids.yaml"
STUBS="$TMPD/bin"; mkdir -p "$STUBS"

# Подменённый docker. `build` запоминает собранное имя, `image inspect` отвечает
# идентификатором, который несёт ИМЯ образа и ПОКОЛЕНИЕ его содержимого: по
# первому разбор узнаёт, чей идентификатор лёг под ключ, по второму — что
# пересборка до карты доехала. Поколение задаётся на образ через файл, потому что
# цели вызываются make, а не этой оболочкой.
cat >"$STUBS/docker" <<'STUB'
#!/usr/bin/env bash
case "$1" in
  build) exit 0 ;;
  image)
    [ "$2" = inspect ] || exit 64
    img="${!#}"; gen=1
    [ -f "$STUB_GEN_DIR/${img//[:\/]/_}" ] && gen="$(cat "$STUB_GEN_DIR/${img//[:\/]/_}")"
    printf 'sha256:stub-%s-g%s\n' "${img//[:\/]/_}" "$gen" ;;
  *) exit 64 ;;
esac
STUB
printf '#!/usr/bin/env bash\nexit 0\n' >"$STUBS/kind"
chmod +x "$STUBS/docker" "$STUBS/kind"
export STUB_GEN_DIR="$TMPD/gen"; mkdir -p "$STUB_GEN_DIR"

run_make() { # цели…
  PATH="$STUBS:$PATH" make --no-print-directory -s -C "$DEPLOY_ROOT" \
    IMAGE_IDS_VALUES="$MAP" CLUSTER_NAME=stub "$@" >"$TMPD/make.log" 2>&1 \
    || { tail -20 "$TMPD/make.log" >&2; fatal "make $* не исполнился — исход рецептов не получен"; }
}

# check <ожидание-поколений> — разбор карты: ключи ровно предмет, значение
# каждого — идентификатор его образа нужного поколения.
check() {
  SERVICES="$SERVICES" UI_PROJECTS="$UI_PROJECTS" GENS="$1" \
    python3 - "$MAP" "$DEPLOY_ROOT" <<'PY'
import os, subprocess, sys, yaml
path, root = sys.argv[1], sys.argv[2]
gens = dict(kv.split("=") for kv in os.environ["GENS"].split()) if os.environ["GENS"].strip() else {}
try:
    doc = yaml.safe_load(open(path)) or {}
except FileNotFoundError:
    print("  карта не записана вовсе"); sys.exit(1)
ids = ((doc.get("global") or {}).get("kachoImageIds")) or {}
want = {}
for s in os.environ["SERVICES"].split():
    want[s] = None  # имя образа службы знает internal/productnaming; сверяем по префиксу ниже
for p in os.environ["UI_PROJECTS"].split():
    want["ui-future/" + p] = "kacho-ui-future-%s_dev" % p
bad = []
for k in sorted(set(ids) - set(want)):
    bad.append(f"{k}: ключ вне предмета (ни служба, ни модуль консоли)")
for k, img in sorted(want.items()):
    v = ids.get(k)
    if v is None:
        bad.append(f"{k}: идентификатора содержимого в карте НЕТ — шаблон пода получит константу и не перекатится")
        continue
    g = gens.get(k, "1")
    if img is not None and v != f"sha256:stub-{img}-g{g}":
        bad.append(f"{k}: в карте «{v}», ожидался идентификатор образа {img} поколения {g}")
    elif img is None and not v.endswith(f"-g{g}"):
        bad.append(f"{k}: в карте «{v}», ожидалось поколение {g}")
for b in bad:
    print("  ✗ " + b)
print(f"  ключей в карте: {len(ids)}; в предмете: {len(want)}; расхождений: {len(bad)}")
sys.exit(1 if bad else 0)
PY
}

echo "=== $SCRIPT: (1) build-services + build-ui → карта покрывает каждый собранный образ ==="
run_make build-services build-ui
check "" || fail "(1) карта не покрывает образы, собранные стендом (перечень выше)"
ok
cp "$MAP" "$TMPD/map-1.yaml"

echo "=== $SCRIPT: (2) цели порознь не стирают чужие ключи ==="
run_make build-services
check "" || fail "(2) build-services стёр ключи консоли (перечень выше)"
run_make build-ui
check "" || fail "(2) build-ui стёр ключи служб (перечень выше)"
ok

echo "=== $SCRIPT: (3) пересборка модуля консоли меняет ровно свой ключ ==="
read -r first _ <<<"$UI_PROJECTS"
echo 2 >"$STUB_GEN_DIR/kacho-ui-future-${first}_dev"
run_make build-ui
check "ui-future/$first=2" || fail "(3) пересборка ui-future/$first до карты не доехала либо задела чужой ключ"
ok
rm -f "$STUB_GEN_DIR/kacho-ui-future-${first}_dev"

echo "=== $SCRIPT: (4) неизменное содержимое — побайтово та же карта ==="
run_make build-services build-ui
cmp -s "$MAP" "$TMPD/map-1.yaml" || {
  diff "$TMPD/map-1.yaml" "$MAP" | sed 's/^/    /'
  fail "(4) на неизменном содержимом карта изменилась — каждый подъём перекатывал бы поды без причины"
}
ok

outcome_verdict "служб: $(wc -w <<<"$SERVICES"); модулей консоли: $(wc -w <<<"$UI_PROJECTS")"
