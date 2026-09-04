// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestContractsAgreeWithTheFilterValueLimitTheParserApplies — контракт не
// обещает о значении фильтра ни отсутствия правила, ни чужого предела.
//
// Предмет, обе половины и границы — в шапке ct2_misc_filter_value_bounds.go.
// Способность гейта упасть и смолчать доказана инъекцией в обе стороны:
// ct2_misc_filter_value_bounds_injection_test.go.
func TestContractsAgreeWithTheFilterValueLimitTheParserApplies(t *testing.T) {
	root := repoRoot(t)

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}

	// Величина берётся у САМОГО разборщика, а не переписывается сюда: копия
	// предела была бы вторым местом об одном предмете — ровно тем, что гейт и
	// ловит в контрактах.
	limit := filter.MaxValueLen

	c, err := collectFilterValueClaims(tree, limit)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	// ── ПЕРЕПИСЬ. Печатается ВСЕГДА и несёт ДВЕ величины: сколько объявлений
	// осмотрено и сколько из них совпало.
	var named []string
	for _, cl := range c.Claims {
		mark := fmt.Sprintf("%v", cl.StatedLimits)
		if cl.DeniesRule {
			mark = "объявляет отсутствие правила"
		}
		named = append(named, fmt.Sprintf("%s:%d %s", cl.Rel, cl.Line, mark))
	}
	t.Logf("перепись: контрактов прочитано %d · полей filter %d · объявлений о "+
		"значении %d (называют предел %d, отрицают правило %d) · совпало %d · "+
		"предел разборщика %d\n         %s",
		c.Files, c.Fields, len(c.Claims), c.Stating, c.Denying, c.Agreeing,
		limit, strings.Join(named, "; "))

	// ── ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. Пустой обход обесценивает вердикт.
	if c.Files == 0 {
		t.Fatal("контрактов не прочитано ни одного — вердикт беспредметен")
	}
	if c.Fields == 0 {
		t.Fatal("полей filter в контрактах не найдено ни одного — распознаватель " +
			"перестал видеть предмет (см. п.7 §«Гейт на класс»)")
	}
	if c.Stating == 0 {
		t.Fatalf("ни один контракт не называет предела длины значения фильтра — "+
			"сверять нечего, а предел у разборщика есть (%d); либо распознаватель "+
			"ослеп, либо контракты перестали описывать правило", limit)
	}

	for _, f := range filterValueClaimFindings(c, limit) {
		t.Errorf("объявление о значении фильтра: %s", f)
	}
}
