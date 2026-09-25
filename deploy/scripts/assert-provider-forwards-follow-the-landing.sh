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
# Про строки ВНЕ блока проба утверждает меньше — ровно то, что перечислено в
# разделе «СУДИТСЯ ВЕСЬ ФАЙЛ» ниже. «На цепочке own к поставщику не открыто ни
# одного проброса вообще» она НЕ утверждает: проброс, чья служба приходит из
# данных, и проброс к цели вне известных суду видов не судятся (граница названа
# там же).
#
# ПРОБА ГОНЯЕТ НАСТОЯЩИЙ БЛОК, А НЕ ЕГО ПЕРЕСКАЗ. Блок вырезается из файла
# прогонщика (от `PROVIDER_ENV_ARGS=()` до строки переписи «пробросы к
# поставщику личности: открыто …») и исполняется под ПОДСТАВНЫМ kubectl, который
# отвечает строкой посадки каждой половины и записывает каждый запрошенный
# проброс. Число пробросов блока берётся из самого блока, а не выписывается.
#
# СУДИТСЯ ВЕСЬ ФАЙЛ, А НЕ ОДИН БЛОК. Исход блока ничего не говорит о строке вне
# его: безусловный проброс к службе поставщика ниже строки переписи или адрес
# суитам, переданный прежней формой при запуске волны, блока не меняют — и проба,
# гонявшая только блок, отдавала на обоих код 0 (#2735). Поэтому каждая
# НЕкомментарная строка прогонщика ВНЕ блока судится ещё и статически, и
# находка — любое из четырёх:
#   проброс к службе, которую блок называет службой поставщика, если служба
#   названа ЛИТЕРАЛОМ после `<вид>/` (продолжение строки обратной косой чертой
#   склеивается: команда судится целиком);
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
# ЧЕГО СУД ВНЕ БЛОКА НЕ ВИДИТ: службу поставщика, которую не называет ни один
# блок, и адрес, собранный при исполнении из частей, не несущих ни ключа, ни
# ручки, ни номера порта. Комментарий не судится: он ничего не открывает.
#
# ГРАНИЦА: ПРОБРОСЫ ИЗ ДАННЫХ НЕ СУДЯТСЯ — задача волны-3 #2797. Проброс вне
# блока, чья цель несёт подстановку (`"svc/$svc"` собственного фронта,
# `"svc/$_osvc"` транспортов компонентов, которые перечисляет манифест
# deploy/e2e-shards.json → optional_transports), называет службу, известную
# только при исполнении, и статический суд её не видит. Правка данных, уводящая
# такой проброс к службе поставщика, этой пробой НЕ ловится: она читает только
# файлы прогонщиков, манифест в её вход не входит. Тем же счётом идёт цель вне
# известных суду видов (`statefulset/…`, имя пода без вида): служба в ней
# литералом, но правило её не узнаёт. Такие пробросы не судятся, но СЧИТАЮТСЯ:
# перепись печатает, сколько вызовов `kubectl … port-forward` вне блока
# осмотрено, сколько названо литералом известного вида (судятся) и сколько нет
# (не судятся) — с координатами. Поэтому «ЧИСТО» не утверждает, что вне блока
# пробросов к поставщику нет: оно утверждает, что их нет среди судимых форм.
#
# «НОЛЬ НАХОДОК» ОТЛИЧИМО ОТ «НОЛЬ ПРОЧИТАННОГО»: перепись печатается всегда
# (прогонщики, блоки, посадки, строки и вызовы проброса, осмотренные вне блока),
# прогонщик без вырезаемого блока — находка, пустой обход — отказ.
#
# Самопроверка: `--self-test` (прогонщик прежней формы — пробросы безусловно —
# обязан быть найден; проброс вне проверки живости — тоже; каждая из четырёх
# находок вне блока — тоже; синтетический законный близнец, отличающийся от
# каждой инъекции одним фактом, обязан молчать; проброс вне блока, которого суд
# не судит, — служба из данных, цель вне известных видов — обязан попасть в
# перепись числом и координатой).
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
# «<отн. путь>|<абс. путь>|<первая строка блока>|<последняя>»; 0|0 — блок не
# вырезался, и вне блока тогда весь файл.
RUNNER_ROWS=()
OUTSIDE_CENSUS="не исполнялся"
# Число вызовов проброса вне блока, которых проба не судит (служба из данных либо
# цель вне известных видов — граница в шапке): строка ЧИСТО называет их числом,
# а не молчит.
OUTSIDE_UNJUDGED="?"

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

# ─── СУД ВНЕ БЛОКА ───────────────────────────────────────────────────────────
# Один проход по всем прогонщикам сразу: поставщик опознаётся по блокам ВСЕХ
# прогонщиков (у одного блок может называть не все службы, что называет другой),
# затем каждая НЕкомментарная строка вне своего блока сверяется с опознанным.
# Вывод: «F|<находка>» и одна строка «C|<перепись>». Ненулевой код разборщика —
# находка: суд, который не исполнился, зелёным не считается.
audit_outside() {
  [ "${#RUNNER_ROWS[@]}" -gt 0 ] || return 0
  local out rc line
  out="$(python3 - "${RUNNER_ROWS[@]}" <<'PY'
import re
import sys

TARGET = re.compile(r'(?<![\w$/-])(?:svc|service|services|deploy|deployment|deployments|pod|pods|po)/([A-Za-z0-9][A-Za-z0-9.-]*)')
PORTSPEC = re.compile(r'"?(?:\$\{([A-Za-z_]\w*)(?::-([0-9]+))?\}|\$([A-Za-z_]\w*)|([0-9]+)):')
KEY = re.compile(r'--env-var[\s=]+"?([A-Za-z_]\w*)=')
KNOB = re.compile(r'^\s*([A-Za-z_]\w*)="\$\{([A-Za-z_]\w*):-([0-9]+)\}"\s*$')


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


PF_WORD = re.compile(r'\bport-forward\b')
KUBECTL = re.compile(r'\bkubectl\b')


def forward_targets(cmd):
    """Цели вызовов `kubectl … port-forward` в команде: (номер строки, цель).

    Цель — первый не-флаг после port-forward; флаг без `=` берёт значение
    следующим словом. Строка, где нет kubectl, вызовом не считается: это
    сообщение о пробросе, а не проброс."""
    words = [(n, w) for n, c in cmd for w in c.rstrip().rstrip("\\").split()]
    if not any(KUBECTL.search(w) for _, w in words):
        return []
    out = []
    for i, (n, w) in enumerate(words):
        if not PF_WORD.search(w):
            continue
        j = i + 1
        while j < len(words) and words[j][1].startswith("-"):
            j += 1 if "=" in words[j][1] else 2
        if j < len(words):
            out.append(words[j])
    return out


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


runners = []
for row in sys.argv[1:]:
    rel, path, first, last = row.split("|", 3)
    with open(path, encoding="utf-8") as fh:
        lines = fh.read().split("\n")
    code = [(n, code_of(t)) for n, t in enumerate(lines, 1)]
    runners.append((rel, int(first), int(last), code))

services, knobs, ports, keys = set(), set(), set(), set()
for rel, first, last, code in runners:
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
decls = [m.groups() for _, _, _, code in runners for _, c in code for m in [KNOB.match(c)] if m]
grew = True
while grew:
    grew = False
    for a, b, num in decls:
        if (a in knobs or b in knobs) and not {a, b, num} <= knobs | ports:
            knobs.update((a, b))
            ports.add(num)
            grew = True

findings, judged = [], 0
# Вызовы проброса вне блока: служба литералом известного вида — судятся правилом
# ниже; цель с подстановкой (служба из данных) и цель вне известных видов — НЕ
# судятся (задача волны-3 #2797), но считаются и называются координатой: «ЧИСТО»
# не вправе молчать о том, чего суд не читал.
fwd_literal, fwd_data, fwd_form = 0, [], []
if not services or not keys:
    findings.append(
        f"поставщик не опознан ни по одному блоку (служб {len(services)}, ключей адреса "
        f"{len(keys)}) — суд вне блока беспредметен")
knob_re = [(k, re.compile(r"\$\{?" + re.escape(k) + r"(?!\w)")) for k in sorted(knobs)]
port_re = [(p, re.compile(r"(?:localhost|127\.0\.0\.1):" + re.escape(p) + r"(?![0-9])")) for p in sorted(ports)]
key_re = [(k, re.compile(r"(?<![\w-])" + re.escape(k) + r"=")) for k in sorted(keys)]
for rel, first, last, code in runners:
    outside = [(n, c) for n, c in code if not (first and first <= n <= last)]
    judged += sum(1 for _, c in outside if c.strip())
    why = {}
    for cmd in commands(outside):
        if "port-forward" not in " ".join(c for _, c in cmd):
            continue
        for n, target in forward_targets(cmd):
            if "$" in target:
                fwd_data.append(f"{rel}:{n}")
            elif TARGET.search(target):
                fwd_literal += 1
            else:
                fwd_form.append(f"{rel}:{n}")
        for n, c in cmd:
            for m in TARGET.finditer(c):
                if m.group(1) in services:
                    why.setdefault(n, []).append(f"проброс к службе поставщика {m.group(1)}")
    for n, c in outside:
        for k, rx in key_re:
            if rx.search(c):
                why.setdefault(n, []).append(f"ключ адреса поставщика {k}")
        if not KNOB.match(c):
            for k, rx in knob_re:
                if rx.search(c):
                    why.setdefault(n, []).append(f"ручка порта поставщика {k}")
        for p, rx in port_re:
            if rx.search(c):
                why.setdefault(n, []).append(f"порт поставщика {p}")
    for n in sorted(why):
        findings.append(
            f"{rel}:{n} · вне блока: {', '.join(why[n])} — к поставщику здесь обращаются "
            f"мимо решения по посадке: на цепочке own его нет, и строка ведёт в пустоту")

for f in findings:
    print("F|" + f)
print(f"C|вне блока строк осмотрено {judged} · опознано по блокам: служб поставщика "
      f"{len(services)}, ручек порта {len(knobs)}, портов {len(ports)}, ключей адреса {len(keys)}"
      f" · вне блока вызовов проброса {fwd_literal + len(fwd_data) + len(fwd_form)}: службой литералом"
      f" {fwd_literal} (судятся), службой из данных {len(fwd_data)} и целью вне известных видов"
      f" {len(fwd_form)} (НЕ судятся, задача волны-3 #2797)"
      + (f": {', '.join(fwd_data + fwd_form)}" if fwd_data or fwd_form else ""))
print(f"D|{len(fwd_data) + len(fwd_form)}")
PY
)"; rc=$?
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
  # Вне блока — то, что там законно: объявления ручек портов, упоминание
  # поставщика в комментарии, проброс к ядру и передача адреса суитам массивом.
  cat > "$tmp/deploy/scripts/newman-twin.sh" <<'TWIN'
PA_PORT="${PA_PORT:-14001}"   # объявление ручки связывает число, а не набирает адрес
PB_PORT="${PB_PORT:-14002}"
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
run --env-var "coreBaseUrl=http://localhost:3" ${PROVIDER_ENV_ARGS[@]+"${PROVIDER_ENV_ARGS[@]}"}
TWIN
  local d="$tmp/deploy/scripts"
  # ИНЪЕКЦИЯ 1: прежняя форма — пробросы открываются безусловно (снято условие).
  grep -v -e '^landing=' -e '^if ' -e '^fi$' "$d/newman-twin.sh" > "$d/newman-old.sh"
  # ИНЪЕКЦИЯ 2: решение верное, но второй проброс не поставлен под проверку
  # живости (снята одна запись PF_WHAT).
  sed 's#; PF_WHAT+=("2|b|/dev/null")##' "$d/newman-twin.sh" > "$d/newman-unwatched.sh"
  # СЛЕПОТА: прогонщик без блока вовсе.
  echo '# прогонщик без пробросов к поставщику' > "$d/newman-blind.sh"
  # ИНЪЕКЦИИ ВНЕ БЛОКА — блок у каждой тот же, что у близнеца; изменён один факт
  # вне его, и каждая задевает ровно одно правило из четырёх.
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
  #   адрес, набранный номером этого порта.
  { cat "$d/newman-twin.sh"; echo 'HOOK_URL="http://127.0.0.1:14002"'; } > "$d/newman-out-port.sh"
  # ГРАНИЦА, А НЕ ПРАВИЛО: проброс вне блока со службой из данных (служба
  # поставщика приходит переменной). Он не судится (задача волны-3 #2797) — и
  # потому обязан попасть в перепись числом и координатой: счётчик, который не
  # доказан инъекцией, мог бы печатать ноль всегда.
  { cat "$d/newman-twin.sh"
    echo '_s=provider-a; kubectl -n "$NS" port-forward "svc/$_s" "7:1" >/dev/null 2>&1 &'
  } > "$d/newman-out-data.sh"
  #   то же для цели вне известных суду видов: служба литералом, вид не узнан.
  { cat "$d/newman-twin.sh"
    echo 'kubectl -n "$NS" port-forward statefulset/provider-a "8:1" >/dev/null 2>&1 &'
  } > "$d/newman-out-form.sh"
  local data_line; data_line=$(( $(wc -l < "$d/newman-twin.sh") + 1 ))

  local out rc
  # Строки «ok» называют и законных близнецов — находками считается остальное.
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$tmp" 2>&1 | grep -v '^  ok   ')"; rc=${PIPESTATUS[0]}
  _st() { if [ "$2" = 1 ]; then echo "  ok   $1"; else echo "  FAIL $1: $3"; fails=$((fails + 1)); fi; }
  # Инъекция вне блока даёт ровно одну находку, и она называет своё правило.
  _one() {  # <файл> <текст правила>
    [ "$(grep -c "$1" <<<"$out")" = 1 ] && grep -q "$1:[0-9]* · вне блока: $2" <<<"$out" && echo 1 || echo 0
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
  _st "о близнеце находок нет — ни в блоке, ни вне его" \
      "$(grep -q 'newman-twin.sh' <<<"$out" && echo 0 || echo 1)" "$out"
  local inj built=1
  for inj in old unwatched out-forward out-continued out-key out-knob out-port out-data out-form; do
    cmp -s "$d/newman-twin.sh" "$d/newman-$inj.sh" && built=0
  done
  _st "инъекции построены (каждая отличается от близнеца)" "$built" \
      "инъекция совпала с близнецом — её правка не применилась"

  echo "ось 4 — слепота пробы объявляется находкой, а не молчанием"
  _st "прогонщик без блока — находка" \
      "$(grep -q 'newman-blind.sh' <<<"$out" && echo 1 || echo 0)" "$out"

  echo "ось 5 — перепись печатается"
  _st "объём осмотренного назван" \
      "$(grep -q 'перепись: прогонщиков осмотрено 11 · блоков вырезано 10' <<<"$out" && echo 1 || echo 0)" "$out"
  _st "и объём осмотренного вне блока — тоже" \
      "$(grep -q 'вне блока строк осмотрено [1-9][0-9]* · опознано по блокам: служб поставщика 2, ручек порта 2, портов 2, ключей адреса 1' <<<"$out" && echo 1 || echo 0)" "$out"
  # Литералом вне блока: по пробросу к ядру у десяти прогонщиков, несущих текст
  # близнеца (он сам и девять инъекций), и ещё по одному у двух инъекций проброса
  # к поставщику; не судимых — ровно по строке у двух инъекций границы, и перепись
  # называет обе.
  _st "вызовы проброса вне блока посчитаны, не судимые названы координатой" \
      "$(grep -q "вне блока вызовов проброса 14: службой литералом 12 (судятся), службой из данных 1 и целью вне известных видов 1 (НЕ судятся, задача волны-3 #2797): deploy/scripts/newman-out-data.sh:$data_line, deploy/scripts/newman-out-form.sh:$data_line\$" <<<"$out" && echo 1 || echo 0)" "$out"

  echo "ось 6 — весь файл: каждая находка вне блока находится своим правилом"
  _st "проброс к службе поставщика ниже строки переписи" \
      "$(_one newman-out-forward.sh 'проброс к службе поставщика provider-a')" "$out"
  _st "он же, команда продолжена на следующую строку" \
      "$(_one newman-out-continued.sh 'проброс к службе поставщика provider-b')" "$out"
  _st "адрес суитам ключом, а не массивом блока" \
      "$(_one newman-out-key.sh 'ключ адреса поставщика providerPublicBaseUrl')" "$out"
  _st "адрес, набранный ручкой порта поставщика" \
      "$(_one newman-out-knob.sh 'ручка порта поставщика PB_PORT')" "$out"
  _st "адрес, набранный номером порта поставщика" \
      "$(_one newman-out-port.sh 'порт поставщика 14002')" "$out"

  echo "ось 7 — поставщик, не опознанный ни по одному блоку, — находка, а не пустой суд"
  mkdir -p "$WORK/noident/deploy/scripts"
  cp "$ROOT_DEFAULT/deploy/scripts/identity-provider-landing.py" "$WORK/noident/deploy/scripts/"
  sed -e 's#svc/provider-a "\$PA_PORT:1"#"svc/$PROVIDER_SVC" "$PA_PORT:1"#' -e '/svc\/provider-b/d' \
    "$d/newman-twin.sh" > "$WORK/noident/deploy/scripts/newman-noident.sh"
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$WORK/noident" 2>&1)"; rc=$?
  _st "служба поставщика не названа литералом — отказ суда вне блока" \
      "$([ "$rc" != 0 ] && grep -q 'поставщик не опознан ни по одному блоку (служб 0' <<<"$out" && echo 1 || echo 0)" "rc=$rc / $out"

  echo "ось 8 — на дереве без прогонщиков вердикт БЕСПРЕДМЕТЕН, а не зелен"
  mkdir -p "$WORK/empty/deploy/scripts"
  out="$("$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" --root "$WORK/empty" 2>&1)"; rc=$?
  _st "пустой обход — отказ" "$([ "$rc" != 0 ] && echo 1 || echo 0)" "rc=$rc / $out"

  echo
  if [ "$fails" -gt 0 ]; then
    echo "ОТКАЗ: провалено утверждений $fails из 19" >&2; return 1
  fi
  echo "ЧИСТО: 19 утверждений — проба способна упасть на прежней форме, на пробросе вне живости и на каждой из четырёх находок вне блока, смолчать на законном близнеце, объявить свою слепоту и назвать в переписи пробросы, которых не судит"
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
echo "ЧИСТО: пробросы блока следуют посадке на каждой из ${#LANDINGS[@]} посадок у каждого прогонщика; вне блока среди судимых форм (служба литералом, ключ адреса, ручка и номер порта) обращений к поставщику нет; вызовов проброса со службой из данных или целью вне известных видов $OUTSIDE_UNJUDGED — НЕ судятся (задача волны-3 #2797), координаты в переписи"
