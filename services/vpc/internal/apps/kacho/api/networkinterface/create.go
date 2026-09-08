// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package networkinterface

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/macutil"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// niReferrerType — ReferrerType в address_references для адресов, привязанных к NIC.
const niReferrerType = "network_interface"

// niMacRetryAttempts — число попыток сгенерировать уникальный MAC при
// cloud-wide UNIQUE-collision (~1e-3 на 1M NIC при 40 битах энтропии — см.
// `internal/apps/kacho/shared/macutil`).
const niMacRetryAttempts = 3

// CreateInput — параметры для CreateNetworkInterfaceUseCase.Execute.
//
// Композиция `domain.NetworkInterface` + два поля запроса, которые этот путь
// принимать НЕ вправе (`InstanceID` / `Index`): они остаются в структуре, чтобы
// отказ был явным и назвал поле — принять и проигнорировать нельзя, а молча
// исполнить привязку этот путь не может (инвариант привязки живёт в охраняемом
// пути, см. Execute).
//
// Поле `n.ID` на входе пустое — назначаем внутри use-case'а через
// `ids.NewID(ids.PrefixNetworkInterface)` (NIC имеет собственный prefix `nic`).
type CreateInput struct {
	NetworkInterface domain.NetworkInterface
	// InstanceID — привязка к машине; на этом пути ОТВЕРГАЕТСЯ явно.
	InstanceID string
	// Index — слот привязки; без привязки смысла не имеет, ОТВЕРГАЕТСЯ явно.
	Index string
}

// CreateNetworkInterfaceUseCase инициирует создание NIC. Sync-проверки (name
// валиден, cardinality v4/v6) выполняются ДО создания Operation; validate+attach
// address-refs — уже в async `doCreate` внутри writer-TX. Async-часть опирается
// на атомарный DB-backstop: FK / CHECK / UNIQUE MAC + atomic-CAS на addresses.used.
//
// Worker открывает ОДНУ writer-TX и делает в ней validate+attach address-refs
// (`w.Addresses()`) + Insert(NIC) + outbox-emit + fga-register атомарно —
// reservation и NIC коммитятся/откатываются вместе (нет orphan used=true без NIC
// при краше). Parent-Subnet validation в `doCreate` идет через
// `kachoRepo.Reader().Subnets().Get` (Reader-TX, уходит на slave-pool, если он настроен).
type CreateNetworkInterfaceUseCase struct {
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
	bandwidth     domain.BandwidthLimitPolicy
}

// WithBandwidthLimitPolicy подключает то, что ПОСАДКА объявила про полосу:
// умеет ли исполнитель датаплейна ограничивать её на логическом порту и какую
// величину он гарантирует интерфейсу.
//
// Нулевое значение (метод не звали) означает «умение не объявлено», и это
// намеренно: композиционный корень, забывший провязку, обязан ОТВЕРГАТЬ поле, а
// не принимать его молча. Обратная полярность была бы удобнее и неверна по
// существу — она даёт «принято-и-проигнорировано» ровно там, где его нельзя
// заметить.
//
// Значение читается ЗДЕСЬ же, на пути запроса, тем же предикатом, что и в
// проверке при старте: «страж пропустил» ⟺ «поле принимается» — по построению.
func (u *CreateNetworkInterfaceUseCase) WithBandwidthLimitPolicy(p domain.BandwidthLimitPolicy) *CreateNetworkInterfaceUseCase {
	u.bandwidth = p
	return u
}

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2): после
// commit NIC owner-tuple синхронно регистрируется в kaname. Nil → sync-путь
// пропускается (только async drainer).
func (u *CreateNetworkInterfaceUseCase) WithRegistrar(r fgaregister.Registrar) *CreateNetworkInterfaceUseCase {
	u.registrar = r
	return u
}

// NewCreateNetworkInterfaceUseCase создает CreateNetworkInterfaceUseCase.
// Address-attach идёт через writer-TX (`w.Addresses()`), поэтому отдельный
// AddressRepo больше не инъектируется.
func NewCreateNetworkInterfaceUseCase(r Repo, projectClient ProjectClient, opsRepo operations.Repo) *CreateNetworkInterfaceUseCase {
	return &CreateNetworkInterfaceUseCase{
		repo:          r,
		projectClient: projectClient,
		opsRepo:       opsRepo,
	}
}

// Execute — sync-валидация + create Operation + запуск worker'а.
func (u *CreateNetworkInterfaceUseCase) Execute(ctx context.Context, in CreateInput) (*operations.Operation, error) {
	n := in.NetworkInterface
	if n.ProjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id required")
	}
	if n.SubnetID == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	// Domain-self-validation: Name/Description/Labels + MAC + cardinality v4/v6
	// через newtype Validate(). Service-слой не зовет corevalidate.* для этих
	// инвариантов.
	if err := serviceerr.FromValidation(n.Validate()); err != nil {
		return nil, err
	}
	// validateNICAddressCardinality — fast-fail с понятным `invalidArg` (и
	// BadRequest-details); domain.Validate тоже это проверяет, но дает generic
	// error. См. helpers.go.
	if err := validateNICAddressCardinality(n.V4AddressIDs, n.V6AddressIDs); err != nil {
		return nil, err
	}
	// Форма ссылок на адреса — СИНХРОННО, до создания Operation и до любого чтения:
	// идентификатор задаёт вызывающий, и его негодность видна без обращения к БД.
	// Без этой ветки мусорный идентификатор доезжал до чтения и возвращался
	// контракт-тоном ОТСУТСТВИЯ РЕСУРСА («address zzz not found») — то есть
	// утверждением об объекте на строку, которая объектом быть не может.
	if err := validateNICAddressRefIDs(n.V4AddressIDs, n.V6AddressIDs); err != nil {
		return nil, err
	}
	// Потолок числа групп — СИНХРОННО, до создания Operation и до любого чтения:
	// величину задаёт вызывающий, и она определяет стоимость запроса. Проверка
	// принадлежности каждой группы идёт позже, в writer-TX (там она обязана быть
	// сериализована с удалением группы), но она уже не может быть вызвана с
	// массивом произвольной длины.
	if err := validateNICSecurityGroupCardinality(n.SecurityGroupIDs); err != nil {
		return nil, err
	}
	// Ограничение полосы — СИНХРОННО, до создания Operation: величину задаёт
	// вызывающий, и её негодность видна без обращения к БД и без вызова соседа.
	// Отдав отказ в асинхронную часть, мы вернули бы вызывающему успешно созданную
	// операцию на настройку, которая не принята.
	if err := validateNICBandwidthLimit(u.bandwidth, n.BandwidthLimitMbps); err != nil {
		return nil, err
	}
	// Привязка интерфейса к машине — НЕ исход публичного создания. Инвариант
	// привязки (та же зона, принадлежность машины, атомарная смена владельца,
	// номер слота) живёт в охраняемом пути привязки; создание не может его
	// исполнить, потому что резолвить машину вправе только её владелец, а
	// обратного ребра к нему у этого сервиса нет и быть не должно. Принять поле
	// и не исполнить обещание — не исход (api-conventions «принято-и-
	// проигнорировано»), поэтому отказ явный, синхронный и с именем поля.
	if in.InstanceID != "" {
		return nil, serviceerr.InvalidArg("instance_id",
			"instance attachment is not performed at NetworkInterface.Create")
	}
	if in.Index != "" {
		return nil, serviceerr.InvalidArg("index",
			"index is meaningful only for instance attachment, which is not performed at NetworkInterface.Create")
	}
	// Sync project.Exists precheck здесь не делаем: он race-prone — между sync-
	// проверкой и async-частью project может удалить peer-сервис. NotFound для
	// несуществующего project'а возвращается через `operation.error` из `doCreate`.

	niID := ids.NewID(ids.PrefixNetworkInterface)
	// Пустое имя не доживает до записи: ресурса без имени не бывает (#715).
	// Подстановка стоит ПОСЛЕ чеканки идентификатора (умолчание производно от
	// него) и ДО сборки строки — и она же снимает нужду в проверке «а не занято
	// ли»: идентификатор глобально уникален by construction, поэтому уникальность
	// имени остаётся за индексом БД, а не за чтением-перед-вставкой (ban #10).
	// Правится `in`, а не снятая выше копия `n`: в асинхронную часть уезжает `in`.
	in.NetworkInterface.Name = domain.RcNameVPC(
		corevalidate.NameOrDefault(string(in.NetworkInterface.Name), niID))
	n.Name = in.NetworkInterface.Name
	// Учёт числа ресурсов: ранний отказ ДО создания операции.
	//
	// Здесь же материализуются строки учёта, если проект их ещё не имеет, —
	// момент, когда владелец типа впервые узнаёт о проекте, и есть обращение к
	// нему. Отказ уходит арендатору синхронно тем же текстом и признаком, каким
	// его произвёл бы триггер: у обеих полос один производитель.
	if u.quota != nil {
		if err := u.quota.Admit(ctx, string(n.ProjectID), "vpc.networkInterface"); err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Create network interface %s", string(n.Name)),
		&vpcv1.CreateNetworkInterfaceMetadata{NetworkInterfaceId: niID},
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
		return u.doCreate(ctx, niID, in)
	})
	return &op, nil
}

// doCreate — async-часть Create (внутри Operation worker'а): project-exists +
// Subnet.Get, затем в writer-TX — validate+attach Address-refs (used + referrer) +
// Insert NIC + outbox + fga-register, с retry MAC-allocation на cloud-wide
// UNIQUE-collision.
//
// Attach(addresses) + Insert(NIC) + outbox-emit + fga-register идут в ОДНОЙ
// writer-TX (`w.Addresses()`), поэтому reservation и NIC коммитятся/откатываются
// атомарно — `w.Abort()` на любой ошибке снимает reservation, компенсация не нужна.
func (u *CreateNetworkInterfaceUseCase) doCreate(ctx context.Context, niID string, in CreateInput) (*anypb.Any, error) {
	n := in.NetworkInterface
	exists, err := u.projectClient.Exists(ctx, n.ProjectID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "project check: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "Project %s not found", n.ProjectID)
	}
	// Parent-Subnet check через CQRS-Reader (на slave-pool, если он настроен).
	// DB-backstop остается: FK `network_interfaces.subnet_id → subnets.id` ON
	// DELETE RESTRICT — если между Get и Insert'ом подсеть удалят, Insert упадет
	// с foreign_key_violation → `mapRepoErr` → FailedPrecondition.
	rd, rerr := u.repo.Reader(ctx)
	if rerr != nil {
		return nil, serviceerr.MapRepoErr(rerr)
	}
	parentSub, serr := rd.Subnets().Get(ctx, n.SubnetID)
	var parentNetDefaultSG string
	if serr == nil {
		// НАСЛЕДОВАНИЕ ГРУППЫ ПО УМОЛЧАНИЮ (обещание контракта, у которого не было
		// исполнителя).
		//
		// Комментарий поля `security_group_ids` обещает: «Дефолт при создании —
		// default_security_group_id сети (наследуется); можно переопределить». Кода,
		// который бы это делал, не существовало: интерфейс с пустым набором получал
		// пустой набор, а по закрытой в обе стороны модели это означает «не
		// разрешено ничего». То есть обещанное умолчание работало ПРОТИВОПОЛОЖНО
		// обещанию — и обнаруживалось это не отказом, а тишиной на трафике.
		//
		// Сеть читается в ТОЙ ЖЕ Reader-TX, что и подсеть: отдельная транзакция
		// давала бы второй снимок, и между ними сеть могла бы сменить группу.
		if net, nerr := rd.Networks().Get(ctx, parentSub.NetworkID); nerr == nil {
			parentNetDefaultSG = net.DefaultSecurityGroupID
		}
	}
	_ = rd.Close()
	if serr != nil {
		return nil, serviceerr.MapRepoErr(serr)
	}
	// BOLA-guard: parent Subnet обязана принадлежать проекту вызывающего — иначе
	// NIC создавался бы в чужой подсети (cross-project reference). Ответ — тот же
	// NotFound, что для несуществующего subnet.
	//
	// Текст выписан по владельцу подсети (`repo/kacho/pg/subnet.go` — `"%w: Subnet %s
	// not found"`), а не собран `serviceerr.MapRepoErr(repo.ErrNotFound)`, как стояло
	// прежде: голый sentinel даёт «not found» БЕЗ имени и идентификатора, то есть
	// отказ читался ОТЛИЧИМО от настоящего промаха — а различимый текст и есть тот
	// оракул существования, который скрытие должно было закрыть. Прежний комментарий
	// заявлял неотличимость, которой в коде не было.
	if parentSub.ProjectID != n.ProjectID {
		return nil, status.Errorf(codes.NotFound, "Subnet %s not found", n.SubnetID)
	}
	st := domain.NIStatusAvailable
	usedByType, usedByID := "", ""
	rec := &domain.NetworkInterface{
		ID:               niID,
		ProjectID:        n.ProjectID,
		Name:             n.Name,
		Description:      n.Description,
		Labels:           n.Labels,
		SubnetID:         n.SubnetID,
		V4AddressIDs:     n.V4AddressIDs,
		V6AddressIDs:     n.V6AddressIDs,
		SecurityGroupIDs: inheritedSecurityGroups(n.SecurityGroupIDs, parentNetDefaultSG),
		UsedByType:       usedByType,
		UsedByID:         usedByID,
		Status:           st,

		BandwidthLimitMbps: n.BandwidthLimitMbps,
	}
	// MAC аллоцируется здесь и больше не меняется на протяжении жизни NIC.
	// При cloud-wide UNIQUE-collision генерируем новый MAC и повторяем Insert.
	// Каждая попытка — отдельная writer-TX (CAS-конфликт на MAC требует start-over).
	//
	// Address-attach (validate + SetReference на addresses) идёт в ТОЙ ЖЕ writer-TX,
	// что и Insert(NIC) + outbox + fga-register — всё коммитится/откатывается атомарно
	// (`w.Abort()` на любой ошибке снимает reservation). Так исключается orphan
	// used=true без persisted NIC при краше worker'а (project-rule #10/#11). На
	// mac-collision retry attach просто переигрывается в свежей TX (после Abort
	// адрес снова свободен). Attach-ошибка (InvalidArgument/FailedPrecondition) —
	// НЕ retry: Abort + возврат сразу.
	for attempt := 0; attempt < niMacRetryAttempts; attempt++ {
		mac, merr := macutil.GenerateMAC()
		if merr != nil {
			return nil, status.Errorf(codes.Internal, "generate mac: %v", merr)
		}
		rec.MAC = mac

		w, werr := u.repo.Writer(ctx)
		if werr != nil {
			return nil, serviceerr.MapRepoErr(werr)
		}
		// Группы безопасности — ссылка от вызывающего без внешнего ключа:
		// существование и принадлежность проверяются В ЭТОЙ ЖЕ writer-TX, где
		// пишется интерфейс, и с share-lock'ом на строках групп (writer-сторона
		// GetMany). Иначе группу можно было удалить между проверкой и коммитом:
		// её предусловие удаления нашего интерфейса ещё не видит, и ссылка
		// оставалась бы висячей.
		if sgErr := validateNICSecurityGroupRefs(ctx, w.SecurityGroups(), n.SecurityGroupIDs,
			string(n.ProjectID), parentSub.NetworkID); sgErr != nil {
			w.Abort()
			return nil, sgErr
		}
		// Validate + attach address-refs в этой writer-TX (используем w.Addresses(),
		// а не отдельный addressRepo). Ошибка attach — не MAC-collision → Abort + return.
		if aerr := attachNICAddresses(ctx, w.Addresses(), niID, string(n.Name), n.ProjectID, n.SubnetID, n.V4AddressIDs, n.V6AddressIDs); aerr != nil {
			w.Abort()
			return nil, aerr
		}
		created, insertErr := w.NetworkInterfaces().Insert(ctx, rec)
		if insertErr != nil {
			w.Abort()
			if errors.Is(insertErr, repo.ErrMacCollision) {
				continue // retry с новым MAC (attach переиграется в свежей TX)
			}
			return nil, serviceerr.MapRepoErr(insertErr)
		}
		if oerr := w.Outbox().Emit(ctx, "NetworkInterface", created.ID, created.ProjectID, "CREATED", helpers.DomainToMap(created)); oerr != nil {
			w.Abort()
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, oerr))
		}
		// Публикуем intent на owner-hierarchy-tuple vpc_network_interface→project
		// в той же writer-TX — чтобы он не терялся при ошибке после commit. В
		// mirror-feed несем labels NIC + parent_project_id (ProjectHierarchyItem),
		// а не голый tuple — иначе resource_mirror в kaname остается без labels и
		// ARM_LABELS-селектор не матчит даже свежесозданный NIC. Симметрично
		// network/subnet/securitygroup create.
		items := []fgaregister.Item{
			fgaregister.ProjectHierarchyItem(string(n.ProjectID), "vpc_network_interface", created.ID,
				domain.LabelsToMap(created.Labels)),
		}
		// Версия intent'а из writer-TX — её же понесёт синхронная регистрация ниже
		// (одна версия на обе доставки ⇒ повтор гасится в любом порядке).
		intentVersion, rerr := w.FGARegister().EmitRegister(ctx, fgaregister.RegisterItems(items...))
		if rerr != nil {
			w.Abort()
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga register intent: %v", repo.ErrInternal, rerr))
		}
		if cerr := w.Commit(); cerr != nil {
			// Commit не прошёл → address-reservation откатилась вместе с TX
			// (attach был в этой же writer-TX). Компенсация не нужна.
			return nil, serviceerr.MapRepoErr(cerr)
		}
		// Sync-primary owner-tuple registration идёт ПОСЛЕ durable commit: она
		// сокращает окно видимости гранта, но НЕ является условием успеха мутации.
		// NIC уже закоммичен и валиден (attach и address-reservation закоммичены
		// вместе с ним), а intent на тот же tuple лежит в fga_register_outbox той
		// же writer-TX → at-least-once дренаж доведёт грант сам. Провалить операцию
		// здесь значило бы отдать вызывающему код узла прав (status.FromError
		// достаёт вложенный статус и подменяет сообщение всей цепочкой) на уже
		// созданный NIC — фантом. Поэтому предупреждение, а не ошибка.
		fgaregister.DeliverAfterCommit(ctx, u.registrar, items, intentVersion, "NetworkInterface", created.ID)
		return marshalNetworkInterfaceRecord(created)
	}
	// Все попытки исчерпаны. Последняя attach-TX уже откачена (`w.Abort()` на
	// mac-collision) — reservation не осталась, компенсация не нужна.
	return nil, status.Errorf(codes.Internal, "could not allocate unique MAC after %d attempts", niMacRetryAttempts)
}

// nicAddressMiss — ЕДИНСТВЕННЫЙ ответ на «названный адрес не резолвится как СВОЙ»:
// строки нет вовсе либо она принадлежит другому проекту. Форма выписана здесь по
// владельцу (`internal/repo/kacho/pg/address.go` — `"%w: Address %s not found"` под
// `ErrNotFound`), а не собрана из внутренних имён: отказ обязан читаться ПОБАЙТОВО
// как настоящий промах владельца, иначе по различию текстов устанавливают, что
// чужой адрес существует.
//
// Код — NotFound: Address принадлежит vpc, значит это полоса direct-read
// («не нашёл СВОЁ»), а не peer-validate (api-conventions.md §By-lane code-split).
func nicAddressMiss(id string) error {
	return status.Errorf(codes.NotFound, "Address %s not found", id)
}

// validateNICAddressRef — годна ли названная ссылка на адрес для ЭТОГО интерфейса.
// Свободная функция поверх любого AddressRepo (в т.ч. `w.Addresses()` writer-TX) —
// общая для Create и Update. При нарушении возвращает gRPC-status.
//
// # Порядок проверок несущий, а не косметический
//
// форма → принадлежность проекту → существование → состояние.
//
//  1. **Форма** (`validateNICAddressRefID`) — до любого чтения. Иначе явный мусор
//     («zzz») получает контракт-тон ОТСУТСТВИЯ РЕСУРСА на строку, которая адресом
//     быть не может, а пустая строка — тот же тон с вырезанным id.
//  2. **Принадлежность и существование — ОДНА полоса ответа.** Адрес чужого проекта
//     и отсутствующий адрес отвечают одним и тем же `nicAddressMiss(id)`: различие
//     между ними и есть оракул существования. Технически принадлежность узнаётся
//     только чтением строки, поэтому в коде чтение стоит раньше сверки — но НАРУЖУ
//     обе ветви неразличимы, и именно это утверждает проба.
//  3. **Состояние** (семейство, подсеть, занятость) — только для адреса СВОЕГО
//     проекта, и здесь различие исходов законно и полезно: это диагностика по
//     собственному объекту. Прежняя редакция отвечала этими же четырьмя текстами
//     на ЛЮБОЙ адрес облака, то есть сообщала посторонним семейство чужого адреса,
//     идентификатор его подсети и его занятость. Текст «уже занят» сохранён именно
//     потому, что теперь он касается только своего адреса: скрыть его значило бы
//     заставить владельца гадать, почему его собственный свободный на вид адрес не
//     привязывается.
func validateNICAddressRef(ctx context.Context, ar AddressRepo, id, nicProject, nicSubnet string, want domain.IpVersion) error {
	if err := validateNICAddressRefID(want, id); err != nil {
		return err
	}
	a, err := ar.Get(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nicAddressMiss(id)
		}
		return serviceerr.MapRepoErr(err)
	}
	if a.ProjectID != nicProject {
		return nicAddressMiss(id)
	}
	switch want {
	case domain.IpVersionIPv4:
		if a.Type != domain.AddressTypeInternal || a.InternalIpv4 == nil {
			return status.Errorf(codes.InvalidArgument, "address %s is not an internal IPv4 address", id)
		}
		if a.InternalIpv4.SubnetID != nicSubnet {
			return status.Errorf(codes.InvalidArgument, "address %s belongs to subnet %s, not %s", id, a.InternalIpv4.SubnetID, nicSubnet)
		}
	case domain.IpVersionIPv6:
		if a.IpVersion != domain.IpVersionIPv6 || a.InternalIpv6 == nil {
			return status.Errorf(codes.InvalidArgument, "address %s is not an internal IPv6 address", id)
		}
		if a.InternalIpv6.SubnetID != nicSubnet {
			return status.Errorf(codes.InvalidArgument, "address %s belongs to subnet %s, not %s", id, a.InternalIpv6.SubnetID, nicSubnet)
		}
	}
	if a.Used {
		return status.Errorf(codes.FailedPrecondition, "address %s is already in use", id)
	}
	return nil
}

// attachNICAddresses — валидирует и помечает used=true + referrer для каждого
// v4/v6 address id поверх любого AddressRepo (в т.ч. `w.Addresses()` writer-TX).
// На ошибке НЕ компенсирует — это решает caller (writer-TX Abort у Update;
// detachNICAddresses у Create). Общая для Create/Update (убрана дупликация).
//
// `nicProject` — проект ИНТЕРФЕЙСА, а не присланное вызывающим значение: у Create
// это проверенный проект запроса, у Update — проект уже существующей строки. Адрес
// другого проекта отвергается как отсутствующий (см. `validateNICAddressRef`).
func attachNICAddresses(ctx context.Context, ar AddressRepo, nicID, nicName, nicProject, nicSubnet string, v4IDs, v6IDs []string) error {
	for _, id := range v4IDs {
		if err := validateNICAddressRef(ctx, ar, id, nicProject, nicSubnet, domain.IpVersionIPv4); err != nil {
			return err
		}
	}
	for _, id := range v6IDs {
		if err := validateNICAddressRef(ctx, ar, id, nicProject, nicSubnet, domain.IpVersionIPv6); err != nil {
			return err
		}
	}
	for _, id := range append(append([]string{}, v4IDs...), v6IDs...) {
		ref := &domain.AddressReference{AddressID: id, ReferrerType: niReferrerType, ReferrerID: nicID, ReferrerName: nicName}
		if _, err := ar.SetReference(ctx, ref); err != nil {
			return serviceerr.MapRepoErr(err)
		}
	}
	return nil
}

// detachNICAddresses — снимает used + referrer-row с каждого address id поверх
// любого AddressRepo. ErrNotFound терпим (адрес мог быть удален), остальное
// возвращается. Общая для Create (best-effort, ошибки caller игнорирует) и
// Update (в writer-TX — ошибка откатывает весь diff).
func detachNICAddresses(ctx context.Context, ar AddressRepo, ids []string) error {
	for _, id := range ids {
		if err := ar.ClearReference(ctx, id); err != nil && !errors.Is(err, repo.ErrNotFound) {
			return serviceerr.MapRepoErr(err)
		}
	}
	return nil
}

// inheritedSecurityGroups — набор групп интерфейса: явный выбор вызывающего сильнее,
// пустой набор наследует группу по умолчанию своей сети.
//
// Почему помощник, а не выражение на месте: свойство «пустое означает наследование, а
// не пустоту» — часть контракта, и оно обязано быть названо один раз. Выражение на
// месте пришлось бы повторить в `Update`, где действует то же правило, и две копии
// разошлись бы на первой же правке.
//
// Пустая группа по умолчанию (сеть заведена до того, как её создание стало
// безусловным) наследования не даёт: подставлять пустую строку в набор ссылок значило
// бы завести висячую ссылку вместо отсутствия.
func inheritedSecurityGroups(explicit []string, networkDefault string) []string {
	if len(explicit) > 0 {
		return explicit
	}
	if networkDefault == "" {
		return explicit
	}
	return []string{networkDefault}
}

// WithQuotaGuard подключает совещательную полосу учёта.
//
// Отдельным глаголом, а не аргументом конструктора: полоса появилась позже
// вызывающих, и обязательный аргумент заставил бы править каждую сборку — в том
// числе те, где соседа с величинами нет вовсе.
func (u *CreateNetworkInterfaceUseCase) WithQuotaGuard(g QuotaGuard) *CreateNetworkInterfaceUseCase {
	u.quota = g
	return u
}
