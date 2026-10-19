#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# own-front-san-pin-inject.sh — доказательство инъекцией для адреса собственного
# фронта по имени из SAN (kacho#3315): настоящий newman против настоящего
# TLS-слушателя на петле, лист которого несёт в SAN только DNS-имя Service.
#
#   A. прежняя форма (https://127.0.0.1:<порт>, УЦ подан) — newman `errored`:
#      запрос не состоялся, вердикта нет;
#   B. законный близнец (https://<имя из SAN>:<порт>, имя запинено на петлю
#      lib/pin-resolve.cjs, тот же УЦ) — вердикт: запрос состоялся, утверждение
#      исполнено;
#   C. проверка цепочки ВКЛЮЧЕНА: тот же близнец без УЦ — снова `errored`;
#   D. пин уводит имя лишь на петлю: пин на иной адрес — отказ при загрузке;
#   E. own-front-tls-name.py выводит имя и УЦ из листов посадки; имени в SAN
#      нет, листов два, ca.crt нет — исход 3 («условие не создано»).
#
# Решение прогонщиков (newman-e2e.sh, newman-parallel.sh) об адресе на каждой
# посадке судит assert-own-front-leaf-decision.sh, а не эта проба.
#
# Нужны bash, openssl, python3, node и newman. Кластер и сеть не нужны.
# Исходы: 0 — все утверждения сошлись; 1 — иначе.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
srv=""
cleanup() { [ -z "$srv" ] || kill "$srv" 2>/dev/null; rm -rf "$tmp"; }
trap cleanup EXIT
pass=0 fail=0
ok()  { pass=$((pass + 1)); printf '  ok     %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf '  ПРОВАЛ %s\n' "$1"; }

for t in openssl python3 node newman; do
  command -v "$t" >/dev/null 2>&1 || { echo "не выполнилось: нет $t" >&2; exit 2; }
done

name="kaname.tprobe.svc"
d="$tmp/pki"; mkdir -p "$d"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 1 \
  -subj /CN=probe-ca -keyout "$d/ca.key" -out "$d/ca.crt" >/dev/null 2>&1 &&
mk_leaf() { # ИМЯ SAN
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -subj /CN=leaf \
    -keyout "$d/$1.key" -out "$d/$1.csr" >/dev/null 2>&1 &&
  openssl x509 -req -in "$d/$1.csr" -CA "$d/ca.crt" -CAkey "$d/ca.key" -CAcreateserial -days 1 \
    -extfile <(printf 'subjectAltName=%s\n' "$2") -out "$d/$1.crt" >/dev/null 2>&1
}
mk_leaf front "DNS:$name.cluster.local,DNS:$name,DNS:kaname.tprobe" &&
mk_leaf other "DNS:other.tprobe.svc" || { echo "не выполнилось: листы не выпущены" >&2; exit 2; }

# ── слушатель на петле: TLS листом фронта, на любой GET — 200 и JSON ─────────────
cat >"$tmp/srv.py" <<'PY'
import http.server, ssl, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        b = b'{"ok":true}'
        self.send_response(200); self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(b))); self.end_headers(); self.wfile.write(b)
    def log_message(self, *a): pass
s = http.server.HTTPServer(("127.0.0.1", 0), H)
c = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER); c.load_cert_chain(sys.argv[1], sys.argv[2])
s.socket = c.wrap_socket(s.socket, server_side=True)
open(sys.argv[3], "w").write(str(s.server_address[1]))
s.serve_forever()
PY
python3 "$tmp/srv.py" "$d/front.crt" "$d/front.key" "$tmp/port" & srv=$!
for _ in $(seq 50); do [ -s "$tmp/port" ] && break; sleep 0.1; done
port="$(cat "$tmp/port" 2>/dev/null)"
[ -n "$port" ] || { echo "не выполнилось: слушатель не поднялся" >&2; exit 2; }

cat >"$tmp/col.json" <<'J'
{"info":{"name":"own-front-probe","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
 "item":[{"name":"front","request":{"method":"GET","url":"{{ownRestBaseUrl}}/probe"},
  "event":[{"listen":"test","script":{"exec":["pm.test('status 200', function () { pm.response.to.have.status(200); });"]}}]}]}
J

# run_newman ИМЯ URL [ЕЩЁ АРГУМЕНТЫ…] — newman с отчётом cli (отчёт json на
# ошибке рукопожатия падает сам: сертификат в ошибке зациклен); печатает
# «<запросов errored> <утверждений> <упавших утверждений>» по строке сводки.
run_newman() {
  local tag="$1" url="$2"; shift 2
  newman run "$tmp/col.json" --env-var "ownRestBaseUrl=$url" --reporters cli --color off \
    "$@" >"$tmp/$tag.out" 2>&1
  local errored row
  errored="$(grep -c '\[errored\]' "$tmp/$tag.out")"
  row="$(grep -E '│ +assertions +│' "$tmp/$tag.out" | tr -d ' ' | awk -F'│' '{ print $3, $4 }')"
  printf '%s %s\n' "$errored" "${row:-нет-сводки нет-сводки}"
}

echo "A. прежняя форма: адрес на петлю по IP — newman errored, вердикта нет"
got="$(run_newman ip "https://127.0.0.1:$port" --ssl-extra-ca-certs "$d/ca.crt")"
if [ "${got%% *}" = 1 ] && grep -q "does not match certificate's altnames" "$tmp/ip.out"; then
  ok "errored: запрос не состоялся по имени сертификата («$(grep -o "Hostname/IP does not match certificate's altnames" "$tmp/ip.out" | head -1)»)"
else
  bad "ждали errored по имени сертификата, получили «$got»"; sed 's/^/      /' "$tmp/ip.out" | tail -5
fi

echo "B. законный близнец: адрес от имени из SAN, имя запинено на петлю — вердикт"
pin_env=(env "KACHO_PIN_RESOLVE=$name=127.0.0.1" "NODE_OPTIONS=--require \"$HERE/lib/pin-resolve.cjs\"")
got="$("${pin_env[@]}" bash -c "$(declare -f run_newman); tmp='$tmp'; run_newman pin 'https://$name:$port' --ssl-extra-ca-certs '$d/ca.crt'")"
if [ "$got" = "0 1 0" ]; then
  ok "запрос состоялся, утверждение исполнено и сошлось"
else
  bad "ждали «0 1 0», получили «$got»"; sed 's/^/      /' "$tmp/pin.out" | tail -5
fi

echo "C. проверка цепочки включена: тот же близнец без УЦ посадки — errored"
got="$("${pin_env[@]}" bash -c "$(declare -f run_newman); tmp='$tmp'; run_newman noca 'https://$name:$port'")"
if [ "${got%% *}" = 1 ] && ! grep -q "altnames" "$tmp/noca.out"; then
  ok "без УЦ запрос не состоялся (цепочка, а не имя) — пин проверку не снимает"
else
  bad "ждали errored по цепочке, получили «$got»"; sed 's/^/      /' "$tmp/noca.out" | tail -5
fi

echo "D. пин уводит имя лишь на петлю"
if KACHO_PIN_RESOLVE="$name=10.0.0.1" node --require "$HERE/lib/pin-resolve.cjs" -e 0 >"$tmp/d.out" 2>&1; then
  bad "пин на 10.0.0.1 принят"
else
  grep -q 'допустима лишь петля' "$tmp/d.out" && ok "пин на иной адрес — отказ при загрузке" || bad "отказ без причины: $(head -2 "$tmp/d.out")"
fi
if KACHO_PIN_RESOLVE="$name=127.0.0.1" node --require "$HERE/lib/pin-resolve.cjs" \
     -e "require('dns').lookup('$name', (e, a) => { if (e || a !== '127.0.0.1') process.exit(1); })" 2>/dev/null; then
  ok "близнец: пин на петлю — имя разрешается в 127.0.0.1"
else
  bad "пин на петлю не разрешил имя"
fi

echo "E. own-front-tls-name.py: имя и УЦ — из листов посадки"
b64() { base64 -w0 <"$1"; }
secret() { printf '{"metadata":{"name":"%s"},"type":"kubernetes.io/tls","data":{"tls.crt":"%s"%s}}' "$1" "$(b64 "$2")" "${3:+,\"ca.crt\":\"$(b64 "$3")\"}"; }
mkdir -p "$tmp/bin"
cat >"$tmp/bin/kubectl" <<'K'
#!/usr/bin/env bash
cat "$PROBE_SECRETS"
K
chmod +x "$tmp/bin/kubectl"
name_of() { # ФАЙЛ-СЕКРЕТОВ → «код|имя»
  local n rc
  n="$(PROBE_SECRETS="$1" KUBECTL="$tmp/bin/kubectl" python3 "$HERE/own-front-tls-name.py" kaname \
        --namespace tprobe --ca-out "$tmp/ca.out" 2>"$tmp/e.err")"; rc=$?
  printf '%s|%s' "$rc" "$n"
}
printf '{"items":[%s,%s]}' "$(secret kaname-server-tls "$d/front.crt" "$d/ca.crt")" "$(secret other "$d/other.crt" "$d/ca.crt")" >"$tmp/s1.json"
got="$(name_of "$tmp/s1.json")"
[ "$got" = "0|$name" ] && cmp -s "$tmp/ca.out" "$d/ca.crt" && ok "имя $name из SAN, УЦ — ca.crt того же секрета" || bad "получили «$got»"
printf '{"items":[%s]}' "$(secret other "$d/other.crt" "$d/ca.crt")" >"$tmp/s2.json"
got="$(name_of "$tmp/s2.json")"
[ "$got" = "3|" ] && grep -q 'не называет Service' "$tmp/e.err" && ok "имени в SAN нет — исход 3 с причиной" || bad "получили «$got»"
printf '{"items":[%s,%s]}' "$(secret a "$d/front.crt" "$d/ca.crt")" "$(secret b "$d/front.crt" "$d/ca.crt")" >"$tmp/s3.json"
got="$(name_of "$tmp/s3.json")"
[ "$got" = "3|" ] && grep -q 'неоднозначно' "$tmp/e.err" && ok "два листа о Service — исход 3, неоднозначно" || bad "получили «$got»"
printf '{"items":[%s]}' "$(secret kaname-server-tls "$d/front.crt")" >"$tmp/s4.json"
got="$(name_of "$tmp/s4.json")"
[ "$got" = "3|" ] && grep -q 'нет ca.crt' "$tmp/e.err" && ok "у листа нет ca.crt — исход 3" || bad "получили «$got»"

echo
echo "итог: утверждений $((pass + fail)) — сошлось $pass, провалено $fail"
[ "$fail" -eq 0 ]
