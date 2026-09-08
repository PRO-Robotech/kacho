// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// clientTruthKanameExclusionFormOptions — координаты НАСТОЯЩЕГО дерева.
//
// Каталогами, а не файлами: и провязка снятия, и код читателя уже переезжали
// между файлами своих деревьев, и привязка к имени файла дала бы «анализатор не
// отработал» вместо вердикта. Исключение одно — файл, ОБЪЯВЛЯЮЩИЙ снятие: его
// собственные определения вызовами не являются, и назвать его надо поимённо.
func clientTruthKanameExclusionFormOptions(t *testing.T) ClientTruthKanameExclusionFormOptions {
	t.Helper()
	return ClientTruthKanameExclusionFormOptions{
		Tree:          clientTruthRepoTree(t),
		GuidePath:     "services/iam/INSTALL.md",
		EdgeDir:       "gateway",
		StripFuncs:    []string{"StripPresentedCredential", "StripCredentialBeforeForwarding"},
		StripDeclFile: "gateway/internal/principalmeta/credential_strip.go",
		ReaderDir:     "services/iam/internal/presentedcred",
		RefuseFunc:    "refuse",
		// ОБА слова: отказ, назвавший одну форму, — про неё одну. Сообщения
		// отказа читателя английские, поэтому и слова английские.
		CoPresenceTerms: []string{"presented", "forwarded"},
	}
}

// TestClientTruthKanameExclusionFormMatchesTheTree — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_kaname_exclusion_form_injection_test.go`): здесь только вердикт.
func TestClientTruthKanameExclusionFormMatchesTheTree(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditClientTruthKanameExclusionForm(
		clientTruthKanameExclusionFormOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// ── премисы: «ноль находок» отличимо от «ноль прочитанного» ──────────────
	if census.GuideParagraphs == 0 {
		t.Fatal("абзацев страницы прочитано 0 — страница пуста или не найдена, " +
			"и вердикт о ней беспредметен")
	}
	if census.EdgeGoFiles == 0 {
		t.Fatal("файлов края разобрано 0 — обход стороны построения пуст")
	}
	if census.ReaderGoFiles == 0 {
		t.Fatal("файлов читателя разобрано 0 — обход стороны отказа пуст")
	}
	if census.RefuseCalls == 0 {
		t.Fatal("отказов у читателя встречено 0 — распознаватель отказов ослеп " +
			"либо читатель перестал отказывать вовсе; и то и другое означает, " +
			"что «производителя нет» получено даром")
	}
	// Премиса ПРЕДМЕТА, а не обхода: гейт судит согласие страницы с ПОСТРОЕНИЕМ.
	// Построения нет — судить не с чем, и молчать об этом нельзя: зелёный на
	// мёртвом механизме неотличим от зелёного на исправном.
	if census.StripCalls == 0 {
		t.Fatal("вызовов снятия удостоверения на крае 0 — построение, которым " +
			"держится взаимоисключение, уехало из дерева. Гейт судит согласие " +
			"страницы с ним; без него он не выносит вердикта, а не разрешает")
	}

	for _, f := range findings {
		t.Errorf("%s", f)
	}
	if len(findings) > 0 {
		t.Log("Взаимоисключение двух форм личности держится ПОСТРОЕНИЕМ: край снимает " +
			"арендаторское удостоверение перед пересылкой за себя, установив личность " +
			"сам. Страница обязана объяснять его этим снятием — и не вправе обещать " +
			"оператору отказ, которого ни одна строка службы не производит.")
	}
}
