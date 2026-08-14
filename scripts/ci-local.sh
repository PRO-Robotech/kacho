#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Локальный прогон тех же проверок, что делает конвейер, — чтобы не узнавать об
# отказе через двадцать минут ожидания.
#
# ЗАЧЕМ. Конвейер тратится один раз, на запрос в ствол; всё, что можно узнать до
# него, обязано узнаваться здесь. Иначе он становится первым читателем кода, а не
# подтверждением на чужой машине.
#
# ЧЕМ ЭТО НЕ ЯВЛЯЕТСЯ. Заменой конвейера. Здесь нет ни кластера, ни поднятого
# стенда, ни сквозных прогонов — они названы в конце явно, чтобы «локально
# зелено» не читалось шире, чем есть.
#
# ПОЧЕМУ ПЛАГИНЫ СТАВЯТСЯ ЗАНОВО. Генерация зависит от версии плагина, а не от
# версии buf: конвейер ставит их `go install` из ЭТОГО модуля, поэтому машина с
# другим бинарём в PATH даёт другой результат — и расхождение видно только в
# конвейере. Здесь они ставятся тем же способом, в отдельный каталог.
#
# Использование:
#   scripts/ci-local.sh            # всё
#   scripts/ci-local.sh proto      # только генерация и совместимость
#   scripts/ci-local.sh go|helm|ui # отдельная группа
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GROUP="${1:-all}"
WORK="${CI_LOCAL_WORK:-${TMPDIR:-/tmp}/kacho-ci-local}"
mkdir -p "$WORK"
cd "$ROOT"

fails=(); ran=0

run() { # run <имя> <команда...>
    local name="$1"; shift
    ran=$((ran + 1))
    printf '\n== %s\n' "$name"
    if "$@" > "$WORK/out.txt" 2>&1; then
        echo "   ok"
    else
        echo "   ОТКАЗ (код $?)"; tail -15 "$WORK/out.txt" | sed 's/^/   | /'
        fails+=("$name")
    fi
}

# ── proto: генерация и совместимость ────────────────────────────────────────
proto_group() {
    local bin="$WORK/protoc-bin"
    mkdir -p "$bin"
    echo "== плагины генерации из модуля (версии как в конвейере)"
    if ! GOBIN="$bin" go install \
        google.golang.org/protobuf/cmd/protoc-gen-go \
        google.golang.org/grpc/cmd/protoc-gen-go-grpc \
        github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway > "$WORK/out.txt" 2>&1; then
        echo "   ОТКАЗ: плагины не поставились"; tail -8 "$WORK/out.txt" | sed 's/^/   | /'
        fails+=("плагины генерации"); return
    fi
    export PATH="$bin:$PATH"

    run "buf lint" bash -c "cd '$ROOT/proto' && buf lint"
    # Та же адъюдикация, что в конвейере, и ТЕМ ЖЕ инструментом — иначе локально и
    # на чужой машине исполнялись бы две разные проверки одного предмета, и разошлись
    # бы они молча: обе отвечают «зелено» на зелёном входе.
    run "adjudicate breaking (перечень объявленных разрывов)" bash -c '
        set -uo pipefail
        go build -o "'"$WORK"'/adjudicate" ./tools/declaredbreak/cmd/adjudicate-declared-breaks
        cd "'"$ROOT"'/proto"
        set +e
        buf breaking --against "'"$ROOT"'/.git#branch=main,subdir=proto"             --error-format=json > "'"$WORK"'/buf-breaking.jsonl" 2>"'"$WORK"'/buf-breaking.err"
        rc=$?
        set -e
        case "$rc" in
            0|100) ;;
            *)  echo "buf breaking не сделал своей работы (код $rc) — это НЕ «разрывов нет»" >&2
                cat "'"$WORK"'/buf-breaking.err" >&2 || true
                exit 2 ;;
        esac
        cd "'"$ROOT"'"
        "'"$WORK"'/adjudicate" proto/declared-breaks.yaml < "'"$WORK"'/buf-breaking.jsonl"
    ' 

    ran=$((ran + 1))
    printf '\n== generate-diff (стабы в синхроне с .proto)\n'
    ( cd "$ROOT/proto" && buf generate ) > "$WORK/out.txt" 2>&1
    local dirty
    dirty="$(git -C "$ROOT" status --porcelain pkg/api | wc -l | tr -d ' ')"
    if [ "$dirty" = "0" ]; then
        echo "   ok — стабы совпадают с генерацией"
    else
        echo "   ОТКАЗ: генерация меняет $dirty файл(ов) — стабы собраны другим плагином"
        git -C "$ROOT" diff --stat pkg/api | tail -3 | sed 's/^/   | /'
        # Диагностика, а не совет: у ЭТОГО отказа две причины, и они лечатся
        # противоположно. Дерево устарело — коммить генерацию. Плагин не тот —
        # коммитить НЕЛЬЗЯ, иначе в ствол уедет выход чужой версии.
        echo "   | различить причины:"
        echo "   |   which -a protoc-gen-grpc-gateway            # больше одной строки — уже находка"
        echo "   |   go version -m \"\$(which protoc-gen-grpc-gateway)\"  # версия ИСПОЛНЯЕМОГО файла"
        echo "   | этот прогон ставит плагины из go.mod и кладёт их первыми в PATH,"
        echo "   | поэтому здесь отказ означает УСТАРЕВШЕЕ ДЕРЕВО. Если тот же diff"
        echo "   | получается вызовом buf напрямую — виновата копия плагина в PATH." 
        fails+=("generate-diff")
    fi
}

go_group() {
    run "go build" go build ./...
    run "go vet" go vet ./...
    run "go test -short" go test ./... -short -count=1
    # Провайдер — ОТДЕЛЬНЫЙ модуль: корневой ./... в него не спускается, и его
    # отказ виден только собственной командой.
    run "провайдер: build" bash -c "cd '$ROOT/terraform' && go build ./..."
    run "провайдер: vet" bash -c "cd '$ROOT/terraform' && go vet ./..."
    run "провайдер: test" bash -c "cd '$ROOT/terraform' && go test ./... -count=1"

    if command -v golangci-lint > /dev/null; then
        # Кэш линтера привязывается к КОРНЮ ЭТОЙ копии. Общий кэш между рабочими
        # копиями отравляет результат: линтер отдаёт вердикт, посчитанный по
        # чужому дереву, и «зелено» перестаёт что-либо значить.
        mkdir -p "$ROOT/.cache/golangci-lint"
        GOLANGCI_LINT_CACHE=$PWD/.cache/golangci-lint run "golangci-lint" golangci-lint run
        GOLANGCI_LINT_CACHE=$PWD/.cache/golangci-lint run "провайдер: golangci-lint" bash -c "cd terraform && golangci-lint run"
    else
        echo -e "\n== golangci-lint\n   ПРОПУСК: не установлен — проверка НЕ выполнена"
    fi

    # gosec запускается В ТОЙ ЖЕ ФОРМЕ, что в конвейере: без -exclude-dir=pkg/api
    # он даёт ложные срабатывания на сгенерённых константах, и локальный прогон
    # разошёлся бы с конвейером — то есть перестал бы о нём что-либо говорить.
    #
    # Отдельно: gosec читает `#nosec`, а НЕ `//nolint:gosec`. Второе здесь не
    # подавляет ничего, и снятие такого комментария открывает настоящую находку.
    if command -v gosec > /dev/null; then
        run "gosec (форма конвейера)" gosec -exclude-dir=pkg/api -quiet ./...
    else
        echo -e "\n== gosec\n   ПРОПУСК: не установлен — проверка НЕ выполнена"
    fi
}

helm_group() {
    if ! command -v helm > /dev/null; then
        echo -e "\n== helm\n   ПРОПУСК: не установлен — проверки НЕ выполнены"; return
    fi
    local c
    for c in "$ROOT"/services/*/deploy "$ROOT"/gateway/deploy "$ROOT"/deploy/helm/umbrella; do
        [ -f "$c/Chart.yaml" ] || continue
        run "helm lint $(basename "$(dirname "$c")")" helm lint "$c"
    done
    # Манифест-проверки посадки — здесь нас ловило чаще всего: они читают ДЕРЕВО
    # (классификацию отказов, круг отправителей, привязку пода к настройкам), а не
    # только рендер, поэтому краснеют на правках скриптов и профилей.
    if [ -f "$ROOT/deploy/Makefile" ]; then
        run "манифест-проверки посадки (deploy/tests/helm)" bash -c "cd '$ROOT/deploy' && make helm-manifest-test"
    fi
}

ui_group() {
    local m
    for m in "$ROOT"/ui-future/*/package.json; do
        local dir; dir="$(dirname "$m")"
        [ -d "$dir/node_modules" ] || { echo -e "\n== ui $(basename "$dir")\n   ПРОПУСК: зависимости не установлены (npm ci --prefix) — НЕ выполнено"; continue; }
        # Прогон проб НЕ заменяет сборку: сборка гоняет строгую проверку типов и
        # роняет то, что пробы пропускают (неиспользованный импорт — реальный
        # случай). Поэтому обе, и в этом порядке.
        run "ui $(basename "$dir"): typecheck" bash -c "cd '$dir' && npm run typecheck"
        run "ui $(basename "$dir"): test" bash -c "cd '$dir' && npm test"
        run "ui $(basename "$dir"): build" bash -c "cd '$dir' && npm run build"
    done
}

case "$GROUP" in
    proto) proto_group ;;
    go)    go_group ;;
    helm)  helm_group ;;
    ui)    ui_group ;;
    all)   proto_group; go_group; helm_group; ui_group ;;
    # Несколько групп через пробел: хук отправки гоняет быстрые, но не медленные.
    *)
        ok=1
        for g in $GROUP; do
            case "$g" in
                proto) proto_group ;;
                go)    go_group ;;
                helm)  helm_group ;;
                ui)    ui_group ;;
                *) echo "неизвестная группа: $g (proto|go|helm|ui|all)" >&2; ok=0 ;;
            esac
        done
        [ "$ok" = "1" ] || exit 2
        ;;
esac

printf '\n== итог: проверок исполнено %d, отказов %d\n' "$ran" "${#fails[@]}"
if [ "${#fails[@]}" -gt 0 ]; then
    printf '   красное: %s\n' "${fails[*]}"
    exit 1
fi
echo "   Здесь НЕ проверялись: сквозные прогоны на поднятом стенде, интеграция с"
echo "   контейнерами (go test без -short), доказательства посадки. «Локально"
echo "   зелено» не означает «конвейер зелёный» — оно означает ровно исполненное выше."
