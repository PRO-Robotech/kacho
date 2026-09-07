#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ГОТОВНОСТЬ СТЕНДА НЕ ВЫВОДИТСЯ ИЗ ПУСТОГО ОТВЕТА.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Весь вердикт `assert-rollout-ready` — два вложенных обхода:
#
#     for kind in deployment statefulset; do
#       for w in $(kubectl -n kacho get $kind -o name 2>/dev/null); do …; done
#     done
#
# Если `kubectl` не смог ответить — контекста нет, кластер не поднят, права не
# те, — то поток ошибок отправлен в никуда (`2>/dev/null`), на выходе ПУСТО,
# тело цикла не исполняется НИ РАЗУ, счётчик отказов остаётся нулём, и цель
# печатает «all workloads rollout-complete» и выходит НУЛЁМ. То есть «не смог
# спросить» зачитывается как «всё готово» — и это не краевой случай, а
# ЕДИНСТВЕННОЕ, что цель делает.
#
# Код возврата подстановки в списке `for` теряется by construction: `set -e` его
# не видит, потому что подстановка здесь не команда, а слово. Поэтому защита
# обязана быть ЯВНОЙ — спросить отдельно, проверить код, проверить непустоту.
#
# ─────────────────────────────────────────────────────────────────────────────
# ДОКАЗАТЕЛЬСТВО — ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ, НАД НАСТОЯЩЕЙ ЦЕЛЬЮ
#
# Проверяется не текст рецепта, а его ИСХОД: цель запускается с подставным
# `kubectl` первым в PATH.
#
#   • «спросить не удалось» ОБЯЗАНО быть красным — иначе зелёное означает
#     «осматривать было нечего»;
#   • «спросили, всё выкатилось» ОБЯЗАНО быть зелёным — иначе проверка ловит
#     форму, а не существо, и её снимут при первом ложном срабатывании;
#   • «спросили, один workload не выкатился» ОБЯЗАНО быть красным — контроль
#     того, что цель вообще смотрит на ответ.
#
# Кластер не нужен: подставной `kubectl` отвечает сам.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$(basename "$0")"

# Три исхода — ОДНОЙ реализацией на весь каталог: 0 зелено · 1 находка о дереве ·
# 2 условие не создано. Здесь расходился ВЕСЬ словарь, потому что своего у скрипта
# не было вовсе:
#   • накопитель находок звался `rc` и печатался безымянным `echo "  ✗ …"` — то
#     есть глагол «нашли и продолжаем» в этом каталоге назывался четвёртым
#     способом;
#   • категории «условие не создано» не существовало НИ В КАКОМ виде: не будь на
#     машине `make` (или Makefile стенда), цель отказала бы, проба прочла бы это
#     как `red` там, где ждала `green`, и скрипт объявил бы НАХОДКУ О ДЕРЕВЕ по
#     причине, к дереву отношения не имеющей;
#   • перепись при ненулевом исходе не печаталась — «сколько успели проверить»
#     оставалось известно только на зелёном.
#
# Подставной `kubectl` ниже отвечает СВОИМИ кодами (в том числе 3 на незаданном
# режиме) — это ответы стороннего инструмента в проверяемой ситуации, а не вердикт
# скрипта: общего словаря исходов они не касаются и не меняются.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
# Проб ровно пять и их число фиксировано: четыре режима подставного kubectl плюс
# отдельное утверждение о ТЕКСТЕ отказа. Объявленное ожидание ловит удалённую
# пробу — прежний счётчик ловил только «ноль проб из пяти».
EXPECTED_ASSERTIONS=5

# `violation` НАКАПЛИВАЕТ находку и продолжает: перечислить все режимы за один
# прогон дешевле, чем чинить по одному. `good` печатает выполненное утверждение —
# имя `ok` занято общей реализацией, где оно бумкает счётчик, а не печатает.
good() { echo "  ✓ $1"; }

# Условия прогона: цель поднимается через `make`, а её рецепт лежит в Makefile
# стенда. Оба отсутствия — «условие не создано», а не свойство дерева.
command -v make >/dev/null 2>&1 || fatal "нет make — цель assert-rollout-ready запускать нечем"
require_file_present "$DEPLOY_ROOT/Makefile" "Makefile стенда"

TMP="$(mktemp -d)" || fatal "не создан временный каталог — подставной kubectl положить некуда"
trap 'rm -rf "$TMP"' EXIT

# ── Подставной kubectl. Поведение выбирается переменной KSTUB_MODE.
#
# Пишется ЗАРАНЕЕ и один раз; режим приезжает окружением, потому что цель
# запускает make, тот — bash, и передать поведение иначе как через окружение
# нечем.
mkdir -p "$TMP/bin"
cat >"$TMP/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
# Подставной kubectl: отвечает так, как ответил бы настоящий в проверяемой
# ситуации. Поток ошибок пишется в stderr — ровно как у настоящего, чтобы
# `2>/dev/null` в рецепте вёл себя так же.
args="$*"
case "${KSTUB_MODE:-}" in
  unreachable)
    echo "E: The connection to the server localhost:8080 was refused" >&2
    exit 1
    ;;
  healthy)
    case "$args" in
      *"get deployment -o name"*) echo "deployment.apps/api-gateway"; echo "deployment.apps/vpc"; exit 0 ;;
      *"get statefulset -o name"*) echo "statefulset.apps/kaname-umbrella-pg-vpc"; exit 0 ;;
      *"rollout status"*) echo "deployment \"x\" successfully rolled out"; exit 0 ;;
      *"get jobs -o json"*|*"get cronjobs -o json"*) echo '{"items":[]}'; exit 0 ;;
      *) exit 0 ;;
    esac
    ;;
  empty)
    # Спросили успешно, ответ ПУСТ: в пространстве имён нет ни одного workload'а.
    # Это не «всё выкатилось» — это «выкатывать нечего», и различать их обязано.
    case "$args" in
      *"get jobs -o json"*|*"get cronjobs -o json"*) echo '{"items":[]}'; exit 0 ;;
      *) exit 0 ;;
    esac
    ;;
  stuck)
    case "$args" in
      *"get deployment -o name"*) echo "deployment.apps/api-gateway"; exit 0 ;;
      *"get statefulset -o name"*) exit 0 ;;
      *"rollout status"*) echo "error: deployment \"api-gateway\" exceeded its progress deadline" >&2; exit 1 ;;
      *"get pods -o json"*) echo '{"items":[]}'; exit 0 ;;
      *"get jobs -o json"*|*"get cronjobs -o json"*) echo '{"items":[]}'; exit 0 ;;
      *) exit 0 ;;
    esac
    ;;
  *) echo "KSTUB_MODE не задан" >&2; exit 3 ;;
esac
STUB
chmod +x "$TMP/bin/kubectl"

run_target() { # <режим> → печатает код возврата цели
  local mode="$1" out
  out="$(cd "$DEPLOY_ROOT" && PATH="$TMP/bin:$PATH" KSTUB_MODE="$mode" \
        make --no-print-directory assert-rollout-ready 2>&1)"
  local code=$?
  printf '%s\n' "$out" >"$TMP/out.$mode"
  return $code
}

probe() { # <метка> <ожидание: red|green> <режим>
  local label="$1" want="$2" mode="$3"
  ok
  if run_target "$mode"; then got=green; else got=red; fi
  if [ "$got" = "$want" ]; then
    good "$label — $got, как и требуется"
  else
    violation "$label — цель дала $got, а обязана $want. Вывод цели:"
    sed 's/^/      /' "$TMP/out.$mode"
  fi
}

echo "=== $SCRIPT: готовность стенда против подставного kubectl ==="

probe "спросить не удалось (кластера/контекста нет)" red   unreachable
probe "спросили, ответ ПУСТ (в ns нет workload'ов)"  red   empty
probe "спросили, всё выкатилось"                    green healthy
probe "спросили, один workload не выкатился"        red   stuck

# ── Отдельное утверждение о ТЕКСТЕ отказа: оператор обязан узнать, что
#    осматривать было нечего, а не гадать по коду возврата.
ok
if grep -q 'НЕ ПРОЧИТАН\|не удалось\|НЕ ОСМОТРЕН' "$TMP/out.unreachable" 2>/dev/null; then
  good "отказ называет причину: список workload'ов не прочитан"
else
  violation "отказ не говорит, ЧТО пошло не так — оператор увидит только код возврата:"
  sed 's/^/      /' "$TMP/out.unreachable" 2>/dev/null | tail -5
fi

echo
echo "проверок исполнено: $N из $EXPECTED_ASSERTIONS"
# Вердикт печатает общая реализация: она же роняет прогон на накопленных находках
# и на неполной переписи. Прежний страж ловил только «ноль проб»; объявленное
# ожидание ловит и «четыре пробы из пяти» — то есть удалённую пробу, молчание о
# которой ничего не доказывает.
outcome_verdict "режимов подставного kubectl: 4 (unreachable, empty, healthy, stuck)"
