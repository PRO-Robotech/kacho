#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# identity-substitution-output-inject.sh — доказательство того, что шаг подстановки
# судит СВОЙ ВЫХОД, а не только свой вход: голое ИМЯ переменной, стоящее в
# отрендеренной конфигурации ЗНАЧЕНИЕМ, роняет запуск и называет это имя.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ — ТРЕТЬЯ РАЗЛИЧИМАЯ ПОДПИСЬ, О КОТОРОЙ ШАГ НЕ ЗНАЛ
#
# Подписей отказа полосы обратного вызова три, и они различимы на проводе:
#
#   1. ПУСТО                     — заголовок уходит пустым;
#   2. `${ИМЯ}`                  — уходит неподставленная ссылка;
#   3. `ИМЯ`  (голое, без скобок) — уходит собственное имя переменной.
#
# Первые две шаг ловил: пустую — стражем величины, ссылку — переписью остатка ПО
# ФОРМЕ `${ИМЯ}`. Третью не ловил НИ ОДНА проверка, и это не край, а слепая зона
# ровно того рода, который корпус называет главным (`testing.md` §«Гейт на
# класс», п.7): форма, о которой распознаватель не знает, не даёт ни красного,
# ни зелёного — она МОЛЧИТ. Перепись при этом печатала «осталось 1» и была
# ПРАВА: голых имён она не считает by construction.
#
# Наблюдалось на проводе (задача #1754): служба личности отправила заголовок со
# строкой имени, служба прав ответила `401`, край перевёл это арендатору в `502`,
# посев церемонии не встал, десять коллекций из тридцати восьми остались без
# отчёта — при поднятом стенде и зелёном шаге подстановки.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ИМЕННО ПРОГОНЯЕТСЯ
#
# Скрипт, ИЗВЛЕЧЁННЫЙ ИЗ ОТРЕНДЕРЕННОГО пода, а не его копия: копия разошлась бы
# с шаблоном молча — тот самый класс, который эти гейты и ловят. Пути каталогов
# подменяются на временные, дерево не трогается, кластер не нужен.
#
# ПОЧЕМУ ОТДЕЛЬНОЕ ДОКАЗАТЕЛЬСТВО, А НЕ СЛУЧАЙ В СОСЕДНЕМ. Сосед
# (identity-mail-lane-runtime-inject.sh) судит ПОЧТОВУЮ полосу и обрывается на
# ней; предмет здесь другой — конфигурация, которую шаг ОТДАЁТ процессу. Молчание
# одного ничего не говорит о способности второго упасть.
#
# ГРАНИЦА НАЗВАНА: голое имя, которым шаг НЕ владеет, находкой не считается —
# его источник законен и другой (переменная по пути ключа самой службы личности),
# и суждение о нём принадлежит владельцу этого имени, а не этому шагу. Ниже это
# утверждается законным близнецом, а не подразумевается.

set -uo pipefail
cd "$(dirname "$0")/../.."

command -v helm >/dev/null || { echo "SKIP: helm не установлен — доказательство не выполнено"; exit 2; }

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/src" "$TMP/rendered"

helm template kacho-umbrella ./helm/umbrella -n kacho \
  $(bash tests/helm/stacks.sh --args dev-prod ./helm/umbrella) > "$TMP/render.yaml" 2>"$TMP/err" || {
    echo "УСЛОВИЕ НЕ СОЗДАНО: рендер не удался (зависимости умбреллы не материализованы?)"
    tail -3 "$TMP/err"; exit 2; }

# ШАГ БЕРЁТСЯ ТАК, КАК ЕГО ПОЛУЧАЕТ ОБОЛОЧКА В ПОДЕ, а не так, как он объявлен:
# между чартом и оболочкой стоит подстановка Kubernetes над доводом контейнера.
# Прежде извлечённый текст подавался в `sh` НАПРЯМУЮ, то есть мимо неё, и класс
# задачи #1786 (удвоенный знак доллара схлопывается, подстановка пишет ИМЯ
# переменной вместо её величины) в этом доказательстве не воспроизводился
# НИКОГДА: фикстура была снисходительнее продукта (`e2e-flow.md` §5).
# Извлечение живёт в ОДНОМ месте на оба доказательства — две копии разошлись бы.
python3 tests/helm/identity-step-as-kubelet-delivers.py "$TMP/render.yaml" > "$TMP/script.raw"
[ -s "$TMP/script.raw" ] || { echo "ОТКАЗ: шаг подстановки не извлечён — проверять нечего"; exit 1; }

# Перечень владения берётся ИЗ ТОГО ЖЕ рендера, а не выписывается здесь:
# выписанный разошёлся бы с чартом молча.
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

# Имя, которым шаг владеет, — ПЕРВОЕ из перечня. Оно тоже не выписано: перечень
# может вырасти, и фикстура обязана расти вместе с ним.
OWNED_NAME="${SUBST_VARS%% *}"
[ -n "$OWNED_NAME" ] || { echo "ОТКАЗ: перечень владения пуст — фикстуре не на чём стоять"; exit 1; }

export KACHO_IDENTITY_SUBSTITUTED_VARS="$SUBST_VARS"
sed "s#/etc/kacho-identity-src#$TMP/src#g; s#/etc/kacho-identity-rendered#$TMP/rendered#g" \
  "$TMP/script.raw" > "$TMP/script.sh"

# Величина той же формы, что чеканит посев стенда (`openssl rand -hex 24`): 48
# знаков, ни одного, требующего экранирования. Форма взята оттуда, а не
# придумана, — иначе доказательство говорило бы о величине, которой не бывает.
TOKEN="$(printf '%048d' 0 | tr '0' 'a')"

rc=0; red=0; green=0
run() { # имя · ожидание(RED|GREEN) · улика · добавка к конфигурации
  local name="$1" want="$2" needle="$3" extra="$4" got out
  {
    printf 'dsn: postgres://kratos@pg/kratos\n'
    printf 'selfservice:\n'
    printf '  hook_token: "${%s}"\n' "$OWNED_NAME"
    printf '%s\n' "$extra"
  } > "$TMP/src/kratos.yaml"
  if out="$(env "$OWNED_NAME=$TOKEN" \
              COURIER_SMTP_CONNECTION_URI="smtp://kacho-umbrella-mailpit:1025/" \
              sh -euc "$(cat "$TMP/script.sh")" 2>&1)"
  then got=GREEN; else got=RED; fi
  if [ "$got" != "$want" ]; then
    echo "  ОТКАЗ $name → $got, ожидалось $want"; printf '       %s\n' "$out" | tail -3; rc=1; return
  fi
  if [ "$want" = RED ]; then
    case "$out" in
      *"$needle"*) : ;;
      *) echo "  ОТКАЗ $name → RED, но отказ не называет «$needle»"
         printf '       %s\n' "$out" | tail -3; rc=1; return ;;
    esac
    case "$out" in
      *"$OWNED_NAME"*) : ;;
      *) echo "  ОТКАЗ $name → RED, но отказ не называет саму переменную"
         printf '       %s\n' "$out" | tail -3; rc=1; return ;;
    esac
    red=$((red+1))
  else
    green=$((green+1))
  fi
  echo "  ok   $name → $got"
}

echo "=== законные близнецы: шаг обязан МОЛЧАТЬ ==="
run "только ссылки формы"        GREEN "" "  second: \"\${$OWNED_NAME}\""
run "имя в комментарии"          GREEN "" "  # источник величины — $OWNED_NAME, подставляется шагом"
run "имя после подстановки"      GREEN "" "  already: \"$TOKEN\""
run "чужое имя голым"            GREEN "" "  foreign: KACHO_NOT_OWNED_BY_THIS_STEP"

echo "=== инъекции: голое ИМЯ величиной роняет запуск и называет предмет ==="
run "голое имя величиной"        RED "ГОЛЫМ ИМЕНЕМ" "  header_value: $OWNED_NAME"
run "голое имя в кавычках"       RED "ГОЛЫМ ИМЕНЕМ" "  header_value: \"$OWNED_NAME\""
run "голое имя во вложении"      RED "ГОЛЫМ ИМЕНЕМ" "  hooks:
    - auth:
        value: $OWNED_NAME"

# ─────────────────────────────────────────────────────────────────────────────
# ИНЪЕКЦИЯ В НАСТОЯЩУЮ КАРТУ НАСТРОЕК, А НЕ В СИНТЕТИКУ
#
# Всё выше подаёт шагу карту из трёх строк. Этого достаточно, чтобы утверждать
# про РАСПОЗНАВАТЕЛЬ, и НЕдостаточно, чтобы утверждать про продукт: настоящая
# карта несёт 400+ строк, пять ссылок на одну величину, две ЧУЖИЕ ссылки формы
# `${ИМЯ}` и почтовый раздел. Класс #1786 сидел именно на ней.
#
# Карта берётся ИЗ ТОГО ЖЕ РЕНДЕРА — выписанная копия разошлась бы с чартом молча.
REAL_CM="$TMP/real-kratos.yaml"
python3 - "$TMP/render.yaml" "$REAL_CM" <<'PYCM'
import sys, yaml
for d in yaml.safe_load_all(open(sys.argv[1], encoding="utf-8")):
    if d and d.get("kind") == "ConfigMap" and d["metadata"]["name"].endswith("kacho-iam-kratos-config"):
        open(sys.argv[2], "w", encoding="utf-8").write(d["data"]["kratos.yaml"])
        raise SystemExit(0)
raise SystemExit("карта настроек службы личности не найдена в рендере")
PYCM
[ -s "$REAL_CM" ] || { echo "ОТКАЗ: настоящая карта не извлечена — инъекции не на чем стоять"; exit 1; }

run_real() { # имя · ожидание(RED|GREEN) · улика · файл-карты
  local name="$1" want="$2" needle="$3" cfg="$4" got out
  cp "$cfg" "$TMP/src/kratos.yaml"
  rm -f "$TMP/rendered/kratos.yaml"
  if out="$(env "$OWNED_NAME=$TOKEN" \
              COURIER_SMTP_CONNECTION_URI="smtps://kacho-umbrella-mailpit:1025/" \
              sh -euc "$(cat "$TMP/script.sh")" 2>&1)"
  then got=GREEN; else got=RED; fi
  if [ "$got" != "$want" ]; then
    echo "  ОТКАЗ $name → $got, ожидалось $want"; printf '       %s\n' "$out" | tail -3; rc=1; return
  fi
  if [ "$want" = RED ]; then
    case "$out" in
      *"$needle"*) : ;;
      *) echo "  ОТКАЗ $name → RED, но отказ не называет «$needle»"; rc=1; return ;;
    esac
    red=$((red+1))
  else
    # ВЕЛИЧИНА ОБЯЗАНА ДОЕХАТЬ В ОТДАВАЕМЫЙ ФАЙЛ. Это и есть прямое утверждение
    # про класс #1786: там подстановка ОТРАБОТАЛА и записала ИМЯ переменной, а
    # «шаг прошёл» об этом не говорит ничего.
    if ! grep -qF -- "$TOKEN" "$TMP/rendered/kratos.yaml"; then
      echo "  ОТКАЗ $name → GREEN, но ВЕЛИЧИНЫ в отдаваемом файле НЕТ — подстановка"
      echo "         записала не то, и «шаг прошёл» это скрывает"; rc=1; return
    fi
    green=$((green+1))
  fi
  echo "  ok   $name → $got"
}

echo "=== настоящая карта настроек: законный близнец и инъекция в него ==="
run_real "настоящая карта как есть"   GREEN ""              "$REAL_CM"
sed "s/value: \${$OWNED_NAME}/value: $OWNED_NAME/" "$REAL_CM" > "$TMP/real-bare.yaml"
if [ "$(grep -c "value: $OWNED_NAME\$" "$TMP/real-bare.yaml")" -lt 1 ]; then
  echo "ОТКАЗ: инъекция в настоящую карту не создала условия — голого имени в ней нет"; rc=1
fi
run_real "голое имя в настоящей карте" RED "ГОЛЫМ ИМЕНЕМ"   "$TMP/real-bare.yaml"

echo "перепись: инъекций красных $red · законных близнецов зелёных $green"
[ "$red" -ge 4 ] || { echo "ОТКАЗ: красных инъекций $red — доказательство неполно"; rc=1; }
[ "$green" -ge 5 ] || { echo "ОТКАЗ: зелёных близнецов $green — отрицание не проверено в обратную сторону"; rc=1; }
[ "$rc" = 0 ] && echo "ИТОГ: шаг подстановки судит свой ВЫХОД и молчит на законном" || echo "ИТОГ: ОТКАЗ"
exit "$rc"
