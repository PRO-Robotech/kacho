// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/platformmodules"
)

func clientDocsFieldCoverageOptions(t *testing.T) ClientDocsFieldCoverageOptions {
	t.Helper()
	return ClientDocsFieldCoverageOptions{
		Root:         repoRoot(t),
		ProtoRoot:    "proto/kacho/cloud",
		ServicesRoot: "services",
		// Псевдоним ВЗЯТ у словаря имён модулей, а не выписан здесь: до #1885
		// то же соответствие жило пятью копиями этого корпуса.
		DomainAliases: platformmodules.AliasesByService(),
	}
}

// TestClientDocsNameEveryFieldTheContractCarries — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clientdocsfieldcoverage_injection_test.go`): здесь только вердикт.
func TestClientDocsNameEveryFieldTheContractCarries(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditClientDocsFieldCoverage(clientDocsFieldCoverageOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.ProtoFiles < 20 {
		t.Fatalf("файлов контракта прочитано %d — сверять не с чем", census.ProtoFiles)
	}
	if census.DocPages < 40 {
		t.Fatalf("страниц раздела API прочитано %d — обход пуст, вердикт беспредметен", census.DocPages)
	}
	if census.PagesJudged < 25 {
		t.Fatalf("страниц сопоставлено сообщению %d — правило имени страницы разошлось с деревом",
			census.PagesJudged)
	}
	if census.FieldsJudged < 300 {
		t.Fatalf("полей рассужено %d — разбор контракта перестал видеть объявления полей",
			census.FieldsJudged)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("страница ресурса молчит о поле, которое несёт её контракт "+
		"(страниц рассужено %d, полей %d):\n%s\n\n"+
		"Поле, которое приходит в ответе и не названо страницей, вызывающий читает как "+
		"случайность: он не знает ни что оно значит, ни на что опираться. Снятие — назвать "+
		"поле в таблице «Поля ресурса» страницы; ведомости прощённых у этого гейта нет "+
		"намеренно (см. шапку clientdocsfieldcoverage.go).",
		census.PagesJudged, census.FieldsJudged, strings.Join(lines, "\n"))
}
