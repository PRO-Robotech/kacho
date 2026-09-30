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
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЛОСА, НА КОТОРОЙ ЭТО МЕРИЛОСЬ, СМЕНИЛАСЬ — И ОДИН ДОВОД ПРИ ЭТОМ ВЫБЫЛ
//
// Приводом всех случаев ниже была сессия ЧУЖОГО поставщика: её ответ нёс
// `authentication_methods`, и оттуда край брал `amr_claims`. Поставщик снят
// целиком, и браузерная полоса осталась одна — НАША сессия.
//
// Наша сессия множества предъявленного НЕ НЕСЁТ: состав ответа службы объявлен
// решением владельца (Ф3-09, Д12; Ф11 Р7), и полоса передаёт сборке доводов
// пустой перечень явно — `setSessionAssuranceHeaders(r, assurance, nil)` в
// `auth_own_session.go`. Следствие названо здесь вслух, потому что оно
// наблюдаемо: довод `amr_claims` на браузерной полосе НЕ ПРОИЗВОДИТСЯ, и
// разрешение, закрытое условием `mfa_fresh`, для браузерной сессии отвергается
// всегда. Отказ fail-closed, дыры нет; потеряна ВОЗМОЖНОСТЬ.
//
// Состояние это не новое и снятием поставщика не создано: под посадкой `own`
// читатель поставщика не провязывался вовсе, то есть на боевом стенде `own`
// довод не производился и раньше — зелёной проба была о полосе, которой у
// такого стенда нет. Снятие лишь сделало разрыв видимым.
//
// ПРЕДИКАТ СНЯТИЯ разрыва: ответ службы о НАШЕЙ сессии начинает нести перечень
// способов, полоса перестаёт передавать `nil`, и `amr_claims` появляется в
// доводах браузерной полосы. Предмет чужой — состав контракта службы доступа, —
// поэтому здесь он ИЗМЕРЯЕТСЯ переписью ниже, а не подпирается.

import (
	"bytes"
	"context"
	"fmt"
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
	"github.com/PRO-Robotech/kacho/internal/contractsource"
)

// conditionProbeNow — «сейчас» пробы. Часы управляемые: свежесть подтверждения
// есть разность двух моментов, и на настоящих часах утверждение о ней было бы
// то верным, то нет.
var conditionProbeNow = time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

// sessionFixture — ответ службы о НАШЕЙ сессии, которым правится одна проба.
//
// Уровень уверенности назван НА ОСИ КАТАЛОГА («1», «2», «3»): наша сессия
// объявляет его сама, и перевода со словаря чужого поставщика здесь нет
// (Ф11 Р7).
type sessionFixture struct {
	assuranceLevel  string
	authenticatedAt *time.Time
}

// ourSessionOf — живая сессия заданной формы, какой её отдаёт служба доступа.
func ourSessionOf(f sessionFixture) HumanSession {
	sess := HumanSession{
		UserID:         "usr_alice_acc_a1b2",
		Email:          "alice@example.test",
		DisplayName:    "Alice A",
		AssuranceLevel: f.assuranceLevel,
		EmailVerified:  true,
	}
	if f.authenticatedAt != nil {
		sess.AuthenticatedAt = *f.authenticatedAt
		sess.ExpiresAt = f.authenticatedAt.Add(24 * time.Hour)
	}
	return sess
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
		WithHumanSession(&fakeHumanSession{found: true, sess: ourSessionOf(f)})

	var out map[string]any
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vt, ok := verifiedTokenFromCtxOrHTTP(r.Context(), r)
		require.True(t, ok, "полоса сессии не донесла личности — привод пробы сломан, "+
			"и всякое утверждение о доводах ниже было бы про пустоту")
		ex := NewContextExtractor(func() time.Time { return conditionProbeNow }, true)
		out = ex.BuildHTTP(vt, r, ResolvedSubject{FGA: "user:usr_alice_acc_a1b2"})
		w.WriteHeader(http.StatusOK)
	}))
	req := withOurCarrier(httptest.NewRequest(http.MethodGet, "/compute/v1/instances/ins-1", nil), "opaque-a")
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
// верхней ступени, подтверждённая две минуты назад, доносит до решения те
// доводы, которые браузерная полоса ПРОИЗВОДИТ.
//
// Перечня способов среди них нет, и это не упущение привода: наша сессия
// множества предъявленного не несёт (шапка файла, Ф11 Р7). Утверждать его
// присутствие здесь значило бы требовать от полосы того, чего её источник не
// отдаёт; утверждать молча его отсутствие — потерять сам факт. Поэтому он
// назван отдельным случаем ниже и посчитан переписью.
func TestSessionLane_CarriesTheConditionArguments(t *testing.T) {
	at := conditionProbeNow.Add(-2 * time.Minute)
	ctxMap := driveSessionLane(t, sessionFixture{
		assuranceLevel:  "3",
		authenticatedAt: &at,
	}, nil)

	assert.Equal(t, "3", ctxMap["acr_value"],
		"ступень уверенности — довод, который полоса донесла ещё до #1252")
	require.Contains(t, ctxMap, "mfa_at",
		"момент подтверждения не доехал до решения: свежесть не с чем сравнить")

	now, okNow := ctxMap["current_time"].(int64)
	require.True(t, okNow, "«сейчас» обязано быть в доводах — иначе сравнивать не с чем")
	mfaAt, okAt := coerceUnixSeconds(ctxMap["mfa_at"])
	require.True(t, okAt, "момент подтверждения приехал в форме, которую не прочесть: %T", ctxMap["mfa_at"])
	assert.Equal(t, at.Unix(), mfaAt, "момент подтверждения обязан быть тем, что назвала служба")
	assert.Less(t, now-mfaAt, int64(15*60),
		"подтверждение внутри окна свежести — иначе положительный контроль ничего не показывает")
}

// TestSessionLane_MethodListIsAbsentOnTheBrowserLane — РАЗРЫВ НАЗВАН, а не
// оставлен молчанием.
//
// Довод `amr_claims` модель просит, а браузерная полоса его не производит:
// источник у неё один — наша сессия, — и множества предъявленного она не несёт.
// Пока это так, разрешение, закрытое условием `mfa_fresh`, браузерной сессии не
// выдаётся ни при каком входе.
//
// Случай заведён УТВЕРЖДЕНИЕМ, а не комментарием: появление перечня способов на
// этой полосе обязано покраснеть здесь — тогда разрыв закрыт, и случай
// снимается вместе с этой строкой. Обратное — молчаливое «стало работать» —
// оставило бы перепись ниже с числом, которое никто не перечитает.
func TestSessionLane_MethodListIsAbsentOnTheBrowserLane(t *testing.T) {
	at := conditionProbeNow.Add(-2 * time.Minute)
	ctxMap := driveSessionLane(t, sessionFixture{
		assuranceLevel:  "1",
		authenticatedAt: &at,
	}, nil)

	// Положительный контроль: ОСТАЛЬНЫЕ доводы доехали. Без него утверждение
	// ниже зеленело бы на полосе, которая не собирает вообще ничего.
	assert.Equal(t, "1", ctxMap["acr_value"], "положительный контроль: ступень доехала")
	require.Contains(t, ctxMap, "mfa_at", "положительный контроль: момент доехал")

	assert.NotContains(t, ctxMap, "amr_claims",
		"перечень способов появился на браузерной полосе — разрыв закрыт: снимите этот "+
			"случай и верните утверждение о нём в положительный контроль выше")
}

// TestSessionLane_MissingInstantIsAbsentNotZero — служба не назвала момента:
// довода нет, и он ОТСУТСТВУЕТ, а не приезжает эпохой. Ноль здесь читался бы
// как «подтверждено в 1970» — то есть как заведомо несвежее, что верно по
// исходу и неверно по смыслу; а любая арифметика над ним даёт число, которое
// нечем опровергнуть.
func TestSessionLane_MissingInstantIsAbsentNotZero(t *testing.T) {
	ctxMap := driveSessionLane(t, sessionFixture{assuranceLevel: "3"}, nil)

	assert.Equal(t, "3", ctxMap["acr_value"],
		"положительный контроль: остальные доводы доехали, отсутствует ровно момент")
	assert.NotContains(t, ctxMap, "mfa_at",
		"момента аутентификации служба не назвала — довод обязан отсутствовать, а не быть нулём")
}

// TestSessionLane_ForgedArgumentsAreNotBelieved — клиент подкладывает оба новых
// довода в ОБЕИХ поверхностных формах. Полоса при этом низкая, и ни один
// подложенный довод не вправе доехать.
//
// На НАШЕЙ полосе случай строже прежнего: перечня способов у источника нет
// вовсе, поэтому единственный способ ему здесь появиться — быть поверенным у
// клиента. Пустота в `amr_claims` тут не «полоса ничего не собрала», а
// «подложенное снято», и положительный контроль подделки рядом это различает.
func TestSessionLane_ForgedArgumentsAreNotBelieved(t *testing.T) {
	at := conditionProbeNow.Add(-2 * time.Minute)
	ctxMap := driveSessionLane(t, sessionFixture{
		assuranceLevel:  "1",
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
		"положительный контроль подделки: ступень взята у службы, а не у клиента")
	mfaAt, ok := coerceUnixSeconds(ctxMap["mfa_at"])
	require.True(t, ok, "положительный контроль: настоящий момент доехал")
	assert.Equal(t, at.Unix(), mfaAt, "момент обязан быть от службы, а не подложенным")
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
//
// КООРДИНАТА РАЗРЕШАЕТСЯ, А НЕ СОБИРАЕТСЯ ИЗ СЕГМЕНТОВ. Под `proto/` этого дерева
// модели больше нет: решением владельца (kacho#2616, исход C, 2026-09-13)
// контракты службы доступа уехали в её репозиторий и приезжают опубликованным
// модулем `github.com/PRO-Robotech/kaname` — каталогом `proto/kaname` внутри него.
// Собранный вручную путь после такого переезда не краснеет по существу: он даёт
// «нет такого файла», то есть отказ по координате вместо вердикта о предмете.
func mfaFreshArgumentsFromModel(t *testing.T) []string {
	t.Helper()
	model, err := contractsource.Path(conditionProbeRepoRoot(t), "kaname/cloud/iam/v1/fga_model.fga")
	require.NoError(t, err, "каноническая модель прав не разрешается — предмет гейта недоступен")
	raw, err := os.ReadFile(model)
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
// свежести, объявленный моделью, производится краем ХОТЯ БЫ НА ОДНОЙ полосе.
//
// Все доводы этого условия — запросные: свежесть подтверждения зависит от
// «сейчас» и не материализуема ни в какой записи (то же сказано на стороне,
// которая условие вычисляет). Поэтому производитель у каждого один — край, и
// довод, которого не производит НИ ОДНА полоса, означает условие, неисполнимое
// при любом входе.
//
// ЕДИНИЦА СЧЁТА — ДОВОД НА ПОЛОСУ, и чисел печатается три: доводов в модели,
// полос осмотрено, доводов покрыто объединением. Одно число здесь скрыло бы
// ровно то, что произошло: полосу, переставшую производить довод, при живом
// объединении.
//
// ПОЧЕМУ ОБЪЕДИНЕНИЕ, А НЕ КАЖДАЯ ПОЛОСА. Прежде утверждение стояло на ОДНОЙ
// полосе — браузерной, — и держалось приводом чужого поставщика. Поставщик
// снят; наша браузерная сессия множества предъявленного не несёт, и требовать
// от неё `amr_claims` значило бы требовать того, чего её источник не отдаёт.
// Утверждение поэтому приведено к тому, что оно и обосновывало дословно:
// «неисполнимо при ЛЮБОМ входе». Разрыв на браузерной полосе при этом не
// растворён — он назван отдельным случаем выше и виден числом здесь.
//
// Довод, добавленный условию со стороны ЗАПИСИ, тоже уронит этот гейт — и это
// верно: такое изменение требует осознанного решения о том, кто его поставляет,
// а не молчаливого прохода.
func TestMfaFreshArguments_EveryOneHasAProducerAtTheEdge(t *testing.T) {
	args := mfaFreshArgumentsFromModel(t)
	at := conditionProbeNow.Add(-2 * time.Minute)

	// Полосы, на которых край собирает доводы. Перечень — тот же, что в
	// сравнении ниже, и составлен из полос, ЖИВУЩИХ сегодня.
	lanes := map[string]map[string]any{
		"браузерная сессия (наша)": driveSessionLane(t, sessionFixture{
			assuranceLevel: "3", authenticatedAt: &at,
		}, nil),
		"предъявитель (REST)": bearerLaneArguments(t, at),
	}
	require.NotEmpty(t, lanes, "обход пуст — «ноль находок» неотличимо от «ноль прочитанного»")

	names := make([]string, 0, len(lanes))
	for n := range lanes {
		names = append(names, n)
	}
	sort.Strings(names)

	covered := 0
	for _, a := range args {
		producers := make([]string, 0, len(lanes))
		for _, n := range names {
			if _, ok := lanes[n][a]; ok {
				producers = append(producers, n)
			}
		}
		if len(producers) > 0 {
			covered++
			t.Logf("довод %q: производят %d из %d полос — %v", a, len(producers), len(lanes), producers)
			continue
		}
		t.Errorf("довод %q условия `mfa_fresh` не производит НИ ОДНА полоса края: условие "+
			"объявлено моделью и НЕИСПОЛНИМО при любом входе. Исходов три — дать производителя, "+
			"снять условие, либо нести записанный предикат снятия; четвёртого нет. "+
			"Осмотрено полос: %v", a, names)
	}
	t.Logf("перепись: доводов условия `mfa_fresh` в модели — %d · полос осмотрено — %d · "+
		"покрыто объединением — %d", len(args), len(lanes), covered)
}

// bearerLaneArguments — доводы, с которыми к решению идёт полоса предъявителя.
//
// Полоса поднимается ТЕМ ЖЕ восстановлением удостоверения из проброшенных
// величин, что и браузерная: предмет обеих — то, что край собрал ДЛЯ СЕБЯ по
// дороге к решению, и мерить их разными приводами значило бы сравнивать приводы.
func bearerLaneArguments(t *testing.T, at time.Time) map[string]any {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/compute/v1/instances/ins-1", nil)
	setPrincipalHeaders(r, "user", "usr_alice_acc_a1b2", "Alice A")
	require.Empty(t, setTokenContextHeaders(r, &VerifiedToken{
		Subject: "usr_alice_acc_a1b2",
		ACR:     "3",
		AMR:     []string{"webauthn"},
		ExtClaims: map[string]any{
			"kaname_principal_type": "user",
			"kaname_principal_id":   "usr_alice_acc_a1b2",
			"kaname_mfa_at":         at.Unix(),
		},
	}))
	vt, ok := verifiedTokenFromCtxOrHTTP(context.Background(), r)
	require.True(t, ok, "полоса предъявителя не донесла удостоверения — привод пробы сломан")
	ex := NewContextExtractor(func() time.Time { return conditionProbeNow }, true)
	return ex.BuildHTTP(vt, r, ResolvedSubject{FGA: "user:usr_alice_acc_a1b2"})
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
//
// Браузерная полоса в сравнение не входит ПО ТОЙ ЖЕ ФОРМЕ и по другой причине,
// и причина названа: источник у неё есть, но множества предъявленного он не
// отдаёт (Ф11 Р7). Свойство «доносит ОБА довода» на ней невыполнимо, и втащить
// её сюда значило бы либо получить красное о чужом предмете, либо ослабить
// сравнение до того, что обе оставшиеся полосы и так делают. Что именно она
// производит — считает перепись выше, единицей «довод на полосу».
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
