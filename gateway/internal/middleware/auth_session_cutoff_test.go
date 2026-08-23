// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// cutoffLookup — SubjectLookuper без расширения Kratos, чтобы полоса шла
// простой веткой резолва и доходила до вопроса про отсечку.
type cutoffLookup struct{ subj Subject }

func (c cutoffLookup) LookupByExternalID(context.Context, string) (Subject, error) {
	return c.subj, nil
}

// fakeCutoff — наш авторитет отзыва. Три исхода задаются явно, потому что
// именно их различение и есть предмет полосы.
type fakeCutoff struct {
	cutoff time.Time
	found  bool
	err    error
	asked  int
	forID  string
}

func (f *fakeCutoff) SessionCutoffOf(_ context.Context, userID string) (time.Time, bool, error) {
	f.asked++
	f.forID = userID
	return f.cutoff, f.found, f.err
}

// kratosStub — провайдер, называющий момент аутентификации сессии.
func kratosStub(t *testing.T, authenticatedAt time.Time) *httptest.Server {
	t.Helper()
	at := ""
	if !authenticatedAt.IsZero() {
		at = `,"authenticated_at":"` + authenticatedAt.UTC().Format(time.RFC3339Nano) + `"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"active":true`+at+
			`,"identity":{"id":"kid-1","traits":{"email":"a@example.com"}}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// carrierEnded — погасил ли ответ носителя браузерной сессии.
func carrierEnded(res *http.Response) bool {
	for _, c := range res.Cookies() {
		if c.Name == "ory_kratos_session" && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

// runCookieLane прогоняет один запрос с cookie сессии через полосу личности.
func runCookieLane(t *testing.T, kratosURL string, cut *fakeCutoff) (*http.Response, bool) {
	t.Helper()
	served := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true })

	a := NewAuthInterceptor(AuthModeDev, "",
		cutoffLookup{subj: Subject{Type: "user", ID: "usr-1", DisplayName: "A"}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	).WithKratos(NewKratosClient(kratosURL))
	if cut != nil {
		a = a.WithSessionCutoffCheck(cut, time.Hour)
	}

	req := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
	// Уникальная cookie на прогон: кэш сессии ключуется целиком по Cookie-header,
	// и общий литерал сделал бы вердикт функцией порядка проб.
	req.Header.Set("Cookie", "ory_kratos_session="+t.Name())
	rec := httptest.NewRecorder()
	a.HTTP(next).ServeHTTP(rec, req)
	return rec.Result(), served
}

// TestCookieLane_SessionAtOrBeforeCutoffIsRefused — ЦЕНТРАЛЬНОЕ утверждение
// подфазы: запись, которую делает НАШ глагол выхода, действует на предъявлении
// браузерной сессии.
//
// До этой полосы административный принудительный выход возвращал успех, а
// человек продолжал работать в консоли: его личность резолвится по cookie, а ни
// один читатель нашей отсечки на этом пути не стоял.
//
// Утверждается НАБЛЮДАЕМОЕ — код ответа, непрохождение запроса дальше и
// окончание носителя, — а не «функция вызвана».
func TestCookieLane_SessionAtOrBeforeCutoffIsRefused(t *testing.T) {
	authAt := time.Now().Add(-2 * time.Hour)
	cut := &fakeCutoff{cutoff: authAt.Add(time.Hour), found: true}

	res, served := runCookieLane(t, kratosStub(t, authAt).URL, cut)

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("сессия старше отсечки обязана быть отвергнута: получено %d, ожидалось 401", res.StatusCode)
	}
	if served {
		t.Fatal("запрос отвергнутой сессии дошёл до backend — отказ не состоялся")
	}
	if !carrierEnded(res) {
		t.Fatal("носитель сессии не погашен: отказ остаётся СТОЯЩИМ — " +
			"человек будет отвергаться при каждом обращении, а повторной аутентификации ничто не запросит")
	}
	if cut.asked != 1 {
		t.Fatalf("авторитет отзыва спрошен %d раз, ожидался 1", cut.asked)
	}
	if cut.forID != "usr-1" {
		t.Fatalf("спрошено про %q, а личность на этой полосе — usr-1", cut.forID)
	}
}

// TestCookieLane_SessionExactlyAtCutoffIsRefused — ГРАНИЦА, и она включающая.
//
// Отсечка ставится моментом «сейчас», а метка времени сессии у провайдера имеет
// конечное разрешение: совпадение двух моментов — не редкость, а обычный исход
// принудительного выхода сразу после входа. Исключающая граница делала бы такой
// выход недействительным ровно в этом случае, и заметить это можно было бы
// только по жалобе.
func TestCookieLane_SessionExactlyAtCutoffIsRefused(t *testing.T) {
	authAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	cut := &fakeCutoff{cutoff: authAt, found: true}

	res, served := runCookieLane(t, kratosStub(t, authAt).URL, cut)

	if res.StatusCode != http.StatusUnauthorized || served {
		t.Fatalf("сессия, аутентифицировавшаяся РОВНО в момент отсечки, обязана быть отвергнута: код %d, дошло=%v",
			res.StatusCode, served)
	}
	if !carrierEnded(res) {
		t.Fatal("носитель не погашен на границе отсечки")
	}
}

// TestCookieLane_SessionAfterCutoffPasses — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ.
//
// Без него отрицание выше зеленело бы на полосе, отвергающей всё. Он же
// утверждает свойство «отсечка действует ВПЕРЁД»: человек, вошедший заново,
// работает — отзыв снимает выданное, а не блокирует принципала навсегда.
func TestCookieLane_SessionAfterCutoffPasses(t *testing.T) {
	authAt := time.Now()
	cut := &fakeCutoff{cutoff: authAt.Add(-time.Hour), found: true}

	res, served := runCookieLane(t, kratosStub(t, authAt).URL, cut)

	if res.StatusCode != http.StatusOK {
		t.Fatalf("сессия моложе отсечки обязана проходить: получено %d", res.StatusCode)
	}
	if !served {
		t.Fatal("законная сессия не дошла до backend")
	}
	if carrierEnded(res) {
		t.Fatal("носитель законной сессии погашен — выход применён к тому, кого не отзывали")
	}
}

// TestCookieLane_NoCutoffPasses — человек, которого никто не отзывал, работает.
// Пустой ответ авторитета обязан означать ПУСТО, а не «отозвано всё».
func TestCookieLane_NoCutoffPasses(t *testing.T) {
	cut := &fakeCutoff{found: false}

	res, served := runCookieLane(t, kratosStub(t, time.Now()).URL, cut)

	if res.StatusCode != http.StatusOK || !served {
		t.Fatalf("без отсечки сессия обязана проходить: код %d, дошло=%v", res.StatusCode, served)
	}
	if carrierEnded(res) {
		t.Fatal("носитель погашен при отсутствующей отсечке")
	}
}

// TestCookieLane_UnansweredAuthorityRefusesButKeepsCarrier — два исхода, которые
// лечатся ПРОТИВОПОЛОЖНО, и потому обязаны различаться.
//
// «Отозван» — определённый ответ: носителя заканчиваем. «Спросить не удалось» —
// заминка своего же соседа: отвергаем (авторитет наш, и мягкий проход означал бы
// «отзываем и свой же отзыв не исполняем»), но носителя НЕ гасим — иначе первый
// перебой выкинул бы всех, кого никто не отзывал.
func TestCookieLane_UnansweredAuthorityRefusesButKeepsCarrier(t *testing.T) {
	cut := &fakeCutoff{err: errors.New("authority unreachable")}

	res, served := runCookieLane(t, kratosStub(t, time.Now()).URL, cut)

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("молчащий СВОЙ авторитет обязан давать отказ: получено %d", res.StatusCode)
	}
	if served {
		t.Fatal("запрос прошёл при неотвеченном вопросе об отзыве")
	}
	if carrierEnded(res) {
		t.Fatal("носитель погашен из-за заминки авторитета — заминка выкинула бы всех, " +
			"кого никто не отзывал")
	}
}

// TestCookieLane_UnsupportedAuthorityPassesLoudly — ТРЕТЬЕ состояние, и слитое
// с «не ответил» оно стоило бы простоя консоли.
//
// Раскат не атомарен: реплика края поднимается раньше, чем докатится служба
// прав, и в этом окне она отвечает «метода нет». Отвергать здесь значит
// отвергнуть КАЖДУЮ браузерную сессию на всё окно раската — авария, а не
// ужесточение; состояние при этом сходится само.
//
// Отличается от неисправности настройки предикатом: расхождение версий исчезает
// от раската, неверный адрес — никогда.
func TestCookieLane_UnsupportedAuthorityPassesLoudly(t *testing.T) {
	cut := &fakeCutoff{err: ErrSessionCutoffUnsupported}

	res, served := runCookieLane(t, kratosStub(t, time.Now()).URL, cut)

	if res.StatusCode != http.StatusOK || !served {
		t.Fatalf("окно раската обязано проходить, иначе консоль лежит весь раскат: код %d, дошло=%v",
			res.StatusCode, served)
	}
	if carrierEnded(res) {
		t.Fatal("носитель погашен в окне раската")
	}
}

// TestCookieLane_SessionWithoutAuthInstantIsRefusedWhenCutoffExists — доказать
// нечем ⇒ не пропускаем.
//
// Момент аутентификации приходит от провайдера, и он вправе его не назвать.
// Тогда сравнить с отсечкой не с чем: пропустить такую сессию значило бы, что
// отзыв обходится ОТСУТСТВИЕМ поля в чужом ответе. Та же посадка, что у хука
// обновления, где «нет момента ⇒ отказ» уже принята.
func TestCookieLane_SessionWithoutAuthInstantIsRefusedWhenCutoffExists(t *testing.T) {
	cut := &fakeCutoff{cutoff: time.Now().Add(-time.Hour), found: true}

	res, served := runCookieLane(t, kratosStub(t, time.Time{}).URL, cut)

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("сессия без момента аутентификации при живой отсечке обязана быть отвергнута: получено %d",
			res.StatusCode)
	}
	if served {
		t.Fatal("сессия без момента аутентификации прошла при живой отсечке")
	}
}

// TestCookieLane_UnmountedReaderLeavesLanePassing — граница названа вслух.
//
// Непровязанный читатель — состояние стенда, а не решение о доступе: полоса
// работает как прежде. Проба стоит затем, чтобы «полоса без читателя» и «полоса
// с читателем, сказавшим „годно“» не сливались в один наблюдаемый исход в глазах
// следующего читателя кода.
func TestCookieLane_UnmountedReaderLeavesLanePassing(t *testing.T) {
	res, served := runCookieLane(t, kratosStub(t, time.Now()).URL, nil)

	if res.StatusCode != http.StatusOK || !served {
		t.Fatalf("без провязанного читателя полоса обязана работать как прежде: код %d, дошло=%v",
			res.StatusCode, served)
	}
}

// TestCookieLane_RefusalMessageNamesOwnSessionOnly — отказ говорит про
// СОБСТВЕННУЮ сессию вызывающего и ничего про чужие: оракулом он быть не должен,
// но и «войди заново» обязан сказать, иначе клиент будет повторять запрос.
func TestCookieLane_RefusalMessageNamesOwnSessionOnly(t *testing.T) {
	authAt := time.Now().Add(-time.Hour)
	cut := &fakeCutoff{cutoff: authAt.Add(time.Minute), found: true}

	res, _ := runCookieLane(t, kratosStub(t, authAt).URL, cut)
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if !strings.Contains(string(body), sessionCutoffDenyDescription) {
		t.Fatalf("отказ не назвал состояние собственной сессии вызывающего: %s", string(body))
	}
	for _, leak := range []string{"usr-1", "kid-1", "a@example.com"} {
		if strings.Contains(string(body), leak) {
			t.Fatalf("в теле отказа личность вызывающего (%q): %s", leak, string(body))
		}
	}
}
