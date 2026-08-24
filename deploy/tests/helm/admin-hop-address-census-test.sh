#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ПЕРЕПИСЬ ПОТРЕБИТЕЛЕЙ АДРЕСА АДМИНИСТРАТИВНОГО API ПРОВАЙДЕРА.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ОБХОД, А НЕ РЕЕСТР
#
# Реестр `adminHopConsumers` (gateway/deploy/admin_hop_transport_test.go)
# резолвит ПУТИ В ФАЙЛАХ ЗНАЧЕНИЙ и потому BY CONSTRUCTION не видит потребителя,
# чей адрес приезжает умолчанием чарта-зависимости либо зашит в шаблон. Его число
# («4/4 проверено») читается как полнота, которой нет.
#
# Померено на этом дереве: потребителей СЕМЬ, реестру видны ЧЕТЫРЕ.
#   • умолчанием зависимости — реконсайлер клиентов провайдера (`maester.enabled`
#     истинно по умолчанию чарта, ни один профиль не переопределяет) и
#     тест-подключение самого чарта провайдера. Ни один из двух не лежит в git:
#     зависимость материализуется архивом, поэтому ТЕКСТОВЫЙ обход дерева их не
#     видит вовсе — их находит только половина по РЕНДЕРУ;
#   • зашитым в шаблон — хук выдачи доверия (values-ключа не имеет вовсе).
#
# Реконсайлер при этом НЕ ИМЕЕТ ПРОБ: при смене адреса он не покраснел бы, а тихо
# перестал разрешать имя. Молчаливое прекращение работы хуже отказа — поэтому
# «потребитель без проб на изменяемом пути» здесь отдельная находка.
#
# Обход — не дубль реестра, а его НЕДОСТАЮЩАЯ ПОЛОВИНА. Реестр остаётся
# единственным местом, где перечислены пути объявлений; эта проверка ЧИТАЕТ его
# оттуда (а не держит копию) и сверяет с тем, что реально попадает в рендер.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРАВИЛО ВЕРДИКТА
#
#   A. НИ ОДНА развёрнутая нагрузка не адресует административный листенер
#      ПРОВАЙДЕРА напрямую (`<release>-hydra-admin`). Открытого участка между
#      подами нет как объекта: листенер слушает петлю, его Service снят.
#   B. КАЖДАЯ нагрузка, адресующая ТЕРМИНАТОР (`<release>-hydra-admin-tls`),
#      объявлена значением, на которое ссылается запись реестра. Адрес в рендере,
#      которого не объявляет ни одна запись, — находка (потребитель, невидимый
#      реестру).
#   C. Долгоживущая нагрузка (Deployment/StatefulSet/DaemonSet), называющая
#      адрес, обязана иметь пробу. Без проб она перестанет работать МОЛЧА.
#   D. Запись списка законных упоминаний, под которую в дереве больше ничего не
#      подпадает, — находка. Исключение живёт, пока у него есть ПРЕДМЕТ.
#
# «Ноль находок» обязано быть отличимо от «ноль прочитанного»: печатаются число
# прочитанных файлов, документов рендера, найденных адресов и пройденные/
# исключённые поддеревья — с причиной по каждому исключению.
#
# ЗАВИСИМОСТЬ ЧАРТА — ПРЕДУСЛОВИЕ, А НЕ ПРОПУСК. Без материализованного чарта
# провайдера рендер не содержит ни реконсайлера, ни тест-подключения: обход нашёл
# бы четырёх потребителей из семи и ОТЧИТАЛСЯ ПОЛНОТОЙ. Поэтому отсутствие
# зависимости — ОТКАЗ (код 2), а не тихо суженный обход.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail
# Состав стендов — из ЕДИНСТВЕННОЙ таблицы дерева (deploy/stacks.txt).
# Своей копии цепочек здесь нет: копии разъезжались молча.
. "$(dirname "$0")/stacks.sh"

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_ROOT/.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
REGISTRY_GO="$REPO_ROOT/gateway/deploy/admin_hop_transport_test.go"

# ── Три исхода — ОБЩЕЙ реализацией каталога, а не своей копией ───────────────
#
# 0 зелено · 1 находка о дереве · 2 условие не создано (плюс текст самого helm).
# Прежде этот файл нёс СВОЮ копию всех трёх решений, и копия уже разошлась с
# каталогом по ДВУМ осям сразу:
#   • имя `fail` означало здесь НАКОПИТЬ И ПРОДОЛЖИТЬ, а в общей реализации —
#     ОБОРВАТЬ кодом 1. Один и тот же вызов в двух скриптах одного каталога делал
#     противоположное, и увидеть это можно было только прочитав оба;
#   • отказ рендера копился как находка о дереве — тем же кодом, каким объявляется
#     настоящий дефект, — поэтому несобранные зависимости умбреллы приходили
#     вердиктом о дереве (класс #1214).
#
# Счёт утверждений скрипт ведёт САМ (их число зависит от состава стендов), поэтому
# `EXPECTED_ASSERTIONS` он не объявляет и `outcome_verdict` не зовёт: вердикт
# печатает `findings_verdict`. `assertion` бумкает и общий счётчик, чтобы перепись
# при коде 2 была правдой, а не нулём.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"

ASSERTIONS=0
# `violation` (накопить находку и продолжить) и `fail` (оборвать кодом 1) берутся
# ИЗ ОБЩЕЙ РЕАЛИЗАЦИИ. Своей копии `violation` здесь не заводится: она отличалась
# бы от общей только именем счётчика, и это ровно та форма расхождения, которую
# сведение снимает.
good() { echo "  ✓ $1"; }
note() { echo "    $1"; }
assertion() { ASSERTIONS=$((ASSERTIONS + 1)); ok; }

# ── ПРЕДМЕТ ОБХОДА ───────────────────────────────────────────────────────────
#
# Административный API провайдера в ЛЮБОМ написании: и имя самого провайдера
# (`…-hydra-admin`), и имя терминатора перед ним (`…-hydra-admin-tls`). Оба —
# один и тот же API, и переписи нужны оба: первое обязано исчезнуть из нагрузок,
# второе обязано быть объявленным.
#
# `-ca` исключается: `…/hydra-admin-ca/ca.crt` — путь монтирования якоря
# доверия, а не адрес.
SUBJECT_RE='hydra-admin(?!-ca)'

# АДРЕС, А НЕ ИМЯ СЕКРЕТА. Имя секрета `<release>-hydra-admin-tls` совпадает с
# именем терминатора посимвольно, поэтому одного вхождения подстроки мало:
# требуется признак адреса — схема, порт или `.svc`. Без этого различения
# ссылка на секрет в томе читалась бы как потребитель API.
ADDRESS_RE='(https?://)?[a-z0-9-]*hydra-admin(-tls)?[a-z0-9.-]*(:[0-9]+)?'
# ПРИЗНАК АДРЕСА НА УРОВНЕ СТРОКИ (половина 1): схема, порт или `.svc`. Строка,
# где предмет — ИМЯ СЕКРЕТА или тома (`secretName: …-hydra-admin-tls`), потребителем
# API не является. В ШАБЛОНЕ признак переживает подстановку
# (`https://{{ .Release.Name }}-hydra-admin…:4445` схему сохраняет), поэтому зашитый
# в шаблон адрес отсюда виден. Объявлен ЗДЕСЬ, до самопроверки: инъекция обязана
# гонять тот же предикат, что и основной проход, а не его копию.
ADDRESS_LINE_RE='://|:[0-9]{2,5}|\.svc'
address_like() { # <token> → 0, если токен выглядит адресом
  case "$1" in
    http://*|https://*) return 0 ;;
    *:[0-9]*)           return 0 ;;
    *.svc*)             return 0 ;;
  esac
  return 1
}

# КОММЕНТАРИЙ — НЕ АДРЕС. Отбрасывается ДО извлечения, и это не предосторожность
# впрок: первая же редакция этой проверки объявила потребителем комментарий в
# шаблоне iam, который ОБЪЯСНЯЕТ, почему выведенный адрес непригоден
# (`https://hydra-admin.<domain>` … UNREACHABLE). Гейт, который красят чужие
# объяснения, снимут при первом ложном срабатывании — тот же класс, что
# «проверка ищет слово в сыром тексте и находит его в комментарии о самой
# проверке» (testing.md §гейт на класс, п. 4).
#
# ПРАВИЛО ВЫБИРАЕТСЯ ПО ЯЗЫКУ, А НЕ ПРИМЕНЯЕТСЯ КО ВСЕМ СРАЗУ. Половина 1 обходит
# и `*.go` (services/*/deploy/*), где комментарий начинается с `//`: отбрасывание
# `#` его не трогает, и объяснение «почему этот адрес непригоден» читалось как
# потребитель. Обратное тоже неверно — срезать `//` в shell и yaml значило бы рубить
# строку на любом URL. Тот же водораздел, что у прогонщика самопроверок.
#
# СТРОКИ НЕ УДАЛЯЮТСЯ, А ОПУСТОШАЮТСЯ. Половина 1 берёт координату из `grep -n`, и
# удаление комментариев сдвигало нумерацию: находка называла строку укороченного
# потока, а не файла. Координата, указывающая не туда, — то же, что её отсутствие.
uncommented() {
  case "$1" in
    *.go|*.js) sed -e 's|[[:space:]]//.*$||' -e 's|^[[:space:]]*//.*$||' "$1" ;;
    *)         sed -e 's/[[:space:]]#[[:space:]].*$//' -e 's/^[[:space:]]*#.*$//' "$1" ;;
  esac
}

# extract_addresses <файл> — адресоподобные вхождения предмета, по одному в строке.
extract_addresses() {
  local tok
  uncommented "$1" | grep -ohP "$ADDRESS_RE" 2>/dev/null | while IFS= read -r tok; do
    address_like "$tok" && echo "$tok"
  done | sort -u
}

# provider_spelling <адрес> — 0, если это имя ПРОВАЙДЕРА, а не терминатора.
provider_spelling() {
  case "$1" in
    *hydra-admin-tls*) return 1 ;;
    *hydra-admin*)     return 0 ;;
  esac
  return 1
}

# ── ЗАКОННЫЕ УПОМИНАНИЯ В ДЕРЕВЕ (проверка D: каждая запись обязана иметь предмет)
#
# Файлы, где называть адрес — это и есть их работа. Всё остальное в пройденном
# поддереве, что называет адрес, обязано быть объявлением потребителя, известным
# реестру.
# Список ведётся ПО ФАКТУ: запись вносится вместе с файлом, а не впрок. Запись
# без предмета — находка (проверка D ниже), поэтому «объявлю заранее» здесь
# обошлось бы дороже, чем внести строку в том же изменении, что создаёт файл.
ALLOWED_TREE=(
  "deploy/helm/umbrella/templates/hydra-admin-certificate.yaml|SAN сертификата терминатора"
  "deploy/helm/umbrella/templates/hydra-admin-tls-configmap.yaml|конфигурация терминатора: адрес апстрима и есть её предмет"
  "deploy/helm/umbrella/templates/hydra-admin-tls-service.yaml|Service терминатора: его имя и есть адрес"
  "deploy/tests/helm/admin-hop-transport-test.sh|гейт транспорта: адрес — его предмет"
  "deploy/tests/helm/admin-hop-address-census-test.sh|эта перепись"
  "deploy/scripts/assert-admin-hop-transport.sh|живой гейт перехода: пробник обращается по обоим адресам"
  "deploy/scripts/remeasure-provider-listener-tls.sh|перемер предпосылки"
  "deploy/scripts/newman-parallel.sh|прогонщик e2e: транспорт к ТЕРМИНАТОРУ открывает он сам (port-forward волны церемонии), развёрнутой нагрузкой не является"
)

# ── ПОДДЕРЕВЬЯ ОБХОДА, ОБЪЯВЛЕННЫЕ ЯВНО ──────────────────────────────────────
#
# Печатаются оба списка. Прозой область не заявляется: печатается то, что реально
# обошли, и то, что исключили — с причиной и ЧИСЛОМ совпадений под исключением.
WALK_PREFIXES='deploy/ gateway/deploy/ services/*/deploy/ ui-future/deploy/'
EXCLUDE_REASON='код сервисов и их тесты: код ЧИТАЕТ объявленное значение и адреса не несёт, литералы в его фикстурах развёрнутой нагрузке адрес не задают'

# tree_files — файлы пройденного поддерева (по содержимому репозитория, как увидит CI).
tree_files() {
  git -C "$REPO_ROOT" ls-files \
    'deploy/*.yaml' 'deploy/*.yml' 'deploy/*.tpl' 'deploy/*.sh' 'deploy/*.py' 'deploy/*.go' \
    'gateway/deploy/*' 'services/*/deploy/*' 'ui-future/deploy/*' 2>/dev/null \
    | grep -E '\.(yaml|yml|tpl|sh|py|go)$'
}

# excluded_files — файлы вне пройденного поддерева (для честного числа под исключением).
excluded_files() {
  git -C "$REPO_ROOT" ls-files 2>/dev/null | grep -E '\.(yaml|yml|tpl|sh|py|go)$' \
    | grep -vE '^(deploy/|gateway/deploy/|services/[^/]+/deploy/|ui-future/deploy/)'
}

# ── РЕЕСТР ЧИТАЕТСЯ ИЗ ЕГО ЕДИНСТВЕННОГО МЕСТА ───────────────────────────────
#
# Пути объявлений берутся из литерала `adminHopConsumers`, а не переписываются
# сюда: копия разошлась бы с оригиналом, и «расхождение с реестром» стало бы
# расхождением с копией. Формат строки литерала:
#   "метка": {"a", "b", "c"},
registry_paths() {
  [ -f "$REGISTRY_GO" ] || return 1
  sed -n '/adminHopConsumers = map\[string\]\[\]string{/,/^}/p' "$REGISTRY_GO" \
    | grep -oP '\{[^{}]*"\}' \
    | sed -e 's/[{}"]//g' -e 's/, */./g' -e 's/ //g'
}

# resolve_declared <пути через \n> <файл значений>… — значения по путям в
# СЛИТОМ дереве значений, как их сольёт helm (карты сливаются ключ за ключом,
# всё прочее замещается целиком).
resolve_declared() {
  local paths="$1"; shift
  printf '%s' "$paths" | python3 -c '
import sys, yaml

def merge(dst, src):
    for k, v in (src or {}).items():
        if isinstance(v, dict) and isinstance(dst.get(k), dict):
            merge(dst[k], v)
        else:
            dst[k] = v
    return dst

paths = [l.strip() for l in sys.stdin if l.strip()]
merged = {}
for f in sys.argv[1:]:
    with open(f) as fh:
        merge(merged, yaml.safe_load(fh) or {})
for p in paths:
    cur = merged
    for key in p.split("."):
        if not isinstance(cur, dict) or key not in cur:
            cur = None
            break
        cur = cur[key]
    if isinstance(cur, str) and cur.strip():
        print(cur.strip())
' "$@"
}

# ── СТЕКИ ────────────────────────────────────────────────────────────────────
#
# Цепочки `-f` НЕ выписаны здесь: их объявляет deploy/stacks.txt, и это
# единственное такое место в дереве. Здесь была своя копия, и копии разъехались
# — о среднем слое накладки образов эта таблица и Go-таблица шлюза утверждали
# ВЗАИМОИСКЛЮЧАЮЩЕЕ, обе оставаясь зелёными. Единственность держит гейт
# TestNoSecondCopyOfAStackChain, а не дисциплина правки.
STACKS="$(stacks_table)"

# ─────────────────────────────────────────────────────────────────────────────
# КЛАССИФИКАЦИЯ ОДНОГО РЕНДЕРА — ЧИСТАЯ ФУНКЦИЯ НАД ТЕКСТОМ.
#
# Вынесена затем, чтобы самопроверка могла скормить ей синтетический рендер БЕЗ
# кластера и без helm, и потребовать покраснеть на каждом внесённом дефекте и
# ПРОМОЛЧАТЬ на законной конструкции той же формы.
#
# Аргументы: <файл-рендера> <имя стека> <файл со списком объявленных адресов>
# Печатает находки, возвращает число находок кодом.
# ─────────────────────────────────────────────────────────────────────────────
census_render() {
  local render="$1" stack="$2" declared_file="$3"
  local findings=0
  local docdir; docdir="$(mktemp -d)"

  # РАЗВЁРНУТ ЛИ ТЕРМИНАТОР В ЭТОМ СТЕКЕ. От этого зависит, чем является прямое
  # обращение к провайдеру: находкой или объявленным dev-классом.
  #
  # Стек, где терминатора нет, адресует административный листенер провайдера
  # НАПРЯМУЮ И ПО ОТКРЫТОМУ ТЕКСТУ — это остаётся ВИДНЫМ (класс называется и
  # считается ниже), но решает этот вопрос не перепись: транспорт на таких
  # стеках гейтит освобождение dev-класса в реестре объявлений, и там оно
  # выведено из САМОЗАЯВЛЕНИЯ стека, а не из списка имён.
  local terminator_deployed=0
  grep -q 'name: .*-hydra-admin-tls' "$render" 2>/dev/null && terminator_deployed=1

  # Рендер режется на документы. Классификация — ПОДОКУМЕНТНО: адрес имеет
  # смысл только вместе с нагрузкой, которая его несёт.
  awk -v dir="$docdir" '
    BEGIN { n = 0; f = sprintf("%s/doc-%05d", dir, n) }
    /^---[[:space:]]*$/ { n++; f = sprintf("%s/doc-%05d", dir, n); next }
    { print > f }
  ' "$render"

  local docs=0 with_subject=0 doc kind name addrs hook
  local -a unaccounted=() direct=() noprobe=() testhook=()

  for doc in "$docdir"/doc-*; do
    [ -f "$doc" ] || continue
    docs=$((docs + 1))
    addrs="$(extract_addresses "$doc")"
    [ -z "$addrs" ] && continue
    with_subject=$((with_subject + 1))

    kind="$(grep -m1 -oP '^kind:[[:space:]]*\K\S+' "$doc" 2>/dev/null)"
    name="$(grep -m1 -oP '^[[:space:]]{2}name:[[:space:]]*\K\S+' "$doc" 2>/dev/null | tr -d '"')"
    [ -z "$kind" ] && kind='?'
    [ -z "$name" ] && name='?'
    hook="$(grep -m1 -oP '"?helm\.sh/hook"?:[[:space:]]*\K\S+' "$doc" 2>/dev/null | tr -d '"')"

    case "$kind" in
      # Объекты, чей ПРЕДМЕТ — сам адрес: Service терминатора, его ConfigMap,
      # сертификат. Они адрес не потребляют, а определяют.
      Service|ConfigMap|Certificate|Secret|Endpoints|EndpointSlice) continue ;;
    esac

    # Тест-подключение чарта провайдера: Pod с хуком `test-*`. Он НЕ
    # разворачивается установкой/обновлением — только явным `helm test`, которого
    # ни одна цель дерева не запускает; его отказ виден кодом возврата helm.
    # Класс назван и посчитан ОТДЕЛЬНО, а не свален в общую находку: чарт
    # провайдера не вендорён, править его шаблон нечем, и молчаливым этот отказ
    # не будет.
    if [ "$kind" = Pod ] && case "$hook" in test-*) true ;; *) false ;; esac; then
      testhook+=("$kind/$name")
      continue
    fi

    local a
    while IFS= read -r a; do
      [ -z "$a" ] && continue
      if provider_spelling "$a"; then
        # A. адресует ПРОВАЙДЕРА напрямую
        direct+=("$kind/$name → $a")
        continue
      fi
      # B. адресует терминатор — обязан быть объявлен записью реестра
      if ! grep -qxF "$a" "$declared_file" 2>/dev/null; then
        unaccounted+=("$kind/$name → $a")
      fi
    done <<<"$addrs"

    # C. долгоживущая нагрузка обязана иметь пробу
    case "$kind" in
      Deployment|StatefulSet|DaemonSet)
        if ! grep -qE '(readiness|liveness|startup)Probe:' "$doc"; then
          noprobe+=("$kind/$name")
        fi
        ;;
    esac
  done
  rm -rf "$docdir"

  echo "  [$stack] документов рендера: $docs, из них называют адрес: $with_subject"
  note "объявлено записями реестра адресов: $(grep -c . "$declared_file" 2>/dev/null | head -1)"

  if [ "${#direct[@]}" -gt 0 ]; then
    if [ "$terminator_deployed" -eq 1 ]; then
      violation "[$stack] терминатор развёрнут, а нагрузка адресует листенер ПРОВАЙДЕРА напрямую (${#direct[@]}):"
      printf '        %s\n' "${direct[@]}"
      note "открытый участок между подами обязан отсутствовать: листенер слушает петлю,"
      note "его Service снят, потребители адресуют терминатор. Этот потребитель отстал —"
      note "ровно тот класс, который тих до первого исполнения его потока."
      findings=$((findings + ${#direct[@]}))
    else
      note "класс «терминатора в стеке нет» (${#direct[@]}): адресуют провайдера напрямую,"
      note "  по открытому тексту — ${direct[*]}"
      note "  Стек объявляет себя dev-классом; транспорт на нём гейтит освобождение"
      note "  dev-класса в реестре объявлений (оно выведено из самозаявления стека,"
      note "  а не из списка имён). Здесь это НАЗВАНО и ПОСЧИТАНО, но находкой не"
      note "  считается: предмет переписи — потребитель, отставший от ПЕРЕВЕДЁННОГО"
      note "  перехода, а на этом стеке переход не переводился."
    fi
  fi
  if [ "${#unaccounted[@]}" -gt 0 ]; then
    violation "[$stack] адрес в рендере, которого НЕ ОБЪЯВЛЯЕТ ни одна запись реестра (${#unaccounted[@]}):"
    printf '        %s\n' "${unaccounted[@]}"
    note "это потребитель, невидимый реестру по объявлениям — тот самый класс, ради"
    note "которого перепись существует. Сделать адрес объявляемым значением и внести в реестр."
    findings=$((findings + ${#unaccounted[@]}))
  fi
  if [ "${#noprobe[@]}" -gt 0 ]; then
    violation "[$stack] долгоживущая нагрузка называет адрес и НЕ ИМЕЕТ ни одной пробы (${#noprobe[@]}):"
    printf '        %s\n' "${noprobe[@]}"
    note "при смене адреса она не покраснеет, а тихо перестанет разрешать имя."
    note "Молчаливое прекращение работы хуже отказа: либо проба, либо снять нагрузку."
    findings=$((findings + ${#noprobe[@]}))
  fi
  if [ "${#testhook[@]}" -gt 0 ]; then
    note "класс «тест-подключение чарта» (${#testhook[@]}): ${testhook[*]}"
    note "  не разворачивается install/upgrade (хук test-*), запускается только явным"
    note "  \`helm test\`; отказ виден кодом возврата. Чарт провайдера не вендорён."
  fi
  return "$findings"
}

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА: инъекция в обе стороны, БЕЗ кластера и БЕЗ helm.
# ─────────────────────────────────────────────────────────────────────────────
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: классификатор переписи против синтетических рендеров ==="
  st_rc=0
  st_checked=0
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

  printf '%s\n' 'https://rel-hydra-admin-tls.ns.svc.cluster.local:4445' >"$tmp/declared.txt"

  # Ожидание задаётся ЧИСЛОМ находок: «покраснел» без указания на сколько
  # неотличимо от «покраснел не на том».
  probe() { # <имя> <ожидаемое число находок> <файл рендера>
    local label="$1" want="$2" file="$3" got out
    st_checked=$((st_checked + 1))
    out="$(census_render "$file" self "$tmp/declared.txt" 2>&1)"; got=$?
    if [ "$got" -eq "$want" ]; then
      echo "  ✓ $label → находок $got, как ожидалось"
    else
      echo "  ✗ $label → находок $got, ожидалось $want"
      printf '%s\n' "$out" | sed 's/^/      /'
      st_rc=1
    fi
  }

  # (1) ЗАКОННАЯ конструкция той же формы — обязано МОЛЧАТЬ. Без этой половины
  #     гейт ловит форму, а не существо, и первый ложный срабат его отключит.
  cat >"$tmp/legit.yaml" <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer
spec:
  template:
    spec:
      containers:
        - name: c
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
          env:
            - name: HYDRA_ADMIN_URL
              value: "https://rel-hydra-admin-tls.ns.svc.cluster.local:4445"
---
apiVersion: v1
kind: Service
metadata:
  name: rel-hydra-admin-tls
spec:
  ports: [{ port: 4445, targetPort: 4445 }]
EOF
  probe "законный потребитель терминатора + Service терминатора" 0 "$tmp/legit.yaml"

  # (2) ссылка на СЕКРЕТ с тем же именем — не адрес, обязано молчать.
  cat >"$tmp/secretref.yaml" <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer
spec:
  template:
    spec:
      volumes:
        - name: anchor
          secret: { secretName: rel-hydra-admin-tls }
      containers:
        - name: c
          livenessProbe:
            httpGet: { path: /healthz, port: 8080 }
EOF
  probe "ссылка на секрет того же имени (не адрес)" 0 "$tmp/secretref.yaml"

  # (2а) АДРЕС ТОЛЬКО В КОММЕНТАРИИ — обязано молчать. Это не гипотетика:
  #      первая редакция этой проверки объявила потребителем комментарий в шаблоне
  #      iam, который объясняет, почему выведенный адрес непригоден. Инъекция
  #      закрепляет, что читается исполняемая часть, а не текст.
  cat >"$tmp/comment.yaml" <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer
spec:
  template:
    spec:
      containers:
        - name: c
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
          env:
            # Пусто → выводится `https://hydra-admin.<domain>` из эмитента,
            # который в кластере НЕ РАЗРЕШАЕТСЯ. Профиль обязан назвать адрес.
            - name: HYDRA_ADMIN_URL
              value: "https://rel-hydra-admin-tls.ns.svc.cluster.local:4445"
EOF
  probe "адрес назван только в комментарии (читается исполняемая часть)" 0 "$tmp/comment.yaml"

  # (3) ТЕРМИНАТОР РАЗВЁРНУТ, а нагрузка адресует провайдера напрямую — это
  #     отставший потребитель, обязано покраснеть.
  cat >"$tmp/direct.yaml" <<'EOF'
apiVersion: v1
kind: Service
metadata:
  name: rel-hydra-admin-tls
spec:
  ports: [{ port: 4445, targetPort: 4445 }]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: leftover
spec:
  template:
    spec:
      containers:
        - name: c
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
          args:
            - --hydra-url=http://rel-hydra-admin
EOF
  probe "терминатор развёрнут + потребитель отстал на провайдере" 1 "$tmp/direct.yaml"

  # (3а) ТЕРМИНАТОРА В СТЕКЕ НЕТ — тот же текст находкой НЕ является: переход на
  #      этом стеке не переводился, и предмета у утверждения нет. Без этой
  #      половины гейт краснел бы на стеках, которых работа не касалась, и был бы
  #      ослаблен целиком при первом же таком срабатывании.
  cat >"$tmp/direct-noterm.yaml" <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: leftover
spec:
  template:
    spec:
      containers:
        - name: c
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
          args:
            - --hydra-url=http://rel-hydra-admin
EOF
  probe "терминатора в стеке нет — прямой адрес не находка" 0 "$tmp/direct-noterm.yaml"
  st_checked=$((st_checked + 1))
  # ВЫВОД БЕРЁТСЯ ПОДСТАНОВКОЙ, А НЕ КОНВЕЙЕРОМ В grep -q. С `pipefail` конвейер
  # `… | grep -q` возвращает ОТКАЗ НА СОВПАДЕНИИ: grep выходит по первому
  # попаданию, писатель слева получает SIGPIPE (141), и статус берётся от него.
  # Зависит от объёма вывода, поэтому проявляется через раз — эта самопроверка
  # так и падала: в одном прогоне зелёная, в другом красная на верном поведении.
  # Тот же класс уже описан у прогонщика самопроверок; здесь он повторён.
  out_noterm="$(census_render "$tmp/direct-noterm.yaml" self "$tmp/declared.txt" 2>&1)"
  if grep -q 'терминатора в стеке нет' <<<"$out_noterm"; then
    good "класс назван и посчитан, а не умолчан"
  else
    echo "  ✗ класс «терминатора в стеке нет» не назван — «не находка» стало «не видно»"; st_rc=1
  fi

  # (4) адрес терминатора, которого реестр НЕ объявляет — обязано покраснеть.
  cat >"$tmp/unaccounted.yaml" <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: invisible
spec:
  template:
    spec:
      containers:
        - name: c
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
          env:
            - name: HYDRA_ADMIN_URL
              value: "https://rel-hydra-admin-tls.other.svc.cluster.local:4445"
EOF
  probe "адрес терминатора вне объявлений реестра" 1 "$tmp/unaccounted.yaml"

  # (5) долгоживущая нагрузка БЕЗ проб — обязано покраснеть (и это ОТДЕЛЬНАЯ
  #     находка от «вне реестра»: адрес объявлен, а наблюдаемости нет).
  cat >"$tmp/noprobe.yaml" <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: silent
spec:
  template:
    spec:
      containers:
        - name: c
          env:
            - name: HYDRA_ADMIN_URL
              value: "https://rel-hydra-admin-tls.ns.svc.cluster.local:4445"
EOF
  probe "долгоживущая нагрузка без проб" 1 "$tmp/noprobe.yaml"

  # (6) тест-подключение чарта — НЕ находка, но обязано быть НАЗВАНО в выводе.
  cat >"$tmp/testhook.yaml" <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: rel-hydra-test-connection
  annotations:
    "helm.sh/hook": test-success
spec:
  containers:
    - name: healthcheck-ready
      args: ['rel-hydra-admin:4445/health/ready']
EOF
  st_checked=$((st_checked + 1))
  out6="$(census_render "$tmp/testhook.yaml" self "$tmp/declared.txt" 2>&1)"; rc6=$?
  if [ "$rc6" -eq 0 ] && grep -q 'тест-подключение чарта' <<<"$out6"; then
    echo "  ✓ тест-подключение чарта: не находка, но класс назван в выводе"
  else
    echo "  ✗ тест-подключение чарта: rc=$rc6, класс в выводе не назван"
    printf '%s\n' "$out6" | sed 's/^/      /'; st_rc=1
  fi

  # (7) ПУСТОЙ рендер обязан быть отличим от чистого: ноль документов — это не
  #     «ноль находок», это «ничего не прочитано».
  : >"$tmp/empty.yaml"
  st_checked=$((st_checked + 1))
  out7="$(census_render "$tmp/empty.yaml" self "$tmp/declared.txt" 2>&1)"
  if grep -qE 'называют адрес: 0' <<<"$out7"; then
    echo "  ✓ пустой рендер: объём осмотренного напечатан числом (0), а не умолчанием"
  else
    echo "  ✗ пустой рендер: объём осмотренного не напечатан"
    printf '%s\n' "$out7" | sed 's/^/      /'; st_rc=1
  fi

  # (8) ПРЕДПОСЫЛКА САМОЙ ПРОВЕРКИ: реестр обязан читаться из своего файла. Если
  #     литерал переименован/удалён — требование парности потеряло предмет, и
  #     молчать об этом нельзя.
  st_checked=$((st_checked + 1))
  if [ "$(registry_paths | grep -c . )" -ge 1 ]; then
    echo "  ✓ предпосылка: реестр читается из $(basename "$REGISTRY_GO") ($(registry_paths | grep -c . ) путей)"
  else
    echo "  ✗ предпосылка не держится: литерал adminHopConsumers не разобран —"
    echo "    парность «реестр ↔ обход» проверять НЕЧЕМ, и молчать об этом нельзя"
    st_rc=1
  fi

  # (9) ПОЛОВИНА 1 — ОТБРАСЫВАНИЕ КОММЕНТАРИЯ ЗАВИСИТ ОТ ЯЗЫКА, а координата
  #     обязана указывать на строку ИСХОДНОГО файла.
  #
  #     Половина 1 обходит и `*.go` (services/*/deploy/*), где комментарий
  #     начинается с `//`, а не с `#`. Пока взгляд отбрасывал только `#`, объяснение
  #     в Go-файле — «почему этот адрес непригоден» — читалось как ПОТРЕБИТЕЛЬ:
  #     ровно тот класс, ради которого написана инъекция (2а) выше, только на другом
  #     языке. Вторая половина того же дефекта — координата: комментарии УДАЛЯЛИСЬ,
  #     поэтому номер строки считался по укороченному потоку и на исходный файл не
  #     указывал (наблюдалось: :44 при настоящей :129).
  tree_probe() { # <файл> → строки-находки половины 1, как их видит основной проход
    uncommented "$1" 2>/dev/null \
      | grep -nP "$SUBJECT_RE" 2>/dev/null \
      | grep -P "$ADDRESS_LINE_RE" 2>/dev/null
  }
  { echo 'package deploy'
    echo '// объяснение: https://rel-hydra-admin-tls.ns.svc.cluster.local:4445 непригоден'
    echo 'const x = 1'; } >"$tmp/comment_only.go"
  st_checked=$((st_checked + 1))
  if [ -z "$(tree_probe "$tmp/comment_only.go")" ]; then
    echo "  ✓ Go-комментарий с адресом потребителем НЕ признан (читается исполняемая часть)"
  else
    echo "  ✗ Go-комментарий принят за потребителя — взгляд отбрасывает только '#'"
    tree_probe "$tmp/comment_only.go" | sed 's/^/      /'; st_rc=1
  fi

  { echo 'package deploy'
    echo '// шапка'
    echo '// ещё строка шапки'
    echo 'const admin = "https://rel-hydra-admin-tls.ns.svc.cluster.local:4445"'; } \
    >"$tmp/exec_go.go"
  st_checked=$((st_checked + 1))
  got_line="$(tree_probe "$tmp/exec_go.go" | head -1 | cut -d: -f1)"
  if [ "$got_line" = "4" ]; then
    echo "  ✓ исполняемая строка Go найдена, и координата указывает на строку 4 исходного файла"
  else
    echo "  ✗ координата половины 1 не совпадает с исходным файлом: получено '$got_line', в файле строка 4"
    st_rc=1
  fi

  { echo '#!/usr/bin/env bash'
    echo '# объяснение: https://rel-hydra-admin-tls.ns.svc.cluster.local:4445 непригоден'
    echo 'echo ok'; } >"$tmp/comment_only.sh"
  st_checked=$((st_checked + 1))
  if [ -z "$(tree_probe "$tmp/comment_only.sh")" ]; then
    echo "  ✓ shell-комментарий с адресом потребителем НЕ признан (контроль прежней формы)"
  else
    echo "  ✗ shell-комментарий принят за потребителя — регресс отбрасывания '#'"
    st_rc=1
  fi

  { echo '#!/usr/bin/env bash'
    echo '# шапка'
    echo '# ещё строка шапки'
    echo 'kubectl port-forward svc/rel-hydra-admin-tls 4445:4445'; } >"$tmp/exec_sh.sh"
  st_checked=$((st_checked + 1))
  got_line="$(tree_probe "$tmp/exec_sh.sh" | head -1 | cut -d: -f1)"
  if [ "$got_line" = "4" ]; then
    echo "  ✓ исполняемая строка shell найдена, координата указывает на строку 4 исходного файла"
  else
    echo "  ✗ координата половины 1 сдвинута удалением комментариев: получено '$got_line', в файле строка 4"
    st_rc=1
  fi

  echo
  echo "инъекций и законных входов проверено: $st_checked"
  [ $st_rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $st_rc
fi

# ─────────────────────────────────────────────────────────────────────────────
# ОСНОВНОЙ ПРОХОД
# ─────────────────────────────────────────────────────────────────────────────
require_helm

# ЗНАЧЕНИЯ ЧИТАЮТСЯ PyYAML, А НЕ `yq` — ОСОЗНАННО.
#
# В PATH под именем `yq` бывают две разные программы: mikefarah v4 и
# python-обёртка над jq. На фильтрах вида `.["a"]["b"]` вторая не отдаёт ошибку —
# она отдаёт ПУСТО. Первая редакция этой проверки на такой машине резолвила НОЛЬ
# объявленных адресов и печатала «объявлено записями реестра: 0», после чего
# утверждение о парности «реестр ↔ обход» становилось вакуумным, оставаясь на вид
# исполненным. Соседний прогонщик самопроверок ровно эту ловушку описывает и
# отказывается работать на неверном `yq`.
#
# Здесь зависимость просто снята: PyYAML разбирает те же файлы однозначно, а
# «инструмент молча вернул пусто» — тот самый класс, ради которого эта проверка
# написана. Гейту не пристало быть его экземпляром.
require_python_yaml

# ПРЕДУСЛОВИЕ: чарт провайдера материализован. Иначе обход НЕ ВЫПОЛНЕН, а не чист:
# без него рендер не содержит ни реконсайлера клиентов, ни тест-подключения, и
# обход нашёл бы ЧАСТЬ потребителей, отчитавшись полнотой.
require_dep_chart "$UMBRELLA" hydra

echo "=== $SCRIPT: перепись потребителей административного адреса ==="
echo
echo "── область обхода (объявлена ЯВНО; печатается то, что реально обошли) ──"
tree_n="$(tree_files | wc -l)"
excl_n="$(excluded_files | wc -l)"
excl_hits="$(excluded_files | xargs grep -lP "$SUBJECT_RE" 2>/dev/null | wc -l)"
echo "  пройдено поддеревьев: $WALK_PREFIXES"
echo "    файлов прочитано: $tree_n"
echo "  ИСКЛЮЧЕНО: остальное дерево ($excl_n файлов, из них называют предмет: $excl_hits)"
echo "    причина: $EXCLUDE_REASON"
echo "  чарты-зависимости лежат АРХИВАМИ и текстом не читаются —"
echo "    их содержимое покрывает половина по рендеру ниже (иначе двое из семи"
echo "    потребителей были бы невидимы обеим половинам)"
echo

# ── половина 1: текстовый обход дерева ───────────────────────────────────────
echo "── половина 1: обход дерева (объявления и зашитые в шаблон адреса) ──"
# Тот же комментарий-отбрасывающий взгляд (`uncommented`), что и в извлечении
# адресов, и тот же признак адреса на уровне строки (`ADDRESS_LINE_RE`) — оба
# объявлены ОДИН раз наверху, вместе с предметом обхода, и оттуда же их берёт
# инъекция самопроверки. Пересказ здесь был бы копией, которая разойдётся.
tree_hits="$(while IFS= read -r f; do
  uncommented "$REPO_ROOT/$f" 2>/dev/null \
    | grep -nP "$SUBJECT_RE" 2>/dev/null \
    | grep -P "$ADDRESS_LINE_RE" 2>/dev/null \
    | sed "s|^|$f:|"
done <<<"$(tree_files)")"
tree_files_hit="$(printf '%s\n' "$tree_hits" | grep -c . )"
echo "  строк с предметом: $tree_files_hit"

allowed_paths=""
for entry in "${ALLOWED_TREE[@]}"; do allowed_paths="$allowed_paths ${entry%%|*}"; done

# Находка: файл в пройденном поддереве, называющий предмет, не входящий в
# ALLOWED_TREE и не являющийся файлом ЗНАЧЕНИЙ (значения — законное место
# объявления потребителя; их согласованность проверяют гейты транспорта и
# половина 2 по рендеру).
assertion
unexpected=""
while IFS= read -r f; do
  [ -z "$f" ] && continue
  case " $allowed_paths " in *" $f "*) continue ;; esac
  case "$f" in
    */values*.yaml|*/values.yaml) continue ;;   # объявление потребителя — законно
  esac
  unexpected="$unexpected$f"$'\n'
done <<<"$(printf '%s\n' "$tree_hits" | sed 's/:.*//' | sort -u)"
unexpected="$(printf '%s' "$unexpected" | grep -c . )"
if [ "$unexpected" -eq 0 ]; then
  good "адрес в дереве называют только объявления значений и файлы, чей предмет — сам адрес"
else
  violation "файлы вне списка законных упоминаний называют адрес ($unexpected):"
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    case " $allowed_paths " in *" $f "*) continue ;; esac
    case "$f" in */values*.yaml|*/values.yaml) continue ;; esac
    printf '        %s\n' "$f"
    printf '%s\n' "$tree_hits" | grep "^$f:" | sed 's/^/          /'
  done <<<"$(printf '%s\n' "$tree_hits" | sed 's/:.*//' | sort -u)"
  note "адрес, зашитый в шаблон, реестру по объявлениям НЕ ВИДЕН — сделать его"
  note "объявляемым значением и внести путь в реестр."
fi

# D. истечение: запись списка законных упоминаний без предмета — находка.
assertion
expired=""
for entry in "${ALLOWED_TREE[@]}"; do
  p="${entry%%|*}"
  if [ ! -f "$REPO_ROOT/$p" ] || ! grep -qP "$SUBJECT_RE" "$REPO_ROOT/$p" 2>/dev/null; then
    expired="$expired  $p — ${entry#*|}"$'\n'
  fi
done
if [ -z "$expired" ]; then
  good "все ${#ALLOWED_TREE[@]} записи списка законных упоминаний имеют предмет"
else
  violation "запись списка законных упоминаний, под которую в дереве НЕЧЕГО исключать:"
  printf '%s' "$expired" | sed 's/^/      /'
  note "исключение живёт, пока у него есть предмет: запись без предмета унаследует"
  note "следующую слепую зону. Удалить запись либо вернуть предмет."
fi
echo

# ── половина 2: обход рендеров (авторитетна для того, что реально разворачивается)
echo "── половина 2: обход рендеров (умолчания зависимостей и аргументы контейнеров) ──"
reg_paths="$(registry_paths)"
reg_n="$(printf '%s\n' "$reg_paths" | grep -c . )"
assertion
if [ "$reg_n" -lt 1 ]; then
  violation "литерал adminHopConsumers не разобран — парность «реестр ↔ обход» проверять нечем"
else
  good "реестр прочитан из $(basename "$REGISTRY_GO"): $reg_n путей объявлений"
fi

work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
while IFS= read -r line; do
  [ -z "$line" ] && continue
  stack="${line%%:*}"; files="${line#*:}"
  args=""; IFS=','; for f in $files; do args="$args -f $UMBRELLA/$f"; done; unset IFS

  valuefiles=""; IFS=','; for f in $files; do valuefiles="$valuefiles $UMBRELLA/$f"; done; unset IFS

  render="$work/$stack.yaml"
  # Отказ рендера — УСЛОВИЕ прогона, а не свойство дерева: код 2 и текст, который
  # сказал сам helm. Сломанный шаблон при этом не прячется — его ловит шаг
  # «umbrella template — каждый стек таблицы», идущий в той же джобе РАНЬШЕ этого.
  # Чарт назван путём, а не через `cd … .`: HELM_OUT/HELM_RC из подоболочки наружу
  # не выходят, а аргументы `-f` здесь абсолютные.
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" $args --namespace kacho
  render_or_fatal "стек $stack"
  printf '%s\n' "$HELM_OUT" >"$render"
  # shellcheck disable=SC2086
  # Адреса, ОБЪЯВЛЕННЫЕ записями реестра в этом стеке. Объявление сводится к
  # тому же виду, в каком адрес извлекается из рендера: путь интроспекции
  # (`…:4445/admin/oauth2/introspect`) — тот же host:port, что и голый адрес,
  # поэтому сравнивается host:port, а не строка целиком.
  raw="$work/$stack.declared.raw"
  declared="$work/$stack.declared.txt"
  # shellcheck disable=SC2086
  resolve_declared "$reg_paths" $valuefiles >"$raw" 2>"$work/$stack.resolve.err" || {
    violation "[$stack] объявления потребителей не разобраны — парность проверять НЕЧЕМ"
    sed 's/^/        /' "$work/$stack.resolve.err" | tail -3
  }
  extract_addresses "$raw" >"$declared" 2>/dev/null || : >"$declared"

  assertion
  census_render "$render" "$stack" "$declared" || true
done <<<"$STACKS"

echo
echo "=== вердикт: утверждений $ASSERTIONS, находок $VIOLATIONS ==="
findings_verdict "стеков осмотрено: $ASSERTIONS"
