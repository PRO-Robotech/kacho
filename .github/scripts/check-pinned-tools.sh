#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# check-pinned-tools.sh — сверяет версии инструментов, ЗАПИНЕННЫЕ ВНУТРИ шагов
# workflow: (а) между собой, если пин встречается больше одного раза, и (б) с
# последними релизами апстрима.
#
# Зачем отдельно от dependabot: он обновляет `uses: owner/action@v4`, go.mod,
# package.json и Dockerfile-FROM, но НЕ видит версию, заданную входом шага
# (`with:` → `version:`), командой (`go install …@v2.12.2`) или URL'ом в curl
# (kind, grpcurl). Мы пиним именно так — осознанно, ради local == CI: незапиненный
# setup-helm однажды притащил Helm 4, и helm-гейт зеленел на версии, которой нет на
# проде. Без сторожа такой пин протухает МОЛЧА (trivy со старой базой CVE, gosec без
# новых правил).
#
# ДВЕ НАХОДКИ, А НЕ ОДНА. Отставание от апстрима — не единственный способ, которым
# пин перестаёт значить то, что обещает:
#
#   • ОТСТАВАНИЕ — пин один, но старее апстрима;
#   • РАСХОЖДЕНИЕ — пин один и тот же инструмент, а значений два. Тогда конвейеры
#     (или конвейер и разработчик локально) исполняют РАЗНЫЕ версии, и вердикты о
#     том же дереве становятся несравнимыми — ровно то, ради чего пин и заводился.
#     Прежняя редакция сторожа читала каждый пин из ОДНОГО названного файла, поэтому
#     второе и последующие вхождения были для неё невидимы: расхождение выглядело бы
#     как «всё свежее», а одна из двух версий выдавалась за истину.
#
# Расхождение проверяется ПО ВСЕМУ ДОМЕНУ и для КАЖДОГО инструмента — это свойство
# сторожа, а не заплатка под конкретный инструмент: пин, встречающийся в N файлах,
# сверяется по всем N.
#
# Печатает markdown-строки по каждой находке в stdout; пусто = всё свежее и сходится.
# Ничего не обновляет: решение и PR — за человеком.
#
# Запуск локально:  bash .github/scripts/check-pinned-tools.sh
#   только расхождение, БЕЗ сети:  bash .github/scripts/check-pinned-tools.sh --divergence-only
#   доказательство инъекцией:      bash .github/scripts/check-pinned-tools.sh --self-test
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT" || exit 2

# ── КАТАЛОГ ПИНОВ ────────────────────────────────────────────────────────────
#
# Строка — один ЗОНД: <инструмент>§<репозиторий апстрима>§<анкер ERE или пусто>§<ERE пина>
#
# РАЗДЕЛИТЕЛЬ — `§` (U+00A7), а не `|`, и это не вкусовщина: поля здесь суть ERE, а
# `|` в ERE означает alternation. Первый же зонд с альтернативой распался бы на два
# обрезанных поля — то есть предикат стал бы искать не то, что в нём написано, и
# сообщил бы «пин не найден» вместо ошибки разбора. `§` в ERE не значит ничего и в
# путях/URL не встречается.
#
# У инструмента может быть НЕСКОЛЬКО зондов, и находки всех его зондов сверяются
# между собой как ОДНО множество. Это не симметрия ради симметрии: grpcurl запинен
# дважды в одной строке — в пути релиза и в имени архива, — и рассинхрон этих двух
# даёт 404 на скачивании, то есть шаг падает ДО первой коллекции.
#
# АНКЕР — шаг, которому принадлежит пин. Вход `version:` сам по себе не различает
# setup-helm и buf-setup-action: у обоих он называется одинаково. Совпадение «у helm
# значение с ведущей v, у buf без» — особенность формата их релизов, а не признак
# принадлежности; предикат, опирающийся на неё, ответил бы верно только пока это
# случайное различие держится.
S='§'
PROBES=(
  "helm${S}helm/helm${S}uses:[[:space:]]*azure/setup-helm@${S}version:[[:space:]]*v?[0-9]+\.[0-9]+\.[0-9]+"
  "buf${S}bufbuild/buf${S}uses:[[:space:]]*bufbuild/buf-setup-action@${S}version:[[:space:]]*v?[0-9]+\.[0-9]+\.[0-9]+"
  "golangci-lint${S}golangci/golangci-lint${S}${S}golangci-lint@v?[0-9]+\.[0-9]+\.[0-9]+"
  "kind${S}kubernetes-sigs/kind${S}${S}kind\.sigs\.k8s\.io/dl/v?[0-9]+\.[0-9]+\.[0-9]+/"
  "grpcurl${S}fullstorydev/grpcurl${S}${S}grpcurl/releases/download/v?[0-9]+\.[0-9]+\.[0-9]+/"
  "grpcurl${S}fullstorydev/grpcurl${S}${S}grpcurl_[0-9]+\.[0-9]+\.[0-9]+_linux"
  "gosec${S}securego/gosec${S}${S}gosec@v?[0-9]+\.[0-9]+\.[0-9]+"
)

# HOLD — пины, которые мы держим ОСОЗНАННО. Сторож обязан молчать об их ОТСТАВАНИИ,
# иначе issue шумит вечно и его перестают читать. Каждая запись — с тикетом;
# снимается вместе с ним. Формат: <name>=<причина>.
#
# HOLD НЕ ГЛУШИТ РАСХОЖДЕНИЕ, и это не оговорка на всякий случай. «Держим старую
# версию осознанно» и «исполняем две разные версии» — разные утверждения: второе
# не бывает осознанным, потому что осознанным может быть только одно значение.
#
# СЕЙЧАС СПИСОК ПУСТ, и это утверждение, а не отсутствие данных: ни один пин не
# удерживается осознанно, поэтому отставание от апстрима по КАЖДОМУ инструменту
# читается как находка. Единственная запись, которая здесь стояла, — helm: он
# держался на прошлой мажорной, пока у аннотации шаблона пода
# `kacho.cloud/openfga-model-id-rev` было два писателя и server-side apply Helm 4
# ронял обновление релиза конфликтом владения (kacho#3). Второй писатель снят,
# конвейеры переведены на Helm 4 — запись ушла вместе со своим предметом, ровно
# как обещало её условие снятия.
declare -A HOLD=()

# ── ДОМЕН ПЕРЕПИСИ ───────────────────────────────────────────────────────────
#
# Пин ИСПОЛНЯЕТСЯ только из определений конвейера, поэтому домен — отслеживаемые
# `.github/workflows/*.y{a,}ml`. Проверено переписью дерева: тулчейны не ставит ни
# `.github/scripts` (там нет ни одного `curl`/`go install` за инструментом), ни
# `deploy/` — `make dev-up` берёт kind и helm из PATH, а не скачивает их.
#
# Единица счёта — ОТСЛЕЖИВАЕМЫЙ git-элемент: объявление, `.gitignore` и поведение не
# могут разъехаться молча. В синтетическом дереве самопроверки `git ls-files` не
# работает — там обход файловой системы; для репозитория авторитет у версионного
# контроля. Тот же приём и по той же причине — в deploy/scripts/run-gate-self-tests.sh.
pin_domain() {
  local root="$1"
  if [ "$root" = "$REPO_ROOT" ] && git -C "$root" rev-parse --git-dir >/dev/null 2>&1; then
    git -C "$root" ls-files '.github/workflows/*.yml' '.github/workflows/*.yaml'
  else
    ( cd "$root" 2>/dev/null && find .github/workflows -type f \
        \( -name '*.yml' -o -name '*.yaml' \) 2>/dev/null | sed 's#^\./##' )
  fi | LC_ALL=C sort
}

# ── ПЕРЕПИСЬ ОДНОГО ЗОНДА ────────────────────────────────────────────────────
#
# census <анкер|""> <ERE пина> <корень> → строки «<файл>:<строка>:<версия>».
#
# ЧИТАЕТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ. Комментарии отбрасываются до поиска, и
# это несущее требование, а не аккуратность: в этих же workflow'ах пин НАЗЫВАЕТСЯ
# прозой («Пин 1.72.0 — тот же, что у job'ов proto и каталога», «setup-helm без
# version ставит Helm 4»), да и шапка этого файла раньше цитировала `version:
# v3.17.0`. Без отбрасывания первый же подъём версии превратил бы каждое такое
# объяснение в мнимое расхождение — и сторожа сняли бы как шумный.
#
# Версия — первая тройка `N.N.N` внутри совпадения: у awk нет групп захвата, а все
# зонды каталога несут ровно одну тройку. Ведущая `v` отпадает by construction.
#
# ERE передаются через ОКРУЖЕНИЕ, а не через `awk -v`: -v обрабатывает
# escape-последовательности, и `\.` доехал бы до awk уже без обратной косой — то
# есть точка стала бы «любым символом», а предикат — шире объявленного.
census() {
  local anchor="$1" pin="$2" root="${3:-$REPO_ROOT}" f
  while IFS= read -r f; do
    [ -f "$root/$f" ] || continue
    ANCHOR="$anchor" PIN="$pin" FILE="$f" awk '
      BEGIN { anchor = ENVIRON["ANCHOR"]; pin = ENVIRON["PIN"]; live = (anchor == "") }
      {
        line = $0
        if (line ~ /^[[:space:]]*#/) next                    # строка-комментарий целиком
        sub(/[[:space:]]#[[:space:]].*$/, "", line)          # хвостовой комментарий
        # Новый элемент списка шагов ЗАКРЫВАЕТ предыдущий анкер: иначе `version:`
        # соседнего шага достался бы предыдущему инструменту.
        if (anchor != "" && line ~ /^[[:space:]]*-[[:space:]]/) live = (line ~ anchor)
        if (!live) next
        if (match(line, pin)) {
          m = substr(line, RSTART, RLENGTH)
          if (match(m, /[0-9]+\.[0-9]+\.[0-9]+/))
            printf "%s:%d:%s\n", ENVIRON["FILE"], NR, substr(m, RSTART, RLENGTH)
        }
      }' "$root/$f"
  done < <(pin_domain "$root")
}

# tool_hits <инструмент> <корень> → перепись по ВСЕМ зондам этого инструмента.
tool_hits() {
  local name="$1" root="${2:-$REPO_ROOT}" p rest
  for p in "${PROBES[@]}"; do
    [ "${p%%"$S"*}" = "$name" ] || continue
    rest="${p#*"$S"}"                    # <репо>§<анкер>§<пин>
    rest="${rest#*"$S"}"                 # <анкер>§<пин>
    census "${rest%"$S"*}" "${rest##*"$S"}" "$root"
  done
}

# tool_list — инструменты каталога в порядке объявления, без повторов.
tool_list() {
  local p n seen=""
  for p in "${PROBES[@]}"; do
    n="${p%%"$S"*}"
    case " $seen " in *" $n "*) continue ;; esac
    seen="$seen $n"; echo "$n"
  done
}

# upstream_of <инструмент> → репозиторий апстрима из каталога.
upstream_of() {
  local p rest
  for p in "${PROBES[@]}"; do
    if [ "${p%%"$S"*}" = "$1" ]; then
      rest="${p#*"$S"}"; echo "${rest%%"$S"*}"; return
    fi
  done
}

# latest_gh <owner/repo> → последний НЕ-prerelease тег, без ведущей v.
latest_gh() {
  local auth=()
  [ -n "${GH_TOKEN:-}" ] && auth=(-H "Authorization: Bearer ${GH_TOKEN}")
  curl -fsSL "${auth[@]}" "https://api.github.com/repos/$1/releases/latest" 2>/dev/null \
    | jq -r '.tag_name // empty' | sed 's/^v//'
}

# ── ПРОХОД ───────────────────────────────────────────────────────────────────
#
# divergence_only=1 — только сверка пинов между собой. Сети не требует вовсе,
# поэтому годится в гейт КАЖДОГО прогона, а не только у недельного сторожа: пока
# расхождение ловилось раз в неделю, оно жило в стволе до недели, и оба конвейера
# всё это время исполняли разные версии.
#
# НЕИЗВЕСТНЫЙ АРГУМЕНТ — ЯВНЫЙ ОТКАЗ. Молча свалиться в полный проход значило бы,
# что опечатка в шаге конвейера (--divergence, --self_test) даёт зелёное на
# проверке, которую никто не заказывал: шаг отработал, вердикт не про то.
divergence_only=0
case "${1:-}" in
  "")                 ;;
  --divergence-only)  divergence_only=1 ;;
  --self-test)        ;;   # разбирается ниже, после объявления функций
  *)
    echo "неизвестный аргумент: $1" >&2
    echo "допустимо: без аргументов | --divergence-only | --self-test" >&2
    exit 2 ;;
esac

stale=""
rc=0
found_total=0

run_pass() {
  local name hits values value files want
  for name in $(tool_list); do
    hits="$(tool_hits "$name")"
    if [ -z "$hits" ]; then
      echo "  ⚠ $name — пин не найден (regex устарел?)" >&2
      rc=1; continue
    fi
    found_total=$((found_total + $(printf '%s\n' "$hits" | grep -c .)))

    # РАСХОЖДЕНИЕ — отдельная находка, и она НЕ глушится HOLD.
    values="$(printf '%s\n' "$hits" | awk -F: '{print $NF}' | LC_ALL=C sort -u)"
    if [ "$(printf '%s\n' "$values" | grep -c .)" -gt 1 ]; then
      files="$(printf '%s\n' "$hits" | awk -F: '{printf "`%s` (%s:%s), ", $NF, $1, $2}' | sed 's/, $//')"
      echo "  ✗ $name — пины РАСХОДЯТСЯ: $files" >&2
      stale="${stale}- **${name}** — пины **расходятся**: ${files}. Конвейеры исполняют разные версии инструмента, поэтому вердикты о том же дереве несравнимы; приведи к одному значению."$'\n'
      rc=1; continue
    fi
    value="$values"

    if [ "$divergence_only" = 1 ]; then
      echo "  ✓ $name: $value — сходится во всех вхождениях ($(printf '%s\n' "$hits" | grep -c .))" >&2
      continue
    fi

    want="$(latest_gh "$(upstream_of "$name")")"
    if [ -z "$want" ]; then
      echo "  ⚠ $name — апстрим недоступен (rate-limit?)" >&2; continue
    fi
    if [ "$value" != "$want" ]; then
      if [ -n "${HOLD[$name]:-}" ]; then
        echo "  ⏸ $name: пин $value, апстрим $want — HOLD: ${HOLD[$name]%%$'\n'*}" >&2
        continue
      fi
      echo "  ✗ $name: пин $value, апстрим $want" >&2
      stale="${stale}- **${name}** — запинен \`${value}\`, апстрим \`${want}\` ($(upstream_of "$name"))"$'\n'
    else
      echo "  ✓ $name: $value" >&2
    fi
  done
}

# ── САМОПРОВЕРКА: инъекция в ОБЕ стороны, сети не требует ─────────────────────
if [ "${1:-}" = "--self-test" ]; then
  echo "=== check-pinned-tools --self-test: инъекции в синтетическое дерево ==="
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  src="$tmp/.github/workflows"; mkdir -p "$src"
  st_rc=0

  # Фикстуры СОБИРАЮТСЯ из подстановки, а не пишутся литералом: литерал сделал бы
  # этот файл своей же находкой при переписи дерева, и «сторож себя не считает»
  # держалось бы на везении.
  A=0.30.0; B=0.32.0
  kindline() { printf '          curl -sSLo k https://kind.sigs.k8s.io/dl/v%s/kind-linux-amd64\n' "$1"; }
  helmstep() { printf '      - uses: azure/setup-helm@v4\n        with:\n          version: v%s\n' "$1"; }
  bufstep()  { printf '      - uses: bufbuild/buf-setup-action@v1\n        with:\n          version: %s\n' "$1"; }

  probe() { # probe <инструмент> → перепись по синтетическому дереву
    tool_hits "$1" "$tmp"
  }
  distinct() { probe "$1" | awk -F: '{print $NF}' | LC_ALL=C sort -u | grep -c .; }

  # (1) РАЗВЕДЁННЫЕ ПИНЫ одного инструмента в ДВУХ файлах — обязано быть видно, и
  #     обязаны быть названы ОБА файла. Это тот самый дефект, который прежняя
  #     редакция не видела by construction: она читала один названный файл.
  { echo 'jobs:'; echo '  a:'; echo '    steps:'; kindline "$A"; } >"$src/one.yml"
  { echo 'jobs:'; echo '  b:'; echo '    steps:'; kindline "$B"; } >"$src/two.yml"
  hits="$(probe kind)"
  if [ "$(distinct kind)" -gt 1 ] \
     && printf '%s\n' "$hits" | grep -q '^\.github/workflows/one\.yml:.*:'"$A"'$' \
     && printf '%s\n' "$hits" | grep -q '^\.github/workflows/two\.yml:.*:'"$B"'$'; then
    echo "  ОК  расхождение видно и названы ОБА файла ($A / $B)"
  else
    echo "  ПРОВАЛ расхождение в двух файлах не найдено или назван не весь состав"
    printf '%s\n' "$hits" | sed 's/^/      /'; st_rc=1
  fi

  # (2) ТЕ ЖЕ ДВА ФАЙЛА, ПРИВЕДЁННЫЕ К ОДНОМУ ЗНАЧЕНИЮ — обязано молчать.
  #     Без этой половины сторож ловил бы форму (пин в двух местах), а не существо
  #     (пин в двух местах РАЗНЫЙ), и первое же законное дублирование его снесло бы.
  { echo 'jobs:'; echo '  b:'; echo '    steps:'; kindline "$A"; } >"$src/two.yml"
  if [ "$(distinct kind)" -eq 1 ] && [ "$(probe kind | grep -c .)" -eq 2 ]; then
    echo "  ОК  два вхождения одного значения расхождением НЕ считаются"
  else
    echo "  ПРОВАЛ законный дубль принят за расхождение"; probe kind | sed 's/^/      /'; st_rc=1
  fi

  # (3) КОММЕНТАРИЙ, НАЗЫВАЮЩИЙ ДРУГУЮ ВЕРСИЮ — не пин. Ровно эта форма живёт в
  #     наших workflow'ах (объяснение, зачем пин), и она обязана НЕ считаться:
  #     иначе подъём версии краснел бы на собственной документации.
  { echo 'jobs:'; echo '  c:'; echo '    steps:'
    printf '          # прежде было v%s — подняли, см. release notes\n' "$B"
    kindline "$A"; printf '          echo done  # раньше v%s\n' "$B"; } >"$src/three.yml"
  if [ "$(distinct kind)" -eq 1 ]; then
    echo "  ОК  версия в комментарии за пин не принята"
  else
    echo "  ПРОВАЛ сторож читает текст, а не исполняемую часть"; probe kind | sed 's/^/      /'; st_rc=1
  fi
  rm -f "$src/three.yml"

  # (4) АНКЕР: `version:` ЧУЖОГО шага — законный близнец той же формы. helm и buf
  #     объявляют вход одинаково, и без анкера версия buf засчиталась бы helm'у как
  #     расхождение (или наоборот — заглушила бы настоящее).
  { echo 'jobs:'; echo '  d:'; echo '    steps:'; helmstep 3.17.0; bufstep 1.72.0; } >"$src/anchored.yml"
  if [ "$(distinct helm)" -eq 1 ] && [ "$(probe helm | grep -c .)" -eq 1 ] \
     && [ "$(distinct buf)" -eq 1 ] && [ "$(probe buf | grep -c .)" -eq 1 ]; then
    echo "  ОК  анкер различает setup-helm и buf-setup-action при одинаковом входе"
  else
    echo "  ПРОВАЛ вход version: приписан не тому шагу"
    probe helm | sed 's/^/  helm: /'; probe buf | sed 's/^/  buf:  /'; st_rc=1
  fi

  # (4б) ТОТ ЖЕ ФАЙЛ с РАЗВЕДЁННЫМ helm — анкер не должен глушить настоящее
  #      расхождение своего инструмента.
  { echo 'jobs:'; echo '  d:'; echo '    steps:'; helmstep 3.17.0; bufstep 1.72.0
    echo '  e:'; echo '    steps:'; helmstep 3.18.0; } >"$src/anchored.yml"
  if [ "$(distinct helm)" -gt 1 ] && [ "$(distinct buf)" -eq 1 ]; then
    echo "  ОК  расхождение анкерованного инструмента видно, сосед не задет"
  else
    echo "  ПРОВАЛ анкер заглушил расхождение своего же инструмента"
    probe helm | sed 's/^/  helm: /'; st_rc=1
  fi
  rm -f "$src/anchored.yml"

  # (5) НОЛЬ ВХОЖДЕНИЙ — находка, а не чистота. Инструмент, чей пин перестал
  #     находиться, не «свежий»: он НЕ ПРОВЕРЕН, и это обязано быть отличимо.
  if [ "$(probe gosec | grep -c .)" -eq 0 ]; then
    echo "  ОК  отсутствие пина отличимо от совпадения (ноль вхождений)"
  else
    echo "  ПРОВАЛ найден пин, которого в синтетическом дереве нет"; st_rc=1
  fi

  # (6) ПУСТОЙ ДОМЕН — «ноль находок» обязано быть отличимо от «ноль прочитанного».
  if [ "$(pin_domain "$tmp" | grep -c .)" -eq 2 ] && [ "$(pin_domain "$tmp/nope" | grep -c .)" -eq 0 ]; then
    echo "  ОК  объём осмотренного считается (домен: 2 файла; пустой корень: 0)"
  else
    echo "  ПРОВАЛ домен переписи не измеряется"; st_rc=1
  fi

  echo
  [ $st_rc -eq 0 ] && echo "PASS: check-pinned-tools --self-test" \
                   || echo "FAIL: check-pinned-tools --self-test"
  exit $st_rc
fi

scanned="$(pin_domain "$REPO_ROOT" | grep -c .)"
run_pass

# ОБЪЁМ ОСМОТРЕННОГО. «Ноль находок» обязано быть отличимо от «ноль прочитанного»:
# без этих чисел сообщение «пины сходятся» одинаково читается и когда перепись
# прочла все определения конвейера, и когда она не дошла ни до одного файла.
echo "=== осмотрено определений конвейера: $scanned; найдено вхождений пинов: $found_total" \
     "по $(tool_list | grep -c .) инструментам ===" >&2
if [ "$scanned" -eq 0 ]; then
  echo "  ✗ домен переписи ПУСТ — сверка не состоялась" >&2
  rc=1
fi

printf '%s' "$stale"
exit $rc
