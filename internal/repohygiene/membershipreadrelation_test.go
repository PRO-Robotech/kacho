// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// membershipreadrelation_test.go — гейт IAM-ID-2-06: чем гейтится чтение членства.
//
// Предмет, довод и НАЗВАННАЯ граница предмета изложены один раз — в шапке
// `membershipreadrelation.go`.

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

func TestMembershipReadIsGatedByTheTierRelationNotTheVerbOne(t *testing.T) {
	root := repoRoot(t)
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева не собран: %v", err)
	}
	c, err := SurveyMembershipReadRelation(tree)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}

	t.Logf("перепись: строк каталога прочитано %d · отношений типа %q разобрано %d · "+
		"из них глагольных ВЫВЕДЕНО %d (%v) · рассмотрено RPC %d из %d объявленных",
		c.CatalogRows, mrrScopeType, c.Relations, len(c.VerbRelations), c.VerbRelations,
		len(c.Subjects), len(mrrSubjects))

	if c.CatalogRows == 0 || c.Relations == 0 {
		t.Fatal("каталог прав либо модель прочитаны пустыми: «совпало» здесь означает " +
			"«не с чем сравнивать», и гейт сторожил бы пустоту")
	}
	if len(c.Subjects) != len(mrrSubjects) {
		t.Errorf("рассмотрено %d RPC из %d — остальные не нашлись в каталоге",
			len(c.Subjects), len(mrrSubjects))
	}

	// ВЫБОР ДОКАЗЫВАЕТСЯ СРАВНЕНИЕМ ДВУХ ОБЪЯВЛЕНИЙ, а не присутствием одного.
	// Без второй половины «ярусное отношение читает ярус» не говорит о выборе
	// ничего: оно было бы верно и в мире, где ярус читают ВСЕ отношения типа.
	if len(c.VerbRelations) == 0 {
		t.Fatal("глагольных отношений типа не выведено НИ ОДНОГО — вторая сторона " +
			"сравнения пуста, и «ярусное отношение читает ярус» перестало что-либо " +
			"доказывать: оно верно и там, где сравнивать не с чем")
	}
	for _, verb := range c.VerbRelations {
		if c.TierReaders[verb] {
			t.Errorf("глагольное отношение %q типа %q ЧИТАЕТ ярус распорядителя — "+
				"сравнение перестало различать два объявления, и выбор отношения "+
				"этим гейтом больше не доказывается. Либо модель изменилась осознанно "+
				"(тогда пересмотрите гейт вместе с ней), либо это регрессия анти-over-grant",
				verb, mrrScopeType)
		}
	}
	for rel, tier := range c.TierReaders {
		t.Logf("  отношение %-14q читает ярус: %-5v · выполнимо подстановкой: %v",
			rel, tier, c.WildcardSat[rel])
	}
	for _, e := range c.Subjects {
		t.Logf("  %s → rel=%q scope={%q,%q} сужение_на_данных=%v",
			e.FQN, e.RequiredRelation, e.ScopeExtractor.ObjectType,
			e.ScopeExtractor.FromRequestField, e.ScopeFiltered)
	}

	for _, f := range c.Findings {
		t.Error(f)
	}
}
