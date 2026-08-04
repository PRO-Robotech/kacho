// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// registry_delete_cascade_unregister_integration_test.go — удаление реестра обязано
// СНЯТЬ регистрации своих репозиториев, а не дать им исчезнуть каскадом.
//
// Предмет. Миграция 0014 объявляет несущее свойство: признак существования репозитория и
// намерение о нём пишутся одной транзакцией, «поэтому «намерение эмитировано» и «ресурс
// существует» не могут разъехаться by construction». Та же миграция объявляет FK
// `ON DELETE CASCADE` на реестр — и он это свойство отменяет ровно в одну сторону:
// удаление родителя убирает признак БЕЗ единого намерения о снятии. Ресурса нет, а
// снятия никто не эмитировал; authz-объект репозитория остаётся стоять со всем, что на
// нём было.
//
// Замер на стенде 2026-08-04: 479 регистраций репозиториев, 60 снятий; 419 объектов
// репозиториев остались в хранилище прав при нуле живых репозиториев и нуле строк
// признака — в точности разница, которую каскад унёс молча.
//
// Асимметрия здесь та же, что и у уцелевшего `owner`: выдача доезжает, снятие — нет, и
// отличить «работает» от «не отозвано» по наблюдаемому поведению нельзя.
package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// TestRegistryDelete_WithdrawsEveryChildRepositoryRegistration — удаление реестра
// эмитирует снятие на КАЖДЫЙ зарегистрированный под ним репозиторий.
//
// Утверждается ИСХОД для принимающей стороны: для каждого дочернего объекта в очереди
// лежит намерение о снятии. Утверждение «признак исчез» тут было бы зелёным и на дефекте —
// каскад его исправно убирает; именно поэтому проверяется очередь, а не признак.
func TestRegistryDelete_WithdrawsEveryChildRepositoryRegistration(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-cascade")

	children := []string{"team/app", "team/api", "infra/base"}
	for _, child := range children {
		require.NoError(t, repo.RegisterRepository(ctx,
			domain.RegisterIntentForRepoPush(regID, child, "prj-P", "service_account:sva-ci")))
		require.Equal(t, 1, countRegistration(t, pool, regID, child), "положительный контроль: признак записан")
	}

	require.NoError(t, repo.Delete(ctx, regID,
		domain.UnregisterIntentForDelete(regID, "prj-P")))

	for _, child := range children {
		require.Equal(t, 1, countOutbox(t, pool, regID+"/"+child, domain.FGAEventUnregister),
			"удаление реестра обязано эмитировать снятие дочернего репозитория %q: "+
				"каскад унёс признак его существования, и без этого намерения объект репозитория "+
				"остаётся в хранилище прав навсегда", child)
		require.Equal(t, 0, countRegistration(t, pool, regID, child),
			"признак снят (каскадом либо явно) — иначе он ссылался бы на несуществующий реестр")
	}

	require.Equal(t, 1, countOutbox(t, pool, regID, domain.FGAEventUnregister),
		"снятие самого реестра остаётся ровно одно")
}

// TestRegistryDelete_NoChildren_EmitsOnlyItsOwnWithdrawal — законный близнец той же формы.
//
// Реестр без зарегистрированных репозиториев обязан эмитировать РОВНО одно снятие — своё.
// Без этой пары предыдущее утверждение удовлетворялось бы «эмитировать снятие на всё
// подряд», а лишнее снятие — это снос authz-объекта, который ещё жив.
func TestRegistryDelete_NoChildren_EmitsOnlyItsOwnWithdrawal(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-cascade-empty")

	require.NoError(t, repo.Delete(ctx, regID,
		domain.UnregisterIntentForDelete(regID, "prj-P")))

	var total int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_registry.registry_outbox WHERE event_type = $1`,
		domain.FGAEventUnregister).Scan(&total))
	require.Equal(t, 1, total, "у реестра без репозиториев снятие ровно одно — своё")
}

// TestRegistryDelete_ChildWithdrawalIsAtomicWithTheDelete — снятия детей и удаление
// строки реестра либо применяются вместе, либо не применяются вовсе.
//
// Повторное удаление уже удалённого реестра не порождает второго набора снятий: намерения
// эмитируются только той транзакцией, что физически удалила строку (та же дисциплина, что
// у собственного снятия реестра).
func TestRegistryDelete_ChildWithdrawalIsAtomicWithTheDelete(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-cascade-idem")

	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, "team/app", "prj-P", "service_account:sva-ci")))
	require.NoError(t, repo.Delete(ctx, regID, domain.UnregisterIntentForDelete(regID, "prj-P")))
	require.Equal(t, 1, countOutbox(t, pool, regID+"/team/app", domain.FGAEventUnregister))

	// Повторное удаление: строки нет → ErrNotFound, второго снятия не появляется.
	require.Error(t, repo.Delete(ctx, regID, domain.UnregisterIntentForDelete(regID, "prj-P")))
	require.Equal(t, 1, countOutbox(t, pool, regID+"/team/app", domain.FGAEventUnregister),
		"повторное удаление не эмитирует дубль снятия ребёнка")
}
