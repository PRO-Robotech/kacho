#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ПОРТ СОСЕДА-ТЕРМИНАТОРА ПРОТИВ ДЕЙСТВУЮЩИХ ПОЛИТИК + ОБЪЯВЛЕНИЯ БОЕВОГО КЛАССА.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ЭТО ОТДЕЛЬНЫЙ ГЕЙТ, А НЕ ПУНКТ В ГРАНИЦАХ РАБОТЫ
#
# «Новых сетевых политик не вводим» НЕ означает «существующие можно не трогать и
# не проверять». Политика, объявленная на боевом профиле, не вправе запрещать
# порт, который переход реально использует.
#
# «ОБЪЯВЛЕННАЯ», А НЕ «ДЕЙСТВУЮЩАЯ» — разница измерена и печатается. Селектор
# политики может не совпасть ни с одним подом; тогда она не ограничивает НИЧЕГО,
# а в рендере выглядит ровно так же, как работающая. Именно это здесь и
# обнаружилось: единственная политика, называющая под провайдера, выбирает поды
# по метке сайдкара, которую не эмитит ни один профиль. Счёт выбранных подов
# уходит в открытый долг (см. конец файла), а не в зелёное.
#
# Ловушка, ради которой гейт написан: политики рендерит НЕ КАЖДЫЙ профиль. Стенд,
# на котором гоняются живые сценарии, политик не рендерит вовсе — поэтому выбор
# порта, проверенный только на нём, может сломать боевой профиль ПРИ ЗЕЛЁНОМ
# ГЕЙТЕ. Здесь сравниваются ДВА РЕНДЕРА (порт из описания пода против портов из
# описания политик), а не рендер с константой в тексте гейта: константа
# разошлась бы с рендером молча.
#
# ─────────────────────────────────────────────────────────────────────────────
# ВТОРОЙ ПРЕДМЕТ: ПРИЁМ ПЛЕЙНТЕКСТА ОТ ПЕТЛИ НА БОЕВОМ КЛАССЕ
#
# После увода листенера провайдера на петлю источником плейнтекста для него
# становится СОСЕД ПО ПЕТЛЕ, то есть `127.0.0.1`. Умолчание чарта перечисляет
# только частные диапазоны — петли в нём нет. Профиль боевого класса, включивший
# терминатор и не объявивший петлю, обязан краснеть ДО развёртывания.
#
# «Боевой класс» определяется ПО ОБЪЯВЛЕНИЮ ЛИБО ПО УМОЛЧАНИЮ ЧАРТА, а не
# списком имён: список стареет молча, и устаревшая запись освобождает ровно тот
# стенд, которому проверка только что понадобилась. Умолчание читается из
# МАТЕРИАЛИЗОВАННОГО чарта, а не вписано сюда числом.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"

FAILURES=0
ASSERTIONS=0
fail() { echo "  ✗ $1"; FAILURES=$((FAILURES + 1)); }
ok()   { echo "  ✓ $1"; }
note() { echo "    $1"; }
assertion() { ASSERTIONS=$((ASSERTIONS + 1)); }

SIDECAR_NAME="${SIDECAR_NAME:-admin-tls}"

# ═════════════════════════════════════════════════════════════════════════════
# ПРЕДИКАТ — ЧИСТАЯ ФУНКЦИЯ НАД ТЕКСТОМ РЕНДЕРА.
#
# Печатает строки вида `KEY=value`, чтобы самопроверка могла скормить ей
# синтетический рендер БЕЗ helm и сверить исход.
#
#   SIDECAR_PORT=<порт соседа внутри пода>       (пусто, если соседа нет)
#   POLICY=<имя>:<порт>,<порт>                    (по одной на политику,
#                                                  регулирующую переход)
#   POLICIES=<число политик, регулирующих переход>
# ═════════════════════════════════════════════════════════════════════════════
analyze_render() {
  python3 - "$1" "$SIDECAR_NAME" <<'PY'
import sys, yaml

path, sidecar = sys.argv[1], sys.argv[2]
with open(path) as fh:
    docs = [d for d in yaml.safe_load_all(fh) if isinstance(d, dict)]

# Метки пода провайдера — их же адресуют podSelector'ы политик.
hydra_labels = {"app.kubernetes.io/name": "hydra"}

sidecar_port = ""
for d in docs:
    if d.get("kind") not in ("Deployment", "StatefulSet"):
        continue
    spec = (d.get("spec") or {}).get("template", {}).get("spec", {})
    labels = (d.get("spec") or {}).get("template", {}).get("metadata", {}).get("labels", {}) or {}
    if labels.get("app.kubernetes.io/name") != "hydra":
        continue
    for c in spec.get("containers") or []:
        if c.get("name") == sidecar:
            for p in c.get("ports") or []:
                if p.get("containerPort"):
                    sidecar_port = str(p["containerPort"])
print("SIDECAR_PORT=%s" % sidecar_port)

def selector_hits_hydra(sel):
    """podSelector, адресующий под провайдера."""
    if not isinstance(sel, dict):
        return False
    ml = sel.get("matchLabels") or {}
    return any(ml.get(k) == v for k, v in hydra_labels.items())

def selector_matches(sel, labels):
    """podSelector против меток шаблона пода. Пустой селектор берёт всё."""
    if sel is None:
        return False
    ml = sel.get("matchLabels") or {}
    if any(labels.get(k) != v for k, v in ml.items()):
        return False
    for e in sel.get("matchExpressions") or []:
        key, op, vals = e.get("key"), e.get("operator"), e.get("values") or []
        cur = labels.get(key)
        if op == "In" and cur not in vals:
            return False
        if op == "NotIn" and cur in vals:
            return False
        if op == "Exists" and key not in labels:
            return False
        if op == "DoesNotExist" and key in labels:
            return False
    return True

pod_labels = []
for d in docs:
    if d.get("kind") not in ("Deployment", "StatefulSet", "DaemonSet"):
        continue
    pod_labels.append(
        ((d.get("spec") or {}).get("template") or {}).get("metadata", {}).get("labels", {}) or {})

governing = 0
for d in docs:
    if d.get("kind") != "NetworkPolicy":
        continue
    name = (d.get("metadata") or {}).get("name", "?")
    spec = d.get("spec") or {}
    ports = set()
    hit = False
    for direction, peerkey in (("egress", "to"), ("ingress", "from")):
        for rule in spec.get(direction) or []:
            peers = rule.get(peerkey) or []
            # Правило БЕЗ пиров разрешает всех — оно переход не РЕГУЛИРУЕТ,
            # а пропускает; считать его разрешением конкретного порта нельзя.
            if not peers:
                continue
            if not any(selector_hits_hydra(p.get("podSelector")) for p in peers):
                continue
            hit = True
            for p in rule.get("ports") or []:
                if p.get("port") is not None:
                    ports.add(str(p["port"]))
    if hit:
        governing += 1
        print("POLICY=%s:%s" % (name, ",".join(sorted(ports))))
        # СКОЛЬКО ПОДОВ ПОЛИТИКА РЕАЛЬНО ВЫБИРАЕТ. Отрендеренная политика и
        # ДЕЙСТВУЮЩАЯ политика — разные вещи: селектор, не совпавший ни с одним
        # подом, не ограничивает НИЧЕГО, а в рендере выглядит ровно так же.
        subjects = sum(1 for lb in pod_labels if selector_matches(spec.get("podSelector"), lb))
        print("POLICY_SUBJECTS=%s:%d" % (name, subjects))
print("POLICIES=%d" % governing)
PY
}

# ═════════════════════════════════════════════════════════════════════════════
# САМОПРОВЕРКА — БЕЗ helm И БЕЗ КЛАСТЕРА.
# ═════════════════════════════════════════════════════════════════════════════
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: предикат порта против политик, инъекции в обе стороны ==="
  rc=0; checked=0
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

  mk_render() { # <порт соседа> <порты политики…>
    local sp="$1"; shift
    local pl=""
    for p in "$@"; do pl="$pl
        - protocol: TCP
          port: $p"; done
    cat <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rel-hydra
spec:
  template:
    metadata:
      labels:
        app.kubernetes.io/name: hydra
    spec:
      containers:
        - name: hydra
          ports:
            - { name: http-admin, containerPort: 4455 }
        - name: admin-tls
          ports:
            - { name: https-admin, containerPort: $sp }
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: consumer-egress
spec:
  podSelector:
    matchLabels: { app.kubernetes.io/name: iam }
  egress:
    - to:
        - podSelector:
            matchLabels:
              app.kubernetes.io/name: hydra
      ports:$pl
EOF
  }

  probe() { # <метка> <ожидаемая строка KEY=value> <файл>
    local label="$1" want="$2" file="$3" got
    checked=$((checked + 1))
    got="$(analyze_render "$file")"
    if grep -qxF "$want" <<<"$got"; then echo "  ✓ $label"
    else echo "  ✗ $label — ожидалось «$want», получено:"; sed 's/^/      /' <<<"$got"; rc=1; fi
  }

  # ЗАКОННАЯ конструкция: сосед на 4445, политика разрешает 4444/4445.
  mk_render 4445 4444 4445 >"$tmp/legit.yaml"
  probe "порт соседа прочитан из описания пода" "SIDECAR_PORT=4445" "$tmp/legit.yaml"
  probe "политика, регулирующая переход, найдена" "POLICY=consumer-egress:4444,4445" "$tmp/legit.yaml"
  checked=$((checked + 1))
  if grep -qxF 'POLICIES=1' <<<"$(analyze_render "$tmp/legit.yaml")"; then
    ok "число регулирующих политик утверждается (1)"
  else echo "  ✗ число регулирующих политик не утверждается"; rc=1; fi

  # ИНЪЕКЦИЯ: сосед сел на порт, которого политика не разрешает.
  mk_render 8445 4444 4445 >"$tmp/bad.yaml"
  checked=$((checked + 1))
  out="$(analyze_render "$tmp/bad.yaml")"
  sp="$(sed -n 's/^SIDECAR_PORT=//p' <<<"$out")"
  pp="$(sed -n 's/^POLICY=[^:]*://p' <<<"$out")"
  if [ "$sp" = 8445 ] && ! grep -q "\b8445\b" <<<"$pp"; then
    ok "инъекция: порт соседа ($sp) вне разрешённых политикой ($pp) — различимо"
  else
    echo "  ✗ инъекция не различена: sp=$sp, разрешено=$pp"; rc=1
  fi

  # ОТРЕНДЕРЕННАЯ ПОЛИТИКА ≠ ДЕЙСТВУЮЩАЯ. Селектор политики может не совпасть ни
  # с одним подом — тогда она не ограничивает НИЧЕГО, а в рендере выглядит ровно
  # так же. Это не гипотеза: единственная политика, называющая под провайдера,
  # выбирает подов по метке, которую не эмитит ни один профиль.
  probe "политика без единого выбранного пода — счёт 0, а не «есть политика»" \
    "POLICY_SUBJECTS=consumer-egress:0" "$tmp/legit.yaml"

  mk_render 4445 4444 4445 >"$tmp/withsubj.yaml"
  cat >>"$tmp/withsubj.yaml" <<'EOF'
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rel-iam
spec:
  template:
    metadata:
      labels:
        app.kubernetes.io/name: iam
    spec:
      containers:
        - name: iam
EOF
  probe "политика с выбранным подом — счёт 1" \
    "POLICY_SUBJECTS=consumer-egress:1" "$tmp/withsubj.yaml"

  # ИНЪЕКЦИЯ: политика, НЕ адресующая под провайдера, переход не регулирует и
  # не должна учитываться (иначе чужое правило «разрешало» бы наш порт).
  cat >"$tmp/other.yaml" <<'EOF'
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: unrelated
spec:
  podSelector:
    matchLabels: { app.kubernetes.io/name: iam }
  egress:
    - to:
        - podSelector:
            matchLabels:
              app.kubernetes.io/name: postgres
      ports:
        - { protocol: TCP, port: 8445 }
EOF
  probe "чужая политика переход НЕ регулирует" "POLICIES=0" "$tmp/other.yaml"

  # ИНЪЕКЦИЯ: соседа нет вовсе — «порт не найден» обязано быть отличимо от
  # «порт разрешён».
  cat >"$tmp/nosidecar.yaml" <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rel-hydra
spec:
  template:
    metadata:
      labels: { app.kubernetes.io/name: hydra }
    spec:
      containers:
        - name: hydra
          ports:
            - { name: http-admin, containerPort: 4445 }
EOF
  probe "соседа нет — порт пуст, а не «совпал»" "SIDECAR_PORT=" "$tmp/nosidecar.yaml"

  # ── объявления боевого класса ──
  echo
  echo "-- боевой класс и петля в списке источников терминации --"
  prodclass() { # <файл значений…> → печатает true|false
    python3 -c '
import sys, yaml
merged = {}
def merge(d, s):
    for k, v in (s or {}).items():
        if isinstance(v, dict) and isinstance(d.get(k), dict): merge(d[k], v)
        else: d[k] = v
for f in sys.argv[1:]:
    merge(merged, yaml.safe_load(open(f)) or {})
dev = merged.get("hydra", {}).get("hydra", {}).get("dev", None)
print("declared" if dev is not None else "default", str(bool(dev)).lower() if dev is not None else "")
' "$@"
  }
  printf 'hydra:\n  hydra:\n    dev: true\n' >"$tmp/v-dev.yaml"
  printf 'hydra:\n  hydra:\n    dev: false\n' >"$tmp/v-prod.yaml"
  printf 'other: 1\n' >"$tmp/v-silent.yaml"
  checked=$((checked + 1))
  a="$(prodclass "$tmp/v-dev.yaml")"; b="$(prodclass "$tmp/v-prod.yaml")"; c="$(prodclass "$tmp/v-silent.yaml")"
  if [ "$a" = "declared true" ] && [ "$b" = "declared false" ] && [ "$c" = "default " ]; then
    ok "класс профиля читается по объявлению И по его отсутствию (умолчание чарта)"
  else
    echo "  ✗ класс профиля читается неверно: '$a' / '$b' / '$c'"; rc=1
  fi

  echo
  echo "инъекций и законных входов проверено: $checked"
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $rc
fi

# ═════════════════════════════════════════════════════════════════════════════
# ОСНОВНОЙ ПРОХОД
# ═════════════════════════════════════════════════════════════════════════════
command -v helm >/dev/null 2>&1 || { echo "FATAL: нужен helm"; exit 2; }
python3 -c 'import yaml' 2>/dev/null || { echo "FATAL: нужен python3 с PyYAML"; exit 2; }

CHART_TGZ="$(ls "$UMBRELLA"/charts/hydra-*.tgz 2>/dev/null | head -1)"
if [ -z "$CHART_TGZ" ] && [ ! -d "$UMBRELLA/charts/hydra" ]; then
  echo "FATAL: чарт провайдера не материализован — умолчание отладочного флага и"
  echo "       список источников терминации читать НЕ ОТКУДА. Собрать:"
  echo "       (cd $UMBRELLA && helm dep update)"
  exit 2
fi

# УМОЛЧАНИЕ ЧАРТА ЧИТАЕТСЯ ИЗ ЧАРТА, а не вписано сюда числом.
if [ -n "$CHART_TGZ" ]; then
  CHART_VALUES="$(tar xzOf "$CHART_TGZ" hydra/values.yaml 2>/dev/null)"
else
  CHART_VALUES="$(cat "$UMBRELLA/charts/hydra/values.yaml")"
fi
DEV_DEFAULT="$(printf '%s' "$CHART_VALUES" | python3 -c 'import yaml,sys; print(str(yaml.safe_load(sys.stdin)["hydra"]["dev"]).lower())')"
DEFAULT_TERMINATION="$(printf '%s' "$CHART_VALUES" | python3 -c '
import yaml,sys
v = yaml.safe_load(sys.stdin)
print(",".join(v["hydra"]["config"]["serve"]["tls"]["allow_termination_from"]))')"

STACKS='dev:values.dev.yaml
dev-prod:values.dev.yaml,values.dev-prod.yaml
prod:values.prod.yaml
fe3455:values.prod.yaml,values.fe3455.yaml,values.fe3455-prod.yaml
prorobotech:values.dev.yaml,values.prorobotech.yaml'

echo "=== $SCRIPT: порт соседа против политик + объявления боевого класса ==="
echo "  умолчание чарта: отладочный флаг = $DEV_DEFAULT; источники терминации = $DEFAULT_TERMINATION"
echo

work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
stacks_total=0; stacks_with_policies=0; prodclass_total=0; prodclass_live=0; inert_policies=0
# Стенд поднимает ровно один стек — на нём и только на нём приём плейнтекста от
# петли подтверждается ЖИВЫМ наблюдением.
LIVE_STACK="${LIVE_STACK:-dev-prod}"

while IFS= read -r line; do
  [ -z "$line" ] && continue
  stack="${line%%:*}"; files="${line#*:}"
  stacks_total=$((stacks_total + 1))
  args=""; vfiles=""
  IFS=','; for f in $files; do args="$args -f $UMBRELLA/$f"; vfiles="$vfiles $UMBRELLA/$f"; done; unset IFS

  render="$work/$stack.yaml"
  if ! (cd "$UMBRELLA" && helm template kacho-umbrella . $args --namespace kacho) >"$render" 2>"$work/$stack.err"; then
    assertion; fail "[$stack] рендер не собрался — утверждения НЕ ВЫПОЛНЕНЫ, а не чисты"
    sed 's/^/        /' "$work/$stack.err" | tail -4
    continue
  fi

  info="$(analyze_render "$render")"
  sidecar_port="$(sed -n 's/^SIDECAR_PORT=//p' <<<"$info")"
  npolicies="$(sed -n 's/^POLICIES=//p' <<<"$info")"

  # ── класс профиля ──
  # shellcheck disable=SC2086
  devflag="$(python3 -c '
import sys, yaml
merged = {}
def merge(d, s):
    for k, v in (s or {}).items():
        if isinstance(v, dict) and isinstance(d.get(k), dict): merge(d[k], v)
        else: d[k] = v
for f in sys.argv[1:]:
    merge(merged, yaml.safe_load(open(f)) or {})
dev = merged.get("hydra", {}).get("hydra", {}).get("dev", None)
print("unset" if dev is None else str(bool(dev)).lower())
' $vfiles)"
  effective_dev="$devflag"
  [ "$devflag" = unset ] && effective_dev="$DEV_DEFAULT"
  klass=dev; [ "$effective_dev" = false ] && klass=production

  echo "  [$stack] класс: $klass (флаг: $devflag${devflag:+, действует $effective_dev}) · сосед на порту: ${sidecar_port:-НЕТ} · регулирующих политик: $npolicies"

  # ── ОТРЕНДЕРЕННАЯ ПОЛИТИКА ≠ ДЕЙСТВУЮЩАЯ ─────────────────────────────────
  # Политика, чей селектор не совпал НИ С ОДНИМ подом, не ограничивает ничего, а
  # в рендере выглядит ровно так же. Это НЕ гипотеза: единственная политика,
  # называющая под провайдера, выбирает подов по метке, которую сегодня не
  # эмитит ни один профиль. Поэтому счёт печатается ЯВНО и уходит в открытый
  # долг, а не в зелёное: обоснование, опирающееся на такую политику, сейчас
  # силы не имеет.
  while IFS= read -r ps; do
    [ -z "$ps" ] && continue
    pname="${ps%%:*}"; pcount="${ps##*:}"
    if [ "$pcount" -gt 0 ]; then
      note "политика $pname выбирает подов: $pcount — действует"
    else
      note "политика $pname выбирает подов: 0 — ОТРЕНДЕРЕНА, НО НЕ ДЕЙСТВУЕТ"
      inert_policies=$((inert_policies + 1))
    fi
  done <<<"$(sed -n 's/^POLICY_SUBJECTS=//p' <<<"$info")"

  # ── SEC-HAT-21: порт соседа разрешён КАЖДОЙ регулирующей политикой ──
  if [ "$npolicies" -gt 0 ]; then
    stacks_with_policies=$((stacks_with_policies + 1))
    assertion
    if [ -z "$sidecar_port" ]; then
      fail "[$stack] соседа-терминатора в рендере НЕТ, а политики переход регулируют —"
      note "сверять нечего: это «не проверили», а не «порт разрешён»."
    else
      bad=""
      while IFS= read -r pol; do
        [ -z "$pol" ] && continue
        pname="${pol%%:*}"; pports="${pol#*:}"
        case ",$pports," in
          *",$sidecar_port,"*) ;;
          *) bad="$bad      политика $pname разрешает [$pports], сосед слушает $sidecar_port"$'\n' ;;
        esac
      done <<<"$(sed -n 's/^POLICY=//p' <<<"$info")"
      if [ -z "$bad" ]; then
        ok "[$stack] порт соседа $sidecar_port разрешён каждой из $npolicies политик, регулирующих переход"
      else
        fail "[$stack] политика запрещает порт, который переход РЕАЛЬНО использует:"
        printf '%s' "$bad"
        note "профиль, на котором это ломается, живыми сценариями не гоняется —"
        note "гейт на стенде без политик этого не увидел бы."
      fi
    fi
  fi

  # ── SEC-HAT-22: боевой класс — петля в источниках терминации + заголовок ──
  if [ "$klass" = production ]; then
    prodclass_total=$((prodclass_total + 1))
    [ "$stack" = "$LIVE_STACK" ] && prodclass_live=$((prodclass_live + 1))

    # Утверждение имеет предмет ТОЛЬКО когда терминатор в профиле включён:
    # без соседа плейнтекст на листенер приходит не от петли.
    if [ -n "$sidecar_port" ]; then
      assertion
      # shellcheck disable=SC2086
      term="$(python3 -c '
import sys, yaml
merged = {}
def merge(d, s):
    for k, v in (s or {}).items():
        if isinstance(v, dict) and isinstance(d.get(k), dict): merge(d[k], v)
        else: d[k] = v
for f in sys.argv[1:]:
    merge(merged, yaml.safe_load(open(f)) or {})
try:
    lst = merged["hydra"]["hydra"]["config"]["serve"]["tls"]["allow_termination_from"]
except Exception:
    lst = None
print("" if lst is None else ",".join(lst))
' $vfiles)"
      if [ -z "$term" ]; then
        fail "[$stack] боевой класс: список источников терминации НЕ ОБЪЯВЛЕН →"
        note "действует умолчание чарта [$DEFAULT_TERMINATION], петли в нём нет."
        note "После увода листенера на петлю источником станет 127.0.0.1 — вне списка."
        note "Ключ: hydra.hydra.config.serve.tls.allow_termination_from"
      elif ! grep -q '127\.0\.0\.1/32' <<<"$term"; then
        fail "[$stack] боевой класс: в источниках терминации нет петли ([$term])"
        note "Ключ: hydra.hydra.config.serve.tls.allow_termination_from — добавить 127.0.0.1/32"
      else
        ok "[$stack] боевой класс: петля объявлена в источниках терминации ([$term])"
        # СПИСОК ЗАМЕЩАЕТСЯ ЦЕЛИКОМ (helm сливает карты, но заменяет списки), и это
        # НАЗЫВАЕТСЯ, а не проверяется на совпадение с умолчанием.
        #
        # Первая редакция требовала здесь сохранить умолчания чарта дословно и
        # покраснела на профиле, который сознательно объявил СУЖЕННЫЙ список
        # (диапазоны подов, служб и узла вместо всего 10/8). Проверка была НЕ ПРАВА:
        # она требовала бы РАСШИРИТЬ круг источников, которым доверяют, — ровно
        # наоборот тому, чего хочет безопасность. Требование здесь одно: петля
        # присутствует. Насколько список у́же умолчания — решение профиля, и
        # сужение лучше.
        narrowed=""
        IFS=','; for cidr in $DEFAULT_TERMINATION; do
          grep -q "$(printf '%s' "$cidr" | sed 's/[.[\*^$/]/\\&/g')" <<<"$term" || narrowed="$narrowed $cidr"
        done; unset IFS
        [ -n "$narrowed" ] && note "профиль СУЖАЕТ умолчание чарта (не перечисляет:$narrowed) — это его решение"
      fi

      # Заголовок протокола: без него провайдер боевого класса отвергнет
      # плейнтекст даже от разрешённого источника.
      assertion
      if grep -qiE 'X-Forwarded-Proto[^\n]*https' "$render"; then
        ok "[$stack] конфигурация соседа объявляет X-Forwarded-Proto: https"
      else
        fail "[$stack] боевой класс: сосед НЕ объявляет X-Forwarded-Proto: https —"
        note "провайдер отвергнет плейнтекст даже от разрешённого источника."
      fi
    fi
  fi
done <<<"$STACKS"

echo
echo "── объём осмотренного ──"
echo "  стеков отрендерено: $stacks_total; из них рендерят политики: $stacks_with_policies"
echo "  профилей боевого класса: $prodclass_total"
echo
echo "── открытый долг (НЕ зачитывается в зелёное) ──"
echo "  политик, регулирующих переход, но не выбирающих НИ ОДНОГО пода: $inert_policies"
if [ "$inert_policies" -gt 0 ]; then
  echo "  (порядок портов остаётся верным ВПЕРЁД: когда такая политика получит"
  echo "   подданных, переход уже укладывается в её объём. Но сегодня она ничего"
  echo "   не ограничивает, и обоснование выбора порта ссылаться на неё как на"
  echo "   действующее ограничение НЕ ВПРАВЕ.)"
fi
debt=$((prodclass_total - prodclass_live))
echo "  профилей боевого класса, чей приём плейнтекста от петли ЖИВЫМ прогоном НЕ подтверждён: $debt"
echo "  (стенд поднимает «$LIVE_STACK»; остальные подтверждаются первой боевой посадкой)"
if [ "$prodclass_live" -eq 0 ]; then
  echo "  ВНИМАНИЕ: живого подтверждения нет НИ ОДНОГО — весь класс держится на объявлениях."
fi

echo
echo "=== вердикт: утверждений $ASSERTIONS, находок $FAILURES ==="
if [ "$ASSERTIONS" -eq 0 ]; then
  echo "FAIL: не выполнено НИ ОДНОГО утверждения — это провал, а не чистота."
  exit 1
fi
[ "$FAILURES" -ne 0 ] && { echo "FAIL: $SCRIPT"; exit 1; }
echo "PASS: $SCRIPT"
