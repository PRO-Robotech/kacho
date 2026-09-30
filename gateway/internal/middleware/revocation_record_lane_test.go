// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// revocation_record_lane_test.go — полоса отзыва токена, издатель которого не
// помечен нашим (#2734), спрашивает НАШУ запись отзыва и на всяком
// неопределённом исходе отказывает.
//
// Прежде у этой полосы был мягкий проход: «авторитет не ответил» пропускало
// запрос дальше. Он был объявлен ради третьей стороны — прежнего поставщика,
// чьей доступностью мы не управляем. Поставщик снят, и спрашивать на этой
// полосе больше некого, кроме нас самих; мягкий проход означал бы «отзываем и
// свой же отзыв не исполняем» — контроль, действующий на выдаче и не
// действующий на предъявлении.
//
// Все случаи идут через цепочку `AuthInterceptor.HTTP` и нативную поверхность:
// утверждается наблюдаемое — дошёл ли запрос до следующего звена и чем ответил
// край.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// recordLaneVerifier — проверяющий, признающий предъявленное подписанным
// токеном издателя БЕЗ пометки «наш»: вопрос об отзыве уходит на полосу записи.
// ours ставит пометку — и вопрос уходит нашему авторитету.
type recordLaneVerifier struct {
	jti  string
	ours bool
}

func (v recordLaneVerifier) Verify(context.Context, string) (*VerifiedToken, error) {
	return &VerifiedToken{
		Subject: "usr-record-1",
		JTI:     v.jti,
		Raw:     recordLaneBearer,
		Claims: map[string]any{
			"kaname_principal_type": "user",
			"kaname_principal_id":   "usr-record-1",
		},
		ReadRevocation: v.ours,
	}, nil
}

// recordLaneBearer — строка, которую селектор полос признаёт асимметричным
// токеном (заголовок `alg=RS256`); подпись судит дублёр проверяющего выше.
const recordLaneBearer = "eyJhbGciOiJSUzI1NiJ9.e30.c2ln"

// recordLaneChecker — источник ответа полосы; исход задаётся явно.
type recordLaneChecker struct {
	err   error
	asked int
}

func (c *recordLaneChecker) Introspect(context.Context, string, string) (IntrospectionResult, error) {
	c.asked++
	if c.err != nil {
		return IntrospectionResult{}, c.err
	}
	return IntrospectionResult{Active: true}, nil
}

func recordLaneAuth(jti string, checker TokenRevocationChecker) *AuthInterceptor {
	return NewAuthInterceptor(AuthModeProduction, "", cutoffLookup{},
		slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithVerifier(recordLaneVerifier{jti: jti}).
		WithRevocationCheck(checker, time.Hour)
}

// recordLaneREST — код ответа и признак «дошёл до следующего звена».
func recordLaneREST(a *AuthInterceptor) (int, bool) {
	code, served, _ := recordLaneRESTBody(a)
	return code, served
}

// recordLaneRESTBody — то же и поле `message` тела отказа.
func recordLaneRESTBody(a *AuthInterceptor) (int, bool, string) {
	served := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
	req.Header.Set("Authorization", "Bearer "+recordLaneBearer)
	rec := httptest.NewRecorder()
	a.HTTP(next).ServeHTTP(rec, req)
	var body struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, served, body.Message
}

// recordLaneGRPC — код нативной поверхности.
func recordLaneGRPC(a *AuthInterceptor) codes.Code {
	return status.Code(recordLaneGRPCErr(a))
}

// recordLaneGRPCErr — ответ нативной поверхности целиком.
func recordLaneGRPCErr(a *AuthInterceptor) error {
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer "+recordLaneBearer))
	_, err := a.authorize(ctx, "/kacho.cloud.vpc.v1.NetworkService/List")
	return err
}

// recordLaneRefusalText — отказ «спросить не удалось» несёт ПОСТОЯННЫЙ текст на
// обеих поверхностях, и поданного источником текста в нём нет: причину знает
// журнал, провод получает одно и то же. Утверждается наблюдаемое — тело REST и
// сообщение статуса, — а не только код: эхо чужого текста в отказ код не меняет.
func recordLaneRefusalText(t *testing.T, a *AuthInterceptor, fed string) {
	t.Helper()
	_, _, msg := recordLaneRESTBody(a)
	if msg != revocationUnavailableReason {
		t.Fatalf("тело отказа REST: %q, ожидался постоянный текст %q", msg, revocationUnavailableReason)
	}
	grpcMsg := status.Convert(recordLaneGRPCErr(a)).Message()
	if grpcMsg != revocationUnavailableReason {
		t.Fatalf("сообщение отказа нативной поверхности: %q, ожидался постоянный текст %q",
			grpcMsg, revocationUnavailableReason)
	}
	if fed == "" {
		return
	}
	for surface, got := range map[string]string{"REST": msg, "gRPC": grpcMsg} {
		if strings.Contains(got, fed) {
			t.Fatalf("%s: в отказ ушёл текст, поданный источником (%q): %q", surface, fed, got)
		}
	}
}

// Законный близнец: запись ответила «не отозван» — запрос проходит на обеих
// поверхностях. Без него отказы ниже зеленели бы на полосе, отвергающей всё.
func TestRecordLane_LiveTokenPasses(t *testing.T) {
	checker := &recordLaneChecker{}
	a := recordLaneAuth("jti-live", checker)
	if code, served := recordLaneREST(a); code != http.StatusOK || !served {
		t.Fatalf("живой токен не прошёл REST: код %d, дошёл=%v", code, served)
	}
	if c := recordLaneGRPC(a); c != codes.OK {
		t.Fatalf("живой токен не прошёл нативную поверхность: %v", c)
	}
	if checker.asked != 2 {
		t.Fatalf("запись спрошена %d раз, ожидалось 2 (по разу на поверхность)", checker.asked)
	}
}

// Отозван в нашей записи — отказ «войди заново» на обеих поверхностях.
func TestRecordLane_RevokedTokenIsRefused(t *testing.T) {
	a := recordLaneAuth("jti-revoked", &recordLaneChecker{err: ErrTokenInactive})
	if code, served := recordLaneREST(a); code != http.StatusUnauthorized || served {
		t.Fatalf("отозванный токен: код %d, дошёл=%v — ожидался 401 без прохода", code, served)
	}
	if c := recordLaneGRPC(a); c != codes.Unauthenticated {
		t.Fatalf("отозванный токен на нативной поверхности: %v, ожидалось Unauthenticated", c)
	}
}

// СУТЬ: источник записи не ответил — ОТКАЗ, а не мягкий проход.
func TestRecordLane_SilentSourceRefusesInsteadOfPassing(t *testing.T) {
	const fed = "сосед не ответил: 10.0.0.7:9091 connection refused"
	a := recordLaneAuth("jti-silent", &recordLaneChecker{err: errors.New(fed)})
	code, served := recordLaneREST(a)
	if served {
		t.Fatalf("запрос прошёл при неотвеченном вопросе об отзыве (код %d): отозванный токен "+
			"действовал бы всё время, пока наша запись молчит", code)
	}
	if code != http.StatusServiceUnavailable {
		t.Fatalf("молчание источника обязано отвечать 503 «повтори позже», получено %d", code)
	}
	if c := recordLaneGRPC(a); c != codes.Unavailable {
		t.Fatalf("молчание источника на нативной поверхности: %v, ожидалось Unavailable", c)
	}
	recordLaneRefusalText(t, a, fed)
}

// Токен без идентификатора спросить о записи нечем — ОТКАЗ, а не «проверять
// нечего, проходи».
func TestRecordLane_TokenWithoutIdentifierIsRefused(t *testing.T) {
	checker := &recordLaneChecker{}
	a := recordLaneAuth("", checker)
	if code, served := recordLaneREST(a); served || code != http.StatusServiceUnavailable {
		t.Fatalf("токен без идентификатора: код %d, дошёл=%v — ожидался 503 без прохода", code, served)
	}
	if c := recordLaneGRPC(a); c != codes.Unavailable {
		t.Fatalf("токен без идентификатора на нативной поверхности: %v, ожидалось Unavailable", c)
	}
	if checker.asked != 0 {
		t.Fatalf("источник спрошен %d раз о токене без идентификатора", checker.asked)
	}
	recordLaneRefusalText(t, a, "")
}

// Журнал отказа по отзыву называет ТОТ источник, который ответил «отозван»:
// нашу запись на полосе записи и наш авторитет на полосе нашей чеканки. Прежний
// поставщик снят (#2734), и строка, называющая ответившим его, посылала бы
// оператора разбираться с тем, чего у края нет. Источник выводится тем же
// признаком записи издателя, которым выбрана полоса, — разойтись им нечем.
func TestRevocationRefusalLogNamesTheSourceThatAnswered(t *testing.T) {
	for _, lane := range []struct {
		ours bool
		want string
	}{
		{ours: false, want: revocationSourceRecord},
		{ours: true, want: revocationSourceAuthority},
	} {
		var logs bytes.Buffer
		a := NewAuthInterceptor(AuthModeProduction, "", cutoffLookup{},
			slog.New(slog.NewTextHandler(&logs, nil))).
			WithVerifier(recordLaneVerifier{jti: "jti-revoked", ours: lane.ours})
		checker := &recordLaneChecker{err: ErrTokenInactive}
		if lane.ours {
			a = a.WithPlatformRevocationCheck(checker, time.Hour)
		} else {
			a = a.WithRevocationCheck(checker, time.Hour)
		}
		if code, served := recordLaneREST(a); code != http.StatusUnauthorized || served {
			t.Fatalf("наш=%t: отозванный токен: код %d, дошёл=%v", lane.ours, code, served)
		}
		if c := recordLaneGRPC(a); c != codes.Unauthenticated {
			t.Fatalf("наш=%t: отозванный токен на нативной поверхности: %v", lane.ours, c)
		}
		if checker.asked != 2 {
			t.Fatalf("наш=%t: источник спрошен %d раз, ожидалось 2", lane.ours, checker.asked)
		}

		var named int
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			if strings.Contains(strings.ToLower(line), "provider") {
				t.Fatalf("наш=%t: журнал называет ответившим поставщика: %s", lane.ours, line)
			}
			if strings.Contains(line, `source="`+lane.want+`"`) {
				named++
			}
		}
		if named != 2 {
			t.Fatalf("наш=%t: строк отказа, называющих источником %q, %d, ожидалось 2 (по одной на "+
				"поверхность); журнал:\n%s", lane.ours, lane.want, named, logs.String())
		}
	}
}
