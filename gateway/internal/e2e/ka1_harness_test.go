// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ka1_harness_test.go — харнесс слоя аутентификации края для приёмки KA1
// (`docs/specs/sub-phase-KA1-edge-refusals-and-call-budgets-acceptance.md`,
// раздел «Посев», строки П1–П5, П8).
//
// # Что поднято и почему настоящее
//
// Слой аутентификации края — `AuthInterceptor.HTTP` и `AuthInterceptor.Unary` в
// боевом режиме — стоит перед пробным обработчиком, отвечающим `200` / `OK`.
// REST идёт через настоящий http-сервер, нативная поверхность — через настоящий
// TLS-слушатель: ограничения живут в транспорте и в форме ответа, и проба над
// собранным в памяти входом о них не узнала бы.
//
// Служба доступа (П3) — НАСТОЯЩИЙ gRPC-сервер с контрактом внутреннего
// слушателя, а край спрашивает его НАСТОЯЩИМИ адаптерами
// (`clients.SessionRevocationsAdapter`, `middleware.NewBasicAuthorityFromStub`):
// перевод кодов транспорта в исходы полосы принадлежит адаптеру, и дублёр порта
// вместо соседа этого перевода не исполнил бы.
//
// Проверка прав (authz) в сборке не участвует: «200» здесь значит «запрос прошёл
// слой аутентификации и дошёл до обработчика».
package e2e_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/PRO-Robotech/corelib/credsecret"
	operation "github.com/PRO-Robotech/corelib/api/corelib/operation"
	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

// Значения посева (П3, П4) — дословно из приёмки.
const (
	ka1User           = "usr-00000000000000ka1"
	ka1SessionLive    = "ka1-live"
	ka1SessionUnknown = "ka1-unknown"
	ka1BasicID        = "bas-00000000000000ka1"
	ka1JTILive        = "jti-ka1-live"
	ka1JTIRevoked     = "jti-ka1-revoked"

	ka1UndeclaredIssuer = "https://undeclared.example.test"

	// ka1ListRoute — маршрут пути запроса без пола уверенности (П5).
	ka1ListRoute = "/iam/v1/accounts"
	// ka1FloorRoute — глагол с полом «2» встроенного каталога (П5).
	ka1FloorRoute = "/iam/v1/users/" + ka1User + "/tokens"
	// Нативные глаголы харнесса: без пола и с полом (П5).
	ka1PingMethod  = "/kaname.cloud.iam.v1.ProbeService/Ping"
	ka1FloorMethod = "/kaname.cloud.iam.v1.UserTokenService/Issue"

	// ka1ProbeDeadline — «Срок пробы»: проба, ждущая ответа края, держит свой
	// срок 3s; его истечение — исход строки (край висит), а не поломка пробы.
	ka1ProbeDeadline = 3 * time.Second
)

// Ответы, которые утверждает приёмка, — дословно (Р1, Р2). Проба держит их
// литералом, а не берёт у производителя: утверждается текст приёмки, и общий
// с продуктом источник сделал бы пробу согласной с любой его правкой.
const (
	ka1UnavailableBody = `{"code":14,"message":"credential state could not be established"}`
	ka1UnavailableText = "credential state could not be established"
	ka1RefusalBody     = `{"code":16,"message":"authentication failed","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"AUTHN_REQUIRED","domain":"kaname.cloud.iam.v1"}]}`
	ka1RefusalChallenge = `Bearer realm="kacho", error="invalid_token"`
	ka1RefusalText      = "authentication failed"
)

// ka1State — состояние вопроса службе (П1, П3).
type ka1State int32

const (
	ka1Answers ka1State = iota
	ka1Silent
	ka1Unavailable
	ka1Unimplemented
)

func (s ka1State) String() string {
	return [...]string{"отвечает", "молчит", "UNAVAILABLE", "UNIMPLEMENTED"}[s]
}

// ka1Question — один вопрос края соседу: состояние, управляемая задержка ответа,
// счёт принятых и освобождение молчащего ответа пробой.
type ka1Question struct {
	state   atomic.Int32
	delay   atomic.Int64
	asked   atomic.Int64
	release chan struct{}
}

func newKA1Question() *ka1Question { return &ka1Question{release: make(chan struct{})} }

func (q *ka1Question) set(s ka1State) { q.state.Store(int32(s)) }

// gate — что сосед делает с вопросом ДО ответа. nil — отвечать.
//
// «Молчит» — соединение принято, ответа нет до освобождения пробой (или пока
// спрашивающий сам не оборвёт вызов). Освобождение ответа не даёт: молчавший
// сосед ответить уже не успел.
func (q *ka1Question) gate(ctx context.Context) error {
	q.asked.Add(1)
	switch ka1State(q.state.Load()) {
	case ka1Silent:
		select {
		case <-q.release:
		case <-ctx.Done():
		}
		return status.Error(codes.Unavailable, "doubler: released without an answer")
	case ka1Unavailable:
		return status.Error(codes.Unavailable, "doubler: unavailable")
	case ka1Unimplemented:
		return status.Error(codes.Unimplemented, "doubler: question not offered")
	}
	if d := time.Duration(q.delay.Load()); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		}
	}
	return nil
}

// ─── П1: авторитет отзыва нашей чеканки ──────────────────────────────────────

// ka1Authority — авторитет отзыва формы RFC 7662: «жив» / «отозван» по `jti`,
// «молчит», UNAVAILABLE.
type ka1Authority struct {
	url string
	q   *ka1Question
}

func newKA1Authority(t *testing.T) *ka1Authority {
	t.Helper()
	a := &ka1Authority{q: newKA1Question()}
	srv := privateloopback.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := a.q.gate(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"unavailable"}`))
			return
		}
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"active": jtiOf(r.Form.Get("token")) != ka1JTIRevoked})
	}))
	t.Cleanup(srv.Close)
	// Освобождение — раньше закрытия сервера (очистки идут в обратном порядке):
	// закрытие ждёт ответа на каждый принятый запрос.
	t.Cleanup(func() { close(a.q.release) })
	a.url = srv.URL + "/internal/tokens/introspect"
	return a
}

// ─── П3: служба доступа на внутреннем слушателе ──────────────────────────────

// ka1Identity — дублёр службы доступа: сессия, отсечка, запись отзыва, годность
// базового удостоверения и отзыв при выходе, у каждого вопроса своё состояние.
type ka1Identity struct {
	iamv1.UnimplementedInternalHumanSessionServiceServer
	iamv1.UnimplementedInternalSessionRevocationsServiceServer
	iamv1.UnimplementedInternalIAMServiceServer

	resolve, cutoff, isRevoked, basic, revoke *ka1Question

	t0 time.Time
	// cutoffAtT0 — отсечка субъекта равна T0 (иначе отсечки нет).
	cutoffAtT0 atomic.Bool
	// noAuthInstant — ответ о сессии без момента аутентификации.
	noAuthInstant atomic.Bool
	// basicSecrets — предъявленная строка каждого известного удостоверения.
	basicSecrets map[string]string

	mu      sync.Mutex
	revoked []*iamv1.RevokeRequest

	conn *grpc.ClientConn
}

func newKA1Identity(t *testing.T) *ka1Identity {
	t.Helper()
	id := &ka1Identity{
		resolve: newKA1Question(), cutoff: newKA1Question(), isRevoked: newKA1Question(),
		basic: newKA1Question(), revoke: newKA1Question(),
		t0:           time.Now().Add(-time.Hour).Truncate(time.Microsecond),
		basicSecrets: map[string]string{},
	}
	lis := privateloopback.Listen(t)
	srv := grpc.NewServer()
	iamv1.RegisterInternalHumanSessionServiceServer(srv, id)
	iamv1.RegisterInternalSessionRevocationsServiceServer(srv, id)
	iamv1.RegisterInternalIAMServiceServer(srv, id)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	t.Cleanup(func() {
		for _, q := range []*ka1Question{id.resolve, id.cutoff, id.isRevoked, id.basic, id.revoke} {
			close(q.release)
		}
	})
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	id.conn = conn
	return id
}

// mintBasic чеканит базовое удостоверение и, если known, сообщает его службе.
func (id *ka1Identity) mintBasic(t *testing.T, credentialID string, known bool) string {
	t.Helper()
	s, _, err := credsecret.Mint(credentialID)
	require.NoError(t, err)
	if known {
		id.basicSecrets[credentialID] = s
	}
	return s
}

func (id *ka1Identity) Resolve(ctx context.Context, in *iamv1.ResolveHumanSessionRequest) (*iamv1.ResolveHumanSessionResponse, error) {
	if err := id.resolve.gate(ctx); err != nil {
		return nil, err
	}
	if in.GetBearer() != ka1SessionLive {
		return &iamv1.ResolveHumanSessionResponse{Found: false}, nil
	}
	s := &iamv1.HumanSession{
		UserId: ka1User, Email: "ka1@example.test", DisplayName: "KA1",
		ExpiresAt:      timestamppb.New(time.Now().Add(time.Hour)),
		AssuranceLevel: "1", EmailVerified: true,
	}
	if !id.noAuthInstant.Load() {
		s.AuthenticatedAt = timestamppb.New(id.t0)
	}
	return &iamv1.ResolveHumanSessionResponse{Found: true, Session: s}, nil
}

func (id *ka1Identity) SessionCutoffOf(ctx context.Context, _ *iamv1.SessionCutoffOfRequest) (*iamv1.SessionCutoffOfResponse, error) {
	if err := id.cutoff.gate(ctx); err != nil {
		return nil, err
	}
	if !id.cutoffAtT0.Load() {
		return &iamv1.SessionCutoffOfResponse{Found: false}, nil
	}
	return &iamv1.SessionCutoffOfResponse{Found: true, RevokeBefore: timestamppb.New(id.t0)}, nil
}

func (id *ka1Identity) IsRevoked(ctx context.Context, in *iamv1.IsRevokedRequest) (*iamv1.IsRevokedResponse, error) {
	if err := id.isRevoked.gate(ctx); err != nil {
		return nil, err
	}
	return &iamv1.IsRevokedResponse{Revoked: in.GetTokenJti() == ka1JTIRevoked}, nil
}

func (id *ka1Identity) Revoke(ctx context.Context, in *iamv1.RevokeRequest) (*operation.Operation, error) {
	if err := id.revoke.gate(ctx); err != nil {
		return nil, err
	}
	id.mu.Lock()
	id.revoked = append(id.revoked, in)
	id.mu.Unlock()
	return &operation.Operation{Id: "op-ka1", Done: true}, nil
}

func (id *ka1Identity) ResolveBasicCredential(ctx context.Context, in *iamv1.ResolveBasicCredentialRequest) (*iamv1.ResolveBasicCredentialResponse, error) {
	if err := id.basic.gate(ctx); err != nil {
		return nil, err
	}
	p, err := credsecret.Parse(in.GetPresented())
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "credential refused")
	}
	if want, ok := id.basicSecrets[p.CredentialID]; !ok || want != in.GetPresented() {
		return nil, status.Error(codes.Unauthenticated, "credential refused")
	}
	return &iamv1.ResolveBasicCredentialResponse{
		PrincipalType: "user", PrincipalId: ka1User, DisplayName: "KA1",
		CredentialId: p.CredentialID, ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)),
	}, nil
}

// ─── П8: сертификаты нативной поверхности ────────────────────────────────────

type ka1Cert struct {
	tls   tls.Certificate
	thumb string // x5t#S256 — base64url(sha256(DER))
}

func newKA1Cert(t *testing.T, cn string, server bool) ka1Cert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		tmpl.DNSNames = []string{"ka1.example.test"}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	sum := sha256.Sum256(der)
	return ka1Cert{
		tls:   tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		thumb: base64.RawURLEncoding.EncodeToString(sum[:]),
	}
}

// ─── харнесс ─────────────────────────────────────────────────────────────────

type ka1Options struct {
	// requireBinding — объявлена обязательная привязка машинных токенов (строка
	// (ж), образец newF1bStandWithRequirement).
	requireBinding bool
	// dpop — включён KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP: смонтированы
	// DPoPMiddleware и NewCnfBindingInterceptor, как в main.go.
	dpop bool
}

type ka1Stand struct {
	restURL  string
	grpcAddr string
	srvCA    *x509.CertPool

	ours, legacy, undeclared *f1bSigner
	ourAuth                  *ka1Authority
	ident                    *ka1Identity

	certA, certB ka1Cert

	basicGood, basicUnknownID, basicWrongSecret string
}

// ka1Lookup — резолв субъекта по `sub` для токена без утверждений принципала
// (строка (е) и её близнец): полоса предъявителя спрашивает его на запасной
// ветви.
type ka1Lookup struct{}

func (ka1Lookup) LookupByExternalID(_ context.Context, ext string) (middleware.Subject, error) {
	return middleware.Subject{Type: "user", ID: ka1User, DisplayName: ext}, nil
}

func newKA1Stand(t *testing.T, opt ka1Options) *ka1Stand {
	t.Helper()
	st := &ka1Stand{
		ours:       newF1bSigner(t, f1bPlatformIssuer, "ka1-ours"),
		legacy:     newF1bSigner(t, f1bLegacyIssuer, "ka1-legacy"),
		undeclared: newF1bSigner(t, ka1UndeclaredIssuer, "ka1-undeclared"),
		ourAuth:    newKA1Authority(t),
		ident:      newKA1Identity(t),
		certA:      newKA1Cert(t, "ka1-a", false),
		certB:      newKA1Cert(t, "ka1-b", false),
	}
	st.basicGood = st.ident.mintBasic(t, ka1BasicID, true)
	st.basicUnknownID = st.ident.mintBasic(t, "bas-0000000000unknown", false)
	// Неверный секрет к ИЗВЕСТНОМУ идентификатору: вторая чеканка того же
	// идентификатора службе не сообщается.
	st.basicWrongSecret = st.ident.mintBasic(t, ka1BasicID, false)
	require.NotEqual(t, st.basicGood, st.basicWrongSecret)

	verifier, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers: []middleware.IssuerKeySet{
			{Issuer: f1bLegacyIssuer, KeySetURL: st.legacy.url,
				TokenTypes:              []string{middleware.LegacyTokenType, middleware.PlatformTokenType},
				TolerateAbsentTokenType: true},
			{Issuer: f1bPlatformIssuer, KeySetURL: st.ours.url,
				TokenTypes: []string{middleware.PlatformTokenType}, ReadRevocation: true},
		},
		ExpectedAudience: testAudience,
	})
	require.NoError(t, err)

	// Читатель отзыва нашей чеканки — тот же, что собирает процесс; его срок —
	// умолчание загрузчика KACHO_INTROSPECTION_TIMEOUT_MS (1000).
	platformIntrospection, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		IntrospectionURL: st.ourAuth.url, TTL: time.Millisecond, Timeout: time.Second,
	})
	require.NoError(t, err)

	adapter := clients.NewSessionRevocationsAdapter(st.ident.conn)
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	auth := middleware.NewAuthInterceptor(middleware.AuthModeProduction, "", ka1Lookup{}, logger).
		WithVerifier(verifier).
		WithRevocationCheck(middleware.NewOwnRevocationSource(adapter), time.Hour).
		WithPlatformRevocationCheck(platformIntrospection, time.Hour).
		WithHumanSession(adapter).
		WithSessionCutoffCheck(adapter, time.Hour).
		WithBasicCredentialLane(middleware.NewBasicCredentialLane(
			middleware.NewBasicAuthorityFromStub(iamv1.NewInternalIAMServiceClient(st.ident.conn)))).
		WithRequireMachineTokenBinding(opt.requireBinding).
		WithStepUp(middleware.NewStepUpGate(nil), middleware.NewCatalogPermissionLookup(catalog), middleware.NewRestRouter())

	var probe http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	unary := []grpc.UnaryServerInterceptor{auth.Unary()}
	if opt.dpop {
		dv, derr := middleware.NewDPoPValidator(middleware.DPoPValidatorConfig{
			ReplayCache:  middleware.NewDPoPReplayCache(middleware.DPoPReplayCacheConfig{MaxEntries: 1024, TTL: 2 * time.Minute}),
			IatFreshness: time.Minute,
		})
		require.NoError(t, derr)
		mw, merr := middleware.NewDPoPMiddleware(middleware.DPoPMiddlewareConfig{
			Verifier: verifier, DPoP: dv, MTLS: middleware.NewMTLSBoundValidator(),
			StepUp:           middleware.NewStepUpGate(time.Now),
			PermissionLookup: middleware.NewCatalogPermissionLookup(catalog),
			RestRouter:       middleware.NewRestRouter(),
			Logger:           logger, APIDomain: apiDomain,
		})
		require.NoError(t, merr)
		probe = mw.Wrap(probe)
		cnf, cerr := middleware.NewCnfBindingInterceptor(verifier, middleware.NewMTLSBoundValidator(), logger)
		require.NoError(t, cerr)
		unary = append(unary, cnf.Unary())
	}

	rest := privateloopback.NewServer(t, auth.HTTP(probe))
	t.Cleanup(rest.Close)
	st.restURL = rest.URL

	srvCert := newKA1Cert(t, "ka1-edge", true)
	st.srvCA = x509.NewCertPool()
	leaf, err := x509.ParseCertificate(srvCert.tls.Certificate[0])
	require.NoError(t, err)
	st.srvCA.AddCert(leaf)
	lis := privateloopback.Listen(t)
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{srvCert.tls},
			// Клиентский сертификат запрашивается, но не обязателен: сверку
			// привязки с ним делает перехватчик привязки (П8), а не слушатель.
			ClientAuth: tls.RequestClientCert,
			MinVersion: tls.VersionTLS12,
		})),
		grpc.ChainUnaryInterceptor(unary...),
	)
	for _, m := range []string{ka1PingMethod, ka1FloorMethod} {
		svc, name, _ := strings.Cut(strings.TrimPrefix(m, "/"), "/")
		full := m
		srv.RegisterService(&grpc.ServiceDesc{
			ServiceName: svc, HandlerType: (*any)(nil), Metadata: "ka1",
			Methods: []grpc.MethodDesc{{MethodName: name,
				Handler: func(_ any, ctx context.Context, dec func(any) error, ic grpc.UnaryServerInterceptor) (any, error) {
					if err := dec(&emptyMsg{}); err != nil {
						return nil, err
					}
					h := func(context.Context, any) (any, error) { return &emptyMsg{}, nil }
					return ic(ctx, &emptyMsg{}, &grpc.UnaryServerInfo{FullMethod: full}, h)
				}}},
		}, struct{}{})
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	st.grpcAddr = lis.Addr().String()
	return st
}

// ─── предъявленные ───────────────────────────────────────────────────────────

// ourToken — токен нашего издателя (П1, П4) с утверждениями принципала.
func (s *ka1Stand) ourToken(t *testing.T, jti string, mutate func(jwt.MapClaims)) string {
	t.Helper()
	return s.ours.mint(t, middleware.PlatformTokenType, jti, func(c jwt.MapClaims) {
		c["sub"] = ka1User
		c["kaname_principal_id"] = ka1User
		if mutate != nil {
			mutate(c)
		}
	})
}

func (s *ka1Stand) legacyToken(t *testing.T, jti string) string {
	t.Helper()
	return s.legacy.mint(t, middleware.LegacyTokenType, jti, func(c jwt.MapClaims) {
		c["sub"] = ka1User
		c["kaname_principal_id"] = ka1User
	})
}

// tamper меняет один байт подписи.
func tamper(tok string) string {
	b := []byte(tok)
	i := len(b) - 3
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	return string(b)
}

// ─── вызовы ──────────────────────────────────────────────────────────────────

// ka1Shot — ответ REST-поверхности целиком. Заголовки — все, кроме двух,
// различных у любых двух ответов по построению (`Date`, `X-Request-Id`; Р2).
type ka1Shot struct {
	status   int
	header   http.Header
	body     []byte
	elapsed  time.Duration
	timedOut bool
}

func (s ka1Shot) String() string {
	if s.timedOut {
		return "ответа нет за " + ka1ProbeDeadline.String() + " (край висит)"
	}
	keys := make([]string, 0, len(s.header))
	for k := range s.header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(http.StatusText(s.status))
	for _, k := range keys {
		b.WriteString(" | " + k + ": " + strings.Join(s.header[k], " ; "))
	}
	b.WriteString(" | тело: " + string(bytes.TrimSpace(s.body)))
	return b.String()
}

// same — побайтовое равенство по правилу сравнения Р2.
func (s ka1Shot) same(o ka1Shot) bool {
	if s.timedOut || o.timedOut || s.status != o.status || !bytes.Equal(s.body, o.body) {
		return false
	}
	if len(s.header) != len(o.header) {
		return false
	}
	for k, v := range s.header {
		w, ok := o.header[k]
		if !ok || strings.Join(v, "\x00") != strings.Join(w, "\x00") {
			return false
		}
	}
	return true
}

type ka1Presented func(*http.Request)

func bearer(tok string) ka1Presented {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

func sessionCarrier(v string) ka1Presented {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: v})
	}
}

func nothing(*http.Request) {}

func (s *ka1Stand) rest(t *testing.T, method, path string, present ka1Presented) ka1Shot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), ka1ProbeDeadline)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, s.restURL+path, nil)
	require.NoError(t, err)
	present(req)
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ka1Shot{timedOut: true, elapsed: time.Since(start)}
		}
		require.NoError(t, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if errors.Is(err, context.DeadlineExceeded) {
		return ka1Shot{timedOut: true, elapsed: time.Since(start)}
	}
	require.NoError(t, err)
	h := resp.Header.Clone()
	h.Del("Date")
	h.Del("X-Request-Id")
	return ka1Shot{status: resp.StatusCode, header: h, body: body, elapsed: time.Since(start)}
}

// ka1Native — исход нативного вызова и его срок.
type ka1Native struct {
	st      *status.Status
	elapsed time.Duration
}

func (n ka1Native) String() string {
	b, _ := json.Marshal(n.st.Proto())
	return n.st.Code().String() + " " + string(b)
}

// grpc зовёт нативный метод харнесса по TLS; cert — клиентский сертификат
// соединения (nil — без него).
func (s *ka1Stand) grpc(t *testing.T, method, tok string, cert *ka1Cert) ka1Native {
	t.Helper()
	cfg := &tls.Config{RootCAs: s.srvCA, ServerName: "ka1.example.test", MinVersion: tls.VersionTLS12}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{cert.tls}
	}
	conn, err := grpc.NewClient(s.grpcAddr, grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), ka1ProbeDeadline)
	defer cancel()
	if tok != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+tok)
	}
	start := time.Now()
	err = conn.Invoke(ctx, method, &emptyMsg{}, &emptyMsg{})
	return ka1Native{st: status.Convert(err), elapsed: time.Since(start)}
}

// requireUnavailable — ответ Р1 на REST-поверхности.
func requireUnavailable(t *testing.T, where string, got ka1Shot) {
	t.Helper()
	if got.timedOut || got.status != http.StatusServiceUnavailable ||
		got.header.Get("Content-Type") != "application/json" ||
		string(bytes.TrimSpace(got.body)) != ka1UnavailableBody ||
		got.header.Get("WWW-Authenticate") != "" || len(got.header.Values("Set-Cookie")) != 0 {
		t.Errorf("%s: ответ не Р1 (503, JSON %s, без WWW-Authenticate и Set-Cookie)\n  получено: %s",
			where, ka1UnavailableBody, got)
	}
}

// requireNativeUnavailable — ответ Р1 на нативной поверхности: тот же текст, без деталей.
func requireNativeUnavailable(t *testing.T, where string, got ka1Native) {
	t.Helper()
	if got.st.Code() != codes.Unavailable || got.st.Message() != ka1UnavailableText || len(got.st.Details()) != 0 {
		t.Errorf("%s: нативный ответ не Р1 (UNAVAILABLE %q, без деталей)\n  получено: %s",
			where, ka1UnavailableText, got)
	}
}

// requireRefusal — отказ Р2 на REST-поверхности.
func requireRefusal(t *testing.T, where string, got ka1Shot, carrierEndings bool) {
	t.Helper()
	ok := !got.timedOut && got.status == http.StatusUnauthorized &&
		got.header.Get("Content-Type") == "application/json" &&
		got.header.Get("WWW-Authenticate") == ka1RefusalChallenge &&
		string(bytes.TrimSpace(got.body)) == ka1RefusalBody
	if carrierEndings {
		ok = ok && sameCookies(got.header.Values("Set-Cookie"), carrierEndingLines())
	} else {
		ok = ok && len(got.header.Values("Set-Cookie")) == 0
	}
	if !ok {
		t.Errorf("%s: отказ не Р2 (401, WWW-Authenticate %s, тело %s, гашение носителя=%v)\n  получено: %s",
			where, ka1RefusalChallenge, ka1RefusalBody, carrierEndings, got)
	}
}

// requireNativeRefusal — отказ Р2 на нативной поверхности.
func requireNativeRefusal(t *testing.T, where string, got ka1Native) {
	t.Helper()
	ok := got.st.Code() == codes.Unauthenticated && got.st.Message() == ka1RefusalText
	ds := got.st.Proto().GetDetails()
	if ok && len(ds) == 1 {
		info := errorInfoOf(got.st)
		ok = info != nil && info.GetReason() == "AUTHN_REQUIRED" &&
			info.GetDomain() == "kaname.cloud.iam.v1" && len(info.GetMetadata()) == 0
	} else {
		ok = false
	}
	if !ok {
		t.Errorf("%s: нативный отказ не Р2 (UNAUTHENTICATED %q, одна ErrorInfo AUTHN_REQUIRED/kaname.cloud.iam.v1 без metadata)\n  получено: %s",
			where, ka1RefusalText, got)
	}
}

func marshalStatus(t *testing.T, st *status.Status) []byte {
	t.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(st.Proto())
	require.NoError(t, err)
	return b
}

// carrierEndingLines — набор Set-Cookie, который производит SessionCarrierEndings().
func carrierEndingLines() []string {
	var out []string
	for _, c := range middleware.SessionCarrierEndings() {
		out = append(out, c.String())
	}
	return out
}

func sameCookies(a, b []string) bool {
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, "\n") == strings.Join(b, "\n")
}
