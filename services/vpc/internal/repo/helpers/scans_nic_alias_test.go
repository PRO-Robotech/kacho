// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package helpers_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// Перечень колонок интерфейса живёт в ОДНОМ месте, а его алиасная форма из него
// ВЫВОДИТСЯ. Здесь проверяется, что вывод сохраняет и состав, и порядок: приёмники
// `ScanNI` позиционные, поэтому переставленная колонка — не косметика, а чтение
// одного значения в поле другого.
//
// Класс, ради которого это стоит: рукописная копия перечня уже разошлась с
// оригиналом — колонку добавили в один, не добавили в другой, и запрос упал на
// несовпадении числа колонок и приёмников. Проба зафиксировала бы расхождение
// раньше базы.

func cols(list string) []string {
	var out []string
	for _, raw := range strings.Split(list, ",") {
		if c := strings.TrimSpace(raw); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// TestNICColsAliased_PreservesCompositionAndOrder — состав и порядок сохранены,
// каждая колонка получила ровно один алиас.
func TestNICColsAliased_PreservesCompositionAndOrder(t *testing.T) {
	plain := cols(helpers.NICCols)
	aliased := cols(helpers.NICColsAliased("ni"))

	require.NotEmpty(t, plain, "перечень пуст — сверять нечего, и вердикт ниже беспредметен")
	require.Len(t, aliased, len(plain))
	for i := range plain {
		assert.Equal(t, "ni."+plain[i], aliased[i],
			"позиция %d: приёмники ScanNI позиционные, порядок — часть контракта", i)
	}
}

// TestNICColsAliased_RefusesAnExpression — предпосылка преобразования проверяется
// им самим: приписать алиас к выражению нельзя, и молчаливо испорченный SQL хуже
// отсутствия функции.
//
// Отрицание в паре с положительным: голое имя проходит, выражение — нет.
func TestNICColsAliased_RefusesAnExpression(t *testing.T) {
	assert.NotPanics(t, func() { _ = helpers.NICColsAliased("ni") },
		"на действующем перечне функция обязана работать — иначе отрицание ниже ничего не различает")
	assert.Panics(t, func() { _ = helpers.AliasColumns("ni", "id, count(*)") },
		"выражение в перечне обязано быть отказом, а не тихо испорченным запросом")
}
