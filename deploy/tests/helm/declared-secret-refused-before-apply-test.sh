#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# declared-secret-refused-before-apply-test.sh — ОБЪЯВЛЕННЫЙ ШАГ ОПЕРАТОРА
# (`existingSecret` в профиле стенда) ОБЯЗАН БЫТЬ ПРОВЕРЕН ДО ПРИМЕНЕНИЯ (#891).
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Профиль посадки объявляет `…existingSecret: <имя>` и пишет рядом «ШАГ
# ОПЕРАТОРА (до раската): создать Secret». Комментарий предусловием не является:
# если секрета нет, применение проходит, а под встаёт в
# `CreateContainerConfigError` — и не сразу, а когда его пересоздадут. У живого
# пода том остаётся смонтированным с прежним содержимым, поэтому отсутствие
# условия НЕВИДИМО, пока поды живы, и проявляется у ДРУГОГО компонента спустя
# произвольное время. Наблюдалось на внешнем стенде с учётными данными
# хранилища слоёв.
#
# Шаг, который отказывает до применения, в дереве ЕСТЬ — у `make stack-up` это
# scripts/stack-secrets.sh, у раскатки боевой площадки — предполёт её рецепта.
# Но то, что он действительно видит КАЖДОЕ объявление, не держалось ничем: шаг
# выводит требуемое из рендера, и объявление, потребляемое формой, которой
# вывод не знает, выпадало из-под отказа молча. Ровно так и было: обе копии
# вывода не читали том `projected`, одна из них — ещё и `envFrom`.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО УТВЕРЖДАЕТСЯ (по каждому стенду таблицы deploy/stacks.txt)
#
#   1. каждое имя, объявленное `existingSecret` в цепочке профилей стенда, либо
#      производит САМ рендер (`kind: Secret`), либо входит в перечень, который
#      предполёт требует ДО применения. Перечень берётся той же производной, что
#      зовёт рецепт (scripts/required-secrets.py), — второй копии здесь нет;
#   2. у стендов `make stack-up` каждое такое требуемое имя имеет строку в
#      ведомости производителей шага (`stack-secrets.sh --producer-of`): отказ
#      называет не только имя, но и того, кто его заводит;
#   3. рецепт, раскатывающий стенд, зовёт предполёт РАНЬШЕ `helm upgrade`, и
#      его отказ не заглушён: в одной команде — под `set -e` и без `|| …`;
#   4. стенды, которые `stack-up` отсылает к своему рецепту, объявлены здесь
#      (REDIRECTED) и совпадают с отказами самой цели в ОБЕ стороны.
#
# ЧЕГО НЕ УТВЕРЖДАЕТ: что секрет существует в кластере — это рантайм, и его судит
# сам предполёт; что содержимое секрета верно; производителей рецепта боевой
# площадки — их таблицу держит deploy/rollout_preflight_covers_required_secrets_test.go.
# Отказ самого шага `stack-up` на отсутствующем секрете доказывает самопроверка
# (заглушки helm/kubectl, кластер не нужен).
#
# Исходы — общей реализацией каталога (outcome.sh): 0 зелено · 1 находка о
# дереве · 2 условие не создано.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
MAKEFILE="${DECLARED_SECRET_MAKEFILE:-$DEPLOY_ROOT/Makefile}"
STACK_SECRETS="$DEPLOY_ROOT/scripts/stack-secrets.sh"
DERIVE="$DEPLOY_ROOT/scripts/required-secrets.py"

# shellcheck source=deploy/tests/helm/stacks.sh
. "$HERE/stacks.sh"
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"

# Стенды, которые `stack-up` НЕ раскатывает, и рецепт, которым они раскатываются.
# `<стенд>|<рецепт от deploy/>`. Сверяется с отказами цели в обе стороны (п.4):
# запись без отказа и отказ без записи одинаково находка.
REDIRECTED="
fe3455|helm/umbrella/cutover-fe3455.sh
"

# ── Разбор рецептов — ИСПОЛНЯЕМАЯ часть, а не текст ──────────────────────────
# recipe_order <makefile> <цель> <признак предполёта> — печатает находки по п.3
# (по строке) либо ничего. Рецепт режется на ЛОГИЧЕСКИЕ команды (продолжения `\`
# склеены): make обрывает цель на первой отказавшей команде, поэтому предполёт в
# отдельной команде раньше применения — законная форма; в ОДНОЙ команде с
# применением он законен только под `set -e`. Комментарии и тексты сообщений
# (`echo`, `printf`) исполняемой частью не считаются.
recipe_order() {
  python3 - "$1" "$2" "$3" <<'PY'
import re, sys
path, target, token = sys.argv[1:4]
lines = open(path, encoding="utf-8").read().split("\n")
body, inb = [], False
for ln in lines:
    if re.match(rf"^{re.escape(target)}:", ln):
        inb = True
        continue
    if inb:
        if ln.startswith("\t") or ln == "":
            body.append(ln)
        else:
            break
if not body:
    print(f"цели {target} в {path} нет — рецепт раската не найден")
    sys.exit(0)
cmds, cur = [], ""
for ln in body:
    s = ln[1:] if ln.startswith("\t") else ln
    if s.lstrip().startswith("#") or not s.strip():
        if cur:
            cmds.append(cur); cur = ""
        continue
    cur += " " + s.rstrip("\\").strip()
    if not s.rstrip().endswith("\\"):
        cmds.append(cur); cur = ""
if cur:
    cmds.append(cur)
def code(cmd):
    # тексты сообщений отбрасываются: слова предполёта стоят и в них
    parts = re.split(r";", cmd)
    return ";".join(p for p in parts if not re.match(r"^\s*@?-?\s*(echo|printf)\b", p))
pre = [(i, code(c).find(token)) for i, c in enumerate(cmds) if token in code(c)]
app = [(i, code(c).find("helm upgrade")) for i, c in enumerate(cmds) if "helm upgrade" in code(c)]
if not app:
    print(f"цель {target}: применения (`helm upgrade`) в рецепте не найдено — судить о порядке не о чем")
    sys.exit(0)
if not pre:
    print(f"цель {target}: предполёт `{token}` в рецепте НЕ ЗОВЁТСЯ — применение пройдёт и при отсутствующем секрете")
    sys.exit(0)
(ai, apos), (pi, ppos) = app[0], pre[0]
cmd = code(cmds[pi]).lstrip(" @-")
if pi > ai or (pi == ai and ppos > apos):
    print(f"цель {target}: предполёт `{token}` стоит ПОСЛЕ `helm upgrade` — это объяснение случившегося, а не отказ")
elif pi == ai and not re.match(r"^set -[a-z]*e", cmd):
    print(f"цель {target}: предполёт и `helm upgrade` в одной команде без `set -e` — отказ предполёта не остановит применение")
tail = code(cmds[pi])[ppos:].split(";")[0]
if "||" in tail:
    print(f"цель {target}: отказ предполёта ЗАГЛУШЁН (`{tail.strip()[:80]}`) — применение пойдёт дальше")
PY
}

# script_order <скрипт> — предполёт-производная стоит в исполняемой строке
# РАНЬШЕ первого `helm upgrade`, и скрипт, применяющий сам, идёт под `set -e`.
script_order() {
  python3 - "$1" <<'PY'
import re, sys
path = sys.argv[1]
lines = open(path, encoding="utf-8").read().split("\n")
code = [(i, l) for i, l in enumerate(lines) if l.strip() and not l.lstrip().startswith("#")
        and not re.match(r"^\s*(echo|printf|log|warn|die)\b", l)]
pre = [i for i, l in code if "required-secrets.py" in l]
app = [i for i, l in code if re.search(r"\bhelm upgrade\b", l)]
if not pre:
    print(f"{path}: предполёт не зовёт производную required-secrets.py — перечень требуемого выводится не ею")
elif app and pre[0] > app[0]:
    print(f"{path}: производная требуемого стоит ПОСЛЕ `helm upgrade`")
# `set -e` требуется ТОЛЬКО от скрипта, который применяет сам: у шага, который
# лишь судит (stack-secrets.sh), применения нет, и его отказ — явный код выхода,
# который останавливает рецепт вызывающего.
if app and not any(re.match(r"^set -[a-z]*e", l.strip()) for _, l in code):
    print(f"{path}: скрипт применяет сам и не под `set -e` — отказ предполёта не остановит применение")
PY
}

# judge <render> <профиль>… — по п.1: строки
#   DECL <имя> <путь>   · OK <имя> rendered|required · BLIND <имя> <путь> · REQ <имя>
judge() {
  python3 - "$DERIVE" "$@" <<'PY'
import importlib.util, sys, yaml
derive, render, profiles = sys.argv[1], sys.argv[2], sys.argv[3:]
spec = importlib.util.spec_from_file_location("required_secrets", derive)
rs = importlib.util.module_from_spec(spec); spec.loader.exec_module(rs)

def merge(a, b):
    # Семантика слоёв helm: отображения сливаются, прочее заменяется, null снимает ключ.
    if isinstance(a, dict) and isinstance(b, dict):
        out = dict(a)
        for k, v in b.items():
            if v is None:
                out.pop(k, None)
            else:
                out[k] = merge(a.get(k), v) if k in a else v
        return out
    return b

def walk(node, path, out):
    if isinstance(node, dict):
        for k, v in node.items():
            p = f"{path}.{k}" if path else str(k)
            if k == "existingSecret" and isinstance(v, str) and v.strip():
                out.setdefault(v.strip(), p)
            else:
                walk(v, p, out)
    elif isinstance(node, list):
        for i, v in enumerate(node):
            walk(v, f"{path}[{i}]", out)

merged = {}
for p in profiles:
    with open(p, encoding="utf-8") as fh:
        merged = merge(merged, yaml.safe_load(fh) or {})
declared = {}
walk(merged, "", declared)
docs = [d for d in yaml.safe_load_all(open(render, encoding="utf-8")) if isinstance(d, dict)]
if rs.carriers(docs) == 0:
    print("NORENDER")
    sys.exit(0)
made = {(d.get("metadata") or {}).get("name") for d in docs if d.get("kind") == "Secret"}
need = {n for n, _, _ in rs.required(docs)}
for name, path in sorted(declared.items()):
    print(f"DECL {name} {path}")
    if name in made:
        print(f"OK {name} rendered")
    elif name in need:
        print(f"OK {name} required")
        print(f"REQ {name}")
    else:
        print(f"BLIND {name} {path}")
PY
}

# check_stack <стенд> <рецепт: stack-up|путь> — утверждения п.1–п.2 по стенду.
check_stack() {
  local stack="$1" recipe="$2" args render out names prof
  args="$(stacks_args "$stack" "$UMBRELLA")" \
    || fatal "стенд $stack: цепочка не прочитана из stacks.txt — рендерить нечем"
  # shellcheck disable=SC2086  # $args — намеренно раскрываемый набор -f
  helm_try kacho-umbrella "$UMBRELLA" -n kacho $args
  render_or_fatal "стенд $stack"
  render="$(mktemp)"; printf '%s\n' "$HELM_OUT" >"$render"
  # КОД ЧИТАТЕЛЯ ПЕРЕЧНЯ ПОТРЕБОВАН ПРИСВАИВАНИЕМ, а не списком `for`: подстановка
  # в списке `for` теряет код всегда, и отказ таблицы дал бы суд над НУЛЁМ
  # профилей — «объявлений ноль» вместо «таблица не прочиталась».
  local profiles=() chain
  chain="$(stacks_chain "$stack")" \
    || fatal "стенд $stack: состав цепочки не прочитан из stacks.txt — судить объявления не о чем"
  for prof in $chain; do profiles+=("$UMBRELLA/$prof"); done
  out="$(judge "$render" "${profiles[@]}")" \
    || { rm -f "$render"; fatal "стенд $stack: разбор объявлений сорвался — судить не о чем"; }
  rm -f "$render"
  case "$out" in *NORENDER*) fatal "стенд $stack: в рендере ноль шаблонов пода — прочитано ноль, а не требуется ноль" ;; esac
  local decl=0 line kind name rest req=""
  while IFS= read -r line; do
    kind="${line%% *}"; rest="${line#* }"; name="${rest%% *}"
    case "$kind" in
      DECL)  decl=$((decl + 1)) ;;
      OK)    ok ;;
      REQ)   req="$req $name" ;;
      BLIND) violation "стенд $stack: объявлен existingSecret '$name' (${rest#* }), но его не производит рендер и НЕ ТРЕБУЕТ предполёт — либо ни один шаблон его не читает (объявление без читателя), либо читает формой, которой производная scripts/required-secrets.py не знает. Раскат не откажет до применения." ;;
    esac
  done <<<"$out"
  DECLARED_TOTAL=$((DECLARED_TOTAL + decl))
  STACK_CENSUS="$STACK_CENSUS $stack:$decl"
  # п.2 — ведомость производителей шага `stack-up`.
  if [ "$recipe" = stack-up ] && [ -n "$req" ]; then
    # shellcheck disable=SC2086  # $req — перечень имён, дробление намеренное
    names="$(bash "$STACK_SECRETS" --producer-of $req)" \
      || fatal "ведомость производителей stack-secrets.sh не ответила — судить не о чем"
    while IFS=$'\t' read -r name rest; do
      [ -n "$name" ] || continue
      if [ -n "$rest" ]; then ok
      else violation "стенд $stack: у требуемого '$name' в ведомости производителей stack-secrets.sh НЕТ строки — отказ назовёт имя, но не того, кто его заводит"; fi
    done <<<"$names"
  fi
}

# ── п.3–п.4: рецепты ─────────────────────────────────────────────────────────
check_recipes() {
  local f refused want line name script
  f="$(recipe_order "$MAKEFILE" stack-up "scripts/stack-secrets.sh")"
  if [ -n "$f" ]; then while IFS= read -r line; do violation "$line"; done <<<"$f"; else ok; fi
  f="$(script_order "$STACK_SECRETS")"
  if [ -n "$f" ]; then while IFS= read -r line; do violation "$line"; done <<<"$f"; else ok; fi
  # Отказы читаются из ТЕЛА цели stack-up, а не из всего файла: та же форма
  # сравнения в другой цели отказом этой цели не является.
  refused="$(awk '/^stack-up:/{inb=1; next} inb && /^[^\t]/{exit} inb' "$MAKEFILE" \
    | grep -oE '"\$\(STACK\)" = "[a-z0-9-]+"' | sed -E 's/.*= "([a-z0-9-]+)"/\1/' | sort -u)"
  want="$(printf '%s\n' "$REDIRECTED" | grep -v '^$' | cut -d'|' -f1 | sort -u)"
  if [ "$refused" = "$want" ]; then ok
  else violation "стенды, которые stack-up отсылает к своему рецепту ($(tr '\n' ' ' <<<"$refused")), расходятся с REDIRECTED этой проверки ($(tr '\n' ' ' <<<"$want")) — стенд без рецепта с предполётом либо запись без предмета"; fi
  while IFS='|' read -r name script; do
    [ -n "$name" ] || continue
    require_file_present "$DEPLOY_ROOT/$script" "рецепт стенда $name"
    f="$(script_order "$DEPLOY_ROOT/$script")"
    if [ -n "$f" ]; then while IFS= read -r line; do violation "$line"; done <<<"$f"; else ok; fi
  done <<<"$REDIRECTED"
}

recipe_of() {
  local line
  while IFS='|' read -r name script; do
    [ "$name" = "$1" ] && { printf '%s\n' "$script"; return 0; }
  done <<<"$REDIRECTED"
  echo stack-up
}

# ═════════════════════════════════════════════════════════════════════════════
# САМОПРОВЕРКА — инъекции в обе стороны, живой рабочей копии не касается.
# ═════════════════════════════════════════════════════════════════════════════
# `$(STACK)` в самопроверке — ЛИТЕРАЛ make-рецепта синтетического Makefile, а не
# подстановка оболочки; одинарные кавычки там намеренны.
# shellcheck disable=SC2016
self_test() {
  local st rc=0 n=0
  st="$(mktemp -d)"; trap 'rm -rf "$st"' RETURN
  expect() { # expect <метка> <условие-истинно?>
    n=$((n + 1))
    if [ "$2" = yes ]; then echo "  ✓ $1"; else echo "  ✗ $1"; rc=1; fi
  }
  has() { [[ "$1" == *"$2"* ]] && echo yes || echo no; }
  hasnt() { [[ "$1" != *"$2"* ]] && echo yes || echo no; }

  # ── Производная: формы ссылок и вычитаемое ────────────────────────────────
  pod() { # pod <имя> <yaml тома/окружения> → Deployment
    printf 'apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: %s}\nspec:\n  template:\n    spec:\n%s\n' "$1" "$2"
  }
  pod probe "      containers: [{name: c, image: x}]
      volumes: [{name: v, projected: {sources: [{secret: {name: declared-but-absent}}]}}]" >"$st/projected.yaml"
  pod probe "      containers: [{name: c, image: x}]
      volumes: [{name: v, projected: {sources: [{secret: {name: declared-but-absent, optional: true}}]}}]" >"$st/projected-opt.yaml"
  pod probe "      containers: [{name: c, image: x, envFrom: [{secretRef: {name: declared-but-absent}}]}]" >"$st/envfrom.yaml"
  pod probe "      containers: [{name: c, image: x}]
      volumes: [{name: v, secret: {secretName: minted-by-cert-manager}}]" >"$st/cert.yaml"
  printf -- '---\napiVersion: cert-manager.io/v1\nkind: Certificate\nmetadata: {name: c}\nspec: {secretName: minted-by-cert-manager}\n' >>"$st/cert.yaml"
  printf 'apiVersion: v1\nkind: ConfigMap\nmetadata: {name: nothing}\n' >"$st/nopods.yaml"

  echo "-- производная (scripts/required-secrets.py) --"
  expect "том projected, обязательный → требуется (обе прежние копии его не видели)" \
    "$(has "$(python3 "$DERIVE" <"$st/projected.yaml")" declared-but-absent)"
  expect "том projected, optional → не требуется (законный близнец)" \
    "$(hasnt "$(python3 "$DERIVE" <"$st/projected-opt.yaml")" declared-but-absent)"
  expect "envFrom, обязательный → требуется (копия боевой раскатки его не видела)" \
    "$(has "$(python3 "$DERIVE" <"$st/envfrom.yaml")" declared-but-absent)"
  expect "секрет, который чеканит cert-manager того же применения → не требуется" \
    "$(hasnt "$(python3 "$DERIVE" <"$st/cert.yaml")" minted-by-cert-manager)"
  local drc; python3 "$DERIVE" <"$st/nopods.yaml" >/dev/null 2>&1 && drc=0 || drc=$?
  expect "ноль шаблонов пода → код 2 («прочитано ноль»), а не пустой перечень (код $drc)" \
    "$([ "$drc" = 2 ] && echo yes || echo no)"

  # ── Шаг stack-up: отказ ДО применения на отсутствующем секрете ─────────────
  mkdir -p "$st/bin"
  cat >"$st/bin/helm" <<'STUB'
#!/usr/bin/env bash
[ "${1:-}" = template ] && { cat "$STUB_RENDER"; exit 0; }
exit 0
STUB
  cat >"$st/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
echo "kubectl $*" >>"$STUB_LOG"
case " $* " in
  *" config current-context "*) echo "managed-stand"; exit 0 ;;
  *" get namespace "*) exit 0 ;;
  *" get secret "*)
    for a in "$@"; do last="$a"; done
    for s in $STUB_ABSENT; do [ "$s" = "$last" ] && { echo "NotFound" >&2; exit 1; }; done
    exit 0 ;;
  *" apply "*|*" create "*) echo "ПРИМЕНЕНИЕ" >>"$STUB_LOG"; exit 0 ;;
esac
exit 0
STUB
  chmod +x "$st/bin/helm" "$st/bin/kubectl"
  run_step() { # run_step <render> <отсутствующие> → STEP_RC, STEP_OUT, STEP_LOG
    : >"$st/kubectl.log"
    STEP_OUT="$(PATH="$st/bin:$PATH" STUB_RENDER="$1" STUB_ABSENT="$2" STUB_LOG="$st/kubectl.log" \
      bash "$STACK_SECRETS" dev 2>&1)" && STEP_RC=0 || STEP_RC=$?
    STEP_LOG="$(cat "$st/kubectl.log")"
  }
  echo "-- шаг stack-up (stack-secrets.sh; helm и kubectl — заглушки, площадка управляемая) --"
  run_step "$st/projected.yaml" "declared-but-absent"
  expect "секрета нет, потребитель — том projected → отказ кодом 1 с именем (код $STEP_RC)" \
    "$([ "$STEP_RC" = 1 ] && [[ "$STEP_OUT" == *declared-but-absent* ]] && [[ "$STEP_OUT" == *"НЕ применена"* ]] && echo yes || echo no)"
  expect "…и ничего не применено (kubectl apply/create не звался)" "$(hasnt "$STEP_LOG" ПРИМЕНЕНИЕ)"
  run_step "$st/projected.yaml" ""
  expect "тот же стенд, секрет на месте → код 0 (законный близнец; код $STEP_RC)" \
    "$([ "$STEP_RC" = 0 ] && [[ "$STEP_OUT" == *"отсутствует 0"* ]] && echo yes || echo no)"
  run_step "$st/nopods.yaml" ""
  expect "производная прочитала ноль → код 2, а не «на месте всё» (код $STEP_RC)" \
    "$([ "$STEP_RC" = 2 ] && echo yes || echo no)"

  # ── Суд над объявлениями ───────────────────────────────────────────────────
  echo "-- суд над объявлениями профиля --"
  printf 'registry:\n  zot:\n    auth:\n      existingSecret: declared-but-absent\n' >"$st/profile.yaml"
  local out
  out="$(judge "$st/nopods.yaml" "$st/profile.yaml")"
  expect "рендер без единого шаблона пода → суд отказывается судить (NORENDER), а не «объявления покрыты»" \
    "$(has "$out" NORENDER)"
  pod other "      containers: [{name: c, image: x}]" >"$st/unrelated.yaml"
  out="$(judge "$st/unrelated.yaml" "$st/profile.yaml")"
  expect "объявлено, ни один шаблон не читает → BLIND с путём объявления" \
    "$(has "$out" "BLIND declared-but-absent registry.zot.auth.existingSecret")"
  out="$(judge "$st/projected.yaml" "$st/profile.yaml")"
  expect "объявлено, читает том projected → требуется предполётом (законный близнец)" \
    "$(has "$out" "OK declared-but-absent required")"
  printf 'registry:\n  zot:\n    auth:\n      existingSecret: null\n' >"$st/profile-null.yaml"
  out="$(judge "$st/unrelated.yaml" "$st/profile.yaml" "$st/profile-null.yaml")"
  expect "верхний слой снимает объявление (null) → объявления нет, находки нет" \
    "$(hasnt "$out" declared-but-absent)"

  # ── Порядок в рецепте ──────────────────────────────────────────────────────
  echo "-- порядок в рецепте stack-up --"
  mk() { printf 'stack-up:\n%b\n\nnext:\n\t@true\n' "$1" >"$st/Makefile"; recipe_order "$st/Makefile" stack-up scripts/stack-secrets.sh; }
  out="$(mk '\t@set -e; \\\n\tbash scripts/stack-secrets.sh $(STACK); \\\n\thelm upgrade --install x ./helm/umbrella')"
  expect "форма дерева: одна команда под set -e, предполёт раньше → молчит" "$([ -z "$out" ] && echo yes || echo no)"
  out="$(mk '\t@set -e; \\\n\thelm upgrade --install x ./helm/umbrella')"
  expect "предполёт не зовётся → находка" "$(has "$out" "НЕ ЗОВЁТСЯ")"
  out="$(mk '\t@set -e; \\\n\thelm upgrade --install x ./helm/umbrella; \\\n\tbash scripts/stack-secrets.sh $(STACK)')"
  expect "предполёт после helm upgrade → находка" "$(has "$out" "ПОСЛЕ")"
  out="$(mk '\t@bash scripts/stack-secrets.sh $(STACK) || true; \\\n\thelm upgrade --install x ./helm/umbrella')"
  expect "отказ предполёта заглушён «|| true» → находка" "$(has "$out" "ЗАГЛУШЁН")"
  out="$(mk '\t@echo "позови scripts/stack-secrets.sh"; \\\n\thelm upgrade --install x ./helm/umbrella')"
  expect "предполёт назван только в тексте сообщения → находка (упоминание не вызов)" "$(has "$out" "НЕ ЗОВЁТСЯ")"
  out="$(mk '\t@bash scripts/stack-secrets.sh $(STACK)\n\thelm upgrade --install x ./helm/umbrella')"
  expect "предполёт отдельной командой раньше применения → молчит (make обрывает цель)" "$([ -z "$out" ] && echo yes || echo no)"

  # ── Ведомость производителей ───────────────────────────────────────────────
  echo "-- ведомость производителей шага stack-up --"
  out="$(bash "$STACK_SECRETS" --producer-of zot-auth operator-only-unledgered)"
  expect "имя из ведомости → производитель назван" "$(has "$(grep '^zot-auth' <<<"$out")" оператор)"
  expect "имя вне ведомости → пустое поле (гейт назовёт его находкой)" \
    "$([ "$(awk -F'\t' '$1=="operator-only-unledgered"{print $2}' <<<"$out")" = "" ] && echo yes || echo no)"

  echo "случаев исполнено: $n"
  [ "$n" -eq 21 ] || { echo "FAIL: исполнено $n случаев из 21"; rc=1; }
  # SCRIPT объявляет outcome.sh (имя вызывающего скрипта).
  # shellcheck disable=SC2153
  if [ "$rc" -eq 0 ]; then echo "PASS: $SCRIPT --self-test"; else echo "FAIL: $SCRIPT --self-test"; fi
  return "$rc"
}

require_python_yaml
require_file_present "$DERIVE" "производная требуемых секретов"
require_file_present "$STACK_SECRETS" "шаг stack-up"
require_file_present "$MAKEFILE" "Makefile"

if [ "${1:-}" = "--self-test" ]; then
  self_test; exit $?
fi

require_helm
DECLARED_TOTAL=0
STACK_CENSUS=""
STACKS="$(stacks_names)" || fatal "перечень стендов не прочитан"
check_recipes
for s in $STACKS; do
  check_stack "$s" "$(recipe_of "$s")"
done
[ "$DECLARED_TOTAL" -gt 0 ] \
  || fatal "ни один стенд не объявляет existingSecret — либо ключ переехал (проверка слепа), либо предмет исчез; «ноль объявлений» не есть «всё проверено»"
findings_verdict "стендов $(wc -w <<<"$STACKS"), объявлений existingSecret $DECLARED_TOTAL (по стендам:$STACK_CENSUS)"
