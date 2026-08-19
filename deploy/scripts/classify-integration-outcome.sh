#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# classify-integration-outcome.sh <код-возврата-go-test> <файл-с-выводом>
#
# Различает ТРИ исхода контейнерного прогона и печатает свой вердикт:
#
#   0  — зелёный;
#   1  — КРАСНЫЙ: проба упала, в коде есть что чинить;
#   75 — УСЛОВИЕ НЕ СОЗДАНО: Postgres не поднялся, вердикта нет НИ У ОДНОЙ пробы
#        пакета, включая те, что успели пройти.
#
# Зачем отдельным скриптом, а не веткой внутри Makefile: ветку в рецепте нечем
# доказать инъекцией, а без доказательства она сама становится тем классом,
# который мы ловим — формой без содержания. Здесь вход подаётся файлом, поэтому
# обе стороны проверяются за миллисекунды (см. classify-integration-outcome-inject.sh).
#
# Третий исход НЕ зелёный: не исполнено ничего. Он ненулевой ровно затем, чтобы
# «не выполнилось» не читалось как «прошло»; отличается он ТЕКСТОМ и кодом, а не
# снисходительностью.
set -uo pipefail

# --- самопроверка: доказательство инъекцией в обе стороны ---------------------
#
# Живёт ФЛАГОМ этого же скрипта, а не соседним файлом: перечень самопроверок
# дерева собирается по исполняемой ветке `--self-test`, и отдельный файл в него
# не попал бы — то есть не исполнялся бы никогда.
if [ "${1:-}" = "--self-test" ]; then
    TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
probes=0; failed=0
run() { # run <ожидаемый-код> <имя> <код-go-test> <тело-лога>
    local want="$1" name="$2" rc="$3" body="$4" got=0
    probes=$((probes + 1))
    printf '%s\n' "$body" > "$TMP/log"
    "$0" "$rc" "$TMP/log" >/dev/null 2>&1 || got=$?
    if [ "$got" -ne "$want" ]; then
        echo "  ПРОВАЛ $name — ждали код $want, получили $got" >&2
        failed=$((failed + 1))
        return
    fi
    echo "  ok   $name (код $got)"
}

echo "=== различение трёх исходов контейнерного прогона ==="

run 75 "(+) Postgres не поднялся — УСЛОВИЕ НЕ СОЗДАНО, а не красное" 1 \
    'ok  	github.com/PRO-Robotech/kacho/services/vpc/internal/clients	14.4s
integration Postgres unavailable: start postgres: run postgres: reaper: wait for reaper
FAIL	github.com/PRO-Robotech/kacho/services/vpc/internal/repo	60.3s'

# Законный близнец: НАСТОЯЩЕЕ падение пробы обязано остаться красным. Без него
# правило вырождалось бы в «любое падение — не наша вина».
run 1 "(−) упавшее утверждение — по-прежнему красное" 1 \
    '--- FAIL: TestSubnetCidrExclusion (0.21s)
    subnet_integration_test.go:88: ожидали 23P01, получили nil
FAIL	github.com/PRO-Robotech/kacho/services/vpc/internal/repo	31.0s'

run 0 "(−) зелёный прогон — зелёный" 0 \
    'ok  	github.com/PRO-Robotech/kacho/services/vpc/internal/repo	138.9s'

# Отказ окружения при НУЛЕВОМ коде невозможен по построению, но если такое
# придёт — исход решает код, а не текст: иначе строка в логе прошлого прогона
# перекрасила бы зелёный.
run 0 "(−) признак в логе при нулевом коде зелёного не меняет" 0 \
    'integration Postgres unavailable: (строка из прошлого прогона)
ok  	github.com/PRO-Robotech/kacho/services/vpc/internal/repo	138.9s'

# Иной ненулевой код (снятие по времени, паника харнесса) третьим исходом НЕ
# становится: у него нет признака, и подавать его как «условие не создано»
# значило бы прощать настоящую поломку.
run 2 "(−) чужой ненулевой код проходит как есть" 2 \
    'panic: test timed out after 20m0s'

echo
echo "classify-integration-outcome --self-test: проб исполнено $probes, провалов $failed"
if [ "$probes" -eq 0 ]; then
    echo "ПРОВАЛ: ни одной пробы не исполнено" >&2; exit 2
fi
if [ "$failed" -gt 0 ]; then
    echo "FAIL: различение исходов не доказано — провалов $failed из $probes" >&2; exit 1
fi
echo "PASS: различение исходов доказано в обе стороны — проб $probes"
    exit 0
fi

rc="${1:-}"
log="${2:-}"

if [ -z "$rc" ] || [ -z "$log" ] || [ ! -f "$log" ]; then
    echo "classify-integration-outcome: нужен код возврата и файл вывода" >&2
    exit 2
fi

# Признак берётся из строки, которую печатает САМ харнесс проб при отказе
# подъёма (`testmain_integration_test.go`). Это не догадка по тексту докера:
# сообщений докера много и они меняются, а эта строка — наша и стабильна.
UNAVAILABLE_MARK='integration Postgres unavailable'

if [ "$rc" -eq 0 ]; then
    exit 0
fi

if grep -q "$UNAVAILABLE_MARK" "$log"; then
    {
        echo
        echo "УСЛОВИЕ НЕ СОЗДАНО: Postgres для integration не поднялся."
        echo "Вердикта нет НИ У ОДНОЙ пробы этих пакетов — включая те, что успели пройти."
        echo "Это ТРЕТИЙ исход, а не красный: искать дефект в коде здесь нечего,"
        echo "прогон недействителен и повторяется после устранения причины."
    } >&2
    exit 75
fi

exit "$rc"
