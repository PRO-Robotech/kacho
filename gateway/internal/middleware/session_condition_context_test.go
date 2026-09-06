// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// session_condition_context_test.go — доводы условия модели прав, собранные на
// БРАУЗЕРНОЙ полосе (#1252).
//
// ПРЕДМЕТ. Условие `mfa_fresh` модели прав требует верхней ступени уверенности
// ВМЕСТЕ с видом способа входа и свежестью подтверждения. Три из четырёх его
// доводов приезжают с запросом, и собирает их край: страж прав восстанавливает
// удостоверение из проброшенных заголовков и отдаёт его извлекателю контекста.
//
// До #1252 в этом наборе не было ни перечня способов, ни момента подтверждения:
// обе величины наполнялись только из утверждений настоящего токена, а
// восстановленный вид их не нёс. Условие оставалось объявленным и неисполнимым
// НИ ПРИ КАКОМ входе браузера — не «открытым», а именно неисполнимым.
//
// ПОЧЕМУ ПРОБА ВНУТРЕННЯЯ. Предмет — то, что край собирает ДЛЯ СЕБЯ по дороге к
// решению: восстановление удостоверения (`verifiedTokenFromCtxOrHTTP`) и сборка
// доводов (`ContextExtractor`). С поверхности виден только исход вопроса о
// правах, и он одинаков для «довод не приехал» и «довод приехал и не подошёл» —
// то есть ровно та разница, ради которой проба написана.
//
// ПОЧЕМУ С ПОЛОЖИТЕЛЬНЫМ КОНТРОЛЕМ. Утверждение «условие не выполняется» на
// полосе, которая вообще ничего не собирает, тождественно истинно. Поэтому
// каждый отрицательный близнец здесь обязан показать, что ОСТАЛЬНЫЕ доводы
// доехали, — иначе он зеленел бы на пустом наборе.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// conditionProbeNow — «сейчас» пробы. Часы управляемые: свежесть подтверждения
// есть разность двух моментов, и на настоящих часах утверждение о ней было бы
// то верным, то нет.
var conditionProbeNow = time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

// sessionFixture — ответ провайдера сессий, которым правится одна проба.
type sessionFixture struct {
	assuranceLevel  string
	methods         []string
	authenticatedAt *time.Time
}

// fakeKratosWithMethods поднимает провайдера сессий, отвечающего ЖИВОЙ сессией
// заданной формы. Перечень способов и момент подтверждения — те самые поля,
// которых до #1252 край не читал вовсе.
func fakeKratosWithMethods(t *testing.T, f sessionFixture) *httptest.Server {
	t.Helper()
	body := map[string]any{
		"active":                        true,
		"authenticator_assurance_level": f.assuranceLevel,
		"identity": map[string]any{
			"id":     "dc609064-d9f3-4e24-b574-d561c9f18359",
			"traits": map[string]any{"email": "alice@example.test"},
		},
	}
	if f.authenticatedAt != nil {
		body["authenticated_at"] = f.authenticatedAt.Format(time.RFC3339)
	}
	if f.methods != nil {
		ams := make([]any, 0, len(f.methods))
		for _, m := range f.methods {
			ams = append(ams, map[string]any{"method": m, "aal": f.assuranceLevel})
		}
		body["authentication_methods"] = ams
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/whoami" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Writer(w).Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// conditionProbeLookup — резолвер субъекта браузерной полосы.
type conditionProbeLookup struct{}

func (conditionProbeLookup) LookupByExternalID(context.Context, string) (Subject, error) {
	return Subject{Type: "user", ID: "usr_alice_acc_a1b2", DisplayName: "Alice A"}, nil
}

func conditionProbeLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

// driveSessionLane прогоняет ОДИН браузерный запрос через настоящую точку входа
// края и возвращает доводы, с которыми край пошёл бы за решением о правах.
//
// forged — заголовки, которые подкладывает КЛИЕНТ. Они здесь не украшение:
// namespace-политика края обязана снять их до выбора полосы, и проба подделки
// пользуется тем же приводом, что и положительная.
func driveSessionLane(t *testing.T, f sessionFixture, forged map[string]string) map[string]any {
	t.Helper()
	auth := NewAuthInterceptor(AuthModeProduction, "", conditionProbeLookup{}, conditionProbeLogger()).
		WithKratos(NewKratosClient(fakeKratosWithMethods(t, f).URL))

	var out map[string]any
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vt, ok := verifiedTokenFromCtxOrHTTP(r.Context(), r)
		require.True(t, ok, "полоса сессии не донесла личности — привод пробы сломан, "+
			"и всякое утверждение о доводах ниже было бы про пустоту")
		ex := NewContextExtractor(func() time.Time { return conditionProbeNow }, true)
		out = ex.BuildHTTP(vt, r, ResolvedSubject{FGA: "user:usr_alice_acc_a1b2"})
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/compute/v1/instances/ins-1", nil)
	req.Header.Set("Cookie", "ory_kratos_session=opaque-a")
	for k, v := range forged {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code,
		"браузерный запрос обязан дойти до backend: иначе доводов не собрано вовсе")
	require.NotNil(t, out, "доводы условия не собраны — привод не доехал до сборки")
	return out
}

// amrHas — есть ли способ среди приехавших. Читает то, что реально лежит в
// доводах, а не то, что проба надеется там увидеть.
func amrHas(ctxMap map[string]any, method string) bool {
	list, ok := ctxMap["amr_claims"].([]string)
	if !ok {
		return false
	}
	for _, m := range list {
		if m == method {
			return true
		}
	}
	return false
}

// TestSessionLane_CarriesTheConditionArguments — ПОЛОЖИТЕЛЬНЫЙ контроль: сессия
// верхней ступени, подтверждённая аппаратным ключом две минуты назад, доносит
// до решения ВСЕ доводы, которых условие просит от вызывающей стороны.
func TestSessionLane_CarriesTheConditionArguments(t *testing.T) {
	at := conditionProbeNow.Add(-2 * time.Minute)
	ctxMap := driveSessionLane(t, sessionFixture{
		assuranceLevel:  "aal3",
		methods:         []string{"password", "webauthn"},
		authenticatedAt: &at,
	}, nil)

	assert.Equal(t, "3", ctxMap["acr_value"],
		"ступень уверенности — довод, который полоса донесла ещё до #1252")
	assert.True(t, amrHas(ctxMap, "webauthn"),
		"перечень способов не доехал до решения: условие остаётся неисполнимым "+
			"(приехало: %v)", ctxMap["amr_claims"])
	require.Contains(t, ctxMap, "mfa_at",
		"момент подтверждения не доехал до решения: свежесть не с чем сравнить")

	now, okNow := ctxMap["current_time"].(int64)
	require.True(t, okNow, "«сейчас» обязано быть в доводах — иначе сравнивать не с чем")
	mfaAt, okAt := coerceUnixSeconds(ctxMap["mfa_at"])
	require.True(t, okAt, "момент подтверждения приехал в форме, которую не прочесть: %T", ctxMap["mfa_at"])
	assert.Equal(t, at.Unix(), mfaAt, "момент подтверждения обязан быть тем, что назвал провайдер")
	assert.Less(t, now-mfaAt, int64(15*60),
		"подтверждение внутри окна свежести — иначе положительный контроль ничего не показывает")
}

// TestSessionLane_MethodListTravelsEvenWhenItDoesNotSatisfy — ОТРИЦАТЕЛЬНЫЙ
// близнец с положительным контролем: вход по паролю доносит перечень способов
// ТОЖЕ, и в нём нет аппаратного ключа. Без первой половины утверждение «условие
// не выполнено» было бы тождественно истинным.
func TestSessionLane_MethodListTravelsEvenWhenItDoesNotSatisfy(t *testing.T) {
	at := conditionProbeNow.Add(-2 * time.Minute)
	ctxMap := driveSessionLane(t, sessionFixture{
		assuranceLevel:  "aal1",
		methods:         []string{"password"},
		authenticatedAt: &at,
	}, nil)

	list, ok := ctxMap["amr_claims"].([]string)
	require.True(t, ok, "перечень способов обязан доехать и здесь: иначе отрицание ниже пусто")
	assert.Equal(t, []string{"password"}, list)
	assert.False(t, amrHas(ctxMap, "webauthn"),
		"вход по паролю не вправе выглядеть подтверждённым аппаратным ключом")
	assert.Equal(t, "1", ctxMap["acr_value"])
}

// TestSessionLane_MissingInstantIsAbsentNotZero — провайдер не назвал момента:
// довода нет, и он ОТСУТСТВУЕТ, а не приезжает эпохой. Ноль здесь читался бы
// как «подтверждено в 1970» — то есть как заведомо несвежее, что верно по
// исходу и неверно по смыслу; а любая арифметика над ним даёт число, которое
// нечем опровергнуть.
func TestSessionLane_MissingInstantIsAbsentNotZero(t *testing.T) {
	ctxMap := driveSessionLane(t, sessionFixture{
		assuranceLevel: "aal3",
		methods:        []string{"webauthn"},
	}, nil)

	assert.True(t, amrHas(ctxMap, "webauthn"),
		"положительный контроль: остальные доводы доехали, отсутствует ровно момент")
	assert.NotContains(t, ctxMap, "mfa_at",
		"момента подтверждения провайдер не назвал — довод обязан отсутствовать, а не быть нулём")
}

// TestSessionLane_ForgedArgumentsAreNotBelieved — клиент подкладывает оба новых
// довода в ОБЕИХ поверхностных формах. Полоса при этом низкая, и ни один
// подложенный довод не вправе доехать.
func TestSessionLane_ForgedArgumentsAreNotBelieved(t *testing.T) {
	at := conditionProbeNow.Add(-2 * time.Minute)
	ctxMap := driveSessionLane(t, sessionFixture{
		assuranceLevel:  "aal1",
		methods:         []string{"password"},
		authenticatedAt: &at,
	}, map[string]string{
		"X-Kacho-Token-Amr":                  "webauthn",
		"Grpc-Metadata-X-Kacho-Token-Amr":    "webauthn",
		"X-Kacho-Token-Mfa-At":               fmt.Sprint(conditionProbeNow.Unix()),
		"Grpc-Metadata-X-Kacho-Token-Mfa-At": fmt.Sprint(conditionProbeNow.Unix()),
		"X-Kacho-Token-Acr":                  "3",
	})

	assert.False(t, amrHas(ctxMap, "webauthn"),
		"подложенный клиентом способ доехал до решения о правах")
	assert.Equal(t, "1", ctxMap["acr_value"],
		"положительный контроль подделки: ступень взята у провайдера, а не у клиента")
	mfaAt, ok := coerceUnixSeconds(ctxMap["mfa_at"])
	require.True(t, ok, "положительный контроль: настоящий момент доехал")
	assert.Equal(t, at.Unix(), mfaAt, "момент обязан быть провайдерским, а не подложенным")
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДИКАТ СНЯТИЯ, ВЫВЕДЕННЫЙ ИЗ МОДЕЛИ, А НЕ ВЫПИСАННЫЙ
//
// Перечень доводов условия принадлежит МОДЕЛИ ПРАВ и меняется вместе с ней.
// Выписанный здесь литералом, он измерял бы память автора: довод, добавленный
// условию завтра, не покраснел бы ничем — и условие снова стало бы объявленным
// и неисполнимым, ровно как до #1252.

// reMfaFreshSignature — объявление условия свежести в канонической модели.
var reMfaFreshSignature = regexp.MustCompile(`(?m)^condition\s+mfa_fresh\s*\(([^)]*)\)`)

// mfaFreshArgumentsFromModel выводит имена доводов условия из канонической
// модели прав.
//
// Читает ОБЪЯВЛЕНИЕ, а не чужой исходник: модель — данные, и другого источника
// у этого перечня нет. Отсутствие условия в модели — находка, а не повод
// промолчать: гейт, потерявший предмет, обязан краснеть, иначе он переживёт то,
// что им обозначалось.
func mfaFreshArgumentsFromModel(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(conditionProbeRepoRoot(t),
		"proto", "kacho", "cloud", "iam", "v1", "fga_model.fga"))
	require.NoError(t, err, "каноническая модель прав не прочитана — предмет гейта недоступен")
	m := reMfaFreshSignature.FindSubmatch(raw)
	require.NotNil(t, m,
		"в канонической модели нет объявления `condition mfa_fresh(...)`. Либо условие сняли — "+
			"тогда снимите и этот гейт вместе с производителями доводов на краю, — либо разбор "+
			"сломался. Гейт, которому нечего осматривать, обязан быть КРАСНЫМ, а не зелёным.")
	var args []string
	for _, p := range strings.Split(string(m[1]), ",") {
		name := strings.TrimSpace(strings.Split(strings.TrimSpace(p), ":")[0])
		if name != "" {
			args = append(args, name)
		}
	}
	require.NotEmpty(t, args, "объявление условия разобрано в ноль доводов — разбор сломан")
	return args
}

func conditionProbeRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller не ответил — корень дерева не найти")
	dir := filepath.Dir(file)
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("go.mod не найден обходом вверх от %s", file)
	return ""
}

// TestMfaFreshArguments_EveryOneHasAProducerAtTheEdge — КАЖДЫЙ довод условия
// свежести, объявленный моделью, производится краем на браузерной полосе.
//
// Все доводы этого условия — запросные: свежесть подтверждения зависит от
// «сейчас» и не материализуема ни в какой записи (то же сказано на стороне,
// которая условие вычисляет). Поэтому производитель у каждого один — край, и
// довод без производителя означает условие, неисполнимое при любом входе.
//
// Довод, добавленный условию со стороны ЗАПИСИ, тоже уронит этот гейт — и это
// верно: такое изменение требует осознанного решения о том, кто его поставляет,
// а не молчаливого прохода.
func TestMfaFreshArguments_EveryOneHasAProducerAtTheEdge(t *testing.T) {
	args := mfaFreshArgumentsFromModel(t)
	at := conditionProbeNow.Add(-2 * time.Minute)
	ctxMap := driveSessionLane(t, sessionFixture{
		assuranceLevel:  "aal3",
		methods:         []string{"webauthn"},
		authenticatedAt: &at,
	}, nil)

	produced := 0
	for _, a := range args {
		if _, ok := ctxMap[a]; ok {
			produced++
			continue
		}
		t.Errorf("довод %q условия `mfa_fresh` не производится краем: условие объявлено моделью "+
			"и НЕИСПОЛНИМО на браузерной полосе при любом входе. Исходов три — дать производителя, "+
			"снять условие, либо нести записанный предикат снятия; четвёртого нет. "+
			"Приехало: %v", a, ctxMap)
	}
	t.Logf("перепись: доводов условия `mfa_fresh` в модели — %d · произведено краем — %d",
		len(args), produced)
}

// TestConditionArguments_LanesAgree — сравнение ПОЛОС по одному свойству:
// доносит ли полоса доводы условия до решения о правах.
//
// Сравнение, а не проба каждой полосы отдельно: проба одной требует знать, каким
// свойство ДОЛЖНО быть, — а расхождение полос обычно и возникает как побочный
// эффект чужой правки, которую никто не принимал. Здесь спрашивается другое:
// решал ли кто-нибудь, что полосы различаются.
//
// Полоса базового удостоверения в сравнение не входит, и это НЕ умолчание: у
// однострочного секрета нет ни способа подтверждения, ни его момента — источника
// доводов не существует, и утверждать про него было бы нечего (то же основание,
// по которому у неё нулевой момент аутентификации).
func TestConditionArguments_LanesAgree(t *testing.T) {
	at := conditionProbeNow.Add(-3 * time.Minute)
	// Удостоверение предъявителя ровно того же смысла, что и сессия выше:
	// подтверждено аппаратным ключом, момент назван.
	bearer := &VerifiedToken{
		Subject: "usr_alice_acc_a1b2",
		ACR:     "3",
		AMR:     []string{"webauthn"},
		ExtClaims: map[string]any{
			"kaname_principal_type": "user",
			"kaname_principal_id":   "usr_alice_acc_a1b2",
			"kaname_mfa_at":         at.Unix(),
		},
	}

	lanes := map[string]func(t *testing.T) *VerifiedToken{
		"сессия провайдера (REST)": func(t *testing.T) *VerifiedToken {
			var got *VerifiedToken
			auth := NewAuthInterceptor(AuthModeProduction, "", conditionProbeLookup{}, conditionProbeLogger()).
				WithKratos(NewKratosClient(fakeKratosWithMethods(t, sessionFixture{
					assuranceLevel: "aal3", methods: []string{"webauthn"}, authenticatedAt: &at,
				}).URL))
			h := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got, _ = verifiedTokenFromCtxOrHTTP(r.Context(), r)
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/compute/v1/instances/ins-1", nil)
			req.Header.Set("Cookie", "ory_kratos_session=opaque-a")
			h.ServeHTTP(httptest.NewRecorder(), req)
			return got
		},
		"предъявитель (REST)": func(t *testing.T) *VerifiedToken {
			r := httptest.NewRequest(http.MethodGet, "/compute/v1/instances/ins-1", nil)
			setPrincipalHeaders(r, "user", "usr_alice_acc_a1b2", "Alice A")
			require.Empty(t, setTokenContextHeaders(r, bearer))
			got, _ := verifiedTokenFromCtxOrHTTP(context.Background(), r)
			return got
		},
		"предъявитель (нативный gRPC)": func(t *testing.T) *VerifiedToken {
			ctx, unusable := withTokenContextMetadata(context.Background(), bearer)
			require.Empty(t, unusable)
			md, _ := metadata.FromIncomingContext(ctx)
			md = md.Copy()
			md.Set(principalmeta.MetaPrincipalID, "usr_alice_acc_a1b2")
			md.Set(principalmeta.MetaPrincipalType, "user")
			got, _ := verifiedTokenFromCtxOrHTTP(metadata.NewIncomingContext(context.Background(), md), nil)
			return got
		},
	}

	ex := NewContextExtractor(func() time.Time { return conditionProbeNow }, true)
	carrying := 0
	names := make([]string, 0, len(lanes))
	for n := range lanes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		vt := lanes[name](t)
		require.NotNil(t, vt, "полоса %q не донесла удостоверения — привод сломан", name)
		ctxMap := ex.BuildHTTP(vt, nil, ResolvedSubject{FGA: "user:usr_alice_acc_a1b2"})
		hasMethod := amrHas(ctxMap, "webauthn")
		_, hasInstant := ctxMap["mfa_at"]
		if hasMethod && hasInstant {
			carrying++
			continue
		}
		t.Errorf("полоса %q не доносит доводы условия (способ=%v, момент=%v): "+
			"полосы одного механизма разошлись, и этого никто не решал. Приехало: %v",
			name, hasMethod, hasInstant, ctxMap)
	}
	t.Logf("перепись: полос с источником доводов — %d · доносят доводы — %d", len(lanes), carrying)
}
