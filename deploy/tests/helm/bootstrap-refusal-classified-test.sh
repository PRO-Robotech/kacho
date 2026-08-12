#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# УСТАНОВКА ПРАВ ОБЯЗАНА ОТЛИЧАТЬ НАСТРОЙКУ ОТ СБОЯ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Шаг первичной установки прав обращается к хранилищу прав и к API кластера.
# Отказ этих зависимостей бывает двух РАЗНЫХ природ, и различить их обязан сам
# шаг:
#
#   • НАСТРОЙКА — ответ, который повтором не исправится никогда: не тот
#     эндпоинт, не тот формат, отказ в правах, ссылка на несуществующую модель.
#     Такой ответ обязан быть громким и ронять установку;
#   • СБОЙ — зависимость сейчас не отвечает (сеть, таймаут, 5xx). Мягкий проход
#     здесь защитим, но обязан нести СЧЁТЧИК, иначе он невидим.
#
# Если шаг их не различает, постоянная неправильная настройка навсегда
# становится штатным режимом: шаг присутствует, исполняется на каждой
# установке — и не отказал НИ РАЗУ за всё время жизни.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ЭТО ПРОВЕРЯЕТСЯ ИСХОДОМ, А НЕ ПОИСКОМ ПО ТЕКСТУ
#
# Утверждение «шаг зовёт хранилище» зеленеет и тогда, когда ответ хранилища
# выброшен. Поэтому здесь исполняется НАСТОЯЩИЙ скрипт установки против
# подставного хранилища прав и подставного клиента кластера, а утверждается
# КОД ВОЗВРАТА и то, что напечатано. Ровно поэтому тело шага и вынесено из
# шаблона в файл: тело, доступное только через рендер чарта, проверяется лишь
# на глаз.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО УТВЕРЖДАЕТСЯ (объявление + семь исходов + перепись класса)
#
#   A0 задание объявляет телом шага этот файл (иначе A1..A7 не про поставку)
#   A1 отказ в правах на записи             → установка ПАДАЕТ, отказ назван
#   A2 не тот эндпоинт (404 на записи)      → установка ПАДАЕТ
#   A3 «такая связь уже есть» (400 dup)     → установка проходит, счётчик dup
#   A4 хранилище недоступно по сети         → мягкий проход, СЧЁТЧИК ненулевой
#   A5 итог печатается ВСЕГДА               → «ноль отказов» отличимо от
#                                             «шаг ни разу не исполнялся»
#   A6 отказ клиента кластера в патче       → установка ПАДАЕТ
#   A7 отказ перечисления хранилищ          → установка ПАДАЕТ, а НЕ «создаю
#                                             хранилище заново»
#   B  перепись по дереву: ни один вызов внешней зависимости на пути установки
#      не поглощает отказ терминально
#
# ─────────────────────────────────────────────────────────────────────────────
# ГРАНИЦА (названа, чтобы «зелено» не читалось шире, чем есть)
#
# Проверяется скрипт установки и синтаксис остальных тел на пути установки.
# Что произойдёт с уже поднятым кластером — предмет гейта посадки
# (scripts/assert-production-posture.sh), не этого файла.
#
# Самопроверка (`--self-test`) вносит поглощение обратно в КОПИЮ скрипта и
# требует красного, а рядом ставит ЗАКОННОГО БЛИЗНЕЦА той же формы (поглощение
# с разбором подставленного значения, как в задании доверенных издателей) и
# требует молчания.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_ROOT/.." && pwd)"
BOOTSTRAP_SH="$DEPLOY_ROOT/helm/umbrella/charts/openfga-bootstrap/files/bootstrap.sh"

rc=0
ok()   { echo "  ok   — $*"; }
bad()  { echo "  FAIL — $*"; rc=1; }
note() { echo "         $*"; }

for tool in python3 curl jq; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "FAIL: $SCRIPT: нет $tool — проверка не может исполниться, и это НЕ зелёный."
    exit 2
  }
done
[ -f "$BOOTSTRAP_SH" ] || { echo "FAIL: $SCRIPT: нет $BOOTSTRAP_SH"; exit 2; }

# ─────────────────────────────────────────────────────────────────────────────
# ПОДСТАВНОЕ ХРАНИЛИЩЕ ПРАВ
#
# Отвечает по сценарию, заданному переменной FAKE_MODE. Пишет полученные записи
# в журнал, чтобы «шаг не исполнялся» было отличимо от «исполнился и промолчал».
# ─────────────────────────────────────────────────────────────────────────────
fake_server_py() {
  cat <<'PY'
import json, os, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODE = os.environ.get("FAKE_MODE", "ok")
LOG = os.environ.get("FAKE_LOG", "/dev/null")
STORE = "01STORE0000000000000000000"
MODEL = "01MODEL0000000000000000000"

def log(line):
    with open(LOG, "a") as f:
        f.write(line + "\n")

class H(BaseHTTPRequestHandler):
    def _send(self, code, body):
        raw = body.encode()
        self.send_response(code)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, *a):
        pass

    def do_GET(self):
        if self.path == "/healthz":
            return self._send(200, '{"status":"SERVING"}')
        if self.path == "/stores":
            if MODE == "stores-5xx":
                return self._send(503, '{"code":"unavailable"}')
            return self._send(200, json.dumps({"stores": [{"id": STORE, "name": "kacho-store"}]}))
        if self.path.startswith("/stores/") and "/authorization-models/" in self.path:
            return self._send(200, json.dumps({"authorization_model": {"id": MODEL}}))
        return self._send(404, '{"code":"not_found"}')

    def do_POST(self):
        n = int(self.headers.get("content-length") or 0)
        body = self.rfile.read(n).decode()
        if self.path.endswith("/authorization-models"):
            log("MODEL-WRITE")
            return self._send(201, json.dumps({"authorization_model_id": MODEL}))
        if self.path.endswith("/write"):
            log("TUPLE-WRITE " + body.replace("\n", " ")[:120])
            if MODE == "write-403":
                return self._send(403, '{"code":"forbidden","message":"store write forbidden"}')
            if MODE == "write-404":
                return self._send(404, '<html>404 not found</html>')
            if MODE == "write-dup":
                return self._send(400, '{"code":"write_failed_due_to_invalid_input",'
                                       '"message":"cannot write a tuple which already exists"}')
            if MODE == "write-5xx":
                return self._send(503, '{"code":"unavailable"}')
            if MODE == "write-netfail":
                # Сеть: соединение рвётся, ОТВЕТА НЕТ вовсе (curl → 000).
                try:
                    self.connection.close()
                except OSError:
                    pass
                return
            return self._send(200, "{}")
        if self.path == "/stores":
            return self._send(201, json.dumps({"id": STORE}))
        return self._send(404, '{"code":"not_found"}')

srv = ThreadingHTTPServer(("127.0.0.1", 0), H)
print(srv.server_address[1], flush=True)
srv.serve_forever()
PY
}

# ─────────────────────────────────────────────────────────────────────────────
# ПОДСТАВНОЙ КЛИЕНТ КЛАСТЕРА
#
# Дублёр НЕ снисходительнее настоящего: он отказывает там, где отказал бы
# настоящий (KUBECTL_MODE=patch-denied), и хранит состояние секрета в файле,
# поэтому проверка на отметку прежней установки настоящая, а не всегда-пустая.
# ─────────────────────────────────────────────────────────────────────────────
make_kubectl_shim() {
  local dir="$1"
  cat >"$dir/kubectl" <<'SHIM'
#!/bin/sh
# Подставной клиент кластера. Состояние — файлы в $SHIM_STATE.
: "${SHIM_STATE:?}"
mode="${KUBECTL_MODE:-ok}"
args="$*"
case "$args" in
  *"get secret"*|*"get "*secret*)
    # Возврат отметки прежней установки: по умолчанию её нет.
    exit 1 ;;
  *"get deployment"*)
    [ "${KUBECTL_DEPLOYMENT_PRESENT:-1}" = "1" ] || exit 1
    echo "deployment.apps/x"; exit 0 ;;
  *"patch deployment"*)
    if [ "$mode" = "patch-denied" ]; then
      echo 'Error from server (Forbidden): deployments.apps "x" is forbidden' >&2
      exit 1
    fi
    echo "patched" >>"$SHIM_STATE/patched"; exit 0 ;;
  *"create secret"*|*"apply -f"*|*annotate*)
    echo "applied" >>"$SHIM_STATE/applied"; exit 0 ;;
esac
exit 0
SHIM
  chmod +x "$dir/kubectl"
}

start_fake() {
  local mode="$1" log="$2" outdir="$3"
  FAKE_MODE="$mode" FAKE_LOG="$log" python3 -c "$(fake_server_py)" >"$outdir/port" 2>"$outdir/srv.err" &
  echo $! >"$outdir/pid"
  local i=0
  while [ $i -lt 100 ]; do
    port="$(head -1 "$outdir/port" 2>/dev/null)"
    [ -n "${port:-}" ] && { echo "$port"; return 0; }
    sleep 0.05
    i=$((i + 1))
  done
  return 1
}

stop_fake() {
  local outdir="$1"
  [ -f "$outdir/pid" ] && kill "$(cat "$outdir/pid")" 2>/dev/null
  wait "$(cat "$outdir/pid" 2>/dev/null)" 2>/dev/null
  return 0
}

# run_case <скрипт> <режим-хранилища> <режим-клиента> → печатает вывод, ставит CASE_RC
run_case() {
  local script="$1" fmode="$2" kmode="${3:-ok}" port url
  CASE_DIR="$(mktemp -d)"
  mkdir -p "$CASE_DIR/bin" "$CASE_DIR/state" "$CASE_DIR/model"
  make_kubectl_shim "$CASE_DIR/bin"
  printf 'model dsl\n' >"$CASE_DIR/model/model.fga"
  printf '{"type_definitions":[]}\n' >"$CASE_DIR/model/model.json"
  : >"$CASE_DIR/tuples.log"

  if [ "$fmode" = "net-down" ]; then
    # Порт, на котором никто не слушает: сетевой отказ, а не ответ.
    port="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
  else
    port="$(start_fake "$fmode" "$CASE_DIR/tuples.log" "$CASE_DIR")" || return 2
  fi
  url="http://127.0.0.1:$port"

  CASE_OUT="$(
    PATH="$CASE_DIR/bin:$PATH" \
    SHIM_STATE="$CASE_DIR/state" \
    KUBECTL_MODE="$kmode" \
    OPENFGA_URL="$url" \
    MODEL_DIR="$CASE_DIR/model" \
    NAMESPACE=kacho \
    CONSUMER_DEPLOYMENTS="kacho-iam" \
    BOOTSTRAP_HEALTH_ATTEMPTS=2 \
    BOOTSTRAP_HEALTH_SLEEP=0 \
    BOOTSTRAP_WRITE_ATTEMPTS=2 \
    BOOTSTRAP_WRITE_SLEEP=0 \
    sh "$script" 2>&1
  )"
  CASE_RC=$?
  [ "$fmode" = "net-down" ] || stop_fake "$CASE_DIR"
  return 0
}

# ═════════════════════════════════════════════════════════════════════════════
# ЧАСТЬ A — ИСХОД УСТАНОВКИ
# ═════════════════════════════════════════════════════════════════════════════
part_a() {
  local script="${1:-$BOOTSTRAP_SH}"
  local a_rc=0
  local saved=$rc
  rc=0

  echo "── A0: задание ОБЪЯВЛЯЕТ телом шага именно этот файл"
  # Без этого часть A проверяла бы файл, который в задание не приезжает: тело
  # можно вернуть в шаблон обратно, и все семь исходов ниже остались бы зелёными
  # про артефакт, которого нет в поставке. Читается ОБЪЯВЛЕНИЕ, поэтому проверке
  # не нужен helm и она не умеет пропуститься.
  local job="$DEPLOY_ROOT/helm/umbrella/charts/openfga-bootstrap/templates/openfga-bootstrap-job.yaml"
  if grep -q 'Files.Get "files/bootstrap.sh"' "$job"; then
    ok "тело шага приезжает из files/bootstrap.sh"
  else
    bad "задание не объявляет files/bootstrap.sh телом шага — часть A проверяет не то, что поставляется"
  fi

  echo "── A1: отказ в правах на записи — установка обязана упасть"
  run_case "$script" write-403
  if [ "$CASE_RC" -ne 0 ]; then ok "код возврата $CASE_RC"; else bad "установка вышла успехом на отказе в правах"; fi
  if grep -qiE 'отказ|forbidden|403' <<<"$CASE_OUT"; then ok "отказ назван в выводе"; else bad "отказ не назван"; note "$(head -c 300 <<<"$CASE_OUT")"; fi

  echo "── A2: не тот эндпоинт (404 на записи) — установка обязана упасть"
  run_case "$script" write-404
  if [ "$CASE_RC" -ne 0 ]; then ok "код возврата $CASE_RC"; else bad "установка вышла успехом на 404 записи"; fi

  echo "── A3: связь уже существует — законный исход, установка проходит"
  run_case "$script" write-dup
  if [ "$CASE_RC" -eq 0 ]; then ok "код возврата 0"; else bad "установка упала на законном повторе (код $CASE_RC)"; note "$(head -c 300 <<<"$CASE_OUT")"; fi

  echo "── A4: запись не доехала по сети — мягкий проход, но со СЧЁТЧИКОМ"
  run_case "$script" write-netfail
  if [ "$CASE_RC" -eq 0 ]; then ok "мягкий проход сохранён"; else bad "сетевой отказ уронил установку (код $CASE_RC)"; fi
  if grep -qE 'soft_pass=[1-9]' <<<"$CASE_OUT"; then ok "счётчик мягких проходов ненулевой"; else bad "нет ненулевого счётчика soft_pass"; note "$(head -c 400 <<<"$CASE_OUT")"; fi

  echo "── A5: итог печатается ВСЕГДА — «ноль отказов» отличимо от «не исполнялся»"
  run_case "$script" ok
  if [ "$CASE_RC" -eq 0 ]; then ok "успешный проход"; else bad "успешный сценарий упал (код $CASE_RC)"; note "$(head -c 400 <<<"$CASE_OUT")"; fi
  if grep -qE 'writes_attempted=[0-9]+' <<<"$CASE_OUT" && grep -qE 'writes_ok=[0-9]+' <<<"$CASE_OUT"; then
    ok "итог назвал число попыток и успехов"
  else
    bad "итог не называет числа — «ноль отказов» неотличимо от «шаг не исполнялся»"
    note "$(head -c 400 <<<"$CASE_OUT")"
  fi
  if grep -qE 'writes_attempted=[1-9]' <<<"$CASE_OUT"; then ok "попытки записи действительно были"; else bad "записей не было вовсе"; fi

  echo "── A6: клиент кластера отказал в патче — установка обязана упасть"
  run_case "$script" ok patch-denied
  if [ "$CASE_RC" -ne 0 ]; then ok "код возврата $CASE_RC"; else bad "отказ в патче поглощён"; fi

  echo "── A7: перечисление хранилищ отказало — не создавать хранилище заново"
  run_case "$script" stores-5xx
  if [ "$CASE_RC" -ne 0 ]; then ok "код возврата $CASE_RC"; else bad "отказ перечисления прочитан как «хранилища нет»"; fi
  if grep -q 'store not found — creating' <<<"$CASE_OUT"; then bad "шаг пошёл создавать хранилище поверх отказа"; else ok "хранилище заново не создавалось"; fi

  a_rc=$rc
  rc=$saved
  return $a_rc
}

# ═════════════════════════════════════════════════════════════════════════════
# ЧАСТЬ B — ПЕРЕПИСЬ ПО ДЕРЕВУ
#
# Ищет ТЕРМИНАЛЬНОЕ поглощение отказа внешней зависимости в теле, исполняемом на
# пути установки. Терминальное — значит подставленное значение никому не
# достаётся: оно не присвоено переменной и не подставлено в команду, поэтому
# отличить «зависимость не отвечает» от «мы стучимся не туда» после него уже
# нечем.
#
# ЗАКОННЫЙ БЛИЗНЕЦ ТОЙ ЖЕ ФОРМЫ, который обязан молчать: поглощение, чьё
# подставленное значение РАЗБИРАЕТСЯ (`code=$(curl … || echo "000")` и следом
# `case`). Он есть в дереве — задание доверенных издателей, — поэтому предикат
# проверяется не только внесённым дефектом, но и живым законным случаем.
# ═════════════════════════════════════════════════════════════════════════════
part_b() {
  local root="${1:-$REPO_ROOT}"
  python3 - "$root" <<'PY'
import os, re, sys

root = sys.argv[1]
EXTERNAL = re.compile(r'(?<![\w-])(curl|wget|kubectl|psql|pg_isready|fga|mc|nc)\b')
ABSORB   = re.compile(r'\|\|\s*(echo|true|:)(\s|$|")')
# Присваивание / подстановка: значение достаётся кому-то дальше → это НЕ терминал.
CAPTURED = re.compile(r'(\w+=|\$\(|`)')

def install_path(rel):
    if rel.startswith('deploy/tests/') or rel.startswith('deploy/load-tests/'):
        return False
    if rel.startswith('deploy/helm/') and (rel.endswith('.yaml') or rel.endswith('.sh')):
        return True
    if rel.startswith('deploy/scripts/') and rel.endswith('.sh'):
        return True
    return False

files = lines = 0
findings = []
for dirpath, dirnames, filenames in os.walk(root):
    dirnames[:] = [d for d in dirnames if d not in ('.git', 'node_modules', 'vendor')]
    for fn in filenames:
        p = os.path.join(dirpath, fn)
        rel = os.path.relpath(p, root)
        if not install_path(rel):
            continue
        try:
            text = open(p, encoding='utf-8').read()
        except (OSError, UnicodeDecodeError):
            continue
        src = text.split('\n')
        files += 1
        lines += len(src)
        for i, ln in enumerate(src):
            if ln.lstrip().startswith('#'):
                continue
            if not ABSORB.search(ln):
                continue
            # Логический стейтмент: вверх по продолжениям.
            j = i
            while j > 0 and (src[j - 1].rstrip().endswith('\\') or src[j].lstrip().startswith(('"', '\\"', '}', '-', '|'))):
                j -= 1
            stmt = '\n'.join(src[j:i + 1])
            if not EXTERNAL.search(stmt):
                continue
            head = src[j].lstrip()
            if CAPTURED.match(head) or CAPTURED.search(head.split('||')[0]):
                continue          # значение кому-то достаётся → разбор возможен
            # Диагностика перед безусловным выходом — не поглощение.
            nxt = ' '.join(src[i + 1:i + 3])
            if re.search(r'\bexit\s+[1-9]', nxt):
                continue
            findings.append((rel, j + 1, i + 1, stmt.strip().split('\n')[0][:110]))

print(f"  ОСМОТРЕНО: файлов {files}, строк {lines}")
if files == 0 or lines == 0:
    print("  FAIL: осмотрено ноль — «ноль находок» здесь ничего не значит.")
    sys.exit(1)
print(f"  НАХОДОК: {len(findings)}")
for rel, a, b, first in findings:
    print(f"  FAIL — {rel}:{a}-{b}")
    print(f"         {first}")
sys.exit(1 if findings else 0)
PY
}

# ═════════════════════════════════════════════════════════════════════════════
if [ "${1:-}" != "--self-test" ]; then
  echo "=== $SCRIPT: часть A — исход установки ==="
  part_a || rc=1
  echo "=== $SCRIPT: часть B — перепись класса по дереву ==="
  part_b || rc=1
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT" || echo "FAILED: $SCRIPT"
  exit $rc
fi

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА — инъекция в обе стороны.
# ─────────────────────────────────────────────────────────────────────────────
echo "=== $SCRIPT: self-test ==="
st=0

# (а) ВНЕСЁННЫЙ ДЕФЕКТ: возвращаем поглощение отказа записи в копию скрипта.
inj="$(mktemp -d)"
cp "$BOOTSTRAP_SH" "$inj/bootstrap.sh"
python3 - "$inj/bootstrap.sh" <<'PY'
import re, sys
p = sys.argv[1]
src = open(p, encoding='utf-8').read()
# Заменяем классифицирующую запись на поглощающую — ту самую форму, что была.
needle = re.search(r'^fga_write\(\).*?^}\n', src, re.S | re.M)
assert needle, "не найдена функция записи — самопроверка не может внести дефект"
swallow = (
    'fga_write() {\n'
    '  _label="$1"; _payload="$2"\n'
    '  WRITES_ATTEMPTED=$((WRITES_ATTEMPTED + 1))\n'
    '  curl -sf -XPOST "${OPENFGA_URL}/stores/${STORE_ID}/write" \\\n'
    '    -H \'content-type: application/json\' \\\n'
    '    -d "${_payload}" >/dev/null 2>&1 || echo "[bootstrap] ${_label} already exists (OK)"\n'
    '}\n'
)
open(p, 'w', encoding='utf-8').write(src[:needle.start()] + swallow + src[needle.end():])
PY
echo "-- (а) внесён дефект: отказ записи поглощён"
if part_a "$inj/bootstrap.sh" >/dev/null 2>&1; then
  echo "  FAIL: на внесённом поглощении часть A осталась ЗЕЛЁНОЙ"
  st=1
else
  echo "  ok: часть A краснеет на внесённом поглощении"
fi

# (б) ЗАКОННЫЙ БЛИЗНЕЦ: поглощение, чьё значение разбирается. Он ЖИВОЙ в дереве
#     (задание доверенных издателей), плюс синтетическая копия той же формы.
twin="$(mktemp -d)/tree"
mkdir -p "$twin/deploy/helm/umbrella/templates"
cat >"$twin/deploy/helm/umbrella/templates/legit-job.yaml" <<'TWIN'
              code=$(curl -s -o /tmp/r.json -w '%{http_code}' -X POST "$URL/x" || echo "000")
              case "$code" in
                2*) echo ok ;;
                409) echo dup ;;
                *) echo "FAIL HTTP $code"; exit 1 ;;
              esac
TWIN
echo "-- (б) законный близнец: поглощение с разбором подставленного значения"
if part_b "$twin" >/dev/null 2>&1; then
  echo "  ok: перепись молчит на законной форме"
else
  echo "  FAIL: перепись покраснела на законной конструкции — она ловит форму, не существо"
  part_b "$twin"
  st=1
fi

# (в) ВНЕСЁННЫЙ ДЕФЕКТ ДЛЯ ПЕРЕПИСИ: то же тело, но без разбора.
bad="$(mktemp -d)/tree"
mkdir -p "$bad/deploy/helm/umbrella/templates"
cat >"$bad/deploy/helm/umbrella/templates/bad-job.yaml" <<'BAD'
              curl -sf -XPOST "$URL/write" -d '{}' >/dev/null 2>&1 || echo "already exists (OK)"
BAD
echo "-- (в) внесён дефект: терминальное поглощение"
if part_b "$bad" >/dev/null 2>&1; then
  echo "  FAIL: перепись НЕ увидела терминального поглощения"
  st=1
else
  echo "  ok: перепись краснеет и называет координату"
  part_b "$bad" 2>&1 | grep -E 'FAIL —|ОСМОТРЕНО' | sed 's/^/     /'
fi

[ $st -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
exit $st
