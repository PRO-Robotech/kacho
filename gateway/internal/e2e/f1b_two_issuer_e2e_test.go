// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_two_issuer_e2e_test.go — Ф1б-09, Ф1б-11, Ф1б-12: край принимает НАШЕГО
// издателя наравне с прежним, и делает это на ОБЕИХ своих поверхностях.
//
// # Почему через настоящее соединение, а не вызовом функции проверки
//
// Это живой периметр безопасности. Ограничения живут в библиотеках — в разборе
// заголовка, в транспорте, в форме ответа, — и проба над собранным в памяти
// входом о них не узнает. Поэтому REST здесь идёт через настоящий http-сервер и
// настоящего клиента, а нативная gRPC — через настоящий слушатель TCP.
//
// # Что здесь утверждается сверх «токен принят»
//
// Полоса отзыва выбирается ПО ИЗДАТЕЛЮ, и на двух полосах «авторитет не
// ответил» значит разное: на нашей — отказ, на полосе прежнего издателя
// сохраняется задокументированный мягкий проход. Обе половины предъявлены, и
// рядом с каждым отрицанием стоит положительный контроль: без него читатель
// отзыва, ВСЕГДА отвечающий отказом, прошёл бы пробу целиком.
package e2e_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

const (
	f1bPlatformIssuer = "https://kaname.kacho.local"
	f1bLegacyIssuer   = "https://hydra.api.kacho.cloud"
)

// f1bSigner — источник набора проверочных ключей ОДНОГО издателя плюс его
// приватная половина. Наборы у издателей РАЗНЫЕ: общий набор означал бы, что
// ключ одного проверяет токен другого.
type f1bSigner struct {
	issuer string
	kid    string
	priv   *ecdsa.PrivateKey
	url    string
	hits   *atomic.Int64
}

func newF1bSigner(t *testing.T, issuer, kid string) *f1bSigner {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	s := &f1bSigner{issuer: issuer, kid: kid, priv: priv, hits: &atomic.Int64{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
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

// mint чеканит токен этого издателя. Тип и идентификатор задаёт вызывающий:
// проба обязана уметь выпустить в том числе токен без идентификатора отзыва.
func (s *f1bSigner) mint(t *testing.T, typ, jti string, mutate func(jwt.MapClaims)) string {
	t.Helper()
	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"iss": s.issuer, "aud": []any{testAudience}, "sub": "usr_alice_acc_a1b2",
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

// f1bAuthority — авторитет отзыва по форме RFC 7662, чьим поведением управляет
// проба: отвечает «действует», «отозван» либо не отвечает вовсе.
type f1bAuthority struct {
	url      string
	asked    *atomic.Int64
	revoked  *atomic.Bool
	down     *atomic.Bool
	notFound *atomic.Bool
}

func newF1bAuthority(t *testing.T) *f1bAuthority {
	t.Helper()
	a := &f1bAuthority{
		asked: &atomic.Int64{}, revoked: &atomic.Bool{},
		down: &atomic.Bool{}, notFound: &atomic.Bool{},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		a.asked.Add(1)
		switch {
		case a.notFound.Load():
			// По адресу отвечает не авторитет отзыва — постоянная неисправность.
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
		case a.down.Load():
			// Авторитет не ответил: временный отказ, повтор осмыслен.
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"unavailable"}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"active": !a.revoked.Load()})
		}
	}))
	t.Cleanup(srv.Close)
	a.url = srv.URL + "/internal/tokens/introspect"
	return a
}

// f1bStand — край, поднятый с ДВУМЯ объявленными записями приёма, обеими
// поверхностями и двумя авторитетами отзыва.
type f1bStand struct {
	restURL  string
	grpcAddr string
	ours     *f1bSigner
	legacy   *f1bSigner
	ourAuth  *f1bAuthority
	oldAuth  *f1bAuthority
	restHits *atomic.Int64
	grpcHits *atomic.Int64
	// restPrincipal / grpcPrincipal — принципал, дошедший ДО обработчика.
	// Захватывается, чтобы Ф1б-11 могла утверждать своё «принципал определён так
	// же, как для токена прежнего издателя», а не только «запрос прошёл».
	restPrincipal *atomic.Value
	grpcPrincipal *atomic.Value
	closeGRPC     func()
}

func newF1bStand(t *testing.T, acceptPlatform bool) *f1bStand {
	t.Helper()
	return newF1bStandWith(t, acceptPlatform, false)
}

// newF1bStandWithRequirement — тот же край с ВКЛЮЧЁННЫМ требованием привязки к
// машинному принципалу.
func newF1bStandWithRequirement(t *testing.T) *f1bStand {
	t.Helper()
	return newF1bStandWith(t, true, true)
}

func newF1bStandWith(t *testing.T, acceptPlatform, requireBinding bool) *f1bStand {
	t.Helper()
	st := &f1bStand{
		ours:          newF1bSigner(t, f1bPlatformIssuer, "ours-es256"),
		legacy:        newF1bSigner(t, f1bLegacyIssuer, "legacy-es256"),
		ourAuth:       newF1bAuthority(t),
		oldAuth:       newF1bAuthority(t),
		restHits:      &atomic.Int64{},
		grpcHits:      &atomic.Int64{},
		restPrincipal: &atomic.Value{},
		grpcPrincipal: &atomic.Value{},
	}

	records := []middleware.IssuerKeySet{{
		Issuer: f1bLegacyIssuer, KeySetURL: st.legacy.url,
		TokenTypes:              []string{middleware.LegacyTokenType, middleware.PlatformTokenType},
		TolerateAbsentTokenType: true,
	}}
	if acceptPlatform {
		records = append(records, middleware.IssuerKeySet{
			Issuer: f1bPlatformIssuer, KeySetURL: st.ours.url,
			TokenTypes:     []string{middleware.PlatformTokenType},
			ReadRevocation: true,
		})
	}
	verifier, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers:          records,
		ExpectedAudience: testAudience,
	})
	require.NoError(t, err)

	legacyIntrospection, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: st.oldAuth.url,
		TTL:                   time.Millisecond, Timeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	platformIntrospection, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: st.ourAuth.url,
		TTL:                   time.Millisecond, Timeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)

	auth := middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "", nil,
		slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
	).
		WithVerifier(verifier).
		WithRevocationCheck(legacyIntrospection, time.Hour).
		WithPlatformRevocationCheck(platformIntrospection, time.Hour).
		WithRequireMachineTokenBinding(requireBinding)

	// REST — НАСТОЯЩИЙ сервер и настоящее соединение.
	rest := httptest.NewServer(auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st.restHits.Add(1)
		st.restPrincipal.Store(r.Header.Get(principalmeta.HeaderPrincipalType) + ":" +
			r.Header.Get(principalmeta.HeaderPrincipalID))
		w.WriteHeader(http.StatusOK)
	})))
	t.Cleanup(rest.Close)
	st.restURL = rest.URL

	// Нативная gRPC — НАСТОЯЩИЙ слушатель TCP.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(auth.Unary()),
		grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
			return stream.SendMsg(&struct{}{})
		}),
	)
	// Пробный метод регистрируется вручную: предмет пробы — слой authN, а не
	// тело обработчика, поэтому службы из контракта здесь не нужно.
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "kacho.cloud.iam.v1.ProbeService",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Ping",
			Handler: func(_ any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				if err := dec(&emptyMsg{}); err != nil {
					return nil, err
				}
				h := func(ctx context.Context, _ any) (any, error) {
					st.grpcHits.Add(1)
					var pt, pid string
					if md, ok := metadata.FromIncomingContext(ctx); ok {
						if v := md.Get(principalmeta.HeaderPrincipalType); len(v) > 0 {
							pt = v[0]
						}
						if v := md.Get(principalmeta.HeaderPrincipalID); len(v) > 0 {
							pid = v[0]
						}
					}
					st.grpcPrincipal.Store(pt + ":" + pid)
					return &emptyMsg{}, nil
				}
				if interceptor == nil {
					return h(ctx, nil)
				}
				return interceptor(ctx, &emptyMsg{}, &grpc.UnaryServerInfo{
					FullMethod: "/kacho.cloud.iam.v1.ProbeService/Ping",
				}, h)
			},
		}},
		Metadata: "probe",
	}, struct{}{})
	go func() { _ = srv.Serve(lis) }()
	st.grpcAddr = lis.Addr().String()
	st.closeGRPC = func() { srv.Stop(); _ = lis.Close() }
	t.Cleanup(st.closeGRPC)
	return st
}

// emptyMsg — пустое сообщение proto-подобной формы для пробного метода.
type emptyMsg struct{}

func (m *emptyMsg) Reset()         {}
func (m *emptyMsg) String() string { return "" }
func (m *emptyMsg) ProtoMessage()  {}

// callREST предъявляет токен REST-поверхности через настоящее соединение.
func (s *f1bStand) callREST(t *testing.T, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.restURL+"/iam/v1/whoami", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// callGRPC предъявляет токен нативной gRPC-поверхности через настоящий сокет.
func (s *f1bStand) callGRPC(t *testing.T, token string) codes.Code {
	t.Helper()
	conn, err := grpc.NewClient(s.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	err = conn.Invoke(ctx, "/kacho.cloud.iam.v1.ProbeService/Ping", &emptyMsg{}, &emptyMsg{})
	return status.Code(err)
}

// ─── Ф1б-11 и Ф1б-12: обе поверхности принимают нашего издателя ─────────────

func TestF1b11_12_BothEdgeSurfacesAcceptOurIssuerAndTheLegacyOne(t *testing.T) {
	st := newF1bStand(t, true)

	ourToken := st.ours.mint(t, middleware.PlatformTokenType, "jti-ours-1", nil)
	legacyToken := st.legacy.mint(t, middleware.LegacyTokenType, "jti-legacy-1", nil)

	if got := st.callREST(t, ourToken); got != http.StatusOK {
		t.Fatalf("REST-поверхность отвергла токен НАШЕГО издателя: %d", got)
	}
	if got := st.callREST(t, legacyToken); got != http.StatusOK {
		t.Fatalf("REST-поверхность отвергла токен ПРЕЖНЕГО издателя: %d — положительный "+
			"контроль обратимости не выполняется", got)
	}
	if got := st.callGRPC(t, ourToken); got != codes.OK {
		t.Fatalf("gRPC-поверхность отвергла токен НАШЕГО издателя: %v", got)
	}
	if got := st.callGRPC(t, legacyToken); got != codes.OK {
		t.Fatalf("gRPC-поверхность отвергла токен ПРЕЖНЕГО издателя: %v", got)
	}

	// Токен издателя, для которого записи нет, отвергается на ОБЕИХ.
	stranger := newF1bSigner(t, "https://issuer.example.invalid", "x-1")
	strangerToken := stranger.mint(t, middleware.PlatformTokenType, "jti-x-1", nil)
	if got := st.callREST(t, strangerToken); got == http.StatusOK {
		t.Fatalf("REST-поверхность приняла токен издателя без объявленной записи источника")
	}
	if got := st.callGRPC(t, strangerToken); got == codes.OK {
		t.Fatalf("gRPC-поверхность приняла токен издателя без объявленной записи источника")
	}
	if stranger.hits.Load() != 0 {
		t.Fatalf("к источнику постороннего издателя обращались (%d) — адрес выведен из самого "+
			"издателя либо записи перебирались подряд", stranger.hits.Load())
	}
}

// TestF1b11_ReversibleWhenOurIssuerIsNotDeclared — откат состоит в СНЯТИИ
// объявления, а не в перекатке: край поднимается и работает без нашей чеканки.
func TestF1b11_ReversibleWhenOurIssuerIsNotDeclared(t *testing.T) {
	st := newF1bStand(t, false)

	legacyToken := st.legacy.mint(t, middleware.LegacyTokenType, "jti-legacy-2", nil)
	if got := st.callREST(t, legacyToken); got != http.StatusOK {
		t.Fatalf("посадка БЕЗ нашей чеканки отвергает токен прежнего издателя: %d", got)
	}
	if got := st.callGRPC(t, legacyToken); got != codes.OK {
		t.Fatalf("посадка БЕЗ нашей чеканки отвергает токен прежнего издателя на gRPC: %v", got)
	}

	ourToken := st.ours.mint(t, middleware.PlatformTokenType, "jti-ours-2", nil)
	if got := st.callREST(t, ourToken); got == http.StatusOK {
		t.Fatalf("наш издатель не объявлен, а его токен принят — выключатель перехода не работает")
	}
}

// ─── Ф1б-09: отзыв НАШЕГО токена читается У НАС ────────────────────────────

func TestF1b09_OurTokenRevocationIsAskedOfOurAuthorityAndFailsClosed(t *testing.T) {
	st := newF1bStand(t, true)

	// (1) Не отозван, авторитет доступен ⇒ ПРИНИМАЕТСЯ. Несущая половина: без
	// неё читатель, всегда отвечающий отказом, прошёл бы пробу целиком.
	live := st.ours.mint(t, middleware.PlatformTokenType, "jti-live", nil)
	require.Equal(t, http.StatusOK, st.callREST(t, live))
	require.Positive(t, st.ourAuth.asked.Load(), "НАШ авторитет отзыва не спрашивали вовсе")
	require.Zero(t, st.oldAuth.asked.Load(),
		"вопрос об отзыве НАШЕГО токена ушёл авторитету ПРЕЖНЕГО издателя — он о наших "+
			"токенах не знает by construction, и его ответ есть утверждение о чужом предмете")

	// (2) Отозван ⇒ ОТВЕРГАЕТСЯ, на обеих поверхностях.
	st.ourAuth.revoked.Store(true)
	revoked := st.ours.mint(t, middleware.PlatformTokenType, "jti-revoked", nil)
	assert.Equal(t, http.StatusUnauthorized, st.callREST(t, revoked))
	assert.Equal(t, codes.Unauthenticated, st.callGRPC(t, revoked))
	st.ourAuth.revoked.Store(false)

	// (3) Авторитет НЕ ОТВЕТИЛ ⇒ ОТКАЗ, а не проход. Мягкий проход означал бы:
	// чеканим, отзываем и свой же отзыв не исполняем.
	st.ourAuth.down.Store(true)
	before := st.restHits.Load()
	unanswered := st.ours.mint(t, middleware.PlatformTokenType, "jti-unanswered", nil)
	got := st.callREST(t, unanswered)
	if got == http.StatusOK {
		t.Fatalf("НАШ авторитет отзыва недоступен, а токен принят — «не дозвонился» " +
			"не означает «разрешено»")
	}
	if st.restHits.Load() != before {
		t.Fatalf("запрос дошёл до обработчика при недоступном авторитете отзыва")
	}
	if code := st.callGRPC(t, unanswered); code == codes.OK {
		t.Fatalf("та же посадка приняла токен на gRPC-поверхности: %v", code)
	}
	st.ourAuth.down.Store(false)

	// (4) Положительный контроль ВОССТАНОВЛЕНИЯ: недоступность авторитета —
	// состояние временное, и отказ обязан кончаться вместе с ней. Без этой
	// половины «отказ при недоступности» неотличимо от «отказ навсегда после
	// первого сбоя».
	//
	// Порядок здесь значим: восстановление проверяется ДО постоянной
	// неисправности (6), потому что та по построению не лечится и остаётся
	// липкой — см. её ветку.
	stillOK := st.ours.mint(t, middleware.PlatformTokenType, "jti-still-ok", nil)
	require.Equal(t, http.StatusOK, st.callREST(t, stillOK),
		"после восстановления авторитета отзыв снова отвечает, а токен снова принимается")

	// (5) НАШ токен без идентификатора отзыва ⇒ ОТКАЗ, а не «не спрашивали».
	// Производитель идентификатора на нашей полосе — мы сами.
	noJTI := st.ours.mint(t, middleware.PlatformTokenType, "", nil)
	if code := st.callREST(t, noJTI); code == http.StatusOK {
		t.Fatalf("наш токен без идентификатора отзыва принят — мы выпустили то, что не умеем отозвать")
	}

	// (6) По адресу отвечает НЕ авторитет ⇒ отказ. Это ПОСТОЯННАЯ
	// неисправность: она отвечает каждому повтору одинаково, пока кто-нибудь не
	// изменит настройку, — поэтому вердикт липкий, и проба ставит этот случай
	// последним намеренно.
	st.ourAuth.notFound.Store(true)
	misconfigured := st.ours.mint(t, middleware.PlatformTokenType, "jti-misconfigured", nil)
	if code := st.callREST(t, misconfigured); code == http.StatusOK {
		t.Fatalf("по адресу авторитета отвечает не авторитет, а токен принят")
	}
	if code := st.callGRPC(t, misconfigured); code == codes.OK {
		t.Fatalf("та же посадка приняла токен на gRPC-поверхности")
	}
}

// TestF1b09_LegacyLaneKeepsItsDocumentedSoftPass — размен назван вслух и
// ПРЕДЪЯВЛЕН: полоса прежнего издателя своего поведения не меняет.
func TestF1b09_LegacyLaneKeepsItsDocumentedSoftPass(t *testing.T) {
	st := newF1bStand(t, true)

	st.oldAuth.down.Store(true)
	tok := st.legacy.mint(t, middleware.LegacyTokenType, "jti-legacy-down", nil)
	if got := st.callREST(t, tok); got != http.StatusOK {
		t.Fatalf("авторитет ПРЕЖНЕГО издателя недоступен, и его токен отвергнут (%d) — "+
			"фаза меняет поведение полосы, которого менять не собиралась: авторитет там "+
			"третья сторона, её доступностью мы не управляем", got)
	}

	// Отзыв на той полосе по-прежнему исполняется — мягкий проход относится к
	// НЕДОСТУПНОСТИ, а не к отказу.
	st.oldAuth.down.Store(false)
	st.oldAuth.revoked.Store(true)
	revoked := st.legacy.mint(t, middleware.LegacyTokenType, "jti-legacy-revoked", nil)
	if got := st.callREST(t, revoked); got == http.StatusOK {
		t.Fatalf("отозванный токен прежнего издателя принят — мягкий проход подменил собой " +
			"весь контроль")
	}
}

// ─── Ф1б-10: край требует привязку и валидирует её на ОБЕИХ поверхностях ────

// TestF1b10_EdgeRequiresBindingFromOurIssuersMachineTokens — вторая половина
// привязки: контур её проставляет, край её требует.
func TestF1b10_EdgeRequiresBindingFromOurIssuersMachineTokens(t *testing.T) {
	st := newF1bStandWithRequirement(t)

	machine := func(cnf map[string]any, jti string) string {
		return st.ours.mint(t, middleware.PlatformTokenType, jti, func(c jwt.MapClaims) {
			c["kaname_principal_type"] = "service_account"
			c["kaname_principal_id"] = "sva_deployer_a1b2"
			c["sub"] = "sva_deployer_a1b2"
			if cnf != nil {
				c["cnf"] = cnf
			}
		})
	}

	// (1) Машинный принципал НАШЕГО издателя БЕЗ привязки — отвергается на обеих.
	unbound := machine(nil, "jti-machine-unbound")
	if got := st.callREST(t, unbound); got == http.StatusOK {
		t.Fatalf("REST принял машинный токен НАШЕГО издателя без привязки — удостоверение, " +
			"воспроизводимое как обычный предъявительский, у машины ничем не защищено")
	}
	if got := st.callGRPC(t, unbound); got == codes.OK {
		t.Fatalf("нативная gRPC приняла машинный токен НАШЕГО издателя без привязки — " +
			"утверждение о крае, предъявленное на одной поверхности, сказано про половину")
	}

	// (2) Тот же токен С привязкой — принимается на обеих. Несущая половина:
	// без неё требование зеленеет на крае, отвергающем каждый машинный токен.
	bound := machine(map[string]any{"x5t#S256": "TCyU4b8s7Yy0aBcDeFgHiJkLmNoPqRsTuVwXyZ012ab"},
		"jti-machine-bound")
	if got := st.callREST(t, bound); got != http.StatusOK {
		t.Fatalf("REST отверг машинный токен НАШЕГО издателя С привязкой: %d", got)
	}
	if got := st.callGRPC(t, bound); got != codes.OK {
		t.Fatalf("нативная gRPC отвергла машинный токен НАШЕГО издателя С привязкой: %v", got)
	}

	// (3) ЧЕЛОВЕЧЕСКИЙ принципал без привязки остаётся законным: сужение здесь
	// было бы новым отказом тенанту, которого фаза не вводила.
	human := st.ours.mint(t, middleware.PlatformTokenType, "jti-human-unbound", nil)
	if got := st.callREST(t, human); got != http.StatusOK {
		t.Fatalf("человеческий токен без привязки отвергнут: %d — привязка потребована там, "+
			"где её не просили", got)
	}
}

// TestF1b11_12_PrincipalIsResolvedIdenticallyForBothIssuers — половина
// сценариев Ф1б-11/12, которую прежняя редакция объявляла и не утверждала.
//
// «Запрос прошёл» и «принципал определён так же» — разные утверждения, и
// разница между ними есть весь смысл перевода: токен нашей чеканки обязан
// давать ТОГО ЖЕ принципала, иначе смена издателя тихо сменила бы, за кого
// говорит удостоверение.
func TestF1b11_12_PrincipalIsResolvedIdenticallyForBothIssuers(t *testing.T) {
	st := newF1bStand(t, true)

	ourToken := st.ours.mint(t, middleware.PlatformTokenType, "jti-parity-ours", nil)
	legacyToken := st.legacy.mint(t, middleware.LegacyTokenType, "jti-parity-legacy", nil)

	require.Equal(t, http.StatusOK, st.callREST(t, ourToken))
	ourREST, _ := st.restPrincipal.Load().(string)
	require.Equal(t, http.StatusOK, st.callREST(t, legacyToken))
	legacyREST, _ := st.restPrincipal.Load().(string)

	require.NotEmpty(t, ourREST, "принципал не дошёл до обработчика вовсе — сравнивать нечего, "+
		"и молчание этой пробы сказано ни о чём")
	require.Equal(t, legacyREST, ourREST,
		"REST: токен НАШЕГО издателя дал принципала %q, токен прежнего — %q; смена чеканки "+
			"не вправе тихо сменить того, за кого говорит удостоверение", ourREST, legacyREST)

	require.Equal(t, codes.OK, st.callGRPC(t, ourToken))
	ourGRPC, _ := st.grpcPrincipal.Load().(string)
	require.Equal(t, codes.OK, st.callGRPC(t, legacyToken))
	legacyGRPC, _ := st.grpcPrincipal.Load().(string)

	require.NotEmpty(t, ourGRPC, "принципал не дошёл до обработчика на нативной поверхности")
	require.Equal(t, legacyGRPC, ourGRPC,
		"gRPC: %q против %q", ourGRPC, legacyGRPC)

	// И между поверхностями тоже: конфигурация одна, значит и принципал один.
	require.Equal(t, ourREST, ourGRPC,
		"одна конфигурация дала РАЗНЫХ принципалов на своих двух поверхностях (%q и %q) — "+
			"утверждение о крае, предъявленное на одной, сказано про половину", ourREST, ourGRPC)
}

// TestF1b12_AuthNRefusalsAreIndistinguishableOnTheNativeSurface — вторая
// объявленная и не утверждавшаяся половина: нативная поверхность отвечает на
// неудачу проверки подлинности ОДНИМ постоянным сообщением.
//
// Различимые тексты — перечислительный оракул: по ним предъявитель узнаёт, чем
// именно негоден его токен, и подбирает следующий.
func TestF1b12_AuthNRefusalsAreIndistinguishableOnTheNativeSurface(t *testing.T) {
	st := newF1bStand(t, true)
	stranger := newF1bSigner(t, "https://issuer.example.invalid", "x-1")

	refusals := map[string]string{
		"издатель без объявленной записи": stranger.mint(t, middleware.PlatformTokenType, "j1", nil),
		"наш издатель, чужой ключ": st.legacy.mint(t, middleware.PlatformTokenType, "j2",
			func(c jwt.MapClaims) { c["iss"] = f1bPlatformIssuer }),
		"наш издатель, тип не тот": st.ours.mint(t, "JWT", "j3", nil),
		"истёкший срок": st.ours.mint(t, middleware.PlatformTokenType, "j4",
			func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }),
		"чужой адресат": st.ours.mint(t, middleware.PlatformTokenType, "j5",
			func(c jwt.MapClaims) { c["aud"] = []any{"https://registry.kacho.test"} }),
	}

	seen := map[string][]string{}
	for name, tok := range refusals {
		conn, err := grpc.NewClient(st.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+tok)
		ierr := conn.Invoke(ctx, "/kacho.cloud.iam.v1.ProbeService/Ping", &emptyMsg{}, &emptyMsg{})
		cancel()
		_ = conn.Close()

		require.Error(t, ierr, "«%s» принят — тогда перечень отказов ниже сказан ни о чём", name)
		stt, _ := status.FromError(ierr)
		require.Equal(t, codes.Unauthenticated, stt.Code(), "«%s»: код отказа не тот", name)
		seen[stt.Message()] = append(seen[stt.Message()], name)
	}

	if len(seen) != 1 {
		var lines []string
		for msg, names := range seen {
			lines = append(lines, fmt.Sprintf("  %q ← %v", msg, names))
		}
		sort.Strings(lines)
		t.Fatalf("нативная поверхность ответила %d РАЗНЫМИ сообщениями на %d неудач проверки "+
			"подлинности:\n%s\n\nРазличимые тексты — перечислительный оракул: по ним предъявитель "+
			"узнаёт, чем именно негоден его токен, и подбирает следующий.",
			len(seen), len(refusals), strings.Join(lines, "\n"))
	}
	t.Logf("перепись: неудач проверки подлинности предъявлено %d, различимых сообщений %d",
		len(refusals), len(seen))
}
