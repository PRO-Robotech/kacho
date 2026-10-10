// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// edge_forwards_session_record_test.go — край называет службе доступа текущую
// сессию человека НОМЕРОМ ЕЁ ЗАПИСИ, и клиентское значение того же ключа до
// службы не доходит (kacho#3126, сторона края PRO-Robotech/kaname#677).
//
// ПРЕДМЕТ. Снятие ключа доступа гасит прочие сессии человека и оставляет
// текущую (Ф13 Р8). Служба узнаёт текущую из метаданного
// `principalwire.MetaTokenSessionID`, которое читает за вердиктом о доверенном
// отправителе — тем же каналом, что пересланную личность. Номер записи краю
// называет ответ службы о носителе (`InternalHumanSessionService/Resolve`,
// поле `session_id`). Край, не вернувший номер, оставляет службе «текущая не
// названа» — и снятие ключа из консоли гасит сессию, из которой ключ сняли.
//
// ПОЧЕМУ СКВОЗЬ ВСЮ ЦЕПОЧКУ. Звеньев три, и каждое могло бы потерять номер
// молча: адаптер ответа о носителе, полоса нашей сессии и мост REST→gRPC
// (`principalHeaderMatcher`). Проба собирает их боевыми конструкторами —
// `clients.NewSessionRevocationsAdapter`, `middleware.AuthInterceptor.HTTP`,
// `NewMux` — против двух дублёров службы на настоящем gRPC: ответа о носителе
// и `AccessKeyService.Revoke`. Утверждение — о том, что увидела служба в
// `metadata.FromIncomingContext`.
//
// СРЕЗ. Ключ лежит в подсемействе `x-kacho-token-`: край снимает ВСЁ
// клиентское пространство `x-kacho-` до выбора полосы. Клиентское значение в
// обеих поверхностных формах (голой и мостовой) не доходит ни рядом с
// номером края, ни вместо него.
//
// КЕЙСЫ различаются ОДНИМ фактом против положительного: подложено ли
// клиентское значение и в какой форме; предъявлен ли носитель; назвала ли
// служба номер записи в ответе о носителе.

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/principalwire"
	iampb "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

const (
	sessionRecordUser    = "usr-00000000000003126"
	sessionRecordCarrier = "carrier-3126-live"
	sessionRecordLive    = "hss-000000000000003126"
	sessionRecordForged  = "hss-0000000000000forged"
)

// sessionRecordIdentity — дублёр ответа службы о носителе: живой носитель —
// живая сессия; номер записи — тот, что назван (пусто — служба его не
// называет).
type sessionRecordIdentity struct {
	iampb.UnimplementedInternalHumanSessionServiceServer
	record string
}

func (s *sessionRecordIdentity) Resolve(_ context.Context, in *iampb.ResolveHumanSessionRequest) (*iampb.ResolveHumanSessionResponse, error) {
	if in.GetBearer() != sessionRecordCarrier {
		return &iampb.ResolveHumanSessionResponse{Found: false}, nil
	}
	at := time.Now().Add(-time.Minute)
	return &iampb.ResolveHumanSessionResponse{Found: true, Session: &iampb.HumanSession{
		UserId: sessionRecordUser, Email: "s3126@example.test", DisplayName: "S3126",
		AuthenticatedAt: timestamppb.New(at), ExpiresAt: timestamppb.New(at.Add(time.Hour)),
		AssuranceLevel: "1", EmailVerified: true, SessionId: s.record,
	}}, nil
}

// revokeRecorder — дублёр `AccessKeyService.Revoke`: запоминает значения
// ключа номера записи в метаданных вызова.
type revokeRecorder struct {
	iampb.UnimplementedAccessKeyServiceServer
	got chan []string
}

func (r *revokeRecorder) Revoke(ctx context.Context, _ *iampb.RevokeAccessKeyRequest) (*operationv1.Operation, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	r.got <- md.Get(principalwire.MetaTokenSessionID)
	return &operationv1.Operation{Id: "iop00000000000003126"}, nil
}

func serveGRPC(t *testing.T, register func(*grpc.Server)) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	register(gs)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	return lis.Addr().String()
}

type sessionRecordLookup struct{}

func (sessionRecordLookup) LookupByExternalID(context.Context, string) (middleware.Subject, error) {
	return middleware.Subject{}, nil
}

// edgeWithSessionLane — край: полоса нашей сессии над боевым мостом.
func edgeWithSessionLane(t *testing.T, record string) (http.Handler, *revokeRecorder) {
	t.Helper()
	identity := serveGRPC(t, func(gs *grpc.Server) {
		iampb.RegisterInternalHumanSessionServiceServer(gs, &sessionRecordIdentity{record: record})
	})
	conn, err := grpc.NewClient(identity, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial identity: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	rec := &revokeRecorder{got: make(chan []string, 1)}
	addrs := geoMuxAddrs()
	addrs["iam"] = serveGRPC(t, func(gs *grpc.Server) { iampb.RegisterAccessKeyServiceServer(gs, rec) })
	mux, err := NewMux(context.Background(), addrs, nil, nil, 30*time.Second)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", sessionRecordLookup{},
		slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithHumanSession(clients.NewSessionRevocationsAdapter(conn, 5*time.Second))
	return auth.HTTP(mux), rec
}

// TestEdgeForwardsTheSessionRecordAndDropsTheClientOne — номер записи текущей
// сессии доезжает до службы ровно одним значением — тем, что назвала служба;
// клиентское значение не доходит ни в одном кейсе.
func TestEdgeForwardsTheSessionRecordAndDropsTheClientOne(t *testing.T) {
	cases := []struct {
		name    string
		record  string            // номер записи в ответе службы о носителе
		carrier bool              // предъявлен ли наш носитель
		forged  map[string]string // клиентские заголовки
		want    []string          // что увидела служба
	}{
		{name: "session", record: sessionRecordLive, carrier: true, want: []string{sessionRecordLive}},
		{name: "session+forged-bridge-form", record: sessionRecordLive, carrier: true,
			forged: map[string]string{principalwire.HeaderGRPCMetaTokenSessionID: sessionRecordForged},
			want:   []string{sessionRecordLive}},
		{name: "session+forged-bare-form", record: sessionRecordLive, carrier: true,
			forged: map[string]string{principalwire.HeaderTokenSessionID: sessionRecordForged},
			want:   []string{sessionRecordLive}},
		{name: "no-carrier+forged", record: sessionRecordLive, carrier: false,
			forged: map[string]string{
				principalwire.HeaderGRPCMetaTokenSessionID: sessionRecordForged,
				principalwire.HeaderTokenSessionID:         sessionRecordForged,
			},
			want: nil},
		{name: "service-names-no-record+forged", record: "", carrier: true,
			forged: map[string]string{principalwire.HeaderGRPCMetaTokenSessionID: sessionRecordForged},
			want:   nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edge, rec := edgeWithSessionLane(t, tc.record)
			req := httptest.NewRequest(http.MethodDelete,
				"/iam/v1/users/"+sessionRecordUser+"/accessKeys/akey0000000000003126", nil)
			if tc.carrier {
				req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: sessionRecordCarrier})
			}
			for k, v := range tc.forged {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			edge.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("DELETE accessKey: http %d, body %s", w.Code, w.Body.String())
			}
			var got []string
			select {
			case got = <-rec.got:
			default:
				t.Fatal("AccessKeyService.Revoke не вызван — край не довёл запрос до службы")
			}
			if len(got) != len(tc.want) || (len(got) == 1 && got[0] != tc.want[0]) {
				t.Errorf("служба увидела %s = %q, want %q", principalwire.MetaTokenSessionID, got, tc.want)
			}
			for _, v := range got {
				if v == sessionRecordForged {
					t.Errorf("клиентское значение %q доехало до службы", v)
				}
			}
		})
	}
}
