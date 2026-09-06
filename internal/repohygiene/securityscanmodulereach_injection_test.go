// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «скан читает каждый модуль» СПОСОБЕН упасть —
// и что падает он на существе, а не на тексте.
//
// Инъекция идёт в обе стороны, потому что одного «краснеет» мало (проверка,
// краснеющая на всём, ничего не измеряет), и одного «молчит» мало тоже
// (молчание бывает от того, что читать не стали):
//
//	прямой вызов сканера по `./...`   → находка, названа координатой шага;
//	законный близнец (зовёт перепись) → молчит, и перепись его ЗАСЧИТЫВАЕТ;
//	установка пина gosec@vX           → НЕ находка (она ничего не сканирует);
//	`./...` только в комментарии      → НЕ находка (судится исполняемая часть);
//	нет вызова переписи               → видно по счётчику CensusCalls;
//	нет вызова вердикта               → видно по счётчику VerdictCalls;
//	объявление вовсе без шагов run    → RunBlocks = 0, предпосылка не выполнена;
//	неразбираемый YAML                → ошибка, а не тишина.
//
// БЛИЗНЕЦ НАСТОЯЩИЙ, А НЕ КОПИЯ КРАСНОГО СЛУЧАЯ: он несёт слово `gosec` во всех
// местах, где оно законно, — в имени задания, в подписи шага, в установке пина,
// в имени самих скриптов и в комментарии. Отличие от красного случая ровно
// одно: сканер не зовётся по множеству пакетов.
//
// Обе половины гоняют ТУ ЖЕ функцию (`auditSecurityScanWiring`), что и прогон по
// дереву: проба, повторяющая логику гейта своей копией, доказывала бы свойство
// копии.
package repohygiene

import "testing"

// ДЕФЕКТ. Скан идёт прямым `./...` из корня — ровно состояние до #2092.
const synthScanDirectGosec = `name: security-scan
jobs:
  gosec:
    name: gosec (Go static analysis)
    steps:
      - name: install gosec
        run: go install github.com/securego/gosec/v2/cmd/gosec@v2.28.0
      - name: run gosec
        run: gosec -exclude-dir=pkg/api -fmt sarif -out gosec.sarif ./...
      - name: fail on level=error
        run: ./scripts/gosec-verdict.sh
`

// ЗАКОННЫЙ БЛИЗНЕЦ. Слово `gosec` везде, где оно законно; сканер по множеству
// пакетов не зовётся — его зовёт перепись модулей.
const synthScanViaCensus = `name: security-scan
jobs:
  gosec:
    name: gosec (Go static analysis)
    steps:
      - name: install gosec
        run: go install github.com/securego/gosec/v2/cmd/gosec@v2.28.0
      - name: run gosec
        # прежде здесь стоял прямой вызов по ./... — он и был слепой зоной
        run: ./scripts/gosec-scan-modules.sh
      - name: fail on level=error
        run: ./scripts/gosec-verdict.sh
`

// ЛОВУШКА ТЕКСТА. `./...` стоит только в комментарии оболочки; исполняется
// перепись. Гейт, судящий по сырому тексту, покраснел бы на собственном
// объяснении.
const synthScanMentionsGlobInComment = `name: security-scan
jobs:
  gosec:
    steps:
      - name: run gosec
        run: |
          # gosec ./... читал бы пакеты одного модуля — потому здесь перепись
          ./scripts/gosec-scan-modules.sh
      - name: fail on level=error
        run: ./scripts/gosec-verdict.sh
`

// ПЕРЕПИСЬ СНИМАЕТСЯ, НО НИКЕМ НЕ ЧИТАЕТСЯ: вердикта нет.
const synthScanWithoutVerdict = `name: security-scan
jobs:
  gosec:
    steps:
      - name: run gosec
        run: ./scripts/gosec-scan-modules.sh
`

// ШАГОВ `run` НЕТ ВОВСЕ — предпосылка гейта не выполняется.
const synthScanNoRunSteps = `name: security-scan
jobs:
  trivy:
    steps:
      - uses: aquasecurity/trivy-action@v0.36.0
`

func TestSecurityScanWiringGateCatchesDirectGosec(t *testing.T) {
	w, err := auditSecurityScanWiring([]byte(synthScanDirectGosec))
	if err != nil {
		t.Fatalf("синтетика не разобралась: %v", err)
	}
	if len(w.DirectGosec) != 1 {
		t.Fatalf("прямой вызов сканера по ./... НЕ найден (DirectGosec=%v). Гейт "+
			"структурно неспособен увидеть свой предмет.", w.DirectGosec)
	}
	if len(w.CensusCalls) != 0 {
		t.Errorf("перепись модулей засчитана там, где её не зовут: %v", w.CensusCalls)
	}
	// Установка пина — не сканирование, и в находки попасть не должна.
	if got := w.DirectGosec[0]; got != "gosec / run gosec" {
		t.Errorf("находка названа не той координатой: %q. Ожидался шаг скана, а не "+
			"шаг установки пина — иначе читатель пойдёт чинить не то.", got)
	}
	t.Logf("инъекция: заданий %d, блоков run %d, прямых вызовов %d — %v",
		w.Jobs, w.RunBlocks, len(w.DirectGosec), w.DirectGosec)
}

func TestSecurityScanWiringGateStaysSilentOnCensus(t *testing.T) {
	w, err := auditSecurityScanWiring([]byte(synthScanViaCensus))
	if err != nil {
		t.Fatalf("близнец не разобрался: %v", err)
	}
	if len(w.DirectGosec) != 0 {
		t.Errorf("гейт краснеет на ЗАКОННОМ объявлении: %v. Слово `gosec` в имени "+
			"задания, в подписи шага и в установке пина сканированием не является.",
			w.DirectGosec)
	}
	// Молчание обязано быть от ПРОЧИТАННОГО, а не от непрочитанного.
	if w.RunBlocks != 3 {
		t.Errorf("прочитано блоков run: %d, ожидалось 3 — молчание гейта здесь "+
			"неотличимо от того, что он ничего не читал", w.RunBlocks)
	}
	if len(w.CensusCalls) != 1 || len(w.VerdictCalls) != 1 {
		t.Errorf("законный близнец не засчитан: перепись %v, вердикт %v",
			w.CensusCalls, w.VerdictCalls)
	}
}

func TestSecurityScanWiringGateReadsCodeNotComments(t *testing.T) {
	w, err := auditSecurityScanWiring([]byte(synthScanMentionsGlobInComment))
	if err != nil {
		t.Fatalf("синтетика не разобралась: %v", err)
	}
	if len(w.DirectGosec) != 0 {
		t.Errorf("гейт посчитал сканированием то, что стоит в КОММЕНТАРИИ: %v. "+
			"Проверка по сырому тексту краснела бы на собственном объяснении.",
			w.DirectGosec)
	}
	if len(w.CensusCalls) != 1 {
		t.Errorf("исполняемая часть не прочитана: перепись %v", w.CensusCalls)
	}
}

func TestSecurityScanWiringGateSeesMissingVerdict(t *testing.T) {
	w, err := auditSecurityScanWiring([]byte(synthScanWithoutVerdict))
	if err != nil {
		t.Fatalf("синтетика не разобралась: %v", err)
	}
	if len(w.VerdictCalls) != 0 {
		t.Fatalf("вердикт засчитан там, где его не зовут: %v", w.VerdictCalls)
	}
	if len(w.CensusCalls) != 1 {
		t.Errorf("перепись не засчитана: %v — тогда проба утверждает не о том", w.CensusCalls)
	}
}

func TestSecurityScanWiringGateSeesEmptyPremise(t *testing.T) {
	w, err := auditSecurityScanWiring([]byte(synthScanNoRunSteps))
	if err != nil {
		t.Fatalf("синтетика не разобралась: %v", err)
	}
	if w.RunBlocks != 0 {
		t.Fatalf("прочитано %d блоков run там, где их нет", w.RunBlocks)
	}
	if w.Jobs != 1 {
		t.Errorf("заданий прочитано %d, ожидалось 1 — перепись обязана отличать "+
			"«нет шагов run» от «нечего было читать»", w.Jobs)
	}
}

func TestSecurityScanWiringGateFailsOnUnparsableDeclaration(t *testing.T) {
	if _, err := auditSecurityScanWiring([]byte("jobs: [ это не карта\n")); err == nil {
		t.Fatal("неразбираемое объявление принято молча. Файл, который НЕ проверен, " +
			"обязан давать ошибку, а не тишину: тишина здесь читается как чистота.")
	}
}
