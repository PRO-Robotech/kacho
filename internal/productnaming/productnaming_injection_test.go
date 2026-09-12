// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package productnaming_test

// productnaming_injection_test.go — доказательство, что гейт ведомости СПОСОБЕН
// упасть, и что он молчит на законном близнеце.
//
// Инъекция подаёт НАСТОЯЩИЙ вход той же функции, которую зовёт гейт
// (`ledgerFindings`), а не повторяет её логику своей копией: копия осталась бы
// зелёной ровно тогда, когда гейт перестал бы работать.
//
// Одно-фактность: миры отрицательных случаев отличаются от положительного
// РОВНО ОДНИМ фактом — отсутствием одного каталога.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// synthTree — синтетическое дерево с обеими сторонами одной записи.
func synthTree(t *testing.T, withService, withChart bool) string {
	t.Helper()
	root := t.TempDir()
	if withService {
		if err := os.MkdirAll(filepath.Join(root, "services", "iam"), 0o755); err != nil {
			t.Fatalf("синтетика не строится: %v", err)
		}
	}
	if withChart {
		if err := os.MkdirAll(
			filepath.Join(root, "deploy", "helm", "umbrella", "charts", "kaname"), 0o755); err != nil {
			t.Fatalf("синтетика не строится: %v", err)
		}
	}
	return root
}

func TestLedgerGateCanFail(t *testing.T) {
	ledger := map[string]string{"iam": "kaname"}
	none := map[string]string{}
	external := map[string]string{"iam": "PRO-Robotech/kaname"}

	// Законный близнец: обе стороны на месте, часть не вынесена — находок НЕТ.
	if got := ledgerFindingsWithExternal(synthTree(t, true, true), ledger, none); len(got) != 0 {
		t.Errorf("на целой ведомости гейт нашёл %d: %v — он краснеет на исправном", len(got), got)
	}

	// Один факт: нет каталога исходников, и вынос НЕ объявлен.
	got := ledgerFindingsWithExternal(synthTree(t, false, true), ledger, none)
	if len(got) != 1 || !strings.Contains(got[0], "services/iam") {
		t.Errorf("снятый каталог исходников не назван координатой: %v", got)
	}

	// Один факт: нет чарта.
	got = ledgerFindingsWithExternal(synthTree(t, true, false), ledger, none)
	if len(got) != 1 || !strings.Contains(got[0], "charts/kaname") {
		t.Errorf("снятый чарт не назван координатой: %v", got)
	}

	// Законный близнец разреза: каталога исходников нет И вынос ОБЪЯВЛЕН —
	// молчание. Без этой оси гейт краснел бы на верно исполненном разрезе, а
	// проверку, краснеющую на верной работе, отключают первой.
	if got := ledgerFindingsWithExternal(synthTree(t, false, true), ledger, external); len(got) != 0 {
		t.Errorf("объявленный вынос дал находки %v — гейт краснеет на верной работе", got)
	}

	// Обратная ось: вынос объявлен, а каталог исходников В ДЕРЕВЕ ЕСТЬ — часть
	// вернулась, объявление пережило свой предмет. Без этой оси послабление не
	// истекало бы никогда.
	got = ledgerFindingsWithExternal(synthTree(t, true, true), ledger, external)
	if len(got) != 1 || !strings.Contains(got[0], "часть вернулась") {
		t.Errorf("вернувшаяся часть не названа находкой: %v", got)
	}

	// Вынос объявлен, но и чарта нет: поставки не осталось — находка про чарт.
	got = ledgerFindingsWithExternal(synthTree(t, false, false), ledger, external)
	if len(got) != 1 || !strings.Contains(got[0], "charts/kaname") {
		t.Errorf("вынесенная часть без чарта не названа находкой: %v", got)
	}

	// Пустая ведомость — цель, а не поломка: находок нет.
	if got := ledgerFindingsWithExternal(synthTree(t, true, true), none, none); len(got) != 0 {
		t.Errorf("пустая ведомость дала находки %v — идеал превращён в поломку", got)
	}
}
