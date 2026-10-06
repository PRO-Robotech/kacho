// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f6b_address_gate_e2e_test.go — приёмка F6b, стадия S1 (край, kacho#2900):
// сценарии, которым нужна вся цепочка края, — полосы личности, решение по
// каталогу прав и ответ владельца прав через настоящий клиент.
//
//   - F6b-45 — решение по каталогу прав отвечает «нет» с причиной службы
//     `email_not_verified`: край произносит значение отказа адреса (Р3а), и
//     одно и то же для любого объекта, раньше скрытия существования;
//   - F6b-35 — удостоверение, которому служба отказывает на предъявлении
//     (базовый секрет, наш токен человека), дальше края не уходит ни на одной
//     поверхности (Р16). Поведение существует до кода, сценарий держит его как
//     опору правила.
//
// # Почему через настоящее соединение
//
// Та же причина, что у Ф1б (f1b_two_issuer_e2e_test.go): ограничения живут в
// разборе, транспорте и форме ответа. REST идёт через настоящий http-сервер,
// нативная поверхность — через настоящий слушатель TCP, а вопрос о праве —
// через клиента края к дублёру службы прав по gRPC: причина отказа проходит тот
// же адаптер, что на стенде.
package e2e_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/PRO-Robotech/corelib/credsecret"
	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/e2e/ka1stand"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// f6bAddressRefusal — отказ адреса побайтово: значение службы
// (`loginlanehttp.writeRefusal` у kaname), без `metadata` (приёмка F6b, Р3).
const f6bAddressRefusal = `{"code":7,"message":"email address is not verified: confirm it with the code from the letter (POST /iam/v1/auth/verify-email/confirm)","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"EMAIL_NOT_VERIFIED","domain":"iam.kaname.cloud"}]}`

const (
	f6bProject        = "prj00000000000000f6b"
	f6bNetworkPresent = "enp5d8a0c1b2e3f4g5h6"
	f6bNetworkAbsent  = "enp0000000000000000x"
	f6bHuman          = "usr0000000000000f6b1"
)

// f6bSessionReader — дублёр `InternalHumanSessionService.Resolve` на один
// вопрос: живая сессия с ПОДТВЕРЖДЁННЫМ адресом. Отметку сняли между разбором
// сессии и вопросом о праве (Р3а) — это вход, который строит F6b-45 (а).
type f6bSessionReader struct{ asked atomic.Int64 }

func (f *f6bSessionReader) ResolveHumanSession(_ context.Context, _ string) (middleware.HumanSession, bool, error) {
	f.asked.Add(1)
	return middleware.HumanSession{
		UserID:          f6bHuman,
		Email:           "f6b@example.com",
		DisplayName:     "F6b",
		AuthenticatedAt: time.Now().Add(-time.Minute),
		ExpiresAt:       time.Now().Add(time.Hour),
		AssuranceLevel:  "1",
		EmailVerified:   true,
	}, true, nil
}

// f6bBasicAuthority — дублёр авторитета базового секрета: отказывает так, как
// служба ответит владельцу-человеку с неподтверждённым адресом (Р5 службы:
// `UNAUTHENTICATED`, `credential refused`), либо признаёт секрет годным.
type f6bBasicAuthority struct {
	refuse atomic.Bool
	calls  atomic.Int64
}

func (a *f6bBasicAuthority) Resolve(_ context.Context, presented string) (*iamv1.ResolveBasicCredentialResponse, error) {
	a.calls.Add(1)
	p, err := credsecret.Parse(presented)
	if err != nil || a.refuse.Load() {
		return nil, status.Error(codes.Unauthenticated, "credential refused")
	}
	return &iamv1.ResolveBasicCredentialResponse{
		PrincipalType: "user",
		PrincipalId:   f6bHuman,
		DisplayName:   "F6b",
		CredentialId:  p.CredentialID,
	}, nil
}

// f6bStand — край с полосой сессии, полосой базового секрета и полосой
// предъявителя нашего издателя, решением по настоящему каталогу прав и ОБЕИМИ
// поверхностями. Следующее звено на каждой поверхности считает дошедшие запросы.
type f6bStand struct {
	restURL    string
	grpcAddr   string
	auth       *middleware.AuthInterceptor
	ours       *f1bSigner
	ourAuth    *f1bAuthority
	recordAuth *f1bAuthority
	basic      *f6bBasicAuthority
	session    *f6bSessionReader
	authz      *authzStub
	restHits   *atomic.Int64
	grpcHits   *atomic.Int64
}

func newF6bStand(t *testing.T) *f6bStand {
	t.Helper()
	st := &f6bStand{
		ours:       newF1bSigner(t, f1bPlatformIssuer, "ours-es256"),
		ourAuth:    newF1bAuthority(t),
		recordAuth: newF1bAuthority(t),
		basic:      &f6bBasicAuthority{},
		session:    &f6bSessionReader{},
		restHits:   &atomic.Int64{},
		grpcHits:   &atomic.Int64{},
	}

	verifier, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers: []middleware.IssuerKeySet{{
			Issuer: f1bPlatformIssuer, KeySetURL: st.ours.url,
			TokenTypes:     []string{middleware.PlatformTokenType},
			ReadRevocation: true,
		}},
		ExpectedAudience: testAudience,
	})
	require.NoError(t, err)
	// Окно кэша вердикта сверки — миллисекунда: проба меняет ответ дублёра между
	// обращениями и не должна читать прежний вердикт.
	ourCheck, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		IntrospectionURL: st.ourAuth.url, TTL: time.Millisecond, Timeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	recordCheck, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		IntrospectionURL: st.recordAuth.url, TTL: time.Millisecond, Timeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)

	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	st.auth = middleware.NewAuthInterceptor(middleware.AuthModeProduction, "", nil, logger).
		WithHumanSession(st.session).
		WithBasicCredentialLane(middleware.NewBasicCredentialLane(st.basic, time.Second)).
		WithVerifier(verifier).
		WithRevocationCheck(recordCheck, time.Hour).
		WithPlatformRevocationCheck(ourCheck, time.Hour)

	stub, addr, stop := startAuthzStub(t)
	t.Cleanup(stop)
	st.authz = stub
	rawClient, err := clients.NewIAMAuthorizeClient(clients.IAMAuthorizeClientConfig{
		Addr: addr, Timeout: 2 * time.Second, Logger: silentLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawClient.Close() })
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	router := middleware.NewRestRouter()
	authz, err := middleware.NewAuthzMiddleware(middleware.AuthzMiddlewareConfig{
		Enabled:         true,
		Catalog:         catalog,
		Subjects:        middleware.NewSubjectExtractor(true),
		Context:         middleware.NewContextExtractor(time.Now, true),
		Resources:       middleware.NewResourceExtractor(router.PathTemplates()),
		Checker:         clients.NewAuthzChecker(rawClient),
		Logger:          silentLogger(),
		CacheTTL:        5 * time.Second,
		CacheMaxEntries: 100,
		PublicAllowlist: middleware.DefaultPublicAllowlist(),
		RestRouter:      router,
	})
	require.NoError(t, err)

	rest := privateloopback.NewServer(t, st.auth.HTTP(authz.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		st.restHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))))
	t.Cleanup(rest.Close)
	st.restURL = rest.URL

	lis := privateloopback.Listen(t)
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(st.auth.Unary(), authz.Unary()))
	// Методы регистрируются вручную: предмет — полосы и решение края, а не тело
	// обработчика. Запрос материализован, как на унарной полосе края.
	probe := func(full string, newReq func() proto.Message) grpc.MethodDesc {
		return grpc.MethodDesc{
			MethodName: full[strings.LastIndexByte(full, '/')+1:],
			Handler: func(_ any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				in := newReq()
				if err := dec(in); err != nil {
					return nil, err
				}
				h := func(context.Context, any) (any, error) {
					st.grpcHits.Add(1)
					return &emptypb.Empty{}, nil
				}
				return interceptor(ctx, in, &grpc.UnaryServerInfo{FullMethod: full}, h)
			},
		}
	}
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "kacho.cloud.vpc.v1.NetworkService",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			probe("/kacho.cloud.vpc.v1.NetworkService/Create", func() proto.Message { return &vpcv1.CreateNetworkRequest{} }),
			probe("/kacho.cloud.vpc.v1.NetworkService/Get", func() proto.Message { return &vpcv1.GetNetworkRequest{} }),
		},
		Metadata: "f6b-probe",
	}, struct{}{})
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "kaname.cloud.iam.v1.ProjectService",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			probe("/kaname.cloud.iam.v1.ProjectService/List", func() proto.Message { return &iamv1.ListProjectsRequest{} }),
		},
		Metadata: "f6b-probe",
	}, struct{}{})
	go func() { _ = srv.Serve(lis) }()
	st.grpcAddr = lis.Addr().String()
	t.Cleanup(func() { srv.Stop(); _ = lis.Close() })
	return st
}

// f6bShot — ответ REST: статус, заголовки (кроме идентификатора запроса и
// `Date`) и тело.
type f6bShot struct {
	code   int
	header http.Header
	body   string
}

func (s f6bShot) String() string {
	var keys []string
	for k := range s.header {
		if k == "X-Request-Id" || k == "Date" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(http.StatusText(s.code))
	for _, k := range keys {
		b.WriteString("\n" + k + ": " + strings.Join(s.header[k], ","))
	}
	b.WriteString("\n\n" + s.body)
	return b.String()
}

// callREST — обращение к REST-поверхности через настоящее соединение.
// Удостоверение задаёт `present`: печенье сессии или заголовок предъявителя.
func (s *f6bStand) callREST(t *testing.T, method, path, body string, present func(*http.Request)) f6bShot {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.restURL+path, rd)
	require.NoError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	present(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return f6bShot{code: resp.StatusCode, header: resp.Header, body: string(raw)}
}

// callGRPC — обращение к нативной поверхности через настоящий сокет.
func (s *f6bStand) callGRPC(t *testing.T, fullMethod, bearer string, in proto.Message) *status.Status {
	t.Helper()
	conn, err := grpc.NewClient(s.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+bearer)
	return status.Convert(conn.Invoke(ctx, fullMethod, in, &emptypb.Empty{}))
}

func withSessionCarrier(req *http.Request) {
	req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "opaque-f6b"})
}

func withBearer(tok string) func(*http.Request) {
	return func(req *http.Request) { req.Header.Set("Authorization", "Bearer "+tok) }
}

// errorInfoOf — `google.rpc.ErrorInfo` статуса gRPC; nil, если его нет.
func errorInfoOf(st *status.Status) *errdetails.ErrorInfo {
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	return nil
}

// f6bPaths — три обращения F6b-45: создание в посеянном проекте (запись
// каталога спрашивает отношение на проекте) и два чтения сети, чей отказ край
// по каталогу произносит `404` скрытия существования.
var f6bPaths = []struct{ method, path, body string }{
	{http.MethodPost, "/vpc/v1/networks", `{"projectId":"` + f6bProject + `","name":"n1"}`},
	{http.MethodGet, "/vpc/v1/networks/" + f6bNetworkPresent, ""},
	{http.MethodGet, "/vpc/v1/networks/" + f6bNetworkAbsent, ""},
}

// requireF6bAddressRefusal — отказ адреса: 403, тело побайтово, без вызова на
// аутентификацию и без `Set-Cookie`.
func requireF6bAddressRefusal(t *testing.T, where string, got f6bShot) {
	t.Helper()
	require.Equal(t, http.StatusForbidden, got.code, "%s: %s", where, got.body)
	require.Equal(t, f6bAddressRefusal, got.body, "%s: тело отказа адреса не то побайтово", where)
	require.Empty(t, got.header.Get("WWW-Authenticate"), "%s: отказ адреса — не вызов на аутентификацию", where)
	require.Empty(t, got.header.Values("Set-Cookie"), "%s: отказ адреса не трогает носитель", where)
}

// ─── F6b-45 ─────────────────────────────────────────────────────────────────

// TestF6b45_DecisionRefusalWithTheServiceReasonIsTheAddressRefusalOnEveryObject —
// ответ решения `allowed = false`, `deny_reasons = ["email_not_verified"]` край
// произносит значением отказа адреса на ОБЕИХ полосах, выставляющих личность
// человека, и на обеих поверхностях; ответ одинаков для существующего и
// несуществующего объекта, потому что стоит раньше скрытия существования.
func TestF6b45_DecisionRefusalWithTheServiceReasonIsTheAddressRefusalOnEveryObject(t *testing.T) {
	st := newF6bStand(t)
	st.authz.allow.Store(false)
	st.authz.reasons.Store(&[]string{"email_not_verified"})

	token := st.ours.mint(t, middleware.PlatformTokenType, "jti-f6b45-live", func(c jwt.MapClaims) {
		c["sub"], c["kaname_principal_id"] = f6bHuman, f6bHuman
	})
	lanes := []struct {
		name    string
		present func(*http.Request)
	}{
		{"(а) носитель сессии, отметку сняли после разбора сессии", withSessionCarrier},
		{"(б) наш токен человека, вердикт сверки «действует»", withBearer(token)},
	}
	for _, lane := range lanes {
		checksBefore := st.authz.calls.Load()
		var shots []string
		for _, p := range f6bPaths {
			got := st.callREST(t, p.method, p.path, p.body, lane.present)
			requireF6bAddressRefusal(t, lane.name+" "+p.method+" "+p.path, got)
			shots = append(shots, got.String())
		}
		for i := 1; i < len(shots); i++ {
			require.Equal(t, shots[0], shots[i], "%s: отказ адреса различается по объекту — оракул того, чего он не читал", lane.name)
		}
		// Вердикт «нет» в кэш решений не попадает: каждое обращение спрошено
		// заново. Повтор тех же трёх обращений — ещё три вопроса, а не ноль.
		for _, p := range f6bPaths {
			st.callREST(t, p.method, p.path, p.body, lane.present)
		}
		require.Equal(t, int64(2*len(f6bPaths)), st.authz.calls.Load()-checksBefore,
			"%s: отказ с причиной адреса обязан спрашиваться у службы на каждом обращении", lane.name)
	}

	// Нативная поверхность — полоса предъявителя: форма gRPC того же значения.
	for _, call := range []struct {
		method string
		in     proto.Message
	}{
		{"/kacho.cloud.vpc.v1.NetworkService/Create", &vpcv1.CreateNetworkRequest{ProjectId: f6bProject, Name: "n1"}},
		{"/kacho.cloud.vpc.v1.NetworkService/Get", &vpcv1.GetNetworkRequest{NetworkId: f6bNetworkPresent}},
		{"/kacho.cloud.vpc.v1.NetworkService/Get", &vpcv1.GetNetworkRequest{NetworkId: f6bNetworkAbsent}},
	} {
		got := st.callGRPC(t, call.method, token, call.in)
		require.Equal(t, codes.PermissionDenied, got.Code(), "%s: %v", call.method, got.Err())
		require.Equal(t, "email address is not verified: confirm it with the code from the letter (POST /iam/v1/auth/verify-email/confirm)", got.Message(), call.method)
		info := errorInfoOf(got)
		require.NotNil(t, info, "%s: отказ адреса без ErrorInfo", call.method)
		require.Equal(t, "EMAIL_NOT_VERIFIED", info.GetReason(), call.method)
		require.Equal(t, "iam.kaname.cloud", info.GetDomain(), call.method)
		require.Empty(t, info.GetMetadata(), "%s: отказ адреса без metadata", call.method)
	}

	require.Zero(t, st.restHits.Load(), "запрос дошёл до следующего звена REST")
	require.Zero(t, st.grpcHits.Load(), "запрос дошёл до следующего звена нативной поверхности")
	require.Zero(t, st.auth.SessionLane().Snapshot().AddressNotVerified,
		"клетку отказа адреса растит рубеж полосы сессии, а этот отказ произнесён решением")
	require.Positive(t, st.session.asked.Load(), "предпосылка: полоса сессии спрошена")
}

// TestF6b45_Twin_AnyOtherReasonIsTheCatalogRefusalAsBefore — близнец F6b-45:
// та же полоса, причина `no path`. Ответы — как до этой под-фазы: `403` с
// `AUTHZ_DENIED` на создании и `404` скрытия существования на обоих чтениях;
// отказа адреса нет ни в одном.
func TestF6b45_Twin_AnyOtherReasonIsTheCatalogRefusalAsBefore(t *testing.T) {
	st := newF6bStand(t)
	st.authz.allow.Store(false)
	st.authz.reasons.Store(&[]string{"no path"})

	token := st.ours.mint(t, middleware.PlatformTokenType, "jti-f6b45-twin", func(c jwt.MapClaims) {
		c["sub"], c["kaname_principal_id"] = f6bHuman, f6bHuman
	})
	for _, present := range []func(*http.Request){withSessionCarrier, withBearer(token)} {
		create := st.callREST(t, f6bPaths[0].method, f6bPaths[0].path, f6bPaths[0].body, present)
		require.Equal(t, http.StatusForbidden, create.code, create.body)
		require.Contains(t, create.body, `"reason":"AUTHZ_DENIED"`, create.body)
		for _, p := range f6bPaths[1:] {
			got := st.callREST(t, p.method, p.path, p.body, present)
			require.Equal(t, http.StatusNotFound, got.code, "%s %s: %s", p.method, p.path, got.body)
			require.NotContains(t, got.body, "EMAIL_NOT_VERIFIED")
		}
		require.NotContains(t, create.body, "EMAIL_NOT_VERIFIED")
	}
	create := st.callGRPC(t, "/kacho.cloud.vpc.v1.NetworkService/Create", token,
		&vpcv1.CreateNetworkRequest{ProjectId: f6bProject, Name: "n1"})
	require.Equal(t, codes.PermissionDenied, create.Code())
	require.NotNil(t, errorInfoOf(create))
	require.Equal(t, "AUTHZ_DENIED", errorInfoOf(create).GetReason())
	read := st.callGRPC(t, "/kacho.cloud.vpc.v1.NetworkService/Get", token,
		&vpcv1.GetNetworkRequest{NetworkId: f6bNetworkAbsent})
	require.Equal(t, codes.NotFound, read.Code())
	require.Zero(t, st.restHits.Load()+st.grpcHits.Load())
}

// ─── F6b-35 ─────────────────────────────────────────────────────────────────

// TestF6b35_CredentialRefusedOnPresentationGoesNoFurtherOnBothSurfaces — полосы
// Л2 и Л3 (Р16): держатель правила — служба на предъявлении, край исполняет её
// ответ своим отказом полосы. Поведение края существует до этой под-фазы.
func TestF6b35_CredentialRefusedOnPresentationGoesNoFurtherOnBothSurfaces(t *testing.T) {
	st := newF6bStand(t)
	st.authz.allow.Store(true)

	// Л2 — базовый секрет: служба отказывает владельцу-человеку с
	// неподтверждённым адресом (Р5 службы).
	st.basic.refuse.Store(true)
	secret, _, err := credsecret.Mint("uoc_000000000000f6b35")
	require.NoError(t, err)
	l2 := st.callREST(t, http.MethodGet, "/iam/v1/projects", "", withBearer(secret))
	require.Equal(t, http.StatusUnauthorized, l2.code)
	// Единый отказ края (приёмка KA1, Р2) — тот же, что у любой причины.
	require.Equal(t, ka1stand.RefusalBody, strings.TrimSpace(l2.body))
	require.Equal(t, ka1stand.RefusalChallenge, l2.header.Get("WWW-Authenticate"))
	l2n := st.callGRPC(t, "/kaname.cloud.iam.v1.ProjectService/List", secret, &iamv1.ListProjectsRequest{})
	require.Equal(t, codes.Unauthenticated, l2n.Code())
	require.Equal(t, ka1stand.RefusalText, l2n.Message(), "один текст отказа на обеих поверхностях")

	// Л3 — наш токен человека: сверка нашего авторитета отвечает «не действует»
	// (`{"active": false}`, Р5а службы).
	st.ourAuth.revoked.Store(true)
	refused := st.ours.mint(t, middleware.PlatformTokenType, "jti-f6b35-refused", func(c jwt.MapClaims) {
		c["sub"], c["kaname_principal_id"] = f6bHuman, f6bHuman
	})
	l3 := st.callREST(t, http.MethodGet, "/iam/v1/projects", "", withBearer(refused))
	require.Equal(t, http.StatusUnauthorized, l3.code)
	require.Equal(t, ka1stand.RefusalBody, strings.TrimSpace(l3.body))
	require.Equal(t, ka1stand.RefusalChallenge, l3.header.Get("WWW-Authenticate"))
	l3n := st.callGRPC(t, "/kaname.cloud.iam.v1.ProjectService/List", refused, &iamv1.ListProjectsRequest{})
	require.Equal(t, codes.Unauthenticated, l3n.Code())
	require.Equal(t, "authentication failed", l3n.Message(),
		"на нативной поверхности — единый текст всякой неудачи подлинности, а не текст отзыва")

	require.Zero(t, st.restHits.Load()+st.grpcHits.Load(), "удостоверение, отвергнутое службой, дошло до следующего звена")
	require.Zero(t, st.recordAuth.asked.Load(), "о токене нашей записи край спрашивает только сверку")
	require.Positive(t, st.ourAuth.asked.Load(), "предпосылка: сверку нашего авторитета спросили")

	// Близнец: секрет годен, токен действует — каждое обращение дошло до
	// следующего звена, по одному.
	st.basic.refuse.Store(false)
	st.ourAuth.revoked.Store(false)
	live := st.ours.mint(t, middleware.PlatformTokenType, "jti-f6b35-live", func(c jwt.MapClaims) {
		c["sub"], c["kaname_principal_id"] = f6bHuman, f6bHuman
	})
	for _, bearer := range []string{secret, live} {
		got := st.callREST(t, http.MethodGet, "/iam/v1/projects", "", withBearer(bearer))
		require.Equal(t, http.StatusOK, got.code, got.body)
		require.Equal(t, codes.OK, st.callGRPC(t, "/kaname.cloud.iam.v1.ProjectService/List", bearer, &iamv1.ListProjectsRequest{}).Code())
	}
	require.Equal(t, int64(2), st.restHits.Load())
	require.Equal(t, int64(2), st.grpcHits.Load())
	require.Zero(t, st.recordAuth.asked.Load())
}
