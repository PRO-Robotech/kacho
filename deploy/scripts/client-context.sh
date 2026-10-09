#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# client-context.sh — ЕДИНСТВЕННЫЙ ВЫБОР КОНТЕКСТА -client ФАЙЛА ПРОФИЛЯ ПЛОЩАДКИ
# (kacho#3065).
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Файл профиля площадки несёт два контекста одного кластера — `…-client` и
# `…-infra`, — и его current-context переключает человек под свою работу.
# Ставить продукт можно ТОЛЬКО в client (решение владельца 2026-10-09), поэтому
# активный контекст файла выбором стенда НЕ является. Прежде так выбирал только
# стенд проб (scripts/stand-ns.sh), а `stack-up` по стенду управляемого кластера
# шёл в активный контекст: при current-context=infra выкатка уехала бы в infra.
# Выбор один на оба пути — этот файл; второй копии правила нет.
#
#   файл профиля  STAND_KUBECONFIG, иначе KUBECONFIG — ровно один файл;
#   контекст      ровно один контекст файла с суффиксом -client; ноль или больше
#                 одного — отказ ДО первого обращения к кластеру. Переопределение
#                 — только STAND_CONTEXT, и только именем, оканчивающимся на -client.
#
# current-context файла профиля не читается и не меняется.
#
# ─────────────────────────────────────────────────────────────────────────────
# РЕЖИМЫ
#
#   source client-context.sh     — функция client_context_pick: выбирает файл и
#                                  контекст (CC_FILE, CC_CTX) либо печатает отказ
#                                  и возвращает 2. Её зовёт scripts/stand-ns.sh.
#
#   client-context.sh exec -- КОМАНДА…
#       выбирает контекст и исполняет КОМАНДУ так, что каждый kubectl и helm в
#       ней и в её потомках идёт в client ЯВНО:
#         • PATH начинается каталогом обёрток kubectl/helm: обёртка дописывает
#           `--context <client>` (helm — `--kube-context`), а вызов, назвавший
#           другой контекст или переключающий/правящий контексты, отвергает;
#         • KUBECONFIG = файл из одной строки current-context=<client> (первый
#           файл, объявивший current-context, при слиянии побеждает), затем файл
#           профиля — шаги, читающие активный контекст (`kubectl config …`,
#           guard-destructive), видят client; HELM_KUBECONTEXT тоже;
#         • KACHO_CLIENT_CONTEXT, KACHO_CLIENT_CONTEXT_BIN и STAND_APISERVER
#           (адрес контекста -client из файла профиля) — для `verify`.
#       Рабочий каталог (файл выбора и обёртки) снимается на любом исходе.
#       Код — код КОМАНДЫ; 2 — отказ выбора, КОМАНДА не исполнялась.
#
#   client-context.sh verify [--ambient]
#       страж ВНУТРИ закреплённого вызова: 0, если контекст закреплён этим
#       файлом — KACHO_CLIENT_CONTEXT оканчивается на -client, активный
#       контекст равен ему, и (без --ambient) kubectl и helm на PATH — обёртки
#       закрепления. Иначе 2: цель зовут мимо `exec`, и она ушла бы в активный
#       контекст. `--ambient` — для пути, закрепляющего контекст файлом выбора
#       без обёрток (scripts/stand-ns.sh).
#
# Держат: deploy/stack_client_context_test.go, deploy/stand_ns_context_test.go.
set -uo pipefail

cc_fail() { printf 'ABORT: выбор контекста -client — %s\n' "$1" >&2; return 2; }

# cc_profile_contexts FILE — имена контекстов файла, по одному в строке. Разбор
# YAML, а не kubectl: выбор не зависит от активного контекста и не спрашивает
# кластер.
cc_profile_contexts() {
  python3 - "$1" <<'PY'
import sys, yaml
d = yaml.safe_load(open(sys.argv[1])) or {}
for c in d.get("contexts") or []:
    if isinstance(c, dict) and c.get("name"):
        print(c["name"])
PY
}

# client_context_pick — выбор файла и контекста; CC_FILE, CC_CTX. Отказ — 2.
client_context_pick() {
  local file="${STAND_KUBECONFIG:-}" names clients n
  CC_FILE="" CC_CTX=""
  if [ -z "$file" ]; then
    case "${KUBECONFIG:-}" in
      "") cc_fail "файл профиля не назван: продукт ставится в контекст -client ЯВНО названного файла.
       Что сделать: STAND_KUBECONFIG=<файл профиля площадки> make -C deploy <цель> …"; return 2 ;;
      *:*) cc_fail "KUBECONFIG — список файлов, а файл профиля должен быть один.
       Что сделать: STAND_KUBECONFIG=<файл профиля площадки> make -C deploy <цель> …"; return 2 ;;
    esac
    file="$KUBECONFIG"
  fi
  [ -f "$file" ] && [ -r "$file" ] || { cc_fail "файл профиля «$file» не читается"; return 2; }
  names="$(cc_profile_contexts "$file")" || { cc_fail "контексты файла профиля «$file» не разобраны"; return 2; }
  if [ -n "${STAND_CONTEXT:-}" ]; then
    [[ "$STAND_CONTEXT" == *-client ]] || { cc_fail "STAND_CONTEXT=«$STAND_CONTEXT» не оканчивается на -client — продукт ставится только в client.
       Что сделать: сними STAND_CONTEXT (выберется единственный -client файла) либо назови контекст -client"; return 2; }
    grep -Fxq -- "$STAND_CONTEXT" <<<"$names" || { cc_fail "в файле профиля «$file» нет контекста STAND_CONTEXT=«$STAND_CONTEXT»"; return 2; }
    CC_CTX="$STAND_CONTEXT"
  else
    clients="$(grep -E -- '-client$' <<<"$names")"
    n="$(grep -c . <<<"$clients")"
    [ "$n" = 1 ] || { cc_fail "контекстов с суффиксом -client в файле профиля «$file»: $n, нужен ровно один.
       Контексты файла: $(tr '\n' ' ' <<<"$names")
       Что сделать: возьми файл профиля площадки с её контекстом -client, либо при нескольких
       назови один: STAND_CONTEXT=<имя, оканчивающееся на -client>"; return 2; }
    CC_CTX="$clients"
  fi
  CC_FILE="$file"
}

# client_context_server — адрес apiserver'а выбранного контекста, из ФАЙЛА
# профиля (кластер не спрашивается); CC_SERVER. Объявлен STAND_APISERVER —
# обязан совпасть. Это авторитет, который переименованием контекста не
# подделать: `verify` сверяет с ним адрес активного для потомков контекста.
client_context_server() {
  CC_SERVER="$(python3 - "$CC_FILE" "$CC_CTX" <<'PY'
import sys, yaml
d = yaml.safe_load(open(sys.argv[1])) or {}
ctx = next((c.get("context") or {} for c in d.get("contexts") or [] if isinstance(c, dict) and c.get("name") == sys.argv[2]), {})
cl = next((c.get("cluster") or {} for c in d.get("clusters") or [] if isinstance(c, dict) and c.get("name") == ctx.get("cluster")), {})
print(cl.get("server") or "")
PY
)"
  [ -n "$CC_SERVER" ] || { cc_fail "контекст -client файла профиля не отдал адрес apiserver'а — пинить кластер нечем"; return 2; }
  [ -z "${STAND_APISERVER:-}" ] || [ "$STAND_APISERVER" = "$CC_SERVER" ] ||
    { cc_fail "объявленный STAND_APISERVER не адрес контекста -client файла профиля — выкатка ушла бы не туда"; return 2; }
}

# client_context_file PATH — файл выбора: одна строка current-context=<CC_CTX>.
client_context_file() {
  local quoted
  quoted="$(python3 -c 'import json, sys; print(json.dumps(sys.argv[1]))' "$CC_CTX")" && [ -n "$quoted" ] ||
    { cc_fail "имя контекста не записано в файл выбора"; return 2; }
  printf 'apiVersion: v1\nkind: Config\ncurrent-context: %s\n' "$quoted" >"$1" ||
    { cc_fail "файл выбора контекста не записан"; return 2; }
}

# Обёртки. Контекст и путь настоящего исполняемого файла вписываются при
# заведении: обёртка не читает окружение, которое потомок мог бы переписать.
cc_write_wrappers() {
  local bin="$1" real_kubectl real_helm
  real_kubectl="$(type -P kubectl)" || { cc_fail "нет kubectl — закреплять нечего"; return 2; }
  real_helm="$(type -P helm)" || { cc_fail "нет helm — закреплять нечего"; return 2; }
  mkdir -p "$bin" || { cc_fail "каталог обёрток не заведён"; return 2; }
  if ! { cc_wrapper kubectl "$real_kubectl" --context >"$bin/kubectl" &&
    cc_wrapper helm "$real_helm" --kube-context >"$bin/helm" &&
    chmod 0755 "$bin/kubectl" "$bin/helm"; }; then
    cc_fail "обёртки не записаны"; return 2
  fi
}

cc_wrapper() { # NAME REAL FLAG
  local q_ctx q_real
  q_ctx="$(printf '%q' "$CC_CTX")" q_real="$(printf '%q' "$2")"
  cat <<EOF
#!/usr/bin/env bash
# Обёртка закрепления контекста -client (deploy/scripts/client-context.sh).
ctx=$q_ctx
real=$q_real
flag=$3
prev=""
for a in "\$@"; do
  case "\$a" in
    --context=*|--kube-context=*) v="\${a#*=}"; [ "\$v" = "\$ctx" ] || { echo "ABORT: $1 назвал контекст «\$v», закреплён client" >&2; exit 2; } ;;
  esac
  case "\$prev" in
    --context|--kube-context) [ "\$a" = "\$ctx" ] || { echo "ABORT: $1 назвал контекст «\$a», закреплён client" >&2; exit 2; } ;;
  esac
  prev="\$a"
done
EOF
  if [ "$1" = kubectl ]; then
    cat <<'EOF'
if [ "${1:-}" = config ]; then
  case "${2:-}" in
    use-context|set-context|delete-context|rename-context|set|unset|set-cluster|set-credentials|delete-cluster|delete-user)
      echo "ABORT: kubectl config ${2} — контексты закреплённого вызова не правятся" >&2; exit 2 ;;
  esac
  exec "$real" "$@"
fi
EOF
  fi
  cat <<'EOF'
exec "$real" "$flag" "$ctx" "$@"
EOF
}

cc_exec() {
  [ "${1:-}" = "--" ] && shift
  [ "$#" -gt 0 ] || { cc_fail "exec без команды"; return 2; }
  client_context_pick || return 2
  client_context_server || return 2
  local work code
  work="$(mktemp -d "${TMPDIR:-/tmp}/kacho-client-context.XXXXXX")" || { cc_fail "рабочий каталог не заведён"; return 2; }
  # shellcheck disable=SC2064 # путь фиксируется при заведении
  trap "rm -rf '$work'" EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  client_context_file "$work/context.yaml" || return 2
  cc_write_wrappers "$work/bin" || return 2
  echo "=== кластер: контекст -client файла профиля (активный контекст файла не читается) ==="
  KACHO_CLIENT_CONTEXT="$CC_CTX" KACHO_CLIENT_CONTEXT_BIN="$work/bin" \
    KUBECONFIG="$work/context.yaml:$CC_FILE" HELM_KUBECONTEXT="$CC_CTX" PATH="$work/bin:$PATH" \
    STAND_KUBECONFIG="$CC_FILE" STAND_CONTEXT="$CC_CTX" STAND_APISERVER="$CC_SERVER" \
    "$@"
  code=$?
  return "$code"
}

cc_verify() {
  local ambient=0 ctx have
  [ "${1:-}" = "--ambient" ] && ambient=1
  ctx="${KACHO_CLIENT_CONTEXT:-}"
  [ -n "$ctx" ] || { cc_fail "цель стенда, который ставится только в client, позвана мимо закрепления контекста.
       Что сделать: зови её путём выкатки (make -C deploy stack-up STACK=… / stand-ns-…) с
       STAND_KUBECONFIG=<файл профиля площадки>"; return 2; }
  [[ "$ctx" == *-client ]] || { cc_fail "закреплённый контекст «$ctx» не оканчивается на -client"; return 2; }
  have="$(kubectl config current-context 2>/dev/null)"
  [ "$have" = "$ctx" ] || { cc_fail "активный контекст «${have:-<нет>}» не закреплённый client «$ctx» — цель ушла бы не туда"; return 2; }
  # Имя — первый отсев; идентичность — адрес apiserver'а, выведенный при
  # закреплении из файла профиля (STAND_APISERVER).
  have="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null)"
  [ -n "${STAND_APISERVER:-}" ] && [ "$have" = "$STAND_APISERVER" ] ||
    { cc_fail "адрес apiserver'а активного контекста не адрес закреплённого client — цель ушла бы не в тот кластер"; return 2; }
  if [ "$ambient" = 0 ]; then
    local bin="${KACHO_CLIENT_CONTEXT_BIN:-}" t
    [ -n "$bin" ] || { cc_fail "каталог обёрток закрепления не назван"; return 2; }
    for t in kubectl helm; do
      [ "$(type -P "$t")" = "$bin/$t" ] || { cc_fail "$t на PATH — не обёртка закрепления: вызов ушёл бы без явного контекста"; return 2; }
    done
  fi
  return 0
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  mode="${1:-}"; shift || true
  case "$mode" in
    exec) cc_exec "$@"; exit $? ;;
    verify) cc_verify "$@"; exit $? ;;
    pick) client_context_pick && printf '%s\n' "$CC_CTX"; exit $? ;;
    *) echo "использование: $0 exec -- КОМАНДА… | verify [--ambient] | pick" >&2; exit 2 ;;
  esac
fi
