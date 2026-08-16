// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// TestInternalAddressClient_PropagatesPrincipal — регрессия: worker-вызовы vpc
// обязаны нести principal тенанта в outgoing-metadata (auth.PropagateOutgoing),
// иначе vpc authz отвергает как authz_no_principal. Проверяем, что SetReference,
// вызванный с ctx-principal'ом, доносит x-kacho-principal-* до сервера.
func TestInternalAddressClient_PropagatesPrincipal(t *testing.T) {
	internalAddrs := &fakeInternalAddressService{}
	conn := startFakeVPC(t, nil, nil, nil, internalAddrs, nil)
	client := NewInternalAddressClient(conn, conn)

	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr-regress", DisplayName: "Regress User"})
	err := client.SetReference(ctx, "adr-1",
		AddressOwner{Kind: "network_load_balancer", ID: "nlb-1"}, true)
	require.NoError(t, err)

	got := internalAddrs.lastSetMD.Get(auth.MDKeyPrincipalID)
	require.Len(t, got, 1, "principal id must reach vpc via outgoing metadata")
	assert.Equal(t, "usr-regress", got[0])
	assert.Equal(t, []string{"user"}, internalAddrs.lastSetMD.Get(auth.MDKeyPrincipalType))
	assert.True(t, internalAddrs.setCalls[0].GetOwned(), "owned=true пробрасывается")
}

// fakeAddressForAlloc реализует AddressService.{Create,Delete}.
// Возвращает done=true Operation с inline Address response (для теста auto-alloc).
type fakeAddressForAlloc struct {
	vpcpb.UnimplementedAddressServiceServer

	mu sync.Mutex

	createResp *vpcpb.Address // что положить в Operation.response
	createErr  error          // ошибка на сам Create call

	deleteErr      error
	deleteNotFound bool // если true — Delete возвращает NotFound

	// deleteErrTimes>0 → deleteErr отдаётся только на первых N вызовах Delete,
	// дальше вызов проходит (модель закрывающегося окна материализации).
	// 0 (по умолчанию) — прежнее поведение: deleteErr на КАЖДОМ вызове.
	deleteErrTimes int

	createCalls int
	deleteCalls int
	lastCreate  *vpcpb.CreateAddressRequest
}

func (f *fakeAddressForAlloc) Create(_ context.Context, req *vpcpb.CreateAddressRequest) (*operationpb.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.lastCreate = req
	if f.createErr != nil {
		return nil, f.createErr
	}
	any, _ := anypb.New(f.createResp)
	return &operationpb.Operation{
		Id:     "op-alloc-1",
		Done:   true,
		Result: &operationpb.Operation_Response{Response: any},
	}, nil
}

func (f *fakeAddressForAlloc) Delete(_ context.Context, _ *vpcpb.DeleteAddressRequest) (*operationpb.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls++
	if f.deleteNotFound {
		return nil, status.Error(codes.NotFound, "no such address")
	}
	if f.deleteErr != nil && (f.deleteErrTimes == 0 || f.deleteCalls <= f.deleteErrTimes) {
		return nil, f.deleteErr
	}
	emptyAny, _ := anypb.New(&vpcpb.Address{}) // payload не важен для Delete
	return &operationpb.Operation{
		Id:     "op-del-1",
		Done:   true,
		Result: &operationpb.Operation_Response{Response: emptyAny},
	}, nil
}

// fakeInternalAddressService реализует InternalAddressService.{Set,Clear}Reference.
type fakeInternalAddressService struct {
	vpcpb.UnimplementedInternalAddressServiceServer

	mu sync.Mutex

	setErr   error
	clearErr error

	// release* — путь `ReleaseOwnedAddress`. Дублёр обязан уметь и НАЗВАТЬ
	// исход, и отказать: без отказа «что делает полоса, когда владелец не снял
	// аренду» не проверяемо вовсе, а это единственный путь, на котором аренда
	// теряется безвозвратно.
	releaseOutcome vpcpb.ReleaseOwnedAddressResponse_Outcome
	releaseErr     error
	releaseCalls   []*vpcpb.ReleaseOwnedAddressRequest

	// setErrTimes>0 → setErr is returned only for the first N SetAddressReference
	// calls, then the call succeeds. Models a per-object authz materialisation
	// window that closes on its own. 0 (default) keeps the original behaviour:
	// setErr is returned for EVERY call.
	setErrTimes int

	setCalls   []*vpcpb.SetAddressReferenceRequest
	clearCalls []*vpcpb.ClearAddressReferenceRequest

	lastSetMD metadata.MD // incoming metadata последнего SetAddressReference (проверка principal-проброса)

	// createOwned* — путь `CreateOwnedAddress` (создание адреса, СРАЗУ
	// привязанного к владельцу, одной writer-TX). Счётчик и последний запрос
	// нужны пробам, утверждающим, что на пути ОДНА мутация и она несёт
	// владельца.
	createOwnedResp   *vpcpb.Address
	createOwnedErr    error
	createOwnedCalls  int
	lastCreateOwned   *vpcpb.CreateOwnedAddressRequest
	lastCreateOwnedMD metadata.MD
	// addrFake — дублёр публичного AddressService той же пробы. Когда своего
	// ответа не задано, `CreateOwnedAddress` отвечает ЕГО поведением: создание
	// адреса не изменилось — изменилось лишь то, что привязка приехала той же
	// транзакцией. Связывается в `startFakeVPC`, чтобы фикстура не оказалась
	// снисходительнее продукта.
	addrFake *fakeAddressForAlloc
	// createDelegate — то же для дублёров, отдающих НЕзавершённую операцию
	// (проба поллера): создание адреса им и принадлежит.
	createDelegate func(context.Context, *vpcpb.CreateAddressRequest) (*operationpb.Operation, error)
}

func (f *fakeInternalAddressService) CreateOwnedAddress(
	ctx context.Context, req *vpcpb.CreateOwnedAddressRequest,
) (*operationpb.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createOwnedCalls++
	f.lastCreateOwned = req
	f.lastCreateOwnedMD, _ = metadata.FromIncomingContext(ctx)
	if f.createOwnedErr != nil {
		return nil, f.createOwnedErr
	}
	if f.createOwnedResp == nil && f.addrFake != nil {
		return f.addrFake.Create(ctx, req.GetAddress())
	}
	if f.createOwnedResp == nil && f.createDelegate != nil {
		return f.createDelegate(ctx, req.GetAddress())
	}
	resp := f.createOwnedResp
	if resp == nil {
		resp = &vpcpb.Address{Id: "adr-owned-default"}
	}
	any, _ := anypb.New(resp)
	return &operationpb.Operation{
		Id:     "op-alloc-owned-1",
		Done:   true,
		Result: &operationpb.Operation_Response{Response: any},
	}, nil
}

func (f *fakeInternalAddressService) SetAddressReference(
	ctx context.Context, req *vpcpb.SetAddressReferenceRequest,
) (*vpcpb.AddressReference, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastSetMD, _ = metadata.FromIncomingContext(ctx)
	f.setCalls = append(f.setCalls, req)
	if f.setErr != nil && (f.setErrTimes == 0 || len(f.setCalls) <= f.setErrTimes) {
		return nil, f.setErr
	}
	return &vpcpb.AddressReference{
		AddressId:    req.AddressId,
		ReferrerType: req.ReferrerType,
		ReferrerId:   req.ReferrerId,
	}, nil
}

func (f *fakeInternalAddressService) ClearAddressReference(
	_ context.Context, req *vpcpb.ClearAddressReferenceRequest,
) (*vpcpb.ClearAddressReferenceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearCalls = append(f.clearCalls, req)
	if f.clearErr != nil {
		return nil, f.clearErr
	}
	return &vpcpb.ClearAddressReferenceResponse{}, nil
}

// fakeOperationService — Operation.Get для долгих операций
// (наш fake возвращает Done=true сразу, OperationService не вызывается; но он
// нужен для регистрации, иначе server NewClient'у может не нравиться).
type fakeOperationService struct {
	operationpb.UnimplementedOperationServiceServer
}

func (f *fakeOperationService) Get(_ context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	return &operationpb.Operation{Id: req.OperationId, Done: true}, nil
}

func TestInternalAddressClient_AllocateExternalIP_HappyPath(t *testing.T) {
	allocResp := &vpcpb.Address{
		Id:        "e9b-ip-1",
		ProjectId: "prj-1",
		Address: &vpcpb.Address_ExternalIpv4Address{
			ExternalIpv4Address: &vpcpb.ExternalIpv4Address{
				Address: "203.0.113.5",
				ZoneId:  "ru-central1-a",
			},
		},
	}
	addrSvc := &fakeAddressForAlloc{createResp: allocResp}
	intAddrSvc := &fakeInternalAddressService{}
	opSvc := &fakeOperationService{}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, opSvc)

	c := NewInternalAddressClient(conn, conn)
	require.NotNil(t, c)

	resp, err := c.AllocateExternalIP(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1",
		Name:      "listener-vip-1",
		ZoneID:    "ru-central1-a",
		Owner:     AddressOwner{Kind: "nlb_listener", ID: "lst-1", Name: "listener-a"},
	})
	require.NoError(t, err)
	assert.Equal(t, "e9b-ip-1", resp.AddressID)
	assert.Equal(t, "203.0.113.5", resp.Value)
	// Одна мутация несёт и создание, и владельца: отдельного вызова привязки
	// на этом пути больше нет (он и вносил зависимость от окна материализации).
	assert.Equal(t, 1, addrSvc.createCalls, "делегированный дублёр видел ровно одно создание")
	assert.Zero(t, intAddrSvc.setCallCount())
	got := intAddrSvc.lastCreateOwnedReq()
	require.NotNil(t, got)
	assert.Equal(t, "nlb_listener", got.GetReferrerType())
	assert.Equal(t, "lst-1", got.GetReferrerId())
	// referrer_name пробрасывается в vpc — used_by-зеркало показывает имя
	// потребителя (иначе UI не может отрендерить ссылку на ресурс).
	assert.Equal(t, "listener-a", got.GetReferrerName())
}

// ЗДЕСЬ СТОЯЛИ ДВЕ ПРОБЫ КОМПЕНСАЦИИ — у них не осталось предмета.
//
// Обе описывали двухшаговый путь авто-аллокации: `Create` коммитил адрес, а
// отдельная привязка могла отказать, и тогда клиент обязан был вернуть
// half-allocated адрес в пул (и громко пожаловаться, если возврат тоже не
// прошёл). Путь снят: создание, аллокация и привязка живут в ОДНОЙ транзакции
// на стороне vpc, поэтому half-allocated адреса не бывает, компенсировать
// нечего и «тихой утечки аренды» на этом пути возникнуть не может.
//
// Свойство, ради которого они существовали, теперь утверждает
// `TestAllocate_FailureLeavesNothingToCompensate` (alloc_own_lane_visibility_test.go):
// отказ мутации НЕ приводит к компенсирующему Delete, потому что откатывать
// нечего. Утечка аренды на путях, где две стороны остаются (BYO-привязка,
// teardown), покрыта своими пробами и этим изменением не затронута.

func TestInternalAddressClient_AllocateExternalIP_PoolExhausted(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{createErr: status.Error(codes.FailedPrecondition, "pool exhausted")}
	conn := startFakeVPC(t, nil, nil, addrSvc, &fakeInternalAddressService{}, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	_, err := c.AllocateExternalIP(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1", ZoneID: "z", Owner: AddressOwner{Kind: "nlb_listener", ID: "lst-1"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrFailedPrecondition))
}

func TestInternalAddressClient_AllocateInternalIP_HappyPath(t *testing.T) {
	allocResp := &vpcpb.Address{
		Id: "e9b-ip-3",
		Address: &vpcpb.Address_InternalIpv4Address{
			InternalIpv4Address: &vpcpb.InternalIpv4Address{Address: "10.128.0.7"},
		},
	}
	addrSvc := &fakeAddressForAlloc{createResp: allocResp}
	intAddrSvc := &fakeInternalAddressService{}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	resp, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "n", SubnetID: "e9b-1",
		Owner: AddressOwner{Kind: "nlb_listener", ID: "lst-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "e9b-ip-3", resp.AddressID)
	assert.Equal(t, "10.128.0.7", resp.Value)
	assert.Empty(t, resp.PoolID, "internal IP не имеет pool_id")
}

func TestInternalAddressClient_AllocateExternalIPv6_HappyPath(t *testing.T) {
	allocResp := &vpcpb.Address{
		Id:        "e9b-ip6-1",
		ProjectId: "prj-1",
		Address: &vpcpb.Address_ExternalIpv6Address{
			ExternalIpv6Address: &vpcpb.ExternalIpv6Address{
				Address: "2001:db8::5",
				ZoneId:  "ru-central1-a",
			},
		},
	}
	addrSvc := &fakeAddressForAlloc{createResp: allocResp}
	intAddrSvc := &fakeInternalAddressService{}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	resp, err := c.AllocateExternalIPv6(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1",
		Name:      "v6-vip",
		ZoneID:    "ru-central1-a",
		Owner:     AddressOwner{Kind: "nlb_listener", ID: "lst-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "e9b-ip6-1", resp.AddressID)
	assert.Equal(t, "2001:db8::5", resp.Value)
	require.NotNil(t, addrSvc.lastCreate.GetExternalIpv6AddressSpec(), "must build external_ipv6 spec, not v4")
	// Владелец едет той же единственной мутацией; отдельной привязки нет.
	assert.Zero(t, intAddrSvc.setCallCount())
	require.NotNil(t, intAddrSvc.lastCreateOwnedReq())
	assert.Equal(t, "lst-1", intAddrSvc.lastCreateOwnedReq().GetReferrerId())
}

// NOTE: an empty ZoneID is NOT an error — it is the anycast (zone-independent)
// allocation a REGIONAL load balancer VIP requires. Locked, with its v4/v6 parity
// and the "a supplied zone is still forwarded" counterpart, in
// alloc_zone_independent_test.go.

func TestInternalAddressClient_AllocateInternalIPv6_HappyPath(t *testing.T) {
	allocResp := &vpcpb.Address{
		Id: "e9b-ip6-2",
		Address: &vpcpb.Address_InternalIpv6Address{
			InternalIpv6Address: &vpcpb.InternalIpv6Address{
				Address: "fd00::9",
				Scope:   &vpcpb.InternalIpv6Address_SubnetId{SubnetId: "e9b-sub6"},
			},
		},
	}
	addrSvc := &fakeAddressForAlloc{createResp: allocResp}
	intAddrSvc := &fakeInternalAddressService{}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	resp, err := c.AllocateInternalIPv6(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "n", SubnetID: "e9b-sub6",
		Owner: AddressOwner{Kind: "nlb_listener", ID: "lst-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "e9b-ip6-2", resp.AddressID)
	assert.Equal(t, "fd00::9", resp.Value)
	require.NotNil(t, addrSvc.lastCreate.GetInternalIpv6AddressSpec(), "must build internal_ipv6 spec, not v4")
	assert.Equal(t, "e9b-sub6", addrSvc.lastCreate.GetInternalIpv6AddressSpec().GetSubnetId())
}

func TestInternalAddressClient_AllocateInternalIP_EmptySubnetRejected(t *testing.T) {
	c := NewInternalAddressClient(
		startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, &fakeInternalAddressService{}, &fakeOperationService{}),
		startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, &fakeInternalAddressService{}, &fakeOperationService{}),
	)
	_, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "p", Owner: AddressOwner{Kind: "k", ID: "i"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidArg))
}

func (f *fakeInternalAddressService) ReleaseOwnedAddress(
	_ context.Context, req *vpcpb.ReleaseOwnedAddressRequest,
) (*vpcpb.ReleaseOwnedAddressResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls = append(f.releaseCalls, req)
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	out := f.releaseOutcome
	if out == vpcpb.ReleaseOwnedAddressResponse_OUTCOME_UNSPECIFIED {
		out = vpcpb.ReleaseOwnedAddressResponse_RELEASED
	}
	return &vpcpb.ReleaseOwnedAddressResponse{Outcome: out}, nil
}

// Повтор законен и НАЗВАН: полоса освобождения работает at-least-once, поэтому
// «аренда снята ранее» — исход, а не ошибка. Прежде на этом месте стояла проба,
// закреплявшая ПРОТИВОПОЛОЖНОЕ: она требовала, чтобы ответ владельца
// «не найдено» читался как успех. Тот ответ такого утверждения не несёт (им же
// отвечают на промах чужого проекта и на опрос операции без ключа владельца), и
// проба закрепляла дефект, а не свойство.
func TestInternalAddressClient_ReleaseLease_RepeatIsNamed(t *testing.T) {
	intAddrSvc := &fakeInternalAddressService{
		releaseOutcome: vpcpb.ReleaseOwnedAddressResponse_ALREADY_RELEASED,
	}
	conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, intAddrSvc, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	out, err := c.ReleaseLease(ctxBackground(), ReleaseLeaseRequest{
		ProjectID: "prj-1", AddressID: "e9b-already-gone",
		Owner: AddressOwner{Kind: "nlb_network_load_balancer", ID: "lb-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, LeaseAlreadyReleased, out, "исход обязан быть НАЗВАН, а не выведен из кода ошибки")
}

func TestInternalAddressClient_ReleaseLease_HappyPath(t *testing.T) {
	intAddrSvc := &fakeInternalAddressService{}
	conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, intAddrSvc, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	out, err := c.ReleaseLease(ctxBackground(), ReleaseLeaseRequest{
		ProjectID: "prj-1", AddressID: "e9b-ip-1",
		Owner: AddressOwner{Kind: "nlb_network_load_balancer", ID: "lb-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, LeaseReleased, out)
	require.Len(t, intAddrSvc.releaseCalls, 1)
	assert.Equal(t, "prj-1", intAddrSvc.releaseCalls[0].GetProjectId(),
		"якорь права — проект: без него глагол не авторизуем")
	assert.Equal(t, "lb-1", intAddrSvc.releaseCalls[0].GetReferrerId())
}

// Ни один код ошибки владельца не читается как «аренда снята» — это и есть
// предмет #439. Табличная проба перечисляет ВСЕ полосы отказа, включая
// `NOT_FOUND`, которого глагол не производит: получить его можно только говоря
// не с тем глаголом, и это настройка, а не «уже снято».
func TestInternalAddressClient_ReleaseLease_NoRefusalIsReadAsSuccess(t *testing.T) {
	cases := []struct {
		name string
		peer error
		want error
	}{
		{"нет права", status.Error(codes.PermissionDenied, "no path"), domain.ErrFailedPrecondition},
		{"аренда чужая", status.Error(codes.FailedPrecondition, "not leased by"), domain.ErrFailedPrecondition},
		{"негодный ввод", status.Error(codes.InvalidArgument, "bad id"), domain.ErrInvalidArg},
		{"глагол не обслуживается", status.Error(codes.NotFound, "unknown method"), domain.ErrFailedPrecondition},
		{"владелец недоступен", status.Error(codes.Unavailable, "vpc down"), domain.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intAddrSvc := &fakeInternalAddressService{releaseErr: tc.peer}
			conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, intAddrSvc, &fakeOperationService{})
			c := NewInternalAddressClient(conn, conn)
			out, err := c.ReleaseLease(ctxBackground(), ReleaseLeaseRequest{
				ProjectID: "prj-1", AddressID: "e9b-ip-1",
				Owner: AddressOwner{Kind: "nlb_network_load_balancer", ID: "lb-1"},
			})
			require.Error(t, err, "отказ владельца НЕ ЕСТЬ доказательство снятой аренды")
			assert.True(t, errors.Is(err, tc.want), "ожидалась полоса %v, получено %v", tc.want, err)
			assert.Empty(t, out, "исход не называется там, где работа не сделана")
		})
	}
}

// Неназванный исход — тоже отказ. Без этой пробы `OUTCOME_UNSPECIFIED` читался
// бы как нулевое значение и вернул бы вывод, который метод устраняет.
func TestInternalAddressClient_ReleaseLease_UnnamedOutcomeIsRefused(t *testing.T) {
	intAddrSvc := &fakeInternalAddressService{}
	conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, intAddrSvc, &fakeOperationService{})
	c := NewInternalAddressClient(conn, conn)

	// Дублёр по умолчанию отвечает RELEASED, поэтому неназванный исход подаём
	// напрямую — иначе проба утверждала бы о своей копии, а не о клиенте.
	out, ok := leaseOutcomeFromProto(vpcpb.ReleaseOwnedAddressResponse_OUTCOME_UNSPECIFIED)
	require.False(t, ok, "неназванный исход обязан быть нераспознан")
	assert.Empty(t, out)
	_ = c
}

func TestInternalAddressClient_SetReference_AlreadyExistsMapsToPrecondition(t *testing.T) {
	intAddrSvc := &fakeInternalAddressService{setErr: status.Error(codes.AlreadyExists, "owner mismatch")}
	conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, intAddrSvc, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	err := c.SetReference(ctxBackground(), "e9b-ip-1", AddressOwner{Kind: "nlb_listener", ID: "lst-1"}, true)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrFailedPrecondition))
}

func TestInternalAddressClient_SetReference_NotFoundMapsToInvalidArg(t *testing.T) {
	intAddrSvc := &fakeInternalAddressService{setErr: status.Error(codes.NotFound, "address not found")}
	conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, intAddrSvc, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	err := c.SetReference(ctxBackground(), "e9b-nx", AddressOwner{Kind: "k", ID: "i"}, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidArg))
}

func TestInternalAddressClient_AllocateExternalIP_EmptyArgs(t *testing.T) {
	c := NewInternalAddressClient(
		startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, &fakeInternalAddressService{}, &fakeOperationService{}),
		startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, &fakeInternalAddressService{}, &fakeOperationService{}),
	)
	cases := []struct {
		name string
		req  AllocateExternalIPRequest
	}{
		// An empty ZoneID is deliberately NOT here: zone-less = anycast, the only
		// valid form for a REGIONAL load balancer VIP (alloc_zone_independent_test.go).
		{"empty project", AllocateExternalIPRequest{ZoneID: "z", Owner: AddressOwner{Kind: "k", ID: "i"}}},
		{"empty owner", AllocateExternalIPRequest{ProjectID: "p", ZoneID: "z"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.AllocateExternalIP(ctxBackground(), tc.req)
			require.Error(t, err)
			assert.True(t, errors.Is(err, domain.ErrInvalidArg))
		})
	}
}

func TestInternalAddressClient_SetReference_EmptyArgs(t *testing.T) {
	c := NewInternalAddressClient(
		startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, &fakeInternalAddressService{}, &fakeOperationService{}),
		startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, &fakeInternalAddressService{}, &fakeOperationService{}),
	)
	cases := []struct {
		name, id string
		owner    AddressOwner
	}{
		{"empty id", "", AddressOwner{Kind: "k", ID: "i"}},
		{"empty kind", "e9b-ip-1", AddressOwner{ID: "i"}},
		{"empty owner id", "e9b-ip-1", AddressOwner{Kind: "k"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.SetReference(ctxBackground(), tc.id, tc.owner, false)
			require.Error(t, err)
			assert.True(t, errors.Is(err, domain.ErrInvalidArg))
		})
	}
}

func TestInternalAddressClient_NilConn(t *testing.T) {
	assert.Nil(t, NewInternalAddressClient(nil, nil))
	conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, &fakeInternalAddressService{}, &fakeOperationService{})
	assert.Nil(t, NewInternalAddressClient(conn, nil))
	assert.Nil(t, NewInternalAddressClient(nil, conn))
}

// fakeAddressForAllocPolling — Create возвращает Done=false, OperationService
// должен поллить до Done=true (test for waitOperation loop).
type fakeAddressForAllocPolling struct {
	vpcpb.UnimplementedAddressServiceServer

	createResp *vpcpb.Address
}

func (f *fakeAddressForAllocPolling) Create(_ context.Context, _ *vpcpb.CreateAddressRequest) (*operationpb.Operation, error) {
	// Operation not done — caller will poll via OperationService.Get.
	return &operationpb.Operation{Id: "op-poll-1", Done: false}, nil
}

// fakeOpServicePolling — после N Get'ов возвращает Done=true с inline Address.
type fakeOpServicePolling struct {
	operationpb.UnimplementedOperationServiceServer

	mu        sync.Mutex
	getCalls  int
	doneAfter int
	addrResp  *vpcpb.Address
	opErr     *opErrStatus // nil → return inline response; non-nil → return with.error set
}

type opErrStatus struct {
	code    codes.Code
	message string
}

func (f *fakeOpServicePolling) Get(_ context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.getCalls < f.doneAfter {
		return &operationpb.Operation{Id: req.OperationId, Done: false}, nil
	}
	if f.opErr != nil {
		return &operationpb.Operation{
			Id:     req.OperationId,
			Done:   true,
			Result: &operationpb.Operation_Error{Error: status.New(f.opErr.code, f.opErr.message).Proto()},
		}, nil
	}
	any, _ := anypb.New(f.addrResp)
	return &operationpb.Operation{
		Id:     req.OperationId,
		Done:   true,
		Result: &operationpb.Operation_Response{Response: any},
	}, nil
}

func TestInternalAddressClient_AllocateExternalIP_PollLoop(t *testing.T) {
	// Create returns Done=false → adapter polls Operation.Get; on 3rd call Done=true.
	addrResp := &vpcpb.Address{
		Id: "e9b-ip-poll",
		Address: &vpcpb.Address_ExternalIpv4Address{
			ExternalIpv4Address: &vpcpb.ExternalIpv4Address{Address: "203.0.113.99"},
		},
	}
	addrSvc := &fakeAddressForAllocPolling{createResp: addrResp}
	intAddrSvc := &fakeInternalAddressService{}
	opSvc := &fakeOpServicePolling{doneAfter: 3, addrResp: addrResp}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, opSvc)

	c := NewInternalAddressClient(conn, conn)
	resp, err := c.AllocateExternalIP(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1", ZoneID: "z",
		Owner: AddressOwner{Kind: "nlb_listener", ID: "lst-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "e9b-ip-poll", resp.AddressID)
	assert.Equal(t, "203.0.113.99", resp.Value)
	assert.GreaterOrEqual(t, opSvc.getCalls, 3, "must have polled ≥3 times before Done")
}

func TestInternalAddressClient_AllocateExternalIP_OperationFailure(t *testing.T) {
	// Operation completes with error — adapter must surface gRPC status as sentinel.
	addrSvc := &fakeAddressForAllocPolling{}
	opSvc := &fakeOpServicePolling{
		doneAfter: 1,
		opErr:     &opErrStatus{code: codes.FailedPrecondition, message: "pool exhausted"},
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, &fakeInternalAddressService{}, opSvc)

	c := NewInternalAddressClient(conn, conn)
	_, err := c.AllocateExternalIP(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1", ZoneID: "z",
		Owner: AddressOwner{Kind: "nlb_listener", ID: "lst-1"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrFailedPrecondition))
}

// AddressClientFromStub / etc. constructor wired tests
func TestVPC_FromStubConstructors_Nil(t *testing.T) {
	assert.Nil(t, NewAddressClientFromStub(nil))
	assert.Nil(t, NewSubnetClientFromStub(nil))
	assert.Nil(t, NewNetworkInterfaceClientFromStub(nil))
	assert.Nil(t, NewInternalAddressClientFromStubs(nil, nil, nil))
}

func TestInternalAddressClient_ReleaseLease_Unavailable(t *testing.T) {
	intAddrSvc := &fakeInternalAddressService{releaseErr: status.Error(codes.Unavailable, "down")}
	conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, intAddrSvc, &fakeOperationService{})
	c := NewInternalAddressClient(conn, conn)
	ctx, cancel := context.WithTimeout(ctxBackground(), 200*time.Millisecond)
	defer cancel()
	_, err := c.ReleaseLease(ctx, ReleaseLeaseRequest{
		ProjectID: "prj-1", AddressID: "e9b-ip-1",
		Owner: AddressOwner{Kind: "nlb_network_load_balancer", ID: "lb-1"},
	})
	require.Error(t, err, "недоступность владельца — не «уже снято»")
	if !errors.Is(err, domain.ErrUnavailable) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected ErrUnavailable or DeadlineExceeded; got %v", err)
	}
}

// --- round-6 audit finding 2 sweep: hanging-peer regression tests ---
//
// The internal address client wires EVERY outbound call (SetAddressReference,
// ClearAddressReference, AddressService.Create/Delete/Get, Operation.Get) through
// c.withCallTimeout — a stalled kacho-vpc peer must fail closed within the
// configured per-call timeout, not park the calling goroutine (and, for
// free_ip_runner's reconcileOne, the FOR UPDATE SKIP LOCKED row-lock + tx)
// forever. Each test below drives one call site with a blocking fake.

// blockingInternalAddressService — fake InternalAddressServiceServer whose
// SetAddressReference/ClearAddressReference never return until released
// (simulates a hung/stalled kacho-vpc peer).
type blockingInternalAddressService struct {
	vpcpb.UnimplementedInternalAddressServiceServer
	release chan struct{}
}

func (f *blockingInternalAddressService) SetAddressReference(
	_ context.Context, _ *vpcpb.SetAddressReferenceRequest,
) (*vpcpb.AddressReference, error) {
	<-f.release
	return &vpcpb.AddressReference{}, nil
}

func (f *blockingInternalAddressService) ClearAddressReference(
	_ context.Context, _ *vpcpb.ClearAddressReferenceRequest,
) (*vpcpb.ClearAddressReferenceResponse, error) {
	<-f.release
	return &vpcpb.ClearAddressReferenceResponse{}, nil
}

func (f *blockingInternalAddressService) ReleaseOwnedAddress(
	_ context.Context, _ *vpcpb.ReleaseOwnedAddressRequest,
) (*vpcpb.ReleaseOwnedAddressResponse, error) {
	<-f.release
	return &vpcpb.ReleaseOwnedAddressResponse{}, nil
}

func TestInternalAddressClient_SetReference_HangingPeer_BoundsToConfiguredTimeout(t *testing.T) {
	fake := &blockingInternalAddressService{release: make(chan struct{})}
	conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, fake, &fakeOperationService{})

	const configuredTimeout = 100 * time.Millisecond
	c := NewInternalAddressClientWithTimeout(conn, conn, configuredTimeout)

	start := time.Now()
	err := c.SetReference(context.Background(), "e9b-ip-1", AddressOwner{Kind: "nlb_listener", ID: "lst-1"}, true)
	elapsed := time.Since(start)
	// Release the still-in-flight fake handler goroutine synchronously (NOT
	// via t.Cleanup: startFakeVPC's own GracefulStop cleanup runs LIFO and
	// would deadlock waiting on this still-blocked handler otherwise).
	close(fake.release)

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnavailable),
		"expected fail-closed domain.ErrUnavailable on peer hang; got %v", err)
	assert.Less(t, elapsed, 2*time.Second,
		"SetReference must bound to the configured per-call timeout (~%s), not hang; took %s",
		configuredTimeout, elapsed)
}

func TestInternalAddressClient_ReleaseLease_HangingPeer_BoundsToConfiguredTimeout(t *testing.T) {
	fake := &blockingInternalAddressService{release: make(chan struct{})}
	conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, fake, &fakeOperationService{})

	const configuredTimeout = 100 * time.Millisecond
	c := NewInternalAddressClientWithTimeout(conn, conn, configuredTimeout)

	start := time.Now()
	_, err := c.ReleaseLease(context.Background(), ReleaseLeaseRequest{
		ProjectID: "prj-1", AddressID: "e9b-ip-1",
		Owner: AddressOwner{Kind: "nlb_network_load_balancer", ID: "lb-1"},
	})
	elapsed := time.Since(start)
	close(fake.release)

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnavailable),
		"expected fail-closed domain.ErrUnavailable on peer hang; got %v", err)
	assert.Less(t, elapsed, 2*time.Second,
		"ReleaseLease must bound to the configured per-call timeout (~%s), not hang; took %s",
		configuredTimeout, elapsed)
}

// blockingAddressForAlloc — fake AddressServiceServer whose Delete/Create
// never return until released (simulates a hung/stalled kacho-vpc peer).
type blockingAddressForAlloc struct {
	vpcpb.UnimplementedAddressServiceServer
	release chan struct{}
}

func (f *blockingAddressForAlloc) Delete(
	_ context.Context, _ *vpcpb.DeleteAddressRequest,
) (*operationpb.Operation, error) {
	<-f.release
	return &operationpb.Operation{Id: "op-del-1", Done: true, Result: &operationpb.Operation_Response{}}, nil
}

func (f *blockingAddressForAlloc) Create(
	_ context.Context, _ *vpcpb.CreateAddressRequest,
) (*operationpb.Operation, error) {
	<-f.release
	return &operationpb.Operation{Id: "op-create-1", Done: false}, nil
}

// blockingOperationService — fake OperationServiceServer whose Get never
// returns until released (simulates a hung/stalled kacho-vpc peer mid-poll).
type blockingOperationService struct {
	operationpb.UnimplementedOperationServiceServer
	release chan struct{}
}

func (f *blockingOperationService) Get(
	_ context.Context, _ *operationpb.GetOperationRequest,
) (*operationpb.Operation, error) {
	<-f.release
	return &operationpb.Operation{Done: true}, nil
}

// TestInternalAddressClient_AllocateExternalIP_HangingPollPeer_BoundsToConfiguredTimeout —
// regression for round-6 audit finding 2: waitOperation's per-iteration
// Operation.Get must be bounded too (not just the initial Create), otherwise a
// peer that stalls mid-poll (after acking Create with done=false) parks the
// calling goroutine forever despite the client having "started" the call
// with a per-call timeout on Create.
func TestInternalAddressClient_AllocateExternalIP_HangingPollPeer_BoundsToConfiguredTimeout(t *testing.T) {
	addrSvc := &fakeAddressForAllocPolling{}
	opSvc := &blockingOperationService{release: make(chan struct{})}
	conn := startFakeVPC(t, nil, nil, addrSvc, &fakeInternalAddressService{}, opSvc)

	const configuredTimeout = 100 * time.Millisecond
	c := NewInternalAddressClientWithTimeout(conn, conn, configuredTimeout)

	start := time.Now()
	_, err := c.AllocateExternalIP(context.Background(), AllocateExternalIPRequest{
		ProjectID: "prj-1", ZoneID: "z", Owner: AddressOwner{Kind: "nlb_listener", ID: "lst-1"},
	})
	elapsed := time.Since(start)
	close(opSvc.release)

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnavailable),
		"expected fail-closed domain.ErrUnavailable on peer hang; got %v", err)
	// vpcOpPollInterval (50ms) ticks + one bounded 100ms Get call; generous
	// bound accounts for a few poll iterations before the deadline trips.
	assert.Less(t, elapsed, 3*time.Second,
		"AllocateExternalIP must bound the poll-loop's Get calls to the configured "+
			"per-call timeout (~%s), not hang; took %s", configuredTimeout, elapsed)
}
