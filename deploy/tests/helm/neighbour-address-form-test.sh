#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ФОРМА АДРЕСА СОСЕДА: РАЗРЕШИМОСТЬ НЕ ДЕЛЕГИРУЕТСЯ ЧУЖОМУ resolv.conf.
#
# ─────────────────────────────────────────────────────────────────────────────
# СВОЙСТВО, КОТОРОЕ ЭНФОРСИМ
#
#   Адрес соседа обязан разрешаться, НЕ ВЫХОДЯ за пределы поисковых записей,
#   которые задаёт сам кластер.
#
# Кластер даёт поду три записи: `<ns>.svc.cluster.local svc.cluster.local
# cluster.local`. Kubelet при `dnsPolicy: ClusterFirst` ДОПИСЫВАЕТ к ним
# поисковые домены УЗЛА (в kind узел берёт их у Docker, Docker — у хоста).
# Умолчание `ndots:5` означает: имя с числом точек МЕНЬШЕ пяти трактуется как
# относительное, поэтому поисковый список консультируется ДО абсолютного
# запроса.
#
# Отсюда весь дефект. Написание `<svc>.<ns>.svc.cluster.local` несёт РОВНО
# ЧЕТЫРЕ точки: оно исчерпывает все три кластерные записи и уходит в домены
# ХОЗЯИНА МАШИНЫ. Если хоть один из них отвечает не-NXDOMAIN (SERVFAIL, REFUSED,
# таймаут — например локальный агент, обслуживающий свою зону), musl- и
# Go-резолверы ПРЕКРАЩАЮТ обход и отдают «bad address», НЕ ДОЙДЯ до абсолютного
# имени. Наверху это выглядит как `UNAVAILABLE` — то есть как отказ СОСЕДА, а не
# как отказ ИМЕНИ, и разбирается соответственно долго.
#
# Написание `<svc>.<ns>.svc` несёт две точки и попадает в `cluster.local` ТРЕТЬЕЙ
# кластерной записью — до чужих доменов очередь не доходит НИКОГДА. Абсолютная
# форма `<svc>.<ns>.svc.cluster.local.` тоже годится (один запрос, поисковый
# список не консультируется вовсе).
#
# Существо дефекта не в чужом resolv.conf, а в НАШЕМ решении: корректность
# каждого межсервисного вызова была делегирована тому, что оператор положил себе
# на узел. Это свойство нашего дерева, и держать его обязано дерево.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ДВЕ ПОЛОВИНЫ, А НЕ ОДНА
#
# Адрес соседа приезжает в под ИЗ ТРЁХ МЕСТ: файла значений, нашего шаблона и
# УМОЛЧАНИЯ, ВКОМПИЛИРОВАННОГО В БИНАРЬ. Половина по рендеру видит первые два и
# by construction НЕ ВИДИТ третьего: умолчание не попадает в манифест вовсе —
# его подставляет процесс при старте.
#
# Это не гипотеза. Померено на этом дереве: `KACHO_API_GATEWAY_KRATOS_PUBLIC_URL`
# не выставляет НИ ОДИН чарт, поэтому живым значением работает вкомпилированное
# умолчание — и в рендере его нет ни одного вхождения. Гейт «только по рендеру»
# объявил бы дерево чистым, пока шлюз ходит к провайдеру личности по дефектной
# форме.
#
# Обратное тоже верно: половина по исходникам не видит адресов, приезжающих
# умолчанием ЧУЖОГО чарта-зависимости (он материализуется архивом, в git его
# нет). Половины покрывают ровно то, чего структурно не может увидеть другая, и
# ни одна не является дублем другой.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ЧИТАЕТСЯ СТРУКТУРНО, А НЕ ТЕКСТОМ
#
# `Certificate.spec.dnsNames` обязан нести ПОЛНУЮ форму и остаётся как есть: это
# перечень имён, которые предъявляет сертификат, а не адрес, по которому кто-то
# ходит. Текстовый обход пометил бы их находкой и потребовал снять SAN, который
# обязан остаться, — поэтому рендер разбирается ДОКУМЕНТНО, а поле пропускается
# по своей координате в структуре, а не по соседству слов.
#
# ─────────────────────────────────────────────────────────────────────────────
# ВИДОВ ЗНАЧЕНИЙ ТРИ, А НЕ ДВА: РЕЗОЛВИТ LIBC · РЕЗОЛВИТ NGINX · НЕ РЕЗОЛВИТ НИКТО
#
# Первые два вида различает вопрос «КТО резолвит». Третий вид отвечает на него
# иначе: имя НЕ РЕЗОЛВИТСЯ ВОВСЕ — оно СРАВНИВАЕТСЯ. Это `ServerName` клиента
# TLS: библиотека кладёт его в SNI и сверяет с SAN серверного листа, DNS-запроса
# по нему не делается НИ ОДНОГО. Поисковый список к такому значению отношения не
# имеет, и правило формы адреса к нему не применимо: гейт, судящий его правилом
# для адресов, требует изменить величину по основанию, которого у неё нет.
#
# Наблюдалось на этом дереве: `KANAME_REST_UPSTREAM_MTLS_SERVERNAME`. Дозвон у
# этого ребра идёт в ПЕТЛЮ — `restfront.DialTarget` отдаёт `127.0.0.1:<порт>`,
# литеральный адрес, — а лист выписан на имя Service. То есть DNS на ребре не
# участвует вообще, и единственное имя на нём существует ровно затем, чтобы его
# сверили побайтово.
#
# ПРИЗНАК ТРЕТЬЕГО ВИДА — КОНЪЮНКЦИЯ ДВУХ УСЛОВИЙ, И НИ ОДНО НЕ ДАЁТСЯ
# ПЕРЕИМЕНОВАНИЕМ:
#
#   1. КТО ЧИТАЕТ — имя оканчивается на `_<ТЕГ>_SERVERNAME`, где `<ТЕГ>` есть
#      envconfig-тег поля, чей ТИП имеет форму TLS-клиента (в одном struct поля
#      `ServerName` и `CAFiles`). Перечень тегов ВЫВОДИТСЯ обходом
#      `git ls-files '*.go'`, а не выписан здесь: выписанный разошёлся бы с
#      деревом молча, и разошёлся бы он там, где это не видно.
#   2. КАК УПОТРЕБЛЕНО ЗДЕСЬ — в ТОМ ЖЕ списке `env` того же контейнера объявлен
#      `<та же голова>_CAFILES`: набор CA, ПРОТИВ которого имя и сверяется. Без
#      него сверять не с чем, и одинокий `_SERVERNAME` прощению не подлежит.
#
# Обойти переименованием нельзя: условие 1 гранит не имя, а ОБЪЯВЛЕНИЕ В GO —
# чтобы под него попасть, надо завести поле типа формы TLS-клиента с этим тегом,
# то есть на самом деле объявить ребро со сверкой имени; условие 2 требует, чтобы
# чарт объявил рядом набор CA того же ребра. Переименование адреса в
# `..._SERVERNAME` не даёт ничего: такого тега в дереве нет, а одна голова без
# `_CAFILES` не прощается. Обратное тоже верно: снимут `ServerName` из типа —
# перечень тегов схлопнется, и прощение ИСТЕЧЁТ САМО.
#
# ОТВЕРГНУТО, И ЗАМЕРОМ, А НЕ ВКУСОМ: признак «значение побайтово равно записи
# `Certificate.spec.dnsNames` того же рендера». Он выглядит структурным и
# маскирует настоящую находку: в стеках `prod` и `fe3455` SAN базы несёт
# `kacho-umbrella-pg-<домен>.kacho.svc.cluster.local`, а `KACHO_<ДОМЕН>_DB_HOST`
# — ГОЛЫЙ хост того же пира, без порта и схемы. Написание полной формы в этой
# ручке есть ровно тот дефект, ради которого гейт заведён, и по такому признаку
# оно было бы прощено молча. Перепись по шести рендерам: значений, побайтово
# равных какому-то SAN, — 63 позиции, из них 22 суть имена-для-сверки и 3 суть
# адреса дозвона (`KACHO_{COMPUTE,GEO,REGISTRY}_DB_HOST`).
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДПОСЫЛКА — ОТКАЗ, А НЕ ПРОПУСК
#
# Несобравшийся рендер, ноль документов, ноль прочитанных исходников — код 2
# («не выполнилось»), а не «чисто». Печатаются число стеков, документов, строк,
# файлов и найденных адресов: «ноль находок» обязано быть отличимо от «ноль
# прочитанного».
#
# Самопроверка: `--self-test` — классификатор против синтетических рендеров, без
# helm и без кластера. Требует покраснеть на внесённом дефекте И промолчать на
# законной конструкции той же формы.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

# any_line_matches <многострочное значение> <ERE> — как `grep -qE`: истинно, если
# ХОТЬ ОДНА строка значения совпадает с выражением. Построчность важна: у `grep`
# точка не переходит через перевод строки, а у `[[ =~ ]]` на всём значении —
# переходит. Труба убрана из-за ложного отказа на совпадении (задача #658).
any_line_matches() {
  local _l
  while IFS= read -r _l; do
    if [[ "$_l" =~ $2 ]]; then return 0; fi
  done <<<"$1"
  return 1
}
# Состав стендов — из ЕДИНСТВЕННОЙ таблицы дерева (deploy/stacks.txt).
# Своей копии цепочек здесь нет: копии разъезжались молча.
. "$(dirname "$0")/stacks.sh"

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_ROOT/.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"

# ── Три исхода — ОБЩЕЙ реализацией каталога, а не своей копией ───────────────
#
# 0 зелено · 1 находка о дереве · 2 условие не создано (плюс текст самого helm).
# Свой словарь исходов здесь назывался ТРЕТЬИМ именем каталога — `refuse`, — при
# том что соседи ту же категорию звали `fatal`, а её код (2) шапка объявляла со
# ссылкой на них же. Три имени одного решения читаются как три решения.
#
# Категория «архив зависимости СТАРШЕ своего исходника» была у этого файла и
# БОЛЬШЕ НИ У КОГО в каталоге; при сведении она перенесена в общую реализацию
# (`require_fresh_dep_charts`), а не подрезана под неё — см. её разбор в outcome.sh.
#
# Счёт утверждений скрипт ведёт САМ, поэтому вердикт печатает `findings_verdict`.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"

ASSERTIONS=0
assertion() { ASSERTIONS=$((ASSERTIONS + 1)); ok; }
good() { echo "  ✓ $1"; }
# `violation` (накопить находку и продолжить) и `fail` (оборвать кодом 1) берутся
# ИЗ ОБЩЕЙ РЕАЛИЗАЦИИ. Своей копии `violation` здесь не заводится: она отличалась
# бы от общей только именем счётчика, и это ровно та форма расхождения, которую
# сведение снимает.
note() { echo "    · $1"; }

# ─────────────────────────────────────────────────────────────────────────────
# КЛАССИФИКАТОР — ЧИСТАЯ ФУНКЦИЯ НАД ОДНИМ ФАЙЛОМ РЕНДЕРА.
#
# Вынесен затем, чтобы самопроверка могла скормить ему синтетический рендер БЕЗ
# helm и потребовать покраснеть на дефекте и промолчать на законном близнеце.
# ─────────────────────────────────────────────────────────────────────────────
CLASSIFIER="$(mktemp)"
DERIVER="$(mktemp)"
trap 'rm -f "$CLASSIFIER" "$DERIVER"' EXIT
cat >"$CLASSIFIER" <<'PYEOF'
import os
import re
import sys

try:
    import yaml
except ImportError:
    sys.stderr.write("нужен python3-yaml — классификатор разбирает рендер документно\n")
    sys.exit(2)

# Имя, оканчивающееся на .svc.cluster.local и НЕ абсолютное (без точки в конце).
ADDR = re.compile(r'[A-Za-z0-9][A-Za-z0-9.-]*\.svc\.cluster\.local(?!\.)')

# Законные полные формы, которые НЕ являются адресом нашего соседа.
# Запись живёт, пока у неё есть предмет: не встретилась ни разу — сама находка.
# Потребители, у которых резолвер НЕ libc. Полная форма для них ОБЯЗАТЕЛЬНА, и это
# не послабление, а другое требование: nginx с `resolver` поисковый список не
# применяет (доказано воспроизведением; см. lessons/rule-true-for-one-resolver-
# applied-to-all-addresses). Признак — имя переменной потребителя.
NGINX_RESOLVED = re.compile(r'KACHO_UI_[A-Z0-9_]*UPSTREAM|_UPSTREAM$')
nginx_hits = {}

# ТРЕТИЙ ВИД — имя, которое НЕ резолвится, а СРАВНИВАЕТСЯ (TLS ServerName).
# Перечень тегов приезжает ИЗ ОБХОДА ДЕРЕВА (см. шапку и блок вывода в оболочке),
# здесь он не выписан НИ ОДНОЙ строкой: выписанный был бы вторым местом об одном
# предмете. Пустой перечень означает «дерево не читали» — оболочка на нём
# отказывается выносить вердикт ДО того, как классификатор будет позван.
VERIFY_TAGS = tuple(t for t in os.environ.get('KACHO_TLS_VERIFY_NAME_TAGS', '').split() if t)
verify_hits = {}


def is_verification_name(envname, siblings):
    """Имя-для-сверки TLS: оба условия обязательны (см. шапку скрипта)."""
    if not envname.endswith('_SERVERNAME'):
        return False
    head = envname[:-len('_SERVERNAME')]
    # (1) КТО ЧИТАЕТ: голова оканчивается объявленным в дереве тегом.
    if not any(head == t or head.endswith('_' + t) for t in VERIFY_TAGS):
        return False
    # (2) КАК УПОТРЕБЛЕНО: рядом объявлен набор CA, против которого сверяют.
    return (head + '_CAFILES') in siblings


def env_entry_names(node):
    """Имена соседей по списку env — по ФОРМЕ записи, а не по имени ключа.

    Запись окружения узнаётся тем, что она несёт `name` и один из `value` /
    `valueFrom`. Привязка к ключу `env` была бы привязкой к имени, а не к виду.
    """
    names = set()
    for item in node:
        if not isinstance(item, dict) or not isinstance(item.get('name'), str):
            continue
        if 'value' in item or 'valueFrom' in item:
            names.add(item['name'])
    return frozenset(names)


ALLOWED = {
    'kubernetes.default.svc.cluster.local':
        'идентификатор издателя API-сервера (--service-account-issuer), сверяется '
        'побайтово с полем iss в токене — это не наш сосед, а внешний контракт',
}


def classify(path, stack):
    """Печатает находки. Возвращает (находки, документы, строки, попадания_allow)."""
    try:
        docs = [d for d in yaml.safe_load_all(open(path)) if d]
    except Exception as exc:                       # noqa: BLE001
        print("  ✗ [%s] рендер не разобран как YAML: %s" % (stack, exc))
        return (1, 0, 0, {})

    findings = 0
    strings = 0
    allow_hits = {}

    def walk(node, kind, where, ctx='', siblings=frozenset()):
        nonlocal findings, strings
        if isinstance(node, dict):
            # Пара {name, value} у переменной окружения: имя стоит рядом со
            # значением, и без него значение неотличимо от любого другого адреса.
            envname = node.get('name') if isinstance(node.get('name'), str) else ''
            if envname and NGINX_RESOLVED.search(envname):
                nginx_hits[envname] = nginx_hits.get(envname, 0) + 1
                return
            # ТРЕТИЙ ВИД: значение не резолвится, а сверяется с SAN. Правило
            # формы адреса к нему не применимо — предмета у него нет.
            if envname and is_verification_name(envname, siblings):
                verify_hits[envname] = verify_hits.get(envname, 0) + 1
                return
            for key, val in node.items():
                # SAN — перечень предъявляемых имён, а не адрес вызова.
                if kind == 'Certificate' and key == 'dnsNames':
                    continue
                # Адрес, который резолвит НЕ libc, а nginx: он читает его через
                # свой `resolver` и поисковый список НЕ применяет — короткая
                # форма не резолвится никогда, апстрим отвечает 502. Признак
                # различения — КТО резолвит, поэтому исключение привязано к имени
                # переменной потребителя, а не к файлу и не к перечню адресов.
                if isinstance(key, str) and NGINX_RESOLVED.search(key):
                    nginx_hits[key] = nginx_hits.get(key, 0) + 1
                    continue
                walk(val, kind, where + '.' + str(key), str(key), siblings)
        elif isinstance(node, list):
            # Список записей окружения задаёт КРУГ СОСЕДЕЙ для условия (2):
            # набор CA ищется в том же контейнере, а не где угодно в документе.
            here = env_entry_names(node)
            below = here if here else siblings
            for i, val in enumerate(node):
                walk(val, kind, '%s[%d]' % (where, i), ctx, below)
        elif isinstance(node, str):
            strings += 1
            if NGINX_RESOLVED.search(ctx or ''):
                nginx_hits[ctx] = nginx_hits.get(ctx, 0) + 1
                return
            for m in ADDR.finditer(node):
                name = m.group(0)
                if name in ALLOWED:
                    allow_hits[name] = allow_hits.get(name, 0) + 1
                    continue
                findings += 1
                short = name[:-len('.cluster.local')]
                print("  ✗ [%s] %s  %s" % (stack, where.lstrip('.'), name))
                print("      документ: %s/%s" % (kind, cur_name[0]))
                print("      значение: %s" % node.strip()[:120])
                print("      привести к: %s   (или %s. — абсолютная форма)" % (short, name))

    cur_name = ['?']
    for d in docs:
        kind = d.get('kind', '?')
        cur_name[0] = (d.get('metadata') or {}).get('name', '?')
        walk(d, kind, '')
    return (findings, len(docs), strings, allow_hits)


if __name__ == '__main__':
    render_path, stack_name = sys.argv[1], sys.argv[2]
    n, ndocs, nstr, hits = classify(render_path, stack_name)
    for name, cnt in sorted(hits.items()):
        print("  allow %s ×%d — %s" % (name, cnt, ALLOWED[name]))
    # ОДНА строка на стек, а не строка на имя: перечень целиком печатается
    # ОДИН раз в конце обхода (машинная строка VERIFY собирает его в оболочке).
    # Сто тридцать шесть одинаковых строк перестают читать на третьем стеке, и
    # вместе с ними перестают читать настоящую находку.
    if verify_hits:
        print("  verify имён-для-сверки: %d (сверяются с SAN, не резолвятся; "
              "различных %d)" % (sum(verify_hits.values()), len(verify_hits)))
        print("VERIFY %s" % " ".join(sorted(verify_hits)))
    print("SCOPE docs=%d strings=%d findings=%d verify=%d"
          % (ndocs, nstr, n, sum(verify_hits.values())))
    sys.exit(1 if n else 0)
PYEOF

require_python_yaml

# ─────────────────────────────────────────────────────────────────────────────
# ВЫВОД ПЕРЕЧНЯ ИМЁН-ДЛЯ-СВЕРКИ ИЗ ДЕРЕВА (третий вид, см. шапку).
#
# Читается ОБЪЯВЛЕНИЕ, а не слово: тип формы TLS-клиента опознаётся по составу
# полей (`ServerName` рядом с `CAFiles`) в одном struct, а тег — по `envconfig`
# у поля ЭТОГО типа. Тип берётся ПАКЕТ-КВАЛИФИЦИРОВАННЫМ: в дереве по два разных
# `TLSServer` и `TLSClient` в разных пакетах, и сопоставление по короткому имени
# прощало бы теги серверных рёбер, у которых `_SERVERNAME` не бывает вовсе, —
# прощение шире предмета.
# ─────────────────────────────────────────────────────────────────────────────
cat >"$DERIVER" <<'DERIVEEOF'
import re
import subprocess
import sys

FIELD = re.compile(r'^\s*(\w+)\s+([\w\.\[\]\*]+)(?:\s+`([^`]*)`)?', re.M)
PKG = re.compile(r'^package\s+(\w+)', re.M)
STRUCT = re.compile(r'type\s+(\w+)\s+struct\s*\{')
ENVCONFIG = re.compile(r'envconfig:"([^"]*)"')

out = subprocess.run(['git', 'ls-files', '*.go'], capture_output=True, text=True)
if out.returncode != 0:
    sys.stderr.write('git ls-files не выполнился: %s\n' % out.stderr.strip())
    sys.exit(2)
files = [f for f in out.stdout.split() if not f.endswith('_test.go')]

types = set()          # пакет.Тип — форма TLS-клиента
decls = []             # (пакет.Тип поля, envconfig-тег)
for path in files:
    try:
        src = open(path, encoding='utf-8', errors='replace').read()
    except OSError:
        continue
    pkgm = PKG.search(src)
    if not pkgm:
        continue
    pkg = pkgm.group(1)
    for m in STRUCT.finditer(src):
        i = m.end()
        depth = 1
        j = i
        while j < len(src) and depth:
            if src[j] == '{':
                depth += 1
            elif src[j] == '}':
                depth -= 1
            j += 1
        body = src[i:j - 1]
        fields = {fm.group(1): (fm.group(2), fm.group(3)) for fm in FIELD.finditer(body)}
        if 'ServerName' in fields and 'CAFiles' in fields:
            types.add(pkg + '.' + m.group(1))
        for ftype, tag in fields.values():
            if not tag or 'envconfig:' not in tag:
                continue
            bare = ftype.lstrip('*[]')
            decls.append((bare if '.' in bare else pkg + '.' + bare,
                          ENVCONFIG.search(tag).group(1)))

tags = sorted({tag for qual, tag in decls if qual in types and tag not in ('', '-')})

for t in sorted(types):
    print('TYPE %s' % t)
for t in tags:
    print('TAG %s' % t)
print('DERIVE gofiles=%d types=%d tags=%d' % (len(files), len(types), len(tags)))

# «Ноль тегов» и «дерево не читали» обязаны быть различимы: на пустом перечне
# прощение выключено целиком, и гейт объявил бы находкой каждое имя-для-сверки —
# то есть вынес бы вердикт о дереве, которого не читал.
if not files:
    sys.stderr.write('не прочитано ни одного не-тестового .go\n')
    sys.exit(2)
if not types:
    sys.stderr.write('в дереве не найдено ни одного типа формы TLS-клиента '
                     '(ServerName рядом с CAFiles) — перечень выводить не из чего\n')
    sys.exit(2)
if not tags:
    sys.stderr.write('ни одно envconfig-поле не имеет типа формы TLS-клиента — '
                     'перечень имён-для-сверки пуст\n')
    sys.exit(2)
DERIVEEOF

derive_out="$(cd "$REPO_ROOT" && python3 "$DERIVER" 2>&1)" || {
  printf '%s\n' "$derive_out" | sed 's/^/      /'
  fatal "перечень имён-для-сверки НЕ ВЫВЕДЕН из дерева — прощать по нему нельзя, вердикт НЕ ВЫНЕСЕН"
}
DERIVE_SCOPE="$(printf '%s\n' "$derive_out" | sed -n 's/^DERIVE //p' | tail -1)"
KACHO_TLS_VERIFY_NAME_TAGS="$(printf '%s\n' "$derive_out" | sed -n 's/^TAG //p' | tr '\n' ' ')"
export KACHO_TLS_VERIFY_NAME_TAGS
DERIVED_TYPES="$(printf '%s\n' "$derive_out" | sed -n 's/^TYPE //p' | tr '\n' ' ')"

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА
# ─────────────────────────────────────────────────────────────────────────────
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: классификатор против синтетических рендеров ==="
  echo "  перечень имён-для-сверки выведен из дерева: $DERIVE_SCOPE"
  echo "    типы формы TLS-клиента: $DERIVED_TYPES"
  echo "    теги: $KACHO_TLS_VERIFY_NAME_TAGS"
  st_rc=0
  probe() {
    local title="$1" body="$2" want="$3" grep_for="${4:-}"
    local f; f="$(mktemp)"
    printf '%s\n' "$body" >"$f"
    local out; out="$(python3 "$CLASSIFIER" "$f" selftest 2>&1)"; local rc=$?
    if [ "$rc" -ne "$want" ]; then
      echo "  ✗ $title — ожидался код $want, получен $rc"
      printf '%s\n' "$out" | sed 's/^/      /'
      st_rc=1
    elif [ -n "$grep_for" ] && ! any_line_matches "$out" "$grep_for"; then
      echo "  ✗ $title — код верен, но координата «$grep_for» НЕ НАЗВАНА"
      printf '%s\n' "$out" | sed 's/^/      /'
      st_rc=1
    else
      echo "  ✓ $title"
    fi
    rm -f "$f"
  }

  # (а) ВНЕСЁННЫЙ ДЕФЕКТ — обязан покраснеть и назвать координату.
  probe "дефект: полная форма в env нагрузки → красный + координата" \
'apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway
spec:
  template:
    spec:
      containers:
        - name: gw
          env:
            - name: KACHO_API_GATEWAY_IAM_GRPC
              value: "kaname.kacho.svc.cluster.local:9090"' 1 "kaname.kacho.svc.cluster.local"

  probe "дефект: полная форма в data настроек → красный" \
'apiVersion: v1
kind: ConfigMap
metadata:
  name: kratos
data:
  kratos.yaml: |
    selfservice:
      default_browser_return_url: http://kacho-umbrella-kratos-public.kacho.svc.cluster.local:80/' 1 "kratos-public.kacho.svc.cluster.local"

  # (б) ЗАКОННЫЙ БЛИЗНЕЦ ТОЙ ЖЕ ФОРМЫ — обязан промолчать.
  probe "законно: тот же адрес короткой формой → молчит" \
'apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway
spec:
  template:
    spec:
      containers:
        - name: gw
          env:
            - name: KACHO_API_GATEWAY_IAM_GRPC
              value: "kaname.kacho.svc:9090"' 0

  probe "законно: абсолютная форма (точка в конце) → молчит" \
'apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway
spec:
  template:
    spec:
      containers:
        - name: gw
          env:
            - name: KACHO_API_GATEWAY_IAM_GRPC
              value: "kaname.kacho.svc.cluster.local.:9090"' 0

  # САМЫЙ ВАЖНЫЙ БЛИЗНЕЦ: SAN обязан остаться полной формой.
  # Без этой пробы гейт потребовал бы снять имя, которое обязано быть в
  # сертификате, — и «починка» по нему сломала бы проверку узла TLS.
  probe "законно: та же строка в Certificate.dnsNames → молчит (это SAN, не вызов)" \
'apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: kaname-server
spec:
  dnsNames:
    - kaname.kacho.svc.cluster.local
    - kaname.kacho.svc
    - kaname.kacho' 0

  probe "законно: внешний издатель kubernetes.default → молчит (внешний контракт)" \
'apiVersion: v1
kind: ConfigMap
metadata:
  name: iam
data:
  trust.yaml: |
    issuer: "https://kubernetes.default.svc.cluster.local"' 0

  probe "законно: внешнее имя вне кластера → молчит" \
'apiVersion: v1
kind: ConfigMap
metadata:
  name: iam
data:
  trust.yaml: |
    issuer: "https://token.actions.githubusercontent.com"' 0

  # ── ТРЕТИЙ ВИД: ИМЯ, КОТОРОЕ СРАВНИВАЮТ, А НЕ РЕЗОЛВЯТ ────────────────────
  #
  # Тег для фикстур берётся ИЗ ВЫВЕДЕННОГО ПЕРЕЧНЯ, а не выписан: фикстура,
  # назвавшая тег литералом, пережила бы снятие этого тега из дерева и молча
  # доказывала бы работу прощения, которого больше нет.
  ST_TAG="$(printf '%s' "$KACHO_TLS_VERIFY_NAME_TAGS" | tr ' ' '\n' | grep . | sort | head -1)"
  if [ -z "$ST_TAG" ]; then
    echo "  ✗ перечень тегов пуст — пробы третьего вида были бы вакуумными"
    st_rc=1
  fi
  # Заведомо НЕ объявленный тег: если он вдруг окажется в перечне, пробу
  # «незаявленный тег → красный» доказывать нечем, и это отказ, а не пропуск.
  ST_UNDECLARED="ZZ_NOT_DECLARED_IN_TREE_MTLS"
  case " $KACHO_TLS_VERIFY_NAME_TAGS " in
    *" $ST_UNDECLARED "*)
      echo "  ✗ синтетический тег $ST_UNDECLARED объявлен в дереве — проба вакуумна"
      st_rc=1 ;;
  esac

  probe "законно: имя-для-сверки (тег $ST_TAG из дерева + рядом CAFILES) → молчит" \
"apiVersion: apps/v1
kind: Deployment
metadata:
  name: peer-client
spec:
  template:
    spec:
      containers:
        - name: svc
          env:
            - name: KACHO_SELFTEST_${ST_TAG}_CAFILES
              value: \"/etc/tls/ca.crt\"
            - name: KACHO_SELFTEST_${ST_TAG}_SERVERNAME
              value: \"kaname.kacho.svc.cluster.local\"" 0

  probe "дефект: тот же тег, но БЕЗ соседнего CAFILES → красный (сверять не с чем)" \
"apiVersion: apps/v1
kind: Deployment
metadata:
  name: peer-client
spec:
  template:
    spec:
      containers:
        - name: svc
          env:
            - name: KACHO_SELFTEST_${ST_TAG}_SERVERNAME
              value: \"kaname.kacho.svc.cluster.local\"" 1 "kaname.kacho.svc.cluster.local"

  probe "дефект: _SERVERNAME с НЕобъявленным в дереве тегом → красный" \
"apiVersion: apps/v1
kind: Deployment
metadata:
  name: peer-client
spec:
  template:
    spec:
      containers:
        - name: svc
          env:
            - name: KACHO_SELFTEST_${ST_UNDECLARED}_CAFILES
              value: \"/etc/tls/ca.crt\"
            - name: KACHO_SELFTEST_${ST_UNDECLARED}_SERVERNAME
              value: \"kaname.kacho.svc.cluster.local\"" 1 "kaname.kacho.svc.cluster.local"

  probe "дефект: набор CA есть, но у СОСЕДНЕГО контейнера → красный (круг соседей — свой env)" \
"apiVersion: apps/v1
kind: Deployment
metadata:
  name: peer-client
spec:
  template:
    spec:
      containers:
        - name: a
          env:
            - name: KACHO_SELFTEST_${ST_TAG}_CAFILES
              value: \"/etc/tls/ca.crt\"
        - name: b
          env:
            - name: KACHO_SELFTEST_${ST_TAG}_SERVERNAME
              value: \"kaname.kacho.svc.cluster.local\"" 1 "kaname.kacho.svc.cluster.local"

  # ДОКАЗАТЕЛЬСТВО, ЧТО ПРОПУСК SAN ИМЕННО СТРУКТУРНЫЙ, А НЕ ПО ИМЕНИ ПОЛЯ:
  # то же имя поля в НЕ-Certificate документе пропуску не подлежит.
  probe "дефект: поле dnsNames вне Certificate пропуску НЕ подлежит → красный" \
'apiVersion: v1
kind: ConfigMap
metadata:
  name: peers
data:
  dnsNames: "kacho-geo.kacho.svc.cluster.local:9090"' 1 "kacho-geo.kacho.svc.cluster.local"

  echo
  [ $st_rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $st_rc
fi

# ─────────────────────────────────────────────────────────────────────────────
# ПОЛОВИНА 1 — ОБХОД РЕНДЕРОВ
# ─────────────────────────────────────────────────────────────────────────────
echo "── половина 1: рендеры профилей (то, что реально получает под) ──"

require_dir_present "$UMBRELLA" "чарт умбреллы"
require_helm
require_umbrella_charts "$UMBRELLA"

# ── УСТАРЕВШИЙ АРХИВ — ОТКАЗ, А НЕ ВЕРДИКТ ───────────────────────────────────
#
# Категория была реализована ЗДЕСЬ и больше нигде в каталоге, хотя рендерят
# умбреллу двадцать скриптов. Реализация переехала в `outcome.sh`
# (`require_fresh_dep_charts`) вместе со своим разбором: почему успешный непустой
# рендер по устаревшему архиву — это «не выполнилось», а не «чисто», и почему он
# ошибается в ОБЕ стороны. Здесь остался вызов.
require_fresh_dep_charts "$UMBRELLA"

# ТАБЛИЦА СТЕКОВ ЧИТАЕТСЯ ИЗ ЕДИНСТВЕННОГО МЕСТА, А НЕ ДЕРЖИТСЯ КОПИЕЙ: два
# места об одном предмете разъезжаются молча, и разъедутся они там, где это не
# видно. Раньше она выковыривалась выражением из соседнего гейта — выражение
# пережило правку соседа ровно так же, как переживает копия.
STACKS="$(stacks_table)"
stack_n="$(printf '%s\n' "$STACKS" | grep -c . || true)"
assertion
if [ "${stack_n:-0}" -lt 1 ]; then
  fatal "таблица стеков не разобрана — обходить нечего"
fi
good "таблица стеков прочитана из deploy/stacks.txt: $stack_n стеков"

work="$(mktemp -d)"
trap 'rm -rf "$work"; rm -f "$CLASSIFIER"' EXIT

tot_docs=0
tot_strings=0
tot_find=0
tot_verify=0
verify_names=""
rendered=0

while IFS= read -r line; do
  [ -z "$line" ] && continue
  stack="${line%%:*}"
  files="${line#*:}"
  args=""
  IFS=','
  for f in $files; do args="$args -f $UMBRELLA/$f"; done
  unset IFS

  render="$work/$stack.yaml"
  # Отказ рендера — УСЛОВИЕ прогона, а не свойство дерева: код 2 и текст самого helm.
  # Сломанный шаблон при этом не прячется — его ловит шаг «umbrella template — каждый
  # стек таблицы», идущий в той же джобе РАНЬШЕ этого.
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" $args --namespace kacho
  render_or_fatal "стек $stack"
  printf '%s\n' "$HELM_OUT" >"$render"
  rendered=$((rendered + 1))

  assertion
  out="$(python3 "$CLASSIFIER" "$render" "$stack")"
  rc=$?
  scope="$(printf '%s\n' "$out" | grep '^SCOPE ' | tail -1)"
  d=$(printf '%s' "$scope" | sed -nE 's/.*docs=([0-9]+).*/\1/p')
  s=$(printf '%s' "$scope" | sed -nE 's/.*strings=([0-9]+).*/\1/p')
  n=$(printf '%s' "$scope" | sed -nE 's/.*findings=([0-9]+).*/\1/p')
  v=$(printf '%s' "$scope" | sed -nE 's/.*verify=([0-9]+).*/\1/p')
  tot_docs=$((tot_docs + ${d:-0}))
  tot_strings=$((tot_strings + ${s:-0}))
  tot_find=$((tot_find + ${n:-0}))
  tot_verify=$((tot_verify + ${v:-0}))
  verify_names="$verify_names $(printf '%s\n' "$out" | sed -n 's/^VERIFY //p' | tr '\n' ' ')"

  if [ "$rc" -ne 0 ]; then
    printf '%s\n' "$out" | grep -v '^SCOPE ' | grep -v '^VERIFY ' | grep -v '^  allow ' | grep -v '^  verify '
    violation "[$stack] адресов дефектной формы: ${n:-?} (документов $d, строк $s)"
  else
    good "[$stack] чисто: 0 находок (документов $d, строк $s)"
  fi
  printf '%s\n' "$out" | grep -E '^  (allow|verify) ' | sed 's/^/  /' || true
done <<<"$STACKS"

assertion
if [ "$rendered" -lt 1 ]; then
  fatal "не собрался НИ ОДИН рендер — «ноль находок» здесь означает «ноль прочитанного»"
fi
if [ "$tot_docs" -lt 1 ]; then
  fatal "прочитано 0 документов — обходить было нечего"
fi
good "объём половины 1: стеков $rendered/$stack_n, документов $tot_docs, строк $tot_strings"
assertion
uniq_verify="$(printf '%s' "$verify_names" | tr ' ' '\n' | grep . | sort -u)"
uniq_verify_n="$(printf '%s\n' "$uniq_verify" | grep -c . || true)"
good "перечень имён-для-сверки выведен из дерева: $DERIVE_SCOPE; вхождений в рендерах $tot_verify, различных ${uniq_verify_n:-0}"
note "типы формы TLS-клиента: $DERIVED_TYPES"
note "теги: $KACHO_TLS_VERIFY_NAME_TAGS"
# Перечень печатается ЦЕЛИКОМ и ровно один раз: прощённое обязано быть видно
# поимённо, иначе новое прощение приезжает неотличимо от старого.
printf '%s\n' "$uniq_verify" | sed 's/^/    · /'

# ── исключение живёт, пока у него есть ПРЕДМЕТ ───────────────────────────────
#
# Предмет ищется в ОТСЛЕЖИВАЕМОМ ДЕРЕВЕ, а не в рендере, и это не послабление, а
# точность: единственная запись сегодня (издатель API-сервера) лежит под
# выключенным флагом и служит образцом для оператора, поэтому в рендер не
# попадает НИ В ОДНОМ стеке. Проверять её попаданием в рендер значило бы
# требовать снять исключение, которое станет нужным ровно в тот момент, когда
# оператор флаг включит.
#
# Самоистечение при этом настоящее: строка исчезнет из дерева — запись станет
# находкой.
assertion
# ИЗВЛЕЧЕНИЕ ЧИТАЕТ БЛОК ПО ЕГО СОБСТВЕННОМУ ЗАГОЛОВКУ, и его НЕНАХОЖДЕНИЕ —
# ОТКАЗ, а не «записей нет». Прежняя редакция брала адрес диапазона sed из
# соседних строк классификатора; правка классификатора утащила в этот адрес
# несколько строк, диапазон перестал совпадать с чем бы то ни было, и проверка
# самоистечения печатала «исключать нечего, проверять нечего» при живой записи
# — ровно тот класс, который она сама и ловит.
allow_block="$(sed -n '/^ALLOWED = {/,/^}/p' "$CLASSIFIER")"
if [ -z "$allow_block" ]; then
  fatal "блок ALLOWED не найден в классификаторе — «записей нет» и «блок не прочитан» неразличимы; вердикт об исключениях НЕ ВЫНЕСЕН"
fi
allow_names="$(printf '%s\n' "$allow_block" | sed -nE "s/^    '([^']+)':.*/\\1/p")"
allow_n="$(printf '%s\n' "$allow_names" | grep -c . || true)"
if [ "${allow_n:-0}" -lt 1 ]; then
  good "записей законных полных форм нет — исключать нечего, проверять нечего"
else
  # ОТСУТСТВИЕ отличается от НЕИСПРАВНОГО ВЫЗОВА. Первая редакция этой проверки
  # звала `git grep -qsF`; ключа `-s` у git grep нет (это ключ обычного grep),
  # поэтому вызов падал с кодом 129, а `2>/dev/null` его прятал — и предикат
  # УВЕРЕННО сообщал «предмета нет» про строку, лежащую в дереве. Код >1 теперь
  # ОТКАЗ, а не «не найдено».
  # ПРЕДМЕТ ИЩЕТСЯ ВНЕ ФАЙЛА, КОТОРЫЙ ОБЪЯВЛЯЕТ ИСКЛЮЧЕНИЕ. Без этого поиск
  # находит саму запись — она лежит в отслеживаемом файле, — и предикат
  # тождественно истинен: ни одна запись не может остаться без предмета, сколько
  # бы их ни накопилось. Тот же класс, что «проверка ищет слово в сыром тексте и
  # находит его в комментарии о самой проверке».
  SELF_REL="${0#"$REPO_ROOT/"}"
  case "$SELF_REL" in /*) SELF_REL="deploy/tests/helm/$SCRIPT" ;; esac
  dead=""
  while IFS= read -r nm; do
    [ -z "$nm" ] && continue
    (cd "$REPO_ROOT" && git grep -q -F -- "$nm" -- ":(exclude)$SELF_REL" >/dev/null 2>&1)
    case $? in
      0) : ;;
      1) dead="$dead $nm" ;;
      *) fatal "обход дерева за предметом исключения не выполнился (git grep, код $?) — вердикт об исключениях НЕ ВЫНЕСЕН" ;;
    esac
  done <<<"$allow_names"
  if [ -n "$dead" ]; then
    violation "записям законных полных форм нечего исключать в дереве:$dead"
    note "исключение без предмета унаследует следующую слепую зону — снять его"
  else
    good "все записи законных полных форм ($allow_n) имеют предмет в отслеживаемом дереве"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────────
# ПОЛОВИНА 2 — ВКОМПИЛИРОВАННЫЕ УМОЛЧАНИЯ (в рендере их НЕТ ни одного)
# ─────────────────────────────────────────────────────────────────────────────
echo
echo "── половина 2: умолчания в исходниках (рендер их не показывает by construction) ──"

cd "$REPO_ROOT" || fatal "не перейти в корень репозитория"

# Обход по СОДЕРЖИМОМУ РЕПОЗИТОРИЯ — то же множество, что увидит CI на свежем
# checkout'е, а не локальный мусор рабочего дерева.
go_files="$(git ls-files '*.go' | grep -v '_test\.go$' || true)"
go_n="$(printf '%s\n' "$go_files" | grep -c . || true)"
assertion
if [ "${go_n:-0}" -lt 1 ]; then
  fatal "git ls-files не вернул ни одного не-тестового .go — половина 2 НЕ ВЫПОЛНЕНА"
fi

# Признак — ОБЪЯВЛЕНИЕ УМОЛЧАНИЯ, а не слово в тексте: комментарий, называющий
# адрес, разбору не подлежит и находкой не является.
decl="$(printf '%s\n' "$go_files" | xargs -r git grep -nE \
  '(default:"[^"]*\.svc\.cluster\.local(:[0-9]+|/)?[^"]*"|SetDefault\([^)]*"[^"]*\.svc\.cluster\.local[^"]*"\))' -- 2>/dev/null || true)"
# Абсолютная форма — законна, из находок вычитается.
bad="$(printf '%s\n' "$decl" | grep -vE '\.svc\.cluster\.local\.' | grep -E '\.svc\.cluster\.local' || true)"
bad_n="$(printf '%s\n' "$bad" | grep -c . || true)"

good "объём половины 2: прочитано не-тестовых .go — $go_n файлов"
assertion
if [ "${bad_n:-0}" -gt 0 ]; then
  printf '%s\n' "$bad" | sed 's/^/      /'
  violation "вкомпилированных умолчаний дефектной формы: $bad_n"
  note "такое умолчание НЕ ВИДНО НИ В ОДНОМ рендере: его подставляет процесс при старте,"
  note "поэтому гейт «только по рендеру» объявил бы дерево чистым при живом дефекте"
else
  good "вкомпилированных умолчаний дефектной формы: 0"
fi

echo
echo "=== вердикт: утверждений $ASSERTIONS, находок $VIOLATIONS ==="
echo "=== объём: стеков $rendered, документов $tot_docs, строк $tot_strings, файлов .go $go_n; $DERIVE_SCOPE, имён-для-сверки прощено $tot_verify ==="
findings_verdict "стеков $rendered, документов $tot_docs, строк $tot_strings, файлов .go $go_n, $DERIVE_SCOPE, имён-для-сверки прощено $tot_verify"
