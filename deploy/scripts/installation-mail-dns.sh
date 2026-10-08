#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# installation-mail-dns.sh — объект ключа DKIM и записи DKIM, SPF, DMARC домена
# отправителя установки с внешним ретранслятором (kacho#3017; NTF-1 Р19).
#
#   installation-mail-dns.sh <профиль> seed    [доп. аргументы helm…]
#   installation-mail-dns.sh <профиль> records [доп. аргументы helm…]
#   installation-mail-dns.sh <профиль> verify  [доп. аргументы helm…]
#
# ЗАЧЕМ. Страж DNS notify на старте читает три записи домена `From` — DKIM под
# селектором объекта ключа, SPF и DMARC — и без них в готовность не выходит. У
# стенда записям отвечает зона стенда (seed-stand-dkim.sh). У установки с
# внешним ретранслятором (признак `relay` в deploy/stacks-mail.txt) записи
# публикуются во ВНЕШНЕМ DNS домена — у регистратора, рукой его владельца, — и
# дерево может только породить их и сверить опубликованное. Это и делает скрипт;
# публикацию он не делает и сделать не может.
#
#   seed    — объект ключа DKIM в пространстве установки: пара RSA 2048 и
#             селектор выпускаются ТОЛЬКО если объекта нет; существующий не
#             трогается (смена ключа — новым селектором, порядок —
#             deploy/README.md, раздел о почте установки).
#   records — три записи для публикации: DKIM выводится из открытого ключа и
#             селектора объекта ключа, SPF и DMARC — строки профиля в
#             deploy/stacks-mail-dns.txt. Закрытый ключ из кластера читается во
#             временный каталог и удаляется сразу после вывода открытого.
#   verify  — опубликованное против порождённого: по каждой записи один
#             ответ DNS, DKIM — тем же открытым ключом (N, E), что у объекта,
#             SPF и DMARC — равны строкам профиля. Печатает дату замера и
#             ответы — это артефакт DoD задачи.
#
# ОТКУДА ВЕЛИЧИНЫ — ИЗ ДЕЙСТВУЮЩИХ ЗНАЧЕНИЙ ЦЕПОЧКИ, А НЕ ВТОРЫМ НАПИСАНИЕМ.
# Домен `From`, объект ключа DKIM (`global.kacho.identity.smtp.dkim`) и домен
# возврата берутся из значений, которые helm вычисляет для цепочки профиля
# (deploy/stacks.txt), — раздел COMPUTED VALUES пробной установки без кластера.
# Рендер notify здесь не годится: пока записей нет, notify в рендере установки
# нет (Д102), а записи нужны ДО его возврата.
#
# Резолвер verify — системный; KACHO_MAIL_DNS_RESOLVER=<адрес> задаёт другой
# (например, публичный, чтобы не читать кэш локального).
#
# Коды: 0 — сделано и согласно, 1 — отказ либо расхождение (названо), 2 —
# условие не создано (нет инструмента, рендер не прошёл, кластер или DNS не
# ответили).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
NS="${KACHO_NAMESPACE:-${STACK_NAMESPACE:-kacho}}"
RELEASE="${STACK_RELEASE:-kacho-umbrella}"
MAIL_TABLE="$DEPLOY_ROOT/stacks-mail.txt"
RECORDS_TABLE="$DEPLOY_ROOT/stacks-mail-dns.txt"

die() { printf 'ABORT: installation-mail-dns — %s\n' "$1" >&2; exit "${2:-1}"; }
log() { printf '=== installation-mail-dns: %s\n' "$1"; }

PROFILE="${1:-}"
ACTION="${2:-}"
[ -n "$PROFILE" ] || die "профиль не назван: умолчания нет — скрипт читает и пишет кластер активного контекста" 2
case "$ACTION" in
  seed|records|verify) ;;
  *) die "действие «$ACTION» не из seed|records|verify" 2 ;;
esac
shift 2

for t in helm kubectl python3 openssl base64; do
  command -v "$t" >/dev/null 2>&1 || die "нет '$t' — условие прогона, а не находка" 2
done
[ "$ACTION" != verify ] || command -v dig >/dev/null 2>&1 || die "нет 'dig' — опрашивать DNS нечем" 2
python3 -c 'import yaml' 2>/dev/null || die "нет PyYAML — значения цепочки разобрать нечем" 2

# ── 1. Профиль — с внешним ретранслятором ────────────────────────────────────
LANE="$(awk -F: -v p="$PROFILE" '$0 !~ /^[[:space:]]*#/ && $1 == p { print $2 }' "$MAIL_TABLE")"
[ "$LANE" = relay ] \
  || die "профиль $PROFILE: признак почтовой полосы «${LANE:-строки нет}» ($MAIL_TABLE). Записи этой цели — только
       у признака relay: стенду отвечает зона стенда (seed-stand-dkim.sh), у operator записи объявляет оператор" 1

# ── 2. Строки профиля в таблице записей: ровно одна spf и одна dmarc ─────────
declared() { # declared <вид>
  local n
  n="$(grep -c "^$PROFILE|$1|" "$RECORDS_TABLE" || true)"
  [ "$n" = 1 ] || die "профиль $PROFILE: строк $1 в $RECORDS_TABLE — $n, ожидалась одна" 1
  grep "^$PROFILE|$1|" "$RECORDS_TABLE" | cut -d'|' -f3-
}
SPF="$(declared spf)"
DMARC="$(declared dmarc)"

# ── 3. Действующие значения цепочки ──────────────────────────────────────────
# shellcheck source=../tests/helm/stacks.sh
. "$DEPLOY_ROOT/tests/helm/stacks.sh"
ARGS="$(stacks_args "$PROFILE" "$UMBRELLA")" || die "цепочка '$PROFILE' не прочиталась" 2
[ -n "$ARGS" ] || die "цепочка '$PROFILE' прочиталась ПУСТОЙ" 2
# shellcheck disable=SC2086
OUT="$(helm install "$RELEASE" "$UMBRELLA" -n "$NS" $ARGS "$@" --dry-run=client --debug 2>&1)" || {
  printf '%s\n' "$OUT" | tail -20 >&2
  die "пробная установка цепочки '$PROFILE' отказала (текст выше) — значения вычислить не из чего" 2
}
DECL="$(printf '%s\n' "$OUT" | python3 -c '
import sys, yaml
lines = sys.stdin.read().split("\n")
try:
    i = lines.index("COMPUTED VALUES:")
except ValueError:
    sys.exit("в выводе helm нет раздела COMPUTED VALUES")
j = next((k for k in range(i + 1, len(lines)) if lines[k] in ("HOOKS:", "MANIFEST:")), None)
if j is None:
    sys.exit("раздел COMPUTED VALUES не закрыт разделом HOOKS или MANIFEST")
v = yaml.safe_load("\n".join(lines[i + 1:j])) or {}
def g(*path):
    cur = v
    for k in path:
        if not isinstance(cur, dict) or k not in cur:
            return ""
        cur = cur[k]
    return "" if cur is None else str(cur).strip()
addr = g("global", "kacho", "identity", "smtp", "fromAddress")
dom = addr.rpartition("@")[2].rstrip(".").lower() if "@" in addr else ""
dk = ("global", "kacho", "identity", "smtp", "dkim")
fields = [
    ("global.kacho.identity.smtp.fromAddress (домен)", dom),
    ("global.kacho.identity.smtp.dkim.secretName", g(*dk, "secretName")),
    ("global.kacho.identity.smtp.dkim.privateKeyKey", g(*dk, "privateKeyKey")),
    ("global.kacho.identity.smtp.dkim.selectorKey", g(*dk, "selectorKey")),
    ("notify.returnDomain", g("notify", "returnDomain").rstrip(".").lower()),
]
missing = [n for n, val in fields if not val or any(c.isspace() for c in val)]
if missing:
    sys.exit("не объявлено либо не одно слово: " + ", ".join(missing))
print(" ".join(val for _, val in fields))
')" || die "значения цепочки '$PROFILE' не дают записей (причина выше)" 1
read -r DOMAIN DKIM_NAMED KEY_KEY SEL_KEY RETURN_DOMAIN <<<"$DECL"

# Имя объекта ключа — литерал, а не выведенная величина: проверка дерева
# (tests/helm/secret-material-survives-recreation-test.sh) судит, что заведение
# секрета стоит в ветке переиспользования ЭТОГО ЖЕ имени. Профиль, назвавший
# другой объект, — отказ: notify ждал бы объекта, которого цель не заводит.
DKIM_SECRET=kacho-notify-dkim
[ "$DKIM_NAMED" = "$DKIM_SECRET" ] \
  || die "профиль $PROFILE называет объект ключа DKIM «$DKIM_NAMED», а цель заводит $DKIM_SECRET
       (global.kacho.identity.smtp.dkim.secretName)" 1
# Кластер называется адресом его сервера API, а не именем контекста: имя —
# локальный ярлык kubeconfig, и одноимённый контекст уже вёл в другой кластер.
API="$(kubectl config view --minify -o 'jsonpath={.clusters[0].cluster.server}' 2>/dev/null || true)"
log "профиль $PROFILE: домен $DOMAIN · возврат $RETURN_DOMAIN · объект ключа $NS/$DKIM_SECRET ($KEY_KEY, $SEL_KEY) · сервер API ${API:-не задан}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# ── seed: объект ключа — выпуск только при отсутствии ────────────────────────
if [ "$ACTION" = seed ]; then
  kubectl get namespace "$NS" >/dev/null 2>&1 \
    || die "пространства '$NS' нет либо кластер не отвечает — объект ключа заводится в пространстве установки" 2
  if out="$(kubectl -n "$NS" get secret kacho-notify-dkim -o name 2>&1)"; then
    log "$DKIM_SECRET уже есть — переиспользуется, пара не трогается"
  elif [[ "$out" == *"(NotFound)"* ]]; then
    openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$WORK/new.key" 2>/dev/null \
      || die "пара RSA 2048 не выпущена" 1
    # Селектор — метка RFC 6376 §3.1 с датой выпуска: новая пара — новый селектор.
    printf 'notify%s' "$(date -u +%Y%m%d)" > "$WORK/new.sel"
    if ! out="$(kubectl -n "$NS" create secret generic kacho-notify-dkim \
          --from-file="$KEY_KEY=$WORK/new.key" --from-file="$SEL_KEY=$WORK/new.sel" 2>&1)"; then
      case "$out" in
        *"(AlreadyExists)"*) log "$DKIM_SECRET появился между проверкой и заведением — переиспользуется" ;;
        *) die "заведение $DKIM_SECRET отказало: $out" 1 ;;
      esac
    else
      log "$DKIM_SECRET выпущен: RSA 2048, селектор $(cat "$WORK/new.sel")"
    fi
    rm -f "$WORK/new.key"
  else
    die "есть ли объект $DKIM_SECRET, НЕ УСТАНОВЛЕНО — сервер ответил не NotFound, а отказом: $out" 2
  fi
  exit 0
fi

# ── Открытый ключ и селектор объекта (records, verify) ───────────────────────
# Выход 2 — ответа нет; выход 1 — объект есть и вне формы.
object_key() {
  local out
  if ! out="$(kubectl -n "$NS" get secret "$DKIM_SECRET" -o "go-template={{index .data \"$KEY_KEY\"}} {{index .data \"$SEL_KEY\"}}" 2>&1)"; then
    printf 'объект %s/%s не прочитан: %s\n' "$NS" "$DKIM_SECRET" "$out" >&2
    return 2
  fi
  local k s
  read -r k s <<<"$out"
  { [ -n "$k" ] && [ "$k" != "<no value>" ] && [ -n "$s" ] && [ "$s" != "<no value>" ]; } \
    || { printf 'в объекте %s нет ключа %s или %s\n' "$DKIM_SECRET" "$KEY_KEY" "$SEL_KEY" >&2; return 1; }
  printf '%s' "$k" | base64 -d > "$WORK/obj.key" || return 1
  SELECTOR="$(printf '%s' "$s" | base64 -d | tr -d '[:space:]')"
  [[ "$SELECTOR" =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$ ]] \
    || { printf 'селектор объекта %s вне формы метки RFC 6376 §3.1\n' "$DKIM_SECRET" >&2; rm -f "$WORK/obj.key"; return 1; }
  if ! openssl pkey -in "$WORK/obj.key" -pubout -outform DER -out "$WORK/obj.pub.der" 2>/dev/null; then
    rm -f "$WORK/obj.key"
    printf 'закрытый ключ объекта %s не разбирается\n' "$DKIM_SECRET" >&2
    return 1
  fi
  rm -f "$WORK/obj.key"
  P_VALUE="$(base64 -w0 < "$WORK/obj.pub.der")"
}
ne() { openssl rsa -pubin -inform DER -in "$1" -noout -modulus -text 2>/dev/null | grep -E '^(Modulus=|Exponent:)'; }

SELECTOR="" P_VALUE=""
key_rc=0
object_key || key_rc=$?

if [ "$ACTION" = records ]; then
  [ "$key_rc" = 0 ] || die "объекта ключа DKIM нет либо он вне формы — сначала: $0 $PROFILE seed" "$key_rc"
  DKIM_TXT="v=DKIM1; k=rsa; p=$P_VALUE"
  log "записи домена $DOMAIN для публикации во внешнем DNS (TTL — на усмотрение владельца домена)"
  printf '%s\tTXT\t"%s"\n' "${SELECTOR}._domainkey.${DOMAIN}." "$DKIM_TXT" "${DOMAIN}." "$SPF" "_dmarc.${DOMAIN}." "$DMARC"
  # Значение длиннее 255 октетов в одной строке TXT невыразимо (RFC 1035
  # §3.3.14): интерфейс, который не режет сам, принимает его частями одной записи.
  printf '\nDKIM частями одной записи TXT (если интерфейс регистратора не делит сам):\n'
  s="$DKIM_TXT"
  while [ -n "$s" ]; do printf '"%s" ' "${s:0:250}"; s="${s:250}"; done
  printf '\n'
  exit 0
fi

# ── verify: опубликованное против порождённого ────────────────────────────────
RESOLVER="${KACHO_MAIL_DNS_RESOLVER:-}"
log "замер $(date -u +%Y-%m-%dT%H:%M:%SZ), резолвер ${RESOLVER:-системный}; NS домена: $(dig +short NS "${DOMAIN}." ${RESOLVER:+@"$RESOLVER"} 2>/dev/null | tr '\n' ' ')"
red=0 silent=0

# txt <имя> — записи TXT имени, одна на строку (строки одной записи склеены).
# Выход 2 — ответа нет (тайм-аут, отказ сервера).
txt() {
  local out rc=0
  out="$(dig +short +time=5 +tries=2 TXT "$1" ${RESOLVER:+@"$RESOLVER"} 2>&1)" || rc=$?
  if [ "$rc" != 0 ] || grep -q '^;;' <<<"$out"; then
    printf '%s\n' "$out" | sed 's/^/      /' >&2
    return 2
  fi
  printf '%s\n' "$out" | python3 -c '
import re, sys
for line in sys.stdin:
    parts = re.findall(r"\"((?:[^\"\\\\]|\\\\.)*)\"", line)
    if parts:
        print(re.sub(r"\\\\(.)", r"\1", "".join(parts)))
'
}
norm_tags() { python3 -c 'import sys; print("; ".join(p.strip() for p in sys.argv[1].split(";") if p.strip()))' "$1"; }
verdict() { # verdict <check> <ok|red|silent> <текст>
  printf '  %-6s %-8s %s\n' "$1" "$2" "$3"
  case "$2" in red) red=1 ;; silent) silent=1 ;; esac
}

# DKIM
if [ "$key_rc" != 0 ]; then
  verdict dkim silent "объект ключа не прочитан — сверять открытый ключ не с чем"
else
  name="${SELECTOR}._domainkey.${DOMAIN}."
  if ! got="$(txt "$name")"; then
    verdict dkim silent "$name: ответа нет"
  else
    n="$(grep -c . <<<"$got" || true)"
    printf '    %s → %s\n' "$name" "${got:-(записи нет)}"
    if [ "$n" != 1 ]; then
      verdict dkim red "записей под селектором $n, ожидалась одна"
    else
      p="$(python3 -c 'import sys
t = dict((k.strip(), v.strip()) for k, _, v in (x.partition("=") for x in sys.argv[1].split(";")) if k.strip())
print("".join(t.get("p", "").split()))' "$got")"
      if [ -z "$p" ] || ! printf '%s' "$p" | base64 -d > "$WORK/dns.pub.der" 2>/dev/null; then
        verdict dkim red "тег p= пуст либо не base64"
      elif [ -n "$(ne "$WORK/obj.pub.der")" ] && [ "$(ne "$WORK/obj.pub.der")" = "$(ne "$WORK/dns.pub.der")" ]; then
        verdict dkim ok "(N, E) записи равны открытому ключу $NS/$DKIM_SECRET, селектор $SELECTOR"
      else
        verdict dkim red "(N, E) записи не равны открытому ключу $NS/$DKIM_SECRET"
      fi
    fi
  fi
fi

# SPF и DMARC — равенство строке профиля
for check in spf dmarc; do
  if [ "$check" = spf ]; then name="${DOMAIN}." want="$SPF"; else name="_dmarc.${DOMAIN}." want="$DMARC"; fi
  if ! got="$(txt "$name")"; then
    verdict "$check" silent "$name: ответа нет"
    continue
  fi
  if [ "$check" = spf ]; then
    recs="$(grep -iE '^v=spf1( |$)' <<<"$got" || true)"
    a="$(tr -s ' ' <<<"$recs")" b="$(tr -s ' ' <<<"$want")"
  else
    recs="$(grep -E '^[[:space:]]*v[[:space:]]*=[[:space:]]*DMARC1[[:space:]]*(;|$)' <<<"$got" || true)"
    a="$(norm_tags "$recs")" b="$(norm_tags "$want")"
  fi
  printf '    %s → %s\n' "$name" "${recs:-(записи нет)}"
  n="$(grep -c . <<<"$recs" || true)"
  if [ "$n" != 1 ]; then
    verdict "$check" red "записей $n, ожидалась одна"
  elif [ "$a" = "$b" ]; then
    verdict "$check" ok "равна строке профиля ($RECORDS_TABLE)"
  else
    verdict "$check" red "не равна строке профиля «$want»"
  fi
done

if [ "$red" = 1 ]; then
  die "записи домена $DOMAIN расходятся с порождёнными (строки выше)" 1
fi
[ "$silent" = 0 ] || die "по части записей ответа нет — исход не установлен" 2
log "все три записи домена $DOMAIN опубликованы и согласны"
