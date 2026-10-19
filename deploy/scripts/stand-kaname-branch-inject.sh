#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stand-kaname-branch-inject.sh — доказательство инъекцией для параметра стенда
# проб «образ ветки службы доступа вместо пина» (kacho#3312, вслед kaname#916):
# форма тега, голова ветки, выбор тега реестром, отказ без молчаливого отката на
# пин, накладка значений и сверка провенанса с головой ветки.
#
# Устройство то же, что у stand-pr-images-inject.sh: каждый механизм проверяется
# ДВАЖДЫ — на исправном коде утверждения сходятся (контроль), на коде с ОДНИМ
# внесённым дефектом краснеют (мутант убит). Мутант вносится в ПАМЯТИ (`.`
# stand-ns.sh, функция переопределяется правленой копией своего текста), рабочая
# копия не трогается; мутант, у которого правка не легла, — провал доказательства.
# Заглушка стоит на ГРАНИЦЕ процесса: реестр — таблица вместо `reachable`
# (раздел C, D; саму `reachable` судит stand-pr-images-inject.sh, раздел G),
# origin службы — временный репозиторий (раздел B). Провенанс судит самопроверка
# stand-provenance.sh (оси 23–27, kubectl — заглушка), раздел E зовёт её целиком.
#
# Кластер, реестр и сеть не нужны. Нужны bash, git, python3 с yaml и go
# (читатель имён частей продукта собирается из дерева).
#
# Исходы: 0 — все контроли сошлись, все мутанты убиты; 1 — иначе.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
pass=0 fail=0 mutants=0 killed=0 survived=0 unapplied=0

ok()  { pass=$((pass + 1)); printf '  ok     %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf '  ПРОВАЛ %s\n' "$1"; }

mutate() {
  local before after
  before="$(declare -f "$1")" || return 1
  after="$(sed -E "$2" <<<"$before")"
  [ "$before" != "$after" ] || return 1
  eval "$after"
}

verdict() {
  mutants=$((mutants + 1))
  case "$2" in
    0) survived=$((survived + 1)); bad "мутант «$1» ВЫЖИЛ — утверждения его не видят" ;;
    1) killed=$((killed + 1)); ok "мутант «$1» убит" ;;
    3) unapplied=$((unapplied + 1)); bad "мутант «$1» не лёг — образец устарел, доказательство пусто" ;;
    *) unapplied=$((unapplied + 1)); bad "мутант «$1»: прогон не выполнился (код $2) — это не «убит»" ;;
  esac
}

sha_a="702d4575c0ffee00112233445566778899aabbcc"

# ── A. ФОРМА ТЕГА: та, которой публикует конвейер службы (kaname#916) ──────────
echo "A. тег выводится из ветки и ревизии: форма задачи, затем форма посаженного"
a_checks() { # [ФУНКЦИЯ SED]
  (
    fn="${1-}" sd="${2-}"
    # Подключение — без аргументов: `.` отдаёт подключённому позиционные параметры.
    set --
    # shellcheck source=deploy/scripts/stand-ns.sh
    . "$HERE/stand-ns.sh" || exit 2
    if [ -n "$fn" ]; then mutate "$fn" "$sd" || exit 3; fi
    got="$(kaname_tags 918 "$sha_a")" || exit 1
    [ "$got" = "$(printf 'task-918-702d4575\n918-702d4575')" ] || exit 1
    got="$(kaname_tags feat/x "$sha_a")" || exit 1
    [ "$got" = "$(printf 'task-feat-x-702d4575\nfeat-x-702d4575')" ] || exit 1
    kaname_tags 918 702d4575 >/dev/null 2>&1 && exit 1
    kaname_tags "" "$sha_a" >/dev/null 2>&1 && exit 1
    exit 0
  )
}
a_checks; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: task-<ветка>-<8>, затем <ветка>-<8>; слэш — дефис; ревизия не sha40 и пустая ветка — отказ" || bad "контроль: код $rc"
a_checks kaname_tags 's|slug="\$\{branch//\\//-\}"|slug="$branch"|';   verdict "слэш ветки не переводится" $?
a_checks kaname_tags 's|\$\{sha:0:8\}|$sha|g';                        verdict "ревизия не обрезана до восьми" $?
a_checks kaname_tags 's|\^\[0-9a-f\]\{40\}\$|^[0-9a-f]+$|';           verdict "короткая ревизия принята" $?

# ── B. ГОЛОВА ВЕТКИ — ОТ ORIGIN СЛУЖБЫ, А НЕ ВВОДИТСЯ РУКАМИ ─────────────────────
echo "B. ревизия по умолчанию — голова ветки на origin службы"
origin="$tmp/origin.git"
git init -q --bare "$origin"
git init -q "$tmp/w" && git -C "$tmp/w" -c user.email=t@t -c user.name=t commit -q --allow-empty -m one
git -C "$tmp/w" push -q "$origin" HEAD:refs/heads/918 HEAD:refs/heads/918-other HEAD:refs/heads/x/918
head918="$(git -C "$tmp/w" rev-parse HEAD)"
b_checks() {
  (
    fn="${1-}" sd="${2-}"
    # Подключение — без аргументов: `.` отдаёт подключённому позиционные параметры.
    set --
    # shellcheck source=deploy/scripts/stand-ns.sh
    . "$HERE/stand-ns.sh" || exit 2
    if [ -n "$fn" ]; then mutate "$fn" "$sd" || exit 3; fi
    got="$(kaname_branch_head "$origin" 918 2>/dev/null)" || exit 1
    [ "$got" = "$head918" ] || exit 1
    kaname_branch_head "$origin" no-such 2>/dev/null; [ $? -eq 1 ] || exit 1
    kaname_branch_head "$tmp/no-repo" 918 2>/dev/null; [ $? -eq 2 ] || exit 1
    exit 0
  )
}
b_checks; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: голова ветки прочитана, тёзки (918-other, x/918) не спутаны; ветки нет — 1; origin молчит — 2" || bad "контроль: код $rc"
b_checks kaname_branch_head 's|"refs/heads/\$branch" 2>&1|"$branch*" 2>\&1|; s|\$2 == r \{ print \$1 \}|{ print $1 }|'; verdict "ветка спрошена образцом без точного отбора — тёзки подмешаны" $?
b_checks kaname_branch_head 's|^(\s*)return 1$|\1return 0|';             verdict "ветки нет — принято за успех" $?

# ── C. ВЫБОР ТЕГА РЕЕСТРОМ; НЕТ ОБРАЗА — ОТКАЗ С ИМЕНЕМ ТЕГА ─────────────────────
echo "C. тег выбирает реестр; нет образа — отказ с названием тега и командой сборки"
# registry — таблица «тег код» вместо реестра; код reachable: 0 есть, 1 404, 2 молчание.
c_checks() {
  (
    fn="${1-}" sd="${2-}"
    # Подключение — без аргументов: `.` отдаёт подключённому позиционные параметры.
    set --
    # shellcheck source=deploy/scripts/stand-ns.sh
    . "$HERE/stand-ns.sh" || exit 2
    reachable() { awk -v t="$2" '$1 == t { print $2; f=1 } END { if (!f) print 1 }' "$tmp/registry" | { read -r c; return "$c"; }; }
    if [ -n "$fn" ]; then mutate "$fn" "$sd" || exit 3; fi
    printf 'task-918-702d4575 0\n' >"$tmp/registry"
    kaname_pick_tag docker.io/prorobotech/kaname 918 "$sha_a" PRO-Robotech/kaname 2>/dev/null || exit 1
    [ "$KANAME_TAG" = task-918-702d4575 ] || exit 1
    printf '918-702d4575 0\n' >"$tmp/registry"
    kaname_pick_tag docker.io/prorobotech/kaname 918 "$sha_a" PRO-Robotech/kaname 2>/dev/null || exit 1
    [ "$KANAME_TAG" = 918-702d4575 ] || exit 1
    : >"$tmp/registry"
    kaname_pick_tag docker.io/prorobotech/kaname 918 "$sha_a" PRO-Robotech/kaname 2>"$tmp/c.err"; [ $? -eq 1 ] || exit 1
    [ -z "$KANAME_TAG" ] || exit 1
    grep -q 'task-918-702d4575' "$tmp/c.err" || exit 1
    grep -q 'gh workflow run docker-build.yml -R PRO-Robotech/kaname --ref 918' "$tmp/c.err" || exit 1
    printf 'task-918-702d4575 2\n918-702d4575 0\n' >"$tmp/registry"
    kaname_pick_tag docker.io/prorobotech/kaname 918 "$sha_a" PRO-Robotech/kaname 2>/dev/null; [ $? -eq 2 ] || exit 1
    exit 0
  )
}
c_checks; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: форма задачи; близнец — форма посаженного; нет образа — 1 с тегом и командой; молчание — 2" || bad "контроль: код $rc"
c_checks kaname_pick_tag 's|tried="\$tried \$repo:\$tag"$|KANAME_TAG="$tag"; return 0|'; verdict "нет образа — тег принят без реестра (молчаливый откат)" $?
c_checks kaname_pick_tag 's|^(\s*)return 2$|\1continue|'; verdict "молчание реестра принято за «образа нет»" $?
c_checks kaname_pick_tag 's|--ref \$branch|--ref main|';                   verdict "команда сборки называет не ту ветку" $?

# ── D. НАКЛАДКА ЗНАЧЕНИЙ: репозиторий — из цепочки, тег — выбранный, отпечаток снят ─
echo "D. накладка: репозиторий пина, тег ветки, отпечаток пуст; без параметра — накладки нет"
mkdir -p "$tmp/layers"
printf 'kaname:\n  image:\n    repository: kaname\n    tag: dev\n' >"$tmp/layers/0.yaml"
printf 'kaname:\n  image:\n    repository: docker.io/prorobotech/kaname\n    tag: main-efac3adf\n    digest: "sha256:0000"\n' >"$tmp/layers/1.yaml"
d_checks() {
  (
    fn="${1-}" sd="${2-}"
    # Подключение — без аргументов: `.` отдаёт подключённому позиционные параметры.
    set --
    # shellcheck source=deploy/scripts/stand-ns.sh
    . "$HERE/stand-ns.sh" || exit 2
    reachable() { [ "$2" = "task-918-${head918:0:8}" ] && return 0; return 1; }
    if [ -n "$fn" ]; then mutate "$fn" "$sd" || exit 3; fi
    export STAND_KANAME_REMOTE="$origin"
    KANAME_BRANCH="" KANAME_REF=""
    kaname_overlay "$tmp/k0.yaml" "$tmp/layers/0.yaml" "$tmp/layers/1.yaml" || exit 1
    [ ! -s "$tmp/k0.yaml" ] && [ -z "$KANAME_IMAGE_REF" ] || exit 1
    KANAME_BRANCH=918
    kaname_overlay "$tmp/k1.yaml" "$tmp/layers/0.yaml" "$tmp/layers/1.yaml" >/dev/null || exit 1
    python3 - "$tmp/k1.yaml" "${head918:0:8}" <<'PY' || exit 1
import sys, yaml
d = yaml.safe_load(open(sys.argv[1]))["kaname"]["image"]
assert d == {"repository": "docker.io/prorobotech/kaname", "tag": "task-918-" + sys.argv[2], "digest": ""}, d
PY
    [ "$KANAME_IMAGE_REF" = "docker.io/prorobotech/kaname:task-918-${head918:0:8}" ] && [ "$KANAME_SHA" = "$head918" ] || exit 1
    KANAME_BRANCH="" KANAME_REF="$head918"
    kaname_overlay "$tmp/k2.yaml" "$tmp/layers/0.yaml" "$tmp/layers/1.yaml" 2>/dev/null; [ $? -eq 2 ] || exit 1
    KANAME_BRANCH=no-such KANAME_REF=""
    kaname_overlay "$tmp/k3.yaml" "$tmp/layers/0.yaml" "$tmp/layers/1.yaml" 2>/dev/null; [ $? -eq 1 ] || exit 1
    exit 0
  )
}
d_checks; rc=$?
[ "$rc" -eq 0 ] && ok "контроль: без ветки — пусто; с веткой — репозиторий пина, тег головы, digest снят; ревизия без ветки — 2; ветки нет — 1" || bad "контроль: код $rc"
d_checks kaname_overlay 's|    digest: ""\\n||';                    verdict "отпечаток пина не снят — тег ветки игнорируется чартом" $?
d_checks kaname_overlay 's#\[ -z "\$KANAME_REF" \] \|\|#true ||#'; verdict "ревизия без ветки молча проглочена" $?

# ── E. ПРОВЕНАНС: ссылка ветки И ревизия образа сверяются с головой ветки ─────────
echo "E. провенанс сверяет ссылку стенда и ревизию образа ветки (самопроверка stand-provenance.sh)"
bash "$HERE/stand-provenance.sh" --self-test >"$tmp/prov.out" 2>&1; rc=$?
axes="$(grep -cE '^(23|24|25|26|27)\. ' "$tmp/prov.out")"
if [ "$rc" -eq 0 ] && [ "$axes" -eq 5 ]; then
  ok "самопроверка провенанса зелёная, осей образа ветки 5"
else
  bad "самопроверка провенанса: код $rc, осей образа ветки $axes из 5"; grep -E 'ПРОВАЛ|^2[3-7]\.' "$tmp/prov.out" | sed 's/^/      /'
fi

echo
echo "итог: утверждений $((pass + fail)) — сошлось $pass, провалено $fail; мутантов $mutants — убито $killed, выжило $survived, не легло $unapplied"
[ "$fail" -eq 0 ]
