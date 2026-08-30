// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

func subscriptionKindOptions(t *testing.T) SubscriptionKindOptions {
	t.Helper()
	return SubscriptionKindOptions{
		Root:      repoRoot(t),
		ProtoRoot: "proto",
		GoRoots:   []string{"pkg", "services", "gateway", "terraform", "internal", "cmd"},
		// Клиентская страница подписки: второе место об одном предмете, и
		// сверяется оно множествами в обе стороны.
		ClientPage: "gateway/docs/content/api/subscription.mdx",
	}
}

// TestSubscriptionKindVocabularyHasOneWriting — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`subscriptionkindvocabulary_injection_test.go`): здесь только вердикт.
func TestSubscriptionKindVocabularyHasOneWriting(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditSubscriptionKindVocabulary(subscriptionKindOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок» было бы
	// достижимо пустым обходом.
	if census.ProtoFiles < 20 || census.GoFiles < 500 {
		t.Fatalf("файлов контракта %d, файлов прод-кода %d — обход пуст, вердикт беспредметен",
			census.ProtoFiles, census.GoFiles)
	}
	// Вторая половина вердикта (объявлен ли тип платформой) выносится ТОЛЬКО о
	// разрешённых словах. Ноль разрешённых означал бы, что она не вынесена ни
	// разу, — и «находок ноль» получено даром.
	if census.ObjectTypesUsed == 0 {
		t.Fatalf("записей вида %d, а разрешённых типов объекта 0: вторая половина вердикта не вынесена ни разу",
			census.KindEntries)
	}
	// Страница сверена, а не пропущена: ноль прочитанных байт означал бы, что
	// половина про клиентскую документацию не выносилась вовсе.
	if census.PageBytes == 0 || census.PageKinds == 0 {
		t.Fatalf("клиентская страница: прочитано %d байт, видов названо %d — сверка не состоялась",
			census.PageBytes, census.PageKinds)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("написание вида предмета подписки разошлось (объявлений журнала %d, записей вида %d):\n%s",
		census.JournalMappings, census.KindEntries, strings.Join(lines, "\n"))
}
