#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ПРОБА: пробросы прогонщика к поставщику личности следуют ПОСАДКЕ ЦЕПОЧКИ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ (#2841)
#
# Прогонщики сквозных проб открывали пробросы к службам поставщика личности
# безусловно. На цепочке own поставщика нет (#2735 выключил его в базе зонта для
# всех стендов), `kubectl port-forward svc/<нет такого>` не встаёт, и блок
# живости объявляет прогон недействительным: во всех четырёх шардах исполнено
# 0 коллекций из 58. Для пробросов к компонентам тот же класс уже закрыт
# (открываются по спросу); здесь спрос задаёт посадка: поставщика нет ровно
# тогда, когда обе половины цепочки объявили own
# (deploy/scripts/identity-provider-landing.py).
#
# Исходов у прогонщика два, и оба судятся:
#   поставщика нет  → не открыт НИ ОДИН проброс к нему и адрес не инъектируется;
#   поставщик нужен → открыты ВСЕ пробросы блока, каждый под проверкой живости
#                     (если прогонщик её ведёт), адрес инъектирован — как прежде.
# «Нужен» включает «посадка не прочитана»: пропуск проброса там снял бы
# обязательный транспорт молча.
#
# ПРОБА ГОНЯЕТ НАСТОЯЩИЙ БЛОК, А НЕ ЕГО ПЕРЕСКАЗ. Блок вырезается из файла
# прогонщика (от `PROVIDER_ENV_ARGS=()` до строки переписи «пробросы к
# поставщику личности: открыто …») и исполняется под ПОДСТАВНЫМ kubectl, который
# отвечает строкой посадки каждой половины и записывает каждый запрошенный
# проброс. Число пробросов блока берётся из самого блока, а не выписывается.
#
# «НОЛЬ НАХОДОК» ОТЛИЧИМО ОТ «НОЛЬ ПРОЧИТАННОГО»: перепись печатается всегда,
# прогонщик без вырезаемого блока — находка, пустой обход — отказ.
#
# Самопроверка: `--self-test` (прогонщик прежней формы — пробросы безусловно —
# обязан быть найден; проброс вне проверки живости — тоже; синтетический
# законный близнец, отличающийся от каждой инъекции одним фактом, обязан молчать).
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DEFAULT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ROOT="$ROOT_DEFAULT"
SELF_TEST=0
while [ $# -gt 0 ]; do
  case "$1" in
    --root) ROOT="$2"; shift 2 ;;
    --self-test) SELF_TEST=1; shift ;;
    *) echo "ОТКАЗ: неизвестный аргумент «$1»" >&2; exit 1 ;;
  esac
done

# WORK — вне любого репозитория: подставной инструмент и вырезанные блоки не
# должны попадаться обходчикам дерева.
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

RUNNERS_SEEN=0
BLOCKS_CUT=0
LANDINGS_RUN=0
FINDINGS=()

# ─── ПОДСТАВНОЙ kubectl ──────────────────────────────────────────────────────
# Посадка задаётся двумя значениями, по одному на половину; «-» — строки посадки
# в логе нет (лог не отдан). Каждая строка таблицы ниже меняет РОВНО один факт
# против соседней.
write_stub_kubectl() {  # <служба> <край>
  mkdir -p "$WORK/bin"
  : > "$WORK/pf.calls"
  {
    echo '#!/usr/bin/env bash'
    echo 'case "$*" in'
    for half in "kaname|$1" "api-gateway|$2"; do
      local dep="${half%%|*}" val="${half#*|}"
      if [ "$val" = "-" ]; then
        echo "  *\"logs deploy/$dep \"*) echo 'Error from server (NotFound)' >&2; exit 1 ;;"
      else
        echo "  *\"logs deploy/$dep \"*) echo 'starting'; printf '%s\\n' '{\"level\":\"info\",\"msg\":\"boot security posture\",\"identity_provider\":\"$val\"}'; exit 0 ;;"
      fi
    done
    echo "  *port-forward*) printf '%s\\n' \"\$*\" >> '$WORK/pf.calls'; sleep 5 & exit 0 ;;"
    echo 'esac'
    echo 'exit 0'
  } > "$WORK/bin/kubectl"
  chmod +x "$WORK/bin/kubectl"
}

# ─── ВЫРЕЗАНИЕ НАСТОЯЩЕГО БЛОКА ──────────────────────────────────────────────
# Журналы пробросов блок пишет в /tmp/e2e-*.log — туда же, куда настоящий
# прогон на этой машине. Вырезанная копия переводится в свой каталог: проба не
# вправе затирать журнал чужого идущего прогона. Больше в блоке не меняется
# ничего.
cut_block() {  # <файл прогонщика> <куда>
  mkdir -p "$WORK/tmp"
  awk '/^PROVIDER_ENV_ARGS=\(\)/{f=1} f{print} f && /пробросы к поставщику личности: открыто/{exit}' "$1" \
    | sed "s#/tmp/#$WORK/tmp/#g" > "$2"
  grep -q 'port-forward' "$2" && grep -q 'пробросы к поставщику личности: открыто' "$2"
}

# ─── ОДНА ПОСАДКА ────────────────────────────────────────────────────────────
# Печатает «<запрошено пробросов> <PF_PIDS> <PF_WHAT> <адрес ДА|НЕТ>».
run_landing() {  # <блок> <каталог помощника> <служба> <край>
  write_stub_kubectl "$3" "$4"
  cat > "$WORK/drive.sh" <<'DRV'
set -o pipefail
PATH="$WORK/bin:$PATH"
NS=kacho
PF_PIDS=(); PF_WHAT=(); TMP_DIRS=()
. "$BLOCK" >/dev/null 2>&1
# Подставной kubectl ушёл в фон вместе с пробросом; запись о вызове появляется,
# когда он завершится. Ждём поимённо — других потомков у водителя нет.
for _p in "${PF_PIDS[@]}"; do wait "$_p" 2>/dev/null; done
declare -p PROVIDER_ENV_ARGS >/dev/null 2>&1 || PROVIDER_ENV_ARGS=()
printf '%s %s %s %s\n' "$(grep -c . "$WORK/pf.calls")" "${#PF_PIDS[@]}" "${#PF_WHAT[@]}" \
  "$( [ "${#PROVIDER_ENV_ARGS[@]}" -gt 0 ] && echo ДА || echo НЕТ )"
DRV
  WORK="$WORK" BLOCK="$1" SCRIPT_DIR="$2" bash "$WORK/drive.sh" 2>/dev/null
}

# посадка|служба|край|ожидание (none|all)|за что отвечает
LANDINGS=(
  "цепочка own|own|own|none|поставщика нет — проброс к нему валит прогон целиком (#2841)"
  "стенд с поставщиком|external|external|all|поставщик есть — все пробросы открыты, как прежде"
  "край не прочитан|own|-|all|отсутствие не установлено — пропуск снял бы обязательный транспорт молча"
  "край называет поставщика|own|external|all|половине поставщик нужен"
)

audit_runner() {  # <относительный путь> <абсолютный путь> <каталог помощника>
  local rel="$1" abs="$2" helper="$3" blk="$WORK/blk.sh"
  RUNNERS_SEEN=$((RUNNERS_SEEN + 1))
  if ! cut_block "$abs" "$blk"; then
    FINDINGS+=("$rel: блок пробросов к поставщику не вырезался — форма записи пробе НЕИЗВЕСТНА, и решение о пробросах стоит вне наблюдения")
    return
  fi
  BLOCKS_CUT=$((BLOCKS_CUT + 1))
  local n liveness=0
  # Пробросы блока — его НЕкомментарные строки с вызовом проброса.
  n="$(grep -v '^[[:space:]]*#' "$blk" | grep -c 'port-forward')"
  grep -q '^PF_WHAT=' "$abs" && liveness=1
  local row name iam edge want why got calls pids what env exp_calls exp_env
  for row in "${LANDINGS[@]}"; do
    IFS='|' read -r name iam edge want why <<<"$row"
    LANDINGS_RUN=$((LANDINGS_RUN + 1))
    got="$(run_landing "$blk" "$helper" "$iam" "$edge")"
    read -r calls pids what env <<<"$got"
    if [ "$want" = none ]; then exp_calls=0; exp_env=НЕТ; else exp_calls="$n"; exp_env=ДА; fi
    if [ "${calls:-}" != "$exp_calls" ] || [ "${pids:-}" != "$exp_calls" ] || [ "${env:-}" != "$exp_env" ]; then
      FINDINGS+=("$rel · $name: ждали «пробросов $exp_calls из $n, адрес $exp_env», получили «пробросов ${calls:-?}, PF_PIDS ${pids:-?}, адрес ${env:-?}» — $why")
      continue
    fi
    if [ "$liveness" = 1 ] && [ "${what:-}" != "$exp_calls" ]; then
      FINDINGS+=("$rel · $name: пробросов $calls, а под проверкой живости ${what:-?} — не вставший проброс пройдёт молча")
      continue
    fi
    echo "  ok   $rel · $name (служба $iam, край $edge): пробросов $calls из $n, адрес $env$( [ "$liveness" = 1 ] && echo ", под проверкой живости $what")"
  done
}

# ─── САМОПРОВЕРКА ────────────────────────────────────────────────────────────
self_test() {
  local fails=0 tmp="$WORK/st" ; mkdir -p "$tmp/deploy/scripts"
  cp "$ROOT_DEFAULT/deploy/scripts/identity-provider-landing.py" "$tmp/deploy/scripts/"

  # САМОПРОВЕРКА СТРОИТСЯ НА СИНТЕТИКЕ, А НЕ НА ПРОГОНЩИКАХ ДЕРЕВА. Законный
  # близнец — синтетический прогонщик верной формы; каждая инъекция отличается
  # от него ОДНИМ фактом. Прогонщики дерева судит основной проход: взятые
  # близнецом, они сделали бы самопроверку зависимой от живого входа, и её
  # краснота на регрессии прогонщика читалась бы как поломка пробы.
  #
  # ЗАКОННЫЙ БЛИЗНЕЦ: решение по посадке, оба проброса под проверкой живости.
  cat > "$tmp/deploy/scripts/newman-twin.sh" <<'TWIN'
PF_PIDS=()
PF_WHAT=()
PROVIDER_ENV_ARGS=()
landing="$(python3 "$SCRIPT_DIR/identity-provider-landing.py" --namespace "$NS")" || landing="present|отказ"
if [ "${landing%%|*}" != absent ]; then
  kubectl -n "$NS" port-forward svc/provider-a "1:1" >/dev/null 2>&1 & PF_PIDS+=($!); PF_WHAT+=("1|a|/dev/null")
  kubectl -n "$NS" port-forward svc/provider-b "2:2" >/dev/null 2>&1 & PF_PIDS+=($!); PF_WHAT+=("2|b|/dev/null")
  PROVIDER_ENV_ARGS=(--env-var "providerPublicBaseUrl=http://localhost:1")
fi
echo "[x] пробросы к поставщику личности: открыто ${#PF_PIDS[@]} — ${landing#*|}"
TWIN
  # ИНЪЕКЦИЯ 1: прежняя форма — пробросы открываются безусловно (снято условие).
  grep -v -e '^landing=' -e '^if ' -e '^fi$' "$tmp/deploy/scripts/newman-twin.sh" \
    > "$tmp/deploy/scripts/newman-old.sh"
  # ИНЪЕКЦИЯ 2: решение верное, но второй проброс не поставлен под проверку
  # живости (снята одна запись PF_WHAT).
  sed 's#; PF_WHAT+=("2|b|/dev/null")##' "$tmp/deploy/scripts/newman-twin.sh" \
    > "$tmp/deploy/scripts/newman-unwatched.sh"
  # СЛЕПОТА: прогонщик без блока вовсе.
  echo '# прогонщик без пробросов к поставщику' > "$tmp/deploy/scripts/newman-blind.sh"

  local out rc
  # Строки «ok» называют и законных близнецов — находками считается остальное.
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$tmp" 2>&1 | grep -v '^  ok   ')"; rc=${PIPESTATUS[0]}
  _st() { if [ "$2" = 1 ]; then echo "  ok   $1"; else echo "  FAIL $1: $3"; fails=$((fails + 1)); fi; }

  echo "ось 1 — прежняя форма (безусловные пробросы) находится на цепочке own"
  _st "инъекция даёт находку и называет посадку" \
      "$(grep -q 'newman-old.sh · цепочка own' <<<"$out" && echo 1 || echo 0)" "$out"
  _st "на стенде с поставщиком прежняя форма молчит (изменён один факт — посадка)" \
      "$(grep -q 'newman-old.sh · стенд с поставщиком' <<<"$out" && echo 0 || echo 1)" "$out"
  _st "код возврата ненулевой" "$([ "$rc" != 0 ] && echo 1 || echo 0)" "rc=$rc"

  echo "ось 2 — проброс вне проверки живости находится"
  _st "инъекция даёт находку о живости" \
      "$(grep -q 'newman-unwatched.sh · стенд с поставщиком: пробросов 2, а под проверкой живости 1' <<<"$out" && echo 1 || echo 0)" "$out"
  _st "и молчит на цепочке own, где пробросов нет" \
      "$(grep -q 'newman-unwatched.sh · цепочка own' <<<"$out" && echo 0 || echo 1)" "$out"

  echo "ось 3 — законный близнец МОЛЧИТ, и инъекции действительно от него отличаются"
  _st "о близнеце находок нет" \
      "$(grep -q 'newman-twin.sh' <<<"$out" && echo 0 || echo 1)" "$out"
  _st "инъекции построены (каждая отличается от близнеца)" \
      "$(! cmp -s "$tmp/deploy/scripts/newman-twin.sh" "$tmp/deploy/scripts/newman-old.sh" \
         && ! cmp -s "$tmp/deploy/scripts/newman-twin.sh" "$tmp/deploy/scripts/newman-unwatched.sh" \
         && echo 1 || echo 0)" "инъекция совпала с близнецом — её правка не применилась"

  echo "ось 4 — слепота пробы объявляется находкой, а не молчанием"
  _st "прогонщик без блока — находка" \
      "$(grep -q 'newman-blind.sh' <<<"$out" && echo 1 || echo 0)" "$out"

  echo "ось 5 — перепись печатается"
  _st "объём осмотренного назван" \
      "$(grep -q 'перепись: прогонщиков осмотрено 4 · блоков вырезано 3' <<<"$out" && echo 1 || echo 0)" "$out"

  echo "ось 6 — на дереве без прогонщиков вердикт БЕСПРЕДМЕТЕН, а не зелен"
  mkdir -p "$WORK/empty/deploy/scripts"
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$WORK/empty" 2>&1)"; rc=$?
  _st "пустой обход — отказ" "$([ "$rc" != 0 ] && echo 1 || echo 0)" "rc=$rc / $out"

  echo
  if [ "$fails" -gt 0 ]; then
    echo "ОТКАЗ: провалено утверждений $fails из 10" >&2; return 1
  fi
  echo "ЧИСТО: 10 утверждений — проба способна упасть на прежней форме и на пробросе вне живости, смолчать на дереве и объявить свою слепоту"
  return 0
}

[ "$SELF_TEST" = 1 ] && { self_test; exit $?; }

for f in "$ROOT"/deploy/scripts/newman-*.sh; do
  [ -e "$f" ] || continue
  audit_runner "deploy/scripts/$(basename "$f")" "$f" "$ROOT/deploy/scripts"
done

echo "перепись: прогонщиков осмотрено $RUNNERS_SEEN · блоков вырезано $BLOCKS_CUT · посадок прогнано $LANDINGS_RUN"
if [ "$RUNNERS_SEEN" -eq 0 ]; then
  echo "ОТКАЗ: прогонщиков не прочитано — вердикт беспредметен" >&2; exit 1
fi
if [ "${#FINDINGS[@]}" -gt 0 ]; then
  echo "НАХОДКИ (${#FINDINGS[@]}):" >&2
  for x in "${FINDINGS[@]}"; do echo "  $x" >&2; done
  exit 1
fi
echo "ЧИСТО: пробросы к поставщику следуют посадке на каждой из ${#LANDINGS[@]} посадок у каждого прогонщика"
