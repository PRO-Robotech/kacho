// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
)

// # Предмет
//
// Полоса освобождения аренды НЕ ВПРАВЕ выводить «работа сделана» из кода ошибки
// владельца. `NOT_FOUND` на пообъектной полосе не несёт утверждения «аренды
// нет», и приходит он там по нескольким разным причинам НАМЕРЕННО: тем же
// ответом владелец отвечает на промах чужого проекта (иначе ответ стал бы
// оракулом существования чужих ресурсов), и в тот же ответ схлопывается опрос
// операции — «строки нет», «владелец другой» и «у тебя нет ключа владельца»
// неразличимы by design. Правило верное и не трогается. Полоса читала этот
// ответ как доказательство снятой аренды и строила на нём необратимый шаг:
// снос строки потребителя, после которого координаты аренды не остаётся ни у
// кого — реконсайлер ищет ОТ строки потребителя, а обратной развёртки у
// владельца нет.
//
// # Что здесь утверждается
//
// Инвариант формулируется по НАБЛЮДАЕМОМУ состоянию аренды, а не по факту
// вызова:
//
//	успех полосы освобождения ⇒ владелец аренду больше не держит.
//
// Проба, утверждающая «вызвали ClearReference и FreeIP», осталась бы зелёной на
// дефекте: вызовы происходили, ответы приходили, аренда оставалась.
//
// # Почему дублёр владельца отвечает именно так
//
// `leaseLedgerVPC` — не заглушка, а модель ЛЕДЖЕРА владельца: он держит аренду и
// отвечает ровно теми кодами, которыми на такой вход отвечает боевой vpc.
// Различие кодов между полосами — не украшение фикстуры, а следствие ЯКОРЯ права:
//
//   - пообъектные глаголы (`ClearAddressReference`, публичный `AddressService.Delete`)
//     анкорены на самом адресе → недостижимый объект отвечает `NOT_FOUND` с
//     контрактным текстом владельца;
//   - глагол, анкоренный на проекте, пообъектной пробы существования не запускает
//     вовсе → отказ приходит как `PERMISSION_DENIED`.
//
// Оба ответа означают одно: РАБОТА НЕ СДЕЛАНА. Проба не различает их и не обязана
// — она требует, чтобы полоса не называла успехом ни один из них.
//
// # Цена ошибки несимметрична, и сторона выбрана осознанно
//
// Здесь fail-closed означает НЕ ОСВОБОЖДАТЬ. Ошибка в эту сторону оставляет
// аренду занятой: пул истощается, это заметно и подбирается реконсайлером, пока
// строка потребителя цела. Ошибка в другую сторону вернула бы в свободный список
// адрес, который у владельца всё ещё занят, — и второй арендатор получил бы
// адрес, работающий у первого. Первое чинится, второе — пересечение арендаторов.
func TestDelete_ReleaseLaneSuccessImpliesOwnerNoLongerHoldsTheLease(t *testing.T) {
	t.Parallel()

	ledger := newLeaseLedgerVPC()
	repo := newFakeRepo()
	opsRepo := newFakeOpsRepo()
	uc := NewDeleteLoadBalancerUseCase(repo, opsRepo, ledger.client(t), slog.Default())

	id := seedLB(t, repo, "prj-a", "edge-truthful")
	rec := repo.lbs[id]
	rec.IPFamilies = []domain.IPVersion{domain.IPVersionV4}
	rec.AddressV4 = domain.IPAddress("10.0.0.7")
	rec.AddressIDV4 = domain.AddressID("adr-held-1")
	rec.VipOriginV4 = domain.VipOriginAuto

	// Аренда заводится ТЕМ ЖЕ владением, каким её потом предъявят к снятию:
	// иначе проба утверждала бы о несуществующей аренде и зеленела бы на любой
	// реализации.
	ledger.hold("adr-held-1", vpcclient.OwnerKindLoadBalancer, id, true)
	// Владелец не намерен раскрывать этому вызывающему существование адреса.
	ledger.denyCaller = true

	op, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: id,
	})
	require.NoError(t, err, "sync-часть Delete обязана пройти: предмет пробы — асинхронная полоса освобождения")

	done := awaitOpDone(t, opsRepo, op.ID)

	// НЕСУЩЕЕ УТВЕРЖДЕНИЕ: успех обязан означать снятую аренду.
	if done.Error == nil {
		require.False(t, ledger.stillHeld("adr-held-1"),
			"полоса освобождения отчиталась успехом, а владелец аренду всё ещё держит: "+
				"ответ, не доказывающий отсутствия аренды, принят за доказательство её отсутствия")
	}

	// Обратная сторона того же инварианта — fail-closed НЕ ОСВОБОЖДАТЬ: аренда
	// не должна быть потеряна, то есть координата обязана пережить отказ.
	require.True(t, ledger.stillHeld("adr-held-1"),
		"аренда исчезла из леджера владельца — освобождать её было нечем и незачем")
	require.Contains(t, repo.lbs, id,
		"строка потребителя снесена при неснятой аренде: координаты аренды не осталось ни у кого")
}

// Положительный контроль к пробе выше.
//
// Без него отрицание зеленело бы на полосе, которая не работает НИКОГДА: «успех
// ⇒ аренда снята» тождественно истинно, если успеха не бывает.
func TestDelete_ReleaseLaneReturnsTheLeaseWhenTheOwnerActuallyReleasesIt(t *testing.T) {
	t.Parallel()

	ledger := newLeaseLedgerVPC()
	repo := newFakeRepo()
	opsRepo := newFakeOpsRepo()
	uc := NewDeleteLoadBalancerUseCase(repo, opsRepo, ledger.client(t), slog.Default())

	id := seedLB(t, repo, "prj-a", "edge-positive")
	rec := repo.lbs[id]
	rec.IPFamilies = []domain.IPVersion{domain.IPVersionV4}
	rec.AddressV4 = domain.IPAddress("10.0.0.8")
	rec.AddressIDV4 = domain.AddressID("adr-held-2")
	rec.VipOriginV4 = domain.VipOriginAuto

	ledger.hold("adr-held-2", vpcclient.OwnerKindLoadBalancer, id, true)

	op, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: id,
	})
	require.NoError(t, err)

	done := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, done.Error, "владелец снял аренду — операция обязана завершиться успехом")
	require.False(t, ledger.stillHeld("adr-held-2"), "аренда обязана быть снята у владельца")
	require.NotContains(t, repo.lbs, id, "строка потребителя обязана быть снесена после снятой аренды")
}

// ---------------------------------------------------------------------------
// leaseLedgerVPC — дублёр ВЛАДЕЛЬЦА аренды.
//
// Он не снисходительнее продукта: пустой идентификатор отвергает, чужую пару
// (referrer_type, referrer_id) не снимает, и на недостижимом для вызывающего
// объекте отвечает ровно тем кодом, каким отвечает боевой владелец на
// соответствующей полосе.
// ---------------------------------------------------------------------------

type heldLease struct {
	referrerType string
	referrerID   string
	owned        bool
}

// doneOperations — дублёр опроса Operation: у владельца асинхронный шаг уже
// завершён, поэтому опрос ничего не решает и вынесен отдельным типом (у
// AddressService и OperationService совпадают имена методов `Get`).
type doneOperations struct {
	operationpb.UnimplementedOperationServiceServer
}

func (doneOperations) Get(
	_ context.Context, req *operationpb.GetOperationRequest,
) (*operationpb.Operation, error) {
	return &operationpb.Operation{Id: req.GetOperationId(), Done: true, CreatedAt: timestamppb.Now()}, nil
}

type leaseLedgerVPC struct {
	vpcpb.UnimplementedInternalAddressServiceServer
	vpcpb.UnimplementedAddressServiceServer

	mu     sync.Mutex
	leases map[string]heldLease
	// denyCaller — владелец не намерен раскрывать вызывающему существование
	// объекта. Именно этот вход и есть предмет проб выше.
	denyCaller bool
}

func newLeaseLedgerVPC() *leaseLedgerVPC {
	return &leaseLedgerVPC{leases: map[string]heldLease{}}
}

func (f *leaseLedgerVPC) hold(addressID, referrerType, referrerID string, owned bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leases[addressID] = heldLease{referrerType: referrerType, referrerID: referrerID, owned: owned}
}

func (f *leaseLedgerVPC) stillHeld(addressID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.leases[addressID]
	return ok
}

// client поднимает дублёра на loopback и отдаёт БОЕВОЙ клиент поверх него.
// Подменять сам клиент здесь нельзя: предмет проб живёт именно в нём — в том,
// как он читает ответ владельца.
func (f *leaseLedgerVPC) client(t *testing.T) InternalAddressClient {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	vpcpb.RegisterInternalAddressServiceServer(srv, f)
	vpcpb.RegisterAddressServiceServer(srv, f)
	operationpb.RegisterOperationServiceServer(srv, doneOperations{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.GracefulStop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return vpcclient.NewInternalAddressClient(conn, conn)
}

// hideExistence — контрактный тон владельца на пообъектной полосе. Текст
// побайтово тот же, что у настоящего промаха: в этом и смысл скрытия.
func hideExistence(addressID string) error {
	return status.Errorf(codes.NotFound, "Address %s not found", addressID)
}

// ClearAddressReference — пообъектная полоса: анкорена на самом адресе.
func (f *leaseLedgerVPC) ClearAddressReference(
	_ context.Context, req *vpcpb.ClearAddressReferenceRequest,
) (*vpcpb.ClearAddressReferenceResponse, error) {
	if req.GetAddressId() == "" {
		return nil, status.Error(codes.InvalidArgument, "address_id: required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.denyCaller {
		return nil, hideExistence(req.GetAddressId())
	}
	delete(f.leases, req.GetAddressId())
	return &vpcpb.ClearAddressReferenceResponse{}, nil
}

// ReleaseOwnedAddress — полоса, анкоренная на ПРОЕКТЕ. Пообъектной пробы
// существования у неё нет, поэтому и полосы скрытия нет: отказ приходит
// отказом, а исход — полем.
func (f *leaseLedgerVPC) ReleaseOwnedAddress(
	_ context.Context, req *vpcpb.ReleaseOwnedAddressRequest,
) (*vpcpb.ReleaseOwnedAddressResponse, error) {
	switch {
	case req.GetProjectId() == "":
		return nil, status.Error(codes.InvalidArgument, "project_id: required")
	case req.GetAddressId() == "":
		return nil, status.Error(codes.InvalidArgument, "address_id: required")
	case req.GetReferrerType() == "" || req.GetReferrerId() == "":
		return nil, status.Error(codes.InvalidArgument, "referrer: required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.denyCaller {
		return nil, status.Error(codes.PermissionDenied, "no path")
	}
	held, ok := f.leases[req.GetAddressId()]
	if !ok {
		return &vpcpb.ReleaseOwnedAddressResponse{
			Outcome: vpcpb.ReleaseOwnedAddressResponse_ALREADY_RELEASED,
		}, nil
	}
	if held.referrerType != req.GetReferrerType() || held.referrerID != req.GetReferrerId() {
		return nil, status.Errorf(codes.FailedPrecondition,
			"address %s is not leased by %s %s",
			req.GetAddressId(), req.GetReferrerType(), req.GetReferrerId())
	}
	delete(f.leases, req.GetAddressId())
	out := vpcpb.ReleaseOwnedAddressResponse_DETACHED
	if held.owned {
		out = vpcpb.ReleaseOwnedAddressResponse_RELEASED
	}
	return &vpcpb.ReleaseOwnedAddressResponse{Outcome: out}, nil
}

// Delete — публичная полоса: тоже анкорена на самом адресе.
func (f *leaseLedgerVPC) Delete(
	_ context.Context, req *vpcpb.DeleteAddressRequest,
) (*operationpb.Operation, error) {
	if req.GetAddressId() == "" {
		return nil, status.Error(codes.InvalidArgument, "address_id: required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.denyCaller {
		return nil, hideExistence(req.GetAddressId())
	}
	delete(f.leases, req.GetAddressId())
	return &operationpb.Operation{Id: "opv-" + req.GetAddressId(), Done: true, CreatedAt: timestamppb.Now()}, nil
}
