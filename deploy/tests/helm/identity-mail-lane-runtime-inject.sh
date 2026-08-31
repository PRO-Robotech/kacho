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

# Шаг судит СВОЙ перечень владения и роняет исполнение, если он не объявлен, —
# страж заведён работой о подстановке величин и стоит РАНЬШЕ почтовых проверок.
# Песочница обязана объявить его так же, как под, иначе инъекция обрывается до
# собственного предмета: обе работы верны, неверно их сочетание.
#
# Величина берётся ИЗ ТОГО ЖЕ рендера, а не выписывается здесь: выписанная
# разошлась бы с чартом молча.
SUBST_VARS="$(python3 - "$TMP/render.yaml" <<'PYVARS'
import sys, yaml
for d in yaml.safe_load_all(open(sys.argv[1])):
    if not d or d.get('kind') not in ('Deployment', 'StatefulSet'):
        continue
    for c in d['spec']['template']['spec'].get('initContainers', []):
        if c['name'] != 'identity-config-render':
            continue
        for e in c.get('env', []):
            if e.get('name') == 'KACHO_IDENTITY_SUBSTITUTED_VARS':
                print(e.get('value', '')); raise SystemExit(0)
raise SystemExit("перечень владения не объявлен в рендере")
PYVARS
)" || { echo "ОТКАЗ: перечень владения не извлечён из рендера"; exit 1; }
export KACHO_IDENTITY_SUBSTITUTED_VARS="$SUBST_VARS"
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
  case "$out" in *"$needle"*) has_needle=1 ;; *) has_needle=0 ;; esac
  if [ "$want" = RED ] && [ "$has_needle" -eq 0 ]; then
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

# ── УДОСТОВЕРЕНИЕ: ПОДСТАНОВКА, ОТКАЗ И НЕРАЗГЛАШЕНИЕ (решение Р6) ──────────
#
# ВТОРОЙ РЕНДЕР, А НЕ ПОДМЕНА ПЕРЕЧНЯ РУКАМИ. Перечень имён во владении шага
# зависит от того, объявлен ли источник удостоверения; выписанный здесь, он
# разошёлся бы с чартом молча — ровно тот класс, который эти гейты и ловят.
#
# ЗДЕСЬ НЕТ НИ ОДНОГО НАСТОЯЩЕГО УДОСТОВЕРЕНИЯ: строка ниже заведомо негодна и
# нужна лишь затем, чтобы её можно было ИСКАТЬ в выводе.
CRED_VALUE='injected-not-a-secret-7f3a'
helm template kacho-umbrella ./helm/umbrella -n kacho \
  $(bash tests/helm/stacks.sh --args dev-prod ./helm/umbrella) \
  --set 'global.kacho.identity.smtp.connectionURI=smtp://noreply%40kacho.cloud@kacho-umbrella-mailpit:1025/' \
  --set global.kacho.identity.smtp.credentialSecret.name=kacho-identity-smtp \
  --set global.kacho.identity.smtp.credentialSecret.key=password \
  > "$TMP/render-cred.yaml" 2>"$TMP/err-cred" || {
    echo "ОТКАЗ: рендер с объявленным источником удостоверения не удался"; tail -3 "$TMP/err-cred"; exit 1; }

python3 - "$TMP/render-cred.yaml" > "$TMP/script-cred.raw" <<'PY'
import sys, yaml
for d in yaml.safe_load_all(open(sys.argv[1])):
    if not d or d.get('kind') not in ('Deployment', 'StatefulSet'):
        continue
    for c in d['spec']['template']['spec'].get('initContainers', []):
        if c['name'] == 'identity-config-render':
            print(c['args'][0]); raise SystemExit(0)
raise SystemExit("шаг подстановки не найден в рендере с удостоверением")
PY
[ -s "$TMP/script-cred.raw" ] || { echo "ОТКАЗ: шаг подстановки не извлечён (ветка удостоверения)"; exit 1; }

CRED_VARS="$(python3 - "$TMP/render-cred.yaml" <<'PYVARS'
import sys, yaml
for d in yaml.safe_load_all(open(sys.argv[1])):
    if not d or d.get('kind') not in ('Deployment', 'StatefulSet'):
        continue
    for c in d['spec']['template']['spec'].get('initContainers', []):
        if c['name'] != 'identity-config-render':
            continue
        for e in c.get('env', []):
            if e.get('name') == 'KACHO_IDENTITY_SUBSTITUTED_VARS':
                print(e.get('value', '')); raise SystemExit(0)
raise SystemExit("перечень владения не объявлен в рендере с удостоверением")
PYVARS
)" || { echo "ОТКАЗ: перечень владения (ветка удостоверения) не извлечён"; exit 1; }

# ПРЕДПОСЫЛКА ДОКАЗАТЕЛЬСТВА, ПРОВЕРЯЕМАЯ ЯВНО. Если чарт перестанет добавлять
# имя удостоверения в перечень владения, все оси ниже станут вакуумными —
# зелёными по отсутствию предмета, а не по исправности. Это «условие не
# создано», и оно обязано быть отличимо от вердикта.
case "$CRED_VARS" in
  *KACHO_IDENTITY_SMTP_CREDENTIAL*) : ;;
  *) echo "ОТКАЗ: чарт не назвал KACHO_IDENTITY_SMTP_CREDENTIAL в перечне владения ($CRED_VARS) — оси удостоверения проверяли бы пустоту"; exit 1 ;;
esac

sed "s#/etc/kacho-identity-src#$TMP/src#g; s#/etc/kacho-identity-rendered#$TMP/rendered#g" \
  "$TMP/script-cred.raw" > "$TMP/script-cred.sh"

run_cred() { # имя · ожидание(RED|GREEN) · улика · величина-удостоверения · uri-в-файле
  local name="$1" want="$2" needle="$3" cred="$4" fileuri="$5" got out
  cat > "$TMP/src/kratos.yaml" <<YAML
dsn: postgres://kratos@pg/kratos
courier:
  smtp:
    connection_uri: "$fileuri"
    from_address: "noreply@kacho.local"
selfservice:
  hook_token: "\${KACHO_IAM_HOOK_TOKEN}"
YAML
  if out="$(KACHO_IDENTITY_SUBSTITUTED_VARS="$CRED_VARS" KACHO_IAM_HOOK_TOKEN=tok \
            KACHO_IDENTITY_SMTP_CREDENTIAL="$cred" COURIER_SMTP_CONNECTION_URI='' \
            sh -euc "$(cat "$TMP/script-cred.sh")" 2>&1)"
  then got=GREEN; else got=RED; fi
  if [ "$got" != "$want" ]; then
    echo "  ОТКАЗ $name → $got, ожидалось $want"; printf '       %s\n' "$out" | tail -2; rc=1; return
  fi
  # Сравнение БЕЗ внешнего процесса: `grep -q` выходит до конца входа, писатель
  # слева получает SIGPIPE, и под `pipefail` найденное объявляется ненайденным.
  if [ "$want" = RED ] && [[ "$out" != *"$needle"* ]]; then
    echo "  ОТКАЗ $name → RED, но отказ не называет $needle"; printf '       %s\n' "$out" | tail -2; rc=1; return
  fi
  # НЕРАЗГЛАШЕНИЕ ПРОВЕРЯЕТСЯ НА КАЖДОЙ ОСИ, а не отдельной. Журнал пода
  # читается шире секрета ровно как карта настроек, ради которой удостоверение
  # оттуда и выносили: величина, напечатанная в отказе, вернулась бы туда же —
  # только другой дверью. Утверждение отрицательное, поэтому рядом стоит
  # положительный контроль: подставленная величина ДОЕЗЖАЕТ до файла.
  #
  # ГРАНИЦА НАЗВАНА: утверждение относится к величине, ОТЛИЧНОЙ от имени ручки.
  # На оси «величина равна своему имени» совпадение законно и обязательно —
  # отказ там называет РУЧКУ, а называть ручку он обязан (иначе оператор не
  # узнает, что чинить). Сверять эту ось на неразглашение значило бы требовать
  # молчания ровно там, где требуется речь.
  if [ "$cred" = "$CRED_VALUE" ] && [[ "$out" == *"$cred"* ]]; then
    echo "  ОТКАЗ $name → вывод шага НЕСЁТ удостоверение — оно ушло в журнал пода"; rc=1; return
  fi
  echo "  ok   $name → $got"
  [ "$want" = RED ] && red=$((red+1)) || green=$((green+1))
}

CRED_URI='smtp://noreply%40kacho.cloud:${KACHO_IDENTITY_SMTP_CREDENTIAL}@kacho-umbrella-mailpit:1025/'

echo "=== удостоверение: подстановка, отказ, неразглашение ==="
run_cred "величина подставляется, в журнал не идёт" GREEN "" "$CRED_VALUE" "$CRED_URI"
# ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ К НЕРАЗГЛАШЕНИЮ: без него отрицание «величины в выводе
# нет» зеленело бы и на шаге, который её никуда не подставил.
if grep -qF -- "$CRED_VALUE" "$TMP/rendered/kratos.yaml"; then
  echo "  ok   величина ДОЕХАЛА до отрендеренного файла → GREEN"; green=$((green+1))
else
  echo "  ОТКАЗ: величина до файла не доехала — «в журнале её нет» ничего не доказывает"; rc=1
fi
run_cred "удостоверение пусто"                 RED "пуста"        ""                              "$CRED_URI"
run_cred "величина равна своему имени"         RED "собственное" "KACHO_IDENTITY_SMTP_CREDENTIAL" "$CRED_URI"
# ВХОЖДЕНИЕ, А НЕ ТОЛЬКО РАВЕНСТВО. Класс — «величина несёт собственное имя в
# заголовок»; равенство ловит один его частный случай, и величина вида `ИМЯ=…`
# проходила проверку входа, уезжала в заголовок и отвергалась тем же `401`.
# Отказ приходил тремя проверками позже — от стража ВЫХОДА — и советовал править
# конфигурацию, тогда как править надо секрет (задача #1786).
run_cred "величина СОДЕРЖИТ своё имя"          RED "собственное" "KACHO_IDENTITY_SMTP_CREDENTIAL=$CRED_VALUE" "$CRED_URI"
run_cred "шифрование снято, величина настоящая" RED "без шифрования" "$CRED_VALUE" \
  'smtp://noreply%40kacho.cloud:${KACHO_IDENTITY_SMTP_CREDENTIAL}@relay:1025/?disable_starttls=true'

echo "перепись: инъекций красных $red · законных близнецов зелёных $green"
[ "$red" -ge 13 ] || { echo "ОТКАЗ: красных инъекций $red — доказательство неполно"; rc=1; }
[ "$green" -ge 5 ] || { echo "ОТКАЗ: зелёных близнецов $green — отрицание не проверено в обратную сторону"; rc=1; }
[ "$rc" = 0 ] && echo "ИТОГ: шаг подстановки способен упасть и способен смолчать" || echo "ИТОГ: ОТКАЗ"
exit "$rc"
