// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// repository_lifecycle_projection_test.go — один и тот же репозиторий обязан
// выглядеть одинаково независимо от того, как его спросили: поштучным чтением или
// списком. Исчезаемость (durable / ephemeral) — часть публичного ответа, и её
// отсутствие в списочной выдаче означает, что клиент не может отличить
// долговременный репозиторий от одноразового, пока не запросит каждый по одному.
package handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// overlayCfg — наложение с одной долговременной строкой.
type overlayCfg struct{ rows []*domain.RepositoryConfig }

func (c *overlayCfg) GetConfig(_ context.Context, _, name string) (*domain.RepositoryConfig, error) {
	for _, r := range c.rows {
		if r.Name == name {
			return r, nil
		}
	}
	return nil, regerrors.ErrNotFound
}

func (c *overlayCfg) ListConfigs(context.Context, string) ([]*domain.RepositoryConfig, error) {
	return append([]*domain.RepositoryConfig(nil), c.rows...), nil
}

func (c *overlayCfg) InsertConfig(context.Context, *domain.RepositoryConfig, ...registry.OutboxIntent) (*domain.RepositoryConfig, error) {
	return nil, regerrors.ErrUnavailable
}

func (c *overlayCfg) UpdateConfig(context.Context, registry.RepositoryConfigUpdate, ...registry.OutboxIntent) (*domain.RepositoryConfig, error) {
	return nil, regerrors.ErrUnavailable
}

func (c *overlayCfg) RekeyConfig(context.Context, string, string, string, ...registry.OutboxIntent) (*domain.RepositoryConfig, error) {
	return nil, regerrors.ErrUnavailable
}

func (c *overlayCfg) DeleteConfig(context.Context, string, string, ...registry.OutboxIntent) error {
	return regerrors.ErrUnavailable
}

// lifecycleZot — fakeZotH, у которого поштучная проекция ОТВЕЧАЕТ по тем же репо, что
// и списочная (базовая заглушка всегда отдаёт «проекции нет», из-за чего поштучное
// чтение не нашло бы ephemeral-репозиторий и сравнивать было бы нечего).
type lifecycleZot struct{ *fakeZotH }

func (z lifecycleZot) RepositoryProjection(_ context.Context, registryID, repository string) (*domain.Repository, error) {
	for _, r := range z.repos {
		if r.Name == repository {
			cp := *r
			cp.RegistryID = registryID
			return &cp, nil
		}
	}
	return nil, nil
}

// newLifecycleHandler — хендлер над реестром с двумя репозиториями: "durable"
// (строка наложения + теги в движке) и "ephemeral" (только теги, строки нет).
func newLifecycleHandler(t *testing.T) *RegistryHandler {
	t.Helper()
	cfg := &overlayCfg{rows: []*domain.RepositoryConfig{{
		RegistryID: validReg,
		Name:       "durable",
		Visibility: domain.VisibilityPrivate,
		Lifecycle:  domain.LifecycleDurable,
		CreatedAt:  time.Unix(1600000000, 0).UTC(),
	}}}
	zot := lifecycleZot{&fakeZotH{repos: []*domain.Repository{
		{RegistryID: validReg, Name: "durable", TagCount: 2},
		{RegistryID: validReg, Name: "ephemeral", TagCount: 1},
	}}}
	uc := registry.New(stubRepo{}, stubRepo{}, cfg, zot, stubIAM{}, stubGeo{}, stubRepo{},
		newMemOpsH(), "registry.kacho.local")
	az := &recordingAuthorizer{allow: map[string]bool{
		registryObjectRef(validReg):                true,
		repositoryObjectRef(validReg, "durable"):   true,
		repositoryObjectRef(validReg, "ephemeral"): true,
	}}
	return NewRegistryHandler(uc, az)
}

// Списочная выдача обязана нести ту же исчезаемость, что и поштучное чтение.
func TestHandler_ListRepositories_CarriesLifecycle_SameAsGet(t *testing.T) {
	h := newLifecycleHandler(t)

	resp, err := h.ListRepositories(carolCtx(), &registryv1.ListRepositoriesRequest{RegistryId: validReg})
	require.NoError(t, err)

	inList := map[string]registryv1.RepositoryLifecycle{}
	for _, r := range resp.GetRepositories() {
		inList[r.GetName()] = r.GetLifecycle()
	}
	require.Contains(t, inList, "durable")
	require.Contains(t, inList, "ephemeral")

	for _, name := range []string{"durable", "ephemeral"} {
		one, gerr := h.GetRepository(carolCtx(),
			&registryv1.GetRepositoryRequest{RegistryId: validReg, Repository: name})
		require.NoError(t, gerr)
		require.Equalf(t, one.GetLifecycle(), inList[name],
			"репозиторий %s: поштучное чтение отдаёт %s, список — %s; один объект не может "+
				"выглядеть по-разному в зависимости от того, как его спросили",
			name, one.GetLifecycle(), inList[name])
	}
}

// И конкретные значения, а не только совпадение: список, отдающий «не задано» для
// обоих, «совпадал» бы сам с собой, если бы поштучное чтение тоже сломали.
func TestHandler_ListRepositories_LifecycleValuesAreAuthoritative(t *testing.T) {
	h := newLifecycleHandler(t)

	resp, err := h.ListRepositories(carolCtx(), &registryv1.ListRepositoriesRequest{RegistryId: validReg})
	require.NoError(t, err)

	got := map[string]registryv1.RepositoryLifecycle{}
	for _, r := range resp.GetRepositories() {
		got[r.GetName()] = r.GetLifecycle()
	}
	require.Equal(t, registryv1.RepositoryLifecycle_DURABLE, got["durable"],
		"репозиторий со строкой наложения долговременный")
	require.Equal(t, registryv1.RepositoryLifecycle_EPHEMERAL, got["ephemeral"],
		"репозиторий без строки наложения одноразовый")
}
