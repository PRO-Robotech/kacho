#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# identity-mail-lane-runtime-inject.sh — доказательство того, что шаг подстановки
# (место С2 решения Р4а приёмки ID-MAIL-1) СПОСОБЕН уронить запуск на негодной
# почтовой величине и МОЛЧИТ на законной.
#
# ПОЧЕМУ ОТДЕЛЬНОЕ ДОКАЗАТЕЛЬСТВО, А НЕ ОДНО НА ДВА МЕСТА. У С1 и С2 разные
# предметы: страж рендера видит ОБЪЯВЛЕНИЯ профиля и не видит величину, которая
# приезжает секретом либо слоем учётных данных вне git; шаг подстановки видит
# ровно доехавшую величину и не видит, объявил ли профиль ручку. Молчание одного
# ничего не говорит о способности второго упасть, поэтому доказательства два.
#
# ЧТО ИМЕННО ПРОГОНЯЕТСЯ: скрипт, ИЗВЛЕЧЁННЫЙ ИЗ ОТРЕНДЕРЕННОГО пода, а не его
# копия. Копия разошлась бы с шаблоном молча — ровно тот класс, который эти
# гейты и ловят. Пути каталогов подменяются на временные: дерево не трогается,
# кластер не нужен.
#
# ОСОБО ПРОВЕРЯЕТСЯ ПОРЯДОК ИСТОЧНИКОВ. Служба личности переопределяет ключи
# конфигурации переменными, и ПЕРЕМЕННАЯ БЬЁТ ФАЙЛ. Проверка, судящая файл при
# действующей переменной, была бы зелена ровно в том состоянии, ради которого
# написана, — поэтому среди случаев есть пара «файл законен, переменная негодна».

set -uo pipefail
cd "$(dirname "$0")/../.."

command -v helm >/dev/null || { echo "SKIP: helm не установлен — доказательство не выполнено"; exit 2; }

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/src" "$TMP/rendered"

helm template kacho-umbrella ./helm/umbrella -n kacho \
  $(bash tests/helm/stacks.sh --args dev-prod ./helm/umbrella) > "$TMP/render.yaml" 2>"$TMP/err" || {
    echo "ОТКАЗ: рендер не удался"; tail -3 "$TMP/err"; exit 1; }

python3 - "$TMP/render.yaml" > "$TMP/script.raw" <<'PY'
import sys, yaml
for d in yaml.safe_load_all(open(sys.argv[1])):
    if not d or d.get('kind') not in ('Deployment', 'StatefulSet'):
        continue
    for c in d['spec']['template']['spec'].get('initContainers', []):
        if c['name'] == 'identity-config-render':
            print(c['args'][0]); raise SystemExit(0)
raise SystemExit("шаг подстановки не найден в рендере")
PY
[ -s "$TMP/script.raw" ] || { echo "ОТКАЗ: шаг подстановки не извлечён — проверять нечего"; exit 1; }
sed "s#/etc/kacho-identity-src#$TMP/src#g; s#/etc/kacho-identity-rendered#$TMP/rendered#g" \
  "$TMP/script.raw" > "$TMP/script.sh"

rc=0; red=0; green=0
run() { # имя · ожидание(RED|GREEN) · улика · uri-в-файле · uri-в-переменной
  local name="$1" want="$2" needle="$3" fileuri="$4" envuri="$5" got out
  cat > "$TMP/src/kratos.yaml" <<YAML
dsn: postgres://kratos@pg/kratos
courier:
  smtp:
    connection_uri: "$fileuri"
    from_address: "noreply@kacho.local"
selfservice:
  hook_token: "\${KACHO_IAM_HOOK_TOKEN}"
YAML
  if out="$(KACHO_IAM_HOOK_TOKEN=tok COURIER_SMTP_CONNECTION_URI="$envuri" sh -euc "$(cat "$TMP/script.sh")" 2>&1)"
  then got=GREEN; else got=RED; fi
  if [ "$got" != "$want" ]; then
    echo "  ОТКАЗ $name → $got, ожидалось $want"; printf '       %s\n' "$out" | tail -2; rc=1; return
  fi
  if [ "$want" = RED ] && ! printf '%s' "$out" | grep -qF -- "$needle"; then
    echo "  ОТКАЗ $name → RED, но отказ не называет $needle"; printf '       %s\n' "$out" | tail -2; rc=1; return
  fi
  echo "  ok   $name → $got"
  [ "$want" = RED ] && red=$((red+1)) || green=$((green+1))
}

LEGAL="smtp://kacho-umbrella-mailpit:1025/"

echo "=== законные близнецы: шаг обязан пропустить ==="
run "STARTTLS, величина из файла"      GREEN "" "$LEGAL" ""
run "неявный TLS, величина из файла"   GREEN "" "smtps://relay.example:465/" ""
run "величина из переменной законна"   GREEN "" "" "$LEGAL"

echo "=== инъекции: негодная величина роняет запуск и называет предмет ==="
run "пусто и там и там"                RED "пуста"            ""    ""
run "одинокая запятая"                 RED "пуста"            ","   ""
run "пробелы"                          RED "пуста"            "   " ""
run "шифрование снято"                 RED "без шифрования"   "smtp://mailhog.kacho.svc:1025/?disable_starttls=true" ""
run "проверка сертификата снята"       RED "без шифрования"   "smtps://h:465/?skip_ssl_verify=true" ""
run "неподставленная ссылка"           RED "неподставленную"  'smtp://${RELAY}:1025/' ""
run "схема без узла"                   RED "узла нет"         "smtp://" ""
run "чужая схема"                      RED "не распознана"    "http://h:25/" ""

echo "=== порядок источников: переменная БЬЁТ файл ==="
run "файл законен, переменная негодна" RED "переменная COURIER_SMTP_CONNECTION_URI" \
    "$LEGAL" "smtp://mailhog.kacho.svc:1025/?disable_starttls=true"

echo "перепись: инъекций красных $red · законных близнецов зелёных $green"
[ "$red" -ge 9 ] || { echo "ОТКАЗ: красных инъекций $red — доказательство неполно"; rc=1; }
[ "$green" -ge 3 ] || { echo "ОТКАЗ: зелёных близнецов $green — отрицание не проверено в обратную сторону"; rc=1; }
[ "$rc" = 0 ] && echo "ИТОГ: шаг подстановки способен упасть и способен смолчать" || echo "ИТОГ: ОТКАЗ"
exit "$rc"
