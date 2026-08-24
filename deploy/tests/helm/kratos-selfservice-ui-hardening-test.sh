#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# INFRA sec-hardening r2 manifest-assertion guard (offline; no kind cluster).
#
# Covers the second-audit INFRA findings on the umbrella-owned auxiliary
# workloads that the first sec-hardening pass (sec-hardening-test.sh) did NOT
# reach — the Ory kratos-selfservice-ui sub-chart.
#
# Здесь же проверялся этаж PSS у задания доверия издателям (federation-in).
# Задание снято вместе со своим предметом: перечень доверенных издателей стал
# нашей таблицей (#1124), и запись у поставщика на решение о доступе не влияет.
# Утверждение о нём снято ВМЕСТЕ с ним — проба, чей предмет исчез, либо
# утверждает несуществующее, либо не может упасть.
#
#   1. kratos-selfservice-ui COOKIE_SECRET / CSRF_COOKIE_SECRET no longer ship a
#      git-committed default. The chart FAILS render when the cookie secret is
#      unset (enabled but neither .existingSecret nor .value given), wires a
#      secretKeyRef under the prod profile, and honours a dev inline .value.
#   2. NODE_TLS_REJECT_UNAUTHORIZED=0 is gated behind insecureSkipTLSVerify — it
#      is ABSENT under the prod profile (cert verification stays ON) and present
#      only on the dev stand.
#   3. kratos-selfservice-ui carries the
#      restricted securityContext floor (runAsNonRoot, runAsUser!=0,
#      readOnlyRootFilesystem, drop ALL caps, allowPrivilegeEscalation=false,
#      seccompProfile RuntimeDefault) on every container.
#
# ─────────────────────────────────────────────────────────────────────────────
# ИСХОДОВ ТРИ, И ОНИ РАЗЛИЧИМЫ (#1187)
#
# До этой правки исходов наблюдалось два, и они были неразличимы. `render()`
# гасила stderr (`2>/dev/null`), файл идёт под `set -e`, поэтому отказ helm по
# причине, НЕ относящейся к предмету проверки — зависимости чарта не собраны, —
# убивал прогон на первом же вызове, НЕ СКАЗАВ НИЧЕГО: код 1, ноль байт вывода.
# «Гейт нашёл дефект», «гейт сам сломан» и «условие для гейта не создано» давали
# ОДИН наблюдаемый результат, и читатель шёл искать дефект в дереве, которого
# там нет.
#
#   0 — зелёно. Печатается перепись: сколько утверждений выполнено из скольких
#       объявленных и сколько рендеров сделано.
#   1 — НАХОДКА О ДЕРЕВЕ: `FAIL: <причина>`. Утверждение не выполнилось, либо
#       выполнилось не всё объявленное (секцию удалили — молчание не является
#       доказательством).
#   2 — «УСЛОВИЕ НЕ СОЗДАНО» / отказ инструмента: `FATAL: <причина>` плюс ТЕКСТ,
#       который напечатал сам helm. Зависимости не собраны, нет helm, нет yq, в
#       PATH не тот yq, профиля нет на диске. Это НЕ вердикт о дереве.
#
# Двойка, а не тройка: ровно этот код у соседнего prod-profile-fail-closed-test.sh
# и у tests/helm/stacks.sh для той же категории. Второй контракт кодов в одном
# каталоге был бы двумя местами об одном предмете.
#
# ДИАГНОСТИКА ИДЁТ В stderr — И ЭТО НЕ СТИЛЬ. Половина вызовов здесь стоит
# внутри подстановки (`UI_PROD=$(render …)`), а всё, что функция печатает в
# stdout изнутри `$( )`, попадает В ПЕРЕМЕННУЮ, а не читателю. Проверено:
# `r() { echo x; exit 2; }; V=$(r)` под `set -e` даёт код 2 и НОЛЬ БАЙТ вывода —
# то есть наивная починка «убрать 2>/dev/null» вернула бы текст helm, но
# по-прежнему проглотила бы собственное сообщение гейта. В stderr этот класс
# невозможен by construction, поэтому туда пишут ОБЕ функции вердикта.
#
# ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ПЕРЕД СЕКЦИЕЙ 1a — ВТОРАЯ ПОЛОВИНА ТОГО ЖЕ ДЕФЕКТА.
# Секция 1a утверждает ОТРИЦАНИЕ: «рендер обязан отказать, когда cookie-секрет
# не задан». Без собранных зависимостей рендер отказывает ВСЕГДА, поэтому
# отрицание проходило ВАКУУМНО: замерено — на дереве без зависимостей секция 1a
# засчитывалась (`N=1`), и гейт отчитывался о проверенном свойстве безопасности,
# не проверив ничего. Разделяет эти два случая только положительный контроль:
# сперва тот же рендер БЕЗ накладки обязан пройти, и лишь тогда отказ накладки
# приписывается накладке.
#
# Самопроверка: --self-test (три исхода обязаны быть различимы; инъекции в обе
# стороны идут в КОПИЮ дерева, живой рабочей копии самопроверка не касается).
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
DEV="$UMBRELLA/values.dev.yaml"
PROD="$UMBRELLA/values.prod.yaml"
UI_TMPL="charts/kratos-selfservice-ui/templates/deployment.yaml"

# ── Перепись и вердикт — по счётчикам, никогда по «дошли до конца» ───────────
N=0
RENDERS=0
EXPECTED_ASSERTIONS=6
census() { echo "  перепись: утверждений выполнено $N из $EXPECTED_ASSERTIONS; рендеров helm: $RENDERS" >&2; }
fail()  { echo "FAIL: $1" >&2; census; exit 1; }
fatal() { echo "FATAL: $1" >&2; census; exit 2; }
ok() { N=$((N + 1)); }

# ── Предпосылки инструмента — это «условие не создано», а не находка ─────────
command -v helm >/dev/null 2>&1 || fatal "нужен helm — рендерить нечем"
# yq MUST be mikefarah v4. On many machines /usr/bin/yq is the python jq-wrapper
# of the SAME NAME: its filter syntax is incompatible, so it errors to stderr and
# prints NOTHING on stdout. Assertions shaped like `[ -z "$(yq ... 2>/dev/null)" ]`
# then pass VACUOUSLY — the check reports success having verified nothing at all.
# That false-green is the exact class these hardening tests exist to prevent, so
# detect the impostor explicitly instead of trusting `command -v`.
command -v yq >/dev/null 2>&1 || fatal "yq not installed (mikefarah yq v4 required)"
# Сравнение — БЕЗ трубы: `… | grep -q` под `set -o pipefail` возвращает ОТКАЗ
# НА СОВПАДЕНИИ (grep выходит по первому попаданию, писатель получает SIGPIPE,
# и `pipefail` поднимает ЕГО статус до статуса конвейера). Задача #658.
YQ_VER="$(yq --version 2>&1 || true)"
[[ "${YQ_VER,,}" == *mikefarah* ]] || fatal \
  "wrong 'yq' on PATH ($(command -v yq)): '$(yq --version 2>&1 | head -1)'. \
mikefarah yq v4 is required — the python-yq jq wrapper emits empty output on these \
filters, which would make the assertions below pass without checking anything."

# helm_try <values> <tmpl> [доп. -f …] — рендер, чей КОД ВОЗВРАТА берётся КАК
# ДАННЫЕ, а не как условие продолжения. Сама ничего не решает: «отказал» и
# «отказал по НАШЕЙ причине» — разные вопросы, и второй решает вызывающий.
#
# РЕЗУЛЬТАТ ОТДАЁТСЯ ГЛОБАЛЬНОЙ ПЕРЕМЕННОЙ, А НЕ В stdout — и это не вкус.
# Вызов вида `V=$(helm_try …)` исполняет функцию в ПОДОБОЛОЧКЕ: и код возврата
# helm, и текст его отказа, и счётчик рендеров остались бы там и до вызывающего
# не доехали. Тогда различать три исхода было бы нечем — то есть вернулся бы
# ровно тот дефект, ради которого эта правка сделана.
HELM_OUT=""
HELM_ERR=""
HELM_RC=0
helm_try() {
  local values="$1" tmpl="$2"; shift 2
  local errf
  errf="$(mktemp)"
  HELM_OUT="$(helm template kacho-umbrella "$UMBRELLA" -f "$values" "$@" --show-only "$tmpl" 2>"$errf")" \
    && HELM_RC=0 || HELM_RC=$?
  RENDERS=$((RENDERS + 1))
  # Предупреждение про kubeconfig helm печатает всегда, и оно не про отказ.
  HELM_ERR="$(grep -vE 'WARNING: Kubernetes configuration' "$errf" || true)"
  rm -f "$errf"
}

# render_or_fatal <что рендерили> — превращает отказ helm в исход «условие не
# создано» (код 2) и ПЕЧАТАЕТ ТЕКСТ, который сказал сам helm. Без этого текста
# читатель получает голое «render failed» и идёт искать дефект в дереве.
render_or_fatal() {
  [ "$HELM_RC" -eq 0 ] && return 0
  echo "--- helm отказал: $1 ---" >&2
  if [ -n "$HELM_ERR" ]; then printf '%s\n' "$HELM_ERR" >&2; else echo "(helm не сказал ничего)" >&2; fi
  echo "--- конец текста helm ---" >&2
  fatal "helm template отказал на «$1» — это УСЛОВИЕ прогона, а не свойство дерева. \
Зависимости умбреллы в git не лежат; соберите их: bash deploy/scripts/helm-umbrella-deps.sh"
}

# env_val <doc> <container> <env-name> — prints .value of the named env var.
env_val() {
  echo "$1" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[] | select(.name==\"$2\") | .env[] | select(.name==\"$3\") | .value" - 2>/dev/null
}
# env_secret_ref <doc> <container> <env-name> — prints valueFrom.secretKeyRef.name.
env_secret_ref() {
  echo "$1" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[] | select(.name==\"$2\") | .env[] | select(.name==\"$3\") | .valueFrom.secretKeyRef.name" - 2>/dev/null
}

# assert_sc <doc> <container-name> <where> — restricted floor on a container.
assert_sc() {
  local doc="$1" cname="$2" where="$3" sc
  sc=$(echo "$doc" | yq eval-all \
    "select(.kind==\"Deployment\" or .kind==\"Job\") | (.spec.template.spec.containers[], .spec.template.spec.initContainers[]) | select(.name==\"$cname\") | .securityContext" - 2>/dev/null)
  [ -n "$sc" ] && [ "$sc" != "null" ] || fail "$where: container '$cname' has no securityContext"
  [ "$(echo "$sc" | yq '.runAsNonRoot')" = "true" ] || fail "$where/$cname: runAsNonRoot != true"
  [ "$(echo "$sc" | yq '.runAsUser')" != "0" ] || fail "$where/$cname: runAsUser == 0"
  [ "$(echo "$sc" | yq '.runAsUser')" != "null" ] || fail "$where/$cname: runAsUser unset"
  [ "$(echo "$sc" | yq '.readOnlyRootFilesystem')" = "true" ] || fail "$where/$cname: readOnlyRootFilesystem != true"
  [ "$(echo "$sc" | yq '.allowPrivilegeEscalation')" = "false" ] || fail "$where/$cname: allowPrivilegeEscalation != false"
  [ "$(echo "$sc" | yq '.capabilities.drop[0]')" = "ALL" ] || fail "$where/$cname: capabilities.drop != [ALL]"
  [ "$(echo "$sc" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "$where/$cname: seccompProfile.type != RuntimeDefault"
  ok
}

# ═════════════════════════════════════════════════════════════════════════════
# САМОПРОВЕРКА — три исхода обязаны быть РАЗЛИЧИМЫ, инъекции в обе стороны
#
# Гейт, доказанный с одной стороны, ловит форму, а не существо. Здесь четыре
# случая: два законных входа (зелёный прогон; отказ ЧУЖОЙ причины) и две
# инъекции (настоящий дефект чарта; удалённая секция).
#
# ИНЪЕКЦИЯ ИДЁТ В КОПИЮ ДЕРЕВА (#696). Правка живых отслеживаемых файлов с
# возвратом по ловушке не закрывает снятие, которое не перехватывается (SIGKILL,
# нехватка памяти или места) — и тогда в дереве остаётся ровно тот дефект,
# который этот гейт и ловит. Копия закрывает класс by construction.
# ═════════════════════════════════════════════════════════════════════════════
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: три исхода (0 / 1 / 2) обязаны быть различимы ==="
  st_rc=0
  st_checked=0
  WORK="$(mktemp -d)"
  trap 'rm -rf "$WORK"' EXIT
  mkdir -p "$WORK/tests/helm"
  cp -r "$REPO_ROOT/helm" "$WORK/helm" || fatal "копия чартов не собрана — инъекциям некуда идти"
  [ -d "$WORK/helm/umbrella" ] || fatal "в копии нет умбреллы ($WORK/helm/umbrella)"
  cp "$0" "$WORK/tests/helm/$SCRIPT"
  SUT="$WORK/tests/helm/$SCRIPT"

  # probe <метка> <ожидаемый код> <обязательная подстрока> — гоняет ИСПЫТУЕМЫЙ
  # СКРИПТ ЦЕЛИКОМ (не свои функции) и судит по трём вещам сразу: код возврата,
  # НЕПУСТОТА вывода и присутствие названного текста. Непустота — предикат
  # снятия #1187 дословно: `wc -c` вывода > 0 при любом ненулевом коде.
  st_probe() {
    local label="$1" want_rc="$2" want_txt="$3" out rc bytes
    st_checked=$((st_checked + 1))
    out="$(bash "$SUT" 2>&1)" && rc=0 || rc=$?
    bytes=${#out}
    if [ "$rc" -ne "$want_rc" ]; then
      echo "  ✗ $label — код $rc, ожидался $want_rc"
      printf '%s\n' "$out" | sed 's/^/      /'
      st_rc=1
      return
    fi
    if [ "$rc" -ne 0 ] && [ "$bytes" -eq 0 ]; then
      echo "  ✗ $label — ненулевой код при НУЛЕ БАЙТ вывода (это и есть #1187)"
      st_rc=1
      return
    fi
    case "$out" in
      *"$want_txt"*) echo "  ✓ $label — код $rc, вывод $bytes б., назван '$want_txt'" ;;
      *) echo "  ✗ $label — код $rc верен, но в выводе нет '$want_txt':"
         printf '%s\n' "$out" | sed 's/^/      /'
         st_rc=1 ;;
    esac
  }

  echo
  echo "-- законный вход: чужая причина (зависимости чарта не собраны) --"
  # Ровно состояние из #1187: свежая рабочая копия, `charts/*.tgz` в git не лежат.
  rm -f "$WORK/helm/umbrella/charts/"*.tgz
  st_probe "зависимости не собраны → «условие не создано»" 2 "--- helm отказал"

  # Материализация зависимостей В КОПИИ. Единственный владелец операции —
  # scripts/helm-umbrella-deps.sh; своей копии `helm dep update` здесь не
  # заводится. Его код 3 означает «удалённый источник не ответил» — это НЕ
  # находка о дереве, и самопроверка обязана сказать то же самое, а не покраснеть.
  if compgen -G "$REPO_ROOT/helm/umbrella/charts/*.tgz" >/dev/null; then
    cp "$REPO_ROOT/helm/umbrella/charts/"*.tgz "$WORK/helm/umbrella/charts/"
  else
    echo
    echo "-- зависимостей нет и в живом дереве: материализую в КОПИИ --"
    bash "$REPO_ROOT/scripts/helm-umbrella-deps.sh" "$WORK/helm/umbrella" && deps_rc=0 || deps_rc=$?
    case "$deps_rc" in
      0) ;;
      3) fatal "источник чартов не ответил — условие для самопроверки не создано (не находка о дереве)" ;;
      *) fatal "материализация зависимостей в копии отказала (код $deps_rc)" ;;
    esac
  fi

  echo
  echo "-- законный вход: нетронутый чарт --"
  st_probe "нетронутый чарт → зелено" 0 "PASS: $SCRIPT"

  echo
  echo "-- инъекция: удалена секция (молчание не доказательство) --"
  SAVED_SUT="$(mktemp)"; cp "$SUT" "$SAVED_SUT"
  sed -i '/^# ── 3b\./,/^ok$/d' "$SUT"
  st_probe "секция 3b удалена → находка о дереве" 1 "утверждений выполнено"
  cp "$SAVED_SUT" "$SUT"

  echo
  echo "-- инъекция: настоящий дефект чарта --"
  # Проверка TLS выключается безусловно → NODE_TLS_REJECT_UNAUTHORIZED=0
  # приезжает в ПРОДОВЫЙ рендер. Ровно тот дефект, ради которого секция 2 есть.
  UI_DEPLOY="$WORK/helm/umbrella/charts/kratos-selfservice-ui/templates/deployment.yaml"
  [ -f "$UI_DEPLOY" ] || fatal "в копии нет шаблона $UI_DEPLOY — инъектировать нечего"
  sed -i 's/{{- if \.Values\.kratosSelfServiceUI\.insecureSkipTLSVerify }}/{{- if true }}/' "$UI_DEPLOY"
  grep -q '{{- if true }}' "$UI_DEPLOY" || fatal "инъекция не внеслась — образец в шаблоне переехал, проверять нечего"
  st_probe "проверка TLS снята в prod → находка о дереве" 1 "NODE_TLS_REJECT_UNAUTHORIZED"

  echo
  echo "случаев проверено: $st_checked (законных 2, инъекций 2)"
  [ "$st_checked" -eq 4 ] || { echo "FAIL: исполнено $st_checked случаев из 4"; st_rc=1; }
  [ $st_rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $st_rc
fi

[ -f "$DEV" ]  || fatal "values.dev.yaml нет на диске ($DEV)"
[ -f "$PROD" ] || fatal "values.prod.yaml нет на диске ($PROD)"

# ── 0. ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: чарт обязан рендериться ВООБЩЕ ────────────────
# Секция 1a ниже утверждает ОТРИЦАНИЕ («рендер обязан отказать»). Пока не
# доказано, что тот же рендер БЕЗ накладки проходит, отказ накладки ничего не
# значит: без собранных зависимостей отказывает вообще всё, и отрицание проходит
# вакуумно. Это не предположение — замерено: на дереве без зависимостей секция
# 1a засчитывалась, а прогон умирал следующей строкой, ничего не напечатав.
#
# Отказ здесь — исход «условие не создано» (код 2), а не находка о дереве.
helm_try "$DEV" "$UI_TMPL"
render_or_fatal "values.dev.yaml → $UI_TMPL (положительный контроль)"
UI_DEV="$HELM_OUT"
[ -n "$UI_DEV" ] || fatal "рендер values.dev.yaml → $UI_TMPL ПУСТ — подчарт kratos-selfservice-ui выключен на этом профиле, проверять нечего"

# ── 1a. cookieSecret unset (enabled, no source) → render MUST fail ────────────
# Layer an explicitly-empty cookieSecret over the otherwise-complete dev profile
# so only the missing cookie secret can trip the render (everything else renders).
EMPTY_OVERLAY="$(mktemp)"
trap 'rm -f "$EMPTY_OVERLAY"' EXIT
cat >"$EMPTY_OVERLAY" <<'EOF'
kratos-selfservice-ui:
  kratosSelfServiceUI:
    enabled: true
    cookieSecret:
      existingSecret: ""
      existingSecretKey: cookieSecret
      value: ""
EOF
helm_try "$DEV" "$UI_TMPL" -f "$EMPTY_OVERLAY"
[ "$HELM_RC" -ne 0 ] || fail "kratos-ui: render succeeded with cookieSecret unset — expected fail-closed"
# И отказал ПО ТОЙ ПРИЧИНЕ, о которой секция. Текст — наш собственный (`fail` в
# шаблоне подчарта), не helm'а, поэтому сверка с ним не хрупка к версии helm, а
# является сверкой с контрактом чарта. Без неё утверждение принимает ЛЮБОЙ
# отказ — включая тот, которым #1187 и был.
case "$HELM_ERR" in
  *cookieSecret*) ;;
  *) echo "--- helm отказал: values.dev.yaml + пустой cookieSecret ---" >&2
     printf '%s\n' "${HELM_ERR:-(helm не сказал ничего)}" >&2
     echo "--- конец текста helm ---" >&2
     fail "kratos-ui: рендер отказал НЕ из-за cookieSecret — отрицание секции 1a проходило бы вакуумно" ;;
esac
ok

# ── 1b. prod profile wires COOKIE_SECRET + CSRF_COOKIE_SECRET via secretKeyRef ─
helm_try "$PROD" "$UI_TMPL"
render_or_fatal "values.prod.yaml → $UI_TMPL"
UI_PROD="$HELM_OUT"
[ -n "$UI_PROD" ] || fatal "kratos-ui: prod profile rendered empty (sub-chart disabled?)"
CS_REF=$(env_secret_ref "$UI_PROD" ui COOKIE_SECRET)
CSRF_REF=$(env_secret_ref "$UI_PROD" ui CSRF_COOKIE_SECRET)
[ -n "$CS_REF" ] && [ "$CS_REF" != "null" ] || fail "kratos-ui/prod: COOKIE_SECRET not via secretKeyRef"
[ -n "$CSRF_REF" ] && [ "$CSRF_REF" != "null" ] || fail "kratos-ui/prod: CSRF_COOKIE_SECRET not via secretKeyRef"
# No committed literal cookie secret must survive in the prod render.
[[ "$UI_PROD" == *"please-change-this-32-bytes"* ]] \
  && fail "kratos-ui/prod: committed default cookie secret still present"
ok

# ── 1c. dev inline .value renders as a literal COOKIE_SECRET/CSRF value ───────
[ -n "$(env_val "$UI_DEV" ui COOKIE_SECRET)" ] || fail "kratos-ui/dev: COOKIE_SECRET inline value missing"
[ -n "$(env_val "$UI_DEV" ui CSRF_COOKIE_SECRET)" ] || fail "kratos-ui/dev: CSRF_COOKIE_SECRET inline value missing"
ok

# ── 2. NODE_TLS_REJECT_UNAUTHORIZED gate ─────────────────────────────────────
# prod: verification stays ON → the disable-env must be ABSENT.
[ -z "$(env_val "$UI_PROD" ui NODE_TLS_REJECT_UNAUTHORIZED)" ] \
  || fail "kratos-ui/prod: NODE_TLS_REJECT_UNAUTHORIZED present (cert verification disabled in prod)"
# dev: opt-in flag set → the disable-env is present with value 0.
[ "$(env_val "$UI_DEV" ui NODE_TLS_REJECT_UNAUTHORIZED)" = "0" ] \
  || fail "kratos-ui/dev: NODE_TLS_REJECT_UNAUTHORIZED != 0 (dev opt-in expected)"
ok

# ── 3a. restricted securityContext floor on the container ────────────────────
assert_sc "$UI_DEV" ui "kratos-ui"

# ── 3b. pod-level floor on kratos-ui ─────────────────────────────────────────
UI_POD_SC=$(echo "$UI_DEV" | yq 'select(.kind=="Deployment") | .spec.template.spec.securityContext')
[ "$(echo "$UI_POD_SC" | yq '.runAsNonRoot')" = "true" ] || fail "kratos-ui: pod runAsNonRoot != true"
[ "$(echo "$UI_POD_SC" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "kratos-ui: pod seccomp != RuntimeDefault"
ok

# ── Вердикт — по счётчику, никогда по «дошли до конца» ───────────────────────
# Секция, которую удалили или закомментировали, НЕ имеет права оставить за собой
# зелёный вердикт: её утверждения не исполнялись, и молчание о них ничего не
# доказывает. Это находка о дереве (код 1), а не «условие не создано».
if [ "$N" -ne "$EXPECTED_ASSERTIONS" ]; then
  echo "FAIL: $SCRIPT — утверждений выполнено $N из $EXPECTED_ASSERTIONS: секция пропущена или удалена, её утверждения не исполнялись" >&2
  exit 1
fi
echo "PASS: $SCRIPT ($N assertions) — рендеров helm: $RENDERS; профилей прочитано: 2 (dev, prod)"
