// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// CreateLoadBalancerUseCase — async Create с sync-precheck матрицы source×type×
// placement (fail-fast ДО Operation) и per-family VIP fan-out в worker'е.
//
// Файл сфокусирован на оркестрации саги allocate→persist→finalize→compensate.
// Смежные концерны вынесены в соседние файлы пакета:
//   - vip_source.go   — parse VipSource oneof → familyVIPSpec + матрица source×type.
//   - enum_mapping.go — proto enum ↔ domain (type/session-affinity).
//   - payloads.go     — outbox/FGA-intent/owner-tuple builders.
//   - peer_errors.go  — анти-oracle маппинг peer-ошибок в gRPC-status.
//   - zones.go        — валидация disabled_announce_zones + normalize.
type CreateLoadBalancerUseCase struct {
	// quota — совещательная полоса учёта числа ресурсов.
	//
	// nil означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции, и исчерпание приезжает отказом
	// операции. Различие наблюдаемо (429 синхронно против отказа в операции),
	// поэтому провязка обязательна на любом поднятом стенде; отсутствие
	// допустимо только там, где нет и соседа, у которого спрашивать величины.
	quota QuotaGuard

	repo          Repo
	opsRepo       operations.Repo
	projectClient ProjectClient
	regionClient  RegionClient
	zoneClient    ZoneClient
	subnetClient  SubnetClient
	zoneRegion    ZoneRegionClient      // авторитетный zone→region (geo)
	addressReader AddressClient         // public AddressService.Get — link-resolution
	addressClient InternalAddressClient // VIP alloc/link/release
	// registrar — sync-primary owner-tuple registrar (kaname RegisterResource)
	// вызывается BEST-EFFORT после durable commit LB. nil → только async
	// register-drainer (dev/no-iam). См. WithRegistrar.
	registrar Registrar
	// sgClient — NLB-1b MIGRATE peer-validate of security_group_ids (same-project
	// existence via vpc). nil → SG validation skipped (DB CHECK backstop). См.
	// WithSecurityGroupClient.
	sgClient SecurityGroupClient
	logger   *slog.Logger
}

// NewCreateLoadBalancerUseCase конструктор.
func NewCreateLoadBalancerUseCase(
	repo Repo, opsRepo operations.Repo,
	pc ProjectClient, rc RegionClient, zc ZoneClient, zrc ZoneRegionClient,
	snc SubnetClient, ar AddressClient, ac InternalAddressClient,
	logger *slog.Logger,
) *CreateLoadBalancerUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &CreateLoadBalancerUseCase{
		repo: repo, opsRepo: opsRepo,
		projectClient: pc, regionClient: rc, zoneClient: zc, zoneRegion: zrc,
		subnetClient: snc, addressReader: ar, addressClient: ac,
		logger: logger,
	}
}

// WithRegistrar подключает sync-primary owner-tuple registrar. После durable
// commit LB (+ его `fga_register_outbox`-intent'а) те же owner/containment-tuple'ы
// синхронно регистрируются в kaname — grant создателя доступен сразу, без
// гонки с async register-drainer'ом. BEST-EFFORT: сбой sync-Register логируется
// и глотается (durable intent + drainer — backstop), Operation.done НЕ гейтится
// на видимость (ban #9). nil registrar → sync-путь пропускается. Возвращает self
// для chaining в composition root.
func (u *CreateLoadBalancerUseCase) WithRegistrar(r Registrar) *CreateLoadBalancerUseCase {
	u.registrar = r
	return u
}

// WithSecurityGroupClient wires the vpc SecurityGroup peer-client used to
// peer-validate security_group_ids (same-project existence, fail-closed). nil →
// SG validation is skipped (DB CHECK INTERNAL-only remains the backstop). Returns
// self for chaining in the composition root.
func (u *CreateLoadBalancerUseCase) WithSecurityGroupClient(c SecurityGroupClient) *CreateLoadBalancerUseCase {
	u.sgClient = c
	return u
}

// syncRegister — BEST-EFFORT sync owner-tuple регистрация после durable commit.
// Ошибка ЛОГИРУЕТСЯ и ГЛОТАЕТСЯ: durable fga_register_outbox-intent +
// register-drainer — at-least-once backstop; Operation.done НЕ гейтится на
// видимость owner-tuple (ban #9 — иначе phantom-ресурс). nil registrar → no-op.
func (u *CreateLoadBalancerUseCase) syncRegister(ctx context.Context, intent domain.FGARegisterIntent, intentVersion time.Time) {
	if u.registrar == nil {
		return
	}
	if err := u.registrar.Register(ctx, intent, intentVersion); err != nil {
		u.logger.Warn("LoadBalancer.Create sync owner-tuple registration incomplete; register-drainer will reconcile",
			"err", err, "load_balancer_id", intent.ResourceID)
	}
}

// Execute — sync-precheck (тип/placement/матрица источника/drain/резолв
// адресов+подсетей+сети) fail-fast ДО Operation; затем ops insert + worker.
func (u *CreateLoadBalancerUseCase) Execute(
	ctx context.Context, req *lbv1.CreateNetworkLoadBalancerRequest,
) (*operations.Operation, error) {
	if req.GetProjectId() == "" {
		return nil, errInvalidArg("project_id", "required")
	}
	if req.GetRegionId() == "" {
		return nil, errInvalidArg("region_id", "required")
	}

	// NLB CONTRACT (F2 / NLB-1-08): `placement` is the SOLE authoritative mode input
	// and is required; it drives the derived (type, placement_type) columns persisted
	// for read. Writing legacy type/placement_type is an explicit reject.
	lbType, placement, placementMode, err := resolvePlacementAuthoritative(req)
	if err != nil {
		return nil, err
	}

	// VipSource oneof → упорядоченный (v4, v6) набор; ≥1 семейство; malformed id sync.
	specs, err := resolveVipSources(req.GetV4Source(), req.GetV6Source())
	if err != nil {
		return nil, err
	}

	// source × type матрица: subnet⟹INTERNAL, public⟹EXTERNAL.
	if err := validateSourceTypeMatrix(specs, lbType); err != nil {
		return nil, err
	}

	// Builder + Validate (multi-err).
	lb := domain.NewLoadBalancer(
		domain.ProjectID(req.GetProjectId()),
		domain.RegionID(req.GetRegionId()),
		domain.LbName(req.GetName()),
		domain.LbDescription(req.GetDescription()),
		domain.LabelsFromMap(req.GetLabels()),
		lbType,
	)
	lb.PlacementType = placement
	lb.DisabledAnnounceZones = normalizeZones(req.GetDisabledAnnounceZones())
	if req.GetDeletionProtection() {
		lb.DeletionProtection = true
	}
	sa, err := lbSessionAffinityFromPb(req.GetSessionAffinity())
	if err != nil {
		return nil, mapDomainErr(err)
	}
	lb.SessionAffinity = sa
	// NLB-1b EXPAND (additive): admin_state — default ENABLED via builder;
	// explicit input overrides. LIVE-mutable (Update), not yet status-authoritative.
	if as := adminStateFromPb(req.GetAdminState()); as != "" {
		lb.AdminState = as
	}
	// NLB-1b MIGRATE (F2): placement is authoritative — persisted as resolved above.
	lb.Placement = placementMode
	// NLB-1b MIGRATE (F3/NLB-1-16): cross_zone_enabled is REGIONAL-only — reject it
	// on ZONAL placement (a ZONAL LB serves a single zone). REGIONAL/anycast passes.
	if req.GetCrossZoneEnabled() && !domain.CrossZoneApplicable(placement) {
		return nil, status.Error(codes.InvalidArgument, crossZoneZonalMsg)
	}
	lb.CrossZoneEnabled = req.GetCrossZoneEnabled()
	// NLB-1b MIGRATE (F2/NLB-1-51): security_group_ids — vpc SecurityGroup refs
	// firewalling the VIP (peer-validated below in the sync phase). INTERNAL-only.
	lb.SecurityGroupIDs = normalizeSecurityGroupIDs(req.GetSecurityGroupIds())
	// ip_families — заявленные семейства VIP (проставляются ДО Insert-handle:
	// family-guard CHECK требует семейство в ip_families прежде чем persist-VIP
	// запишет непустой address).
	lb.IPFamilies = familiesFromSpecs(specs)
	if err := lb.Validate(); err != nil {
		return nil, mapDomainErr(err)
	}

	// disabled_announce_zones: REGIONAL-only + зоны ∈ регион + не все зоны (geo).
	if err := u.validateDisabledAnnounceZones(ctx, lb); err != nil {
		return nil, err
	}

	// Резолв источников: placement подсети/адреса == placement LB;
	// kind/family/ownership link'а; derived network + dualstack same-network.
	if err := u.resolveSources(ctx, lb, specs); err != nil {
		return nil, err
	}

	// Площадка ZONAL-балансировщика (#1473) — зона резолвнутой подсети VIP.
	// Берётся ЗДЕСЬ, а не спрашивается заново: `resolveSources` только что
	// прочитал её у владельца подсети и там же сверил согласие семейств, поэтому
	// второй вызов дал бы то же значение ценой лишнего обращения к соседу — и
	// разошёлся бы с проверкой, если бы подсеть между ними изменилась.
	//
	// Для REGIONAL и EXTERNAL остаётся пусто: у anycast зональной координаты нет
	// by construction, а зона внешнего VIP деривится платформой и наружу не
	// выходит (placement-leak). Исключительность держит DB-CHECK.
	lb.ZoneID = domain.ZoneID(zoneOfSpecs(lb.PlacementType, specs))

	// NLB-1b MIGRATE (F2/NLB-1-51/52): security_group_ids peer-validate (INTERNAL-only
	// + same-project existence via vpc; fail-closed). No region-coherence check.
	if err := validateSecurityGroups(ctx, u.sgClient, lb.Type, string(lb.ProjectID), lb.SecurityGroupIDs); err != nil {
		return nil, err
	}

	// Sync duplicate-name check (worker-Insert UNIQUE — атомарный backstop).
	//
	// Условия «имя непусто» здесь нет: оно было тождественно истинным. lb.Validate()
	// выше отвергает пустое имя (nlb требует имя на создании), и между ним и этой
	// строкой имя не присваивается, — то есть ветка «имя пусто» недостижима by
	// construction. Проверка, которая не может не выполниться, читается как
	// защита от случая, которого нет, и переживает своё основание.
	if err := u.assertNameUnique(ctx, string(lb.ProjectID), string(lb.Name)); err != nil {
		return nil, err
	}

	// Учёт числа ресурсов: ранний отказ ДО создания операции.
	//
	// Здесь же материализуются строки учёта, если проект их ещё не имеет, —
	// момент, когда владелец типа впервые узнаёт о проекте, и есть обращение к
	// нему. Отказ уходит арендатору синхронно тем же текстом и признаком, каким
	// его произвёл бы триггер: у обеих полос один производитель.
	if u.quota != nil {
		if err := u.quota.Admit(ctx, string(lb.ProjectID), "loadbalancer.networkLoadBalancers"); err != nil {
			return nil, mapDomainErr(err)
		}
	}

	// ---- Operation row ----
	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("Create NetworkLoadBalancer %s", lb.Name),
		&lbv1.CreateNetworkLoadBalancerMetadata{NetworkLoadBalancerId: string(lb.ID)},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}

	// Durable commit → op done сразу. Owner-tuple LB материализуется eventually-
	// consistent (writer-TX fga_register_outbox intent → register-drainer →
	// kaname RegisterResource → reconciler backstop); Operation.done означает
	// durability ресурса, не видимость owner-tuple в FGA.
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doCreate(workerCtx, lb, principal, specs)
	})

	return &op, nil
}

// validateDisabledAnnounceZones — REGIONAL-only + зоны ∈ регион + не все зоны.
func (u *CreateLoadBalancerUseCase) validateDisabledAnnounceZones(ctx context.Context, lb domain.LoadBalancer) error {
	return checkDisabledAnnounceZones(ctx, u.zoneClient, lb.PlacementType, string(lb.RegionID), lb.DisabledAnnounceZones)
}

// resolveSources — резолв каждого источника через peer-API: placement
// подсети/адреса == placement LB; link kind/family/ownership; derived network +
// dualstack same-network. Заполняет specs[i].networkID (INTERNAL).
func (u *CreateLoadBalancerUseCase) resolveSources(ctx context.Context, lb domain.LoadBalancer, specs []familyVIPSpec) error {
	for i := range specs {
		if err := u.resolveOneSource(ctx, lb, &specs[i]); err != nil {
			return err
		}
	}
	// dualstack same-network (INTERNAL): derived network семейств должен совпасть.
	var net string
	for _, fs := range specs {
		if fs.networkID == "" {
			continue
		}
		if net == "" {
			net = fs.networkID
			continue
		}
		if fs.networkID != net {
			return status.Error(codes.InvalidArgument,
				"dualstack load balancer families must resolve to the same network")
		}
	}
	// dualstack same-zone (INTERNAL ZONAL): derived zone семейств должен совпасть
	// (placement-coherence). Дополнительна к same-network: одна Network может
	// нести ZONAL-подсети разных зон. REGIONAL/anycast исключён by construction —
	// его подсети zone_id не несут (fs.zoneID пусто → пропуск). single-family —
	// сравнивать не с чем.
	if lb.PlacementType == domain.PlacementZonal {
		var zone string
		for _, fs := range specs {
			if fs.zoneID == "" {
				continue
			}
			if zone == "" {
				zone = fs.zoneID
				continue
			}
			if fs.zoneID != zone {
				return status.Error(codes.InvalidArgument,
					"dualstack load balancer families must resolve to the same zone")
			}
		}
	}
	return nil
}

// zoneOfSpecs — площадка ZONAL-балансировщика: зона резолвнутой подсети VIP.
//
// Семейства к этому моменту уже обязаны сойтись в ОДНОЙ зоне (это проверяет
// `resolveSources`), поэтому первая непустая и есть общая. Не-ZONAL размещение
// зональной координаты не имеет: у REGIONAL её нет by construction, у EXTERNAL
// она скрыта намеренно, — и оба случая закрываются здесь, а не у вызывающего,
// чтобы «когда зона пуста» решалось в одном месте.
func zoneOfSpecs(placement domain.PlacementType, specs []familyVIPSpec) string {
	if placement != domain.PlacementZonal {
		return ""
	}
	for _, fs := range specs {
		if fs.zoneID != "" {
			return fs.zoneID
		}
	}
	return ""
}

// resolveOneSource — резолв одного семейства.
func (u *CreateLoadBalancerUseCase) resolveOneSource(ctx context.Context, lb domain.LoadBalancer, fs *familyVIPSpec) error {
	switch fs.kind {
	case srcSubnetAuto:
		// Несконфигурированный vpc — неверная конфигурация стенда, НЕ режим
		// работы: placement/region-когерентность подсети проверить нечем, а
		// мутация обязана быть fail-closed (data-integrity.md
		// §Placement-coherence + security.md). Прежний `return nil` молча
		// снимал инвариант. Boot-guard (config.Validate, production) ловит эту
		// же ошибку на старте.
		if u.subnetClient == nil {
			return status.Error(codes.Unavailable, "subnet lookup unavailable")
		}
		sn, err := u.subnetClient.Get(ctx, fs.subnetID)
		if err != nil {
			return subnetPeerErr(err, fs.subnetID)
		}
		if !subnetPlacementMatches(sn.PlacementType, lb.PlacementType) {
			return status.Error(codes.InvalidArgument,
				"subnet placement does not match load balancer placement")
		}
		// region-coherence: caller-supplied subnet_id → descriptive текст (форма
		// запроса, не oracle). subnet.RegionID — denormalised mirror (REGIONAL →
		// region_id; ZONAL → zone→region резолв в adapter'е).
		if err := subnetRegionCoherent(sn, lb.RegionID); err != nil {
			return err
		}
		fs.networkID = sn.NetworkID
		fs.zoneID = sn.ZoneID
		return nil
	case srcPublicAuto:
		return nil // EXTERNAL public — ни сети, ни зоны: anycast-VIP зоне-независим
	case srcAddressLink:
		return u.resolveLinkedAddress(ctx, lb, fs)
	}
	return nil
}

// resolveLinkedAddress — sync-precheck link'а: kind/family/ownership/placement
// через public AddressService.Get под tenant-identity. Анти-oracle: любой
// mismatch/no-access → generic InvalidArgument "Illegal argument addressId".
func (u *CreateLoadBalancerUseCase) resolveLinkedAddress(ctx context.Context, lb domain.LoadBalancer, fs *familyVIPSpec) error {
	// Несконфигурированный vpc → ownership/family/kind/placement связанного
	// Address проверить нечем. Пропуск означал бы приём ЧУЖОГО address_id
	// (cross-project VIP-hijack), поэтому fail-closed (см. resolveOneSource).
	if u.addressReader == nil {
		return status.Error(codes.Unavailable, "address lookup unavailable")
	}
	addr, err := u.addressReader.Get(ctx, fs.addressID)
	if err != nil {
		return linkedAddressErr(err)
	}
	// ownership + family + kind↔type.
	internalWanted := lb.Type == domain.LBTypeInternal
	if addr.ProjectID != string(lb.ProjectID) ||
		addr.Family != string(fs.family) ||
		addr.External == internalWanted {
		return status.Error(codes.InvalidArgument, "Illegal argument addressId")
	}
	// EXTERNAL: у внешнего адреса нет подсети — его placement несёт он сам
	// (`external_ipv*.zone_id`). LB типа EXTERNAL всегда REGIONAL, поэтому
	// проверяется региональная когерентность (зона адреса ∈ регион LB), а
	// anycast-адрес (зоны нет) из зональной проверки исключён by construction.
	if !internalWanted {
		return u.externalAddressRegionCoherent(ctx, lb, addr)
	}
	// INTERNAL: placement подсети адреса == placement LB (derived network).
	// Несконфигурированный vpc → fail-closed (тот же инвариант, что выше).
	if u.subnetClient == nil {
		return status.Error(codes.Unavailable, "subnet lookup unavailable")
	}
	sn, err := u.subnetClient.Get(ctx, addr.SubnetID)
	if err != nil {
		// подсеть адреса не резолвится — не подтверждаем детали (generic),
		// vpc недоступен → Unavailable (fail-closed).
		if errors.Is(err, domain.ErrUnavailable) {
			return status.Error(codes.Unavailable, "subnet lookup unavailable")
		}
		return status.Error(codes.InvalidArgument, "Illegal argument addressId")
	}
	if !subnetPlacementMatches(sn.PlacementType, lb.PlacementType) {
		return status.Error(codes.InvalidArgument, "Illegal argument addressId")
	}
	// region-coherence: linked address_id → generic текст (анти-oracle, не
	// подтверждаем placement чужого адреса), в отличие от descriptive-текста
	// caller-supplied subnet_id (resolveOneSource).
	if err := subnetRegionCoherentOpaque(sn, lb.RegionID); err != nil {
		return err
	}
	fs.networkID = sn.NetworkID
	fs.zoneID = sn.ZoneID
	return nil
}

// externalAddressRegionCoherent — region-coherence зоно-привязанного EXTERNAL
// адреса: его зона обязана принадлежать региону LB. Регион зоны берётся ТОЛЬКО у
// владельца Geography (geo.v1.ZoneService.Get) — из имени зоны он не выводится.
//
//   - `zone_id` пуст → anycast: адрес зоне-независим by construction, зональной
//     координаты нет, сравнивать не с чем (data-integrity.md §Placement-coherence,
//     «Anycast/regional исключение») → coherent;
//   - резолв невозможен (нет резолвера / geo недоступен / пустой ответ) →
//     UNAVAILABLE (fail-closed на мутации);
//   - чужой регион → generic "Illegal argument addressId" (анти-oracle: не
//     раскрываем, в какой зоне/регионе лежит адрес).
func (u *CreateLoadBalancerUseCase) externalAddressRegionCoherent(
	ctx context.Context, lb domain.LoadBalancer, addr *vpcclient.Address,
) error {
	if addr.ZoneID == "" {
		return nil
	}
	if u.zoneRegion == nil {
		return status.Error(codes.Unavailable, "address region lookup unavailable")
	}
	region, err := u.zoneRegion.RegionOfZone(ctx, addr.ZoneID)
	if err != nil || region == "" {
		if err != nil {
			u.logger.Warn("LoadBalancer.Create: zone→region resolve failed for linked external address",
				"err", err, "address_id", addr.ID)
		}
		return status.Error(codes.Unavailable, "address region lookup unavailable")
	}
	if region != string(lb.RegionID) {
		return status.Error(codes.InvalidArgument, "Illegal argument addressId")
	}
	return nil
}

// doCreate — async worker: durable-handle сага с per-family VIP fan-out.
func (u *CreateLoadBalancerUseCase) doCreate(
	ctx context.Context, lb domain.LoadBalancer, principal operations.Principal, specs []familyVIPSpec,
) (*anypb.Any, error) {
	// Worker-ctx детачнут от request'а — восстанавливаем principal из Operation,
	// чтобы downstream-вызовы (vpc AddressService.Create / SetReference, geo Zone/
	// Region) несли identity тенанта (auth.PropagateOutgoing). Без этого vpc authz
	// отвергает Create как authz_no_principal.
	ctx = operations.WithPrincipal(ctx, principal)
	if u.projectClient != nil {
		if _, err := u.projectClient.Get(ctx, string(lb.ProjectID)); err != nil {
			return nil, peerErrToStatus(err, "project", string(lb.ProjectID))
		}
	}
	if u.regionClient != nil {
		if _, err := u.regionClient.Get(ctx, string(lb.RegionID)); err != nil {
			return nil, peerErrToStatus(err, "region", string(lb.RegionID))
		}
	}

	lb.Status = domain.LBStatusCreating
	if err := u.insertHandle(ctx, &lb); err != nil {
		return nil, err
	}

	finalized := false
	allocated := map[domain.IPVersion]vipAllocResult{}
	defer func() {
		if finalized {
			return
		}
		u.compensateCreate(ctx, string(lb.ProjectID), string(lb.ID), allocated)
	}()

	for _, fs := range specs {
		alloc, err := u.acquireFamilyVIP(ctx, lb, fs)
		if err != nil {
			return nil, err
		}
		allocated[fs.family] = alloc
		if _, err := u.persistVIP(ctx, string(lb.ID), fs.family, alloc.address, alloc.addressID, alloc.origin); err != nil {
			return nil, mapDomainErr(err)
		}
	}

	created, intent, intentVersion, err := u.finalizeCreate(ctx, lb, principal)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	finalized = true

	// Sync-primary owner-tuple registration (после durable commit LB + его
	// fga_register_outbox-intent'а): grant создателя доступен сразу, закрывая
	// async-only окно. BEST-EFFORT — сбой логируется и глотается (durable intent
	// + register-drainer — backstop); Operation.done НЕ гейтится на видимость
	// owner-tuple (ban #9).
	u.syncRegister(ctx, intent, intentVersion)

	pb, err := lbRecordToProto(created)
	if err != nil {
		return nil, err
	}
	out, err := anypb.New(pb)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	return out, nil
}

// vipAllocResult — outcome acquire-ветки одного семейства.
type vipAllocResult struct {
	addressID string
	address   string
	origin    domain.VipOrigin // auto → two-step release; linked → ClearReference
}

// acquireFamilyVIP — внешний side-effect одного семейства: auto-аллокация
// (subnet internal / platform public) либо link существующего Address.
//
// Ответ клиенту намеренно ЛОССИ (анти-oracle): ёмкость → generic
// FAILED_PRECONDITION; link-конфликт → generic `Illegal argument addressId`; vpc
// недоступен → Unavailable; нерезолвящаяся caller-supplied подсеть → тот же
// текст, что уже даёт sync-precheck (см. allocAcquireErr). Поэтому каждая
// alloc-неудача ЛОГИРУЕТСЯ с исходной причиной — иначе провал аллокации
// неатрибутируем в проде (CWE-778 silent swallow: реальный инцидент — vpc
// отвечал `Subnet <id> not found` под cross-service read-your-writes, а в логе
// и в Operation.error было только «could not allocate»).
func (u *CreateLoadBalancerUseCase) acquireFamilyVIP(
	ctx context.Context, lb domain.LoadBalancer, fs familyVIPSpec,
) (vipAllocResult, error) {
	if u.addressClient == nil {
		return vipAllocResult{}, status.Error(codes.Unavailable, "vpc internal address client not configured")
	}
	owner := lbAddressOwner(string(lb.ID), string(lb.Name))
	switch fs.kind {
	case srcAddressLink:
		resp, err := u.addressClient.AttachExisting(ctx, vpcclient.AttachExistingRequest{
			AddressID: fs.addressID,
			Owner:     owner,
			Owned:     false,
		})
		if err != nil {
			// Link-полоса теряет причину так же, как две соседние, и её ответ
			// ЕДИНСТВЕННЫЙ, который не сужает ничего: `Illegal argument
			// addressId` производят и промах, и отказ в правах, и проигранный
			// CAS. Без этой записи отказ операции неатрибутируем.
			u.logVIPAcquireFailure(lb, fs, err)
			return vipAllocResult{}, linkAcquireErr(err)
		}
		return vipAllocResult{addressID: resp.AddressID, address: resp.Value, origin: domain.VipOriginLinked}, nil
	case srcPublicAuto:
		// EXTERNAL — единственное external-placement'о `EXTERNAL_REGIONAL`, т.е.
		// ВСЕГДА REGIONAL/anycast, а REGIONAL зоне-независим by construction
		// (data-integrity.md §Placement-coherence). Поэтому zone НЕ задаётся:
		// vpc резолвит зоне-независимый (anycast) AddressPool. Указание зоны
		// пинило бы «anycast»-VIP к префиксу и failure-domain'у одной зоны.
		req := vpcclient.AllocateExternalIPRequest{
			ProjectID: string(lb.ProjectID),
			Name:      domain.LBAnycastAddressName(lb.ID, fs.family),
			Owner:     owner,
		}
		var (
			resp *vpcclient.AllocateResponse
			err2 error
		)
		if fs.family == domain.IPVersionV6 {
			resp, err2 = u.addressClient.AllocateExternalIPv6(ctx, req)
		} else {
			resp, err2 = u.addressClient.AllocateExternalIP(ctx, req)
		}
		if err2 != nil {
			// public-полоса: ссылку выбирает платформа, subnetRef пуст → ответ
			// остаётся непрозрачным (underlay-зона/пул — инфра-данные).
			u.logVIPAcquireFailure(lb, fs, err2)
			return vipAllocResult{}, allocAcquireErr(err2, "")
		}
		return vipAllocResult{addressID: resp.AddressID, address: resp.Value, origin: domain.VipOriginAuto}, nil
	default: // srcSubnetAuto
		req := vpcclient.AllocateInternalIPRequest{
			ProjectID: string(lb.ProjectID),
			Name:      domain.LBAnycastAddressName(lb.ID, fs.family),
			SubnetID:  fs.subnetID,
			Owner:     owner,
		}
		var (
			resp *vpcclient.AllocateResponse
			err  error
		)
		if fs.family == domain.IPVersionV6 {
			resp, err = u.addressClient.AllocateInternalIPv6(ctx, req)
		} else {
			resp, err = u.addressClient.AllocateInternalIP(ctx, req)
		}
		if err != nil {
			u.logVIPAcquireFailure(lb, fs, err)
			return vipAllocResult{}, allocAcquireErr(err, fs.subnetID)
		}
		return vipAllocResult{addressID: resp.AddressID, address: resp.Value, origin: domain.VipOriginAuto}, nil
	}
}

// logVIPAcquireFailure — фиксирует причину, которую ответ клиенту намеренно
// теряет (анти-oracle). Без этого лога отказ VIP-аллокации неатрибутируем:
// «could not allocate load balancer address» одинаково покрывает исчерпанный
// пул, отсутствующий AddressPool и нерезолвящуюся подсеть.
func (u *CreateLoadBalancerUseCase) logVIPAcquireFailure(
	lb domain.LoadBalancer, fs familyVIPSpec, err error,
) {
	logger := u.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("load_balancer_vip_acquire_failed",
		"load_balancer_id", string(lb.ID),
		"project_id", string(lb.ProjectID),
		"family", string(fs.family),
		"source_kind", fs.kind.String(),
		"subnet_id", fs.subnetID,
		// Ссылка, которую назвал вызывающий на link-полосе. Без неё запись
		// говорит, ЧТО отказано, но не НА ЧЁМ: у link-полосы subnet_id пуст
		// by construction, и адрес — единственная её координата.
		"address_id", fs.addressID,
		"err", err.Error(),
	)
}

// insertHandle — TX-1: INSERT durable-handle строки LB (status='CREATING').
// UNIQUE (project_id,name) 23505 → каноничный ALREADY_EXISTS-текст (name-dup).
func (u *CreateLoadBalancerUseCase) insertHandle(ctx context.Context, lb *domain.LoadBalancer) error {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return mapDomainErr(err)
	}
	committed := false
	defer func() {
		if !committed {
			w.Abort()
		}
	}()
	if _, err := w.LoadBalancers().Insert(ctx, lb); err != nil {
		if errors.Is(err, kachorepo.ErrAlreadyExists) || errors.Is(err, domain.ErrAlreadyExists) {
			return status.Errorf(codes.AlreadyExists,
				"NetworkLoadBalancer with name %s already exists in project", lb.Name)
		}
		return mapDomainErr(err)
	}
	if err := w.Commit(); err != nil {
		return mapDomainErr(err)
	}
	committed = true
	return nil
}

// persistVIP — отдельный commit CAS-attach VIP одного семейства в CREATING-handle.
func (u *CreateLoadBalancerUseCase) persistVIP(
	ctx context.Context, id string, family domain.IPVersion, address, addressID string, origin domain.VipOrigin,
) (*kachorepo.LoadBalancerRecord, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			w.Abort()
		}
	}()
	rec, err := w.LoadBalancers().AttachVIP(ctx, id, family, address, addressID, origin)
	if err != nil {
		return nil, err
	}
	if err := w.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return rec, nil
}

// finalizeCreate — финальный commit: CAS CREATING→INACTIVE + outbox CREATED +
// FGA-register-intent (project-hierarchy + creator) в одной writer-TX.
func (u *CreateLoadBalancerUseCase) finalizeCreate(
	ctx context.Context, lb domain.LoadBalancer, principal operations.Principal,
) (*kachorepo.LoadBalancerRecord, domain.FGARegisterIntent, time.Time, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, domain.FGARegisterIntent{}, time.Time{}, err
	}
	committed := false
	defer func() {
		if !committed {
			w.Abort()
		}
	}()
	created, err := w.LoadBalancers().SetStatusCAS(ctx, string(lb.ID),
		domain.LBStatusCreating, domain.LBStatusInactive)
	if err != nil {
		return nil, domain.FGARegisterIntent{}, time.Time{}, err
	}
	if err := w.Outbox().Emit(ctx,
		kachorepo.OutboxResourceLoadBalancer, string(created.ID), string(created.ProjectID),
		kachorepo.OutboxActionCreated, kachorepo.LoadBalancerStatePayload(created),
	); err != nil {
		return nil, domain.FGARegisterIntent{}, time.Time{}, err
	}
	// Намерение строится ОДИН раз и доставляется обеими доставками — этой и
	// дренажом той же строки; штамп writer-транзакции возвращается вместе с ним,
	// потому что синхронная доставка обязана нести ИМЕННО ЕГО.
	intent := lbRegisterIntent(created)
	intentVersion, err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister, intent)
	if err != nil {
		return nil, domain.FGARegisterIntent{}, time.Time{}, err
	}
	if err := w.Commit(); err != nil {
		return nil, domain.FGARegisterIntent{}, time.Time{}, err
	}
	committed = true
	return created, intent, intentVersion, nil
}

// compensateCreate — best-effort откат до finalize: снимает аренду каждого
// аллоцированного VIP одним глаголом владельца и удаляет handle. Ошибки
// логируются; краш раньше — free_ip_runner.
func (u *CreateLoadBalancerUseCase) compensateCreate(ctx context.Context, projectID, lbID string, allocated map[domain.IPVersion]vipAllocResult) {
	logger := u.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("load_balancer_id", lbID)

	releaseFailed := false
	if u.addressClient != nil {
		for family, alloc := range allocated {
			// Аллокация без ключа аренды сюда НЕ ДОХОДИТ: обе полосы
			// `acquireFamilyVIP` возвращают непустой ключ — авто-полоса отвергает
			// ответ vpc без него (`allocFromCreate`), полоса привязки требует ключ
			// на входе (`AttachExisting`). Ветка оставлена РОВНО как отказ, а не
			// как пропуск: пропустив, компенсация оставила бы `releaseFailed=false`,
			// снесла handle — и потеряла бы аренду навсегда, то есть воспроизвела
			// бы #467 с другого конца.
			if alloc.addressID == "" {
				releaseFailed = true
				logger.Warn("LoadBalancer.Create compensation cannot release a lease without its id",
					"family", string(family))
				continue
			}
			if rerr := u.releaseAddress(ctx, projectID, lbID, alloc.addressID); rerr != nil {
				releaseFailed = true
				logger.Warn("LoadBalancer.Create compensation release failed",
					"err", rerr, "address_id", alloc.addressID, "family", string(family))
			}
		}
	}
	// HANDLE СНОСИТСЯ ТОЛЬКО ПОСЛЕ ПОДТВЕРЖДЁННОГО ОСВОБОЖДЕНИЯ.
	//
	// Строка балансировщика — единственная координата, по которой реконсайлер
	// вообще способен найти аренду: он выбирает `load_balancers` в состояниях
	// DELETING/CREATING и идёт от них к `address_id`. Обратной развёртки со
	// стороны vpc («освободить всё, чем владеет этот владелец») в системе нет.
	// Значит снос handle при НЕосвобождённом адресе превращает временный отказ
	// соседа в ВЕЧНУЮ утечку: адрес остаётся жить, его подсеть больше никогда не
	// удаляется, а найти его некому — прежняя редакция при этом писала в журнал
	// «free_ip_runner will reconcile», то есть обещала ровно то, что сама и
	// делала невозможным.
	//
	// Оставленный handle не «мусор»: он в CREATING, его подберёт free_ip_runner
	// по возрасту и доведёт освобождение под системной личностью. Это дороже на
	// один цикл реконсиляции и дешевле на одну безвозвратно потерянную аренду.
	if releaseFailed {
		logger.Warn("LoadBalancer.Create compensation kept the durable handle: "+
			"a released-but-unconfirmed address is only reachable through it",
			"load_balancer_id", lbID)
		return
	}
	if err := u.deleteHandle(ctx, lbID); err != nil {
		logger.Warn("LoadBalancer.Create compensation delete handle failed; free_ip_runner will reconcile", "err", err)
	}
}

// releaseAddress — снятие аренды одного адреса на компенсации сорвавшегося
// создания. Один глагол владельца, исход читается из поля.
//
// Ветку «удалить адрес» либо «оставить адрес арендатора» выбирает ВЛАДЕЛЕЦ по
// своей колонке — потребитель её больше не выводит. Прежде это решение
// принимали три места по собственной копии признака, и спрашивали они
// по-разному.
func (u *CreateLoadBalancerUseCase) releaseAddress(ctx context.Context, projectID, lbID, addressID string) error {
	if u.addressClient == nil {
		return status.Error(codes.Unavailable, "vpc internal address client not configured")
	}
	_, err := u.addressClient.ReleaseLease(ctx, vpcclient.ReleaseLeaseRequest{
		ProjectID: projectID,
		AddressID: addressID,
		Owner:     vpcclient.AddressOwner{Kind: lbAddressOwnerKind, ID: lbID},
	})
	return err
}

// deleteHandle — best-effort DELETE durable-handle строки LB в собственной TX.
func (u *CreateLoadBalancerUseCase) deleteHandle(ctx context.Context, id string) error {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			w.Abort()
		}
	}()
	if err := w.LoadBalancers().Delete(ctx, id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := w.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// assertNameUnique — sync precheck дубликата (project_id, name).
func (u *CreateLoadBalancerUseCase) assertNameUnique(ctx context.Context, projectID, name string) error {
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return mapDomainErr(err)
	}
	defer func() { _ = rd.Close() }()

	existing, _, err := rd.LoadBalancers().List(ctx,
		kachorepo.LoadBalancerFilter{ProjectID: projectID, Name: kachorepo.ExactName(name)},
		kachorepo.Pagination{},
	)
	if err != nil {
		return mapDomainErr(err)
	}
	if len(existing) > 0 {
		return status.Errorf(codes.AlreadyExists,
			"NetworkLoadBalancer with name %s already exists in project", name)
	}
	return nil
}

// WithQuotaGuard подключает совещательную полосу учёта.
//
// Отдельным глаголом, а не аргументом конструктора: полоса появилась позже
// вызывающих, и обязательный аргумент заставил бы править каждую сборку — в том
// числе те, где соседа с величинами нет вовсе.
func (u *CreateLoadBalancerUseCase) WithQuotaGuard(g QuotaGuard) *CreateLoadBalancerUseCase {
	u.quota = g
	return u
}
