// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// CreateGatewayUseCase инициирует создание Gateway. Sync-проверки формата входа
// выполняются ДО создания Operation — клиент получает fast-fail gRPC-status, а не
// «200 + операция, упавшая через секунду». Async-часть (`doCreate`) — атомарный
// backstop через FK.
//
// Worker открывает одну Writer-TX, делает Insert(Gateway) + outbox emit и Commit:
// либо все видно, либо ничего — окно orphan-Gateway / потерянного outbox-event'а
// закрыто.
type CreateGatewayUseCase struct {
	// quota — совещательная полоса учёта (порт QuotaGuard).
	//
	// nil означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции, и исчерпание приезжает отказом
	// операции. Различие наблюдаемо (429 синхронно против отказа в операции), и
	// потому провязка обязательна на любом поднятом стенде; отсутствие допустимо
	// только там, где нет и соседа, у которого спрашивать величины.
	quota         QuotaGuard
	repo          Repo
	projectClient ProjectClient
	opsRepo       operations.Repo
	registrar     fgaregister.Registrar
}

// NewCreateGatewayUseCase создает CreateGatewayUseCase.
func NewCreateGatewayUseCase(r Repo, projectClient ProjectClient, opsRepo operations.Repo) *CreateGatewayUseCase {
	return &CreateGatewayUseCase{
		repo:          r,
		projectClient: projectClient,
		opsRepo:       opsRepo,
	}
}

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2): после
// commit Gateway owner-tuple синхронно регистрируется в kaname. Nil →
// sync-путь пропускается (только async drainer).
func (u *CreateGatewayUseCase) WithRegistrar(r fgaregister.Registrar) *CreateGatewayUseCase {
	u.registrar = r
	return u
}

// Execute — sync-валидация + create Operation + запуск worker'а. Возвращает
// созданный Operation указателем (caller'у нужен он для `OperationService.Get`).
//
// Принимает `domain.Gateway` напрямую, без обертки-DTO. Поле `g.ID` на входе
// пустое — назначим внутри use-case'а через `ids.NewID(ids.PrefixGateway)`.
func (u *CreateGatewayUseCase) Execute(ctx context.Context, g domain.Gateway) (*operations.Operation, error) {
	if g.ProjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id required")
	}
	name := string(g.Name)
	// Единственная форма имени дерева; пустое на создании законно и означает
	// «назови сам» — умолчание подставляется ниже, когда id уже сгенерирован.
	if err := corevalidate.NameOnCreate("name", name); err != nil {
		return nil, err
	}
	// Domain self-validation для description/labels.
	if err := serviceerr.FromValidation(g.Validate()); err != nil {
		return nil, err
	}
	// Ветвь oneof `gateway` ОБЯЗАТЕЛЬНА и отвергается ИМЕНЕМ ПОЛЯ: шлюз без вида
	// не несёт поведения, которое можно создать. Проверка идёт по набору
	// известных видов, а не «не пусто»: неизвестное значение (например пришедшее
	// из старого клиента) обязано получить отказ, а не уехать в CHECK базы
	// фиксированным INTERNAL.
	switch g.GatewayType {
	case domain.GatewayTypeNat, domain.GatewayTypeEgressOnly:
	default:
		return nil, serviceerr.InvalidArg("gateway", "gateway: required")
	}
	// Якорь размещения обязателен и проверяется по формату СВОЕГО id первым
	// стейтментом: подсеть принадлежит vpc, значит id own-owned. Существование и
	// когерентность семейства решает оператор вставки (repo), не проверка здесь —
	// иначе между проверкой и записью подсеть могла бы исчезнуть.
	if g.SubnetID == "" {
		return nil, serviceerr.InvalidArg("subnet_id", "subnet_id: required")
	}
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, g.SubnetID); err != nil {
		return nil, err
	}

	// Sync project.Exists precheck тут не делаем — он race-prone: между sync-проверкой
	// и async-частью project может быть удален peer-сервисом, и second-writer-wins
	// безусловно создал бы ресурс. NotFound возвращается через `operation.error` из
	// async `doCreate`. Имена Gateway НЕ уникальны — name-uniqueness тут не проверяем
	// (в отличие от Network/Subnet/RouteTable/SecurityGroup).

	gwID := ids.NewID(ids.PrefixGateway)
	// Пустое имя не доживает до записи: ресурса без имени не бывает (#715).
	name = corevalidate.NameOrDefault(name, gwID)
	g.Name = domain.RcNameVPC(name)
	// Учёт числа ресурсов: ранний отказ ДО создания операции.
	//
	// Здесь же материализуются строки учёта, если проект их ещё не имеет, —
	// момент, когда владелец типа впервые узнаёт о проекте, и есть обращение к
	// нему. Отказ уходит арендатору синхронно тем же текстом и признаком, каким
	// его произвёл бы триггер: у обеих полос один производитель.
	if u.quota != nil {
		if err := u.quota.Admit(ctx, string(g.ProjectID), "vpc.gateway"); err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Create gateway %s", name),
		&vpcv1.CreateGatewayMetadata{GatewayId: gwID},
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
		return u.doCreate(ctx, gwID, g)
	})

	return &op, nil
}

// doCreate — async-часть Create (внутри Operation worker'а). Атомарный backstop:
// project-exists + Insert (FK / UNIQUE-нарушения).
//
// ВСЕ в одной writer-TX: Insert(Gateway) + outbox emit Gateway.CREATED ходят через
// ту же pgx.Tx writer'а, поэтому либо оба видны (Commit), либо ни один (Abort/crash).
func (u *CreateGatewayUseCase) doCreate(ctx context.Context, gwID string, g domain.Gateway) (*anypb.Any, error) {
	exists, err := u.projectClient.Exists(ctx, g.ProjectID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "project check: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "Project %s not found", g.ProjectID)
	}

	// Вид шлюза уже проверен по закрытому набору в Execute — подстановки по
	// умолчанию здесь НЕТ и быть не может: молчаливый выбор вида за вызывающего
	// означал бы шлюз, делающий не то, о чём просили.
	g.ID = gwID

	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	// Внешний адрес выделяется В ЭТОЙ ЖЕ транзакции и ДО вставки шлюза.
	//
	// «До» — потому что биусловие `gateways_nat_has_address_chk` (0038) связывает
	// каждую записываемую строку: состояния «шлюз трансляции без адреса» не
	// существует даже на один оператор, поэтому приписать адрес следом нельзя.
	//
	// «В этой же» — потому что предмет здесь within-service: и шлюз, и адрес, и
	// учёт пула живут в ОДНОЙ базе. Аренда, взятая из пула, и строка шлюза
	// коммитятся вместе либо не коммитятся вовсе, поэтому у сорвавшегося создания
	// нет окна, в котором аренда уже занята, а шлюза ещё нет: откат возвращает её
	// сам. Очередь компенсаций (B12) здесь не нужна и была бы хуже — она
	// закрывает окно МЕЖДУ транзакциями, которого тут нет by construction, но
	// платит за это доставкой «хотя бы раз» и временем, в течение которого пул
	// считает аренду занятой.
	if g.GatewayType == domain.GatewayTypeNat {
		addrID, aerr := u.allocateExternalAddress(ctx, w, &g)
		if aerr != nil {
			return nil, aerr
		}
		g.ExternalAddressID = addrID
	}

	created, err := w.Gateways().Insert(ctx, &g)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}

	// Обратная сторона привязки — та, которую видит ВЛАДЕЛЕЦ адреса. Ставится
	// после вставки, потому что ссылка обязана называть существующий шлюз.
	// `Owned` — адрес заказан шлюзом, а не принесён вызывающим: его жизнь связана
	// со шлюзом, и Delete снимает его целиком (см. delete.go).
	if created.ExternalAddressID != "" {
		if _, rerr := w.Addresses().SetReference(ctx, &domain.AddressReference{
			AddressID:    created.ExternalAddressID,
			ReferrerType: domain.GatewayReferrerType,
			ReferrerID:   created.ID,
			ReferrerName: string(created.Name),
			Owned:        true,
		}); rerr != nil {
			return nil, serviceerr.MapRepoErr(rerr)
		}
	}
	if oerr := w.Outbox().Emit(ctx, "Gateway", created.ID, created.ProjectID, "CREATED", helpers.DomainToMap(created)); oerr != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, oerr))
	}
	// Записываем INTENT hierarchy-tuple vpc_gateway→project в той же writer-TX,
	// чтобы register-намерение было атомарно с Insert и не терялось при ошибке.
	// В mirror-feed несем labels Gateway + parent_project_id (ProjectHierarchyItem),
	// а не голый tuple — иначе resource_mirror в kaname остается без labels и
	// ARM_LABELS-селектор не матчит даже свежесозданный Gateway. Симметрично
	// network/subnet/securitygroup create.
	items := []fgaregister.Item{
		fgaregister.ProjectHierarchyItem(string(g.ProjectID), "vpc_gateway", created.ID,
			domain.LabelsToMap(created.Labels)),
	}
	// Адрес шлюза — самостоятельный ресурс, и его владелец обязан получить на
	// него право ТЕМ ЖЕ порядком, что и на адрес, созданный напрямую. Без этого
	// `NatGateway.address_id` был бы координатой, по которой вызывающему нечего
	// прочитать: `AddressService.Get` гейтится по объекту адреса, а не шлюза.
	if created.ExternalAddressID != "" {
		items = append(items, fgaregister.ProjectHierarchyItem(
			string(g.ProjectID), "vpc_address", created.ExternalAddressID, nil))
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
	// и подменяет сообщение всей цепочкой) на уже созданный gateway — фантом.
	// Поэтому предупреждение, а не ошибка.
	fgaregister.DeliverAfterCommit(ctx, u.registrar, items, intentVersion, "Gateway", created.ID)
	return marshalGatewayRecord(created)
}

// allocateExternalAddress — внешний IPv4 шлюзу трансляции, внутри уже открытой
// writer-TX вызывающего.
//
// РАЗМЕЩЕНИЕ НЕ ПРИНИМАЕТСЯ НА ВХОД, А ВЫВОДИТСЯ ИЗ ЯКОРЯ. Зона берётся у
// подсети-якоря и только у неё, поэтому когерентность здесь — свойство
// построения, а не проверка, которую можно забыть выполнить: второму написанию
// зоны просто неоткуда взяться, а значит нечему разойтись с первым.
//
//   - зональный якорь → пул СВОЕЙ зоны. Провала в зоне-независимый пул нет
//     намеренно: выкроить «адрес зоны A» из anycast-префикса значило бы выдать
//     адрес, объявляющий зону, которой у его префикса нет;
//   - REGIONAL (anycast) якорь зоны не несёт вовсе → зоне-независимый пул. Из
//     зональной сверки такой шлюз исключён by construction — сравнивать не с чем.
//
// Подсеть читается через СОБСТВЕННУЮ TX writer'а (`GetForShare`), а не отдельным
// Reader'ом: у writer'а уже держится соединение пула, и открытие второго под
// held-writer'ом — nested-conn deadlock под нагрузкой (тот же инвариант, что у
// аллокатора адресов). Share-lock сериализует чтение якоря против его правки и
// совместим сам с собой — параллельные создания не выстраиваются в очередь.
func (u *CreateGatewayUseCase) allocateExternalAddress(ctx context.Context, w Writer, g *domain.Gateway) (string, error) {
	sub, err := w.Subnets().GetForShare(ctx, g.SubnetID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			// Байт-идентично настоящему промаху — тот же текст, что даёт вставка
			// шлюза. Различимый ответ был бы оракулом существования подсети.
			return "", status.Errorf(codes.NotFound, "Subnet %s not found", g.SubnetID)
		}
		return "", serviceerr.MapRepoErr(err)
	}
	if sub.ProjectID != g.ProjectID {
		return "", status.Errorf(codes.NotFound, "Subnet %s not found", g.SubnetID)
	}

	pool, err := w.AddressPools().GetDefaultForZone(ctx, sub.ZoneID, domain.AddressPoolKindExternalPublic)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return "", noExternalAddressForGateway(ctx, g.ID, sub.ZoneID, "no default external pool for this placement")
	case err != nil:
		return "", serviceerr.MapRepoErr(err)
	case len(pool.V4CIDRBlocks) == 0:
		return "", noExternalAddressForGateway(ctx, g.ID, sub.ZoneID, "resolved pool carries no IPv4 blocks")
	}

	addrID := ids.NewID(ids.PrefixAddress)
	// `Reserved` остаётся ложью осознанно: адрес не заказан арендатором сам по
	// себе, он возник как следствие создания шлюза, и его жизнь связана со
	// шлюзом.
	//
	// ИМЯ ЗАДАЁТСЯ, И ОНО ПРОИЗВОДНОЕ ОТ `id`. Здесь стояло обратное — «имя не
	// задаётся, а частичный UNIQUE считает пустые имена различными», — и это
	// перестало быть правдой в тот момент, когда форма имени стала одной на
	// дерево: пустое имя больше не доживает до записи, а уникальность держит
	// полный UNIQUE(project_id, name). Прежнее допущение не просто устарело —
	// на нём создание шлюза переставало работать целиком.
	//
	// Производное от `id` уникально by construction: `id` глобально уникален,
	// поэтому подбирать свободное имя не нужно, а подбор был бы проверкой
	// перед вставкой — тем самым check-then-act, который запрещён.
	if _, err := w.Addresses().Insert(ctx, &domain.Address{
		ID:           addrID,
		ProjectID:    g.ProjectID,
		Name:         domain.GatewayAddressName(addrID),
		Type:         domain.AddressTypeExternal,
		IpVersion:    domain.IpVersionIPv4,
		ExternalIpv4: &domain.ExternalIpv4Spec{ZoneID: sub.ZoneID},
	}); err != nil {
		return "", serviceerr.MapRepoErr(err)
	}
	if _, err := w.Addresses().AllocateIPFromFreelist(ctx, pool.ID, addrID); err != nil {
		if errors.Is(err, repo.ErrPoolExhausted) {
			return "", noExternalAddressForGateway(ctx, g.ID, sub.ZoneID, "freelist empty")
		}
		slog.ErrorContext(ctx, "gateway: external IPv4 allocate failed",
			"gateway_id", g.ID, "pool_id", pool.ID, "err", err)
		return "", serviceerr.MapRepoErr(fmt.Errorf("%w: allocate external address", repo.ErrInternal))
	}
	return addrID, nil
}

// reasonExternalUnavailable — машинный признак причины отказа, уезжающий
// вызывающему в деталях ответа. Тот же токен, каким отвечает полоса выделения
// адреса напрямую: у одной причины один признак, иначе клиент, ключующийся на
// токен, обязан был бы знать, каким путём он к ней пришёл.
const reasonExternalUnavailable = "EXTERNAL_ADDRESS_UNAVAILABLE"

// noExternalAddressForGateway — ЕДИНСТВЕННЫЙ ответ вызывающему на любую причину,
// по которой платформе нечего выдать шлюзу под трансляцию: пула для этого
// размещения нет, у пула нет блоков IPv4, его учёт пуст.
//
// Одна причина — не упрощение, а требование: пул адресов живёт в `Internal*` на
// :9091, то есть это ресурс АДМИНИСТРАТОРА, и различие перечисленных состояний
// выводимо из его ёмкости и настройки. Оператору различие адресовано — оно
// уходит в журнал вместе с зоной якоря.
func noExternalAddressForGateway(ctx context.Context, gatewayID, zoneID, cause string) error {
	slog.WarnContext(ctx, "gateway: no external IPv4 address available for translation",
		"gateway_id", gatewayID, "anchor_zone_id", zoneID, "cause", cause)
	st := status.New(codes.FailedPrecondition, "no external IPv4 address available")
	withDetails, derr := st.WithDetails(&errdetails.ErrorInfo{
		Reason: reasonExternalUnavailable,
		Domain: "vpc.kacho.cloud",
	})
	if derr != nil {
		return st.Err()
	}
	return withDetails.Err()
}

// WithQuotaGuard подключает совещательную полосу учёта.
//
// Отдельным глаголом, а не аргументом конструктора: полоса появилась позже
// вызывающих, и обязательный аргумент заставил бы править каждую сборку — в том
// числе те, где соседа с величинами нет вовсе.
func (u *CreateGatewayUseCase) WithQuotaGuard(g QuotaGuard) *CreateGatewayUseCase {
	u.quota = g
	return u
}
