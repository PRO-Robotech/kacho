// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry_test

// list_repositories_bounded_test.go — стоимость страницы не зависит от размера
// реестра.
//
// Что было. Перечисление репозиториев на КАЖДОЙ странице читало ВСЕ строки
// наложения реестра целиком, вместе с метками, и материализовало их в память —
// ещё до того, как выбиралась полоса. Клиент, честно листающий по 50, оплачивал
// весь каталог 400 раз; при page_size=1 отношение «прочитано/отдано» равнялось
// размеру реестра. Квоты на число репозиториев в реестре нет вовсе, а создание
// репозитория дёшево — то есть множитель задаёт вызывающий.
//
// Почему это не «просто медленно» (второй тест, рядом в пакете движка): ответ
// движка читается под ограничителем размера, поэтому на достаточно большом реестре
// перечисление перестаёт работать НАВСЕГДА — при живом реестре и живых правах, с
// кодом, приглашающим повторить, хотя валидным он уже не станет.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// countingCfg — наложение, считающее ПРОЧИТАННЫЕ строки и полные сканы.
type countingCfg struct {
	*orderedCfg
	rowsRead  int
	fullScans int
}

func (c *countingCfg) ListConfigs(ctx context.Context, registryID string) ([]*domain.RepositoryConfig, error) {
	out, err := c.orderedCfg.ListConfigs(ctx, registryID)
	c.fullScans++
	c.rowsRead += len(out)
	return out, err
}

func (c *countingCfg) ListConfigsExcludingNames(ctx context.Context, registryID string, excluded []string, offset, limit int) ([]*domain.RepositoryConfig, error) {
	out, err := c.orderedCfg.ListConfigsExcludingNames(ctx, registryID, excluded, offset, limit)
	c.rowsRead += len(out)
	return out, err
}

func (c *countingCfg) ConfigsByNames(ctx context.Context, registryID string, names []string) ([]*domain.RepositoryConfig, error) {
	out, err := c.orderedCfg.ConfigsByNames(ctx, registryID, names)
	c.rowsRead += len(out)
	return out, err
}

// TestListRepositories_PageCostIsBoundedByPageSize — страница окна проекции читает
// строки наложения ТОЛЬКО для имён этого окна, а не весь реестр.
func TestListRepositories_PageCostIsBoundedByPageSize(t *testing.T) {
	const total = 500
	zot := &pageZot{tagCount: 3}
	base := &orderedCfg{}
	for i := range total {
		name := fmt.Sprintf("app-%03d", i)
		zot.projected = append(zot.projected, name)
		base.rows = append(base.rows, &domain.RepositoryConfig{
			RegistryID: "reg-A", Name: name, Lifecycle: domain.LifecycleDurable,
			Visibility: domain.VisibilityPrivate, CreatedAt: time.Unix(1600000000, 0).UTC(),
		})
	}
	cfg := &countingCfg{orderedCfg: base}
	uc := registry.New(&mockRepo{}, &mockRepo{}, cfg, zot, &mockIAM{}, &mockGeo{},
		&mockRepoReg{}, newMemOps(), "registry.kacho.local")

	page, next, err := uc.ListRepositories(context.Background(),
		registry.RepoListQuery{RegistryID: "reg-A", PageSize: 10})
	require.NoError(t, err)
	require.Len(t, page, 10)
	require.NotEmpty(t, next)

	require.Zero(t, cfg.fullScans,
		"страница не вправе читать весь каталог наложения: стоимость обязана зависеть "+
			"от page_size, а не от размера реестра")
	require.LessOrEqual(t, cfg.rowsRead, 20,
		"прочитано %d строк наложения на страницу в 10 — стоимость страницы всё ещё "+
			"пропорциональна размеру реестра", cfg.rowsRead)
}

// TestListRepositories_DurableLaneReadsInBoundedBatches — полоса наложения
// (долговременные строки без единого тега) тоже не материализует весь каталог:
// читает пакетами, а не «весь набор → нарезать окно».
func TestListRepositories_DurableLaneReadsInBoundedBatches(t *testing.T) {
	const total = 400
	zot := &pageZot{tagCount: 0} // движок не помнит ни одного имени → все строки durable-empty
	base := &orderedCfg{}
	for i := range total {
		base.rows = append(base.rows, &domain.RepositoryConfig{
			RegistryID: "reg-A", Name: fmt.Sprintf("empty-%03d", i), Lifecycle: domain.LifecycleDurable,
			Visibility: domain.VisibilityPrivate, CreatedAt: time.Unix(1600000000, 0).UTC(),
		})
	}
	cfg := &countingCfg{orderedCfg: base}
	uc := registry.New(&mockRepo{}, &mockRepo{}, cfg, zot, &mockIAM{}, &mockGeo{},
		&mockRepoReg{}, newMemOps(), "registry.kacho.local")

	page, next, err := uc.ListRepositories(context.Background(),
		registry.RepoListQuery{RegistryID: "reg-A", PageSize: 10})
	require.NoError(t, err)
	require.Len(t, page, 10)
	require.NotEmpty(t, next, "полоса не исчерпана — курсор обязан продолжать")

	require.Zero(t, cfg.fullScans, "полоса наложения читает пакетами, а не весь каталог")
	require.LessOrEqual(t, cfg.rowsRead, 40,
		"прочитано %d строк наложения на страницу в 10", cfg.rowsRead)
}
