// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// kacho#3328 — RepositoryCounts считает репозитории так же, как RepositoryDeclared
// решает их существование: строка наложения ЛИБО строка регистрации, имя — один раз.
func TestRepo_3328_RepositoryCountsUnionOfOverlayAndRegistration(t *testing.T) {
	pool := setupTestDB(t)
	regRepo := kachopg.NewRegistryRepo(pool)
	cfgRepo := kachopg.NewRepositoryConfigRepo(pool)
	ctx := context.Background()

	regA := seedRegistry(t, pool, "prj-P", "reg-count-a")
	regB := seedRegistry(t, pool, "prj-P", "reg-count-b")
	regEmpty := seedRegistry(t, pool, "prj-P", "reg-count-empty")
	regAside := seedRegistry(t, pool, "prj-P", "reg-count-aside")

	// regA: x — только наложение; y — наложение и регистрация; z — только регистрация.
	for _, name := range []string{"x", "y"} {
		_, _, err := cfgRepo.InsertConfig(ctx, newCfg(regA, name, domain.VisibilityPrivate, nil))
		require.NoError(t, err)
	}
	for _, name := range []string{"y", "z"} {
		require.NoError(t, regRepo.RegisterRepository(ctx,
			domain.RegisterIntentForRepoPush(regA, name, "prj-P", "service_account:sva-ci")))
	}
	// regB: одна регистрация; regAside — репозитории есть, но его не спрашивают.
	require.NoError(t, regRepo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regB, "app", "prj-P", "service_account:sva-ci")))
	require.NoError(t, regRepo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regAside, "app", "prj-P", "service_account:sva-ci")))

	got, err := regRepo.RepositoryCounts(ctx, []string{regA, regB, regEmpty})
	require.NoError(t, err)
	require.Equal(t, map[string]int32{regA: 3, regB: 1}, got,
		"regA: x ∪ y ∪ z = 3 (y — один раз); regB: 1; реестр без репозиториев и неспрошенный — без строки")

	// Снятие регистрации z (удалён последний тег) уменьшает число; y остаётся наложением.
	require.NoError(t, regRepo.UnregisterRepository(ctx, domain.UnregisterIntentForRepo(regA, "z")))
	require.NoError(t, regRepo.UnregisterRepository(ctx, domain.UnregisterIntentForRepo(regA, "y")))
	got, err = regRepo.RepositoryCounts(ctx, []string{regA})
	require.NoError(t, err)
	require.Equal(t, map[string]int32{regA: 2}, got, "x и y — наложения, z снят")

	got, err = regRepo.RepositoryCounts(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, got)
}
