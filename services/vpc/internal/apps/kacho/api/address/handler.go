// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/applystate"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/pbconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"

	// Blank-import регистрирует Address/time DTO-трансферы через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
)

// Handler — реализация vpcv1.AddressServiceServer на основе use-case'ов.
// Тонкий transport-слой: proto-request → domain → use-case → proto-response.
// Никакой бизнес-логики.
type Handler struct {
	vpcv1.UnimplementedAddressServiceServer

	create         *CreateAddressUseCase
	update         *UpdateAddressUseCase
	delete         *DeleteAddressUseCase
	get            *GetAddressUseCase
	getByValue     *GetByValueUseCase
	list           *ListAddressesUseCase
	listBySubnet   *ListBySubnetUseCase
	listOperations *ListOperationsUseCase
	// applyState — заполнитель публичного поля состояния применения.
	// Провязывается композиционным корнем; нулевое значение означает
	// «утверждения нет» и к базе не ходит.
	applyState *applystate.Filler
}

// NewHandler собирает Handler из готовых use-case'ов. Конструктор намеренно
// принимает все use-case'ы — composition-root (cmd/vpc/main.go) собирает их
// с одинаковыми зависимостями (repo / subnetReader / projectClient / opsRepo /
// pools).
func NewHandler(
	create *CreateAddressUseCase,
	update *UpdateAddressUseCase,
	deleteUC *DeleteAddressUseCase,
	get *GetAddressUseCase,
	getByValue *GetByValueUseCase,
	list *ListAddressesUseCase,
	listBySubnet *ListBySubnetUseCase,
	listOps *ListOperationsUseCase,
) *Handler {
	return &Handler{
		create:         create,
		update:         update,
		delete:         deleteUC,
		get:            get,
		getByValue:     getByValue,
		list:           list,
		listBySubnet:   listBySubnet,
		listOperations: listOps,
	}
}

// WithApplyState провязывает заполнитель состояния применения.
//
// Отдельным методом, а не аргументом конструктора: у семи ресурсов
// конструкторы разной формы, и добавление восьмого позиционного аргумента
// сделало бы каждую их правку правкой всех вызывающих. Провязку боевой
// сборки держит гейт по дереву — он же ловит забытый вызов.
func (h *Handler) WithApplyState(f *applystate.Filler) *Handler {
	h.applyState = f
	return h
}

// Get — sync read. Per-object AuthZ (включая existence-hiding на deny) энфорсит
// per-RPC authz-interceptor прямым Check'ом — см. GetAddressUseCase.
func (h *Handler) Get(ctx context.Context, req *vpcv1.GetAddressRequest) (*vpcv1.Address, error) {
	if req.AddressId == "" {
		return nil, status.Error(codes.InvalidArgument, "address_id required")
	}
	a, err := h.get.Execute(ctx, req.AddressId)
	if err != nil {
		return nil, err
	}
	pb, err := addressToPb(a)
	if err != nil {
		return nil, err
	}
	// Состояние применения — ОТДЕЛЬНЫМ вопросом к проекции подтверждений, а не
	// полем строки ресурса: оно выводится сравнением ревизий и живёт в другой
	// таблице. Незаполненное поле означает «утверждения нет».
	if pb.ApplyState, err = h.applyState.One(ctx, pb.GetId()); err != nil {
		return nil, err
	}
	return pb, nil
}

// List — project_id required + FGA list-filter. Project-scope AuthZ (`viewer @
// project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) List(ctx context.Context, req *vpcv1.ListAddressesRequest) (*vpcv1.ListAddressesResponse, error) {
	addrs, nextToken, err := h.list.Execute(ctx, AddressFilter{
		ProjectID: req.ProjectId,
		Filter:    req.Filter,
		SubnetID:  req.SubnetId,
		// Сужение по значению — замена снятому поиску по значению. Область запроса
		// берётся из `project_id`, который вызывающий и так обязан назвать, а страница
		// проверяется по правам построчно: у снятого метода внешняя ветвь была
		// неавторизуема по построению, потому что область бралась из подсети, которой
		// у внешнего адреса нет.
		IPAddress: req.IpAddress,
	}, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListAddressesResponse{NextPageToken: nextToken}
	for _, a := range addrs {
		pb, err := addressToPb(a)
		if err != nil {
			return nil, err
		}
		resp.Addresses = append(resp.Addresses, pb)
	}
	// Состояние применения СТРАНИЦЫ — одним обращением к проекции: стоимость
	// принадлежит запросу, а не популяции проекта. Спрашивается ПОСЛЕ того, как
	// страница отобрана и сужена правами, то есть по идентификаторам, которые
	// вызывающий и так увидит.
	if ferr := applystate.FillPage(ctx, h.applyState, resp.Addresses,
		func(p *vpcv1.Address) string { return p.GetId() },
		func(p *vpcv1.Address, st *vpcv1.ApplyState) { p.ApplyState = st },
	); ferr != nil {
		return nil, ferr
	}
	return resp, nil
}

// Create — proto → CreateInput → use-case. Project-scope AuthZ (`editor @
// project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) Create(ctx context.Context, req *vpcv1.CreateAddressRequest) (*operationpb.Operation, error) {
	op, err := h.create.Execute(ctx, createInputFromProto(req))
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// CreateOwnedAddress — internal-путь: то же создание, что и публичное, плюс
// владелец, к которому адрес привязывается ТОЙ ЖЕ writer-TX (см. контракт RPC в
// `internal_address_service.proto`). Тело создания разбирается ОДНОЙ функцией с
// публичным путём — иначе внутренний путь тихо разошёлся бы с публичным на
// первом же новом поле спецификации адреса.
//
// Право проверяет тот же per-RPC Check, что и у публичного Create (`editor` на
// project'е из `address.project_id`) — см. `check.PermissionMap`.
func (h *Handler) CreateOwnedAddress(ctx context.Context, req *vpcv1.CreateOwnedAddressRequest) (*operationpb.Operation, error) {
	if req.GetAddress() == nil {
		return nil, status.Error(codes.InvalidArgument, "address: required")
	}
	if req.GetReferrerType() == "" {
		return nil, status.Error(codes.InvalidArgument, "referrer_type: required")
	}
	if req.GetReferrerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "referrer_id: required")
	}
	in := createInputFromProto(req.GetAddress())
	in.Owner = &AddressOwner{
		ReferrerType: req.GetReferrerType(),
		ReferrerID:   req.GetReferrerId(),
		ReferrerName: req.GetReferrerName(),
		Owned:        req.GetOwned(),
	}
	op, err := h.create.Execute(ctx, in)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// createInputFromProto — единственный разбор тела создания адреса; общий для
// публичного `Create` и внутреннего `CreateOwnedAddress`.
func createInputFromProto(req *vpcv1.CreateAddressRequest) CreateInput {
	in := CreateInput{
		ProjectID:          req.ProjectId,
		Name:               req.Name,
		Description:        req.Description,
		Labels:             req.Labels,
		DeletionProtection: req.DeletionProtection,
	}

	if ext := req.GetExternalIpv4AddressSpec(); ext != nil {
		in.ExternalSpec = &ExternalAddrSpec{
			Address: ext.Address,
			ZoneID:  ext.ZoneId,
		}
	} else if intSpec := req.GetInternalIpv4AddressSpec(); intSpec != nil {
		in.InternalSpec = &InternalAddrSpec{
			Address:  intSpec.Address,
			SubnetID: intSpec.GetSubnetId(),
		}
	} else if int6Spec := req.GetInternalIpv6AddressSpec(); int6Spec != nil {
		in.InternalIpv6Spec = &InternalAddrSpec{
			Address:  int6Spec.Address,
			SubnetID: int6Spec.GetSubnetId(),
		}
	} else if ext6 := req.GetExternalIpv6AddressSpec(); ext6 != nil {
		// external IPv6 address.
		in.ExternalIpv6Spec = &ExternalAddrSpec{
			Address: ext6.Address,
			ZoneID:  ext6.ZoneId,
		}
	}

	return in
}

// Update — sync repo.Get (existence → NotFound) + use-case. Per-object AuthZ
// энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) Update(ctx context.Context, req *vpcv1.UpdateAddressRequest) (*operationpb.Operation, error) {
	if req.AddressId == "" {
		return nil, status.Error(codes.InvalidArgument, "address_id required")
	}
	if _, err := h.get.Execute(ctx, req.AddressId); err != nil {
		return nil, err
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	op, err := h.update.Execute(ctx, UpdateInput{
		AddressID:          req.AddressId,
		Name:               req.Name,
		Description:        req.Description,
		Labels:             req.Labels,
		DeletionProtection: req.DeletionProtection,
		Reserved:           req.Reserved,
		UpdateMask:         mask,
	})
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// ListOperations — best-effort existence-probe: ресурс удален (NotFound от get)
// → пропускаем (история операций должна оставаться доступной), прочая ошибка
// возвращается. Per-object AuthZ энфорсит per-RPC authz-interceptor.
func (h *Handler) ListOperations(ctx context.Context, req *vpcv1.ListAddressOperationsRequest) (*vpcv1.ListAddressOperationsResponse, error) {
	if req.AddressId == "" {
		return nil, status.Error(codes.InvalidArgument, "address_id required")
	}
	if _, gerr := h.get.Execute(ctx, req.AddressId); gerr != nil && status.Code(gerr) != codes.NotFound {
		return nil, gerr
	}
	ops, nextToken, err := h.listOperations.Execute(ctx, req.AddressId, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListAddressOperationsResponse{NextPageToken: nextToken}
	for i := range ops {
		resp.Operations = append(resp.Operations, pbconv.OperationToProto(&ops[i]))
	}
	return resp, nil
}

// Delete — sync repo.Get (existence → NotFound), затем use-case. Per-object
// AuthZ энфорсит per-RPC authz-interceptor прямым Check'ом.
func (h *Handler) Delete(ctx context.Context, req *vpcv1.DeleteAddressRequest) (*operationpb.Operation, error) {
	if req.AddressId == "" {
		return nil, status.Error(codes.InvalidArgument, "address_id required")
	}
	if _, err := h.get.Execute(ctx, req.AddressId); err != nil {
		return nil, err
	}
	op, err := h.delete.Execute(ctx, req.AddressId)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// addressToPb — repo-entity Address → proto Address через DTO-реестр.
func addressToPb(rec *kachorepo.AddressRecord) (*vpcv1.Address, error) {
	var dst *vpcv1.Address
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, status.Error(codes.Internal, "dto.Transfer Address failed")
	}
	return dst, nil
}
