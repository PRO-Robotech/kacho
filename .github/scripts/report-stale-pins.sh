#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# report-stale-pins.sh — заводит/обновляет ОДИН issue со списком отставших пинов.
#
# Вынесен из workflow'а: многострочный markdown в inline-`run:` уже ломал YAML этого
# файла («could not find expected ':'»). Скрипт вдобавок прогоняется локально.
#
# Вход: $STALE — markdown-строки от check-pinned-tools.sh; $GH_TOKEN.
set -euo pipefail

TITLE="Запиненные инструменты отстали от апстрима"

BODY=$(cat <<EOF
Версии, запиненные **внутри шагов** workflow. Dependabot их не видит: он обновляет
\`uses:\`, go.mod, package.json и Dockerfile-FROM, но не входы шагов
(\`with: {version: …}\`), не \`go install …@vX\` и не URL'ы в \`curl\`.

${STALE}

---

Пины — **осознанные**, ради local == CI: незапиненный \`setup-helm\` однажды притащил
Helm 4, и helm-гейт зеленел на версии, которой нет на проде. Поэтому обновление идёт
через PR с зелёным CI, а не автоматически.

Инструменты, которые мы держим намеренно (HOLD с тикетом), в список **не попадают** —
см. \`.github/scripts/check-pinned-tools.sh\`.

_Заведено \`pinned-tools-freshness\`; тело обновляется на каждом прогоне._
EOF
)

num=$(gh issue list --state open --search "$TITLE in:title" --json number -q '.[0].number' 2>/dev/null || true)
if [ -n "$num" ]; then
  gh issue edit "$num" --body "$BODY"
  echo "обновлён issue #$num"
else
  gh issue create --title "$TITLE" --label tech-debt --body "$BODY"
fi
