// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// treeroot_test.go — два общих помощника внешнего пакета проб: корень дерева и
// печать ключей переписи.
//
// # Откуда они здесь
//
// Оба приехали из носителя гейта «владелец считаемого вида обязан быть
// читателем пределов» (`quotareadergrant_test.go`). Предметом того гейта была
// ПАРА фактов, из которых второй — членство служебной учётки владельца в группе
// читателей — объявлялся только миграциями службы доступа. Служба вынесена
// отдельным продуктом, второго факта в дереве нет ни в одной форме (предикат:
// `git grep -ln quota_readers` даёт только сам гейт), и гейт снят.
//
// Помощники пережили его своим предметом: `repoRootFor` зовут 17 файлов пакета,
// `joinKeys` — гейт чтения квоты арендатором. Ни один из них со службой доступа
// не связан.
//
// Файл назван по тому, что он делает, а не по гейту, которого больше нет.
package repohygiene_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// repoRootFor — корень репозитория; пути перечней относительны ему.
func repoRootFor(t testing.TB) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	return root
}

// joinKeys — ключи переписи в устойчивом порядке, для печати в отчёт.
func joinKeys(m map[string]string) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
