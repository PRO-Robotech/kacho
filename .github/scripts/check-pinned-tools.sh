#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# check-pinned-tools.sh — сверяет версии инструментов, ЗАПИНЕННЫЕ ВНУТРИ шагов
# workflow, с последними релизами апстрима.
#
# Зачем отдельно от dependabot: он обновляет `uses: owner/action@v4`, go.mod,
# package.json и Dockerfile-FROM, но НЕ видит версию, заданную входом шага
# (`with: {version: v3.17.0}`), командой (`go install …@v2.12.2`) или URL'ом в curl
# (kind, grpcurl). Мы пиним именно так — осознанно, ради local == CI: незапиненный
# setup-helm однажды притащил Helm 4, и helm-гейт зеленел на версии, которой нет на
# проде. Без сторожа такой пин протухает МОЛЧА (trivy со старой базой CVE, gosec без
# новых правил).
#
# Печатает markdown-строки по каждому отставшему в stdout; пусто = всё свежее.
# Ничего не обновляет: решение и PR — за человеком.
#
# Запуск локально:  bash .github/scripts/check-pinned-tools.sh
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1

# latest_gh <owner/repo> → последний НЕ-prerelease тег, без ведущей v.
latest_gh() {
  local auth=()
  [ -n "${GH_TOKEN:-}" ] && auth=(-H "Authorization: Bearer ${GH_TOKEN}")
  curl -fsSL "${auth[@]}" "https://api.github.com/repos/$1/releases/latest" 2>/dev/null \
    | jq -r '.tag_name // empty' | sed 's/^v//'
}

# pinned <glob-файлов> <ERE с одной группой> → значение пина без ведущей v.
# ВАЖНО: разделитель sed — `#`. `/` не годится (в regex'ах URL'ы со слэшами), `|` тоже
# (в ERE это alternation) — на обоих sed падает «unknown option to `s'».
pinned() {
  # shellcheck disable=SC2086  # $1 — намеренный glob
  grep -ohE "$2" $1 2>/dev/null | head -1 | sed -E "s#$2#\1#" | sed 's/^v//'
}

# HOLD — пины, которые мы держим ОСОЗНАННО. Сторож обязан о них молчать, иначе
# issue шумит вечно и его перестают читать. Каждая запись — с тикетом; снимается вместе
# с ним. Формат: <name>=<причина>.
declare -A HOLD=(
  [helm]="Helm 4 ломает umbrella: server-side apply конфликтует с kubectl-patch за
annotation openfga-model-id-rev (двойное владение) — kacho#3. Пин снимается вместе с ним."
)

stale=""
rc=0

check_one() {
  local name="$1" repo="$2" have="$3" want
  want="$(latest_gh "$repo")"
  if [ -z "$have" ]; then
    echo "  ⚠ $name — пин не найден (regex устарел?)" >&2; rc=1; return
  fi
  if [ -z "$want" ]; then
    echo "  ⚠ $name — апстрим недоступен (rate-limit?)" >&2; return
  fi
  if [ "$have" != "$want" ]; then
    if [ -n "${HOLD[$name]:-}" ]; then
      echo "  ⏸ $name: пин $have, апстрим $want — HOLD: ${HOLD[$name]%%$'\n'*}" >&2
      return
    fi
    echo "  ✗ $name: пин $have, апстрим $want" >&2
    stale="${stale}- **${name}** — запинен \`${have}\`, апстрим \`${want}\` (${repo})"$'\n'
  else
    echo "  ✓ $name: $have" >&2
  fi
}

check_one helm          helm/helm              "$(pinned '.github/workflows/*.yml .github/workflows/*.yaml' 'version: v?([0-9]+\.[0-9]+\.[0-9]+)')"
check_one golangci-lint golangci/golangci-lint "$(pinned '.github/workflows/ci.yaml'          'golangci-lint@v?([0-9]+\.[0-9]+\.[0-9]+)')"
check_one buf           bufbuild/buf           "$(pinned '.github/workflows/ci.yaml'          'version: ([0-9]+\.[0-9]+\.[0-9]+)')"
check_one kind          kubernetes-sigs/kind   "$(pinned '.github/workflows/e2e-newman.yml'   'dl/v?([0-9]+\.[0-9]+\.[0-9]+)/kind-linux')"
check_one grpcurl       fullstorydev/grpcurl   "$(pinned '.github/workflows/e2e-newman.yml'   'grpcurl_([0-9]+\.[0-9]+\.[0-9]+)_linux')"

printf '%s' "$stale"
exit $rc
