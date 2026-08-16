// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// leasePool — ограниченный пул аренд, ведущий себя как vpc по НАБЛЮДАЕМОМУ
// свойству: выдаёт, пока есть свободные, и принимает возврат. Ровно это и
// проверяется — что удаление возвращает КАЖДУЮ выданную аренду.
//
// Пул здесь, а не в vpc, намеренно: предмет #467 живёт в use-case удаления, а
// не в SQL. Пул на стороне владельца проверяется сквозной пробой; здесь
// проверяется то, что удаление вообще делает вызов на каждое семейство.
type leasePool struct {
	mu    sync.Mutex
	free  []string
	held  map[string]bool
	freed []string
}

func newLeasePool(size int) *leasePool {
	p := &leasePool{held: map[string]bool{}}
	for i := 0; i < size; i++ {
		p.free = append(p.free, fmt.Sprintf("adr-%03d", i))
	}
	return p
}

func (p *leasePool) take() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.free) == 0 {
		return "", fmt.Errorf("could not allocate: pool exhausted")
	}
	id := p.free[0]
	p.free = p.free[1:]
	p.held[id] = true
	return id, nil
}

func (p *leasePool) give(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.held[id] {
		return fmt.Errorf("release of a lease the pool never handed out: %q", id)
	}
	delete(p.held, id)
	p.free = append(p.free, id)
	p.freed = append(p.freed, id)
	return nil
}

func (p *leasePool) outstanding() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.held))
	for id := range p.held {
		out = append(out, id)
	}
	return out
}

// poolAddressClient — дублёр адресного клиента поверх пула. Пустой
// идентификатор отвергает так же, как боевой.
type poolAddressClient struct {
	fakeAddressClient
	pool *leasePool
}

func (c *poolAddressClient) FreeIP(ctx context.Context, addressID string) error {
	if addressID == "" {
		return fmt.Errorf("%w: address_id is empty", domain.ErrInvalidArg)
	}
	return c.pool.give(addressID)
}

func (c *poolAddressClient) ClearReference(ctx context.Context, addressID string) error {
	if addressID == "" {
		return fmt.Errorf("%w: address_id is empty", domain.ErrInvalidArg)
	}
	return nil
}

// Пул не деградирует: N заведений → N удалений → N заведений снова проходят.
//
// ПРЕДМЕТ (#467). Невозвращённая аренда исчерпывает пул, а исчерпанный пул даёт
// `could not allocate` — и дальше каскад по всему набору проб. Именно так
// невозврат одного семейства читался как «нестабильность» шарда: отказ приходил
// не туда, где дефект, и не тогда, когда он случился.
//
// Удаления идут КОНКУРЕНТНО: последовательный прогон не отличил бы «вернули всё»
// от «вернули по одному разу на процесс», а под параллелью пул — общий ресурс,
// на котором расхождение видно сразу.
func TestDelete_DualStack_PoolDoesNotDegradeAcrossCycles(t *testing.T) {
	t.Parallel()
	const n = 8                 // балансировщиков за цикл
	pool := newLeasePool(2 * n) // ровно под два семейства на каждый — запаса нет

	for cycle := 1; cycle <= 3; cycle++ {
		repo := newFakeRepo()
		opsRepo := newFakeOpsRepo()
		addr := &poolAddressClient{pool: pool}
		uc := NewDeleteLoadBalancerUseCase(repo, opsRepo, addr, slog.Default())

		ids := make([]string, 0, n)
		for i := 0; i < n; i++ {
			v4, err := pool.take()
			require.NoErrorf(t, err, "цикл %d: пул исчерпан на выдаче v4 — предыдущий цикл не вернул аренды", cycle)
			v6, err := pool.take()
			require.NoErrorf(t, err, "цикл %d: пул исчерпан на выдаче v6 — предыдущий цикл не вернул аренды", cycle)

			id := seedLB(t, repo, "prj-a", fmt.Sprintf("edge-%d-%d", cycle, i))
			rec := repo.lbs[id]
			rec.IPFamilies = []domain.IPVersion{domain.IPVersionV4, domain.IPVersionV6}
			rec.AddressV4, rec.AddressIDV4, rec.VipOriginV4 = domain.IPAddress("10.0.0."+v4), domain.AddressID(v4), domain.VipOriginAuto
			rec.AddressV6, rec.AddressIDV6, rec.VipOriginV6 = domain.IPAddress("2a02::"+v6), domain.AddressID(v6), domain.VipOriginAuto
			ids = append(ids, id)
		}

		var wg sync.WaitGroup
		opIDs := make([]string, n)
		for i, id := range ids {
			wg.Add(1)
			go func(i int, id string) {
				defer wg.Done()
				op, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
					NetworkLoadBalancerId: id,
				})
				require.NoError(t, err)
				opIDs[i] = op.ID
			}(i, id)
		}
		wg.Wait()
		for _, opID := range opIDs {
			require.Nil(t, awaitOpDone(t, opsRepo, opID).Error)
		}

		require.Emptyf(t, pool.outstanding(),
			"цикл %d: аренды не вернулись в пул — они висят на подсети, и вернуть их уже некому", cycle)
	}
}
