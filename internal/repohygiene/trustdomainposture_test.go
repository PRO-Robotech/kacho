// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trustdomainposture_test.go — выпускающая и принимающая стороны домена доверия
// берут его из ОДНОГО объявления (приёмка KAN-WIRE-1, сценарий KAN-W4-04,
// предмет `ПР-4`).
//
// Разбор, формы записи и границы распознавателя — в шапке
// `trustdomainposture.go`; здесь не пересказываются.
//
// # Почему это ОТДЕЛЬНЫЙ гейт, а не следствие переписи литералов
//
// Перепись литералов требует нуля в коде и на дереве без литералов молчит
// независимо от того, какую величину объявляет посадка. Расхождение двух сторон
// она не видит by construction — у неё другой предмет.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// trustDomainDeployRoot — дерево шаблонов посадки.
const trustDomainDeployRoot = "deploy/helm/"

// TestAcceptedTrustDomainMatchesIssued — сам гейт.
func TestAcceptedTrustDomainMatchesIssued(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// ── имена ручек выводятся из КОДА, а не выписываются ───────────────────────
	knobs := map[string]string{} // служба → текст ручки
	var goFiles int
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || skipPath(rel) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		found, perr := ScanTrustDomainKnobs(rel, src)
		if perr != nil {
			t.Fatalf("разбор %s: %v", rel, perr)
		}
		goFiles++
		for svc, knob := range found {
			knobs[svc] = knob
		}
	}

	var tokens []string
	for _, knob := range knobs {
		tokens = append(tokens, TrustDomainKnobTokens(knob)...)
	}
	sort.Strings(tokens)

	// ── шаблоны посадки ───────────────────────────────────────────────────────
	var rels []string
	for rel := range tt.files {
		if !strings.HasPrefix(rel, trustDomainDeployRoot) {
			continue
		}
		if !strings.HasSuffix(rel, ".yaml") && !strings.HasSuffix(rel, ".yml") && !strings.HasSuffix(rel, ".tpl") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		census    TrustDomainPostureCensus
		decls     []TrustDomainDeclSite
		uses      []TrustDomainUseSite
		findings  []string
		byChartIn = map[string]int{}
		byChartAc = map[string]int{}
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		d, u, c := ScanTrustDomainPosture(rel, trustDomainChartOf(rel), src, tokens)
		census.Files += c.Files
		census.Actions += c.Actions
		decls = append(decls, d...)
		uses = append(uses, u...)
	}
	census.Knobs = len(knobs)

	for _, u := range uses {
		switch u.Side {
		case "issuing":
			byChartIn[u.Chart]++
		case "accepting":
			byChartAc[u.Chart]++
		}
		if !u.FromHelper {
			findings = append(findings, fmt.Sprintf(
				"%s:%d  сторона=%s  чарт=%s — домен взят НЕ из объявления чарта "+
					"(нет include \"%s.trustDomain\")", u.File, u.Line, u.Side, u.Chart, u.Chart))
		}
	}
	for _, d := range decls {
		if d.Helper == "" {
			findings = append(findings, fmt.Sprintf(
				"%s:%d  чарт=%s — умолчание %q объявлено ВНЕ именованного шаблона: у домена "+
					"стало два адреса, и расходиться они будут молча", d.File, d.Line, d.Chart, d.Default))
		}
	}
	if mismatch := TrustDomainDeclDisagreement(decls); mismatch != "" {
		findings = append(findings, "умолчания домена РАСХОДЯТСЯ: "+mismatch)
	}
	// Чарт, который ЧЕКАНИТ сертификат под доменом, обязан тем же доменом
	// назвать и принимающую сторону своего процесса. Половина перехода — это
	// ровно такой чарт: величина переведена, а процесс её не читает.
	for chart, n := range byChartIn {
		if _, hasKnob := knobs[chart]; !hasKnob {
			// У чарта нет процесса, объявившего ручку домена (например, чеканка
			// личности для внешнего предъявителя). Требовать принимающую сторону
			// там нечего, и это не послабление: код сам сказал, что ручки нет.
			continue
		}
		if byChartAc[chart] == 0 {
			findings = append(findings, fmt.Sprintf(
				"чарт %s чеканит сертификат под доменом (%d место(а)), а ручку %q своему процессу "+
					"НЕ отдаёт: выпускающая сторона переведена, принимающая осталась прежней",
				chart, n, knobs[chart]))
		}
	}

	t.Logf("перепись: файлов Go разобрано %d, ручек выведено из кода %d (%v), "+
		"шаблонов посадки прочитано %d, действий шаблона осмотрено %d, объявлений домена %d, "+
		"употреблений %d (чеканка %d, приём %d), находок %d",
		goFiles, census.Knobs, tokens, census.Files, census.Actions, len(decls), len(uses),
		sumInts(byChartIn), sumInts(byChartAc), len(findings))

	if census.Files == 0 || census.Actions == 0 {
		t.Fatalf("прочитано %d шаблонов и %d действий — обход перестал видеть предмет, и его "+
			"молчание сказано ни о чём", census.Files, census.Actions)
	}
	if census.Knobs == 0 {
		t.Fatalf("из кода не выведено ни одной ручки домена (Spec.TrustDomainKnob) — связывать "+
			"стороны нечем, и гейт молчал бы при любом расхождении")
	}

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("домен доверия объявлен ДВАЖДЫ либо только с одной стороны — %d находк(и):\n  %s\n\n"+
			"Сертификат чеканится под доменом из величины профиля, а процесс признаёт своим тот, "+
			"что назван его ручкой. Разойдутся — законный отправитель перестанет опознаваться: "+
			"сертификат настоящий, выпущен настоящим центром, а принимающий его домена не знает, "+
			"и отказ неотличим от вызова без личности.\n"+
			"Снятие: объявить домен ОДИН раз именованным шаблоном чарта (`define \"<чарт>.trustDomain\"`) "+
			"и брать его через include на ОБЕИХ сторонах.",
			len(findings), strings.Join(findings, "\n  "))
	}
	if len(decls) == 0 {
		t.Fatalf("в %s не найдено ни одного объявления домена — гейт беспредметен: он молчит и "+
			"тогда, когда посадка домен назначать перестала", trustDomainDeployRoot)
	}
}

// trustDomainChartOf — чарт, которому принадлежит файл. Umbrella-шаблоны
// относятся к чарту `umbrella`: у них своё объявление и свой процесс.
func trustDomainChartOf(rel string) string {
	const charts = "/charts/"
	if i := strings.Index(rel, charts); i >= 0 {
		rest := rel[i+len(charts):]
		if j := strings.IndexByte(rest, '/'); j > 0 {
			return rest[:j]
		}
	}
	return "umbrella"
}

func sumInts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
