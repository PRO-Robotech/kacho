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
#   scripts/ci-local.sh go|terraform|helm|ui  # отдельная группа
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GROUP="${1:-all}"
WORK="${CI_LOCAL_WORK:-${TMPDIR:-/tmp}/kacho-ci-local}"
mkdir -p "$WORK"
cd "$ROOT"

# НЕПРОВЯЗАННЫЙ КЛОН НАЗЫВАЕТСЯ ЗДЕСЬ (#462). Этот прогонщик исполняется хуком
# отправки — и ровно поэтому его запуск РУКАМИ есть признак того, что хука может
# не быть: провязанный клон позвал бы его сам. Напоминание молчит, когда провязка
# есть, и ничего не роняет: предмет прогонщика — код, а не настройка клона.
#
# Почему это не дублирует `hooks-notice` на целях `make test*`: те закрывают
# того, кто гоняет пробы через make. Прямой `scripts/ci-local.sh` их не касается,
# а он-то как раз и стоит непосредственно перед отправкой.
[ -n "${CI:-}" ] || bash "$ROOT/scripts/hooks/install.sh" notice || true

fails=(); ran=0

run() { # run <имя> <команда...>
    local name="$1"; shift
    ran=$((ran + 1))
    printf '\n== %s\n' "$name"

    # Журнал у КАЖДОЙ проверки свой и остаётся после прогона. Прежде все они писали в
    # один файл, и он доставался следующей: к моменту, когда читаешь отказ, разбирать
    # уже нечего — журнал перезаписан соседом. Плюс на экран идёт только хвост, а
    # инструменты, печатающие координату ПЕРВОЙ строкой и длинную сводку после (tofu
    # test — ровно такой), теряют в этом хвосте имя падения: вердикт остаётся, разбор
    # невозможен. Поэтому путь к целому журналу называется в самом отказе.
    # Порядковый номер в имени — потому что имена проверок русские, а `tr` режет по
    # БАЙТАМ: от многобайтного имени в слаге остаётся вереница дефисов, и два разных
    # отказа получили бы один файл.
    local log slug
    slug="$(printf '%s' "$name" | tr -c 'A-Za-z0-9' '-' | tr -s '-' | sed 's/^-//;s/-$//' | cut -c1-30)"
    log="$WORK/$(printf 'log-%02d-%s.txt' "$ran" "$slug")"

    if "$@" > "$log" 2>&1; then
        echo "   ok"
    else
        echo "   ОТКАЗ (код $?)"
        tail -15 "$log" | sed 's/^/   | /'
        echo "   | целиком: $log"
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
    #
    # БАЗА СРАВНЕНИЯ — `origin/main`, А НЕ ЛОКАЛЬНАЯ ВЕТКА `main`.
    #
    # Конвейер сравнивает с `main` НА GITHUB. Локальная ветка `main` в общем клоне
    # живёт своей жизнью: её никто не обязан обновлять, и она отстаёт ровно настолько,
    # насколько давно тут не делали checkout. Сравнение с ней даёт вердикт о другом
    # дереве, причём ошибается в ОБЕ стороны сразу.
    #
    # Замер 2026-08-14, локальная `main` отставала на 78 коммитов: против неё — 29
    # находок и «четыре необъявленных разрыва», по которым была заведена задача;
    # против `origin/main` — 0 находок и 21 истёкшая запись перечня, то есть ровно то,
    # на чём покраснел бы MR в ствол. Отставшая база показывает разрывы, которых нет,
    # и одновременно ПРЯЧЕТ истёкшие послабления: перечень выглядит действующим,
    # потому что сопоставляется с находками, которых в стволе давно нет.
    run "adjudicate breaking (перечень объявленных разрывов)" bash -c '
        set -uo pipefail
        go build -o "'"$WORK"'/adjudicate" ./tools/declaredbreak/cmd/adjudicate-declared-breaks
        cd "'"$ROOT"'/proto"

        # Свежесть базы — предпосылка вердикта, поэтому она проверяется, а не
        # предполагается. Молчаливый откат на локальную ветку запрещён: он вернул бы
        # ровно тот вердикт о другом дереве, ради которого написан этот абзац.
        if git -C "'"$ROOT"'" rev-parse --verify --quiet origin/main >/dev/null; then
            against="'"$ROOT"'/.git#ref=origin/main,subdir=proto"
            behind=$(git -C "'"$ROOT"'" rev-list --count origin/main..main 2>/dev/null || echo 0)
            ahead=$(git -C "'"$ROOT"'" rev-list --count main..origin/main 2>/dev/null || echo 0)
            [ "${ahead:-0}" -gt 0 ] && echo "   (локальная main отстаёт от origin/main на ${ahead} коммитов — сравниваю с origin/main, как конвейер)"
        else
            echo "   ОТКАЗ: origin/main не разрешается — базы сравнения нет." >&2
            echo "   Локальная main ею НЕ является: вердикт был бы о другом дереве." >&2
            echo "   Почините: git fetch origin" >&2
            exit 2
        fi

        set +e
        buf breaking --against "$against" --error-format=json > "'"$WORK"'/buf-breaking.jsonl" 2>"'"$WORK"'/buf-breaking.err"
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
    # Здесь стояли ещё три «провайдер: …» — те же build/vet/test, вызванные из
    # terraform/, — и объяснялись тем, что провайдер живёт ОТДЕЛЬНЫМ модулем, куда
    # корневой ./... не спускается. Модуль в дереве один (предикат: `git ls-files
    # '*go.mod'` → одна строка), поэтому команды выше провайдера уже видят, а те три
    # были повтором, оправданным утверждением, пережившим свой предмет.
    #
    # Цикл terraform ими всё равно не исполнялся: он в группе terraform ниже.

    if command -v golangci-lint > /dev/null; then
        # Кэш линтера привязывается к КОРНЮ ЭТОЙ копии. Общий кэш между рабочими
        # копиями отравляет результат: линтер отдаёт вердикт, посчитанный по
        # чужому дереву, и «зелено» перестаёт что-либо значить.
        mkdir -p "$ROOT/.cache/golangci-lint"
        GOLANGCI_LINT_CACHE=$PWD/.cache/golangci-lint run "golangci-lint" golangci-lint run
    else
        echo -e "\n== golangci-lint\n   ПРОПУСК: не установлен — проверка НЕ выполнена"
    fi

    # gosec запускается В ТОЙ ЖЕ ФОРМЕ, что в конвейере, и «та же форма» — это ТРИ
    # вещи разом, а не одна. Прежняя редакция совпадала с конвейером по флагу
    # исключения и расходилась по двум другим, утверждая при этом тождество.
    #
    # 1. ВЕРСИЯ. Конвейер ставит пин (`security-scan.yml`, шаг «install gosec»);
    #    прежняя редакция брала то, что лежит в PATH. Замер 2026-08-14: в PATH был
    #    2.22.10 против пина v2.28.0, и правило G115 у них срабатывает по-разному.
    # 2. ПРЕДИКАТ. Конвейер гейтит по SARIF `level == "error"`; код возврата gosec
    #    ненулевой при находке ЛЮБОГО уровня. Что эти предикаты расходятся, записано
    #    в самом `security-scan.yml`: «локальная проверка „severity HIGH“ показывала
    #    0, пока CI честно краснел». Здесь тот же класс в обратную сторону.
    # 3. ИСКЛЮЧЕНИЕ КАТАЛОГА — единственное, что совпадало и раньше.
    #
    # Цена расхождения измерена на объединённой волне: по коду возврата — шесть
    # находок и красное, по предикату конвейера — ноль ошибок при семи результатах
    # ниже порога. Ложное красное учит игнорировать локальный прогон, а он —
    # единственное, что стоит между правкой и стволом: PR внутрь релизной ветки
    # конвейером не проверяется.
    #
    # Отдельно: gosec читает `#nosec`, а НЕ `//nolint:gosec`. Второе здесь не
    # подавляет ничего, и снятие такого комментария открывает настоящую находку.
    local gosec_pin gosec_bin
    gosec_pin=$(grep -oE 'gosec/v2/cmd/gosec@v[0-9.]+' "$ROOT/.github/workflows/security-scan.yml" 2>/dev/null | head -1)
    if ! command -v jq > /dev/null; then
        # Без jq предикат конвейера не вычислить, а откатываться на код возврата
        # нельзя: он отвечает на другой вопрос и даёт ложное красное. Пропуск
        # называется пропуском — «не выполнено» не есть «находок нет».
        echo -e "\n== gosec\n   ПРОПУСК: нет jq, предикат конвейера (SARIF level=error) не вычислить."
        echo "   Проверка НЕ выполнена. Поставьте jq — откат на код возврата дал бы"
        echo "   красное на находках ниже порога, то есть вердикт о другом вопросе."
    elif [ -z "$gosec_pin" ]; then
        echo -e "\n== gosec\n   ОТКАЗ: пин версии не найден в security-scan.yml —"
        echo "   без него локальный прогон судил бы другой версией, чем конвейер"
        fails+=("gosec: пин не найден")
    else
        gosec_bin="$WORK/gosec-bin"
        printf '\n== gosec (%s, как в конвейере)\n' "${gosec_pin##*@}"
        if GOBIN="$gosec_bin" go install "github.com/securego/${gosec_pin}" > "$WORK/gosec-install.txt" 2>&1; then
            ran=$((ran + 1))
            # Гейт — по SARIF level=error, тем же jq-предикатом, что в конвейере.
            # Код возврата gosec здесь НЕ вердикт: он ненулевой и при находках
            # ниже порога, то есть отвечает на другой вопрос.
            "$gosec_bin/gosec" -exclude-dir=pkg/api -fmt sarif -out "$WORK/gosec.sarif" ./... \
                > "$WORK/gosec-run.txt" 2>&1 || true
            if [ ! -s "$WORK/gosec.sarif" ]; then
                echo "   ОТКАЗ: отчёта нет — сканер не дошёл до вердикта, и это НЕ «находок нет»"
                tail -5 "$WORK/gosec-run.txt" | sed 's/^/   | /'
                fails+=("gosec: отчёт не создан")
            else
                local errs total
                errs=$(jq '[.runs[].results[]|select(.level=="error")]|length' "$WORK/gosec.sarif")
                total=$(jq '[.runs[].results[]]|length' "$WORK/gosec.sarif")
                # Перепись — отдельное утверждение: «ноль ошибок» обязано быть
                # отличимо от «ноль прочитанного».
                echo "   осмотрено результатов ${total}; из них level=error: ${errs}"
                if [ "$errs" -eq 0 ]; then
                    echo "   ok"
                else
                    jq -r '.runs[].results[]|select(.level=="error")|"   | \(.ruleId) \(.locations[0].physicalLocation.artifactLocation.uri):\(.locations[0].physicalLocation.region.startLine)"' "$WORK/gosec.sarif"
                    fails+=("gosec (level=error)")
                fi
            fi
        else
            echo "   ОТКАЗ: пиннутый gosec не поставился — проверка НЕ выполнена"
            tail -5 "$WORK/gosec-install.txt" | sed 's/^/   | /'
            fails+=("gosec: установка")
        fi
    fi
}

# ── terraform: конфигурации и приёмка провайдера ────────────────────────────
#
# ЧТО ЭТО ЗАКРЫВАЕТ. Файлы .tf не компилируются ничем: `go build` их не читает, и
# опечатка в связывании ресурсов доезжает до первого apply у пользователя. Go-сторона
# провайдера, наоборот, покрыта группой go выше — но она молчит о том, как ресурс ведёт
# себя под НАСТОЯЩИМ terraform: план, повторный план, пересоздание, импорт, уборка.
#
# ПОЧЕМУ СВОЙ ЭКЗЕМПЛЯР tofu, А НЕ ИЗ PATH. Вердикт имеет смысл, только если инструмент
# и версия те же, что в конвейере. Чужой tofu из PATH дал бы вердикт о другом
# инструменте — и разошёлся бы с конвейером молча, потому что на зелёном входе оба
# отвечают «зелено». Версия ниже — тот же пин, что у конвейера, и за их расхождением
# следит .github/scripts/check-pinned-tools.sh.
TOFU_VERSION=1.12.5

terraform_group() {
    local bin="$WORK/tofu-bin" tofu
    tofu="$bin/tofu"

    if [ ! -x "$tofu" ] || ! "$tofu" version 2>/dev/null | head -1 | grep -qF "v$TOFU_VERSION"; then
        local os arch
        case "$(uname -s)" in
            Linux)  os=linux ;;
            Darwin) os=darwin ;;
            *) echo -e "\n== tofu\n   ПРОПУСК: $(uname -s) не поддержан этим прогоном — проверки НЕ выполнены"; return ;;
        esac
        case "$(uname -m)" in
            x86_64|amd64)  arch=amd64 ;;
            aarch64|arm64) arch=arm64 ;;
            *) echo -e "\n== tofu\n   ПРОПУСК: $(uname -m) не поддержан этим прогоном — проверки НЕ выполнены"; return ;;
        esac

        printf '\n== tofu %s (%s_%s) — тот же артефакт, что ставит конвейер\n' "$TOFU_VERSION" "$os" "$arch"
        mkdir -p "$bin"
        if ! curl -fsSL -o "$WORK/tofu.zip" \
            "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_${os}_${arch}.zip" > "$WORK/tofu-install.txt" 2>&1 \
            || ! unzip -oq "$WORK/tofu.zip" -d "$bin" >> "$WORK/tofu-install.txt" 2>&1; then
            echo "   ОТКАЗ: tofu $TOFU_VERSION не поставился — НИ ОДНА проверка конфигураций НЕ выполнена"
            tail -8 "$WORK/tofu-install.txt" | sed 's/^/   | /'
            fails+=("tofu не поставился"); return
        fi
        echo "   ok"
    fi
    # Свой экземпляр — ПЕРВЫМ: он же достаётся приёмочным пробам, которые ищут
    # исполнителя в PATH.
    export PATH="$bin:$PATH"

    # Пробы гоняются против ТОГО ЖЕ провайдера, что собирается из этого дерева, а не
    # против опубликованного: иначе они судили бы чужую версию.
    mkdir -p "$WORK/tf-plugins"
    if ! go build -o "$WORK/tf-plugins/terraform-provider-kacho" \
        ./terraform/cmd/terraform-provider-kacho > "$WORK/tofu-install.txt" 2>&1; then
        echo -e "\n== провайдер для tofu\n   ОТКАЗ: не собрался — проверки конфигураций НЕ выполнены"
        tail -8 "$WORK/tofu-install.txt" | sed 's/^/   | /'
        fails+=("провайдер для tofu"); return
    fi
    cat > "$WORK/tofu.tfrc" <<RC
provider_installation {
  dev_overrides { "PRO-Robotech/kacho" = "$WORK/tf-plugins" }
  direct {}
}
RC
    export TF_CLI_CONFIG_FILE="$WORK/tofu.tfrc"

    run "формат .tf" tofu fmt -check -recursive terraform
    run "модульные пробы (tofu test)" tofu_modules
    run "примеры разбираются и сходятся по типам" tofu_examples
    # Приёмка провайдера: полный цикл terraform против поддельного края в том же
    # процессе. Исполнителя пробы находят сами — он выше положен в PATH.
    run "приёмка провайдера (цикл terraform)" go test ./terraform/internal/provider -run Acceptance -count=1
}

# Перечень модулей ВЫВОДИТСЯ из дерева, а не выписывается: рукописный список разошёлся
# бы с деревом молча, и новый модуль приехал бы непроверенным. Ноль модулей — ОТКАЗ, а
# не успех: обходчику, которому нечего обходить, положено быть отличимым от того, у кого
# всё сошлось. Печатается объём осмотренного.
tofu_modules() {
    local modules m suites seen=0
    modules=$(git ls-files 'terraform/modules/*/main.tf' | xargs -r -n1 dirname | sort -u)
    if [ -z "$modules" ]; then
        echo "модулей terraform не найдено — предикат поиска устарел или каталог переехал" >&2
        return 1
    fi
    for m in $modules; do
        suites=$(git ls-files "$m/tests/*.tftest.hcl" | wc -l)
        if [ "$suites" -eq 0 ]; then
            echo "$m/main.tf: модуль без модульных проб — заведите $m/tests/*.tftest.hcl" >&2
            return 1
        fi
        echo "── $m ($suites сюит)"
        ( cd "$m" && tofu test -no-color ) || return 1
        seen=$((seen + 1))
    done
    echo "модулей осмотрено: $seen"
}

# Примеры — то, что читатель копирует первым. Непроверяемый пример устаревает тише
# всего: он не собирается ничем и не падает нигде.
tofu_examples() {
    local examples e seen=0
    examples=$(git ls-files 'terraform/examples/*/main.tf' | xargs -r -n1 dirname | sort -u)
    if [ -z "$examples" ]; then
        echo "примеров terraform не найдено — предикат поиска устарел" >&2
        return 1
    fi
    for e in $examples; do
        # `tofu get`, а НЕ `tofu init`: провайдер здесь приходит подменой пути
        # (dev_overrides), и init на нём выходит с кодом 1, потому что честно не находит
        # его в реестре. Глушить этот код было бы мягким проходом, не отличающим
        # настройку от сбоя; `get` ставит только модули и таким вопросом не задаётся.
        ( cd "$e" && tofu get -no-color && tofu validate -no-color ) || return 1
        seen=$((seen + 1))
    done
    echo "примеров осмотрено: $seen"
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
    proto)     proto_group ;;
    go)        go_group ;;
    terraform) terraform_group ;;
    helm)      helm_group ;;
    ui)        ui_group ;;
    all)       proto_group; go_group; terraform_group; helm_group; ui_group ;;
    # Несколько групп через пробел: хук отправки гоняет быстрые, но не медленные.
    *)
        ok=1
        for g in $GROUP; do
            case "$g" in
                proto)     proto_group ;;
                go)        go_group ;;
                terraform) terraform_group ;;
                helm)      helm_group ;;
                ui)        ui_group ;;
                *) echo "неизвестная группа: $g (proto|go|terraform|helm|ui|all)" >&2; ok=0 ;;
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
