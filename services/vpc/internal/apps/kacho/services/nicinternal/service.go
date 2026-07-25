// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package nicinternal — координация NIC↔Instance attach на стороне владельца
// (kacho-vpc), обслуживает InternalNetworkInterfaceService (:9091). Оживляет
// отложенную в KAC-266 явную привязку NIC↔Instance.
//
// Attach — compute-инициируемый и self-describing: запрос несёт
// instance_id/name/zone/project, vpc валидирует СВОИ строки network_interfaces +
// subnets атомарным CAS на used_by_id (zone-coherence + anycast-исключение) и
// НИКОГДА не зовёт compute (иначе цикл compute↔vpc). tenant-мутация остаётся async
// через compute-`AttachNetworkInterface`-Operation, поэтому ban #9 не нарушен.
//
// AuthN(mTLS)+AuthZ(per-RPC Check) энфорсятся цепочкой интерсепторов :9091 (см.
// cmd/vpc/main.go internalUnary + check.PermissionMap): Attach/Detach — editor на
// vpc_network_interface:<nic_id>, ListByInstance — viewer cluster-scoped. Повторная
// проверка в этом сервисе не нужна (gateway/interceptor scope-extractor энфорсит).
package nicinternal

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// attachMaxRetries — верхняя граница retry auto-index CAS при slot-collision
// (ErrNICIndexTaken). Реалистичное число NIC на инстанс мало; запас с большим
// хвостом — защита от патологической гонки, а не нормальный путь.
const attachMaxRetries = 64

// ZoneRegistry — узкий port резолва зоны в её регион у владельца Geography
// (kacho-geo, `geo.v1.ZoneService.Get` → `Zone.region_id`). Регион НИКОГДА не
// выводится из имени зоны — имена региона и зоны произвольны. Impl —
// `*clients.GeoZoneClient` (per-call deadline, positive-TTL кэш).
type ZoneRegistry = repo.ZoneRegistry

// Service — координатор NIC↔Instance attach поверх CQRS-Repository.
type Service struct {
	repo kachorepo.Repository
	// zones — резолв зоны инстанса в регион для региональной полосы
	// placement-coherence (anycast-подсеть). nil → полоса fail-closed.
	zones ZoneRegistry
}

// NewService создаёт Service.
func NewService(r kachorepo.Repository) *Service {
	return &Service{repo: r}
}

// WithZoneRegistry подключает резолвер зоны инстанса в её регион (владелец
// Geography — kacho-geo). Нужен для региональной полосы placement-coherence:
// REGIONAL (anycast) подсеть зоны не несёт, поэтому её region_id сверяется с
// регионом ЗОНЫ инстанса. Регион из имени зоны НЕ выводится — только резолв у
// владельца. nil / ошибка резолва → регион не передаётся в CAS, и attach в
// anycast-подсеть fail-closed; зональная полоса при этом продолжает работать.
// Вызывается один раз из composition-root до приёма трафика.
func (s *Service) WithZoneRegistry(z ZoneRegistry) *Service {
	s.zones = z
	return s
}

// resolveInstanceRegion — регион зоны инстанса через владельца Geography.
// Ошибка/отсутствие резолвера → "" (CAS не сматчит anycast-полосу → fail-closed
// с ErrNICRegionUnverifiable), ZONAL-полоса не затрагивается.
func (s *Service) resolveInstanceRegion(ctx context.Context, zoneID string) string {
	if s.zones == nil || zoneID == "" {
		return ""
	}
	z, err := s.zones.Get(ctx, zoneID)
	if err != nil || z == nil {
		return ""
	}
	return z.RegionID
}

// Attach — атомарный CAS NIC↔Instance (§3a). p.Index >=0 → явный слот; <0
// (kachorepo.AutoIndex) → первый свободный, с retry на slot-collision. Возвращает
// обновлённую NIC-запись либо gRPC-status с contract-текстом:
//   - "NetworkInterface is in use"                               (NIC занят другим инстансом)
//   - "NetworkInterface subnet is in zone %s, instance zone is %s" (ZONAL zone mismatch)
//   - "NetworkInterface subnet must be in the same region as the instance" (REGIONAL/anycast)
//   - "zone region lookup unavailable"                            (регион зоны не разрешён)
//   - "Network interface %s not found"                           (NIC отсутствует)
//   - leak-safe "attach network interface failed" fallback       (незамапленная DB-ошибка)
func (s *Service) Attach(ctx context.Context, p kachorepo.AttachNICParams) (*kachorepo.NetworkInterfaceRecord, error) {
	auto := p.Index < 0
	for attempt := 0; ; attempt++ {
		rec, err := s.attachOnce(ctx, p)
		if err == nil {
			return rec, nil
		}
		if errors.Is(err, repo.ErrNICIndexTaken) {
			// Явный слот занят — не retry'им (пользователь выбрал конкретный index).
			if !auto {
				return nil, status.Errorf(codes.FailedPrecondition,
					"network interface index %d is already in use on instance %s", p.Index, p.InstanceID)
			}
			// auto-index: слот занят конкурентным attach → пересчитать и повторить.
			if attempt < attachMaxRetries {
				continue
			}
			return nil, status.Error(codes.FailedPrecondition, "no free network interface slot on instance")
		}
		return nil, s.mapAttachErr(err, p.NICID)
	}
}

// attachOnce — одна попытка attach-CAS в свежей writer-TX (commit при успехе, abort
// при ошибке). Ошибку не транслирует — это делает Attach (для retry-классификации).
func (s *Service) attachOnce(ctx context.Context, p kachorepo.AttachNICParams) (*kachorepo.NetworkInterfaceRecord, error) {
	if p.InstanceRegionID == "" {
		p.InstanceRegionID = s.resolveInstanceRegion(ctx, p.InstanceZoneID)
	}
	w, err := s.repo.Writer(ctx)
	if err != nil {
		return nil, err
	}
	rec, err := w.NetworkInterfaces().AttachToInstance(ctx, p)
	if err != nil {
		w.Abort()
		return nil, err
	}
	if cerr := w.Commit(); cerr != nil {
		return nil, cerr
	}
	return rec, nil
}

// mapAttachErr — repo-ошибка → gRPC-status с точным contract-текстом (behaviour-level).
func (s *Service) mapAttachErr(err error, nicID string) error {
	var zoneErr *repo.NICZoneMismatchError
	switch {
	case errors.Is(err, repo.ErrNICInUse):
		return status.Error(codes.FailedPrecondition, "NetworkInterface is in use")
	case errors.As(err, &zoneErr):
		return status.Errorf(codes.FailedPrecondition,
			"NetworkInterface subnet is in zone %s, instance zone is %s", zoneErr.SubnetZone, zoneErr.InstanceZone)
	case errors.Is(err, repo.ErrNICRegionMismatch):
		return status.Error(codes.FailedPrecondition,
			"NetworkInterface subnet must be in the same region as the instance")
	case errors.Is(err, repo.ErrNICRegionUnverifiable):
		return status.Error(codes.Unavailable, "zone region lookup unavailable")
	case errors.Is(err, repo.ErrNotFound):
		return status.Errorf(codes.NotFound, "Network interface %s not found", nicID)
	default:
		// Незамапленная DB/pgx-ошибка → фикс. leak-safe текст (INTERNAL, no leak).
		return serviceerr.MapRepoErrLeakSafe(err, "attach network interface failed")
	}
}

// Detach — идемпотентное снятие привязки NIC↔Instance (§3a). Возвращает обновлённую
// NIC-запись; уже отвязанный / привязанный к другому инстансу NIC → OK (no-op);
// отсутствующий NIC → NotFound.
func (s *Service) Detach(ctx context.Context, nicID, instanceID string) (*kachorepo.NetworkInterfaceRecord, error) {
	w, err := s.repo.Writer(ctx)
	if err != nil {
		return nil, err
	}
	rec, err := w.NetworkInterfaces().DetachFromInstance(ctx, nicID, instanceID)
	if err != nil {
		w.Abort()
		if errors.Is(err, repo.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "Network interface %s not found", nicID)
		}
		return nil, serviceerr.MapRepoErrLeakSafe(err, "detach network interface failed")
	}
	if cerr := w.Commit(); cerr != nil {
		return nil, serviceerr.MapRepoErrLeakSafe(cerr, "detach network interface failed")
	}
	return rec, nil
}

// ListByInstance — batched read NIC-привязок для набора инстансов (compute-side
// зеркало Instance.Get/List; не N+1). Пустой набор → пустой результат.
func (s *Service) ListByInstance(ctx context.Context, instanceIDs []string) ([]*kachorepo.NetworkInterfaceAttachment, error) {
	rd, err := s.repo.Reader(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rd.Close() }()
	att, err := rd.NetworkInterfaces().ListByInstanceIDs(ctx, instanceIDs)
	if err != nil {
		return nil, serviceerr.MapRepoErrLeakSafe(err, "list network interfaces by instance failed")
	}
	return att, nil
}
