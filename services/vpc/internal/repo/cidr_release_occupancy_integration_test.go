// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/address"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/addresspool"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/subnet"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/cqrsadapter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Освобождение диапазона, в котором ЖИВУТ адреса, снимает несущее ограничение
// под живыми данными: диапазон достаётся следующему владельцу, и один адрес
// оказывается выдан дважды. Предмет проверки в сервисе уже распознан (у пула —
// для одной семьи, у сети — для подсетей), поэтому оба глагола обязаны
// спрашивать занятость, и обе семьи одинаково.

// --- пул: снятие v6-диапазона с живым адресом ---

func mkPoolWithV6(t *testing.T, ctx context.Context, r kacho.Repository, name string, v4, v6 []string) *kacho.AddressPoolRecord {
	t.Helper()
	uc := addresspool.NewCreateAddressPoolUseCase(r, nil) // nil zoneReg → skip zone-check
	p, err := uc.Execute(ctx, addresspool.CreatePoolReq{
		Name:         name,
		Kind:         domain.AddressPoolKindExternalPublic,
		ZoneID:       "zone-a",
		V4CIDRBlocks: v4,
		V6CIDRBlocks: v6,
	})
	require.NoError(t, err)
	return p
}

// Снятие v6-блока, из которого выдан внешний IPv6, обязано быть отвергнуто тем
// же предусловием и тем же тоном, что и v4 — иначе диапазон уходит другому пулу
// вместе с живым адресом, а счётчик выдачи нового владельца начинает с вершины
// того же префикса.
func TestIntegration_AddressPoolCIDR_RemoveV6InUse_FailedPrecondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	p := mkPoolWithV6(t, ctx, r, "pool-v6-inuse", []string{"198.51.100.0/28"}, []string{"2001:db8:c1::/64"})

	addrID := insertTestAddressFreelist(t, ctx, pgPool)
	var allocIP string
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		ip, e := w.Addresses().AllocateExternalIPv6(ctx, p.ID, addrID, "")
		allocIP = ip
		return e
	}))
	require.NotEmpty(t, allocIP)

	rmUC := addresspool.NewRemoveCidrBlocksUseCase(r)
	_, err = rmUC.Execute(ctx, p.ID, nil, []string{"2001:db8:c1::/64"})
	require.Error(t, err, "снятие v6-диапазона с выданным адресом обязано быть отвергнуто")
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "has allocated addresses")

	// TX abort: состав пула не изменился, диапазон по-прежнему числится за пулом.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	rec, err := rd.AddressPools().Get(ctx, p.ID)
	require.NoError(t, err)
	_ = rd.Close()
	assert.ElementsMatch(t, []string{"2001:db8:c1::/64"}, rec.V6CIDRBlocks)

	var cidrRows int
	require.NoError(t, pgPool.QueryRow(ctx,
		`SELECT count(*) FROM address_pool_cidrs WHERE pool_id = $1 AND block = '2001:db8:c1::/64'::cidr`,
		p.ID).Scan(&cidrRows))
	require.Equal(t, 1, cidrRows, "отвергнутое снятие не имеет права освободить диапазон для другого пула")
}

// Пустой v6-блок снимается штатно — предусловие не превращается в запрет
// сужения пула (negative-control к проверке выше).
func TestIntegration_AddressPoolCIDR_RemoveV6Clean_Succeeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	p := mkPoolWithV6(t, ctx, r, "pool-v6-clean", []string{"198.51.100.0/28"}, []string{"2001:db8:c2::/64"})

	rmUC := addresspool.NewRemoveCidrBlocksUseCase(r)
	updated, err := rmUC.Execute(ctx, p.ID, nil, []string{"2001:db8:c2::/64"})
	require.NoError(t, err)
	require.Empty(t, updated.V6CIDRBlocks)

	var cidrRows int
	require.NoError(t, pgPool.QueryRow(ctx,
		`SELECT count(*) FROM address_pool_cidrs WHERE pool_id = $1 AND block = '2001:db8:c2::/64'::cidr`,
		p.ID).Scan(&cidrRows))
	require.Zero(t, cidrRows, "снятие пустого диапазона обязано освободить его для будущих пулов")
}

// --- подсеть: снятие диапазона с живыми адресами ---

func mkSubnetWithBlocks(t *testing.T, r kacho.Repository, ctx context.Context, v4Blocks []string) (networkID, subnetID string) {
	t.Helper()
	networkID = ids.NewID(ids.PrefixNetwork)
	subnetID = ids.NewID(ids.PrefixSubnet)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		if _, e := w.Networks().Insert(ctx, &domain.Network{
			ID:        networkID,
			ProjectID: "b1gtestproject00000",
			Name:      domain.RcNameVPC("net-" + networkID[len(networkID)-6:]),
		}); e != nil {
			return e
		}
		_, e := w.Subnets().Insert(ctx, &domain.Subnet{
			ID:            subnetID,
			ProjectID:     "b1gtestproject00000",
			NetworkID:     networkID,
			Name:          domain.RcNameVPC("sub-" + subnetID[len(subnetID)-6:]),
			PlacementType: domain.PlacementZonal,
			ZoneID:        "zone-a",
			V4CidrBlocks:  v4Blocks,
		})
		return e
	}))
	return networkID, subnetID
}

// Снятие диапазона подсети, в котором живут внутренние адреса, обязано быть
// отвергнуто: строка, несущая запрет пересечения диапазонов внутри сети,
// исчезает вместе с диапазоном, после чего тот же адрес выдаётся во второй
// подсети той же сети — база это уже не поймает (уникальность внутреннего
// адреса ключуется подсетью).
func TestIntegration_SubnetCIDR_RemoveInUse_FailedPrecondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	_, subnetID := mkSubnetWithBlocks(t, r, ctx, []string{"10.0.0.0/24", "10.0.1.0/24"})

	// Живой внутренний адрес во ВТОРИЧНОМ блоке.
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Addresses().Insert(ctx, &domain.Address{
			ID: ids.NewID(ids.PrefixAddress), Name: fixtureName(),
			ProjectID:    "b1gtestproject00000",
			Type:         domain.AddressTypeInternal,
			IpVersion:    domain.IpVersionIPv4,
			InternalIpv4: &domain.InternalIpv4Spec{Address: "10.0.1.5", SubnetID: subnetID},
		})
		return e
	}))

	rmUC := subnet.NewRemoveCidrBlocksUseCase(r, repomock.NewOpsRepo())
	op, err := rmUC.Execute(ctx, subnetID, []string{"10.0.1.0/24"}, nil)
	require.NoError(t, err)
	require.True(t, op.Done)
	require.NotNil(t, op.Error, "снятие диапазона с живыми адресами обязано быть отвергнуто")
	st := status.FromProto(op.Error)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Equal(t, "subnet CIDR 10.0.1.0/24 has allocated addresses", st.Message())

	// TX abort: набор диапазонов и строка запрета пересечения на месте.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	rec, err := rd.Subnets().Get(ctx, subnetID)
	require.NoError(t, err)
	_ = rd.Close()
	assert.ElementsMatch(t, []string{"10.0.0.0/24", "10.0.1.0/24"}, rec.V4CidrBlocks)

	var blockRows int
	require.NoError(t, pgPool.QueryRow(ctx,
		`SELECT count(*) FROM subnet_cidr_blocks WHERE subnet_id = $1`, subnetID).Scan(&blockRows))
	require.Equal(t, 2, blockRows, "отвергнутое снятие не имеет права освободить диапазон внутри сети")
}

// Пустой диапазон подсети снимается штатно (negative-control).
func TestIntegration_SubnetCIDR_RemoveClean_Succeeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	_, subnetID := mkSubnetWithBlocks(t, r, ctx, []string{"10.1.0.0/24", "10.1.1.0/24"})

	rmUC := subnet.NewRemoveCidrBlocksUseCase(r, repomock.NewOpsRepo())
	op, err := rmUC.Execute(ctx, subnetID, []string{"10.1.1.0/24"}, nil)
	require.NoError(t, err)
	require.True(t, op.Done)
	require.Nil(t, op.Error, "снятие пустого диапазона обязано проходить")

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	rec, err := rd.Subnets().Get(ctx, subnetID)
	require.NoError(t, err)
	_ = rd.Close()
	require.ElementsMatch(t, []string{"10.1.0.0/24"}, rec.V4CidrBlocks)

	var blockRows int
	require.NoError(t, pgPool.QueryRow(ctx,
		`SELECT count(*) FROM subnet_cidr_blocks WHERE subnet_id = $1`, subnetID).Scan(&blockRows))
	require.Equal(t, 1, blockRows)
}

// Цена страницы адресов подсети и текст предусловия удаления не зависят от
// размера таблицы и от числа интерфейсов: план запроса обязан идти по индексу
// (а не читать таблицу всех проектов), а сообщение об отказе — быть ограничено.
func TestIntegration_Subnet_ReadPathCost_Bounded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	_, subnetID := mkSubnetWithBlocks(t, r, ctx, []string{"10.5.0.0/16"})

	// Шум: адреса ЧУЖИХ подсетей в той же таблице.
	noiseNet, noiseSub := mkSubnetWithBlocks(t, r, ctx, []string{"10.6.0.0/16"})
	_ = noiseNet
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		for i := 1; i < 200; i++ {
			if _, e := w.Addresses().Insert(ctx, &domain.Address{
				ID: ids.NewID(ids.PrefixAddress), Name: fixtureName(),
				ProjectID: "b1gtestproject00000",
				Type:      domain.AddressTypeInternal,
				IpVersion: domain.IpVersionIPv4,
				InternalIpv4: &domain.InternalIpv4Spec{
					Address: fmt.Sprintf("10.6.0.%d", i), SubnetID: noiseSub,
				},
			}); e != nil {
				return e
			}
		}
		return nil
	}))
	_, err = pgPool.Exec(ctx, `ANALYZE addresses`)
	require.NoError(t, err)

	// Гейт читает ТОТ ЖЕ предикат, который исполняет репозиторий
	// (helpers.AddressesBySubnetWhere) — иначе он проверял бы собственную копию
	// запроса и остался бы зелёным при возврате дефекта.
	rows, err := pgPool.Query(ctx, fmt.Sprintf(`
		EXPLAIN (FORMAT TEXT)
		SELECT id FROM addresses WHERE %s
		 ORDER BY created_at ASC, id ASC LIMIT 51`, helpers.AddressesBySubnetWhere), subnetID)
	require.NoError(t, err)
	var plan string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan += line + "\n"
	}
	rows.Close()
	require.NoError(t, rows.Err())
	// Индекс назван ТОТ, что остался: одноколоночный по подсети снят (#963) —
	// его ключ целиком лежит префиксом курсорного, и план идёт по нему, читая
	// только индекс (`Index Only Scan`). Утверждение сохраняет свой предмет:
	// страница подсети не читает таблицу адресов всех проектов.
	require.Contains(t, plan, "addresses_subnet_cursor_idx",
		"страница адресов подсети обязана идти по индексу подсети, а не читать таблицу всех проектов: %s", plan)
}

// Предусловие удаления подсети отвечает на вопрос «есть ли хоть один
// интерфейс», поэтому ни ответ базы, ни текст отказа не имеют права расти
// вместе с числом интерфейсов: сообщение статуса едет в трейлере ответа и на
// обычном размере подсети выходит за бюджеты заголовков у прокси на пути —
// задокументированное предусловие вырождается в транспортный сбой.
func TestIntegration_Subnet_DeletePrecondition_MessageBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	_, subnetID := mkSubnetWithBlocks(t, r, ctx, []string{"10.7.0.0/24"})

	const nicCount = 25
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		for i := 0; i < nicCount; i++ {
			if _, e := w.NetworkInterfaces().Insert(ctx, &domain.NetworkInterface{
				ID: ids.NewID(ids.PrefixNetworkInterface), Name: fixtureName(),
				ProjectID: "b1gtestproject00000",
				SubnetID:  subnetID,
				MAC:       fmt.Sprintf("02:00:00:00:%02x:%02x", i/256, i%256),
				Status:    domain.NIStatusAvailable,
			}); e != nil {
				return e
			}
		}
		return nil
	}))

	delUC := subnet.NewDeleteSubnetUseCase(r,
		cqrsadapter.NewNetworkInterface(r), repomock.NewOpsRepo())
	_, err = delUC.Execute(ctx, subnetID)
	require.Error(t, err, "подсеть с интерфейсами удалить нельзя")
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), fmt.Sprintf("subnet %s has %d network interface(s)", subnetID, nicCount),
		"отказ обязан называть полное число")
	assert.Contains(t, st.Message(), "and 15 more", "перечень обязан быть ограничен, остальное — числом")
	assert.Less(t, len(st.Message()), 512, "текст отказа обязан быть ограничен по построению")
}

// Выдача внутреннего адреса обязана быть сериализована с мутацией набора
// диапазонов подсети: она ЧИТАЕТ набор и на основании прочитанного пишет адрес.
// Проба детерминированная — держим на строке подсети ту же исключительную
// блокировку, что берёт снятие диапазона, и требуем, чтобы выдача её ДОЖДАЛАСЬ.
// Со слабым чтением (без share-lock) выдача проходит мимо и записывает адрес по
// снимку, которого уже нет: тогда снятие успевает освободить диапазон, а адрес
// остаётся вне объявленных диапазонов своей подсети.
//
// Неявная блокировка внешнего ключа этого не даёт: она берётся, только когда
// меняется колонка подсети, а выдача адреса её не меняет.
func TestIntegration_SubnetCIDR_InternalAllocate_WaitsForRangeMutation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	_, subnetID := mkSubnetWithBlocks(t, r, ctx, []string{"10.30.0.0/24", "10.30.1.0/24"})

	addrID := ids.NewID(ids.PrefixAddress)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Addresses().Insert(ctx, &domain.Address{
			ID: addrID, Name: domain.RcNameVPC(addrID),
			ProjectID:    "b1gtestproject00000",
			Type:         domain.AddressTypeInternal,
			IpVersion:    domain.IpVersionIPv4,
			Reserved:     true,
			InternalIpv4: &domain.InternalIpv4Spec{SubnetID: subnetID},
		})
		return e
	}))

	// Держим строку подсети под той же блокировкой, что берёт снятие диапазона.
	holder, err := pgPool.Begin(ctx)
	require.NoError(t, err)
	var lockedID string
	require.NoError(t, holder.QueryRow(ctx,
		`SELECT id FROM subnets WHERE id = $1 FOR UPDATE`, subnetID).Scan(&lockedID))

	done := make(chan error, 1)
	go func() {
		waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		uc := address.NewAllocateUseCase(r, nil)
		_, e := uc.AllocateInternalIP(waitCtx, addrID)
		done <- e
	}()

	select {
	case e := <-done:
		_ = holder.Rollback(ctx)
		t.Fatalf("выдача адреса не дождалась мутатора набора диапазонов (err=%v) — значит набор читается без блокировки, и адрес может быть записан по снимку, которого уже нет", e)
	case <-time.After(1500 * time.Millisecond):
		// ожидаемо: выдача ждёт держателя блокировки
	}

	require.NoError(t, holder.Rollback(ctx))

	select {
	case e := <-done:
		require.NoError(t, e, "после снятия блокировки выдача обязана пройти")
	case <-time.After(5 * time.Second):
		t.Fatal("выдача не завершилась после освобождения строки подсети")
	}

	// Выданный адрес обязан лежать в объявленных диапазонах подсети.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	rec, err := rd.Addresses().Get(ctx, addrID)
	require.NoError(t, err)
	sub, err := rd.Subnets().Get(ctx, subnetID)
	require.NoError(t, err)
	_ = rd.Close()
	ip := netip.MustParseAddr(rec.InternalIpv4.Address)
	inDeclared := false
	for _, raw := range sub.V4CidrBlocks {
		if netip.MustParsePrefix(raw).Contains(ip) {
			inDeclared = true
			break
		}
	}
	require.True(t, inDeclared, "выданный адрес %s вне объявленных диапазонов %v", ip, sub.V4CidrBlocks)
}

// Зеркальные семьи предусловия занятости: у подсети — внутренний IPv6, у пула —
// внешний IPv4. Без них «обе семьи» проверено лишь наполовину, и вторая
// половина предиката держится на честном слове.
func TestIntegration_CIDRRelease_MirrorFamilies(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	// --- подсеть: занят ВНУТРЕННИЙ IPv6 ---
	netID := ids.NewID(ids.PrefixNetwork)
	subnetID := ids.NewID(ids.PrefixSubnet)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		if _, e := w.Networks().Insert(ctx, &domain.Network{
			ID: netID, ProjectID: "b1gtestproject00000",
			Name: domain.RcNameVPC("net-" + netID[len(netID)-6:]),
		}); e != nil {
			return e
		}
		if _, e := w.Subnets().Insert(ctx, &domain.Subnet{
			ID: subnetID, ProjectID: "b1gtestproject00000", NetworkID: netID,
			Name:          domain.RcNameVPC("sub-" + subnetID[len(subnetID)-6:]),
			PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
			V4CidrBlocks: []string{"10.40.0.0/24", "10.41.0.0/24"},
			V6CidrBlocks: []string{"2001:db8:f0::/64", "2001:db8:f1::/64"},
		}); e != nil {
			return e
		}
		_, e := w.Addresses().Insert(ctx, &domain.Address{
			ID: ids.NewID(ids.PrefixAddress), Name: fixtureName(),
			ProjectID:    "b1gtestproject00000",
			Type:         domain.AddressTypeInternal,
			IpVersion:    domain.IpVersionIPv6,
			InternalIpv6: &domain.InternalIpv6Spec{Address: "2001:db8:f1::9", SubnetID: subnetID},
		})
		return e
	}))

	rmSub := subnet.NewRemoveCidrBlocksUseCase(r, repomock.NewOpsRepo())
	op, err := rmSub.Execute(ctx, subnetID, nil, []string{"2001:db8:f1::/64"})
	require.NoError(t, err)
	require.True(t, op.Done)
	require.NotNil(t, op.Error, "снятие v6-диапазона подсети с живым адресом обязано быть отвергнуто")
	assert.Equal(t, "subnet CIDR 2001:db8:f1::/64 has allocated addresses",
		status.FromProto(op.Error).Message())

	// Отказ называет ТОЛЬКО занятый диапазон: чистый в том же запросе не
	// объявляется занятым.
	op2, err := rmSub.Execute(ctx, subnetID, []string{"10.41.0.0/24"}, []string{"2001:db8:f1::/64"})
	require.NoError(t, err)
	require.NotNil(t, op2.Error)
	assert.Equal(t, "subnet CIDR 2001:db8:f1::/64 has allocated addresses",
		status.FromProto(op2.Error).Message(),
		"в отказе не должно быть чистого диапазона из того же запроса")

	// --- пул: занят ВНЕШНИЙ IPv4 в одном из двух снимаемых диапазонов ---
	p := mkPoolWithV6(t, ctx, r, "pool-mirror", []string{"198.51.100.0/28", "203.0.113.0/28"}, nil)
	autoID := insertTestAddressFreelist(t, ctx, pgPool)
	require.NoError(t, freelistWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Addresses().AllocateIPFromFreelist(ctx, p.ID, autoID)
		return e
	}))

	rmPool := addresspool.NewRemoveCidrBlocksUseCase(r)
	_, err = rmPool.Execute(ctx, p.ID, []string{"198.51.100.0/28"}, nil)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Equal(t, "address pool CIDR 198.51.100.0/28 has allocated addresses", st.Message())
}
