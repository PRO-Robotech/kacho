#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ДОКАЗАТЕЛЬСТВО ИНЪЕКЦИЕЙ для секции E гейта посадки
# (deploy/scripts/assert-cert-issuance-policy.sh, NTF1-J04).
#
# Самопроверка секции кормит предикат вердикта синтетическими наблюдениями и
# требует красного на каждом дефектном. Но самопроверка, которая проходит, ещё
# не доказывает, что она способна НЕ пройти: ось, чьё ожидание случайно совпало
# с поведением сломанного предиката, зеленеет и на нём. Поэтому здесь в КОПИЮ
# секции вносится по одному дефекту — ровно тот, который прятал бы отсутствие
# политики, — и самопроверка обязана покраснеть на каждом мутанте и остаться
# зелёной на исходнике (законный близнец той же формы).
#
# Мутанты — классы, а не опечатки:
#   builtin-approver-ignored  встроенный одобритель вправе одобрять УЦ, а вердикт
#                             считает это нормой — политика декоративна, гейт молчит;
#   unaccepted-arm-passes     ветвь политики есть, но контроллер её не принял, а
#                             вердикт засчитывает само существование объекта;
#   unread-notify-passes      «поднят ли notify» не прочитано, а вердикт читает
#                             это как «не поднят» — «не прочитано» стало «нет»;
#   headline-dropped          отказ не называет предмет — приёмка требует фразу
#                             «notify требует политики выпуска сертификатов служб»;
#   notify-identity-ignored   «поднят ли notify» судится только по имени и меткам,
#                             учётка пода не читается — notify, заведённый чартом
#                             с иным именем, читается «не поднят» (ревью 2915-J1, F1);
#   notify-unparsed-is-absent неразобранный список нагрузок читается «notify нет»;
#   approver-zero-pods-alive  контроллер политики «жив» по одному условию
#                             Available, которое Kubernetes ставит и Deployment'у
#                             с replicas: 0, — выключенный контроллер читается живым.
#
# Коды: 0 — каждый мутант покраснел, исходник зелен; 1 — гейт не способен
# покраснеть на названном мутанте (находка); 2 — опыт не поставлен.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
SECTION="$HERE/assert-cert-issuance-policy.sh"
[ -f "$SECTION" ] || { echo "ОТКАЗ: нет секции $SECTION — доказывать нечего"; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo "ОТКАЗ: нет python3 — мутанты порождать нечем"; exit 2; }
TMP="$(mktemp -d)" || { echo "ОТКАЗ: не создан временный каталог"; exit 2; }
trap 'rm -rf "$TMP"' EXIT

# Законный близнец: исходник обязан быть зелёным, иначе красный мутанта ничего
# не доказывает — он был бы красным и без дефекта.
if ! out="$(bash "$SECTION" --self-check 2>&1)"; then
  echo "ОТКАЗ: самопроверка ИСХОДНИКА красная — мутантам не с чем сравниваться:"
  echo "$out" | sed 's/^/    | /'
  exit 2
fi

python3 - "$SECTION" "$TMP" <<'PY' || { echo "ОТКАЗ: мутанты не порождены — опыт не внесён"; exit 2; }
import os, sys
src, out = sys.argv[1], sys.argv[2]
text = open(src, encoding="utf-8").read()
MUTANTS = [
  ("builtin-approver-ignored",
   '    yes)    gaps+=("встроенный одобритель cert-manager ВПРАВЕ',
   '    yes)    : || gaps+=("встроенный одобритель cert-manager ВПРАВЕ'),
  ("unaccepted-arm-passes",
   '      notready) gaps+=(',
   '      notready) : || gaps+=('),
  ("unread-notify-passes",
   '      echo "  ✗ поднят ли notify, НЕ ПРОЧИТАНО — без этого требование политики выпуска не оценить"\n      return 1 ;;',
   '      echo "  ✓ notify не поднят"\n      return 0 ;;'),
  ("headline-dropped",
   '      echo "  ✗ $HEADLINE:"',
   '      echo "  ✗ политика не найдена:"'),
  ("notify-identity-ignored",
   '    if sa == sa_want or any(',
   '    if any('),
  ("notify-unparsed-is-absent",
   '    print("unread 0 0"); sys.exit(0)',
   '    print("absent 0 0"); sys.exit(0)'),
  ("approver-zero-pods-alive",
   '    return cond and (st.get("availableReplicas") or 0) >= 1',
   '    return cond'),
]
with open(os.path.join(out, "index"), "w", encoding="utf-8") as idx:
    for name, a, b in MUTANTS:
        n = text.count(a)
        if n != 1:
            print(f"якорь мутанта {name} найден {n} раз (нужен ровно 1) — секция изменилась, мутант не внесён")
            sys.exit(1)
        with open(os.path.join(out, name + ".sh"), "w", encoding="utf-8") as f:
            f.write(text.replace(a, b))
        idx.write(name + "\n")
PY

bad=0 n=0
while read -r name; do
  n=$((n + 1))
  if out="$(bash "$TMP/$name.sh" --self-check 2>&1)"; then
    echo "  ✗ мутант $name: самопроверка ЗЕЛЁНАЯ — гейт не способен покраснеть на этом дефекте"
    bad=1
  else
    echo "  ✓ мутант $name: самопроверка покраснела ($(grep -c '✗' <<<"$out") осей)"
  fi
done <"$TMP/index"

[ "$n" -gt 0 ] || { echo "ОТКАЗ: мутантов ноль — опыт не поставлен"; exit 2; }
if [ "$bad" -ne 0 ]; then
  echo "FAIL: секция E не доказана инъекцией (мутантов $n)"
  exit 1
fi
echo "PASS: секция E краснеет на каждом из $n мутантов, исходник зелен"
