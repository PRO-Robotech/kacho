#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# console-serves-identity-flows-test.sh — НА ЦЕПОЧКЕ ПОСАДКИ own РАЗДАЧА КОНСОЛИ
# НЕ ОТДАЁТ АДРЕСА ЦЕРЕМОНИЙ ЧУЖОМУ ЭКРАНУ, И ТАКАЯ ЦЕПОЧКА С КОНСОЛЬЮ ЕСТЬ.
# Судится РЕНДЕР, а не шаблон.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ (#2777, приёмка F8 §4)
#
# Посадка читается из РЕНДЕРА той же цепочки: посадка края — переменная его пода
# (`KACHO_API_GATEWAY_IDENTITY_PROVIDER`); посадка службы доступа ОДНА — своя
# полоса (kaname#363), и ключа посадки у её карты настроек нет (kacho#2818), так
# что её половина — `own` на каждой цепочке. На цепочке, где ОБЕ половины стоят
# на `own`, человека проверяет наша полоса, а экраны церемоний рисует консоль
# (приёмка F8, Р1): полоса к чужому экрану (`location ~ ^/(login|…)`) и полоса к
# публичному слушателю поставщика (`location ^~ /.ory/…`) там — вторая дверь в
# ту же систему, ведущая к соседу, которого посадка не поднимает. Любая из них
# на такой цепочке — находка с именем цепочки.
#
# И ЗНАМЕНАТЕЛЬ: цепочек, где консоль развёрнута И обе половины на `own`,
# обязано быть не меньше одной. Иначе предусловие прогона сценариев F8 (§4:
# цепочка, которая разворачивает консоль и не отдаёт адреса церемоний чужому
# экрану) не создано нигде, и «на own полос ноль» было бы истинно и
# бессодержательно — ровно так, как §1.7 приёмки измерил его до этой правки.
#
# ПРЕЖДЕ ЗДЕСЬ БЫЛО И ПЕРВОЕ УТВЕРЖДЕНИЕ — «браузерный адрес потока, объявленный
# настройками службы личности (`ui_url:`), обслуживается полосой раздачи». Эти
# настройки рендерил подчарт службы доступа; их шаблон снят вместе с провязкой
# поставщика (kacho#2818), и объявлений в рендере не бывает ни при каком
# профиле. Утверждение снято вместе со своим предметом; распознаватель полосы
# (`served_segments`, снятие комментариев) остался — им судится полоса на own.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ «ШАБЛОН НЕ НАШЁЛСЯ» — ЭТО ОТВЕТ, А НЕ ОТКАЗ
#
# Цепочка, не разворачивающая консоль, отвечает на `--show-only` фразой
# «could not find template … in chart». Эту фразу гейт принимает за ответ
# «консоли здесь нет» — и ТОЛЬКО её; любой другой отказ рендера уходит в код 2,
# потому что «не спросили» не есть «получили нет».
#
# У признания есть цена: ту же фразу helm скажет, если шаблон ПЕРЕИМЕНОВАЛИ, и
# тогда все цепочки разом прочитались бы как «консоли нет». Закрыта она ДВУМЯ
# механизмами, и ловят они РАЗНОЕ — перемерено 2026-09-22 на копии дерева с
# переименованным шаблоном раздачи:
#
#   • НАСТОЯЩЕЕ переименование до второго механизма НЕ ДОХОДИТ. Соседний шаблон
#     включает раздачу ради отпечатка конфигурации (`deployment-vpc.yaml`:
#     `include (print $.Template.BasePath "/configmap-nginx.yaml")`), поэтому
#     первым падает ПОЛНЫЙ рендер первой же цепочки — `render_nonempty_or_fatal`,
#     код 2, текстом helm `template: no template "…/configmap-nginx.yaml"
#     associated with template "gotpl"`. Переименование ловит ОН. Код 2 по
#     контракту библиотеки исходов означает «условие прогона, а не свойство
#     дерева»; категория задана библиотекой, общая для всех её потребителей и
#     названа здесь как есть (`PRO-Robotech/kacho#2782`);
#   • `served_renders == 0` закрывает ОСТАВШУЮСЯ половину: полный рендер прошёл,
#     а раздача не отрендерилась ни на одной цепочке — консоль снята отовсюду
#     либо путь `--show-only` разошёлся с деревом, не ломая включения. Это
#     находка о дереве, код 1, и пустой обход зелёного не даёт.
#
# Порядок несущий: пока включение живо, первым говорит полный рендер; снимут
# включение — переименование начнёт ловить вторая проверка.
#
# Проверки «файл шаблона лежит на диске» здесь НЕТ намеренно, и это сказано, а
# не умолчано: она не различила бы заявленного (переименование оставляет файл на
# диске, только под другим именем), а её отказ уходил бы кодом 2 — «условие не
# создано» о том, что на самом деле есть свойство дерева.
#
# Самопроверка: --self-test (законный вход · инъекции в обе стороны · пустой
# обход · знаменатель · чужая причина). Инъекции идут в КОПИЮ дерева — живой рабочей копии
# самопроверка не касается.
set -euo pipefail

SCRIPT="$(basename "$0")"
# Каталог снимается АБСОЛЮТНЫМ до любого `cd`: подключение библиотек по пути,
# производному от `$0`, после смены рабочего каталога молча не происходит.
HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
# Путь шаблона раздачи ВНУТРИ умбреллы — им же helm отвечает «not found».
SERVING_TPL="charts/uif/templates/configmap-nginx.yaml"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
# shellcheck source=deploy/tests/helm/stacks.sh
. "$HERE/stacks.sh"

# ─────────────────────────────────────────────────────────────────────────────
# РАЗБОР — PYTHON
# ─────────────────────────────────────────────────────────────────────────────
adjudicate_chain() {
  local chain="$1" full="$2" conf="$3" console="$4"
  CHAIN="$chain" FULL="$full" CONF="$conf" CONSOLE="$console" python3 - <<'PY'
import os, re, sys
import yaml

chain   = os.environ["CHAIN"]
console = os.environ["CONSOLE"] == "yes"
full    = open(os.environ["FULL"], encoding="utf-8").read()
conf    = open(os.environ["CONF"], encoding="utf-8").read() if console else ""

# Объявление браузерного адреса потока в рендере.

# ── ПОЛОСА — ЭТО ОБЪЯВЛЕНИЕ, А НЕ УПОМИНАНИЕ О НЁМ ──────────────────────────
#
# Прежний образец шёл поиском по подстроке: не привязан к началу строки и не
# отличал исполняемую часть от комментария. Измерено (2026-09-22): рендер, где
# посадка сняла адрес поставщика, а в шаблоне остался КОММЕНТАРИЙ, называющий
# снятую полосу, читался как обслуживаемый — ноль переадресаций, вердикт
# зелёный, и перепись ДОСЛОВНО совпадала с переписью здорового дерева. То есть
# «числа до и после сошлись» ложного зелёного не ловит вовсе.
#
# Полосой считается то, что удовлетворяет ТРЁМ признакам сразу:
#
#   1. объявление с НАЧАЛА СТРОКИ. Решётку закрывает ЭТОТ признак, и он один:
#      перед `location` образец допускает пробел и таб, больше ничего.
#      Подстановка в сам образец (2026-09-22): `    location ~ …` — совпадение;
#      `    #location ~ …`, `    # location ~ …`, `# location ~ …` — ни одного;
#   2. комментарии сняты ДО разбора — НЕ ради п.1, который в этом не
#      нуждается, а ради СЧЁТА СКОБОК: `block_body` считает `{` и `}` буквально
#      и о комментариях не знает. `}` в комментарии внутри тела обрывает тело
#      раньше переадресации, и РАБОЧАЯ полоса читается как необслуживающая;
#      незакрытая `{` уводит счётчик за `}` своей полосы и отдаёт ей
#      переадресацию СОСЕДНЕЙ — а это уже ложное зелёное.
#      Снимаются они по правилу лексера nginx, а не целыми строками
#      (kacho#2810, п. 2): `#` открывает комментарий до конца строки, когда
#      стоит ВНЕ кавычек и на ГРАНИЦЕ лексемы (начало строки, пробел, `;`, `{`,
#      `}`), — поэтому снимается и ХВОСТОВОЙ комментарий (`set $x "y"; # было:
#      if ($slow) {`), который прежнее снятие целых строк оставляло, и его
#      скобка давала то же ложное зелёное. `#` в кавычках и внутри лексемы —
#      знак; скобка в кавычках — знак строки, а не граница блока; незакрытая
#      кавычка — отказ: разбор не угадывает, где она кончается.
#      СНЯТИЕ КОММЕНТАРИЕВ НЕ УБИРАТЬ НИ В КАКОЙ ФОРМЕ: прежняя редакция
#      обосновывала его обходом п.1, обоснование было ЛОЖНЫМ, и снявший пункт
#      по нему внесёт ломкость на комментарии со скобкой. Прежде случаи пункта
#      мерила Go-проба объявлений адресов потока поставщика; она снята вместе с
#      ними (#1276), и проход держит своя ось самопроверки ниже («хвостовой `}`
#      в комментарии рабочей полосы»): обезвреженный проход без неё не
#      покраснил бы ничего;
#   3. в теле блока есть ПЕРЕАДРЕСАЦИЯ.
#
# Второй копии признака в дереве нет: Go-проба, исполнявшая его по ОБЪЯВЛЕНИЮ,
# снята вместе с объявлениями адресов потока поставщика (#1276).
BAND = re.compile(r"(?m)^[ \t]*location\s+~\s+\^/\(([a-z0-9|_-]+)\)\(/\|\$\)\s*\{")
# strip_comments — текст раздачи без комментариев и со скобками в кавычках,
# погашенными до `_` (п. 2 выше); None — кавычка не закрыта до конца текста.
def strip_comments(conf):
    out, quote, boundary, i, n = [], None, True, 0, len(conf)
    while i < n:
        c = conf[i]
        if quote:
            if c == "\\":
                out.append(c)
                if i + 1 < n:
                    i += 1
                    out.append("_" if conf[i] in "{}" else conf[i])
            elif c == quote:
                quote, boundary = None, False
                out.append(c)
            else:
                out.append("_" if c in "{}" else c)
            i += 1
            continue
        if c == "#" and boundary:
            j = conf.find("\n", i)
            if j < 0:
                break
            i = j  # перевод строки комментарий не забирает
            continue
        if c in "\"'" and boundary:
            quote = c
            out.append(c)
            i += 1
            continue
        boundary = c in " \t\n\r;{}"
        out.append(c)
        i += 1
    return None if quote else "".join(out)
# Переадресация в теле блока. Без неё блок не обслуживает ничего.
PROXY = re.compile(r"(?m)^[ \t]*proxy_pass\s")
# Полоса к публичному слушателю поставщика — префиксная, `^~ /.ory/…`. Те же три
# признака, что у полосы потоков: с начала строки, после снятия комментариев, с
# переадресацией в теле.
ORY_BAND = re.compile(r"(?m)^[ \t]*location\s+\^~\s+(/\.ory/[^\s{]*)\s*\{")


def block_body(s, open_idx):
    """Тело блока, открытого `{` по индексу; None — блок не закрыт."""
    depth = 0
    for i in range(open_idx, len(s)):
        if s[i] == "{":
            depth += 1
        elif s[i] == "}":
            depth -= 1
            if depth == 0:
                return s[open_idx + 1:i]
    return None


def served_segments(conf):
    """(множество сегментов, число полос без переадресации, причина нечитаемости)."""
    clean = strip_comments(conf)
    if clean is None:
        return set(), 0, ("кавычка в раздаче не закрыта до конца текста — где кончается "
                          "строка, разбор не угадывает, и скобки после неё считать нельзя")
    segs, without_proxy = set(), 0
    for m in BAND.finditer(clean):
        body = block_body(clean, clean.rindex("{", 0, m.end()))
        if body is None or not PROXY.search(body):
            without_proxy += 1
            continue
        segs |= {x.strip() for x in m.group(1).split("|") if x.strip()}
    return segs, without_proxy, None

def ory_lanes(conf):
    """Префиксы полос `/.ory/…`, которые ПЕРЕАДРЕСУЮТ (после снятия комментариев)."""
    clean = strip_comments(conf)
    if clean is None:
        return []
    out = []
    for m in ORY_BAND.finditer(clean):
        body = block_body(clean, clean.rindex("{", 0, m.end()))
        if body is not None and PROXY.search(body):
            out.append(m.group(1))
    return out


def posture_of(text):
    """(посадка службы доступа, посадка края) по рендеру; "" — не прочитана.

    Половина службы не читается: посадка у неё одна — `own` (kaname#363), и ключа
    посадки у её карты настроек нет (kacho#2818). Половина края — переменная его
    пода."""
    iam, edge = "own", ""
    try:
        docs = [d for d in yaml.safe_load_all(text) if isinstance(d, dict)]
    except yaml.YAMLError:
        return iam, ""
    for d in docs:
        if d.get("kind") == "Deployment":
            pod = (((d.get("spec") or {}).get("template") or {}).get("spec") or {})
            for c in pod.get("containers") or []:
                for e in c.get("env") or []:
                    if e.get("name") == "KACHO_API_GATEWAY_IDENTITY_PROVIDER":
                        edge = str(e.get("value") or "").strip()
    return iam, edge


# Полос может оказаться несколько — множество обслуживаемых сегментов есть
# ОБЪЕДИНЕНИЕ всех, а не первая попавшаяся: чтение одной объявило бы
# необслуживаемым адрес, который обслуживает соседняя.
segs, bands_without_proxy, unreadable = (served_segments(conf) if console else (set(), 0, None))

iam_posture, edge_posture = posture_of(full)
ory = ory_lanes(conf) if console else []

print("BAND %s" % (" ".join(sorted(segs)) if segs else "-"))
print("POSTURE %s/%s" % (iam_posture or "?", edge_posture or "?"))
print("ORY %s" % (" ".join(sorted(ory)) if ory else "-"))

if not console:
    sys.exit(0)

if unreadable:
    print("VIOLATION цепочка %s: раздача консоли не читается — %s." % (chain, unreadable))
    sys.exit(0)

# ── ПОСАДКА own: ни полосы к чужому экрану, ни полосы к слушателю поставщика ──
if iam_posture == "own" and edge_posture == "own":
    print("OWNCONSOLE")
    if segs:
        print("VIOLATION цепочка %s: обе половины на посадке own, а раздача консоли "
              "отдаёт адреса церемоний [%s] чужому экрану — полоса обслуживания ведёт "
              "к соседу, которого посадка не поднимает. Под own церемонию рисует консоль "
              "(приёмка F8, Р1), а внешний экран входа снят (#1276): снимите полосу из "
              "шаблона раздачи консоли (ui-future/deploy/templates/configmap-nginx.yaml)."
              % (chain, " ".join(sorted(segs))))
    if ory:
        print("VIOLATION цепочка %s: обе половины на посадке own, а раздача консоли "
              "проксирует [%s] к публичному слушателю поставщика — дорога к службе, "
              "которой посадка не поднимает и которой в дереве нет (#1276). Снимите "
              "полосу из шаблона раздачи консоли." % (chain, " ".join(sorted(ory))))
PY
}

# ─────────────────────────────────────────────────────────────────────────────
# ОБХОД
# ─────────────────────────────────────────────────────────────────────────────
run_checks() {
  local names chain args full conf out line
  local chains=0 console_chains=0 band_chains=0 served_renders=0
  local own_console_chains=0

  names="$(stacks_names)" || return $?
  [ -n "$names" ] || fail "состав стендов пуст — обходить нечего"

  local work; work="$(mktemp -d)"
  # shellcheck disable=SC2064
  trap "rm -rf '$work'" RETURN

  for chain in $names; do
    chains=$((chains + 1))
    args="$(stacks_args "$chain" "$UMBRELLA")" || return $?

    # ПЕРВЫЙ рендер цепочки — положительный контроль: чарт с этой цепочкой
    # вообще рендерится. Без него всякое последующее утверждение о ней прошло бы
    # вакуумно.
    # shellcheck disable=SC2086
    helm_try kacho "$UMBRELLA" $args --namespace kacho
    render_nonempty_or_fatal "цепочка $chain (полный рендер)"
    full="$work/$chain.full.yaml"
    printf '%s\n' "$HELM_OUT" >"$full"

    # Раздача консоли этой цепочки. «Шаблон не найден» — ОТВЕТ «консоли здесь
    # нет»; любой другой отказ — условие прогона, а не свойство дерева.
    conf="$work/$chain.serving.yaml"
    local console=no
    # shellcheck disable=SC2086
    helm_try kacho "$UMBRELLA" $args --namespace kacho --show-only "$SERVING_TPL"
    if [ "$HELM_RC" -eq 0 ]; then
      console=yes
      console_chains=$((console_chains + 1))
      served_renders=$((served_renders + 1))
      printf '%s\n' "$HELM_OUT" >"$conf"
    else
      case "$HELM_ERR" in
        *"could not find template"*) : ;;
        *) helm_said "цепочка $chain → $SERVING_TPL"
           fatal "рендер раздачи консоли на цепочке $chain отказал НЕ фразой «шаблон не найден» — \
это условие прогона, а не ответ «консоли здесь нет»" ;;
      esac
      : >"$conf"
    fi

    out="$(adjudicate_chain "$chain" "$full" "$conf" "$console")"
    local b="-" pst="?/?" ory="-"
    while IFS= read -r line; do
      case "$line" in
        BAND\ -)      b="-" ;;
        BAND\ *)      b="${line#BAND }"; band_chains=$((band_chains + 1)) ;;
        POSTURE\ *)   pst="${line#POSTURE }" ;;
        ORY\ *)       ory="${line#ORY }" ;;
        OWNCONSOLE)   own_console_chains=$((own_console_chains + 1)) ;;
        VIOLATION\ *) violation "${line#VIOLATION }" ;;
      esac
    done <<<"$out"

    if [ "$console" = yes ]; then
      echo "  цепочка $chain: посадка $pst; консоль развёрнута; полоса обслуживания [$b]; полосы /.ory/ [$ory]"
    else
      echo "  цепочка $chain: посадка $pst; консоль НЕ разворачивается"
    fi
    ok
  done

  # ── ПУСТОЙ ОБХОД ЗЕЛЁНОГО НЕ ДАЁТ ────────────────────────────────────────
  # Ни одна цепочка не отрендерила раздачу — значит либо консоль не
  # разворачивается нигде, либо шаблон переехал и фраза «не найден» прочиталась
  # как ответ на всех цепочках сразу. Оба случая означают, что гейт не осмотрел
  # НИЧЕГО, и зелёное здесь было бы свойством обхода, а не дерева.
  if [ "$served_renders" -eq 0 ]; then
    fail "раздача консоли не отрендерилась НИ НА ОДНОЙ из $chains цепочек ($SERVING_TPL). \
Либо консоль снята отовсюду, либо шаблон переименован — и тогда фраза «шаблон не найден» \
прочиталась как «консоли здесь нет» на каждой цепочке, а гейт не осмотрел ничего"
  fi

  # ── ЗНАМЕНАТЕЛЬ ПОСАДКИ own (приёмка F8 §4) ─────────────────────────────
  # Раздача отрендерилась, но ни на одной цепочке с консолью обе половины не
  # стоят на own: предусловие прогона сценариев F8 не создано нигде, и ноль
  # полос к чужому экрану на own был бы нулём без предмета.
  if [ "$served_renders" -gt 0 ] && [ "$own_console_chains" -eq 0 ]; then
    violation "ни на одной из $console_chains цепочек, где консоль развёрнута, обе половины \
не стоят на посадке own — цепочки, которая разворачивает консоль и отдаёт адреса церемоний \
оболочке, нет ни одной (предусловие прогона приёмки F8, §4)"
  fi

  findings_verdict "цепочек $chains (с консолью $console_chains, из них с полосой обслуживания \
$band_chains, на посадке own обеими половинами $own_console_chains)"
}

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА
# ─────────────────────────────────────────────────────────────────────────────
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: три исхода различимы; инъекции в обе стороны ==="
  st_rc=0; st_checked=0

  # ── КОПИЯ ДЕРЕВА, А НЕ ПРАВКА ЖИВЫХ ФАЙЛОВ (#696) ─────────────────────────
  # Правка отслеживаемых файлов с возвратом по ловушке не закрывает снятие,
  # которое не перехватывается (SIGKILL, нехватка памяти или места): тогда в
  # дереве остаётся ровно тот дефект, который гейт и ловит.
  #
  # Раскладка копии повторяет дерево ДВУМЯ каталогами, и это несущее: подчарт
  # консоли приезжает в умбреллу по `file://../../../ui-future/deploy`, то есть
  # копия обязана иметь и `deploy/`, и `ui-future/deploy/` — иначе инъекция в
  # шаблон раздачи не доехала бы до рендера, а доказательство стало бы вакуумным.
  WORK="$(mktemp -d)"
  trap 'rm -rf "$WORK"' EXIT
  mkdir -p "$WORK/deploy/tests/helm" "$WORK/ui-future"
  cp -r "$DEPLOY_ROOT/helm" "$WORK/deploy/helm" || fatal "копия чартов не собрана — инъекциям некуда идти"
  [ -d "$WORK/deploy/helm/umbrella" ] || fatal "в копии нет умбреллы ($WORK/deploy/helm/umbrella)"
  cp "$DEPLOY_ROOT/stacks.txt" "$WORK/deploy/stacks.txt"
  cp -r "$DEPLOY_ROOT/../ui-future/deploy" "$WORK/ui-future/deploy"
  cp -r "$WORK/ui-future/deploy" "$WORK/ui-future-pristine"
  cp "$0" "$WORK/deploy/tests/helm/$SCRIPT"
  # Общие реализации едут вместе с испытуемым: он подключает их по своему
  # каталогу, и без них самопроверка мерила бы отсутствие файла.
  cp "$HERE/outcome.sh" "$HERE/stacks.sh" "$WORK/deploy/tests/helm/"
  SUT="$WORK/deploy/tests/helm/$SCRIPT"
  COPY_SRC="$WORK/ui-future/deploy"
  COPY_CHARTS="$WORK/deploy/helm/umbrella/charts"

  # repack — пересобрать архив подчарта консоли из ИСХОДНИКА КОПИИ. Без этого
  # рендер показывал бы прошлое состояние копии, и всякая инъекция в шаблон
  # проходила бы мимо: ось выглядела бы исполненной, ничего не проверив.
  repack() {
    rm -f "$COPY_CHARTS"/uif-*.tgz
    helm package "$COPY_SRC" -d "$COPY_CHARTS" >/dev/null \
      || fatal "архив подчарта консоли не пересобран — инъекция не доехала бы до рендера"
  }
  # restore_src — вернуть исходник копии к нетронутому виду: инъекции одно-фактны,
  # и следующая обязана отличаться от близнеца РОВНО одним фактом.
  restore_src() {
    rm -rf "$COPY_SRC"
    cp -r "$WORK/ui-future-pristine" "$COPY_SRC"
    repack
  }
  repack

  st_probe() {
    local label="$1" want_rc="$2" want_txt="$3" out rc bytes
    st_checked=$((st_checked + 1))
    out="$(bash "$SUT" 2>&1)" && rc=0 || rc=$?
    bytes=${#out}
    if [ "$rc" -ne "$want_rc" ]; then
      echo "  ✗ $label — код $rc, ожидался $want_rc"; printf '%s\n' "$out" | sed 's/^/      /'; st_rc=1; return
    fi
    if [ "$rc" -ne 0 ] && [ "$bytes" -eq 0 ]; then
      echo "  ✗ $label — ненулевой код при НУЛЕ БАЙТ вывода"; st_rc=1; return
    fi
    case "$out" in
      *"$want_txt"*) echo "  ✓ $label — код $rc, вывод $bytes б., назван '$want_txt'" ;;
      *) echo "  ✗ $label — код $rc верен, но в выводе нет '$want_txt':"
         printf '%s\n' "$out" | sed 's/^/      /'; st_rc=1 ;;
    esac
  }

  echo
  echo "-- чужая причина: зависимости умбреллы не материализованы --"
  mv "$COPY_CHARTS" "$WORK/charts.hidden"
  st_probe "зависимостей нет → «условие не создано» (код 2, не красное)" 2 "зависимости умбреллы не материализованы"
  mv "$WORK/charts.hidden" "$COPY_CHARTS"

  echo
  echo "-- законный вход: копия дерева как есть --"
  st_probe "дерево как есть → зелёное" 0 "PASS:"

  # ── ПОЛОСА ВНОСИТСЯ В ШАБЛОН КОПИИ, ВКЛЮЧАЕТСЯ СЛОЕМ ПОВЕРХ КОРНЯ dev-ЦЕПОЧЕК ─
  #
  # На дереве полосы к чужому экрану нет ни в шаблоне раздачи, ни на одной
  # цепочке (#1276): прежняя ручка её адреса снята вместе с полосой. Поэтому
  # инъекция вносит полосу в шаблон КОПИИ — под условием значения, которого в
  # дереве нет, — и включает её слоем поверх корня dev-цепочек копии; цепочки
  # боевого корня (prod, own, fe3455) слой не получают и остаются без полосы.
  # Адресат полосы — подставной экран: имена в ней пробные, а не имена
  # какого-либо соседа.
  band_template() { # [хвост строки тела] — полоса под условием в шаблоне копии
    python3 - "$COPY_SRC/templates/configmap-nginx.yaml" "${1:-}" <<'INJB'
import io, sys
p, tail = sys.argv[1], sys.argv[2]
s = io.open(p, encoding="utf-8").read()
# Якорь — объявление в шаблоне о том, что полосы здесь нет: оно стоит в
# серверном блоке оболочки ровно один раз (блоков `location = /healthz` в
# шаблоне несколько — по одному на модуль).
anchor = "        # ЭКРАНЫ ЦЕРЕМОНИЙ ВХОДА РИСУЕТ КОНСОЛЬ"
assert s.count(anchor) == 1, "инъекция не внесена: якорь полосы не найден (совпадений %d)" % s.count(anchor)
band = ("        {{- if .Values.host.probeScreenBand }}\n"
        "        location ~ ^/(login|registration)(/|$) {\n"
        "            set $probe_screen \"${KACHO_UI_PROBE_SCREEN_UPSTREAM}\";" + tail + "\n"
        "            proxy_pass http://$probe_screen;\n"
        "        }\n"
        "        {{- end }}\n\n")
io.open(p, "w", encoding="utf-8").write(s.replace(anchor, band + anchor, 1))
INJB
    repack
  }
  world_external() { # <имя слоя> <yaml> — слой поверх корня dev-цепочек копии
    printf '%s\n' "$2" >"$WORK/deploy/helm/umbrella/$1"
    python3 - "$DEPLOY_ROOT/stacks.txt" "$WORK/deploy/stacks.txt" "$1" <<'WORLD'
import io, re, sys
src, dst, layer = sys.argv[1:4]
out, n = [], 0
for line in io.open(src, encoding="utf-8").read().splitlines():
    if re.match(r"^[a-z0-9][a-z0-9-]*:values\.dev\.yaml(,|$)", line):
        line += "," + layer
        n += 1
    out.append(line)
assert n > 0, "ни одной цепочки на корне values.dev.yaml — внешнему миру не на что лечь"
io.open(dst, "w", encoding="utf-8").write("\n".join(out) + "\n")
WORLD
  }
  world_reset() {
    rm -f "$WORK/deploy/helm/umbrella"/values.zz-self-test-*.yaml
    cp "$DEPLOY_ROOT/stacks.txt" "$WORK/deploy/stacks.txt"
  }
  # Посадка края — единственная половина, которую профиль ещё объявляет.
  POSTURE_EXTERNAL='api-gateway:
  authn:
    identityProvider: external'
  BAND_ON='uif:
  host:
    probeScreenBand: true'

  echo
  echo "-- инъекция B (ОДИН факт против дерева): полоса к чужому экрану на посадке own --"
  band_template
  world_external values.zz-self-test-band.yaml "$BAND_ON"
  st_probe "полоса на own → находка о дереве" 1 "обе половины на посадке own"
  world_reset

  echo
  echo "-- законный близнец B (ОДИН факт): та же полоса, край на посадке external --"
  world_external values.zz-self-test-band-ext.yaml "$POSTURE_EXTERNAL
$BAND_ON"
  st_probe "полоса, край не на own → зелёное" 0 "PASS:"
  world_reset
  restore_src

  echo
  echo "-- инъекция C (ОДИН факт против инъекции B): хвостовой комментарий со \`}\` в теле полосы на own --"
  # Ось держит КОПИЮ лексического прохода на Python (kacho#2781, kacho#2810 п. 2).
  # Снятие целых строк оставляло хвостовой комментарий, его `}` обрывала тело до
  # переадресации, и РАБОЧАЯ полоса читалась необслуживающей — то есть полоса к
  # чужому экрану на own прошла бы мимо. Обезвредьте `strip_comments` — ось
  # покраснеет: находки не будет; невредимая — находка есть.
  band_template "  # прежняя ветка кончалась здесь: }"
  world_external values.zz-self-test-band.yaml "$BAND_ON"
  st_probe "хвостовой комментарий со скобкой в полосе на own → находка, а не зелёное" 1 "обе половины на посадке own"
  restore_src
  world_reset

  echo
  echo "-- пустой обход: состав стендов не называет ни одной цепочки с консолью --"
  # Переименование шаблона для этой оси НЕ годится и это названо, а не умолчано:
  # соседние шаблоны включают его ради отпечатка конфигурации, поэтому рендер
  # отказывает целиком, и исход — «условие прогона», а не пустой обход.
  #
  # Здесь снимается ровно тот факт, который делает обход пустым: цепочки, где
  # консоль разворачивается, из состава стендов убраны. Гейту нечего осматривать,
  # и зелёное стало бы свойством обхода, а не дерева.
  grep -E '^prod:' "$DEPLOY_ROOT/stacks.txt" >"$WORK/deploy/stacks.txt"
  st_probe "консоли нет ни на одной цепочке состава → находка, а не зелёное" 1 "НИ НА ОДНОЙ"
  world_reset

  echo
  echo "-- знаменатель own (ОДИН факт): консоль есть, но ни одна цепочка с ней не на own --"
  # Приёмка F8 §4: без цепочки, которая разворачивает консоль И стоит на own,
  # ни одному сценарию церемонии негде исполниться. Состав сужен до prod (консоли
  # нет) и dev (консоль есть); у dev сменена ровно посадка.
  printf '%s\n' "$POSTURE_EXTERNAL" >"$WORK/deploy/helm/umbrella/values.zz-self-test-posture.yaml"
  { grep -E '^prod:' "$DEPLOY_ROOT/stacks.txt"
    grep -E '^dev:' "$DEPLOY_ROOT/stacks.txt" | sed 's/$/,values.zz-self-test-posture.yaml/'
  } >"$WORK/deploy/stacks.txt"
  st_probe "консоль только на external → находка, а не зелёное" 1 "предусловие прогона приёмки F8"

  echo
  echo "-- законный близнец знаменателя (ОДИН факт): тот же состав, dev на own --"
  { grep -E '^prod:' "$DEPLOY_ROOT/stacks.txt"
    grep -E '^dev:' "$DEPLOY_ROOT/stacks.txt"
  } >"$WORK/deploy/stacks.txt"
  st_probe "консоль на own → зелёное" 0 "PASS:"
  world_reset

  echo
  if [ "$st_rc" -eq 0 ]; then
    echo "$SCRIPT --self-test: все $st_checked оси исполнены и сошлись"
  else
    echo "$SCRIPT --self-test: ОСИ НЕ СОШЛИСЬ (проверено $st_checked)"
  fi
  exit $st_rc
fi

# ── Предпосылки. Каждая — «условие не создано», а не находка о дереве ────────
require_helm
require_python3
require_umbrella_charts "$UMBRELLA"
# Архив подчарта консоли обязан быть НЕ СТАРШЕ своего исходника: рендер по
# вчерашнему архиву вынес бы вердикт о дереве недельной давности — и ошибся бы
# в обе стороны сразу.
# Подчарт консоли — предмет этого гейта; без его архива рендер показал бы стек
# БЕЗ раздачи, и все утверждения прошли бы вакуумно.
require_dep_chart "$UMBRELLA" uif
require_fresh_dep_charts "$UMBRELLA"

run_checks
