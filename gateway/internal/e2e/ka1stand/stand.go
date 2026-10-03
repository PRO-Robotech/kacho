// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package ka1stand — харнесс слоя аутентификации края для приёмки KA1
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
package ka1stand

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
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	operation "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/credsecret"
	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

// Значения посева (П3, П4) — дословно из приёмки.
const (
	User           = "usr-00000000000000ka1"
	SessionLive    = "ka1-live"
	SessionUnknown = "ka1-unknown"
	BasicID        = "bas-00000000000000ka1"
	JTILive        = "jti-ka1-live"
	JTIRevoked     = "jti-ka1-revoked"

	UndeclaredIssuer = "https://undeclared.example.test"

	// ListRoute — маршрут пути запроса без пола уверенности (П5).
	ListRoute = "/iam/v1/accounts"
	// FloorRoute — глагол с полом «2» встроенного каталога (П5).
	FloorRoute = "/iam/v1/users/" + User + "/tokens"
	// Нативные глаголы харнесса: без пола и с полом (П5).
	PingMethod  = "/kaname.cloud.iam.v1.ProbeService/Ping"
	FloorMethod = "/kaname.cloud.iam.v1.UserTokenService/Issue"

	// ProbeDeadline — «Срок пробы»: проба, ждущая ответа края, держит свой
	// срок 3s; его истечение — исход строки (край висит), а не поломка пробы.
	ProbeDeadline = 3 * time.Second
)

// Ответы, которые утверждает приёмка, — дословно (Р1, Р2). Проба держит их
// литералом, а не берёт у производителя: утверждается текст приёмки, и общий
// с продуктом источник сделал бы пробу согласной с любой его правкой.
const (
	UnavailableBody  = `{"code":14,"message":"credential state could not be established"}`
	UnavailableText  = "credential state could not be established"
	RefusalBody      = `{"code":16,"message":"authentication failed","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"AUTHN_REQUIRED","domain":"kaname.cloud.iam.v1"}]}`
	RefusalChallenge = `Bearer realm="kacho", error="invalid_token"`
	RefusalText      = "authentication failed"
)

// State — состояние вопроса службе (П1, П3).
type State int32

const (
	Answers State = iota
	Silent
	Unavailable
	Unimplemented
)

func (s State) String() string {
	return [...]string{"отвечает", "молчит", "UNAVAILABLE", "UNIMPLEMENTED"}[s]
}

// Question — один вопрос края соседу: состояние, управляемая задержка ответа,
// счёт принятых и освобождение молчащего ответа пробой.
type Question struct {
	state   atomic.Int32
	delay   atomic.Int64
	asked   atomic.Int64
	release chan struct{}
}

func newQuestion() *Question { return &Question{release: make(chan struct{})} }

// Set задаёт состояние вопроса.
func (q *Question) Set(s State) { q.state.Store(int32(s)) }

// Delay задаёт управляемую задержку ответа в состоянии «отвечает».
func (q *Question) Delay(d time.Duration) { q.delay.Store(int64(d)) }

// Asked — сколько раз вопрос был задан.
func (q *Question) Asked() int64 { return q.asked.Load() }

// gate — что сосед делает с вопросом ДО ответа. nil — отвечать.
//
// «Молчит» — соединение принято, ответа нет до освобождения пробой (или пока
// спрашивающий сам не оборвёт вызов). Освобождение ответа не даёт: молчавший
// сосед ответить уже не успел.
func (q *Question) gate(ctx context.Context) error {
	q.asked.Add(1)
	switch State(q.state.Load()) {
	case Silent:
		select {
		case <-q.release:
		case <-ctx.Done():
		}
		return status.Error(codes.Unavailable, "doubler: released without an answer")
	case Unavailable:
		return status.Error(codes.Unavailable, "doubler: unavailable")
	case Unimplemented:
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

// Authority — авторитет отзыва формы RFC 7662: «жив» / «отозван» по `jti`,
// «молчит», UNAVAILABLE.
type Authority struct {
	url string
	Q   *Question
}

func newAuthority(t *testing.T) *Authority {
	t.Helper()
	a := &Authority{Q: newQuestion()}
	srv := privateloopback.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := a.Q.gate(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"unavailable"}`))
			return
		}
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"active": JTIOf(r.Form.Get("token")) != JTIRevoked})
	}))
	t.Cleanup(srv.Close)
	// Освобождение — раньше закрытия сервера (очистки идут в обратном порядке):
	// закрытие ждёт ответа на каждый принятый запрос.
	t.Cleanup(func() { close(a.Q.release) })
	a.url = srv.URL + "/internal/tokens/introspect"
	return a
}

// ─── П3: служба доступа на внутреннем слушателе ──────────────────────────────

// Identity — дублёр службы доступа: сессия, отсечка, запись отзыва, годность
// базового удостоверения и отзыв при выходе, у каждого вопроса своё состояние.
type Identity struct {
	iamv1.UnimplementedInternalHumanSessionServiceServer
	iamv1.UnimplementedInternalSessionRevocationsServiceServer
	iamv1.UnimplementedInternalIAMServiceServer

	SessionQ, CutoffQ, RevokedQ, BasicQ, RevokeQ *Question

	T0 time.Time
	// CutoffAtT0 — отсечка субъекта равна T0 (иначе отсечки нет).
	CutoffAtT0 atomic.Bool
	// NoAuthInstant — ответ о сессии без момента аутентификации.
	NoAuthInstant atomic.Bool
	// basicSecrets — предъявленная строка каждого известного удостоверения.
	basicSecrets map[string]string

	mu      sync.Mutex
	revoked []*iamv1.RevokeRequest

	conn *grpc.ClientConn
}

func newIdentity(t *testing.T) *Identity {
	t.Helper()
	id := &Identity{
		SessionQ: newQuestion(), CutoffQ: newQuestion(), RevokedQ: newQuestion(),
		BasicQ: newQuestion(), RevokeQ: newQuestion(),
		T0:           time.Now().Add(-time.Hour).Truncate(time.Microsecond),
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
		for _, q := range []*Question{id.SessionQ, id.CutoffQ, id.RevokedQ, id.BasicQ, id.RevokeQ} {
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
func (id *Identity) mintBasic(t *testing.T, credentialID string, known bool) string {
	t.Helper()
	s, _, err := credsecret.Mint(credentialID)
	require.NoError(t, err)
	if known {
		id.basicSecrets[credentialID] = s
	}
	return s
}

func (id *Identity) Resolve(ctx context.Context, in *iamv1.ResolveHumanSessionRequest) (*iamv1.ResolveHumanSessionResponse, error) {
	if err := id.SessionQ.gate(ctx); err != nil {
		return nil, err
	}
	if in.GetBearer() != SessionLive {
		return &iamv1.ResolveHumanSessionResponse{Found: false}, nil
	}
	s := &iamv1.HumanSession{
		UserId: User, Email: "ka1@example.test", DisplayName: "KA1",
		ExpiresAt:      timestamppb.New(time.Now().Add(time.Hour)),
		AssuranceLevel: "1", EmailVerified: true,
	}
	if !id.NoAuthInstant.Load() {
		s.AuthenticatedAt = timestamppb.New(id.T0)
	}
	return &iamv1.ResolveHumanSessionResponse{Found: true, Session: s}, nil
}

func (id *Identity) SessionCutoffOf(ctx context.Context, _ *iamv1.SessionCutoffOfRequest) (*iamv1.SessionCutoffOfResponse, error) {
	if err := id.CutoffQ.gate(ctx); err != nil {
		return nil, err
	}
	if !id.CutoffAtT0.Load() {
		return &iamv1.SessionCutoffOfResponse{Found: false}, nil
	}
	return &iamv1.SessionCutoffOfResponse{Found: true, RevokeBefore: timestamppb.New(id.T0)}, nil
}

func (id *Identity) IsRevoked(ctx context.Context, in *iamv1.IsRevokedRequest) (*iamv1.IsRevokedResponse, error) {
	if err := id.RevokedQ.gate(ctx); err != nil {
		return nil, err
	}
	return &iamv1.IsRevokedResponse{Revoked: in.GetTokenJti() == JTIRevoked}, nil
}

func (id *Identity) Revoke(ctx context.Context, in *iamv1.RevokeRequest) (*operation.Operation, error) {
	if err := id.RevokeQ.gate(ctx); err != nil {
		return nil, err
	}
	id.mu.Lock()
	id.revoked = append(id.revoked, in)
	id.mu.Unlock()
	return &operation.Operation{Id: "op-ka1", Done: true}, nil
}

func (id *Identity) ResolveBasicCredential(ctx context.Context, in *iamv1.ResolveBasicCredentialRequest) (*iamv1.ResolveBasicCredentialResponse, error) {
	if err := id.BasicQ.gate(ctx); err != nil {
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
		PrincipalType: "user", PrincipalId: User, DisplayName: "KA1",
		CredentialId: p.CredentialID, ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)),
	}, nil
}

// ─── П8: сертификаты нативной поверхности ────────────────────────────────────

type Cert struct {
	TLS   tls.Certificate
	Thumb string // x5t#S256 — base64url(sha256(DER))
}

func NewCert(t *testing.T, cn string, server bool) Cert {
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
	return Cert{
		TLS:   tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		Thumb: base64.RawURLEncoding.EncodeToString(sum[:]),
	}
}

// ─── харнесс ─────────────────────────────────────────────────────────────────

type Options struct {
	// RequireBinding — объявлена обязательная привязка машинных токенов (строка
	// (ж), образец newF1bStandWithRequirement).
	RequireBinding bool
	// DPoP — включён KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP: смонтированы
	// DPoPMiddleware и NewCnfBindingInterceptor, как в main.go.
	DPoP bool
}

type Stand struct {
	restURL  string
	grpcAddr string
	srvCA    *x509.CertPool

	Ours, Legacy, Undeclared *Signer
	OurAuth                  *Authority
	Ident                    *Identity

	CertA, CertB Cert

	BasicGood, BasicUnknownID, BasicWrongSecret string
}

// lookup — резолв субъекта по `sub` для токена без утверждений принципала
// (строка (е) и её близнец): полоса предъявителя спрашивает его на запасной
// ветви.
type lookup struct{}

func (lookup) LookupByExternalID(_ context.Context, ext string) (middleware.Subject, error) {
	return middleware.Subject{Type: "user", ID: User, DisplayName: ext}, nil
}

func New(t *testing.T, opt Options) *Stand {
	t.Helper()
	st := &Stand{
		Ours:       NewSigner(t, PlatformIssuer, "ka1-ours"),
		Legacy:     NewSigner(t, LegacyIssuer, "ka1-legacy"),
		Undeclared: NewSigner(t, UndeclaredIssuer, "ka1-undeclared"),
		OurAuth:    newAuthority(t),
		Ident:      newIdentity(t),
		CertA:      NewCert(t, "ka1-a", false),
		CertB:      NewCert(t, "ka1-b", false),
	}
	st.BasicGood = st.Ident.mintBasic(t, BasicID, true)
	st.BasicUnknownID = st.Ident.mintBasic(t, "bas-0000000000unknown", false)
	// Неверный секрет к ИЗВЕСТНОМУ идентификатору: вторая чеканка того же
	// идентификатора службе не сообщается.
	st.BasicWrongSecret = st.Ident.mintBasic(t, BasicID, false)
	require.NotEqual(t, st.BasicGood, st.BasicWrongSecret)

	verifier, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers: []middleware.IssuerKeySet{
			{Issuer: LegacyIssuer, KeySetURL: st.Legacy.url,
				TokenTypes:              []string{middleware.LegacyTokenType, middleware.PlatformTokenType},
				TolerateAbsentTokenType: true},
			{Issuer: PlatformIssuer, KeySetURL: st.Ours.url,
				TokenTypes: []string{middleware.PlatformTokenType}, ReadRevocation: true},
		},
		ExpectedAudience: Audience,
	})
	require.NoError(t, err)

	// Читатель отзыва нашей чеканки — тот же, что собирает процесс; его срок —
	// умолчание загрузчика KACHO_INTROSPECTION_TIMEOUT_MS (1000).
	platformIntrospection, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		IntrospectionURL: st.OurAuth.url, TTL: time.Millisecond, Timeout: time.Second,
	})
	require.NoError(t, err)

	adapter := clients.NewSessionRevocationsAdapter(st.Ident.conn)
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	auth := middleware.NewAuthInterceptor(middleware.AuthModeProduction, "", lookup{}, logger).
		WithVerifier(verifier).
		WithRevocationCheck(middleware.NewOwnRevocationSource(adapter), time.Hour).
		WithPlatformRevocationCheck(platformIntrospection, time.Hour).
		WithHumanSession(adapter).
		WithSessionCutoffCheck(adapter, time.Hour).
		WithBasicCredentialLane(middleware.NewBasicCredentialLane(
			middleware.NewBasicAuthorityFromStub(iamv1.NewInternalIAMServiceClient(st.Ident.conn)))).
		WithRequireMachineTokenBinding(opt.RequireBinding).
		WithStepUp(middleware.NewStepUpGate(nil), middleware.NewCatalogPermissionLookup(catalog), middleware.NewRestRouter())

	var probe http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	unary := []grpc.UnaryServerInterceptor{auth.Unary()}
	if opt.DPoP {
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
			Logger:           logger, APIDomain: APIDomain,
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

	srvCert := NewCert(t, "ka1-edge", true)
	st.srvCA = x509.NewCertPool()
	leaf, err := x509.ParseCertificate(srvCert.TLS.Certificate[0])
	require.NoError(t, err)
	st.srvCA.AddCert(leaf)
	lis := privateloopback.Listen(t)
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{srvCert.TLS},
			// Клиентский сертификат запрашивается, но не обязателен: сверку
			// привязки с ним делает перехватчик привязки (П8), а не слушатель.
			ClientAuth: tls.RequestClientCert,
			MinVersion: tls.VersionTLS12,
		})),
		grpc.ChainUnaryInterceptor(unary...),
	)
	for _, m := range []string{PingMethod, FloorMethod} {
		svc, name, _ := strings.Cut(strings.TrimPrefix(m, "/"), "/")
		full := m
		srv.RegisterService(&grpc.ServiceDesc{
			ServiceName: svc, HandlerType: (*any)(nil), Metadata: "ka1",
			Methods: []grpc.MethodDesc{{MethodName: name,
				Handler: func(_ any, ctx context.Context, dec func(any) error, ic grpc.UnaryServerInterceptor) (any, error) {
					if err := dec(&EmptyMsg{}); err != nil {
						return nil, err
					}
					h := func(context.Context, any) (any, error) { return &EmptyMsg{}, nil }
					return ic(ctx, &EmptyMsg{}, &grpc.UnaryServerInfo{FullMethod: full}, h)
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
func (s *Stand) OurToken(t *testing.T, jti string, mutate func(jwt.MapClaims)) string {
	t.Helper()
	return s.Ours.Mint(t, middleware.PlatformTokenType, jti, func(c jwt.MapClaims) {
		c["sub"] = User
		c["kaname_principal_id"] = User
		if mutate != nil {
			mutate(c)
		}
	})
}

func (s *Stand) LegacyToken(t *testing.T, jti string) string {
	t.Helper()
	return s.Legacy.Mint(t, middleware.LegacyTokenType, jti, func(c jwt.MapClaims) {
		c["sub"] = User
		c["kaname_principal_id"] = User
	})
}

// tamper меняет один байт подписи.
func Tamper(tok string) string {
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

// Shot — ответ REST-поверхности целиком. Заголовки — все, кроме двух,
// различных у любых двух ответов по построению (`Date`, `X-Request-Id`; Р2).
type Shot struct {
	Status   int
	Header   http.Header
	Body     []byte
	Elapsed  time.Duration
	TimedOut bool
}

func (s Shot) String() string {
	if s.TimedOut {
		return "ответа нет за " + ProbeDeadline.String() + " (край висит)"
	}
	keys := make([]string, 0, len(s.Header))
	for k := range s.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(http.StatusText(s.Status))
	for _, k := range keys {
		b.WriteString(" | " + k + ": " + strings.Join(s.Header[k], " ; "))
	}
	b.WriteString(" | тело: " + string(bytes.TrimSpace(s.Body)))
	return b.String()
}

// same — побайтовое равенство по правилу сравнения Р2.
func (s Shot) Same(o Shot) bool {
	if s.TimedOut || o.TimedOut || s.Status != o.Status || !bytes.Equal(s.Body, o.Body) {
		return false
	}
	if len(s.Header) != len(o.Header) {
		return false
	}
	for k, v := range s.Header {
		w, ok := o.Header[k]
		if !ok || strings.Join(v, "\x00") != strings.Join(w, "\x00") {
			return false
		}
	}
	return true
}

type Presented func(*http.Request)

func Bearer(tok string) Presented {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

func SessionCarrier(v string) Presented {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: v})
	}
}

func Nothing(*http.Request) {}

func (s *Stand) REST(t *testing.T, method, path string, present Presented) Shot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), ProbeDeadline)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, s.restURL+path, nil)
	require.NoError(t, err)
	present(req)
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Shot{TimedOut: true, Elapsed: time.Since(start)}
		}
		require.NoError(t, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if errors.Is(err, context.DeadlineExceeded) {
		return Shot{TimedOut: true, Elapsed: time.Since(start)}
	}
	require.NoError(t, err)
	h := resp.Header.Clone()
	h.Del("Date")
	h.Del("X-Request-Id")
	return Shot{Status: resp.StatusCode, Header: h, Body: body, Elapsed: time.Since(start)}
}

// Native — исход нативного вызова и его срок.
type Native struct {
	St      *status.Status
	Elapsed time.Duration
}

func (n Native) String() string {
	b, _ := json.Marshal(n.St.Proto())
	return n.St.Code().String() + " " + string(b)
}

// grpc зовёт нативный метод харнесса по TLS; cert — клиентский сертификат
// соединения (nil — без него).
func (s *Stand) GRPC(t *testing.T, method, tok string, cert *Cert) Native {
	t.Helper()
	cfg := &tls.Config{RootCAs: s.srvCA, ServerName: "ka1.example.test", MinVersion: tls.VersionTLS12}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{cert.TLS}
	}
	conn, err := grpc.NewClient(s.grpcAddr, grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), ProbeDeadline)
	defer cancel()
	if tok != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+tok)
	}
	start := time.Now()
	err = conn.Invoke(ctx, method, &EmptyMsg{}, &EmptyMsg{})
	return Native{St: status.Convert(err), Elapsed: time.Since(start)}
}

// RequireUnavailable — ответ Р1 на REST-поверхности.
func RequireUnavailable(t *testing.T, where string, got Shot) {
	t.Helper()
	if got.TimedOut || got.Status != http.StatusServiceUnavailable ||
		got.Header.Get("Content-Type") != "application/json" ||
		string(bytes.TrimSpace(got.Body)) != UnavailableBody ||
		got.Header.Get("WWW-Authenticate") != "" || len(got.Header.Values("Set-Cookie")) != 0 {
		t.Errorf("%s: ответ не Р1 (503, JSON %s, без WWW-Authenticate и Set-Cookie)\n  получено: %s",
			where, UnavailableBody, got)
	}
}

// RequireNativeUnavailable — ответ Р1 на нативной поверхности: тот же текст, без деталей.
func RequireNativeUnavailable(t *testing.T, where string, got Native) {
	t.Helper()
	if got.St.Code() != codes.Unavailable || got.St.Message() != UnavailableText || len(got.St.Details()) != 0 {
		t.Errorf("%s: нативный ответ не Р1 (UNAVAILABLE %q, без деталей)\n  получено: %s",
			where, UnavailableText, got)
	}
}

// RequireRefusal — отказ Р2 на REST-поверхности.
func RequireRefusal(t *testing.T, where string, got Shot, carrierEndings bool) {
	t.Helper()
	ok := !got.TimedOut && got.Status == http.StatusUnauthorized &&
		got.Header.Get("Content-Type") == "application/json" &&
		got.Header.Get("WWW-Authenticate") == RefusalChallenge &&
		string(bytes.TrimSpace(got.Body)) == RefusalBody
	if carrierEndings {
		ok = ok && sameCookies(got.Header.Values("Set-Cookie"), CarrierEndingLines())
	} else {
		ok = ok && len(got.Header.Values("Set-Cookie")) == 0
	}
	if !ok {
		t.Errorf("%s: отказ не Р2 (401, WWW-Authenticate %s, тело %s, гашение носителя=%v)\n  получено: %s",
			where, RefusalChallenge, RefusalBody, carrierEndings, got)
	}
}

// RequireNativeRefusal — отказ Р2 на нативной поверхности.
func RequireNativeRefusal(t *testing.T, where string, got Native) {
	t.Helper()
	ok := got.St.Code() == codes.Unauthenticated && got.St.Message() == RefusalText
	ds := got.St.Proto().GetDetails()
	if ok && len(ds) == 1 {
		info := ErrorInfoOf(got.St)
		ok = info != nil && info.GetReason() == "AUTHN_REQUIRED" &&
			info.GetDomain() == "kaname.cloud.iam.v1" && len(info.GetMetadata()) == 0
	} else {
		ok = false
	}
	if !ok {
		t.Errorf("%s: нативный отказ не Р2 (UNAUTHENTICATED %q, одна ErrorInfo AUTHN_REQUIRED/kaname.cloud.iam.v1 без metadata)\n  получено: %s",
			where, RefusalText, got)
	}
}

func MarshalStatus(t *testing.T, st *status.Status) []byte {
	t.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(st.Proto())
	require.NoError(t, err)
	return b
}

// CarrierEndingLines — набор Set-Cookie, который производит SessionCarrierEndings().
func CarrierEndingLines() []string {
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

// ─── общие помощники пробы ───────────────────────────────────────────────────

// Издатели, адресат и домен края харнесса (П1, П2).
const (
	PlatformIssuer = "https://kaname.kacho.local"
	LegacyIssuer   = "https://legacy.api.kacho.cloud"
	Audience       = "https://api.kacho.cloud"
	APIDomain      = "api.kacho.cloud"
)

// Signer — источник набора проверочных ключей ОДНОГО издателя плюс его
// приватная половина. Наборы у издателей разные: общий набор означал бы, что
// ключ одного проверяет токен другого.
type Signer struct {
	issuer string
	kid    string
	priv   *ecdsa.PrivateKey
	url    string
}

// NewSigner поднимает набор ключей издателя на петле.
func NewSigner(t *testing.T, issuer, kid string) *Signer {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	s := &Signer{issuer: issuer, kid: kid, priv: priv}
	srv := privateloopback.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "EC", "kid": kid, "alg": "ES256", "use": "sig", "crv": "P-256",
			"x": base64.RawURLEncoding.EncodeToString(priv.X.FillBytes(make([]byte, 32))),
			"y": base64.RawURLEncoding.EncodeToString(priv.Y.FillBytes(make([]byte, 32))),
		}}})
	}))
	t.Cleanup(srv.Close)
	s.url = srv.URL + "/.well-known/jwks.json"
	return s
}

// KeySetURL — адрес набора ключей издателя.
func (s *Signer) KeySetURL() string { return s.url }

// Mint чеканит токен этого издателя; тип и идентификатор задаёт вызывающий.
func (s *Signer) Mint(t *testing.T, typ, jti string, mutate func(jwt.MapClaims)) string {
	t.Helper()
	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"iss": s.issuer, "aud": []any{Audience}, "sub": "usr_alice_acc_a1b2",
		"iat": now, "nbf": now, "exp": now + 900, "acr": "2",
		"kaname_principal_type": "user", "kaname_principal_id": "usr_alice_acc_a1b2",
	}
	if jti != "" {
		claims["jti"] = jti
	}
	if mutate != nil {
		mutate(claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = s.kid
	if typ != "" {
		tok.Header["typ"] = typ
	} else {
		delete(tok.Header, "typ")
	}
	signed, err := tok.SignedString(s.priv)
	require.NoError(t, err)
	return signed
}

// JTIOf — идентификатор из тела JWT без проверки подписи.
func JTIOf(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(payload, &m) != nil {
		return ""
	}
	v, _ := m["jti"].(string)
	return v
}

// EmptyMsg — пустое сообщение proto-подобной формы для пробных методов.
type EmptyMsg struct{}

func (m *EmptyMsg) Reset()         {}
func (m *EmptyMsg) String() string { return "" }
func (m *EmptyMsg) ProtoMessage()  {}

// ErrorInfoOf — первая деталь ErrorInfo статуса.
func ErrorInfoOf(st *status.Status) *errdetails.ErrorInfo {
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	return nil
}

// RevokeRequests — принятые дублёром запросы Revoke.
func (id *Identity) RevokeRequests() []*iamv1.RevokeRequest {
	id.mu.Lock()
	defer id.mu.Unlock()
	return append([]*iamv1.RevokeRequest(nil), id.revoked...)
}
