// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// dtSeededSlugs — пять слагов, которые засевала миграция 0004 и сняла 0016. Держим
// их списком, чтобы «каталог пуст» проверялось поимённо: пустой список у List и
// нерезолвящийся слаг у Get — разные утверждения, и второе ловит остаток посева,
// который первое пропустило бы при непустой странице по другой причине.
var dtSeededSlugs = []string{"block-standard", "block-balanced", "block-fast", "block-single", "block-io-max"}

// dtInsert заводит класс через репозиторий. Состояние обращения называется ЯВНО:
// репозиторий незаполненное поле не угадывает, а домен объявляет пустое состояние
// невалидным — умолчание проставляет край (disktype.CreateAdmin).
func dtInsert(t *testing.T, r *pg.DiskTypeRepo, d *domain.DiskType) *domain.DiskType {
	t.Helper()
	if d.Lifecycle == "" {
		d.Lifecycle = domain.LifecycleActive
	}
	created, err := r.Insert(context.Background(), d)
	require.NoError(t, err)
	return created
}

// dtBackend регистрирует бэкенд прямым INSERT. Репозиторий бэкендов заводится своей
// волной, а выводу способностей нужен ФАКТ существующей ревизии, а не путь, которым
// она заведена: привязка ссылается на бэкенд ограничительной внешней связью, поэтому
// без строки бэкенда ревизию не вставить вовсе.
func dtBackend(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO storage_backends (id, name, kind, endpoint, credentials_ref)
		 VALUES ($1, $1, 'CEPH_RBD', 'cfg://ceph/' || $1, 'vault://kacho/storage/' || $1)`, id)
	require.NoError(t, err)
	return id
}

// dtBinding вставляет ревизию привязки (класс, зона) с названными способностями.
// status — 'ACTIVE' либо 'SUPERSEDED': вытеснённая ревизия обязана остаться в
// таблице (append-only) и при этом не участвовать в выводе.
func dtBinding(t *testing.T, pool *pgxpool.Pool, id, diskTypeID, zoneID, backendID string,
	revision int, status string, c domain.Capabilities,
) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO disk_type_bindings
		   (id, disk_type_id, zone_id, backend_id, revision, pool, status,
		    cap_snapshots, cap_clone_from_snapshot, cap_clone_from_image,
		    cap_online_grow, cap_multi_attach, cap_encryption_at_rest)
		 VALUES ($1,$2,$3,$4,$5,'kacho-block',$6,$7,$8,$9,$10,$11,$12)`,
		id, diskTypeID, zoneID, backendID, revision, status,
		c.Snapshots, c.CloneFromSnapshot, c.CloneFromImage,
		c.OnlineGrow, c.MultiAttach, c.EncryptionAtRest)
	require.NoError(t, err)
}

// dtCapsAll — все способности объявлены. Служит вторым полюсом каждой пары: без
// него «ничего не объявлено» зеленело бы на реализации, не читающей привязки вовсе.
var dtCapsAll = domain.Capabilities{
	Snapshots: true, CloneFromSnapshot: true, CloneFromImage: true,
	OnlineGrow: true, MultiAttach: true, EncryptionAtRest: true,
}

// dtQueryCounter считает запросы на соединении. Нужен, чтобы «без N+1» было
// НАБЛЮДАЕМЫМ утверждением: форма запроса в коде читается глазами и переживает
// любую правку, а число запросов на страницу — нет.
type dtQueryCounter struct{ n atomic.Int64 }

func (c *dtQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *dtQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// dtTracedPool — пул на ОДНО соединение со счётчиком запросов. Одно соединение,
// потому что иначе установка второго добавила бы к счёту служебные запросы и
// измерялась бы не стоимость страницы, а поведение пула.
func dtTracedPool(t *testing.T) (*pgxpool.Pool, *dtQueryCounter) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test (testcontainers Postgres) — skipped with -short")
	}
	dsn := pgtest.NewDB(t) + "&pool_max_conns=1"
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	counter := &dtQueryCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	// Третий конструктор пула в пакете — и он тоже обязан завести строки учёта:
	// вставка строки ресурса списывает место независимо от того, считает ли
	// проба запросы. Счётчику это не мешает — он читается разностью вокруг
	// измеряемого вызова, а посев идёт до неё.
	seedFixtureQuotas(t, pool)
	return pool, counter
}

// TestDiskTypeGetNotFound — well-formed-но-нет → ErrNotFound "DiskType <id> not found".
func TestDiskTypeGetNotFound(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newBareTestPool(t))
	_, err := dr.Get(context.Background(), "dtp-nonexistent")
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "got %v", err)
	require.Equal(t, "DiskType dtp-nonexistent not found", err.Error()[len("not found: "):])
}

// TestDiskTypeCatalogEmptyAfterSeedRemoval (STOR-P-01) — посев снят, пустой каталог
// законен. Отрицание идёт В ПАРЕ с положительным контролем: после регистрации класса
// тот же List его отдаёт, иначе «пусто» зеленело бы на репозитории, который вообще
// ничего не читает.
func TestDiskTypeCatalogEmptyAfterSeedRemoval(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newBareTestPool(t))
	ctx := context.Background()

	page, next, err := dr.List(ctx, disktype.Pagination{PageSize: 50})
	require.NoError(t, err)
	require.Empty(t, page, "каталог свежей базы пуст: провайдер регистрирует классы сам")
	require.Empty(t, next)

	for _, slug := range dtSeededSlugs {
		_, gerr := dr.Get(ctx, slug)
		require.True(t, stderrors.Is(gerr, storageerr.ErrNotFound), "слаг посева %s не резолвится, got %v", slug, gerr)
	}

	dtInsert(t, dr, &domain.DiskType{ID: "block-registered", Name: "registered", PerformanceTier: domain.TierBalanced})
	page, _, err = dr.List(ctx, disktype.Pagination{PageSize: 50})
	require.NoError(t, err)
	require.Len(t, page, 1, "зарегистрированный класс виден — пустота выше про посев, а не про чтение")
	require.Equal(t, "block-registered", page[0].ID)
}

// TestDiskTypePolicyRoundTrip — класс несёт ПОЛИТИКУ: состояние обращения, границы
// размера и типизированный ярус. Читается и Get, и List: поле, живущее только в
// одном из двух чтений, — половина контракта, и расходится это молча.
func TestDiskTypePolicyRoundTrip(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newBareTestPool(t))
	ctx := context.Background()

	want := &domain.DiskType{
		ID: "block-policy", Name: "policy", Description: "с политикой",
		ZoneIDs:         []string{"ru-central1-a", "ru-central1-b"},
		PerformanceTier: domain.TierFast,
		Lifecycle:       domain.LifecycleDeprecated,
		Limits: domain.SizeLimits{
			MinSizeBytes: 1 << 30, MaxSizeBytes: 16 << 40, SizeStepBytes: 1 << 30,
		},
	}
	created := dtInsert(t, dr, want)
	require.Equal(t, domain.LifecycleDeprecated, created.Lifecycle)
	require.Equal(t, want.Limits, created.Limits)
	require.Equal(t, domain.TierFast, created.PerformanceTier)

	got, err := dr.Get(ctx, "block-policy")
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleDeprecated, got.Lifecycle)
	require.Equal(t, want.Limits, got.Limits)
	require.Equal(t, domain.TierFast, got.PerformanceTier)
	require.Equal(t, want.ZoneIDs, got.ZoneIDs)
	require.False(t, got.AcceptsNewVolumes(), "DEPRECATED новые тома не принимает")

	page, _, err := dr.List(ctx, disktype.Pagination{PageSize: 50})
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, domain.LifecycleDeprecated, page[0].Lifecycle, "список несёт то же состояние, что и Get")
	require.Equal(t, want.Limits, page[0].Limits, "список несёт те же границы, что и Get")
	require.Equal(t, domain.TierFast, page[0].PerformanceTier)
}

// TestDiskTypePolicyHeldByDBInvariants — политика энфорсится ОГРАНИЧЕНИЯМИ БД, а не
// проверкой в коде репозитория. Каждое отрицание — в паре с законным близнецом той же
// формы, иначе проба зеленела бы на реализации, отвергающей вообще всё.
func TestDiskTypePolicyHeldByDBInvariants(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newBareTestPool(t))
	ctx := context.Background()

	// Ярус — закрытый словарь. Свободная строка проходит мимо гейта проекции,
	// который читает ИМЕНА полей, поэтому канал закрыт по значениям.
	_, err := dr.Insert(ctx, &domain.DiskType{
		ID: "block-tier-bad", Name: "bad", PerformanceTier: "nvme-fast", Lifecycle: domain.LifecycleActive,
	})
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "ярус вне словаря отвергается БД, got %v", err)
	dtInsert(t, dr, &domain.DiskType{ID: "block-tier-ok", Name: "ok", PerformanceTier: domain.TierIOMax})

	// Пустое состояние обращения — не умолчание, а незаполненное поле: репозиторий
	// намерение админа не угадывает.
	_, err = dr.Insert(ctx, &domain.DiskType{ID: "block-lc-bad", Name: "bad", Lifecycle: ""})
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "пустое состояние отвергается, got %v", err)
	dtInsert(t, dr, &domain.DiskType{ID: "block-lc-ok", Name: "ok", Lifecycle: domain.LifecycleRetired})

	// Границы размера: минимум выше максимума и не кратный шагу минимум.
	_, err = dr.Insert(ctx, &domain.DiskType{
		ID: "block-lim-bad", Name: "bad", Lifecycle: domain.LifecycleActive,
		Limits: domain.SizeLimits{MinSizeBytes: 16 << 40, MaxSizeBytes: 1 << 30},
	})
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "min > max отвергается, got %v", err)
	_, err = dr.Insert(ctx, &domain.DiskType{
		ID: "block-step-bad", Name: "bad", Lifecycle: domain.LifecycleActive,
		Limits: domain.SizeLimits{MinSizeBytes: 3, MaxSizeBytes: 1 << 30, SizeStepBytes: 2},
	})
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "минимум не кратен шагу отвергается, got %v", err)
	dtInsert(t, dr, &domain.DiskType{
		ID: "block-lim-ok", Name: "ok", Lifecycle: domain.LifecycleActive,
		Limits: domain.SizeLimits{MinSizeBytes: 1 << 30, MaxSizeBytes: 16 << 40, SizeStepBytes: 1 << 30},
	})
}

// TestDiskTypeCapabilitiesIntersectActiveBindings — способности класса ВЫВОДЯТСЯ из
// его действующих ревизий привязки, а не хранятся на классе. Пересечение
// консервативное: способность объявляется, только если её несут ВСЕ действующие
// ревизии — класс предлагается в нескольких зонах, а зоны обслуживаются разными
// бэкендами, и объявить умение, которого нет в одной из них, значит соврать
// арендатору ровно там, где он этого не проверит.
func TestDiskTypeCapabilitiesIntersectActiveBindings(t *testing.T) {
	pool := newBareTestPool(t)
	dr := pg.NewDiskTypeRepo(pool)
	ctx := context.Background()
	be := dtBackend(t, pool, "sb-caps-1")

	// Одна ревизия — способности класса суть её способности.
	dtInsert(t, dr, &domain.DiskType{ID: "block-one", Name: "one"})
	dtBinding(t, pool, "dtb-one-a", "block-one", "ru-central1-a", be, 1, "ACTIVE",
		domain.Capabilities{Snapshots: true, OnlineGrow: true})

	// Две согласные ревизии в разных зонах — обе несут способность.
	dtInsert(t, dr, &domain.DiskType{ID: "block-agree", Name: "agree"})
	dtBinding(t, pool, "dtb-agree-a", "block-agree", "ru-central1-a", be, 1, "ACTIVE",
		domain.Capabilities{Snapshots: true, CloneFromImage: true})
	dtBinding(t, pool, "dtb-agree-b", "block-agree", "ru-central1-b", be, 1, "ACTIVE",
		domain.Capabilities{Snapshots: true, CloneFromImage: true})

	// Две расходящиеся: снимки несут обе, множественную привязку — только одна.
	dtInsert(t, dr, &domain.DiskType{ID: "block-split", Name: "split"})
	dtBinding(t, pool, "dtb-split-a", "block-split", "ru-central1-a", be, 1, "ACTIVE",
		domain.Capabilities{Snapshots: true, MultiAttach: true})
	dtBinding(t, pool, "dtb-split-b", "block-split", "ru-central1-b", be, 1, "ACTIVE",
		domain.Capabilities{Snapshots: true})

	// Ноль действующих ревизий: вытеснённая остаётся в таблице (append-only) и в
	// вывод не входит — иначе снятая политика продолжала бы обещать за бэкенд.
	dtInsert(t, dr, &domain.DiskType{ID: "block-none", Name: "none"})
	dtBinding(t, pool, "dtb-none-a", "block-none", "ru-central1-a", be, 1, "SUPERSEDED", dtCapsAll)

	one, err := dr.Get(ctx, "block-one")
	require.NoError(t, err)
	require.Equal(t, domain.Capabilities{Snapshots: true, OnlineGrow: true}, one.Capabilities)

	agree, err := dr.Get(ctx, "block-agree")
	require.NoError(t, err)
	require.Equal(t, domain.Capabilities{Snapshots: true, CloneFromImage: true}, agree.Capabilities)

	split, err := dr.Get(ctx, "block-split")
	require.NoError(t, err)
	require.True(t, split.Capabilities.Snapshots, "способность, которую несут обе ревизии, объявляется")
	require.False(t, split.Capabilities.MultiAttach, "способность одной ревизии из двух НЕ объявляется")
	require.NoError(t, split.RequireCapability(domain.CapSnapshots))
	require.EqualError(t, split.RequireCapability(domain.CapMultiAttach),
		"DiskType block-split does not support multiAttach")

	none, err := dr.Get(ctx, "block-none")
	require.NoError(t, err)
	require.Equal(t, domain.Capabilities{}, none.Capabilities, "при нуле действующих ревизий не объявляется ничего")

	// Список выводит то же самое: способность, видная только через Get, — второй
	// источник истины, который разойдётся молча.
	page, _, err := dr.List(ctx, disktype.Pagination{PageSize: 50})
	require.NoError(t, err)
	byID := map[string]*domain.DiskType{}
	for _, d := range page {
		byID[d.ID] = d
	}
	require.Len(t, byID, 4)
	require.Equal(t, one.Capabilities, byID["block-one"].Capabilities)
	require.Equal(t, agree.Capabilities, byID["block-agree"].Capabilities)
	require.Equal(t, split.Capabilities, byID["block-split"].Capabilities)
	require.Equal(t, domain.Capabilities{}, byID["block-none"].Capabilities)
}

// TestDiskTypeCapabilitiesReadWithoutPerRowQuery — вывод способностей стоит ОДИН
// запрос на страницу, а не один плюс по одному на класс. Утверждение
// наблюдаемое (счётчик запросов на соединении), потому что форма запроса в коде
// правку переживает, а стоимость страницы — нет.
func TestDiskTypeCapabilitiesReadWithoutPerRowQuery(t *testing.T) {
	pool, counter := dtTracedPool(t)
	dr := pg.NewDiskTypeRepo(pool)
	ctx := context.Background()
	be := dtBackend(t, pool, "sb-n1-1")

	const classes = 3
	for i := 0; i < classes; i++ {
		id := fmt.Sprintf("block-n1-%d", i)
		dtInsert(t, dr, &domain.DiskType{ID: id, Name: id})
		dtBinding(t, pool, "dtb-n1-"+id, id, "ru-central1-a", be, 1, "ACTIVE",
			domain.Capabilities{Snapshots: true})
	}

	before := counter.n.Load()
	page, _, err := dr.List(ctx, disktype.Pagination{PageSize: 50})
	require.NoError(t, err)
	require.Len(t, page, classes)
	for _, d := range page {
		require.True(t, d.Capabilities.Snapshots, "страница несёт выведенные способности")
	}
	require.EqualValues(t, 1, counter.n.Load()-before,
		"страница из %d классов стоит один запрос: добор способностей построчно — это N+1", classes)

	before = counter.n.Load()
	got, err := dr.Get(ctx, "block-n1-0")
	require.NoError(t, err)
	require.True(t, got.Capabilities.Snapshots)
	require.EqualValues(t, 1, counter.n.Load()-before, "чтение одного класса тоже стоит один запрос")
}

// TestDiskTypeUpdateNotRetroactiveForExistingVolumes (STOR-P-20) — правка класса не
// трогает уже созданные ресурсы. Отрицание в паре с положительным контролем: тот же
// узкий список зон отвергает НОВЫЙ том, иначе проба зеленела бы на правке, которая
// вообще ничего не изменила.
func TestDiskTypeUpdateNotRetroactiveForExistingVolumes(t *testing.T) {
	pool := newBareTestPool(t)
	dr := pg.NewDiskTypeRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	dtInsert(t, dr, &domain.DiskType{
		ID: "block-retro", Name: "retro", ZoneIDs: []string{"region-1-a", "region-1-b"},
		PerformanceTier: domain.TierBalanced, Lifecycle: domain.LifecycleActive,
	})
	offerDiskTypeInZone(t, pool, "block-retro", "region-1-a", "region-1-b")
	vol, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-retro",
		ZoneID: "region-1-b", DiskTypeID: "block-retro", SizeBytes: 1 << 30,
	}, "")
	require.NoError(t, err)
	before, err := vr.Get(ctx, vol.ID)
	require.NoError(t, err)

	retroName, retroDesc := "renamed", "суженный"
	retroZones, retroTier := []string{"region-1-a"}, domain.TierFast
	updated, err := dr.Update(ctx, "block-retro", disktype.DiskTypeUpdate{
		Name: &retroName, Description: &retroDesc, ZoneIDs: &retroZones, PerformanceTier: &retroTier,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"region-1-a"}, updated.ZoneIDs, "правка применилась")
	require.Equal(t, domain.TierFast, updated.PerformanceTier)

	after, err := vr.Get(ctx, vol.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "созданный том не изменён ни в одном поле правкой класса")

	_, _, err = vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-retro-new",
		ZoneID: "region-1-b", DiskTypeID: "block-retro", SizeBytes: 1 << 30,
	}, "")
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition),
		"НОВЫЙ том в снятой зоне отвергается — правка не была холостой, got %v", err)
	require.Equal(t, "DiskType block-retro is not offered in zone region-1-b",
		err.Error()[len("failed precondition: "):])
}

// TestDiskTypeCreateUpdateAdmin — admin Create (slug id, zone_ids/ярус) → Get; повтор
// того же id → AlreadyExists; правка, назвавшая все изменяемые поля (форма полного
// PATCH при пустой маске), применяет всё названное.
func TestDiskTypeCreateUpdateAdmin(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newBareTestPool(t))
	ctx := context.Background()

	created := dtInsert(t, dr, &domain.DiskType{
		ID: "block-nvme", Name: "block-nvme", Description: "nvme",
		ZoneIDs: []string{"region-1-a", "region-1-b"}, PerformanceTier: domain.TierFast,
	})
	require.Equal(t, "block-nvme", created.ID)
	require.Equal(t, []string{"region-1-a", "region-1-b"}, created.ZoneIDs)

	got, err := dr.Get(ctx, "block-nvme")
	require.NoError(t, err)
	require.Equal(t, domain.TierFast, got.PerformanceTier)

	_, err = dr.Insert(ctx, &domain.DiskType{ID: "block-nvme", Name: "dup", Lifecycle: domain.LifecycleActive})
	require.True(t, stderrors.Is(err, storageerr.ErrAlreadyExists), "duplicate slug → AlreadyExists, got %v", err)

	newName, newDesc := "renamed", "new-desc"
	newZones, newTier := []string{"region-2-a"}, domain.TierIOMax
	upd, err := dr.Update(ctx, "block-nvme", disktype.DiskTypeUpdate{
		Name: &newName, Description: &newDesc, ZoneIDs: &newZones, PerformanceTier: &newTier,
	})
	require.NoError(t, err)
	require.Equal(t, "renamed", upd.Name)
	require.Equal(t, "new-desc", upd.Description)
	require.Equal(t, []string{"region-2-a"}, upd.ZoneIDs)
	require.Equal(t, domain.TierIOMax, upd.PerformanceTier)

	_, err = dr.Update(ctx, "dtp-missing", disktype.DiskTypeUpdate{Name: &newName})
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "update missing → NotFound, got %v", err)
}

// TestDiskTypeUpdateAppliesOnlyNamedFields — правка применяет ТОЛЬКО названные поля:
// nil-указатель означает «не менять», а не «обнулить». Каждое утверждение — пара:
// названное поле изменилось (положительный контроль), всё остальное осталось
// побайтово тем же (иначе проба зеленела бы на правке, не делающей ничего).
func TestDiskTypeUpdateAppliesOnlyNamedFields(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newBareTestPool(t))
	ctx := context.Background()

	before := dtInsert(t, dr, &domain.DiskType{
		ID: "block-mask", Name: "исходное", Description: "исходное описание",
		ZoneIDs:         []string{"ru-central1-a", "ru-central1-b"},
		PerformanceTier: domain.TierBalanced,
		Lifecycle:       domain.LifecycleActive,
		Limits:          domain.SizeLimits{MinSizeBytes: 1 << 30, MaxSizeBytes: 16 << 40, SizeStepBytes: 1 << 30},
	})

	name := "переименованный"
	got, err := dr.Update(ctx, "block-mask", disktype.DiskTypeUpdate{Name: &name})
	require.NoError(t, err)
	want := *before
	want.Name = name
	require.Equal(t, &want, got, "названо имя — изменилось только имя")

	zones := []string{"ru-central1-c"}
	got, err = dr.Update(ctx, "block-mask", disktype.DiskTypeUpdate{ZoneIDs: &zones})
	require.NoError(t, err)
	want.ZoneIDs = zones
	require.Equal(t, &want, got, "названы зоны — имя предыдущей правки на месте")

	lifecycle := domain.LifecycleDeprecated
	got, err = dr.Update(ctx, "block-mask", disktype.DiskTypeUpdate{Lifecycle: &lifecycle})
	require.NoError(t, err)
	want.Lifecycle = lifecycle
	require.Equal(t, &want, got, "названо состояние обращения — границы размера не тронуты")

	minSize := int64(2) << 30
	got, err = dr.Update(ctx, "block-mask", disktype.DiskTypeUpdate{MinSizeBytes: &minSize})
	require.NoError(t, err)
	want.Limits.MinSizeBytes = minSize
	require.Equal(t, &want, got, "назван минимум — максимум и шаг не тронуты")

	// Пустая правка законна и ничего не меняет: «маска не назвала ни одного поля»
	// отличается от «маска назвала поля пустыми».
	got, err = dr.Update(ctx, "block-mask", disktype.DiskTypeUpdate{})
	require.NoError(t, err)
	require.Equal(t, &want, got, "правка без названных полей не меняет ничего")

	// Снять описание — тоже правка: пустая строка есть ЗНАЧЕНИЕ, а не отсутствие.
	empty := ""
	got, err = dr.Update(ctx, "block-mask", disktype.DiskTypeUpdate{Description: &empty})
	require.NoError(t, err)
	want.Description = ""
	require.Equal(t, &want, got, "названное пустым описание снимается, остальное на месте")

	_, err = dr.Update(ctx, "dtp-missing", disktype.DiskTypeUpdate{Name: &name})
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "правка отсутствующего → NotFound, got %v", err)
}

// TestDiskTypeFullPatchLeavesLifecycleUntouched — форма ПОЛНОГО PATCH (пустая маска:
// названы все изменяемые поля, включая границы размера) обязана оставить состояние
// обращения нетронутым: его меняет отдельный глагол SetLifecycle, и правка описания
// не вправе вернуть выведенный класс в обращение. Пара: зоны и границы она
// действительно замещает, иначе проба зеленела бы на правке, не делающей ничего.
func TestDiskTypeFullPatchLeavesLifecycleUntouched(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newBareTestPool(t))
	ctx := context.Background()

	before := dtInsert(t, dr, &domain.DiskType{
		ID: "block-portform", Name: "исходное", ZoneIDs: []string{"ru-central1-a"},
		PerformanceTier: domain.TierBalanced,
		Lifecycle:       domain.LifecycleDeprecated,
		Limits:          domain.SizeLimits{MinSizeBytes: 1 << 30, MaxSizeBytes: 16 << 40, SizeStepBytes: 1 << 30},
	})

	name, desc := "новое", "новое описание"
	zones, tier := []string{"ru-central1-b"}, domain.TierFast
	minSize, maxSize, step := int64(2)<<30, int64(32)<<40, int64(2)<<30
	got, err := dr.Update(ctx, "block-portform", disktype.DiskTypeUpdate{
		Name: &name, Description: &desc, ZoneIDs: &zones, PerformanceTier: &tier,
		MinSizeBytes: &minSize, MaxSizeBytes: &maxSize, SizeStepBytes: &step,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"ru-central1-b"}, got.ZoneIDs, "зоны замещены — форма не холостая")
	require.Equal(t, domain.TierFast, got.PerformanceTier)
	require.Equal(t, minSize, got.Limits.MinSizeBytes, "границы замещены — форма не холостая")
	require.Equal(t, before.Lifecycle, got.Lifecycle, "состояние обращения этим путём не приходит и не меняется")
}

// TestDiskTypeDeleteFKRestrict — Q4: Delete типа, на который ссылается том →
// FailedPrecondition "DiskType <id> is in use" (FK RESTRICT 23503); после удаления
// тома delete проходит; несуществующий → NotFound.
func TestDiskTypeDeleteFKRestrict(t *testing.T) {
	pool := newBareTestPool(t)
	dr := pg.NewDiskTypeRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	dtInsert(t, dr, &domain.DiskType{ID: "block-temp", Name: "temp"})
	offerDiskTypeInZone(t, pool, "block-temp", "region-1-a")
	vol, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-ondtp",
		ZoneID: "region-1-a", DiskTypeID: "block-temp", SizeBytes: 1 << 30,
	}, "")
	require.NoError(t, err)

	err = dr.Delete(ctx, "block-temp")
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "in-use → FailedPrecondition, got %v", err)
	require.Equal(t, "DiskType block-temp is in use", err.Error()[len("failed precondition: "):])
	_, gerr := dr.Get(ctx, "block-temp")
	require.NoError(t, gerr, "in-use disk type still present")

	require.NoError(t, vr.Delete(ctx, vol.ID))

	// Том ушёл, но класс всё ещё СВЯЗАН: на него ссылается ревизия привязки, и
	// ссылка эта — RESTRICT. Это верно по существу: удалив класс под живой
	// ревизией, мы оставили бы её висеть на том, чего нет. Поэтому снимается и
	// она — тем же порядком, каким заводилась.
	_, derr := pool.Exec(ctx, `DELETE FROM disk_type_bindings WHERE disk_type_id = $1`, "block-temp")
	require.NoError(t, derr)

	require.NoError(t, dr.Delete(ctx, "block-temp"), "delete allowed once nothing references it")
	_, gerr = dr.Get(ctx, "block-temp")
	require.True(t, stderrors.Is(gerr, storageerr.ErrNotFound))

	err = dr.Delete(ctx, "dtp-missing")
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "delete missing → NotFound, got %v", err)
}

// TestDiskTypeDeleteFKRestrictRace — FK RESTRICT под concurrency (data-integrity п.5):
// пока том ссылается на тип, N конкурентных Delete(тип) все получают
// FailedPrecondition (RESTRICT держит); тип не исчезает. Под -race.
func TestDiskTypeDeleteFKRestrictRace(t *testing.T) {
	pool := newBareTestPool(t)
	dr := pg.NewDiskTypeRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	dtInsert(t, dr, &domain.DiskType{ID: "block-race", Name: "race"})
	offerDiskTypeInZone(t, pool, "block-race", "region-1-a")
	_, _, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-race-ref",
		ZoneID: "region-1-a", DiskTypeID: "block-race", SizeBytes: 1 << 30,
	}, "")
	require.NoError(t, err)

	const n = 6
	var blocked, other atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			derr := dr.Delete(context.Background(), "block-race")
			switch {
			case stderrors.Is(derr, storageerr.ErrFailedPrecondition):
				blocked.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, n, blocked.Load(), "every delete blocked by live FK reference")
	require.EqualValues(t, 0, other.Load())
	_, gerr := dr.Get(ctx, "block-race")
	require.NoError(t, gerr, "disk type intact while referenced")
}

// TestDiskTypeListCursor — cursor (created_at,id) ASC пагинация каталога. Каталог
// свежей базы пуст (посев снят 0016), поэтому проба сеет свои классы сама.
func TestDiskTypeListCursor(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newBareTestPool(t))
	ctx := context.Background()

	const total = 5
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("block-page-%d", i)
		dtInsert(t, dr, &domain.DiskType{ID: id, Name: id})
	}

	page1, next, err := dr.List(ctx, disktype.Pagination{PageSize: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, next)

	seen := map[string]struct{}{}
	for _, d := range page1 {
		seen[d.ID] = struct{}{}
	}
	token := next
	for token != "" {
		var pg2 []*domain.DiskType
		pg2, token, err = dr.List(ctx, disktype.Pagination{PageSize: 2, PageToken: token})
		require.NoError(t, err)
		for _, d := range pg2 {
			_, dup := seen[d.ID]
			require.False(t, dup, "cursor yields no duplicates: %s", d.ID)
			seen[d.ID] = struct{}{}
		}
	}
	require.Len(t, seen, total, "all types paged through")
	require.True(t, strings.HasPrefix(page1[0].ID, "block-page-"))
}
