#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# identity-hook-credential-provenance-inject.sh — доказательство того, что отчёт
# о происхождении величины НЕ ПРОИЗНОСИТ ВЕРДИКТА НАД НЕПРОЧИТАННЫМ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ (задача #1803)
#
# Отчёт печатал ИТОГ «голых имён нет» БЕЗУСЛОВНО — в том числе в прогоне, где
# отдаваемых файлов прочитано НОЛЬ: ни один работающий под не смонтировал
# каталог с отрендеренной конфигурацией, читать было нечего, а строка утверждала
# отсутствие искомого. Наблюдалось на прогоне, где конфигурация несла ПЯТЬ голых
# имён: отчёт объявил их отсутствие, не открыв ни одного файла.
#
# Это `testing.md` §«Гейт на класс», п. 3 в чистом виде: «ноль находок» обязано
# быть отличимо от «ноль прочитанного», и объём осмотренного печатается ВСЕГДА.
# Опасность здесь не в неточности, а в ЧИТАТЕЛЕ: строку видит человек посреди
# разбора, и прочитанная как вердикт она закрывает вопрос, который никто не
# задавал.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ИМЕННО ПРОГОНЯЕТСЯ — НАСТОЯЩИЙ СКРИПТ, А НЕ ЕГО ПЕРЕСКАЗ
#
# Прогоняется `deploy/scripts/identity-hook-credential-provenance.sh` целиком, с
# ПОДСТАВНЫМ `kubectl` на `PATH`. Подставной отдаёт заранее подготовленный
# перечень подов, а `exec` исполняет тело меры ЛОКАЛЬНО — тем же `sh -s`, каким
# его исполнил бы под. То есть удалённые тела (разбор заголовка, поиск голого
# имени, метка осмотра) исполняются НАСТОЯЩИЕ; подменена только доставка.
#
# Кластер не нужен, дерево не трогается, `helm` не зовётся.
#
# ─────────────────────────────────────────────────────────────────────────────
# ГРАНИЦА НАЗВАНА ЧЕСТНО
#
#   * подставной `kubectl` не проверяет прав, полей запроса и версий API — он
#     доставляет вход. Что доставка настоящая, доказывается тем, что тела мер
#     исполняются дословно и их вывод разбирается настоящим скриптом;
#   * доказательство говорит о ФОРМЕ ВЕРДИКТА и объёме осмотренного. О том, одну
#     ли величину держат стороны на ЖИВОМ стенде, оно не утверждает ничего — это
#     предмет самого отчёта, и снимается он только на стенде.

set -uo pipefail

INJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$INJECT_DIR/../.." && pwd)"
SCRIPT="$DEPLOY_ROOT/scripts/identity-hook-credential-provenance.sh"
[ -r "$SCRIPT" ] || { echo "ОТКАЗ: отчёт $SCRIPT не найден — доказывать нечего"; exit 1; }

command -v python3 >/dev/null 2>&1 || {
  echo "SKIP: python3 не установлен — доказательство НЕ ВЫПОЛНЕНО (это не красное)"; exit 2; }

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/rendered"

NAME="KACHO_IAM_HOOK_TOKEN"
HEADER="X-Kacho-Hook-Token"
RENDERED="$TMP/rendered/kratos.yaml"
# Величина той же формы, что чеканит посев стенда: 48 знаков, ни одного,
# требующего экранирования. Форма взята оттуда, а не придумана.
TOKEN="$(printf '%048d' 0 | tr '0' 'b')"

# ── ПОДСТАВНОЙ kubectl ───────────────────────────────────────────────────────
# Он ДОСТАВЛЯЕТ, а не отвечает: `exec` исполняет полученное на stdin тело меры
# настоящим `sh -s`, поэтому вся удалённая половина отчёта проверяется живой.
cat > "$TMP/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
args=("$@"); verb=""; rest=()
i=0
while [ "$i" -lt "${#args[@]}" ]; do
  case "${args[i]}" in
    -n|--namespace) i=$((i+2)); continue ;;
    get|exec) verb="${args[i]}"; rest=("${args[@]:i+1}"); break ;;
  esac
  i=$((i+1))
done
case "$verb" in
  get)
    case "${rest[0]:-}" in
      ns|namespace|namespaces) exit "${STUB_NS_RC:-0}" ;;
      secret|secrets)
        [ -n "${STUB_SECRET_B64:-}" ] || exit 1
        printf '%s' "$STUB_SECRET_B64"; exit 0 ;;
      pods|pod)
        for a in "${rest[@]}"; do
          if [ "$a" = json ]; then cat "$STUB_PODS"; exit 0; fi
        done
        exit 0 ;;
      *) exit 1 ;;
    esac ;;
  exec)
    # Всё после `--` — команда, которую под исполнил бы у себя. Исполняем её
    # здесь тем же интерпретатором и с тем же stdin.
    cmd=(); seen=0
    for a in "${rest[@]}"; do
      if [ "$seen" = 1 ]; then cmd+=("$a"); continue; fi
      [ "$a" = "--" ] && seen=1
    done
    [ "${#cmd[@]}" -gt 0 ] || exit 1
    exec "${cmd[@]}" ;;
  *) exit 1 ;;
esac
STUB
chmod +x "$TMP/bin/kubectl"

# ── Перечни подов: три формы входа ───────────────────────────────────────────
# Отправитель опознаётся по монтированию каталога отрендеренной конфигурации,
# принимающая — по переменной из того же Secret'а. Оба признака — те же, по
# которым отчёт выводит перечень; выписанных имён объектов здесь нет.
mount_dir="${RENDERED%/*}"
mkpods() { # <файл> <есть-отправитель:1|0> <перечень-владения>
  python3 - "$1" "$2" "$3" "$mount_dir" <<'PY'
import json, sys
out, sender, owned, mount = sys.argv[1], sys.argv[2] == '1', sys.argv[3], sys.argv[4]
items = []
if sender:
    items.append({
        "metadata": {"name": "kacho-identity-0"},
        "status": {"phase": "Running"},
        "spec": {
            "initContainers": [{
                "name": "identity-config-render",
                "env": [{"name": "KACHO_IDENTITY_SUBSTITUTED_VARS", "value": owned}],
            }],
            "containers": [{
                "name": "kratos",
                "volumeMounts": [{"name": "rendered", "mountPath": mount}],
                "env": [],
            }],
        },
    })
items.append({
    "metadata": {"name": "kaname-0"},
    "status": {"phase": "Running"},
    "spec": {"initContainers": [], "containers": [{
        "name": "iam",
        "volumeMounts": [],
        "env": [{"name": "KACHO_IAM_HOOK_TOKEN", "valueFrom": {
            "secretKeyRef": {"name": "kaname-hook-token", "key": "token"}}}],
    }]},
})
json.dump({"items": items}, open(out, "w"))
PY
}

mkconfig() { # <строка-величины заголовка>
  {
    printf 'selfservice:\n'
    printf '  flows:\n'
    printf '    registration:\n'
    printf '      after:\n'
    printf '        hooks:\n'
    printf '          - hook: web_hook\n'
    printf '            config:\n'
    printf '              headers:\n'
    printf '                - name: %s\n' "$HEADER"
    printf '                  value: "%s"\n' "$1"
  } > "$RENDERED"
}

rc=0; red=0; green=0

run_case() { # <имя> <файл-подов> <ожидание rc>
  CASE_OUT="$(env PATH="$TMP/bin:$PATH" \
    STUB_PODS="$2" STUB_SECRET_B64="$(printf %s "$TOKEN" | base64 | tr -d '\n')" \
    KACHO_IDENTITY_RENDERED_PATH="$RENDERED" \
    KACHO_IAM_HOOK_TOKEN="$TOKEN" \
    bash "$SCRIPT" kacho 2>&1)"
  CASE_RC=$?
  if [ "$CASE_RC" != "$3" ]; then
    echo "  ОТКАЗ $1: код $CASE_RC, ожидался $3"
    printf '       %s\n' "$CASE_OUT" | tail -6; rc=1; return 1
  fi
  return 0
}

# ПЕРЕПИСЬ ОБЯЗАНА БЫТЬ В КАЖДОМ ИСХОДЕ. Без этого «ноль находок» неотличимо от
# «ноль прочитанного» — то есть предмет доказательства ускользает целиком.
census_says() { # <вывод> <подстрока>
  case "$1" in *"$2"*) return 0 ;; *) return 1 ;; esac
}

echo "=== A. контроль: отправитель прочитан, голых имён действительно нет ==="
mkconfig "$TOKEN"
mkpods "$TMP/pods-a.json" 1 "$NAME"
if run_case "A" "$TMP/pods-a.json" 0; then
  if census_says "$CASE_OUT" "отдаваемых файлов прочитано 1" \
     && census_says "$CASE_OUT" "имён осмотрено 1" \
     && census_says "$CASE_OUT" "голых имён нет среди осмотренного"; then
    echo "  ok   A: прочитан 1 файл, осмотрено 1 имя — и только тогда сказано «нет»"
    green=$((green+1))
  else
    echo "  ОТКАЗ A: вердикт не назвал объём осмотренного либо не сказал «нет» при прочитанном"
    printf '       %s\n' "$CASE_OUT" | tail -6; rc=1
  fi
fi

echo "=== B. ДЕФЕКТ #1803: отправителя нет — вердикт не смеет говорить «нет» ==="
# Конфигурация НЕСЁТ голое имя, и это существенно: прогон, объявивший его
# отсутствие, ошибался бы не формально, а по существу — ровно как наблюдалось.
mkconfig "$NAME"
mkpods "$TMP/pods-b.json" 0 "$NAME"
if run_case "B" "$TMP/pods-b.json" 0; then
  if census_says "$CASE_OUT" "голых имён нет"; then
    echo "  ОТКАЗ B: ИТОГ объявил «голых имён нет», прочитав НОЛЬ отдаваемых файлов"
    printf '       %s\n' "$CASE_OUT" | tail -4; rc=1
  elif census_says "$CASE_OUT" "не измерено НИЧЕГО" \
       && census_says "$CASE_OUT" "отдаваемых файлов прочитано 0" \
       && census_says "$CASE_OUT" "имён осмотрено 0"; then
    echo "  ok   B: вердикт говорит «не измерено ничего» и называет оба нуля"
    red=$((red+1))
  else
    echo "  ОТКАЗ B: вердикт не сказал ни «нет», ни «не измерено» — читателю нечего понять"
    printf '       %s\n' "$CASE_OUT" | tail -4; rc=1
  fi
fi

echo "=== C. отправитель прочитан и голое имя ЕСТЬ — это находка, а не молчание ==="
mkconfig "$NAME"
mkpods "$TMP/pods-c.json" 1 "$NAME"
if run_case "C" "$TMP/pods-c.json" 1; then
  if census_says "$CASE_OUT" "ГОЛЫМ ИМЕНЕМ" && census_says "$CASE_OUT" "$NAME"; then
    echo "  ok   C: находка названа вместе с именем — отрицание в разделе A не вакуумно"
    red=$((red+1))
  else
    echo "  ОТКАЗ C: код 1 получен, но находка не названа"
    printf '       %s\n' "$CASE_OUT" | tail -4; rc=1
  fi
fi

echo "=== D. отправитель прочитан, но перечня владения нет — искать было НЕ ПО ЧЕМУ ==="
# Вторая форма того же класса: файл прочитан, а имён ноль. Отличать её от
# «прочитано ноль файлов» обязательно — причина у них разная, а исход один.
mkconfig "$TOKEN"
mkpods "$TMP/pods-d.json" 1 ""
if run_case "D" "$TMP/pods-d.json" 0; then
  if census_says "$CASE_OUT" "голых имён нет"; then
    echo "  ОТКАЗ D: ИТОГ объявил «голых имён нет» при ПУСТОМ перечне владения"
    printf '       %s\n' "$CASE_OUT" | tail -4; rc=1
  elif census_says "$CASE_OUT" "не измерено НИЧЕГО" \
       && census_says "$CASE_OUT" "отдаваемых файлов прочитано 1" \
       && census_says "$CASE_OUT" "имён осмотрено 0"; then
    echo "  ok   D: файл прочитан (1), имён осмотрено 0 — вердикт различает две причины нуля"
    red=$((red+1))
  else
    echo "  ОТКАЗ D: вердикт не различил «файл не читали» и «читать было не по чему»"
    printf '       %s\n' "$CASE_OUT" | tail -4; rc=1
  fi
fi

echo "перепись: осей 4 — подтверждающих объём $green, требующих честного нуля/находки $red"
[ "$green" -ge 1 ] || { echo "ОТКАЗ: положительного контроля нет — отрицания зеленели бы на всём сломанном"; rc=1; }
[ "$red" -ge 3 ]   || { echo "ОТКАЗ: осей, требующих различения, $red — доказательство неполно"; rc=1; }
[ "$rc" = 0 ] && echo "ИТОГ: отчёт о происхождении величины не произносит вердикта над непрочитанным" \
              || echo "ИТОГ: ОТКАЗ"
exit "$rc"
