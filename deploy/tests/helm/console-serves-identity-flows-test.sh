#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# console-serves-identity-flows-test.sh — НА КАЖДОЙ ЦЕПОЧКЕ ЛИБО ЕСТЬ ОБСЛУЖИВАНИЕ
# ЦЕРЕМОНИИ ВХОДА, ЛИБО НЕТ ЕЁ ОБЪЯВЛЕНИЯ. Судится РЕНДЕР, а не шаблон.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Об одном предмете говорят два места: служба личности объявляет браузерные
# адреса потоков (`ui_url:`), раздача консоли объявляет, какие пути обслуживает
# (полоса `location ~ ^/(…)(/|$)`). Согласие половин уже проверяет
# `deploy/identity_flow_path_is_served_test.go` — но он читает ТЕКСТ шаблона, то
# есть говорит об одном глобальном факте: «полоса в файле есть». Пока полоса
# стояла безусловно, этого хватало.
#
# Полоса стала УСЛОВНОЙ: она есть там, где посадка объявила адрес интерфейса
# внешнего поставщика личности (`host.upstreams.kratosUi`), и её нет там, где не
# объявила. С этой секунды текстовый гейт СЛЕП к расхождению по цепочкам: он
# зелен и тогда, когда на конкретной посадке условие ложно, а объявления адресов
# стоят. Наблюдаемая цена — та же, что была измерена на боевом стенде: путь, не
# покрытый ни одной полосой, достаётся общему `location /`, а тот отвечает `200`
# с оболочкой консоли. Отказ выглядит УСПЕХОМ — ни `404`, ни строки в журнале,
# только «вход не открывается».
#
# Класс шире этого случая и стоил нам вечера: ОБЪЯВЛЕНИЕ ЖИВО, ИСПОЛНЕНИЯ НЕТ.
# Поймать его чтением шаблона нельзя by construction — нужен рендер цепочки,
# потому что только он показывает, во что объявление превращается НА ЭТОЙ
# ПОСАДКЕ. Соседний замер того же вечера: величины одной полосы оказались мертвы
# потому, что стояли под подчартом, снятым другой, — чтение файла показывало их
# живыми, рендер давал по ним ноль вхождений.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — И ЧЕГО ГЕЙТ НЕ УТВЕРЖДАЕТ
#
# По КАЖДОЙ цепочке `deploy/stacks.txt`:
#
#   • цепочка, где консоль НЕ разворачивается, судится отдельной строкой переписи
#     и находкой не является: обслуживать эти адреса на ней обязан не она. Но
#     число объявлений печатается и там — иначе «ноль находок» стало бы
#     неотличимо от «ноль прочитанного»;
#   • цепочка, где консоль разворачивается: либо рендер её раздачи несёт полосу
#     потоков, покрывающую ПЕРВЫЙ СЕГМЕНТ каждого объявленного адреса, либо
#     объявлений нет вовсе. Третьего исхода нет, и «объявлено, но не
#     обслуживается» — находка с именем цепочки, адресом и сегментом.
#
# Гейт НЕ утверждает, что церемония входа вообще должна быть: посадка вправе её
# не иметь. Он утверждает, что ОБЪЯВЛЕНИЕ и ИСПОЛНЕНИЕ снимаются и заводятся
# ВМЕСТЕ. Шесть-десять объявленных адресов без обслуживания — хуже отсутствия
# объявления, потому что каждый из них выглядит работающим.
#
# ── ПОСАДКА `own`: АДРЕСА ЦЕРЕМОНИЙ ОТДАЁТ ОБОЛОЧКА (#2777, приёмка F8 §4) ──
#
# Третье утверждение — о ПОСАДКЕ, и оно читается из РЕНДЕРА той же цепочки:
# посадку службы доступа объявляет её карта настроек (`kaname-config`,
# `authn.identity-provider`), посадку края — переменная его пода
# (`KACHO_API_GATEWAY_IDENTITY_PROVIDER`). На цепочке, где ОБЕ половины стоят на
# `own`, человека проверяет наша полоса, а экраны церемоний рисует консоль
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
# Перечень обслуживаемых сегментов здесь НЕ выписан: он выводится из самой
# полосы рендера. Расширение раздачи гейта не касается, сужение немедленно
# делает его строже.
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
# обход · чужая причина). Инъекции идут в КОПИЮ дерева — живой рабочей копии
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
# РАЗБОР — PYTHON, И ОН ОТВЕРГАЕТ НЕПОНЯТОЕ
#
# Формы объявления адреса здесь ровно две, и обе есть в дереве: абсолютный адрес
# со схемой и властью (`https://host/login`) и адрес, относительный корню
# (`/login`). Всё прочее — находка, а не пропуск: разбор, молча пропускающий
# непонятую форму, имел бы дыру ровно там, где живёт дефект.
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
UI_URL = re.compile(r"(?m)^\s*ui_url:\s*(.+?)\s*$")

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
#      по нему внесёт ломкость на комментарии со скобкой. Какие случаи держит
#      пункт, ИЗМЕРЯЕТ `TestFlowBandIsADeclarationNotAMentionOfOne` в
#      `deploy/identity_flow_path_is_served_injection_test.go`: он прогоняет
#      каждый случай ещё и с обезвреженным проходом. ЭТУ копию прохода держит
#      своя ось самопроверки ниже («хвостовой `}` в комментарии рабочей
#      полосы»): Go-проба копию на Python не судит, и обезвреженный проход
#      здесь без своей оси не покраснил бы ничего;
#   3. в теле блока есть ПЕРЕАДРЕСАЦИЯ.
#
# Тот же признак и теми же тремя пунктами исполняет
# `deploy/identity_flow_path_is_served_test.go` (`flowSegmentsFrom`): он судит
# ОБЪЯВЛЕНИЕ, этот — РЕНДЕР, и расхождение признаков было бы той же дырой в
# другом месте.
BAND = re.compile(r"(?m)^[ \t]*location\s+~\s+\^/\(([a-z0-9|_-]+)\)\(/\|\$\)\s*\{")
# strip_comments — текст раздачи без комментариев и со скобками в кавычках,
# погашенными до `_` (п. 2 выше); None — кавычка не закрыта до конца текста.
# Правило то же, что у `stripNginxComments` в
# deploy/identity_flow_path_is_served_test.go: расхождение копий было бы той же
# дырой в другом месте.
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
    """(посадка службы доступа, посадка края) по рендеру; "" — не прочитана."""
    iam = edge = ""
    try:
        docs = [d for d in yaml.safe_load_all(text) if isinstance(d, dict)]
    except yaml.YAMLError:
        return "", ""
    for d in docs:
        meta = d.get("metadata") or {}
        if d.get("kind") == "ConfigMap" and meta.get("name") == "kaname-config":
            try:
                cfg = yaml.safe_load((d.get("data") or {}).get("config.yaml") or "") or {}
            except yaml.YAMLError:
                cfg = {}
            iam = str(((cfg.get("authn") or {}) if isinstance(cfg, dict) else {}).get("identity-provider") or "").strip()
        if d.get("kind") == "Deployment":
            pod = (((d.get("spec") or {}).get("template") or {}).get("spec") or {})
            for c in pod.get("containers") or []:
                for e in c.get("env") or []:
                    if e.get("name") == "KACHO_API_GATEWAY_IDENTITY_PROVIDER":
                        edge = str(e.get("value") or "").strip()
    return iam, edge


def first_segment(raw):
    """(сегмент, None) либо (None, причина-отказа-разбора)."""
    v = raw.strip().strip('"').strip("'")
    if not v:
        return None, "объявление пусто"
    for scheme in ("https://", "http://"):
        if v.startswith(scheme):
            rest = v[len(scheme):]
            i = rest.find("/")
            if i < 0:
                return None, "абсолютный адрес без пути — первого сегмента не существует"
            v = rest[i:]
            break
    if not v.startswith("/"):
        return None, "форма адреса не разбирается (ни схема+власть, ни путь от корня)"
    return v.lstrip("/").split("/", 1)[0], None

decls = [m.group(1) for m in UI_URL.finditer(full)]
# Полос может оказаться несколько — множество обслуживаемых сегментов есть
# ОБЪЕДИНЕНИЕ всех, а не первая попавшаяся: чтение одной объявило бы
# необслуживаемым адрес, который обслуживает соседняя.
segs, bands_without_proxy, unreadable = (served_segments(conf) if console else (set(), 0, None))

iam_posture, edge_posture = posture_of(full)
ory = ory_lanes(conf) if console else []

print("DECLS %d" % len(decls))
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
              "(приёмка F8, Р1): опустошите host.upstreams.kratosUi на этой цепочке."
              % (chain, " ".join(sorted(segs))))
    if ory:
        print("VIOLATION цепочка %s: обе половины на посадке own, а раздача консоли "
              "проксирует [%s] к публичному слушателю поставщика — дорога к службе, "
              "которой посадка не поднимает. Опустошите host.upstreams.kratosPublic на "
              "этой цепочке." % (chain, " ".join(sorted(ory))))

if decls and not segs:
    why = ("полосы обслуживания в РЕНДЕРЕ её раздачи нет ни одной"
           if bands_without_proxy == 0 else
           "полос в РЕНДЕРЕ её раздачи объявлено %d, и НИ ОДНА не переадресует "
           "(`proxy_pass` в теле блока)" % bands_without_proxy)
    print("VIOLATION цепочка %s: консоль разворачивается, объявлено браузерных адресов "
          "потока %d, а %s. "
          "Объявления ведут в общий `location /`, и он отвечает 200 оболочкой консоли — "
          "отказ выглядит успехом. Исходов два: обслуживать (посадка объявляет "
          "host.upstreams.kratosUi) либо снять объявления вместе с церемонией."
          % (chain, len(decls), why))
    sys.exit(0)

for raw in decls:
    seg, why = first_segment(raw)
    if why:
        print("VIOLATION цепочка %s: объявление %r не разбирается — %s. Непонятая форма "
              "отвергается, а не пропускается: пропуск превратил бы «ноль находок» в "
              "«ноль прочитанного»." % (chain, raw.strip(), why))
        continue
    if seg not in segs:
        print("VIOLATION цепочка %s: объявлен %r → первый сегмент пути %r, а полоса "
              "обслуживания рендера называет [%s] — этот адрес раздача консоли НЕ "
              "обслуживает." % (chain, raw.strip(), seg, " ".join(sorted(segs))))
PY
}

# ─────────────────────────────────────────────────────────────────────────────
# ОБХОД
# ─────────────────────────────────────────────────────────────────────────────
run_checks() {
  local names chain args full conf out line
  local chains=0 console_chains=0 band_chains=0 decls_total=0 served_renders=0
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
    local d="-" b="-" pst="?/?" ory="-"
    while IFS= read -r line; do
      case "$line" in
        DECLS\ *)     d="${line#DECLS }"; decls_total=$((decls_total + d)) ;;
        BAND\ -)      b="-" ;;
        BAND\ *)      b="${line#BAND }"; band_chains=$((band_chains + 1)) ;;
        POSTURE\ *)   pst="${line#POSTURE }" ;;
        ORY\ *)       ory="${line#ORY }" ;;
        OWNCONSOLE)   own_console_chains=$((own_console_chains + 1)) ;;
        VIOLATION\ *) violation "${line#VIOLATION }" ;;
      esac
    done <<<"$out"

    if [ "$console" = yes ]; then
      echo "  цепочка $chain: посадка $pst; консоль развёрнута; объявлений $d; полоса обслуживания [$b]; полосы /.ory/ [$ory]"
    else
      echo "  цепочка $chain: посадка $pst; консоль НЕ разворачивается (обслуживать эти адреса обязана не она); объявлений $d"
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
$band_chains, на посадке own обеими половинами $own_console_chains); объявлений браузерного \
адреса всего $decls_total"
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

  # ── ВНЕШНИЙ МИР — ВТОРОЙ ЗАКОННЫЙ ВХОД, А НЕ СОСТОЯНИЕ ДЕРЕВА ─────────────
  #
  # На дереве посадку own объявляют все цепочки, объявлений браузерного адреса
  # поставщика нет ни одного и полосы к чужому экрану не рендерится ни на одной
  # (#2735, #2777). Оси, которые судят ФОРМУ полосы (переадресация, комментарий,
  # скобка), на таком дереве не на чем исполнить. Поэтому им строится законный
  # «внешний мир»: слой поверх корня dev-цепочек, где обе половины на посадке
  # external, наша карта настроек поставщика рендерится (объявления есть) и
  # раздача объявляет интерфейс поставщика (полоса есть). Цепочки боевого корня
  # (prod, own, fe3455) слой не получают и остаются на own без полос — поэтому
  # знаменатель own с консолью в этом мире не пуст, и мир зелёный.
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
  POSTURE_EXTERNAL='kaname:
  config:
    authn:
      identityProvider: external
api-gateway:
  authn:
    identityProvider: external'
  DECLS_ON='kaname:
  kratos:
    config:
      enabled: true'
  BAND_ON='uif:
  host:
    upstreams:
      kratosUi: kratos-selfservice-ui.kacho.svc.cluster.local:3000'
  # Внешний мир целиком — три факта одним слоем (слияние узлов `kaname` —
  # руками: два одноимённых ключа верхнего уровня в одном файле helm отвергает).
  WORLD='kaname:
  config:
    authn:
      identityProvider: external
  kratos:
    config:
      enabled: true
api-gateway:
  authn:
    identityProvider: external
uif:
  host:
    upstreams:
      kratosUi: kratos-selfservice-ui.kacho.svc.cluster.local:3000'

  echo
  echo "-- законный вход второго рода: внешний мир (посадка external, объявления и полоса есть) --"
  world_external values.zz-self-test-world.yaml "$WORLD"
  st_probe "внешний мир → зелёное" 0 "PASS:"
  world_reset

  echo
  echo "-- инъекция A (ОДИН факт против дерева): объявления вернулись, полосы нет --"
  # Ровно тот класс, против которого гейт заведён: служба личности объявляет
  # браузерные адреса потоков, а раздача их не обслуживает.
  world_external values.zz-self-test-decls.yaml "$DECLS_ON"
  st_probe "объявления без полосы → находка о дереве" 1 "полосы обслуживания в РЕНДЕРЕ"
  world_reset

  echo
  echo "-- инъекция B (ОДИН факт против дерева): полоса к чужому экрану на посадке own --"
  world_external values.zz-self-test-band.yaml "$BAND_ON"
  st_probe "полоса на own → находка о дереве" 1 "обе половины на посадке own"
  world_reset

  echo
  echo "-- законный близнец B (ОДИН факт): та же полоса на посадке external --"
  world_external values.zz-self-test-band-ext.yaml "$POSTURE_EXTERNAL
uif:
  host:
    upstreams:
      kratosUi: kratos-selfservice-ui.kacho.svc.cluster.local:3000"
  st_probe "полоса на external без объявлений → зелёное" 0 "PASS:"
  world_reset

  echo
  echo "-- инъекция 1 (ОДИН факт против внешнего мира): полоса снята из раздачи БЕЗУСЛОВНО --"
  world_external values.zz-self-test-world.yaml "$WORLD"
  python3 - "$COPY_SRC/templates/configmap-nginx.yaml" <<'INJ1'
import io, re, sys
p = sys.argv[1]
s = io.open(p, encoding="utf-8").read()
s2 = re.sub(r"\n        location ~ \^/\([a-z0-9|_-]+\)\(/\|\$\) \{.*?\n        \}\n", "\n", s, flags=re.S)
assert s2 != s, "инъекция не внесена — форма полосы изменилась, доказательство стало бы вакуумным"
io.open(p, "w", encoding="utf-8").write(s2)
INJ1
  repack
  st_probe "полоса снята → находка о дереве" 1 "полосы обслуживания в РЕНДЕРЕ"
  restore_src

  echo
  echo "-- инъекция 3 (ОДИН факт против внешнего мира): полоса снята, КОММЕНТАРИЙ о ней остался --"
  # Ось заведена по измеренному ложному зелёному (2026-09-22): образец шёл
  # поиском по подстроке, и упоминание снятой полосы в комментарии читалось как
  # обслуживание. Комментарий, называющий снятое, — первое, что пишет полоса
  # снятия.
  python3 - "$COPY_SRC/templates/configmap-nginx.yaml" <<'INJ3'
import io, re, sys
p = sys.argv[1]
s = io.open(p, encoding="utf-8").read()
s2, n = re.subn(r"\n        location ~ \^/\([a-z0-9|_-]+\)\(/\|\$\) \{.*?\n        \}\n", "\n", s, flags=re.S)
assert n == 1, "инъекция не внесена: полоса не найдена (совпадений %d)" % n
anchor = "        location = /healthz {"
assert s2.count(anchor) >= 1
note = ("        # Снята полоса `location ~ ^/(login|registration|recovery|verification|"
        "settings|error|consent|logout)(/|$)` вместе с чужим поставщиком личности.\n")
io.open(p, "w", encoding="utf-8").write(s2.replace(anchor, note + anchor, 1))
INJ3
  repack
  st_probe "полоса снята, комментарий остался → находка, а не зелёное" 1 "полосы обслуживания в РЕНДЕРЕ"
  restore_src

  echo
  echo "-- инъекция 4 (ОДИН факт против внешнего мира): полоса объявлена, но НЕ переадресует --"
  # Полоса без `proxy_pass` не обслуживает ничего: запрос доходит до раздачи и
  # уходит в общий `location /`. Объявление живо, исполнения нет — тот же класс
  # с третьей стороны.
  python3 - "$COPY_SRC/templates/configmap-nginx.yaml" <<'INJ4'
import io, re, sys
p = sys.argv[1]
s = io.open(p, encoding="utf-8").read()
s2, n = re.subn(r"(?m)^(            proxy_pass http://\$kratos_ui;)$", r"            # \1", s)
assert n == 1, "инъекция не внесена: переадресация полосы не найдена (совпадений %d)" % n
io.open(p, "w", encoding="utf-8").write(s2)
INJ4
  repack
  st_probe "полоса без переадресации → находка, а не зелёное" 1 "НИ ОДНА не переадресует"
  restore_src

  echo
  echo "-- законный близнец (ОДИН факт против внешнего мира): хвостовой комментарий со \`}\` в теле РАБОЧЕЙ полосы --"
  # Ось держит КОПИЮ лексического прохода на Python (kacho#2781, kacho#2810 п. 2):
  # Go-проба судит свою копию, а эту — только рендер. Снятие целых строк
  # оставляло хвостовой комментарий, его `}` обрывала тело до переадресации, и
  # рабочая полоса читалась необслуживающей. Обезвредьте `strip_comments` — ось
  # покраснеет; невредимая — зелёное.
  python3 - "$COPY_SRC/templates/configmap-nginx.yaml" <<'INJ5'
import io, sys
p = sys.argv[1]
s = io.open(p, encoding="utf-8").read()
line = '            set $kratos_ui "${KACHO_UI_KRATOS_UI_UPSTREAM}";\n'
assert s.count(line) == 1, "инъекция не внесена: строка тела полосы не найдена (совпадений %d)" % s.count(line)
io.open(p, "w", encoding="utf-8").write(
    s.replace(line, line[:-1] + "  # прежняя ветка кончалась здесь: }\n", 1))
INJ5
  repack
  st_probe "хвостовой комментарий со скобкой в рабочей полосе → зелёное, а не отказ" 0 "PASS:"
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
