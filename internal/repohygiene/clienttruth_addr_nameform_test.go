// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// nameFormClaimOptions — вход вердикта о НАСТОЯЩЕМ дереве.
//
// Носители перечислены двумя корнями, а не списком файлов: список разошёлся бы с
// деревом молча, а корень растёт вместе с ним. Отбор носителя — [nameFormCarrier].
func nameFormClaimOptions(t *testing.T) NameFormClaimOptions {
	t.Helper()
	return NameFormClaimOptions{
		Tree:      clientTruthRepoTree(t),
		ProtoRoot: "proto",
		DocsRoots: []string{"services"},
	}
}

// TestDeclaredNameFormMatchesTheEnforcedOne — вердикт о настоящем дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_addr_nameform_injection_test.go`): здесь только вердикт.
func TestDeclaredNameFormMatchesTheEnforcedOne(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditNameFormClaims(nameFormClaimOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.ProtoFiles < 20 {
		t.Fatalf("файлов контракта %d — обход пуст, вердикт беспредметен", census.ProtoFiles)
	}
	if census.DocsFiles < 20 {
		t.Fatalf("носителей сайта %d — обход пуст, вердикт беспредметен", census.DocsFiles)
	}
	// Вердикт выносится только о РАСПОЗНАННЫХ утверждениях. Ноль означал бы, что
	// он не вынесен ни разу, — «находок ноль» получено даром.
	if census.Claims == 0 {
		t.Fatalf("утверждений о форме имени распознано 0 — сверка не состоялась")
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("объявленная клиенту форма имени расходится с применяемой "+
		"(утверждений %d, совпало %d, снято послаблением %d):\n%s\n\n"+
		"Форма у платформы ОДНА и объявлена в `"+NameFormSourceRel+"`; её же читает "+
		"доменный newtype каждого сервиса. Правьте объявление, а не этот список: "+
		"клиент узнаёт алфавит имени отсюда и сервер об этом не спрашивает. "+
		"Строка про устаревшее послабление означает обратное: предмет исчез, "+
		"снимите запись в `nameFormClaimOptions`.",
		census.Claims, census.Agreeing, census.Exempted, strings.Join(lines, "\n"))
}
