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
// `VolumeService/Get` → `viewer`; то же значение несёт запись каталога шлюза).
//
// Здесь оно выписано отдельной константой НАМЕРЕННО: тест обязан провалиться,
// если предикат страницы разойдётся с чтением, — а не переехать вслед за ним.
// Репо-широкую сверку этого значения с каталогом делает
// `internal/repohygiene/listreadrelationparity_test.go`.
const readRelation = "viewer"

// Страница не может быть ШИРЕ чтения.
//
// Объект попадает в выдачу List по предикату `visibilityRelations`, а прочитать
// его одиночным Get можно по `readRelation`. Пока это РАЗНЫЕ множества, вызывающий
// узнаёт о существовании объекта, которого не вправе читать, — и, поскольку List
// отдаёт то же самое сообщение, что и Get, получает его СОДЕРЖИМОЕ целиком. Ярусные
// отношения (`viewer`/`editor`/`admin`) и глагольные (`v_*`) в модели развязаны: ни
// одно не выводится из другого (fga_model.fga). Этот домен гейтит чтение ЯРУСОМ,
// поэтому здесь тест держит границу с другой стороны: глагольный грант (`v_get`/
// `v_list`) сам по себе НЕ впускает объект в страницу.
//
// Отрицание идёт В ПАРЕ с положительным: одиночное «не видит» зеленеет сильнее
// всего, когда фильтр не видит НИЧЕГО.
func TestFGAFilter_PageMembershipRequiresReadRelation(t *testing.T) {
	const (
		outsiderObj = "vol_verb_only" // держит глаголы, не держит ярус чтения
		legitObj    = "vol_readable"  // держит ровно то, чем гейтится Get
	)

	cli := newFakeAuthorizeClient().
		allow("v_get", outsiderObj).
		allow("v_list", outsiderObj).
		allow(readRelation, legitObj)
	f := NewFGAFilter(cli, DefaultConfig())

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_x", ResourceTypeVolume, ActionVolumeList,
		[]string{outsiderObj, legitObj})
	require.NoError(t, err)

	assert.NotContains(t, got, outsiderObj,
		"объект, который вызывающему не даёт прочитать Get (%s), не может попасть в страницу List: "+
			"List отдаёт то же сообщение, что и Get, поэтому членство в странице раскрывает содержимое", readRelation)
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
