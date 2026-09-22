// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_session_pii_log_test.go — ПЕРСОНАЛЬНЫЕ ДАННЫЕ И НОСИТЕЛЬ СЕССИИ В ЖУРНАЛ
// НЕ ПОПАДАЮТ (C4).
//
// # Замок был снят вместе с чужой полосой, а предмет переехал
//
// Держатель запрета был ОДИН на всё дерево — `auth_kratos_pii_log_test.go`
// (перемерено: `git grep -l PII <база> -- '*_test.go'` → 1 файл, на голове
// полосы → 0). Он утверждал на уровне НАБЛЮДАЕМОГО: буфер журнала не содержит
// почты, но содержит неперсональный идентификатор. Полоса, которую он сторожил,
// снята вместе с чужим поставщиком, и вместе с ней снят замок — но НЕ предмет:
// выжившая полоса нашей сессии несёт те же `Email` и `DisplayName`
// (`HumanSession`), и её строка внедрения личности отстоит от возврата класса
// на одну правку.
//
// Снятие замка без замены есть снятие ЗАЩИТЫ, а не снятие предмета
// (`testing.md` §subject-removal-two-outcomes): исходов два — снять вместе с
// предметом одним изменением либо перевести на живой признак. Предмет жив,
// поэтому здесь второе.
//
// # Что именно сторожится и почему трёх величин, а не одной
//
//   - `Email` и `DisplayName` — персональные данные человека
//     (`security-hardening.md`, инвариант «персональные данные не журналируются;
//     соотносить по неперсональному идентификатору»);
//   - носитель сессии — не персональные данные, а ДЕЙСТВУЮЩЕЕ УДОСТОВЕРЕНИЕ:
//     попав в журнал, оно становится второй дверью в ту же систему, и срок
//     жизни этой двери равен сроку хранения журналов.
//
// # Обе полосы и оба пути
//
// Полос, читающих нашу сессию, ДВЕ — личность на пути запроса (`tryOwnSession`)
// и ответ «кто я», — и они обязаны отвечать о сессии одинаково. Путей у каждой
// два: успех и отказ. Проверка только успешного пути оставила бы открытым
// ровно тот путь, на котором в журнал пишут охотнее всего — путь диагностики.
//
// # Положительный контроль обязателен
//
// Утверждение «почты в журнале нет» на потоке, где почты не было ВООБЩЕ,
// исполняется само и не сторожит ничего. Поэтому каждый случай доказывает, что
// величина БЫЛА в обращении: на пути запроса — тем, что журнал несёт
// неперсональный идентификатор того же субъекта; на «кто я» — тем, что почта и
// имя уехали в ТЕЛО ОТВЕТА, куда им и положено.
//
// # Чем доказано, что замок способен упасть
//
// Инъекцией в живую строку журнала: `"email", sess.Email` в
// `auth_own_session.go` красит `TestOwnSessionPII_RequestPathSuccess…` дословно
// «персональные данные утекли в журнал». Законный близнец — дерево как есть —
// молчит.
package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Величины НАРОЧИТО различимы: подстрока, случайно совпавшая с идентификатором
// или маршрутом, дала бы находку ни о чём — либо, наоборот, спрятала бы утечку
// за общим префиксом.
const (
	piiEmail       = "zoe.quartzman@example.test"
	piiDisplayName = "Zoe Quartzman"
	piiUserID      = "usr-pii-7"
	piiBearer      = "carrier-value-do-not-log-8f3a1c"
)

// piiSession — живая наша сессия, несущая обе персональные величины.
func piiSession() HumanSession {
	return HumanSession{
		UserID:          piiUserID,
		Email:           piiEmail,
		DisplayName:     piiDisplayName,
		AuthenticatedAt: ownAuthAt,
		ExpiresAt:       ownAuthAt.Add(24 * time.Hour),
		AssuranceLevel:  "1",
		EmailVerified:   true,
	}
}

// piiLogger — журнал в буфер, на уровне Debug: замок обязан видеть ВСЁ, что
// процесс пишет, а не только то, что переживает боевой порог.
func piiLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// requireNoSecretsInLog — общее утверждение обоих путей обеих полос.
func requireNoSecretsInLog(t *testing.T, where, logged string) {
	t.Helper()
	for _, secret := range []struct{ what, value string }{
		{"персональные данные (почта)", piiEmail},
		{"персональные данные (отображаемое имя)", piiDisplayName},
		{"действующий носитель сессии", piiBearer},
	} {
		if strings.Contains(logged, secret.value) {
			t.Errorf("%s: %s утекли в журнал.\n\nСоотносить субъекта надо по "+
				"НЕПЕРСОНАЛЬНОМУ идентификатору; носитель — действующее удостоверение, "+
				"и в журнале он живёт столько, сколько хранятся журналы.\n\nЖурнал:\n%s",
				where, secret.what, logged)
		}
	}
}

// TestOwnSessionPII_RequestPathSuccessLogsTheIdentifierNotThePerson — полоса
// личности, УСПЕШНЫЙ путь.
func TestOwnSessionPII_RequestPathSuccessLogsTheIdentifierNotThePerson(t *testing.T) {
	logger, buf := piiLogger()
	reader := &fakeHumanSession{sess: piiSession(), found: true}
	cut := &fakeCutoff{found: false}

	a := NewAuthInterceptor(AuthModeDev, "", cutoffLookup{}, logger).
		WithHumanSession(reader).
		WithSessionCutoffCheck(cut, time.Hour)
	next := &countingNext{}
	mux := http.NewServeMux()
	mux.Handle("/", next)

	rec := serve(a.HTTP(mux), withOurCarrier(
		httptest.NewRequest(http.MethodGet, platformPath, nil), piiBearer))

	if rec.Code != http.StatusOK || next.served != 1 {
		t.Fatalf("живая сессия не пропущена (код %d, следующее звено вызвано %d раз) — "+
			"предмет замка до строки журнала не доехал: %s", rec.Code, next.served, rec.Body.String())
	}

	logged := buf.String()
	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: строка внедрения личности состоялась и соотносит
	// субъекта по неперсональному идентификатору. Без него «почты нет»
	// неотличимо от «журнал пуст».
	if !strings.Contains(logged, piiUserID) {
		t.Fatalf("в журнале нет неперсонального идентификатора %q — строка внедрения "+
			"личности не состоялась, и утверждение о ней сказано ни о чём.\nЖурнал:\n%s",
			piiUserID, logged)
	}
	requireNoSecretsInLog(t, "полоса личности, успешный путь", logged)
}

// TestOwnSessionPII_RequestPathRefusalLogsTheRouteNotTheCarrier — полоса
// личности, путь ОТКАЗА.
//
// На этом пути сессии нет, и персональных данных в обращении тоже нет — а
// носитель ЕСТЬ, и он доехал до строки доклада вместе с маршрутом. Именно этот
// путь чаще всего дописывают «чтобы было видно, что не резолвится».
func TestOwnSessionPII_RequestPathRefusalLogsTheRouteNotTheCarrier(t *testing.T) {
	logger, buf := piiLogger()
	reader := &fakeHumanSession{err: errors.New("authority unavailable")}

	a := NewAuthInterceptor(AuthModeDev, "", cutoffLookup{}, logger).
		WithHumanSession(reader)
	next := &countingNext{}
	mux := http.NewServeMux()
	mux.Handle("/", next)

	rec := serve(a.HTTP(mux), withOurCarrier(
		httptest.NewRequest(http.MethodGet, platformPath, nil), piiBearer))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("неотвеченный вопрос о сессии обязан отвергаться (F4d-23), получено %d %s",
			rec.Code, rec.Body.String())
	}

	logged := buf.String()
	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: доклад состоялся и соотносится по маршруту.
	if !strings.Contains(logged, platformPath) {
		t.Fatalf("в журнале нет маршрута %q — доклад об отказе не состоялся, и "+
			"утверждение о нём сказано ни о чём.\nЖурнал:\n%s", platformPath, logged)
	}
	requireNoSecretsInLog(t, "полоса личности, путь отказа", logged)
}

// TestOwnSessionPII_WhoAmISuccessAnswersWithThePersonAndLogsNeither — «кто я»,
// УСПЕШНЫЙ путь.
//
// Здесь положительный контроль сильнейший из возможных: почта и имя уехали В
// ТЕЛО ОТВЕТА — то есть были в обращении заведомо, — и в журнале их всё равно
// нет. Ответ «кто я» и есть то место, где эти величины законны.
func TestOwnSessionPII_WhoAmISuccessAnswersWithThePersonAndLogsNeither(t *testing.T) {
	logger, buf := piiLogger()
	reader := &fakeHumanSession{sess: piiSession(), found: true}
	cut := &fakeCutoff{found: false}

	a := NewAuthInterceptor(AuthModeDev, "", cutoffLookup{},
		slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithHumanSession(reader).
		WithSessionCutoffCheck(cut, time.Hour)
	mux := http.NewServeMux()
	NewSessionIdentityHandler(logger).
		WithHumanSession(reader).
		WithSessionCutoff(cut).
		Register(mux)

	rec := serve(a.HTTP(mux), withOurCarrier(
		httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil), piiBearer))

	if rec.Code != http.StatusOK {
		t.Fatalf("«кто я» на живой сессии обязан отвечать 200, получено %d %s",
			rec.Code, rec.Body.String())
	}
	var answer struct {
		User struct {
			Email       string `json:"email"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("ответ «кто я» не разбирается: %v (%s)", err, rec.Body.String())
	}
	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: величины были в обращении и доехали туда, где
	// законны, — в ответ спрашивающему о себе.
	if answer.User.Email != piiEmail || answer.User.DisplayName != piiDisplayName {
		t.Fatalf("ответ «кто я» не несёт почты и имени (%q / %q) — величин не было в "+
			"обращении, и утверждение «их нет в журнале» исполнилось само",
			answer.User.Email, answer.User.DisplayName)
	}
	requireNoSecretsInLog(t, "«кто я», успешный путь", buf.String())
}

// TestOwnSessionPII_WhoAmIRefusalLogsNeitherThePersonNorTheCarrier — «кто я»,
// путь ОТКАЗА при УЖЕ РАЗРЕШЁННОЙ сессии.
//
// Единственный путь, на котором персональные данные и отказ встречаются в
// одном запросе: сессия разрешена (почта и имя на руках), а вопрос об отсечке
// остался без ответа — и строку пишет диагностика.
func TestOwnSessionPII_WhoAmIRefusalLogsNeitherThePersonNorTheCarrier(t *testing.T) {
	logger, buf := piiLogger()
	reader := &fakeHumanSession{sess: piiSession(), found: true}
	cut := &fakeCutoff{err: errors.New("cutoff authority unavailable")}

	mux := http.NewServeMux()
	NewSessionIdentityHandler(logger).
		WithHumanSession(reader).
		WithSessionCutoff(cut).
		Register(mux)

	// Обработчик зовётся напрямую: боевая цепочка на этом исходе отвергает
	// запрос РАНЬШЕ (F4d-23), и путь отказа самого «кто я» остался бы не пройден.
	rec := serve(mux, withOurCarrier(
		httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil), piiBearer))

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"user":null`) {
		t.Fatalf("неотвеченный вопрос об отсечке обязан отвечать анонимом, получено %d %s",
			rec.Code, rec.Body.String())
	}
	logged := buf.String()
	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: диагностическая строка состоялась.
	if !strings.Contains(logged, "session revocation check unanswered") {
		t.Fatalf("в журнале нет строки о неотвеченной отсечке — путь отказа не пройден, "+
			"и утверждение о нём сказано ни о чём.\nЖурнал:\n%s", logged)
	}
	if reader.asked == 0 {
		t.Fatal("сессия не разрешалась — персональных данных в обращении не было, " +
			"и замок исполнился сам")
	}
	requireNoSecretsInLog(t, "«кто я», путь отказа", logged)
}
