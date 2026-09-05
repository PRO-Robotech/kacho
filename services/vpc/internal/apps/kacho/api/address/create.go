// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/addresspool"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// ExternalAddrSpec — спецификация внешнего адреса.
type ExternalAddrSpec struct {
	Address string
	ZoneID  string
}

// InternalAddrSpec — спецификация внутреннего адреса (v4/v6 одинаковый shape).
type InternalAddrSpec struct {
	Address  string
	SubnetID string
}

// AddressOwner — владелец, которому адрес привязывается ТОЙ ЖЕ транзакцией, что
// и создаётся (внутренний путь `InternalAddressService.CreateOwnedAddress`).
//
// Публичный путь тенанта владельца не несёт и нести не может: тенант заказывает
// адрес сам по себе, а не под чужой ресурс. Разделение существует затем, чтобы
// у платформенного заказчика (сага балансировщика) не было ВТОРОГО решения о
// доступе — на объекте, которого в начале операции ещё не существовало.
type AddressOwner struct {
	ReferrerType string
	ReferrerID   string
	ReferrerName string
	// Owned — владелец распоряжается жизненным циклом адреса (release =
	// ClearReference + Delete), а не просто ссылается на него.
	Owned bool
}

// CreateInput — параметры для CreateAddressUseCase.Execute.
//
// Композирует поля Address-запроса + четыре family-specific spec'а (External
// v4/v6, Internal v4/v6) — это не тривиальная обертка вокруг domain.Address.
// Spec'и нельзя смержить в domain.Address: там oneof выражен через указатели, и
// валидация семейств через nil-проверки плоского CreateInput получается чище.
type CreateInput struct {
	ProjectID          string
	Name               string
	Description        string
	Labels             map[string]string
	DeletionProtection bool
	// Для external IPv4 (если ExternalSpec != nil):
	ExternalSpec *ExternalAddrSpec
	// Для internal IPv4 (если InternalSpec != nil):
	InternalSpec *InternalAddrSpec
	// Для internal IPv6 (если InternalIpv6Spec != nil):
	InternalIpv6Spec *InternalAddrSpec
	// Для external IPv6 (если ExternalIpv6Spec != nil):
	ExternalIpv6Spec *ExternalAddrSpec
	// Owner != nil — адрес рождается уже привязанным к владельцу; привязка
	// выполняется в той же writer-TX, что вставка и IPAM-аллокация. Заполняется
	// ТОЛЬКО внутренним путём (`CreateOwnedAddress`); публичный Create его
	// не принимает.
	Owner *AddressOwner
}

// CreateAddressUseCase инициирует создание Address (multi-family). Sync-
// проверки (project exists, subnet exists, name unique, explicit-IP-in-CIDR)
// выполняются ДО создания Operation — клиент получает fast-fail gRPC-status,
// а не «200 + операция, упавшая через секунду». Async-часть (`doCreate`)
// собирает domain.Address по family, Insert, затем inline IPAM allocation
// (AllocateExternalIP/AllocateInternalIP/AllocateInternalIPv6/AllocateExternalIPv6).
//
// pools может быть nil в test-setup'ах — тогда v4/v6-external allocate
// недоступен, IP в результате остается пустым (Allocate*IP возвращает
// Unavailable). v6-internal allocate не зависит от pools — он работает
// random-pick'ом внутри subnet.v6_cidr_blocks.
//
// worker открывает ОДНУ writer-TX и делает в ней Insert(Address) → Allocate*IP
// (через writer.Addresses().Set*Spec / AllocateIPFromFreelist /
// AllocateExternalIPv6) → Outbox.Emit Address.CREATED. Либо весь композит виден
// (Commit), либо ничего (Abort/crash) — окно orphan-address-without-allocated-IP
// закрыто, compensating delete-after-failure не нужен (Abort сам снимает Insert).
type CreateAddressUseCase struct {
	// quota — совещательная полоса учёта (порт QuotaGuard).
	//
	// nil означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции, и исчерпание приезжает отказом
	// операции. Различие наблюдаемо (429 синхронно против отказа в операции), и
	// потому провязка обязательна на любом поднятом стенде; отсутствие допустимо
	// только там, где нет и соседа, у которого спрашивать величины.
	quota         QuotaGuard
	repo          Repo
	subnetReader  SubnetReader
	projectClient ProjectClient
	opsRepo       operations.Repo
	pools         PoolService // nil → external IPAM недоступна (test-only)
	registrar     fgaregister.Registrar
	zoneReg       ZoneRegistry // nil → external zone_id existence не проверяется
}

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2): после
// commit Address owner-tuple синхронно регистрируется в kaname. Nil →
// sync-путь пропускается (только async drainer).
func (u *CreateAddressUseCase) WithRegistrar(r fgaregister.Registrar) *CreateAddressUseCase {
	u.registrar = r
	return u
}

// WithZoneRegistry подключает geo zone-registry для existence-валидации `zone_id`
// external-адреса (placement-coherence, ребро vpc→geo). Nil → проверка
// пропускается (тест-fallback; в composition root инжектится всегда).
func (u *CreateAddressUseCase) WithZoneRegistry(zr ZoneRegistry) *CreateAddressUseCase {
	u.zoneReg = zr
	return u
}

// NewCreateAddressUseCase создает CreateAddressUseCase.
func NewCreateAddressUseCase(r Repo, subnetReader SubnetReader, projectClient ProjectClient, opsRepo operations.Repo, pools PoolService) *CreateAddressUseCase {
	return &CreateAddressUseCase{
		repo:          r,
		subnetReader:  subnetReader,
		projectClient: projectClient,
		opsRepo:       opsRepo,
		pools:         pools,
	}
}

// Execute — sync-валидация + create Operation + запуск worker'а.
func (u *CreateAddressUseCase) Execute(ctx context.Context, in CreateInput) (*operations.Operation, error) {
	if in.InternalSpec != nil {
		if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, in.InternalSpec.SubnetID); err != nil {
			return nil, err
		}
	}
	if in.ProjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id required")
	}
	if in.InternalIpv6Spec != nil {
		if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, in.InternalIpv6Spec.SubnetID); err != nil {
			return nil, err
		}
	}
	if in.ExternalSpec == nil && in.InternalSpec == nil && in.InternalIpv6Spec == nil && in.ExternalIpv6Spec == nil {
		return nil, status.Error(codes.InvalidArgument, "address_spec required")
	}

	// Domain self-validation: Name / Description / Labels через newtypes —
	// всё проходит через Address.Validate(). Пустое имя здесь законно и означает
	// «назови сам»: оно заменяется умолчанием ниже, когда идентификатор уже
	// сгенерирован.
	addrForValidate := domain.Address{
		Name:        domain.RcNameVPC(in.Name),
		Description: domain.RcDescription(in.Description),
		Labels:      domain.LabelsFromMap(in.Labels),
	}
	if err := serviceerr.FromValidation(addrForValidate.Validate()); err != nil {
		return nil, err
	}

	// Явный внешний адрес: формат и семейство проверяются СИНХРОННО, первым
	// делом, и адрес приводится к канонической форме. Без этого в книгу учёта
	// внешних адресов ложилась любая строка (уникальность внешнего адреса —
	// текстовый индекс, поэтому «2001:DB8::1» и «2001:db8::1» разъезжались бы),
	// а занятие адреса в пуле вообще нельзя было бы выразить.
	if in.ExternalSpec != nil && in.ExternalSpec.Address != "" {
		canon, err := canonicalExplicitIP("external_ipv4_address_spec.address", in.ExternalSpec.Address, domain.IpVersionIPv4)
		if err != nil {
			return nil, err
		}
		in.ExternalSpec.Address = canon
	}
	if in.ExternalIpv6Spec != nil && in.ExternalIpv6Spec.Address != "" {
		canon, err := canonicalExplicitIP("external_ipv6_address_spec.address", in.ExternalIpv6Spec.Address, domain.IpVersionIPv6)
		if err != nil {
			return nil, err
		}
		in.ExternalIpv6Spec.Address = canon
	}

	// Placement-coherence: existence-валидация `zone_id` external-адреса через geo
	// (зеркало subnet.validateZoneID). Условная — непустой zone_id → existence-
	// check; пустой zone_id ВАЛИДЕН (anycast из global-пула, зоне-независим — в
	// отличие от Subnet). Симметрия v4/v6. Sync fail-fast ДО Operation.
	if in.ExternalSpec != nil {
		if err := u.validateExternalZone(ctx, in.ExternalSpec.ZoneID); err != nil {
			return nil, err
		}
	}
	if in.ExternalIpv6Spec != nil {
		if err := u.validateExternalZone(ctx, in.ExternalIpv6Spec.ZoneID); err != nil {
			return nil, err
		}
	}

	// Sync project.Exists precheck убран как race-prone: между sync-проверкой и
	// async-частью project может быть удален peer-сервисом, и second-writer-wins
	// безусловно создавал бы ресурс. NotFound возвращается через
	// `operation.error` из async `doCreate`. Sync subnet/uniqueness-проверки
	// (через DB-state в той же сервис-БД) остаются — они race-free относительно
	// peer-сервисов.
	if in.InternalSpec != nil && in.InternalSpec.SubnetID != "" {
		if err := u.assertSubnetOwned(ctx, in.InternalSpec.SubnetID, in.ProjectID); err != nil {
			return nil, err
		}
	}
	if in.InternalIpv6Spec != nil && in.InternalIpv6Spec.SubnetID != "" {
		if err := u.assertSubnetOwned(ctx, in.InternalIpv6Spec.SubnetID, in.ProjectID); err != nil {
			return nil, err
		}
	}
	if in.Name != "" {
		rd, err := u.repo.Reader(ctx)
		if err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
		existing, _, lerr := rd.Addresses().List(ctx, AddressFilter{ProjectID: in.ProjectID, Name: in.Name}, Pagination{})
		_ = rd.Close()
		if lerr != nil {
			return nil, serviceerr.MapRepoErr(lerr)
		}
		if len(existing) > 0 {
			return nil, status.Errorf(codes.AlreadyExists, "Address with name %s already exists", in.Name)
		}
	}

	// Sync-проверка: explicit IP (`internal_ipv4_address_spec.address`) должен
	// принадлежать CIDR-блоку указанной subnet. Если IP вне CIDR — возвращаем
	// sync InvalidArgument: иначе адрес попадает в БД минуя любые FK, что
	// приводит к мусору в IPAM.
	if in.InternalSpec != nil && in.InternalSpec.SubnetID != "" && in.InternalSpec.Address != "" {
		if err := u.validateInternalIPInSubnet(ctx, in.InternalSpec.SubnetID, in.InternalSpec.Address); err != nil {
			return nil, err
		}
	}
	// Симметрично v4: explicit internal IPv6 тоже обязан лежать в одном из
	// v6_cidr_blocks указанной subnet — иначе мусорный IP попадал бы в БД.
	if in.InternalIpv6Spec != nil && in.InternalIpv6Spec.SubnetID != "" && in.InternalIpv6Spec.Address != "" {
		if err := u.validateInternalIPv6InSubnet(ctx, in.InternalIpv6Spec.SubnetID, in.InternalIpv6Spec.Address); err != nil {
			return nil, err
		}
	}

	addrID := ids.NewID(ids.PrefixAddress)
	// Пустое имя не доживает до записи: ресурса без имени не бывает (#715).
	// Подстановка стоит ПОСЛЕ чеканки идентификатора (умолчание производно от
	// него) и ДО сборки строки — и она же снимает нужду в проверке «а не занято
	// ли»: идентификатор глобально уникален by construction, поэтому уникальность
	// имени остаётся за индексом БД, а не за чтением-перед-вставкой (ban #10).
	in.Name = corevalidate.NameOrDefault(in.Name, addrID)
	// Учёт числа ресурсов: ранний отказ ДО создания операции.
	//
	// Здесь же материализуются строки учёта, если проект их ещё не имеет, —
	// момент, когда владелец типа впервые узнаёт о проекте, и есть обращение к
	// нему. Отказ уходит арендатору синхронно тем же текстом и признаком, каким
	// его произвёл бы триггер: у обеих полос один производитель.
	if u.quota != nil {
		if err := u.quota.Admit(ctx, string(in.ProjectID), "vpc.address"); err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Create address %s", in.Name),
		&vpcv1.CreateAddressMetadata{AddressId: addrID},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}

	// Create — durable commit → op done сразу после worker-fn. Owner-tuple
	// материализуется eventually-consistent (sync-registrar + drainer/reconciler
	// backstop), а не гейтит done.
	operations.Run(ctx, u.opsRepo, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		return u.doCreate(ctx, addrID, in)
	})

	return &op, nil
}

// canonicalExplicitIP — разбор явно заданного адреса + проверка семейства +
// канонизация. Семейство обязано совпадать с выбранной ветвью спека: адрес
// «10.0.0.1» в IPv6-спеке — не опечатка вызывающего, а другой ресурс.
func canonicalExplicitIP(field, address string, want domain.IpVersion) (string, error) {
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return "", serviceerr.InvalidArg(field, "address is not a valid IP")
	}
	switch want {
	case domain.IpVersionIPv4:
		if !addr.Is4() {
			return "", serviceerr.InvalidArg(field, "address is not a valid IPv4 address")
		}
	case domain.IpVersionIPv6:
		if !addr.Is6() || addr.Is4In6() {
			return "", serviceerr.InvalidArg(field, "address is not a valid IPv6 address")
		}
	default:
		return "", serviceerr.InvalidArg(field, "address is not a valid IP")
	}
	return addr.String(), nil
}

// validateInternalIPInSubnet проверяет sync-ом, что explicit IP лежит в CIDR
// одной из v4_cidr_blocks указанной subnet. Если subnet не найден — пропуск
// (NotFound будет возвращен async через doCreate, как и для всех остальных FK).
// Любая другая ошибка чтения subnetReader → Internal: pass-through.
func (u *CreateAddressUseCase) validateInternalIPInSubnet(ctx context.Context, subnetID, address string) error {
	sub, err := u.subnetReader.Get(ctx, subnetID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			// Subnet 404 — отдаем async, не sync.
			return nil
		}
		return serviceerr.MapRepoErr(err)
	}
	if len(sub.V4CidrBlocks) == 0 {
		// CIDR-less subnet (v4_cidr_blocks не required на Subnet.Create): нельзя
		// ни валидировать explicit address, ни выделить internal IPv4 в такой
		// подсети.
		return status.Errorf(codes.FailedPrecondition, "subnet %s has no IPv4 CIDR", subnetID)
	}
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return serviceerr.InvalidArg(
			"internal_ipv4_address_spec.address",
			"address is not a valid IP",
		)
	}
	for _, raw := range sub.V4CidrBlocks {
		cidr, err := netip.ParsePrefix(raw)
		if err != nil {
			return status.Errorf(codes.Internal, "subnet has invalid cidr block %q", raw)
		}
		if cidr.Contains(addr) {
			return nil
		}
	}
	return serviceerr.InvalidArg(
		"internal_ipv4_address_spec.address",
		fmt.Sprintf("address %s is not within subnet cidr %s", address, sub.V4CidrBlocks[0]),
	)
}

// validateInternalIPv6InSubnet — v6-зеркало validateInternalIPInSubnet:
// explicit IPv6 обязан лежать в одном из v6_cidr_blocks subnet'а. Subnet 404 →
// пропуск (async NotFound через doCreate).
func (u *CreateAddressUseCase) validateInternalIPv6InSubnet(ctx context.Context, subnetID, address string) error {
	sub, err := u.subnetReader.Get(ctx, subnetID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil
		}
		return serviceerr.MapRepoErr(err)
	}
	if len(sub.V6CidrBlocks) == 0 {
		return status.Errorf(codes.FailedPrecondition, "subnet %s has no IPv6 CIDR", subnetID)
	}
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return serviceerr.InvalidArg("internal_ipv6_address_spec.address", "address is not a valid IP")
	}
	for _, raw := range sub.V6CidrBlocks {
		cidr, perr := netip.ParsePrefix(raw)
		if perr != nil {
			return status.Errorf(codes.Internal, "subnet has invalid cidr block %q", raw)
		}
		if cidr.Contains(addr) {
			return nil
		}
	}
	return serviceerr.InvalidArg(
		"internal_ipv6_address_spec.address",
		fmt.Sprintf("address %s is not within subnet cidr %s", address, sub.V6CidrBlocks[0]),
	)
}

// validateExternalZone — placement-coherence existence-check `zone_id`
// external-адреса через geo (зеркало subnet.validateZoneID). Условная:
//   - пустой zone_id → пропуск (anycast из global-пула, зоне-независим — в
//     отличие от Subnet, где ZONAL требует непустой zone_id);
//   - zoneReg == nil → пропуск (тест-fallback без geo);
//   - зона не найдена (geo NotFound → repo.ErrNotFound) → полоса peer-validate:
//     FailedPrecondition `unknown zone id '<X>'` с машинным признаком
//     (verbatim-зеркало subnet.validateZoneID — общий serviceerr.UnknownZone,
//     а не совпадающая копия текста);
//   - geo недоступен → fail-closed (MapRepoErr пробрасывает Unavailable).
//
// internal Address зону НЕ несёт (наследует через subnet_id) — сюда не попадает.
func (u *CreateAddressUseCase) validateExternalZone(ctx context.Context, zoneID string) error {
	if zoneID == "" || u.zoneReg == nil {
		return nil
	}
	if _, err := u.zoneReg.Get(ctx, zoneID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return serviceerr.UnknownZone(zoneID)
		}
		return serviceerr.MapRepoErr(err)
	}
	return nil
}

// assertSubnetOwned — общая FK+BOLA-валидация для internal-family (v4 и v6):
// referenced Subnet обязана существовать И принадлежать проекту вызывающего.
// Пустой subnetID пропускается (CIDR-less подсеть легальна). Cross-project
// reference («subnet есть, но чужого проекта») отдаёт тот же NotFound
// "Subnet <X> not found", что и несуществующий subnet — без existence-oracle.
// Это чтение связанного ресурса, а не side-effect — остаётся в use-case.
func (u *CreateAddressUseCase) assertSubnetOwned(ctx context.Context, subnetID, projectID string) error {
	if subnetID == "" {
		return nil
	}
	sub, serr := u.subnetReader.Get(ctx, subnetID)
	if serr != nil {
		return status.Errorf(codes.NotFound, "Subnet %s not found", subnetID)
	}
	if sub.ProjectID != projectID {
		return status.Errorf(codes.NotFound, "Subnet %s not found", subnetID)
	}
	return nil
}

// applyAddressSpec — заполняет family-specific поля domain.Address по тому
// единственному из четырех spec'ов, что задан в CreateInput (взаимоисключимость
// гарантирована валидатором Execute). Все четыре ветви идут через общие
// helper'ы (mapRequirements для external, checkSubnetExists для internal), чтобы
// новое family-инвариант-правило не пришлось дублировать по-семейно.
func (u *CreateAddressUseCase) applyAddressSpec(ctx context.Context, a *domain.Address, in CreateInput) error {
	switch {
	case in.ExternalSpec != nil:
		a.Type = domain.AddressTypeExternal
		a.IpVersion = domain.IpVersionIPv4
		a.ExternalIpv4 = &domain.ExternalIpv4Spec{
			Address: in.ExternalSpec.Address,
			ZoneID:  in.ExternalSpec.ZoneID,
		}
	case in.InternalSpec != nil:
		a.Type = domain.AddressTypeInternal
		a.IpVersion = domain.IpVersionIPv4
		if err := u.assertSubnetOwned(ctx, in.InternalSpec.SubnetID, in.ProjectID); err != nil {
			return err
		}
		a.InternalIpv4 = &domain.InternalIpv4Spec{
			Address:  in.InternalSpec.Address,
			SubnetID: in.InternalSpec.SubnetID,
		}
	case in.InternalIpv6Spec != nil:
		a.Type = domain.AddressTypeInternal
		a.IpVersion = domain.IpVersionIPv6
		if err := u.assertSubnetOwned(ctx, in.InternalIpv6Spec.SubnetID, in.ProjectID); err != nil {
			return err
		}
		a.InternalIpv6 = &domain.InternalIpv6Spec{
			Address:  in.InternalIpv6Spec.Address,
			SubnetID: in.InternalIpv6Spec.SubnetID,
		}
	case in.ExternalIpv6Spec != nil:
		// external IPv6: sparse counter-based allocator из глобального
		// AddressPool с v6 CIDR (cascade resolve как у v4).
		a.Type = domain.AddressTypeExternal
		a.IpVersion = domain.IpVersionIPv6
		a.ExternalIpv6 = &domain.ExternalIpv6Spec{
			Address: in.ExternalIpv6Spec.Address,
			ZoneID:  in.ExternalIpv6Spec.ZoneID,
		}
	default:
		// Ни одна ветвь oneof не выбрана. Раньше сюда падала внешняя IPv6 —
		// то есть `default` означал «обязательно четвёртый вариант», а не
		// «неизвестный», и сразу разыменовывал `ExternalIpv6Spec`. Локально
		// это ничем не было выражено: единственной защитой была проверка в
		// Execute, за несколько сотен строк и один вызов отсюда.
		//
		// Запрос без выбранной ветви — не редкость: обёртка вокруг имени
		// самого oneof отбрасывается краем целиком (в protojson oneof
		// прозрачен, на проводе лежит ключ ВЕТВИ), и до сервиса доезжает
		// тело без единого спека. Так и было в четырёх местах подготовки
		// набора nlb — во всех сохранённых прогонах. Проверка в Execute
		// выдержала; добавь кто-нибудь пятую ветвь и забудь про неё здесь —
		// тот же вход разыменовал бы nil уже внутри воркера операции.
		//
		// Ветвь выбирается СВОИМ условием, `default` — отказ.
		return status.Error(codes.InvalidArgument, "address_spec required")
	}
	return nil
}

// doCreate — async-часть Create (внутри Operation worker'а). Атомарный
// backstop: project-exists + Insert + multi-family IPAM allocation +
// outbox-emit Address.CREATED — все в одной writer-TX. Defer w.Abort() —
// при любой ошибке Insert и Allocate-side-effects откатываются автоматически,
// orphan-address window закрыт.
func (u *CreateAddressUseCase) doCreate(ctx context.Context, addrID string, in CreateInput) (*anypb.Any, error) {
	exists, err := u.projectClient.Exists(ctx, in.ProjectID)
	if err != nil {
		// Проза соседа — оператору, в журнал. Наружу — фиксированный текст: сырой
		// отказ транспорта несёт адрес и порт узла, а через `operation.error`
		// уезжает арендатору. Клиент, приняв «не годится» за «повтори позже» (или
		// наоборот), выбирает не то действие, поэтому важен КОД, а не проза: он
		// остаётся `UNAVAILABLE` — непроверяемое предусловие мутации не считается
		// выполненным (fail-closed).
		//
		// Текст совпадает с `compute` дословно и намеренно: у двух сервисов один
		// вопрос к одному соседу, и два разных ответа на него читались бы как два
		// разных состояния платформы.
		slog.ErrorContext(ctx, "address create: project existence check failed",
			"project_id", in.ProjectID, "address_id", addrID, "err", err)
		return nil, status.Error(codes.Unavailable, "project check: upstream project service unavailable")
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "Project %s not found", in.ProjectID)
	}

	a := &domain.Address{
		ID:                 addrID,
		ProjectID:          in.ProjectID,
		Name:               domain.RcNameVPC(in.Name),
		Description:        domain.RcDescription(in.Description),
		Labels:             domain.LabelsFromMap(in.Labels),
		DeletionProtection: in.DeletionProtection,
		Reserved:           true,
	}

	if err := u.applyAddressSpec(ctx, a, in); err != nil {
		return nil, err
	}

	// Резолвим external-пулы ДО открытия Writer-TX (отдельная Reader-TX).
	// Иначе resolve открывал бы вторую pool-конн под уже держимым Writer'ом —
	// при burst-нагрузке все коннекты заняты Writer'ами, ждущими Reader'ов →
	// deadlock пула. После pre-resolve внутри Writer'а остается только атомарный
	// freelist-pop по уже известному pool.ID.
	var v4Pool, v6Pool *addresspool.ResolvedPool
	if u.pools != nil {
		if a.ExternalIpv4 != nil && a.ExternalIpv4.Address == "" {
			v4Pool, err = u.pools.ResolvePoolForAddressObjFamily(ctx, &kachorepo.AddressRecord{Address: *a}, addresspool.FamilyV4)
			if err != nil {
				return nil, poolResolveFailure(ctx, addrID, domain.IpVersionIPv4, err)
			}
		}
		if a.ExternalIpv6 != nil && a.ExternalIpv6.Address == "" {
			v6Pool, err = u.pools.ResolvePoolForAddressObjFamily(ctx, &kachorepo.AddressRecord{Address: *a}, addresspool.FamilyV6)
			if err != nil {
				return nil, poolResolveFailure(ctx, addrID, domain.IpVersionIPv6, err)
			}
		}
	}

	// Открываем ОДНУ writer-TX на Insert + Allocate + Outbox — atomic.
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	created, err := w.Addresses().Insert(ctx, a)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}

	// Inline IPAM allocation в той же writer-TX (atomic с Insert + outbox).
	// При любой ошибке Abort откатывает Insert — compensating delete больше
	// не нужен. Пулы уже резолвлены до Writer'а (см. выше).
	if created.ExternalIpv4 != nil && created.ExternalIpv4.Address == "" && v4Pool != nil {
		res, aerr := u.allocateExternalIPv4(ctx, w, created, v4Pool)
		if aerr != nil {
			return nil, aerr
		}
		created.ExternalIpv4.Address = res.IP
		created.ExternalIpv4.AddressPoolID = res.PoolID
	}
	if u.pools != nil {
		if created.InternalIpv4 != nil && created.InternalIpv4.Address == "" && created.InternalIpv4.SubnetID != "" {
			res, aerr := u.allocateInternalIPv4(ctx, w, created)
			if aerr != nil {
				return nil, aerr
			}
			created.InternalIpv4.Address = res.IP
		}
	}
	// IPv6 internal IPAM не зависит от pools (адрес выбирается случайно внутри
	// subnet.v6_cidr_blocks[0]) — аллоцируем независимо от u.pools.
	if created.InternalIpv6 != nil && created.InternalIpv6.Address == "" && created.InternalIpv6.SubnetID != "" {
		res, aerr := u.allocateInternalIPv6(ctx, w, created)
		if aerr != nil {
			return nil, aerr
		}
		created.InternalIpv6.Address = res.IP
	}
	// External IPv6: sparse counter-based allocator. Пул резолвлен до Writer'а
	// (v6Pool).
	if created.ExternalIpv6 != nil && created.ExternalIpv6.Address == "" && v6Pool != nil {
		res, aerr := u.allocateExternalIPv6(ctx, w, created, v6Pool)
		if aerr != nil {
			return nil, aerr
		}
		created.ExternalIpv6.Address = res.IP
		created.ExternalIpv6.AddressPoolID = res.PoolID
	}

	// Привязка к владельцу — В ТОЙ ЖЕ writer-TX, после аллокации и до outbox.
	// Порядок важен только тем, что адрес к этому моменту уже вставлен; сама
	// запись атомарна с ним по построению: отказ ниже (или отсутствие коммита)
	// снимает и вставку, и привязку, поэтому half-linked адреса не бывает и
	// компенсировать вызывающему нечего.
	if in.Owner != nil {
		if _, rerr := w.Addresses().SetReference(ctx, &domain.AddressReference{
			AddressID:    created.ID,
			ReferrerType: in.Owner.ReferrerType,
			ReferrerID:   in.Owner.ReferrerID,
			ReferrerName: in.Owner.ReferrerName,
			Owned:        in.Owner.Owned,
		}); rerr != nil {
			return nil, serviceerr.MapRepoErr(rerr)
		}
		created.Used = true
	}
	if err := w.Outbox().Emit(ctx, "Address", created.ID, created.ProjectID, "CREATED", helpers.DomainToMap(created)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	// Публикуем INTENT на owner-tuple vpc_address→project в той же writer-TX
	// (atomic с Insert + IPAM allocate). Так intent не теряется при ошибке,
	// в отличие от best-effort emit после commit. В mirror-feed несем labels
	// Address + parent_project_id (ProjectHierarchyItem), а не голый tuple — иначе
	// resource_mirror в kaname остается без labels и ARM_LABELS-селектор не
	// матчит даже свежесозданный Address. Симметрично network/subnet/securitygroup.
	items := []fgaregister.Item{
		fgaregister.ProjectHierarchyItem(in.ProjectID, "vpc_address", created.ID,
			domain.LabelsToMap(created.Labels)),
	}
	// Версия, которой БД проштамповала intent ВНУТРИ writer-TX: её же понесёт
	// синхронная регистрация ниже, чтобы повторную доставку гасило монотонное
	// сравнение у принимающей стороны независимо от того, кто пришёл первым.
	intentVersion, err := w.FGARegister().EmitRegister(ctx, fgaregister.RegisterItems(items...))
	if err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga register intent: %v", repo.ErrInternal, err))
	}
	if err := w.Commit(); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	// Sync-primary owner-tuple registration идёт ПОСЛЕ durable commit: она
	// сокращает окно видимости гранта, но НЕ является условием успеха мутации.
	// Intent на тот же tuple лежит в fga_register_outbox той же writer-TX →
	// at-least-once дренаж доведёт грант сам. Провалить операцию здесь значило бы
	// отдать вызывающему код узла прав (status.FromError достаёт вложенный статус
	// и подменяет сообщение всей цепочкой) на уже созданный адрес — фантом.
	// Поэтому предупреждение, а не ошибка.
	fgaregister.DeliverAfterCommit(ctx, u.registrar, items, intentVersion, "Address", created.ID)
	return marshalAddressRecord(created)
}

// --- Allocation helpers ------------------------------------------------------
//
// Create-time обёртки для 4 семейств (internal v4/v6, external v4/v6): делают
// pre-checks (nil-spec / already-allocated / empty subnet|pool) и оборачивают
// результат в allocResult. Сам двухфазный IPAM-цикл (random-pick + sweep) и
// external freelist-pop вынесены в `alloc_shared.go` — единый источник,
// переиспользуемый и AllocateUseCase (internal Allocate RPC), чтобы алгоритм не
// дрейфовал между create- и allocate-путём.
//
// Каждый helper принимает открытый Writer-TX — SetInternalIPv4/SetInternalIPv6/
// AllocateIPFromFreelist/AllocateExternalIPv6 идут через `w.Addresses().*`,
// atomic с Insert + Outbox в одной TX.

// allocateMaxAttempts — максимум попыток random-pick + retry-on-conflict.
// При near-full CIDR (≥95% занято) random-pick имеет high false-fail rate
// (см. allocateRandomPhase ниже). После этого порога переключаемся на
// deterministic sweep.
const allocateMaxAttempts = 32

// allocateRandomPhase — сколько попыток сделать random-pick'ом до того
// как переключиться на deterministic sweep по тем же CIDR. Random в первые
// N попыток дешевле (1 SQL/попытка), при low/medium occupancy сходится
// быстро. Переход в sweep гарантирует closure под high-occupancy.
const allocateRandomPhase = 8

// v6AllocateMaxAttempts — число попыток random-pick + retry-on-conflict для
// internal-IPv6. IPv6-подсети огромные (обычно /64), коллизии редки —
// небольшого числа попыток достаточно.
const v6AllocateMaxAttempts = 16

// allocResult — итог inline-аллокации одного семейства.
type allocResult struct {
	IP     string
	PoolID string // только для external; "" для internal
}

func (u *CreateAddressUseCase) allocateInternalIPv4(ctx context.Context, w Writer, addr *kachorepo.AddressRecord) (*allocResult, error) {
	if addr.InternalIpv4 == nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"address %s has no internal_ipv4 spec", addr.ID)
	}
	if addr.InternalIpv4.Address != "" {
		return &allocResult{IP: addr.InternalIpv4.Address}, nil
	}
	if addr.InternalIpv4.SubnetID == "" {
		return nil, status.Errorf(codes.FailedPrecondition,
			"address %s internal_ipv4.subnet_id is empty", addr.ID)
	}
	// Shared IPAM-цикл (alloc_shared.go) — общий с AllocateUseCase.AllocateInternalIP.
	// Subnet читается на TX writer'а внутри (single-conn, без nested reader-conn).
	updated, err := allocateInternalV4IntoTx(ctx, w, addr)
	if err != nil {
		return nil, err
	}
	return &allocResult{IP: updated.InternalIpv4.Address}, nil
}

func (u *CreateAddressUseCase) allocateInternalIPv6(ctx context.Context, w Writer, addr *kachorepo.AddressRecord) (*allocResult, error) {
	if addr.InternalIpv6 == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "address %s has no internal_ipv6 spec", addr.ID)
	}
	if addr.InternalIpv6.Address != "" {
		return &allocResult{IP: addr.InternalIpv6.Address}, nil
	}
	if addr.InternalIpv6.SubnetID == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "address %s internal_ipv6.subnet_id is empty", addr.ID)
	}
	updated, err := allocateInternalV6IntoTx(ctx, w, addr)
	if err != nil {
		return nil, err
	}
	return &allocResult{IP: updated.InternalIpv6.Address}, nil
}

func (u *CreateAddressUseCase) allocateExternalIPv4(ctx context.Context, w Writer, addr *kachorepo.AddressRecord, resolved *addresspool.ResolvedPool) (*allocResult, error) {
	if addr.ExternalIpv4 == nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"address %s has no external_ipv4 spec", addr.ID)
	}
	if addr.ExternalIpv4.Address != "" {
		return &allocResult{
			IP:     addr.ExternalIpv4.Address,
			PoolID: addr.ExternalIpv4.AddressPoolID,
		}, nil
	}
	pool := resolved.Pool
	if len(pool.V4CIDRBlocks) == 0 {
		return nil, poolCarriesNoBlocks(ctx, pool.ID, addr.ID, domain.IpVersionIPv4)
	}
	ip, err := allocateExternalV4IntoTx(ctx, w, pool.ID, addr.ID)
	if err != nil {
		return nil, err
	}
	return &allocResult{IP: ip, PoolID: pool.ID}, nil
}

func (u *CreateAddressUseCase) allocateExternalIPv6(ctx context.Context, w Writer, addr *kachorepo.AddressRecord, resolved *addresspool.ResolvedPool) (*allocResult, error) {
	if addr.ExternalIpv6 == nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"address %s has no external_ipv6 spec", addr.ID)
	}
	if addr.ExternalIpv6.Address != "" {
		return &allocResult{
			IP:     addr.ExternalIpv6.Address,
			PoolID: addr.ExternalIpv6.AddressPoolID,
		}, nil
	}
	pool := resolved.Pool
	if len(pool.V6CIDRBlocks) == 0 {
		return nil, poolCarriesNoBlocks(ctx, pool.ID, addr.ID, domain.IpVersionIPv6)
	}
	ip, err := allocateExternalV6IntoTx(ctx, w, pool.ID, addr.ID, addr.ExternalIpv6.ZoneID)
	if err != nil {
		return nil, err
	}
	return &allocResult{IP: ip, PoolID: pool.ID}, nil
}

// WithQuotaGuard подключает совещательную полосу учёта.
//
// Отдельным глаголом, а не аргументом конструктора: полоса появилась позже
// вызывающих, и обязательный аргумент заставил бы править каждую сборку — в том
// числе те, где соседа с величинами нет вовсе.
func (u *CreateAddressUseCase) WithQuotaGuard(g QuotaGuard) *CreateAddressUseCase {
	u.quota = g
	return u
}
