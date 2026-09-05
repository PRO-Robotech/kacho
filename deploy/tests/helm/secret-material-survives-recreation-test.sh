#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# secret-material-survives-recreation-test.sh — величина, которой УЖЕ ЧТО-ТО
# ЗАПИСАНО, обязана пережить повторный прогон посева (задача #1062).
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ЛОВИТ
#
# Посев стенда порождал ключ ОБЁРТКИ приватной половины подписного ключа заново
# на КАЖДОМ прогоне (`ENC_KEY="$(openssl rand -hex 32)"`). Этим ключом обёрнута
# колонка kaname.token_signing_keys.private_key_wrapped, поэтому повторный
# прогон делал всё ранее обёрнутое нерасшифровываемым НАВСЕГДА.
#
# Отказ при этом ТИХИЙ: служба поднимается, набор ключей отвечает, и только
# подпись перестаёт совпадать с уже выданными токенами. То есть «пересоздали
# стенд» и «потеряли все подписи» выглядели одинаково.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
#
# Для КАЖДОГО `create secret generic <имя>` в deploy/scripts/*.sh: вызов стоит
# внутри ветки, проверяющей присутствие ЭТОГО ЖЕ секрета (`kubectl … get secret
# <имя>`), то есть повторный прогон переиспользует существующее значение —
# ЛИБО имя названо в ведомости ниже с причиной.
#
# ЧЕГО НЕ УТВЕРЖДАЕТ: что значение секрета годно (это рантайм — iam сверяет
# длину ключа обёртки при старте и отказывается подниматься) и что секрет вообще
# нужен (это prerequisite-secrets-test.sh).
#
# ─────────────────────────────────────────────────────────────────────────────
# ГРАНИЦА ПРЕДМЕТА НАЗВАНА, А НЕ ПОДРАЗУМЕВАЕТСЯ
#
# Судятся только `create secret generic`. `create secret tls` (gen-tls-cert.sh)
# НЕ судится: его материал — самоподписанный сертификат, порождаемый из файлов
# /tmp и переиспользуемый по их наличию; его перевыпуск ломает клиентов,
# пинивших сертификат, но НИЧЕГО записанного не теряет — класс другой. Число
# несуждённых вызовов печатается всегда, чтобы «не судили» было отличимо от
# «не нашли».
#
# Самопроверка: --self-test (гейт обязан краснеть на внесённом дефекте и
# молчать на законной конструкции той же формы).
set -uo pipefail

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"   # …/deploy
SCRIPTS_DIR="$REPO_ROOT/scripts"

# Три исхода — ОДНОЙ реализацией на весь каталог: 0 зелено · 1 находка о дереве ·
# 2 условие не создано. Что расходилось здесь — три разных вещи, и две молча:
#   • `fail` означал ровно то же, что `fail` общей реализации (обрыв кодом 1), но
#     печатал в stdout и БЕЗ переписи. Частное определение снято: у одного глагола
#     в каталоге обязана быть одна реализация, иначе один и тот же вызов в двух
#     скриптах значит разное;
#   • `census` — ИМЯ ОБЩЕЙ ФУНКЦИИ, печатающей перепись при каждом ненулевом
#     исходе (её зовут `fail` и `fatal`). Частная функция того же имени делала
#     здесь совсем другое — перечисляла вызовы создания секретов, — и после
#     объявления контракта одна из двух молча перебивала бы вторую, а какая
#     именно, решал бы порядок строк в файле. Перепись вызовов переименована в
#     `secret_census`; общая перепись осталась за `census`;
#   • «нет python3» объявлялось НАХОДКОЙ О ДЕРЕВЕ (код 1). Отсутствие инструмента
#     свойством дерева не является — это условие прогона, то есть код 2.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
# Утверждение на уровне оболочки здесь РОВНО ОДНО: перепись напечатана, то есть
# вердикт вообще есть. Сами находки приезжают перечнем из разбора и накапливаются
# `violation` — их число заранее не объявить, оно зависит от посева.
EXPECTED_ASSERTIONS=1

# Проверка стоит ДО развилки самопроверки: разбор идёт python3 и в ней тоже,
# поэтому «нет python3» обязано называться условием прогона на ОБОИХ путях, а не
# только на основном (прежде самопроверка упиралась в это без единого слова).
# PyYAML не требуется — разбор идёт `re`, а не YAML, — поэтому
# `require_python_yaml` объявил бы «условие не создано» там, где оно есть.
require_python3

# ─────────────────────────────────────────────────────────────────────────────
# ВЕДОМОСТЬ ИСКЛЮЧЕНИЙ. Формат: <имя секрета>|<причина>
#
# Запись ИСТЕКАЕТ САМА: как только названный секрет становится защищённым
# проверкой присутствия (или исчезает из посева вовсе), запись теряет предмет —
# и гейт падает, требуя её снять. Список прощённых, который не истекает, — это
# место, куда следующий дефект вносят незамеченным.
LEDGER=(
)

# secret_census <каталог> — по строке на каждый вызов создания секрета:
#   <generic|tls>\t<имя>\t<GUARDED|UNGUARDED>\t<файл>:<строка>
#
# Читается ИСПОЛНЯЕМАЯ часть: строки-комментарии и хвостовые `#`-комментарии
# отбрасываются до разбора. Иначе шапка ЭТОГО ЖЕ гейта, называющая дефект,
# читалась бы как дефект — гейт, который красят его собственные объяснения,
# снимут при первом ложном срабатывании.
secret_census() {
  python3 - "$1" <<'PY'
import os, re, sys

root = sys.argv[1]
files = sorted(
    os.path.join(root, f) for f in os.listdir(root)
    if f.endswith(".sh") and os.path.isfile(os.path.join(root, f))
)
print(f"#FILES\t{len(files)}")

open_if = re.compile(r"^(if)\b")
guard_if = re.compile(r"^if\b.*\bget\s+secret\s+([A-Za-z0-9._-]+)\b")
close_fi = re.compile(r"^fi\b|^fi$")
# Имя берётся ЛЮБЫМ непробельным токеном, а не только литералом. Узкий образец
# `[A-Za-z0-9._-]+` не видел бы `create secret generic "$NAME"` ВООБЩЕ — то есть
# самый простой способ обойти гейт остался бы невидимым, а перепись показала бы
# ноль и читалась как «чисто». Нелитеральное имя — отдельный исход (ниже).
create = re.compile(r"create\s+secret\s+(generic|tls)\s+(\S+)")
literal = re.compile(r"^[A-Za-z0-9._-]+$")

for path in files:
    raw = open(path, encoding="utf-8").read().split("\n")
    code = []
    for ln in raw:
        st = ln.strip()
        if st.startswith("#"):
            code.append("")
            continue
        # хвостовой комментарий: срезаем только форму «пробел + # + пробел»,
        # чтобы не рубить строки на `#` внутри значений и URL.
        code.append(re.sub(r"\s#\s.*$", "", ln))

    # Области, накрытые проверкой присутствия секрета: (имя, начало, конец).
    regions, stack = [], []
    for i, ln in enumerate(code):
        st = ln.strip()
        if open_if.match(st):
            m = guard_if.match(st)
            stack.append((m.group(1) if m else None, i))
        elif close_fi.match(st):
            if stack:
                name, start = stack.pop()
                if name:
                    regions.append((name, start, i))

    for i, ln in enumerate(code):
        for m in create.finditer(ln):
            kind, name = m.group(1), m.group(2)
            if not literal.match(name):
                print(f"{kind}\t{name}\tNONLITERAL\t{os.path.basename(path)}:{i+1}")
                continue
            guarded = any(n == name and s < i < e for n, s, e in regions)
            state = "GUARDED" if guarded else "UNGUARDED"
            print(f"{kind}\t{name}\t{state}\t{os.path.basename(path)}:{i+1}")
PY
}

# verdict <каталог> — печатает находки (по строке) и перепись в stderr-подобной
# форме `#…`. Пусто среди не-`#`-строк = гейт молчит.
# verdict <каталог> [<запись ведомости>…]
#
# Без дополнительных доводов судит ВЕДОМОСТЬЮ ДЕРЕВА (LEDGER). Самопроверка
# передаёт СВОЮ, синтетическую, и это не удобство: её фикстуры иначе зависят от
# СОДЕРЖИМОГО живой ведомости и истекают вместе с ним. Так и вышло — запись про
# общий секрет обратных вызовов сняли вместе с посадкой её предмета (она сама
# это и предписывала), ведомость стала пустой, и две пробы, молча опиравшиеся на
# неё, покраснели: одна ложным срабатыванием, другая пропуском. Фикстура,
# привязанная к снимаемому предмету, истекает вместе с ним.
verdict() {
  local dir="$1"; shift
  local out
  local -a led=()
  if [ "$#" -gt 0 ]; then led=("$@"); else led=("${LEDGER[@]}"); fi
  out="$(secret_census "$dir")" || { echo "ОТКАЗ РАЗБОРА"; return; }
  python3 - "$out" "${led[@]}" <<'PY'
import sys

rows = sys.argv[1].split("\n")
ledger = {}
for ent in sys.argv[2:]:
    # Пустой довод — это «ведомость ЯВНО пуста», а не запись с пустым именем:
    # иначе проба, которой нужна пустая ведомость, сама родила бы исключение
    # без предмета и покраснела на собственном доводе.
    if not ent.strip():
        continue
    name, _, reason = ent.partition("|")
    ledger[name] = reason

files = 0
generic, tls = [], []
for r in rows:
    if r.startswith("#FILES\t"):
        files += int(r.split("\t")[1])
        continue
    if not r.strip():
        continue
    kind, name, state, where = r.split("\t")
    (generic if kind == "generic" else tls).append((name, state, where))

guarded = [g for g in generic if g[1] == "GUARDED"]
unguarded = [g for g in generic if g[1] == "UNGUARDED"]
nonliteral = [g for g in generic if g[1] == "NONLITERAL"]

findings = []
for name, _, where in nonliteral:
    findings.append(
        f"ИМЯ СЕКРЕТА НЕ ЛИТЕРАЛ: {where} создаёт секрет под именем '{name}'. "
        f"Гейт не может установить, переиспользуется ли значение, и молчать об "
        f"этом значило бы отдать самый простой способ пройти мимо проверки. "
        f"Задай имя литералом либо назови его в ведомости с причиной."
    )
for name, _, where in unguarded:
    if name in ledger:
        continue
    findings.append(
        f"НЕ ПЕРЕЖИВАЕТ ПОВТОРНЫЙ ПРОГОН: секрет '{name}' ({where}) создаётся "
        f"безусловно — каждый прогон посева даёт новое значение. Если им что-то "
        f"уже подписано или обёрнуто, записанное становится нечитаемым НАВСЕГДА, "
        f"и отказ будет тихим. Оберни создание проверкой присутствия "
        f"(`if kubectl -n \"$NS\" get secret {name} …; then reuse; else create; fi`) "
        f"либо внеси имя в ведомость гейта С ПРИЧИНОЙ."
    )

guarded_names = {n for n, _, _ in guarded}
present = {n for n, _, _ in generic}
for name in ledger:
    if name in guarded_names:
        findings.append(
            f"ИСКЛЮЧЕНИЕ ПОТЕРЯЛО ПРЕДМЕТ: '{name}' теперь переиспользуется, "
            f"а ведомость гейта продолжает его прощать. Снимите запись — "
            f"исключение живёт, пока у него есть предмет. Причина записи была: "
            f"{ledger[name]}"
        )
    elif name not in present:
        findings.append(
            f"ИСКЛЮЧЕНИЕ ПОТЕРЯЛО ПРЕДМЕТ: '{name}' посевом больше не заводится, "
            f"а ведомость гейта его прощает. Снимите запись."
        )

if files == 0:
    findings.append("ПЕРЕПИСЬ БЕСПРЕДМЕТНА: прочитано 0 скриптов — вердикта нет.")
elif not generic:
    findings.append(
        "ПЕРЕПИСЬ БЕСПРЕДМЕТНА: прочитано скриптов "
        f"{files}, вызовов `create secret generic` — 0. «Ноль находок» здесь "
        "неотличимо от «ноль прочитанного»: либо посев переехал, либо предикат "
        "перестал его видеть."
    )

for f in findings:
    print(f)
print(f"#перепись: скриптов {files} · `create secret generic` {len(generic)} "
      f"(переиспользуется {len(guarded)}, порождается заново {len(unguarded)}, "
      f"имя не литерал {len(nonliteral)}) · "
      f"`create secret tls` не судится {len(tls)} · ведомость {len(ledger)}")
PY
}

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА
mkscripts() { # mkscripts <каталог> — копия посева дерева
  mkdir -p "$1"
  cp "$SCRIPTS_DIR"/*.sh "$1"/ 2>/dev/null || true
}

self_test() {
  local rc=0 tmp out
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' RETURN

  # (0) дерево как есть — гейт обязан МОЛЧАТЬ.
  out="$(verdict "$SCRIPTS_DIR" | grep -v '^#' || true)"
  if [ -z "$out" ]; then echo "  (0) дерево как есть                        → МОЛЧИТ"
  else echo "  (0) дерево как есть                        → красный: $out"; rc=1; fi

  # Фикстуры ниже судятся СВОЕЙ ведомостью: пробе, которой ведомость нужна, она
  # передаётся доводом; пробе, которой не нужна, — не передаётся вовсе. Прежде
  # здесь стояло обратное: прощённый ведомостью ДЕРЕВА секрет оставляли в каждой
  # фикстуре, и самопроверка оказывалась связана с содержимым, которое обязано
  # меняться. Ведомость опустела штатно — и пробы (B) и (D) покраснели, не имея
  # ни одного дефекта в предмете, который стерегут.
  #
  # (A) ИНЪЕКЦИЯ — ровно исходный дефект: ключ обёртки создаётся безусловно.
  mkscripts "$tmp/a"
  cat > "$tmp/a/dev-prod-secrets.sh" <<'INJ'
#!/usr/bin/env bash
NS=kacho
ENC_KEY="$(openssl rand -hex 32)"
kubectl -n "$NS" create secret generic kacho-iam-jwks-enc-key \
  --from-literal=enc_key="$ENC_KEY" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
HOOK_TOKEN="$(openssl rand -hex 24)"
kubectl -n "$NS" create secret generic kacho-iam-hook-token \
  --from-literal=token="$HOOK_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
INJ
  out="$(verdict "$tmp/a" | grep -v '^#' || true)"
  if [[ "$out" == *"kacho-iam-jwks-enc-key"* && "$out" == *"НЕ ПЕРЕЖИВАЕТ"* ]]; then
    echo "  (A) ключ обёртки создаётся безусловно      → КРАСНЫЙ с координатой"
  else echo "  (A) ключ обёртки создаётся безусловно      → ПРОПУСТИЛ: $out"; rc=1; fi

  # (B) КОНТРОЛЬ той же формы: тот же секрет, но переиспользуемый — обязан МОЛЧАТЬ.
  #     Без него (A) была бы зелена на гейте, краснеющем на любом создании секрета.
  #     Ведомость здесь ПУСТА намеренно: проба судит форму, а не прощение.
  mkscripts "$tmp/b"
  cat > "$tmp/b/dev-prod-secrets.sh" <<'INJ'
#!/usr/bin/env bash
NS=kacho
if kubectl -n "$NS" get secret kacho-iam-jwks-enc-key >/dev/null 2>&1; then
  echo reusing
else
  ENC_KEY="$(openssl rand -hex 32)"
  kubectl -n "$NS" create secret generic kacho-iam-jwks-enc-key \
    --from-literal=enc_key="$ENC_KEY" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
fi
INJ
  out="$(verdict "$tmp/b" "" | grep -v '^#' || true)"
  if [ -z "$out" ]; then echo "  (B) тот же секрет, но переиспользуемый     → МОЛЧИТ"
  else echo "  (B) тот же секрет, но переиспользуемый     → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; fi

  # (B2) ВЕДОМОСТЬ ДЕЛАЕТ СВОЮ РАБОТУ: названное ею безусловное создание прощено.
  #      Пара к (D): здесь запись предмет ИМЕЕТ и обязана молчать, там предмет
  #      потеряла и обязана краснеть. Прежде это свойство проверялось лишь
  #      побочно — тем, что живая ведомость прощала секрет, оставленный в (B).
  mkscripts "$tmp/b2"
  cat > "$tmp/b2/dev-prod-secrets.sh" <<'INJ'
#!/usr/bin/env bash
NS=kacho
T="$(openssl rand -hex 24)"
kubectl -n "$NS" create secret generic kacho-selftest-forgiven \
  --from-literal=token="$T" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
INJ
  out="$(verdict "$tmp/b2" "kacho-selftest-forgiven|синтетическая запись самопроверки" \
         | grep -v '^#' || true)"
  if [ -z "$out" ]; then echo "  (B2) ведомость прощает названное ею       → МОЛЧИТ"
  else echo "  (B2) ведомость прощает названное ею       → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; fi

  # (C) ИНЪЕКЦИЯ: новый секрет, которого нет в ведомости, создаётся безусловно.
  mkscripts "$tmp/c"
  cp "$tmp/b/dev-prod-secrets.sh" "$tmp/c/dev-prod-secrets.sh"
  cat >> "$tmp/c/dev-prod-secrets.sh" <<'INJ'
NEW="$(openssl rand -hex 32)"
kubectl -n "$NS" create secret generic kacho-some-new-key \
  --from-literal=k="$NEW" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
INJ
  out="$(verdict "$tmp/c" | grep -v '^#' || true)"
  if [[ "$out" == *"kacho-some-new-key"* ]]; then
    echo "  (C) НОВЫЙ секрет вне ведомости             → КРАСНЫЙ с координатой"
  else echo "  (C) НОВЫЙ секрет вне ведомости             → ПРОПУСТИЛ: $out"; rc=1; fi

  # (D) САМОИСТЕЧЕНИЕ ведомости: прощённый секрет стал переиспользуемым —
  #     запись потеряла предмет и обязана быть снята. Ведомость здесь СВОЯ: в
  #     ведомости дерева этой записи может не быть вовсе (и сейчас нет — её сняли
  #     вместе с посадкой предмета), а проба обязана судить СВОЙСТВО ГЕЙТА, а не
  #     сегодняшний состав ведомости. Пара к (B2).
  mkscripts "$tmp/d"
  cat > "$tmp/d/dev-prod-secrets.sh" <<'INJ'
#!/usr/bin/env bash
NS=kacho
if kubectl -n "$NS" get secret kacho-selftest-forgiven >/dev/null 2>&1; then
  echo reusing
else
  T="$(openssl rand -hex 24)"
  kubectl -n "$NS" create secret generic kacho-selftest-forgiven \
    --from-literal=token="$T" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
fi
INJ
  out="$(verdict "$tmp/d" "kacho-selftest-forgiven|синтетическая запись самопроверки" \
         | grep -v '^#' || true)"
  if [[ "$out" == *"ИСКЛЮЧЕНИЕ ПОТЕРЯЛО ПРЕДМЕТ"* && "$out" == *"kacho-selftest-forgiven"* ]]; then
    echo "  (D) прощённый секрет стал переиспользуемым → КРАСНЫЙ: снимите запись"
  else echo "  (D) прощённый секрет стал переиспользуемым → ПРОПУСТИЛ: $out"; rc=1; fi

  # (E) ПРЕДПОСЫЛКА ГЕЙТА: посев не найден вовсе — «ноль находок» обязано быть
  #     отличимо от «ноль прочитанного».
  mkdir -p "$tmp/e"
  out="$(verdict "$tmp/e" | grep -v '^#' || true)"
  if [[ "$out" == *"ПЕРЕПИСЬ БЕСПРЕДМЕТНА"* ]]; then
    echo "  (E) скриптов не найдено                    → КРАСНЫЙ, не «чисто»"
  else echo "  (E) скриптов не найдено                    → ПРОПУСТИЛ: $out"; rc=1; fi

  # (F) КОНТРОЛЬ границы: `create secret tls` НЕ судится, но и не теряется —
  #     он обязан попасть в перепись отдельным числом.
  mkscripts "$tmp/f"
  out="$(verdict "$tmp/f" | grep '^#перепись')"
  if [[ "$out" == *"не судится 1"* ]]; then
    echo "  (F) create secret tls                      → НЕ СУДИТСЯ, но назван числом"
  else echo "  (F) create secret tls                      → ПРОПУСТИЛ: $out"; rc=1; fi

  # (G) КОНТРОЛЬ СЛЕПОЙ ЗОНЫ: имя секрета задано переменной — гейт судить не
  #     может и обязан СКАЗАТЬ это, а не промолчать.
  mkscripts "$tmp/g"
  cat > "$tmp/g/dev-prod-secrets.sh" <<'INJ'
#!/usr/bin/env bash
NS=kacho
NAME=kacho-iam-jwks-enc-key
kubectl -n "$NS" create secret generic "$NAME" \
  --from-literal=enc_key="$(openssl rand -hex 32)" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
HOOK_TOKEN="$(openssl rand -hex 24)"
kubectl -n "$NS" create secret generic kacho-iam-hook-token \
  --from-literal=token="$HOOK_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
INJ
  out="$(verdict "$tmp/g" | grep -v '^#' || true)"
  if [[ "$out" == *"ИМЯ СЕКРЕТА НЕ ЛИТЕРАЛ"* ]]; then
    echo "  (G) имя секрета задано переменной          → КРАСНЫЙ, не молчание"
  else echo "  (G) имя секрета задано переменной          → ПРОПУСТИЛ: $out"; rc=1; fi

  echo "самопроверка: $( [ $rc -eq 0 ] && echo ПРОЙДЕНА || echo ПРОВАЛЕНА )"
  return $rc
}

[ "${1:-}" = "--self-test" ] && { self_test; exit $?; }

# Каталог посева — часть ДЕРЕВА, а не оснастка машины: его отсутствие остаётся
# находкой (код 1), как и было. Общей проверки каталога в outcome.sh нет —
# `require_file_present` судит `-f` и на каталоге отказала бы неверным словом.
[ -d "$SCRIPTS_DIR" ] || fail "каталог посева не найден: $SCRIPTS_DIR"

RESULT="$(verdict "$SCRIPTS_DIR")"
CENSUS="$(printf '%s\n' "$RESULT" | grep '^#перепись' || true)"
FINDINGS="$(printf '%s\n' "$RESULT" | grep -v '^#' | sed '/^$/d' || true)"

[ -n "$CENSUS" ] || fail "перепись не напечатана — вердикта нет"
ok
echo "  ${CENSUS#\#}"

# Находки НАКАПЛИВАЮТСЯ, а не обрываются первой: перечень секретов оператору
# дешевле увидеть за один прогон. Прежде они печатались своим `FAIL: $SCRIPT` с
# отступом — третий префикс находки в каталоге; теперь глагол один на всех.
if [ -n "$FINDINGS" ]; then
  while IFS= read -r line; do
    [ -n "$line" ] && violation "$line"
  done <<<"$FINDINGS"
fi

outcome_verdict "перепись выше"
