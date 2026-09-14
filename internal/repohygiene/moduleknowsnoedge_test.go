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
	t.Parallel()
	var log strings.Builder
	findings, census, err := AuditModuleKnowsNoEdge(moduleKnowsNoEdgeOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок» было бы
	// достижимо пустым обходом — то есть неотличимо от «ноль прочитанного».
	//
	// Величины взяты ОТ ЧИСЛА МОДУЛЕЙ, а не абсолютными литералами, и это не
	// косметика. Прежняя редакция требовала «модулей не меньше 7, файлов Go не
	// меньше 1000» — числа, снятые с популяции, в которую входила служба
	// доступа. Служба вынесена отдельным продуктом, модулей стало 6, файлов 724,
	// и премиса покраснела на ИСПРАВНОМ дереве: она измеряла состав популяции, а
	// не полноту обхода. Абсолютный литерал стареет молча при каждом переносе
	// модуля, и починка «поправить число» вернула бы тот же отказ следующим
	// переносом.
	//
	// Перечень модулей ВЫВОДИТСЯ из дерева (deriveModules), и пустой перечень
	// анализатор отвергает сам. Поэтому здесь остаётся то, что обход обязан
	// произвести НА КАЖДЫЙ выведенный модуль: свой каталог чарта у каждого и
	// непустой вклад в оба корпуса. Такая премиса ловит ровно тот отказ, ради
	// которого заведена (обход прочитал не то дерево либо половину его), и
	// переживает и появление восьмого модуля, и уход шестого.
	switch {
	case len(census.Modules) == 0:
		t.Fatalf("модулей ноль — перечень не выведен, вердикт беспредметен")
	case census.ChartDirs < len(census.Modules):
		t.Fatalf("модулей %d, каталогов чарта %d — у части модулей чарт не найден, "+
			"обход половины АДРЕСА неполон", len(census.Modules), census.ChartDirs)
	case census.GoFiles < 100*len(census.Modules) || census.ChartFiles < 5*len(census.Modules):
		t.Fatalf("модулей %d, файлов Go %d, файлов чарта %d — на модуль приходится "+
			"меньше, чем даёт любой живой модуль дерева: обход пуст либо усечён",
			len(census.Modules), census.GoFiles, census.ChartFiles)
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
