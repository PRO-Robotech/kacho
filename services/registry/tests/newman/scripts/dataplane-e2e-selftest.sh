#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/dataplane-e2e-selftest.sh — самопроверка ЧЕСТНОСТИ вердикта
# dataplane-e2e.sh.
#
# Предмет: единственный сквозной прогон плоскости данных не имеет права превращать
# «не выполнилось» в «прошло». Отказ на инициализации загрузки снимал с прогона
# загрузку блоба, манифест, скачивание, область блоба и список тегов — то есть
# отказ в начале делал суиту зелёной, а не красной.
#
# Гоняет НАСТОЯЩИЙ dataplane-e2e.sh против заглушки плоскости данных (python3
# http.server, ниже в этом же файле) и утверждает КОД ВОЗВРАТА:
#
#   happy           — весь поток отвечает как исправный стенд → 0  (КОНТРОЛЬ: гейт
#                     молчит на законной форме, а не «всегда красный»);
#   push404         — инициализация загрузки отвечает 404         → НЕ 0;
#   helm-no-session — инициализация загрузки артефакта без сессии → НЕ 0;
#   no-token        — выдача токена не отдаёт токен               → НЕ 0.
#
# Стенд не нужен: заглушка поднимается на свободном порту петлевого интерфейса.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HARNESS="$HERE/dataplane-e2e.sh"

command -v python3 >/dev/null 2>&1 || { echo "SELFTEST SKIP: нет python3"; exit 0; }
command -v curl    >/dev/null 2>&1 || { echo "SELFTEST SKIP: нет curl";    exit 0; }
[[ -f "$HARNESS" ]] || { echo "SELFTEST FAIL: нет $HARNESS"; exit 1; }

TMP="$(mktemp -d "${TMPDIR:-/tmp}/reg-dpe2e-selftest.XXXXXX")"
STUB_PID=""
cleanup() {
  [[ -n "$STUB_PID" ]] && kill "$STUB_PID" 2>/dev/null
  rm -rf "$TMP"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Заглушка плоскости данных + плоскости управления. STUB_MODE управляет ровно
# одним ответом — всё остальное отвечает как исправный стенд.
# ---------------------------------------------------------------------------
cat > "$TMP/stub.py" <<'PY'
import http.server, json, os

MODE = os.environ.get("STUB_MODE", "happy")
MANIFEST = json.dumps({"schemaVersion": 2, "layers": []}).encode()

# Плоскость управления: ListRepositories по обоим репозиториям, которые харнесс
# заводит push'ем (docker + артефакт), с классификацией и back-compat-полями.
def repositories(run):
    return json.dumps({"repositories": [
        {"name": "e2e-app-%s" % run, "artifact_type": "ARTIFACT_TYPE_CONTAINER_IMAGE",
         "tag_count": 1, "size_bytes": 80, "updated_at": "2026-07-30T00:00:00Z"},
        {"name": "e2e-app-%s-helm" % run, "artifact_type": "ARTIFACT_TYPE_HELM_CHART",
         "tag_count": 1, "size_bytes": 60, "updated_at": "2026-07-30T00:00:00Z"},
    ]}).encode()


class H(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):  # тишина: вывод заглушки не должен мешать вердикту
        pass

    def _drain(self):
        n = int(self.headers.get("Content-Length") or 0)
        if n:
            self.rfile.read(n)

    def _send(self, code, body=b"", headers=None):
        self.send_response(code)
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def _authed(self):
        return (self.headers.get("Authorization") or "").startswith("Bearer ")

    def _route(self):
        p, m = self.path, self.command

        # обход по закодированному разделителю — отвергается до всякой авторизации
        if "%2f" in p.lower():
            return self._send(400, b'{"errors":[{"code":"NAME_INVALID"}]}')

        if p.startswith("/iam/token"):
            if MODE == "no-token":
                return self._send(200, b'{"expires_in":300}')
            return self._send(200, b'{"token":"stub-identity-jwt"}')

        # плоскость управления (cross-check шага 10)
        if m == "GET" and "/registry/v1/registries/" in p and p.endswith("/repositories"):
            if not self._authed():
                return self._send(401, b'{"code":16}')
            return self._send(200, repositories(os.environ.get("STUB_RUN", "selftest")))

        if p == "/v2/":
            if not self._authed():
                return self._send(401, b'{"errors":[{"code":"UNAUTHORIZED"}]}',
                                  {"WWW-Authenticate": 'Bearer realm="http://stub/token",service="registry"'})
            return self._send(200, b"{}")

        if not self._authed():
            return self._send(401, b'{"errors":[{"code":"UNAUTHORIZED"}]}',
                              {"WWW-Authenticate": 'Bearer realm="http://stub/token",service="registry"'})

        if m == "POST" and p.endswith("/blobs/uploads/"):
            is_helm = "-helm/" in p
            if MODE == "push404" and not is_helm:
                return self._send(404, b'{"errors":[{"code":"NAME_UNKNOWN"}]}')
            if MODE == "helm-no-session" and is_helm:
                return self._send(500, b'{"errors":[{"code":"UNKNOWN"}]}')
            return self._send(202, b"", {"Location": p + "stub-session-id"})

        if m == "PUT" and "/blobs/uploads/" in p:
            return self._send(201, b"", {"Docker-Content-Digest": "sha256:stub"})

        if m == "PUT" and "/manifests/" in p:
            return self._send(201, b"", {"Docker-Content-Digest": "sha256:stub"})

        if m == "DELETE" and "/manifests/" in p:
            return self._send(405, b'{"errors":[{"code":"UNSUPPORTED"}]}')

        if m == "GET" and "/manifests/" in p:
            return self._send(200, MANIFEST,
                              {"Content-Type": "application/vnd.oci.image.manifest.v1+json"})

        if m == "GET" and "/blobs/sha256:" in p:
            return self._send(200, b"{}")

        if m == "GET" and p.endswith("/tags/list"):
            return self._send(200, json.dumps({"name": "stub", "tags": ["v1"]}).encode())

        return self._send(404, b'{"errors":[{"code":"NAME_UNKNOWN"}]}')

    def do_GET(self):
        self._drain(); self._route()

    def do_POST(self):
        self._drain(); self._route()

    def do_PUT(self):
        self._drain(); self._route()

    def do_DELETE(self):
        self._drain(); self._route()


srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
print(srv.server_address[1], flush=True)
srv.serve_forever()
PY

PASS=0
FAIL=0

# run_case MODE EXPECT{zero|nonzero} LABEL
run_case() {
  local mode="$1" expect="$2" label="$3"
  local out="$TMP/${mode}.out"

  STUB_MODE="$mode" STUB_RUN="selftest" python3 "$TMP/stub.py" \
    > "$TMP/port.$mode" 2>"$TMP/stub.$mode.err" &
  STUB_PID=$!
  local port="" i
  for i in $(seq 1 60); do
    port="$(head -n1 "$TMP/port.$mode" 2>/dev/null)"
    [[ -n "$port" ]] && break
    sleep 0.1
  done
  if [[ -z "$port" ]]; then
    echo "SELFTEST FAIL [$label] — заглушка не поднялась"
    FAIL=$((FAIL + 1)); kill "$STUB_PID" 2>/dev/null; STUB_PID=""
    return
  fi

  RUN="selftest" \
  REG_TOKEN_URL="http://127.0.0.1:${port}" \
  DATAPLANE_URL="http://127.0.0.1:${port}" \
  GATEWAY_URL="http://127.0.0.1:${port}" \
  ADMIN_JWT="stub-admin-jwt" \
  CREDENTIAL_ID="soc_selftest000000001" \
  CREDENTIAL_SECRET="kacho_soc_selftest000000001_0000000000000000000000000000000000" \
  REGISTRY_ID="regselftest000000000" \
    bash "$HARNESS" > "$out" 2>&1
  local rc=$?

  kill "$STUB_PID" 2>/dev/null; wait "$STUB_PID" 2>/dev/null; STUB_PID=""

  local ok=0
  if [[ "$expect" == "zero" && "$rc" -eq 0 ]]; then ok=1; fi
  if [[ "$expect" == "nonzero" && "$rc" -ne 0 ]]; then ok=1; fi

  # Необязательный 4-й аргумент — регулярка, которую вывод ОБЯЗАН содержать. Код
  # возврата — не весь вердикт: сводка обязана называть числа, иначе «ноль находок»
  # неотличимо от «ноль прочитанного».
  if [[ "$ok" == 1 && -n "${4:-}" ]]; then
    if ! grep -qE "$4" "$out"; then
      echo "FAIL [$label] — exit=$rc верен, но сводка не содержит /$4/; вывод харнесса:"
      sed 's/^/    | /' "$out"
      FAIL=$((FAIL + 1))
      return
    fi
  fi

  if [[ "$ok" == 1 ]]; then
    echo "PASS [$label] — exit=$rc (ожидалось $expect)"
    PASS=$((PASS + 1))
  else
    echo "FAIL [$label] — exit=$rc (ожидалось $expect); вывод харнесса:"
    sed 's/^/    | /' "$out"
    FAIL=$((FAIL + 1))
  fi
}

echo "=== dataplane-e2e.sh — самопроверка вердикта ==="

# КОНТРОЛЬ: законный поток проходит. Без него гейт нельзя отличить от «всегда красный»
# — а такой гейт отключат при первом же ложном срабатывании.
# Сводка обязана называть числа И не произносить слово PASS, пока есть непроверенный
# инвариант: «ноль находок» должно быть отличимо от «ноль прочитанного».
run_case happy           zero    "исправный поток → PASS + сводка в числах" \
  'summary: hard-pass=[1-9][0-9]*  hard-fail=0  NOT-RUN=0  UNVERIFIED=1'
run_case happy           zero    "исправный поток: вердикт при долге не называется PASS" \
  'RESULT: GREEN-WITH-DEBT'


# ПРЕДМЕТ: отказ на инициализации загрузки снимал с прогона пять шагов и оставлял зелёный.
run_case push404         nonzero "инициализация загрузки 404 → прогон КРАСНЫЙ (шаги сняты, значит не проверены)" \
  'NOT-RUN=[1-9]'


# Тот же класс на ветке артефакта: нет сессии → шаги классификации не исполняются.
run_case helm-no-session nonzero "инициализация загрузки артефакта без сессии → прогон КРАСНЫЙ"

# Тот же класс на выдаче токена: без токена сняты все шаги, требующие личности.
run_case no-token        nonzero "выдача не отдала токен → прогон КРАСНЫЙ"


# ── страж вида удостоверения (#1143) ────────────────────────────────────────
#
# Полоса отвергает негодный вид ТЕМ ЖЕ 401, что и неверный секрет, — снаружи это
# неотличимо, и это правильно. Значит различить «настроен по-старому» обязан
# харнесс, у которого строка на руках. Проба ПАРНАЯ: строка без марки — отказ с
# именем переменной; строка с маркой — молчание (её проходимость утверждают
# случаи выше).
_guard_case() {
  local label="$1" secret="$2" expect="$3"
  local out="$TMP/guard.out"
  RUN="selftest" \
  REG_TOKEN_URL="http://127.0.0.1:1" \
  DATAPLANE_URL="http://127.0.0.1:1" \
  GATEWAY_URL="http://127.0.0.1:1" \
  ADMIN_JWT="stub-admin-jwt" \
  CREDENTIAL_ID="soc_selftest000000001" \
  CREDENTIAL_SECRET="$secret" \
  REGISTRY_ID="regselftest000000000" \
    bash "$HARNESS" > "$out" 2>&1
  local rc=$?
  if [[ "$expect" == "refused" ]]; then
    if [[ "$rc" -eq 2 ]] && grep -q "CREDENTIAL_SECRET" "$out"; then
      echo "PASS [$label] — exit=2 и отказ называет переменную"; PASS=$((PASS + 1))
    else
      echo "FAIL [$label] — exit=$rc, вывод:"; sed 's/^/    | /' "$out"; FAIL=$((FAIL + 1))
    fi
  else
    # Законный близнец: страж вида молчит, и прогон уходит дальше — до сети,
    # которой в этой пробе нет. Отказ ДРУГОЙ природы (не 2) и есть доказательство
    # того, что страж не сработал вхолостую.
    if [[ "$rc" -ne 2 ]]; then
      echo "PASS [$label] — страж пропустил годную марку (exit=$rc)"; PASS=$((PASS + 1))
    else
      echo "FAIL [$label] — страж отверг годную марку; вывод:"; sed 's/^/    | /' "$out"; FAIL=$((FAIL + 1))
    fi
  fi
}

_guard_case "ключевой материал в поле пароля отвергается харнессом" \
  "-----BEGIN PRIVATE KEY-----" refused
_guard_case "строка с маркой стражем вида пропускается" \
  "kacho_soc_selftest000000001_0000000000000000000000000000000000" passes

echo
echo "=== итог самопроверки: pass=${PASS} fail=${FAIL} ==="
[[ "$FAIL" -eq 0 ]] || exit 1
exit 0
