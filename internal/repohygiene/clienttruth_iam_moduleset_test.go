// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

func clientTruthIAMModuleSetOptions(t *testing.T) ClientTruthIAMModuleSetOptions {
	t.Helper()
	return ClientTruthIAMModuleSetOptions{
		Tree:          clientTruthRepoTree(t),
		ModuleSetFile: "services/iam/internal/authzmap/fga_types.go",
		ModuleSetVar:  "objectTypes",
		Surfaces:      []string{"services/iam/docs/content", "proto/kacho/cloud/iam"},
		SurfaceExts:   []string{".mdx", ".md", ".proto"},
	}
}

// TestClientTruthIAMModuleSetEnumerationsAreComplete — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_iam_moduleset_injection_test.go`): здесь только вердикт.
func TestClientTruthIAMModuleSetEnumerationsAreComplete(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditClientTruthIAMModuleSet(clientTruthIAMModuleSetOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.Modules < 4 {
		t.Fatalf("модулей выведено %d — объявление набора не прочитано, судить не по чему",
			census.Modules)
	}
	if census.TypeKeys < 20 {
		t.Fatalf("ключей типа прочитано %d — таблица прочитана не вся, набор мог выйти неполным",
			census.TypeKeys)
	}
	if census.SurfaceFiles < 20 {
		t.Fatalf("файлов клиентской поверхности %d — обход пуст, вердикт беспредметен",
			census.SurfaceFiles)
	}
	// Вердикт выносится ТОЛЬКО о перечнях. Ноль распознанных означал бы, что он
	// не вынесен ни разу, — и «находок ноль» получено даром.
	if census.Enumerations == 0 {
		t.Fatal("перечней распознано 0 — сверка не состоялась ни разу")
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("клиентская поверхность называет набор модулей неполно "+
		"(модулей %d, перечней рассужено %d):\n%s\n\n"+
		"Модуль — то, чем клиент выражает грант (`Rule.module`). Перечень, назвавший меньше, "+
		"чем принимает сервер, читается как «тонко выдать доступ к недостающему домену нельзя», "+
		"а единственный выход при таком чтении — системная роль на весь уровень, то есть "+
		"заведомое расширение доступа. Набор выводится из приставок ключей "+
		"`authzmap.objectTypes`; правьте перечень, а не набор.",
		census.Modules, census.Enumerations, strings.Join(lines, "\n"))
}
