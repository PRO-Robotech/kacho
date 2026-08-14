// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// Учёт числа ресурсов арендатора у kacho-storage: списание, возврат, отказ.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S4 п.1 — механизм S2 повторён у storage. Сценарии
// QV2-11 (исчерпание), QV2-12 (возврат), QV2-13 (конкуренция), QV2-14
// (понижение предела), QV2-15 («потолок не назван»), QV2-40 (тот же механизм у
// второго домена).
//
// # Почему эти пробы заводят строку учёта САМИ
//
// Их предмет — поведение при заведённой и при ОТСУТСТВУЮЩЕЙ строке. Заведи им
// строку общая фикстура пакета (`quota_fixture_test.go`), они утверждали бы про
// состояние, которого не создавали, и «потолок не назван» стало бы невыразимо.
// Поэтому идентичности проектов здесь свои (`prj-quota-*`) и в перечень
// фикстуры не входят.

// quotaFixtureAccount — зеркало аккаунта у строк учёта этих проб.
//
// Непусто, потому что схема отвергает пустое, и отвергает правильно: строка без
// зеркала невидима аккаунтной дельте, а снаружи это неотличимо от исправной
// работы.
const quotaFixtureAccount = "acc-quota-probe"

// seedQuota заводит строку учёта ТЕМ ЖЕ оператором, которым пользуется живой
// путь материализации.
//
// Своего INSERT здесь нет намеренно: копия оператора разошлась бы с настоящим
// молча, и разошлась бы именно там, где расхождение не видно глазом, — на
// составе столбцов.
func seedQuota(t *testing.T, pool *pgxpool.Pool, project, kind string, limit int64) {
	t.Helper()
	n, err := pg.MaterializeQuotas(context.Background(), pool, []quota.Row{{
		CarrierType:   quota.CarrierProject,
		CarrierID:     project,
		Kind:          kind,
		Limit:         limit,
		SourceScope:   "DEFAULT",
		SourceScopeID: "",
		LimitRevision: 0,
		AccountID:     quotaFixtureAccount,
	}})
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "перепись: заведена ровно одна строка учёта")
}

// readQuota читает пару «занято и предел» — то, что видит арендатор.
func readQuota(t *testing.T, pool *pgxpool.Pool, project, kind string) (used, limit int64) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `
		SELECT used, limit_value FROM project_resource_quotas
		 WHERE carrier_type = $1 AND carrier_id = $2 AND kind = $3`,
		quota.CarrierProject, project, kind).Scan(&used, &limit)
	require.NoError(t, err)
	return used, limit
}

// TestQuota_ChargeRefundAndRefusal — QV2-11 и QV2-12 на уровне владельца.
//
// Одна проба на три утверждения намеренно: списание, отказ на исчерпании и
// возврат — это ОДИН механизм в трёх состояниях, и разнеси их по трём пробам,
// каждая заводила бы своё состояние заново, а связь «отказал → освободили →
// прошло» перестала бы утверждаться вовсе.
func TestQuota_ChargeRefundAndRefusal(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	const project = "prj-quota-charge"

	seedQuota(t, pool, project, "storage.volumes", 2)

	// Списание: две вставки укладываются в предел.
	mkVolume(t, pool, repo, project, "vol-1", 1<<30)
	second := mkVolume(t, pool, repo, project, "vol-2", 1<<30)

	used, limit := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(2), used, "списано ровно столько, сколько создано")
	require.Equal(t, int64(2), limit)

	// Исчерпание: третья вставка отвергается, и отказ ОТЛИЧИМ от сбоя.
	_, _, err := repo.Insert(context.Background(), &domain.Volume{
		ID:         ids.NewID(domain.PrefixVolume),
		ProjectID:  project,
		Name:       "vol-3",
		ZoneID:     "region-1-a",
		DiskTypeID: seededDiskType,
		SizeBytes:  1 << 30,
	}, "")
	require.Error(t, err)
	require.True(t, stderrors.Is(err, storageerr.ErrQuotaExceeded),
		"исчерпание предела — свой sentinel, а не общий отказ хранилища: %v", err)
	require.Contains(t, err.Error(), project,
		"текст отказа называет носителя — он часть контракта")
	require.Contains(t, err.Error(), "storage.volumes",
		"текст отказа называет вид")

	usedAfterRefusal, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(2), usedAfterRefusal,
		"отвергнутая вставка места НЕ занимает: списание и вставка — одна транзакция")

	// Возврат: удаление освобождает место, и следующая вставка проходит.
	require.NoError(t, repo.Delete(context.Background(), second.ID))
	usedAfterDelete, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(1), usedAfterDelete, "удаление вернуло место")

	mkVolume(t, pool, repo, project, "vol-4", 1<<30)
	usedAfterReuse, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(2), usedAfterReuse, "освободившееся место переиспользовано")
}

// TestQuota_NotProvisionedIsRefusalNotPermission — QV2-15 на уровне владельца.
//
// Отсутствие строки учёта — ОТКАЗ, и он отличим от исчерпания. Обратная
// трактовка («нет строки ⇒ без предела») уже существовала в дереве у предела
// машин и измерена как механизм, не отказавший ни разу за всю свою жизнь.
func TestQuota_NotProvisionedIsRefusalNotPermission(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	const project = "prj-quota-unprovisioned"

	_, _, err := repo.Insert(context.Background(), &domain.Volume{
		ID:         ids.NewID(domain.PrefixVolume),
		ProjectID:  project,
		Name:       "vol-1",
		ZoneID:     "region-1-a",
		DiskTypeID: seededDiskType,
		SizeBytes:  1 << 30,
	}, "")
	require.Error(t, err, "проект без потолка НЕ создаёт ресурс")
	require.True(t, stderrors.Is(err, storageerr.ErrQuotaNotProvisioned),
		"«потолок не назван» — свой sentinel: %v", err)
	require.False(t, stderrors.Is(err, storageerr.ErrQuotaExceeded),
		"он НЕ равен исчерпанию: администратору эти два состояния велят разное")

	// Положительный контроль: тот же проект с заведённым потолком создаёт.
	seedQuota(t, pool, project, "storage.volumes", 1)
	mkVolume(t, pool, repo, project, "vol-1", 1<<30)
	used, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(1), used)
}

// TestQuota_ConcurrentInsertsTakeExactlyTheLastSlot — QV2-13.
//
// Гонка — предмет пробы, а не её помеха: потолок держит предикат условного
// UPDATE, и доказать это можно только конкуренцией. Чтение с последующим
// сравнением пропустило бы обе вставки, увидев одно и то же свободное место.
func TestQuota_ConcurrentInsertsTakeExactlyTheLastSlot(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	const project = "prj-quota-race"

	seedQuota(t, pool, project, "storage.volumes", 1)

	const writers = 5
	var ok, exhausted atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, _, err := repo.Insert(context.Background(), &domain.Volume{
				ID:         ids.NewID(domain.PrefixVolume),
				ProjectID:  project,
				Name:       fmt.Sprintf("vol-race-%d", i),
				ZoneID:     "region-1-a",
				DiskTypeID: seededDiskType,
				SizeBytes:  1 << 30,
			}, "")
			switch {
			case err == nil:
				ok.Add(1)
			case stderrors.Is(err, storageerr.ErrQuotaExceeded):
				exhausted.Add(1)
			default:
				t.Errorf("неожиданный отказ: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	require.Equal(t, int64(1), ok.Load(), "последнее место занимает ровно один писатель")
	require.Equal(t, int64(writers-1), exhausted.Load(),
		"остальные получают исчерпание, а не сбой")

	used, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(1), used, "счётчик равен числу созданных, а не числу попыток")
}

// TestQuota_LoweringTheLimitBelowUsageIsAllowed — QV2-14.
//
// Понижение предела ниже потребления — ШТАТНОЕ административное действие.
// `CHECK (used <= limit_value)` сделал бы его невыразимым: администратор,
// желающий ограничить проект, получал бы отказ, пока проект сам не освободит
// место, то есть административное действие становилось бы заложником того, кого
// оно ограничивает.
func TestQuota_LoweringTheLimitBelowUsageIsAllowed(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	const project = "prj-quota-lower"

	seedQuota(t, pool, project, "storage.volumes", 5)
	created := make([]*domain.Volume, 0, 3)
	for i := 0; i < 3; i++ {
		created = append(created, mkVolume(t, pool, repo, project, fmt.Sprintf("vol-%d", i), 1<<30))
	}

	// Понижение проходит — схема состояние `used > limit_value` допускает.
	_, err := pool.Exec(ctx, `
		UPDATE project_resource_quotas SET limit_value = 1
		 WHERE carrier_type = $1 AND carrier_id = $2 AND kind = $3`,
		quota.CarrierProject, project, "storage.volumes")
	require.NoError(t, err, "понижение предела ниже потребления отвергнуто — CHECK вернулся")

	used, limit := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(3), used)
	require.Equal(t, int64(1), limit)

	// Новые нельзя.
	_, _, err = repo.Insert(ctx, &domain.Volume{
		ID:         ids.NewID(domain.PrefixVolume),
		ProjectID:  project,
		Name:       "vol-over",
		ZoneID:     "region-1-a",
		DiskTypeID: seededDiskType,
		SizeBytes:  1 << 30,
	}, "")
	require.True(t, stderrors.Is(err, storageerr.ErrQuotaExceeded), "%v", err)

	// Старые живут.
	for _, v := range created {
		got, gerr := repo.Get(ctx, v.ID)
		require.NoError(t, gerr, "созданное до понижения продолжает читаться")
		require.Equal(t, v.ID, got.ID)
	}

	// Удаление работает и возвращает место.
	require.NoError(t, repo.Delete(ctx, created[0].ID))
	usedAfter, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(2), usedAfter)
}

// TestQuota_EveryTenantKindOfTheDomainIsCharged — QV2-40.
//
// Механизм ПАРАМЕТРИЗОВАН видом, а не написан под том. Проба идёт по всем трём
// видам домена: вид, у которого триггера нет, отказал бы «потолок не назван»
// при заведённой строке — то есть тихо остался бы неограниченным.
func TestQuota_EveryTenantKindOfTheDomainIsCharged(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	const project = "prj-quota-kinds"

	for _, kind := range []string{"storage.volumes", "storage.snapshots", "storage.images"} {
		seedQuota(t, pool, project, kind, 1)
	}

	volRepo := pg.NewVolumeRepo(pool)
	snapRepo := pg.NewSnapshotRepo(pool)

	vol := mkVolume(t, pool, volRepo, project, "vol-kinds", 1<<30)
	usedVol, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(1), usedVol, "том списан")

	snap := mkSnapshot(t, snapRepo, project, "snap-kinds", vol.ID)
	usedSnap, _ := readQuota(t, pool, project, "storage.snapshots")
	require.Equal(t, int64(1), usedSnap, "снимок списан")

	// Снимок рождается СОЗДАВАЕМЫМ — годным его объявляет сверщик. Образ из
	// негодного снимка не заводится вовсе, поэтому подтверждение здесь не
	// украшение сценария, а его предусловие.
	confirmReady(t, pool, reconciler.KindSnapshot, snap.ID, 1<<30)

	// Второй снимок в том же проекте отвергается — предел на снимки ДЕЙСТВУЕТ.
	_, _, err := snapRepo.Insert(ctx, &domain.Snapshot{
		ID:             ids.NewID(domain.PrefixSnapshot),
		ProjectID:      project,
		Name:           "snap-kinds-2",
		SourceVolumeID: vol.ID,
	})
	require.True(t, stderrors.Is(err, storageerr.ErrQuotaExceeded),
		"предел на снимки действует так же, как на тома: %v", err)

	// Образ: тот же механизм, третий вид.
	imgRepo := pg.NewImageRepo(pool)
	mkImageFromSnapshot(t, pool, imgRepo, project, "img-kinds", "region-1", snap.ID)
	usedImg, _ := readQuota(t, pool, project, "storage.images")
	require.Equal(t, int64(1), usedImg, "образ списан")
}

// TestQuota_AttachingAVolumeMovesNoCounter — QV2-34 в переводе на домен storage.
//
// Привязка тома к машине — это НЕ появление ресурса: строка появляется в
// `volume_attachments`, а число томов проекта не меняется. Списать привязку
// значило бы, что предел на тома молча становится пределом на привязки, и
// арендатор, переставивший один том между машинами, тратил бы место дважды.
//
// Утверждение важно именно у storage: связующая таблица живёт ЗДЕСЬ (раскол
// блочного хранения оставил `volume_attachments` у владельца), несёт свой
// `project_id` и потому выглядит как кандидат на учёт ровно так же, как сами
// тома.
func TestQuota_AttachingAVolumeMovesNoCounter(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	const project = "prj-quota-attach"

	seedQuota(t, pool, project, "storage.volumes", 2)

	vol := mkVolume(t, pool, repo, project, "vol-attach", 1<<30)
	usedAfterCreate, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(1), usedAfterCreate)

	att := mkAttach(vol.ID, "ins-quota-attach0001", "/dev/vdb", false)
	att.ProjectID = project
	require.NoError(t, repo.Attach(ctx, att))

	usedAfterAttach, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(1), usedAfterAttach,
		"привязка счётчик НЕ двигает: ресурса не прибавилось")

	require.NoError(t, repo.Detach(ctx, vol.ID, "ins-quota-attach0001"))
	usedAfterDetach, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(1), usedAfterDetach,
		"отвязка счётчик тоже не двигает: ресурс никуда не делся")

	// Положительный контроль: НОВЫЙ том счётчик двигает — то есть проба
	// способна отличить «не считается» от «не считается ничего».
	mkVolume(t, pool, repo, project, "vol-attach-2", 1<<30)
	usedAfterSecond, _ := readQuota(t, pool, project, "storage.volumes")
	require.Equal(t, int64(2), usedAfterSecond)
}
