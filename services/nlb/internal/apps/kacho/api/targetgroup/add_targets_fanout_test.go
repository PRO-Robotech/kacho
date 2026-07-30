// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients/compute"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Пакетная валидация целей: сколько РАЗ спрошен сосед и НАСКОЛЬКО последовательно.
//
// Контракт принимает до domain.MaxTargetsPerGroup целей за вызов, и каждая
// instance-цель стоит двух обращений (compute + geo), каждое со своим 5s
// дедлайном. Пока обход строго последователен и один и тот же вопрос про зону
// задаётся заново на каждую цель, полная пачка не укладывается в потолок
// исполнения операции — и тогда не добавляется НИ ОДНА цель (writer-TX
// открывается после всех проверок). Эти тесты называют оба числа.

// peerBudget — модельные величины утверждения о бюджете. Обе — не «магия»:
//   - peerLatency: наблюдаемая задержка соседа, при которой дефект проявляется
//     (полоса срабатывания — задержка ∈ (1.2s, 5s) при 200 последовательных
//     round-trip'ах);
//   - opBudget: потолок исполнения одной operation-fn (pkg/operations
//     defaultOpTimeout = 4m; константа неэкспортирована, поэтому продублирована
//     здесь ЯВНО — если она изменится, тест перестанет описывать реальность и
//     это надо заметить).
const (
	peerLatency = 1500 * time.Millisecond
	opBudget    = 4 * time.Minute
)

// peerCallRecorder — счётчик обращений к соседям в пределах одной операции +
// наблюдаемая параллельность.
//
// Утверждение о параллельности сделано детерминированным ЗАЩЁЛКОЙ, а не
// ожиданием по часам: она открывается, как только внутри клиента одновременно
// оказались `want` вызывающих, и остаётся открытой навсегда. Последовательный
// код защёлку никогда не наберёт и откроет её по failsafe — заплатив эту
// задержку РОВНО ОДИН раз за весь прогон, а не на каждом вызове.
type peerCallRecorder struct {
	mu       sync.Mutex
	calls    map[string]int
	inFlight int
	maxSeen  int

	want     int // 0 → защёлки нет, только счёт
	failsafe time.Duration
	gate     chan struct{}
	gateOnce sync.Once
}

func newPeerCallRecorder(want int, failsafe time.Duration) *peerCallRecorder {
	return &peerCallRecorder{
		calls: map[string]int{}, want: want, failsafe: failsafe,
		gate: make(chan struct{}),
	}
}

func (r *peerCallRecorder) enter(kind string) {
	r.mu.Lock()
	r.calls[kind]++
	r.inFlight++
	if r.inFlight > r.maxSeen {
		r.maxSeen = r.inFlight
	}
	reached := r.want > 0 && r.inFlight >= r.want
	r.mu.Unlock()
	if reached {
		r.openGate()
	}
}

func (r *peerCallRecorder) leave() {
	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()
}

func (r *peerCallRecorder) openGate() { r.gateOnce.Do(func() { close(r.gate) }) }

// awaitGate — держит вызывающего внутри клиента, пока не наберётся want
// одновременных обращений либо не истечёт failsafe (тогда защёлка открывается
// навсегда: последовательный прогон платит её один раз).
func (r *peerCallRecorder) awaitGate() {
	if r.want == 0 {
		return
	}
	select {
	case <-r.gate:
	case <-time.After(r.failsafe):
		r.openGate()
	}
}

func (r *peerCallRecorder) count(kind string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[kind]
}

func (r *peerCallRecorder) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		n += c
	}
	return n
}

func (r *peerCallRecorder) maxConcurrency() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maxSeen
}

// ---- считающие двойники соседей -------------------------------------------

type countingInstanceClient struct {
	rec    *peerCallRecorder
	zoneID string
	gated  bool // держать вызывающего на защёлке (для утверждения о параллельности)
	panicOn string
}

func (c *countingInstanceClient) Get(_ context.Context, id string) (*compute.Instance, error) {
	c.rec.enter("instance")
	defer c.rec.leave()
	if c.gated {
		c.rec.awaitGate()
	}
	if c.panicOn != "" && id == c.panicOn {
		panic("peer client blew up on " + id)
	}
	return &compute.Instance{ID: id, ZoneID: c.zoneID, PrimaryNICAddress: "10.0.0.10"}, nil
}

type countingZoneRegionClient struct {
	rec      *peerCallRecorder
	regionID string
}

func (c *countingZoneRegionClient) RegionOfZone(_ context.Context, _ string) (string, error) {
	c.rec.enter("zone")
	defer c.rec.leave()
	return c.regionID, nil
}

type countingSubnetClient struct {
	rec *peerCallRecorder
}

func (c *countingSubnetClient) Get(_ context.Context, id string) (*vpc.Subnet, error) {
	c.rec.enter("subnet")
	defer c.rec.leave()
	return &vpc.Subnet{
		ID: id, PlacementType: "ZONAL", ZoneID: "zone-x", RegionID: "ru-central1",
		V4CIDRBlocks: []string{"10.0.0.0/24"},
	}, nil
}

// hundredInstanceTargets — полная разрешённая пачка целей: 100 РАЗНЫХ инстансов
// в ОДНОЙ зоне (вопрос про зону — один и тот же 100 раз).
func hundredInstanceTargets() []*lbv1.Target {
	out := make([]*lbv1.Target, 0, domain.MaxTargetsPerGroup)
	for i := 0; i < domain.MaxTargetsPerGroup; i++ {
		out = append(out, &lbv1.Target{
			Identity: &lbv1.Target_InstanceId{InstanceId: fmt.Sprintf("epd-inst%03d", i)},
			Weight:   100,
		})
	}
	return out
}

// hundredIPRefTargets — 100 адресов ОДНОЙ подсети (вопрос про подсеть — один и
// тот же 100 раз).
func hundredIPRefTargets() []*lbv1.Target {
	out := make([]*lbv1.Target, 0, domain.MaxTargetsPerGroup)
	for i := 0; i < domain.MaxTargetsPerGroup; i++ {
		out = append(out, &lbv1.Target{
			Identity: &lbv1.Target_IpRef{IpRef: &lbv1.Target_InCloudIP{
				SubnetId: "e9b-one-subnet", Address: fmt.Sprintf("10.0.0.%d", i+1),
			}},
			Weight: 100,
		})
	}
	return out
}

// Один и тот же вопрос про зону задаётся ОДИН раз на операцию, а не на цель.
func TestAdd_HundredTargetsOneZone_AsksGeoOnce(t *testing.T) {
	rec := newPeerCallRecorder(0, 0)
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "one-zone-100")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewAddTargetsUseCase(repo, opsRepo,
		&countingInstanceClient{rec: rec, zoneID: "zone-x"},
		&fakeNICClient{},
		&countingSubnetClient{rec: rec},
		&countingZoneRegionClient{rec: rec, regionID: "ru-central1"},
		nil,
	)

	op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets:       hundredInstanceTargets(),
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nilf(t, final.Error, "op error: %v", final.Error)

	require.Equal(t, domain.MaxTargetsPerGroup, rec.count("instance"),
		"каждая цель — свой инстанс, поэтому 100 вопросов про инстансы")
	require.Equal(t, 1, rec.count("zone"),
		"зона у всех целей одна: geo спрашивается ОДИН раз на операцию, а не на цель")
}

// Тот же инвариант для ip_ref-целей: одна подсеть — один вопрос к vpc.
func TestAdd_HundredIPRefsOneSubnet_AsksVpcOnce(t *testing.T) {
	rec := newPeerCallRecorder(0, 0)
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "one-subnet-100")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewAddTargetsUseCase(repo, opsRepo,
		&countingInstanceClient{rec: rec, zoneID: "zone-x"},
		&fakeNICClient{},
		&countingSubnetClient{rec: rec},
		&countingZoneRegionClient{rec: rec, regionID: "ru-central1"},
		nil,
	)

	op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets:       hundredIPRefTargets(),
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nilf(t, final.Error, "op error: %v", final.Error)

	require.Equal(t, 1, rec.count("subnet"),
		"подсеть у всех целей одна: vpc спрашивается ОДИН раз на операцию, а не на цель")
}

// Полная пачка целей укладывается в потолок исполнения операции.
//
// Проверка НЕ ждёт по часам: она измеряет два числа — сколько обращений к
// соседям сделано и какая параллельность при этом наблюдалась — и считает по
// ним модельный бюджет (волна = maxConcurrency одновременных round-trip'ов
// длиной peerLatency). Строго последовательный обход даёт 200 волн × 1.5s =
// 300s > 240s, то есть операция терминально падает по дедлайну и не добавляет
// ни одной цели.
func TestAdd_HundredTargets_FitsOperationBudget(t *testing.T) {
	rec := newPeerCallRecorder(targetPeerFanout, time.Second)
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "budget-100")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewAddTargetsUseCase(repo, opsRepo,
		&countingInstanceClient{rec: rec, zoneID: "zone-x", gated: true},
		&fakeNICClient{},
		&countingSubnetClient{rec: rec},
		&countingZoneRegionClient{rec: rec, regionID: "ru-central1"},
		nil,
	)

	op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets:       hundredInstanceTargets(),
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nilf(t, final.Error, "op error: %v", final.Error)

	conc := rec.maxConcurrency()
	require.GreaterOrEqual(t, conc, targetPeerFanout,
		"peer-валидация пачки обязана идти ограниченным фан-аутом, а не строго по одной")

	total := rec.total()
	waves := (total + conc - 1) / conc
	virtual := time.Duration(waves) * peerLatency
	require.Lessf(t, virtual, opBudget,
		"%d обращений при параллельности %d = %d волн × %s = %s, потолок операции %s",
		total, conc, waves, peerLatency, virtual, opBudget)
}

// Паника внутри peer-валидации остаётся ПОЙМАННОЙ: фан-аут не выносит работу из
// горутины, вокруг которой стоит recover воркера. Иначе процесс падал бы целиком.
func TestAdd_PeerValidatePanic_StaysContained(t *testing.T) {
	rec := newPeerCallRecorder(0, 0)
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "panic-contained")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewAddTargetsUseCase(repo, opsRepo,
		&countingInstanceClient{rec: rec, zoneID: "zone-x", panicOn: "epd-inst042"},
		&fakeNICClient{},
		&countingSubnetClient{rec: rec},
		&countingZoneRegionClient{rec: rec, regionID: "ru-central1"},
		nil,
	)

	op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets:       hundredInstanceTargets(),
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error, "паника обязана стать терминальной ошибкой операции")
	require.Equal(t, int32(codes.Internal), final.Error.Code)
	require.Equal(t, "internal worker error", final.Error.Message,
		"фиксированный текст воркера: наружу не течёт ни паника, ни стек")
	require.Empty(t, repo.outboxEvents(), "цели не добавлены — writer-TX не открывался")
}
