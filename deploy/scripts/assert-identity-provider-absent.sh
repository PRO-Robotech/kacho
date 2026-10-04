#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ЖИВОЙ ГЕЙТ ПОСАДКИ ЛИЧНОСТИ: на стенде поставщика личности НЕТ — и это
# отсутствие НАСТОЯЩЕЕ, а не объявленное.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ (kacho#1276, часть 3; прежде — половина own гейта административного
# перехода, kacho#2816)
#
# Посадка личности у службы доступа одна — своя (kaname#363), подчартов
# поставщика в зонте нет (kacho#1276): поднять его на стенде нечем ни одной
# цепочкой. Отсутствие обязано быть настоящим, и судят его ЖИВЫЕ процессы и
# ЖИВОЙ кластер, а не рендер:
#
#   • обе половины — служба доступа и край — назвали посадку `own` своей строкой
#     «boot security posture» при старте. Половина, назвавшая другое, — отказ:
#     посадке external в зонте не на чем стоять; половины, назвавшие разное, —
#     отказ: такой стенд решает о личности двумя способами сразу; молчание
#     процесса — «посадка НЕ ПРОЧИТАНА», а не «own»;
#   • подов поставщика в пространстве имён нет;
#   • ни один потребитель (край, служба доступа) не несёт в окружении адреса
#     административного API поставщика — дороги к соседу, которого нет.
#
# Рендер это уже утверждает (deploy/stack_render_carries_no_vendor_residue_test.go,
# deploy/own_posture_foreign_identity_test.go) — но рендер судит ДЕРЕВО. Здесь
# судится ПРИМЕНЁННОЕ: стенд, на котором осталась нагрузка прежней выкатки или
# под, поднятый мимо зонта, рендер не видит by construction.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО СНЯТО И ПОЧЕМУ (kacho#1276, часть 3)
#
# Прежде файл назывался assert-admin-hop-transport.sh и судил шифрование
# административного перехода к поставщику: терминатор TLS перед его
# административным слушателем, пробник без настроек продукта, журнал дальнего
# конца, отпечаток конфигурации терминатора, предпосылку «версия поставщика не
# читает объявление TLS отдельного слушателя» (её запись и перемерщик). Все эти
# утверждения стояли на предмете, которого больше нет: терминатор снят вместе с
# подчартами поставщика, перехода не существует, и судить его шифрование —
# судить пустоту. Осталась ровно та половина, что судила отсутствие; ветвь
# «посадка не own ⇒ судит половина перехода» стала отказом с причиной.
#
# «НЕ ПРОЧИТАНО» — ОТДЕЛЬНЫЙ ИСХОД, А НЕ НОЛЬ (kacho#2816): чтение отдаёт код
# отдельно от счёта, и отказ сервера не читается как «поставщика нет».
#
# Кластерная половина исполняется самопроверкой ЦЕЛИКОМ под подменёнными
# kubectl и kind; её подключение к общим функциям чтения и суждения держит
# инъекция deploy/scripts/identity-provider-absent-cluster-half-inject.sh.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

SCRIPT="$(basename "$0")"
NS="${NS:-kacho}"

FAILED=0
ASSERTIONS=0
fail() { echo "  ✗ $*"; FAILED=1; }
ok()   { echo "  ✓ $*"; }
note() { echo "    $*"; }
assertion() { ASSERTIONS=$((ASSERTIONS + 1)); }

# ═════════════════════════════════════════════════════════════════════════════
# ПРЕДИКАТЫ ВЕРДИКТА — ЧИСТЫЕ ФУНКЦИИ НАД ТЕКСТОМ.
#
# Вынесены из кластерной половины затем, чтобы самопроверка могла скормить им
# синтетические наблюдения БЕЗ кластера и потребовать покраснеть на каждом
# дефектном и промолчать на законном той же формы.
# ═════════════════════════════════════════════════════════════════════════════

# own_absence_verdict <посадка службы> <посадка края> <подов поставщика> <адресов у потребителей>
#   → именованный исход.
#
# Посадку называют ЖИВЫЕ процессы (строка самоотчёта при старте), а не
# настройки. Порядок проверок — от посадки к нагрузке: о подах и адресах
# судить бессмысленно, пока не установлено, какую посадку стенд исполняет.
#
#   posture-unread  — половина промолчала: «не прочитано» не равно «own»;
#   not-own         — обе половины назвали одну посадку, и она не own;
#   halves-disagree — половины назвали разное;
#   pods-unread / consumers-unread — чтение отказало: отсутствие НЕ установлено;
#   provider-present / consumer-names-provider — отсутствие не выполнено;
#   ok              — отсутствие настоящее.
own_absence_verdict() {
  local iam="$1" edge="$2" pods="$3" addrs="$4"
  if [ -z "$iam" ] || [ -z "$edge" ]; then echo posture-unread; return; fi
  if [ "$iam" != "$edge" ]; then echo halves-disagree; return; fi
  if [ "$iam" != own ]; then echo not-own; return; fi
  case "$pods" in ''|*[!0-9]*) echo pods-unread; return ;; esac
  if [ "$pods" -ne 0 ]; then echo provider-present; return; fi
  case "$addrs" in ''|*[!0-9]*) echo consumers-unread; return ;; esac
  if [ "$addrs" -ne 0 ]; then echo consumer-names-provider; return; fi
  echo ok
}

# provider_pod_census <код kubectl> <вывод `get pods -o name`> → число | исход отказа.
#
# КОД ЧТЕНИЯ БЕРЁТСЯ ОТДЕЛЬНО ОТ СЧЁТА СТРОК. Счёт строк конвейером
# (`kubectl … 2>/dev/null | grep -c .`) отвечает «0» и на пустой список, и на
# отказ сервера — отсутствие, которое гейт утверждает, становится неотличимым от
# отсутствия чтения. Код ненулевой — не прочитано, даже если строки были
# (частичный вывод числом не является). Строка не формы `pod/<имя>` — тоже не
# прочитано: корзины «прочее» у разбора нет.
provider_pod_census() {
  local rc="$1" out="$2" n=0 line
  if [ "$rc" != 0 ]; then echo unread-refused; return; fi
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    case "$line" in
      pod/?*) n=$((n + 1)) ;;
      *) echo unread-shape; return ;;
    esac
  done <<<"$out"
  echo "$n"
}

# consumer_addr_census <код kubectl> <запрошено объектов> <«прочитано-объектов адресов»>
#   → число адресов | исход отказа.
#
# Тот же класс у соседнего чтения. `get deploy A B` при одном отсутствующем
# объекте печатает найденный И выходит ненулевым кодом; счёт адресов по
# напечатанному дал бы «0 адресов» о потребителе, которого не прочитали. Поэтому:
# код ненулевой — не прочитано; объектов меньше запрошенного — не прочитано;
# разбор дал не два числа — не прочитано.
consumer_addr_census() {
  local rc="$1" want="$2" counts="$3" items="" addrs="" rest=""
  if [ "$rc" != 0 ]; then echo unread-refused; return; fi
  read -r items addrs rest <<<"$counts"
  case "$items" in ''|*[!0-9]*) echo unread-shape; return ;; esac
  case "$addrs" in ''|*[!0-9]*) echo unread-shape; return ;; esac
  if [ -n "$rest" ]; then echo unread-shape; return; fi
  if [ "$items" -ne "$want" ]; then echo unread-partial; return; fi
  echo "$addrs"
}

# read_provider_pods — ЕДИНСТВЕННОЕ чтение подов поставщика: им пользуются и
# кластерная половина, и самопроверка (с подменённым kubectl). Ставит
# PODS_CENSUS (исход provider_pod_census) и PODS_REFUSAL (текст отказа kubectl,
# чтобы «не прочитано» называло причину, а не только факт).
read_provider_pods() {
  local out rc errf
  PODS_CENSUS=unread-refused
  PODS_REFUSAL=""
  if ! errf="$(mktemp "${TMPDIR:-/tmp}/identity-provider-pods.XXXXXX")"; then
    PODS_REFUSAL="не удалось завести файл для текста отказа kubectl (mktemp)"
    return
  fi
  out="$(kubectl -n "$NS" get pods -l 'app.kubernetes.io/name in (hydra,kratos)' -o name 2>"$errf")"
  rc=$?
  PODS_CENSUS="$(provider_pod_census "$rc" "$out")"
  case "$PODS_CENSUS" in
    unread-*) PODS_REFUSAL="код kubectl $rc: $(head -c 300 "$errf" | tr '\n' ' ')" ;;
  esac
  rm -f "$errf"
}


# ═════════════════════════════════════════════════════════════════════════════
# САМОПРОВЕРКА — БЕЗ КЛАСТЕРА.
# ═════════════════════════════════════════════════════════════════════════════
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: предикаты вердикта против синтетических наблюдений ==="
  rc=0; checked=0
  echo
  echo "-- посадка own: поставщика нет, отсутствие обязано быть настоящим --"
  expect_own() { # <метка> <ожидаемо> <служба> <край> <подов> <адресов>
    local label="$1" want="$2" got
    checked=$((checked + 1))
    got="$(own_absence_verdict "$3" "$4" "$5" "$6")"
    if [ "$got" = "$want" ]; then echo "  ✓ $label → $got"
    else echo "  ✗ $label → получено '$got', ожидалось '$want'"; rc=1; fi
  }
  expect_own "own обеих половин, поставщика нет, адресов нет" ok own own 0 0
  expect_own "own, но под поставщика поднят" provider-present own own 1 0
  expect_own "own, но потребитель называет адрес поставщика" consumer-names-provider own own 0 1
  expect_own "половины назвали разное" halves-disagree own external 0 0
  expect_own "обе половины external — посадке не на чем стоять" not-own external external 0 0
  # «НЕ ПРОЧИТАНО» у ПОСАДКИ — тот же класс, что у подов: молчание процесса за
  # «own» не идёт, и за «разное» тоже — это третий исход.
  expect_own "служба промолчала о посадке" posture-unread "" own 0 0
  expect_own "край промолчал о посадке" posture-unread own "" 0 0
  # «НЕ ПРОЧИТАНО» ≠ «НЕТ». Суждение получает от чтения не число, а исход отказа —
  # и обязано отказать, а не провалиться сквозь сравнение с нулём в `ok`.
  expect_own "подов не прочитано (отказ сервера)" pods-unread own own unread-refused 0
  expect_own "подов не прочитано (пустое значение)" pods-unread own own "" 0
  expect_own "окружение потребителей не прочитано" consumers-unread own own 0 unread-partial

  echo
  echo "-- счёт подов поставщика: код kubectl читается ОТДЕЛЬНО от счёта строк --"
  expect_pods() { # <метка> <ожидаемо> <код kubectl> <вывод -o name>
    local label="$1" want="$2" got
    checked=$((checked + 1))
    got="$(provider_pod_census "$3" "$4")"
    if [ "$got" = "$want" ]; then echo "  ✓ $label → $got"
    else echo "  ✗ $label → получено '$got', ожидалось '$want'"; rc=1; fi
  }
  expect_pods "список пуст, код 0 — поставщика нет (законный близнец)" 0 0 ""
  expect_pods "два пода, код 0" 2 0 'pod/provider-admin-0
pod/provider-session-0'
  expect_pods "отказ сервера, вывода нет" unread-refused 1 ""
  expect_pods "отказ после частичного вывода" unread-refused 1 "pod/provider-admin-0"
  expect_pods "код 0, но строка — не имя пода" unread-shape 0 "No resources found in kacho namespace."

  echo
  echo "-- счёт адресов у потребителей: прочитаны ОБА названных объекта, иначе не прочитано --"
  expect_consumers() { # <метка> <ожидаемо> <код kubectl> <запрошено объектов> <«объектов адресов»>
    local label="$1" want="$2" got
    checked=$((checked + 1))
    got="$(consumer_addr_census "$3" "$4" "$5")"
    if [ "$got" = "$want" ]; then echo "  ✓ $label → $got"
    else echo "  ✗ $label → получено '$got', ожидалось '$want'"; rc=1; fi
  }
  expect_consumers "оба прочитаны, адресов нет (законный близнец)" 0 0 2 "2 0"
  expect_consumers "оба прочитаны, адрес есть" 1 0 2 "2 1"
  expect_consumers "отказ сервера, вывода нет" unread-refused 1 2 ""
  expect_consumers "прочитан один из двух, второй NotFound" unread-refused 1 2 "1 0"
  expect_consumers "код 0, но объектов меньше запрошенного" unread-partial 0 2 "1 0"
  expect_consumers "разбор не дал чисел" unread-shape 0 2 ""
  expect_consumers "разбор дал лишнее поле" unread-shape 0 2 "2 0 7"

  echo
  echo "-- путь чтения подов — ТОТ ЖЕ код, что у кластерной половины; kubectl подменён --"
  # Подменяется ровно один факт — исход `get pods`; суждение берёт то, что
  # вернуло чтение, и ничего сверх. Подмена — функцией в подоболочке: вызывается
  # read_provider_pods кластерной половины, а не её копия.
  expect_read() { # <метка> <режим get pods> <ожидаемое суждение> <подстрока причины | ->
    local label="$1" mode="$2" want="$3" why="$4" got
    checked=$((checked + 1))
    got="$(
      kubectl() {
        case "$mode" in
          empty)   return 0 ;;
          present) echo 'pod/provider-admin-0'; return 0 ;;
          refused) echo 'Unable to connect to the server: net/http: TLS handshake timeout' >&2; return 1 ;;
        esac
        return 97
      }
      read_provider_pods
      printf '%s|%s' "$(own_absence_verdict own own "$PODS_CENSUS" 0)" "$PODS_REFUSAL"
    )"
    local verdict="${got%%|*}" refusal="${got#*|}"
    if [ "$verdict" != "$want" ]; then
      echo "  ✗ $label → суждение '$verdict', ожидалось '$want'"; rc=1; return
    fi
    if [ "$why" != - ] && ! grep -qF "$why" <<<"$refusal"; then
      echo "  ✗ $label → суждение '$verdict', но причина отказа не названа (получено '$refusal')"; rc=1; return
    fi
    echo "  ✓ $label → $verdict${refusal:+ ($refusal)}"
  }
  expect_read "get pods пуст — поставщика нет (законный близнец)" empty ok -
  expect_read "get pods отказал — НЕ зелёное, причина названа" refused pods-unread 'TLS handshake timeout'
  expect_read "под поставщика есть (положительный контроль)" present provider-present -

  echo
  echo "-- кластерная половина исполняется ЦЕЛИКОМ; kubectl и kind подменены в PATH --"
  # ПОДКЛЮЧЕНИЕ ЗАКРЕПЛЯЕТСЯ ИСХОДОМ, А НЕ ФУНКЦИЯМИ. Всё выше судит функции
  # суждения и чтения напрямую — и остаётся зелёным, если кластерная половина
  # перестанет ими пользоваться (вернётся к счёту строк конвейером или к своему
  # сравнению с нулём): функции целы, а гейт на стенде снова читает отказ сервера
  # как «поставщика нет» (kacho#2845, мутант M7 ревью волны kacho#2795). Поэтому
  # здесь исполняется сам этот файл БЕЗ `--self-test`, от проверки контекста до
  # выхода, и судится его код выхода и текст. Подменён ровно один факт на случай
  # против законного близнеца «оба чтения удались, поставщика нет».
  #
  # Код выхода сравнивается ТОЧНО: 2 — отказ проверки контекста, то есть подмена
  # не сработала и половина до суждения не дошла; такой исход не засчитывается ни
  # за «покраснела», ни за «промолчала».
  if ! command -v jq >/dev/null 2>&1; then
    echo "  ✗ кластерная половина НЕ ИСПОЛНЕНА: нет jq (им она читает посадку и"
    echo "    окружение потребителей) — самопроверка не выполнилась, это не «прошло»"
    exit 2
  fi
  SELF="${BASH_SOURCE[0]}"
  fake_bin="$(mktemp -d "${TMPDIR:-/tmp}/identity-provider-absent-selftest.XXXXXX")" || {
    echo "  ✗ кластерная половина НЕ ИСПОЛНЕНА: не удалось завести каталог подмен (mktemp)"
    exit 2
  }
  cat >"$fake_bin/kind" <<'FAKE_KIND'
#!/usr/bin/env bash
[ "$1 $2" = "get kubeconfig" ] && { echo "    server: https://self-test.invalid:6443"; exit 0; }
echo "self-test kind: неожиданный вызов: $*" >&2; exit 97
FAKE_KIND
  cat >"$fake_bin/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
# Двойник отвечает только на вызовы ветки посадки own; любой другой вызов —
# отказ с кодом 97, чтобы половина, ушедшая мимо этой ветки, не нашла кластера.
case "$*" in
  "config current-context") echo "kind-$CLUSTER_NAME"; exit 0 ;;
  "config view --minify "*) echo "https://self-test.invalid:6443"; exit 0 ;;
  *" logs deploy/kaname")
    [ "${FAKE_IAM:-own}" = none ] && exit 0
    echo "{\"msg\":\"boot security posture\",\"identity_provider\":\"${FAKE_IAM:-own}\"}"; exit 0 ;;
  *" logs deploy/api-gateway")
    echo "{\"msg\":\"boot security posture\",\"identity_provider\":\"${FAKE_EDGE:-own}\"}"; exit 0 ;;
  *" get pods -l "*" -o name")
    case "$FAKE_PODS" in
      empty)   exit 0 ;;
      present) echo 'pod/provider-admin-0'; exit 0 ;;
      refused) echo 'Unable to connect to the server: net/http: TLS handshake timeout' >&2; exit 1 ;;
    esac ;;
  *" get deploy api-gateway kaname -o json")
    item='{"spec":{"template":{"spec":{"containers":[{"env":[ENV]}]}}}}'
    none="${item/ENV/{\"name\":\"KACHO_APP_ENV\",\"value\":\"dev\"\}}"
    case "$FAKE_DEPLOY" in
      read)    echo "{\"items\":[$none,$none]}"; exit 0 ;;
      partial) echo "{\"items\":[$none]}"
               echo 'Error from server (NotFound): deployments.apps "kaname" not found' >&2; exit 1 ;;
    esac ;;
esac
echo "self-test kubectl: неожиданный вызов: $*" >&2; exit 97
FAKE_KUBECTL
  chmod +x "$fake_bin/kind" "$fake_bin/kubectl"

  expect_half() { # <метка> <поды> <окружение> <посадка края> <ожидаемый код> <подстрока вывода>
    local label="$1" out code
    checked=$((checked + 1))
    out="$(PATH="$fake_bin:$PATH" CLUSTER_NAME=self-test NS=kacho \
      FAKE_PODS="$2" FAKE_DEPLOY="$3" FAKE_EDGE="$4" \
      timeout 60 bash "$SELF" 2>&1)" && code=0 || code=$?
    if [ "$code" != "$5" ] || ! grep -qF -- "$6" <<<"$out"; then
      echo "  ✗ кластерная половина: $label → код $code, ожидался $5 с «$6»"
      echo "    половина не берёт чтение (read_provider_pods, consumer_addr_census) или"
      echo "    суждение (own_absence_verdict) из общих функций; её вывод:"
      printf '%s\n' "$out" | tail -6 | sed 's/^/      /'
      rc=1; return
    fi
    echo "  ✓ кластерная половина: $label → код $code"
  }
  expect_half "оба чтения удались, поставщика нет (законный близнец)" \
    empty read own 0 'поставщика на стенде нет'
  expect_half "get pods отказал — не «поставщика нет», причина названа" \
    refused read own 1 'TLS handshake timeout'
  expect_half "под поставщика есть (положительный контроль)" \
    present read own 1 'подов поставщика 1'
  expect_half "окружение потребителей прочитано частично (NotFound второго)" \
    empty partial own 1 'НЕ ПРОЧИТАНО (unread-refused'
  expect_half "половины назвали разную посадку" \
    empty read external 1 'назвали РАЗНУЮ'
  FAKE_IAM=external expect_half "обе половины назвали external — поставщика в зонте нет" \
    empty read external 1 'посадка личности не own'
  FAKE_IAM=none expect_half "посадка службы не прочитана — не «разная», а непрочитанная" \
    empty read own 1 'посадка НЕ ПРОЧИТАНА'
  rm -rf "$fake_bin"

  echo
  echo "синтетических наблюдений и законных входов проверено: $checked"
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $rc
fi

# ═════════════════════════════════════════════════════════════════════════════
# КЛАСТЕРНАЯ ПОЛОВИНА
# ═════════════════════════════════════════════════════════════════════════════
command -v kubectl >/dev/null 2>&1 || { echo "FATAL: нужен kubectl"; exit 2; }

# ЦЕЛЬ ПИНИТСЯ ПО КЛАСТЕРУ, А НЕ ПО ИМЕНИ КОНТЕКСТА. Одноимённый контекст уже
# однажды вёл в другой кластер: имя выбирает автор kubeconfig, совпадение имён
# ничего не доказывает.
# ИМЯ КЛАСТЕРА БЕРЁТСЯ ИЗ ОКРУЖЕНИЯ, А НЕ ВЫПИСЫВАЕТСЯ. Шарды сквозных проб
# поднимают каждый свой кластер (`kacho-iam`, `kacho-vpc`, …), и выписанное имя
# делает пробу неисполнимой на всех, кроме одного: она честно отказывается
# судить и роняет подъём. Умолчание совпадает с объявлением сборки, поэтому
# одиночный стенд ведёт себя как прежде.
CLUSTER_NAME="${CLUSTER_NAME:-kacho}"
ctx="$(kubectl config current-context 2>/dev/null)"
case "$ctx" in
  "kind-$CLUSTER_NAME") ;;
  *) echo "ABORT: активный kube-контекст '$ctx' — не kind-$CLUSTER_NAME."; exit 2 ;;
esac
want_srv="$(kind get kubeconfig --name "$CLUSTER_NAME" 2>/dev/null | sed -n 's/^ *server: *//p' | head -1)"
have_srv="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null)"
if [ -z "$want_srv" ]; then
  echo "ABORT: kind не знает кластера '$CLUSTER_NAME' — сверить адрес apiserver'а НЕ С ЧЕМ,"
  echo "       а «не с чем сверить» означает «не проверили», а не «всё хорошо»."
  exit 2
fi
if [ "$want_srv" != "$have_srv" ]; then
  echo "ABORT: контекст называется kind-'$CLUSTER_NAME', но ведёт НЕ в этот кластер."
  echo "       активный → $have_srv"
  echo "       kind-$CLUSTER_NAME на самом деле → $want_srv"
  exit 2
fi


echo "=== D. посадка личности own на живом стенде: поставщика нет ==="

# ── ПОСАДКА ЧИТАЕТСЯ У ЖИВЫХ ПРОЦЕССОВ (kacho#2816) ──────────────────────────
# identity_provider из строки «boot security posture» текущего контейнера службы
# доступа и края. Пусто — посадка не прочитана: молчание процесса за объявление
# отсутствия не идёт.
boot_identity_provider() { # <deployment>
  kubectl -n "$NS" logs "deploy/$1" 2>/dev/null | grep -F '"boot security posture"' | tail -1 \
    | jq -r '.identity_provider // empty' 2>/dev/null
}

# judge_live_stand — кластерная половина целиком: прочитать посадку, поды и
# окружение потребителей и вынести суждение ОБЩЕЙ функцией. Оба чтения отдают
# КОД отдельно от счёта: «не прочитано» — отказ с причиной, а не «0»
# (kacho#2816, возврат ревью волны kacho#2795).
judge_live_stand() {
  IAM_POSTURE="$(boot_identity_provider kaname)"
  EDGE_POSTURE="$(boot_identity_provider api-gateway)"
  read_provider_pods
  PROVIDER_PODS="$PODS_CENSUS"
  CONSUMER_OBJECTS=(api-gateway kaname)
  consumer_json="$(kubectl -n "$NS" get deploy "${CONSUMER_OBJECTS[@]}" -o json 2>/dev/null)"
  consumer_rc=$?
  consumer_counts="$(jq -r '
    "\(.items | length) \([ .items[].spec.template.spec.containers[].env[]?
      | select(.name == "KACHO_HYDRA_INTROSPECTION_URL" or .name == "KACHO_HYDRA_ADMIN_URL"
               or .name == "KANAME_HYDRA_ADMIN_URL")
      | select((.value // "") != "") ] | length)"' <<<"$consumer_json" 2>/dev/null)"
  CONSUMER_ADDRS="$(consumer_addr_census "$consumer_rc" "${#CONSUMER_OBJECTS[@]}" "$consumer_counts")"
  assertion
  case "$(own_absence_verdict "$IAM_POSTURE" "$EDGE_POSTURE" "$PROVIDER_PODS" "$CONSUMER_ADDRS")" in
    ok)
      ok "посадка own (служба: $IAM_POSTURE, край: $EDGE_POSTURE): поставщика на стенде нет — подов поставщика $PROVIDER_PODS, адресов его административного API у потребителей $CONSUMER_ADDRS (прочитано объектов ${#CONSUMER_OBJECTS[@]} из ${#CONSUMER_OBJECTS[@]})" ;;
    posture-unread)
      fail "посадка НЕ ПРОЧИТАНА: служба '${IAM_POSTURE}', край '${EDGE_POSTURE}' — строки «boot security posture» у текущего контейнера нет"
      note "молчание процесса за «own» не идёт: какую посадку стенд исполняет, не установлено." ;;
    halves-disagree)
      fail "посадку половины назвали РАЗНУЮ: служба '$IAM_POSTURE', край '$EDGE_POSTURE'" ;;
    not-own)
      fail "посадка личности не own (служба и край: '$IAM_POSTURE') — поставщика в зонте нет (kacho#1276), посадке '$IAM_POSTURE' не на чем стоять" ;;
    pods-unread)
      fail "поды поставщика НЕ ПРОЧИТАНЫ ($PROVIDER_PODS) — отсутствие поставщика НЕ установлено"
      note "$PODS_REFUSAL"
      note "«не прочитано» не равно «нет»: суждению о посадке own не на чем стоять." ;;
    consumers-unread)
      fail "окружение потребителей (${CONSUMER_OBJECTS[*]}) НЕ ПРОЧИТАНО ($CONSUMER_ADDRS; код kubectl $consumer_rc, объектов и адресов: '${consumer_counts}') — отсутствие адресов НЕ установлено" ;;
    provider-present)
      fail "посадка own, а подов поставщика $PROVIDER_PODS — объявленное отсутствие не выполнено" ;;
    consumer-names-provider)
      fail "посадка own, а потребители называют адрес административного API поставщика ($CONSUMER_ADDRS) — дорога к соседу, которого нет" ;;
    *)
      fail "исход суждения о посадке own не распознан" ;;
  esac
}
judge_live_stand

echo
echo "  утверждений выполнено: $ASSERTIONS"
if [ "$ASSERTIONS" -eq 0 ]; then
  echo "FAIL: не выполнено НИ ОДНОГО утверждения — это провал, а не чистота."
  exit 1
fi
[ "$FAILED" -ne 0 ] && { echo "FAIL: $SCRIPT"; exit 1; }
echo "PASS: $SCRIPT"
