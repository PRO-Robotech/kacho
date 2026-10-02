#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# cert-manager-release-before-product-inject.sh — доказательство того, что
# проба cert-manager-release-before-product-test.sh СПОСОБНА покраснеть на
# каждом дефекте порядка cert-manager, который ей поручен, и молчит на дереве.
#
# ОТКУДА ДЕФЕКТЫ. Девять — опыты ревью полосы kacho#2840: каждый прошёл
# прежнюю редакцию пробы с кодом 0 (10 из 10). Ещё два — полосы kacho#2931,
# заведшей третий путь подъёма `own-up`: редакция пробы без А4 пропускала оба
# кодом 0. Они записаны здесь, а не в журнале полосы, затем чтобы слепота,
# однажды снятая, не вернулась молча.
#
# ИНЪЕКЦИЯ — НАСТОЯЩИМ ВХОДОМ. Мутант — копия Makefile дерева с ОДНОЙ подменой;
# подмена обязана найтись в Makefile ровно один раз, иначе опыт не внесён и это
# «условие не создано» (код 2), а не зелёное. Проба читает мутант через
# MAKEFILE_UNDER_TEST; рецепт цели исполняется настоящим make против подставных
# kubectl и helm самой пробы.
#
# ИСХОД СВЕРЯЕТСЯ С МЕТКОЙ. Мало, чтобы проба покраснела: она обязана
# покраснеть ТЕМ утверждением, которому дефект поручен. Красное от соседа
# означало бы, что поручение не держит никто.
#
# Исходов три: 0 — каждый мутант пойман своим утверждением, дерево зелено;
# 1 — мутант прошёл либо пойман не тем; 2 — условие не создано.
set -uo pipefail

INJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROBE="$INJECT_DIR/cert-manager-release-before-product-test.sh"
MAKEFILE="$INJECT_DIR/../../Makefile"

# ПРЕДПОСЫЛКА: ЗАВИСИМОСТИ УМБРЕЛЛЫ МАТЕРИАЛИЗОВАНЫ. Проба берёт версию из
# архива чарта cert-manager (CERT_MANAGER_CHART лежит в helm/umbrella/charts/,
# которого нет в git), и без него ответила бы кодом 2 на дереве и на каждом
# мутанте. Вопрос задаётся общей библиотекой каталога — довод в шапке premise.sh.
# shellcheck source=tests/helm/premise.sh
. "$INJECT_DIR/premise.sh" || { echo "ОТКАЗ: библиотека предпосылки не подключилась — молчаливый пропуск предпосылки хуже её отсутствия"; exit 1; }
premise_chart_deps

[ -f "$PROBE" ] || { echo "ОТКАЗ: нет пробы $PROBE — доказывать нечего"; exit 2; }
[ -f "$MAKEFILE" ] || { echo "ОТКАЗ: нет Makefile $MAKEFILE — мутировать нечего"; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo "ОТКАЗ: нет python3 — мутанты порождать нечем"; exit 2; }

TMP="$(mktemp -d)" || { echo "ОТКАЗ: не создан временный каталог"; exit 2; }
trap 'rm -rf "$TMP"' EXIT

# Метка · утверждение, обязанное поймать · подмена (было → стало). Подмены — в
# python: в них табуляции, `$$` и обратные слэши рецепта, и оболочка их бы
# переписала.
python3 - "$MAKEFILE" "$TMP" <<'PY' || { echo "ОТКАЗ: мутанты не порождены — опыт не внесён"; exit 2; }
import os, sys
src, out = sys.argv[1], sys.argv[2]
text = open(src, encoding="utf-8").read()
# Вызов цели в `dev-up` и в `own-up` (kacho#2931) записан одинаково, поэтому
# якорь каждого берёт и СОСЕДНЮЮ строку своего рецепта: без неё подмена нашлась
# бы дважды, и опыт не был бы внесён ни в один из них.
KIND_CALL = ("\t$(MAKE) --no-print-directory cert-manager-up CERT_MANAGER_NAMESPACE=kacho \\\n"
             "\t  EXPECT_CONTEXT=kind-$(CLUSTER_NAME); \\\n")
DEVUP_CALL = KIND_CALL + '\techo "=== предусловные секреты'
OWNUP_NEXT = "\t$(MAKE) --no-print-directory module-manifests-configmap MODULE_MANIFESTS_STACK=own \\\n"
OWNUP_CALL = KIND_CALL + OWNUP_NEXT
STACKUP_CALL = ("\t$(MAKE) --no-print-directory cert-manager-up CERT_MANAGER_NAMESPACE=$(STACK_NAMESPACE) \\\n"
                "\t  EXPECT_CONTEXT=\"$$ctx\"; \\\n")
MUTANTS = [
  ("nohelm-installs", "Б6",
   '    echo "    Не ставится и не ждётся: второй экземпляр упёрся бы во владение CRD."; \\\n\t    exit 0 ;; \\\n',
   '    why="поставлен не helm" ;; \\\n'),
  ("garbled-read-as-absent", "Б7",
   'd=json.loads(s) if s else None;',
   'd=(json.loads(s) if s.startswith("{") else None) if s else None;'),
  ("release-status-ignored", "Б8",
   'if [ "$$have" = "deployed|$$want" ]; then',
   'if [ "$${have#*|}" = "$$want" ]; then'),
  ("install-namespace-literal", "Б1",
   '-n "$$ns" --create-namespace',
   '-n kacho --create-namespace'),
  ("list-without-filter", "Б2",
   'helm list -n "$$ns" --filter "^$$rel\\$$" -o json',
   'helm list -n "$$ns" -o json'),
  ("second-install-short-flag", "А1",
   DEVUP_CALL,
   "\thelm upgrade -i $(CERT_MANAGER_RELEASE) $(CERT_MANAGER_CHART) -n kacho --set crds.enabled=true; \\\n" + DEVUP_CALL),
  ("second-install-literal-release", "А1",
   DEVUP_CALL,
   "\thelm upgrade --install kacho-cert-manager $(CERT_MANAGER_CHART) -n kacho --set crds.enabled=true; \\\n" + DEVUP_CALL),
  ("stackup-call-is-unquoted-echo", "А3",
   STACKUP_CALL,
   "\techo $(MAKE) --no-print-directory cert-manager-up CERT_MANAGER_NAMESPACE=$(STACK_NAMESPACE); \\\n"),
  ("stackup-call-is-printf-argument", "А3",
   STACKUP_CALL,
   "\tprintf '%s\\n' \"$(MAKE) --no-print-directory cert-manager-up CERT_MANAGER_NAMESPACE=$(STACK_NAMESPACE)\"; \\\n"),
  # kacho#2931: третий путь подъёма. Оба опыта прошли редакцию пробы без А4
  # кодом 0 — путь применял бы продукт без судьи порядка.
  ("ownup-call-is-unquoted-echo", "А4",
   OWNUP_CALL,
   "\techo $(MAKE) --no-print-directory cert-manager-up CERT_MANAGER_NAMESPACE=kacho; \\\n" + OWNUP_NEXT),
  ("ownup-product-before-call", "А4",
   OWNUP_CALL,
   "\thelm upgrade --install kacho-umbrella ./helm/umbrella -n kacho; \\\n" + OWNUP_CALL),
]
with open(os.path.join(out, "index"), "w", encoding="utf-8") as idx:
    for name, label, a, b in MUTANTS:
        n = text.count(a)
        if n != 1:
            print(f"ОТКАЗ: {name}: подмена найдена в Makefile {n} раз(а), а не один — опыт не внесён")
            sys.exit(2)
        open(os.path.join(out, name + ".mk"), "w", encoding="utf-8").write(text.replace(a, b))
        idx.write(f"{name} {label}\n")
print(f"мутантов порождено: {len(MUTANTS)}")
PY

rc=0; caught=0; total=0

bash "$PROBE" >"$TMP/tree.out" 2>&1; t=$?
case "$t" in
  0) echo "  ✓ дерево: проба зелена (код 0)" ;;
  2) echo "ОТКАЗ: проба на дереве — условие не создано (код 2):"; tail -3 "$TMP/tree.out"; exit 2 ;;
  *) echo "  ✗ дерево: проба красна (код $t) — законный близнец обязан молчать:"; sed 's/^/      | /' "$TMP/tree.out" | tail -20; rc=1 ;;
esac

while read -r name label; do
  total=$((total + 1))
  MAKEFILE_UNDER_TEST="$TMP/$name.mk" bash "$PROBE" >"$TMP/$name.out" 2>&1; got=$?
  if [ "$got" -eq 2 ]; then
    echo "ОТКАЗ: мутант $name — проба ответила «условие не создано» (код 2):"; tail -3 "$TMP/$name.out"; exit 2
  fi
  if [ "$got" -eq 1 ] && grep -q "^  ✗ $label" "$TMP/$name.out"; then
    caught=$((caught + 1))
    echo "  ✓ $name: пойман утверждением $label (код 1)"
  elif [ "$got" -eq 1 ]; then
    rc=1
    echo "  ✗ $name: проба красна, но не утверждением $label — поручение не держит никто:"
    grep '^  ✗' "$TMP/$name.out" | sed 's/^/      | /'
  else
    rc=1
    echo "  ✗ $name: проба ПРОПУСТИЛА дефект (код $got), ждали находку $label"
  fi
done <"$TMP/index"

echo "мутантов: $total, поймано своим утверждением: $caught; дерево — законный близнец"
[ "$total" -gt 0 ] || { echo "ОТКАЗ: мутантов ноль — «все пойманы» значило бы «ни один не внесён»"; exit 2; }
exit "$rc"
