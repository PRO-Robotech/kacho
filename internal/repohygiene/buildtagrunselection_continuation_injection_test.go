// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// buildtagrunselection_continuation_injection_test.go — вызов `go test`,
// записанный НЕСКОЛЬКИМИ строками, читается гейтом отбора так же, как его
// читает исполнитель (#2883).
//
// # Что было
//
// Судья читал объявление по ФИЗИЧЕСКИМ строкам. В `.github/workflows/ci.yaml`
// шаг рендера умбреллы записан свёрнутым скаляром `run: >-`: признак и пакет
// стоят на строке с `go test`, а `-run` — строкой ниже. YAML сворачивает эти
// строки в ОДНУ команду, и `go test` получает сужение; судья же видел строку
// без `-run`, читал отбор как «все» и печатал «из них отбирается 30» при
// тридцати пробах, пока две пробы тега, заведённые в #2778, не исполнялись ни
// разу. Третья категория исхода, поданная как покрытие.
//
// # Законные формы многострочной записи — все, а не одна
//
// Форма, о которой распознаватель не знает, делает сужение НЕВИДИМЫМ
// (`testing.md` recognizer-knows-all-lawful-forms), поэтому каждая проверяется
// своим прогоном: свёрнутые скаляры обоих видов, буквальный с переносом
// обратной косой, многострочные простой и кавычечные скаляры, рецепт Makefile
// и скрипт с переносом, а также признак и пакет, стоящие на строке переноса.
//
// # Дефект и законный близнец отличаются ОДНИМ фактом
//
// У каждой формы два прогона на одном и том же тексте объявления: сужение
// называет одно имя из двух (находка обязана назвать вторую пробу) либо оба
// (молчание). Меняется только значение `-run`.
//
// # Флаг принадлежит своей программе
//
// Чтение продолжения обязано кончаться там, где кончается команда: `-run`
// после конвейера, `;`, `&&` или комментария принадлежит ДРУГОЙ программе.
// Судья, читающий хвост строки целиком, увидел бы сужение, которого `go test`
// не применяет, — и краснел бы на законном вызове либо, хуже, зеленел бы на
// чужом шире-отборе. Эти прогоны — законные близнецы: вызов берёт всё, гейт
// молчит.
package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

// continuationForm — одна законная форма многострочной записи вызова.
//
// decl — текст объявления с `%s` на месте значения `-run`; значение
// подставляется уже в кавычках нужного вида (quote).
type continuationForm struct {
	name    string
	declRel string
	decl    string
	quote   func(pattern string) string
}

func shellQuoted(p string) string { return "'" + p + "'" }

// yamlSingleQuoted — внутри скаляра в одинарных кавычках YAML кавычка
// удваивается: исполнитель получит `'TestAlpha'`.
func yamlSingleQuoted(p string) string { return "''" + p + "''" }

func continuationForms() []continuationForm {
	const wf = ".github/workflows/ci.yaml"
	const head = "jobs:\n  probe:\n    steps:\n      - name: прогон\n"
	return []continuationForm{
		{"YAML: свёрнутый скаляр >- (форма шага рендера умбреллы)", wf, head +
			"        run: >-\n" +
			"          go test -tags=synthtag ./deploy/\n" +
			"          -run %s\n" +
			"          -count=1 -v\n", shellQuoted},
		{"YAML: свёрнутый скаляр >", wf, head +
			"        run: >\n" +
			"          go test -tags=synthtag ./deploy/\n" +
			"          -run %s -count=1\n", shellQuoted},
		{"YAML: буквальный скаляр с переносом обратной косой", wf, head +
			"        run: |\n" +
			"          set -euo pipefail\n" +
			"          go test -tags=synthtag ./deploy/ \\\n" +
			"            -run %s \\\n" +
			"            -count=1\n", shellQuoted},
		{"YAML: многострочный простой скаляр", wf, head +
			"        run: go test -tags=synthtag ./deploy/\n" +
			"          -run %s -count=1\n", shellQuoted},
		{"YAML: многострочный скаляр в двойных кавычках", wf, head +
			"        run: \"go test -tags=synthtag ./deploy/\n" +
			"          -run %s -count=1\"\n", shellQuoted},
		{"YAML: многострочный скаляр в одинарных кавычках", wf, head +
			"        run: 'go test -tags=synthtag ./deploy/\n" +
			"          -run %s -count=1'\n", yamlSingleQuoted},
		{"YAML: выражение с || перед сужением не кончает команду", wf, head +
			"        run: >-\n" +
			"          go test -tags=synthtag ./deploy/ -count=${{ inputs.count || 1 }}\n" +
			"          -run %s\n", shellQuoted},
		{"Makefile: рецепт с переносом", "Makefile",
			"test-synth:\n" +
				"\t$(GO) test -tags=synthtag ./deploy/ \\\n" +
				"\t  -run %s \\\n" +
				"\t  -count=1\n", shellQuoted},
		{"скрипт: перенос обратной косой", "scripts/synth.sh",
			"#!/usr/bin/env bash\nset -euo pipefail\n" +
				"go test -tags=synthtag ./deploy/ \\\n" +
				"  -run %s \\\n" +
				"  -count=1\n", shellQuoted},
		{"скрипт: признак на строке переноса", "scripts/synth.sh",
			"#!/usr/bin/env bash\nset -euo pipefail\n" +
				"go test ./deploy/ \\\n" +
				"  -tags=synthtag -run %s\n", shellQuoted},
		{"скрипт: пакет на строке переноса", "scripts/synth.sh",
			"#!/usr/bin/env bash\nset -euo pipefail\n" +
				"go test -tags=synthtag \\\n" +
				"  ./deploy/ -run %s\n", shellQuoted},
		{"вызов внутри подстановки команды", "scripts/synth.sh",
			"#!/usr/bin/env bash\n" +
				"out=\"$(go test -tags=synthtag ./deploy/ \\\n" +
				"  -run %s -count=1 2>&1)\"; rc=$?\n" +
				"echo \"$out\"; exit \"$rc\"\n", shellQuoted},
	}
}

// TestSelectionGateReadsTheRunOnAContinuationLine — направление (а) по каждой
// форме: сужение на строке продолжения называет одно имя из двух — гейт
// КРАСНЕЕТ и называет пробу вне отбора.
func TestSelectionGateReadsTheRunOnAContinuationLine(t *testing.T) {
	t.Parallel()
	for _, f := range continuationForms() {
		t.Run(f.name, func(t *testing.T) {
			decl := fmt.Sprintf(f.decl, f.quote("TestAlpha"))
			root := synthSelectTreeWithDecl(t, f.declRel, decl, "deploy", synthTwoProbes)
			findings, census := auditSynthSelect(t, root)
			t.Log(census.String())

			if census.RunsFound != 1 {
				t.Fatalf("прогонов найдено %d, в объявлении вызов один — форма записи не "+
					"прочитана как вызов:\n%s\n%s", census.RunsFound, decl, census)
			}
			if len(findings) == 0 {
				t.Fatalf("сужение стоит на строке продолжения и берёт одну пробу из двух, "+
					"а гейт молчит — форма записи делает сужение НЕВИДИМЫМ:\n%s\n%s", decl, census)
			}
			joined := joinSelectionFindings(findings)
			if !strings.Contains(joined, "TestBeta") || strings.Contains(joined, "TestAlpha:") {
				t.Fatalf("находка обязана назвать ровно пробу вне отбора (TestBeta):\n%s", joined)
			}
			if !strings.Contains(joined, `-run "TestAlpha"`) {
				t.Fatalf("находка не показывает сужение из объявления — читателю нечего "+
					"сверять:\n%s", joined)
			}
			if !strings.Contains(joined, f.declRel+":") {
				t.Fatalf("находка не называет координату объявления %s:\n%s", f.declRel, joined)
			}
		})
	}
}

// TestSelectionGateSilentWhenTheContinuationNamesEveryProbe — направление (б),
// законный близнец по каждой форме: тот же текст, сужение называет ОБА имени —
// гейт МОЛЧИТ и считает отобранными обе.
func TestSelectionGateSilentWhenTheContinuationNamesEveryProbe(t *testing.T) {
	t.Parallel()
	for _, f := range continuationForms() {
		t.Run(f.name, func(t *testing.T) {
			decl := fmt.Sprintf(f.decl, f.quote("TestAlpha|TestBeta"))
			root := synthSelectTreeWithDecl(t, f.declRel, decl, "deploy", synthTwoProbes)
			findings, census := auditSynthSelect(t, root)
			t.Log(census.String())

			if census.RunsFound != 1 {
				t.Fatalf("прогонов найдено %d, ожидался 1:\n%s\n%s", census.RunsFound, decl, census)
			}
			if len(findings) != 0 {
				t.Fatalf("сужение называет обе пробы, а гейт краснеет:\n%s\n%s",
					decl, joinSelectionFindings(findings))
			}
			if census.FuncsSelected != 2 {
				t.Fatalf("отобранных проб %d, ожидалось 2: %s", census.FuncsSelected, census)
			}
			if len(census.NarrowingRuns) != 1 {
				t.Fatalf("сужающих прогонов %d, ожидался 1 — сужение прочитано не как "+
					"сужение: %s", len(census.NarrowingRuns), census)
			}
		})
	}
}

// TestSelectionGateLeavesAFlagOfAnotherProgramAlone — законные близнецы
// границы команды: `-run` стоит ПОСЛЕ конца вызова `go test` и принадлежит
// другой программе. Вызов берёт все пробы пакета — гейт МОЛЧИТ.
//
// Каждая строка меняет против формы выше ОДИН факт: между `go test` и `-run`
// стоит конец команды (конвейер, `;`, `&&`, комментарий).
func TestSelectionGateLeavesAFlagOfAnotherProgramAlone(t *testing.T) {
	t.Parallel()
	const wf = ".github/workflows/ci.yaml"
	const head = "jobs:\n  probe:\n    steps:\n      - name: прогон\n"
	cases := []struct{ name, declRel, decl string }{
		{"конвейер на той же строке", wf, head +
			"        run: go test -tags=synthtag ./deploy/ -count=1 2>&1 | tool -run 'TestAlpha'\n"},
		{"конвейер перед переносом", wf, head +
			"        run: |\n" +
			"          go test -tags=synthtag ./deploy/ -count=1 2>&1 | \\\n" +
			"            tool -run 'TestAlpha'\n"},
		{"точка с запятой перед переносом", "scripts/synth.sh",
			"#!/usr/bin/env bash\n" +
				"go test -tags=synthtag ./deploy/ -count=1; \\\n" +
				"  tool -run 'TestAlpha'\n"},
		{"&& на той же строке", "Makefile",
			"test-synth:\n\t$(GO) test -tags=synthtag ./deploy/ -count=1 && tool -run 'TestAlpha'\n"},
		{"комментарий на строке переноса", "scripts/synth.sh",
			"#!/usr/bin/env bash\n" +
				"go test -tags=synthtag ./deploy/ -count=1 \\\n" +
				"  # -run 'TestAlpha'\n"},
		{"свёрнутый скаляр: конвейер перед сужением другой программы", wf, head +
			"        run: >-\n" +
			"          go test -tags=synthtag ./deploy/ -count=1 |\n" +
			"          tool -run 'TestAlpha'\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := synthSelectTreeWithDecl(t, c.declRel, c.decl, "deploy", synthTwoProbes)
			findings, census := auditSynthSelect(t, root)
			t.Log(census.String())

			if census.RunsFound != 1 {
				t.Fatalf("прогонов найдено %d, ожидался 1:\n%s\n%s", census.RunsFound, c.decl, census)
			}
			if len(findings) != 0 {
				t.Fatalf("`-run` принадлежит другой программе, а гейт приписал его `go test`:\n%s\n%s",
					c.decl, joinSelectionFindings(findings))
			}
			if census.FuncsSelected != 2 || len(census.NarrowingRuns) != 0 {
				t.Fatalf("вызов без своего сужения берёт обе пробы: %s", census)
			}
		})
	}
}
