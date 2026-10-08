#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# installation-mail-dns-test.sh — сверка опубликованных записей домена
# отправителя установки (deploy/scripts/installation-mail-dns.sh, kacho#3017)
# способна покраснеть и не краснеет на согласных записях.
#
# ЧТО УТВЕРЖДАЕТСЯ — исходами настоящего скрипта на настоящей цепочке профиля
# a8f60d (значения вычисляет helm), с подставными `kubectl` (объект ключа DKIM
# отдаёт пару, выпущенную здесь же) и `dig` (ответы DNS — файлы каталога):
#
#   1. записи равны порождённым — код 0, у каждой из трёх проверок «ok»;
#   2. DKIM несёт открытый ключ ДРУГОЙ пары — код 1, причина «(N, E)»;
#   3. под селектором две записи — код 1;
#   4. SPF разрешает всё (`?all`) — код 1, «не равна строке профиля»;
#   5. записи DMARC нет — код 1, «записей 0»;
#   6. DNS не ответил по одной записи — код 2 (исход не установлен), а не 1;
#   7. профиль стенда — код 1, отказ называет признак почтовой полосы;
#   8. records печатает запись DKIM, чей ключ равен открытому ключу объекта, и
#      строки SPF/DMARC профиля.
#
# Кластер и сеть не нужны.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$(basename "$0")"
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=8

require_helm
require_python_yaml
require_umbrella_charts "$DEPLOY_ROOT/helm/umbrella"
command -v openssl >/dev/null 2>&1 || fatal "нет openssl — пару ключа выпускать нечем"

TARGET="$DEPLOY_ROOT/scripts/installation-mail-dns.sh"
require_file_present "$TARGET" "скрипт записей установки"
TABLE="$DEPLOY_ROOT/stacks-mail-dns.txt"
SPF="$(grep '^a8f60d|spf|' "$TABLE" | cut -d'|' -f3-)"
DMARC="$(grep '^a8f60d|dmarc|' "$TABLE" | cut -d'|' -f3-)"
[ -n "$SPF" ] && [ -n "$DMARC" ] || fatal "в $TABLE нет строк spf/dmarc профиля a8f60d — вход проверки исчез"

W="$(mktemp -d "${TMPDIR:-/tmp}/installation-mail-dns-test.XXXXXX")"
trap 'rm -rf "$W"' EXIT
mkdir -p "$W/bin" "$W/dns"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$W/obj.key" 2>/dev/null || fatal "пара объекта не выпущена"
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$W/other.key" 2>/dev/null || fatal "вторая пара не выпущена"
pub() { openssl pkey -in "$1" -pubout -outform DER 2>/dev/null | base64 -w0; }
OBJ_P="$(pub "$W/obj.key")"
OTHER_P="$(pub "$W/other.key")"
SEL=notify20261008
DOM=prorobotech.ru

# Подставной kubectl: объект ключа DKIM и адрес сервера API.
cat > "$W/bin/kubectl" <<EOF
#!/usr/bin/env bash
case "\$*" in
  *"get secret kacho-notify-dkim"*) printf '%s %s' "$(base64 -w0 < "$W/obj.key")" "$(printf %s "$SEL" | base64 -w0)" ;;
  "config view"*) printf 'https://stub.invalid:6443' ;;
  *) echo "подставной kubectl: вызов не предусмотрен: \$*" >&2; exit 1 ;;
esac
EOF
# Подставной dig: ответ — файл каталога по имени запроса; файл SILENT — тайм-аут.
cat > "$W/bin/dig" <<EOF
#!/usr/bin/env bash
name=""; for a in "\$@"; do case "\$a" in *.) name="\$a" ;; esac; done
f="$W/dns/\$name"
if [ -f "$W/dns/SILENT" ] && [ "\$(cat "$W/dns/SILENT")" = "\$name" ]; then
  echo ";; communications error to 192.0.2.1#53: timed out"; exit 9
fi
[ -f "\$f" ] && cat "\$f"
exit 0
EOF
chmod +x "$W/bin/kubectl" "$W/bin/dig"

publish() { # publish <dkim p> <spf> <dmarc> — пустое значение: записи нет
  rm -f "$W/dns/"*
  [ -z "$1" ] || printf '"v=DKIM1; k=rsa; " "p=%s"\n' "$1" > "$W/dns/$SEL._domainkey.$DOM."
  [ -z "$2" ] || printf '"%s"\n"google-site-verification=x"\n' "$2" > "$W/dns/$DOM."
  [ -z "$3" ] || printf '"%s"\n' "$3" > "$W/dns/_dmarc.$DOM."
}
run() { # run <профиль> <действие> → RC, OUT
  OUT="$(PATH="$W/bin:$PATH" bash "$TARGET" "$1" "$2" 2>&1)"
  RC=$?
}
expect() { # expect <случай> <код> <подстрока>
  if [ "$RC" != "$2" ] || ! grep -qF -- "$3" <<<"$OUT"; then
    printf '%s\n' "$OUT" | sed 's/^/    /' >&2
    fail "$1: код $RC (ожидался $2), «$3» в выводе: $(grep -cF -- "$3" <<<"$OUT")"
  fi
  echo "  ✓ $1"
  ok
}

publish "$OBJ_P" "$SPF" "$DMARC"
run a8f60d verify
[ "$RC" = 2 ] && grep -q 'отказала\|COMPUTED' <<<"$OUT" && fatal "значения цепочки a8f60d не вычислены: $OUT"
expect "согласные записи" 0 "все три записи домена $DOM опубликованы и согласны"

publish "$OTHER_P" "$SPF" "$DMARC"; run a8f60d verify
expect "DKIM чужой пары" 1 "(N, E) записи не равны"

publish "$OBJ_P" "$SPF" "$DMARC"; printf '"v=DKIM1; p=%s"\n' "$OTHER_P" >> "$W/dns/$SEL._domainkey.$DOM."; run a8f60d verify
expect "две записи под селектором" 1 "записей под селектором 2"

publish "$OBJ_P" "${SPF/-all/?all}" "$DMARC"; run a8f60d verify
expect "SPF разрешает всё" 1 "не равна строке профиля «$SPF»"

publish "$OBJ_P" "$SPF" ""; run a8f60d verify
expect "DMARC нет" 1 "записей 0, ожидалась одна"

publish "$OBJ_P" "$SPF" "$DMARC"; printf '%s' "_dmarc.$DOM." > "$W/dns/SILENT"; run a8f60d verify
expect "DNS не ответил" 2 "ответа нет — исход не установлен"

run dev verify
expect "профиль стенда" 1 "признак почтовой полосы «stand»"

run a8f60d records
expect "records: DKIM из объекта" 0 "$SEL._domainkey.$DOM."$'\t'"TXT"$'\t'"\"v=DKIM1; k=rsa; p=$OBJ_P\""
grep -qF "\"$SPF\"" <<<"$OUT" && grep -qF "\"$DMARC\"" <<<"$OUT" \
  || fail "records: строки SPF/DMARC профиля не напечатаны"

outcome_verdict
