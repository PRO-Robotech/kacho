#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# identity-callback-credential-source-test.sh — величина учётных данных обратного
# вызова обязана иметь ИСТОЧНИК в поде (задача #948).
#
# ЧТО ЛОВИТ. Конфигурация объявляла учётные данные ссылкой `${ИМЯ}`, читатель
# подстановки в значениях не делает, переменной с этим именем в поде не было — и
# литерал уезжал в заголовок дословно. Служба прав отвечала `401`, край переводил
# это арендатору в `502`; вход паролем не проходил, церемония вставала на пятой
# стадии, девять коллекций оставались без отчёта.
#
# Дефект прожил незамеченным потому, что выглядел обеспеченным: один профиль
# объявлял ссылку и НИ ОДНОГО источника, другой — ту же ссылку и переменную по
# пути ключа, которая её перекрывала. Прозой это было записано верно, в
# соседнем файле, и не роняло ничего.
#
# ПОЧЕМУ РЕНДЕР, А НЕ ОБЪЯВЛЕНИЯ. Предмет — то, что достанется ПРОЦЕССУ. Ссылка,
# перекрытая переменной, и ссылка, не перекрытая ничем, в значениях выглядят
# одинаково; различает их только под. Соседний гейт транспорта
# (deploy/identity_callback_transport_test.go) судит ОБЪЯВЛЕНИЯ и потому видит и
# те полосы, которые никто не монтирует; этот судит ПОДЫ и потому видит источник
# величины. Оси разные, и обе нужны.
#
# Разбор живёт отдельным файлом (identity-callback-credential-source.py): его
# можно проверить, ничего не поднимая, — что самопроверка ниже и делает.
#
# Самопроверка: --self-test (гейт обязан краснеть на внесённом дефекте и молчать
# на законной конструкции той же формы). Обычный прогон исполняет её ПЕРВОЙ:
# разбор работает над уже готовым текстом, стоит миллисекунды, и откладывать
# доказательство способности упасть было бы нечем оправдать.
set -uo pipefail
. "$(dirname "$0")/stacks.sh"

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
CHECKER="$HERE/identity-callback-credential-source.py"

# Три исхода — ОДНОЙ реализацией на весь каталог. Здесь они были СВОИ (`fail` и
# `refuse`), то есть вторым местом об одном предмете: коды совпадали, а «что
# сказал helm» терялось, и «стек не рендерится» уходило кодом 1 — тем же, каким
# объявляется полоса без источника величины (задача #1214).
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
require_helm

command -v python3 >/dev/null || fatal "нужен python3 (разбор рендера)"
python3 -c 'import yaml' 2>/dev/null || fatal "нужен PyYAML (python3 -c 'import yaml')"
[ -r "$CHECKER" ] || fatal "разбор $CHECKER не читается"

# ── самопроверка ────────────────────────────────────────────────────────────
# Вход синтезируется на уровне МАНИФЕСТА, а не значений: страж чарта
# (templates/identity-callback-credential-guard.yaml) часть этих форм не даёт
# даже отрендерить, и самопроверка, зависящая от него, проверяла бы его, а не
# этот разбор.
self_test() {
  local rc=0 tmp out code
  tmp="$(mktemp -d)"

  # Основа: под-читатель, конфигурация картой настроек, полоса с источником.
  cat > "$tmp/base.yaml" <<'YEOF'
apiVersion: v1
kind: ConfigMap
metadata: { name: cfg }
data:
  app.yaml: |
    oauth2:
      token_hook:
        url: "http://iam-internal.kacho.svc:9092/iam/v1/hooks/token"
        auth:
          type: api_key
          config: { in: header, name: X-Kacho-Hook-Token, value: "" }
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: reader }
spec:
  template:
    spec:
      volumes:
        - name: c
          configMap: { name: cfg }
      containers:
        - name: srv
          args: ["serve", "--config", "/etc/x/app.yaml"]
          env:
            - name: OAUTH2_TOKEN_HOOK_AUTH_CONFIG_VALUE
              valueFrom:
                secretKeyRef: { name: hook-token, key: token }
          volumeMounts:
            - { name: c, mountPath: /etc/x }
YEOF

  run() { out="$(python3 "$CHECKER" < "$1" 2>&1)"; code=$?; }

  # (A) законный близнец: дерево-образец как есть — обязан МОЛЧАТЬ
  run "$tmp/base.yaml"
  if [ "$code" = 0 ]; then echo "  (A) образец с источником               → МОЛЧИТ"
  else echo "  (A) образец с источником               → ЛОЖНОЕ СРАБАТЫВАНИЕ ($code): $out"; rc=1; fi

  # (B) ИНЪЕКЦИЯ: ссылка вернулась в значение — обязан КРАСНЕТЬ
  sed 's/value: ""/value: "${KANAME_HOOK_TOKEN}"/' "$tmp/base.yaml" > "$tmp/ref.yaml"
  run "$tmp/ref.yaml"
  if [ "$code" = 1 ] && [[ "$out" == *'несёт ссылку ${KANAME_HOOK_TOKEN}'* ]]; then
    echo "  (B) ссылка в значении конфигурации     → КРАСНЫЙ с координатой"
  else echo "  (B) ссылка в значении конфигурации     → ПРОПУСТИЛ ($code): $out"; rc=1; fi

  # (C) ИНЪЕКЦИЯ: источник снят — обязан КРАСНЕТЬ
  python3 - "$tmp/base.yaml" "$tmp/noenv.yaml" <<'PEOF'
import sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src).read()
i = text.index("          env:")
j = text.index("          volumeMounts:")
open(dst, "w").write(text[:i] + text[j:])
PEOF
  run "$tmp/noenv.yaml"
  if [ "$code" = 1 ] && [[ "$out" == *'OAUTH2_TOKEN_HOOK_AUTH_CONFIG_VALUE в контейнере нет'* ]]; then
    echo "  (C) переменной-источника нет           → КРАСНЫЙ с именем ручки"
  else echo "  (C) переменной-источника нет           → ПРОПУСТИЛ ($code): $out"; rc=1; fi

  # (D) ИНЪЕКЦИЯ: источник есть, но открытым текстом — обязан КРАСНЕТЬ
  python3 - "$tmp/base.yaml" "$tmp/plain.yaml" <<'PEOF'
import sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src).read()
text = text.replace(
    """              valueFrom:
                secretKeyRef: { name: hook-token, key: token }
""",
    """              value: "s3cret-in-the-render"
""",
)
open(dst, "w").write(text)
PEOF
  run "$tmp/plain.yaml"
  if [ "$code" = 1 ] && [[ "$out" == *'НЕ из secretKeyRef'* ]]; then
    echo "  (D) величина открытым текстом          → КРАСНЫЙ"
  else echo "  (D) величина открытым текстом          → ПРОПУСТИЛ ($code): $out"; rc=1; fi

  # (E) ИНЪЕКЦИЯ: полоса внутри массива, картой настроек — источника не бывает.
  #     Это форма конфигурации службы личности; обязан КРАСНЕТЬ.
  python3 - "$tmp/arr.yaml" <<'PEOF'
import sys
open(sys.argv[1], "w").write("""apiVersion: v1
kind: ConfigMap
metadata: { name: cfg }
data:
  app.yaml: |
    selfservice:
      flows:
        login:
          after:
            password:
              hooks:
                - hook: web_hook
                  config:
                    url: "http://iam-internal.kacho.svc:9092/iam/v1/hooks/provision"
                    auth:
                      type: api_key
                      config: { in: header, name: X-Kacho-Hook-Token, value: "" }
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: reader }
spec:
  template:
    spec:
      volumes:
        - name: c
          configMap: { name: cfg }
      containers:
        - name: srv
          args: ["serve", "--config", "/etc/x/app.yaml"]
          volumeMounts:
            - { name: c, mountPath: /etc/x }
""")
PEOF
  run "$tmp/arr.yaml"
  if [ "$code" = 1 ] && [[ "$out" == *'внутри массива'* ]]; then
    echo "  (E) полоса в массиве, картой настроек  → КРАСНЫЙ"
  else echo "  (E) полоса в массиве, картой настроек  → ПРОПУСТИЛ ($code): $out"; rc=1; fi

  # (F) законный близнец: ТА ЖЕ полоса в массиве, но конфигурацию процессу
  #     готовит шаг ДО старта (том — не карта настроек). Обязан МОЛЧАТЬ.
  sed 's/          configMap: { name: cfg }/          emptyDir: {}/' "$tmp/arr.yaml" > "$tmp/arr-rendered.yaml"
  run "$tmp/arr-rendered.yaml"
  if [ "$code" = 0 ]; then echo "  (F) та же полоса, подготовлена до старта → МОЛЧИТ"
  else echo "  (F) та же полоса, подготовлена до старта → ЛОЖНОЕ СРАБАТЫВАНИЕ ($code): $out"; rc=1; fi

  # (G) ПРЕДПОСЫЛКА: читателей ноль — отказ, а не молчаливый успех
  printf 'apiVersion: v1\nkind: ConfigMap\nmetadata: { name: x }\ndata: {}\n' > "$tmp/empty.yaml"
  run "$tmp/empty.yaml"
  if [ "$code" = 2 ] && [[ "$out" == *'предпосылка сломана'* ]]; then
    echo "  (G) читателей ноль                     → ОТКАЗ, не успех"
  else echo "  (G) читателей ноль                     → ПРОПУСТИЛ ($code): $out"; rc=1; fi

  rm -rf "$tmp"
  echo "самопроверка: $( [ $rc -eq 0 ] && echo ПРОЙДЕНА || echo ПРОВАЛЕНА ) (7 утверждений, из них 2 — законные близнецы)"
  return $rc
}

[ "${1:-}" = "--self-test" ] && { self_test; exit $?; }

echo "--- самопроверка разбора ---"
self_test || fail "самопроверка провалена: разбор не доказал способности упасть и смолчать"

# ── собственно проверка: каждый развёртываемый стенд ─────────────────────────
STACKS="$(stacks_names)"
EXPECTED_ASSERTIONS="$(printf '%s\n' "$STACKS" | grep -c . || true)"
[ "$EXPECTED_ASSERTIONS" -ge 1 ] || fatal "таблица стендов не дала ни одного имени — обходить нечего"
for stack in $STACKS; do
  args="$(stacks_args "$stack" "$UMBRELLA")"
  render="$(mktemp)"
  # Отказ рендера — УСЛОВИЕ прогона (несобранные зависимости умбреллы, нет helm),
  # а не свойство полосы обратного вызова, о которой эта проверка написана.
  # shellcheck disable=SC2086  # args — намеренно раскрываемый набор -f
  helm_try kacho-umbrella "$UMBRELLA" $args
  render_or_fatal "стек $stack"
  printf '%s\n' "$HELM_OUT" > "$render"
  echo "--- стек $stack ---"
  python3 "$CHECKER" < "$render"
  code=$?
  rm -f "$render"
  case "$code" in
    0) ok ;;
    2) fatal "стек $stack: разбору нечего осматривать (см. строку выше)" ;;
    *)
      echo "FAIL: стек $stack разворачивает полосу обратного вызова без источника величины." >&2
      echo "      Процесс отправит пустой либо дословный заголовок, служба прав ответит 401," >&2
      echo "      а край переведёт это арендатору в 502. Объяви величину переменной по пути" >&2
      echo "      ключа конфигурации из secretKeyRef, либо готовь конфигурацию шагом ДО" >&2
      echo "      старта процесса. Комментарий в профиле источником не является." >&2
      fail "стек $stack разворачивает полосу обратного вызова без источника величины"
      ;;
  esac
done

[ "$N" -ge 1 ] || fatal "не осмотрено ни одного стека — таблица стендов пуста"
outcome_verdict "стеков осмотрено: $N"
