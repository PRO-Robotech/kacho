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
# Исходов у БЛОКА прогонщика два, и оба судятся:
#   поставщика нет  → блок не открыл НИ ОДНОГО проброса и не инъектировал адрес;
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
# ─────────────────────────────────────────────────────────────────────────────
# ВНЕ БЛОКА СУДИТСЯ ИСПОЛНЕНИЕ, А НЕ ТЕКСТ (#2866)
#
# Исход блока ничего не говорит о строке вне его: безусловный проброс к службе
# поставщика ниже строки переписи блока не меняет, и проба, гонявшая только
# блок, отдавала на нём код 0 (#2735). Прежде вне блока проброс судился по
# ТЕКСТУ — цель опознавалась литералом `svc/<имя>`, — и служба, приходящая из
# данных, не судилась вовсе: опыт x1b (служба компонента в optional_transports
# манифеста deploy/e2e-shards.json заменена службой поставщика) давал код 0 и
# строку «ЧИСТО».
#
# Теперь каждый прогонщик ИСПОЛНЯЕТСЯ — от первой строки до конца команды, в
# которой стоит его последний вызов проброса, — под тем же подставным kubectl в
# двух мирах: «цепочка own» и «стенд с поставщиком». Прогонщик идёт в зеркале
# дерева, где файлы — ссылки на настоящие, кроме двух родов: прогонщики лежат
# своими префиксами (журналы из /tmp/ переведены в свой каталог, как у блока),
# а производитель транспортов компонентов — прокладкой (см. ниже). Каждый
# запрошенный проброс записывает оболочка `kubectl` вместе со СТЕКОМ вызова —
# каждой рамкой (файл:строка, функция) от места kubectl наружу — и целью.
# Находка — проброс вне блока, чья цель любого вида (`svc/`, `deploy/`,
# `statefulset/` …) ведёт к поставщику: литерал, переменная, ответ помощника и
# строка манифеста судятся ОДНИМ правилом, потому что суд видит уже
# подставленную цель. Координата находки — самая внешняя рамка прогонщика: у
# вызова через помощника это строка, откуда помощника позвали.
#
# ВЫЗОВ ПОМОЩНИКА — ТОЖЕ ВЫЗОВ ПРОБРОСА (#2866, круг 2). Функция, чьё тело
# открывает проброс (или зовёт такую функцию), — функция проброса: её узнают по
# телам функций (дамп функций после префикса и текст прогонщика) и по стеку
# записанных вызовов. Строка, где функция проброса ВЫЗВАНА, — место вызова, как и
# команда `kubectl … port-forward`, и префикс ДОРАСТАЕТ до последней такой строки:
# помощник, позванный ниже последнего вызова, записанного текстом, прежде за
# концом префикса не исполнялся, а место его тела исполнял законный вызов выше, и
# покрытие молчало.
#
# МЕСТО ВЫЗОВА — ВЫЗОВ В РАЗОБРАННОЙ КОМАНДЕ, А НЕ ИМЯ В ТЕКСТЕ (#2893). Прогонщик
# разбирается до простых команд (кавычки, экранирование, продолжение строки,
# here-doc, подстановка команды, определение функции, case, [[ ]] и (( ))):
# вызов — это имя на месте имени команды, в том числе внутри `$(…)`, `<(…)` и
# обратных кавычек; вызов kubectl — команда, чьё имя (или программа за обёрткой
# command, env, exec …) kubectl, а аргумент — port-forward. Имя внутри строкового
# литерала (`echo "own_front_forward …"`) вызовом не является: прежде такая
# строка давала находку о неисполненном вызове, а ниже префикса — и продление.
#
# ПРОДЛЕНИЕ НЕ ИСПОЛНЯЕТ ШАГОВ С ПОБОЧНЫМ ДЕЙСТВИЕМ (#2893). Строки, на которые
# префикс дорастает, — шаги прогонщика после пробросов: посев, прогон суит.
# Исполняются они, только если каждая их команда (и каждая команда тел функций,
# которые они зовут, по замыканию) ИНЕРТНА под подменами суда: встроенная
# команда оболочки без исполнения чужого кода, подменённый инструмент, python3 с
# объявленным производителем данных, чистый фильтр текста; перенаправление — только
# в /dev/null, дескриптор или /tmp/ (переведён в каталог суда). Перечень закрыт;
# шаг вне него — находка «продлевать нельзя» с его строкой и причиной, продление
# на нём останавливается (вызовы выше него ещё дорастают), а вызовы ниже — не
# исполняются и названы в той же находке. Прежде продление исполняло такие шаги
# как есть: вызов помощника ниже строки посева запускал настоящий посев.
#
# ЦЕЛЬ ОПОЗНАЁТСЯ ПО СТВОЛУ ИМЕНИ (#2866, круг 2). Чарт называет службы
# `<полное имя>-<роль>`, а нагрузку — полным именем: kacho-umbrella-hydra-public
# и deploy/kacho-umbrella-hydra. Цель, равная службе блока, — поставщик; цель,
# равная стволу службы блока (имя без последнего сегмента) или начинающаяся с
# «ствол-», — тоже: это его нагрузка или соседняя служба того же чарта.
#
# ДАННЫЕ БЕРУТСЯ У НАСТОЯЩИХ ПРОИЗВОДИТЕЛЕЙ:
#   посадка цепочки   — identity-provider-landing.py, как у блока;
#   собственный фронт — own-rest-front-address.py; подставной kubectl отвечает
#                       ему Service и Deployment С ТЕМИ ИМЕНАМИ, О КОТОРЫХ
#                       спросили, поэтому цель проброса несёт имя, выведенное
#                       помощником, а не выписанное здесь;
#   транспорты        — НАСТОЯЩИЙ разбор манифеста e2e-optional-transports.py со
#                       спросом «весь объявленный»: подменена одна его функция
#                       (кто из суит набирает транспорт). Любой шард может
#                       набрать любой объявленный транспорт, а строка манифеста,
#                       которую сегодня не набирает никто, поведёт к поставщику
#                       в тот день, когда кейс её наберёт; спрос текущего дерева
#                       спрятал бы её до этого дня.
#
# МЕСТО ВЫЗОВА, КОТОРОЕ НЕ ИСПОЛНИЛОСЬ, — НАХОДКА. Каждый вызов проброса вне
# блока (команда kubectl … port-forward и вызов функции проброса) обязан исполниться хотя бы в
# одном мире: иначе его цель суду неизвестна, и «ЧИСТО» о нём утверждать нечего.
# Тем же счётом идёт прогон, прерванный до конца префикса, и вызов проброса мимо
# оболочки записи (его ловит подставной kubectl, но координаты у такой записи
# нет).
#
# СТАТИЧЕСКИ вне блока по-прежнему судятся (в том числе строки, которых
# исполнение не достигает):
#   проброс к поставщику — служба блока или её ствол имени (см. выше), — если
#   цель вызова kubectl … port-forward названа ЛИТЕРАЛОМ после `<вид>/`
#   (продолжение строки склеивает разбор, координата — первая строка команды);
#   ключ адреса, который блок кладёт в PROVIDER_ENV_ARGS (так ловится адрес
#   суитам в прежней форме, например при запуске волны, launch_wave);
#   ручка порта проброса к поставщику ($ИМЯ, ${ИМЯ…}) — кроме её объявления
#   `ИМЯ="${ДРУГОЕ:-число}"`: оно связывает число, а не набирает адрес;
#   номер такого порта в адресе localhost / 127.0.0.1.
# Поставщик опознаётся по САМИМ блокам всех прогонщиков дерева — службы их
# пробросов, ручки портов вместе с цепочкой объявлений, ключи массива адреса, — а
# не выписывается здесь: перечень рядом с блоком разошёлся бы с ним молча. Ни
# одной службы или ни одного ключа не опознано — находка: суд вне блока
# беспредметен.
#
# ЧЕГО СУД ВНЕ БЛОКА НЕ ВИДИТ: службу или нагрузку поставщика, не делящую
# ствола имени ни с одной службой блока (чарт с переопределённым полным именем);
# цель без вида — имя пода, а не службы (такие пробросы считаются и называются
# координатой в переписи; цели-нагрузки считаются числом — их суд держится на
# соглашении об именах чарта); вызов функции проброса ниже конца префикса по
# имени, которого нет на месте имени команды (`eval`, имя, собранное из частей);
# адрес, собранный при исполнении из частей, не несущих ни ключа, ни ручки, ни
# номера порта; вызов kubectl по абсолютному пути (он минует и оболочку, и
# подставной kubectl). Комментарий и текст строки не судятся: они ничего не
# открывают. Шаги префикса ДО последнего вызова, записанного текстом, исполняются
# как есть: их инертность проба не судит (судится только продление), и то, что
# они не пишут в дерево и не ходят в сеть, держится формой прогонщиков —
# пробросы открываются до посева, — а не пробой.
#
# «НОЛЬ НАХОДОК» ОТЛИЧИМО ОТ «НОЛЬ ПРОЧИТАННОГО»: перепись печатается всегда
# (прогонщики, блоки, посадки, строки, места вызова проброса вне блока — из них
# вызовы функций проброса — и сколько из них исполнено, исполнения и записанные
# вызовы, продления префикса и остановки продления, цели без вида и
# цели-нагрузки), прогонщик без вырезаемого блока — находка, пустой обход —
# отказ. Что проба не пишет в осматриваемое дерево, держится на двух вещах и не
# шире: байткод разборщика и прокладки не пишется (PYTHONDONTWRITEBYTECODE), а
# продление префикса не исполняет шагов с побочным действием (выше); про префикс
# до последнего вызова, записанного текстом, — граница выше.
#
# Самопроверка: `--self-test` (прогонщик прежней формы — пробросы безусловно —
# обязан быть найден; проброс вне проверки живости — тоже; каждая из четырёх
# статических находок вне блока — тоже; служба поставщика из переменной, из
# манифеста (опыт x1b), нагрузка поставщика, названная полным именем чарта, и
# помощник проброса, позванный после последнего вызова, записанного текстом, —
# тоже; место вызова, не исполненное ни в одном мире, — тоже; имя функции
# проброса в тексте строки обязано молчать, а тот же вызов в подстановке команды —
# найтись; шаг посева в продлеваемых строках и в теле функции проброса — находка
# «продлевать нельзя», и посев не исполняется (корень его оси не меняется), а
# близнец с инертным шагом продлевается и судится; осматриваемый
# корень после прогона тот же, что до него; синтетический законный
# близнец, отличающийся от каждой инъекции одним фактом, обязан молчать; цель
# без вида обязана попасть в перепись числом и координатой).
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

# WORK — вне любого репозитория: подставной инструмент, вырезанные блоки и
# зеркало дерева не должны попадаться обходчикам дерева.
WORK="$(mktemp -d)"
# Прокладка грузит НАСТОЯЩИЙ разбор манифеста из осматриваемого корня, и Python
# без этой ручки кладёт рядом с ним __pycache__: проба писала в дерево, которое
# судит (#2866, круг 2). Ручка стоит на весь прогон — на разборщик, прокладку и
# помощников.
export PYTHONDONTWRITEBYTECODE=1
trap 'rm -rf "$WORK"' EXIT

# ПОДМЕНЫ СУДА И ЕГО ПРОИЗВОДИТЕЛИ — одним перечнем на двух читателей: подставной
# мир кладёт заглушку на каждый подменённый инструмент (kubectl — своей записью), а
# разборщик считает инертным при продлении префикса только их и объявленных
# производителей данных (#2893). Перечень рядом с разборщиком разошёлся бы с
# заглушками молча.
SUBSTITUTED_TOOLS=(newman grpcurl jq)
PRODUCERS=(identity-provider-landing.py own-rest-front-address.py e2e-optional-transports.py)
export FWD_GATE_SUBSTITUTED="kubectl ${SUBSTITUTED_TOOLS[*]}"
export FWD_GATE_PRODUCERS="${PRODUCERS[*]}"

# Срок одного исполнения (блок в посадке, префикс прогонщика в мире). Прогон
# под подставным kubectl идёт доли секунды; повисший прогон — не «чисто», а
# находка с кодом срока.
EXEC_TIMEOUT="${EXEC_TIMEOUT:-60}"

RUNNERS_SEEN=0
BLOCKS_CUT=0
LANDINGS_RUN=0
FINDINGS=()
# «<отн. путь>|<абс. путь>|<первая строка блока>|<последняя>»; 0|0 — блок не
# вырезался, и вне блока тогда весь файл.
RUNNER_ROWS=()
# То же плюс «|<префикс в зеркале>|<записи миров через запятую>»; пусто —
# вызовов проброса в файле нет, исполнять нечего.
EXEC_ROWS=()
OUTSIDE_CENSUS="не исполнялся"
# Число вызовов проброса вне блока, которых проба не судит (цель без вида —
# граница в шапке): строка ЧИСТО называет их числом, а не молчит.
OUTSIDE_UNJUDGED="?"

# ─── ПОДСТАВНОЙ kubectl ──────────────────────────────────────────────────────
# Посадка задаётся двумя значениями, по одному на половину; «-» — строки посадки
# в логе нет (лог не отдан). Каждая строка таблицы ниже меняет РОВНО один факт
# против соседней.
# Помощнику собственного фронта подставной kubectl отвечает Service и
# Deployment с теми именами, о которых спросили (порты — по именам, которые
# объявляет чарт службы): имя цели проброса выводит помощник. Ответ, которого
# помощник не примет, даёт не молчание, а неисполненное место вызова.
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
    cat <<'STUB'
  *" get svc/"*)
    s=""; d=""
    for a in "$@"; do case "$a" in svc/*) s="${a#svc/}" ;; deploy/*) d="${a#deploy/}" ;; esac; done
    printf '{"items":[{"kind":"Service","metadata":{"name":"%s"},"spec":{"ports":[{"name":"http-rest","port":9098},{"name":"http-rest-int","port":9099}]}},{"kind":"Deployment","metadata":{"name":"%s"},"spec":{"template":{"spec":{"containers":[{"name":"%s","env":[]}]}}}}]}\n' "$s" "$d" "$d"
    exit 0 ;;
  *" get secret"*) echo 'Error from server (NotFound): secrets not found' >&2; exit 1 ;;
STUB
    echo "  *port-forward*) printf '%s\\n' \"\$*\" >> '$WORK/pf.calls'; sleep 5 & exit 0 ;;"
    echo 'esac'
    echo 'exit 0'
  } > "$WORK/bin/kubectl"
  chmod +x "$WORK/bin/kubectl"
  # Прочие инструменты, чьё наличие прогонщик проверяет до пробросов: префикс
  # их не зовёт, но без них он выходит раньше первого вызова.
  local t
  for t in "${SUBSTITUTED_TOOLS[@]}"; do
    printf '#!/usr/bin/env bash\nexit 0\n' > "$WORK/bin/$t"; chmod +x "$WORK/bin/$t"
  done
}

# ─── ВЫРЕЗАНИЕ НАСТОЯЩЕГО БЛОКА ──────────────────────────────────────────────
# Журналы пробросов блок пишет в /tmp/e2e-*.log — туда же, куда настоящий
# прогон на этой машине. Вырезанная копия переводится в свой каталог: проба не
# вправе затирать журнал чужого идущего прогона. Больше в блоке не меняется
# ничего.
# Границы блока (номера первой и последней строки) остаются в CUT_RANGE: по ним
# суд вне блока знает, что уже судит исход.
cut_block() {  # <файл прогонщика> <куда>
  mkdir -p "$WORK/tmp"
  CUT_RANGE="$(awk '/^PROVIDER_ENV_ARGS=\(\)/ && !s {s=NR} s && /пробросы к поставщику личности: открыто/ {print s, NR; exit}' "$1")"
  [ -n "$CUT_RANGE" ] || return 1
  sed -n "${CUT_RANGE% *},${CUT_RANGE#* }p" "$1" | sed "s#/tmp/#$WORK/tmp/#g" > "$2"
  grep -q 'port-forward' "$2"
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
  local drv="$WORK/drive.sh"
  WORK="$WORK" BLOCK="$1" SCRIPT_DIR="$2" timeout "$EXEC_TIMEOUT" bash "$drv" 2>/dev/null </dev/null
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
    RUNNER_ROWS+=("$rel|$abs|0|0")
    FINDINGS+=("$rel: блок пробросов к поставщику не вырезался — форма записи пробе НЕИЗВЕСТНА, и решение о пробросах стоит вне наблюдения")
    return
  fi
  RUNNER_ROWS+=("$rel|$abs|${CUT_RANGE% *}|${CUT_RANGE#* }")
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

# ─── РАЗБОРЩИК ───────────────────────────────────────────────────────────────
# Один файл на четыре вопроса: `last <файл>` — последняя строка последней команды
# с вызовом проброса (где кончается префикс, который исполняется); `extend` — куда
# префиксу дорастать; `inert` — нет ли в продлеваемых строках шага с побочным
# действием; `judge <строки>` — суд вне блока по разобранному тексту и по записям
# исполнения.
write_analyzer() {
  cat > "$WORK/analyze.py" <<'PY'
import os
import re
import sys

TARGET = re.compile(r'(?<![\w$/-])(?:svc|service|services|deploy|deployment|deployments|pod|pods|po)/([A-Za-z0-9][A-Za-z0-9.-]*)')
PORTSPEC = re.compile(r'"?(?:\$\{([A-Za-z_]\w*)(?::-([0-9]+))?\}|\$([A-Za-z_]\w*)|([0-9]+)):')
KEY = re.compile(r'--env-var[\s=]+"?([A-Za-z_]\w*)=')
KNOB = re.compile(r'^\s*([A-Za-z_]\w*)="\$\{([A-Za-z_]\w*):-([0-9]+)\}"\s*$')
PF_WORD = re.compile(r'\bport-forward\b')


def code_of(line):
    """Строка без shell-комментария; кавычки и экранирование учитываются."""
    quote, esc = None, False
    for i, ch in enumerate(line):
        if esc:
            esc = False
            continue
        if ch == "\\" and quote != "'":
            esc = True
            continue
        if quote:
            if ch == quote:
                quote = None
            continue
        if ch in "'\"":
            quote = ch
            continue
        if ch == "#" and (i == 0 or line[i - 1] in " \t;&|()"):
            return line[:i]
    return line


def target_after(words, i):
    """Цель вызова, чьё слово `port-forward` стоит на месте i: первый не-флаг
    после него; флаг без `=` берёт значение следующим словом."""
    j = i + 1
    while j < len(words) and words[j].startswith("-"):
        j += 1 if "=" in words[j] else 2
    return words[j] if j < len(words) else None


def targets_of(words):
    """Цели всех вызовов проброса в списке слов."""
    return [t for i, w in enumerate(words) if PF_WORD.search(w)
            for t in [target_after(words, i)] if t is not None]


def commands(numbered):
    """Команды из строк (номер, код): продолжение `\\` склеивается."""
    group = []
    for no, code in numbered:
        group.append((no, code))
        if not code.rstrip().endswith("\\"):
            yield group
            group = []
    if group:
        yield group


def read_code(path):
    with open(path, encoding="utf-8") as fh:
        lines = fh.read().split("\n")
    return [(n, code_of(t)) for n, t in enumerate(lines, 1)]


# ─── РАЗБОР: КОМАНДА, А НЕ ТЕКСТ (#2893) ─────────────────────────────────────
# Место вызова — это вызов в РАЗОБРАННОЙ команде: имя, стоящее на месте имени
# команды. Имя внутри строкового литерала (`echo "own_front_forward …"`) вызовом
# не является, а подстановка команды внутри той же строки (`"$(own_front_forward …)"`)
# — является. Разбор знает ровно столько грамматики shell, сколько нужно, чтобы
# отличить одно от другого.
RESERVED_OPEN = {"if", "then", "else", "elif", "do", "while", "until", "!", "time", "coproc"}
RESERVED_CLOSE = {"fi", "done"}
ASSIGN = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=")
META = set(" \t\n;&|()<>")


class Cmd:
    """Простая команда разобранного текста: слова, присваивания, перенаправления.

    `fn` — функция, в теле которой команда стоит (None — вне тела); `kind` —
    simple | test ([[ … ]]) | arith ((( … ))); `line`/`end` — первая и последняя
    строка команды."""

    def __init__(self, line, fn):
        self.line, self.end, self.fn, self.kind = line, line, fn, "simple"
        self.words, self.assigns, self.redirs = [], [], []

    def empty(self):
        return not (self.words or self.assigns or self.redirs) and self.kind == "simple"


def unquote(raw):
    """Значение слова без кавычек; None — в слове есть подстановка ($, `)."""
    out, i, q = [], 0, None
    while i < len(raw):
        ch = raw[i]
        if q == "'":
            if ch == "'":
                q = None
            else:
                out.append(ch)
        elif ch == "\\" and i + 1 < len(raw):
            out.append(raw[i + 1])
            i += 1
        elif ch in "$`":
            return None
        elif ch == '"':
            q = None if q == '"' else '"'
        elif ch == "'" and q is None:
            q = "'"
        else:
            out.append(ch)
        i += 1
    return "".join(out)


class Lexer:
    """Разбор shell-текста до простых команд: кавычки, экранирование, продолжение
    строки, комментарий, here-doc, подстановка команды ($(…), `…`, <(…), >(…)) —
    её команды тоже команды, — присваивание (и массивом), определение функции,
    [[ … ]], (( … )), case с образцами, for/select со списком слов."""

    def __init__(self, text, line=1):
        self.s, self.i, self.line = text, 0, line
        self.cmds, self.heredocs = [], []

    def peek(self, k=0):
        j = self.i + k
        return self.s[j] if j < len(self.s) else ""

    def adv(self, n=1):
        for _ in range(n):
            if self.i < len(self.s):
                if self.s[self.i] == "\n":
                    self.line += 1
                self.i += 1

    def skip_blank(self):
        while True:
            ch = self.peek()
            if ch and ch in " \t":
                self.adv()
            elif ch == "\\" and self.peek(1) == "\n":
                self.adv(2)
            else:
                return

    def next_is(self, ch):
        j = self.i
        while j < len(self.s) and self.s[j] in " \t":
            j += 1
        return self.s.startswith(ch, j)

    # Вложенное внутри слова. Каждая функция стоит на первом символе своей
    # конструкции (или сразу за открытием) и уходит за её конец.
    def subst(self, fn):
        """За `$(`: список команд до парной `)` — его команды тоже команды."""
        self.parse_list(fn, stop=")")
        if self.peek() == ")":
            self.adv()

    def arith(self, fn):
        """За `((`: до парной `))`. Своих команд арифметика не несёт, но подстановка
        команды внутри неё исполняется — её команды тоже команды."""
        depth = 2
        while self.i < len(self.s) and depth:
            ch = self.peek()
            if self.inside(fn, ch):
                continue
            depth += {"(": 1, ")": -1}.get(ch, 0)
            self.adv()

    def backtick(self, fn):
        """За открывающей обратной кавычкой: текст до парной разбирается отдельно."""
        start, line = self.i, self.line
        while self.i < len(self.s) and self.peek() != "`":
            if self.peek() == "\\":
                self.adv()
            self.adv()
        inner = self.s[start:self.i]
        self.adv()
        sub = Lexer(inner.replace("\\`", "`"), line)
        sub.parse_list(fn)
        self.cmds.extend(sub.cmds)

    def squote(self):
        self.adv()
        while self.i < len(self.s) and self.peek() != "'":
            self.adv()
        self.adv()

    def dquote(self, fn):
        self.adv()
        while self.i < len(self.s) and self.peek() != '"':
            ch = self.peek()
            if ch == "\\":
                self.adv(2)
            elif ch == "$":
                self.dollar(fn)
            elif ch == "`":
                self.adv()
                self.backtick(fn)
            else:
                self.adv()
        self.adv()

    def inside(self, fn, ch):
        """Общее для содержимого ${…} и (…): кавычки и подстановки. True — съедено."""
        if ch == "\\":
            self.adv(2)
        elif ch == "'":
            self.squote()
        elif ch == '"':
            self.dquote(fn)
        elif ch == "$":
            self.dollar(fn)
        elif ch == "`":
            self.adv()
            self.backtick(fn)
        else:
            return False
        return True

    def dollar(self, fn):
        nxt = self.peek(1)
        if nxt == "(":
            if self.peek(2) == "(":
                self.adv(3)
                self.arith(fn)
            else:
                self.adv(2)
                self.subst(fn)
        elif nxt == "{":
            self.adv(2)
            depth = 1
            while self.i < len(self.s) and depth:
                ch = self.peek()
                if self.inside(fn, ch):
                    continue
                depth += {"{": 1, "}": -1}.get(ch, 0)
                self.adv()
        elif nxt == "'":
            self.adv(2)
            while self.i < len(self.s) and self.peek() != "'":
                if self.peek() == "\\":
                    self.adv()
                self.adv()
            self.adv()
        else:
            self.adv()

    def balanced(self, fn):
        """На `(` внутри слова (присваивание массивом, extglob): до парной `)`."""
        self.adv()
        depth = 1
        while self.i < len(self.s) and depth:
            ch = self.peek()
            if self.inside(fn, ch):
                continue
            if ch == "#" and self.s[self.i - 1] in " \t\n(":
                while self.i < len(self.s) and self.peek() != "\n":
                    self.adv()
                continue
            depth += {"(": 1, ")": -1}.get(ch, 0)
            self.adv()

    def word(self, fn):
        start = self.i
        while self.i < len(self.s):
            ch = self.peek()
            if ch == "\\":
                self.adv(2)
                continue
            if self.inside(fn, ch):
                continue
            if ch == "(":
                so_far = self.s[start:self.i]
                if (ASSIGN.match(so_far) and so_far.endswith("=")) or (so_far and so_far[-1] in "@!+*?"):
                    self.balanced(fn)
                    continue
                break
            if ch in META:
                break
            self.adv()
        return self.s[start:self.i].replace("\\\n", "")

    def token(self, fn):
        """(вид, значение, строка): WORD | OP | NL | EOF."""
        self.skip_blank()
        line = self.line
        ch = self.peek()
        if not ch:
            return "EOF", "", line
        if ch == "#":
            while self.i < len(self.s) and self.peek() != "\n":
                self.adv()
            return self.token(fn)
        if ch == "\n":
            self.adv()
            return "NL", "\n", line
        for op in (";;&", ";;", ";&", "&&", "||", "|&", "&>>", "&>", ";", "&", "|", "(", ")"):
            if self.s.startswith(op, self.i):
                self.adv(len(op))
                return "OP", op, line
        m = re.match(r"(\d+|\{[A-Za-z_]\w*\})?(<<<|<<-|<<|<>|<&|>&|>>|>\||<|>)", self.s[self.i:self.i + 40])
        if m:
            if m.group(1) is None and m.group(2) in ("<", ">") and self.peek(1) == "(":
                start = self.i  # подстановка процесса <(…) / >(…): слово с командами внутри
                self.adv(2)
                self.subst(fn)
                return "WORD", self.s[start:self.i], line
            self.adv(len(m.group(0)))
            return "OP", m.group(2), line
        return "WORD", self.word(fn), line

    def read_heredocs(self, fn):
        """Тела here-doc после конца строки: текст, а не команды; у тела без
        кавычек в ограничителе подстановки команд исполняются — их команды тоже команды."""
        for delim, strip, expand in self.heredocs:
            while self.i < len(self.s):
                start = self.i
                while self.i < len(self.s) and self.s[self.i] != "\n":
                    self.i += 1
                text, body_line = self.s[start:self.i], self.line
                if self.i < len(self.s):
                    self.i += 1
                    self.line += 1
                if (text.lstrip("\t") if strip else text) == delim:
                    break
                if expand and ("$(" in text or "`" in text):
                    sub = Lexer(text, body_line)
                    while sub.i < len(sub.s):
                        if not sub.inside(fn, sub.peek()):
                            sub.adv()
                    self.cmds.extend(sub.cmds)
        self.heredocs = []

    def parse_list(self, fn=None, stop=None):
        cur = Cmd(self.line, fn)
        blocks = []          # («{» | «(», функция, чьё это тело, или None)
        pending_fn = None    # имя определённой функции: следующий блок — её тело
        state = "cmd"        # cmd | args | for_name | for_words | case_word | case_in | pattern | test
        cases = 0

        def here_fn():
            for _, f in reversed(blocks):
                if f:
                    return f
            return fn

        def flush():
            nonlocal cur
            if not cur.empty():
                self.cmds.append(cur)
            cur = Cmd(self.line, here_fn())

        while True:
            kind, val, line = self.token(here_fn())
            if kind == "EOF":
                flush()
                return
            if kind == "NL":
                flush()
                if self.heredocs:
                    self.read_heredocs(here_fn())
                if state in ("args", "for_words", "for_name"):
                    state = "cmd"
                continue
            if state == "test":
                if kind == "WORD" and val == "]]":
                    state = "args"
                cur.end = line
                continue
            if state == "pattern":
                if kind == "WORD" and val == "esac":
                    cases -= 1
                    state = "args"
                elif kind == "OP" and val == ")":
                    state = "cmd"
                continue
            if state == "case_word":
                state = "case_in"
                continue
            if state == "case_in":
                if kind == "WORD" and val == "in":
                    state = "pattern"
                continue
            if state == "for_name":
                if kind == "OP" and val == "(" and self.peek() == "(":
                    self.adv()
                    self.arith(here_fn())
                state = "for_words"
                continue
            if state == "for_words":
                if kind == "OP" and val in (";", "&"):
                    state = "cmd"
                continue
            if kind == "OP":
                if val in ("<", ">", ">>", ">|", "<>", "<&", ">&", "&>", "&>>", "<<", "<<-", "<<<"):
                    _, target, l2 = self.token(here_fn())
                    if val in ("<<", "<<-"):
                        self.heredocs.append((unquote(target.replace("$", "")) or target, val == "<<-",
                                              not any(q in target for q in "'\"\\")))
                    cur.redirs.append((val, target, l2))
                    cur.end = l2
                    continue
                if val == "(":
                    if state == "args" and len(cur.words) == 1 and not cur.assigns and self.next_is(")"):
                        self.token(here_fn())  # определение функции: имя () тело
                        pending_fn = cur.words[0][0]
                        cur = Cmd(self.line, here_fn())
                        state = "cmd"
                        continue
                    if state == "cmd" and self.peek() == "(":
                        self.adv()
                        self.arith(here_fn())
                        cur.kind, cur.end, state = "arith", self.line, "args"
                        continue
                    flush()
                    blocks.append(("(", pending_fn))
                    pending_fn = None
                    cur = Cmd(self.line, here_fn())
                    state = "cmd"
                    continue
                if val == ")":
                    flush()
                    if blocks and blocks[-1][0] == "(":
                        blocks.pop()
                        cur = Cmd(self.line, here_fn())
                        state = "args"
                        continue
                    if stop == ")":
                        self.i -= 1
                        return
                    state = "args"
                    continue
                flush()
                state = "pattern" if val in (";;", ";&", ";;&") and cases else "cmd"
                continue
            lit = unquote(val)
            if state == "cmd" and not cur.words and not cur.assigns:
                if lit in RESERVED_OPEN:
                    continue
                if lit in RESERVED_CLOSE or (lit == "esac" and cases):
                    flush()
                    cases -= lit == "esac"
                    state = "args"
                    continue
                if lit == "{":
                    flush()
                    blocks.append(("{", pending_fn))
                    pending_fn = None
                    cur = Cmd(self.line, here_fn())
                    continue
                if lit == "}":
                    flush()
                    while blocks and blocks.pop()[0] != "{":
                        pass
                    cur = Cmd(self.line, here_fn())
                    state = "args"
                    continue
                if lit in ("for", "select"):
                    state = "for_name"
                    continue
                if lit == "case":
                    cases += 1
                    state = "case_word"
                    continue
                if lit == "function":
                    _, pending_fn, _ = self.token(here_fn())
                    if self.next_is("("):
                        self.token(here_fn())
                        if self.next_is(")"):
                            self.token(here_fn())
                    continue
                if lit == "[[":
                    cur.line, cur.kind, state = line, "test", "test"
                    continue
            if state == "cmd" and not cur.words and ASSIGN.match(val):
                if cur.empty():
                    cur.line = line
                cur.assigns.append((val, line))
                cur.end = self.line
                continue
            if cur.empty():
                cur.line = line
            cur.words.append((val, line))
            cur.end = self.line
            state = "args"


def parse_text(text):
    lx = Lexer(text)
    lx.parse_list()
    return lx.cmds


def parse_file(path):
    with open(path, encoding="utf-8", errors="replace") as fh:
        return parse_text(fh.read())


# Обёртки, которые исполняют программу, названную их аргументом: вызов kubectl
# за ними — тоже вызов (так его видит и подставной kubectl).
WRAPPERS = {"command", "builtin", "exec", "env", "nohup", "timeout", "nice", "setsid", "stdbuf", "xargs"}


def call_name(c):
    """Имя, которое команда вызывает как функцию: слово на месте имени команды."""
    return unquote(c.words[0][0]) if c.kind == "simple" and c.words else None


def is_forward(c):
    """Индекс слова `port-forward` вызова kubectl в команде, иначе None. kubectl —
    имя команды либо программа за обёрткой; слово в тексте вызовом не считается."""
    if c.kind != "simple" or not c.words:
        return None
    lits = [unquote(w) or "" for w, _ in c.words]
    base = [x.rsplit("/", 1)[-1] for x in lits]
    if base[0] == "kubectl":
        k = 0
    elif lits[0] in WRAPPERS and "kubectl" in base:
        k = base.index("kubectl")
    else:
        return None
    return next((j for j in range(k + 1, len(lits)) if lits[j] == "port-forward"), None)


def forward_target(c):
    """Слово цели вызова проброса (как написано) либо None."""
    j = is_forward(c)
    return None if j is None else target_after([w for w, _ in c.words], j)


def forward_commands(cmds):
    return [c for c in cmds if is_forward(c) is not None]


def defs_of(cmds):
    """Тела функций: имя → команды тела (вложенные подстановки — тоже тело)."""
    defs = {}
    for c in cmds:
        if c.fn:
            defs.setdefault(c.fn, []).append(c)
    return defs


# Обёртка записи — функция исполнителя суда, а не прогонщика.
OWN_FUNCS = {"kubectl"}


def dump_defs(paths):
    """Тела функций из дампов `declare -f` миров."""
    defs = {}
    for path in paths:
        if os.path.exists(path):
            for n, body in defs_of(parse_file(path)).items():
                defs.setdefault(n, []).extend(body)
    return defs


def function_bodies(recs, cmds):
    """Имя → (команды тела, взято ли из текста прогонщика): из дампов миров (в том
    числе функции, заведённые подключёнными файлами) и из самого прогонщика (в том
    числе функции, определённые ниже конца префикса, — их дамп не видел)."""
    funcs = {n: (b, False) for n, b in dump_defs([r + ".funcs" for r in recs]).items()}
    funcs.update({n: (b, True) for n, b in defs_of(cmds).items()})
    return funcs


def forwarding(funcs):
    """Функции, чьё исполнение открывает проброс: в теле вызов kubectl … port-forward
    либо вызов другой такой функции (замыкание)."""
    fwd = {n for n, (body, _) in funcs.items() if n not in OWN_FUNCS and forward_commands(body)}
    grew = True
    while grew:
        grew = False
        for n, (body, _) in funcs.items():
            if n not in fwd and n not in OWN_FUNCS and any(call_name(c) in fwd for c in body):
                fwd.add(n)
                grew = True
    return fwd


def called_funcs(recs, prefix):
    """Функции прогонщика, стоявшие в стеке записанного вызова проброса."""
    names = set()
    for rec in recs:
        if not os.path.exists(rec):
            continue
        with open(rec, encoding="utf-8", errors="replace") as fh:
            for line in fh:
                for fr in line.split("\t", 1)[0].split("\x1e"):
                    parts = fr.split("\x1d")
                    if len(parts) == 3 and parts[0] == prefix and parts[2] not in {"source", "main"} | OWN_FUNCS:
                        names.add(parts[2])
    return names


def forwarding_of(recs, prefix, cmds):
    """Функции проброса: по телам (дампы миров и текст прогонщика) и по стеку
    записанных вызовов."""
    return forwarding(function_bodies(recs, cmds)) | called_funcs(recs, prefix)


def call_lines(cmds, fwd):
    """Строки, где функция проброса ВЫЗВАНА: её имя стоит на месте имени команды
    (в том числе в подстановке команды и в теле другой функции). Определение
    функции и имя в тексте вызовом не считаются."""
    return sorted({c.words[0][1] for c in cmds if call_name(c) in fwd})


# ─── ПРОДЛЕНИЕ НЕ ИСПОЛНЯЕТ ШАГОВ С ПОБОЧНЫМ ДЕЙСТВИЕМ (#2893) ────────────────
# Строки, на которые префикс дорастает, исполняются, только если каждая их команда
# ИНЕРТНА под подменами суда: не ходит в сеть, не пишет вне журналов суда и не
# исполняет кода, который суд не объявил. Инертна команда, чьё имя —
#   встроенная команда оболочки без исполнения чужого кода (ниже, INERT_BUILTINS);
#   подменённый инструмент (подставной kubectl и заглушки — FWD_GATE_SUBSTITUTED);
#   функция прогонщика, каждая команда тела которой инертна (по замыканию);
#   python3 с объявленным производителем данных суда (FWD_GATE_PRODUCERS);
#   чистый фильтр текста (PURE);
# и чьи перенаправления пишут только в /dev/null, дескриптор или /tmp/ (журналы
# префикса переведены в каталог суда). Остальное — шаг с побочным действием, и
# продление на нём ОСТАНАВЛИВАЕТСЯ находкой. Список закрыт: имя, которого в нём
# нет, — не «наверное безвредно», а причина остановиться.
SUBSTITUTED = set(os.environ.get("FWD_GATE_SUBSTITUTED", "").split())
PRODUCERS = set(os.environ.get("FWD_GATE_PRODUCERS", "").split())
INERT_BUILTINS = {":", "true", "false", "echo", "printf", "local", "declare", "typeset", "export",
                  "readonly", "unset", "shift", "set", "shopt", "test", "[", "read", "wait", "return",
                  "break", "continue", "let", "cd", "pushd", "popd", "getopts", "mapfile", "readarray",
                  "type", "exit", "pwd"}
PURE = {"cut", "tr", "head", "tail", "wc", "basename", "dirname", "sleep", "grep", "cat", "base64",
        "date", "seq", "readlink", "realpath"}
WRITES = {">", ">>", ">|", "<>", "&>", "&>>", ">&"}
# Имена, присваивание которых меняет, ЧТО исполнится дальше: подмены суда живут в PATH.
SEARCH_VARS = {"PATH", "BASH_ENV", "ENV"}


def plain(raw):
    """Слово без кавычек, подстановки остаются текстом."""
    return raw.replace('"', "").replace("'", "")


def safe_write(op, target):
    t = plain(target)
    if op in (">&", "<&") and re.fullmatch(r"\d+-?|-", t):
        return True
    return t in ("/dev/null", "/dev/stdout", "/dev/stderr") or t.startswith("/dev/fd/") \
        or (t.startswith("/tmp/") and ".." not in t)


def step_of(c, funcs, seen=frozenset()):
    """(строка, что это) первого шага с побочным действием в команде; None — инертна."""
    for op, target, line in c.redirs:
        if op in WRITES and not safe_write(op, target):
            return line, f"запись перенаправлением в {target}"
    for a, line in c.assigns:
        if re.split(r"[+\[=]", a, maxsplit=1)[0] in SEARCH_VARS:
            return line, f"присваивание {a.split('=', 1)[0]} меняет поиск программ, а подмены суда живут в нём"
    if c.kind != "simple" or not c.words:
        return None
    raw, line = c.words[0]
    name = unquote(raw)
    args = [unquote(w) for w, _ in c.words[1:]]
    if name is None:
        return line, f"имя команды {raw} собирается при исполнении"
    if name == "command":
        if args[:1] in (["-v"], ["-V"]):
            return None
        return line, "command обходит функции прогонщика и подмены суда"
    if name in ("export", "declare", "typeset", "local", "readonly") and \
            any(re.split(r"[+\[=]", a or "", maxsplit=1)[0] in SEARCH_VARS for a in args):
        return line, f"{name} меняет поиск программ, а подмены суда живут в нём"
    # Функция прогонщика прежде встроенной: одноимённая функция исполняется вместо неё.
    if name in funcs and name not in OWN_FUNCS:
        if name in seen:
            return None
        body, from_text = funcs[name]
        for b in sorted(body, key=lambda x: x.line):
            s = step_of(b, funcs, seen | {name})
            if s:
                where = f" (строка {s[0]})" if from_text else ""
                return line, f"в теле функции {name}{where}: {s[1]}"
        return None
    if name in INERT_BUILTINS or name in SUBSTITUTED:
        return None
    if name in ("python3", "python"):
        script = next((w for w, _ in c.words[1:] if not (unquote(w) or "").startswith("-")), "")
        if any(plain(script) == p or plain(script).endswith("/" + p) for p in PRODUCERS):
            return None
        return line, f"python3 {script} — не объявленный производитель данных суда"
    if name in PURE:
        return None
    return line, f"внешняя программа {name}"


def first_step(cmds, after, upto, funcs):
    """Первый шаг с побочным действием среди команд строк (after, upto] вне тел
    функций (тело исполняется, только когда функцию зовут, и судится через вызов)."""
    for c in sorted((c for c in cmds if c.fn is None and after < c.line <= upto), key=lambda c: c.line):
        s = step_of(c, funcs)
        if s:
            return s
    return None


if sys.argv[1] == "last":
    ends = [c.end for c in forward_commands(parse_file(sys.argv[2]))]
    if ends:
        print(max(ends))
    sys.exit(0)

if sys.argv[1] == "extend":
    # extend <файл> <конец префикса> <префикс> <предел> <записи миров…> — строка ниже
    # конца префикса (и выше предела, если он не 0), где зовётся функция проброса
    # (последняя такая); пусто — некуда.
    cmds = parse_file(sys.argv[2])
    end, limit = int(sys.argv[3]), int(sys.argv[5])
    below = [n for n in call_lines(cmds, forwarding_of(sys.argv[6:], sys.argv[4], cmds))
             if n > end and (not limit or n < limit)]
    if below:
        print(max(below))
    sys.exit(0)

if sys.argv[1] == "inert":
    # inert <файл> <от> <до> <записи миров…> — «<строка>|<что это>» первого шага с
    # побочным действием в строках (от, до]; пусто — продлевать можно.
    cmds = parse_file(sys.argv[2])
    s = first_step(cmds, int(sys.argv[3]), int(sys.argv[4]), function_bodies(sys.argv[5:], cmds))
    if s:
        print(f"{s[0]}|{s[1].replace('|', '¦')}")
    sys.exit(0)

# ─── judge ───────────────────────────────────────────────────────────────────
worlds = sys.argv[2].split(";")
mirror = sys.argv[3]
runners = []
# Разобранный текст и исход продления у каждого прогонщика: «<продлений>:<строка
# остановки>:<причина>», 0 — продление не останавливалось.
parsed, extension = {}, {}
for row in sys.argv[4:]:
    rel, path, first, last, prefix, ext, recs = row.split("|", 6)
    runners.append((rel, int(first), int(last), read_code(path), prefix,
                    [r for r in recs.split(",") if r]))
    parsed[rel] = parse_file(path)
    n_ext, stop, stop_why = (ext.split(":", 2) + ["0", "0", ""])[:3] if ext else ("0", "0", "")
    extension[rel] = (int(n_ext or 0), int(stop or 0), stop_why)

services, knobs, ports, keys = set(), set(), set(), set()
SERVICE_KINDS = {"svc", "service", "services"}
for rel, first, last, code, _, _ in runners:
    if not first:
        continue
    block = [(n, c) for n, c in code if first <= n <= last]
    for cmd in commands(block):
        text = " ".join(c for _, c in cmd)
        for c in (c for _, c in cmd):
            keys.update(KEY.findall(c))
        if "port-forward" not in text:
            continue
        for m in TARGET.finditer(text):
            services.add(m.group(1))
            p = PORTSPEC.match(text[m.end():].lstrip())
            if p:
                var = p.group(1) or p.group(3)
                if var:
                    knobs.add(var)
                for num in (p.group(2), p.group(4)):
                    if num:
                        ports.add(num)

# Цепочка объявлений ручек: `A="${B:-N}"` связывает A, B и N — во всех прогонщиках.
decls = [m.groups() for _, _, _, code, _, _ in runners for _, c in code for m in [KNOB.match(c)] if m]
grew = True
while grew:
    grew = False
    for a, b, num in decls:
        if (a in knobs or b in knobs) and not {a, b, num} <= knobs | ports:
            knobs.update((a, b))
            ports.add(num)
            grew = True

# СТВОЛ ИМЕНИ. Чарт называет службы `<полное имя>-<роль>`, а нагрузку — полным
# именем: у поставщика личности это kacho-umbrella-hydra-public при
# deploy/kacho-umbrella-hydra. Проброс к нагрузке (deploy/, statefulset/ …) по
# имени службы блока не опознаётся (#2866, круг 2): поэтому служба блока без
# последнего сегмента — ствол, и цель, равная стволу или начинающаяся с
# «ствол-», — тоже поставщик (его нагрузка или соседняя служба того же чарта).
stems = {}
for sv in services:
    if "-" in sv:
        stems.setdefault(sv.rsplit("-", 1)[0], set()).add(sv)


def provider_ref(target):
    """Причина находки, если цель (`<вид>/<имя>`) ведёт к поставщику; иначе None."""
    name = target.split("/", 1)[1] if "/" in target else target
    if name in services:
        return f"проброс к службе поставщика {name}"
    for st in sorted(stems, key=len, reverse=True):
        if name == st or name.startswith(st + "-"):
            return f"проброс к поставщику {target} (ствол {st} служб блока {', '.join(sorted(stems[st]))})"
    return None


findings, judged = [], 0
if not services or not keys:
    findings.append(
        f"поставщик не опознан ни по одному блоку (служб {len(services)}, ключей адреса "
        f"{len(keys)}) — суд вне блока беспредметен")
knob_re = [(k, re.compile(r"\$\{?" + re.escape(k) + r"(?!\w)")) for k in sorted(knobs)]
port_re = [(p, re.compile(r"(?:localhost|127\.0\.0\.1):" + re.escape(p) + r"(?![0-9])")) for p in sorted(ports)]
key_re = [(k, re.compile(r"(?<![\w-])" + re.escape(k) + r"=")) for k in sorted(keys)]


def add(why, n, reason):
    have = why.setdefault(n, [])
    if reason not in have:
        have.append(reason)


sites_all, covered_all, execs, calls, bare, unrecorded = 0, 0, 0, 0, [], 0
fn_sites_all, fn_names_all, workload = 0, set(), set()
exts_all, stops_all = 0, 0
for rel, first, last, code, prefix, recs in runners:
    outside = [(n, c) for n, c in code if not (first and first <= n <= last)]
    judged += sum(1 for _, c in outside if c.strip())
    why, why_other, dead, blocked = {}, {}, [], []
    cmds = parsed[rel]
    n_ext, stop, stop_why = extension[rel]
    exts_all += n_ext
    stops_all += 1 if stop else 0
    # Места вызова вне блока — первая строка каждой РАЗОБРАННОЙ команды с вызовом
    # kubectl … port-forward и каждая строка, где функция проброса ВЫЗВАНА (её
    # вызов — тоже вызов проброса). Имя в тексте строки местом вызова не является.
    fwd = forwarding_of(recs, prefix, cmds)
    fn_names_all |= fwd
    fwd_cmds = [c for c in forward_commands(cmds) if not (first and first <= c.line <= last)]
    fn_sites = {n for n in call_lines(cmds, fwd) if not (first and first <= n <= last)}
    sites = sorted({c.line for c in fwd_cmds} | fn_sites)
    sites_all += len(sites)
    fn_sites_all += len(fn_sites)
    # СТАТИЧЕСКИ: служба поставщика литералом в цели вызова (судится и там, куда
    # исполнение не доходит).
    for c in fwd_cmds:
        t = forward_target(c)
        m = TARGET.search(t) if t else None
        r = provider_ref(m.group(0)) if m else None
        if r:
            add(why, c.line, r)
    # ИСПОЛНЕНИЕМ: цель каждого записанного вызова — уже подставленная.
    executed = set()
    for k, rec in enumerate(recs):
        execs += 1
        world = worlds[k] if k < len(worlds) else f"мир {k}"
        done = os.path.exists(rec + ".done") and open(rec + ".done").read().strip() == "done"
        if not done:
            rc = open(rec + ".rc").read().strip() if os.path.exists(rec + ".rc") else "?"
            tail = ""
            if os.path.exists(rec + ".out"):
                tail = " / ".join(x for x in open(rec + ".out", encoding="utf-8", errors="replace").read().splitlines()[-3:] if x.strip())
            findings.append(
                f"{rel} · мир «{world}»: исполнение прервано до последнего вызова проброса "
                f"(код {rc}{'; ' + tail if tail else ''}) — неисполненные вызовы суду не видны")
        if os.path.exists(rec + ".stub"):
            for line in open(rec + ".stub", encoding="utf-8", errors="replace"):
                if line.strip():
                    unrecorded += 1
                    findings.append(
                        f"{rel} · мир «{world}»: вызов проброса мимо оболочки записи («{line.strip()}») — "
                        f"координаты у него нет, и место его суду неизвестно")
        for line in open(rec, encoding="utf-8", errors="replace"):
            line = line.rstrip("\n")
            if not line:
                continue
            stack, argstr = line.split("\t", 1)
            calls += 1
            frames = [(f, int(x)) for f, x, _ in (fr.split("\x1d", 2) for fr in stack.split("\x1e") if fr)]
            args = argstr.split("\x1f")[:-1]
            # Исполнилось КАЖДОЕ место стека в прогонщике: и строка тела помощника,
            # и строка, откуда его позвали. Координата находки — самая внешняя
            # рамка прогонщика: там решено, куда пойдёт проброс.
            own = [x for f, x in frames if f == prefix]
            executed.update(own)
            if own:
                where, no = rel, own[-1]
                if first and first <= no <= last:
                    continue  # блок судят посадки
            else:
                other = [(f, x) for f, x in frames if f.startswith(mirror + os.sep)]
                if not other:
                    continue
                where, no = os.path.relpath(other[0][0], mirror), other[0][1]
            for target in targets_of(args):
                if "/" not in target:
                    if f"{where}:{no}" not in bare:
                        bare.append(f"{where}:{no}")
                    continue
                if target.split("/", 1)[0] not in SERVICE_KINDS:
                    workload.add(f"{where}:{no}")
                r = provider_ref(target)
                if r:
                    add(why if where == rel else why_other, no if where == rel else (where, no), r)
    # Не исполненное из-за остановки продления — не «мёртвое место», а следствие
    # названной находки: вызов ниже строки остановки и вызов в теле функции, каждый
    # вызов которой сам заблокирован (по замыканию).
    unexec = [n for n in sites if n not in executed]
    covered_all += len(sites) - len(unexec)
    held = {n for n in unexec if stop and n >= stop}
    if stop:
        owner = {c.words[0][1]: c.fn for c in cmds if call_name(c) in fwd}
        owner.update({c.line: c.fn for c in fwd_cmds})
        callers = {}
        for c in cmds:
            if c.words and c.kind == "simple":
                callers.setdefault(call_name(c), set()).add(c.words[0][1])
        grew = True
        while grew:
            grew = False
            for n in unexec:
                f = owner.get(n)
                if n not in held and f and callers.get(f) and all(
                        x in held or x >= stop for x in callers[f]):
                    held.add(n)
                    grew = True
    blocked = sorted(held)
    dead = [n for n in unexec if n not in held]
    for n, c in outside:
        for k, rx in key_re:
            if rx.search(c):
                add(why, n, f"ключ адреса поставщика {k}")
        if not KNOB.match(c):
            for k, rx in knob_re:
                if rx.search(c):
                    add(why, n, f"ручка порта поставщика {k}")
        for p, rx in port_re:
            if rx.search(c):
                add(why, n, f"порт поставщика {p}")
    for n in sorted(why):
        findings.append(
            f"{rel}:{n} · вне блока: {', '.join(why[n])} — к поставщику здесь обращаются "
            f"мимо решения по посадке: на цепочке own его нет, и строка ведёт в пустоту")
    for other, n in sorted(why_other):
        findings.append(
            f"{other}:{n} · вне блока (исполнением из {rel}): {', '.join(why_other[(other, n)])} — "
            f"к поставщику здесь обращаются мимо решения по посадке")
    for n in dead:
        findings.append(
            f"{rel}:{n} · вне блока: вызов проброса не исполнился ни в одном мире суда "
            f"({', '.join(worlds)}) — его цель суду неизвестна, и «ЧИСТО» о нём утверждать нечего")
    if stop:
        findings.append(
            f"{rel}:{stop} · продлевать нельзя: {stop_why} — префикс дорастает до вызова "
            f"функции проброса ниже, а этот шаг исполнился бы вне подмен суда; проба его не "
            f"исполняет, и вызовы на строках {', '.join(map(str, blocked)) or '—'} суду не видны")

for f in findings:
    print("F|" + f)
print(f"C|вне блока строк осмотрено {judged} · опознано по блокам: служб поставщика "
      f"{len(services)}, ручек порта {len(knobs)}, портов {len(ports)}, ключей адреса {len(keys)}"
      f" · вне блока мест вызова проброса {sites_all} (из них вызовов функций проброса {fn_sites_all};"
      f" функций {len(fn_names_all)}): исполнено в мирах суда {covered_all},"
      f" не исполнено {sites_all - covered_all} · исполнений прогонщика {execs},"
      f" вызовов проброса записано {calls}, мимо записи {unrecorded}"
      f" · продлений префикса {exts_all}, остановлено на шаге с побочным действием {stops_all}"
      f" · целью без вида {len(bare)} (НЕ судятся: имя пода не называет службы)"
      + (f": {', '.join(bare)}" if bare else "")
      + f" · целью-нагрузкой {len(workload)} (судятся по стволу имени служб блока)")
print(f"D|{len(bare)}")
PY
}

# ─── ИСПОЛНЕНИЕ ПРОГОНЩИКА ───────────────────────────────────────────────────
# Зеркало дерева: всё — ссылки на настоящие файлы, кроме прогонщиков (кладутся
# префиксами) и производителя транспортов (кладётся прокладка ниже). Помощники
# по ссылке находят свой корень по настоящему пути и читают НАСТОЯЩИЕ данные
# дерева, в том числе манифест.
MIRROR="$WORK/mirror"
build_mirror() {
  [ -d "$MIRROR" ] && return 0
  mkdir -p "$MIRROR/deploy/scripts" "$WORK/tmp"
  local e
  for e in "$ROOT"/*; do [ -e "$e" ] && [ "$(basename "$e")" != deploy ] && ln -s "$e" "$MIRROR/"; done
  for e in "$ROOT"/deploy/*; do [ -e "$e" ] && [ "$(basename "$e")" != scripts ] && ln -s "$e" "$MIRROR/deploy/"; done
  for e in "$ROOT"/deploy/scripts/*; do
    [ -e "$e" ] || continue
    case "$(basename "$e")" in newman-*.sh|e2e-optional-transports.py) ;; *) ln -s "$e" "$MIRROR/deploy/scripts/" ;; esac
  done
  # ПРОКЛАДКА: настоящий разбор манифеста, спрос — весь объявленный (довод в
  # шапке). Функция спроса, которой нет, — отказ, а не тихий возврат к спросу
  # дерева.
  cat > "$MIRROR/deploy/scripts/e2e-optional-transports.py" <<'SHIM'
import importlib.util
import os
import sys

real = os.environ["FWD_GATE_TRANSPORTS"]
spec = importlib.util.spec_from_file_location("e2e_optional_transports", real)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
if not callable(getattr(mod, "transports_dialled_by", None)):
    sys.stderr.write(f"прокладка суда пробросов: у {real} нет функции спроса transports_dialled_by — подменять нечего\n")
    sys.exit(3)
mod.transports_dialled_by = lambda suite, variables: (set(variables), 0)
sys.exit(mod.main(sys.argv[1:]))
SHIM
}

# Мир исполнения: «имя|служба|край». Оба мира судят одно правило вне блока;
# второй нужен, чтобы исполнились и места, стоящие за решением по посадке.
EXEC_WORLDS=(
  "цепочка own|own|own"
  "стенд с поставщиком|external|external"
)

exec_prefix() {  # <префикс> <запись> <служба> <край>
  write_stub_kubectl "$3" "$4"
  cat > "$WORK/exec.sh" <<'DRV'
PATH="$WORK/bin:$PATH"
NS=kacho
SCRIPT_DIR="$(dirname "$PREFIX")"
# Оболочка записи: каждый вызов проброса — строкой «стек<TAB>слова», слова через
# \037. Стек — КАЖДАЯ рамка вызова от места kubectl наружу
# («файл\035строка\035функция» через \036): вызов через помощника несёт и строку
# тела помощника, и строку, откуда помощника позвали, и имя помощника, — суд знает,
# какое место вызова исполнилось и какая функция открыла проброс, даже когда
# префикс прерван до дампа функций.
# Строка собирается целиком и пишется ОДНОЙ записью: вызовы идут в фоне
# параллельно, и запись по частям перемешалась бы.
kubectl() {
  case " $* " in
    *" port-forward "*)
      local _rec="" _i
      for ((_i = 1; _i < ${#BASH_SOURCE[@]}; _i++)); do
        _rec+="${_rec:+$'\036'}${BASH_SOURCE[$_i]}"$'\035'"${BASH_LINENO[$((_i - 1))]}"$'\035'"${FUNCNAME[$_i]}"
      done
      _rec+=$'\t'"$(printf '%s\037' "$@")"
      printf '%s\n' "$_rec" >> "$REC"
      return 0 ;;
  esac
  command kubectl "$@"
}
. "$PREFIX"
wait
# Функции, заведённые префиксом: по ним суд узнаёт помощников проброса.
declare -f > "$REC.funcs"
printf 'done\n' > "$REC.done"
DRV
  : > "$2"; : > "$2.done"; : > "$2.funcs"
  env -u SETUP_NS -u SERVICES WORK="$WORK" PREFIX="$1" REC="$2" \
    FWD_GATE_TRANSPORTS="$ROOT/deploy/scripts/e2e-optional-transports.py" \
    timeout "$EXEC_TIMEOUT" bash "$WORK/exec.sh" </dev/null >"$2.out" 2>&1
  echo $? > "$2.rc"
  cp "$WORK/pf.calls" "$2.stub"
}

# construct_end <файл> <строка> — первая строка не раньше данной, до которой
# префикс разбирается оболочкой; пусто — такой нет до конца файла.
construct_end() {
  local n e="$2"; n="$(wc -l < "$1")"
  while [ "$e" -le "$n" ] && ! bash -n <(sed -n "1,${e}p" "$1") 2>/dev/null; do e=$((e + 1)); done
  [ "$e" -le "$n" ] && echo "$e"
}

exec_runners() {
  local row rel abs last_fwd e ext pre recs w k name iam edge r n_ext stop stop_why e2 step next
  local -a fns
  write_analyzer
  for row in "${RUNNER_ROWS[@]}"; do
    IFS='|' read -r rel abs _ _ <<<"$row"
    last_fwd="$(python3 "$WORK/analyze.py" last "$abs")" || {
      FINDINGS+=("$rel: разборщик не прочитал файл — исполнять нечего и судить нечем"); EXEC_ROWS+=("$row|||"); continue; }
    if [ -z "$last_fwd" ]; then EXEC_ROWS+=("$row|||"); continue; fi
    # Префикс кончается там, где кончается КОНСТРУКЦИЯ с последним вызовом:
    # вызов внутри цикла или функции без её конца не разбирается оболочкой.
    e="$(construct_end "$abs" "$last_fwd")"
    if [ -z "$e" ]; then
      FINDINGS+=("$rel: префикс до последнего вызова проброса (строка $last_fwd) не разбирается оболочкой ни на одной строке до конца файла — исполнять нечего")
      EXEC_ROWS+=("$row|||"); continue
    fi
    build_mirror
    pre="$MIRROR/deploy/scripts/$(basename "$abs")"
    # ВЫЗОВ ПОМОЩНИКА — ТОЖЕ ВЫЗОВ ПРОБРОСА (#2866, круг 2). Функция, открывающая
    # проброс, позванная ниже последнего вызова, записанного текстом, за концом
    # префикса не исполнялась, а место её тела исполнялось законным вызовом выше —
    # и покрытие, считавшее места, молчало. Поэтому префикс ДОРАСТАЕТ: после
    # исполнения разборщик узнаёт функции проброса (по дампу функций миров и по
    # тексту прогонщика) и ищет ниже конца префикса строку, где такая функция
    # ВЫЗВАНА; нашлась — префикс продлевается до конца её конструкции и исполняется
    # заново.
    #
    # ПРОДЛЕНИЕ НЕ ИСПОЛНЯЕТ ШАГОВ С ПОБОЧНЫМ ДЕЙСТВИЕМ (#2893). Строки, на которые
    # префикс дорастает, — это шаги прогонщика ПОСЛЕ пробросов: посев, прогон суит.
    # Прежде продление исполняло их как есть, и вызов помощника ниже строки посева
    # запускал настоящий посев — запросы к портам localhost, запись в судимое
    # дерево. Теперь до исполнения разборщик судит каждую команду продлеваемых строк
    # (и тела функций, которые они зовут): шаг вне подмен суда — не исполняется,
    # продление останавливается на нём (строка остановки — предел для следующих
    # продлений: вызовы выше него ещё дорастают), а вызовы ниже становятся
    # находкой «продлевать нельзя» с его строкой и причиной.
    n_ext=0; stop=0; stop_why=""
    while :; do
      sed -n "1,${e}p" "$abs" | sed "s#/tmp/#$WORK/tmp/#g" > "$pre"
      recs=""; k=0; fns=()
      for w in "${EXEC_WORLDS[@]}"; do
        IFS='|' read -r name iam edge <<<"$w"
        k=$((k + 1))
        r="$WORK/rec.$RUNNERS_SEEN.$(basename "$abs").$k"
        exec_prefix "$pre" "$r" "$iam" "$edge"
        recs="$recs${recs:+,}$r"; fns+=("$r")
      done
      next=""
      while :; do
        ext="$(python3 "$WORK/analyze.py" extend "$abs" "$e" "$pre" "$stop" "${fns[@]}")" || {
          FINDINGS+=("$rel: разборщик не узнал функций проброса — продлевать префикс нечем, и вызов помощника ниже суду не виден"); break 2; }
        [ -n "$ext" ] || break 2
        e2="$(construct_end "$abs" "$ext")"
        if [ -z "$e2" ]; then
          FINDINGS+=("$rel: префикс до вызова функции проброса (строка $ext) не разбирается оболочкой ни на одной строке до конца файла — вызов суду не виден")
          break 2
        fi
        step="$(python3 "$WORK/analyze.py" inert "$abs" "$e" "$e2" "${fns[@]}")" || {
          FINDINGS+=("$rel: разборщик не рассудил, инертны ли строки $((e + 1))–$e2 — продлевать префикс нельзя, и вызов помощника на строке $ext суду не виден"); break 2; }
        [ -n "$step" ] || { next="$e2"; break; }
        stop="${step%%|*}"; stop_why="${step#*|}"
      done
      e="$next"; n_ext=$((n_ext + 1))
    done
    EXEC_ROWS+=("$row|$pre|$n_ext:$stop:$stop_why|$recs")
  done
}

# ─── СУД ВНЕ БЛОКА ───────────────────────────────────────────────────────────
# Один проход по всем прогонщикам сразу: поставщик опознаётся по блокам ВСЕХ
# прогонщиков (у одного блок может называть не все службы, что называет другой),
# затем каждая НЕкомментарная строка вне своего блока сверяется с опознанным, а
# каждый записанный вызов проброса — по его подставленной цели.
# Вывод: «F|<находка>» и одна строка «C|<перепись>». Ненулевой код разборщика —
# находка: суд, который не исполнился, зелёным не считается.
audit_outside() {
  [ "${#RUNNER_ROWS[@]}" -gt 0 ] || return 0
  exec_runners
  local out rc line worlds="" w
  for w in "${EXEC_WORLDS[@]}"; do worlds="$worlds${worlds:+;}${w%%|*}"; done
  out="$(python3 "$WORK/analyze.py" judge "$worlds" "$MIRROR" "${EXEC_ROWS[@]}")"; rc=$?
  if [ "$rc" != 0 ]; then
    FINDINGS+=("суд вне блока НЕ ИСПОЛНИЛСЯ (код разборщика $rc): $out")
    return
  fi
  while IFS= read -r line; do
    case "$line" in
      F\|*) FINDINGS+=("${line#F|}") ;;
      C\|*) OUTSIDE_CENSUS="${line#C|}" ;;
      D\|*) OUTSIDE_UNJUDGED="${line#D|}" ;;
    esac
  done <<<"$out"
}

# ─── САМОПРОВЕРКА ────────────────────────────────────────────────────────────
# Синтетический манифест транспортов компонентов: одна строка, служба — вторым
# аргументом. Разбирает его НАСТОЯЩИЙ производитель (копия
# e2e-optional-transports.py рядом), поэтому опыт x1b ставится правкой данных, а
# не текста прогонщика.
write_manifest() {  # <корень> <служба транспорта>
  printf '{"optional_transports": {"compBaseUrl": {"component": "comp", "service": "%s", "target_port": 5, "port_env": "COMP_PORT", "default_port": 15005, "scheme": "http", "why": "транспорт компонента"}}}\n' \
    "$2" > "$1/deploy/e2e-shards.json"
}

self_test() {
  # Счёт утверждений ведёт само исполнение: `ran` растёт в `_st` на каждом
  # вызове, и строки ЧИСТО и ОТКАЗ называют его, а не литерал. Литерал
  # расходился с исполненным: печатал 19 при 18 исполненных.
  local fails=0 ran=0 tmp="$WORK/st" ; mkdir -p "$tmp/deploy/scripts"
  cp "$ROOT_DEFAULT/deploy/scripts/identity-provider-landing.py" \
     "$ROOT_DEFAULT/deploy/scripts/e2e-optional-transports.py" "$tmp/deploy/scripts/"
  write_manifest "$tmp" comp

  # САМОПРОВЕРКА СТРОИТСЯ НА СИНТЕТИКЕ, А НЕ НА ПРОГОНЩИКАХ ДЕРЕВА. Законный
  # близнец — синтетический прогонщик верной формы; каждая инъекция отличается
  # от него ОДНИМ фактом. Прогонщики дерева судит основной проход: взятые
  # близнецом, они сделали бы самопроверку зависимой от живого входа, и её
  # краснота на регрессии прогонщика читалась бы как поломка пробы.
  #
  # ЗАКОННЫЙ БЛИЗНЕЦ: решение по посадке, оба проброса под проверкой живости.
  # Вне блока — то, что там законно: объявления ручек портов, упоминание
  # поставщика в комментарии, проброс к ядру литералом, проброс к ядру службой
  # из переменной, цикл транспортов компонентов по манифесту (форма прогонщиков
  # дерева) и передача адреса суитам массивом.
  cat > "$tmp/deploy/scripts/newman-twin.sh" <<'TWIN'
PA_PORT="${PA_PORT:-14001}"   # объявление ручки связывает число, а не набирает адрес
PB_PORT="${PB_PORT:-14002}"
run() { :; }                  # подставной исполнитель суит
pf_core() { kubectl -n "$NS" port-forward "svc/$1" "$2:3" >/dev/null 2>&1 & PF_PIDS+=($!); }   # помощник проброса
# Комментарий не судится: svc/provider-a, providerPublicBaseUrl=, $PA_PORT, localhost:14001
PF_PIDS=()
PF_WHAT=()
PROVIDER_ENV_ARGS=()
landing="$(python3 "$SCRIPT_DIR/identity-provider-landing.py" --namespace "$NS")" || landing="present|отказ"
if [ "${landing%%|*}" != absent ]; then
  kubectl -n "$NS" port-forward svc/provider-a "$PA_PORT:1" >/dev/null 2>&1 & PF_PIDS+=($!); PF_WHAT+=("1|a|/dev/null")
  kubectl -n "$NS" port-forward svc/provider-b "${PB_PORT}:2" >/dev/null 2>&1 & PF_PIDS+=($!); PF_WHAT+=("2|b|/dev/null")
  PROVIDER_ENV_ARGS=(--env-var "providerPublicBaseUrl=http://localhost:$PA_PORT")
fi
echo "[x] пробросы к поставщику личности: открыто ${#PF_PIDS[@]} — ${landing#*|}"
kubectl -n "$NS" port-forward svc/core "3:3" >/dev/null 2>&1 & PF_PIDS+=($!)   # к ядру вне блока — законно
_s=core; kubectl -n "$NS" port-forward "svc/$_s" "4:3" >/dev/null 2>&1 & PF_PIDS+=($!)   # служба из переменной — ядро
kubectl -n "$NS" port-forward deploy/core "7:3" >/dev/null 2>&1 & PF_PIDS+=($!)   # нагрузка ядра — по имени нагрузки
pf_core core 10               # помощник до последнего вызова проброса, записанного текстом
while IFS='|' read -r _ovar _osvc _otport _oportenv _odport _oscheme _owhy; do
  [ -n "${_ovar:-}" ] || continue
  kubectl -n "$NS" port-forward "svc/$_osvc" "${!_oportenv:-$_odport}:$_otport" >/dev/null 2>&1 & PF_PIDS+=($!)
done < <(python3 "$SCRIPT_DIR/e2e-optional-transports.py" --suites twin --census)
run --env-var "coreBaseUrl=http://localhost:3" ${PROVIDER_ENV_ARGS[@]+"${PROVIDER_ENV_ARGS[@]}"}
pf_core core 11               # помощник ПОСЛЕ последнего вызова проброса, записанного текстом
TWIN
  local d="$tmp/deploy/scripts"
  local twin_lines data_line loop_line
  twin_lines="$(wc -l < "$d/newman-twin.sh")"
  data_line="$(grep -n '^_s=core;' "$d/newman-twin.sh" | cut -d: -f1)"
  loop_line="$(grep -n 'port-forward "svc/\$_osvc"' "$d/newman-twin.sh" | cut -d: -f1)"
  local form_line late_line
  form_line="$(grep -n 'port-forward deploy/core ' "$d/newman-twin.sh" | cut -d: -f1)"
  late_line="$(grep -n '^pf_core core 11 ' "$d/newman-twin.sh" | cut -d: -f1)"
  local app_line=$((twin_lines + 1))
  # ИНЪЕКЦИЯ 1: прежняя форма — пробросы открываются безусловно (снято условие).
  grep -v -e '^landing=' -e '^if ' -e '^fi$' "$d/newman-twin.sh" > "$d/newman-old.sh"
  # ИНЪЕКЦИЯ 2: решение верное, но второй проброс не поставлен под проверку
  # живости (снята одна запись PF_WHAT).
  sed 's#; PF_WHAT+=("2|b|/dev/null")##' "$d/newman-twin.sh" > "$d/newman-unwatched.sh"
  # СЛЕПОТА: прогонщик без блока вовсе.
  echo '# прогонщик без пробросов к поставщику' > "$d/newman-blind.sh"
  # ИНЪЕКЦИИ ВНЕ БЛОКА — блок у каждой тот же, что у близнеца; изменён один факт
  # вне его, и каждая задевает ровно одно правило.
  #   проброс к службе поставщика ниже строки переписи, безусловно (форма #2841);
  { cat "$d/newman-twin.sh"
    echo 'kubectl -n "$NS" port-forward svc/provider-a "5:1" >/dev/null 2>&1 & PF_PIDS+=($!)'
  } > "$d/newman-out-forward.sh"
  #   то же, команда продолжена на следующую строку;
  { cat "$d/newman-twin.sh"
    printf '%s\n' 'kubectl -n "$NS" port-forward \' '  svc/provider-b "6:2" >/dev/null 2>&1 &'
  } > "$d/newman-out-continued.sh"
  #   адрес суитам в прежней форме — ключом, а не массивом блока;
  sed 's#\${PROVIDER_ENV_ARGS\[@\]+"\${PROVIDER_ENV_ARGS\[@\]}"}#--env-var "providerPublicBaseUrl=http://localhost:3"#' \
    "$d/newman-twin.sh" > "$d/newman-out-key.sh"
  #   адрес, набранный ручкой порта поставщика;
  { cat "$d/newman-twin.sh"; echo 'HOOK_URL="http://localhost:$PB_PORT"'; } > "$d/newman-out-knob.sh"
  #   адрес, набранный номером этого порта;
  { cat "$d/newman-twin.sh"; echo 'HOOK_URL="http://127.0.0.1:14002"'; } > "$d/newman-out-port.sh"
  #   служба из ПЕРЕМЕННОЙ называет поставщика — у близнеца та же строка
  #   называет ядро (#2866: прежде такой проброс не судился вовсе);
  sed 's#^_s=core;#_s=provider-a;#' "$d/newman-twin.sh" > "$d/newman-out-data.sh"
  #   цель — НАГРУЗКА поставщика, названная так, как её называет чарт: полным
  #   именем, от которого службы блока отличаются ролью (provider-a, provider-b
  #   при нагрузке provider — как kacho-umbrella-hydra-public при
  #   deploy/kacho-umbrella-hydra). У близнеца та же строка ведёт к нагрузке
  #   ядра (#2866, круг 2: имя службы у нагрузки — идеализация);
  sed 's#port-forward deploy/core #port-forward deploy/provider #' "$d/newman-twin.sh" > "$d/newman-out-form.sh"
  #   помощник проброса, вызванный ПОСЛЕ последнего вызова, записанного текстом,
  #   ведёт к поставщику — у близнеца тот же вызов ведёт к ядру (#2866, круг 2:
  #   покрытие считало места вызова, а не вызовы);
  sed 's#^pf_core core 11 #pf_core provider-a 11 #' "$d/newman-twin.sh" > "$d/newman-out-late.sh"
  #   место вызова, которое не исполняется ни в одном мире суда (функция без
  #   вызова): его цель суду неизвестна, и молчать о нём нельзя.
  { cat "$d/newman-twin.sh"
    echo '_never() { kubectl -n "$NS" port-forward "svc/$1" "9:1" >/dev/null 2>&1 & }'
  } > "$d/newman-out-dead.sh"
  #   вызов проброса мимо оболочки записи (`command kubectl`): подставной
  #   kubectl его видит, координаты у записи нет;
  { cat "$d/newman-twin.sh"
    echo 'command kubectl -n "$NS" port-forward svc/core "3:3" >/dev/null 2>&1 &'
  } > "$d/newman-out-bypass.sh"
  #   исполнение, прерванное до последнего вызова: вызов за `exit` не исполнен.
  { cat "$d/newman-twin.sh"
    echo 'exit 3'
    echo 'kubectl -n "$NS" port-forward svc/core "3:3" >/dev/null 2>&1 &'
  } > "$d/newman-out-abort.sh"
  # ГРАНИЦА, А НЕ ПРАВИЛО: цель без вида — имя пода, а не службы. Такой проброс
  # не судится — и потому обязан попасть в перепись числом и координатой:
  # счётчик, который не доказан инъекцией, мог бы печатать ноль всегда.
  { cat "$d/newman-twin.sh"
    echo 'kubectl -n "$NS" port-forward provider-a-0 "8:1" >/dev/null 2>&1 &'
  } > "$d/newman-out-bare.sh"

  local out rc before after
  before="$(find "$tmp" | sort)"
  # Строки «ok» называют и законных близнецов — находками считается остальное.
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$tmp" 2>&1 | grep -v '^  ok   ')"; rc=${PIPESTATUS[0]}
  after="$(find "$tmp" | sort)"
  # Одно утверждение — одна строка «ok» или «FAIL» с этого отступа: подробность
  # отказа (вывод прогона, многострочный) сдвигается глубже, чтобы её строки
  # «  ok   …» от прогона не читались строками самопроверки.
  _st() {
    ran=$((ran + 1))
    if [ "$2" = 1 ]; then echo "  ok   $1"
    else echo "  FAIL $1: ${3//$'\n'/$'\n'       }"; fails=$((fails + 1)); fi
  }
  # Инъекция вне блока даёт ровно одну находку, и она называет своё правило.
  _one() {  # <файл> <текст правила> [номер строки]
    [ "$(grep -c "$1" <<<"$out")" = 1 ] && grep -q "$1:${3:-[0-9]*} · вне блока: $2" <<<"$out" && echo 1 || echo 0
  }

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
  _st "о близнеце находок нет — ни в блоке, ни вне его, ни исполнением" \
      "$(grep -q 'newman-twin.sh' <<<"$out" && echo 0 || echo 1)" "$out"
  local inj built=1
  for inj in old unwatched out-forward out-continued out-key out-knob out-port out-data out-form out-late out-dead out-bypass out-abort out-bare; do
    cmp -s "$d/newman-twin.sh" "$d/newman-$inj.sh" && built=0
  done
  _st "инъекции построены (каждая отличается от близнеца)" "$built" \
      "инъекция совпала с близнецом — её правка не применилась"

  echo "ось 3а — осматриваемое дерево проба не меняет"
  _st "после прогона в осматриваемом корне ни одного нового файла (в том числе байткода прокладки)" \
      "$([ "$before" = "$after" ] && echo 1 || echo 0)" "$(diff <(printf '%s\n' "$before") <(printf '%s\n' "$after"))"

  echo "ось 4 — слепота пробы объявляется находкой, а не молчанием"
  _st "прогонщик без блока — находка" \
      "$(grep -q 'newman-blind.sh' <<<"$out" && echo 1 || echo 0)" "$out"

  echo "ось 5 — перепись печатается"
  _st "объём осмотренного назван" \
      "$(grep -q 'перепись: прогонщиков осмотрено 16 · блоков вырезано 15' <<<"$out" && echo 1 || echo 0)" "$out"
  _st "и объём осмотренного вне блока — тоже" \
      "$(grep -q 'вне блока строк осмотрено [1-9][0-9]* · опознано по блокам: служб поставщика 2, ручек порта 2, портов 2, ключей адреса 1' <<<"$out" && echo 1 || echo 0)" "$out"
  # Мест вызова вне блока: по семь у пятнадцати прогонщиков, несущих текст
  # близнеца (помощник, ядро литералом, ядро переменной, нагрузка ядра, два вызова
  # помощника, цикл транспортов), и ещё по одному у шести инъекций, дописывающих
  # вызов, — 111, из них вызовов помощника 30; не исполнены три: функция без
  # вызова, вызов мимо оболочки и вызов за `exit`. Исполнений — пятнадцать
  # прогонщиков на два мира; у прогонщика без блока мест вызова нет. Записанных
  # вызовов: в мире own по шесть у пятнадцати, три дописанных исполненных и два
  # безусловных у прежней формы (95); в мире с поставщиком те же 93 вне блока и
  # по два в блоке у пятнадцати (123) — 218. Мимо записи — по разу в каждом мире
  # у одной инъекции. Цель без вида — одна; целей-нагрузок — по одной у
  # пятнадцати прогонщиков.
  _st "места вызова проброса вне блока посчитаны и исполнены, не судимая цель названа координатой" \
      "$(grep -q "вне блока мест вызова проброса 111 (из них вызовов функций проброса 30; функций 2): исполнено в мирах суда 108, не исполнено 3 · исполнений прогонщика 30, вызовов проброса записано 218, мимо записи 2 · " <<<"$out" \
         && grep -q "целью без вида 1 (НЕ судятся: имя пода не называет службы): deploy/scripts/newman-out-bare.sh:$app_line · " <<<"$out" \
         && grep -q "целью-нагрузкой 15 (судятся по стволу имени служб блока)\$" <<<"$out" && echo 1 || echo 0)" "$out"

  echo "ось 6 — весь файл: каждая находка вне блока находится своим правилом"
  _st "проброс к службе поставщика ниже строки переписи" \
      "$(_one newman-out-forward.sh 'проброс к службе поставщика provider-a' "$app_line")" "$out"
  _st "он же, команда продолжена на следующую строку" \
      "$(_one newman-out-continued.sh 'проброс к службе поставщика provider-b' "$app_line")" "$out"
  _st "адрес суитам ключом, а не массивом блока" \
      "$(_one newman-out-key.sh 'ключ адреса поставщика providerPublicBaseUrl')" "$out"
  _st "адрес, набранный ручкой порта поставщика" \
      "$(_one newman-out-knob.sh 'ручка порта поставщика PB_PORT')" "$out"
  _st "адрес, набранный номером порта поставщика" \
      "$(_one newman-out-port.sh 'порт поставщика 14002')" "$out"

  echo "ось 7 — суд исполнением: служба из данных судится так же, как литерал (#2866)"
  _st "служба из переменной называет поставщика — находка с координатой вызова" \
      "$(_one newman-out-data.sh 'проброс к службе поставщика provider-a' "$data_line")" "$out"
  _st "нагрузка поставщика, названная полным именем чарта, — находка по стволу имени служб блока" \
      "$(_one newman-out-form.sh 'проброс к поставщику deploy/provider (ствол provider служб блока provider-a, provider-b)' "$form_line")" "$out"
  _st "помощник проброса, вызванный после последнего вызова, записанного текстом, — находка с координатой вызова помощника" \
      "$(_one newman-out-late.sh 'проброс к службе поставщика provider-a' "$late_line")" "$out"
  _st "место вызова, не исполненное ни в одном мире, — находка, а не молчание" \
      "$(_one newman-out-dead.sh 'вызов проброса не исполнился ни в одном мире суда' "$app_line")" "$out"
  _st "вызов мимо оболочки записи — находка в каждом мире" \
      "$([ "$(grep -c 'newman-out-bypass.sh · мир «[^»]*»: вызов проброса мимо оболочки записи' <<<"$out")" = 2 ] && echo 1 || echo 0)" "$out"
  _st "исполнение, прерванное до последнего вызова, — находка с кодом выхода" \
      "$(grep -q 'newman-out-abort.sh · мир «цепочка own»: исполнение прервано до последнего вызова проброса (код 3' <<<"$out" \
         && grep -q "newman-out-abort.sh:$((app_line + 1)) · вне блока: вызов проброса не исполнился" <<<"$out" && echo 1 || echo 0)" "$out"

  echo "ось 8 — опыт x1b: служба компонента в манифесте заменена службой поставщика"
  mkdir -p "$WORK/x1b/deploy/scripts"
  cp "$ROOT_DEFAULT/deploy/scripts/identity-provider-landing.py" \
     "$ROOT_DEFAULT/deploy/scripts/e2e-optional-transports.py" "$d/newman-twin.sh" "$WORK/x1b/deploy/scripts/"
  write_manifest "$WORK/x1b" provider-a
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$WORK/x1b" 2>&1)"; rc=$?
  _st "прогонщик тот же, что у близнеца, изменены только данные — находка с координатой цикла" \
      "$([ "$rc" != 0 ] && [ "$(grep -c ' · вне блока: ' <<<"$out")" = 1 ] \
         && grep -q "newman-twin.sh:$loop_line · вне блока: проброс к службе поставщика provider-a" <<<"$out" && echo 1 || echo 0)" "rc=$rc / $out"

  echo "ось 9 — поставщик, не опознанный ни по одному блоку, — находка, а не пустой суд"
  mkdir -p "$WORK/noident/deploy/scripts"
  cp "$ROOT_DEFAULT/deploy/scripts/identity-provider-landing.py" \
     "$ROOT_DEFAULT/deploy/scripts/e2e-optional-transports.py" "$WORK/noident/deploy/scripts/"
  write_manifest "$WORK/noident" comp
  sed -e 's#svc/provider-a "\$PA_PORT:1"#"svc/$PROVIDER_SVC" "$PA_PORT:1"#' -e '/svc\/provider-b/d' \
    "$d/newman-twin.sh" > "$WORK/noident/deploy/scripts/newman-noident.sh"
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$WORK/noident" 2>&1)"; rc=$?
  _st "служба поставщика не названа литералом — отказ суда вне блока" \
      "$([ "$rc" != 0 ] && grep -q 'поставщик не опознан ни по одному блоку (служб 0' <<<"$out" && echo 1 || echo 0)" "rc=$rc / $out"

  echo "ось 10 — на дереве без прогонщиков вердикт БЕСПРЕДМЕТЕН, а не зелен"
  mkdir -p "$WORK/empty/deploy/scripts"
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$WORK/empty" 2>&1)"; rc=$?
  _st "пустой обход — отказ" "$([ "$rc" != 0 ] && echo 1 || echo 0)" "rc=$rc / $out"

  # Оси 11 и 12 идут в СВОИХ корнях: их прогонщики не меняют переписи основного
  # корня (ось 5), и каждая пара «инъекция — близнец» судится своим выводом.
  # _one_in <вывод> <файл> <строка> <текст находки>: о файле ровно одна находка,
  # и она стоит на этой строке с этим текстом.
  _one_in() {
    [ "$(grep -c "$2" <<<"$1")" = 1 ] && grep -q "$2:$3 · $4" <<<"$1" && echo 1 || echo 0
  }
  _seed_root() {  # <корень> — синтетика близнеца и производители, как у основного
    mkdir -p "$1/deploy/scripts"
    cp "$ROOT_DEFAULT/deploy/scripts/identity-provider-landing.py" \
       "$ROOT_DEFAULT/deploy/scripts/e2e-optional-transports.py" "$1/deploy/scripts/"
    write_manifest "$1" comp
  }

  echo "ось 11 — место вызова — вызов в разобранной команде, а не имя в тексте строки (#2893)"
  # Имя помощника проброса внутри строкового литерала — ниже префикса и в теле
  # функции — вызовом не является: ни места вызова, ни продления, ни находки о
  # «неисполненном вызове» (опыт g3). Близнец отличается ОДНИМ фактом: в теле
  # той же функции имя стоит в подстановке команды, `"$(pf_core …)"`, — это вызов,
  # и он ведёт к поставщику.
  local txt="$WORK/text"
  _seed_root "$txt"
  { cat "$d/newman-twin.sh"
    echo 'note() { echo "pf_core provider-a 12 — имя помощника в тексте"; }'
    echo 'note; echo "[x] pf_core provider-a 13"'
  } > "$txt/deploy/scripts/newman-text.sh"
  { cat "$d/newman-twin.sh"
    echo 'note() { echo "$(pf_core provider-a 12)"; }'
    echo 'note; echo "[x] pf_core provider-a 13"'
  } > "$txt/deploy/scripts/newman-call.sh"
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$txt" 2>&1 | grep -v '^  ok   ')"; rc=${PIPESTATUS[0]}
  _st "имя функции проброса в тексте строки — не место вызова: о прогонщике ни одной находки" \
      "$(grep -q 'newman-text.sh' <<<"$out" && echo 0 || echo 1)" "$out"
  _st "подстановка команды с тем же именем — вызов: находка с координатой вызова функции" \
      "$(_one_in "$out" newman-call.sh "$((twin_lines + 2))" 'вне блока: проброс к службе поставщика provider-a')" "$out"
  # Мест вызова вне блока: по семь от текста близнеца у обоих прогонщиков и два у
  # близнеца оси — вызов pf_core в теле note и вызов note; в прогонщике с именем в
  # тексте мест не прибавилось. Функций проброса две: pf_core и note.
  _st "перепись считает вызовы, а не имена: мест 16, из них вызовов функций 6, функций 2, исполнены все" \
      "$(grep -q 'вне блока мест вызова проброса 16 (из них вызовов функций проброса 6; функций 2): исполнено в мирах суда 16, не исполнено 0 · ' <<<"$out" && echo 1 || echo 0)" "$out"

  echo "ось 12 — продление префикса не исполняет шагов с побочным действием (#2893)"
  # Посев стоит в дереве вне каталога прогонщиков и, исполнившись, пишет рядом с
  # собой — в осматриваемый корень, как настоящий посев через ссылку зеркала. Вызов
  # помощника ниже шага посева (в тексте прогонщика и в теле его функции) обязан
  # остановить продление находкой, не исполнив посева. Близнецы отличаются ОДНИМ
  # фактом — на месте `bash` встроенная `:`, шаг инертен: продление исполняется, и
  # вызов ниже судится.
  local side="$WORK/side"
  _seed_root "$side"
  mkdir -p "$side/tests/fx"
  cat > "$side/tests/fx/setup.sh" <<'SEED'
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$here/out" && echo seeded > "$here/out/seeded"
SEED
  local sd='"$SCRIPT_DIR/../../tests/fx/setup.sh"'
  { cat "$d/newman-twin.sh"; echo "bash $sd"; echo 'pf_core provider-a 12'; } > "$side/deploy/scripts/newman-seed.sh"
  { cat "$d/newman-twin.sh"; echo ": $sd"; echo 'pf_core provider-a 12'; } > "$side/deploy/scripts/newman-seed-twin.sh"
  { cat "$d/newman-twin.sh"; echo "seed_pf() { bash $sd; pf_core \"\$1\" 12; }"; echo 'seed_pf provider-a'; } \
    > "$side/deploy/scripts/newman-seed-body.sh"
  { cat "$d/newman-twin.sh"; echo "seed_pf() { : $sd; pf_core \"\$1\" 12; }"; echo 'seed_pf provider-a'; } \
    > "$side/deploy/scripts/newman-seed-body-twin.sh"
  # Состав корня — с ДИСКА, а не из индекса: предмет утверждения — файл, которого
  # в индексе нет (запись посева), а у синтетического корня индекса нет вовсе.
  before="$(find "$side" | sort)"
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$side" 2>&1 | grep -v '^  ok   ')"; rc=${PIPESTATUS[0]}
  after="$(find "$side" | sort)"
  _st "посев не исполнен: осматриваемый корень после прогона тот же (посева out/ в нём нет)" \
      "$([ "$before" = "$after" ] && [ ! -e "$side/tests/fx/out" ] && echo 1 || echo 0)" \
      "$(diff <(printf '%s\n' "$before") <(printf '%s\n' "$after"))"
  _st "шаг посева в продлеваемых строках — находка «продлевать нельзя» с его строкой и заблокированным вызовом" \
      "$(_one_in "$out" newman-seed.sh "$((twin_lines + 1))" "продлевать нельзя: внешняя программа bash — .*на строках $((twin_lines + 2)) суду не видны")" "$out"
  _st "близнец (шаг инертен) — продление исполнено, вызов ниже судится" \
      "$(_one_in "$out" newman-seed-twin.sh "$((twin_lines + 2))" 'вне блока: проброс к службе поставщика provider-a')" "$out"
  _st "посев в теле функции проброса — та же находка на строке вызова функции" \
      "$(_one_in "$out" newman-seed-body.sh "$((twin_lines + 2))" "продлевать нельзя: в теле функции seed_pf (строка $((twin_lines + 1))): внешняя программа bash")" "$out"
  _st "близнец тела (шаг инертен) — продление исполнено, вызов через функцию судится" \
      "$(_one_in "$out" newman-seed-body-twin.sh "$((twin_lines + 2))" 'вне блока: проброс к службе поставщика provider-a')" "$out"
  _st "остановки видны в переписи числом" \
      "$(grep -q 'остановлено на шаге с побочным действием 2 · ' <<<"$out" && [ "$rc" != 0 ] && echo 1 || echo 0)" "rc=$rc / $out"

  echo
  if [ "$ran" -eq 0 ]; then
    echo "ОТКАЗ: самопроверка не исполнила ни одного утверждения — вердикт беспредметен" >&2; return 1
  fi
  if [ "$fails" -gt 0 ]; then
    echo "ОТКАЗ: провалено утверждений $fails из $ran" >&2; return 1
  fi
  echo "ЧИСТО: $ran утверждений — проба способна упасть на прежней форме, на пробросе вне живости, на каждой из четырёх находок вне блока, на службе поставщика из переменной, из манифеста (x1b), на нагрузке поставщика под полным именем чарта, на помощнике, позванном после последнего вызова, записанного текстом, на месте вызова, которое не исполнилось, на вызове мимо записи и на прерванном исполнении, на вызове в подстановке команды и на шаге с побочным действием в продлеваемых строках, смолчать на имени функции проброса в тексте строки, не исполнить посев, не тронуть осматриваемое дерево, смолчать на законном близнеце, объявить свою слепоту и назвать в переписи цель, которой не судит"
  return 0
}

[ "$SELF_TEST" = 1 ] && { self_test; exit $?; }

for f in "$ROOT"/deploy/scripts/newman-*.sh; do
  [ -e "$f" ] || continue
  audit_runner "deploy/scripts/$(basename "$f")" "$f" "$ROOT/deploy/scripts"
done

audit_outside

echo "перепись: прогонщиков осмотрено $RUNNERS_SEEN · блоков вырезано $BLOCKS_CUT · посадок прогнано $LANDINGS_RUN · $OUTSIDE_CENSUS"
if [ "$RUNNERS_SEEN" -eq 0 ]; then
  echo "ОТКАЗ: прогонщиков не прочитано — вердикт беспредметен" >&2; exit 1
fi
if [ "${#FINDINGS[@]}" -gt 0 ]; then
  echo "НАХОДКИ (${#FINDINGS[@]}):" >&2
  for x in "${FINDINGS[@]}"; do echo "  $x" >&2; done
  exit 1
fi
echo "ЧИСТО: пробросы блока следуют посадке на каждой из ${#LANDINGS[@]} посадок у каждого прогонщика; вне блока каждое место вызова проброса исполнено, в том числе каждый вызов функции проброса, и ни один исполненный проброс не ведёт к поставщику — ни к его службе, ни к его нагрузке по стволу имени служб блока, ни литералом, ни из данных (переменная, помощник, манифест); ключа адреса, ручки и номера порта поставщика вне блока нет; вызовов проброса с целью без вида $OUTSIDE_UNJUDGED — НЕ судятся, координаты в переписи"
