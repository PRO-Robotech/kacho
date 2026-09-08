// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestQuotaSyncStartsUnderTheDeclaration — приёмка KAN-QUOTA-1, DoD S1 п.6.
//
// Каждый потребитель ребра величин заводит фонового тянущего ПОД ОБЪЯВЛЕНИЕМ
// домена величин, а не под наличием соединения соседа. Решение принимает
// `quota.StartLimitSync`, читая объявление; всякое объемлющее условие означает,
// что потребитель решил ещё раз и по своему признаку.
//
// Перепись печатает ОБЕ величины: потребителей и мест подъёма. Одно число
// скрыло бы ровно тот случай, ради которого гейт заведён, — службу, у которой
// подъёма нет вовсе.
func TestQuotaSyncStartsUnderTheDeclaration(t *testing.T) {
	root := repoRoot(t)

	consumers, err := quotaConsumers(root)
	require.NoError(t, err)
	require.NotEmpty(t, consumers,
		"обход беспредметен: служб, несущих таблицу %s, не найдено ни одной — "+
			"«ноль находок» здесь неотличимо от «ноль прочитанного»", quotaCursorTable)

	sites := 0
	for _, svc := range consumers {
		found, serr := quotaStartSitesOf(root, svc)
		require.NoError(t, serr)
		require.Len(t, found, 1,
			"служба %s несёт таблицу курсора дельты, значит тянущий у неё обязан быть "+
				"заведён ровно один раз; найдено мест подъёма: %d", svc, len(found))
		site := found[0]
		require.False(t, site.Conditional,
			"%s:%d — подъём тянущего стоит под `%s`. Решение «заводить ли» принимает "+
				"quota.StartLimitSync, читая объявление домена величин; объемлющее условие "+
				"означает, что потребитель решил ещё раз и по своему признаку — обычно по "+
				"наличию соединения соседа. Именно это ломает пять служб при снятии "+
				"авторитета: отказ сборки тянущего у них фатален",
			site.File, site.Line, site.Under)
		sites++
	}

	t.Logf("перепись: потребителей ребра величин %d, мест подъёма тянущего %d",
		len(consumers), sites)
}
