#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# internal-ca-root-copy-test.sh — корень внутреннего CA, рождённый ВНЕ namespace
# релиза, не имеет в namespace релиза КОПИИ, на которую кто-то опирается; а если
# опора на копию всё же появится (слоем, которого дерево не видит), предполёт
# стенда требует копию и сверяет её с ТЕКУЩИМ корнем (kacho#3054).
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО СЛУЧИЛОСЬ
#
# Профиль с `mtls.internalCA.caNamespace` ≠ namespace релиза рождает корень в
# пространстве ресурсов cert-manager. Край же опирался хопом за набором ключей на
# секрет того же имени В СВОЁМ namespace — копию, которую чарт не производит и не
# синхронизирует: её клал ручной шаг. После переустановки корень стал новым,
# копия осталась прежней, поды поднялись (секрет существует), а проверка листа
# службы выдачи токенов отказывала — набор ключей краю не открывался. Предполёт
# `stack-secrets.sh` копию не требовал: множество «производится применением» он
# считал по ИМЕНИ, без namespace, и Certificate корня в чужом namespace
# засчитывал копию произведённой.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ КОПИЮ НЕ ПРОИЗВОДИТ ЧАРТ
#
# Производить её значит читать секрет корня, а в нём ЗАКРЫТЫЙ КЛЮЧ: право чтения
# секрета нельзя сузить до одного его ключа. Учётка с таким правом выпускает лист
# на любое имя внутренней сети. Копия не нужна вовсе: у каждого хопа к соседу
# есть якорь, который cert-manager кладёт рядом с листом соседа (`ca.crt` его
# секрета) и обновляет вместе с ним, — он всегда от того корня, что выпустил
# предъявляемый лист. Так устроены боевые цепочки, и так теперь устроена a8f60d.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО УТВЕРЖДАЕТСЯ
#
#   1. дерево: в рендере каждой цепочки deploy/stacks.txt, где корень
#      (Certificate с isCA) рождён вне namespace релиза, НИ ОДИН под namespace
#      релиза не ссылается на секрет корня (том, проекция, env, envFrom).
#      Законный близнец — цепочка с корнем в namespace релиза: там ссылка
#      законна и считается, а не судится;
#   2. страж чарта: якорь края на копию при caNamespace ≠ namespace релиза —
#      ОТКАЗ РЕНДЕРА с текстом свойства; та же ручка при корне в namespace
#      релиза — рендер проходит (близнец);
#   3. предполёт stack-secrets.sh в подменённом мире (helm и kubectl — двойники,
#      рендер — потребитель копии): копии нет → код 1; копия от другого корня →
#      код 1; корня нет при существующей копии → код 1; от текущего → код 0;
#      чтение корня отказано сервером → код 2 («не установлено» ≠ «нет»);
#   4. способность упасть: копия скрипта с namespace-слепым множеством
#      «производится» и копия без сверки отпечатка — мир 3 обязан это заметить.
#
# Коды: 0 зелено · 1 находка о дереве · 2 условие не создано (outcome.sh).
set -uo pipefail
. "$(dirname "$0")/stacks.sh"

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$DEPLOY/helm/umbrella"
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
require_helm
require_python_yaml
command -v openssl >/dev/null 2>&1 || fatal "нет openssl — сертификатов мира не выпустить"

STACKS="$(stacks_table | cut -d: -f1)" || fatal "таблица стенда не прочитана"
STACKS_N="$(printf '%s\n' "$STACKS" | grep -c .)"
# 1: по утверждению на цепочку; 2: два отказа и два близнеца; 3: пять миров;
# 4: две инъекции.
EXPECTED_ASSERTIONS=$((STACKS_N + 4 + 5 + 2))

WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT

# root_consumers <файл рендера> <ns релиза> → «<ns корня> <секрет> <число> <кто…>»
# по строке на корень (Certificate с isCA).
root_consumers() {
  python3 - "$1" "$2" <<'PY'
import sys, yaml
docs = [d for d in yaml.safe_load_all(open(sys.argv[1])) if isinstance(d, dict)]
rel = sys.argv[2]
def ns_of(d):
    return ((d.get("metadata") or {}).get("namespace")) or rel
roots = []
for d in docs:
    s = d.get("spec") or {}
    if d.get("kind") == "Certificate" and s.get("isCA") and s.get("secretName"):
        roots.append((ns_of(d), s["secretName"]))
def refs(pod):
    out = set()
    for c in (pod.get("containers") or []) + (pod.get("initContainers") or []):
        for e in c.get("env") or []:
            r = (e.get("valueFrom") or {}).get("secretKeyRef")
            if r: out.add(r.get("name"))
        for ef in c.get("envFrom") or []:
            r = ef.get("secretRef")
            if r: out.add(r.get("name"))
    for v in pod.get("volumes") or []:
        s = v.get("secret")
        if s: out.add(s.get("secretName"))
        for src in ((v.get("projected") or {}).get("sources") or []):
            s = src.get("secret")
            if s: out.add(s.get("name"))
    return out
for rns, name in roots:
    who = []
    for d in docs:
        spec = d.get("spec") or {}
        pod = spec if d.get("kind") == "Pod" else ((spec.get("template") or ((spec.get("jobTemplate") or {}).get("spec") or {}).get("template") or {}).get("spec"))
        if not pod or ns_of(d) != rel: continue
        if name in refs(pod):
            who.append(d["kind"] + "/" + d["metadata"]["name"])
    print(rns, name, len(who), ",".join(who) or "-")
PY
}

# ── 1. ДЕРЕВО: у корня вне namespace релиза нет потребителей копии ──────────
echo "1. потребители секрета корня в namespace релиза, по цепочкам:"
roots_outside=0
for s in $STACKS; do
  args="$(stacks_args "$s" "$UMBRELLA")" || fatal "цепочка $s не прочитана"
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" -n kacho $args
  render_nonempty_or_fatal "цепочка $s"
  printf '%s\n' "$HELM_OUT" > "$WORK/r.yaml"
  lines="$(root_consumers "$WORK/r.yaml" kacho)" || fatal "рендер цепочки $s не разобран"
  [ -n "$lines" ] || { violation "$s: корня (Certificate с isCA) в рендере нет — судить нечего, обход слеп"; ok; continue; }
  while read -r rns name n who; do
    if [ "$rns" = kacho ]; then
      echo "   $s: корень $name в namespace релиза, ссылок $n ($who) — законный близнец"
    elif [ "$n" -gt 0 ]; then
      roots_outside=$((roots_outside + 1))
      violation "$s: корень $name рождён в другом namespace, а в namespace релиза на секрет того же имени опираются $n: $who — это копия, которую чарт не производит"
    else
      roots_outside=$((roots_outside + 1))
      echo "   $s: корень $name в namespace $rns, потребителей копии 0"
    fi
  done <<<"$lines"
  ok
done
[ "$roots_outside" -gt 0 ] || violation "ни одна цепочка не рождает корень вне namespace релиза — утверждение 1 вакуумно"

# ── 2. СТРАЖ ЧАРТА ───────────────────────────────────────────────────────────
GUARD_TEXT="копию корня внутреннего CA чарт не производит"
echo "2. страж чарта: якорь края на секрет корня"
outside="$(stacks_args a8f60d "$UMBRELLA")" || fatal "цепочка a8f60d не прочитана"
inside="$(stacks_args dev "$UMBRELLA")" || fatal "цепочка dev не прочитана"
for knob in issuerKeySetsCa revocationCa; do
  set_args=(--set "api-gateway.tokenAcceptance.$knob.secretName=kacho-internal-ca-root" --set "api-gateway.tokenAcceptance.$knob.key=tls.crt")
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" -n kacho $outside "${set_args[@]}"
  if [ "$HELM_RC" -ne 0 ] && [[ "$HELM_ERR" == *"$GUARD_TEXT"* ]]; then
    echo "   a8f60d + $knob → копия: ОТКАЗ РЕНДЕРА со свойством"
  elif [ "$HELM_RC" -ne 0 ]; then
    helm_said "a8f60d + $knob"; fatal "рендер отказал не стражем — отрицание проходило бы вакуумно"
  else
    violation "a8f60d + $knob → копия корня: рендер ПРОШЁЛ — опора на копию, которую чарт не производит, не отвергнута"
  fi
  ok
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" -n kacho $inside "${set_args[@]}"
  if [ "$HELM_RC" -eq 0 ]; then echo "   dev + $knob → корень в namespace релиза: рендер проходит (близнец)"
  else helm_said "dev + $knob"; violation "dev + $knob: при корне в namespace релиза тот же якорь законен, а рендер отказал"; fi
  ok
done

# ── 3. ПРЕДПОЛЁТ stack-secrets.sh В ПОДМЕНЁННОМ МИРЕ ────────────────────────
BIN="$WORK/bin"; mkdir -p "$BIN"
SRC_NS=ca-system
FIXTURE="$WORK/fixture.yaml"
cat > "$FIXTURE" <<YAML
apiVersion: cert-manager.io/v1
kind: Certificate
metadata: {name: kacho-internal-ca-root, namespace: $SRC_NS}
spec: {isCA: true, secretName: kacho-internal-ca-root}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: probe-copy-consumer}
spec:
  template:
    spec:
      containers: [{name: probe, image: probe}]
      volumes:
        - name: anchor
          secret: {secretName: kacho-internal-ca-root, items: [{key: tls.crt, path: ca.crt}]}
YAML
cat > "$BIN/helm" <<'HELM'
#!/usr/bin/env bash
[ "${1:-}" = template ] && { cat "$FIXTURE"; exit 0; }
echo "двойник helm: неожиданный вызов: $*" >&2; exit 98
HELM
# Состояние: $STATE/<ns>/<имя>.crt — секрет есть, в его tls.crt этот сертификат.
cat > "$BIN/kubectl" <<'KUBECTL'
#!/usr/bin/env bash
set -uo pipefail
ns=""; args=()
while [ $# -gt 0 ]; do
  case "$1" in -n|--namespace) ns="$2"; shift 2 ;; *) args+=("$1"); shift ;; esac
done
set -- "${args[@]}"
case "$1 ${2:-}" in
  "config current-context") echo "probe-managed"; exit 0 ;;
  "get namespace") exit 0 ;;
  "get secret")
    if [ "$ns" = "${REFUSE_NS:-}" ]; then
      echo "Error from server (Forbidden): secrets \"$3\" is forbidden: User \"probe\" cannot get resource \"secrets\" in the namespace \"$ns\"" >&2; exit 1
    fi
    f="$STATE/$ns/$3.crt"
    [ -f "$f" ] || { echo "Error from server (NotFound): secrets \"$3\" not found" >&2; exit 1; }
    case "$*" in
      *"-o name"*) echo "secret/$3" ;;
      *"jsonpath={.data.tls\.crt}"*) base64 -w0 < "$f" ;;
      *) echo "двойник kubectl: чтение секрета не одним ключом сертификата: $*" >&2; exit 97 ;;
    esac
    exit 0 ;;
esac
echo "двойник kubectl: неожиданный вызов: $*" >&2; exit 98
KUBECTL
chmod +x "$BIN/helm" "$BIN/kubectl"
mkcert() { openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -keyout /dev/null \
  -subj "/CN=$1" -days 1 -out "$2" 2>/dev/null || fatal "openssl не выпустил сертификат мира"; }
mkcert probe-root-current "$WORK/current.crt"
mkcert probe-root-previous "$WORK/previous.crt"
export FIXTURE

# world <каталог deploy> <копия: none|current|previous> <корень: current|none> [REFUSE_NS]
world() {
  local root="$1" copy="$2" src="$3"
  STATE="$(mktemp -d "$WORK/state.XXXXXX")"; mkdir -p "$STATE/kacho" "$STATE/$SRC_NS"
  [ "$copy" = none ] || cp "$WORK/$copy.crt" "$STATE/kacho/kacho-internal-ca-root.crt"
  [ "$src" = none ] || cp "$WORK/$src.crt" "$STATE/$SRC_NS/kacho-internal-ca-root.crt"
  OUT="$(env PATH="$BIN:$PATH" STATE="$STATE" REFUSE_NS="${4:-}" STACK_NAMESPACE=kacho STACK_RELEASE=kacho-umbrella \
    bash "$root/scripts/stack-secrets.sh" a8f60d 2>&1)"; RC=$?
}
expect() {  # <что> <код> <подстрока>
  if [ "$RC" -eq "$2" ] && [[ "$OUT" == *"$3"* ]]; then echo "   $1 → код $RC"
  else violation "$1: ждали код $2 и «$3», получили код $RC: $(printf '%s' "$OUT" | tail -n 4 | tr '\n' ' ')"; fi
  ok
}
echo "3. stack-secrets.sh, корень в $SRC_NS, потребитель копии в namespace релиза:"
world "$DEPLOY" none current;         expect "копии нет"                 1 "kacho-internal-ca-root"
world "$DEPLOY" previous current;     expect "копия от другого корня"    1 "не от текущего корня"
world "$DEPLOY" previous none;        expect "корня нет, копия есть"     1 "не от текущего корня"
world "$DEPLOY" current current;      expect "копия от текущего корня"   0 "отсутствует 0"
world "$DEPLOY" current current "$SRC_NS"; expect "чтение корня отказано" 2 "НЕ УСТАНОВЛЕНО"

# ── 4. СПОСОБНОСТЬ УПАСТЬ: две инъекции в копию скрипта ─────────────────────
inject() {  # <имя> <sed-выражение> — копия deploy/ со сломанным скриптом
  local d="$WORK/inj-$1"
  mkdir -p "$d/scripts" "$d/tests"
  cp "$DEPLOY/scripts/"*.sh "$d/scripts/"; cp -r "$DEPLOY/tests/helm" "$d/tests/"; cp "$DEPLOY/stacks.txt" "$d/"
  ln -s "$DEPLOY/helm" "$d/helm"
  sed -i "$2" "$d/scripts/stack-secrets.sh"
  if cmp -s "$d/scripts/stack-secrets.sh" "$DEPLOY/scripts/stack-secrets.sh"; then
    return 1
  fi
  printf '%s' "$d"
}
echo "4. инъекции:"
if ! d="$(inject ns-blind 's/made\.add((ns_of(d), \(.*\)))/made.add((rel, \1))/')"; then
  violation "инъекция ns-blind не нашла строки множества «производится» по (namespace, имя) — образец разошёлся с кодом"
elif world "$d" none current; [ "$RC" -ne 1 ]; then echo "   namespace-слепое «производится» → мир «копии нет» дал код $RC — проба это видит"
else violation "namespace-слепое «производится» не изменило исход мира «копии нет» (код $RC) — проба упасть не может"; fi
ok
if ! d="$(inject no-fingerprint 's/^\([[:space:]]*\)\[ "\$have" = "\$want" \]/\1true/')"; then
  violation "инъекция no-fingerprint не нашла сверки отпечатка — образец разошёлся с кодом"
elif world "$d" previous current; [ "$RC" -eq 0 ]; then echo "   сверка отпечатка снята → мир «другой корень» дал код 0 — проба это видит"
else violation "снятая сверка отпечатка не изменила исход мира «другой корень» (код $RC) — проба упасть не может"; fi
ok

outcome_verdict "цепочек $STACKS_N, корней вне namespace релиза $roots_outside"
