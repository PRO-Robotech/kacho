// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// kacho#3328 — Registry.repositoryCount несёт фактическое число репозиториев на Get и
// List; прежде ProtoRegistry поле не ставил, и оно было 0 всегда.

// countsRecorder отдаёт заданные числа и записывает каждый вызов: страница List обязана
// стоить ОДНОГО запроса чисел, а не запроса на реестр.
type countsRecorder struct {
	mu    sync.Mutex
	calls [][]string
	by    map[string]int32
	err   error
}

func (c *countsRecorder) fn(_ context.Context, ids []string) (map[string]int32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, append([]string(nil), ids...))
	if c.err != nil {
		return nil, c.err
	}
	out := map[string]int32{}
	for _, id := range ids {
		if n, ok := c.by[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func regRow(id string) *domain.Registry {
	return &domain.Registry{ID: id, ProjectID: "prj-P", Name: id, Status: domain.RegistryStatusActive}
}

func TestRegistry_3328_GetCarriesRepositoryCount(t *testing.T) {
	rec := &countsRecorder{by: map[string]int32{validRegID: 3}}
	repo := &mockRepo{
		getFn:    func(context.Context, string) (*domain.Registry, error) { return regRow(validRegID), nil },
		countsFn: rec.fn,
	}
	uc := newUC(repo, &mockZot{}, &mockIAM{}, newMemOps())

	r, err := uc.Get(context.Background(), validRegID)
	require.NoError(t, err)
	require.Equal(t, int32(3), uc.ProtoRegistry(r).GetRepositoryCount())
	require.Equal(t, [][]string{{validRegID}}, rec.calls)
}

func TestRegistry_3328_ListCarriesRepositoryCountsInOneQuery(t *testing.T) {
	const (
		regA = "regAAAA0000000000000"
		regB = "regBBBB0000000000000"
		regC = "regCCCC0000000000000"
	)
	rec := &countsRecorder{by: map[string]int32{regA: 2, regB: 5}}
	repo := &mockRepo{
		listFn: func(context.Context, registry.ListQuery) ([]*domain.Registry, string, error) {
			return []*domain.Registry{regRow(regA), regRow(regB), regRow(regC)}, "next", nil
		},
		countsFn: rec.fn,
	}
	uc := newUC(repo, &mockZot{}, &mockIAM{}, newMemOps())

	items, next, err := uc.List(context.Background(), registry.ListQuery{ProjectID: "prj-P"})
	require.NoError(t, err)
	require.Equal(t, "next", next)
	got := map[string]int32{}
	for _, r := range items {
		got[r.ID] = uc.ProtoRegistry(r).GetRepositoryCount()
	}
	require.Equal(t, map[string]int32{regA: 2, regB: 5, regC: 0}, got,
		"реестр без репозиториев несёт 0, прочие — своё число")
	require.Equal(t, [][]string{{regA, regB, regC}}, rec.calls, "числа страницы — одним запросом")
}

func TestRegistry_3328_EmptyListAsksNoCounts(t *testing.T) {
	rec := &countsRecorder{}
	uc := newUC(&mockRepo{countsFn: rec.fn}, &mockZot{}, &mockIAM{}, newMemOps())
	items, _, err := uc.List(context.Background(), registry.ListQuery{ProjectID: "prj-P"})
	require.NoError(t, err)
	require.Empty(t, items)
	require.Empty(t, rec.calls, "пустая страница чисел не спрашивает")
}

func TestRegistry_3328_CountsUnavailableFailsTheRead(t *testing.T) {
	rec := &countsRecorder{err: regerrors.ErrUnavailable}
	repo := &mockRepo{
		getFn: func(context.Context, string) (*domain.Registry, error) { return regRow(validRegID), nil },
		listFn: func(context.Context, registry.ListQuery) ([]*domain.Registry, string, error) {
			return []*domain.Registry{regRow(validRegID)}, "", nil
		},
		countsFn: rec.fn,
	}
	uc := newUC(repo, &mockZot{}, &mockIAM{}, newMemOps())

	_, err := uc.Get(context.Background(), validRegID)
	require.Equal(t, codes.Unavailable, codeOf(t, err), "число не прочитано — ответа с ложным 0 нет")
	_, _, err = uc.List(context.Background(), registry.ListQuery{ProjectID: "prj-P"})
	require.Equal(t, codes.Unavailable, codeOf(t, err))
}
