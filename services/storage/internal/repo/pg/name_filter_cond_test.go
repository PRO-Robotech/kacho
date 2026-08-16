// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/filter"
)

// storageListAliases — псевдонимы таблиц трёх списков этого сервиса. Проверять
// надо каждый: колонка уточнена псевдонимом, и ошибка в нём даёт предикат по
// чужой таблице, а не отказ.
var storageListAliases = []struct{ resource, alias string }{
	{"volume", "v"},
	{"image", "i"},
	{"snapshot", "s"},
}

func mustParseName(t *testing.T, expr string) *filter.FilterAST {
	t.Helper()
	ast, err := filter.Parse(expr, []string{"name"})
	require.NoError(t, err)
	require.NotNil(t, ast)
	return ast
}

// Запрос подстроки строит предикат ПОДСТРОКИ на колонке своего ресурса (#460).
func TestNameFilterCond_ContainsBecomesLike(t *testing.T) {
	for _, r := range storageListAliases {
		t.Run(r.resource, func(t *testing.T) {
			frag, args := nameFilterCond(r.alias, mustParseName(t, `name CONTAINS "prod"`), 1)
			assert.Equal(t, r.alias+".name LIKE $1", frag,
				"CONTAINS обязан строить предикат подстроки на колонке %s, никогда равенство", r.resource)
			assert.Equal(t, []any{"%prod%"}, args)
		})
	}
}

// Положительный контроль: равенство осталось равенством. Без него «подстрока
// работает» было бы неотличимо от «всё стало подстрокой», а это вторая половина
// того же дефекта — список отвечал бы шире, чем спросили.
func TestNameFilterCond_EqualsStaysEquals(t *testing.T) {
	for _, r := range storageListAliases {
		t.Run(r.resource, func(t *testing.T) {
			frag, args := nameFilterCond(r.alias, mustParseName(t, `name="prod"`), 1)
			assert.Equal(t, r.alias+".name = $1", frag)
			assert.Equal(t, []any{"prod"}, args)
		})
	}
}

// Подстановочные знаки, пришедшие ЗНАЧЕНИЕМ, экранируются: иначе `%` в строке
// поиска совпал бы со всем подряд, и ответ пришёл бы про другой набор строк, чем
// спросили — выглядя при этом результатом поиска.
func TestNameFilterCond_EscapesValueWildcards(t *testing.T) {
	_, args := nameFilterCond("v", mustParseName(t, `name CONTAINS "50%_x"`), 1)
	assert.Equal(t, []any{`%50\%\_x%`}, args)
}

// Номер placeholder'а берётся от вызывающего: предикат имени встаёт после
// сужения по проекту, и жёсткая `$1` увела бы значение в чужой аргумент.
func TestNameFilterCond_UsesCallerArgIndex(t *testing.T) {
	frag, args := nameFilterCond("v", mustParseName(t, `name="prod"`), 3)
	assert.Equal(t, "v.name = $3", frag)
	assert.Equal(t, []any{"prod"}, args)
}

// Сужения нет — предиката нет. Пустой фрагмент и есть сигнал вызывающему не
// добавлять условие; узел-пустышка добавил бы всегда-истинное условие и лишний
// аргумент.
func TestNameFilterCond_NilMeansNoPredicate(t *testing.T) {
	frag, args := nameFilterCond("v", nil, 1)
	assert.Equal(t, "", frag)
	assert.Empty(t, args)
}
