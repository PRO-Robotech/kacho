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
Версии, запиненные **внутри шагов** workflow: входы шагов (\`with: {version: …}\`),
\`go install …@vX\`, URL'ы в \`curl\`.

Эта полоса — **единственная**, следящая за свежестью пинов: автообновление зависимостей
снято владельцем 2026-08-27. Отставание \`uses:\`, go.mod, package.json и Dockerfile-FROM
сюда **не попадает** и не отслеживается сегодня ничем — это известный пробел, не граница.

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
