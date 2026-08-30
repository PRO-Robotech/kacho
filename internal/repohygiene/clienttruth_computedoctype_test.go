// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

func clientDocsAnyTypeOptions(t *testing.T) ClientDocsAnyTypeOptions {
	t.Helper()
	return ClientDocsAnyTypeOptions{Root: repoRoot(t), ProtoRoot: "proto"}
}

// TestClientDocsAnyTypeResolvesInTheContracts — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_computedoctype_injection_test.go`): здесь только вердикт.
func TestClientDocsAnyTypeResolvesInTheContracts(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditClientDocsAnyType(clientDocsAnyTypeOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.ProtoFiles < 20 {
		t.Fatalf("файлов контракта %d — дерево контрактов не прочитано, словарь строить не из чего",
			census.ProtoFiles)
	}
	if census.ContractTypes < 200 {
		t.Fatalf("типов в словаре %d — разбор контрактов не состоялся", census.ContractTypes)
	}
	if census.NestedTypes == 0 {
		t.Fatalf("вложенных типов в словаре 0 — разбор читает только верхний уровень, " +
			"и всякое вложенное имя объявлялось бы несуществующим")
	}
	if census.DocFiles < 50 {
		t.Fatalf("страниц клиентской документации %d — обход пуст, вердикт беспредметен", census.DocFiles)
	}
	// Вердикт выносится ТОЛЬКО о полных именах пространства kacho. Ноль
	// рассуженных означал бы, что он не вынесен ни разу.
	if census.Judged == 0 {
		t.Fatalf("полных имён рассужено 0 при %d встреченных — сверка не состоялась", census.TypeURLs)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("клиентская документация называет тип, которого в контрактах нет "+
		"(имён встречено %d, рассужено %d):\n%s\n\n"+
		"`Operation.metadata` — это `Any`, и клиент распаковывает его ПО ЭТОМУ ИМЕНИ: "+
		"имени без сообщения соответствует неразбираемый ответ, а не неточная проза. "+
		"Пакет судится наравне с именем: тип соседнего домена разбирается не лучше "+
		"выдуманного и вдобавок посылает поллить операцию не тому владельцу. "+
		"Словарь выводится из `proto/`; правьте документ, а не этот список.",
		census.TypeURLs, census.Judged, strings.Join(lines, "\n"))
}
