// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package addresspool

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// CreatePoolReq — параметры создания пула.
//
// CIDR-блоки разделены по family на v4_cidr_blocks + v6_cidr_blocks. Хотя бы
// одно поле должно быть непустым. Family каждого блока обязательна: IPv6-префикс
// в V4CIDRBlocks → InvalidArgument, и симметрично.
type CreatePoolReq struct {
	Name             string
	Description      string
	Labels           map[string]string
	V4CIDRBlocks     []string
	V6CIDRBlocks     []string
	Kind             domain.AddressPoolKind
	ZoneID           string // "" = глобальный пул (default fallback)
	IsDefault        bool
	SelectorLabels   map[string]string
	SelectorPriority int32
}

// CreateAddressPoolUseCase — admin-only Create. Sync-поток (нет Operation —
// AddressPool не выставляется на external endpoint, async-worker'а нет):
// валидация → zone-check (опц.) → Writer-TX: Insert + PopulateFreelistForPool +
// InitIPv6PoolCursor (если v6 непустой) + outbox-emit `AddressPool.CREATED` →
// Commit.
//
// Атомарность DML + freelist materialization + outbox-emit гарантируется одной
// pgx.Tx writer'а `kacho.Repository.Writer(ctx)`: либо все, либо ничего (иначе
// crash между шагами оставил бы orphan-state).
type CreateAddressPoolUseCase struct {
	repo    Repo
	zoneReg ZoneRegistry // nil → zone-check skip
}

// NewCreateAddressPoolUseCase собирает use-case.
func NewCreateAddressPoolUseCase(r Repo, zoneReg ZoneRegistry) *CreateAddressPoolUseCase {
	return &CreateAddressPoolUseCase{repo: r, zoneReg: zoneReg}
}

// Execute создает AddressPool.
func (u *CreateAddressPoolUseCase) Execute(ctx context.Context, req CreatePoolReq) (*kachorepo.AddressPoolRecord, error) {
	p, err := u.Validate(ctx, req)
	if err != nil {
		return nil, err
	}
	return u.Persist(ctx, p)
}

// Validate — вся проверка входа, исполнимая ДО обращения к своей БД: форма
// запроса плюс существование чужой зоны у её владельца.
//
// Вынесена из тела Execute отдельным методом, чтобы у ПУБЛИЧНОГО пути
// (`AsyncMutations`, ответ — `Operation`) и у внутреннего была ОДНА проверка, а
// не две копии. Копии здесь разошлись бы молча и в худшую сторону: асинхронный
// путь обязан отвергать негодный вход синхронно, до создания операции, иначе
// вызывающий получает `200` с операцией, несущей отказ, — то есть «принято» на
// том, что не принято.
//
// Порядок половин сохранён дословно: форма — до peer-вызова, чтобы явный мусор
// не оплачивался обращением к соседу.
//
// Возвращает ГОТОВЫЙ доменный объект, а не только приговор. Иначе вызывающему
// пришлось бы собрать его второй раз — и второй сборке достался бы ДРУГОЙ
// идентификатор, потому что он чеканится здесь. Публичный путь кладёт этот же
// идентификатор в метаданные операции ещё до её применения.
func (u *CreateAddressPoolUseCase) Validate(ctx context.Context, req CreatePoolReq) (*domain.AddressPool, error) {
	if req.Kind == domain.AddressPoolKindUnspecified {
		return nil, status.Error(codes.InvalidArgument, "kind must be specified")
	}
	// Хотя бы одно из v4_cidr_blocks / v6_cidr_blocks непусто.
	if len(req.V4CIDRBlocks) == 0 && len(req.V6CIDRBlocks) == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"v4_cidr_blocks and v6_cidr_blocks must not be both empty")
	}
	// Family-strict валидация каждого слота.
	if err := validateAddressPoolCIDRs("v4_cidr_blocks", req.V4CIDRBlocks, familyV4Strict); err != nil {
		return nil, err
	}
	if err := validateAddressPoolCIDRs("v6_cidr_blocks", req.V6CIDRBlocks, familyV6Strict); err != nil {
		return nil, err
	}
	// Sync within-request precheck — блоки В САМОМ запросе должны быть попарно
	// непересекающимися (fast-fail до writer-TX). DB EXCLUDE (миграция 0004) —
	// backstop для cross-pool/concurrent.
	if err := checkPoolCIDRsDisjoint(req.V4CIDRBlocks, req.V6CIDRBlocks); err != nil {
		return nil, err
	}
	// zone_id existence — Geography (Region/Zone) — leaf-домен kacho-geo;
	// валидируется вызовом geo.v1.ZoneService.Get через ZoneRegistry-порт.
	// Промах — полоса peer-validate (общая на все geo-точки сервиса).
	if req.ZoneID != "" && u.zoneReg != nil {
		if _, err := u.zoneReg.Get(ctx, req.ZoneID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, serviceerr.UnknownZone(req.ZoneID)
			}
			return nil, serviceerr.MapRepoErr(err)
		}
	}
	p := &domain.AddressPool{
		ID:               ids.NewID(ids.PrefixAddressPool),
		Name:             domain.RcNameVPC(req.Name),
		Description:      domain.RcDescription(req.Description),
		Labels:           domain.LabelsFromMap(req.Labels),
		V4CIDRBlocks:     req.V4CIDRBlocks,
		V6CIDRBlocks:     req.V6CIDRBlocks,
		Kind:             req.Kind,
		ZoneID:           req.ZoneID,
		IsDefault:        req.IsDefault,
		SelectorLabels:   domain.LabelsFromMap(req.SelectorLabels),
		SelectorPriority: req.SelectorPriority,
	}
	// Self-validating domain: name/description/labels/selector_* проверяются ДО
	// Insert (DB CHECK — backstop). Невалидный пул отбивается здесь, до writer-TX.
	if err := serviceerr.FromValidation(p.Validate()); err != nil {
		return nil, err
	}
	return p, nil
}

// Persist — вторая половина создания: одна writer-TX (Insert + нормализация
// CIDR-блоков + материализация freelist + outbox) и Commit.
//
// Отделена от Validate ровно по границе «что можно решить, не трогая свою БД».
// Публичный путь исполняет её ВНУТРИ операции, внутренний — сразу за проверкой;
// тело одно, поэтому два пути записи в одну таблицу не могут разойтись ни
// валидацией, ни умолчаниями, ни набором заполняемых полей.
func (u *CreateAddressPoolUseCase) Persist(ctx context.Context, p *domain.AddressPool) (*kachorepo.AddressPoolRecord, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, err
	}
	defer w.Abort()

	created, err := w.AddressPools().Insert(ctx, p)
	if err != nil {
		return nil, err
	}
	// Нормализуем CIDR-блоки в address_pool_cidrs (EXCLUDE gist) до
	// freelist-materialization — пересечение с другим пулом / внутри пула →
	// FailedPrecondition "address pool CIDRs can not overlap" (DB-level backstop,
	// within-service инвариант на DB-уровне). В той же writer-TX → atomic rollback
	// при overlap.
	if err := w.AddressPools().InsertCidrBlocks(ctx, created.ID, created.Kind,
		created.V4CIDRBlocks, created.V6CIDRBlocks); err != nil {
		return nil, mapCIDROverlap(err)
	}
	// Материализуем per-IP freelist только для V4CIDRBlocks (миграция 0014).
	if err := w.AddressPools().PopulateFreelistForPool(ctx, created.ID); err != nil {
		return nil, fmt.Errorf("%w: populate freelist: %v", serviceerr.ErrInternal, err)
	}
	// Pool с IPv6 CIDR использует sparse counter-based allocator.
	if len(created.V6CIDRBlocks) > 0 {
		if err := w.Addresses().InitIPv6PoolCursor(ctx, created.ID); err != nil {
			return nil, fmt.Errorf("%w: init ipv6 cursor: %v", serviceerr.ErrInternal, err)
		}
	}
	if err := w.Outbox().Emit(ctx, "AddressPool", created.ID, "CREATED",
		helpers.AddressPoolDomainPayload(&created.AddressPool)); err != nil {
		return nil, fmt.Errorf("%w: outbox emit: %v", serviceerr.ErrInternal, err)
	}
	if err := w.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}
