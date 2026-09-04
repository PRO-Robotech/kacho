// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

// route_table_static_routes_cardinality_integration_test.go — DB-backstop потолка
// числа статических маршрутов (миграция 0028, CHECK
// route_tables_static_routes_cardinality).
//
// Зачем отдельно от пробы use-case: синхронная проверка ограничивает ОДИН
// запрос, прошедший через use-case, и ничего не утверждает о строке. CHECK
// ограничивает саму строку — то есть отвергает превышение у ЛЮБОГО writer'а,
// включая того, кто синхронную проверку не позвал. Проба обязана обращаться к
// репозиторию напрямую, иначе она снова мерила бы use-case.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/migrations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// staticRoutesFixture — n различающихся законных маршрутов: каждая запись сама
// по себе валидна, поэтому отказ на таком наборе может быть только по ДЛИНЕ.
func staticRoutesFixture(n int) []domain.StaticRoute {
	out := make([]domain.StaticRoute, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, domain.StaticRoute{
			DestinationPrefix: fmt.Sprintf("10.%d.%d.0/24", i/256, i%256),
			NextHopAddress:    "192.168.0.1",
		})
	}
	return out
}

func TestIntegration_RouteTable_StaticRoutesCardinality_DBCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	defer r.Close()

	// Parent Network — FK route_tables.network_id → networks(id).
	netID := ids.NewID(ids.PrefixNetwork)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, &domain.Network{
			ID: netID, ProjectID: "prj-rt-card", Name: domain.RcNameVPC("net-rt-card"),
		})
		return e
	}))

	// Ровно на потолке — проходит. Положительный контроль: без него отрицания
	// ниже зеленели бы и на CHECK'е, отвергающем любой непустой набор.
	atCap := ids.NewID(ids.PrefixRouteTable)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Insert(ctx, &domain.RouteTable{
			ID: atCap, ProjectID: "prj-rt-card", NetworkID: netID,
			Name:         domain.RcNameVPC("rt-at-cap"),
			StaticRoutes: staticRoutesFixture(domain.MaxStaticRoutes),
		})
		return e
	}))

	// Выше потолка на Insert — отвергнуто CHECK'ом (23514 → InvalidArgument).
	over := ids.NewID(ids.PrefixRouteTable)
	err = legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Insert(ctx, &domain.RouteTable{
			ID: over, ProjectID: "prj-rt-card", NetworkID: netID,
			Name:         domain.RcNameVPC("rt-over-cap"),
			StaticRoutes: staticRoutesFixture(domain.MaxStaticRoutes + 1),
		})
		return e
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, repo.ErrInvalidArg, "over-cap Insert → 23514 → InvalidArgument")

	// Выше потолка на Update (путь Update с маской static_routes) — тоже отвергнуто.
	err = legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Update(ctx, &domain.RouteTable{
			ID: atCap, ProjectID: "prj-rt-card", NetworkID: netID,
			Name:         domain.RcNameVPC("rt-at-cap"),
			StaticRoutes: staticRoutesFixture(domain.MaxStaticRoutes + 1),
		})
		return e
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, repo.ErrInvalidArg, "over-cap Update → 23514 → InvalidArgument")

	// Строка не пострадала — отвергнутая TX откатилась целиком.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.RouteTables().Get(ctx, atCap)
	require.NoError(t, err)
	assert.Len(t, got.StaticRoutes, domain.MaxStaticRoutes)

	// Отсутствующая строка over-cap: отвергнутый Insert не оставил ресурса.
	_, err = rd.RouteTables().Get(ctx, over)
	assert.ErrorIs(t, err, repo.ErrNotFound)

	// Сужение набора потолком не блокируется (shrink всегда легален).
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Update(ctx, &domain.RouteTable{
			ID: atCap, ProjectID: "prj-rt-card", NetworkID: netID,
			Name:         domain.RcNameVPC("rt-at-cap"),
			StaticRoutes: staticRoutesFixture(3),
		})
		return e
	}))
}

// TestStaticRoutesCapMatchesDBConstraint — потолок в коде и потолок в БД суть
// ОДНА величина; разойдясь, они начинают отвергать разное, и расхождение видно
// только там, где вторая проверка отработала (то есть на стенде, а не в прогоне).
//
// Проба читает ТЕКСТ миграции, а не помнит число, поэтому правка одной стороны
// без другой краснеет здесь. Постгрес ей не нужен — она не пропускается под
// `-short`, в отличие от пробы выше: иначе единственное, что связывает две
// величины, исчезало бы вместе с контейнером.
func TestStaticRoutesCapMatchesDBConstraint(t *testing.T) {
	const migration = "0028_route_tables_static_routes_cardinality.sql"
	body, err := migrations.FS.ReadFile(migration)
	require.NoError(t, err, "миграция потолка обязана лежать под этим именем")
	want := fmt.Sprintf("jsonb_array_length(static_routes) <= %d", domain.MaxStaticRoutes)
	assert.True(t, strings.Contains(string(body), want),
		"миграция %s не несёт предиката %q — значит DB-backstop и domain.MaxStaticRoutes разошлись",
		migration, want)
}
