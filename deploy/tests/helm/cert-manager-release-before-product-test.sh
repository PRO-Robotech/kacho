#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# cert-manager ставится ОТДЕЛЬНЫМ релизом ДО продукта на КАЖДОМ пути подъёма —
# и этот порядок записан в Makefile ОДИН раз (kacho#2840).
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# `dev-up` ставил cert-manager своим релизом, ждал его вебхук и лишь потом
# применял продукт. `stack-up` применял ту же умбреллу с
# `cert-manager.enabled=false` (UMBRELLA_OPTS) и отдельного релиза не ставил
# вовсе. На кластере без cert-manager первое же применение цепочки упиралось в
# отсутствие CRD `Certificate`/`Issuer`: порядок жил копией в одном рецепте из
# двух, и у второго копии не было.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО УТВЕРЖДАЕТСЯ
#
# Часть А — по ИСПОЛНЯЕМОЙ части Makefile (комментарии и echo вырезаны):
#   А1. релиз cert-manager ставит ровно ОДНА строка дерева, и живёт она в цели
#       `cert-manager-up` — второй копии порядка нет;
#   А2. `dev-up` зовёт `cert-manager-up` РАНЬШЕ своего helm-прогона продукта;
#   А3. `stack-up` — то же.
#
# Часть Б — ПОВЕДЕНИЕ цели `cert-manager-up` против подставных kubectl и helm
# (настоящий рецепт, кластер не нужен):
#   Б1. CRD нет                               → ставит релиз и ждёт вебхук;
#   Б2. CRD наш, релиз той же версии, deployed → НЕ переставляет, ждёт вебхук;
#   Б3. CRD наш, релиз другой версии           → ставит (перекат версии);
#   Б4. CRD принадлежит ЧУЖОМУ релизу          → не ставит, называет владельца;
#   Б5. наличие не прочитано (API не ответил)  → отказ, релиз НЕ ставится.
#
# Б5 — не формальность: «не прочитано» обязано быть отличимо от «нет». Спутай
# их — и цель на кластере с недоступным API поставит второй cert-manager туда,
# где первый уже стоит (a8f60d: оператор держит свой в ns beget-cert-manager).
#
# Три исхода — общей библиотекой каталога: 0 зелено · 1 находка · 2 условие
# не создано. Офлайновая проверка, как и остальные tests/helm/*.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$(basename "$0")"
MAKEFILE="${MAKEFILE_UNDER_TEST:-$DEPLOY_ROOT/Makefile}"

. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=10

good() { echo "  ✓ $1"; }

command -v make >/dev/null 2>&1 || fatal "нет make — цель cert-manager-up запускать нечем"
require_python3
require_helm
require_file_present "$MAKEFILE" "Makefile стенда"
# Имя релиза и архив чарта берутся у Makefile, а не выписываются здесь: иначе
# переименование релиза разошлось бы с пробой молча, а радиус переименования
# перестал бы быть одним файлом.
REL="$(sed -n 's/^CERT_MANAGER_RELEASE[[:space:]]*:=[[:space:]]*//p' "$MAKEFILE")"
CHART_REL="$(sed -n 's/^CERT_MANAGER_CHART[[:space:]]*:=[[:space:]]*//p' "$MAKEFILE")"
[ -n "$REL" ] && [ -n "$CHART_REL" ] \
  || fatal "в Makefile не прочитаны CERT_MANAGER_RELEASE/CERT_MANAGER_CHART — сверять не с чем"
CHART="$DEPLOY_ROOT/${CHART_REL#./}"
require_file_present "$CHART" "архив чарта cert-manager (CERT_MANAGER_CHART)"

REAL_HELM="$(command -v helm)"
CHART_VERSION="$("$REAL_HELM" show chart "$CHART" 2>/dev/null | sed -n 's/^version: *//p' | head -1)"
[ -n "$CHART_VERSION" ] || fatal "версия чарта cert-manager не прочиталась из $CHART — сравнивать не с чем"

TMP="$(mktemp -d)" || fatal "не создан временный каталог — подставные kubectl/helm положить некуда"
trap 'rm -rf "$TMP"' EXIT

# ─── Часть А: исполняемая часть Makefile ─────────────────────────────────────
#
# Разбор делает python: рецепт — `; \`-цепочка, и «какая строка раньше» внутри
# цели судится порядком строк её тела, а не поиском по файлу. Тело цели —
# строки, начинающиеся с TAB, вслед за строкой `имя:`; комментарии и
# аргументы echo/printf вырезаются, иначе подсказка оператору читалась бы как
# действие.
static_out="$(python3 - "$MAKEFILE" <<'PY'
import re, sys
path = sys.argv[1]
try:
    lines = open(path, encoding="utf-8").read().split("\n")
except OSError as e:
    print(f"UNREAD|{e}")
    sys.exit(0)

targets, cur = {}, None
head = re.compile(r"^([A-Za-z0-9_.-]+):(?!=)")
for raw in lines:
    m = head.match(raw)
    if m and not raw.startswith("\t"):
        cur = m.group(1)
        targets.setdefault(cur, [])
        continue
    if raw.startswith("\t") and cur:
        t = raw.strip()
        if t.startswith("#"):
            continue
        t = re.sub(r'\b(echo|printf)\s+("([^"\\]|\\.)*"|\'[^\']*\')', "", t)
        targets[cur].append(t)
    elif raw and not raw.startswith("\t") and not raw.startswith("#"):
        cur = None if not head.match(raw) else cur

print(f"TARGETS|{len(targets)}")
install = re.compile(r"helm\s+upgrade\s+--install\s+\$\(CERT_MANAGER_RELEASE\)")
sites = [(n, i) for n, body in targets.items() for i, l in enumerate(body) if install.search(l)]
print("INSTALL|" + ",".join(n for n, _ in sites))

def order(target, product):
    body = targets.get(target)
    if body is None:
        return "NO_TARGET"
    call = next((i for i, l in enumerate(body) if re.search(r"\$\(MAKE\)[^;]*\bcert-manager-up\b", l)), None)
    prod = next((i for i, l in enumerate(body) if re.search(product, l)), None)
    if prod is None:
        return "NO_PRODUCT"
    if call is None:
        return "NO_CALL"
    return "BEFORE" if call < prod else f"AFTER({call}>{prod})"

print("DEVUP|" + order("dev-up", r"helm\s+upgrade\s+--install\s+kacho-umbrella\b"))
print("STACKUP|" + order("stack-up", r"helm\s+upgrade\s+--install\s+\$\(STACK_RELEASE\)"))
PY
)" || fatal "разбор Makefile не состоялся (python3 отказал)"

field() { printf '%s\n' "$static_out" | sed -n "s/^$1|//p" | head -1; }

case "$(field UNREAD)" in "") ;; *) fatal "Makefile не прочитан: $(field UNREAD)";; esac
[ "${static_out}" != "" ] && [ "$(field TARGETS)" -gt 0 ] 2>/dev/null \
  || fatal "в Makefile не найдено НИ ОДНОЙ цели — разбор перестал узнавать файл, а не файл стал чистым"

echo "=== $SCRIPT: часть А — исполняемая часть $MAKEFILE (целей разобрано: $(field TARGETS)) ==="

ok
install_sites="$(field INSTALL)"
if [ "$install_sites" = "cert-manager-up" ]; then
  good "А1: релиз cert-manager ставит ровно одна строка — в цели cert-manager-up"
else
  violation "А1: строки, ставящие релиз cert-manager, найдены в целях «${install_sites:-<ни в одной>}»; обязана быть ровно одна, в cert-manager-up"
fi

ok
case "$(field DEVUP)" in
  BEFORE) good "А2: dev-up зовёт cert-manager-up раньше helm-прогона продукта" ;;
  *) violation "А2: dev-up — $(field DEVUP) (обязан звать cert-manager-up РАНЬШЕ helm-прогона продукта)" ;;
esac

ok
case "$(field STACKUP)" in
  BEFORE) good "А3: stack-up зовёт cert-manager-up раньше helm-прогона продукта" ;;
  *) violation "А3: stack-up — $(field STACKUP) (обязан звать cert-manager-up РАНЬШЕ helm-прогона продукта)" ;;
esac

# ─── Часть Б: поведение цели против подставных kubectl и helm ───────────────
# Подставные kubectl и helm НЕ снисходительнее настоящих и в разборе ФЛАГОВ:
# каждый вызов сперва проходит разбор настоящего инструмента (`<args> --help`
# разбирает флаги и ничего не делает), и незнакомый ему флаг — отказ с его же
# текстом. Без этого подставной helm принимал `list -a`, которого у helm v4 нет,
# и ветка «наш релиз» зеленела здесь, отказывая на живом кластере.
REAL_KUBECTL="$(command -v kubectl)" || fatal "нет kubectl — разбирать его флаги нечем"
mkdir -p "$TMP/bin"
cat >"$TMP/bin/kubectl" <<STUB
#!/usr/bin/env bash
echo "kubectl \$*" >>"\$STUB_LOG"
if ! err="\$("$REAL_KUBECTL" "\$@" --help 2>&1 >/dev/null)"; then echo "\$err" >&2; exit 1; fi
exec "$TMP/bin/kubectl-answer" "\$@"
STUB
cat >"$TMP/bin/kubectl-answer" <<'STUB'
#!/usr/bin/env bash
args="$*"
case "$args" in
  "config current-context") echo "kind-stub"; exit 0 ;;
esac
# Подставной kubectl НЕ снисходительнее настоящего: без --ignore-not-found
# отсутствующий объект — это отказ с кодом 1, как у kubectl, а вывод отдаётся
# только в той форме, о которой спросили (-o json).
crd_json() { # <релиз> <namespace>
  printf '{"kind":"CustomResourceDefinition","metadata":{"name":"certificates.cert-manager.io","annotations":{"meta.helm.sh/release-name":"%s","meta.helm.sh/release-namespace":"%s"}}}\n' "$1" "$2"
}
case "$args" in
  *"get crd certificates.cert-manager.io"*)
    case "$args" in *"-o json"*) ;; *) echo "stub: спрошена не та форма вывода: $args" >&2; exit 3 ;; esac
    case "$STUB_MODE" in
      absent)
        case "$args" in *--ignore-not-found*) exit 0 ;; esac
        echo 'Error from server (NotFound): customresourcedefinitions.apiextensions.k8s.io "certificates.cert-manager.io" not found' >&2
        exit 1 ;;
      ours-same|ours-drift) crd_json "$STUB_REL" kacho; exit 0 ;;
      foreign) crd_json beget-cert-manager beget-cert-manager; exit 0 ;;
      unreachable) echo "The connection to the server 127.0.0.1:6443 was refused" >&2; exit 1 ;;
    esac ;;
  *"rollout status"*) echo "deployment successfully rolled out"; exit 0 ;;
esac
exit 0
STUB
cat >"$TMP/bin/helm" <<STUB
#!/usr/bin/env bash
echo "helm \$*" >>"\$STUB_LOG"
if ! err="\$("$REAL_HELM" "\$@" --help 2>&1 >/dev/null)"; then echo "\$err" >&2; exit 1; fi
case "\$1" in
  show) exec "$REAL_HELM" "\$@" ;;
  list)
    case "\$STUB_MODE" in
      ours-same)  printf '[{"name":"$REL","namespace":"kacho","status":"deployed","chart":"cert-manager-$CHART_VERSION"}]\n' ;;
      ours-drift) printf '[{"name":"$REL","namespace":"kacho","status":"deployed","chart":"cert-manager-v0.0.1"}]\n' ;;
      *) printf '[]\n' ;;
    esac
    exit 0 ;;
esac
exit 0
STUB
chmod +x "$TMP/bin/kubectl" "$TMP/bin/kubectl-answer" "$TMP/bin/helm"

run_mode() { # <режим> → код возврата цели; вывод и журнал вызовов — в $TMP
  local mode="$1"
  : >"$TMP/log.$mode"
  (cd "$DEPLOY_ROOT" && PATH="$TMP/bin:$PATH" STUB_MODE="$mode" STUB_LOG="$TMP/log.$mode" STUB_REL="$REL" \
     make --no-print-directory -f "$MAKEFILE" cert-manager-up \
       CERT_MANAGER_NAMESPACE=kacho EXPECT_CONTEXT=kind-stub) >"$TMP/out.$mode" 2>&1
}

installed() { grep -Eq "^helm upgrade --install $REL " "$TMP/log.$1"; }
waited()    { grep -Eq "^kubectl -n kacho rollout status deploy/$REL-webhook( |\$)" "$TMP/log.$1"; }

behaviour() { # <метка> <режим> <ждём код: 0|nz> <ждём установку: yes|no> <ждём ожидание вебхука: yes|no|any>
  local label="$1" mode="$2" want_rc="$3" want_install="$4" want_wait="$5" rc got_install got_wait
  ok
  run_mode "$mode" && rc=0 || rc=$?
  if installed "$mode"; then got_install=yes; else got_install=no; fi
  if waited "$mode"; then got_wait=yes; else got_wait=no; fi
  local bad=""
  if [ "$want_rc" = 0 ] && [ "$rc" -ne 0 ]; then bad="$bad код=$rc (ждали 0)"; fi
  if [ "$want_rc" = nz ] && [ "$rc" -eq 0 ]; then bad="$bad код=0 (ждали отказ)"; fi
  [ "$got_install" = "$want_install" ] || bad="$bad установка=$got_install (ждали $want_install)"
  if [ "$want_wait" != any ] && [ "$got_wait" != "$want_wait" ]; then bad="$bad ожидание-вебхука=$got_wait (ждали $want_wait)"; fi
  if [ -z "$bad" ]; then
    good "$label — код $rc, установка $got_install, ожидание вебхука $got_wait"
  else
    violation "$label —$bad. Вывод цели и журнал вызовов:"
    sed 's/^/      | /' "$TMP/out.$mode"
    sed 's/^/      > /' "$TMP/log.$mode"
  fi
}

echo "=== $SCRIPT: часть Б — цель cert-manager-up против подставных kubectl/helm (версия чарта $CHART_VERSION) ==="
behaviour "Б1: CRD нет"                                absent      0  yes yes
behaviour "Б2: наш релиз той же версии"                ours-same   0  no  yes
behaviour "Б3: наш релиз другой версии"                ours-drift  0  yes yes
behaviour "Б4: CRD принадлежит чужому релизу"          foreign     0  no  no
behaviour "Б5: наличие не прочитано (API не ответил)"  unreachable nz no  no

ok
if grep -q 'НЕ ПРОЧИТАНО' "$TMP/out.unreachable" 2>/dev/null; then
  good "Б5: отказ называет причину — наличие cert-manager не прочитано"
else
  violation "Б5: отказ не говорит, что наличие НЕ ПРОЧИТАНО — «не ответил» неотличимо от любого другого отказа:"
  sed 's/^/      | /' "$TMP/out.unreachable" 2>/dev/null | tail -5
fi

ok
if grep -q 'beget-cert-manager' "$TMP/out.foreign" 2>/dev/null; then
  good "Б4: вывод называет владельца чужого cert-manager"
else
  violation "Б4: вывод не называет, ЧЕЙ cert-manager стоит — оператор не отличит «чужой» от «никакого»:"
  sed 's/^/      | /' "$TMP/out.foreign" 2>/dev/null | tail -5
fi

echo
echo "проверок исполнено: $N из $EXPECTED_ASSERTIONS"
findings_verdict "режимов подставного кластера: 5 (absent, ours-same, ours-drift, foreign, unreachable)"
