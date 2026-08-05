// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readRelation — отношение, на котором per-RPC Check энфорсит ЧТЕНИЕ одиночного
// объекта этого домена (`internal/check/permission_map.go`,
// `VolumeService/Get` → `v_get`; то же значение несёт запись каталога шлюза).
//
// Здесь оно выписано отдельной константой НАМЕРЕННО: тест обязан провалиться,
// если предикат страницы разойдётся с чтением, — а не переехать вслед за ним.
// Репо-широкую сверку этого значения с каталогом делает
// `internal/repohygiene/listreadrelationparity_test.go`.
const readRelation = "v_get"

// Страница не может быть ШИРЕ чтения.
//
// Объект попадает в выдачу List по предикату `visibilityRelations`, а прочитать
// его одиночным Get можно по `readRelation`. Пока это РАЗНЫЕ множества, вызывающий
// узнаёт о существовании объекта, которого не вправе читать, — и, поскольку List
// отдаёт то же самое сообщение, что и Get, получает его СОДЕРЖИМОЕ целиком. Ярусные
// отношения (`viewer`/`editor`/`admin`) и глагольные (`v_*`) в модели развязаны: ни
// одно не выводится из другого (fga_model.fga). Этот домен гейтит чтение ГЛАГОЛОМ,
// поэтому граница держится с ярусной стороны: ярусный грант (`viewer`/`editor`) сам
// по себе НЕ впускает объект в страницу.
//
// Сторона важна именно эта. Ярусный кортеж реконсайлер пишет на КАЖДЫЙ
// материализованный объект, а его значение выводится из класса глаголов правила,
// поэтому выдача, назвавшая один лишь `create`, несёт `editor` — и, будь предикат
// страницы ярусным, показывала бы содержимое всего, что этот субъект вправе только
// создавать. Плюс `v_list` без `v_get`: он остаётся отношением модели и гейтит
// вложенные списки, но членством в странице ПОЛНЫХ строк не является.
//
// Отрицание идёт В ПАРЕ с положительным: одиночное «не видит» зеленеет сильнее
// всего, когда фильтр не видит НИЧЕГО.
func TestFGAFilter_PageMembershipRequiresReadRelation(t *testing.T) {
	const (
		outsiderTier = "vol_tier_only" // держит ярус, не держит глагол чтения
		outsiderList = "vol_list_only" // держит `v_list`, не держит `v_get`
		legitObj     = "vol_readable"  // держит ровно то, чем гейтится Get
	)

	cli := newFakeAuthorizeClient().
		allow("viewer", outsiderTier).
		allow("editor", outsiderTier).
		allow("v_list", outsiderList).
		allow(readRelation, legitObj)
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeVolume, ActionVolumeList,
		[]string{outsiderTier, outsiderList, legitObj})
	require.NoError(t, err)

	for _, outsider := range []string{outsiderTier, outsiderList} {
		assert.NotContains(t, got, outsider,
			"объект, который вызывающему не даёт прочитать Get (%s), не может попасть в страницу List: "+
				"List отдаёт то же сообщение, что и Get, поэтому членство в странице раскрывает содержимое", readRelation)
	}
	assert.Contains(t, got, legitObj,
		"объект, который вызывающему даёт прочитать Get (%s), обязан быть в его странице: "+
			"иначе собственный читаемый ресурс невидим в своём же списке", readRelation)
}

// Предикат страницы спрашивается РОВНО тем отношением, которым гейтится чтение.
//
// Предыдущий тест смотрит на исход фильтрации; этот — на заданный вопрос. Без него
// «не видит» осталось бы неотличимо от «фильтр отказал всем по любой причине».
func TestFGAFilter_AsksExactlyTheReadRelation(t *testing.T) {
	cli := newFakeAuthorizeClient().allow(readRelation, "sto_a")
	f := NewFGAFilter(cli, DefaultConfig())

	_, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeVolume, ActionVolumeList,
		[]string{"sto_a", "sto_b"})
	require.NoError(t, err)

	require.NotEmpty(t, cli.gotReqs, "фильтр обязан спросить модель, а не решить сам")
	asked := map[string]struct{}{}
	for _, r := range cli.gotReqs {
		asked[r.GetRequiredRelation()] = struct{}{}
	}
	assert.Equal(t, map[string]struct{}{readRelation: {}}, asked,
		"предикат видимости страницы обязан совпадать с отношением чтения (%s); "+
			"любое ДОПОЛНИТЕЛЬНОЕ отношение расширяет страницу за пределы читаемого", readRelation)
}
