#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# render-umbrella-profiles.sh <каталог вывода> — отрендерить то, что РАЗВЁРТЫВАЕТСЯ:
#   * каждый профиль зонтика — цепочка `-f` из таблицы стеков (`deploy/stacks.txt`,
#     читатель `deploy/tests/helm/stacks.sh`) плюс `UMBRELLA_OPTS` из `deploy/Makefile`
#     (то, что несёт КАЖДЫЙ вызов helm по зонтику), С ХУКАМИ, по файлу на шаблон:
#     `<вывод>/stack-<стек>/<чарт>/…`;
#   * релизы вне зонтика, которые ставит `deploy/Makefile` до продукта (`cert-manager-up`):
#     cert-manager и контроллер политики выпуска — `<вывод>/release-<имя>/<чарт>/…`.
# Рядом пишется `releases.tsv` (каталог релиза → архив чарта в дереве) и `stacks.tsv`
# (стек → его каталог вывода): по ним гейты приводят цели к путям дерева и считают
# перепись по единицам рендера.
#
# ЗАЧЕМ. IaC-скан осматривал подчарты зонтика с их СОБСТВЕННЫМИ умолчаниями: значения
# профиля и хуки до сканера не доходили, а записи ведомости о долге переживали
# исправление значений (приёмка 4240ac56fd3). Скан того, что развёртывается, закрывает
# обе слепоты.
#
# ПОЧЕМУ `stack-<стек>`, А НЕ `<стек>`. trivy 0.70.0 молча пропускает каталоги `dev`,
# `proc`, `sys` в КОРНЕ скана и `.git` на любой глубине — встроенный перечень обходчика
# (в журнале `--debug`: «Skipping path path="dev"»), который `--skip-dirs` не
# переопределяет (замер 2026-10-02: каталог `dev` под корнем — 0 целей, тот же каталог
# как `devx` или `a/dev` — осмотрен). Стек `dev` выпадал из осмотра целиком. Приставка
# исключает совпадение с любым системным именем; перепись по стекам держит гейт покрытия.
#
# Флаги релизов вне зонтика (`crds.enabled`, `approveSignerNames`) повторяют рецепт
# `cert-manager-up` — совпадение держится вниманием; пути архивов и `UMBRELLA_OPTS`
# читаются из Makefile, а не выписаны. Имя выпускающего в `approveSignerNames` — заглушка
# только для рендера: от него зависит текст правила политики, а не права.
#
# Одна реализация на двух читателей: шаг задания trivy в `security-scan.yml` и гейты
# `deploy/scripts/assert-iac-*` (через `iac_scan_passes.py`) зовут ЭТОТ скрипт.
#
# Коды: 0 — всё отрендерено; 2 — условие не создано (нет helm, make, таблицы,
# зависимостей) либо стек/релиз не рендерится: вердикта нет.
set -uo pipefail

OUT="${1:?укажи каталог вывода}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)" || exit 2
UMBRELLA="$ROOT/deploy/helm/umbrella"
# Версия Kubernetes — узел стенда (kind 0.32.0 → `kindest/node:v1.36.1`); без неё helm
# рендерит под умолчанием, и чарты с требованием `kubeVersion` отказывают.
KUBE_VERSION="1.36.1"
SCAN_ISSUER="clusterissuers.cert-manager.io/iac-scan-only"

die() { echo "ОТКАЗ: $*" >&2; exit 2; }
command -v helm >/dev/null 2>&1 || die "нужен helm — рендерить нечем"
command -v make >/dev/null 2>&1 || die "нужен make — UMBRELLA_OPTS и пути релизов читаются из deploy/Makefile"
# shellcheck source=deploy/tests/helm/stacks.sh
. "$ROOT/deploy/tests/helm/stacks.sh" || die "читатель таблицы стеков не загружен"
ROWS="$(stacks_table)" || die "таблица стеков не прочитана"

# shellcheck disable=SC2016 # `$(…)` — выражения make, раскрывает их make, а не оболочка
if ! VARS="$(make -s -C "$ROOT/deploy" --no-print-directory \
      --eval 'iac-print-vars: ; @printf "%s\n%s\n%s\n" "$(UMBRELLA_OPTS)" "$(CERT_MANAGER_CHART)" "$(APPROVER_POLICY_CHART)"' \
      iac-print-vars)"; then
  die "переменные deploy/Makefile не прочитаны"
fi
UMBRELLA_OPTS="$(sed -n 1p <<<"$VARS")"
CERT_MANAGER_CHART="$(sed -n 2p <<<"$VARS")"; CERT_MANAGER_CHART="${CERT_MANAGER_CHART#./}"
APPROVER_POLICY_CHART="$(sed -n 3p <<<"$VARS")"; APPROVER_POLICY_CHART="${APPROVER_POLICY_CHART#./}"
[ -n "$CERT_MANAGER_CHART" ] && [ -n "$APPROVER_POLICY_CHART" ] \
  || die "в deploy/Makefile нет CERT_MANAGER_CHART либо APPROVER_POLICY_CHART"
read -r -a UMBRELLA_OPTS_ARR <<<"$UMBRELLA_OPTS"

bash "$ROOT/deploy/scripts/helm-umbrella-deps.sh" "$UMBRELLA" >&2 \
  || die "зависимости зонтика не материализованы — рендер был бы неполным"
rm -rf "$UMBRELLA"/tmpcharts-* || true

if ! { rm -rf -- "$OUT" && mkdir -p -- "$OUT"; }; then
  die "каталог вывода $OUT не создан"
fi

# $1 — каталог вывода, $2 — метка, далее — аргументы `helm template`
render() {
  local dst="$1" label="$2" errf wrote count; shift 2
  errf="$OUT/.err"
  # Число файлов — из отчёта самого helm («wrote <путь>» на каждый записанный шаблон),
  # а не обходом диска: считается то, что записал рендер, а не то, что лежит рядом.
  if ! wrote="$(helm template "$@" --kube-version "$KUBE_VERSION" --output-dir "$OUT/$dst" 2>"$errf")"; then
    die "$label не рендерится: $(head -c 400 "$errf")"
  fi
  rm -f -- "$errf"
  count="$(printf '%s\n' "$wrote" | grep -c '^wrote ' || true)"
  [ "$count" -ge 1 ] || die "$label отрендерил ноль записей"
  echo "render-umbrella-profiles: $label — записей шаблонов $count" >&2
}

n=0
: >"$OUT/stacks.tsv"
while IFS= read -r row; do
  [ -z "$row" ] && continue
  stack="${row%%:*}"; files="${row#*:}"
  args=()
  IFS=','; for f in $files; do args+=(-f "$UMBRELLA/$f"); done; unset IFS
  dir="stack-$stack"
  render "$dir" "стек $stack ($files)" kacho-umbrella "$UMBRELLA" -n kacho \
    "${args[@]}" "${UMBRELLA_OPTS_ARR[@]}"
  printf '%s\t%s\n' "$stack" "$dir" >>"$OUT/stacks.tsv"
  n=$((n + 1))
done <<<"$ROWS"
[ "$n" -ge 1 ] || die "ни одного стека не отрендерено"

render release-cert-manager "релиз cert-manager (cert-manager-up)" kacho-cert-manager \
  "$ROOT/deploy/$CERT_MANAGER_CHART" -n cert-manager \
  --set crds.enabled=true --set-json "approveSignerNames=[\"$SCAN_ISSUER\"]"
render release-approver-policy "релиз политики выпуска (cert-manager-up)" kacho-approver-policy \
  "$ROOT/deploy/$APPROVER_POLICY_CHART" -n cert-manager \
  --set crds.enabled=true --set-json "app.approveSignerNames=[\"$SCAN_ISSUER\"]"
printf 'release-cert-manager\tdeploy/%s\nrelease-approver-policy\tdeploy/%s\n' \
  "$CERT_MANAGER_CHART" "$APPROVER_POLICY_CHART" >"$OUT/releases.tsv"
echo "render-umbrella-profiles: стеков $n, релизов вне зонтика 2 в $OUT" >&2
