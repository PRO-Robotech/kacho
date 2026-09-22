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
#       молчит by construction. Требуется из них ровно то, на что рендер
#       ССЫЛАЕТСЯ и что служба ЧИТАЕТ под посадкой этого рендера: ключ обёртки
#       секретов второго фактора читается только под `own` (страж старта службы,
#       `Lanes: own`), и под `external` его отсутствие стенду не мешает — так и
#       обещает шапка посева. Прежде перечень брал ВСЕ имена посева безусловно и
#       отказывал стенду, который поднимался (задача #2803);
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
# «Смотрит» — значит СПРАШИВАЕТ СЕРВЕР, и исходов у вопроса три (задача #2803):
# есть · NotFound · отказ. Прежде отказ `get` (срок, сеть, RBAC) читался как «нет»,
# величина чеканилась и уходила `apply`, а он существующий объект перезаписывает.
# Теперь отказ — отказ шага с кодом 2, а заведение идёт АТОМАРНО на сервере
# (`create`): объект, появившийся между проверкой и заведением, отвечает
# AlreadyExists и переиспользуется. Держит
# deploy/tests/helm/seed-secrets-refuse-unknown-state-test.sh.
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

seed_verb='create secret'
# ОБРАЗЕЦ ПОИСКА ПО ЧУЖОМУ ФАЙЛУ, А НЕ ЗАВЕДЕНИЕ СЕКРЕТА, и глагол собран из
# двух частей именно поэтому. Гейт дисциплины ключевого материала
# (tests/helm/secret-material-survives-recreation-test.sh) разбирает КАЖДЫЙ
# файл этого каталога и читает дословный глагол создания как заведение
# секрета; записанный здесь целиком, он читался бы как заведение секрета с
# НЕЛИТЕРАЛЬНЫМ именем — то есть гейт краснел бы на собственном читателе.
# Разрыв виден глазом и ничего не обходит: имя секрета здесь не создаётся.
SEED_NAMES="$(grep -oE "$seed_verb generic [a-z0-9][a-z0-9-]*" "$SEED_SH" | awk '{print $4}' | sort -u)"
[ -n "$SEED_NAMES" ] || die "из посева $SEED_SH не прочитано ни одного имени секрета — судить (б) нечем" 2
REQUIRED="$(
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
# (б) ПОСЕВ: имя требуется, только если рендер на него ССЫЛАЕТСЯ (любой
# ссылкой, обязательной или нет) и служба читает его под посадкой ЭТОГО рендера.
# Полосность — одна строка на секрет, читаемый не всеми посадками; перенос
# требования стража старта службы (`Lanes: own` у второго фактора), а не выбор.
LANES = {"kaname-second-factor-enc-key": {"own"}}
referenced=set()
def scan_any(pod):
    for c in (pod.get("containers") or [])+(pod.get("initContainers") or []):
        for e in c.get("env") or []:
            r=(e.get("valueFrom") or {}).get("secretKeyRef")
            if r: referenced.add(r["name"])
        for ef in c.get("envFrom") or []:
            r=ef.get("secretRef")
            if r: referenced.add(r["name"])
    for v in pod.get("volumes") or []:
        s=v.get("secret")
        if s and s.get("secretName"): referenced.add(s["secretName"])
for d in docs:
    spec=d.get("spec") or {}
    if d.get("kind")=="Pod":
        scan_any(spec); continue
    tpl=spec.get("template") or ((spec.get("jobTemplate") or {}).get("spec") or {}).get("template")
    if tpl: scan_any(tpl.get("spec") or {})
posture=None
for d in docs:
    if d.get("kind")=="ConfigMap" and d["metadata"]["name"]=="kaname-config":
        cfg=yaml.safe_load((d.get("data") or {}).get("config.yaml") or "") or {}
        posture=(cfg.get("authn") or {}).get("identity-provider")
seed=[n for n in sys.argv[1].split() if n]
for n in seed:
    if n not in referenced or n in made: continue
    lanes=LANES.get(n)
    if lanes is not None:
        if posture is None:
            sys.stderr.write(f"посадка не прочитана из рендера (kaname-config, authn.identity-provider): требуемость {n}, читаемого только под {sorted(lanes)}, судить нечем\n")
            sys.exit(3)
        if posture not in lanes: continue
    need.add(n)
print("\n".join(sorted(n for n in need if n not in made)))
' "$SEED_NAMES" | sort -u | grep -v '^$'
)" || die "перечень требуемых секретов не выведен (причина выше)" 2

req_n="$(printf '%s\n' "$REQUIRED" | grep -c .)"
[ "$req_n" -gt 0 ] || die "требуемых секретов выведено НОЛЬ — обход слеп, а не стенд готов" 2

# secret_state <имя> — «present» | «absent» по ОТВЕТУ СЕРВЕРА. Любой иной отказ
# (срок, сеть, RBAC) — код 2 и текст сервера на stdout: «не знаю» не равно «нет».
secret_state() {
  local out
  if out="$(kubectl -n "$NS" get secret "$1" -o name 2>&1)"; then
    echo present; return 0
  fi
  case "$out" in *"(NotFound)"*) echo absent; return 0 ;; esac
  printf '%s' "$out"
  return 2
}

missing=""
for s in $REQUIRED; do
  st="$(secret_state "$s")" || die "есть ли секрет $s в ns $NS, НЕ УСТАНОВЛЕНО — сервер ответил не
       NotFound, а отказом: $st
       Прочитать это как «секрета нет» значило бы перечеканить величину поверх
       существующей. Выкатка не применена; повтори, когда кластер отвечает." 2
  [ "$st" = present ] || missing="$missing $s"
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
    *)                        echo "" ;;
  esac
}

# create_generic <имя> <ключ=значение>… — чеканит ОДИН раз; уже существующий
# объект не трогает вовсе.
create_generic() {
  local name="$1"; shift
  local st out
  st="$(secret_state "$name")" || { warn "есть ли секрет $name, не установлено — сервер ответил отказом: $st"; return 1; }
  if [ "$st" = present ]; then
    log "$name уже есть — переиспользуется (величина могла быть чем-то записана)"
    return 0
  fi
  # МАНИФЕСТОМ, ЗАВОДИМЫМ НА СЕРВЕРЕ (`create -f -`), А НЕ ГЛАГОЛОМ СОЗДАНИЯ И НЕ
  # `apply`. `apply` существующий объект ПЕРЕЗАПИСЫВАЕТ, `create` отвечает на
  # него AlreadyExists — и объект, появившийся между проверкой выше и этой
  # строкой, переиспользуется, а не теряет величину (задача #2803). Форма
  # манифеста, а не глагол, — по двум причинам, и обе несущие:
  #   • величины уезжают в `data:` УЖЕ в base64, поэтому ни ключ обёртки, ни
  #     строка bcrypt (`$2b$…`), ни строка соединения не проходят через разбор
  #     YAML и цитирование оболочки — а они несут знаки, на которых он ломается;
  #   • глагол `create secret generic <имя>` с именем-переменной гейт дисциплины
  #     ключевого материала читает как заведение секрета, о переиспользовании
  #     которого он ничего установить не может. Здесь переиспользование
  #     обеспечено ветвью выше, а не формой команды, и форма не должна выглядеть
  #     тем, чем не является.
  local kv
  {
    printf 'apiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\n  namespace: %s\ntype: Opaque\ndata:\n' \
      "$name" "$NS"
    for kv in "$@"; do
      printf '  %s: %s\n' "${kv%%=*}" "$(printf '%s' "${kv#*=}" | base64 -w0)"
    done
  } | { out="$(kubectl -n "$NS" create -f - 2>&1)"; rc=$?
        if [ "$rc" -ne 0 ]; then
          case "$out" in
            *"(AlreadyExists)"*) log "$name появился между проверкой и заведением — переиспользуется, величина не тронута"; exit 0 ;;
          esac
          warn "заведение $name отказало: $out"; exit 1
        fi
        log "заведён $name"; }
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
  st="$(secret_state "$s")" || die "есть ли секрет $s после посева, НЕ УСТАНОВЛЕНО — сервер ответил отказом: $st" 2
  [ "$st" = present ] || still="$still $s"
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
