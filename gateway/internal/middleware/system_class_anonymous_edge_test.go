// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

package middleware_test

// system_class_anonymous_edge_test.go — стадия S3 приёмки «аноним и вошедший —
// системный класс субъекта» (дом приёмки PRO-Robotech/kaname,
// docs/engineering/acceptance/anonymous-and-authenticated-are-subject-classes.md,
// отпечаток fa4ff26f…bb21757e; зеркало края — kacho#3238): путь анонима через
// цепочку края в процессе, посев П3.
//
// П3 — боевая посадка: слой аутентификации `NewAuthInterceptor(AuthModeProduction…)`
// с проверяющим подписи фикстуры, за ним посредник прав, за ним обработчик, на
// двух полосах — нативной (`auth.Unary()` → `mw.Unary()`) и REST
// (`auth.HTTP(mw.HTTP(…))`). Маркер `system/anonymous` и посадка `dev` не
// используются: боевая посадка такого состояния не производит.
//
// Каталог — одна строка `NetworkService/Get`, отличие строк — ровно поле
// `required_acr_min`: «0», «1» либо поле отсутствует. Ответчик модели —
// таблица исходов по субъекту вопроса (расширение `fakeChecker`, заказанное
// приёмкой для SC-34).
//
// Идентификатор объекта — правильной формы (`enp…`, 20 знаков): короткий
// `net-obj1` из текста приёмки отвергается краем как неправильный id раньше
// вопроса к модели (шаг 5b посредника) и ничего бы не различал.

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

const (
	scFQN       = "kacho.cloud.vpc.v1.NetworkService/Get"
	scObj1      = "enp00000000000000001" // объект существует
	scObjAbsent = "enp00000000000000002" // правильной формы, объекта нет
	scAnon      = "system_class:anonymous"
)

// scRow — строка каталога `NetworkService/Get`; floor == "-" — поле отсутствует.
func scRow(floor string) string {
	base := `{"fqn":"` + scFQN + `","permission":"vpc.networks.get","required_relation":"v_get",` +
		`"scope_extractor":{"object_type":"vpc_network","from_request_field":"network_id"}`
	if floor == "-" {
		return base + `}`
	}
	return base + `,"required_acr_min":"` + floor + `"}`
}

// tableChecker — ответчик модели с исходом по субъекту вопроса; записывает
// вопросы и считает вызовы.
type tableChecker struct {
	mu     sync.Mutex
	byWho  map[string]bool
	calls  atomic.Int64
	inputs []middleware.AuthzCheckInput
}

func newTableChecker(byWho map[string]bool) *tableChecker {
	return &tableChecker{byWho: byWho}
}

func (c *tableChecker) set(who string, allowed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byWho[who] = allowed
}

func (c *tableChecker) Check(_ context.Context, in middleware.AuthzCheckInput) (middleware.AuthzCheckResult, error) {
	c.calls.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inputs = append(c.inputs, in)
	return middleware.AuthzCheckResult{Allowed: c.byWho[in.Subject], CheckedAt: time.Now()}, nil
}

func (c *tableChecker) last() middleware.AuthzCheckInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.inputs) == 0 {
		return middleware.AuthzCheckInput{}
	}
	return c.inputs[len(c.inputs)-1]
}

// p3 — цепочка П3 на одном каталоге.
type p3 struct {
	t       *testing.T
	auth    *middleware.AuthInterceptor
	mw      *middleware.AuthzMiddleware
	checker *tableChecker
	jwks    *jwksFixture
	logBuf  *bytes.Buffer
	now     *atomic.Int64
}

func newP3(t *testing.T, floor string, checker *tableChecker) *p3 {
	t.Helper()
	fix := newJWKSFixture(t, "RS256")
	cat := buildCatalog(t, scRow(floor))
	auth := middleware.NewAuthInterceptor(middleware.AuthModeProduction, "secret", &fakeLookup{}, authTestLogger()).
		WithVerifier(rs256Verifier(t, fix))
	buf := &bytes.Buffer{}
	now := &atomic.Int64{}
	now.Store(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC).UnixNano())
	mw := buildAuthzMiddleware(t, cat, checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = &fakeRestRouter{m: map[string]string{
			"GET /vpc/v1/networks/" + scObj1:      scFQN,
			"GET /vpc/v1/networks/" + scObjAbsent: scFQN,
		}}
		c.Logger = slog.New(slog.NewTextHandler(&lockedWriter{w: buf}, &slog.HandlerOptions{Level: slog.LevelInfo}))
		c.Now = func() time.Time { return time.Unix(0, now.Load()).UTC() }
	})
	return &p3{t: t, auth: auth, mw: mw, checker: checker, jwks: fix, logBuf: buf, now: now}
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// nativeOutcome — что увидела нативная полоса.
type nativeOutcome struct {
	err        error
	reached    bool
	principals metadata.MD
}

func (p *p3) native(objID, bearer string) nativeOutcome {
	p.t.Helper()
	md := metadata.MD{}
	if bearer != "" {
		md.Set("authorization", "Bearer "+bearer)
	}
	ctx := metadata.NewIncomingContext(context.Background(), md)
	info := &grpc.UnaryServerInfo{FullMethod: "/" + scFQN}
	var out nativeOutcome
	_, out.err = p.auth.Unary()(ctx, &vpcv1.GetNetworkRequest{NetworkId: objID}, info,
		func(ctx context.Context, req any) (any, error) {
			return p.mw.Unary()(ctx, req, info, func(ctx context.Context, _ any) (any, error) {
				out.reached = true
				in, _ := metadata.FromIncomingContext(ctx)
				out.principals = metadata.MD{
					principalmeta.MetaPrincipalType: in.Get(principalmeta.MetaPrincipalType),
					principalmeta.MetaPrincipalID:   in.Get(principalmeta.MetaPrincipalID),
				}
				return &vpcv1.Network{Id: objID}, nil
			})
		})
	return out
}

// restOutcome — что увидела REST-полоса.
type restOutcome struct {
	code    int
	body    string
	reached bool
	pType   string
	pID     string
}

func (p *p3) rest(objID, bearer string) restOutcome {
	p.t.Helper()
	var out restOutcome
	h := p.auth.HTTP(p.mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out.reached = true
		out.pType = r.Header.Get(principalmeta.HeaderGRPCMetaPrincipalType)
		out.pID = r.Header.Get(principalmeta.HeaderGRPCMetaPrincipalID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"` + objID + `"}`))
	})))
	r := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks/"+objID, nil)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	out.code, out.body = w.Code, w.Body.String()
	return out
}

func (p *p3) usrBToken() string {
	return p.jwks.sign(p.t, issuerClaims("user", "usr-B"))
}

// statusBytes — статус целиком (код, текст, details) в байтах провода.
func statusBytes(t *testing.T, err error) []byte {
	t.Helper()
	st, ok := status.FromError(err)
	require.Truef(t, ok, "отказ не gRPC-статус: %v", err)
	b, merr := proto.Marshal(st.Proto())
	require.NoError(t, merr)
	return b
}

// todayRefusal — сегодняшний отказ без удостоверения на обеих полосах:
// строка «1», запрос без Authorization.
func todayRefusal(t *testing.T) (nativeErr []byte, restCode int, restBody string) {
	t.Helper()
	p := newP3(t, "1", newTableChecker(map[string]bool{scAnon: true}))
	n := p.native(scObj1, "")
	require.Error(t, n.err, "контроль: без удостоверения на строке «1» — отказ")
	r := p.rest(scObj1, "")
	return statusBytes(t, n.err), r.code, r.body
}

// SC-30 — аноним проходит до решения на методе с полом «0», обе полосы; до
// обработчика не доходят метаданные принципала. Близнец «И» — вошедший usr-B.
func TestSystemClass_SC30_AnonymousReachesTheDecisionOnFloorZero(t *testing.T) {
	twin := newP3(t, "0", newTableChecker(map[string]bool{"user:usr-B": true}))
	tn := twin.native(scObj1, twin.usrBToken())
	require.NoError(t, tn.err, "SC-30 близнец: вошедший usr-B на строке «0»")
	require.True(t, tn.reached)
	assert.Equal(t, []string{"user"}, tn.principals.Get(principalmeta.MetaPrincipalType))
	assert.Equal(t, []string{"usr-B"}, tn.principals.Get(principalmeta.MetaPrincipalID))
	tr := twin.rest(scObj1, twin.usrBToken())
	require.Equal(t, http.StatusOK, tr.code, "SC-30 близнец REST: тело %s", tr.body)
	assert.Equal(t, "user", tr.pType)
	assert.Equal(t, "usr-B", tr.pID)

	nat := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	n := nat.native(scObj1, "")
	require.NoError(t, n.err, "SC-30 нативная: аноним на строке «0» с выдачей")
	assert.True(t, n.reached, "SC-30 нативная: обработчик вызван")
	assert.EqualValues(t, 1, nat.checker.calls.Load(), "SC-30 нативная: дублёр спрошен ровно раз")
	in := nat.checker.last()
	assert.Equal(t, scAnon, in.Subject, "SC-30 нативная: субъект вопроса")
	assert.Equal(t, "v_get", in.RequiredRelation)
	assert.Equal(t, "vpc_network", in.ResourceType)
	assert.Equal(t, scObj1, in.ResourceID)
	assert.Empty(t, n.principals.Get(principalmeta.MetaPrincipalType), "SC-30 нативная: тип принципала не задан")
	assert.Empty(t, n.principals.Get(principalmeta.MetaPrincipalID), "SC-30 нативная: id принципала не задан")

	rst := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	r := rst.rest(scObj1, "")
	require.Equal(t, http.StatusOK, r.code, "SC-30 REST: аноним на строке «0» (тело %s)", r.body)
	assert.True(t, r.reached)
	assert.EqualValues(t, 1, rst.checker.calls.Load(), "SC-30 REST: дублёр спрошен ровно раз")
	assert.Equal(t, scAnon, rst.checker.last().Subject)
	assert.Empty(t, r.pType, "SC-30 REST: заголовок типа принципала отсутствует")
	assert.Empty(t, r.pID, "SC-30 REST: заголовок id принципала отсутствует")
}

// scRefusedBeforeDecision — отказ без удостоверения до решения на обеих
// полосах, побайтово равный сегодняшнему; дублёр не спрошен.
func scRefusedBeforeDecision(t *testing.T, floor, why string) {
	t.Helper()
	wantNative, wantCode, wantBody := todayRefusal(t)
	p := newP3(t, floor, newTableChecker(map[string]bool{scAnon: true}))
	n := p.native(scObj1, "")
	require.Error(t, n.err, why+": нативная")
	assert.Equal(t, codes.Unauthenticated, status.Code(n.err), why+": нативная — код")
	assert.Equal(t, wantNative, statusBytes(t, n.err), why+": нативная — статус побайтово")
	assert.False(t, n.reached, why+": нативная — обработчик не вызван")
	r := p.rest(scObj1, "")
	assert.Equal(t, wantCode, r.code, why+": REST — код")
	assert.Equal(t, wantBody, r.body, why+": REST — тело побайтово")
	assert.False(t, r.reached, why+": REST — обработчик не вызван")
	assert.Zero(t, p.checker.calls.Load(), why+": дублёр не спрошен")
}

// SC-31 — аноним на методе с полом «1» отвергается до решения; близнец — SC-30.
func TestSystemClass_SC31_FloorOneRefusesAnonymousBeforeTheDecision(t *testing.T) {
	twin := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	require.NoError(t, twin.native(scObj1, "").err, "SC-31 близнец SC-30: строка «0» пропускает анонима")
	scRefusedBeforeDecision(t, "1", "SC-31")
}

// SC-45 — пустой пол анонима не допускает даже при выдаче; близнец — SC-30.
func TestSystemClass_SC45_AbsentFloorDoesNotAdmitAnonymous(t *testing.T) {
	twin := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	require.NoError(t, twin.native(scObj1, "").err, "SC-45 близнец SC-30: строка «0» пропускает анонима")
	scRefusedBeforeDecision(t, "-", "SC-45")
}

// SC-32 — без выдачи анониму — прежний отказ, существование скрыто: (а), (б),
// (в) побайтово равны между собой и сегодняшнему отказу; близнец — SC-30.
func TestSystemClass_SC32_NoGrantRefusalHidesExistence(t *testing.T) {
	twin := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	require.NoError(t, twin.native(scObj1, "").err, "SC-32 близнец SC-30: с выдачей аноним проходит")

	wantNative, wantCode, wantBody := todayRefusal(t)
	for _, c := range []struct{ name, floor, obj string }{
		{"(а) существует", "0", scObj1},
		{"(б) нет объекта", "0", scObjAbsent},
		{"(в) строка «1»", "1", scObj1},
	} {
		p := newP3(t, c.floor, newTableChecker(map[string]bool{scAnon: false}))
		n := p.native(c.obj, "")
		require.Error(t, n.err, "SC-32 %s: нативная", c.name)
		assert.Equal(t, wantNative, statusBytes(t, n.err), "SC-32 %s: нативная — статус побайтово", c.name)
		assert.False(t, n.reached)
		r := p.rest(c.obj, "")
		assert.Equal(t, wantCode, r.code, "SC-32 %s: REST — код", c.name)
		assert.Equal(t, wantBody, r.body, "SC-32 %s: REST — тело побайтово", c.name)
		assert.False(t, r.reached)
	}
}

// SC-33 — негодное удостоверение анонимом не становится; близнец — SC-30.
func TestSystemClass_SC33_BadCredentialNeverBecomesAnonymous(t *testing.T) {
	twin := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	require.NoError(t, twin.native(scObj1, "").err, "SC-33 близнец SC-30: без заголовка аноним проходит")

	p := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	alien := newJWKSFixture(t, "RS256")
	bad := alien.sign(t, issuerClaims("user", "usr-B"))
	n := p.native(scObj1, bad)
	require.Error(t, n.err)
	assert.Equal(t, codes.Unauthenticated, status.Code(n.err), "SC-33 нативная")
	assert.False(t, n.reached)
	r := p.rest(scObj1, bad)
	assert.Equal(t, http.StatusUnauthorized, r.code, "SC-33 REST")
	assert.False(t, r.reached)
	assert.Zero(t, p.checker.calls.Load(), "SC-33: дублёр не спрошен")
}

// SC-34 — кэш решений края разделяет классы; положительный вердикт живёт не
// дольше TTL (5 с); близнец — сдвиг на 4 с ещё из кэша.
func TestSystemClass_SC34_DecisionCacheSeparatesClasses(t *testing.T) {
	ch := newTableChecker(map[string]bool{"user:usr-B": true, scAnon: false})
	p := newP3(t, "0", ch)
	require.NoError(t, p.native(scObj1, p.usrBToken()).err, "SC-34 Дано: вызов usr-B прошёл")

	n := p.native(scObj1, "")
	require.Error(t, n.err, "SC-34: ответ usr-B анониму не достался")
	assert.Equal(t, codes.Unauthenticated, status.Code(n.err))
	assert.Equal(t, scAnon, ch.last().Subject, "SC-34: дублёр спрошен субъектом анонима")

	ch.set(scAnon, true)
	require.NoError(t, p.native(scObj1, "").err, "SC-34: с выдачей анониму вызов проходит")
	ch.set(scAnon, false)
	p.now.Add(int64(4 * time.Second))
	require.NoError(t, p.native(scObj1, "").err, "SC-34 близнец: через 4 с — ещё из кэша")
	p.now.Add(int64(2 * time.Second))
	n = p.native(scObj1, "")
	require.Error(t, n.err, "SC-34: через 6 с после отзыва — отказ")
	assert.Equal(t, codes.Unauthenticated, status.Code(n.err))
}

// SC-35 — каталог отвергает пол «0» там, где решения нет, и нераспознанный пол;
// близнецы — «0», отсутствующее поле и «1» приняты.
func TestSystemClass_SC35_CatalogRefusesFloorZeroWithoutADecision(t *testing.T) {
	load := func(row string) error {
		return middleware.NewPermissionCatalog().LoadFromBytes([]byte("[" + row + "]"))
	}
	for _, twin := range []string{scRow("0"), scRow("-"), scRow("1")} {
		require.NoError(t, load(twin), "SC-35 близнец принят: %s", twin)
	}
	for name, row := range map[string]string{
		"(а) scope_filtered": `{"fqn":"` + scFQN + `","permission":"vpc.networks.get","required_relation":"v_get","scope_filtered":true,"required_acr_min":"0"}`,
		"(б) <exempt>":       `{"fqn":"` + scFQN + `","permission":"<exempt>","required_acr_min":"0"}`,
		"(в) v_update":       `{"fqn":"` + scFQN + `","permission":"vpc.networks.update","required_relation":"v_update","scope_extractor":{"object_type":"vpc_network","from_request_field":"network_id"},"required_acr_min":"0"}`,
		"(г) \"00\"":         scRow("00"),
		"(г) \" 0\"":         scRow(" 0"),
		"(г) \"high\"":       scRow("high"),
	} {
		err := load(row)
		if assert.Errorf(t, err, "SC-35 %s: строка обязана быть отвергнута", name) {
			assert.Contains(t, err.Error(), scFQN, "SC-35 %s: отказ называет метод", name)
			assert.Contains(t, err.Error(), "required_acr_min", "SC-35 %s: отказ называет поле", name)
		}
	}
}

// SC-38 — журнал решения анонима: строка на решение с методом, субъектом
// класса и исходом; ни заголовка Authorization, ни тела. Близнец — usr-B.
func TestSystemClass_SC38_DecisionLogNamesTheAnonymousClass(t *testing.T) {
	ch := newTableChecker(map[string]bool{"user:usr-B": true, scAnon: true})
	p := newP3(t, "0", ch)
	tok := p.usrBToken()
	require.NoError(t, p.native(scObj1, tok).err, "SC-38 близнец: решение usr-B")
	require.Contains(t, p.logBuf.String(), `subject=user:usr-B`, "SC-38 близнец: строка решения называет субъект")

	require.NoError(t, p.native(scObj1, "").err, "SC-38: SC-30")
	ch.set(scAnon, false)
	_ = p.native(scObjAbsent, "")
	log := p.logBuf.String()
	assert.Contains(t, log, `msg="authz allow" fqn=`+scFQN+` subject=`+scAnon, "SC-38: строка allow анонима")
	assert.Contains(t, log, `msg="authz deny" fqn=`+scFQN+` subject=`+scAnon, "SC-38: строка deny анонима")
	assert.NotContains(t, log, tok, "SC-38: токен в журнал не попадает")
	assert.NotContains(t, log, "Bearer", "SC-38: заголовок Authorization в журнал не попадает")
}
