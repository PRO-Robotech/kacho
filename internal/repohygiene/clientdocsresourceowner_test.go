// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/platformmodules"
)

func clientDocsResourceOwnerOptions(t *testing.T) ClientDocsResourceOwnerOptions {
	t.Helper()
	return ClientDocsResourceOwnerOptions{
		Root:      repoRoot(t),
		ProtoRoot: "proto",
		// Псевдоним не выписан, а ВЗЯТ у словаря имён модулей
		// (pkg/platformmodules): выводить его не из чего — расхождение
		// установлено решением, а не правилом, — но и объявлять его здесь
		// нельзя: до #1885 то же соответствие жило пятью копиями в этом
		// корпусе, и каждая объясняла в комментарии, почему вывести его
		// невозможно.
		DomainAliases: platformmodules.AliasesByService(),
	}
}

// TestClientDocsAttributeResourcesToTheirOwner — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clientdocsresourceowner_injection_test.go`): здесь только вердикт.
func TestClientDocsAttributeResourcesToTheirOwner(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditClientDocsResourceOwner(clientDocsResourceOwnerOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.ProtoFiles < 20 {
		t.Fatalf("файлов контракта %d — дерево контрактов не прочитано, владение выводить не из чего",
			census.ProtoFiles)
	}
	if census.DocFiles < 50 {
		t.Fatalf("страниц клиентской документации %d — обход пуст, вердикт беспредметен", census.DocFiles)
	}
	if census.OwnedNames < 20 {
		t.Fatalf("имён с единственным владельцем %d — словарь платформы не построен", census.OwnedNames)
	}
	// Вторая половина вердикта выносится ТОЛЬКО о строках владения. Ноль
	// распознанных строк либо ноль рассуженных имён означал бы, что она не
	// вынесена ни разу, — и «находок ноль» получено даром.
	if census.OwnershipRow == 0 || census.NamesJudged == 0 {
		t.Fatalf("строк владения распознано %d, имён рассужено %d — сверка не состоялась",
			census.OwnershipRow, census.NamesJudged)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("клиентская документация приписывает ресурс чужому домену "+
		"(строк владения %d, имён рассужено %d):\n%s\n\n"+
		"Имя домена — значение параметра, а не проза: им адресуются поток изменений "+
		"(`?owner=…`), REST-префикс и префикс идентификатора операции. Строка, называющая "+
		"чужой домен, ведёт вызывающего туда, где предмета нет. Владелец выводится из "+
		"`proto/kacho/cloud/<домен>/v1/<Ресурс>Service`; правьте документ, а не этот список.",
		census.OwnershipRow, census.NamesJudged, strings.Join(lines, "\n"))
}
