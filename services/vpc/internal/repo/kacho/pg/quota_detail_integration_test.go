// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"
	vpcrepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// TestQuota_RefusalCarriesTheProducerAmounts — величины, посчитанные
// производителем отказа, доезжают из ЖИВОЙ базы до вызывающего (задача #1605).
//
// ПОЧЕМУ ЭТО ОБЯЗАНО БЫТЬ ИНТЕГРАЦИОННОЙ ПРОБОЙ, А НЕ ЮНИТОМ. Юнит подаёт
// `DETAIL` строкой, которую написали мы сами, и потому утверждает о НАШЕМ
// разборе. Здесь проверяется другое звено и единственное, которого юнит
// коснуться не может: что `RAISE ... USING DETAIL` доезжает до драйвера полем
// `pgconn.PgError.Detail`. Разойдись это допущение с действительностью — весь
// путь был бы исправен по коду и пуст по величинам, а на юнитах зелен.
func TestQuota_RefusalCarriesTheProducerAmounts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-detail"
	seedQuota(t, ctx, pool, project, "vpc.network", 1)

	// Заполняем предел: положительный контроль — под пределом проходит.
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Networks().Insert(ctx, newNetwork(project, "net-detail-under"))
	require.NoError(t, err, "создание под пределом обязано проходить")
	require.NoError(t, w.Commit())

	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.Networks().Insert(ctx, newNetwork(project, "net-detail-over"))
	require.Error(t, err, "создание сверх предела обязано отвергаться")
	require.ErrorIs(t, err, vpcrepo.ErrQuotaExceeded)

	d, ok := quotadetail.FromError(err)
	require.True(t, ok,
		"величины производителя обязаны доезжать до вызывающего: без них клиент "+
			"узнаёт предел только разбором прозы")

	assert.Equal(t, "project", d.CarrierType, "носитель НАЗЫВАЕТСЯ, а не подразумевается")
	assert.Equal(t, project, d.CarrierID)
	assert.Equal(t, "vpc.network", d.Kind)
	require.NotNil(t, d.Limit, "предел назван — значит доезжает")
	assert.Equal(t, int64(1), *d.Limit)
	require.NotNil(t, d.Used, "занятое посчитано базой — значит доезжает")
	assert.Equal(t, int64(1), *d.Used)
}

// TestQuota_NotProvisionedCarriesTheCarrierWithoutAmounts — полоса «потолок не
// назван» несёт носителя и вид и НЕ несёт величин.
//
// Парный отрицанию положительный контроль: без него утверждение «предела нет»
// зеленело бы и на потерянной `DETAIL` целиком.
func TestQuota_NotProvisionedCarriesTheCarrierWithoutAmounts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const bare = "prj-quota-detail-bare"
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Networks().Insert(ctx, newNetwork(bare, "net-detail-bare"))
	require.Error(t, err)
	require.ErrorIs(t, err, vpcrepo.ErrQuotaNotProvisioned)

	d, ok := quotadetail.FromError(err)
	require.True(t, ok, "носитель и вид обязаны доезжать и на этой полосе")

	assert.Equal(t, bare, d.CarrierID)
	assert.Equal(t, "vpc.network", d.Kind)
	assert.Nil(t, d.Limit, "предел не назван — величины нет, а не ноль")
	assert.Nil(t, d.Used)
}
