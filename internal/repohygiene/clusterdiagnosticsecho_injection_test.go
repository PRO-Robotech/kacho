// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clusterdiagnosticsecho_injection_test.go — доказательство, что гейт #728
// СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Гейт, не доказавший обоих умений, доказательством не является: молчащий на
// дефекте бесполезен, а кричащий на законной конструкции будет снят первым же,
// кому он помешает. Поэтому обе стороны проверяются на синтетическом
// содержимом, а не на дереве, — иначе проба умрёт вместе с починкой дерева и
// унесёт доказательство с собой.
//
// Законных близнецов ПЯТЬ, и все они РАЗНЫЕ конструкции, а не копии одной:
// уборка кластера, оффлайновый вызов инструмента, чтение без функции состояния,
// диагностика со стражем через `||` и диагностика со стражем в условии `if !`.
// Копия прежнего близнеца близнецом не является: она подтверждает ровно то, что
// уже подтверждено, и оставляет непроверенной ту форму, ради которой её пишут.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────── ДЕФЕКТЫ ────────────────────────────────────

// Ровно то, что наблюдалось: диагностика под `always()` без привязки, без стража
// и с чтением, которое становится кодом возврата шага.
const injDiagBareEcho = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        run: make dev-up
      - name: что поднялось
        if: always()
        run: kubectl -n kacho get pods -o wide
`

// Привязка есть, страж есть — но чтение всё ещё выносит вердикт.
const injDiagFatalRead = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        id: stand
        run: make dev-up
      - name: что поднялось
        if: ${{ always() && steps.stand.outcome == 'success' }}
        run: |
          "$GITHUB_WORKSPACE/.github/scripts/stand-present-or-explain.sh" "стенд" || exit 0
          kubectl -n kacho get pods -o wide
`

// Привязка есть, вердикта нет — но стража нет: шаг МОЛЧИТ, и причина ищется в
// продукте. Тихая форма того же класса.
const injDiagNoGuard = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        id: stand
        run: make dev-up
      - name: журналы
        if: ${{ always() && steps.stand.outcome == 'failure' }}
        run: |
          kubectl -n kacho logs deploy/vpc > vpc.log 2>&1 || echo "(журнал недоступен)" > vpc.log
`

// Страж и гашение есть — привязки нет.
const injDiagNoBinding = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        id: stand
        run: make dev-up
      - name: что поднялось
        if: always()
        run: |
          "$GITHUB_WORKSPACE/.github/scripts/stand-present-or-explain.sh" "стенд" || exit 0
          kubectl -n kacho get pods -o wide || true
`

// Страж зовётся ПОСЛЕ первого чтения: объяснять уже нечего.
const injDiagGuardAfterRead = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        id: stand
        run: make dev-up
      - name: что поднялось
        if: ${{ always() && steps.stand.outcome == 'success' }}
        run: |
          kubectl -n kacho get pods -o wide || true
          "$GITHUB_WORKSPACE/.github/scripts/stand-present-or-explain.sh" "стенд" || exit 0
`

// Глагол вне словаря: у гейта нет корзины «прочее», и он это говорит.
const injDiagUnknownVerb = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        id: stand
        run: make dev-up
      - name: что-то новое
        if: ${{ always() && steps.stand.outcome == 'success' }}
        run: kubectl -n kacho debug node/x || true
`

// Страж позван, но в форме, которая роняет шаг САМА: его собственный код
// возврата под `bash -e` и есть тот второй красный, от которого уходили.
const injDiagGuardUnguarded = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        id: stand
        run: make dev-up
      - name: что поднялось
        if: ${{ always() && steps.stand.outcome == 'success' }}
        run: |
          "$GITHUB_WORKSPACE/.github/scripts/stand-present-or-explain.sh" "стенд"
          kubectl -n kacho get pods -o wide || true
`

// ─────────────────────────── ЗАКОННЫЕ БЛИЗНЕЦЫ ──────────────────────────────

// (1) Уборка. Снос кластера обязан идти ВСЕГДА — привязка к исходу подъёма
// отняла бы у него право освободить ранер после обрыва, а отчитываться ему не о
// чем. Не предмет правила by construction.
const injDiagTeardown = `
name: проба
jobs:
  probes:
    steps:
      - name: снести стенд
        if: always()
        run: kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true
`

// (2) Оффлайн. `helm template` кластера не касается вовсе.
const injDiagOffline = `
name: проба
jobs:
  probes:
    steps:
      - name: рендер чартов
        if: always()
        run: helm template ci . > /dev/null
`

// (3) Чтение БЕЗ функции состояния: такой шаг пропустится сам, если
// предшественник упал, — эха не бывает by construction.
const injDiagNoStateFunc = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        run: make dev-up
      - name: что поднялось
        run: kubectl -n kacho get pods -o wide
`

// (4) Канон: привязка, страж через `||`, гашение кода возврата.
const injDiagCanonical = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        id: stand
        run: make dev-up
      - name: что поднялось
        if: ${{ always() && (steps.stand.outcome == 'success' || steps.stand.outcome == 'failure') }}
        run: |
          "$GITHUB_WORKSPACE/.github/scripts/stand-present-or-explain.sh" "стенд" || exit 0
          kubectl -n kacho get pods -o wide || true
`

// (5) Канон ДРУГОЙ формы: страж в условии `if !`, чтения в цикле с запасным
// путём через `|| echo`, и точка с запятой внутри программы awk — та самая, на
// которой наивная нарезка сегментов объявляла бы находкой верный код.
const injDiagCanonicalCondition = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        id: stand
        run: make dev-up
      - name: журналы
        if: ${{ always() && (steps.stand.outcome == 'success' || steps.stand.outcome == 'failure') }}
        run: |
          mkdir -p stand-logs
          if ! "$GITHUB_WORKSPACE/.github/scripts/stand-present-or-explain.sh" "стенд" > stand-logs/no-cluster.txt 2>&1; then
            cat stand-logs/no-cluster.txt
            exit 0
          fi
          for d in vpc iam; do
            kubectl -n kacho logs "deploy/$d" > "stand-logs/$d.log" 2>&1 || echo "(журнал недоступен)" > "stand-logs/$d.log"
          done
          ready=$(kubectl -n kacho get pods --no-headers 2>/dev/null | awk '{split($2,a,"/"); if (a[1]==a[2]) n++} END{print n+0}') || true
          echo "готовых $ready"
`

// (6) Упоминание в комментарии и в строке — не вызов. Гейт читает исполняемое.
// Здесь мимо гейта обязано пройти ВСЁ: и «kubectl get pods» в комментарии, и он
// же внутри строки, и `kacho#655` — решётка внутри кавычек, на которой наивная
// вырезка комментариев унесла бы половину исполняемого текста.
const injDiagMentionOnly = `
name: проба
jobs:
  probes:
    steps:
      - name: сообщение
        if: always()
        run: |
          # тут раньше стоял kubectl get pods -o wide, и он краснел эхом
          echo "::error::подробности — kubectl get pods; предмет kacho#655"
`

// ──────────────────────────────── ПРОБЫ ─────────────────────────────────────

func TestClusterDiagnosticsGateRedsOnTheDefect(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string // фрагмент, по которому узнаётся ИМЕННО это требование
	}{
		{"без привязки, без стража, чтение выносит вердикт", injDiagBareEcho, "ни к какому исходу шага НЕ привязан"},
		{"чтение выносит вердикт", injDiagFatalRead, "выносит вердикт"},
		{"стража нет — шаг молчит", injDiagNoGuard, "не зовёт общий страж"},
		{"привязки нет", injDiagNoBinding, "ни к какому исходу шага НЕ привязан"},
		{"страж после первого чтения", injDiagGuardAfterRead, "ПОСЛЕ первого чтения"},
		{"глагол вне словаря", injDiagUnknownVerb, "не отнесён ни к чтению кластера"},
		{"страж роняет шаг сам", injDiagGuardUnguarded, "не зовёт общий страж"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, census := checkClusterDiagnosticsEcho("проба.yml", tc.yaml)
			if len(findings) == 0 {
				t.Fatalf("гейт СМОЛЧАЛ на дефекте (перепись: шагов %d, читающих %d, предмет %d)",
					census.Steps, census.Reading, census.Subject)
			}
			joined := strings.Join(findings, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("гейт покраснел, но не на том требовании: ждали фрагмент %q, получили:\n%s", tc.want, joined)
			}
			// Координата обязана быть названа: находка без адреса не чинится.
			if !strings.Contains(joined, "проба.yml") || !strings.Contains(joined, "шаг #") {
				t.Errorf("находка без координаты:\n%s", joined)
			}
		})
	}
}

func TestClusterDiagnosticsGateStaysQuietOnLawfulShapes(t *testing.T) {
	cases := []struct {
		name        string
		yaml        string
		wantSubject int // сколько шагов гейт обязан признать своим предметом
	}{
		{"уборка кластера", injDiagTeardown, 0},
		{"оффлайновый вызов инструмента", injDiagOffline, 0},
		{"чтение без функции состояния", injDiagNoStateFunc, 0},
		{"канон: страж через ||", injDiagCanonical, 1},
		{"канон: страж в условии, чтения в цикле", injDiagCanonicalCondition, 1},
		{"упоминание в комментарии и в строке", injDiagMentionOnly, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, census := checkClusterDiagnosticsEcho("проба.yml", tc.yaml)
			if len(findings) != 0 {
				t.Errorf("гейт покраснел на ЗАКОННОЙ конструкции:\n%s", strings.Join(findings, "\n"))
			}
			// Молчание обязано быть осмысленным: гейт либо признал шаг своим
			// предметом и не нашёл в нём изъяна, либо честно признал, что предмета
			// нет. Без этой сверки «тихо» и «не прочитано» неотличимы.
			if census.Subject != tc.wantSubject {
				t.Errorf("предметом признано %d шаг(ов), ожидалось %d — гейт молчит не по той причине",
					census.Subject, tc.wantSubject)
			}
		})
	}
}

// TestClusterDiagnosticsCensusSeparatesQuietFromUnread — «ноль находок» обязано
// быть отличимо от «ноль прочитанного». Проба закрепляет ИМЕННО это: на корпусе
// без предмета перепись даёт ноль, и тогда обход по дереву обязан упасть, а не
// зазеленеть.
func TestClusterDiagnosticsCensusSeparatesQuietFromUnread(t *testing.T) {
	findings, census := checkClusterDiagnosticsEcho("пусто.yml", "name: пусто\njobs: {}\n")
	if len(findings) != 0 {
		t.Errorf("на пустом корпусе гейт нашёл: %v", findings)
	}
	if census.Files != 1 || census.Steps != 0 || census.Subject != 0 {
		t.Errorf("перепись пустого корпуса: файлов %d, шагов %d, предмет %d — ожидались 1/0/0",
			census.Files, census.Steps, census.Subject)
	}

	// И обратное: у канона перепись НЕ нулевая, то есть обход действительно
	// читает шаги, а не выходит на первом же ветвлении.
	_, live := checkClusterDiagnosticsEcho("канон.yml", injDiagCanonical)
	if live.Steps != 2 || live.Reading != 1 || live.Subject != 1 {
		t.Errorf("перепись канона: шагов %d, читающих %d, предмет %d — ожидались 2/1/1",
			live.Steps, live.Reading, live.Subject)
	}
}

// TestClusterDiagnosticsGateRejectsUnparsableWorkflow — файл, который не
// разобрался, обязан стать НАХОДКОЙ, а не молча выпасть из обхода: иначе первая
// же опечатка в YAML выводит конвейер из-под правила без единого слова.
func TestClusterDiagnosticsGateRejectsUnparsableWorkflow(t *testing.T) {
	findings, _ := checkClusterDiagnosticsEcho("битый.yml", "jobs: [ не yaml\n  - совсем")
	if len(findings) == 0 {
		t.Fatal("неразбираемый YAML прошёл молча — файл выпал из обхода незамеченным")
	}
	if !strings.Contains(strings.Join(findings, "\n"), "НЕ проверен") {
		t.Errorf("находка не говорит, что файл не проверен: %v", findings)
	}
}
