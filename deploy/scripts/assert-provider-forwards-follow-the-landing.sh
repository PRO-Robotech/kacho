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
# дампу функций после префикса и по стеку записанных вызовов. Строка, где имя
# функции проброса стоит словом, — место вызова, как и команда с `kubectl … port-forward`,
# и префикс ДОРАСТАЕТ до последней такой строки: помощник, позванный ниже
# последнего вызова, записанного текстом, прежде за концом префикса не
# исполнялся, а место его тела исполнял законный вызов выше, и покрытие молчало.
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
# блока (команда с `kubectl` и `port-forward`) обязан исполниться хотя бы в
# одном мире: иначе его цель суду неизвестна, и «ЧИСТО» о нём утверждать нечего.
# Тем же счётом идёт прогон, прерванный до конца префикса, и вызов проброса мимо
# оболочки записи (его ловит подставной kubectl, но координаты у такой записи
# нет).
#
# СТАТИЧЕСКИ вне блока по-прежнему судятся (в том числе строки, которых
# исполнение не достигает):
#   проброс к поставщику — служба блока или её ствол имени (см. выше), — если
#   цель названа ЛИТЕРАЛОМ после `<вид>/` (продолжение строки обратной косой
#   чертой склеивается: команда судится целиком, координата — её первая строка);
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
# имени, которое в тексте словом не стоит (`eval`, имя, собранное из частей);
# адрес, собранный при исполнении из частей, не несущих ни ключа, ни ручки, ни
# номера порта; вызов kubectl по абсолютному пути (он минует и оболочку, и
# подставной kubectl). Комментарий не судится: он ничего не открывает.
#
# «НОЛЬ НАХОДОК» ОТЛИЧИМО ОТ «НОЛЬ ПРОЧИТАННОГО»: перепись печатается всегда
# (прогонщики, блоки, посадки, строки, места вызова проброса вне блока — из них
# вызовы функций проброса — и сколько из них исполнено, исполнения и записанные
# вызовы, цели без вида и цели-нагрузки), прогонщик без вырезаемого блока —
# находка, пустой обход — отказ. Осматриваемое дерево проба не меняет: байткод
# прокладки не пишется (PYTHONDONTWRITEBYTECODE).
#
# Самопроверка: `--self-test` (прогонщик прежней формы — пробросы безусловно —
# обязан быть найден; проброс вне проверки живости — тоже; каждая из четырёх
# статических находок вне блока — тоже; служба поставщика из переменной, из
# манифеста (опыт x1b), нагрузка поставщика, названная полным именем чарта, и
# помощник проброса, позванный после последнего вызова, записанного текстом, —
# тоже; место вызова, не исполненное ни в одном мире, — тоже; осматриваемый
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
  for t in newman grpcurl jq; do
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
# Один файл на два вопроса: `last <файл>` — последняя строка последней команды с
# вызовом проброса (где кончается префикс, который исполняется); `judge <строки>`
# — суд вне блока по тексту и по записям исполнения.
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
KUBECTL = re.compile(r'\bkubectl\b')


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


def forward_targets(cmd):
    """Цели вызовов `kubectl … port-forward` в команде.

    Строка, где нет kubectl, вызовом не считается: это сообщение о пробросе, а
    не проброс."""
    words = [w for _, c in cmd for w in c.rstrip().rstrip("\\").split()]
    if not any(KUBECTL.search(w) for w in words):
        return []
    return targets_of(words)


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


def forward_commands(code):
    """(первая строка, последняя строка) каждой команды с вызовом проброса."""
    for cmd in commands(code):
        if "port-forward" in " ".join(c for _, c in cmd) and forward_targets(cmd):
            yield cmd[0][0], cmd[-1][0]


FN_HEAD = re.compile(r"^([^\s()=]+) \(\)\s*$")
# Обёртка записи — функция исполнителя суда, а не прогонщика.
OWN_FUNCS = {"kubectl"}


def read_funcs(paths):
    """Тела функций из дампов `declare -f` миров: имя → строки кода тела."""
    funcs = {}
    for path in paths:
        if not os.path.exists(path):
            continue
        name = None
        with open(path, encoding="utf-8", errors="replace") as fh:
            for line in fh.read().split("\n"):
                m = FN_HEAD.match(line)
                if m:
                    name = m.group(1)
                    funcs.setdefault(name, [])
                elif name:
                    funcs[name].append(code_of(line))
    return funcs


def word_re(name):
    return re.compile(r"(?<![\w-])" + re.escape(name) + r"(?![\w-])")


def def_re(name):
    return re.compile(r"^\s*(?:function\s+" + re.escape(name) + r"\b|" + re.escape(name) + r"\s*\(\))")


def forwarding(funcs):
    """Функции, чьё исполнение открывает проброс: в теле вызов kubectl … port-forward
    либо вызов другой такой функции (замыкание)."""
    fwd = {n for n, body in funcs.items() if n not in OWN_FUNCS
           and any(forward_targets([(0, c)]) for c in body)}
    grew = True
    while grew:
        grew = False
        for n, body in funcs.items():
            if n in fwd or n in OWN_FUNCS:
                continue
            if any(word_re(f).search(c) for f in fwd for c in body):
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


def forwarding_of(recs, prefix):
    """Функции проброса: по дампам функций миров и по стеку записанных вызовов."""
    return forwarding(read_funcs([r + ".funcs" for r in recs])) | called_funcs(recs, prefix)


def call_lines(code, fwd):
    """Строки кода, где имя функции проброса стоит словом, — кроме её объявления."""
    rx = [(f, word_re(f), def_re(f)) for f in sorted(fwd)]
    return sorted({n for n, c in code for f, w, d in rx if w.search(c) and not d.match(c)})


if sys.argv[1] == "last":
    ends = [last for _, last in forward_commands(read_code(sys.argv[2]))]
    if ends:
        print(max(ends))
    sys.exit(0)

if sys.argv[1] == "extend":
    # extend <файл> <конец префикса> <префикс> <записи миров…> — строка ниже конца
    # префикса, где зовётся функция проброса (последняя такая); пусто — некуда.
    end = int(sys.argv[3])
    below = [n for n in call_lines(read_code(sys.argv[2]), forwarding_of(sys.argv[5:], sys.argv[4])) if n > end]
    if below:
        print(max(below))
    sys.exit(0)

# ─── judge ───────────────────────────────────────────────────────────────────
worlds = sys.argv[2].split(";")
mirror = sys.argv[3]
runners = []
for row in sys.argv[4:]:
    rel, path, first, last, prefix, recs = row.split("|", 5)
    runners.append((rel, int(first), int(last), read_code(path), prefix,
                    [r for r in recs.split(",") if r]))

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
for rel, first, last, code, prefix, recs in runners:
    outside = [(n, c) for n, c in code if not (first and first <= n <= last)]
    judged += sum(1 for _, c in outside if c.strip())
    why, why_other, dead = {}, {}, []
    # Места вызова вне блока — первая строка каждой команды с вызовом проброса и
    # каждая строка, где зовётся функция проброса (её вызов — тоже вызов проброса).
    fwd = forwarding_of(recs, prefix)
    fn_names_all |= fwd
    fn_sites = set(call_lines(outside, fwd))
    sites = sorted({f for f, _ in forward_commands(outside)} | fn_sites)
    sites_all += len(sites)
    fn_sites_all += len(fn_sites)
    # СТАТИЧЕСКИ: служба поставщика литералом (судится и там, куда исполнение
    # не доходит).
    for cmd in commands(outside):
        if "port-forward" not in " ".join(c for _, c in cmd):
            continue
        for n, c in cmd:
            for m in TARGET.finditer(c):
                r = provider_ref(m.group(0))
                if r:
                    add(why, cmd[0][0], r)
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
    for n in sites:
        if n in executed:
            covered_all += 1
        else:
            dead.append(n)
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

for f in findings:
    print("F|" + f)
print(f"C|вне блока строк осмотрено {judged} · опознано по блокам: служб поставщика "
      f"{len(services)}, ручек порта {len(knobs)}, портов {len(ports)}, ключей адреса {len(keys)}"
      f" · вне блока мест вызова проброса {sites_all} (из них вызовов функций проброса {fn_sites_all};"
      f" функций {len(fn_names_all)}): исполнено в мирах суда {covered_all},"
      f" не исполнено {sites_all - covered_all} · исполнений прогонщика {execs},"
      f" вызовов проброса записано {calls}, мимо записи {unrecorded}"
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
  local row rel abs last_fwd e ext pre recs w k name iam edge r
  local -a fns
  write_analyzer
  for row in "${RUNNER_ROWS[@]}"; do
    IFS='|' read -r rel abs _ _ <<<"$row"
    last_fwd="$(python3 "$WORK/analyze.py" last "$abs")" || {
      FINDINGS+=("$rel: разборщик не прочитал файл — исполнять нечего и судить нечем"); EXEC_ROWS+=("$row||"); continue; }
    if [ -z "$last_fwd" ]; then EXEC_ROWS+=("$row||"); continue; fi
    # Префикс кончается там, где кончается КОНСТРУКЦИЯ с последним вызовом:
    # вызов внутри цикла или функции без её конца не разбирается оболочкой.
    e="$(construct_end "$abs" "$last_fwd")"
    if [ -z "$e" ]; then
      FINDINGS+=("$rel: префикс до последнего вызова проброса (строка $last_fwd) не разбирается оболочкой ни на одной строке до конца файла — исполнять нечего")
      EXEC_ROWS+=("$row||"); continue
    fi
    build_mirror
    pre="$MIRROR/deploy/scripts/$(basename "$abs")"
    # ВЫЗОВ ПОМОЩНИКА — ТОЖЕ ВЫЗОВ ПРОБРОСА (#2866, круг 2). Функция, открывающая
    # проброс, позванная ниже последнего вызова, записанного текстом, за концом
    # префикса не исполнялась, а место её тела исполнялось законным вызовом выше —
    # и покрытие, считавшее места, молчало. Поэтому префикс ДОРАСТАЕТ: после
    # исполнения разборщик узнаёт функции проброса (по дампу функций миров) и
    # ищет ниже конца префикса строку, где имя такой функции стоит словом; нашлась —
    # префикс продлевается до конца её конструкции и исполняется заново.
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
      ext="$(python3 "$WORK/analyze.py" extend "$abs" "$e" "$pre" "${fns[@]}")" || {
        FINDINGS+=("$rel: разборщик не узнал функций проброса — продлевать префикс нечем, и вызов помощника ниже суду не виден"); break; }
      [ -n "$ext" ] || break
      e="$(construct_end "$abs" "$ext")"
      if [ -z "$e" ]; then
        FINDINGS+=("$rel: префикс до вызова функции проброса (строка $ext) не разбирается оболочкой ни на одной строке до конца файла — вызов суду не виден")
        break
      fi
    done
    EXEC_ROWS+=("$row|$pre|$recs")
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

  echo
  if [ "$ran" -eq 0 ]; then
    echo "ОТКАЗ: самопроверка не исполнила ни одного утверждения — вердикт беспредметен" >&2; return 1
  fi
  if [ "$fails" -gt 0 ]; then
    echo "ОТКАЗ: провалено утверждений $fails из $ran" >&2; return 1
  fi
  echo "ЧИСТО: $ran утверждений — проба способна упасть на прежней форме, на пробросе вне живости, на каждой из четырёх находок вне блока, на службе поставщика из переменной, из манифеста (x1b), на нагрузке поставщика под полным именем чарта, на помощнике, позванном после последнего вызова, записанного текстом, на месте вызова, которое не исполнилось, на вызове мимо записи и на прерванном исполнении, не тронуть осматриваемое дерево, смолчать на законном близнеце, объявить свою слепоту и назвать в переписи цель, которой не судит"
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
