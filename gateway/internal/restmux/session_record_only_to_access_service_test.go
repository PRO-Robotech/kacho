// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// session_record_only_to_access_service_test.go — номер записи текущей сессии
// человека край отдаёт ТОЛЬКО службе доступа (kacho#3136).
//
// ПРЕДМЕТ. Номер записи читает одна служба — служба доступа (снятие ключа
// доступа щадит текущую сессию, kacho#3126). Прочим звеньям он не нужен, и
// минимальность передаваемого требует, чтобы он к ним не уезжал. Решение
// принимается по АДРЕСАТУ вызова, а не по месту, где метаданные собираются:
// мостом REST→gRPC номер мог уехать к любому бэкенду, проксёй операций — к
// владельцу операции, проекцией потока подписки — к владельцу журнала
// (проба последнего живёт рядом с проекцией,
// subscriptionstream/session_record_stays_at_the_edge_test.go).
//
// КАК СОБРАНО. Тот же край, что у TestEdgeForwardsTheSessionRecordAndDropsTheClientOne:
// полоса нашей сессии боевыми конструкторами над NewMux, дублёры служб на
// настоящем gRPC. Утверждение — о том, что увидел дублёр в
// metadata.FromIncomingContext, и о том, что вернулось клиенту.
//
// КЕЙСЫ отличаются от положительного ОДНИМ фактом — адресатом: та же сессия,
// тот же носитель, тот же номер записи в ответе службы о носителе.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/principalwire"
	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	iampb "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

const (
	recordNetworkID   = "enp00000000000003136"
	recordOperationID = "e9b00000000000003136" // префикс e9b — операции vpc
)

// seen — что увидел дублёр: значения ключа номера записи и идентификатор
// вызывающего (положительный контроль: личность до адресата доехала, значит
// канал метаданных провязан и «номера нет» не означает «нет ничего»).
type seen struct {
	record    []string
	principal []string
}

func seenFrom(ctx context.Context) seen {
	md, _ := metadata.FromIncomingContext(ctx)
	return seen{record: md.Get(principalwire.MetaTokenSessionID), principal: md.Get(principalmeta.MetaPrincipalID)}
}

type recordAccessKeys struct {
	iampb.UnimplementedAccessKeyServiceServer
	got chan seen
}

func (s *recordAccessKeys) Revoke(ctx context.Context, _ *iampb.RevokeAccessKeyRequest) (*operationv1.Operation, error) {
	s.got <- seenFrom(ctx)
	return &operationv1.Operation{Id: "iop00000000000003136"}, nil
}

type recordNetworks struct {
	vpcpb.UnimplementedNetworkServiceServer
	got chan seen
}

func (s *recordNetworks) Delete(ctx context.Context, _ *vpcpb.DeleteNetworkRequest) (*operationv1.Operation, error) {
	s.got <- seenFrom(ctx)
	return &operationv1.Operation{Id: "enp00000000000013136"}, nil
}

// recordOperations — владелец операции за проксёй операций. Операцию он
// отдаёт от имени того, кто спрашивает, — иначе проверка владения края
// отказала бы и запрос не дошёл бы до клиента 200-м.
type recordOperations struct {
	operationv1.UnimplementedOperationServiceServer
	got chan seen
}

func (s *recordOperations) Get(ctx context.Context, in *operationv1.GetOperationRequest) (*operationv1.Operation, error) {
	got := seenFrom(ctx)
	s.got <- got
	md, _ := metadata.FromIncomingContext(ctx)
	op := &operationv1.Operation{Id: in.GetOperationId(), Done: true}
	if v := md.Get(principalmeta.MetaPrincipalType); len(v) > 0 {
		op.PrincipalType = v[0]
	}
	if len(got.principal) > 0 {
		op.PrincipalId = got.principal[0]
	}
	return op, nil
}

// recordEdge — край: полоса нашей сессии над NewMux; служба доступа, служба
// сети и владелец операций — дублёры.
func recordEdge(t *testing.T) (http.Handler, *recordAccessKeys, *recordNetworks, *recordOperations) {
	t.Helper()
	identity := serveGRPC(t, func(gs *grpc.Server) {
		iampb.RegisterInternalHumanSessionServiceServer(gs, &sessionRecordIdentity{record: sessionRecordLive})
	})
	idConn, err := grpc.NewClient(identity, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial identity: %v", err)
	}
	t.Cleanup(func() { _ = idConn.Close() })

	keys := &recordAccessKeys{got: make(chan seen, 1)}
	nets := &recordNetworks{got: make(chan seen, 1)}
	ops := &recordOperations{got: make(chan seen, 1)}
	addrs := geoMuxAddrs()
	addrs["iam"] = serveGRPC(t, func(gs *grpc.Server) { iampb.RegisterAccessKeyServiceServer(gs, keys) })
	vpc := serveGRPC(t, func(gs *grpc.Server) {
		vpcpb.RegisterNetworkServiceServer(gs, nets)
		operationv1.RegisterOperationServiceServer(gs, ops)
	})
	addrs["vpc"] = vpc
	vpcConn, err := grpc.NewClient(vpc, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial vpc: %v", err)
	}
	t.Cleanup(func() { _ = vpcConn.Close() })

	mux, err := NewMux(t.Context(), addrs, map[string]*grpc.ClientConn{"vpc": vpcConn}, nil, 30*time.Second)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", sessionRecordLookup{}, silentTestLogger()).
		WithHumanSession(clients.NewSessionRevocationsAdapter(idConn, 5*time.Second))
	return auth.HTTP(mux), keys, nets, ops
}

// TestSessionRecordReachesOnlyTheAccessService — положительный кейс и его
// близнецы по адресату. Служба доступа видит ровно [номер]; служба сети и
// владелец операции под той же сессией — ничего; клиенту номер не уходит ни в
// одном кейсе.
func TestSessionRecordReachesOnlyTheAccessService(t *testing.T) {
	edge, keys, nets, ops := recordEdge(t)
	cases := []struct {
		name   string
		method string
		path   string
		got    chan seen
		want   []string
	}{
		{name: "access-service", method: http.MethodDelete,
			path: "/iam/v1/users/" + sessionRecordUser + "/accessKeys/akey0000000000003136",
			got:  keys.got, want: []string{sessionRecordLive}},
		{name: "network-service", method: http.MethodDelete,
			path: "/vpc/v1/networks/" + recordNetworkID, got: nets.got, want: nil},
		{name: "operation-owner", method: http.MethodGet,
			path: "/operations/" + recordOperationID, got: ops.got, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: sessionRecordCarrier})
			w := httptest.NewRecorder()
			edge.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("%s %s: http %d, body %s", tc.method, tc.path, w.Code, w.Body.String())
			}
			var got seen
			select {
			case got = <-tc.got:
			default:
				t.Fatalf("адресат %s не вызван — край не довёл запрос", tc.name)
			}
			if len(got.principal) != 1 || got.principal[0] != sessionRecordUser {
				t.Fatalf("контроль: адресат увидел личность %q, want [%q] — канал метаданных не провязан, «номера нет» ничего не значит",
					got.principal, sessionRecordUser)
			}
			if len(got.record) != len(tc.want) || (len(got.record) == 1 && got.record[0] != tc.want[0]) {
				t.Errorf("адресат %s увидел %s = %q, want %q", tc.name, principalwire.MetaTokenSessionID, got.record, tc.want)
			}
			for k, vs := range w.Header() {
				for _, v := range vs {
					if strings.Contains(v, sessionRecordLive) {
						t.Errorf("номер записи ушёл клиенту в заголовке ответа %s: %q", k, v)
					}
				}
			}
			if strings.Contains(w.Body.String(), sessionRecordLive) {
				t.Errorf("номер записи ушёл клиенту в теле ответа: %s", w.Body.String())
			}
		})
	}
}
