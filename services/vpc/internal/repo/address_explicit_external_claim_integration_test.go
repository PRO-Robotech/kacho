// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Явно заданный внешний адрес обязан ИЗЪЯТЬ себя из книги учёта пула так же,
// как это делает автоматическая выдача. Пока изъятия нет, реестр занятости
// расходится с реальностью на пути ЗАНЯТИЯ: адрес занят ресурсом, но числится
// свободным, и следующая автоматическая выдача обязана его предложить. Пул при
// этом общий на зону — то есть на всех тенантов.
//
// Эти пробы держат обе половины контракта ограниченного пула: изъятие при
// занятии (здесь) и возврат при высвобождении (address_freelist_*).

// insertPoolWithCidrs создаёт пул ТАК, как его создаёт продуктовый Create:
// массивы в address_pools + нормализованные строки в address_pool_cidrs (именно
// они несут EXCLUDE и служат картой «какой пул владеет этим адресом»).
func insertPoolWithCidrs(t testing.TB, ctx context.Context, pgPool *pgxpool.Pool, v4, v6 []string) string {
	t.Helper()
	poolID := ids.NewID("apl")
	if v4 == nil {
		v4 = []string{}
	}
	if v6 == nil {
		v6 = []string{}
	}
	_, err := pgPool.Exec(ctx, `
		INSERT INTO address_pools (id, name, v4_cidr_blocks, v6_cidr_blocks, kind)
		VALUES ($1, $2, $3::text[], $4::text[], $5)
	`, poolID, poolID, v4, v6, int16(domain.AddressPoolKindExternalPublic))
	require.NoError(t, err)
	for _, b := range append(append([]string{}, v4...), v6...) {
		_, err = pgPool.Exec(ctx, `
			INSERT INTO address_pool_cidrs (pool_id, kind, block) VALUES ($1, $2, $3::cidr)
		`, poolID, int16(domain.AddressPoolKindExternalPublic), b)
		require.NoError(t, err)
	}
	return poolID
}

// insertExplicitExternalV4Address — продуктовый путь Address.Create с ЯВНЫМ
// внешним адресом: use-case пишет строку через Addresses().Insert и, в отличие
// от автоматической выдачи, аллокатор не зовёт.
func insertExplicitExternalV4Address(t testing.TB, ctx context.Context, r kacho.Repository, projectID, ip string) (string, *kacho.AddressRecord) {
	t.Helper()
	addrID := ids.NewID(ids.PrefixAddress)
	var rec *kacho.AddressRecord
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	rec, err = w.Addresses().Insert(ctx, &domain.Address{
		ID: addrID, Name: domain.RcNameVPC(addrID),
		ProjectID:    projectID,
		Type:         domain.AddressTypeExternal,
		IpVersion:    domain.IpVersionIPv4,
		Reserved:     true,
		ExternalIpv4: &domain.ExternalIpv4Spec{Address: ip},
	})
	if err != nil {
		w.Abort()
		require.NoError(t, err)
	}
	require.NoError(t, w.Commit())
	return addrID, rec
}

func freeIPPresent(t testing.TB, ctx context.Context, pgPool *pgxpool.Pool, poolID, ip string) bool {
	t.Helper()
	var n int
	require.NoError(t, pgPool.QueryRow(ctx,
		`SELECT count(*) FROM address_pool_free_ips WHERE pool_id = $1 AND ip = $2::inet`, poolID, ip).Scan(&n))
	return n > 0
}

// Занятие явным адресом обязано изъять его из свободного списка: следующая
// автоматическая выдача из того же пула получает ДРУГОЙ адрес и НЕ падает.
// Без изъятия аллокатор берёт строго наименьший свободный — то есть ровно
// занятый — и упирается в глобальную уникальность внешнего адреса; отказ
// восстанавливает свободную строку, поэтому следующий запрос падает так же.
// Выдача внешних адресов в зоне прекращается для ВСЕХ тенантов и сама не
// восстанавливается.
func TestExplicitExternalIPv4_ClaimedFromPool_AutoAllocateSkipsIt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	poolID := insertPoolWithCidrs(t, ctx, pgPool, []string{"198.51.100.0/28"}, nil)
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.AddressPools().PopulateFreelistForPool(ctx, poolID)
	}))
	const explicitIP = "198.51.100.1" // наименьший свободный — тот, что возьмёт аллокатор

	_, rec := insertExplicitExternalV4Address(t, ctx, r, "b1gtestproject00000", explicitIP)

	// Автоматическая выдача другому адресу: обязана пройти и дать ДРУГОЙ адрес.
	autoID := insertTestAddressFreelist(t, ctx, pgPool)
	var autoIP string
	err = freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		ip, e := w.Addresses().AllocateIPFromFreelist(ctx, poolID, autoID)
		autoIP = ip
		return e
	})
	require.NoError(t, err, "автоматическая выдача не должна упираться в явно занятый адрес")
	require.NotEqual(t, explicitIP, autoIP, "аллокатор не имеет права выдать уже занятый адрес")

	require.False(t, freeIPPresent(t, ctx, pgPool, poolID, explicitIP),
		"занятый явным образом адрес обязан быть изъят из свободного списка пула")
	require.Equal(t, poolID, rec.ExternalIpv4.AddressPoolID,
		"явный адрес внутри диапазона пула обязан быть привязан к пулу-владельцу — иначе его не вернуть в пул при удалении")
}

// Явный адрес, который в пуле уже НЕ свободен (выдан автоматически), обязан
// быть отвергнут предусловием, а не приниматься второй раз.
func TestExplicitExternalIPv4_AlreadyTakenInPool_Rejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	poolID := insertPoolWithCidrs(t, ctx, pgPool, []string{"198.51.100.0/28"}, nil)
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.AddressPools().PopulateFreelistForPool(ctx, poolID)
	}))
	autoID := insertTestAddressFreelist(t, ctx, pgPool)
	var takenIP string
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		ip, e := w.Addresses().AllocateIPFromFreelist(ctx, poolID, autoID)
		takenIP = ip
		return e
	}))
	require.NotEmpty(t, takenIP)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Addresses().Insert(ctx, &domain.Address{
		ID: ids.NewID(ids.PrefixAddress), Name: fixtureName(),
		ProjectID:    "b1gtestproject00001",
		Type:         domain.AddressTypeExternal,
		IpVersion:    domain.IpVersionIPv4,
		ExternalIpv4: &domain.ExternalIpv4Spec{Address: takenIP},
	})
	w.Abort()
	require.Error(t, err, "занятый в пуле адрес не может быть занят второй раз явным указанием")
	require.ErrorIs(t, err, repo.ErrFailedPrecondition)
}

// Конкуренция: две транзакции занимают ОДИН явный адрес. Ровно одна проходит,
// вторая получает ожидаемый признак, и из пула уходит ровно один адрес.
func TestExplicitExternalIPv4_ConcurrentClaims_ExactlyOneWins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	poolID := insertPoolWithCidrs(t, ctx, pgPool, []string{"198.51.100.0/28"}, nil)
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.AddressPools().PopulateFreelistForPool(ctx, poolID)
	}))
	before := poolFreeCount(t, ctx, pgPool, poolID)
	const explicitIP = "198.51.100.9"

	const writers = 6
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		ok   int
		errs []error
	)
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			w, werr := r.Writer(ctx)
			defer w.Abort()
			if werr != nil {
				mu.Lock()
				errs = append(errs, werr)
				mu.Unlock()
				return
			}
			_, ierr := w.Addresses().Insert(ctx, &domain.Address{
				ID: ids.NewID(ids.PrefixAddress), Name: fixtureName(),
				ProjectID:    fmt.Sprintf("b1gtestproject%05d", i),
				Type:         domain.AddressTypeExternal,
				IpVersion:    domain.IpVersionIPv4,
				ExternalIpv4: &domain.ExternalIpv4Spec{Address: explicitIP},
			})
			if ierr == nil {
				ierr = w.Commit()
			} else {
				w.Abort()
			}
			mu.Lock()
			if ierr == nil {
				ok++
			} else {
				errs = append(errs, ierr)
			}
			mu.Unlock()
		}(i)
	}
	close(start)
	wg.Wait()

	require.Equal(t, 1, ok, "ровно одна транзакция вправе занять адрес: %v", errs)
	for _, e := range errs {
		require.True(t,
			isAnyErr(e, repo.ErrFailedPrecondition, repo.ErrAlreadyExists),
			"проигравший обязан получить ожидаемый признак, получено: %v", e)
	}
	require.Equal(t, before-1, poolFreeCount(t, ctx, pgPool, poolID),
		"из пула обязан уйти ровно один адрес")
}

// Пул не деградирует: N занятий (одно явное, остальные автоматические) → N
// высвобождений → N занятий снова проходят.
func TestExplicitExternalIPv4_PoolNotDegraded_AllocReleaseAlloc(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	poolID := insertPoolWithCidrs(t, ctx, pgPool, []string{"198.51.100.0/28"}, nil)
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.AddressPools().PopulateFreelistForPool(ctx, poolID)
	}))
	total := poolFreeCount(t, ctx, pgPool, poolID)
	require.Equal(t, 14, total, "/28 даёт 14 пригодных адресов")

	round := func(explicitIP string) []string {
		held := make([]string, 0, total)
		addrIDs := make([]string, 0, total)

		id, _ := insertExplicitExternalV4Address(t, ctx, r, "b1gtestproject00000", explicitIP)
		held = append(held, explicitIP)
		addrIDs = append(addrIDs, id)

		for len(held) < total {
			autoID := insertTestAddressFreelist(t, ctx, pgPool)
			var ip string
			require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
				got, e := w.Addresses().AllocateIPFromFreelist(ctx, poolID, autoID)
				ip = got
				return e
			}), "занятие %d из %d обязано пройти", len(held)+1, total)
			require.NotContains(t, held, ip, "один адрес не может быть выдан дважды")
			held = append(held, ip)
			addrIDs = append(addrIDs, autoID)
		}
		require.Zero(t, poolFreeCount(t, ctx, pgPool, poolID))

		// Высвобождение всех: возврат в свободный список + удаление строки.
		for i, ip := range held {
			addrID := addrIDs[i]
			require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
				if e := w.Addresses().ReturnIPToFreelist(ctx, poolID, ip); e != nil {
					return e
				}
				_, e := w.Addresses().DeleteGuarded(ctx, addrID)
				return e
			}))
		}
		require.Equal(t, total, poolFreeCount(t, ctx, pgPool, poolID), "пул обязан восстановиться целиком")
		return held
	}

	round("198.51.100.7")
	round("198.51.100.7") // второй проход: пул не деградировал
}

// Один маршрутизируемый внешний IPv6 не может принадлежать двум ресурсам —
// как и внешний IPv4. Уникальность обязана жить в базе, а не в надежде на то,
// что явный адрес никто не задаст.
func TestExplicitExternalIPv6_GloballyUnique(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	const ip6 = "2001:db8:aaaa::1234"
	insert := func(project string) error {
		w, werr := r.Writer(ctx)
		require.NoError(t, werr)
		_, ierr := w.Addresses().Insert(ctx, &domain.Address{
			ID: ids.NewID(ids.PrefixAddress), Name: fixtureName(),
			ProjectID:    project,
			Type:         domain.AddressTypeExternal,
			IpVersion:    domain.IpVersionIPv6,
			ExternalIpv6: &domain.ExternalIpv6Spec{Address: ip6},
		})
		if ierr != nil {
			w.Abort()
			return ierr
		}
		return w.Commit()
	}
	require.NoError(t, insert("b1gtestproject00000"))
	err = insert("b1gtestproject00001")
	require.Error(t, err, "тот же внешний IPv6 во втором проекте обязан быть отвергнут")
	require.ErrorIs(t, err, repo.ErrAlreadyExists)
}

// Явный внешний IPv6 внутри диапазона пула обязан быть записан в книгу учёта
// пула, чтобы счётчик выдачи никогда не предложил его второй раз.
func TestExplicitExternalIPv6_ClaimedInLedger_CursorSkipsIt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	poolID := insertPoolWithCidrs(t, ctx, pgPool, nil, []string{"2001:db8:bbbb::/64"})
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Addresses().InitIPv6PoolCursor(ctx, poolID)
	}))

	// Счётчик выдаёт префикс+1, префикс+2, … — занимаем явным образом третий.
	const explicitIP = "2001:db8:bbbb::3"
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	rec, err := w.Addresses().Insert(ctx, &domain.Address{
		ID: ids.NewID(ids.PrefixAddress), Name: fixtureName(),
		ProjectID:    "b1gtestproject00000",
		Type:         domain.AddressTypeExternal,
		IpVersion:    domain.IpVersionIPv6,
		ExternalIpv6: &domain.ExternalIpv6Spec{Address: explicitIP},
	})
	if err != nil {
		w.Abort()
		require.NoError(t, err)
	}
	require.NoError(t, w.Commit())
	require.Equal(t, poolID, rec.ExternalIpv6.AddressPoolID,
		"явный адрес внутри диапазона пула обязан быть привязан к пулу-владельцу")

	var ledger int
	require.NoError(t, pgPool.QueryRow(ctx,
		`SELECT count(*) FROM ipv6_allocated_ips WHERE pool_id = $1 AND ip = $2::inet`,
		poolID, explicitIP).Scan(&ledger))
	require.Equal(t, 1, ledger, "занятие обязано попасть в книгу учёта пула")

	// Четыре автоматические выдачи: ни одна не имеет права вернуть занятый
	// адрес и ни одна не имеет права упасть.
	seen := map[string]bool{explicitIP: true}
	for i := 0; i < 4; i++ {
		autoID := insertTestAddressFreelist(t, ctx, pgPool)
		var ip string
		require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
			got, e := w.Addresses().AllocateExternalIPv6(ctx, poolID, autoID, "")
			ip = got
			return e
		}), "автоматическая выдача %d обязана пройти", i+1)
		require.False(t, seen[ip], "адрес %s выдан дважды", ip)
		seen[ip] = true
		_, perr := netip.ParseAddr(ip)
		require.NoError(t, perr)
	}
}

// Ключ книги учёта свободных адресов — только host-форма. Найдено при фиксе
// занятия явного адреса: материализация диапазона писала адрес С МАСКОЙ
// диапазона, а возврат при удалении — без маски; тип inet считает их разными
// значениями, поэтому (а) точечное занятие по значению не находило строку и
// (б) первичный ключ не мешал одному адресу лежать в свободном списке дважды.
func TestFreelistKey_HostFormOnly_NoTwinKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	poolID := insertPoolWithCidrs(t, ctx, pgPool, []string{"198.51.100.0/28"}, nil)
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.AddressPools().PopulateFreelistForPool(ctx, poolID)
	}))

	var masked int
	require.NoError(t, pgPool.QueryRow(ctx,
		`SELECT count(*) FROM address_pool_free_ips WHERE pool_id = $1 AND ip <> host(ip)::inet`,
		poolID).Scan(&masked))
	require.Zero(t, masked, "материализация диапазона обязана писать адрес без маски")

	// Занять → вернуть → материализовать диапазон заново: адрес не имеет права
	// удвоиться (иначе вторая выдача упрётся в глобальную уникальность).
	autoID := insertTestAddressFreelist(t, ctx, pgPool)
	var ip string
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		got, e := w.Addresses().AllocateIPFromFreelist(ctx, poolID, autoID)
		ip = got
		return e
	}))
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Addresses().ReturnIPToFreelist(ctx, poolID, ip)
	}))
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.AddressPools().PopulateFreelistForPool(ctx, poolID)
	}))
	require.Equal(t, 14, poolFreeCount(t, ctx, pgPool, poolID),
		"повторная материализация не имеет права удвоить уже вернувшийся адрес")

	// Форма закреплена базой, а не соглашением между двумя запросами.
	_, err = pgPool.Exec(ctx,
		`INSERT INTO address_pool_free_ips (pool_id, ip) VALUES ($1, '198.51.100.1/28')`, poolID)
	require.Error(t, err, "адрес с маской диапазона не имеет права попасть в книгу учёта")
}

func isAnyErr(err error, targets ...error) bool {
	for _, tgt := range targets {
		if errors.Is(err, tgt) {
			return true
		}
	}
	return false
}

// Пул с ДВУМЯ v6-диапазонами: явный адрес во втором диапазоне не имеет права
// быть отвергнут только потому, что его номер внутри своего диапазона совпал с
// номером уже выданного адреса из первого. Номер в книге учёта обязан считаться
// от ОДНОГО якоря на весь пул — тогда он взаимно однозначен с адресом, и
// «занято» означает занятость самого адреса, а не совпадение номеров.
func TestExplicitExternalIPv6_SecondBlock_NotRejectedByOffsetCollision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	poolID := insertPoolWithCidrs(t, ctx, pgPool, nil,
		[]string{"2001:db8:d0::/64", "2001:db8:d1::/64"})
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Addresses().InitIPv6PoolCursor(ctx, poolID)
	}))

	// Три автоматические выдачи из ПЕРВОГО диапазона занимают номера 1, 2, 3.
	for i := 0; i < 3; i++ {
		autoID := insertTestAddressFreelist(t, ctx, pgPool)
		require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
			_, e := w.Addresses().AllocateExternalIPv6(ctx, poolID, autoID, "")
			return e
		}))
	}

	// Явный адрес во ВТОРОМ диапазоне, чей номер внутри своего диапазона — 2.
	const explicitIP = "2001:db8:d1::2"
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	rec, err := w.Addresses().Insert(ctx, &domain.Address{
		ID: ids.NewID(ids.PrefixAddress), Name: fixtureName(),
		ProjectID:    "b1gtestproject00000",
		Type:         domain.AddressTypeExternal,
		IpVersion:    domain.IpVersionIPv6,
		ExternalIpv6: &domain.ExternalIpv6Spec{Address: explicitIP},
	})
	if err != nil {
		w.Abort()
		require.NoError(t, err, "свободный адрес второго диапазона обязан приниматься")
	}
	require.NoError(t, w.Commit())
	require.Equal(t, poolID, rec.ExternalIpv6.AddressPoolID)

	var ledger int
	require.NoError(t, pgPool.QueryRow(ctx,
		`SELECT count(*) FROM ipv6_allocated_ips WHERE pool_id = $1 AND ip = $2::inet`,
		poolID, explicitIP).Scan(&ledger))
	require.Equal(t, 1, ledger, "занятие обязано попасть в книгу учёта пула")

	// Повторное занятие того же адреса — отказ (книга учёта работает).
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.Addresses().Insert(ctx, &domain.Address{
		ID: ids.NewID(ids.PrefixAddress), Name: fixtureName(),
		ProjectID:    "b1gtestproject00001",
		Type:         domain.AddressTypeExternal,
		IpVersion:    domain.IpVersionIPv6,
		ExternalIpv6: &domain.ExternalIpv6Spec{Address: explicitIP},
	})
	w2.Abort()
	require.Error(t, err, "второе занятие того же адреса обязано быть отвергнуто")
}

// Освобождение явного адреса ВТОРОГО диапазона не имеет права заклинить выдачу.
// Номер такого адреса лежит вне счётного пространства первого диапазона (того,
// которым нумерует счётчик), поэтому вернуть его «в свободные номера» нельзя:
// следующая выдача вывела бы из него адрес вне своего префикса, признала бы пул
// исчерпанным, откатила транзакцию — и номер вернулся бы на место. Пул перестал
// бы выдавать адреса навсегда, для всех тенантов, и сам бы не восстановился.
func TestExplicitExternalIPv6_ReleaseOfSecondBlock_DoesNotWedgeAllocation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	poolID := insertPoolWithCidrs(t, ctx, pgPool, nil,
		[]string{"2001:db8:e0::/64", "2001:db8:e1::/64"})
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Addresses().InitIPv6PoolCursor(ctx, poolID)
	}))

	// Явный адрес второго диапазона: занимается, затем освобождается.
	const explicitIP = "2001:db8:e1::7"
	addrID := ids.NewID(ids.PrefixAddress)
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Addresses().Insert(ctx, &domain.Address{
		ID: addrID, Name: domain.RcNameVPC(addrID),
		ProjectID:    "b1gtestproject00000",
		Type:         domain.AddressTypeExternal,
		IpVersion:    domain.IpVersionIPv6,
		ExternalIpv6: &domain.ExternalIpv6Spec{Address: explicitIP},
	})
	if err != nil {
		w.Abort()
		require.NoError(t, err)
	}
	require.NoError(t, w.Commit())
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Addresses().FreeExternalIPv6(ctx, addrID)
	}))

	// Автоматическая выдача обязана продолжать работать — дважды подряд.
	for i := 0; i < 2; i++ {
		autoID := insertTestAddressFreelist(t, ctx, pgPool)
		var ip string
		require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
			got, e := w.Addresses().AllocateExternalIPv6(ctx, poolID, autoID, "")
			ip = got
			return e
		}), "выдача %d после освобождения адреса второго диапазона обязана пройти", i+1)
		require.True(t, netip.MustParsePrefix("2001:db8:e0::/64").Contains(netip.MustParseAddr(ip)),
			"счётчик обязан выдавать из своего диапазона, выдал %s", ip)
	}
}
