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

DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$(basename "$0")"

rc=0
checked=0

TMP="$(mktemp -d)"
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
      *"get statefulset -o name"*) echo "statefulset.apps/kacho-umbrella-pg-vpc"; exit 0 ;;
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
  checked=$((checked + 1))
  if run_target "$mode"; then got=green; else got=red; fi
  if [ "$got" = "$want" ]; then
    echo "  ✓ $label — $got, как и требуется"
  else
    echo "  ✗ $label — цель дала $got, а обязана $want. Вывод цели:"
    sed 's/^/      /' "$TMP/out.$mode"
    rc=1
  fi
}

echo "=== $SCRIPT: готовность стенда против подставного kubectl ==="

probe "спросить не удалось (кластера/контекста нет)" red   unreachable
probe "спросили, ответ ПУСТ (в ns нет workload'ов)"  red   empty
probe "спросили, всё выкатилось"                    green healthy
probe "спросили, один workload не выкатился"        red   stuck

# ── Отдельное утверждение о ТЕКСТЕ отказа: оператор обязан узнать, что
#    осматривать было нечего, а не гадать по коду возврата.
checked=$((checked + 1))
if grep -q 'НЕ ПРОЧИТАН\|не удалось\|НЕ ОСМОТРЕН' "$TMP/out.unreachable" 2>/dev/null; then
  echo "  ✓ отказ называет причину: список workload'ов не прочитан"
else
  echo "  ✗ отказ не говорит, ЧТО пошло не так — оператор увидит только код возврата:"
  sed 's/^/      /' "$TMP/out.unreachable" 2>/dev/null | tail -5
  rc=1
fi

echo
echo "проверок исполнено: $checked"
if [ "$checked" -eq 0 ]; then
  echo "FAIL: $SCRIPT — не исполнено ни одной пробы, «ноль находок» здесь означало бы «ноль прочитанного»"
  exit 1
fi
[ $rc -eq 0 ] && echo "PASS: $SCRIPT" || echo "FAIL: $SCRIPT"
exit $rc
