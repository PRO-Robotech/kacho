// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestQuotaShowcaseKnowsWhetherTheAuthorityIsDeployed — витрина занятого обязана
// отличать «домена величин нет» от «возможности нет» (#2515).
//
// Разбор предмета и трёх требований — в шапке `quotashowposture.go`; здесь он не
// пересказывается, чтобы два места об одном предмете не разошлись.
func TestQuotaShowcaseKnowsWhetherTheAuthorityIsDeployed(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	owners, census, err := quotaShowOwners(root)
	require.NoError(t, err, "без состава дерева вердикт недействителен")

	t.Logf("перепись: %s", census)

	// Пустой обход — не «находок нет», а «прочитано ноль»: вердикт беспредметен.
	require.NotZero(t, census.Services, "каталогов служб осмотрено ноль — обход пуст")
	require.NotZero(t, census.Files, "файлов корней прочитано ноль — обход пуст")
	require.NotZero(t, census.Showing,
		"витрину не выставляет ни один корень — предмета у гейта нет, и молчание "+
			"его ничего не значит")

	var findings []string
	for _, o := range owners {
		for _, v := range o.Violations() {
			findings = append(findings, o.Service+": "+v)
		}
	}
	require.Empty(t, findings,
		"корень, выставляющий витрину, обязан вывести посадку из объявления и "+
			"спросить про объявленное отсутствие:\n  %s", findings)
}
