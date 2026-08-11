// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package geo

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"

	geopb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/peer"
	"github.com/PRO-Robotech/kacho/pkg/retry"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// DefaultRegionGetTimeout — per-call deadline применяемый к
// RegionService.Get, когда client построен без явного timeout'а. Без него
// retry.OnUnavailable не бывает бесконечным (MaxElapsed 30s), но НИ один из
// его retry-попыток не ограничен по времени сам по себе — зависший
// (не отвечающий, не Unavailable) peer парковал бы вызывающую горутину
// навсегда (см. check_client.go DefaultCheckTimeout — тот же класс проблемы).
const DefaultRegionGetTimeout = 5 * time.Second

// Region — lean projection ресурса kacho-geo.Region. Используется sync-валидацией
// NetworkLoadBalancer.region_id / TargetGroup.region_id. Зоны региона не
// перечисляются — kacho-nlb region-precheck зоны не использует (см. doc.go).
type Region struct {
	ID   string
	Name string
}

// RegionClient — port-интерфейс для service-слоя.
type RegionClient interface {
	// Get возвращает Region. Семантика ошибок — по полосам резолва
	// (api-conventions.md §By-lane code-split), собранным статусом:
	//   - kacho-geo NotFound (regionID не существует) → полоса peer-validate:
	//     FAILED_PRECONDITION "Region <id> not found" + PEER_RESOURCE_MISSING.
	//     Консумер здесь не «не нашёл своё», а «предусловие на ЧУЖОЙ ресурс не
	//     выполнено».
	//   - PermissionDenied (region — публичный read-only справочник, но edge-
	//     case при agg-route filtering) → ТА ЖЕ полоса и тот же текст: иначе по
	//     коду отличали бы «нет региона» от «есть, но не виден» — оракул.
	//   - Unavailable/DeadlineExceeded → UNAVAILABLE + PEER_UNAVAILABLE
	//     (fail-closed на мутации).
	//   - Любая другая ошибка → wrapped error без обёртки полосы.
	Get(ctx context.Context, regionID string) (*Region, error)
}

// regionClient — реализация RegionClient через gRPC. Stateless pass-through:
// один geo.RegionService.Get-вызов под retry.OnUnavailable, без кэша.
type regionClient struct {
	regions geopb.RegionServiceClient
	timeout time.Duration
}

// NewRegionClient оборачивает grpc-conn в typed adapter. conn — `clients.Build`.
// RegionService живёт на public-listener kacho-geo (9090) — публичный read-only
// справочник Geography. Per-call timeout — DefaultRegionGetTimeout.
func NewRegionClient(conn grpc.ClientConnInterface) RegionClient {
	return NewRegionClientWithTimeout(conn, DefaultRegionGetTimeout)
}

// NewRegionClientWithTimeout — как NewRegionClient, но с явным per-call
// timeout'ом. timeout<=0 → DefaultRegionGetTimeout.
func NewRegionClientWithTimeout(conn grpc.ClientConnInterface, timeout time.Duration) RegionClient {
	if conn == nil {
		return nil
	}
	return &regionClient{regions: geopb.NewRegionServiceClient(conn), timeout: resolveRegionTimeout(timeout)}
}

// NewRegionClientFromStubs — конструктор для тестов: принимает напрямую stub.
func NewRegionClientFromStubs(regions geopb.RegionServiceClient) RegionClient {
	return NewRegionClientFromStubsWithTimeout(regions, DefaultRegionGetTimeout)
}

// NewRegionClientFromStubsWithTimeout — как NewRegionClientFromStubs, но с
// явным per-call timeout'ом (используется тестами concurrency/timeout-фиксов).
func NewRegionClientFromStubsWithTimeout(regions geopb.RegionServiceClient, timeout time.Duration) RegionClient {
	if regions == nil {
		return nil
	}
	return &regionClient{regions: regions, timeout: resolveRegionTimeout(timeout)}
}

func resolveRegionTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultRegionGetTimeout
	}
	return d
}

// Get — см. контракт RegionClient.Get.
func (c *regionClient) Get(ctx context.Context, regionID string) (*Region, error) {
	if regionID == "" {
		return nil, fmt.Errorf("%w: region_id is empty", domain.ErrInvalidArg)
	}

	// Per-call deadline — bounds the ENTIRE retry.OnUnavailable operation,
	// independent of the caller's own ctx (architecture.md "Per-call deadline
	// на КАЖДОМ внешнем вызове"; see check_client.go DefaultCheckTimeout for
	// the sibling rationale).
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var resp *geopb.Region
	if err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		var rerr error
		resp, rerr = c.regions.Get(auth.PropagateOutgoing(ctx), &geopb.GetRegionRequest{RegionId: regionID})
		return rerr
	}); err != nil {
		return nil, mapRegionErr(regionID, err)
	}

	return &Region{ID: resp.GetId(), Name: resp.GetName()}, nil
}

// mapRegionErr транслирует ответ владельца Geography в полосу резолва.
//
// Возвращается СОБРАННЫЙ статус, а не sentinel: полоса обязана пережить
// сервисный маппер (shared.PeerErrToStatus отдаёт готовый статус как есть),
// иначе машинный признак теряется ровно там, где собирается ответ клиенту.
//
// Текст промаха прежний — "Region <id> not found". Сменился код (полоса
// peer-validate) и ушёл двойной sentinel-префикс, которым сообщение обрастало
// по дороге: клиент видел "region: invalid argument: Region <id> not found",
// где «invalid argument» — текст внутреннего sentinel, а не утверждение о
// регионе.
func mapRegionErr(regionID string, err error) error {
	// Полосу выбирает носитель (pkg/peer). Прежде здесь стоял свой разбор кодов, и
	// он уже разошёлся с соседними файлами того же сервиса: PermissionDenied тут
	// сводился к промаху, а в клиенте зон — к отдельному sentinel'у.
	switch o := peer.Classify(err); {
	case o.RefusedReference():
		// Промах, отказ в правах (edge-case agg-route filtering) и негодный по
		// мнению владельца id — одна полоса и один текст: иначе по коду отличали
		// бы «нет региона» от «есть, но не виден».
		return regionLane(o, regionID)
	case o.Transient():
		return regionLane(o, regionID)
	}
	// Непонятый ответ соседа полосой контракта не притворяется.
	return fmt.Errorf("geo region get %q: %w", regionID, err)
}

// serviceDomain — источник отказа в ErrorInfo.domain. Назван один раз на сервис.
const serviceDomain = "nlb"

// regionLane — ответ ребра nlb→geo. Тексты — часть контракта:
// "Region <id> not found" утверждается пробами и e2e, а "region lookup
// unavailable" дословно повторяет то, что прежде собирал сервисный маппер.
//
// Код и машинный признак берутся у полосы, поэтому разойтись не могут; прежде
// текст промаха обрастал по дороге двойным sentinel-префиксом, и клиент видел
// "region: invalid argument: Region <id> not found", где «invalid argument» —
// текст внутреннего sentinel'а, а не утверждение о регионе.
func regionLane(o peer.Outcome, regionID string) error {
	return o.Status(
		peer.Ref{Service: serviceDomain, ResourceType: "geo.region", ResourceID: regionID},
		peer.Prose{Missing: "Region %s not found", Unavailable: "region lookup unavailable"})
}
