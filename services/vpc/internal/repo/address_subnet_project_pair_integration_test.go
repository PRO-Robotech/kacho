// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// «Адрес и подсеть, на которую он ссылается, принадлежат ОДНОМУ проекту» — эти
// пробы утверждают, что инвариант держит БАЗА, а не порядок операторов в
// use-case'е.
//
// Что было до миграции 0033 (замерено исполнением этих же проб на дереве без
// неё): равенство проверялось единственным местом —
// `apps/kacho/api/address/create.go` (`assertSubnetOwned`), запросом
// `SELECT … FROM subnets WHERE id=$1` через ЧИТАЮЩЕЕ соединение, и сравнением
// `sub.ProjectID != projectID` в Go. Оба вызова стоят ДО открытия writer-TX
// (`create.go` — проверка, затем `u.repo.Writer(ctx)` строкой ниже по потоку),
// то есть чтение и запись лежат в РАЗНЫХ транзакциях, а между ними — граница
// Operation, окно произвольной длины. На уровне базы равенства не было выражено
// ничем: единственный внешний ключ шёл на `subnets(id)` и о проекте не знал,
// поэтому ЛЮБОЙ писатель, минующий этот use-case, вставлял адрес со ссылкой на
// подсеть чужого проекта, и база принимала строку.
//
// Предикат, которым это проверяется здесь: вставка идёт СРАЗУ через порт
// репозитория (`w.Addresses().Insert`) — тем самым путём, которым пользуется
// любой второй писатель, — минуя use-case целиком. До 0033 проходили все
// вставки; после — ровно одна, чей проект совпадает с проектом подсети.
//
// Отказ обязан быть тем же, что и на синхронном пути: `NOT_FOUND
// "Subnet <id> not found"`, дословно тот же текст, что отдаёт `assertSubnetOwned`
// на НЕСУЩЕСТВУЮЩУЮ подсеть. Различимый текст здесь был бы existence-oracle:
// по нему отличали бы «подсеть есть, но чужая» от «подсети нет» — ровно то, что
// скрытие и должно закрыть (`security.md` §hardening, п.6).

// seedNetworkAndSubnet кладёт сеть и подсеть одного проекта и возвращает id
// подсети. Отдельная функция, потому что ею пользуются обе пробы ниже.
func seedNetworkAndSubnet(t *testing.T, ctx context.Context, r kacho.Repository, projectID string) string {
	t.Helper()
	netID := ids.NewID(ids.PrefixNetwork)
	subID := ids.NewID(ids.PrefixSubnet)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		if _, e := w.Networks().Insert(ctx, &domain.Network{
			ID: netID, ProjectID: projectID, Name: domain.RcNameVPC("n-pair"),
		}); e != nil {
			return e
		}
		_, e := w.Subnets().Insert(ctx, &domain.Subnet{
			ID: subID, ProjectID: projectID, Name: domain.RcNameVPC("s-pair"),
			NetworkID: netID, ZoneID: "zone-pair", PlacementType: domain.PlacementZonal,
			V4CidrBlocks: []string{"10.77.0.0/24"},
		})
		return e
	}))
	return subID
}

// internalAddress — адрес с internal-ссылкой на подсеть и БЕЗ выбранного IP.
// Пустой `address` выбран намеренно: частичные уникальные индексы
// `addresses_internal_subnet_ip_uniq` / `…_ipv6_uniq` покрывают строки с
// НЕПУСТЫМ адресом, поэтому пустой оставляет под наблюдением ровно один предмет —
// внешний ключ на пару (проект, подсеть), а не уникальность адреса в подсети.
func internalAddress(projectID, subnetID string, v6 bool) *domain.Address {
	a := &domain.Address{
		ID: ids.NewID(ids.PrefixAddress), Name: fixtureName(),
		ProjectID: projectID,
		Type:      domain.AddressTypeInternal,
		Reserved:  true,
	}
	if v6 {
		a.IpVersion = domain.IpVersionIPv6
		a.InternalIpv6 = &domain.InternalIpv6Spec{SubnetID: subnetID}
		return a
	}
	a.IpVersion = domain.IpVersionIPv4
	a.InternalIpv4 = &domain.InternalIpv4Spec{SubnetID: subnetID}
	return a
}

// TestIntegration_Address_SubnetProjectPair_ConcurrentInsert_OnlyOwnerProjectCommits —
// восемь параллельных транзакций вставляют адрес со ссылкой на ОДНУ подсеть;
// проект совпадает ровно у одной. Ожидание: одна коммитится, семь получают
// `NOT_FOUND "Subnet <id> not found"`, в таблице ровно одна строка.
//
// Положительный контроль встроен в саму пробу и не отделим от неё: если бы
// внешний ключ отвергал ВСЁ (например, из-за неверного порядка колонок в паре),
// «ровно один успех» не выполнилось бы так же, как при отсутствии ключа. Поэтому
// утверждается не только число отказов, но и ЧЕЙ вход прошёл.
func TestIntegration_Address_SubnetProjectPair_ConcurrentInsert_OnlyOwnerProjectCommits(t *testing.T) {
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

	const ownerProject = "prj-pair-owner"
	subID := seedNetworkAndSubnet(t, ctx, r, ownerProject)

	const writers = 8
	type outcome struct {
		project string
		id      string
		err     error
	}
	results := make([]outcome, writers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			project := ownerProject
			if i != 0 {
				project = fmt.Sprintf("prj-pair-other-%d", i)
			}
			addr := internalAddress(project, subID, i%2 == 1)
			<-start
			w, werr := r.Writer(ctx)
			if werr != nil {
				results[i] = outcome{project: project, err: werr}
				return
			}
			defer w.Abort()
			if _, ierr := w.Addresses().Insert(ctx, addr); ierr != nil {
				results[i] = outcome{project: project, err: ierr}
				return
			}
			results[i] = outcome{project: project, id: addr.ID, err: w.Commit()}
		}(i)
	}
	close(start)
	wg.Wait()

	var committed []outcome
	for _, got := range results {
		if got.err == nil {
			committed = append(committed, got)
			continue
		}
		require.ErrorIs(t, got.err, helpers.ErrNotFound,
			"вставка из проекта %s обязана быть отвергнута полосой отсутствия, а не иной", got.project)
		assert.Contains(t, got.err.Error(), fmt.Sprintf("Subnet %s not found", subID),
			"текст отказа — часть контракта и обязан совпадать с ответом на НЕСУЩЕСТВУЮЩУЮ подсеть")
	}
	require.Len(t, committed, 1, "ровно одна транзакция обязана пройти")
	assert.Equal(t, ownerProject, committed[0].project,
		"пройти обязана та, чей проект совпадает с проектом подсети")

	var rows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM addresses WHERE internal_subnet_id = $1`, subID).Scan(&rows))
	assert.Equal(t, 1, rows, "в таблице обязана остаться ровно одна ссылающаяся строка")
}

// TestIntegration_Address_SubnetProjectPair_SettersRefuseForeignSubnet — путь
// АЛЛОКАТОРА (`SetInternalIPv4` / `SetInternalIPv6`) тоже меняет хранимую колонку
// `internal_subnet_id`, а значит тоже подпадает под ключ.
//
// Проба стоит здесь потому, что перевод отказа в контрактный тон живёт в трёх
// местах — вставка и два этих обмена, — и без неё два из трёх были бы кодом, ни
// разу не исполненным: пробы выше идут только через вставку. Тогда generic-ветка
// («<kind> has dependent resources», FailedPrecondition) вернулась бы на этот путь
// молча.
//
// Положительный контроль в каждом подслучае: подсеть СВОЕГО проекта тем же
// обменом принимается — иначе «отвергнуто» не отличалось бы от «этот обмен не
// работает вовсе».
func TestIntegration_Address_SubnetProjectPair_SettersRefuseForeignSubnet(t *testing.T) {
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

	const ownerProject = "prj-pair-set"
	ownSubnet := seedNetworkAndSubnet(t, ctx, r, ownerProject)
	foreignSubnet := seedNetworkAndSubnet(t, ctx, r, "prj-pair-set-other")

	for _, tc := range []struct {
		name string
		v6   bool
	}{{"internal ipv4", false}, {"internal ipv6", true}} {
		t.Run(tc.name, func(t *testing.T) {
			addr := internalAddress(ownerProject, ownSubnet, tc.v6)
			require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
				_, e := w.Addresses().Insert(ctx, addr)
				return e
			}))

			set := func(subnetID string) error {
				return legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
					var e error
					if tc.v6 {
						_, e = w.Addresses().SetInternalIPv6(ctx, addr.ID,
							&domain.InternalIpv6Spec{Address: "2001:db8::5", SubnetID: subnetID})
						return e
					}
					_, e = w.Addresses().SetInternalIPv4(ctx, addr.ID,
						&domain.InternalIpv4Spec{Address: "10.77.0.5", SubnetID: subnetID})
					return e
				})
			}

			require.NoError(t, set(ownSubnet), "подсеть своего проекта обязана приниматься")

			err := set(foreignSubnet)
			require.Error(t, err, "подсеть чужого проекта обязана быть отвергнута базой")
			require.ErrorIs(t, err, helpers.ErrNotFound,
				"полоса отказа — отсутствие, а не generic-ветка внешнего ключа")
			assert.Contains(t, err.Error(), fmt.Sprintf("Subnet %s not found", foreignSubnet),
				"названа обязана быть ТА подсеть, которую приняла бы хранимая колонка")
		})
	}
}

// TestIntegration_Address_SubnetProjectPair_ConcurrentSubnetDelete_OneWinner —
// та же пара под КОНКУРЕНТНЫМ удалением подсети: адрес вставляется и удерживается
// незакоммиченным, параллельная транзакция удаляет подсеть и обязана встать в
// очередь за строкой, а после коммита первой — получить отказ по состоянию.
//
// Проба стоит здесь потому, что 0033 СНИМАЕТ прежний одностолбцовый внешний ключ
// `addresses.internal_subnet_id → subnets(id)` как поглощённый парой. Свойство
// «живой адрес не даёт удалить свою подсеть» принадлежало снятому ключу; без
// этой пробы его потеря прошла бы молча — все остальные пробы остались бы
// зелёными.
func TestIntegration_Address_SubnetProjectPair_ConcurrentSubnetDelete_OneWinner(t *testing.T) {
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

	const ownerProject = "prj-pair-del"
	subID := seedNetworkAndSubnet(t, ctx, r, ownerProject)

	// TX-A: вставляет адрес и НЕ коммитит — держит строку подсети от удаления.
	addr := internalAddress(ownerProject, subID, false)
	wa, err := r.Writer(ctx)
	require.NoError(t, err)
	defer wa.Abort()
	_, err = wa.Addresses().Insert(ctx, addr)
	require.NoError(t, err)

	// TX-B: удаляет подсеть — обязана заблокироваться до исхода TX-A.
	bDone := make(chan error, 1)
	go func() {
		wb, werr := r.Writer(ctx)
		if werr != nil {
			bDone <- werr
			return
		}
		defer wb.Abort()
		if derr := wb.Subnets().Delete(ctx, subID); derr != nil {
			bDone <- derr
			return
		}
		bDone <- wb.Commit()
	}()
	waitForLockWaiter(t, ctx, pool)

	require.NoError(t, wa.Commit())
	delErr := <-bDone
	require.Error(t, delErr, "удаление подсети с живым адресом обязано быть отвергнуто")
	require.ErrorIs(t, delErr, helpers.ErrFailedPrecondition)

	// Положительный контроль: снятие адреса освобождает подсеть — отказ выше
	// относится к ЖИВОЙ ссылке, а не к удалению подсети вообще.
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Addresses().Delete(ctx, addr.ID)
	}))
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Subnets().Delete(ctx, subID)
	}))
}
