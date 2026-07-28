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
// AuthN(mTLS) и авторизация мутаций энфорсятся цепочкой интерсепторов :9091 (см.
// cmd/vpc/main.go internalUnary + check.PermissionMap): Attach/Detach — editor на
// vpc_network_interface:<nic_id>, повторная проверка в этом сервисе не нужна.
//
// ListByInstance — исключение: он авторизуется НА УРОВНЕ ДАННЫХ, здесь. Единого
// per-RPC вопроса для него не существует — инстансы называет вызывающий, а ответ
// касается интерфейсов, у каждого из которых свой владелец. Прежний вопрос («viewer
// на singleton cluster») отношения к делу не имел: это relation глобального
// справочника (регионы, зоны, типы дисков), и bootstrap кластера намеренно пишет
// `cluster:<root>#viewer@user:*`, чтобы справочник читал ЛЮБОЙ аутентифицированный
// субъект. Значит проверка пропускала всех и отдавала привязки любых названных
// инстансов — из чужих проектов и аккаунтов. Теперь страница читается из своей БД, а
// модель спрашивается про идентификаторы ЭТОЙ страницы (`viewer ∪ v_list` на
// `vpc_network_interface:<id>`, батчами) — та же дисциплина, что у публичных List.
package nicinternal

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
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

// ListFilter — port per-page фильтра видимости. Реализация —
// `authzfilter.AsPort(*authzfilter.FGAFilter)`. Возвращает подмножество переданных
// id, видимое subject'у (порядок сохраняется); err → fail-closed (страница не
// отдаётся). nil → unfiltered passthrough, поэтому в production его наличие
// гарантируется boot-guard'ом `config.ValidateListFilter` (ListByInstance помечен
// ScopeFiltered в check.PermissionMap и попадает в ScopeFilteredRPCs()).
type ListFilter interface {
	FilterVisibleIDs(ctx context.Context, subject, resourceType, action string, ids []string) ([]string, error)
}

// Service — координатор NIC↔Instance attach поверх CQRS-Repository.
type Service struct {
	repo kachorepo.Repository
	// zones — резолв зоны инстанса в регион для региональной полосы
	// placement-coherence (anycast-подсеть). nil → полоса fail-closed.
	zones ZoneRegistry
	// filter — per-object видимость для ListByInstance (see package doc).
	filter ListFilter
}

// NewService создаёт Service.
func NewService(r kachorepo.Repository) *Service {
	return &Service{repo: r}
}

// WithListFilter подключает per-page фильтр видимости для ListByInstance.
// Вызывается один раз из composition-root до приёма трафика.
func (s *Service) WithListFilter(f ListFilter) *Service {
	s.filter = f
	return s
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
//
// Инстансы называет ВЫЗЫВАЮЩИЙ, поэтому набор привязок сужается до тех, что модель
// разрешает subject'у видеть — per-object, на идентификаторах прочитанной страницы
// (см. package doc). Три случая subject'а различаются намеренно:
//   - реальный `user:`/`service_account:` → спрашиваем модель;
//   - `authzfilter.SystemSubject` → доверенный system-вызов, passthrough;
//   - "" (identity не извлечена) → это «не знаю, кто ты», а НЕ «доверенный»:
//     fail-closed, пустой результат.
//
// Пустой subject отсекается БЕЗУСЛОВНО — в отличие от публичных List, где тот же
// guard стоит под `s.filter != nil`. Разница не косметическая: у публичных List
// остаётся per-RPC Check, который сам отвергает вызывающего без принципала, а этот
// RPC помечен ScopeFiltered, то есть Check за него не задаётся вовсе. Привяжи мы
// fail-closed к наличию фильтра — конфигурация без фильтра отдавала бы вообще всё и
// вообще всем, что ровно та дыра, ради которой сюда пришли.
func (s *Service) ListByInstance(ctx context.Context, subject string, instanceIDs []string) ([]*kachorepo.NetworkInterfaceAttachment, error) {
	if subject == "" {
		return nil, nil
	}
	rd, err := s.repo.Reader(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rd.Close() }()
	att, err := rd.NetworkInterfaces().ListByInstanceIDs(ctx, instanceIDs)
	if err != nil {
		return nil, serviceerr.MapRepoErrLeakSafe(err, "list network interfaces by instance failed")
	}
	if s.filter == nil || subject == authzfilter.SystemSubject || len(att) == 0 {
		return att, nil
	}
	return s.filterVisible(ctx, subject, att)
}

// filterVisible оставляет только те привязки, чей NIC видим subject'у, сохраняя
// порядок. Ошибка резолва видимости — fail-closed: страницу не отдаём (недоступный
// ответ модели не есть ответ «да»).
func (s *Service) filterVisible(
	ctx context.Context,
	subject string,
	att []*kachorepo.NetworkInterfaceAttachment,
) ([]*kachorepo.NetworkInterfaceAttachment, error) {
	pageIDs := make([]string, 0, len(att))
	for _, a := range att {
		pageIDs = append(pageIDs, a.NICID)
	}
	visibleIDs, err := s.filter.FilterVisibleIDs(ctx, subject,
		authzfilter.ResourceTypeNetworkInterface, authzfilter.ActionNetworkInterfaceList, pageIDs)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = struct{}{}
	}
	out := make([]*kachorepo.NetworkInterfaceAttachment, 0, len(visibleIDs))
	for _, a := range att {
		if _, ok := visible[a.NICID]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}
