// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"testing"
)

// removedpathcensus_test.go — держатель условия «снятое по дереву объяснено».
// Предмет, замер, из которого выведена форма, идиом объявления и границы — в
// шапке removedpathcensus.go; здесь только добыча входа, перепись и вердикт.

// TestRemovedPathsAreExplained — каждый снятый носитель гейта вне корпуса обязан
// быть объявлен надгробием.
//
// Перепись печатает ДЕСЯТЬ величин, а не одну. Одного числа мало ровно в том
// случае, ради которого перепись заведена: «необъяснённых 0» при непрочитанной
// базе выглядит так же, как чистая ветка.
//
// Пустая дельта (ветка ничего не снимает) — ЗАКОННЫЙ ЗЕЛЁНЫЙ, а не отказ: проба
// не имеет права падать на достижении своей цели. Отказом остаётся
// НЕПРОЧИТАННОЕ — база не резолвится либо состав базы пуст.
func TestRemovedPathsAreExplained(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	// Присутствие рабочего дерева git и «ствол не резолвится — ОТКАЗ, а не
	// пропуск» держит соседний помощник; базу он даёт стволовую, а этой переписи
	// нужна точка ответвления линии — её выводит gateCarrierBase, и второго
	// вывода базы здесь не заводится: два места об одном предмете разошлись бы
	// молча.
	_ = requireTrunkRef(t, root)

	base, how, err := gateCarrierBase(root)
	if err != nil {
		t.Fatalf("определить базу: %v", err)
	}

	// ПРЕДПОСЫЛКА. Пустой состав базы — отказ, а не пустой успех.
	baseSize, err := baseTreeSize(root, base)
	if err != nil {
		t.Fatalf("%s: %v", removedPathCensusSubject, err)
	}

	names, err := removedPathsBetween(root, base)
	if err != nil {
		t.Fatalf("перечислить снятые пути: %v", err)
	}
	removed, err := classifyRemovedPaths(root, base, names)
	if err != nil {
		t.Fatalf("разобрать снятое: %v", err)
	}

	ledger, err := readGateCarrierLedger(root)
	if err != nil {
		t.Fatalf("прочитать %s: %v", gateCarrierLedgerName, err)
	}

	findings, counts := judgeRemovedPathCensus(removed, ledger, scopeProbes{
		LiveUnder:   func(s string) (int, error) { return livePathsUnder(root, s) },
		EverRemoved: func(s string) (bool, error) { return carrierWasEverRemoved(root, s) },
	})

	// База печатается ЦЕЛИКОМ. Обрезка до десяти знаков верна для хеша и лжёт
	// на имени ссылки: `origin/release/kaname-tail` превращается в `origin/rel`,
	// то есть в координату, которой нет.
	t.Logf("осмотрено: база %s несёт путей %d; база выведена как %s; %s",
		base, baseSize, how, counts)

	// Контроль предиката носителя в ОБРАТНУЮ сторону: «носителей снято 0»
	// обязано быть отличимо от «признак носителя не срабатывает ни на чём».
	// Считается то же самое, что и у снятых, — по ЖИВОМУ дереву.
	if live, err := liveGateCarrierCount(root); err != nil {
		t.Fatalf("перепись живых носителей: %v", err)
	} else {
		t.Logf("контроль предиката: носителей гейта в ЖИВОМ дереве %d "+
			"(из них вне корпуса %d) — предикат видит предмет",
			live.Total, live.OutsideCorpus)
		if live.Total == 0 {
			t.Fatalf("признак носителя не сработал НИ НА ОДНОМ файле живого дерева — "+
				"это отказ, а не пустой успех: %q ничего не измерил",
				removedPathCensusSubject)
		}
	}

	for _, f := range findings {
		t.Error(f)
	}
}
