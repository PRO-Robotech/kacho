// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

func clientDocsExampleFieldsOptions(t *testing.T) ClientDocsExampleFieldsOptions {
	t.Helper()
	return ClientDocsExampleFieldsOptions{
		Root: repoRoot(t),
		// Край — такой же сайт документации, как сервисный, и его страницы
		// судятся тем же правилом. Перечень корней выписан, а не выведен, ровно
		// потому, что корней ДВА и оба названы в раскладке монорепо.
		DocRoots: []string{"services", "."},
	}
}

// TestClientDocsExampleCarriesEveryFieldTheTableNames — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clientdocsexamplefields_injection_test.go`): здесь только вердикт.
func TestClientDocsExampleCarriesEveryFieldTheTableNames(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditClientDocsExampleFields(clientDocsExampleFieldsOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.Pages < 40 {
		t.Fatalf("страниц раздела API прочитано %d — обход пуст, вердикт беспредметен", census.Pages)
	}
	if census.PagesWithTable < 10 {
		t.Fatalf("страниц с таблицей «Поля ресурса» %d — разбор таблицы разошёлся с деревом",
			census.PagesWithTable)
	}
	if census.PagesWithRead < 10 {
		t.Fatalf("страниц с полным примером чтения по идентификатору %d — разбор примера "+
			"разошёлся с деревом", census.PagesWithRead)
	}
	if census.PagesJudged == 0 {
		t.Fatalf("судимых страниц ноль — обещание «ответ несёт все поля ресурса» снято")
	}
	if census.TableFields < 20 {
		t.Fatalf("полей таблицы у судимых страниц рассужено %d — разбор перестал видеть строки",
			census.TableFields)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("пример ответа и таблица полей одной страницы называют РАЗНЫЕ множества "+
		"(судимых страниц %d, полей таблицы %d, полей примера %d):\n%s\n\n"+
		"Вызывающий строит клиента по примеру — он копируется целиком, — поэтому поле, "+
		"названное таблицей и выпавшее из примера, читается как случайность. Снятие — "+
		"привести пример и таблицу к одному множеству; ведомости прощённых у этого гейта "+
		"нет намеренно (см. шапку clientdocsexamplefields.go).",
		census.PagesJudged, census.TableFields, census.ExampleFields, strings.Join(lines, "\n"))
}
