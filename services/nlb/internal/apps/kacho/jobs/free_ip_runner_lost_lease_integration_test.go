// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Реконсайлер НЕ сносит строку, чья аренда потеряна, и НЕ клинит на ней очередь
// (#467).
//
// ПРЕДМЕТ. Путь удаления научили различать два состояния: «семейства нет» (ни
// адреса, ни ключа аренды — освобождать нечего) и «адрес выдан, ключа нет»
// (аренда есть, вернуть её нечем) — второе отвергается, строка остаётся в
// DELETING как зацепка. Реконсайлер этого различения не имел: он смотрит только
// на ключ, на пустом молча ничего не освобождает и УДАЛЯЕТ строку.
//
// ПОЧЕМУ ЭТО НЕ ПОВТОР УЖЕ ЗАКРЫТОГО. Отказ на пути удаления оставляет строку
// именно ДЛЯ реконсайлера — он единственный, кто подбирает застрявшие строки.
// Если он же её и сносит, тот отказ покупает один цикл реконсиляции и ничего
// больше: аренду всё равно теряют, только позже и тише. Со снятой строкой аренду
// не видит НИКТО — выборка идёт по `load_balancers` в DELETING/CREATING, а
// обратного поиска «что принадлежит этому балансировщику» у vpc нет.
//
// ОТКУДА БЕРЁТСЯ ТАКАЯ СТРОКА. Перекос запрещён миграцией 0035, но она объявлена
// NOT VALID — намеренно: уже записанные строки при выкатке не проверяются,
// потому что снос такой строки уничтожил бы последнюю зацепку к висящей аренде.
// Значит встретить её может ровно тот, кто разбирает застрявшие строки.
func TestFreeIP_FamilyWithAddressButNoLeaseID_KeptAndReported(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	for _, family := range []domain.IPVersion{domain.IPVersionV4, domain.IPVersionV6} {
		t.Run(string(family), func(t *testing.T) {
			ctx := context.Background()
			dsn := setupTestDB(t)
			pool, err := coredb.NewPool(ctx, dsn)
			require.NoError(t, err)
			pgtest.ClosePoolAtEnd(t, pool)

			// Испорченная — СТАРШЕ, то есть выборка взяла бы её первой.
			lostID := insertPreMigrationSkewedLB(t, ctx, pool, family, 20*time.Minute)
			// Здоровая рядом: она и доказывает, что очередь не заклинило.
			const healthyLease = "adr0000LOSTLEASEOK01"
			healthyID, _ := insertStuckLB(t, ctx, pool, domain.LBStatusDeleting,
				"auto", healthyLease, "", "", 10*time.Minute)

			var reported []string
			rel := &fakeReleaser{}
			r := NewFreeIPRunner(pool, rel, observability.NewSlogger(discardWriter{}), time.Second, time.Minute,
				WithPoisonObserver(func(id string) { reported = append(reported, id) }))

			n, err := r.reconcileOnce(ctx)
			require.NoError(t, err, "тик не прерывается: одна испорченная строка не блокирует очередь")

			assert.True(t, lbExists(t, ctx, pool, lostID),
				"строка обязана уцелеть: сняв её, аренду не найдёт ни реконсайлер, ни человек")
			assert.Equal(t, []string{lostID}, reported,
				"потерянный ключ аренды обязан быть НАБЛЮДАЕМ, а не тихо пропущен")

			// Освобождена РОВНО одна аренда — здоровая: по испорченной строке
			// освобождать нечем, и попыток по ней не делается.
			assert.Equal(t, []string{healthyLease}, rel.frees(),
				"вернулась ровно аренда здоровой строки, и никакая другая")

			// Здоровая разобрана, хотя испорченная стоит перед ней в очереди —
			// это и есть отсутствие head-of-line-блокировки.
			assert.Equal(t, 1, n, "здоровая строка обязана быть разобрана в том же тике")
			assert.False(t, lbExists(t, ctx, pool, healthyID), "здоровая строка снята")
			assert.Contains(t, rel.clears(), healthyLease, "аренда здоровой строки возвращена")

			// Повторный тик исхода не меняет: испорченная строка не клеймится
			// вовсе, поэтому не переизбирается первой и очередь не клинит.
			n2, err := r.reconcileOnce(ctx)
			require.NoError(t, err)
			assert.Equal(t, 0, n2, "разбирать больше нечего")
			assert.True(t, lbExists(t, ctx, pool, lostID), "испорченная строка по-прежнему цела")
		})
	}
}

// Положительный контроль: строка БЕЗ семейства вовсе (ни адреса, ни ключа) —
// законный вход, освобождать нечего, реконсайлер её сносит как и прежде.
//
// Без этого контроля предыдущая проба могла бы означать «оставляем всё подряд»,
// и реконсайлер перестал бы делать свою работу — застрявшие строки копились бы
// вместо того, чтобы разбираться.
func TestFreeIP_NoFamilyAtAll_StillDeletedWithoutRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	lbID, _ := insertStuckLB(t, ctx, pool, domain.LBStatusCreating, "", "", "", "", 10*time.Minute)

	var reported []string
	rel := &fakeReleaser{}
	r := NewFreeIPRunner(pool, rel, observability.NewSlogger(discardWriter{}), time.Second, time.Minute,
		WithPoisonObserver(func(id string) { reported = append(reported, id) }))

	n, err := r.reconcileOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "строка разобрана реконсайлером, а не оставлена")
	assert.False(t, lbExists(t, ctx, pool, lbID), "у строки нет ни одного семейства — сносим")
	assert.Empty(t, reported, "законный вход не объявляется испорченным")
	assert.Empty(t, rel.frees())
}

// insertPreMigrationSkewedLB — строка с адресом, но БЕЗ ключа аренды у одного
// семейства.
//
// Такое состояние запрещено миграцией 0035, поэтому фикстура снимает её
// ограничение на время вставки и возвращает тем же NOT VALID. Это не обход
// проверки ради удобства: ограничение объявлено NOT VALID именно затем, чтобы
// уже записанные перекошенные строки пережили выкатку, — а значит реконсайлер
// обязан уметь их встретить, и проверить это можно только такой строкой.
//
// Адрес берётся тем же помощником, что и у здоровых строк (`vipAddrFor`):
// диапазоны для документации, счётчик против per-region UNIQUE.
func insertPreMigrationSkewedLB(t testing.TB, ctx context.Context, pool *pgxpool.Pool,
	family domain.IPVersion, age time.Duration) (id string) {
	t.Helper()
	id = ids.NewID(ids.PrefixLoadBalancer)
	projectID := "prj01" + ids.NewUID()[:15]
	seedQuotaForProject(t, ctx, pool, projectID)

	var addrV4, addrV6 string
	if family == domain.IPVersionV6 {
		addrV6 = vipAddrFor(t, domain.IPVersionV6, "lost-lease-v6")
	} else {
		addrV4 = vipAddrFor(t, domain.IPVersionV4, "lost-lease-v4")
	}
	families := []string{string(family)}

	_, err := pool.Exec(ctx, `ALTER TABLE kacho_nlb.load_balancers
		DROP CONSTRAINT IF EXISTS load_balancers_v4_lease_id_present_check,
		DROP CONSTRAINT IF EXISTS load_balancers_v6_lease_id_present_check`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO kacho_nlb.load_balancers
			(id, project_id, name, region_id, type, status, placement_type, ip_families,
			 address_v4, address_id_v4, vip_origin_v4,
			 address_v6, address_id_v6, vip_origin_v6,
			 created_at, updated_at)
		VALUES ($1, $2, $1, 'region-1', 'INTERNAL', 'DELETING', 'REGIONAL', $3,
		        $4, '', 'auto', $5, '', 'auto', now() - $6::interval, now() - $6::interval)
	`, id, projectID, families, addrV4, addrV6, age.String())
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `ALTER TABLE kacho_nlb.load_balancers
		ADD CONSTRAINT load_balancers_v4_lease_id_present_check
		CHECK ((address_v4 = '') = (address_id_v4 = '')) NOT VALID`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ALTER TABLE kacho_nlb.load_balancers
		ADD CONSTRAINT load_balancers_v6_lease_id_present_check
		CHECK ((address_v6 = '') = (address_id_v6 = '')) NOT VALID`)
	require.NoError(t, err)
	return id
}
