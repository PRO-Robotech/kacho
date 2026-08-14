// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// Две полосы учёта говорят ОДНО И ТО ЖЕ — потому что производитель отказа один.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), §7.4 и DoD S2 п.3, повторённый у storage в DoD S4 п.1.
//
// # Что здесь утверждается и почему это не тавтология
//
// Полос две по построению: совещательная отвечает арендатору синхронно, до
// создания операции, а авторитетная списывает место триггером внутри той же
// транзакции, что вставка. Написанные порознь, их тексты разошлись бы на первой
// правке, и расхождение НЕ БЫЛО БЫ ВИДНО ни одной из сторон: каждая по
// отдельности верна, а арендатор получал бы на один отказ два разных сообщения в
// зависимости от того, какая сработала первой.
//
// Проба сравнивает ответы обеих полос ДОСЛОВНО. Она проходит сегодня потому, что
// место одно (`kacho_quota_refuse`), и покраснеет ровно тогда, когда у отказа
// заведётся второй производитель — то есть утверждает конструкцию, а не
// совпадение двух литералов.

// TestQuotaBands_RefuseInTheSameWords — исчерпание и «потолок не назван»
// формулируются обеими полосами побайтово одинаково.
func TestQuotaBands_RefuseInTheSameWords(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	quotaRepo := pg.NewQuotaRepo(pool)
	ctx := context.Background()

	t.Run("исчерпание", func(t *testing.T) {
		const project = "prj-quota-bands-full"
		seedQuota(t, pool, project, "storage.volumes", 1)
		mkVolume(t, pool, repo, project, "vol-1", 1<<30)

		// Авторитетная полоса: отказ приезжает из вставки.
		_, _, authoritative := repo.Insert(ctx, &domain.Volume{
			ID:         ids.NewID(domain.PrefixVolume),
			ProjectID:  project,
			Name:       "vol-2",
			ZoneID:     "region-1-a",
			DiskTypeID: seededDiskType,
			SizeBytes:  1 << 30,
		}, "")
		require.Error(t, authoritative)

		// Совещательная полоса: тот же вопрос, ничего не занимая.
		advisory := quotaRepo.Admit(ctx, "project", project, "storage.volumes")
		require.Error(t, advisory)

		require.True(t, stderrors.Is(authoritative, storageerr.ErrQuotaExceeded))
		require.True(t, stderrors.Is(advisory, storageerr.ErrQuotaExceeded),
			"совещательная полоса обязана назвать ТУ ЖЕ полосу отказа: %v", advisory)
		require.Equal(t, authoritative.Error(), advisory.Error(),
			"тексты полос обязаны совпадать ПОБАЙТОВО: производитель отказа один, "+
				"и согласовывать тут нечего — согласовать можно только два места")
	})

	t.Run("потолок не назван", func(t *testing.T) {
		const project = "prj-quota-bands-none"

		_, _, authoritative := repo.Insert(ctx, &domain.Volume{
			ID:         ids.NewID(domain.PrefixVolume),
			ProjectID:  project,
			Name:       "vol-1",
			ZoneID:     "region-1-a",
			DiskTypeID: seededDiskType,
			SizeBytes:  1 << 30,
		}, "")
		require.Error(t, authoritative)

		advisory := quotaRepo.Admit(ctx, "project", project, "storage.volumes")
		require.Error(t, advisory)

		require.True(t, stderrors.Is(authoritative, storageerr.ErrQuotaNotProvisioned))
		require.True(t, stderrors.Is(advisory, storageerr.ErrQuotaNotProvisioned))
		require.Equal(t, authoritative.Error(), advisory.Error())
	})

	t.Run("положительный контроль: место есть — обе полосы молчат", func(t *testing.T) {
		const project = "prj-quota-bands-free"
		seedQuota(t, pool, project, "storage.volumes", 2)

		require.NoError(t, quotaRepo.Admit(ctx, "project", project, "storage.volumes"),
			"совещательная полоса на свободном месте не отказывает")
		mkVolume(t, pool, repo, project, "vol-1", 1<<30)
		require.NoError(t, quotaRepo.Admit(ctx, "project", project, "storage.volumes"),
			"после первого создания место ещё есть")
	})
}

// TestQuotaAdvisoryBand_TakesNoSlot — совещательная полоса ЧИТАЕТ и не занимает.
//
// Без этого утверждения «ранний отказ» был бы неотличим от раннего списания:
// вопрос, тратящий место, превратил бы предел в два — один на создание, второй
// на попытки.
func TestQuotaAdvisoryBand_TakesNoSlot(t *testing.T) {
	pool := newTestPool(t)
	quotaRepo := pg.NewQuotaRepo(pool)
	ctx := context.Background()
	const project = "prj-quota-bands-read"

	seedQuota(t, pool, project, "storage.volumes", 3)

	for i := 0; i < 5; i++ {
		require.NoError(t, quotaRepo.Admit(ctx, "project", project, "storage.volumes"))
	}
	used, limit := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(0), used, "пять вопросов не заняли ни одного места")
	require.Equal(t, int64(3), limit)
}

// TestQuotaMaterialise_IsIdempotentAndKeepsUsage — материализация заводит
// ТОЛЬКО отсутствующее.
//
// Повторный вызов на уже материализованном проекте обязан менять ноль строк:
// материализация зовётся на промахе, а промах под конкуренцией случается у
// нескольких запросов сразу. И она НЕ перезаписывает снимок: перезапись обнулила
// бы потребление у проекта, который уже что-то создал, — то есть механизм,
// призванный ограничивать, сам бы возвращал место.
func TestQuotaMaterialise_IsIdempotentAndKeepsUsage(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	quotaRepo := pg.NewQuotaRepo(pool)
	ctx := context.Background()
	const project = "prj-quota-materialise"

	rows := quotaRowsFor(project, 4)
	n, err := quotaRepo.Materialize(ctx, rows)
	require.NoError(t, err)
	require.Equal(t, int64(len(rows)), n, "первый вызов завёл все строки")

	mkVolume(t, pool, repo, project, "vol-1", 1<<30)
	usedBefore, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(1), usedBefore)

	n2, err := quotaRepo.Materialize(ctx, rows)
	require.NoError(t, err)
	require.Equal(t, int64(0), n2,
		"повторная материализация не заводит ничего — иначе «ноль заведённых» было бы "+
			"неотличимо от «не звали»")

	usedAfter, limitAfter := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(1), usedAfter,
		"повторная материализация НЕ обнулила потребление: снимок величины она не перезаписывает")
	require.Equal(t, int64(4), limitAfter)
}

// TestQuotaMaterialise_RejectsARowWithoutTheAccountMirror — строка без зеркала
// аккаунта не вставляется вовсе.
//
// Такая строка НЕВИДИМА аккаунтной дельте: изменение аккаунтной области её не
// найдёт, и она проживёт со старой величиной, а снаружи это неотличимо от
// исправной работы — дельта отчитается успехом, просто не тронув её. Поэтому
// состояние запрещено схемой, а не вниманием.
func TestQuotaMaterialise_RejectsARowWithoutTheAccountMirror(t *testing.T) {
	pool := newTestPool(t)
	quotaRepo := pg.NewQuotaRepo(pool)
	ctx := context.Background()

	rows := quotaRowsFor("prj-quota-no-account", 4)
	for i := range rows {
		rows[i].AccountID = ""
	}
	_, err := quotaRepo.Materialize(ctx, rows)
	require.Error(t, err, "строка учёта без зеркала аккаунта отвергается схемой")

	// Положительный контроль: та же строка с зеркалом проходит — то есть проба
	// отличает «отвергнуто зеркало» от «отвергнуто всё подряд».
	ok := quotaRowsFor("prj-quota-no-account", 4)
	n, err := quotaRepo.Materialize(ctx, ok)
	require.NoError(t, err)
	require.Equal(t, int64(len(ok)), n)
}
