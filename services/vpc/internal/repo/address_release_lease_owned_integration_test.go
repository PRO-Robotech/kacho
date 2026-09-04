// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

// Ветку снятия аренды выбирает КОЛОНКА владения — и проба стоит у того, кто её
// читает.
//
// ПРЕДМЕТ (#550). Решение «удалить адрес модуля» либо «оставить адрес
// арендатора» принимает vpc: `ReleaseLease` возвращает `ar.owned` одним
// стейтментом с CAS, а `ReleaseOwnedAddressUseCase` доводит до конца только
// ветку RELEASED. До #439 то же решение выводил ПОТРЕБИТЕЛЬ (nlb ветвился по
// своему признаку и звал разные глаголы), и держали его пробы потребителя.
// Решение переехало к владельцу, а проба за ним не переехала: пробы nlb
// проверяют, как он обходится с УЖЕ НАЗВАННЫМ исходом, и работают против
// дублёра — а дублёр модель, а не владелец.
//
// ПОЧЕМУ ЗДЕСЬ, А НЕ РЯДОМ С USE-CASE'ОМ. Ветку выбирает `RETURNING ar.owned`
// из настоящего оператора; проба против дублёра закрепила бы ответ дублёра.
// Поэтому use-case собран поверх НАСТОЯЩЕГО репозитория, а харнесс Postgres
// живёт в этом пакете.
//
// ЧТО УТВЕРЖДАЕТСЯ — наблюдаемое, а не путь внутри:
//
//	owned=false → исход DETACHED · строка адреса ЖИВА · аренда снята ·
//	              адрес в пул НЕ вернулся;
//	owned=true  → исход RELEASED · строки адреса НЕТ · адрес в пул вернулся.
//
// Обе стороны нужны вместе: одна без другой зеленеет на реализации, которая
// всегда выбирает её ветку.
//
// КРАСНОГО ДО ФИКСА НЕТ — продукт верен, чинить нечего. Способность пробы упасть
// доказана ИНЪЕКЦИЕЙ в обе стороны: подмена колонки владения на константу в
// `ReleaseLease` (`RETURNING ar.owned` → `RETURNING true`) роняет первую пробу с
// координатой (адрес арендатора удалён, исход RELEASED вместо DETACHED), а
// вторая — законный близнец — остаётся зелёной. Обратная подмена (`RETURNING
// false`) роняет вторую и оставляет первую.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	addrapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/address"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Проект фикстуры назван литералом намеренно: перечень проектов строкой учёта
// собирается ЧТЕНИЕМ исходников проб пакета (quota_fixture_integration_test.go),
// и имя, собранное конкатенацией, в него не попадёт.
const leaseFixtureProject = "prj-lease"

// leaseFixture — общая посадка обеих проб: пул БЕЗ материализованного свободного
// списка плюс внешний адрес, привязанный к этому пулу.
//
// Свободный список пуст НАМЕРЕННО. Тогда «адрес вернулся в пул» и «не вернулся»
// различаются одним числом — счётчиком строк пула, — а не разбором того, лежал
// ли этот IP там раньше.
type leaseFixture struct {
	repo    kacho.Repository
	pgPool  *pgxpool.Pool
	poolID  string
	address *domain.Address
	ip      string
}

func newLeaseFixture(t *testing.T, ctx context.Context, owned bool) *leaseFixture {
	t.Helper()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	t.Cleanup(func() { r.Close() })

	// Имя пула — короткий литерал, а не `t.Name()`: имя ресурса ограничено 63
	// символами (`address_pools_name_chk`), а имена этих проб длиннее. Своя база
	// у каждой пробы, поэтому одинаковое имя им не мешает.
	poolID := ids.NewID("apl")
	_, err = pgPool.Exec(ctx, `
		INSERT INTO address_pools (id, name, v4_cidr_blocks, kind)
		VALUES ($1, 'pool-lease', ARRAY['198.51.100.0/28']::text[], 1)`, poolID)
	require.NoError(t, err)
	const ip = "198.51.100.5"

	addr := &domain.Address{
		ID:        ids.NewID(ids.PrefixAddress),
		ProjectID: leaseFixtureProject,
		Name:      domain.RcNameVPC("addr-lease"),
		Type:      domain.AddressTypeExternal,
		IpVersion: domain.IpVersionIPv4,
		ExternalIpv4: &domain.ExternalIpv4Spec{
			Address:       ip,
			ZoneID:        "zone-a",
			AddressPoolID: poolID,
		},
	}
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Addresses().Insert(ctx, addr)
		return e
	}))
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Addresses().SetReference(ctx, &domain.AddressReference{
			AddressID:    addr.ID,
			ReferrerType: "network_load_balancer",
			ReferrerID:   "nlb00000000lease1",
			ReferrerName: "lb-lease",
			Owned:        owned,
		})
		return e
	}))

	f := &leaseFixture{repo: r, pgPool: pgPool, poolID: poolID, address: addr, ip: ip}
	// Предпосылка пробы утверждается, а не предполагается: без неё «ноль строк в
	// пуле» после вызова означало бы и «не вернули», и «фикстура не сложилась».
	require.Equal(t, owned, f.reference(t, ctx).Owned, "фикстура обязана лечь на объявленную колонку владения")
	require.Equal(t, 0, f.poolFreeIPs(t, ctx), "свободный список пула обязан быть пуст ДО вызова")
	return f
}

func (f *leaseFixture) reference(t *testing.T, ctx context.Context) *domain.AddressReference {
	t.Helper()
	rd, err := f.repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	ref, err := rd.Addresses().GetReference(ctx, f.address.ID)
	require.NoError(t, err)
	return ref
}

func (f *leaseFixture) poolFreeIPs(t *testing.T, ctx context.Context) int {
	t.Helper()
	var n int
	require.NoError(t, f.pgPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM address_pool_free_ips WHERE pool_id = $1`, f.poolID).Scan(&n))
	return n
}

// release зовёт глагол ровно так, как его зовёт потребитель: предъявлением пары
// и проекта, без признака владения.
func (f *leaseFixture) release(ctx context.Context) (kacho.LeaseOutcome, error) {
	return addrapp.NewReleaseOwnedAddressUseCase(f.repo).Execute(ctx, addrapp.ReleaseLeaseInput{
		ProjectID:    leaseFixtureProject,
		AddressID:    f.address.ID,
		ReferrerType: "network_load_balancer",
		ReferrerID:   "nlb00000000lease1",
	})
}

// Адрес АРЕНДАТОРА (owned=false): ссылка снята, адрес остался, в пул не вернулся.
//
// Это и есть свойство, ради которого задача заведена: возврат чужого адреса в
// пул выдал бы его второму арендатору, у которого он работал бы у первого.
func TestIntegration_ReleaseOwnedAddress_TenantAddressIsDetachedAndSurvives(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newLeaseFixture(t, ctx, false)

	outcome, err := f.release(ctx)
	require.NoError(t, err)
	assert.Equal(t, kacho.LeaseDetached, outcome,
		"на адресе арендатора владелец обязан НАЗВАТЬ отвязывание, а не освобождение")

	rd, err := f.repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	got, err := rd.Addresses().Get(ctx, f.address.ID)
	require.NoError(t, err, "адрес арендатора обязан пережить снятие аренды")
	assert.False(t, got.Used, "снятие аренды обязано освободить адрес: used=false")

	_, refErr := rd.Addresses().GetReference(ctx, f.address.ID)
	assert.ErrorIs(t, refErr, repo.ErrNotFound, "аренда обязана быть снята")

	assert.Equal(t, 0, f.poolFreeIPs(t, ctx),
		"адрес арендатора в свободный список пула попасть не может: там его выдали бы второму")
}

// Адрес МОДУЛЯ (owned=true): законный близнец — та же форма вызова, другая
// колонка владения. Без него первая проба зеленела бы на реализации, которая
// не удаляет никогда.
func TestIntegration_ReleaseOwnedAddress_ModuleAddressIsReleasedAndReturnedToPool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newLeaseFixture(t, ctx, true)

	outcome, err := f.release(ctx)
	require.NoError(t, err)
	assert.Equal(t, kacho.LeaseReleased, outcome,
		"на адресе модуля владелец обязан НАЗВАТЬ освобождение")

	rd, err := f.repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	_, getErr := rd.Addresses().Get(ctx, f.address.ID)
	assert.ErrorIs(t, getErr, repo.ErrNotFound, "адрес модуля обязан быть удалён вместе с арендой")

	assert.Equal(t, 1, f.poolFreeIPs(t, ctx),
		"аренда модуля обязана вернуться в пул, иначе пул истощается на каждом сносе потребителя")
	var returned string
	require.NoError(t, f.pgPool.QueryRow(ctx,
		`SELECT host(ip)::text FROM address_pool_free_ips WHERE pool_id = $1`, f.poolID).Scan(&returned))
	assert.Equal(t, f.ip, returned, "в пул обязан вернуться ТОТ адрес, который освободили")
}
