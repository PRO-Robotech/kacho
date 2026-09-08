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

	// Законный близнец: обе стороны на месте — находок НЕТ.
	if got := ledgerFindings(synthTree(t, true, true), ledger); len(got) != 0 {
		t.Errorf("на целой ведомости гейт нашёл %d: %v — он краснеет на исправном", len(got), got)
	}

	// Один факт: нет каталога исходников.
	got := ledgerFindings(synthTree(t, false, true), ledger)
	if len(got) != 1 || !strings.Contains(got[0], "services/iam") {
		t.Errorf("снятый каталог исходников не назван координатой: %v", got)
	}

	// Один факт: нет чарта.
	got = ledgerFindings(synthTree(t, true, false), ledger)
	if len(got) != 1 || !strings.Contains(got[0], "charts/kaname") {
		t.Errorf("снятый чарт не назван координатой: %v", got)
	}

	// Пустая ведомость — цель, а не поломка: находок нет.
	if got := ledgerFindings(synthTree(t, true, true), map[string]string{}); len(got) != 0 {
		t.Errorf("пустая ведомость дала находки %v — идеал превращён в поломку", got)
	}
}
