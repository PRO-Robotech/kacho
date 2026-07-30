// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/addresspool"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/subnet"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
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
	defer pgPool.Close()
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
	defer pgPool.Close()
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

func mkSubnetWithBlocks(t *testing.T, ctx context.Context, r kacho.Repository, v4Blocks []string) (networkID, subnetID string) {
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
	defer pgPool.Close()
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	_, subnetID := mkSubnetWithBlocks(t, ctx, r, []string{"10.0.0.0/24", "10.0.1.0/24"})

	// Живой внутренний адрес во ВТОРИЧНОМ блоке.
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Addresses().Insert(ctx, &domain.Address{
			ID:           ids.NewID(ids.PrefixAddress),
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
	defer pgPool.Close()
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	_, subnetID := mkSubnetWithBlocks(t, ctx, r, []string{"10.1.0.0/24", "10.1.1.0/24"})

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
