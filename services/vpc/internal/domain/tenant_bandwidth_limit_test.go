// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// Правило приёма арендаторского ограничения полосы: обе границы и оба исхода
// признака профиля, каждый отрицательный — в паре с положительным контролем.
//
// Пара обязательна, потому что «отвергнуто» само по себе неотличимо от «отвергается
// всё»: правило, отвергающее любую величину, прошло бы каждую отрицательную пробу и
// было бы полностью сломанным.

const (
	floor   = domain.GuaranteedInterfaceBandwidthFloorMbps // 1000, опубликованный пол
	ceiling = 10000                                        // объявление СТЕНДА, не продукта
)

func declared() domain.BandwidthLimitPolicy {
	return domain.NewBandwidthLimitPolicy(true, ceiling)
}

func notDeclared() domain.BandwidthLimitPolicy {
	return domain.NewBandwidthLimitPolicy(false, ceiling)
}

// TestBandwidthLimit_RequiresTheCapability — без признака профиля величина
// отвергается, с признаком та же величина принимается.
func TestBandwidthLimit_RequiresTheCapability(t *testing.T) {
	t.Parallel()
	const legal = floor + 1

	err := notDeclared().Check(legal)
	require.Error(t, err, "стенд без умения обязан отвергать, а не принимать молча")
	require.ErrorIs(t, err, domain.ErrBandwidthLimitNotSettable)
	assert.Contains(t, err.Error(), "does not declare",
		"отказ обязан назвать ПРИЧИНУ: арендатор чинит настройку, а не гадает")

	// Положительный контроль — ровно та же величина при объявленном умении.
	require.NoError(t, declared().Check(legal),
		"с объявленным умением величина обязана проходить, иначе отрицание выше беспредметно")
}

// TestBandwidthLimit_UnsetIsAlwaysLegal — отсутствие просьбы не есть просьба:
// канонический ноль проходит на обоих стендах.
//
// Без этой пробы отказ «умение не объявлено» ловил бы КАЖДОЕ создание интерфейса
// на стенде без умения — то есть чинил бы поле ценой ресурса.
func TestBandwidthLimit_UnsetIsAlwaysLegal(t *testing.T) {
	t.Parallel()
	require.NoError(t, notDeclared().Check(domain.TenantBandwidthLimitUnset))
	require.NoError(t, declared().Check(domain.TenantBandwidthLimitUnset))
}

// TestBandwidthLimit_Bounds — обе границы проверены с обеих сторон.
func TestBandwidthLimit_Bounds(t *testing.T) {
	t.Parallel()
	p := declared()

	for _, tc := range []struct {
		name  string
		v     int64
		valid bool
		why   string
	}{
		{"на единицу ниже пола", floor - 1, false,
			"ниже гарантированного пола ограничение противоречит обещанию продукта"},
		{"ровно пол", floor, false,
			"граница СТРОГАЯ: ограничить ровно тем, что и так гарантировано, нечем"},
		{"на единицу выше пола", floor + 1, true,
			"первая осмысленная величина — она обязана проходить"},
		{"ровно потолок стенда", ceiling, true,
			"граница ВКЛЮЧАЮЩАЯ: «не больше гарантии» — законная просьба"},
		{"на единицу выше потолка", ceiling + 1, false,
			"выше объявленной гарантии ограничивать нечего — оно бы не подействовало"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := p.Check(tc.v)
			if tc.valid {
				require.NoError(t, err, tc.why)
				return
			}
			require.Error(t, err, tc.why)
			require.ErrorIs(t, err, domain.ErrBandwidthLimitOutOfRange)
			assert.False(t, errors.Is(err, domain.ErrBandwidthLimitNotSettable),
				"величина вне промежутка — не то же, что отсутствие умения: "+
					"клиент различает исходы по признаку, а не по прозе")
		})
	}
}

// TestBandwidthLimit_PolicyIsCanonicalWhenNotDeclared — «не объявлено» имеет ровно
// одно представление.
func TestBandwidthLimit_PolicyIsCanonicalWhenNotDeclared(t *testing.T) {
	t.Parallel()
	assert.Equal(t, domain.BandwidthLimitPolicy{}, notDeclared(),
		"объявленная гарантия при снятом умении не должна попадать в значение: "+
			"иначе у одного состояния два представления, и сравнение по одному "+
			"однажды разойдётся с другим")
	assert.False(t, notDeclared().Settable())
	assert.True(t, declared().Settable())
	assert.Equal(t, int64(ceiling), declared().CeilingMbps())
}
