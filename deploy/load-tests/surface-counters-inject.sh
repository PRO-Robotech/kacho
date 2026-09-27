#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# surface-counters-inject.sh — доказательство инъекцией для съёма величин
# приборами нагрузки (lib/surface-counters.sh), БЕЗ стенда: kubectl подменяется
# заглушкой, которая знает объявления сбора подов и транспорт их слушателей.
#
# Что доказывается (задача #2171):
#   1. адрес берётся из объявления сбора ЭТОГО пода — порт И схема: слушатель под
#      TLS с объявленным `https` читается, выписанный адрес на нём не читался бы;
#   2. объявление снято при слушателе под TLS → величины НЕ прочитаны, и это
#      ЧИСЛО (`surfaces_read=0/1`) с причиной, а не пустая строка;
#   3. снимок пула на непрочитанной поверхности печатает причину и перепись, а не
#      НИЧЕГО (прежде `|| true` глушил его целиком);
#   4. законный близнец (обе поверхности читаются) — суммы по всем подам.
#
# Исходы: 0 — все оси доказаны; 1 — ось не доказана (находка о приборе).
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ST="$(mktemp -d)"
trap 'rm -rf "$ST"' EXIT
mkdir -p "$ST/bin"

# Заглушка kubectl. Поды iam-a (10.0.0.1) и iam-b (10.0.0.2); у iam-b слушатель
# под TLS (STUB_TLS_B=1). Объявление схемы iam-b задаёт STUB_SCHEME_B.
cat >"$ST/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
args=" $* "
case "$args" in
  *" run "*|*" wait "*|*" delete "*) exit 0 ;;
esac
if [[ "$args" == *" exec "* ]]; then
  url=""; for a in "$@"; do case "$a" in http://*|https://*) url="$a" ;; esac; done
  ip="${url#*://}"; ip="${ip%%:*}"
  want=http; [ "$ip" = 10.0.0.2 ] && [ "${STUB_TLS_B:-0}" = 1 ] && want=https
  if [[ "$url" != "$want://"* ]]; then printf '\n000\n'; exit 0; fi
  case "$ip" in
    10.0.0.1) printf 'kaname_authz_check_duration_seconds_count 10\nkaname_authz_check_duration_seconds_sum 0.5\nkaname_db_pool_acquired 3\n200\n' ;;
    10.0.0.2) printf 'kaname_authz_check_duration_seconds_count 5\nkaname_authz_check_duration_seconds_sum 0.25\nkaname_db_pool_acquired 4\n200\n' ;;
  esac
  exit 0
fi
case "$args" in
  *" get pod iam-a "*) echo "iam-a|10.0.0.1|9095||" ;;
  *" get pod iam-b "*) echo "iam-b|10.0.0.2|9095|${STUB_SCHEME_B:-}|" ;;
esac
exit 0
STUB
chmod +x "$ST/bin/kubectl"

# shellcheck source=deploy/load-tests/lib/surface-counters.sh
. "$HERE/lib/surface-counters.sh"
PATH="$ST/bin:$PATH"
scrape_probe_start kacho >/dev/null || { echo "FAIL: заглушечный пробник не поднялся"; exit 1; }

rc=0; n=0
expect() {
  n=$((n + 1))
  if [[ "$2" == *"$3"* ]]; then echo "  ✓ $1"; else echo "  ✗ $1 — нет «$3» в: $2"; rc=1; fi
}
SPEC=(checks='^kaname_authz_check_duration_seconds_count' sum='^kaname_authz_check_duration_seconds_sum:%.6f')

echo "== съём величин нагрузки: адрес из объявления сбора пода =="
out="$(STUB_TLS_B=1 STUB_SCHEME_B=https counters kacho "iam-a iam-b" "${SPEC[@]}")"
expect "iam-b под TLS, объявлено https → прочитаны обе, суммы по подам" "$out" "checks=15 sum=0.750000 surfaces_read=2/2"
out="$(STUB_TLS_B=1 counters kacho "iam-a iam-b" "${SPEC[@]}")"
expect "объявление схемы снято при TLS → суммы NA, но ЧИСЛО прочитанного" "$out" "checks=NA sum=NA surfaces_read=1/2"
expect "…и причина названа с адресом" "$out" "iam-b: ответ 000 по http://10.0.0.2:9095/metrics"
out="$(STUB_TLS_B=1 series_lines kacho "iam-a iam-b" '^kaname_db_pool_')"
expect "снимок пула: прочитанный под — строки рядов" "$out" "iam-a kaname_db_pool_acquired 3"
expect "снимок пула: непрочитанный — причина, а не тишина" "$out" "# величины НЕ ПРОЧИТАНЫ — iam-b"
expect "снимок пула: перепись" "$out" "# поверхностей прочитано 1 из 2"
out="$(STUB_TLS_B=1 STUB_SCHEME_B=ftp counters kacho "iam-b" "${SPEC[@]}")"
expect "схема вне пары http|https → отказ с причиной" "$out" "называет схему 'ftp'"

echo "случаев исполнено: $n"
[ "$n" -eq 7 ] || { echo "FAIL: исполнено $n из 7"; rc=1; }
[ "$rc" -eq 0 ] && echo "PASS: surface-counters-inject.sh" || echo "FAIL: surface-counters-inject.sh"
exit "$rc"
