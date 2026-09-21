#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stack-secrets.sh — ПРЕДУСЛОВНЫЕ СЕКРЕТЫ ОБЪЯВЛЕННОГО СТЕНДА: проверить, что они
# есть, и — только на локальном стенде — завести недостающие.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Цепочка стенда `own` (`values.prod.yaml` + `values.own.yaml`) объявляет
# одиннадцать секретов ссылкой `existingSecret`: девять пар учётных данных баз,
# учётные данные хранилища слоёв и подписной секрет печенья консоли входа. Ни
# один шаблон их не создаёт — под боевым слоем это ШАГ ОПЕРАТОРА, и так и
# написано в самом профиле. Ещё четыре — ключевой материал службы доступа —
# заводит посев (`dev-prod-secrets.sh`), но его звал ТОЛЬКО `dev-up`.
#
# Следствие, наблюдавшееся вживую: `make stack-up STACK=own` на чистом кластере
# применял выкатку, поды вставали в `CreateContainerConfigError` либо в отказ
# стража старта, `helm --wait` выстаивал свой предел и падал ПО СРОКУ, а не по
# причине. Стенд `own` при этом «существовал» — но поднимался руками, а не из
# дерева, и любой вердикт с него относился неизвестно к чему.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ОДИН СКРИПТ ДЕЛАЕТ ДВА РАЗНЫХ ДЕЛА (и почему это не «режим на глаз»)
#
# Производитель ключевого материала зависит от ПЛОЩАДКИ, а не от желания
# раскатывающего:
#
#   • управляемый кластер — материал приходит ВНЕ дерева (хранилище секретов,
#     external-secrets, руки оператора). Чеканить его здесь нельзя: мы завели бы
#     объект, за который отвечает другой механизм, и он бы с ним потом воевал.
#     Поэтому там скрипт ТОЛЬКО СУДИТ и отказывает ДО применения, называя
#     каждый недостающий секрет и его производителя;
#
#   • локальный стенд kind — производителя вне дерева НЕТ вовсе. «Оператор
#     заведёт руками» здесь означает «стенд из дерева не поднимается», а это
#     ровно тот дефект, ради которого скрипт написан. Поэтому там недостающее
#     ПРОИЗВОДИТСЯ по ведомости ниже.
#
# Площадка разрешается ЖИВЫМ kubectl и kind, а не переменной окружения: имя
# контекста выбирает автор kubeconfig, и совпадение имён ничего не доказывает —
# сверяется адрес apiserver'а (тот же довод, что у `guard-kind-context`).
#
# ─────────────────────────────────────────────────────────────────────────────
# ТРЕБУЕМОЕ МНОЖЕСТВО ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ
#
# Три источника, и каждый закрывает то, чего не видят два других:
#
#   (а) ОБЯЗАТЕЛЬНЫЕ ссылки отрендеренного стенда (`secretKeyRef`, `envFrom`,
#       том): их kubelet не подставит вовсе, контейнер не стартует;
#   (б) секреты, которые заводит ПОСЕВ: ссылки на них НЕОБЯЗАТЕЛЬНЫ, под
#       поднимется и откажет позже уже стражем старта службы — рендер про них
#       молчит by construction;
#   (в) ведомость производителей ниже — те, у кого на локальном стенде есть
#       рецепт.
#
# Из (а) ВЫЧИТАЕТСЯ то, что производит сам выкат: `kind: Secret` рендера и
# `spec.secretName` каждого `Certificate` — их чеканит cert-manager из того же
# применения, требовать их ДО него значило бы требовать невозможного.
#
# ─────────────────────────────────────────────────────────────────────────────
# ВЕЛИЧИНА ПОРОЖДАЕТСЯ ОДНАЖДЫ И ПЕРЕИСПОЛЬЗУЕТСЯ
#
# Каждый рецепт сначала смотрит, есть ли секрет, и только потом чеканит. Это не
# стиль: паролем базы УЖЕ инициализирован том, ключом обёртки УЖЕ обёрнуты
# записи. Перевыпуск не «обновляет секрет», а делает записанное недоступным —
# тихо, без единого отказа. Дисциплину держит
# deploy/tests/helm/secret-material-survives-recreation-test.sh.
#
# ─────────────────────────────────────────────────────────────────────────────
# КОДЫ ВОЗВРАТА
#
#   0  все требуемые секреты на месте (часть могла быть заведена этим прогоном)
#   1  недостающие остались: на управляемом кластере — всегда (их заводит не
#      этот скрипт), на локальном — если у секрета нет рецепта либо чеканка
#      отказала. Каждый назван поимённо вместе с производителем
#   2  ПРЕДПОСЫЛКА ИСЧЕЗЛА: нет helm/kubectl/python3, кластер не отвечает,
#      цепочка стенда не прочиталась, рендер отказал либо требуемых секретов
#      выведено НОЛЬ. «Ноль находок» обязано быть отличимо от «ноль прочитанного»
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
SEED_SH="$HERE/dev-prod-secrets.sh"

NS="${KACHO_NAMESPACE:-${STACK_NAMESPACE:-kacho}}"
RELEASE="${STACK_RELEASE:-kacho-umbrella}"

log()  { printf '=== stack-secrets: %s\n' "$1"; }
warn() { printf 'stack-secrets: %s\n' "$1" >&2; }
die()  { printf 'ABORT: stack-secrets — %s\n' "$1" >&2; exit "${2:-1}"; }

STACK="${1:-}"
[ -n "$STACK" ] || die "имя стенда не задано. Умолчания здесь быть не может: скрипт
       смотрит на кластер активного контекста, а «наверное, dev» — не выбор стенда." 2
shift

for t in helm kubectl python3 openssl; do
  command -v "$t" >/dev/null 2>&1 || die "нет '$t' — судить нечем (условие прогона, а не находка)" 2
done
python3 -c 'import yaml' 2>/dev/null || die "нет PyYAML — рендер разобрать нечем" 2

# ── Цепочка — из ЕДИНСТВЕННОЙ таблицы дерева ────────────────────────────────
# shellcheck source=deploy/tests/helm/stacks.sh
. "$DEPLOY_ROOT/tests/helm/stacks.sh"
ARGS="$(stacks_args "$STACK" "$UMBRELLA")" \
  || die "цепочка стенда '$STACK' не прочиталась — helm без единого -f сел бы на
       умолчания чарта, то есть на посадку РАЗРАБОТКИ" 2
[ -n "$ARGS" ] || die "цепочка стенда '$STACK' прочиталась ПУСТОЙ" 2

kubectl -n "$NS" get namespace "$NS" >/dev/null 2>&1 \
  || kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1 \
  || die "кластер не отвечает: ни прочитать, ни завести namespace '$NS'" 2

# ── Рендер ТОЙ ЖЕ цепочки, которой пойдёт выкатка ───────────────────────────
# shellcheck disable=SC2086  # $ARGS — намеренно раскрываемый набор -f
RENDER="$(helm template "$RELEASE" "$UMBRELLA" -n "$NS" $ARGS "$@" 2>&1)" || {
  printf '%s\n' "$RENDER" >&2
  die "helm template цепочки '$STACK' отказал (его текст выше) — вывести требуемое не из чего.
       Это отказ, а не пустой успех: предполёт, который ничего не прочитал, неотличим
       от предполёта, которому нечего сказать." 2
}

REQUIRED="$(
  {
    printf '%s\n' "$RENDER" | python3 -c '
import sys, yaml
docs=[d for d in yaml.safe_load_all(sys.stdin) if isinstance(d, dict)]
# Производит сам выкат: Secret рендера и то, что чеканит cert-manager по Certificate.
made={d["metadata"]["name"] for d in docs if d.get("kind")=="Secret"}
made|={(d.get("spec") or {}).get("secretName") for d in docs if d.get("kind")=="Certificate"}
made.discard(None)
# Имя, названное в аргументах задания (Job) ТОГО ЖЕ применения, этим
# применением и заводится (напр. `--secret-name=…` у создателя удостоверения
# вебхука допуска). Требовать его ДО применения значит требовать невозможного.
for d in docs:
    if d.get("kind")!="Job": continue
    pod=((d.get("spec") or {}).get("template") or {}).get("spec") or {}
    for c in (pod.get("containers") or [])+(pod.get("initContainers") or []):
        for a in (c.get("command") or [])+(c.get("args") or []):
            for part in str(a).replace("="," ").split():
                made.add(part)
need=set()
def scan(pod):
    for c in (pod.get("containers") or [])+(pod.get("initContainers") or []):
        for e in c.get("env") or []:
            r=(e.get("valueFrom") or {}).get("secretKeyRef")
            if r and not r.get("optional", False): need.add(r["name"])
        for ef in c.get("envFrom") or []:
            r=ef.get("secretRef")
            if r and not r.get("optional", False): need.add(r["name"])
    for v in pod.get("volumes") or []:
        s=v.get("secret")
        if s and s.get("secretName") and not s.get("optional", False): need.add(s["secretName"])
for d in docs:
    spec=d.get("spec") or {}
    if d.get("kind")=="Pod":
        scan(spec); continue
    tpl=spec.get("template") or ((spec.get("jobTemplate") or {}).get("spec") or {}).get("template")
    if tpl: scan(tpl.get("spec") or {})
print("\n".join(sorted(n for n in need if n not in made)))
'
    grep -oE 'create secret generic [a-z0-9][a-z0-9-]*' "$SEED_SH" | awk '{print $4}'
  } | sort -u | grep -v '^$'
)" || die "перечень требуемых секретов не выведен" 2

req_n="$(printf '%s\n' "$REQUIRED" | grep -c .)"
[ "$req_n" -gt 0 ] || die "требуемых секретов выведено НОЛЬ — обход слеп, а не стенд готов" 2

missing=""
for s in $REQUIRED; do
  kubectl -n "$NS" get secret "$s" >/dev/null 2>&1 || missing="$missing $s"
done

if [ -z "$missing" ]; then
  log "стенд $STACK: требуется $req_n, на месте $req_n, отсутствует 0."
  exit 0
fi

# ── ПЛОЩАДКА: разрешается живым kubectl и kind ──────────────────────────────
# Имя контекста ничего не доказывает — сверяется адрес apiserver'а.
is_local_stand() {
  local ctx have want name
  ctx="$(kubectl config current-context 2>/dev/null)" || return 1
  [ -n "$ctx" ] || return 1
  case "$ctx" in kind-*) ;; *) return 1 ;; esac
  command -v kind >/dev/null 2>&1 || return 1
  name="${ctx#kind-}"
  want="$(kind get kubeconfig --name "$name" 2>/dev/null | sed -n 's/^ *server: *//p' | head -1)"
  have="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null)"
  [ -n "$want" ] && [ "$want" = "$have" ]
}

# ── ВЕДОМОСТЬ ПРОИЗВОДИТЕЛЕЙ ────────────────────────────────────────────────
# Строка объясняет, КТО заводит секрет на управляемой площадке; функция ниже
# умеет завести его на локальном стенде. Секрет без строки — находка, а не
# «наверное, появится»: отказ назовёт его отдельно.
producer_of() {
  case "$1" in
    kaname-hook-token)        echo "посев dev-prod-secrets.sh · на площадке — оператор: общий секрет обратных вызовов, ключ token" ;;
    kaname-jwks-enc-key)      echo "посев dev-prod-secrets.sh · на площадке — оператор: ключ обёртки подписного ключа, ключ enc_key" ;;
    kaname-second-factor-enc-key) echo "посев dev-prod-secrets.sh · на площадке — оператор: ключ обёртки секретов второго фактора, ключ enc_key" ;;
    kaname-bootstrap-sa-key)  echo "посев dev-prod-secrets.sh · на площадке — оператор: ключ ES256 учётки первичной чеканки, ключ private_key_pem" ;;
    "$RELEASE"-pg-*)          echo "учётные данные базы (ключи password + postgres-password) — профиль объявляет их existingSecret, на площадке заводит оператор" ;;
    zot-auth)                 echo "учётные данные хранилища слоёв (username + password + htpasswd, bcrypt того же пароля) — на площадке заводит оператор" ;;
    kratos-selfservice-ui-cookie-secret) echo "подписной секрет печенья консоли входа (ключ cookieSecret, 32 знака) — на площадке заводит оператор" ;;
    "$RELEASE"-hydra-stand|"$RELEASE"-kratos-stand) echo "секрет поставщика ВНЕ helm (ключи dsn + величины сессий) — вторая законная форма из identity-session-secret-guard; на площадке заводит слой площадки" ;;
    *)                        echo "" ;;
  esac
}

# create_generic <имя> <ключ=значение>… — чеканит ОДИН раз; уже существующий
# объект не трогает вовсе.
create_generic() {
  local name="$1"; shift
  if kubectl -n "$NS" get secret "$name" >/dev/null 2>&1; then
    log "$name уже есть — переиспользуется (величина могла быть чем-то записана)"
    return 0
  fi
  local args=()
  local kv
  for kv in "$@"; do args+=(--from-literal="$kv"); done
  kubectl -n "$NS" create secret generic "$name" "${args[@]}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null || return 1
  log "заведён $name"
}

# bcrypt <пользователь> <пароль> — строка htpasswd, которую понимает zot.
# Порядок источников, и каждый называется вслух: своего bcrypt у оболочки нет, а
# `openssl passwd` его не умеет вовсе. Нечем — это «условие не создано», а не
# повод положить формат, который хранилище слоёв молча не примет.
bcrypt_line() {
  local user="$1" pass="$2"
  if command -v htpasswd >/dev/null 2>&1; then
    htpasswd -Bbn "$user" "$pass" | head -1
    return 0
  fi
  python3 - "$user" "$pass" <<'PY' 2>/dev/null
import sys
try:
    import bcrypt
except ImportError:
    sys.exit(3)
u, p = sys.argv[1], sys.argv[2]
print(u + ":" + bcrypt.hashpw(p.encode(), bcrypt.gensalt(prefix=b"2b")).decode())
PY
}

# pg_facts <имя набора> — «<пользователь> <база> <режим шифрования>», прочитанные
# из РЕНДЕРА того же применения. Выписывать их здесь значило бы завести вторую
# копию того, что объявляет профиль, и копия разошлась бы молча.
pg_facts() {
  printf '%s\n' "$RENDER" | python3 -c '
import sys, yaml
want=sys.argv[1]
for d in yaml.safe_load_all(sys.stdin):
    if not isinstance(d, dict) or d.get("kind")!="StatefulSet": continue
    if d["metadata"]["name"]!=want: continue
    env={}
    for c in (d["spec"]["template"]["spec"].get("containers") or []):
        for e in (c.get("env") or []):
            if e.get("value") is not None: env[e["name"]]=e["value"]
    tls = str(env.get("POSTGRESQL_ENABLE_TLS","no")).lower()=="yes"
    print(env.get("POSTGRES_USER",""), env.get("POSTGRES_DATABASE",""), "require" if tls else "disable")
    break
' "$1"
}

# ory_stand_secret <имя секрета> <имя набора базы> <ключ величины сессии>…
# Строка соединения и величины сессий чеканятся ОДНИМ объектом: чарт поставщика,
# переведённый на секрет вне helm, перенаправляет на него ВСЕ ключи.
ory_stand_secret() {
  local name="$1" pg="$2"; shift 2
  produce "$pg" || return 1
  local pass user db mode facts
  pass="$(kubectl -n "$NS" get secret "$pg" -o jsonpath='{.data.password}' | base64 -d)" || return 1
  [ -n "$pass" ] || { warn "у секрета $pg нет ключа password — строку соединения собрать не из чего"; return 1; }
  facts="$(pg_facts "$pg")"
  user="$(printf '%s' "$facts" | awk '{print $1}')"
  db="$(printf '%s' "$facts" | awk '{print $2}')"
  mode="$(printf '%s' "$facts" | awk '{print $3}')"
  [ -n "$user" ] && [ -n "$db" ] && [ -n "$mode" ] || {
    warn "рендер не назвал пользователя/базу/режим для $pg — собирать строку соединения вслепую нельзя"
    return 1
  }
  local kv=("dsn=postgres://$user:$pass@$pg:5432/$db?sslmode=$mode")
  local k
  for k in "$@"; do kv+=("$k=$(openssl rand -hex 16)"); done
  create_generic "$name" "${kv[@]}"
}

seed_ran=0
produce() {
  local name="$1"
  case "$name" in
    kaname-*)
      # Ключевой материал службы доступа заводит ОДИН производитель — посев.
      # Второго места о том же предмете не заводится: они разошлись бы ровно
      # там, где расхождение опасно.
      if [ "$seed_ran" -eq 0 ]; then
        KACHO_NAMESPACE="$NS" bash "$SEED_SH" || return 1
        seed_ran=1
      fi
      kubectl -n "$NS" get secret "$name" >/dev/null 2>&1
      ;;
    "$RELEASE"-pg-*)
      create_generic "$name" \
        "password=$(openssl rand -hex 24)" \
        "postgres-password=$(openssl rand -hex 24)"
      ;;
    zot-auth)
      local user pass line
      user="kacho-registry"
      pass="$(openssl rand -hex 24)"
      line="$(bcrypt_line "$user" "$pass")" && [ -n "$line" ] || {
        warn "bcrypt взять нечем: нет ни htpasswd(1), ни модуля bcrypt у python3."
        warn "  Это УСЛОВИЕ ПРОГОНА: хранилище слоёв принимает только bcrypt, и"
        warn "  подставить сюда другой формат значило бы завести стенд, который"
        warn "  не аутентифицирует никого и выглядит поднятым."
        return 1
      }
      create_generic "$name" "username=$user" "password=$pass" "htpasswd=$line"
      ;;
    kratos-selfservice-ui-cookie-secret)
      create_generic "$name" "cookieSecret=$(openssl rand -hex 16)"
      ;;
    "$RELEASE"-hydra-stand)
      ory_stand_secret "$name" "$RELEASE-pg-hydra" secretsSystem secretsCookie
      ;;
    "$RELEASE"-kratos-stand)
      ory_stand_secret "$name" "$RELEASE-pg-kratos" secretsDefault secretsCookie secretsCipher
      ;;
    *) return 1 ;;
  esac
}

if ! is_local_stand; then
  warn "стенд $STACK: требуется $req_n, ОТСУТСТВУЕТ $(printf '%s\n' $missing | grep -c .):"
  for s in $missing; do
    who="$(producer_of "$s")"
    printf '  %s — %s\n' "$s" "${who:-производитель НЕ НАЗВАН}" >&2
  done
  die "выкатка НЕ применена. Отказ наступает ЗДЕСЬ, до применения, а не через предел
       ожидания готовности: под, которому не хватает секрета, ждёт молча, и \`helm --wait\`
       истекает ПО СРОКУ, не называя причины. Активный контекст — не локальный стенд kind,
       поэтому ключевой материал здесь не чеканится: на управляемой площадке он приходит
       вне дерева, и объект, заведённый мимо своего механизма, потом с ним воюет.
       Заведи перечисленное в ns $NS и повтори."
fi

log "локальный стенд kind: производителя вне дерева нет — завожу недостающее ($(printf '%s\n' $missing | grep -c .) из $req_n)"
unmade=""
for s in $missing; do
  produce "$s" || unmade="$unmade $s"
done

still=""
for s in $REQUIRED; do
  kubectl -n "$NS" get secret "$s" >/dev/null 2>&1 || still="$still $s"
done
if [ -n "$still" ]; then
  warn "стенд $STACK: после посева ОТСУТСТВУЕТ $(printf '%s\n' $still | grep -c .):"
  for s in $still; do
    who="$(producer_of "$s")"
    printf '  %s — %s\n' "$s" "${who:-рецепта на локальном стенде НЕТ}" >&2
  done
  die "выкатка НЕ применена: предусловие стенда не создано."
fi
[ -z "$unmade" ] || warn "рецепт отказал, но секрет оказался на месте:$unmade"
log "стенд $STACK: требуется $req_n, на месте $req_n, отсутствует 0."
