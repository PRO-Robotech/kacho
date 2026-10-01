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
# Часть А — по ИСПОЛНЯЕМОЙ части Makefile. Рецепт разбирается на простые
# команды (разделители `;`, `&&`, `||`, `|` вне кавычек, продолжения строк
# склеены); команда `echo`/`printf` снимается ЦЕЛИКОМ со всеми аргументами, а не
# первым строковым литералом — иначе `echo $(MAKE) cert-manager-up` без кавычек
# или второй аргумент `printf` читались бы как вызов:
#   А1. релиз cert-manager ставит ровно ОДНА команда дерева, и живёт она в цели
#       `cert-manager-up` — второй копии порядка нет. Установкой считается
#       `helm upgrade` (с `--install`, `-i` или без) и `helm install`, чей
#       релиз либо чарт — cert-manager: переменной Makefile ИЛИ литералом;
#   А2. `dev-up` зовёт `cert-manager-up` РАНЬШЕ своего helm-прогона продукта;
#   А3. `stack-up` — то же.
#
# Часть Б — ПОВЕДЕНИЕ цели `cert-manager-up` против подставных kubectl и helm
# (настоящий рецепт, кластер не нужен):
#   Б1. CRD нет                                → ставит релиз и ждёт вебхук;
#   Б2. CRD наш, релиз той же версии, deployed  → НЕ переставляет, ждёт вебхук;
#   Б3. CRD наш, релиз другой версии            → ставит (перекат версии);
#   Б4. CRD принадлежит ЧУЖОМУ релизу helm      → не ставит, называет владельца;
#   Б5. наличие не прочитано (API не ответил)   → отказ, релиз НЕ ставится;
#   Б6. CRD есть, записи владельца helm нет     → не ставит, говорит «не helm»;
#   Б7. ответ API о CRD не разобран как JSON    → отказ, релиз НЕ ставится;
#   Б8. CRD наш, релиз той же версии, НЕ deployed → ставит (доводит).
#
# Б5 и Б7 — не формальность: «не прочитано» обязано быть отличимо от «нет».
# Спутай их — и цель на кластере с недоступным API поставит второй cert-manager
# туда, где первый уже стоит (a8f60d: оператор держит свой в ns
# beget-cert-manager). Б6 — та же опасность с другой стороны: cert-manager,
# поставленный не helm, записи владельца не несёт, и прочесть это как «нет»
# значит поставить второй экземпляр поверх первого.
#
# ПОДСТАВНЫЕ НЕ СНИСХОДИТЕЛЬНЕЕ НАСТОЯЩИХ. Флаги каждого вызова разбирает
# настоящий инструмент. Подставной `helm list` читает `-n` и `--filter`: в ns
# держатся ДВА релиза (продукт и cert-manager), и список без фильтра отдаёт оба.
# Пространство имён прогона — НЕ `kacho`: литерал `kacho`, подставленный на
# место переданного ns, иначе совпал бы с ним и прошёл бы незамеченным.
#
# Доказательство, что каждое утверждение способно покраснеть, — соседний
# cert-manager-release-before-product-inject.sh (девять дефектов, каждый
# прошёл прежнюю редакцию пробы зелёным).
#
# Три исхода — общей библиотекой каталога: 0 зелено · 1 находка · 2 условие
# не создано. Офлайновая проверка, как и остальные tests/helm/*.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$(basename "$0")"
MAKEFILE="${MAKEFILE_UNDER_TEST:-$DEPLOY_ROOT/Makefile}"

. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=17
# Пространство имён прогона части Б. Намеренно не `kacho` — см. шапку.
NS="cm-probe-ns"

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

# Объявленное право одобрения встроенного одобрителя — тем же чтением, что у
# рецепта: корневой выпускающий из values.yaml зонтика.
require_mikefarah_yq
ROOT_ISSUER="$(yq -r '.mtls.internalCA.rootIssuerName // ""' "$DEPLOY_ROOT/helm/umbrella/values.yaml")"
[ -n "$ROOT_ISSUER" ] || fatal "mtls.internalCA.rootIssuerName не прочитан из values.yaml — объявленное право одобрения сверять не с чем"
APPROVE="clusterissuers.cert-manager.io/$ROOT_ISSUER"
REAL_HELM="$(command -v helm)"
CHART_VERSION="$("$REAL_HELM" show chart "$CHART" 2>/dev/null | sed -n 's/^version: *//p' | head -1)"
[ -n "$CHART_VERSION" ] || fatal "версия чарта cert-manager не прочиталась из $CHART — сравнивать не с чем"

TMP="$(mktemp -d)" || fatal "не создан временный каталог — подставные kubectl/helm положить некуда"
trap 'rm -rf "$TMP"' EXIT

# ─── Часть А: исполняемая часть Makefile ─────────────────────────────────────
#
# Разбор делает python: рецепт — `; \`-цепочка, и «какая команда раньше» внутри
# цели судится порядком команд её тела, а не поиском по файлу. Тело цели —
# строки, начинающиеся с TAB, вслед за строкой `имя:`; строки-комментарии
# снимаются, продолжения `\` склеиваются, и тело режется на простые команды.
static_out="$(python3 - "$MAKEFILE" "$REL" "$CHART_REL" <<'PY'
import bisect, re, shlex, sys
path, rel_lit, chart_lit = sys.argv[1], sys.argv[2], sys.argv[3]
try:
    lines = open(path, encoding="utf-8").read().split("\n")
except OSError as e:
    print(f"UNREAD|{e}")
    sys.exit(0)

targets, cur = {}, None
head = re.compile(r"^([A-Za-z0-9_.-]+):(?!=)")
for no, raw in enumerate(lines, 1):
    m = head.match(raw)
    if m and not raw.startswith("\t"):
        cur = m.group(1)
        targets.setdefault(cur, [])
        continue
    if raw.startswith("\t") and cur:
        if raw.strip().startswith("#"):
            continue
        targets[cur].append((no, raw[1:]))
    elif raw and not raw.startswith("\t") and not raw.startswith("#"):
        cur = None

def commands(body):
    """Простые команды тела: [(номер строки, текст)]. Продолжение `\\` склеено."""
    text, starts, nos = "", [], []
    for no, ln in body:
        starts.append(len(text)); nos.append(no)
        if ln.endswith("\\"):
            text += ln[:-1] + " "
        else:
            text += ln + "\n"
    out, buf, bstart, q, i = [], "", 0, None, 0
    def flush(at):
        nonlocal buf, bstart
        if buf.strip():
            out.append((nos[bisect.bisect_right(starts, bstart) - 1], buf.strip()))
        buf, bstart = "", at
    while i < len(text):
        c = text[i]
        if q is None and c == "\\" and i + 1 < len(text):
            buf += text[i:i + 2]; i += 2; continue
        if q == '"' and c == "\\" and i + 1 < len(text):
            buf += text[i:i + 2]; i += 2; continue
        if q is None and c in "'\"":
            q = c
        elif q == c:
            q = None
        elif q is None and (text.startswith("&&", i) or text.startswith("||", i)):
            flush(i + 2); i += 2; continue
        elif q is None and c in ";|\n":
            flush(i + 1); i += 1; continue
        buf += c
        i += 1
    flush(len(text))
    return out

LEAD = re.compile(r"^(?:[@+-]+|[{(!]|(?:then|else|elif|if|do|while|until|time|exec)(?=\s)|"
                  r"(?!\$\()[^\s()]*\))\s*")
def strip_lead(cmd):
    prev = None
    while prev != cmd:
        prev, cmd = cmd, LEAD.sub("", cmd, count=1).lstrip()
    return cmd

def words(cmd):
    try:
        return shlex.split(cmd, comments=False, posix=True)
    except ValueError:
        return cmd.split()

VALUE_FLAGS = {"-n", "--namespace", "-f", "--values", "--set", "--set-string", "--set-file",
               "--set-json", "--set-literal", "--version", "--timeout", "--kube-context",
               "--kubeconfig", "--repo", "-o", "--output", "--post-renderer",
               "--post-renderer-args", "--description", "--username", "--password",
               "--ca-file", "--cert-file", "--key-file", "--keyring", "--history-max",
               "--wait-for", "--labels", "-l"}
def helm_installs(w):
    """[(релиз, чарт)] по каждой установке helm среди слов команды."""
    found = []
    for k, t in enumerate(w):
        if t != "helm" and not t.endswith("/helm"):
            continue
        rest, sub, j = w[k + 1:], None, 0
        while j < len(rest):
            if rest[j] in VALUE_FLAGS:
                j += 2; continue
            if rest[j].startswith("-"):
                j += 1; continue
            sub = rest[j]; break
        if sub not in ("upgrade", "install"):
            continue
        pos, j = [], j + 1
        while j < len(rest) and len(pos) < 2:
            a = rest[j]
            if a in VALUE_FLAGS:
                j += 2; continue
            if a.startswith("-"):
                j += 1; continue
            pos.append(a); j += 1
        found.append((pos[0] if pos else "", pos[1] if len(pos) > 1 else ""))
    return found

CM_NAME = re.compile(r"(^|[/_-])cert-manager([._-]|$)")
def is_cert_manager(release, chart):
    return (release in ("$(CERT_MANAGER_RELEASE)", rel_lit) or CM_NAME.search(release) is not None
            or chart in ("$(CERT_MANAGER_CHART)", chart_lit) or CM_NAME.search(chart) is not None)

def is_make_call(w, target):
    while w and re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", w[0]):
        w = w[1:]
    if not w or w[0] not in ("$(MAKE)", "${MAKE}", "make"):
        return False
    args = w[1:]
    if any(a in ("-C", "-f", "--directory", "--file", "--makefile") or re.match(r"^-(C|f).+", a)
           or a.startswith(("--directory=", "--file=", "--makefile=")) for a in args):
        return False
    return target in [a for a in args if not a.startswith("-") and "=" not in a]

parsed, dropped = {}, 0
for name, body in targets.items():
    kept = []
    for no, cmd in commands(body):
        c = strip_lead(cmd)
        w = words(c)
        if w and w[0] in ("echo", "printf"):
            dropped += 1
            continue
        kept.append((no, w))
    parsed[name] = kept

print(f"TARGETS|{len(targets)}")
print(f"DROPPED|{dropped}")
sites = [f"{n}:{no}" for n, cmds in parsed.items() for no, w in cmds
         for r, c in helm_installs(w) if is_cert_manager(r, c)]
print("INSTALL|" + ",".join(sites))

def order(target, product_release):
    cmds = parsed.get(target)
    if cmds is None:
        return "NO_TARGET"
    call = next((i for i, (_, w) in enumerate(cmds) if is_make_call(w, "cert-manager-up")), None)
    prod = next((i for i, (_, w) in enumerate(cmds)
                 if any(r == product_release for r, _ in helm_installs(w))), None)
    if prod is None:
        return "NO_PRODUCT"
    if call is None:
        return "NO_CALL"
    return "BEFORE" if call < prod else f"AFTER(команда {call} > {prod})"

print("DEVUP|" + order("dev-up", "kacho-umbrella"))
print("STACKUP|" + order("stack-up", "$(STACK_RELEASE)"))
PY
)" || fatal "разбор Makefile не состоялся (python3 отказал)"

field() { printf '%s\n' "$static_out" | sed -n "s/^$1|//p" | head -1; }

case "$(field UNREAD)" in "") ;; *) fatal "Makefile не прочитан: $(field UNREAD)";; esac
[ "${static_out}" != "" ] && [ "$(field TARGETS)" -gt 0 ] 2>/dev/null \
  || fatal "в Makefile не найдено НИ ОДНОЙ цели — разбор перестал узнавать файл, а не файл стал чистым"

echo "=== $SCRIPT: часть А — исполняемая часть $MAKEFILE (целей разобрано: $(field TARGETS), команд echo/printf снято: $(field DROPPED)) ==="

ok
install_sites="$(field INSTALL)"
case "$install_sites" in
  cert-manager-up:[0-9]*)
    case "$install_sites" in
      *,*) violation "А1: команды, ставящие релиз cert-manager: «$install_sites»; обязана быть ровно одна, в cert-manager-up" ;;
      *) good "А1: релиз cert-manager ставит ровно одна команда — в цели cert-manager-up ($install_sites)" ;;
    esac ;;
  *) violation "А1: команды, ставящие релиз cert-manager: «${install_sites:-<ни одной>}»; обязана быть ровно одна, в cert-manager-up" ;;
esac

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
      ours-same|ours-drift|ours-failed|ours-wide) crd_json "$STUB_REL" "$STUB_NS"; exit 0 ;;
      foreign) crd_json beget-cert-manager beget-cert-manager; exit 0 ;;
      # Поставлен не helm (манифестом, оператором): CRD есть, записи владельца нет.
      foreign-nohelm)
        printf '{"kind":"CustomResourceDefinition","metadata":{"name":"certificates.cert-manager.io","labels":{"app.kubernetes.io/name":"cert-manager"}}}\n'
        exit 0 ;;
      # Посредник ответил своей страницей с кодом 0: ответ есть, JSON нет.
      garbled) printf '<html><body><h1>502 Bad Gateway</h1></body></html>\n'; exit 0 ;;
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
esac
exec "$TMP/bin/helm-answer" "\$@"
STUB
# Подставной `helm list` отвечает по ТЕМ ЖЕ осям, что настоящий: пространство
# имён (`-n`) и фильтр имени (`--filter`, регулярное выражение). В ns прогона
# держатся два релиза — продукт и cert-manager, — поэтому список без фильтра
# отдаёт оба, и первым стоит НЕ cert-manager.
cat >"$TMP/bin/helm-answer" <<'STUB'
#!/usr/bin/env bash
if [ "$1" = get ] && [ "$2" = values ]; then
  # Значения релиза: право одобрения встроенного одобрителя. «ours-wide» — релиз,
  # поднятый до политики выпуска (умолчание чарта: одобрять всё).
  case "$STUB_MODE" in
    ours-wide) printf '{"crds":{"enabled":true}}\n' ;;
    *) printf '{"crds":{"enabled":true},"approveSignerNames":["%s"]}\n' "$STUB_APPROVE" ;;
  esac
  exit 0
fi
[ "$1" = list ] || exit 0
shift
ns="default"; filter=""; json=no
while [ $# -gt 0 ]; do
  case "$1" in
    -n|--namespace) ns="$2"; shift 2 ;;
    --namespace=*) ns="${1#*=}"; shift ;;
    -f|--filter) filter="$2"; shift 2 ;;
    --filter=*) filter="${1#*=}"; shift ;;
    -o|--output) [ "$2" = json ] && json=yes; shift 2 ;;
    --output=json|-ojson) json=yes; shift ;;
    *) shift ;;
  esac
done
STUB_LIST_NS="$ns" STUB_LIST_FILTER="$filter" STUB_LIST_JSON="$json" exec python3 -c '
import json, os, re
m, ns, ver = os.environ["STUB_MODE"], os.environ["STUB_NS"], os.environ["STUB_CHART_VERSION"]
rows = []
if os.environ["STUB_LIST_NS"] == ns:
    rows.append({"name": "kacho-umbrella", "namespace": ns, "status": "deployed", "chart": "kacho-umbrella-0.1.0"})
    cm = {"ours-same": ("deployed", ver), "ours-drift": ("deployed", "v0.0.1"),
          "ours-failed": ("failed", ver), "ours-wide": ("deployed", ver)}.get(m)
    if cm:
        rows.append({"name": os.environ["STUB_REL"], "namespace": ns, "status": cm[0],
                     "chart": "cert-manager-" + cm[1]})
f = os.environ["STUB_LIST_FILTER"]
if f:
    rows = [r for r in rows if re.search(f, r["name"])]
if os.environ["STUB_LIST_JSON"] == "yes":
    print(json.dumps(rows))
else:
    print("NAME\tNAMESPACE\tSTATUS\tCHART")
    for r in rows:
        print("\t".join((r["name"], r["namespace"], r["status"], r["chart"])))
'
STUB
chmod +x "$TMP/bin/kubectl" "$TMP/bin/kubectl-answer" "$TMP/bin/helm" "$TMP/bin/helm-answer"

run_mode() { # <режим> → код возврата цели; вывод и журнал вызовов — в $TMP
  local mode="$1"
  : >"$TMP/log.$mode"
  (cd "$DEPLOY_ROOT" && PATH="$TMP/bin:$PATH" STUB_MODE="$mode" STUB_LOG="$TMP/log.$mode" \
     STUB_REL="$REL" STUB_NS="$NS" STUB_CHART_VERSION="$CHART_VERSION" STUB_APPROVE="$APPROVE" \
     make --no-print-directory -f "$MAKEFILE" cert-manager-up \
       CERT_MANAGER_NAMESPACE="$NS" EXPECT_CONTEXT=kind-stub) >"$TMP/out.$mode" 2>&1
}

# Установка — любой `helm upgrade`/`helm install` этого релиза; в НАШЕ ли ns она
# легла — отдельный вопрос, и он задаётся отдельно (installed_here).
installed()      { grep -Eq "^helm (upgrade|install) (.* )?$REL( |\$)" "$TMP/log.$1"; }
installed_here() { grep -Eq "^helm (upgrade|install) (.* )?$REL .*(-n|--namespace)[ =]$NS( |\$)" "$TMP/log.$1"; }
waited()         { grep -Eq "^kubectl -n $NS rollout status deploy/$REL-webhook( |\$)" "$TMP/log.$1"; }

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
  if [ "$got_install" = yes ] && ! installed_here "$mode"; then bad="$bad установка не в ns $NS, переданное целью"; fi
  if [ "$want_wait" != any ] && [ "$got_wait" != "$want_wait" ]; then bad="$bad ожидание-вебхука=$got_wait (ждали $want_wait)"; fi
  if [ -z "$bad" ]; then
    good "$label — код $rc, установка $got_install, ожидание вебхука $got_wait"
  else
    violation "$label —$bad. Вывод цели и журнал вызовов:"
    sed 's/^/      | /' "$TMP/out.$mode"
    sed 's/^/      > /' "$TMP/log.$mode"
  fi
}

says() { # <метка> <режим> <фраза> <что не сказано>
  ok
  if grep -q "$3" "$TMP/out.$2" 2>/dev/null; then
    good "$1"
  else
    violation "$1 — вывод не содержит «$3»: $4"
    sed 's/^/      | /' "$TMP/out.$2" 2>/dev/null | tail -5
  fi
}

echo "=== $SCRIPT: часть Б — цель cert-manager-up против подставных kubectl/helm (версия чарта $CHART_VERSION, ns $NS) ==="
behaviour "Б1: CRD нет"                                   absent         0  yes yes
behaviour "Б2: наш релиз той же версии"                   ours-same      0  no  yes
behaviour "Б3: наш релиз другой версии"                   ours-drift     0  yes yes
behaviour "Б4: CRD принадлежит чужому релизу"             foreign        0  no  no
behaviour "Б5: наличие не прочитано (API не ответил)"     unreachable    nz no  no
behaviour "Б6: CRD есть, записи владельца helm нет"       foreign-nohelm 0  no  no
behaviour "Б7: ответ API о CRD не разобран как JSON"      garbled        nz no  no
behaviour "Б8: наш релиз той же версии, не deployed"      ours-failed    0  yes yes
behaviour "Б9: наш релиз той же версии, одобритель шире объявленного" ours-wide 0 yes yes

says "Б5: отказ называет причину — наличие cert-manager не прочитано" unreachable 'НЕ ПРОЧИТАНО' \
  "«не ответил» неотличимо от любого другого отказа"
says "Б4: вывод называет владельца чужого cert-manager" foreign 'beget-cert-manager' \
  "оператор не отличит «чужой» от «никакого»"
says "Б6: вывод говорит, что cert-manager поставлен не helm" foreign-nohelm 'поставлен не helm' \
  "оператор не отличит «стоит чужой» от «никакого»"
says "Б7: отказ называет причину — ответ не прочитан как JSON" garbled 'НЕ ПРОЧИТАНО как JSON' \
  "неразобранный ответ неотличим от недоступного API"
says "Б9: вывод называет дрейф права одобрения" ours-wide 'встроенный одобритель вправе одобрять' \
  "перестановку релиза той же версии не объяснить ничем"

echo
echo "проверок исполнено: $N из $EXPECTED_ASSERTIONS"
findings_verdict "режимов подставного кластера: 9 (absent, ours-same, ours-drift, foreign, unreachable, foreign-nohelm, garbled, ours-failed, ours-wide)"
