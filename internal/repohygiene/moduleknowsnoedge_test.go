// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

func moduleKnowsNoEdgeOptions(t *testing.T) ModuleKnowsNoEdgeOptions {
	t.Helper()
	return ModuleKnowsNoEdgeOptions{
		Root:        repoRoot(t),
		ServicesDir: "services",
		// Два дома чарта, оба настоящие: свой у каждого модуля и зонтичный —
		// у тех, кто в зонт входит. Несуществующий пропускается, найденные
		// считаются переписью, поэтому опечатка в шаблоне видна нулём.
		ChartDirTemplates: []string{
			"services/%s/deploy",
			"deploy/helm/umbrella/charts/kacho-%s",
		},
	}
}

// TestNoModuleIsTypedByItsConsumer — вердикт о НАСТОЯЩЕМ дереве по ВСЕМ модулям.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`moduleknowsnoedge_injection_test.go`): здесь только вердикт.
func TestNoModuleIsTypedByItsConsumer(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditModuleKnowsNoEdge(moduleKnowsNoEdgeOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок» было бы
	// достижимо пустым обходом — то есть неотличимо от «ноль прочитанного».
	// Порог по модулям намеренно нижний, а не точный: перечень ВЫВОДИТСЯ, и
	// точное число здесь запретило бы заводить восьмой модуль.
	if len(census.Modules) < 7 || census.GoFiles < 1000 || census.ChartDirs < 7 || census.ChartFiles < 40 {
		t.Fatalf("модулей %d, файлов Go %d, каталогов чарта %d, файлов чарта %d — обход пуст, вердикт беспредметен",
			len(census.Modules), census.GoFiles, census.ChartDirs, census.ChartFiles)
	}

	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("\n  " + f.String())
	}
	t.Fatalf("модуль знает своего потребителя — находок %d:%s\n\n"+
		"соединение открывает ПОТРЕБИТЕЛЬ: край сам открывает поток к модулю и сам "+
		"читает журналы курсором. Поэтому ни типа края, ни его адреса у модуля быть "+
		"не должно — ни в коде, ни в посадке. Обязательная ручка адреса вдобавок "+
		"означает отказ старта там, где края нет вовсе, и это делает вынос модуля "+
		"отдельным продуктом невыразимым.", len(findings), b.String())
}
