// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_posture_reaches_every_stand_injection_test.go — ИНЪЕКЦИЯ в обе
// стороны для гейта «посадку объявляет каждый стенд».
//
// Строится на СИНТЕТИКЕ, а не на живом стенде дерева: проба, привязанная к
// предмету, который однажды сойдётся, истекает вместе с ним — стенды объявят
// посадку все, и самопроверка покраснела бы на достижении своей цели.
//
// Зовётся ТО ЖЕ тело (judgeStandPostures), что исполняется на дереве.
package deploy_test

import (
	"strings"
	"testing"
)

// ДЕФЕКТ: стенд не объявил посадку ни одним слоем цепочки — едет на умолчании.
func TestInjection_AStandInheritingItsPostureIsFound(t *testing.T) {
	t.Parallel()
	declaring, findings := judgeStandPostures([]standPosture{
		{Stand: "synthetic", Layers: []string{"values.synthetic.yaml"}},
	})
	if declaring != 0 {
		t.Errorf("необъявивший стенд засчитан объявившим: declaring=%d", declaring)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "НИ ОДИН слой") {
		t.Fatalf("унаследованная посадка не найдена: %v", findings)
	}
	if !strings.Contains(findings[0], "values.synthetic.yaml") {
		t.Errorf("находка не называет цепочку: %v", findings)
	}
}

// ДЕФЕКТ: объявила только одна половина — вторая унаследует умолчание, и стенд
// разъедется без чьего-либо решения. ДВЕ формы, обе обязаны находиться.
func TestInjection_AStandDeclaringOnlyOneHalfIsFound(t *testing.T) {
	t.Parallel()
	_, onlyIAM := judgeStandPostures([]standPosture{
		{Stand: "synthetic", Layers: []string{"values.synthetic.yaml"},
			IAM: "own", IAMAt: "values.synthetic.yaml"},
	})
	if len(onlyIAM) != 1 || !strings.Contains(onlyIAM[0], "ТОЛЬКО служба прав") {
		t.Fatalf("односторонняя декларация (служба прав) не найдена: %v", onlyIAM)
	}
	_, onlyEdge := judgeStandPostures([]standPosture{
		{Stand: "synthetic", Layers: []string{"values.synthetic.yaml"},
			Edge: "own", EdgeAt: "values.synthetic.yaml"},
	})
	if len(onlyEdge) != 1 || !strings.Contains(onlyEdge[0], "ТОЛЬКО край") {
		t.Fatalf("односторонняя декларация (край) не найдена: %v", onlyEdge)
	}
}

// ДЕФЕКТ: половины объявили РАЗНОЕ.
func TestInjection_AStandWhoseHalvesDisagreeIsFound(t *testing.T) {
	t.Parallel()
	declaring, findings := judgeStandPostures([]standPosture{
		{Stand: "synthetic", Layers: []string{"values.synthetic.yaml"},
			Edge: "own", EdgeAt: "values.synthetic.yaml",
			IAM: "external", IAMAt: "values.synthetic.yaml"},
	})
	if declaring != 1 {
		t.Errorf("объявивший стенд не засчитан: declaring=%d", declaring)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "РАЗНОЕ") {
		t.Fatalf("расхождение половин не найдено: %v", findings)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ: обе половины объявлены и согласны — гейт молчит.
//
// Против каждого дефекта выше здесь меняется РОВНО ОДИН факт.
func TestInjection_AStandDeclaringBothHalvesAlikeIsSilent(t *testing.T) {
	t.Parallel()
	declaring, findings := judgeStandPostures([]standPosture{
		{Stand: "synthetic", Layers: []string{"values.base.yaml", "values.overlay.yaml"},
			Edge: "external", EdgeAt: "values.base.yaml",
			IAM: "external", IAMAt: "values.base.yaml"},
	})
	if len(findings) != 0 {
		t.Fatalf("согласные половины объявлены находкой: %v", findings)
	}
	if declaring != 1 {
		t.Fatalf("перепись не засчитала объявивший стенд: declaring=%d", declaring)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ ВТОРОГО РОДА: посадку объявил НИЖНИЙ слой цепочки, а
// накладка о ней молчит — это не находка. Иначе гейт требовал бы повтора в
// каждой накладке, а повтор в последнем слое опаснее молчания: при расхождении
// с нижним слоем выигрывает он, и стенд молча поедет на устаревшей копии.
func TestInjection_APostureDeclaredByALowerLayerIsSilent(t *testing.T) {
	t.Parallel()
	_, findings := judgeStandPostures([]standPosture{
		{Stand: "synthetic", Layers: []string{"values.base.yaml", "values.images.yaml"},
			Edge: "own", EdgeAt: "values.base.yaml",
			IAM: "own", IAMAt: "values.base.yaml"},
	})
	if len(findings) != 0 {
		t.Fatalf("объявление нижним слоем объявлено находкой: %v", findings)
	}
}
